package promotion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// root is the repository root: this package sits at internal/promotion.
func root(t *testing.T) string {
	t.Helper()
	r, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func committed(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root(t), filepath.FromSlash(PacketPath)))
	if err != nil {
		t.Fatalf("the packet must be committed at %s: %v", PacketPath, err)
	}
	return string(b)
}

// TestPacketCitesRealDrills holds the committed packet to the tree:
// every cited drill is declared in its cited file, every criterion has
// a status from the vocabulary, and the III.R ledger is complete
// (plans/os-98ce6f8a.md D2). A packet that cites what the tree no
// longer holds is a stale claim at the promotion gate.
func TestPacketCitesRealDrills(t *testing.T) {
	p, err := Load(root(t))
	if err != nil {
		t.Fatalf("the committed packet does not parse: %v", err)
	}
	findings, err := Check(root(t), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Errorf("stale citation: %s", f)
	}
	if len(p.Criteria) != Criteria {
		t.Fatalf("criteria: got %d, want %d", len(p.Criteria), Criteria)
	}
	if len(p.Citations) < Criteria {
		t.Fatalf("the packet cites %d drills; a packet with fewer citations than criteria is not evidence", len(p.Citations))
	}
	if len(p.Drills()) == 0 {
		t.Fatal("Drills() must list the cited set")
	}
}

// TestPacketWritesTheCutoverDown is criterion 5's own evidence: the
// cutover and rollback section answers the build plan's three clauses
// by name (plans/os-98ce6f8a.md D4), and the two cutovers are presented
// as reserved decisions rather than choices (D3).
func TestPacketWritesTheCutoverDown(t *testing.T) {
	doc := committed(t)
	for _, heading := range []string{
		"## The cutover and the rollback",
		"### Which entry point flips when",
		"### What stays authoritative where during the window",
		"### The path back",
		"## The two cutovers are escalations",
		"## The III.R measurement ledger",
		"## The shadow run, as a protocol",
	} {
		if !strings.Contains(doc, heading+"\n") {
			t.Errorf("the packet lacks the section %q", heading)
		}
	}
	p, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Criteria {
		if c.Status == "reserved" && !strings.HasSuffix(c.Question, "?") {
			t.Errorf("criterion %d is reserved and its question does not end in a question mark: %q", c.Number, c.Question)
		}
	}
	// Both cutovers stand as questions the parser models, so a deleted
	// or answered one is a parse failure and not prose read past.
	if len(p.Escalations) != len(EscalationNames) {
		t.Fatalf("the packet presents %d cutovers as questions, want %v", len(p.Escalations), EscalationNames)
	}
	for i, e := range p.Escalations {
		if e.Name != EscalationNames[i] || !strings.HasSuffix(e.Question, "?") {
			t.Errorf("escalation %d: %+v", i, e)
		}
	}
	for _, mutation := range []string{
		strings.Replace(doc, "**Distribution.** Question:", "**Distribution.** Answer:", 1),
		strings.Replace(doc, "**Self-hosting.** Question:", "**Self-hosting.** Decided:", 1),
	} {
		if _, err := Parse(mutation); err == nil || !strings.Contains(err.Error(), "presents no question") {
			t.Errorf("a cutover answered in place must fail the parse: %v", err)
		}
	}
	for _, m := range p.Ledger {
		if m.Status != "not measured" {
			t.Errorf("ledger row %s claims %q; no measurement exists before the shadow run (D5)", m.Row, m.Status)
		}
	}
}

// TestPlantedBogusCitationFails is the mutation evidence: a drill the
// tree does not declare, and a file the tree does not hold, each fail
// by name.
func TestPlantedBogusCitationFails(t *testing.T) {
	doc := committed(t)
	p, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	first := p.Citations[0]
	cases := map[string]string{
		"a drill that does not exist": strings.Replace(doc, "`"+first.Drill+"`", "`TestNoSuchDrillExistsAnywhere`", 1),
		"a file that does not exist":  strings.Replace(doc, "`"+first.File+"`", "`cmd/seed/no_such_file_test.go`", 1),
	}
	for name, mutated := range cases {
		mp, err := Parse(mutated)
		if err != nil {
			t.Fatalf("%s: the mutated packet must still parse: %v", name, err)
		}
		findings, err := Check(root(t), mp)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) == 0 {
			t.Fatalf("%s: the check reported nothing", name)
		}
		got := findings[0].String()
		if !strings.Contains(got, "NoSuchDrillExistsAnywhere") && !strings.Contains(got, "no_such_file_test.go") {
			t.Errorf("%s: the finding does not name the citation: %s", name, got)
		}
	}
}

const minimal = `# packet

## 1. One
Status: met
| ` + "`TestA`" + ` | ` + "`x/a_test.go`" + ` | #1 |
## 2. Two
Status: partial
Missing: the rest.
| ` + "`TestB`" + ` | ` + "`x/b_test.go`" + ` | #2 |
## 3. Three
Status: not started
Missing: everything.
## 4. Four
Status: reserved
Missing: the decision.
Question: which?
## 5. Five
Status: met
| ` + "`TestE`" + ` | ` + "`x/e_test.go`" + ` | #5 |
## 6. Six
Status: met
| ` + "`TestF`" + ` | ` + "`x/f_test.go`" + ` | #6 |
## 7. Seven
Status: met
| ` + "`TestG`" + ` | ` + "`x/g_test.go`" + ` | #7 |
## Ledger
| row | measure | surface | status |
|---|---|---|---|
| R.1 | m | s | not measured |
| R.2 | m | s | not measured |
| R.3 | m | s | not measured |
| R.4 | m | s | not measured |
| R.5 | m | s | not measured |
| R.6 | m | s | not measured |
| R.7 | m | s | measured |
## The two cutovers are escalations
**Self-hosting.** Question: move now?
**Distribution.** Question: publish where?
`

// TestParseIsStrict pins the shape: each departure from the plan's
// packet fails naming the departure (plans/os-98ce6f8a.md D1, D5).
func TestParseIsStrict(t *testing.T) {
	if p, err := Parse(minimal); err != nil {
		t.Fatalf("the minimal packet must parse: %v", err)
	} else if len(p.Citations) != 5 || len(p.Ledger) != 7 || p.Criteria[3].Question != "which?" || len(p.Escalations) != 2 || p.Escalations[1].Question != "publish where?" {
		t.Fatalf("parsed shape: %d citations, %d ledger rows, question %q, escalations %+v", len(p.Citations), len(p.Ledger), p.Criteria[3].Question, p.Escalations)
	}
	cases := []struct{ name, from, to, want string }{
		{"status outside the vocabulary", "Status: not started", "Status: mostly", "outside the vocabulary"},
		{"met without evidence", "| `TestA` | `x/a_test.go` | #1 |", "", "met with no evidence row"},
		{"partial without a missing line", "Missing: the rest.", "", "does not say what is missing"},
		{"reserved without a question", "Question: which?", "", "asks no question"},
		{"no status", "Status: met\n| `TestE`", "| `TestE`", "has no status line"},
		{"two statuses", "Status: met\n| `TestE`", "Status: met\nStatus: met\n| `TestE`", "second status line"},
		{"criterion missing", "## 7. Seven\nStatus: met\n| `TestG` | `x/g_test.go` | #7 |\n", "", "criterion sections"},
		{"criterion out of order", "## 6. Six", "## 8. Six", "out of order"},
		{"criterion twice", "## 6. Six", "## 5. Six", "appears twice"},
		{"ledger row missing", "| R.4 | m | s | not measured |\n", "", "lacks row R.4"},
		{"ledger row twice", "| R.4 | m | s | not measured |", "| R.3 | m | s | not measured |", "appears twice"},
		{"ledger status outside the vocabulary", "| R.7 | m | s | measured |", "| R.7 | m | s | soon |", "outside the vocabulary"},
		{"ledger row short", "| R.7 | m | s | measured |", "| R.7 | m | measured |", "has 3 cells"},
		{"ledger row empty measure", "| R.7 | m | s | measured |", "| R.7 |  | s | measured |", "names no measure"},
		{"evidence row short", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | #1 |", "this one has 2 cells"},
		{"evidence row malformed", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | x/a_test.go | #1 |", "an evidence row is"},
		{"evidence row not a test file", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | `x/a.go` | #1 |", "not a test file"},
		{"evidence row climbs out of next", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | `../x/a_test.go` | #1 |", "not a clean relative path under next/"},
		{"evidence row absolute", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | `/x/a_test.go` | #1 |", "not a clean relative path under next/"},
		{"evidence row unclean", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | `x//a_test.go` | #1 |", "not a clean relative path under next/"},
		{"evidence row dot element", "| `TestA` | `x/a_test.go` | #1 |", "| `TestA` | `./x/a_test.go` | #1 |", "not a clean relative path under next/"},
		{"escalation deleted", "**Distribution.** Question: publish where?\n", "", "presents no question for the Distribution cutover"},
		{"escalation answered", "**Self-hosting.** Question: move now?", "**Self-hosting.** Yes, at the position the window closed.", "presents no question for the Self-hosting cutover"},
		{"escalation not a question", "**Self-hosting.** Question: move now?", "**Self-hosting.** Question: move now.", "does not end in a question mark"},
		{"escalation twice", "**Distribution.** Question: publish where?", "**Self-hosting.** Question: publish where?", "presented twice"},
		{"escalation unknown", "**Distribution.** Question: publish where?", "**Renaming.** Question: publish where?", "not one of the two cutovers"},
		{"escalations section missing", "## The two cutovers are escalations\n", "## The two cutovers\n", "presents no question for the Self-hosting cutover"},
	}
	for _, c := range cases {
		mutated := strings.Replace(minimal, c.from, c.to, 1)
		if mutated == minimal {
			t.Fatalf("%s: the mutation did not apply", c.name)
		}
		_, err := Parse(mutated)
		if err == nil {
			t.Errorf("%s: parsed", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not name %q", c.name, err, c.want)
		}
	}
}

// TestCheckReadsTheTree covers Check's arms on a synthetic tree: a
// declared drill passes, an undeclared one and a missing file fail,
// and a file read twice is read once.
func TestCheckReadsTheTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x", "a_test.go"), []byte("package x\n\nfunc TestA(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Packet{Citations: []Citation{
		{Drill: "TestA", File: "x/a_test.go", Line: 1},
		{Drill: "TestZ", File: "x/a_test.go", Line: 2},
		{Drill: "TestB", File: "x/b_test.go", Line: 3},
		{Drill: "TestC", File: "x/b_test.go", Line: 4},
	}}
	findings, err := Check(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 3 {
		t.Fatalf("findings: %v", findings)
	}
	if !strings.Contains(findings[0].Reason, "no such test") || !strings.Contains(findings[1].Reason, "does not exist") || !strings.Contains(findings[2].Reason, "does not exist") {
		t.Errorf("reasons: %v", findings)
	}
	if _, err := Load(dir); err == nil {
		t.Error("Load must fail where no packet exists")
	}
	if _, err := Check(dir, &Packet{Citations: []Citation{{Drill: "TestA", File: "x", Line: 5}}}); err == nil {
		t.Error("a citation naming a directory must surface the read error")
	}
}
