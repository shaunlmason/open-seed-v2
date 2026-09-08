package conformance_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/conformance"
)

const root = "../.."

func charter(t *testing.T) []conformance.CharterPillar {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, conformance.CharterPath))
	if err != nil {
		t.Fatal(err)
	}
	c, err := conformance.ParseCharter(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func table(t *testing.T) conformance.Table {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(conformance.TablePath)))
	if err != nil {
		t.Fatal(err)
	}
	tb, err := conformance.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// conformance: plans/os-83bc3d84.md D2, AC1 — the charter parses into
// its eighteen pillars and the committed table is the charter row for
// row: the same ids, titles, texts and postures.
func TestTableIsTheCharterRowForRow(t *testing.T) {
	c := charter(t)
	if len(c) != 18 || c[0].ID != "A" || c[17].ID != "R" {
		t.Fatalf("Part III is eighteen lettered pillars A through R: %d", len(c))
	}
	rows := 0
	for _, p := range c {
		rows += len(p.Rows)
		for _, r := range p.Rows {
			if strings.Contains(r.Text, "  ") || strings.HasPrefix(r.Text, "- [ ]") {
				t.Fatalf("%s.%d: the text is whitespace-normalized: %q", p.ID, r.Row, r.Text)
			}
		}
	}
	if rows == 0 {
		t.Fatal("no rows parsed")
	}
	if err := conformance.Validate(table(t), c); err != nil {
		t.Fatalf("the committed table is not the charter: %v", err)
	}
	if _, err := conformance.Load(root); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The enforced-only marker is read off the text: opening the row
	// it marks the whole row, inside it one clause (mixed).
	whole, mixed := 0, 0
	for _, p := range c {
		for _, r := range p.Rows {
			switch {
			case strings.HasPrefix(r.Text, conformance.Marker):
				if r.Posture != conformance.PostureEnforcedOnly {
					t.Fatalf("%s.%d opens with the marker and is not enforced-only", p.ID, r.Row)
				}
				whole++
			case strings.Contains(r.Text, conformance.Marker):
				if r.Posture != conformance.PostureMixed {
					t.Fatalf("%s.%d carries the marker inside a clause and is not mixed", p.ID, r.Row)
				}
				mixed++
			case r.Posture != conformance.PostureAny:
				t.Fatalf("%s.%d carries no marker and is not any", p.ID, r.Row)
			}
		}
	}
	if whole == 0 || mixed == 0 {
		t.Fatalf("the charter marks whole rows (%d) and clauses (%d) enforced-only", whole, mixed)
	}
}

// conformance: D2 — a row added to, removed from or reworded on either
// side fails validation by name.
func TestTableDriftFromTheCharterIsRefused(t *testing.T) {
	c := charter(t)
	tb := table(t)
	dropped := tb
	dropped.Pillars = append([]conformance.Pillar(nil), tb.Pillars...)
	p := dropped.Pillars[1]
	p.Rows = p.Rows[:len(p.Rows)-1]
	dropped.Pillars[1] = p
	if err := conformance.Validate(dropped, c); err == nil || !strings.Contains(err.Error(), "pillar B") {
		t.Fatalf("a charter row missing from the table is refused by pillar: %v", err)
	}
	reworded := table(t)
	reworded.Pillars[0].Rows[0].Text += " and more"
	if err := conformance.Validate(reworded, c); err == nil || !strings.Contains(err.Error(), "A.1") {
		t.Fatalf("a reworded row is refused by id: %v", err)
	}
	grown := c
	grown = append([]conformance.CharterPillar(nil), c...)
	grown[2].Rows = append(grown[2].Rows, conformance.CharterRow{Row: len(grown[2].Rows) + 1, Text: "a new criterion", Posture: conformance.PostureAny})
	if err := conformance.Validate(table(t), grown); err == nil || !strings.Contains(err.Error(), "pillar C") {
		t.Fatalf("a charter row the table lacks is refused: %v", err)
	}
	posture := table(t)
	posture.Pillars[1].Rows[0].Posture = conformance.PostureAny
	if err := conformance.Validate(posture, c); err == nil || !strings.Contains(err.Error(), "posture") {
		t.Fatalf("a posture that is not the charter's is refused: %v", err)
	}
}

// conformance: D1, AC2 — the vocabulary holds: a status outside it, a
// met row without evidence, a partial or routed row without a note.
func TestVocabularyHolds(t *testing.T) {
	c := charter(t)
	for _, tc := range []struct {
		name string
		edit func(r *conformance.Row)
		want string
	}{
		{"outside", func(r *conformance.Row) { r.Status = "done" }, "outside the vocabulary"},
		{"met without evidence", func(r *conformance.Row) { r.Status = conformance.StatusMet; r.Evidence = "" }, "names its evidence"},
		{"partial without note", func(r *conformance.Row) { r.Status = conformance.StatusPartial; r.Note = "" }, "names where the rest lives"},
		{"routed without note", func(r *conformance.Row) { r.Status = conformance.StatusRouted; r.Note = "" }, "names where the rest lives"},
	} {
		tb := table(t)
		tc.edit(&tb.Pillars[0].Rows[0])
		if err := conformance.Validate(tb, c); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	tb := table(t)
	tb.Pillars[0].Rows[0].Status = conformance.StatusOpen
	tb.Pillars[0].Rows[0].Evidence = ""
	if err := conformance.Validate(tb, c); err != nil {
		t.Fatalf("an open row needs nothing else: %v", err)
	}
}

// conformance: the table decodes strictly.
func TestParseIsStrict(t *testing.T) {
	if _, err := conformance.Parse([]byte(`{"pillars": [], "extra": 1}`)); err == nil {
		t.Fatal("an unknown field is refused")
	}
	if _, err := conformance.Parse([]byte(`{"pillars": []} {}`)); err == nil {
		t.Fatal("trailing bytes are refused")
	}
	// A stray closing delimiter is trailing bytes too: More would not
	// see it (review finding on the task PR).
	if _, err := conformance.Parse([]byte(`{"pillars": []}]`)); err == nil || !strings.Contains(err.Error(), "bytes follow") {
		t.Fatalf("a stray closing delimiter is refused: %v", err)
	}
	if _, err := conformance.Parse([]byte(`{"pillars": []}
`)); err != nil {
		t.Fatalf("a trailing newline is not trailing bytes: %v", err)
	}
	if _, err := conformance.ParseCharter([]byte("# nothing here\n")); err == nil {
		t.Fatal("a charter without Part III is refused")
	}
}

// conformance: D4, AC4 — complete means every applicable row is met:
// open, partial and routed rows are all outstanding, named by pillar
// and row with their status; the cooperative posture sets the
// enforced-only rows aside as documented-not-holding, judges the mixed
// rows while naming them, and is complete when what applies is met
// (the charter defines conformance at the declared posture).
func TestAssessJudgesAtThePosture(t *testing.T) {
	tb := conformance.Table{Pillars: []conformance.Pillar{{ID: "B", Title: "The Admission Boundary", Rows: []conformance.Row{
		{Row: 1, Text: "x", Posture: conformance.PostureEnforcedOnly, Status: conformance.StatusMet, Evidence: "#1"},
		{Row: 2, Text: "y", Posture: conformance.PostureAny, Status: conformance.StatusOpen, Phase: "13", Note: "item 6"},
		{Row: 3, Text: "z", Posture: conformance.PostureAny, Status: conformance.StatusRouted, Note: "backlog"},
		{Row: 4, Text: "w", Posture: conformance.PostureMixed, Status: conformance.StatusPartial, Note: "half"},
	}}}}
	rep := conformance.Assess(tb, true)
	ids := func(rows []conformance.OutstandingRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.ID+":"+r.Status)
		}
		return strings.Join(out, " ")
	}
	if rep.Complete || ids(rep.Outstanding) != "B.2:open B.3:routed B.4:partial" || rep.Outstanding[0].Phase != "13" || rep.Counts.Met != 1 || rep.Counts.Routed != 1 || rep.Counts.Open != 1 || rep.Counts.Partial != 1 || len(rep.NotApplicable) != 0 || len(rep.MixedHere) != 0 {
		t.Fatalf("enforced: every unmet row is outstanding, by status: %+v", rep)
	}
	for i := 1; i < 4; i++ {
		tb.Pillars[0].Rows[i].Status = conformance.StatusMet
		tb.Pillars[0].Rows[i].Evidence = "#2"
		tb.Pillars[0].Rows[i].Note = ""
	}
	rep = conformance.Assess(tb, true)
	if !rep.Complete || len(rep.Outstanding) != 0 || !strings.Contains(rep.Because, "every applicable row is met") {
		t.Fatalf("enforced with every row met: complete: %+v", rep)
	}
	rep = conformance.Assess(tb, false)
	if !rep.Complete || len(rep.NotApplicable) != 1 || rep.NotApplicable[0] != "B.1" || len(rep.MixedHere) != 1 || rep.MixedHere[0] != "B.4" || rep.Counts.Met != 3 || !strings.Contains(rep.Because, "cooperative") || !strings.Contains(rep.Because, "1 enforced-only rows documented") {
		t.Fatalf("cooperative with every applicable row met: complete, the enforced-only row documented as not holding, the mixed row judged and named: %+v", rep)
	}
	tb.Pillars[0].Rows[3].Status = conformance.StatusPartial
	tb.Pillars[0].Rows[3].Note = "half"
	if rep := conformance.Assess(tb, false); rep.Complete || len(rep.Outstanding) != 1 || rep.Outstanding[0].ID != "B.4" {
		t.Fatalf("cooperative with a mixed row partial: not complete, the row outstanding: %+v", rep)
	}
	b, _ := json.Marshal(rep)
	if !strings.Contains(string(b), `"outstanding_rows":[]`) {
		t.Fatalf("the outstanding rows render as an empty list, never null: %s", b)
	}
}

// notClaimed matches a note that parks a row on the grounds that its
// criterion was never claimed, rather than naming outstanding work or
// a measurement. It is deliberately the phrase and not the idea: a
// note may say a capability is caller-optional (III.H row 7) while
// still naming where the rest lives, and that is not this shape.
var notClaimed = regexp.MustCompile(`(?i)\bnot claimed\b|\bunclaimed\b`)

// conformance: os-9ef9ab34 D2 — a row is never left outstanding on the
// grounds that nobody claimed its criterion. Assess reports complete
// only when every applicable row is met, so a row parked that way can
// never be flipped by any later record and makes complete unreachable
// forever: III.B row 6, a permission, sat open with the note "not
// claimed: MAY" and would have blocked the Phase 13 exit record and
// promotion with it. A permission is met by abstention, and the table
// says so.
func TestNoRowIsOutstandingBecauseNobodyClaimedIt(t *testing.T) {
	tb := table(t)
	for _, p := range tb.Pillars {
		for _, r := range p.Rows {
			if r.Status != conformance.StatusMet && notClaimed.MatchString(r.Note) {
				t.Errorf("%s.%d is %s and its note excuses the criterion as unclaimed: %q\n"+
					"    a row parked this way can never be met, and Assess needs every row met to report complete",
					p.ID, r.Row, r.Status, r.Note)
			}
		}
	}
	// The guard fails on the shape it exists to catch, so it cannot
	// pass by matching nothing.
	planted := table(t)
	planted.Pillars[0].Rows[0].Status = conformance.StatusOpen
	planted.Pillars[0].Rows[0].Note = "not claimed: MAY; nobody has exercised it"
	found := false
	for _, r := range planted.Pillars[0].Rows {
		if r.Status != conformance.StatusMet && notClaimed.MatchString(r.Note) {
			found = true
		}
	}
	if !found {
		t.Fatal("the guard does not fire on a planted unclaimed row")
	}
}

// conformance: os-9ef9ab34 D1 — the one permission-shaped row in Part
// III is met by abstention, with its evidence naming what holds: a
// single intake path and ordering derived from the admitted chain.
func TestThePermissionRowIsMetByAbstention(t *testing.T) {
	tb := table(t)
	for _, p := range tb.Pillars {
		if p.ID != "B" {
			continue
		}
		for _, r := range p.Rows {
			if r.Row != 6 {
				continue
			}
			if !strings.Contains(r.Text, "may shard proposal intake") {
				t.Fatalf("III.B row 6 is no longer the permission this drill reads: %q", r.Text)
			}
			if r.Status != conformance.StatusMet {
				t.Errorf("the permission row is %s; a permission is met by abstention (os-9ef9ab34)", r.Status)
			}
			if !strings.Contains(r.Note, "abstention") {
				t.Errorf("the row's note states the reading: %q", r.Note)
			}
			return
		}
	}
	t.Fatal("III.B row 6 not found in the table")
}

// admissionFiles parses a package directory's non-test sources. It
// fails rather than returning nothing, so a drill reading the surface
// cannot pass because the surface moved out from under it.
func admissionFiles(t *testing.T, dir string) []*ast.File {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("no non-test sources under %s: this drill would pass vacuously", dir)
	}
	return files
}

// constructorsReturning names the exported functions whose result list
// holds want, written as it appears in the source. It reads the syntax
// only: no type checking, so it works on a planted file too.
func constructorsReturning(files []*ast.File, want string) []string {
	var names []string
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}
			for _, r := range fn.Type.Results.List {
				if types.ExprString(r.Type) == want {
					names = append(names, fn.Name.Name)
					break
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// conformance: os-9ef9ab34 D1 — the row's evidence, not just the row.
// III.B row 6 is a permission met by abstention, and abstention is a
// property of the tree rather than of the table: the row is earned by
// intake being single-path, so a drill that reads only the committed
// status can keep saying `met` after the tree starts sharding, and
// `seed doctor` would report complete without the semantics-equivalence
// evidence the note demands. This reads the admission surface instead.
// Sharded intake cannot be built without a per-shard admission context
// or a second rule set, because a shard applying the same rules over
// the same whole prefix is not a shard; both are visible in the syntax.
func TestIntakeIsSinglePathAsTheRowsEvidenceStates(t *testing.T) {
	const reEarn = "\n    III.B row 6 is met by abstention, and its evidence is that intake is single-path.\n" +
		"    A tree that shards re-earns the row by showing semantics unchanged (the row's note), or restates that evidence."

	admitPkg := admissionFiles(t, filepath.Join(root, "internal", "admit"))
	// Two context constructors, both whole-chain: one over a store's
	// materialized tip, one over a complete record prefix. A third that
	// selects a subset is what sharding needs.
	if got, want := constructorsReturning(admitPkg, "*Context"), []string{"ContextAt", "ContextOver"}; !slices.Equal(got, want) {
		t.Errorf("admission builds its context through %v, not the whole-prefix %v;"+reEarn, got, want)
	}
	// One rule set. A second is a second intake by definition: two
	// paths judging by different rules is the semantics change the
	// charter's clause is about.
	if got, want := constructorsReturning(admitPkg, "[]Rule"), []string{"Default"}; !slices.Equal(got, want) {
		t.Errorf("admission draws its rules from %v, not %v;"+reEarn, got, want)
	}

	// The boundary agrees: cmd/seed-admit's two intake paths (the
	// pre-receive hook and the propose service) reach admission through
	// those same constructors and nothing else.
	var refs []string
	seen := map[string]bool{}
	for _, f := range admissionFiles(t, filepath.Join(root, "cmd", "seed-admit")) {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "admit" || !strings.HasPrefix(sel.Sel.Name, "Context") {
				return true
			}
			if !seen[sel.Sel.Name] {
				seen[sel.Sel.Name] = true
				refs = append(refs, sel.Sel.Name)
			}
			return true
		})
	}
	sort.Strings(refs)
	if len(refs) == 0 {
		t.Error("cmd/seed-admit names no admission context: this arm would pass vacuously")
	}
	for _, name := range refs {
		switch name {
		case "Context", "ContextAt", "ContextOver":
		default:
			t.Errorf("the boundary reaches admission through admit.%s, which is neither whole-prefix constructor;"+reEarn, name)
		}
	}

	// The guard has teeth: the shape it exists to catch fails it.
	planted, err := parser.ParseFile(token.NewFileSet(), "shard.go", `package admit

func ContextForShard(shard int, records []*event.Record) (*Context, error) { return nil, nil }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := constructorsReturning([]*ast.File{planted}, "*Context"); !slices.Equal(got, []string{"ContextForShard"}) {
		t.Fatalf("the scanner does not see a planted per-shard constructor, got %v", got)
	}
}
