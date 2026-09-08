package trajectory_test

// The decider re-decision (plans/os-16e55c11.md D4, AC4): with the
// configuration and the chain unchanged, an agreeing decider diverges
// nowhere, and a decider that would choose a different act at a
// frame-determined point fires choice_diverged there — the behavioral
// regression the five frame/configuration classes cannot see.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/trajectory"
)

func hasStr(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func TestReplayWithDeciderCatchesChoiceDivergence(t *testing.T) {
	c := scenario(t, false)
	worker := c.keys["worker"]
	records := c.records()
	traj, _, err := trajectory.Record(records, nil, worker, shippedLanes(t), "implementer")
	if err != nil {
		t.Fatal(err)
	}

	// The agreeing decider chooses the act the point recorded, and
	// abstains where the frame does not determine it: nothing diverges.
	agree := func(f trajectory.Frame, m lane.Manifest) string {
		if f.State == "ready" && hasStr(f.Affordances, "claim.taken") {
			return "claim take"
		}
		return ""
	}
	res, err := trajectory.ReplayWithDecider(traj, records, worker, shippedLanes(t), agree)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Points {
		if v.Class == trajectory.ChoiceDiverged {
			t.Fatalf("an agreeing decider must not diverge: %+v", v)
		}
	}

	// A planted decider parks where the lane took: choice_diverged at
	// the claim-take points, the configuration and frame unchanged.
	planted := func(f trajectory.Frame, m lane.Manifest) string {
		if f.State == "ready" && hasStr(f.Affordances, "claim.taken") {
			return "claim park"
		}
		return ""
	}
	res, err = trajectory.ReplayWithDecider(traj, records, worker, shippedLanes(t), planted)
	if err != nil {
		t.Fatal(err)
	}
	diverged := 0
	for _, v := range res.Points {
		if v.Class == trajectory.ChoiceDiverged {
			diverged++
			if v.Verb != "claim.taken" {
				t.Errorf("choice_diverged must fire only at the frame-determined claim-take points, got %s", v.Verb)
			}
		}
	}
	if diverged == 0 {
		t.Fatal("a planted decider change must fire choice_diverged at a determinate point")
	}

	// The five existing classes are untouched: without a decider, the
	// replay is all Same on the unchanged configuration.
	plain, err := trajectory.Replay(traj, records, worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Diverged() {
		t.Fatalf("the plain replay must be all Same on an unchanged configuration: %+v", plain.Divergent())
	}
}
