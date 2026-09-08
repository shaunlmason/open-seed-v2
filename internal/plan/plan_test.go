package plan_test

// The falsifiable-plan lint drills (plans/os-16c1d142.md): a complete
// plan lints clean, each missing part refuses separately with missing
// retention the distinct named finding, a commands section covering
// only one set fails, and the classifier maps every path shape. The
// repository's own plan for this task must lint clean: the lint
// accepts the shape the loop actually writes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/plan"
)

const goodPlan = `# Plan: do the thing

## Acceptance Criteria

**Boundary set (new, shown working):**

- the new verb refuses bad input

**Retention set (existing, shown unharmed):**

- every existing suite passes unchanged

## Validation Commands

- Boundary: go test ./internal/thing/...
- Retention: go test ./... and make check

## Expected diff shape

One new package with tests; no deletions.
`

func kinds(fs []plan.Finding) string {
	var ks []string
	for _, f := range fs {
		ks = append(ks, f.Kind)
	}
	return strings.Join(ks, ",")
}

func TestLint(t *testing.T) {
	if fs := plan.Lint([]byte(goodPlan)); len(fs) != 0 {
		t.Fatalf("a complete falsifiable plan lints clean: %v", fs)
	}

	cases := []struct{ name, cut, want string }{
		{"missing boundary", "**Boundary set (new, shown working):**\n\n- the new verb refuses bad input\n", plan.KindMissingBoundary},
		{"missing retention", "**Retention set (existing, shown unharmed):**\n\n- every existing suite passes unchanged\n", plan.KindMissingRetention},
		{"missing commands", "## Validation Commands\n\n- Boundary: go test ./internal/thing/...\n- Retention: go test ./... and make check\n", plan.KindMissingCommands},
		{"missing diff shape", "## Expected diff shape\n\nOne new package with tests; no deletions.\n", plan.KindMissingDiffShape},
	}
	for _, c := range cases {
		mutated := strings.Replace(goodPlan, c.cut, "", 1)
		if mutated == goodPlan {
			t.Fatalf("%s: mutation did not apply", c.name)
		}
		fs := plan.Lint([]byte(mutated))
		if len(fs) != 1 || fs[0].Kind != c.want {
			t.Fatalf("%s: want exactly [%s], got %v", c.name, c.want, fs)
		}
	}

	// The retention refusal carries the charter's clause.
	fs := plan.Lint([]byte(strings.Replace(goodPlan, "**Retention set (existing, shown unharmed):**\n\n- every existing suite passes unchanged\n", "", 1)))
	if len(fs) != 1 || !strings.Contains(fs[0].Detail, "does not lint") {
		t.Fatalf("missing retention must quote the charter clause: %v", fs)
	}

	// Commands covering only one set fail on the other.
	oneSided := strings.Replace(goodPlan, "- Retention: go test ./... and make check\n", "", 1)
	fs = plan.Lint([]byte(oneSided))
	if len(fs) != 1 || fs[0].Kind != plan.KindCommandsMissingRetention {
		t.Fatalf("commands missing the retention side must fail: %v", fs)
	}

	// Prose mentioning the words is not a command line: the labeled
	// form is the contract, and a bare label carries no command.
	prose := strings.Replace(goodPlan,
		"- Boundary: go test ./internal/thing/...\n- Retention: go test ./... and make check\n",
		"This discusses boundary and retention at length.\n", 1)
	fs = plan.Lint([]byte(prose))
	if kinds(fs) != plan.KindCommandsMissingBoundary+","+plan.KindCommandsMissingRetention {
		t.Fatalf("prose without labeled command lines must fail both sides: %v", fs)
	}
	bare := strings.Replace(goodPlan, "- Boundary: go test ./internal/thing/...\n", "- Boundary:\n", 1)
	fs = plan.Lint([]byte(bare))
	if kinds(fs) != plan.KindCommandsMissingBoundary {
		t.Fatalf("a bare label is not a command: %v", fs)
	}

	// An empty section is present but resumes nothing.
	empty := strings.Replace(goodPlan, "- the new verb refuses bad input\n", "", 1)
	fs = plan.Lint([]byte(empty))
	if kinds(fs) != plan.KindEmptySection {
		t.Fatalf("an empty boundary section must fail: %v", fs)
	}
}

// The lint accepts the loop's own artifacts: the plan that authorized
// this task lints clean, so the grammar and the practice cannot drift.
func TestOwnPlanLintsClean(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "plans", "os-16c1d142.md"))
	if err != nil {
		t.Skipf("plan file not present: %v", err)
	}
	if fs := plan.Lint(b); len(fs) != 0 {
		t.Fatalf("the task's own plan must lint clean: %v", fs)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		paths []string
		want  plan.Class
	}{
		{[]string{"plans/os-1.md"}, plan.ClassPlan},
		{[]string{"internal/plan/plan.go", "spec/plans.md"}, plan.ClassImplementation},
		{[]string{"plans/os-1.md", "internal/plan/plan.go"}, plan.ClassMixed},
		{[]string{"plans/os-1.md", "plans/os-2.md"}, plan.ClassMixed},
		{[]string{}, plan.ClassImplementation},
	}
	for _, c := range cases {
		if got := plan.Classify(c.paths); got != c.want {
			t.Fatalf("Classify(%v) = %s, want %s", c.paths, got, c.want)
		}
	}
}

func TestCommandsExtraction(t *testing.T) {
	doc := []byte("# Spec\n\nProse.\n\n## Validation Commands\n\n" +
		"- Boundary: `go test ./...`\n" +
		"- Retention: `make check` and more prose\n" +
		"- `sh -c 'echo colon: inside'`\n" +
		"- bare command without backticks\n" +
		"\n## Expected diff shape\n\n- `never extracted`\n")
	got := plan.Commands(doc)
	want := []string{
		"go test ./...",
		"make check",
		"sh -c 'echo colon: inside'",
		"bare command without backticks",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d commands %v, want %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCommandsEmptyCases(t *testing.T) {
	if got := plan.Commands([]byte("# Doc\n\nno commands section\n")); len(got) != 0 {
		t.Fatalf("a doc without the section yields no commands, got %v", got)
	}
	if got := plan.Commands([]byte("## Validation Commands\n\n")); len(got) != 0 {
		t.Fatalf("an empty section yields no commands, got %v", got)
	}
}

func TestCommandsDoesNotBreakLint(t *testing.T) {
	// The factoring must leave Lint's behavior untouched: a linting
	// plan still lints, and Commands reads the same section the lint
	// checked the labels in.
	doc := []byte("**Boundary set**\n- new thing\n\n**Retention set**\n- old thing\n\n" +
		"**Validation commands**\n- Boundary: `cmd-b`\n- Retention: `cmd-r`\n\n" +
		"**Expected diff shape**\n- small\n")
	if fs := plan.Lint(doc); len(fs) != 0 {
		t.Fatalf("fixture must lint clean, got %v", fs)
	}
	got := plan.Commands(doc)
	if len(got) != 2 || got[0] != "cmd-b" || got[1] != "cmd-r" {
		t.Fatalf("Commands reads the linted section: %v", got)
	}
}
