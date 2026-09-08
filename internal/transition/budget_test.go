package transition_test

// The budget fold and derivation drills (plans/os-cecac5de.md;
// spec/budgets.md): facts fold tolerantly as independent lists,
// nothing mutates, dangling closes are anomalies, and the derivation
// implements claimed math — open, settled, overrun, first-valid-close
// wins — under caller-supplied validity.

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: III.H — the spec class table and the code capacity map
// cannot drift apart one-sidedly.
func TestBudgetClassTableMirrorsSpec(t *testing.T) {
	b, err := os.ReadFile("../../spec/budgets.md")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `([a-z]+)` \\| `(\\d+)` \\|$")
	rows := row.FindAllStringSubmatch(string(b), -1)
	if len(rows) == 0 {
		t.Fatal("no class rows parsed from spec/budgets.md")
	}
	seen := map[string]bool{}
	for _, m := range rows {
		class, want := m[1], m[2]
		got, ok := transition.BudgetCapacity(class)
		if !ok {
			t.Errorf("class %q is in the spec table but not the code map", class)
			continue
		}
		if gotStr := itoa(got); gotStr != want {
			t.Errorf("class %q: code says %s, spec says %s", class, gotStr, want)
		}
		seen[class] = true
	}
	for _, class := range []string{"small", "medium", "large"} {
		if !seen[class] {
			t.Errorf("class %q is governed by code but missing from the spec table", class)
		}
	}
	if _, ok := transition.BudgetCapacity("bespoke"); ok {
		t.Error("an unknown class must have no capacity")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func budgetEvent(verb, subject, payload string) *event.Record {
	return payloadEvent("seed/1", verb, subject, payload)
}

func TestBudgetFoldAndDerivation(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	records := []*event.Record{
		payloadEvent("seed/1", "intent.filed", "c-1", `{"tier": "trivial", "budget": "small"}`), // 0
		lifecycleEvent("contract.specified", "c-1"),                                             // 1
		lifecycleEvent("claim.taken", "c-1"),                                                    // 2
		budgetEvent("budget.reserve", "c-1", `{"amount": "60"}`),                                // 3
		budgetEvent("budget.reserve", "c-1", `{"amount": "nope"}`),                              // 4: malformed, no fact
		budgetEvent("budget.settle", "c-1", `{"reservation": "3", "actuals": "70"}`),            // 5: overrun close
		budgetEvent("budget.release", "c-1", `{"reservation": "99"}`),                           // 6: dangling, anomaly
		budgetEvent("budget.reserve", "c-1", `{"amount": "10"}`),                                // 7
	}
	fold := tab.FoldRecords(records)
	s, ok := fold.State("c-1")
	if !ok || s.Budget != "small" {
		t.Fatalf("the filed budget class folds at birth: %+v", s.Budget)
	}
	if len(s.Reservations) != 2 || s.Reservations[0].Pos != 3 || s.Reservations[0].Amount != 60 || s.Reservations[1].Pos != 7 {
		t.Fatalf("two well-shaped reserves fold, the malformed one to nothing: %+v", s.Reservations)
	}
	if len(s.BudgetCloses) != 1 || s.BudgetCloses[0].Reservation != 3 || s.BudgetCloses[0].Kind != "settle" || s.BudgetCloses[0].Actuals != 70 {
		t.Fatalf("the close attempt folds independently: %+v", s.BudgetCloses)
	}
	if s.Anomalies == 0 {
		t.Fatal("a close citing no reservation counts an anomaly, never a fact")
	}

	// Derivation with everything valid: the settle closes reservation
	// 3 with overrun actuals; remaining = 100 - 10 (open) - 70
	// (settled) = 20.
	all := func(transition.ReservationFact) bool { return true }
	own := func(c transition.CloseFact, r transition.ReservationFact) bool { return c.Signer == r.Signer }
	v := s.DeriveBudget(all, own)
	if !v.Known || v.Capacity != 100 || v.Settled != 70 || v.Remaining != 20 || len(v.Open) != 1 || v.Open[0].Pos != 7 {
		t.Fatalf("overrun settle math: %+v", v)
	}
	if c, ok := v.ClosedBy[3]; !ok || c.Pos != 5 {
		t.Fatalf("the effective close is recorded by reservation: %+v", v.ClosedBy)
	}

	// An invalid reservation consumes nothing and its close decides
	// nothing; an invalid close leaves the reservation open. First
	// valid close wins over later ones.
	records = append(records,
		budgetEvent("budget.release", "c-1", `{"reservation": "7"}`),                // 8: foreign attempt (invalid below)
		budgetEvent("budget.settle", "c-1", `{"reservation": "7", "actuals": "5"}`), // 9: owner attempt
	)
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	notPos8 := func(c transition.CloseFact, r transition.ReservationFact) bool { return c.Pos != 8 }
	v = s.DeriveBudget(all, notPos8)
	if c, ok := v.ClosedBy[7]; !ok || c.Pos != 9 || c.Kind != "settle" {
		t.Fatalf("the invalid attempt decides nothing and the valid one closes: %+v", v.ClosedBy)
	}
	if v.Remaining != 100-70-5 {
		t.Fatalf("remaining after both settles: %+v", v)
	}
	onlyFirst := func(r transition.ReservationFact) bool { return r.Pos == 3 }
	v = s.DeriveBudget(onlyFirst, own)
	if len(v.Open) != 0 || v.Remaining != 100-70 {
		t.Fatalf("an invalid reservation consumes nothing: %+v", v)
	}
}

// Remaining can never INCREASE through integer wraparound (review
// finding on the task PR): two enormous settled actuals saturate the
// spend sum instead of wrapping it negative, so remaining stays far
// below zero and every further reserve refuses.
func TestBudgetSpendSaturates(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	huge := fmt.Sprintf("%d", math.MaxInt)
	records := []*event.Record{
		payloadEvent("seed/1", "intent.filed", "c-1", `{"tier": "trivial", "budget": "small"}`), // 0
		lifecycleEvent("contract.specified", "c-1"),                                             // 1
		lifecycleEvent("claim.taken", "c-1"),                                                    // 2
		budgetEvent("budget.reserve", "c-1", `{"amount": "1"}`),                                 // 3
		budgetEvent("budget.reserve", "c-1", `{"amount": "1"}`),                                 // 4
		budgetEvent("budget.settle", "c-1", `{"reservation": "3", "actuals": "`+huge+`"}`),      // 5
		budgetEvent("budget.settle", "c-1", `{"reservation": "4", "actuals": "`+huge+`"}`),      // 6
	}
	fold := tab.FoldRecords(records)
	s, _ := fold.State("c-1")
	all := func(transition.ReservationFact) bool { return true }
	own := func(c transition.CloseFact, r transition.ReservationFact) bool { return true }
	v := s.DeriveBudget(all, own)
	if v.Settled != math.MaxInt {
		t.Fatalf("the settled sum saturates instead of wrapping: %d", v.Settled)
	}
	if v.Remaining >= 0 {
		t.Fatalf("remaining never increases through wraparound: %d", v.Remaining)
	}
}
