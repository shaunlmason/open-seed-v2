package admit

// RevokedHolder is the third reap corroboration, beside InterruptValid
// and WedgeDeclared, and the only one the ledger itself supplies
// (plans/os-32d06c65.md D1; charter III.E row 8): a claim whose holder
// has been revoked can be reaped on the revocation alone, because a
// revoked holder provably cannot exit its own window — every proposal
// from its key refuses at admission from the revocation's position on,
// so it can neither submit, release, park nor be interrupted. It is a
// LEDGER fact, not a stream classification, so unlike interrupt and
// wedge it corroborates a reap even against a no_data stream.
//
// Like InterruptValid it is boundary-valid, never fold-presence: the
// actor.revoked record's signer must have held the accepted lane
// (operator) at the revocation's own position, and the holder's
// standing must actually be revoked after it, so a raw-pushed
// revocation — or a suspension, whose standing can return — corroborates
// nothing.

import (
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// RevokedHolder reports whether the subject's active claim at `fence` is
// held by an actor a boundary-valid actor.revoked has revoked.
func RevokedHolder(records []*event.Record, table *transition.Table, subject string, fence int) bool {
	if table == nil {
		return false
	}
	s, ok := table.StateAt(records, subject)
	if !ok || s.Claim == nil || s.Claim.Fence != fence {
		return false
	}
	holder := s.Claim.Holder
	for pos, rec := range records {
		if rec.Event.Verb != keyring.VerbRevoked || rec.Event.Subject != holder {
			continue
		}
		// The signer held the revoking lane at the revocation's own
		// position — the InterruptValid posture, so a raw-pushed
		// revocation the boundary would refuse corroborates nothing.
		ring, _, err := keyring.StateAt(records[:pos])
		if err != nil || ring == nil ||
			!ring.HasAnyCapability(rec.Event.Actor, keyring.AcceptedCapabilities(keyring.VerbRevoked)) {
			continue
		}
		// And it took effect: the holder is REVOKED after this record,
		// not merely suspended (whose standing can return) and not a
		// no-op the fold ignored.
		after, _, err := keyring.StateAt(records[:pos+1])
		if err != nil || after == nil {
			continue
		}
		if e, ok := after.Get(holder); ok && e.Standing == keyring.StandingRevoked {
			return true
		}
	}
	return false
}
