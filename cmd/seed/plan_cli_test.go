package main

// The plan verbs end-to-end (plans/os-16c1d142.md): the lint accepts
// the repository's own plan shape and refuses a retention-less one;
// the classifier admits plan and implementation shapes and refuses
// mixed at exit 9.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCLI(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "plan.md")
	doc := "# Plan\n\n**Boundary set (new):**\n\n- x works\n\n**Retention set (existing):**\n\n- y unharmed\n\n## Validation Commands\n\n- Boundary: go test ./x/...\n- Retention: make check\n\n## Expected diff shape\n\nSmall.\n"
	if err := os.WriteFile(good, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "plan", "lint", good); code != 0 || e.Result["falsifiable"] != true {
		t.Fatalf("a falsifiable plan lints clean: %d %+v", code, e)
	}
	bad := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(bad, []byte(strings.Replace(doc, "**Retention set (existing):**\n\n- y unharmed\n\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "plan", "lint", bad); code != 9 || e.Error == nil || !strings.Contains(e.Error.Message, "missing_retention") {
		t.Fatalf("a retention-less plan must refuse at 9 naming the finding: %d %+v", code, e)
	}
	if _, code := runEnv(t, "plan", "lint", filepath.Join(dir, "absent.md")); code != 66 {
		t.Fatal("an unreadable plan is exit 66")
	}

	if e, code := runEnv(t, "plan", "classify", "plans/os-1.md"); code != 0 || e.Result["class"] != "plan" {
		t.Fatalf("one plan file classifies as plan: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "classify", "a.go", "b.go"); code != 0 || e.Result["class"] != "implementation" {
		t.Fatalf("code-only classifies as implementation: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "classify", "plans/os-1.md", "a.go"); code != 9 || e.Error == nil ||
		!strings.Contains(e.Error.Message, "structurally disjoint") {
		t.Fatalf("a mixed set must refuse at 9: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "classify", "plans/os-1.md", "plans/os-2.md"); code != 9 || e.Error == nil {
		t.Fatalf("two plan files must refuse at 9: %d %+v", code, e)
	}
}
