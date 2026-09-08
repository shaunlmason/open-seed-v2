// Package plan is the falsifiable-plan lint and the plan/implementation
// PR classifier (plans/os-16c1d142.md; SEED-NEXT.md Part II §6 "Plans
// as falsifiable change contracts"; conformance III.F "Plans are
// falsifiable: boundary set, retention set, validation commands for
// both, expected diff shape; missing retention fails lint; plan and
// implementation PRs are structurally disjoint"). The lint reads
// content structure only and never executes commands.
package plan

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Finding kinds: one per named violation, missing retention distinct
// because the charter singles it out ("a plan without a retention
// check does not lint").
const (
	KindMissingBoundary          = "missing_boundary"
	KindMissingRetention         = "missing_retention"
	KindMissingCommands          = "missing_commands"
	KindMissingDiffShape         = "missing_diff_shape"
	KindEmptySection             = "empty_section"
	KindCommandsMissingBoundary  = "commands_missing_boundary"
	KindCommandsMissingRetention = "commands_missing_retention"
)

// Finding is one lint violation.
type Finding struct {
	Kind   string
	Detail string
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s", f.Kind, f.Detail) }

// The four required parts. A section marker is a markdown heading or a
// bold line whose text begins with the part's phrase, case-insensitive
// (the repository's own plan shape, the v1-continuity default).
var parts = []struct {
	phrase, missing string
}{
	{"boundary set", KindMissingBoundary},
	{"retention set", KindMissingRetention},
	{"validation commands", KindMissingCommands},
	{"expected diff shape", KindMissingDiffShape},
}

// markerText returns the marker phrase of a line, or "" for content.
func markerText(line string) string {
	t := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(t, "#"):
		return strings.ToLower(strings.TrimSpace(strings.TrimLeft(t, "#")))
	case strings.HasPrefix(t, "**"):
		return strings.ToLower(strings.Trim(t, "*: "))
	}
	return ""
}

// Lint checks a plan document for the four falsifiable parts: each
// present and non-empty, with the commands section visibly covering
// the boundary AND the retention set.
func Lint(doc []byte) []Finding {
	lines := strings.Split(string(doc), "\n")
	// sections maps part phrase -> content lines until the next marker.
	sections := map[string][]string{}
	current := ""
	for _, line := range lines {
		if m := markerText(line); m != "" {
			current = ""
			for _, p := range parts {
				if strings.HasPrefix(m, p.phrase) {
					current = p.phrase
					if _, seen := sections[current]; !seen {
						sections[current] = []string{}
					}
					break
				}
			}
			continue
		}
		if current != "" && strings.TrimSpace(line) != "" {
			sections[current] = append(sections[current], line)
		}
	}
	var findings []Finding
	for _, p := range parts {
		content, ok := sections[p.phrase]
		if !ok {
			detail := fmt.Sprintf("the plan names no %q section", p.phrase)
			if p.missing == KindMissingRetention {
				detail = "a plan without a retention check does not lint: name what works and will be shown unharmed"
			}
			findings = append(findings, Finding{Kind: p.missing, Detail: detail})
			continue
		}
		if len(content) == 0 {
			findings = append(findings, Finding{Kind: KindEmptySection, Detail: fmt.Sprintf("the %q section is empty", p.phrase)})
		}
	}
	if cmds, ok := sections["validation commands"]; ok {
		if !hasLabeledCommand(cmds, "boundary:") {
			findings = append(findings, Finding{Kind: KindCommandsMissingBoundary, Detail: "the validation commands need a non-empty \"Boundary:\" command line covering the boundary set"})
		}
		if !hasLabeledCommand(cmds, "retention:") {
			findings = append(findings, Finding{Kind: KindCommandsMissingRetention, Detail: "the validation commands need a non-empty \"Retention:\" command line covering the retention set"})
		}
	}
	return findings
}

// Commands extracts the document's validation commands: the same
// marked-section walk Lint runs, exported so the verifier executes
// exactly what the lint reads (plans/os-f6d2c267.md; the acceptance
// body's command grammar is the plan grammar, spec/verdicts.md).
// Each non-empty line of the "validation commands" section yields one
// command: the list marker and any short leading label ("Boundary:",
// "Retention:") are stripped, and a line carrying a backtick span
// yields the span's content, so prose around the span never executes.
func Commands(doc []byte) []string {
	lines := strings.Split(string(doc), "\n")
	current := ""
	var cmds []string
	for _, line := range lines {
		if m := markerText(line); m != "" {
			if strings.HasPrefix(m, "validation commands") {
				current = "validation commands"
			} else {
				current = ""
			}
			continue
		}
		if current == "" {
			continue
		}
		t := strings.TrimLeft(strings.TrimSpace(line), "-*+ \t")
		if t == "" {
			continue
		}
		t = strings.TrimSpace(commandLabelRE.ReplaceAllString(t, ""))
		if i := strings.IndexByte(t, '`'); i >= 0 {
			if j := strings.IndexByte(t[i+1:], '`'); j >= 0 {
				t = t[i+1 : i+1+j]
			}
		}
		t = strings.TrimSpace(t)
		if t != "" {
			cmds = append(cmds, t)
		}
	}
	return cmds
}

// commandLabelRE matches a short leading label ("Boundary:",
// "Retention:") ahead of the command text; a real command's own
// colons sit past its first whitespace and never match.
var commandLabelRE = regexp.MustCompile(`^[A-Za-z][A-Za-z ]{0,19}:\s`)

// hasLabeledCommand reports whether some line carries the label (after
// any list marker or emphasis) with a non-empty command behind it:
// prose that merely mentions the word cannot satisfy the
// falsifiability contract (spec/plans.md).
func hasLabeledCommand(lines []string, label string) bool {
	for _, l := range lines {
		t := strings.TrimLeft(strings.TrimSpace(l), "-*+ \t")
		if len(t) >= len(label) && strings.EqualFold(t[:len(label)], label) && strings.TrimSpace(t[len(label):]) != "" {
			return true
		}
	}
	return false
}

// Class is a changed-path set's classification.
type Class string

const (
	ClassPlan           Class = "plan"
	ClassImplementation Class = "implementation"
	ClassMixed          Class = "mixed"
)

// PlansDir is the plan documents' directory in an instantiation.
const PlansDir = "plans/"

// Classify maps a changed-path set to plan | implementation | mixed:
// exactly one path, under plans/, is a plan PR; zero plans/** paths is
// an implementation PR; anything else — a mixture, or two plan files —
// is mixed, and the CI entrypoint refuses it. Structural disjointness
// is the III.F contract; making the check forge-required for
// self-hosted deployments is the Phase 12 protections reconciler's
// item.
func Classify(paths []string) Class {
	plans := 0
	for _, p := range paths {
		if strings.HasPrefix(p, PlansDir) {
			plans++
		}
	}
	switch {
	case plans == 0:
		return ClassImplementation
	case plans == len(paths) && plans == 1:
		return ClassPlan
	default:
		return ClassMixed
	}
}

// Item is one rubric item (plans/os-2e34f66a.md D1): the residue the
// acceptance spec could not make a command, scored item by item by the
// verifier with cited evidence and explicit uncertainty, never as one
// holistic score (SEED-NEXT.md §7).
type Item struct {
	ID        string `json:"id"`
	Criterion string `json:"criterion"`
}

// rubricIDRE is the id grammar: a slug, unique within the spec.
var rubricIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// RubricError names the part of a rubric the parser refuses: a render
// on such a spec refuses spec_unrunnable, since a rubric that cannot be
// scored item by item cannot decide.
type RubricError struct {
	Detail string
}

func (e *RubricError) Error() string { return "rubric: " + e.Detail }

// Rubric reads the "## Rubric" section exactly as Commands reads
// "Validation commands": each bullet is `- <id>: <criterion>`, the id
// a slug unique within the spec and the criterion non-empty. A spec
// may carry both sections; a spec with neither, or an empty section,
// yields no items and no error.
func Rubric(doc []byte) ([]Item, error) {
	lines := strings.Split(string(doc), "\n")
	current := ""
	var items []Item
	seen := map[string]bool{}
	for _, line := range lines {
		if m := markerText(line); m != "" {
			if m == "rubric" {
				current = "rubric"
			} else {
				current = ""
			}
			continue
		}
		if current == "" {
			continue
		}
		raw := strings.TrimSpace(line)
		if raw == "" || !strings.ContainsAny(raw[:1], "-*+") {
			continue
		}
		t := strings.TrimSpace(strings.TrimLeft(raw, "-*+ \t"))
		if t == "" {
			continue
		}
		id, criterion, ok := strings.Cut(t, ":")
		id = strings.Trim(strings.TrimSpace(id), "`*_")
		criterion = strings.TrimSpace(criterion)
		if !ok || id == "" {
			return nil, &RubricError{Detail: fmt.Sprintf("item %q carries no id: an item is `- <id>: <criterion>`", t)}
		}
		if !rubricIDRE.MatchString(id) {
			return nil, &RubricError{Detail: fmt.Sprintf("id %q is not a slug (lowercase letters, digits and dashes)", id)}
		}
		if criterion == "" {
			return nil, &RubricError{Detail: fmt.Sprintf("item %q carries no criterion", id)}
		}
		if seen[id] {
			return nil, &RubricError{Detail: fmt.Sprintf("id %q appears twice: an id is unique within the spec, since the scorecard cites it", id)}
		}
		seen[id] = true
		items = append(items, Item{ID: id, Criterion: criterion})
	}
	return items, nil
}

// TraceAttributesError names the part of a trace-attributes section
// the parser refuses: a receipt on such a spec refuses spec_unrunnable,
// since a declaration that cannot be read cannot bound what
// reproduces.
type TraceAttributesError struct {
	Detail string
}

func (e *TraceAttributesError) Error() string { return "trace attributes: " + e.Detail }

// TraceAttributes reads the "## Trace attributes" section exactly as
// Rubric reads "## Rubric" (plans/os-7fc2ca38.md D3): each bullet is
// one attribute key `- <key>`, the key non-empty, without whitespace
// and unique within the spec. Only declared keys survive trace
// normalization, so a spec with no such section retains none, which
// is a valid declaration and yields no error.
func TraceAttributes(doc []byte) ([]string, error) {
	lines := strings.Split(string(doc), "\n")
	current := ""
	var keys []string
	seen := map[string]bool{}
	for _, line := range lines {
		if m := markerText(line); m != "" {
			if m == "trace attributes" {
				current = m
			} else {
				current = ""
			}
			continue
		}
		if current == "" {
			continue
		}
		raw := strings.TrimSpace(line)
		if raw == "" || !strings.ContainsAny(raw[:1], "-*+") {
			continue
		}
		t := strings.TrimSpace(strings.TrimLeft(raw, "-*+ \t"))
		key := strings.Trim(t, "`*_ ")
		if key == "" {
			return nil, &TraceAttributesError{Detail: fmt.Sprintf("item %q carries no key: an item is `- <key>`", t)}
		}
		if strings.ContainsAny(key, " \t") {
			return nil, &TraceAttributesError{Detail: fmt.Sprintf("key %q carries whitespace: an attribute key is one token", key)}
		}
		if seen[key] {
			return nil, &TraceAttributesError{Detail: fmt.Sprintf("key %q appears twice: a key is declared once", key)}
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys, nil
}

// Scope lists the paths a plan's "File Scope" section names: every
// backticked path or prefix in that section, deduplicated, in order of
// appearance. A plan with no such section scopes nothing, which the
// lint already refuses. The path floor (plans/os-0d4f2af3.md D3)
// compares these against the declaration's guardrails.
func Scope(doc []byte) []string {
	lines := strings.Split(string(doc), "\n")
	in := false
	var out []string
	seen := map[string]bool{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			in = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")), "file scope")
			continue
		}
		if !in {
			continue
		}
		for _, tok := range backticked(trimmed) {
			tok = strings.TrimSuffix(strings.TrimSpace(tok), "/**")
			tok = strings.TrimSuffix(tok, "/*")
			tok = strings.TrimSuffix(tok, "/")
			if tok == "" || strings.ContainsAny(tok, " \t") {
				continue
			}
			// Canonical form, so `./next/x` and `next/x` are one token
			// and a floor's prefix compares against what the path is;
			// a parent or absolute token stays visible for the floor
			// check to refuse.
			tok = path.Clean(tok)
			if tok == "." || seen[tok] {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

func backticked(line string) []string {
	var out []string
	for {
		i := strings.Index(line, "`")
		if i < 0 {
			return out
		}
		rest := line[i+1:]
		j := strings.Index(rest, "`")
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		line = rest[j+1:]
	}
}
