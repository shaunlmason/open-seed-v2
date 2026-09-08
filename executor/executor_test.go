package executor

// The adapter surface's pure drills; the full bracket (Provision
// against an admitted run.started, metering, the SIGKILL
// disposability drill) runs in cmd/seed, where the ledger and
// repository fixtures live.

import (
	"testing"
)

func TestLocalWorktreeSurface(t *testing.T) {
	var lw LocalWorktree
	// The static report is the two fields this adapter controls and
	// nothing else: principal, model and tool policy are the caller's
	// judgment, and an adapter that invented one would declare a
	// configuration nobody chose (plans/os-8e53ffd9.md D1).
	if got := lw.Tuple(); got.Harness != LocalHarness || got.Environment != LocalEnvironment ||
		got.Principal != "" || got.Model != "" || got.ToolPolicy != "" || got.Complete() {
		t.Fatalf("the local adapter's static report: %+v", got)
	}
	if err := lw.Wake("anyone"); err != nil {
		t.Fatalf("wake is the advisory no-op — its total failure costs latency, and the honest v0 does nothing: %v", err)
	}
	if _, err := lw.Provision(ProvisionSpec{Ledger: t.TempDir()}); err == nil {
		t.Fatal("a provision without a verifiable ledger refuses — no run outside the reservation gate")
	}
}
