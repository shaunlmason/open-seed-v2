// Search: an advisory lexical read over what the pipeline holds
// (spec/curation.md "Search"). The claim-time delivery is a
// predicate match, exact and auditable, and it answers one question:
// which promoted lessons select THIS subject. A worker with a question
// mid-task has no surface for it short of reading the whole store, the
// dead ends by contract and the decision log top to bottom. This file
// is that surface: BM25 over the surfacing set, the standing dead ends
// and any markdown the reader names, ranked, never delivered. Nothing
// here changes what a claim receives.
//
// The retrieval idea is okf-agent-memory's (github.com/okf-memory/
// okf-agent-memory: local lexical BM25 over a git-native markdown
// store, no embeddings, no vendor). Its format and its self-declared
// trust tiers were not adopted: the pipeline's stages are enforced at
// the boundary, and a lesson reaches the store through a gate, not a
// frontmatter label. Reimplemented rather than imported: the corpus
// is derived from the fold and verified against the repository the
// reader holds, which no external index knows how to do, and the
// scorer is two hundred lines against a dependency on the trust
// surface.

package curation

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
)

// The kinds a searched document can be.
const (
	SearchKindLesson  = "lesson"
	SearchKindDeadEnd = "deadend"
	SearchKindDoc     = "doc"
)

// BM25 parameters, the literature's defaults: k1 saturates term
// frequency, b weights length normalization.
const (
	BM25K1 = 1.2
	BM25B  = 0.75
)

// SnippetRunes bounds the rendered snippet.
const SnippetRunes = 200

// Document is one indexed unit: a verified lesson at its anchor, a
// standing dead end, or a section of a named markdown file.
type Document struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Text is indexed and never rendered whole: a hit carries a
	// snippet, and the reader goes to the id for the rest.
	Text string `json:"-"`
}

// Hit is one ranked result.
type Hit struct {
	Rank    int      `json:"rank"`
	Score   float64  `json:"score"`
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Snippet string   `json:"snippet"`
	Matched []string `json:"matched"`
}

// Index is an in-memory BM25 index over a corpus.
type Index struct {
	docs   []Document
	tf     []map[string]int
	length []int
	df     map[string]int
	avgdl  float64
}

// Tokenize lowercases and splits on anything that is not a letter or
// a digit, keeping tokens of two or more runes. No stemming and no
// stop list: the inverse document frequency already discounts what
// every document says, and one derivation with no knobs is easier to
// reason about than a better one with three.
func Tokenize(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) >= 2 {
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return out
}

// NewIndex builds the index over the documents, in the order given.
func NewIndex(docs []Document) *Index {
	ix := &Index{docs: docs, df: map[string]int{}}
	total := 0
	for _, d := range docs {
		freq := map[string]int{}
		toks := Tokenize(d.Title + "\n" + d.Text)
		for _, t := range toks {
			freq[t]++
		}
		for t := range freq {
			ix.df[t]++
		}
		ix.tf = append(ix.tf, freq)
		ix.length = append(ix.length, len(toks))
		total += len(toks)
	}
	if len(docs) > 0 {
		ix.avgdl = float64(total) / float64(len(docs))
	}
	return ix
}

// Len is the number of indexed documents.
func (ix *Index) Len() int { return len(ix.docs) }

// idf is the Lucene form, ln(1 + (N - df + 0.5) / (df + 0.5)): never
// negative, so a term in most documents counts a little rather than
// against.
func (ix *Index) idf(term string) float64 {
	n, df := float64(len(ix.docs)), float64(ix.df[term])
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// QueryTerms are the query's distinct tokens in first-seen order.
func QueryTerms(query string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range Tokenize(query) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Search ranks the documents against the query: the sum over the
// query's distinct terms of idf times the saturated, length-normalized
// term frequency. A document scoring zero is no hit. Ties break on
// kind then id, so the ranking is a function of the corpus and the
// query alone. limit at or below zero means every hit.
func (ix *Index) Search(query string, limit int) []Hit {
	terms := QueryTerms(query)
	// Never nil: an envelope renders no hits as an empty list, not null.
	hits := []Hit{}
	for i, d := range ix.docs {
		score, matched := 0.0, []string{}
		norm := BM25K1 * (1 - BM25B + BM25B*float64(ix.length[i])/ix.avgdlOrOne())
		for _, t := range terms {
			tf := float64(ix.tf[i][t])
			if tf == 0 {
				continue
			}
			score += ix.idf(t) * tf * (BM25K1 + 1) / (tf + norm)
			matched = append(matched, t)
		}
		if score <= 0 {
			continue
		}
		hits = append(hits, Hit{Score: score, Kind: d.Kind, ID: d.ID, Title: d.Title, Snippet: snippet(d.Text, matched), Matched: matched})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		if hits[a].Kind != hits[b].Kind {
			return hits[a].Kind < hits[b].Kind
		}
		return hits[a].ID < hits[b].ID
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	for i := range hits {
		hits[i].Rank = i + 1
	}
	return hits
}

func (ix *Index) avgdlOrOne() float64 {
	if ix.avgdl == 0 {
		return 1
	}
	return ix.avgdl
}

// snippet is the first body line carrying a matched term, trimmed and
// bounded; the first non-empty body line when no line does (a
// title-only match). Headings are skipped: the hit's title already
// shows the first, and a section's own heading says nothing its title
// did not.
func snippet(text string, matched []string) string {
	want := map[string]bool{}
	for _, t := range matched {
		want[t] = true
	}
	first, heading := "", ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if heading == "" {
				heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
			}
			continue
		}
		if first == "" {
			first = line
		}
		for _, t := range Tokenize(line) {
			if want[t] {
				return truncate(line)
			}
		}
	}
	if first == "" {
		// A document that is all heading: the heading is the snippet.
		return truncate(heading)
	}
	return truncate(first)
}

func truncate(s string) string {
	r := []rune(s)
	if len(r) <= SnippetRunes {
		return s
	}
	return string(r[:SnippetRunes]) + "…"
}

// LessonDocuments is the searchable slice of the store: every lesson
// in the surfacing set at the instant, subject-less (promoted, not
// contested, not retired, not expired), whose fact resolves in the
// repository, read at its anchor so the indexed text is the reviewed
// bytes and never the working tree's. The rest is reported unresolved
// exactly as delivery reports it; with no repository nothing is
// indexed, the delivery posture.
func LessonDocuments(st *State, repo string, at time.Time) ([]Document, []Unresolved) {
	docs, unresolved := []Document{}, []Unresolved{}
	for _, l := range CandidatesAt(st, nil, "", at) {
		if repo == "" {
			unresolved = append(unresolved, Unresolved{Lesson: l.Lesson, Hypothesis: l.Hypothesis, Reason: "no repository to verify against"})
			continue
		}
		if err := Verify(repo, l); err != nil {
			unresolved = append(unresolved, Unresolved{Lesson: l.Lesson, Hypothesis: l.Hypothesis, Reason: err.Error()})
			continue
		}
		p, commit, _ := AnchorParts(l.Lesson)
		b, err := gitShow(repo, commit, p)
		if err != nil {
			unresolved = append(unresolved, Unresolved{Lesson: l.Lesson, Hypothesis: l.Hypothesis, Reason: "the file does not exist at the anchor: " + err.Error()})
			continue
		}
		body := lessonBody(string(b))
		docs = append(docs, Document{ID: l.Lesson, Kind: SearchKindLesson, Title: markdownTitle(body, p), Text: body})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs, unresolved
}

// lessonBody strips the frontmatter block: its keys are the fact's,
// already searchable through the id, and "hypothesis" or "expires" in
// every lesson would only flatten the ranking.
func lessonBody(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return text
	}
	after := rest[end+4:]
	if i := strings.Index(after, "\n"); i >= 0 {
		return after[i+1:]
	}
	return ""
}

// markdownTitle is the first heading's text, else the fallback.
func markdownTitle(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return fallback
}

// DeadEndDocuments is every standing dead end in the fold, by contract
// then position: a retired one is evidence kept, not applicable, and
// stays out (the held-out listing's posture).
func DeadEndDocuments(st *State) []Document {
	docs := []Document{}
	contracts := make([]string, 0, len(st.DeadEnds))
	for c := range st.DeadEnds {
		contracts = append(contracts, c)
	}
	sort.Strings(contracts)
	for _, c := range contracts {
		for _, d := range st.DeadEnds[c] {
			if d.Retired {
				continue
			}
			text := strings.Join([]string{"tried: " + d.Tried, "outcome: " + d.Outcome, "condition: " + d.Condition, "environment: " + d.Environment}, "\n")
			if d.Pointer != "" {
				text += "\npointer: " + d.Pointer
			}
			docs = append(docs, Document{ID: fmt.Sprintf("%s@%d", c, d.Pos), Kind: SearchKindDeadEnd, Title: c + ": " + d.Tried, Text: text})
		}
	}
	return docs
}

// SectionDocuments splits a markdown file into one document per
// level-one or level-two heading, the text before the first heading
// standing as a section titled by the file. Ids are the path and the
// heading's slug, a repeated heading numbered from its second
// occurrence, so an id names one place a reader can go.
func SectionDocuments(name, body string) []Document {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var docs []Document
	seen := map[string]int{}
	title, buf := "", []string{}
	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = buf[:0]
		if title == "" && text == "" {
			return
		}
		id, t := name, title
		if title != "" {
			s := slug(title)
			seen[s]++
			if seen[s] > 1 {
				s = fmt.Sprintf("%s-%d", s, seen[s])
			}
			id = name + "#" + s
		} else {
			t = path.Base(name)
		}
		docs = append(docs, Document{ID: id, Kind: SearchKindDoc, Title: t, Text: text})
	}
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if !inFence && (strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ")) {
			flush()
			title = strings.TrimSpace(strings.TrimLeft(line, "#"))
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return docs
}

// slug is the heading lowercased with runs of non-alphanumerics
// collapsed to one hyphen.
func slug(s string) string {
	var out []rune
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
			dash = false
			continue
		}
		if !dash && len(out) > 0 {
			out = append(out, '-')
			dash = true
		}
	}
	return strings.TrimRight(string(out), "-")
}

// CleanRelative reports whether a doc path a reader names is relative
// and clean, so it names a file under the repository and cannot climb
// out of it (the lessons store's own rule, UnderLessonsDir).
func CleanRelative(p string) bool {
	return p != "" && path.Clean(p) == p && !strings.HasPrefix(p, "/") && p != "." && !strings.HasPrefix(p, "../") && p != ".."
}
