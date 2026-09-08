package project_test

// The refusal-rate report drills (plans/os-edf73d66.md D4c-d;
// charter III.I row 4): the section derives from the declared
// attempts journal alone — the plan review's worked example, one
// refusal beside a hundred admissions, must read 0.0099 and never
// the chain-denominator 0.5000 — builds are byte-identical for
// identical inputs, an input-free build states refusals: null, and
// the declared-inputs digest covers the journal, alone or beside
// the observation family.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
)

func attemptsFixture(t *testing.T) *refusals.Journal {
	t.Helper()
	dir := t.TempDir()
	refusals.Note(dir, refusals.Entry{
		TS: "2026-09-01T02:00:00Z", Position: "10", Actor: "aa11",
		Verb: "claim.taken", Subject: "c-1", Outcome: refusals.OutcomeRefused, Code: "fenced",
	})
	for i := 0; i < 100; i++ {
		refusals.Note(dir, refusals.Entry{
			TS: "2026-09-01T02:01:00Z", Position: "11", Actor: "aa11",
			Verb: "message.sent", Subject: "c-1", Outcome: refusals.OutcomeAdmitted,
		})
	}
	j, err := refusals.Load(filepath.Join(dir, refusals.File))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestReportRefusalsSection(t *testing.T) {
	root := pKey(t, 1)
	dir, resolve, _ := fixtureChain(t, root)
	journal := attemptsFixture(t)
	in := project.Inputs{Refusals: journal}

	out := lockedTempOut(t, "views")
	if _, err := project.RebuildWith(dir, out, project.Default(), resolve, in); err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	if rep.Refusals == nil {
		t.Fatal("a declared journal must produce the refusals section")
	}
	if rep.Observation != nil {
		t.Fatalf("no observation inputs were declared: %+v", rep.Observation)
	}
	s := rep.Refusals
	if s.Refused != 1 || s.Admitted != 100 || s.Inputs.Entries != 101 {
		t.Fatalf("counts come from the journal's outcomes: %+v", s)
	}
	// The plan review's worked example: the rate is one population,
	// never refusals over a chain span (which would read 0.5000).
	if s.Rate != "0.0099" {
		t.Fatalf("1 refusal beside 100 admissions reads 0.0099, got %q", s.Rate)
	}
	if s.Span == nil || s.Span.From != 10 || s.Span.To != 11 {
		t.Fatalf("the span is the journal's stamped positions: %+v", s.Span)
	}
	if s.ByCode["fenced"] != 1 || s.ByVerb["claim.taken"] != 1 || len(s.ByCode) != 1 || len(s.ByVerb) != 1 {
		t.Fatalf("breakdowns count refusals only: %+v %+v", s.ByCode, s.ByVerb)
	}
	jd, err := journal.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if s.Inputs.Digest != jd {
		t.Fatalf("the section echoes the journal's own digest: %s vs %s", s.Inputs.Digest, jd)
	}

	// The stamp and build id carry the declared-inputs digest, the
	// refusals family declaring alone.
	full, err := in.Digest()
	if err != nil {
		t.Fatal(err)
	}
	var stamp project.Stamp
	readView(t, out, "report", project.StampFile, &stamp)
	if stamp.Inputs != full {
		t.Fatalf("the stamp carries the inputs digest: %+v", stamp)
	}
	cur, err := os.ReadFile(filepath.Join(out, "report", "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(cur)), "-i"+full[:12]) {
		t.Fatalf("the input-bearing id ends in the digest segment: %s", cur)
	}

	// Byte-identical for identical inputs.
	out2 := lockedTempOut(t, "views")
	if _, err := project.RebuildWith(dir, out2, project.Default(), resolve, in); err != nil {
		t.Fatal(err)
	}
	one := readRaw(t, out, "report", project.ReportFile)
	two := readRaw(t, out2, "report", project.ReportFile)
	if !bytes.Equal(one, two) {
		t.Fatal("identical inputs must build identical report bytes")
	}

	// Input-free: the section is null, stated not fabricated, and
	// the version bump alone republishes existing prefixes.
	out3 := lockedTempOut(t, "views")
	if _, err := project.Rebuild(dir, out3, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	readView(t, out3, "report", project.ReportFile, &rep)
	if rep.Refusals != nil {
		t.Fatalf("an input-free report must state refusals: null, got %+v", rep.Refusals)
	}
	// The refusals section arrived at version 10; the knowledge section
	// moved the report to 11, its retired and stale counts to 12, and
	// the lanes section to 13, each republishing every prefix in its
	// turn.
	if v := project.Report().Version; v != "19" {
		t.Fatalf("the report's version is 18 (10 added the refusals section, 11 the knowledge section, 12 its retired and stale counts, 13 the lanes section, 14 the flywheel section, 15 the lanes section's by_kind split, 16 the planner's strongest, 17 the adapters section, 18 the refusals section's blind-retry counts), got %s", v)
	}
}

// conformance: the declared-inputs digest spans EVERY declared
// input, families contributing only when declared, so an obs-only
// digest is unchanged and a journal rekeys any build it joins.
func TestInputsDigestCoversRefusals(t *testing.T) {
	journal := attemptsFixture(t)
	other := &refusals.Journal{Entries: journal.Entries[:1]}
	obsIn := project.Inputs{Obs: &obs.Snapshot{}, AsOf: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC), Thresholds: obs.DefaultThresholds()}
	both := obsIn
	both.Refusals = journal
	dObs, err := obsIn.Digest()
	if err != nil {
		t.Fatal(err)
	}
	dBoth, err := both.Digest()
	if err != nil {
		t.Fatal(err)
	}
	dJournal, err := project.Inputs{Refusals: journal}.Digest()
	if err != nil {
		t.Fatal(err)
	}
	dOther, err := project.Inputs{Refusals: other}.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if dObs == dBoth || dJournal == dOther || dJournal == dBoth || dObs == dJournal {
		t.Fatalf("every declared combination digests distinctly: obs %s both %s journal %s other %s", dObs, dBoth, dJournal, dOther)
	}
}

func readRaw(t *testing.T, out, name, file string) []byte {
	t.Helper()
	cur, err := os.ReadFile(filepath.Join(out, name, "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, name, "builds", strings.TrimSpace(string(cur)), file))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// blindFixture is a journal with digests: a refusal re-sent unchanged
// from the same position (a blind retry, refused alike), then
// corrected (admitted); a contention spin re-sent unchanged from the
// same position (blind, under its own code); a refusal of another act
// followed by a different act on the same subject (no retry); a
// contention refusal whose unchanged re-send is admitted after the
// tip moved (convergence, the first arm, never blind); a same act
// refused again with another code (a new answer); a same act refused
// alike from an advanced position (a new state); a line from before
// the digest existed; and another actor's same-digest attempt, which
// is nobody's retry.
func blindFixture(t *testing.T) *refusals.Journal {
	t.Helper()
	dir := t.TempDir()
	note := func(actor, verb, subject, outcome, code, position string, payload string) {
		e := refusals.Entry{TS: "2026-09-01T02:00:00Z", Position: position, Actor: actor, Verb: verb, Subject: subject, Outcome: outcome, Code: code}
		if payload != "" {
			e.Digest = refusals.AttemptDigest(actor, verb, subject, []byte(payload))
		}
		refusals.Note(dir, e)
	}
	note("aa11", "request.filed", "system", refusals.OutcomeRefused, "request_refused", "10", `{"kind": "wish"}`)
	note("aa11", "request.filed", "system", refusals.OutcomeRefused, "request_refused", "10", `{"kind": "wish"}`)
	note("aa11", "request.filed", "system", refusals.OutcomeAdmitted, "", "11", `{"kind": "mirror-edit"}`)
	note("ee55", "claim.taken", "c-5", refusals.OutcomeRefused, "contention", "12", `{}`)
	note("ee55", "claim.taken", "c-5", refusals.OutcomeRefused, "contention", "12", `{}`)
	note("aa11", "claim.taken", "c-1", refusals.OutcomeRefused, "fenced_out", "12", `{"fence": "3"}`)
	note("aa11", "claim.released", "c-1", refusals.OutcomeAdmitted, "", "13", `{"fence": "4"}`)
	note("bb22", "claim.taken", "c-2", refusals.OutcomeRefused, "contention", "13", `{}`)
	note("bb22", "claim.taken", "c-2", refusals.OutcomeAdmitted, "", "15", `{}`)
	note("ff66", "claim.taken", "c-6", refusals.OutcomeRefused, "contention", "15", `{}`)
	note("ff66", "claim.taken", "c-6", refusals.OutcomeRefused, "fenced_out", "15", `{}`)
	note("aa11", "claim.taken", "c-7", refusals.OutcomeRefused, "fenced_out", "15", `{}`)
	note("aa11", "claim.taken", "c-7", refusals.OutcomeRefused, "fenced_out", "16", `{}`)
	note("cc33", "message.sent", "c-3", refusals.OutcomeRefused, "fenced_out", "16", "")
	note("cc33", "message.sent", "c-3", refusals.OutcomeAdmitted, "", "16", "")
	note("dd44", "claim.taken", "c-2", refusals.OutcomeAdmitted, "", "17", `{}`)
	j, err := refusals.Load(filepath.Join(dir, refusals.File))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// conformance: III.R row 5's blind-retry clause (plans/os-a9e715dc.md
// D3, AC2), by modes.md's definition — a refusal followed by the same
// actor's same-digest attempt on the same subject, refused with the
// same code from the same position, counts as one blind retry under
// that code; a corrected retry, an admitted next attempt
// (convergence), a refusal with another code, a refusal from an
// advanced position and a different act on the subject count none;
// another actor's same act is nobody's retry; undigested lines are
// counted as such and judged not at all.
func TestReportCountsBlindRetries(t *testing.T) {
	root := pKey(t, 1)
	dir, resolve, _ := fixtureChain(t, root)
	out := lockedTempOut(t, "views")
	if _, err := project.RebuildWith(dir, out, project.Default(), resolve, project.Inputs{Refusals: blindFixture(t)}); err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	s := rep.Refusals
	if s == nil {
		t.Fatal("a declared journal must produce the refusals section")
	}
	if s.Refused != 11 || s.Admitted != 5 {
		t.Fatalf("the counts are the journal's: %+v", s)
	}
	if s.BlindRetries != 2 {
		t.Fatalf("the unchanged request and the contention spin are the two blind retries: %+v", s)
	}
	if s.BlindRetriesByCode["request_refused"] != 1 || s.BlindRetriesByCode["contention"] != 1 || len(s.BlindRetriesByCode) != 2 {
		t.Fatalf("split by the refusal's code: %+v", s.BlindRetriesByCode)
	}
	if s.Undigested != 2 {
		t.Fatalf("two lines carry no digest: %+v", s)
	}
}

// conformance: plans/os-a9e715dc.md D3 at scale (review finding on
// the task PR): the pairing reads the journal once from the tail, so
// a journal of many one-off refusals, each a distinct actor and
// subject that never attempts again, still counts exactly the blind
// retries it holds: the one unchanged re-send buried among them.
func TestReportPairsBlindRetriesInOnePass(t *testing.T) {
	dir := t.TempDir()
	note := func(actor, subject, outcome, code, position, payload string) {
		e := refusals.Entry{TS: "2026-09-01T02:00:00Z", Position: position, Actor: actor, Verb: "claim.taken", Subject: subject, Outcome: outcome, Code: code}
		if payload != "" {
			e.Digest = refusals.AttemptDigest(actor, "claim.taken", subject, []byte(payload))
		}
		refusals.Note(dir, e)
	}
	const oneOffs = 20000
	for i := 0; i < oneOffs; i++ {
		note(fmt.Sprintf("k%05d", i), fmt.Sprintf("c-%d", i), refusals.OutcomeRefused, "fenced_out", "20", `{}`)
		if i == oneOffs/2 {
			note("aa11", "c-blind", refusals.OutcomeRefused, "contention", "20", `{}`)
		}
	}
	note("aa11", "c-blind", refusals.OutcomeRefused, "contention", "20", `{}`)
	note("aa11", "c-blind", refusals.OutcomeAdmitted, "", "21", `{}`)
	j, err := refusals.Load(filepath.Join(dir, refusals.File))
	if err != nil {
		t.Fatal(err)
	}
	root := pKey(t, 1)
	chain, resolve, _ := fixtureChain(t, root)
	out := lockedTempOut(t, "views")
	if _, err := project.RebuildWith(chain, out, project.Default(), resolve, project.Inputs{Refusals: j}); err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	s := rep.Refusals
	if s == nil || s.Refused != oneOffs+2 || s.Admitted != 1 {
		t.Fatalf("the counts are the journal's: %+v", s)
	}
	if s.BlindRetries != 1 || s.BlindRetriesByCode["contention"] != 1 || len(s.BlindRetriesByCode) != 1 || s.Undigested != 0 {
		t.Fatalf("one blind retry among the one-offs: %+v", s)
	}
}
