package ledger

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

func signedVersioned(t *testing.T, v, verb, subject, payload, prev string) *event.Record {
	t.Helper()
	priv := fixtureKey(t, 1)
	rec, err := event.Sign(event.Event{
		V: v, TS: "2026-09-01T00:00:00Z", Actor: fingerprintOf(t, priv),
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: prev,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func buildChain(t *testing.T, recs []*event.Record) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), WithClock(clockAt("2026-09-01T00:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	resolve := fixtureResolver(t, fixtureKey(t, 1))
	for _, rec := range recs {
		if _, err := s.Append(rec, resolve); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func chainWithUpgrade(t *testing.T) []*event.Record {
	t.Helper()
	a := signedVersioned(t, "seed/0", "message.sent", "c-0001", `{"n": 1}`, event.EmptyHash)
	ha, _ := a.Event.Hash()
	up := signedVersioned(t, "seed/0", UpgradeVerb, "system", `{"to": "seed/1"}`, ha)
	hup, _ := up.Event.Hash()
	b := signedVersioned(t, "seed/1", "message.sent", "c-0001", `{"n": 2}`, hup)
	return []*event.Record{a, up, b}
}

// conformance: III.A — verification runs against a declared
// supported-versions set with the version active at each chain position,
// so a valid older prefix replays after a protocol upgrade
// (spec/protocol.md; review finding on #75).
func TestUpgradedChainVerifiesWithSupportedSet(t *testing.T) {
	s := buildChain(t, chainWithUpgrade(t))
	resolve := fixtureResolver(t, fixtureKey(t, 1))
	rep, err := s.VerifyFromGenesis(resolve, WithSupportedVersions("seed/0", "seed/1"))
	if err != nil || rep.Count != 3 {
		t.Fatalf("upgraded chain must verify with both versions supported: %+v %v", rep, err)
	}
	if _, err := s.VerifyFromGenesis(resolve, WithSupportedVersions("seed/0")); err == nil {
		t.Fatal("a seed/0-only supported set must refuse the seed/1 suffix")
	}
}

func TestUnsupportedActiveVersionRefuses(t *testing.T) {
	s := buildChain(t, chainWithUpgrade(t))
	resolve := fixtureResolver(t, fixtureKey(t, 1))
	_, err := s.VerifyFromGenesis(resolve, WithSupportedVersions("seed/1"))
	var fail *Failure
	if !errors.As(err, &fail) || fail.Reason != ReasonVersionUnsupported || fail.Position != 0 {
		t.Fatalf("seed/0 prefix under seed/1-only support must refuse as %s@0, got %v", ReasonVersionUnsupported, err)
	}
}

// conformance: III.A — version mismatch refuses with a distinct reason: an
// event whose v differs from the active-at-position version breaks the
// discipline even when the version itself is supported.
func TestVersionMismatchWithoutUpgradeRefuses(t *testing.T) {
	a := signedVersioned(t, "seed/0", "message.sent", "c-0001", `{"n": 1}`, event.EmptyHash)
	ha, _ := a.Event.Hash()
	b := signedVersioned(t, "seed/1", "message.sent", "c-0001", `{"n": 2}`, ha)
	s := buildChain(t, []*event.Record{a, b})
	resolve := fixtureResolver(t, fixtureKey(t, 1))
	_, err := s.VerifyFromGenesis(resolve, WithSupportedVersions("seed/0", "seed/1"))
	var fail *Failure
	if !errors.As(err, &fail) || fail.Reason != ReasonVersionMismatch || fail.Position != 1 {
		t.Fatalf("want %s@1, got %v", ReasonVersionMismatch, err)
	}
}

func TestUpgradePayloadMustNameTarget(t *testing.T) {
	a := signedVersioned(t, "seed/0", "message.sent", "c-0001", `{"n": 1}`, event.EmptyHash)
	ha, _ := a.Event.Hash()
	up := signedVersioned(t, "seed/0", UpgradeVerb, "system", `{"note": "missing to"}`, ha)
	s := buildChain(t, []*event.Record{a, up})
	resolve := fixtureResolver(t, fixtureKey(t, 1))
	_, err := s.VerifyFromGenesis(resolve)
	var fail *Failure
	if !errors.As(err, &fail) || fail.Reason != ReasonBadPayload || fail.Position != 1 {
		t.Fatalf("upgrade without 'to' must refuse as %s@1, got %v", ReasonBadPayload, err)
	}
}

// conformance: III.A — the initial active version is the version genesis
// names in its payload; a genesis event whose own v disagrees refuses as a
// mismatch at position 0 instead of being accepted tautologically (#83
// review finding).
func TestGenesisPayloadBootstrapsActiveVersion(t *testing.T) {
	priv := fixtureKey(t, 1)
	lying := signedVersioned(t, "seed/1", "system.genesis", "system",
		`{"protocol": "seed/0", "governance_root": [{"fingerprint": "x", "public_key": "y"}]}`, event.EmptyHash)
	s := buildChain(t, []*event.Record{lying})
	resolve := fixtureResolver(t, priv)
	_, err := s.VerifyFromGenesis(resolve, WithSupportedVersions("seed/0", "seed/1"))
	var fail *Failure
	if !errors.As(err, &fail) || fail.Reason != ReasonVersionMismatch || fail.Position != 0 {
		t.Fatalf("genesis carrying a v that differs from its named protocol must refuse as %s@0, got %v", ReasonVersionMismatch, err)
	}
}

// WithObserver delivers each record after it fully verifies: in order,
// exactly once, and never past a failure (plans/os-3898f232.md step 1).
func TestObserverSeesVerifiedRecordsOnly(t *testing.T) {
	recs := chainWithUpgrade(t)
	s := buildChain(t, recs)
	resolve := fixtureResolver(t, fixtureKey(t, 1))

	var seen []int
	rep, err := s.VerifyFromGenesis(resolve,
		WithSupportedVersions("seed/0", "seed/1"),
		WithObserver(func(pos int, rec *event.Record) {
			seen = append(seen, pos)
			if rec == nil {
				t.Error("observer must receive the record")
			}
		}))
	if err != nil || rep.Count != 3 {
		t.Fatalf("green chain: %+v %v", rep, err)
	}
	if len(seen) != 3 || seen[0] != 0 || seen[1] != 1 || seen[2] != 2 {
		t.Fatalf("observer must see every record once, in order, got %v", seen)
	}
	if rep.ActiveVersion != "seed/1" {
		t.Fatalf("report must carry the active version, got %q", rep.ActiveVersion)
	}

	// Under a seed/0-only set the same chain fails at position 2; the
	// observer must not see the failing record.
	seen = nil
	if _, err := s.VerifyFromGenesis(resolve, WithSupportedVersions("seed/0"),
		WithObserver(func(pos int, rec *event.Record) {
			seen = append(seen, pos)
		})); err == nil {
		t.Fatal("a seed/0-only set must refuse the upgraded suffix")
	}
	if len(seen) != 2 {
		t.Fatalf("observer must stop at the failure, got %v", seen)
	}
}

// The upgrade schema is one shared definition (plans/os-3898f232.md):
// admission refuses schema-broken upgrades up front, while admitted
// history containing an off-system upgrade-verb event stays verifiable.
func TestValidateUpgradeShape(t *testing.T) {
	cases := map[string]struct {
		subject, payload string
		ok               bool
	}{
		"good":       {"system", `{"to": "seed/1"}`, true},
		"missing to": {"system", `{"note": "x"}`, false},
		"empty to":   {"system", `{"to": ""}`, false},
		"off-system": {"c-0001", `{"to": "seed/1"}`, false},
	}
	for name, tc := range cases {
		rec := signedVersioned(t, "seed/0", UpgradeVerb, tc.subject, tc.payload, event.EmptyHash)
		if err := ValidateUpgradeShape(&rec.Event); (err == nil) != tc.ok {
			t.Errorf("%s: got %v", name, err)
		}
	}
	plain := signedVersioned(t, "seed/0", "message.sent", "c-0001", `{"n": 1}`, event.EmptyHash)
	if err := ValidateUpgradeShape(&plain.Event); err != nil {
		t.Errorf("non-upgrade verbs pass through, got %v", err)
	}

	// History containing an off-system upgrade-verb event is an ordinary
	// event to the verifier: no wedge, no version switch.
	a := signedVersioned(t, "seed/0", UpgradeVerb, "c-0001", `{"to": "seed/9"}`, event.EmptyHash)
	ha, _ := a.Event.Hash()
	b := signedVersioned(t, "seed/0", "message.sent", "c-0001", `{"n": 2}`, ha)
	s := buildChain(t, []*event.Record{a, b})
	rep, err := s.VerifyFromGenesis(fixtureResolver(t, fixtureKey(t, 1)))
	if err != nil || rep.Count != 2 || rep.ActiveVersion != "seed/0" {
		t.Fatalf("off-system upgrade verb in history must stay ordinary: %+v %v", rep, err)
	}
}
