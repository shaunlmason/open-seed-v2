package admit

// Rubric verdicts (plans/os-2e34f66a.md D2 to D5): the scorecard's
// record half at admission, the derivation, the human verdict and the
// deferral, the boundary's reapplication along the merge chain, the
// calibration qualification and the set rule at render.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const scoreDigest = "1111111111111111111111111111111111111111111111111111111111111111"

// scored is a scorecard citation with the given items.
func scored(items ...string) string {
	return `{"digest": "` + scoreDigest + `", "items": [` + strings.Join(items, ", ") + `]}`
}

func item(id, score, uncertainty string) string {
	return fmt.Sprintf(`{"id": %q, "score": %q, "uncertainty": %q}`, id, score, uncertainty)
}

// scoredBody is levelBody with a scorecard.
func scoredBody(verdict string, subPos int, level, tup, scorecard string) string {
	body := fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": %q`, verdict, testReceipt, subPos, level)
	if tup != "" {
		body += `, "tuple": ` + tup
	}
	if scorecard != "" {
		body += `, "scorecard": ` + scorecard
	}
	return body + `}`
}

func deferBody(subPos int, items ...string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = fmt.Sprintf("%q", it)
	}
	return fmt.Sprintf(`{"receipt": %q, "scorecard": %q, "submission": "%d", "items": [%s]}`, testReceipt, scoreDigest, subPos, strings.Join(quoted, ", "))
}

// tierDeferral is the whole-verdict deferral a human-review tier
// admits: the receipt and the submission, no scorecard, no items.
func tierDeferral(subPos int) string {
	return fmt.Sprintf(`{"receipt": %q, "submission": "%d"}`, testReceipt, subPos)
}

func codeOf(t *testing.T, name string, err error, code string) {
	t.Helper()
	var ve *VerdictError
	if !errors.As(err, &ve) || ve.Code != code {
		t.Fatalf("%s: refuses with code %q, got %v", name, code, err)
	}
}

// conformance: AC3, AC5 — the derivation over the payload's items at
// admission: all pass at low admits pass; a fail item refuses pass as
// rubric_red naming it and leaves fail; a high item refuses both as
// human_verdict; a verdict that contradicts its own all-pass
// scorecard refuses; the shape refuses naming the part; before seed/4
// the field refuses by version.
func TestScorecardDerivationAtAdmission(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	base := tupleJSON(t, nil)
	sub := st.drive("c-1", filedTier("standard"), specBody, base)
	pass := scored(item("tone", "pass", "low"), item("taste", "pass", "low"))
	if err := st.render(t, "c-1", scoredBody("pass", sub, "L1", "", pass)); err != nil {
		t.Fatalf("every item pass at low renders pass: %v", err)
	}
	red := scored(item("tone", "pass", "low"), item("taste", "fail", "low"))
	err := st.render(t, "c-1", scoredBody("pass", sub, "L1", "", red))
	codeOf(t, "a fail item under pass", err, transition.CodeRubricRed)
	if !strings.Contains(err.Error(), `"taste"`) {
		t.Fatalf("the refusal names the item: %v", err)
	}
	if err := st.render(t, "c-1", scoredBody("fail", sub, "L1", "", red)); err != nil {
		t.Fatalf("fail stays renderable over a failing item: %v", err)
	}
	high := scored(item("tone", "pass", "low"), item("taste", "pass", "high"))
	for _, v := range []string{"pass", "fail"} {
		codeOf(t, v+" over a high item", st.render(t, "c-1", scoredBody(v, sub, "L1", "", high)), transition.CodeHumanVerdict)
	}
	if err := st.render(t, "c-1", scoredBody("fail", sub, "L1", "", pass)); err == nil || !strings.Contains(err.Error(), "scorecard says pass") {
		t.Fatalf("fail over an all-pass scorecard contradicts its derivation: %v", err)
	}
	for _, row := range []struct{ name, scorecard, names string }{
		{"a smuggled key", `{"digest": "` + scoreDigest + `", "items": [], "verdict": "pass"}`, "strict object"},
		{"a malformed digest", `{"digest": "abc", "items": []}`, "sha256"},
		{"an empty id", scored(item("", "pass", "low")), "non-empty"},
		{"a duplicate id", scored(item("tone", "pass", "low"), item("tone", "pass", "low")), "once"},
		{"a score outside the vocabulary", scored(item("tone", "ok", "low")), "pass, fail"},
		{"an uncertainty outside the vocabulary", scored(item("tone", "pass", "medium")), "low, high"},
	} {
		err := st.render(t, "c-1", scoredBody("pass", sub, "L1", "", row.scorecard))
		var ve *VerdictError
		if !errors.As(err, &ve) || ve.Code != "" || !strings.Contains(err.Error(), row.names) {
			t.Fatalf("%s refuses naming the part (%s): %v", row.name, row.names, err)
		}
	}
	// A scorecard with no items permits pass: the rubric's absence
	// is the spec's, and the render surface refuses a scorecard on a
	// spec without a rubric before anything is drafted.
	if err := st.render(t, "c-1", scoredBody("pass", sub, "L1", "", scored())); err != nil {
		t.Fatalf("no items, nothing refuted: %v", err)
	}
	// Before seed/4 the field refuses naming the version.
	old := levelFixture(t, version.Seed3)
	subOld := old.drive("c-1", filedTier("standard"), specBody, base)
	if err := old.render(t, "c-1", scoredBody("pass", subOld, "L1", "", pass)); err == nil || !strings.Contains(err.Error(), version.Seed4) {
		t.Fatalf("a scorecard at seed/3 refuses naming seed/4: %v", err)
	}
	if err := old.render(t, "c-1", levelBody("pass", subOld, "L1", "")); err != nil {
		t.Fatalf("a seed/3 verdict without a scorecard keeps its judgment: %v", err)
	}
}

// conformance: AC3, AC4 — the human verdict: on a human-review tier a
// render from a verdict-only key refuses human_verdict whatever the
// scorecard says and one from a key with operator standing admits;
// the deferral's rows; after a deferral only a human renders, and the
// deferring key refuses.
func TestHumanVerdictAndTheDeferral(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	k := st.keys
	base := tupleJSON(t, nil)
	other := tupleJSON(t, map[string]string{"model": "other/1"})
	pass := scored(item("tone", "pass", "low"))
	// The tier's column, consumed.
	subC := st.drive("c-crit", filedTier("critical"), specBody, base)
	err := st.render(t, "c-crit", scoredBody("pass", subC, "L2", other, pass))
	codeOf(t, "a verdict-only key on a human-review tier", err, transition.CodeHumanVerdict)
	if !strings.Contains(err.Error(), "critical") || !strings.Contains(err.Error(), "operator standing") {
		t.Fatalf("the refusal names the tier and the standing: %v", err)
	}
	if err := st.renderAs(t, k.human, "c-crit", scoredBody("pass", subC, "L2", other, pass)); err != nil {
		t.Fatalf("a key with a verdict grant and operator standing renders on the human-review tier: %v", err)
	}
	// A root with an explicit verdict grant is a human too.
	st.step(k.signer, version.Seed4, keyring.VerbGranted, fpOf(t, k.signer), `{"capability": "`+keyring.CapVerdict+`"}`)
	if err := st.renderAs(t, k.signer, "c-crit", scoredBody("pass", subC, "L2", other, pass)); err != nil {
		t.Fatalf("a governance root holding an explicit verdict grant renders: %v", err)
	}

	// The deferral on a standard-tier submission.
	sub := st.drive("c-1", filedTier("standard"), specBody, base)
	deferAs := func(key ed25519.PrivateKey, subject, body string) error {
		return Check(st.ctx, draftV(t, key, version.Seed4, transition.VerdictDeferredVerb, subject, body, st.ctx.Tip))
	}
	if err := deferAs(k.verifier, "c-1", deferBody(sub, "tone")); err != nil {
		t.Fatalf("the verifier defers the items it could not judge: %v", err)
	}
	var oog *OutOfGrantError
	if err := deferAs(k.supervisor, "c-1", deferBody(sub, "tone")); !errors.As(err, &oog) {
		t.Fatalf("a key holding no verdict grant cannot defer: %v", err)
	}
	if err := deferAs(k.holder, "c-1", deferBody(sub, "tone")); !errors.As(err, &oog) {
		t.Fatalf("an implementing key holds no verdict grant and cannot defer: %v", err)
	}
	// A verdict-granted key that implemented the contract is refused
	// by L1 exactly as at the verdict.
	st.step(k.signer, version.Seed4, keyring.VerbGranted, fpOf(t, k.holder), `{"capability": "`+keyring.CapVerdict+`"}`)
	var nie *NotIndependentError
	if err := deferAs(k.holder, "c-1", deferBody(sub, "tone")); !errors.As(err, &nie) {
		t.Fatalf("an implementing key cannot defer: %v", err)
	}
	var inc *transition.IncompleteError
	if err := deferAs(k.verifier, "c-1", fmt.Sprintf(`{"scorecard": %q, "items": ["tone"]}`, scoreDigest)); !errors.As(err, &inc) || strings.Join(inc.Missing, ",") != "receipt,submission" {
		t.Fatalf("a deferral without its receipt and submission is incomplete naming both: %v", err)
	}
	for _, row := range []struct{ name, body, names string }{
		{"a smuggled key", fmt.Sprintf(`{"receipt": %q, "scorecard": %q, "submission": "%d", "items": ["tone"], "verdict": "pass"}`, testReceipt, scoreDigest, sub), "strict object"},
		{"a malformed receipt", fmt.Sprintf(`{"receipt": "x", "submission": "%d"}`, sub), "sha256"},
		{"a malformed scorecard digest", fmt.Sprintf(`{"receipt": %q, "scorecard": "x", "submission": "%d", "items": ["tone"]}`, testReceipt, sub), "sha256"},
		{"items with no scorecard", fmt.Sprintf(`{"receipt": %q, "submission": "%d", "items": ["tone"]}`, testReceipt, sub), "cites no scorecard"},
		{"nothing deferred on a standard tier", tierDeferral(sub), "nothing is deferred"},
		{"another submission", deferBody(sub+1, "tone"), "bound submission"},
		{"a duplicate item", deferBody(sub, "tone", "tone"), "once"},
	} {
		if err := deferAs(k.verifier, "c-1", row.body); err == nil || !strings.Contains(err.Error(), row.names) {
			t.Fatalf("%s refuses naming the part: %v", row.name, err)
		}
	}
	// On the human-review tier the whole verdict defers: no rubric,
	// no items, the receipt and the submission alone.
	if err := deferAs(k.verifier, "c-crit", tierDeferral(subC)); err != nil {
		t.Fatalf("a critical contract's verdict defers whole: %v", err)
	}
	st.step(k.human, version.Seed4, transition.VerdictRenderedVerb, "c-crit", scoredBody("pass", subC, "L2", other, pass))
	if err := deferAs(k.verifier, "c-crit", tierDeferral(subC)); err == nil || !strings.Contains(err.Error(), "judged already") {
		t.Fatalf("a judged submission cannot be deferred: %v", err)
	}
	deferPos := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictDeferredVerb, "c-1", deferBody(sub, "tone"))
	s, _ := st.ctx.Lifecycle.State("c-1")
	if s.State != "review" || s.Deferred == nil || s.Deferred.Pos != deferPos || s.Deferred.Items[0] != "tone" || s.Deferred.Scorecard != scoreDigest || s.Deferred.Receipt != testReceipt {
		t.Fatalf("the deferral changes no state and folds on the window: %s %+v", s.State, s.Deferred)
	}
	if err := deferAs(k.verifier, "c-1", deferBody(sub, "taste")); err == nil || !strings.Contains(err.Error(), "already stands") {
		t.Fatalf("a second deferral refuses: %v", err)
	}
	// After the deferral the verdict is a human's.
	codeOf(t, "the deferring key rendering after its deferral", st.render(t, "c-1", scoredBody("pass", sub, "L1", "", pass)), transition.CodeHumanVerdict)
	if err := st.renderAs(t, k.human, "c-1", scoredBody("pass", sub, "L1", "", pass)); err != nil {
		t.Fatalf("the human renders over the deferral: %v", err)
	}
	// The human's own scorecard is judged like any: a high item stays
	// undecided even for a human.
	codeOf(t, "a human deferring again by scoring high", st.renderAs(t, k.human, "c-1", scoredBody("pass", sub, "L1", "", scored(item("tone", "pass", "high")))), transition.CodeHumanVerdict)
	// Before seed/4 the verb refuses naming the version.
	old := levelFixture(t, version.Seed3)
	subOld := old.drive("c-1", filedTier("standard"), specBody, base)
	if err := Check(old.ctx, draftV(t, old.keys.verifier, version.Seed3, transition.VerdictDeferredVerb, "c-1", deferBody(subOld, "tone"), old.ctx.Tip)); err == nil || !strings.Contains(err.Error(), version.Seed4) {
		t.Fatalf("a deferral at seed/3 refuses naming seed/4: %v", err)
	}
}

// conformance: AC4, AC5 — the boundary reapplies the derivation and
// the standing: a raw pass whose items carry a fail, a raw verdict
// over a high item, a raw fail over all-pass items, and a raw machine
// pass after a deferral authenticate nothing for merge.requested; the
// lockout ignores a raw fail whose items are all pass; a human's raw
// pass over the deferral is cited.
func TestScoreBoundaryAlongTheMergeChain(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	k := st.keys
	base := tupleJSON(t, nil)
	sub := st.drive("c-1", filedTier("standard"), specBody, base)
	request := func(pos int) error {
		return Check(st.ctx, draftV(t, k.holder, version.Seed4, "merge.requested", "c-1", fmt.Sprintf(`{"verdict": "%d"}`, pos), st.ctx.Tip))
	}
	raw := func(key ed25519.PrivateKey, body string) int {
		pos := st.ctx.Count
		st.step(key, version.Seed4, transition.VerdictRenderedVerb, "c-1", body)
		return pos
	}
	redPass := raw(k.verifier, scoredBody("pass", sub, "L1", "", scored(item("tone", "fail", "low"))))
	chainRefusal(t, "a raw pass over a failing item", request(redPass), "fails its own derivation")
	highPass := raw(k.verifier, scoredBody("pass", sub, "L1", "", scored(item("tone", "pass", "high"))))
	chainRefusal(t, "a raw pass over a high item", request(highPass), "high uncertainty")
	// A raw fail whose items all pass locks nothing: a proper pass
	// admits after it.
	raw(k.verifier, scoredBody("fail", sub, "L1", "", scored(item("tone", "pass", "low"))))
	if err := st.render(t, "c-1", scoredBody("pass", sub, "L1", "", scored(item("tone", "pass", "low")))); err != nil {
		t.Fatalf("a self-refuting raw fail locks pass out of nothing: %v", err)
	}
	// After a deferral, a raw machine pass authenticates nothing and a
	// human's does.
	st.step(k.verifier, version.Seed4, transition.VerdictDeferredVerb, "c-1", deferBody(sub, "tone"))
	machine := raw(k.verifier, scoredBody("pass", sub, "L1", "", scored(item("tone", "pass", "low"))))
	chainRefusal(t, "a raw machine pass after a deferral", request(machine), "operator standing")
	human := raw(k.human, scoredBody("pass", sub, "L1", "", scored(item("tone", "pass", "low"))))
	if err := request(human); err != nil {
		t.Fatalf("the request citing the human's pass admits: %v", err)
	}
}

// conformance: AC6 (the admission half) — the calibration
// qualification: actor.qualified for capability verdict cites the
// eval's authenticated verdict, names the verifier that rendered and
// the tuple it declared; the set rule then applies to the verifier's
// declared tuple at render on an ordinary contract, never on an eval;
// before seed/4 the capability refuses by version.
func TestCalibrationQualifiesTheVerifierAndBindsItsRenders(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	k := st.keys
	base := tupleJSON(t, nil)
	verifierTuple := tupleJSON(t, map[string]string{"model": "other/1"})
	drifted := tupleJSON(t, map[string]string{"model": "other/2"})
	subE := st.drive("e-1", calibFiled, evalSpecFor("calib"), base)
	verdictPos := st.ctx.Count
	// An executable gated spec supports L3 whatever the declaration.
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "e-1", scoredBody("pass", subE, "L3", verifierTuple, scored(item("tone", "pass", "low"))))
	qualify := func(subject, capability, tup string, pos int) error {
		body := fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-1", "verdict": "%d"}`, capability, tup, pos)
		return Check(st.ctx, draftV(t, k.supervisor, version.Seed4, keyring.VerbQualified, subject, body, st.ctx.Tip))
	}
	if err := qualify(fpOf(t, k.verifier), keyring.CapVerdict, verifierTuple, verdictPos); err != nil {
		t.Fatalf("the verifier qualifies for verdict under the tuple it declared: %v", err)
	}
	chainRefusal(t, "another actor", qualify(fpOf(t, k.holder), keyring.CapVerdict, verifierTuple, verdictPos), "verdict's signer")
	chainRefusal(t, "another tuple", qualify(fpOf(t, k.verifier), keyring.CapVerdict, drifted, verdictPos), "configuration the verifier declared")
	// A verdict declaring no tuple qualifies nobody for verdict.
	subF := st.drive("e-2", calibFiled, evalSpecFor("calib"), base)
	bare := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "e-2", levelBody("pass", subF, "L3", ""))
	if err := Check(st.ctx, draftV(t, k.supervisor, version.Seed4, keyring.VerbQualified, fpOf(t, k.verifier), fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-2", "verdict": "%d"}`, keyring.CapVerdict, verifierTuple, bare), st.ctx.Tip)); err == nil || !strings.Contains(err.Error(), "declares no tuple") {
		t.Fatalf("a verdict with no declaration qualifies nothing for verdict: %v", err)
	}
	// Qualified: the set rule at render on an ordinary contract.
	st.step(k.supervisor, version.Seed4, keyring.VerbQualified, fpOf(t, k.verifier), fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-1", "verdict": "%d"}`, keyring.CapVerdict, verifierTuple, verdictPos))
	sub := st.drive("c-1", filedTier("standard"), specBody, base)
	if err := st.render(t, "c-1", levelBody("pass", sub, "L2", verifierTuple)); err != nil {
		t.Fatalf("a render declaring the cited tuple admits: %v", err)
	}
	var oog *OutOfGrantError
	if err := st.render(t, "c-1", levelBody("pass", sub, "L2", drifted)); !errors.As(err, &oog) || oog.Drift == nil {
		t.Fatalf("a render declaring a configuration the grant does not cite is out of grant: %v", err)
	}
	if err := st.render(t, "c-1", levelBody("pass", sub, "L1", "")); err != nil {
		t.Fatalf("an undeclared render is the bridge: %v", err)
	}
	subE3 := st.drive("e-3", calibFiled, evalSpecFor("calib"), base)
	if err := st.render(t, "e-3", levelBody("pass", subE3, "L3", drifted)); err != nil {
		t.Fatalf("on an eval any declared tuple renders, the drifted one included: %v", err)
	}
	// Drift: the disqualification empties the cited set and the
	// bridge does not reopen for that verifier.
	failPos := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "e-3", scoredBody("fail", subE3, "L3", drifted, scored(item("tone", "fail", "low"))))
	st.step(k.supervisor, version.Seed4, keyring.VerbDisqualified, fpOf(t, k.verifier), fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-3", "verdict": "%d", "reason": "drift"}`, keyring.CapVerdict, verifierTuple, failPos))
	if err := st.render(t, "c-1", levelBody("pass", sub, "L2", verifierTuple)); !errors.As(err, &oog) {
		t.Fatalf("after the disqualification the once-cited tuple is out of grant: %v", err)
	}
	// Before seed/4 the capability refuses by version, at the keyring.
	old := levelFixture(t, version.Seed3)
	if err := Check(old.ctx, draftV(t, old.keys.supervisor, version.Seed3, keyring.VerbQualified, fpOf(t, old.keys.verifier), fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-1", "verdict": "3"}`, keyring.CapVerdict, verifierTuple), old.ctx.Tip)); err == nil || !strings.Contains(err.Error(), version.Seed4) {
		t.Fatalf("a verdict qualification at seed/3 refuses naming seed/4: %v", err)
	}
}

// calibFiled is a filing marked as a calibration (review finding on
// the task PR): the one kind whose verdict qualifies a verifier.
const calibFiled = `{"intent": "eval calib: the scorecard against the gold", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "calib", "kind": "calibration"}}`

// conformance: review finding on the task PR — a verdict qualification
// cites a calibration and nothing else: an ordinary eval's verdict,
// however authenticated and declared, mints no verdict authority; the
// marker's kind is a seed/4 field, refused before it and refused when
// it is neither absent nor calibration.
func TestVerdictQualificationCitesACalibrationOnly(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	k := st.keys
	base := tupleJSON(t, nil)
	verifierTuple := tupleJSON(t, map[string]string{"model": "other/1"})
	subE := st.drive("e-1", evalFiled, evalSpec, base)
	pos := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "e-1", scoredBody("pass", subE, "L3", verifierTuple, scored(item("tone", "pass", "low"))))
	body := fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-1", "verdict": "%d"}`, keyring.CapVerdict, verifierTuple, pos)
	err := Check(st.ctx, draftV(t, k.supervisor, version.Seed4, keyring.VerbQualified, fpOf(t, k.verifier), body, st.ctx.Tip))
	chainRefusal(t, "an ordinary eval", err, "not a calibration")
	// The same verdict on a calibration-marked filing qualifies.
	subC := st.drive("e-2", calibFiled, evalSpecFor("calib"), base)
	posC := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "e-2", scoredBody("pass", subC, "L3", verifierTuple, scored(item("tone", "pass", "low"))))
	bodyC := fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-2", "verdict": "%d"}`, keyring.CapVerdict, verifierTuple, posC)
	if err := Check(st.ctx, draftV(t, k.supervisor, version.Seed4, keyring.VerbQualified, fpOf(t, k.verifier), bodyC, st.ctx.Tip)); err != nil {
		t.Fatalf("a calibration's verdict qualifies the verifier: %v", err)
	}
	if s, _ := st.ctx.Lifecycle.State("e-2"); s.Eval == nil || s.Eval.Kind != transition.EvalKindCalibration {
		t.Fatalf("the fold carries the marker's kind: %+v", s.Eval)
	}
	// The marker's kind: neither absent nor calibration refuses; at
	// seed/3 the field refuses by version.
	odd := strings.Replace(calibFiled, `"kind": "calibration"`, `"kind": "audit"`, 1)
	if err := Check(st.ctx, draftV(t, k.signer, version.Seed4, "intent.filed", "e-3", odd, st.ctx.Tip)); err == nil || !strings.Contains(err.Error(), "neither absent nor") {
		t.Fatalf("an unknown kind refuses by name: %v", err)
	}
	old := levelFixture(t, version.Seed3)
	if err := Check(old.ctx, draftV(t, old.keys.signer, version.Seed3, "intent.filed", "e-1", calibFiled, old.ctx.Tip)); err == nil || !strings.Contains(err.Error(), version.Seed4) {
		t.Fatalf("the kind is a seed/4 field, refused before it naming the version: %v", err)
	}
}

// conformance: review finding on the task PR — a verifier holding
// verdict by a bare grant renders under the bridge; its first failed
// calibration's disqualification admits and closes it, so renders
// under the drifted configuration, and under any other, refuse out of
// grant until a calibration passes again; the calibration subject
// itself still admits any declared tuple.
func TestFirstFailedCalibrationClosesTheBridgeAtRender(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	k := st.keys
	base := tupleJSON(t, nil)
	drifted := tupleJSON(t, map[string]string{"model": "other/2"})
	other := tupleJSON(t, map[string]string{"model": "other/3"})
	sub := st.drive("c-1", filedTier("standard"), specBody, base)
	if err := st.render(t, "c-1", levelBody("pass", sub, "L2", drifted)); err != nil {
		t.Fatalf("under the bridge a bare-grant verifier renders any declared configuration: %v", err)
	}
	subE := st.drive("e-1", calibFiled, evalSpecFor("calib"), base)
	failPos := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "e-1", scoredBody("fail", subE, "L3", drifted, scored(item("tone", "fail", "low"))))
	drop := fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": "e-1", "verdict": "%d", "reason": "drift"}`, keyring.CapVerdict, drifted, failPos)
	if err := Check(st.ctx, draftV(t, k.supervisor, version.Seed4, keyring.VerbDisqualified, fpOf(t, k.verifier), drop, st.ctx.Tip)); err != nil {
		t.Fatalf("the first failed calibration disqualifies the configuration nothing had cited: %v", err)
	}
	st.step(k.supervisor, version.Seed4, keyring.VerbDisqualified, fpOf(t, k.verifier), drop)
	var oog *OutOfGrantError
	for name, tup := range map[string]string{"the drifted configuration": drifted, "any other": other} {
		if err := st.render(t, "c-1", levelBody("pass", sub, "L2", tup)); !errors.As(err, &oog) || oog.Drift == nil {
			t.Fatalf("%s: after the first failed calibration the bridge is closed: %v", name, err)
		}
	}
	subE2 := st.drive("e-2", calibFiled, evalSpecFor("calib"), base)
	if err := st.render(t, "e-2", levelBody("pass", subE2, "L3", drifted)); err != nil {
		t.Fatalf("a calibration subject still admits any declared tuple, where the configuration re-proves itself: %v", err)
	}
}
