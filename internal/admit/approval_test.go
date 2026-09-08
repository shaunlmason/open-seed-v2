package admit

// The require-approval mode (plans/os-5781a026.md D1 to D4, D7;
// charter §II.14, III.L row 4): the declaration names the verbs, the
// kinds and the floor; a governed act refuses until the operator's
// grant stands and spends that grant; the three verbs' shapes and
// citations hold regardless of declaration; the affordances list a
// governed act exactly while an open grant stands.

import (
	"crypto/ed25519"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

type keyForType = ed25519.PrivateKey

type stepFunc = func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context

func approvalPos(n int) string { return strconv.Itoa(n) }

const approvalDeclaration = `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "min_tier": "standard"}]}}`

const standardBody = `{"intent": "the middle one", "tier": "standard", "budget": "small", "routing": "core"}`

// approvalStand is grantFixture with a human claimant enrolled, every
// claimant granted claim, and c-1 (standard) and c-2 (trivial)
// specified.
func approvalStand(t *testing.T) (ctx *Context, signer, worker, maintainer, human keyForType, step stepFunc) {
	t.Helper()
	var s stepFunc
	ctx, signer, worker, maintainer, s = grantFixture(t)
	step = s
	human = fixtureKey(t, 7)
	ctx = step(signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, human), enrollBody(t, human, "human", "alice"))
	for _, k := range []keyForType{worker, maintainer, human} {
		ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, k), `{"capability": "`+keyring.CapClaim+`"}`)
	}
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", standardBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(signer, version.Seed1, "intent.filed", "c-2", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-2", specBody)
	return
}

func under(ctx *Context, declaration string, t *testing.T) *Context {
	with := *ctx
	with.Declaration = declared(t, declaration)
	return &with
}

func requestBody(t *testing.T, actor keyForType, verb string) string {
	return `{"verb": "` + verb + `", "actor": "` + fpOf(t, actor) + `", "reason": "the drill asks"}`
}

// conformance: III.L row 4 (require-approval), AC1 and AC2 — under a
// declaration naming claim.taken at the standard floor, an agent's
// claim on a standard contract refuses approval_required naming the
// request to file; a human's, a trivial contract's and an undeclared
// deployment's admit as today; the agent's request lands on standing
// alone; a claim-granted key's grant is out of grant; the operator's
// grant admits the act; the act spends the grant so the next refuses
// again; a denied request admits nothing.
func TestGovernedActNeedsAnOpenGrantAndSpendsIt(t *testing.T) {
	ctx, signer, worker, maintainer, human, step := approvalStand(t)
	take := func(c *Context, key keyForType, subject string) error {
		return Check(c, draftV(t, key, version.Seed1, "claim.taken", subject, `{}`, c.Tip))
	}
	with := under(ctx, approvalDeclaration, t)
	var need *ApprovalRequiredError
	err := take(with, worker, "c-1")
	if !errors.As(err, &need) || need.Kind != "agent" || need.Tier != "standard" || need.MinTier != "standard" || need.Pending != nil {
		t.Fatalf("an agent's governed act refuses naming kind, tier and floor: %v", err)
	}
	if !strings.Contains(err.Error(), "seed approval request --subject c-1 --verb claim.taken") {
		t.Fatalf("the refusal names the request to file: %v", err)
	}
	if err := take(with, maintainer, "c-1"); !errors.As(err, &need) || need.Kind != "service" {
		t.Fatalf("a service key is governed like an agent: %v", err)
	}
	if err := take(with, human, "c-1"); err != nil {
		t.Fatalf("a human key is not governed by an entry naming no kinds: %v", err)
	}
	if err := take(with, worker, "c-2"); err != nil {
		t.Fatalf("a trivial contract is under the standard floor: %v", err)
	}
	if err := take(ctx, worker, "c-1"); err != nil {
		t.Fatalf("with no declaration nothing needs an approval: %v", err)
	}
	if err := take(under(ctx, `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "kinds": ["human"]}]}}`, t), worker, "c-1"); err != nil {
		t.Fatalf("an entry naming human reaches no agent: %v", err)
	}
	if err := take(under(ctx, `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "kinds": ["human"]}]}}`, t), human, "c-1"); !errors.As(err, &need) || need.Kind != "human" {
		t.Fatalf("an entry naming human reaches the human: %v", err)
	}
	if err := take(under(ctx, `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "min_tier": "standrad"}]}}`, t), worker, "c-2"); !errors.As(err, &need) {
		t.Fatalf("a floor outside the vocabulary fails closed: %v", err)
	}
	// By actor: an entry naming one fingerprint reaches that key and
	// no other of its kind; named beside kinds, it reaches both.
	byActor := `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "actors": ["` + fpOf(t, worker) + `"]}]}}`
	if err := take(under(ctx, byActor, t), worker, "c-2"); !errors.As(err, &need) {
		t.Fatalf("the named agent needs an approval on every tier: %v", err)
	}
	if err := take(under(ctx, byActor, t), maintainer, "c-1"); err != nil {
		t.Fatalf("another key of the same kind is not named: %v", err)
	}
	both := `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "actors": ["` + fpOf(t, human) + `"], "kinds": ["service"]}]}}`
	if err := take(under(ctx, both, t), human, "c-1"); !errors.As(err, &need) || need.Kind != "human" {
		t.Fatalf("a named human is reached by its fingerprint: %v", err)
	}
	if err := take(under(ctx, both, t), maintainer, "c-1"); !errors.As(err, &need) {
		t.Fatalf("the named kind is reached beside the named actor: %v", err)
	}
	if err := take(under(ctx, both, t), worker, "c-1"); err != nil {
		t.Fatalf("an unnamed agent is reached by neither selector: %v", err)
	}

	// The request: standing only, on the contract, naming the actor.
	// The operator's grant admits; a claim key's does not.
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "c-1", requestBody(t, worker, "claim.taken"))
	requested := ctx.Count - 1
	with = under(ctx, approvalDeclaration, t)
	if err := take(with, worker, "c-1"); !errors.As(err, &need) || need.Pending == nil || *need.Pending != requested {
		t.Fatalf("a pending request names the operator's turn: %v", err)
	}
	grant := `{"request": "` + approvalPos(requested) + `"}`
	var oog *OutOfGrantError
	if err := Check(with, draftV(t, worker, version.Seed1, approval.GrantedVerb, "c-1", grant, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a claim-granted key grants nothing: %v", err)
	}
	if err := Check(with, draftV(t, signer, version.Seed1, approval.GrantedVerb, "c-1", grant, ctx.Tip)); err != nil {
		t.Fatalf("the operator grants: %v", err)
	}
	ctx = step(signer, version.Seed1, approval.GrantedVerb, "c-1", grant)
	with = under(ctx, approvalDeclaration, t)
	if err := take(with, worker, "c-1"); err != nil {
		t.Fatalf("the granted act admits: %v", err)
	}
	if err := take(with, maintainer, "c-1"); !errors.As(err, &need) {
		t.Fatalf("the grant names one actor: %v", err)
	}
	// The act spends the grant: after the window closes, the same act
	// refuses again.
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	ctx = step(worker, version.Seed1, "claim.released", "c-1", fencedExit(t, ctx, "c-1"))
	with = under(ctx, approvalDeclaration, t)
	if err := take(with, worker, "c-1"); !errors.As(err, &need) || need.Pending != nil {
		t.Fatalf("one grant admits one act: %v", err)
	}
	// A denied request admits nothing, and a second answer refuses.
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "c-1", requestBody(t, worker, "claim.taken"))
	denied := ctx.Count - 1
	ctx = step(signer, version.Seed1, approval.DeniedVerb, "c-1", `{"request": "`+approvalPos(denied)+`", "reason": "not now"}`)
	with = under(ctx, approvalDeclaration, t)
	if err := take(with, worker, "c-1"); !errors.As(err, &need) || need.Pending != nil {
		t.Fatalf("a denial admits nothing: %v", err)
	}
	var apr *approval.Error
	if err := Check(with, draftV(t, signer, version.Seed1, approval.GrantedVerb, "c-1", `{"request": "`+approvalPos(denied)+`"}`, ctx.Tip)); !errors.As(err, &apr) || !strings.Contains(err.Error(), "answered once") {
		t.Fatalf("a request is answered once: %v", err)
	}
	// The laundering countermeasure (memory/LEARNINGS.md; D4): a
	// well-shaped grant raw-pushed by a key without operator standing
	// cites a real open request, folds as a fact, and authorizes
	// nothing: the act still refuses and the affordances do not list
	// it. The operator's grant of the same request then admits.
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "c-1", requestBody(t, worker, "claim.taken"))
	laundered := ctx.Count - 1
	ctx = step(worker, version.Seed1, approval.GrantedVerb, "c-1", `{"request": "`+approvalPos(laundered)+`"}`)
	with = under(ctx, approvalDeclaration, t)
	if g, open := ctx.Lifecycle.OpenApproval("c-1", "claim.taken", fpOf(t, worker)); !open || g.Answerer != fpOf(t, worker) {
		t.Fatalf("the tolerant fold keeps the raw grant as a fact: %+v %v", g, open)
	}
	if ApprovalValid(ctx.Records, mustOpen(t, ctx, "c-1", fpOf(t, worker))) {
		t.Fatal("a grant signed without operator standing is not valid")
	}
	if err := take(with, worker, "c-1"); !errors.As(err, &need) {
		t.Fatalf("a laundered grant admits nothing: %v", err)
	}
	for _, v := range Affordances(with, worker, "c-1") {
		if v == "claim.taken" {
			t.Fatal("a laundered grant lists nothing")
		}
	}
	// The request the raw grant answered is answered in the fold, so
	// the operator files a fresh one on the actor's behalf and grants
	// it; the valid grant admits.
	ctx = step(signer, version.Seed1, approval.RequestedVerb, "c-1", requestBody(t, worker, "claim.taken"))
	fresh := ctx.Count - 1
	ctx = step(signer, version.Seed1, approval.GrantedVerb, "c-1", `{"request": "`+approvalPos(fresh)+`"}`)
	with = under(ctx, approvalDeclaration, t)
	if !ApprovalValid(ctx.Records, mustOpenAt(t, ctx, fresh)) {
		t.Fatal("the operator's grant is valid")
	}
	if err := take(with, worker, "c-1"); err != nil {
		t.Fatalf("the operator's grant admits beside the laundered one: %v", err)
	}
	// Policy, not validity: the chain that carries every act above
	// builds a context under the declaration.
	if built, err := ContextOver(ctx.Records, WithDeclaration(declared(t, approvalDeclaration))); err != nil || built.Declaration == nil {
		t.Fatalf("a declaration never makes a chain fail to build: %v", err)
	}
}

// conformance: plans/os-5781a026.md D2, D4 — the shapes and citations
// hold with or without a declaration: a request naming no catalog
// verb, an approval verb or an unenrolled actor refuses; a request on
// a contract the chain does not know refuses and one on system
// admits; a grant citing nothing, citing another subject's request or
// carrying a reason refuses; a denial without a reason refuses.
func TestApprovalShapesAndCitationsHoldRegardlessOfDeclaration(t *testing.T) {
	ctx, signer, worker, _, _, step := approvalStand(t)
	var apr *approval.Error
	refuses := func(name string, key keyForType, verb, subject, payload string) {
		t.Helper()
		if err := Check(ctx, draftV(t, key, version.Seed1, verb, subject, payload, ctx.Tip)); !errors.As(err, &apr) {
			t.Fatalf("%s must refuse as approval_refused, got %v", name, err)
		}
	}
	refuses("no catalog verb", worker, approval.RequestedVerb, "c-1", `{"verb": "claim.wished", "actor": "`+fpOf(t, worker)+`", "reason": "r"}`)
	refuses("an approval verb", worker, approval.RequestedVerb, "c-1", requestBody(t, worker, approval.GrantedVerb))
	refuses("an unenrolled actor", worker, approval.RequestedVerb, "c-1", `{"verb": "claim.taken", "actor": "fp-nobody", "reason": "r"}`)
	refuses("an unknown contract", worker, approval.RequestedVerb, "c-9", requestBody(t, worker, "claim.taken"))
	refuses("a grant citing nothing", signer, approval.GrantedVerb, "c-1", `{"request": "0"}`)
	refuses("a denial without a reason", signer, approval.DeniedVerb, "c-1", `{"request": "0"}`)
	if err := Check(ctx, draftV(t, worker, version.Seed1, approval.RequestedVerb, "system", requestBody(t, worker, "system.checkpoint"), ctx.Tip)); err != nil {
		t.Fatalf("a request on system admits: %v", err)
	}
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "system", requestBody(t, worker, "system.checkpoint"))
	pos := approvalPos(ctx.Count - 1)
	refuses("a grant on another subject", signer, approval.GrantedVerb, "c-1", `{"request": "`+pos+`"}`)
	refuses("a grant with a reason", signer, approval.GrantedVerb, "system", `{"request": "`+pos+`", "reason": "why"}`)
	if err := Check(ctx, draftV(t, signer, version.Seed1, approval.GrantedVerb, "system", `{"request": "`+pos+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("the grant on the request's subject admits: %v", err)
	}
	if err := Check(ctx, draftV(t, signer, version.Seed1, approval.DeniedVerb, "system", `{"request": "`+pos+`", "reason": "no"}`, ctx.Tip)); err != nil {
		t.Fatalf("the denial admits: %v", err)
	}
}

// conformance: plans/os-5781a026.md D7, AC3 — the affordances draft the
// three verbs where they are legal and list a governed act for its
// actor exactly while an open grant stands: the request for any
// standing key on a known subject, the answers for the operator
// while a request is pending, the governed act after the grant and
// not after it is spent.
func TestAffordancesListAGovernedActExactlyUnderAnOpenGrant(t *testing.T) {
	ctx, signer, worker, _, _, step := approvalStand(t)
	has := func(list []string, verb string) bool {
		for _, v := range list {
			if v == verb {
				return true
			}
		}
		return false
	}
	with := under(ctx, approvalDeclaration, t)
	if l := Affordances(with, worker, "c-1"); has(l, "claim.taken") || !has(l, approval.RequestedVerb) || has(l, approval.GrantedVerb) {
		t.Fatalf("before a grant the agent lists the request and not the act: %v", l)
	}
	if l := Affordances(with, signer, "c-1"); has(l, approval.GrantedVerb) || has(l, approval.DeniedVerb) {
		t.Fatalf("with nothing pending the operator lists no answer: %v", l)
	}
	if l := Affordances(with, worker, "c-9"); has(l, approval.RequestedVerb) {
		t.Fatalf("no request on a subject the chain does not know: %v", l)
	}
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "c-1", requestBody(t, worker, "claim.taken"))
	with = under(ctx, approvalDeclaration, t)
	if l := Affordances(with, signer, "c-1"); !has(l, approval.GrantedVerb) || !has(l, approval.DeniedVerb) {
		t.Fatalf("a pending request lists both answers for the operator: %v", l)
	}
	if l := Affordances(with, worker, "c-1"); has(l, approval.GrantedVerb) || has(l, "claim.taken") {
		t.Fatalf("the agent answers nothing and still may not act: %v", l)
	}
	ctx = step(signer, version.Seed1, approval.GrantedVerb, "c-1", `{"request": "`+approvalPos(ctx.Count-1)+`"}`)
	with = under(ctx, approvalDeclaration, t)
	if l := Affordances(with, worker, "c-1"); !has(l, "claim.taken") {
		t.Fatalf("under an open grant the agent lists the act: %v", l)
	}
	if l := Affordances(with, signer, "c-1"); has(l, approval.GrantedVerb) {
		t.Fatalf("an answered request lists no second answer: %v", l)
	}
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	ctx = step(worker, version.Seed1, "claim.released", "c-1", fencedExit(t, ctx, "c-1"))
	with = under(ctx, approvalDeclaration, t)
	if l := Affordances(with, worker, "c-1"); has(l, "claim.taken") {
		t.Fatalf("a spent grant lists the act no more: %v", l)
	}
	if l := Affordances(ctx, worker, "c-1"); !has(l, "claim.taken") {
		t.Fatalf("with no declaration the act lists as today: %v", l)
	}
}

// conformance: plans/os-5781a026.md D2, D4 — a governed contract birth
// can be approved: the request is on the contract the birth creates,
// which no chain yet knows, so its subject check allows the unknown id
// when the requested verb is intent.filed (review on the task PR).
// The birth then needs the open grant like every governed act.
func TestApprovalRequestOnABirthAdmitsAndTheGrantAdmitsTheFiling(t *testing.T) {
	ctx, signer, worker, _, _, step := approvalStand(t)
	birthDeclaration := `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "intent.filed", "min_tier": "trivial"}]}}`
	// intent.filed accepts dispatch or operator; the worker holds neither
	// past claim in the fixture, so it is granted dispatch here.
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapDispatch+`"}`)
	// The request names a contract the chain does not know, for the
	// verb that would create it: the birth's own subject check admits
	// it while a non-birth request on an unknown contract still refuses.
	if err := Check(under(ctx, birthDeclaration, t), draftV(t, worker, version.Seed1, approval.RequestedVerb, "c-9", requestBody(t, worker, "intent.filed"), ctx.Tip)); err != nil {
		t.Fatalf("a birth's approval request is on the contract it creates: %v", err)
	}
	var apr *approval.Error
	if err := Check(under(ctx, birthDeclaration, t), draftV(t, worker, version.Seed1, approval.RequestedVerb, "c-9", requestBody(t, worker, "claim.taken"), ctx.Tip)); !errors.As(err, &apr) || !strings.Contains(err.Error(), "no contract by that id") {
		t.Fatalf("a non-birth request still refuses on an unknown contract: %v", err)
	}
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "c-9", requestBody(t, worker, "intent.filed"))
	requested := ctx.Count - 1
	with := under(ctx, birthDeclaration, t)
	// Until the grant stands, the filing itself refuses naming the
	// request the operator must answer.
	var need *ApprovalRequiredError
	if err := Check(with, draftV(t, worker, version.Seed1, "intent.filed", "c-9", standardBody, ctx.Tip)); !errors.As(err, &need) || need.Pending == nil || *need.Pending != requested {
		t.Fatalf("the birth refuses approval_required until its grant stands: %v", err)
	}
	grant := `{"request": "` + approvalPos(requested) + `"}`
	ctx = step(signer, version.Seed1, approval.GrantedVerb, "c-9", grant)
	with = under(ctx, birthDeclaration, t)
	if err := Check(with, draftV(t, worker, version.Seed1, "intent.filed", "c-9", standardBody, ctx.Tip)); err != nil {
		t.Fatalf("the operator's grant admits the birth: %v", err)
	}
}

func mustOpen(t *testing.T, ctx *Context, subject, actor string) transition.ApprovalFact {
	t.Helper()
	g, ok := ctx.Lifecycle.OpenApproval(subject, "claim.taken", actor)
	if !ok {
		t.Fatal("no open grant")
	}
	return g
}

func mustOpenAt(t *testing.T, ctx *Context, pos int) transition.ApprovalFact {
	t.Helper()
	g, ok := ctx.Lifecycle.ApprovalAt(pos)
	if !ok || !g.Open() {
		t.Fatalf("no open grant at %d", pos)
	}
	return g
}

// conformance: plans/os-5781a026.md D1 (a birth's tier is its filing's),
// AC1 and AC2; review finding on #331: beside the drill above, a
// governed birth reads the floor from its own filing, so a standard
// filing refuses naming the request on the contract it would create
// while a trivial one is under the floor and admits with no grant; the
// operator's grant on the unborn contract is valid, admits the filing
// on that subject and no other, and is spent by the filing at its own
// position, which bears the contract into the lifecycle.
func TestGovernedBirthReadsItsFloorAndSpendsItsGrant(t *testing.T) {
	ctx, signer, worker, _, _, step := approvalStand(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapDispatch+`"}`)
	const flooredBirth = `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "intent.filed", "min_tier": "standard"}]}}`
	file := func(c *Context, key keyForType, subject, body string) error {
		return Check(c, draftV(t, key, version.Seed1, "intent.filed", subject, body, c.Tip))
	}
	with := under(ctx, flooredBirth, t)
	var need *ApprovalRequiredError
	err := file(with, worker, "c-3", standardBody)
	if !errors.As(err, &need) || need.Tier != "standard" || need.MinTier != "standard" || need.Pending != nil {
		t.Fatalf("a governed birth refuses reading the tier from its filing: %v", err)
	}
	if !strings.Contains(err.Error(), "seed approval request --subject c-3 --verb intent.filed") {
		t.Fatalf("the refusal names the request on the contract the filing creates: %v", err)
	}
	if err := file(with, worker, "c-4", filedBody); err != nil {
		t.Fatalf("a trivial birth is under the standard floor: %v", err)
	}
	ctx = step(worker, version.Seed1, approval.RequestedVerb, "c-3", requestBody(t, worker, "intent.filed"))
	requested := ctx.Count - 1
	ctx = step(signer, version.Seed1, approval.GrantedVerb, "c-3", `{"request": "`+approvalPos(requested)+`"}`)
	with = under(ctx, flooredBirth, t)
	if !ApprovalValid(ctx.Records, mustOpenAt(t, ctx, requested)) {
		t.Fatal("the operator's grant of a birth is valid")
	}
	if err := file(with, worker, "c-5", standardBody); !errors.As(err, &need) {
		t.Fatalf("the grant names one subject: %v", err)
	}
	if err := file(with, worker, "c-3", standardBody); err != nil {
		t.Fatalf("the granted birth admits: %v", err)
	}
	ctx = step(worker, version.Seed1, "intent.filed", "c-3", standardBody)
	if a, _ := ctx.Lifecycle.ApprovalAt(requested); a.ConsumedAt == nil || *a.ConsumedAt != ctx.Count-1 || a.Open() {
		t.Fatalf("the birth spends the grant at its own position: %+v", a)
	}
	if _, born := ctx.Lifecycle.State("c-3"); !born {
		t.Fatal("the governed birth is on the chain")
	}
}
