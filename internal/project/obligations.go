// The obligations projection (plans/os-52d5da3f.md; build plan Phase 9
// item 5): what is owed on each subject, derived from the same fold
// admission enforces and never a new authority. Input-free like every
// projection but the report: the rows are a pure function of the
// verified prefix.

package project

import (
	"encoding/json"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// ObligationsFile is the projection's one view file.
const ObligationsFile = "obligations.json"

// ObligationsView is obligations.json: the standing rows at the
// build's position, in stable (subject, kind) order.
type ObligationsView struct {
	Obligations []obligation.Row `json:"obligations"`
}

// Obligations returns the obligations projection.
func Obligations() Projection {
	return Projection{Name: "obligations", Version: "1", Build: buildObligations}
}

// DeriveObligations is the shared derivation: the projection builds
// from it, and the situation read consumes it at the ledger tip.
// Budget openness comes from the one shared budget derivation rather
// than a second opinion here.
func DeriveObligations(records []*event.Record) ([]obligation.Row, error) {
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	ring, _, err := keyring.StateAt(records)
	if err != nil {
		return nil, err
	}
	deps := obligation.Deps{
		BudgetOpen: func(subject string, s transition.SubjectState) []transition.ReservationFact {
			return admit.BudgetViewAt(records, table, subject, s).Open
		},
		// The same capability question the grant rule asks at
		// admission, asked of the party a row would be owed by: an
		// obligation belongs to whoever can still discharge it, and
		// standing is the keyring's answer to give. Still a pure
		// function of the verified prefix, so the projection stays
		// input-free.
		CanDischarge: func(actor string, verbs []string) bool {
			for _, verb := range verbs {
				if ring.HasAnyCapability(actor, keyring.AcceptedCapabilities(verb)) {
					return true
				}
			}
			return false
		},
	}
	return obligation.Derive(records, table, deps), nil
}

func buildObligations(records []*event.Record, _ Inputs) (map[string][]byte, error) {
	rows, err := DeriveObligations(records)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(ObligationsView{Obligations: rows}, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ObligationsFile: append(b, '\n')}, nil
}
