package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
)

// conformance: plans/os-cf13fb51.md D4 and D5 — the rows a small
// export rarely reaches: a plan re-specified at its merged commit and
// a receipt stored from --repo, mail as an artifact and a message, a
// recorded evidence link, an unblock that transitioned and one that
// did not, the bookkeeping drops, a cancel and a close bridged out of
// in_progress, a duplicate create, an entry with no task, and a title
// clipped for the payload cap — each with exactly one disposition.
func TestTransformCoversTheRarerRows(t *testing.T) {
	files := storeFiles()
	long := strings.Repeat("a long title ", 40)
	files["tasks/os-000004.md"] = "---\nid: os-000004\ntitle: '" + long + "'\nstate: cancelled\npriority: P3\nsquad: core\nauthor: alice\ncreated_at: \"2026-01-01T00:07:00Z\"\nupdated_at: \"2026-01-01T00:08:00Z\"\n---\n\nCancelled while held.\n"
	files["tasks/os-000005.md"] = "---\nid: os-000005\ntitle: fifth\nstate: done\npriority: P3\nsquad: \nauthor: bob\ncreated_at: \"2026-01-01T00:09:00Z\"\nupdated_at: \"2026-01-01T00:12:00Z\"\n---\n\nClosed from in_progress, with a receipt and a plan.\n\n## Evidence ev-5 (receipt, bob, 2026-01-01T00:11:00Z)\n\nreceipts/os-000005.json (boundary, retention)\n"
	files["tasks/os-000006.md"] = "---\nid: os-000006\ntitle: sixth\nstate: ready\npriority: P3\nsquad: core\nauthor: alice\ncreated_at: \"2026-01-01T00:13:00Z\"\nupdated_at: \"2026-01-01T00:16:00Z\"\n---\n\nBlocked on a plan, then unblocked.\n"
	files["mail/alice/0001.md"] = "# mail\n\nhello alice\n"
	files["notes.txt"] = "a file of no known kind\n"
	files["run-log.jsonl"] += strings.Join([]string{
		`{"actor":"alice","data":{"title":"` + long + `"},"task":"os-000004","ts":"2026-01-01T00:07:00Z","verb":"create"}`,
		`{"actor":"alice","data":{"title":"again"},"task":"os-000004","ts":"2026-01-01T00:07:01Z","verb":"create"}`,
		`{"actor":"bob","data":{"lease_expires":"2026-01-01T01:00:00Z"},"task":"os-000004","ts":"2026-01-01T00:07:30Z","verb":"claim"}`,
		`{"actor":"alice","data":{"to":"cancelled","transitioned":true},"task":"os-000004","ts":"2026-01-01T00:08:00Z","verb":"cancel"}`,
		`{"actor":"bob","data":{"title":"fifth"},"task":"os-000005","ts":"2026-01-01T00:09:00Z","verb":"create"}`,
		`{"actor":"bob","data":{"to":"ready","transitioned":true},"task":"os-000005","ts":"2026-01-01T00:09:30Z","verb":"promote"}`,
		`{"actor":"bob","data":{"lease_expires":"2026-01-01T01:00:00Z"},"task":"os-000005","ts":"2026-01-01T00:10:00Z","verb":"claim"}`,
		`{"actor":"bob","data":{"evidence":"https://github.com/o/r/pull/9"},"task":"os-000005","ts":"2026-01-01T00:10:30Z","verb":"record-evidence"}`,
		`{"actor":"bob","data":{"kind":"receipt"},"task":"os-000005","ts":"2026-01-01T00:11:00Z","verb":"attach-evidence"}`,
		`{"actor":"alice","data":{"to":"done","transitioned":true},"task":"os-000005","ts":"2026-01-01T00:12:00Z","verb":"close"}`,
		`{"actor":"alice","data":{"title":"sixth"},"task":"os-000006","ts":"2026-01-01T00:13:00Z","verb":"create"}`,
		`{"actor":"alice","data":{"to":"ready","transitioned":true},"task":"os-000006","ts":"2026-01-01T00:13:30Z","verb":"promote"}`,
		`{"actor":"alice","data":{"to":"blocked","transitioned":true},"task":"os-000006","ts":"2026-01-01T00:14:00Z","verb":"transition"}`,
		`{"actor":"shim","data":{"removed":"dep:os-000001"},"task":"os-000006","ts":"2026-01-01T00:14:30Z","verb":"blocker_resolved"}`,
		`{"actor":"alice","data":{"to":"blocked","transitioned":false},"task":"os-000006","ts":"2026-01-01T00:15:00Z","verb":"unblock"}`,
		`{"actor":"alice","data":{"auto_path":"plan_unblock","removed":"plan:3","to":"ready"},"task":"os-000006","ts":"2026-01-01T00:16:00Z","verb":"plan-unblock"}`,
		`{"actor":"seed-maintenance","data":{"failures":1},"task":"","ts":"2026-01-01T00:17:00Z","verb":"halt"}`,
		`{"actor":"alice","data":{"kind":"comment"},"task":"os-000006","ts":"2026-01-01T00:18:00Z","verb":"comment"}`,
	}, "\n") + "\n"
	v := newV1Source(t, files)
	// A repository beside the source with the cited receipt and the
	// plan; the plan is committed so its adding commit anchors it, the
	// receipt is left untracked so the working tree serves it.
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	hardenGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "receipts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "plans", "os-000006.md"), []byte("# plan six\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "plan six")
	if err := os.WriteFile(filepath.Join(repo, "receipts", "os-000005.json"), []byte(`{"contract": "os-000005"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	artDir := filepath.Join(t.TempDir(), "art")
	res, err := Run(Options{Export: v.export(t), Source: v.dir, Repo: repo, LedgerDir: ledgerDir, ArtifactsDir: artDir, Operator: operatorKey(t), Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	m := res.Manifest
	if err := Check(m, mustRead(t, v.export(t))); err != nil {
		t.Fatalf("losslessness: %v", err)
	}
	byRecord := map[string]Disposition{}
	for _, d := range m.Records {
		byRecord[d.Record] = d
	}
	want := map[string]func(d Disposition) bool{
		"run-log#13": func(d Disposition) bool { return strings.Contains(d.Note, "already existed") && len(d.Events) == 0 },
		"run-log#15": func(d Disposition) bool {
			return len(d.Events) == 2 && d.Events[0].Verb == "claim.reaped" && d.Events[1].Verb == "contract.cancelled" && strings.Contains(d.Note, "did not hold")
		},
		"run-log#19": func(d Disposition) bool { return d.Artifact != "" && strings.Contains(d.Note, "") },
		"run-log#20": func(d Disposition) bool {
			return d.Artifact != "" && strings.Contains(d.Note, "receipt receipts/os-000005.json stored")
		},
		"run-log#21": func(d Disposition) bool {
			return len(d.Events) == 4 && d.Events[0].Verb == "submission.made" && d.Events[1].Verb == "verdict.rendered"
		},
		"run-log#25": func(d Disposition) bool { return d.Drop != "" },
		"run-log#26": func(d Disposition) bool {
			return len(d.Events) == 0 && strings.Contains(d.Note, "nothing transitioned")
		},
		"run-log#27": func(d Disposition) bool {
			return len(d.Events) == 2 && d.Events[0].Verb == "contract.unblocked" && d.Events[1].Verb == "contract.specified" && strings.Contains(d.Note, "re-specified at the merged plan")
		},
		"run-log#28": func(d Disposition) bool { return d.Drop != "" },
		"run-log#29": func(d Disposition) bool {
			return len(d.Events) == 1 && d.Events[0].Verb == "message.sent" && strings.Contains(d.Note, "no comment block")
		},
		"mail/alice/0001.md": func(d Disposition) bool { return len(d.Events) == 1 && d.Events[0].Verb == "message.sent" },
		"notes.txt":          func(d Disposition) bool { return d.Artifact != "" && strings.Contains(d.Note, "no known kind") },
	}
	for rec, ok := range want {
		d, present := byRecord[rec]
		if !present || !ok(d) {
			t.Errorf("%s: unexpected disposition %+v", rec, d)
		}
	}
	// The stored receipt and plan are retrievable by digest, and the
	// clipped title fits the payload cap.
	art := artifact.Open(artDir)
	for _, d := range m.Records {
		if d.Artifact != "" {
			if _, err := art.Get(d.Artifact); err != nil {
				t.Errorf("%s: %v", d.Record, err)
			}
		}
	}
	if len(clip(long)) > maxInline+3 {
		t.Fatalf("clip bounds the title: %d bytes", len(clip(long)))
	}
	// The ready-card reconciliation and the export head stand-in are
	// listed, not hidden.
	if len(m.Unresolved) == 0 {
		t.Fatal("the fifth card's merge commit does not resolve from a repository with no PR, and the manifest says so")
	}
}

// conformance: plans/os-cf13fb51.md D1 — an export this build does not
// read, and one whose document wrapper is absent.
func TestReadExportShapes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	head := strings.Repeat("a", 40)
	if e, err := ReadExport(write("bare.json", `{"schema_version": "1.0", "backend": "filecards", "head": "`+head+`", "files": {}}`)); err != nil || e.Head != head {
		t.Fatalf("a bare document reads: %v %+v", err, e)
	}
	for name, body := range map[string]string{
		"schema":  `{"document": {"schema_version": "2.0", "backend": "filecards", "head": "` + head + `", "files": {}}}`,
		"backend": `{"document": {"schema_version": "1.0", "backend": "beads", "head": "` + head + `", "files": {}}}`,
		"head":    `{"document": {"schema_version": "1.0", "backend": "filecards", "head": "short", "files": {}}}`,
		"json":    `{`,
	} {
		if _, err := ReadExport(write(name+".json", body)); err == nil {
			t.Errorf("%s must refuse", name)
		}
	}
	if _, err := ParseManifest(artifact.Open(dir), strings.Repeat("0", 64)); err == nil {
		t.Fatal("a manifest the store lacks refuses")
	}
}
