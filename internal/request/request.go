// Package request is the ingress family (plans/os-48df10a2.md;
// spec/requests.md; SEED-NEXT.md §II.15, III.N row 4, III.J row 2):
// request.filed, the one verb a proposal from a projection surface or
// another deployment enters the ledger by, a fact that changes no
// state and grants nothing; and request.answered, the dispatcher's
// close of one, citing the intent it filed for it or the reason it
// declined.
package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/classify"
)

const (
	FiledVerb    = "request.filed"
	AnsweredVerb = "request.answered"

	// SystemSubject is the subject of a request that concerns no
	// contract on the chain.
	SystemSubject = "system"

	// MaxSummaryBytes bounds the summary: a request carries a
	// reference to what was proposed, never the proposal.
	MaxSummaryBytes = 200
)

// Kinds is the vocabulary of what a request is.
var Kinds = []string{"mirror-edit", "dashboard-action", "cross-repo"}

// Outcomes is the vocabulary of an answer.
var Outcomes = []string{"filed", "declined"}

// Filed is request.filed's strict payload.
type Filed struct {
	Origin    string `json:"origin"`
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Summary   string `json:"summary"`
	About     string `json:"about,omitempty"`
}

// Answered is request.answered's strict payload.
type Answered struct {
	Request string `json:"request"`
	Outcome string `json:"outcome"`
	Intent  string `json:"intent,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Error is a request refusal: the shape or the citation is wrong.
type Error struct {
	Verb    string
	Subject string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s on %s refused: %s (spec/requests.md)", e.Verb, e.Subject, e.Reason)
}

// strict decodes exactly one JSON object into the struct pointer
// `into`, refusing unknown fields and anything after the object but
// whitespace.
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

func known(v string, vocabulary []string) bool {
	for _, k := range vocabulary {
		if k == v {
			return true
		}
	}
	return false
}

// ParseFiled holds a request.filed payload to its shape: the origin a
// non-empty token, the kind in the vocabulary, the reference a
// commit-anchored ref or an artifact digest (never a body), the
// summary non-empty and bounded, and the subject the contract `about`
// names or system when it names none.
func ParseFiled(subject string, raw []byte) (*Filed, error) {
	var p Filed
	if err := strict(raw, &p); err != nil {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {origin, kind, reference, summary, about?}: %v", err)}
	}
	if strings.TrimSpace(p.Origin) == "" || strings.ContainsAny(p.Origin, " \t\r\n") {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: "origin names the surface or remote the proposal came from, one token"}
	}
	if !known(p.Kind, Kinds) {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: fmt.Sprintf("kind %q is not one of %s", p.Kind, strings.Join(Kinds, ", "))}
	}
	if !classify.IsReference(p.Reference) {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: fmt.Sprintf("reference %q is neither a commit-anchored ref (\"path @ commit\") nor an artifact digest — a request names what was proposed, it never carries it", p.Reference)}
	}
	if strings.TrimSpace(p.Summary) == "" {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: "summary is empty"}
	}
	if len(p.Summary) > MaxSummaryBytes {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: fmt.Sprintf("summary is %d bytes, bound %d — a request is a notice, never the proposal", len(p.Summary), MaxSummaryBytes)}
	}
	want := SystemSubject
	if strings.TrimSpace(p.About) != "" {
		want = p.About
	}
	if subject != want {
		return nil, &Error{Verb: FiledVerb, Subject: subject, Reason: fmt.Sprintf("the subject is the contract about names (%q) or system when it names none, got %q", want, subject)}
	}
	return &p, nil
}

// ParseAnswered holds a request.answered payload to its shape: the
// request a chain position, the outcome in the vocabulary, filed with
// the intent's position and declined with a reason.
func ParseAnswered(subject string, raw []byte) (*Answered, int, error) {
	var p Answered
	if err := strict(raw, &p); err != nil {
		return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: fmt.Sprintf("the payload is the strict object {request, outcome, intent?, reason?}: %v", err)}
	}
	pos, err := strconv.Atoi(strings.TrimSpace(p.Request))
	if err != nil || pos < 0 {
		return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: fmt.Sprintf("request %q is not a chain position", p.Request)}
	}
	if !known(p.Outcome, Outcomes) {
		return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: fmt.Sprintf("outcome %q is not one of %s", p.Outcome, strings.Join(Outcomes, ", "))}
	}
	switch p.Outcome {
	case "filed":
		if _, err := strconv.Atoi(strings.TrimSpace(p.Intent)); err != nil || strings.TrimSpace(p.Intent) == "" {
			return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: "filed cites the position of the intent.filed the dispatcher appended for the request"}
		}
		if strings.TrimSpace(p.Reason) != "" {
			return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: "filed carries the intent, not a reason"}
		}
	case "declined":
		if strings.TrimSpace(p.Reason) == "" {
			return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: "declined says why"}
		}
		if strings.TrimSpace(p.Intent) != "" {
			return nil, 0, &Error{Verb: AnsweredVerb, Subject: subject, Reason: "declined files no intent"}
		}
	}
	return &p, pos, nil
}

// RenderFiled and RenderAnswered produce the strict payloads.
func RenderFiled(f Filed) ([]byte, error) { return json.Marshal(f) }

func RenderAnswered(a Answered) ([]byte, error) { return json.Marshal(a) }
