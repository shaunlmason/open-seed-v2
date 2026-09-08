package admit

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

func fixtureKey(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

// seededStore opens a store holding a real genesis and returns it with
// the genesis resolver and the root signer. The directory is recorded in
// dirs for tests that corrupt the layout on disk.
var dirs = map[*ledger.Store]string{}

func seededStore(t *testing.T) (*ledger.Store, ledger.Resolver, ed25519.PrivateKey) {
	t.Helper()
	signer := fixtureKey(t, 1)
	dir := t.TempDir()
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	dirs[store] = dir
	rec, err := genesis.Build(signer, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(rec.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	return store, resolve, signer
}

func appendSigned(t *testing.T, store *ledger.Store, resolve ledger.Resolver, priv ed25519.PrivateKey, verb, subject, payload string) {
	t.Helper()
	tip, _, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	rec := draft(t, priv, verb, subject, payload, tip)
	if _, err := store.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
}

func draft(t *testing.T, priv ed25519.PrivateKey, verb, subject, payload, prev string) *event.Record {
	t.Helper()
	return draftV(t, priv, "seed/0", verb, subject, payload, prev)
}

func draftV(t *testing.T, priv ed25519.PrivateKey, v, verb, subject, payload, prev string) *event.Record {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: v, TS: "2026-09-01T01:00:00Z", Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: prev,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// conformance: III.B (Phase 2 subset) — each rule refuses its hostile
// draft by name, the refusal unwraps to its typed error, and a clean
// draft passes the whole set.
func TestEachRuleRefusesItsCase(t *testing.T) {
	store, _, signer := seededStore(t)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}

	clean := draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	if err := Check(ctx, clean); err != nil {
		t.Fatalf("clean draft must pass the whole set, got %v", err)
	}

	stranger := fixtureKey(t, 9)
	forged := draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	forged.Sig = strings.Repeat("a", 128)
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`

	cases := []struct {
		name string
		rec  *event.Record
		rule string
		as   func(error) bool
	}{
		{"unresolvable actor", draft(t, stranger, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip),
			"actor", func(err error) bool { return errors.Is(err, ledger.ErrUnknownActor) }},
		{"forged signature", forged, "actor", func(err error) bool { return err != nil }},
		{"wrong version", draftV(t, signer, "seed/10", "message.sent", "c-0001", `{"n": 1}`, ctx.Tip),
			"version", func(err error) bool {
				var f *ledger.Failure
				return errors.As(err, &f) && f.Reason == ledger.ReasonVersionMismatch
			}},
		{"classification-hostile payload", draft(t, signer, "message.sent", "c-0001", hostile, ctx.Tip),
			"classification", func(err error) bool {
				var c *ClassificationError
				return errors.As(err, &c) && len(c.Violations) > 0
			}},
		{"malformed lift", draft(t, signer, halt.LiftVerb, "system", `{"note": "x"}`, ctx.Tip),
			"shape", func(err error) bool { return err != nil }},
		{"upgrade missing to", draft(t, signer, ledger.UpgradeVerb, "system", `{"note": "missing to"}`, ctx.Tip),
			"shape", func(err error) bool { return strings.Contains(err.Error(), "'to'") }},
		{"upgrade empty to", draft(t, signer, ledger.UpgradeVerb, "system", `{"to": ""}`, ctx.Tip),
			"shape", func(err error) bool { return strings.Contains(err.Error(), "'to'") }},
		{"upgrade off system", draft(t, signer, ledger.UpgradeVerb, "c-0001", `{"to": "seed/1"}`, ctx.Tip),
			"shape", func(err error) bool { return strings.Contains(err.Error(), "not system") }},
	}
	for _, tc := range cases {
		err := Check(ctx, tc.rec)
		var ref *Refusal
		if !errors.As(err, &ref) || ref.Rule != tc.rule {
			t.Errorf("%s: want refusal by rule %q, got %v", tc.name, tc.rule, err)
			continue
		}
		if !tc.as(err) {
			t.Errorf("%s: refusal must unwrap to its typed error, got %v", tc.name, err)
		}
	}
}

// conformance: III.A halt item — under halt everything but the lift
// refuses as halted, and that refusal dominates shape trouble.
func TestHaltRulesUnderHalt(t *testing.T) {
	store, resolve, signer := seededStore(t)
	appendSigned(t, store, resolve, signer, halt.DeclareVerb, "system", `{"reason": "drill"}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.Halt.Halted || ctx.Halt.Reason != "drill" {
		t.Fatalf("context must project the halt, got %+v", ctx.Halt)
	}

	ordinary := draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	err = Check(ctx, ordinary)
	var herr *halt.HaltedError
	if !errors.As(err, &herr) || herr.Reason != "drill" {
		t.Fatalf("ordinary verb under halt must refuse as halted with the reason, got %v", err)
	}

	malformedDeclare := draft(t, signer, halt.DeclareVerb, "system", `{}`, ctx.Tip)
	err = Check(ctx, malformedDeclare)
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Rule != "halted" {
		t.Fatalf("halted refusal must dominate shape for forbidden drafts, got %v", err)
	}

	lift := draft(t, signer, halt.LiftVerb, "system", `{}`, ctx.Tip)
	if err := Check(ctx, lift); err != nil {
		t.Fatalf("the lift must pass under halt, got %v", err)
	}
}

// conformance: spec/protocol.md — admission follows the active
// version, and an active version outside the supported set refuses
// before anything is written.
func TestVersionDisciplineFollowsUpgrade(t *testing.T) {
	store, resolve, signer := seededStore(t)
	appendSigned(t, store, resolve, signer, ledger.UpgradeVerb, "system", `{"to": "seed/10"}`)

	// Default supported set: the upgraded chain verifies (the trailing
	// upgrade is history) but its tip is not appendable by this build.
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	next := draftV(t, signer, "seed/10", "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	err = Check(ctx, next)
	var f *ledger.Failure
	if !errors.As(err, &f) || f.Reason != ledger.ReasonVersionUnsupported {
		t.Fatalf("unsupported active version must refuse, got %v", err)
	}

	// A build supporting the new version admits it and refuses the old.
	ctx, err = ContextAt(store, WithSupportedVersions("seed/0", "seed/10"))
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Active != "seed/10" {
		t.Fatalf("active version must follow the upgrade, got %q", ctx.Active)
	}
	if err := Check(ctx, next); err != nil {
		t.Fatalf("the new version must pass under the upgraded set, got %v", err)
	}
	stale := draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	err = Check(ctx, stale)
	if !errors.As(err, &f) || f.Reason != ledger.ReasonVersionMismatch {
		t.Fatalf("the old version must refuse under the upgraded set, got %v", err)
	}
}

// The added-not-reworked seam: a later-phase rule appends to the set and
// runs after the defaults, with no edit to any existing rule.
func TestAppendedRuleSlotsIn(t *testing.T) {
	store, _, signer := seededStore(t)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	phase3 := errors.New("capability rule says no")
	rules := append(Default(), Rule{Name: "capability", Check: func(*Context, *event.Record) error {
		return phase3
	}})
	clean := draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	err = Run(ctx, clean, rules)
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Rule != "capability" || !errors.Is(err, phase3) {
		t.Fatalf("the appended rule must be the one refusing, got %v", err)
	}
	stranger := fixtureKey(t, 9)
	bad := draft(t, stranger, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip)
	if err := Run(ctx, bad, rules); !errors.Is(err, ledger.ErrUnknownActor) {
		t.Fatalf("default rules must still refuse first, got %v", err)
	}
}

// Admission never builds over a chain that does not verify.
func TestContextRefusesInvalidChain(t *testing.T) {
	store, resolve, signer := seededStore(t)
	appendSigned(t, store, resolve, signer, "message.sent", "c-0001", `{"n": 1}`)
	dir := dirs[store]
	segs, err := filepath.Glob(filepath.Join(dir, "segments", "*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("no segments under %s", dir)
	}
	b, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	corrupted := strings.Replace(string(b), `"n":1`, `"n":9`, 1)
	if corrupted == string(b) {
		t.Fatal("corruption did not apply")
	}
	if err := os.WriteFile(segs[0], []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ContextAt(store); err == nil {
		t.Fatal("a non-verifying chain must yield no context")
	}
}

// The Validate adapter is the gitref seam: current state in, refusal out.
func TestValidateAdapter(t *testing.T) {
	store, resolve, signer := seededStore(t)
	validate := Validate()
	tip, _, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(store, draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, tip)); err != nil {
		t.Fatalf("clean draft must pass through the adapter, got %v", err)
	}
	appendSigned(t, store, resolve, signer, halt.DeclareVerb, "system", `{"reason": "drill"}`)
	tip, _, err = store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	err = validate(store, draft(t, signer, "message.sent", "c-0002", `{"n": 2}`, tip))
	var herr *halt.HaltedError
	if !errors.As(err, &herr) {
		t.Fatalf("the adapter must re-read current state (now halted), got %v", err)
	}
}

// Error surfaces and refusal rendering, pinned so the envelope layer can
// rely on them.
func TestErrorSurfaces(t *testing.T) {
	store, resolve, signer := seededStore(t)
	appendSigned(t, store, resolve, signer, halt.DeclareVerb, "system", `{"reason": "drill"}`)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	err = Check(ctx, draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, ctx.Tip))
	if msg := err.Error(); !strings.Contains(msg, "refused by rule halted") || !strings.Contains(msg, "drill") {
		t.Fatalf("refusal message must name rule and cause, got %q", msg)
	}

	cerr := &ClassificationError{}
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
	if err := Check(ctxUnhalted(t), draftAt(t, signer, hostile)); !errors.As(err, &cerr) {
		t.Fatalf("want classification error, got %v", err)
	}
	if msg := cerr.Error(); !strings.Contains(msg, "/transcript") || !strings.Contains(msg, "data classification") {
		t.Fatalf("classification message must carry pointers, got %q", msg)
	}

	// An empty store yields no context: nothing to admit onto.
	empty, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ContextAt(empty); err == nil {
		t.Fatal("an empty store must yield no admission context")
	}
	if err := Validate()(empty, draft(t, signer, "message.sent", "c-0001", `{"n": 1}`, "")); err == nil {
		t.Fatal("the adapter must surface context refusals")
	}
}

func ctxUnhalted(t *testing.T) *Context {
	t.Helper()
	store, _, _ := seededStore(t)
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func draftAt(t *testing.T, priv ed25519.PrivateKey, payload string) *event.Record {
	t.Helper()
	return draft(t, priv, "message.sent", "c-0001", payload, "")
}
