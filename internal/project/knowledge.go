// The knowledge projection (plans/os-f30ee0d3.md D3; build plan Phase
// 11 item 1): the curation stages rendered from the verified prefix.
// Dead ends by contract, hypotheses with their stage, lessons by path,
// the standing retirements, and the promotions that cite no admitted
// hypothesis, which are unbound rather than lessons. The one input it
// reads is the declared instant (plans/os-0d537fbd.md D4): with one,
// every expired lesson is flagged stale; without one nothing is, and
// the view says so.

package project

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// KnowledgeFile is the projection's one view file.
const KnowledgeFile = "knowledge.json"

// KnowledgeView is knowledge.json.
type KnowledgeView struct {
	// Stages counts the pipeline's stages: observations (dead ends
	// recorded), hypotheses proposed, promoted and contested, lessons,
	// and the unbound promotions.
	Stages KnowledgeStages `json:"stages"`
	// AsOf is the declared instant the stale flags are judged at, and
	// Staleness what the build could judge (plans/os-0d537fbd.md D4):
	// with no instant declared no lesson is flagged, and the view says
	// so rather than reading as "nothing is stale". Both are present
	// only once the chain holds a lesson.
	AsOf       string                            `json:"as_of,omitempty"`
	Staleness  string                            `json:"staleness,omitempty"`
	DeadEnds   map[string][]curation.DeadEndFact `json:"dead_ends"`
	Hypotheses []curation.HypothesisFact         `json:"hypotheses"`
	Contests   map[string][]curation.ContestFact `json:"contests"`
	Lessons    []KnowledgeLesson                 `json:"lessons"`
	// Retired lists the standing retirements, by position: the
	// conclusion revoked, the evidence (the promotion, its file, its
	// hypothesis) still in the sections above.
	Retired   []curation.RetirementFact `json:"retired,omitempty"`
	Unbound   []curation.LessonFact     `json:"unbound"`
	Anomalies int                       `json:"anomalies"`
}

// KnowledgeLesson is a promotion with whether it surfaces from the
// record alone (the latest promotion of its path, not contested, not
// retired, not expired at the declared instant) and why not; the
// repository half of surfacing (the anchor's ancestry and the digest)
// is the reader's, since a projection build holds no repository.
type KnowledgeLesson struct {
	curation.LessonFact
	Surfaces bool   `json:"surfaces"`
	Reason   string `json:"reason,omitempty"`
	// Stale is the expiry flag at the declared instant; Retired the
	// standing retirement (plans/os-0d537fbd.md D1, D2).
	Stale   bool `json:"stale,omitempty"`
	Retired bool `json:"retired,omitempty"`
}

// KnowledgeStages is the count per stage, the report's section too.
type KnowledgeStages struct {
	Observations int `json:"observations"`
	Hypotheses   int `json:"hypotheses"`
	Promoted     int `json:"promoted"`
	Contested    int `json:"contested"`
	Lessons      int `json:"lessons"`
	Unbound      int `json:"unbound"`
	Retired      int `json:"retired,omitempty"`
	Stale        int `json:"stale,omitempty"`
}

// StalenessUndeclared is what the view says when no instant was
// declared: nothing is flagged, and that is a statement about the
// build, not about the lessons.
const StalenessUndeclared = "undeclared: no instant was declared, so no lesson is flagged stale"

// Knowledge returns the knowledge projection. Version "2" added the
// contested stage, the single-actor note and per-lesson surfacing
// (plans/os-96850e5a.md D7); version "3" the retirements, the latest
// promotion per path and the stale flags at the declared instant
// (plans/os-0d537fbd.md D4), which is why the projection now declares
// input consumption: an instant is an input, and a build at another
// instant is another build.
func Knowledge() Projection {
	return Projection{Name: "knowledge", Version: "3", Inputs: true, Build: buildKnowledge}
}

// instantOf is the declared instant a build judges staleness at: the
// as-of of the declared observation inputs, the one input family that
// carries one, and nothing otherwise (plans/os-0d537fbd.md D4).
func instantOf(in Inputs) *time.Time {
	if in.Obs == nil || in.AsOf.IsZero() {
		return nil
	}
	at := in.AsOf.UTC()
	return &at
}

// DeriveKnowledge renders the view from the prefix with no instant
// declared: the shared derivation the CLI's show renders by default.
func DeriveKnowledge(records []*event.Record) KnowledgeView {
	return DeriveKnowledgeAt(records, nil)
}

// DeriveKnowledgeAt renders the view from the prefix at the declared
// instant, or with none.
func DeriveKnowledgeAt(records []*event.Record, at *time.Time) KnowledgeView {
	return knowledgeView(records, curation.Fold(records), at)
}

// knowledgeView renders the fold; the derivation the projection
// builds from and the CLI's show renders.
func knowledgeView(records []*event.Record, st *curation.State, at *time.Time) KnowledgeView {
	view := KnowledgeView{DeadEnds: st.DeadEnds, Hypotheses: []curation.HypothesisFact{}, Contests: st.Contests,
		Lessons: []KnowledgeLesson{}, Unbound: []curation.LessonFact{}, Anomalies: st.Anomalies}
	if view.DeadEnds == nil {
		view.DeadEnds = map[string][]curation.DeadEndFact{}
	}
	if view.Contests == nil {
		view.Contests = map[string][]curation.ContestFact{}
	}
	var table *transition.Table
	if t, err := transition.Default(); err == nil {
		table = t
	}
	for _, id := range st.HypothesisIDs() {
		h, _ := st.Hypothesis(id)
		fact := *h
		if table != nil {
			fact.SingleActorFamily = st.SingleActorFamily(records, table, id)
		}
		view.Hypotheses = append(view.Hypotheses, fact)
		switch h.Stage {
		case curation.StagePromoted:
			view.Stages.Promoted++
		case curation.StageContested:
			view.Stages.Contested++
		}
	}
	for path, l := range st.Lessons {
		row := KnowledgeLesson{LessonFact: l, Surfaces: true}
		c, _ := curation.ParseCitation(l.Hypothesis)
		switch {
		case st.RetiredPath(path):
			r := st.Retired[path]
			row.Surfaces, row.Retired, row.Reason = false, true, retiredBecause(r)
		case st.Contested(c.Contract):
			row.Surfaces, row.Reason = false, "the hypothesis stands contested"
		}
		if at != nil && curation.Expired(l, *at) {
			row.Stale = true
			view.Stages.Stale++
			if row.Surfaces {
				row.Surfaces, row.Reason = false, fmt.Sprintf("expired at %s: the declared instant %s is at or past it, and a revalidation (a re-promotion with the stamps moved forward) brings it back", l.Expires, at.Format(time.RFC3339))
			}
		}
		view.Lessons = append(view.Lessons, row)
	}
	sort.Slice(view.Lessons, func(i, j int) bool { return view.Lessons[i].Pos < view.Lessons[j].Pos })
	for _, r := range st.Retired {
		view.Retired = append(view.Retired, r)
	}
	sort.Slice(view.Retired, func(i, j int) bool { return view.Retired[i].Pos < view.Retired[j].Pos })
	view.Stages.Retired = len(view.Retired)
	if len(st.Lessons) > 0 {
		if at != nil {
			view.AsOf = at.Format(time.RFC3339)
		} else {
			view.Staleness = StalenessUndeclared
		}
	}
	if st.Unbound != nil {
		view.Unbound = st.Unbound
	}
	for _, ds := range view.DeadEnds {
		view.Stages.Observations += len(ds)
	}
	view.Stages.Hypotheses = len(view.Hypotheses)
	view.Stages.Lessons = len(view.Lessons)
	view.Stages.Unbound = len(view.Unbound)
	return view
}

// retiredBecause renders a standing retirement's reason with the
// evidence its reason carries.
func retiredBecause(r curation.RetirementFact) string {
	out := fmt.Sprintf("retired at position %d: %s", r.Pos, r.Reason)
	switch {
	case r.PR != "":
		out += " (the revert merged as " + r.PR + ")"
	case r.SupersededBy != "":
		out += " (by the promotion at position " + r.SupersededBy + ")"
	}
	return out
}

func buildKnowledge(records []*event.Record, in Inputs) (map[string][]byte, error) {
	b, err := json.MarshalIndent(DeriveKnowledgeAt(records, instantOf(in)), "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{KnowledgeFile: append(b, '\n')}, nil
}
