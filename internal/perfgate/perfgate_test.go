package perfgate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func budgets() *Budgets {
	m := map[string]Budget{}
	for _, name := range Required() {
		m[name] = Budget{Ceiling: 10, Provenance: "test"}
	}
	return &Budgets{History: 5, Writers: 3, Metrics: m}
}

func reading(v float64) Reading {
	r := Reading{}
	for _, name := range Required() {
		r[name] = v
	}
	return r
}

// The gate's table: within budget passes on one measurement; a miss
// re-measures cold once and passes if the second is within; a miss
// that repeats fails naming both; at most two measurements.
func TestRunReMeasuresColdOnce(t *testing.T) {
	calls, cleaned := 0, 0
	deps := func(seq ...Reading) Deps {
		return Deps{
			Measure:    func() (Reading, error) { r := seq[calls]; calls++; return r, nil },
			CleanCache: func() error { cleaned++; return nil },
		}
	}
	calls, cleaned = 0, 0
	if v, msg, err := Run(budgets(), deps(reading(5))); v != Pass || err != nil || calls != 1 || cleaned != 0 || !strings.Contains(msg, "perf ok") {
		t.Fatalf("within budget passes on one measurement: %v %q %v (calls %d cleaned %d)", v, msg, err, calls, cleaned)
	}
	calls, cleaned = 0, 0
	if v, msg, err := Run(budgets(), deps(reading(50), reading(5))); v != Pass || err != nil || calls != 2 || cleaned != 1 || !strings.Contains(msg, "taking the second") {
		t.Fatalf("a noisy miss passes on a cold second: %v %q %v (calls %d cleaned %d)", v, msg, err, calls, cleaned)
	}
	calls, cleaned = 0, 0
	if v, _, err := Run(budgets(), deps(reading(50), reading(50))); v != Fail || err == nil || calls != 2 || !strings.Contains(err.Error(), "twice") || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("a repeated miss fails naming both and the fix: %v %v (calls %d)", v, err, calls)
	}
	calls = 0
	if v, _, err := Run(budgets(), Deps{Measure: func() (Reading, error) { return nil, errors.New("boom") }}); v != Fail || err == nil {
		t.Fatal("a failed measurement fails")
	}
}

// The budget file is validated: every required metric, a positive
// ceiling, a provenance, a positive history and writer count.
func TestLoadValidates(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "budgets.json")
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	good := `{"history": 50, "writers": 20, "metrics": {"admission_ms": {"ceiling": 1, "provenance": "x"}, "replay_ms": {"ceiling": 1, "provenance": "x"}, "rebuild_ms": {"ceiling": 1, "provenance": "x"}, "contention_ms": {"ceiling": 1, "provenance": "x"}, "attempts_ratio": {"ceiling": 1, "provenance": "x"}}}`
	if b, err := Load(write(good)); err != nil || b.History != 50 || b.Writers != 20 {
		t.Fatalf("a complete file loads: %+v %v", b, err)
	}
	for name, body := range map[string]string{
		"missing metric": `{"history": 50, "writers": 20, "metrics": {"admission_ms": {"ceiling": 1, "provenance": "x"}}}`,
		"no provenance":  strings.Replace(good, `"provenance": "x"`, `"provenance": " "`, 1),
		"zero ceiling":   strings.Replace(good, `"ceiling": 1`, `"ceiling": 0`, 1),
		"no history":     strings.Replace(good, `"history": 50`, `"history": 0`, 1),
		"not json":       `{`,
	} {
		if _, err := Load(write(body)); err == nil {
			t.Errorf("%s must refuse", name)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("an absent file refuses")
	}
}
