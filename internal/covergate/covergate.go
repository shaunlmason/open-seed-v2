// Package covergate decides whether a coverage reading may be
// believed (plans/os-cafba959.md). The gate's collection is lossy at a
// low rate: cmd/go's mergeCoverProfile drops a package's profile
// fragment SILENTLY when the fragment file is missing or zero-length
// ("Test did not create profile, which is OK"), with no error, no
// message, and `ok` still printed for every package. The merged total
// then reads far below truth on a tree that is fine.
//
// That presents as a large coverage regression, which is exactly what
// a real one looks like. The correct response — re-collect cold once,
// then treat a second failure as real — is a rule an unattended agent
// must apply against its own instinct, so it is held here instead.
//
// The design verifies the NUMBER, not the collection, and re-collects
// ONLY when the reading is below the threshold. That is chosen over
// every structural alternative for one reason: it cannot false-alarm.
// It engages only where the gate would already have failed, so a
// healthy tree never pays for it and never trips over it.
package covergate

import (
	"errors"
	"fmt"
)

// Verdict is the gate's answer.
type Verdict int

const (
	// Pass means the tree meets the gate.
	Pass Verdict = iota
	// Fail means it does not, or the suite itself failed.
	Fail
)

func (v Verdict) String() string {
	if v == Pass {
		return "pass"
	}
	return "fail"
}

// Reading is one collection: the parsed percentage, the raw string the
// tool printed (kept verbatim so the gate's own line cannot drift from
// go's formatting), and the captured output shown only on failure.
type Reading struct {
	Total float64
	Raw   string
	// Output is the tool output, surfaced only when the suite failed:
	// `check` output must be byte-stable run to run, and go's per-test
	// timings are not.
	Output string
}

// ErrSuiteFailed marks a collection whose test run failed, as opposed
// to one that merely read low. A failing suite is never re-collected:
// the failure is the answer.
var ErrSuiteFailed = errors.New("the test suite failed")

// Deps are the injected effects. Collect runs the suite and reads the
// merged total; CleanCache invalidates go's test cache. Both are
// parameters so the decision can be drilled without running a suite,
// and so the cache clean can be ASSERTED to happen between the two
// readings rather than assumed.
type Deps struct {
	Collect    func() (Reading, error)
	CleanCache func() error
}

// Run applies the gate.
//
//	first reading | second (cold) | verdict
//	≥ gate        | not taken     | pass, output unchanged
//	< gate        | ≥ gate        | pass, naming the lossy collection
//	< gate        | < gate        | fail, printing both readings
//	suite failed  | not taken     | fail on the test failure
//
// At most two collections, ever. The card's own rule is "re-run once,
// then treat a second failure as real": a loop would turn a genuine
// regression into a slow one and hide a systematic failure behind
// eventual success.
func Run(gate float64, d Deps) (Verdict, string, error) {
	first, err := d.Collect()
	if err != nil {
		return Fail, first.Output, err
	}
	if first.Total >= gate {
		// The happy path's output is byte-identical to the target this
		// replaces: the flavor-test core-gate-independence check diffs
		// `make check` output on a green tree.
		return Pass, okLine(first.Raw, gate), nil
	}
	// The second reading must be COLD. go test caches a package's
	// coverage contribution, so a warm re-run replays the lost profile
	// at the SAME number (card os-4eaf8b13, folded in here): a retry
	// without this would make the gate more confident of a false
	// regression than it is today.
	if d.CleanCache != nil {
		if err := d.CleanCache(); err != nil {
			return Fail, "", fmt.Errorf("cannot re-collect cold: %w", err)
		}
	}
	second, err := d.Collect()
	if err != nil {
		return Fail, second.Output, err
	}
	if second.Total >= gate {
		return Pass, fmt.Sprintf(
			"check-next: the first collection read %s and a cold re-collection read %s.\n"+
				"check-next: a collection that loses a package's profile fragment reads low and says nothing (card os-cafba959); taking the second.\n"+
				"%s", first.Raw, second.Raw, okLine(second.Raw, gate)), nil
	}
	// Each reading is described as what it actually was. Only the
	// SECOND is cold: nothing cleans the cache before the first, which
	// therefore reuses whatever the caller's cache held. Calling both
	// cold would overstate the evidence to the one reader this gate
	// exists to protect — an unattended agent deciding whether a
	// regression was independently reproduced (review finding on #201).
	return Fail, "", fmt.Errorf(
		"coverage %s, then %s on a cold re-collection: both below the %g%% gate (docs/build-plan.md §0)",
		first.Raw, second.Raw, gate)
}

// okLine is the gate's one success line, byte-identical to the
// Makefile recipe it replaces.
func okLine(raw string, gate float64) string {
	return fmt.Sprintf("check-next: gofmt/vet/build/test ok; coverage %s (gate %g%%)", raw, gate)
}
