package simulate

// Unit coverage for the helpers the single happy-path E2E does not
// exercise: the clock defaults, the catalog draw, key writing, the
// repository builder, and the fold's error paths when the remote is
// unreachable. These keep the package's own contract honest without a
// full deployment.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	var c Config
	if c.now().IsZero() {
		t.Error("now() defaults to a real instant")
	}
	set := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := (Config{Now: set}).now(); !got.Equal(set) {
		t.Errorf("now() honours a set instant, got %v", got)
	}
	if (Config{WorkDir: "/x"}).workRoot() != "/x" {
		t.Error("workRoot() returns WorkDir")
	}
}

func TestPostureName(t *testing.T) {
	if postureName(true) != "enforced-self-hosted" || postureName(false) != "cooperative" {
		t.Error("postureName maps the two postures")
	}
}

func TestCatalogRotationAndEdges(t *testing.T) {
	if len(catalog(0)) != len(shipped) {
		t.Fatal("catalog returns the whole shipped set")
	}
	// A negative seed still rotates deterministically into range.
	neg := catalog(-1)
	if len(neg) != len(shipped) {
		t.Fatal("a negative seed still yields the whole set")
	}
	// A large seed wraps.
	if catalog(int64(len(shipped)))[0].name != catalog(0)[0].name {
		t.Error("the draw wraps at len(shipped)")
	}
}

func TestTrim(t *testing.T) {
	for in, want := range map[string]string{"x\n": "x", "y \r\n": "y", "z": "z", "": ""} {
		if got := trim(in); got != want {
			t.Errorf("trim(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestKeyAtWritesAndFingerprints(t *testing.T) {
	dir := t.TempDir()
	path, pub, fp, err := keyAt(dir, "impl", 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the key file must exist: %v", err)
	}
	if pub == "" || fp == "" {
		t.Error("the public hex and fingerprint are returned")
	}
	// Deterministic in the seed.
	_, pub2, fp2, _ := keyAt(t.TempDir(), "impl", 12)
	if pub2 != pub || fp2 != fp {
		t.Error("keyAt is deterministic in the seed")
	}
	// An unwritable directory is an error.
	if _, _, _, err := keyAt(filepath.Join(dir, "nope", "deeper"), "x", 1); err == nil {
		t.Error("keyAt errors when the directory cannot be written")
	}
}

func TestBuildRepoProducesThreeCommits(t *testing.T) {
	d := &deployment{dir: t.TempDir()}
	r, err := d.buildRepo("c-1", shipped[0])
	if err != nil {
		t.Fatal(err)
	}
	if r.base == "" || r.spec == "" || r.head == "" || r.src == "" {
		t.Fatalf("buildRepo returns the source and three commits: %+v", r)
	}
	if r.base == r.head {
		t.Error("the head applies the patch, so it differs from base")
	}
	// The acceptance spec is present at the spec commit.
	if _, err := os.Stat(filepath.Join(r.src, "accept.md")); err != nil {
		t.Errorf("accept.md must be written: %v", err)
	}
}

func TestFoldErrorsOnUnreachableRemote(t *testing.T) {
	// A deployment whose remote does not exist: materialize, records and
	// stateOf all take their error paths, and stateOf answers "unknown".
	d := &deployment{dir: t.TempDir(), remote: filepath.Join(t.TempDir(), "no-such-remote.git")}
	if _, err := d.materialize(); err == nil {
		t.Error("materialize must fail on an unreachable remote")
	}
	if _, err := d.records(); err == nil {
		t.Error("records must fail on an unreachable remote")
	}
	if got := d.stateOf("c-1"); got != "unknown" {
		t.Errorf("stateOf answers unknown when the chain cannot be read, got %q", got)
	}
}
