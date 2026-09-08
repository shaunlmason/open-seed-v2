// The ready-queue projection (plans/os-fecfb3f7.md step 2;
// plans/os-d69a6c91.md step 5): the registered claimable-work surface.
// The derivation is the transition table's ready set — subjects whose
// folded lifecycle state is ready, oldest first — retiring the v0
// "none" marker exactly as the 4.2 spec promised; the eligibility
// filter follows later, per the build plan.

package project

import (
	"encoding/json"
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// QueueFile is the ready-queue projection's one view file.
const QueueFile = "queue.json"

// QueueSchemaVersion is the queue view's schema version; entries'
// field set is Phase 5's to extend.
const QueueSchemaVersion = "1"

// QueueDerivationNone marked the v0 derivation, before the transition
// table existed; it survives for the derivation-bump drills.
const QueueDerivationNone = "none"

// QueueDerivationTransitions was the live derivation from Version "2"
// to "3": the transition table's ready set (spec/transitions.json,
// schema generation 1). It survives for the derivation-bump drills.
const QueueDerivationTransitions = "transitions/1"

// QueueDerivationEffective is the live derivation (plans/os-f0ae2cdf.md
// D4): the table's ready set narrowed to the effectively ready, those
// with every dependency terminal and no blocked ancestor, read from
// the topology fold (spec/topology.md).
const QueueDerivationEffective = "transitions/1+topology/1"

// QueueEntry is one claimable subject. The field set is minimal by
// design; Phase 5 extends it with what the transition table derives.
type QueueEntry struct {
	Subject       string `json:"subject"`
	SincePosition int    `json:"since_position"`
}

// QueueView is the queue.json shape.
type QueueView struct {
	SchemaVersion string       `json:"schema_version"`
	Derivation    string       `json:"derivation"`
	Ready         []QueueEntry `json:"ready"`
}

// Queue returns the ready-queue projection. Version "2" replaced the
// underived v0 marker with the transition table's ready set; Version
// "3" republishes the fold's seed/1 activation boundary, since a
// pre-activation record can no longer occupy or vacate ready — each
// via the version-in-identity machinery. Version "4" narrows the set
// to the effectively ready (plans/os-f0ae2cdf.md D4): a dependent that
// waits or a descendant that is held leaves the queue and returns at
// the prefix that frees it, with no lifecycle event written.
func Queue() Projection {
	return Projection{Name: "queue", Version: "4", Build: buildQueue}
}

// readyEntries derives the claimable set: subjects whose folded state
// is ready AND who are effectively ready (every active dependency
// terminal, no blocked ancestor), since_position the position that
// made them ready, oldest first (deterministic; the queue surfaces
// the longest-waiting work first). Relation-free chains reduce to the
// table's ready set.
func readyEntries(records []*event.Record) ([]QueueEntry, error) {
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	fold := table.FoldRecords(records)
	graph := deriveTopology(records, table, fold)
	ready := []QueueEntry{}
	for _, subject := range fold.Subjects() {
		if s, ok := fold.State(subject); ok && s.State == "ready" && graph.EffectiveReady(subject) {
			ready = append(ready, QueueEntry{Subject: subject, SincePosition: s.Since})
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].SincePosition < ready[j].SincePosition })
	return ready, nil
}

func buildQueue(records []*event.Record, _ Inputs) (map[string][]byte, error) {
	ready, err := readyEntries(records)
	if err != nil {
		return nil, err
	}
	view := QueueView{
		SchemaVersion: QueueSchemaVersion,
		Derivation:    QueueDerivationEffective,
		Ready:         ready,
	}
	b, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{QueueFile: append(b, '\n')}, nil
}
