package main

// The reservation race and the status surface end-to-end
// (plans/os-cecac5de.md; spec/budgets.md): two 8-unit drafts
// against a 10-unit class both pass the same pre-admission view, and
// exactly one admits — the second refuses against the tip that
// already carries the first, which is the whole §II.9 argument for
// reservations over observations.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: III.H — concurrent over-spend against one budget is
// structurally impossible for reservable resources: the reserve is
// checked and decremented at admission, the one serialized view.
func TestReservationRaceAndStatus(t *testing.T) {
	restore := transition.InjectBudgetClass("ten", 10)
	defer restore()
	ld, _, _, specCommit, _, priv, _, keys, fps := offerLedger(t)
	for _, step := range [][]string{
		{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "ten", "routing": "core"}`},
		{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", "c-1", "--payload", step[1]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	holder := workerRawKey(22)
	fencePos, err := admitAppend(t, ld, holder, "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	fence := fmt.Sprintf("%d", fencePos)

	// Two racing 8-unit drafts against one tip: each alone fits the
	// 10-unit class, and both pass the pre-admission view — the
	// after-the-fact-metering failure mode. Admission serializes:
	// the first admits, and the second refuses against the tip that
	// already carries it, naming both numbers.
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(holder.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	draft := func() *event.Record {
		rec, err := event.Sign(event.Event{
			V: ctx.Active, TS: time.Now().UTC().Format(time.RFC3339), Actor: fp,
			Verb: "budget.reserve", Subject: "c-1",
			Payload: json.RawMessage(`{"amount": "8", "fence": "` + fence + `"}`), Prev: ctx.Tip,
		}, holder)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	a, b := draft(), draft()
	if err := admit.Check(ctx, a); err != nil {
		t.Fatalf("draft A passes the shared view: %v", err)
	}
	if err := admit.Check(ctx, b); err != nil {
		t.Fatalf("draft B passes the same shared view — both observed 10 remaining: %v", err)
	}
	if _, err := store.Append(a, ctx.Resolve); err != nil {
		t.Fatalf("A admits: %v", err)
	}
	ctx2, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	var be *admit.BudgetError
	if err := admit.Check(ctx2, b); err == nil || !errors.As(err, &be) {
		t.Fatalf("B refuses at admission against the tip carrying A: %v", err)
	}

	// The status surface agrees with the admission computation, and
	// an overrun settle records true actuals: remaining = 10 - 9.
	e, code := runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-1")
	if code != 0 || e.Result["remaining"] != "2" || e.Result["capacity"] != "10" {
		t.Fatalf("status after the reserve: %d %+v", code, e.Result)
	}
	reservePos := fmt.Sprintf("%d", *statusInt(t, e, "open"))
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "budget.settle", "--subject", "c-1", "--payload",
		`{"reservation": "`+reservePos+`", "actuals": "9", "fence": "`+fence+`"}`); code != 0 {
		t.Fatalf("the owner's overrun settle admits through the CLI: %d %+v", code, e)
	}
	e, code = runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-1")
	if code != 0 || e.Result["remaining"] != "1" || e.Result["settled"] != "9" {
		t.Fatalf("status after the overrun settle: %d %+v", code, e.Result)
	}
	if open, _ := e.Result["open"].([]any); len(open) != 0 {
		t.Fatalf("the settled reservation is no longer open: %+v", e.Result)
	}
	if e, code = runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-none"); code == 0 {
		t.Fatalf("an unknown subject is not found: %+v", e)
	}
	_ = fps
}

// conformance: III.H — the same out-of-window close through the CLI
// (plans/os-d6963652.md D5, step 5): the loop verb omits the fence
// key because no window is active, the close lands and returns the
// capacity, and budget status --key LISTS it beforehand, which is
// the surface the unconditional probe citation broke.
func TestBudgetClosesAfterTheWindowThroughTheCLI(t *testing.T) {
	restore := transition.InjectBudgetClass("ten", 10)
	defer restore()
	ld, _, base, specCommit, head, priv, _, keys, _ := offerLedger(t)
	for _, step := range [][]string{
		{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "ten", "routing": "core"}`},
		{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", "c-1", "--payload", step[1]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	worker := keys["workerA"]
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", worker,
		"--subject", "c-1", "--amount", "8"); code != 0 || !e.OK {
		t.Fatalf("reserve: %d %+v", code, e)
	}

	// The window ends with the reservation open: exactly the shape
	// that stranded 8 of the class's 10 units.
	pkt := writePacket(t, "")
	if e, code := runEnv(t, "submission", "make", "--ledger", ld, "--key", worker,
		"--subject", "c-1", "--packet", pkt, "--base", base+".."+head); code != 0 || !e.OK {
		t.Fatalf("submission make: %d %+v", code, e)
	}

	// The status surface lists the closes for the reservation's own
	// signer, outside any window. Without the conditional citation
	// the probes would refuse at the fence rule and this list would
	// hide a legal act.
	e, code := runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-1", "--key", worker)
	if code != 0 {
		t.Fatalf("status: %d %+v", code, e)
	}
	for _, verb := range []string{"budget.settle", "budget.release"} {
		if !slices.Contains(e.Affordances, verb) {
			t.Fatalf("status lists %s outside the window: %v", verb, e.Affordances)
		}
	}

	// And the close itself: no --fence flag exists, and none LANDS,
	// because no window is active to cite.
	e, code = runEnv(t, "budget", "settle", "--ledger", ld, "--key", worker, "--subject", "c-1", "--actuals", "6")
	if code != 0 || !e.OK {
		t.Fatalf("the signer settles after the window ends: %d %+v", code, e)
	}
	if p := payloadAt(t, ld, chainCount(t, ld)-1); p["fence"] != nil {
		t.Fatalf("outside a window the close cites no fence: %+v", p)
	}
	e, code = runEnv(t, "budget", "status", "--ledger", ld, "--subject", "c-1")
	if code != 0 || e.Result["remaining"] != "4" || e.Result["settled"] != "6" {
		t.Fatalf("capacity returns to capacity minus actuals: %d %+v", code, e.Result)
	}
}

// statusInt digs the first open reservation's position out of the
// status envelope.
func statusInt(t *testing.T, e ledgerEnv, key string) *int {
	t.Helper()
	rows, _ := e.Result[key].([]any)
	if len(rows) == 0 {
		t.Fatalf("no %s rows: %+v", key, e.Result)
	}
	row, _ := rows[0].(map[string]any)
	var n int
	if _, err := fmt.Sscanf(row["position"].(string), "%d", &n); err != nil {
		t.Fatal(err)
	}
	return &n
}
