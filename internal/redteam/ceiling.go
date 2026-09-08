// Package redteam is the compromised-actor drill's declarative half
// (plans/os-465e356e.md; SEED-NEXT.md §I.2, "the architecture's
// definition of done"): the §I.2 ceiling as a table, loaded from the
// charter's own words, and the residual set. The drill (redteam_test.go)
// plays the §I.2 adversary — a valid key, a valid credential, arbitrary
// git — against the enforced reference deployment (cmd/seed-admit
// on a bare remote) and asserts, clause by clause, that the ceiling
// holds at the push: every prohibition refused, every permission
// admitted, coverage derived from this table both ways.
package redteam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Kind is a ceiling clause's polarity: a prohibition the adversary
// cannot cross, or a permission the ceiling grants it.
type Kind string

const (
	Prohibition Kind = "prohibition"
	Permission  Kind = "permission"
)

// Side names where a clause is enforced: the ledger ref (admission's
// rule set through the hook) or the code refs (the hook's code-ref
// half). A clause carries one or both.
type Side string

const (
	Ledger Side = "ledger"
	Code   Side = "code"
)

// Clause is one line of the §I.2 ceiling, in the charter's own words,
// with the Seed vocabulary it lands in and the sides it is enforced on.
type Clause struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	Text       string   `json:"text"`
	Sides      []Side   `json:"sides"`
	Vocabulary string   `json:"vocabulary"`
	SidesRaw   []string `json:"-"`
}

// Ceiling is the whole table.
type Ceiling struct {
	Source  string   `json:"source"`
	Clauses []Clause `json:"clauses"`
}

// LoadCeiling reads and validates the ceiling table: a non-empty set of
// clauses, each with an id, a known kind, a non-empty text and
// vocabulary, and at least one known side, ids unique. A malformed
// table refuses rather than letting the drill run against a corpus it
// cannot check both ways.
func LoadCeiling(path string) (*Ceiling, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Ceiling
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("ceiling table %s: %w", path, err)
	}
	if len(c.Clauses) == 0 {
		return nil, fmt.Errorf("ceiling table %s names no clauses — the charter's §I.2 ceiling is twelve", path)
	}
	seen := map[string]bool{}
	for i := range c.Clauses {
		cl := &c.Clauses[i]
		switch {
		case cl.ID == "":
			return nil, fmt.Errorf("clause %d has no id", i)
		case seen[cl.ID]:
			return nil, fmt.Errorf("clause id %q is not unique", cl.ID)
		case cl.Kind != Prohibition && cl.Kind != Permission:
			return nil, fmt.Errorf("clause %q kind %q is neither prohibition nor permission", cl.ID, cl.Kind)
		case cl.Text == "":
			return nil, fmt.Errorf("clause %q has no charter text", cl.ID)
		case cl.Vocabulary == "":
			return nil, fmt.Errorf("clause %q names no Seed vocabulary", cl.ID)
		case len(cl.Sides) == 0:
			return nil, fmt.Errorf("clause %q names no side", cl.ID)
		}
		for _, s := range cl.Sides {
			if s != Ledger && s != Code {
				return nil, fmt.Errorf("clause %q side %q is neither ledger nor code", cl.ID, s)
			}
		}
		seen[cl.ID] = true
	}
	return &c, nil
}

// Targets is the set of (clause, side) pairs the corpus must cover: a
// two-sided clause is two targets, so a clause missing one side fails
// coverage. The key is "<id>/<side>".
func (c *Ceiling) Targets() []string {
	var out []string
	for _, cl := range c.Clauses {
		for _, s := range cl.Sides {
			out = append(out, cl.ID+"/"+string(s))
		}
	}
	return out
}

// Clause returns the clause with the given id.
func (c *Ceiling) Clause(id string) (Clause, bool) {
	for _, cl := range c.Clauses {
		if cl.ID == id {
			return cl, true
		}
	}
	return Clause{}, false
}

// Residual is one place the adversary succeeds within the ceiling,
// named with why, what it can inflict and what stands in the way.
type Residual struct {
	Name           string `json:"name"`
	Why            string `json:"why"`
	Inflicts       string `json:"inflicts"`
	StandsInTheWay string `json:"stands_in_the_way"`
}

// Residuals is the residual table.
type Residuals struct {
	Residuals []Residual `json:"residuals"`
}

// LoadResiduals reads and validates the residual table: a non-empty
// set, each entry with all four fields, names unique.
func LoadResiduals(path string) (*Residuals, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var r Residuals
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("residual table %s: %w", path, err)
	}
	if len(r.Residuals) == 0 {
		return nil, fmt.Errorf("residual table %s names no residuals — an unnamed residual is a hidden one", path)
	}
	seen := map[string]bool{}
	for i, res := range r.Residuals {
		switch {
		case res.Name == "":
			return nil, fmt.Errorf("residual %d has no name", i)
		case seen[res.Name]:
			return nil, fmt.Errorf("residual name %q is not unique", res.Name)
		case res.Why == "" || res.Inflicts == "" || res.StandsInTheWay == "":
			return nil, fmt.Errorf("residual %q must carry why, what it inflicts and what stands in the way", res.Name)
		}
		seen[res.Name] = true
	}
	return &r, nil
}

// Names returns the residual names.
func (r *Residuals) Names() []string {
	out := make([]string, 0, len(r.Residuals))
	for _, res := range r.Residuals {
		out = append(out, res.Name)
	}
	return out
}
