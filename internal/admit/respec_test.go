package admit

import (
	"crypto/ed25519"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	respecFirst  = `{"acceptance": {"ref": "specs/first.md @ abc1234", "executable": false}}`
	respecSecond = `{"acceptance": {"ref": "specs/second.md @ def5678", "executable": false}}`
	respecDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// respecFixture stands up a chain at seed/3 with a dispatcher, a
// worker, a verifier and the root, each resolvable ahead of the
// keyring, and returns a step that appends at a named version.
func respecFixture(t *testing.T) (ctx *Context, keys map[string]ed25519.PrivateKey, step func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	keys = map[string]ed25519.PrivateKey{"root": signer, "worker": fixtureKey(t, 2), "dispatcher": fixtureKey(t, 3), "verifier": fixtureKey(t, 4)}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range keys {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	step = func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	step(signer, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	step(signer, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	ctx = step(signer, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	for name, capability := range map[string]string{"worker": keyring.CapClaim, "dispatcher": keyring.CapDispatch, "verifier": keyring.CapVerdict} {
		step(signer, version.Seed3, keyring.VerbEnrolled, fpOf(t, keys[name]), enrollBody(t, keys[name], "agent", name))
		ctx = step(signer, version.Seed3, keyring.VerbGranted, fpOf(t, keys[name]), `{"capability": "`+capability+`"}`)
	}
	return ctx, keys, step
}

// conformance: plans/os-6bd9ffff.md D4, AC4 — at seed/4 a dispatch key
// re-specifies a ready contract and the fold reads two specifications;
// before seed/4 the ready origin refuses naming the version; on
// in_progress, review, blocked and done it refuses by the table; from
// a claim key it is out of grant; and the dispatcher's affordances
// gain exactly the ready-origin specification at seed/4.
func TestRespecificationActivatesAtSeed4(t *testing.T) {
	ctx, keys, step := respecFixture(t)
	root, worker, dispatcher, verifier := keys["root"], keys["worker"], keys["dispatcher"], keys["verifier"]
	ctx = step(dispatcher, version.Seed3, "intent.filed", "c-1", filedBody)
	ctx = step(dispatcher, version.Seed3, "contract.specified", "c-1", respecFirst)
	if slices.Contains(Affordances(ctx, dispatcher, "c-1"), "contract.specified") {
		t.Fatal("at seed/3 a ready contract cannot be re-specified, so the boundary does not afford it")
	}
	var itr *transition.InvalidTransitionError
	err := Check(ctx, draftV(t, dispatcher, version.Seed3, "contract.specified", "c-1", respecSecond, ctx.Tip))
	if !errors.As(err, &itr) || itr.From != "ready" || !strings.Contains(itr.Reason, version.Seed4) || !strings.Contains(itr.Reason, version.Seed3) {
		t.Fatalf("before seed/4 the ready origin refuses as an illegal transition naming the version, got %v", err)
	}

	ctx = step(root, version.Seed3, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	if !slices.Contains(Affordances(ctx, dispatcher, "c-1"), "contract.specified") {
		t.Fatal("at seed/4 the boundary affords re-specifying a ready contract")
	}
	var oog *OutOfGrantError
	if err := Check(ctx, draftV(t, worker, version.Seed4, "contract.specified", "c-1", respecSecond, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("a claim key cannot re-specify: out of grant, got %v", err)
	}
	ctx = step(dispatcher, version.Seed4, "contract.specified", "c-1", respecSecond)
	s, ok := ctx.Lifecycle.State("c-1")
	if !ok || s.State != "ready" || s.Specifications != 2 || s.Acceptance == nil || s.Acceptance.Ref != "specs/second.md @ def5678" || s.Anomalies != 0 {
		t.Fatalf("the re-specification folds: two specifications, the second acceptance, still ready, no anomaly; got %+v", s)
	}

	// Every other state refuses by the table, with no reason attached:
	// the table's own refusal, not the version's.
	refusesByTable := func(state string) {
		t.Helper()
		err := Check(ctx, draftV(t, dispatcher, version.Seed4, "contract.specified", "c-1", respecFirst, ctx.Tip))
		if !errors.As(err, &itr) || itr.From != state || itr.Reason != "" {
			t.Fatalf("on %s a re-specification refuses by the table, got %v", state, err)
		}
	}
	ctx = step(worker, version.Seed4, "claim.taken", "c-1", `{}`)
	refusesByTable("in_progress")
	fence := activeFence(t, ctx, "c-1")
	ctx = step(worker, version.Seed4, "claim.released", "c-1", `{"fence": "`+fence+`", "packet": `+minPacket+`}`)
	ctx = step(dispatcher, version.Seed4, "contract.blocked", "c-1", `{}`)
	refusesByTable("blocked")
	ctx = step(dispatcher, version.Seed4, "contract.unblocked", "c-1", `{}`)
	ctx = step(worker, version.Seed4, "claim.taken", "c-1", `{}`)
	fence = activeFence(t, ctx, "c-1")
	ctx = step(worker, version.Seed4, "submission.made", "c-1", `{"fence": "`+fence+`", "packet": `+minPacket+`}`)
	refusesByTable("review")
	ctx = step(verifier, version.Seed4, "verdict.rendered", "c-1", `{"verdict": "pass", "receipt": "`+zeros64+`", "submission": "`+submissionOf(t, ctx, "c-1")+`", "independence": "L1"}`)
	ctx = step(worker, version.Seed4, "merge.requested", "c-1", `{"verdict": "`+verdictOf(t, ctx, "c-1")+`"}`)
	ctx = step(root, version.Seed4, "merge.observed", "c-1", `{"merged": "abc1234", "pr": "pr/1"}`)
	refusesByTable("done")
	if s, _ := ctx.Lifecycle.State("c-1"); s.Specifications != 2 {
		t.Fatalf("the count is the applied specifications' and nothing later moves it: %d", s.Specifications)
	}
}

// conformance: D5, AC5 — through the boundary: at seed/4 a proposal
// without a digest is incomplete naming it and one with a digest
// admits; before seed/4 a proposal carrying one refuses naming the
// version.
func TestPlanDigestsThroughTheBoundary(t *testing.T) {
	ctx, keys, step := respecFixture(t)
	root, worker, dispatcher := keys["root"], keys["worker"], keys["dispatcher"]
	ctx = step(dispatcher, version.Seed3, "intent.filed", "c-1", filedBody)
	ctx = step(dispatcher, version.Seed3, "contract.specified", "c-1", respecFirst)
	ctx = step(worker, version.Seed3, "claim.taken", "c-1", `{}`)
	fence := activeFence(t, ctx, "c-1")
	var chain *transition.ChainError
	err := Check(ctx, draftV(t, worker, version.Seed3, "plan.proposed", "c-1", `{"fence": "`+fence+`", "plan": "plans/c-1.md @ abc1234", "digest": "`+respecDigest+`"}`, ctx.Tip))
	if !errors.As(err, &chain) || !strings.Contains(chain.Reason, version.Seed4) {
		t.Fatalf("a digest before seed/4 refuses naming the version, got %v", err)
	}
	if err := Check(ctx, draftV(t, worker, version.Seed3, "plan.proposed", "c-1", `{"fence": "`+fence+`", "plan": "plans/c-1.md @ abc1234"}`, ctx.Tip)); err != nil {
		t.Fatalf("a seed/3 proposal without a digest admits as before: %v", err)
	}
	ctx = step(root, version.Seed3, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	var inc *transition.IncompleteError
	err = Check(ctx, draftV(t, worker, version.Seed4, "plan.proposed", "c-1", `{"fence": "`+fence+`", "plan": "plans/c-1.md @ abc1234"}`, ctx.Tip))
	if !errors.As(err, &inc) || !slices.Contains(inc.Missing, "digest") {
		t.Fatalf("at seed/4 a proposal without a digest is incomplete naming it, got %v", err)
	}
	if !slices.Contains(Affordances(ctx, worker, "c-1"), "plan.proposed") {
		t.Fatal("the probe carries the digest at seed/4, so the holder's proposal is still afforded")
	}
	ctx = step(worker, version.Seed4, "plan.proposed", "c-1", `{"fence": "`+fence+`", "plan": "plans/c-1.md @ abc1234", "digest": "`+respecDigest+`"}`)
	err = Check(ctx, draftV(t, root, version.Seed4, "plan.approved", "c-1", `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1 @ abc1234"}`, ctx.Tip))
	if !errors.As(err, &inc) || !slices.Contains(inc.Missing, "digest") {
		t.Fatalf("at seed/4 an approval without a digest is incomplete naming it, got %v", err)
	}
	ctx = step(root, version.Seed4, "plan.approved", "c-1", `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1 @ abc1234", "digest": "`+respecDigest+`"}`)
	if unedited, measured := ctx.Lifecycle.PlanDigests("c-1").Unedited(); !unedited || !measured {
		t.Fatalf("the approval kept the proposal: unedited and measured, got %v %v", unedited, measured)
	}
}

// conformance: D1 — ContextOver agrees with ContextAt position for
// position: the recorder's frames are the boundary's own derivation,
// not a second opinion.
func TestContextOverAgreesWithContextAt(t *testing.T) {
	store, resolve, signer := seededStore(t)
	lanes := walkLanes(t)
	keys := map[string]ed25519.PrivateKey{"root": signer}
	for name, key := range lanes {
		keys[name] = key
	}
	loose := walkResolver(t, resolve, lanes)
	check := func(pos int) {
		t.Helper()
		at, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		over, err := ContextOver(at.Records)
		if err != nil {
			t.Fatalf("position %d: %v", pos, err)
		}
		if over.Count != at.Count || over.Tip != at.Tip || over.Active != at.Active || over.Halt != at.Halt {
			t.Fatalf("position %d: the two contexts disagree: %+v vs %+v", pos, over.Count, at.Count)
		}
		for _, pair := range []struct{ lane, subject string }{{"holder", "c-1"}, {"supervisor", "c-1"}, {"verifier", "c-1"}, {"root", "system"}} {
			if a, b := Affordances(at, keys[pair.lane], pair.subject), Affordances(over, keys[pair.lane], pair.subject); !slices.Equal(a, b) {
				t.Fatalf("position %d: affordances for %s on %s differ: %v vs %v", pos, pair.lane, pair.subject, a, b)
			}
		}
	}
	check(0)
	for i, s := range walkScript(t, lanes) {
		runWalkStep(t, store, loose, keys, s)
		check(i + 1)
	}
	if _, err := ContextOver(nil); err == nil {
		t.Fatal("an empty prefix has no genesis and no context")
	}
}

// conformance: D4, AC4 — the residual entry: the dispatcher's
// re-specification is named in the residual table with why it is
// admitted and what it can inflict, and the reachability drill
// (TestDispatcherReachableSetIsNamed) stays green beside it.
func TestRespecificationIsANamedResidual(t *testing.T) {
	for _, r := range loadResiduals(t) {
		if r.Verb != "contract.specified" {
			continue
		}
		text := strings.ToLower(r.WhyAdmitted + " " + r.Consequence)
		if !strings.Contains(text, "re-specif") || !strings.Contains(text, "seed/4") {
			t.Fatalf("the contract.specified residual names re-specification and the version it activates at: %q", r.Consequence)
		}
		return
	}
	t.Fatal("contract.specified is not in the residual table")
}
