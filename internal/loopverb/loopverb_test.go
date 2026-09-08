package loopverb

// The registry drills (plans/os-cf1c9688.md D3a). The registry exists
// to be the ONE place the loop acts are written down, so the drills
// that matter are the ones proving it says what the spec says and that
// nothing it names is invented.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// conformance: spec/loop-verbs.md — seven acts close the loop:
// poll, claim, work, meter, submit, exit.
func TestSevenActs(t *testing.T) {
	if len(Acts()) != 7 {
		t.Fatalf("the loop is seven acts, the registry holds %d: %v", len(Acts()), Names())
	}
	want := []string{
		"budget release", "budget reserve", "budget settle",
		"claim park", "claim release", "claim take", "submission make",
	}
	if got := Names(); !slices.Equal(got, want) {
		t.Fatalf("registry names %v, want %v", got, want)
	}
}

// conformance: the registry invents no verbs, and is an INDEX into the
// real vocabulary rather than a second copy of it. An act's verb is
// real in one of two ways, because the loop spans both kinds: the four
// lifecycle acts change state and appear in the transition table,
// while the three budget acts are FACTS that change no state and so
// appear in no table row. What both share is a capability row, which
// is also what makes a lane's grants checkable against them.
func TestEveryVerbIsReal(t *testing.T) {
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	inTable := map[string]bool{}
	for _, v := range table.Verbs() {
		inTable[v] = true
	}
	facts := 0
	for _, a := range Acts() {
		accepted := keyring.AcceptedCapabilities(a.Verb)
		if len(accepted) == 0 {
			t.Errorf("%s appends %q, which accepts no capability: nothing to validate a lane against", a.Name(), a.Verb)
		}
		if !inTable[a.Verb] {
			facts++
			// A fact still has to be a verb the system knows; the
			// capability row above is that proof, and the constant it
			// came from is the transition package's own.
			if !strings.HasPrefix(a.Verb, "budget.") {
				t.Errorf("%s appends %q, which is in no transition row and is not a known fact", a.Name(), a.Verb)
			}
		}
	}
	if facts != 3 {
		t.Errorf("three loop acts are facts that change no state (the budget closes and the reserve), found %d", facts)
	}
}

// conformance: lookups are total and consistent — the two ways in
// resolve the same act, and Subverbs is exactly the registry's own
// grouping.
func TestLookupsAgree(t *testing.T) {
	for _, a := range Acts() {
		byName, ok := ByName(a.Name())
		if !ok || byName.Verb != a.Verb {
			t.Errorf("ByName(%q) did not resolve to %s", a.Name(), a.Verb)
		}
		byPair, ok := Lookup(a.Group, a.Sub)
		if !ok || byPair.Verb != a.Verb {
			t.Errorf("Lookup(%q, %q) did not resolve to %s", a.Group, a.Sub, a.Verb)
		}
	}
	if _, ok := ByName("claim yeet"); ok {
		t.Error("an unknown act must not resolve")
	}
	if _, ok := Lookup("claim", "yeet"); ok {
		t.Error("an unknown subverb must not resolve")
	}
	for _, group := range []string{"claim", "budget", "submission"} {
		subs := Subverbs(group)
		if len(subs) == 0 {
			t.Errorf("%s names no subverbs", group)
		}
		for _, s := range subs {
			if _, ok := Lookup(group, s); !ok {
				t.Errorf("Subverbs(%q) names %q, which does not resolve", group, s)
			}
		}
	}
}

// conformance: only claim.taken is remote-only, because the table
// marks it alone exclusive and only the push round-trip can order two
// rivals.
func TestOnlyTheExclusiveActIsRemoteOnly(t *testing.T) {
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range Acts() {
		if a.RemoteOnly != table.Exclusive(a.Verb) {
			t.Errorf("%s: remote-only is %v, the table's exclusivity for %s is %v",
				a.Name(), a.RemoteOnly, a.Verb, table.Exclusive(a.Verb))
		}
	}
}

// English is what a refusal reads, so it is pinned rather than left to
// whatever the next edit produces.
func TestEnglish(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"take"}, "take"},
		{[]string{"take", "park"}, "take or park"},
		{[]string{"take", "release", "park"}, "take, release, or park"},
	} {
		if got := English(tc.in); got != tc.want {
			t.Errorf("English(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// conformance: the spec and the registry name the same acts. The spec
// is the normative statement; this pins the code to it, so a seventh
// act added to one and not the other fails here.
func TestSpecNamesTheSameActs(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "spec", "loop-verbs.md"))
	if err != nil {
		t.Skipf("spec unavailable: %v", err)
	}
	text := string(body)
	for _, a := range Acts() {
		if !strings.Contains(text, a.Name()) {
			t.Errorf("spec/loop-verbs.md does not name the act %q", a.Name())
		}
		if !strings.Contains(text, a.Verb) {
			t.Errorf("spec/loop-verbs.md does not name the verb %q", a.Verb)
		}
	}
}
