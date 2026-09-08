// The graph in the projections (plans/os-f0ae2cdf.md D8; spec/topology.md
// "Surfaces"): the one adapter through which the contracts view, the
// queue, the report and the cache read the topology derivation. None
// of them walks an edge itself; each renders what internal/topology
// derived from the same fold admission reads, so a subject is hidden,
// held, counted or warned about identically on every surface at one
// prefix. Everything here is present only when the prefix carries a
// trusted relation fact, so builds of chains that carry none stay
// byte-identical (the knowledge and requests sections' posture).

package project

import (
	"fmt"
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// ContractRelation is one folded relation as the views render it:
// the contract or anchor named, the position it was recorded at, and
// the actor that recorded it.
type ContractRelation struct {
	Target   string `json:"target"`
	Position string `json:"position"`
	Actor    string `json:"actor"`
}

// ContractTopology is a contract's graph view: its relations, what
// remains unresolved, whether it is effectively ready, who holds it,
// the mission it serves and where that mission is carried, its
// goal-ancestry warning when open and unanchored, and its rollup when
// it is an initiative. Lifecycle state is the contract's own `state`
// beside it and is never rewritten here.
type ContractTopology struct {
	Requires       []ContractRelation `json:"requires,omitempty"`
	RequiredBy     []string           `json:"required_by,omitempty"`
	Parent         *ContractRelation  `json:"parent,omitempty"`
	Children       []string           `json:"children,omitempty"`
	Mission        *ContractRelation  `json:"mission,omitempty"`
	MissionAnchor  string             `json:"mission_anchor,omitempty"`
	MissionFrom    string             `json:"mission_from,omitempty"`
	Unresolved     []string           `json:"unresolved"`
	EffectiveReady bool               `json:"effective_ready"`
	HeldBy         []string           `json:"held_by"`
	Warning        *topology.Warning  `json:"goal_ancestry_warning,omitempty"`
	Rollup         *topology.Rollup   `json:"rollup,omitempty"`
}

// ReportInitiative is one initiative's report row: the subject, the
// mission it carries or inherits, and its rollup.
type ReportInitiative struct {
	Subject       string          `json:"subject"`
	MissionAnchor string          `json:"mission_anchor,omitempty"`
	MissionFrom   string          `json:"mission_from,omitempty"`
	Rollup        topology.Rollup `json:"rollup"`
}

// ReportTopology is the report's graph section: every initiative with
// its rollup, every goal-ancestry warning over open work, and every
// relation fact the fold kept but does not trust.
type ReportTopology struct {
	Initiatives          []ReportInitiative `json:"initiatives"`
	GoalAncestryWarnings []topology.Warning `json:"goal_ancestry_warnings"`
	Anomalies            []topology.Anomaly `json:"anomalies"`
}

// deriveTopology is the one derivation every builder calls.
func deriveTopology(records []*event.Record, table *transition.Table, fold *transition.Fold) *topology.Derived {
	return topology.Derive(topology.Fold(records, table), fold, table)
}

func relationOf(r topology.Relation) ContractRelation {
	return ContractRelation{Target: r.Target, Position: fmt.Sprintf("%d", r.Pos), Actor: r.Actor}
}

// contractTopology renders one subject's graph view, nil when the
// prefix carries no trusted relation at all.
func contractTopology(d *topology.Derived, subject string) *ContractTopology {
	if d == nil || !d.Graph.Any() {
		return nil
	}
	v := &ContractTopology{Unresolved: d.Unresolved(subject), EffectiveReady: d.EffectiveReady(subject), HeldBy: d.HeldBy(subject)}
	for _, r := range d.Graph.Requires(subject) {
		v.Requires = append(v.Requires, relationOf(r))
	}
	v.RequiredBy = d.Graph.RequiredBy(subject)
	if p, ok := d.Graph.Parent(subject); ok {
		rel := relationOf(p)
		v.Parent = &rel
	}
	v.Children = d.Graph.Children(subject)
	if m, ok := d.Graph.Mission(subject); ok {
		rel := relationOf(m)
		v.Mission = &rel
	}
	if anchor, from, ok := d.Mission(subject); ok {
		v.MissionAnchor, v.MissionFrom = anchor, from
	}
	if w, ok := d.Warning(subject); ok {
		v.Warning = &w
	}
	if r, ok := d.Rollup(subject); ok {
		v.Rollup = &r
	}
	return v
}

// reportTopology renders the report section, nil when the prefix
// carries no trusted relation and no anomaly.
func reportTopology(d *topology.Derived) *ReportTopology {
	if d == nil || (!d.Graph.Any() && len(d.Graph.Anomalies) == 0) {
		return nil
	}
	sec := &ReportTopology{Initiatives: []ReportInitiative{}, GoalAncestryWarnings: d.Warnings(), Anomalies: append([]topology.Anomaly{}, d.Graph.Anomalies...)}
	for _, s := range d.Initiatives() {
		r, _ := d.Rollup(s)
		row := ReportInitiative{Subject: s, Rollup: r}
		if anchor, from, ok := d.Mission(s); ok {
			row.MissionAnchor, row.MissionFrom = anchor, from
		}
		sec.Initiatives = append(sec.Initiatives, row)
	}
	sort.Slice(sec.Initiatives, func(i, j int) bool { return sec.Initiatives[i].Subject < sec.Initiatives[j].Subject })
	return sec
}
