package reconcile

import (
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: plans/os-2e34f66a.md AC5 — the evidence-grade half of
// the scorecard rule: a cited scorecard that does not retrieve, or
// whose stored items disagree with the payload's, classifies
// scorecard_unverified; one that agrees classifies nothing.
func TestScorecardUnverifiedAtEvidenceGrade(t *testing.T) {
	store := artifact.Open(t.TempDir())
	digest, err := store.Put([]byte(`{"contract":"c-1","items":[{"evidence":["transcript:0"],"id":"tone","score":"pass","uncertainty":"low"}],"submission":"12"}`))
	if err != nil {
		t.Fatal(err)
	}
	agree := &transition.VerdictFact{Pos: 20, Verdict: "pass", Scorecard: &transition.ScorecardRef{Digest: digest,
		Items: []transition.ScoreItem{{ID: "tone", Score: "pass", Uncertainty: "low"}}}}
	if f := ScorecardAt("c-1", agree, store); f != nil {
		t.Fatalf("an agreeing scorecard classifies nothing: %+v", f)
	}
	disagree := &transition.VerdictFact{Pos: 20, Verdict: "pass", Scorecard: &transition.ScorecardRef{Digest: digest,
		Items: []transition.ScoreItem{{ID: "tone", Score: "fail", Uncertainty: "low"}}}}
	if f := ScorecardAt("c-1", disagree, store); f == nil || f.Class != ClassScorecardUnverified || !strings.Contains(f.Detail, "tone/fail/low") {
		t.Fatalf("payload items the artifact does not carry classify scorecard_unverified naming the disagreement: %+v", f)
	}
	extra := &transition.VerdictFact{Pos: 20, Verdict: "pass", Scorecard: &transition.ScorecardRef{Digest: digest,
		Items: []transition.ScoreItem{{ID: "tone", Score: "pass", Uncertainty: "low"}, {ID: "taste", Score: "pass", Uncertainty: "low"}}}}
	if f := ScorecardAt("c-1", extra, store); f == nil || !strings.Contains(f.Detail, "scores 2 items") {
		t.Fatalf("a payload scoring more items than the artifact classifies: %+v", f)
	}
	missing := &transition.VerdictFact{Pos: 20, Verdict: "pass", Scorecard: &transition.ScorecardRef{Digest: strings.Repeat("ab", 32)}}
	if f := ScorecardAt("c-1", missing, store); f == nil || f.Class != ClassScorecardUnverified || !strings.Contains(f.Detail, "not retrievable") {
		t.Fatalf("a scorecard the store does not hold classifies: %+v", f)
	}
	// EvidenceAt runs it before the merge-dependent half, so a chain
	// without a merge still grades its scorecard.
	s := transition.SubjectState{Verdict: missing}
	if out := EvidenceAt("c-1", s, store, "", Reproduction{}); len(out) != 1 || out[0].Class != ClassScorecardUnverified {
		t.Fatalf("EvidenceAt grades the scorecard on an unmerged chain: %+v", out)
	}
}
