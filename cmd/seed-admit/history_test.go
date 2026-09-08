package main

// The hook judges every record with the same context the CLI's
// admission builds (plans/os-5c8a312c.md; found by the Phase 12 item 3
// storm): a representative history — reservations, run brackets,
// submissions, verdicts, the merge chain — pushed through the hook in
// one push is admitted, and the same history through the service too.

import (
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/history"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// conformance: III.B — one rule set, wherever it runs: a history the
// cooperative boundary admits is admitted by the hook in one push, and
// by the service one proposal at a time.
func TestHookAndServiceAdmitTheRepresentativeHistory(t *testing.T) {
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	res, err := history.Generate(history.Spec{Seed: 5, Contracts: 3, Dir: ledgerDir})
	if err != nil {
		t.Fatal(err)
	}

	// The hook: the whole history in one push onto an empty guarded ref.
	remote := guardedRemote(t)
	c, err := gitref.NewClient(t.TempDir(), remote, guardedRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitAndPush(ledgerDir, "", "ledger: history"); err != nil {
		t.Fatalf("the hook must admit a history the boundary admits: %v", err)
	}
	// And the next record after it, through the loop, with a budget
	// verb in the mix: the hook's context must carry what the budget
	// rule reads.
	store, err := ledger.OpenReadOnly(ledgerDir)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := store.VerifyFromGenesis(res.Resolve); err != nil || rep.Count != res.Records {
		t.Fatalf("the history verifies: %+v %v", rep, err)
	}

	// The service: the same records, proposed one at a time onto an
	// empty ledger branch, every one admitted.
	d := startService(t, forgeRemote(t))
	var records []*event.Record
	_ = store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil })
	asAdmission(t, func() {
		for i, rec := range records {
			out, err := d.client.Propose(posture.DefaultLedgerRef, []*event.Record{rec})
			if err != nil || out.Position != i {
				t.Fatalf("the service must admit record %d (%s on %s): %+v %v", i, rec.Event.Verb, rec.Event.Subject, out, err)
			}
		}
	})
}
