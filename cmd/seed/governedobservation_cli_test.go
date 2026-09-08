package main

// The governed-observation drill (plans/os-b45c308d.md D6, AC5, AC6)
// over fake GitHub and Forgejo APIs that record every request:
// observing a completed check emits only reads and then one admitted
// fact signed by the observer; observing an unmerged pull request
// emits reads and no fact; a claim-only key is refused; a missing
// credential refuses naming the variable and never a value; a raw
// merge observation with no verdict and no request refuses at
// admission rather than laundering a verdict; and no observation
// invokes a forge write.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/externalfact/facttest"
)

func TestGovernedObservationOverTheForges(t *testing.T) {
	for _, kind := range []string{"github", "forgejo"} {
		t.Run(kind, func(t *testing.T) {
			m := forgeLedger(t)
			m.submit(t)
			pulls := map[string]*facttest.PullRequest{"1": {Head: m.head, Checks: "red", Unresolved: 1, Resolved: 2, ChangesBy: []string{"reviewer"}}}
			var api string
			var forge *facttest.Forge
			if kind == "github" {
				srv, f := facttest.GitHub(pulls)
				t.Cleanup(srv.Close)
				api, forge = srv.URL, f
			} else {
				srv, f := facttest.Forgejo(pulls)
				t.Cleanup(srv.Close)
				api, forge = srv.URL, f
			}
			const tokenEnv = "SEED_TEST_FORGE_TOKEN"
			t.Setenv(tokenEnv, facttest.Token)
			args := []string{"--forge", kind, "--github", "o/r", "--api", api, "--token-env", tokenEnv}
			readsOnly := func(step string, queries int) {
				t.Helper()
				posts := 0
				for _, c := range forge.Recorded() {
					switch {
					case strings.HasPrefix(c, "GET "):
					case c == "POST /graphql" && kind == "github":
						posts++
					default:
						t.Fatalf("%s: the source wrote: %s", step, c)
					}
				}
				if kind == "github" && posts != queries {
					t.Fatalf("%s: the GraphQL query %d times, got %v", step, queries, forge.Recorded())
				}
				if pulls["1"].MergeRequests != 0 {
					t.Fatalf("%s: an observation invoked a forge write", step)
				}
				forge.Reset()
			}
			before := ledgerCount(t, m.ld)
			// A completed red check: reads, then one admitted fact.
			e, code := runEnv(t, append([]string{"check", "observe", "--ledger", m.ld, "--key", m.keys["observer"], "--subject", "c-1"}, args...)...)
			if code != 0 {
				t.Fatalf("check observe --forge %s: %d %+v", kind, code, e)
			}
			readsOnly("check observe", 1)
			if ledgerCount(t, m.ld) != before+1 {
				t.Fatalf("one fact: %d", ledgerCount(t, m.ld)-before)
			}
			s, _ := m.state(t).fold.State("c-1")
			if s.Observation == nil || !s.Observation.Red() || s.Observation.Head != m.head || s.Observation.Signer != m.fps["observer"] || s.Observation.Review != "changes_requested" {
				t.Fatalf("the fold carries the observer's fact from the forge: %+v", s.Observation)
			}
			if kind == "github" && (s.Observation.Threads == nil || *s.Observation.Threads != 1) {
				t.Fatalf("GitHub's unresolved threads are counted: %v", s.Observation.Threads)
			}
			if kind == "forgejo" && s.Observation.Threads != nil {
				t.Fatalf("Forgejo cannot say, so the count is nil: %v", *s.Observation.Threads)
			}
			if b, _ := json.Marshal(e); strings.Contains(string(b), facttest.Marker) {
				t.Fatalf("forge prose crossed into the envelope: %s", b)
			}
			// A pending check is the forge's word too: a fact, and
			// still no debt (the obligation drill's "pending alone").
			pulls["1"].Checks = "pending"
			pulls["1"].ChangesBy = nil
			if e, code := runEnv(t, append([]string{"check", "observe", "--ledger", m.ld, "--key", m.keys["observer"], "--subject", "c-1"}, args...)...); code != 0 {
				t.Fatalf("a changed observation admits: %d %+v", code, e)
			}
			readsOnly("pending", 1)
			// An unmerged pull request: reads, and no fact.
			count := ledgerCount(t, m.ld)
			if e, code := runEnv(t, append([]string{"merge", "observe", "--ledger", m.ld, "--key", m.keys["observer"], "--subject", "c-1", "--pr", "pr/1"}, args...)...); code != 3 || e.Error == nil || e.Error.Code != "not_merged" {
				t.Fatalf("an unmerged pull request refuses, not_merged: %d %+v", code, e)
			}
			readsOnly("merge observe", 0)
			if ledgerCount(t, m.ld) != count {
				t.Fatal("an unmerged observation appended")
			}
			// A merged pull request with no verdict and no request: the
			// forge says merged, and admission still refuses, because a
			// merge observation authenticates no missing verdict.
			pulls["1"].Merged, pulls["1"].MergeSHA = true, m.head
			if e, code := runEnv(t, append([]string{"merge", "observe", "--ledger", m.ld, "--key", m.keys["observer"], "--subject", "c-1", "--pr", "pr/1"}, args...)...); code == 0 || e.Error == nil {
				t.Fatalf("a merge observed with no verdict and no request refuses at admission: %d %+v", code, e)
			}
			readsOnly("raw merge", 0)
			if ledgerCount(t, m.ld) != count {
				t.Fatal("a raw merge observation appended")
			}
			// A claim-only key holds no observer standing: the forge is
			// read, the fact is drafted, and admission refuses it.
			pulls["1"].Checks = "green"
			if e, code := runEnv(t, append([]string{"check", "observe", "--ledger", m.ld, "--key", m.keys["workerA"], "--subject", "c-1"}, args...)...); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
				t.Fatalf("a claim-only key refuses out of grant: %d %+v", code, e)
			}
			forge.Reset()
			// No credential: unavailable, naming the variable, never a
			// value, and no request leaves.
			t.Setenv(tokenEnv, "")
			e, code = runEnv(t, append([]string{"check", "observe", "--ledger", m.ld, "--key", m.keys["observer"], "--subject", "c-1"}, args...)...)
			if code != 5 || e.Error == nil || !strings.Contains(e.Error.Message, "$"+tokenEnv) || strings.Contains(fmt.Sprint(e), facttest.Token) {
				t.Fatalf("no credential refuses by variable name: %d %+v", code, e)
			}
			if len(forge.Recorded()) != 0 {
				t.Fatalf("no request leaves without a credential: %v", forge.Recorded())
			}
			if ledgerCount(t, m.ld) != count {
				t.Fatal("a refused observation appended")
			}
		})
	}
}
