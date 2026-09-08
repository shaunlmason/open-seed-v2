package transition_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	firstSpec  = `{"acceptance": {"ref": "specs/first.md @ abc1234", "executable": false}}`
	secondSpec = `{"acceptance": {"ref": "specs/second.md @ def5678", "executable": false}}`
	digestA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// conformance: plans/os-6bd9ffff.md D4, AC4 — the table carries the
// ready origin for contract.specified and no other new origin: a
// claimed, reviewed, blocked or finished contract is out of reach.
func TestTableCarriesTheReadyOriginOnly(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"backlog", "ready"} {
		if !tab.Allows(from, "contract.specified") {
			t.Fatalf("contract.specified must leave %s", from)
		}
	}
	for _, from := range []string{"in_progress", "review", "blocked", "done", "cancelled"} {
		if tab.Allows(from, "contract.specified") {
			t.Fatalf("contract.specified must not leave %s: re-specification reaches unclaimed contracts only", from)
		}
	}
	to, err := tab.Check("c-1", "ready", "contract.specified")
	if err != nil || to != "ready" {
		t.Fatalf("a re-specification keeps the subject ready, got %q %v", to, err)
	}
}

func specAt(v, verb, subject, payload string) *event.Record {
	return &event.Record{Event: event.Event{V: v, TS: "2026-09-01T00:00:00Z", Actor: "aa", Verb: verb, Subject: subject, Payload: []byte(payload)}}
}

// conformance: D4, AC4 — the fold applies a re-specification at seed/4
// positions only: Specifications counts both and the acceptance is
// the second's; at seed/3 the ready-origin specification is a visible
// anomaly, the count stays one and the first acceptance stands.
func TestFoldAppliesRespecificationAtSeed4Only(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		v          string
		wantCount  int
		wantRef    string
		wantAnoms  int
		wantReason bool
	}{
		{version.Seed4, 2, "specs/second.md @ def5678", 0, false},
		{version.Seed3, 1, "specs/first.md @ abc1234", 1, true},
	} {
		records := []*event.Record{
			specAt(tc.v, "intent.filed", "c-1", `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`),
			specAt(tc.v, "contract.specified", "c-1", firstSpec),
			specAt(tc.v, "contract.specified", "c-1", secondSpec),
		}
		s, ok := tab.FoldRecords(records).State("c-1")
		if !ok || s.State != "ready" {
			t.Fatalf("%s: the subject stays ready, got %+v", tc.v, s)
		}
		if s.Specifications != tc.wantCount || s.Anomalies != tc.wantAnoms {
			t.Fatalf("%s: Specifications %d anomalies %d, want %d and %d", tc.v, s.Specifications, s.Anomalies, tc.wantCount, tc.wantAnoms)
		}
		if s.Acceptance == nil || s.Acceptance.Ref != tc.wantRef {
			t.Fatalf("%s: acceptance %+v, want ref %s", tc.v, s.Acceptance, tc.wantRef)
		}
	}
	// The reason a validator of the earlier version gives names the
	// version that activates the row.
	reason := transition.RespecificationNeeds(version.Seed3)
	if !strings.Contains(reason, version.Seed4) || !strings.Contains(reason, version.Seed3) {
		t.Fatalf("the refusal names both versions: %s", reason)
	}
	e := &transition.InvalidTransitionError{Subject: "c-1", From: "ready", Verb: "contract.specified", Reason: reason}
	if !strings.Contains(e.Error(), "in state ready: re-specification activates at seed/4") {
		t.Fatalf("the transition refusal carries its reason: %s", e.Error())
	}
}

// conformance: D5, AC5 — the fold keeps the FIRST proposal's digest
// across a second proposal and the approval's, so unedited is judged
// against the planner's original decomposition; an approval before
// seed/4 is unmeasured, never guessed, and a digest raw-pushed at a
// seed/3 position is not read.
func TestFoldKeepsTheFirstProposalDigest(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	fold := tab.FoldRecords([]*event.Record{
		specAt(version.Seed4, "plan.proposed", "c-1", `{"plan": "plans/c-1.md @ abc1234", "digest": "`+digestA+`"}`),
		specAt(version.Seed4, "plan.proposed", "c-1", `{"plan": "plans/c-1.md @ def5678", "digest": "`+digestB+`"}`),
		specAt(version.Seed4, "plan.approved", "c-1", `{"plan": "plans/c-1.md @ def5678", "pr": "pr/1 @ def5678", "digest": "`+digestA+`"}`),
		specAt(version.Seed4, "plan.proposed", "c-2", `{"plan": "plans/c-2.md @ abc1234", "digest": "`+digestA+`"}`),
		specAt(version.Seed4, "plan.approved", "c-2", `{"plan": "plans/c-2.md @ abc1234", "pr": "pr/2 @ abc1234", "digest": "`+digestB+`"}`),
		specAt(version.Seed3, "plan.proposed", "c-3", `{"plan": "plans/c-3.md @ abc1234", "digest": "`+digestA+`"}`),
		specAt(version.Seed3, "plan.approved", "c-3", `{"plan": "plans/c-3.md @ abc1234", "pr": "pr/3 @ abc1234", "digest": "`+digestA+`"}`),
		specAt(version.Seed4, "plan.approved", "c-4", `{"plan": "plans/c-4.md @ abc1234", "pr": "pr/4 @ abc1234", "digest": "`+digestA+`"}`),
	})
	d := fold.PlanDigests("c-1")
	if d.Proposed != digestA || d.Approved != digestA {
		t.Fatalf("c-1 keeps the first proposal's digest and the approval's: %+v", d)
	}
	if unedited, measured := d.Unedited(); !unedited || !measured {
		t.Fatalf("c-1's approval kept the first proposal: unedited, measured; got %v %v", unedited, measured)
	}
	if unedited, measured := fold.PlanDigests("c-2").Unedited(); unedited || !measured {
		t.Fatalf("c-2's approval differs from the proposal: edited, measured; got %v %v", unedited, measured)
	}
	if d := fold.PlanDigests("c-3"); d.Proposed != "" || d.Approved != "" {
		t.Fatalf("a seed/3 digest is not a fact: %+v", d)
	}
	if _, measured := fold.PlanDigests("c-3").Unedited(); measured {
		t.Fatal("an approval before seed/4 is unmeasured")
	}
	if _, measured := fold.PlanDigests("c-4").Unedited(); measured {
		t.Fatal("an approval without a proposal digest is unmeasured")
	}
	if _, ok := fold.PlanApproved("c-3"); !ok {
		t.Fatal("the anchor is still the approved plan: the digest gate does not unapprove")
	}
}

// conformance: D5, AC5 — the shape rule: at seed/4 a proposal or
// approval without a well-formed digest is incomplete naming it;
// before seed/4 one carrying a digest refuses naming the version, and
// one without admits as before.
func TestPlanShapeCarriesTheDigestAtSeed4(t *testing.T) {
	var inc *transition.IncompleteError
	var chain *transition.ChainError
	for _, tc := range []struct {
		v, verb, payload string
		wantMissing      string
		wantVersion      bool
	}{
		{version.Seed4, transition.PlanProposedVerb, `{"plan": "plans/c-1.md @ abc1234"}`, "digest", false},
		{version.Seed4, transition.PlanProposedVerb, `{"plan": "plans/c-1.md @ abc1234", "digest": "short"}`, "digest", false},
		{version.Seed4, transition.PlanApprovedVerb, `{"plan": "plans/c-1.md @ abc1234", "digest": "` + digestA + `"}`, "pr", false},
		{version.Seed4, transition.PlanApprovedVerb, `{"pr": "pr/1 @ abc1234"}`, "digest", false},
		{version.Seed3, transition.PlanProposedVerb, `{"plan": "plans/c-1.md @ abc1234", "digest": "` + digestA + `"}`, "", true},
		{version.Seed3, transition.PlanApprovedVerb, `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1 @ abc1234", "digest": "` + digestA + `"}`, "", true},
		{version.Seed3, transition.PlanProposedVerb, `{"plan": "plans/c-1.md @ abc1234"}`, "", false},
		{version.Seed4, transition.PlanProposedVerb, `{"plan": "plans/c-1.md @ abc1234", "digest": "` + digestA + `"}`, "", false},
		{version.Seed4, transition.PlanApprovedVerb, `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1 @ abc1234", "digest": "` + digestA + `"}`, "", false},
		{version.Seed4, "submission.made", `{}`, "", false},
	} {
		err := transition.CheckPlanEventShape(tc.v, tc.verb, "c-1", []byte(tc.payload))
		switch {
		case tc.wantMissing != "":
			if !errors.As(err, &inc) || !contains(inc.Missing, tc.wantMissing) {
				t.Fatalf("%s %s %s: want incomplete naming %s, got %v", tc.v, tc.verb, tc.payload, tc.wantMissing, err)
			}
		case tc.wantVersion:
			if !errors.As(err, &chain) || !strings.Contains(chain.Reason, version.Seed4) || !strings.Contains(chain.Reason, tc.v) {
				t.Fatalf("%s %s %s: want a refusal naming the version, got %v", tc.v, tc.verb, tc.payload, err)
			}
		default:
			if err != nil {
				t.Fatalf("%s %s %s: want admitted, got %v", tc.v, tc.verb, tc.payload, err)
			}
		}
	}
	// The approval's pr is an anchor, "<pr> @ <merged-commit>": a bare
	// name carries no revision to hold the approval to (review finding
	// on the task PR), at seed/4 and before it alike.
	for _, tc := range []struct{ v, payload string }{
		{version.Seed4, `{"plan": "plans/c-1.md @ abc1234", "pr": "x", "digest": "` + digestA + `"}`},
		{version.Seed4, `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1 @ ", "digest": "` + digestA + `"}`},
		{version.Seed3, `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1"}`},
	} {
		err := transition.CheckPlanEventShape(tc.v, transition.PlanApprovedVerb, "c-1", []byte(tc.payload))
		if !errors.As(err, &chain) || !strings.Contains(chain.Reason, "<pr> @ <merged-commit>") {
			t.Fatalf("%s %s: a bare pr refuses naming the anchor form, got %v", tc.v, tc.payload, err)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
