package erasure

import (
	"strings"
	"testing"
)

// conformance: III.A row 7 — the erasure record's shape is strict: a
// digest, a bounded one-line reason, nothing else.
func TestParseIsStrict(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	good := `{"artifact": "` + digest + `", "reason": "a retention obligation"}`
	p, err := Parse("c-1", []byte(good))
	if err != nil || p.Artifact != digest || p.Reason != "a retention obligation" {
		t.Fatalf("a well-formed erasure parses: %v %+v", err, p)
	}
	for name, raw := range map[string]string{
		"unknown field":     `{"artifact": "` + digest + `", "reason": "x", "body": "no"}`,
		"trailing data":     good + ` {}`,
		"not a digest":      `{"artifact": "sha256:` + digest + `", "reason": "x"}`,
		"uppercase digest":  `{"artifact": "` + strings.ToUpper(digest) + `", "reason": "x"}`,
		"empty reason":      `{"artifact": "` + digest + `", "reason": "  "}`,
		"multi-line reason": `{"artifact": "` + digest + `", "reason": "a\nb"}`,
		"long reason":       `{"artifact": "` + digest + `", "reason": "` + strings.Repeat("r", MaxReasonBytes+1) + `"}`,
		"missing artifact":  `{"reason": "x"}`,
	} {
		if _, err := Parse("c-1", []byte(raw)); err == nil {
			t.Errorf("%s must refuse", name)
		} else if e, ok := err.(*Error); !ok || e.Subject != "c-1" || !strings.Contains(err.Error(), Verb) {
			t.Errorf("%s refuses as an erasure error naming the verb and subject: %v", name, err)
		}
	}
}

func TestRenderRoundTrips(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	b, err := Render(Erased{Artifact: digest, Reason: "the subject asked"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(SystemSubject, b)
	if err != nil || p.Artifact != digest {
		t.Fatalf("rendered payload parses: %v %+v", err, p)
	}
	if _, err := Render(Erased{Artifact: "nope", Reason: "x"}); err == nil {
		t.Fatal("render holds the same shape as parse")
	}
	if !IsDigest(digest) || IsDigest("x") {
		t.Fatal("IsDigest is the lowercase-hex sha256 form")
	}
}
