package main

// Mutation evidence for the code-ref half (plans/os-465e356e.md
// acceptance criterion 7): each rule is load-bearing, shown by a case
// that only its presence refuses. The ledger half's mutation evidence is
// the established seed-admit suite (TestHookRefusesHistoryRewriteAndRefShapes,
// TestAdmissionRulesSelectByExclusion); this file pins the code-ref
// rules the compromised-actor drill depends on. If any rule is dropped,
// the matching case here flips from refused to admitted.

import (
	"strings"
	"testing"
)

// Each case is a push the code-ref half must refuse; a mutation that
// removed the rule would admit it. Running them through the real hook is
// the evidence — a green here means every rule still stands.
func TestCodeRefRulesAreLoadBearing(t *testing.T) {
	s := newCodeStand(t)
	// The default branch must exist for a non-fast-forward case to be an
	// update rather than a create.
	if out, err := pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "", map[string]string{"README": "base"}); err != nil {
		t.Fatalf("staging main: %v %s", err, out)
	}
	cases := []struct {
		name, pusher, ref string
		force             bool
		base              string
		files             map[string]string
		delete            bool
		reason            string
	}{
		{
			name: "default-branch operator gate", pusher: s.aFP, ref: "refs/heads/main", base: "refs/heads/main",
			files: map[string]string{"README": "x"}, reason: "operator standing only",
		},
		{
			name: "default-branch append-only", pusher: s.rootFP, ref: "refs/heads/main", force: true,
			files: map[string]string{"README": "x"}, reason: "non-fast-forward update of the default branch is refused for everyone",
		},
		{
			name: "contract-branch claim-holder gate", pusher: s.bFP, ref: "refs/heads/seed/c-1",
			base: "", files: map[string]string{"w": "1"}, reason: "does not hold the active claim on c-1",
		},
		{
			name: "tag immutability", pusher: s.rootFP, ref: "refs/tags/vX",
			files:  map[string]string{"f": "1"}, // create by operator is allowed; the immutability case needs an existing tag, tested below
			reason: "",
		},
		{
			name: "no-identity refusal", pusher: "", ref: "refs/heads/seed/c-1",
			files: map[string]string{"w": "1"}, reason: pusherEnv,
		},
		{
			name: "protected-surface gate", pusher: s.aFP, ref: "refs/heads/seed/c-1", base: "",
			files: map[string]string{"README": "r"}, reason: "", // c-1 has no declaration on default branch here; covered by TestCodeRefProtectedSurface
		},
	}
	for _, c := range cases {
		if c.reason == "" {
			continue // covered by a dedicated drill; listed here for the reader's map
		}
		out, err := pushCode(t, s.remote, c.pusher, c.ref, c.force, c.base, c.files)
		if err == nil || !strings.Contains(out, c.reason) {
			t.Errorf("%s: rule not load-bearing — push was not refused with %q:\n%s", c.name, c.reason, out)
		}
	}

	// Tag immutability needs an existing tag: create as operator, then
	// prove an update refuses. Dropping the immutability rule admits it.
	if out, err := pushCode(t, s.remote, s.rootFP, "refs/tags/rel", false, "", map[string]string{"f": "1"}); err != nil {
		t.Fatalf("operator tag creation must be admitted: %v %s", err, out)
	}
	if out, err := pushCode(t, s.remote, s.rootFP, "refs/tags/rel", true, "", map[string]string{"f": "2"}); err == nil || !strings.Contains(out, "tags are immutable") {
		t.Errorf("tag immutability rule not load-bearing: %v %s", err, out)
	}
}
