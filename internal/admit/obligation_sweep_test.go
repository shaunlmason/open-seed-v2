package admit

// The obligation drift class (plans/os-52d5da3f.md D5): every
// obligation the projection emits must be DISCHARGEABLE by the party
// it is owed by, at the position it is emitted at, judged by the same
// Check admission enforces. This is the III.I row-2 property one
// level up: affordances must not advertise a verb admission refuses,
// and obligations must not name a debt its owner cannot pay.
//
// The assertion is "at least one discharging verb admits for the owed
// actor", not "all of them do", and the distinction is deliberate:
// discharged_by names the acts that END the obligation, while WHO may
// perform each is the affordance layer's business. An active claim,
// for instance, is discharged by claim.released, claim.parked,
// claim.reaped and submission.made — the holder may take three of
// those and the supervisor the fourth. Requiring every verb to admit
// for the owed actor would force capability policy into a derivation
// that must stay a projection.

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: III.I row 2, one level up — an obligation whose
// discharging verbs are all refused at its own position is a bug.
func TestObligationsAreDischargeable(t *testing.T) {
	store, resolve, signer := seededStore(t)
	lanes := walkLanes(t)
	keys := map[string]ed25519.PrivateKey{"root": signer}
	for name, key := range lanes {
		keys[name] = key
	}
	loose := walkResolver(t, resolve, lanes)
	// The owed party's key: a fingerprint for an individual, the
	// lane's enrolled key for a lane-owed kind.
	laneKeys := map[string]ed25519.PrivateKey{
		obligation.LaneVerifier:   lanes["verifier"],
		obligation.LaneObserver:   lanes["observer"],
		obligation.LaneSupervisor: lanes["supervisor"],
		obligation.LaneOperator:   signer,
		// The dispatch lane's debts (request.pending) are payable by
		// operator standing too, which the root holds.
		obligation.LaneDispatcher: signer,
	}
	byFingerprint := map[string]ed25519.PrivateKey{}
	for _, key := range keys {
		byFingerprint[fpOf(t, key)] = key
	}
	synth := map[string]func(*probeView) string{}
	for _, p := range affordanceCatalog {
		synth[p.verb] = p.synth
	}

	sweep := func(pos int) {
		ctx, err := ContextAt(store)
		if err != nil {
			t.Fatalf("position %d: %v", pos, err)
		}
		table := ctx.Table
		rows := obligation.Derive(ctx.Records, table, obligation.Deps{
			BudgetOpen: func(subject string, s transition.SubjectState) []transition.ReservationFact {
				return BudgetViewAt(ctx.Records, table, subject, s).Open
			},
			// The sweep's own copy of the standing wiring, kept
			// independent of the projection's for the same reason
			// probeViewAt is: if the two disagree on who can pay a
			// debt, the probe below refuses and the class fails.
			CanDischarge: func(actor string, verbs []string) bool {
				for _, verb := range verbs {
					if ctx.Keyring.HasAnyCapability(actor, keyring.AcceptedCapabilities(verb)) {
						return true
					}
				}
				return false
			},
		})
		if ctx.Halt.Halted {
			// A halt stops admission globally: every obligation still
			// STANDS (the debt is real) but none is dischargeable
			// until the halt lifts, and that is the halt working, not
			// an obligation defect. The sweep asserts the standing
			// rows are well-formed and leaves dischargeability to the
			// unhalted positions.
			for _, row := range rows {
				if len(row.DischargedBy) == 0 {
					t.Fatalf("obligation class: %s on %s at position %d carries no discharging verb", row.Kind, row.Subject, pos)
				}
			}
			return
		}
		for _, row := range rows {
			if len(row.DischargedBy) == 0 {
				t.Fatalf("obligation class: %s on %s at position %d carries no discharging verb",
					row.Kind, row.Subject, pos)
			}
			key, ok := laneKeys[row.OwedBy]
			if !ok {
				if key, ok = byFingerprint[row.OwedBy]; !ok {
					// An obligation owed by an actor the fixture does
					// not hold a key for cannot be probed; the walk
					// enrolls every lane it exercises, so this would
					// mean the fixture drifted from the taxonomy.
					t.Fatalf("obligation class: %s on %s owed by unknown party %q at position %d",
						row.Kind, row.Subject, row.OwedBy, pos)
				}
			}
			fp := fpOf(t, key)
			v := probeViewAt(ctx, row.Subject, time.Now())
			v.actor = fp
			admitted := false
			var lastErr error
			for _, verb := range row.DischargedBy {
				fill, ok := synth[verb]
				if !ok {
					t.Fatalf("obligation class: %s advertises %s, absent from the probe catalog", row.Kind, verb)
				}
				rec, err := event.Sign(event.Event{
					V: ctx.Active, TS: v.now, Actor: fp, Verb: verb,
					Subject: row.Subject, Payload: []byte(fill(v)), Prev: ctx.Tip,
				}, key)
				if err != nil {
					t.Fatalf("drafting %s: %v", verb, err)
				}
				if err := Check(ctx, rec); err == nil {
					admitted = true
					break
				} else {
					lastErr = err
				}
			}
			if !admitted {
				t.Errorf("obligation class: %s on %s at position %d is owed by %s but none of %v admits (last: %v)",
					row.Kind, row.Subject, pos, row.OwedBy, row.DischargedBy, lastErr)
			}
		}
	}
	sweep(0)
	for i, s := range walkScript(t, lanes) {
		runWalkStep(t, store, loose, keys, s)
		sweep(i + 1)
	}
}
