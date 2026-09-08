// Package classify implements the payload data-classification lint
// (charter Part II section 1, "Data classification"; plans/os-d6f81ec6.md):
// payloads carry coordination facts and references, never content bodies.
// It is a pure library over payload bytes with no IO at lint time; the
// Phase 2 admission rule set (internal/admit) calls Lint unchanged. Bounds
// are data: the operative table is embedded (rules.json) and
// spec/classify.json is the normative reviewable copy, byte-identity
// sync-tested so the two cannot drift.
package classify

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	_ "embed"

	"github.com/gowebpki/jcs"
)

//go:embed rules.json
var rulesJSON []byte

// Rules is the bound table (spec/classify.json is the normative copy).
type Rules struct {
	SchemaVersion            string `json:"schema_version"`
	MaxPayloadBytes          int    `json:"max_payload_bytes"`
	MaxStringBytes           int    `json:"max_string_bytes"`
	AggregateTextBudgetBytes int    `json:"aggregate_text_budget_bytes"`
	MaxDepth                 int    `json:"max_depth"`
	MaxArrayLen              int    `json:"max_array_len"`
	MaxBase64Run             int    `json:"max_base64_run"`
}

// Rule names, one per bound, cited by violations and the corpus.
const (
	RulePayloadTooLarge = "payload_too_large"
	RuleStringTooLong   = "string_too_long"
	RuleAggregateText   = "aggregate_text_budget"
	RuleTooDeep         = "too_deep"
	RuleArrayTooLong    = "array_too_long"
	RuleEmbeddedBlob    = "embedded_blob"
	RuleNotAnObject     = "not_an_object"
)

// remedy is the affordance every refusal teaches: bodies go to the
// artifact store, payloads carry the hash.
const remedy = "store the body in the artifact store and reference its hash (charter II.1 data classification)"

// Violation names one broken rule at one location.
type Violation struct {
	Pointer string `json:"pointer"`
	Rule    string `json:"rule"`
	Detail  string `json:"detail"`
	Remedy  string `json:"remedy"`
}

var (
	hashRef = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// The commit-anchor exemption is deliberately narrow: a path in a
	// filename alphabet (no whitespace, no prose punctuation, at most 200
	// bytes) anchored to a commit or range. Arbitrary text ending in
	// " @ <hex>" is a content body wearing an anchor and stays subject to
	// every text rule (#80 review finding).
	anchorRef = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200} @ [0-9a-f]{7,64}(\.\.[0-9a-f]{7,64})?$`)
	// A bare commit range is the packet's resume coordinate
	// ("<merge-base>..<head>", plans/os-b07b0f59.md): pure anchors,
	// exempt like the anchored-path forms; anything with prose in it
	// fails the hex alphabet and stays budgeted.
	rangeRef  = regexp.MustCompile(`^[0-9a-f]{7,64}\.\.[0-9a-f]{7,64}$`)
	base64Run = regexp.MustCompile(`[A-Za-z0-9+/=]+`)
)

// isReference reports whether a string value is a coordination reference
// (a full content hash, a commit-anchored path per the packet grammar,
// or a bare commit range) and therefore exempt from the free-text caps.
func isReference(s string) bool {
	return hashRef.MatchString(s) || anchorRef.MatchString(s) || rangeRef.MatchString(s)
}

// IsReference exposes the exemption predicate: the packet schema
// (internal/packet) validates its anchors against the same grammar the
// classifier exempts, so the two cannot drift.
func IsReference(s string) bool { return isReference(s) }

// IsAnchoredRef reports the combined-anchor forms alone ("path @
// commit", "ref @ range"): the packet's refs entries must be anchored,
// a bare hash or range is not enough to name an artifact.
func IsAnchoredRef(s string) bool { return anchorRef.MatchString(s) }

// IsRange reports the bare commit-range form, the packet's base
// resume coordinate.
func IsRange(s string) bool { return rangeRef.MatchString(s) }

// LoadRules returns the embedded bound table.
func LoadRules() (Rules, error) {
	var r Rules
	if err := json.Unmarshal(rulesJSON, &r); err != nil {
		return Rules{}, fmt.Errorf("embedded rules.json does not parse: %w", err)
	}
	return r, nil
}

// EmbeddedRulesJSON exposes the raw embedded table for the spec sync test.
func EmbeddedRulesJSON() []byte {
	return append([]byte(nil), rulesJSON...)
}

// Lint checks one payload against the embedded bound table. It
// canonicalizes first (JCS), so results are deterministic and order-stable
// regardless of the input's key order or whitespace; a payload that does
// not canonicalize refuses as not-an-object.
func Lint(payload []byte) []Violation {
	rules, err := LoadRules()
	if err != nil {
		return []Violation{{Pointer: "", Rule: RuleNotAnObject, Detail: err.Error(), Remedy: remedy}}
	}
	return LintWith(rules, payload)
}

// LintWith is Lint against an explicit bound table: the bounds are data,
// and tests (and any future per-deployment tuning decision) exercise that
// by supplying a modified table.
func LintWith(rules Rules, payload []byte) []Violation {
	canon, err := jcs.Transform(payload)
	if err != nil {
		return []Violation{{Pointer: "", Rule: RuleNotAnObject, Detail: fmt.Sprintf("payload is not canonicalizable JSON: %v", err), Remedy: remedy}}
	}
	var root any
	if err := json.Unmarshal(canon, &root); err != nil {
		return []Violation{{Pointer: "", Rule: RuleNotAnObject, Detail: err.Error(), Remedy: remedy}}
	}
	if _, ok := root.(map[string]any); !ok {
		return []Violation{{Pointer: "", Rule: RuleNotAnObject, Detail: "payload must be a JSON object", Remedy: remedy}}
	}

	var vs []Violation
	if len(canon) > rules.MaxPayloadBytes {
		vs = append(vs, Violation{Pointer: "", Rule: RulePayloadTooLarge,
			Detail: fmt.Sprintf("canonical payload is %d bytes, cap %d", len(canon), rules.MaxPayloadBytes), Remedy: remedy})
	}
	textBudget := 0
	walk(root, "", 1, rules, &vs, &textBudget)
	if textBudget > rules.AggregateTextBudgetBytes {
		vs = append(vs, Violation{Pointer: "", Rule: RuleAggregateText,
			Detail: fmt.Sprintf("non-reference string values total %d bytes, budget %d: a body chunked into small strings is still a body", textBudget, rules.AggregateTextBudgetBytes), Remedy: remedy})
	}
	return vs
}

// walk descends the canonicalized value depth-first in sorted-key order
// (order-stable by construction), accumulating violations and the
// free-text byte total.
func walk(v any, pointer string, depth int, rules Rules, vs *[]Violation, textBudget *int) {
	if depth > rules.MaxDepth {
		*vs = append(*vs, Violation{Pointer: pointer, Rule: RuleTooDeep,
			Detail: fmt.Sprintf("nesting depth %d exceeds %d", depth, rules.MaxDepth), Remedy: remedy})
		return
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walk(t[k], pointer+"/"+escapeToken(k), depth+1, rules, vs, textBudget)
		}
	case []any:
		if len(t) > rules.MaxArrayLen {
			*vs = append(*vs, Violation{Pointer: pointer, Rule: RuleArrayTooLong,
				Detail: fmt.Sprintf("array has %d elements, cap %d", len(t), rules.MaxArrayLen), Remedy: remedy})
		}
		for i, e := range t {
			walk(e, fmt.Sprintf("%s/%d", pointer, i), depth+1, rules, vs, textBudget)
		}
	case string:
		if isReference(t) {
			return
		}
		if len(t) > rules.MaxStringBytes {
			*vs = append(*vs, Violation{Pointer: pointer, Rule: RuleStringTooLong,
				Detail: fmt.Sprintf("string is %d bytes, cap %d", len(t), rules.MaxStringBytes), Remedy: remedy})
		}
		if run := longestBase64Run(t); run > rules.MaxBase64Run {
			*vs = append(*vs, Violation{Pointer: pointer, Rule: RuleEmbeddedBlob,
				Detail: fmt.Sprintf("contiguous base64-like run of %d bytes exceeds %d", run, rules.MaxBase64Run), Remedy: remedy})
		}
		*textBudget += len(t)
	}
}

// escapeToken applies RFC 6901 escaping so a key containing / or ~ still
// yields a pointer that locates exactly one field (#80 review finding).
func escapeToken(k string) string {
	k = strings.ReplaceAll(k, "~", "~0")
	return strings.ReplaceAll(k, "/", "~1")
}

func longestBase64Run(s string) int {
	longest := 0
	for _, m := range base64Run.FindAllString(s, -1) {
		if len(m) > longest {
			longest = len(m)
		}
	}
	return longest
}
