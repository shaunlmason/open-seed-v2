package admit

// The red-verdict lockout, return-path, and override admission drills
// (plans/os-d2497eb7.md): pass over a fail-judged submission refuses
// until a new submission, and only authenticated fails lock; the
// return cites the authorizing red verdict; the override is the
// operator's attributable substitute, admitted only over a standing
// validated fail; the chain accepts the override-backed path with no
// step collapsed.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

type lockoutKeys struct {
	signer, worker, worker2, verifier, dispatcher, plain ed25519.PrivateKey
}

// lockoutFixture drives c-1 to review (claimed and submitted by
// worker) with a granted verifier, a dispatcher, a second worker, and
// a grantless key.
func lockoutFixture(t *testing.T) (*Context, lockoutKeys, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context, int) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := lockoutKeys{signer: signer, worker: fixtureKey(t, 2), worker2: fixtureKey(t, 5),
		verifier: fixtureKey(t, 3), dispatcher: fixtureKey(t, 8), plain: fixtureKey(t, 6)}
	all := []ed25519.PrivateKey{k.signer, k.worker, k.worker2, k.verifier, k.dispatcher, k.plain}
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
	}{{k.worker, "worker"}, {k.worker2, "worker2"}, {k.verifier, "verifier"}, {k.dispatcher, "dispatcher"}, {k.plain, "plain"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
	}
	for _, g := range []struct {
		key ed25519.PrivateKey
		cap string
	}{{k.worker, keyring.CapClaim}, {k.worker, keyring.CapVerdict}, {k.worker2, keyring.CapClaim}, {k.verifier, keyring.CapVerdict}, {k.dispatcher, keyring.CapDispatch}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, g.key), `{"capability": "`+g.cap+`"}`)
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
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	s, _ := ctx.Lifecycle.State("c-1")
	sub := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, s.Claim.Fence, minPacket)
	ctx = step(k.worker, version.Seed1, "submission.made", "c-1", sub)
	s, _ = ctx.Lifecycle.State("c-1")
	return ctx, k, step, s.Submission.Pos
}

func TestRedVerdictLockout(t *testing.T) {
	ctx, k, step, subPos := lockoutFixture(t)
	ctx = step(k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", subPos))

	// Pass over the fail-judged submission refuses by name; a fail
	// restatement admits.
	var ve *VerdictError
	err := Check(ctx, draftV(t, k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("pass", subPos), ctx.Tip))
	if !errors.As(err, &ve) || !strings.Contains(ve.Reason, "locks pass out") {
		t.Fatalf("pass over a fail-judged submission refuses: %v", err)
	}
	if err := Check(ctx, draftV(t, k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", subPos), ctx.Tip)); err != nil {
		t.Fatalf("a fail restatement admits: %v", err)
	}
	// A red verdict is unmergeable (6.2, unchanged): the request
	// citing the fail refuses.
	s, _ := ctx.Lifecycle.State("c-1")
	var ce *transition.ChainError
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "unmergeable") {
		t.Fatalf("a red verdict stays unmergeable: %v", err)
	}

	// The return path: worker and plain lanes refuse; dispatcher
	// admits with the fail citation; wrong citations refuse.
	failPos := s.Verdict.Pos
	ret := fmt.Sprintf(`{"verdict": "%d"}`, failPos)
	for name, priv := range map[string]ed25519.PrivateKey{"claim": k.worker, "plain": k.plain} {
		if err := Check(ctx, draftV(t, priv, version.Seed1, transition.ContractReturnedVerb, "c-1", ret, ctx.Tip)); err == nil || !strings.Contains(err.Error(), "not granted any of") {
			t.Fatalf("%s lane must not return contracts: %v", name, err)
		}
	}
	err = Check(ctx, draftV(t, k.dispatcher, version.Seed1, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, subPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "not a fail verdict") {
		t.Fatalf("citing a non-verdict position refuses: %v", err)
	}
	if err := Check(ctx, draftV(t, k.dispatcher, version.Seed1, transition.ContractReturnedVerb, "c-1", `{"verdict": "9", "x": 1}`, ctx.Tip)); err == nil {
		t.Fatal("a loose return payload must refuse strict")
	}
	if err := Check(ctx, draftV(t, k.dispatcher, version.Seed1, transition.ContractReturnedVerb, "c-1", ret, ctx.Tip)); err != nil {
		t.Fatalf("the dispatcher's cited return admits: %v", err)
	}

	// The unlock: return, re-claim, resubmit — pass admits on the new
	// submission.
	ctx = step(k.dispatcher, version.Seed1, transition.ContractReturnedVerb, "c-1", ret)
	if s, _ = ctx.Lifecycle.State("c-1"); s.State != "ready" || len(s.SubmissionFails) != 1 {
		t.Fatalf("the return lands in ready with the window intact until resubmission: %s, %d fails", s.State, len(s.SubmissionFails))
	}
	ctx = step(k.worker2, version.Seed1, "claim.taken", "c-1", `{}`)
	s, _ = ctx.Lifecycle.State("c-1")
	ctx = step(k.worker2, version.Seed1, "submission.made", "c-1", fmt.Sprintf(`{"fence": "%d", "packet": %s}`, s.Claim.Fence, minPacket))
	s, _ = ctx.Lifecycle.State("c-1")
	if len(s.SubmissionFails) != 0 || s.Override != nil {
		t.Fatalf("a new submission opens a new window: %+v", s.SubmissionFails)
	}
	if err := Check(ctx, draftV(t, k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("pass", s.Submission.Pos), ctx.Tip)); err != nil {
		t.Fatalf("pass admits on the new submission: %v", err)
	}
}

func TestUnauthenticatedFailLocksNothing(t *testing.T) {
	ctx, k, step, subPos := lockoutFixture(t)
	// A raw-pushed fail by the grantless key folds into the window but
	// locks nothing and authorizes nothing.
	ctx = step(k.plain, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", subPos))
	s, _ := ctx.Lifecycle.State("c-1")
	if len(s.SubmissionFails) != 1 {
		t.Fatalf("the raw fail folds into the window: %+v", s.SubmissionFails)
	}
	if err := Check(ctx, draftV(t, k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("pass", subPos), ctx.Tip)); err != nil {
		t.Fatalf("an unauthenticated fail must not block a legitimate pass: %v", err)
	}
	var ce *transition.ChainError
	err := Check(ctx, draftV(t, k.dispatcher, version.Seed1, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "no verdict grant") {
		t.Fatalf("a return citing the raw fail refuses at the boundary: %v", err)
	}
	err = Check(ctx, draftV(t, k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", fmt.Sprintf(`{"reason": "ship it", "verdict": "%d"}`, s.Verdict.Pos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "no verdict grant") {
		t.Fatalf("an override citing the raw fail refuses at the boundary: %v", err)
	}
	// The implementer-signed variant: worker raw-pushes a fail on its
	// own submission; the boundary names the implementing key.
	ctx = step(k.worker, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", subPos))
	s, _ = ctx.Lifecycle.State("c-1")
	err = Check(ctx, draftV(t, k.dispatcher, version.Seed1, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "implementing key") {
		t.Fatalf("an implementer-signed fail authorizes nothing: %v", err)
	}
}

func TestOverrideAdmissionMatrix(t *testing.T) {
	ctx, k, step, subPos := lockoutFixture(t)
	overrideBody := func(pos int) string {
		return fmt.Sprintf(`{"reason": "verifier wrong, evidence attached", "verdict": "%d"}`, pos)
	}
	var ce *transition.ChainError

	// No standing fail: the override refuses — an escape hatch, never
	// a bypass.
	err := Check(ctx, draftV(t, k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", overrideBody(subPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "not a fail verdict") {
		t.Fatalf("an override with no standing fail refuses: %v", err)
	}
	ctx = step(k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", subPos))
	s, _ := ctx.Lifecycle.State("c-1")
	failPos := s.Verdict.Pos

	// Lanes and shape.
	for name, priv := range map[string]ed25519.PrivateKey{"claim": k.worker, "dispatch": k.dispatcher, "plain": k.plain} {
		if err := Check(ctx, draftV(t, priv, version.Seed1, transition.MergeOverriddenVerb, "c-1", overrideBody(failPos), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "not granted any of") {
			t.Fatalf("%s lane must not override: %v", name, err)
		}
	}
	var inc *transition.IncompleteError
	err = Check(ctx, draftV(t, k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", fmt.Sprintf(`{"reason": "", "verdict": "%d"}`, failPos), ctx.Tip))
	if !errors.As(err, &inc) {
		t.Fatalf("an empty reason is incomplete — the override is attributable or nothing: %v", err)
	}
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", `{"reason": "x"}`, ctx.Tip)); !errors.As(err, &inc) {
		t.Fatalf("a citation-less override is incomplete: %v", err)
	}

	// The operator's cited override admits; the chain runs through it;
	// a second override in the window refuses.
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", overrideBody(failPos), ctx.Tip)); err != nil {
		t.Fatalf("the operator's override over the validated fail admits: %v", err)
	}
	ctx = step(k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", overrideBody(failPos))
	s, _ = ctx.Lifecycle.State("c-1")
	if s.Override == nil || s.Override.CitedVerdict != failPos {
		t.Fatalf("the override folds with its citation: %+v", s.Override)
	}
	err = Check(ctx, draftV(t, k.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", overrideBody(failPos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "already stands") {
		t.Fatalf("one override per submission window: %v", err)
	}

	// The request cites exactly one path; the override citation admits
	// from the work lane; both/neither refuse; the observed chain
	// lands done through the override.
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d", "override": "%d"}`, failPos, s.Override.Pos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "exactly one") {
		t.Fatalf("citing both paths refuses: %v", err)
	}
	err = Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"override": "%d"}`, s.Override.Pos+7), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "the admitted override") {
		t.Fatalf("a wrong override citation refuses: %v", err)
	}
	if err := Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"override": "%d"}`, s.Override.Pos), ctx.Tip)); err != nil {
		t.Fatalf("the override-backed request admits: %v", err)
	}
	ctx = step(k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"override": "%d"}`, s.Override.Pos))
	if err := Check(ctx, draftV(t, k.signer, version.Seed1, "merge.observed", "c-1", `{"merged": "`+strings.Repeat("ab", 20)+`", "pr": "pr/1"}`, ctx.Tip)); err != nil {
		t.Fatalf("the observed chain lands done through the override: %v", err)
	}
	ctx = step(k.signer, version.Seed1, "merge.observed", "c-1", `{"merged": "`+strings.Repeat("ab", 20)+`", "pr": "pr/1"}`)
	if s, _ = ctx.Lifecycle.State("c-1"); s.State != "done" {
		t.Fatalf("the override chain reaches done: %s", s.State)
	}
}

func TestRawOverrideSubstitutesForNothing(t *testing.T) {
	ctx, k, step, subPos := lockoutFixture(t)
	ctx = step(k.verifier, version.Seed1, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", subPos))
	s, _ := ctx.Lifecycle.State("c-1")
	// The grantless key raw-pushes a facts-complete override; the fold
	// records it (review window, first override), and the chain steps
	// refuse it at the operator boundary.
	ctx = step(k.plain, version.Seed1, transition.MergeOverriddenVerb, "c-1", fmt.Sprintf(`{"reason": "laundered", "verdict": "%d"}`, s.Verdict.Pos))
	s, _ = ctx.Lifecycle.State("c-1")
	if s.Override == nil {
		t.Fatal("the raw override folds as a fact — refusal happens at use")
	}
	var ce *transition.ChainError
	err := Check(ctx, draftV(t, k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"override": "%d"}`, s.Override.Pos), ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "no operator standing") {
		t.Fatalf("a request citing the raw override refuses: %v", err)
	}
	// A raw request over it plus the observation path: the observed
	// chain refuses at the same boundary.
	ctx = step(k.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"override": "%d"}`, s.Override.Pos))
	err = Check(ctx, draftV(t, k.signer, version.Seed1, "merge.observed", "c-1", `{"merged": "`+strings.Repeat("cd", 20)+`", "pr": "pr/2"}`, ctx.Tip))
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "no operator standing") {
		t.Fatalf("the observation over a raw override refuses: %v", err)
	}
	// An operator-capable raw override citing a non-fail folds too,
	// and the chain steps revalidate the citation: without a standing
	// authenticated fail it substitutes for nothing (review finding
	// on this PR).
	ctx2, k2, step2, subPos2 := lockoutFixture(t)
	ctx2 = step2(k2.signer, version.Seed1, transition.MergeOverriddenVerb, "c-1", fmt.Sprintf(`{"reason": "shortcut", "verdict": "%d"}`, subPos2))
	s2, _ := ctx2.Lifecycle.State("c-1")
	if s2.Override == nil {
		t.Fatal("the operator's raw override folds — refusal happens at the chain")
	}
	err2 := Check(ctx2, draftV(t, k2.worker, version.Seed1, "merge.requested", "c-1", fmt.Sprintf(`{"override": "%d"}`, s2.Override.Pos), ctx2.Tip))
	if !errors.As(err2, &ce) || !strings.Contains(ce.Reason, "not a fail verdict") {
		t.Fatalf("a request over an override with no authenticated fail refuses: %v", err2)
	}

	// A raw second override in the window stays an anomaly and the
	// fact stands.
	before, _ := ctx.Lifecycle.State("c-1")
	ctx = step(k.plain, version.Seed1, transition.MergeOverriddenVerb, "c-1", fmt.Sprintf(`{"reason": "again", "verdict": "%d"}`, s.Verdict.Pos))
	after, _ := ctx.Lifecycle.State("c-1")
	if after.Override.Pos != before.Override.Pos || after.Anomalies != before.Anomalies+1 {
		t.Fatalf("a raw second override is a counted anomaly, never the fact: %+v", after.Override)
	}
}
