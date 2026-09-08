// Package transition is the lifecycle transition table as data
// (docs/build-plan.md Phase 5 item 1; plans/os-d69a6c91.md;
// SEED-NEXT.md Part II §6 and conformance III.F "the lifecycle
// vocabulary and transition rules are self-validating data enforced at
// admission; claim is a transition, not a state"). The contract is
// data, not branching: legality comes from the parsed table, and
// hand-written conditionals that re-derive what the table says are a
// design violation. spec/transitions.json is the normative
// reviewable copy; the embedded table.json is byte-identical, pinned
// by test, exactly the classify.json precedent.
package transition

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/erasure"
	"github.com/shaunlmason/open-seed-v2/internal/escalation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

//go:embed table.json
var tableJSON []byte

// TableJSON exposes the embedded bytes for the byte-identity drill
// against spec/transitions.json.
func TableJSON() []byte { return append([]byte(nil), tableJSON...) }

// stateDecl is one declared state in the table file.
type stateDecl struct {
	Name     string `json:"name"`
	Initial  bool   `json:"initial"`
	Terminal bool   `json:"terminal"`
}

// row is one transition row: from is nil for the single birth verb.
// Exclusive marks a verb whose admission grants exclusivity (claims):
// online-only by construction, since only the admission boundary can
// order two rivals (plans/os-5dc16a7c.md).
type row struct {
	Verb      string    `json:"verb"`
	From      *[]string `json:"from"`
	To        string    `json:"to"`
	Exclusive bool      `json:"exclusive"`
}

type tableFile struct {
	SchemaVersion string      `json:"schema_version"`
	States        []stateDecl `json:"states"`
	Transitions   []row       `json:"transitions"`
}

// Table is the parsed, self-validated transition table.
type Table struct {
	SchemaVersion string
	initial       string
	terminal      map[string]bool
	states        map[string]bool
	birth         string
	birthTo       string
	// legal maps verb -> (from-state -> to-state); the birth verb has
	// no entry here.
	legal     map[string]map[string]string
	exclusive map[string]bool
	verbs     []string
}

// InvalidTransitionError is the typed lifecycle refusal (exit 3
// invalid_transition, the v1-continuity allocation): the verb is not
// legal for the subject's current state. From is empty when the
// subject does not exist yet.
type InvalidTransitionError struct {
	Subject string
	From    string
	Verb    string
	// Reason, when set, names why a row the table carries still
	// refuses at this position: the re-specification origin before
	// seed/4 (plans/os-6bd9ffff.md D4), a table row gated by version
	// rather than by state.
	Reason string
}

func (e *InvalidTransitionError) Error() string {
	if e.From == "" {
		return fmt.Sprintf("verb %s is illegal for subject %s: the subject does not exist (only the birth verb creates one)", e.Verb, e.Subject)
	}
	if e.Reason != "" {
		return fmt.Sprintf("verb %s is illegal for subject %s in state %s: %s", e.Verb, e.Subject, e.From, e.Reason)
	}
	return fmt.Sprintf("verb %s is illegal for subject %s in state %s", e.Verb, e.Subject, e.From)
}

// IncompleteError is the contract-completeness shape refusal
// (plans/os-d69a6c91.md): the charter's birth rule enforced as
// payload-field presence at the shape level, content schemas landing
// with their own items.
type IncompleteError struct {
	Verb    string
	Subject string
	Missing []string
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("%s on %s is incomplete: missing non-empty %s (the charter's completeness rule, presence-checked at admission)", e.Verb, e.Subject, strings.Join(e.Missing, ", "))
}

// VocabularyError is the completeness family's value refusal
// (plans/os-be12ac16.md D2, D3; spec/tiers.md): a field that is
// present but names no member of the table it is declared against.
// It rides IncompleteError's exit and wire code, and it names the
// field, the value and the known members, because a refusal says what
// IS legal.
type VocabularyError struct {
	Verb    string
	Subject string
	Field   string
	Value   string
	Known   []string
}

func (e *VocabularyError) Error() string {
	return fmt.Sprintf("%s on %s names %s %q, which is not in the vocabulary: the known members are %s, byte for byte (spec/tiers.md)", e.Verb, e.Subject, e.Field, e.Value, strings.Join(e.Known, ", "))
}

// Parse parses and self-validates a table. Every invariant violation
// refuses by name: an invalid table never loads.
func Parse(b []byte) (*Table, error) {
	var f tableFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("transition table does not parse: %v", err)
	}
	t := &Table{
		SchemaVersion: f.SchemaVersion,
		terminal:      map[string]bool{},
		states:        map[string]bool{},
		legal:         map[string]map[string]string{},
		exclusive:     map[string]bool{},
	}
	for _, s := range f.States {
		if t.states[s.Name] {
			return nil, fmt.Errorf("invalid transition table: duplicate state %q", s.Name)
		}
		t.states[s.Name] = true
		if s.Terminal {
			t.terminal[s.Name] = true
		}
		if s.Initial {
			if t.initial != "" {
				return nil, fmt.Errorf("invalid transition table: two initial states (%q, %q)", t.initial, s.Name)
			}
			t.initial = s.Name
		}
	}
	if t.initial == "" {
		return nil, fmt.Errorf("invalid transition table: no initial state")
	}
	outgoing := map[string]map[string]bool{}
	for _, r := range f.Transitions {
		if _, dup := t.legal[r.Verb]; dup || r.Verb == t.birth {
			return nil, fmt.Errorf("invalid transition table: duplicate verb %q", r.Verb)
		}
		if !t.states[r.To] {
			return nil, fmt.Errorf("invalid transition table: verb %q targets unknown state %q", r.Verb, r.To)
		}
		if r.Exclusive {
			// Exclusivity is meaningful only for a verb that enters a
			// held state from somewhere: a birth verb cannot be a claim.
			if r.From == nil {
				return nil, fmt.Errorf("invalid transition table: the birth verb %q cannot be exclusive", r.Verb)
			}
			t.exclusive[r.Verb] = true
		}
		if r.From == nil {
			if t.birth != "" {
				return nil, fmt.Errorf("invalid transition table: two birth verbs (%q, %q)", t.birth, r.Verb)
			}
			t.birth, t.birthTo = r.Verb, r.To
			t.verbs = append(t.verbs, r.Verb)
			continue
		}
		m := map[string]string{}
		for _, from := range *r.From {
			if !t.states[from] {
				return nil, fmt.Errorf("invalid transition table: verb %q leaves unknown state %q", r.Verb, from)
			}
			if t.terminal[from] {
				return nil, fmt.Errorf("invalid transition table: terminal state %q has an outgoing row (%q)", from, r.Verb)
			}
			if _, dup := m[from]; dup {
				return nil, fmt.Errorf("invalid transition table: verb %q lists state %q twice", r.Verb, from)
			}
			m[from] = r.To
			if outgoing[from] == nil {
				outgoing[from] = map[string]bool{}
			}
			outgoing[from][r.Verb] = true
		}
		t.legal[r.Verb] = m
		t.verbs = append(t.verbs, r.Verb)
	}
	if t.birth == "" {
		return nil, fmt.Errorf("invalid transition table: no birth verb (a row with from: null)")
	}
	if t.birthTo != t.initial {
		return nil, fmt.Errorf("invalid transition table: the birth verb lands on %q, not the initial state %q", t.birthTo, t.initial)
	}
	// Reachability from the initial state; then every non-terminal
	// state must reach a terminal one (no wedge).
	next := map[string]map[string]bool{}
	for _, m := range t.legal {
		for from, to := range m {
			if next[from] == nil {
				next[from] = map[string]bool{}
			}
			next[from][to] = true
		}
	}
	reached := map[string]bool{t.initial: true}
	queue := []string{t.initial}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for to := range next[s] {
			if !reached[to] {
				reached[to] = true
				queue = append(queue, to)
			}
		}
	}
	for s := range t.states {
		if !reached[s] {
			return nil, fmt.Errorf("invalid transition table: state %q is unreachable from the initial state", s)
		}
	}
	for s := range t.states {
		if t.terminal[s] {
			continue
		}
		if !reachesTerminal(s, next, t.terminal, map[string]bool{}) {
			return nil, fmt.Errorf("invalid transition table: state %q cannot reach a terminal state (wedge)", s)
		}
	}
	// The pinned invariant (plans/os-d69a6c91.md, review finding on
	// #113): leaving in_progress happens only through the four
	// deliberate exits, so silent abandonment is structural.
	deliberate := []string{"claim.parked", "claim.released", "claim.reaped", "submission.made"}
	sort.Strings(deliberate)
	var got []string
	for v := range outgoing["in_progress"] {
		got = append(got, v)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(deliberate, " ") {
		return nil, fmt.Errorf("invalid transition table: the in_progress exits must be exactly the four deliberate exits [%s], got [%s]", strings.Join(deliberate, " "), strings.Join(got, " "))
	}
	return t, nil
}

func reachesTerminal(s string, next map[string]map[string]bool, terminal, seen map[string]bool) bool {
	if terminal[s] {
		return true
	}
	if seen[s] {
		return false
	}
	seen[s] = true
	for to := range next[s] {
		if reachesTerminal(to, next, terminal, seen) {
			return true
		}
	}
	return false
}

var (
	defaultOnce  sync.Once
	defaultTable *Table
	defaultErr   error
)

// Default returns the embedded table, parsed and validated once. The
// embedded copy passing self-validation is itself drilled, so a build
// carrying an invalid table cannot pass its own suite.
func Default() (*Table, error) {
	defaultOnce.Do(func() { defaultTable, defaultErr = Parse(tableJSON) })
	return defaultTable, defaultErr
}

// Initial returns the initial state.
func (t *Table) Initial() string { return t.initial }

// BirthVerb returns the single verb that creates a subject.
func (t *Table) BirthVerb() string { return t.birth }

// Terminal reports whether the state is terminal.
func (t *Table) Terminal(state string) bool { return t.terminal[state] }

// Verbs returns the lifecycle vocabulary in table order.
func (t *Table) Verbs() []string { return append([]string(nil), t.verbs...) }

// Exclusive reports whether the verb's admission grants exclusivity.
func (t *Table) Exclusive(verb string) bool { return t.exclusive[verb] }

// Allows reports whether the verb legally leaves the given state.
func (t *Table) Allows(from, verb string) bool {
	_, ok := t.legal[verb][from]
	return ok
}

// IsLifecycleVerb reports whether the verb has a transition row.
func (t *Table) IsLifecycleVerb(verb string) bool {
	if verb == t.birth {
		return true
	}
	_, ok := t.legal[verb]
	return ok
}

// Check returns the state the verb moves the subject to. current is ""
// for a subject that does not exist yet; only the birth verb is legal
// then, and the birth verb is illegal for any existing subject.
func (t *Table) Check(subject, current, verb string) (string, error) {
	if verb == t.birth {
		if current != "" {
			return "", &InvalidTransitionError{Subject: subject, From: current, Verb: verb}
		}
		return t.birthTo, nil
	}
	if current == "" {
		return "", &InvalidTransitionError{Subject: subject, Verb: verb}
	}
	if to, ok := t.legal[verb][current]; ok {
		return to, nil
	}
	return "", &InvalidTransitionError{Subject: subject, From: current, Verb: verb}
}

// AcceptanceInfo is the fold's view of a contract's acceptance spec:
// Gated reports that gate evidence is present or not required, so
// "may this spec run?" is a projection read (plans/os-73c00a50.md).
type AcceptanceInfo struct {
	Ref        string
	Executable bool
	Gated      bool
}

// EvalRoot is where eval definitions live, relative to the repository
// root (spec/evals.md); internal/eval mirrors it. The boundary
// reads the layout, never the repository: an eval's acceptance spec
// MUST be the named definition's fixture, which binds the marker to a
// definition by path at admission and lets the derivation bind it to
// the reviewed anchor.
const EvalRoot = "evals"

// FlywheelRoot is where a flywheel repair contract's acceptance lives,
// relative to the repository: next/flywheel/<shape>/accept.md
// (plans/os-9075c308.md D7), the EvalRoot precedent. The path binds
// the subject to its shape from the record alone.
const FlywheelRoot = "flywheel"

// EvalFixturePrefix is the repository-relative prefix every acceptance
// ref of the named eval must carry.
func EvalFixturePrefix(name string) string { return EvalRoot + "/" + name + "/fixture/" }

// EvalInfo is intent.filed's optional eval object: Name is the shipped
// definition under evals/, and Tuple, present on a spot-check, is
// the configuration under re-test, advisory (the mint reads what the
// run DECLARED, never this).
type EvalInfo struct {
	Name  string
	Tuple *tuple.Tuple
	// Lesson and Carrier bind an adversarial evaluation to the
	// hypothesis it was filed for and the candidate revision it runs
	// against (plans/os-96850e5a.md D5); both empty on an ordinary
	// eval.
	Lesson  string
	Carrier string
	// Kind is the marker's kind: empty for an ordinary eval, or
	// EvalKindCalibration for a calibration (plans/os-2e34f66a.md D5;
	// review finding on the task PR: a verdict qualification cites a
	// calibration and nothing else, so the record says which evals
	// are calibrations rather than leaving the boundary to guess).
	Kind string
}

// EvalKindCalibration is the eval marker's kind for a calibration
// definition: the one kind whose verdict qualifies a verifier. The
// eval package mirrors it as its definition kind.
const EvalKindCalibration = "calibration"

// Claim is the active claim on an in_progress subject: the fence is
// the chain position of the admitted claim.taken record — derived,
// never asserted (plans/os-5dc16a7c.md) — and the holder its signer.
type Claim struct {
	Holder string
	Fence  int
}

// SubjectState is one subject's folded lifecycle state.
type SubjectState struct {
	State string
	// Since is the chain position of the event that put the subject
	// in its current state.
	Since int
	// Anomalies counts lifecycle events on the subject that the table
	// refuses: verification tolerates them (admission policy, not
	// chain validity), the fold skips them, and the projections
	// surface the count, never silence (plans/os-d69a6c91.md).
	Anomalies int
	// Claim is the active claim while the subject is in_progress,
	// cleared by every deliberate exit; nil otherwise.
	Claim *Claim
	// Claims is every active claim on the subject in fence order
	// (plans/os-56bee171.md D2): one for the exclusive case, several
	// on a racing squad's contract. Claim is always the first of them
	// or nil, so every reader of the singular fact reads what it
	// read before racing existed.
	Claims []Claim
	// Racing marks a subject that has held two claims at once: a
	// second claim.taken applied while in_progress, which only a
	// racing squad's contract admits (seed/6). Its later exits are
	// claim-scoped facts rather than the table's transitions, except
	// the first submission and the last racer's departure.
	Racing bool
	// Submissions is every submission of the current review window by
	// fence order; Submission is the first. Verdicts bind to one of
	// them by position.
	Submissions []SubmissionFact
	// Verdicts is every verdict rendered in the current window, in
	// order; Verdict is the latest. The merge chain cites one of them.
	Verdicts []VerdictFact
	// RaceSettled is the position of the merge.observed that closed a
	// race while other claims were still active: those claims are
	// settled-out from then on (their next act refuses race_settled,
	// their own deliberate exit still admits, the reaper reaps the
	// silent ones).
	RaceSettled *int
	// Tier is the contract's filed tier (presence-only data whose one
	// distinguished value, "trivial", exempts the plan gate;
	// plans/os-16c1d142.md).
	Tier string
	// Routing is the squad the intent named, read by the curation
	// predicate (plans/os-96850e5a.md D1).
	Routing string
	// Eval is the eval marker the filing carried at a seed/3 position
	// (plans/os-03e47abb.md D1): the contract is synthetic work with a
	// known verdict, whose pass mints a qualification for the tuple its
	// run declared and whose fail suspends one. Nil for every ordinary
	// contract.
	Eval *EvalInfo
	// Acceptance is the folded acceptance spec from the last admitted
	// contract.specified: the artifact anchor, the executable flag,
	// and whether gate evidence bound to the revision is present (or
	// not required). Nil until specified; a raw-pushed specification
	// whose acceptance is invalid counts an anomaly and leaves what
	// is honestly derivable.
	Acceptance *AcceptanceInfo
	// Specifications counts the applied contract.specified events on
	// the subject: the first from backlog, and from seed/4 every
	// re-specification from ready (plans/os-6bd9ffff.md D4), the
	// dispatcher revising its own triage. Two or more is the report's
	// re-triage figure.
	Specifications int
	// PriorClaimants is every fingerprint that has ever held a claim
	// on this subject: the fence rule's who-must-cite input — a
	// reaped or released worker cannot demote itself to observer
	// (plans/os-5dc16a7c.md, review finding on #114).
	PriorClaimants map[string]bool
	// Submission is the review-entering submission.made the fold
	// records: the chain position a verdict must cite and the signer
	// the L1 independence set includes (plans/os-f6d2c267.md). Set
	// each time a submission applies; the verdict rule consults it.
	Submission *SubmissionFact
	// Verdict is the latest admitted verdict.rendered on the subject
	// (pass or fail: 6.4's lockout will consult it); Requested the
	// latest merge.requested and the verdict position it cited; Merged
	// the admitted merge.observed — singular by construction, since
	// the first valid observation lands on terminal done
	// (plans/os-6cdc15be.md).
	Verdict   *VerdictFact
	Requested *RequestFact
	Merged    *MergeFact
	// Sealed is the sealed-checks commitment, captured only in its
	// legal window (ready, no prior claim, first seal); raw-pushed
	// seals outside it stay anomalies, never facts
	// (plans/os-3128535a.md).
	Sealed *SealedFact
	// Escalation is the standing blocked(needs-you): the question a
	// human gate is being asked and the closed set it may answer with
	// (plans/os-f781f0da.md). Set by an applied escalation.raised or
	// by a claim.parked carrying a question; cleared when
	// decision.recorded or contract.cancelled applies, because both
	// ARE answers. Nothing else about the contract moves meanwhile:
	// the escalation rule refuses contract.unblocked while it stands.
	Escalation *EscalationFact
	// SubmissionFails collects every fail verdict citing the current
	// submission window (cleared on each submission.made): the
	// red-verdict lockout scans the whole window, so a raw-pushed
	// later verdict can never bury an authentic fail
	// (plans/os-d2497eb7.md).
	SubmissionFails []VerdictFact
	// Deferred is the standing human-verdict deferral on the current
	// submission window, cleared on each submission.made
	// (plans/os-2e34f66a.md D4); a later render on the window must
	// come from a key with operator standing beside its verdict grant.
	Deferred *DeferralFact
	// Override is the current window's admitted operator override,
	// cleared on each submission.made; a raw-pushed second override
	// in one window stays an anomaly, never the fact.
	Override *OverrideFact
	// Observation is the latest check.observed folded on the current
	// submission window, cleared on each submission.made
	// (plans/os-0cd18799.md D1): what the forge last said about the
	// head under review. Folded only from its window (review, from
	// seed/8) and with a well-formed payload; raw pushes outside it
	// stay anomalies, never facts.
	Observation *CheckFact
	// Returns is every applied contract.returned on the subject, in
	// chain order, with what each cited (D4, D7).
	Returns []ReturnFact
	// Offers is every well-shaped offer.published on the subject, in
	// chain order (plans/os-c61c3392.md): the tolerant fold records
	// raw pushes too, and the consuming surface applies liveness
	// (ready, unexpired, unconsumed) and the signer's position-
	// accurate supervise boundary. Facts persist; nothing is erased.
	Offers []OfferFact
	// LastClaim is the position of the latest applied claim.taken,
	// the offer-consumption boundary: a claim consumes every offer at
	// or before it ("claimed or expire", SEED-NEXT.md §II.9).
	// Meaningful only when PriorClaimants is non-empty.
	LastClaim int
	// Budget is the contract's filed budget class, captured at birth
	// beside Tier (plans/os-cecac5de.md): presence-only data whose
	// capacity meaning comes from the spec class table.
	Budget string
	// Reservations and BudgetCloses are the budget facts, independent
	// lists in chain order: a close attempt NEVER mutates the
	// reservation it cites — validity and effective closure are
	// derived at every consuming surface (DeriveBudget), the
	// laundering-countermeasure shape. Facts persist; nothing is
	// erased.
	Reservations []ReservationFact
	BudgetCloses []CloseFact
	// ClaimFences is every applied claim.taken position on the
	// subject: the run facts' citation domain
	// (plans/os-1dad487d.md).
	ClaimFences map[int]bool
	// RunStarts and Runs are the execution-run facts, independent
	// lists in chain order (plans/os-1dad487d.md): the gated spend
	// initiation and the once-per-fence metering aggregate. Raw
	// pushes fold tolerantly; a fact citing a fence that is no claim
	// position counts an anomaly, never a fact.
	RunStarts []RunStartFact
	Runs      []RunFact
	// Interrupts is every folded run.interrupted, the same
	// independent-list posture (plans/os-0f718b4e.md): the
	// supervisor's safe-point preemption requests, which conforming
	// workers observe by polling and answer by parking.
	Interrupts []InterruptFact
}

// InterruptFact is one folded run.interrupted: the chain position,
// the signer, and the claim fence whose run it preempts. Admission
// requires the ACTIVE fence; the fold records what stands, and
// consumers judge validity at the fact's own position
// (admit.InterruptValid).
type InterruptFact struct {
	Pos    int
	Signer string
	Fence  int
}

// RunStartFact is one folded run.started: the chain position, the
// signer, the claim fence it fences the run to, and the reservation
// it cites. Admission validates the citation against an open valid
// reservation; the fold records what stands.
type RunStartFact struct {
	Pos         int
	Signer      string
	Fence       int
	Reservation int
	// Tuple is the runtime configuration the start DECLARED
	// (plans/os-8e53ffd9.md D3): nil where the payload carried none or
	// carried one that did not parse, which the tolerant fold records
	// as absence. Admission requires it at seed/2; the executor checks
	// what it resolved against it before releasing execution.
	Tuple *tuple.Tuple
}

// RunFact is one folded run.settled: the chain position, the signer,
// the claim fence, and the aggregate metered units and line count
// from the run's observation stream. Telemetry, never authority:
// budget.settle carries the authoritative actuals.
type RunFact struct {
	Pos    int
	Signer string
	Fence  int
	Units  int
	Lines  int
}

// VerdictFact is the fold's record of the latest rendered verdict.
// Submission is the position the verdict's payload cited (-1 when
// unparseable): the red-verdict lockout binds fails to the submission
// they judged (plans/os-d2497eb7.md).
type VerdictFact struct {
	Pos        int
	Verdict    string
	Receipt    string
	Signer     string
	Submission int
	// Independence is the level the verdict recorded, verbatim
	// (plans/os-99829835.md D3): the boundary requires it to equal the
	// level the record supports, and the merge chain and reconcile
	// re-judge it from the same facts.
	Independence string
	// Tuple is the verifier's declared configuration, nil where the
	// payload carried none or one that did not parse (dropped and
	// counted, the RunStartFact posture).
	Tuple *tuple.Tuple
	// Levels reports whether the record sits at a version where the
	// level vocabulary applies (version.LevelsApply): a seed/3 verdict
	// recorded the literal L1 whatever its acceptance supported, and is
	// judged by seed/3's rule.
	Levels bool
	// Scorecard is the rubric scoring the verdict cites, nil where the
	// payload carried none (plans/os-2e34f66a.md D2): the artifact's
	// digest and, per item, the two enums the derivation reads, so
	// every boundary reapplies the derivation from the record alone.
	Scorecard *ScorecardRef
}

// ScoreItem is the payload half of one scored rubric item: the id and
// exactly the two enums the derivation reads. Evidence and notes are
// bulk and stay in the artifact.
type ScoreItem struct {
	ID          string `json:"id"`
	Score       string `json:"score"`
	Uncertainty string `json:"uncertainty"`
}

// ScorecardRef is the scorecard as the verdict carries it.
type ScorecardRef struct {
	Digest string      `json:"digest"`
	Items  []ScoreItem `json:"items"`
}

// The derivation's refinement codes (plans/os-2e34f66a.md D3), under
// exit 20 checks_red.
const (
	CodeRubricRed    = "rubric_red"
	CodeHumanVerdict = "human_verdict"
)

// DeriveScores is the one derivation every boundary applies to the
// payload's items (D3): an item at high uncertainty forbids both
// verdicts (human_verdict, the item named); a fail item forbids pass
// (rubric_red, the item named) and leaves fail; all pass at low
// permits pass. Returned as the permitted verdict ("" when a human
// must judge), the refinement code and the item it names.
func DeriveScores(items []ScoreItem) (verdict, code, item string) {
	for _, it := range items {
		if it.Uncertainty != "low" {
			return "", CodeHumanVerdict, it.ID
		}
	}
	for _, it := range items {
		if it.Score != "pass" {
			return "fail", CodeRubricRed, it.ID
		}
	}
	return "pass", "", ""
}

// VerdictDeferredVerb is the human-verdict deferral (plans/os-2e34f66a.md
// D4): a fact admitted in review by a verdict key, changing no state,
// naming the items its scorecard left at high uncertainty; it creates
// the verdict.human obligation the operator lane owes.
const VerdictDeferredVerb = "verdict.deferred"

// DeferralFact is the folded deferral on the current submission: the
// receipt the verifier computed (what the human's render cites, since
// sealed checks encrypt to keys without operator standing and a human
// can never unseal), its scorecard where the spec carries a rubric,
// and the items it left at high uncertainty.
type DeferralFact struct {
	Pos        int
	Signer     string
	Submission int
	Receipt    string
	Scorecard  string
	Items      []string
}

// RequestFact is the latest merge.requested and its citation: exactly
// one of CitedVerdict or CitedOverride is set (-1 otherwise), the
// pass-verdict path or the operator-override path
// (plans/os-d2497eb7.md).
type RequestFact struct {
	Pos           int
	CitedVerdict  int
	CitedOverride int
}

// MergeFact is the admitted merge.observed: the chain position and
// the merged commit the observer recorded.
type MergeFact struct {
	Pos int
	SHA string
}

// OverrideFact is the admitted operator override
// (plans/os-d2497eb7.md): its chain position, the operator that signed
// it, the required reason, and the boundary-validated fail verdict it
// overruled. It is never a verdict, and every surface shows it under
// its own name.
type OverrideFact struct {
	Pos          int
	Signer       string
	Reason       string
	CitedVerdict int
}

// SealedFact is the sealed-checks commitment (plans/os-3128535a.md;
// spec/sealed-checks.md): the chain position proves the checks
// predate implementation, Commitment is the salted hash the ciphertext
// must verify against, and Signer is the sealing key the per-subject
// authoring-isolation check consults at claim.taken.
type SealedFact struct {
	Pos        int
	Commitment string
	Signer     string
}

// EscalationFact is a standing blocked(needs-you) (plans/os-f781f0da.md).
// TS is the raising event's own timestamp and it is load-bearing: age
// is ELAPSED TIME, and Pos is an ordinal that orders without measuring
// — an escalation untouched for hours has the same position difference
// as one answered instantly after a burst of unrelated traffic. The
// reading surface computes now minus TS at its own instant, the offer
// liveness posture (spec/offers.md): admission never reads a wall
// clock, a live read may.
type EscalationFact struct {
	Pos      int
	TS       string
	Raiser   string
	Question string
	Options  []escalation.Option
}

// OfferFact is one folded offer.published (plans/os-c61c3392.md): its
// chain position, the publishing signer (whose supervise standing the
// list surface validates at exactly that position), the eligibility
// scopes (empty means unscoped), and the RFC3339 expiry. Liveness is
// derived, never stored: ready subject, unexpired, and no applied
// claim.taken after Pos.
type OfferFact struct {
	Pos          int
	Signer       string
	Capabilities []string
	Tiers        []string
	// Tuples scopes the offer to qualified workers holding one of
	// these configurations (plans/os-8e53ffd9.md D6); empty means
	// unscoped by tuple.
	Tuples  []tuple.Tuple
	Expires string
}

// LiveOffers derives the subject's live offers at now
// (plans/os-c61c3392.md): the subject is ready, the offer is
// unexpired (expires strictly after now), and no applied claim.taken
// landed after it — a claim consumes every offer at or before it. The
// signer's position-accurate supervise boundary is the consuming
// surface's remaining check, since it needs keyring replay.
func (s *SubjectState) LiveOffers(now time.Time) []OfferFact {
	if s == nil || s.State != "ready" {
		return nil
	}
	var live []OfferFact
	for _, o := range s.Offers {
		if len(s.PriorClaimants) > 0 && s.LastClaim > o.Pos {
			continue
		}
		exp, err := time.Parse(time.RFC3339, o.Expires)
		if err != nil || !exp.After(now) {
			continue
		}
		live = append(live, o)
	}
	return live
}

// ReservationFact is one folded budget.reserve
// (plans/os-cecac5de.md): its chain position, the reserving signer
// (whose active-holder-or-operator standing consuming surfaces
// validate at exactly that position), and the amount in class units.
type ReservationFact struct {
	Pos    int
	Signer string
	Amount int
}

// CloseFact is one folded close attempt (budget.settle or
// budget.release) citing a reservation by position. Attempts are
// recorded independently and never mutate the reservation: the
// effective closure is the first attempt whose signer is the
// reservation's own reserving signer or the operator lane at the
// attempt's position, derived per DeriveBudget.
type CloseFact struct {
	Pos         int
	Signer      string
	Reservation int
	Kind        string
	Actuals     int
}

// BudgetView is a subject's derived budget state at one instant of
// judgment (plans/os-cecac5de.md D4): capacity from the class table,
// the open valid reservations, the settled actuals, and remaining =
// capacity − open reserved − settled actuals. Overrun settles shrink
// remaining below what was reserved, recorded never clamped.
type BudgetView struct {
	Class     string
	Capacity  int
	Known     bool
	Open      []ReservationFact
	Settled   int
	Remaining int
	// ClosedBy maps a reservation's position to its effective close.
	ClosedBy map[int]CloseFact
}

// DeriveBudget computes the view. valid reports whether a reservation
// passed the authoring boundary at its own position (the active claim
// holder or the operator lane there); closeValid whether an attempt
// may close the cited reservation (its reserving signer or the
// operator lane at the attempt's position). Invalid reservations
// consume nothing and their closes decide nothing; the first valid
// close wins and later attempts on the same reservation are inert.
func (s *SubjectState) DeriveBudget(valid func(ReservationFact) bool, closeValid func(CloseFact, ReservationFact) bool) BudgetView {
	v := BudgetView{Class: s.Budget, ClosedBy: map[int]CloseFact{}}
	v.Capacity, v.Known = BudgetCapacity(s.Budget)
	byPos := map[int]ReservationFact{}
	for _, r := range s.Reservations {
		byPos[r.Pos] = r
	}
	for _, c := range s.BudgetCloses {
		r, ok := byPos[c.Reservation]
		if !ok {
			continue
		}
		if _, closed := v.ClosedBy[c.Reservation]; closed {
			continue
		}
		if !valid(r) || !closeValid(c, r) {
			continue
		}
		v.ClosedBy[c.Reservation] = c
	}
	spent := 0
	for _, r := range s.Reservations {
		if !valid(r) {
			continue
		}
		if c, closed := v.ClosedBy[r.Pos]; closed {
			if c.Kind == "settle" {
				v.Settled = satAdd(v.Settled, c.Actuals)
				spent = satAdd(spent, c.Actuals)
			}
		} else {
			v.Open = append(v.Open, r)
			spent = satAdd(spent, r.Amount)
		}
	}
	if v.Known {
		v.Remaining = v.Capacity - spent
	}
	return v
}

// satAdd sums non-negative unit counts with saturation at MaxInt:
// raw-pushed facts can carry any machine-sized value, and a wrapped
// sum would make remaining capacity INCREASE through overflow
// (review finding on the task PR). Saturated spend keeps remaining
// pinned far below zero instead, and capacity − spent cannot itself
// wrap because capacity is a small table value.
func satAdd(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

// SubmissionFact binds a verdict to the submission it judges
// (spec/verdicts.md): Pos is the chain position of the admitted
// submission.made, Signer its fingerprint.
type SubmissionFact struct {
	Pos    int
	Signer string
	// PR is the pull request the submission named, when it named one
	// (plans/os-0cd18799.md D2): what the maintenance pass polls the
	// forge for. Empty on a submission that named none, which is
	// legal and simply never observed.
	PR string
}

// milestoneFact is a subject's milestone high-water mark: the highest
// admitted count and the chain position of the latest milestone.
type milestoneFact struct {
	Count int
	Pos   int
}

// Fold is the folded lifecycle state of every subject in a record
// prefix, in first-appearance order.
// IngressFact is one inbound proposal (request.filed) as the fold
// keeps it (plans/os-48df10a2.md D2): who filed it, from where, what
// kind, on which subject, and its answer when the dispatcher closed it.
type IngressFact struct {
	Pos      int
	TS       string
	Signer   string
	Subject  string
	Origin   string
	Kind     string
	Answered *int
	Outcome  string
	Intent   int
	Answerer string
}

type Fold struct {
	states map[string]*SubjectState
	order  []string
	// requests is every request.filed applied, in order, with its
	// answer; RequestAnomalies counts the malformed ones.
	requests         []IngressFact
	RequestAnomalies int
	// planned maps subject -> the last admitted plan.approved's plan
	// anchor. plan.* events are facts, not transitions
	// (plans/os-16c1d142.md): they change no lifecycle state and the
	// submission gate consults them.
	planned map[string]string
	// proposed maps subject -> the FIRST proposal's content digest,
	// and approved maps subject -> the approval's, both read at seed/4
	// positions only, where the plan verbs carry one
	// (plans/os-6bd9ffff.md D5). An approval is unedited iff the two
	// are equal: the planner's original decomposition survived review.
	// The first proposal, not the last: a planner that revises its own
	// proposal before review is exactly the edit the figure counts.
	proposed map[string]string
	approved map[string]string
	// milestones maps subject -> its milestone high-water mark
	// (plans/os-2ff8dbf1.md): progress.milestone is a fact too, and
	// the summarization boundary consults the mark. Raw-pushed
	// regressions keep the maximum count, so tolerated history never
	// lowers the monotonic bar.
	milestones map[string]milestoneFact
	// approvals is every approval.requested applied, in order, with
	// its answer and the act that spent it (plans/os-5781a026.md D3):
	// the require-approval mode's facts, kept beside the lifecycle
	// like the requests. ApprovalAnomalies counts the malformed ones.
	approvals         []ApprovalFact
	ApprovalAnomalies int
	// erasures is every artifact.erased applied, in order
	// (plans/os-db5cd353.md D4): the attributable half of III.A row
	// 7, kept beside the lifecycle like the requests, so the seal
	// audit can name who erased a ciphertext and why rather than
	// report an absence. ErasureAnomalies counts the malformed ones.
	erasures         []ErasureFact
	ErasureAnomalies int
}

// ApprovalFact is one per-verb approval request as the fold keeps it
// (plans/os-5781a026.md D3): who asked, on which subject, for which
// verb by which actor and why; the operator's answer when one landed;
// and the position of the act that spent a grant, since one approval
// admits one act.
type ApprovalFact struct {
	Pos        int
	TS         string
	Requester  string
	Subject    string
	Verb       string
	Actor      string
	Reason     string
	Answered   *int
	Granted    bool
	Answerer   string
	ConsumedAt *int
}

// Open reports whether the grant still admits an act: answered, granted
// and not yet spent.
func (a ApprovalFact) Open() bool {
	return a.Answered != nil && a.Granted && a.ConsumedAt == nil
}

// Approvals is every approval request the fold applied, in chain order.
func (f *Fold) Approvals() []ApprovalFact { return append([]ApprovalFact(nil), f.approvals...) }

// ApprovalAt finds an approval request by its position.
func (f *Fold) ApprovalAt(pos int) (ApprovalFact, bool) {
	for _, a := range f.approvals {
		if a.Pos == pos {
			return a, true
		}
	}
	return ApprovalFact{}, false
}

// OpenApproval finds the oldest open grant for an act: on the subject,
// naming the verb and the actor, granted and not yet spent
// (plans/os-5781a026.md D3). An unanswered or denied request admits
// nothing.
func (f *Fold) OpenApproval(subject, verb, actor string) (ApprovalFact, bool) {
	for _, a := range f.approvals {
		if a.Subject == subject && a.Verb == verb && a.Actor == actor && a.Open() {
			return a, true
		}
	}
	return ApprovalFact{}, false
}

// OpenApprovals is every open grant for an act, oldest first: what the
// admission rule re-judges position-accurately before trusting any of
// them, since the tolerant fold records a well-shaped raw push.
func (f *Fold) OpenApprovals(subject, verb, actor string) []ApprovalFact {
	var out []ApprovalFact
	for _, a := range f.approvals {
		if a.Subject == subject && a.Verb == verb && a.Actor == actor && a.Open() {
			out = append(out, a)
		}
	}
	return out
}

// PendingApprovals is every unanswered request on the subject, oldest
// first: what the answer verbs derive their citation from and what
// the obligations projection owes the operator.
func (f *Fold) PendingApprovals(subject string) []ApprovalFact {
	var out []ApprovalFact
	for _, a := range f.approvals {
		if a.Subject == subject && a.Answered == nil {
			out = append(out, a)
		}
	}
	return out
}

func (f *Fold) foldApproval(pos int, e *event.Event) {
	switch e.Verb {
	case approval.RequestedVerb:
		p, err := approval.ParseRequested(e.Subject, e.Payload)
		if err != nil {
			f.ApprovalAnomalies++
			return
		}
		f.approvals = append(f.approvals, ApprovalFact{Pos: pos, TS: e.TS, Requester: e.Actor, Subject: e.Subject, Verb: p.Verb, Actor: p.Actor, Reason: p.Reason})
	case approval.GrantedVerb, approval.DeniedVerb:
		_, cited, err := approval.ParseAnswer(e.Verb, e.Subject, e.Payload)
		if err != nil {
			f.ApprovalAnomalies++
			return
		}
		for i := range f.approvals {
			a := &f.approvals[i]
			if a.Pos == cited && a.Answered == nil && a.Subject == e.Subject {
				at := pos
				a.Answered, a.Granted, a.Answerer = &at, e.Verb == approval.GrantedVerb, e.Actor
				return
			}
		}
		f.ApprovalAnomalies++
	}
}

// consumeApproval spends the oldest open grant the record matches: the
// first record on the grant's subject whose verb and actor it names,
// at that record's position (plans/os-5781a026.md D3), so one approval
// admits one act and a second act needs a second request.
func (f *Fold) consumeApproval(pos int, e *event.Event) {
	for i := range f.approvals {
		a := &f.approvals[i]
		if a.Subject == e.Subject && a.Verb == e.Verb && a.Actor == e.Actor && a.Open() {
			at := pos
			a.ConsumedAt = &at
			return
		}
	}
}

// ErasureFact is one erasure (artifact.erased) as the fold keeps it:
// who erased which artifact, on which subject, at which position, and
// the obligation named.
type ErasureFact struct {
	Pos      int
	TS       string
	Signer   string
	Subject  string
	Artifact string
	Reason   string
}

// Erasures is every erasure the fold applied, in chain order.
func (f *Fold) Erasures() []ErasureFact { return append([]ErasureFact(nil), f.erasures...) }

// Erasure finds the erasure of an artifact, if one stands, wherever
// it was recorded: the tombstone is digest-wide (plans/os-db5cd353.md
// D2, D4), because the store holds one object per digest, so an
// artifact two contracts reference is erased for both by one act and
// the attribution must reach both.
func (f *Fold) Erasure(artifact string) (ErasureFact, bool) {
	for _, e := range f.erasures {
		if e.Artifact == artifact {
			return e, true
		}
	}
	return ErasureFact{}, false
}

func (f *Fold) foldErasure(pos int, e *event.Event) {
	p, err := erasure.Parse(e.Subject, e.Payload)
	if err != nil {
		f.ErasureAnomalies++
		return
	}
	f.erasures = append(f.erasures, ErasureFact{Pos: pos, TS: e.TS, Signer: e.Actor, Subject: e.Subject, Artifact: p.Artifact, Reason: p.Reason})
}

// Milestone reports the subject's milestone high-water mark: the
// highest admitted count and the position of the latest milestone
// (plans/os-f0ae2cdf.md D7: an initiative's rollup sums its
// descendants' counts, never a percentage).
func (f *Fold) Milestone(subject string) (count, pos int, ok bool) {
	m, has := f.milestones[subject]
	if !has {
		return 0, 0, false
	}
	return m.Count, m.Pos, true
}

// PlanApproved reports the subject's approved-plan anchor, if any.
func (f *Fold) PlanApproved(subject string) (string, bool) {
	ref, ok := f.planned[subject]
	return ref, ok
}

// PlanDigests is the fold's plan-content facts for a subject
// (plans/os-6bd9ffff.md D5): the first proposal's digest and the
// approval's, each empty where the chain carries none (a proposal or
// approval before seed/4, or none at all).
type PlanDigests struct {
	Proposed string
	Approved string
}

// PlanDigests reports the subject's plan digests.
func (f *Fold) PlanDigests(subject string) PlanDigests {
	return PlanDigests{Proposed: f.proposed[subject], Approved: f.approved[subject]}
}

// Unedited reports whether the approval kept the first proposal's
// content: both digests present and equal. measured is false when
// either is absent, the report's unmeasured case, never guessed.
func (d PlanDigests) Unedited() (unedited, measured bool) {
	if d.Proposed == "" || d.Approved == "" {
		return false, false
	}
	return d.Proposed == d.Approved, true
}

// RespecificationNeeds is the reason a ready-origin specification
// refuses at a version before seed/4: the row activates there
// (plans/os-6bd9ffff.md D4, D7), and a validator of the earlier
// version judges the record by its own table.
func RespecificationNeeds(active string) string {
	return fmt.Sprintf("re-specification activates at %s and the chain is at %s (append system.protocol.upgraded first; spec/lifecycle.md)", version.Seed4, active)
}

// FoldRecords folds every subject's lifecycle events, skipping illegal
// history without wedging, the halt.StateAt posture.
func (t *Table) FoldRecords(records []*event.Record) *Fold {
	f := &Fold{states: map[string]*SubjectState{}, planned: map[string]string{}, proposed: map[string]string{}, approved: map[string]string{}, milestones: map[string]milestoneFact{}}
	for pos, rec := range records {
		e := &rec.Event
		// The request ingress (plans/os-48df10a2.md): facts beside the
		// lifecycle, read at seed/7 positions only.
		if e.Verb == "request.filed" || e.Verb == "request.answered" {
			f.foldRequest(pos, e)
			continue
		}
		// The per-verb approval (plans/os-5781a026.md D3): additive
		// catalog growth active from seed/1, beside the lifecycle;
		// every other activated record spends the grant it matches.
		if approval.IsApprovalVerb(e.Verb) {
			if version.Activated(e.V) {
				f.foldApproval(pos, e)
			}
			continue
		}
		if version.Activated(e.V) {
			f.consumeApproval(pos, e)
		}
		// The erasure fact (plans/os-db5cd353.md D4): additive catalog
		// growth active from seed/1, beside the lifecycle.
		if e.Verb == erasure.Verb {
			if version.Activated(e.V) {
				f.foldErasure(pos, e)
			}
			continue
		}
		if !version.Activated(e.V) {
			// Lifecycle semantics activate at seed/1 and stay on at
			// every later registered version (version.Activated, the
			// keyring.Applies posture): grandfathered earlier records
			// stay inert even where their verb names later became
			// lifecycle verbs, so an upgraded ledger's history cannot
			// occupy states or make a real filing look like a second
			// birth. Version discipline pins e.V to the version
			// active at the record's position.
			continue
		}
		if e.Verb == PlanApprovedVerb {
			if ref, _ := planAnchor(e.Payload); ref != "" {
				f.planned[e.Subject] = ref
				// The digest is a seed/4 fact: at earlier positions the
				// field is undefined and a raw copy carrying one is not
				// a measurement (D5, D7).
				if d, ok := planDigest(e.Payload); ok && version.LevelsApply(e.V) {
					f.approved[e.Subject] = d
				}
			}
			continue
		}
		if e.Verb == PlanProposedVerb {
			if d, ok := planDigest(e.Payload); ok && version.LevelsApply(e.V) {
				if _, seen := f.proposed[e.Subject]; !seen {
					f.proposed[e.Subject] = d
				}
			}
			continue
		}
		if e.Verb == MilestoneVerb {
			// Milestone semantics activate at seed/1 like the rest of
			// the summarization boundary: a grandfathered pre-upgrade
			// record whose payload happens to carry a count must not
			// become the high-water mark and wedge legitimate
			// post-upgrade progress. Version discipline pins e.V to
			// the version active at the record's position.
			var m struct {
				Count *int `json:"count"`
			}
			if version.Activated(e.V) && json.Unmarshal(e.Payload, &m) == nil && m.Count != nil {
				fact, seen := f.milestones[e.Subject]
				if !seen || *m.Count > fact.Count {
					fact.Count = *m.Count
				}
				fact.Pos = pos
				f.milestones[e.Subject] = fact
			}
			continue
		}
		if e.Verb == VerdictRenderedVerb {
			// The chain facts activate at seed/1 with the rest of the
			// pipeline; a fact on a subject no lifecycle event created
			// binds nothing.
			if s, ok := f.states[e.Subject]; ok {
				var v struct {
					Verdict      string          `json:"verdict"`
					Receipt      string          `json:"receipt"`
					Submission   string          `json:"submission"`
					Independence string          `json:"independence"`
					Tuple        json.RawMessage `json:"tuple"`
					Scorecard    json.RawMessage `json:"scorecard"`
				}
				if json.Unmarshal(e.Payload, &v) == nil && v.Verdict != "" {
					cited := -1
					if n, err := strconv.Atoi(strings.TrimSpace(v.Submission)); err == nil {
						cited = n
					}
					fact := VerdictFact{Pos: pos, Verdict: v.Verdict, Receipt: v.Receipt, Signer: e.Actor, Submission: cited,
						Independence: v.Independence, Levels: version.LevelsApply(e.V)}
					if len(v.Tuple) > 0 && string(v.Tuple) != "null" {
						if t, err := tuple.Parse(v.Tuple); err == nil {
							fact.Tuple = &t
						} else {
							s.Anomalies++
						}
					}
					if len(v.Scorecard) > 0 && string(v.Scorecard) != "null" {
						var ref ScorecardRef
						if err := json.Unmarshal(v.Scorecard, &ref); err == nil && ref.Digest != "" {
							fact.Scorecard = &ref
						} else {
							s.Anomalies++
						}
					}
					s.Verdict = &fact
					s.Verdicts = append(s.Verdicts, fact)
					// The lockout scans the whole submission window, so
					// a later raw verdict can never bury an authentic
					// fail (plans/os-d2497eb7.md).
					if v.Verdict == "fail" && s.Submission != nil && cited == s.Submission.Pos {
						s.SubmissionFails = append(s.SubmissionFails, fact)
					}
				}
			}
			continue
		}
		if e.Verb == VerdictDeferredVerb {
			// The deferral changes no state: a fact on the current
			// submission window, kept until the next submission
			// (plans/os-2e34f66a.md D4). Whether it passed the boundary
			// is the reader's question, the RunStartValid posture.
			if s, ok := f.states[e.Subject]; ok {
				var d struct {
					Receipt    string   `json:"receipt"`
					Scorecard  string   `json:"scorecard"`
					Submission string   `json:"submission"`
					Items      []string `json:"items"`
				}
				if json.Unmarshal(e.Payload, &d) == nil && d.Receipt != "" && s.Submission != nil {
					if cited, err := strconv.Atoi(strings.TrimSpace(d.Submission)); err == nil && cited == s.Submission.Pos {
						s.Deferred = &DeferralFact{Pos: pos, Signer: e.Actor, Submission: cited, Receipt: d.Receipt, Scorecard: d.Scorecard, Items: d.Items}
					} else {
						s.Anomalies++
					}
				} else {
					s.Anomalies++
				}
			}
			continue
		}
		if e.Verb == CheckObservedVerb {
			// The forge observation folds only from its window: a
			// review subject at a seed/8 position, with the strict
			// shape. Raw pushes outside it stay anomalies, never
			// facts (plans/os-0cd18799.md D1); the admission rule holds
			// the head binding and the change requirement.
			if s, ok := f.states[e.Subject]; ok {
				fact, ferr := ParseCheckObserved(pos, e)
				if ferr == nil && version.ForgeChecksApply(e.V) && s.State == "review" {
					s.Observation = &fact
				} else {
					s.Anomalies++
				}
			}
			continue
		}
		if e.Verb == MergeRequestedVerb {
			if s, ok := f.states[e.Subject]; ok {
				var r struct {
					Verdict  string `json:"verdict"`
					Override string `json:"override"`
				}
				if json.Unmarshal(e.Payload, &r) == nil {
					fact := RequestFact{Pos: pos, CitedVerdict: -1, CitedOverride: -1}
					set := false
					if cited, err := strconv.Atoi(strings.TrimSpace(r.Verdict)); err == nil && r.Verdict != "" {
						fact.CitedVerdict, set = cited, true
					}
					if cited, err := strconv.Atoi(strings.TrimSpace(r.Override)); err == nil && r.Override != "" {
						fact.CitedOverride, set = cited, true
					}
					if set {
						s.Requested = &fact
					}
				}
			}
			continue
		}
		if e.Verb == MergeOverriddenVerb {
			// The override folds only from its window: a review subject
			// with no override yet. Raw pushes outside it stay
			// anomalies, never facts (plans/os-d2497eb7.md).
			if s, ok := f.states[e.Subject]; ok {
				var o struct {
					Reason  string `json:"reason"`
					Verdict string `json:"verdict"`
				}
				cited := -1
				if json.Unmarshal(e.Payload, &o) == nil {
					if n, err := strconv.Atoi(strings.TrimSpace(o.Verdict)); err == nil && o.Verdict != "" {
						cited = n
					}
				}
				if o.Reason != "" && cited >= 0 && s.State == "review" && s.Override == nil {
					s.Override = &OverrideFact{Pos: pos, Signer: e.Actor, Reason: o.Reason, CitedVerdict: cited}
				} else {
					s.Anomalies++
				}
			}
			continue
		}
		if e.Verb == CheckSealedVerb {
			// The commitment folds only from its legal window: ready,
			// no prior claim, first seal. A raw-pushed seal outside it
			// would retroactively claim pre-existence the ordering
			// disproves, so it stays an anomaly, never a fact
			// (plans/os-3128535a.md).
			if s, ok := f.states[e.Subject]; ok {
				var c struct {
					Commitment string `json:"commitment"`
				}
				if json.Unmarshal(e.Payload, &c) == nil && c.Commitment != "" &&
					s.State == "ready" && len(s.PriorClaimants) == 0 && s.Sealed == nil {
					s.Sealed = &SealedFact{Pos: pos, Commitment: strings.TrimSpace(c.Commitment), Signer: e.Actor}
				} else {
					s.Anomalies++
				}
			}
			continue
		}
		if e.Verb == OfferPublishedVerb {
			// The tolerant posture (plans/os-c61c3392.md): any
			// well-shaped offer folds, raw pushes included, and the
			// consuming surface derives liveness and validates the
			// signer's boundary; a malformed payload folds to nothing,
			// and a fact on a subject no lifecycle event created binds
			// nothing.
			if s, ok := f.states[e.Subject]; ok {
				var o struct {
					Eligibility *struct {
						Capabilities []string          `json:"capabilities"`
						Tiers        []string          `json:"tiers"`
						Tuples       []json.RawMessage `json:"tuples"`
					} `json:"eligibility"`
					Expires string `json:"expires"`
				}
				if json.Unmarshal(e.Payload, &o) == nil && o.Eligibility != nil && strings.TrimSpace(o.Expires) != "" {
					// A malformed member makes the whole offer fold to
					// nothing, counted as an anomaly (review finding on
					// the task PR): dropping the member alone would
					// turn a raw-pushed unparseable scope into an
					// UNSCOPED offer every eligible worker sees, and a
					// malformed policy must never widen into a broader
					// one.
					var tuples []tuple.Tuple
					malformed := false
					for _, raw := range o.Eligibility.Tuples {
						t, err := tuple.Parse(raw)
						if err != nil {
							malformed = true
							break
						}
						tuples = append(tuples, t)
					}
					if malformed {
						s.Anomalies++
						continue
					}
					s.Offers = append(s.Offers, OfferFact{
						Pos:          pos,
						Signer:       e.Actor,
						Capabilities: o.Eligibility.Capabilities,
						Tiers:        o.Eligibility.Tiers,
						Tuples:       tuples,
						Expires:      strings.TrimSpace(o.Expires),
					})
				}
			}
			continue
		}
		if e.Verb == BudgetReserveVerb || e.Verb == BudgetSettleVerb || e.Verb == BudgetReleaseVerb {
			// Budget facts fold tolerantly as independent lists
			// (plans/os-cecac5de.md): raw pushes included, nothing
			// mutated — validity and effective closure are derived at
			// the consuming surfaces. A close citing a position that
			// is no reservation on the subject retroactively claims
			// spend history that never existed, so it stays an
			// anomaly, never a fact; malformed payloads fold to
			// nothing; facts on a subject nothing created bind
			// nothing.
			if s, ok := f.states[e.Subject]; ok {
				switch e.Verb {
				case BudgetReserveVerb:
					var p struct {
						Amount string `json:"amount"`
					}
					if json.Unmarshal(e.Payload, &p) == nil {
						if n, err := strconv.Atoi(strings.TrimSpace(p.Amount)); err == nil && n > 0 {
							s.Reservations = append(s.Reservations, ReservationFact{Pos: pos, Signer: e.Actor, Amount: n})
						}
					}
				case BudgetSettleVerb, BudgetReleaseVerb:
					var p struct {
						Reservation string `json:"reservation"`
						Actuals     string `json:"actuals"`
					}
					if json.Unmarshal(e.Payload, &p) == nil {
						cited, err := strconv.Atoi(strings.TrimSpace(p.Reservation))
						if err != nil {
							break
						}
						exists := false
						for _, r := range s.Reservations {
							if r.Pos == cited {
								exists = true
								break
							}
						}
						if !exists {
							s.Anomalies++
							break
						}
						kind, actuals := "release", 0
						if e.Verb == BudgetSettleVerb {
							kind = "settle"
							n, err := strconv.Atoi(strings.TrimSpace(p.Actuals))
							if err != nil || n < 0 {
								break
							}
							actuals = n
						}
						s.BudgetCloses = append(s.BudgetCloses, CloseFact{Pos: pos, Signer: e.Actor, Reservation: cited, Kind: kind, Actuals: actuals})
					}
				}
			}
			continue
		}
		if e.Verb == RunStartedVerb || e.Verb == RunSettledVerb || e.Verb == RunInterruptedVerb {
			// Run facts fold tolerantly as independent lists
			// (plans/os-1dad487d.md; interrupts per
			// plans/os-0f718b4e.md): raw pushes included, nothing
			// mutated. A fact citing a fence that is no applied claim
			// position retroactively invents a run window the ordering
			// disproves, so it counts an anomaly, never a fact.
			if s, ok := f.states[e.Subject]; ok {
				var p struct {
					Fence       string          `json:"fence"`
					Reservation string          `json:"reservation"`
					Units       string          `json:"units"`
					Lines       string          `json:"lines"`
					Tuple       json.RawMessage `json:"tuple"`
				}
				if json.Unmarshal(e.Payload, &p) != nil {
					continue
				}
				fence, err := strconv.Atoi(strings.TrimSpace(p.Fence))
				if err != nil {
					continue
				}
				if !s.ClaimFences[fence] {
					s.Anomalies++
					continue
				}
				if e.Verb == RunInterruptedVerb {
					for _, it := range s.Interrupts {
						if it.Fence == fence {
							s.Anomalies++
							break
						}
					}
					s.Interrupts = append(s.Interrupts, InterruptFact{Pos: pos, Signer: e.Actor, Fence: fence})
					continue
				}
				if e.Verb == RunStartedVerb {
					res, err := strconv.Atoi(strings.TrimSpace(p.Reservation))
					if err != nil {
						continue
					}
					// A raw duplicate on a once-per-fence fact stays
					// visible AND counts an anomaly (the plan's
					// duplicate-history posture; review finding on the
					// task PR).
					for _, st := range s.RunStarts {
						if st.Fence == fence {
							s.Anomalies++
							break
						}
					}
					// A malformed declaration is a malformed payload: no
					// fact, an anomaly (the run.settled posture), never a
					// start that merely lost its tuple.
					var declared *tuple.Tuple
					if len(p.Tuple) > 0 {
						t, err := tuple.Parse(p.Tuple)
						if err != nil {
							s.Anomalies++
							continue
						}
						declared = &t
					}
					s.RunStarts = append(s.RunStarts, RunStartFact{Pos: pos, Signer: e.Actor, Fence: fence, Reservation: res, Tuple: declared})
				} else {
					units, uerr := strconv.Atoi(strings.TrimSpace(p.Units))
					lines, lerr := strconv.Atoi(strings.TrimSpace(p.Lines))
					if uerr != nil || lerr != nil || units < 0 || lines < 0 {
						continue
					}
					for _, r := range s.Runs {
						if r.Fence == fence {
							s.Anomalies++
							break
						}
					}
					s.Runs = append(s.Runs, RunFact{Pos: pos, Signer: e.Actor, Fence: fence, Units: units, Lines: lines})
				}
			}
			continue
		}
		if !t.IsLifecycleVerb(e.Verb) {
			continue
		}
		s, ok := f.states[e.Subject]
		current := ""
		if ok {
			current = s.State
		}
		// Racing (plans/os-56bee171.md D2, D3; seed/6): a second
		// claim.taken while in_progress is a claim-scoped fact, never
		// a transition, and a racer's exit closes that racer's claim
		// alone except the first submission and the last departure.
		// Before seed/6 both fall through to the table, which refuses
		// them as it always did; the tolerant fold counts them.
		if ok && version.RacingApplies(e.V) {
			if e.Verb == "claim.taken" && current == "in_progress" && len(s.Claims) > 0 {
				if _, held := s.HolderFence(e.Actor); held {
					s.Anomalies++
					continue
				}
				s.Claims = append(s.Claims, Claim{Holder: e.Actor, Fence: pos})
				s.Claim = &s.Claims[0]
				s.Racing = true
				if s.PriorClaimants == nil {
					s.PriorClaimants = map[string]bool{}
				}
				s.PriorClaimants[e.Actor] = true
				s.LastClaim = pos
				if s.ClaimFences == nil {
					s.ClaimFences = map[int]bool{}
				}
				s.ClaimFences[pos] = true
				continue
			}
			if IsExit(e.Verb) {
				if cited, has := fenceCited(e.Payload); has {
					if fence, ferr := strconv.Atoi(cited); ferr == nil && s.ClaimScopedExit(e.Verb, fence) {
						if packet.Required(e.Verb) {
							if _, perr := packet.FromPayload(e.Subject, e.Payload); perr != nil {
								s.Anomalies++
							}
						}
						if e.Verb == "submission.made" {
							s.Submissions = append(s.Submissions, SubmissionFact{Pos: pos, Signer: e.Actor, PR: submissionPR(e)})
						}
						s.dropClaim(fence)
						continue
					}
				}
			}
		}
		to, err := t.Check(e.Subject, current, e.Verb)
		if err == nil && e.Verb == "contract.specified" && current == "ready" && !version.LevelsApply(e.V) {
			// The ready origin is a seed/4 row (plans/os-6bd9ffff.md
			// D4): a re-specification at an earlier position is the
			// transition the table of that version refused, so the
			// tolerant fold skips it visibly rather than applying an
			// acceptance the boundary of the day would not have.
			err = &InvalidTransitionError{Subject: e.Subject, From: current, Verb: e.Verb, Reason: RespecificationNeeds(e.V)}
		}
		if err != nil {
			if !ok {
				s = &SubjectState{}
				f.states[e.Subject] = s
				f.order = append(f.order, e.Subject)
			}
			s.Anomalies++
			continue
		}
		if !ok {
			s = &SubjectState{}
			f.states[e.Subject] = s
			f.order = append(f.order, e.Subject)
		}
		// Raw-pushed history can carry a legal transition with a
		// stale or missing fence citation, or a deliberate exit
		// without its handoff packet. Admission refuses both; the
		// tolerant fold applies the transition (the exit happened —
		// skipping it would wedge the subject on a dead holder) and
		// counts the violation visibly, never silently
		// (plans/os-5dc16a7c.md, plans/os-b07b0f59.md).
		if s.Claim != nil && t.Allows(current, e.Verb) && !IsMergeChain(e.Verb) {
			cited, _ := fenceCited(e.Payload)
			citedOK := false
			for _, c := range s.Claims {
				if cited == fmt.Sprintf("%d", c.Fence) {
					citedOK = true
				}
			}
			if !citedOK {
				s.Anomalies++
			}
		}
		if packet.Required(e.Verb) {
			if _, perr := packet.FromPayload(e.Subject, e.Payload); perr != nil {
				s.Anomalies++
			}
		}
		if e.Verb == t.birth {
			var filed struct {
				Tier    string `json:"tier"`
				Budget  string `json:"budget"`
				Routing string `json:"routing"`
				Eval    *struct {
					Name    string          `json:"name"`
					Tuple   json.RawMessage `json:"tuple"`
					Lesson  string          `json:"lesson"`
					Carrier string          `json:"carrier"`
					Kind    string          `json:"kind"`
				} `json:"eval"`
			}
			if json.Unmarshal(e.Payload, &filed) == nil {
				s.Tier = filed.Tier
				s.Budget = filed.Budget
				s.Routing = filed.Routing
				// The marker is read at seed/3 positions only, where
				// admission defines it; an advisory tuple that does
				// not parse is dropped and counted, never folded as
				// a partial configuration.
				if filed.Eval != nil && filed.Eval.Name != "" && version.EvalApplies(e.V) {
					info := &EvalInfo{Name: filed.Eval.Name, Lesson: filed.Eval.Lesson, Carrier: filed.Eval.Carrier, Kind: filed.Eval.Kind}
					if len(filed.Eval.Tuple) > 0 {
						if tu, terr := tuple.Parse(filed.Eval.Tuple); terr == nil {
							info.Tuple = &tu
						} else {
							s.Anomalies++
						}
					}
					s.Eval = info
				}
			}
		}
		if e.Verb == "contract.specified" {
			s.Specifications++
			if a, aerr := ParseAcceptance(e.Subject, e.Payload); aerr == nil {
				s.Acceptance = &AcceptanceInfo{Ref: a.Ref, Executable: a.Executable, Gated: !a.Executable || a.Gate != ""}
			} else {
				// Raw-pushed ungated or malformed acceptance:
				// tolerated in history, visibly counted, with what is
				// honestly derivable kept for the view.
				s.Anomalies++
				var raw struct {
					Acceptance Acceptance `json:"acceptance"`
				}
				if json.Unmarshal(e.Payload, &raw) == nil && raw.Acceptance.Ref != "" {
					s.Acceptance = &AcceptanceInfo{Ref: raw.Acceptance.Ref, Executable: raw.Acceptance.Executable, Gated: false}
				}
			}
		}
		s.State, s.Since = to, pos
		if e.Verb == "submission.made" {
			s.Submission = &SubmissionFact{Pos: pos, Signer: e.Actor, PR: submissionPR(e)}
			s.Submissions = []SubmissionFact{*s.Submission}
			s.Verdicts = nil
			// A new submission opens a new judgment window: the lockout
			// and the override both bind to the submission they judged
			// (plans/os-d2497eb7.md), and so does the forge's word on
			// the head (plans/os-0cd18799.md D1).
			s.SubmissionFails = nil
			s.Deferred = nil
			s.Override = nil
			s.Observation = nil
		}
		if e.Verb == ContractReturnedVerb {
			// What the return cited, for the ceiling and the view
			// (plans/os-0cd18799.md D4, D7): the fail verdict's position
			// or the red observation's.
			var r struct {
				Verdict     string `json:"verdict"`
				Observation string `json:"observation"`
			}
			fact := ReturnFact{Pos: pos, Verdict: -1, Observation: -1}
			if json.Unmarshal(e.Payload, &r) == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(r.Verdict)); err == nil && r.Verdict != "" {
					fact.Verdict = n
				}
				if n, err := strconv.Atoi(strings.TrimSpace(r.Observation)); err == nil && r.Observation != "" {
					fact.Observation = n
				}
			}
			s.Returns = append(s.Returns, fact)
		}
		if e.Verb == MergeObservedVerb {
			// Applied transitions only: a raw-pushed second observation
			// is an anomaly the loop above already skipped, so the fact
			// stays singular (plans/os-6cdc15be.md). An applied
			// observation with skipped chain links (no pass verdict, or
			// no request citing it) is tolerated like a packetless exit:
			// counted visibly, never silently, and reconciliation is
			// what surfaces it.
			var m struct {
				Merged string `json:"merged"`
			}
			_ = json.Unmarshal(e.Payload, &m)
			s.Merged = &MergeFact{Pos: pos, SHA: strings.TrimSpace(m.Merged)}
			if s.Racing && len(s.Claims) > 0 {
				settled := pos
				s.RaceSettled = &settled
			}
			// The verdict the chain rests on is the one the request
			// CITED, never whichever landed last: on a racing subject a
			// fail on the loser's submission follows the winner's pass
			// without unseating it, and reading the singular fact counts
			// an admitted settlement as an anomaly (the racing
			// enumeration's second finding, plans/os-873b5153.md D8).
			passChain := s.Requested != nil && s.CitedPass()
			overrideChain := s.Override != nil && s.Requested != nil &&
				s.Requested.CitedOverride == s.Override.Pos
			if !passChain && !overrideChain {
				s.Anomalies++
			}
		}
		// The escalation channel (plans/os-f781f0da.md). A raise sets
		// the standing question; the two answers clear it. Cancelling
		// counts as an answer because it IS one: the admission rule
		// makes it cite the escalation it closes, so the chain shows
		// which question the cancellation answered.
		if escalation.CarriesQuestion(e.Verb) {
			if q, present, qerr := escalation.FromPayload(e.Subject, e.Payload); present {
				if qerr != nil {
					// Tolerant fold: a raw-pushed malformed question is
					// an anomaly, never a fact. The transition still
					// applied above, so the subject is blocked with no
					// standing question, which the reap lint surfaces.
					s.Anomalies++
				} else {
					s.Escalation = &EscalationFact{
						Pos: pos, TS: e.TS, Raiser: e.Actor,
						Question: q.Question, Options: q.Options,
					}
				}
			} else if e.Verb == escalation.RaiseVerb {
				// A raise with no question at all: same tolerance.
				s.Anomalies++
			}
		}
		if e.Verb == escalation.AnswerVerb || e.Verb == "contract.cancelled" {
			s.Escalation = nil
		}
		if t.exclusive[e.Verb] {
			s.Claims = []Claim{{Holder: e.Actor, Fence: pos}}
			s.Claim = &s.Claims[0]
			if s.PriorClaimants == nil {
				s.PriorClaimants = map[string]bool{}
			}
			s.PriorClaimants[e.Actor] = true
			// The offer-consumption boundary (plans/os-c61c3392.md):
			// an applied claim consumes every offer at or before it,
			// so a taken offer never resurrects when the subject
			// re-readies inside its expiry window.
			s.LastClaim = pos
			if s.ClaimFences == nil {
				s.ClaimFences = map[int]bool{}
			}
			s.ClaimFences[pos] = true
		} else if s.Claim != nil && s.State != "in_progress" {
			// Every deliberate exit ends the claim window; the fence
			// dies with it. A racing subject's other claims outlive
			// the state change (the first submission enters review
			// while the other racers work), so only the exiting
			// racer's claim is dropped there.
			kept := false
			if s.Racing && IsExit(e.Verb) {
				if cited, has := fenceCited(e.Payload); has {
					if fence, ferr := strconv.Atoi(cited); ferr == nil {
						s.dropClaim(fence)
						kept = true
					}
				}
			} else if s.Racing && e.Verb == MergeObservedVerb {
				// Settlement: the other racers' claims outlive done as
				// settled-out claims, closed by their own exits or the
				// reaper (RaceSettled is set above).
				kept = true
			}
			if !kept {
				s.Claims, s.Claim = nil, nil
			}
		}
	}
	return f
}

// State returns a subject's folded state; ok is false when no
// lifecycle event ever named the subject. A subject whose every
// lifecycle event was anomalous has ok true and an empty State.
func (f *Fold) State(subject string) (SubjectState, bool) {
	s, ok := f.states[subject]
	if !ok {
		return SubjectState{}, false
	}
	return *s, true
}

// Subjects returns every folded subject in first-appearance order.
func (f *Fold) Subjects() []string { return append([]string(nil), f.order...) }

// StateAt folds one subject's lifecycle state out of a record prefix,
// the plan's named convenience over FoldRecords.
func (t *Table) StateAt(records []*event.Record, subject string) (SubjectState, bool) {
	return t.FoldRecords(records).State(subject)
}

// completeness maps intent.filed to the payload fields that must be
// present and non-empty (plans/os-d69a6c91.md); contract.specified
// deepened from presence to the structured acceptance rule
// (plans/os-73c00a50.md).
var completeness = map[string][]string{
	"intent.filed": {"intent", "tier", "budget", "routing"},
	// The return path's citation, a fail verdict or from seed/8 a red
	// observation (plans/os-d2497eb7.md; plans/os-0cd18799.md D4), is
	// exactly one of two fields, which presence-per-field cannot say:
	// the return rule in admit holds both presence and the citation.
}

// CheckCompleteness enforces the completeness rules for the verb's
// payload: field presence for filings, the structured acceptance
// field (gate included) for specifications.
func CheckCompleteness(verb, subject string, payload []byte) error {
	if verb == "contract.specified" {
		_, err := ParseAcceptance(subject, payload)
		return err
	}
	fields := completeness[verb]
	if len(fields) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		return &IncompleteError{Verb: verb, Subject: subject, Missing: fields}
	}
	var missing []string
	for _, f := range fields {
		raw, ok := m[f]
		if !ok || emptyJSON(raw) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return &IncompleteError{Verb: verb, Subject: subject, Missing: missing}
	}
	// The vocabularies, after presence (plans/os-be12ac16.md D2, D3):
	// the filed tier and budget class must be table members, byte for
	// byte. A filing outside either table shipped a contract every gate
	// treated as strictly as any, or one no worker could reserve
	// against; both are refused here naming what is legal.
	if verb == "intent.filed" {
		// Each field is decoded on its own: a value that is not a JSON
		// string (a number, an array) is no member of any table, and a
		// decode failure must refuse rather than skip the check (review
		// finding on the task PR).
		for _, f := range []struct {
			name  string
			known func() []string
			has   func(string) bool
		}{
			{"tier", Tiers, func(v string) bool { _, ok := Tier(v); return ok }},
			{"budget", BudgetClasses, func(v string) bool { _, ok := BudgetCapacity(v); return ok }},
		} {
			var v string
			if err := json.Unmarshal(m[f.name], &v); err != nil {
				return &VocabularyError{Verb: verb, Subject: subject, Field: f.name, Value: strings.TrimSpace(string(m[f.name])), Known: f.known()}
			}
			if !f.has(v) {
				return &VocabularyError{Verb: verb, Subject: subject, Field: f.name, Value: v, Known: f.known()}
			}
		}
	}
	return nil
}

// emptyJSON reports a value with no content: JSON null, an empty
// string, an empty object, or an empty array.
func emptyJSON(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == `""` || s == "{}" || s == "[]"
}

// fenceCited extracts a payload's fence citation for the tolerant
// fold; admission's strict twin lives in the fence rule.
// submissionPR reads the optional pull-request reference a
// submission.made names (plans/os-0cd18799.md D2): the merge.observed
// ref grammar, pr/<n> or <n>, defined from seed/8; at earlier
// positions the field is undefined and is not read.
func submissionPR(e *event.Event) string {
	if !version.ForgeChecksApply(e.V) {
		return ""
	}
	var p struct {
		PR string `json:"pr"`
	}
	if json.Unmarshal(e.Payload, &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.PR)
}

// ParseCheckObserved decodes check.observed's strict payload into the
// fact the fold records (plans/os-0cd18799.md D1): every field
// present but the thread count, the literals in their vocabularies,
// the head a full lowercase-hex commit, the count non-negative. The
// binding to the submission's head and the change requirement are
// the admission rule's, which needs the chain.
func ParseCheckObserved(pos int, e *event.Event) (CheckFact, error) {
	var p struct {
		PR      string `json:"pr"`
		Head    string `json:"head"`
		Checks  string `json:"checks"`
		Threads *int   `json:"unresolved_threads"`
		Review  string `json:"review"`
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return CheckFact{}, &ChainError{Subject: e.Subject, Verb: CheckObservedVerb, Reason: fmt.Sprintf("the payload is the strict object {pr, head, checks, unresolved_threads?, review}: %v", err)}
	}
	var missing []string
	for _, f := range []struct{ name, val string }{{"pr", p.PR}, {"head", p.Head}, {"checks", p.Checks}, {"review", p.Review}} {
		if strings.TrimSpace(f.val) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return CheckFact{}, &IncompleteError{Verb: CheckObservedVerb, Subject: e.Subject, Missing: missing}
	}
	head := strings.TrimSpace(p.Head)
	if !fullSHARE.MatchString(head) {
		return CheckFact{}, &ChainError{Subject: e.Subject, Verb: CheckObservedVerb, Reason: fmt.Sprintf("head %q is not a full lowercase-hex commit — the observer records which head the forge judged", p.Head)}
	}
	if !member(CheckStates, p.Checks) {
		return CheckFact{}, &VocabularyError{Verb: CheckObservedVerb, Subject: e.Subject, Field: "checks", Value: p.Checks, Known: CheckStates}
	}
	if !member(ReviewStates, p.Review) {
		return CheckFact{}, &VocabularyError{Verb: CheckObservedVerb, Subject: e.Subject, Field: "review", Value: p.Review, Known: ReviewStates}
	}
	if p.Threads != nil && *p.Threads < 0 {
		return CheckFact{}, &ChainError{Subject: e.Subject, Verb: CheckObservedVerb, Reason: fmt.Sprintf("unresolved_threads %d is negative — a count, or absent where the forge cannot say", *p.Threads)}
	}
	return CheckFact{Pos: pos, Signer: e.Actor, PR: strings.TrimSpace(p.PR), Head: head, Checks: p.Checks, Threads: p.Threads, Review: p.Review}, nil
}

// fullSHARE is the forge-fact wire form for a commit: a full
// lowercase-hex sha, the merge.observed convention.
var fullSHARE = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

func member(list []string, v string) bool {
	for _, m := range list {
		if m == v {
			return true
		}
	}
	return false
}

func fenceCited(payload []byte) (string, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		return "", false
	}
	raw, ok := m["fence"]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.TrimSpace(string(raw)), true
	}
	return s, true
}

// The plan.* vocabulary (the charter catalog's only two plan verbs;
// plans/os-16c1d142.md): proposals and merge observations are facts
// the fold and the submission gate consult.
const (
	PlanProposedVerb = "plan.proposed"
	PlanApprovedVerb = "plan.approved"
	// TrivialTier is the one distinguished tier value: the charter's
	// own term for the tier whose contracts submit without a plan.
	TrivialTier = "trivial"
)

// TierRow is what one tier answers at each gate (spec/tiers.md):
// the table Phase 10's tier system declares against, mirrored from the
// spec and pinned by test. HumanReview is declared and consumed by
// nobody yet: it is the column item 3 and the verdict pipeline's
// human-verdict routing read.
type TierRow struct {
	PlanRequired         bool
	SealedChecksRequired bool
	HumanReview          bool
	// Independence is the minimum level a verdict on the tier must
	// achieve (plans/os-99829835.md D1; spec/tiers.md), satisfied
	// by any achieved level at or above it.
	Independence Level
}

// Level is an independence level (SEED-NEXT.md §7 "Independence is
// failure-domain separation"; plans/os-99829835.md D1): ordered,
// achieved is the highest that holds, and a tier's requirement is
// satisfied by any level at or above it.
type Level string

const (
	// L1 is distinct keys and workspaces: the disjointness every
	// admitted verdict already has.
	L1 Level = "L1"
	// L2 is a distinct runtime tuple: the verifier declared a different
	// model provider or family, or a different harness, from the
	// window's admitted declaration.
	L2 Level = "L2"
	// L3 is deterministic-first verification on a distinct path: an
	// executable, gated acceptance whose receipt reproduces from the
	// verifier's own checkout.
	L3 Level = "L3"
)

// Levels lists the vocabulary in order: what a refusal names as legal.
func Levels() []Level { return []Level{L1, L2, L3} }

// ParseLevel reads a payload literal, byte for byte.
func ParseLevel(v string) (Level, bool) {
	for _, l := range Levels() {
		if string(l) == v {
			return l, true
		}
	}
	return "", false
}

// Rank is the level's place in the order; an unknown level ranks below
// every known one, so it satisfies nothing.
func (l Level) Rank() int {
	for i, k := range Levels() {
		if k == l {
			return i + 1
		}
	}
	return 0
}

// Satisfies reports whether the level meets a requirement: at or above
// it in the order, which is III.G's "L2 or L3" for one requirement.
func (l Level) Satisfies(required Level) bool {
	return l.Rank() > 0 && l.Rank() >= required.Rank()
}

// tierTable mirrors the normative table in spec/tiers.md. The
// three names are the charter's own words made concrete: trivial is its
// term, standard the ordinary contract every gate applies to, critical
// the "high-consequence" tier humans review.
var tierTable = map[string]TierRow{
	TrivialTier: {PlanRequired: false, SealedChecksRequired: false, HumanReview: false, Independence: L1},
	"standard":  {PlanRequired: true, SealedChecksRequired: true, HumanReview: false, Independence: L1},
	"critical":  {PlanRequired: true, SealedChecksRequired: true, HumanReview: true, Independence: L2},
}

// strictestRow is what every reader of a missing row takes: plan
// required, sealed checks required, human review. Absent knowledge is
// never fudged into a relaxation, the BudgetCapacity posture applied to
// authority, and the reason a raw-pushed unknown tier is judged exactly
// as it was before the vocabulary existed.
var strictestRow = TierRow{PlanRequired: true, SealedChecksRequired: true, HumanReview: true, Independence: L3}

// Tier resolves a filed tier to its row. An unknown tier has no row.
func Tier(name string) (TierRow, bool) {
	r, ok := tierTable[name]
	return r, ok
}

// Tiers lists the vocabulary, sorted: what a refusal names as legal.
func Tiers() []string {
	out := make([]string, 0, len(tierTable))
	for name := range tierTable {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TierGates resolves a tier to the row its readers consult, the
// strictest row for a tier the table does not know: the one accessor
// the three authority sites (the plan gate, the reconcile unsealed lint,
// verdict render's unsealed refusal) read, so none of them re-derives
// the rule from the constant.
func TierGates(name string) TierRow {
	if r, ok := Tier(name); ok {
		return r
	}
	return strictestRow
}

// InjectTier adds a row to the tier table and returns a restore func:
// the spec-mirror pin's hook for a planted code row the spec lacks.
// Production code never calls it.
func InjectTier(name string, row TierRow) func() {
	tierTable[name] = row
	return func() { delete(tierTable, name) }
}

// The observation summarization vocabulary (plans/os-2ff8dbf1.md;
// SEED-NEXT.md Part II §5): the ephemeral channel is summarized into
// ledger facts at material transitions, and these are the facts. Both
// are free verbs, never transitions: the pinned four in_progress
// exits stand.
// VerdictRenderedVerb is the verdict pipeline's first piped fact
// (plans/os-f6d2c267.md; spec/verdicts.md): admitted only on
// review subjects under L1 independence, changing no state — done
// still arrives only through merge.observed.
const VerdictRenderedVerb = "verdict.rendered"

// The reconciliation chain's remaining verbs (plans/os-6cdc15be.md;
// spec/reconciliation.md): merge.requested is a fact citing the
// pass verdict; merge.observed is the table's one transition to done,
// deepened to the observer's forge fact.
const (
	MergeRequestedVerb = "merge.requested"
	MergeObservedVerb  = "merge.observed"
)

// CheckSealedVerb is the sealed-checks commitment fact
// (plans/os-3128535a.md; spec/sealed-checks.md): admitted only
// while the subject is in ready with no prior claim, so the position
// ordering is the pre-existence proof. A fact, never a transition.
const CheckSealedVerb = "check.sealed"

// The red-verdict lockout's companions (plans/os-d2497eb7.md):
// contract.returned is the failed verdict's table row out of review
// (lifecycle.md's named extension point, resolved); merge.overridden
// is the operator's attributable substitute for a pass verdict, never
// a disguised one.
const (
	ContractReturnedVerb = "contract.returned"
	MergeOverriddenVerb  = "merge.overridden"
)

// CheckObservedVerb is the observer's forge fact about the submission
// under review (plans/os-0cd18799.md D1; spec/observations-forge.md;
// SEED-NEXT.md §II.4 "External facts"): the checks the forge ran on the
// submission's head, the review threads still open, the review state.
// A fact admitted only on review subjects from seed/8, changing no
// state; it carries counts and literals and never a line of forge
// prose.
const CheckObservedVerb = "check.observed"

// The observation's literal vocabularies (D1). A refusal names them.
var (
	CheckStates  = []string{"green", "red", "pending"}
	ReviewStates = []string{"approved", "changes_requested", "none"}
)

// CheckFact is the folded check.observed: the position and signer, the
// pull request and head it observed, the combined check state, the
// unresolved-thread count (nil where the forge cannot say: Forgejo has
// no thread resolution, spec/forges.md), and the review state.
type CheckFact struct {
	Pos     int
	Signer  string
	PR      string
	Head    string
	Checks  string
	Threads *int
	Review  string
}

// Red reports whether the observation says the submission is not
// mergeable as it stands (D3): a red check, an unresolved thread, or
// a review requesting changes. Pending alone is not red.
func (c CheckFact) Red() bool {
	return c.Checks == "red" || (c.Threads != nil && *c.Threads > 0) || c.Review == "changes_requested"
}

// Same reports whether two observations carry the same facts, which
// is what admission refuses a second time (D1): an unchanged poll
// appends nothing.
func (c CheckFact) Same(o CheckFact) bool {
	if c.PR != o.PR || c.Head != o.Head || c.Checks != o.Checks || c.Review != o.Review {
		return false
	}
	if (c.Threads == nil) != (o.Threads == nil) {
		return false
	}
	return c.Threads == nil || *c.Threads == *o.Threads
}

// ReturnFact is an applied contract.returned and what it cited: the
// fail verdict's position or the red observation's, the other -1
// (plans/os-0cd18799.md D4). The maintenance pass counts the
// observation-cited returns against its ceiling (D7).
type ReturnFact struct {
	Pos         int
	Verdict     int
	Observation int
}

// OfferPublishedVerb is the supervisor's eligibility-scoped invitation
// to claim (plans/os-c61c3392.md; spec/offers.md; SEED-NEXT.md
// §II.9): a fact admitted only on ready subjects, changing no state
// and granting nothing — the claim it invites settles at admission
// like any claim.
const OfferPublishedVerb = "offer.published"

// The budget-reservation facts (plans/os-cecac5de.md;
// spec/budgets.md; SEED-NEXT.md §II.9 "Budgets are reservations,
// not observations"): reserve is checked and decremented at
// admission, the one place with a serialized view; settle and release
// record close attempts whose effective closure is derived, never
// stored. All three are facts on in_progress subjects, changing no
// state.
const (
	BudgetReserveVerb = "budget.reserve"
	BudgetSettleVerb  = "budget.settle"
	BudgetReleaseVerb = "budget.release"
)

// budgetClasses maps the filed budget class to integer capacity
// units, mirroring the normative table in spec/budgets.md
// (pinned by test). The units are abstract in v0; Phase 7.3's adapter
// metering gives them meaning.
var budgetClasses = map[string]int{
	"small":  100,
	"medium": 1000,
	"large":  10000,
}

// BudgetClasses lists the class vocabulary in capacity order: what a
// filing refusal names as legal.
func BudgetClasses() []string {
	out := make([]string, 0, len(budgetClasses))
	for class := range budgetClasses {
		out = append(out, class)
	}
	sort.Slice(out, func(i, j int) bool {
		if budgetClasses[out[i]] != budgetClasses[out[j]] {
			return budgetClasses[out[i]] < budgetClasses[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// BudgetCapacity resolves a filed budget class to its capacity.
// Unknown classes have no capacity, so reserves against them refuse:
// absent knowledge is never fudged into spendable units.
func BudgetCapacity(class string) (int, bool) {
	c, ok := budgetClasses[class]
	return c, ok
}

// The execution-run facts (plans/os-1dad487d.md;
// spec/executors.md): run.started is the spending-verb table's
// first entry, the gated initiation that fences a run to its
// reservation before any executor provisions; run.settled is the
// once-per-fence aggregate of the run's metered observation lines.
const (
	RunStartedVerb = "run.started"
	RunSettledVerb = "run.settled"
)

// RunInterruptedVerb is the supervisor's safe-point preemption
// request (plans/os-0f718b4e.md; SEED-NEXT.md §II.9 "graceful-first"):
// an attributable ledger fact the running worker observes by polling
// (the canonical channel the wakeless drill proved sufficient) and
// answers by parking deliberately with its packet. It is NOT a
// spending verb: preemption is supervisory control and must not be
// budget-gated.
const RunInterruptedVerb = "run.interrupted"

// spendingVerbs is the data table of verbs that require an open,
// valid reservation on their subject (charter §II.9 "spending verbs
// require an admitted budget.reserve"). run.started is its first
// entry (plans/os-1dad487d.md): execution spend initiates through
// it, so no run provisions outside the reservation gate. Tests
// inject further entries to drill the gate in isolation.
var spendingVerbs = map[string]bool{RunStartedVerb: true}

// IsSpendingVerb reports whether the verb spends and therefore needs
// an open reservation at admission.
func IsSpendingVerb(verb string) bool { return spendingVerbs[verb] }

// InjectSpendingVerb adds a verb to the spending table and returns a
// restore func: the drill hook for a gate that ships without
// customers (plans/os-cecac5de.md D5). Production code never calls
// it.
func InjectSpendingVerb(verb string) func() {
	spendingVerbs[verb] = true
	return func() { delete(spendingVerbs, verb) }
}

// InjectBudgetClass adds a class to the capacity table and returns a
// restore func: the race drill's hook for the exact numbers the plan
// pins (two 8-unit reserves against a 10-unit class). Production code
// never calls it.
func InjectBudgetClass(class string, capacity int) func() {
	budgetClasses[class] = capacity
	return func() { delete(budgetClasses, class) }
}

// ChainError refuses a reconciliation-chain event whose links are
// missing or mismatched (spec/reconciliation.md): done is
// reachable only through verdict.rendered(pass), merge.requested, and
// merge.observed, in order. It rides the established shape-refusal
// exit mapping.
type ChainError struct {
	Subject string
	Verb    string
	Reason  string
}

func (e *ChainError) Error() string {
	return fmt.Sprintf("%s on %s refused: %s (spec/reconciliation.md)", e.Verb, e.Subject, e.Reason)
}

const (
	MilestoneVerb     = "progress.milestone"
	WedgeDeclaredVerb = "wedge.declared"
	// MinMilestoneSpacing is the v0 bounded-frequency default: the
	// minimum chain positions since the subject's last admitted
	// milestone. Spacing is measured in positions, never timestamps:
	// ts is human-readable metadata with no ordering authority, so a
	// time rule would be signer-gameable and skew-prone, while
	// position spacing is admission-derived, replay-deterministic,
	// and bounds the protected quantity, the subject's share of
	// ledger volume.
	MinMilestoneSpacing = 25
)

// MilestoneError refuses a progress.milestone that violates the
// monotonic or bounded-frequency rule at the summarization boundary.
// It rides the established shape-refusal exit mapping: the plan
// allocates no new code.
type MilestoneError struct {
	Subject string
	Reason  string
}

func (e *MilestoneError) Error() string {
	return fmt.Sprintf("progress.milestone on %s refused: %s (spec/observations.md)", e.Subject, e.Reason)
}

// CheckMilestone enforces the summarization boundary for a milestone
// landing at position tip: payload presence ({count, step}), a
// strictly advancing count against the fold's high-water mark, and
// the minimum position spacing since the subject's latest milestone.
func (f *Fold) CheckMilestone(subject string, tip int, payload []byte) error {
	var m struct {
		Count *int   `json:"count"`
		Step  string `json:"step"`
	}
	var missing []string
	if json.Unmarshal(payload, &m) != nil || m.Count == nil {
		missing = append(missing, "count")
	}
	if strings.TrimSpace(m.Step) == "" {
		missing = append(missing, "step")
	}
	if len(missing) > 0 {
		return &IncompleteError{Verb: MilestoneVerb, Subject: subject, Missing: missing}
	}
	last, ok := f.milestones[subject]
	if !ok {
		return nil
	}
	if *m.Count <= last.Count {
		return &MilestoneError{Subject: subject, Reason: fmt.Sprintf("count %d does not advance the last admitted milestone count %d — progress counts are monotonic, never clock-derived", *m.Count, last.Count)}
	}
	if tip-last.Pos < MinMilestoneSpacing {
		return &MilestoneError{Subject: subject, Reason: fmt.Sprintf("position spacing %d since the milestone at position %d is under the minimum %d — milestones are coarse, bounded-frequency summaries", tip-last.Pos, last.Pos, MinMilestoneSpacing)}
	}
	return nil
}

// CheckWedgeShape enforces wedge.declared's presence payload
// ({observed, count, since}): the visible wedge condition recorded
// durably, changing no state.
func CheckWedgeShape(subject string, payload []byte) error {
	var m struct {
		Observed string `json:"observed"`
		Count    *int   `json:"count"`
		Since    string `json:"since"`
	}
	if json.Unmarshal(payload, &m) != nil {
		return &IncompleteError{Verb: WedgeDeclaredVerb, Subject: subject, Missing: []string{"observed", "count", "since"}}
	}
	var missing []string
	if strings.TrimSpace(m.Observed) == "" {
		missing = append(missing, "observed")
	}
	if m.Count == nil {
		missing = append(missing, "count")
	}
	if strings.TrimSpace(m.Since) == "" {
		missing = append(missing, "since")
	}
	if len(missing) > 0 {
		return &IncompleteError{Verb: WedgeDeclaredVerb, Subject: subject, Missing: missing}
	}
	return nil
}

// planAnchor extracts a payload's plan anchor.
func planAnchor(payload []byte) (string, bool) {
	var m struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(payload, &m); err != nil {
		return "", false
	}
	return m.Plan, m.Plan != ""
}

// planDigestRE is the digest's shape: the lowercase-hex sha256 of the
// plan bytes at the anchor, the figure seed plan propose derives from
// the repository (plans/os-6bd9ffff.md D5).
var planDigestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// planDigest extracts a payload's well-formed plan digest.
func planDigest(payload []byte) (string, bool) {
	var m struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(payload, &m); err != nil {
		return "", false
	}
	return m.Digest, planDigestRE.MatchString(m.Digest)
}

// PlanDigestNeeds is the reason a plan verb carrying a digest refuses
// at a version before seed/4: the field is defined there
// (plans/os-6bd9ffff.md D5, D7), and a seed/3 validator would judge
// the record without it.
func PlanDigestNeeds(verb, active string) string {
	return fmt.Sprintf("the %s carries a plan digest and the chain is at %s: plan digests activate at %s (spec/plans.md)", verb, active, version.Seed4)
}

// PlanRequiredError is the plan-gate refusal (exit 16 plan_required):
// claiming an unplanned contract authorizes planning only, so a
// submission above the trivial tier needs an approved plan and must
// cite the plan anchor it implements. Missing names which half.
type PlanRequiredError struct {
	Subject string
	Tier    string
	Missing string
}

func (e *PlanRequiredError) Error() string {
	return fmt.Sprintf("submission on %s (tier %q) refused: %s — above the trivial tier, claiming an unplanned contract authorizes planning only; merge the plan PR (observed as plan.approved) and cite its anchor (spec/plans.md)", e.Subject, e.Tier, e.Missing)
}

// CheckPlanGate enforces the submission gate: above the trivial tier,
// the subject carries an admitted plan.approved and the submission
// payload cites the plan anchor it implements. The ancestry binding
// (the implementation actually built on the approved plan) is
// Phase 6's receipt computation, the named closing item.
func (f *Fold) CheckPlanGate(subject, tier string, payload []byte) error {
	if !TierGates(tier).PlanRequired {
		return nil
	}
	approved, ok := f.PlanApproved(subject)
	if !ok {
		return &PlanRequiredError{Subject: subject, Tier: tier, Missing: "no plan.approved on the subject"}
	}
	cited, ok := planAnchor(payload)
	if !ok {
		return &PlanRequiredError{Subject: subject, Tier: tier, Missing: "the submission must cite the plan anchor it implements ({\"plan\": \"<path @ commit>\"})"}
	}
	// Citation means THE approved plan, anchor for anchor: an approval
	// admits one exact revision, and citing any other leaves the
	// receipt verifier holding an anchor nothing vouched for.
	if cited != approved {
		return &PlanRequiredError{Subject: subject, Tier: tier, Missing: fmt.Sprintf("the cited plan %q is not the approved plan %q", cited, approved)}
	}
	return nil
}

// CheckPlanEventShape enforces payload presence for the plan.* verbs
// at the active version v: a proposal names the plan artifact anchor;
// an approval names the plan anchor and the merged PR (both combined
// anchors, the external-fact observation posture); and from seed/4
// both carry the plan's content digest, the sha256 of the plan bytes
// at the anchor (plans/os-6bd9ffff.md D5), required there and refused
// before it, since a seed/3 validator's shape has no such field.
func CheckPlanEventShape(v, verb, subject string, payload []byte) error {
	if verb != PlanProposedVerb && verb != PlanApprovedVerb {
		return nil
	}
	var m struct {
		Plan   string          `json:"plan"`
		PR     string          `json:"pr"`
		Digest json.RawMessage `json:"digest"`
	}
	_ = json.Unmarshal(payload, &m)
	var missing []string
	if m.Plan == "" {
		missing = append(missing, "plan")
	}
	if verb == PlanApprovedVerb && m.PR == "" {
		missing = append(missing, "pr")
	}
	if version.LevelsApply(v) {
		if _, ok := planDigest(payload); !ok {
			missing = append(missing, "digest")
		}
	} else if len(m.Digest) > 0 {
		return &ChainError{Subject: subject, Verb: verb, Reason: PlanDigestNeeds(verb, v)}
	}
	if len(missing) > 0 {
		return &IncompleteError{Verb: verb, Subject: subject, Missing: missing}
	}
	// The approval's pr is the merged plan PR AT its merge commit, the
	// external-fact observation posture: a bare name carries no
	// revision to hold the approval to (review finding on the
	// os-6bd9ffff task PR).
	if verb == PlanApprovedVerb && !isAnchor(m.PR) {
		return &ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("pr %q is not \"<pr> @ <merged-commit>\": an approval observes the plan PR's merge at a revision", m.PR)}
	}
	return nil
}

// isAnchor reports the combined anchor form "<name> @ <commit>" with
// both halves present.
func isAnchor(s string) bool {
	name, commit, ok := strings.Cut(s, " @ ")
	return ok && strings.TrimSpace(name) != "" && strings.TrimSpace(commit) != ""
}

// tierOrder is the vocabulary from least to most consequential, the
// order tiers.md lists it in: what a ceiling (plans/os-0d4f2af3.md D3)
// compares by. Pinned against the table's keys by drill.
var tierOrder = []string{"trivial", "standard", "critical"}

// TierOrder is the vocabulary from least to most consequential.
func TierOrder() []string { return append([]string{}, tierOrder...) }

// TierRank is a tier's position in that order; ok is false for a tier
// the vocabulary does not know, which callers treat as the strictest.
func TierRank(name string) (int, bool) {
	for i, t := range tierOrder {
		if t == name {
			return i, true
		}
	}
	return len(tierOrder), false
}

// TierAbove reports whether tier a is more consequential than tier b;
// an unknown tier is above every known one, the strictest reading.
func TierAbove(a, b string) bool {
	ra, _ := TierRank(a)
	rb, _ := TierRank(b)
	return ra > rb
}

// ActiveFences is every active claim's fence, in order.
func (s SubjectState) ActiveFences() []int {
	out := make([]int, 0, len(s.Claims))
	for _, c := range s.Claims {
		out = append(out, c.Fence)
	}
	return out
}

// ClaimByFence finds the active claim at a fence.
func (s SubjectState) ClaimByFence(fence int) (Claim, bool) {
	for _, c := range s.Claims {
		if c.Fence == fence {
			return c, true
		}
	}
	return Claim{}, false
}

// HolderFence is the fence of the active claim an actor holds.
func (s SubjectState) HolderFence(actor string) (int, bool) {
	for _, c := range s.Claims {
		if c.Holder == actor {
			return c.Fence, true
		}
	}
	return 0, false
}

// SubmissionAt finds a current-window submission by position.
func (s SubjectState) SubmissionAt(pos int) (SubmissionFact, bool) {
	for _, sub := range s.Submissions {
		if sub.Pos == pos {
			return sub, true
		}
	}
	if s.Submission != nil && s.Submission.Pos == pos {
		return *s.Submission, true
	}
	return SubmissionFact{}, false
}

// VerdictAt finds a current-window verdict by position.
func (s SubjectState) VerdictAt(pos int) (VerdictFact, bool) {
	for _, v := range s.Verdicts {
		if v.Pos == pos {
			return v, true
		}
	}
	if s.Verdict != nil && s.Verdict.Pos == pos {
		return *s.Verdict, true
	}
	return VerdictFact{}, false
}

// ClaimScopedExit reports whether a deliberate exit citing a fence
// closes that racer's claim alone, leaving the subject's state where it
// is (plans/os-56bee171.md D3): on a racing subject every exit is
// claim-scoped except the first submission, which enters review, and
// the last racer's departure with no submission yet, which the table
// moves; after the race is settled every exit is claim-scoped. A
// subject that never raced has no claim-scoped exit.
func (s SubjectState) ClaimScopedExit(verb string, fence int) bool {
	if !s.Racing {
		return false
	}
	if _, active := s.ClaimByFence(fence); !active {
		return false
	}
	if s.RaceSettled != nil {
		return true
	}
	if verb == "submission.made" {
		return s.Submission != nil
	}
	return len(s.Claims) > 1 || s.Submission != nil
}

// VerdictFacts is every verdict the fold applied to this subject, oldest
// first. A state carrying the singular fact alone reads as that fact: a
// hand-built state (a classifier's table test) is judged as it was
// before the list existed, and a folded one always carries both.
func (s *SubjectState) VerdictFacts() []VerdictFact {
	if len(s.Verdicts) == 0 && s.Verdict != nil {
		return []VerdictFact{*s.Verdict}
	}
	return s.Verdicts
}

// CitedPass reports whether the request standing on this subject cites a
// pass verdict. The fold keeps every verdict it applied, so the position
// the request named is the one to judge, whatever landed after it.
func (s *SubjectState) CitedPass() bool {
	if s.Requested == nil {
		return false
	}
	for _, v := range s.VerdictFacts() {
		if v.Pos == s.Requested.CitedVerdict && v.Verdict == "pass" {
			return true
		}
	}
	return false
}

// IsMergeChain reports whether the verb is one of the two reconciliation
// chain steps. Both are subject-scoped rather than claim-scoped: their
// payloads are strict objects with no fence slot
// (spec/reconciliation.md), so what they rest on is the subject's
// verdict, never anyone's window. Admission's fence rule exempts them and
// the fence-anomaly counter below does too, or the one rule's two
// consumers disagree on a racing subject where a rival racer is still
// holding at review (plans/os-873b5153.md D8).
func IsMergeChain(verb string) bool {
	return verb == MergeRequestedVerb || verb == MergeObservedVerb
}

// IsExit reports the four deliberate exits from a claim.
func IsExit(verb string) bool {
	return verb == "claim.released" || verb == "claim.parked" || verb == "claim.reaped" || verb == "submission.made"
}

func (s *SubjectState) dropClaim(fence int) {
	kept := s.Claims[:0]
	for _, c := range s.Claims {
		if c.Fence != fence {
			kept = append(kept, c)
		}
	}
	s.Claims = kept
	if len(s.Claims) > 0 {
		s.Claim = &s.Claims[0]
	} else {
		s.Claim = nil
	}
}

// Requests is every request the fold applied, in chain order.
func (f *Fold) Requests() []IngressFact { return append([]IngressFact(nil), f.requests...) }

// RequestAt finds a request by its position.
func (f *Fold) RequestAt(pos int) (IngressFact, bool) {
	for _, r := range f.requests {
		if r.Pos == pos {
			return r, true
		}
	}
	return IngressFact{}, false
}

func (f *Fold) foldRequest(pos int, e *event.Event) {
	if !version.RequestsApply(e.V) {
		f.RequestAnomalies++
		return
	}
	switch e.Verb {
	case "request.filed":
		var p struct {
			Origin string `json:"origin"`
			Kind   string `json:"kind"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.Origin == "" || p.Kind == "" {
			f.RequestAnomalies++
			return
		}
		f.requests = append(f.requests, IngressFact{Pos: pos, TS: e.TS, Signer: e.Actor, Subject: e.Subject, Origin: p.Origin, Kind: p.Kind})
	case "request.answered":
		var p struct {
			Request string `json:"request"`
			Outcome string `json:"outcome"`
			Intent  string `json:"intent"`
		}
		if json.Unmarshal(e.Payload, &p) != nil {
			f.RequestAnomalies++
			return
		}
		cited, err := strconv.Atoi(strings.TrimSpace(p.Request))
		if err != nil {
			f.RequestAnomalies++
			return
		}
		for i := range f.requests {
			r := &f.requests[i]
			if r.Pos == cited && r.Answered == nil && r.Subject == e.Subject {
				at := pos
				r.Answered, r.Outcome, r.Answerer = &at, p.Outcome, e.Actor
				if n, ierr := strconv.Atoi(strings.TrimSpace(p.Intent)); ierr == nil {
					r.Intent = n
				} else {
					r.Intent = -1
				}
				return
			}
		}
		f.RequestAnomalies++
	}
}
