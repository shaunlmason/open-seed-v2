package admit

// The budget admission drills (plans/os-cecac5de.md;
// spec/budgets.md): reserves admit only from the active claim
// holder or operator inside the claim window and within remaining;
// closes only from the reservation's owner or operator on valid,
// still-open reservations; raw foreign facts are inert on every
// consuming path; the spending gate refuses without an open
// reservation.

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

type budgetKeys struct {
	signer, holder, other, plain ed25519.PrivateKey
}

// budgetFixture enrolls two claim workers and a grantless key, files
// and specifies c-1 (budget small = 100 units), and has the holder
// take the claim.
func budgetFixture(t *testing.T) (*Context, budgetKeys, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := budgetKeys{signer: signer, holder: fixtureKey(t, 2), other: fixtureKey(t, 5), plain: fixtureKey(t, 6)}
	all := []ed25519.PrivateKey{k.signer, k.holder, k.other, k.plain}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range all {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, e := range []struct {
		key  ed25519.PrivateKey
		name string
	}{{k.holder, "holder"}, {k.other, "other"}, {k.plain, "plain"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
	}
	for _, g := range []ed25519.PrivateKey{k.holder, k.other} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, g), `{"capability": "claim"}`)
	}
	step := func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ctx := step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(k.holder, version.Seed1, "claim.taken", "c-1", `{}`)
	return ctx, k, step
}

func fenceOf(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Claim == nil {
		t.Fatalf("no active claim on %s", subject)
	}
	return fmt.Sprintf("%d", s.Claim.Fence)
}

func reserveBody(amount, fence string) string {
	return fmt.Sprintf(`{"amount": %q, "fence": %q}`, amount, fence)
}

// conformance: III.H — spending is fenced to admitted reservations;
// concurrent over-spend against one budget is structurally impossible
// because the reserve is checked and decremented at admission.
func TestBudgetAdmissionMatrix(t *testing.T) {
	ctx, k, step := budgetFixture(t)
	fence := fenceOf(t, ctx, "c-1")

	// Shape refusals from the holder in the window.
	for name, body := range map[string]string{
		"extra key":       `{"amount": "10", "fence": "` + fence + `", "note": "x"}`,
		"zero amount":     reserveBody("0", fence),
		"negative amount": reserveBody("-4", fence),
		"word amount":     reserveBody("ten", fence),
	} {
		if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", body, ctx.Tip)); err == nil {
			t.Fatalf("%s must refuse", name)
		}
	}

	// The lanes: the non-holder claim worker and the plain key refuse
	// before capacity logic; the holder and the operator admit.
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.reserve", "c-1", reserveBody("10", fence), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "only the active claim holder or the operator lane reserves") {
		t.Fatalf("a non-holder claim worker must refuse by name: %v", err)
	}
	if err := Check(ctx, draftV(t, k.plain, version.Seed1, "budget.reserve", "c-1", reserveBody("10", fence), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "not granted any of") {
		t.Fatalf("a grantless key is out of the lane entirely: %v", err)
	}
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("60", fence), ctx.Tip)); err != nil {
		t.Fatalf("the holder's reserve admits: %v", err)
	}

	// The serialized view: after the 60 admits, 50 exceeds the 40
	// remaining and refuses naming both numbers; 40 admits; the
	// operator can top up nothing further.
	ctx = step(k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("60", fence))
	err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("50", fence), ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "exceeds remaining 40 of capacity 100") {
		t.Fatalf("over-remaining refuses with both numbers: %v", err)
	}
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("40", fence), ctx.Tip)); err != nil {
		t.Fatalf("a top-up within remaining admits: %v", err)
	}
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, "budget.reserve", "c-1", reserveBody("1", fence), ctx.Tip)); err != nil {
		// The operator lane reserves without holding the claim; the
		// fence rule does not require its citation, but a correct one
		// is legal.
		t.Fatalf("the operator's reserve within remaining admits: %v", err)
	}

	// Closes: the owner settles with overrun actuals recorded, a
	// second close refuses as already closed, and a foreign worker
	// cannot close the owner's reservation.
	resPos := fmt.Sprintf("%d", ctx.Count-1) // the 60-unit reserve applied above
	settleBody := `{"reservation": "` + resPos + `", "actuals": "80", "fence": "` + fence + `"}`
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.settle", "c-1", settleBody, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "belongs to") {
		t.Fatalf("a foreign close refuses by name: %v", err)
	}
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.settle", "c-1", settleBody, ctx.Tip)); err != nil {
		t.Fatalf("the owner's overrun settle admits: %v", err)
	}
	ctx = step(k.holder, version.Seed1, "budget.settle", "c-1", settleBody)
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.release", "c-1", `{"reservation": "`+resPos+`", "fence": "`+fence+`"}`, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "already effectively closed") {
		t.Fatalf("a second close refuses: %v", err)
	}
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.settle", "c-1", `{"reservation": "2", "actuals": "1", "fence": "`+fence+`"}`, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "no reservation") {
		t.Fatalf("citing a non-reservation position refuses: %v", err)
	}

	// The window: reserves refuse outside in_progress.
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-2", filedBody)
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-2", reserveBody("10", ""), ctx.Tip)); err == nil {
		t.Fatal("a reserve in backlog must refuse — spend happens inside a claim window")
	}
}

// conformance: III.H — a reservation outlives the window that opened
// it, so its close does too (plans/os-d6963652.md D1, D2). The gate
// is on the RESERVE alone: outside a window a close cites no fence
// and admits from the reservation's own signer or the operator, a
// close inside a LATER window cites that window's fence, capacity
// returns either way, and neither a foreign closer nor a reserve
// outside a window gains anything by it. The scenario is the one the
// defect taxed: a first attempt fails review and a different worker
// retries.
func TestBudgetClosesOutliveTheClaimWindow(t *testing.T) {
	ctx, k, step := budgetFixture(t)
	// The fixture's grantless key becomes the verifier: it never
	// claimed c-1, so the independence rule is satisfied. The
	// operator lane returns the contract, its own row on the verb.
	ctx = step(k.signer, version.Seed1, keyring.VerbGranted, fpOf(t, k.plain), `{"capability": "`+keyring.CapVerdict+`"}`)

	// The first attempt reserves inside its window, then submits: the
	// window ends with the reservation still open.
	first := fenceOf(t, ctx, "c-1")
	ctx = step(k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("60", first))
	resPos := fmt.Sprintf("%d", ctx.Count-1)
	ctx = step(k.holder, version.Seed1, "submission.made", "c-1", `{"fence": "`+first+`", "packet": `+minPacket+`}`)
	if s, _ := ctx.Lifecycle.State("c-1"); s.State != "review" || s.Claim != nil {
		t.Fatalf("the window must have ended: state %q, claim %v", s.State, s.Claim)
	}

	noFence := `{"reservation": "` + resPos + `", "actuals": "50"}`
	// Out of the window, citing no fence: the signer closes, and so
	// does the operator lane.
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.settle", "c-1", noFence, ctx.Tip)); err != nil {
		t.Fatalf("the reservation's signer settles after the window ends: %v", err)
	}
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, "budget.release", "c-1", `{"reservation": "`+resPos+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("the operator lane releases after the window ends: %v", err)
	}
	// Nobody else does, in or out of a window: ownership is unchanged.
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.settle", "c-1", noFence, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "belongs to") {
		t.Fatalf("a foreign close still refuses by name outside the window: %v", err)
	}
	// A citation outside a window still refuses, at the fence rule:
	// the fence died with the claim, and D1 changed nothing there.
	stale := `{"reservation": "` + resPos + `", "actuals": "50", "fence": "` + first + `"}`
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.settle", "c-1", stale, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "no claim is active") {
		t.Fatalf("citing a dead fence refuses at the fence rule: %v", err)
	}
	// And the reserve keeps its own gate, with its own message.
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", `{"amount": "5"}`, ctx.Tip)); err == nil ||
		!strings.Contains(err.Error(), "is illegal for subject c-1 in state review") {
		t.Fatalf("a reserve outside a window still refuses as an illegal transition: %v", err)
	}

	// Capacity returns: settling 50 of the 60 reserved leaves the
	// class's 100 less the actuals, for whoever claims next.
	settled := step(k.holder, version.Seed1, "budget.settle", "c-1", noFence)
	s, _ := settled.Lifecycle.State("c-1")
	view := BudgetViewAt(settled.Records, settled.Table, "c-1", s)
	if len(view.Open) != 0 || view.Settled != 50 || view.Remaining != 50 {
		t.Fatalf("after the out-of-window settle: open %d, settled %d, remaining %d, want 0/50/50",
			len(view.Open), view.Settled, view.Remaining)
	}

	// The retry: a fail verdict returns the contract, a DIFFERENT
	// worker claims it, and its own reserve sees the capacity the
	// class grants rather than the previous attempt's stranded hold.
	sub := fmt.Sprintf("%d", subPos(t, settled, "c-1"))
	ctx = step(k.plain, version.Seed1, "verdict.rendered", "c-1",
		`{"verdict": "fail", "receipt": "`+zeros64+`", "submission": "`+sub+`", "independence": "L1"}`)
	ctx = step(k.signer, version.Seed1, "contract.returned", "c-1", `{"verdict": "fail"}`)
	ctx = step(k.other, version.Seed1, "claim.taken", "c-1", `{}`)
	second := fenceOf(t, ctx, "c-1")
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.reserve", "c-1", reserveBody("50", second), ctx.Tip)); err != nil {
		t.Fatalf("the retry reserves the full remaining, untaxed by the first attempt: %v", err)
	}

	// A close inside a LATER window cites THAT window's fence: the
	// fence rule's active-citation semantics, unchanged.
	ctx = step(k.other, version.Seed1, "budget.reserve", "c-1", reserveBody("50", second))
	pos2 := fmt.Sprintf("%d", ctx.Count-1)
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.release", "c-1", `{"reservation": "`+pos2+`", "fence": "`+second+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("a close inside a window cites that window's fence: %v", err)
	}
	// The first attempt's holder is a prior claimant now, so the
	// fence rule REQUIRES the citation of them; the operator, who is
	// neither holder nor prior claimant, may omit it.
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.release", "c-1", `{"reservation": "`+pos2+`"}`, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "must cite") {
		t.Fatalf("a prior claimant omitting the active fence refuses: %v", err)
	}
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, "budget.release", "c-1", `{"reservation": "`+pos2+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("the operator lane closes inside another's window without citing: %v", err)
	}
}

// subPos is the bound submission's position.
func subPos(t *testing.T, ctx *Context, subject string) int {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Submission == nil {
		t.Fatalf("no submission on %s", subject)
	}
	return s.Submission.Pos
}

// conformance: III.H — a raw foreign reserve consumes no capacity, a
// raw foreign release frees none, a raw foreign settle neither closes
// nor locks the owner out, and a released prior claimant cannot
// reserve under the next holder's window.
func TestBudgetLaunderingInert(t *testing.T) {
	ctx, k, step := budgetFixture(t)
	fence := fenceOf(t, ctx, "c-1")
	ctx = step(k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("60", fence))
	resPos := fmt.Sprintf("%d", ctx.Count-1)

	// The plain key raw-pushes a well-shaped reserve (appendSignedV is
	// the raw path: no admission Check). It folds but consumes
	// nothing: the holder can still reserve the full 40.
	ctx = step(k.plain, version.Seed1, "budget.reserve", "c-1", reserveBody("40", fence))
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("40", fence), ctx.Tip)); err != nil {
		t.Fatalf("the foreign reserve consumed nothing: %v", err)
	}

	// The plain key raw-pushes a release of the holder's reservation:
	// it frees nothing (a 50 reserve still exceeds remaining 40) and
	// does not close it (the owner's settle still admits).
	ctx = step(k.plain, version.Seed1, "budget.release", "c-1", `{"reservation": "`+resPos+`"}`)
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("50", fence), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "exceeds remaining 40") {
		t.Fatalf("the foreign release freed nothing: %v", err)
	}
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.settle", "c-1", `{"reservation": "`+resPos+`", "actuals": "60", "fence": "`+fence+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("the owner's settle is still the effective closure: %v", err)
	}

	// The prior-claimant leak (review finding on plan #147): the
	// holder releases, the other worker claims, and the released
	// worker's reserve refuses while the new holder's admits.
	ctx = step(k.holder, version.Seed1, "claim.released", "c-1", fmt.Sprintf(`{"fence": %q, "packet": %s}`, fence, minPacket))
	ctx = step(k.other, version.Seed1, "claim.taken", "c-1", `{}`)
	fence2 := fenceOf(t, ctx, "c-1")
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("5", fence2), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "prior claimant") {
		t.Fatalf("the released worker cannot reserve under the next holder's window: %v", err)
	}
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.reserve", "c-1", reserveBody("5", fence2), ctx.Tip)); err != nil {
		t.Fatalf("the new holder reserves: %v", err)
	}
}

// conformance: III.H — spending verbs require an admitted
// budget.reserve; the gate ships empty and is drilled by injection.
func TestSpendingVerbGate(t *testing.T) {
	restore := transition.InjectSpendingVerb("message.sent")
	defer restore()
	ctx, k, step := budgetFixture(t)
	fence := fenceOf(t, ctx, "c-1")
	spend := `{"fence": "` + fence + `"}`
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "message.sent", "c-1", spend, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "spending verbs require an admitted budget.reserve") {
		t.Fatalf("a spending verb with no open reservation refuses by name: %v", err)
	}
	ctx = step(k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("10", fence))
	if err := Check(ctx, draftV(t, k.holder, version.Seed1, "message.sent", "c-1", spend, ctx.Tip)); err != nil {
		t.Fatalf("the same verb admits against the open reservation: %v", err)
	}
}

// An unknown class has no capacity: absent knowledge is never fudged
// into spendable units.
func TestUnknownBudgetClassRefuses(t *testing.T) {
	ctx, k, step := budgetFixture(t)
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-3", `{"intent": "drill", "tier": "trivial", "budget": "bespoke", "routing": "core"}`)
	ctx = step(k.signer, version.Seed1, "contract.specified", "c-3", specBody)
	ctx = step(k.other, version.Seed1, "claim.taken", "c-3", `{}`)
	fence := fenceOf(t, ctx, "c-3")
	if err := Check(ctx, draftV(t, k.other, version.Seed1, "budget.reserve", "c-3", reserveBody("1", fence), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "no capacity in the class table") {
		t.Fatalf("an unknown class refuses by name: %v", err)
	}
}
