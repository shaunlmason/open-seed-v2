package offers

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func testKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func fpOf(t *testing.T, k ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func signed(t *testing.T, key ed25519.PrivateKey, verb, subject, payload string) *event.Record {
	t.Helper()
	rec, err := event.Sign(event.Event{V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fpOf(t, key), Verb: verb, Subject: subject,
		Payload: json.RawMessage(payload), Prev: strings.Repeat("0", 64)}, key)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// ring builds a keyring with no genesis: no capability is held by
// anyone, which is the boundary these helpers must fail closed at.
func TestEligibilityFailsClosedWithoutAKeyring(t *testing.T) {
	o := transition.OfferFact{Capabilities: []string{keyring.CapClaim}}
	if Eligible(nil, "anyone", "trivial", o) {
		t.Fatal("no keyring, no eligibility")
	}
	if Authorized(nil, transition.OfferFact{Pos: 0}) || Authorized([]*event.Record{}, transition.OfferFact{Pos: -1}) {
		t.Fatal("an offer outside the prefix is unauthorized")
	}
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	if rows := Live(nil, topology.Derive(nil, nil, table), nil, "anyone", time.Now()); len(rows) != 0 {
		t.Fatalf("no keyring lists nothing: %+v", rows)
	}
	if rows := Live(nil, nil, keyring.New(), "anyone", time.Now()); len(rows) != 0 {
		t.Fatalf("no derivation lists nothing: %+v", rows)
	}
	// A bridge with no table or no ring reports the delta it can and
	// wakes nobody; a cursor outside the prefix is clamped.
	worker := testKey(2)
	records := []*event.Record{
		signed(t, worker, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`),
		signed(t, worker, "contract.specified", "c-1", `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`),
	}
	res := Bridge(records, nil, nil, time.Now(), 99, nil)
	if res.Since != 2 || res.Position != 2 || len(res.BecameReady) != 0 || len(res.Woken) != 0 {
		t.Fatalf("no table: nothing derived, the cursor clamped: %+v", res)
	}
	res = Bridge(records, table, nil, time.Now(), -5, nil)
	if res.Since != 0 || !reflect.DeepEqual(res.BecameReady, []string{"c-1"}) || len(res.Candidates) != 0 {
		t.Fatalf("no ring: the delta is derived, no candidate is matched: %+v", res)
	}
}

// recorder is a wake channel that remembers, and can fail.
type recorder struct {
	woken []string
	fail  bool
}

func (r *recorder) Wake(actor string) error {
	r.woken = append(r.woken, actor)
	if r.fail {
		return errors.New("the channel is down")
	}
	return nil
}

// conformance: III.F row 12 (advisory wakes where a channel exists) —
// the bridge scopes the offer, wakes only through a registered
// channel, reports a failed wake and never acts on it.
func TestBridgeMatchesScopesAndWakesThroughChannels(t *testing.T) {
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	root, supervisor, workerA, workerB := testKey(1), testKey(3), testKey(4), testKey(5)
	enroll := func(k ed25519.PrivateKey, name, cap string) []*event.Record {
		pub := k.Public().(ed25519.PublicKey)
		return []*event.Record{
			signed(t, root, keyring.VerbEnrolled, fpOf(t, k), fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), name)),
			signed(t, root, keyring.VerbGranted, fpOf(t, k), `{"capability": "`+cap+`"}`),
		}
	}
	g, err := genesis.Build(root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	records := []*event.Record{g, signed(t, root, "system.protocol.upgraded", "system", `{"to": "`+version.Seed1+`"}`)}
	records = append(records, enroll(supervisor, "supervisor", keyring.CapSupervise)...)
	records = append(records, enroll(workerA, "a", keyring.CapClaim)...)
	records = append(records, enroll(workerB, "b", keyring.CapClaim)...)
	ring, _, err := keyring.StateAt(records)
	if err != nil {
		t.Fatal(err)
	}
	if ring == nil || !ring.HasAnyCapability(fpOf(t, workerA), []string{keyring.CapClaim}) {
		t.Fatal("the genesis seats a keyring with the grants")
	}
	cursor := len(records)
	records = append(records,
		signed(t, root, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`),
		signed(t, root, "contract.specified", "c-1", `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`),
		signed(t, supervisor, transition.OfferPublishedVerb, "c-1", `{"expires": "2099-01-01T00:00:00Z", "eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}}`),
	)
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	chA := &recorder{}
	chB := &recorder{fail: true}
	res := Bridge(records, table, ring, now, cursor, Channels{fpOf(t, workerA): chA, fpOf(t, workerB): chB})
	// Both claim workers and the root (operator standing satisfies
	// every scope) are candidates; only the two channels are woken.
	if !reflect.DeepEqual(res.BecameReady, []string{"c-1"}) || len(res.Candidates) != 1 || len(res.Candidates[0].Actors) != 3 {
		t.Fatalf("the claim workers and the operator are candidates for the scoped offer: %+v", res)
	}
	errs := map[string]string{}
	for _, w := range res.Woken {
		errs[w.Actor] = w.Error
	}
	if len(res.Woken) != 2 || errs[fpOf(t, workerA)] != "" || errs[fpOf(t, workerB)] != "the channel is down" || chA.woken[0] != fpOf(t, workerA) {
		t.Fatalf("one wake per channel, a failure reported and nothing else: %+v", res.Woken)
	}
	// No channel for B: B stays a candidate and is not woken.
	res = Bridge(records, table, ring, now, cursor, Channels{fpOf(t, workerA): chA})
	if len(res.Candidates[0].Actors) != 3 || len(res.Woken) != 1 {
		t.Fatalf("no channel is a no-op, not a lost candidate: %+v", res)
	}
	// From the tip nothing became ready.
	if res := Bridge(records, table, ring, now, len(records), Channels{}); len(res.BecameReady) != 0 || len(res.Woken) != 0 {
		t.Fatalf("no delta, no wake: %+v", res)
	}
	// The poll lists the same offer for A and nothing for an unknown actor.
	d := topology.DeriveRecords(records, table)
	if rows := Live(records, d, ring, fpOf(t, workerA), now); len(rows) != 1 || rows[0].Subject != "c-1" || rows[0].Tier != "trivial" {
		t.Fatalf("the poll lists the live eligible offer: %+v", rows)
	}
	if rows := Live(records, d, ring, "nobody", now); len(rows) != 0 {
		t.Fatalf("an unknown actor lists nothing: %+v", rows)
	}
	// Scopes: a capability the actor lacks, a tier the subject is not,
	// and a tuple set the actor's grants do not cite.
	if Eligible(ring, fpOf(t, workerA), "trivial", transition.OfferFact{Capabilities: []string{keyring.CapVerdict}}) {
		t.Fatal("a scoped capability the actor lacks is ineligible")
	}
	if Eligible(ring, fpOf(t, workerA), "standard", transition.OfferFact{Tiers: []string{"trivial"}}) {
		t.Fatal("a tier outside the scope is ineligible")
	}
	if Eligible(ring, fpOf(t, workerA), "trivial", transition.OfferFact{Tuples: []tuple.Tuple{{Harness: "x"}}}) {
		t.Fatal("a tuple scope nobody cites is ineligible")
	}
	if !Eligible(ring, fpOf(t, root), "trivial", transition.OfferFact{Capabilities: []string{keyring.CapVerdict}, Tuples: []tuple.Tuple{{Harness: "x"}}}) {
		t.Fatal("operator standing satisfies every scope")
	}
}
