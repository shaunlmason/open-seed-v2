package main

// The ranking drills at the terminal (plans/os-c7554f18.md D2, D3;
// spec/ranking.md): offer publish --strongest fills the tuples
// scope from the ranking and refuses on an empty one rather than
// widening, the scope it writes is the one --tuple writes by hand
// (workers see it by the same rule), and the doctor names the top
// tuple per capability at a ledger's tip.

import (
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: D2, AC2 — --strongest carries exactly the top n; an
// empty ranking refuses ranking_empty (exit 4); --strongest is not
// combined with --tuple and reads exactly one --capability.
func TestOfferStrongestFillsTheScopeByPolicy(t *testing.T) {
	ld, _, _, specCommit, priv, _, keys, fps, _ := qualifiedLedger(t)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	offerFile(t, ld, priv, specCommit, "c-1")
	publish := func(extra ...string) (ledgerEnv, int) {
		t.Helper()
		args := append([]string{"offer", "publish", "--ledger", ld, "--subject", "c-1",
			"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z"}, extra...)
		return runEnv(t, args...)
	}

	// Nothing qualified yet: policy has nothing to say, and the offer
	// refuses rather than going out unscoped.
	if e, code := publish("--strongest", "1", "--capability", "claim"); code != 4 || e.Error == nil || e.Error.Code != "ranking_empty" {
		t.Fatalf("an empty ranking refuses ranking_empty: %d %+v", code, e)
	}
	// The flag's own shape: one capability, never beside --tuple.
	if e, code := publish("--strongest", "1"); code != 64 || !strings.Contains(e.Error.Message, "exactly one --capability") {
		t.Fatalf("--strongest without a capability is usage: %d %+v", code, e)
	}
	if e, code := publish("--strongest", "1", "--capability", "claim", "--tuple", drillTuple(nil)); code != 64 || !strings.Contains(e.Error.Message, "not combined with --tuple") {
		t.Fatalf("--strongest beside --tuple is usage: %d %+v", code, e)
	}
	if _, code := publish("--strongest", "1", "--capability", "claim", "--capability", "verdict"); code != 64 {
		t.Fatalf("--strongest with two capabilities is usage: %d", code)
	}

	// workerA's configuration qualified: the offer carries it, and the
	// scope reads like a hand-written one: workerA sees the offer,
	// workerB (no cited tuple) does not.
	rootAppend(t, ld, priv, "actor.qualified", fps["workerA"], `{"capability": "claim", "tuple": `+drillTuple(nil)+`, "contract": "e-1", "verdict": "3"}`)
	if e, code := publish("--strongest", "2", "--capability", "claim"); code != 0 {
		t.Fatalf("publish --strongest: %d %+v", code, e)
	}
	rows := listOffers(t, ld, fps["workerA"], "")
	if len(rows) != 1 {
		t.Fatalf("workerA sees the policy-scoped offer: %+v", rows)
	}
	tuples, _ := rows[0].(map[string]any)["tuples"].([]any)
	if len(tuples) != 1 {
		t.Fatalf("one tuple ranks, so the offer carries exactly one (n past the end takes what ranks): %+v", rows[0])
	}
	if got := tuples[0].(map[string]any)["environment"]; got != "detached-git-worktree" {
		t.Fatalf("the scope is the ranked tuple: %+v", tuples[0])
	}
	if got := listOffers(t, ld, fps["workerB"], ""); len(got) != 0 {
		t.Fatalf("a worker citing no tuple does not see a policy-scoped offer: %+v", got)
	}
	// The scope is matched against claim grants, so the verdict
	// ranking can never fill it: usage, not a lookup.
	if e, code := publish("--strongest", "1", "--capability", "verdict"); code != 64 || !strings.Contains(e.Error.Message, "claim ranking") {
		t.Fatalf("--strongest reads the claim ranking alone: %d %+v", code, e)
	}
}

// conformance: D3, AC5 — the doctor names the top tuple per
// capability at the ledger's tip, null where nothing ranks, and has
// no ranking section without a ledger.
func TestDoctorNamesTheStrongestPerCapability(t *testing.T) {
	cfg := writePosture(t, `{"posture": "cooperative"}`)
	e, code, _ := runDoctorEnv(t, "--config", cfg)
	if code != 0 {
		t.Fatalf("doctor: %d %+v", code, e)
	}
	if _, ok := e.Result["ranking"]; ok {
		t.Fatal("without a ledger the doctor reads the declaration alone")
	}
	ld, _, _, _, priv, _, _, fps, _ := qualifiedLedger(t)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	e, code, _ = runDoctorEnv(t, "--config", cfg, "--ledger", ld)
	if code != 0 {
		t.Fatalf("doctor --ledger: %d %+v", code, e)
	}
	section, _ := e.Result["ranking"].(map[string]any)
	strongest, _ := section["strongest"].(map[string]any)
	if section["as_of"] != "" || strongest["claim"] != nil || strongest["verdict"] != nil {
		t.Fatalf("nothing ranks yet: both null, and no instant until a qualification exists: %+v", section)
	}
	rootAppend(t, ld, priv, "actor.qualified", fps["workerA"], `{"capability": "claim", "tuple": `+drillTuple(nil)+`, "contract": "e-1", "verdict": "3"}`)
	e, _, _ = runDoctorEnv(t, "--config", cfg, "--ledger", ld)
	section, _ = e.Result["ranking"].(map[string]any)
	strongest, _ = section["strongest"].(map[string]any)
	top, _ := strongest["claim"].(map[string]any)
	if top["environment"] != "detached-git-worktree" || strongest["verdict"] != nil || section["as_of"] == "" {
		t.Fatalf("the doctor names the top claim tuple and null for verdict, at the qualification's own instant: %+v", section)
	}
}
