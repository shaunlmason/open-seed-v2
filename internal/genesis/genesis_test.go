package genesis

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

func key(t *testing.T, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func now(t *testing.T) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, "2026-09-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// conformance: III.A — a genesis event names the governance root and
// protocol version; the ledger begins with it and verifies from it.
func TestInitWritesVerifyingGenesis(t *testing.T) {
	signer := key(t, 1)
	other := key(t, 7).Public().(ed25519.PublicKey)
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Init(store, signer, []ed25519.PublicKey{other}, now(t))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Protocol != "seed/0" {
		t.Fatalf("genesis protocol = %q", payload.Protocol)
	}
	if len(payload.GovernanceRoot) != 2 {
		t.Fatalf("governance root has %d keys, want signer + 1 operator", len(payload.GovernanceRoot))
	}
	resolve, err := payload.Resolver(rec.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil || rep.Count != 1 {
		t.Fatalf("genesis chain must verify: %+v %v", rep, err)
	}
}

func TestInitRefusesNonEmptyLedger(t *testing.T) {
	signer := key(t, 1)
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(store, signer, nil, now(t)); err != nil {
		t.Fatal(err)
	}
	before, count, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Init(store, signer, nil, now(t))
	if !errors.Is(err, ledger.ErrNotEmpty) {
		t.Fatalf("re-init must refuse with ErrNotEmpty, got %v", err)
	}
	after, count2, err := store.Tip()
	if err != nil || after != before || count2 != count {
		t.Fatalf("refused init must write nothing: %s/%d vs %s/%d %v", before, count, after, count2, err)
	}
}

func TestDuplicateOperatorKeysDeduplicate(t *testing.T) {
	signer := key(t, 1)
	rec, err := Build(signer, []ed25519.PublicKey{signer.Public().(ed25519.PublicKey)}, now(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.GovernanceRoot) != 1 {
		t.Fatalf("duplicate keys must deduplicate, got %d entries", len(p.GovernanceRoot))
	}
}

func TestParseRefusals(t *testing.T) {
	signer := key(t, 1)
	good, err := Build(signer, nil, now(t))
	if err != nil {
		t.Fatal(err)
	}
	wrongVerb := *good
	wrongVerb.Event.Verb = "claim.taken"
	wrongSubject := *good
	wrongSubject.Event.Subject = "c-0001"
	wrongPrev := *good
	wrongPrev.Event.Prev = strings.Repeat("0", 64)
	badPayload := *good
	badPayload.Event.Payload = json.RawMessage(`{"protocol": ""}`)
	for name, rec := range map[string]*event.Record{
		"wrong verb": &wrongVerb, "wrong subject": &wrongSubject,
		"wrong prev": &wrongPrev, "empty payload fields": &badPayload,
	} {
		if _, err := Parse(rec); err == nil {
			t.Errorf("%s must refuse to parse as genesis", name)
		}
	}
}

func TestResolverRefusals(t *testing.T) {
	signer := key(t, 1)
	rec, err := Build(signer, nil, now(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Resolver(strings.Repeat("f", 64)); err == nil {
		t.Fatal("a genesis signer outside the governance root must refuse")
	}
	corrupt := *p
	corrupt.GovernanceRoot = []RootKey{{Fingerprint: p.GovernanceRoot[0].Fingerprint, PublicKey: "zz"}}
	if _, err := corrupt.Resolver(rec.Event.Actor); err == nil {
		t.Fatal("a non-hex root key must refuse")
	}
	swapped := *p
	swapped.GovernanceRoot = []RootKey{{Fingerprint: strings.Repeat("a", 64), PublicKey: p.GovernanceRoot[0].PublicKey}}
	if _, err := swapped.Resolver(rec.Event.Actor); err == nil {
		t.Fatal("a fingerprint that does not match its key must refuse")
	}
}

func TestInitSurfacesUnreadableStore(t *testing.T) {
	dir := t.TempDir()
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segments", "2026-09-01.jsonl"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(store, key(t, 1), nil, now(t)); err == nil {
		t.Fatal("init over an unparseable stream must error")
	}
}
