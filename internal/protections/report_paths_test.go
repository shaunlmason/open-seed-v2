package protections

// Plan and Apply's refusal and reporting arms (plans/os-f262585a.md
// D1, D3). Every assertion reads a value the operator acts on: what the
// report counts as drift, what Apply refuses rather than half-doing,
// and what the credential-free observer says when the snapshot cannot
// answer.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingForge answers whichever call the case under test needs to
// fail, so a refusal can be traced to the call that produced it.
type failingForge struct {
	state    *State
	readErr  error
	applyErr error
	applied  int
}

func (f *failingForge) Read() (*State, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.state, nil
}

func (f *failingForge) Apply(changes []Change, desired *State) error {
	f.applied++
	return f.applyErr
}

func TestPlanCarriesTheForgesRefusalOut(t *testing.T) {
	boom := errors.New("the forge is unreachable")
	_, _, err := Plan(declaration(t), &failingForge{readErr: boom}, t.TempDir())
	if !errors.Is(err, boom) {
		t.Fatalf("a read the forge refused must reach the caller unchanged, got %v", err)
	}
}

func TestApplyDoesNotWriteWhenTheForgeRefuses(t *testing.T) {
	boom := errors.New("403 from the forge")
	repo := t.TempDir()
	forge := &failingForge{state: &State{DefaultBranch: "main", Rulesets: map[string]Ruleset{}}, applyErr: boom}

	_, err := Apply(declaration(t), forge, repo)
	if !errors.Is(err, boom) {
		t.Fatalf("Apply must surface the forge's refusal, got %v", err)
	}
	if forge.applied != 1 {
		t.Errorf("the forge was called %d times; one attempt, then the refusal", forge.applied)
	}
	if _, err := os.Stat(filepath.Join(repo, CodeownersPath)); !errors.Is(err, os.ErrNotExist) {
		t.Error("CODEOWNERS must not be written when the ruleset changes did not land: a half-applied surface is worse than none")
	}
}

func TestApplyWritesCodeownersAndReportsItClean(t *testing.T) {
	repo := t.TempDir()
	forge := &failingForge{state: &State{DefaultBranch: "main", Rulesets: map[string]Ruleset{}}}

	rep, err := Apply(declaration(t), forge, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Applied {
		t.Error("the report after an apply says so")
	}
	if rep.Codeowners != "clean" {
		t.Errorf("CODEOWNERS was just written, so the re-read plan reads clean, got %q", rep.Codeowners)
	}
	b, err := os.ReadFile(filepath.Join(repo, CodeownersPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/Makefile @org/governance") {
		t.Errorf("the declaration's protected surface did not reach the file:\n%s", b)
	}
}

func TestApplyWithNoRepoDirTouchesNoWorkingTree(t *testing.T) {
	// `seed protections apply` against a forge with no checkout: the
	// ruleset changes still go, and nothing is written or linted.
	forge := &failingForge{state: &State{DefaultBranch: "main", Rulesets: map[string]Ruleset{}}}
	rep, err := Apply(declaration(t), forge, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Codeowners != "n/a" {
		t.Errorf("with no working tree there is no CODEOWNERS verdict, got %q", rep.Codeowners)
	}
	if len(rep.Findings) != 0 {
		t.Errorf("with no working tree there are no workflow findings, got %v", rep.Findings)
	}
	if forge.applied != 1 {
		t.Errorf("the forge is still reconciled, called %d times", forge.applied)
	}
}

func TestPlanCountsCodeownersDriftAsDrift(t *testing.T) {
	repo := t.TempDir()
	forge := &failingForge{state: &State{DefaultBranch: "main", Rulesets: map[string]Ruleset{}}}
	cfg := declaration(t)

	before, _, err := Plan(cfg, forge, repo)
	if err != nil {
		t.Fatal(err)
	}
	if before.Codeowners != "drift" {
		t.Fatalf("an absent CODEOWNERS is drift, got %q", before.Codeowners)
	}

	if _, err := WriteCodeowners(cfg, repo); err != nil {
		t.Fatal(err)
	}
	after, _, err := Plan(cfg, forge, repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.Codeowners != "clean" {
		t.Fatalf("the written rendering is clean, got %q", after.Codeowners)
	}
	if after.DriftCount != before.DriftCount-1 {
		t.Errorf("writing CODEOWNERS took exactly one off the drift count: %d then %d", before.DriftCount, after.DriftCount)
	}
}

func TestCodeownersIsSilentWithoutOwners(t *testing.T) {
	// No owners means nothing to render, and every caller stays quiet
	// rather than reporting drift against an empty rendering.
	cfg := declaration(t)
	cfg.Admission.Owners = nil
	repo := t.TempDir()

	if _, ok := Codeowners(cfg); ok {
		t.Fatal("no owners, nothing to render")
	}
	want, drift, err := CodeownersDrift(cfg, repo)
	if err != nil || drift || want != "" {
		t.Errorf("no rendering means no drift, got %q %v %v", want, drift, err)
	}
	wrote, err := WriteCodeowners(cfg, repo)
	if err != nil || wrote {
		t.Errorf("nothing is written, got %v %v", wrote, err)
	}
	if _, err := os.Stat(filepath.Join(repo, CodeownersPath)); !errors.Is(err, os.ErrNotExist) {
		t.Error("no file was created")
	}
}

func TestLintWorkflowsWithoutTheDirectory(t *testing.T) {
	// A checkout with no .github/workflows is not a lint failure.
	findings, err := LintWorkflows(t.TempDir())
	if err != nil || findings != nil {
		t.Fatalf("an absent workflows directory is silent, got %v %v", findings, err)
	}
}
