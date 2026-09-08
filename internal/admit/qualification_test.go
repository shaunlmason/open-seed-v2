package admit

// The qualification drills (plans/os-8e53ffd9.md D2, D4, D8;
// spec/qualification.md; SEED-NEXT.md §II.5): at seed/2 a
// run.started declares the runtime tuple, the CLAIM HOLDER's claim
// grants cite a set of tuples, and a declaration equal to no member,
// per field, is out of grant. Before seed/2 the field is refused, so a
// chain that never upgraded keeps its judgment.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// baseTuple is the configuration the drills qualify; variants differ
// from it in exactly one named field.
var baseTuple = map[string]string{
	"principal": "acme", "harness": "local-worktree/v0", "model": "fable/5.1",
	"tool_policy": "default", "environment": "detached-git-worktree",
}

func tupleJSON(t *testing.T, override map[string]string) string {
	t.Helper()
	m := map[string]string{}
	for k, v := range baseTuple {
		m[k] = v
	}
	for k, v := range override {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func startBodyAt(fence, res, tup string) string {
	if tup == "" {
		return fmt.Sprintf(`{"fence": %q, "reservation": %q}`, fence, res)
	}
	return fmt.Sprintf(`{"fence": %q, "reservation": %q, "tuple": %s}`, fence, res, tup)
}

func grantBody(tup string) string {
	if tup == "" {
		return `{"capability": "claim"}`
	}
	return `{"capability": "claim", "tuple": ` + tup + `}`
}

// qualifiedFixture is runFixture carried to seed/2: the upgrade lands
// inside the open claim window, then the holder's reservation, so a
// run.started drafted at the tip is judged under tuple semantics. The
// holder's grant from runFixture is a seed/1 grant with no tuple: the
// bridge grant every existing chain's workers hold.
func qualifiedFixture(t *testing.T) (*Context, runKeys, stepFn, string, string) {
	t.Helper()
	ctx, k, step := runFixture(t)
	ctx = step(k.signer, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	if ctx.Active != version.Seed2 {
		t.Fatalf("the upgrade must move the active version: %s", ctx.Active)
	}
	fence := fenceOf(t, ctx, "c-1")
	ctx = step(k.holder, version.Seed2, "budget.reserve", "c-1", reserveBody("10", fence))
	return ctx, k, step, fence, fmt.Sprintf("%d", ctx.Count-1)
}

type ed25519PrivateKey = ed25519.PrivateKey

type stepFn = func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context

// conformance: D8 — the tuple is required at seed/2 and refused before
// it; malformed declarations refuse by shape; and a holder none of
// whose claim grants cite a tuple is unqualified, the bridge, and
// admits any declared configuration.
func TestTupleIsRequiredAtSeed2AndRefusedBefore(t *testing.T) {
	ctx, k, step := runFixture(t)
	fence := fenceOf(t, ctx, "c-1")
	ctx = step(k.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("10", fence))
	res := fmt.Sprintf("%d", ctx.Count-1)
	err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)), ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "tuple semantics activate at "+version.Seed2) {
		t.Fatalf("a seed/1 start carrying a tuple refuses by version, so a chain that never upgraded keeps its judgment: %v", err)
	}
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed1, "run.started", "c-1", startBodyAt(fence, res, ""), ctx.Tip)); err != nil {
		t.Fatalf("a seed/1 start without a tuple admits exactly as before: %v", err)
	}

	ctx, k, _, fence, res = qualifiedFixture(t)
	err = Check(ctx, draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, ""), ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "declares no runtime tuple") {
		t.Fatalf("at seed/2 the tuple is required: a run with no configuration is a run nothing can qualify: %v", err)
	}
	for name, bad := range map[string]string{
		"null":          "null",
		"not an object": `"acme"`,
		"missing field": `{"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p"}`,
		"empty field":   tupleJSON(t, map[string]string{"model": ""}),
		"unknown field": `{"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e", "extra": "x"}`,
		"non-string":    `{"principal": 1, "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e"}`,
	} {
		err := Check(ctx, draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, bad), ctx.Tip))
		var re *RunError
		if !errors.As(err, &re) {
			t.Errorf("%s: a malformed declaration refuses by shape at the run rule, got %v", name, err)
		}
	}
	// The bridge: the holder's only claim grant cites no tuple, so the
	// set is empty and any configuration admits.
	for _, tup := range []string{tupleJSON(t, nil), tupleJSON(t, map[string]string{"model": "other/9"})} {
		if err := Check(ctx, draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tup), ctx.Tip)); err != nil {
			t.Fatalf("an unqualified holder admits any declared configuration (the bridge): %v", err)
		}
	}
}

// conformance: D4 — drift is a per-field inequality against the CLAIM
// HOLDER's cited set, refused out_of_grant naming the holder, the
// field and both values; D2 — the set rule's three consequences.
func TestDriftIsPerFieldAgainstTheHolder(t *testing.T) {
	ctx, k, step, fence, res := qualifiedFixture(t)
	holder := fpOf(t, k.holder)
	draft := func(tup string) error {
		t.Helper()
		return Check(ctx, draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tup), ctx.Tip))
	}
	drift := func(tup string) *Drift {
		t.Helper()
		err := draft(tup)
		var oog *OutOfGrantError
		if !errors.As(err, &oog) || oog.Drift == nil {
			t.Fatalf("drift refuses out_of_grant carrying the Drift detail, got %v", err)
		}
		if oog.Drift.Holder != holder {
			t.Fatalf("drift names the holder %s, not the signer: %+v", holder, oog.Drift)
		}
		if msg := err.Error(); !strings.Contains(msg, holder) || !strings.Contains(msg, oog.Drift.Field) || !strings.Contains(msg, oog.Drift.Have) {
			t.Fatalf("the refusal says which field moved and to what: %s", msg)
		}
		return oog.Drift
	}

	// Set rule (i): the qualified grant lands AFTER the bridge grant
	// runFixture enrolled the holder with, and from here the bridge
	// is retired with no migration verb: the set is what is read.
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, holder, grantBody(tupleJSON(t, nil)))
	// A mismatch planted on the SIGNER's own claim grant must not
	// matter: the supervisor signs, and the holder's window spends.
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, fpOf(t, k.supervisor),
		grantBody(tupleJSON(t, map[string]string{"principal": "someone-else"})))

	for _, field := range tuple.Fields() {
		d := drift(tupleJSON(t, map[string]string{field: "drifted"}))
		if d.Field != field || d.Have != "drifted" {
			t.Fatalf("%s: the refusal names the field that moved and the declared value: %+v", field, d)
		}
		if len(d.Cited) != 1 || !strings.Contains(d.Cited[0], baseTuple[field]) {
			t.Fatalf("%s: the refusal cites the holder's set: %+v", field, d.Cited)
		}
	}
	if err := draft(tupleJSON(t, nil)); err != nil {
		t.Fatalf("the matching configuration admits, the signer's mismatched grant notwithstanding: %v", err)
	}

	// Set rule (ii): a second qualified grant ADDS a configuration
	// rather than replacing the first; a third refuses citing both.
	second := map[string]string{"model": "fable/5.2"}
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, holder, grantBody(tupleJSON(t, second)))
	for _, tup := range []string{tupleJSON(t, nil), tupleJSON(t, second)} {
		if err := draft(tup); err != nil {
			t.Fatalf("either cited configuration admits: %v", err)
		}
	}
	if d := drift(tupleJSON(t, map[string]string{"model": "fable/6.0"})); len(d.Cited) != 2 || d.Field != "model" {
		t.Fatalf("a third configuration refuses citing both members: %+v", d)
	}

	// Set rule (iii): a tuple-less grant appended after a qualified
	// one changes nothing.
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, holder, grantBody(""))
	if d := drift(tupleJSON(t, map[string]string{"model": "fable/6.0"})); len(d.Cited) != 2 {
		t.Fatalf("a later bridge grant does not reopen the bridge: %+v", d)
	}
	if err := draft(tupleJSON(t, nil)); err != nil {
		t.Fatalf("the cited configuration still admits: %v", err)
	}

	// The admitted start folds with its declaration, and passes the
	// position-accurate boundary Provision replays.
	ctx = step(k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)))
	s, _ := ctx.Lifecycle.State("c-1")
	st := s.RunStarts[len(s.RunStarts)-1]
	if st.Tuple == nil || st.Tuple.Model != baseTuple["model"] || !RunStartValid(ctx.Records, ctx.Table, "c-1", st) {
		t.Fatalf("the admitted start carries its declared tuple and is valid: %+v", st)
	}
}

// conformance: D4 — the reverse of the signer row above: the SIGNER's
// grant matches and the HOLDER's does not, and the run refuses naming
// the holder, whoever signs.
func TestDriftReadsTheHolderNotTheSigner(t *testing.T) {
	ctx, k, step, fence, res := qualifiedFixture(t)
	holder := fpOf(t, k.holder)
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, holder,
		grantBody(tupleJSON(t, map[string]string{"principal": "not-acme"})))
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, fpOf(t, k.supervisor), grantBody(tupleJSON(t, nil)))
	// The supervisor, whose own claim grant cites exactly the declared
	// configuration; and the root, which holds no claim grant at all.
	for name, key := range map[string]ed25519PrivateKey{"supervisor": k.supervisor, "root": k.signer} {
		err := Check(ctx, draftV(t, key, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)), ctx.Tip))
		var oog *OutOfGrantError
		if !errors.As(err, &oog) || oog.Drift == nil || oog.Drift.Holder != holder || oog.Drift.Field != "principal" || oog.Drift.Have != "acme" {
			t.Fatalf("%s signing: the check reads the holder's set, so a holder mismatch refuses whatever the signer holds: %v", name, err)
		}
	}
}

// conformance: review finding on the task PR — fold presence is never
// proof of admission, and that includes the declaration. RunStartValid
// re-judges a raw-pushed seed/2 start's tuple under the record's own
// version and against the holder's cited set at its prefix, so a start
// with no tuple, a malformed one, or a drifting one provisions nothing
// (Provision reads this same derivation), launders no settle, and
// blocks no legitimate start; the matching one is valid.
func TestRunStartValidReappliesTupleAdmission(t *testing.T) {
	ctx, k, step, fence, res := qualifiedFixture(t)
	holder := fpOf(t, k.holder)
	ctx = step(k.signer, version.Seed2, keyring.VerbGranted, holder, grantBody(tupleJSON(t, nil)))
	legit := draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)), ctx.Tip)
	if err := Check(ctx, legit); err != nil {
		t.Fatalf("the matching start admits before any raw push: %v", err)
	}
	lastStart := func(ctx *Context) (transition.RunStartFact, int) {
		s, _ := ctx.Lifecycle.State("c-1")
		if len(s.RunStarts) == 0 {
			return transition.RunStartFact{}, 0
		}
		return s.RunStarts[len(s.RunStarts)-1], len(s.RunStarts)
	}
	for name, body := range map[string]string{
		"no tuple": startBodyAt(fence, res, ""),
		"drifting": startBodyAt(fence, res, tupleJSON(t, map[string]string{"model": "other/9"})),
	} {
		// The raw seam: signed by the supervisor, judged by nothing.
		ctx = step(k.supervisor, version.Seed2, "run.started", "c-1", body)
		st, n := lastStart(ctx)
		if n == 0 || st.Pos != ctx.Count-1 {
			t.Fatalf("%s: the raw start folds as a fact: %+v", name, st)
		}
		if RunStartValid(ctx.Records, ctx.Table, "c-1", st) {
			t.Fatalf("%s: a raw start whose declaration the boundary would refuse is not a valid start", name)
		}
		legit = draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)), ctx.Tip)
		if err := Check(ctx, legit); err != nil {
			t.Fatalf("%s: the raw start blocks no legitimate start: %v", name, err)
		}
	}
	// A malformed declaration is no fact at all, and an anomaly.
	before, nBefore := lastStart(ctx)
	ctx = step(k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, `{"principal": "x"}`))
	if after, n := lastStart(ctx); n != nBefore || after.Pos != before.Pos {
		t.Fatalf("a malformed declaration folds to nothing: %+v", after)
	}
	if s, _ := ctx.Lifecycle.State("c-1"); s.Anomalies == 0 {
		t.Fatal("a malformed declaration counts an anomaly")
	}
	// The matching start, raw-pushed, is exactly as valid as an admitted
	// one: the derivation reads the record, not the path it took.
	ctx = step(k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)))
	st, _ := lastStart(ctx)
	if !RunStartValid(ctx.Records, ctx.Table, "c-1", st) || st.Tuple == nil {
		t.Fatalf("the matching start is valid and carries its declaration: %+v", st)
	}
	if err := Check(ctx, draftV(t, k.supervisor, version.Seed2, "run.started", "c-1", startBodyAt(fence, res, tupleJSON(t, nil)), ctx.Tip)); err == nil || !strings.Contains(err.Error(), "one run per claim window") {
		t.Fatalf("and it is the one run the window carries: %v", err)
	}

	// A seed/1 chain: a raw start carrying a tuple is judged under the
	// record's own version, and is not a valid start there either.
	ctx1, k1, step1 := runFixture(t)
	fence1 := fenceOf(t, ctx1, "c-1")
	ctx1 = step1(k1.holder, version.Seed1, "budget.reserve", "c-1", reserveBody("10", fence1))
	res1 := fmt.Sprintf("%d", ctx1.Count-1)
	ctx1 = step1(k1.supervisor, version.Seed1, "run.started", "c-1", startBodyAt(fence1, res1, tupleJSON(t, nil)))
	s1, _ := ctx1.Lifecycle.State("c-1")
	if len(s1.RunStarts) != 1 || RunStartValid(ctx1.Records, ctx1.Table, "c-1", s1.RunStarts[0]) {
		t.Fatalf("a seed/1 start carrying a tuple is not a valid start: %+v", s1.RunStarts)
	}
}
