package project_test

// The helpers the projection drills share: a locked temp output tree,
// its unlock and removal, a mode read, and the mode-enforcement guard.
// Unconstrained, so every drill builds on every platform; the drills
// that observe modes are Unix-only (boundary_test.go).

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func unlockWalk(path string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o755)
		}
		return nil
	})
}

// lockedTempOut hands a test an output root inside its TempDir and
// unlocks it at cleanup, before testing's own RemoveAll runs: locked
// publication would otherwise wedge the framework's cleanup on an
// unprivileged runner.
func lockedTempOut(t *testing.T, name string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	t.Cleanup(func() {
		if err := unlockWalk(out); err != nil {
			t.Errorf("cleanup unlock: %v", err)
		}
	})
	return out
}

func unlockAndRemove(t *testing.T, path string) {
	t.Helper()
	if err := unlockWalk(path); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// requireModeEnforcement skips drills that assert the operating system
// refuses a write: uid 0 bypasses permission checks (CAP_DAC_OVERRIDE),
// so the refusals are only observable on an unprivileged runner, which
// is what CI provides. The locking itself (modes on disk, the engine's
// relock) is asserted unconditionally in TestPublicationLocksTheTree.
func requireModeEnforcement(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("mode enforcement is not observable as root; CI runs unprivileged")
	}
}
