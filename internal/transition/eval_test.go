package transition_test

// The eval marker fold (plans/os-03e47abb.md D1, D8; spec/evals.md):
// intent.filed's optional eval object folds to SubjectState.Eval at
// seed/3 positions only, its advisory tuple parsed strictly, a
// malformed one dropped and counted rather than folded as a partial
// configuration.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func TestEvalMarkerFoldsAtSeed3Only(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	marked := `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check", "tuple": ` + foldTuple + `}}`
	recs := []*event.Record{
		payloadEvent("seed/2", "intent.filed", "e-2", marked), // 0: the field is not read before seed/3
		payloadEvent("seed/3", "intent.filed", "e-3", marked), // 1
		payloadEvent("seed/3", "intent.filed", "e-plain", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check"}}`),                            // 2: a first eval names no tuple
		payloadEvent("seed/3", "intent.filed", "e-bad", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check", "tuple": {"principal": "x"}}}`), // 3: malformed tuple
		payloadEvent("seed/3", "intent.filed", "e-noname", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"tuple": `+foldTuple+`}}`),                            // 4: no name, no marker
		payloadEvent("seed/3", "intent.filed", "c-1", `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`),                                                                   // 5: an ordinary contract
	}
	f := tab.FoldRecords(recs)
	state := func(id string) transition.SubjectState {
		t.Helper()
		s, ok := f.State(id)
		if !ok {
			t.Fatalf("%s folds", id)
		}
		return s
	}
	if s := state("e-2"); s.Eval != nil {
		t.Fatalf("at a seed/2 position the marker is unknown and stays unread: %+v", s.Eval)
	}
	if s := state("e-3"); s.Eval == nil || s.Eval.Name != "fix-the-check" || s.Eval.Tuple == nil || s.Eval.Tuple.Model != "fable/5.1" {
		t.Fatalf("at seed/3 the marker folds with its name and its parsed tuple: %+v", s.Eval)
	}
	if s := state("e-plain"); s.Eval == nil || s.Eval.Name != "fix-the-check" || s.Eval.Tuple != nil {
		t.Fatalf("a first eval folds with no tuple under re-test: %+v", s.Eval)
	}
	if s := state("e-bad"); s.Eval == nil || s.Eval.Tuple != nil || s.Anomalies == 0 {
		t.Fatalf("a malformed advisory tuple is dropped and counted, never folded partially: %+v (anomalies %d)", s.Eval, s.Anomalies)
	}
	if s := state("e-noname"); s.Eval != nil {
		t.Fatalf("an eval object with no name marks nothing: %+v", s.Eval)
	}
	if s := state("c-1"); s.Eval != nil || s.Tier != "trivial" {
		t.Fatalf("an ordinary filing folds exactly as before: %+v", s)
	}
}
