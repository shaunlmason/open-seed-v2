package escalation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/escalation"
)

const good = `{"question": "which base?", "options": [{"id": "a", "choice": "main"}, {"id": "b", "choice": "the release branch"}]}`

func TestParseAcceptsAMinimalDecision(t *testing.T) {
	e, err := escalation.Parse("c-1", []byte(good))
	if err != nil {
		t.Fatalf("a two-option question is the minimal decision: %v", err)
	}
	if e.Question != "which base?" || len(e.Options) != 2 {
		t.Fatalf("parsed wrong: %+v", e)
	}
	if !e.Offers("a") || !e.Offers("b") || e.Offers("c") {
		t.Fatalf("Offers must answer from the option set: %+v", e)
	}
	if got := strings.Join(e.IDs(), ","); got != "a,b" {
		t.Fatalf("IDs names what IS legal, got %q", got)
	}
}

// conformance: III — "every escalation is one packet + one question +
// one decision; transcript-dumping is a defect". Each planted shape
// refuses, and the refusal names the offending PART, so a caller is
// told what to fix rather than that something is wrong.
func TestPlantedShapesRefuseByPart(t *testing.T) {
	for _, tc := range []struct{ name, raw, part string }{
		{"no question", `{"options": [{"id": "a", "choice": "x"}, {"id": "b", "choice": "y"}]}`, "question"},
		{"empty question", `{"question": "  ", "options": [{"id": "a", "choice": "x"}, {"id": "b", "choice": "y"}]}`, "question"},
		{"no options", `{"question": "q?"}`, "options"},
		{"null options", `{"question": "q?", "options": null}`, "options"},
		{"one option", `{"question": "q?", "options": [{"id": "a", "choice": "x"}]}`, "options"},
		{"zero options", `{"question": "q?", "options": []}`, "options"},
		{"blank id", `{"question": "q?", "options": [{"id": " ", "choice": "x"}, {"id": "b", "choice": "y"}]}`, "options"},
		{"blank choice", `{"question": "q?", "options": [{"id": "a", "choice": ""}, {"id": "b", "choice": "y"}]}`, "options"},
		{"duplicate id", `{"question": "q?", "options": [{"id": "a", "choice": "x"}, {"id": "a", "choice": "y"}]}`, "options"},
		{"unknown key", `{"question": "q?", "options": [], "transcript": "..."}`, "escalation"},
		{"unknown option key", `{"question": "q?", "options": [{"id": "a", "choice": "x", "why": "no"}, {"id": "b", "choice": "y"}]}`, "escalation"},
		{"not an object", `["q?"]`, "escalation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := escalation.Parse("c-1", []byte(tc.raw))
			var se *escalation.Error
			if !errors.As(err, &se) {
				t.Fatalf("must refuse as a shape error, got %v", err)
			}
			if se.Part != tc.part {
				t.Errorf("refusal names part %q, want %q: %v", se.Part, tc.part, se)
			}
			if !strings.Contains(se.Error(), "c-1") {
				t.Errorf("the refusal names its subject: %v", se)
			}
		})
	}
}

// The two-option floor is the plan's D5 and its most arguable
// decision, so it is asserted directly rather than left implicit in
// the table above: one option is not a decision, and the refusal says
// why rather than merely counting.
func TestTheTwoOptionFloorSaysWhy(t *testing.T) {
	_, err := escalation.Parse("c-1", []byte(`{"question": "q?", "options": [{"id": "a", "choice": "x"}]}`))
	if err == nil {
		t.Fatal("one option must refuse")
	}
	if !strings.Contains(err.Error(), "design work") {
		t.Errorf("the refusal must say a question with no answer set is a request for design work, got %v", err)
	}
	if escalation.MinOptions != 2 {
		t.Errorf("the floor is two: %d", escalation.MinOptions)
	}
}

func TestFromPayloadDistinguishesAbsentFromMalformed(t *testing.T) {
	// Absent is legal here: the boundary decides obligation, this
	// package decides shape.
	e, present, err := escalation.FromPayload("c-1", []byte(`{"fence": "12"}`))
	if err != nil || present || e != nil {
		t.Fatalf("an absent question is not a shape error: %v %v %v", e, present, err)
	}
	// Present-but-malformed reports BOTH, so a caller can tell "no
	// question" from "a broken one" — the distinction the raise rule
	// needs to refuse only the second.
	_, present, err = escalation.FromPayload("c-1", []byte(`{"escalation": {"question": "q?"}}`))
	if !present || err == nil {
		t.Fatalf("a malformed question is present and refused: present=%v err=%v", present, err)
	}
	if _, _, err := escalation.FromPayload("c-1", []byte(`["not an object"]`)); err == nil {
		t.Fatal("a non-object payload must refuse")
	}
	e, present, err = escalation.FromPayload("c-1", []byte(`{"escalation": `+good+`}`))
	if err != nil || !present || e.Question != "which base?" {
		t.Fatalf("a well-shaped question parses: %v %v %v", e, present, err)
	}
}

func TestCarriesQuestionIsExactlyTheTwoVerbs(t *testing.T) {
	if !escalation.CarriesQuestion(escalation.RaiseVerb) || !escalation.CarriesQuestion("claim.parked") {
		t.Fatal("the raise and the park may carry a question")
	}
	for _, v := range []string{"claim.released", "claim.reaped", "submission.made", "contract.blocked", escalation.AnswerVerb} {
		if escalation.CarriesQuestion(v) {
			t.Errorf("%s must not carry a question: a question rides the raise, or the ONE exit that also asks something", v)
		}
	}
}
