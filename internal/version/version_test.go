package version

import (
	"regexp"
	"testing"
)

func TestIdentity(t *testing.T) {
	if Name != "seed" {
		t.Fatalf("Name = %q, want %q (the successor claims the name)", Name, "seed")
	}
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	// conformance: III.A groundwork; a genesis event names the governance
	// root and protocol version. The protocol identifier must stay in the
	// seed/N form the spec defines so version-mismatch refusal can compare.
	if !regexp.MustCompile(`^seed/[0-9]+$`).MatchString(Protocol) {
		t.Fatalf("Protocol = %q, want seed/<n> per spec/protocol.md", Protocol)
	}
}

// conformance: plans/os-99829835.md AC2, D4 — seed/4 is registered and
// every gate is a named list: eval semantics hold at seed/3 and seed/4
// alike, the levels at seed/4 exactly, and an unregistered version
// activates nothing however it would sort.
func TestSeed4GatesAreNamedLists(t *testing.T) {
	supported := map[string]bool{}
	for _, v := range Supported() {
		supported[v] = true
	}
	if !supported[Seed3] || !supported[Seed4] || !supported[Seed5] || !supported[Seed6] || !supported[Seed7] || !supported[Seed8] || !supported[Seed9] {
		t.Fatalf("Supported must carry seed/3, seed/4 and seed/5: %v", Supported())
	}
	for _, v := range []string{Seed1, Seed2, Seed3, Seed4, Seed5, Seed6, Seed7, Seed8, Seed9} {
		if !Activated(v) {
			t.Fatalf("Activated(%s) must hold", v)
		}
	}
	if Activated(Protocol) || Activated("seed/10") {
		t.Fatal("Activated is a named list: the genesis default and an unregistered version activate nothing")
	}
	if EvalApplies(Seed2) || !EvalApplies(Seed3) || !EvalApplies(Seed4) || !EvalApplies(Seed5) || !EvalApplies(Seed6) || !EvalApplies(Seed7) || !EvalApplies(Seed8) || !EvalApplies(Seed9) || EvalApplies("seed/10") {
		t.Fatal("EvalApplies is the named list {seed/3, seed/4, seed/5}: the equality that was right while seed/3 was newest closes on every later version")
	}
	if LevelsApply(Seed3) || !LevelsApply(Seed4) || !LevelsApply(Seed5) || !LevelsApply(Seed6) || !LevelsApply(Seed7) || !LevelsApply(Seed8) || !LevelsApply(Seed9) || LevelsApply("seed/10") {
		t.Fatal("LevelsApply is the named list {seed/4, seed/5}")
	}
	if ImportApplies(Seed4) || !ImportApplies(Seed5) || !ImportApplies(Seed6) || !ImportApplies(Seed7) || !ImportApplies(Seed8) || !ImportApplies(Seed9) || ImportApplies("seed/10") {
		t.Fatal("ImportApplies is seed/5 exactly, a named list of one")
	}
	if RacingApplies(Seed5) || !RacingApplies(Seed6) || !RacingApplies(Seed7) || !RacingApplies(Seed8) || !RacingApplies(Seed9) || RacingApplies("seed/10") {
		t.Fatal("racing's widened origins are defined from seed/6, as a named list")
	}
	if RequestsApply(Seed6) || !RequestsApply(Seed7) || !RequestsApply(Seed8) || !RequestsApply(Seed9) || RequestsApply("seed/10") {
		t.Fatal("the request ingress is defined from seed/7, as a named list")
	}
	if ForgeChecksApply(Seed7) || !ForgeChecksApply(Seed8) || !ForgeChecksApply(Seed9) || ForgeChecksApply("seed/10") {
		t.Fatal("the forge observation is defined at seed/8 alone, as a named list of one")
	}
}

// conformance: III.P row 1 — the release workflow stamps the version
// with -ldflags -X (plans/os-2e46aa2f.md D3), which needs a var; from
// source it is the pre-release default.
func TestVersionIsStampableAndPreReleaseFromSource(t *testing.T) {
	if Version != "0.0.0-dev" {
		t.Fatalf("from source the version is the pre-release default, got %q", Version)
	}
	saved := Version
	Version = "0.1.0"
	if Version != "0.1.0" {
		t.Fatal("the version is assignable, as the ldflags stamp requires")
	}
	Version = saved
}
