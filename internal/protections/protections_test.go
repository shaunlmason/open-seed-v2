package protections

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

func declaration(t *testing.T) *posture.Config {
	t.Helper()
	cfg, err := posture.Parse([]byte(`{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://127.0.0.1:1", "identity": "app:4242", "checks": ["verify", "check"], "reviews": 1, "owners": ["@org/governance"]}, "protected": ["spec/", "Makefile", ".github/workflows/"]}`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func writeSnapshot(t *testing.T, st State) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "forge.json")
	b, _ := json.Marshal(st)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// conformance: III.L — the desired state derives from the declaration
// and the charter's rules with no second source: the ledger branch
// updatable by the admission identity alone, the default branch gated
// by the declared checks and reviews with thread resolution and code
// owners, contract branches and release tags protected.
func TestDesiredDerivesFromTheDeclaration(t *testing.T) {
	st, err := Desired(declaration(t), "main")
	if err != nil {
		t.Fatal(err)
	}
	ledger := st.Rulesets[RulesetLedger]
	if ledger.Target != TargetBranch || len(ledger.Refs) != 1 || ledger.Refs[0] != posture.DefaultLedgerRef || len(ledger.Bypass) != 1 || ledger.Bypass[0] != "app:4242" {
		t.Fatalf("the ledger ruleset must name the branch and the identity: %+v", ledger)
	}
	if !hasRule(ledger, RuleUpdate) || !hasRule(ledger, RuleDeletion) || !hasRule(ledger, RuleNonFastForward) {
		t.Fatalf("the ledger branch is update-restricted, undeletable and fast-forward only: %+v", ledger)
	}
	def := st.Rulesets[RulesetDefault]
	if def.Refs[0] != "refs/heads/main" || len(def.Bypass) != 0 {
		t.Fatalf("the default branch is read from HEAD and bypassed by nobody: %+v", def)
	}
	pr := ruleIndex(def.Rules)[RulePullRequest]
	if pr.Params["required_approving_review_count"] != 1 || pr.Params["required_review_thread_resolution"] != true || pr.Params["require_code_owner_review"] != true {
		t.Fatalf("reviews, thread resolution and code owners: %+v", pr.Params)
	}
	checks := ruleIndex(def.Rules)[RuleStatusChecks].Params["contexts"].([]string)
	if strings.Join(checks, ",") != "check,verify" {
		t.Fatalf("checks are the declared ones, sorted: %v", checks)
	}
	if st.Rulesets[RulesetContracts].Refs[0] != "refs/heads/seed/*" || st.Rulesets[RulesetTags].Target != TargetTag {
		t.Fatalf("contract branches and release tags: %+v %+v", st.Rulesets[RulesetContracts], st.Rulesets[RulesetTags])
	}
	if _, err := Desired(declaration(t), ""); err == nil {
		t.Fatal("an empty default branch is refused: it is read, never invented")
	}
	coop, _ := posture.Parse([]byte(`{"posture": "cooperative"}`))
	if _, err := Desired(coop, "main"); err == nil {
		t.Fatal("only the forge-hosted declaration derives protections")
	}
}

func hasRule(r Ruleset, typ string) bool {
	_, ok := ruleIndex(r.Rules)[typ]
	return ok
}

// conformance: III.L — plan before apply: drift is named by kind and
// ruleset, a rule the forge cannot express is manual work naming the
// click, a stray Seed ruleset is a deletion, and apply through the
// snapshot arm re-reads to a clean plan with the manual rule still
// named.
func TestPlanAndApplyThroughTheSnapshot(t *testing.T) {
	cfg := declaration(t)
	repo := t.TempDir()
	snap := Snapshot{Path: writeSnapshot(t, State{DefaultBranch: "main", Rulesets: map[string]Ruleset{
		"seed-old": {Name: "seed-old", Target: TargetBranch, Refs: []string{"refs/heads/x"}},
		"theirs":   {Name: "theirs", Target: TargetBranch, Refs: []string{"refs/heads/y"}},
	}, Unexpressible: []string{RuleUpdate}})}

	rep, _, err := Plan(cfg, snap, repo)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, c := range rep.Changes {
		kinds[c.Kind+":"+c.Ruleset]++
	}
	for _, want := range []string{"create:" + RulesetLedger, "create:" + RulesetDefault, "create:" + RulesetContracts, "create:" + RulesetTags, "delete:seed-old", "manual:" + RulesetLedger, "manual:" + RulesetTags} {
		if kinds[want] != 1 {
			t.Errorf("plan must carry %s once, got %+v", want, rep.Changes)
		}
	}
	if kinds["delete:theirs"] != 0 {
		t.Error("foreign rulesets are left alone")
	}
	if rep.Manual != 2 || rep.DriftCount != 5+1 || rep.Codeowners != "drift" {
		t.Fatalf("manual 2, drift = 5 ruleset changes + absent CODEOWNERS, got %+v", rep)
	}
	for _, c := range rep.Changes {
		if c.Kind == ChangeManual && !strings.Contains(c.Detail, "by hand") {
			t.Fatalf("a manual change names the click, got %q", c.Detail)
		}
	}

	after, err := Apply(cfg, snap, repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.DriftCount != 0 || after.Manual != 2 || after.Codeowners != "clean" || !after.Applied {
		t.Fatalf("after apply the plan is clean but the manual rules stay named, got %+v", after)
	}
	own, err := os.ReadFile(filepath.Join(repo, CodeownersPath))
	if err != nil || !strings.Contains(string(own), "/spec @org/governance") || !strings.Contains(string(own), "/Makefile @org/governance") {
		t.Fatalf("CODEOWNERS renders every protected prefix for the owners: %q %v", own, err)
	}
	cur, _ := snap.Read()
	if _, ok := cur.Rulesets["seed-old"]; ok {
		t.Fatal("the stray Seed ruleset is deleted")
	}
	if _, ok := cur.Rulesets["theirs"]; !ok {
		t.Fatal("the foreign ruleset survives")
	}
	if hasRule(cur.Rulesets[RulesetLedger], RuleUpdate) {
		t.Fatal("an unexpressible rule is never written as if expressed")
	}

	// A hand edit to the forge reappears as an update, by name.
	cur.Rulesets[RulesetDefault] = Ruleset{Name: RulesetDefault, Target: TargetBranch, Refs: []string{"refs/heads/main"}, Rules: []Rule{{Type: RuleDeletion}}}
	b, _ := json.Marshal(cur)
	_ = os.WriteFile(snap.Path, b, 0o644)
	rep, _, _ = Plan(cfg, snap, repo)
	if rep.DriftCount != 1 || rep.Changes[0].Kind != ChangeUpdate || rep.Changes[0].Ruleset != RulesetDefault || !strings.Contains(rep.Changes[0].Detail, "missing rule "+RulePullRequest) {
		t.Fatalf("a weakened forge ruleset is an update naming the missing rule, got %+v", rep.Changes)
	}
}

// The CI-identity lint: a scheduled workflow granting itself
// contents: write is the job the charter forbids; a scheduled one with
// read access and a push-triggered one with write access are not.
func TestLintWorkflowsFindsScheduledWriters(t *testing.T) {
	repo := t.TempDir()
	wf := filepath.Join(repo, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"nightly.yml": "on:\n  schedule:\n    - cron: '0 3 * * *'\npermissions:\n  contents: write\njobs: {}\n",
		"report.yml":  "on:\n  schedule:\n    - cron: '0 3 * * *'\npermissions:\n  contents: read\njobs: {}\n",
		"release.yml": "on:\n  push:\n    tags: ['v*']\npermissions:\n  contents: write\njobs: {}\n",
		"notes.md":    "schedule:\ncontents: write\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(wf, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LintWorkflows(repo)
	if err != nil || len(got) != 1 || got[0].File != filepath.Join(".github", "workflows", "nightly.yml") || !strings.Contains(got[0].Detail, "least-privilege") {
		t.Fatalf("exactly the scheduled writer is a finding, got %+v %v", got, err)
	}
	if got, err := LintWorkflows(t.TempDir()); err != nil || len(got) != 0 {
		t.Fatalf("no workflows, no findings: %+v %v", got, err)
	}
	rep, _, err := Plan(declaration(t), Snapshot{Path: writeSnapshot(t, State{DefaultBranch: "main"})}, repo)
	if err != nil || len(rep.Findings) != 1 {
		t.Fatalf("the plan carries the finding: %+v %v", rep, err)
	}
}

// CODEOWNERS is rendered only when both a protected list and owners
// exist, sorted, one line per prefix.
func TestCodeownersRendering(t *testing.T) {
	cfg := declaration(t)
	got, ok := Codeowners(cfg)
	if !ok {
		t.Fatal("a declaration with owners and a protected list renders")
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if lines[len(lines)-4] != "/.github/workflows @org/governance" || lines[len(lines)-3] != "/Makefile @org/governance" || lines[len(lines)-2] != "/seed.json @org/governance" || lines[len(lines)-1] != "/spec @org/governance" {
		t.Fatalf("sorted prefixes, trailing slash trimmed, the declaration itself included, owners appended: %q", got)
	}
	noOwners, _ := posture.Parse([]byte(`{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": "app:1"}, "protected": ["Makefile"]}`))
	if _, ok := Codeowners(noOwners); ok {
		t.Fatal("no owners, nothing to render")
	}
	if !cfg.Protects("spec/lifecycle.md") || !cfg.Protects("Makefile") || !cfg.Protects("seed.json") || cfg.Protects("internal/x.go") || cfg.Protects("Makefile.bak") {
		t.Fatal("the surface is exact-or-under-as-directory, the declaration included")
	}
	repo := t.TempDir()
	if _, drift, err := CodeownersDrift(cfg, repo); err != nil || !drift {
		t.Fatalf("an absent file drifts: %v %v", drift, err)
	}
	if _, err := WriteCodeowners(cfg, repo); err != nil {
		t.Fatal(err)
	}
	if _, drift, _ := CodeownersDrift(cfg, repo); drift {
		t.Fatal("the written file is clean")
	}
}

// The snapshot adapter refuses an absent or malformed file rather than
// reading an empty forge into a plan full of creates.
func TestSnapshotRefusesWhatItCannotRead(t *testing.T) {
	if _, err := (Snapshot{Path: filepath.Join(t.TempDir(), "absent.json")}).Read(); err == nil {
		t.Fatal("an absent snapshot is not an empty forge")
	}
	p := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(p, []byte("{"), 0o644)
	if _, err := (Snapshot{Path: p}).Read(); err == nil {
		t.Fatal("a malformed snapshot refuses")
	}
}
