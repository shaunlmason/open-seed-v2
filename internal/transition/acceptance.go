// The acceptance-spec field and the spec gate (plans/os-73c00a50.md;
// SEED-NEXT.md Part II §6 "Acceptance specs are privileged code"): a
// spec ultimately causes command execution on a verifier, so the spec
// body is a repo artifact that merges through the same review gate as
// code, and the contract carries a commit-anchored reference. What
// this stage enforces is the gate-before-specified half; the
// gate-before-run half is the verifier's refusal to execute ungated
// content and lands with verdicts (Phase 6).

package transition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/classify"
)

// Acceptance is contract.specified's structured acceptance field.
type Acceptance struct {
	// Ref anchors the spec artifact: "<path> @ <commit>".
	Ref string `json:"ref"`
	// Executable declares whether the spec body contains runnable
	// content; prose-only criteria need no gate.
	Executable bool `json:"executable"`
	// Gate is the review-gate evidence, "<pr> @ <merged-commit>",
	// present iff Executable: an observation of forge fact, the
	// plan.approved posture. Its merged commit MUST equal Ref's
	// commit, so gate evidence is bound to the exact acceptance
	// revision (review finding on #116).
	Gate string `json:"gate,omitempty"`
}

// AcceptanceError is the structured-acceptance shape refusal, naming
// the missing or mismatched piece.
type AcceptanceError struct {
	Subject string
	Field   string
	Reason  string
}

func (e *AcceptanceError) Error() string {
	return fmt.Sprintf("contract.specified on %s: %s: %s (spec/acceptance.md)", e.Subject, e.Field, e.Reason)
}

// anchorCommit extracts the single-commit part of a combined anchor;
// ok is false for ranges or malformed anchors.
func anchorCommit(anchor string) (string, bool) {
	_, commit, found := strings.Cut(anchor, " @ ")
	if !found || strings.Contains(commit, "..") {
		return "", false
	}
	return commit, true
}

// ParseAcceptance validates the structured acceptance field of a
// contract.specified payload.
func ParseAcceptance(subject string, payload []byte) (*Acceptance, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance", Reason: "payload is not an object"}
	}
	raw, ok := m["acceptance"]
	if !ok {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance", Reason: "a contract cannot become claimable without its acceptance spec"}
	}
	var a Acceptance
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance", Reason: fmt.Sprintf("the acceptance field is the structured object {ref, executable, gate?}: %v", err)}
	}
	// The executable marker is an explicit declaration: an absent or
	// null key decodes into the same false a declared one does, and
	// silence must never decide whether content is armed. Only the
	// literal booleans admit.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance", Reason: "the acceptance field is not an object"}
	}
	ex := string(bytes.TrimSpace(fields["executable"]))
	if ex != "true" && ex != "false" {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance.executable", Reason: "the executable marker is declared explicitly, true or false — an absent or null marker is not a declaration, and Phase 6 reads \"may this run?\" from what was declared"}
	}
	if !classify.IsAnchoredRef(a.Ref) {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance.ref", Reason: fmt.Sprintf("%q is not a commit-anchored artifact reference (\"path @ commit\") — the spec body is a repo artifact, never inline prose that executes", a.Ref)}
	}
	refCommit, ok := anchorCommit(a.Ref)
	if !ok {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance.ref", Reason: "the ref anchors one commit, not a range"}
	}
	if a.Executable {
		if a.Gate == "" {
			return nil, &AcceptanceError{Subject: subject, Field: "acceptance.gate", Reason: "executable content requires review-gate evidence at every tier — no text becomes an executed command without a gate between (the provenance-gated relaxation waits for the tier system)"}
		}
		if !classify.IsAnchoredRef(a.Gate) {
			return nil, &AcceptanceError{Subject: subject, Field: "acceptance.gate", Reason: fmt.Sprintf("%q is not gate evidence (\"pr @ merged-commit\")", a.Gate)}
		}
		gateCommit, ok := anchorCommit(a.Gate)
		if !ok {
			return nil, &AcceptanceError{Subject: subject, Field: "acceptance.gate", Reason: "the gate anchors one merged commit, not a range"}
		}
		if gateCommit != refCommit {
			return nil, &AcceptanceError{Subject: subject, Field: "acceptance.gate", Reason: fmt.Sprintf("gate evidence is bound to the acceptance revision: the gate's merged commit %s must equal the ref's commit %s — an unrelated merged PR vouches for nothing", gateCommit, refCommit)}
		}
	} else if a.Gate != "" {
		return nil, &AcceptanceError{Subject: subject, Field: "acceptance.gate", Reason: "gate evidence is present iff the spec is executable; prose-only criteria carry none"}
	}
	return &a, nil
}

// ProposalVerbPrefix marks the inbound-proposal surface: request.*
// events may carry proposed acceptance prose, and nothing more.
const ProposalVerbPrefix = "request."

// armingKeys are the keys a proposal payload structurally cannot
// carry, at any depth: outside text can propose, never arm (III.F
// row 2). Scanning keys, not values, keeps prose free.
var armingKeys = map[string]bool{"executable": true, "gate": true}

// CheckProposalShape refuses a request.* payload that smuggles arming
// keys anywhere in its structure.
func CheckProposalShape(subject string, payload []byte) error {
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil
	}
	if key, found := findArmingKey(v); found {
		return &AcceptanceError{Subject: subject, Field: key, Reason: "proposals are data by construction: a request event can propose acceptance prose but never carry executable or gate keys — only a capability-checked contract.specified with gate evidence arms content"}
	}
	return nil
}

func findArmingKey(v any) (string, bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if armingKeys[k] {
				return k, true
			}
			if key, found := findArmingKey(child); found {
				return key, true
			}
		}
	case []any:
		for _, child := range t {
			if key, found := findArmingKey(child); found {
				return key, true
			}
		}
	}
	return "", false
}
