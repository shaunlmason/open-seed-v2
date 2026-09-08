package externalfact

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

// specRows parses the normative table in spec/external-facts.md:
// verb, signers, since.
func specRows(t *testing.T) map[string]struct {
	signers []string
	since   string
} {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "external-facts.md"))
	if err != nil {
		t.Fatal(err)
	}
	tick := regexp.MustCompile("`([^`]+)`")
	rows := map[string]struct {
		signers []string
		since   string
	}{}
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.HasPrefix(line, "## The table")
			continue
		}
		if !in || !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 8 {
			t.Fatalf("a table row has six cells: %s", line)
		}
		verb := tick.FindStringSubmatch(cells[1])
		var signers []string
		for _, m := range tick.FindAllStringSubmatch(cells[3], -1) {
			signers = append(signers, m[1])
		}
		since := tick.FindStringSubmatch(cells[6])
		if verb == nil || since == nil || len(signers) == 0 {
			t.Fatalf("a table row names its verb, signers and version: %s", line)
		}
		rows[verb[1]] = struct {
			signers []string
			since   string
		}{signers, since[1]}
	}
	return rows
}

// observerVerbs parses spec/actors.md's capability table for
// every verb whose accepted set names observer.
func observerVerbs(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "actors.md"))
	if err != nil {
		t.Fatal(err)
	}
	tick := regexp.MustCompile("`([^`]+)`")
	var out []string
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.HasPrefix(line, "## Capabilities")
			continue
		}
		if !in || !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		verb := tick.FindStringSubmatch(cells[1])
		if verb == nil {
			continue
		}
		for _, m := range tick.FindAllStringSubmatch(cells[2], -1) {
			if m[1] == keyring.CapObserver {
				out = append(out, verb[1])
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

// conformance: III.D row 7 (plans/os-b45c308d.md D4, AC4) — the code
// catalog and the normative table agree in both directions, every
// row's signer set is the keyring's row (so admission enforces it),
// every verb the keyring accepts for observer is catalogued, the
// catalog is sorted and closed, and plan.approved stays operator-only.
func TestExternalFactCatalogPins(t *testing.T) {
	spec := specRows(t)
	if !slices.IsSorted(Verbs()) || len(Verbs()) != len(Catalog) {
		t.Fatalf("the catalog is sorted by verb: %v", Verbs())
	}
	for _, f := range Catalog {
		row, ok := spec[f.Verb]
		if !ok {
			t.Errorf("%s is catalogued but the spec table has no row", f.Verb)
			continue
		}
		if !slices.Equal(row.signers, f.Signers) {
			t.Errorf("%s: the spec table says %v, the catalog %v", f.Verb, row.signers, f.Signers)
		}
		if row.since != f.Since {
			t.Errorf("%s: the spec table says %s, the catalog %s", f.Verb, row.since, f.Since)
		}
		if got := keyring.AcceptedCapabilities(f.Verb); !slices.Equal(got, f.Signers) {
			t.Errorf("%s: the keyring accepts %v, the catalog says %v", f.Verb, got, f.Signers)
		}
		if f.Authority == "" || f.Source == "" || f.Consequence == "" {
			t.Errorf("%s: every row names its authority, source and consequence", f.Verb)
		}
	}
	for verb := range spec {
		if _, ok := Lookup(verb); !ok {
			t.Errorf("the spec table lists %s, which the catalog does not hold", verb)
		}
	}
	// Conversely: every verb the observer capability admits is an
	// external fact, so a verb cannot join the observer's row without
	// joining the inventory.
	for _, verb := range observerVerbs(t) {
		if _, ok := Lookup(verb); !ok {
			t.Errorf("actors.md accepts observer for %s, which the catalog does not hold", verb)
		}
	}
	if got := keyring.AcceptedCapabilities("plan.approved"); !slices.Equal(got, []string{keyring.CapOperator}) {
		t.Fatalf("plan.approved stays operator-only, no observer fallback: %v", got)
	}
	if _, ok := Lookup("verdict.rendered"); ok {
		t.Fatal("a verdict is the verifier's judgment, not an external fact")
	}
	if _, ok := Lookup("request.filed"); ok {
		t.Fatal("a request asserts nothing about the world")
	}
}
