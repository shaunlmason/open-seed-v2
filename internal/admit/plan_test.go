package admit

// The plan-gate drills (plans/os-16c1d142.md): above the trivial tier
// a submission needs an admitted plan.approved and must cite the plan
// anchor it implements (exit 16 plan_required); trivial contracts
// submit planless; plan.proposed rides the claim lane under the fence
// matrix; plan.approved is operator-attested.

import (
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const standardFiling = `{"intent": "fix it properly", "tier": "standard", "budget": "small", "routing": "core"}`

func TestPlanGateAboveTrivialTier(t *testing.T) {
	ctx, signer, worker, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", standardFiling)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	fence := activeFence(t, ctx, "c-1")
	made := `{"branch": "seed/c-1", "fence": "` + fence + `", "packet": ` + minPacket + `}`

	// No plan.approved: the submission refuses 16 naming the gap.
	err := Check(ctx, draftV(t, worker, version.Seed1, "submission.made", "c-1", made, ctx.Tip))
	var pre *transition.PlanRequiredError
	if !errors.As(err, &pre) || pre.Tier != "standard" || pre.Subject != "c-1" {
		t.Fatalf("a standard-tier submission without a plan must refuse plan_required, got %v", err)
	}

	// The claim holder proposes (fence cited: the fence matrix binds
	// free events from the holder); the approval is operator-only.
	proposed := `{"plan": "plans/c-1.md @ abc1234", "fence": "` + fence + `"}`
	if err := Check(ctx, draftV(t, worker, version.Seed1, "plan.proposed", "c-1", proposed, ctx.Tip)); err != nil {
		t.Fatalf("the claim holder proposes a plan: %v", err)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "plan.proposed", "c-1", `{"fence": "`+fence+`"}`, ctx.Tip))
	var inc *transition.IncompleteError
	if !errors.As(err, &inc) {
		t.Fatalf("a proposal without its plan anchor must refuse, got %v", err)
	}
	approval := `{"plan": "plans/c-1.md @ abc1234", "pr": "77 @ abc1234", "fence": "` + fence + `"}`
	var oog *OutOfGrantError
	if err := Check(ctx, draftV(t, worker, version.Seed1, "plan.approved", "c-1", approval, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("plan.approved is operator-attested in v0, got %v", err)
	}
	ctx = step(signer, version.Seed1, "plan.approved", "c-1", approval)

	// Approved but uncited still refuses; cited admits.
	err = Check(ctx, draftV(t, worker, version.Seed1, "submission.made", "c-1", made, ctx.Tip))
	if !errors.As(err, &pre) {
		t.Fatalf("an approved plan must still be cited by the submission, got %v", err)
	}
	// Citing any anchor but THE approved one refuses: an approval
	// admits one exact revision, and the refusal names both.
	mismatched := `{"branch": "seed/c-1", "plan": "plans/c-1.md @ def5678", "fence": "` + fence + `", "packet": ` + minPacket + `}`
	err = Check(ctx, draftV(t, worker, version.Seed1, "submission.made", "c-1", mismatched, ctx.Tip))
	if !errors.As(err, &pre) || !strings.Contains(pre.Missing, "def5678") || !strings.Contains(pre.Missing, "abc1234") {
		t.Fatalf("a mismatched plan citation must refuse naming both anchors, got %v", err)
	}
	cited := `{"branch": "seed/c-1", "plan": "plans/c-1.md @ abc1234", "fence": "` + fence + `", "packet": ` + minPacket + `}`
	if err := Check(ctx, draftV(t, worker, version.Seed1, "submission.made", "c-1", cited, ctx.Tip)); err != nil {
		t.Fatalf("a planned, cited submission admits: %v", err)
	}
}

func TestPlanGateTrivialTierExempt(t *testing.T) {
	ctx, signer, _, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody) // trivial tier
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(signer, version.Seed1, "claim.taken", "c-1", `{}`)
	made := fencedExit(t, ctx, "c-1")
	if err := Check(ctx, draftV(t, signer, version.Seed1, "submission.made", "c-1", made, ctx.Tip)); err != nil {
		t.Fatalf("a trivial-tier contract submits without a plan: %v", err)
	}
}
