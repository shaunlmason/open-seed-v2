package obligation

// The forge-observed kind (plans/os-0cd18799.md D3, AC2): the
// unmergeable obligation arises from a red observation on the
// submission under review and from nothing else, is owed by the
// dispatch lane, carries the head, is not emitted for pending alone
// nor once a fail verdict stands, and displaces the merge debt while
// the forge says red; a verdict is owed only while the subject is
// under review.

import (
	"slices"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func TestForgeObservationKinds(t *testing.T) {
	one := 1
	zero := 0
	head := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sub := &transition.SubmissionFact{Pos: 8, Signer: "aa", PR: "pr/1"}
	for name, tc := range map[string]struct {
		state transition.SubjectState
		want  []string
	}{
		"a red check is owed to the dispatch lane beside the verdict": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Observation: &transition.CheckFact{Pos: 9, Head: head, Checks: "red", Threads: &zero, Review: "none"}},
			want: []string{KindSubmissionPending, KindSubmissionUnmergeable},
		},
		"an unresolved thread is red": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Observation: &transition.CheckFact{Pos: 9, Head: head, Checks: "green", Threads: &one, Review: "approved"}},
			want: []string{KindSubmissionPending, KindSubmissionUnmergeable},
		},
		"changes requested is red": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Observation: &transition.CheckFact{Pos: 9, Head: head, Checks: "green", Review: "changes_requested"}},
			want: []string{KindSubmissionPending, KindSubmissionUnmergeable},
		},
		"pending alone is not red": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Observation: &transition.CheckFact{Pos: 9, Head: head, Checks: "pending", Threads: &zero, Review: "none"}},
			want: []string{KindSubmissionPending},
		},
		"green with no threads owes nothing new": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Observation: &transition.CheckFact{Pos: 9, Head: head, Checks: "green", Threads: &zero, Review: "approved"}},
			want: []string{KindSubmissionPending},
		},
		"a standing fail verdict owns the return": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Verdict:         &transition.VerdictFact{Pos: 10, Verdict: "fail", Submission: 8},
				SubmissionFails: []transition.VerdictFact{{Pos: 10, Verdict: "fail", Submission: 8}},
				Observation:     &transition.CheckFact{Pos: 9, Head: head, Checks: "red", Review: "none"}},
			want: nil,
		},
		"a pass verdict under a red observation owes the return, not the merge": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Verdict:     &transition.VerdictFact{Pos: 10, Verdict: "pass", Submission: 8},
				Observation: &transition.CheckFact{Pos: 11, Head: head, Checks: "red", Review: "none"}},
			want: []string{KindSubmissionUnmergeable},
		},
		"a pass verdict under a green observation owes the merge": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: sub,
				Verdict:     &transition.VerdictFact{Pos: 10, Verdict: "pass", Submission: 8},
				Observation: &transition.CheckFact{Pos: 11, Head: head, Checks: "green", Review: "approved"}},
			want: []string{KindVerdictUnmerged},
		},
		"a returned subject owes neither the verdict nor the return": {
			state: transition.SubjectState{State: "ready", Since: 12, Submission: sub,
				Observation: &transition.CheckFact{Pos: 9, Head: head, Checks: "red", Review: "none"},
				Returns:     []transition.ReturnFact{{Pos: 12, Verdict: -1, Observation: 9}}},
			want: nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rows := rowsFor(t, tc.state, nil)
			if got := kinds(rows); !slices.Equal(got, tc.want) {
				t.Fatalf("kinds = %v, want %v", got, tc.want)
			}
			for _, r := range rows {
				if r.Kind != KindSubmissionUnmergeable {
					continue
				}
				if r.OwedBy != LaneDispatcher || r.Head != head || r.Since != tc.state.Observation.Pos {
					t.Fatalf("the row names the dispatch lane, the head and the observation's position: %+v", r)
				}
				if !slices.Equal(r.DischargedBy, []string{"contract.returned"}) {
					t.Fatalf("the return discharges it: %v", r.DischargedBy)
				}
			}
		})
	}
}
