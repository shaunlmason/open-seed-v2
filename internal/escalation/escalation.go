// Package escalation implements the shape of a blocked(needs-you):
// the question a human gate is asked and the closed set of answers it
// may choose between. Design authority: SEED-NEXT.md §II.7 and §II.11
// ("packet + question + minimal decision ... never a transcript"),
// spec/escalation.md; plan plans/os-f781f0da.md.
//
// The shape is deliberately narrow. A free-text question with no
// answer set is an invitation to design work, which is what "minimal
// decision" excludes, so options are required and the answer cites
// one by id. That is what makes "one decision, never a transcript"
// checkable rather than aspirational.
package escalation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Key is the payload key carrying the question on a raise, and the
// cited position on an answer. One key, two shapes: on a raise it is
// an object, on an answer a position string.
const Key = "escalation"

// MinOptions is the floor on a minimal decision. Two, because one
// option is not a decision and none is a request for design work.
const MinOptions = 2

// Option is one answer the raiser is offering.
type Option struct {
	ID     string `json:"id"`
	Choice string `json:"choice"`
}

// Escalation is the raise's question and its closed option set.
type Escalation struct {
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

// Error is a shape refusal naming the offending part, the packet.Error
// posture: the caller is told which part is wrong, not merely that
// something is.
type Error struct {
	Subject string
	Part    string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("escalation on %s: %s: %s", e.Subject, e.Part, e.Reason)
}

// RaiseVerb is the standalone raise. From in_progress an escalation
// rides claim.parked instead, because nothing new may leave that
// state: the four deliberate exits are pinned by self-validation and
// III.F depends on that set being closed (spec/lifecycle.md).
const RaiseVerb = "escalation.raised"

// AnswerVerb closes a standing escalation. Operator only, no
// fallback: a machine lane answering a human gate inverts the
// charter's humans-hold-gates rule (§I.3).
const AnswerVerb = "decision.recorded"

// CarriesQuestion reports whether the verb's payload may carry a
// question. claim.parked MAY (an exit from a held window that also
// asks something); escalation.raised MUST.
func CarriesQuestion(verb string) bool {
	return verb == RaiseVerb || verb == "claim.parked"
}

// FromPayload extracts the question. present is false when the key is
// absent, which is legal on claim.parked and refused on the raise by
// the admission rule, not here: this package validates shape, the
// boundary decides obligation.
func FromPayload(subject string, payload []byte) (e *Escalation, present bool, err error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, false, &Error{Subject: subject, Part: Key, Reason: "payload is not an object"}
	}
	raw, ok := m[Key]
	if !ok {
		return nil, false, nil
	}
	parsed, err := Parse(subject, raw)
	if err != nil {
		return nil, true, err
	}
	return parsed, true, nil
}

// Parse validates the strict shape, part by part.
func Parse(subject string, raw []byte) (*Escalation, error) {
	var e Escalation
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return nil, &Error{Subject: subject, Part: Key, Reason: fmt.Sprintf("strict shape: %v", err)}
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, &Error{Subject: subject, Part: Key, Reason: "escalation is not an object"}
	}
	for _, k := range []string{"question", "options"} {
		if _, ok := keys[k]; !ok {
			return nil, &Error{Subject: subject, Part: k, Reason: "the part is absent — an escalation is one packet, one question, one decision"}
		}
	}
	// Presence is not shape: a null decodes into the same nil slice an
	// absent key does, the packets.md precedent.
	if v := bytes.TrimLeft(keys["options"], " \t\r\n"); len(v) == 0 || v[0] != '[' {
		return nil, &Error{Subject: subject, Part: "options", Reason: "the part is not an array"}
	}
	if strings.TrimSpace(e.Question) == "" {
		return nil, &Error{Subject: subject, Part: "question", Reason: "a human's unit of interruption is one question — an empty one asks nothing"}
	}
	if len(e.Options) < MinOptions {
		return nil, &Error{Subject: subject, Part: "options", Reason: fmt.Sprintf("%d option(s): a minimal decision offers at least %d, because a question with no answer set is a request for design work, not a decision", len(e.Options), MinOptions)}
	}
	seen := map[string]bool{}
	for i, o := range e.Options {
		id := strings.TrimSpace(o.ID)
		if id == "" {
			return nil, &Error{Subject: subject, Part: "options", Reason: fmt.Sprintf("entry %d has no id — the answer cites an option by id", i)}
		}
		if strings.TrimSpace(o.Choice) == "" {
			return nil, &Error{Subject: subject, Part: "options", Reason: fmt.Sprintf("entry %d (%q) has no choice text", i, id)}
		}
		if seen[id] {
			return nil, &Error{Subject: subject, Part: "options", Reason: fmt.Sprintf("entry %d repeats id %q — an answer citing it would be ambiguous", i, id)}
		}
		seen[id] = true
	}
	return &e, nil
}

// Offers reports whether the option set contains the id.
func (e *Escalation) Offers(id string) bool {
	for _, o := range e.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}

// IDs lists the offered option ids, for a refusal that names what IS
// legal (spec/refusals.md).
func (e *Escalation) IDs() []string {
	out := make([]string, 0, len(e.Options))
	for _, o := range e.Options {
		out = append(out, o.ID)
	}
	return out
}
