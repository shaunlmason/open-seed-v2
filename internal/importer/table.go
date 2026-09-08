// Package importer is the predecessor import (SEED-NEXT.md §II.17,
// Appendix D.2 and D.3; plans/os-cf13fb51.md): a lossless open-seed
// export, its anchors verified against the source history before any
// transform, transformed by a table into events an empty ledger admits
// through the same boundary every other record met, with a manifest
// giving every export record its disposition.
package importer

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed table.json
var tableJSON []byte

// Identity is how a v1 actor name enrolls: the kind the operator
// asserts and the name the roster shows.
type Identity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// VerbRow maps one v1 run-log verb: to an event family, or to a named
// drop with its reason. A drop is a row; a verb with no row is unmapped
// and the import refuses.
type VerbRow struct {
	Event string `json:"event,omitempty"`
	Drop  string `json:"drop,omitempty"`
}

// Table is the transform table, data the spec carries verbatim
// (spec/import-open-seed.json) and this package embeds.
type Table struct {
	Schema     string              `json:"schema"`
	Source     string              `json:"source"`
	Identities map[string]Identity `json:"identities"`
	States     map[string]string   `json:"states"`
	Verbs      map[string]VerbRow  `json:"verbs"`
	Defaults   struct {
		Tier    string `json:"tier"`
		Budget  string `json:"budget"`
		Routing string `json:"routing"`
	} `json:"defaults"`
}

// TableJSON is the embedded table's bytes, for the spec-parity drill.
func TableJSON() []byte { return append([]byte{}, tableJSON...) }

// LoadTable parses a table and validates it: every row is an event or
// a drop, never both nor neither.
func LoadTable(b []byte) (*Table, error) {
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("the transform table does not parse: %v", err)
	}
	if t.Schema != "seed-import/0" || t.Source != "open-seed" {
		return nil, fmt.Errorf("the transform table is seed-import/0 for open-seed, got %s for %s", t.Schema, t.Source)
	}
	for verb, row := range t.Verbs {
		if (row.Event == "") == (row.Drop == "") {
			return nil, fmt.Errorf("verb %q: a row is an event or a named drop, never both nor neither", verb)
		}
	}
	for name, id := range t.Identities {
		if id.Kind != "human" && id.Kind != "agent" && id.Kind != "service" || strings.TrimSpace(id.Name) == "" {
			return nil, fmt.Errorf("identity %q: kind is human, agent or service and the name is non-empty", name)
		}
	}
	if t.Defaults.Tier == "" || t.Defaults.Budget == "" || t.Defaults.Routing == "" {
		return nil, fmt.Errorf("the table declares default tier, budget and routing")
	}
	return &t, nil
}

// DefaultTable is the embedded table.
func DefaultTable() (*Table, error) { return LoadTable(tableJSON) }

// Unmapped lists the run-log verbs the table has no row for.
func (t *Table) Unmapped(verbs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range verbs {
		if _, ok := t.Verbs[v]; !ok && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// IdentityFor resolves a v1 actor name: the table's row, or an agent
// under the name itself for a name the table does not know (listed in
// the manifest as such).
func (t *Table) IdentityFor(name string) (Identity, bool) {
	if id, ok := t.Identities[name]; ok {
		return id, true
	}
	n := name
	if strings.TrimSpace(n) == "" {
		n = "unattributed"
	}
	return Identity{Kind: "agent", Name: n}, false
}
