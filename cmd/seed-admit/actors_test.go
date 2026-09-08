package main

// The enforced boundary under seed/1 (plans/os-52a2d688.md step 3;
// conformance III.E): enrollment chains admit through the hook, non-root
// actor verbs refuse at the standing rule, and actor verbs before the
// upgrade refuse as inactive — the same judgments the cooperative client
// makes, from the same rule set.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func altKey(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func fpFor(t testing.TB, priv ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func signedBy(t *testing.T, priv ed25519.PrivateKey, v, verb, subject, payload, prev string) *event.Record {
	t.Helper()
	rec, err := event.Sign(event.Event{
		V: v, TS: "2026-09-01T02:00:00Z", Actor: fpFor(t, priv),
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: prev,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func anyResolver(t testing.TB, privs ...ed25519.PrivateKey) ledger.Resolver {
	t.Helper()
	ring := map[string]ed25519.PublicKey{}
	for _, p := range privs {
		ring[fpFor(t, p)] = p.Public().(ed25519.PublicKey)
	}
	return func(fp string) (ed25519.PublicKey, bool) {
		pub, ok := ring[fp]
		return pub, ok
	}
}

func enrollFor(t testing.TB, priv ed25519.PrivateKey, kind, name string) string {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf(`{"key": %q, "kind": %q, "name": %q}`, hex.EncodeToString(pub), kind, name)
}

func TestHookAdmitsEnrollmentChain(t *testing.T) {
	remote := guardedRemote(t)
	seedGenesis(t, remote)
	root, worker := fixtureKey(t), altKey(t, 9)
	loose := anyResolver(t, root, worker)

	err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`, tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, worker), enrollFor(t, worker, "agent", "worker"), tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, worker, version.Seed1, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store)))
	})
	if err != nil {
		t.Fatalf("the hook must admit an upgrade + enrollment + enrolled-key event: %v", err)
	}

	// The landed chain verifies from genesis through the client seam.
	c, err := gitref.NewClient(t.TempDir(), remote, guardedRef)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(anyResolver(t, root))
	if err != nil || rep.Count != 4 || rep.ActiveVersion != version.Seed1 {
		t.Fatalf("the admitted chain must verify: %+v %v", rep, err)
	}
}

func TestHookRefusesNonRootActorVerb(t *testing.T) {
	remote := guardedRemote(t)
	seedGenesis(t, remote)
	root, worker, third := fixtureKey(t), altKey(t, 9), altKey(t, 10)
	loose := anyResolver(t, root, worker, third)

	if err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`, tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, worker), enrollFor(t, worker, "agent", "worker"), tipOf(t, store)))
	}); err != nil {
		t.Fatalf("setup push must land: %v", err)
	}

	before := remoteTip(t, remote)
	err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, worker, version.Seed1, "actor.enrolled", fpFor(t, third), enrollFor(t, third, "agent", "third"), tipOf(t, store)))
	})
	if err == nil || remoteTip(t, remote) != before {
		t.Fatalf("a non-root enrollment must refuse with the ref unmoved, got %v", err)
	}
	if !strings.Contains(err.Error(), "grant") || !strings.Contains(err.Error(), "not granted") {
		t.Fatalf("the refusal must come from the grant rule as out of grant, got %v", err)
	}
}

func TestHookRefusesActorVerbBeforeUpgrade(t *testing.T) {
	remote := guardedRemote(t)
	seedGenesis(t, remote)
	root, worker := fixtureKey(t), altKey(t, 9)
	loose := anyResolver(t, root, worker)

	before := remoteTip(t, remote)
	err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, "seed/0", "actor.enrolled", fpFor(t, worker), enrollFor(t, worker, "agent", "worker"), tipOf(t, store)))
	})
	if err == nil || remoteTip(t, remote) != before {
		t.Fatalf("an actor verb at a seed/0 position must refuse, got %v", err)
	}
	if !strings.Contains(err.Error(), "not active at protocol") {
		t.Fatalf("the refusal must name the inactive semantics, got %v", err)
	}
}

// conformance: III.E — the grant checks run at the enforced boundary
// too: an operator verb from a non-operator refuses with the ref
// unmoved, and a maintenance grant checkpoints without holding operator
// authority (plans/os-3979d48b.md).
func TestHookChecksGrants(t *testing.T) {
	remote := guardedRemote(t)
	seedGenesis(t, remote)
	root, worker := fixtureKey(t), altKey(t, 9)
	loose := anyResolver(t, root, worker)
	if err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`, tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, worker), enrollFor(t, worker, "agent", "worker"), tipOf(t, store)))
	}); err != nil {
		t.Fatalf("setup push must land: %v", err)
	}
	before := remoteTip(t, remote)
	err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, worker, version.Seed1, "system.halt.declared", "system", `{"reason": "x"}`, tipOf(t, store)))
	})
	if err == nil || remoteTip(t, remote) != before || !strings.Contains(err.Error(), "not granted") {
		t.Fatalf("a non-operator halt must refuse at the boundary, got %v", err)
	}
	if err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.granted", fpFor(t, worker), `{"capability": "maintenance"}`, tipOf(t, store)))
		// A real snapshot citation: the checkpoint rule refuses an
		// arbitrary payload (plans/os-8a5f14bb.md D4.5), and this
		// drill is about the GRANT reaching the pre-receive hook.
		cp, err := checkpoint.Payload(strings.Repeat("b", 64), 0)
		if err != nil {
			t.Fatal(err)
		}
		appendRaw(t, store, loose, signedBy(t, worker, version.Seed1, checkpoint.Verb, "system", string(cp), tipOf(t, store)))
	}); err != nil {
		t.Fatalf("a maintenance checkpoint must admit at the boundary: %v", err)
	}
}
