package main

// The eval drills end to end (plans/os-03e47abb.md; spec/evals.md):
// a definition checked (fixture red, solution green) through the
// verifier's own runner; filed as an ordinary contract marked as an
// eval; offered, claimed, run under a declared tuple, worked, submitted
// and judged by the production machinery; the pass minting the
// holder's qualification once its receipt recomputes green; the fail
// disqualifying every holder of the configuration; spot-checks aging
// from the qualification's own ts; and the acts performed only by the
// lanes that own them.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// evalRepo is a repository carrying the shipped eval definitions,
// committed under a squash-merge subject so the anchor derivation finds
// a reviewed revision. It returns the repository and the anchor commit.
func evalRepo(t *testing.T) (repo, anchor string) {
	t.Helper()
	repo = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("eval fixture repository\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "base")
	copyTree(t, filepath.Join("..", "..", "evals"), filepath.Join(repo, eval.Root))
	git("add", ".")
	git("commit", "--quiet", "-m", "evals: the shipped definitions (#1)")
	return repo, git("rev-parse", "HEAD")
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// plantEval writes a second definition beside the shipped one and
// commits it under the given subject.
func plantEval(t *testing.T, repo, name, greeting, solution, subject string) {
	t.Helper()
	dir := filepath.Join(repo, eval.Root, name)
	for _, f := range []struct{ path, body string }{
		{"eval.json", fmt.Sprintf(`{"name": %q, "summary": "planted", "tier": "trivial", "acceptance": "evals/%s/fixture/accept.md"}`, name, name)},
		{"fixture/greet.sh", "#!/bin/sh\nprintf '" + greeting + "\\n'\n"},
		{"fixture/check.sh", "#!/bin/sh\nout=$(sh \"$(dirname \"$0\")/greet.sh\")\n[ \"$out\" = \"hello, world\" ] || exit 1\n"},
		{"fixture/accept.md", "# planted\n\n## Validation commands\n\n- `sh evals/" + name + "/fixture/check.sh`\n"},
		{"solution/greet.sh", "#!/bin/sh\nprintf '" + solution + "\\n'\n"},
	} {
		p := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "--quiet", "-m", subject}} {
		full := append([]string{"-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// conformance: plans/os-03e47abb.md AC1 — the known verdict is proven,
// never asserted: the unsolved fixture is red and the solution green
// through the verifier's own runner; a vacuous eval, a red solution, an
// unreviewed definition and a dirty one each refuse by name.
func TestEvalCheckProvesTheKnownVerdict(t *testing.T) {
	repo, anchor := evalRepo(t)
	e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "fix-the-check")
	if code != 0 {
		t.Fatalf("the shipped eval checks clean: %d %+v", code, e.Error)
	}
	got, _ := e.Result["anchor"].(map[string]any)
	if got["commit"] != anchor || got["pr"] != "pr/1" || got["ref"] != "evals/fix-the-check/fixture/accept.md @ "+anchor || got["gate"] != "pr/1 @ "+anchor {
		t.Fatalf("the anchor is the definition's last squash-merged commit and its PR, ref and gate on the same commit: %+v", got)
	}
	fixture, _ := e.Result["fixture"].([]any)
	solution, _ := e.Result["solution"].([]any)
	if len(fixture) != 1 || fixture[0].(map[string]any)["exit"].(float64) == 0 {
		t.Fatalf("the unsolved fixture is red: %+v", fixture)
	}
	if len(solution) != 1 || solution[0].(map[string]any)["exit"].(float64) != 0 {
		t.Fatalf("the solution is green: %+v", solution)
	}
	if rows, _ := runEnv(t, "eval", "list", "--repo", repo); rows.Result == nil {
		t.Fatal("eval list renders")
	} else if evals, _ := rows.Result["evals"].([]any); len(evals) != 1 || evals[0].(map[string]any)["reviewed"] != true {
		t.Fatalf("eval list reports the reviewed definition: %+v", rows.Result)
	}

	plantEval(t, repo, "vacuous", "hello, world", "hello, world", "evals: vacuous (#2)")
	if e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "vacuous"); code != 19 || e.Error == nil || e.Error.Code != "eval_vacuous" {
		t.Fatalf("an eval whose fixture already passes decides nothing: %d %+v", code, e.Error)
	}
	plantEval(t, repo, "stays-red", "hello, wrold", "hello, wrold", "evals: stays-red (#3)")
	if e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "stays-red"); code != 20 || e.Error == nil || e.Error.Code != "checks_red" {
		t.Fatalf("a solution that stays red cannot reproduce the known verdict: %d %+v", code, e.Error)
	}
	plantEval(t, repo, "unreviewed", "hello, wrold", "hello, world", "evals: unreviewed, no pull request")
	if e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "unreviewed"); code != 18 || e.Error == nil || e.Error.Code != "ungated" || !strings.Contains(e.Error.Message, "no merged pull request number") {
		t.Fatalf("a definition whose last commit is not a merged PR is not at a reviewed revision: %d %+v", code, e.Error)
	}
	if err := os.WriteFile(filepath.Join(repo, eval.Root, "fix-the-check", "fixture", "greet.sh"), []byte("#!/bin/sh\nprintf 'dirty\\n'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "fix-the-check"); code != 18 || e.Error == nil || !strings.Contains(e.Error.Message, "uncommitted") {
		t.Fatalf("a dirty definition is not at a reviewed revision: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "eval", "check", "--repo", repo, "--eval", "nope"); code != 4 || e.Error == nil {
		t.Fatalf("an unknown definition is not found: %d %+v", code, e.Error)
	}
	for name, args := range map[string][]string{
		"no subverb": {"eval"}, "unknown subverb": {"eval", "run"}, "check without eval": {"eval", "check", "--repo", repo},
		"file without transport": {"eval", "file", "--repo", repo, "--eval", "fix-the-check", "--key", "x"},
	} {
		if e, code := runEnv(t, args...); code != 64 || e.Error == nil {
			t.Fatalf("%s refuses as usage: %d %+v", name, code, e)
		}
	}
}

// evalLedger is offerLedger carried to seed/3.
func evalLedger(t *testing.T) (ld, priv string, keys, fps map[string]string) {
	t.Helper()
	var rootKey any
	ld, _, _, _, _, priv, rootKey, keys, fps = offerLedger(t)
	_ = rootKey
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	return ld, priv, keys, fps
}

// solve commits the eval's reference solution on a work branch from
// the anchor and returns the head: the worker's submission range.
func solve(t *testing.T, repo, anchor, name string, fix bool) string {
	t.Helper()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=worker", "-c", "user.email=worker@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	branch := fmt.Sprintf("work-%d", time.Now().UnixNano())
	git("checkout", "--quiet", "-b", branch, anchor)
	defer git("checkout", "--quiet", "main")
	if fix {
		b, err := os.ReadFile(filepath.Join(repo, eval.Root, name, "solution", "greet.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, eval.Root, name, "fixture", "greet.sh"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(repo, eval.Root, name, "fixture", "NOTES"), []byte("tried\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "the work")
	return git("rev-parse", "HEAD")
}

func dueActs(t *testing.T, ld, repo string, extra ...string) ([]any, []any) {
	t.Helper()
	args := append([]string{"eval", "status", "--ledger", ld, "--repo", repo}, extra...)
	e, code := runEnv(t, args...)
	if code != 0 {
		t.Fatalf("eval status: %d %+v", code, e.Error)
	}
	owed, _ := e.Result["owed"].([]any)
	notes, _ := e.Result["notes"].([]any)
	return owed, notes
}

func kinds(acts []any) string {
	var out []string
	for _, a := range acts {
		m := a.(map[string]any)
		out = append(out, fmt.Sprintf("%s:%s", m["kind"], m["subject"]))
	}
	return strings.Join(out, " ")
}

// conformance: plans/os-03e47abb.md AC2, AC3 (the recomputation half),
// AC4, AC5, AC7 — the eval lifecycle end to end on a local ledger.
func TestEvalLifecycleMintsDisqualifiesAndReTests(t *testing.T) {
	repo, anchor := evalRepo(t)
	ld, priv, keys, fps := evalLedger(t)

	// The dispatcher's filing (the root is the operator here): an
	// ordinary contract marked as an eval, its acceptance anchored at
	// the reviewed commit with the gate on the same commit.
	e, code := runEnv(t, "eval", "file", "--ledger", ld, "--repo", repo, "--key", priv, "--eval", "fix-the-check")
	if code != 0 {
		t.Fatalf("eval file: %d %+v", code, e.Error)
	}
	subject, _ := e.Result["subject"].(string)
	if !strings.HasPrefix(subject, "eval-") || e.Result["gate"] != "pr/1 @ "+anchor {
		t.Fatalf("the filing names the eval and cites the reviewed commit on both sides: %+v", e.Result)
	}
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	if s, _ := st.fold.State(subject); s.Eval == nil || s.Eval.Name != "fix-the-check" || s.State != "ready" {
		t.Fatalf("the eval folds with its marker and is ready: %+v", s)
	}

	// What is owed: the offer, the supervisor's. The dispatcher's key
	// (here the root's, which is operator) performs everything; a key
	// holding neither refuses at the door with nothing appended.
	if owed, _ := dueActs(t, ld, repo); kinds(owed) != "offer:"+subject {
		t.Fatalf("a ready eval with no live offer owes an offer: %s", kinds(owed))
	}
	before := st.count
	e, code = runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["verifier"])
	if code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a key holding no eval lane refuses out_of_grant: %d %+v", code, e.Error)
	}
	if refused, _ := e.Result["refused"].([]any); len(refused) != 1 || refused[0].(map[string]any)["code"] != "out_of_grant" || refused[0].(map[string]any)["kind"] != "offer" {
		t.Fatalf("each owed act is reported refused by name: %+v", e.Result)
	}
	if st, _ = loadVerdictState(ld); st.count != before {
		t.Fatal("an unentitled act appends nothing")
	}
	e, code = runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"])
	if code != 0 {
		t.Fatalf("the supervisor publishes the offer: %d %+v", code, e.Error)
	}
	if performed, _ := e.Result["performed"].([]any); len(performed) != 1 || performed[0].(map[string]any)["verb"] != "offer.published" {
		t.Fatalf("performed: %+v", e.Result)
	}
	if offers := listOffers(t, ld, fps["workerA"], ""); len(offers) != 1 {
		t.Fatalf("the eval's offer lists to the worker, unscoped by tuple: %+v", offers)
	}

	// The production machinery: claim, reserve, the supervisor's start
	// declaring the worker's configuration (the worker is unqualified,
	// so any declaration admits on an ordinary contract; on an eval any
	// admits regardless), the work, the submission, the verdict.
	fence, _ := openWindow(t, ld, 22, keys["workerA"], subject)
	declare := []string{"--principal", "acme", "--model", "fable/5.1", "--tool-policy", "default"}
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", subject}, declare...)...); code != 0 {
		t.Fatalf("run start on the eval: %d %+v", code, e.Error)
	}
	head := solve(t, repo, anchor, "fix-the-check", true)
	submit := func(subject, fence, head string) {
		t.Helper()
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"], "--verb", "submission.made", "--subject", subject, "--payload",
			fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["the check is green"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, anchor+".."+head)); code != 0 {
			t.Fatalf("submission: %d %+v", code, e.Error)
		}
	}
	submit(subject, fence, head)
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", subject, "--repo", repo, "--key", keys["verifier"], "--verdict", "pass"); code != 0 {
		t.Fatalf("the verifier renders the pass with real transcripts: %d %+v", code, e.Error)
	}

	// The mint is owed to the supervisor for the HOLDER and the DECLARED
	// tuple, and performed only after the receipt recomputed green.
	owed, notes := dueActs(t, ld, repo)
	if kinds(owed) != "mint:"+fps["workerA"] {
		t.Fatalf("a recomputed green pass owes the holder's mint: %s (notes %+v)", kinds(owed), notes)
	}
	var mint map[string]any
	if err := json.Unmarshal([]byte(owed[0].(map[string]any)["payload"].(string)), &mint); err != nil {
		t.Fatal(err)
	}
	if tu, _ := mint["tuple"].(map[string]any); tu["model"] != "fable/5.1" || tu["harness"] != "local-worktree/v0" || mint["contract"] != subject {
		t.Fatalf("the mint cites the declared configuration and the eval: %+v", mint)
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"]); code != 0 {
		t.Fatalf("the supervisor mints: %d %+v", code, e.Error)
	}
	if owed, _ := dueActs(t, ld, repo); len(owed) != 0 {
		t.Fatalf("one verdict, one consequence: nothing is owed after the mint: %s", kinds(owed))
	}
	st, _ = loadVerdictState(ld)
	if s, _ := st.fold.State(subject); s.Eval == nil {
		t.Fatal("the judged eval stays in the fold")
	}

	// AC2's consequence on an ordinary contract: under the qualified
	// configuration a run admits; under one differing in a field it is
	// out of grant, item 1's rule fed by a mint rather than a hand.
	offerFile(t, ld, priv, "0000000000000000000000000000000000000000", "c-ord")
	openWindow(t, ld, 22, keys["workerA"], "c-ord")
	if e, code := runEnv(t, "run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-ord", "--principal", "acme", "--model", "fable/9.9", "--tool-policy", "default"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a differing model on an ordinary contract is out of grant: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-ord"}, declare...)...); code != 0 {
		t.Fatalf("the qualified configuration admits: %d %+v", code, e.Error)
	}

	// AC3, the recomputation half: a pass whose receipt no run produced
	// mints nothing. A second eval, judged by a raw-pushed pass from the
	// verdict-granted, disjoint verifier citing an invented digest.
	e, code = runEnv(t, "eval", "file", "--ledger", ld, "--repo", repo, "--key", priv, "--eval", "fix-the-check")
	if code != 0 {
		t.Fatalf("second filing: %d %+v", code, e.Error)
	}
	second, _ := e.Result["subject"].(string)
	if second == subject {
		t.Fatal("a second filing of the same eval gets its own id")
	}
	fence2, _ := openWindow(t, ld, 23, keys["workerB"], second)
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", second}, declare...)...); code != 0 {
		t.Fatalf("run start on the second eval: %d %+v", code, e.Error)
	}
	head2 := solve(t, repo, anchor, "fix-the-check", true)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerB"], "--verb", "submission.made", "--subject", second, "--payload",
		fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["the check is green"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence2, anchor+".."+head2)); code != 0 {
		t.Fatalf("submission: %d %+v", code, e.Error)
	}
	st, _ = loadVerdictState(ld)
	sub2, _ := st.fold.State(second)
	rawAppendAt(t, ld, workerRawKey(24), version.Seed3, "verdict.rendered", second,
		fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`, strings.Repeat("ab", 32), sub2.Submission.Pos))
	owed, notes = dueActs(t, ld, repo)
	if len(owed) != 0 {
		t.Fatalf("an invented receipt mints nothing: %s", kinds(owed))
	}
	found := false
	for _, n := range notes {
		if m := n.(map[string]any); m["subject"] == second && m["kind"] == "receipt_missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the derivation says why by name: %+v", notes)
	}

	// And the third arm of the same criterion: a pass citing a receipt
	// that retrieves and reproduces but carries a red transcript. The
	// fail over unsolved work stores exactly such a receipt; a raw pass
	// by the verdict-granted, disjoint verifier cites its digest.
	e, code = runEnv(t, "eval", "file", "--ledger", ld, "--repo", repo, "--key", priv, "--eval", "fix-the-check")
	if code != 0 {
		t.Fatalf("red filing: %d %+v", code, e.Error)
	}
	red, _ := e.Result["subject"].(string)
	fenceRed, _ := openWindow(t, ld, 23, keys["workerB"], red)
	// Declared under a configuration nobody holds, so the fail that
	// follows disqualifies nobody and the row isolates the receipt.
	if e, code := runEnv(t, "run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", red, "--principal", "acme", "--model", "fable/7.7", "--tool-policy", "default"); code != 0 {
		t.Fatalf("run start on the red eval: %d %+v", code, e.Error)
	}
	headRed := solve(t, repo, anchor, "fix-the-check", false)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerB"], "--verb", "submission.made", "--subject", red, "--payload",
		fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["the check is green"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fenceRed, anchor+".."+headRed)); code != 0 {
		t.Fatalf("submission: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", red, "--repo", repo, "--key", keys["verifier"], "--verdict", "fail")
	if code != 0 {
		t.Fatalf("the fail over unsolved work renders: %d %+v", code, e.Error)
	}
	redReceipt, _ := e.Result["receipt"].(string)
	st, _ = loadVerdictState(ld)
	subRed, _ := st.fold.State(red)
	rawAppendAt(t, ld, workerRawKey(24), version.Seed3, "verdict.rendered", red,
		fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`, redReceipt, subRed.Submission.Pos))
	owed, notes = dueActs(t, ld, repo)
	if len(owed) != 0 {
		t.Fatalf("a pass over a red receipt mints nothing: %s", kinds(owed))
	}
	found = false
	for _, n := range notes {
		if m := n.(map[string]any); m["subject"] == red && m["kind"] == "checks_red" {
			found = true
		}
	}
	if !found {
		t.Fatalf("and the derivation names the red transcript: %+v", notes)
	}

	// AC5: spot-checks age from the qualification's own ts. Younger
	// than the interval, nothing; older, the dispatcher's filing and
	// specification are owed and the supervisor cannot perform them; an
	// instant BEFORE the qualification's ts makes it due at once, noted.
	if owed, _ := dueActs(t, ld, repo, "--spot-check-after", "24h"); len(owed) != 0 {
		t.Fatalf("a fresh qualification owes no re-test: %s", kinds(owed))
	}
	later := time.Now().UTC().Add(25 * time.Hour).Format(time.RFC3339)
	owed, _ = dueActs(t, ld, repo, "--spot-check-after", "24h", "--as-of", later)
	if len(owed) != 2 || owed[0].(map[string]any)["kind"] != "spot-check" || owed[0].(map[string]any)["verb"] != "intent.filed" || owed[1].(map[string]any)["verb"] != "contract.specified" {
		t.Fatalf("a stale qualification owes a spot-check filing and specification: %s", kinds(owed))
	}
	spot, _ := owed[0].(map[string]any)["subject"].(string)
	if owed, _ := dueActs(t, ld, repo, "--spot-check-after", "0", "--as-of", later); len(owed) != 0 {
		t.Fatalf("zero disables spot-checks: %s", kinds(owed))
	}
	earlier := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	owed, notes = dueActs(t, ld, repo, "--spot-check-after", "24h", "--as-of", earlier)
	if len(owed) != 2 {
		t.Fatalf("a qualification dated after the declared instant is due at once: %s", kinds(owed))
	}
	if !strings.Contains(fmt.Sprint(notes), "cannot postpone") {
		t.Fatalf("and it is noted as an anomaly: %+v", notes)
	}
	e, code = runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"], "--spot-check-after", "24h", "--as-of", later)
	if code != 0 {
		t.Fatalf("the supervisor's act: %d %+v", code, e.Error)
	}
	if owedBy, _ := e.Result["owed"].([]any); len(owedBy) != 2 || owedBy[0].(map[string]any)["lane"] != "dispatch" {
		t.Fatalf("the filing is the dispatcher's, reported as owed: %+v", e.Result)
	}
	e, code = runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", priv, "--spot-check-after", "24h", "--as-of", later)
	if code != 0 {
		t.Fatalf("the operator files the spot-check: %d %+v", code, e.Error)
	}
	if performed, _ := e.Result["performed"].([]any); len(performed) != 2 {
		t.Fatalf("the filing and the specification: %+v", e.Result)
	}
	st, _ = loadVerdictState(ld)
	if s, _ := st.fold.State(spot); s.Eval == nil || s.Eval.Tuple == nil || s.Eval.Tuple.Model != "fable/5.1" {
		t.Fatalf("the spot-check names the configuration under re-test: %+v", s.Eval)
	}
	// Each act is one derivation at one instant: the offer the now-ready
	// spot-check owes surfaces on the next, owed by the supervisor, and
	// with the eval open nothing else is due for the tuple.
	if owed, _ := dueActs(t, ld, repo, "--spot-check-after", "24h", "--as-of", later); kinds(owed) != "offer:"+spot || owed[0].(map[string]any)["lane"] != "supervise" {
		t.Fatalf("the open spot-check owes only its offer: %s", kinds(owed))
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"], "--spot-check-after", "24h", "--as-of", later); code != 0 {
		t.Fatalf("the supervisor publishes the spot-check: %d %+v", code, e.Error)
	}
	if owed, _ := dueActs(t, ld, repo, "--spot-check-after", "24h", "--as-of", later); len(owed) != 0 {
		t.Fatalf("with a spot-check open nothing more is owed: %s", kinds(owed))
	}

	// AC4: the spot-check fails, and every holder of the configuration
	// is disqualified: workerA (qualified by the mint) and workerB
	// (granted the same tuple by hand). Afterwards workerA, whose only
	// cited configuration it was, admits nothing; a passing eval
	// re-qualifies.
	rootAppend(t, ld, priv, "actor.granted", fps["workerB"], `{"capability": "claim", "tuple": `+drillTuple(nil)+`}`)
	fence3, _ := openWindow(t, ld, 22, keys["workerA"], spot)
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", spot}, declare...)...); code != 0 {
		t.Fatalf("run start on the spot-check: %d %+v", code, e.Error)
	}
	head3 := solve(t, repo, anchor, "fix-the-check", false)
	submit(spot, fence3, head3)
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", spot, "--repo", repo, "--key", keys["verifier"], "--verdict", "pass"); code != 20 {
		t.Fatalf("the unsolved work is red, so pass is not renderable: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", spot, "--repo", repo, "--key", keys["verifier"], "--verdict", "fail"); code != 0 {
		t.Fatalf("the fail renders: %d %+v", code, e.Error)
	}
	owed, _ = dueActs(t, ld, repo)
	if kinds(owed) != "disqualify:"+fps["workerA"]+" disqualify:"+fps["workerB"] && kinds(owed) != "disqualify:"+fps["workerB"]+" disqualify:"+fps["workerA"] {
		t.Fatalf("a fail disqualifies every holder of the configuration: %s", kinds(owed))
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"]); code != 0 {
		t.Fatalf("the supervisor disqualifies: %d %+v", code, e.Error)
	}
	offerFile(t, ld, priv, "0000000000000000000000000000000000000000", "c-ord2")
	openWindow(t, ld, 22, keys["workerA"], "c-ord2")
	e, code = runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-ord2"}, declare...)...)
	if code != 14 || e.Error == nil || !strings.Contains(e.Error.Message, "every cited configuration is disqualified") {
		t.Fatalf("the bridge does not reopen: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-ord2", "--principal", "anyone", "--model", "anything/0", "--tool-policy", "x"); code != 14 {
		t.Fatalf("nor for any other configuration: %d %+v", code, e.Error)
	}
	// Re-qualification: a fresh eval passes under the configuration.
	e, code = runEnv(t, "eval", "file", "--ledger", ld, "--repo", repo, "--key", priv, "--eval", "fix-the-check")
	if code != 0 {
		t.Fatalf("third filing: %d %+v", code, e.Error)
	}
	third, _ := e.Result["subject"].(string)
	fence4, _ := openWindow(t, ld, 22, keys["workerA"], third)
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", third}, declare...)...); code != 0 {
		t.Fatalf("a disqualified configuration still runs an eval (D6): %d %+v", code, e.Error)
	}
	submit(third, fence4, solve(t, repo, anchor, "fix-the-check", true))
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", third, "--repo", repo, "--key", keys["verifier"], "--verdict", "pass"); code != 0 {
		t.Fatalf("pass: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"]); code != 0 {
		t.Fatalf("re-qualification: %d %+v", code, e.Error)
	}
	// The window on c-ord2 is still open: the re-qualified configuration admits there now.
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-ord2"}, declare...)...); code != 0 {
		t.Fatalf("the re-qualified configuration admits: %d %+v", code, e.Error)
	}
}

// conformance: plans/os-03e47abb.md AC3's tail and AC9 — at a seed/2
// position the qualification verbs fail verification as
// bad_actor_event at their position, naming the version that defines
// them; this build verifies a seed/3 chain, and a seed/2-only build
// refuses it at the first seed/3 record by version, never by judging a
// qualification.
func TestQualificationVerbsAreVersionedLikeTheGrantTuple(t *testing.T) {
	ld, _, _, _, _, rootKey, _, fps, _ := qualifiedLedger(t)
	pos := rawAppendAt(t, ld, rootKey, version.Seed2, "actor.qualified", fps["workerA"],
		`{"capability": "claim", "tuple": `+drillTuple(nil)+`, "contract": "eval-1", "verdict": "3"}`)
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" ||
		!strings.Contains(e.Error.Message, "bad_actor_event") || !strings.Contains(e.Error.Message, fmt.Sprintf("position %d", pos)) || !strings.Contains(e.Error.Message, version.Seed3) {
		t.Fatalf("a raw actor.qualified at a seed/2 position fails verification at its position as bad_actor_event naming %s: %d %+v", version.Seed3, code, e.Error)
	}

	ld3, priv3, _, fps3 := evalLedger(t)
	first := rootAppend(t, ld3, priv3, "actor.granted", fps3["workerA"], `{"capability": "claim", "tuple": `+drillTuple(nil)+`}`)
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld3); code != 0 {
		t.Fatalf("this build verifies the seed/3 chain: %d %+v", code, e)
	}
	e, code := runEnv(t, "ledger", "verify", "--ledger", ld3, "--supported", version.Protocol+","+version.Seed1+","+version.Seed2)
	if code == 0 || e.Error == nil || e.Error.Code != "version_unsupported" || e.Position == nil || *e.Position != fmt.Sprintf("%d", first) {
		t.Fatalf("a seed/2-only build refuses at the first seed/3 record by version: %d %+v", code, e)
	}
	if strings.Contains(strings.ToLower(e.Error.Message), "qualif") || !strings.Contains(e.Error.Message, version.Seed3) {
		t.Fatalf("the refusal names the version, never a qualification: %s", e.Error.Message)
	}
}

// conformance: plans/os-03e47abb.md AC7 (seed maintain run appends
// none of the new verbs) and AC8 (D10: a judged eval left in review
// files no defect through the maintenance pass and owes nothing in the
// situation read), drilled beside the ordinary contract the same pass
// DOES file unreconciled and the same read DOES report owed.
func TestJudgedEvalOwesNothingAndMaintenanceFilesNoDefect(t *testing.T) {
	repo, anchor := evalRepo(t)
	ld, src, base, specCommit, head0, priv, _, keys, fps := offerLedger(t)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	mPath, mPub, mFP := writeWorkerKey(t, 31)
	rootAppend(t, ld, priv, "actor.enrolled", mFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "maintenance"}`, mPub))
	rootAppend(t, ld, priv, "actor.granted", mFP, `{"capability": "maintenance"}`)
	rootAppend(t, ld, priv, "actor.granted", mFP, `{"capability": "operator"}`)
	declare := []string{"--principal", "acme", "--model", "fable/5.1", "--tool-policy", "default"}

	// The eval: filed, offered, worked, judged pass, left in review.
	e, code := runEnv(t, "eval", "file", "--ledger", ld, "--repo", repo, "--key", priv, "--eval", "fix-the-check")
	if code != 0 {
		t.Fatalf("eval file: %d %+v", code, e.Error)
	}
	subject, _ := e.Result["subject"].(string)
	if e, code := runEnv(t, "eval", "act", "--ledger", ld, "--repo", repo, "--key", keys["supervisor"]); code != 0 {
		t.Fatalf("offer: %d %+v", code, e.Error)
	}
	fence, _ := openWindow(t, ld, 22, keys["workerA"], subject)
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", subject}, declare...)...); code != 0 {
		t.Fatalf("run start: %d %+v", code, e.Error)
	}
	head := solve(t, repo, anchor, "fix-the-check", true)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"], "--verb", "submission.made", "--subject", subject, "--payload",
		fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["the check is green"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, anchor+".."+head)); code != 0 {
		t.Fatalf("submission: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", subject, "--repo", repo, "--key", keys["verifier"], "--verdict", "pass"); code != 0 {
		t.Fatalf("pass: %d %+v", code, e.Error)
	}

	// An ordinary contract beside it, judged pass and left in review:
	// the pre-change behavior, which stays.
	offerFile(t, ld, priv, specCommit, "c-ord")
	fenceOrd, _ := openWindow(t, ld, 23, keys["workerB"], "c-ord")
	if e, code := runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-ord"}, declare...)...); code != 0 {
		t.Fatalf("run start on the ordinary contract: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerB"], "--verb", "submission.made", "--subject", "c-ord", "--payload",
		fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["accept.md @ %s"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fenceOrd, specCommit, base+".."+head0)); code != 0 {
		t.Fatalf("ordinary submission: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-ord", "--repo", src, "--key", keys["verifier"], "--verdict", "pass"); code != 0 {
		t.Fatalf("ordinary pass: %d %+v", code, e.Error)
	}

	// The situation read: the operator owes a merge on the ordinary
	// contract and nothing on the eval.
	_, sit, code := situationOf(t, "--ledger", ld, "--key", priv)
	if code != 0 {
		t.Fatalf("situation: %d", code)
	}
	owedOrd := false
	for _, row := range sit.Obligations {
		if row["subject"] == subject {
			t.Fatalf("a judged eval owes nothing: %+v", row)
		}
		if row["subject"] == "c-ord" && row["kind"] == "verdict.unmerged" {
			owedOrd = true
		}
	}
	if !owedOrd {
		t.Fatalf("the ordinary pass still owes its merge: %+v", sit.Obligations)
	}

	// The maintenance pass files the ordinary contract's unreconciled
	// finding and nothing for the eval, and appends none of the
	// qualification verbs: the mint stays the supervisor's.
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	before := st.count
	e, code = runEnv(t, "maintain", "run", "--ledger", ld, "--repo", repo, "--key", mPath, "--obs", t.TempDir(),
		"--artifacts", filepath.Join(repo, "var", "artifacts"), "--as-of", "2026-09-01T12:00:00Z")
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e.Error)
	}
	rep := report(t, e)
	filedOrd := false
	for _, f := range rep.Filed {
		if f.Subject == subject {
			t.Fatalf("a judged eval files no defect: %+v", f)
		}
		if f.Subject == "c-ord" && f.Class == "unreconciled" {
			filedOrd = true
		}
	}
	for _, f := range rep.Findings {
		if f.Subject == subject && f.Class == "unreconciled" {
			t.Fatalf("a judged eval is never unreconciled: %+v", f)
		}
	}
	if !filedOrd {
		t.Fatalf("the ordinary pass with no merge is filed unreconciled, as before: %+v", rep.Filed)
	}
	st, _ = loadVerdictState(ld)
	for _, r := range st.records[before:] {
		if r.Event.Verb == "actor.qualified" || r.Event.Verb == "actor.disqualified" {
			t.Fatalf("seed maintain run appends none of the qualification verbs: %s at %s", r.Event.Verb, r.Event.Subject)
		}
	}
	if owed, _ := dueActs(t, ld, repo); kinds(owed) != "mint:"+fps["workerA"] {
		t.Fatalf("the mint is still the supervisor's to perform: %s", kinds(owed))
	}
}
