package transition_test

// The independence levels (plans/os-99829835.md D1, D3;
// spec/verdicts.md "Independence levels"): an ordered vocabulary,
// a tier column read through TierGates with the strictest row at L3,
// and a verdict fold that records the level and the verifier's
// declared tuple at seed/4 positions, a malformed tuple dropped and
// counted.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: AC1, AC4 — the order, the vocabulary, and the column.
func TestLevelsAreOrderedAndTiered(t *testing.T) {
	if got := transition.Levels(); len(got) != 3 || got[0] != transition.L1 || got[1] != transition.L2 || got[2] != transition.L3 {
		t.Fatalf("the vocabulary is L1, L2, L3 in order: %v", got)
	}
	for _, l := range []transition.Level{transition.L1, transition.L2, transition.L3} {
		if p, ok := transition.ParseLevel(string(l)); !ok || p != l {
			t.Fatalf("ParseLevel round-trips %s", l)
		}
	}
	for _, bad := range []string{"l1", "L0", "L4", "", "L1 ", "high"} {
		if _, ok := transition.ParseLevel(bad); ok {
			t.Fatalf("%q is not a level", bad)
		}
	}
	rows := []struct {
		have, need transition.Level
		ok         bool
	}{
		{transition.L1, transition.L1, true}, {transition.L1, transition.L2, false}, {transition.L1, transition.L3, false},
		{transition.L2, transition.L1, true}, {transition.L2, transition.L2, true}, {transition.L2, transition.L3, false},
		{transition.L3, transition.L1, true}, {transition.L3, transition.L2, true}, {transition.L3, transition.L3, true},
		{transition.Level("L9"), transition.L1, false},
	}
	for _, r := range rows {
		if got := r.have.Satisfies(r.need); got != r.ok {
			t.Errorf("%s satisfies %s = %v, want %v", r.have, r.need, got, r.ok)
		}
	}
	for tier, want := range map[string]transition.Level{"trivial": transition.L1, "standard": transition.L1, "critical": transition.L2, "wizard": transition.L3, "": transition.L3} {
		if got := transition.TierGates(tier).Independence; got != want {
			t.Errorf("TierGates(%q).Independence = %s, want %s (an unknown tier takes the strictest row)", tier, got, want)
		}
	}
}

// conformance: D3 — the fold records the level and the declared tuple
// at seed/4 positions, marks which records the levels apply to, and
// drops a malformed tuple as an anomaly rather than folding it.
func TestVerdictFoldRecordsLevelAndTuple(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	verdict := func(v, level, tup string) string {
		body := `{"verdict": "pass", "receipt": "ab", "submission": "3", "independence": "` + level + `"`
		if tup != "" {
			body += `, "tuple": ` + tup
		}
		return body + `}`
	}
	recs := []*event.Record{
		payloadEvent("seed/1", "intent.filed", "c-1", `{"tier": "critical", "budget": "small"}`),         // 0
		lifecycleEvent("contract.specified", "c-1"),                                                      // 1
		lifecycleEvent("claim.taken", "c-1"),                                                             // 2
		lifecycleEvent("submission.made", "c-1"),                                                         // 3
		payloadEvent("seed/3", "verdict.rendered", "c-1", verdict("seed/3", "L1", "")),                   // 4: before the levels
		payloadEvent("seed/4", "verdict.rendered", "c-1", verdict("seed/4", "L2", foldTuple)),            // 5
		payloadEvent("seed/4", "verdict.rendered", "c-1", verdict("seed/4", "L2", `{"principal": "x"}`)), // 6: malformed tuple
	}
	f := tab.FoldRecords(recs[:5])
	s, ok := f.State("c-1")
	if !ok || s.Verdict == nil {
		t.Fatal("c-1 folds with a verdict")
	}
	if s.Verdict.Levels || s.Verdict.Independence != "L1" || s.Verdict.Tuple != nil {
		t.Fatalf("a seed/3 verdict records the literal and no levels: %+v", s.Verdict)
	}
	s, _ = tab.FoldRecords(recs[:6]).State("c-1")
	if !s.Verdict.Levels || s.Verdict.Independence != "L2" || s.Verdict.Tuple == nil || s.Verdict.Tuple.Model != "fable/5.1" {
		t.Fatalf("a seed/4 verdict records the level, the tuple and that the levels apply: %+v", s.Verdict)
	}
	before := s.Anomalies
	s, _ = tab.FoldRecords(recs).State("c-1")
	if s.Verdict == nil || s.Verdict.Pos != 6 || s.Verdict.Tuple != nil || s.Verdict.Independence != "L2" || s.Anomalies != before+1 {
		t.Fatalf("a malformed tuple is dropped and counted, the verdict still folds: %+v (anomalies %d, before %d)", s.Verdict, s.Anomalies, before)
	}
}
