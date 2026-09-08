package envelope

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// conformance: III.I groundwork; every verb response is a versioned,
// schema-stable envelope with structured errors and meaningful exit codes.
// This test pins the serialized field set: adding, removing, or renaming a
// field is a schema change and must bump V alongside spec/envelope.md.
func TestEnvelopeSchemaStable(t *testing.T) {
	var buf bytes.Buffer
	if err := OK(map[string]any{"k": "v"}).Render(&buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := `{"v":"seed-envelope/0","ok":true,"result":{"k":"v"},"error":null,"position":null,"affordances":[],"budget":null,"exit":0}` + "\n"
	if got != want {
		t.Fatalf("success envelope drifted from the spec shape:\n got %s\nwant %s", got, want)
	}

	buf.Reset()
	if err := Fail(ExitUsage, "usage", "missing verb").Render(&buf); err != nil {
		t.Fatal(err)
	}
	got = buf.String()
	want = `{"v":"seed-envelope/0","ok":false,"result":null,"error":{"code":"usage","message":"missing verb"},"position":null,"affordances":[],"budget":null,"exit":64}` + "\n"
	if got != want {
		t.Fatalf("refusal envelope drifted from the spec shape:\n got %s\nwant %s", got, want)
	}
}

// conformance: III.I groundwork; exit codes are meaningful and
// documented. The spec table is authoritative and these constants
// mirror it exactly — asserted in BOTH directions, by PARSING the
// table rather than by copying it.
//
// The copy is what failed. This drill used to be a hand-written map of
// 18 of the 26 constants, and a hand-written map cannot notice a
// constant it was never told about: adding a code took three edits
// (the constant, the spec row, this list) with nothing forcing the
// second or third. Five codes drifted through that gap — seal_broken,
// not_recipient, unsealed, red_locked and lane_invalid were emitted by
// shipped code while absent from the table the spec calls
// authoritative, against envelope.md's own rule that a code "lands as
// a PR editing this table before any code emits it".
//
// Both directions are load-bearing. A one-way drill (every constant
// has a row) passes a table carrying rows for codes nothing emits,
// which is how a retired code keeps its number reserved forever; the
// other way is what those five actually violated.
func TestExitCodesMatchSpecTable(t *testing.T) {
	table, err := specTable()
	if err != nil {
		t.Fatal(err)
	}
	consts, err := constantTable()
	if err != nil {
		t.Fatal(err)
	}
	if len(table) < 20 || len(consts) < 20 {
		t.Fatalf("the pin parsed %d rows and %d constants; one of the two readers is broken, "+
			"and a parity drill that parses nothing agrees with everything", len(table), len(consts))
	}
	for name, code := range consts {
		row, ok := table[name]
		if !ok {
			t.Errorf("constant %s = %d has NO ROW in spec/envelope.md — the table is authoritative "+
				"and a code lands there before any code emits it", name, code)
			continue
		}
		if row != code {
			t.Errorf("%s: constant %d, table %d", name, code, row)
		}
	}
	for name, code := range table {
		if _, ok := consts[name]; !ok {
			t.Errorf("the table lists %s = %d with no constant in internal/envelope — a row for a "+
				"code nothing emits reserves a number forever", name, code)
		}
	}
}

// specTable parses spec/envelope.md's allocation table: rows of
// "| <n> | `<name>` | …".
func specTable() (map[string]int, error) {
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "envelope.md"))
	if err != nil {
		return nil, err
	}
	row := regexp.MustCompile("^\\|\\s*(\\d+)\\s*\\|\\s*`([a-z_]+)`\\s*\\|")
	out := map[string]int{}
	// Bounded to the ALLOCATION table. The refinements table below the
	// allocation rule has the same row shape and names codes that are
	// deliberately not constants, so an unbounded parse would read them
	// as rows with no constant and fail the parity it exists to prove.
	body := string(b)
	if i := strings.Index(body, "**Allocation rule for new codes**"); i >= 0 {
		body = body[:i]
	}
	for _, line := range strings.Split(body, "\n") {
		m := row.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, err
		}
		out[m[2]] = n
	}
	return out, nil
}

// constantTable reads the Exit* constants out of this package's own
// source and maps them to their wire names. Deriving both sides is the
// point: a constant added without a row appears here whether or not
// anyone remembered this test.
func constantTable() (map[string]int, error) {
	b, err := os.ReadFile("envelope.go")
	if err != nil {
		return nil, err
	}
	decl := regexp.MustCompile(`^\s*Exit([A-Za-z]+)\s*=\s*(\d+)\s*$`)
	out := map[string]int{}
	for _, line := range strings.Split(string(b), "\n") {
		m := decl.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return nil, err
		}
		out[wireName(m[1])] = n
	}
	return out, nil
}

// wireName maps a constant's Go spelling to the code string the
// envelope carries. Most are the snake_case of the identifier; the
// listed pairs are the ones where the constant and the wire name were
// deliberately given different words.
func wireName(id string) string {
	switch id {
	case "OK":
		return "ok"
	case "Fenced":
		return "fenced_out"
	case "ClassificationRef":
		// The constant abbreviates; the wire name does not.
		return "classification_refused"
	}
	var b strings.Builder
	for i, r := range id {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestRenderSingleLine(t *testing.T) {
	var buf bytes.Buffer
	if err := Fail(ExitNotFound, "not_found", "no such subject").Render(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.HasSuffix(s, "\n") || strings.Count(s, "\n") != 1 {
		t.Fatalf("envelope must render as exactly one JSON line, got %q", s)
	}
}

func TestRenderErrorPath(t *testing.T) {
	e := OK(map[string]any{"bad": make(chan int)})
	if err := e.Render(&bytes.Buffer{}); err == nil {
		t.Fatal("expected a marshal error for an unserializable result")
	}
}

// conformance: the OTHER half of the parity, and the one the first
// version could not see. TestExitCodesMatchSpecTable derives a wire
// name by transforming a CONSTANT IDENTIFIER, so it proves nothing
// whatever about the string a caller actually receives — a call site
// passing any code at all with a documented exit was invisible to it
// (review finding on #208). This drill reads the emitted pairs out of
// the tree and requires every one of them to be in the spec: either as
// its exit's own row, or as a listed refinement of it.
//
// So the spec is now authoritative over the two things that were
// separately drifting: which numbers exist, and which strings ship on
// them.
func TestEmittedCodesAppearInTheTable(t *testing.T) {
	primary, err := specTable()
	if err != nil {
		t.Fatal(err)
	}
	byExit := map[int]string{}
	for name, code := range primary {
		byExit[code] = name
	}
	refinements, err := refinementTable()
	if err != nil {
		t.Fatal(err)
	}
	constants, err := constantTable()
	if err != nil {
		t.Fatal(err)
	}
	emitted, err := emittedPairs()
	if err != nil {
		t.Fatal(err)
	}
	if len(emitted) == 0 {
		t.Fatal("no emissions found: the scan is broken, and a scan that finds nothing agrees with everything")
	}
	for pair := range emitted {
		// constants is keyed by WIRE NAME, so the identifier is mapped
		// through the same transform the parity drill uses.
		exit, ok := constants[wireName(strings.TrimPrefix(pair.constant, "Exit"))]
		if !ok {
			t.Errorf("%s is emitted with code %q and is not a constant in this package",
				pair.constant, pair.code)
			continue
		}
		if byExit[exit] == pair.code {
			continue
		}
		if refinements[pair.code] == exit {
			continue
		}
		if n, listed := refinements[pair.code]; listed {
			t.Errorf("code %q is documented as a refinement of exit %d and is emitted on exit %d (%s)",
				pair.code, n, exit, pair.constant)
			continue
		}
		t.Errorf("exit %d ships code %q, and spec/envelope.md says exit %d is %q: a second code on "+
			"an exit is a REFINEMENT and belongs in the refinements table, or it is a different meaning "+
			"and takes its own exit — either way the table decides, not the call site",
			exit, pair.code, exit, byExit[exit])
	}
}

type emission struct{ constant, code string }

// emittedPairs scans the tree for envelope.Fail(envelope.ExitX, "code")
// and returns the distinct pairs. Test files are excluded: a drill
// constructing an envelope is not the CLI shipping one, and including
// them would let a test's fixture code silently satisfy the spec.
func emittedPairs() (map[emission]bool, error) {
	call := regexp.MustCompile(`envelope\.Fail\(\s*envelope\.(Exit[A-Za-z]+)\s*,\s*"([a-z_]+)"`)
	// Inside this package the calls are unqualified.
	local := regexp.MustCompile(`\bFail\(\s*(Exit[A-Za-z]+)\s*,\s*"([a-z_]+)"`)
	out := map[emission]bool{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, re := range []*regexp.Regexp{call, local} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				out[emission{m[1], m[2]}] = true
			}
		}
		return nil
	})
	return out, err
}

// refinementTable parses the refinements listed below the allocation
// rule: "| <exit> | `<code>` | …". It is deliberately a SEPARATE table
// from the allocation one — a refinement has no constant of its own,
// which is exactly what distinguishes it from an allocation.
func refinementTable() (map[string]int, error) {
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "envelope.md"))
	if err != nil {
		return nil, err
	}
	body := string(b)
	i := strings.Index(body, "**Allocation rule for new codes**")
	if i < 0 {
		return nil, errors.New("spec/envelope.md no longer states the allocation rule")
	}
	row := regexp.MustCompile("^\\|\\s*(\\d+)\\s*\\|\\s*`([a-z_]+)`\\s*\\|")
	out := map[string]int{}
	for _, line := range strings.Split(body[i:], "\n") {
		m := row.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, err
		}
		out[m[2]] = n
	}
	return out, nil
}
