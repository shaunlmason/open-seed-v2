package transition_test

// The spec-gate drills (plans/os-73c00a50.md): executable content
// requires gate evidence at every tier, the gate binds to the exact
// acceptance revision, prose-only criteria stay gateless, and a
// request.* proposal structurally cannot arm content.

import (
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func TestAcceptanceShape(t *testing.T) {
	ok := func(payload string) {
		t.Helper()
		if _, err := transition.ParseAcceptance("c-1", []byte(payload)); err != nil {
			t.Fatalf("must admit: %v", err)
		}
	}
	refuse := func(payload, field, want string) {
		t.Helper()
		_, err := transition.ParseAcceptance("c-1", []byte(payload))
		ae, isAE := err.(*transition.AcceptanceError)
		if !isAE {
			t.Fatalf("want a typed acceptance error for %s, got %v", payload, err)
		}
		if ae.Field != field || !strings.Contains(ae.Reason, want) {
			t.Fatalf("want field %q reason containing %q, got %v", field, want, err)
		}
	}

	// Prose-only criteria: gateless at any tier.
	ok(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": false}}`)
	// Executable with gate evidence bound to the same revision.
	ok(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": true, "gate": "116 @ abc1234"}}`)

	// Executable without a gate refuses — there is no tier exemption.
	refuse(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": true}}`,
		"acceptance.gate", "every tier")
	// Gate evidence bound to a different commit vouches for nothing.
	refuse(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": true, "gate": "116 @ def5678"}}`,
		"acceptance.gate", "def5678")
	// A gate on prose-only content violates the iff.
	refuse(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": false, "gate": "116 @ abc1234"}}`,
		"acceptance.gate", "iff")
	// Bare or malformed anchors refuse.
	refuse(`{"acceptance": {"ref": "specs/a.md", "executable": false}}`,
		"acceptance.ref", "commit-anchored")
	refuse(`{"acceptance": {"ref": "specs/a.md @ abc1234..def5678", "executable": false}}`,
		"acceptance.ref", "range")
	refuse(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": true, "gate": "the reviewers agreed"}}`,
		"acceptance.gate", "pr @ merged-commit")
	// The executable marker is an explicit declaration: an absent or
	// null marker decodes into the same false a declared one does, and
	// silence must never decide whether content is armed.
	refuse(`{"acceptance": {"ref": "specs/a.sh @ abc1234"}}`,
		"acceptance.executable", "declared explicitly")
	refuse(`{"acceptance": {"ref": "specs/a.sh @ abc1234", "executable": null}}`,
		"acceptance.executable", "declared explicitly")

	// The old inline string form and unknown keys refuse: the spec
	// body is an artifact, and the shape is strict.
	refuse(`{"acceptance": "specs/a.md @ abc1234"}`, "acceptance", "structured")
	refuse(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": false, "body": "run: rm -rf"}}`,
		"acceptance", "structured")
	refuse(`{}`, "acceptance", "claimable")
}

// conformance: III.F row 2 — text originating outside the trust
// boundary can propose but never directly become executable content:
// the arming keys refuse on the proposal shape at any depth.
func TestProposalCannotArm(t *testing.T) {
	if err := transition.CheckProposalShape("c-1", []byte(`{"proposal": "please verify the widget spins", "acceptance_prose": "the widget spins"}`)); err != nil {
		t.Fatalf("proposed prose is data and admits: %v", err)
	}
	for name, payload := range map[string]string{
		"top-level executable": `{"executable": true}`,
		"top-level gate":       `{"gate": "116 @ abc1234"}`,
		"nested executable":    `{"proposal": {"acceptance": {"executable": true}}}`,
		"gate in an array":     `{"notes": [{"gate": "x"}]}`,
	} {
		err := transition.CheckProposalShape("c-1", []byte(payload))
		ae, ok := err.(*transition.AcceptanceError)
		if !ok || !strings.Contains(ae.Reason, "never carry") {
			t.Fatalf("%s must refuse as smuggling, got %v", name, err)
		}
	}
}
