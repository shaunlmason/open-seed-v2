// Package erasure is the erasure fact (plans/os-db5cd353.md; SEED-NEXT.md
// III.A row 7): artifact.erased, the signed record that an operator
// erased an artifact the chain references by digest. The chain
// references bodies by hash and never carries them, so erasing the
// bytes never breaks verification; what the row asks beside that is
// that the erasure be an attributable event, which is this record.
// It is additive catalog growth active from seed/1 under the operator
// grant, changing no lifecycle state.
package erasure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	// Verb is the erasure fact.
	Verb = "artifact.erased"

	// SystemSubject is the subject of an erasure whose reference the
	// operator attests rather than the fold: an artifact the chain
	// references from a payload no contract's fold indexes by digest.
	SystemSubject = "system"

	// MaxReasonBytes bounds the reason: one line naming the obligation
	// honored, never a body.
	MaxReasonBytes = 200
)

// Erased is artifact.erased's strict payload.
type Erased struct {
	Artifact string `json:"artifact"`
	Reason   string `json:"reason"`
}

// Error is an erasure refusal: the shape, the reference or the once is
// wrong.
type Error struct {
	Subject string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s on %s refused: %s (spec/protocol.md, Erasure)", Verb, e.Subject, e.Reason)
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IsDigest reports whether s is a lowercase-hex sha256, the one form
// the chain references an artifact by.
func IsDigest(s string) bool { return digestRE.MatchString(s) }

func strict(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the payload")
	}
	return nil
}

// Parse holds an artifact.erased payload to its shape: the artifact a
// lowercase-hex sha256, the reason one non-empty bounded line.
func Parse(subject string, raw []byte) (*Erased, error) {
	var p Erased
	if err := strict(raw, &p); err != nil {
		return nil, &Error{Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {artifact, reason}: %v", err)}
	}
	if !IsDigest(p.Artifact) {
		return nil, &Error{Subject: subject, Reason: fmt.Sprintf("artifact %q is not a lowercase-hex sha256 — the chain references an artifact by its digest and the erasure names that digest", p.Artifact)}
	}
	if strings.TrimSpace(p.Reason) == "" {
		return nil, &Error{Subject: subject, Reason: "reason is empty — an erasure names the obligation it honors"}
	}
	if strings.ContainsAny(p.Reason, "\r\n") {
		return nil, &Error{Subject: subject, Reason: "reason is one line"}
	}
	if len(p.Reason) > MaxReasonBytes {
		return nil, &Error{Subject: subject, Reason: fmt.Sprintf("reason is %d bytes, bound %d — an erasure names its obligation, it never carries a body", len(p.Reason), MaxReasonBytes)}
	}
	return &p, nil
}

// Render is the payload for an erasure, in the shape Parse holds.
func Render(e Erased) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(SystemSubject, b); err != nil {
		return nil, err
	}
	return b, nil
}
