package docs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// exitCode is one numeric exit allocation parsed from the envelope
// source: the constant name and its value.
type exitCode struct {
	name  string
	value int
}

// refineCode is one refinement code — a string code that shares a base
// exit, named by a Code* constant in the envelope package.
type refineCode struct {
	name string
	code string
}

// renderExitCodes reads the envelope package's own constants — the one
// source the exit allocations live in — and renders the numeric table
// and the refinement-code registry. Parsing the source (rather than a
// hand-kept slice) means a planted change to a constant fails
// `docs check` (plans/os-16e55c11.md D1). The refinement codes are the
// exported Code* string constants the Fail call sites cite.
func renderExitCodes(root string) (string, error) {
	src := filepath.Join(root, "internal", "envelope", "envelope.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		return "", err
	}
	var exits []exitCode
	var refs []refineCode
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, nm := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				switch {
				case strings.HasPrefix(nm.Name, "Exit") && lit.Kind == token.INT:
					v, err := strconv.Atoi(lit.Value)
					if err != nil {
						return "", fmt.Errorf("exit constant %s: %w", nm.Name, err)
					}
					exits = append(exits, exitCode{nm.Name, v})
				case strings.HasPrefix(nm.Name, "Code") && lit.Kind == token.STRING:
					unq, err := strconv.Unquote(lit.Value)
					if err != nil {
						return "", fmt.Errorf("refinement constant %s: %w", nm.Name, err)
					}
					refs = append(refs, refineCode{nm.Name, unq})
				}
			}
		}
	}
	sort.SliceStable(exits, func(i, j int) bool {
		if exits[i].value != exits[j].value {
			return exits[i].value < exits[j].value
		}
		return exits[i].name < exits[j].name
	})
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].name < refs[j].name })

	var b strings.Builder
	b.WriteString(preamble("internal/envelope/envelope.go constants"))
	b.WriteString("# Exit codes\n\nThe process exit allocations, read from the `envelope` package's own constants. `spec/envelope.md` explains each; this table is the authority for the enumeration.\n\n")
	b.WriteString("| Exit | Constant |\n|---|---|\n")
	for _, e := range exits {
		fmt.Fprintf(&b, "| %d | `%s` |\n", e.value, e.name)
	}
	b.WriteString("\n## Refinement codes\n\nString codes that refine a base exit with a finer message — same exit, same wire code family, a more specific `code`. Cited by the `Fail` call sites.\n\n")
	if len(refs) == 0 {
		b.WriteString("_None declared._\n")
	} else {
		b.WriteString("| Constant | Code |\n|---|---|\n")
		for _, r := range refs {
			fmt.Fprintf(&b, "| `%s` | `%s` |\n", r.name, r.code)
		}
	}
	return b.String(), nil
}
