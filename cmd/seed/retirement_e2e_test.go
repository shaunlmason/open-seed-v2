package main

// The end-to-end drill (plans/os-0d537fbd.md AC6) in the small-team
// fixture: a promotion surfaces at the next claim; the revert is
// observed and the claim after carries nothing; a revalidation moves
// the stamps and surfaces again, at the instants the claims declare;
// a dead end is retired, the listing drops it, un-retired, the listing
// shows it.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestSmallTeamRetirementAndRevalidationAtClaimTime(t *testing.T) {
	m := buildMode(t, append(append([]identity{}, smallTeam...),
		identity{lane: "implementer", actor: "impl2", seed: 54},
		identity{lane: "curator", actor: "curator", seed: 55},
		identity{lane: "dispatcher", actor: "dispatch", seed: 53}))
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	gitSrc := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", m.src, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	copyTree(t, filepath.Join("..", "..", "evals"), filepath.Join(m.src, eval.Root))
	gitSrc("add", ".")
	gitSrc("commit", "--quiet", "-m", "evals: the shipped definitions (#1)")
	anchor := gitSrc("rev-parse", "HEAD")

	deadEnd := func(actor, subject string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"knowledge", "deadend"}, m.posture()...), "--key", m.keys[actor], "--subject", subject,
			"--tried", "retrying the fetch", "--outcome", "the mirror timed out", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")...)
	}
	work := func(actor string) string {
		t.Helper()
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys[actor],
			loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
				if e, code := deadEnd(actor, s); code != 0 {
					return 0, fmt.Errorf("the holder's dead end: %d %+v", code, e.Error)
				}
				return 2, nil
			}), loop.WithBase(m.base+".."+m.head))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil || step.Outcome != loop.Submitted {
			t.Fatalf("the loop claims, works and submits: %+v %v", step, err)
		}
		return step.Subject
	}
	m.contract(t, "c-a", "supervisor")
	if got := work("impl"); got != "c-a" {
		t.Fatalf("impl took c-a: %s", got)
	}
	m.contract(t, "c-b", "supervisor")
	if got := work("impl2"); got != "c-b" {
		t.Fatalf("impl2 took c-b: %s", got)
	}
	show := func() map[string]any {
		t.Helper()
		e, code := runEnv(t, append([]string{"knowledge", "show"}, m.posture()...)...)
		if code != 0 {
			t.Fatalf("knowledge show: %d %+v", code, e)
		}
		return e.Result
	}
	firstDeadEnd := func(view map[string]any, contract string) int {
		t.Helper()
		ends, _ := view["dead_ends"].(map[string]any)
		list, _ := ends[contract].([]any)
		if len(list) == 0 {
			t.Fatalf("no dead end on %s: %+v", contract, view)
		}
		pos, _ := list[0].(map[string]any)["position"].(float64)
		return int(pos)
	}
	view := show()
	pA, pB := firstDeadEnd(view, "c-a"), firstDeadEnd(view, "c-b")

	claim := "record the mirror's temperature before retrying the fetch"
	id := curation.HypothesisID(claim, nil)
	e, code := runEnv(t, append(append([]string{"knowledge", "propose"}, m.posture()...), "--key", m.keys["curator"],
		"--claim", claim, "--applies-when", `{"routing": "core"}`, "--support", fmt.Sprintf("c-a@%d", pA), "--support", fmt.Sprintf("c-b@%d", pB),
		"--provenance", "plans/os-0d537fbd.md @ "+anchor)...)
	if code != 0 || e.Result["hypothesis"] != id {
		t.Fatalf("the curator proposes: %d %+v", code, e)
	}
	view = show()
	hyps, _ := view["hypotheses"].([]any)
	hposF, _ := hyps[0].(map[string]any)["position"].(float64)
	cited := fmt.Sprintf("%s@%d", id, int(hposF))

	// The lesson lands on main; the stamps are the reviewed file's.
	lessonPath := filepath.Join(m.src, curation.LessonsDir, "mirror.md")
	commitLesson := func(lastValidated, expires, msg string) (string, string) {
		t.Helper()
		body := "---\nhypothesis: " + cited + "\napplies-when: {\"routing\": \"core\"}\nsupport: " + fmt.Sprintf("c-a@%d, c-b@%d", pA, pB) +
			"\nprovenance: plans/os-0d537fbd.md @ " + anchor + "\nlast-validated: " + lastValidated + "\nexpires: " + expires + "\ncarrier: role\n---\n\n# Record the mirror's temperature\n"
		if err := os.MkdirAll(filepath.Dir(lessonPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lessonPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitSrc("add", ".")
		gitSrc("commit", "--quiet", "-m", msg)
		commit := gitSrc("rev-parse", "HEAD")
		return curation.LessonsDir + "/mirror.md @ " + commit, commit
	}
	carrier, carrierCommit := commitLesson("2026-09-01T00:00:00Z", "2026-12-01T00:00:00Z", "knowledge: the mirror lesson (#2)")

	// The bound eval through the production machinery: filed, offered,
	// worked from the carrier, passed by the disjoint verifier.
	clone := filepath.Join(t.TempDir(), "worker")
	if out, err := exec.Command("git", "clone", "--quiet", m.src, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	hardenGitRepo(t, clone)
	gitClone := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", clone, "-c", "user.name=impl", "-c", "user.email=impl@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	branches := 0
	survived := func(carrier, base string) int {
		t.Helper()
		e, code := runEnv(t, append(append([]string{"eval", "file"}, m.posture()...), "--repo", m.src, "--key", m.keys["dispatch"],
			"--eval", "fix-the-check", "--for-lesson", cited, "--carrier", carrier)...)
		if code != 0 {
			t.Fatalf("the dispatcher files the bound eval: %d %+v", code, e.Error)
		}
		subject, _ := e.Result["subject"].(string)
		if e, code := runEnv(t, append(append([]string{"eval", "act"}, m.posture()...), "--repo", m.src, "--key", m.keys["supervisor"])...); code != 0 {
			t.Fatalf("the supervisor's act: %d %+v", code, e.Error)
		}
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl2"],
			loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
				branches++
				branch := fmt.Sprintf("solve-%d", branches)
				gitClone("fetch", "--quiet", "origin")
				gitClone("checkout", "--quiet", "-B", branch, base)
				b, err := os.ReadFile(filepath.Join(clone, eval.Root, "fix-the-check", "solution", "greet.sh"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(clone, eval.Root, "fix-the-check", "fixture", "greet.sh"), b, 0o644); err != nil {
					t.Fatal(err)
				}
				gitClone("add", ".")
				gitClone("commit", "--quiet", "-m", "the work")
				gitClone("push", "--quiet", "origin", branch)
				return 2, nil
			}), loop.WithRepo(clone))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil || step.Outcome != loop.Submitted || step.Subject != subject {
			t.Fatalf("impl2 works the bound eval: %+v %v", step, err)
		}
		if e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
			"--subject", subject, "--repo", m.src, "--key", m.keys["verify"], "--verdict", "pass")...); code != 0 {
			t.Fatalf("the disjoint verifier passes the solved eval under the candidate: %d %+v", code, e.Error)
		}
		st, failEnv := loadVerdictState(m.materialize(t))
		if failEnv != nil {
			t.Fatalf("the remote chain must verify: %+v", failEnv)
		}
		evalState, _ := st.fold.State(subject)
		if evalState.Verdict == nil {
			t.Fatal("the pass folded")
		}
		return evalState.Verdict.Pos
	}
	promote := func(carrier, pr string, pass int) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"knowledge", "promote"}, m.posture()...), "--key", m.keys["observer"],
			"--lesson", carrier, "--hypothesis", cited, "--pr", pr, "--repo", m.src, "--carrier", "role",
			"--adversarial", fmt.Sprintf("fix-the-check@%d", pass))...)
	}
	if e, code := promote(carrier, "pr/2 @ "+carrierCommit, survived(carrier, carrierCommit)); code != 0 || e.Result["subject"] != id {
		t.Fatalf("the observer promotes citing the survived eval: %d %+v", code, e)
	}
	contracts := 0
	claimLessons := func(actor, now string) []any {
		t.Helper()
		contracts++
		subject := fmt.Sprintf("c-%d", contracts)
		m.contract(t, subject, "supervisor")
		args := append(append([]string{"claim", "take"}, m.posture()...), "--key", m.keys[actor], "--subject", subject, "--repo", m.src)
		if now != "" {
			args = append(args, "--now", now)
		}
		e, code := runEnv(t, args...)
		if code != 0 {
			t.Fatalf("claim take on %s: %d %+v", subject, code, e.Error)
		}
		lessons, _ := e.Result["lessons"].([]any)
		return lessons
	}
	// The promotion surfaces at the next claim, at an instant before
	// its expiry; at one past it, nothing.
	if lessons := claimLessons("impl", "2026-10-01T00:00:00Z"); len(lessons) != 1 || lessons[0].(map[string]any)["lesson"] != carrier {
		t.Fatalf("the claim before the expiry receives the promoted lesson: %+v", lessons)
	}
	if lessons := claimLessons("impl2", "2026-12-01T00:00:00Z"); len(lessons) != 0 {
		t.Fatalf("the claim at the expiry receives nothing: %+v", lessons)
	}

	// The regression: the lesson PR reverted, one command because it
	// was a PR; the observer records the revert, and the claim after
	// carries nothing at any instant.
	gitSrc("revert", "--no-edit", carrierCommit)
	revert := gitSrc("rev-parse", "HEAD")
	e, code = runEnv(t, append(append([]string{"knowledge", "retire"}, m.posture()...), "--key", m.keys["observer"],
		"--lesson", carrier, "--hypothesis", cited, "--reason", "regression", "--pr", "pr/3 @ "+revert)...)
	if code != 0 || e.Result["subject"] != id {
		t.Fatalf("the observer observes the revert: %d %+v", code, e)
	}
	if lessons := claimLessons("impl", "2026-10-01T00:00:00Z"); len(lessons) != 0 {
		t.Fatalf("a retired lesson reaches no claim: %+v", lessons)
	}
	if stages, _ := show()["stages"].(map[string]any); stages["retired"] != 1.0 || stages["lessons"] != 1.0 || stages["observations"] != 2.0 {
		t.Fatalf("the evidence stays: %+v", stages)
	}

	// Revalidation: the file back on main with the stamps moved, a
	// fresh survival at the new anchor, and the observer's new
	// promotion; the claim after carries it again, until its own
	// expiry.
	carrier2, carrier2Commit := commitLesson("2026-12-15T00:00:00Z", "2027-06-01T00:00:00Z", "knowledge: the mirror lesson revalidated (#4)")
	if e, code := promote(carrier2, "pr/4 @ "+carrier2Commit, survived(carrier2, carrier2Commit)); code != 0 {
		t.Fatalf("the revalidation promotes: %d %+v", code, e.Error)
	}
	if lessons := claimLessons("impl2", "2027-01-01T00:00:00Z"); len(lessons) != 1 || lessons[0].(map[string]any)["lesson"] != carrier2 {
		t.Fatalf("the revalidated lesson surfaces at its new anchor: %+v", lessons)
	}
	if lessons := claimLessons("impl", "2027-06-01T00:00:00Z"); len(lessons) != 0 {
		t.Fatalf("the revalidated lesson expires at its own stamp: %+v", lessons)
	}
	if stages, _ := show()["stages"].(map[string]any); stages["retired"] != nil || stages["lessons"] != 1.0 {
		t.Fatalf("the new promotion is the path's one lesson and clears the retirement: %+v", stages)
	}

	// A dead end retired and un-retired, the listing changing: impl's
	// dead end on its first claimed contract is held-out evidence.
	if e, code := deadEnd("impl", "c-1"); code != 0 {
		t.Fatalf("the held-out dead end: %d %+v", code, e.Error)
	}
	p1 := firstDeadEnd(show(), "c-1")
	listing := func() (held []any, row map[string]any) {
		t.Helper()
		e, code := runEnv(t, append(append([]string{"knowledge", "validate"}, m.posture()...), "--hypothesis", cited, "--environment", "ci-runner/v0")...)
		if code != 0 {
			t.Fatalf("knowledge validate: %d %+v", code, e)
		}
		held, _ = e.Result["held_out"].([]any)
		rows, _ := e.Result["dead_ends"].([]any)
		for _, r := range rows {
			if mm, _ := r.(map[string]any); mm["contract"] == "c-1" {
				row = mm
			}
		}
		return held, row
	}
	held, row := listing()
	if len(held) != 1 || row["applies"] != true || row["retired"] != false {
		t.Fatalf("the dead end is held out and applies in its environment: %+v %+v", held, row)
	}
	act := func(name, environment string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"knowledge", "deadend", name}, m.posture()...), "--key", m.keys["curator"],
			"--deadend", fmt.Sprintf("c-1@%d", p1), "--environment", environment, "--reason", "the runner image moved")...)
	}
	if e, code := act("retire", "ci-runner/v1"); code != 0 {
		t.Fatalf("the curator retires the dead end: %d %+v", code, e.Error)
	}
	if held, row := listing(); len(held) != 0 || row["retired"] != true || row["applies"] != false {
		t.Fatalf("the listing drops the retired dead end: %+v %+v", held, row)
	}
	if e, code := act("unretire", "ci-runner/v2"); code != 0 {
		t.Fatalf("the curator un-retires it: %d %+v", code, e.Error)
	}
	if held, row := listing(); len(held) != 1 || row["retired"] != false || row["applies"] != true {
		t.Fatalf("the listing shows it again: %+v %+v", held, row)
	}
}
