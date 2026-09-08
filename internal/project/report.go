// The report projection (plans/os-fecfb3f7.md step 4): the operational
// summary whose sections later phases extend. Sections that need
// Phase 5+ facts (claims, offers, budgets, expiry-vs-wedge,
// divergence) are named in the spec as extension points, not emitted
// empty.

package project

import (
	"encoding/json"
	"fmt"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"sort"
	"strconv"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/offers"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
)

// ReportFile is the report projection's one view file.
const ReportFile = "report.json"

// checkpointVerb mirrors keyring's private constant (keyring cannot be
// the exporter: it mirrors the same literal itself); the report drill
// pins parity by counting a real checkpoint record.
const checkpointVerb = "system.checkpoint"

// ReportChain is the chain section: the verified prefix's identity.
type ReportChain struct {
	Position      int    `json:"position"`
	Tip           string `json:"tip"`
	ActiveVersion string `json:"active_version"`
}

// ReportActors is the actor section: counts by standing plus roots.
type ReportActors struct {
	ByStanding map[string]int `json:"by_standing"`
	Roots      int            `json:"roots"`
	Total      int            `json:"total"`
}

// ReportHalt is the halt section; DeclaredPosition is present only
// while halted.
type ReportHalt struct {
	Halted           bool   `json:"halted"`
	DeclaredPosition *int   `json:"declared_position,omitempty"`
	By               string `json:"by,omitempty"`
}

// ReportCheckpoints is the checkpoint section; LastPosition is present
// only when a checkpoint exists.
type ReportCheckpoints struct {
	Count        int  `json:"count"`
	LastPosition *int `json:"last_position,omitempty"`
}

// ReportContracts is the work section: subject and event counts.
type ReportContracts struct {
	Subjects int `json:"subjects"`
	Events   int `json:"events"`
}

// ReportObservationInputs echoes the declared inputs an observation
// section was computed from: reproducibility means naming them.
type ReportObservationInputs struct {
	AsOf               string `json:"as_of"`
	Digest             string `json:"digest"`
	ExpiryAfterSeconds int    `json:"expiry_after_seconds"`
	WedgeAfterSeconds  int    `json:"wedge_after_seconds"`
}

// ReportObservation is the expiry-vs-wedge section: one classification
// per active claim, read ONLY from the claim's own fence-keyed stream.
type ReportObservation struct {
	Inputs ReportObservationInputs       `json:"inputs"`
	Claims map[string]obs.Classification `json:"claims"`
}

// ReportRefusalsInputs echoes the declared attempts journal a
// refusals section was computed from: its digest and how many
// attempt lines it carried.
type ReportRefusalsInputs struct {
	Digest  string `json:"digest"`
	Entries int    `json:"entries"`
}

// ReportRefusalsSpan is the journal's position context: the min and
// max tip-ordinal positions its attempts were stamped at. Context
// only — the rate never reads the chain (spec/refusals.md).
type ReportRefusalsSpan struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// ReportRefusals is the affordance-gap section (charter III.I row
// 4): outcome counts over the declared attempts journal — one
// population for numerator and denominator — refusal breakdowns by
// code and by verb, the position span as context, and the rate as a
// fixed four-decimal string. Span is null on an empty journal.
type ReportRefusals struct {
	Inputs   ReportRefusalsInputs `json:"inputs"`
	Refused  int                  `json:"refused"`
	Admitted int                  `json:"admitted"`
	ByCode   map[string]int       `json:"by_code"`
	ByVerb   map[string]int       `json:"by_verb"`
	Span     *ReportRefusalsSpan  `json:"span"`
	Rate     string               `json:"rate"`
	// BlindRetries is III.R row 5's blind-retry clause measured
	// (plans/os-a9e715dc.md D3), by the definition spec/modes.md
	// gives the loop drills: a refused attempt followed by the same
	// actor's next attempt on the same subject with the same act
	// digest, refused with the same code from the same stamped
	// position (the view did not advance), counted once per refusal.
	// An admitted next attempt is convergence, and a refusal with
	// another code or from an advanced position is a new answer to a
	// new state; neither is blind. BlindRetriesByCode splits the count
	// by the code, so the optimistic loop's contention spins read
	// apart from a fenced or invalid act re-sent unchanged; Undigested
	// is the lines that carry no digest and could not be judged.
	BlindRetries       int            `json:"blind_retries"`
	BlindRetriesByCode map[string]int `json:"blind_retries_by_code"`
	Undigested         int            `json:"undigested"`
}

// ReportView is the report.json shape. Observation is null when the
// rebuild declared no observation inputs, and Refusals when it
// declared no attempts journal: absence of data is stated, never
// fabricated (spec/observations.md; spec/refusals.md).
type ReportView struct {
	Chain          ReportChain           `json:"chain"`
	Actors         ReportActors          `json:"actors"`
	Halt           ReportHalt            `json:"halt"`
	Checkpoints    ReportCheckpoints     `json:"checkpoints"`
	Contracts      ReportContracts       `json:"contracts"`
	Reconciliation *ReportReconciliation `json:"reconciliation"`
	Observation    *ReportObservation    `json:"observation"`
	Refusals       *ReportRefusals       `json:"refusals"`
	// Knowledge counts the curation stages (plans/os-f30ee0d3.md D3),
	// present only when the prefix carries a curation fact, so builds
	// of chains that carry none stay byte-identical.
	Knowledge *KnowledgeStages `json:"knowledge,omitempty"`
	// Flywheel is the conversion-rate section (plans/os-9075c308.md
	// D5): shapes recurring, proposed and merged, the repair contracts
	// filed and done, and merged over recurring, record-derivable from
	// the fold alone. Null when no work subject exists.
	Flywheel *flywheel.Metrics `json:"flywheel"`
	// Lanes is the lane-quality section (plans/os-6bd9ffff.md D6;
	// charter III.J row 3): the dispatcher's re-triage rate and the
	// planner's unedited-approval rate, record-derivable from the
	// fold alone. Null when no work subject exists, the reconciliation
	// section's posture.
	Lanes *ReportLanes `json:"lanes"`
	// Requests is the ingress section (plans/os-48df10a2.md D2;
	// spec/requests.md): requests by kind and by outcome, the
	// unanswered count, and the answer latency in elapsed seconds
	// over the answered ones, present only when the prefix carries a
	// request, so builds of chains that carry none stay
	// byte-identical.
	Requests *ReportRequests `json:"requests,omitempty"`
	// Adapters is the per-executor-substrate section (plans/os-083112ac.md
	// D2): the runs started under each harness and its budget posture,
	// derived from the run.started tuples the fold holds. Present only
	// when the prefix carries a run.started, so chains that carry none
	// stay byte-identical. A cloud or remote adapter never reads
	// enforced; the safe default for an unknown harness is a risk limit.
	Adapters []ReportAdapter `json:"adapters,omitempty"`
	// Topology is the graph section (plans/os-f0ae2cdf.md D8;
	// spec/topology.md): initiatives with their rollups, the
	// goal-ancestry warnings over open work, and the relation facts the
	// fold kept but does not trust. Present only when the prefix
	// carries a relation fact, so chains that carry none stay
	// byte-identical.
	Topology *ReportTopology `json:"topology,omitempty"`
}

// ReportAdapter is one executor substrate's report row.
type ReportAdapter struct {
	Harness string `json:"harness"`
	Runs    int    `json:"runs"`
	Budget  string `json:"budget"`
}

// ReportRequests counts the inbound requests: total, by kind, by
// outcome (`pending` for the unanswered), and the mean answer latency
// in elapsed seconds as a string, null when nothing was answered.
type ReportRequests struct {
	Total             int            `json:"total"`
	Unanswered        int            `json:"unanswered"`
	ByKind            map[string]int `json:"by_kind"`
	ByOutcome         map[string]int `json:"by_outcome"`
	MeanAnswerSeconds *string        `json:"mean_answer_seconds"`
}

// ReportLanes carries the two lane-quality metrics III.J row 3 asks
// for, each over the fold's own facts.
type ReportLanes struct {
	Dispatcher ReportDispatcher `json:"dispatcher"`
	Planner    ReportPlanner    `json:"planner"`
	// ByKind splits both figures by the acting key's roster kind
	// (plans/os-0d4f2af3.md D6; III.E row 9): a specification counts
	// under the kind of the key that appended it, an approval under
	// its approver's, so an operator can see whether the agents or the
	// humans are the ones re-triaging. Keys are the kinds seen.
	ByKind map[string]*ReportLanesKind `json:"by_kind"`
}

// ReportLanesKind is one kind's share of the two figures.
type ReportLanesKind struct {
	Dispatcher ReportDispatcher `json:"dispatcher"`
	Planner    ReportPlanner    `json:"planner"`
}

// ReportDispatcher is the re-triage figure: subjects with one or more
// applied specifications, those with two or more (a re-specification
// from ready, the seed/4 row), and their ratio as a three-decimal
// string, null at a zero denominator.
type ReportDispatcher struct {
	Specified    int     `json:"specified"`
	Respecified  int     `json:"respecified"`
	RetriageRate *string `json:"retriage_rate"`
}

// ReportPlanner is the unedited-approval figure: subjects carrying an
// approval, split into unedited (the approval's digest equals the
// first proposal's), edited, and unmeasured (an approval or proposal
// before seed/4 carries no digest: stated, never guessed), with the
// unedited share of the measured approvals as a three-decimal
// string, null at a zero denominator.
type ReportPlanner struct {
	Approvals    int     `json:"approvals"`
	Unedited     int     `json:"unedited"`
	Edited       int     `json:"edited"`
	Unmeasured   int     `json:"unmeasured"`
	UneditedRate *string `json:"unedited_rate"`
	// Strongest is the tuples scope of the latest applied
	// offer.published that carries one (plans/os-c7554f18.md D3;
	// spec/ranking.md): the input the planner lane last received
	// by policy. Absent when no offer has carried a scope, and never
	// split by kind.
	Strongest []tuple.Tuple `json:"strongest,omitempty"`
}

// ReportReconciliation is the record-derivable half of divergence
// detection (plans/os-6cdc15be.md; spec/reconciliation.md):
// findings by class over the lifecycle fold, with seed reconcile named
// for the evidence-grade rest — projection builds never read the
// artifact store or the repository. Null only when no work subject
// exists at all, so the section's presence is itself record-derived.
type ReportReconciliation struct {
	Findings []reconcile.Finding `json:"findings"`
	ByClass  map[string]int      `json:"by_class"`
	// EvidenceGrade names the surface carrying the checks a build
	// cannot run (attested heads, target rewrites).
	EvidenceGrade string `json:"evidence_grade"`
}

// Report returns the report projection. Version "2" added the
// observation section and the declared-inputs identity (the section is
// null on an input-free build, so version, not content, is what
// republishes existing prefixes); Version "3" the reconciliation
// section (plans/os-6cdc15be.md); Version "4" the unsealed class in
// it (plans/os-3128535a.md); Version "5" the override classes
// (plans/os-d2497eb7.md); Version "6" republishes over the offer-fact
// fold (plans/os-c61c3392.md) — offers add no report section, so
// content is unchanged and version, not content, is what republishes
// existing prefixes; Version "7" republishes over the budget-fact
// fold (plans/os-cecac5de.md) the same way — the per-adapter
// risk-limit surface arrives with 7.3's adapters; Version "8"
// republishes over the run-fact fold (plans/os-1dad487d.md), the
// same posture; Version "9" over the interrupt fold
// (plans/os-0f718b4e.md) likewise; Version "10" adds the refusals
// section from the declared attempts journal (plans/os-edf73d66.md),
// null on builds that declare none, so version, not content, is
// what republishes existing input-free prefixes. Version "11" adds
// the knowledge section (plans/os-f30ee0d3.md) and its contested
// count (plans/os-96850e5a.md): the section changes the bytes of
// every prefix carrying a curation fact, so an unchanged tip
// republishes under a new build id rather than keeping a tree without
// it (review finding on the item 3 PR). Version "12" moves with the
// section again: the retired and stale counts, the latter judged at
// the declared instant (plans/os-0d537fbd.md D4). Version "18" adds the
// blind-retry counts to the refusals section (plans/os-a9e715dc.md D3),
// which only a build declaring a journal carries. Version "17" adds the
// per-adapter section from the run.started tuples (plans/os-083112ac.md
// D2), present only when a run.started with a harness is carried; "16"
// was the planner's strongest (plans/os-c7554f18.md D3), which landed
// first. Version "13" adds
// the lanes section (plans/os-6bd9ffff.md D6) and version "14" the
// flywheel section (plans/os-9075c308.md D5), both the same posture:
// record-derivable from the fold, so an unchanged tip republishes
// with them. Inputs marks it as
// an input-consuming projection; the knowledge projection is the
// other since version "3", and everything else stays byte-identical
// with and without inputs by construction.
func Report() Projection {
	return Projection{Name: "report", Version: "19", Inputs: true, Build: buildReport}
}

// reportView is the report derivation shared by the JSON view and the
// cache tables.
func reportView(records []*event.Record) (*ReportView, error) {
	state, active, err := keyring.StateAt(records)
	if err != nil {
		return nil, err
	}
	view := ReportView{
		Chain:  ReportChain{Position: len(records), ActiveVersion: active},
		Actors: ReportActors{ByStanding: map[string]int{}},
	}
	if n := len(records); n > 0 {
		tip, err := records[n-1].Event.Hash()
		if err != nil {
			return nil, err
		}
		view.Chain.Tip = tip
	}
	for _, fp := range candidateFingerprints(records) {
		e, ok := state.Get(fp)
		if !ok {
			continue
		}
		view.Actors.ByStanding[string(e.Standing)]++
		view.Actors.Total++
		if e.Root {
			view.Actors.Roots++
		}
	}
	hs := halt.StateAt(records)
	view.Halt.Halted = hs.Halted
	view.Halt.By = hs.By
	if hs.Halted {
		for pos, rec := range records {
			if rec.Event.Verb == halt.DeclareVerb {
				p := pos
				view.Halt.DeclaredPosition = &p
			}
		}
	}
	for pos, rec := range records {
		if rec.Event.Verb == checkpointVerb {
			view.Checkpoints.Count++
			p := pos
			view.Checkpoints.LastPosition = &p
		}
	}
	entries := contractEntries(records)
	view.Contracts.Subjects = len(entries)
	for _, e := range entries {
		view.Contracts.Events += len(e.Events)
	}
	if len(entries) > 0 {
		table, err := transition.Default()
		if err != nil {
			return nil, err
		}
		fold := table.FoldRecords(records)
		findings := reconcile.Classify(records, fold)
		if findings == nil {
			findings = []reconcile.Finding{}
		}
		rec := &ReportReconciliation{Findings: findings, ByClass: map[string]int{},
			EvidenceGrade: "seed reconcile (attested heads, target rewrites; spec/reconciliation.md)"}
		for _, f := range findings {
			rec.ByClass[f.Class]++
		}
		view.Reconciliation = rec
		metrics := flywheel.Derive(records, fold)
		view.Flywheel = &metrics
		view.Lanes = lanesSection(records, fold)
		view.Lanes.ByKind = lanesByKind(records, fold)
	}
	if curation.Fold(records).Any() {
		stages := DeriveKnowledge(records).Stages
		view.Knowledge = &stages
	}
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	folded := table.FoldRecords(records)
	if reqs := folded.Requests(); len(reqs) > 0 {
		view.Requests = requestsSection(reqs, records)
	}
	if ads := adaptersSection(folded); len(ads) > 0 {
		view.Adapters = ads
	}
	view.Topology = reportTopology(deriveTopology(records, table, folded))
	return &view, nil
}

// requestsSection counts the requests the fold applied and measures
// the answer latency from the two records' own timestamps: elapsed
// time, never a position difference (the obligation projection's
// posture on age).
func requestsSection(reqs []transition.IngressFact, records []*event.Record) *ReportRequests {
	sec := &ReportRequests{ByKind: map[string]int{}, ByOutcome: map[string]int{}}
	var total float64
	answered := 0
	for _, r := range reqs {
		sec.Total++
		sec.ByKind[r.Kind]++
		if r.Answered == nil {
			sec.Unanswered++
			sec.ByOutcome["pending"]++
			continue
		}
		sec.ByOutcome[r.Outcome]++
		if *r.Answered < len(records) {
			filed, err1 := time.Parse(time.RFC3339, r.TS)
			at, err2 := time.Parse(time.RFC3339, records[*r.Answered].Event.TS)
			if err1 == nil && err2 == nil {
				secs := at.Sub(filed).Seconds()
				if secs < 0 {
					secs = 0
				}
				total += secs
				answered++
			}
		}
	}
	if answered > 0 {
		mean := fmt.Sprintf("%.1f", total/float64(answered))
		sec.MeanAnswerSeconds = &mean
	}
	return sec
}

// lanesSection derives the lane-quality metrics from the fold
// (plans/os-6bd9ffff.md D6): re-triage over subjects with a
// specification, unedited approvals over the measured ones.
func lanesSection(records []*event.Record, fold *transition.Fold) *ReportLanes {
	sec := &ReportLanes{}
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok {
			continue
		}
		if s.Specifications >= 1 {
			sec.Dispatcher.Specified++
		}
		if s.Specifications >= 2 {
			sec.Dispatcher.Respecified++
		}
		if _, approved := fold.PlanApproved(subject); approved {
			sec.Planner.Approvals++
			unedited, measured := fold.PlanDigests(subject).Unedited()
			switch {
			case !measured:
				sec.Planner.Unmeasured++
			case unedited:
				sec.Planner.Unedited++
			default:
				sec.Planner.Edited++
			}
		}
	}
	sec.Dispatcher.RetriageRate = reportRate(sec.Dispatcher.Respecified, sec.Dispatcher.Specified)
	sec.Planner.UneditedRate = reportRate(sec.Planner.Unedited, sec.Planner.Unedited+sec.Planner.Edited)
	sec.Planner.Strongest = strongestOffered(records, fold)
	return sec
}

// strongestOffered is the tuples scope of the latest AUTHORIZED offer
// that carries one, across every subject: the configurations the
// planner lane last received by policy (spec/ranking.md). The
// tolerant fold records a raw-pushed offer as a fact, so the signer's
// supervise standing is replayed to the offer's own position before
// it counts, the same check the list surface makes (review finding on
// the task PR). Nil when no such offer exists.
func strongestOffered(records []*event.Record, fold *transition.Fold) []tuple.Tuple {
	latest := -1
	var out []tuple.Tuple
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok {
			continue
		}
		for _, o := range s.Offers {
			if len(o.Tuples) == 0 || o.Pos <= latest || !offers.Authorized(records, o) {
				continue
			}
			latest = o.Pos
			out = append([]tuple.Tuple(nil), o.Tuples...)
		}
	}
	return out
}

// reportRate is a ratio as a fixed three-decimal string, null at a
// zero denominator: a rate over nothing is not zero.
func reportRate(num, den int) *string {
	if den == 0 {
		return nil
	}
	s := fmt.Sprintf("%.3f", float64(num)/float64(den))
	return &s
}

func buildReport(records []*event.Record, in Inputs) (map[string][]byte, error) {
	view, err := reportView(records)
	if err != nil {
		return nil, err
	}
	// The knowledge counts are judged at the declared instant where
	// one is declared (plans/os-0d537fbd.md D4): the stale count is
	// the one section field an instant changes.
	if at := instantOf(in); at != nil && view.Knowledge != nil {
		stages := DeriveKnowledgeAt(records, at).Stages
		view.Knowledge = &stages
	}
	if in.Obs != nil {
		section, err := observationSection(records, in)
		if err != nil {
			return nil, err
		}
		view.Observation = section
	}
	if in.Refusals != nil {
		section, err := refusalsSection(in.Refusals)
		if err != nil {
			return nil, err
		}
		view.Refusals = section
	}
	b, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ReportFile: append(b, '\n')}, nil
}

// observationSection classifies every active claim against its own
// fence-keyed stream at the declared as_of. A claim whose stream is
// missing classifies no_data; a predecessor's stream under a dead
// fence can neither revive nor wedge the current claim, because
// StreamFor keys on (holder, fence) exactly.
// refusalsSection derives the affordance-gap metric from the
// declared attempts journal alone: outcome counts over one
// population, refusal breakdowns by code and verb, the
// stamped-position span as context, and refused/(refused+admitted)
// as a fixed four-decimal string. The chain is never the
// denominator (spec/refusals.md; review finding on the plan
// PR: a chain denominator over a refusal-bounded span measures
// ledger traffic, not affordance gaps).
func refusalsSection(j *refusals.Journal) (*ReportRefusals, error) {
	digest, err := j.Digest()
	if err != nil {
		return nil, err
	}
	section := &ReportRefusals{
		Inputs:             ReportRefusalsInputs{Digest: digest, Entries: len(j.Entries)},
		ByCode:             map[string]int{},
		ByVerb:             map[string]int{},
		Rate:               "0.0000",
		BlindRetriesByCode: map[string]int{},
	}
	// The blind-retry pass (plans/os-a9e715dc.md D3): for each refused
	// line with a digest, the same actor's next line on the same
	// subject, in journal order, is a blind retry when it is the same
	// act (the digest), refused with the same code, from the same
	// position. One pass from the tail: the nearest following line
	// per (actor, subject) is remembered as the scan walks toward the
	// head, so a journal of one-off refusals is read once, not once
	// per refusal (review finding on the task PR).
	type attempter struct{ actor, subject string }
	following := map[attempter]int{}
	for i := len(j.Entries) - 1; i >= 0; i-- {
		e := j.Entries[i]
		key := attempter{e.Actor, e.Subject}
		if e.Digest == "" {
			section.Undigested++
		} else if e.Outcome == refusals.OutcomeRefused {
			if n, ok := following[key]; ok {
				next := j.Entries[n]
				if next.Digest == e.Digest && next.Outcome == refusals.OutcomeRefused && next.Code == e.Code && next.Position == e.Position {
					section.BlindRetries++
					section.BlindRetriesByCode[e.Code]++
				}
			}
		}
		following[key] = i
	}
	for _, e := range j.Entries {
		pos, err := strconv.Atoi(e.Position)
		if err != nil {
			return nil, fmt.Errorf("attempt position %q is not numeric", e.Position)
		}
		if section.Span == nil {
			section.Span = &ReportRefusalsSpan{From: pos, To: pos}
		} else {
			if pos < section.Span.From {
				section.Span.From = pos
			}
			if pos > section.Span.To {
				section.Span.To = pos
			}
		}
		switch e.Outcome {
		case refusals.OutcomeAdmitted:
			section.Admitted++
		case refusals.OutcomeRefused:
			section.Refused++
			section.ByCode[e.Code]++
			section.ByVerb[e.Verb]++
		}
	}
	if total := section.Refused + section.Admitted; total > 0 {
		section.Rate = fmt.Sprintf("%.4f", float64(section.Refused)/float64(total))
	}
	return section, nil
}

func observationSection(records []*event.Record, in Inputs) (*ReportObservation, error) {
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	digest, err := in.Digest()
	if err != nil {
		return nil, err
	}
	section := &ReportObservation{
		Inputs: ReportObservationInputs{
			AsOf:               in.AsOf.UTC().Format(time.RFC3339),
			Digest:             digest,
			ExpiryAfterSeconds: int(in.Thresholds.ExpiryAfter / time.Second),
			WedgeAfterSeconds:  int(in.Thresholds.WedgeAfter / time.Second),
		},
		Claims: map[string]obs.Classification{},
	}
	fold := table.FoldRecords(records)
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok || s.State != "in_progress" || s.Claim == nil {
			continue
		}
		stream, _ := in.Obs.StreamFor(s.Claim.Holder, obs.FormatFence(s.Claim.Fence))
		section.Claims[subject] = obs.Classify(stream, in.AsOf, in.Thresholds)
	}
	return section, nil
}

// lanesByKind attributes the lane-quality figures to the roster kind
// of the key that acted (plans/os-0d4f2af3.md D6): specifications to
// the kind of their appender, approvals to their approver's. The kind
// is read from the keyring as it stood at the record's own position,
// folded once over the chain, so a re-enrollment later never rewrites
// an earlier act. Per subject, the dispatcher figure follows the FIRST
// specifier's kind (a subject is counted once) while re-specification
// counts under the kind that re-specified.
func lanesByKind(records []*event.Record, fold *transition.Fold) map[string]*ReportLanesKind {
	out := map[string]*ReportLanesKind{}
	get := func(kind string) *ReportLanesKind {
		if kind == "" {
			kind = "unknown"
		}
		k, ok := out[kind]
		if !ok {
			k = &ReportLanesKind{}
			out[kind] = k
		}
		return k
	}
	ring := keyring.New()
	active := ""
	specifiedBy := map[string]string{}
	respecCounted := map[string]bool{}
	approvedBy := map[string]string{}
	for i, rec := range records {
		if active == "" {
			active = rec.Event.V
			if i == 0 && rec.Event.Verb == "system.genesis" && rec.Event.Subject == "system" {
				var g struct {
					Protocol string `json:"protocol"`
				}
				if json.Unmarshal(rec.Event.Payload, &g) == nil && g.Protocol != "" {
					active = g.Protocol
				}
				ring.SeedGenesis(rec)
			}
		}
		kind := ""
		if e, ok := ring.Get(rec.Event.Actor); ok {
			kind = e.Kind
		} else if ring.IsActiveRoot(rec.Event.Actor) {
			kind = "root"
		}
		switch rec.Event.Verb {
		case "contract.specified":
			s, ok := fold.State(rec.Event.Subject)
			if ok && s.Specifications >= 1 {
				if _, seen := specifiedBy[rec.Event.Subject]; !seen {
					specifiedBy[rec.Event.Subject] = kind
					get(kind).Dispatcher.Specified++
				} else if !respecCounted[rec.Event.Subject] {
					respecCounted[rec.Event.Subject] = true
					get(kind).Dispatcher.Respecified++
				}
			}
		case "plan.approved":
			// The fold keeps the last approval per subject, so the
			// subject is attributed once, to the kind that signed the
			// approval the fold kept (the last one), after the walk.
			if _, approved := fold.PlanApproved(rec.Event.Subject); approved {
				approvedBy[rec.Event.Subject] = kind
			}
		}
		if keyring.Applies(active) {
			_ = ring.Advance(rec)
		}
		if rec.Event.Verb == ledger.UpgradeVerb && rec.Event.Subject == "system" {
			var up struct {
				To string `json:"to"`
			}
			if json.Unmarshal(rec.Event.Payload, &up) == nil {
				active = up.To
			}
		}
	}
	subjects := make([]string, 0, len(approvedBy))
	for subject := range approvedBy {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	for _, subject := range subjects {
		k := get(approvedBy[subject])
		k.Planner.Approvals++
		unedited, measured := fold.PlanDigests(subject).Unedited()
		switch {
		case !measured:
			k.Planner.Unmeasured++
		case unedited:
			k.Planner.Unedited++
		default:
			k.Planner.Edited++
		}
	}
	for _, k := range out {
		k.Dispatcher.RetriageRate = reportRate(k.Dispatcher.Respecified, k.Dispatcher.Specified)
		k.Planner.UneditedRate = reportRate(k.Planner.Unedited, k.Planner.Unedited+k.Planner.Edited)
	}
	return out
}

// adapterBudgets maps a harness to its budget posture; an unknown harness
// is a risk limit, the safe default (plans/os-083112ac.md D2).
var adapterBudgets = map[string]string{
	"local-worktree/v0": "enforced",
	"container/v0":      "enforced",
	"cloud-session/v0":  "risk-limit",
	"remote-worker/v0":  "risk-limit",
}

func adapterBudget(harness string) string {
	if b, ok := adapterBudgets[harness]; ok {
		return b
	}
	return "risk-limit"
}

// adaptersSection derives the per-adapter section from the run.started
// tuples the fold holds: the runs under each harness and its posture.
func adaptersSection(fold *transition.Fold) []ReportAdapter {
	counts := map[string]int{}
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok {
			continue
		}
		for _, st := range s.RunStarts {
			if st.Tuple == nil || st.Tuple.Harness == "" {
				continue
			}
			counts[st.Tuple.Harness]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	harnesses := make([]string, 0, len(counts))
	for h := range counts {
		harnesses = append(harnesses, h)
	}
	sort.Strings(harnesses)
	out := make([]ReportAdapter, 0, len(harnesses))
	for _, h := range harnesses {
		out = append(out, ReportAdapter{Harness: h, Runs: counts[h], Budget: adapterBudget(h)})
	}
	return out
}
