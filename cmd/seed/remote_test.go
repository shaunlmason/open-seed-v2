package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

const remoteRef = "refs/seed/ledger"

// fixturePriv is the same key writeKeys marshals for the CLI, so the
// CLI's --key is the genesis governance root.
func fixturePriv(t testing.TB) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

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
	body := "[gc]\n\tauto = 0\n\tautoDetach = false\n[receive]\n\tautoGC = false\n"
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

// seedRemoteGenesis lands the genesis on the remote through the loop and
// returns the resolver for library-side appends.
func seedRemoteGenesis(t *testing.T, remote string) ledger.Resolver {
	t.Helper()
	return seedRemoteGenesisAs(t, remote, fixturePriv(t))
}

// seedRemoteGenesisAs lands a genesis rooted at the given key: the
// federation drills need remotes with distinct roots.
func seedRemoteGenesisAs(t *testing.T, remote string, priv ed25519.PrivateKey) ledger.Resolver {
	t.Helper()
	c, err := gitref.NewClient(t.TempDir(), remote, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := genesis.Build(priv, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
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
	res, err := c.AppendLoop(gitref.Draft{
		V: rec.Event.V, TS: rec.Event.TS, Actor: rec.Event.Actor,
		Verb: rec.Event.Verb, Subject: rec.Event.Subject, Payload: rec.Event.Payload,
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, nil, 3)
	if err != nil || res.Position != 0 {
		t.Fatalf("genesis append: %+v %v", res, err)
	}
	return resolve
}

// libAppend lands one signed event on the remote through the loop.
func libAppend(t *testing.T, remote string, resolve ledger.Resolver, v, verb, subject, payload string, vopts ...ledger.VerifyOption) {
	t.Helper()
	libAppendAs(t, remote, resolve, fixturePriv(t), v, verb, subject, payload, vopts...)
}

// libAppendAs lands one signed event under the given key.
func libAppendAs(t *testing.T, remote string, resolve ledger.Resolver, priv ed25519.PrivateKey, v, verb, subject, payload string, vopts ...ledger.VerifyOption) {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	c, err := gitref.NewClient(t.TempDir(), remote, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AppendLoop(gitref.Draft{
		V: v, TS: "2026-09-01T01:00:00Z", Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload),
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, nil, 5, vopts...); err != nil {
		t.Fatal(err)
	}
}

func remoteTip(t *testing.T, remote string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "--quiet", "--verify", remoteRef).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// conformance: III.B cooperative posture — a clean remote append lands
// through self-validation and the landed chain verifies from genesis.
func TestRemoteAppendRoundTrip(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	seedRemoteGenesis(t, remote)

	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", filepath.Join(dir, "state"),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "1" {
		t.Fatalf("remote append failed: %d %+v", code, e)
	}
	if e.Result["commit"] == "" || e.Result["appended"] == "" || e.Result["attempts"].(float64) < 1 {
		t.Fatalf("result must carry commit, hash, attempts: %+v", e.Result)
	}

	// The landed chain verifies from genesis on a fresh materialization.
	c, err := gitref.NewClient(t.TempDir(), remote, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	ld := filepath.Join(dir, "materialized")
	if err := c.Materialize(tip, ld); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 || e.Result["count"].(float64) != 2 {
		t.Fatalf("landed chain must verify: %d %+v", code, e)
	}
}

// conformance: III.B cooperative posture — invalid drafts refuse locally
// with the remote tip unchanged.
func TestRemoteAppendCooperativeRefusals(t *testing.T) {
	_, priv, _ := writeKeys(t)

	t.Run("halted chain refuses at 7", func(t *testing.T) {
		remote := bareRemote(t)
		resolve := seedRemoteGenesis(t, remote)
		libAppend(t, remote, resolve, "seed/0", "system.halt.declared", "system", `{"reason": "drill"}`)
		before := remoteTip(t, remote)
		e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", t.TempDir(),
			"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
		if code != 7 || e.Error == nil || e.Error.Code != "halted" || !strings.Contains(e.Error.Message, "drill") {
			t.Fatalf("halted remote must refuse at 7 with the reason, got %d %+v", code, e)
		}
		if e.Position == nil || *e.Position != "1" {
			t.Fatalf("admission refusals are stamped at the tip they were computed at, got %+v", e.Position)
		}
		if after := remoteTip(t, remote); after != before {
			t.Fatalf("refused draft must push nothing: %s vs %s", before, after)
		}
	})

	t.Run("hostile payload refuses at 9", func(t *testing.T) {
		remote := bareRemote(t)
		seedRemoteGenesis(t, remote)
		before := remoteTip(t, remote)
		hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
		e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", t.TempDir(),
			"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", hostile)
		if code != 9 || e.Error == nil || e.Error.Code != "classification_refused" {
			t.Fatalf("hostile payload must refuse at 9, got %d %+v", code, e)
		}
		if after := remoteTip(t, remote); after != before {
			t.Fatal("refused draft must push nothing")
		}
	})

	t.Run("upgraded remote refuses stale build at 10", func(t *testing.T) {
		remote := bareRemote(t)
		resolve := seedRemoteGenesis(t, remote)
		libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "seed/10"}`)
		before := remoteTip(t, remote)
		e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", t.TempDir(),
			"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
		if code != 10 || e.Error == nil || e.Error.Code != "version_unsupported" {
			t.Fatalf("upgraded remote must refuse the stale build at 10, got %d %+v", code, e)
		}
		if after := remoteTip(t, remote); after != before {
			t.Fatal("refused draft must push nothing")
		}
	})
}

// installRivalHook arms the remote so each push attempt loses to the
// next prepared rival (a valid chain extension) via ref-lock contention,
// until the rivals run out.
func installRivalHook(t *testing.T, remote string, rivals []string) {
	t.Helper()
	rf := filepath.Join(remote, "rivals")
	if err := os.WriteFile(rf, []byte(strings.Join(rivals, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-receive hooks run in git's quarantine, which forbids ref
	// updates; the rival commits already live in the main object store
	// (pushed, then rewound), so lifting the quarantine env for the
	// update is safe and gives a deterministic mid-push race.
	hook := fmt.Sprintf(`#!/bin/sh
r=$(head -n1 %[1]s 2>/dev/null)
if [ -n "$r" ]; then
  unset GIT_QUARANTINE_PATH
  git update-ref %[2]s "$r"
  tail -n +2 %[1]s > %[1]s.n && mv %[1]s.n %[1]s
fi
exit 0
`, rf, remoteRef)
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
}

// buildRivals prepares n successive valid appends and rewinds the ref,
// returning the commit at each step for the hook to replay.
func buildRivals(t *testing.T, remote string, resolve ledger.Resolver, n int) []string {
	t.Helper()
	base := remoteTip(t, remote)
	var rivals []string
	for i := 0; i < n; i++ {
		libAppend(t, remote, resolve, "seed/0", "message.sent", "c-rival", fmt.Sprintf(`{"n": %d}`, i))
		rivals = append(rivals, remoteTip(t, remote))
	}
	if out, err := exec.Command("git", "--git-dir", remote, "update-ref", remoteRef, base).CombinedOutput(); err != nil {
		t.Fatalf("rewind: %v %s", err, out)
	}
	return rivals
}

func TestRemoteAppendRaceRetriesAndLands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rival lands through a pre-receive hook, which needs a POSIX git server (spec/platform.md)")
	}
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	installRivalHook(t, remote, buildRivals(t, remote, resolve, 1))

	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", filepath.Join(dir, "state"),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 9}`)
	if code != 0 || !e.OK {
		t.Fatalf("raced append must still land: %d %+v", code, e)
	}
	if e.Result["attempts"].(float64) < 2 {
		t.Fatalf("the race was not real: %+v", e.Result)
	}
	if e.Position == nil || *e.Position != "2" {
		t.Fatalf("landed position must sit after the rival, got %+v", e.Position)
	}
}

func TestRemoteAppendExhaustsAtContention(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rival lands through a pre-receive hook, which needs a POSIX git server (spec/platform.md)")
	}
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	installRivalHook(t, remote, buildRivals(t, remote, resolve, remoteMaxAttempts+1))

	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", filepath.Join(dir, "state"),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 9}`)
	if code != 2 || e.Error == nil || e.Error.Code != "contention" {
		t.Fatalf("perpetual rivals must exhaust at 2 contention, got %d %+v", code, e)
	}
}

func TestRemoteAppendHookRejectionAt11(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server; a bare Windows checkout runs the cooperative or forge-hosted posture (spec/platform.md)")
	}
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	seedRemoteGenesis(t, remote)
	hook := "#!/bin/sh\necho 'seed policy says no' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	before := remoteTip(t, remote)
	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", filepath.Join(dir, "state"),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
	if code != 11 || e.Error == nil || e.Error.Code != "remote_rejected" {
		t.Fatalf("hook decline must exit 11 remote_rejected, got %d %+v", code, e)
	}
	if !strings.Contains(e.Error.Message, "seed policy says no") {
		t.Fatalf("the remote's reason must ride the message, got %q", e.Error.Message)
	}
	if after := remoteTip(t, remote); after != before {
		t.Fatal("rejected push must land nothing")
	}
}

func TestRemoteAppendVanishedRefRegression(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	seedRemoteGenesis(t, remote)
	state := filepath.Join(dir, "state")

	if e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
		"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`); code != 0 {
		t.Fatalf("first append failed: %d %+v", code, e)
	}
	if out, err := exec.Command("git", "--git-dir", remote, "update-ref", "-d", remoteRef).CombinedOutput(); err != nil {
		t.Fatalf("delete ref: %v %s", err, out)
	}
	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
		"--key", priv, "--verb", "message.sent", "--subject", "c-0002", "--payload", `{"n": 2}`)
	if code != 12 || e.Error == nil || e.Error.Code != "head_regression" {
		t.Fatalf("vanished ref after a verified head must exit 12 head_regression, got %d %+v", code, e)
	}
}

// conformance: III.A freshness — a rollback landing mid-invocation
// (after the pre-flight verify, before the loop's push wins) refuses at
// exit 12 via the recorded verified head; nothing appends over the
// regression (#91 review).
func TestRemoteAppendMidInvocationRollbackRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rival lands through a pre-receive hook, which needs a POSIX git server (spec/platform.md)")
	}
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	genesisTip := remoteTip(t, remote)
	libAppend(t, remote, resolve, "seed/0", "message.sent", "c-0001", `{"n": 1}`)

	rf := filepath.Join(remote, "rollback")
	if err := os.WriteFile(rf, []byte(genesisTip+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := fmt.Sprintf(`#!/bin/sh
r=$(head -n1 %[1]s 2>/dev/null)
if [ -n "$r" ]; then
  unset GIT_QUARANTINE_PATH
  git update-ref %[2]s "$r"
  : > %[1]s
fi
exit 0
`, rf, remoteRef)
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}

	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", filepath.Join(dir, "state"),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0002", "--payload", `{"n": 2}`)
	if code != 12 || e.Error == nil || e.Error.Code != "head_regression" {
		t.Fatalf("mid-invocation rollback must refuse at exit 12, got %d %+v", code, e)
	}
	if tip := remoteTip(t, remote); tip != genesisTip {
		t.Fatalf("nothing may append over the regression, ref at %s", tip)
	}
}

// Two appenders sharing one state dir (the no --state default shape)
// serialize on the state lock instead of corrupting the shared git
// index, tracking ref, or persisted-head cache (#93 review).
func TestRemoteAppendSharedStateSerializes(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	seedRemoteGenesis(t, remote)
	state := filepath.Join(dir, "shared-state")

	type outcome struct {
		code int
		out  string
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			var out, errOut strings.Builder
			code := run([]string{"ledger", "append", "--remote", remote, "--state", state,
				"--key", priv, "--verb", "message.sent", "--subject", fmt.Sprintf("c-%04d", i),
				"--payload", fmt.Sprintf(`{"n": %d}`, i)}, &out, &errOut)
			results <- outcome{code, out.String()}
		}(i)
	}
	for i := 0; i < 2; i++ {
		r := <-results
		if r.code != 0 {
			t.Fatalf("shared-state appenders must serialize and land, got %d %s", r.code, r.out)
		}
	}
	c, err := gitref.NewClient(t.TempDir(), remote, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	ld := t.TempDir()
	if err := c.Materialize(tip, ld); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 || e.Result["count"].(float64) != 3 {
		t.Fatalf("both appends must land: %d %+v", code, e)
	}
}

// conformance: III.F — the lifecycle happy path admits through the CLI
// (the cooperative client runs the shared rule set), done is reached
// only through merge.observed, and illegal jumps refuse exit 3 naming
// subject, state, and verb (plans/os-d69a6c91.md step 7).
func TestRemoteAppendLifecycle(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "seed/1"}`)
	state := filepath.Join(dir, "state")
	vkey, vpub, vfp := writeWorkerKey(t, 11)

	// Done is reachable only through the full reconciliation chain
	// (plans/os-6cdc15be.md): a disjoint verdict-granted key renders
	// pass, the work lane requests citing it, and the observation
	// records the merged commit. Positions: genesis 0, upgrade 1,
	// enroll 2, grant 3, filed 4, specified 5, taken 6 (the fence),
	// submission 7, verdict 8, request 9, observation 10.
	steps := []struct{ key, verb, subject, payload string }{
		{priv, "actor.enrolled", vfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, vpub)},
		{priv, "actor.granted", vfp, `{"capability": "verdict"}`},
		{priv, "intent.filed", "c-1", `{"intent": "fix", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`},
		{priv, "claim.taken", "c-1", `{}`},
		{priv, "submission.made", "c-1", `{"branch": "seed/c-1", "fence": "6", "packet": {"acceptance": ["c-1 resumes"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}}`},
		{vkey, "verdict.rendered", "c-1", `{"verdict": "pass", "receipt": "` + strings.Repeat("ab", 32) + `", "submission": "7", "independence": "L1"}`},
		{priv, "merge.requested", "c-1", `{"verdict": "8"}`},
		{priv, "merge.observed", "c-1", `{"merged": "` + strings.Repeat("cd", 20) + `", "pr": "9"}`},
	}
	for _, s := range steps {
		e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
			"--key", s.key, "--verb", s.verb, "--subject", s.subject, "--payload", s.payload)
		if code != 0 || !e.OK {
			t.Fatalf("%s must land through the CLI: %d %+v", s.verb, code, e)
		}
	}

	before := remoteTip(t, remote)
	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
		"--key", priv, "--verb", "claim.taken", "--subject", "c-1", "--payload", `{}`)
	if code != 3 || e.Error == nil || e.Error.Code != "invalid_transition" ||
		!strings.Contains(e.Error.Message, "c-1") || !strings.Contains(e.Error.Message, "done") ||
		!strings.Contains(e.Error.Message, "claim.taken") {
		t.Fatalf("an illegal transition must refuse exit 3 naming subject, state, and verb: %d %+v", code, e)
	}
	if after := remoteTip(t, remote); after != before {
		t.Fatal("refused draft must push nothing")
	}

	// Completeness is a shape refusal: an incomplete filing never
	// leaves the client.
	e, code = runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
		"--key", priv, "--verb", "intent.filed", "--subject", "c-2", "--payload", `{"intent": "x"}`)
	if code != 8 || e.Error == nil || !strings.Contains(e.Error.Message, "incomplete") {
		t.Fatalf("an incomplete filing must refuse as a shape refusal: %d %+v", code, e)
	}
	if after := remoteTip(t, remote); after != before {
		t.Fatal("refused draft must push nothing")
	}
}

// conformance: III.F — claims are exclusive with fencing at the CLI:
// contention returns the structured envelope, a fence-free holder
// event refuses 6, and exclusive verbs refuse offline (the cooperative
// client's online-only seam; plans/os-5dc16a7c.md).
func TestRemoteClaimFencingCLI(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "seed/1"}`)
	state := filepath.Join(dir, "state")
	appendCLI := func(verb, subject, payload string) (ledgerEnv, int) {
		return runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
			"--key", priv, "--verb", verb, "--subject", subject, "--payload", payload)
	}
	if _, code := appendCLI("intent.filed", "c-1", `{"intent": "fix", "tier": "trivial", "budget": "small", "routing": "core"}`); code != 0 {
		t.Fatal("filing failed")
	}
	if _, code := appendCLI("contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`); code != 0 {
		t.Fatal("specification failed")
	}
	if _, code := appendCLI("claim.taken", "c-1", `{}`); code != 0 {
		t.Fatal("claim failed")
	}

	// Contention: the same key re-claiming its held contract loses
	// with the structured envelope (one claim at a time, whoever asks).
	e, code := appendCLI("claim.taken", "c-1", `{}`)
	if code != 2 || e.Error == nil || e.Error.Code != "contention" ||
		!strings.Contains(e.Error.Message, "fence 4") {
		t.Fatalf("a rival claim must refuse structured contention at 2, got %d %+v", code, e)
	}

	// The holder's fence-free milestone refuses 6 naming the fence.
	e, code = appendCLI("message.sent", "c-1", `{"n": 1}`)
	if code != 6 || e.Error == nil || e.Error.Code != "fenced_out" ||
		!strings.Contains(e.Error.Message, "4") {
		t.Fatalf("a fence-free holder event must refuse 6, got %d %+v", code, e)
	}
	if _, code := appendCLI("message.sent", "c-1", `{"n": 1, "fence": "4"}`); code != 0 {
		t.Fatal("the holder citing the active fence must admit")
	}

	// A packetless exit is a shape refusal: the packet obligation
	// travels with the four deliberate exits (plans/os-b07b0f59.md).
	e, code = appendCLI("claim.released", "c-1", `{"fence": "4"}`)
	if code != 8 || e.Error == nil || !strings.Contains(e.Error.Message, "packet") {
		t.Fatalf("a packetless exit must refuse as a shape refusal naming the packet, got %d %+v", code, e)
	}

	// Offline: an exclusive verb refuses through the local dev tool
	// with the charter's posture in the message; reading stays local.
	ld := filepath.Join(t.TempDir(), "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "claim.taken", "--subject", "c-9", "--payload", `{}`)
	if code != 2 || e.Error == nil || e.Error.Code != "contention" ||
		!strings.Contains(e.Error.Message, "online-only") {
		t.Fatalf("an offline exclusive verb must refuse with the online-only posture, got %d %+v", code, e)
	}
	if _, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 {
		t.Fatal("offline reading is unaffected")
	}
}
