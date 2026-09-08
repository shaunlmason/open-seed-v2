package protections

// The Forgejo adapter is held to the same desired state as GitHub, over
// an in-process fake of Forgejo's branch- and tag-protection API
// (plans/os-ad610334.md D2, D5): plan on an empty forge creates the four
// rulesets, the pull-request rule reports manual (unexpressible), apply
// leaves no drift, and a weakened or stray protection is reconciled.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

type fakeForgejo struct {
	mu       sync.Mutex
	branches map[string]fjBranch // by rule_name
	tags     map[int64]fjTag     // by id
	nextTag  int64
	calls    []string
}

func newFakeForgejo() *fakeForgejo {
	return &fakeForgejo{branches: map[string]fjBranch{}, tags: map[int64]fjTag{}}
}

func (f *fakeForgejo) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		p := r.URL.Path
		const base = "/api/v1/repos/o/r"
		switch {
		case r.Method == http.MethodGet && p == base:
			json.NewEncoder(w).Encode(fjRepo{DefaultBranch: "trunk"})
		case r.Method == http.MethodGet && p == base+"/branch_protections":
			out := []fjBranch{}
			for _, b := range f.branches {
				out = append(out, b)
			}
			json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && p == base+"/branch_protections":
			var b fjBranch
			json.NewDecoder(r.Body).Decode(&b)
			f.branches[b.RuleName] = b
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(b)
		case strings.HasPrefix(p, base+"/branch_protections/"):
			name := strings.TrimPrefix(p, base+"/branch_protections/")
			if _, ok := f.branches[name]; !ok && r.Method != http.MethodPost {
				http.Error(w, "no such branch protection", http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodPatch:
				var b fjBranch
				json.NewDecoder(r.Body).Decode(&b)
				b.RuleName = name
				f.branches[name] = b
				json.NewEncoder(w).Encode(b)
			case http.MethodDelete:
				delete(f.branches, name)
				w.WriteHeader(http.StatusNoContent)
			}
		case r.Method == http.MethodGet && p == base+"/tag_protections":
			out := []fjTag{}
			for _, tg := range f.tags {
				out = append(out, tg)
			}
			json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && p == base+"/tag_protections":
			var tg fjTag
			json.NewDecoder(r.Body).Decode(&tg)
			f.nextTag++
			tg.ID = f.nextTag
			f.tags[tg.ID] = tg
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(tg)
		case strings.HasPrefix(p, base+"/tag_protections/"):
			id, _ := strconv.ParseInt(strings.TrimPrefix(p, base+"/tag_protections/"), 10, 64)
			if _, ok := f.tags[id]; !ok {
				http.Error(w, "no such tag protection", http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodPatch:
				var tg fjTag
				json.NewDecoder(r.Body).Decode(&tg)
				tg.ID = id
				f.tags[id] = tg
				json.NewEncoder(w).Encode(tg)
			case http.MethodDelete:
				delete(f.tags, id)
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotFound)
		}
	})
}

func TestForgejoAdapterReconciles(t *testing.T) {
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	cfg := declaration(t)
	fj := NewForgejo(srv.URL, "o", "r", "tok")

	// Plan on the empty forge: four creates, the pull-request rule manual.
	rep, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.DefaultBranch != "trunk" {
		t.Fatalf("default branch read from the forge, got %q", rep.DefaultBranch)
	}
	if rep.DriftCount != 4 {
		t.Fatalf("four rulesets to create, got drift %d (%+v)", rep.DriftCount, rep.Changes)
	}
	if rep.Manual != 1 {
		t.Fatalf("the pull-request rule is manual on Forgejo, got %d manual", rep.Manual)
	}

	// Apply, then re-plan: no drift, the manual rule persists.
	after, err := Apply(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.DriftCount != 0 {
		t.Fatalf("apply must leave no drift, got %d (%+v)", after.DriftCount, after.Changes)
	}
	if after.Manual != 1 {
		t.Fatalf("the manual rule persists after apply, got %d", after.Manual)
	}
	// One tag protection per release-tag pattern (plans/os-2e46aa2f.md
	// D8): the template's v* and Seed's seed/v*.
	if len(fake.branches) != 3 || len(fake.tags) != 2 {
		t.Fatalf("three branch protections and two tag protections, got %d/%d", len(fake.branches), len(fake.tags))
	}
	patterns := map[string]bool{}
	for _, tg := range fake.tags {
		patterns[tg.NamePattern] = true
	}
	if !patterns["v*"] || !patterns["seed/v*"] {
		t.Fatalf("the release-tag ruleset protects both namespaces on the forge, got %v", patterns)
	}
	// The ledger branch's sole writer is the admission identity.
	led, ok := fake.branches["seed-ledger"]
	if !ok || led.EnablePush || !led.EnablePushWhitelist || len(led.PushWhitelistUsernames) != 1 || led.PushWhitelistUsernames[0] != "app:4242" {
		t.Fatalf("the ledger protection's sole writer must be the admission identity, got %+v", led)
	}
	// The default branch carries the declared checks.
	def := fake.branches["trunk"]
	if !def.EnableStatusCheck || strings.Join(def.StatusCheckContexts, ",") != "check,verify" {
		t.Fatalf("the default branch requires the declared checks, got %+v", def)
	}

	// Drift: weaken the ledger's whitelist and add a stray tag; reconcile.
	fake.mu.Lock()
	stray := fake.branches["seed-ledger"]
	stray.PushWhitelistUsernames = []string{"app:4242", "intruder"}
	fake.branches["seed-ledger"] = stray
	fake.calls = nil
	fake.mu.Unlock()
	drift, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if drift.DriftCount == 0 {
		t.Fatal("a widened ledger whitelist must be caught as drift")
	}
	if _, err := Apply(cfg, fj, ""); err != nil {
		t.Fatal(err)
	}
	clean, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if clean.DriftCount != 0 {
		t.Fatalf("apply must restore the sole writer, got drift %d", clean.DriftCount)
	}
}

func TestForgejoNeedsAToken(t *testing.T) {
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	if _, err := NewForgejo(srv.URL, "o", "r", "").Read(); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("a tokenless read must be refused 401, got %v", err)
	}
}

func TestForgejoMutationEvidence(t *testing.T) {
	// The desired state is one table: both forges derive from Desired,
	// never a forge-specific fork.
	cfg := declaration(t)
	want, err := Desired(cfg, "trunk")
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Rulesets) != 4 {
		t.Fatalf("Desired is the one shared table of four rulesets, got %d", len(want.Rulesets))
	}

	// A Forgejo read that drops the ledger branch's sole writer is caught
	// as drift, never accepted.
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	if _, err := Apply(cfg, fj, ""); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	led := fake.branches["seed-ledger"]
	led.EnablePush = true // anyone may push — the whitelist is gone
	led.EnablePushWhitelist = false
	led.PushWhitelistUsernames = nil
	fake.branches["seed-ledger"] = led
	fake.mu.Unlock()
	rep, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.DriftCount == 0 {
		t.Fatal("dropping the ledger branch's sole writer must be drift")
	}
}

// TestForgejoLive reconciles a real Forgejo instance when one is
// configured; it skips with a named reason otherwise (never silently).
func TestForgejoLive(t *testing.T) {
	url, repo, tok := os.Getenv("SEED_FORGEJO_URL"), os.Getenv("SEED_FORGEJO_REPO"), os.Getenv("FORGEJO_TOKEN")
	if url == "" || repo == "" || tok == "" {
		t.Skip("live Forgejo drill: set SEED_FORGEJO_URL, SEED_FORGEJO_REPO and FORGEJO_TOKEN to run against a real instance")
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		t.Fatalf("SEED_FORGEJO_REPO must be owner/name, got %q", repo)
	}
	cfg := declaration(t)
	fj := NewForgejo(url, owner, name, tok)
	if _, err := Apply(cfg, fj, ""); err != nil {
		t.Fatalf("applying to the live Forgejo: %v", err)
	}
	after, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.DriftCount != 0 {
		t.Fatalf("the live Forgejo must reconcile to no drift, got %d", after.DriftCount)
	}
}

func TestForgejoCustomLedgerBranch(t *testing.T) {
	// A deployment whose ledger rides a non-default branch: Read maps the
	// protection back to the ledger ruleset by the declared branch, not
	// the seed-ledger convention, so a custom-ledger deployment does not
	// drift-loop on its own ledger protection.
	cfg, err := posture.Parse([]byte(`{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://127.0.0.1:1", "identity": "app:4242", "ledger_ref": "refs/heads/my-ledger", "checks": ["verify"], "reviews": 1}, "protected": ["Makefile"]}`))
	if err != nil {
		t.Fatal(err)
	}
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	fj.LedgerBranch = "my-ledger"
	if _, err := Apply(cfg, fj, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.branches["my-ledger"]; !ok {
		t.Fatalf("the ledger protection lands under the declared branch, got %v", keysOf(fake.branches))
	}
	after, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.DriftCount != 0 {
		t.Fatalf("a custom-ledger deployment must reconcile to no drift, got %d", after.DriftCount)
	}
}

// conformance: SEED-NEXT.md §II.14: a released tag cannot be
// retargeted, so a weakened release-tag protection must read as drift
// (plans/os-2e46aa2f.md D8). Forgejo holds one protection per pattern
// with its own whitelist and the adapter compares every one of them,
// never the first for both: a widened seed/v* whitelist beside a
// compliant v* is drift and Apply repairs it, with the desired bypass
// empty (as Desired renders it) and with a bypass identity (the case
// the review on #329 named), and an emptied seed/v* whitelist beside a
// compliant v* is drift as well.
func TestForgejoComparesEveryTagWhitelist(t *testing.T) {
	fake := newFakeForgejo()
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()
	cfg := declaration(t)
	fj := NewForgejo(srv.URL, "o", "r", "tok")
	if _, err := Apply(cfg, fj, ""); err != nil {
		t.Fatal(err)
	}
	set := func(pattern string, users []string) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for id, tg := range fake.tags {
			if tg.NamePattern == pattern {
				tg.WhitelistUsernames = users
				fake.tags[id] = tg
			}
		}
	}
	get := func(pattern string) string {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, tg := range fake.tags {
			if tg.NamePattern == pattern {
				return strings.Join(tg.WhitelistUsernames, ",")
			}
		}
		return "<absent>"
	}

	// The desired bypass is empty: widen seed/v* alone, v* still matches.
	set("seed/v*", []string{"intruder"})
	rep, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.DriftCount == 0 {
		t.Fatal("a widened seed/v* whitelist beside a compliant v* must be drift")
	}
	if _, err := Apply(cfg, fj, ""); err != nil {
		t.Fatal(err)
	}
	if got := get("seed/v*"); got != "" {
		t.Fatalf("apply must restore the empty seed/v* whitelist, got %q", got)
	}
	clean, _, err := Plan(cfg, fj, "")
	if err != nil {
		t.Fatal(err)
	}
	if clean.DriftCount != 0 {
		t.Fatalf("apply must leave no drift, got %d (%+v)", clean.DriftCount, clean.Changes)
	}

	// The desired bypass is an identity: v* holds exactly it and seed/v*
	// something else, so a read that took v*'s whitelist for both would
	// match desired state and never repair seed/v*.
	desired, err := Desired(cfg, "trunk")
	if err != nil {
		t.Fatal(err)
	}
	rs := desired.Rulesets[RulesetTags]
	rs.Bypass = []string{"app:4242"}
	desired.Rulesets[RulesetTags] = rs
	tagChanges := func() []Change {
		cur, err := fj.Read()
		if err != nil {
			t.Fatal(err)
		}
		var out []Change
		for _, c := range Diff(desired, cur) {
			if c.Ruleset == RulesetTags {
				out = append(out, c)
			}
		}
		return out
	}
	for _, tc := range []struct {
		name string
		seed []string
	}{
		{"widened", []string{"app:4242", "intruder"}},
		{"emptied", nil},
	} {
		set("v*", []string{"app:4242"})
		set("seed/v*", tc.seed)
		changes := tagChanges()
		if len(changes) != 1 || changes[0].Kind != ChangeUpdate || !strings.Contains(changes[0].Detail, "seed/v*") {
			t.Fatalf("%s seed/v* whitelist beside a compliant v* must be one update naming the pattern, got %+v", tc.name, changes)
		}
		if err := fj.Apply(changes, desired); err != nil {
			t.Fatal(err)
		}
		if get("v*") != "app:4242" || get("seed/v*") != "app:4242" {
			t.Fatalf("%s: apply must set every pattern's whitelist to the desired bypass, got v*=%q seed/v*=%q", tc.name, get("v*"), get("seed/v*"))
		}
		if changes := tagChanges(); len(changes) != 0 {
			t.Fatalf("%s: apply must leave no drift, got %+v", tc.name, changes)
		}
	}
}

func keysOf(m map[string]fjBranch) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
