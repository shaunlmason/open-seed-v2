package redteam

import (
	"os"
	"path/filepath"
	"testing"
)

// The loader's own edge branches: a missing clause id, a read failure,
// a residual with no name, and Clause's not-found return — so the table
// validators are covered where the corpus drills do not reach.
func TestCeilingLoaderEdges(t *testing.T) {
	c, err := LoadCeiling("testdata/ceiling.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Clause("no-such-clause"); ok {
		t.Error("Clause must report a miss for an unknown id")
	}

	if _, err := LoadCeiling(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("a missing ceiling file must fail to load")
	}
	if _, err := LoadResiduals(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("a missing residual file must fail to load")
	}

	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "t.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, body := range map[string]string{
		"no id":    `{"clauses": [{"id": "", "kind": "prohibition", "text": "t", "sides": ["ledger"], "vocabulary": "v"}]}`,
		"bad json": `{"clauses": [`,
	} {
		if _, err := LoadCeiling(write(body)); err == nil {
			t.Errorf("ceiling %q must refuse", name)
		}
	}
	for name, body := range map[string]string{
		"no name":  `{"residuals": [{"name": "", "why": "w", "inflicts": "i", "stands_in_the_way": "s"}]}`,
		"bad json": `{"residuals": [`,
	} {
		if _, err := LoadResiduals(write(body)); err == nil {
			t.Errorf("residuals %q must refuse", name)
		}
	}
}
