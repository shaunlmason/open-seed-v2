// Package approval is the require-approval mode of per-verb policy
// (plans/os-5781a026.md; spec/protocol.md "Per-verb approval";
// SEED-NEXT.md §II.14, III.L row 4): three fact verbs, additive catalog
// growth active from seed/1. approval.requested is the request an
// actor's governed act waits on, a fact that changes no state and
// grants nothing; approval.granted and approval.denied are the
// operator's attributable answers, citing the request. The declaration
// says which verbs need one (posture.Guardrails.Approvals); the fold
// keeps the facts and spends each grant on exactly one act; the
// admission rule holds the shapes, the citations and the policy.
package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	RequestedVerb = "approval.requested"
	GrantedVerb   = "approval.granted"
	DeniedVerb    = "approval.denied"

	// SystemSubject is the subject of an approval whose act concerns
	// no contract on the chain.
	SystemSubject = "system"

	// MaxReasonBytes bounds the reason: one line, never a transcript,
	// the request.filed summary's posture.
	MaxReasonBytes = 200
)

// Verbs is the family, in catalog order.
func Verbs() []string { return []string{RequestedVerb, GrantedVerb, DeniedVerb} }

// IsApprovalVerb reports whether the verb is one of the three.
func IsApprovalVerb(verb string) bool {
	return verb == RequestedVerb || verb == GrantedVerb || verb == DeniedVerb
}

// Requested is approval.requested's strict payload: the governed verb,
// the fingerprint of the key that will perform it, and why.
type Requested struct {
	Verb   string `json:"verb"`
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

// Answer is approval.granted's and approval.denied's strict payload:
// the request's chain position, and on a denial the reason.
type Answer struct {
	Request string `json:"request"`
	Reason  string `json:"reason,omitempty"`
}

// Error is an approval refusal: the shape or the citation is wrong.
type Error struct {
	Verb    string
	Subject string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s on %s refused: %s (spec/protocol.md, \"Per-verb approval\")", e.Verb, e.Subject, e.Reason)
}

// strict decodes exactly one JSON object into the struct pointer,
// refusing unknown fields and anything after the object but whitespace.
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

func token(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.ContainsAny(s, " \t\r\n")
}

// oneLine holds a reason to one bounded line.
func oneLine(verb, subject, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return &Error{Verb: verb, Subject: subject, Reason: "reason is empty: an approval names why, in one line"}
	}
	if strings.ContainsAny(reason, "\r\n") {
		return &Error{Verb: verb, Subject: subject, Reason: "reason is one line, never a transcript"}
	}
	if len(reason) > MaxReasonBytes {
		return &Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("reason is %d bytes, bound %d: one line, never a transcript", len(reason), MaxReasonBytes)}
	}
	return nil
}

// ParseRequested holds an approval.requested payload to its shape: the
// verb and the actor non-empty tokens, the reason one bounded line.
// Whether the verb is a catalog verb and the actor an enrolled key is
// the admission rule's to judge, where the catalog and the keyring
// are; this package validates shape.
func ParseRequested(subject string, raw []byte) (*Requested, error) {
	var p Requested
	if err := strict(raw, &p); err != nil {
		return nil, &Error{Verb: RequestedVerb, Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {verb, actor, reason}: %v", err)}
	}
	if !token(p.Verb) {
		return nil, &Error{Verb: RequestedVerb, Subject: subject, Reason: "verb names the governed act, one catalog verb"}
	}
	if IsApprovalVerb(p.Verb) {
		return nil, &Error{Verb: RequestedVerb, Subject: subject, Reason: fmt.Sprintf("verb %q is an approval verb, and the three are never governed: an approval that needed an approval would wait on itself", p.Verb)}
	}
	if !token(p.Actor) {
		return nil, &Error{Verb: RequestedVerb, Subject: subject, Reason: "actor names the fingerprint of the key that will act"}
	}
	if err := oneLine(RequestedVerb, subject, p.Reason); err != nil {
		return nil, err
	}
	return &p, nil
}

// ParseAnswer holds an approval.granted or approval.denied payload to
// its shape: the request a chain position; a grant carries no reason
// and a denial says why.
func ParseAnswer(verb, subject string, raw []byte) (*Answer, int, error) {
	if verb != GrantedVerb && verb != DeniedVerb {
		return nil, 0, &Error{Verb: verb, Subject: subject, Reason: "not an answer verb"}
	}
	var p Answer
	if err := strict(raw, &p); err != nil {
		return nil, 0, &Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {request, reason?}: %v", err)}
	}
	pos, err := strconv.Atoi(strings.TrimSpace(p.Request))
	if err != nil || pos < 0 || strings.TrimSpace(p.Request) != p.Request {
		return nil, 0, &Error{Verb: verb, Subject: subject, Reason: fmt.Sprintf("request %q is not a chain position", p.Request)}
	}
	switch verb {
	case GrantedVerb:
		if strings.TrimSpace(p.Reason) != "" {
			return nil, 0, &Error{Verb: verb, Subject: subject, Reason: "a grant carries the request and nothing else: the reason was the request's"}
		}
	case DeniedVerb:
		if err := oneLine(verb, subject, p.Reason); err != nil {
			return nil, 0, err
		}
	}
	return &p, pos, nil
}

// RenderRequested, RenderGranted and RenderDenied produce the strict
// payloads.
func RenderRequested(r Requested) ([]byte, error) { return json.Marshal(r) }

func RenderGranted(request int) ([]byte, error) {
	return json.Marshal(Answer{Request: strconv.Itoa(request)})
}

func RenderDenied(request int, reason string) ([]byte, error) {
	return json.Marshal(Answer{Request: strconv.Itoa(request), Reason: reason})
}
