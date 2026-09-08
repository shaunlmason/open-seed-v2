package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
)

// conformance: plans/os-cf13fb51.md AC1, AC4 — the importer's refusal
// edges, each named: the table's shape, the manifest's losslessness
// check and parse, the run-log and card grammar, the export document,
// the anchor's every refusal, and the transform's placement check.
func TestImporterRefusalEdges(t *testing.T) {
	t.Run("table shape", func(t *testing.T) {
		for name, body := range map[string]string{
			"unparseable":    `{`,
			"wrong schema":   `{"schema":"other","source":"open-seed"}`,
			"wrong source":   `{"schema":"seed-import/0","source":"else"}`,
			"event and drop": `{"schema":"seed-import/0","source":"open-seed","verbs":{"x":{"event":"a","drop":"b"}},"defaults":{"tier":"L1","budget":"b","routing":"r"}}`,
			"neither":        `{"schema":"seed-import/0","source":"open-seed","verbs":{"x":{}},"defaults":{"tier":"L1","budget":"b","routing":"r"}}`,
			"identity kind":  `{"schema":"seed-import/0","source":"open-seed","identities":{"a":{"kind":"robot","name":"a"}},"defaults":{"tier":"L1","budget":"b","routing":"r"}}`,
			"identity name":  `{"schema":"seed-import/0","source":"open-seed","identities":{"a":{"kind":"agent","name":" "}},"defaults":{"tier":"L1","budget":"b","routing":"r"}}`,
			"no defaults":    `{"schema":"seed-import/0","source":"open-seed"}`,
		} {
			if _, err := LoadTable([]byte(body)); err == nil {
				t.Errorf("%s: the table loaded", name)
			}
		}
		table, err := DefaultTable()
		if err != nil {
			t.Fatal(err)
		}
		if id, known := table.IdentityFor("nobody-the-table-knows"); known || id.Kind != "agent" || id.Name != "nobody-the-table-knows" {
			t.Errorf("an unknown name is an agent under itself: %+v %v", id, known)
		}
		if id, _ := table.IdentityFor("  "); id.Name != "unattributed" {
			t.Errorf("a blank name is unattributed: %+v", id)
		}
		if (Entry{}).Str("x") != "" {
			t.Error("an entry without data reads empty")
		}
	})
	t.Run("run-log grammar", func(t *testing.T) {
		for name, body := range map[string]string{
			"unparseable": "{not json}\n",
			"no verb":     `{"ts":"2026-01-01T00:00:00Z","actor":"a"}` + "\n",
			"no ts":       `{"verb":"create","actor":"a"}` + "\n",
			"bad task id": `{"ts":"2026-01-01T00:00:00Z","verb":"create","task":"../x"}` + "\n",
		} {
			if _, err := ParseRunLog(body); err == nil {
				t.Errorf("%s: the run-log parsed", name)
			}
		}
		if entries, err := ParseRunLog("\n\n"); err != nil || len(entries) != 0 {
			t.Errorf("blank lines are skipped: %v %d", err, len(entries))
		}
	})
	t.Run("export document", func(t *testing.T) {
		dir := t.TempDir()
		write := func(name, body string) string {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}
		if _, err := ReadExport(filepath.Join(dir, "absent.json")); err == nil {
			t.Error("an absent export read")
		}
		if _, err := ReadExport(write("bad.json", "{")); err == nil {
			t.Error("an unparseable export read")
		}
		head := strings.Repeat("a", 40)
		for name, body := range map[string]string{
			"schema":  `{"schema_version":"2.0","backend":"filecards","head":"` + head + `"}`,
			"backend": `{"schema_version":"1.0","backend":"sqlite","head":"` + head + `"}`,
			"head":    `{"schema_version":"1.0","backend":"filecards","head":"short"}`,
		} {
			if _, err := ReadExport(write(name+".json", body)); err == nil {
				t.Errorf("%s: the export validated", name)
			}
		}
		e, err := ReadExport(write("bare.json", `{"schema_version":"1.0","backend":"filecards","head":"`+head+`"}`))
		if err != nil || e.Files == nil {
			t.Fatalf("a bare document with no files reads with an empty file map: %v %+v", err, e)
		}
	})
	t.Run("source grammar", func(t *testing.T) {
		base := &Export{SchemaVersion: "1.0", Backend: "filecards", Head: strings.Repeat("b", 40)}
		cases := map[string]map[string]string{
			"card id mismatch": {"tasks/os-1.md": "---\nid: os-2\nstate: ready\n---\nbody\n"},
			"card no front":    {"tasks/os-1.md": "no frontmatter\n"},
			"card unclosed":    {"tasks/os-1.md": "---\nid: os-1\n"},
			"handoff id":       {"handoff/bad id.md": "# handoff\n"},
			"bad run-log":      {"run-log.jsonl": "{\n"},
		}
		for name, files := range cases {
			e := *base
			e.Files = files
			if _, err := Read(&e); err == nil {
				t.Errorf("%s: the source read", name)
			}
		}
		e := *base
		e.Files = map[string]string{"mail/a.md": "x", "other/thing.txt": "y"}
		src, err := Read(&e)
		if err != nil || len(src.Mail) != 1 {
			t.Errorf("mail is listed and other files kept: %v %+v", err, src)
		}
		if (*Handoff)(nil).AnchorSHA() != "" || (&Handoff{Anchor: "no at"}).AnchorSHA() != "" {
			t.Error("a nil handoff or one without an anchor names no commit")
		}
		if got := (&Handoff{Anchor: "branch @ 0123456789ab"}).AnchorSHA(); got != "0123456789ab" {
			t.Errorf("the anchor's commit: %q", got)
		}
	})
	t.Run("anchor refusals", func(t *testing.T) {
		v := newV1Source(t, storeFiles())
		e := v.export(t)
		bare := t.TempDir()
		gitIn(t, bare, "init", "-q", "-b", "seed-state")
		hardenGitRepo(t, bare)
		if _, err := VerifyAnchor(e, bare, ""); refusalKind(err) != "unanchored" {
			t.Errorf("a source with no anchor tag: %v", err)
		}
		if _, err := VerifyAnchor(e, v.dir, "seed-anchor/absent"); refusalKind(err) != "unanchored" {
			t.Errorf("a tag that does not resolve: %v", err)
		}
		foreign := *e
		foreign.Head = strings.Repeat("c", 40)
		if _, err := VerifyAnchor(&foreign, v.dir, ""); refusalKind(err) != "unanchored" {
			t.Errorf("a head outside the source: %v", err)
		}
		extra := v.export(t)
		extra.Files["extra.md"] = "x"
		if _, err := VerifyAnchor(extra, v.dir, ""); refusalKind(err) != "export_mismatch" {
			t.Errorf("a file the tree lacks: %v", err)
		}
		differs := v.export(t)
		differs.Files["run-log.jsonl"] += "\n"
		if _, err := VerifyAnchor(differs, v.dir, ""); refusalKind(err) != "export_mismatch" {
			t.Errorf("a file that differs: %v", err)
		}
		missing := v.export(t)
		delete(missing.Files, "handoff/os-000001.md")
		if _, err := VerifyAnchor(missing, v.dir, ""); refusalKind(err) != "export_mismatch" {
			t.Errorf("a tree file the export lacks: %v", err)
		}
		if _, err := VerifyAnchor(e, v.dir, v.tag); err != nil {
			t.Errorf("the named anchor verifies: %v", err)
		}
	})
	t.Run("run edges", func(t *testing.T) {
		if _, err := Run(Options{}); err == nil {
			t.Error("no export ran")
		}
		files := storeFiles()
		files["tasks/os-000002.md"] = strings.Replace(files["tasks/os-000002.md"], "state: ready", "state: limbo", 1)
		v := newV1Source(t, files)
		if _, err := importInto(t, v, v.export(t), filepath.Join(t.TempDir(), "ledger")); refusalKind(err) != "import_unmapped" {
			t.Errorf("a card state without a row: %v", err)
		}
		v = newV1Source(t, storeFiles())
		res, err := Run(Options{Export: v.export(t), Source: v.dir, LedgerDir: filepath.Join(t.TempDir(), "ledger"), ArtifactsDir: filepath.Join(t.TempDir(), "art"), Operator: operatorKey(t)})
		if err != nil || res.Records == 0 {
			t.Fatalf("an import with no clock and no repo runs at now over the source: %v", err)
		}
		if _, err := ledgerRecords(filepath.Join(v.dir, "run-log.jsonl")); err == nil {
			t.Error("a file is not a ledger directory")
		}
	})
	t.Run("manifest check and parse", func(t *testing.T) {
		v := newV1Source(t, storeFiles())
		e := v.export(t)
		artDir := filepath.Join(t.TempDir(), "art")
		res, err := Run(Options{Export: e, Source: v.dir, LedgerDir: filepath.Join(t.TempDir(), "ledger"), ArtifactsDir: artDir, Operator: operatorKey(t), Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		src := mustRead(t, e)
		clone := func() *Manifest {
			b, _ := json.Marshal(res.Manifest)
			var m Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			return &m
		}
		if err := Check(clone(), src); err != nil {
			t.Fatalf("the real manifest checks: %v", err)
		}
		dup := clone()
		dup.Records = append(dup.Records, dup.Records[0])
		if err := Check(dup, src); err == nil || !strings.Contains(err.Error(), "two dispositions") {
			t.Errorf("a duplicated disposition: %v", err)
		}
		unknown := clone()
		unknown.Records[0].Record = "nowhere.md"
		if err := Check(unknown, src); err == nil || !strings.Contains(err.Error(), "does not hold") {
			t.Errorf("a disposition for nothing: %v", err)
		}
		short := clone()
		short.Records = short.Records[1:]
		if err := Check(short, src); err == nil || !strings.Contains(err.Error(), "no disposition") {
			t.Errorf("a record without a disposition: %v", err)
		}
		counts := clone()
		counts.Counts.Records++
		if err := Check(counts, src); err == nil || !strings.Contains(err.Error(), "counts disagree") {
			t.Errorf("stated counts off: %v", err)
		}
		// A count other than the two the first check reads, moved:
		// the recomputation from the dispositions catches it.
		b, _ := json.Marshal(res.Manifest)
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		cm := raw["counts"].(map[string]any)
		moved := false
		for k, val := range cm {
			if k == "records" || k == "dispositions" {
				continue
			}
			if n, ok := val.(float64); ok {
				cm[k] = n + 1
				moved = true
				break
			}
		}
		if moved {
			rb, _ := json.Marshal(raw)
			var m Manifest
			if err := json.Unmarshal(rb, &m); err != nil {
				t.Fatal(err)
			}
			if err := Check(&m, src); err == nil || !strings.Contains(err.Error(), "recomputed") {
				t.Errorf("a derived count off: %v", err)
			}
		}
		store := artifact.Open(artDir)
		if m, err := ParseManifest(store, res.ManifestDigest); err != nil || m.Schema != ManifestSchema {
			t.Errorf("the stored manifest parses: %v", err)
		}
		if _, err := ParseManifest(store, strings.Repeat("0", 64)); err == nil {
			t.Error("an absent digest parsed")
		}
		bad, _ := store.Put([]byte("{not json"))
		if _, err := ParseManifest(store, bad); err == nil {
			t.Error("an unparseable manifest parsed")
		}
		other, _ := store.Put([]byte(`{"schema":"other/0"}`))
		if _, err := ParseManifest(store, other); err == nil {
			t.Error("a manifest of another schema parsed")
		}
		// The placement check between passes, each difference named.
		same := func() (*Manifest, *Manifest) { return clone(), clone() }
		a, b2 := same()
		b2.Records = b2.Records[1:]
		if err := samePlacement(a, b2); err == nil {
			t.Error("a disposition count that moved")
		}
		a, b2 = same()
		b2.Records[0].Record = "moved"
		if err := samePlacement(a, b2); err == nil {
			t.Error("a record that moved")
		}
		a, b2 = same()
		for i := range b2.Records {
			if len(b2.Records[i].Events) > 0 {
				b2.Records[i].Events[0].Position++
				break
			}
		}
		if err := samePlacement(a, b2); err == nil {
			t.Error("an event position that moved")
		}
		a, b2 = same()
		b2.Identities = b2.Identities[1:]
		if err := samePlacement(a, b2); err == nil {
			t.Error("an identity count that moved")
		}
		a, b2 = same()
		b2.Identities[0].Enrolled++
		if err := samePlacement(a, b2); err == nil {
			t.Error("an enrollment position that moved")
		}
		if err := samePlacement(clone(), clone()); err != nil {
			t.Errorf("the same manifest twice: %v", err)
		}
	})
	t.Run("repository paths", func(t *testing.T) {
		for _, bad := range []string{"", "/abs", `a\b`, "a//b", "./a", "../a", "a/./b"} {
			if repoRelative(bad) {
				t.Errorf("%q is not repository-relative", bad)
			}
		}
		if !repoRelative("plans/os-1.md") {
			t.Error("a clean path beneath the root is")
		}
	})
}

// conformance: plans/os-cf13fb51.md AC4 — the repository lookup reads
// a cited path at a commit when it can, from the tree otherwise, and
// nothing outside the repository; the commit lookups name a merge by
// its PR, a file by the commit that added it, and the head.
func TestRepoLookupEdges(t *testing.T) {
	none := newRepoLookup("")
	if none.file("plans/x.md", "") != nil || none.mergeCommit("1") != "" || none.addedCommit("x") != "" || none.headCommit() != "" {
		t.Error("no repository reads nothing")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	hardenGitRepo(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plans", "os-1.md"), []byte("# plan v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "add plan (#7)")
	first := gitIn(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "plans", "os-1.md"), []byte("# plan v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := newRepoLookup(dir)
	if got := string(l.file("plans/os-1.md", first)); got != "# plan v1\n" {
		t.Errorf("the file at the commit: %q", got)
	}
	if got := string(l.file("plans/os-1.md", strings.Repeat("d", 40))); got != "# plan v2\n" {
		t.Errorf("an unreadable commit falls back to the tree: %q", got)
	}
	if l.file("plans/absent.md", "") != nil || l.file("../escape.md", "") != nil {
		t.Error("an absent or escaping path reads nothing")
	}
	if l.mergeCommit("7") != first || l.mergeCommit("7") != first {
		t.Errorf("the merge naming the PR, cached: %q", l.mergeCommit("7"))
	}
	if l.mergeCommit("99") != "" {
		t.Error("a PR nothing names has no merge")
	}
	if l.addedCommit("plans/os-1.md") != first || l.addedCommit("plans/none.md") != "" {
		t.Error("the commit that added the file, and none for an unknown path")
	}
	if l.headCommit() != first {
		t.Errorf("the head: %q", l.headCommit())
	}
}
