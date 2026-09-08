// Package tuple is the runtime tuple: the charter's configuration a
// qualification binds to (SEED-NEXT.md §II.5, glossary), spelled once.
// "What passes an eval is not an agent, it is a configuration tuple:
// principal, harness and version, model family and version, tool
// policy, environment profile." Grants cite one, adapters report one,
// and a materially different one is out of grant
// (plans/os-8e53ffd9.md).
package tuple

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Tuple is the five-field configuration. Every field is a non-empty
// string; harness and model carry their versions as "<name>/<version>"
// and "<family>/<version>" by convention, which the parser does not
// police: what it polices is presence, because drift is a per-field
// comparison and a missing field is a field that cannot drift.
type Tuple struct {
	Principal   string `json:"principal"`
	Harness     string `json:"harness"`
	Model       string `json:"model"`
	ToolPolicy  string `json:"tool_policy"`
	Environment string `json:"environment"`
}

// Fields names the five, in the order Diff reports them.
func Fields() []string {
	return []string{"principal", "harness", "model", "tool_policy", "environment"}
}

// Parse decodes a tuple strictly: the object must carry exactly the
// five fields, each a non-empty string. An unknown field refuses, so a
// misspelling cannot pass as an absent one.
func Parse(raw []byte) (Tuple, error) {
	var t Tuple
	if strings.TrimSpace(string(raw)) == "null" {
		return Tuple{}, fmt.Errorf("the tuple is null: a configuration is the strict object {principal, harness, model, tool_policy, environment} or absent, never null")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return Tuple{}, fmt.Errorf("the tuple is the strict object {principal, harness, model, tool_policy, environment}: %v", err)
	}
	if dec.More() {
		return Tuple{}, fmt.Errorf("the tuple is one object, and bytes follow it")
	}
	for _, f := range Fields() {
		if strings.TrimSpace(t.Field(f)) == "" {
			return Tuple{}, fmt.Errorf("tuple field %q is empty: every field of the runtime tuple is a non-empty string", f)
		}
	}
	return t, nil
}

// Field reads one named field, in Fields order, so a caller reporting on
// a field by name (a refusal, a missing-flag message) reads the same
// spelling Diff and Parse do.
func (t Tuple) Field(field string) string {
	switch field {
	case "principal":
		return t.Principal
	case "harness":
		return t.Harness
	case "model":
		return t.Model
	case "tool_policy":
		return t.ToolPolicy
	case "environment":
		return t.Environment
	}
	return ""
}

// Equal is per-field equality: the whole of "materially different" in
// v0, where no tolerance policy exists yet and the honest rule is that
// any difference is a difference (plans/os-8e53ffd9.md D4).
func (t Tuple) Equal(o Tuple) bool {
	_, _, _, differs := t.Diff(o)
	return !differs
}

// Diff names the FIRST field on which two tuples differ, with both
// values, so a refusal can say which one moved rather than that
// something did.
func (t Tuple) Diff(o Tuple) (field, have, want string, differs bool) {
	for _, f := range Fields() {
		if t.Field(f) != o.Field(f) {
			return f, t.Field(f), o.Field(f), true
		}
	}
	return "", "", "", false
}

// Complete reports whether every field is set: an adapter's static
// report is partial by design (a worktree cannot see which model a
// lane will call), and the caller fills the rest before declaring.
func (t Tuple) Complete() bool {
	for _, f := range Fields() {
		if strings.TrimSpace(t.Field(f)) == "" {
			return false
		}
	}
	return true
}

// Applies reports whether tuple semantics are active under the given
// protocol version, as a named list, never an ordering: seed/2
// introduced them (spec/qualification.md) and seed/3 and seed/4
// kept them. The list was not extended when seed/5 through seed/8 were
// registered, and the binaries that shipped those versions judged a
// tupleless grant, start and offer as valid at those positions, so
// those versions keep that recorded judgment here for good
// (spec/protocol.md: records at earlier positions keep their
// earlier judgment). seed/9 restores the semantics
// (plans/os-29e2fef2.md; the resumption needs a declared tuple on the
// forge loop's chain), and every version registered after it must be
// named here too.
func Applies(active string) bool {
	switch active {
	case version.Seed2, version.Seed3, version.Seed4, version.Seed9:
		return true
	}
	return false
}

// ModelLineage splits a model string by the convention of
// spec/qualification.md: "<family>/<version>" or, since the levels
// (plans/os-99829835.md D1), "<provider>/<family>/<version>". A
// three-part model names its provider; a two-part one names none, and
// a bare string is its own family. Versions are never part of the
// lineage: a newer build of the same family is the same failure domain.
func ModelLineage(model string) (provider, family string) {
	parts := strings.Split(model, "/")
	switch {
	case len(parts) >= 3:
		return parts[0], parts[1]
	case len(parts) == 2:
		return "", parts[0]
	default:
		return "", model
	}
}

// SeparatesModel reports whether two model strings name different
// failure domains in the charter's sense ("different model family or
// provider"): the families differ, or both name a provider and the
// providers differ. A provider named on one side only is not a
// difference the record can prove, so it does not count.
func SeparatesModel(a, b string) bool {
	pa, fa := ModelLineage(a)
	pb, fb := ModelLineage(b)
	if fa != fb {
		return true
	}
	return pa != "" && pb != "" && pa != pb
}

// SeparatesHarness reports whether two harness strings name different
// harnesses: the name before the version, "<name>/<version>" by the
// same convention, so a newer build of one harness is one failure
// domain and a different harness image is another.
func SeparatesHarness(a, b string) bool {
	return strings.SplitN(a, "/", 2)[0] != strings.SplitN(b, "/", 2)[0]
}
