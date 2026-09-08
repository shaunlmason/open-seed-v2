package halt

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

func fixtureKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func rec(t *testing.T, priv ed25519.PrivateKey, verb, subject, payload, prev string) *event.Record {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	r, err := event.Sign(event.Event{
		V: "seed/0", TS: "2026-09-01T00:00:00Z", Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: prev,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// conformance: III.A — halt.declared stops admission of everything except
// an operator's halt.lifted; the rule is pure data over the chain (the
// boundary enforcement lands with internal/admit in Phase 2).
func TestHaltRefusesEverythingButLift(t *testing.T) {
	priv := fixtureKey(t)
	declared := rec(t, priv, DeclareVerb, "system", `{"reason": "keyring rotation"}`, event.EmptyHash)
	s := StateAt([]*event.Record{declared})
	if !s.Halted || s.Reason != "keyring rotation" || s.By != declared.Event.Actor {
		t.Fatalf("StateAt must project halted+by+reason, got %+v", s)
	}

	ordinary := rec(t, priv, "claim.taken", "c-0001", `{"fence": "f"}`, event.EmptyHash)
	err := Check(s, &ordinary.Event)
	var herr *HaltedError
	if !errors.As(err, &herr) || herr.By != declared.Event.Actor || herr.Reason != "keyring rotation" {
		t.Fatalf("ordinary verb under halt must refuse with actor+reason, got %v", err)
	}

	lift := rec(t, priv, LiftVerb, "system", `{}`, event.EmptyHash)
	if err := Check(s, &lift.Event); err != nil {
		t.Fatalf("the lift must pass under halt, got %v", err)
	}
}

func TestLiftRestoresAdmissionAndTogglesRepeat(t *testing.T) {
	priv := fixtureKey(t)
	chain := []*event.Record{
		rec(t, priv, DeclareVerb, "system", `{"reason": "one"}`, event.EmptyHash),
		rec(t, priv, LiftVerb, "system", `{}`, event.EmptyHash),
	}
	s := StateAt(chain)
	if s.Halted {
		t.Fatalf("lift must clear the halt, got %+v", s)
	}
	ordinary := rec(t, priv, "claim.taken", "c-0001", `{"fence": "f"}`, event.EmptyHash)
	if err := Check(s, &ordinary.Event); err != nil {
		t.Fatalf("ordinary verb after lift must pass, got %v", err)
	}

	chain = append(chain, rec(t, priv, DeclareVerb, "system", `{"reason": "two"}`, event.EmptyHash))
	s = StateAt(chain)
	if !s.Halted || s.Reason != "two" {
		t.Fatalf("re-declare must halt again with the new reason, got %+v", s)
	}
}

func TestShapeRefusals(t *testing.T) {
	priv := fixtureKey(t)
	noReason := rec(t, priv, DeclareVerb, "system", `{}`, event.EmptyHash)
	cases := map[string]event.Event{
		"declare off-system":  rec(t, priv, DeclareVerb, "c-0001", `{"reason": "r"}`, event.EmptyHash).Event,
		"declare no reason":   noReason.Event,
		"declare bad payload": rec(t, priv, DeclareVerb, "system", `{"reason": 7}`, event.EmptyHash).Event,
		"lift off-system":     rec(t, priv, LiftVerb, "c-0001", `{}`, event.EmptyHash).Event,
		"lift with members":   rec(t, priv, LiftVerb, "system", `{"note": "x"}`, event.EmptyHash).Event,
		// The event layer refuses to sign non-object payloads, so these two
		// reach the rule only as unsigned drafts; the rule still refuses.
		"lift null payload": {Verb: LiftVerb, Subject: "system", Payload: json.RawMessage(`null`)},
		"lift non-object":   {Verb: LiftVerb, Subject: "system", Payload: json.RawMessage(`[]`)},
	}
	for name, e := range cases {
		if err := Check(State{}, &e); err == nil {
			t.Errorf("%s must refuse", name)
		}
	}
	ok := rec(t, priv, "claim.taken", "c-0001", `{"fence": "f"}`, event.EmptyHash)
	if err := Check(State{}, &ok.Event); err != nil {
		t.Errorf("non-halt verbs pass shape validation untouched, got %v", err)
	}
	if s := StateAt([]*event.Record{noReason}); s.Halted {
		t.Error("malformed declares must not change replayed state")
	}
}

// The halted refusal dominates shape validation: under halt, a malformed
// non-lift proposal refuses as halted (exit-code mapping depends on the
// typed error), while a malformed lift is still a shape refusal, never a
// pass. A payload-carrying lift in history is likewise no lift at all.
func TestHaltedRefusalDominatesShape(t *testing.T) {
	priv := fixtureKey(t)
	declared := rec(t, priv, DeclareVerb, "system", `{"reason": "drill"}`, event.EmptyHash)
	s := StateAt([]*event.Record{declared})
	if !s.Halted {
		t.Fatalf("precondition: state must be halted, got %+v", s)
	}

	malformedDeclare := rec(t, priv, DeclareVerb, "system", `{}`, event.EmptyHash)
	var herr *HaltedError
	if err := Check(s, &malformedDeclare.Event); !errors.As(err, &herr) {
		t.Fatalf("malformed non-lift under halt must refuse as halted, got %v", err)
	}

	fatLift := rec(t, priv, LiftVerb, "system", `{"note": "x"}`, event.EmptyHash)
	err := Check(s, &fatLift.Event)
	if err == nil || errors.As(err, &herr) {
		t.Fatalf("malformed lift under halt must refuse on shape, got %v", err)
	}

	if s := StateAt([]*event.Record{declared, fatLift}); !s.Halted {
		t.Fatal("a payload-carrying lift in history must not clear the halt")
	}
}

// conformance: III.A — halt gates admission of new events, never the
// validity of admitted history: a halt window inside a chain replays green.
func TestHaltWindowInsideChainVerifies(t *testing.T) {
	priv := fixtureKey(t)
	fp, _ := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	resolve := func(got string) (ed25519.PublicKey, bool) {
		return priv.Public().(ed25519.PublicKey), got == fp
	}
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prev := event.EmptyHash
	steps := []struct{ verb, subject, payload string }{
		{"message.sent", "c-0001", `{"n": 1}`},
		{DeclareVerb, "system", `{"reason": "drill"}`},
		{LiftVerb, "system", `{}`},
		{"message.sent", "c-0001", `{"n": 2}`},
	}
	var records []*event.Record
	for _, st := range steps {
		r := rec(t, priv, st.verb, st.subject, st.payload, prev)
		if _, err := store.Append(r, resolve); err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
		prev, _ = r.Event.Hash()
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil || rep.Count != 4 {
		t.Fatalf("a chain containing a halt window must verify: %+v %v", rep, err)
	}
	if s := StateAt(records); s.Halted {
		t.Fatalf("state after the window must be lifted, got %+v", s)
	}
}
