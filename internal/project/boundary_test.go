//go:build !windows

// These drills observe file modes and the umask, which Windows does
// not enforce (spec/platform.md).

package project_test

// The write-boundary drills (plans/os-8d5e9c45.md step 3; conformance
// III.D "no code path writes a projection directly"): published trees
// are locked, replacement operations fail at the operating system,
// the engine's own window relocks after every publication, and the
// documented mode walk plus one rebuild is the deletion story.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)

// unlockWalk is the documented mode walk (spec/projections.md
// "Write boundary"): directories back to 0755. The engine runs the
// same walk itself; a human deleting output by hand needs exactly
// this.
func TestPublicationLocksTheTree(t *testing.T) {
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(out, "roster")
	build, err := project.Current(out, "roster")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{out, root, filepath.Join(root, "builds"), build} {
		if m := mode(t, d); m != 0o555 {
			t.Fatalf("directory %s must relock to 0555, got %o", d, m)
		}
	}
	for _, f := range []string{filepath.Join(root, project.CurrentFile), filepath.Join(build, project.RosterFile), filepath.Join(build, project.StampFile)} {
		if m := mode(t, f); m != 0o444 {
			t.Fatalf("file %s must publish at 0444, got %o", f, m)
		}
	}

	// The engine's own paths stay green over locked trees: a repeat
	// rebuild (same-id discard) and a fresh rebuild after the
	// documented deletion walk, relocked both times.
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatalf("a repeat rebuild must reopen and close its own window: %v", err)
	}
	if m := mode(t, root); m != 0o555 {
		t.Fatalf("the root must relock after a repeat rebuild, got %o", m)
	}
	before := treeHash(t, out)
	unlockAndRemove(t, out)
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if treeHash(t, out) != before {
		t.Fatal("the mode walk plus one rebuild must reproduce the tree byte-identically")
	}
	if m := mode(t, filepath.Join(root, "builds")); m != 0o555 {
		t.Fatalf("modes must relock after the recovery rebuild, got %o", m)
	}
}

func TestLockedTreeRefusesReplacement(t *testing.T) {
	requireModeEnforcement(t)
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	build, err := project.Current(out, "roster")
	if err != nil {
		t.Fatal(err)
	}
	view := filepath.Join(build, project.RosterFile)
	before := treeHash(t, out)

	forged := filepath.Join(t.TempDir(), "forged.json")
	if err := os.WriteFile(forged, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(forged, view); err == nil {
		t.Fatal("renaming over a published view must fail outside the engine")
	}
	if err := os.Remove(view); err == nil {
		t.Fatal("unlinking a published view must fail outside the engine")
	}
	if err := os.WriteFile(filepath.Join(build, "evil.json"), []byte("{}"), 0o644); err == nil {
		t.Fatal("creating inside a published build must fail outside the engine")
	}
	cur := filepath.Join(out, "roster", project.CurrentFile)
	forged2 := filepath.Join(t.TempDir(), "CURRENT.forged")
	if err := os.WriteFile(forged2, []byte("99999999-ffffffffffff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(forged2, cur); err == nil {
		t.Fatal("repointing CURRENT must fail outside the engine")
	}
	// Rename permission lives in the parent, so the output root locks
	// too: a whole projection root cannot be renamed away (review
	// finding on #112).
	rosterRoot := filepath.Join(out, "roster")
	if err := os.Rename(rosterRoot, rosterRoot+".bak"); err == nil {
		t.Fatal("renaming a projection root away must fail outside the engine")
	}
	if treeHash(t, out) != before {
		t.Fatal("refused operations must leave the published bytes unchanged")
	}
	if got, err := project.Current(out, "roster"); err != nil || got != build {
		t.Fatalf("the resolved view must be unchanged: %s (%v)", got, err)
	}
}

// A failed publication relocks its window on the error path (review
// finding on #112): only a killed process leaves a window open.
func TestWindowRelocksOnError(t *testing.T) {
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	bad := []project.Projection{{Name: "probe", Build: func([]*event.Record, project.Inputs) (map[string][]byte, error) {
		return map[string][]byte{"../escape.json": []byte("{}")}, nil
	}}}
	if _, err := project.Rebuild(dir, out, bad, resolve); err == nil {
		t.Fatal("an escaping builder filename must fail the rebuild")
	}
	for _, d := range []string{out, filepath.Join(out, "probe"), filepath.Join(out, "probe", "builds")} {
		if m := mode(t, d); m != 0o555 {
			t.Fatalf("a failed publication must relock %s, got %o", d, m)
		}
	}
}

// The process umask cannot weaken the published modes (review finding
// on #112): every published mode is set by explicit chmod.
func TestUmaskCannotWeakenModes(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(out, "roster")
	build, err := project.Current(out, "roster")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{out, root, filepath.Join(root, "builds"), build} {
		if m := mode(t, d); m != 0o555 {
			t.Fatalf("umask must not weaken directory %s, got %o", d, m)
		}
	}
	for _, f := range []string{filepath.Join(root, project.CurrentFile), filepath.Join(build, project.RosterFile)} {
		if m := mode(t, f); m != 0o444 {
			t.Fatalf("umask must not weaken file %s, got %o", f, m)
		}
	}
}
