package project_test

// The budget projection drills (plans/os-cecac5de.md): the derived
// budget object and reservations land in the contracts "10" view and
// the cache's generation-9 tables only beside budget facts, remaining
// counts valid reservations only, and budget-inactive subjects keep
// their prior bodies (pinned by the untouched goldens).

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/project"

	_ "modernc.org/sqlite"
)

func TestBudgetFactsInContractsAndCache(t *testing.T) {
	root, foreign := pKey(t, 1), pKey(t, 3)
	dir, resolve, add := fixtureChain(t, root, foreign)
	add(root, "seed/0", "system.protocol.upgraded", "system", `{"to": "seed/1"}`)
	add(root, "seed/1", "actor.enrolled", pFP(t, foreign), enrollJSON(t, foreign, "agent", "plain"))
	add(root, "seed/1", "intent.filed", "c-1", `{"tier": "trivial", "budget": "small"}`)
	add(root, "seed/1", "contract.specified", "c-1", `{}`)
	add(root, "seed/1", "claim.taken", "c-1", `{}`)
	add(root, "seed/1", "budget.reserve", "c-1", `{"amount": "60"}`)                     // valid: operator
	add(foreign, "seed/1", "budget.reserve", "c-1", `{"amount": "40"}`)                  // raw grantless: folds, consumes nothing
	add(root, "seed/1", "budget.settle", "c-1", `{"reservation": "6", "actuals": "70"}`) // overrun close of the valid one
	add(root, "seed/1", "intent.filed", "c-2", `{"tier": "trivial", "budget": "small"}`)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}

	build, err := project.Current(out, "contracts")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(build, project.ContractsFile))
	if err != nil {
		t.Fatal(err)
	}
	var entries []project.ContractEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	byName := map[string]*project.ContractEntry{}
	for i := range entries {
		byName[entries[i].Subject] = &entries[i]
	}
	c1 := byName["c-1"]
	if c1 == nil || c1.Budget == nil || len(c1.Reservations) != 2 {
		t.Fatalf("c-1 carries the derived budget and every folded reservation: %+v", c1)
	}
	// The foreign reserve consumes nothing: remaining = 100 - 70
	// settled actuals, with the valid reservation closed and the
	// foreign one shown open-but-inert.
	if c1.Budget.Class != "small" || c1.Budget.Capacity != 100 || c1.Budget.Remaining != 30 {
		t.Fatalf("remaining counts valid reservations only: %+v", c1.Budget)
	}
	if c1.Reservations[0].Closed == nil || c1.Reservations[0].Closed.Kind != "settle" || c1.Reservations[0].Closed.Actuals != 70 {
		t.Fatalf("the valid reservation carries its effective close: %+v", c1.Reservations[0])
	}
	if c1.Reservations[1].Closed != nil {
		t.Fatalf("the foreign reservation has no effective close: %+v", c1.Reservations[1])
	}
	if c2 := byName["c-2"]; c2 == nil || c2.Budget != nil || len(c2.Reservations) != 0 {
		t.Fatalf("a budget-inactive subject serializes no budget object: %+v", c2)
	}

	// The cache agrees: the reservations table rows and the budget
	// columns, NULL where inactive.
	db, _ := openCacheRO(t, out)
	defer db.Close()
	if n := one[int](t, db, `SELECT COUNT(*) FROM reservations WHERE subject = 'c-1'`); n != 2 {
		t.Fatalf("reservations rows: %d", n)
	}
	if rem := one[int](t, db, `SELECT budget_remaining FROM contract_state WHERE subject = 'c-1'`); rem != 30 {
		t.Fatalf("cache remaining: %d", rem)
	}
	if class := one[string](t, db, `SELECT budget_class FROM contract_state WHERE subject = 'c-2'`); class != "small" {
		t.Fatalf("the filed class is data either way: %s", class)
	}
	var rem sql.NullInt64
	if err := db.QueryRow(`SELECT budget_remaining FROM contract_state WHERE subject = 'c-2'`).Scan(&rem); err != nil {
		t.Fatal(err)
	}
	if rem.Valid {
		t.Fatalf("a budget-inactive subject carries NULL remaining, got %d", rem.Int64)
	}
}
