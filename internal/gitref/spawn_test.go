package gitref

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The client's per-append path spawns git as few times as the work
// allows (spec/platform.md: the cmd/seed suite's Windows residual
// is process creation). These drills pin the two in-process seams: the
// tracking-ref read and the archive unpack. The hardening write stays
// on git's own writer (plans/os-711b3028.md D1; review on #298), and
// its drills live in gitref_test.go.

func TestIsObjectID(t *testing.T) {
	sha1 := strings.Repeat("a1", 20)
	sha256 := strings.Repeat("b2", 32)
	for id, want := range map[string]bool{sha1: true, sha256: true, sha1[:39]: false, strings.ToUpper(sha1): false, sha1[:39] + "g": false, "": false} {
		if got := isObjectID(id); got != want {
			t.Errorf("isObjectID(%q) = %v, want %v", id, got, want)
		}
	}
}

// The tracking tip is read from the loose ref a fetch writes, with no
// process; a ref stored any other way is resolved by git itself.
func TestTrackingTipReadsLooseRefsAndFallsBackToGit(t *testing.T) {
	state := t.TempDir()
	c, err := NewClient(state, bareRemote(t), "refs/seed/ledger")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.trackingTip(); err == nil {
		t.Fatal("an absent tracking ref is an error, not an empty tip")
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, err := c.Commit(work, "", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(c.gitDir, "update-ref", localTracking, commit); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(c.gitDir, filepath.FromSlash(localTracking))
	if _, err := os.Stat(loose); err != nil {
		t.Fatalf("update-ref writes the ref loose: %v", err)
	}
	if tip, err := c.trackingTip(); err != nil || tip != commit {
		t.Fatalf("loose read: %q %v, want %q", tip, err, commit)
	}
	if _, err := runGit(c.gitDir, "pack-refs", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(loose); err == nil {
		t.Fatal("pack-refs removes the loose file, so the fallback is exercised")
	}
	if tip, err := c.trackingTip(); err != nil || tip != commit {
		t.Fatalf("packed read: %q %v, want %q", tip, err, commit)
	}
}

// Fetch's common path is one git process; ls-remote runs only when
// the fetch refuses, to tell a fresh ledger from an unreachable one.
func TestFetchDistinguishesAnAbsentRefFromAnUnreachableRemote(t *testing.T) {
	remote := bareRemote(t)
	c, err := NewClient(t.TempDir(), remote, "refs/seed/ledger")
	if err != nil {
		t.Fatal(err)
	}
	if tip, err := c.Fetch(); err != nil || tip != "" {
		t.Fatalf("an absent ref is a fresh ledger: %q %v", tip, err)
	}
	gone, err := NewClient(t.TempDir(), filepath.Join(t.TempDir(), "nowhere.git"), "refs/seed/ledger")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gone.Fetch(); err == nil || !strings.Contains(err.Error(), ErrUnavailable.Error()) {
		t.Fatalf("an unreachable remote is unavailable, not fresh: %v", err)
	}
}

func TestMaterializeUnpacksNestedTreesWithModes(t *testing.T) {
	c, err := NewClient(t.TempDir(), bareRemote(t), "refs/seed/ledger")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "sub", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "sub", "deeper", "a.jsonl"), []byte("{\"a\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	commit, err := c.Commit(work, "", "tree")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if err := c.Materialize(commit, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "sub", "deeper", "a.jsonl"))
	if err != nil || string(b) != "{\"a\":1}\n" {
		t.Fatalf("nested file: %q %v", b, err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(out, "run.sh"))
		if err != nil || st.Mode().Perm()&0o100 == 0 {
			t.Fatalf("the executable bit survives the unpack: %v %v", st, err)
		}
	}
	if err := c.Materialize("0123456789012345678901234567890123456789", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("an unknown commit is git's error, surfaced")
	}
}

func TestUntarRefusesEscapesAndUnsupportedEntries(t *testing.T) {
	build := func(entries ...tar.Header) *bytes.Buffer {
		var buf bytes.Buffer
		w := tar.NewWriter(&buf)
		for i := range entries {
			h := entries[i]
			if err := w.WriteHeader(&h); err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg {
				w.Write(bytes.Repeat([]byte("x"), int(h.Size)))
			}
		}
		w.Close()
		return &buf
	}
	dir := t.TempDir()
	if err := untar(build(tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644}), dir); err == nil {
		t.Fatal("an entry above the target directory is refused")
	}
	if err := untar(build(tar.Header{Name: "dev", Typeflag: tar.TypeChar, Mode: 0o644}), dir); err == nil {
		t.Fatal("a device entry is refused")
	}
	// A pax global header (git archive writes the commit id in one) is
	// skipped, a directory entry is created, and a symlink is linked.
	entries := []tar.Header{
		{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "abc"}},
		{Name: "d/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "d/f", Typeflag: tar.TypeReg, Size: 3, Mode: 0o644},
	}
	if runtime.GOOS != "windows" {
		entries = append(entries, tar.Header{Name: "d/link", Typeflag: tar.TypeSymlink, Linkname: "f", Mode: 0o777})
	}
	if err := untar(build(entries...), dir); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "d", "f")); err != nil || string(b) != "xxx" {
		t.Fatalf("d/f: %q %v", b, err)
	}
	if runtime.GOOS != "windows" {
		if target, err := os.Readlink(filepath.Join(dir, "d", "link")); err != nil || target != "f" {
			t.Fatalf("d/link: %q %v", target, err)
		}
	}
}

// The seams refuse cleanly when the filesystem or the stream is not
// what a healthy run hands them.
func TestUntarSurfacesStreamAndFilesystemErrors(t *testing.T) {
	header := func(h tar.Header) []byte {
		var buf bytes.Buffer
		w := tar.NewWriter(&buf)
		if err := w.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		w.Flush()
		return buf.Bytes()
	}
	if err := untar(bytes.NewReader(bytes.Repeat([]byte("not a tar stream"), 64)), t.TempDir()); err == nil {
		t.Fatal("a malformed stream is an error")
	}
	// A header promising ten bytes the stream never delivers.
	if err := untar(bytes.NewReader(header(tar.Header{Name: "short", Typeflag: tar.TypeReg, Size: 10, Mode: 0o644})), t.TempDir()); err == nil {
		t.Fatal("a truncated entry is an error")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := untar(bytes.NewReader(header(tar.Header{Name: "f/x", Typeflag: tar.TypeReg, Size: 0, Mode: 0o644})), dir); err == nil {
		t.Fatal("a file where a directory must be created is an error")
	}
	if err := untar(bytes.NewReader(header(tar.Header{Name: "f/", Typeflag: tar.TypeDir, Mode: 0o755})), dir); err == nil {
		t.Fatal("a file where a directory entry lands is an error")
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := untar(bytes.NewReader(header(tar.Header{Name: "d", Typeflag: tar.TypeReg, Size: 0, Mode: 0o644})), dir); err == nil {
		t.Fatal("a directory where a file lands is an error")
	}
	if runtime.GOOS != "windows" {
		if err := untar(bytes.NewReader(header(tar.Header{Name: "d", Typeflag: tar.TypeSymlink, Linkname: "f", Mode: 0o777})), dir); err == nil {
			t.Fatal("a directory where a symlink lands is an error")
		}
		if err := untar(bytes.NewReader(header(tar.Header{Name: "f/l", Typeflag: tar.TypeSymlink, Linkname: "f", Mode: 0o777})), dir); err == nil {
			t.Fatal("a file where a symlink's parent must be created is an error")
		}
	}
}
