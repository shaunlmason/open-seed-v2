package posture

// The preseed validator's refusals (plans/os-f262585a.md D1). Every one
// of these is a seed.json a deployment would otherwise run on, so the
// assertion is the message an operator reads: it has to name the block
// and what it wanted, not just fail.

import (
	"strings"
	"testing"
)

func refusal(t *testing.T, body, want string) {
	t.Helper()
	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatalf("this declaration must be refused:\n%s", body)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal must name %q, got: %v", want, err)
	}
}

func TestBoundaryBlockIsHeldToItsShape(t *testing.T) {
	const head = `{"posture": "cooperative", "boundary": `
	refusal(t, head+`{"accepts": [], "ingress": "origin"}}`, "boundary.accepts names the request kinds")
	refusal(t, head+`{"accepts": ["contract", "contract"], "ingress": "origin"}}`, "distinct non-empty tokens")
	refusal(t, head+`{"accepts": ["  "], "ingress": "origin"}}`, "distinct non-empty tokens")
	refusal(t, head+`{"accepts": ["two words"], "ingress": "origin"}}`, "distinct non-empty tokens")
	refusal(t, head+`{"accepts": ["contract"], "ingress": "   "}}`, "boundary.ingress names the remote")

	cfg, err := Parse([]byte(head + `{"accepts": ["contract", "question"], "ingress": "https://forge.example/seed"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Boundary.Accepts) != 2 || cfg.Boundary.Ingress == "" {
		t.Errorf("a well-formed boundary block round trips, got %+v", cfg.Boundary)
	}
	// An absent block is not a malformed one.
	if cfg, err := Parse([]byte(`{"posture": "cooperative"}`)); err != nil || cfg.Boundary != nil {
		t.Errorf("no boundary block is no boundary, got %+v %v", cfg, err)
	}
}

func TestGovernanceBlockIsHeldToTheChartersOneProcess(t *testing.T) {
	const head = `{"posture": "cooperative", "governance": `
	refusal(t, head+`{"root": "  ", "change_process": "pr+owner-review"}}`, "governance.root names")
	refusal(t, head+`{"root": "fp-root", "change_process": "trust-me"}}`, "the one process the charter names")
	refusal(t, head+`{"root": "fp-root", "change_process": "pr+owner-review", "owners": ["@org/gov", " "]}}`, "empty identity")

	if _, err := Parse([]byte(head + `{"root": "fp-root", "change_process": "pr+owner-review", "owners": ["@org/gov"]}}`)); err != nil {
		t.Fatalf("a well-formed governance block parses: %v", err)
	}
}

func TestGuardrailsAreHeldToTierAndPathShape(t *testing.T) {
	const head = `{"posture": "cooperative", "guardrails": `
	refusal(t, head+`{"squads": {"  ": {"default": "trivial", "max_agent": "standard"}}}}`, "empty squad name")
	refusal(t, head+`{"squads": {"core": {"default": "trivial"}}}}`, "declares both default and max_agent")
	refusal(t, head+`{"squads": {"core": {"default": "trivial", "max_agent": "standard", "racing": {"racers": 1, "cost": "x"}}}}}`, "a race is two or more claims")
	refusal(t, head+`{"squads": {"core": {"default": "trivial", "max_agent": "standard", "racing": {"racers": 2, "cost": " "}}}}}`, "in the operator's words")
	refusal(t, head+`{"paths": [{"prefix": "/next", "min": "deep"}]}}`, "clean repository-relative prefixes")
	refusal(t, head+`{"paths": [{"prefix": "../etc", "min": "deep"}]}}`, "clean repository-relative prefixes")
	refusal(t, head+`{"paths": [{"prefix": "spec", "min": "  "}]}}`, "clean repository-relative prefixes")
}

func TestTeamsAreHeldToDistinctNamedSquadsWithLanes(t *testing.T) {
	const head = `{"posture": "cooperative", "teams": `
	refusal(t, head+`{"squads": [{"name": " ", "lanes": ["planner"]}]}}`, "empty squad name")
	refusal(t, head+`{"squads": [{"name": "core", "lanes": ["planner"]}, {"name": "core", "lanes": ["implementer"]}]}}`, `names "core" twice`)
	refusal(t, head+`{"squads": [{"name": "core", "lanes": []}]}}`, "runs at least one lane manifest")
	refusal(t, head+`{"squads": [{"name": "core", "lanes": [" "]}]}}`, "names an empty lane")
}
