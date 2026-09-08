package event

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func validLine(t *testing.T) (string, ed25519.PublicKey) {
	t.Helper()
	priv := fixtureKey(t)
	rec, err := Sign(fixtureEvent(), priv)
	if err != nil {
		t.Fatal(err)
	}
	line, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(line), "\n"), priv.Public().(ed25519.PublicKey)
}

// conformance: III.A — there is exactly one accepted wire form for a
// record: unknown fields, duplicate keys (payload included), and trailing
// data refuse at parse, so no unsigned material can ride a valid signature
// into storage (review finding on #76).
func TestStrictRecordParsing(t *testing.T) {
	line, pub := validLine(t)

	if rec, err := ParseRecord([]byte(line)); err != nil {
		t.Fatalf("valid record must parse: %v", err)
	} else if err := rec.Verify(pub); err != nil {
		t.Fatalf("valid record must verify: %v", err)
	}

	cases := map[string]string{
		"unknown wrapper field": strings.Replace(line, `{"event":`, `{"smuggled":1,"event":`, 1),
		"unknown event field":   strings.Replace(line, `{"v":"seed/0"`, `{"extra":"unsigned","v":"seed/0"`, 1),
		"duplicate event key":   strings.Replace(line, `{"v":"seed/0"`, `{"v":"seed/9","v":"seed/0"`, 1),
		"duplicate payload key": strings.Replace(line, `"alpha":"ü"`, `"alpha":"x","alpha":"ü"`, 1),
		"duplicate wrapper key": strings.Replace(line, `{"event":`, `{"sig":"00","event":`, 1),
		"trailing data":         line + ` {"more":true}`,
		"non-string key":        `{"event": {1:2}}`,
	}
	for name, mutated := range cases {
		if mutated == line {
			t.Fatalf("case %q did not mutate the line", name)
		}
		if _, err := ParseRecord([]byte(mutated)); err == nil {
			t.Errorf("%s must refuse to parse:\n%s", name, mutated)
		}
	}
}

// conformance: III.A — the signature encoding is lowercase hex only;
// uppercase decodes to the same bytes and must refuse (review finding on
// #76).
func TestUppercaseSignatureRefuses(t *testing.T) {
	priv := fixtureKey(t)
	rec, err := Sign(fixtureEvent(), priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)

	upper := *rec
	upper.Sig = strings.ToUpper(rec.Sig)
	if upper.Sig == rec.Sig {
		t.Fatal("fixture signature has no letters; pick a different fixture")
	}
	if err := upper.Verify(pub); err != ErrBadSigEncoding {
		t.Fatalf("uppercase sig must refuse with ErrBadSigEncoding, got %v", err)
	}
	mixed := *rec
	mixed.Sig = rec.Sig[:10] + strings.ToUpper(rec.Sig[10:12]) + rec.Sig[12:]
	if mixed.Sig != rec.Sig {
		if err := mixed.Verify(pub); err != ErrBadSigEncoding {
			t.Fatalf("mixed-case sig must refuse with ErrBadSigEncoding, got %v", err)
		}
	}
}

func TestDuplicateKeysInsideArraysRefuse(t *testing.T) {
	line, _ := validLine(t)
	mutated := strings.Replace(line, `"alpha":"ü"`, `"alpha":[{"x":1,"x":2}]`, 1)
	if mutated == line {
		t.Fatal("mutation did not apply")
	}
	if _, err := ParseRecord([]byte(mutated)); err == nil {
		t.Fatal("duplicate keys inside array elements must refuse")
	}
	ok := strings.Replace(line, `"alpha":"ü"`, `"alpha":[{"x":1},{"x":2}]`, 1)
	if _, err := ParseRecord([]byte(ok)); err != nil {
		t.Fatalf("distinct keys across array elements are fine, got %v", err)
	}
}

func TestTruncatedRecordRefuses(t *testing.T) {
	for _, frag := range []string{`{"event":{"v":`, `{"event":[`, `{"event"`} {
		if _, err := ParseRecord([]byte(frag)); err == nil {
			t.Fatalf("truncated record %q must refuse", frag)
		}
	}
}
