// Package admit is the admission rule set as a pure library
// (docs/build-plan.md Phase 2 item 1; plans/os-3898f232.md): one
// ordered set of rules importable by the cooperative client (2.2), the
// seed-admit pre-receive hook (2.3), and later the forge service, so
// postures differ in where the rules run, never in which rules run.
// Phase 2 carries the rules that exist so far: halt, shape, actor
// signature against the genesis governance root (the Phase 3 keyring
// projection replaces the resolver, not the rule), protocol version
// discipline, and payload classification. Capability rules land in
// Phase 3, fences in Phase 5, budget reservations in Phase 7, each as an
// appended rule; nothing here special-cases them in advance.
package admit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/classify"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/erasure"
	"github.com/shaunlmason/open-seed-v2/internal/escalation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/imported"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/request"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Context is everything a rule may consult, derived by one verified
// replay of the chain. Rules are pure functions of (context, record).
type Context struct {
	Count     int
	Tip       string
	Active    string
	Halt      halt.State
	Resolve   ledger.Resolver
	Keyring   *keyring.State
	Table     *transition.Table
	Lifecycle *transition.Fold
	Supported map[string]bool
	// Records is the verified prefix the context was computed from:
	// the budget rule's position-accurate validity replays need it
	// (plans/os-cecac5de.md D4).
	Records []*event.Record
	// Declaration is the deployment declaration the policy rules read
	// (plans/os-0d4f2af3.md D3, D4): the agent claim ceiling and the
	// declared squads. Nil means no declaration, and the rules are
	// no-ops — admission policy, never chain validity, so a chain never
	// changes meaning because of a file beside it.
	Declaration *posture.Config
}

// Option configures context construction.
type Option func(*options)

type options struct {
	supported   []string
	declaration *posture.Config
}

// WithDeclaration supplies the deployment declaration the policy rules
// read. Absent, the rules do nothing: today's behavior byte for byte.
func WithDeclaration(cfg *posture.Config) Option {
	return func(o *options) { o.declaration = cfg }
}

// WithSupportedVersions declares the protocol versions this admission
// point accepts, mirroring ledger.WithSupportedVersions. The default is
// the build's own protocol version.
func WithSupportedVersions(vs ...string) Option {
	return func(o *options) { o.supported = vs }
}

// ContextAt builds the admission context with a single VerifyFromGenesis
// replay (records observed via ledger.WithObserver feed the halt
// projection). A chain that does not verify yields no context: admission
// never grows an invalid chain.
func ContextAt(store *ledger.Store, opts ...Option) (*Context, error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, fmt.Errorf("admission context: %w", err)
	}
	supported := map[string]bool{}
	for _, v := range version.Supported() {
		supported[v] = true
	}
	var vopts []ledger.VerifyOption
	if len(o.supported) > 0 {
		vopts = append(vopts, ledger.WithSupportedVersions(o.supported...))
		supported = map[string]bool{}
		for _, v := range o.supported {
			supported[v] = true
		}
	}
	var records []*event.Record
	vopts = append(vopts, ledger.WithObserver(func(pos int, rec *event.Record) {
		records = append(records, rec)
	}))
	rep, err := store.VerifyFromGenesis(resolve, vopts...)
	if err != nil {
		return nil, err
	}
	ring, _, err := keyring.StateAt(records)
	if err != nil {
		// A verified chain cannot fail the keyring projection: the
		// replay above already applied the same transitions.
		return nil, err
	}
	if keyring.Applies(rep.ActiveVersion) && ring.Seeded() {
		// From seed/1 the keyring is the resolver: standing decides who
		// signs at the tip (the Phase 3 projection replacing the genesis
		// resolver, exactly as the package doc promised).
		resolve = ring.Resolver()
	}
	table, err := transition.Default()
	if err != nil {
		// The embedded table failing self-validation is a build
		// defect, not a chain condition; admission refuses outright.
		return nil, fmt.Errorf("admission context: %w", err)
	}
	return &Context{
		Count:       rep.Count,
		Tip:         rep.Tip,
		Active:      rep.ActiveVersion,
		Halt:        halt.StateAt(records),
		Resolve:     resolve,
		Keyring:     ring,
		Table:       table,
		Lifecycle:   table.FoldRecords(records),
		Supported:   supported,
		Records:     records,
		Declaration: o.declaration,
	}, nil
}

// ContextOver builds the admission context over an ALREADY-VERIFIED
// prefix of records, without a store: the frame a lane decided from
// at every position of one chain (plans/os-6bd9ffff.md D1), which the
// trajectory recorder and replayer derive from the same rules
// admission enforces. Verification is the caller's: a prefix of a
// chain VerifyFromGenesis accepted is itself verified, and nothing
// here re-checks a signature. The resolver, the keyring, the halt
// state, the active version and the fold are the ones ContextAt
// derives, computed from the records rather than replayed from disk,
// so the two agree position for position (pinned by drill). An empty
// prefix has no genesis and no context.
func ContextOver(records []*event.Record, opts ...Option) (*Context, error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if len(records) == 0 {
		return nil, genesis.ErrNoGenesis
	}

	payload, err := genesis.Parse(records[0])
	if err != nil {
		return nil, fmt.Errorf("admission context: %w: %v", genesis.ErrNoGenesis, err)
	}
	resolve, err := payload.Resolver(records[0].Event.Actor)
	if err != nil {
		return nil, err
	}
	ring, active, err := keyring.StateAt(records)
	if err != nil {
		return nil, err
	}
	if keyring.Applies(active) && ring.Seeded() {
		resolve = ring.Resolver()
	}
	table, err := transition.Default()
	if err != nil {
		return nil, fmt.Errorf("admission context: %w", err)
	}
	tip, err := records[len(records)-1].Event.Hash()
	if err != nil {
		return nil, err
	}
	supported := map[string]bool{}
	for _, v := range version.Supported() {
		supported[v] = true
	}
	return &Context{
		Count:       len(records),
		Tip:         tip,
		Active:      active,
		Halt:        halt.StateAt(records),
		Resolve:     resolve,
		Keyring:     ring,
		Table:       table,
		Lifecycle:   table.FoldRecords(records),
		Supported:   supported,
		Records:     records,
		Declaration: o.declaration,
	}, nil
}

// Refusal is an admission refusal naming the rule that refused. It
// unwraps to the rule's own typed error, so the envelope layer keeps the
// established exit mapping (halted 7, chain 8, classification 9,
// version 10).
type Refusal struct {
	Rule string
	Err  error
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("admission refused by rule %s: %v", r.Rule, r.Err)
}

func (r *Refusal) Unwrap() error { return r.Err }

// ClassificationError carries the lint violations for a refused payload;
// its message matches the CLI's exit-9 rendering.
type ClassificationError struct{ Violations []classify.Violation }

func (e *ClassificationError) Error() string {
	parts := make([]string, 0, len(e.Violations))
	for _, v := range e.Violations {
		parts = append(parts, fmt.Sprintf("%s: %s", v.Pointer, v.Rule))
	}
	return "payload fails data classification: " + strings.Join(parts, "; ")
}

// OutOfGrantError is the capability refusal the charter names
// (SEED-NEXT.md Part II "Capabilities"): the actor holds none of the
// capabilities the verb accepts. Grants are events (actor.granted),
// checked at admission on every verb against the vocabulary in
// spec/actors.md; governance roots hold operator implicitly. It
// maps to exit 14 out_of_grant (spec/envelope.md).
type OutOfGrantError struct {
	Actor    string
	Verb     string
	Accepted []string
	// Drift is the qualification refinement of the same family
	// (plans/os-8e53ffd9.md D4): the actor holds the capability, but
	// the run declares a configuration none of its qualified grants
	// cite. Same exit, same wire code, a finer message: a refinement
	// inside a family keeps the family's exit (spec/envelope.md).
	Drift *Drift
}

// Drift names which field of the runtime tuple moved, and both values.
type Drift struct {
	Holder string
	Field  string
	Have   string
	Cited  []string
}

func (e *OutOfGrantError) Error() string {
	if e.Drift != nil && len(e.Drift.Cited) == 0 {
		return fmt.Sprintf("actor %s has been qualified for %s and every cited configuration is disqualified: no admissible runtime tuple remains, and a disqualified holder never falls back to the bridge — a passing eval re-qualifies it (spec/evals.md)",
			e.Drift.Holder, e.Verb)
	}
	if e.Drift != nil {
		return fmt.Sprintf("actor %s is qualified for %s under %d cited tuple(s) and the run declares a materially different configuration: %s is %q, and no cited tuple carries it (the closest cites %s) — an actor invoking a different configuration than its grant cites is out of grant (SEED-NEXT.md §II.5; spec/qualification.md)",
			e.Drift.Holder, e.Verb, len(e.Drift.Cited), e.Drift.Field, e.Drift.Have, strings.Join(e.Drift.Cited, " | "))
	}
	return fmt.Sprintf("actor %s is not granted any of [%s], which %s accepts — grants are capability data checked at admission (plans/os-3979d48b.md)", e.Actor, strings.Join(e.Accepted, ", "), e.Verb)
}

// FenceError is the stale-or-missing fence refusal (exit 6
// fenced_out, the v1-continuity row): on a subject with an active
// claim, the event either had to cite the fence and did not, cited a
// fence that is not the active one, or cited a fence when no claim is
// active. Cited is empty for a missing citation; Active is -1 when no
// claim is active (plans/os-5dc16a7c.md).
type FenceError struct {
	Subject string
	Cited   string
	Active  int
	Holder  string
}

func (e *FenceError) Error() string {
	if e.Active < 0 {
		return fmt.Sprintf("event on %s cites fence %s but no claim is active — a fence dies with its claim window", e.Subject, e.Cited)
	}
	if e.Cited == "" {
		return fmt.Sprintf("event on %s must cite the active fence %d held by %s — claim-scoped events carry {\"fence\": \"<position>\"}", e.Subject, e.Active, e.Holder)
	}
	return fmt.Sprintf("event on %s cites stale fence %s; the active fence is %d, held by %s", e.Subject, e.Cited, e.Active, e.Holder)
}

// ContentionError is the exclusivity refusal (exit 2 contention): the
// subject is already held. The fence is the position the active claim
// was taken at, so the loser learns who holds and since when.
type ContentionError struct {
	Subject string
	Holder  string
	Fence   int
	// Racers and Cap are set on a racing squad's contract at its cap:
	// every active claim, and the declared limit.
	Racers []transition.Claim
	Cap    int
}

func (e *ContentionError) Error() string {
	if e.Cap > 0 {
		parts := make([]string, 0, len(e.Racers))
		for _, r := range e.Racers {
			parts = append(parts, fmt.Sprintf("%s (fence %d)", r.Holder, r.Fence))
		}
		return fmt.Sprintf("contract %s is at its squad's racing cap of %d: held by %s — a racer joins only below the cap (seed.json guardrails.squads.<squad>.racing; spec/lifecycle.md, Racing)", e.Subject, e.Cap, strings.Join(parts, ", "))
	}
	return fmt.Sprintf("contract %s is already claimed by %s (fence %d, held since position %d) — exclusivity is granted at admission, one claim at a time", e.Subject, e.Holder, e.Fence, e.Fence)
}

// RaceSettledError is a settled-out racer's act after the race closed
// (plans/os-56bee171.md D3): the first verified success settled the
// contract at SettledAt, the actor's claim outlived it, and only its
// own deliberate exit is still admissible.
type RaceSettledError struct {
	Subject   string
	Actor     string
	Fence     int
	SettledAt int
}

func (e *RaceSettledError) Error() string {
	return fmt.Sprintf("contract %s was settled at position %d while %s still held fence %d — a settled-out racer's next act refuses; its own deliberate exit (submission, release, park) still admits, and the reaper reaps what does not exit (spec/lifecycle.md, Racing)", e.Subject, e.SettledAt, e.Actor, e.Fence)
}

// LevelShortError is the level-short refusal (exit 17 not_independent
// refining level_short; plans/os-99829835.md D3): the level the record
// supports for this verdict does not meet the level the subject's tier
// requires. The family's answer, "this verifier cannot judge this
// contract", is already right; the refining word says it is the
// configuration rather than the key.
type LevelShortError struct {
	Subject, Tier      string
	Required, Achieved transition.Level
}

func (e *LevelShortError) Error() string {
	return fmt.Sprintf("verdict on %s refused: tier %q requires independence %s and the record supports %s — a verifier's configuration that cannot separate from the work's cannot judge a high-consequence contract (spec/verdicts.md, spec/tiers.md)", e.Subject, e.Tier, e.Required, e.Achieved)
}

// The level half of a pass's authentication is installed into the
// curation package at init (plans/os-96850e5a.md, review fixes): the
// fold's promotion replay authenticates the adversarial pass through
// curation.AuthenticPass, and the level rule must be the same one the
// verdict rule and the merge chain apply, not a second copy.
func init() {
	curation.PassLevelCheck = func(records []*event.Record, table *transition.Table, subject string, s transition.SubjectState, fact transition.VerdictFact) bool {
		return levelBoundary(&Context{Records: records, Table: table}, subject, "", s, fact) == nil
	}
}

// LevelAchieved computes the independence level the record supports
// for a verdict on the subject (plans/os-99829835.md D1, D3): the
// highest that holds. L3 when the acceptance is executable and gated,
// the deterministic-first path whose reproduction half is left to
// recomputation since the boundary runs nothing; L2 when the
// verifier's declared tuple differs from the window's admitted
// declaration in model provider or family, or in harness name
// (versions, principal, tool policy and environment do not count); L1
// otherwise, the disjointness every admitted verdict already has.
// Shared by the verdict rule, the merge chain, render and reconcile,
// so the four never disagree on what a record supports.
func LevelAchieved(records []*event.Record, table *transition.Table, subject string, s transition.SubjectState, declared *tuple.Tuple) transition.Level {
	if s.Acceptance != nil && s.Acceptance.Executable && s.Acceptance.Gated {
		return transition.L3
	}
	if declared != nil {
		if window := submissionDeclaration(records, table, subject, s); window != nil {
			if tuple.SeparatesModel(declared.Model, window.Model) || tuple.SeparatesHarness(declared.Harness, window.Harness) {
				return transition.L2
			}
		}
	}
	return transition.L1
}

// submissionDeclaration is the tuple the claim window that produced the
// bound submission declared through its admitted run.started, nil
// where no submission stands, the submission cites no window, or no
// admitted start declared one: windowDeclaration's lookup, taken from
// records and the table so reconcile can share it without a Context.
func submissionDeclaration(records []*event.Record, table *transition.Table, subject string, s transition.SubjectState) *tuple.Tuple {
	if s.Submission == nil {
		return nil
	}
	fence, _, ok := submissionWindow(records, subject, s.Submission.Pos)
	if !ok {
		return nil
	}
	for i := range s.RunStarts {
		st := s.RunStarts[i]
		if st.Fence == fence && RunStartValid(records, table, subject, st) {
			return st.Tuple
		}
	}
	return nil
}

// levelBoundary reapplies the level and the tier to a folded verdict
// (plans/os-99829835.md D3, review finding on the plan PR): a
// raw-pushed verdict whose recorded level the record does not support,
// or which is short of the subject's tier, authenticates nothing, so
// it cannot be laundered into done through an admitted merge chain and
// the red-verdict lockout does not count it. A verdict at a version
// before the levels keeps that version's judgment.
func levelBoundary(c *Context, subject, verb string, s transition.SubjectState, fact transition.VerdictFact) *transition.ChainError {
	if !fact.Levels {
		return nil
	}
	achieved := LevelAchieved(c.Records, c.Table, subject, s, fact.Tuple)
	if fact.Independence != string(achieved) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d records independence %q and the record supports %s — a level the record does not support is not launderable through the admitted chain", fact.Pos, fact.Independence, achieved)}
	}
	if required := transition.TierGates(s.Tier).Independence; !achieved.Satisfies(required) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d achieved %s and tier %q requires %s — a level short of the tier authenticates nothing", fact.Pos, achieved, s.Tier, required)}
	}
	return nil
}

// NotIndependentError is the L1 independence refusal (exit 17
// not_independent, spec/verdicts.md): the verdict signer is an
// implementing key on this contract — a claimant, past or present, or
// the bound submission's signer. Distinct from OutOfGrantError:
// capability is global, independence is per-contract, and a perfectly
// good verdict grant does not cure being disqualified here.
type NotIndependentError struct {
	Subject string
	Actor   string
	Role    string
}

func (e *NotIndependentError) Error() string {
	return fmt.Sprintf("verdict on %s refused: signer %s is an implementing key on this contract (%s) — L1 independence separates failure domains, and a verdict grant does not cure disqualification on the contract being judged (spec/verdicts.md)", e.Subject, e.Actor, e.Role)
}

// VerdictError is the verdict.rendered shape-and-binding refusal: a
// malformed payload, an illegal literal, or a citation of anything but
// the bound submission. It rides the established shape-refusal exit
// mapping, like the milestone rule's.
type VerdictError struct {
	Subject string
	Reason  string
	// Code is the refinement the rubric derivation names
	// (plans/os-2e34f66a.md D3): rubric_red or human_verdict under
	// checks_red; empty for every other verdict refusal.
	Code string
}

func (e *VerdictError) Error() string {
	return fmt.Sprintf("verdict.rendered on %s refused: %s (spec/verdicts.md)", e.Subject, e.Reason)
}

// OfferError is the offer.published shape refusal: a malformed
// payload, a malformed expiry, or a born-dead expiry at or before the
// event's own ts. It rides the established shape-refusal exit
// mapping, like the verdict rule's.
type OfferError struct {
	Subject string
	Reason  string
}

func (e *OfferError) Error() string {
	return fmt.Sprintf("offer.published on %s refused: %s (spec/offers.md)", e.Subject, e.Reason)
}

// BudgetError is the budget rule's refusal: a malformed payload, a
// reserve outside the holder/operator boundary or beyond remaining,
// a close of a missing, invalid, or already-closed reservation, or a
// spending verb with no open reservation. It rides the established
// shape-refusal exit mapping.
type BudgetError struct {
	Subject string
	Reason  string
	// Exhausted marks the ONE refusal a caller can act on: capacity
	// spent. It is a field rather than a distinct type so that
	// errors.As(err, &BudgetError{}) still catches every budget
	// refusal - anything treating them uniformly keeps working - and
	// field inspection has precedent in the same mapper, where
	// failureEnvelope maps *ledger.Failure by looking inside it.
	//
	// What makes the narrowness safe is not this comment but the
	// matrix in cmd/seed: the other thirteen refusals are asserted to
	// still come back as chain_invalid, through the CLI, where the
	// conversion actually happens.
	Exhausted bool
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("budget on %s refused: %s (spec/budgets.md)", e.Subject, e.Reason)
}

// RunError is the run rule's refusal: a malformed payload, a start
// outside its claim window or citation boundary, or a settle on a
// start-less or already-settled fence. It rides the established
// shape-refusal exit mapping.
type RunError struct {
	Subject string
	Reason  string
}

func (e *RunError) Error() string {
	return fmt.Sprintf("run on %s refused: %s (spec/executors.md)", e.Subject, e.Reason)
}

// VerbInactiveError refuses a verb whose semantics are not active under
// the chain's protocol version: an actor.* draft on a seed/0 tip is a
// verb illegal in this state (exit 3) until the deployment upgrades.
type VerbInactiveError struct {
	Verb   string
	Active string
	Needs  string
}

func (e *VerbInactiveError) Error() string {
	return fmt.Sprintf("verb %s is not active at protocol %s: it activates at %s (append system.protocol.upgraded first)", e.Verb, e.Active, e.Needs)
}

// Rule is one named admission rule.
// EscalationError is a refusal from the escalation channel: a
// malformed question, an answer that cites nothing standing, or an act
// the standing question forbids. Exit 3 shape, the packet precedent —
// no new exit code, because an escalation adds no new authority.
type EscalationError struct {
	Subject string
	Reason  string
}

func (e *EscalationError) Error() string {
	return fmt.Sprintf("escalation on %s: %s", e.Subject, e.Reason)
}

type Rule struct {
	Name  string
	Check func(*Context, *event.Record) error
}

// Default returns the ordered Phase 2 rule set. The halted refusal
// dominates (a malformed forbidden draft under halt refuses as halted,
// the reviewed halt.Check ordering); later phases append rules to this
// set rather than editing it.
func Default() []Rule {
	return append(coreRules(), policyRules()...)
}

// CeilingError is a claim by an agent-kind key above its squad's
// declared ceiling (plans/os-0d4f2af3.md D3): an agent-only guardrail
// reading the roster's kind, refused at admission as policy.
type CeilingError struct {
	Actor, Kind, Squad, Tier, Ceiling string
	// Unknown marks a ceiling outside the tier vocabulary: the claim
	// fails closed until the declaration is fixed.
	Unknown bool
}

func (e *CeilingError) Error() string {
	if e.Unknown {
		return fmt.Sprintf("claim on a %s contract refused: the %s squad's agent ceiling %q is not a tier in the vocabulary (%s), so agent claims fail closed until seed.json is fixed (seed preseed check)", e.Tier, e.Squad, e.Ceiling, strings.Join(transition.TierOrder(), ", "))
	}
	return fmt.Sprintf("claim on a %s contract refused: %s is enrolled as %s and the %s squad's agent ceiling is %s (a human key is not ceilinged; seed.json guardrails)", e.Tier, e.Actor, e.Kind, e.Squad, e.Ceiling)
}

// RoutingError is an intent routed to a squad the declaration does not
// name (plans/os-0d4f2af3.md D4): the residual tiers.md named, closed.
type RoutingError struct {
	Routing string
	Squads  []string
}

func (e *RoutingError) Error() string {
	return fmt.Sprintf("routing %q names no declared squad (seed.json teams: %s)", e.Routing, strings.Join(e.Squads, ", "))
}

// ApprovalRequiredError is a governed act with no open grant
// (plans/os-5781a026.md D4): the declaration names the verb for this
// actor's roster kind at or above the floor, and no approval.granted
// naming this verb and actor stands unspent on the subject. Refused at
// admission as policy, the ceiling's family; the message names the
// exact request to file and the grant that would answer it.
type ApprovalRequiredError struct {
	Subject, Verb, Actor, Kind, Tier, MinTier string
	// Pending is the position of this actor's unanswered request for
	// the same act, when one stands: the operator's turn, not the
	// actor's.
	Pending *int
}

func (e *ApprovalRequiredError) Error() string {
	scope := "every tier"
	if e.MinTier != "" {
		scope = "the " + e.MinTier + " tier and above"
	}
	on := "on a contract"
	if e.Tier != "" {
		on = "on a " + e.Tier + " contract"
	}
	if e.Pending != nil {
		return fmt.Sprintf("%s %s refused: the deployment's guardrails govern %s for %s-kind keys at %s (seed.json guardrails.approvals), and %s's request at position %d awaits the operator's `seed approval grant --subject %s --request %d`",
			e.Verb, on, e.Verb, e.Kind, scope, e.Actor, *e.Pending, e.Subject, *e.Pending)
	}
	return fmt.Sprintf("%s %s refused: the deployment's guardrails govern %s for %s-kind keys at %s (seed.json guardrails.approvals), and no open approval names %s for it — file `seed approval request --subject %s --verb %s --reason <why>` and wait for the operator's `seed approval grant --subject %s`; one grant admits one act",
		e.Verb, on, e.Verb, e.Kind, scope, e.Actor, e.Subject, e.Verb, e.Subject)
}

// ApprovalValid re-judges a granted approval position-accurately
// (plans/os-5781a026.md D4; memory/LEARNINGS.md, the laundering
// countermeasure): the request record at its position re-parses and
// names a catalog verb; the answer record at its position re-parses
// and cites the request; and at the answer's position the keyring
// held the answerer at active operator standing (the grant row's
// capabilities) and the named actor enrolled. A grant that fails any
// of these folds as a fact and authorizes nothing, so a raw-pushed
// grant signed by a key without the operator's standing admits no act.
func ApprovalValid(records []*event.Record, g transition.ApprovalFact) bool {
	if g.Answered == nil || !g.Granted || g.Pos < 0 || g.Pos >= len(records) || *g.Answered <= g.Pos || *g.Answered >= len(records) {
		return false
	}
	req := &records[g.Pos].Event
	if req.Verb != approval.RequestedVerb || req.Subject != g.Subject || !version.Activated(req.V) {
		return false
	}
	p, err := approval.ParseRequested(req.Subject, req.Payload)
	if err != nil || p.Verb != g.Verb || p.Actor != g.Actor || !catalogHas(p.Verb) {
		return false
	}
	ans := &records[*g.Answered].Event
	if ans.Verb != approval.GrantedVerb || ans.Subject != g.Subject || ans.Actor != g.Answerer || !version.Activated(ans.V) {
		return false
	}
	if _, cited, err := approval.ParseAnswer(ans.Verb, ans.Subject, ans.Payload); err != nil || cited != g.Pos {
		return false
	}
	ring, _, err := keyring.StateAt(records[:*g.Answered])
	if err != nil || ring == nil {
		return false
	}
	if _, enrolled := ring.Get(g.Actor); !enrolled {
		return false
	}
	return ring.HasAnyCapability(g.Answerer, keyring.AcceptedCapabilities(approval.GrantedVerb))
}

// catalogHas reports whether the affordance catalog drafts the verb:
// what "a catalog verb" means for the approval request's citation.
func catalogHas(verb string) bool {
	for _, p := range affordanceCatalog {
		if p.verb == verb {
			return true
		}
	}
	return false
}

// policyRules are the declaration-driven rules: no-ops without a
// declaration, and admission policy rather than chain validity with
// one — the tiers precedent (spec/tiers.md): a raw-pushed record
// that would have refused folds as filed, every chain verifies byte
// for byte, and no protocol version bumps.
func policyRules() []Rule {
	return []Rule{
		{Name: "require-approval", Check: func(c *Context, rec *event.Record) error {
			// The require-approval mode (plans/os-5781a026.md D1, D4;
			// charter §II.14): a record whose verb the declaration
			// governs, by a key whose roster kind the rule names, on a
			// subject at or above the floor, refuses unless an open
			// grant names the verb and the actor. The three approval
			// verbs are never governed; a floor outside the tier
			// vocabulary fails closed, the ceiling's posture.
			if c.Declaration == nil || c.Keyring == nil || c.Lifecycle == nil || approval.IsApprovalVerb(rec.Event.Verb) {
				return nil
			}
			verb, subject, actor := rec.Event.Verb, rec.Event.Subject, rec.Event.Actor
			rule, governed := c.Declaration.ApprovalRule(verb)
			if !governed {
				return nil
			}
			entry, ok := c.Keyring.Get(actor)
			if !ok || !rule.Governs(actor, entry.Kind) {
				return nil
			}
			tier := ""
			if s, ok := c.Lifecycle.State(subject); ok {
				tier = s.Tier
			} else if verb == "intent.filed" {
				// A birth's tier is in its payload: the floor reads
				// the contract the act creates.
				var p struct {
					Tier string `json:"tier"`
				}
				if json.Unmarshal(rec.Event.Payload, &p) == nil {
					tier = p.Tier
				}
			}
			if tier != "" && rule.MinTier != "" {
				if _, known := transition.Tier(rule.MinTier); known && transition.TierAbove(rule.MinTier, tier) {
					return nil
				}
			}
			// The laundering countermeasure (memory/LEARNINGS.md; the
			// RunStartValid posture): the tolerant fold records any
			// well-shaped raw push, so an open grant is trusted only
			// after its signer and its citation are re-judged at their
			// own positions.
			for _, g := range c.Lifecycle.OpenApprovals(subject, verb, actor) {
				if ApprovalValid(c.Records, g) {
					return nil
				}
			}
			err := &ApprovalRequiredError{Subject: subject, Verb: verb, Actor: actor, Kind: entry.Kind, Tier: tier, MinTier: rule.MinTier}
			for _, a := range c.Lifecycle.PendingApprovals(subject) {
				if a.Verb == verb && a.Actor == actor {
					pos := a.Pos
					err.Pending = &pos
					break
				}
			}
			return err
		}},
		{Name: "ceiling", Check: func(c *Context, rec *event.Record) error {
			if c.Declaration == nil || rec.Event.Verb != "claim.taken" || c.Keyring == nil {
				return nil
			}
			entry, ok := c.Keyring.Get(rec.Event.Actor)
			if !ok || entry.Kind == "human" {
				return nil
			}
			s, ok := c.Lifecycle.State(rec.Event.Subject)
			if !ok {
				return nil
			}
			ceiling, declared := c.Declaration.AgentCeiling(s.Routing)
			if !declared {
				return nil
			}
			if _, ok := transition.Tier(ceiling); !ok {
				// A ceiling outside the vocabulary ranks above every
				// tier, which would let every agent claim pass on a
				// typo; the rule fails closed instead.
				return &CeilingError{Actor: rec.Event.Actor, Kind: entry.Kind, Squad: s.Routing, Tier: s.Tier, Ceiling: ceiling, Unknown: true}
			}
			if transition.TierAbove(s.Tier, ceiling) {
				return &CeilingError{Actor: rec.Event.Actor, Kind: entry.Kind, Squad: s.Routing, Tier: s.Tier, Ceiling: ceiling}
			}
			return nil
		}},
		{Name: "routing", Check: func(c *Context, rec *event.Record) error {
			if c.Declaration == nil || rec.Event.Verb != "intent.filed" {
				return nil
			}
			squads := c.Declaration.SquadNames()
			if squads == nil {
				return nil
			}
			var p struct {
				Routing string `json:"routing"`
			}
			if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
				return nil // the shape rule owns malformed payloads
			}
			for _, sq := range squads {
				if sq == p.Routing {
					return nil
				}
			}
			return &RoutingError{Routing: p.Routing, Squads: squads}
		}},
	}
}

// coreRules is the ordered rule set every phase since Phase 2 has
// appended to: what admission enforces with or without a declaration.
func coreRules() []Rule {
	return []Rule{
		{Name: "halted", Check: func(c *Context, rec *event.Record) error {
			if c.Halt.Halted && rec.Event.Verb != halt.LiftVerb {
				return &halt.HaltedError{By: c.Halt.By, Reason: c.Halt.Reason}
			}
			return nil
		}},
		{Name: "shape", Check: func(c *Context, rec *event.Record) error {
			if _, err := rec.Event.Hash(); err != nil {
				return err
			}
			if err := halt.ValidateShape(&rec.Event); err != nil {
				return err
			}
			// Admission applies the upgrade schema unconditionally: a
			// signed but schema-broken upgrade admitted to the chain
			// would wedge every later verification at bad_payload.
			return ledger.ValidateUpgradeShape(&rec.Event)
		}},
		{Name: "standing", Check: func(c *Context, rec *event.Record) error {
			// The activation check precedes signer resolution: an actor
			// verb before the seed/1 boundary is illegal for every
			// signer, so the cooperative client must refuse it exactly
			// as the hook does (exit 3; review finding on #100), never
			// as an unresolvable-signer chain complaint.
			if keyring.IsActorVerb(rec.Event.Verb) && !keyring.Applies(c.Active) {
				return &VerbInactiveError{Verb: rec.Event.Verb, Active: c.Active, Needs: version.Seed1}
			}
			return nil
		}},
		{Name: "actor", Check: func(c *Context, rec *event.Record) error {
			pub, ok := c.Resolve(rec.Event.Actor)
			if !ok {
				return fmt.Errorf("%w: %s", ledger.ErrUnknownActor, rec.Event.Actor)
			}
			return rec.Verify(pub)
		}},
		{Name: "version", Check: func(c *Context, rec *event.Record) error {
			if !c.Supported[c.Active] {
				return &ledger.Failure{Position: c.Count, Reason: ledger.ReasonVersionUnsupported,
					Detail: fmt.Sprintf("active version %q is not in this implementation's supported set", c.Active)}
			}
			if rec.Event.V != c.Active {
				return &ledger.Failure{Position: c.Count, Reason: ledger.ReasonVersionMismatch,
					Detail: fmt.Sprintf("event carries %q, the version active at the tip is %q", rec.Event.V, c.Active)}
			}
			return nil
		}},
		{Name: "classification", Check: func(c *Context, rec *event.Record) error {
			if vs := classify.Lint(rec.Event.Payload); len(vs) > 0 {
				return &ClassificationError{Violations: vs}
			}
			return nil
		}},
		{Name: "grant", Check: func(c *Context, rec *event.Record) error {
			if !keyring.Applies(c.Active) || c.Keyring == nil {
				return nil
			}
			if accepted := keyring.AcceptedCapabilities(rec.Event.Verb); len(accepted) > 0 &&
				!c.Keyring.HasAnyCapability(rec.Event.Actor, accepted) {
				return &OutOfGrantError{Actor: rec.Event.Actor, Verb: rec.Event.Verb, Accepted: accepted}
			}
			if keyring.IsActorVerb(rec.Event.Verb) {
				// The shared transition function is the shape and
				// legality authority; admission previews it so a draft
				// that history would refuse never leaves the client.
				return c.Keyring.Preview(rec)
			}
			return nil
		}},
		{Name: "fence", Check: func(c *Context, rec *event.Record) error {
			// The fence rule (plans/os-5dc16a7c.md), between grant and
			// lifecycle per the charter's check order. The fence is the
			// admitted claim.taken position, derived never asserted; on
			// a held subject the four deliberate exits must cite it, so
			// must free events from the holder or any prior claimant of
			// the subject (a reaped worker cannot demote itself to
			// observer), and any citation present must match the active
			// fence whoever signs. Outside a claim window no fence
			// exists: none is required, and citing one refuses.
			if !keyring.Applies(c.Active) || c.Table == nil || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			if verb == transition.RunStartedVerb || verb == transition.RunSettledVerb ||
				verb == transition.RunInterruptedVerb {
				// The run facts' "fence" field is the run's window
				// reference, not a fence-rule citation: a settle
				// legitimately cites a PRIOR fence after its window
				// closed (plans/os-1dad487d.md), which the
				// active-fence citation semantics here would refuse.
				// The run rule validates the reference against the
				// applied claim positions instead (the interrupt's
				// per plans/os-0f718b4e.md).
				return nil
			}
			if transition.IsMergeChain(verb) {
				// The merge chain is subject-scoped, never claim-scoped:
				// both payloads are strict objects the chain rule pins
				// ({verdict} or {override}, and {merged, pr}), so neither
				// carries a citation slot to satisfy this rule with, and
				// what each rests on is the subject's verdict rather than
				// anyone's window. On an exclusive subject the window is
				// already closed by the submission that reached review,
				// so the rule never met them; on a racing one a rival
				// racer is still holding, and demanding its fence would
				// refuse every settlement, taking the settled-out facts
				// §II.6 rests on with it (plans/os-873b5153.md D8).
				return nil
			}
			if c.Table.Exclusive(verb) {
				if s, ok := c.Lifecycle.State(rec.Event.Subject); ok && s.Claim != nil {
					// A rival claim is contention, the lifecycle
					// rule's structured refusal, never a fence
					// complaint.
					return nil
				}
				// With no active claim no fence exists, and the
				// claiming verb asserts none: fences are derived from
				// the admitted position, so a citation here refuses
				// like any other claimless citation.
				if cited, hasCited := fenceCitation(rec.Event.Payload); hasCited {
					return &FenceError{Subject: rec.Event.Subject, Cited: cited, Active: -1}
				}
				return nil
			}
			cited, hasCited := fenceCitation(rec.Event.Payload)
			s, ok := c.Lifecycle.State(rec.Event.Subject)
			if !ok || s.Claim == nil {
				if hasCited {
					return &FenceError{Subject: rec.Event.Subject, Cited: cited, Active: -1}
				}
				return nil
			}
			// The actor's own fence when it holds one of the active
			// claims (the one on an exclusive subject, its own on a
			// racing one); any active fence for a prior claimant
			// (plans/os-56bee171.md D2).
			own, holds := s.HolderFence(rec.Event.Actor)
			required := false
			if c.Table.IsLifecycleVerb(verb) {
				// The deliberate exits are exactly the lifecycle verbs
				// the table allows out of the held state — and, on a
				// racing subject, every racer's exit wherever the
				// state stands.
				required = c.Table.Allows(s.State, verb) || (s.Racing && transition.IsExit(verb))
			} else if holds || s.PriorClaimants[rec.Event.Actor] {
				required = true
			}
			if !hasCited {
				if required {
					active, holder := s.Claim.Fence, s.Claim.Holder
					if holds {
						active, holder = own, rec.Event.Actor
					}
					return &FenceError{Subject: rec.Event.Subject, Active: active, Holder: holder}
				}
				return nil
			}
			if holds {
				if cited != fmt.Sprintf("%d", own) {
					return &FenceError{Subject: rec.Event.Subject, Cited: cited, Active: own, Holder: rec.Event.Actor}
				}
				return nil
			}
			for _, f := range s.ActiveFences() {
				if cited == fmt.Sprintf("%d", f) {
					return nil
				}
			}
			return &FenceError{Subject: rec.Event.Subject, Cited: cited, Active: s.Claim.Fence, Holder: s.Claim.Holder}
		}},
		{Name: "packet", Check: func(c *Context, rec *event.Record) error {
			// Every deliberate exit carries a four-part handoff packet
			// (plans/os-b07b0f59.md): the shape refusal lands before the
			// transition applies, after the fence. seed/0 stays inert.
			if !keyring.Applies(c.Active) {
				return nil
			}
			if !packet.Required(rec.Event.Verb) {
				return nil
			}
			_, err := packet.FromPayload(rec.Event.Subject, rec.Event.Payload)
			return err
		}},
		{Name: "race_settled", Check: func(c *Context, rec *event.Record) error {
			// A settled-out racer (plans/os-56bee171.md D3): the race
			// closed at RaceSettled while its claim was active. Its
			// own deliberate exit still admits (a packet is never
			// refused after the fact); every other act on the subject
			// refuses, naming the settlement.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			s, ok := c.Lifecycle.State(rec.Event.Subject)
			if !ok || s.RaceSettled == nil {
				return nil
			}
			fence, holds := s.HolderFence(rec.Event.Actor)
			if !holds || transition.IsExit(rec.Event.Verb) {
				return nil
			}
			return &RaceSettledError{Subject: rec.Event.Subject, Actor: rec.Event.Actor, Fence: fence, SettledAt: *s.RaceSettled}
		}},
		{Name: "escalation", Check: func(c *Context, rec *event.Record) error {
			// The escalation channel (plans/os-f781f0da.md): a question
			// is shape-checked wherever it may ride, an answer must cite
			// the standing question and choose from its own option set,
			// and while a question stands nothing else about the
			// contract moves — which is the charter's §II.11 made
			// structural rather than hoped for.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			return checkEscalation(c, rec)
		}},
		{Name: "checkpoint", Check: func(c *Context, rec *event.Record) error {
			// The checkpoint's snapshot citation
			// (plans/os-8a5f14bb.md D4.5; SEED-NEXT.md §II
			// checkpoints): the payload names a retrievable
			// materialization under a versioned format, so a fresh
			// reader can fetch it, verify it against this signature,
			// and start without rebuilding the very state the
			// checkpoint was meant to spare it. Before this rule the
			// boundary took any payload at all, which let a
			// checkpoint be signed, admitted, counted in the report,
			// and useless.
			//
			// Shape is all admission can judge: this context carries
			// no artifact store, so whether the snapshot is really
			// there is the reader's check, not the door's. Saying
			// which check lives where is the honest version of
			// "validated at admission".
			if !keyring.Applies(c.Active) {
				return nil
			}
			if rec.Event.Verb != checkpoint.Verb {
				return nil
			}
			_, err := checkpoint.Parse(rec.Event.Subject, rec.Event.Payload)
			return err
		}},
		{Name: "imported", Check: func(c *Context, rec *event.Record) error {
			// The import's provenance record (plans/os-cf13fb51.md D2;
			// spec/import.md): defined from seed/5, the strict
			// payload naming the predecessor, the export's head, the
			// anchor and the manifest, and admitted once per ledger —
			// a second import is a second history, which is not a
			// thing a genesis ledger can hold.
			if rec.Event.Verb != imported.Verb {
				return nil
			}
			if !version.ImportApplies(c.Active) {
				return &Refusal{Rule: "imported", Err: fmt.Errorf("%s is not defined at %s: the import needs a chain at %s", imported.Verb, c.Active, version.Seed5)}
			}
			if _, err := imported.Parse(rec.Event.Subject, rec.Event.Payload); err != nil {
				return err
			}
			for i, prior := range c.Records {
				if prior.Event.Verb == imported.Verb {
					return &Refusal{Rule: "imported", Err: fmt.Errorf("the ledger already carries an import at position %d: a genesis import refuses a ledger that is not empty", i)}
				}
			}
			return nil
		}},
		{Name: "approval", Check: func(c *Context, rec *event.Record) error {
			// The per-verb approval's shapes and citations
			// (plans/os-5781a026.md D2, D4; spec/protocol.md
			// "Per-verb approval"), held regardless of declaration so
			// a raw-pushed grant citing nothing folds as an anomaly
			// and admits nothing: approval.requested names a catalog
			// verb that is not an approval verb, an enrolled actor and
			// one bounded reason, on a contract the chain knows or on
			// system, except that a request for intent.filed is on the
			// contract the filing would create, which the chain cannot
			// know until the governed filing succeeds (review finding
			// on #331); approval.granted and approval.denied cite an
			// approval.requested on the same subject not yet
			// answered. Standing for the request is the keyring
			// rule's; the operator grant for the answers is the grant
			// rule's; whether an act NEEDS one is the policy rule's.
			verb := rec.Event.Verb
			if !approval.IsApprovalVerb(verb) || !keyring.Applies(c.Active) {
				return nil
			}
			subject := rec.Event.Subject
			if c.Lifecycle == nil {
				return &approval.Error{Verb: verb, Subject: subject, Reason: "no lifecycle is known to resolve the subject"}
			}
			if verb == approval.RequestedVerb {
				p, err := approval.ParseRequested(subject, rec.Event.Payload)
				if err != nil {
					return err
				}
				if !catalogHas(p.Verb) {
					return &approval.Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("verb %q is no catalog verb: an approval is asked for an act the boundary drafts", p.Verb)}
				}
				// The policy rule finds a grant on the governed act's
				// own subject, and a birth's subject is the contract it
				// creates: a request for intent.filed names that
				// contract before the chain knows it, or no grant could
				// ever admit a governed birth. Every other verb acts on
				// a contract the chain knows, or on system.
				if subject != approval.SystemSubject {
					if _, ok := c.Lifecycle.State(subject); !ok && p.Verb != "intent.filed" {
						return &approval.Error{Verb: verb, Subject: subject, Reason: "no contract by that id is on this chain: a request is on the contract the act concerns, or on system; a birth's request is on the contract it creates"}
					}
				}
				if c.Keyring != nil {
					if _, ok := c.Keyring.Get(p.Actor); !ok {
						return &approval.Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("actor %q is no enrolled key: the request names the key that will act", p.Actor)}
					}
				}
				return nil
			}
			_, pos, err := approval.ParseAnswer(verb, subject, rec.Event.Payload)
			if err != nil {
				return err
			}
			fact, ok := c.Lifecycle.ApprovalAt(pos)
			if !ok {
				return &approval.Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("request %d is no %s on this chain", pos, approval.RequestedVerb)}
			}
			if fact.Subject != subject {
				return &approval.Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("request %d is on %s: the answer's subject is the request's", pos, fact.Subject)}
			}
			if fact.Answered != nil {
				outcome := "denied"
				if fact.Granted {
					outcome = "granted"
				}
				return &approval.Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("request %d was %s at position %d by %s: a request is answered once", pos, outcome, *fact.Answered, fact.Answerer)}
			}
			return nil
		}},
		{Name: "erasure", Check: func(c *Context, rec *event.Record) error {
			// The erasure fact (plans/os-db5cd353.md D1, D2;
			// spec/protocol.md "Erasure"): artifact.erased is a
			// fact that changes no state, additive catalog growth
			// active from seed/1, admitted with its strict shape on
			// the contract whose fold references the digest (its
			// sealed commitment or a verdict's receipt) or on system,
			// where the operator's attestation is the reference; an
			// artifact erased once on a subject is not erased there
			// again. Standing and the operator grant are the keyring
			// and grant rules'; this rule holds the shape, the
			// reference and the once.
			if rec.Event.Verb != erasure.Verb || !keyring.Applies(c.Active) {
				return nil
			}
			subject := rec.Event.Subject
			p, err := erasure.Parse(subject, rec.Event.Payload)
			if err != nil {
				return err
			}
			if c.Lifecycle == nil {
				return &erasure.Error{Subject: subject, Reason: "no lifecycle is known to resolve the reference"}
			}
			if subject != erasure.SystemSubject {
				s, ok := c.Lifecycle.State(subject)
				if !ok {
					return &erasure.Error{Subject: subject, Reason: "no contract by that id is on this chain: an erasure is on the contract that references the artifact, or on system"}
				}
				referenced := s.Sealed != nil && s.Sealed.Commitment == p.Artifact
				var known []string
				if s.Sealed != nil {
					known = append(known, "sealed commitment "+s.Sealed.Commitment)
				}
				for _, v := range s.Verdicts {
					if v.Receipt == p.Artifact {
						referenced = true
					}
					if v.Receipt != "" {
						known = append(known, fmt.Sprintf("receipt %s (verdict at position %d)", v.Receipt, v.Pos))
					}
				}
				if !referenced {
					what := "nothing by digest"
					if len(known) > 0 {
						what = strings.Join(known, ", ")
					}
					return &erasure.Error{Subject: subject, Reason: fmt.Sprintf("artifact %s is not one this contract references: its fold holds %s — erase a referenced artifact on its contract, or an artifact referenced elsewhere on system", p.Artifact, what)}
				}
			}
			// Only a tombstone that passed the boundary stands
			// (ErasureValid): a raw one under a key without the grant
			// attributes nothing and must not block the operator's
			// own record.
			if prior, done := Erasure(c.Records, c.Lifecycle, p.Artifact); done {
				return &erasure.Error{Subject: subject, Reason: fmt.Sprintf("artifact %s was erased at position %d by %s on %s: an artifact is erased once, wherever it was recorded, and a second record would attribute an act that did nothing", p.Artifact, prior.Pos, prior.Signer, prior.Subject)}
			}
			return nil
		}},
		{Name: "topology", Check: func(c *Context, rec *event.Record) error {
			// The relation facts (plans/os-f0ae2cdf.md D2, D3;
			// spec/topology.md): dependency.linked and unlinked,
			// hierarchy.parented and goal.aligned are facts beside the
			// lifecycle, additive catalog growth active from seed/1,
			// admitted with their strict shape on a known nonterminal
			// contract, naming a known target, never a self edge or a
			// cycle, links a set. Standing and the dispatch grant are
			// the keyring and grant rules'; this rule holds the shape
			// and the graph rule, judged against the graph that passed
			// the boundary (a raw-pushed edge shapes no cycle check).
			if !topology.IsRelationVerb(rec.Event.Verb) || !keyring.Applies(c.Active) {
				return nil
			}
			g := topology.Fold(c.Records, c.Table)
			return g.Apply(c.Lifecycle, c.Table, c.Count, &rec.Event)
		}},
		{Name: "request", Check: func(c *Context, rec *event.Record) error {
			// The request ingress (plans/os-48df10a2.md D1, D2;
			// spec/requests.md): request.filed is a fact that
			// changes no state and grants nothing, admitted from
			// seed/7 with its strict shape, on the contract `about`
			// names (which must be on the chain) or on system;
			// request.answered is the dispatcher's close of one
			// request not yet answered, citing the intent it filed
			// (an intent.filed admitted after the request) or the
			// reason it declined. Standing for the filing is the
			// keyring rule's; dispatch for the answer is the grant
			// rule's; this rule holds the shapes and the citations.
			verb := rec.Event.Verb
			if verb != request.FiledVerb && verb != request.AnsweredVerb {
				return nil
			}
			if !version.RequestsApply(c.Active) {
				return &Refusal{Rule: "request", Err: fmt.Errorf("%s is not defined at %s: the request ingress needs a chain at %s", verb, c.Active, version.Seed7)}
			}
			if verb == request.FiledVerb {
				p, err := request.ParseFiled(rec.Event.Subject, rec.Event.Payload)
				if err != nil {
					return err
				}
				if p.About != "" {
					if c.Lifecycle == nil {
						return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("about names %q, and no lifecycle is known to resolve it", p.About)}
					}
					if _, ok := c.Lifecycle.State(p.About); !ok {
						return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("about names %q, which is no contract on this chain: a request is about a contract the chain knows or about nothing", p.About)}
					}
				}
				return nil
			}
			p, pos, err := request.ParseAnswered(rec.Event.Subject, rec.Event.Payload)
			if err != nil {
				return err
			}
			if c.Lifecycle == nil {
				return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: "no lifecycle is known to resolve the request"}
			}
			fact, ok := c.Lifecycle.RequestAt(pos)
			if !ok {
				return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("request %d is no %s on this chain", pos, request.FiledVerb)}
			}
			if fact.Subject != rec.Event.Subject {
				return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("request %d is on %s: the answer's subject is the request's", pos, fact.Subject)}
			}
			if fact.Answered != nil {
				return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("request %d was answered at position %d (%s): a request is answered once", pos, *fact.Answered, fact.Outcome)}
			}
			if p.Outcome == "filed" {
				n, _ := strconv.Atoi(strings.TrimSpace(p.Intent))
				if n <= pos || n >= len(c.Records) || c.Records[n].Event.Verb != "intent.filed" {
					return &request.Error{Verb: verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("intent %d is not an intent.filed admitted after request %d: filed cites the intent the dispatcher appended for it", n, pos)}
				}
			}
			return nil
		}},
		{Name: "boundary", Check: func(c *Context, rec *event.Record) error {
			// The card's word (plans/os-40ed0ca0.md D1, D5): a deployment
			// that declares a boundary block accepts the request kinds
			// it names and no other, so a request the card refuses the
			// ingress refuses too — policy from the declaration, never
			// chain validity, like the ceiling.
			if rec.Event.Verb != request.FiledVerb || c.Declaration == nil || c.Declaration.Boundary == nil {
				return nil
			}
			p, err := request.ParseFiled(rec.Event.Subject, rec.Event.Payload)
			if err != nil {
				return nil // the request rule's refusal
			}
			for _, k := range c.Declaration.Boundary.Accepts {
				if k == p.Kind {
					return nil
				}
			}
			return &request.Error{Verb: rec.Event.Verb, Subject: rec.Event.Subject, Reason: fmt.Sprintf("kind %q is not one this deployment's card accepts (%s)", p.Kind, strings.Join(c.Declaration.Boundary.Accepts, ", "))}
		}},
		{Name: "plan", Check: func(c *Context, rec *event.Record) error {
			// The plan gate (plans/os-16c1d142.md): plan.* payloads
			// carry their anchors, and a submission above the trivial
			// tier requires an admitted plan.approved plus the cited
			// plan anchor (exit 16 plan_required). The ancestry
			// binding is Phase 6's receipt computation.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			if err := transition.CheckPlanEventShape(c.Active, verb, rec.Event.Subject, rec.Event.Payload); err != nil {
				return err
			}
			if verb != "submission.made" {
				return nil
			}
			tier := ""
			if s, ok := c.Lifecycle.State(rec.Event.Subject); ok {
				tier = s.Tier
			}
			return c.Lifecycle.CheckPlanGate(rec.Event.Subject, tier, rec.Event.Payload)
		}},
		{Name: "observation", Check: func(c *Context, rec *event.Record) error {
			// The summarization boundary (plans/os-2ff8dbf1.md):
			// milestones are coarse, monotonic, position-throttled
			// facts, and a declared wedge carries its evidence. Both
			// are free verbs; capability rows and the fence matrix
			// apply through their own rules.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			switch rec.Event.Verb {
			case transition.MilestoneVerb:
				return c.Lifecycle.CheckMilestone(rec.Event.Subject, c.Count, rec.Event.Payload)
			case transition.WedgeDeclaredVerb:
				return transition.CheckWedgeShape(rec.Event.Subject, rec.Event.Payload)
			}
			return nil
		}},
		{Name: "forge", Check: func(c *Context, rec *event.Record) error {
			// The forge observation (plans/os-0cd18799.md D1;
			// spec/observations-forge.md): check.observed is a fact
			// admitted only on review subjects from seed/8, bound to
			// the head the submission under review names, and
			// admitted only when it says something the standing
			// observation does not. Capability rides the grant rule
			// (the observer's row); this rule holds the shape, the
			// head binding and the change requirement.
			if rec.Event.Verb != transition.CheckObservedVerb {
				return nil
			}
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			if !version.ForgeChecksApply(c.Active) {
				return &Refusal{Rule: "forge", Err: fmt.Errorf("%s is not defined at %s: the forge observation needs a chain at %s", rec.Event.Verb, c.Active, version.Seed8)}
			}
			subject := rec.Event.Subject
			s, ok := c.Lifecycle.State(subject)
			if !ok {
				return &transition.InvalidTransitionError{Subject: subject, Verb: rec.Event.Verb}
			}
			if s.State != "review" {
				return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: rec.Event.Verb}
			}
			fact, err := transition.ParseCheckObserved(c.Count, &rec.Event)
			if err != nil {
				return err
			}
			head, ok := submissionHead(c, subject, s)
			if !ok {
				return &transition.ChainError{Subject: subject, Verb: rec.Event.Verb, Reason: "the submission under review names no head in its packet's base range — an observation binds to a head, and there is none to bind to"}
			}
			if fact.Head != head {
				return &transition.ChainError{Subject: subject, Verb: rec.Event.Verb, Reason: fmt.Sprintf("head %s is not the submission under review, whose head is %s — a stale poll never stands for the head being judged", fact.Head, head)}
			}
			if s.Submission != nil && s.Submission.PR != "" && fact.PR != s.Submission.PR {
				return &transition.ChainError{Subject: subject, Verb: rec.Event.Verb, Reason: fmt.Sprintf("pr %q is not the submission's, which named %q", fact.PR, s.Submission.PR)}
			}
			if s.Observation != nil && s.Observation.Same(fact) {
				return &transition.ChainError{Subject: subject, Verb: rec.Event.Verb, Reason: fmt.Sprintf("the observation at position %d already says exactly this — an unchanged poll appends nothing, and the subject's share of the ledger is bounded by change", s.Observation.Pos)}
			}
			return nil
		}},
		{Name: "proposal", Check: func(c *Context, rec *event.Record) error {
			// Outside text can propose, never arm (III.F row 2,
			// plans/os-73c00a50.md): request.* payloads structurally
			// cannot carry executable or gate keys at any depth.
			if !keyring.Applies(c.Active) {
				return nil
			}
			if !strings.HasPrefix(rec.Event.Verb, transition.ProposalVerbPrefix) {
				return nil
			}
			return transition.CheckProposalShape(rec.Event.Subject, rec.Event.Payload)
		}},
		{Name: "verdict", Check: func(c *Context, rec *event.Record) error {
			// The verdict pipeline's admission half
			// (plans/os-f6d2c267.md; spec/verdicts.md): a fact
			// admitted only on review subjects, bound to the
			// submission it judges, signed by a key disjoint from
			// every implementing key (L1). Capability rides the grant
			// rule; the fence rule already refuses citations outside
			// a claim window.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			if rec.Event.Verb != transition.VerdictRenderedVerb {
				return nil
			}
			subject := rec.Event.Subject
			s, ok := c.Lifecycle.State(subject)
			if !ok {
				return &transition.InvalidTransitionError{Subject: subject, Verb: rec.Event.Verb}
			}
			if s.State != "review" {
				return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: rec.Event.Verb}
			}
			var p struct {
				Verdict      string          `json:"verdict"`
				Receipt      string          `json:"receipt"`
				Submission   string          `json:"submission"`
				Independence string          `json:"independence"`
				Tuple        json.RawMessage `json:"tuple"`
				Scorecard    json.RawMessage `json:"scorecard"`
			}
			dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {verdict, receipt, submission, independence, tuple?, scorecard?}: %v", err)}
			}
			var missing []string
			for _, f := range []struct{ name, v string }{{"verdict", p.Verdict}, {"receipt", p.Receipt}, {"submission", p.Submission}, {"independence", p.Independence}} {
				if strings.TrimSpace(f.v) == "" {
					missing = append(missing, f.name)
				}
			}
			if len(missing) > 0 {
				return &transition.IncompleteError{Verb: rec.Event.Verb, Subject: subject, Missing: missing}
			}
			if p.Verdict != "pass" && p.Verdict != "fail" {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("verdict %q is neither literal pass nor fail", p.Verdict)}
			}
			if !receiptDigestRE.MatchString(p.Receipt) {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("receipt %q is not a lowercase-hex sha256 digest of the receipt's JCS bytes", p.Receipt)}
			}
			// The level vocabulary and the verifier's declaration are
			// seed/4's (plans/os-99829835.md D3, D4): before it the
			// literal L1 alone, and no tuple, so a seed/3 chain keeps
			// seed/3's judgment.
			if version.LevelsApply(c.Active) {
				if _, ok := transition.ParseLevel(p.Independence); !ok {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("independence %q is not a level: the vocabulary is L1, L2, L3 (spec/verdicts.md)", p.Independence)}
				}
			} else {
				if len(p.Tuple) > 0 {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("the verdict declares a tuple and the chain is at %s: the verifier's declaration activates at %s (spec/protocol.md)", c.Active, version.Seed4)}
				}
				if len(p.Scorecard) > 0 {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("the verdict cites a scorecard and the chain is at %s: rubric verdicts activate at %s (spec/verdicts.md)", c.Active, version.Seed4)}
				}
				if p.Independence != "L1" {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("independence %q is not the literal \"L1\": the level vocabulary widens at %s (spec/verdicts.md)", p.Independence, version.Seed4)}
				}
			}
			if s.Submission == nil {
				return &VerdictError{Subject: subject, Reason: "the fold records no submission on this review subject, so there is nothing to bind a verdict to"}
			}
			citedSub, err := strconv.Atoi(strings.TrimSpace(p.Submission))
			bound, isBound := s.SubmissionAt(citedSub)
			if err != nil || !isBound {
				if len(s.Submissions) <= 1 {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("submission %q is not the bound submission at position %d — a verdict judges exactly the submission that produced the review state", p.Submission, s.Submission.Pos)}
				}
				positions := make([]string, 0, len(s.Submissions))
				for _, sub := range s.Submissions {
					positions = append(positions, fmt.Sprintf("%d", sub.Pos))
				}
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("submission %q is not a bound submission of this racing window (%s) — a verdict judges exactly the submission it cites", p.Submission, strings.Join(positions, ", "))}
			}
			// The rest of the rule judges the cited submission: on a
			// racing subject each racer's submission is its own window
			// for the lockout and the independence check.
			s.Submission = &bound
			// The red-verdict lockout (plans/os-d2497eb7.md): pass
			// over a submission an authenticated fail already judged
			// refuses until a NEW submission; fail restatements stay
			// admissible. Only boundary-validated fails lock, and the
			// whole window is scanned, so a raw-pushed later verdict
			// can never bury an authentic fail.
			if p.Verdict == "pass" {
				if locked := authenticFail(c, subject, s); locked != nil {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("a fail verdict at position %d already judged the bound submission — a red verdict locks pass out until a new submission (contract.returned, re-claim, resubmit)", locked.Pos)}
				}
			}
			if s.PriorClaimants[rec.Event.Actor] {
				return &NotIndependentError{Subject: subject, Actor: rec.Event.Actor, Role: "a claimant, past or present"}
			}
			if rec.Event.Actor == s.Submission.Signer {
				return &NotIndependentError{Subject: subject, Actor: rec.Event.Actor, Role: "the bound submission's signer"}
			}
			// The level is exactly what the records support, and it
			// satisfies the tier (plans/os-99829835.md D3): a claim
			// below the computed level would underreport the audit
			// data the level exists to produce, a claim above it
			// would assert what the record does not show.
			if version.LevelsApply(c.Active) {
				var declared *tuple.Tuple
				if len(p.Tuple) > 0 {
					t, err := tuple.Parse(p.Tuple)
					if err != nil {
						return &VerdictError{Subject: subject, Reason: "the verifier's declared tuple: " + err.Error()}
					}
					declared = &t
				}
				achieved := LevelAchieved(c.Records, c.Table, subject, s, declared)
				if claimed, _ := transition.ParseLevel(p.Independence); claimed != achieved {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("independence %s is not the level the record supports: the records support %s — the verdict records exactly the level achieved, never less and never more (spec/verdicts.md)", claimed, achieved)}
				}
				if required := transition.TierGates(s.Tier).Independence; !achieved.Satisfies(required) {
					return &LevelShortError{Subject: subject, Tier: s.Tier, Required: required, Achieved: achieved}
				}
				// The scorecard's record half (plans/os-2e34f66a.md D2,
				// D3): the payload's items are what every boundary
				// derives from, so a verdict whose own items refute it
				// never lands. The evidence stays in the artifact,
				// which admission cannot read by design.
				if len(p.Scorecard) > 0 && string(p.Scorecard) != "null" {
					ref, err := parseScorecardRef(p.Scorecard)
					if err != nil {
						return &VerdictError{Subject: subject, Reason: err.Error()}
					}
					derived, code, item := transition.DeriveScores(ref.Items)
					switch {
					case code == transition.CodeHumanVerdict:
						return &VerdictError{Subject: subject, Code: code, Reason: fmt.Sprintf("item %q is scored at high uncertainty: low confidence routes to a human verdict (verdict.deferred), and neither pass nor fail is renderable over it", item)}
					case p.Verdict == "pass" && derived == "fail":
						return &VerdictError{Subject: subject, Code: transition.CodeRubricRed, Reason: fmt.Sprintf("item %q scores fail: the verdict derives from the scorecard, and a failing item forbids pass (fail stays renderable)", item)}
					case p.Verdict != derived:
						return &VerdictError{Subject: subject, Reason: fmt.Sprintf("verdict %s over a scorecard whose every item passes at low uncertainty: with a rubric the verdict is the derivation's, and the scorecard says %s", p.Verdict, derived)}
					}
				}
				// The human verdict (D4): on a human-review tier, or
				// after a deferral on this window, the render is a
				// human's, and a human is a key with operator standing
				// beside its verdict grant.
				if human, why := HumanVerdictRequired(s, len(c.Records)); human && !OperatorStanding(c.Keyring, rec.Event.Actor) {
					return &VerdictError{Subject: subject, Code: transition.CodeHumanVerdict, Reason: fmt.Sprintf("%s, and %s holds no operator standing — a human is a key with operator standing beside its verdict grant, and a verdict-only key cannot stand in for one", why, rec.Event.Actor)}
				}
				// The set rule at render (D5), exactly as at run.started:
				// the verifier's grants for verdict cite zero or more
				// tuples, and a declared configuration outside the set
				// is out of grant until a calibration eval passes
				// again. An eval subject is where the configuration
				// proves itself, so the rule never gates it there.
				if d := tupleDrift(c.Keyring, rec.Event.Actor, declared, s.Eval != nil, keyring.CapVerdict); d != nil {
					return &OutOfGrantError{Actor: rec.Event.Actor, Verb: rec.Event.Verb,
						Accepted: keyring.AcceptedCapabilities(rec.Event.Verb), Drift: d}
				}
			}
			return nil
		}},
		{Name: "deferral", Check: func(c *Context, rec *event.Record) error {
			// The human-verdict deferral (plans/os-2e34f66a.md D4): a
			// fact admitted in review by a verdict key that is no
			// implementing key, on the bound submission, citing the
			// receipt it computed (the human's render cites it, since
			// a key with operator standing is never a sealed-checks
			// recipient and cannot compute one) and, where the spec
			// carries a rubric, its scorecard and the items left at
			// high uncertainty; on a human-review tier the whole
			// verdict defers and the items may be empty. It changes no
			// state and creates the verdict.human obligation. One per
			// window: a second deferral refuses, and so does one over
			// a submission already judged.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil || rec.Event.Verb != transition.VerdictDeferredVerb {
				return nil
			}
			subject := rec.Event.Subject
			if !version.LevelsApply(c.Active) {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("the chain is at %s: the human-verdict deferral activates at %s (spec/verdicts.md)", c.Active, version.Seed4)}
			}
			s, ok := c.Lifecycle.State(subject)
			if !ok {
				return &transition.InvalidTransitionError{Subject: subject, Verb: rec.Event.Verb}
			}
			if s.State != "review" {
				return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: rec.Event.Verb}
			}
			var p struct {
				Receipt    string   `json:"receipt"`
				Scorecard  string   `json:"scorecard,omitempty"`
				Submission string   `json:"submission"`
				Items      []string `json:"items,omitempty"`
			}
			dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("the deferral is the strict object {receipt, submission, scorecard?, items?}: %v", err)}
			}
			var missing []string
			if strings.TrimSpace(p.Receipt) == "" {
				missing = append(missing, "receipt")
			}
			if strings.TrimSpace(p.Submission) == "" {
				missing = append(missing, "submission")
			}
			if len(missing) > 0 {
				return &transition.IncompleteError{Verb: rec.Event.Verb, Subject: subject, Missing: missing}
			}
			if !receiptDigestRE.MatchString(p.Receipt) {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("receipt %q is not a lowercase-hex sha256 digest of the receipt's JCS bytes", p.Receipt)}
			}
			if p.Scorecard != "" && !receiptDigestRE.MatchString(p.Scorecard) {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("scorecard %q is not a lowercase-hex sha256 digest of the scorecard's JCS bytes", p.Scorecard)}
			}
			if len(p.Items) > 0 && p.Scorecard == "" {
				return &VerdictError{Subject: subject, Reason: "items name what a scorecard left at high uncertainty, and the deferral cites no scorecard"}
			}
			if len(p.Items) == 0 && !transition.TierGates(s.Tier).HumanReview {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("nothing is deferred: name the items the scorecard scored at high uncertainty, since tier %q does not route every verdict to a human", s.Tier)}
			}
			if s.Submission == nil {
				return &VerdictError{Subject: subject, Reason: "the fold records no submission on this review subject, so there is nothing to defer"}
			}
			if p.Submission != fmt.Sprintf("%d", s.Submission.Pos) {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("submission %q is not the bound submission at position %d — a deferral defers exactly the submission under judgment", p.Submission, s.Submission.Pos)}
			}
			seen := map[string]bool{}
			for _, id := range p.Items {
				if strings.TrimSpace(id) == "" || seen[id] {
					return &VerdictError{Subject: subject, Reason: fmt.Sprintf("items name each deferred rubric item once, non-empty: got %q", id)}
				}
				seen[id] = true
			}
			if s.Deferred != nil {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("a deferral already stands at position %d on this submission: one deferral, one human verdict", s.Deferred.Pos)}
			}
			if s.Verdict != nil && s.Verdict.Submission == s.Submission.Pos {
				return &VerdictError{Subject: subject, Reason: fmt.Sprintf("the submission is judged already at position %d: a deferral precedes the verdict it hands to a human, never follows one", s.Verdict.Pos)}
			}
			if s.PriorClaimants[rec.Event.Actor] {
				return &NotIndependentError{Subject: subject, Actor: rec.Event.Actor, Role: "a claimant, past or present"}
			}
			if rec.Event.Actor == s.Submission.Signer {
				return &NotIndependentError{Subject: subject, Actor: rec.Event.Actor, Role: "the bound submission's signer"}
			}
			return nil
		}},
		{Name: "offer", Check: func(c *Context, rec *event.Record) error {
			// The supervisor's invitation (plans/os-c61c3392.md;
			// spec/offers.md; SEED-NEXT.md §II.9): a fact admitted
			// only while the subject folds to ready, strictly shaped,
			// whose expiry lies strictly after the event's own ts —
			// admission never reads a wall clock, so a born-dead offer
			// refuses deterministically. Capability rides the grant
			// rule; liveness (claimed-or-expire) is derived at the
			// consuming surface, never stored.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			if rec.Event.Verb != transition.OfferPublishedVerb {
				return nil
			}
			subject := rec.Event.Subject
			s, ok := c.Lifecycle.State(subject)
			if !ok {
				return &transition.InvalidTransitionError{Subject: subject, Verb: rec.Event.Verb}
			}
			if s.State != "ready" {
				return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: rec.Event.Verb}
			}
			var p struct {
				Eligibility *struct {
					Capabilities []string `json:"capabilities"`
					Tiers        []string `json:"tiers"`
					// Raw, so PRESENCE is what the version gate reads:
					// a seed/1 validator strictly decodes eligibility as
					// {capabilities, tiers} and refuses the field however
					// it is valued, and this one must agree on every
					// seed/1 record (review finding on the task PR), so
					// an explicit "tuples": [] or null before seed/2
					// refuses exactly as a populated list does.
					Tuples json.RawMessage `json:"tuples"`
				} `json:"eligibility"`
				Expires string `json:"expires"`
			}
			dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return &OfferError{Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {eligibility{capabilities, tiers, tuples}, expires}: %v", err)}
			}
			if p.Eligibility != nil && p.Eligibility.Tuples != nil {
				// Tuple scoping is seed/2's (plans/os-8e53ffd9.md D6).
				if !tuple.Applies(c.Active) {
					return &OfferError{Subject: subject, Reason: fmt.Sprintf("the offer scopes by runtime tuple and the chain is at %s: tuple semantics activate at %s", c.Active, version.Seed2)}
				}
				var members []json.RawMessage
				if err := json.Unmarshal(p.Eligibility.Tuples, &members); err != nil {
					return &OfferError{Subject: subject, Reason: fmt.Sprintf("eligibility.tuples is a list of runtime tuples: %v", err)}
				}
				for i, raw := range members {
					if _, err := tuple.Parse(raw); err != nil {
						return &OfferError{Subject: subject, Reason: fmt.Sprintf("eligibility.tuples[%d]: %v", i, err)}
					}
				}
			}
			var missing []string
			if p.Eligibility == nil {
				missing = append(missing, "eligibility")
			}
			if strings.TrimSpace(p.Expires) == "" {
				missing = append(missing, "expires")
			}
			if len(missing) > 0 {
				return &transition.IncompleteError{Verb: rec.Event.Verb, Subject: subject, Missing: missing}
			}
			exp, err := time.Parse(time.RFC3339, strings.TrimSpace(p.Expires))
			if err != nil {
				return &OfferError{Subject: subject, Reason: fmt.Sprintf("expires %q is not an RFC3339 timestamp", p.Expires)}
			}
			ts, err := time.Parse(time.RFC3339, rec.Event.TS)
			if err != nil {
				return &OfferError{Subject: subject, Reason: fmt.Sprintf("the event ts %q is not an RFC3339 timestamp to anchor expiry against", rec.Event.TS)}
			}
			if !exp.After(ts) {
				return &OfferError{Subject: subject, Reason: fmt.Sprintf("expires %s is not after the event's own ts %s — a born-dead offer invites nothing (claimed or expire, SEED-NEXT.md §II.9)", exp.Format(time.RFC3339), ts.Format(time.RFC3339))}
			}
			return nil
		}},
		{Name: "budget", Check: func(c *Context, rec *event.Record) error {
			// The reservation machinery (plans/os-cecac5de.md;
			// spec/budgets.md; SEED-NEXT.md §II.9): admission is
			// the one place with a serialized view, so the reserve is
			// checked and decremented HERE — the second of two racing
			// drafts admits against the tip that already carries the
			// first. Capability rides the grant rule; the fence rule
			// forces the holder's citation; validity and effective
			// closure are position-accurate derivations, never stored
			// state.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			isBudget := verb == transition.BudgetReserveVerb || verb == transition.BudgetSettleVerb || verb == transition.BudgetReleaseVerb
			if !isBudget && !transition.IsSpendingVerb(verb) {
				return nil
			}
			subject := rec.Event.Subject
			s, ok := c.Lifecycle.State(subject)
			if !ok {
				return &transition.InvalidTransitionError{Subject: subject, Verb: verb}
			}
			view := BudgetViewAt(c.Records, c.Table, subject, s)
			if !isBudget {
				// The spending gate (D5): a listed verb needs an open
				// valid reservation; the table ships empty and 7.3's
				// metering fills it.
				if len(view.Open) == 0 {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("%s spends, and no open valid reservation stands — spending verbs require an admitted budget.reserve (SEED-NEXT.md §II.9)", verb)}
				}
				return nil
			}
			operatorNow := c.Keyring != nil && c.Keyring.HasAnyCapability(rec.Event.Actor, []string{keyring.CapOperator})
			switch verb {
			case transition.BudgetReserveVerb:
				// Reserving is the one budget act a claim window gates
				// (plans/os-d6963652.md D1): capacity is committed for
				// the window that will spend it, so a reserve outside
				// one has nothing to spend under, and the
				// holder-or-operator check below says the same thing
				// about WHOSE window. The two closes carry no state
				// gate: a reservation outlives its window, closing one
				// honestly is wrong in no state, and a gate on the
				// verb FAMILY stranded the capacity of every attempt
				// that ended in a fail verdict.
				if s.State != "in_progress" {
					return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: verb}
				}
				var p struct {
					Amount string `json:"amount"`
					Fence  string `json:"fence"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("the reserve payload is the strict object {amount, fence}: %v", err)}
				}
				amount, err := strconv.Atoi(strings.TrimSpace(p.Amount))
				if err != nil || amount <= 0 {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("amount %q is not a positive integer of class units", p.Amount)}
				}
				if !operatorNow && (s.Claim == nil || s.Claim.Holder != rec.Event.Actor) {
					return &BudgetError{Subject: subject, Reason: "only the active claim holder or the operator lane reserves — a prior claimant's reserve would consume a budget it no longer works under"}
				}
				if !view.Known {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("budget class %q has no capacity in the class table — absent knowledge is never fudged into spendable units", s.Budget)}
				}
				if amount > view.Remaining {
					// The ONE exhaustion site out of this rule's
					// fourteen refusals, and the only one a caller
					// can act on by asking for less
					// (plans/os-d03bde01.md D1).
					return &BudgetError{Subject: subject, Exhausted: true, Reason: fmt.Sprintf("amount %d exceeds remaining %d of capacity %d — reservations are checked and decremented at admission, the serialized view", amount, view.Remaining, view.Capacity)}
				}
				return nil
			default:
				var p struct {
					Reservation string `json:"reservation"`
					Actuals     string `json:"actuals"`
					Fence       string `json:"fence"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("the close payload is the strict object {reservation%s, fence}: %v", map[bool]string{true: ", actuals"}[verb == transition.BudgetSettleVerb], err)}
				}
				if verb == transition.BudgetReleaseVerb && p.Actuals != "" {
					return &BudgetError{Subject: subject, Reason: "release frees a reservation with zero actuals — settle is the verb that records spend"}
				}
				cited, err := strconv.Atoi(strings.TrimSpace(p.Reservation))
				if err != nil {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("reservation %q is not a chain position", p.Reservation)}
				}
				var res *transition.ReservationFact
				for i := range s.Reservations {
					if s.Reservations[i].Pos == cited {
						res = &s.Reservations[i]
						break
					}
				}
				if res == nil {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("position %d is no reservation on this subject", cited)}
				}
				if !ReservationValid(c.Records, c.Table, subject, *res) {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("the reservation at position %d never passed the authoring boundary — closing it would launder it into spend history", cited)}
				}
				if closed, ok := view.ClosedBy[cited]; ok {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("the reservation at position %d is already effectively closed at position %d", cited, closed.Pos)}
				}
				if verb == transition.BudgetSettleVerb {
					n, err := strconv.Atoi(strings.TrimSpace(p.Actuals))
					if err != nil || n < 0 {
						return &BudgetError{Subject: subject, Reason: fmt.Sprintf("actuals %q is not a non-negative integer of class units", p.Actuals)}
					}
				}
				if rec.Event.Actor != res.Signer && !operatorNow {
					return &BudgetError{Subject: subject, Reason: fmt.Sprintf("only the reservation's own reserving signer or the operator lane closes it — the reservation at %d belongs to %s", cited, res.Signer)}
				}
				return nil
			}
		}},
		{Name: "run", Check: func(c *Context, rec *event.Record) error {
			// The execution-run facts (plans/os-1dad487d.md;
			// spec/executors.md): run.started is the spending
			// gate's first customer (the budget rule already required
			// an open valid reservation on the subject; this rule
			// revalidates the SPECIFIC citation, the laundering
			// shape), and run.settled is the once-per-fence aggregate
			// on a fence that carries an admitted start. Capability
			// rides the grant rule.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			if verb != transition.RunStartedVerb && verb != transition.RunSettledVerb &&
				verb != transition.RunInterruptedVerb {
				return nil
			}
			subject := rec.Event.Subject
			s, ok := c.Lifecycle.State(subject)
			if !ok {
				return &transition.InvalidTransitionError{Subject: subject, Verb: verb}
			}
			startValid := func(st transition.RunStartFact) bool {
				return RunStartValid(c.Records, c.Table, subject, st)
			}
			if verb == transition.RunInterruptedVerb {
				// The safe-point preemption request
				// (plans/os-0f718b4e.md): strict {fence}, only while a
				// run window is live, only the ACTIVE fence, once per
				// fence — gated on boundary validity, so a raw invalid
				// interrupt neither blocks the legitimate supervisor's
				// nor is one.
				var p struct {
					Fence string `json:"fence"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("the interrupt payload is the strict object {fence}: %v", err)}
				}
				if s.State != "in_progress" {
					return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: verb}
				}
				fence, err := strconv.Atoi(strings.TrimSpace(p.Fence))
				if err != nil || s.Claim == nil || fence != s.Claim.Fence {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %q is not the active claim fence — an interrupt preempts the run inside its live window", p.Fence)}
				}
				for _, it := range s.Interrupts {
					if it.Fence == fence && InterruptValid(c.Records, c.Table, subject, it) {
						return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %d already carries a run.interrupted at position %d — one interrupt per claim window", fence, it.Pos)}
					}
				}
				return nil
			}
			if verb == transition.RunStartedVerb {
				var p struct {
					Fence       string          `json:"fence"`
					Reservation string          `json:"reservation"`
					Tuple       json.RawMessage `json:"tuple,omitempty"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("the start payload is the strict object {fence, reservation, tuple}: %v", err)}
				}
				// The declared configuration (plans/os-8e53ffd9.md D3):
				// required once seed/2 is active, refused before it, so
				// a chain that never upgraded keeps its judgment. The
				// same decode RunStartValid re-runs at the record's own
				// position (review finding on the task PR).
				declared, reason := declaredTuple(c.Active, p.Tuple)
				if reason != "" {
					return &RunError{Subject: subject, Reason: reason}
				}
				if s.State != "in_progress" {
					return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: verb}
				}
				fence, err := strconv.Atoi(strings.TrimSpace(p.Fence))
				if err != nil || s.Claim == nil || fence != s.Claim.Fence {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %q is not the active claim fence — a run starts inside the claim window it spends under", p.Fence)}
				}
				for _, st := range s.RunStarts {
					if st.Fence == fence && startValid(st) {
						return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %d already carries a run.started at position %d — one run per claim window", fence, st.Pos)}
					}
				}
				cited, err := strconv.Atoi(strings.TrimSpace(p.Reservation))
				if err != nil {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("reservation %q is not a chain position", p.Reservation)}
				}
				var res *transition.ReservationFact
				for i := range s.Reservations {
					if s.Reservations[i].Pos == cited {
						res = &s.Reservations[i]
						break
					}
				}
				if res == nil {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("position %d is no reservation on this subject", cited)}
				}
				view := BudgetViewAt(c.Records, c.Table, subject, s)
				if !ReservationValid(c.Records, c.Table, subject, *res) {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("the reservation at position %d never passed the authoring boundary — a run cannot be fenced to laundered spend", cited)}
				}
				if _, closed := view.ClosedBy[cited]; closed {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("the reservation at position %d is already effectively closed — a run needs an open reservation", cited)}
				}
				// The qualification SET rule (plans/os-8e53ffd9.md D2, D4):
				// the CLAIM HOLDER's grants for claim cite zero or more
				// tuples. Zero: unqualified, the bridge, admit. Any: the
				// declared configuration must equal one member, per
				// field, or the holder is invoking a configuration its
				// grant does not cite. The holder, never the signer: the
				// supervisor signs the start, the work executes under
				// the holder's window.
				if s.Claim != nil {
					if d := tupleDrift(c.Keyring, s.Claim.Holder, declared, s.Eval != nil, keyring.CapClaim); d != nil {
						return &OutOfGrantError{Actor: rec.Event.Actor, Verb: verb,
							Accepted: keyring.AcceptedCapabilities(verb), Drift: d}
					}
				}
				return nil
			}
			var p struct {
				Fence string `json:"fence"`
				Units string `json:"units"`
				Lines string `json:"lines"`
			}
			dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return &RunError{Subject: subject, Reason: fmt.Sprintf("the settle payload is the strict object {fence, units, lines}: %v", err)}
			}
			fence, err := strconv.Atoi(strings.TrimSpace(p.Fence))
			if err != nil || !s.ClaimFences[fence] {
				return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %q is not an applied claim position on this subject", p.Fence)}
			}
			started := false
			for _, st := range s.RunStarts {
				if st.Fence == fence && startValid(st) {
					started = true
					break
				}
			}
			if !started {
				return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %d carries no admitted run.started — a run settles only after its gated initiation", fence)}
			}
			for _, r := range s.Runs {
				// Boundary-valid prior settles only (the RunStartValid
				// posture): a raw invalid settle must not permanently
				// block the legitimate aggregate.
				if r.Fence == fence && RunSettleValid(c.Records, c.Table, subject, r) {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("fence %d already carries a run.settled at position %d — one run, one aggregate", fence, r.Pos)}
				}
			}
			for name, v := range map[string]string{"units": p.Units, "lines": p.Lines} {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil || n < 0 {
					return &RunError{Subject: subject, Reason: fmt.Sprintf("%s %q is not a non-negative integer", name, v)}
				}
			}
			return nil
		}},
		{Name: "qualification", Check: func(c *Context, rec *event.Record) error {
			// The qualification verbs' cross-references
			// (plans/os-03e47abb.md D2, D4; spec/evals.md). The
			// keyring previewed the shape and standing legality in the
			// grant rule; this rule reads the lifecycle: the cited
			// contract is an eval, the cited verdict is the eval's
			// authenticated pass (or fail), the run admitted in that
			// verdict's window declared exactly the cited tuple, the
			// qualified actor is that window's holder, the record's ts
			// does not precede the verdict's, and no earlier
			// qualification cites the same verdict.
			verb := rec.Event.Verb
			if verb == "intent.filed" {
				var filed struct {
					Eval json.RawMessage `json:"eval"`
				}
				if json.Unmarshal(rec.Event.Payload, &filed) != nil || len(filed.Eval) == 0 {
					return nil
				}
				if !version.EvalApplies(c.Active) {
					// The marker is defined at seed/3 (D8): presence is
					// the gate, read raw, so a filing a seed/2
					// validator's fold would silently drop is refused
					// rather than admitted as an ordinary contract.
					return &transition.ChainError{Subject: rec.Event.Subject, Verb: verb,
						Reason: fmt.Sprintf("the filing carries an eval marker and the chain is at %s: eval semantics activate at %s (spec/evals.md)", c.Active, version.Seed3)}
				}
				return checkEvalMarker(c.Active, rec.Event.Subject, filed.Eval)
			}
			if verb != keyring.VerbQualified && verb != keyring.VerbDisqualified {
				return nil
			}
			if c.Lifecycle == nil || c.Keyring == nil {
				return nil
			}
			var p struct {
				Capability string          `json:"capability"`
				Tuple      json.RawMessage `json:"tuple"`
				Contract   string          `json:"contract"`
				Verdict    string          `json:"verdict"`
				Reason     string          `json:"reason,omitempty"`
			}
			if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
				return nil // the keyring preview refused the shape already
			}
			cited, err := tuple.Parse(p.Tuple)
			if err != nil {
				return nil
			}
			pos, err := strconv.Atoi(strings.TrimSpace(p.Verdict))
			if err != nil {
				return nil
			}
			refuse := func(reason string) error {
				return &transition.ChainError{Subject: p.Contract, Verb: verb, Reason: reason}
			}
			s, ok := c.Lifecycle.State(p.Contract)
			if !ok {
				return refuse("the cited contract is not in the fold")
			}
			if s.Eval == nil {
				return refuse("the cited contract is not an eval: only synthetic work with a known verdict qualifies or disqualifies a configuration")
			}
			if !evalBound(s) {
				return refuse(fmt.Sprintf("the cited contract names eval %q but its acceptance spec is not that definition's fixture at a gated revision: a contract may carry the marker, but only the shipped definition's own spec is an eval, and its verdict qualifies nobody", s.Eval.Name))
			}
			if pos < 0 || pos >= len(c.Records) {
				return refuse(fmt.Sprintf("the cited verdict position %d is not on the chain", pos))
			}
			var fact *transition.VerdictFact
			switch {
			case p.Capability == keyring.CapVerdict:
				// A calibration qualifies or disqualifies on agreement
				// with the gold, whichever way the verdict went
				// (plans/os-2e34f66a.md D5): the cited verdict is the
				// eval's authenticated one, pass or fail. The cited
				// contract must be a calibration (review finding on
				// the task PR): an ordinary eval's verdict proves a
				// configuration for work and says nothing about the
				// verifier's judgment, so citing one would mint verdict
				// authority past the calibration gate.
				if s.Eval.Kind != transition.EvalKindCalibration {
					return refuse(fmt.Sprintf("the cited contract is not a calibration: a %s qualification cites a calibration eval's verdict, the one compared to a committed gold, and %q is filed as an ordinary eval", keyring.CapVerdict, s.Eval.Name))
				}
				if fact = AuthenticVerdict(c, p.Contract, s); fact == nil || fact.Pos != pos {
					return refuse(fmt.Sprintf("position %d is not the calibration's authenticated verdict: a verdict qualification cites the verdict whose scorecard was compared to the gold, rendered by a verdict-granted key disjoint from the implementer", pos))
				}
			case verb == keyring.VerbQualified:
				if fact = authenticPass(c, p.Contract, s); fact == nil || fact.Pos != pos {
					return refuse(fmt.Sprintf("position %d is not the eval's authenticated pass verdict: a qualification cites the pass that proved the configuration, rendered by a verdict-granted key disjoint from the implementer", pos))
				}
			default:
				if fact = windowFail(s, pos); fact == nil || verdictBoundaryAt(c, p.Contract, "", s, *fact) != nil {
					return refuse(fmt.Sprintf("position %d is not an authenticated fail verdict on the eval: a disqualification cites the fail that ended the configuration", pos))
				}
			}
			if p.Capability == keyring.CapVerdict {
				// Calibration (plans/os-2e34f66a.md D5): the verifier
				// under calibration is the verdict's signer, and the
				// configuration it proved is the tuple the render
				// declared, never a window's.
				if fact.Tuple == nil {
					return refuse("the cited verdict declares no tuple: the verifier's declared configuration is what calibration qualifies, and the render declared none")
				}
				if !fact.Tuple.Equal(cited) {
					field, _, want, _ := cited.Diff(*fact.Tuple)
					return refuse(fmt.Sprintf("the cited tuple differs from the configuration the verifier declared: %s was %q at the render — a calibration is for the configuration that rendered, never another", field, want))
				}
				if verb == keyring.VerbQualified && rec.Event.Subject != fact.Signer {
					return refuse(fmt.Sprintf("the qualified actor %s is not the verdict's signer (%s): calibration qualifies the verifier that rendered", rec.Event.Subject, fact.Signer))
				}
			} else {
				fence, holder, ok := submissionWindow(c.Records, p.Contract, fact.Submission)
				if !ok {
					return refuse(fmt.Sprintf("the verdict's submission at position %d cites no claim window", fact.Submission))
				}
				var declared *tuple.Tuple
				for i := range s.RunStarts {
					st := s.RunStarts[i]
					if st.Fence == fence && RunStartValid(c.Records, c.Table, p.Contract, st) {
						declared = st.Tuple
						break
					}
				}
				if declared == nil {
					return refuse(fmt.Sprintf("the claim window at fence %d carries no admitted run.started declaring a tuple: the configuration that ran is what qualifies, and nothing declared one", fence))
				}
				if !declared.Equal(cited) {
					field, _, want, _ := cited.Diff(*declared)
					return refuse(fmt.Sprintf("the cited tuple differs from the configuration the run declared: %s was %q in the run — a qualification is for the configuration that ran, never another", field, want))
				}
				if verb == keyring.VerbQualified && rec.Event.Subject != holder {
					return refuse(fmt.Sprintf("the qualified actor %s is not the holder of the window that ran (%s): the supervisor declares, the holder executes, and the holder is who the eval qualifies", rec.Event.Subject, holder))
				}
			}
			if !tsNotBefore(rec.Event.TS, c.Records[pos].Event.TS) {
				return refuse(fmt.Sprintf("the record's ts %s precedes the cited verdict's %s: a qualification cannot predate the verdict it cites", rec.Event.TS, c.Records[pos].Event.TS))
			}
			for _, q := range c.Keyring.Qualifications(rec.Event.Subject) {
				if q.Contract == p.Contract && q.Verdict == pos && q.Disqualified == (verb == keyring.VerbDisqualified) {
					return refuse(fmt.Sprintf("the verdict at position %d is already cited by an earlier %s on this actor: one verdict, one consequence", pos, verb))
				}
			}
			return nil
		}},
		{Name: "chain", Check: func(c *Context, rec *event.Record) error {
			// The reconciliation chain (plans/os-6cdc15be.md;
			// spec/reconciliation.md): done is reachable only
			// through verdict.rendered(pass), merge.requested, and
			// merge.observed, in order. merge.requested is a fact
			// admitted only in review citing the pass verdict;
			// merge.observed's state legality stays the table's (the
			// lifecycle rule), and this rule holds its chain links and
			// forge-fact shape.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			switch verb {
			case transition.MergeRequestedVerb:
				subject := rec.Event.Subject
				s, ok := c.Lifecycle.State(subject)
				if !ok {
					return &transition.InvalidTransitionError{Subject: subject, Verb: verb}
				}
				if s.State != "review" {
					return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: verb}
				}
				var p struct {
					Verdict  string `json:"verdict"`
					Override string `json:"override"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the payload is the strict object {verdict} or {override}: %v", err)}
				}
				hasV, hasO := strings.TrimSpace(p.Verdict) != "", strings.TrimSpace(p.Override) != ""
				if hasV == hasO {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: "the request cites exactly one of verdict or override — the two chain paths never blur (plans/os-d2497eb7.md)"}
				}
				if ce := forgeRed(c, subject, verb, s); ce != nil {
					return ce
				}
				if hasO {
					cited, err := strconv.Atoi(strings.TrimSpace(p.Override))
					if err != nil {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("override %q is not a chain position", p.Override)}
					}
					if s.Override == nil {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: "no override stands on this subject — merge.overridden is the operator's attributable act, and citing one that does not exist substitutes for nothing"}
					}
					if cited != s.Override.Pos {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("cites position %d; the admitted override on this subject is at position %d", cited, s.Override.Pos)}
					}
					if ce := overrideBacking(c, subject, verb, s); ce != nil {
						return ce
					}
					return nil
				}
				cited, err := strconv.Atoi(strings.TrimSpace(p.Verdict))
				if err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("verdict %q is not a chain position", p.Verdict)}
				}
				if s.Verdict == nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: "no verdict has been rendered on this subject — the chain starts at verdict.rendered(pass)"}
				}
				fact, rendered := s.VerdictAt(cited)
				if latest, ok := latestVerdictFor(s, fact.Submission); !rendered || (ok && latest.Pos != fact.Pos) {
					// The admitted verdict is the latest on its
					// submission; an earlier one is stale.
					named := s.Verdict.Pos
					if ok {
						named = latest.Pos
					}
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("cites position %d; the admitted verdict on this subject is at position %d", cited, named)}
				}
				if fact.Verdict != "pass" {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the verdict at position %d is %q — a red verdict is unmergeable", fact.Pos, fact.Verdict)}
				}
				if c.Keyring != nil {
					if ce := verdictBoundary(c, subject, verb, s, fact); ce != nil {
						return ce
					}
				}
				return nil
			case transition.MergeObservedVerb:
				subject := rec.Event.Subject
				s, ok := c.Lifecycle.State(subject)
				if !ok || s.State != "review" {
					// State legality is the table's; the lifecycle rule
					// refuses with the proper positioned message.
					return nil
				}
				var p struct {
					Merged string `json:"merged"`
					PR     string `json:"pr"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the payload is the strict object {merged, pr}: %v", err)}
				}
				var missing []string
				if strings.TrimSpace(p.Merged) == "" {
					missing = append(missing, "merged")
				}
				if strings.TrimSpace(p.PR) == "" {
					missing = append(missing, "pr")
				}
				if len(missing) > 0 {
					return &transition.IncompleteError{Verb: verb, Subject: subject, Missing: missing}
				}
				if !mergedSHARE.MatchString(strings.TrimSpace(p.Merged)) {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("merged %q is not a full lowercase-hex commit — the observer records which commit the forge merged", p.Merged)}
				}
				// The override-backed path (plans/os-d2497eb7.md): an
				// admitted override plus a request citing it stands in
				// for the pass verdict plus its citation — each step
				// still its own event.
				if s.Override != nil && s.Requested != nil && s.Requested.CitedOverride == s.Override.Pos {
					if ce := overrideBacking(c, subject, verb, s); ce != nil {
						return ce
					}
					return nil
				}
				if s.Verdict == nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: "no pass verdict on this subject — done is reachable only through the full chain"}
				}
				if s.Requested == nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("no merge.requested cites the pass verdict at position %d — each chain step is its own event", s.Verdict.Pos)}
				}
				fact, rendered := s.VerdictAt(s.Requested.CitedVerdict)
				if latest, ok := latestVerdictFor(s, fact.Submission); rendered && ok && latest.Pos != fact.Pos {
					rendered = false
				}
				if !rendered || fact.Verdict != "pass" {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: "no pass verdict on this subject — done is reachable only through the full chain"}
				}
				if c.Keyring != nil {
					if ce := verdictBoundary(c, subject, verb, s, fact); ce != nil {
						return ce
					}
				}
				return nil
			case transition.MergeOverriddenVerb:
				// The operator's attributable substitute for a pass
				// verdict (plans/os-d2497eb7.md): admitted only over a
				// standing, boundary-validated fail on the current
				// submission — an escape hatch, never a bypass.
				subject := rec.Event.Subject
				s, ok := c.Lifecycle.State(subject)
				if !ok {
					return &transition.InvalidTransitionError{Subject: subject, Verb: verb}
				}
				if s.State != "review" {
					return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: verb}
				}
				var p struct {
					Reason  string `json:"reason"`
					Verdict string `json:"verdict"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the payload is the strict object {reason, verdict}: %v", err)}
				}
				var missing []string
				if strings.TrimSpace(p.Reason) == "" {
					missing = append(missing, "reason")
				}
				if strings.TrimSpace(p.Verdict) == "" {
					missing = append(missing, "verdict")
				}
				if len(missing) > 0 {
					return &transition.IncompleteError{Verb: verb, Subject: subject, Missing: missing}
				}
				if s.Override != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("an override already stands at position %d — one override per submission window; a return and a new submission open the next", s.Override.Pos)}
				}
				cited, err := strconv.Atoi(strings.TrimSpace(p.Verdict))
				if err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("verdict %q is not a chain position", p.Verdict)}
				}
				fail := windowFail(s, cited)
				if fail == nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("position %d is not a fail verdict on the current submission — the override overrules a standing red verdict, it never routes around independent verification", cited)}
				}
				if ce := verdictBoundary(c, subject, verb, s, *fail); ce != nil {
					return ce
				}
				return nil
			case transition.ContractReturnedVerb:
				// State legality is the table's (the lifecycle rule);
				// this case holds the citation: the return is
				// authorized by a standing, boundary-validated fail
				// (plans/os-d2497eb7.md).
				subject := rec.Event.Subject
				s, ok := c.Lifecycle.State(subject)
				if !ok || s.State != "review" {
					return nil
				}
				var p struct {
					Verdict     string `json:"verdict"`
					Observation string `json:"observation"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the payload is the strict object {verdict} or {observation}: %v", err)}
				}
				hasV, hasO := strings.TrimSpace(p.Verdict) != "", strings.TrimSpace(p.Observation) != ""
				if !hasV && !hasO {
					return &transition.IncompleteError{Verb: verb, Subject: subject, Missing: []string{"verdict"}}
				}
				if hasV && hasO {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: "the return cites exactly one of verdict or observation — the fail verdict's path and the red observation's never blur (plans/os-0cd18799.md D4)"}
				}
				if hasO {
					// The red observation's return (plans/os-0cd18799.md
					// D4): a routing decision, not a verdict, so it
					// records no lockout and the prior submitter is the
					// natural next claimant. Only the LATEST observation
					// authorizes it: a later green one supersedes a red.
					if !version.ForgeChecksApply(c.Active) {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("an observation citation is not defined at %s: the forge observation needs a chain at %s", c.Active, version.Seed8)}
					}
					cited, err := strconv.Atoi(strings.TrimSpace(p.Observation))
					if err != nil {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("observation %q is not a chain position", p.Observation)}
					}
					if s.Observation == nil {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: "no forge observation stands on this submission — a return by observation cites what the observer recorded"}
					}
					if cited != s.Observation.Pos {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("cites position %d; the standing observation on this submission is at position %d — a superseded observation authorizes nothing", cited, s.Observation.Pos)}
					}
					if head, ok := submissionHead(c, subject, s); !ok || head != s.Observation.Head {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the observation at position %d names head %s, which is not the submission under review", s.Observation.Pos, s.Observation.Head)}
					}
					if !s.Observation.Red() {
						return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the observation at position %d reports the submission mergeable (%s) — nobody yanks a green pull request", s.Observation.Pos, describeObservation(*s.Observation))}
					}
					return nil
				}
				cited, err := strconv.Atoi(strings.TrimSpace(p.Verdict))
				if err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("verdict %q is not a chain position", p.Verdict)}
				}
				fail := windowFail(s, cited)
				if fail == nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("position %d is not a fail verdict on the current submission — nobody yanks an in-review contract whose verdict is pass or pending", cited)}
				}
				if ce := verdictBoundary(c, subject, verb, s, *fail); ce != nil {
					return ce
				}
				return nil
			}
			return nil
		}},
		{Name: "seal", Check: func(c *Context, rec *event.Record) error {
			// The sealed-checks commitment and its authoring isolation
			// (plans/os-3128535a.md; spec/sealed-checks.md).
			// Capability rides the grant rule (sealer only); grant
			// disjointness rides the keyring preview.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			switch verb {
			case transition.CheckSealedVerb:
				subject := rec.Event.Subject
				s, ok := c.Lifecycle.State(subject)
				if !ok {
					return &transition.InvalidTransitionError{Subject: subject, Verb: verb}
				}
				if s.State != "ready" {
					return &transition.InvalidTransitionError{Subject: subject, From: s.State, Verb: verb}
				}
				if len(s.PriorClaimants) > 0 {
					// Ready again after claim.released or claim.reaped:
					// implementation already began, and a commitment
					// appended now would not prove pre-existence
					// (review finding on the 6.3 plan).
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: "the subject has already been claimed — a commitment after implementation began proves nothing; cancel and re-file to change sealed checks"}
				}
				if s.Sealed != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("a commitment already stands at position %d — one seal per subject; rotation re-encrypts, never re-commits", s.Sealed.Pos)}
				}
				var p struct {
					Commitment string `json:"commitment"`
				}
				dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the payload is the strict object {commitment}: %v", err)}
				}
				if strings.TrimSpace(p.Commitment) == "" {
					return &transition.IncompleteError{Verb: verb, Subject: subject, Missing: []string{"commitment"}}
				}
				if !receiptDigestRE.MatchString(strings.TrimSpace(p.Commitment)) {
					return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("commitment %q is not a lowercase-hex sha256 — the ledger references the sealed body by its salted hash", p.Commitment)}
				}
				return nil
			case "claim.taken":
				// The per-subject half of authoring isolation: the key
				// that sealed the checks never implements against them.
				s, ok := c.Lifecycle.State(rec.Event.Subject)
				if ok && s.Sealed != nil && s.Sealed.Signer == rec.Event.Actor {
					return &transition.ChainError{Subject: rec.Event.Subject, Verb: verb, Reason: fmt.Sprintf("the claiming key authored this subject's sealed checks at position %d — sealed checks are authored under a grant disjoint from implementation", s.Sealed.Pos)}
				}
				return nil
			}
			return nil
		}},
		{Name: "lifecycle", Check: func(c *Context, rec *event.Record) error {
			// Lifecycle legality is admission policy at seed/1, the
			// halt/classification/grant precedent (plans/os-d69a6c91.md):
			// seed/0 records are grandfathered inert, verification
			// tolerates illegal history, and the projection fold skips
			// it visibly. The table is the only legality authority.
			if !keyring.Applies(c.Active) || c.Table == nil || c.Lifecycle == nil {
				return nil
			}
			verb := rec.Event.Verb
			if !c.Table.IsLifecycleVerb(verb) {
				return nil
			}
			if err := transition.CheckCompleteness(verb, rec.Event.Subject, rec.Event.Payload); err != nil {
				return err
			}
			current := ""
			var claim *transition.Claim
			s, known := c.Lifecycle.State(rec.Event.Subject)
			if known {
				current = s.State
				claim = s.Claim
			}
			if c.Table.Exclusive(verb) && claim != nil {
				// Exclusivity not granted: the subject is held. The
				// loser learns who holds and since when (exit 2).
				// Racing (plans/os-56bee171.md D2): on a racing squad's
				// contract, at seed/6, a further claim admits below the
				// declared cap for a claimant holding none — a claim
				// like any other, exclusive in the table's sense
				// (granted here, never offline), its fence its own
				// position.
				if version.RacingApplies(c.Active) && c.Declaration != nil && current == "in_progress" {
					if racers, _, races := c.Declaration.RacingFor(s.Routing); races {
						if own, holds := s.HolderFence(rec.Event.Actor); holds {
							// A racer that already holds a claim is refused
							// naming its own fence, not the first holder's.
							return &ContentionError{Subject: rec.Event.Subject, Holder: rec.Event.Actor, Fence: own}
						}
						if len(s.Claims) >= racers {
							return &ContentionError{Subject: rec.Event.Subject, Holder: claim.Holder, Fence: claim.Fence, Racers: s.Claims, Cap: racers}
						}
						return nil
					}
				}
				return &ContentionError{Subject: rec.Event.Subject, Holder: claim.Holder, Fence: claim.Fence}
			}
			// A racer's claim-scoped exit (D3) closes its own claim and
			// moves no state, so the table's origin check does not
			// apply to it; the fence and packet rules still do.
			if known && s.Racing && transition.IsExit(verb) && version.RacingApplies(c.Active) {
				if cited, has := fenceCitation(rec.Event.Payload); has {
					if fence, ferr := strconv.Atoi(cited); ferr == nil && s.ClaimScopedExit(verb, fence) {
						return nil
					}
				}
			}
			if verb == "contract.specified" && current == "ready" && !version.LevelsApply(c.Active) {
				// The ready origin is the table's seed/4 row
				// (plans/os-6bd9ffff.md D4): re-specification is the
				// dispatcher revising its own triage, and a validator
				// of an earlier version judges the record by its own
				// table, so the row refuses by version before it,
				// naming what activates it. Every other origin the
				// table refuses stays the table's refusal.
				return &transition.InvalidTransitionError{Subject: rec.Event.Subject, From: current, Verb: verb, Reason: transition.RespecificationNeeds(c.Active)}
			}
			if _, err := c.Table.Check(rec.Event.Subject, current, verb); err != nil {
				return err
			}
			if verb == "claim.taken" {
				// Effective readiness (plans/os-f0ae2cdf.md D4, D5): the
				// table admits a claim on ready; the graph refuses one
				// on a subject that waits on an open dependency or sits
				// under a blocked ancestor, the same predicate the
				// queue and the poll hide it by. Relation-free chains
				// never reach the fold.
				return Topology(c).Check(rec.Event.Subject)
			}
			return nil
		}},
		{Name: "flywheel", Check: func(c *Context, rec *event.Record) error {
			// The flywheel's two facts (plans/os-9075c308.md D4;
			// spec/flywheel.md): a proposal from the curator's
			// grant cites at least RecurringAfter distinct admitted
			// done occurrences folding to its shape, recomputed from
			// the record, names a file under the registry, stands
			// alone for its shape, and cites a passed repair where one
			// exists; a merge observation cites the standing proposal.
			// Capability rides the grant rule.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil || c.Table == nil {
				return nil
			}
			subject := rec.Event.Subject
			switch rec.Event.Verb {
			case flywheel.ProposedVerb:
				p, err := flywheel.ParseProposal(subject, rec.Event.Payload)
				if err != nil {
					return err
				}
				return flywheel.CheckProposal(c.Records, c.Lifecycle, subject, p)
			case flywheel.MergedVerb:
				m, err := flywheel.ParseMerge(subject, rec.Event.Payload)
				if err != nil {
					return err
				}
				return flywheel.CheckMerge(c.Records, c.Lifecycle, subject, m)
			}
			return nil
		}},
		{Name: "curation", Check: func(c *Context, rec *event.Record) error {
			// The staged curation stores (plans/os-f30ee0d3.md;
			// spec/curation.md): a dead end is the window holder's
			// candidate, citing the active fence; a hypothesis lives on
			// the subject its claim derives, cites at least two
			// admitted observations on two distinct non-failed
			// contracts, and refuses as a duplicate once proposed; a
			// promotion cites an admitted hypothesis on its own subject.
			// Capability rides the grant rule (the proposal's curate
			// alone); no stage skips, by citation.
			if !keyring.Applies(c.Active) || c.Lifecycle == nil || c.Table == nil {
				return nil
			}
			return checkCuration(c, rec)
		}},
	}
}

func checkCuration(c *Context, rec *event.Record) error {
	subject := rec.Event.Subject
	switch rec.Event.Verb {
	case curation.DeadEndVerb:
		d, err := curation.ParseDeadEnd(subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		// The window and the fence are the fence rule's: a dead end
		// always cites a fence, so outside a window it has already
		// refused there ("no claim is active"), and a stale citation
		// too. What the fence rule lets through is a claim key that
		// is not the holder citing the right fence, and that is this
		// rule's refusal: a candidate observation is the holder's own.
		s, ok := c.Lifecycle.State(subject)
		if !ok || s.Claim == nil {
			return &FenceError{Subject: subject, Cited: d.Fence, Active: -1}
		}
		if rec.Event.Actor != s.Claim.Holder {
			return curation.NewGateError(curation.GateDeadEndHolder, rec.Event.Verb, subject, fmt.Sprintf("signer %s is not the window's holder %s: a candidate observation is the holder's own", rec.Event.Actor, s.Claim.Holder))
		}
		return nil
	case curation.HypothesisVerb:
		h, err := curation.ParseHypothesis(subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		if prior, dup := curation.Fold(c.Records).Hypothesis(subject); dup {
			return curation.NewGateError(curation.GateSupportDuplicate, rec.Event.Verb, subject, fmt.Sprintf("the claim was proposed at position %d: one claim with one exception set derives one subject, and a re-proposal changes nothing", prior.Pos))
		}
		_, err = curation.CheckSupport(c.Records, c.Table, c.Lifecycle, subject, h)
		return err
	case curation.ContestVerb:
		// The contested state (plans/os-96850e5a.md D3): held-out
		// evidence, by construction, on contracts the predicate
		// selects, against an admitted hypothesis on its own subject.
		ct, err := curation.ParseContest(subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		_, err = curation.CheckContest(c.Records, c.Table, c.Lifecycle, subject, ct)
		return err
	case curation.LessonVerb:
		// The promotion gate's ledger half (plans/os-96850e5a.md D4):
		// an admitted, uncontested hypothesis whose support still
		// satisfies the arms, and an adversarial evaluation that is an
		// authenticated pass, replayed at its own position, on an eval
		// filed after the hypothesis and bound to it and to this
		// lesson anchor (D5).
		l, err := curation.ParseLesson(subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		// One implementation for the boundary and the fold: the
		// promotion is judged here exactly as the fold re-judges it at
		// its own position (review finding on the task PR).
		return curation.CheckPromotion(c.Records, c.Table, c.Lifecycle, subject, l)
	case curation.RetireVerb:
		// Retirement (plans/os-0d537fbd.md D2): the cited promotion is
		// the latest admitted one of its path, the reason's field
		// rides it, and superseded_by names a later admitted
		// promotion. The fold re-judges it through the same check.
		r, err := curation.ParseRetirement(subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		_, err = curation.CheckRetirement(c.Records, c.Table, subject, r)
		return err
	case curation.DeadEndRetireVerb, curation.DeadEndUnretireVerb:
		// A dead end retires and un-retires on the environment, by a
		// curator's attributable act (plans/os-0d537fbd.md D3): the
		// citation is an admitted dead end, the environment moved,
		// and the standing act is the one the verb expects.
		d, err := curation.ParseDeadEndRetirement(rec.Event.Verb, subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		return curation.CheckDeadEndRetirement(c.Records, c.Table, rec.Event.Verb, subject, d)
	}
	return nil
}

// Run applies the rules in order; the first refusing rule wraps its
// error in a Refusal.
func Run(ctx *Context, rec *event.Record, rules []Rule) error {
	for _, r := range rules {
		if err := r.Check(ctx, rec); err != nil {
			return &Refusal{Rule: r.Name, Err: err}
		}
	}
	return nil
}

// Check runs the default rule set.
func Check(ctx *Context, rec *event.Record) error {
	return Run(ctx, rec, Default())
}

// Validate adapts the rule set to the gitref.Validate seam: the closure
// the cooperative client hands to AppendLoop (2.2) and the hook embeds
// (2.3). The store the loop passes is the refreshed materialized tip, so
// every retry re-runs admission against current state.
func Validate(opts ...Option) func(*ledger.Store, *event.Record) error {
	return func(store *ledger.Store, rec *event.Record) error {
		ctx, err := ContextAt(store, opts...)
		if err != nil {
			return err
		}
		return Check(ctx, rec)
	}
}

// receiptDigestRE is the verdict payload's receipt-digest wire form.
var receiptDigestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// mergedSHARE is merge.observed's forge-fact wire form: a full
// lowercase-hex commit (40-64 hex, the classify anchor convention).
var mergedSHARE = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

// launderedVerdict refuses an admitted chain step built on a folded
// verdict that never passed the verifier boundary (review finding on
// the 6.2 task PR): in raw-pushed history any active signer can plant
// a verdict.rendered, and without this check an ADMITTED
// merge.requested or merge.observed would launder it into a clean
// chain. The signer must hold the verdict capability (checked against
// the tip keyring, the v0 approximation of standing at the verdict's
// own position; seed reconcile replays position-accurately) and be
// disjoint from the implementing keys, exactly what the verdict rule
// enforces on the front door.
func launderedVerdict(c *Context, subject, verb string, s transition.SubjectState) *transition.ChainError {
	if c.Keyring == nil || s.Verdict == nil {
		return nil
	}
	return verdictBoundary(c, subject, verb, s, *s.Verdict)
}

// verdictBoundary checks one folded verdict fact against the verifier
// boundary: the signer holds the verdict grant (tip keyring, the v0
// approximation) and is no implementing key on the contract.
func verdictBoundary(c *Context, subject, verb string, s transition.SubjectState, fact transition.VerdictFact) *transition.ChainError {
	if c.Keyring == nil {
		return nil
	}
	signer := fact.Signer
	if !c.Keyring.HasAnyCapability(signer, keyring.AcceptedCapabilities(transition.VerdictRenderedVerb)) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d was signed by %s, which holds no verdict grant — a raw-pushed verdict cannot be laundered through the admitted chain", fact.Pos, signer)}
	}
	if s.PriorClaimants[signer] || (s.Submission != nil && signer == s.Submission.Signer) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d was signed by implementing key %s — L1 independence is not launderable through the admitted chain", fact.Pos, signer)}
	}
	if err := levelBoundary(c, subject, verb, s, fact); err != nil {
		return err
	}
	return scoreBoundary(c.Keyring, subject, verb, s, fact)
}

// authenticFail returns the first fail verdict in the current
// submission window whose signer passes the verifier boundary: the
// red-verdict lockout consults only authenticated fails, and scanning
// the window means a later raw verdict can never bury one
// (plans/os-d2497eb7.md).
// submissionWindow reads the claim window a submission closed: the
// fence its holder-signed payload cites and the holder, the signer of
// the claim.taken at that fence. Both are record facts, so a raw
// submission citing no fence, or a fence that is no claim on the
// subject, yields nothing.
func submissionWindow(records []*event.Record, subject string, submission int) (fence int, holder string, ok bool) {
	if submission < 0 || submission >= len(records) {
		return 0, "", false
	}
	var p struct {
		Fence string `json:"fence"`
	}
	if err := json.Unmarshal(records[submission].Event.Payload, &p); err != nil {
		return 0, "", false
	}
	fence, err := strconv.Atoi(strings.TrimSpace(p.Fence))
	if err != nil || fence < 0 || fence >= len(records) {
		return 0, "", false
	}
	claim := records[fence].Event
	if claim.Verb != "claim.taken" || claim.Subject != subject {
		return 0, "", false
	}
	return fence, claim.Actor, true
}

// tsNotBefore reports whether a is at or after b as RFC3339 instants;
// an unparseable pair is never "not before".
func tsNotBefore(a, b string) bool {
	ta, err := time.Parse(time.RFC3339, a)
	if err != nil {
		return false
	}
	tb, err := time.Parse(time.RFC3339, b)
	if err != nil {
		return false
	}
	return !ta.Before(tb)
}

// authenticPass is the pass half (plans/os-03e47abb.md D2): the
// subject's latest admitted verdict, when it is a pass whose signer
// held the verdict capability and was no implementing key. This is the
// admission-side check; the mint decision additionally recomputes the
// receipt, because admission cannot tell an invented digest from a real
// one (spec/evals.md).
func authenticPass(c *Context, subject string, s transition.SubjectState) *transition.VerdictFact {
	if s.Verdict == nil || s.Verdict.Verdict != "pass" {
		return nil
	}
	if verdictBoundaryAt(c, subject, "", s, *s.Verdict) != nil {
		return nil
	}
	return s.Verdict
}

// AuthenticVerdict is the subject's latest admitted verdict, pass or
// fail, when its signer passed the verifier boundary at the verdict's
// own position (plans/os-2e34f66a.md D5): what a calibration compares
// to its gold, whichever way it went.
func AuthenticVerdict(c *Context, subject string, s transition.SubjectState) *transition.VerdictFact {
	if s.Verdict == nil || verdictBoundaryAt(c, subject, "", s, *s.Verdict) != nil {
		return nil
	}
	return s.Verdict
}

// verdictBoundaryAt is verdictBoundary replayed to the verdict's own
// position (plans/os-03e47abb.md D2; review finding on the task PR):
// the signer held a verdict grant THERE, so a raw-pushed verdict from an
// ungranted key does not become authentic when the key is granted
// later, and a legitimate verdict does not stop being one when its
// signer is suspended afterwards. The independence half reads the
// fold's claimant set as VerifyVerdicts does.
func verdictBoundaryAt(c *Context, subject, verb string, s transition.SubjectState, fact transition.VerdictFact) *transition.ChainError {
	if c.Keyring == nil {
		return nil
	}
	if fact.Pos < 0 || fact.Pos > len(c.Records) {
		return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the cited verdict position %d is not on the chain", fact.Pos)}
	}
	ring, _, err := keyring.StateAt(c.Records[:fact.Pos])
	if err != nil {
		return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the keyring cannot be replayed to the verdict's position %d: %v", fact.Pos, err)}
	}
	signer := fact.Signer
	if !ring.HasAnyCapability(signer, keyring.AcceptedCapabilities(transition.VerdictRenderedVerb)) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d was signed by %s, which held no verdict grant there — a raw-pushed verdict never passed the verifier boundary, and a later grant does not reach back", fact.Pos, signer)}
	}
	if s.PriorClaimants[signer] || (s.Submission != nil && signer == s.Submission.Signer) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d was signed by implementing key %s — L1 independence never held", fact.Pos, signer)}
	}
	if err := levelBoundary(c, subject, verb, s, fact); err != nil {
		return err
	}
	return scoreBoundary(ring, subject, verb, s, fact)
}

// scoreBoundary reapplies the rubric derivation and the human-verdict
// standing to a folded verdict (plans/os-2e34f66a.md D3, D4): a verdict
// whose own items refute it, or a machine verdict on a submission a
// human owed, authenticates nothing, so the merge chain cannot cite
// it and the lockout does not count it. The ring is the one the
// caller judges standing at: the tip for the live boundary, the
// verdict's own position for the replay.
func scoreBoundary(ring *keyring.State, subject, verb string, s transition.SubjectState, fact transition.VerdictFact) *transition.ChainError {
	if !fact.Levels {
		return nil
	}
	if fact.Scorecard != nil {
		derived, code, item := transition.DeriveScores(fact.Scorecard.Items)
		if code == transition.CodeHumanVerdict {
			return &transition.ChainError{Subject: subject, Verb: verb,
				Reason: fmt.Sprintf("the cited verdict at position %d scores item %q at high uncertainty — a verdict that fails its own derivation authenticates nothing", fact.Pos, item)}
		}
		if fact.Verdict != derived {
			named := "every item scored pass"
			if item != "" {
				named = fmt.Sprintf("item %q scored fail", item)
			}
			return &transition.ChainError{Subject: subject, Verb: verb,
				Reason: fmt.Sprintf("the cited verdict at position %d records %s over %s — a verdict that fails its own derivation authenticates nothing", fact.Pos, fact.Verdict, named)}
		}
	}
	if human, why := HumanVerdictRequired(s, fact.Pos); human && !OperatorStanding(ring, fact.Signer) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited verdict at position %d is a machine's where %s: its signer %s holds no operator standing, and a human's verdict is not launderable through a verdict-only key", fact.Pos, why, fact.Signer)}
	}
	return nil
}

// HumanVerdictRequired reports whether a verdict at the position on
// the current submission is a human's (plans/os-2e34f66a.md D4): the
// tier's human-review column, or a deferral standing on the window
// before the position.
func HumanVerdictRequired(s transition.SubjectState, pos int) (bool, string) {
	if transition.TierGates(s.Tier).HumanReview {
		return true, fmt.Sprintf("tier %q requires human review", s.Tier)
	}
	if s.Deferred != nil && s.Deferred.Pos < pos {
		return true, fmt.Sprintf("the verifier deferred at position %d naming %s", s.Deferred.Pos, strings.Join(s.Deferred.Items, ", "))
	}
	return false, ""
}

// OperatorStanding is the tree's one structural proxy for a person:
// a governance root's implicit standing or an explicit operator grant.
func OperatorStanding(ring *keyring.State, fp string) bool {
	return ring != nil && (ring.IsActiveRoot(fp) || ring.HasAnyCapability(fp, []string{keyring.CapOperator}))
}

// parseScorecardRef decodes the payload half of a scorecard strictly:
// a sha256 digest and items with unique non-empty ids and the two
// enums.
func parseScorecardRef(raw json.RawMessage) (*transition.ScorecardRef, error) {
	var ref transition.ScorecardRef
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ref); err != nil {
		return nil, fmt.Errorf("scorecard is the strict object {digest, items: [{id, score, uncertainty}]}: %v", err)
	}
	if !receiptDigestRE.MatchString(ref.Digest) {
		return nil, fmt.Errorf("scorecard digest %q is not a lowercase-hex sha256 digest of the scorecard's JCS bytes", ref.Digest)
	}
	seen := map[string]bool{}
	for _, it := range ref.Items {
		if strings.TrimSpace(it.ID) == "" || seen[it.ID] {
			return nil, fmt.Errorf("scorecard items name each rubric item once, non-empty: got %q", it.ID)
		}
		seen[it.ID] = true
		if it.Score != "pass" && it.Score != "fail" {
			return nil, fmt.Errorf("scorecard item %q scores %q: the vocabulary is pass, fail", it.ID, it.Score)
		}
		if it.Uncertainty != "low" && it.Uncertainty != "high" {
			return nil, fmt.Errorf("scorecard item %q carries uncertainty %q: the vocabulary is low, high", it.ID, it.Uncertainty)
		}
	}
	return &ref, nil
}

// qualifiedFail is authenticFail with the boundary replayed to each
// fail's own position: what a disqualification may cite.
func qualifiedFail(c *Context, subject string, s transition.SubjectState) *transition.VerdictFact {
	for i := range s.SubmissionFails {
		if verdictBoundaryAt(c, subject, "", s, s.SubmissionFails[i]) == nil {
			return &s.SubmissionFails[i]
		}
	}
	return nil
}

// evalBound reports whether an eval subject's acceptance spec is the
// named definition's fixture at a gated revision: the boundary binds
// the marker by PATH (it reads no repository), and the derivation binds
// it to the reviewed anchor (internal/eval). A contract that names an
// eval and cites any other spec is not an eval, whatever its verdict.
func evalBound(s transition.SubjectState) bool {
	return s.Eval != nil && s.Acceptance != nil && s.Acceptance.Executable && s.Acceptance.Gated &&
		strings.HasPrefix(s.Acceptance.Ref, transition.EvalFixturePrefix(s.Eval.Name))
}

func authenticFail(c *Context, subject string, s transition.SubjectState) *transition.VerdictFact {
	for i := range s.SubmissionFails {
		// A fail locks the submission it judged: on a racing subject
		// another racer's submission is another window.
		if s.Submission != nil && s.SubmissionFails[i].Submission != s.Submission.Pos {
			continue
		}
		if verdictBoundary(c, subject, "", s, s.SubmissionFails[i]) == nil {
			return &s.SubmissionFails[i]
		}
	}
	return nil
}

// windowFail finds the fail verdict at the cited position in the
// current submission window, nil when no such fail exists there.
// submissionHead reads the head the submission under review names:
// the tail of its packet's base range (spec/packets.md), the
// commit the forge's checks and threads are about
// (plans/os-0cd18799.md D1). A submission whose packet names no
// parseable head binds no observation.
func submissionHead(c *Context, subject string, s transition.SubjectState) (string, bool) {
	if s.Submission == nil || s.Submission.Pos < 0 || s.Submission.Pos >= len(c.Records) {
		return "", false
	}
	p, err := packet.FromPayload(subject, c.Records[s.Submission.Pos].Event.Payload)
	if err != nil {
		return "", false
	}
	_, head, ok := strings.Cut(p.Base, "..")
	head = strings.TrimSpace(head)
	if !ok || head == "" {
		return "", false
	}
	return head, true
}

// forgeRed refuses a merge request while the forge's latest word on
// the head under review says the submission is not mergeable
// (plans/os-0cd18799.md D4): a chain that branch protection would hold
// anyway must not sit unreconciled behind it. A later observation
// supersedes; an observation on another head binds nothing here.
func forgeRed(c *Context, subject, verb string, s transition.SubjectState) *transition.ChainError {
	if s.Observation == nil || !s.Observation.Red() {
		return nil
	}
	if head, ok := submissionHead(c, subject, s); !ok || head != s.Observation.Head {
		return nil
	}
	return &transition.ChainError{Subject: subject, Verb: verb, Reason: fmt.Sprintf("the forge's latest observation at position %d reports %s on head %s — a red pull request is unmergeable until a later observation supersedes it", s.Observation.Pos, describeObservation(*s.Observation), s.Observation.Head)}
}

// describeObservation renders an observation's three facts for a
// refusal: literals and a count, never forge prose.
func describeObservation(o transition.CheckFact) string {
	threads := "threads unknown"
	if o.Threads != nil {
		threads = fmt.Sprintf("%d unresolved thread(s)", *o.Threads)
	}
	return fmt.Sprintf("checks %s, %s, review %s", o.Checks, threads, o.Review)
}

func windowFail(s transition.SubjectState, pos int) *transition.VerdictFact {
	for i := range s.SubmissionFails {
		if s.SubmissionFails[i].Pos == pos {
			return &s.SubmissionFails[i]
		}
	}
	return nil
}

// launderedOverride checks the folded override's signer against the
// operator boundary (tip keyring): a raw-pushed override by an
// ungranted key substitutes for nothing (plans/os-d2497eb7.md).
func launderedOverride(c *Context, subject, verb string, o *transition.OverrideFact) *transition.ChainError {
	if c.Keyring == nil || o == nil {
		return nil
	}
	if !c.Keyring.HasAnyCapability(o.Signer, keyring.AcceptedCapabilities(transition.MergeOverriddenVerb)) {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the cited override at position %d was signed by %s, which holds no operator standing — a raw-pushed override substitutes for nothing", o.Pos, o.Signer)}
	}
	return nil
}

// overrideBacking validates everything a chain step must re-check
// before trusting the folded override: the signer's operator standing
// AND the override's own citation — a standing, boundary-validated
// fail on the current submission. The override admission rule checks
// the citation for cooperative appends, but a raw-pushed well-shaped
// override by an operator-capable key folds without it, and trusting
// it here would turn the escape hatch into a wholesale bypass (review
// finding on the task PR).
func overrideBacking(c *Context, subject, verb string, s transition.SubjectState) *transition.ChainError {
	if ce := launderedOverride(c, subject, verb, s.Override); ce != nil {
		return ce
	}
	if s.Override == nil {
		return nil
	}
	fail := windowFail(s, s.Override.CitedVerdict)
	if fail == nil {
		return &transition.ChainError{Subject: subject, Verb: verb,
			Reason: fmt.Sprintf("the override at position %d cites position %d, which is not a fail verdict on the current submission — an override overrules a standing red verdict, it never routes around independent verification", s.Override.Pos, s.Override.CitedVerdict)}
	}
	if ce := verdictBoundary(c, subject, verb, s, *fail); ce != nil {
		return ce
	}
	return nil
}

// fenceCitation extracts the payload's fence field: the string form
// of the cited claim position, absent when the payload carries none.
// ReservationValid replays the keyring and the fold to the
// reservation's own position (plans/os-cecac5de.md D4, the
// VerifySeals pattern): valid iff the signer was the operator lane
// there, or held the claim capability AND was the subject's ACTIVE
// claim holder there. Prior claimants are excluded — a released
// worker reserving under the next holder's window would consume a
// budget it no longer works under (review finding on plan #147).
func ReservationValid(records []*event.Record, table *transition.Table, subject string, r transition.ReservationFact) bool {
	if r.Pos < 0 || r.Pos >= len(records) {
		return false
	}
	ring, _, err := keyring.StateAt(records[:r.Pos])
	if err != nil || ring == nil {
		return false
	}
	if ring.HasAnyCapability(r.Signer, []string{keyring.CapOperator}) {
		return true
	}
	if !ring.HasAnyCapability(r.Signer, []string{keyring.CapClaim}) {
		return false
	}
	s, ok := table.StateAt(records[:r.Pos], subject)
	return ok && s.Claim != nil && s.Claim.Holder == r.Signer
}

// BudgetCloseValid reports whether a close attempt may close the
// cited reservation: its signer is the reservation's own reserving
// signer (identity, not escalation), or the operator lane at the
// attempt's position — so a raw foreign settle or release never
// closes anyone's reservation (review finding on plan #147).
func BudgetCloseValid(records []*event.Record, c transition.CloseFact, r transition.ReservationFact) bool {
	if c.Signer == r.Signer {
		return true
	}
	if c.Pos < 0 || c.Pos >= len(records) {
		return false
	}
	ring, _, err := keyring.StateAt(records[:c.Pos])
	return err == nil && ring != nil && ring.HasAnyCapability(c.Signer, []string{keyring.CapOperator})
}

// BudgetViewAt derives the subject's budget view over the verified
// prefix with the position-accurate callbacks: the one computation
// admission, seed budget status, and the projections share.
func BudgetViewAt(records []*event.Record, table *transition.Table, subject string, s transition.SubjectState) transition.BudgetView {
	return s.DeriveBudget(
		func(r transition.ReservationFact) bool {
			return ReservationValid(records, table, subject, r)
		},
		func(c transition.CloseFact, r transition.ReservationFact) bool {
			return BudgetCloseValid(records, c, r)
		},
	)
}

// runFactRecord returns the record a folded run fact points at, when
// it is the expected verb on the expected subject: fold positions
// index the verified record sequence directly, and the record, not
// the fact, is the authority the boundary re-judges.
func runFactRecord(records []*event.Record, pos int, verb, subject string) (*event.Record, bool) {
	if pos < 0 || pos >= len(records) {
		return nil, false
	}
	rec := records[pos]
	if rec.Event.Verb != verb || rec.Event.Subject != subject {
		return nil, false
	}
	return rec, true
}

// strictRunPayload re-runs the verb's strict payload decode on the
// record itself (review finding on this PR: the tolerant fold accepts
// shapes admission refuses, so a validity helper that skips the shape
// check launders a would-be-refused raw push into effect). The
// decoded fields must round-trip to the folded fact exactly.
func strictRunPayload(rec *event.Record, into any) bool {
	dec := json.NewDecoder(bytes.NewReader(rec.Event.Payload))
	dec.DisallowUnknownFields()
	return dec.Decode(into) == nil
}

// declaredTuple decodes a run.started's declaration under the version
// it is judged at (plans/os-8e53ffd9.md D3, D8): required and strict
// once seed/2 is active, refused before it, so a chain that never
// upgraded keeps its judgment. A non-empty reason is the refusal, in
// the run rule's own words; the admission rule and RunStartValid share
// it so a raw-pushed start is judged exactly as a proposed one.
func declaredTuple(v string, raw json.RawMessage) (*tuple.Tuple, string) {
	if tuple.Applies(v) {
		if len(raw) == 0 || string(raw) == "null" {
			return nil, "the start declares no runtime tuple: a run with no configuration is a run nothing can qualify (spec/qualification.md)"
		}
		t, err := tuple.Parse(raw)
		if err != nil {
			return nil, err.Error()
		}
		return &t, ""
	}
	if len(raw) > 0 {
		return nil, fmt.Sprintf("the start carries a runtime tuple and the chain is at %s: tuple semantics activate at %s (spec/protocol.md)", v, version.Seed2)
	}
	return nil, ""
}

// tupleDrift applies the qualification SET rule (plans/os-8e53ffd9.md
// D2, D4) to one declaration: the CLAIM HOLDER's grants for claim cite
// zero or more tuples. Zero: unqualified, the bridge, admissible. Any:
// the declared configuration must equal one member, per field, or the
// holder is invoking a configuration its grant does not cite, and the
// returned Drift names the holder, the first field that moved, the
// declared value and the cited set. The holder, never the signer: the
// supervisor signs the start, the work executes under the holder's
// window. A nil ring or a nil declaration (a seed/1 start) drifts
// nothing.
func tupleDrift(ring *keyring.State, holder string, declared *tuple.Tuple, eval bool, capability string) *Drift {
	if declared == nil || ring == nil {
		return nil
	}
	// An eval subject is where a configuration proves itself, so the
	// rule qualification feeds cannot gate the act that mints it: any
	// declared tuple runs there, a disqualified one included
	// (plans/os-03e47abb.md D6).
	if eval {
		return nil
	}
	cited := ring.GrantTuples(holder, capability)
	if len(cited) == 0 {
		// Two empty sets (plans/os-03e47abb.md D4): a holder never
		// cited is unqualified and bridges; a holder whose every
		// cited configuration was disqualified admits nothing. The
		// bridge does not reopen.
		if ring.EverCited(holder, capability) {
			return &Drift{Holder: holder}
		}
		return nil
	}
	for _, t := range cited {
		if t.Equal(*declared) {
			return nil
		}
	}
	field, have, _, _ := declared.Diff(cited[0])
	shown := make([]string, 0, len(cited))
	for _, t := range cited {
		b, _ := json.Marshal(t)
		shown = append(shown, string(b))
	}
	return &Drift{Holder: holder, Field: field, Have: have, Cited: shown}
}

// RunStartValid reports whether a folded run.started passed the
// admission boundary at its own position (review findings on the
// task PR and its follow-up: fold presence is never proof of
// admission): the record carries the strict {fence, reservation}
// shape, the signer held the run lanes there, the cited fence was
// the subject's ACTIVE claim fence there, and the cited reservation
// already existed there, passed the authoring boundary, and was
// still effectively open there — every check against the prefix the
// start actually appended onto, so a start citing a later-appended
// or already-closed reservation validates nothing, while a close
// landing after an admitted start never retroactively invalidates
// it. The run rule's one-per-fence and carries-a-start checks and
// the executor's Provision gate all share this one derivation, so a
// raw start neither blocks the legitimate supervisor, nor launders a
// settle through, nor provisions an unbudgeted workspace.
func RunStartValid(records []*event.Record, table *transition.Table, subject string, st transition.RunStartFact) bool {
	rec, ok := runFactRecord(records, st.Pos, transition.RunStartedVerb, subject)
	if !ok {
		return false
	}
	var p struct {
		Fence       string          `json:"fence"`
		Reservation string          `json:"reservation"`
		Tuple       json.RawMessage `json:"tuple,omitempty"`
	}
	if !strictRunPayload(rec, &p) {
		return false
	}
	if f, err := strconv.Atoi(strings.TrimSpace(p.Fence)); err != nil || f != st.Fence {
		return false
	}
	if r, err := strconv.Atoi(strings.TrimSpace(p.Reservation)); err != nil || r != st.Reservation {
		return false
	}
	// The declaration is re-judged under the record's own version and
	// against the holder's cited set at this record's prefix (review
	// finding on the task PR): a raw-pushed seed/2 start with no
	// tuple, a malformed one, or one the holder's grants do not cite
	// never passed the boundary, so it provisions nothing (Provision
	// would otherwise skip the resolved-tuple comparison on a nil
	// declaration), launders no settle, and blocks no legitimate
	// start.
	declared, reason := declaredTuple(rec.Event.V, p.Tuple)
	if reason != "" {
		return false
	}
	prefix := records[:st.Pos]
	ring, _, err := keyring.StateAt(prefix)
	if err != nil || ring == nil ||
		!ring.HasAnyCapability(st.Signer, keyring.AcceptedCapabilities(transition.RunStartedVerb)) {
		return false
	}
	prior, ok := table.StateAt(prefix, subject)
	if !ok || prior.Claim == nil || prior.Claim.Fence != st.Fence {
		return false
	}
	if tupleDrift(ring, prior.Claim.Holder, declared, prior.Eval != nil, keyring.CapClaim) != nil {
		return false
	}
	for _, r := range prior.Reservations {
		if r.Pos == st.Reservation {
			if !ReservationValid(prefix, table, subject, r) {
				return false
			}
			_, closed := BudgetViewAt(prefix, table, subject, prior).ClosedBy[st.Reservation]
			return !closed
		}
	}
	return false
}

// Topology is the graph derived at the context's prefix
// (plans/os-f0ae2cdf.md D3): the relation facts that passed the
// boundary at their own positions, read against the lifecycle fold.
// Every consumer (the claim rule, the queue, the poll, the report)
// derives from this one function; a prefix carrying no relation fact
// folds nothing and costs nothing.
func Topology(c *Context) *topology.Derived {
	if c == nil {
		return topology.Derive(nil, nil, nil)
	}
	return topology.Derive(topology.Fold(c.Records, c.Table), c.Lifecycle, c.Table)
}

// ErasureValid reports whether a folded artifact.erased passed the
// admission boundary at its own position (the RunStartValid posture:
// fold presence is never proof of admission): the record at the
// position is that erasure, on that subject, by that signer, naming
// that digest, and the signer held a capability the verb accepts
// against the prefix it appended onto, the keyring replayed there the
// way the seal's own authorization is. The raw seam lets any standing
// key land a well-shaped tombstone and the fold keeps it, and one that
// never passed the boundary attributes nothing: it neither cleans the
// seal audit, nor blocks the operator's own record, nor resumes a
// removal (review findings on the task PR; plans/os-db5cd353.md D3).
func ErasureValid(records []*event.Record, st transition.ErasureFact) bool {
	rec, ok := runFactRecord(records, st.Pos, erasure.Verb, st.Subject)
	if !ok || rec.Event.Actor != st.Signer || !version.Activated(rec.Event.V) {
		return false
	}
	p, err := erasure.Parse(rec.Event.Subject, rec.Event.Payload)
	if err != nil || p.Artifact != st.Artifact {
		return false
	}
	ring, _, err := keyring.StateAt(records[:st.Pos])
	return err == nil && ring != nil &&
		ring.HasAnyCapability(st.Signer, keyring.AcceptedCapabilities(erasure.Verb))
}

// Erasure finds the admitted erasure of an artifact, wherever it was
// recorded: the fold's digest-wide lookup (transition.Fold.Erasure)
// narrowed to the tombstones that passed the boundary. Every consumer
// of the fact reads it through here: the once rule, the affordance
// probe, the seal audit, the render's refusal and the verb's resume.
func Erasure(records []*event.Record, fold *transition.Fold, artifact string) (transition.ErasureFact, bool) {
	if fold == nil {
		return transition.ErasureFact{}, false
	}
	for _, e := range fold.Erasures() {
		if e.Artifact == artifact && ErasureValid(records, e) {
			return e, true
		}
	}
	return transition.ErasureFact{}, false
}

// InterruptValid reports whether a folded run.interrupted passed the
// admission boundary at its own position (plans/os-0f718b4e.md; the
// RunStartValid posture: fold presence is never proof of admission):
// the record carries the strict {fence} shape, the signer held an
// accepted lane there, and the cited fence was the subject's ACTIVE
// claim fence there, judged against the prefix the fact appended
// onto — so a shape a strict admission would refuse parks no one and
// blocks nothing.
func InterruptValid(records []*event.Record, table *transition.Table, subject string, it transition.InterruptFact) bool {
	rec, ok := runFactRecord(records, it.Pos, transition.RunInterruptedVerb, subject)
	if !ok {
		return false
	}
	var p struct {
		Fence string `json:"fence"`
	}
	if !strictRunPayload(rec, &p) {
		return false
	}
	if f, err := strconv.Atoi(strings.TrimSpace(p.Fence)); err != nil || f != it.Fence {
		return false
	}
	prefix := records[:it.Pos]
	ring, _, err := keyring.StateAt(prefix)
	if err != nil || ring == nil ||
		!ring.HasAnyCapability(it.Signer, keyring.AcceptedCapabilities(transition.RunInterruptedVerb)) {
		return false
	}
	prior, ok := table.StateAt(prefix, subject)
	return ok && prior.Claim != nil && prior.Claim.Fence == it.Fence
}

// RunSettleValid reports whether a folded run.settled passed the
// admission boundary at its own position: the strict {fence, units,
// lines} shape with non-negative counts, the signer's lane there,
// and an applied claim fence carrying a boundary-valid start there.
// The settle rule's once-per-fence check consumes it, so a raw
// invalid settle never permanently blocks the legitimate aggregate.
func RunSettleValid(records []*event.Record, table *transition.Table, subject string, r transition.RunFact) bool {
	rec, ok := runFactRecord(records, r.Pos, transition.RunSettledVerb, subject)
	if !ok {
		return false
	}
	var p struct {
		Fence string `json:"fence"`
		Units string `json:"units"`
		Lines string `json:"lines"`
	}
	if !strictRunPayload(rec, &p) {
		return false
	}
	if f, err := strconv.Atoi(strings.TrimSpace(p.Fence)); err != nil || f != r.Fence {
		return false
	}
	if u, err := strconv.Atoi(strings.TrimSpace(p.Units)); err != nil || u < 0 || u != r.Units {
		return false
	}
	if l, err := strconv.Atoi(strings.TrimSpace(p.Lines)); err != nil || l < 0 || l != r.Lines {
		return false
	}
	prefix := records[:r.Pos]
	ring, _, err := keyring.StateAt(prefix)
	if err != nil || ring == nil ||
		!ring.HasAnyCapability(r.Signer, keyring.AcceptedCapabilities(transition.RunSettledVerb)) {
		return false
	}
	prior, ok := table.StateAt(prefix, subject)
	if !ok || !prior.ClaimFences[r.Fence] {
		return false
	}
	for _, st := range prior.RunStarts {
		if st.Fence == r.Fence && RunStartValid(prefix, table, subject, st) {
			return true
		}
	}
	return false
}

// InterruptRequested reports whether a boundary-valid interrupt
// stands for the fence: the one derivation conforming workers poll
// at their safe points and the drills share, so a raw unprivileged
// interrupt parks no one (the denial-of-service shape,
// plans/os-0f718b4e.md D3).
func InterruptRequested(records []*event.Record, table *transition.Table, subject string, s transition.SubjectState, fence int) bool {
	for _, it := range s.Interrupts {
		if it.Fence == fence && InterruptValid(records, table, subject, it) {
			return true
		}
	}
	return false
}

// WedgeDeclared reports whether an admitted wedge.declared stands on
// this subject's active claim window (plans/os-8a5f14bb.md D4).
//
// It is the second half of the maintenance reap's corroboration, and
// it needs its own derivation for a reason worth stating: unlike
// run.interrupted, wedge.declared is a FREE verb. It has no
// transition-table row and folds to no fact, so there is nothing on
// SubjectState to consume and the records are the only place the
// declaration exists. Deriving it here rather than in the maintenance
// loop keeps "did this pass the boundary at its own position" in the
// one package that answers that question for every other fact.
//
// The bar is the InterruptValid posture: the record carries the
// strict wedge shape, the signer held an accepted lane at that
// position, and the subject's claim there was the fence in hand. A
// raw unprivileged declaration corroborates nothing, which is what
// stops silence plus a forged wedge from reaping live work.
func WedgeDeclared(records []*event.Record, table *transition.Table, subject string, fence int) bool {
	for pos, rec := range records {
		if rec.Event.Verb != transition.WedgeDeclaredVerb || rec.Event.Subject != subject {
			continue
		}
		if err := transition.CheckWedgeShape(subject, rec.Event.Payload); err != nil {
			continue
		}
		prefix := records[:pos]
		ring, _, err := keyring.StateAt(prefix)
		if err != nil || ring == nil ||
			!ring.HasAnyCapability(rec.Event.Actor, keyring.AcceptedCapabilities(transition.WedgeDeclaredVerb)) {
			continue
		}
		prior, ok := table.StateAt(prefix, subject)
		if !ok || prior.Claim == nil || prior.Claim.Fence != fence {
			continue
		}
		// The CITATION, judged by the fence rule's own terms (review
		// finding on #205). Checking only that the claim at this
		// position carried the fence is not enough: a wedge naming a
		// STALE fence is refused at admission and would still have
		// corroborated here, so a boundary-refused declaration could
		// have reaped a live claim — the precise hole this whole
		// derivation exists to close.
		//
		// The two conditions mirror the rule rather than tightening
		// it: any citation present must match the active fence, and a
		// holder or prior claimant must cite one at all. Being
		// stricter than admission would refuse to reap on evidence the
		// boundary accepts, which is a different bug.
		if cited, hasCited := fenceCitation(rec.Event.Payload); hasCited {
			if cited != strconv.Itoa(fence) {
				continue
			}
		} else if rec.Event.Actor == prior.Claim.Holder || prior.PriorClaimants[rec.Event.Actor] {
			continue
		}
		return true
	}
	return false
}

func fenceCitation(payload []byte) (string, bool) {
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
		// A non-string fence is a citation that matches nothing.
		return strings.TrimSpace(string(raw)), true
	}
	return s, true
}

// checkEscalation is the escalation channel's whole rule
// (plans/os-f781f0da.md). It runs after the fence and before the
// transition, the packet rule's position: a malformed question refuses
// before the state moves.
func checkEscalation(c *Context, rec *event.Record) error {
	subject := rec.Event.Subject
	verb := rec.Event.Verb
	var standing *transition.EscalationFact
	if s, ok := c.Lifecycle.State(subject); ok {
		standing = s.Escalation
	}

	// A question is shape-checked wherever it may ride. On the raise it
	// is required; on a park it is optional, because most parks ask
	// nothing.
	if escalation.CarriesQuestion(verb) {
		q, present, err := escalation.FromPayload(subject, rec.Event.Payload)
		if err != nil {
			return err
		}
		if !present && verb == escalation.RaiseVerb {
			return &EscalationError{Subject: subject, Reason: `no "escalation" — the raise IS the question, and a raise that asks nothing blocks the contract for no one to answer`}
		}
		if present && standing != nil {
			return &EscalationError{Subject: subject, Reason: fmt.Sprintf(
				"a question already stands at position %d and nothing else moves until it is answered — %s answers it",
				standing.Pos, escalation.AnswerVerb)}
		}
		_ = q
	}

	switch verb {
	case escalation.AnswerVerb:
		if standing == nil {
			return &EscalationError{Subject: subject, Reason: fmt.Sprintf(
				"no question stands — %s answers one raised by %s or by a claim.parked that carried one",
				escalation.AnswerVerb, escalation.RaiseVerb)}
		}
		var ans struct {
			Escalation string `json:"escalation"`
			Choice     string `json:"choice"`
			Because    string `json:"because"`
		}
		if err := strictJSON(rec.Event.Payload, &ans); err != nil {
			return &EscalationError{Subject: subject, Reason: fmt.Sprintf("strict shape: %v", err)}
		}
		if ans.Escalation != fmt.Sprintf("%d", standing.Pos) {
			return &EscalationError{Subject: subject, Reason: fmt.Sprintf(
				"cites escalation %q, but the standing question is at position %d — an answer to a question nobody asked is not an answer",
				ans.Escalation, standing.Pos)}
		}
		q := &escalation.Escalation{Question: standing.Question, Options: standing.Options}
		if !q.Offers(ans.Choice) {
			return &EscalationError{Subject: subject, Reason: fmt.Sprintf(
				"choice %q is not offered — the question at %d offers %s, and answering outside the set is a new decision, not this one",
				ans.Choice, standing.Pos, strings.Join(q.IDs(), ", "))}
		}
	case "contract.unblocked":
		// The lockout. blocked has exactly two machine-visible exits;
		// this closes the one a machine holds, so "nothing else about
		// the contract moves until it is answered" is structural.
		if standing != nil {
			return &EscalationError{Subject: subject, Reason: fmt.Sprintf(
				"a question stands at position %d and only its answer reopens this contract — %s with the chosen option, or contract.cancelled citing it",
				standing.Pos, escalation.AnswerVerb)}
		}
	case "contract.cancelled":
		// Cancelling stays legal, because refusing it would trap the
		// contract with no operator path out, which is a worse failure
		// than the one prevented. But it must cite the question it
		// answers, or the standing obligation would simply vanish and
		// take the audit link with it.
		if standing != nil {
			var cite struct {
				Escalation string `json:"escalation"`
			}
			_ = json.Unmarshal(rec.Event.Payload, &cite)
			if cite.Escalation != fmt.Sprintf("%d", standing.Pos) {
				return &EscalationError{Subject: subject, Reason: fmt.Sprintf(
					`a question stands at position %d: cancelling answers it and must say so, carrying {"escalation": "%d"} — otherwise the question disappears with no record of what closed it`,
					standing.Pos, standing.Pos)}
			}
		}
	}
	return nil
}

// strictJSON decodes with unknown fields refused, the wire-parsing
// precedent this tree applies to every payload it shapes.
func strictJSON(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// checkEvalMarker is the marker's shape at filing (plans/os-96850e5a.md
// D5): {name, tuple?, lesson?, carrier?}, lesson and carrier both
// present or both absent, the lesson a hypothesis citation and the
// carrier an anchored path. A bound marker names the hypothesis and
// the exact candidate revision the eval is for, on the record at
// filing.
func checkEvalMarker(active, subject string, raw json.RawMessage) error {
	var m struct {
		Name    string          `json:"name"`
		Tuple   json.RawMessage `json:"tuple"`
		Lesson  string          `json:"lesson"`
		Carrier string          `json:"carrier"`
		Kind    string          `json:"kind"`
	}
	if err := strictJSON(raw, &m); err != nil {
		return &transition.ChainError{Subject: subject, Verb: "intent.filed",
			Reason: fmt.Sprintf("the eval marker is the strict object {name, tuple?, lesson?, carrier?, kind?}: %v", err)}
	}
	// The kind is a seed/4 field (plans/os-2e34f66a.md D5, D6): it
	// marks a calibration, the one kind whose verdict qualifies a
	// verifier, and a seed/3 validator's marker has no such field.
	if m.Kind != "" {
		if !version.LevelsApply(active) {
			return &transition.ChainError{Subject: subject, Verb: "intent.filed",
				Reason: fmt.Sprintf("the eval marker names a kind and the chain is at %s: calibration activates at %s (spec/evals.md)", active, version.Seed4)}
		}
		if m.Kind != transition.EvalKindCalibration {
			return &transition.ChainError{Subject: subject, Verb: "intent.filed",
				Reason: fmt.Sprintf("the eval marker's kind %q is neither absent nor %q", m.Kind, transition.EvalKindCalibration)}
		}
	}
	if (m.Lesson == "") != (m.Carrier == "") {
		return &transition.ChainError{Subject: subject, Verb: "intent.filed",
			Reason: "a bound eval marker names both the lesson (\"<h-id>@<position>\") and the carrier (\"<path> @ <commit>\"), or neither"}
	}
	if m.Lesson != "" {
		if cit, ok := curation.ParseCitation(m.Lesson); !ok || !curation.IsHypothesisSubject(cit.Contract) {
			return &transition.ChainError{Subject: subject, Verb: "intent.filed",
				Reason: fmt.Sprintf("the eval marker's lesson %q is not \"<h-id>@<position>\"", m.Lesson)}
		}
		if _, _, ok := curation.AnchorParts(m.Carrier); !ok {
			return &transition.ChainError{Subject: subject, Verb: "intent.filed",
				Reason: fmt.Sprintf("the eval marker's carrier %q is not \"<path> @ <commit>\"", m.Carrier)}
		}
	}
	return nil
}

// latestVerdictFor is the newest verdict of the window on one
// submission: the one the merge chain may cite. With one submission it
// is the subject's latest verdict; on a racing subject each racer's
// submission carries its own.
func latestVerdictFor(s transition.SubjectState, submission int) (transition.VerdictFact, bool) {
	var out transition.VerdictFact
	found := false
	for _, v := range s.Verdicts {
		if v.Submission == submission {
			out, found = v, true
		}
	}
	if !found && s.Verdict != nil && s.Verdict.Submission == submission {
		return *s.Verdict, true
	}
	return out, found
}
