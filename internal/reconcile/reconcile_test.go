package reconcile

import (
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func find(fs []Finding, class string) *Finding {
	for i := range fs {
		if fs[i].Class == class {
			return &fs[i]
		}
	}
	return nil
}

func TestSubjectClassifiesInducedDivergences(t *testing.T) {
	pass := &transition.VerdictFact{Pos: 9, Verdict: "pass", Receipt: "r"}
	fail := &transition.VerdictFact{Pos: 9, Verdict: "fail", Receipt: "r"}
	req := &transition.RequestFact{Pos: 10, CitedVerdict: 9}
	merged := &transition.MergeFact{Pos: 11, SHA: "abc"}
	// The chain drills run on the trivial tier so the sealed-checks
	// class stays out of their way; the sealed drills below induce it
	// deliberately (plans/os-3128535a.md).
	triv := transition.TrivialTier

	cases := map[string]struct {
		state transition.SubjectState
		want  []string
	}{
		"clean full chain": {
			transition.SubjectState{State: "done", Tier: triv, Verdict: pass, Requested: req, Merged: merged}, nil},
		"no chain activity": {
			transition.SubjectState{State: "in_progress", Tier: triv}, nil},
		"merge without any verdict": {
			transition.SubjectState{State: "done", Tier: triv, Merged: merged}, []string{ClassMergeWithoutVerdict}},
		"merge over a fail verdict": {
			transition.SubjectState{State: "done", Tier: triv, Verdict: fail, Merged: merged}, []string{ClassMergeWithoutVerdict}},
		"chain skipped, no request": {
			transition.SubjectState{State: "done", Tier: triv, Verdict: pass, Merged: merged}, []string{ClassChainSkipped}},
		"chain skipped, wrong citation": {
			transition.SubjectState{State: "done", Tier: triv, Verdict: pass,
				Requested: &transition.RequestFact{Pos: 10, CitedVerdict: 3}, Merged: merged}, []string{ClassChainSkipped}},
		"unreconciled pass verdict": {
			transition.SubjectState{State: "review", Tier: triv, Verdict: pass}, []string{ClassUnreconciled}},
		"fail verdict alone is not unreconciled": {
			transition.SubjectState{State: "review", Tier: triv, Verdict: fail}, nil},
		"a judged eval is complete, never unreconciled": {
			// plans/os-03e47abb.md D10: the eval's verdict is its
			// terminal fact and no merge is owed.
			transition.SubjectState{State: "review", Tier: triv, Verdict: pass,
				Eval: &transition.EvalInfo{Name: "fix-the-check"}}, nil},
		"an eval merged around the chain still diverges": {
			// The exclusion is one class wide: an eval that reached
			// done with no pass verdict is merge_without_verdict like
			// any subject.
			transition.SubjectState{State: "done", Tier: triv, Merged: merged,
				Eval: &transition.EvalInfo{Name: "fix-the-check"}}, []string{ClassMergeWithoutVerdict}},
		"above-trivial implementation with no commitment": {
			transition.SubjectState{State: "in_progress", Tier: "standard"}, []string{ClassUnsealed}},
		"above-trivial sealed subject is clean": {
			transition.SubjectState{State: "in_progress", Tier: "standard",
				Sealed: &transition.SealedFact{Pos: 3, Commitment: "c"}}, nil},
		"above-trivial still ready is not yet flagged": {
			transition.SubjectState{State: "ready", Tier: "standard"}, nil},
		"critical implementation with no commitment": {
			// The lint reads the tier table (plans/os-be12ac16.md D4).
			transition.SubjectState{State: "in_progress", Tier: "critical"}, []string{ClassUnsealed}},
		"an unknown tier takes the strictest row": {
			transition.SubjectState{State: "review", Tier: "wizard"}, []string{ClassUnsealed}},
		"trivial by the table, not the constant": {
			transition.SubjectState{State: "review", Tier: "trivial"}, nil},
		"a planted row saying no sealed checks is exempt too": {
			transition.SubjectState{State: "review", Tier: "sandbox"}, nil},
		"override-backed chain is sanctioned, by name": {
			transition.SubjectState{State: "done", Tier: triv, Verdict: fail, Merged: merged,
				Override:  &transition.OverrideFact{Pos: 12, Reason: "r", CitedVerdict: 9},
				Requested: &transition.RequestFact{Pos: 13, CitedVerdict: -1, CitedOverride: 12}}, []string{ClassOverridden}},
		"override without a citing request is divergence": {
			transition.SubjectState{State: "done", Tier: triv, Verdict: fail, Merged: merged,
				Override: &transition.OverrideFact{Pos: 12, Reason: "r", CitedVerdict: 9}}, []string{ClassMergeWithoutVerdict}},
	}
	restore := transition.InjectTier("sandbox", transition.TierRow{})
	defer restore()
	for name, c := range cases {
		got := Subject("c-x", c.state)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %+v, want classes %v", name, got, c.want)
		}
		for _, w := range c.want {
			if find(got, w) == nil {
				t.Fatalf("%s: missing class %s in %+v", name, w, got)
			}
		}
	}
}

func TestUnreconciledStaysNeutral(t *testing.T) {
	// The class is a surfaced state, never an accusation: with no wall
	// clock in any build, pending versus failed is maintenance's age
	// judgment. The detail prose is pinned to say so.
	fs := Subject("c-1", transition.SubjectState{State: "review",
		Verdict: &transition.VerdictFact{Pos: 4, Verdict: "pass"}})
	f := find(fs, ClassUnreconciled)
	if f == nil {
		t.Fatal("a pass verdict with no merge is unreconciled")
	}
	low := strings.ToLower(f.Detail)
	for _, banned := range []string{"failed", "stale", "violat", "accus"} {
		if strings.Contains(low, banned) {
			t.Fatalf("unreconciled must stay neutral, detail %q contains %q", f.Detail, banned)
		}
	}
}
