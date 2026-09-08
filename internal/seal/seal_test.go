package seal

// The crypto-seam drills (plans/os-3128535a.md): the commitment
// round-trips and detects tampering; encryption opens only for
// recipients; rotation changes recipients without changing the
// commitment; the header tag scan matches keyring-derived tags both
// directions; and the empty-seal hole is closed at construction and at
// parse.

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func sealKey(t *testing.T, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = first + byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func pub(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

func TestCommitmentRoundTripAndMismatch(t *testing.T) {
	env, err := NewEnvelope([]string{"go test ./...", "gofmt -l ."})
	if err != nil {
		t.Fatal(err)
	}
	c1, err := env.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := env.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	parsed, c2, err := ParseEnvelope(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if c2 != c1 || len(parsed.Checks) != 2 {
		t.Fatalf("the parsed envelope must re-derive the same commitment: %s vs %s", c2, c1)
	}
	// A tampered check body re-derives a different commitment: the
	// caller comparing against the ledger's value catches it.
	tampered := strings.Replace(string(plaintext), "go test", "true # skip", 1)
	_, c3, err := ParseEnvelope([]byte(tampered))
	if err != nil {
		t.Fatal(err)
	}
	if c3 == c1 {
		t.Fatal("a tampered envelope must not re-derive the committed hash")
	}
}

func TestEmptySealRefusedAtBothEnds(t *testing.T) {
	if _, err := NewEnvelope(nil); err == nil {
		t.Fatal("an empty check list must not seal — a vacuous pass in waiting")
	}
	// The raw-crafted half: an envelope with zero checks that decrypts
	// cleanly still refuses at parse.
	salt := strings.Repeat("ab", 32)
	if _, _, err := ParseEnvelope([]byte(`{"salt": "` + salt + `", "checks": []}`)); err == nil {
		t.Fatal("a zero-check envelope must refuse at parse")
	}
	if _, _, err := ParseEnvelope([]byte(`{"checks": ["x"]}`)); err == nil {
		t.Fatal("a saltless envelope must refuse at parse")
	}
	// A degenerate salt surrenders the commitment's dictionary
	// resistance, so shape, not presence, is what parse validates
	// (review finding on the task PR).
	for _, bad := range []string{`"x"`, `"AB` + salt[2:] + `"`, `"` + salt[:62] + `"`} {
		if _, _, err := ParseEnvelope([]byte(`{"salt": ` + bad + `, "checks": ["x"]}`)); err == nil {
			t.Fatalf("salt %s must refuse at parse", bad)
		}
	}
}

func TestEncryptOpensOnlyForRecipients(t *testing.T) {
	verifier, implementer := sealKey(t, 1), sealKey(t, 50)
	env, err := NewEnvelope([]string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	plaintext, _ := env.Canonical()
	ct, err := Encrypt(plaintext, []ed25519.PublicKey{pub(verifier)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(ct, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatal("the recipient must recover the exact plaintext")
	}
	// The capability audit's cryptographic core: a non-recipient
	// (implementing) identity fails, with the rotation-lag error type.
	if _, err := Decrypt(ct, implementer); err == nil {
		t.Fatal("a non-recipient identity must not decrypt")
	} else if _, ok := err.(*NotRecipientError); !ok {
		t.Fatalf("the refusal names the recipient boundary, got %T: %v", err, err)
	}
	if _, err := Encrypt(plaintext, nil); err == nil {
		t.Fatal("an empty recipient set must refuse — nothing could ever unseal it")
	}
}

func TestRotationChangesRecipientsNotCommitment(t *testing.T) {
	old, next := sealKey(t, 1), sealKey(t, 80)
	env, err := NewEnvelope([]string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	plaintext, _ := env.Canonical()
	commitment, _ := env.Commitment()
	ct1, err := Encrypt(plaintext, []ed25519.PublicKey{pub(old)})
	if err != nil {
		t.Fatal(err)
	}
	// Rotate: decrypt with the old identity, re-encrypt to the new
	// keyring only.
	pt, err := Decrypt(ct1, old)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := Encrypt(pt, []ed25519.PublicKey{pub(next)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(ct2, old); err == nil {
		t.Fatal("the revoked identity must be locked out after rotation")
	}
	got, err := Decrypt(ct2, next)
	if err != nil {
		t.Fatal(err)
	}
	_, c, err := ParseEnvelope(got)
	if err != nil || c != commitment {
		t.Fatalf("rotation must not change the commitment: %s vs %s (%v)", c, commitment, err)
	}
}

func TestRecipientTagsMatchKeyring(t *testing.T) {
	a, b, c := sealKey(t, 1), sealKey(t, 90), sealKey(t, 120)
	ct, err := Encrypt([]byte("x"), []ed25519.PublicKey{pub(a), pub(b)})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := RecipientTags(ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("two recipients, got tags %v", tags)
	}
	present := map[string]bool{}
	for _, tag := range tags {
		present[tag] = true
	}
	for _, k := range []ed25519.PrivateKey{a, b} {
		tag, err := Tag(pub(k))
		if err != nil {
			t.Fatal(err)
		}
		if !present[tag] {
			t.Fatalf("recipient tag %s must appear in the header, got %v", tag, tags)
		}
	}
	cTag, err := Tag(pub(c))
	if err != nil {
		t.Fatal(err)
	}
	if present[cTag] {
		t.Fatal("a key outside the recipient set must not match a header tag")
	}
	if _, err := RecipientTags([]byte("not an age file")); err == nil {
		t.Fatal("non-age bytes must refuse the scan")
	}
}
