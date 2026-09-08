package main

// The forge-hosted posture's drills (plans/os-5c8a312c.md D8): the
// service admits, refuses with the boundary's own code, reports a race
// as a race, and holds no state a fresh clone does not rebuild; the
// fixture models the forge's ruleset with a pre-receive that lets the
// admission identity alone write the ledger branch.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/propose"
	"github.com/shaunlmason/open-seed-v2/internal/refusal"
)

const admissionIdentity = "seed-admission[bot]"

// forgeRemote is a bare repository with NO admission hook — the
// forge-hosted posture has none — whose pre-receive models the forge's
// ruleset: the ledger branch is writable by the admission identity
// alone, asserted here through SEED_PUSHER because a bare repository
// on a local path has no other notion of who is pushing. In production
// the forge authenticates the service's credential; this is the model
// of that rule, labeled as such (plans/os-5c8a312c.md D8(a)).
func forgeRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "forge.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("bare init: %v %s", err, out)
	}
	hardenGitRepo(t, dir)
	ruleset := "#!/bin/sh\n" +
		"# fixture: the forge's ruleset — " + posture.DefaultLedgerRef + " is writable by " + admissionIdentity + " alone\n" +
		"while read old new ref; do\n" +
		"  if [ \"$ref\" = \"" + posture.DefaultLedgerRef + "\" ] && [ \"$SEED_PUSHER\" != \"" + admissionIdentity + "\" ]; then\n" +
		"    echo \"ruleset: " + posture.DefaultLedgerRef + " is writable by " + admissionIdentity + " alone\" >&2; exit 1\n" +
		"  fi\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "hooks", "pre-receive"), []byte(ruleset), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// asAdmission runs fn with the service's identity asserted to the
// fixture ruleset, and restores the actor's (absent) identity after.
func asAdmission(t *testing.T, fn func()) {
	t.Helper()
	prev, had := os.LookupEnv("SEED_PUSHER")
	os.Setenv("SEED_PUSHER", admissionIdentity)
	defer func() {
		if had {
			os.Setenv("SEED_PUSHER", prev)
		} else {
			os.Unsetenv("SEED_PUSHER")
		}
	}()
	fn()
}

type forgeDeployment struct {
	remote string
	svc    *service
	srv    *httptest.Server
	client *propose.Client
}

// newForgeDeployment starts one service instance over a fresh forge
// remote and seeds genesis through it: an empty ledger admits a genesis
// proposal only, and the operator has no push right to the branch.
func newForgeDeployment(t *testing.T) (*forgeDeployment, ledger.Resolver) {
	t.Helper()
	d := startService(t, forgeRemote(t))
	priv := fixtureKey(t)
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
	var res *gitref.Result
	asAdmission(t, func() { res, err = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{rec}) })
	if err != nil || res.Position != 0 {
		t.Fatalf("genesis through the service: %+v %v", res, err)
	}
	return d, resolve
}

func startService(t *testing.T, remote string) *forgeDeployment {
	t.Helper()
	svc := &service{remote: remote, ref: posture.DefaultLedgerRef, stateDir: t.TempDir()}
	srv := httptest.NewServer(svc.handler())
	t.Cleanup(srv.Close)
	return &forgeDeployment{remote: remote, svc: svc, srv: srv, client: propose.New(srv.URL)}
}

func forgeTip(t *testing.T, remote string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "--quiet", "--verify", posture.DefaultLedgerRef).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// proposeRaw posts a body verbatim and returns the status and envelope.
func proposeRaw(t *testing.T, d *forgeDeployment, body string) (int, envelope.Envelope) {
	t.Helper()
	resp, err := http.Post(d.srv.URL+"/propose", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env envelope.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("no envelope: %v", err)
	}
	return resp.StatusCode, env
}

// materializedTip is the remote ledger as a store, for building records
// linked to the current tip.
func materializedTip(t *testing.T, remote string) (*ledger.Store, string) {
	t.Helper()
	c, err := gitref.NewClient(t.TempDir(), remote, posture.DefaultLedgerRef)
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
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store, tip
}

// conformance: III.B — under the forge-hosted posture the validator is
// the ledger ref's sole writer: the service admits a valid proposal and
// advances the branch, refuses a hostile one with the boundary's own
// code and the branch unmoved, and an actor's direct push is refused by
// the forge's rule with the branch unmoved (the attempted-direct-push
// row, this posture).
func TestServiceAdmitsRefusesAndTheForgeRefusesTheActor(t *testing.T) {
	d, resolve := newForgeDeployment(t)
	store, _ := materializedTip(t, d.remote)

	// The health probe reports the genesis the service just admitted.
	resp, err := http.Get(d.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var h propose.Health
	_ = json.NewDecoder(resp.Body).Decode(&h)
	resp.Body.Close()
	if h.Ref != posture.DefaultLedgerRef || h.Position == nil || *h.Position != 0 || h.Tip != forgeTip(t, d.remote) {
		t.Fatalf("the probe must report the last admitted position and tip, got %+v", h)
	}

	before := forgeTip(t, d.remote)
	valid := signed(t, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store))
	var res *gitref.Result
	asAdmission(t, func() { res, err = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{valid}) })
	if err != nil || res.Position != 1 || res.Commit == "" || res.Commit != forgeTip(t, d.remote) || forgeTip(t, d.remote) == before {
		t.Fatalf("a valid proposal must be admitted and the branch advanced by the service: %+v %v", res, err)
	}

	after := forgeTip(t, d.remote)
	store, _ = materializedTip(t, d.remote)
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
	bad := signed(t, "message.sent", "c-0002", hostile, tipOf(t, store))
	asAdmission(t, func() { _, err = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{bad}) })
	ref, ok := propose.IsRefusal(err)
	if !ok || ref.Exit != envelope.ExitClassificationRef || ref.Code != "classification_refused" || ref.Position == nil || *ref.Position != "1" {
		t.Fatalf("a hostile proposal must refuse with the boundary's own code stamped at the tip, got %v", err)
	}
	if forgeTip(t, d.remote) != after {
		t.Fatal("a refused proposal must leave the branch unmoved")
	}

	// The actor's credential cannot write the branch directly: the
	// fixture ruleset refuses, as the forge's would.
	c, err := gitref.NewClient(t.TempDir(), d.remote, posture.DefaultLedgerRef)
	if err != nil {
		t.Fatal(err)
	}
	priv := fixtureKey(t)
	fp, _ := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	_, err = c.AppendLoop(gitref.Draft{
		V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
		Verb: "message.sent", Subject: "c-0003", Payload: json.RawMessage(`{"n": 3}`),
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, admit.Validate(), 3)
	if !errors.Is(err, gitref.ErrRemoteRejected) || !strings.Contains(err.Error(), "writable by "+admissionIdentity+" alone") || forgeTip(t, d.remote) != after {
		t.Fatalf("an actor's direct push must be refused by the forge's rule with the branch unmoved, got %v", err)
	}

	// The same client, proposing instead of pushing, lands — the
	// posture's whole point: the credential proposes, the service
	// writes. (In this one process the service's push carries the
	// admission identity only while asAdmission holds; in production
	// the service's own credential does.)
	asAdmission(t, func() {
		res, err = c.WithProposer(d.client).AppendLoop(gitref.Draft{
			V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
			Verb: "message.sent", Subject: "c-0003", Payload: json.RawMessage(`{"n": 3}`),
		}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, admit.Validate(), 3)
	})
	if err != nil || res.Position != 2 || res.Attempts != 1 {
		t.Fatalf("the proposing client must land through the service: %+v %v", res, err)
	}
	asAdmission(t, func() {
		_, err = c.AppendLoop(gitref.Draft{
			V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
			Verb: "message.sent", Subject: "c-0004", Payload: json.RawMessage(hostile),
		}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, nil, 3)
	})
	if ref, ok := propose.IsRefusal(err); !ok || ref.Code != "classification_refused" {
		t.Fatalf("a client that skips self-validation still meets the boundary at the service, got %v", err)
	}
}

// conformance: III.B — a proposal linked to a tip that is no longer the
// tip is a race, reported as one (409) and never as chain trouble; the
// proposer's own loop re-links and lands second.
func TestServiceReportsTheRaceAndTheLoopRetries(t *testing.T) {
	d, resolve := newForgeDeployment(t)
	store, _ := materializedTip(t, d.remote)
	stale := tipOf(t, store)

	// A rival lands first.
	asAdmission(t, func() {
		if _, err := d.client.Propose(posture.DefaultLedgerRef, []*event.Record{signed(t, "message.sent", "c-0001", `{"n": 1}`, stale)}); err != nil {
			t.Fatalf("rival: %v", err)
		}
	})
	var err error
	asAdmission(t, func() {
		_, err = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{signed(t, "message.sent", "c-0002", `{"n": 2}`, stale)})
	})
	if !errors.Is(err, gitref.ErrNonFastForward) || !strings.Contains(err.Error(), "re-link and propose again") {
		t.Fatalf("a stale proposal must come back as the race it is, got %v", err)
	}
	if _, ok := propose.IsRefusal(err); ok {
		t.Fatal("a race is not a refusal")
	}

	// The loop over the proposer: the rival lands between this
	// client's fetch and its proposal, so the first attempt races and
	// the second lands.
	priv := fixtureKey(t)
	fp, _ := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	c, err := gitref.NewClient(t.TempDir(), d.remote, posture.DefaultLedgerRef)
	if err != nil {
		t.Fatal(err)
	}
	raced := false
	var res *gitref.Result
	asAdmission(t, func() {
		res, err = c.WithProposer(d.client).AppendLoop(gitref.Draft{
			V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
			Verb: "message.sent", Subject: "c-0003", Payload: json.RawMessage(`{"n": 3}`),
		}, func(e event.Event) (*event.Record, error) {
			if !raced {
				raced = true
				st, _ := materializedTip(t, d.remote)
				if _, rerr := d.client.Propose(posture.DefaultLedgerRef, []*event.Record{signed(t, "message.sent", "c-0009", `{"n": 9}`, tipOf(t, st))}); rerr != nil {
					t.Fatalf("mid-flight rival: %v", rerr)
				}
			}
			return event.Sign(e, priv)
		}, resolve, admit.Validate(), 3)
	})
	if err != nil || res.Attempts != 2 || res.Position != 3 {
		t.Fatalf("the loop must retry the race through the proposer and land second: %+v %v", res, err)
	}
}

// The proposal endpoint is strict: a wrong ref, an empty proposal, a
// malformed record and an unchained pair each refuse before anything is
// judged, and every answer is an envelope.
func TestServiceProposalShape(t *testing.T) {
	d, _ := newForgeDeployment(t)
	store, _ := materializedTip(t, d.remote)
	valid := signed(t, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store))
	line, _ := json.Marshal(valid)
	for name, tc := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"wrong ref":      {`{"ref": "refs/seed/ledger", "records": [` + string(line) + `]}`, propose.StatusRefused, "not_found"},
		"empty":          {`{"ref": "` + posture.DefaultLedgerRef + `", "records": []}`, propose.StatusRefused, "chain_invalid"},
		"unknown field":  {`{"ref": "` + posture.DefaultLedgerRef + `", "records": [` + string(line) + `], "token": "x"}`, propose.StatusRefused, "chain_invalid"},
		"not a record":   {`{"ref": "` + posture.DefaultLedgerRef + `", "records": [{"event": 1}]}`, propose.StatusRefused, "chain_invalid"},
		"unchained pair": {`{"ref": "` + posture.DefaultLedgerRef + `", "records": [` + string(line) + `, ` + string(line) + `]}`, propose.StatusRefused, "chain_invalid"},
	} {
		status, env := proposeRaw(t, d, tc.body)
		if status != tc.status || env.OK || env.Error == nil || env.Error.Code != tc.code {
			t.Errorf("%s: want %d %s, got %d %+v", name, tc.status, tc.code, status, env)
		}
	}
	if forgeTip(t, d.remote) == "" {
		t.Fatal("genesis must still stand")
	}
	resp, err := http.Get(d.srv.URL + "/propose")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /propose is not a proposal, got %d", resp.StatusCode)
	}
}

// adversaryRows is #99's shared adversary table: the same hostile
// content under every posture (plans/os-5c8a312c.md D8(a), (c)).
func adversaryRows(t *testing.T, store *ledger.Store) map[string][]*event.Record {
	t.Helper()
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
	tip := tipOf(t, store)
	halted := signed(t, "system.halt.declared", "system", `{"reason": "drill"}`, tip)
	haltedHash, _ := halted.Event.Hash()
	return map[string][]*event.Record{
		"classification": {signed(t, "message.sent", "c-0009", hostile, tip)},
		"halted":         {halted, signed(t, "message.sent", "c-0009", `{"n": 9}`, haltedHash)},
		"verify":         {signedV(t, "seed/9", "message.sent", "c-0009", `{"n": 9}`, tip)},
	}
}

// conformance: III.B — one derivation, three callers: for every row of
// the shared adversary table the service's envelope code equals the
// code the in-process boundary (admit.Check, mapped by
// internal/refusal) produces, and the hook posture refuses the same row
// naming the same rule.
func TestServiceAgreesWithTheBoundaryAndTheHook(t *testing.T) {
	d, _ := newForgeDeployment(t)
	store, _ := materializedTip(t, d.remote)
	rows := adversaryRows(t, store)

	// The in-process boundary over the same store.
	inProcess := map[string]string{}
	for name, recs := range rows {
		dir := t.TempDir()
		work, err := ledger.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		resolve, _, err := genesis.Bootstrap(store)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Records(func(pos int, r *event.Record) error {
			_, err := work.Append(r, resolve)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		var code string
		for _, rec := range recs {
			ctx, err := admit.ContextAt(work)
			if err != nil {
				t.Fatal(err)
			}
			if err := admit.Check(ctx, rec); err != nil {
				code = refusal.Envelope(err).Error.Code
				break
			}
			if _, err := work.Append(rec, resolve); err != nil {
				code = refusal.Envelope(err).Error.Code
				break
			}
		}
		if code == "" {
			t.Fatalf("%s: the boundary must refuse this row", name)
		}
		inProcess[name] = code
	}

	// The service over the same rows.
	before := forgeTip(t, d.remote)
	for name, recs := range rows {
		var err error
		asAdmission(t, func() { _, err = d.client.Propose(posture.DefaultLedgerRef, recs) })
		ref, ok := propose.IsRefusal(err)
		if !ok {
			t.Fatalf("%s: the service must refuse, got %v", name, err)
		}
		if ref.Code != inProcess[name] {
			t.Errorf("%s: the service said %s, the boundary said %s — two derivations", name, ref.Code, inProcess[name])
		}
		if forgeTip(t, d.remote) != before {
			t.Fatalf("%s: a refused proposal must leave the branch unmoved", name)
		}
	}

	// The hook posture over the same rows, naming the same rule.
	hooked := newDeployment(t, posture.EnforcedSelfHosted)
	resolve := seedGenesis(t, hooked.remote)
	ruleFor := map[string]string{"classification_refused": "rule classification", "halted": "rule halt", "version_mismatch": "rule verify", "chain_invalid": "rule verify"}
	for name, recs := range rows {
		err := craftPush(t, hooked.remote, resolve, func(dir string, st *ledger.Store) {
			// Re-link the row onto the hooked deployment's own tip.
			prev := tipOf(t, st)
			for _, rec := range recs {
				re := signedV(t, rec.Event.V, rec.Event.Verb, rec.Event.Subject, string(rec.Event.Payload), prev)
				appendRaw(t, st, resolve, re)
				prev, _ = re.Event.Hash()
			}
		})
		if !errors.Is(err, gitref.ErrRemoteRejected) {
			t.Fatalf("%s: the hook must refuse, got %v", name, err)
		}
		if want := ruleFor[inProcess[name]]; !strings.Contains(ruleLine(err.Error()), want) {
			t.Errorf("%s: the hook named %q, the boundary's code %s implies %q", name, ruleLine(err.Error()), inProcess[name], want)
		}
	}
}

// conformance: III.B statelessness — kill the service, start another
// from a fresh state dir over the same remote, and every decision comes
// out the same: the same refusals, an admitted valid proposal, and a
// chain that verifies from genesis.
func TestServiceKillAndReplace(t *testing.T) {
	d, resolve := newForgeDeployment(t)
	store, _ := materializedTip(t, d.remote)
	valid := signed(t, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store))
	asAdmission(t, func() {
		if _, err := d.client.Propose(posture.DefaultLedgerRef, []*event.Record{valid}); err != nil {
			t.Fatal(err)
		}
	})
	decisions := func(dep *forgeDeployment) map[string]string {
		st, _ := materializedTip(t, dep.remote)
		got := map[string]string{}
		for name, recs := range adversaryRows(t, st) {
			var err error
			asAdmission(t, func() { _, err = dep.client.Propose(posture.DefaultLedgerRef, recs) })
			ref, ok := propose.IsRefusal(err)
			if !ok {
				t.Fatalf("%s must refuse, got %v", name, err)
			}
			got[name] = ref.Code + " " + ref.Message
		}
		return got
	}
	original := decisions(d)
	d.srv.Close() // the host is gone; only the remote's replicated data crosses over

	replacement := startService(t, d.remote)
	if got := decisions(replacement); len(got) != len(original) {
		t.Fatal("the replacement must judge every row")
	} else {
		for name, want := range original {
			if got[name] != want {
				t.Fatalf("%s decision drifted across kill-and-replace: %q vs %q", name, want, got[name])
			}
		}
	}
	st, _ := materializedTip(t, d.remote)
	asAdmission(t, func() {
		res, err := replacement.client.Propose(posture.DefaultLedgerRef, []*event.Record{signed(t, "message.sent", "c-0002", `{"n": 2}`, tipOf(t, st))})
		if err != nil || res.Position != 2 {
			t.Fatalf("the replacement must admit a valid proposal: %+v %v", res, err)
		}
	})
	st, _ = materializedTip(t, d.remote)
	rep, err := st.VerifyFromGenesis(resolve)
	if err != nil || rep.Count != 3 {
		t.Fatalf("the chain must verify from genesis after the replacement: %+v %v", rep, err)
	}
}

// The serve entry point itself: it announces its endpoint, answers the
// probe, and refuses to start without a remote.
func TestServeEntryPoint(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runServe(nil, &out, &errOut); code != 64 || !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("serve without a remote is a usage error, got %d %q", code, errOut.String())
	}
	remote := forgeRemote(t)
	announce := filepath.Join(t.TempDir(), "endpoint")
	cmd := exec.Command(hookBin, "serve", "--remote", remote, "--announce", announce, "--state", t.TempDir())
	cmd.Env = append(os.Environ(), "SEED_PUSHER="+admissionIdentity)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var endpoint string
	for i := 0; i < 100 && endpoint == ""; i++ {
		if b, err := os.ReadFile(announce); err == nil {
			endpoint = strings.TrimSpace(string(b))
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !strings.HasPrefix(endpoint, "http://") {
		t.Fatalf("the service must announce its endpoint, got %q", endpoint)
	}
	h, err := propose.New(endpoint).Probe()
	if err != nil || h.Ref != posture.DefaultLedgerRef || h.Remote != remote || h.Position != nil {
		t.Fatalf("the probe must report the service before any proposal, got %+v %v", h, err)
	}
	priv := fixtureKey(t)
	rec, err := genesis.Build(priv, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	res, err := propose.New(endpoint).Propose(posture.DefaultLedgerRef, []*event.Record{rec})
	if err != nil || res.Position != 0 || res.Commit != forgeTip(t, remote) {
		t.Fatalf("the subprocess service must admit genesis: %+v %v", res, err)
	}
}
