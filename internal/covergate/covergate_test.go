package covergate_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/covergate"
)

// recorder counts collections and records the ORDER of effects, which
// is what lets the cache-clean assertion be about placement rather
// than mere occurrence: a clean before the first reading or after the
// second would satisfy "it was called" and protect nothing.
type recorder struct {
	readings []covergate.Reading
	errs     []error
	calls    int
	order    []string
	cleanErr error
}

func (r *recorder) deps() covergate.Deps {
	return covergate.Deps{
		Collect: func() (covergate.Reading, error) {
			r.order = append(r.order, "collect")
			i := r.calls
			r.calls++
			if i >= len(r.readings) {
				// A third collection is a defect, not a scenario: the
				// drill fails loudly rather than growing the fixture.
				return covergate.Reading{}, fmt.Errorf("collection %d attempted; at most two are legal", i+1)
			}
			var err error
			if i < len(r.errs) {
				err = r.errs[i]
			}
			return r.readings[i], err
		},
		CleanCache: func() error {
			r.order = append(r.order, "clean")
			return r.cleanErr
		},
	}
}

func reading(total float64) covergate.Reading {
	return covergate.Reading{Total: total, Raw: fmt.Sprintf("%.1f%%", total)}
}

// The whole decision table, and the collection COUNT for each row: a
// gate that always re-collects would double the cost of every green
// run, so "not taken" is asserted rather than assumed.
func TestTheDecisionTable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readings []covergate.Reading
		want     covergate.Verdict
		calls    int
		says     string
	}{
		{"at the gate passes on one collection", []covergate.Reading{reading(90.0)}, covergate.Pass, 1, "coverage 90.0% (gate 90%)"},
		{"above the gate passes on one collection", []covergate.Reading{reading(91.2)}, covergate.Pass, 1, "coverage 91.2% (gate 90%)"},
		{"low then good passes, naming both", []covergate.Reading{reading(61.5), reading(91.1)}, covergate.Pass, 2, "os-cafba959"},
		{"low twice fails, printing both", []covergate.Reading{reading(87.8), reading(87.9)}, covergate.Fail, 2, "87.8%, then 87.9% on a cold re-collection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{readings: tc.readings}
			got, msg, err := covergate.Run(90.0, r.deps())
			if got != tc.want {
				t.Fatalf("verdict %v, want %v (err %v)", got, tc.want, err)
			}
			if r.calls != tc.calls {
				t.Errorf("%d collections, want %d — %v", r.calls, tc.calls, r.order)
			}
			said := msg
			if err != nil {
				said = err.Error()
			}
			if !strings.Contains(said, tc.says) {
				t.Errorf("must say %q, said %q", tc.says, said)
			}
		})
	}
}

// The happy path's line is byte-identical to the Makefile recipe this
// replaces, because the flavor-test core-gate-independence check diffs
// `make check` output on a green tree.
func TestTheGreenLineIsByteIdentical(t *testing.T) {
	r := &recorder{readings: []covergate.Reading{{Total: 91.2, Raw: "91.2%"}}}
	_, msg, err := covergate.Run(90.0, r.deps())
	if err != nil {
		t.Fatal(err)
	}
	const want = "check-next: gofmt/vet/build/test ok; coverage 91.2% (gate 90%)"
	if msg != want {
		t.Fatalf("green output drifted:\n got %q\nwant %q", msg, want)
	}
}

// The cache clean happens BETWEEN the two readings — not before the
// first, not after the second. Placement is the whole point: go test
// caches a package's coverage contribution, so a warm re-run replays
// the lost profile at the same number and would confirm the bad
// reading rather than test it (card os-4eaf8b13).
func TestTheSecondReadingIsCold(t *testing.T) {
	r := &recorder{readings: []covergate.Reading{reading(61.5), reading(91.1)}}
	if _, _, err := covergate.Run(90.0, r.deps()); err != nil {
		t.Fatal(err)
	}
	want := []string{"collect", "clean", "collect"}
	if strings.Join(r.order, ",") != strings.Join(want, ",") {
		t.Fatalf("effect order %v, want %v — a clean anywhere else protects nothing", r.order, want)
	}
}

// A green run never cleans the cache: the fast path must stay fast,
// and clearing it would make every subsequent run cold.
func TestAGreenRunNeverCleansTheCache(t *testing.T) {
	r := &recorder{readings: []covergate.Reading{reading(91.2)}}
	if _, _, err := covergate.Run(90.0, r.deps()); err != nil {
		t.Fatal(err)
	}
	for _, step := range r.order {
		if step == "clean" {
			t.Fatalf("a passing run cleaned the cache: %v", r.order)
		}
	}
}

// A failing suite is never re-collected: the failure IS the answer,
// and re-running it would double the wait before reporting it.
func TestAFailingSuiteIsNotReCollected(t *testing.T) {
	r := &recorder{
		readings: []covergate.Reading{{Output: "--- FAIL: TestThing"}},
		errs:     []error{covergate.ErrSuiteFailed},
	}
	got, out, err := covergate.Run(90.0, r.deps())
	if got != covergate.Fail || !errors.Is(err, covergate.ErrSuiteFailed) {
		t.Fatalf("a failing suite fails the gate: %v %v", got, err)
	}
	if r.calls != 1 {
		t.Errorf("%d collections: a failing suite is not re-run — %v", r.calls, r.order)
	}
	if !strings.Contains(out, "--- FAIL") {
		t.Errorf("the suite output is surfaced on failure: %q", out)
	}
}

// At most two collections, ever. A loop would turn a genuine
// regression into a slow one and hide a systematic failure behind
// eventual success.
func TestNeverAThirdCollection(t *testing.T) {
	r := &recorder{readings: []covergate.Reading{reading(10.0), reading(20.0)}}
	got, _, err := covergate.Run(90.0, r.deps())
	if got != covergate.Fail {
		t.Fatalf("two low readings fail: %v", got)
	}
	if err == nil || strings.Contains(err.Error(), "at most two are legal") {
		t.Fatalf("a third collection was attempted: %v", err)
	}
	if r.calls != 2 {
		t.Fatalf("%d collections, want exactly 2: %v", r.calls, r.order)
	}
}

// Each reading is described as what it WAS. Only the second is cold —
// nothing cleans the cache before the first — so calling both cold
// would overstate the evidence to an unattended agent deciding whether
// a regression was independently reproduced (review finding on #201).
// This gate exists to stop exactly that kind of overstatement, so its
// own diagnostics must not commit one.
func TestOnlyTheRetryIsDescribedAsCold(t *testing.T) {
	r := &recorder{readings: []covergate.Reading{reading(61.5), reading(62.0)}}
	_, _, err := covergate.Run(90.0, r.deps())
	if err == nil {
		t.Fatal("two low readings fail")
	}
	if strings.Contains(err.Error(), "two cold") {
		t.Errorf("the first reading is not cold and must not be called so: %v", err)
	}
	if !strings.Contains(err.Error(), "cold re-collection") {
		t.Errorf("the second reading IS cold and the message says which: %v", err)
	}
	// The pass-after-loss line has the same obligation.
	r2 := &recorder{readings: []covergate.Reading{reading(61.5), reading(91.1)}}
	_, msg, err := covergate.Run(90.0, r2.deps())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg, "two cold") {
		t.Errorf("the pass-after-loss line must not overstate either: %q", msg)
	}
	if !strings.Contains(msg, "cold re-collection") {
		t.Errorf("it names which reading was cold: %q", msg)
	}
}

// A cache clean that fails is a refusal, not a silent warm re-run:
// the second reading's coldness is what makes it evidence.
func TestAFailedCleanRefusesRatherThanReadingWarm(t *testing.T) {
	r := &recorder{readings: []covergate.Reading{reading(61.5), reading(91.1)}, cleanErr: errors.New("permission denied")}
	got, _, err := covergate.Run(90.0, r.deps())
	if got != covergate.Fail || err == nil {
		t.Fatalf("a failed clean must refuse: %v %v", got, err)
	}
	if !strings.Contains(err.Error(), "cold") {
		t.Errorf("the refusal says why coldness matters: %v", err)
	}
	if r.calls != 1 {
		t.Errorf("%d collections: the warm second reading must not be taken — %v", r.calls, r.order)
	}
}

// The verdict prints as a word, because it appears in the tool's own
// diagnostics and "0"/"1" would tell a reader nothing.
func TestVerdictReadsAsAWord(t *testing.T) {
	if covergate.Pass.String() != "pass" || covergate.Fail.String() != "fail" {
		t.Fatalf("verdicts: %q %q", covergate.Pass, covergate.Fail)
	}
}

// The second collection can fail its suite too, and that is the
// failure reported — not a coverage verdict derived from a run that
// did not finish.
func TestASecondCollectionThatFailsIsReportedAsAFailure(t *testing.T) {
	r := &recorder{
		readings: []covergate.Reading{reading(61.5), {Output: "--- FAIL: TestLate"}},
		errs:     []error{nil, covergate.ErrSuiteFailed},
	}
	got, out, err := covergate.Run(90.0, r.deps())
	if got != covergate.Fail || !errors.Is(err, covergate.ErrSuiteFailed) {
		t.Fatalf("a failing second suite fails the gate: %v %v", got, err)
	}
	if !strings.Contains(out, "--- FAIL") {
		t.Errorf("its output is surfaced too: %q", out)
	}
}

// CleanCache is optional: a caller with no cache to clear (a drill, a
// one-shot) still gets the two-reading decision rather than a panic.
func TestAbsentCleanCacheIsLegal(t *testing.T) {
	calls := 0
	got, _, err := covergate.Run(90.0, covergate.Deps{
		Collect: func() (covergate.Reading, error) {
			calls++
			if calls == 1 {
				return reading(61.5), nil
			}
			return reading(91.1), nil
		},
	})
	if got != covergate.Pass || err != nil {
		t.Fatalf("a nil CleanCache must not panic: %v %v", got, err)
	}
	if calls != 2 {
		t.Errorf("%d collections, want 2", calls)
	}
}
