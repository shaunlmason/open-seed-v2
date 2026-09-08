package protections

// The GitHub adapter's identity grammar and its refusal paths
// (plans/os-f262585a.md D1, D3). Every assertion here checks a value or
// an error the caller would act on: the bypass identity forms round
// trip, a malformed one is refused by name, a rule the adapter cannot
// translate refuses rather than shipping a partial ruleset, and Apply
// refuses an update or a delete it has no id for instead of calling the
// forge with a zero.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubBypassIdentitiesRoundTrip(t *testing.T) {
	for _, id := range []string{"deploy-key", "org-admin", "app:12345", "team:67"} {
		a, err := actorFor(id)
		if err != nil {
			t.Fatalf("actorFor(%q): %v", id, err)
		}
		if got := identityFor(a); got != id {
			t.Errorf("actorFor then identityFor turned %q into %q", id, got)
		}
	}
	// The wire shape is what GitHub documents, not just something that
	// survives the round trip.
	app, err := actorFor("app:12345")
	if err != nil {
		t.Fatal(err)
	}
	if app.ActorType != "Integration" || app.ActorID == nil || *app.ActorID != 12345 || app.BypassMode != "always" {
		t.Errorf("app:12345 became %+v", app)
	}
	key, err := actorFor("deploy-key")
	if err != nil {
		t.Fatal(err)
	}
	if key.ActorType != "DeployKey" || key.ActorID != nil {
		t.Errorf("deploy-key became %+v: a deploy key carries no actor id", key)
	}
}

func TestGitHubRefusesAnIdentityItCannotRead(t *testing.T) {
	for _, id := range []string{"app:not-a-number", "team:", "", "user:shaunlmason", "app"} {
		got, err := actorFor(id)
		if !errors.Is(err, ErrIdentityForm) {
			t.Errorf("actorFor(%q) = %+v, %v; want ErrIdentityForm", id, got, err)
		}
		if err != nil && !strings.Contains(err.Error(), id) {
			t.Errorf("the refusal for %q does not quote it: %v", id, err)
		}
	}
}

func TestGitHubIdentityForFallsBackToTheActorType(t *testing.T) {
	// GitHub can name an actor the grammar has no short form for, and
	// an Integration or Team with no id is not addressable as app:/team:.
	for _, a := range []ghActor{
		{ActorType: "Integration"},
		{ActorType: "Team"},
		{ActorType: "RepositoryRole"},
	} {
		if got := identityFor(a); got != a.ActorType {
			t.Errorf("identityFor(%+v) = %q; want the actor type verbatim", a, got)
		}
	}
}

func TestNewGitHubDefaultsAndTrimsTheBase(t *testing.T) {
	if g := NewGitHub("", "o", "r", "tok"); g.Base != "https://api.github.com" {
		t.Errorf("an empty base is api.github.com, got %q", g.Base)
	}
	g := NewGitHub("https://ghe.example.com/api/v3/", "o", "r", "tok")
	if g.Base != "https://ghe.example.com/api/v3" {
		t.Errorf("the trailing slash is trimmed so paths do not double it, got %q", g.Base)
	}
	if g.HTTP == nil || g.HTTP.Timeout == 0 {
		t.Error("the adapter carries a client with a timeout: a hung forge must not hang the run")
	}
}

func TestToGitHubRefusesARuleItCannotTranslate(t *testing.T) {
	_, err := toGitHub(Ruleset{Name: "seed-x", Target: "branch", Rules: []Rule{{Type: "invented_rule"}}})
	if err == nil || !strings.Contains(err.Error(), "invented_rule") {
		t.Fatalf("a rule with no GitHub translation must refuse by name, got %v", err)
	}
	_, err = toGitHub(Ruleset{Name: "seed-x", Target: "branch", Bypass: []string{"app:x"}})
	if !errors.Is(err, ErrIdentityForm) {
		t.Fatalf("a bypass identity the grammar rejects must refuse, got %v", err)
	}
}

func TestToGitHubCoercesPullRequestCounts(t *testing.T) {
	// A count decoded from JSON arrives as float64, from a Go literal as
	// int; both must reach the wire as the same number.
	for _, count := range []any{2, float64(2)} {
		out, err := toGitHub(Ruleset{Name: "seed-x", Target: "branch", Rules: []Rule{{
			Type: RulePullRequest,
			Params: map[string]any{
				"required_approving_review_count":   count,
				"required_review_thread_resolution": true,
				"require_code_owner_review":         true,
			},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		if err := json.Unmarshal(out.Rules[0].Parameters, &p); err != nil {
			t.Fatal(err)
		}
		if p["required_approving_review_count"] != float64(2) {
			t.Errorf("%T count reached the wire as %v", count, p["required_approving_review_count"])
		}
		if p["required_review_thread_resolution"] != true || p["require_code_owner_review"] != true {
			t.Errorf("the declared booleans did not reach the wire: %v", p)
		}
	}
}

func TestToGitHubReadsStatusCheckContextsFromEitherShape(t *testing.T) {
	for _, contexts := range []any{[]string{"check", "verify"}, []any{"check", "verify", 7}} {
		out, err := toGitHub(Ruleset{Name: "seed-x", Target: "branch", Rules: []Rule{{
			Type: RuleStatusChecks, Params: map[string]any{"contexts": contexts},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Checks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		}
		if err := json.Unmarshal(out.Rules[0].Parameters, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Checks) != 2 || p.Checks[0].Context != "check" || p.Checks[1].Context != "verify" {
			t.Errorf("%T contexts became %+v; a non-string entry is dropped, not rendered", contexts, p.Checks)
		}
	}
}

func TestGitHubApplyRefusesAChangeItHasNoIDFor(t *testing.T) {
	// Read ran and found no seed- ruleset, so ids is non-nil and empty:
	// an update or a delete naming one is a bug in the caller, not a
	// call to make against the forge with a zero id.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case strings.HasSuffix(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		}
	}))
	defer srv.Close()

	desired := &State{Rulesets: map[string]Ruleset{"seed-main": {Name: "seed-main", Target: "branch"}}}
	for _, kind := range []string{ChangeUpdate, ChangeDelete} {
		g := NewGitHub(srv.URL, "o", "r", "tok")
		before := calls
		err := g.Apply([]Change{{Kind: kind, Ruleset: "seed-main"}}, desired)
		if err == nil || !strings.Contains(err.Error(), "seed-main") {
			t.Fatalf("%s with no id must refuse naming the ruleset, got %v", kind, err)
		}
		if calls != before+2 {
			t.Errorf("%s made %d calls beyond the read; it must not call the forge", kind, calls-before-2)
		}
	}
}

func TestGitHubApplySkipsTheOperatorsChanges(t *testing.T) {
	// A manual change is the operator's to make; the adapter reads to
	// learn the ids and then does nothing with it.
	var mutations int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations++
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		}
	}))
	defer srv.Close()

	g := NewGitHub(srv.URL, "o", "r", "tok")
	if err := g.Apply([]Change{{Kind: ChangeManual, Ruleset: "seed-main"}}, &State{Rulesets: map[string]Ruleset{}}); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 {
		t.Errorf("a manual change made %d forge mutations", mutations)
	}
}

func TestGitHubSurfacesTheForgesRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	g := NewGitHub(srv.URL, "o", "r", "tok")
	_, err := g.Read()
	if err == nil {
		t.Fatal("a 403 from the forge must reach the caller")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("the refusal must carry the status and the forge's own words: %v", err)
	}
	// Apply reads first when it has no ids, so the same refusal blocks it.
	if err := g.Apply([]Change{{Kind: ChangeCreate, Ruleset: "seed-main"}}, &State{Rulesets: map[string]Ruleset{}}); err == nil {
		t.Fatal("Apply must not proceed on a read it could not make")
	}
}

func TestGitHubReadIgnoresForeignRulesets(t *testing.T) {
	// Rulesets the deployment did not declare are left alone by the
	// diff, so they are never fetched in full.
	var fetched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/rulesets"):
			_, _ = w.Write([]byte(`[{"id":1,"name":"legacy-protection"},{"id":2,"name":"seed-main"}]`))
		case strings.Contains(r.URL.Path, "/rulesets/"):
			fetched = append(fetched, r.URL.Path)
			_, _ = w.Write([]byte(`{"id":2,"name":"seed-main","target":"branch","conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"]}}}`))
		default:
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		}
	}))
	defer srv.Close()

	st, err := NewGitHub(srv.URL, "o", "r", "tok").Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 1 || !strings.HasSuffix(fetched[0], "/rulesets/2") {
		t.Errorf("only the seed- ruleset is fetched in full, got %v", fetched)
	}
	if _, ok := st.Rulesets["legacy-protection"]; ok {
		t.Error("a foreign ruleset must not enter the observed state")
	}
	if got := st.Rulesets["seed-main"]; got.Refs[0] != "~DEFAULT_BRANCH" {
		t.Errorf("the seed ruleset came back as %+v", got)
	}
	if st.DefaultBranch != "main" {
		t.Errorf("the default branch is read from the repo, got %q", st.DefaultBranch)
	}
}
