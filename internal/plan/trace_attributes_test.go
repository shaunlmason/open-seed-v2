package plan

import (
	"strings"
	"testing"
)

// conformance: plans/os-7fc2ca38.md D3 — the declared attribute keys
// are read from the spec's "## Trace attributes" section exactly as
// the rubric is: bullets, one key each, backticks tolerated, an absent
// section declares nothing, and an empty, whitespace-bearing or
// duplicate key refuses with the part named.
func TestTraceAttributesSection(t *testing.T) {
	spec := "# Acceptance\n\n## Validation Commands\n\n- `true`\n\n## Trace attributes\n\n- `herdr.outcome`\n- herdr.reason\n* **attempt**\n\n## Rubric\n\n- taste: fine\n"
	keys, err := TraceAttributes([]byte(spec))
	if err != nil || strings.Join(keys, ",") != "herdr.outcome,herdr.reason,attempt" {
		t.Fatalf("keys %v err %v", keys, err)
	}
	if keys, err := TraceAttributes([]byte("# Acceptance\n\n## Validation Commands\n\n- `true`\n")); err != nil || len(keys) != 0 {
		t.Fatalf("an absent section declares nothing: %v %v", keys, err)
	}
	if keys, err := TraceAttributes([]byte("## Trace attributes\n\nprose only\n")); err != nil || len(keys) != 0 {
		t.Fatalf("a section without bullets declares nothing: %v %v", keys, err)
	}
	for _, bad := range []struct{ body, part string }{
		{"## Trace attributes\n\n- ``\n", "no key"},
		{"## Trace attributes\n\n- two words\n", "whitespace"},
		{"## Trace attributes\n\n- a\n- a\n", "twice"},
	} {
		_, err := TraceAttributes([]byte(bad.body))
		var te *TraceAttributesError
		if err == nil || !strings.Contains(err.Error(), bad.part) {
			t.Fatalf("%q must refuse naming %q, got %v", bad.body, bad.part, err)
		}
		if _, ok := err.(*TraceAttributesError); !ok {
			t.Fatalf("the refusal is a TraceAttributesError, got %T", err)
		}
		_ = te
	}
	// Rubric and commands are untouched by the new section.
	items, err := Rubric([]byte(spec))
	if err != nil || len(items) != 1 || items[0].ID != "taste" {
		t.Fatalf("rubric unaffected: %v %v", items, err)
	}
	if cmds := Commands([]byte(spec)); len(cmds) != 1 || cmds[0] != "true" {
		t.Fatalf("commands unaffected: %v", cmds)
	}
}
