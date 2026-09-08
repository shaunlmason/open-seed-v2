package simulate

// The seven-day bar, audited from the ledger alone (plans/os-16e55c11.md
// D5): the simulation reconstructs and justifies every claim from the
// chain, never from its own bookkeeping. Each bar names the records
// that violate it; a clean run leaves every list empty.

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// AuditResult is the five bars III.R holds a run to, each an evidence
// list rather than a boolean, so a violation names itself.
type AuditResult struct {
	ChainViolations    []string `json:"chain_violations"`
	LostUpdates        []string `json:"lost_updates"`
	SilentAbandonments []string `json:"silent_abandonments"`
	GuardrailBreaches  []string `json:"guardrail_breaches"`
	UnreservedSpend    []string `json:"unreserved_spend"`
	Clean              bool     `json:"clean"`
	// Declared reports whether the guardrail bar's ceiling arm judged
	// under a declaration with guardrails (plans/os-b5051f2e.md D5): a
	// reading that says clean says whether the ceiling was among the
	// things it read.
	Declared bool `json:"declared"`
}

// ClaimTakenVerb is the one verb the bars read that the protocol
// publishes no constant for (plans/os-b86dab4c.md D1).
const ClaimTakenVerb = "claim.taken"

// AuditedVerbs are the verbs the bars switch on. Each must be one the
// boundary drafts: a bar that counts a verb the protocol never emits
// is a bar that never fires, which is how the unreserved-spend bar
// came to count "budget.reserved" against a protocol that emits
// budget.reserve. The drill holds this list to admit.CatalogVerbs
// (D3), not to the transition table, whose Verbs() is the lifecycle
// subset and carries neither budget.reserve nor run.started. The
// deliberate exits are absent because the audit no longer names them:
// transition.IsExit is their one authority (D2).
var AuditedVerbs = []string{
	transition.OfferPublishedVerb,
	ClaimTakenVerb,
	transition.BudgetReserveVerb,
	transition.RunStartedVerb,
}

// Audit reconstructs the five bars from the records alone: the reading
// every caller had before the ceiling arm existed, byte for byte.
func Audit(records []*event.Record) AuditResult {
	return AuditUnder(records, nil)
}

// AuditUnder reconstructs the five bars from the records, judging the
// guardrail bar's ceiling arm under the deployment declaration when
// one is given (plans/os-b5051f2e.md D1). The ceiling is admission
// policy rather than chain validity, read from seed.json and never
// carried by the ledger, so a chain alone cannot show it; a caller
// that hands the audit the declaration admission read gets the
// reading admission would have given. A nil declaration is the
// records-only audit, exactly as admission with no declaration is a
// no-op.
func AuditUnder(records []*event.Record, declaration *posture.Config) AuditResult {
	res := AuditResult{
		ChainViolations:    []string{},
		LostUpdates:        []string{},
		SilentAbandonments: []string{},
		GuardrailBreaches:  []string{},
		UnreservedSpend:    []string{},
	}

	// Chain violations: the admitted chain must fold without an illegal
	// transition. StateAt replays every record; a raw-pushed illegal
	// transition surfaces as a fold error.
	tbl, err := transition.Default()
	if err != nil {
		res.ChainViolations = append(res.ChainViolations, "no transition table: "+err.Error())
	} else if _, foldErr := foldAll(tbl, records); foldErr != nil {
		res.ChainViolations = append(res.ChainViolations, foldErr.Error())
	}

	// Lost updates: the materialized chain is the tip, so every record
	// read is present; the bar is that the chain is non-empty and its
	// prev-links are contiguous (a gap is a lost update). event.Record
	// verification on materialize already rejects a broken link, so a
	// non-empty chain that materialized is contiguous by construction;
	// an empty chain is itself a lost-everything.
	if len(records) == 0 {
		res.LostUpdates = append(res.LostUpdates, "the chain is empty")
	}

	// Per-subject: silent abandonment (a window opened by claim.taken
	// and never closed by a deliberate exit) and unreserved spend.
	type window struct {
		open   bool
		starts []int
	}
	subj := map[string]*window{}
	get := func(s string) *window {
		w := subj[s]
		if w == nil {
			w = &window{}
			subj[s] = w
		}
		return w
	}
	for pos, rec := range records {
		s := rec.Event.Subject
		switch rec.Event.Verb {
		case ClaimTakenVerb:
			// A claim rides a published offer in the scheduling model
			// (SEED-NEXT.md II.9), but admission does not require one:
			// its claim arms are authoring isolation and the lifecycle
			// transition, and nothing there reads the subject's offers.
			// So an unoffered claim is not a guardrail breach; the bar
			// reports what the boundary refuses, and the scheduling
			// concern is internal/eval's ready-with-no-live-offers read
			// (plans/os-aaec6a3c.md D1, D3).
			w := get(s)
			w.open = true
		case transition.BudgetReserveVerb:
			// Read below, against the protocol's own rule rather than a
			// boolean kept here (plans/os-88df7ab2.md D1).
			get(s)
		case transition.RunStartedVerb:
			w := get(s)
			w.starts = append(w.starts, pos)
		}
		// The deliberate exits are the protocol's, not a copy of them
		// (D2): transition.IsExit names the four verbs that legally end
		// an in_progress window.
		if transition.IsExit(rec.Event.Verb) {
			get(s).open = false
		}
	}
	res.GuardrailBreaches = append(res.GuardrailBreaches, sealedAuthorClaims(tbl, records)...)
	res.GuardrailBreaches = append(res.GuardrailBreaches, ceilingClaims(tbl, records, declaration)...)
	res.Declared = declaration != nil && declaration.Guardrails != nil

	// A subject still open (claim taken, no deliberate exit) is a silent
	// abandonment; a done contract is closed by construction.
	for s, w := range subj {
		if w.open {
			res.SilentAbandonments = append(res.SilentAbandonments, s)
		}
	}
	starts := make(map[string][]int, len(subj))
	for s, w := range subj {
		starts[s] = w.starts
	}
	res.UnreservedSpend = append(res.UnreservedSpend, unreservedSpend(tbl, records, starts)...)

	res.Clean = len(res.ChainViolations) == 0 && len(res.LostUpdates) == 0 &&
		len(res.SilentAbandonments) == 0 && len(res.GuardrailBreaches) == 0 &&
		len(res.UnreservedSpend) == 0
	return res
}

// unreservedSpend names every subject whose run was not fenced to an
// open, valid reservation of its own (plans/os-88df7ab2.md D1, D7).
//
// A start is fenced only if BOTH halves hold, because the fold and
// admission answer different questions:
//
//   - The fold recorded it. transition records a RunStartFact only
//     where the payload named a fence and a reservation it could read,
//     so a run.started the fold did not record cited nothing checkable
//     and is spend with no fence at all (D7). Counting records against
//     facts is what keeps a malformed raw start visible.
//   - admit.RunStartValid accepts it. A start cites ONE reservation
//     (RunStartFact.Reservation) and that predicate judges the citation
//     at the start's own position: the strict payload, the fence
//     against the active claim, the cited reservation's validity, and
//     BudgetViewAt proving it was not already closed there. Asking
//     instead "was some reservation open" is weaker than the protocol
//     and misses the fencing the bar exists to check: a start citing a
//     closed or absent reservation passes it whenever an unrelated
//     reservation is open (review finding on plan #309).
//
// The cost is the protocol's too, and it is not linear: each
// RunStartValid replays the keyring and the table over the start's
// prefix and derives a budget view there, so a chain of n records with
// r runs and k reservations costs about O(r*k*n) per subject (D6).
//
// Where the table could not be built the bar reports nothing: the
// chain-violation arm has already named that, and a bar cannot judge a
// chain it cannot fold (D4).
func unreservedSpend(tbl *transition.Table, records []*event.Record, starts map[string][]int) []string {
	if tbl == nil {
		return nil
	}
	subjects := make([]string, 0, len(starts))
	for s := range starts {
		subjects = append(subjects, s)
	}
	// Map iteration is unordered and this list is evidence: one order
	// on every run.
	sort.Strings(subjects)
	var out []string
	for _, s := range subjects {
		if len(starts[s]) == 0 {
			continue
		}
		state, ok := tbl.StateAt(records, s)
		if !ok {
			// The fold placed no state for a subject that started a
			// run: nothing fenced any of them.
			for range starts[s] {
				out = append(out, s)
			}
			continue
		}
		// The fold's facts by the position of the record each came
		// from, so a raw start is judged against its own fact rather
		// than against a count (D1).
		fact := make(map[int]transition.RunStartFact, len(state.RunStarts))
		for _, st := range state.RunStarts {
			fact[st.Pos] = st
		}
		for _, pos := range starts[s] {
			st, folded := fact[pos]
			if !folded {
				// The fold read no fence and no reservation from this
				// record, so there is no citation to judge: spend with
				// no fence, named before any citation check (D1).
				out = append(out, s)
				continue
			}
			if !admit.RunStartValid(records, tbl, s, st) {
				out = append(out, s)
			}
		}
	}
	return out
}

// sealedAuthorClaims names every subject claimed by the key that
// sealed its checks (plans/os-aaec6a3c.md D1). This is the guardrail
// admission actually enforces on the claim path — "the key that sealed
// the subject's checks never implements against them" — and it is
// visible in the chain alone, because the fold carries the sealing
// position and signer. A raw push is the case that matters: the
// boundary would have refused the claim, so a chain that holds one is
// a chain where the guardrail was bypassed.
func sealedAuthorClaims(tbl *transition.Table, records []*event.Record) []string {
	if tbl == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, rec := range records {
		if rec.Event.Verb != ClaimTakenVerb {
			continue
		}
		s := rec.Event.Subject
		if seen[s] {
			continue
		}
		state, ok := tbl.StateAt(records, s)
		if !ok || state.Sealed == nil {
			continue
		}
		if state.Sealed.Signer == rec.Event.Actor {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ceilingClaims names every claim.taken an agent-kind key made above
// its squad's declared agent ceiling (plans/os-b5051f2e.md D1, D2).
// It mirrors admission's ceiling rule arm for arm: the claiming key's
// roster kind is read from the keyring replayed over the records to
// the claim's position (the roster admission saw), the subject's tier
// and routing from the fold, the ceiling from the declaration. A
// non-human key above the ceiling is a breach; a ceiling outside the
// tier vocabulary is a breach, because admission fails closed there
// and a bar that let a typo pass every claim would be weaker than the
// boundary it audits; a human key, an undeclared squad, an absent
// guardrails block and no declaration are silence. A chain whose
// roster does not replay yields no kinds to judge and the arm is
// silent, admission's Keyring == nil posture; verification refuses
// such a chain before the audit reads it. The evidence names itself:
// the subject, the kind and key, the tier, the position, the squad
// and the ceiling, so a reader can tell this arm's finding from the
// sealed-author arm's.
func ceilingClaims(tbl *transition.Table, records []*event.Record, declaration *posture.Config) []string {
	if tbl == nil || declaration == nil || declaration.Guardrails == nil {
		return nil
	}
	fold := tbl.FoldRecords(records)
	ring := keyring.New()
	active := ""
	var out []string
	for pos, rec := range records {
		// The keyring package's own replay (keyring.StateAt), taken one
		// record at a time so the kind read at a claim is the kind the
		// roster held there.
		if active == "" {
			active = rec.Event.V
			if pos == 0 && rec.Event.Verb == genesis.Verb && rec.Event.Subject == "system" {
				var g struct {
					Protocol string `json:"protocol"`
				}
				if err := json.Unmarshal(rec.Event.Payload, &g); err == nil && g.Protocol != "" {
					active = g.Protocol
				}
				ring.SeedGenesis(rec)
			}
		}
		if keyring.Applies(active) {
			if err := ring.Advance(rec); err != nil {
				return nil
			}
		}
		if rec.Event.Verb == ledger.UpgradeVerb && rec.Event.Subject == "system" {
			var up struct {
				To string `json:"to"`
			}
			if err := json.Unmarshal(rec.Event.Payload, &up); err == nil && up.To != "" {
				active = up.To
			}
		}
		if rec.Event.Verb != ClaimTakenVerb {
			continue
		}
		entry, ok := ring.Get(rec.Event.Actor)
		if !ok || entry.Kind == "human" {
			continue
		}
		s, ok := fold.State(rec.Event.Subject)
		if !ok {
			continue
		}
		ceiling, declared := declaration.AgentCeiling(s.Routing)
		if !declared {
			continue
		}
		if _, known := transition.Tier(ceiling); !known {
			out = append(out, fmt.Sprintf("%s: %s key %s claimed a %s contract at position %d in the %s squad, whose agent ceiling %q is not a tier", rec.Event.Subject, entry.Kind, rec.Event.Actor, s.Tier, pos, s.Routing, ceiling))
			continue
		}
		if transition.TierAbove(s.Tier, ceiling) {
			out = append(out, fmt.Sprintf("%s: %s key %s claimed a %s contract at position %d above the %s squad's agent ceiling %s", rec.Event.Subject, entry.Kind, rec.Event.Actor, s.Tier, pos, s.Routing, ceiling))
		}
	}
	return out
}

// foldAll replays the lifecycle verbs through the table, tracking each
// subject's state locally and returning the first illegal transition.
// It reads the verbs and subjects, never signatures, so it judges the
// chain the audit was handed rather than re-deriving a fold.
func foldAll(tbl *transition.Table, records []*event.Record) (int, error) {
	state := map[string]string{}
	for i, rec := range records {
		v := rec.Event.Verb
		if !tbl.IsLifecycleVerb(v) {
			continue
		}
		s := rec.Event.Subject
		to, err := tbl.Check(s, state[s], v)
		if err != nil {
			return i, err
		}
		state[s] = to
	}
	return len(records), nil
}
