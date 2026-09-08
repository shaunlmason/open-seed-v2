package admit

// The staged curation stores at the boundary (plans/os-f30ee0d3.md
// AC1 through AC5; plans/os-96850e5a.md AC1 through AC4): the dead end
// inside the holder's window, the proposal's support floor, its actor
// arm and its curate-only row, the grant-level disjointness, the
// contest over held-out evidence, the promotion gate's ledger half
// with its adversarial arm, the curator's raise, and the curator's
// reachable set derived from the boundary.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const trivialFiling = `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`

// findingPacket is a packet carrying one finding: a deliberate exit
// that is a stage-one observation.
const findingPacket = `{"acceptance": ["resume"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": [{"tried": "x", "outcome": "y"}]}`

// evalSpec is the shipped definition's fixture at a gated revision:
// what evalBound requires.
const boundEvalSpec = `{"acceptance": {"ref": "evals/fix-the-check/fixture/accept.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`

type curationStand struct {
	ctx                                      *Context
	root, worker, worker2, curator, observer ed25519.PrivateKey
	verifier, dispatcher, stranger           ed25519.PrivateKey
	step                                     func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context
	// The observation positions.
	deadEnd1, deadEnd1b, park2, submission3, deadEnd6 int
	deadEnd7, deadEnd8, deadEnd9, raise5              int
	claim, id                                         string
	v                                                 string
	// level is the independence an eval pass records; L1 unless a
	// drill sets it (the level vocabulary applies from seed/4).
	level string
}

func deadEndBody(fence string) string {
	return fmt.Sprintf(`{"fence": %q, "tried": "x", "outcome": "y", "condition": "z", "environment": "w"}`, fence)
}

func filing(tier, routing string) string {
	return fmt.Sprintf(`{"intent": "drill", "tier": %q, "budget": "small", "routing": %q}`, tier, routing)
}

// curationFixture stages the record the drills judge against, at
// seed/3 so the eval marker is defined: contracts worked by two
// workers (dead ends, a park with a finding, a submission that failed,
// a raise, a standard-tier pair held by one worker, one routed
// elsewhere), and the curation lanes enrolled with their grants.
func curationFixture(t *testing.T) *curationStand {
	t.Helper()
	store, resolve, root := seededStore(t)
	st := &curationStand{root: root, worker: fixtureKey(t, 2), claim: "retry the fetch once", v: version.Seed3}
	st.id = curation.HypothesisID(st.claim, nil)
	st.worker2, st.curator, st.observer, st.verifier, st.dispatcher = fixtureKey(t, 11), fixtureKey(t, 12), fixtureKey(t, 13), fixtureKey(t, 14), fixtureKey(t, 15)
	st.stranger = fixtureKey(t, 17)
	keys := []ed25519.PrivateKey{root, st.worker, st.worker2, st.curator, st.observer, st.verifier, st.dispatcher, fixtureKey(t, 16), st.stranger}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range keys {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	appendSignedV(t, store, loose, root, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	appendSignedV(t, store, loose, root, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	v := st.v
	step := func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	st.step = step
	var ctx *Context
	for _, e := range []struct {
		key  ed25519.PrivateKey
		name string
		cap  string
	}{
		{st.worker, "worker", keyring.CapClaim}, {st.worker2, "worker2", keyring.CapClaim}, {st.curator, "curator", keyring.CapCurate},
		{st.observer, "observer", keyring.CapObserver}, {st.verifier, "verifier", keyring.CapVerdict},
		{st.dispatcher, "dispatcher", keyring.CapDispatch},
	} {
		ctx = step(root, v, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
		ctx = step(root, v, keyring.VerbGranted, fpOf(t, e.key), `{"capability": "`+e.cap+`"}`)
	}
	// The stranger is enrolled and holds nothing: the key whose raw
	// pushes the boundary drills re-judge.
	ctx = step(root, v, keyring.VerbEnrolled, fpOf(t, st.stranger), enrollBody(t, st.stranger, "agent", "stranger"))
	open := func(subject, tier, routing string, holder ed25519.PrivateKey) *Context {
		ctx = step(root, v, "intent.filed", subject, filing(tier, routing))
		ctx = step(root, v, "contract.specified", subject, specBody)
		return step(holder, v, "claim.taken", subject, `{}`)
	}
	deadEnd := func(subject string, holder ed25519.PrivateKey) int {
		t.Helper()
		body := deadEndBody(activeFence(t, ctx, subject))
		if err := Check(ctx, draftV(t, holder, v, curation.DeadEndVerb, subject, body, ctx.Tip)); err != nil {
			t.Fatalf("the holder's dead end inside its window admits: %v", err)
		}
		at := ctx.Count
		ctx = step(holder, v, curation.DeadEndVerb, subject, body)
		return at
	}
	// c-4: specified and never claimed, the ready contract.
	ctx = step(root, v, "intent.filed", "c-4", trivialFiling)
	ctx = step(root, v, "contract.specified", "c-4", specBody)
	// c-1: the worker's, two dead ends.
	ctx = open("c-1", "trivial", "core", st.worker)
	st.deadEnd1 = deadEnd("c-1", st.worker)
	st.deadEnd1b = deadEnd("c-1", st.worker)
	// c-2: worker2's, parked with a finding.
	ctx = open("c-2", "trivial", "core", st.worker2)
	st.park2 = ctx.Count
	ctx = step(st.worker2, v, "claim.parked", "c-2", `{"fence": "`+activeFence(t, ctx, "c-2")+`", "packet": `+findingPacket+`}`)
	// c-5: held, then a raise carrying a finding: a question, not an
	// exit, so no observation.
	ctx = open("c-5", "trivial", "core", st.worker)
	st.raise5 = ctx.Count
	ctx = step(st.worker, v, "escalation.raised", "c-5", `{"fence": "`+activeFence(t, ctx, "c-5")+`", "packet": `+findingPacket+`, "escalation": {"question": "which?", "options": [{"id": "a", "choice": "this"}, {"id": "b", "choice": "that"}]}}`)
	// c-3: the worker's, submitted with a finding, then failed.
	ctx = open("c-3", "trivial", "core", st.worker)
	st.submission3 = ctx.Count
	ctx = step(st.worker, v, "submission.made", "c-3", `{"fence": "`+activeFence(t, ctx, "c-3")+`", "packet": `+findingPacket+`}`)
	ctx = step(st.verifier, v, "verdict.rendered", "c-3",
		`{"verdict": "fail", "receipt": "`+strings.Repeat("0", 64)+`", "submission": "`+fmt.Sprint(st.submission3)+`", "independence": "L1"}`)
	// c-6: the worker's second contract with a dead end: two contracts,
	// one holder.
	ctx = open("c-6", "trivial", "core", st.worker)
	st.deadEnd6 = deadEnd("c-6", st.worker)
	// c-7 and c-8: standard tier, both the worker's: a family with one
	// holder.
	ctx = open("c-7", "standard", "core", st.worker)
	st.deadEnd7 = deadEnd("c-7", st.worker)
	ctx = open("c-8", "standard", "core", st.worker)
	st.deadEnd8 = deadEnd("c-8", st.worker)
	// c-9: routed elsewhere, with a dead end: unselected by "core".
	ctx = open("c-9", "trivial", "other", st.worker)
	st.deadEnd9 = deadEnd("c-9", st.worker)
	st.ctx = ctx
	return st
}

const appliesCore = `{"routing": "core"}`

func (st *curationStand) proposalWith(claim, applies string, exceptions []string, support ...string) string {
	q := func(xs []string) string {
		out := make([]string, len(xs))
		for i, x := range xs {
			out[i] = fmt.Sprintf("%q", x)
		}
		return "[" + strings.Join(out, ", ") + "]"
	}
	return fmt.Sprintf(`{"claim": %q, "applies_when": %s, "support": %s, "exceptions": %s, "provenance": ["plans/x.md @ 0123456"]}`,
		claim, applies, q(support), q(exceptions))
}

func (st *curationStand) proposal(support ...string) string {
	return st.proposalWith(st.claim, appliesCore, nil, support...)
}

func cite(contract string, pos int) string { return fmt.Sprintf("%s@%d", contract, pos) }

func gate(t *testing.T, err error) string {
	t.Helper()
	var ge *curation.GateError
	if !errors.As(err, &ge) {
		t.Fatalf("a curation refusal is a GateError at a registered gate, got %v", err)
	}
	if _, ok := curation.GateDescription(ge.Gate); !ok {
		t.Fatalf("gate %q is not registered", ge.Gate)
	}
	return ge.Gate
}

// admitHypothesis proposes the stand's claim from the curator and
// returns its position.
func (st *curationStand) admitHypothesis(t *testing.T) int {
	t.Helper()
	good := st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2))
	if err := Check(st.ctx, draftV(t, st.curator, st.v, curation.HypothesisVerb, st.id, good, st.ctx.Tip)); err != nil {
		t.Fatalf("two observations on two contracts from two holders from curate admit: %v", err)
	}
	pos := st.ctx.Count
	st.ctx = st.step(st.curator, st.v, curation.HypothesisVerb, st.id, good)
	return pos
}

// conformance: AC1 — the dead end admits from the window's holder
// citing the active fence and refuses everything else naming the part.
func TestDeadEndIsTheHoldersInsideItsWindow(t *testing.T) {
	st := curationFixture(t)
	ctx, v := st.ctx, st.v
	fence := activeFence(t, ctx, "c-1")
	// Outside a window the fence rule speaks first: no claim is
	// active, so the citation refuses as a fence refusal naming the
	// window's absence.
	var fe *FenceError
	if err := Check(ctx, draftV(t, st.worker, v, curation.DeadEndVerb, "c-4", deadEndBody("0"), ctx.Tip)); !errors.As(err, &fe) || !strings.Contains(err.Error(), "no claim is active") {
		t.Fatalf("outside a window refuses naming the window: %v", err)
	}
	if err := Check(ctx, draftV(t, st.worker2, v, curation.DeadEndVerb, "c-1", deadEndBody(fence), ctx.Tip)); err == nil || gate(t, err) != curation.GateDeadEndHolder || !strings.Contains(err.Error(), "holder") {
		t.Fatalf("a non-holder refuses naming the holder: %v", err)
	}
	if err := Check(ctx, draftV(t, st.worker, v, curation.DeadEndVerb, "c-1", deadEndBody("0"), ctx.Tip)); !errors.As(err, &fe) {
		t.Fatalf("a stale fence refuses as a fence refusal: %v", err)
	}
	var inc *transition.IncompleteError
	if err := Check(ctx, draftV(t, st.worker, v, curation.DeadEndVerb, "c-1",
		`{"fence": "`+fence+`", "tried": "x", "outcome": "y", "condition": "", "environment": "w"}`, ctx.Tip)); !errors.As(err, &inc) || fmt.Sprint(inc.Missing) != "[condition]" {
		t.Fatalf("a missing field refuses naming it: %v", err)
	}
	if err := Check(ctx, draftV(t, st.worker, v, curation.DeadEndVerb, "c-1",
		`{"fence": "`+fence+`", "tried": "x", "outcome": "y", "condition": "z", "environment": "w", "pointer": "notes.md"}`, ctx.Tip)); err == nil || gate(t, err) != curation.GateDeadEndShape || !strings.Contains(err.Error(), "anchored") {
		t.Fatalf("a bare-path pointer refuses: %v", err)
	}
	var oog *OutOfGrantError
	if err := Check(ctx, draftV(t, st.dispatcher, v, curation.DeadEndVerb, "c-1", deadEndBody(fence), ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a dispatch-only key cannot reach the dead end: %v", err)
	}
	if err := Check(ctx, draftV(t, st.worker, v, curation.DeadEndVerb, "c-1",
		`{"fence": "`+fence+`", "tried": "x", "outcome": "y", "condition": "z", "environment": "w", "pointer": "notes.md @ 0123456"}`, ctx.Tip)); err != nil {
		t.Fatalf("an anchored pointer admits: %v", err)
	}
}

// conformance: item 1 AC2 and item 2 AC1, AC2 — the proposal admits
// from curate on the derived subject citing two admitted observations
// on two distinct non-failed contracts from two holders where the
// family allows it, and refuses each other shape at its gate, the root
// included.
func TestHypothesisNeedsTwoObservationsOnTwoContractsFromCurate(t *testing.T) {
	st := curationFixture(t)
	ctx, v := st.ctx, st.v
	good := st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2))
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, good, ctx.Tip)); err != nil {
		t.Fatalf("two observations on two contracts from two holders from curate admit: %v", err)
	}
	var oog *OutOfGrantError
	if err := Check(ctx, draftV(t, st.worker, v, curation.HypothesisVerb, st.id, good, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a claim key refuses out of grant: %v", err)
	}
	if err := Check(ctx, draftV(t, st.root, v, curation.HypothesisVerb, st.id, good, ctx.Tip)); !errors.As(err, &oog) || fmt.Sprint(oog.Accepted) != "[curate]" {
		t.Fatalf("a root's implicit operator standing does not reach the proposal; the accepted set is [curate] alone: %v", err)
	}
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, "h-000000000000", good, ctx.Tip)); err == nil || gate(t, err) != curation.GateProposalSubject || !strings.Contains(err.Error(), st.id) {
		t.Fatalf("a subject not derived from the claim refuses naming the derived one: %v", err)
	}
	for name, c := range map[string]struct{ body, gate string }{
		"one contract":     {st.proposal(cite("c-1", st.deadEnd1)), curation.GateSupportFloor},
		"two on one":       {st.proposal(cite("c-1", st.deadEnd1), cite("c-1", st.deadEnd1b)), curation.GateSupportFloor},
		"not observation":  {st.proposal(cite("c-1", st.deadEnd1-3), cite("c-2", st.park2)), curation.GateSupportObservation},
		"a raise":          {st.proposal(cite("c-1", st.deadEnd1), cite("c-5", st.raise5)), curation.GateSupportObservation},
		"failed contract":  {st.proposal(cite("c-1", st.deadEnd1), cite("c-3", st.submission3)), curation.GateSupportFailed},
		"wrong contract":   {st.proposal(cite("c-2", st.deadEnd1), cite("c-2", st.park2)), curation.GateSupportObservation},
		"cited twice":      {st.proposal(cite("c-1", st.deadEnd1), cite("c-1", st.deadEnd1), cite("c-2", st.park2)), curation.GateSupportObservation},
		"out of the chain": {st.proposal(cite("c-1", st.deadEnd1), cite("c-2", 99999)), curation.GateSupportObservation},
		// The actor arm (item 2 AC2): the family "core" has two
		// holders, so two contracts held by one key refuse.
		"one holder": {st.proposal(cite("c-1", st.deadEnd1), cite("c-6", st.deadEnd6)), curation.GateSupportActors},
		// The predicate (item 2 AC1).
		"empty predicate":   {st.proposalWith(st.claim, `{}`, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)), curation.GateAppliesWhen},
		"unknown predicate": {st.proposalWith(st.claim, `{"routing": "core", "squad": "x"}`, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)), curation.GateAppliesWhen},
		"empty paths":       {st.proposalWith(st.claim, `{"paths": []}`, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)), curation.GateAppliesWhen},
		"non-string":        {st.proposalWith(st.claim, `{"tier": 1}`, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)), curation.GateAppliesWhen},
	} {
		err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, c.body, ctx.Tip))
		if err == nil {
			t.Errorf("%s: refuses", name)
			continue
		}
		if got := gate(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s (%v)", name, c.gate, got, err)
		}
	}
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1)), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "2 distinct") {
		t.Fatalf("the refusal names the floor: %v", err)
	}
	// A family with one holder (the standard tier's) admits support
	// from that one holder, and the fold records why the arm was
	// waived; the family is the selection's, never the support's.
	single := "standard contracts retry"
	singleID := curation.HypothesisID(single, nil)
	singleBody := st.proposalWith(single, `{"tier": "standard"}`, nil, cite("c-7", st.deadEnd7), cite("c-8", st.deadEnd8))
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, singleID, singleBody, ctx.Tip)); err != nil {
		t.Fatalf("in a family with one holder the same-holder support admits: %v", err)
	}
	ctx = st.step(st.curator, v, curation.HypothesisVerb, singleID, singleBody)
	if fold := curation.Fold(ctx.Records); !fold.SingleActorFamily(ctx.Records, ctx.Table, singleID) {
		t.Fatal("the fold records single_actor_family for the waived arm")
	}
	// The duplicate: one claim with one exception set derives one
	// subject; an added exception is a new subject.
	ctx = st.step(st.curator, v, curation.HypothesisVerb, st.id, good)
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, good, ctx.Tip)); err == nil || gate(t, err) != curation.GateSupportDuplicate {
		t.Fatalf("a re-proposal of an admitted claim refuses as a duplicate: %v", err)
	}
	exID := curation.HypothesisID(st.claim, []string{"a warm mirror"})
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, exID, st.proposalWith(st.claim, appliesCore, []string{"a warm mirror"}, cite("c-1", st.deadEnd1), cite("c-2", st.park2)), ctx.Tip)); err != nil {
		t.Fatalf("a proposal adding an exception is a new subject: %v", err)
	}
}

// conformance: AC3 — curate is disjoint from claim and operator at
// the grant, both directions, as chain validity; a root included.
func TestCurateIsDisjointAtTheGrant(t *testing.T) {
	st := curationFixture(t)
	ctx := st.ctx
	for name, draft := range map[string][2]string{
		"curate onto claim":    {fpOf(t, st.worker), keyring.CapCurate},
		"claim onto curate":    {fpOf(t, st.curator), keyring.CapClaim},
		"operator onto curate": {fpOf(t, st.curator), keyring.CapOperator},
		"curate onto a root":   {fpOf(t, st.root), keyring.CapCurate},
	} {
		err := Check(ctx, draftV(t, st.root, st.v, keyring.VerbGranted, draft[0], `{"capability": "`+draft[1]+`"}`, ctx.Tip))
		if err == nil || !strings.Contains(err.Error(), "disjoint") {
			t.Errorf("%s: refuses at the grant naming the disjointness: %v", name, err)
		}
	}
}

// conformance: item 2 AC3 — the contest admits from curate citing
// held-out observations on selected contracts, refuses each other
// citation and each other key at its gate, moves the fold to
// contested, and closes the promotion.
func TestContestCitesHeldOutEvidence(t *testing.T) {
	st := curationFixture(t)
	v := st.v
	hp := st.admitHypothesis(t)
	ctx := st.ctx
	contest := func(evidence ...string) string {
		q := make([]string, len(evidence))
		for i, e := range evidence {
			q[i] = fmt.Sprintf("%q", e)
		}
		return fmt.Sprintf(`{"hypothesis": "%s", "evidence": [%s], "reason": "the mirror was warm and it still failed"}`, cite(st.id, hp), strings.Join(q, ", "))
	}
	good := contest(cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6))
	if err := Check(ctx, draftV(t, st.curator, v, curation.ContestVerb, st.id, good, ctx.Tip)); err != nil {
		t.Fatalf("held-out observations on selected contracts contest: %v", err)
	}
	var oog *OutOfGrantError
	if err := Check(ctx, draftV(t, st.worker, v, curation.ContestVerb, st.id, good, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a claim key cannot contest: %v", err)
	}
	if err := Check(ctx, draftV(t, st.root, v, curation.ContestVerb, st.id, good, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a root holding no curate cannot contest: %v", err)
	}
	for name, c := range map[string]struct{ body, gate string }{
		"support set":     {contest(cite("c-1", st.deadEnd1)), curation.GateContestHeldOut},
		"unselected":      {contest(cite("c-9", st.deadEnd9)), curation.GateContestSelected},
		"not observation": {contest(cite("c-5", st.raise5)), curation.GateContestEvidence},
		"no hypothesis":   {strings.Replace(good, cite(st.id, hp), cite(st.id, hp+1), 1), curation.GateContestHypothesis},
	} {
		err := Check(ctx, draftV(t, st.curator, v, curation.ContestVerb, st.id, c.body, ctx.Tip))
		if err == nil {
			t.Errorf("%s: refuses", name)
			continue
		}
		if got := gate(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s (%v)", name, c.gate, got, err)
		}
	}
	held := map[string]bool{}
	for _, o := range curation.HeldOut(ctx.Records, ctx.Table, ctx.Lifecycle, mustHyp(t, ctx, st.id)) {
		held[cite(o.Contract, o.Position)] = true
	}
	if !held[cite("c-1", st.deadEnd1b)] || !held[cite("c-6", st.deadEnd6)] || held[cite("c-1", st.deadEnd1)] || held[cite("c-2", st.park2)] || held[cite("c-9", st.deadEnd9)] || held[cite("c-5", st.raise5)] {
		t.Fatalf("the held-out listing is the selected observations outside the support set: %v", held)
	}
	ctx = st.step(st.curator, v, curation.ContestVerb, st.id, good)
	if !curation.Fold(ctx.Records).Contested(st.id) {
		t.Fatal("the fold reads contested")
	}
	if err := Check(ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", 0), ctx.Tip)); err == nil || gate(t, err) != curation.GatePromotionContested {
		t.Fatalf("a contested hypothesis refuses promotion at its gate: %v", err)
	}
}

func mustHyp(t *testing.T, ctx *Context, id string) *curation.HypothesisFact {
	t.Helper()
	h, ok := curation.Fold(ctx.Records).Hypothesis(id)
	if !ok {
		t.Fatalf("no hypothesis %s", id)
	}
	return h
}

func lessonBody(id string, hp int, evalName string, verdictPos int) string {
	return fmt.Sprintf(`{"lesson": "%s/retry.md @ 0123456", "hypothesis": "%s", "pr": "pr/9 @ 0123456", "carrier": "knowledge", "adversarial": {"eval": %q, "verdict": "%d"}, "last_validated": "2026-09-01T00:00:00Z", "expires": "2026-12-01T00:00:00Z", "digest": "%s"}`,
		curation.LessonsDir, cite(id, hp), evalName, verdictPos, strings.Repeat("a", 64))
}

// evalRun files a bound eval and works it to a verdict, returning the
// verdict's position. The marker binds lesson and carrier; verdict is
// pass or fail; the verifier signs unless another key is given.
func (st *curationStand) evalRun(t *testing.T, subject, lesson, carrier, verdict string, signer ed25519.PrivateKey) int {
	t.Helper()
	v := st.v
	marker := `{"name": "fix-the-check"`
	if lesson != "" {
		marker += fmt.Sprintf(`, "lesson": %q, "carrier": %q`, lesson, carrier)
	}
	marker += `}`
	st.ctx = st.step(st.root, v, "intent.filed", subject, `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": `+marker+`}`)
	st.ctx = st.step(st.root, v, "contract.specified", subject, boundEvalSpec)
	st.ctx = st.step(st.worker2, v, "claim.taken", subject, `{}`)
	sub := st.ctx.Count
	st.ctx = st.step(st.worker2, v, "submission.made", subject, `{"fence": "`+activeFence(t, st.ctx, subject)+`", "packet": `+findingPacket+`}`)
	pos := st.ctx.Count
	if signer == nil {
		signer = st.verifier
	}
	level := st.level
	if level == "" {
		level = "L1"
	}
	st.ctx = st.step(signer, v, "verdict.rendered", subject,
		fmt.Sprintf(`{"verdict": %q, "receipt": "%s", "submission": "%d", "independence": %q}`, verdict, strings.Repeat("0", 64), sub, level))
	return pos
}

// conformance: item 1 AC4 and item 2 AC4 — the promotion admits from
// observer citing the admitted hypothesis and an authenticated pass on
// an eval bound to it and to this lesson anchor, filed after it, and
// refuses every other shape and citation at its gate; a raw-pushed
// promotion citing nothing folds unbound.
func TestPromotionCitesAnAdmittedHypothesisAndASurvivedEval(t *testing.T) {
	st := curationFixture(t)
	v := st.v
	anchor := curation.LessonsDir + "/retry.md @ 0123456"
	// An eval filed BEFORE the hypothesis, bound to the position it
	// will take: a counter-trajectory constructed before the candidate
	// existed is not survival.
	early := st.evalRun(t, "eval-early", cite(st.id, st.ctx.Count+5), anchor, "pass", nil)
	hp := st.admitHypothesis(t)
	if hp != early-4 {
		// The early eval's marker cited the wrong position; make the
		// arithmetic visible rather than silently passing.
		t.Logf("early eval bound to %d, hypothesis at %d", early-4, hp)
	}
	bound := st.evalRun(t, "eval-bound", cite(st.id, hp), anchor, "pass", nil)
	failed := st.evalRun(t, "eval-failed", cite(st.id, hp), anchor, "fail", nil)
	other := "retry twice"
	otherID := curation.HypothesisID(other, nil)
	otherBound := st.evalRun(t, "eval-other-hyp", cite(otherID, hp), anchor, "pass", nil)
	otherCarrier := st.evalRun(t, "eval-other-carrier", cite(st.id, hp), curation.LessonsDir+"/retry.md @ 9999999", "pass", nil)
	unbound := st.evalRun(t, "eval-unbound", "", "", "pass", nil)
	// A pass signed by the eval's own implementer: L1 independence
	// never held, whatever grant the key acquires.
	selfPass := st.evalRun(t, "eval-self", cite(st.id, hp), anchor, "pass", st.worker2)
	// The same with a verdict grant raw-pushed onto the implementer's
	// key past the grant's disjointness: the grant alone would
	// authenticate the pass, and disjointness at the verdict's
	// position is what still refuses it.
	st.ctx = st.step(st.root, v, keyring.VerbGranted, fpOf(t, st.worker2), `{"capability": "`+keyring.CapVerdict+`"}`)
	selfGranted := st.evalRun(t, "eval-self-granted", cite(st.id, hp), anchor, "pass", st.worker2)
	// A pass signed by a key granted verdict only afterwards.
	late := fixtureKey(t, 16)
	st.ctx = st.step(st.root, v, keyring.VerbEnrolled, fpOf(t, late), enrollBody(t, late, "agent", "late"))
	latePass := st.evalRun(t, "eval-late", cite(st.id, hp), anchor, "pass", late)
	st.ctx = st.step(st.root, v, keyring.VerbGranted, fpOf(t, late), `{"capability": "`+keyring.CapVerdict+`"}`)
	// An ordinary contract's pass: no marker at all.
	st.ctx = st.step(st.root, v, "intent.filed", "c-10", trivialFiling)
	st.ctx = st.step(st.root, v, "contract.specified", "c-10", specBody)
	st.ctx = st.step(st.worker2, v, "claim.taken", "c-10", `{}`)
	sub10 := st.ctx.Count
	st.ctx = st.step(st.worker2, v, "submission.made", "c-10", `{"fence": "`+activeFence(t, st.ctx, "c-10")+`", "packet": `+findingPacket+`}`)
	plainPass := st.ctx.Count
	st.ctx = st.step(st.verifier, v, "verdict.rendered", "c-10", fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("0", 64), sub10))
	ctx := st.ctx

	good := lessonBody(st.id, hp, "fix-the-check", bound)
	if err := Check(ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, good, ctx.Tip)); err != nil {
		t.Fatalf("the observer promotes the admitted hypothesis citing the survived eval: %v", err)
	}
	var oog *OutOfGrantError
	if err := Check(ctx, draftV(t, st.curator, v, curation.LessonVerb, st.id, good, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a curate key cannot promote: %v", err)
	}
	for name, c := range map[string]struct{ body, gate string }{
		"no hypothesis":      {lessonBody(st.id, hp+1, "fix-the-check", bound), curation.GatePromotionHypothesis},
		"bare path":          {strings.Replace(good, "/retry.md @ 0123456", "/retry.md", 1), curation.GatePromotionShape},
		"climbs out":         {strings.Replace(good, "/retry.md @ 0123456", "/../../retry.md @ 0123456", 1), curation.GatePromotionShape},
		"dot segment":        {strings.Replace(good, "/retry.md @ 0123456", "/./retry.md @ 0123456", 1), curation.GatePromotionShape},
		"absolute":           {strings.Replace(good, curation.LessonsDir+"/retry.md @ 0123456", "/"+curation.LessonsDir+"/retry.md @ 0123456", 1), curation.GatePromotionShape},
		"no carrier":         {strings.Replace(good, `"carrier": "knowledge", `, "", 1), ""},
		"unknown carrier":    {strings.Replace(good, `"knowledge"`, `"prompt"`, 1), curation.GatePromotionCarrier},
		"no stamp":           {strings.Replace(good, `"expires": "2026-12-01T00:00:00Z", `, "", 1), ""},
		"stamps unordered":   {strings.Replace(good, "2026-12-01", "2026-08-01", 1), curation.GatePromotionStamps},
		"no digest":          {strings.Replace(good, `, "digest": "`+strings.Repeat("a", 64)+`"`, "", 1), ""},
		"no adversarial":     {strings.Replace(good, `"adversarial": {"eval": "fix-the-check", "verdict": "`+fmt.Sprint(bound)+`"}, `, "", 1), ""},
		"a fail":             {lessonBody(st.id, hp, "fix-the-check", failed), curation.GatePromotionAdversary},
		"no marker":          {lessonBody(st.id, hp, "fix-the-check", plainPass), curation.GatePromotionAdversary},
		"another eval":       {lessonBody(st.id, hp, "other-eval", bound), curation.GatePromotionAdversary},
		"unbound marker":     {lessonBody(st.id, hp, "fix-the-check", unbound), curation.GatePromotionAdversary},
		"another hypothesis": {lessonBody(st.id, hp, "fix-the-check", otherBound), curation.GatePromotionAdversary},
		"another carrier":    {lessonBody(st.id, hp, "fix-the-check", otherCarrier), curation.GatePromotionAdversary},
		"filed before":       {lessonBody(st.id, hp, "fix-the-check", early), curation.GatePromotionAdversary},
		"wrong position":     {lessonBody(st.id, hp, "fix-the-check", bound-1), curation.GatePromotionAdversary},
		"later-granted key":  {lessonBody(st.id, hp, "fix-the-check", latePass), curation.GatePromotionAdversary},
		"implementer's pass": {lessonBody(st.id, hp, "fix-the-check", selfPass), curation.GatePromotionAdversary},
		"self-granted pass":  {lessonBody(st.id, hp, "fix-the-check", selfGranted), curation.GatePromotionAdversary},
	} {
		err := Check(ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, c.body, ctx.Tip))
		if err == nil {
			t.Errorf("%s: refuses", name)
			continue
		}
		if c.gate == "" {
			var inc *transition.IncompleteError
			if !errors.As(err, &inc) {
				t.Errorf("%s: refuses as incomplete naming the field, got %v", name, err)
			}
			continue
		}
		if got := gate(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s (%v)", name, c.gate, got, err)
		}
	}
	// A role carrier admits with the same survived eval: no carrier is
	// exempt, and none is refused.
	if err := Check(ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, strings.Replace(good, `"knowledge"`, `"role"`, 1), ctx.Tip)); err != nil {
		t.Fatalf("any carrier admits with a survived eval: %v", err)
	}
	// A raw-pushed proposal never passed the boundary (a claim key
	// signed it), so a promotion citing it refuses: no stage skips.
	rawPos := ctx.Count
	ctx = st.step(st.worker, v, curation.HypothesisVerb, otherID, st.proposalWith(other, appliesCore, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	if err := Check(ctx, draftV(t, st.observer, v, curation.LessonVerb, otherID, lessonBody(otherID, rawPos, "fix-the-check", otherBound), ctx.Tip)); err == nil || gate(t, err) != curation.GatePromotionHypothesis {
		t.Fatalf("a promotion citing a proposal that never passed the boundary refuses: %v", err)
	}
	ctx = st.step(st.observer, v, curation.LessonVerb, "h-000000000000", lessonBody("h-000000000000", 0, "fix-the-check", bound))
	if fold := curation.Fold(ctx.Records); len(fold.Unbound) != 1 || len(fold.Lessons) != 0 {
		t.Fatalf("a raw-pushed promotion citing no hypothesis folds unbound, never as a lesson: %+v", fold.Unbound)
	}
	// The marker's shape at filing: a lesson without a carrier, or a
	// malformed citation, refuses at intent.filed.
	for name, marker := range map[string]string{
		"half-bound": `{"name": "fix-the-check", "lesson": "` + cite(st.id, hp) + `"}`,
		"bad lesson": `{"name": "fix-the-check", "lesson": "c-1@3", "carrier": "` + anchor + `"}`,
		"bare":       `{"name": "fix-the-check", "lesson": "` + cite(st.id, hp) + `", "carrier": "x.md"}`,
		"unknown":    `{"name": "fix-the-check", "for": "x"}`,
	} {
		if err := Check(ctx, draftV(t, st.root, v, "intent.filed", "eval-shape", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": `+marker+`}`, ctx.Tip)); err == nil {
			t.Errorf("%s: a malformed bound marker refuses at filing", name)
		}
	}
}

// conformance: AC5 — the curator raises exactly as any raiser, and its
// reachable set, derived from the boundary, is the proposal, the
// contest, the raise and the standing-only relay, each a named
// residual.
func TestCuratorRaisesAndItsReachableSetIsNamed(t *testing.T) {
	st := curationFixture(t)
	v := st.v
	st.admitHypothesis(t)
	ctx := st.ctx
	raise := `{"packet": ` + minPacket + `, "escalation": {"question": "which?", "options": [{"id": "a", "choice": "this"}, {"id": "b", "choice": "that"}]}}`
	// c-4 is ready, c-2 blocked by its park, c-1 held: the curator's
	// answer on each is the dispatcher's.
	for _, subject := range []string{"c-4", "c-2", "c-1"} {
		curatorErr := Check(ctx, draftV(t, st.curator, v, "escalation.raised", subject, raise, ctx.Tip))
		dispatchErr := Check(ctx, draftV(t, st.dispatcher, v, "escalation.raised", subject, raise, ctx.Tip))
		if (curatorErr == nil) != (dispatchErr == nil) {
			t.Fatalf("on %s the curator raises exactly as the dispatcher does: %v vs %v", subject, curatorErr, dispatchErr)
		}
	}
	if err := Check(ctx, draftV(t, st.curator, v, "escalation.raised", "c-4", raise, ctx.Tip)); err != nil {
		t.Fatalf("the curator raises on a ready contract: %v", err)
	}
	if err := Check(ctx, draftV(t, st.curator, v, "escalation.raised", "c-2", raise, ctx.Tip)); err == nil {
		t.Fatal("a blocked contract refuses the raise from the curator as from any raiser")
	}

	named := map[string]bool{}
	for _, r := range loadResidualsFrom(t, "curator-residuals.json") {
		named[r.Verb] = true
	}
	seen := map[string]bool{}
	sweep := func() {
		for _, subject := range []string{"c-1", "c-2", "c-3", "c-4"} {
			for _, verb := range Affordances(st.ctx, st.curator, subject) {
				seen[verb] = true
			}
		}
	}
	sweep()
	// The un-retirement is reachable only over a standing retirement
	// (plans/os-0d537fbd.md D3): retire one dead end and sweep again,
	// so the table names both directions of the curator's act.
	st.ctx = st.step(st.curator, v, curation.DeadEndRetireVerb, "c-1",
		`{"deadend": "`+cite("c-1", st.deadEnd1)+`", "environment": "moved", "reason": "the environment moved"}`)
	sweep()
	// The flywheel's proposal is reachable only over a recurring shape
	// (plans/os-9075c308.md D4), which this stand never forms: the
	// flywheel stand's reach joins the sweep so the table names it.
	for _, verb := range flywheelCuratorReach(t) {
		seen[verb] = true
	}
	for verb := range seen {
		if !named[verb] {
			t.Errorf("the curator can reach %s and the residual table does not name it", verb)
		}
	}
	for verb := range named {
		if !seen[verb] {
			t.Errorf("the residual table names %s and the curator cannot reach it", verb)
		}
	}
	// Before two observations stand, the proposal is not an affordance:
	// a fresh stand holds the curator and no observation yet.
	early := curationEarly(t)
	for _, verb := range Affordances(early.ctx, early.curator, "c-1") {
		if verb == curation.HypothesisVerb || verb == curation.ContestVerb {
			t.Fatalf("with nothing citable %s is invisible", verb)
		}
	}
}

// A well-shaped proposal raw-pushed by a key holding no curate passed
// no boundary, so it reserves nothing: the curator's proposal of the
// same claim still admits, the fold counts the raw one an anomaly and
// binds the promotion to the admitted position (review finding on the
// item 1 PR: the duplicate rule and the fold read admission, never
// presence).
func TestRawProposalReservesNoSubject(t *testing.T) {
	st := curationFixture(t)
	v := st.v
	good := st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2))
	rawPos := st.ctx.Count
	st.ctx = st.step(st.worker, v, curation.HypothesisVerb, st.id, good)
	if fold := curation.Fold(st.ctx.Records); len(fold.HypothesisIDs()) != 0 || fold.Anomalies != 1 {
		t.Fatalf("the raw proposal folds as an anomaly, never a hypothesis: %v %d", fold.HypothesisIDs(), fold.Anomalies)
	}
	if _, ok := curation.HypothesisValid(st.ctx.Records, st.ctx.Table, curation.Citation{Contract: st.id, Position: rawPos}); ok {
		t.Fatal("a proposal by a claim key is never an admitted hypothesis")
	}
	if err := Check(st.ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, good, st.ctx.Tip)); err != nil {
		t.Fatalf("the curator's proposal is no duplicate of a raw push: %v", err)
	}
	admitted := st.admitHypothesis(t)
	fold := curation.Fold(st.ctx.Records)
	h, ok := fold.Hypothesis(st.id)
	if !ok || h.Pos != admitted || h.Stage != curation.StageProposed || fold.Anomalies != 1 {
		t.Fatalf("the admitted proposal folds at its own position: %+v %d", h, fold.Anomalies)
	}
	if err := Check(st.ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, good, st.ctx.Tip)); err == nil || gate(t, err) != curation.GateSupportDuplicate || !strings.Contains(err.Error(), fmt.Sprintf("position %d", admitted)) {
		t.Fatalf("a re-proposal refuses against the admitted position, not the raw one: %v", err)
	}
	// The curator's own re-proposal, raw-pushed past the boundary,
	// is the duplicate the rule refused: it folds as an anomaly and a
	// promotion citing it re-judges the same duplicate.
	dupPos := st.ctx.Count
	st.ctx = st.step(st.curator, v, curation.HypothesisVerb, st.id, good)
	if fold := curation.Fold(st.ctx.Records); fold.Anomalies != 2 || len(fold.HypothesisIDs()) != 1 {
		t.Fatalf("the raw duplicate folds as an anomaly: %d %v", fold.Anomalies, fold.HypothesisIDs())
	}
	if _, ok := curation.HypothesisValid(st.ctx.Records, st.ctx.Table, curation.Citation{Contract: st.id, Position: dupPos}); ok {
		t.Fatal("a duplicate of an admitted proposal is not itself admitted")
	}
	anchor := curation.LessonsDir + "/retry.md @ 0123456"
	bound := st.evalRun(t, "eval-bound", cite(st.id, admitted), anchor, "pass", nil)
	for name, pos := range map[string]int{"raw": rawPos, "duplicate": dupPos} {
		if err := Check(st.ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, lessonBody(st.id, pos, "fix-the-check", bound), st.ctx.Tip)); err == nil || gate(t, err) != curation.GatePromotionHypothesis {
			t.Fatalf("a promotion citing the %s position refuses: %v", name, err)
		}
	}
	promotion := lessonBody(st.id, admitted, "fix-the-check", bound)
	if err := Check(st.ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, promotion, st.ctx.Tip)); err != nil {
		t.Fatalf("a promotion citing the admitted position admits: %v", err)
	}
	promoted := st.ctx.Count
	st.ctx = st.step(st.observer, v, curation.LessonVerb, st.id, promotion)
	fold = curation.Fold(st.ctx.Records)
	h, _ = fold.Hypothesis(st.id)
	if h == nil || h.Stage != curation.StagePromoted || h.Lesson == nil || *h.Lesson != promoted || len(fold.Lessons) != 1 || len(fold.Unbound) != 0 || fold.Anomalies != 2 {
		t.Fatalf("the promotion folds the admitted hypothesis to promoted: %+v %+v", h, fold.Unbound)
	}
	// The promoted lesson is a delivery candidate until a contest
	// lands on the admitted position; then the fold reads contested
	// and the candidate list drops it.
	if c := curation.Candidates(fold, st.ctx.Lifecycle, ""); len(c) != 1 || c[0].Pos != promoted {
		t.Fatalf("the promoted lesson is the one candidate: %+v", c)
	}
	contest := fmt.Sprintf(`{"hypothesis": %q, "evidence": [%q, %q], "reason": "it still failed"}`, cite(st.id, admitted), cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6))
	if err := Check(st.ctx, draftV(t, st.curator, v, curation.ContestVerb, st.id, contest, st.ctx.Tip)); err != nil {
		t.Fatalf("held-out evidence contests the promoted hypothesis: %v", err)
	}
	st.ctx = st.step(st.curator, v, curation.ContestVerb, st.id, contest)
	fold = curation.Fold(st.ctx.Records)
	if !fold.Contested(st.id) || len(curation.Candidates(fold, st.ctx.Lifecycle, "")) != 0 {
		t.Fatal("a contested hypothesis's lesson is no candidate")
	}
}

// A claim raw-pushed by a grantless key opens an apparent window in
// the lifecycle fold, and a dead end signed by that key inside it
// looks like the holder's own; neither passed a boundary, so the
// observation supports nothing (review finding on the item 1 PR: the
// window a citation rests on is re-judged, not read off the fold).
func TestObservationsInsideAGrantlessWindowSupportNothing(t *testing.T) {
	st := curationFixture(t)
	ctx, v := st.ctx, st.v
	rawClaim := ctx.Count
	ctx = st.step(st.stranger, v, "claim.taken", "c-4", `{}`)
	s, ok := ctx.Lifecycle.State("c-4")
	if !ok || s.Claim == nil || s.Claim.Holder != fpOf(t, st.stranger) || s.Claim.Fence != rawClaim {
		t.Fatalf("the lifecycle fold applies the raw claim whoever signed it: %+v", s.Claim)
	}
	if curation.WindowAdmitted(ctx.Records, s) {
		t.Fatal("a window opened by a grantless key is not admitted")
	}
	if held, _ := ctx.Lifecycle.State("c-1"); !curation.WindowAdmitted(ctx.Records, held) {
		t.Fatal("the worker's window on c-1 is admitted")
	}
	rawDeadEnd := ctx.Count
	ctx = st.step(st.stranger, v, curation.DeadEndVerb, "c-4", deadEndBody(fmt.Sprint(rawClaim)))
	if _, ok := curation.ObservationAt(ctx.Records, ctx.Table, curation.Citation{Contract: "c-4", Position: rawDeadEnd}); ok {
		t.Fatal("a dead end inside a grantless window is no observation")
	}
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-4", rawDeadEnd)), ctx.Tip)); err == nil || gate(t, err) != curation.GateSupportObservation || !strings.Contains(err.Error(), "not an admitted observation") {
		t.Fatalf("a proposal citing it refuses at the observation gate: %v", err)
	}
	if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)), ctx.Tip)); err != nil {
		t.Fatalf("the admitted observations still support: %v", err)
	}
}

// A failed contract stays failed under a raw pass: the verdict the
// support rule reads is the latest AUTHENTICATED one, and a pass
// signed by the implementer, or by a grantless key, authenticates
// nothing (review finding on the item 1 PR).
func TestARawPassClearsNoAuthenticFail(t *testing.T) {
	st := curationFixture(t)
	ctx, v := st.ctx, st.v
	if !curation.FailedAt(ctx.Records, ctx.Table, "c-3") || curation.FailedAt(ctx.Records, ctx.Table, "c-2") {
		t.Fatal("the verifier's fail stands on c-3 and nowhere else")
	}
	pass := `{"verdict": "pass", "receipt": "` + strings.Repeat("0", 64) + `", "submission": "` + fmt.Sprint(st.submission3) + `", "independence": "L1"}`
	for _, signer := range []ed25519.PrivateKey{st.worker, st.stranger} {
		ctx = st.step(signer, v, transition.VerdictRenderedVerb, "c-3", pass)
		if s, _ := ctx.Lifecycle.State("c-3"); s.Verdict == nil || s.Verdict.Verdict != "pass" || s.State != "review" {
			t.Fatalf("the lifecycle fold records the raw pass as the latest verdict: %+v", s.Verdict)
		}
		if !curation.FailedAt(ctx.Records, ctx.Table, "c-3") {
			t.Fatal("a raw pass clears nothing")
		}
		if err := Check(ctx, draftV(t, st.curator, v, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-3", st.submission3)), ctx.Tip)); err == nil || gate(t, err) != curation.GateSupportFailed {
			t.Fatalf("the failed trajectory still supports nothing: %v", err)
		}
	}
}

// A contest or a promotion pushed past the boundary binds nothing:
// the fold re-judges both at their own position through the same
// checks admission runs (review findings on the task PR), so a
// raw-pushed contest by a claim key, or by the curator citing the
// support set, moves nothing to contested and every delivery keeps the
// lesson; and a raw-pushed promotion refused at the adversarial gate,
// or signed by a key holding no observer grant, folds unbound and
// surfaces nowhere.
func TestRawPushedContestsAndPromotionsBindNothing(t *testing.T) {
	st := curationFixture(t)
	v := st.v
	hp, bound, _ := func() (int, int, string) {
		hp := st.admitHypothesis(t)
		anchor := curation.LessonsDir + "/retry.md @ 0123456"
		return hp, st.evalRun(t, "eval-bound", cite(st.id, hp), anchor, "pass", nil), anchor
	}()
	good := lessonBody(st.id, hp, "fix-the-check", bound)
	// A promotion the boundary refuses (a plain pass cited as the
	// adversarial evaluation), raw-pushed: unbound, never a candidate.
	plain := plainPass(t, st)
	smuggled := lessonBody(st.id, hp, "fix-the-check", plain)
	if err := Check(st.ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, smuggled, st.ctx.Tip)); err == nil || gate(t, err) != curation.GatePromotionAdversary {
		t.Fatalf("the boundary refuses the smuggled promotion: %v", err)
	}
	st.ctx = st.step(st.observer, v, curation.LessonVerb, st.id, smuggled)
	// A well-formed promotion signed by the curator, which holds no
	// observer grant, raw-pushed: unbound too.
	st.ctx = st.step(st.curator, v, curation.LessonVerb, st.id, good)
	fold := curation.Fold(st.ctx.Records)
	if len(fold.LessonsOf(st.id)) != 0 || len(fold.Unbound) != 2 || len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 0 {
		t.Fatalf("raw-pushed promotions fold unbound and are no candidates: %+v %+v", fold.LessonsOf(st.id), fold.Unbound)
	}
	if h, _ := fold.Hypothesis(st.id); h.Stage != curation.StageProposed {
		t.Fatalf("the hypothesis stays proposed: %+v", h)
	}
	// The legitimate promotion admits and binds.
	if err := Check(st.ctx, draftV(t, st.observer, v, curation.LessonVerb, st.id, good, st.ctx.Tip)); err != nil {
		t.Fatalf("the observer's promotion citing the survived eval admits: %v", err)
	}
	promoted := st.ctx.Count
	st.ctx = st.step(st.observer, v, curation.LessonVerb, st.id, good)
	fold = curation.Fold(st.ctx.Records)
	if h, _ := fold.Hypothesis(st.id); h.Stage != curation.StagePromoted || h.Lesson == nil || *h.Lesson != promoted || len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 1 {
		t.Fatalf("the admitted promotion binds and is the one candidate: %+v", h)
	}
	// Contests the boundary refuses, raw-pushed: by the worker citing
	// held-out evidence, and by the curator citing the support set.
	// Neither moves the stage, and the lesson keeps surfacing.
	heldOut := contestBody(st, hp, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6))
	var oog *OutOfGrantError
	if err := Check(st.ctx, draftV(t, st.worker, v, curation.ContestVerb, st.id, heldOut, st.ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a claim key cannot contest: %v", err)
	}
	st.ctx = st.step(st.worker, v, curation.ContestVerb, st.id, heldOut)
	fromSupport := contestBody(st, hp, cite("c-1", st.deadEnd1))
	if err := Check(st.ctx, draftV(t, st.curator, v, curation.ContestVerb, st.id, fromSupport, st.ctx.Tip)); err == nil || gate(t, err) != curation.GateContestHeldOut {
		t.Fatalf("support-set evidence refuses: %v", err)
	}
	st.ctx = st.step(st.curator, v, curation.ContestVerb, st.id, fromSupport)
	fold = curation.Fold(st.ctx.Records)
	if fold.Contested(st.id) || len(fold.Contests[st.id]) != 0 || fold.Anomalies != 2 || len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 1 {
		t.Fatalf("raw-pushed contests are anomalies and the lesson keeps surfacing: contested=%v anomalies=%d", fold.Contested(st.id), fold.Anomalies)
	}
	if _, ok := curation.ContestValid(st.ctx.Records, st.ctx.Table, st.ctx.Count-1); ok {
		t.Fatal("a contest citing the support set is never an admitted contest")
	}
	// The legitimate contest admits, moves the stage, and closes the
	// delivery.
	if err := Check(st.ctx, draftV(t, st.curator, v, curation.ContestVerb, st.id, heldOut, st.ctx.Tip)); err != nil {
		t.Fatalf("held-out evidence from curate contests: %v", err)
	}
	st.ctx = st.step(st.curator, v, curation.ContestVerb, st.id, heldOut)
	fold = curation.Fold(st.ctx.Records)
	if !fold.Contested(st.id) || len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 0 {
		t.Fatal("the admitted contest moves the stage and removes the lesson from delivery")
	}
	// Admission and the fold agree by construction: every promotion
	// and contest on the chain is admitted exactly when the fold binds
	// it.
	for pos, rec := range st.ctx.Records {
		switch rec.Event.Verb {
		case curation.LessonVerb:
			_, valid := curation.PromotionValid(st.ctx.Records, st.ctx.Table, pos)
			if valid != (pos == promoted) {
				t.Fatalf("promotion at %d: valid=%v", pos, valid)
			}
		case curation.ContestVerb:
			_, valid := curation.ContestValid(st.ctx.Records, st.ctx.Table, pos)
			if valid != (pos == st.ctx.Count-1) {
				t.Fatalf("contest at %d: valid=%v", pos, valid)
			}
		}
	}
}

// The promotion's pass authentication applies the level rule from
// seed/4 (plans/os-99829835.md D3) through the one implementation the
// verdict rule and the merge chain share, installed into the curation
// package at init: a pass recorded at a level the record does not
// support (L1 on an executable gated eval that supports L3), pushed
// past the verdict boundary, is not survival, and the same pass at the
// level the record supports is.
func TestPromotionPassAuthenticationAppliesTheLevelRule(t *testing.T) {
	if curation.PassLevelCheck == nil {
		t.Fatal("admit installs the level half of the pass authentication into curation at init")
	}
	st := curationFixture(t)
	hp := st.admitHypothesis(t)
	st.ctx = st.step(st.root, st.v, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	st.v = version.Seed4
	anchor := curation.LessonsDir + "/retry.md @ 0123456"
	st.level = "L1"
	short := st.evalRun(t, "eval-short", cite(st.id, hp), anchor, "pass", nil)
	if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", short), st.ctx.Tip)); err == nil || gate(t, err) != curation.GatePromotionAdversary {
		t.Fatalf("a pass recorded at L1 on a record that supports L3 is not survival: %v", err)
	}
	st.level = "L3"
	full := st.evalRun(t, "eval-full", cite(st.id, hp), anchor, "pass", nil)
	if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", full), st.ctx.Tip)); err != nil {
		t.Fatalf("the same pass at the level the record supports is survival: %v", err)
	}
	// The fold agrees: the level-short pass binds no promotion.
	st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", short))
	if fold := curation.Fold(st.ctx.Records); len(fold.LessonsOf(st.id)) != 0 || len(fold.Unbound) != 1 {
		t.Fatalf("a promotion citing the level-short pass folds unbound: %+v", fold.Unbound)
	}
}

// The fold's promotion replay never refolds: CheckPromotion reads the
// contested state from a position-accurate scan, so a chain carrying
// many promotions folds in linear passes rather than re-entering every
// earlier promotion's replay (review finding on the item 3 PR, where
// the refold was exponential in the promotions a chain carried).
func TestFoldingManyPromotionsNeverRefolds(t *testing.T) {
	st := curationFixture(t)
	hp := st.admitHypothesis(t)
	plain := plainPass(t, st)
	body := lessonBody(st.id, hp, "fix-the-check", plain)
	for i := 0; i < 24; i++ {
		st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, body)
	}
	done := make(chan *curation.State, 1)
	go func() { done <- curation.Fold(st.ctx.Records) }()
	select {
	case fold := <-done:
		if len(fold.Unbound) != 24 || len(fold.LessonsOf(st.id)) != 0 {
			t.Fatalf("twenty-four raw promotions fold unbound: %d unbound", len(fold.Unbound))
		}
	case <-time.After(20 * time.Second):
		t.Fatal("folding twenty-four promotions did not finish in twenty seconds: the replay refolds")
	}
}

// contestBody is a contest of the stand's hypothesis citing the given
// evidence.
func contestBody(st *curationStand, hp int, evidence ...string) string {
	q := make([]string, len(evidence))
	for i, e := range evidence {
		q[i] = fmt.Sprintf("%q", e)
	}
	return fmt.Sprintf(`{"hypothesis": "%s", "evidence": [%s], "reason": "the mirror was warm and it still failed"}`, cite(st.id, hp), strings.Join(q, ", "))
}

// plainPass works an ordinary contract to an authenticated pass: a
// verdict with no eval marker at all.
func plainPass(t *testing.T, st *curationStand) int {
	t.Helper()
	v := st.v
	st.ctx = st.step(st.root, v, "intent.filed", "c-plain", trivialFiling)
	st.ctx = st.step(st.root, v, "contract.specified", "c-plain", specBody)
	st.ctx = st.step(st.worker2, v, "claim.taken", "c-plain", `{}`)
	sub := st.ctx.Count
	st.ctx = st.step(st.worker2, v, "submission.made", "c-plain", `{"fence": "`+activeFence(t, st.ctx, "c-plain")+`", "packet": `+findingPacket+`}`)
	pos := st.ctx.Count
	st.ctx = st.step(st.verifier, v, "verdict.rendered", "c-plain", fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("0", 64), sub))
	return pos
}

// curationEarly is the stand before any contract: the lanes enrolled
// and granted, nothing to cite.
func curationEarly(t *testing.T) *curationStand {
	t.Helper()
	store, resolve, root := seededStore(t)
	st := &curationStand{root: root, curator: fixtureKey(t, 12)}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range []ed25519.PrivateKey{root, st.curator} {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	appendSignedV(t, store, loose, root, version.Seed1, keyring.VerbEnrolled, fpOf(t, st.curator), enrollBody(t, st.curator, "agent", "curator"))
	appendSignedV(t, store, loose, root, version.Seed1, keyring.VerbGranted, fpOf(t, st.curator), `{"capability": "`+keyring.CapCurate+`"}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	st.ctx = ctx
	return st
}

func loadResidualsFrom(t *testing.T, name string) []residual {
	t.Helper()
	b, err := readInjectionFile(t, name)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Residuals []residual `json:"residuals"`
	}
	if err := jsonUnmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Residuals) == 0 {
		t.Fatalf("%s is empty: the reachable set would pass vacuously", name)
	}
	return doc.Residuals
}
