package perfgate

// The gate's two remaining refusal arms (plans/os-f262585a.md D1): a
// cache it cannot clean, and a second measurement that fails. Both are
// failures of the gate's own machinery rather than of the tree, so the
// message has to say which, or an operator re-runs a regression that
// was never measured.

import (
	"errors"
	"strings"
	"testing"
)

func TestRunFailsWhenItCannotReMeasureCold(t *testing.T) {
	calls := 0
	v, msg, err := Run(budgets(), Deps{
		Measure:    func() (Reading, error) { calls++; return reading(50), nil },
		CleanCache: func() error { return errors.New("read-only cache directory") },
	})
	if v != Fail || err == nil {
		t.Fatalf("a cache it cannot clean fails the gate: %v %q %v", v, msg, err)
	}
	if !strings.Contains(err.Error(), "cannot re-measure cold") || !strings.Contains(err.Error(), "read-only cache directory") {
		t.Errorf("the message must name the gate's own failure and its cause: %v", err)
	}
	if calls != 1 {
		t.Errorf("the second measurement must not run after the clean failed, got %d measurements", calls)
	}
}

func TestRunFailsWhenTheSecondMeasurementDoes(t *testing.T) {
	calls := 0
	v, _, err := Run(budgets(), Deps{
		Measure: func() (Reading, error) {
			calls++
			if calls == 1 {
				return reading(50), nil
			}
			return nil, errors.New("the history could not be built")
		},
		CleanCache: func() error { return nil },
	})
	if v != Fail || err == nil || !strings.Contains(err.Error(), "could not be built") {
		t.Fatalf("a failed cold re-measurement fails naming the cause: %v %v", v, err)
	}
	if calls != 2 {
		t.Errorf("at most two measurements, ever; got %d", calls)
	}
}

func TestRunWithNoCacheCleanerStillReMeasures(t *testing.T) {
	// CleanCache is optional: a caller with nothing to clean still gets
	// the second measurement rather than an immediate failure.
	calls := 0
	v, msg, err := Run(budgets(), Deps{
		Measure: func() (Reading, error) {
			calls++
			if calls == 1 {
				return reading(50), nil
			}
			return reading(5), nil
		},
	})
	if v != Pass || err != nil || calls != 2 {
		t.Fatalf("a nil cleaner still re-measures: %v %q %v (calls %d)", v, msg, err, calls)
	}
	if !strings.Contains(msg, "taking the second") {
		t.Errorf("the pass says the second reading is the one taken: %q", msg)
	}
}

func TestTheGateNamesEveryMetricOnBothVerdicts(t *testing.T) {
	// The numbers are visible on success as on failure, so a reviewer
	// reading a green run still sees the margins.
	_, msg, err := Run(budgets(), Deps{Measure: func() (Reading, error) { return reading(5), nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range Required() {
		if !strings.Contains(msg, m) {
			t.Errorf("the passing line omits %s: %q", m, msg)
		}
	}
	_, _, err = Run(budgets(), Deps{
		Measure:    func() (Reading, error) { return reading(50), nil },
		CleanCache: func() error { return nil },
	})
	if err == nil {
		t.Fatal("a repeated miss fails")
	}
	for _, m := range Required() {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("the failure omits %s: %v", m, err)
		}
	}
}
