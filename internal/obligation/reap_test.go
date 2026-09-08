package obligation

// The revoked-holder reap obligation (plans/os-32d06c65.md D4): an
// in_progress claim whose holder has been revoked owes a reap
// specifically — KindReapOwed, distinct from KindClaimHeld — and the
// obligation is derived from the keyring's standing, the same source
// the reap itself consults.

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// reapChain stages genesis → seed/1 → enroll+grant a worker → file →
// specify → claim → (optionally) revoke, returning the records and the
// worker's key. Every event is signed; the root signs governance and
// the worker signs its claim.
func reapChain(t *testing.T, revoke bool) []*event.Record {
	t.Helper()
	root := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	ws := make([]byte, ed25519.SeedSize)
	ws[0] = 9
	worker := ed25519.NewKeyFromSeed(ws)
	fp := func(k ed25519.PrivateKey) string {
		f, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	g, err := genesis.Build(root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	records := []*event.Record{g}
	prev, err := g.Event.Hash()
	if err != nil {
		t.Fatal(err)
	}
	add := func(k ed25519.PrivateKey, v, verb, subject, payload string) {
		rec, err := event.Sign(event.Event{
			V: v, TS: "2026-09-01T02:00:00Z", Actor: fp(k), Verb: verb,
			Subject: subject, Payload: []byte(payload), Prev: prev,
		}, k)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, rec)
		if prev, err = rec.Event.Hash(); err != nil {
			t.Fatal(err)
		}
	}
	pub := hex.EncodeToString(worker.Public().(ed25519.PublicKey))
	add(root, version.Protocol, "system.protocol.upgraded", "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, "actor.enrolled", fp(worker), `{"key": "`+pub+`", "kind": "agent", "name": "worker"}`)
	add(root, version.Seed1, "actor.granted", fp(worker), `{"capability": "`+keyring.CapClaim+`"}`)
	add(root, version.Seed1, "intent.filed", "c-1", `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-1", `{"acceptance": {"ref": "accept.md @ 0000000000000000000000000000000000000000", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	if revoke {
		add(root, version.Seed1, "actor.revoked", fp(worker), `{"reason": "compromise"}`)
	}
	return records
}

func TestRevokedHolderOwesAReap(t *testing.T) {
	tbl := table(t)

	// Held but not revoked: the claim is owed (KindClaimHeld), no reap.
	held := Derive(reapChain(t, false), tbl, Deps{})
	if hasKind(held, KindReapOwed) {
		t.Fatal("an un-revoked holder owes no reap")
	}
	if !hasKind(held, KindClaimHeld) {
		t.Fatal("an active claim is owed as KindClaimHeld")
	}

	// Revoked: the reap is owed specifically, discharged by claim.reaped,
	// owed by the operator lane.
	revoked := Derive(reapChain(t, true), tbl, Deps{})
	var reap *Row
	for i := range revoked {
		if revoked[i].Kind == KindReapOwed {
			reap = &revoked[i]
		}
	}
	if reap == nil {
		t.Fatal("a revoked holder's open claim must owe a reap")
	}
	if reap.OwedBy != LaneOperator {
		t.Errorf("the reap is owed by the operator lane, got %q", reap.OwedBy)
	}
	if len(reap.DischargedBy) != 1 || reap.DischargedBy[0] != "claim.reaped" {
		t.Errorf("the reap is discharged by claim.reaped, got %v", reap.DischargedBy)
	}
}

func hasKind(rows []Row, kind string) bool {
	for _, r := range rows {
		if r.Kind == kind {
			return true
		}
	}
	return false
}
