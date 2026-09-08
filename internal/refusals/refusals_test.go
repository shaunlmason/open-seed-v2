package refusals

// The journal drills (plans/os-edf73d66.md D4a): Note appends
// well-formed lines and swallows every failure, Load refuses
// malformed declared input line by line, and the digest is keyed by
// content.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func entry(outcome, code string) Entry {
	return Entry{
		TS: "2026-09-01T02:30:00Z", Position: "7", Actor: "aa11",
		Verb: "message.sent", Subject: "c-1", Outcome: outcome, Code: code,
	}
}

func TestNoteAppendsAndLoadReads(t *testing.T) {
	dir := t.TempDir()
	Note(dir, entry(OutcomeAdmitted, ""))
	Note(dir, entry(OutcomeRefused, "chain_invalid"))
	j, err := Load(filepath.Join(dir, File))
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Entries) != 2 || j.Entries[0].Outcome != OutcomeAdmitted || j.Entries[1].Code != "chain_invalid" {
		t.Fatalf("the journal replays what was noted: %+v", j.Entries)
	}
}

// conformance: journaling must never fail or slow the verb it rides:
// an unwritable target journals nothing and raises nothing.
func TestNoteSwallowsFailures(t *testing.T) {
	Note("", entry(OutcomeAdmitted, ""))
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The "directory" is a regular file, so the open fails; Note
	// returns anyway.
	Note(filepath.Join(file, "sub"), entry(OutcomeAdmitted, ""))
}

// conformance: inputs are declared, so garbage is the declarer's
// error, never silently skipped telemetry; refusals name the line.
func TestLoadRefusesMalformedLines(t *testing.T) {
	for name, line := range map[string]string{
		"not json":         `{`,
		"unknown field":    `{"ts": "t", "position": "1", "actor": "a", "verb": "v", "subject": "s", "outcome": "admitted", "extra": 1}`,
		"trailing data":    `{"ts": "t", "position": "1", "actor": "a", "verb": "v", "subject": "s", "outcome": "admitted"} {}`,
		"unknown outcome":  `{"ts": "t", "position": "1", "actor": "a", "verb": "v", "subject": "s", "outcome": "maybe"}`,
		"refused no code":  `{"ts": "t", "position": "1", "actor": "a", "verb": "v", "subject": "s", "outcome": "refused"}`,
		"admitted w/ code": `{"ts": "t", "position": "1", "actor": "a", "verb": "v", "subject": "s", "outcome": "admitted", "code": "fenced"}`,
		"bad position":     `{"ts": "t", "position": "tip", "actor": "a", "verb": "v", "subject": "s", "outcome": "admitted"}`,
		"missing verb":     `{"ts": "t", "position": "1", "actor": "a", "verb": "", "subject": "s", "outcome": "admitted"}`,
	} {
		path := filepath.Join(t.TempDir(), File)
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "line 1") {
			t.Fatalf("%s must refuse naming the line, got %v", name, err)
		}
	}
}

// conformance: the commit-marker rule (review finding on the task
// PR) — a final unterminated fragment is an uncommitted attempt
// (torn short write, crash mid-append) and never poisons the
// journal; a terminated malformed line still refuses.
func TestLoadSkipsTornTail(t *testing.T) {
	dir := t.TempDir()
	Note(dir, entry(OutcomeAdmitted, ""))
	path := filepath.Join(dir, File)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(`{"ts": "torn`)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	j, err := Load(path)
	if err != nil || len(j.Entries) != 1 {
		t.Fatalf("the torn tail is uncommitted, the committed line survives: %v %+v", err, j)
	}
	// The same bytes followed by a newline are a committed line and
	// refuse as declarer garbage.
	if err := os.WriteFile(path, []byte(`{"ts": "torn`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("a terminated malformed line still refuses: %v", err)
	}
}

func TestDigestKeyedByContent(t *testing.T) {
	a := &Journal{Entries: []Entry{entry(OutcomeAdmitted, "")}}
	b := &Journal{Entries: []Entry{entry(OutcomeRefused, "fenced")}}
	da, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	db, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if da == db {
		t.Fatal("different journals must digest differently")
	}
	da2, err := (&Journal{Entries: []Entry{entry(OutcomeAdmitted, "")}}).Digest()
	if err != nil {
		t.Fatal(err)
	}
	if da != da2 {
		t.Fatal("equal journals must digest equally")
	}
}

// conformance: plans/os-a9e715dc.md D1, AC1 — the digest identifies
// the act: two attempts of one act by one key digest alike whatever
// the instant or the tip (neither is an input), and a changed
// payload, verb, subject or actor digests differently; a non-object
// payload still digests, apart from any object's.
func TestAttemptDigestIdentifiesTheAct(t *testing.T) {
	a := AttemptDigest("aa11", "claim.taken", "c-1", []byte(`{"fence": "3"}`))
	if len(a) != 64 || !digestShape(a) {
		t.Fatalf("a sha256 in lowercase hex: %q", a)
	}
	if b := AttemptDigest("aa11", "claim.taken", "c-1", []byte(` {"fence":"3"} `)); b != a {
		t.Fatalf("the canonical form ignores whitespace: %s vs %s", a, b)
	}
	for name, other := range map[string]string{
		"payload": AttemptDigest("aa11", "claim.taken", "c-1", []byte(`{"fence": "4"}`)),
		"verb":    AttemptDigest("aa11", "claim.released", "c-1", []byte(`{"fence": "3"}`)),
		"subject": AttemptDigest("aa11", "claim.taken", "c-2", []byte(`{"fence": "3"}`)),
		"actor":   AttemptDigest("bb22", "claim.taken", "c-1", []byte(`{"fence": "3"}`)),
	} {
		if other == a {
			t.Errorf("a changed %s must digest differently", name)
		}
	}
	bad := AttemptDigest("aa11", "claim.taken", "c-1", []byte(`[1]`))
	if bad == "" || bad == a || bad != AttemptDigest("aa11", "claim.taken", "c-1", []byte(`[1]`)) {
		t.Fatalf("a non-object payload digests as itself, stably: %q", bad)
	}
	if AttemptDigest("aa11", "claim.taken", "c-1", nil) != AttemptDigest("aa11", "claim.taken", "c-1", []byte(`{}`)) {
		t.Fatal("an empty payload digests as an empty object")
	}
}

// conformance: plans/os-a9e715dc.md D1 — a journal written before the
// field existed still loads; a present digest that is no sha256
// refuses naming the line; a noted digest round-trips.
func TestLoadHoldsTheDigestShape(t *testing.T) {
	dir := t.TempDir()
	with := entry(OutcomeRefused, "fenced_out")
	with.Digest = AttemptDigest(with.Actor, with.Verb, with.Subject, []byte(`{}`))
	Note(dir, entry(OutcomeAdmitted, ""))
	Note(dir, with)
	j, err := Load(filepath.Join(dir, File))
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Entries) != 2 || j.Entries[0].Digest != "" || j.Entries[1].Digest != with.Digest {
		t.Fatalf("lines with and without a digest load: %+v", j.Entries)
	}
	path := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(path, []byte(`{"ts": "2026-09-01T02:30:00Z", "position": "7", "actor": "aa11", "verb": "message.sent", "subject": "c-1", "outcome": "admitted", "digest": "not-a-digest"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("a malformed digest refuses naming the line: %v", err)
	}
}
