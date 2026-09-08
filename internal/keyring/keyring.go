// Package keyring is the actor standing projection over a chain prefix
// (docs/build-plan.md Phase 3 item 1; plans/os-52a2d688.md;
// SEED-NEXT.md Part II "Enrollment" and "Capabilities"). Enrollment,
// grants, suspension, and revocation are ledger events; the keyring every
// verifier and admission point consults is a pure projection of them —
// seeded from the genesis governance root and advanced by one transition
// function (Advance) shared by verification replay and admission preview,
// never stored anywhere. The semantics activate at protocol version
// seed/1 per the bump discipline in spec/protocol.md: actor events
// at seed/0 positions are grandfathered as inert, so chains that verified
// before Phase 3 still verify.
package keyring

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// The actor lifecycle verbs from the charter's catalog
// (spec/protocol.md; payload schemas in spec/actors.md).
const (
	VerbEnrolled  = "actor.enrolled"
	VerbGranted   = "actor.granted"
	VerbSuspended = "actor.suspended"
	VerbRevoked   = "actor.revoked"
	// The qualification verbs (plans/os-03e47abb.md D2, D4;
	// spec/evals.md), defined at seed/3: a qualification is a
	// grant with evidence, minted from an eval's pass verdict for the
	// tuple the run declared; a disqualification suspends that grant,
	// citing the fail that ended it.
	VerbQualified    = "actor.qualified"
	VerbDisqualified = "actor.disqualified"
)

// These mirror ledger.UpgradeVerb, genesis.Verb, halt.DeclareVerb, and
// halt.LiftVerb; keyring cannot import those packages (ledger imports
// keyring), so the parity is pinned by tests.
const (
	upgradeVerb      = "system.protocol.upgraded"
	genesisVerb      = "system.genesis"
	haltDeclaredVerb = "system.halt.declared"
	haltLiftedVerb   = "system.halt.lifted"
	checkpointVerb   = "system.checkpoint"
	importedVerb     = "system.imported"
)

// The capability vocabulary (plans/os-3979d48b.md; SEED-NEXT.md Part II
// "Capabilities"): grants are events, checked at admission on every
// verb.
const (
	CapOperator    = "operator"
	CapMaintenance = "maintenance"
	// CapDispatch is queue management: filing, specifying, blocking,
	// unblocking, and reaping contracts (plans/os-d69a6c91.md).
	CapDispatch = "dispatch"
	// CapObserver is the observer lane (plans/os-6cdc15be.md): a
	// governed observer records forge fact (merge.observed) behind
	// the full chain rule; the charter names merge.observed an
	// observation by a governed observer, and Phase 6 adds the lane.
	CapObserver = "observer"
	// CapVerdict is the verifier lane: rendering verdicts
	// (plans/os-f6d2c267.md). Deliberately the one row without the
	// operator fallback: III.G names operator override its own
	// attributable verb, never a disguised verdict — that verb lands
	// with 6.4, and a governance root that judges holds an explicit
	// verdict grant, with L1 independence applying to every signer.
	CapVerdict = "verdict"
	// CapClaim is the worker set: taking, releasing, and parking
	// claims, and submitting work.
	CapClaim = "claim"
	// CapSealer authors sealed checks (plans/os-3128535a.md). Like the
	// verdict lane it has no operator fallback: operator already
	// stands in the claim and submission lanes, so an operator row
	// here would put authoring and implementation authority on one
	// capability and the charter's capability audit could prove
	// nothing. Grant-level disjointness with claim and operator is
	// enforced at actor.granted admission.
	CapSealer = "sealer"
	// CapSupervise is the supervisor lane (plans/os-c61c3392.md;
	// SEED-NEXT.md §II.9): publishing eligibility-scoped offers. No
	// disjointness constraints attach — an offer grants nothing, the
	// claim it invites settles at admission like any claim — so the
	// row keeps the standard operator fallback.
	CapSupervise = "supervise"
	// CapCurate is the curator lane's proposal grant
	// (plans/os-f30ee0d3.md; SEED-NEXT.md §II.12): proposing
	// hypotheses from the observations workers appended. The fifth
	// no-fallback row: operator already reaches claim.taken and the
	// deliberate exits, so an operator fallback here would let one key
	// write a trajectory's observations and then conclude from them.
	// Disjoint from claim and operator at the grant, both directions,
	// so a worker promoting its own runs is refused at the grant.
	CapCurate = "curate"
)

// Capabilities is the whole vocabulary, in declaration order: the one
// place a consumer asks "is this a real capability" rather than
// writing the list down again (plans/os-cf1c9688.md D3). Pinned by
// test against the constants above.
func Capabilities() []string {
	return []string{
		CapOperator, CapMaintenance, CapDispatch, CapObserver,
		CapVerdict, CapClaim, CapSealer, CapSupervise, CapCurate,
	}
}

// Kinds is the roster-kind vocabulary an enrollment asserts
// (SEED-NEXT.md Part II "Enrollment"): the distinction the agent-only
// guardrails read. One place, so the enrollment shape, the ceiling and
// the approvals lint cannot disagree on what a kind is.
func Kinds() []string { return []string{"human", "agent", "service"} }

// KnownKind reports whether the name is a roster kind.
func KnownKind(kind string) bool {
	for _, k := range Kinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// Known reports whether the name is a capability in the vocabulary.
func Known(capability string) bool {
	for _, c := range Capabilities() {
		if c == capability {
			return true
		}
	}
	return false
}

// AcceptedCapabilities returns the set of capabilities any one of which
// admits the verb, mirroring the normative table in
// spec/actors.md "Capabilities" (pinned by test). A nil result
// means the verb needs active standing only. The table is data:
// later phases append rows (claim rights by squad and tier, verdict
// rights, curation-proposal rights) when their verbs land.
func AcceptedCapabilities(verb string) []string {
	// The qualification verbs are the first actor.* rows that are not
	// operator-only (plans/os-03e47abb.md D7): the charter's supervisor
	// mints and suspends, attributably, with no operator ceremony, and
	// operator stays the standing human override.
	if verb == VerbQualified || verb == VerbDisqualified {
		return []string{CapSupervise, CapOperator}
	}
	if IsActorVerb(verb) {
		return []string{CapOperator}
	}
	switch verb {
	case haltDeclaredVerb, haltLiftedVerb, upgradeVerb, importedVerb:
		// The import's provenance record (plans/os-cf13fb51.md D2) is
		// the importing operator's, once, before the replayed history.
		return []string{CapOperator}
	case checkpointVerb:
		// The charter names checkpoints as signed by the maintenance
		// actor or an operator; folding maintenance into operator would
		// hand the Phase 9 loop halt and actor-management authority it
		// must not hold (review finding on #101).
		return []string{CapMaintenance, CapOperator}
	// The contract-lifecycle rows (plans/os-d69a6c91.md, review
	// finding on #113: a verb without a row needs active standing
	// only, which would let any enrolled actor cancel or specify
	// anything). Dispatch manages the queue; claim is the worker set;
	// cancellation and the done observation stay operator-gated in v0
	// (Phase 6 adds the observer lane for merge.observed).
	case "intent.filed", "contract.specified", "contract.blocked",
		"contract.unblocked", "claim.reaped":
		return []string{CapDispatch, CapOperator}
	// The relation facts (plans/os-f0ae2cdf.md D2; spec/topology.md):
	// dependency links, the parent and the mission anchor are queue
	// shaping, the dispatcher's, with the standard operator fallback.
	case "dependency.linked", "dependency.unlinked", "hierarchy.parented", "goal.aligned":
		return []string{CapDispatch, CapOperator}
	case "claim.taken", "claim.released", "claim.parked", "submission.made":
		return []string{CapClaim, CapOperator}
	case "contract.cancelled":
		return []string{CapOperator}
	// The escalation channel (plans/os-f781f0da.md). Raising is broad
	// because the charter says ANY lane can raise blocked(needs-you),
	// and it is safe to be broad because raising a question GRANTS
	// nothing: the offer.published argument. A raised contract leaves
	// blocked only through the operator's decision.recorded or a
	// citing cancellation, so a raiser can stop work and hand a human
	// the decision, never move it. Answering is operator and nothing
	// else, the FOURTH no-fallback row: the charter names the act
	// attributable human judgement (§I.3, humans hold gates), and a
	// dispatch fallback would let a machine lane answer a human gate.
	case "escalation.raised":
		return []string{CapClaim, CapDispatch, CapVerdict, CapSupervise, CapOperator, CapCurate}
	case "decision.recorded":
		return []string{CapOperator}
	// The reconciliation chain (plans/os-6cdc15be.md): asking for the
	// merge is the work lane's act; observing the forge fact is the
	// observer lane's.
	case "merge.requested":
		return []string{CapClaim, CapOperator}
	case "merge.observed":
		return []string{CapObserver, CapOperator}
	// The forge observation (plans/os-0cd18799.md D1): what the forge's
	// checks and review threads say about the head under review is the
	// observer's fact, the merge.observed row.
	case "check.observed":
		return []string{CapObserver, CapOperator}
	// The sealed-checks commitment (plans/os-3128535a.md): sealer
	// only, no operator fallback, mirroring the verdict lane's
	// posture — authoring isolation is the row's whole point.
	case "check.sealed":
		return []string{CapSealer}
	// The red-verdict companions (plans/os-d2497eb7.md): returning a
	// fail-verdicted contract to the queue is queue management; the
	// override is the third no-fallback row — its own attributable
	// verb, operator judgment and nothing else.
	case "contract.returned":
		return []string{CapDispatch, CapOperator}
	case "merge.overridden":
		return []string{CapOperator}
	case "request.answered":
		// The dispatcher's close of an inbound proposal
		// (plans/os-48df10a2.md D1); request.filed itself is standing-only,
		// like message.sent, and appears in no case.
		return []string{CapDispatch, CapOperator}
	case "approval.granted", "approval.denied", "artifact.erased":
		// The per-verb approval's answers (plans/os-5781a026.md D2):
		// operator only, the decision.recorded posture, because an
		// approval is a gate a human holds and a machine-lane fallback
		// would let the governed lane answer for itself.
		// approval.requested is standing-only, like request.filed,
		// since asking grants nothing, and appears in no case.
		// The erasure fact (plans/os-db5cd353.md D3): an erasure
		// obligation is a governance act a human answers for, the
		// decision.recorded posture, and no lane's loop erases.
		return []string{CapOperator}
	// The supervisor lane (plans/os-c61c3392.md): offers invite
	// claims and grant nothing, so the standard operator fallback
	// stands.
	case "offer.published":
		return []string{CapSupervise, CapOperator}
	// The budget-reservation facts (plans/os-cecac5de.md): the claim
	// lane reserves and settles inside its window; the budget rule
	// further pins reserves to the ACTIVE holder and closes to the
	// reservation's owner, which capability rows alone cannot say.
	case "budget.reserve", "budget.settle", "budget.release":
		return []string{CapClaim, CapOperator}
	// The execution-run facts (plans/os-1dad487d.md; the safe-point
	// interrupt per plans/os-0f718b4e.md): adapter-side initiation,
	// summarization, and preemption are the supervisor lane's acts,
	// like offers.
	case "run.started", "run.settled", "run.interrupted":
		return []string{CapSupervise, CapOperator}
	// The plan verbs (plans/os-16c1d142.md): the claim holder plans
	// (the fence matrix applies on a claimed subject); approval is an
	// external-fact observation, operator-attested in v0 like
	// merge.observed.
	case "plan.proposed":
		return []string{CapClaim, CapOperator}
	case "plan.approved":
		return []string{CapOperator}
	// The observation summarization verbs (plans/os-2ff8dbf1.md): a
	// milestone is the claim lane's coarse fact (the fence matrix
	// applies on the claimed subject); declaring a wedge is operator
	// judgment in v0, the merge.observed posture.
	case "progress.milestone":
		return []string{CapClaim, CapOperator}
	case "wedge.declared":
		return []string{CapOperator}
	// The verdict lane (plans/os-f6d2c267.md): verdict-granted keys
	// only, no operator fallback — the one such row, see CapVerdict.
	case "verdict.rendered":
		return []string{CapVerdict}
	// The human-verdict deferral (plans/os-2e34f66a.md D4): the
	// verifier's own act, the same no-fallback row, since a deferral
	// names what the verifier could not judge.
	case "verdict.deferred":
		return []string{CapVerdict}
	// The staged curation stores (plans/os-f30ee0d3.md): a dead end is
	// the window holder's candidate observation (the fence matrix
	// applies); the proposal is the curator's alone, the fifth
	// no-fallback row; the promotion is the observation of a lesson
	// PR's merge, the merge.observed posture.
	case "curation.deadend.recorded":
		return []string{CapClaim, CapOperator}
	case "curation.hypothesis.proposed", "curation.hypothesis.contested":
		return []string{CapCurate}
	case "curation.lesson.promoted":
		return []string{CapObserver, CapOperator}
	// The flywheel (plans/os-9075c308.md D4): the workflow proposal is
	// the curator's alone, the proposal posture; the merge is the
	// observation of the workflow PR's landing, the merge.observed
	// row.
	case "workflow.proposed":
		return []string{CapCurate}
	case "workflow.merged":
		return []string{CapObserver, CapOperator}
	// Expiry, retirement and rollback (plans/os-0d537fbd.md D2, D3): a
	// lesson's retirement is the promotion's own row (the observation
	// of a revoked conclusion), a dead end's retirement and
	// un-retirement the curator's alone, judging applicability.
	case "curation.lesson.retired":
		return []string{CapObserver, CapOperator}
	case "curation.deadend.retired", "curation.deadend.unretired":
		return []string{CapCurate}
	}
	return nil
}

// Applies reports whether the keyring semantics are active under the
// given protocol version: seed/1 introduced them (spec/actors.md),
// records at earlier positions are grandfathered as inert, and every
// later version keeps them (seed/2 adds tuple semantics on top,
// tuple.Applies). Named versions rather than an ordering: an unknown
// "seed/10" is not a version this build implements, and a keyring that
// guessed it had actor semantics would be judging a chain it cannot
// verify.
func Applies(active string) bool { return version.Activated(active) }

// GrantTuples returns every runtime tuple the actor's grants and
// qualifications for the capability cite and no disqualification has
// since removed, in application order: the ADMISSIBLE set the
// qualification rule reads (plans/os-8e53ffd9.md D2;
// plans/os-03e47abb.md D4). Empty means either never qualified or
// wholly disqualified; EverCited tells the two apart.
func (s *State) GrantTuples(actor, capability string) []tuple.Tuple {
	e, ok := s.entries[actor]
	if !ok || e.Tuples == nil {
		return nil
	}
	return append([]tuple.Tuple(nil), e.Tuples[capability]...)
}

// EverCited reports whether any grant or qualification ever cited a
// tuple for the actor's capability. An actor for which it is true and
// whose admissible set is empty has had every configuration
// disqualified, and admits nothing: the bridge is for the never
// qualified, and does not reopen (plans/os-03e47abb.md D4).
func (s *State) EverCited(actor, capability string) bool {
	e, ok := s.entries[actor]
	return ok && e.everCited[capability]
}

// Qualifications returns the actor's applied qualification events, in
// application order, for the derivation that schedules re-tests.
func (s *State) Qualifications(actor string) []Qualification {
	e, ok := s.entries[actor]
	if !ok {
		return nil
	}
	return append([]Qualification(nil), e.Qualifications...)
}

// Actors returns every enrolled fingerprint, sorted, for derivations
// that walk the keyring rather than one entry.
func (s *State) Actors() []string {
	out := make([]string, 0, len(s.entries))
	for fp := range s.entries {
		out = append(out, fp)
	}
	sort.Strings(out)
	return out
}

func (e *Entry) citeTuple(capability string, t tuple.Tuple) {
	if e.Tuples == nil {
		e.Tuples = map[string][]tuple.Tuple{}
	}
	for _, have := range e.Tuples[capability] {
		if have.Equal(t) {
			return
		}
	}
	e.Tuples[capability] = append(e.Tuples[capability], t)
}

func (e *Entry) markCited(capability string) {
	if e.everCited == nil {
		e.everCited = map[string]bool{}
	}
	e.everCited[capability] = true
}

func cloneCited(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTuples(in map[string][]tuple.Tuple) map[string][]tuple.Tuple {
	if in == nil {
		return nil
	}
	out := make(map[string][]tuple.Tuple, len(in))
	for k, v := range in {
		out[k] = append([]tuple.Tuple(nil), v...)
	}
	return out
}

// IsActorVerb reports whether the verb is in the actor.* namespace.
func IsActorVerb(verb string) bool { return strings.HasPrefix(verb, "actor.") }

// Standing is an actor's current standing in the projection.
type Standing string

const (
	StandingActive    Standing = "active"
	StandingSuspended Standing = "suspended"
	StandingRevoked   Standing = "revoked"
)

// Entry is one actor's projected state. Kind is the enrolling operator's
// assertion, never a cryptographic fact (SEED-NEXT.md Part II
// "Enrollment"); Grants accumulate as capability data for the Phase 3.2
// admission checks.
type Entry struct {
	Key      ed25519.PublicKey
	Kind     string
	Name     string
	Standing Standing
	Root     bool
	Grants   []string
	// Tuples holds, per capability, the runtime tuples this actor's
	// grants cite (plans/os-8e53ffd9.md D2). It sits beside Grants
	// rather than replacing it so every reader of the string view is
	// untouched: a grant with no tuple contributes to Grants only, a
	// grant with one to both. An actor with NO cited tuple for a
	// capability is unqualified for it, and admits as before seed/2;
	// one with any is qualified, and a run must match one of them.
	// From seed/3 this is the ADMISSIBLE set: a disqualification
	// removes its tuple here and the entry remembers it was cited.
	Tuples map[string][]tuple.Tuple
	// Qualifications is every actor.qualified and actor.disqualified
	// applied to this actor, in application order, for the derivation
	// that schedules re-tests (plans/os-03e47abb.md D5): the latest
	// per (capability, tuple) says whether the tuple is admissible and
	// the attested instant it was last proven.
	Qualifications []Qualification
	// everCited records, per capability, that some grant or
	// qualification has ever cited a tuple, so an actor whose whole
	// admissible set was disqualified is told apart from one never
	// qualified: the bridge does not reopen (D4).
	everCited map[string]bool
}

// Qualification is one applied actor.qualified or actor.disqualified:
// the capability and tuple it cites, the eval contract and the verdict
// position it acted on, the record's own attested TS (the time anchor
// spot-checks age from), and whether it disqualified.
type Qualification struct {
	Capability   string
	Tuple        tuple.Tuple
	Contract     string
	Verdict      int
	TS           string
	Disqualified bool
	Reason       string
}

// State is the keyring at one chain position.
type State struct {
	entries map[string]*Entry
	seeded  bool
}

// New returns an empty, unseeded keyring.
func New() *State { return &State{entries: map[string]*Entry{}} }

// Seeded reports whether a governance root has been loaded. An unseeded
// keyring refuses every actor event: standing has no anchor without one.
func (s *State) Seeded() bool { return s.seeded }

// SeedGenesis loads the governance root out of a system.genesis record's
// payload (the same schema internal/genesis owns; entries are taken
// verbatim, since genesis.Bootstrap already enforces the
// fingerprint-to-key binding on every CLI path). It never fails a chain:
// an unparseable payload just leaves the keyring unseeded, and the
// refusals then surface where standing is actually consulted.
func (s *State) SeedGenesis(rec *event.Record) {
	var p struct {
		GovernanceRoot []struct {
			Fingerprint string `json:"fingerprint"`
			PublicKey   string `json:"public_key"`
		} `json:"governance_root"`
	}
	if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
		return
	}
	for _, rk := range p.GovernanceRoot {
		raw, err := hex.DecodeString(rk.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		s.entries[rk.Fingerprint] = &Entry{Key: raw, Standing: StandingActive, Root: true}
		s.seeded = true
	}
}

// Resolve maps a fingerprint to its public key iff the actor's standing
// is active: the standing-aware half of signature resolution.
func (s *State) Resolve(fp string) (ed25519.PublicKey, bool) {
	e := s.entries[fp]
	if e == nil || e.Standing != StandingActive {
		return nil, false
	}
	return e.Key, true
}

// Resolver adapts Resolve to the ledger.Resolver shape.
func (s *State) Resolver() func(string) (ed25519.PublicKey, bool) {
	return s.Resolve
}

// Get returns a copy of an actor's entry.
func (s *State) Get(fp string) (Entry, bool) {
	e := s.entries[fp]
	if e == nil {
		return Entry{}, false
	}
	cp := *e
	cp.Grants = append([]string(nil), e.Grants...)
	cp.Tuples = cloneTuples(e.Tuples)
	cp.Qualifications = append([]Qualification(nil), e.Qualifications...)
	cp.everCited = cloneCited(e.everCited)
	return cp, true
}

// IsActiveRoot reports whether the fingerprint is a governance root in
// active standing.
func (s *State) IsActiveRoot(fp string) bool {
	e := s.entries[fp]
	return e != nil && e.Root && e.Standing == StandingActive
}

// sealerDisjoint refuses a grant that would co-hold sealed-check
// authoring and implementation authority on one key: sealer cannot
// join claim or operator (a governance root's implicit operator
// standing included), and neither can join sealer.
func sealerDisjoint(cur *Entry, granting string) error {
	implLane := map[string]bool{CapClaim: true, CapOperator: true}
	has := func(c string) bool {
		if c == CapOperator && cur.Root {
			return true
		}
		for _, g := range cur.Grants {
			if g == c {
				return true
			}
		}
		return false
	}
	if granting == CapSealer && (has(CapClaim) || has(CapOperator)) {
		return errors.New("sealer cannot be granted to a key holding claim or operator — sealed checks are authored under a grant disjoint from implementation grants (plans/os-3128535a.md)")
	}
	if implLane[granting] && has(CapSealer) {
		return fmt.Errorf("%s cannot be granted to a key holding sealer — sealed checks are authored under a grant disjoint from implementation grants (plans/os-3128535a.md)", granting)
	}
	// Curation-proposal isolation (plans/os-f30ee0d3.md D2): the
	// sealer rule one capability over, against both lanes it names. A
	// worker promoting its own runs, and a root concluding from its
	// own, are refused at the grant, not at the proposal.
	if granting == CapCurate && (has(CapClaim) || has(CapOperator)) {
		return errors.New("curate cannot be granted to a key holding claim or operator — hypotheses are proposed under a grant disjoint from the lanes that write observations (plans/os-f30ee0d3.md)")
	}
	if implLane[granting] && has(CapCurate) {
		return fmt.Errorf("%s cannot be granted to a key holding curate — hypotheses are proposed under a grant disjoint from the lanes that write observations (plans/os-f30ee0d3.md)", granting)
	}
	return nil
}

// Granted returns the active entries holding the capability, sorted by
// fingerprint: the sealed-check recipient set is "the current verifier
// keyring", and rotation re-derives it from here.
func (s *State) Granted(capability string) []Entry {
	var out []Entry
	fps := make([]string, 0, len(s.entries))
	for fp := range s.entries {
		fps = append(fps, fp)
	}
	sort.Strings(fps)
	for _, fp := range fps {
		e := s.entries[fp]
		if e.Standing != StandingActive {
			continue
		}
		for _, g := range e.Grants {
			if g == capability {
				out = append(out, *e)
				break
			}
		}
	}
	return out
}

// HasAnyCapability reports whether the actor holds any of the listed
// capabilities: governance roots hold operator implicitly (the genesis
// trust anchor a deployment's first grants must come from), enrolled
// actors hold exactly what actor.granted accumulated, and only active
// standing counts — a suspended or revoked actor holds nothing.
func (s *State) HasAnyCapability(fp string, capabilities []string) bool {
	e := s.entries[fp]
	if e == nil || e.Standing != StandingActive {
		return false
	}
	for _, want := range capabilities {
		if want == CapOperator && e.Root {
			return true
		}
		for _, g := range e.Grants {
			if g == want {
				return true
			}
		}
	}
	return false
}

func (s *State) activeRoots() int {
	n := 0
	for _, e := range s.entries {
		if e.Root && e.Standing == StandingActive {
			n++
		}
	}
	return n
}

// Clone deep-copies the state.
func (s *State) Clone() *State {
	c := New()
	c.seeded = s.seeded
	for fp, e := range s.entries {
		cp := *e
		cp.Grants = append([]string(nil), e.Grants...)
		cp.Tuples = cloneTuples(e.Tuples)
		cp.Qualifications = append([]Qualification(nil), e.Qualifications...)
		cp.everCited = cloneCited(e.everCited)
		c.entries[fp] = &cp
	}
	return c
}

// Preview applies one record to a clone: admission's dry run of the
// shared transition function, leaving the real state untouched.
func (s *State) Preview(rec *event.Record) error {
	return s.Clone().Advance(rec)
}

func strict(payload json.RawMessage, into any) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// Advance applies one record's actor-event effect: the one transition
// function, owning payload shapes, standing legality, and effects, that
// both verification replay and admission consume (the shared-rule-set
// requirement). Non-actor verbs no-op. Callers gate it on Applies: at
// seed/0 positions actor events are grandfathered and Advance is not
// called.
func (s *State) Advance(rec *event.Record) error {
	e := &rec.Event
	if !IsActorVerb(e.Verb) {
		return nil
	}
	if !s.seeded {
		return errors.New("keyring has no governance root: the chain's genesis names none")
	}
	switch e.Verb {
	case VerbEnrolled:
		var p struct {
			Key  string `json:"key"`
			Kind string `json:"kind"`
			Name string `json:"name"`
		}
		if err := strict(e.Payload, &p); err != nil {
			return fmt.Errorf("%s payload: %v", e.Verb, err)
		}
		raw, err := hex.DecodeString(p.Key)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return fmt.Errorf("%s key must be the raw 32-byte ed25519 public key in hex", e.Verb)
		}
		if !KnownKind(p.Kind) {
			return fmt.Errorf("%s kind %q is not one of human, agent, service", e.Verb, p.Kind)
		}
		if p.Name == "" {
			return fmt.Errorf("%s requires a display name", e.Verb)
		}
		fp, err := event.Fingerprint(ed25519.PublicKey(raw))
		if err != nil {
			return err
		}
		if e.Subject != fp {
			return fmt.Errorf("%s subject %q does not match the enrolled key's fingerprint %s", e.Verb, e.Subject, fp)
		}
		switch cur := s.entries[fp]; {
		case cur == nil:
			s.entries[fp] = &Entry{Key: raw, Kind: p.Kind, Name: p.Name, Standing: StandingActive}
		case cur.Standing == StandingRevoked:
			return fmt.Errorf("actor %s is revoked; revocation is terminal", fp)
		case cur.Standing == StandingSuspended:
			// Re-enrollment reinstates a suspended actor (recorded
			// decision, plans/os-52a2d688.md).
			cur.Standing = StandingActive
			cur.Kind = p.Kind
			cur.Name = p.Name
		default:
			return fmt.Errorf("actor %s is already enrolled and active", fp)
		}
	case VerbGranted:
		// The payload shape is CHAIN VALIDITY (spec/actors.md), so
		// the tuple field exists only where seed/2 is active: a seed/1
		// record carrying one fails here exactly as a seed/1 validator
		// fails it, and the two builds agree at every position
		// (plans/os-8e53ffd9.md D8).
		var p struct {
			Capability string          `json:"capability"`
			Tuple      json.RawMessage `json:"tuple,omitempty"`
		}
		if tuple.Applies(e.V) {
			if err := strict(e.Payload, &p); err != nil {
				return fmt.Errorf("%s payload: %v", e.Verb, err)
			}
		} else {
			var legacy struct {
				Capability string `json:"capability"`
			}
			if err := strict(e.Payload, &legacy); err != nil {
				return fmt.Errorf("%s payload: %v", e.Verb, err)
			}
			p.Capability = legacy.Capability
		}
		if p.Capability == "" {
			return fmt.Errorf("%s must name a capability", e.Verb)
		}
		var cited *tuple.Tuple
		if len(p.Tuple) > 0 {
			parsed, err := tuple.Parse(p.Tuple)
			if err != nil {
				return fmt.Errorf("%s tuple: %v", e.Verb, err)
			}
			cited = &parsed
		}
		cur := s.entries[e.Subject]
		if cur == nil {
			return fmt.Errorf("%s subject %s is not enrolled", e.Verb, e.Subject)
		}
		if cur.Standing == StandingRevoked {
			return fmt.Errorf("actor %s is revoked; revocation is terminal", e.Subject)
		}
		// Sealed-check authoring isolation (plans/os-3128535a.md):
		// sealer and the implementation lanes (claim, and operator,
		// which stands in for claim) are disjoint at the grant, both
		// directions, so the capability audit has something to prove.
		if err := sealerDisjoint(cur, p.Capability); err != nil {
			return fmt.Errorf("%s to %s: %v", e.Verb, e.Subject, err)
		}
		cur.Grants = append(cur.Grants, p.Capability)
		if cited != nil {
			cur.citeTuple(p.Capability, *cited)
			cur.markCited(p.Capability)
		}
	case VerbQualified, VerbDisqualified:
		// Defined at seed/3 positions only (plans/os-03e47abb.md D8):
		// before it the verb is unknown and the chain fails here, at
		// its position, exactly as a seed/2 validator fails it.
		if !version.EvalApplies(e.V) {
			return fmt.Errorf("actor verb %q is not defined at %s: the qualification verbs activate at %s (spec/evals.md)", e.Verb, e.V, version.Seed3)
		}
		var p struct {
			Capability string          `json:"capability"`
			Tuple      json.RawMessage `json:"tuple"`
			Contract   string          `json:"contract"`
			Verdict    string          `json:"verdict"`
			Reason     string          `json:"reason,omitempty"`
		}
		if err := strict(e.Payload, &p); err != nil {
			return fmt.Errorf("%s payload: %v", e.Verb, err)
		}
		if p.Capability == "" {
			return fmt.Errorf("%s must name a capability", e.Verb)
		}
		if p.Capability == CapVerdict && !version.LevelsApply(e.V) {
			return fmt.Errorf("%s for capability verdict is not defined at %s: calibration activates at %s (spec/evals.md)", e.Verb, e.V, version.Seed4)
		}
		if p.Capability != CapClaim && p.Capability != CapVerdict {
			// An eval proves a configuration for WORK (plans/os-03e47abb.md
			// D1; review finding on the task PR): a qualification
			// grants claim and nothing else, or a supervise key could
			// mint operator standing through a green eval.
			return fmt.Errorf("%s qualifies the %s or %s capability only, got %q: an eval proves a configuration for work or for judgment, never another authority", e.Verb, CapClaim, CapVerdict, p.Capability)
		}
		if len(p.Tuple) == 0 {
			return fmt.Errorf("%s must cite the runtime tuple it qualifies", e.Verb)
		}
		cited, err := tuple.Parse(p.Tuple)
		if err != nil {
			return fmt.Errorf("%s tuple: %v", e.Verb, err)
		}
		if p.Contract == "" {
			return fmt.Errorf("%s must cite the eval contract it acted on", e.Verb)
		}
		verdictPos, err := strconv.Atoi(strings.TrimSpace(p.Verdict))
		if err != nil || verdictPos < 0 {
			return fmt.Errorf("%s verdict %q is not a chain position", e.Verb, p.Verdict)
		}
		if e.Verb == VerbDisqualified && p.Reason == "" {
			return fmt.Errorf("%s requires a reason", e.Verb)
		}
		if e.Verb == VerbQualified && p.Reason != "" {
			return fmt.Errorf("%s carries no reason: the cited verdict is the reason", e.Verb)
		}
		cur := s.entries[e.Subject]
		if cur == nil {
			return fmt.Errorf("%s subject %s is not enrolled", e.Verb, e.Subject)
		}
		if cur.Standing == StandingRevoked {
			return fmt.Errorf("actor %s is revoked; revocation is terminal", e.Subject)
		}
		q := Qualification{Capability: p.Capability, Tuple: cited, Contract: p.Contract, Verdict: verdictPos, TS: e.TS, Reason: p.Reason}
		if e.Verb == VerbQualified {
			// A qualification IS a grant with evidence: it grants the
			// capability if absent and adds the tuple to the admissible
			// set, under the same disjointness rule a grant obeys.
			if err := sealerDisjoint(cur, p.Capability); err != nil {
				return fmt.Errorf("%s to %s: %v", e.Verb, e.Subject, err)
			}
			if !slices.Contains(cur.Grants, p.Capability) {
				cur.Grants = append(cur.Grants, p.Capability)
			}
			cur.citeTuple(p.Capability, cited)
			cur.markCited(p.Capability)
		} else {
			// Nothing to disqualify is a refusal, so a disqualification
			// always names a configuration that was admissible.
			kept := cur.Tuples[p.Capability][:0:0]
			removed := false
			for _, have := range cur.Tuples[p.Capability] {
				if have.Equal(cited) {
					removed = true
					continue
				}
				kept = append(kept, have)
			}
			if !removed {
				// A verifier holding verdict by a bare grant renders
				// under the bridge, every configuration admissible and
				// none cited; its first failed calibration disqualifies
				// the configuration it rendered under and closes the
				// bridge, so a drifted verifier does not keep rendering
				// because nothing had cited its tuple yet (review
				// finding on the task PR). The bridge is the never
				// cited's, so once cited there is nothing left to
				// disqualify, exactly as before.
				bridging := p.Capability == CapVerdict && slices.Contains(cur.Grants, CapVerdict) && !cur.everCited[CapVerdict]
				if !bridging {
					return fmt.Errorf("%s: actor %s holds no admissible %s grant citing that tuple, so there is nothing to disqualify", e.Verb, e.Subject, p.Capability)
				}
				cur.markCited(p.Capability)
			}
			if cur.Tuples == nil {
				cur.Tuples = map[string][]tuple.Tuple{}
			}
			cur.Tuples[p.Capability] = kept
			q.Disqualified = true
		}
		cur.Qualifications = append(cur.Qualifications, q)
	case VerbSuspended, VerbRevoked:
		var p struct {
			Reason string `json:"reason"`
		}
		if err := strict(e.Payload, &p); err != nil {
			return fmt.Errorf("%s payload: %v", e.Verb, err)
		}
		if p.Reason == "" {
			return fmt.Errorf("%s must name a reason", e.Verb)
		}
		cur := s.entries[e.Subject]
		if cur == nil {
			return fmt.Errorf("%s subject %s is not enrolled", e.Verb, e.Subject)
		}
		if cur.Standing == StandingRevoked {
			return fmt.Errorf("actor %s is already revoked; revocation is terminal", e.Subject)
		}
		if e.Verb == VerbSuspended && cur.Standing == StandingSuspended {
			return fmt.Errorf("actor %s is already suspended", e.Subject)
		}
		if cur.Root && cur.Standing == StandingActive && s.activeRoots() == 1 {
			return errors.New("refusing to end the last active governance root's standing: the keyring must keep at least one active root (root liveness, plans/os-52a2d688.md)")
		}
		if e.Verb == VerbSuspended {
			cur.Standing = StandingSuspended
		} else {
			cur.Standing = StandingRevoked
		}
	default:
		return fmt.Errorf("actor verb %q is not defined at %s (actor.qualified cites eval results, which land with Phase 10 item 2, docs/build-plan.md)", e.Verb, e.V)
	}
	return nil
}

// StateAt projects the keyring over a prefix of verified records,
// tracking the active protocol version the same way verification does:
// genesis names the initial version, and system.protocol.upgraded
// switches it as the last event of the old version. It returns the state
// and the version active after the last record. The prefix must already
// have verified; an Advance error here means it had not.
func StateAt(records []*event.Record) (*State, string, error) {
	s := New()
	active := ""
	for pos, rec := range records {
		if active == "" {
			active = rec.Event.V
			if pos == 0 && rec.Event.Verb == genesisVerb && rec.Event.Subject == "system" {
				var g struct {
					Protocol string `json:"protocol"`
				}
				if err := json.Unmarshal(rec.Event.Payload, &g); err == nil && g.Protocol != "" {
					active = g.Protocol
				}
				s.SeedGenesis(rec)
			}
		}
		if Applies(active) {
			if err := s.Advance(rec); err != nil {
				return nil, "", fmt.Errorf("position %d: %v", pos, err)
			}
		}
		if rec.Event.Verb == upgradeVerb && rec.Event.Subject == "system" {
			var up struct {
				To string `json:"to"`
			}
			if err := json.Unmarshal(rec.Event.Payload, &up); err == nil && up.To != "" {
				active = up.To
			}
		}
	}
	if active == "" {
		active = version.Protocol
	}
	return s, active, nil
}
