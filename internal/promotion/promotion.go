// Package promotion reads the promotion evidence packet
// (docs/promotion.md; plans/os-98ce6f8a.md) and holds its
// citations to the tree. The packet is what the operator reads at the
// build plan's promotion gate (docs/build-plan.md section 5): one
// section per criterion, each opening with a status from a closed
// vocabulary and citing the drills that back it by name and file. A
// packet that cites a drill the tree no longer declares is a stale
// claim at the gate, which is the rot Check refuses.
//
// The parser is strict on purpose: the seven criteria in the build
// plan's order, exactly one status line each, evidence rows in one
// shape, a question on every reserved decision, a missing sentence on
// every criterion that is not met, and the III.R measurement ledger
// with one row per charter row. Nothing here decides anything; the
// packet presents and this package checks that what it presents is
// real.
package promotion

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PacketPath is the repo-relative path of the packet.
const PacketPath = "docs/promotion.md"

// Criteria is the count of build plan section 5's criteria; the packet
// carries exactly this many criterion sections, numbered 1 to 7.
const Criteria = 7

// Statuses is the closed vocabulary a criterion's status is drawn from.
// "reserved" marks a decision the packet presents but cannot make.
var Statuses = []string{"met", "partial", "not started", "reserved"}

// MeasureStatuses is the closed vocabulary of the III.R measurement
// ledger: a measure is measured or it is not.
var MeasureStatuses = []string{"not measured", "measured"}

// EscalationsHeading is the section that presents the two cutovers as
// reserved decisions, and EscalationNames are the two the build plan
// reserves (section 5): each must stand in that section as one
// question, never as an answer.
const EscalationsHeading = "## The two cutovers are escalations"

var EscalationNames = []string{"Self-hosting", "Distribution"}

// Criterion is one numbered section of the packet.
type Criterion struct {
	Number   int
	Title    string
	Status   string
	Missing  string     // the sentence after "Missing:", required unless met
	Question string     // the sentence after "Question:", required when reserved
	Evidence []Citation // the evidence rows in the section, in order
}

// Citation is one evidence row: a drill, the file that declares it, and
// the pull request that landed it.
type Citation struct {
	Drill string
	File  string // relative to next/
	PR    string
	Line  int // the packet line the row is on
}

// Measure is one row of the III.R measurement ledger.
type Measure struct {
	Row     string // R.1 .. R.7
	Measure string
	Surface string
	Status  string
}

// Escalation is one of the two cutovers, presented as the question the
// operator answers (plans/os-98ce6f8a.md D3).
type Escalation struct {
	Name     string // one of EscalationNames
	Question string
	Line     int
}

// Packet is the parsed document.
type Packet struct {
	Criteria    []Criterion
	Citations   []Citation // every evidence row in the packet, criterion sections and others alike
	Ledger      []Measure
	Escalations []Escalation
}

var (
	sectionRE   = regexp.MustCompile(`^## (\d+)\. (.+)$`)
	statusRE    = regexp.MustCompile(`^Status: (.+)$`)
	missingRE   = regexp.MustCompile(`^Missing: (.+)$`)
	questionRE  = regexp.MustCompile(`^Question: (.+)$`)
	drillCellRE = regexp.MustCompile("^`(Test[A-Za-z0-9_]+)`$")
	fileCellRE  = regexp.MustCompile("^`([A-Za-z0-9_./-]+)`$")
	prCellRE    = regexp.MustCompile(`^#\d+$`)
	ledgerRowRE = regexp.MustCompile(`^R\.([1-7])$`)
	// An escalation is one physical line: its name in bold, then the
	// question, so a deleted or answered question is a parse error and
	// not prose the gate reads past.
	escalationRE = regexp.MustCompile(`^\*\*([A-Za-z-]+)\.\*\* Question: (.+)$`)
	funcRE       = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
)

// Parse reads the packet's text. It refuses a packet whose shape drifts
// from the plan's, naming the line, so a review reads findings rather
// than prose.
func Parse(content string) (*Packet, error) {
	p := &Packet{}
	var cur *Criterion
	var errs []error
	seen := map[int]bool{}
	inEscalations := false
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), " \t")
		if strings.HasPrefix(text, "## ") {
			cur = nil
			inEscalations = text == EscalationsHeading
			if m := sectionRE.FindStringSubmatch(text); m != nil {
				n, _ := strconv.Atoi(m[1])
				if seen[n] {
					errs = append(errs, fmt.Errorf("line %d: criterion %d appears twice", line, n))
					continue
				}
				seen[n] = true
				p.Criteria = append(p.Criteria, Criterion{Number: n, Title: strings.TrimSpace(m[2])})
				cur = &p.Criteria[len(p.Criteria)-1]
			}
			continue
		}
		if inEscalations {
			if m := escalationRE.FindStringSubmatch(text); m != nil {
				p.Escalations = append(p.Escalations, Escalation{Name: m[1], Question: strings.TrimSpace(m[2]), Line: line})
			}
			continue
		}
		if cur != nil {
			if m := statusRE.FindStringSubmatch(text); m != nil {
				if cur.Status != "" {
					errs = append(errs, fmt.Errorf("line %d: criterion %d carries a second status line", line, cur.Number))
					continue
				}
				cur.Status = strings.TrimSpace(m[1])
				continue
			}
			if m := missingRE.FindStringSubmatch(text); m != nil {
				cur.Missing = strings.TrimSpace(m[1])
				continue
			}
			if m := questionRE.FindStringSubmatch(text); m != nil {
				cur.Question = strings.TrimSpace(m[1])
				continue
			}
		}
		if !strings.HasPrefix(text, "|") {
			continue
		}
		cells := splitRow(text)
		if len(cells) == 0 {
			continue
		}
		if m := ledgerRowRE.FindStringSubmatch(cells[0]); m != nil {
			if len(cells) != 4 {
				errs = append(errs, fmt.Errorf("line %d: ledger row %s has %d cells, the ledger has four", line, cells[0], len(cells)))
				continue
			}
			p.Ledger = append(p.Ledger, Measure{Row: cells[0], Measure: cells[1], Surface: cells[2], Status: cells[3]})
			continue
		}
		if !strings.HasPrefix(cells[0], "`Test") {
			continue
		}
		c, err := parseCitation(cells, line)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p.Citations = append(p.Citations, c)
		if cur != nil {
			cur.Evidence = append(cur.Evidence, c)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	errs = append(errs, p.validate()...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return p, nil
}

func splitRow(text string) []string {
	trimmed := strings.Trim(strings.TrimSpace(text), "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func parseCitation(cells []string, line int) (Citation, error) {
	if len(cells) != 3 {
		return Citation{}, fmt.Errorf("line %d: an evidence row is drill, file and pull request; this one has %d cells", line, len(cells))
	}
	d := drillCellRE.FindStringSubmatch(cells[0])
	f := fileCellRE.FindStringSubmatch(cells[1])
	if d == nil || f == nil || !prCellRE.MatchString(cells[2]) {
		return Citation{}, fmt.Errorf("line %d: an evidence row is `TestName` | `path/under/next` | #pr; got %q", line, strings.Join(cells, " | "))
	}
	if !strings.HasSuffix(f[1], "_test.go") {
		return Citation{}, fmt.Errorf("line %d: %s cites %s, which is not a test file", line, d[1], f[1])
	}
	if !underNext(f[1]) {
		return Citation{}, fmt.Errorf("line %d: %s cites %s, which is not a clean relative path under next/", line, d[1], f[1])
	}
	return Citation{Drill: d[1], File: f[1], PR: cells[2], Line: line}, nil
}

// underNext reports whether a cited file is a clean relative path
// whose every element names a directory or file: no leading slash, no
// "." or ".." element, nothing filepath.Join would clean into a read
// outside next/.
func underNext(file string) bool {
	if file == "" || strings.HasPrefix(file, "/") || path.Clean(file) != file {
		return false
	}
	for _, el := range strings.Split(file, "/") {
		if el == "" || el == "." || el == ".." {
			return false
		}
	}
	return true
}

func (p *Packet) validate() []error {
	var errs []error
	if len(p.Criteria) != Criteria {
		errs = append(errs, fmt.Errorf("the packet carries %d criterion sections; build plan section 5 has %d", len(p.Criteria), Criteria))
	}
	for i, c := range p.Criteria {
		if c.Number != i+1 {
			errs = append(errs, fmt.Errorf("criterion %d is out of order (position %d)", c.Number, i+1))
		}
		if c.Status == "" {
			errs = append(errs, fmt.Errorf("criterion %d has no status line", c.Number))
			continue
		}
		if !contains(Statuses, c.Status) {
			errs = append(errs, fmt.Errorf("criterion %d: status %q is outside the vocabulary %v", c.Number, c.Status, Statuses))
			continue
		}
		if (c.Status == "met" || c.Status == "partial") && len(c.Evidence) == 0 {
			errs = append(errs, fmt.Errorf("criterion %d is %s with no evidence row", c.Number, c.Status))
		}
		if c.Status != "met" && c.Missing == "" {
			errs = append(errs, fmt.Errorf("criterion %d is %s and does not say what is missing (a Missing: line)", c.Number, c.Status))
		}
		if c.Status == "reserved" && c.Question == "" {
			errs = append(errs, fmt.Errorf("criterion %d is reserved and asks no question (a Question: line)", c.Number))
		}
	}
	rows := map[string]bool{}
	for _, m := range p.Ledger {
		if rows[m.Row] {
			errs = append(errs, fmt.Errorf("ledger row %s appears twice", m.Row))
		}
		rows[m.Row] = true
		if !contains(MeasureStatuses, m.Status) {
			errs = append(errs, fmt.Errorf("ledger row %s: status %q is outside the vocabulary %v", m.Row, m.Status, MeasureStatuses))
		}
		if m.Measure == "" || m.Surface == "" {
			errs = append(errs, fmt.Errorf("ledger row %s names no measure or no surface", m.Row))
		}
	}
	for i := 1; i <= 7; i++ {
		if !rows["R."+strconv.Itoa(i)] {
			errs = append(errs, fmt.Errorf("the III.R measurement ledger lacks row R.%d", i))
		}
	}
	questions := map[string]string{}
	for _, e := range p.Escalations {
		if _, dup := questions[e.Name]; dup {
			errs = append(errs, fmt.Errorf("line %d: the %s cutover is presented twice", e.Line, e.Name))
			continue
		}
		if !contains(EscalationNames, e.Name) {
			errs = append(errs, fmt.Errorf("line %d: %q is not one of the two cutovers %v", e.Line, e.Name, EscalationNames))
			continue
		}
		questions[e.Name] = e.Question
		if !strings.HasSuffix(e.Question, "?") {
			errs = append(errs, fmt.Errorf("line %d: the %s cutover's question does not end in a question mark: %q", e.Line, e.Name, e.Question))
		}
	}
	for _, name := range EscalationNames {
		if _, ok := questions[name]; !ok {
			errs = append(errs, fmt.Errorf("the section %q presents no question for the %s cutover (a **%s.** Question: line)", EscalationsHeading, name, name))
		}
	}
	return errs
}

func contains(set []string, s string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

// Finding is one citation the tree does not hold.
type Finding struct {
	Citation Citation
	Reason   string
}

func (f Finding) String() string {
	return fmt.Sprintf("line %d: %s (%s): %s", f.Citation.Line, f.Citation.Drill, f.Citation.File, f.Reason)
}

// Check holds every citation to the tree under root (the repository
// root): the cited file exists under next/ and declares the cited
// drill as a top-level test function. Findings are in packet order.
func Check(root string, p *Packet) ([]Finding, error) {
	var out []Finding
	files := map[string]string{}
	for _, c := range p.Citations {
		path := filepath.Join(root, filepath.FromSlash(c.File))
		src, ok := files[c.File]
		if !ok {
			b, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					out = append(out, Finding{Citation: c, Reason: "the cited file does not exist"})
					files[c.File] = ""
					continue
				}
				return nil, err
			}
			src = string(b)
			files[c.File] = src
		}
		if src == "" {
			out = append(out, Finding{Citation: c, Reason: "the cited file does not exist"})
			continue
		}
		if !declares(src, c.Drill) {
			out = append(out, Finding{Citation: c, Reason: "the cited file declares no such test"})
		}
	}
	return out, nil
}

func declares(src, drill string) bool {
	for _, m := range funcRE.FindAllStringSubmatch(src, -1) {
		if m[1] == drill {
			return true
		}
	}
	return false
}

// Load reads and parses the packet under root.
func Load(root string) (*Packet, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(PacketPath)))
	if err != nil {
		return nil, err
	}
	return Parse(string(b))
}

// Drills lists every distinct drill the packet cites, sorted, for a
// reader that wants the evidence as a set.
func (p *Packet) Drills() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range p.Citations {
		key := c.File + ":" + c.Drill
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
