package project

// The report's per-adapter budget posture (plans/os-083112ac.md D2): the
// two remote substrates are never enforced, and an unknown harness is a
// risk limit — the safe default.

import "testing"

func TestAdapterBudgetPosture(t *testing.T) {
	for harness, want := range map[string]string{
		"local-worktree/v0": "enforced",
		"container/v0":      "enforced",
		"cloud-session/v0":  "risk-limit",
		"remote-worker/v0":  "risk-limit",
		"something/v9":      "risk-limit", // unknown: the safe default
	} {
		if got := adapterBudget(harness); got != want {
			t.Errorf("adapterBudget(%q) = %q, want %q", harness, got, want)
		}
	}
}
