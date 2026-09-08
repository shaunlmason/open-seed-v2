package project

// Internal drill for the write window's failure path (review finding
// on #118): a partial open must roll itself back. The failure is
// provoked structurally (builds/ occupied by a regular file), not by
// modes, so the drill holds for any runner, root included.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDirsRollsBackOnPartialFailure(t *testing.T) {
	out := t.TempDir()
	root := filepath.Join(out, "p")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, buildsDir), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	// TempDir cleanup unlinks through root, so unlock it again after
	// the drill whatever mode the code under test left it in.
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	err := publish(root, "00000001-aaaaaaaaaaaa-v1", map[string][]byte{"f": []byte("y")})
	if err == nil {
		t.Fatal("publish must fail when builds/ is occupied by a regular file")
	}
	info, statErr := os.Stat(root)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("a failed open must leave the projection root relocked, got %v", info.Mode().Perm())
	}
}
