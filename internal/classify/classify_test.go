package classify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readCorpus(t *testing.T, kind string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", kind))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("testdata", kind, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = b
	}
	if len(out) == 0 {
		t.Fatalf("empty %s corpus", kind)
	}
	return out
}

// conformance: III.A — payload data classification: coordination facts and
// references only; the hostile-payload lint corpus passes (every fixture
// refuses with the rule its filename names, at a located pointer).
func TestHostileCorpusRefuses(t *testing.T) {
	for name, payload := range readCorpus(t, "hostile") {
		parts := strings.Split(name, ".")
		if len(parts) != 3 {
			t.Fatalf("hostile fixture %q must be <name>.<rule>.json", name)
		}
		wantRule := parts[1]
		vs := Lint(payload)
		if len(vs) == 0 {
			t.Errorf("%s: hostile payload passed the lint", name)
			continue
		}
		found := false
		for _, v := range vs {
			if v.Rule == wantRule {
				found = true
				if v.Remedy == "" || v.Detail == "" {
					t.Errorf("%s: violation must carry detail and the artifact-store remedy, got %+v", name, v)
				}
			}
		}
		if !found {
			t.Errorf("%s: expected rule %s among violations, got %+v", name, wantRule, vs)
		}
	}
}

func TestBenignCorpusPasses(t *testing.T) {
	for name, payload := range readCorpus(t, "benign") {
		if vs := Lint(payload); len(vs) != 0 {
			t.Errorf("%s: benign payload must pass, got %+v", name, vs)
		}
	}
}

// conformance: III.A groundwork — the bounds are data: the operative table
// is embedded and byte-identical to the normative spec copy, so the two
// cannot drift.
func TestEmbeddedRulesMatchSpecCopy(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "spec", "classify.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(spec) != string(EmbeddedRulesJSON()) {
		t.Fatal("spec/classify.json and the embedded rules.json have drifted — they must be byte-identical")
	}
	r, err := LoadRules()
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != "1" || r.MaxPayloadBytes <= 0 || r.MaxStringBytes <= 0 ||
		r.AggregateTextBudgetBytes <= 0 || r.MaxDepth <= 0 || r.MaxArrayLen <= 0 || r.MaxBase64Run <= 0 {
		t.Fatalf("rule table fails schema sanity: %+v", r)
	}
}

// Bounds are data, not code: tightening a bound in the table changes the
// verdict with no code change.
func TestBoundsDriveTheVerdict(t *testing.T) {
	rules, err := LoadRules()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"note": "a perfectly ordinary short note"}`)
	if vs := LintWith(rules, payload); len(vs) != 0 {
		t.Fatalf("baseline must pass, got %+v", vs)
	}
	tightened := rules
	tightened.MaxStringBytes = 5
	vs := LintWith(tightened, payload)
	if len(vs) == 0 || vs[0].Rule != RuleStringTooLong {
		t.Fatalf("tightened bound must change the verdict, got %+v", vs)
	}
}

func TestDeterministicAndOrderStable(t *testing.T) {
	a := []byte(`{"z": "zebra", "a": {"y": 1, "b": "` + strings.Repeat("w", 600) + `"}, "list": ["` + strings.Repeat("v", 600) + `"]}`)
	b := []byte(`{"list": ["` + strings.Repeat("v", 600) + `"], "a": {"b": "` + strings.Repeat("w", 600) + `", "y": 1}, "z": "zebra"}`)
	v1 := Lint(a)
	v2 := Lint(a)
	v3 := Lint(b)
	if !reflect.DeepEqual(v1, v2) {
		t.Fatalf("same input must lint identically:\n%+v\n%+v", v1, v2)
	}
	if !reflect.DeepEqual(v1, v3) {
		t.Fatalf("key order must not change the verdict (canonicalize-first):\n%+v\n%+v", v1, v3)
	}
	if len(v1) < 2 {
		t.Fatalf("fixture should produce multiple violations, got %+v", v1)
	}
}

func TestPointersLocateViolations(t *testing.T) {
	payload := []byte(`{"outer": {"inner": ["ok", "` + strings.Repeat("x", 600) + `"]}}`)
	vs := Lint(payload)
	found := false
	for _, v := range vs {
		if v.Rule == RuleStringTooLong && v.Pointer == "/outer/inner/1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected string_too_long at /outer/inner/1, got %+v", vs)
	}
}

func TestReferenceShapesAreExempt(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"anchor": "some/long/path/deep/in/the/tree.go @ ` + strings.Repeat("ab", 20) + `"`)
	for i := 0; i < 20; i++ {
		b.WriteString(`, "h` + string(rune('a'+i)) + `": "` + strings.Repeat("0", 63) + string(rune('a'+i%6)) + `"`)
	}
	b.WriteString(`}`)
	if vs := Lint([]byte(b.String())); len(vs) != 0 {
		t.Fatalf("reference-shaped strings must not consume the text budget, got %+v", vs)
	}
}

func TestNonObjectPayloadsRefuse(t *testing.T) {
	for _, payload := range []string{`"just a string"`, `[1,2,3]`, `42`, `null`, `{"broken":`} {
		vs := Lint([]byte(payload))
		if len(vs) != 1 || vs[0].Rule != RuleNotAnObject {
			t.Errorf("payload %q must refuse as %s, got %+v", payload, RuleNotAnObject, vs)
		}
	}
}

// The Phase 2 seam: no IO at lint time is proven by construction (pure
// function over bytes); determinism across two loads is asserted here.
func TestLoadRulesStable(t *testing.T) {
	r1, err1 := LoadRules()
	r2, err2 := LoadRules()
	if err1 != nil || err2 != nil || !reflect.DeepEqual(r1, r2) {
		t.Fatalf("LoadRules must be stable: %+v/%v vs %+v/%v", r1, err1, r2, err2)
	}
	var direct Rules
	if err := json.Unmarshal(EmbeddedRulesJSON(), &direct); err != nil || !reflect.DeepEqual(direct, r1) {
		t.Fatalf("EmbeddedRulesJSON must round-trip to LoadRules: %v", err)
	}
}

// The commit-anchor exemption must not admit prose wearing an anchor
// suffix, and pointers must escape / and ~ per RFC 6901 (#80 review).
func TestAnchorExemptionIsNarrow(t *testing.T) {
	body := strings.Repeat("word and more prose ", 30) + " @ deadbeef"
	vs := Lint([]byte(`{"x": ` + string(mustJSON(t, body)) + `}`))
	if len(vs) == 0 {
		t.Fatal("prose ending in ' @ <hex>' must not be exempt")
	}
	ok := Lint([]byte(`{"ref": "internal/classify/classify.go @ deadbeefdeadbeef"}`))
	if len(ok) != 0 {
		t.Fatalf("a real commit anchor must stay exempt, got %+v", ok)
	}
}

func TestPointerEscaping(t *testing.T) {
	long := strings.Repeat("x", 600)
	vs := Lint([]byte(`{"a/b": ` + string(mustJSON(t, long)) + `, "c~d": ` + string(mustJSON(t, long)) + `}`))
	want := map[string]bool{"/a~1b": false, "/c~0d": false}
	for _, v := range vs {
		if v.Rule == RuleStringTooLong {
			if _, exists := want[v.Pointer]; exists {
				want[v.Pointer] = true
			}
		}
	}
	for ptr, seen := range want {
		if !seen {
			t.Errorf("expected a string_too_long violation at %s, got %+v", ptr, vs)
		}
	}
}
