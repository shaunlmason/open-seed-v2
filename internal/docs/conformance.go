package docs

import (
	"fmt"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/conformance"
)

// renderConformance renders the Part III table (plans/os-83bc3d84.md
// D3): a summary of every pillar's rows by status, then each pillar's
// rows with the status, the phase whose record judged it, the
// charter's own text, the evidence and the note. The table is loaded
// and validated against the charter first, so a table that drifted
// from SEED-NEXT.md never renders. No date and no clock: the table is
// the source and the exit records are the evidence.
func renderConformance(root string) (string, error) {
	t, err := conformance.Load(root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(preamble("spec/conformance.json, held row for row to SEED-NEXT.md Part III"))
	b.WriteString("# Part III conformance\n\n")
	b.WriteString("Every row of the charter's Part III with the status the phase exit records gave it: ")
	b.WriteString("`met` with its evidence, `partial` or `routed` with a note naming where the rest lives, `open` where no record has met it. ")
	b.WriteString("Rows marked `(*enforced-only*)` are judged at the enforced postures alone; `seed doctor` reports the table at the declared posture.\n\n")
	b.WriteString("## Summary\n\n| pillar | met | partial | routed | open |\n|---|---|---|---|---|\n")
	var all []conformance.Row
	for _, p := range t.Pillars {
		c := conformance.Count(p.Rows)
		fmt.Fprintf(&b, "| %s. %s | %d | %d | %d | %d |\n", p.ID, p.Title, c.Met, c.Partial, c.Routed, c.Open)
		all = append(all, p.Rows...)
	}
	c := conformance.Count(all)
	fmt.Fprintf(&b, "| **all** | %d | %d | %d | %d |\n", c.Met, c.Partial, c.Routed, c.Open)
	for _, p := range t.Pillars {
		fmt.Fprintf(&b, "\n## %s. %s\n\n| row | status | phase | criterion | evidence | note |\n|---|---|---|---|---|---|\n", p.ID, p.Title)
		for _, r := range p.Rows {
			fmt.Fprintf(&b, "| %s.%d | `%s` | %s | %s | %s | %s |\n", p.ID, r.Row, r.Status, cell(r.Phase), cell(r.Text), cell(r.Evidence), cell(r.Note))
		}
	}
	return b.String(), nil
}

// cell escapes a value for a table cell; an empty value renders as a
// dash so the columns stay aligned for a reader.
func cell(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "|", "\\|")
}
