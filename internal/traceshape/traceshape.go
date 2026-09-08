// Package traceshape normalizes a test harness's trace export into the
// shape a receipt binds (plans/os-7fc2ca38.md D3; spec/verdicts.md
// "Trace-shaped evidence"). A harness that honors SEED_TRACE_EXPORT
// writes one OTLP/JSON ExportTraceServiceRequest; this package reads
// the documented span fields with encoding/json and imports no
// OpenTelemetry package, because the contract is a file the harness
// writes, never a vendor SDK. What survives normalization is exactly
// what reproduces across runs: span name, kind, status code, the
// attributes the acceptance spec declares, and the tree. Ids, times,
// durations, events, links, resource, scope, the status message and
// every undeclared attribute are stripped by construction, so a
// receipt that binds the shape's digest recomputes to the same digest
// from a fresh run.
package traceshape

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gowebpki/jcs"
)

// Node is one normalized span: its name, kind and status code as
// words, the declared attributes as canonical JSON values, and its
// children sorted by their own canonical bytes.
type Node struct {
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Status     string         `json:"status"`
	Attributes map[string]any `json:"attributes"`
	Children   []*Node        `json:"children"`
}

// Shape is the document one transcript's export normalizes to: the
// transcript index (and whether it is a sealed one) and one entry per
// root span, a span whose parent is absent from the export. A trace
// with orphaned spans yields one root per orphan.
type Shape struct {
	Transcript int     `json:"transcript"`
	Sealed     bool    `json:"sealed,omitempty"`
	Traces     []*Node `json:"traces"`
}

// MalformedError is an export that exists but is not an OTLP/JSON
// trace document: a fact the receipt records (the entry carries
// malformed: true and no shape), never a refusal.
type MalformedError struct{ Reason string }

func (e *MalformedError) Error() string { return "trace export: " + e.Reason }

// Normalize parses one OTLP/JSON export and builds the shape for
// transcript n, retaining only the declared attribute keys.
func Normalize(raw []byte, n int, sealed bool, declared []string) (*Shape, error) {
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, &MalformedError{Reason: "not a JSON object: " + err.Error()}
	}
	rs, ok := field(doc, "resourceSpans", "resource_spans").([]any)
	if !ok {
		return nil, &MalformedError{Reason: "no resourceSpans array"}
	}
	keep := map[string]bool{}
	for _, k := range declared {
		keep[k] = true
	}
	type key struct{ trace, span string }
	nodes := map[key]*Node{}
	parents := map[key]key{}
	var order []key
	for _, r := range rs {
		rm, _ := r.(map[string]any)
		ss, _ := field(rm, "scopeSpans", "scope_spans").([]any)
		for _, s := range ss {
			sm, _ := s.(map[string]any)
			spans, _ := field(sm, "spans", "spans").([]any)
			for _, sp := range spans {
				m, ok := sp.(map[string]any)
				if !ok {
					return nil, &MalformedError{Reason: "a span is not an object"}
				}
				name, _ := field(m, "name", "name").(string)
				if name == "" {
					return nil, &MalformedError{Reason: "a span carries no name"}
				}
				tid, _ := field(m, "traceId", "trace_id").(string)
				sid, _ := field(m, "spanId", "span_id").(string)
				pid, _ := field(m, "parentSpanId", "parent_span_id").(string)
				k := key{tid, sid}
				if sid == "" || nodes[k] != nil {
					// A span without an id, or with a duplicate id,
					// cannot be placed in a tree: it becomes a root of
					// its own.
					k = key{tid, fmt.Sprintf("anon-%d", len(order))}
					pid = ""
				}
				node := &Node{Name: name, Kind: kindWord(field(m, "kind", "kind")), Status: statusWord(field(m, "status", "status")), Attributes: map[string]any{}, Children: []*Node{}}
				attrs, _ := field(m, "attributes", "attributes").([]any)
				for _, a := range attrs {
					am, _ := a.(map[string]any)
					ak, _ := field(am, "key", "key").(string)
					if ak == "" || !keep[ak] {
						continue
					}
					node.Attributes[ak] = anyValue(field(am, "value", "value"))
				}
				nodes[k] = node
				parents[k] = key{tid, pid}
				order = append(order, k)
			}
		}
	}
	shape := &Shape{Transcript: n, Sealed: sealed, Traces: []*Node{}}
	for _, k := range order {
		p := parents[k]
		if parent, ok := nodes[p]; ok && p.span != "" && p != k {
			parent.Children = append(parent.Children, nodes[k])
			continue
		}
		shape.Traces = append(shape.Traces, nodes[k])
	}
	// A cycle of parent ids (a raw export nobody sane writes, but a
	// file is a file) would leave every member reachable from no root;
	// such spans are attached nowhere and the count below would
	// disagree with the input, so they become roots.
	reach := map[*Node]bool{}
	var mark func(*Node)
	mark = func(nd *Node) {
		if reach[nd] {
			return
		}
		reach[nd] = true
		for _, c := range nd.Children {
			mark(c)
		}
	}
	for _, r := range shape.Traces {
		mark(r)
	}
	for _, k := range order {
		if nd := nodes[k]; !reach[nd] {
			nd.Children = detachCycle(nd, reach)
			shape.Traces = append(shape.Traces, nd)
			mark(nd)
		}
	}
	sortTree(shape.Traces)
	return shape, nil
}

// detachCycle keeps a cycle member's children that are still
// unreached, so a cycle folds into a chain from the first member met.
func detachCycle(nd *Node, reach map[*Node]bool) []*Node {
	out := []*Node{}
	for _, c := range nd.Children {
		if !reach[c] && c != nd {
			out = append(out, c)
		}
	}
	return out
}

// sortTree orders every child list by canonical bytes, bottom-up, so
// the shape is independent of the export's span order.
func sortTree(nodes []*Node) {
	for _, nd := range nodes {
		sortTree(nd.Children)
	}
	keys := make(map[*Node][]byte, len(nodes))
	for _, nd := range nodes {
		keys[nd] = canonical(nd)
	}
	sort.SliceStable(nodes, func(i, j int) bool { return bytes.Compare(keys[nodes[i]], keys[nodes[j]]) < 0 })
}

func canonical(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	c, err := jcs.Transform(b)
	if err != nil {
		return b
	}
	return c
}

// Canonical returns the shape's RFC 8785 (JCS) bytes, the artifact
// the store holds under the shape digest.
func (s *Shape) Canonical() ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

// Digest returns the SHA-256 hex of the canonical bytes: the receipt's
// shape_sha256.
func (s *Shape) Digest() (string, error) {
	b, err := s.Canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Count returns the node count and the count of nodes whose status is
// error, the receipt entry's spans and errors.
func (s *Shape) Count() (spans, errors int) {
	var walk func(*Node)
	walk = func(nd *Node) {
		spans++
		if nd.Status == "error" {
			errors++
		}
		for _, c := range nd.Children {
			walk(c)
		}
	}
	for _, r := range s.Traces {
		walk(r)
	}
	return spans, errors
}

// Parse decodes a stored shape document strictly.
func Parse(raw []byte) (*Shape, error) {
	var s Shape
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("trace shape: %w", err)
	}
	return &s, nil
}

// Resolve returns the node a citation path names: dot-separated child
// indexes from the root list down ("0" is the first root, "0.3.1" its
// fourth child's second child).
func (s *Shape) Resolve(path string) (*Node, error) {
	parts := strings.Split(path, ".")
	var cur []*Node = s.Traces
	var nd *Node
	for i, p := range parts {
		idx, err := strconv.Atoi(p)
		if err != nil || idx < 0 || p != strconv.Itoa(idx) {
			return nil, fmt.Errorf("path %q: segment %d is not an index", path, i)
		}
		if idx >= len(cur) {
			return nil, fmt.Errorf("path %q: index %d at segment %d is beyond the %d nodes there", path, idx, i, len(cur))
		}
		nd = cur[idx]
		cur = nd.Children
	}
	return nd, nil
}

// Walk visits every node depth-first with its citation path.
func (s *Shape) Walk(fn func(path string, depth int, nd *Node)) {
	var walk func(prefix string, depth int, nodes []*Node)
	walk = func(prefix string, depth int, nodes []*Node) {
		for i, nd := range nodes {
			p := strconv.Itoa(i)
			if prefix != "" {
				p = prefix + "." + p
			}
			fn(p, depth, nd)
			walk(p, depth+1, nd.Children)
		}
	}
	walk("", 0, s.Traces)
}

// field reads a key by its proto3 JSON name or its snake_case
// original: exporters disagree, and the shape should not.
func field(m map[string]any, camel, snake string) any {
	if m == nil {
		return nil
	}
	if v, ok := m[camel]; ok {
		return v
	}
	return m[snake]
}

var kinds = []string{"unspecified", "internal", "server", "client", "producer", "consumer"}

// kindWord renders the OTLP span kind, an enum number or its
// SPAN_KIND_* name, as a word; anything else is unspecified.
func kindWord(v any) string {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil && i >= 0 && int(i) < len(kinds) {
			return kinds[i]
		}
	case string:
		w := strings.ToLower(strings.TrimPrefix(x, "SPAN_KIND_"))
		for _, k := range kinds {
			if k == w {
				return k
			}
		}
	}
	return kinds[0]
}

var statuses = []string{"unset", "ok", "error"}

// statusWord renders the status code, an enum number or its
// STATUS_CODE_* name, as a word; an absent status is unset. The
// message is stripped: it is prose that changes between runs.
func statusWord(v any) string {
	m, _ := v.(map[string]any)
	switch x := field(m, "code", "code").(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil && i >= 0 && int(i) < len(statuses) {
			return statuses[i]
		}
	case string:
		w := strings.ToLower(strings.TrimPrefix(x, "STATUS_CODE_"))
		for _, s := range statuses {
			if s == w {
				return s
			}
		}
	}
	return statuses[0]
}

// anyValue renders an OTLP AnyValue to its canonical JSON value:
// strings, numbers, booleans, arrays and key-value lists as their
// plain JSON counterparts. An int carried as a string (the proto3
// JSON mapping of int64) becomes a number when it parses.
func anyValue(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if s, ok := field(m, "stringValue", "string_value").(string); ok {
		return s
	}
	if b, ok := field(m, "boolValue", "bool_value").(bool); ok {
		return b
	}
	if iv := field(m, "intValue", "int_value"); iv != nil {
		switch x := iv.(type) {
		case json.Number:
			return x
		case string:
			if i, err := strconv.ParseInt(x, 10, 64); err == nil {
				return json.Number(strconv.FormatInt(i, 10))
			}
			return x
		}
	}
	if dv, ok := field(m, "doubleValue", "double_value").(json.Number); ok {
		return dv
	}
	if av, ok := field(m, "arrayValue", "array_value").(map[string]any); ok {
		vals, _ := field(av, "values", "values").([]any)
		out := make([]any, 0, len(vals))
		for _, x := range vals {
			out = append(out, anyValue(x))
		}
		return out
	}
	if kv, ok := field(m, "kvlistValue", "kvlist_value").(map[string]any); ok {
		vals, _ := field(kv, "values", "values").([]any)
		out := map[string]any{}
		for _, x := range vals {
			xm, _ := x.(map[string]any)
			k, _ := field(xm, "key", "key").(string)
			if k != "" {
				out[k] = anyValue(field(xm, "value", "value"))
			}
		}
		return out
	}
	if bv, ok := field(m, "bytesValue", "bytes_value").(string); ok {
		return bv
	}
	return v
}
