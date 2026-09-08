package admit

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const racingDeclaration = `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard", "racing": {"racers": 2, "cost": "two runs per contract, the loser's spend written off"}}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`

// raceFixture is a seed/6 chain with c-1 specified, two claim-granted
// workers, a third at the ready, and a verdict-granted verifier, plus a
// context view under the racing declaration.
type raceFixture struct {
	ctx                        *Context
	signer                     ed25519.PrivateKey
	a, b, c, verifier, maint   ed25519.PrivateKey
	step                       func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context
	under                      func() *Context
	fenceA, fenceB, submission int
}

func newRaceFixture(t *testing.T) *raceFixture {
	t.Helper()
	store, resolve, signer := seededStore(t)
	a, b, c, verifier, maint := fixtureKey(t, 2), fixtureKey(t, 7), fixtureKey(t, 11), fixtureKey(t, 9), fixtureKey(t, 3)
	keys := []ed25519.PrivateKey{signer, a, b, c, verifier, maint}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range keys {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	step := func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		ctx, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
	appendSigned(t, store, loose, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{version.Seed2, version.Seed3, version.Seed4, version.Seed5, version.Seed6} {
		ctx = step(signer, ctx.Active, ledger.UpgradeVerb, "system", `{"to": "`+v+`"}`)
	}
	for _, k := range []ed25519.PrivateKey{a, b, c} {
		ctx = step(signer, version.Seed6, keyring.VerbEnrolled, fpOf(t, k), enrollBody(t, k, "agent", "racer"))
		ctx = step(signer, version.Seed6, keyring.VerbGranted, fpOf(t, k), `{"capability": "`+keyring.CapClaim+`"}`)
	}
	ctx = step(signer, version.Seed6, keyring.VerbEnrolled, fpOf(t, verifier), enrollBody(t, verifier, "agent", "verifier"))
	ctx = step(signer, version.Seed6, keyring.VerbGranted, fpOf(t, verifier), `{"capability": "`+keyring.CapVerdict+`"}`)
	ctx = step(signer, version.Seed6, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed6, "contract.specified", "c-1", specBody)
	f := &raceFixture{ctx: ctx, signer: signer, a: a, b: b, c: c, verifier: verifier, maint: maint, step: step}
	cfg := declared(t, racingDeclaration)
	f.under = func() *Context { with := *f.ctx; with.Declaration = cfg; return &with }
	return f
}

func (f *raceFixture) state(t *testing.T) transition.SubjectState {
	t.Helper()
	s, ok := f.ctx.Lifecycle.State("c-1")
	if !ok {
		t.Fatal("c-1 is unknown to the fold")
	}
	return s
}

// conformance: plans/os-56bee171.md AC1, AC2 — the opt-in is declared,
// each racer holds its own fence: a second claim admits under the
// racing declaration and refuses contention without it, a third at
// the cap refuses naming both holders, a racer citing the other's
// fence refuses fenced_out, and a racer's exit kills its claim alone.
func TestRacingClaimsAdmitUnderTheOptInAlone(t *testing.T) {
	f := newRaceFixture(t)
	f.ctx = f.step(f.a, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceA = f.state(t).Claim.Fence
	// No declaration: contention, as it always was.
	var ce *ContentionError
	if err := Check(f.ctx, draftV(t, f.b, version.Seed6, "claim.taken", "c-1", `{}`, f.ctx.Tip)); !errors.As(err, &ce) || ce.Cap != 0 {
		t.Fatalf("without the opt-in a second claim is contention: %v", err)
	}
	// A declaration whose squad does not race: contention.
	plain := *f.ctx
	plain.Declaration = declared(t, ceilingDeclaration)
	if err := Check(&plain, draftV(t, f.b, version.Seed6, "claim.taken", "c-1", `{}`, f.ctx.Tip)); !errors.As(err, &ce) || ce.Cap != 0 {
		t.Fatalf("a squad without the racing block is exclusive: %v", err)
	}
	// Under the opt-in the second racer admits, with its own fence.
	if err := Check(f.under(), draftV(t, f.b, version.Seed6, "claim.taken", "c-1", `{}`, f.ctx.Tip)); err != nil {
		t.Fatalf("the second racer admits under the opt-in: %v", err)
	}
	f.ctx = f.step(f.b, version.Seed6, "claim.taken", "c-1", `{}`)
	s := f.state(t)
	if !s.Racing || len(s.Claims) != 2 || s.State != "in_progress" {
		t.Fatalf("two active claims on an in_progress racing subject: %+v", s.Claims)
	}
	f.fenceB = s.Claims[1].Fence
	if f.fenceA == f.fenceB || s.Claim.Fence != f.fenceA {
		t.Fatalf("each racer's fence is its own claim's position and the singular fact is the first: %d %d %+v", f.fenceA, f.fenceB, s.Claim)
	}
	// The same racer cannot hold twice; a third refuses at the cap
	// naming both holders.
	if err := Check(f.under(), draftV(t, f.a, version.Seed6, "claim.taken", "c-1", `{}`, f.ctx.Tip)); !errors.As(err, &ce) {
		t.Fatalf("a racer holding a claim cannot claim again: %v", err)
	}
	if err := Check(f.under(), draftV(t, f.c, version.Seed6, "claim.taken", "c-1", `{}`, f.ctx.Tip)); !errors.As(err, &ce) || ce.Cap != 2 || len(ce.Racers) != 2 {
		t.Fatalf("a third racer refuses at the cap naming both: %v", err)
	}
	// A racer's fact cites its own fence; the other's refuses.
	var fe *FenceError
	if err := Check(f.under(), draftV(t, f.a, version.Seed6, "progress.milestone", "c-1", fmt.Sprintf(`{"fence": "%d", "count": 1, "step": "probe"}`, f.fenceB), f.ctx.Tip)); !errors.As(err, &fe) {
		t.Fatalf("citing the other racer's fence refuses fenced_out: %v", err)
	}
	if err := Check(f.under(), draftV(t, f.a, version.Seed6, "progress.milestone", "c-1", fmt.Sprintf(`{"fence": "%d", "count": 1, "step": "probe"}`, f.fenceA), f.ctx.Tip)); err != nil {
		t.Fatalf("a racer's own fence admits: %v", err)
	}
	// A racer's park kills its claim alone: the subject stays
	// in_progress under the other racer.
	park := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceA, minPacket)
	if err := Check(f.under(), draftV(t, f.a, version.Seed6, "claim.parked", "c-1", park, f.ctx.Tip)); err != nil {
		t.Fatalf("a racer's park admits claim-scoped: %v", err)
	}
	f.ctx = f.step(f.a, version.Seed6, "claim.parked", "c-1", park)
	s = f.state(t)
	if s.State != "in_progress" || len(s.Claims) != 1 || s.Claims[0].Fence != f.fenceB {
		t.Fatalf("the park closed one claim and moved no state: %s %+v", s.State, s.Claims)
	}
	// The last racer's release is the table's transition: ready.
	release := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceB, minPacket)
	f.ctx = f.step(f.b, version.Seed6, "claim.released", "c-1", release)
	if s = f.state(t); s.State != "ready" || len(s.Claims) != 0 {
		t.Fatalf("the last racer's departure re-readies the subject: %s %+v", s.State, s.Claims)
	}
}

// conformance: plans/os-56bee171.md AC3 — first verified success
// settles: submissions coexist by fence, verdicts bind to their own
// submission (a fail on one racer's locks only that racer), the first
// pass settles through the chain, the loser's next act refuses
// race_settled, and its own exit still admits.
func TestFirstVerifiedSuccessSettlesTheRace(t *testing.T) {
	f := newRaceFixture(t)
	f.ctx = f.step(f.a, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceA = f.state(t).Claim.Fence
	f.ctx = f.step(f.b, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceB = f.state(t).Claims[1].Fence
	// A submits first: the subject enters review; B still holds.
	subA := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceA, minPacket)
	if err := Check(f.under(), draftV(t, f.a, version.Seed6, "submission.made", "c-1", subA, f.ctx.Tip)); err != nil {
		t.Fatalf("the first submission admits: %v", err)
	}
	f.ctx = f.step(f.a, version.Seed6, "submission.made", "c-1", subA)
	s := f.state(t)
	if s.State != "review" || len(s.Claims) != 1 || s.Submission == nil {
		t.Fatalf("review with B's claim active: %s %+v", s.State, s.Claims)
	}
	subPosA := s.Submission.Pos
	// B submits from review: claim-scoped, a second submission.
	subB := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceB, minPacket)
	if err := Check(f.under(), draftV(t, f.b, version.Seed6, "submission.made", "c-1", subB, f.ctx.Tip)); err != nil {
		t.Fatalf("the second racer's submission admits from review: %v", err)
	}
	f.ctx = f.step(f.b, version.Seed6, "submission.made", "c-1", subB)
	s = f.state(t)
	if s.State != "review" || len(s.Claims) != 0 || len(s.Submissions) != 2 {
		t.Fatalf("two submissions in the window, no claims: %s %+v %+v", s.State, s.Claims, s.Submissions)
	}
	subPosB := s.Submissions[1].Pos
	// A fail on A's submission locks A alone; a pass on B's admits.
	fail := fmt.Sprintf(`{"verdict": "fail", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, subPosA)
	f.ctx = f.step(f.verifier, version.Seed6, "verdict.rendered", "c-1", fail)
	passA := fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, subPosA)
	if err := Check(f.under(), draftV(t, f.verifier, version.Seed6, "verdict.rendered", "c-1", passA, f.ctx.Tip)); err == nil {
		t.Fatal("a pass over A's failed submission is locked out")
	}
	passB := fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, subPosB)
	if err := Check(f.under(), draftV(t, f.verifier, version.Seed6, "verdict.rendered", "c-1", passB, f.ctx.Tip)); err != nil {
		t.Fatalf("a pass over B's submission admits beside A's fail: %v", err)
	}
	f.ctx = f.step(f.verifier, version.Seed6, "verdict.rendered", "c-1", passB)
	passPos := f.ctx.Count - 1
	// The chain cites B's pass, and done follows.
	req := fmt.Sprintf(`{"verdict": "%d"}`, passPos)
	if err := Check(f.under(), draftV(t, f.b, version.Seed6, "merge.requested", "c-1", req, f.ctx.Tip)); err != nil {
		t.Fatalf("the request cites the pass verdict: %v", err)
	}
	f.ctx = f.step(f.b, version.Seed6, "merge.requested", "c-1", req)
	f.ctx = f.step(f.signer, version.Seed6, "merge.observed", "c-1", `{"merged": "`+zeros40+`", "pr": "pr/1"}`)
	if s = f.state(t); s.State != "done" || s.RaceSettled != nil {
		t.Fatalf("done, with no racer settled-out since both submitted: %s %v", s.State, s.RaceSettled)
	}
}

// conformance: plans/os-56bee171.md AC3 — a racer that never submitted
// is settled-out when the other's pass lands: its next act refuses
// race_settled naming the settlement, and its own park still admits,
// claim-scoped on the done subject.
func TestSettledOutRacerRefusesButMayExit(t *testing.T) {
	f := newRaceFixture(t)
	f.ctx = f.step(f.a, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceA = f.state(t).Claim.Fence
	f.ctx = f.step(f.b, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceB = f.state(t).Claims[1].Fence
	subA := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceA, minPacket)
	f.ctx = f.step(f.a, version.Seed6, "submission.made", "c-1", subA)
	subPos := f.state(t).Submission.Pos
	pass := fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, subPos)
	f.ctx = f.step(f.verifier, version.Seed6, "verdict.rendered", "c-1", pass)
	passPos := f.ctx.Count - 1
	f.ctx = f.step(f.a, version.Seed6, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, passPos))
	f.ctx = f.step(f.signer, version.Seed6, "merge.observed", "c-1", `{"merged": "`+zeros40+`", "pr": "pr/1"}`)
	s := f.state(t)
	if s.State != "done" || s.RaceSettled == nil || len(s.Claims) != 1 || s.Claims[0].Fence != f.fenceB {
		t.Fatalf("settled with B still holding: %s %v %+v", s.State, s.RaceSettled, s.Claims)
	}
	var rs *RaceSettledError
	if err := Check(f.under(), draftV(t, f.b, version.Seed6, "progress.milestone", "c-1", fmt.Sprintf(`{"fence": "%d", "count": 1, "step": "late"}`, f.fenceB), f.ctx.Tip)); !errors.As(err, &rs) || rs.SettledAt != *s.RaceSettled {
		t.Fatalf("the settled-out racer's act refuses race_settled naming the settlement: %v", err)
	}
	park := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceB, minPacket)
	if err := Check(f.under(), draftV(t, f.b, version.Seed6, "claim.parked", "c-1", park, f.ctx.Tip)); err != nil {
		t.Fatalf("the settled-out racer's own exit admits: %v", err)
	}
	f.ctx = f.step(f.b, version.Seed6, "claim.parked", "c-1", park)
	if s = f.state(t); s.State != "done" || len(s.Claims) != 0 {
		t.Fatalf("the exit closed the claim and left done alone: %s %+v", s.State, s.Claims)
	}
	// A pre-seed/6 validator's view: at a seed/5 position the second
	// claim was contention and the fold applied nothing, so the
	// racing rows are seed/6's alone.
	if version.RacingApplies(version.Seed5) {
		t.Fatal("racing is defined at seed/6 alone")
	}
}

const zeros40 = "0000000000000000000000000000000000000000"

// conformance: plans/os-56bee171.md AC3 (the reaper's admission) — a
// settled-out claim is reaped on the done subject by a maintenance
// key (dispatch-granted, as the maintenance lane is for reaping) citing
// that claim's fence with a packet, the claim-scoped exit
// the boundary admits after settlement (the pass itself is drilled in
// internal/maintain).
func TestSettledOutClaimIsReapableAtTheBoundary(t *testing.T) {
	f := newRaceFixture(t)
	maint := f.maint
	f.ctx = f.step(f.signer, version.Seed6, keyring.VerbEnrolled, fpOf(t, maint), enrollBody(t, maint, "service", "maint"))
	f.ctx = f.step(f.signer, version.Seed6, keyring.VerbGranted, fpOf(t, maint), `{"capability": "`+keyring.CapDispatch+`"}`)
	f.ctx = f.step(f.a, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceA = f.state(t).Claim.Fence
	f.ctx = f.step(f.b, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceB = f.state(t).Claims[1].Fence
	f.ctx = f.step(f.a, version.Seed6, "submission.made", "c-1", fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceA, minPacket))
	subPos := f.state(t).Submission.Pos
	f.ctx = f.step(f.verifier, version.Seed6, "verdict.rendered", "c-1", fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, subPos))
	f.ctx = f.step(f.a, version.Seed6, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, f.ctx.Count-1))
	f.ctx = f.step(f.signer, version.Seed6, "merge.observed", "c-1", `{"merged": "`+zeros40+`", "pr": "pr/1"}`)
	if f.state(t).RaceSettled == nil {
		t.Fatal("the race settled with B's claim active")
	}
	reap := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceB, minPacket)
	if err := Check(f.under(), draftV(t, maint, version.Seed6, "claim.reaped", "c-1", reap, f.ctx.Tip)); err != nil {
		t.Fatalf("the maintenance key reaps the settled-out claim on the done subject: %v", err)
	}
	// Reaping a fence nobody holds refuses at the fence rule.
	stale := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceA, minPacket)
	var fe *FenceError
	if err := Check(f.under(), draftV(t, maint, version.Seed6, "claim.reaped", "c-1", stale, f.ctx.Tip)); !errors.As(err, &fe) {
		t.Fatalf("a reap of a closed fence refuses fenced_out: %v", err)
	}
	f.ctx = f.step(maint, version.Seed6, "claim.reaped", "c-1", reap)
	if s := f.state(t); s.State != "done" || len(s.Claims) != 0 {
		t.Fatalf("the reap closed the settled-out claim and left done alone: %s %+v", s.State, s.Claims)
	}
}

// conformance: charter §II.6 (settlement while a rival racer holds),
// the named regression for the divergence the racing enumeration found
// (plans/os-873b5153.md D8). The fixtures around it append their chains
// straight to the store, so nothing was checking that the settlement
// chain ADMITS on a racing subject: a rival's claim is still active at
// review, and until this landed the fence rule demanded that rival's
// fence from both merge steps, neither of whose strict payloads has a
// slot to carry one in. Every settled-out fact §II.6 rests on was
// therefore unreachable through the front door. The trace is the
// enumeration's own: claim(A) claim(B) submit(A) verdict.pass(A)
// request settle.
func TestSettlementAdmitsWhileARivalRacerHolds(t *testing.T) {
	f := newRaceFixture(t)
	f.ctx = f.step(f.a, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceA = f.state(t).Claim.Fence
	f.ctx = f.step(f.b, version.Seed6, "claim.taken", "c-1", `{}`)
	f.fenceB = f.state(t).Claims[1].Fence
	f.ctx = f.step(f.a, version.Seed6, "submission.made", "c-1", fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.fenceA, minPacket))
	subPos := f.state(t).Submission.Pos
	f.ctx = f.step(f.verifier, version.Seed6, "verdict.rendered", "c-1", fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, subPos))
	passPos := f.ctx.Count - 1
	// The requester is A, whose own claim closed on its submission while
	// B still holds: a prior claimant of a racing subject, which the
	// fence rule's free-event arm would have required to cite B's fence.
	req := fmt.Sprintf(`{"verdict": "%d"}`, passPos)
	if err := Check(f.under(), draftV(t, f.a, version.Seed6, "merge.requested", "c-1", req, f.ctx.Tip)); err != nil {
		t.Fatalf("the submitted racer's merge request admits with a rival holding: %v", err)
	}
	f.ctx = f.step(f.a, version.Seed6, "merge.requested", "c-1", req)
	obs := `{"merged": "` + zeros40 + `", "pr": "pr/1"}`
	if err := Check(f.under(), draftV(t, f.signer, version.Seed6, "merge.observed", "c-1", obs, f.ctx.Tip)); err != nil {
		t.Fatalf("the settlement admits with a rival racer holding: %v", err)
	}
	// The exemption is the merge chain's alone: B's own claim-scoped
	// acts still answer to the fence rule.
	var fe *FenceError
	late := `{"count": 1, "step": "late"}`
	if err := Check(f.under(), draftV(t, f.b, version.Seed6, "progress.milestone", "c-1", late, f.ctx.Tip)); !errors.As(err, &fe) {
		t.Fatalf("a holder's unfenced milestone still refuses: %v", err)
	}
}
