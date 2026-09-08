// Package authoritylint makes III.D row 6 a structural claim
// (plans/os-b45c308d.md D3; spec/projections.md "Components"): no
// deployable component holds both an export path and a coordination
// write path. A component is a main package under cmd/; its closure is
// the transitive import graph parsed from source; the two classes are
// derived from the packages and call sites that own them, never from a
// maintained list of names. The real-tree walk runs under go test, so
// a future executable importing both fails check-next by name.
package authoritylint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Package is one parsed production package: its import path, whether
// it is a main package, its imports, and its files.
type Package struct {
	Path    string
	Main    bool
	Imports []string
	Files   map[string]*ast.File
}

// Graph is the module's production package graph.
type Graph struct {
	Module   string
	Packages map[string]*Package
	fset     *token.FileSet
}

// The primitives the coordination write class is derived from: the
// package that owns each, and the method names whose call sites make a
// caller a writer. A package is a writer when it owns a primitive or
// calls one; a component is on the write path when its closure holds a
// writer.
var writePrimitives = map[string][]string{
	"internal/ledger":  {"Append", "AppendAll"},
	"internal/gitref":  {"Commit", "CommitAndPush", "Push"},
	"internal/propose": {"Propose"},
}

// RegistryDir is the sealed mirror registry's directory under the
// module: the package every export path is derived from.
const RegistryDir = "mirror"

// adapterMethods is the mirror adapter's method set: a type declaring
// all four outside the registry package is an adapter hidden from the
// suite, which the lint refuses.
var adapterMethods = []string{"Name", "List", "Create", "Update"}

// ForbiddenFlags are the Seed-side flags a component on the export
// path may not define: each names a ledger, a key, a remote, a
// declaration or a proposal, none of which the mirror has.
var ForbiddenFlags = []string{"ledger", "key", "remote", "config", "as", "propose", "admission"}

// Load parses every non-test production package under root, a module
// directory with a go.mod. testdata directories are skipped.
func Load(root string) (*Graph, error) {
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	module := ""
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "module ") {
			module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			break
		}
	}
	if module == "" {
		return nil, fmt.Errorf("%s/go.mod names no module", root)
	}
	sources := map[string]string{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || (d.Name() != "." && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Synthetic(module, sources)
}

// Synthetic builds a graph from sources keyed by module-relative file
// path ("cmd/x/main.go"), so the detector's own cases can be planted
// without touching the tree.
func Synthetic(module string, sources map[string]string) (*Graph, error) {
	g := &Graph{Module: module, Packages: map[string]*Package{}, fset: token.NewFileSet()}
	for rel, src := range sources {
		dir := filepath.ToSlash(filepath.Dir(rel))
		path := module
		if dir != "." {
			path = module + "/" + dir
		}
		f, err := parser.ParseFile(g.fset, rel, src, 0)
		if err != nil {
			return nil, err
		}
		p := g.Packages[path]
		if p == nil {
			p = &Package{Path: path, Files: map[string]*ast.File{}}
			g.Packages[path] = p
		}
		if f.Name.Name == "main" {
			p.Main = true
		}
		p.Files[rel] = f
		for _, imp := range f.Imports {
			v, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(v, module+"/") || v == module {
				p.Imports = append(p.Imports, v)
			}
		}
	}
	for _, p := range g.Packages {
		sort.Strings(p.Imports)
		p.Imports = dedupe(p.Imports)
	}
	return g, nil
}

func dedupe(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// Closure is a package's transitive import closure within the module,
// itself included, sorted.
func (g *Graph) Closure(path string) []string {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		if pkg := g.Packages[p]; pkg != nil {
			for _, imp := range pkg.Imports {
				walk(imp)
			}
		}
	}
	walk(path)
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Component is one executable and the classified members of its
// closure.
type Component struct {
	Path   string   `json:"path"`
	Export []string `json:"export,omitempty"`
	Write  []string `json:"write,omitempty"`
}

// Report is the lint's finding over a graph.
type Report struct {
	Components []Component `json:"components"`
	// Exporters and Writers are the derived classes over every package.
	Exporters  []string `json:"exporters"`
	Writers    []string `json:"writers"`
	Violations []string `json:"violations"`
}

func (g *Graph) registryPath() string { return g.Module + "/" + RegistryDir }

// exporters derives the export class: the sealed registry package and
// every package under it.
func (g *Graph) exporters() map[string]bool {
	out := map[string]bool{}
	reg := g.registryPath()
	for p := range g.Packages {
		if p == reg || strings.HasPrefix(p, reg+"/") {
			out[p] = true
		}
	}
	return out
}

// writers derives the coordination write class: the packages owning a
// write primitive and every package with a call site of one.
func (g *Graph) writers() (map[string]bool, map[string][]string) {
	out := map[string]bool{}
	why := map[string][]string{}
	owners := map[string][]string{}
	for dir, methods := range writePrimitives {
		owners[g.Module+"/"+dir] = methods
	}
	for p := range g.Packages {
		if _, owns := owners[p]; owns {
			out[p] = true
			why[p] = append(why[p], "owns a write primitive")
		}
	}
	for path, pkg := range g.Packages {
		for file, f := range pkg.Files {
			// The local names the file binds each primitive owner to,
			// so an aliased import is seen through.
			names := map[string][]string{}
			for _, imp := range f.Imports {
				v, _ := strconv.Unquote(imp.Path.Value)
				methods, owns := owners[v]
				if !owns {
					continue
				}
				name := v[strings.LastIndex(v, "/")+1:]
				if imp.Name != nil {
					name = imp.Name.Name
				}
				names[name] = methods
			}
			if len(names) == 0 {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				for _, methods := range names {
					for _, m := range methods {
						if sel.Sel.Name == m {
							out[path] = true
							why[path] = append(why[path], fmt.Sprintf("%s calls .%s", file, m))
						}
					}
				}
				return true
			})
		}
	}
	return out, why
}

// hiddenAdapters finds a type outside the registry declaring the
// adapter method set.
func (g *Graph) hiddenAdapters() []string {
	var out []string
	reg := g.registryPath()
	for path, pkg := range g.Packages {
		if path == reg {
			continue
		}
		have := map[string]map[string]bool{}
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				recv := receiverName(fn.Recv.List[0].Type)
				if recv == "" {
					continue
				}
				if have[recv] == nil {
					have[recv] = map[string]bool{}
				}
				have[recv][fn.Name.Name] = true
			}
		}
		for recv, methods := range have {
			all := true
			for _, m := range adapterMethods {
				if !methods[m] {
					all = false
				}
			}
			if all {
				out = append(out, fmt.Sprintf("%s.%s declares the adapter method set outside the registry", path, recv))
			}
		}
	}
	sort.Strings(out)
	return out
}

func receiverName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return receiverName(x.X)
	case *ast.IndexListExpr:
		return receiverName(x.X)
	}
	return ""
}

// flagsDefined lists the flag names a package's files define through
// the flag package's definers, whatever the FlagSet is called.
func flagsDefined(pkg *Package) []string {
	definers := map[string]bool{"String": true, "Int": true, "Bool": true, "Duration": true, "Float64": true, "Var": true,
		"StringVar": true, "IntVar": true, "BoolVar": true, "DurationVar": true, "Float64Var": true}
	var out []string
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !definers[sel.Sel.Name] {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil {
					out = append(out, s)
				}
				break
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

// Check derives the classes and judges every component.
func Check(g *Graph) Report {
	rep := Report{}
	exporters := g.exporters()
	writers, why := g.writers()
	for p := range exporters {
		rep.Exporters = append(rep.Exporters, p)
	}
	for p := range writers {
		rep.Writers = append(rep.Writers, p)
	}
	sort.Strings(rep.Exporters)
	sort.Strings(rep.Writers)
	// An export package importing a writer is refused at the package,
	// before any component is judged.
	for p := range exporters {
		for _, imp := range g.Packages[p].Imports {
			if writers[imp] {
				rep.Violations = append(rep.Violations, fmt.Sprintf("%s imports the coordination writer %s (%s)", p, imp, strings.Join(why[imp], "; ")))
			}
		}
	}
	rep.Violations = append(rep.Violations, g.hiddenAdapters()...)
	var mains []string
	for p, pkg := range g.Packages {
		if pkg.Main {
			mains = append(mains, p)
		}
	}
	sort.Strings(mains)
	for _, m := range mains {
		c := Component{Path: m}
		for _, p := range g.Closure(m) {
			if exporters[p] {
				c.Export = append(c.Export, p)
			}
			if writers[p] {
				c.Write = append(c.Write, p)
			}
		}
		if len(c.Export) > 0 && len(c.Write) > 0 {
			rep.Violations = append(rep.Violations, fmt.Sprintf("%s holds both an export path (%s) and a coordination write path (%s)", m, strings.Join(c.Export, ", "), strings.Join(c.Write, ", ")))
		}
		if len(c.Export) > 0 {
			for _, name := range flagsDefined(g.Packages[m]) {
				for _, bad := range ForbiddenFlags {
					if name == bad {
						rep.Violations = append(rep.Violations, fmt.Sprintf("%s defines --%s, a Seed-side flag no export component may take", m, name))
					}
				}
			}
		}
		rep.Components = append(rep.Components, c)
	}
	sort.Strings(rep.Violations)
	return rep
}
