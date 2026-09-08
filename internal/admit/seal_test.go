package admit

// The sealed-checks admission drills (plans/os-3128535a.md;
// spec/sealed-checks.md): check.sealed admits only in ready with
// no prior claim, one commitment per subject, sealer lane only (no
// operator fallback, the verdict-lane posture); the seal author's
// later claim refuses; raw-pushed seals outside the legal window fold
// as anomalies, never facts.

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const testCommitment = "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"

type sealKeys struct {
	signer, sealer, worker, worker2, plain ed25519.PrivateKey
}

// sealFixture enrolls a sealer (sealer grant only), two workers
// (claim), and a grantless key, then files and specifies c-1 (ready).
func sealFixture(t *testing.T) (*Context, sealKeys, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := sealKeys{signer: signer, sealer: fixtureKey(t, 7), worker: fixtureKey(t, 2), worker2: fixtureKey(t, 5), plain: fixtureKey(t, 6)}
	all := []ed25519.PrivateKey{k.signer, k.sealer, k.worker, k.worker2, k.plain}
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
	}{{k.sealer, "sealer"}, {k.worker, "worker"}, {k.worker2, "worker2"}, {k.plain, "plain"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
	}
	for _, g := range []struct {
		key ed25519.PrivateKey
		cap string
	}{{k.sealer, keyring.CapSealer}, {k.worker, keyring.CapClaim}, {k.worker2, keyring.CapClaim}} {
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
	return ctx, k, step
}

func sealBody(commitment string) string {
	return fmt.Sprintf(`{"commitment": %q}`, commitment)
}

func TestSealAdmissionMatrix(t *testing.T) {
	ctx, k, step := sealFixture(t)

	// The lanes: sealer only. Operator (the root signer), claim, and
	// plain standing all refuse before any state logic runs.
	for name, priv := range map[string]ed25519.PrivateKey{"operator": k.signer, "claim": k.worker, "plain": k.plain} {
		err := Check(ctx, draftV(t, priv, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment), ctx.Tip))
		if err == nil || !strings.Contains(err.Error(), "not granted any of [sealer]") {
			t.Fatalf("%s must be out of the sealer lane: %v", name, err)
		}
	}

	// Shape refusals on the ready subject.
	for name, body := range map[string]string{
		"extra key":        `{"commitment": "` + testCommitment + `", "note": "x"}`,
		"empty commitment": `{"commitment": ""}`,
		"short commitment": `{"commitment": "abc123"}`,
		"wrong type":       `{"commitment": 7}`,
	} {
		if err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-1", body, ctx.Tip)); err == nil {
			t.Fatalf("%s must refuse", name)
		}
	}

	// Unknown subject and pre-ready state.
	if err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-none", sealBody(testCommitment), ctx.Tip)); err == nil {
		t.Fatal("a seal on an unknown subject must refuse")
	}
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-2", filedBody)
	if err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-2", sealBody(testCommitment), ctx.Tip)); err == nil {
		t.Fatal("a seal in backlog must refuse — the commitment window opens at ready")
	}

	// The happy path admits in ready, and a second commitment refuses.
	if err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment), ctx.Tip)); err != nil {
		t.Fatalf("the sealer's commitment in ready must admit: %v", err)
	}
	ctx = step(k.sealer, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment))
	err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-1", sealBody(strings.Repeat("ef", 32)), ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "already stands") {
		t.Fatalf("a second commitment must refuse by name: %v", err)
	}

	// In progress and review refuse.
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	if err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment), ctx.Tip)); err == nil {
		t.Fatal("a seal on an in_progress subject must refuse")
	}
}

func TestSealRefusedAfterAnyPriorClaim(t *testing.T) {
	// The release-path laundering hole (review finding on the 6.3
	// plan): claim.released lands the subject back in ready, and a
	// commitment appended there proves nothing.
	ctx, k, step := sealFixture(t)
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	s, _ := ctx.Lifecycle.State("c-1")
	release := fmt.Sprintf(`{"fence": "%d", "packet": %s}`, s.Claim.Fence, minPacket)
	ctx = step(k.worker, version.Seed1, "claim.released", "c-1", release)
	if got, _ := ctx.Lifecycle.State("c-1"); got.State != "ready" {
		t.Fatalf("release must land the subject back in ready, got %s", got.State)
	}
	err := Check(ctx, draftV(t, k.sealer, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment), ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "already been claimed") {
		t.Fatalf("a seal after a prior claim must refuse by name: %v", err)
	}
}

func TestSealAuthorCannotClaim(t *testing.T) {
	// The per-subject half of authoring isolation, drilled through raw
	// history: grant disjointness blocks a cooperative sealer from
	// ever holding claim, so the raw-pushed case is the reachable one.
	// worker raw-pushes a seal in the legal window (the fold captures
	// it — verification cannot refuse), and worker's own cooperative
	// claim then refuses while worker2's admits.
	ctx, k, step := sealFixture(t)
	ctx = step(k.worker, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment))
	s, _ := ctx.Lifecycle.State("c-1")
	if s.Sealed == nil || s.Sealed.Signer != fpOf(t, k.worker) {
		t.Fatalf("the raw seal in its legal window folds as the fact: %+v", s.Sealed)
	}
	err := Check(ctx, draftV(t, k.worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "authored this subject's sealed checks") {
		t.Fatalf("the seal author's claim must refuse by name: %v", err)
	}
	if err := Check(ctx, draftV(t, k.worker2, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("a non-author's claim on the sealed subject must admit: %v", err)
	}
}

func TestRawSealsOutsideTheWindowFoldAsAnomalies(t *testing.T) {
	ctx, k, step := sealFixture(t)
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	before, _ := ctx.Lifecycle.State("c-1")
	// Raw-pushed post-claim seal: no fact, one visible anomaly.
	ctx = step(k.plain, version.Seed1, "check.sealed", "c-1", sealBody(testCommitment))
	s, _ := ctx.Lifecycle.State("c-1")
	if s.Sealed != nil {
		t.Fatalf("a post-claim seal must never become the fact: %+v", s.Sealed)
	}
	if s.Anomalies != before.Anomalies+1 {
		t.Fatalf("the raw seal is counted visibly: %d -> %d", before.Anomalies, s.Anomalies)
	}
	// A raw second seal after a legal one stays an anomaly and the
	// first fact stands.
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-3", filedBody)
	ctx = step(k.signer, version.Seed1, "contract.specified", "c-3", specBody)
	ctx = step(k.sealer, version.Seed1, "check.sealed", "c-3", sealBody(testCommitment))
	first, _ := ctx.Lifecycle.State("c-3")
	ctx = step(k.plain, version.Seed1, "check.sealed", "c-3", sealBody(strings.Repeat("ef", 32)))
	s3, _ := ctx.Lifecycle.State("c-3")
	if s3.Sealed == nil || s3.Sealed.Commitment != testCommitment || s3.Sealed.Pos != first.Sealed.Pos {
		t.Fatalf("the first legal seal stands: %+v", s3.Sealed)
	}
	if s3.Anomalies != first.Anomalies+1 {
		t.Fatalf("the raw second seal is counted: %d -> %d", first.Anomalies, s3.Anomalies)
	}
}
