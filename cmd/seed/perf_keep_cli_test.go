package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// conformance: plans/os-5063e8ba.md AC3 — seed perf run --keep on a storm
// with one planted refusal (a hook declining the first writer's push as
// bad_prev beyond its append, then admitting) leaves the remote and the
// writers' state dirs under the named path, the refused tree among
// them, and reports relinked. The storm still lands every writer: the
// seventh shape re-links instead of losing the append.
func TestPerfRunKeepsTheStormAndReportsRelinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server (spec/platform.md)")
	}
	dir := t.TempDir()
	seeded := filepath.Join(dir, "seeded")
	once := filepath.Join(dir, "refused.lock")
	hook := filepath.Join(dir, "hook.sh")
	// The first push is the seeder's (admitted, before any writer
	// starts); exactly one later push is refused as the storm's shape,
	// chosen atomically with mkdir since two writers' pushes can reach
	// the hook at once; the rest are admitted.
	script := fmt.Sprintf("#!/bin/sh\nif [ ! -f %[1]s ]; then\n  echo x > %[1]s\n  exit 0\nfi\nif mkdir %[2]s 2>/dev/null; then\n  echo 'seed-admit: rule verify: position 9999: bad_prev: prev b692fec4bc38 does not cite tip 2492338b3552' >&2\n  exit 1\nfi\nexit 0\n", seeded, once)
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "kept")
	// The kept work dir holds the rebuild's published projections, whose
	// builds are locked read-only (projections.md), so the harness's
	// TempDir cleanup could not remove them on a non-root runner:
	// restore the write bits before it runs.
	t.Cleanup(func() {
		_ = filepath.WalkDir(keep, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
	})
	e, code := runEnv(t, "perf", "run", "--history", "1", "--writers", "2", "--hook", hook, "--keep", keep)
	if code != 0 || !e.OK {
		t.Fatalf("the storm lands every writer despite the planted refusal: %d %+v", code, e)
	}
	if e.Result["relinked"] != float64(1) {
		t.Fatalf("one re-link is reported: %v", e.Result["relinked"])
	}
	if e.Result["kept"] != keep {
		t.Fatalf("the kept directory is named: %v", e.Result["kept"])
	}
	works, err := filepath.Glob(filepath.Join(keep, "seed-perf-*"))
	if err != nil || len(works) != 1 {
		t.Fatalf("the work dir is kept under the named path: %v %v", works, err)
	}
	for _, sub := range []string{"remote.git", "writer-000", "writer-001", "seeder"} {
		if _, err := os.Stat(filepath.Join(works[0], sub)); err != nil {
			t.Errorf("%s survives under the kept work dir: %v", sub, err)
		}
	}
	refused, _ := filepath.Glob(filepath.Join(works[0], "writer-*", "refused", "*", "message.txt"))
	if len(refused) != 1 {
		t.Fatalf("the refused tree is kept beside the writer that pushed it: %v", refused)
	}
	for _, half := range []string{"commit", "worktree"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(refused[0]), half, "HEAD")); err != nil {
			t.Errorf("the refused %s half is kept: %v", half, err)
		}
	}
	if b, err := os.ReadFile(refused[0]); err != nil || !strings.Contains(string(b), "position 9999: bad_prev") {
		t.Fatalf("the hook's message is kept: %q %v", b, err)
	}
	// Without --keep nothing is named and nothing is left behind.
	e, code = runEnv(t, "perf", "run", "--history", "1", "--writers", "1", "--hook", "none")
	if code != 0 || !e.OK || e.Result["relinked"] != float64(0) {
		t.Fatalf("a clean storm re-links zero times: %d %+v", code, e)
	}
	if _, ok := e.Result["kept"]; ok {
		t.Fatal("kept is absent when nothing is kept")
	}
}
