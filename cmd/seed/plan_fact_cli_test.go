package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// planRepo commits a plan file at two revisions and returns the
// repository with both anchors and both digests.
func planRepo(t *testing.T) (dir, anchor1, anchor2, digest1, digest2 string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "plans", "c-1.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "--quiet", "-m", "plan")
		sum := sha256.Sum256([]byte(body))
		return hex.EncodeToString(sum[:])
	}
	digest1 = write("# Plan\n\nfirst\n")
	anchor1 = "plans/c-1.md @ " + git("rev-parse", "HEAD")
	digest2 = write("# Plan\n\nsecond, edited in review\n")
	anchor2 = "plans/c-1.md @ " + git("rev-parse", "HEAD")
	return dir, anchor1, anchor2, digest1, digest2
}

func upgradeLedgerTo(t *testing.T, ld, priv, to string) {
	t.Helper()
	for _, v := range []string{version.Seed2, version.Seed3, version.Seed4} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+v+`"}`); code != 0 {
			t.Fatalf("upgrade to %s: %d %+v", v, code, e)
		}
		if v == to {
			return
		}
	}
}

func foldState(t *testing.T, ld, subject string) (admit.Context, bool) {
	t.Helper()
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := ctx.Lifecycle.State(subject)
	return *ctx, ok
}

// conformance: plans/os-6bd9ffff.md D5, AC5 — seed plan propose and
// approve derive the digest from the repository at the anchor and
// refuse an anchor the repository lacks; at seed/4 the fold reads the
// first proposal's digest and the approval's, so an approval at the
// same content is unedited and one at a revised plan is edited;
// before seed/4 the verbs refuse naming the version.
func TestPlanProposeAndApproveDeriveTheDigest(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	repo, anchor1, anchor2, digest1, digest2 := planRepo(t)
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}

	// Before seed/4 the digest has nowhere to go: the boundary refuses
	// naming the version, and nothing is appended.
	e, code := runEnv(t, "plan", "propose", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1", "--plan", anchor1, "--repo", repo)
	if code != 8 || e.Error == nil || !strings.Contains(e.Error.Message, version.Seed4) {
		t.Fatalf("a proposal before seed/4 refuses naming the version: %d %+v", code, e)
	}
	upgradeLedgerTo(t, ld, priv, version.Seed4)

	// An anchor the repository lacks has no digest to carry.
	if e, code := runEnv(t, "plan", "propose", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1",
		"--plan", "plans/missing.md @ "+strings.Split(anchor1, " @ ")[1], "--repo", repo); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("an anchor the repository lacks refuses not_found: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "propose", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1",
		"--plan", "not an anchor", "--repo", repo); code != 64 {
		t.Fatalf("a malformed anchor is usage: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "propose", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1",
		"--plan", anchor1, "--repo", repo, "--pr", "pr/1 @ abc1234"); code != 64 {
		t.Fatalf("a proposal carries no PR: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "approve", "--ledger", ld, "--key", priv, "--subject", "c-1",
		"--plan", anchor1, "--repo", repo); code != 64 {
		t.Fatalf("an approval names the merged PR: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "approve", "--ledger", ld, "--key", priv, "--subject", "c-1",
		"--plan", anchor1, "--repo", repo, "--pr", "x"); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "<pr> @ <merged-commit>") {
		t.Fatalf("an approval's PR is an anchor, refused as usage before the session opens: %d %+v", code, e)
	}

	// The holder proposes: the digest is the plan bytes' at the anchor
	// and the fence is derived from the window.
	e, code = runEnv(t, "plan", "propose", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1", "--plan", anchor1, "--repo", repo)
	if code != 0 || e.Result["digest"] != digest1 || e.Result["plan"] != anchor1 {
		t.Fatalf("the proposal carries the derived digest: %d %+v", code, e)
	}
	if !journalHas(t, ld, "plan.proposed", "admitted") {
		t.Fatal("the proposal rides the loop seam and journals its attempt")
	}
	// A second proposal at the revised plan: the fold keeps the FIRST.
	if e, code := runEnv(t, "plan", "propose", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1", "--plan", anchor2, "--repo", repo); code != 0 {
		t.Fatalf("a second proposal admits: %d %+v", code, e)
	}
	ctx, _ := foldState(t, ld, "c-1")
	if d := ctx.Lifecycle.PlanDigests("c-1"); d.Proposed != digest1 || d.Approved != "" {
		t.Fatalf("the fold keeps the first proposal's digest and no approval yet: %+v", d)
	}

	// The operator approves at the revised anchor: measured, edited.
	e, code = runEnv(t, "plan", "approve", "--ledger", ld, "--key", priv, "--subject", "c-1", "--plan", anchor2, "--pr", "pr/1 @ "+specCommit, "--repo", repo)
	if code != 0 || e.Result["digest"] != digest2 {
		t.Fatalf("the approval carries the derived digest: %d %+v", code, e)
	}
	ctx, _ = foldState(t, ld, "c-1")
	if unedited, measured := ctx.Lifecycle.PlanDigests("c-1").Unedited(); unedited || !measured {
		t.Fatalf("an approval at revised content is edited and measured: %v %v", unedited, measured)
	}
	if approved, _ := ctx.Lifecycle.PlanApproved("c-1"); approved != anchor2 {
		t.Fatalf("the approved anchor is the approval's: %s", approved)
	}

	// The report reads the figure: one approval, edited, rate 0.000.
	out := filepath.Join(t.TempDir(), "views")
	unlockForCleanup(t, out)
	if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out); code != 0 {
		t.Fatalf("rebuild: %d %+v", code, e)
	}
	cur, err := os.ReadFile(filepath.Join(out, "report", "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(filepath.Join(out, "report", "builds", strings.TrimSpace(string(cur)), "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"approvals": 1`, `"edited": 1`, `"unedited_rate": "0.000"`, `"specified": 1`, `"retriage_rate": "0.000"`} {
		if !strings.Contains(string(report), want) {
			t.Errorf("the report's lanes section carries %s: %s", want, report)
		}
	}
	_ = fmt.Sprint
}
