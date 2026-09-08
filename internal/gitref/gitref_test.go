package gitref

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// hardenGitRepo disables every path that can spawn a git process
// outliving the test that created the repository
// (plans/os-c4e8b57a.md D1, D2). `t.TempDir` removes its tree
// recursively at cleanup, and a detached auto-gc still writing under
// it fails the removal AFTER the assertions passed: the worst shape of
// flake for an unattended loop, because the signal says "your change
// is broken" when the change is fine.
//
// The three settings are three different spawners, and are WRITTEN
// into the repository rather than passed as `git -c` flags: `-c`
// scopes a value to one invocation and writes nothing, so the later
// commits, and above all a bare remote's own receive-pack, would still
// run under stock auto-gc. `init` and `clone --bare` produce no
// objects of their own, so a config write on the next line is still
// before the first object and there is no window to lose.
func hardenGitRepo(t testing.TB, repo string) {
	t.Helper()
	for _, kv := range [][2]string{
		{"gc.auto", "0"},            // the heuristic itself
		{"gc.autoDetach", "false"},  // any gc that runs stays in the foreground
		{"receive.autoGC", "false"}, // the push path, which is the one that bit
		{"core.autocrlf", "false"},  // the ledger is LF-only on every platform (spec/platform.md)
		{"core.eol", "lf"},
	} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("hardening %s (%s): %v %s", repo, kv[0], err, out)
		}
	}
}

// hardenGitEnv points GIT_CONFIG_GLOBAL at a config disabling the same
// three auto-gc paths for EVERY git process this test binary spawns,
// whoever spawns it (plans/os-c4e8b57a.md D1, D5).
//
// hardenGitRepo covers the repositories the fixtures create. It cannot
// cover the one that actually lost this race in CI: `gitref.NewClient`
// inits a private bare git dir at <stateDir>/gitdir, and the tests hand
// it a t.TempDir, so the repository is created by PRODUCTION code with
// no fixture line to harden. `git -c` is not an option there either, for
// the same reason it was not one for the fixtures: it writes nothing.
// The environment reaches it without one line of production change (D4),
// because gc.auto and receive.autoGC are read from every config scope.
//
// Both belts are kept. This one is process-scoped and a future test that
// clears the environment would drop it; the repo-local writes survive
// that, and each one is verifiable with `git -C <repo> config --get`.
func hardenGitEnv() (func(), error) {
	dir, err := os.MkdirTemp("", "seed-gitconfig-*")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config")
	body := "[gc]\n\tauto = 0\n\tautoDetach = false\n[receive]\n\tautoGC = false\n[core]\n\tautocrlf = false\n\teol = lf\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.Setenv("GIT_CONFIG_GLOBAL", path); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return func() { os.RemoveAll(dir) }, nil
}

func TestMain(m *testing.M) {
	cleanup, err := hardenGitEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func bareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("bare init: %v %s", err, out)
	}
	hardenGitRepo(t, dir)
	return dir
}

func fixtureKey(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

const ref = "refs/seed/ledger"

// seedGenesis lands the genesis on the remote through the loop itself.
func seedGenesis(t *testing.T, remote string, signer ed25519.PrivateKey) ledger.Resolver {
	t.Helper()
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := genesis.Build(signer, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(rec.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.AppendLoop(Draft{
		V: rec.Event.V, TS: rec.Event.TS, Actor: rec.Event.Actor,
		Verb: rec.Event.Verb, Subject: rec.Event.Subject, Payload: rec.Event.Payload,
	}, func(e event.Event) (*event.Record, error) {
		return event.Sign(e, signer)
	}, resolve, nil, 3)
	if err != nil || res.Position != 0 {
		t.Fatalf("genesis append: %+v %v", res, err)
	}
	return resolve
}

func milestoneDraft(t testing.TB, priv ed25519.PrivateKey, n int) Draft {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return Draft{
		V: "seed/0", TS: "2026-09-01T01:00:00Z", Actor: fp,
		Verb: "message.sent", Subject: "c-0001",
		Payload: json.RawMessage(fmt.Sprintf(`{"n": %d}`, n)),
	}
}

// conformance: III.A — the race drill: two concurrent appenders, no lost
// updates, one linear verifying chain, and a real retry observed.
func TestRaceDrillTwoConcurrentAppenders(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)

	const perWriter = 4
	totalAttempts := make([]int, 2)
	var wg sync.WaitGroup
	errs := make(chan error, 2*perWriter)
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			c, err := NewClient(t.TempDir(), remote, ref)
			if err != nil {
				errs <- err
				return
			}
			for i := 0; i < perWriter; i++ {
				res, err := c.AppendLoop(milestoneDraft(t, signer, w*100+i),
					func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) },
					resolve, nil, 20)
				if err != nil {
					errs <- err
					return
				}
				totalAttempts[w] += res.Attempts
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil {
		t.Fatalf("converged chain must verify: %v", err)
	}
	if rep.Count != 1+2*perWriter {
		t.Fatalf("lost updates: chain has %d events, want %d", rep.Count, 1+2*perWriter)
	}
	if totalAttempts[0]+totalAttempts[1] <= 2*perWriter {
		t.Fatalf("the race was not real: total attempts %d for %d appends", totalAttempts[0]+totalAttempts[1], 2*perWriter)
	}
}

// conformance: III.A — rollback to a valid earlier tip is refused by the
// monotonic-head rule at the fetch boundary (plans/os-62e2aa1d.md step 2).
func TestRollbackDrillRefusesHeadRegression(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)

	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AppendLoop(milestoneDraft(t, signer, 2),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5); err != nil {
		t.Fatal(err)
	}

	// Roll the remote back to the earlier, internally valid tip.
	if out, err := exec.Command("git", "--git-dir", remote, "update-ref", ref, first.Commit).CombinedOutput(); err != nil {
		t.Fatalf("rollback: %v %s", err, out)
	}
	_, err = c.AppendLoop(milestoneDraft(t, signer, 3),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
	if !errors.Is(err, ErrHeadRegression) {
		t.Fatalf("rolled-back remote must refuse with ErrHeadRegression, got %v", err)
	}

	// A fresh client with no persisted head cannot detect the rollback:
	// the charter's honest residual, asserted so nobody claims otherwise.
	fresh, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Fetch(); err != nil {
		t.Fatalf("fresh client bounds freshness only out-of-band, got %v", err)
	}
}

// conformance: III.A — deleting the remote ref outright is the deepest
// rollback; a client holding a verified head must refuse it rather than
// push a fresh root over deleted history (#86 review).
func TestVanishedRefIsHeadRegression(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "--git-dir", remote, "update-ref", "-d", ref).CombinedOutput(); err != nil {
		t.Fatalf("delete ref: %v %s", err, out)
	}
	if _, err := c.Fetch(); !errors.Is(err, ErrHeadRegression) {
		t.Fatalf("a vanished ref after a verified head must refuse with ErrHeadRegression, got %v", err)
	}
	// A client that never verified a head still reads it as a fresh ledger.
	fresh, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	if tip, err := fresh.Fetch(); err != nil || tip != "" {
		t.Fatalf("headless client must see a fresh ledger, got %q %v", tip, err)
	}
}

// A remote policy refusal (pre-receive hook declining) is not a race: it
// must surface as ErrRemoteRejected on the first attempt, never loop into
// ErrRetriesSpent (#86 review).
func TestPolicyRejectionSurfacesWithoutRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server; a bare Windows checkout runs the cooperative or forge-hosted posture (spec/platform.md)")
	}
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	counter := filepath.Join(t.TempDir(), "invocations")
	hook := fmt.Sprintf("#!/bin/sh\necho x >> %s\necho 'seed policy says no' >&2\nexit 1\n", counter)
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
	if !errors.Is(err, ErrRemoteRejected) {
		t.Fatalf("a declined push must surface ErrRemoteRejected, got %v", err)
	}
	if errors.Is(err, ErrRetriesSpent) {
		t.Fatalf("a policy refusal must not exhaust retries: %v", err)
	}
	b, err := os.ReadFile(counter)
	if err != nil || strings.Count(string(b), "x") != 1 {
		t.Fatalf("the refused push must not retry: hook ran %q (%v)", b, err)
	}
}

func TestValidateRefusalSurfacesAndNothingPushes(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tipBefore, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("policy says no")
	_, err = c.AppendLoop(milestoneDraft(t, signer, 9),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) },
		resolve,
		func(store *ledger.Store, rec *event.Record) error { return refusal },
		5)
	if !errors.Is(err, refusal) {
		t.Fatalf("validate refusal must surface its reason, got %v", err)
	}
	tipAfter, err := c.Fetch()
	if err != nil || tipAfter != tipBefore {
		t.Fatalf("a refused draft must push nothing: %s vs %s (%v)", tipBefore, tipAfter, err)
	}
}

func TestUnreachableRemoteIsTyped(t *testing.T) {
	c, err := NewClient(t.TempDir(), filepath.Join(t.TempDir(), "missing.git"), ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing remote must refuse with ErrUnavailable, got %v", err)
	}
}

func TestMaterializeRoundTrip(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	seedGenesis(t, remote, signer)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil || tip == "" {
		t.Fatalf("fetch: %q %v", tip, err)
	}
	a := t.TempDir()
	if err := c.Materialize(tip, a); err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile(filepath.Join(a, "HEAD"))
	if err != nil || len(head) == 0 {
		t.Fatalf("materialized tree must carry HEAD: %v", err)
	}
	commit, err := c.CommitAndPush(a, tip, "ledger: no-op recommit")
	if err != nil {
		t.Fatalf("recommit of identical tree: %v", err)
	}
	b := t.TempDir()
	if err := c.Materialize(commit, b); err != nil {
		t.Fatal(err)
	}
	headB, err := os.ReadFile(filepath.Join(b, "HEAD"))
	if err != nil || string(headB) != string(head) {
		t.Fatalf("round trip drifted: %q vs %q (%v)", head, headB, err)
	}
}

func TestRetryBoundExhausts(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	// A validate hook that sneaks a competing append in first on every
	// attempt forces perpetual non-fast-forward losses.
	n := 100
	_, err = c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) },
		resolve,
		func(store *ledger.Store, rec *event.Record) error {
			rival, err := NewClient(t.TempDir(), remote, ref)
			if err != nil {
				return err
			}
			n++
			_, err = rival.AppendLoop(milestoneDraft(t, signer, n),
				func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
			return err
		},
		2)
	if !errors.Is(err, ErrRetriesSpent) {
		t.Fatalf("perpetual losses must exhaust with ErrRetriesSpent, got %v", err)
	}
}

func TestSmallErrorPaths(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Materialize("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", t.TempDir()); err == nil {
		t.Error("materializing an unknown commit must error")
	}
	blocked := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Materialize("", filepath.Join(blocked, "sub")); err == nil {
		t.Error("materializing under a file path must error")
	}

	signErr := errors.New("keyless")
	if _, err := c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return nil, signErr },
		resolve, nil, 0); !errors.Is(err, signErr) {
		t.Errorf("signer failure must surface (and maxAttempts<1 clamps to 1), got %v", err)
	}

	if _, err := NewClient(filepath.Join(blocked, "state"), remote, ref); err == nil {
		t.Error("NewClient under a file path must error")
	}
}

// RecordVerifiedHead arms the monotonic-head rule for a caller that
// verified a fetched tip itself, before any AppendLoop persistence
// (plans/os-895bf828.md step 1: the pre-flight rollback window).
func TestRecordVerifiedHeadArmsRegressionRefusal(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	genesisTip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5); err != nil {
		t.Fatal(err)
	}

	fresh, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := fresh.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.RecordVerifiedHead(tip); err != nil {
		t.Fatal(err)
	}
	// Roll the remote back to the earlier valid tip: the recorded head
	// must make the next fetch refuse, with no append in between.
	if out, err := exec.Command("git", "--git-dir", remote, "update-ref", ref, genesisTip).CombinedOutput(); err != nil {
		t.Fatalf("rollback: %v %s", err, out)
	}
	if _, err := fresh.Fetch(); !errors.Is(err, ErrHeadRegression) {
		t.Fatalf("recorded head must refuse the rollback, got %v", err)
	}
}

// conformance: III.F — the claim race storm (plans/os-5dc16a7c.md, the
// Phase 5 exit drill): N concurrent claimants race one ready contract
// through the full admission rule set against a real remote. Exactly
// one claim admits, every loser receives the structured contention
// refusal naming holder and fence, the converged chain verifies from
// genesis, and no update is lost.
func TestClaimRaceStorm(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	sign := func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }

	// Stage: upgrade to seed/1, file, specify — the contract is ready.
	setup, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct{ v, verb, subject, payload string }{
		{"seed/0", "system.protocol.upgraded", "system", `{"to": "seed/1"}`},
		{"seed/1", "intent.filed", "c-race", `{"intent": "storm", "tier": "standard", "budget": "small", "routing": "core"}`},
		{"seed/1", "contract.specified", "c-race", `{"acceptance": {"ref": "specs/race.md @ abc1234", "executable": false}}`},
	} {
		if _, err := setup.AppendLoop(Draft{
			V: s.v, TS: "2026-09-01T01:00:00Z", Actor: fp,
			Verb: s.verb, Subject: s.subject, Payload: json.RawMessage(s.payload),
		}, sign, resolve, admit.Validate(), 5); err != nil {
			t.Fatalf("staging %s: %v", s.verb, err)
		}
	}

	// Each claimant carries its own timestamp: distinct drafts make
	// the race real (byte-identical drafts from one key converge on
	// one commit, and git treats pushing an already-landed commit as
	// an idempotent success — the chain stays correct, but nobody
	// races anything).
	const claimants = 6
	var wg sync.WaitGroup
	results := make([]error, claimants)
	for i := 0; i < claimants; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := NewClient(t.TempDir(), remote, ref)
			if err != nil {
				results[i] = err
				return
			}
			_, err = c.AppendLoop(Draft{
				V: "seed/1", TS: fmt.Sprintf("2026-09-01T01:00:%02dZ", i+1), Actor: fp,
				Verb: "claim.taken", Subject: "c-race", Payload: json.RawMessage(`{}`),
			}, sign, resolve, admit.Validate(), 30)
			results[i] = err
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range results {
		if err == nil {
			winners++
			continue
		}
		var ce *admit.ContentionError
		if !errors.As(err, &ce) || ce.Subject != "c-race" || ce.Holder != fp || ce.Fence < 0 {
			t.Fatalf("claimant %d must lose with structured contention naming holder and fence, got %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("exactly one claim must admit, got %d", winners)
	}

	// The converged chain verifies from genesis with exactly one claim
	// landed: genesis, upgrade, filed, specified, one claim.taken.
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil {
		t.Fatalf("converged chain must verify: %v", err)
	}
	if rep.Count != 5 {
		t.Fatalf("no lost or duplicated updates: chain has %d events, want 5", rep.Count)
	}
	claims := 0
	if err := store.Records(func(pos int, r *event.Record) error {
		if r.Event.Verb == "claim.taken" {
			claims++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("exactly one claim record may land, got %d", claims)
	}
}

// conformance: plans/os-c4e8b57a.md — the client's own private git dir
// is a repository no fixture creates, so it is the one the fixture
// hardening cannot reach. It is also the one that lost the race in CI
// (unlinkat .../state/gitdir: directory not empty, after the
// assertions had passed).
//
// What this pins is the EFFECTIVE setting under the test binary, and
// the scope supplying it is the process-wide GIT_CONFIG_GLOBAL that
// hardenGitEnv installs, NOT a repository-local write: NewClient inits
// the git dir and writes no config, because D4 keeps production
// unchanged (review finding on this PR). `git config --get` reads
// every scope, which is the question worth asking here — "can a
// detached gc still fire under this directory" — and the drill earns
// its place by failing when the mechanism is removed, which is
// mutation-checked. It does NOT establish that the client hardens its
// own git dir, and outside a test binary nothing does; whether it
// should is a production question this card's scope excludes, carded
// separately.
func TestClientGitDirHasNoAutoGC(t *testing.T) {
	state := t.TempDir()
	if _, err := NewClient(state, bareRemote(t), "refs/seed/ledger"); err != nil {
		t.Fatal(err)
	}
	assertLocalNoAutoGC(t, filepath.Join(state, "gitdir"), "the client's git dir")
}

// assertLocalNoAutoGC reads each key with --local, the repository's
// own scope, which the process-wide global TestMain installs cannot
// satisfy (plans/os-711b3028.md D3): before that plan the drill read
// the effective value and passed for the wrong reason.
func assertLocalNoAutoGC(t *testing.T, gitDir, what string) {
	t.Helper()
	for key, want := range map[string]string{
		"gc.auto":        "0",
		"gc.autoDetach":  "false",
		"receive.autoGC": "false",
	} {
		// The read scrubs GIT_CONFIG too: under it --local refuses, and
		// the drill that plants the variable must still read the
		// repository's own file.
		cmd := exec.Command("git", "-C", gitDir, "config", "--local", "--get", key)
		cmd.Env = withoutGitConfigSelection(os.Environ())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s unset in %s's own config: %v %s", key, what, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s = %q in %s, want %q — a detached gc under this dir outlives the process that armed it", key, got, what, want)
		}
	}
}

// GIT_CONFIG selects the file `git config` reads and writes: an
// unqualified write under it lands in the operator's file, and --local
// under it refuses outright (review finding on #232). The client must
// harden its own dir regardless, and the file the operator selected
// must come out untouched.
func TestClientHardensDespiteGitConfigSelection(t *testing.T) {
	// The fixture remote is built before the variable is planted: its
	// own test-side hardening (hardenGitRepo) is an unqualified write
	// that would land in the operator's file too, and this drill is
	// about the client, not the fixture.
	remote := bareRemote(t)
	external := filepath.Join(t.TempDir(), "operator-config")
	t.Setenv("GIT_CONFIG", external)
	state := t.TempDir()
	if _, err := NewClient(state, remote, "refs/seed/ledger"); err != nil {
		t.Fatalf("NewClient under GIT_CONFIG: %v", err)
	}
	assertLocalNoAutoGC(t, filepath.Join(state, "gitdir"), "the client's git dir under GIT_CONFIG")
	if b, err := os.ReadFile(external); err == nil {
		t.Fatalf("the operator's selected config file was written: %q", b)
	}
}

// An older build's state dir carries no configuration at all: the
// git dir exists, so NewClient does not init, and the write must
// happen on that path too (D1: every construction, never only init).
// A second construction changes nothing, which is what makes the
// unconditional write safe to run on every open.
func TestClientHardensAnOlderBuildsGitDir(t *testing.T) {
	state := t.TempDir()
	gitDir := filepath.Join(state, "gitdir")
	if out, err := exec.Command("git", "init", "-q", "--bare", gitDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	hardenGitRepo(t, gitDir)
	for _, key := range []string{"gc.auto", "gc.autoDetach", "receive.autoGC"} {
		if out, err := exec.Command("git", "-C", gitDir, "config", "--local", "--unset", key).CombinedOutput(); err != nil {
			t.Fatalf("unset %s to stage the older build's dir: %v %s", key, err, out)
		}
	}
	if out, err := exec.Command("git", "-C", gitDir, "config", "--local", "--get", "gc.auto").CombinedOutput(); err == nil {
		t.Fatalf("the staged dir still carries gc.auto=%s: the drill would prove nothing", strings.TrimSpace(string(out)))
	}
	if _, err := NewClient(state, bareRemote(t), "refs/seed/ledger"); err != nil {
		t.Fatal(err)
	}
	assertLocalNoAutoGC(t, gitDir, "the older build's git dir")
	before, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(state, bareRemote(t), "refs/seed/ledger"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("a second NewClient rewrote the config:\n%s\n--- became ---\n%s", before, after)
	}
}
