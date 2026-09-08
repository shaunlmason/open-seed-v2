// The budget status surface (plans/os-cecac5de.md;
// spec/budgets.md; SEED-NEXT.md §II.9): the read-only view of a
// contract's derived budget — class, capacity, open reservations,
// settled actuals, remaining — agreeing with the admission
// computation by construction, since both call the same derivation.
// Reserve, settle, and release are loop verbs (loop.go): they derive
// the fence from the active window and the reservation a close cites
// from this same view, and pre-flight through admission before
// anything is signed.

package main

import (
	"os"

	"flag"
	"fmt"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"io"
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
)

func runBudget(args []string, stdout, stderr io.Writer) int {
	// status is a read and not a loop act, so it is named here rather
	// than in the registry; the three that APPEND come from
	// internal/loopverb, which the lane validator reads too.
	known := loopverb.English(append([]string{"status"}, loopverb.Subverbs("budget")...))
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "budget requires a subverb: "+known), stdout, stderr)
	}
	if args[0] != "status" {
		act, ok := loopverb.Lookup("budget", args[0])
		if !ok {
			return render(envelope.Fail(envelope.ExitUsage, "usage",
				fmt.Sprintf("unknown budget subverb %q — %s", args[0], known)), stdout, stderr)
		}
		return runBudgetLoop(args[1:], act.Verb, act.Name(), stdout, stderr)
	}
	fs := flag.NewFlagSet("budget status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	subject := fs.String("subject", "", "contract to report")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key; with it the response stamps affordances")
	if err := fs.Parse(args[1:]); err != nil || *dir == "" || *subject == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "budget status requires --ledger <dir> --subject <id> [--key <path>]"), stdout, stderr)
	}
	st, failEnv := loadVerdictState(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	s, ok := st.fold.State(*subject)
	if !ok {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found",
			fmt.Sprintf("no contract %s in the fold", *subject)), st.count), stdout, stderr)
	}
	view := admit.BudgetViewAt(st.records, st.table, *subject, s)
	open := []map[string]any{}
	for _, r := range view.Open {
		open = append(open, map[string]any{
			"position": fmt.Sprintf("%d", r.Pos),
			"signer":   r.Signer,
			"amount":   fmt.Sprintf("%d", r.Amount),
		})
	}
	var closedPositions []int
	for pos := range view.ClosedBy {
		closedPositions = append(closedPositions, pos)
	}
	sort.Ints(closedPositions)
	closes := []map[string]any{}
	for _, pos := range closedPositions {
		c := view.ClosedBy[pos]
		closes = append(closes, map[string]any{
			"reservation": fmt.Sprintf("%d", pos),
			"position":    fmt.Sprintf("%d", c.Pos),
			"kind":        c.Kind,
			"actuals":     fmt.Sprintf("%d", c.Actuals),
		})
	}
	result := map[string]any{
		"subject": *subject,
		"class":   view.Class,
		"known":   view.Known,
		"open":    open,
		"settled": fmt.Sprintf("%d", view.Settled),
		"closes":  closes,
	}
	if view.Known {
		result["capacity"] = fmt.Sprintf("%d", view.Capacity)
		result["remaining"] = fmt.Sprintf("%d", view.Remaining)
	}
	env := envelope.OK(result)
	if *keyPath != "" {
		// A fingerprint alone cannot sign probes, so the read surface
		// stamps affordances only when handed the key itself; without
		// one it guesses no identity and keeps the empty list.
		if keyBytes, err := os.ReadFile(*keyPath); err == nil {
			if signer, err := event.ParsePrivateKey(keyBytes); err == nil {
				env = stampAffordances(env, *dir, signer, *subject)
			}
		}
	}
	return render(stampTip(env, st.count), stdout, stderr)
}
