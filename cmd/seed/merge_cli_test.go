package main

// The merge chain's two verbs (plans/os-6a08b166.md D6.5;
// spec/reconciliation.md). Before this card they had no CLI verb
// at all — only `ledger append`, the raw dev seam that runs no rules —
// so the chain's terminal steps had no admitted surface a lane could
// drive, and a fixture using them asserted nothing.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// mergeStand builds a contract standing at `review` with an admitted
// pass verdict: the state the merge chain starts from.
type mergeCtx struct {
	ld, src, head string
	keys          map[string]string
}

func mergeStand(t *testing.T) *mergeCtx {
	t.Helper()
	ld, src, base, specCommit, head, priv, _, keys, fps := offerLedger(t)
	offerFile(t, ld, priv, specCommit, "c-1")

	// The observer identity: merge.requested and merge.observed are
	// the observer's and operator's work, so the fixture provisions
	// one rather than reusing a claim key.
	oPath, oPub, oFP := writeWorkerKey(t, 41)
	keys["observer"], fps["observer"] = oPath, oFP
	for _, step := range [][]string{
		{"actor.enrolled", oFP, `{"key": "` + oPub + `", "kind": "agent", "name": "observer"}`},
		{"actor.granted", oFP, `{"capability": "observer"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	fence, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := admitAppend(t, ld, workerRawKey(22), "submission.made", "c-1",
		`{"fence": "`+itoa(fence)+`", "packet": {"acceptance": ["c-1"], "decisions": [], "base": "`+
			base+".."+head+`", "refs": [], "findings": []}}`); err != nil {
		t.Fatalf("submission: %v", err)
	}
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1",
		"--repo", src, "--key", keys["verifier"], "--verdict", "pass"); code != 0 {
		t.Fatalf("verdict render: %d %+v", code, e)
	}
	return &mergeCtx{ld: ld, src: src, head: head, keys: keys}
}

func itoa(i int) string { return strings.TrimSpace(string(mustJSONNumber(i))) }

func mustJSONNumber(i int) []byte {
	b, _ := json.Marshal(i)
	return b
}

// conformance: the merge chain runs to `done` on the LOCAL posture
// through admitted verbs, asserted by folding the chain back.
func TestMergeChainReachesDoneLocally(t *testing.T) {
	m := mergeStand(t)
	// The request is the WORK lane's act (keyring: merge.requested
	// accepts claim or operator) and the observation is the observer
	// lane's. The fixture uses each identity rather than an operator
	// key for both, so the drill exercises the split the keyring
	// draws instead of papering over it.
	if e, code := runEnv(t, "merge", "request", "--ledger", m.ld, "--key", m.keys["workerA"],
		"--subject", "c-1"); code != 0 {
		t.Fatalf("merge request: %d %+v", code, e)
	}
	if e, code := runEnv(t, "merge", "observe", "--ledger", m.ld, "--key", m.keys["observer"],
		"--subject", "c-1", "--merged", m.head, "--pr", "pr/1"); code != 0 {
		t.Fatalf("merge observe: %d %+v", code, e)
	}
	st, failEnv := loadVerdictState(m.ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify: %+v", failEnv)
	}
	s, _ := st.fold.State("c-1")
	if s.State != "done" {
		t.Fatalf("the merge chain ends at done, got %q", s.State)
	}
	if s.Requested == nil || s.Verdict == nil || s.Requested.CitedVerdict != s.Verdict.Pos {
		t.Errorf("the request must cite the pass verdict it merges: %+v", s.Requested)
	}
	if s.Merged == nil || s.Merged.SHA != m.head {
		t.Errorf("the observation records the forge fact verbatim: %+v", s.Merged)
	}
}

// conformance: acceptance criterion 3b — the request's DERIVED citation
// is re-examined against the refreshed view and REFUSED rather than
// silently re-pointed.
//
// The assertion drives `recheckDerivation` directly, which is the
// guard `pushDraft` runs on every attempt of the optimistic loop.
// Reproducing a real mid-flight tip move would be a race dressed as a
// test; what matters is the guard's DECISION, and that is what this
// checks — a drafted citation that no longer matches the refreshed
// derivation must refuse, not be replaced.
func TestMergeRequestRefusesAMovedCitation(t *testing.T) {
	m := mergeStand(t)
	store, failEnv := openStore(m.ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	ctx, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	live, env := verdictCitation(ctx, "c-1")
	if env != nil {
		t.Fatalf("the stand must carry a verdict to cite: %+v", env)
	}

	// A citation drafted against a DIFFERENT view: the same shape,
	// naming a position the refreshed view does not agree with.
	stale := []byte(`{"verdict": "0"}`)
	if string(stale) == string(live) {
		t.Fatal("the fixture must make the stale citation differ, or the drill asserts nothing")
	}
	act := loopAct{verb: transition.MergeRequestedVerb, payload: stale,
		derive: func(c *admit.Context) ([]byte, *envelope.Envelope) { return verdictCitation(c, "c-1") }}
	recheck := recheckDerivation(act, "c-1")
	if recheck == nil {
		t.Fatal("an act with a derive must produce a recheck, or nothing guards the citation")
	}
	rerr := recheck(ctx)
	if rerr == nil {
		t.Fatal("a citation the refreshed view disagrees with must REFUSE, never be re-pointed")
	}
	var div *derivedDivergence
	if !errors.As(rerr, &div) {
		t.Fatalf("the refusal must be the derived-divergence one: %v", rerr)
	}
	if !strings.Contains(rerr.Error(), "different value is a different decision") {
		t.Errorf("the refusal must say why it does not substitute: %v", rerr)
	}

	// And the control: the LIVE citation passes the same guard, so the
	// refusal above is about the moved value rather than the guard
	// refusing everything.
	ok := loopAct{verb: transition.MergeRequestedVerb, payload: live,
		derive: func(c *admit.Context) ([]byte, *envelope.Envelope) { return verdictCitation(c, "c-1") }}
	if err := recheckDerivation(ok, "c-1")(ctx); err != nil {
		t.Fatalf("an unmoved citation must pass the guard: %v", err)
	}
}

// merge observe carries NO derived citation, and the plan predicted
// otherwise — it named "the observation's cited request" as a third
// derived value. The rule cites no request at all: `{merged, pr}` are
// the caller's observations of the forge, and the request it follows
// is checked against the fold rather than named in the payload. Pinned
// so the correction cannot quietly regress into an invented citation.
func TestMergeObserveCarriesNoDerivedCitation(t *testing.T) {
	m := mergeStand(t)
	// The request is the WORK lane's act (keyring: merge.requested
	// accepts claim or operator) and the observation is the observer
	// lane's. The fixture uses each identity rather than an operator
	// key for both, so the drill exercises the split the keyring
	// draws instead of papering over it.
	if e, code := runEnv(t, "merge", "request", "--ledger", m.ld, "--key", m.keys["workerA"],
		"--subject", "c-1"); code != 0 {
		t.Fatalf("merge request: %d %+v", code, e)
	}
	if e, code := runEnv(t, "merge", "observe", "--ledger", m.ld, "--key", m.keys["observer"],
		"--subject", "c-1", "--merged", m.head, "--pr", "pr/1"); code != 0 {
		t.Fatalf("merge observe: %d %+v", code, e)
	}
	st, failEnv := loadVerdictState(m.ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	for _, rec := range st.records {
		if rec.Event.Verb != transition.MergeObservedVerb {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if len(p) != 2 || p["merged"] == nil || p["pr"] == nil {
			t.Errorf("the observation's payload is exactly {merged, pr}: %v", p)
		}
	}
}

// Usage: both verbs demand exactly one posture, like every other act.
func TestMergeUsageDemandsOnePosture(t *testing.T) {
	for _, args := range [][]string{
		{"merge"},
		{"merge", "elsewhere"},
		{"merge", "request", "--key", "k", "--subject", "c-1"},
		{"merge", "request", "--ledger", "d", "--remote", "r", "--key", "k", "--subject", "c-1"},
		{"merge", "observe", "--ledger", "d", "--key", "k", "--subject", "c-1"},
	} {
		e, code := runEnv(t, args...)
		if code != 64 || e.Error == nil || e.Error.Code != "usage" {
			t.Errorf("%v must refuse as usage, got %d %+v", args, code, e.Error)
		}
	}
}
