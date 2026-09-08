package traceshape

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// span builds one OTLP/JSON span object.
func span(trace, id, parent, name string, kind any, status any, attrs map[string]any, ts int) map[string]any {
	m := map[string]any{
		"traceId": trace, "spanId": id, "name": name, "kind": kind,
		"startTimeUnixNano": fmt.Sprintf("%d", ts), "endTimeUnixNano": fmt.Sprintf("%d", ts+1000),
	}
	if parent != "" {
		m["parentSpanId"] = parent
	}
	if status != nil {
		m["status"] = status
	}
	var list []any
	for k, v := range attrs {
		list = append(list, map[string]any{"key": k, "value": v})
	}
	m["attributes"] = list
	return m
}

func export(spans ...map[string]any) []byte {
	doc := map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "svc"}}}},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "svc"}, "spans": toAny(spans)}},
	}}}
	b, _ := json.Marshal(doc)
	return b
}

func toAny(spans []map[string]any) []any {
	out := make([]any, 0, len(spans))
	for _, s := range spans {
		out = append(out, s)
	}
	return out
}

func str(s string) map[string]any { return map[string]any{"stringValue": s} }

// run is the harness run the drills compare: a root with two children,
// one of them failing, with ids, times, sibling order and an undeclared
// run id varying per call.
func run(salt string, ts int, swap bool) []byte {
	root := span("t"+salt, "r"+salt, "", "sdk.execution", 1, nil, map[string]any{"sdk.run_id": str("run-" + salt)}, ts)
	list := span("t"+salt, "l"+salt, "r"+salt, "WorkspaceService.list", "SPAN_KIND_INTERNAL", map[string]any{"code": 2, "message": "boom " + salt},
		map[string]any{"herdr.outcome": str("failure"), "herdr.reason": str("malformed_json"), "sdk.run_id": str("run-" + salt)}, ts+10)
	closeS := span("t"+salt, "c"+salt, "r"+salt, "PopupService.close", 1, map[string]any{"code": "STATUS_CODE_OK"},
		map[string]any{"herdr.outcome": str("success"), "attempt": map[string]any{"intValue": "3"}}, ts+20)
	if swap {
		return export(closeS, list, root)
	}
	return export(root, list, closeS)
}

var declared = []string{"herdr.outcome", "herdr.reason", "attempt"}

// conformance: III.G row 5 via plans/os-7fc2ca38.md D3 — the shape is
// what reproduces: two runs differing in ids, timestamps, durations,
// sibling order, an undeclared per-run attribute and the status
// message normalize to identical bytes and digest; a changed name,
// kind, status code, declared attribute or tree position changes the
// digest; orphans become roots; malformed input is reported as such
// rather than as an empty shape; paths resolve exactly.
func TestTraceShapeNormalizes(t *testing.T) {
	a, err := Normalize(run("a", 100, false), 2, false, declared)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize(run("b", 999, true), 2, false, declared)
	if err != nil {
		t.Fatal(err)
	}
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da != db {
		ca, _ := a.Canonical()
		cb, _ := b.Canonical()
		t.Fatalf("two runs of one harness must normalize to one shape:\n%s\n%s", ca, cb)
	}
	ca, _ := a.Canonical()
	for _, stripped := range []string{"traceId", "spanId", "startTime", "run-a", "boom", "sdk.run_id", "service.name"} {
		if strings.Contains(string(ca), stripped) {
			t.Fatalf("the shape strips %q: %s", stripped, ca)
		}
	}
	if spans, errs := a.Count(); spans != 3 || errs != 1 {
		t.Fatalf("count: %d spans, %d errors", spans, errs)
	}
	if len(a.Traces) != 1 || a.Traces[0].Name != "sdk.execution" || len(a.Traces[0].Children) != 2 {
		t.Fatalf("one root with two children: %s", ca)
	}
	// Children sort by canonical bytes, not export order.
	first := a.Traces[0].Children[0]
	if first.Name != "PopupService.close" || first.Status != "ok" || first.Kind != "internal" || first.Attributes["attempt"] != json.Number("3") {
		t.Fatalf("first child is the sorted one with declared attributes: %+v", first)
	}
	second := a.Traces[0].Children[1]
	if second.Status != "error" || second.Attributes["herdr.reason"] != "malformed_json" || second.Attributes["sdk.run_id"] != nil {
		t.Fatalf("second child keeps status and declared attributes only: %+v", second)
	}

	// Each semantic change changes the digest.
	changes := map[string]func(m map[string]any){
		"name":   func(m map[string]any) { m["name"] = "PopupService.open" },
		"kind":   func(m map[string]any) { m["kind"] = 2 },
		"status": func(m map[string]any) { m["status"] = map[string]any{"code": 2} },
		"attribute": func(m map[string]any) {
			m["attributes"] = []any{map[string]any{"key": "herdr.outcome", "value": str("failure")}}
		},
		"position": func(m map[string]any) { m["parentSpanId"] = "lx" },
	}
	for what, change := range changes {
		root := span("tx", "rx", "", "sdk.execution", 1, nil, nil, 1)
		list := span("tx", "lx", "rx", "WorkspaceService.list", 1, map[string]any{"code": 2}, map[string]any{"herdr.outcome": str("failure"), "herdr.reason": str("malformed_json")}, 2)
		closeS := span("tx", "cx", "rx", "PopupService.close", 1, map[string]any{"code": 1}, map[string]any{"herdr.outcome": str("success"), "attempt": map[string]any{"intValue": "3"}}, 3)
		change(closeS)
		c, err := Normalize(export(root, list, closeS), 2, false, declared)
		if err != nil {
			t.Fatal(err)
		}
		if dc, _ := c.Digest(); dc == da {
			t.Fatalf("a changed %s must change the digest", what)
		}
	}
	// The same bytes under a different transcript index or the sealed
	// flag are a different shape: the entry binds to its transcript.
	if s, _ := Normalize(run("a", 100, false), 3, false, declared); func() bool { d, _ := s.Digest(); return d == da }() {
		t.Fatal("the transcript index is part of the shape")
	}
	if s, _ := Normalize(run("a", 100, false), 2, true, declared); func() bool { d, _ := s.Digest(); return d == da }() {
		t.Fatal("the sealed flag is part of the shape")
	}
	// No declared keys: name, kind and status alone.
	bare, _ := Normalize(run("a", 100, false), 2, false, nil)
	if len(bare.Traces[0].Children[1].Attributes) != 0 {
		t.Fatalf("undeclared attributes are stripped: %+v", bare.Traces[0].Children[1].Attributes)
	}

	// An orphan (parent absent from the export) is a root of its own.
	orphan, err := Normalize(export(span("t", "o", "missing", "orphan", 1, nil, nil, 1), span("t", "r", "", "root", 1, nil, nil, 2)), 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphan.Traces) != 2 {
		t.Fatalf("an orphan becomes a root: %d roots", len(orphan.Traces))
	}
	// snake_case field names read the same as camelCase.
	snake := []byte(`{"resource_spans":[{"scope_spans":[{"spans":[{"trace_id":"t","span_id":"a","name":"root","kind":2,"status":{"code":"STATUS_CODE_ERROR"}}]}]}]}`)
	sn, err := Normalize(snake, 0, false, nil)
	if err != nil || len(sn.Traces) != 1 || sn.Traces[0].Kind != "server" || sn.Traces[0].Status != "error" {
		t.Fatalf("snake_case export: %v %+v", err, sn)
	}

	// Malformed input is a MalformedError, never an empty shape.
	for _, bad := range []string{"", "not json", `[]`, `{"spans": []}`, `{"resourceSpans": [{"scopeSpans": [{"spans": [{"spanId": "x"}]}]}]}`} {
		if _, err := Normalize([]byte(bad), 0, false, nil); err == nil {
			t.Fatalf("malformed export %q must be reported", bad)
		} else if _, ok := err.(*MalformedError); !ok {
			t.Fatalf("malformed export %q reports MalformedError, got %T", bad, err)
		}
	}
	// An export with no spans is a valid, empty shape.
	empty, err := Normalize([]byte(`{"resourceSpans": []}`), 0, false, nil)
	if err != nil || len(empty.Traces) != 0 {
		t.Fatalf("an empty export is an empty shape: %v", err)
	}

	// Paths resolve to the node they name and refuse beyond the tree.
	if nd, err := a.Resolve("0.1"); err != nil || nd.Name != "WorkspaceService.list" {
		t.Fatalf("0.1 is the second child: %v %+v", err, nd)
	}
	for _, bad := range []string{"1", "0.2", "0.0.0", "x", "0.-1", "0.01", ""} {
		if _, err := a.Resolve(bad); err == nil {
			t.Fatalf("path %q must refuse", bad)
		}
	}
	var paths []string
	a.Walk(func(p string, depth int, nd *Node) { paths = append(paths, fmt.Sprintf("%s:%d:%s", p, depth, nd.Name)) })
	if strings.Join(paths, " ") != "0:0:sdk.execution 0.0:1:PopupService.close 0.1:1:WorkspaceService.list" {
		t.Fatalf("walk order and paths: %v", paths)
	}
	// The stored document parses back to the same digest.
	parsed, err := Parse(ca)
	if err != nil {
		t.Fatal(err)
	}
	if dp, _ := parsed.Digest(); dp != da {
		t.Fatal("a stored shape parses back to its digest")
	}
	if _, err := Parse([]byte(`{"transcript": 0, "traces": [], "extra": 1}`)); err == nil {
		t.Fatal("a shape document is strict")
	}
}
