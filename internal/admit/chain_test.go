package admit

// The reconciliation-chain drills (plans/os-6cdc15be.md;
// spec/reconciliation.md): merge.requested admits only in review
// citing the admitted pass verdict; merge.observed records the forge
// fact behind the full chain rule; the capability lanes hold; and
// raw-pushed skipped links fold tolerated with anomalies counted,
// which is what divergence detection exists to catch.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const observedSHA = "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"

func observedBody(sha string) string {
	return fmt.Sprintf(`{"merged": %q, "pr": "pr/9"}`, sha)
}

func TestMergeRequestedCitesThePassVerdict(t *testing.T) {
	ctx, k, step, subPos := verdictFixture(t)

	// The chain starts at verdict.rendered(pass): with no verdict at
	// all, a request has nothing to cite.
	var ce *transition.ChainError
	err := Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", `{"verdict": "3"}`, ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "no verdict") {
		t.Fatalf("a request before any verdict refuses, got %v", err)
	}

	// A fail verdict is unmergeable at the chain itself.
	failPos := ctx.Count
	ctx = step(k.verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody("fail", subPos))
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, failPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "unmergeable") {
		t.Fatalf("citing a fail verdict refuses as unmergeable, got %v", err)
	}

	// A later pass verdict is the one to cite; the stale fail position
	// no longer names the admitted verdict.
	passPos := ctx.Count
	ctx = step(k.verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos))
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, failPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, fmt.Sprintf("position %d", passPos)) {
		t.Fatalf("citing a stale position refuses naming the admitted verdict, got %v", err)
	}

	// Shape: a missing citation refuses under the citation-choice rule
	// (exactly one of verdict or override, plans/os-d2497eb7.md); a
	// non-position refuses; an unknown key refuses strict.
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", `{}`, ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "exactly one") {
		t.Fatalf("a citation-less request refuses under the citation choice, got %v", err)
	}
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", `{"verdict": "vast"}`, ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "position") {
		t.Fatalf("a non-position citation refuses, got %v", err)
	}
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d", "extra": 1}`, passPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "strict") {
		t.Fatalf("an unknown key refuses strict, got %v", err)
	}

	// A fence citation refuses, as the strict shape rather than as a
	// fence complaint: the merge chain is subject-scoped and its
	// payload has no citation slot, so "fence" is an unknown key like
	// any other and the fence rule leaves the chain alone
	// (plans/os-873b5153.md D8).
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d", "fence": "9"}`, passPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "strict") {
		t.Fatalf("a fence citation outside a claim window refuses strict, got %v", err)
	}

	// The work lane requests; a verdict-only key holds no claim
	// capability.
	var oog *OutOfGrantError
	err = Check(ctx, draftV(t, k.verifier, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, passPos), ctx.Tip))
	if !errors.As(err, &oog) {
		t.Fatalf("a verdict-only key cannot request the merge, got %v", err)
	}
	if err := Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, passPos), ctx.Tip)); err != nil {
		t.Fatalf("the claim lane's request citing the pass verdict admits: %v", err)
	}

	// Outside review the request is illegal in that state.
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-b", filedBody)
	var itr *transition.InvalidTransitionError
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-b", `{"verdict": "1"}`, ctx.Tip))
	if !errors.As(err, &itr) || itr.From != "backlog" {
		t.Fatalf("a request outside review refuses invalid_transition, got %v", err)
	}
}

func TestMergeObservedNeedsTheFullChain(t *testing.T) {
	ctx, k, step, subPos := verdictFixture(t)
	ctx = step(k.signer, version.Seed1, keyring.VerbGranted, fpOf(t, k.worker2), `{"capability": "`+keyring.CapObserver+`"}`)

	// Without a pass verdict the observation refuses at the chain.
	var ce *transition.ChainError
	err := Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "no pass verdict") {
		t.Fatalf("an observation without a verdict refuses, got %v", err)
	}

	passPos := ctx.Count
	ctx = step(k.verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos))

	// With the verdict but no request, each chain step being its own
	// event still holds.
	err = Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "merge.requested") {
		t.Fatalf("an observation without the request refuses, got %v", err)
	}
	ctx = step(k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, passPos))

	// Shape: both fields presence-checked; the sha is a full
	// lowercase-hex commit; unknown keys refuse strict.
	var inc *transition.IncompleteError
	err = Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", `{"pr": "pr/9"}`, ctx.Tip))
	if !errors.As(err, &inc) {
		t.Fatalf("an observation without the merged sha refuses incomplete, got %v", err)
	}
	err = Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", observedBody("HEAD"), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "commit") {
		t.Fatalf("a non-hex merged value refuses, got %v", err)
	}
	err = Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", `{"merged": "`+observedSHA+`", "pr": "pr/9", "x": 1}`, ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "strict") {
		t.Fatalf("an unknown key refuses strict, got %v", err)
	}

	// The lanes: claim-only cannot observe; the observer lane and the
	// root (operator) can.
	var oog *OutOfGrantError
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip))
	if !errors.As(err, &oog) {
		t.Fatalf("a claim-lane key cannot observe the merge, got %v", err)
	}
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip)); err != nil {
		t.Fatalf("the operator observation admits: %v", err)
	}
	if err := Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip)); err != nil {
		t.Fatalf("the observer-lane observation admits: %v", err)
	}
	ctx = step(k.worker2, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA))

	// The fold recorded the chain facts, and done is terminal: the
	// observation is singular by construction.
	s, ok := ctx.Lifecycle.State("c-1")
	if !ok || s.State != "done" || s.Merged == nil || s.Merged.SHA != observedSHA {
		t.Fatalf("the fold records the merged commit: %+v", s)
	}
	if s.Verdict == nil || s.Verdict.Verdict != "pass" || s.Requested == nil || s.Requested.CitedVerdict != s.Verdict.Pos {
		t.Fatalf("the fold records the verdict and request facts: %+v", s)
	}
	var itr *transition.InvalidTransitionError
	err = Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip))
	if !errors.As(err, &itr) || itr.From != "done" {
		t.Fatalf("a second observation refuses on the terminal state, got %v", err)
	}
}

func TestRawPushedSkippedLinksFoldTolerated(t *testing.T) {
	// A raw-pushed merge.observed with no verdict and no request is
	// tolerated history: the transition applies (done happened), the
	// anomaly is counted, and divergence detection is what surfaces
	// it (spec/reconciliation.md).
	ctx, k, step, _ := verdictFixture(t)
	ctx = step(k.signer, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA))
	s, ok := ctx.Lifecycle.State("c-1")
	if !ok || s.State != "done" {
		t.Fatalf("the tolerant fold applies the raw-pushed observation: %+v", s)
	}
	if s.Verdict != nil || s.Requested != nil {
		t.Fatalf("no chain facts exist for the skipped links: %+v", s)
	}
	if s.Merged == nil || s.Merged.SHA != observedSHA {
		t.Fatalf("the merged commit is still recorded for reconciliation: %+v", s)
	}
	if s.Anomalies == 0 {
		t.Fatalf("skipped chain links are counted visibly, never silently: %+v", s)
	}
}

func TestLaunderedVerdictRefusesAtTheChain(t *testing.T) {
	// A raw-pushed verdict that never passed the verifier boundary
	// must not be launderable through the ADMITTED chain steps
	// (review finding on the 6.2 task PR).
	ctx, k, step, subPos := verdictFixture(t)

	// worker2 holds claim but no verdict grant: its raw-pushed pass
	// verdict folds, and the admitted request refuses on the grant.
	rawPos := ctx.Count
	ctx = step(k.worker2, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos))
	var ce *transition.ChainError
	err := Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, rawPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "verdict grant") {
		t.Fatalf("an ungranted raw verdict cannot be laundered through the request, got %v", err)
	}

	// The worker's own raw verdict (grant held, independence not):
	// refused on the implementing key — and the observation path is
	// equally closed when the request itself was raw-pushed.
	implPos := ctx.Count
	ctx = step(k.worker, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos))
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, implPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "implementing key") {
		t.Fatalf("an implementing key's raw verdict cannot be laundered, got %v", err)
	}
	ctx = step(k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, implPos))
	ctx = step(k.signer, version.Seed1, keyring.VerbGranted, fpOf(t, k.worker2), `{"capability": "`+keyring.CapObserver+`"}`)
	err = Check(ctx, draftV(t, k.worker2, version.Seed1, "merge.observed", "c-1", observedBody(observedSHA), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "implementing key") {
		t.Fatalf("the observation refuses the laundered chain too, got %v", err)
	}
}
