package perfgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conformance: III.C row 4 — the contention benchmark at the target
// scale. The scale profile is pinned (plans/os-a00d3f34.md D4): it
// loads through the same Load the gate uses, its writers are hundreds
// (200 or more), its history is at least the per-PR profile's so the
// two readings are about the same chain, and its attempts ceiling is
// at least the per-PR profile's, since attempts per landed append
// grow with writers (12.5 at 24, 51 measured at 200: a landed writer
// leaves the pool, so the ratio sits well under the writers/2 of a
// pool that never shrinks), so the profile cannot shrink below the
// charter's scale or claim a cheaper loop than the small storm's
// without failing make check.
func TestScaleProfileIsHundredsOfWriters(t *testing.T) {
	scale, err := Load(filepath.Join("..", "..", "perf", "budgets-scale.json"))
	if err != nil {
		t.Fatalf("the scale profile loads: %v", err)
	}
	perPR, err := Load(filepath.Join("..", "..", "perf", "budgets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if scale.Writers < 200 {
		t.Fatalf("the scale profile runs %d writers; hundreds means 200 or more", scale.Writers)
	}
	if scale.History < perPR.History {
		t.Fatalf("the scale history (%d) is smaller than the per-PR history (%d): the readings would be about different chains", scale.History, perPR.History)
	}
	if got, want := scale.Metrics[MetricAttempts].Ceiling, perPR.Metrics[MetricAttempts].Ceiling; got < want {
		t.Fatalf("the attempts ceiling %v is below the per-PR profile's %v: attempts per landed append grow with writers", got, want)
	}
	for _, m := range Required() {
		if !strings.Contains(scale.Metrics[m].Provenance, "202") {
			t.Errorf("%s: the provenance names no date: %q", m, scale.Metrics[m].Provenance)
		}
	}
	// Mutation: the per-PR profile, 24 writers, is not a scale profile.
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "..", "perf", "budgets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "budgets-scale.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	small, err := Load(filepath.Join(dir, "budgets-scale.json"))
	if err != nil {
		t.Fatal(err)
	}
	if small.Writers >= 200 {
		t.Fatal("the mutation did not apply")
	}
}
