// Package decider is the trajectory harness's missing half
// (plans/os-16e55c11.md D4): a Decider re-runs at a recorded decision
// point and says which loop act it would choose there, so a behavioral
// regression — the configuration unchanged, the choice changed — is
// caught (III.O row 5) where #239's five classes, which judge the frame
// and the configuration, could not.
//
// The reference decider is PARTIAL by design. The loop's per-iteration
// act depends on internal state the trajectory.Frame does not carry —
// the very reason #239 recorded frames rather than deciders — so a
// point where the frame underdetermines the act is abstained (""), not
// guessed. It decides only where the frame determines the choice: a
// claimable contract in `ready` is taken. A decider that stops taking
// claimable work, or takes it with the wrong act, diverges there.
package decider

import (
	"slices"

	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/trajectory"
)

// Decider chooses the loop act (loopverb spelling, e.g. "claim take") a
// lane would take at a frame, or "" when the frame does not determine
// the choice.
type Decider func(frame trajectory.Frame, m lane.Manifest) string

// Scripted is the reference decider.
func Scripted(frame trajectory.Frame, m lane.Manifest) string {
	if frame.State == "ready" &&
		slices.Contains(frame.Affordances, "claim.taken") &&
		slices.Contains(m.ActsThrough, "claim take") {
		return "claim take"
	}
	return ""
}
