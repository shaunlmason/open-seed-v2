// Package conformance is the Part III table (plans/os-83bc3d84.md;
// build plan Phase 13's exit line): the charter's conformance rows,
// each with the status the exit records gave it, held row for row to
// SEED-NEXT.md by a parser of the charter itself. It is a document
// and a doctor section, never an admission rule: nothing here refuses
// a record, and the table's authority is the exit records it
// transcribes.
package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The status vocabulary (D1): met with evidence, partial or routed
// with a note naming where the rest lives, or open.
const (
	StatusMet     = "met"
	StatusPartial = "partial"
	StatusRouted  = "routed"
	StatusOpen    = "open"
)

// Statuses is the vocabulary in the order the report counts it.
var Statuses = []string{StatusMet, StatusPartial, StatusRouted, StatusOpen}

// The postures a row is judged at: any deployment; the enforced
// postures alone (the charter's (*enforced-only*) marker opening the
// row); or mixed, a row whose marker qualifies one clause, judged at
// every posture with the enforced clause named as not exercised at
// the cooperative one (review finding on the plan PR).
const (
	PostureAny          = "any"
	PostureEnforcedOnly = "enforced-only"
	PostureMixed        = "mixed"
)

// Marker is the charter's enforced-only marker.
const Marker = "(*enforced-only*)"

// TablePath is the table's location under the repository root, and
// CharterPath the charter's.
const (
	TablePath   = "spec/conformance.json"
	CharterPath = "SEED-NEXT.md"
)

// Row is one charter row and its judgment.
type Row struct {
	Row      int    `json:"row"`
	Text     string `json:"text"`
	Posture  string `json:"posture"`
	Status   string `json:"status"`
	Phase    string `json:"phase"`
	Evidence string `json:"evidence"`
	Note     string `json:"note"`
}

// Pillar is one lettered section of Part III.
type Pillar struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Rows  []Row  `json:"rows"`
}

// Table is the whole of Part III as judged.
type Table struct {
	Pillars []Pillar `json:"pillars"`
}

// CharterRow is one row as the charter states it.
type CharterRow struct {
	Row     int
	Text    string
	Posture string
}

// CharterPillar is one lettered section as the charter states it.
type CharterPillar struct {
	ID    string
	Title string
	Rows  []CharterRow
}

var (
	pillarHeading = regexp.MustCompile(`^### ([A-R])\. (.+)$`)
	rowStart      = regexp.MustCompile(`^- \[ \] (.*)$`)
	spaces        = regexp.MustCompile(`\s+`)
)

// ParseCharter reads Part III out of the charter: the lettered
// headings and every checkbox row with its continuation lines
// (indented by six spaces), whitespace-normalized, the posture read
// off the (*enforced-only*) marker. The section runs from the "Part
// III" heading to the next second-level heading.
func ParseCharter(b []byte) ([]CharterPillar, error) {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## Part III") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, errors.New("the charter has no \"## Part III\" heading")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	var pillars []CharterPillar
	var cur *CharterPillar
	var row *CharterRow
	for _, l := range lines[start:end] {
		if m := pillarHeading.FindStringSubmatch(l); m != nil {
			pillars = append(pillars, CharterPillar{ID: m[1], Title: strings.TrimSpace(m[2])})
			cur = &pillars[len(pillars)-1]
			row = nil
			continue
		}
		if cur == nil {
			continue
		}
		if m := rowStart.FindStringSubmatch(l); m != nil {
			cur.Rows = append(cur.Rows, CharterRow{Row: len(cur.Rows) + 1, Text: strings.TrimSpace(m[1])})
			row = &cur.Rows[len(cur.Rows)-1]
			continue
		}
		if row != nil && strings.HasPrefix(l, "      ") && strings.TrimSpace(l) != "" {
			row.Text += " " + strings.TrimSpace(l)
			continue
		}
		if strings.TrimSpace(l) == "" {
			row = nil
		}
	}
	if len(pillars) == 0 {
		return nil, errors.New("the charter's Part III has no lettered pillars")
	}
	for i := range pillars {
		for j := range pillars[i].Rows {
			r := &pillars[i].Rows[j]
			r.Text = strings.TrimSpace(spaces.ReplaceAllString(r.Text, " "))
			switch {
			case strings.HasPrefix(r.Text, Marker):
				r.Posture = PostureEnforcedOnly
			case strings.Contains(r.Text, Marker):
				r.Posture = PostureMixed
			default:
				r.Posture = PostureAny
			}
		}
	}
	return pillars, nil
}

// Parse decodes the table strictly.
func Parse(b []byte) (Table, error) {
	var t Table
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return Table{}, fmt.Errorf("the conformance table is the strict object {pillars: [{id, title, rows: [{row, text, posture, status, phase, evidence, note}]}]}: %v", err)
	}
	// One object and nothing after it: a second decode must hit EOF,
	// since More reports only further array or object elements and
	// would accept a stray closing delimiter (review finding on the
	// task PR).
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return Table{}, errors.New("the conformance table is one object, and bytes follow it")
	}
	return t, nil
}

// Load reads and validates the table and the charter under root.
func Load(root string) (Table, error) {
	tb, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(TablePath)))
	if err != nil {
		return Table{}, err
	}
	t, err := Parse(tb)
	if err != nil {
		return Table{}, err
	}
	cb, err := os.ReadFile(filepath.Join(root, CharterPath))
	if err != nil {
		return Table{}, err
	}
	charter, err := ParseCharter(cb)
	if err != nil {
		return Table{}, err
	}
	if err := Validate(t, charter); err != nil {
		return Table{}, err
	}
	return t, nil
}

// Validate holds the table to the charter row for row (D2) and to the
// vocabulary (D1): the same pillars in the same order with the same
// titles, the same rows with the same text and posture, every status
// in the vocabulary, met rows carrying evidence, partial and routed
// rows carrying a note.
func Validate(t Table, charter []CharterPillar) error {
	if len(t.Pillars) != len(charter) {
		return fmt.Errorf("the table has %d pillars, the charter %d", len(t.Pillars), len(charter))
	}
	for i, p := range t.Pillars {
		c := charter[i]
		if p.ID != c.ID || p.Title != c.Title {
			return fmt.Errorf("pillar %d: the table says %s. %q, the charter %s. %q", i+1, p.ID, p.Title, c.ID, c.Title)
		}
		if len(p.Rows) != len(c.Rows) {
			return fmt.Errorf("pillar %s: the table has %d rows, the charter %d", p.ID, len(p.Rows), len(c.Rows))
		}
		for j, r := range p.Rows {
			cr := c.Rows[j]
			id := fmt.Sprintf("%s.%d", p.ID, j+1)
			if r.Row != cr.Row {
				return fmt.Errorf("%s: the row is numbered %d", id, r.Row)
			}
			if r.Text != cr.Text {
				return fmt.Errorf("%s: the table's text is not the charter's:\n table: %s\n charter: %s", id, r.Text, cr.Text)
			}
			if r.Posture != cr.Posture {
				return fmt.Errorf("%s: the posture is %q, the charter says %q", id, r.Posture, cr.Posture)
			}
			if err := validateStatus(r); err != nil {
				return fmt.Errorf("%s: %v", id, err)
			}
		}
	}
	return nil
}

func validateStatus(r Row) error {
	switch r.Status {
	case StatusMet:
		if strings.TrimSpace(r.Evidence) == "" {
			return errors.New("a met row names its evidence")
		}
	case StatusPartial, StatusRouted:
		if strings.TrimSpace(r.Note) == "" {
			return fmt.Errorf("a %s row's note names where the rest lives", r.Status)
		}
	case StatusOpen:
	default:
		return fmt.Errorf("status %q is outside the vocabulary %v", r.Status, Statuses)
	}
	return nil
}

// Counts is the table's rows by status.
type Counts struct {
	Met     int `json:"met"`
	Partial int `json:"partial"`
	Routed  int `json:"routed"`
	Open    int `json:"open"`
}

// Count tallies a set of rows.
func Count(rows []Row) Counts {
	var c Counts
	for _, r := range rows {
		switch r.Status {
		case StatusMet:
			c.Met++
		case StatusPartial:
			c.Partial++
		case StatusRouted:
			c.Routed++
		default:
			c.Open++
		}
	}
	return c
}

// OutstandingRow names one row not yet met: its id, status, phase,
// posture and the note saying where the rest lives.
type OutstandingRow struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Phase   string `json:"phase,omitempty"`
	Posture string `json:"posture"`
	Note    string `json:"note,omitempty"`
}

// Report is what the doctor says about the table at a posture (D4):
// the counts over the rows judged there, every row not yet met (open,
// partial and routed alike: a criterion is met or it is not), the
// enforced-only rows a cooperative deployment cannot judge (the
// charter asks such a deployment to document exactly these), the
// mixed rows whose enforced clause that posture does not exercise,
// and whether Part III is complete at this posture: every applicable
// row met. The charter defines conformance at the declared posture
// and asks a cooperative deployment only to document the
// enforced-only criteria that do not hold for it, so those rows are
// listed, not counted against it (review finding on the task PR).
type Report struct {
	Counts        Counts           `json:"counts"`
	Outstanding   []OutstandingRow `json:"outstanding_rows"`
	NotApplicable []string         `json:"not_applicable_here,omitempty"`
	MixedHere     []string         `json:"mixed_here,omitempty"`
	Complete      bool             `json:"complete"`
	Because       string           `json:"because"`
}

// Assess judges the table at a posture: enforced reads every row;
// cooperative sets the enforced-only rows aside as not applicable and
// judges the mixed rows while naming them. Complete means every
// applicable row is met: partial and routed rows are outstanding,
// since the charter admits a conformance claim only when every
// criterion holds (review finding on the plan PR).
func Assess(t Table, enforced bool) Report {
	var rep Report
	var judged []Row
	for _, p := range t.Pillars {
		for _, r := range p.Rows {
			id := fmt.Sprintf("%s.%d", p.ID, r.Row)
			if !enforced && r.Posture == PostureEnforcedOnly {
				rep.NotApplicable = append(rep.NotApplicable, id)
				continue
			}
			if !enforced && r.Posture == PostureMixed {
				rep.MixedHere = append(rep.MixedHere, id)
			}
			judged = append(judged, r)
			if r.Status != StatusMet {
				rep.Outstanding = append(rep.Outstanding, OutstandingRow{ID: id, Status: r.Status, Phase: r.Phase, Posture: r.Posture, Note: r.Note})
			}
		}
	}
	rep.Counts = Count(judged)
	sort.Strings(rep.NotApplicable)
	sort.Strings(rep.MixedHere)
	posture := "the enforced posture"
	if !enforced {
		posture = fmt.Sprintf("the cooperative posture, with %d enforced-only rows documented as not holding here", len(rep.NotApplicable))
	}
	switch {
	case len(rep.Outstanding) > 0:
		rep.Complete = false
		rep.Because = fmt.Sprintf("%d applicable rows are not met (%d open, %d partial, %d routed) at %s", len(rep.Outstanding), rep.Counts.Open, rep.Counts.Partial, rep.Counts.Routed, posture)
	default:
		rep.Complete = true
		rep.Because = "every applicable row is met at " + posture
	}
	if rep.Outstanding == nil {
		rep.Outstanding = []OutstandingRow{}
	}
	return rep
}
