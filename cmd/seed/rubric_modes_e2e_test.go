package main

// Rubric verdicts end to end in small-team mode (plans/os-2e34f66a.md
// AC7): a rubric contract reaches done through a scorecard render, and
// a deferred one through the human's render over the deferral's
// receipt. Drift suspending a verifier until re-calibrated is drilled
// at the terminal (calibration_cli_test.go) on the same machinery.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestSmallTeamRubricContractsReachDone(t *testing.T) {
	m := buildMode(t, smallTeam)
	m.upgradeTo(t, version.Seed4)
	gitSrc := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", m.src, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.WriteFile(filepath.Join(m.src, "rubric.md"), []byte("# Judged\n\n## Rubric\n\n- tone: reads as the operator's\n- taste: the abstraction carries its weight\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitSrc("add", ".")
	gitSrc("commit", "--quiet", "-m", "the rubric spec")
	rubricCommit := gitSrc("rev-parse", "HEAD")
	rubricContract := func(subject string) {
		t.Helper()
		m.appendRaw("intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
		m.appendRaw("contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "rubric.md @ %s", "executable": false}}`, rubricCommit))
		offer := fmt.Sprintf(`{"eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}, "expires": %q}`,
			time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
		if e, code := runEnv(t, "ledger", "append", "--remote", m.remote, "--state", m.state,
			"--key", m.keys["supervisor"], "--verb", "offer.published", "--subject", subject, "--payload", offer); code != 0 {
			t.Fatalf("offer: %d %+v", code, e)
		}
	}
	scorecard := func(subject string, toneU, tasteU string) string {
		t.Helper()
		st, failEnv := loadVerdictState(m.materialize(t))
		if failEnv != nil {
			t.Fatalf("the remote chain must verify: %+v", failEnv)
		}
		s, _ := st.fold.State(subject)
		path := filepath.Join(t.TempDir(), "scorecard.json")
		body := fmt.Sprintf(`{"contract": %q, "submission": "%d", "items": [{"id": "tone", "score": "pass", "evidence": ["accept.md @ %s"], "uncertainty": %q}, {"id": "taste", "score": "pass", "evidence": ["accept.md @ %s"], "uncertainty": %q}]}`,
			subject, s.Submission.Pos, m.spec, toneU, m.spec, tasteU)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	render := func(actor, subject, path string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
			"--subject", subject, "--repo", m.src, "--key", m.keys[actor], "--verdict", "pass", "--scorecard", path)...)
	}

	// A rubric contract: worked, scored item by item, landed.
	rubricContract("c-rubric")
	if got := m.work(t, "impl", "fable/5.1"); got != "c-rubric" {
		t.Fatalf("the loop took the rubric contract: %s", got)
	}
	if e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
		"--subject", "c-rubric", "--repo", m.src, "--key", m.keys["verify"], "--verdict", "pass")...); code != 64 || !strings.Contains(e.Error.Message, "--scorecard") {
		t.Fatalf("a rubric spec renders only over a scorecard: %d %+v", code, e.Error)
	}
	e, code := render("verify", "c-rubric", scorecard("c-rubric", "low", "low"))
	if code != 0 || e.Result["scorecard"] == nil {
		t.Fatalf("the verifier renders over its scorecard: %d %+v", code, e)
	}
	m.land(t, "c-rubric", "impl", "pr/7")

	// A deferred contract: the verifier scores an item at high
	// uncertainty, defers, and the human renders over the deferral's
	// receipt; the chain lands through the human's verdict.
	rubricContract("c-deferred")
	if got := m.work(t, "impl", "fable/5.1"); got != "c-deferred" {
		t.Fatalf("the loop took the deferred contract: %s", got)
	}
	high := scorecard("c-deferred", "low", "high")
	if e, code := render("verify", "c-deferred", high); code != 20 || e.Error == nil || e.Error.Code != "human_verdict" {
		t.Fatalf("a high item refuses human_verdict: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, append(append([]string{"verdict", "defer"}, m.posture()...),
		"--subject", "c-deferred", "--repo", m.src, "--key", m.keys["verify"], "--scorecard", high)...); code != 0 || fmt.Sprint(e.Result["items"]) != "[taste]" {
		t.Fatalf("the verifier defers naming the item: %d %+v", code, e)
	}
	if e, code := render("verify", "c-deferred", scorecard("c-deferred", "low", "low")); code != 20 || e.Error == nil || e.Error.Code != "human_verdict" {
		t.Fatalf("after the deferral the verifier's render refuses human_verdict: %d %+v", code, e.Error)
	}
	m.human(t)
	if e, code := render("human", "c-deferred", scorecard("c-deferred", "low", "low")); code != 0 {
		t.Fatalf("the human renders over the deferral: %d %+v", code, e.Error)
	}
	m.land(t, "c-deferred", "impl", "pr/8")
}
