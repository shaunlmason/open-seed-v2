package admit

// The summarization-boundary drills (plans/os-2ff8dbf1.md): milestones
// are coarse (25-position spacing), monotonic (counts strictly
// advance), claim-lane facts under the fence matrix; wedge.declared is
// operator-gated with a presence-checked payload and changes no state.
// The ephemeral streams themselves never reach admission: only these
// summaries do.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestMilestoneSummarizationBoundary(t *testing.T) {
	ctx, signer, worker, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	fence := activeFence(t, ctx, "c-1")
	milestone := func(count int) string {
		return fmt.Sprintf(`{"count": %d, "step": "building", "fence": %q}`, count, fence)
	}

	// Shape: count and step are presence-checked.
	var inc *transition.IncompleteError
	err := Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", `{"fence": "`+fence+`"}`, ctx.Tip))
	if !errors.As(err, &inc) {
		t.Fatalf("a milestone without count and step must refuse incomplete, got %v", err)
	}

	// The claim lane under the fence matrix: the holder's milestone
	// cites the active fence, and an uncited one refuses fenced.
	var fe *FenceError
	err = Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", `{"count": 1, "step": "building"}`, ctx.Tip))
	if !errors.As(err, &fe) {
		t.Fatalf("the holder's milestone must cite the active fence, got %v", err)
	}
	if err := Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", milestone(1), ctx.Tip)); err != nil {
		t.Fatalf("the first cited milestone admits: %v", err)
	}
	first := ctx.Count // the position the milestone lands at
	ctx = step(worker, version.Seed1, "progress.milestone", "c-1", milestone(1))

	// Monotonic: equal and lower counts refuse whatever the spacing.
	var me *transition.MilestoneError
	err = Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", milestone(1), ctx.Tip))
	if !errors.As(err, &me) || !strings.Contains(me.Reason, "monotonic") {
		t.Fatalf("an equal count must refuse monotonic, got %v", err)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", milestone(0), ctx.Tip))
	if !errors.As(err, &me) {
		t.Fatalf("a lower count must refuse, got %v", err)
	}

	// Spacing: at 24 positions since the last milestone the next one
	// refuses; at 25 it admits. Position spacing, never timestamps.
	for ctx.Count-first < transition.MinMilestoneSpacing-1 {
		ctx = step(signer, version.Seed1, "message.sent", "c-other", `{"n": 1}`)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", milestone(2), ctx.Tip))
	if !errors.As(err, &me) || !strings.Contains(me.Reason, "spacing") {
		t.Fatalf("24-position spacing must refuse, got %v", err)
	}
	ctx = step(signer, version.Seed1, "message.sent", "c-other", `{"n": 2}`)
	if err := Check(ctx, draftV(t, worker, version.Seed1, "progress.milestone", "c-1", milestone(2), ctx.Tip)); err != nil {
		t.Fatalf("25-position spacing admits: %v", err)
	}
}

func TestWedgeDeclaredOperatorFact(t *testing.T) {
	ctx, signer, worker, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)

	// Presence: the declaration carries its evidence.
	var inc *transition.IncompleteError
	err := Check(ctx, draftV(t, signer, version.Seed1, "wedge.declared", "c-1", `{"observed": "2026-09-01T01:00:00Z"}`, ctx.Tip))
	if !errors.As(err, &inc) || !strings.Contains(strings.Join(inc.Missing, ","), "count") {
		t.Fatalf("a wedge declaration without its evidence must refuse, got %v", err)
	}

	// Operator-gated in v0: the claim lane cannot declare wedges.
	body := `{"observed": "2026-09-01T01:00:00Z", "count": 5, "since": "2026-09-01T00:20:00Z"}`
	var oog *OutOfGrantError
	err = Check(ctx, draftV(t, worker, version.Seed1, "wedge.declared", "c-1", body, ctx.Tip))
	if !errors.As(err, &oog) {
		t.Fatalf("wedge.declared is operator-gated, got %v", err)
	}

	// The operator's declaration admits and changes no lifecycle
	// state: a wedge is a fact, and the claim exit stays the
	// deliberate one.
	before, ok := ctx.Lifecycle.State("c-1")
	if !ok {
		t.Fatal("fixture broke: c-1 must exist")
	}
	ctx = step(signer, version.Seed1, "wedge.declared", "c-1", body)
	after, ok := ctx.Lifecycle.State("c-1")
	if !ok || after.State != before.State || after.Anomalies != before.Anomalies {
		t.Fatalf("wedge.declared must change no state: %+v -> %+v", before, after)
	}
}
