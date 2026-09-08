package ranking

// The resumption (plans/os-29e2fef2.md D1, D3; spec/ranking.md
// "Resume"): after a contract is returned on the forge's word the
// prior submitter is the natural next claimant, and the chain already
// knows its configuration. Resume reads it back: the latest return by
// observation, the submission it returned, the claim window that
// submission closed, the tuple the window's admitted run.started
// declared, and the offer that claim consumed. It is a per-subject
// preference the supervisor and the maintenance pass write into a
// re-offer's tuples scope; like the ranking it is policy, never
// admission, and like the ranking it reads the chain and no clock.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/offers"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
)

// Resumption is what the chain says about the window a return by
// observation closed. Every position is -1 where the link is missing,
// and Because names the first missing link; Tuple is nil until every
// link holds, so a caller that wants the preference reads ok from
// Resume, and a caller that wants the window (the re-offer's scope,
// D3) reads the fields that derived.
type Resumption struct {
	// Return is the latest applied contract.returned on the subject
	// and Observation the check.observed it cited.
	Return      int
	Observation int
	// Submission is the submission.made the return returned, Fence the
	// claim.taken that opened its window, Holder that claim's signer.
	Submission int
	Fence      int
	Holder     string
	// Offer is the offer the claim consumed: the latest offer before
	// the claim that was authorized at its own position and that the
	// holder was eligible for there. Nil where none stood, so a
	// raw-pushed offer nobody granted never lends its scope (D3).
	Offer *transition.OfferFact
	// Tuple is the configuration the window's admitted run.started
	// declared, present only when the holder is active and can still
	// take an offer scoped to it (D1).
	Tuple   *tuple.Tuple
	Because string
}

func (r Resumption) fail(because string) (Resumption, bool) {
	r.Because = because
	return r, false
}

// Resume derives the prior submitter's tuple for a subject returned on
// the forge's word (D1). It refuses by name when the latest return
// cited a verdict (a failed submission routes to the strongest
// configuration, not back to the one that failed), when no return
// stands, when the window carried no admitted run.started or one that
// declared no tuple, when the holder is suspended or revoked, and when
// the holder's admissible claim grant no longer cites the tuple: a
// disqualification removes a tuple while the actor stays active, and
// the listing judges a scoped offer by that set, so a preference for
// such a tuple would be an offer its worker cannot see. The test is
// the listing's own (offers.Eligible against an offer scoped to the
// tuple alone), so the two cannot drift. Record-derived; no clock.
func Resume(records []*event.Record, fold *transition.Fold, subject string) (Resumption, bool) {
	r := Resumption{Return: -1, Observation: -1, Submission: -1, Fence: -1}
	if fold == nil {
		return r.fail("no lifecycle fold to read")
	}
	s, ok := fold.State(subject)
	if !ok {
		return r.fail(fmt.Sprintf("%s is not on the chain", subject))
	}
	if len(s.Returns) == 0 {
		return r.fail(fmt.Sprintf("%s carries no return: nothing to resume", subject))
	}
	last := s.Returns[len(s.Returns)-1]
	r.Return = last.Pos
	if last.Observation < 0 {
		return r.fail(fmt.Sprintf("the latest return on %s (position %d) cited a verdict, not an observation: a failed submission routes by the ranking, not back to the configuration that failed", subject, last.Pos))
	}
	r.Observation = last.Observation
	if s.Submission == nil || s.Submission.Pos >= last.Pos {
		return r.fail(fmt.Sprintf("no submission stands before the return at position %d on %s", last.Pos, subject))
	}
	r.Submission = s.Submission.Pos
	fence, holder, ok := window(records, subject, s.Submission.Pos)
	if !ok {
		return r.fail(fmt.Sprintf("the submission at position %d names no claim window on %s", s.Submission.Pos, subject))
	}
	r.Fence, r.Holder = fence, holder
	r.Offer = consumed(records, s, fence, holder)
	// The start is the first BOUNDARY-VALID one at the fence: the
	// tolerant fold keeps a raw-pushed start too, and a raw start
	// before the legitimate one would otherwise name an attacker's
	// configuration, so each candidate is judged by the run rule's own
	// derivation (admit.RunStartValid) against the prefix it appended
	// onto. It is the supervisor's act on the holder's window, so the
	// signer is not the holder and is not matched.
	table, err := transition.Default()
	if err != nil {
		return r.fail(fmt.Sprintf("the transition table does not load: %v", err))
	}
	var start *transition.RunStartFact
	for i := range s.RunStarts {
		if s.RunStarts[i].Fence == fence && admit.RunStartValid(records, table, subject, s.RunStarts[i]) {
			start = &s.RunStarts[i]
			break
		}
	}
	if start == nil {
		return r.fail(fmt.Sprintf("fence %d on %s carries no admitted run.started, so the window declared no configuration", fence, subject))
	}
	if start.Tuple == nil {
		return r.fail(fmt.Sprintf("the run.started at position %d on %s declared no tuple", start.Pos, subject))
	}
	ring, _, err := keyring.StateAt(records)
	if err != nil || ring == nil {
		return r.fail(fmt.Sprintf("the keyring does not derive: %v", err))
	}
	e, ok := ring.Get(holder)
	if !ok || e.Standing != keyring.StandingActive {
		standing := "unknown"
		if ok {
			standing = string(e.Standing)
		}
		return r.fail(fmt.Sprintf("the holder %s is %s: a preference nobody active can take is an offer nobody can take", holder, standing))
	}
	probe := transition.OfferFact{Capabilities: []string{keyring.CapClaim}, Tuples: []tuple.Tuple{*start.Tuple}}
	if !offers.Eligible(ring, holder, s.Tier, probe) {
		return r.fail(fmt.Sprintf("the holder %s can no longer take an offer scoped to the tuple it declared at position %d: its admissible claim grant does not cite it", holder, start.Pos))
	}
	t := *start.Tuple
	r.Tuple = &t
	return r, true
}

// window reads the claim window a submission.made closed: the fence
// its payload cites and that claim's signer, the reading
// internal/admit applies to the same field.
func window(records []*event.Record, subject string, submission int) (fence int, holder string, ok bool) {
	if submission < 0 || submission >= len(records) || records[submission] == nil {
		return 0, "", false
	}
	var p struct {
		Fence string `json:"fence"`
	}
	if err := json.Unmarshal(records[submission].Event.Payload, &p); err != nil {
		return 0, "", false
	}
	fence, err := strconv.Atoi(strings.TrimSpace(p.Fence))
	if err != nil || fence < 0 || fence >= len(records) || records[fence] == nil {
		return 0, "", false
	}
	claim := records[fence].Event
	if claim.Verb != "claim.taken" || claim.Subject != subject {
		return 0, "", false
	}
	return fence, claim.Actor, true
}

// consumed is the offer the claim at fence consumed (D3): the latest
// offer on the subject before the claim whose signer held the
// supervise boundary at its own position and whose scopes the holder
// met at the claim's position. The fold keeps every well-shaped offer,
// raw pushes included, and the listing makes an unauthorized one inert
// only at listing time, so the derivation applies the listing's two
// predicates rather than copying the latest folded fact.
func consumed(records []*event.Record, s transition.SubjectState, fence int, holder string) *transition.OfferFact {
	if fence <= 0 || fence > len(records) {
		return nil
	}
	ring, _, err := keyring.StateAt(records[:fence])
	if err != nil || ring == nil {
		return nil
	}
	for i := len(s.Offers) - 1; i >= 0; i-- {
		o := s.Offers[i]
		if o.Pos >= fence || !offers.Authorized(records, o) || !offers.Eligible(ring, holder, s.Tier, o) {
			continue
		}
		return &o
	}
	return nil
}
