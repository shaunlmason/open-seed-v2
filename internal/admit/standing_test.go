package admit

// The standing rule (plans/os-52a2d688.md step 3; conformance III.E):
// actor verbs refuse before the seed/1 boundary, require an active
// governance root while grant checks are pending (3.2), and preview the
// shared keyring transition so an illegal draft never leaves the client.

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func fpOf(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func enrollBody(t *testing.T, priv ed25519.PrivateKey, kind, name string) string {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf(`{"key": %q, "kind": %q, "name": %q}`, hex.EncodeToString(pub), kind, name)
}

func appendSignedV(t *testing.T, store *ledger.Store, resolve ledger.Resolver, priv ed25519.PrivateKey, v, verb, subject, payload string) {
	t.Helper()
	tip, _, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(draftV(t, priv, v, verb, subject, payload, tip), resolve); err != nil {
		t.Fatal(err)
	}
}

func TestStandingRefusesActorVerbsBeforeSeed1(t *testing.T) {
	store, _, signer := seededStore(t)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	worker := fixtureKey(t, 2)
	err = Check(ctx, draft(t, signer, keyring.VerbEnrolled, fpOf(t, worker), enrollBody(t, worker, "agent", "worker"), ctx.Tip))
	var ref *Refusal
	var vin *VerbInactiveError
	if !errors.As(err, &ref) || ref.Rule != "standing" || !errors.As(err, &vin) {
		t.Fatalf("an actor verb at a seed/0 tip must refuse by the standing rule as inactive, got %v", err)
	}
	if vin.Needs != version.Seed1 {
		t.Fatalf("the refusal must name the activating version, got %+v", vin)
	}

	// The activation refusal precedes signer resolution (review finding
	// on #100): an unknown signer's inactive actor verb gets the same
	// refusal the hook gives, never an unresolvable-signer complaint.
	stranger := fixtureKey(t, 9)
	err = Check(ctx, draft(t, stranger, keyring.VerbEnrolled, fpOf(t, stranger), enrollBody(t, stranger, "agent", "s"), ctx.Tip))
	if !errors.As(err, &vin) {
		t.Fatalf("an unknown signer must still refuse as inactive at a seed/0 tip, got %v", err)
	}
}

func TestStandingRequiresActiveRootAndPreviews(t *testing.T) {
	store, resolve, signer := seededStore(t)
	appendSigned(t, store, resolve, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Active != version.Seed1 || ctx.Keyring == nil {
		t.Fatalf("the context must carry the keyring at seed/1, got %+v", ctx)
	}

	worker := fixtureKey(t, 2)
	good := draftV(t, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, worker), enrollBody(t, worker, "agent", "worker"), ctx.Tip)
	if err := Check(ctx, good); err != nil {
		t.Fatalf("a root-signed enrollment must admit: %v", err)
	}

	// The preview half: a shape-broken enrollment and a root-liveness
	// violation refuse through the shared transition function.
	bad := draftV(t, signer, version.Seed1, keyring.VerbEnrolled, "c-0001", `{"garbage": true}`, ctx.Tip)
	var ref *Refusal
	if err := Check(ctx, bad); !errors.As(err, &ref) || ref.Rule != "grant" {
		t.Fatalf("a malformed enrollment must refuse by the grant rule, got %v", err)
	}
	selfRevoke := draftV(t, signer, version.Seed1, keyring.VerbRevoked, fpOf(t, signer), `{"reason": "x"}`, ctx.Tip)
	if err := Check(ctx, selfRevoke); err == nil {
		t.Fatal("revoking the last active root must refuse at admission")
	}
}

func TestStandingRefusesNonRootActorVerbs(t *testing.T) {
	store, resolve, signer := seededStore(t)
	worker := fixtureKey(t, 2)
	third := fixtureKey(t, 3)
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range []ed25519.PrivateKey{signer, worker} {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, worker), enrollBody(t, worker, "agent", "worker"))
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}

	// The enrolled worker signs ordinary verbs (the keyring is the
	// resolver now) but not actor lifecycle verbs.
	if err := Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)); err != nil {
		t.Fatalf("an enrolled key must admit ordinary verbs: %v", err)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, keyring.VerbEnrolled, fpOf(t, third), enrollBody(t, third, "agent", "third"), ctx.Tip))
	var oog *OutOfGrantError
	if !errors.As(err, &oog) || oog.Actor != fpOf(t, worker) {
		t.Fatalf("a non-root actor verb must refuse out of grant, got %v", err)
	}

	// After revocation the worker resolves nowhere: the actor rule
	// refuses its ordinary verbs too.
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbRevoked, fpOf(t, worker), `{"reason": "compromise"}`)
	ctx, err = ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	err = Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-0002", `{"n": 2}`, ctx.Tip))
	if !errors.Is(err, ledger.ErrUnknownActor) {
		t.Fatalf("a revoked key must not admit anything, got %v", err)
	}
}
