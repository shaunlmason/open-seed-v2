package boundary

// The canonical form is RFC 8785 (plans/os-f11601e0.md D3, D4): the
// card's bytes are what an independent JCS verifier reconstructs, so
// a signature made here is one a stranger can check. The property is
// the drill, not an example, because the regression class is "two
// canonicalizers in one tree disagree" and an example only catches
// the disagreement it happens to name.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gowebpki/jcs"
)

// jcsOf is the independent expectation: the card marshaled, its
// signature dropped, run through the JCS transform. It is deliberately
// not Canonical's own code path, so the two agreeing means something.
func jcsOf(t *testing.T, c Card) []byte {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	delete(generic, "signature")
	stripped, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jcs.Transform(stripped)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// conformance: plans/os-f11601e0.md AC1 — Canonical equals the JCS
// transform over a corpus including the characters the hand-rolled
// encoder got wrong. U+2028 and U+2029 are the reason this card
// exists: encoding/json escapes them, JCS does not, and check
// constrains Name only to be non-empty, so both are admissible.
func TestCanonicalIsRFC8785(t *testing.T) {
	base := func() Card {
		return Card{
			Name:     "open-seed",
			Protocol: "seed/8",
			Ingress:  CardIngress{Kinds: []string{"cross-repo"}, Through: "git@example:o/r.git"},
			Squads:   []CardSquad{{Name: "core", Tiers: []string{"standard"}}},
			// Artifacts is deliberately the shared slice's copy: a
			// canonicalizer that mutated its input would surface here.
			Artifacts: append([]string{}, ArtifactKinds...),
			Signer:    strings.Repeat("a", 64),
			Signature: strings.Repeat("b", 128),
		}
	}
	for name, mutate := range map[string]func(*Card){
		"plain":                    func(c *Card) {},
		"line separator in name":   func(c *Card) { c.Name = "open\u2028seed" },
		"paragraph separator":      func(c *Card) { c.Name = "open\u2029seed" },
		"both, in the ingress":     func(c *Card) { c.Ingress.Through = "git@e:\u2028o/\u2029r.git" },
		"both, in a squad name":    func(c *Card) { c.Squads[0].Name = "co\u2028re\u2029" },
		"html-significant runes":   func(c *Card) { c.Name = `a<b>c&d"e` },
		"non-ascii":                func(c *Card) { c.Name = "sæd-ünïcode-種" },
		"an emoji outside the bmp": func(c *Card) { c.Name = "seed \U0001F331" },
		"no squads":                func(c *Card) { c.Squads = []CardSquad{} },
		"unsigned":                 func(c *Card) { c.Signer, c.Signature = "", "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := base()
			mutate(&c)
			got, err := c.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if want := jcsOf(t, c); string(got) != string(want) {
				t.Fatalf("Canonical is not the JCS transform:\n got %s\nwant %s", got, want)
			}
			// The signature is never in the signed bytes, whatever it holds.
			if strings.Contains(string(got), `"signature"`) {
				t.Fatalf("the canonical bytes carry the signature: %s", got)
			}
		})
	}
}

// conformance: plans/os-f11601e0.md AC2 — the switch to JCS changes
// the checked-in card's bytes not at all, so no signature anywhere is
// invalidated by it. The digest is the one measured on the tree
// immediately before the change; a card edit that moves it is meant to
// move it, and re-signing is then the operator's act.
//
// This asserts BYTES, not verification. docs/decisions.md records
// that the fixture card is signed by a throwaway key kept out of the
// tree, so no test here can verify its signature: a public key is
// derivable from neither a fingerprint nor a signature. Saying so is
// better than a skip that reads like coverage.
func TestCheckedInCardCanonicalizesUnchanged(t *testing.T) {
	const before = "9c8aa4d223133bb94d098c254d0e77b900cbdcc14357a5f43c56e706df703960"
	raw, err := os.ReadFile("../../boundary/card.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := c.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if sum := hex.EncodeToString(hashOf(canon)); sum != before {
		t.Fatalf("the checked-in card's canonical bytes moved: %s, was %s\n%s", sum, before, canon)
	}
	if c.Signature == "" || c.Signer == "" {
		t.Fatal("the checked-in card is signed; this test's premise is that the switch left it alone")
	}
}

func hashOf(b []byte) []byte { sum := sha256.Sum256(b); return sum[:] }

// conformance: plans/os-f11601e0.md AC4 — Verify over a keypair the
// test generates, so the mechanism is drilled end to end without the
// repository's absent operator key. A good signature verifies; the
// three ways one can be wrong each refuse.
func TestVerifyOverAGeneratedKey(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	card := func() *Card {
		c := &Card{
			Name:      "stranger",
			Protocol:  "seed/8",
			Ingress:   CardIngress{Kinds: []string{"cross-repo"}, Through: "git@example:o/r.git"},
			Squads:    []CardSquad{{Name: "core", Tiers: []string{"standard"}}},
			Artifacts: append([]string{}, ArtifactKinds...),
		}
		if err := Sign(c, priv); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if err := Verify(card(), pub); err != nil {
		t.Fatalf("a card signed by the key verifies against it: %v", err)
	}
	other, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(card(), other); err == nil {
		t.Fatal("a card signed by another key must refuse")
	}
	truncated := card()
	truncated.Signature = truncated.Signature[:len(truncated.Signature)-2]
	if err := Verify(truncated, pub); err == nil {
		t.Fatal("a truncated signature must refuse")
	}
	unsigned := card()
	unsigned.Signature = ""
	if err := Verify(unsigned, pub); err == nil {
		t.Fatal("an absent signature must refuse")
	}
	// The bytes are what is signed: a field edited after signing
	// refuses, which is the property the whole card rests on.
	tampered := card()
	tampered.Name = "not-stranger"
	if err := Verify(tampered, pub); err == nil {
		t.Fatal("a card edited after signing must refuse")
	}
}
