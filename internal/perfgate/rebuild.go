package perfgate

import (
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)

// rebuildMS times a cold projection rebuild of the ledger into out.
// It lives apart from the measurer because the projection engine's
// write-boundary lint holds every file importing it to no filesystem
// writes of its own; the temp tree it rebuilds into is the caller's.
func rebuildMS(ledgerDir, out string, resolve ledger.Resolver) (float64, error) {
	start := time.Now()
	if _, err := project.Rebuild(ledgerDir, out, project.Default(), resolve); err != nil {
		return 0, err
	}
	return ms(time.Since(start)), nil
}
