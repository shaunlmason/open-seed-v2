package importer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Entry is one attributed run-log line: v1 wrote {ts, actor, verb,
// task, data} per verb, data a small object or absent.
type Entry struct {
	Index int
	TS    string
	Actor string
	Verb  string
	Task  string
	Data  map[string]any
}

// Str reads a string field of the entry's data.
func (e Entry) Str(k string) string {
	if e.Data == nil {
		return ""
	}
	s, _ := e.Data[k].(string)
	return s
}

// ParseRunLog reads run-log.jsonl in order; a line that does not parse
// refuses the import, since a history with an unreadable line is not
// the history the export claims.
func ParseRunLog(content string) ([]Entry, error) {
	var out []Entry
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			TS    string         `json:"ts"`
			Actor string         `json:"actor"`
			Verb  string         `json:"verb"`
			Task  string         `json:"task"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("run-log.jsonl line %d does not parse: %v", i+1, err)
		}
		if raw.Verb == "" || raw.TS == "" {
			return nil, fmt.Errorf("run-log.jsonl line %d has no verb or ts", i+1)
		}
		if raw.Task != "" && !ValidID(raw.Task) {
			return nil, fmt.Errorf("run-log.jsonl line %d: task %q is not an id the transform accepts", i+1, raw.Task)
		}
		out = append(out, Entry{Index: len(out), TS: raw.TS, Actor: raw.Actor, Verb: raw.Verb, Task: raw.Task, Data: raw.Data})
	}
	return out, nil
}

// Evidence is one `## Evidence ev-… (kind, actor, ts)` block of a card.
type Evidence struct {
	ID    string
	Kind  string
	Actor string
	TS    string
	Text  string
}

// Card is a v1 task card: YAML-ish frontmatter, a body, evidence blocks.
type Card struct {
	Path      string
	ID        string
	Title     string
	State     string
	Priority  string
	Squad     string
	Author    string
	CreatedAt string
	UpdatedAt string
	Body      string
	Evidence  []Evidence
	Raw       string
}

var evidenceHead = regexp.MustCompile(`^## Evidence (ev-[0-9a-f]+) \(([^,()]+), ([^,()]*), ([^()]+)\)\s*$`)

// ParseCard reads a card: the frontmatter between the two `---` lines
// as key: value pairs (quotes stripped), the body, and the evidence
// blocks in order.
func ParseCard(path, content string) (*Card, error) {
	c := &Card{Path: path, Raw: content}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("%s: a card starts with a --- frontmatter line", path)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("%s: the frontmatter never closes", path)
	}
	for _, l := range lines[1:end] {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'' || v[0] == '"' && v[len(v)-1] == '"') {
			v = v[1 : len(v)-1]
			v = strings.ReplaceAll(v, "''", "'")
		}
		switch k {
		case "id":
			c.ID = v
		case "title":
			c.Title = v
		case "state":
			c.State = v
		case "priority":
			c.Priority = v
		case "squad":
			c.Squad = v
		case "author":
			c.Author = v
		case "created_at":
			c.CreatedAt = v
		case "updated_at":
			c.UpdatedAt = v
		}
	}
	if c.ID == "" || c.State == "" {
		return nil, fmt.Errorf("%s: a card names its id and state", path)
	}
	var body []string
	var cur *Evidence
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(cur.Text)
			c.Evidence = append(c.Evidence, *cur)
			cur = nil
		}
	}
	for _, l := range lines[end+1:] {
		if m := evidenceHead.FindStringSubmatch(l); m != nil {
			flush()
			cur = &Evidence{ID: m[1], Kind: strings.TrimSpace(m[2]), Actor: strings.TrimSpace(m[3]), TS: strings.TrimSpace(m[4])}
			continue
		}
		if cur != nil {
			cur.Text += l + "\n"
		} else {
			body = append(body, l)
		}
	}
	flush()
	c.Body = strings.TrimSpace(strings.Join(body, "\n"))
	return c, nil
}

// Handoff is a v1 continuation packet's mechanical sections: the task
// line, the workspace anchor, and what it was blocked on.
type Handoff struct {
	Path    string
	ID      string
	Task    string
	Anchor  string // "branch <name> @ <sha>" as written
	Blocked string
	Raw     string
}

// ParseHandoff reads the sections a packet is synthesized from; prose
// is never read.
func ParseHandoff(path, content string) *Handoff {
	h := &Handoff{Path: path, Raw: content}
	h.ID = strings.TrimSuffix(strings.TrimPrefix(path, "handoff/"), ".md")
	section := ""
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(t, "## "))
			continue
		}
		if t == "" || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "#") {
			continue
		}
		switch section {
		case "Task":
			if h.Task == "" {
				h.Task = t
			}
		case "Workspace anchor":
			if h.Anchor == "" {
				h.Anchor = t
			}
		case "Blocked on":
			if h.Blocked == "" {
				h.Blocked = t
			}
		}
	}
	return h
}

var shaRE = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)

// AnchorSHA extracts the commit a handoff's workspace anchor names.
func (h *Handoff) AnchorSHA() string {
	if h == nil {
		return ""
	}
	_, after, ok := strings.Cut(h.Anchor, "@")
	if !ok {
		return ""
	}
	return shaRE.FindString(strings.TrimSpace(after))
}

// Source is the export read into its record kinds. Every file of the
// export is exactly one of: the run-log (whose entries are the
// records), a card, a handoff, a mail file, or an unknown file, which
// is recorded as such so the disposition count still covers it.
type Source struct {
	Export   *Export
	Entries  []Entry
	Cards    map[string]*Card
	CardIDs  []string
	Handoffs map[string]*Handoff
	Mail     []string
	Other    []string
}

// idRE is the predecessor's id grammar as the transform trusts it: a
// task id becomes a subject, a path segment under tasks/, handoff/,
// plans/ and receipts/, so it is held to one safe shape before
// anything is derived from it.
var idRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidID reports an id the transform accepts.
func ValidID(id string) bool {
	return idRE.MatchString(id) && !strings.Contains(id, "..")
}

// Read parses the export's files.
func Read(e *Export) (*Source, error) {
	s := &Source{Export: e, Cards: map[string]*Card{}, Handoffs: map[string]*Handoff{}}
	paths := make([]string, 0, len(e.Files))
	for p := range e.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		content := e.Files[p]
		switch {
		case p == "run-log.jsonl":
			entries, err := ParseRunLog(content)
			if err != nil {
				return nil, err
			}
			s.Entries = entries
		case strings.HasPrefix(p, "tasks/") && strings.HasSuffix(p, ".md"):
			c, err := ParseCard(p, content)
			if err != nil {
				return nil, err
			}
			if !ValidID(c.ID) || p != "tasks/"+c.ID+".md" {
				return nil, fmt.Errorf("%s: card id %q is not an id the transform accepts (one path segment, letters, digits, . _ -)", p, c.ID)
			}
			s.Cards[c.ID] = c
			s.CardIDs = append(s.CardIDs, c.ID)
		case strings.HasPrefix(p, "handoff/") && strings.HasSuffix(p, ".md"):
			h := ParseHandoff(p, content)
			if !ValidID(h.ID) {
				return nil, fmt.Errorf("%s: handoff id %q is not an id the transform accepts", p, h.ID)
			}
			s.Handoffs[h.ID] = h
		case strings.HasPrefix(p, "mail/"):
			s.Mail = append(s.Mail, p)
		default:
			s.Other = append(s.Other, p)
		}
	}
	return s, nil
}

// Verbs lists the distinct run-log verbs in first-seen order.
func (s *Source) Verbs() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range s.Entries {
		if !seen[e.Verb] {
			seen[e.Verb] = true
			out = append(out, e.Verb)
		}
	}
	return out
}

// RecordCount is the number of export records the manifest must give
// a disposition: every run-log entry, plus every file that is not the
// run-log.
func (s *Source) RecordCount() int {
	n := len(s.Entries)
	for p := range s.Export.Files {
		if p != "run-log.jsonl" {
			n++
		}
	}
	return n
}
