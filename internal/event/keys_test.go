package event

import (
	"crypto/ed25519"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeFixtureKeys serializes the deterministic fixture key (seed bytes
// 1..32) in OpenSSH form into a temp dir at test time. The files are
// generated rather than committed: forge push protection rightly refuses
// anything shaped like an SSH private key, synthetic or not, and the
// loaders only care about the wire format, which ssh.MarshalPrivateKey
// produces exactly.
func writeFixtureKeys(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	priv := fixtureKey(t)
	pub := priv.Public().(ed25519.PublicKey)

	block, err := ssh.MarshalPrivateKey(priv, "seed-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		t.Fatal(err)
	}
	encBlock, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "seed-fixture-encrypted", []byte("sealed"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519_encrypted"), pem.EncodeToMemory(encBlock), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readKeyFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// conformance: III.E groundwork — identity is a keypair; the protocol
// fingerprint is the lowercase-hex SHA-256 of the raw 32-byte public key,
// and the OpenSSH display form appears nowhere on the wire.
func TestFingerprintVector(t *testing.T) {
	priv := fixtureKey(t)
	pub := priv.Public().(ed25519.PublicKey)
	fp, err := Fingerprint(pub)
	if err != nil {
		t.Fatal(err)
	}
	const want = "65b60673d6ed884bf01c2c222d82ada0740f29ac3355d6a925c81f17f47a27b8"
	if fp != want {
		t.Fatalf("fingerprint = %s, want %s (sha256 of the raw key)", fp, want)
	}
	if strings.HasPrefix(fp, "SHA256:") {
		t.Fatal("fingerprint must not be the OpenSSH display form")
	}
	if _, err := Fingerprint(ed25519.PublicKey([]byte("short"))); err == nil {
		t.Fatal("non-ed25519 key length must refuse")
	}
}

// conformance: III.E groundwork — OpenSSH ed25519 keys are accepted at key
// load and resolve to the same raw key and protocol fingerprint.
func TestOpenSSHKeyLoading(t *testing.T) {
	dir := writeFixtureKeys(t)
	pub, err := ParsePublicKey(readKeyFile(t, dir, "id_ed25519.pub"))
	if err != nil {
		t.Fatal(err)
	}
	priv, err := ParsePrivateKey(readKeyFile(t, dir, "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	derived := priv.Public().(ed25519.PublicKey)
	if !derived.Equal(pub) {
		t.Fatal("fixture private key does not derive the fixture public key")
	}
	fpPub, _ := Fingerprint(pub)
	fpPriv, _ := Fingerprint(derived)
	if fpPub != fpPriv || fpPub != "65b60673d6ed884bf01c2c222d82ada0740f29ac3355d6a925c81f17f47a27b8" {
		t.Fatalf("OpenSSH-loaded key fingerprints diverge: %s vs %s", fpPub, fpPriv)
	}

	rec, err := Sign(fixtureEvent(), priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Verify(pub); err != nil {
		t.Fatalf("record signed by an OpenSSH-loaded key must verify: %v", err)
	}
}

func TestEncryptedPrivateKeyRefuses(t *testing.T) {
	dir := writeFixtureKeys(t)
	_, err := ParsePrivateKey(readKeyFile(t, dir, "id_ed25519_encrypted"))
	if !errors.Is(err, ErrEncryptedKey) {
		t.Fatalf("encrypted key must refuse with ErrEncryptedKey, got %v", err)
	}
}

func TestKeyParseRefusals(t *testing.T) {
	if _, err := ParsePublicKey([]byte("not a key at all")); err == nil {
		t.Fatal("garbage public key must refuse")
	}
	if _, err := ParsePrivateKey([]byte("-----BEGIN NONSENSE-----\n")); err == nil {
		t.Fatal("garbage private key must refuse")
	}
}
