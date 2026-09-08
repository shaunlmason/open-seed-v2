package event

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixtureEvent() Event {
	return Event{
		V:       "seed/0",
		TS:      "2026-08-30T00:00:00Z",
		Actor:   strings.Repeat("a", 64),
		Verb:    "claim.taken",
		Subject: "c-0001",
		Payload: json.RawMessage(`{"z": true, "n": 1.0, "alpha": "ü"}`),
		Prev:    EmptyHash,
	}
}

func fixtureKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// conformance: III.A — events carry ts, actor fingerprint, verb, subject,
// schema-valid payload, previous-event hash; the canonical form is RFC 8785
// (JCS) per spec/protocol.md. This vector pins field ordering, payload
// key sorting, ES6 number form (1.0 -> 1), and literal UTF-8.
func TestCanonicalJCSVector(t *testing.T) {
	e := fixtureEvent()
	got, err := e.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"actor":"` + strings.Repeat("a", 64) + `","payload":{"alpha":"ü","n":1,"z":true},"prev":"` + EmptyHash + `","subject":"c-0001","ts":"2026-08-30T00:00:00Z","v":"seed/0","verb":"claim.taken"}`
	if string(got) != want {
		t.Fatalf("canonical bytes drifted from the JCS vector:\n got %s\nwant %s", got, want)
	}
}

func TestHashIsLowercaseHexSHA256(t *testing.T) {
	e := fixtureEvent()
	h, err := e.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 64 || strings.ToLower(h) != h {
		t.Fatalf("hash %q is not 64 lowercase hex chars", h)
	}
	h2, _ := e.Hash()
	if h2 != h {
		t.Fatal("hash must be deterministic")
	}
}

// conformance: III.A — any mutation of any canonical field changes the hash
// and breaks the signature.
func TestEveryFieldMutationBreaksHashAndSignature(t *testing.T) {
	priv := fixtureKey(t)
	pub := priv.Public().(ed25519.PublicKey)
	base := fixtureEvent()
	rec, err := Sign(base, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Verify(pub); err != nil {
		t.Fatalf("baseline record must verify: %v", err)
	}
	baseHash, _ := base.Hash()

	mutations := map[string]func(*Event){
		"v":       func(e *Event) { e.V = "seed/1" },
		"ts":      func(e *Event) { e.TS = "2026-08-30T00:00:01Z" },
		"actor":   func(e *Event) { e.Actor = strings.Repeat("b", 64) },
		"verb":    func(e *Event) { e.Verb = "claim.released" },
		"subject": func(e *Event) { e.Subject = "c-0002" },
		"payload": func(e *Event) { e.Payload = json.RawMessage(`{"z": false, "n": 1.0, "alpha": "ü"}`) },
		"prev":    func(e *Event) { e.Prev = strings.Repeat("0", 64) },
	}
	for field, mutate := range mutations {
		e := fixtureEvent()
		mutate(&e)
		h, err := e.Hash()
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		if h == baseHash {
			t.Errorf("mutating %s did not change the hash", field)
		}
		tampered := &Record{Event: e, Sig: rec.Sig}
		if err := tampered.Verify(pub); !errors.Is(err, ErrBadSignature) {
			t.Errorf("mutating %s did not break the signature (got %v)", field, err)
		}
	}
}

func TestVerifyRefusals(t *testing.T) {
	priv := fixtureKey(t)
	pub := priv.Public().(ed25519.PublicKey)
	rec, err := Sign(fixtureEvent(), priv)
	if err != nil {
		t.Fatal(err)
	}

	otherSeed := make([]byte, ed25519.SeedSize)
	otherSeed[0] = 0xff
	otherPub := ed25519.NewKeyFromSeed(otherSeed).Public().(ed25519.PublicKey)
	if err := rec.Verify(otherPub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key must refuse with ErrBadSignature, got %v", err)
	}

	bad := *rec
	bad.Sig = "zz" + rec.Sig[2:]
	if err := bad.Verify(pub); !errors.Is(err, ErrBadSigEncoding) {
		t.Fatalf("non-hex sig must refuse with ErrBadSigEncoding, got %v", err)
	}
	short := *rec
	short.Sig = rec.Sig[:126]
	if err := short.Verify(pub); !errors.Is(err, ErrBadSigEncoding) {
		t.Fatalf("short sig must refuse with ErrBadSigEncoding, got %v", err)
	}
}

func TestSignatureEncoding(t *testing.T) {
	rec, err := Sign(fixtureEvent(), fixtureKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Sig) != 128 || strings.ToLower(rec.Sig) != rec.Sig {
		t.Fatalf("sig %q is not 128 lowercase hex chars", rec.Sig)
	}
}

// conformance: III.A groundwork — the wrapper tolerates field order and
// whitespace; verification recomputes canonical bytes from the parsed
// event, never trusting stored bytes.
func TestRecordWrapperRoundTrip(t *testing.T) {
	priv := fixtureKey(t)
	pub := priv.Public().(ed25519.PublicKey)
	rec, err := Sign(fixtureEvent(), priv)
	if err != nil {
		t.Fatal(err)
	}
	line, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(line), "\n") || strings.Count(string(line), "\n") != 1 {
		t.Fatalf("Marshal must yield one JSONL line, got %q", line)
	}
	back, err := ParseRecord(line)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(pub); err != nil {
		t.Fatalf("round-tripped record must verify: %v", err)
	}

	reordered := `{"sig":"` + rec.Sig + `", "event": {"prev":"` + rec.Event.Prev + `","v":"seed/0","ts":"2026-08-30T00:00:00Z","actor":"` + rec.Event.Actor + `","verb":"claim.taken","subject":"c-0001","payload":{"n": 1.0, "alpha": "ü", "z": true}}}`
	back2, err := ParseRecord([]byte(reordered))
	if err != nil {
		t.Fatal(err)
	}
	if err := back2.Verify(pub); err != nil {
		t.Fatalf("wrapper field order must not matter: %v", err)
	}

	if _, err := ParseRecord([]byte(`{"event": nope}`)); err == nil {
		t.Fatal("garbage record must refuse to parse")
	}
}

func TestPayloadMustBeObject(t *testing.T) {
	for _, payload := range []string{`"string"`, `[1,2]`, `42`, `null`, ``} {
		e := fixtureEvent()
		e.Payload = json.RawMessage(payload)
		if _, err := e.Canonical(); !errors.Is(err, ErrBadPayload) {
			t.Errorf("payload %q must refuse with ErrBadPayload, got %v", payload, err)
		}
		if _, err := e.Hash(); !errors.Is(err, ErrBadPayload) {
			t.Errorf("hash with payload %q must refuse, got %v", payload, err)
		}
		if _, err := Sign(e, fixtureKey(t)); !errors.Is(err, ErrBadPayload) {
			t.Errorf("sign with payload %q must refuse, got %v", payload, err)
		}
	}
	e := fixtureEvent()
	e.Payload = json.RawMessage(" \t{}")
	if _, err := e.Canonical(); err != nil {
		t.Fatalf("leading whitespace before an object payload is fine, got %v", err)
	}
}

func TestEmptyHashConstant(t *testing.T) {
	// SHA-256 of zero bytes, the spec'd genesis prev.
	if EmptyHash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("EmptyHash drifted from the spec constant")
	}
}
