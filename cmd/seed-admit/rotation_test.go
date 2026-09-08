package main

// The Phase 3.3 drills (plans/os-d1f35a8c.md; conformance III.E,
// Phase 3 subset): key rotation with history attribution preserved,
// terminality and grants-dying-with-standing at the enforced boundary,
// and the compromised-key cut per posture.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func signerFor(priv ed25519.PrivateKey) gitref.Signer {
	return func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }
}

func mustClient(t *testing.T, remote string) *gitref.Client {
	t.Helper()
	c, err := gitref.NewClient(t.TempDir(), remote, guardedRef)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// conformance: III.E — the rotation drill: enroll A, let it work,
// rotate to B, revoke A. A's history stays attributed and verifies;
// every post-revocation A proposal refuses at the rule set and the
// boundary; B works; terminality and root liveness hold through the
// churn.
func TestDrillKeyRotation(t *testing.T) {
	remote := guardedRemote(t)
	resolve := seedGenesis(t, remote)
	root, keyA, keyB := fixtureKey(t), altKey(t, 21), altKey(t, 22)
	loose := anyResolver(t, root, keyA, keyB)

	client := func() *gitref.Client { return mustClient(t, remote) }

	// Enrollment era: upgrade, enroll A with a maintenance grant, and
	// let A work through the legitimate client.
	if err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`, tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, keyA), enrollFor(t, keyA, "agent", "worker-a"), tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.granted", fpFor(t, keyA), `{"capability": "maintenance"}`, tipOf(t, store)))
	}); err != nil {
		t.Fatalf("enrollment era must land: %v", err)
	}
	if _, err := client().AppendLoop(gitref.Draft{
		V: version.Seed1, TS: "2026-09-01T04:00:00Z", Actor: fpFor(t, keyA),
		Verb: "message.sent", Subject: "c-1001", Payload: json.RawMessage(`{"n": 1}`),
	}, signerFor(keyA), resolve, admit.Validate(), 3); err != nil {
		t.Fatalf("A must work before the rotation: %v", err)
	}

	// The rotation: enroll B, revoke A.
	if err := craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, keyB), enrollFor(t, keyB, "agent", "worker-b"), tipOf(t, store)))
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.revoked", fpFor(t, keyA), `{"reason": "rotation"}`, tipOf(t, store)))
	}); err != nil {
		t.Fatalf("the rotation must land: %v", err)
	}

	// History stays attributed: the chain verifies from genesis and A's
	// pre-revocation work still carries A's fingerprint.
	c := client()
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
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil || rep.Count != 7 {
		t.Fatalf("the rotated chain must verify from genesis: %+v %v", rep, err)
	}
	attributed := false
	if err := store.Records(func(pos int, rec *event.Record) error {
		if rec.Event.Verb == "message.sent" && rec.Event.Subject == "c-1001" {
			attributed = rec.Event.Actor == fpFor(t, keyA)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !attributed {
		t.Fatal("A's pre-revocation work must stay attributed to A")
	}

	// Post-revocation, A refuses at the rule set (the client refuses to
	// even push) and at the raw boundary; B works. The grant A held
	// confers nothing: standing is read first.
	_, err = client().AppendLoop(gitref.Draft{
		V: version.Seed1, TS: "2026-09-01T04:10:00Z", Actor: fpFor(t, keyA),
		Verb: "message.sent", Subject: "c-1002", Payload: json.RawMessage(`{"n": 2}`),
	}, signerFor(keyA), resolve, admit.Validate(), 3)
	if !errors.Is(err, ledger.ErrUnknownActor) {
		t.Fatalf("the rule set must refuse revoked A, got %v", err)
	}
	_, err = client().AppendLoop(gitref.Draft{
		V: version.Seed1, TS: "2026-09-01T04:11:00Z", Actor: fpFor(t, keyA),
		Verb: "system.checkpoint", Subject: "system", Payload: json.RawMessage(`{"n": 1}`),
	}, signerFor(keyA), resolve, admit.Validate(), 3)
	if !errors.Is(err, ledger.ErrUnknownActor) {
		t.Fatalf("A's pre-revocation maintenance grant must confer nothing, got %v", err)
	}
	before := remoteTip(t, remote)
	err = craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, keyA, version.Seed1, "message.sent", "c-1003", `{"n": 3}`, tipOf(t, store)))
	})
	if !errors.Is(err, gitref.ErrRemoteRejected) || remoteTip(t, remote) != before || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("the boundary must refuse revoked A naming the standing, got %v", err)
	}
	if _, err := client().AppendLoop(gitref.Draft{
		V: version.Seed1, TS: "2026-09-01T04:12:00Z", Actor: fpFor(t, keyB),
		Verb: "message.sent", Subject: "c-1004", Payload: json.RawMessage(`{"n": 4}`),
	}, signerFor(keyB), resolve, admit.Validate(), 3); err != nil {
		t.Fatalf("B must work after the rotation: %v", err)
	}

	// Terminality and root liveness hold through the churn, at the
	// boundary rather than only in the library.
	before = remoteTip(t, remote)
	err = craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, keyA), enrollFor(t, keyA, "agent", "worker-a"), tipOf(t, store)))
	})
	if !errors.Is(err, gitref.ErrRemoteRejected) || remoteTip(t, remote) != before || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("re-enrolling revoked A must refuse as terminal at the boundary, got %v", err)
	}
	err = craftPush(t, remote, loose, func(dir string, store *ledger.Store) {
		appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.revoked", fpFor(t, root), `{"reason": "self"}`, tipOf(t, store)))
	})
	if !errors.Is(err, gitref.ErrRemoteRejected) || !strings.Contains(err.Error(), "root liveness") {
		t.Fatalf("root liveness must hold through the rotation, got %v", err)
	}
}

// conformance: III.E + the compromised-actor consequence — the
// compromised-key cut, per posture: a hostile holder of a revoked key
// pushes raw. Enforced refuses with the ref unmoved; under cooperative
// the push lands and breaks the shared chain at exactly the revoked
// signer's position, which every reader's pre-flight replay reports
// with the standing named.
func TestDrillCompromisedKeyCutPerPosture(t *testing.T) {
	for _, p := range []posture.Posture{posture.EnforcedSelfHosted, posture.Cooperative} {
		t.Run(string(p), func(t *testing.T) {
			d := newDeployment(t, p)
			resolve := seedGenesis(t, d.remote)
			root, keyA := fixtureKey(t), altKey(t, 23)
			loose := anyResolver(t, root, keyA)
			if err := craftPush(t, d.remote, loose, func(dir string, store *ledger.Store) {
				appendRaw(t, store, loose, signedBy(t, root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`, tipOf(t, store)))
				appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.enrolled", fpFor(t, keyA), enrollFor(t, keyA, "agent", "worker-a"), tipOf(t, store)))
				appendRaw(t, store, loose, signedBy(t, root, version.Seed1, "actor.revoked", fpFor(t, keyA), `{"reason": "compromise"}`, tipOf(t, store)))
			}); err != nil {
				t.Fatalf("the revocation era must land: %v", err)
			}

			before := remoteTip(t, d.remote)
			err := craftPush(t, d.remote, loose, func(dir string, store *ledger.Store) {
				appendRaw(t, store, loose, signedBy(t, keyA, version.Seed1, "message.sent", "c-2001", `{"n": 1}`, tipOf(t, store)))
			})
			after := remoteTip(t, d.remote)
			if d.posture.Enforced() {
				if !errors.Is(err, gitref.ErrRemoteRejected) || after != before || !strings.Contains(err.Error(), "revoked") {
					t.Fatalf("enforced must cut the compromised key with the ref unmoved, got %v", err)
				}
				return
			}
			if err != nil || after == before {
				t.Fatalf("under cooperative the raw push lands — %q — got %v", posture.Consequence, err)
			}
			// The landed record breaks the shared chain for every
			// reader at the revoked signer's position.
			_, err = mustClient(t, d.remote).AppendLoop(gitref.Draft{
				V: version.Seed1, TS: "2026-09-01T05:00:00Z", Actor: fpFor(t, root),
				Verb: "message.sent", Subject: "c-2002", Payload: json.RawMessage(`{"n": 2}`),
			}, signerFor(root), resolve, admit.Validate(), 3)
			if err == nil || !strings.Contains(err.Error(), "failed verification") || !strings.Contains(err.Error(), "revoked") {
				t.Fatalf("the client's replay must report the broken chain with the standing named, got %v", err)
			}
		})
	}
}
