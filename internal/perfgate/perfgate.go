// Package perfgate budgets and tracks the ledger's performance
// (SEED-NEXT.md III.A row 12, III.C row 4; plans/os-7508ab9e.md D6):
// admission latency, replay time, projection rebuild time, and a
// contention storm's wall time and its attempts ratio — five metrics,
// each measured against a representative generated history and held
// to a ceiling from a checked-in budget file. A miss
// is re-measured cold once before it fails, the coverage gate's rule
// for the same reason: one noisy reading must not fail a healthy tree,
// and a real regression fails twice.
package perfgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Metric names, the vocabulary the budget file and the measurement
// share.
const (
	MetricAdmission  = "admission_ms"   // one admit.Check at the tip of the history, averaged
	MetricReplay     = "replay_ms"      // VerifyFromGenesis over the history
	MetricRebuild    = "rebuild_ms"     // RebuildWith of every registered projection
	MetricContention = "contention_ms"  // wall time of the storm at the declared writer count
	MetricAttempts   = "attempts_ratio" // the storm's attempts per landed append
)

// Budget is one metric's ceiling and where it came from.
type Budget struct {
	Ceiling    float64 `json:"ceiling"`
	Provenance string  `json:"provenance"`
}

// Budgets is the checked-in file: the history size the ceilings were
// set against, the storm's writer count, and the ceilings.
type Budgets struct {
	History int               `json:"history"`
	Writers int               `json:"writers"`
	Metrics map[string]Budget `json:"metrics"`
}

// Required is the metric set a budget file must carry.
func Required() []string {
	return []string{MetricAdmission, MetricReplay, MetricRebuild, MetricContention, MetricAttempts}
}

// Load reads and validates the budget file: every required metric with
// a positive ceiling and a non-empty provenance, a positive history
// size and writer count. A ceiling with no provenance is a number
// nobody can re-derive, which is how a budget gets raised silently.
func Load(path string) (*Budgets, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out Budgets
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("the budget file does not parse: %v", err)
	}
	if out.History <= 0 || out.Writers <= 0 {
		return nil, errors.New("the budget file declares a positive history size and writer count")
	}
	for _, m := range Required() {
		bud, ok := out.Metrics[m]
		if !ok {
			return nil, fmt.Errorf("the budget file carries no ceiling for %s", m)
		}
		if bud.Ceiling <= 0 {
			return nil, fmt.Errorf("%s: a ceiling is a positive number, got %v", m, bud.Ceiling)
		}
		if strings.TrimSpace(bud.Provenance) == "" {
			return nil, fmt.Errorf("%s: a ceiling carries its provenance (when, against what, with what headroom)", m)
		}
	}
	return &out, nil
}

// Reading is one measurement of every metric.
type Reading map[string]float64

// Verdict is the gate's answer.
type Verdict int

const (
	Pass Verdict = iota
	Fail
)

// Deps are the injected effects: Measure runs the benchmarks,
// CleanCache invalidates whatever a warm second reading would reuse.
type Deps struct {
	Measure    func() (Reading, error)
	CleanCache func() error
}

// Run applies the gate: measure; if every metric is within its ceiling
// pass; otherwise clean and re-measure once, and fail only if a miss
// repeats. At most two measurements, ever. The message names every
// metric with its reading and ceiling, so the numbers are visible on
// success as on failure.
func Run(b *Budgets, d Deps) (Verdict, string, error) {
	first, err := d.Measure()
	if err != nil {
		return Fail, "", err
	}
	if misses := over(b, first); len(misses) == 0 {
		return Pass, line(b, first, ""), nil
	} else {
		if d.CleanCache != nil {
			if err := d.CleanCache(); err != nil {
				return Fail, "", fmt.Errorf("cannot re-measure cold: %w", err)
			}
		}
		second, err := d.Measure()
		if err != nil {
			return Fail, "", err
		}
		if again := over(b, second); len(again) == 0 {
			return Pass, line(b, second, fmt.Sprintf("check-next: the first measurement missed %s and a cold re-measurement did not; taking the second.\n", strings.Join(misses, ","))), nil
		} else {
			return Fail, "", fmt.Errorf("perf over budget twice: first %s, cold %s (%s); fix the regression or raise the ceiling in perf/budgets.json with its provenance", strings.Join(misses, ","), strings.Join(again, ","), describe(b, second))
		}
	}
}

func over(b *Budgets, r Reading) []string {
	var out []string
	for _, m := range Required() {
		if r[m] > b.Metrics[m].Ceiling {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

func describe(b *Budgets, r Reading) string {
	parts := make([]string, 0, len(Required()))
	for _, m := range Required() {
		parts = append(parts, fmt.Sprintf("%s %.3g/%.3g", m, r[m], b.Metrics[m].Ceiling))
	}
	return strings.Join(parts, " ")
}

func line(b *Budgets, r Reading, prefix string) string {
	return fmt.Sprintf("%scheck-next: perf ok; %s (history %d, writers %d)", prefix, describe(b, r), b.History, b.Writers)
}
