package protections

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub models the rulesets endpoints the adapter uses: the repo,
// the listing, one ruleset by id, create, update, delete — with the
// token checked on every call.
type fakeGitHub struct {
	mu       sync.Mutex
	next     int64
	rulesets map[int64]ghRuleset
	calls    []string
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r":
			_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": "trunk"})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/rulesets":
			list := []map[string]any{}
			for id, rs := range f.rulesets {
				list = append(list, map[string]any{"id": id, "name": rs.Name})
			}
			_ = json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/rulesets":
			var rs ghRuleset
			if err := json.NewDecoder(r.Body).Decode(&rs); err != nil {
				http.Error(w, err.Error(), 422)
				return
			}
			f.next++
			rs.ID = f.next
			f.rulesets[rs.ID] = rs
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(rs)
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/rulesets/"):
			id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repos/o/r/rulesets/"), 10, 64)
			rs, ok := f.rulesets[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(rs)
			case http.MethodPut:
				var upd ghRuleset
				if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
					http.Error(w, err.Error(), 422)
					return
				}
				upd.ID = id
				f.rulesets[id] = upd
				_ = json.NewEncoder(w).Encode(upd)
			case http.MethodDelete:
				delete(f.rulesets, id)
				w.WriteHeader(http.StatusNoContent)
			default:
				http.Error(w, "nope", http.StatusMethodNotAllowed)
			}
		default:
			http.NotFound(w, r)
		}
	})
}

// conformance: III.L — the GitHub adapter reads the default branch and
// the Seed-named rulesets, creates what is missing with the token from
// the environment, updates a weakened ruleset by id, deletes a stray
// one, and a re-read after apply is a clean plan.
func TestGitHubAdapterReconciles(t *testing.T) {
	fake := &fakeGitHub{rulesets: map[int64]ghRuleset{}}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	cfg := declaration(t)

	gh := NewGitHub(srv.URL, "o", "r", "tok")
	rep, _, err := Plan(cfg, gh, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.DefaultBranch != "trunk" || rep.DriftCount != 4 || rep.Manual != 0 {
		t.Fatalf("an empty forge plans four creates on the read default branch, got %+v", rep)
	}
	after, err := Apply(cfg, gh, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.DriftCount != 0 || len(fake.rulesets) != 4 {
		t.Fatalf("apply creates the four and re-reads clean, got %+v (%d rulesets)", after, len(fake.rulesets))
	}
	// What landed is the GitHub shape: the ledger branch's update rule
	// bypassed by the app, the default branch's PR rule with thread
	// resolution and code owners, the checks as contexts.
	var ledger, def ghRuleset
	for _, rs := range fake.rulesets {
		switch rs.Name {
		case RulesetLedger:
			ledger = rs
		case RulesetDefault:
			def = rs
		}
	}
	if len(ledger.BypassActors) != 1 || ledger.BypassActors[0].ActorType != "Integration" || *ledger.BypassActors[0].ActorID != 4242 || ledger.Conditions.RefName.Include[0] != "refs/heads/seed-ledger" {
		t.Fatalf("the ledger ruleset lands with the app as its bypass actor: %+v", ledger)
	}
	var prParams, checkParams string
	for _, rule := range def.Rules {
		if rule.Type == RulePullRequest {
			prParams = string(rule.Parameters)
		}
		if rule.Type == RuleStatusChecks {
			checkParams = string(rule.Parameters)
		}
	}
	if !strings.Contains(prParams, `"required_review_thread_resolution":true`) || !strings.Contains(prParams, `"require_code_owner_review":true`) || !strings.Contains(checkParams, `{"context":"check"}`) || def.Conditions.RefName.Include[0] != "refs/heads/trunk" {
		t.Fatalf("the default branch ruleset lands with reviews, threads, owners and checks: %s %s %+v", prParams, checkParams, def.Conditions)
	}

	// Weaken one by hand and add a stray: the plan is an update and a
	// delete, apply uses PUT and DELETE on the ids it read.
	def.Rules = def.Rules[:1]
	fake.rulesets[def.ID] = def
	fake.next++
	fake.rulesets[fake.next] = ghRuleset{ID: fake.next, Name: "seed-stray", Target: "branch"}
	rep, _, err = Plan(cfg, gh, "")
	if err != nil || rep.DriftCount != 2 {
		t.Fatalf("a weakened and a stray ruleset are two drifts, got %+v %v", rep, err)
	}
	fake.calls = nil
	if after, err = Apply(cfg, gh, ""); err != nil || after.DriftCount != 0 {
		t.Fatalf("apply reconciles both: %+v %v", after, err)
	}
	joined := strings.Join(fake.calls, "\n")
	if !strings.Contains(joined, "PUT /repos/o/r/rulesets/"+strconv.FormatInt(def.ID, 10)) || !strings.Contains(joined, "DELETE /repos/o/r/rulesets/") {
		t.Fatalf("apply must PUT the weakened ruleset and DELETE the stray, calls:\n%s", joined)
	}

	// The token is required, and the identity form is checked before
	// anything is written.
	if _, err := NewGitHub(srv.URL, "o", "r", "").Read(); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("no token, no read: %v", err)
	}
	if bad, err := toGitHub(Ruleset{Name: "x", Target: "branch", Bypass: []string{"alice"}}); err == nil || !strings.Contains(err.Error(), "app:<id>") {
		t.Fatalf("a login is not an actor form the API takes, got %v %+v", err, bad)
	}
}
