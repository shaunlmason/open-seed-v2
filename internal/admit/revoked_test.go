package admit

// RevokedHolder is the reap arm's boundary check (plans/os-32d06c65.md
// D1): a claim whose holder was revoked is reapable on the revocation
// alone. It holds only for a boundary-valid revocation of the fence's
// own holder — a suspension, a non-holder's revocation, a raw-pushed
// one, or the wrong fence corroborate nothing.

import (
	"crypto/ed25519"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// revokeFixture drives a contract to in_progress under a worker and
// returns the context, the cast, the store/resolver for further
// staging, and the worker's claim fence.
func revokeFixture(t *testing.T) (*Context, *ledger.Store, ledger.Resolver, ed25519.PrivateKey, ed25519.PrivateKey, ed25519.PrivateKey, int) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	worker := fixtureKey(t, 2)
	peer := fixtureKey(t, 3)
	all := []ed25519.PrivateKey{signer, worker, peer}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range all {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSignedV(t, store, loose, signer, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, w := range []struct {
		key  ed25519.PrivateKey
		name string
	}{{worker, "worker"}, {peer, "peer"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, w.key), enrollBody(t, w.key, "agent", w.name))
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, w.key), `{"capability": "`+keyring.CapClaim+`"}`)
	}
	appendSignedV(t, store, loose, signer, version.Seed1, "intent.filed", "c-1", `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	appendSignedV(t, store, loose, signer, version.Seed1, "contract.specified", "c-1", `{"acceptance": {"ref": "accept.md @ 0000000000000000000000000000000000000000", "executable": false}}`)
	appendSignedV(t, store, loose, worker, version.Seed1, "claim.taken", "c-1", `{}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := ctx.Lifecycle.State("c-1")
	if !ok || s.Claim == nil {
		t.Fatalf("c-1 must be claimed: %+v", s)
	}
	return ctx, store, loose, signer, worker, peer, s.Claim.Fence
}

func TestRevokedHolderReapsOnTheRevocationAlone(t *testing.T) {
	ctx, store, loose, signer, worker, _, fence := revokeFixture(t)

	// Before any revocation: not reapable on this path.
	if RevokedHolder(ctx.Records, ctx.Table, "c-1", fence) {
		t.Fatal("an un-revoked holder is not reapable on the revocation path")
	}

	// The operator revokes the worker: now reapable, with no interrupt
	// and no wedge anywhere.
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbRevoked, fpOf(t, worker), `{"reason": "compromise"}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if !RevokedHolder(ctx.Records, ctx.Table, "c-1", fence) {
		t.Fatal("a boundary-valid revocation of the fence's holder must corroborate a reap")
	}
	// The wrong fence corroborates nothing.
	if RevokedHolder(ctx.Records, ctx.Table, "c-1", fence+1) {
		t.Fatal("a revocation corroborates only the active fence")
	}
	// A subject with no claim is not reapable.
	if RevokedHolder(ctx.Records, ctx.Table, "c-none", fence) {
		t.Fatal("a subject with no active claim is not reapable")
	}
}

func TestRevokedHolderRejectsSuspensionAndNonHolderAndRawPush(t *testing.T) {
	// Suspension is not revocation: a suspended holder's standing can
	// return, so its claim is not reaped.
	ctx, store, loose, signer, worker, peer, fence := revokeFixture(t)
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbSuspended, fpOf(t, worker), `{"reason": "paused"}`)
	ctx, _ = ContextAt(store)
	if RevokedHolder(ctx.Records, ctx.Table, "c-1", fence) {
		t.Fatal("a SUSPENDED holder is not reapable — its standing can return")
	}

	// A non-holder's revocation corroborates nothing: revoke the peer,
	// who does not hold c-1.
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbRevoked, fpOf(t, peer), `{"reason": "unrelated"}`)
	ctx, _ = ContextAt(store)
	if RevokedHolder(ctx.Records, ctx.Table, "c-1", fence) {
		t.Fatal("a revocation of someone other than the holder corroborates nothing")
	}
}

func TestRevokedHolderRejectsUnprivilegedRevocation(t *testing.T) {
	// A revocation signed by a key with no operator standing is refused
	// at the boundary; RevokedHolder, judging at the record's own
	// position like InterruptValid, gives it no weight even though the
	// fold might tolerate it in raw history.
	ctx, store, loose, _, worker, peer, fence := revokeFixture(t)
	// The peer (claim only, no operator) tries to revoke the worker.
	raw := draftV(t, peer, version.Seed1, keyring.VerbRevoked, fpOf(t, worker), `{"reason": "forged"}`, ctx.Tip)
	if _, err := store.Append(raw, loose); err != nil {
		t.Fatalf("staging the raw revocation for the drill: %v", err)
	}
	ctx, _ = ContextAt(store)
	if RevokedHolder(ctx.Records, ctx.Table, "c-1", fence) {
		t.Fatal("a revocation whose signer held no operator standing corroborates nothing")
	}
}
