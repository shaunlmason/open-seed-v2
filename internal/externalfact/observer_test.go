package externalfact

// The observer fills merge.observed's sha from a forge and refuses an
// unmerged pull request (plans/os-ad610334.md D4). The snapshot arm and
// a fake Forgejo both answer the same shape.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotObserver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pulls.json")
	os.WriteFile(path, []byte(`{"pulls": {"pr/1": {"merged": true, "merge_commit_sha": "abc123"}, "pr/2": {"merged": false}}}`), 0o644)
	obs := Snapshot{Path: path}
	sha, merged, err := obs.Merged("pr/1")
	if err != nil || !merged || sha != "abc123" {
		t.Fatalf("a merged PR returns its sha, got %q %v %v", sha, merged, err)
	}
	if _, merged, _ := obs.Merged("pr/2"); merged {
		t.Error("an unmerged PR reports not merged")
	}
	if _, _, err := obs.Merged("pr/9"); err == nil {
		t.Error("an unknown PR is an error, never a silent false")
	}
}

func TestForgejoObserver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/repos/o/r/pulls/7":
			json.NewEncoder(w).Encode(prState{Merged: true, MergeCommitSHA: "deadbeef"})
		case "/api/v1/repos/o/r/pulls/8":
			json.NewEncoder(w).Encode(prState{Merged: false})
		default:
			http.Error(w, "no such pr", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	sha, merged, err := fj.Merged("pr/7")
	if err != nil || !merged || sha != "deadbeef" {
		t.Fatalf("Forgejo observer returns the merge sha, got %q %v %v", sha, merged, err)
	}
	if _, merged, _ := fj.Merged("8"); merged {
		t.Error("an unmerged Forgejo PR reports not merged")
	}
	if _, _, err := fj.Merged("not-a-number"); err == nil {
		t.Error("a non-numeric pr is refused")
	}
}

func TestGitHubObserver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			json.NewEncoder(w).Encode(prState{Merged: true, MergeCommitSHA: "cafef00d"})
		case "/repos/o/r/pulls/8":
			json.NewEncoder(w).Encode(prState{Merged: false})
		default:
			http.Error(w, "no such pr", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	gh := NewGitHub(srv.URL, "o", "r", "tok")
	sha, merged, err := gh.Merged("pr/7")
	if err != nil || !merged || sha != "cafef00d" {
		t.Fatalf("a merged GitHub PR returns its sha, got %q %v %v", sha, merged, err)
	}
	if _, merged, _ := gh.Merged("8"); merged {
		t.Error("an unmerged GitHub PR reports not merged")
	}
	if _, _, err := gh.Merged(""); err == nil {
		t.Error("an empty pr reference is refused, never a silent false")
	}
	if _, _, err := gh.Merged("pr/x"); err == nil {
		t.Error("a non-numeric pr is refused")
	}
	if _, _, err := gh.Merged("pr/9"); err == nil {
		t.Error("a forge error surfaces, never a silent false")
	}
}

func TestSnapshotObserverRefusalsNameTheCause(t *testing.T) {
	dir := t.TempDir()

	absent := Snapshot{Path: filepath.Join(dir, "nope.json")}
	if _, _, err := absent.Merged("pr/1"); err == nil || !strings.Contains(err.Error(), "reading the pull-request snapshot") {
		t.Errorf("an absent snapshot names itself, got %v", err)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (Snapshot{Path: bad}).Merged("pr/1"); err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Errorf("a malformed snapshot names itself, got %v", err)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"pulls": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, merged, err := (Snapshot{Path: empty}).Merged("pr/7")
	if err == nil || !strings.Contains(err.Error(), "pr/7") {
		t.Errorf("an unknown pull request refuses by name rather than reading as unmerged, got %v", err)
	}
	if merged {
		t.Error("a refusal never reports merged")
	}
}
