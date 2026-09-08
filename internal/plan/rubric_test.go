package plan

import (
	"errors"
	"strings"
	"testing"
)

const rubricDoc = "# Spec\n\n## Validation commands\n\n- Boundary: `go test ./...`\n\n## Rubric\n\n- tone: the error messages read as the operator's, not the machine's\n- `taste`: the abstraction carries its weight\n\n## Notes\n\n- not-an-item: this is prose under another heading\n"

// conformance: AC1 — the section's items with ids and criteria; both
// sections yield both; the refusals name the part.
func TestRubricReadsItsSectionAlone(t *testing.T) {
	items, err := Rubric([]byte(rubricDoc))
	if err != nil || len(items) != 2 || items[0].ID != "tone" || items[1].ID != "taste" || !strings.HasPrefix(items[0].Criterion, "the error messages") {
		t.Fatalf("the rubric section's items: %+v %v", items, err)
	}
	if cmds := Commands([]byte(rubricDoc)); len(cmds) != 1 || cmds[0] != "go test ./..." {
		t.Fatalf("the commands section is read beside it: %v", cmds)
	}
	if items, err := Rubric([]byte("# Spec\n\n## Validation commands\n\n- `go test ./...`\n")); err != nil || len(items) != 0 {
		t.Fatalf("no section, no items, no error: %+v %v", items, err)
	}
	for _, row := range []struct{ name, doc, names string }{
		{"a duplicate id", "## Rubric\n\n- tone: a\n- tone: b\n", "twice"},
		{"an empty id", "## Rubric\n\n- : a criterion\n", "no id"},
		{"no colon", "## Rubric\n\n- just prose\n", "no id"},
		{"an empty criterion", "## Rubric\n\n- tone:\n", "no criterion"},
		{"a non-slug id", "## Rubric\n\n- Tone Of Voice: a\n", "not a slug"},
	} {
		_, err := Rubric([]byte(row.doc))
		var re *RubricError
		if !errors.As(err, &re) || !strings.Contains(err.Error(), row.names) {
			t.Fatalf("%s refuses naming the part (%s): %v", row.name, row.names, err)
		}
	}
}
