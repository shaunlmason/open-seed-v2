// Package offers is the one derivation of who may take what
// (plans/os-f0ae2cdf.md step 3; spec/offers.md): the eligibility
// an offer's scopes place on a polling worker, the position-accurate
// supervise check on the offer's signer, the live rows a poll lists,
// and the advisory wake bridge over readiness deltas. `seed offer
// list`, the report and the bridge read the same functions, so they
// agree by construction; none of them grants anything, because the
// claim settles at admission like any claim.
package offers

import (
	"fmt"
	"sort"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
)

// Eligible applies the offer's scopes to the polling worker: every
// scoped capability must be held, with operator standing satisfying
// every scope (a root's implicit operator included), since scopes
// describe the taking lane and admission already lets the operator act
// everywhere in it, so hiding offers from operators would let them
// claim work they cannot discover. The subject's filed tier must be in
// the scoped tier set. A scoped tuple set is met by a worker whose
// claim grants cite one of its members, per field
// (plans/os-8e53ffd9.md D6). Empty scopes match any active worker, any
// tier, any configuration.
func Eligible(ring *keyring.State, fp, tier string, o transition.OfferFact) bool {
	if ring == nil {
		return false
	}
	for _, c := range o.Capabilities {
		if !ring.HasAnyCapability(fp, []string{c, keyring.CapOperator}) {
			return false
		}
	}
	if len(o.Tiers) > 0 {
		found := false
		for _, t := range o.Tiers {
			if t == tier {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(o.Tuples) > 0 && !ring.HasAnyCapability(fp, []string{keyring.CapOperator}) {
		found := false
		for _, cited := range ring.GrantTuples(fp, keyring.CapClaim) {
			for _, want := range o.Tuples {
				if cited.Equal(want) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Authorized replays the keyring to the offer's own position and
// checks the supervise boundary retroactively: the tolerant fold
// records raw pushes, so a foreign offer folds as a fact and must be
// inert at every consuming surface (spec/offers.md "Foreign
// offers are inert").
func Authorized(records []*event.Record, o transition.OfferFact) bool {
	if o.Pos < 0 || o.Pos >= len(records) {
		return false
	}
	ring, _, err := keyring.StateAt(records[:o.Pos])
	return err == nil && ring != nil &&
		ring.HasAnyCapability(o.Signer, keyring.AcceptedCapabilities(transition.OfferPublishedVerb))
}

// Row is one live, eligible offer as a poll lists it.
type Row struct {
	Subject      string        `json:"subject"`
	Position     string        `json:"position"`
	Tier         string        `json:"tier,omitempty"`
	Capabilities []string      `json:"capabilities,omitempty"`
	Tiers        []string      `json:"tiers,omitempty"`
	Tuples       []tuple.Tuple `json:"tuples,omitempty"`
	Expires      string        `json:"expires"`
}

// Live lists the offers a polling worker may take at the instant: the
// actor active, the subject effectively ready (plans/os-f0ae2cdf.md
// D4: a dependent that waits or a descendant that is held lists
// nothing, its stored offers kept until it becomes effectively ready),
// the offer live, its signer authorized at its position, and its
// scopes met. The fold's own liveness (a ready subject, an unexpired
// offer, no claim after it) is LiveOffers'.
func Live(records []*event.Record, d *topology.Derived, ring *keyring.State, actor string, now time.Time) []Row {
	rows := []Row{}
	if ring == nil || d == nil || d.Lifecycle() == nil {
		return rows
	}
	e, ok := ring.Get(actor)
	if !ok || e.Standing != keyring.StandingActive {
		return rows
	}
	fold := d.Lifecycle()
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok || !d.EffectiveReady(subject) {
			continue
		}
		for _, o := range s.LiveOffers(now) {
			if !Eligible(ring, actor, s.Tier, o) || !Authorized(records, o) {
				continue
			}
			rows = append(rows, Row{Subject: subject, Position: fmt.Sprintf("%d", o.Pos), Tier: s.Tier,
				Capabilities: o.Capabilities, Tiers: o.Tiers, Tuples: o.Tuples, Expires: o.Expires})
		}
	}
	return rows
}

// Channel is the advisory wake seam, the executor adapter's Wake
// (executor: "its total failure costs latency, never
// correctness").
type Channel interface {
	Wake(actor string) error
}

// Channels maps an actor fingerprint to the channel its registered
// adapter offers; an actor with no entry has no channel and is never
// woken, which loses it nothing but latency.
type Channels map[string]Channel

// Candidate is one subject the delta found newly claimable, with the
// active actors eligible for a live offer on it.
type Candidate struct {
	Subject string   `json:"subject"`
	Actors  []string `json:"actors"`
}

// Woken is one wake attempted: the actor, the subject that occasioned
// it, and the error the channel returned, if any, reported and never
// acted on.
type Woken struct {
	Actor   string `json:"actor"`
	Subject string `json:"subject"`
	Error   string `json:"error,omitempty"`
}

// Result is what one bridge pass found and did.
type Result struct {
	Since       int         `json:"since"`
	Position    int         `json:"position"`
	BecameReady []string    `json:"became_ready"`
	Candidates  []Candidate `json:"candidates"`
	Woken       []Woken     `json:"woken"`
}

// Bridge is the supervisor's advisory wake over a readiness delta
// (plans/os-f0ae2cdf.md D6): the subjects effectively ready at the tip
// and not at the caller's last observed position, each matched to the
// active actors eligible for a live authorized offer on it, and one
// Wake per such actor that has a channel. A wake error is reported
// and changes nothing; a subject still held or waiting is no
// candidate; wakes may repeat across retries, since they are advisory
// and readiness stays ledger-derived and idempotent.
func Bridge(records []*event.Record, table *transition.Table, ring *keyring.State, now time.Time, since int, channels Channels) Result {
	if since < 0 {
		since = 0
	}
	if since > len(records) {
		since = len(records)
	}
	res := Result{Since: since, Position: len(records), BecameReady: []string{}, Candidates: []Candidate{}, Woken: []Woken{}}
	if table == nil {
		return res
	}
	before := topology.DeriveRecords(records[:since], table)
	after := topology.DeriveRecords(records, table)
	res.BecameReady = topology.ReadinessDelta(before, after)
	if ring == nil {
		return res
	}
	actors := ring.Actors()
	sort.Strings(actors)
	fold := after.Lifecycle()
	for _, subject := range res.BecameReady {
		s, ok := fold.State(subject)
		if !ok {
			continue
		}
		c := Candidate{Subject: subject, Actors: []string{}}
		for _, fp := range actors {
			e, ok := ring.Get(fp)
			if !ok || e.Standing != keyring.StandingActive {
				continue
			}
			eligible := false
			for _, o := range s.LiveOffers(now) {
				if Eligible(ring, fp, s.Tier, o) && Authorized(records, o) {
					eligible = true
					break
				}
			}
			if !eligible {
				continue
			}
			c.Actors = append(c.Actors, fp)
			if ch, has := channels[fp]; has && ch != nil {
				w := Woken{Actor: fp, Subject: subject}
				if err := ch.Wake(fp); err != nil {
					w.Error = err.Error()
				}
				res.Woken = append(res.Woken, w)
			}
		}
		if len(c.Actors) > 0 {
			res.Candidates = append(res.Candidates, c)
		}
	}
	return res
}
