package event

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Invalid JSON inside the raw payload passes the object sniff but must fail
// canonicalization, and every consumer must propagate that failure.
func TestInvalidPayloadJSONFailsEverywhere(t *testing.T) {
	e := fixtureEvent()
	e.Payload = json.RawMessage(`{"broken": `)
	if _, err := e.Canonical(); err == nil {
		t.Fatal("truncated payload JSON must fail canonicalization")
	}
	if _, err := e.Hash(); err == nil {
		t.Fatal("truncated payload JSON must fail hashing")
	}
	if _, err := Sign(e, fixtureKey(t)); err == nil {
		t.Fatal("truncated payload JSON must fail signing")
	}
	pub := fixtureKey(t).Public().(ed25519.PublicKey)
	rec := &Record{Event: e, Sig: strings.Repeat("ab", 64)}
	if err := rec.Verify(pub); err == nil {
		t.Fatal("verify must propagate canonicalization failure")
	}
	if _, err := rec.Marshal(); err == nil {
		t.Fatal("marshal must propagate invalid payload JSON")
	}
}

func TestNonEd25519OpenSSHKeysRefuse(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(ec.Public())
	if err != nil {
		t.Fatal(err)
	}
	if _, perr := ParsePublicKey(ssh.MarshalAuthorizedKey(sshPub)); perr == nil || !strings.Contains(perr.Error(), "not ed25519") {
		t.Fatalf("ecdsa public key must refuse as not ed25519, got %v", perr)
	}

	block, err := ssh.MarshalPrivateKey(ec, "ecdsa-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, kerr := ParsePrivateKey(pem.EncodeToMemory(block)); kerr == nil || !strings.Contains(kerr.Error(), "not ed25519") {
		t.Fatalf("ecdsa private key must refuse as not ed25519, got %v", kerr)
	}
}
