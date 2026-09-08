package main

// The knowledge verbs end to end (plans/os-f30ee0d3.md AC7;
// plans/os-96850e5a.md AC5, AC6): the seven subverbs against a real
// ledger with the fence and the hypothesis id derived, refusing at
// usage what the boundary would refuse; the promotion citing a bound
// eval's pass; the lint's file half; the delivery at claim time, in
// the situation read and in the provisioned handoff; reconcile's
// lesson_unverified; the projection and the report carrying the
// stages.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// lessonRepo is a repository holding a plan (the provenance anchor)
// and one lesson file committed at an anchor, returning the repo, the
// plan commit and the lesson commit.
func lessonRepo(t *testing.T, id string, hpos int, support []string) (repo, planCommit, lessonCommit, body string) {
	t.Helper()
	repo = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	for _, d := range []string{"plans", curation.LessonsDir} {
		if err := os.MkdirAll(filepath.Join(repo, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "plans", "x.md"), []byte("plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "plan")
	planCommit = git("rev-parse", "HEAD")
	body = "---\nhypothesis: " + id + "@" + fmt.Sprint(hpos) + "\napplies-when: {\"routing\": \"core\"}\nsupport: " + strings.Join(support, ", ") + "\nprovenance: plans/x.md @ " + planCommit + "\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: knowledge\n---\n\n# Retry when cold\n\n## Claim\n\nRetry the fetch once when the mirror is cold.\n\n## Evidence\n\nThe support set.\n\n## Applies when\n\nRouting core.\n"
	if err := os.WriteFile(filepath.Join(repo, curation.LessonsDir, "retry-when-cold.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "lesson")
	lessonCommit = git("rev-parse", "HEAD")
	return
}

func TestKnowledgeVerbsDriveTheStages(t *testing.T) {
	ld, src, _, _, _, priv, rootKey, keys, _ := offerLedger(t)
	for _, to := range []string{version.Seed2, version.Seed3} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+to+`"}`); code != 0 {
			t.Fatalf("upgrade to %s: %d %+v", to, code, e)
		}
	}
	v := version.Seed3
	curatorKey, curatorPub, curatorFP := writeWorkerKey(t, 26)
	observerKey, observerPub, observerFP := writeWorkerKey(t, 27)
	for _, step := range [][]string{
		{"actor.enrolled", curatorFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "curator"}`, curatorPub)},
		{"actor.granted", curatorFP, `{"capability": "curate"}`},
		{"actor.enrolled", observerFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "observer"}`, observerPub)},
		{"actor.granted", observerFP, `{"capability": "observer"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	open := func(subject string) {
		rawAppendAt(t, ld, rootKey, v, "intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
		rawAppendAt(t, ld, rootKey, v, "contract.specified", subject, `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`)
	}
	open("c-1")
	open("c-2")
	// workerA holds c-1, workerB holds c-2: two holders in the family.
	rawAppendAt(t, ld, workerRawKey(22), v, "claim.taken", "c-1", `{}`)
	deadend := func(key, subject string) (ledgerEnv, int) {
		return runEnv(t, "knowledge", "deadend", "--ledger", ld, "--key", key, "--subject", subject,
			"--tried", "retrying the fetch", "--outcome", "the mirror timed out", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")
	}
	if e, code := deadend(keys["workerA"], "c-1"); code != 0 || e.Result["subject"] != "c-1" {
		t.Fatalf("the holder records a dead end inside its window, the fence derived: %d %+v", code, e)
	}
	if e, code := deadend(keys["workerA"], "c-2"); code != 3 || e.Error == nil || e.Error.Code != "invalid_transition" {
		t.Fatalf("no window on c-2 refuses before anything is signed: %d %+v", code, e.Error)
	}
	if e, code := deadend(keys["workerB"], "c-1"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "holder") {
		t.Fatalf("a non-holder's dead end refuses naming the holder: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "knowledge", "deadend", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1",
		"--tried", "x", "--outcome", "y", "--condition", "z"); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "--environment") {
		t.Fatalf("a missing field refuses at usage naming it: %d %+v", code, e.Error)
	}
	rawAppendAt(t, ld, workerRawKey(23), v, "claim.taken", "c-2", `{}`)
	if e, code := deadend(keys["workerB"], "c-2"); code != 0 {
		t.Fatalf("the second dead end: %d %+v", code, e)
	}
	// A third dead end on c-1, the held-out evidence.
	if e, code := deadend(keys["workerA"], "c-1"); code != 0 {
		t.Fatalf("the third dead end: %d %+v", code, e)
	}

	show := func() map[string]any {
		t.Helper()
		e, code := runEnv(t, "knowledge", "show", "--ledger", ld)
		if code != 0 {
			t.Fatalf("knowledge show: %d %+v", code, e)
		}
		return e.Result
	}
	positionsOf := func(view map[string]any, contract string) []int {
		t.Helper()
		ends, _ := view["dead_ends"].(map[string]any)
		list, _ := ends[contract].([]any)
		var out []int
		for _, d := range list {
			m, _ := d.(map[string]any)
			pos, _ := m["position"].(float64)
			out = append(out, int(pos))
		}
		return out
	}
	view := show()
	c1, c2 := positionsOf(view, "c-1"), positionsOf(view, "c-2")
	if len(c1) != 2 || len(c2) != 1 {
		t.Fatalf("three dead ends stand: %+v", view["dead_ends"])
	}
	p1, p1b, p2 := c1[0], c1[1], c2[0]
	if stages, _ := view["stages"].(map[string]any); stages["observations"] != 3.0 {
		t.Fatalf("three observations stand: %+v", view["stages"])
	}

	claim := "retry the fetch once when the mirror is cold"
	propose := func(key, applies string, support ...string) (ledgerEnv, int) {
		args := []string{"knowledge", "propose", "--ledger", ld, "--key", key, "--claim", claim, "--applies-when", applies,
			"--provenance", "plans/os-f30ee0d3.md @ 0123456", "--exception", "a warm mirror"}
		for _, s := range support {
			args = append(args, "--support", s)
		}
		return runEnv(t, args...)
	}
	if e, code := propose(keys["workerA"], `{"routing": "core"}`, fmt.Sprintf("c-1@%d", p1)); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "2 --support") {
		t.Fatalf("one citation refuses at usage naming the floor: %d %+v", code, e.Error)
	}
	if e, code := propose(curatorKey, `flaky`, fmt.Sprintf("c-1@%d", p1), fmt.Sprintf("c-2@%d", p2)); code != 64 {
		t.Fatalf("a non-object predicate refuses at usage: %d %+v", code, e.Error)
	}
	if e, code := propose(curatorKey, `{}`, fmt.Sprintf("c-1@%d", p1), fmt.Sprintf("c-2@%d", p2)); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "applies_when") {
		t.Fatalf("an empty predicate refuses at usage naming the part: %d %+v", code, e.Error)
	}
	if e, code := propose(keys["workerA"], `{"routing": "core"}`, fmt.Sprintf("c-1@%d", p1), fmt.Sprintf("c-2@%d", p2)); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim key cannot propose: %d %+v", code, e.Error)
	}
	id := curation.HypothesisID(claim, []string{"a warm mirror"})
	e, code := propose(curatorKey, `{"routing": "core"}`, fmt.Sprintf("c-1@%d", p1), fmt.Sprintf("c-2@%d", p2))
	if code != 0 || e.Result["hypothesis"] != id {
		t.Fatalf("the curator proposes on the derived subject: %d %+v", code, e)
	}
	if e, code := propose(curatorKey, `{"routing": "core"}`, fmt.Sprintf("c-1@%d", p1), fmt.Sprintf("c-2@%d", p2)); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "proposed at position") {
		t.Fatalf("a re-proposal refuses as a duplicate: %d %+v", code, e.Error)
	}
	view = show()
	hyps, _ := view["hypotheses"].([]any)
	if len(hyps) != 1 {
		t.Fatalf("one hypothesis stands: %+v", view)
	}
	hyp, _ := hyps[0].(map[string]any)
	hposF, _ := hyp["position"].(float64)
	hpos := int(hposF)
	if hyp["id"] != id || hyp["stage"] != "proposed" {
		t.Fatalf("the hypothesis is proposed: %+v", hyp)
	}
	cited := fmt.Sprintf("%s@%d", id, hpos)

	// The held-out listing: the third dead end on c-1, never the
	// support set.
	e, code = runEnv(t, "knowledge", "validate", "--ledger", ld, "--hypothesis", cited)
	if code != 0 {
		t.Fatalf("knowledge validate: %d %+v", code, e)
	}
	heldOut, _ := e.Result["held_out"].([]any)
	if len(heldOut) != 1 || heldOut[0].(map[string]any)["position"] != fmt.Sprint(p1b) {
		t.Fatalf("validate lists the held-out observations and nothing from the support set: %+v", e.Result)
	}

	// The lesson file, the bound eval and its pass, then the promotion.
	support := []string{fmt.Sprintf("c-1@%d", p1), fmt.Sprintf("c-2@%d", p2)}
	repo, _, lessonCommit, body := lessonRepo(t, id, hpos, support)
	anchor := curation.LessonsDir + "/retry-when-cold.md @ " + lessonCommit
	rawAppendAt(t, ld, rootKey, v, "intent.filed", "eval-1", fmt.Sprintf(`{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check", "lesson": %q, "carrier": %q}}`, cited, anchor))
	rawAppendAt(t, ld, rootKey, v, "contract.specified", "eval-1", `{"acceptance": {"ref": "evals/fix-the-check/fixture/accept.md @ 0123456", "executable": true, "gate": "pr/1 @ 0123456"}}`)
	fence := rawAppendAt(t, ld, workerRawKey(23), v, "claim.taken", "eval-1", `{}`)
	sub := rawAppendAt(t, ld, workerRawKey(23), v, "submission.made", "eval-1", fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["eval-1 ok"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}}`, fence))
	pass := rawAppendAt(t, ld, workerRawKey(24), v, "verdict.rendered", "eval-1", fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("0", 64), sub))

	promote := func(key, lesson, hypothesis string, extra ...string) (ledgerEnv, int) {
		args := []string{"knowledge", "promote", "--ledger", ld, "--key", key, "--lesson", lesson, "--hypothesis", hypothesis, "--pr", "pr/7 @ " + lessonCommit,
			"--repo", repo, "--carrier", "knowledge", "--adversarial", fmt.Sprintf("fix-the-check@%d", pass),
			"--last-validated", "2026-09-01T00:00:00Z", "--expires", "2026-12-01T00:00:00Z"}
		return runEnv(t, append(args, extra...)...)
	}
	if e, code := promote(observerKey, curation.LessonsDir+"/retry-when-cold.md", cited); code != 64 || e.Error == nil {
		t.Fatalf("a bare lesson path refuses at usage: %d %+v", code, e.Error)
	}
	if e, code := promote(observerKey, curation.LessonsDir+"/missing.md @ "+lessonCommit, cited); code != 4 {
		t.Fatalf("a lesson absent at its anchor refuses not_found, the digest being read from the file: %d %+v", code, e.Error)
	}
	if e, code := promote(observerKey, anchor, fmt.Sprintf("%s@%d", id, hpos+1)); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "admitted hypothesis") {
		t.Fatalf("citing a position that is no hypothesis refuses: %d %+v", code, e.Error)
	}
	// The stamps are the reviewed file's: a flag that disagrees refuses
	// at usage naming both, and the promotion needs no stamp flag at
	// all (review finding on the item 3 PR).
	if e, code := promote(observerKey, anchor, cited, "--expires", "2027-06-01T00:00:00Z"); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "disagrees with the reviewed file") {
		t.Fatalf("a stamp flag that disagrees with the frontmatter refuses at usage: %d %+v", code, e.Error)
	}
	promoteBare := func(key, lesson, hypothesis string) (ledgerEnv, int) {
		return runEnv(t, "knowledge", "promote", "--ledger", ld, "--key", key, "--lesson", lesson, "--hypothesis", hypothesis, "--pr", "pr/7 @ "+lessonCommit,
			"--repo", repo, "--carrier", "knowledge", "--adversarial", fmt.Sprintf("fix-the-check@%d", pass))
	}
	if e, code := promote(curatorKey, anchor, cited); code != 14 {
		t.Fatalf("the curator cannot promote: %d %+v", code, e.Error)
	}
	if e, code := promoteBare(observerKey, anchor, cited); code != 0 || e.Result["subject"] != id {
		t.Fatalf("the observer promotes the admitted hypothesis citing the survived eval: %d %+v", code, e)
	}
	view = show()
	stages, _ := view["stages"].(map[string]any)
	if stages["observations"] != 3.0 || stages["hypotheses"] != 1.0 || stages["promoted"] != 1.0 || stages["lessons"] != 1.0 || stages["unbound"] != 0.0 || stages["contested"] != 0.0 {
		t.Fatalf("the stages count: %+v", stages)
	}

	// The lint's file half: the shipped file agrees with the fact, the
	// hypothesis and the repository; a stale instant refuses.
	file := filepath.Join(repo, curation.LessonsDir, "retry-when-cold.md")
	if e, code := runEnv(t, "knowledge", "lint", "--ledger", ld, "--repo", repo, "--now", "2026-10-01T00:00:00Z", file); code != 0 || e.Result["lint"] != "ok" {
		t.Fatalf("the lesson lints against its fact: %d %+v", code, e)
	}
	if e, code := runEnv(t, "knowledge", "lint", "--ledger", ld, "--repo", repo, "--now", "2027-01-01T00:00:00Z", file); code != 20 || e.Error == nil || e.Error.Code != "lint_refused" || !strings.Contains(e.Error.Message, "lint.stamps") {
		t.Fatalf("an expired lesson refuses at the stamps gate: %d %+v", code, e.Error)
	}
	edited := filepath.Join(t.TempDir(), "retry-when-cold.md")
	if err := os.WriteFile(edited, []byte(strings.Replace(body, "carrier: knowledge", "carrier: role", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "knowledge", "lint", "--ledger", ld, "--repo", repo, "--now", "2026-10-01T00:00:00Z", edited); code != 4 {
		t.Fatalf("a file outside the store cites no promotion: %d %+v", code, e.Error)
	}

	// Delivery: the situation read for the holder of a matching
	// contract lists the lesson with --repo and reports the count
	// without; a non-matching contract gets nothing.
	open("c-3")
	rawAppendAt(t, ld, workerRawKey(22), v, "claim.taken", "c-3", `{}`)
	rawAppendAt(t, ld, rootKey, v, "intent.filed", "c-4", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "other"}`)
	rawAppendAt(t, ld, rootKey, v, "contract.specified", "c-4", `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`)
	rawAppendAt(t, ld, workerRawKey(23), v, "claim.taken", "c-4", `{}`)
	windowsOf := func(e ledgerEnv) map[string]int {
		out := map[string]int{}
		ws, _ := e.Result["windows"].([]any)
		for _, w := range ws {
			m, _ := w.(map[string]any)
			s, _ := m["subject"].(string)
			ls, has := m["lessons"].([]any)
			if has {
				out[s] = len(ls)
			} else {
				out[s] = -1
			}
		}
		return out
	}
	e, code = runEnv(t, "situation", "--ledger", ld, "--key", keys["workerA"], "--repo", repo)
	if code != 0 || windowsOf(e)["c-3"] != 1 || windowsOf(e)["c-1"] != 1 {
		t.Fatalf("the situation read lists the lesson for the held matching subjects: %d %+v", code, e.Result["windows"])
	}
	e, code = runEnv(t, "situation", "--ledger", ld, "--key", keys["workerB"], "--repo", repo)
	if code != 0 || windowsOf(e)["c-4"] != 0 || windowsOf(e)["c-2"] != 1 {
		t.Fatalf("a non-matching held subject gets nothing: %d %+v", code, e.Result["windows"])
	}
	e, code = runEnv(t, "situation", "--ledger", ld, "--key", keys["workerA"])
	if code != 0 || windowsOf(e)["c-3"] != -1 || e.Result["lessons_unverified"] == nil {
		t.Fatalf("without --repo the read reports the count unverified and no rows: %d %+v", code, e.Result)
	}

	// Reconcile: the fact resolves; a fact whose anchor is no ancestor
	// is lesson_unverified.
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", repo)
	if code != 0 || classesOf(t, e)["lesson_unverified"] != 0 {
		t.Fatalf("a resolving promotion classifies nothing: %d %+v", code, e.Result["by_class"])
	}
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", src)
	if code != 0 || classesOf(t, e)["lesson_unverified"] != 1 {
		t.Fatalf("against a repository that never merged the promotion, the fact is lesson_unverified: %d %+v", code, e.Result["by_class"])
	}

	// A contest removes the lesson from every delivery and closes the
	// promotion, leaving the file and the facts.
	e, code = runEnv(t, "knowledge", "contest", "--ledger", ld, "--key", curatorKey, "--hypothesis", cited, "--evidence", fmt.Sprintf("c-1@%d", p1), "--reason", "no")
	if code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "contest.held_out") {
		t.Fatalf("a contest citing the support set refuses at its gate: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, "knowledge", "contest", "--ledger", ld, "--key", curatorKey, "--hypothesis", cited, "--evidence", fmt.Sprintf("c-1@%d", p1b), "--reason", "the mirror was warm and it still failed")
	if code != 0 || e.Result["hypothesis"] != cited {
		t.Fatalf("the curator contests with held-out evidence: %d %+v", code, e)
	}
	view = show()
	if stages, _ := view["stages"].(map[string]any); stages["contested"] != 1.0 || stages["lessons"] != 1.0 {
		t.Fatalf("a contest moves the hypothesis and keeps the lesson fact: %+v", stages)
	}
	e, code = runEnv(t, "situation", "--ledger", ld, "--key", keys["workerA"], "--repo", repo)
	if code != 0 || windowsOf(e)["c-3"] != 0 {
		t.Fatalf("a contested hypothesis's lesson surfaces nowhere: %+v", e.Result["windows"])
	}

	// The projection publishes the same view and the report counts.
	out := filepath.Join(t.TempDir(), "out")
	unlockForCleanup(t, out)
	if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out); code != 0 {
		t.Fatalf("project rebuild: %d %+v", code, e)
	}
	for _, name := range []string{"knowledge", "report"} {
		cur, code := runEnv(t, "project", "current", "--out", out, "--name", name)
		if code != 0 {
			t.Fatalf("project current %s: %d %+v", name, code, cur)
		}
		b, err := os.ReadFile(filepath.Join(cur.Result["path"].(string), name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"contested": 1`) || !strings.Contains(string(b), `"lessons": 1`) {
			t.Fatalf("%s carries the stage counts: %s", name, b)
		}
	}
}
