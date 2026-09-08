// The contract-detail projection (plans/os-fecfb3f7.md step 1): every
// work subject with its full event stream. The v0 work classifier is a
// prefix rule: verbs outside the system.* and actor.* governance
// namespaces are work vocabulary; Phase 5's transition table replaces
// the rule with explicit vocabulary (the spec and decision log say so).
// One file, not per-subject files: subjects are opaque strings, and
// mapping them to paths would trade the engine's path safety for an
// encoding scheme nothing consumes yet; the 4.3 cache is the
// lookup-throughput answer.

package project

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// ContractsFile is the contract-detail projection's one view file.
const ContractsFile = "contracts.json"

// ContractEvent is one work event in a contract's stream. Actor is the
// signer fingerprint; Payload is the canonical payload verbatim.
type ContractEvent struct {
	Position int             `json:"position"`
	Verb     string          `json:"verb"`
	Actor    string          `json:"actor"`
	Payload  json.RawMessage `json:"payload"`
}

// ContractEntry is one work subject's stream, in chain order. State
// is the transition table's folded lifecycle state (null for a
// subject no lifecycle event ever validly created), and Anomalies
// counts the lifecycle events the table refused: tolerated in history
// per the cooperative posture, skipped by the fold, surfaced here,
// never silent (plans/os-d69a6c91.md).
type ContractEntry struct {
	Subject      string                `json:"subject"`
	State        *string               `json:"state"`
	Anomalies    int                   `json:"anomalies"`
	Claim        *ContractClaim        `json:"claim,omitempty"`
	Racing       *ContractRacing       `json:"racing,omitempty"`
	Acceptance   *ContractAcceptance   `json:"acceptance,omitempty"`
	Verdict      *ContractVerdict      `json:"verdict"`
	Requested    *string               `json:"requested"`
	Merged       *ContractMerge        `json:"merged"`
	Sealed       *ContractSealed       `json:"sealed"`
	Override     *ContractOverride     `json:"override"`
	Offers       []ContractOffer       `json:"offers,omitempty"`
	LastClaim    *string               `json:"last_claim,omitempty"`
	Budget       *ContractBudget       `json:"budget,omitempty"`
	Reservations []ContractReservation `json:"reservations,omitempty"`
	RunStarts    []ContractRunStart    `json:"run_starts,omitempty"`
	Runs         []ContractRun         `json:"runs,omitempty"`
	Interrupts   []ContractInterrupt   `json:"interrupts,omitempty"`
	// Topology is the contract's graph view (plans/os-f0ae2cdf.md D8;
	// spec/topology.md), present only when the prefix carries a
	// trusted relation fact, so relation-free chains keep byte-identical
	// bodies.
	Topology      *ContractTopology `json:"topology,omitempty"`
	FirstPosition int               `json:"first_position"`
	LastPosition  int               `json:"last_position"`
	Events        []ContractEvent   `json:"events"`
}

// ContractBudget is the derived budget view (plans/os-cecac5de.md;
// spec/budgets.md): the filed class, its table capacity, and
// remaining = capacity − open valid reservations − settled actuals.
// Serialized only when the subject carries budget facts, so
// budget-inactive chains keep byte-identical v9 bodies; a class
// missing from the table serializes capacity and remaining as -1,
// stated never fudged.
type ContractBudget struct {
	Class     string `json:"class"`
	Capacity  int    `json:"capacity"`
	Remaining int    `json:"remaining"`
}

// ContractReservation is one folded budget.reserve with its effective
// close, if any: attempts by anyone else are recorded in the chain
// but never shown as closure (the derivation, not mutation, posture).
type ContractReservation struct {
	Position string         `json:"position"`
	Signer   string         `json:"signer"`
	Amount   int            `json:"amount"`
	Closed   *ContractClose `json:"closed,omitempty"`
}

// ContractClose is a reservation's effective close: the position, the
// kind (settle or release), and settled actuals.
type ContractClose struct {
	Position string `json:"position"`
	Kind     string `json:"kind"`
	Actuals  int    `json:"actuals"`
}

// ContractRunStart is one folded run.started (plans/os-1dad487d.md;
// spec/executors.md): the gated spend initiation fencing a run
// to its reservation. Omitted when a subject has no run facts, so
// run-free chains keep byte-identical views.
type ContractRunStart struct {
	Position    string `json:"position"`
	Signer      string `json:"signer"`
	Fence       string `json:"fence"`
	Reservation string `json:"reservation"`
}

// ContractInterrupt is one folded run.interrupted
// (plans/os-0f718b4e.md): the supervisor's safe-point preemption
// request on the fence. Omitted when a subject has none, so
// interrupt-free chains keep byte-identical views.
type ContractInterrupt struct {
	Position string `json:"position"`
	Signer   string `json:"signer"`
	Fence    string `json:"fence"`
}

// ContractRun is one folded run.settled: the once-per-fence metering
// aggregate. Telemetry, never authority — budget.settle carries the
// actuals.
type ContractRun struct {
	Position string `json:"position"`
	Signer   string `json:"signer"`
	Fence    string `json:"fence"`
	Units    int    `json:"units"`
	Lines    int    `json:"lines"`
}

// ContractAcceptance is the folded acceptance spec: the artifact
// anchor, the executable flag, and whether gate evidence bound to the
// revision is present (or not required) — "may this spec run?" is a
// projection read Phase 6's verifier consumes (plans/os-73c00a50.md).
type ContractAcceptance struct {
	Ref        string `json:"ref"`
	Executable bool   `json:"executable"`
	Gated      bool   `json:"gated"`
}

// ContractVerdict is the fold's latest rendered verdict on a subject
// (plans/os-6cdc15be.md): the chain position (string per the envelope
// position convention), the pass/fail literal, and the cited receipt
// digest, so the reconciliation chain is independently checkable
// against the published view.
type ContractVerdict struct {
	Position string `json:"position"`
	Verdict  string `json:"verdict"`
	Receipt  string `json:"receipt"`
	// Independence is the level the verdict recorded
	// (plans/os-99829835.md D5), verbatim from the payload; empty on
	// a fact that carried none.
	Independence string `json:"independence,omitempty"`
}

// ContractMerge is the admitted merge.observed: its chain position
// and the merged commit the observer recorded.
type ContractMerge struct {
	Position string `json:"position"`
	SHA      string `json:"sha"`
}

// ContractSealed is the sealed-checks commitment fact
// (plans/os-3128535a.md): the chain position that proves the checks
// predate implementation and the salted hash the ciphertext verifies
// against. Explicit null when absent, the v6 chain-field convention.
type ContractSealed struct {
	Position   string `json:"position"`
	Commitment string `json:"commitment"`
}

// ContractOverride is the current window's operator override
// (plans/os-d2497eb7.md): its chain position and required reason,
// shown under its own name, never as a verdict. Explicit null when
// absent, the chain-field convention.
type ContractOverride struct {
	Position string `json:"position"`
	Reason   string `json:"reason"`
}

// ContractOffer is one folded offer.published (plans/os-c61c3392.md;
// spec/offers.md): the chain position (string per the envelope
// position convention), the publishing signer, the eligibility scopes
// (empty means unscoped), and the RFC3339 expiry. Facts, not
// liveness: the offer list surface derives claimed-or-expire liveness
// and validates the signer's supervise boundary at this position.
// Omitted when a subject has no offers, so offer-free chains keep
// byte-identical views.
type ContractOffer struct {
	Position     string   `json:"position"`
	Signer       string   `json:"signer"`
	Capabilities []string `json:"capabilities,omitempty"`
	Tiers        []string `json:"tiers,omitempty"`
	Expires      string   `json:"expires"`
}

// ContractClaim is the active claim while a subject is in_progress:
// the holder's fingerprint and the fence (the admitted claim.taken
// position, string per the envelope position convention), so
// contention answers and stale-fence refusals are independently
// checkable against the published view (plans/os-5dc16a7c.md). Absent
// outside a claim window.
type ContractClaim struct {
	Holder string `json:"holder"`
	Fence  string `json:"fence"`
}

// ContractRacing is the race on a racing squad's contract
// (plans/os-56bee171.md D4): every active claim by fence order, and
// after the first verified success the settling position and the
// claims it settled out. Absent on a subject that never raced, so
// every existing view stays byte-identical.
type ContractRacing struct {
	Racers     []ContractClaim `json:"racers"`
	SettledAt  *string         `json:"settled_at,omitempty"`
	SettledOut []ContractClaim `json:"settled_out,omitempty"`
}

// Contracts returns the contract-detail projection. Version "2" added
// the folded state and anomaly count; Version "3" the claim object;
// Version "4" the acceptance field; Version "5" the fold's seed/1
// activation boundary (pre-activation records inert); Version "6" the
// reconciliation-chain facts (verdict, requested, merged;
// plans/os-6cdc15be.md); Version "7" the sealed-checks commitment
// (plans/os-3128535a.md); Version "8" the operator override
// (plans/os-d2497eb7.md); Version "9" the offer facts and the
// last-claim consumption boundary (plans/os-c61c3392.md), both
// omitted on offer-free, never-claimed subjects; Version "10" the
// derived budget view and reservations (plans/os-cecac5de.md),
// omitted on budget-inactive subjects; Version "11" the
// execution-run facts (plans/os-1dad487d.md), omitted on run-free
// subjects; Version "12" the safe-point interrupts
// (plans/os-0f718b4e.md), omitted on interrupt-free subjects; Version
// "13" the verdict's independence level (plans/os-99829835.md), which
// changes the bytes of every prefix carrying a verdict — each
// republishing under a new build id via the version-in-identity
// machinery.
func Contracts() Projection {
	return Projection{Name: "contracts", Version: "15", Build: buildContracts}
}

// isWorkVerb is the v0 classifier: everything outside the governance
// namespaces contributes to contract detail, keyed by subject.
func isWorkVerb(verb string) bool {
	return !strings.HasPrefix(verb, "system.") && !strings.HasPrefix(verb, "actor.")
}

func contractEntries(records []*event.Record) []*ContractEntry {
	bySubject := map[string]*ContractEntry{}
	var order []*ContractEntry
	for pos, rec := range records {
		e := &rec.Event
		if !isWorkVerb(e.Verb) {
			continue
		}
		entry, ok := bySubject[e.Subject]
		if !ok {
			entry = &ContractEntry{Subject: e.Subject, FirstPosition: pos}
			bySubject[e.Subject] = entry
			order = append(order, entry)
		}
		entry.LastPosition = pos
		entry.Events = append(entry.Events, ContractEvent{
			Position: pos,
			Verb:     e.Verb,
			Actor:    e.Actor,
			Payload:  e.Payload,
		})
	}
	return order
}

func buildContracts(records []*event.Record, _ Inputs) (map[string][]byte, error) {
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	fold := table.FoldRecords(records)
	graph := deriveTopology(records, table, fold)
	budgetOf := func(subject string, s transition.SubjectState) (*ContractBudget, []ContractReservation) {
		if len(s.Reservations) == 0 && len(s.BudgetCloses) == 0 {
			return nil, nil
		}
		view := admit.BudgetViewAt(records, table, subject, s)
		b := &ContractBudget{Class: view.Class, Capacity: -1, Remaining: -1}
		if view.Known {
			b.Capacity, b.Remaining = view.Capacity, view.Remaining
		}
		var out []ContractReservation
		for _, r := range s.Reservations {
			row := ContractReservation{Position: fmt.Sprintf("%d", r.Pos), Signer: r.Signer, Amount: r.Amount}
			if c, ok := view.ClosedBy[r.Pos]; ok {
				row.Closed = &ContractClose{Position: fmt.Sprintf("%d", c.Pos), Kind: c.Kind, Actuals: c.Actuals}
			}
			out = append(out, row)
		}
		return b, out
	}
	entries := contractEntries(records)
	out := make([]ContractEntry, 0, len(entries))
	for _, e := range entries {
		if s, ok := fold.State(e.Subject); ok {
			e.Anomalies = s.Anomalies
			if s.State != "" {
				st := s.State
				e.State = &st
			}
			if s.Claim != nil {
				e.Claim = &ContractClaim{Holder: s.Claim.Holder, Fence: fmt.Sprintf("%d", s.Claim.Fence)}
			}
			// The racing object rides every subject that raced, claim
			// standing or none: the racers while it runs, the settling
			// position and the settled-out claims after, an empty
			// racer list when every racer has left.
			if s.Racing {
				race := &ContractRacing{Racers: []ContractClaim{}}
				for _, c := range s.Claims {
					race.Racers = append(race.Racers, ContractClaim{Holder: c.Holder, Fence: fmt.Sprintf("%d", c.Fence)})
				}
				if s.RaceSettled != nil {
					at := fmt.Sprintf("%d", *s.RaceSettled)
					race.SettledAt = &at
					race.SettledOut = race.Racers
					race.Racers = []ContractClaim{}
				}
				e.Racing = race
			}
			if s.Acceptance != nil {
				e.Acceptance = &ContractAcceptance{Ref: s.Acceptance.Ref, Executable: s.Acceptance.Executable, Gated: s.Acceptance.Gated}
			}
			if s.Verdict != nil {
				e.Verdict = &ContractVerdict{Position: fmt.Sprintf("%d", s.Verdict.Pos), Verdict: s.Verdict.Verdict, Receipt: s.Verdict.Receipt,
					Independence: s.Verdict.Independence}
			}
			if s.Requested != nil {
				pos := fmt.Sprintf("%d", s.Requested.Pos)
				e.Requested = &pos
			}
			if s.Sealed != nil {
				e.Sealed = &ContractSealed{Position: fmt.Sprintf("%d", s.Sealed.Pos), Commitment: s.Sealed.Commitment}
			}
			if s.Override != nil {
				e.Override = &ContractOverride{Position: fmt.Sprintf("%d", s.Override.Pos), Reason: s.Override.Reason}
			}
			if s.Merged != nil {
				e.Merged = &ContractMerge{Position: fmt.Sprintf("%d", s.Merged.Pos), SHA: s.Merged.SHA}
			}
			for _, o := range s.Offers {
				e.Offers = append(e.Offers, ContractOffer{
					Position:     fmt.Sprintf("%d", o.Pos),
					Signer:       o.Signer,
					Capabilities: o.Capabilities,
					Tiers:        o.Tiers,
					Expires:      o.Expires,
				})
			}
			// The consumption boundary is exposed only beside offer
			// facts: an ever-claimed, offer-free subject keeps its v8
			// body byte-identical (the plan's compatibility promise);
			// the fold keeps the boundary internally either way.
			if len(s.Offers) > 0 && len(s.PriorClaimants) > 0 {
				lc := fmt.Sprintf("%d", s.LastClaim)
				e.LastClaim = &lc
			}
			e.Budget, e.Reservations = budgetOf(e.Subject, s)
			e.Topology = contractTopology(graph, e.Subject)
			for _, st := range s.RunStarts {
				e.RunStarts = append(e.RunStarts, ContractRunStart{
					Position:    fmt.Sprintf("%d", st.Pos),
					Signer:      st.Signer,
					Fence:       fmt.Sprintf("%d", st.Fence),
					Reservation: fmt.Sprintf("%d", st.Reservation),
				})
			}
			for _, r := range s.Runs {
				e.Runs = append(e.Runs, ContractRun{
					Position: fmt.Sprintf("%d", r.Pos),
					Signer:   r.Signer,
					Fence:    fmt.Sprintf("%d", r.Fence),
					Units:    r.Units,
					Lines:    r.Lines,
				})
			}
			for _, it := range s.Interrupts {
				e.Interrupts = append(e.Interrupts, ContractInterrupt{
					Position: fmt.Sprintf("%d", it.Pos),
					Signer:   it.Signer,
					Fence:    fmt.Sprintf("%d", it.Fence),
				})
			}
		}
		out = append(out, *e)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ContractsFile: append(b, '\n')}, nil
}
