package externalfact

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports are the owners an observation source may not
// import: the export side, the mutating forge adapters, and process
// execution.
var forbiddenImports = []string{
	"os/exec",
	"github.com/shaunlmason/open-seed-v2/mirror",
	"github.com/shaunlmason/open-seed-v2/internal/protections",
}

// lintSource judges one parsed production file of the observation
// package: no forbidden import; no HTTP method selector but MethodGet
// outside the graphql helper, and none but MethodPost inside it; no
// HTTP method string literal anywhere.
func lintSource(fset *token.FileSet, f *ast.File) []string {
	var findings []string
	for _, imp := range f.Imports {
		v, _ := strconv.Unquote(imp.Path.Value)
		for _, bad := range forbiddenImports {
			if v == bad {
				findings = append(findings, fset.Position(imp.Pos()).String()+": imports "+v)
			}
		}
	}
	methods := map[string]bool{"MethodPost": true, "MethodPut": true, "MethodPatch": true, "MethodDelete": true, "MethodConnect": true, "MethodTrace": true}
	literals := map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true, "GET": true}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		inGraphQL := fn.Name.Name == "graphql"
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok || id.Name != "http" {
					return true
				}
				name := x.Sel.Name
				switch {
				case name == "MethodGet" && inGraphQL:
					findings = append(findings, fset.Position(x.Pos()).String()+": the graphql helper posts one query and nothing else")
				case methods[name] && !(inGraphQL && name == "MethodPost"):
					findings = append(findings, fset.Position(x.Pos()).String()+": http."+name+" in "+fn.Name.Name+", a source reads and never writes")
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if s, err := strconv.Unquote(x.Value); err == nil && literals[strings.ToUpper(s)] {
						findings = append(findings, fset.Position(x.Pos()).String()+": HTTP method literal "+x.Value)
					}
				}
			}
			return true
		})
	}
	return findings
}

// conformance: III.D row 7 (plans/os-b45c308d.md D6, AC6) — the
// observation-control lint over the real package: every production
// file of the observation component passes, and the one POST in the
// tree is the GraphQL query helper.
func TestObservationControlLint(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	posts := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range lintSource(fset, f) {
			t.Errorf("observation control: %s", v)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "MethodPost" {
				posts++
			}
			return true
		})
	}
	if posts != 1 {
		t.Fatalf("exactly one POST in the observation component, the GraphQL query: %d", posts)
	}
	// The sub-package of fakes is test support, but it is production
	// Go under this directory: it may not import the mutation owners
	// either.
	sub, err := parser.ParseDir(fset, filepath.Join(".", "facttest"), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sub {
		for name, f := range p.Files {
			for _, imp := range f.Imports {
				v, _ := strconv.Unquote(imp.Path.Value)
				for _, bad := range forbiddenImports {
					if v == bad {
						t.Errorf("%s imports %s", name, v)
					}
				}
			}
		}
	}
}

// The lint self-checks: a planted POST, a planted mutator import, a
// planted method literal and a GET inside the helper each fail by
// name; the real helper's shape passes; and the runtime helper
// refuses a mutation before it is sent.
func TestObservationControlLintSelfCheck(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(src string) *ast.File {
		f, err := parser.ParseFile(fset, "planted.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	for name, tc := range map[string]struct {
		src  string
		want string
	}{
		"planted POST": {
			`package x
import "net/http"
func merge() { _, _ = http.NewRequest(http.MethodPost, "u", nil) }`, "http.MethodPost in merge"},
		"planted PUT": {
			`package x
import "net/http"
func protect() { _, _ = http.NewRequest(http.MethodPut, "u", nil) }`, "http.MethodPut in protect"},
		"planted mutator import": {
			`package x
import _ "github.com/shaunlmason/open-seed-v2/internal/protections"
func read() {}`, "imports github.com/shaunlmason/open-seed-v2/internal/protections"},
		"planted mirror import": {
			`package x
import _ "github.com/shaunlmason/open-seed-v2/mirror"
func read() {}`, "imports github.com/shaunlmason/open-seed-v2/mirror"},
		"planted exec": {
			`package x
import "os/exec"
func run() { _ = exec.Command("gh") }`, "imports os/exec"},
		"planted method literal": {
			`package x
func read() string { return "DELETE" }`, `HTTP method literal "DELETE"`},
		"a GET inside the helper": {
			`package x
import "net/http"
func graphql() { _, _ = http.NewRequest(http.MethodGet, "u", nil) }`, "the graphql helper posts one query"},
	} {
		findings := lintSource(fset, parse(tc.src))
		found := false
		for _, f := range findings {
			if strings.Contains(f, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no finding naming %q: %v", name, tc.want, findings)
		}
	}
	clean := parse(`package x
import "net/http"
func get() { _, _ = http.NewRequest(http.MethodGet, "u", nil) }
func graphql() { _, _ = http.NewRequest(http.MethodPost, "u/graphql", nil) }`)
	if findings := lintSource(fset, clean); len(findings) != 0 {
		t.Fatalf("a GET-only source with the one query helper passes: %v", findings)
	}
	g := NewGitHub("http://127.0.0.1:1", "o", "r", "tok")
	if err := g.graphql(`mutation { mergePullRequest(input: {pullRequestId: "x"}) { clientMutationId } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "queries only") {
		t.Fatalf("a planted mutation refuses before any request: %v", err)
	}
}
