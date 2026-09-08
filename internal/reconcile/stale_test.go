package reconcile

// The stale lint (plans/os-0d537fbd.md D5): the latest admitted
// promotion of a path, unretired, expired at the declared instant for
// at least the threshold, files under the promotion's own position.

import (
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
)

func staleState() *curation.State {
	return &curation.State{
		Lessons: map[string]curation.LessonFact{
			"knowledge/lessons/a.md": {Pos: 10, Lesson: "knowledge/lessons/a.md @ 0123456", Hypothesis: "h-aaaaaaaaaaaa@4", Expires: "2026-12-01T00:00:00Z"},
			"knowledge/lessons/b.md": {Pos: 12, Lesson: "knowledge/lessons/b.md @ 0123456", Hypothesis: "h-bbbbbbbbbbbb@5", Expires: "2027-06-01T00:00:00Z"},
			"knowledge/lessons/c.md": {Pos: 14, Lesson: "knowledge/lessons/c.md @ 0123456", Hypothesis: "h-cccccccccccc@6", Expires: "2026-12-01T00:00:00Z"},
		},
		Retired: map[string]curation.RetirementFact{
			"knowledge/lessons/c.md": {Pos: 20, Lesson: "knowledge/lessons/c.md @ 0123456", Reason: "expired"},
		},
	}
}

// conformance: AC5 — the finding names the promotion position in its
// subject, skips the unexpired and the retired, and honours the
// threshold at and past the boundary.
func TestLessonsStaleFilesTheUnretiredExpiredPromotion(t *testing.T) {
	st := staleState()
	expires := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	got := LessonsStale(st, expires.Add(48*time.Hour), 24*time.Hour)
	if len(got) != 1 || got[0].Class != ClassLessonStale || got[0].Subject != "knowledge/lessons/a.md@10" {
		t.Fatalf("one finding on the expired, unretired path, keyed by its promotion position: %+v", got)
	}
	if !strings.Contains(got[0].Detail, "2026-12-01T00:00:00Z") || !strings.Contains(got[0].Detail, "revalidat") || !strings.Contains(got[0].Detail, "retire") {
		t.Fatalf("the finding names the expiry and asks for revalidation or retirement: %s", got[0].Detail)
	}
	// Before the expiry, nothing; at the expiry with a zero threshold,
	// filed; within the threshold, not yet; at the threshold, filed.
	for _, row := range []struct {
		at    time.Time
		after time.Duration
		want  int
	}{
		{expires.Add(-time.Second), 0, 0},
		{expires, 0, 1},
		{expires.Add(23 * time.Hour), 24 * time.Hour, 0},
		{expires.Add(24 * time.Hour), 24 * time.Hour, 1},
	} {
		if got := LessonsStale(st, row.at, row.after); len(got) != row.want {
			t.Fatalf("at %s with stale-after %s: %d findings, want %d", row.at.Format(time.RFC3339), row.after, len(got), row.want)
		}
	}
	// A re-promotion of the path moves the position and the expiry:
	// the same instant files nothing, and a later one files under the
	// NEW position, which is new work by identity.
	st.Lessons["knowledge/lessons/a.md"] = curation.LessonFact{Pos: 30, Lesson: "knowledge/lessons/a.md @ 1234567", Hypothesis: "h-aaaaaaaaaaaa@4", Expires: "2027-03-01T00:00:00Z"}
	if got := LessonsStale(st, expires.Add(48*time.Hour), 0); len(got) != 0 {
		t.Fatalf("a re-promoted path is not stale before its new expiry: %+v", got)
	}
	if got := LessonsStale(st, time.Date(2027, 3, 2, 0, 0, 0, 0, time.UTC), 0); len(got) != 1 || got[0].Subject != "knowledge/lessons/a.md@30" {
		t.Fatalf("the later promotion expiring in its turn files under its own position: %+v", got)
	}
	// An expiry that does not parse is stale at any instant: a lesson
	// whose expiry cannot be read has none a reader can trust.
	st.Lessons["knowledge/lessons/a.md"] = curation.LessonFact{Pos: 40, Lesson: "knowledge/lessons/a.md @ 2345678", Expires: "never"}
	if got := LessonsStale(st, expires, 24*time.Hour); len(got) != 1 || got[0].Subject != "knowledge/lessons/a.md@40" {
		t.Fatalf("an unreadable expiry files: %+v", got)
	}
}
