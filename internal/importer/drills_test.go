package importer

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// v1Source is a small predecessor: a git repository whose seed-state
// branch holds the store files, anchored by a seed-anchor tag, the
// shape scripts/seed state anchor leaves behind.
type v1Source struct {
	dir    string
	commit string
	tag    string
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=v1", "GIT_AUTHOR_EMAIL=v1@example.invalid", "GIT_COMMITTER_NAME=v1", "GIT_COMMITTER_EMAIL=v1@example.invalid", "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// storeFiles is the v1 store the drills use: one card through create,
// promote, claim, submit and accept, a lease renewal (a drop row), a
// comment with its block, a card that stays ready, a cancelled one,
// and a handoff for the packet.
func storeFiles() map[string]string {
	card := `---
id: os-000001
title: 'first card: a title with a colon'
state: done
priority: P2
squad: core
author: alice
created_at: "2026-01-01T00:00:00Z"
updated_at: "2026-01-01T00:05:00Z"
---

The body of the first card.

## Evidence ev-1 (comment, alice, 2026-01-01T00:03:00Z)

a comment on the card

## Evidence ev-2 (pr, bob, 2026-01-01T00:04:00Z)

https://github.com/o/r/pull/7
`
	ready := `---
id: os-000002
title: second card
state: ready
priority: P3
squad: core
author: alice
created_at: "2026-01-01T00:00:10Z"
updated_at: "2026-01-01T00:00:20Z"
---

Still ready at export time.
`
	cancelled := `---
id: os-000003
title: third card
state: cancelled
priority: P3
squad: core
author: alice
created_at: "2026-01-01T00:00:30Z"
updated_at: "2026-01-01T00:00:40Z"
---

Cancelled from backlog.
`
	handoff := `# Continuation packet — os-000001
> Generated 2026-01-01T00:02:30Z by seed handoff (reason: transition).

## Task
first card: a title with a colon — state in_progress, priority P2.

## Workspace anchor
branch feature @ 0123456789abcdef

## Next step
Re-read the card.
`
	log := strings.Join([]string{
		`{"actor":"alice","data":{"title":"first card: a title with a colon"},"task":"os-000001","ts":"2026-01-01T00:00:00Z","verb":"create"}`,
		`{"actor":"alice","data":{"to":"ready","transitioned":true},"task":"os-000001","ts":"2026-01-01T00:00:05Z","verb":"promote"}`,
		`{"actor":"alice","data":{"title":"second card"},"task":"os-000002","ts":"2026-01-01T00:00:10Z","verb":"create"}`,
		`{"actor":"alice","data":{"to":"ready","transitioned":true},"task":"os-000002","ts":"2026-01-01T00:00:20Z","verb":"promote"}`,
		`{"actor":"alice","data":{"title":"third card"},"task":"os-000003","ts":"2026-01-01T00:00:30Z","verb":"create"}`,
		`{"actor":"alice","data":{"to":"cancelled","transitioned":true},"task":"os-000003","ts":"2026-01-01T00:00:40Z","verb":"cancel"}`,
		`{"actor":"bob","data":{"lease_expires":"2026-01-01T01:00:00Z"},"task":"os-000001","ts":"2026-01-01T00:01:00Z","verb":"claim"}`,
		`{"actor":"bob","data":{"lease_expires":"2026-01-01T02:00:00Z"},"task":"os-000001","ts":"2026-01-01T00:02:00Z","verb":"lease-renew"}`,
		`{"actor":"alice","data":{"kind":"comment"},"task":"os-000001","ts":"2026-01-01T00:03:00Z","verb":"comment"}`,
		`{"actor":"bob","data":{"kind":"pr"},"task":"os-000001","ts":"2026-01-01T00:04:00Z","verb":"attach-evidence"}`,
		`{"actor":"bob","data":{"to":"review","transitioned":true},"task":"os-000001","ts":"2026-01-01T00:04:30Z","verb":"transition"}`,
		`{"actor":"alice","data":{"to":"done","transitioned":true},"task":"os-000001","ts":"2026-01-01T00:05:00Z","verb":"accept"}`,
	}, "\n") + "\n"
	return map[string]string{
		"tasks/os-000001.md":   card,
		"tasks/os-000002.md":   ready,
		"tasks/os-000003.md":   cancelled,
		"handoff/os-000001.md": handoff,
		"run-log.jsonl":        log,
	}
}

// newV1Source writes the files on a seed-state branch and anchors it.
func newV1Source(t *testing.T, files map[string]string) *v1Source {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "seed-state")
	hardenGitRepo(t, dir)
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "state")
	commit := gitIn(t, dir, "rev-parse", "HEAD")
	tag := "seed-anchor/20260101T000000Z"
	gitIn(t, dir, "tag", tag, commit)
	return &v1Source{dir: dir, commit: commit, tag: tag}
}

// exportAt is the v1 export document of a commit's tree.
func (v *v1Source) exportAt(t *testing.T, commit string) *Export {
	t.Helper()
	e := &Export{SchemaVersion: "1.0", Backend: "filecards", Head: commit, Files: map[string]string{}}
	for _, p := range strings.Split(gitIn(t, v.dir, "ls-tree", "-r", "--name-only", commit), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			raw, err := gitRaw(v.dir, "show", commit+":"+p)
			if err != nil {
				t.Fatal(err)
			}
			e.Files[p] = string(raw)
		}
	}
	return e
}

func (v *v1Source) export(t *testing.T) *Export { return v.exportAt(t, v.commit) }

// writeExport writes the document as the v1 command prints it.
func writeExport(t *testing.T, e *Export) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"document": e, "exported_at": "2026-01-01T00:10:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func importInto(t *testing.T, v *v1Source, e *Export, ledgerDir string) (*Result, error) {
	t.Helper()
	return Run(Options{Export: e, Source: v.dir, LedgerDir: ledgerDir, ArtifactsDir: filepath.Join(t.TempDir(), "art"), Operator: operatorKey(t), Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
}

func refusalKind(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Kind
	}
	return ""
}

func ledgerEmpty(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if e.Name() == "HEAD" {
			return false
		}
		if e.IsDir() {
			if sub, _ := os.ReadDir(filepath.Join(dir, e.Name())); len(sub) > 0 {
				return false
			}
		}
	}
	return true
}

// conformance: plans/os-cf13fb51.md AC2, AC3, AC5 — a small predecessor
// imports: the done card through the verdict chain over an import
// note, the ready card ready, the cancelled card cancelled, the lease
// renewal a drop row, the comment a message, the packet from the
// handoff, every identity suspended.
func TestSyntheticPredecessorImports(t *testing.T) {
	v := newV1Source(t, storeFiles())
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	artDir := filepath.Join(t.TempDir(), "art")
	res, err := Run(Options{Export: v.export(t), Source: v.dir, LedgerDir: ledgerDir, ArtifactsDir: artDir, Operator: operatorKey(t), Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	store, err := ledger.OpenReadOnly(ledgerDir)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := transition.Default()
	var records []*event.Record
	if err := store.Records(func(pos int, rec *event.Record) error { records = append(records, rec); return nil }); err != nil {
		t.Fatal(err)
	}
	fold := table.FoldRecords(records)
	for id, want := range map[string]string{"os-000001": "done", "os-000002": "ready", "os-000003": "cancelled"} {
		if s, ok := fold.State(id); !ok || s.State != want {
			t.Errorf("%s: folds to %+v, want %s", id, s.State, want)
		}
	}
	m := res.Manifest
	byRecord := map[string]Disposition{}
	for _, d := range m.Records {
		byRecord[d.Record] = d
	}
	if d := byRecord["run-log#7"]; d.Drop == "" || !strings.Contains(d.Drop, "lease") {
		t.Errorf("lease-renew is a drop row, got %+v", d)
	}
	if d := byRecord["run-log#8"]; len(d.Events) != 1 || d.Events[0].Verb != "message.sent" {
		t.Errorf("comment is message.sent, got %+v", d)
	}
	if d := byRecord["run-log#11"]; !strings.Contains(d.Note, "import note") || len(d.Events) != 3 {
		t.Errorf("accept without a receipt is the chain over an import note, got %+v", d)
	} else if d.Events[0].Verb != "verdict.rendered" || d.Events[1].Verb != "merge.requested" || d.Events[2].Verb != "merge.observed" {
		t.Errorf("the chain order is verdict, request, observe: %+v", d.Events)
	}
	if d := byRecord["run-log#10"]; len(d.Events) != 1 || d.Events[0].Verb != "submission.made" {
		t.Errorf("the transition to review is submission.made, got %+v", d)
	}
	if d := byRecord["handoff/os-000001.md"]; d.Artifact == "" {
		t.Errorf("the handoff is an artifact, got %+v", d)
	}
	if err := Check(m, mustRead(t, v.export(t))); err != nil {
		t.Errorf("losslessness: %v", err)
	}
	for _, row := range m.Identities {
		if row.Suspended <= row.Enrolled {
			t.Errorf("%s is not suspended after its enrollment: %+v", row.Name, row)
		}
	}
	// The verdict was rendered by alice's key, not bob's: alice never
	// claimed the card, so she is independent.
	for _, d := range m.Records {
		for _, e := range d.Events {
			if e.Verb == "verdict.rendered" {
				if e.Signer == fingerprintOf(t, m, "bob") {
					t.Error("the claimant's key rendered the verdict")
				}
			}
		}
	}
	if res.Imported != 6 {
		t.Errorf("system.imported at %d: genesis, five upgrades, then the provenance record", res.Imported)
	}
	// Every replayed record is signed by the mapped identity's key:
	// alice filed, bob claimed and submitted, alice observed the merge.
	alice, bob := fingerprintOf(t, m, "alice"), fingerprintOf(t, m, "bob")
	for _, d := range m.Records {
		for _, e := range d.Events {
			switch e.Verb {
			case "intent.filed", "contract.specified", "merge.observed", "message.sent":
				if e.Signer != alice {
					t.Errorf("%s at %d signed by %s, not alice", e.Verb, e.Position, e.Signer[:12])
				}
			case "claim.taken", "submission.made", "merge.requested":
				if e.Signer != bob {
					t.Errorf("%s at %d signed by %s, not bob", e.Verb, e.Position, e.Signer[:12])
				}
			case "contract.cancelled":
				if e.Signer == alice || e.Signer == bob {
					t.Errorf("contract.cancelled at %d signed by a generated key; it is the operator's", e.Position)
				}
			}
		}
	}
	// The keyring at the tip holds every generated key suspended, and
	// every verdict's receipt is in the artifact store.
	ring, _, err := keyring.StateAt(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range m.Identities {
		if entry, ok := ring.Get(row.Fingerprint); !ok || entry.Standing != keyring.StandingSuspended {
			t.Errorf("%s is not suspended at the tip: %+v", row.Source, entry)
		}
	}
	assertReceiptsStored(t, records, artifact.Open(artDir))
}

// assertReceiptsStored holds every verdict.rendered to a receipt the
// artifact store can retrieve.
func assertReceiptsStored(t *testing.T, records []*event.Record, art *artifact.Store) {
	t.Helper()
	n := 0
	for pos, rec := range records {
		if rec.Event.Verb != "verdict.rendered" {
			continue
		}
		var v struct {
			Receipt string `json:"receipt"`
		}
		if err := json.Unmarshal(rec.Event.Payload, &v); err != nil || v.Receipt == "" {
			t.Errorf("position %d: no receipt in the verdict", pos)
			continue
		}
		if _, err := art.Get(v.Receipt); err != nil {
			t.Errorf("position %d: the verdict cites receipt %s, not in the store: %v", pos, v.Receipt[:12], err)
		}
		n++
	}
	if n == 0 {
		t.Error("no verdict was rendered")
	}
}

func fingerprintOf(t *testing.T, m *Manifest, name string) string {
	t.Helper()
	for _, row := range m.Identities {
		if row.Source == name {
			return row.Fingerprint
		}
	}
	t.Fatalf("%s is not in the manifest", name)
	return ""
}

func mustRead(t *testing.T, e *Export) *Source {
	t.Helper()
	s, err := Read(e)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// conformance: plans/os-cf13fb51.md AC1 — anchors first: a tampered
// export refuses export_mismatch naming the path, an export whose head
// no anchor covers refuses unanchored, both before any write.
func TestAnchorRefusalsPrecedeEveryWrite(t *testing.T) {
	v := newV1Source(t, storeFiles())
	tampered := v.export(t)
	tampered.Files["tasks/os-000001.md"] = strings.Replace(tampered.Files["tasks/os-000001.md"], "state: done", "state: ready", 1)
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	_, err := importInto(t, v, tampered, ledgerDir)
	if refusalKind(err) != "export_mismatch" || !strings.Contains(err.Error(), "tasks/os-000001.md") {
		t.Fatalf("a tampered export refuses export_mismatch naming the path, got %v", err)
	}
	if !ledgerEmpty(t, ledgerDir) {
		t.Fatal("the refusal wrote into the ledger")
	}
	// The anchor check precedes the transform: an export that is both
	// tampered and unmappable refuses as tampered.
	both := v.export(t)
	both.Files["run-log.jsonl"] += `{"actor":"alice","data":{},"task":"os-000002","ts":"2026-01-01T00:06:00Z","verb":"nudge"}` + "\n"
	if _, err := importInto(t, v, both, ledgerDir); refusalKind(err) != "export_mismatch" {
		t.Fatalf("anchors are checked before the table, got %v", err)
	}
	extra := v.export(t)
	extra.Files["tasks/os-000009.md"] = "---\nid: os-000009\nstate: backlog\n---\n"
	if _, err := importInto(t, v, extra, ledgerDir); refusalKind(err) != "export_mismatch" {
		t.Fatalf("a file the anchored tree lacks refuses export_mismatch, got %v", err)
	}
	missing := v.export(t)
	delete(missing.Files, "handoff/os-000001.md")
	if _, err := importInto(t, v, missing, ledgerDir); refusalKind(err) != "export_mismatch" {
		t.Fatalf("a file the export lacks refuses export_mismatch, got %v", err)
	}
	// A head past the anchor: a second state commit nobody anchored.
	if err := os.WriteFile(filepath.Join(v.dir, "tasks", "os-000002.md"), []byte(strings.Replace(storeFiles()["tasks/os-000002.md"], "state: ready", "state: blocked", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, v.dir, "commit", "-q", "-am", "later")
	later := v.exportAt(t, gitIn(t, v.dir, "rev-parse", "HEAD"))
	if _, err := importInto(t, v, later, ledgerDir); refusalKind(err) != "unanchored" {
		t.Fatalf("a head no anchor covers refuses unanchored, got %v", err)
	}
	// No anchor at all.
	bare := newV1Source(t, storeFiles())
	gitIn(t, bare.dir, "tag", "-d", bare.tag)
	if _, err := importInto(t, bare, bare.export(t), ledgerDir); refusalKind(err) != "unanchored" {
		t.Fatalf("a source with no anchor refuses unanchored, got %v", err)
	}
	if !ledgerEmpty(t, ledgerDir) {
		t.Fatal("a refusal wrote into the ledger")
	}
	// An ancestor of the anchor is covered: the export at the first
	// commit imports against the later anchor.
	v2 := newV1Source(t, storeFiles())
	if err := os.WriteFile(filepath.Join(v2.dir, "note.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, v2.dir, "add", "-A")
	gitIn(t, v2.dir, "commit", "-q", "-m", "later")
	gitIn(t, v2.dir, "tag", "seed-anchor/20260102T000000Z", "HEAD")
	if _, err := importInto(t, v2, v2.export(t), filepath.Join(t.TempDir(), "l2")); err != nil {
		t.Fatalf("an ancestor of the newest anchor is anchored: %v", err)
	}
}

// conformance: plans/os-cf13fb51.md AC2 — genesis import refuses a
// non-empty ledger.
func TestNonEmptyLedgerRefuses(t *testing.T) {
	v := newV1Source(t, storeFiles())
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	if _, err := importInto(t, v, v.export(t), ledgerDir); err != nil {
		t.Fatal(err)
	}
	store, _ := ledger.OpenReadOnly(ledgerDir)
	_, before, _ := store.Tip()
	if _, err := importInto(t, v, v.export(t), ledgerDir); refusalKind(err) != "ledger_not_empty" {
		t.Fatalf("a second import refuses ledger_not_empty, got %v", err)
	}
	_, after, _ := store.Tip()
	if before != after {
		t.Fatalf("the refusal changed the ledger: %d then %d", before, after)
	}
}

// conformance: plans/os-cf13fb51.md AC5 — a run-log verb with no row
// refuses import_unmapped before any write; a drop is a row.
func TestUnmappedVerbRefuses(t *testing.T) {
	files := storeFiles()
	files["run-log.jsonl"] += `{"actor":"alice","data":{},"task":"os-000002","ts":"2026-01-01T00:06:00Z","verb":"nudge"}` + "\n"
	v := newV1Source(t, files)
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	_, err := importInto(t, v, v.export(t), ledgerDir)
	if refusalKind(err) != "import_unmapped" || !strings.Contains(err.Error(), "nudge") {
		t.Fatalf("an unmapped verb refuses import_unmapped naming it, got %v", err)
	}
	if !ledgerEmpty(t, ledgerDir) {
		t.Fatal("the refusal wrote into the ledger")
	}
	table, _ := DefaultTable()
	for verb, row := range table.Verbs {
		if row.Drop != "" && strings.TrimSpace(row.Drop) == "" {
			t.Errorf("%s: a drop names its reason", verb)
		}
	}
	if _, err := LoadTable([]byte(`{"schema":"seed-import/0","source":"open-seed","identities":{},"states":{},"verbs":{"x":{}},"defaults":{"tier":"trivial","budget":"small","routing":"core"}}`)); err == nil {
		t.Error("a row that is neither an event nor a drop refuses to load")
	}
}

// conformance: plans/os-cf13fb51.md D4 — the spec carries the table
// verbatim.
func TestTableMirrorsTheSpec(t *testing.T) {
	spec, err := os.ReadFile("../../spec/import-open-seed.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(spec) != string(TableJSON()) {
		t.Fatal("spec/import-open-seed.json differs from the embedded table")
	}
	if _, err := DefaultTable(); err != nil {
		t.Fatal(err)
	}
}
