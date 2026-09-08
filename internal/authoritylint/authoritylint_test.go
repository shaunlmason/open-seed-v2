package authoritylint

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const mod = "example.com/m"

// clean is the split the tree is required to keep: an export binary
// over the registry, a coordination binary over the ledger, nothing in
// between.
func clean() map[string]string {
	return map[string]string{
		"mirror/mirror.go":          "package mirror\nimport \"context\"\ntype Adapter interface{ Name() string; List(context.Context) ([]Issue, error); Create(context.Context, Desired) (Issue, error); Update(context.Context, string, Desired, []string) error }\ntype Issue struct{}\ntype Desired struct{}\ntype snap struct{}\nfunc (snap) Name() string { return \"\" }\nfunc (snap) List(context.Context) ([]Issue, error) { return nil, nil }\nfunc (snap) Create(context.Context, Desired) (Issue, error) { return Issue{}, nil }\nfunc (snap) Update(context.Context, string, Desired, []string) error { return nil }\n",
		"internal/ledger/ledger.go": "package ledger\ntype Store struct{}\nfunc (s *Store) Append(x any) (int, error) { return 0, nil }\n",
		"internal/gitref/gitref.go": "package gitref\ntype Client struct{}\nfunc (c *Client) Push(s string) error { return nil }\n",
		"internal/propose/p.go":     "package propose\ntype Client struct{}\nfunc (c *Client) Propose(s string) error { return nil }\n",
		"internal/loop/loop.go":     "package loop\nimport \"" + mod + "/internal/ledger\"\nfunc Do(s *ledger.Store) { s.Append(nil) }\n",
		"internal/read/read.go":     "package read\nimport \"" + mod + "/internal/ledger\"\nfunc Do(s *ledger.Store) *ledger.Store { return s }\n",
		"cmd/seed/main.go":          "package main\nimport (\"flag\"; \"" + mod + "/internal/loop\"; \"" + mod + "/internal/read\")\nfunc main() { fs := flag.NewFlagSet(\"x\", 0); _ = fs.String(\"ledger\", \"\", \"\"); loop.Do(nil); read.Do(nil) }\n",
		"cmd/seed-mirror/main.go":   "package main\nimport (\"flag\"; \"" + mod + "/mirror\")\nfunc main() { fs := flag.NewFlagSet(\"x\", 0); _ = fs.String(\"projections\", \"\", \"\"); var _ mirror.Adapter }\n",
	}
}

func check(t *testing.T, src map[string]string) Report {
	t.Helper()
	g, err := Synthetic(mod, src)
	if err != nil {
		t.Fatal(err)
	}
	return Check(g)
}

func expectViolation(t *testing.T, rep Report, want string) {
	t.Helper()
	for _, v := range rep.Violations {
		if strings.Contains(v, want) {
			return
		}
	}
	t.Fatalf("no violation naming %q: %v", want, rep.Violations)
}

// conformance: plans/os-b45c308d.md D3 — the detector self-checks
// against synthetic graphs: a clean split passes with both classes
// derived non-empty; an exporter importing an append primitive fails;
// a coordination binary importing the registry fails; an adapter
// hidden outside the registry fails; an export component defining a
// Seed-side flag fails; an aliased primitive import is seen through;
// a read-only ledger consumer is not a writer.
func TestAuthorityBoundarySelfCheck(t *testing.T) {
	rep := check(t, clean())
	if len(rep.Violations) != 0 {
		t.Fatalf("the clean split passes: %v", rep.Violations)
	}
	if strings.Join(rep.Exporters, ",") != mod+"/mirror" {
		t.Fatalf("the export class is the registry: %v", rep.Exporters)
	}
	if strings.Join(rep.Writers, ",") != strings.Join([]string{mod + "/internal/gitref", mod + "/internal/ledger", mod + "/internal/loop", mod + "/internal/propose"}, ",") {
		t.Fatalf("the write class is the owners and the callers, never the read-only consumer: %v", rep.Writers)
	}
	byPath := map[string]Component{}
	for _, c := range rep.Components {
		byPath[c.Path] = c
	}
	if c := byPath[mod+"/cmd/seed"]; len(c.Write) == 0 || len(c.Export) != 0 {
		t.Fatalf("seed writes and exports nothing: %+v", c)
	}
	if c := byPath[mod+"/cmd/seed-mirror"]; len(c.Export) == 0 || len(c.Write) != 0 {
		t.Fatalf("seed-mirror exports and writes nothing: %+v", c)
	}

	src := clean()
	src["mirror/evil.go"] = "package mirror\nimport \"" + mod + "/internal/ledger\"\nfunc leak(s *ledger.Store) { s.Append(nil) }\n"
	expectViolation(t, check(t, src), "mirror imports the coordination writer")
	expectViolation(t, check(t, src), "holds both an export path")

	src = clean()
	src["cmd/seed/mirror.go"] = "package main\nimport \"" + mod + "/mirror\"\nvar _ mirror.Adapter\n"
	expectViolation(t, check(t, src), "cmd/seed holds both an export path")

	src = clean()
	src["internal/hidden/h.go"] = "package hidden\nimport \"context\"\ntype gitlab struct{}\nfunc (*gitlab) Name() string { return \"\" }\nfunc (*gitlab) List(context.Context) ([]int, error) { return nil, nil }\nfunc (*gitlab) Create(context.Context, int) (int, error) { return 0, nil }\nfunc (*gitlab) Update(context.Context, string, int, []string) error { return nil }\n"
	expectViolation(t, check(t, src), "internal/hidden.gitlab declares the adapter method set outside the registry")

	src = clean()
	src["cmd/seed-mirror/flags.go"] = "package main\nimport \"flag\"\nvar ledgerDir = flag.String(\"ledger\", \"\", \"\")\n"
	expectViolation(t, check(t, src), "cmd/seed-mirror defines --ledger")

	src = clean()
	src["cmd/seed-mirror/push.go"] = "package main\nimport g \"" + mod + "/internal/gitref\"\nfunc push(c *g.Client) { c.Push(\"\") }\n"
	expectViolation(t, check(t, src), "cmd/seed-mirror holds both an export path")

	src = clean()
	src["cmd/seed-mirror/propose.go"] = "package main\nimport \"" + mod + "/internal/propose\"\nvar _ propose.Client\n"
	expectViolation(t, check(t, src), "coordination write path (example.com/m/internal/propose)")
}

// conformance: III.D row 6 (plans/os-b45c308d.md D3, AC3) — the
// real-tree walk: no executable's closure holds both classes;
// seed-mirror is on the export path with no write path and no
// Seed-side flag; seed and seed-admit are on the write path and do
// not import the registry; both derived classes are non-empty, so the
// lint is proving something.
func TestAuthorityBoundaryHoldsInTheTree(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this source file")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	g, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(g)
	for _, v := range rep.Violations {
		t.Errorf("authority boundary: %s", v)
	}
	if len(rep.Exporters) == 0 || len(rep.Writers) == 0 {
		t.Fatalf("both classes derive non-empty: exporters %v writers %v", rep.Exporters, rep.Writers)
	}
	byPath := map[string]Component{}
	for _, c := range rep.Components {
		byPath[c.Path] = c
	}
	m := g.Module
	if c, ok := byPath[m+"/cmd/seed-mirror"]; !ok || len(c.Export) == 0 || len(c.Write) != 0 {
		t.Fatalf("seed-mirror is the export component and holds no write path: %+v", c)
	}
	for _, name := range []string{"seed", "seed-admit"} {
		if c, ok := byPath[m+"/cmd/"+name]; !ok || len(c.Write) == 0 || len(c.Export) != 0 {
			t.Fatalf("%s writes coordination state and does not import the registry: %+v", name, c)
		}
	}
	for _, flag := range flagsDefined(g.Packages[m+"/cmd/seed-mirror"]) {
		for _, bad := range ForbiddenFlags {
			if flag == bad {
				t.Errorf("seed-mirror defines --%s", flag)
			}
		}
	}
}
