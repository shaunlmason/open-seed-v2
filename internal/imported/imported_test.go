package imported

// The provenance record's shape (plans/os-f262585a.md D1, D3). This is
// the one fact a genesis import appends before the replayed history,
// and admission validates it through this parser, so every refusal here
// is a chain the boundary would take or refuse. The package shipped
// with no drill at all: `-coverpkg` counted its statements and nothing
// exercised them.

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	head     = "0123456789abcdef0123456789abcdef01234567"
	manifest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestRenderRoundTripsThroughParse(t *testing.T) {
	raw, err := Render(head, "seed-export-2026-09", manifest)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(Subject, raw)
	if err != nil {
		t.Fatalf("what Render writes must be what Parse takes: %v", err)
	}
	if p.Source != SourceOpenSeed || p.ExportHead != head || p.Anchor != "seed-export-2026-09" || p.Manifest != manifest {
		t.Errorf("the payload did not round trip: %+v", p)
	}
	// Render names the source itself: a caller cannot import from a
	// predecessor this build does not understand by passing one in.
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["source"] != SourceOpenSeed {
		t.Errorf("Render fixes the source, got %v", fields["source"])
	}
	if len(fields) != 4 {
		t.Errorf("the payload is the strict object {source, export_head, anchor, manifest}, got %v", fields)
	}
}

func TestParseRefusesEverythingButTheStrictObject(t *testing.T) {
	good := `{"source": "open-seed", "export_head": "` + head + `", "anchor": "seed-export-2026-09", "manifest": "` + manifest + `"}`
	if _, err := Parse(Subject, []byte(good)); err != nil {
		t.Fatalf("the well-formed payload is the control and must pass: %v", err)
	}

	for _, tc := range []struct {
		name, subject, raw, want string
	}{
		{"another subject", "c-1", good, `is not "system"`},
		{"an unknown field", Subject, `{"source": "open-seed", "export_head": "` + head + `", "anchor": "a", "manifest": "` + manifest + `", "extra": 1}`, "strict object"},
		{"not an object", Subject, `[]`, "strict object"},
		{"trailing data", Subject, good + ` {}`, "trailing data"},
		{"another predecessor", Subject, `{"source": "somewhere-else", "export_head": "` + head + `", "anchor": "a", "manifest": "` + manifest + `"}`, "the one predecessor this build imports"},
		{"a short head", Subject, `{"source": "open-seed", "export_head": "abc", "anchor": "a", "manifest": "` + manifest + `"}`, "forty lowercase hex characters"},
		{"an uppercase head", Subject, `{"source": "open-seed", "export_head": "` + strings.ToUpper(head) + `", "anchor": "a", "manifest": "` + manifest + `"}`, "forty lowercase hex characters"},
		{"an empty anchor", Subject, `{"source": "open-seed", "export_head": "` + head + `", "anchor": "  ", "manifest": "` + manifest + `"}`, "anchor names the tag"},
		{"an anchor with a space", Subject, `{"source": "open-seed", "export_head": "` + head + `", "anchor": "two words", "manifest": "` + manifest + `"}`, "anchor names the tag"},
		{"a manifest that is no digest", Subject, `{"source": "open-seed", "export_head": "` + head + `", "anchor": "a", "manifest": "` + head + `"}`, "sha256 digest"},
	} {
		p, err := Parse(tc.subject, []byte(tc.raw))
		if err == nil {
			t.Errorf("%s: must be refused, got %+v", tc.name, p)
			continue
		}
		if p != nil {
			t.Errorf("%s: a refusal returns no payload", tc.name)
		}
		var e *Error
		if !asImportedError(err, &e) {
			t.Errorf("%s: the refusal is an *imported.Error, got %T", tc.name, err)
			continue
		}
		if !strings.Contains(e.Error(), tc.want) {
			t.Errorf("%s: the refusal must say %q, got %q", tc.name, tc.want, e.Error())
		}
		if !strings.HasPrefix(e.Error(), Verb+": ") {
			t.Errorf("%s: the refusal names the verb it is about, got %q", tc.name, e.Error())
		}
	}
}

func asImportedError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
