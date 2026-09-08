package transition_test

// The tier table drills (plans/os-be12ac16.md; spec/tiers.md): the
// code table mirrors the spec's rows in both directions, the filing
// check validates tier and budget against their tables byte for byte,
// and the plan gate reads the table with the strictest row for a tier
// it does not know.

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// tierRows parses the spec's table: | `tier` | `yes|no` | `yes|no` | `yes|no` |.
func tierRows(spec []byte) map[string]transition.TierRow {
	row := regexp.MustCompile("(?m)^\\| `([a-z]+)` \\| `(yes|no)` \\| `(yes|no)` \\| `(yes|no)` \\| `(L[123])` \\|$")
	out := map[string]transition.TierRow{}
	for _, m := range row.FindAllStringSubmatch(string(spec), -1) {
		out[m[1]] = transition.TierRow{PlanRequired: m[2] == "yes", SealedChecksRequired: m[3] == "yes", HumanReview: m[4] == "yes",
			Independence: transition.Level(m[5])}
	}
	return out
}

// tierMismatches compares the spec's rows against the code table in
// both directions: a spec row the code lacks or disagrees with, and a
// code row the spec lacks.
func tierMismatches(spec []byte) []string {
	var out []string
	rows := tierRows(spec)
	if len(rows) == 0 {
		return []string{"no tier rows parsed from the spec"}
	}
	for name, want := range rows {
		got, ok := transition.Tier(name)
		if !ok {
			out = append(out, fmt.Sprintf("tier %q is in the spec table but not the code table", name))
			continue
		}
		if got != want {
			out = append(out, fmt.Sprintf("tier %q: code says %+v, spec says %+v", name, got, want))
		}
	}
	for _, name := range transition.Tiers() {
		if _, ok := rows[name]; !ok {
			out = append(out, fmt.Sprintf("tier %q is governed by code but missing from the spec table", name))
		}
	}
	return out
}

// conformance: AC4 — the spec-mirror pin, the TestBudgetClassTableMirrorsSpec
// shape: the two tables cannot drift one-sidedly.
func TestTierTableMirrorsSpec(t *testing.T) {
	b, err := os.ReadFile("../../spec/tiers.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range tierMismatches(b) {
		t.Error(m)
	}
	if _, ok := transition.Tier("wizard"); ok {
		t.Error("an unknown tier must have no row")
	}
	if got := transition.Tiers(); strings.Join(got, ",") != "critical,standard,trivial" {
		t.Fatalf("the vocabulary is the three charter tiers, sorted: %v", got)
	}
}

// conformance: AC4's second half — the pin fails in either direction:
// a planted extra spec row, a planted extra code row.
func TestTierPinFailsInEitherDirection(t *testing.T) {
	b, err := os.ReadFile("../../spec/tiers.md")
	if err != nil {
		t.Fatal(err)
	}
	planted := append([]byte{}, b...)
	planted = append(planted, []byte("\n| `wizard` | `no` | `no` | `no` | `L1` |\n")...)
	if ms := tierMismatches(planted); len(ms) != 1 || !strings.Contains(ms[0], `"wizard" is in the spec table but not the code table`) {
		t.Fatalf("a spec row the code lacks fails the pin: %v", ms)
	}
	disagreeing := []byte(strings.Replace(string(b), "| `trivial` | `no` | `no` | `no` | `L1` |", "| `trivial` | `yes` | `no` | `no` | `L1` |", 1))
	if ms := tierMismatches(disagreeing); len(ms) != 1 || !strings.Contains(ms[0], `tier "trivial": code says`) {
		t.Fatalf("a spec row the code disagrees with fails the pin: %v", ms)
	}
	restore := transition.InjectTier("wizard", transition.TierRow{})
	defer restore()
	if ms := tierMismatches(b); len(ms) != 1 || !strings.Contains(ms[0], `"wizard" is governed by code but missing from the spec table`) {
		t.Fatalf("a code row the spec lacks fails the pin: %v", ms)
	}
}

// conformance: AC1, AC2 — the filing check validates tier and budget
// against their tables, exactly, after presence.
func TestCompletenessValidatesTheVocabularies(t *testing.T) {
	filed := func(tier, budget string) error {
		return transition.CheckCompleteness("intent.filed", "c-1",
			[]byte(fmt.Sprintf(`{"intent": "x", "tier": %q, "budget": %q, "routing": "core"}`, tier, budget)))
	}
	for _, tier := range []string{"wizard", "Trivial", "trivial ", "TRIVIAL", "standard-ish"} {
		var ve *transition.VocabularyError
		err := filed(tier, "small")
		if !errors.As(err, &ve) || ve.Field != "tier" || ve.Value != tier || ve.Verb != "intent.filed" || ve.Subject != "c-1" {
			t.Fatalf("tier %q refuses as a vocabulary refusal naming the field and the value: %v", tier, err)
		}
		if msg := err.Error(); !strings.Contains(msg, "critical, standard, trivial") || !strings.Contains(msg, tier) {
			t.Fatalf("tier %q: the refusal names the value and the members: %s", tier, msg)
		}
	}
	var inc *transition.IncompleteError
	if err := filed("", "small"); !errors.As(err, &inc) {
		t.Fatalf("the empty string still refuses as incomplete, presence first: %v", err)
	}
	for _, tier := range []string{"trivial", "standard", "critical"} {
		if err := filed(tier, "small"); err != nil {
			t.Fatalf("member %q files: %v", tier, err)
		}
	}
	for _, class := range []string{"bespoke", "Small", "small ", "s"} {
		var ve *transition.VocabularyError
		err := filed("trivial", class)
		if !errors.As(err, &ve) || ve.Field != "budget" || ve.Value != class || !strings.Contains(err.Error(), "small, medium, large") {
			t.Fatalf("class %q refuses naming the classes: %v", class, err)
		}
	}
	for _, class := range []string{"small", "medium", "large"} {
		if err := filed("trivial", class); err != nil {
			t.Fatalf("class %q files: %v", class, err)
		}
	}
	// A value that is not a JSON string is no member either, and the
	// decode failure refuses rather than skipping the check.
	for name, payload := range map[string]string{
		"numeric tier":   `{"intent": "x", "tier": 1, "budget": "small", "routing": "core"}`,
		"array budget":   `{"intent": "x", "tier": "trivial", "budget": ["small"], "routing": "core"}`,
		"object tier":    `{"intent": "x", "tier": {"name": "trivial"}, "budget": "small", "routing": "core"}`,
		"boolean budget": `{"intent": "x", "tier": "trivial", "budget": true, "routing": "core"}`,
	} {
		var ve *transition.VocabularyError
		err := transition.CheckCompleteness("intent.filed", "c-1", []byte(payload))
		if !errors.As(err, &ve) {
			t.Fatalf("%s: a non-string value refuses as a vocabulary refusal, never skips the check: %v", name, err)
		}
	}
	// The check is intent.filed's: other completeness verbs are as before.
	if err := transition.CheckCompleteness("contract.returned", "c-1", []byte(`{"verdict": "3"}`)); err != nil {
		t.Fatalf("contract.returned's completeness is unchanged: %v", err)
	}
}

// conformance: AC3 — CheckPlanGate reads the table: trivial is exempt,
// standard and critical require a plan, and an unknown string takes the
// strictest row.
func TestPlanGateReadsTheTierTable(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	fold := tab.FoldRecords(nil)
	if err := fold.CheckPlanGate("c-1", "trivial", []byte(`{}`)); err != nil {
		t.Fatalf("trivial is exempt: %v", err)
	}
	for _, tier := range []string{"standard", "critical", "wizard", "", "Trivial"} {
		var pre *transition.PlanRequiredError
		if err := fold.CheckPlanGate("c-1", tier, []byte(`{}`)); !errors.As(err, &pre) || pre.Tier != tier {
			t.Fatalf("tier %q requires a plan, the strictest row for an unknown one: %v", tier, err)
		}
	}
	// The gate reads the TABLE, not the constant: a planted tier whose
	// row says no plan is exempt too, which no comparison against
	// "trivial" could grant.
	restore := transition.InjectTier("sandbox", transition.TierRow{})
	defer restore()
	if err := fold.CheckPlanGate("c-1", "sandbox", []byte(`{}`)); err != nil {
		t.Fatalf("a table row saying no plan exempts, whatever the tier is called: %v", err)
	}
	// TierGates is the one accessor the sites read.
	if g := transition.TierGates("wizard"); !g.PlanRequired || !g.SealedChecksRequired || !g.HumanReview {
		t.Fatalf("an unknown tier resolves to the strictest row: %+v", g)
	}
	if g := transition.TierGates("critical"); !g.PlanRequired || !g.SealedChecksRequired || !g.HumanReview {
		t.Fatalf("critical is the reviewed tier: %+v", g)
	}
	if g := transition.TierGates("trivial"); g.PlanRequired || g.SealedChecksRequired || g.HumanReview {
		t.Fatalf("trivial answers no at every gate: %+v", g)
	}
}
