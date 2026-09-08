package approval

import (
	"strings"
	"testing"
)

// conformance: plans/os-5781a026.md D2 — the three shapes are strict:
// a request names a catalog-shaped verb, an actor and one bounded
// reason and never an approval verb; a grant cites a position and
// nothing else; a denial cites a position and says why.
func TestShapesAreStrict(t *testing.T) {
	good, err := ParseRequested("c-1", []byte(`{"verb": "claim.taken", "actor": "fp-1", "reason": "the drill asks"}`))
	if err != nil || good.Verb != "claim.taken" || good.Actor != "fp-1" {
		t.Fatalf("a well-formed request parses: %v %+v", err, good)
	}
	long := strings.Repeat("r", MaxReasonBytes+1)
	for name, raw := range map[string]string{
		"unknown field":  `{"verb": "claim.taken", "actor": "fp-1", "reason": "r", "extra": 1}`,
		"trailing data":  `{"verb": "claim.taken", "actor": "fp-1", "reason": "r"} x`,
		"no verb":        `{"actor": "fp-1", "reason": "r"}`,
		"verb with room": `{"verb": "claim taken", "actor": "fp-1", "reason": "r"}`,
		"approval verb":  `{"verb": "approval.granted", "actor": "fp-1", "reason": "r"}`,
		"no actor":       `{"verb": "claim.taken", "actor": " ", "reason": "r"}`,
		"empty reason":   `{"verb": "claim.taken", "actor": "fp-1", "reason": ""}`,
		"two lines":      `{"verb": "claim.taken", "actor": "fp-1", "reason": "a\nb"}`,
		"long reason":    `{"verb": "claim.taken", "actor": "fp-1", "reason": "` + long + `"}`,
		"not an object":  `[]`,
	} {
		if _, err := ParseRequested("c-1", []byte(raw)); err == nil {
			t.Errorf("%s must refuse", name)
		} else if e, ok := err.(*Error); !ok || e.Verb != RequestedVerb || e.Subject != "c-1" {
			t.Errorf("%s refuses with the typed error naming the verb and subject, got %v", name, err)
		}
	}

	if a, pos, err := ParseAnswer(GrantedVerb, "c-1", []byte(`{"request": "7"}`)); err != nil || pos != 7 || a.Reason != "" {
		t.Fatalf("a grant cites a position: %v %d %+v", err, pos, a)
	}
	if _, pos, err := ParseAnswer(DeniedVerb, "c-1", []byte(`{"request": "7", "reason": "not now"}`)); err != nil || pos != 7 {
		t.Fatalf("a denial cites a position and says why: %v %d", err, pos)
	}
	for name, tc := range map[string]struct{ verb, raw string }{
		"grant with a reason":   {GrantedVerb, `{"request": "7", "reason": "why not"}`},
		"denial without reason": {DeniedVerb, `{"request": "7"}`},
		"not a position":        {GrantedVerb, `{"request": "seven"}`},
		"negative position":     {GrantedVerb, `{"request": "-1"}`},
		"padded position":       {GrantedVerb, `{"request": " 7"}`},
		"unknown field":         {DeniedVerb, `{"request": "7", "reason": "r", "verb": "x"}`},
		"not an answer verb":    {RequestedVerb, `{"request": "7"}`},
	} {
		if _, _, err := ParseAnswer(tc.verb, "c-1", []byte(tc.raw)); err == nil {
			t.Errorf("%s must refuse", name)
		}
	}
}

// The renderers produce what the parsers accept, and the family is
// closed: three verbs, each recognized, nothing else.
func TestRenderRoundTripsAndTheFamilyIsClosed(t *testing.T) {
	raw, err := RenderRequested(Requested{Verb: "claim.taken", Actor: "fp-1", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRequested("system", raw); err != nil {
		t.Fatalf("the rendered request parses: %v", err)
	}
	g, _ := RenderGranted(3)
	if _, pos, err := ParseAnswer(GrantedVerb, "system", g); err != nil || pos != 3 {
		t.Fatalf("the rendered grant parses: %v %d", err, pos)
	}
	d, _ := RenderDenied(3, "no")
	if _, pos, err := ParseAnswer(DeniedVerb, "system", d); err != nil || pos != 3 {
		t.Fatalf("the rendered denial parses: %v %d", err, pos)
	}
	if len(Verbs()) != 3 {
		t.Fatalf("three verbs: %v", Verbs())
	}
	for _, v := range Verbs() {
		if !IsApprovalVerb(v) {
			t.Errorf("%s is in the family", v)
		}
	}
	if IsApprovalVerb("escalation.raised") || IsApprovalVerb("approval") {
		t.Fatal("the family is closed")
	}
	if msg := (&Error{Verb: GrantedVerb, Subject: "c-1", Reason: "x"}).Error(); !strings.Contains(msg, "approval.granted on c-1 refused: x") {
		t.Fatalf("the error names the verb, the subject and the reason: %s", msg)
	}
}
