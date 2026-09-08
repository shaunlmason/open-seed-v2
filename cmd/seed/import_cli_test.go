package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v1Repo is the smallest predecessor the CLI drill needs: one card
// created, promoted and left ready, on an anchored seed-state branch.
func v1Repo(t *testing.T, extraLog string) (dir, commit string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=v1", "GIT_AUTHOR_EMAIL=v1@example.invalid", "GIT_COMMITTER_NAME=v1", "GIT_COMMITTER_EMAIL=v1@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "seed-state")
	hardenGitRepo(t, dir)
	files := map[string]string{
		"tasks/os-000001.md": "---\nid: os-000001\ntitle: one\nstate: ready\npriority: P3\nsquad: core\nauthor: alice\ncreated_at: \"2026-01-01T00:00:00Z\"\nupdated_at: \"2026-01-01T00:00:05Z\"\n---\n\nbody\n",
		"run-log.jsonl": `{"actor":"alice","data":{"title":"one"},"task":"os-000001","ts":"2026-01-01T00:00:00Z","verb":"create"}` + "\n" +
			`{"actor":"alice","data":{"to":"ready","transitioned":true},"task":"os-000001","ts":"2026-01-01T00:00:05Z","verb":"promote"}` + "\n" + extraLog,
	}
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "-A")
	git("commit", "-q", "-m", "state")
	commit = git("rev-parse", "HEAD")
	git("tag", "seed-anchor/20260101T000000Z", commit)
	return dir, commit
}

func v1Export(t *testing.T, dir, commit string, edit func(files map[string]string)) string {
	t.Helper()
	files := map[string]string{}
	for _, p := range []string{"tasks/os-000001.md", "run-log.jsonl"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		files[p] = string(b)
	}
	if edit != nil {
		edit(files)
	}
	b, _ := json.Marshal(map[string]any{"document": map[string]any{"schema_version": "1.0", "backend": "filecards", "head": commit, "files": files}})
	p := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// conformance: plans/os-cf13fb51.md AC1, AC2, AC5, AC6 — the second
// command, its envelope on success, and the refusal codes: exit 29
// import_refused refined as unanchored, export_mismatch and
// import_unmapped; exit 3 ledger_not_empty; usage for a missing flag.
func TestImportCommandEnvelopes(t *testing.T) {
	_, priv, _ := writeKeys(t)
	src, commit := v1Repo(t, "")
	export := v1Export(t, src, commit, nil)
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	artDir := filepath.Join(t.TempDir(), "art")

	if e, code := runEnv(t, "import", "--from-open-seed", export, "--source", src, "--ledger", ledgerDir, "--key", priv); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("a missing --artifacts is usage: %d %+v", code, e)
	}
	e, code := runEnv(t, "import", "--from-open-seed", export, "--source", src, "--ledger", ledgerDir, "--artifacts", artDir, "--key", priv)
	if code != 0 || !e.OK {
		t.Fatalf("the import: %d %+v", code, e)
	}
	if e.Result["imported"] != float64(6) || e.Result["manifest"] == "" {
		t.Fatalf("the envelope names system.imported's position and the manifest digest: %+v", e.Result)
	}
	if anchor, _ := e.Result["anchor"].(map[string]any); anchor["tag"] != "seed-anchor/20260101T000000Z" || anchor["commit"] != commit {
		t.Fatalf("the envelope names the anchor verified: %+v", e.Result["anchor"])
	}
	counts, _ := e.Result["counts"].(map[string]any)
	if counts["records"] != counts["dispositions"] || counts["records"] != float64(3) {
		t.Fatalf("two run-log entries and one card are three records with three dispositions: %+v", counts)
	}
	if e, code := runEnv(t, "import", "--from-open-seed", export, "--source", src, "--ledger", ledgerDir, "--artifacts", artDir, "--key", priv); code != 3 || e.Error == nil || e.Error.Code != "ledger_not_empty" {
		t.Fatalf("a second import refuses ledger_not_empty: %d %+v", code, e)
	}
	fresh := func() string { return filepath.Join(t.TempDir(), "ledger") }
	tampered := v1Export(t, src, commit, func(f map[string]string) {
		f["tasks/os-000001.md"] = strings.Replace(f["tasks/os-000001.md"], "state: ready", "state: done", 1)
	})
	if e, code := runEnv(t, "import", "--from-open-seed", tampered, "--source", src, "--ledger", fresh(), "--artifacts", artDir, "--key", priv); code != 29 || e.Error == nil || e.Error.Code != "export_mismatch" || !strings.Contains(e.Error.Message, "tasks/os-000001.md") {
		t.Fatalf("a tampered export refuses export_mismatch naming the path: %d %+v", code, e)
	}
	unanchored := v1Export(t, src, commit, nil)
	if e, code := runEnv(t, "import", "--from-open-seed", unanchored, "--source", src, "--anchor", "seed-anchor/absent", "--ledger", fresh(), "--artifacts", artDir, "--key", priv); code != 29 || e.Error == nil || e.Error.Code != "unanchored" {
		t.Fatalf("an anchor that does not resolve refuses unanchored: %d %+v", code, e)
	}
	src2, commit2 := v1Repo(t, `{"actor":"alice","data":{},"task":"os-000001","ts":"2026-01-01T00:00:09Z","verb":"nudge"}`+"\n")
	if e, code := runEnv(t, "import", "--from-open-seed", v1Export(t, src2, commit2, nil), "--source", src2, "--ledger", fresh(), "--artifacts", artDir, "--key", priv); code != 29 || e.Error == nil || e.Error.Code != "import_unmapped" || !strings.Contains(e.Error.Message, "nudge") {
		t.Fatalf("an unmapped verb refuses import_unmapped naming it: %d %+v", code, e)
	}
	if e, code := runEnv(t, "import", "--from-open-seed", filepath.Join(t.TempDir(), "absent.json"), "--source", src, "--ledger", fresh(), "--artifacts", artDir, "--key", priv); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("an absent export is usage: %d %+v", code, e)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"document": {"schema_version": "2.0", "backend": "filecards", "head": "`+commit+`", "files": {}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "import", "--from-open-seed", bad, "--source", src, "--ledger", fresh(), "--artifacts", artDir, "--key", priv); code != 66 || e.Error == nil || e.Error.Code != "unreadable" {
		t.Fatalf("an export this build does not read is unreadable: %d %+v", code, e)
	}
}
