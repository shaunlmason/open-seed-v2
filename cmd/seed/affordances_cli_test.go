package main

// The envelope-stamping drills (plans/os-f5551001.md; charter
// III.I row 1): append-path responses carry the affordances for
// their signing actor and subject at the stamped position, and the
// budget block on budget-active subjects — on ledger append and on
// a specialized append path (verdict render) alike — while
// responses without a ledger+key+subject context keep the empty
// list and the null block.

import (
	"fmt"
	"slices"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// conformance: III.I — every verb response includes the verbs
// currently legal for this actor on this subject, computed from the
// same rule set admission enforces, at the position it carries.
func TestAffordanceStamping(t *testing.T) {
	ld, src, base, specCommit, head, priv, _, keys, _ := offerLedger(t)
	rng := base + ".." + head
	offerFile(t, ld, priv, specCommit, "c-1")

	// A plain append stamps the signer's affordances at the response
	// tip; no budget facts stand, so the block stays null.
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-1", "--payload", `{"n": 1}`)
	if code != 0 {
		t.Fatalf("append: %d %+v", code, e)
	}
	if !slices.Contains(e.Affordances, "contract.cancelled") || !slices.Contains(e.Affordances, "message.sent") {
		t.Fatalf("the operator's response lists the ready-state verbs: %v", e.Affordances)
	}
	if !slices.IsSorted(e.Affordances) {
		t.Fatalf("stamped affordances are sorted: %v", e.Affordances)
	}
	if e.Budget != nil {
		t.Fatalf("no budget facts stand, so the block stays null: %+v", e.Budget)
	}

	// After the claim, the holder's reserve response carries both
	// fields: the window verbs and the derived budget block.
	fencePos, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	fence := fmt.Sprintf("%d", fencePos)
	e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "budget.reserve", "--subject", "c-1", "--payload", `{"amount": "10", "fence": "`+fence+`"}`)
	if code != 0 {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	for _, verb := range []string{"budget.settle", "claim.released", "submission.made"} {
		if !slices.Contains(e.Affordances, verb) {
			t.Fatalf("the holder's response lists %s inside the window: %v", verb, e.Affordances)
		}
	}
	if e.Budget == nil || e.Budget.Reserved != "10" || e.Budget.Remaining != "90" {
		t.Fatalf("the budget block derives from the open reservation against class small: %+v", e.Budget)
	}

	// The specialized append path stamps too: the verifier's verdict
	// response lists their next legal moves on the judged subject.
	if _, err := admitAppend(t, ld, workerRawKey(22), "submission.made", "c-1", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["c-1 ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fencePos, rng)); err != nil {
		t.Fatalf("submission: %v", err)
	}
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", keys["verifier"], "--verdict", "pass")
	if code != 0 {
		t.Fatalf("verdict: %d %+v", code, e)
	}
	if !slices.Contains(e.Affordances, "message.sent") {
		t.Fatalf("the verifier's verdict response stamps affordances: %v", e.Affordances)
	}
	if e.Budget == nil || e.Budget.Reserved != "10" {
		t.Fatalf("the budget block rides the specialized path too: %+v", e.Budget)
	}

	// A read surface guesses no identity: without --key the list is
	// empty and the envelope block null; with the key it stamps.
	e, code = runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-1")
	if code != 0 {
		t.Fatalf("budget status: %d %+v", code, e)
	}
	if len(e.Affordances) != 0 || e.Budget != nil {
		t.Fatalf("keyless status keeps the advisory fields empty: %v %+v", e.Affordances, e.Budget)
	}
	e, code = runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-1", "--key", keys["workerA"])
	if code != 0 {
		t.Fatalf("budget status --key: %d %+v", code, e)
	}
	if len(e.Affordances) == 0 || e.Budget == nil {
		t.Fatalf("keyed status stamps both advisory fields: %v %+v", e.Affordances, e.Budget)
	}
}

// conformance: III.I row 2 — the position stamp that makes a
// concurrent event detectable (plans/os-148d3ba1.md D4): the
// stamped position and the stamped list must agree with an
// independent recomputation at that position, or divergence between
// a listing and a later refusal cannot be attributed. Both halves
// stamp the tip ordinal of the context the list was computed at
// (ctx.Count - 1: ContextAt counts records, the stamp is
// zero-based) — for a success that is the appended record's own
// ordinal, for a preview refusal the unmoved tip's.
func TestAffordanceStampPositionAgreement(t *testing.T) {
	ld, _, _, _, _, priv, rootKey, _, _ := offerLedger(t)

	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-1", "--payload", `{"n": 1}`)
	if code != 0 || e.Position == nil {
		t.Fatalf("append: %d %+v", code, e)
	}
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d", ctx.Count-1); *e.Position != want {
		t.Fatalf("the success stamp carries the tip ordinal: stamped %s, context tip %s", *e.Position, want)
	}
	if direct := admit.Affordances(ctx, rootKey, "c-1"); !slices.Equal(direct, e.Affordances) {
		t.Fatalf("the stamped list is the list at the stamped position: stamped %v, recomputed %v", e.Affordances, direct)
	}

	// A malformed actor event fails the keyring preview before
	// anything is written; the refusal envelope still stamps
	// position and affordances, computed at the unmoved tip for the
	// refused signer.
	e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "actor.enrolled", "--subject", "c-2", "--payload", `{"garbage": true}`)
	if code == 0 || e.Error == nil || e.Position == nil {
		t.Fatalf("the malformed enroll refuses with a stamped envelope: %d %+v", code, e)
	}
	after, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if after.Count != ctx.Count {
		t.Fatalf("a refusal moves no tip: %d vs %d", after.Count, ctx.Count)
	}
	if want := fmt.Sprintf("%d", after.Count-1); *e.Position != want {
		t.Fatalf("the refusal stamp carries the unmoved tip ordinal: stamped %s, want %s", *e.Position, want)
	}
	if direct := admit.Affordances(after, rootKey, "c-2"); !slices.Equal(direct, e.Affordances) {
		t.Fatalf("the refusal's stamped list is the signer's list at the unmoved tip: stamped %v, recomputed %v", e.Affordances, direct)
	}
}
