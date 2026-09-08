package admit

// The offer admission drills (plans/os-c61c3392.md;
// spec/offers.md): offer.published admits only in ready from the
// supervise/operator lanes, strictly shaped, with a deterministic
// born-dead refusal against the event's own ts; a re-readied subject
// is offerable again by a fresh publication.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Draft events carry ts 2026-09-01T01:00:00Z: this expiry lies after
// it, so the offer is well-born.
const liveOfferBody = `{"eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}, "expires": "2026-09-02T00:00:00Z"}`

type offerKeys struct {
	signer, supervisor, worker, plain ed25519.PrivateKey
}

// offerFixture enrolls a supervisor (supervise grant only), a worker
// (claim), and a grantless key, then files and specifies c-1 (ready).
func offerFixture(t *testing.T) (*Context, offerKeys, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := offerKeys{signer: signer, supervisor: fixtureKey(t, 11), worker: fixtureKey(t, 2), plain: fixtureKey(t, 6)}
	all := []ed25519.PrivateKey{k.signer, k.supervisor, k.worker, k.plain}
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
	}{{k.supervisor, "supervisor"}, {k.worker, "worker"}, {k.plain, "plain"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
	}
	for _, g := range []struct {
		key ed25519.PrivateKey
		cap string
	}{{k.supervisor, keyring.CapSupervise}, {k.worker, keyring.CapClaim}} {
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

// conformance: III.H — offers are supervisor acts inviting claims;
// they admit only on ready subjects and grant nothing.
func TestOfferAdmissionMatrix(t *testing.T) {
	ctx, k, step := offerFixture(t)

	// The lanes: supervise or operator. Claim and plain standing
	// refuse before any state logic runs; the supervisor and the
	// root's implicit operator both admit.
	for name, priv := range map[string]ed25519.PrivateKey{"claim": k.worker, "plain": k.plain} {
		err := Check(ctx, draftV(t, priv, version.Seed1, "offer.published", "c-1", liveOfferBody, ctx.Tip))
		if err == nil || !strings.Contains(err.Error(), "not granted any of [supervise, operator]") {
			t.Fatalf("%s must be out of the supervisor lane: %v", name, err)
		}
	}
	for name, priv := range map[string]ed25519.PrivateKey{"supervisor": k.supervisor, "operator": k.signer} {
		if err := Check(ctx, draftV(t, priv, version.Seed1, "offer.published", "c-1", liveOfferBody, ctx.Tip)); err != nil {
			t.Fatalf("the %s lane must admit in ready: %v", name, err)
		}
	}

	// Shape refusals on the ready subject: strict object, required
	// fields, RFC3339 expiry, and the deterministic born-dead check
	// against the event's own ts (equal and earlier both refuse).
	for name, body := range map[string]string{
		"extra key":           `{"eligibility": {}, "expires": "2026-09-02T00:00:00Z", "note": "x"}`,
		"missing eligibility": `{"expires": "2026-09-02T00:00:00Z"}`,
		"missing expires":     `{"eligibility": {}}`,
		"malformed expires":   `{"eligibility": {}, "expires": "tomorrow"}`,
		"expires at ts":       `{"eligibility": {}, "expires": "2026-09-01T01:00:00Z"}`,
		"expires before ts":   `{"eligibility": {}, "expires": "2026-08-31T00:00:00Z"}`,
	} {
		if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", body, ctx.Tip)); err == nil {
			t.Fatalf("%s must refuse", name)
		}
	}

	// Empty scopes are an open offer, not a shape violation.
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", `{"eligibility": {}, "expires": "2026-09-02T00:00:00Z"}`, ctx.Tip)); err != nil {
		t.Fatalf("unscoped eligibility must admit: %v", err)
	}

	// The window: unknown subjects and every non-ready state refuse.
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-none", liveOfferBody, ctx.Tip)); err == nil {
		t.Fatal("an offer on an unknown subject must refuse")
	}
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-2", filedBody)
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-2", liveOfferBody, ctx.Tip)); err == nil {
		t.Fatal("an offer in backlog must refuse — the window opens at ready")
	}
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", liveOfferBody, ctx.Tip)); err == nil {
		t.Fatal("an offer on an in_progress subject must refuse")
	}

	// A re-readied subject is offerable again by a fresh publication.
	s, _ := ctx.Lifecycle.State("c-1")
	ctx = step(k.worker, version.Seed1, "claim.released", "c-1", fmt.Sprintf(`{"fence": "%d", "packet": %s}`, s.Claim.Fence, minPacket))
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", liveOfferBody, ctx.Tip)); err != nil {
		t.Fatalf("a fresh offer on the re-readied subject must admit: %v", err)
	}

	// Review refuses too: offers invite claims, and review subjects
	// are past claiming.
	ctx = step(k.worker, version.Seed1, "claim.taken", "c-1", `{}`)
	s, _ = ctx.Lifecycle.State("c-1")
	ctx = step(k.worker, version.Seed1, "submission.made", "c-1", submissionBody(fmt.Sprintf("%d", s.Claim.Fence)))
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", liveOfferBody, ctx.Tip)); err == nil {
		t.Fatal("an offer on a review subject must refuse")
	}
}

// conformance: review finding on the task PR — the tuples scope's
// version gate reads the field's PRESENCE, not its length. A seed/1
// validator strictly decodes eligibility as {capabilities, tiers} and
// refuses the field however it is valued, so this validator must refuse
// an explicit "tuples": [] or null on a seed/1 record exactly as it
// refuses a populated list; at seed/2 an empty or null list is an
// unscoped offer, and a list that is not a list of tuples refuses by
// shape.
func TestOfferTuplesFieldPresenceIsTheVersionGate(t *testing.T) {
	ctx, k, step := offerFixture(t)
	body := func(tuples string) string {
		return `{"eligibility": {"capabilities": ["claim"], "tuples": ` + tuples + `}, "expires": "2027-01-01T00:00:00Z"}`
	}
	for name, tuples := range map[string]string{"empty list": "[]", "null": "null", "populated": `[{"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e"}]`} {
		err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", body(tuples), ctx.Tip))
		if err == nil || !strings.Contains(err.Error(), "tuple semantics activate at "+version.Seed2) {
			t.Fatalf("%s: an explicit tuples field before seed/2 refuses by version: %v", name, err)
		}
	}
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "offer.published", "c-1", liveOfferBody, ctx.Tip)); err != nil {
		t.Fatalf("an offer without the field admits at seed/1 as before: %v", err)
	}
	ctx = step(k.signer, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	for name, tuples := range map[string]string{"empty list": "[]", "null": "null"} {
		if err := Check(ctx, draftV(t, k.supervisor, version.Seed2, "offer.published", "c-1", body(tuples), ctx.Tip)); err != nil {
			t.Fatalf("%s: at seed/2 an empty scope is an unscoped offer: %v", name, err)
		}
	}
	for name, tuples := range map[string]string{"not a list": `"acme"`, "object": `{"principal": "acme"}`, "malformed member": `[{"principal": "acme"}]`} {
		err := Check(ctx, draftV(t, k.supervisor, version.Seed2, "offer.published", "c-1", body(tuples), ctx.Tip))
		var oe *OfferError
		if !errors.As(err, &oe) {
			t.Fatalf("%s: a scope that is not a list of tuples refuses by shape: %v", name, err)
		}
	}
}
