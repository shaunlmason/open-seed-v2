package transition_test

// The run-fact fold drills (plans/os-1dad487d.md;
// spec/executors.md): well-shaped facts fold tolerantly as
// independent lists, a fact citing a fence that is no applied claim
// position counts an anomaly, and the claim-fence domain tracks every
// applied claim.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func TestRunFactFold(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	records := []*event.Record{
		payloadEvent("seed/1", "intent.filed", "c-1", `{"tier": "trivial", "budget": "small"}`),     // 0
		lifecycleEvent("contract.specified", "c-1"),                                                 // 1
		lifecycleEvent("claim.taken", "c-1"),                                                        // 2
		budgetEvent("budget.reserve", "c-1", `{"amount": "10"}`),                                    // 3
		payloadEvent("seed/1", "run.started", "c-1", `{"fence": "2", "reservation": "3"}`),          // 4
		payloadEvent("seed/1", "run.started", "c-1", `{"fence": "9", "reservation": "3"}`),          // 5: dangling fence, anomaly
		payloadEvent("seed/1", "run.settled", "c-1", `{"fence": "2", "units": "7", "lines": "3"}`),  // 6
		payloadEvent("seed/1", "run.settled", "c-1", `{"fence": "2", "units": "-1", "lines": "0"}`), // 7: malformed, no fact
	}
	fold := tab.FoldRecords(records)
	s, ok := fold.State("c-1")
	if !ok || !s.ClaimFences[2] || len(s.ClaimFences) != 1 {
		t.Fatalf("the claim-fence domain tracks applied claims: %+v", s.ClaimFences)
	}
	if len(s.RunStarts) != 1 || s.RunStarts[0].Pos != 4 || s.RunStarts[0].Fence != 2 || s.RunStarts[0].Reservation != 3 {
		t.Fatalf("the well-shaped start folds: %+v", s.RunStarts)
	}
	if len(s.Runs) != 1 || s.Runs[0].Pos != 6 || s.Runs[0].Units != 7 || s.Runs[0].Lines != 3 {
		t.Fatalf("the well-shaped settle folds, the malformed one to nothing: %+v", s.Runs)
	}
	if s.Anomalies == 0 {
		t.Fatal("a run fact citing a dangling fence counts an anomaly, never a fact")
	}

	// Raw duplicates on a once-per-fence fact stay visible AND count
	// anomalies (review finding on the task PR).
	before := s.Anomalies
	records = append(records,
		payloadEvent("seed/1", "run.started", "c-1", `{"fence": "2", "reservation": "3"}`),         // 8: duplicate start
		payloadEvent("seed/1", "run.settled", "c-1", `{"fence": "2", "units": "1", "lines": "1"}`), // 9: duplicate settle
	)
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	if len(s.RunStarts) != 2 || len(s.Runs) != 2 {
		t.Fatalf("duplicates stay visible: %d starts, %d runs", len(s.RunStarts), len(s.Runs))
	}
	if s.Anomalies != before+2 {
		t.Fatalf("each raw duplicate counts an anomaly: %d then %d", before, s.Anomalies)
	}

	// Interrupts fold in the same posture (plans/os-0f718b4e.md): the
	// well-shaped fact as an independent list entry, a dangling fence
	// as an anomaly, a same-fence duplicate as an anomaly that stays
	// visible.
	before = s.Anomalies
	records = append(records,
		payloadEvent("seed/1", "run.interrupted", "c-1", `{"fence": "2"}`),  // 10
		payloadEvent("seed/1", "run.interrupted", "c-1", `{"fence": "9"}`),  // 11: dangling fence, anomaly
		payloadEvent("seed/1", "run.interrupted", "c-1", `{"fence": "2"}`),  // 12: duplicate, anomaly + visible
		payloadEvent("seed/1", "run.interrupted", "c-1", `{"fence": "no"}`), // 13: malformed, no fact
	)
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	if len(s.Interrupts) != 2 || s.Interrupts[0].Pos != 10 || s.Interrupts[0].Fence != 2 || s.Interrupts[1].Pos != 12 {
		t.Fatalf("interrupts fold as an independent list, duplicates visible: %+v", s.Interrupts)
	}
	if s.Anomalies != before+2 {
		t.Fatalf("the dangling fence and the duplicate each count an anomaly: %d then %d", before, s.Anomalies)
	}
}
