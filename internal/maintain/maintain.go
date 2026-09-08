// Package maintain is the unattended maintenance loop
// (plans/os-8a5f14bb.md; SEED-NEXT.md conformance III.J): reap
// expired or wedged claims, reconcile divergence, rebuild
// projections, checkpoint, and lint — runnable with no scheduler and
// no wake channel, and audited as an ordinary actor.
//
// The decision logic lives here with its effects injected, so every
// rule below is drillable without a ledger. What the lane may
// actually DO is not decided here at all: every act it takes is
// signed with the maintenance key and crosses the same admission
// boundary as anyone else's, which is what "audited as an ordinary
// actor" has to mean if it is to mean anything.
package maintain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/externalfact"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
)

// Corroboration is the LEDGER-side half of a reap's evidence: facts at
// known chain positions saying the holder was asked to stop.
//
// It exists as its own type because the reap rule turns on the two
// halves being independent, and a type you have to fill in separately
// from the classification makes that structural rather than hoped for.
type Corroboration struct {
	// Interrupted: an admitted run.interrupted stands on the active
	// fence with no deliberate exit after it. The "no exit after it"
	// half needs no scan: every deliberate exit leaves in_progress,
	// and a reap is admissible only FROM in_progress on that same
	// fence, so a window still standing on the interrupted fence has
	// had no exit by construction.
	Interrupted bool
	// Wedged: an admitted wedge.declared stands on the active fence.
	Wedged bool
	// Revoked: the active claim's holder has been revoked
	// (admit.RevokedHolder). Unlike Interrupted and Wedged this is a
	// LEDGER fact rather than a stream signal — a revoked holder
	// provably cannot exit its window — so it corroborates a reap in
	// every classification state, no_data included
	// (plans/os-32d06c65.md D1, D5).
	Revoked bool
}

// Stands reports whether any corroborating fact was found.
func (c Corroboration) Stands() bool { return c.Interrupted || c.Wedged || c.Revoked }

// Reapable is the whole reap rule, and it is deliberately pure.
//
// A reap answers an UNANSWERED REQUEST, never a timeout. The
// observation channel is declared ephemeral and lossy
// (spec/observations.md), so a dropped stream and dead work look
// identical from outside and silence alone can never reap. The
// corroboration that makes a reap honest is a ledger fact that the
// holder was asked to stop and did not — which is exactly the force
// path spec/executors.md names this loop as the consumer of:
// "a worker that ignores its interrupt is killed... B-style automatic
// timeout reaping is the Phase 9 maintenance loop's job; it
// presupposes exactly these semantics."
//
// So a reap means "someone asked, and nothing happened", not "long
// enough has passed". That is the only corroboration a channel
// declared lossy can support, and it is why there is no threshold
// here to tune.
//
// no_data carries NO reap path whatever, however old the claim: a
// stream that holds nothing at all is the absence of evidence, and
// this is where the instinct to reap is strongest and the evidence
// weakest.
func Reapable(c obs.Classification, corr Corroboration) (bool, string) {
	// A revocation is a ledger fact, not a stream classification: a
	// revoked holder can no longer act on its claim, so the claim is
	// reapable in EVERY classification state, no_data included — the
	// one place a no_data stream is reaped, and only because the chain,
	// not the channel, corroborates it (plans/os-32d06c65.md D5).
	if corr.Revoked {
		return true, ""
	}
	switch c.State {
	case obs.NoData:
		return false, "the stream holds nothing at all, so there is no evidence either way — absence of data is stated, never read as death (spec/observations.md)"
	case obs.Live:
		return false, fmt.Sprintf("the stream is live (last observation %s)", c.LastObservation)
	case obs.Expired, obs.Wedged:
	default:
		return false, fmt.Sprintf("unclassified stream state %q", c.State)
	}
	if !corr.Stands() {
		return false, fmt.Sprintf("the stream is %s, but nothing in the chain says the holder was asked to stop: silence is not a request nobody answered, and the channel is lossy by design", c.State)
	}
	return true, ""
}

// Reap is one decided reap, carrying the evidence that decided it.
type Reap struct {
	Subject string `json:"subject"`
	Fence   int    `json:"fence"`
	Holder  string `json:"holder"`
	State   string `json:"state"`
	Because string `json:"because"`
}

// Skip is one claim the pass looked at and did NOT reap, with the
// reason. Skips are reported rather than dropped: a maintenance loop
// that silently declines is indistinguishable from one that never
// ran, and an operator reading the report is owed the difference.
type Skip struct {
	Subject string `json:"subject"`
	State   string `json:"state"`
	Because string `json:"because"`
}

// Filing is one defect contract the pass filed for a lint finding.
type Filing struct {
	Subject string `json:"subject"`
	Class   string `json:"class"`
	Filed   string `json:"filed"`
}

// Refusal is an act the boundary declined. The loop holds no private
// powers, so its acts can be refused like anyone else's, and a refused
// act is reported rather than retried or worked around.
type Refusal struct {
	Verb    string `json:"verb"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// Observed is one forge observation the pass recorded
// (plans/os-0cd18799.md D6): the subject, the pull request and head,
// and the three literals; never a line of forge prose.
type Observed struct {
	Subject string `json:"subject"`
	PR      string `json:"pr"`
	Head    string `json:"head"`
	Checks  string `json:"checks"`
	Threads *int   `json:"unresolved_threads,omitempty"`
	Review  string `json:"review"`
}

// Returned is one submission the pass returned to the queue on the
// forge's word: the subject, the observation cited and its head, and
// how many observation-cited returns the subject now carries.
type Returned struct {
	Subject     string `json:"subject"`
	Observation int    `json:"observation"`
	Head        string `json:"head"`
	Returns     int    `json:"returns"`
}

// Escalated is one subject the pass froze at the return ceiling
// (D7): the count that reached it and the ceiling itself.
type Escalated struct {
	Subject string `json:"subject"`
	Returns int    `json:"returns"`
	Ceiling int    `json:"ceiling"`
}

// Reoffered is one subject the pass re-offered after returning it
// (plans/os-29e2fef2.md D3): the tuple the re-offer is scoped to and
// the holder it was declared by, absent where the window declared
// none the holder can still take, and the expiry the offer carries.
type Reoffered struct {
	Subject string       `json:"subject"`
	Tuple   *tuple.Tuple `json:"tuple,omitempty"`
	Holder  string       `json:"holder,omitempty"`
	Expires string       `json:"expires"`
}

// Report is what one pass did.
type Report struct {
	Reaped     []Reap                 `json:"reaped"`
	Observed   []Observed             `json:"observed"`
	Returned   []Returned             `json:"returned"`
	Escalated  []Escalated            `json:"escalated"`
	Reoffered  []Reoffered            `json:"reoffered"`
	Skipped    []Skip                 `json:"skipped"`
	Findings   []reconcile.Finding    `json:"findings"`
	Filed      []Filing               `json:"filed"`
	Rebuilt    []string               `json:"rebuilt"`
	Checkpoint *checkpoint.Checkpoint `json:"checkpoint,omitempty"`
	Refusals   []Refusal              `json:"refusals"`
}

// DefaultReturnCeiling bounds the observation-cited returns one
// subject may carry before the pass escalates instead
// (plans/os-0cd18799.md D7): SEED-NEXT.md §II.13's "max revisions" on
// the contract loop.
const DefaultReturnCeiling = 3

// DefaultReofferTTL is how long a re-offer the pass publishes stays
// live (plans/os-29e2fef2.md D3): a declared duration added to the
// append's own instant. An expired re-offer is the supervisor's to
// renew, because the pass returns work and does not run the queue.
const DefaultReofferTTL = 24 * time.Hour

// Deps is everything the pass reads and everything it does. The
// effects are injected so the rules above are drillable without a
// ledger, which is the shape internal/covergate established
// (plans/os-cafba959.md): a rule living in a CLI verb is a
// correctness claim nothing can check.
type Deps struct {
	// Now is the DECLARED as-of instant the classification is judged
	// at. It is a parameter rather than a clock read for the same
	// reason obs.Classify takes one: a pass must be reproducible, and
	// a maintenance loop that consulted a wall clock could not be
	// replayed.
	Now time.Time
	// StaleAfter is how long past its expiry a lesson may stand,
	// unrevalidated and unretired, before the lesson_stale lint files
	// it (plans/os-0d537fbd.md D5); zero files on expiry itself.
	StaleAfter time.Duration
	Records    []*event.Record
	Table      *transition.Table
	Fold       *transition.Fold
	Obs        *obs.Snapshot
	Thresholds obs.Thresholds
	Store      *artifact.Store
	Repo       string
	// Unseal opens a sealed subject's checks under the maintenance
	// actor's identity, for the L3 reproduction the evidence grade
	// runs (plans/os-99829835.md D5); a subject it cannot open is
	// reported skipped with the reason, never passed over silently.
	Unseal func(s transition.SubjectState) (*verdict.SealedInput, error)
	// Obligations are the rows the obligation projection derived. The
	// unsettled-run lint CONSUMES these; re-deriving the anchoring
	// here would put it in two places (D2).
	Obligations []obligation.Row
	// Observe reads what the forge says about a pull request's head
	// (plans/os-0cd18799.md D6). Nil means no forge is configured: the
	// observe step then reports every observable submission skipped
	// with that reason, never silently, and CI runs that way.
	Observe func(pr string) (externalfact.Observation, error)
	// Refresh re-reads the ledger between steps that append and steps
	// that read what was appended: the return cites the observation
	// the pass just recorded, and the re-offer reads the return the
	// pass just appended (plans/os-29e2fef2.md D3), rather than the
	// opening view's. Nil means the opening records, fold and
	// obligations stand throughout.
	Refresh func() ([]*event.Record, *transition.Fold, []obligation.Row, error)
	// ReturnCeiling bounds the observation-cited returns one subject
	// may carry before the pass escalates instead (D7); zero means
	// DefaultReturnCeiling.
	ReturnCeiling int
	// ReofferTTL is how long past its own instant a re-offer stays
	// live (plans/os-29e2fef2.md D3); zero means DefaultReofferTTL.
	ReofferTTL time.Duration
	// Instant is the effect that chooses a re-offer's instant, read
	// once per re-offer and handed to both the payload and the record
	// through AppendAt, so the offer's expires and the record's ts
	// derive from one reading and the offer cannot be born dead. Nil
	// means the wall clock, at second precision.
	Instant func() time.Time
	// AppendAt signs and appends one act at the instant given, the
	// re-offer's seam (plans/os-29e2fef2.md D3). Nil means the
	// re-offer step is skipped and reported.
	AppendAt func(at time.Time, verb, subject string, payload []byte) error

	// Corroborate answers the ledger half of the reap rule for one
	// subject's active fence. Injected because the derivation belongs
	// to internal/admit, which owns "did this fact pass the boundary
	// at its own position" for every other fact too.
	Corroborate func(subject string, fence int) Corroboration
	// Append signs and appends one act. A refusal comes back as an
	// error and is reported, never retried.
	Append func(verb, subject string, payload []byte) error
	// File files a defect contract for a finding and returns the id
	// it filed under.
	File func(f reconcile.Finding) (string, error)
	// Rebuild rebuilds the projections and names what it rebuilt.
	Rebuild func() ([]string, error)
	// Materialize returns the canonical snapshot bytes and the
	// position they materialize.
	Materialize func() (body []byte, position int, err error)
}

// Run executes one pass in the fixed order: reap, observe, return,
// reoffer, lint, file, rebuild, checkpoint. The observations come
// before the lints so the lints read fresh facts, the return before
// the filing so a returned subject is not also filed as a finding,
// the re-offer right after the return so what the pass returned is
// claimable again before the pass ends (plans/os-29e2fef2.md D3), and
// the checkpoint is last, because it attests to the state the rest of
// the pass produced (plans/os-0cd18799.md D6).
func Run(d Deps) (Report, error) {
	rep := Report{
		Reaped: []Reap{}, Observed: []Observed{}, Returned: []Returned{}, Escalated: []Escalated{},
		Reoffered: []Reoffered{}, Skipped: []Skip{}, Findings: []reconcile.Finding{},
		Filed: []Filing{}, Rebuilt: []string{}, Refusals: []Refusal{},
	}
	d.reap(&rep)
	d.observe(&rep)
	if err := d.returnRed(&rep); err != nil {
		return rep, err
	}
	if err := d.reoffer(&rep); err != nil {
		return rep, err
	}
	rep.Findings = append(rep.Findings, d.lint(&rep)...)
	d.file(&rep)
	if err := d.rebuild(&rep); err != nil {
		return rep, err
	}
	return rep, d.checkpoint(&rep)
}

func (d Deps) reap(rep *Report) {
	if d.Fold == nil || d.Obs == nil {
		return
	}
	for _, id := range d.Fold.Subjects() {
		s, ok := d.Fold.State(id)
		if !ok || s.Claim == nil {
			continue
		}
		// A settled race (plans/os-56bee171.md D3): the first verified
		// success closed the contract while other racers still held
		// claims. The ledger itself corroborates the reap — nothing a
		// settled-out racer does can land except its own exit — so
		// each remaining claim is reaped with a packet naming the
		// settlement, and the losers' work is visibly over.
		if s.RaceSettled != nil {
			for _, c := range s.Claims {
				payload, err := RaceReapPacket(s, c.Fence, *s.RaceSettled)
				if err != nil {
					rep.Refusals = append(rep.Refusals, Refusal{Verb: "claim.reaped", Subject: id, Reason: err.Error()})
					continue
				}
				if d.Append == nil {
					continue
				}
				if err := d.Append("claim.reaped", id, payload); err != nil {
					rep.Refusals = append(rep.Refusals, Refusal{Verb: "claim.reaped", Subject: id, Reason: err.Error()})
					continue
				}
				rep.Reaped = append(rep.Reaped, Reap{
					Subject: id, Fence: c.Fence, Holder: c.Holder,
					State: "settled", Because: fmt.Sprintf("the race was settled at position %d and this claim outlived it", *s.RaceSettled),
				})
			}
			continue
		}
		if s.State != "in_progress" {
			continue
		}
		// Every active claim is classified on its own stream: one on
		// an exclusive subject, each racer's on a racing one.
		for _, c := range s.Claims {
			stream, _ := d.Obs.StreamFor(c.Holder, obs.FormatFence(c.Fence))
			class := obs.Classify(stream, d.Now, d.Thresholds)
			var corr Corroboration
			if d.Corroborate != nil {
				corr = d.Corroborate(id, c.Fence)
			}
			ok2, because := Reapable(class, corr)
			if !ok2 {
				rep.Skipped = append(rep.Skipped, Skip{Subject: id, State: string(class.State), Because: because})
				continue
			}
			payload, err := ReapPacket(s, c.Fence, class, corr)
			if err != nil {
				rep.Refusals = append(rep.Refusals, Refusal{Verb: "claim.reaped", Subject: id, Reason: err.Error()})
				continue
			}
			if d.Append == nil {
				continue
			}
			if err := d.Append("claim.reaped", id, payload); err != nil {
				rep.Refusals = append(rep.Refusals, Refusal{Verb: "claim.reaped", Subject: id, Reason: err.Error()})
				continue
			}
			rep.Reaped = append(rep.Reaped, Reap{
				Subject: id, Fence: c.Fence, Holder: c.Holder,
				State: string(class.State), Because: reapBecause(corr),
			})
		}
	}
}

// observe records what the forge says about every submission under
// review that names a pull request (plans/os-0cd18799.md D6): one
// check.observed per subject whose observation differs from the
// standing one, a skip with its reason for every subject it cannot
// or need not observe, a refusal reported rather than retried.
func (d Deps) observe(rep *Report) {
	if d.Fold == nil {
		return
	}
	for _, id := range d.Fold.Subjects() {
		s, ok := d.Fold.State(id)
		if !ok || s.State != "review" || s.Submission == nil || s.Submission.PR == "" {
			continue
		}
		pr := s.Submission.PR
		if d.Observe == nil {
			rep.Skipped = append(rep.Skipped, Skip{Subject: id, State: "review",
				Because: fmt.Sprintf("no forge is configured, so %s is not observed — pass --forge to poll it", pr)})
			continue
		}
		o, err := d.Observe(pr)
		if err != nil {
			rep.Skipped = append(rep.Skipped, Skip{Subject: id, State: "review",
				Because: fmt.Sprintf("reading %s from the forge: %v", pr, err)})
			continue
		}
		fact := transition.CheckFact{PR: pr, Head: o.Head, Checks: o.Checks, Threads: o.UnresolvedThreads, Review: o.Review}
		if s.Observation != nil && s.Observation.Same(fact) {
			rep.Skipped = append(rep.Skipped, Skip{Subject: id, State: "review",
				Because: fmt.Sprintf("the observation at position %d already says exactly this about %s — an unchanged poll appends nothing", s.Observation.Pos, pr)})
			continue
		}
		payload, err := ObservationPayload(pr, o)
		if err != nil {
			rep.Refusals = append(rep.Refusals, Refusal{Verb: transition.CheckObservedVerb, Subject: id, Reason: err.Error()})
			continue
		}
		if d.Append == nil {
			continue
		}
		if err := d.Append(transition.CheckObservedVerb, id, payload); err != nil {
			rep.Refusals = append(rep.Refusals, Refusal{Verb: transition.CheckObservedVerb, Subject: id, Reason: err.Error()})
			continue
		}
		rep.Observed = append(rep.Observed, Observed{Subject: id, PR: pr, Head: o.Head, Checks: o.Checks, Threads: o.UnresolvedThreads, Review: o.Review})
	}
}

// ObservationPayload renders check.observed's strict object from the
// forge's answer: literals and a count, the thread field absent where
// the forge cannot say. One renderer for the pass and the verb, so
// the two cannot disagree about the shape.
func ObservationPayload(pr string, o externalfact.Observation) ([]byte, error) {
	out := map[string]any{"pr": pr, "head": o.Head, "checks": o.Checks, "review": o.Review}
	if o.UnresolvedThreads != nil {
		out["unresolved_threads"] = *o.UnresolvedThreads
	}
	return json.Marshal(out)
}

// returnRed returns every submission the forge's latest word says is
// not mergeable (plans/os-0cd18799.md D6, D7): for each
// submission.unmergeable row the fresh view carries, a
// contract.returned citing the observation, unless the subject has
// reached the return ceiling, in which case an escalation carrying
// the packet, the observation and one decision freezes it instead.
func (d Deps) returnRed(rep *Report) error {
	fold, rows := d.Fold, d.Obligations
	if d.Refresh != nil {
		_, fresh, freshRows, err := d.Refresh()
		if err != nil {
			return err
		}
		fold, rows = fresh, freshRows
	}
	if fold == nil {
		return nil
	}
	ceiling := d.ReturnCeiling
	if ceiling <= 0 {
		ceiling = DefaultReturnCeiling
	}
	for _, row := range rows {
		if row.Kind != obligation.KindSubmissionUnmergeable {
			continue
		}
		s, ok := fold.State(row.Subject)
		if !ok || s.Observation == nil || s.State != "review" {
			continue
		}
		if d.Append == nil {
			continue
		}
		if AtCeiling(s, ceiling) {
			payload, err := CeilingPacket(d.Records, row.Subject, s, ceiling)
			if err != nil {
				rep.Refusals = append(rep.Refusals, Refusal{Verb: "escalation.raised", Subject: row.Subject, Reason: err.Error()})
				continue
			}
			if err := d.Append("escalation.raised", row.Subject, payload); err != nil {
				rep.Refusals = append(rep.Refusals, Refusal{Verb: "escalation.raised", Subject: row.Subject, Reason: err.Error()})
				continue
			}
			rep.Escalated = append(rep.Escalated, Escalated{Subject: row.Subject, Returns: ReturnsByObservation(s), Ceiling: ceiling})
			continue
		}
		payload, _ := json.Marshal(map[string]string{"observation": strconv.Itoa(s.Observation.Pos)})
		if err := d.Append(transition.ContractReturnedVerb, row.Subject, payload); err != nil {
			rep.Refusals = append(rep.Refusals, Refusal{Verb: transition.ContractReturnedVerb, Subject: row.Subject, Reason: err.Error()})
			continue
		}
		rep.Returned = append(rep.Returned, Returned{Subject: row.Subject, Observation: s.Observation.Pos, Head: s.Observation.Head, Returns: ReturnsByObservation(s) + 1})
	}
	return nil
}

// reoffer publishes a fresh offer on every subject the pass returned
// in this pass (plans/os-29e2fef2.md D3), so the unattended loop does
// not stall on a subject no worker's poll can see: after a return the
// subject is ready with no live offer, and the poll lists nothing.
// The scope is the resumption's (internal/ranking.Resume): the prior
// submitter's tuple where the holder can still take it, the consumed
// offer's capabilities and tiers, one instant for the payload and the
// record. One re-offer per return, in the pass that returned it: a
// second pass over the same subject returns nothing and so re-offers
// nothing, and an expired re-offer is the supervisor's to renew.
func (d Deps) reoffer(rep *Report) error {
	if len(rep.Returned) == 0 {
		return nil
	}
	records, fold := d.Records, d.Fold
	if d.Refresh != nil {
		freshRecords, fresh, _, err := d.Refresh()
		if err != nil {
			return err
		}
		records, fold = freshRecords, fresh
	}
	if fold == nil {
		return nil
	}
	ttl := d.ReofferTTL
	if ttl <= 0 {
		ttl = DefaultReofferTTL
	}
	for _, ret := range rep.Returned {
		s, ok := fold.State(ret.Subject)
		if !ok || s.State != "ready" {
			rep.Skipped = append(rep.Skipped, Skip{Subject: ret.Subject, State: s.State,
				Because: "the returned subject is no longer ready, so the pass does not re-offer it"})
			continue
		}
		r, _ := ranking.Resume(records, fold, ret.Subject)
		if r.Observation != ret.Observation {
			// The latest return on the chain is not the one this pass
			// appended: the re-offer is one per return, never a
			// re-offer of someone else's.
			rep.Skipped = append(rep.Skipped, Skip{Subject: ret.Subject, State: s.State,
				Because: fmt.Sprintf("the latest return on the chain does not cite the observation at position %d this pass returned on, so the pass does not re-offer it", ret.Observation)})
			continue
		}
		if d.AppendAt == nil {
			rep.Skipped = append(rep.Skipped, Skip{Subject: ret.Subject, State: s.State,
				Because: "no append-at effect is wired, so the returned subject is not re-offered"})
			continue
		}
		at := time.Now().UTC().Truncate(time.Second)
		if d.Instant != nil {
			at = d.Instant().UTC().Truncate(time.Second)
		}
		payload, row, err := Reoffer(ret.Subject, s, r, ttl, at)
		if err != nil {
			rep.Refusals = append(rep.Refusals, Refusal{Verb: transition.OfferPublishedVerb, Subject: ret.Subject, Reason: err.Error()})
			continue
		}
		if err := d.AppendAt(at, transition.OfferPublishedVerb, ret.Subject, payload); err != nil {
			rep.Refusals = append(rep.Refusals, Refusal{Verb: transition.OfferPublishedVerb, Subject: ret.Subject, Reason: err.Error()})
			continue
		}
		rep.Reoffered = append(rep.Reoffered, row)
	}
	return nil
}

// Reoffer renders the offer.published payload the pass publishes on a
// returned subject (plans/os-29e2fef2.md D3), purely from the
// subject's state, its resumption and the instant it is handed: the
// consumed offer's capabilities and tiers, or `[claim]` and the
// subject's filed tier where no offer stood; the prior submitter's
// tuple alone where it derived, else the consumed offer's own tuple
// scope, so the re-offer is never wider than what the claim consumed;
// expires at the instant plus the ttl, the same instant the record is
// signed at. Refused: a non-positive ttl (a born-dead offer invites
// nothing) and a subject that is not ready.
func Reoffer(subject string, s transition.SubjectState, r ranking.Resumption, ttl time.Duration, at time.Time) ([]byte, Reoffered, error) {
	if ttl <= 0 {
		return nil, Reoffered{}, fmt.Errorf("a re-offer needs a positive ttl, got %s: an offer must expire strictly after its own instant", ttl)
	}
	if s.State != "ready" {
		return nil, Reoffered{}, fmt.Errorf("a re-offer invites claims on a ready subject, and this one folds to %q", s.State)
	}
	capabilities := []string{keyring.CapClaim}
	var tiers []string
	if s.Tier != "" {
		tiers = []string{s.Tier}
	}
	var tuples []tuple.Tuple
	if r.Offer != nil {
		capabilities = append([]string(nil), r.Offer.Capabilities...)
		tiers = append([]string(nil), r.Offer.Tiers...)
		tuples = append([]tuple.Tuple(nil), r.Offer.Tuples...)
	}
	row := Reoffered{Subject: subject, Expires: at.Add(ttl).UTC().Format(time.RFC3339)}
	if r.Tuple != nil {
		t := *r.Tuple
		tuples = []tuple.Tuple{t}
		row.Tuple, row.Holder = &t, r.Holder
	}
	type eligibility struct {
		Capabilities []string      `json:"capabilities,omitempty"`
		Tiers        []string      `json:"tiers,omitempty"`
		Tuples       []tuple.Tuple `json:"tuples,omitempty"`
	}
	payload, err := json.Marshal(struct {
		Eligibility eligibility `json:"eligibility"`
		Expires     string      `json:"expires"`
	}{eligibility{Capabilities: capabilities, Tiers: tiers, Tuples: tuples}, row.Expires})
	if err != nil {
		return nil, Reoffered{}, err
	}
	return payload, row, nil
}

// ReturnsByObservation counts the subject's applied returns that
// cited an observation rather than a verdict: the quantity the
// ceiling bounds (plans/os-0cd18799.md D7). Verdict-cited returns are
// the verifier's routing and do not count.
func ReturnsByObservation(s transition.SubjectState) int {
	n := 0
	for _, r := range s.Returns {
		if r.Observation >= 0 {
			n++
		}
	}
	return n
}

// AtCeiling reports whether the next red return would exceed the
// ceiling: with the default of three, the fourth red observation
// escalates rather than returning. The ceiling is a declared
// threshold, never a clock: rounds are counted on the chain.
func AtCeiling(s transition.SubjectState, ceiling int) bool {
	if ceiling <= 0 {
		ceiling = DefaultReturnCeiling
	}
	return ReturnsByObservation(s) >= ceiling
}

// CeilingPacket composes the escalation.raised payload the pass
// appends at the ceiling (D7): a packet naming the standing
// observation's position and head and the count, and one decision
// with three answers. Literals and counts only, never forge prose.
func CeilingPacket(records []*event.Record, subject string, s transition.SubjectState, ceiling int) ([]byte, error) {
	if s.Observation == nil || s.Submission == nil {
		return nil, fmt.Errorf("no observation stands on %s", subject)
	}
	acceptance := "the contract's acceptance spec, which the fold does not carry"
	if s.Acceptance != nil && s.Acceptance.Ref != "" {
		acceptance = s.Acceptance.Ref
	}
	base := packet.ZeroRange
	if s.Submission.Pos >= 0 && s.Submission.Pos < len(records) {
		if p, err := packet.FromPayload(subject, records[s.Submission.Pos].Event.Payload); err == nil && p.Base != "" {
			base = p.Base
		}
	}
	threads := "threads unknown"
	if s.Observation.Threads != nil {
		threads = fmt.Sprintf("%d unresolved thread(s)", *s.Observation.Threads)
	}
	returns := ReturnsByObservation(s)
	body, err := packet.Render(packet.Packet{
		Acceptance: []string{acceptance},
		Decisions:  []packet.Decision{},
		Base:       base,
		Refs:       []string{},
		Findings: []packet.Finding{{
			Tried:   fmt.Sprintf("returned %d time(s) on the forge's word, the ceiling being %d", returns, ceiling),
			Outcome: fmt.Sprintf("the observation at position %d reports checks %s, %s, review %s on head %s — red again, and the maintenance lane returns no more", s.Observation.Pos, s.Observation.Checks, threads, s.Observation.Review, s.Observation.Head),
		}},
	})
	if err != nil {
		return nil, err
	}
	question, err := json.Marshal(map[string]any{
		"question": fmt.Sprintf("%s has been returned %d time(s) on the forge's word and is red again: raise the ceiling, cancel, or merge by hand?", s.Submission.PR, returns),
		"options": []map[string]string{
			{"id": "raise-ceiling", "choice": "raise the return ceiling and let the maintenance lane keep returning it"},
			{"id": "cancel", "choice": "cancel the contract"},
			{"id": "merge-by-hand", "choice": "the operator merges by hand and observes the merge"},
		},
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		packet.Key:   json.RawMessage(body),
		"escalation": json.RawMessage(question),
	})
}

// RaceReapPacket composes the claim.reaped payload for a settled-out
// racer (plans/os-56bee171.md D3): the fence it kills and a packet
// whose finding names the settlement, so the loser's packet says why
// its work is over without inventing what it found.
func RaceReapPacket(s transition.SubjectState, fence int, settledAt int) ([]byte, error) {
	acceptance := "the contract's acceptance spec, which the fold does not carry"
	if s.Acceptance != nil && s.Acceptance.Ref != "" {
		acceptance = s.Acceptance.Ref
	}
	body, err := packet.Render(packet.Packet{
		Acceptance: []string{acceptance},
		Decisions:  []packet.Decision{},
		Base:       packet.ZeroRange,
		Refs:       []string{},
		Findings: []packet.Finding{{
			Tried:   "racing this contract against another claim",
			Outcome: fmt.Sprintf("the race was settled at position %d by the first verified success; this claim outlived it and was reaped by the maintenance lane, its work written off as the squad's declared cost", settledAt),
		}},
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"fence":    strconv.Itoa(fence),
		packet.Key: json.RawMessage(body),
	})
}

func reapBecause(corr Corroboration) string {
	switch {
	case corr.Revoked:
		return "the claim's holder was revoked and can no longer act on it, so its open claim is reaped on the revocation alone"
	case corr.Interrupted:
		return "an admitted run.interrupted on the active fence went unanswered"
	default:
		return "an admitted wedge.declared stands on the active fence"
	}
}

// ReapPacket composes the whole claim.reaped payload: the fence the
// reap closes, and the honest four-part packet a forced reap leaves
// behind "from what is known" (spec/executors.md) — acceptance
// from the contract's specified criteria, base as the zero-length
// range because no pushed work is known, and findings recording the
// ignored request and the reap. Nothing here is invented: where the
// loop does not know, it says so.
//
// The FENCE citation is not decoration. claim.reaped is a
// claim-scoped event, and the fence rule refuses one that does not
// cite the active window — which is how a reap aimed at a window that
// already closed is refused rather than landing on whatever claim
// stands now. The first draft of this function returned the bare
// packet and every reap refused at the boundary; the drill that
// caught it is the one that read the chain back instead of the
// report.
func ReapPacket(s transition.SubjectState, fence int, c obs.Classification, corr Corroboration) ([]byte, error) {
	acceptance := "the contract's acceptance spec, which the fold does not carry"
	if s.Acceptance != nil && s.Acceptance.Ref != "" {
		acceptance = s.Acceptance.Ref
	}
	tried := "the holder was asked to stop by an admitted run.interrupted on this fence"
	switch {
	case corr.Revoked:
		tried = "the holder's key was revoked, so it can no longer act on this claim and the open window is reaped on the revocation alone"
	case !corr.Interrupted:
		tried = "the holder's run was declared wedged on this fence"
	}
	outcome := fmt.Sprintf("no deliberate exit followed and the stream classified %s (last observation %q, last advance %q, count %d) — reaped by the maintenance lane, and the run's actuals settle afterward",
		c.State, c.LastObservation, c.LastAdvance, c.Count)
	body, err := packet.Render(packet.Packet{
		Acceptance: []string{acceptance},
		Decisions:  []packet.Decision{},
		Base:       packet.ZeroRange,
		Refs:       []string{},
		Findings:   []packet.Finding{{Tried: tried, Outcome: outcome}},
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"fence":    strconv.Itoa(fence),
		packet.Key: json.RawMessage(body),
	})
}

// lint runs the CLOSED finding set: the record-derived classes, the
// evidence-grade classes that need the artifact store and the
// repository, and the one class this card adds. Closed means a new
// lint lands by adding a class WITH the spec that pairs it to its
// fact; an open-ended list would make this loop a policy surface,
// which is what "audited as an ordinary actor" denies.
func (d Deps) lint(rep *Report) []reconcile.Finding {
	if d.Fold == nil {
		return nil
	}
	out := reconcile.Classify(d.Records, d.Fold)
	// The evidence-grade half. A pass built on Classify alone reports
	// clean over a rewritten target or an unretrievable receipt —
	// green, and omitting exactly the divergence this loop is
	// chartered to reconcile (D2.5).
	if d.Store != nil && d.Repo != "" {
		// The chain and the fold ride along so the L3 reproduction
		// runs here as it does under `seed reconcile` (review finding
		// on the item 3 task PR: a wrapper passing nil disabled it for
		// every maintenance pass); what the actor's key cannot open is
		// a skip the report carries.
		repro := reconcile.Reproduction{Records: d.Records, Fold: d.Fold, Unseal: d.Unseal,
			NotAttempted: func(subject, why string) {
				state := ""
				if s, ok := d.Fold.State(subject); ok {
					state = s.State
				}
				rep.Skipped = append(rep.Skipped, Skip{Subject: subject, State: state,
					Because: "the L3 verdict's receipt was not reproduced: " + why})
			}}
		for _, id := range d.Fold.Subjects() {
			if s, ok := d.Fold.State(id); ok {
				out = append(out, reconcile.EvidenceAt(id, s, d.Store, d.Repo, repro)...)
			}
		}
	}
	out = append(out, reconcile.Unsettled(d.Obligations)...)
	// The stale half reads the declared instant, the one clock a pass
	// has (plans/os-0d537fbd.md D5): what nobody revalidated or
	// retired becomes work, never a retirement the loop performs.
	out = append(out, reconcile.LessonsStale(curation.Fold(d.Records), d.Now, d.StaleAfter)...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Class < out[j].Class
	})
	return out
}

// file turns each finding into a FILED DEFECT CONTRACT, never an
// escalation. The distinction is real: an escalation freezes a
// contract and demands a human decision, while a lint finding is work
// somebody should do.
//
// The consequence is worth stating rather than burying: this loop can
// therefore create work, which is authority. It is bounded by being
// attributable — its own key, its own lane — and by filing nothing but
// contracts, since it cannot claim what it files.
func (d Deps) file(rep *Report) {
	if d.File == nil {
		return
	}
	for _, f := range rep.Findings {
		id, err := d.File(f)
		if err != nil {
			rep.Refusals = append(rep.Refusals, Refusal{Verb: "intent.filed", Subject: f.Subject, Reason: err.Error()})
			continue
		}
		rep.Filed = append(rep.Filed, Filing{Subject: f.Subject, Class: f.Class, Filed: id})
	}
}

func (d Deps) rebuild(rep *Report) error {
	if d.Rebuild == nil {
		return nil
	}
	names, err := d.Rebuild()
	if err != nil {
		return err
	}
	rep.Rebuilt = append(rep.Rebuilt, names...)
	return nil
}

// checkpoint materializes the canonical projection state, stores it
// retrievably, and appends the signed citation.
//
// A checkpoint that carried only a signature over a hash would let
// every other criterion in this pass pass while the checkpoint itself
// was unusable: a reader could confirm somebody attested to a state it
// has no way to obtain, and would replay anyway. So the snapshot is
// written FIRST and the event names where it is and what it hashes to.
func (d Deps) checkpoint(rep *Report) error {
	if d.Materialize == nil || d.Store == nil || d.Append == nil {
		return nil
	}
	body, position, err := d.Materialize()
	if err != nil {
		return err
	}
	digest, err := d.Store.Put(body)
	if err != nil {
		return err
	}
	payload, err := checkpoint.Payload(digest, position)
	if err != nil {
		return err
	}
	if err := d.Append(checkpoint.Verb, "seed/0", payload); err != nil {
		rep.Refusals = append(rep.Refusals, Refusal{Verb: checkpoint.Verb, Subject: "seed/0", Reason: err.Error()})
		return nil
	}
	c, err := checkpoint.Parse("seed/0", payload)
	if err != nil {
		return err
	}
	rep.Checkpoint = c
	return nil
}

// DefectID is the id a finding files under: a stable hash of the class
// and the subject, prefixed so a filed defect is recognizable as one.
//
// Deriving it rather than allocating a fresh id makes filing
// IDEMPOTENT through the ledger itself: a second pass over the same
// standing finding re-files the same subject and the boundary refuses
// the duplicate. The alternative is for the loop to remember what it
// filed, and a maintenance loop that remembers is a maintenance loop
// that can forget — which on this surface means filing the same defect
// once per pass, forever.
func DefectID(f reconcile.Finding) string {
	sum := sha256.Sum256([]byte(f.Class + "\x00" + f.Subject))
	return "d-" + hex.EncodeToString(sum[:8])
}
