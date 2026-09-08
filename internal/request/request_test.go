package request

import (
	"strings"
	"testing"
)

// conformance: plans/os-48df10a2.md AC1 — the two shapes, each field
// held: the origin a token, the kind in the vocabulary, the reference
// a reference and never a body, the summary bounded, the subject the
// contract about names or system; the answer a position, an outcome,
// and the field its outcome needs and not the other's.
func TestShapes(t *testing.T) {
	good := `{"origin": "mirror-a", "kind": "mirror-edit", "reference": "cards/c-1.md @ 0123456", "summary": "a title edit", "about": "c-1"}`
	f, err := ParseFiled("c-1", []byte(good))
	if err != nil || f.About != "c-1" || f.Kind != "mirror-edit" {
		t.Fatalf("the good filing: %v %+v", err, f)
	}
	if _, err := ParseFiled("system", []byte(good)); err == nil {
		t.Error("about names c-1, so the subject is c-1")
	}
	digest := strings.Repeat("a", 64)
	if _, err := ParseFiled("system", []byte(`{"origin": "dash", "kind": "dashboard-action", "reference": "`+digest+`", "summary": "s"}`)); err != nil {
		t.Errorf("a digest is a reference: %v", err)
	}
	for name, body := range map[string]string{
		"trailing":    good + ` {}`,
		"unknown key": `{"origin": "m", "kind": "mirror-edit", "reference": "a @ 0123456", "summary": "s", "body": "x"}`,
		"no summary":  `{"origin": "m", "kind": "mirror-edit", "reference": "a @ 0123456", "summary": " "}`,
		"origin ws":   `{"origin": "a b", "kind": "mirror-edit", "reference": "a @ 0123456", "summary": "s"}`,
		"bare hash":   `{"origin": "m", "kind": "mirror-edit", "reference": "not a ref", "summary": "s"}`,
		"kind":        `{"origin": "m", "kind": "wish", "reference": "a @ 0123456", "summary": "s"}`,
		"long":        `{"origin": "m", "kind": "mirror-edit", "reference": "a @ 0123456", "summary": "` + strings.Repeat("s", MaxSummaryBytes+1) + `"}`,
	} {
		if _, err := ParseFiled("system", []byte(body)); err == nil {
			t.Errorf("%s: the filing parsed", name)
		} else if !strings.Contains(err.Error(), "request.filed on system refused") {
			t.Errorf("%s: the refusal names the verb and subject: %v", name, err)
		}
	}
	a, pos, err := ParseAnswered("system", []byte(`{"request": "12", "outcome": "filed", "intent": "14"}`))
	if err != nil || pos != 12 || a.Intent != "14" {
		t.Fatalf("the good answer: %v %d %+v", err, pos, a)
	}
	if _, pos, err := ParseAnswered("system", []byte(`{"request": " 3 ", "outcome": "declined", "reason": "no"}`)); err != nil || pos != 3 {
		t.Errorf("a padded position: %v %d", err, pos)
	}
	for name, body := range map[string]string{
		"negative":        `{"request": "-1", "outcome": "declined", "reason": "no"}`,
		"not a position":  `{"request": "latest", "outcome": "declined", "reason": "no"}`,
		"outcome":         `{"request": "1", "outcome": "maybe"}`,
		"filed intent":    `{"request": "1", "outcome": "filed"}`,
		"filed reason":    `{"request": "1", "outcome": "filed", "intent": "2", "reason": "x"}`,
		"declined why":    `{"request": "1", "outcome": "declined"}`,
		"declined intent": `{"request": "1", "outcome": "declined", "reason": "no", "intent": "2"}`,
		"extra":           `{"request": "1", "outcome": "declined", "reason": "no", "body": "x"}`,
	} {
		if _, _, err := ParseAnswered("system", []byte(body)); err == nil {
			t.Errorf("%s: the answer parsed", name)
		}
	}
	if b, err := RenderFiled(Filed{Origin: "m", Kind: "cross-repo", Reference: "x/c-1 @ 0123456", Summary: "s"}); err != nil || strings.Contains(string(b), "about") {
		t.Errorf("an empty about is omitted: %s %v", b, err)
	}
	if b, err := RenderAnswered(Answered{Request: "1", Outcome: "declined", Reason: "no"}); err != nil || strings.Contains(string(b), "intent") {
		t.Errorf("an empty intent is omitted: %s %v", b, err)
	}
}
