package admit

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	question    = `{"question": "which base?", "options": [{"id": "a", "choice": "main"}, {"id": "b", "choice": "release"}]}`
	goodPacket  = `{"acceptance": ["ship it"], "decisions": [], "base": "0000000000000000000000000000000000000000..0000000000000000000000000000000000000000", "refs": [], "findings": []}`
	raiseBody   = `{"packet": ` + goodPacket + `, "escalation": ` + question + `}`
	specifyBody = `{"acceptance": {"ref": "accept.md @ 0000000000000000000000000000000000000000", "executable": false}}`
)

// escalationFixture readies c-1 with a dispatch lane, a claim lane and
// a verdict lane, so each row of the capability table can be exercised
// by a key that really holds it rather than by an unenrolled one.
func escalationFixture(t *testing.T) (*Context, ed25519.PrivateKey, ed25519.PrivateKey, ed25519.PrivateKey,
	func(ed25519.PrivateKey, string, string, string, string) *Context) {
	t.Helper()
	ctx, signer, worker, maintainer, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, maintainer), `{"capability": "`+keyring.CapDispatch+`"}`)
	ctx = step(maintainer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(maintainer, version.Seed1, "contract.specified", "c-1", specifyBody)
	_ = ctx
	return ctx, signer, worker, maintainer, step
}

// conformance: III — a raised subject is distinguishable from an
// ordinarily blocked one. Asserted in BOTH directions, so the fact
// cannot pass by being set always.
func TestARaiseStandsAndAPlainBlockDoesNot(t *testing.T) {
	ctx, _, _, maintainer, step := escalationFixture(t)
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "contract.blocked", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("a plain block is legal: %v", err)
	}
	ctx = step(maintainer, version.Seed1, "contract.blocked", "c-1", `{}`)
	if s, _ := ctx.Lifecycle.State("c-1"); s.Escalation != nil {
		t.Fatal("a plain block raises no question")
	}
	// Unblocking a plainly blocked contract is legal: the lockout must
	// not fire on a subject nobody asked a question about.
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "contract.unblocked", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("no question stands, so the machine exit is open: %v", err)
	}
	ctx = step(maintainer, version.Seed1, "contract.unblocked", "c-1", `{}`)

	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody)
	s, _ := ctx.Lifecycle.State("c-1")
	if s.Escalation == nil {
		t.Fatal("the raise must stand as a fact")
	}
	if s.Escalation.Question != "which base?" || len(s.Escalation.Options) != 2 {
		t.Fatalf("the fact carries the question verbatim: %+v", s.Escalation)
	}
	if s.Escalation.TS == "" {
		t.Fatal("the fact carries the raising event's ts: age is elapsed time, and a position measures nothing")
	}
	if s.State != "blocked" {
		t.Fatalf("a raise blocks: %q", s.State)
	}
}

// The lockout: while a question stands, the machine-held exit refuses
// and the refusal names what IS legal.
func TestAStandingQuestionLocksTheMachineExit(t *testing.T) {
	ctx, _, _, maintainer, step := escalationFixture(t)
	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody)
	err := Check(ctx, draftV(t, maintainer, version.Seed1, "contract.unblocked", "c-1", `{}`, ctx.Tip))
	var ee *EscalationError
	if !errors.As(err, &ee) {
		t.Fatalf("contract.unblocked must refuse while a question stands, got %v", err)
	}
	for _, want := range []string{"decision.recorded", "contract.cancelled"} {
		if !strings.Contains(ee.Reason, want) {
			t.Errorf("the refusal names what IS legal (%s): %v", want, ee)
		}
	}
}

// Cancelling stays legal, because a lockout that traps the contract is
// a worse failure than the one it prevents — but it must cite the
// question it answers, or the obligation vanishes with no record.
func TestCancellingAnswersTheQuestionAndMustSaySo(t *testing.T) {
	ctx, signer, _, maintainer, step := escalationFixture(t)
	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody)
	s, _ := ctx.Lifecycle.State("c-1")
	pos := s.Escalation.Pos

	err := Check(ctx, draftV(t, signer, version.Seed1, "contract.cancelled", "c-1", `{}`, ctx.Tip))
	var ee *EscalationError
	if !errors.As(err, &ee) {
		t.Fatalf("an uncited cancel must refuse, got %v", err)
	}
	if !strings.Contains(ee.Reason, "no record of what closed it") {
		t.Errorf("the refusal must say why the citation matters: %v", ee)
	}
	cite := `{"escalation": "` + itoa(pos) + `"}`
	if err := Check(ctx, draftV(t, signer, version.Seed1, "contract.cancelled", "c-1", cite, ctx.Tip)); err != nil {
		t.Fatalf("a citing cancel is legal — the contract is never trapped: %v", err)
	}
	ctx = step(signer, version.Seed1, "contract.cancelled", "c-1", cite)
	if s, _ := ctx.Lifecycle.State("c-1"); s.Escalation != nil {
		t.Fatal("cancelling IS an answer: the question must not stand after it")
	}
}

// The answer: operator only, citing the standing question, choosing
// from its own set.
func TestTheAnswerIsOperatorOnlyAndCitesWhatStands(t *testing.T) {
	ctx, signer, _, maintainer, step := escalationFixture(t)
	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody)
	s, _ := ctx.Lifecycle.State("c-1")
	answer := `{"escalation": "` + itoa(s.Escalation.Pos) + `", "choice": "a"}`

	// The whole of D2: a dispatch key holds a real capability and
	// still cannot answer, so the refusal is attributable to the row
	// rather than to standing.
	err := Check(ctx, draftV(t, maintainer, version.Seed1, "decision.recorded", "c-1", answer, ctx.Tip))
	var oog *OutOfGrantError
	if !errors.As(err, &oog) {
		t.Fatalf("a machine lane must not answer a human gate, got %v", err)
	}
	if err := Check(ctx, draftV(t, signer, version.Seed1, "decision.recorded", "c-1", answer, ctx.Tip)); err != nil {
		t.Fatalf("the operator answers: %v", err)
	}

	var ee *EscalationError
	stale := `{"escalation": "999", "choice": "a"}`
	if err := Check(ctx, draftV(t, signer, version.Seed1, "decision.recorded", "c-1", stale, ctx.Tip)); !errors.As(err, &ee) {
		t.Fatalf("an answer to a question nobody asked must refuse, got %v", err)
	}
	unoffered := `{"escalation": "` + itoa(s.Escalation.Pos) + `", "choice": "z"}`
	err = Check(ctx, draftV(t, signer, version.Seed1, "decision.recorded", "c-1", unoffered, ctx.Tip))
	if !errors.As(err, &ee) {
		t.Fatalf("a choice outside the set must refuse, got %v", err)
	}
	if !strings.Contains(ee.Reason, "a, b") {
		t.Errorf("the refusal names the offered ids: %v", ee)
	}

	ctx = step(signer, version.Seed1, "decision.recorded", "c-1", answer)
	after, _ := ctx.Lifecycle.State("c-1")
	if after.Escalation != nil {
		t.Fatal("the answer clears the question")
	}
	if after.State != "ready" {
		t.Fatalf("answering returns the subject to the queue: %q", after.State)
	}
}

// An answer with no standing question refuses, so decision.recorded
// cannot be used as a general unblock.
func TestTheAnswerNeedsAQuestion(t *testing.T) {
	ctx, signer, _, maintainer, step := escalationFixture(t)
	ctx = step(maintainer, version.Seed1, "contract.blocked", "c-1", `{}`)
	err := Check(ctx, draftV(t, signer, version.Seed1, "decision.recorded", "c-1",
		`{"escalation": "3", "choice": "a"}`, ctx.Tip))
	var ee *EscalationError
	if !errors.As(err, &ee) {
		t.Fatalf("an answer with nothing to answer must refuse, got %v", err)
	}
}

// A raise that asks nothing blocks the contract for no one to answer,
// and a malformed question refuses by part.
func TestARaiseMustCarryAWellShapedQuestion(t *testing.T) {
	ctx, _, _, maintainer, _ := escalationFixture(t)
	var ee *EscalationError
	err := Check(ctx, draftV(t, maintainer, version.Seed1, "escalation.raised", "c-1",
		`{"packet": `+goodPacket+`}`, ctx.Tip))
	if !errors.As(err, &ee) {
		t.Fatalf("a raise with no question must refuse, got %v", err)
	}
	one := `{"packet": ` + goodPacket + `, "escalation": {"question": "q?", "options": [{"id": "a", "choice": "x"}]}}`
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "escalation.raised", "c-1", one, ctx.Tip)); err == nil {
		t.Fatal("a one-option question must refuse: it is not a decision")
	}
	// And the packet obligation is the charter's, not optional: an
	// escalation carries the packet, the question and the decision.
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "escalation.raised", "c-1",
		`{"escalation": `+question+`}`, ctx.Tip)); err == nil {
		t.Fatal("a raise without a packet must refuse")
	}
}

// D1, enforced rather than described: the new verb cannot leave
// in_progress, so the four deliberate exits stay exactly four.
func TestTheRaiseCannotLeaveInProgress(t *testing.T) {
	ctx, _, worker, _, step := escalationFixture(t)
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	if s, _ := ctx.Lifecycle.State("c-1"); s.State != "in_progress" {
		t.Fatalf("the drill needs a held subject: %q", s.State)
	}
	err := Check(ctx, draftV(t, worker, version.Seed1, "escalation.raised", "c-1", raiseBody, ctx.Tip))
	if err == nil {
		t.Fatal("escalation.raised must refuse from in_progress: nothing new may leave it")
	}
	if strings.Contains(err.Error(), "escalation on c-1") {
		t.Fatalf("it must refuse at the LIFECYCLE table, not the escalation rule: %v", err)
	}
}

// A question may ride the one exit that also asks something, and the
// fact it leaves is the same fact a standalone raise leaves.
func TestAParkMayCarryAQuestion(t *testing.T) {
	ctx, _, worker, _, step := escalationFixture(t)
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	s, _ := ctx.Lifecycle.State("c-1")
	fence := itoa(s.Claim.Fence)
	park := `{"fence": "` + fence + `", "packet": ` + goodPacket + `, "escalation": ` + question + `}`
	if err := Check(ctx, draftV(t, worker, version.Seed1, "claim.parked", "c-1", park, ctx.Tip)); err != nil {
		t.Fatalf("a park may carry a question: %v", err)
	}
	ctx = step(worker, version.Seed1, "claim.parked", "c-1", park)
	after, _ := ctx.Lifecycle.State("c-1")
	if after.Escalation == nil || after.Escalation.Question != "which base?" {
		t.Fatalf("the park's question stands like any other: %+v", after.Escalation)
	}
	// A plain park raises nothing, so the kind cannot pass by always
	// being set.
	ctx2, _, worker2, _, step2 := escalationFixture(t)
	ctx2 = step2(worker2, version.Seed1, "claim.taken", "c-1", `{}`)
	s2, _ := ctx2.Lifecycle.State("c-1")
	plain := `{"fence": "` + itoa(s2.Claim.Fence) + `", "packet": ` + goodPacket + `}`
	ctx2 = step2(worker2, version.Seed1, "claim.parked", "c-1", plain)
	if after2, _ := ctx2.Lifecycle.State("c-1"); after2.Escalation != nil {
		t.Fatal("a plain park asks nothing")
	}
}

// One question at a time: a second raise while one stands refuses,
// because "nothing else about the contract moves" includes asking a
// different question over the top of the first.
func TestASecondQuestionRefuses(t *testing.T) {
	ctx, _, _, maintainer, step := escalationFixture(t)
	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody)
	err := Check(ctx, draftV(t, maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody, ctx.Tip))
	if err == nil {
		t.Fatal("a second question must refuse while the first stands")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// D1's other half, and the one the plan's first draft got wrong: a
// VERIFIER holding a review subject can raise. The charter says any
// lane can raise blocked(needs-you), and the route the first draft
// left it — render a fail verdict to reach contract.returned, then
// escalate from ready — would launder an environmental problem into a
// judgement about the submission.
func TestAVerifierCanRaiseOnAReviewSubject(t *testing.T) {
	ctx, signer, worker, maintainer, step := escalationFixture(t)
	verifier := fixtureKey(t, 7)
	ctx = step(signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, verifier), enrollBody(t, verifier, "agent", "v7"))
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, verifier), `{"capability": "`+keyring.CapVerdict+`"}`)
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	s, _ := ctx.Lifecycle.State("c-1")
	submit := `{"fence": "` + itoa(s.Claim.Fence) + `", "packet": ` + goodPacket + `}`
	ctx = step(worker, version.Seed1, "submission.made", "c-1", submit)
	if s, _ := ctx.Lifecycle.State("c-1"); s.State != "review" {
		t.Fatalf("the drill needs a subject in review: %q", s.State)
	}
	if err := Check(ctx, draftV(t, verifier, version.Seed1, "escalation.raised", "c-1", raiseBody, ctx.Tip)); err != nil {
		t.Fatalf("a verifier holding a review subject must be able to raise: %v", err)
	}
	// The boundary's own answer above IS the conformance point. The
	// fold half below is driven by the dispatch key instead, because
	// grantFixture's resolver knows only its three keys and cannot
	// APPEND as a fourth — a fixture limit, not a rule. Both keys hold
	// a capability the row accepts, so the record is the same shape
	// either signs.
	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody)
	after, _ := ctx.Lifecycle.State("c-1")
	if after.State != "blocked" || after.Escalation == nil {
		t.Fatalf("the raise blocks and stands: %q %+v", after.State, after.Escalation)
	}
	// And the answer returns it to ready, exactly as contract.returned
	// does from review, with prior facts persisting as history.
	answer := `{"escalation": "` + itoa(after.Escalation.Pos) + `", "choice": "a"}`
	ctx = step(signer, version.Seed1, "decision.recorded", "c-1", answer)
	final, _ := ctx.Lifecycle.State("c-1")
	if final.State != "ready" {
		t.Fatalf("answering re-queues the contract: %q", final.State)
	}
	if final.Submission == nil {
		t.Error("prior facts persist as history: the submission is not erased")
	}
}

// The raise carries no fence, because outside in_progress there is no
// active fence and citing one refuses — the landed rule holding rather
// than a new one. The packet obligation is the charter's, and the
// zero-length base range is how a raise with no work to hand off
// spells that honestly.
func TestTheRaiseCarriesAPacketAndNoFence(t *testing.T) {
	ctx, _, _, maintainer, _ := escalationFixture(t)
	fenced := `{"fence": "3", "packet": ` + goodPacket + `, "escalation": ` + question + `}`
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "escalation.raised", "c-1", fenced, ctx.Tip)); err == nil {
		t.Fatal("a fence outside in_progress must refuse: a fence dies with its claim window")
	}
	// goodPacket IS the zero-length range, so the happy path above
	// already asserts it is accepted; this pins that it is the shape a
	// raise with no work to hand off uses.
	if !strings.Contains(goodPacket, "0000000000000000000000000000000000000000..0000000000000000000000000000000000000000") {
		t.Fatal("the drill's packet must use the zero-length base range")
	}
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "escalation.raised", "c-1", raiseBody, ctx.Tip)); err != nil {
		t.Fatalf("the zero-length range is legal on a raise: %v", err)
	}
}

// conformance: the orientation read must not hide a legal act. The
// affordance catalog SIGNS a synthesized payload per verb and lists
// the verb only if the boundary admits it, so a synthesizer that does
// not match what the current state accepts makes a legal act look
// unavailable (review findings on #200).
//
// Two shapes of that, both drilled here: an answer whose option ids
// are not the catalog's guess, and a cancellation once a citation is
// required.
func TestAffordancesOfferTheAnswersTheQuestionActuallyAllows(t *testing.T) {
	ctx, signer, _, maintainer, step := escalationFixture(t)
	// Ids deliberately unlike any a catalog could guess.
	q := `{"question": "ship?", "options": [{"id": "yes", "choice": "ship it"}, {"id": "no", "choice": "hold"}]}`
	ctx = step(maintainer, version.Seed1, "escalation.raised", "c-1", `{"packet": `+goodPacket+`, "escalation": `+q+`}`)

	got := Affordances(ctx, signer, "c-1")
	has := func(verb string) bool {
		for _, v := range got {
			if v == verb {
				return true
			}
		}
		return false
	}
	// The operator CAN answer: the two documented ways out of a
	// standing question are the answer and a citing cancellation, and
	// the read must offer both.
	if !has("decision.recorded") {
		t.Errorf("%s is legal and must be offered — the probe has to choose an id the question OFFERS, not a guess: %v", "decision.recorded", got)
	}
	if !has("contract.cancelled") {
		t.Errorf("contract.cancelled is legal with the citation and must be offered — the probe has to carry it: %v", got)
	}
	// And the machine exit is NOT offered, so the list is not merely
	// permissive.
	if has("contract.unblocked") {
		t.Errorf("contract.unblocked is locked out and must not be offered: %v", got)
	}
}
