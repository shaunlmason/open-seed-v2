package curation

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// conformance: spec/curation.md "Search": an advisory lexical
// read whose ranking is a function of the corpus and the query alone.

func TestTokenizeIsLowercaseAlphanumericRuns(t *testing.T) {
	got := Tokenize("Retry the Fetch: mirror-timeout (ci-runner/v0), a 2nd time é!")
	want := []string{"retry", "the", "fetch", "mirror", "timeout", "ci", "runner", "v0", "2nd", "time"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens: %v", got)
	}
	if got := QueryTerms("fetch Fetch the fetch"); !reflect.DeepEqual(got, []string{"fetch", "the"}) {
		t.Fatalf("query terms are distinct, first-seen order: %v", got)
	}
}

func TestSearchRanksByBM25(t *testing.T) {
	docs := []Document{
		{ID: "a", Kind: SearchKindDoc, Title: "mirror", Text: "the mirror was cold so the fetch timed out"},
		{ID: "b", Kind: SearchKindDoc, Title: "lockfile", Text: "a lockfile can never record the sha of the commit that contains it"},
		{ID: "c", Kind: SearchKindDoc, Title: "fetch", Text: "git fetch with no destination refspec stores no local ref; resolve through FETCH_HEAD after the fetch"},
		{ID: "d", Kind: SearchKindDoc, Title: "gate", Text: "a gate is only trustworthy if it runs before the action it guards"},
	}
	ix := NewIndex(docs)
	if ix.Len() != 4 {
		t.Fatalf("four documents: %d", ix.Len())
	}
	hits := ix.Search("fetch", 0)
	if len(hits) != 2 || hits[0].ID != "c" || hits[1].ID != "a" {
		t.Fatalf("the document saying fetch three times outranks the one saying it once, and no other is a hit: %+v", hits)
	}
	if hits[0].Rank != 1 || hits[1].Rank != 2 || hits[0].Score <= hits[1].Score {
		t.Fatalf("ranks are positional and scores descend: %+v", hits)
	}
	if !reflect.DeepEqual(hits[0].Matched, []string{"fetch"}) || hits[0].Snippet == "" || !strings.Contains(strings.ToLower(hits[0].Snippet), "fetch") {
		t.Fatalf("a hit names what matched and shows the line it matched on: %+v", hits[0])
	}
	// A rare term outweighs a common one: "the" is in every document
	// but one; "lockfile" in one.
	hits = ix.Search("the lockfile", 0)
	if len(hits) != 4 || hits[0].ID != "b" || !reflect.DeepEqual(hits[0].Matched, []string{"the", "lockfile"}) {
		t.Fatalf("idf ranks the rare term's document first: %+v", hits)
	}
	if got := ix.Search("the lockfile", 1); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("limit bounds the hits: %+v", got)
	}
	if got := ix.Search("", 0); got == nil || len(got) != 0 {
		t.Fatalf("an empty query is no hit: %+v", got)
	}
	if got := ix.Search("zzzz", 0); len(got) != 0 {
		t.Fatalf("a term nobody says is no hit: %+v", got)
	}
	if got := NewIndex(nil).Search("fetch", 0); len(got) != 0 {
		t.Fatalf("an empty corpus is no hit: %+v", got)
	}
	if got := NewIndex([]Document{{ID: "blank", Kind: SearchKindDoc}}).Search("fetch", 0); len(got) != 0 {
		t.Fatalf("a corpus of empty documents is no hit, and divides by nothing: %+v", got)
	}
	// Title-only matches snippet the first line; ties break on kind
	// then id so the ranking is deterministic.
	tied := NewIndex([]Document{
		{ID: "z", Kind: SearchKindLesson, Title: "same words here", Text: "one line"},
		{ID: "y", Kind: SearchKindDoc, Title: "same words here", Text: "one line"},
		{ID: "x", Kind: SearchKindDoc, Title: "same words here", Text: "one line"},
	})
	hits = tied.Search("same words", 0)
	if len(hits) != 3 || hits[0].ID != "x" || hits[1].ID != "y" || hits[2].ID != "z" || hits[0].Snippet != "one line" {
		t.Fatalf("ties break on kind then id, and a title-only match shows the first line: %+v", hits)
	}
	// Length normalization: the same single mention in a longer
	// document scores lower.
	long := NewIndex([]Document{
		{ID: "short", Kind: SearchKindDoc, Text: "mirror cold"},
		{ID: "long", Kind: SearchKindDoc, Text: "mirror " + strings.Repeat("word ", 40)},
	})
	hits = long.Search("mirror", 0)
	if len(hits) != 2 || hits[0].ID != "short" {
		t.Fatalf("the shorter document with the same mention ranks first: %+v", hits)
	}
	if got := snippet("# Only a heading\n\n## And another\n", []string{"only"}); got != "Only a heading" {
		t.Fatalf("a document that is all heading snippets its first heading: %q", got)
	}
	if s := truncate(strings.Repeat("é", SnippetRunes+5)); len([]rune(s)) != SnippetRunes+1 || !strings.HasSuffix(s, "…") {
		t.Fatalf("the snippet is bounded in runes: %d", len([]rune(s)))
	}
}

func TestSectionDocumentsSplitOnHeadings(t *testing.T) {
	body := "preamble line\n\n# Title one\n\ntext one\n```\n## not a heading\n```\n## Sub two\n\ntext two\n## Sub two\n\ntext three\n### deeper stays inside\n"
	docs := SectionDocuments("docs/decisions.md", body)
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	want := []string{"docs/decisions.md", "docs/decisions.md#title-one", "docs/decisions.md#sub-two", "docs/decisions.md#sub-two-2"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("one document per level-one or level-two heading, the preamble by the file, a repeated heading numbered: %v", ids)
	}
	if docs[0].Title != "decisions.md" || docs[0].Text != "preamble line" || docs[0].Kind != SearchKindDoc {
		t.Fatalf("the preamble is titled by the file: %+v", docs[0])
	}
	if !strings.Contains(docs[1].Text, "## not a heading") || !strings.Contains(docs[3].Text, "### deeper stays inside") {
		t.Fatalf("a fenced heading and a deeper one stay in their section: %+v %+v", docs[1], docs[3])
	}
	if got := SectionDocuments("empty.md", "\n\n"); len(got) != 0 {
		t.Fatalf("an empty file is no document: %+v", got)
	}
	if got := slug("  A: b's -- C  "); got != "a-b-s-c" {
		t.Fatalf("slug: %q", got)
	}
	for p, ok := range map[string]bool{"docs/decisions.md": true, "a.md": true, "": false, "/etc/x": false, "../x": false, "a/../../x": false, ".": false, "..": false, "a//b": false, "./a": false} {
		if CleanRelative(p) != ok {
			t.Errorf("CleanRelative(%q) = %v", p, !ok)
		}
	}
}

func TestDeadEndDocumentsAreTheStandingOnes(t *testing.T) {
	retiredAt := 9
	st := &State{DeadEnds: map[string][]DeadEndFact{
		"c-2": {{Pos: 5, Tried: "retrying the fetch", Outcome: "the mirror timed out", Condition: "the mirror was cold", Environment: "ci-runner/v0", Pointer: "logs/x @ 0123456"}},
		"c-1": {
			{Pos: 1, Tried: "waiting", Outcome: "nothing", Condition: "cold", Environment: "ci-runner/v0"},
			{Pos: 3, Tried: "warming", Outcome: "gone", Condition: "cold", Environment: "ci-runner/v0", Retired: true, RetiredEnvironment: "ci-runner/v1", RetiredAt: &retiredAt},
		},
	}}
	docs := DeadEndDocuments(st)
	if len(docs) != 2 || docs[0].ID != "c-1@1" || docs[1].ID != "c-2@5" || docs[0].Kind != SearchKindDeadEnd {
		t.Fatalf("standing dead ends by contract then position, the retired one out: %+v", docs)
	}
	if docs[1].Title != "c-2: retrying the fetch" || !strings.Contains(docs[1].Text, "pointer: logs/x @ 0123456") || !strings.Contains(docs[1].Text, "environment: ci-runner/v0") {
		t.Fatalf("a dead end's document carries its fields: %+v", docs[1])
	}
	hits := NewIndex(docs).Search("mirror timed out", 0)
	if len(hits) != 1 || hits[0].ID != "c-2@5" || hits[0].Snippet != "outcome: the mirror timed out" {
		t.Fatalf("the dead end is found by its outcome: %+v", hits)
	}
	if got := DeadEndDocuments(&State{}); len(got) != 0 {
		t.Fatalf("an empty fold is no document: %+v", got)
	}
}

func TestLessonDocumentsAreTheVerifiedSurfacingSet(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, LessonsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, LessonsDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "--quiet", "-m", name)
		return git("rev-parse", "HEAD")
	}
	head := "---\nhypothesis: h@4\napplies-when: {\"routing\": \"core\"}\nsupport: c-1@4, c-2@9\nprovenance: plans/x.md @ 0123456\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: knowledge\n---\n"
	mirror := head + "\n# Record the mirror's temperature\n\n## Claim\n\nrecord the mirror's temperature before retrying the fetch\n"
	mirrorAnchor := write("mirror.md", mirror)
	lock := head + "\n# Lockfiles\n\n## Claim\n\na lockfile cannot record its own commit\n"
	lockAnchor := write("lock.md", lock)
	stale := head + "\n# Stale\n\n## Claim\n\nthe mirror lesson that expired\n"
	staleAnchor := write("stale.md", stale)
	gone := head + "\n# Retired\n\n## Claim\n\nthe mirror lesson that was retired\n"
	goneAnchor := write("gone.md", gone)
	// The working tree moves on; the index reads the anchor's bytes.
	if err := os.WriteFile(filepath.Join(repo, LessonsDir, "mirror.md"), []byte(mirror+"\nedited: refspec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "--quiet", "-am", "edit")

	ids := map[string]string{"mirror": HypothesisID("mirror", nil), "lock": HypothesisID("lock", nil), "stale": HypothesisID("stale", nil), "gone": HypothesisID("gone", nil), "contested": HypothesisID("contested", nil), "unmerged": HypothesisID("unmerged", nil)}
	st := &State{Hypotheses: map[string]*HypothesisFact{}, Lessons: map[string]LessonFact{}, Retired: map[string]RetirementFact{}, Contests: map[string][]ContestFact{}}
	add := func(name, file, anchor, body, stage, expires string) {
		st.Hypotheses[ids[name]] = &HypothesisFact{ID: ids[name], Stage: stage, AppliesWhen: AppliesWhen{Routing: "core"}}
		st.order = append(st.order, ids[name])
		p := LessonsDir + "/" + file
		st.Lessons[p] = LessonFact{Lesson: p + " @ " + anchor, Hypothesis: ids[name] + "@4", Carrier: "knowledge", Digest: Digest([]byte(body)), LastValidated: "2026-09-01T00:00:00Z", Expires: expires}
	}
	add("mirror", "mirror.md", mirrorAnchor, mirror, StagePromoted, "2026-12-01T00:00:00Z")
	add("lock", "lock.md", lockAnchor, lock, StagePromoted, "2026-12-01T00:00:00Z")
	add("stale", "stale.md", staleAnchor, stale, StagePromoted, "2026-09-15T00:00:00Z")
	add("gone", "gone.md", goneAnchor, gone, StagePromoted, "2026-12-01T00:00:00Z")
	st.Retired[LessonsDir+"/gone.md"] = RetirementFact{Lesson: LessonsDir + "/gone.md @ " + goneAnchor, Reason: "regression"}
	add("contested", "contested.md", mirrorAnchor, mirror, StageContested, "2026-12-01T00:00:00Z")
	add("unmerged", "unmerged.md", strings.Repeat("a", 40), mirror, StagePromoted, "2026-12-01T00:00:00Z")

	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	docs, unresolved := LessonDocuments(st, repo, at)
	if len(docs) != 2 || docs[0].ID != LessonsDir+"/lock.md @ "+lockAnchor || docs[1].ID != LessonsDir+"/mirror.md @ "+mirrorAnchor {
		t.Fatalf("the surfacing set at the instant, verified: promoted, uncontested, unretired, unexpired, resolving: %+v", docs)
	}
	if docs[1].Title != "Record the mirror's temperature" || docs[1].Kind != SearchKindLesson || strings.Contains(docs[1].Text, "hypothesis:") || strings.Contains(docs[1].Text, "refspec") {
		t.Fatalf("the document is the reviewed body at the anchor, its frontmatter stripped, its title the heading: %+v", docs[1])
	}
	if len(unresolved) != 1 || unresolved[0].Lesson != LessonsDir+"/unmerged.md @ "+strings.Repeat("a", 40) || !strings.Contains(unresolved[0].Reason, "not an ancestor") {
		t.Fatalf("a fact that does not resolve is reported, never indexed: %+v", unresolved)
	}
	hits := NewIndex(docs).Search("mirror temperature", 0)
	if len(hits) != 1 || hits[0].ID != docs[1].ID || hits[0].Snippet != "record the mirror's temperature before retrying the fetch" {
		t.Fatalf("the lesson is found by its claim: %+v", hits)
	}
	docs, unresolved = LessonDocuments(st, "", at)
	if len(docs) != 0 || len(unresolved) != 3 || unresolved[0].Reason != "no repository to verify against" {
		t.Fatalf("with no repository nothing is indexed and every candidate is unresolved: %+v %+v", docs, unresolved)
	}
	if b := lessonBody("no frontmatter\n"); b != "no frontmatter\n" {
		t.Fatalf("a body without frontmatter is itself: %q", b)
	}
	if b := lessonBody("---\nopen: block"); b != "---\nopen: block" {
		t.Fatalf("an unclosed block is left alone: %q", b)
	}
	if b := lessonBody("---\nk: v\n---"); b != "" {
		t.Fatalf("a closed block with nothing after is empty: %q", b)
	}
	if markdownTitle("no heading\n", "fallback") != "fallback" {
		t.Fatal("the title falls back to the path")
	}
}
