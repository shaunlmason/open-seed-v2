package main

// Rubric verdicts at the terminal (plans/os-2e34f66a.md AC2 to AC5):
// a spec with a rubric renders only over a scorecard; the derivation's
// refinements; the deferral and the human's render over the
// deferral's receipt; the situation read's debt; reconcile's
// scorecard_unverified.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// rubricStand is a seed/4 ledger over a repository carrying a rubric
// spec, with a verifier, a sealer and a human (verdict grant beside
// operator standing).
type rubricStand struct {
	ld, src, priv, rubricCommit, base string
	keys, fps                         map[string]string
	rootKey                           ed25519.PrivateKey
	drive                             func(subject, tier string) int
}

func newRubricStand(t *testing.T) *rubricStand {
	t.Helper()
	ld, src, base, _, head, priv, rootKey, keys, fps := offerLedger(t)
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", src, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.WriteFile(filepath.Join(src, "rubric.md"), []byte("# Judged\n\n## Rubric\n\n- tone: reads as the operator's\n- taste: the abstraction carries its weight\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "the rubric spec")
	rubricCommit := git("rev-parse", "HEAD")
	humanKey, humanPub, humanFP := writeWorkerKey(t, 26)
	keys["human"], fps["human"] = humanKey, humanFP
	for _, step := range [][]string{
		{"actor.enrolled", humanFP, fmt.Sprintf(`{"key": %q, "kind": "human", "name": "reviewer"}`, humanPub)},
		{"actor.granted", humanFP, `{"capability": "verdict"}`},
		{"actor.granted", humanFP, `{"capability": "operator"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	for _, to := range []string{version.Seed2, version.Seed3, version.Seed4} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+to+`"}`); code != 0 {
			t.Fatalf("upgrade to %s: %d %+v", to, code, e)
		}
	}
	rng := base + ".." + head
	st := &rubricStand{ld: ld, src: src, priv: priv, rubricCommit: rubricCommit, base: base, keys: keys, fps: fps, rootKey: rootKey}
	st.drive = func(subject, tier string) int {
		t.Helper()
		rawAppendAt(t, ld, rootKey, version.Seed4, "intent.filed", subject, fmt.Sprintf(`{"intent": "drill", "tier": %q, "budget": "small", "routing": "core"}`, tier))
		rawAppendAt(t, ld, rootKey, version.Seed4, "contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "rubric.md @ %s", "executable": false}}`, rubricCommit))
		fence := rawAppendAt(t, ld, rootKey, version.Seed4, "claim.taken", subject, `{}`)
		res := rawAppendAt(t, ld, rootKey, version.Seed4, "budget.reserve", subject, fmt.Sprintf(`{"amount": "10", "fence": "%d"}`, fence))
		rawAppendAt(t, ld, workerRawKey(21), version.Seed4, "run.started", subject, fmt.Sprintf(`{"fence": "%d", "reservation": "%d", "tuple": %s}`, fence, res, levelTuple("fable/5.1")))
		return rawAppendAt(t, ld, rootKey, version.Seed4, "submission.made", subject, fmt.Sprintf(
			`{"fence": "%d", "packet": {"acceptance": ["%s ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, subject, rng))
	}
	return st
}

// scorecardFile writes a scorecard scoring tone and taste with the
// given scores and uncertainties, the evidence an anchored path.
func (st *rubricStand) scorecardFile(t *testing.T, subject string, sub int, tone, toneU, taste, tasteU string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scorecard.json")
	body := fmt.Sprintf(`{"contract": %q, "submission": "%d", "items": [{"id": "tone", "score": %q, "evidence": ["hello.txt @ %s#L1-L1"], "uncertainty": %q, "note": "terse"}, {"id": "taste", "score": %q, "evidence": ["hello.txt @ %s"], "uncertainty": %q}]}`,
		subject, sub, tone, st.base, toneU, taste, st.base, tasteU)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (st *rubricStand) render(t *testing.T, key, subject, verdict string, extra ...string) (ledgerEnv, int) {
	t.Helper()
	return runEnv(t, append([]string{"verdict", "render", "--ledger", st.ld, "--subject", subject, "--repo", st.src, "--key", key, "--verdict", verdict}, extra...)...)
}

// conformance: AC2, AC3, AC4, AC5 — the terminal.
func TestRubricVerdictsAtTheTerminal(t *testing.T) {
	st := newRubricStand(t)
	sub := st.drive("c-r", "trivial")
	e, code := st.render(t, st.keys["verifier"], "c-r", "pass")
	if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "rubric of 2 items") || !strings.Contains(e.Error.Message, "--scorecard") {
		t.Fatalf("a spec with a rubric refuses at usage without a scorecard, naming it: %d %+v", code, e.Error)
	}
	// The scorecard's parts, each refused by name.
	for _, row := range []struct{ name, body, names string }{
		{"a missing item", fmt.Sprintf(`{"contract": "c-r", "submission": "%d", "items": [{"id": "tone", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}]}`, sub, st.base), `"taste" is not scored`},
		{"an unknown item", fmt.Sprintf(`{"contract": "c-r", "submission": "%d", "items": [{"id": "tone", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}, {"id": "taste", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}, {"id": "speed", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}]}`, sub, st.base, st.base, st.base), `"speed" is not in the rubric`},
		{"prose as evidence", fmt.Sprintf(`{"contract": "c-r", "submission": "%d", "items": [{"id": "tone", "score": "pass", "evidence": ["it reads well"], "uncertainty": "low"}, {"id": "taste", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}]}`, sub, st.base), "neither"},
		{"a path absent at its commit", fmt.Sprintf(`{"contract": "c-r", "submission": "%d", "items": [{"id": "tone", "score": "pass", "evidence": ["nope.txt @ %s"], "uncertainty": "low"}, {"id": "taste", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}]}`, sub, st.base, st.base), "does not resolve"},
		{"a note over the budget", fmt.Sprintf(`{"contract": "c-r", "submission": "%d", "items": [{"id": "tone", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low", "note": %q}, {"id": "taste", "score": "pass", "evidence": ["hello.txt @ %s"], "uncertainty": "low"}]}`, sub, st.base, strings.Repeat("x", 600), st.base), "budget"},
		{"a smuggled verdict", fmt.Sprintf(`{"contract": "c-r", "submission": "%d", "verdict": "pass", "items": []}`, sub), "strict object"},
	} {
		path := filepath.Join(t.TempDir(), "sc.json")
		if err := os.WriteFile(path, []byte(row.body), 0o644); err != nil {
			t.Fatal(err)
		}
		e, code := st.render(t, st.keys["verifier"], "c-r", "pass", "--scorecard", path)
		if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, row.names) {
			t.Fatalf("%s refuses naming the part (%s): %d %+v", row.name, row.names, code, e.Error)
		}
	}
	// A valid scorecard: stored, the verdict citing its digest and its
	// items' two enums, nothing else of it.
	e, code = st.render(t, st.keys["verifier"], "c-r", "pass", "--scorecard", st.scorecardFile(t, "c-r", sub, "pass", "low", "pass", "low"))
	if code != 0 || e.Result["scorecard"] == nil || e.Result["items"] == nil {
		t.Fatalf("a valid scorecard renders pass citing its digest: %d %+v", code, e)
	}
	digest, _ := e.Result["scorecard"].(string)
	st2, failEnv := loadVerdictState(st.ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	s, _ := st2.fold.State("c-r")
	if s.Verdict == nil || s.Verdict.Scorecard == nil || s.Verdict.Scorecard.Digest != digest || len(s.Verdict.Scorecard.Items) != 2 || s.Verdict.Scorecard.Items[0].ID != "tone" {
		t.Fatalf("the fold carries the scorecard's digest and items: %+v", s.Verdict)
	}
	if e, code := runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-r", "--repo", st.src); code != 0 || e.Result["scorecard"] != "verified" {
		t.Fatalf("verdict check over a rubric verdict verifies the cited scorecard too: %d %+v", code, e)
	}
	if e, code := runEnv(t, "reconcile", "--ledger", st.ld, "--repo", st.src); code != 0 || classesOf(t, e)["scorecard_unverified"] != 0 {
		t.Fatalf("an agreeing stored scorecard classifies nothing: %d %+v", code, e.Result["by_class"])
	}
	// The scorecard artifact lost: the check is not green over a store
	// that lost what the verifier scored (review finding on the task
	// PR), exactly as reconcile classifies it.
	scorecardPath := filepath.Join(artifactsDir("", st.src), "sha256", digest)
	stored, err := os.ReadFile(scorecardPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(scorecardPath); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-r", "--repo", st.src); code != 21 || e.Error == nil || e.Error.Code != "receipt_mismatch" || !strings.Contains(e.Error.Message, "scorecard") {
		t.Fatalf("a lost scorecard refuses the check as receipt_mismatch naming it: %d %+v", code, e)
	}
	if err := os.WriteFile(scorecardPath, stored, 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-r", "--repo", st.src); code != 0 {
		t.Fatalf("restored, the check is green again: %d %+v", code, e.Error)
	}

	// The derivation's refinements.
	sub2 := st.drive("c-red", "trivial")
	e, code = st.render(t, st.keys["verifier"], "c-red", "pass", "--scorecard", st.scorecardFile(t, "c-red", sub2, "pass", "low", "fail", "low"))
	if code != 20 || e.Error == nil || e.Error.Code != "rubric_red" || !strings.Contains(e.Error.Message, `"taste"`) {
		t.Fatalf("a failing item refuses pass as rubric_red naming it: %d %+v", code, e.Error)
	}
	if e, code := st.render(t, st.keys["verifier"], "c-red", "fail", "--scorecard", st.scorecardFile(t, "c-red", sub2, "pass", "low", "fail", "low")); code != 0 {
		t.Fatalf("fail stays renderable over a failing item: %d %+v", code, e.Error)
	}
	sub3 := st.drive("c-high", "trivial")
	high := st.scorecardFile(t, "c-high", sub3, "pass", "low", "pass", "high")
	for _, v := range []string{"pass", "fail"} {
		if e, code := st.render(t, st.keys["verifier"], "c-high", v, "--scorecard", high); code != 20 || e.Error == nil || e.Error.Code != "human_verdict" {
			t.Fatalf("a high item refuses %s as human_verdict: %d %+v", v, code, e.Error)
		}
	}
	// The deferral: the verifier hands the human the items and the
	// receipt; the situation read shows the operator lane's debt.
	if e, code := runEnv(t, "verdict", "defer", "--ledger", st.ld, "--subject", "c-high", "--repo", st.src, "--key", st.keys["verifier"]); code != 64 {
		t.Fatalf("a deferral on a rubric spec needs the scorecard: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "verdict", "defer", "--ledger", st.ld, "--subject", "c-high", "--repo", st.src, "--key", st.keys["verifier"], "--scorecard", st.scorecardFile(t, "c-high", sub3, "pass", "low", "pass", "low")); code != 64 || !strings.Contains(e.Error.Message, "render instead") {
		t.Fatalf("a scorecard with every item at low defers nothing: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, "verdict", "defer", "--ledger", st.ld, "--subject", "c-high", "--repo", st.src, "--key", st.keys["verifier"], "--scorecard", high)
	if code != 0 || e.Result["owed_by"] != "lane:operator" || fmt.Sprint(e.Result["items"]) != "[taste]" || e.Result["receipt"] == nil {
		t.Fatalf("the verifier defers naming the high item and citing its receipt: %d %+v", code, e)
	}
	e, code = runEnv(t, "situation", "--ledger", st.ld, "--key", st.keys["human"])
	if code != 0 {
		t.Fatalf("situation: %d %+v", code, e.Error)
	}
	owed := false
	for _, row := range e.Result["obligations"].([]any) {
		m, _ := row.(map[string]any)
		if m["kind"] == "verdict.human" && m["subject"] == "c-high" && m["owed_by"] == "lane:operator" {
			owed = true
		}
	}
	if !owed {
		t.Fatalf("the human's situation read shows the deferred verdict owed by the operator lane: %+v", e.Result["obligations"])
	}
	if e, code := runEnv(t, "verdict", "defer", "--ledger", st.ld, "--subject", "c-high", "--repo", st.src, "--key", st.keys["verifier"], "--scorecard", high); code == 0 {
		t.Fatalf("a second deferral refuses: %+v", e)
	}
	low := st.scorecardFile(t, "c-high", sub3, "pass", "low", "pass", "low")
	if e, code := st.render(t, st.keys["verifier"], "c-high", "pass", "--scorecard", low); code != 20 || e.Error == nil || e.Error.Code != "human_verdict" || !strings.Contains(e.Error.Message, "deferred at position") {
		t.Fatalf("after the deferral the deferring key refuses human_verdict naming the deferral: %d %+v", code, e.Error)
	}
	e, code = st.render(t, st.keys["human"], "c-high", "pass", "--scorecard", low)
	if code != 0 || e.Result["scorecard"] == nil {
		t.Fatalf("the human renders over the deferral's receipt with its own scorecard: %d %+v", code, e)
	}
	e, code = runEnv(t, "situation", "--ledger", st.ld, "--key", st.keys["human"])
	for _, row := range e.Result["obligations"].([]any) {
		if m, _ := row.(map[string]any); m["kind"] == "verdict.human" {
			t.Fatalf("the human's render clears the debt: %+v", m)
		}
	}
	if e, code := runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-high", "--repo", st.src); code != 0 {
		t.Fatalf("verdict check recomputes the deferral's receipt the human cited: %d %+v", code, e.Error)
	}
	// The human-review tier: the verifier's one act is the deferral,
	// with no scorecard where the spec has no rubric and no items.
	// (A critical contract needs a seal; the level drill covers it.)

	// Evidence grade: a store that lost the scorecards classifies
	// scorecard_unverified for every verdict citing one.
	if err := os.RemoveAll(filepath.Join(st.src, "var", "artifacts")); err != nil {
		t.Fatal(err)
	}
	e, code = runEnv(t, "reconcile", "--ledger", st.ld, "--repo", st.src)
	if code != 0 || classesOf(t, e)["scorecard_unverified"] != 3 {
		t.Fatalf("lost scorecards classify scorecard_unverified per verdict (c-r, c-red, c-high): %d %+v", code, e.Result["by_class"])
	}
	_ = event.Fingerprint
}
