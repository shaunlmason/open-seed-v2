package admit

// The lifecycle admission rule (plans/os-d69a6c91.md steps 3 and 7):
// legality is the transition table applied as admission policy at
// seed/1 — the happy path admits, every illegal jump refuses with the
// typed error naming subject, state, and verb, completeness is
// presence-checked at the shape level, every lifecycle verb is
// capability-gated, seed/0 is grandfathered inert, and raw-pushed
// illegal history is tolerated without corrupting the fold admission
// consults.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	filedBody = `{"intent": "fix the thing", "tier": "trivial", "budget": "small", "routing": "core"}`
	specBody  = `{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`
	// minPacket is the minimal honest packet: empty decisions, refs,
	// and findings, a zero-length base range (no work pushed).
	minPacket = `{"acceptance": ["resume from here"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
)

func TestLifecycleHappyPathAdmits(t *testing.T) {
	ctx, signer, worker, verifier, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)

	// filed -> specified -> taken -> made -> merge.observed, each
	// checked before it is appended, exactly the cooperative flow.
	if err := Check(ctx, draftV(t, signer, version.Seed1, "intent.filed", "c-1", filedBody, ctx.Tip)); err != nil {
		t.Fatalf("filing must admit: %v", err)
	}
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	if err := Check(ctx, draftV(t, signer, version.Seed1, "contract.specified", "c-1", specBody, ctx.Tip)); err != nil {
		t.Fatalf("specification must admit: %v", err)
	}
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	if err := Check(ctx, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("a claim-granted worker must take a ready contract: %v", err)
	}
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	made := `{"branch": "seed/c-1", "fence": "` + activeFence(t, ctx, "c-1") + `", "packet": ` + minPacket + `}`
	if err := Check(ctx, draftV(t, worker, version.Seed1, "submission.made", "c-1", made, ctx.Tip)); err != nil {
		t.Fatalf("submission must admit: %v", err)
	}
	ctx = step(worker, version.Seed1, "submission.made", "c-1", made)
	// Done is reachable only through the full reconciliation chain
	// (plans/os-6cdc15be.md): a disjoint verdict-granted key (the
	// fixture's third enrolled key, which never touched c-1) renders
	// pass, the work lane requests the merge citing it, and the
	// observation records the merged commit.
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, verifier), `{"capability": "`+keyring.CapVerdict+`"}`)
	verdictPos := ctx.Count
	verdictBody := `{"verdict": "pass", "receipt": "` + strings.Repeat("ab", 32) + `", "submission": "` + fmt.Sprintf("%d", subPosOf(t, ctx, "c-1")) + `", "independence": "L1"}`
	if err := Check(ctx, draftV(t, verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody, ctx.Tip)); err != nil {
		t.Fatalf("the pass verdict must admit: %v", err)
	}
	ctx = step(verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody)
	requested := fmt.Sprintf(`{"verdict": "%d"}`, verdictPos)
	if err := Check(ctx, draftV(t, worker, version.Seed1, "merge.requested", "c-1", requested, ctx.Tip)); err != nil {
		t.Fatalf("the merge request must admit: %v", err)
	}
	ctx = step(worker, version.Seed1, "merge.requested", "c-1", requested)
	observed := `{"merged": "` + strings.Repeat("cd", 20) + `", "pr": "1"}`
	if err := Check(ctx, draftV(t, signer, version.Seed1, "merge.observed", "c-1", observed, ctx.Tip)); err != nil {
		t.Fatalf("the done observation must admit: %v", err)
	}
	ctx = step(signer, version.Seed1, "merge.observed", "c-1", observed)

	// done is terminal: nothing moves the subject again.
	err := Check(ctx, draftV(t, signer, version.Seed1, "contract.cancelled", "c-1", `{}`, ctx.Tip))
	var inv *transition.InvalidTransitionError
	if !errors.As(err, &inv) || inv.From != "done" {
		t.Fatalf("a terminal subject refuses every lifecycle verb, got %v", err)
	}
}

func TestLifecycleIllegalJumpsRefuseTyped(t *testing.T) {
	ctx, signer, _, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)

	cases := map[string]struct {
		verb, subject, from, payload string
	}{
		"claim on backlog":          {"claim.taken", "c-1", "backlog", `{}`},
		"done from backlog":         {"merge.observed", "c-1", "backlog", `{}`},
		"birth on existing":         {"intent.filed", "c-1", "backlog", filedBody},
		"non-birth on fresh":        {"contract.blocked", "c-9", "", `{}`},
		"unblock what is unblocked": {"contract.unblocked", "c-1", "backlog", `{}`},
	}
	for name, c := range cases {
		err := Check(ctx, draftV(t, signer, version.Seed1, c.verb, c.subject, c.payload, ctx.Tip))
		var inv *transition.InvalidTransitionError
		if !errors.As(err, &inv) {
			t.Fatalf("%s must refuse with the typed transition error, got %v", name, err)
		}
		if inv.Subject != c.subject || inv.From != c.from || inv.Verb != c.verb {
			t.Fatalf("%s must name subject/state/verb, got %+v", name, inv)
		}
	}
}

func TestLifecycleDeliberateExits(t *testing.T) {
	ctx, signer, _, _, step := grantFixture(t)
	step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(signer, version.Seed1, "claim.taken", "c-1", `{}`)

	// Cancelling in_progress work is structurally impossible; the four
	// deliberate exits all admit from here (each citing the fence).
	err := Check(ctx, draftV(t, signer, version.Seed1, "contract.cancelled", "c-1", fenced(t, ctx, "c-1"), ctx.Tip))
	var inv *transition.InvalidTransitionError
	if !errors.As(err, &inv) || inv.From != "in_progress" {
		t.Fatalf("cancelling in_progress must be impossible, got %v", err)
	}
	for _, verb := range []string{"claim.released", "claim.parked", "claim.reaped", "submission.made"} {
		if err := Check(ctx, draftV(t, signer, version.Seed1, verb, "c-1", fencedExit(t, ctx, "c-1"), ctx.Tip)); err != nil {
			t.Fatalf("%s must admit from in_progress: %v", verb, err)
		}
	}
	// And each lands where the table says: park, unblock, re-take,
	// release, re-take, reap — folded through real appends, every
	// exit citing the fence of the claim it ends.
	ctx = step(signer, version.Seed1, "claim.parked", "c-1", fencedExit(t, ctx, "c-1"))
	if err := Check(ctx, draftV(t, signer, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err == nil {
		t.Fatal("a parked (blocked) contract is not claimable")
	}
	ctx = step(signer, version.Seed1, "contract.unblocked", "c-1", `{}`)
	ctx = step(signer, version.Seed1, "claim.taken", "c-1", `{}`)
	ctx = step(signer, version.Seed1, "claim.released", "c-1", fencedExit(t, ctx, "c-1"))
	ctx = step(signer, version.Seed1, "claim.taken", "c-1", `{}`)
	ctx = step(signer, version.Seed1, "claim.reaped", "c-1", fencedExit(t, ctx, "c-1"))
	if err := Check(ctx, draftV(t, signer, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("released and reaped contracts return to ready and re-claim: %v", err)
	}
}

func TestLifecycleCompletenessPresence(t *testing.T) {
	ctx, signer, _, _, step := grantFixture(t)

	err := Check(ctx, draftV(t, signer, version.Seed1, "intent.filed", "c-1", `{"intent": "x", "tier": ""}`, ctx.Tip))
	var inc *transition.IncompleteError
	if !errors.As(err, &inc) || len(inc.Missing) != 3 {
		t.Fatalf("an incomplete filing must refuse naming the missing fields, got %v", err)
	}
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	err = Check(ctx, draftV(t, signer, version.Seed1, "contract.specified", "c-1", `{}`, ctx.Tip))
	var ae *transition.AcceptanceError
	if !errors.As(err, &ae) || ae.Field != "acceptance" {
		t.Fatalf("specification without an acceptance spec must refuse structurally, got %v", err)
	}
	// An unspecified contract is not claimable: the completeness gate
	// and the transition legality close the same door from two sides.
	err = Check(ctx, draftV(t, signer, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip))
	var inv *transition.InvalidTransitionError
	if !errors.As(err, &inv) || inv.From != "backlog" {
		t.Fatalf("claiming an unspecified contract must refuse, got %v", err)
	}

	// Executable content requires the gate at every tier; with gate
	// evidence bound to the revision the contract specifies and
	// becomes claimable end-to-end (plans/os-73c00a50.md).
	err = Check(ctx, draftV(t, signer, version.Seed1, "contract.specified", "c-1",
		`{"acceptance": {"ref": "specs/one.sh @ abc1234", "executable": true}}`, ctx.Tip))
	if !errors.As(err, &ae) || ae.Field != "acceptance.gate" {
		t.Fatalf("ungated executable content must refuse at every tier, got %v", err)
	}
	ctx = step(signer, version.Seed1, "contract.specified", "c-1",
		`{"acceptance": {"ref": "specs/one.sh @ abc1234", "executable": true, "gate": "77 @ abc1234"}}`)
	if err := Check(ctx, draftV(t, signer, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("a gated executable spec is claimable: %v", err)
	}
}

// conformance: III.F — every lifecycle verb is capability-gated: the
// claim lane and the dispatch lane are disjoint, an ungranted enrolled
// actor holds neither, and roots pass as implicit operator.
func TestLifecycleCapabilityLanes(t *testing.T) {
	ctx, signer, worker, maintainer, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, maintainer), `{"capability": "`+keyring.CapDispatch+`"}`)

	// The ungranted third actor first: refuse 14 on every lifecycle verb.
	ungranted := fixtureKey(t, 4)
	ctx = step(signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, ungranted), enrollBody(t, ungranted, "agent", "plain"))
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range tab.Verbs() {
		err := Check(ctx, draftV(t, ungranted, version.Seed1, verb, "c-x", filedBody, ctx.Tip))
		var oog *OutOfGrantError
		if !errors.As(err, &oog) {
			t.Fatalf("%s by an ungranted actor must refuse out of grant, got %v", verb, err)
		}
	}

	// Dispatch files and specifies; claim cannot.
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "intent.filed", "c-1", filedBody, ctx.Tip)); err != nil {
		t.Fatalf("dispatch files: %v", err)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "intent.filed", "c-9", filedBody, ctx.Tip))
	var oog *OutOfGrantError
	if !errors.As(err, &oog) {
		t.Fatalf("the claim lane cannot file, got %v", err)
	}
	ctx = step(maintainer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(maintainer, version.Seed1, "contract.specified", "c-1", specBody)

	// Claim takes; dispatch cannot.
	if err := Check(ctx, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("the claim lane takes ready work: %v", err)
	}
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("the dispatch lane cannot take claims, got %v", err)
	}
	ctx = step(worker, version.Seed1, "claim.taken", "c-1", `{}`)

	// Reaping is dispatch's; the worker lane cannot reap itself.
	if err := Check(ctx, draftV(t, worker, version.Seed1, "claim.reaped", "c-1", fencedExit(t, ctx, "c-1"), ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("the claim lane cannot reap, got %v", err)
	}
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "claim.reaped", "c-1", fencedExit(t, ctx, "c-1"), ctx.Tip)); err != nil {
		t.Fatalf("dispatch reaps: %v", err)
	}
	// Cancellation and the done observation are operator-only in v0.
	ctx = step(maintainer, version.Seed1, "claim.reaped", "c-1", fencedExit(t, ctx, "c-1"))
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, "contract.cancelled", "c-1", `{}`, ctx.Tip)); !errors.As(err, &oog) {
		t.Fatalf("dispatch cannot cancel, got %v", err)
	}
	if err := Check(ctx, draftV(t, signer, version.Seed1, "contract.cancelled", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("the root cancels as implicit operator: %v", err)
	}
}

func TestLifecycleInertAtSeedZero(t *testing.T) {
	store, resolve, signer := seededStore(t)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	_ = resolve
	// Before seed/1 the rule is grandfathered inert (the keyring
	// precedent): a lifecycle verb that would be illegal at seed/1
	// admits, and completeness is not enforced.
	if err := Check(ctx, draftV(t, signer, version.Protocol, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("seed/0 records are inert to the lifecycle rule, got %v", err)
	}
	if err := Check(ctx, draftV(t, signer, version.Protocol, "intent.filed", "c-1", `{"intent": "x"}`, ctx.Tip)); err != nil {
		t.Fatalf("seed/0 filings are not completeness-gated, got %v", err)
	}
}

func TestLifecycleTolerantFold(t *testing.T) {
	ctx, signer, _, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)

	// Raw-push an illegal claim (backlog is unclaimable): the appended
	// chain still verifies — lifecycle legality is admission policy,
	// not chain validity — and the fold admission consults skips it.
	ctx = step(signer, version.Seed1, "claim.taken", "c-1", `{}`)
	if s, ok := ctx.Lifecycle.State("c-1"); !ok || s.State != "backlog" || s.Anomalies != 1 {
		t.Fatalf("the fold must skip the illegal event visibly: %+v ok=%v", s, ok)
	}
	// The next legal step for a backlog subject is specification, and
	// it admits against the folded state, not the raw history.
	if err := Check(ctx, draftV(t, signer, version.Seed1, "contract.specified", "c-1", specBody, ctx.Tip)); err != nil {
		t.Fatalf("admission follows the fold, not the anomalous history: %v", err)
	}
}

// activeFence reads the live fence for a subject from the context's
// fold; the tests never mint positions by hand.
func activeFence(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Claim == nil {
		t.Fatalf("no active claim on %s", subject)
	}
	return fmt.Sprintf("%d", s.Claim.Fence)
}

// fenced is a minimal payload citing the subject's active fence.
func fenced(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	return `{"fence": "` + activeFence(t, ctx, subject) + `"}`
}

// fencedExit is a deliberate-exit payload: the fence citation plus the
// minimal honest packet every exit carries.
func fencedExit(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	return `{"fence": "` + activeFence(t, ctx, subject) + `", "packet": ` + minPacket + `}`
}

// subPosOf reads the fold's bound submission position for a subject.
func subPosOf(t *testing.T, ctx *Context, subject string) int {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Submission == nil {
		t.Fatalf("no bound submission on %s", subject)
	}
	return s.Submission.Pos
}
