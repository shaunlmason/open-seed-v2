package main

// The attempts journal's act digest at the terminal (plans/os-a9e715dc.md
// D1, D2, D3; III.R row 5's blind-retry clause): every seam writes the
// digest of the act it journals, the same act re-sent unchanged digests
// alike across instants and tips, a corrected act digests apart, and a
// report built over the journal counts the unchanged re-send as the
// one blind retry.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
)

// conformance: plans/os-a9e715dc.md AC1, AC2 at the terminal — a
// service key files a request with a kind the boundary refuses, files
// the same request again unchanged (a blind retry, refused again),
// then files it corrected (admitted); the journal beside the ledger
// carries a digest on every line, the two refusals' digests are equal
// and the admission's differs, the raw seam's line carries one too,
// and `project rebuild --refusals` reports one blind retry under the
// refusal's code and no undigested line.
func TestJournalDigestsTheActAndTheReportCountsBlindRetries(t *testing.T) {
	ld, root, _, service, _ := requestLedger(t)
	file := func(kind string) ledgerEnv {
		t.Helper()
		e, _ := runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "system",
			"--origin", "dash", "--kind", kind, "--reference", "cards/c-1.md @ 0123456", "--summary", "rename the card")
		return e
	}
	if e := file("wish"); e.OK || e.Error == nil || e.Error.Code != "request_refused" {
		t.Fatalf("an unknown kind refuses at the boundary: %+v", e)
	}
	if e := file("wish"); e.OK || e.Error == nil || e.Error.Code != "request_refused" {
		t.Fatalf("the blind retry refuses again: %+v", e)
	}
	if e := file("mirror-edit"); !e.OK {
		t.Fatalf("the corrected request lands: %+v", e)
	}
	// The raw seam journals its success with a digest too.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", root, "--verb", "message.sent", "--subject", "system", "--payload", `{"n": 1}`); code != 0 {
		t.Fatalf("raw append: %d %+v", code, e)
	}
	j, err := refusals.Load(filepath.Join(ld, refusals.File))
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's own appends journaled before these four lines;
	// the last four are the drill's.
	if len(j.Entries) < 4 {
		t.Fatalf("the drill journals four attempts: %+v", j.Entries)
	}
	last := j.Entries[len(j.Entries)-4:]
	for i, e := range j.Entries {
		if e.Digest == "" {
			t.Fatalf("line %d carries no digest: %+v", i+1, e)
		}
	}
	if last[0].Outcome != refusals.OutcomeRefused || last[1].Outcome != refusals.OutcomeRefused || last[2].Outcome != refusals.OutcomeAdmitted || last[3].Verb != "message.sent" {
		t.Fatalf("refused, refused, admitted, then the raw seam: %+v", last)
	}
	if last[0].Digest != last[1].Digest {
		t.Fatalf("the same act re-sent digests alike across instants and tips: %s vs %s", last[0].Digest, last[1].Digest)
	}
	if last[0].Code != last[1].Code || last[0].Position != last[1].Position {
		t.Fatalf("the blind retry is refused alike from the same position: %+v %+v", last[0], last[1])
	}
	if last[2].Digest == last[0].Digest {
		t.Fatal("the corrected act digests apart")
	}
	if last[0].TS == "" || last[0].Position == "" {
		t.Fatalf("the digest joins the line, it replaces nothing: %+v", last[0])
	}
	// The report over the journal: one blind retry, under the
	// refusal's code, no undigested line.
	out := filepath.Join(t.TempDir(), "views")
	// The projection tree is locked read-only on publish; unlock it so
	// the temp dir's cleanup can remove it (the project drills' posture).
	t.Cleanup(func() {
		_ = filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out, "--refusals", filepath.Join(ld, refusals.File)); code != 0 {
		t.Fatalf("rebuild: %d %+v", code, e)
	}
	cur, err := os.ReadFile(filepath.Join(out, "report", "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "report", "builds", strings.TrimSpace(string(cur)), project.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Refusals == nil {
		t.Fatal("a declared journal produces the refusals section")
	}
	if rep.Refusals.BlindRetries != 1 || rep.Refusals.BlindRetriesByCode["request_refused"] != 1 || rep.Refusals.Undigested != 0 {
		t.Fatalf("one blind retry under the refusal's code, nothing undigested: %+v", rep.Refusals)
	}
	if rep.Refusals.Refused != 2 {
		t.Fatalf("two refusals in the journal: %+v", rep.Refusals)
	}
}
