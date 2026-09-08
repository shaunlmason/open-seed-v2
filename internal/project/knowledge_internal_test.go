package project

// The knowledge view's instant (plans/os-0d537fbd.md D4): with one,
// every expired lesson is flagged stale and leaves the surfacing set;
// without one nothing is flagged and the view says so; a standing
// retirement is rendered with its reason and never surfaces.

import (
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

func viewState() *curation.State {
	return &curation.State{
		Hypotheses: map[string]*curation.HypothesisFact{
			"h-aaaaaaaaaaaa": {ID: "h-aaaaaaaaaaaa", Pos: 4, Stage: curation.StagePromoted},
			"h-bbbbbbbbbbbb": {ID: "h-bbbbbbbbbbbb", Pos: 5, Stage: curation.StagePromoted},
			"h-cccccccccccc": {ID: "h-cccccccccccc", Pos: 6, Stage: curation.StageContested},
		},
		Lessons: map[string]curation.LessonFact{
			"knowledge/lessons/a.md": {Pos: 10, Lesson: "knowledge/lessons/a.md @ 0123456", Hypothesis: "h-aaaaaaaaaaaa@4", Expires: "2026-12-01T00:00:00Z"},
			"knowledge/lessons/b.md": {Pos: 12, Lesson: "knowledge/lessons/b.md @ 0123456", Hypothesis: "h-bbbbbbbbbbbb@5", Expires: "2027-06-01T00:00:00Z"},
			"knowledge/lessons/c.md": {Pos: 14, Lesson: "knowledge/lessons/c.md @ 0123456", Hypothesis: "h-cccccccccccc@6", Expires: "2027-06-01T00:00:00Z"},
		},
		Retired: map[string]curation.RetirementFact{
			"knowledge/lessons/b.md": {Pos: 20, Lesson: "knowledge/lessons/b.md @ 0123456", Hypothesis: "h-bbbbbbbbbbbb@5", Reason: "regression", PR: "pr/10 @ 89abcde"},
		},
	}
}

// conformance: AC1, AC2 — the view at an instant and without one.
func TestKnowledgeViewFlagsStaleOnlyAtADeclaredInstant(t *testing.T) {
	bare := knowledgeView(nil, viewState(), nil)
	if bare.AsOf != "" || bare.Staleness != StalenessUndeclared || bare.Stages.Stale != 0 {
		t.Fatalf("with no instant nothing is flagged and the view says so: %+v", bare)
	}
	for _, l := range bare.Lessons {
		if l.Stale {
			t.Fatalf("no instant, no stale flag: %+v", l)
		}
	}
	at := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	view := knowledgeView(nil, viewState(), &at)
	if view.AsOf != "2026-12-01T00:00:00Z" || view.Staleness != "" || view.Stages.Stale != 1 || view.Stages.Retired != 1 || view.Stages.Lessons != 3 {
		t.Fatalf("at the instant the counts are judged: %+v", view.Stages)
	}
	byPos := map[int]KnowledgeLesson{}
	for _, l := range view.Lessons {
		byPos[l.Pos] = l
	}
	if a := byPos[10]; !a.Stale || a.Surfaces || !strings.Contains(a.Reason, "expired at 2026-12-01T00:00:00Z") || a.Retired {
		t.Fatalf("the lesson at its expiry is stale and does not surface: %+v", a)
	}
	if b := byPos[12]; !b.Retired || b.Surfaces || b.Stale || !strings.Contains(b.Reason, "retired at position 20: regression") || !strings.Contains(b.Reason, "pr/10 @ 89abcde") {
		t.Fatalf("the retired lesson names its retirement: %+v", b)
	}
	if c := byPos[14]; c.Surfaces || c.Stale || c.Retired || c.Reason != "the hypothesis stands contested" {
		t.Fatalf("the contested lesson keeps its reason: %+v", c)
	}
	if len(view.Retired) != 1 || view.Retired[0].Pos != 20 {
		t.Fatalf("the standing retirements are listed: %+v", view.Retired)
	}
	// One second earlier, nothing is stale: expiry is at or past.
	before := at.Add(-time.Second)
	if v := knowledgeView(nil, viewState(), &before); v.Stages.Stale != 0 || !mapLesson(v, 10).Surfaces {
		t.Fatalf("before the expiry the lesson surfaces: %+v", v.Stages)
	}
	// A chain with no lesson says nothing about staleness either way.
	if v := knowledgeView(nil, &curation.State{}, &at); v.AsOf != "" || v.Staleness != "" {
		t.Fatalf("no lesson, no staleness field: %+v", v)
	}
}

func mapLesson(v KnowledgeView, pos int) KnowledgeLesson {
	for _, l := range v.Lessons {
		if l.Pos == pos {
			return l
		}
	}
	return KnowledgeLesson{}
}

// The instant is the declared observation inputs' as-of, and nothing
// otherwise: a bare as-of with no declared family is no input the
// build id would carry, so it declares nothing.
func TestKnowledgeInstantIsTheDeclaredInputsAsOf(t *testing.T) {
	at := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if instantOf(Inputs{}) != nil || instantOf(Inputs{AsOf: at}) != nil {
		t.Fatal("no declared family, no instant")
	}
	got := instantOf(Inputs{Obs: &obs.Snapshot{}, AsOf: at})
	if got == nil || !got.Equal(at) {
		t.Fatalf("the declared as-of is the instant: %v", got)
	}
	if Knowledge().Version != "3" || !Knowledge().Inputs {
		t.Fatalf("the knowledge projection is version 3 and declares input consumption: %+v", Knowledge())
	}
}
