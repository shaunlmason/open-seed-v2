package main

// Calibration at the terminal (plans/os-2e34f66a.md AC6): a
// calibration definition in a temporary repository with its gold held
// outside it, `seed eval check` proving the known verdict, the
// verifier's scorecard render, `seed eval status|act --gold` owing and
// performing the verdict qualification, drift suspending the render
// under that configuration until a calibration passes again, and the
// dispatcher's defect filing once.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const cliGold = `{"items": [{"id": "tone", "score": "pass"}, {"id": "taste", "score": "fail"}, {"id": "clarity", "score": "pass"}, {"id": "brevity", "score": "pass"}, {"id": "care", "score": "pass"}]}`

// plantCalibrationCLI plants a calibration definition at a reviewed
// revision and returns the directory holding its gold, outside the
// repository.
func plantCalibrationCLI(t *testing.T, repo string) (goldDir string) {
	t.Helper()
	g, err := eval.ParseGold([]byte(cliGold))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo, eval.Root, "calib")
	for _, f := range []struct{ path, body string }{
		{"eval.json", fmt.Sprintf(`{"name": "calib", "summary": "planted", "tier": "trivial", "acceptance": "evals/calib/fixture/accept.md", "kind": "calibration", "calibration": {"gold": "sha256:%s"}}`, g.Digest)},
		{"fixture/greet.sh", "#!/bin/sh\nprintf 'hello, wrold\\n'\n"},
		{"fixture/check.sh", "#!/bin/sh\nout=$(sh \"$(dirname \"$0\")/greet.sh\")\n[ \"$out\" = \"hello, world\" ] || exit 1\n"},
		{"fixture/accept.md", "# calib\n\n## Validation commands\n\n- `sh evals/calib/fixture/check.sh`\n\n## Rubric\n\n- tone: reads as the operator's\n- taste: the abstraction carries its weight\n- clarity: says one thing\n- brevity: says it once\n- care: handles the edge\n"},
		{"solution/greet.sh", "#!/bin/sh\nprintf 'hello, world\\n'\n"},
	} {
		p := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "--quiet", "-m", "evals: calib (#2)"}} {
		full := append([]string{"-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	goldDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(goldDir, "calib.json"), []byte(cliGold), 0o644); err != nil {
		t.Fatal(err)
	}
	return goldDir
}

// calibScorecard writes the verifier's scorecard for the calibration,
// the evidence its one transcript.
func calibScorecard(t *testing.T, subject string, sub int, scores map[string]string) string {
	t.Helper()
	var items []string
	for _, id := range []string{"tone", "taste", "clarity", "brevity", "care"} {
		items = append(items, fmt.Sprintf(`{"id": %q, "score": %q, "evidence": ["transcript:0"], "uncertainty": "low"}`, id, scores[id]))
	}
	path := filepath.Join(t.TempDir(), "scorecard.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"contract": %q, "submission": "%d", "items": [%s]}`, subject, sub, strings.Join(items, ", "))), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCalibrationAtTheTerminal(t *testing.T) {
	repo, _ := evalRepo(t)
	goldDir := plantCalibrationCLI(t, repo)
	ld, priv, keys, fps := evalLedger(t)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	if e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "calib"); code != 0 {
		t.Fatalf("the calibration's known verdict is proven like any eval's: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "eval", "list", "--repo", filepath.Join("..", "..")); code != 0 || strings.Contains(fmt.Sprint(e.Result), "calibration") {
		t.Fatalf("the shipped tree ships no calibration definition: %d %+v", code, e.Result)
	}
	declare := func(model string) []string {
		return []string{"--principal", "acme", "--model", model, "--tool-policy", "default"}
	}
	agreeing := map[string]string{"tone": "pass", "taste": "fail", "clarity": "pass", "brevity": "pass", "care": "pass"}
	drifted := map[string]string{"tone": "pass", "taste": "pass", "clarity": "pass", "brevity": "fail", "care": "pass"}
	// calibrate files the calibration, offers it, has workerA solve it
	// and the verifier render with the scorecard and the declared
	// tuple; returns the subject.
	calibrate := func(scores map[string]string, verdict string) string {
		t.Helper()
		e, code := runEnv(t, "eval", "file", "--ledger", ld, "--repo", repo, "--key", priv, "--eval", "calib")
		if code != 0 {
			t.Fatalf("eval file: %d %+v", code, e.Error)
		}
		subject, _ := e.Result["subject"].(string)
		if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"], "--gold", goldDir); code != 0 {
			t.Fatalf("the supervisor offers: %d %+v", code, e.Error)
		}
		fence, _ := openWindow(t, ld, 22, keys["workerA"], subject)
		if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", subject}, declare("fable/5.1")...)...); code != 0 {
			t.Fatalf("run start: %d %+v", code, e.Error)
		}
		st, _ := loadVerdictState(ld)
		s, _ := st.fold.State(subject)
		anchor := strings.TrimPrefix(s.Acceptance.Ref[strings.LastIndex(s.Acceptance.Ref, "@ ")+2:], "")
		head := solve(t, repo, anchor, "calib", true)
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"], "--verb", "submission.made", "--subject", subject, "--payload",
			fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["the check is green"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, anchor+".."+head)); code != 0 {
			t.Fatalf("submission: %d %+v", code, e.Error)
		}
		st, _ = loadVerdictState(ld)
		s, _ = st.fold.State(subject)
		e, code = runEnv(t, append([]string{"verdict", "render", "--ledger", ld, "--subject", subject, "--repo", repo, "--key", keys["verifier"], "--verdict", verdict,
			"--scorecard", calibScorecard(t, subject, s.Submission.Pos, scores)}, declare("other/1")...)...)
		if code != 0 {
			t.Fatalf("the verifier renders the calibration with its scorecard: %d %+v", code, e.Error)
		}
		return subject
	}
	e1 := calibrate(agreeing, "fail")
	if owed, notes := dueActs(t, ld, repo); len(owed) != 0 || !strings.Contains(fmt.Sprint(notes), "gold_missing") {
		t.Fatalf("without --gold the calibration owes nothing, its offer included, and notes gold_missing: %+v / %+v", owed, notes)
	}
	wrongDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrongDir, "calib.json"), []byte(`{"items": [{"id": "tone", "score": "fail"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if owed, notes := dueActs(t, ld, repo, "--gold", wrongDir); len(owed) != 0 || !strings.Contains(fmt.Sprint(notes), "gold_mismatch") {
		t.Fatalf("a gold that is not the commitment scores nothing: %s %+v", kinds(owed), notes)
	}
	owed, _ := dueActs(t, ld, repo, "--gold", goldDir)
	if kinds(owed) != "mint:"+fps["verifier"] {
		t.Fatalf("agreement owes the verifier's verdict mint: %s", kinds(owed))
	}
	var mint map[string]any
	if err := json.Unmarshal([]byte(owed[0].(map[string]any)["payload"].(string)), &mint); err != nil {
		t.Fatal(err)
	}
	if mint["capability"] != "verdict" || mint["contract"] != e1 || mint["tuple"].(map[string]any)["model"] != "other/1" {
		t.Fatalf("the mint is for verdict under the declared tuple: %+v", mint)
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"], "--gold", goldDir); code != 0 {
		t.Fatalf("the supervisor mints: %d %+v", code, e.Error)
	}
	if owed, _ := dueActs(t, ld, repo, "--gold", goldDir); len(owed) != 0 {
		t.Fatalf("one verdict, one consequence: %s", kinds(owed))
	}

	// The set rule at render: an ordinary contract under the cited
	// tuple renders; under another it is out of grant; on a
	// calibration any declared tuple renders.
	ordinary := func(subject string) int {
		t.Helper()
		specCommit := plantSpec(t, repo)
		rootAppend(t, ld, priv, "intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
		rootAppend(t, ld, priv, "contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, specCommit))
		fence, _ := openWindow(t, ld, 22, keys["workerA"], subject)
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"], "--verb", "submission.made", "--subject", subject, "--payload",
			fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["done"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, specCommit+".."+specCommit)); code != 0 {
			t.Fatalf("submission: %d %+v", code, e.Error)
		}
		return 0
	}
	ordinary("c-1")
	render := func(subject, model string) (ledgerEnv, int) {
		args := []string{"verdict", "render", "--ledger", ld, "--subject", subject, "--repo", repo, "--key", keys["verifier"], "--verdict", "pass"}
		if model != "" {
			args = append(args, declare(model)...)
		}
		return runEnv(t, args...)
	}
	if e, code := render("c-1", "other/2"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a render under a configuration the grant does not cite is out of grant: %d %+v", code, e.Error)
	}
	if e, code := render("c-1", "other/1"); code != 0 {
		t.Fatalf("a render under the qualified configuration admits: %d %+v", code, e.Error)
	}

	// Drift: the disqualification and the dispatcher's defect, once;
	// the drifted configuration refuses until a calibration passes.
	e2 := calibrate(drifted, "fail")
	owed, _ = dueActs(t, ld, repo, "--gold", goldDir)
	if kinds(owed) != "disqualify:"+fps["verifier"]+" defect:"+eval.DriftDefectID(e2) {
		t.Fatalf("drift owes the disqualification and the defect: %s", kinds(owed))
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"], "--gold", goldDir); code != 0 {
		t.Fatalf("the supervisor disqualifies; the defect is the dispatcher's and reported owed: %d %+v", code, e.Error)
	} else if o, _ := e.Result["owed"].([]any); len(o) != 1 || o[0].(map[string]any)["kind"] != "defect" {
		t.Fatalf("the defect filing is owed by the dispatch lane: %+v", e.Result)
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", priv, "--gold", goldDir); code != 0 {
		t.Fatalf("the operator's key files the defect: %d %+v", code, e.Error)
	}
	if owed, _ := dueActs(t, ld, repo, "--gold", goldDir); len(owed) != 0 {
		t.Fatalf("the filed defect and the performed disqualification are not owed again: %s", kinds(owed))
	}
	st, _ := loadVerdictState(ld)
	if s, ok := st.fold.State(eval.DriftDefectID(e2)); !ok || s.State != "backlog" {
		t.Fatalf("the defect contract lands in the queue: %+v", s)
	}
	ordinary("c-2")
	if e, code := render("c-2", "other/1"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("after drift the once-cited configuration is out of grant: %d %+v", code, e.Error)
	}
	if e, code := render("c-2", ""); code != 0 {
		t.Fatalf("an undeclared render is the bridge: %d %+v", code, e.Error)
	}
	// A passing calibration re-qualifies.
	calibrate(agreeing, "fail")
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"], "--gold", goldDir); code != 0 {
		t.Fatalf("the supervisor re-qualifies: %d %+v", code, e.Error)
	}
	ordinary("c-3")
	if e, code := render("c-3", "other/1"); code != 0 {
		t.Fatalf("re-qualified, the configuration renders again: %d %+v", code, e.Error)
	}
}

// plantSpec commits a prose spec into the eval repository and returns
// its commit: an ordinary contract's acceptance.
func plantSpec(t *testing.T, repo string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "accept.md"), []byte("# Prose\n\nJudged by hand.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("add", ".")
	git("commit", "--quiet", "--allow-empty", "-m", "the prose spec")
	return git("rev-parse", "HEAD")
}
