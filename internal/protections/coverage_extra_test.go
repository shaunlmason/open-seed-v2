package protections

// Coverage for the Forgejo adapter's delete/update paths and rule-parameter coercion (plans/os-ad610334.md
// D2, D4): the aggregate gate holds the internal tree to 90%, and the
// forge adapter's remove/update branches and the rule helpers earn their
// place under it the same way the create path does.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// desiredForgejoState is a hand-built desired State with a branch
// ruleset (carrying update, pull-request and status-check rules so the
// rule helpers are exercised) and a release-tag ruleset.
func desiredForgejoState() *State {
	return &State{
		DefaultBranch: "trunk",
		Rulesets: map[string]Ruleset{
			RulesetDefault: {
				Name: RulesetDefault, Target: TargetBranch,
				Refs:   []string{"refs/heads/trunk"},
				Bypass: []string{"svc"},
				Rules: []Rule{
					{Type: RuleUpdate},
					{Type: RulePullRequest, Params: map[string]any{"required_approving_review_count": float64(2)}},
					{Type: RuleStatusChecks, Params: map[string]any{"contexts": []any{"ci", "build"}}},
				},
			},
			RulesetTags: {
				Name: RulesetTags, Target: TargetTag,
				Refs:   []string{"refs/tags/v*"},
				Bypass: []string{"svc"},
			},
		},
	}
}

func TestForgejoRemoveAndUpdate(t *testing.T) {
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	desired := desiredForgejoState()

	// Create both rulesets, then update both (PATCH), then delete both.
	if err := fj.Apply([]Change{
		{Kind: ChangeCreate, Ruleset: RulesetDefault},
		{Kind: ChangeCreate, Ruleset: RulesetTags},
	}, desired); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(fake.branches) != 1 || len(fake.tags) != 1 {
		t.Fatalf("create leaves one branch and one tag protection, got %d/%d", len(fake.branches), len(fake.tags))
	}
	if err := fj.Apply([]Change{
		{Kind: ChangeUpdate, Ruleset: RulesetDefault},
		{Kind: ChangeUpdate, Ruleset: RulesetTags},
		{Kind: ChangeManual, Ruleset: RulesetDefault}, // a manual change is a no-op the adapter skips
	}, desired); err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(fake.branches) != 1 || len(fake.tags) != 1 {
		t.Fatalf("update does not add or drop protections, got %d/%d", len(fake.branches), len(fake.tags))
	}
	if err := fj.Apply([]Change{
		{Kind: ChangeDelete, Ruleset: RulesetDefault},
		{Kind: ChangeDelete, Ruleset: RulesetTags},
	}, desired); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(fake.branches) != 0 || len(fake.tags) != 0 {
		t.Fatalf("delete removes both protections, got %d/%d", len(fake.branches), len(fake.tags))
	}
}

func TestForgejoRemoveMissingIsAnError(t *testing.T) {
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	if _, err := fj.Read(); err != nil { // populate the empty maps
		t.Fatalf("read: %v", err)
	}
	desired := desiredForgejoState()
	if err := fj.Apply([]Change{{Kind: ChangeDelete, Ruleset: RulesetTags}}, desired); err == nil {
		t.Error("deleting a release-tag protection that was never created is an error, not a silent no-op")
	}
	if err := fj.Apply([]Change{{Kind: ChangeDelete, Ruleset: RulesetDefault}}, desired); err == nil {
		t.Error("deleting a branch protection that is not held is an error")
	}
}

func TestForgejoRuleHelpers(t *testing.T) {
	// ruleInt coerces every JSON-plausible integer shape and refuses the rest.
	for _, tc := range []struct {
		v    any
		want int
		ok   bool
	}{
		{int(3), 3, true},
		{int64(4), 4, true},
		{float64(5), 5, true},
		{"6", 6, true},
		{"notint", 0, false},
		{true, 0, false},
		{nil, 0, false},
	} {
		got, ok := ruleInt(map[string]any{"k": tc.v}, "k")
		if got != tc.want || ok != tc.ok {
			t.Errorf("ruleInt(%#v) = %d,%v want %d,%v", tc.v, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := ruleInt(map[string]any{}, "absent"); ok {
		t.Error("ruleInt on an absent key reports not-ok")
	}

	// ruleStrings sorts both []string and []any, and refuses the rest.
	if got, ok := ruleStrings(map[string]any{"k": []string{"b", "a"}}, "k"); !ok || got[0] != "a" || got[1] != "b" {
		t.Errorf("ruleStrings([]string) = %v,%v want sorted [a b]", got, ok)
	}
	if got, ok := ruleStrings(map[string]any{"k": []any{"y", "x"}}, "k"); !ok || got[0] != "x" || got[1] != "y" {
		t.Errorf("ruleStrings([]any) = %v,%v want sorted [x y]", got, ok)
	}
	if _, ok := ruleStrings(map[string]any{"k": []any{}}, "k"); ok {
		t.Error("ruleStrings on an empty list reports not-ok (nothing to require)")
	}
	if _, ok := ruleStrings(map[string]any{"k": "scalar"}, "k"); ok {
		t.Error("ruleStrings on a scalar reports not-ok")
	}
}

func TestForgejoDoSurfacesForgeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	if _, err := fj.Read(); err == nil {
		t.Error("a 5xx from the forge surfaces as an error, never a silent read")
	}
}

func TestForgejoGlobAndClient(t *testing.T) {
	if g := glob(Ruleset{Name: "seed-x"}); g != "seed-x" {
		t.Errorf("glob with no refs falls back to the ruleset name, got %q", g)
	}
	if g := glob(Ruleset{Name: "seed-x", Refs: []string{"refs/heads/trunk"}}); g != "trunk" {
		t.Errorf("glob strips refs/heads/, got %q", g)
	}
	if (&Forgejo{}).client() != http.DefaultClient {
		t.Error("a Forgejo with no HTTP client uses the default")
	}
}
