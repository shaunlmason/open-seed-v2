package obligation

// The derivation drills (plans/os-52d5da3f.md D3, D5): each kind
// arises from its folded fact and nothing else, run.unsettled is
// position-anchored, and no emitted row ever carries an empty
// discharging set.

import (
	"slices"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func table(t *testing.T) *transition.Table {
	t.Helper()
	tbl, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	return tbl
}

// rowsFor derives from one hand-built state, bypassing the fold: the
// kinds are a function of the state's facts, and the fold's own
// correctness is its package's business.
func rowsFor(t *testing.T, s transition.SubjectState, open []transition.ReservationFact) []Row {
	t.Helper()
	return rowsWith(t, s, open, nil)
}

// rowsWith is rowsFor with an explicit standing predicate, for the
// drills that turn a reservation signer's standing off.
func rowsWith(t *testing.T, s transition.SubjectState, open []transition.ReservationFact, canDischarge func(string, []string) bool) []Row {
	t.Helper()
	return subjectRows("c-1", s, table(t), Deps{
		BudgetOpen:   func(string, transition.SubjectState) []transition.ReservationFact { return open },
		CanDischarge: canDischarge,
	})
}

func kinds(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Kind)
	}
	slices.Sort(out)
	return out
}

func TestKindsAriseFromTheirFacts(t *testing.T) {
	for name, tc := range map[string]struct {
		state transition.SubjectState
		open  []transition.ReservationFact
		want  []string
	}{
		"ready subject owes nothing": {
			state: transition.SubjectState{State: "ready", Since: 3},
		},
		"an active claim is owed by its holder": {
			state: transition.SubjectState{State: "in_progress", Since: 5, Claim: &transition.Claim{Holder: "aa", Fence: 5}},
			want:  []string{KindClaimHeld},
		},
		"a submission with no verdict is owed by the verifier lane": {
			state: transition.SubjectState{State: "review", Since: 8, Submission: &transition.SubmissionFact{Pos: 8, Signer: "aa"}},
			want:  []string{KindSubmissionPending},
		},
		"a verdict citing the submission discharges it": {
			state: transition.SubjectState{State: "review", Since: 8,
				Submission: &transition.SubmissionFact{Pos: 8, Signer: "aa"},
				Verdict:    &transition.VerdictFact{Pos: 9, Verdict: "pass", Submission: 8}},
			want: []string{KindVerdictUnmerged},
		},
		"a fail verdict owes no merge": {
			state: transition.SubjectState{State: "review", Since: 8,
				Submission: &transition.SubmissionFact{Pos: 8, Signer: "aa"},
				Verdict:    &transition.VerdictFact{Pos: 9, Verdict: "fail", Submission: 8}},
		},
		"a pass verdict on an eval owes no merge": {
			// The verdict is the eval's terminal fact: its consequence
			// is a qualification, never a merge (plans/os-03e47abb.md
			// D10), so the obligation projection carries no row.
			state: transition.SubjectState{State: "review", Since: 8,
				Submission: &transition.SubmissionFact{Pos: 8, Signer: "aa"},
				Verdict:    &transition.VerdictFact{Pos: 9, Verdict: "pass", Submission: 8},
				Eval:       &transition.EvalInfo{Name: "fix-the-check"}},
		},
		"blocked is owed by the operator lane": {
			state: transition.SubjectState{State: "blocked", Since: 4},
			want:  []string{KindContractBlocked},
		},
		"an open reservation inside the window is owed by its signer": {
			state: transition.SubjectState{State: "in_progress", Since: 5, Claim: &transition.Claim{Holder: "aa", Fence: 5}},
			open:  []transition.ReservationFact{{Pos: 6, Signer: "aa", Amount: 2}},
			want:  []string{KindBudgetOpen, KindClaimHeld},
		},
		"an open reservation outside the window is still owed": {
			// The window gates the RESERVE alone (os-d6963652 D1), so
			// both closes stay reachable after it ends and the debt
			// stays a debt: this is the failed-verdict retry, where
			// the hold would otherwise tax the next claimant.
			state: transition.SubjectState{State: "review", Since: 8, Submission: &transition.SubmissionFact{Pos: 8, Signer: "aa"}},
			open:  []transition.ReservationFact{{Pos: 6, Signer: "aa", Amount: 2}},
			want:  []string{KindBudgetOpen, KindSubmissionPending},
		},
		"an open reservation on a done subject is still owed": {
			state: transition.SubjectState{State: "done", Since: 12},
			open:  []transition.ReservationFact{{Pos: 6, Signer: "aa", Amount: 2}},
			want:  []string{KindBudgetOpen},
		},
	} {
		got := kinds(rowsFor(t, tc.state, tc.open))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: kinds %v, want %v", name, got, tc.want)
		}
	}
}

// conformance: an obligation is owed by whoever can still discharge
// it (os-d6963652 D4). The reservation's signer keeps the row while
// they can close it; once suspension or revocation means every close
// from them refuses, the operator lane is the only party left and the
// row must say so, or a keyed situation read hides the debt from the
// one actor able to pay it.
func TestBudgetOwnerFollowsStanding(t *testing.T) {
	held := transition.SubjectState{State: "in_progress", Since: 5, Claim: &transition.Claim{Holder: "bb", Fence: 5}}
	open := []transition.ReservationFact{{Pos: 6, Signer: "aa", Amount: 2}}
	closes := factDischargers[KindBudgetOpen]

	active := func(actor string, verbs []string) bool {
		if !slices.Equal(verbs, closes) {
			t.Fatalf("the standing question asks about the discharging verbs, got %v", verbs)
		}
		return actor == "aa"
	}
	if owner := ownerOf(t, rowsWith(t, held, open, active)); owner != "aa" {
		t.Errorf("an active signer keeps the row: owed by %q, want aa", owner)
	}

	// Even inside a window someone else holds: admission closes a
	// reservation for its own signer or the operator and nobody else,
	// so the holder is never the answer merely for holding.
	none := func(string, []string) bool { return false }
	if owner := ownerOf(t, rowsWith(t, held, open, none)); owner != LaneOperator {
		t.Errorf("a signer who can no longer close hands the row to the operator lane: owed by %q", owner)
	}

	// And with no standing projection at all, nothing establishes
	// that the signer is unable, so the usual owner stands.
	if owner := ownerOf(t, rowsWith(t, held, open, nil)); owner != "aa" {
		t.Errorf("absent a standing predicate the signer stands: owed by %q, want aa", owner)
	}
}

// ownerOf returns the budget.open row's owed-by, failing if no such
// row stands.
func ownerOf(t *testing.T, rows []Row) string {
	t.Helper()
	for _, r := range rows {
		if r.Kind == KindBudgetOpen {
			return r.OwedBy
		}
	}
	t.Fatalf("no %s row in %v", KindBudgetOpen, rows)
	return ""
}

// conformance: the Phase 7 exit's metering-detection obligation is
// position-anchored — post-close settlement is a valid intermediate
// state, so an unsettled run is flagged only once the subject has
// taken a SUBSEQUENT claim window or reached a terminal state.
func TestUnsettledRunIsPositionAnchored(t *testing.T) {
	started := transition.SubjectState{State: "ready", Since: 9,
		RunStarts:   []transition.RunStartFact{{Pos: 7, Fence: 5}},
		ClaimFences: map[int]bool{5: true}}
	if k := kinds(rowsFor(t, started, nil)); slices.Contains(k, KindRunUnsettled) {
		t.Fatalf("a closed window awaiting its settle is not yet a finding: %v", k)
	}
	later := started
	later.ClaimFences = map[int]bool{5: true, 11: true}
	if k := kinds(rowsFor(t, later, nil)); !slices.Contains(k, KindRunUnsettled) {
		t.Fatalf("a subsequent claim window makes the missing settle a finding: %v", k)
	}
	terminal := started
	terminal.State = "done"
	if k := kinds(rowsFor(t, terminal, nil)); !slices.Contains(k, KindRunUnsettled) {
		t.Fatalf("a terminal subject makes the missing settle a finding: %v", k)
	}
	settled := later
	settled.Runs = []transition.RunFact{{Pos: 8, Fence: 5, Units: 1}}
	if k := kinds(rowsFor(t, settled, nil)); slices.Contains(k, KindRunUnsettled) {
		t.Fatalf("a settled fence is no finding: %v", k)
	}
}

// conformance: an obligation nobody can discharge is an anomaly, not
// an obligation — and a drift sweep over an empty discharging set
// would pass vacuously (review finding on the plan PR).
func TestEveryRowCarriesADischarger(t *testing.T) {
	states := []transition.SubjectState{
		{State: "in_progress", Since: 5, Claim: &transition.Claim{Holder: "aa", Fence: 5}},
		{State: "review", Since: 8, Submission: &transition.SubmissionFact{Pos: 8, Signer: "aa"}},
		{State: "review", Since: 8, Verdict: &transition.VerdictFact{Pos: 9, Verdict: "pass", Submission: 8}},
		{State: "blocked", Since: 4},
		{State: "done", Since: 12, RunStarts: []transition.RunStartFact{{Pos: 7, Fence: 5}}, ClaimFences: map[int]bool{5: true}},
	}
	seen := map[string]bool{}
	for _, s := range states {
		for _, row := range rowsFor(t, s, []transition.ReservationFact{{Pos: 6, Signer: "aa", Amount: 2}}) {
			if len(row.DischargedBy) == 0 {
				t.Fatalf("%s carries no discharging verb", row.Kind)
			}
			seen[row.Kind] = true
		}
	}
	for _, kind := range []string{KindClaimHeld, KindSubmissionPending, KindVerdictUnmerged, KindBudgetOpen, KindContractBlocked, KindRunUnsettled} {
		if !seen[kind] {
			t.Errorf("the fixture set never produced %s, so its dischargers are unchecked", kind)
		}
	}
}

// conformance: III — "waiting escalations surface with age". The kind
// arises from the standing question and nothing else, asserted in both
// directions so it cannot pass by always being emitted
// (plans/os-f781f0da.md).
func TestEscalationPendingArisesFromTheStandingQuestion(t *testing.T) {
	blocked := transition.SubjectState{State: "blocked", Since: 7}
	if got := kinds(rowsFor(t, blocked, nil)); slices.Contains(got, KindEscalationPending) {
		t.Fatalf("a plainly blocked subject owes no answer: %v", got)
	}
	escalated := transition.SubjectState{
		State: "blocked", Since: 7,
		Escalation: &transition.EscalationFact{Pos: 5, TS: "2026-09-01T10:00:00Z", Raiser: "fp", Question: "which base?"},
	}
	rows := rowsFor(t, escalated, nil)
	if !slices.Contains(kinds(rows), KindEscalationPending) {
		t.Fatalf("a standing question is owed an answer: %v", kinds(rows))
	}
	// Both kinds stand together: one says a human owes a decision, the
	// other that the contract is stopped. They are not alternatives.
	if !slices.Contains(kinds(rows), KindContractBlocked) {
		t.Fatalf("an escalated subject is also a blocked one: %v", kinds(rows))
	}
	var row Row
	for _, r := range rows {
		if r.Kind == KindEscalationPending {
			row = r
		}
	}
	if row.OwedBy != LaneOperator {
		t.Errorf("a human gate owes the answer, not an actor: %q", row.OwedBy)
	}
	// Since is the RAISE's position, not the state's: a question
	// carried by a claim.parked arrives with the exit that raised it,
	// and a reader needs the position that ASKED.
	if row.Since != 5 {
		t.Errorf("since is the raise's position (5), got %d — the state's is 7", row.Since)
	}
	// And the row carries the raise's ts, which is the whole of "with
	// age": positions order without measuring, so a reader given only
	// Since could compute event count and never elapsed time.
	if row.TS != "2026-09-01T10:00:00Z" {
		t.Errorf("the row carries the raising event's ts, got %q", row.TS)
	}
	if !slices.Contains(row.DischargedBy, "decision.recorded") ||
		!slices.Contains(row.DischargedBy, "contract.cancelled") {
		t.Errorf("both answers discharge it: %v", row.DischargedBy)
	}
	// No other kind carries a ts: the field is present exactly where
	// elapsed time is meaningful, never as decoration.
	for _, r := range rows {
		if r.Kind != KindEscalationPending && r.TS != "" {
			t.Errorf("%s carries a ts it has no use for: %q", r.Kind, r.TS)
		}
	}
}
