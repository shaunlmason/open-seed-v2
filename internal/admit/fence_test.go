package admit

// The fence drills (plans/os-5dc16a7c.md step 6): the fence is the
// admitted claim.taken position; the holder's events and every
// deliberate exit cite it; missing and stale citations refuse exit 6
// naming cited, active, and holder; prior claimants stay fenced (a
// reaped worker cannot demote itself to observer); never-claimed
// signers observe freely; contention is the structured exit-2 refusal.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestFenceLifecycle(t *testing.T) {
	ctx, signer, worker, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	fence := activeFence(t, ctx, "c-1")

	// The holder's free event must cite the fence; missing refuses 6
	// naming the active fence and holder, stale refuses 6 naming both.
	err := Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-1", `{"n": 1}`, ctx.Tip))
	var fe *FenceError
	if !errors.As(err, &fe) || fe.Cited != "" || fmt.Sprintf("%d", fe.Active) != fence || fe.Holder != fpOf(t, worker) {
		t.Fatalf("the holder's fence-free event must refuse naming active and holder, got %v", err)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-1", `{"n": 1, "fence": "999"}`, ctx.Tip))
	if !errors.As(err, &fe) || fe.Cited != "999" {
		t.Fatalf("a stale citation must refuse naming the cited fence, got %v", err)
	}
	if err := Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-1", `{"n": 1, "fence": "`+fence+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("the holder citing the active fence must admit: %v", err)
	}

	// A never-claimed signer's observation passes with no fence, and
	// its citation of the active fence also passes; citing a wrong
	// fence refuses whoever signs.
	if err := Check(ctx, draftV(t, signer, version.Seed1, "message.sent", "c-1", `{"n": 2}`, ctx.Tip)); err != nil {
		t.Fatalf("a never-claimed signer observes freely: %v", err)
	}
	if err := Check(ctx, draftV(t, signer, version.Seed1, "message.sent", "c-1", `{"n": 2, "fence": "0"}`, ctx.Tip)); !errors.As(err, &fe) {
		t.Fatalf("any citation present must match the active fence, got %v", err)
	}

	// Exits cite the fence they end; after release the fence is dead:
	// free events need no citation, and citing the dead fence refuses.
	ctx = step(worker, version.Seed1, "claim.released", "c-1", `{"fence": "`+fence+`", "packet": `+minPacket+`}`)
	if err := Check(ctx, draftV(t, signer, version.Seed1, "message.sent", "c-1", `{"n": 3}`, ctx.Tip)); err != nil {
		t.Fatalf("no active claim, no fence required: %v", err)
	}
	err = Check(ctx, draftV(t, signer, version.Seed1, "message.sent", "c-1", `{"n": 3, "fence": "`+fence+`"}`, ctx.Tip))
	if !errors.As(err, &fe) || fe.Active != -1 {
		t.Fatalf("a fence dies with its claim window, got %v", err)
	}

	// The claiming verb itself asserts no fence: on an unheld subject
	// a claim.taken smuggling a citation refuses rather than admitting
	// an asserted or retired fence into a fresh window.
	err = Check(ctx, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{"fence": "`+fence+`"}`, ctx.Tip))
	if !errors.As(err, &fe) || fe.Active != -1 || fe.Cited != fence {
		t.Fatalf("a claimless claim.taken citation must refuse, got %v", err)
	}

	// A re-claim mints a NEW fence; the old one is stale for the new
	// holder's events.
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	fence2 := activeFence(t, ctx, "c-1")
	if fence2 == fence {
		t.Fatalf("a second claim must mint a new fence: %s vs %s", fence2, fence)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-1", `{"n": 4, "fence": "`+fence+`"}`, ctx.Tip))
	if !errors.As(err, &fe) || fe.Cited != fence {
		t.Fatalf("the retired fence must refuse stale, got %v", err)
	}
}

// conformance: III.F — prior claimants stay fenced (review finding on
// #114): a reaped worker's delayed observation cannot slip through
// fence-free while another claim is active; citing the active fence is
// the explicit acknowledgement a late event needs; after the claim
// window closes the same worker observes freely again.
func TestPriorClaimantsStayFenced(t *testing.T) {
	ctx, signer, workerA, workerB, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, workerA), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, workerB), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(workerA, version.Seed1, "claim.taken", "c-1", `{}`)
	fenceA := activeFence(t, ctx, "c-1")
	ctx = step(signer, version.Seed1, "claim.reaped", "c-1", `{"fence": "`+fenceA+`", "packet": `+minPacket+`}`)
	ctx = step(workerB, version.Seed1, "claim.taken", "c-1", `{}`)
	fenceB := activeFence(t, ctx, "c-1")

	// A's delayed milestone citing A's dead fence: stale. With no
	// citation: missing (A is a prior claimant, not an observer).
	var fe *FenceError
	err := Check(ctx, draftV(t, workerA, version.Seed1, "message.sent", "c-1", `{"n": 9, "fence": "`+fenceA+`"}`, ctx.Tip))
	if !errors.As(err, &fe) || fe.Cited != fenceA {
		t.Fatalf("a prior claimant's stale fence must refuse, got %v", err)
	}
	err = Check(ctx, draftV(t, workerA, version.Seed1, "message.sent", "c-1", `{"n": 9}`, ctx.Tip))
	if !errors.As(err, &fe) || fe.Cited != "" {
		t.Fatalf("a prior claimant cannot demote itself to observer, got %v", err)
	}
	// Citing B's active fence is the explicit acknowledgement.
	if err := Check(ctx, draftV(t, workerA, version.Seed1, "message.sent", "c-1", `{"n": 9, "fence": "`+fenceB+`"}`, ctx.Tip)); err != nil {
		t.Fatalf("a prior claimant citing the active fence admits: %v", err)
	}
	// After B releases there is no claim window: A observes freely.
	ctx = step(workerB, version.Seed1, "claim.released", "c-1", `{"fence": "`+fenceB+`", "packet": `+minPacket+`}`)
	if err := Check(ctx, draftV(t, workerA, version.Seed1, "message.sent", "c-1", `{"n": 10}`, ctx.Tip)); err != nil {
		t.Fatalf("outside a claim window a prior claimant is a plain observer: %v", err)
	}
}

func TestContentionIsStructured(t *testing.T) {
	ctx, signer, workerA, workerB, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, workerA), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, workerB), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(workerA, version.Seed1, "claim.taken", "c-1", `{}`)
	fence := activeFence(t, ctx, "c-1")

	err := Check(ctx, draftV(t, workerB, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip))
	var ce *ContentionError
	if !errors.As(err, &ce) || ce.Holder != fpOf(t, workerA) || fmt.Sprintf("%d", ce.Fence) != fence || ce.Subject != "c-1" {
		t.Fatalf("a rival claim must refuse structured contention naming holder and fence, got %v", err)
	}
	// The holder re-claiming its own held contract is contention too:
	// one claim at a time, whoever asks.
	if err := Check(ctx, draftV(t, workerA, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); !errors.As(err, &ce) {
		t.Fatalf("even the holder cannot double-claim, got %v", err)
	}

	// The tolerant fold keeps claim facts coherent for raw-pushed
	// violations: a raw second claim is skipped, the original holder
	// and fence survive.
	ctx = step(workerB, version.Seed1, "claim.taken", "c-1", `{}`)
	s, ok := ctx.Lifecycle.State("c-1")
	if !ok || s.Claim == nil || s.Claim.Holder != fpOf(t, workerA) || fmt.Sprintf("%d", s.Claim.Fence) != fence || s.Anomalies != 1 {
		t.Fatalf("the fold must keep the original claim through a raw contention push: %+v", s)
	}
}
