package importer

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// FixtureDir is this repository's own v1 state at a named anchor
// (plans/os-cf13fb51.md D6): export.json and seed-state.bundle.
const FixtureDir = "../../fixtures/import/open-seed"

// repoRoot is the repository the cited receipts and plans are read
// from: the checkout this test runs in.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// cloneBundle clones the fixture bundle bare, so the anchors verify
// with no network.
func cloneBundle(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "source.git")
	bundle, err := filepath.Abs(filepath.Join(FixtureDir, "seed-state.bundle"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "clone", "-q", "--bare", bundle, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("clone the bundle: %v: %s", err, out)
	}
	hardenGitRepo(t, dir)
	return dir
}

// hardenGitRepo disables git's detached auto-gc in a repository a test
// created under t.TempDir, the same three settings the gitref and CLI
// suites apply (internal/gitref/fixture_guard_test.go holds every
// creation site to it).
func hardenGitRepo(t testing.TB, repo string) {
	t.Helper()
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"},
		{"core.autocrlf", "false"}, // the ledger is LF-only on every platform (spec/platform.md)
		{"core.eol", "lf"}} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("hardening %s (%s): %v %s", repo, kv[0], err, out)
		}
	}
}

func operatorKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func fixtureExport(t *testing.T) *Export {
	t.Helper()
	e, err := ReadExport(filepath.Join(FixtureDir, "export.json"))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// conformance: plans/os-cf13fb51.md AC2, AC3, AC4 — the real fixture
// imports into an empty ledger with every event admitted, the chain
// verifies from genesis, every contract folds to the state its card
// declares, every v1 name is enrolled at the declared kind and
// suspended, and the manifest is lossless.
func TestRealFixtureImports(t *testing.T) {
	if testing.Short() {
		t.Skip("the real fixture takes a minute")
	}
	src := cloneBundle(t)
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	artDir := filepath.Join(t.TempDir(), "artifacts")
	exp := fixtureExport(t)
	start := time.Now()
	res, err := Run(Options{Export: exp, Source: src, Repo: repoRoot(t), LedgerDir: ledgerDir, ArtifactsDir: artDir, Operator: operatorKey(t), Now: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		Trace: func(phase string, d time.Duration) { t.Logf("%-12s %s", phase, d.Round(time.Millisecond)) }})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Logf("import: %d records, %d artifacts, manifest %s, in %s", res.Records, res.Artifacts, res.ManifestDigest[:12], time.Since(start).Round(time.Millisecond))
	store, err := ledger.OpenReadOnly(ledgerDir)
	if err != nil {
		t.Fatal(err)
	}
	// The chain verifies from genesis under the keyring it enrolled.
	src2, err := Read(exp)
	if err != nil {
		t.Fatal(err)
	}
	var records []*event.Record
	if err := store.Records(func(pos int, rec *event.Record) error { records = append(records, rec); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(records) != res.Records {
		t.Fatalf("the store holds %d records, the import wrote %d", len(records), res.Records)
	}
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	fold := table.FoldRecords(records)
	for _, id := range src2.CardIDs {
		want := src2.Cards[id].State
		s, ok := fold.State(id)
		if !ok {
			t.Errorf("card %s: no contract folded", id)
			continue
		}
		if s.State != want {
			t.Errorf("card %s: folds to %s, the card declares %s", id, s.State, want)
		}
	}
	m := res.Manifest
	if err := Check(m, src2); err != nil {
		t.Errorf("losslessness: %v", err)
	}
	if m.Counts.Records != src2.RecordCount() || m.Counts.Dispositions != m.Counts.Records {
		t.Errorf("counts: %+v against %d records", m.Counts, src2.RecordCount())
	}
	// The manifest is in the artifact store under the digest
	// system.imported cites, and every artifact a disposition names
	// is retrievable.
	art := artifact.Open(artDir)
	if _, err := ParseManifest(art, res.ManifestDigest); err != nil {
		t.Errorf("manifest: %v", err)
	}
	for _, d := range m.Records {
		if d.Artifact != "" {
			if _, err := art.Get(d.Artifact); err != nil {
				t.Errorf("%s: artifact %s: %v", d.Record, d.Artifact[:12], err)
			}
		}
	}
	// Every identity is enrolled at the table's kind and suspended.
	tbl, _ := DefaultTable()
	for _, row := range m.Identities {
		if id, known := tbl.IdentityFor(row.Source); known && id.Kind != row.Kind {
			t.Errorf("%s enrolled as %s, the table says %s", row.Source, row.Kind, id.Kind)
		}
		if row.Suspended <= row.Enrolled {
			t.Errorf("%s: enrolled at %d, suspended at %d", row.Source, row.Enrolled, row.Suspended)
		}
	}
	if _, err := os.Stat(filepath.Join(ledgerDir, "HEAD")); err != nil {
		t.Error(err)
	}
	assertReceiptsStored(t, records, art)
	ring, _, err := keyring.StateAt(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range m.Identities {
		if entry, ok := ring.Get(row.Fingerprint); !ok || entry.Standing != keyring.StandingSuspended {
			t.Errorf("%s is not suspended at the tip", row.Source)
		}
	}
}
