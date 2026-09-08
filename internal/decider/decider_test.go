package decider

// The reference decider is a partial policy (plans/os-16e55c11.md D4):
// it takes a claimable contract in ready and abstains everywhere the
// frame does not determine the act.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/trajectory"
)

func TestScriptedDecidesClaimTakeAndAbstains(t *testing.T) {
	loopLane := lane.Manifest{ActsThrough: []string{"claim take", "budget reserve", "submission make"}}
	ready := trajectory.Frame{State: "ready", Affordances: []string{"claim.taken", "message.sent"}}
	if got := Scripted(ready, loopLane); got != "claim take" {
		t.Errorf("a claimable contract in ready is taken, got %q", got)
	}
	// In progress: the frame does not determine the act — abstain.
	inProg := trajectory.Frame{State: "in_progress", Affordances: []string{"budget.reserve", "submission.made"}}
	if got := Scripted(inProg, loopLane); got != "" {
		t.Errorf("an underdetermined in_progress point abstains, got %q", got)
	}
	// A lane that does not act through claim take does not take.
	nonLoop := lane.Manifest{ActsThrough: nil}
	if got := Scripted(ready, nonLoop); got != "" {
		t.Errorf("a non-loop lane takes nothing, got %q", got)
	}
	// Ready without the affordance (already claimed elsewhere): abstain.
	claimed := trajectory.Frame{State: "ready", Affordances: []string{"message.sent"}}
	if got := Scripted(claimed, loopLane); got != "" {
		t.Errorf("no claim affordance, no take, got %q", got)
	}
}
