package ledger_test

// Position-aware verification behind the seed/1 boundary
// (plans/os-52a2d688.md step 2; conformance III.E): a key signs only
// between its enrollment and its suspension or revocation, history
// signed before a standing change keeps verifying, actor events at
// seed/0 positions are grandfathered, and an illegal actor event at a
// seed/1 position fails at that position. External test package: the
// fixtures need internal/genesis, which imports this package.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func aKey(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func aFP(t testing.TB, priv ed25519.PrivateKey) string {
	t.Helper()
	f, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// anyKey resolves every fixture key: chain construction only, so tests
// can build the exact streams verification must then judge.
func anyKey(t testing.TB, privs ...ed25519.PrivateKey) ledger.Resolver {
	t.Helper()
	ring := map[string]ed25519.PublicKey{}
	for _, p := range privs {
		ring[aFP(t, p)] = p.Public().(ed25519.PublicKey)
	}
	return func(fp string) (ed25519.PublicKey, bool) {
		pub, ok := ring[fp]
		return pub, ok
	}
}

type chainBuilder struct {
	t     *testing.T
	store *ledger.Store
	loose ledger.Resolver
	tip   string
	v     string
}

func newChain(t *testing.T, root ed25519.PrivateKey, privs ...ed25519.PrivateKey) (*chainBuilder, ledger.Resolver) {
	t.Helper()
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := genesis.Init(store, root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(g)
	if err != nil {
		t.Fatal(err)
	}
	rootResolve, err := payload.Resolver(g.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	h, err := g.Event.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return &chainBuilder{
		t: t, store: store, loose: anyKey(t, append(privs, root)...),
		tip: h, v: version.Protocol,
	}, rootResolve
}

func (b *chainBuilder) add(priv ed25519.PrivateKey, verb, subject, payload string) {
	b.t.Helper()
	rec, err := event.Sign(event.Event{
		V: b.v, TS: "2026-09-01T01:00:00Z", Actor: aFP(b.t, priv),
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: b.tip,
	}, priv)
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.store.Append(rec, b.loose); err != nil {
		b.t.Fatal(err)
	}
	h, err := rec.Event.Hash()
	if err != nil {
		b.t.Fatal(err)
	}
	b.tip = h
	if verb == ledger.UpgradeVerb && subject == "system" {
		var up struct {
			To string `json:"to"`
		}
		if err := json.Unmarshal(json.RawMessage(payload), &up); err == nil && up.To != "" {
			b.v = up.To
		}
	}
}

func enrollJSON(t testing.TB, priv ed25519.PrivateKey, kind, name string) string {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf(`{"key": %q, "kind": %q, "name": %q}`, hex.EncodeToString(pub), kind, name)
}

// conformance: III.E — an enrolled key's events verify from genesis on a
// seed/1 chain; the standing changes bound exactly where it signs.
func TestVerifyEnrolledKeyLifecycle(t *testing.T) {
	root, worker := aKey(t, 1), aKey(t, 2)
	b, rootResolve := newChain(t, root, worker)
	b.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b.add(root, "actor.enrolled", aFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	b.add(worker, "message.sent", "c-0001", `{"n": 1}`)

	rep, err := b.store.VerifyFromGenesis(rootResolve)
	if err != nil || rep.Count != 4 || rep.ActiveVersion != version.Seed1 {
		t.Fatalf("enrolled key's chain must verify: %+v %v", rep, err)
	}

	// Revocation ends standing at its position; everything before stays
	// attributed and the suffix refuses.
	b.add(root, "actor.revoked", aFP(t, worker), `{"reason": "compromise"}`)
	if rep, err := b.store.VerifyFromGenesis(rootResolve); err != nil || rep.Count != 5 {
		t.Fatalf("history before the revocation stays attributed: %+v %v", rep, err)
	}
	b.add(worker, "message.sent", "c-0002", `{"n": 2}`)
	var fail *ledger.Failure
	_, err = b.store.VerifyFromGenesis(rootResolve)
	if !errors.As(err, &fail) || fail.Position != 5 || fail.Reason != ledger.ReasonUnknownActor {
		t.Fatalf("a revoked key's later event must refuse at its position, got %v", err)
	}
	if !strings.Contains(fail.Detail, "revoked") {
		t.Fatalf("the refusal must name the standing, got %q", fail.Detail)
	}
}

func TestVerifyPreEnrollmentSignatureRefuses(t *testing.T) {
	root, worker := aKey(t, 1), aKey(t, 2)
	b, rootResolve := newChain(t, root, worker)
	b.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b.add(worker, "message.sent", "c-0001", `{"n": 1}`)
	var fail *ledger.Failure
	_, err := b.store.VerifyFromGenesis(rootResolve)
	if !errors.As(err, &fail) || fail.Position != 2 || fail.Reason != ledger.ReasonUnknownActor {
		t.Fatalf("a pre-enrollment signature must refuse at its position, got %v", err)
	}
}

func TestVerifySuspensionPausesAndReinstates(t *testing.T) {
	root, worker := aKey(t, 1), aKey(t, 2)
	b, rootResolve := newChain(t, root, worker)
	b.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b.add(root, "actor.enrolled", aFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	b.add(root, "actor.suspended", aFP(t, worker), `{"reason": "drift"}`)
	b.add(worker, "message.sent", "c-0001", `{"n": 1}`)
	var fail *ledger.Failure
	if _, err := b.store.VerifyFromGenesis(rootResolve); !errors.As(err, &fail) || fail.Position != 4 {
		t.Fatalf("a suspended key must not sign, got %v", err)
	}

	// Rebuild with reinstatement between suspension and the milestone.
	b2, rootResolve2 := newChain(t, root, worker)
	b2.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b2.add(root, "actor.enrolled", aFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	b2.add(root, "actor.suspended", aFP(t, worker), `{"reason": "drift"}`)
	b2.add(root, "actor.enrolled", aFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	b2.add(worker, "message.sent", "c-0001", `{"n": 1}`)
	if rep, err := b2.store.VerifyFromGenesis(rootResolve2); err != nil || rep.Count != 6 {
		t.Fatalf("reinstatement must restore standing: %+v %v", rep, err)
	}
}

// conformance: the seed/1 activation boundary (review finding on #97) —
// a chain that verified before Phase 3 still verifies: actor events at
// seed/0 positions are inert, and enforcement begins after the upgrade.
func TestVerifyGrandfathersSeed0ActorEvents(t *testing.T) {
	root, worker := aKey(t, 1), aKey(t, 2)
	b, rootResolve := newChain(t, root, worker)
	b.add(root, "actor.enrolled", "c-0001", `{"garbage": true}`)
	b.add(root, "message.sent", "c-0002", `{"n": 1}`)
	if rep, err := b.store.VerifyFromGenesis(rootResolve); err != nil || rep.Count != 3 {
		t.Fatalf("seed/0 actor events are grandfathered: %+v %v", rep, err)
	}

	// The junk event has no keyring effect either: after upgrading, the
	// worker still needs a real enrollment before signing.
	b.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b.add(worker, "message.sent", "c-0003", `{"n": 2}`)
	var fail *ledger.Failure
	if _, err := b.store.VerifyFromGenesis(rootResolve); !errors.As(err, &fail) || fail.Position != 4 {
		t.Fatalf("grandfathered junk grants nothing, got %v", err)
	}
}

func TestVerifyMalformedActorEventFailsAtPosition(t *testing.T) {
	root := aKey(t, 1)
	b, rootResolve := newChain(t, root)
	b.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b.add(root, "actor.enrolled", "c-0001", `{"garbage": true}`)
	var fail *ledger.Failure
	_, err := b.store.VerifyFromGenesis(rootResolve)
	if !errors.As(err, &fail) || fail.Position != 2 || fail.Reason != ledger.ReasonActorEvent {
		t.Fatalf("a malformed actor event at seed/1 must fail there as %s, got %v", ledger.ReasonActorEvent, err)
	}
}

// Root liveness holds in replay too: a raw-crafted chain that ends its
// last active root's standing is invalid, whichever posture admitted it.
func TestVerifyRootLivenessInHistory(t *testing.T) {
	root := aKey(t, 1)
	b, rootResolve := newChain(t, root)
	b.add(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	b.add(root, "actor.revoked", aFP(t, root), `{"reason": "self"}`)
	var fail *ledger.Failure
	_, err := b.store.VerifyFromGenesis(rootResolve)
	if !errors.As(err, &fail) || fail.Position != 2 || fail.Reason != ledger.ReasonActorEvent {
		t.Fatalf("a chain ending its last root is invalid, got %v", err)
	}
}

// The verb literals keyring mirrors (it cannot import ledger or genesis)
// stay pinned to the real constants.
func TestKeyringVerbParity(t *testing.T) {
	if ledger.UpgradeVerb != "system.protocol.upgraded" || genesis.Verb != "system.genesis" {
		t.Fatal("keyring's mirrored verb literals have drifted from the owning packages")
	}
}
