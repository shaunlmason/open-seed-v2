package keyring_test

// The sealer grant-disjointness drills (plans/os-3128535a.md): sealed
// checks are authored under a grant disjoint from implementation
// grants, and the keyring is where the disjointness binds — sealer
// cannot join claim or operator, and neither can join sealer, in
// either grant order. Plus the Granted enumerator the recipient set
// derives from.

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

func TestSealerGrantDisjointness(t *testing.T) {
	root := key(t, 1)
	cases := []struct {
		name     string
		held     string
		granting string
	}{
		{"sealer onto a claim holder", keyring.CapClaim, keyring.CapSealer},
		{"sealer onto an operator", keyring.CapOperator, keyring.CapSealer},
		{"claim onto a sealer", keyring.CapSealer, keyring.CapClaim},
		{"operator onto a sealer", keyring.CapSealer, keyring.CapOperator},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := key(t, 40)
			s := seeded(t, root)
			if err := s.Advance(rec(t, root, keyring.VerbEnrolled, fp(t, agent), enrollPayload(t, agent, "agent", "a"))); err != nil {
				t.Fatal(err)
			}
			if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, agent), `{"capability": "`+c.held+`"}`)); err != nil {
				t.Fatal(err)
			}
			err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, agent), `{"capability": "`+c.granting+`"}`))
			if err == nil {
				t.Fatalf("granting %s to a key holding %s must refuse", c.granting, c.held)
			}
			if !strings.Contains(err.Error(), "disjoint") {
				t.Fatalf("the refusal names the disjointness rule: %v", err)
			}
		})
	}
	// The verdict lane stays compatible with sealing: a verifier may
	// also hold sealer (both sit outside implementation), so the
	// refusal is exactly about the implementation lanes.
	agent := key(t, 40)
	s := seeded(t, root)
	if err := s.Advance(rec(t, root, keyring.VerbEnrolled, fp(t, agent), enrollPayload(t, agent, "agent", "a"))); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{keyring.CapVerdict, keyring.CapSealer} {
		if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, agent), `{"capability": "`+capability+`"}`)); err != nil {
			t.Fatalf("verdict and sealer may co-hold: %v", err)
		}
	}
}

func TestGrantedEnumeratesActiveHolders(t *testing.T) {
	root, a, b, c := key(t, 1), key(t, 41), key(t, 42), key(t, 43)
	s := seeded(t, root)
	for i, priv := range []ed25519.PrivateKey{a, b, c} {
		name := string(rune('a' + i))
		if err := s.Advance(rec(t, root, keyring.VerbEnrolled, fp(t, priv), enrollPayload(t, priv, "agent", name))); err != nil {
			t.Fatal(err)
		}
	}
	for _, priv := range []ed25519.PrivateKey{a, b} {
		if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, priv), `{"capability": "`+keyring.CapVerdict+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, c), `{"capability": "`+keyring.CapClaim+`"}`)); err != nil {
		t.Fatal(err)
	}
	got := s.Granted(keyring.CapVerdict)
	if len(got) != 2 {
		t.Fatalf("two verdict-granted keys, got %d", len(got))
	}
	// A suspended holder leaves the recipient set: rotation after a
	// standing change re-derives from here.
	if err := s.Advance(rec(t, root, keyring.VerbSuspended, fp(t, a), `{"reason": "drill"}`)); err != nil {
		t.Fatal(err)
	}
	got = s.Granted(keyring.CapVerdict)
	if len(got) != 1 || !got[0].Key.Equal(b.Public().(ed25519.PublicKey)) {
		t.Fatalf("only the active verdict holder remains, got %d", len(got))
	}
}
