package main

// conformance: plans/os-96850e5a.md AC7 — in small-team mode two
// workers record dead ends on two contracts, the curator proposes, an
// eval filed for that hypothesis and candidate revision passes only
// once the candidate is applied, the observer promotes citing it, the
// next claim on a matching contract receives the lesson in its
// envelope and in its provisioned lessons.json, and a contest removes
// it from the next claim after that.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/executor"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestSmallTeamPromotionDeliversLessonsAtClaimTime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows git refuses the solve branch's checkout with the lesson mirror reported as locally modified; recorded as an open Windows residual in spec/platform.md")
	}
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

	// Two workers, two contracts, a dead end each inside its window.
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

	// The curator proposes over both holders' observations.
	claim := "record the mirror's temperature before retrying the fetch"
	id := curation.HypothesisID(claim, nil)
	e, code := runEnv(t, append(append([]string{"knowledge", "propose"}, m.posture()...), "--key", m.keys["curator"],
		"--claim", claim, "--applies-when", `{"routing": "core"}`, "--support", fmt.Sprintf("c-a@%d", pA), "--support", fmt.Sprintf("c-b@%d", pB),
		"--provenance", "plans/os-96850e5a.md @ "+anchor)...)
	if code != 0 || e.Result["hypothesis"] != id {
		t.Fatalf("the curator proposes: %d %+v", code, e)
	}
	view = show()
	hyps, _ := view["hypotheses"].([]any)
	hposF, _ := hyps[0].(map[string]any)["position"].(float64)
	cited := fmt.Sprintf("%s@%d", id, int(hposF))

	// The candidate lesson lands on main at a revision: the carrier.
	body := "---\nhypothesis: " + cited + "\napplies-when: {\"routing\": \"core\"}\nsupport: " + fmt.Sprintf("c-a@%d, c-b@%d", pA, pB) +
		"\nprovenance: plans/os-96850e5a.md @ " + anchor + "\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: role\n---\n\n# Record the mirror's temperature\n"
	if err := os.MkdirAll(filepath.Join(m.src, curation.LessonsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.src, curation.LessonsDir, "mirror.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitSrc("add", ".")
	gitSrc("commit", "--quiet", "-m", "knowledge: the mirror lesson (#2)")
	carrierCommit := gitSrc("rev-parse", "HEAD")
	carrier := curation.LessonsDir + "/mirror.md @ " + carrierCommit

	// The bound eval, filed by the dispatcher, offered by the
	// supervisor, worked by impl2 from a base of the drill's choosing.
	fileBound := func() string {
		t.Helper()
		e, code := runEnv(t, append(append([]string{"eval", "file"}, m.posture()...), "--repo", m.src, "--key", m.keys["dispatch"],
			"--eval", "fix-the-check", "--for-lesson", cited, "--carrier", carrier)...)
		if code != 0 {
			t.Fatalf("the dispatcher files the bound eval: %d %+v", code, e.Error)
		}
		subject, _ := e.Result["subject"].(string)
		return subject
	}
	offer := func() {
		t.Helper()
		e, code := runEnv(t, append(append([]string{"eval", "act"}, m.posture()...), "--repo", m.src, "--key", m.keys["supervisor"])...)
		if code != 0 {
			t.Fatalf("the supervisor's act: %d %+v", code, e.Error)
		}
	}
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
	solve := func(base string) {
		t.Helper()
		branches++
		branch := fmt.Sprintf("solve-%d", branches)
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
	}
	workEval := func(base string) string {
		t.Helper()
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl2"],
			loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
				solve(base)
				return 2, nil
			}), loop.WithRepo(clone))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil || step.Outcome != loop.Submitted {
			t.Fatalf("the loop claims, works and submits the eval: %+v %v", step, err)
		}
		return step.Subject
	}
	render := func(subject, verdict string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
			"--subject", subject, "--repo", m.src, "--key", m.keys["verify"], "--verdict", verdict)...)
	}
	// Worked from the definition's anchor, without the candidate: the
	// carrier is absent from the submission head, and no verdict
	// renders.
	e1 := fileBound()
	offer()
	if got := workEval(anchor); got != e1 {
		t.Fatalf("impl2 took the bound eval: %s", got)
	}
	if e, code := render(e1, "pass"); code != 20 || e.Error == nil || e.Error.Code != "carrier_absent" {
		t.Fatalf("a counter-trajectory judged without the candidate applied refuses carrier_absent: %d %+v", code, e.Error)
	}
	// Worked from the carrier: the candidate is applied, and the pass
	// is the survival the promotion cites.
	e2 := fileBound()
	offer()
	if got := workEval(carrierCommit); got != e2 {
		t.Fatalf("impl2 took the second bound eval: %s", got)
	}
	if e, code := render(e2, "pass"); code != 0 {
		t.Fatalf("the disjoint verifier passes the solved eval under the candidate: %d %+v", code, e.Error)
	}
	st, failEnv := loadVerdictState(m.materialize(t))
	if failEnv != nil {
		t.Fatalf("the remote chain must verify: %+v", failEnv)
	}
	evalState, _ := st.fold.State(e2)
	if evalState.Verdict == nil {
		t.Fatal("the pass folded")
	}
	passPos := evalState.Verdict.Pos

	// The observer promotes, citing the survived eval, the digest read
	// from the file at its anchor.
	e, code = runEnv(t, append(append([]string{"knowledge", "promote"}, m.posture()...), "--key", m.keys["observer"],
		"--lesson", carrier, "--hypothesis", cited, "--pr", "pr/2 @ "+carrierCommit, "--repo", m.src, "--carrier", "role",
		"--adversarial", fmt.Sprintf("fix-the-check@%d", passPos), "--last-validated", "2026-09-01T00:00:00Z", "--expires", "2026-12-01T00:00:00Z")...)
	if code != 0 || e.Result["subject"] != id {
		t.Fatalf("the observer promotes citing the survived eval: %d %+v", code, e)
	}

	// The next claim on a matching contract receives the lesson in its
	// envelope, and in its provisioned lessons.json.
	m.contract(t, "c-c", "supervisor")
	e, code = runEnv(t, append(append([]string{"claim", "take"}, m.posture()...), "--key", m.keys["impl"], "--subject", "c-c", "--repo", m.src)...)
	if code != 0 {
		t.Fatalf("claim take: %d %+v", code, e.Error)
	}
	lessons, _ := e.Result["lessons"].([]any)
	if len(lessons) != 1 {
		t.Fatalf("the claim receives the promoted lesson: %+v", e.Result)
	}
	row, _ := lessons[0].(map[string]any)
	if row["lesson"] != carrier || row["digest"] != curation.Digest([]byte(body)) || row["carrier"] != "role" {
		t.Fatalf("the row carries the anchor and the digest of the file at it: %+v", row)
	}
	fence, _ := e.Result["fence"].(string)
	lessonBytes, err := json.Marshal(lessons)
	if err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, append(append([]string{"budget", "reserve"}, m.posture()...), "--key", m.keys["impl"], "--subject", "c-c", "--amount", "3")...); code != 0 {
		t.Fatalf("reserve: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, append(append([]string{"run", "start"}, m.posture()...), "--key", m.keys["supervisor"], "--subject", "c-c",
		"--principal", "acme", "--model", "fable/5.1", "--tool-policy", "default")...)
	if code != 0 {
		t.Fatalf("run start: %d %+v", code, e.Error)
	}
	var fenceN, startedN int
	fmt.Sscanf(fence, "%d", &fenceN)
	if e.Position != nil {
		fmt.Sscanf(*e.Position, "%d", &startedN)
	}
	run, err := executor.LocalWorktree{}.Provision(executor.ProvisionSpec{
		Ledger: m.materialize(t), Repo: m.src, Base: m.head, Subject: "c-c", Actor: m.fps["impl"],
		Fence: fenceN, Started: startedN, Packet: []byte(`{"drill": "packet"}`), ObsDir: t.TempDir(), Lessons: lessonBytes,
	})
	if err != nil {
		t.Fatalf("Provision with the claim's lessons: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(run.Workspace(), ".seed-run", "lessons.json")); err != nil || !strings.Contains(string(b), carrier) {
		t.Fatalf("the provisioned handoff carries the lesson beside the packet: %v %s", err, b)
	}
	_ = run.Dispose()
	// Without a repository nothing surfaces and the count says so.
	e, code = runEnv(t, append(append([]string{"situation"}, m.posture()...), "--key", m.keys["impl"], "--subject", "c-c")...)
	if code != 0 || e.Result["lessons_unverified"] == nil {
		t.Fatalf("the orienting read without --repo reports the lesson unverified: %d %+v", code, e.Result)
	}
	// The advisory read finds the promoted lesson by its words, read
	// at its anchor and verified against the repository; without one
	// it is reported unresolved, never indexed.
	search := func(extra ...string) map[string]any {
		t.Helper()
		e, code := runEnv(t, append(append(append([]string{"knowledge", "search"}, m.posture()...), extra...), "mirror", "temperature")...)
		if code != 0 {
			t.Fatalf("knowledge search: %d %+v", code, e.Error)
		}
		return e.Result
	}
	found := search("--repo", m.src, "--now", "2026-10-01T00:00:00Z")
	if hits, _ := found["hits"].([]any); len(hits) != 3 || hits[0].(map[string]any)["kind"] != "lesson" || hits[0].(map[string]any)["id"] != carrier || hits[0].(map[string]any)["snippet"] != "Record the mirror's temperature" {
		t.Fatalf("the promoted lesson is the first hit by its anchor, ahead of the two dead ends that say mirror: %+v", found)
	}
	blind := search("--now", "2026-10-01T00:00:00Z")
	for _, h := range blind["hits"].([]any) {
		if h.(map[string]any)["kind"] == "lesson" {
			t.Fatalf("without a repository no lesson is indexed: %+v", blind)
		}
	}
	if unresolved, _ := blind["lessons_unresolved"].([]any); len(unresolved) != 1 || unresolved[0].(map[string]any)["lesson"] != carrier {
		t.Fatalf("the candidate is reported unresolved: %+v", blind)
	}

	// A contest over held-out evidence (impl's dead end on c-c, a
	// selected contract outside the support set) removes the lesson
	// from the next claim.
	if e, code := deadEnd("impl", "c-c"); code != 0 {
		t.Fatalf("the held-out dead end: %d %+v", code, e.Error)
	}
	pC := firstDeadEnd(show(), "c-c")
	m.contract(t, "c-d", "supervisor")
	// The contest lands MID-FLIGHT: staged through the boundary, then
	// rewound so the hook replays it when the claim's first push
	// arrives. The claim's session opened at a tip where the lesson
	// surfaced; the retry against the refreshed tip must re-derive
	// the set, so the claim lands reporting what the landed tip
	// delivers, never what the session opened at (review finding on
	// the task PR).
	before := remoteTip(t, m.remote)
	contest := fmt.Sprintf(`{"hypothesis": %q, "evidence": [%q], "reason": "the temperature was recorded and the fetch still failed"}`, cited, fmt.Sprintf("c-c@%d", pC))
	// Landed through the library with its own client state, so the
	// claim's persisted head never sees the rival before the race.
	curatorBytes, err := os.ReadFile(m.keys["curator"])
	if err != nil {
		t.Fatal(err)
	}
	curatorKey, err := event.ParsePrivateKey(curatorBytes)
	if err != nil {
		t.Fatal(err)
	}
	curatorFP, err := event.Fingerprint(curatorKey.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rivalClient, err := gitref.NewClient(t.TempDir(), m.remote, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rivalClient.AppendLoop(gitref.Draft{
		V: version.Seed3, TS: "2026-09-01T02:00:00Z", Actor: curatorFP,
		Verb: curation.ContestVerb, Subject: id, Payload: json.RawMessage(contest),
	}, func(ev event.Event) (*event.Record, error) { return event.Sign(ev, curatorKey) }, m.ringOf(t).Resolver(), nil, 5); err != nil {
		t.Fatalf("the curator's contest lands through the library: %v", err)
	}
	rival := remoteTip(t, m.remote)
	if out, err := exec.Command("git", "--git-dir", m.remote, "update-ref", remoteRef, before).CombinedOutput(); err != nil {
		t.Fatalf("rewind: %v %s", err, out)
	}
	installRivalHook(t, m.remote, []string{rival})
	e, code = runEnv(t, append(append([]string{"claim", "take"}, m.posture()...), "--key", m.keys["impl2"], "--subject", "c-d", "--repo", m.src)...)
	if code != 0 {
		t.Fatalf("claim take after the contest: %d %+v", code, e.Error)
	}
	if attempts, _ := e.Result["attempts"].(float64); attempts < 2 {
		t.Fatalf("fixture: the rival must have landed mid-flight, or the race never happened: %+v", e.Result)
	}
	if lessons, _ := e.Result["lessons"].([]any); len(lessons) != 0 {
		t.Fatalf("a contested hypothesis's lesson reaches no claim, even when the contest lands mid-flight: %+v", lessons)
	}
	// A contested lesson is as absent from the advisory read as from
	// the claim; the held-out dead end that contested it is found.
	contested := search("--repo", m.src, "--now", "2026-10-01T00:00:00Z")
	if hits, _ := contested["hits"].([]any); len(hits) != 3 || hits[0].(map[string]any)["kind"] != "deadend" {
		t.Fatalf("after the contest only the three dead ends hit: %+v", contested)
	}
	if counts, _ := contested["documents"].(map[string]any); counts["lesson"] != 0.0 {
		t.Fatalf("the contested lesson leaves the corpus: %+v", counts)
	}
	if remoteTip(t, m.remote) == rival {
		t.Fatal("fixture: the claim must have landed after the rival")
	}
	if stages, _ := show()["stages"].(map[string]any); stages["contested"] != 1.0 || stages["lessons"] != 1.0 {
		t.Fatalf("the contest keeps the file and the facts: %+v", stages)
	}
}
