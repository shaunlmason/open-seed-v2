// Package halt implements halt semantics as validation rules (charter
// Part II section 1, "Genesis and halt"; plans/os-bce3fb98.md):
// system.halt.declared stops admission of everything except an operator's
// system.halt.lifted. Halt state is derived solely from the chain; there
// is no flag file or second store. This package is a pure library: the
// Phase 2 admission rule set (internal/admit) calls StateAt and Check
// unchanged. Whether the declarer or lifter holds operator standing is a
// grant check that lands in Phase 3; until then this package validates
// verb shape and sequencing only, and says so rather than faking an
// authorization check. Halt gates admission of new events, never the
// validity of admitted history: a halt window inside a chain replays
// green under ledger verification.
package halt

import (
	"encoding/json"
	"fmt"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

// Verbs, from the charter's system.* catalog.
const (
	DeclareVerb = "system.halt.declared"
	LiftVerb    = "system.halt.lifted"
)

// State is the projected halt state: derived by replay, never stored.
type State struct {
	Halted bool
	By     string
	Reason string
}

// HaltedError is the typed refusal Check returns while halted: it carries
// the halting actor and the declaration reason so the envelope layer can
// map it to the distinct halted exit code (7, spec/envelope.md).
type HaltedError struct {
	By     string
	Reason string
}

func (e *HaltedError) Error() string {
	return fmt.Sprintf("admission is halted by %s (%s) — only %s may append", e.By, e.Reason, LiftVerb)
}

// declarePayload is the halt.declared payload shape: a required reason.
type declarePayload struct {
	Reason string `json:"reason"`
}

// ValidateShape checks the halt verbs' event shape: subject system and a
// schema-valid payload (declared requires a reason; lifted requires an
// empty object). Non-halt verbs pass through untouched.
func ValidateShape(e *event.Event) error {
	switch e.Verb {
	case DeclareVerb:
		if e.Subject != "system" {
			return fmt.Errorf("%s subject %q is not system", DeclareVerb, e.Subject)
		}
		var p declarePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("%s payload does not parse: %w", DeclareVerb, err)
		}
		if p.Reason == "" {
			return fmt.Errorf("%s payload must carry a reason", DeclareVerb)
		}
	case LiftVerb:
		if e.Subject != "system" {
			return fmt.Errorf("%s subject %q is not system", LiftVerb, e.Subject)
		}
		var p map[string]json.RawMessage
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("%s payload does not parse: %w", LiftVerb, err)
		}
		if p == nil || len(p) > 0 {
			return fmt.Errorf("%s payload must be an empty object", LiftVerb)
		}
	}
	return nil
}

// StateAt replays the records and returns the halt state after the last
// one. Malformed halt events are skipped for state purposes (admission
// refuses them at the boundary; replay must not wedge on history it did
// not admit).
func StateAt(records []*event.Record) State {
	var s State
	for _, rec := range records {
		e := &rec.Event
		if ValidateShape(e) != nil {
			continue
		}
		switch e.Verb {
		case DeclareVerb:
			var p declarePayload
			_ = json.Unmarshal(e.Payload, &p)
			s = State{Halted: true, By: e.Actor, Reason: p.Reason}
		case LiftVerb:
			s = State{}
		}
	}
	return s
}

// Check is the admission rule: while halted, every verb refuses except the
// lift, and that refusal dominates shape validation (a malformed non-lift
// proposal under halt is refused as halted, so the envelope layer maps it
// to the halted exit code rather than a generic shape refusal). The lift
// itself, and every verb while not halted, still passes shape validation
// (other rules still apply at the boundary).
func Check(s State, proposed *event.Event) error {
	if s.Halted && proposed.Verb != LiftVerb {
		return &HaltedError{By: s.By, Reason: s.Reason}
	}
	return ValidateShape(proposed)
}
