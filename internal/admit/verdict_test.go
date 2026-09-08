package admit

// The verdict admission drills (plans/os-f6d2c267.md;
// spec/verdicts.md): verdict.rendered is a fact admitted only on
// review subjects, bound to the submission that produced the review
// state, signed by a verdict-granted key disjoint from every
// implementing key on the contract (L1). Capability rides the grant
// rule (out_of_grant fires before independence), the fence rule
// refuses citations outside a claim window, and the state stays
// review: done still arrives only through merge.observed.

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

const testReceipt = "abababababababababababababababababababababababababababababababab"

// verdictKeys are the fixture's cast: root signs governance; worker
// (claim + verdict grants, the dual-role actor L1 exists to catch)
// claims and submits c-1; verifier holds a verdict grant only and has
// never touched a contract; worker2 and scribe are enrolled
// claim-capable colleagues for the prior-claimant and
// submission-signer drills.
type verdictKeys struct {
	signer, worker, verifier, worker2, scribe ed25519.PrivateKey
}

// verdictFixture drives contract c-1 to review and returns the
// context, the cast, the step closure, and c-1's bound submission
// position.
func verdictFixture(t *testing.T) (*Context, verdictKeys, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context, int) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := verdictKeys{signer: signer, worker: fixtureKey(t, 2), verifier: fixtureKey(t, 3), worker2: fixtureKey(t, 5), scribe: fixtureKey(t, 6)}
	all := []ed25519.PrivateKey{k.signer, k.worker, k.verifier, k.worker2, k.scribe}
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
	}{{k.worker, "worker"}, {k.verifier, "verifier"}, {k.worker2, "worker2"}, {k.scribe, "scribe"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
	}
	for _, g := range []struct {
		key ed25519.PrivateKey
		cap string
	}{{k.worker, keyring.CapClaim}, {k.worker, keyring.CapVerdict}, {k.verifier, keyring.CapVerdict},
		{k.worker2, keyring.CapClaim}, {k.scribe, keyring.CapClaim}, {k.scribe, keyring.CapVerdict}} {
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
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	fence := activeFence(t, ctx, "c-1")
	subPos := ctx.Count
	ctx = step(k.worker, version.Seed1, "submission.made", "c-1", submissionBody(fence))
	return ctx, k, step, subPos
}

func submissionBody(fence string) string {
	return fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["the thing works"], "decisions": [], "base": "abcdef0..abcdef0", "refs": [], "findings": []}}`, fence)
}

func verdictBody(verdict string, subPos int) string {
	return fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": "L1"}`, verdict, testReceipt, subPos)
}

func TestVerdictAdmitsForDisjointGrantedKey(t *testing.T) {
	ctx, k, step, subPos := verdictFixture(t)
	for _, v := range []string{"pass", "fail"} {
		if err := Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody(v, subPos), ctx.Tip)); err != nil {
			t.Fatalf("a %s verdict by a disjoint verdict-granted key admits: %v", v, err)
		}
	}
	// The verdict is a fact, not a transition: after it lands the
	// subject is still in review.
	ctx = step(k.verifier, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos))
	if s, ok := ctx.Lifecycle.State("c-1"); !ok || s.State != "review" {
		t.Fatalf("a rendered verdict changes no state; got %+v", s)
	}
}

func TestVerdictIndependenceRefusesImplementingKeys(t *testing.T) {
	ctx, k, _, subPos := verdictFixture(t)
	// The holder-submitter, verdict grant and all: refused 17.
	var nie *NotIndependentError
	err := Check(ctx, draftV(t, k.worker, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos), ctx.Tip))
	if !errors.As(err, &nie) || !strings.Contains(nie.Role, "claimant") {
		t.Fatalf("the claimant's own verdict refuses not_independent, got %v", err)
	}
}

func TestVerdictIndependenceRefusesPriorClaimant(t *testing.T) {
	ctx, k, step, _ := verdictFixture(t)
	// A second worker takes over after a release and submits; the
	// first worker is a prior claimant and stays disqualified.
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-2", filedBody)
	ctx = step(k.signer, version.Seed1, "contract.specified", "c-2", specBody)
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-2", `{}`)
	fence := activeFence(t, ctx, "c-2")
	ctx = step(k.worker, version.Seed1, "claim.released", "c-2", submissionBody(fence))
	ctx = step(k.worker2, version.Seed1, "claim.taken", "c-2", `{}`)
	fence2 := activeFence(t, ctx, "c-2")
	subPos := ctx.Count
	ctx = step(k.worker2, version.Seed1, "submission.made", "c-2", submissionBody(fence2))
	var nie *NotIndependentError
	err := Check(ctx, draftV(t, k.worker, version.Seed1, "verdict.rendered", "c-2", verdictBody("fail", subPos), ctx.Tip))
	if !errors.As(err, &nie) || !strings.Contains(nie.Role, "claimant") {
		t.Fatalf("a prior claimant cannot verdict the contract it once held, got %v", err)
	}
}

func TestVerdictIndependenceRefusesSubmissionSigner(t *testing.T) {
	ctx, k, step, _ := verdictFixture(t)
	// A claim-capable colleague who never claimed submits by citing
	// the active fence (the fence is ordering, not authorization);
	// that signature makes them an implementing key.
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-3", filedBody)
	ctx = step(k.signer, version.Seed1, "contract.specified", "c-3", specBody)
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-3", `{}`)
	fence := activeFence(t, ctx, "c-3")
	subPos := ctx.Count
	ctx = step(k.scribe, version.Seed1, "submission.made", "c-3", submissionBody(fence))
	var nie *NotIndependentError
	err := Check(ctx, draftV(t, k.scribe, version.Seed1, "verdict.rendered", "c-3", verdictBody("pass", subPos), ctx.Tip))
	if !errors.As(err, &nie) || !strings.Contains(nie.Role, "submission") {
		t.Fatalf("the bound submission's signer cannot verdict it, got %v", err)
	}
}

func TestVerdictCapabilityRowHasNoOperatorFallback(t *testing.T) {
	ctx, k, _, subPos := verdictFixture(t)
	// The governance root holds operator implicitly and no verdict
	// grant: out_of_grant, naming exactly the verdict lane.
	var oog *OutOfGrantError
	err := Check(ctx, draftV(t, k.signer, version.Seed1, "verdict.rendered", "c-1", verdictBody("pass", subPos), ctx.Tip))
	if !errors.As(err, &oog) || len(oog.Accepted) != 1 || oog.Accepted[0] != keyring.CapVerdict {
		t.Fatalf("a root without a verdict grant refuses out_of_grant naming [verdict], got %v", err)
	}
}

func TestVerdictRefusesOutsideReview(t *testing.T) {
	ctx, k, step, _ := verdictFixture(t)
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-9", filedBody)
	var itr *transition.InvalidTransitionError
	// backlog
	err := Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-9", verdictBody("pass", 1), ctx.Tip))
	if !errors.As(err, &itr) || itr.From != "backlog" {
		t.Fatalf("a verdict on a backlog subject refuses invalid_transition, got %v", err)
	}
	// in_progress
	ctx = step(k.signer, version.Seed1, "contract.specified", "c-9", specBody)
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-9", `{}`)
	err = Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-9", verdictBody("pass", 1), ctx.Tip))
	if !errors.As(err, &itr) || itr.From != "in_progress" {
		t.Fatalf("a verdict on an in_progress subject refuses invalid_transition, got %v", err)
	}
	// unknown subject
	err = Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-none", verdictBody("pass", 1), ctx.Tip))
	if !errors.As(err, &itr) || itr.From != "" {
		t.Fatalf("a verdict on an unknown subject refuses invalid_transition, got %v", err)
	}
}

func TestVerdictShapeAndBinding(t *testing.T) {
	ctx, k, _, subPos := verdictFixture(t)
	refuse := func(payload, want string) {
		t.Helper()
		err := Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-1", payload, ctx.Tip))
		var ve *VerdictError
		if !errors.As(err, &ve) || !strings.Contains(ve.Reason, want) {
			t.Fatalf("payload %s must refuse mentioning %q, got %v", payload, want, err)
		}
	}
	refuse(fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1", "extra": 1}`, testReceipt, subPos), "strict object")
	refuse(fmt.Sprintf(`{"verdict": "maybe", "receipt": %q, "submission": "%d", "independence": "L1"}`, testReceipt, subPos), "neither literal")
	refuse(fmt.Sprintf(`{"verdict": "pass", "receipt": "xyz", "submission": "%d", "independence": "L1"}`, subPos), "sha256")
	refuse(fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L2"}`, testReceipt, subPos), "L1")
	refuse(fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`, testReceipt, subPos+7), "bound submission")
	var inc *transition.IncompleteError
	err := Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-1", `{"verdict": "pass"}`, ctx.Tip))
	if !errors.As(err, &inc) || len(inc.Missing) != 3 {
		t.Fatalf("missing fields refuse incomplete naming them, got %v", err)
	}
}

func TestVerdictCitingFenceRefusesOutsideClaimWindow(t *testing.T) {
	ctx, k, _, subPos := verdictFixture(t)
	// Review is outside in_progress: no fence is active, and a fence
	// dies with its claim window.
	payload := fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1", "fence": "9"}`, testReceipt, subPos)
	var fe *FenceError
	err := Check(ctx, draftV(t, k.verifier, version.Seed1, "verdict.rendered", "c-1", payload, ctx.Tip))
	if !errors.As(err, &fe) || fe.Active >= 0 {
		t.Fatalf("a verdict citing a fence outside a claim window refuses fenced, got %v", err)
	}
}
