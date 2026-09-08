package admit

// The generalized grant rule (plans/os-3979d48b.md; conformance III.E):
// grants are capability data checked at admission on every verb,
// operator-only verbs refuse non-operator keys structurally, delegation
// via actor.granted works beyond the root, and kind is an assertion
// nothing security-relevant reads.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// grantFixture stands up a seed/1 chain with the root plus two enrolled
// non-operator actors, returning a fresh context.
func grantFixture(t *testing.T) (*Context, ed25519.PrivateKey, ed25519.PrivateKey, ed25519.PrivateKey, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	worker := fixtureKey(t, 2)
	maintainer := fixtureKey(t, 3)
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range []ed25519.PrivateKey{signer, worker, maintainer} {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, worker), enrollBody(t, worker, "agent", "worker"))
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, maintainer), enrollBody(t, maintainer, "service", "maint"))
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	step := func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	return ctx, signer, worker, maintainer, step
}

func TestGrantChecksOperatorVerbs(t *testing.T) {
	ctx, signer, worker, _, _ := grantFixture(t)
	cases := map[string]struct{ verb, subject, payload string }{
		"halt declare": {"system.halt.declared", "system", `{"reason": "x"}`},
		"halt lift":    {"system.halt.lifted", "system", `{}`},
		"upgrade":      {ledger.UpgradeVerb, "system", `{"to": "seed/9"}`},
		"actor verb":   {keyring.VerbSuspended, fpOf(t, worker), `{"reason": "x"}`},
	}
	for name, c := range cases {
		err := Check(ctx, draftV(t, worker, version.Seed1, c.verb, c.subject, c.payload, ctx.Tip))
		var oog *OutOfGrantError
		if !errors.As(err, &oog) || len(oog.Accepted) != 1 || oog.Accepted[0] != keyring.CapOperator {
			t.Fatalf("%s by a non-operator must refuse out of grant naming operator, got %v", name, err)
		}
		if err := Check(ctx, draftV(t, signer, version.Seed1, c.verb, c.subject, c.payload, ctx.Tip)); err != nil {
			t.Fatalf("%s by the root (implicit operator) must admit, got %v", name, err)
		}
	}

	// Checkpoints accept maintenance or operator; a plain enrolled key
	// holds neither.
	err := Check(ctx, draftV(t, worker, version.Seed1, "system.checkpoint", "system", `{"n": 1}`, ctx.Tip))
	var oog *OutOfGrantError
	if !errors.As(err, &oog) || len(oog.Accepted) != 2 {
		t.Fatalf("a checkpoint by a plain enrolled key must refuse naming both accepted capabilities, got %v", err)
	}

	// Ordinary verbs need active standing only.
	if err := Check(ctx, draftV(t, worker, version.Seed1, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)); err != nil {
		t.Fatalf("an ungoverned verb by an enrolled key must admit, got %v", err)
	}
}

// conformance: III.E — delegation works: a key granted operator via
// actor.granted exercises the operator verbs without being a genesis
// root, and maintenance checkpoints without operator authority.
func TestGrantDelegation(t *testing.T) {
	_, signer, worker, maintainer, step := grantFixture(t)
	ctx := step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapOperator+`"}`)
	if err := Check(ctx, draftV(t, worker, version.Seed1, keyring.VerbEnrolled, fpOf(t, fixtureKey(t, 4)), enrollBody(t, fixtureKey(t, 4), "agent", "fourth"), ctx.Tip)); err != nil {
		t.Fatalf("a granted operator must exercise actor verbs, got %v", err)
	}
	if err := Check(ctx, draftV(t, worker, version.Seed1, "system.halt.declared", "system", `{"reason": "x"}`, ctx.Tip)); err != nil {
		t.Fatalf("a granted operator must declare halt, got %v", err)
	}

	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, maintainer), `{"capability": "`+keyring.CapMaintenance+`"}`)
	// The payload is a real snapshot citation now: the checkpoint rule
	// refuses an arbitrary one (plans/os-8a5f14bb.md D4.5), and this
	// drill is about the GRANT, so it must not fail for shape.
	cp, perr := checkpoint.Payload(strings.Repeat("a", 64), ctx.Count)
	if perr != nil {
		t.Fatal(perr)
	}
	if err := Check(ctx, draftV(t, maintainer, version.Seed1, checkpoint.Verb, "system", string(cp), ctx.Tip)); err != nil {
		t.Fatalf("a maintenance grant must admit checkpoints, got %v", err)
	}
	err := Check(ctx, draftV(t, maintainer, version.Seed1, "system.halt.declared", "system", `{"reason": "x"}`, ctx.Tip))
	var oog *OutOfGrantError
	if !errors.As(err, &oog) {
		t.Fatalf("maintenance must not carry halt authority, got %v", err)
	}
}

// conformance: III.E — enrolled kind is an operator assertion: two
// actors differing only in kind produce the same admission outcome for
// every governed verb (the charter's language enforced by drill, not
// prose).
func TestKindParityDrill(t *testing.T) {
	_, signer, worker, maintainer, step := grantFixture(t)
	// worker is kind agent, maintainer is kind service; give both the
	// same capability so only kind differs.
	step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapMaintenance+`"}`)
	ctx := step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, maintainer), `{"capability": "`+keyring.CapMaintenance+`"}`)

	for _, c := range []struct{ verb, subject, payload string }{
		{"system.checkpoint", "system", `{"n": 1}`},
		{"system.halt.declared", "system", `{"reason": "x"}`},
		{ledger.UpgradeVerb, "system", `{"to": "seed/9"}`},
		{keyring.VerbGranted, fpOf(t, signer), `{"capability": "x"}`},
		{"message.sent", "c-0001", `{"n": 1}`},
	} {
		errA := Check(ctx, draftV(t, worker, version.Seed1, c.verb, c.subject, c.payload, ctx.Tip))
		errB := Check(ctx, draftV(t, maintainer, version.Seed1, c.verb, c.subject, c.payload, ctx.Tip))
		if (errA == nil) != (errB == nil) {
			t.Fatalf("%s outcome differs by kind: %v vs %v", c.verb, errA, errB)
		}
		if errA != nil {
			var a, b *OutOfGrantError
			if errors.As(errA, &a) != errors.As(errB, &b) || (a != nil && fmt.Sprint(a.Accepted) != fmt.Sprint(b.Accepted)) {
				t.Fatalf("%s refusal differs by kind: %v vs %v", c.verb, errA, errB)
			}
		}
	}
}
