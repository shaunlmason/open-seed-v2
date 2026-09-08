package posture

// The declaration's accessors and their undeclared answers
// (plans/os-f262585a.md D1). Each one distinguishes "declared as
// nothing" from "not declared", and admission's policy rules turn on
// that difference: an undeclared ceiling is a rule that does not run,
// not a ceiling of zero. A nil receiver is part of the contract too,
// since a deployment with no seed.json hands one to every caller.

import (
	"testing"
)

const fullDeclaration = `{
  "posture": "cooperative",
  "teams": {"squads": [{"name": "spec", "lanes": ["planner"]}, {"name": "core", "lanes": ["implementer"]}]},
  "guardrails": {
    "squads": {
      "core": {"default": "trivial", "max_agent": "standard"},
      "spec": {"default": "standard", "max_agent": "deep", "racing": {"racers": 3, "cost": "three times the compute for one contract's latency"}}
    },
    "paths": [{"prefix": "internal/admit/", "min": "deep"}, {"prefix": "internal/", "min": "standard"}]
  },
  "checkpoints": {"trust": "replay"}
}`

func parse(t *testing.T, body string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSquadNamesAreSortedAndAbsentWhenUndeclared(t *testing.T) {
	got := parse(t, fullDeclaration).SquadNames()
	if len(got) != 2 || got[0] != "core" || got[1] != "spec" {
		t.Errorf("the declared squads come back sorted, got %v", got)
	}
	if names := parse(t, `{"posture": "cooperative"}`).SquadNames(); names != nil {
		t.Errorf("undeclared teams name no squads, got %v", names)
	}
	if names := (*Config)(nil).SquadNames(); names != nil {
		t.Errorf("no declaration names no squads, got %v", names)
	}
}

func TestAgentCeilingSeparatesUndeclaredFromEmpty(t *testing.T) {
	cfg := parse(t, fullDeclaration)
	if got, ok := cfg.AgentCeiling("core"); !ok || got != "standard" {
		t.Errorf("core's declared ceiling is standard, got %q %v", got, ok)
	}
	if got, ok := cfg.AgentCeiling("spec"); !ok || got != "deep" {
		t.Errorf("spec's declared ceiling is deep, got %q %v", got, ok)
	}
	// Parse refuses a squad that declares one tier and not the other,
	// so an empty max_agent reaches the accessor only from a Config
	// built in code. It is still no ceiling: the rule must not read it
	// as a ceiling below every tier.
	built := &Config{Guardrails: &Guardrails{Squads: map[string]SquadGuardrail{"core": {Default: "trivial"}}}}
	if got, ok := built.AgentCeiling("core"); ok {
		t.Errorf("an empty max_agent is no ceiling, got %q %v", got, ok)
	}
	for _, c := range []*Config{cfg, parse(t, `{"posture": "cooperative"}`), nil} {
		if _, ok := c.AgentCeiling("no-such-squad"); ok {
			t.Error("an undeclared squad has no ceiling")
		}
	}
}

func TestRacingIsOptInPerSquad(t *testing.T) {
	cfg := parse(t, fullDeclaration)
	racers, cost, ok := cfg.RacingFor("spec")
	if !ok || racers != 3 || cost == "" {
		t.Errorf("spec opted in: %d %q %v", racers, cost, ok)
	}
	if _, _, ok := cfg.RacingFor("core"); ok {
		t.Error("a squad with no racing block did not opt in")
	}
	if _, _, ok := cfg.RacingFor("no-such-squad"); ok {
		t.Error("an undeclared squad did not opt in")
	}
	if _, _, ok := (*Config)(nil).RacingFor("core"); ok {
		t.Error("no declaration is no opt-in")
	}
}

func TestFloorTakesTheStrictestPrefixThePathIsUnder(t *testing.T) {
	cfg := parse(t, fullDeclaration)
	// "deep is above standard" is the ordering this call is given; the
	// declaration's own tier vocabulary lives elsewhere, so the caller
	// supplies the comparison.
	above := func(a, b string) bool { return a == "deep" && b != "deep" }

	for _, tc := range []struct {
		path string
		want string
		ok   bool
	}{
		{"internal/admit/admit.go", "deep", true}, // under both prefixes: the stricter wins
		{"internal/admit", "deep", true},          // the prefix itself, trailing slash trimmed
		{"internal/admit/", "deep", true},         // and with it
		{"internal/plan/plan.go", "standard", true},
		{"README.md", "", false},                      // under no declared prefix
		{"internal/admitting/x.go", "standard", true}, // a sibling that merely shares a prefix string
	} {
		got, ok := cfg.Floor(tc.path, above)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Floor(%q) = %q %v; want %q %v", tc.path, got, ok, tc.want, tc.ok)
		}
	}

	if _, ok := parse(t, `{"posture": "cooperative"}`).Floor("internal/x.md", above); ok {
		t.Error("undeclared guardrails set no floor")
	}
	if _, ok := (*Config)(nil).Floor("internal/x.md", above); ok {
		t.Error("no declaration sets no floor")
	}
}

func TestCheckpointTrustIsDeclaredNeverDefaulted(t *testing.T) {
	if got := parse(t, fullDeclaration).CheckpointTrust(); got != "replay" {
		t.Errorf("the declared trust is read back verbatim, got %q", got)
	}
	// An absent block is undeclared. The charter says the choice is
	// declared, so the accessor must not invent one.
	if got := parse(t, `{"posture": "cooperative"}`).CheckpointTrust(); got != "" {
		t.Errorf("an absent checkpoints block is undeclared, got %q", got)
	}
	if got := (*Config)(nil).CheckpointTrust(); got != "" {
		t.Errorf("no declaration is undeclared, got %q", got)
	}
}
