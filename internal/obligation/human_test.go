package obligation

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: plans/os-2e34f66a.md AC4 — a deferral on the current
// window with no render after it owes the operator lane a human
// verdict, discharged by verdict.rendered; a render after the deferral
// clears it, and one before it (an earlier window's) does not.
func TestHumanVerdictIsOwedByTheOperatorLane(t *testing.T) {
	deferred := transition.SubjectState{State: "review", Since: 9,
		Submission: &transition.SubmissionFact{Pos: 9, Signer: "holder"},
		Deferred:   &transition.DeferralFact{Pos: 12, Signer: "verifier", Submission: 9, Receipt: "aa", Items: []string{"tone"}}}
	rows := rowsFor(t, deferred, nil)
	var human *Row
	for i := range rows {
		if rows[i].Kind == KindVerdictHuman {
			human = &rows[i]
		}
	}
	if human == nil || human.OwedBy != LaneOperator || human.Since != 12 || len(human.DischargedBy) != 1 || human.DischargedBy[0] != "verdict.rendered" {
		t.Fatalf("the deferral owes the operator lane a human verdict since the deferral, discharged by the render: %+v", rows)
	}
	judged := deferred
	judged.Verdict = &transition.VerdictFact{Pos: 15, Verdict: "pass", Submission: 9, Signer: "human"}
	for _, r := range rowsFor(t, judged, nil) {
		if r.Kind == KindVerdictHuman {
			t.Fatalf("a render after the deferral clears the debt: %+v", r)
		}
	}
	stale := deferred
	stale.Verdict = &transition.VerdictFact{Pos: 5, Verdict: "fail", Submission: 3, Signer: "verifier"}
	found := false
	for _, r := range rowsFor(t, stale, nil) {
		found = found || r.Kind == KindVerdictHuman
	}
	if !found {
		t.Fatal("an earlier window's verdict does not answer this window's deferral")
	}
}
