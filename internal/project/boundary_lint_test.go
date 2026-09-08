package project_test

// The write-boundary lints (plans/os-8d5e9c45.md step 2; conformance
// III.D "the write-boundary lint enforces it"), wired into check-next
// by construction: this file is a test in the suite check-next already
// runs. Lint A (vocabulary): no non-test Go file outside the engine
// carries the publication vocabulary literals, so nobody constructs
// projection paths by hand. Lint B (seam/write separation): a
// non-test file outside the engine that imports the engine makes no
// os write-family calls, so the file that can obtain a published path
// cannot write one. Both detectors are self-checked against planted
// fixtures: a lint that fails to fire is itself a test failure. The
// residual risks (cross-file splits, direct syscall, chmod-capable
// owners) are named in the spec; the locked trees close the first,
// nothing closes a root-privileged actor.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/project"
)

// The publication vocabulary (Lint A). The file names are the
// engine's own declarations, so those two cannot drift by
// construction (review finding on #118); the builds directory is
// unexported by design — exporting it would hand non-engine code the
// path piece the lint exists to deny — so it is pinned behaviorally
// against the published layout in TestVocabularyMatchesPublishedLayout.
var vocabularyLiterals = []string{project.CurrentFile, project.StampFile, "builds"}

// The os write family (Lint B): the calls that create, replace,
// remove, relink, or remode filesystem state.
var osWriteFamily = map[string]bool{
	"WriteFile": true, "Create": true, "OpenFile": true, "Rename": true,
	"Remove": true, "RemoveAll": true, "Mkdir": true, "MkdirAll": true,
	"Chmod": true, "Truncate": true, "Link": true, "Symlink": true,
}

const enginePath = "github.com/shaunlmason/open-seed-v2/internal/project"

// lintVocabulary reports every publication-vocabulary string literal
// in the file.
func lintVocabulary(fset *token.FileSet, f *ast.File) []string {
	var findings []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, v := range vocabularyLiterals {
			if s == v {
				findings = append(findings, fset.Position(lit.Pos()).String()+": publication vocabulary literal "+lit.Value)
			}
		}
		return true
	})
	return findings
}

// lintSeamWrites reports every os write-family call in a file that
// imports the engine; a file that does not import it reports nothing.
func lintSeamWrites(fset *token.FileSet, f *ast.File) []string {
	osName := ""
	importsEngine := false
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path == enginePath {
			importsEngine = true
		}
		if path == "os" {
			osName = "os"
			if imp.Name != nil {
				osName = imp.Name.Name
			}
		}
	}
	if !importsEngine || osName == "" || osName == "_" {
		return nil
	}
	var findings []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != osName || !osWriteFamily[sel.Sel.Name] {
			return true
		}
		findings = append(findings, fset.Position(sel.Pos()).String()+": os."+sel.Sel.Name+" in a file importing the projection engine")
		return true
	})
	return findings
}

// TestVocabularyMatchesPublishedLayout pins the lint's vocabulary to
// what the engine actually publishes on disk (review finding on
// #118): every vocabulary entry must name a live piece of a fresh
// layout, so a layout rename breaks this probe and moves the lint
// with it. This covers the unexported builds directory without
// exporting it, and catches even a constant the engine stops using.
func TestVocabularyMatchesPublishedLayout(t *testing.T) {
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(out, "roster")
	b, err := os.ReadFile(filepath.Join(root, vocabularyLiterals[0]))
	if err != nil {
		t.Fatalf("vocabulary %q is not the live pointer name: %v", vocabularyLiterals[0], err)
	}
	id := strings.TrimSpace(string(b))
	stamp := filepath.Join(root, vocabularyLiterals[2], id, vocabularyLiterals[1])
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("vocabulary %q / %q do not name the live layout: %v", vocabularyLiterals[2], vocabularyLiterals[1], err)
	}
}

func TestWriteBoundaryLints(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this source file")
	}
	engineDir := filepath.Dir(thisFile)
	moduleRoot := filepath.Dir(filepath.Dir(engineDir))
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(moduleRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		inEngine := strings.HasPrefix(p, engineDir+string(filepath.Separator))
		if !inEngine {
			violations = append(violations, lintVocabulary(fset, f)...)
			violations = append(violations, lintSeamWrites(fset, f)...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Errorf("write boundary: %s", v)
	}
}

func TestWriteBoundaryLintSelfCheck(t *testing.T) {
	parse := func(src string) (*token.FileSet, *ast.File) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "fixture.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return fset, f
	}

	fset, f := parse(`package evil
func path() string { return "builds" }
`)
	if got := lintVocabulary(fset, f); len(got) != 1 {
		t.Fatalf("the vocabulary detector must fire on a planted literal: %v", got)
	}

	fset, f = parse(`package evil
import (
	"os"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)
func clobber(tmp string) {
	p, _ := project.Current("out", "roster")
	_ = os.Rename(tmp, p)
}
`)
	if got := lintSeamWrites(fset, f); len(got) != 1 {
		t.Fatalf("the seam detector must fire on a planted rename: %v", got)
	}

	fset, f = parse(`package evil
import (
	stdos "os"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)
func clobber(tmp string) {
	p, _ := project.Current("out", "roster")
	_ = stdos.Remove(p)
}
`)
	if got := lintSeamWrites(fset, f); len(got) != 1 {
		t.Fatalf("the seam detector must see through an aliased os import: %v", got)
	}

	fset, f = parse(`package fine
import (
	"os"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)
func read() ([]byte, error) {
	p, _ := project.Current("out", "roster")
	return os.ReadFile(p)
}
`)
	if got := lintSeamWrites(fset, f); len(got) != 0 {
		t.Fatalf("reading through the seam is not a violation: %v", got)
	}

	fset, f = parse(`package fine
import "os"
func write(p string) error { return os.WriteFile(p, nil, 0o644) }
`)
	if got := lintSeamWrites(fset, f); len(got) != 0 {
		t.Fatalf("writers that hold no seam are outside Lint B: %v", got)
	}
}
