package main

// The full III.D row 5 drill (plans/os-b45c308d.md D2, AC2): for each
// registered exporter, render a contract from the published
// projection, mutate the mirror side, prove the ledger did not move,
// file the edit through request.filed with the enrolled standing-only
// service key, prove the request admitted and changed no lifecycle
// state, rerun the export to restore the mirror from the projection,
// and prove a direct coordination act by the service key refuses.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/mirror"
	"github.com/shaunlmason/open-seed-v2/mirror/mirrortest"
)

// ledgerDigest hashes every byte under the ledger directory, so
// "the ledger did not move" is a byte claim rather than a count.
func ledgerDigest(t *testing.T, ld string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.Walk(ld, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\n%d\n", p, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ledgerCount(t *testing.T, ld string) int {
	t.Helper()
	store, err := ledger.OpenReadOnly(ld)
	if err != nil {
		t.Fatal(err)
	}
	_, n, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMirrorExportAndRequestIngressPerExporter(t *testing.T) {
	ld, _, _, service, _ := requestLedger(t)
	out := filepath.Join(t.TempDir(), "projections")
	// Publication locks the projection tree read-only; unlock it before
	// testing's own TempDir cleanup, as every rebuild drill does.
	unlockForCleanup(t, out)
	current := filepath.Join(t.TempDir(), "current.json")
	// rebuild publishes, then resolves through the consumer verb: the
	// mirror reads what `seed project current` printed, never the
	// published layout itself.
	rebuild := func() {
		t.Helper()
		if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out); code != 0 {
			t.Fatalf("project rebuild: %d %+v", code, e)
		}
		e, code := runEnv(t, "project", "current", "--name", "contracts", "--out", out)
		if code != 0 {
			t.Fatalf("project current: %d %+v", code, e)
		}
		b, err := json.Marshal(map[string]any{"ok": true, "result": e.Result})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(current, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rebuild()
	stateOf := func() string {
		t.Helper()
		st, failEnv := loadVerdictState(ld)
		if failEnv != nil {
			t.Fatalf("the chain must verify: %+v", failEnv)
		}
		s, _ := st.fold.State("c-1")
		return s.State
	}
	for _, name := range mirror.Names() {
		t.Run(name, func(t *testing.T) {
			var adapter mirror.Adapter
			var forge *mirrortest.Forge
			switch name {
			case "github":
				srv, f := mirrortest.GitHub()
				t.Cleanup(srv.Close)
				t.Setenv(mirror.GitHubTokenEnv, mirrortest.Token)
				a, err := mirror.Open(name, mirror.Config{Owner: "o", Repo: "r", BaseURL: srv.URL})
				if err != nil {
					t.Fatal(err)
				}
				adapter, forge = a, f
			case "forgejo":
				srv, f := mirrortest.Forgejo()
				t.Cleanup(srv.Close)
				t.Setenv(mirror.ForgejoTokenEnv, mirrortest.Token)
				a, err := mirror.Open(name, mirror.Config{Owner: "o", Repo: "r", BaseURL: srv.URL})
				if err != nil {
					t.Fatal(err)
				}
				adapter, forge = a, f
			case "snapshot":
				a, err := mirror.Open(name, mirror.Config{Snapshot: filepath.Join(t.TempDir(), "issues.json")})
				if err != nil {
					t.Fatal(err)
				}
				adapter = a
			}
			ctx := context.Background()
			export := func() mirror.Plan {
				t.Helper()
				rows, stamp, err := mirror.Load(current)
				if err != nil {
					t.Fatal(err)
				}
				existing, err := adapter.List(ctx)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := mirror.Compute(mirror.Render(rows, stamp), existing, stamp)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := mirror.Apply(ctx, adapter, plan); err != nil {
					t.Fatal(err)
				}
				return plan
			}
			// Export renders the contract.
			plan := export()
			if len(plan.Actions) != 1 || plan.Actions[0].Kind != mirror.ActionCreate || plan.Actions[0].Subject != "c-1" {
				t.Fatalf("the one contract is created: %+v", plan)
			}
			desired := plan.Actions[0].Issue
			before, count, state := ledgerDigest(t, ld), ledgerCount(t, ld), stateOf()
			// A person edits the mirror: retitles, relabels, closes.
			issues, err := adapter.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			id := issues[0].ID
			if forge != nil {
				var n int
				fmt.Sscanf(id, "%d", &n)
				forge.Edit(n, func(i *mirrortest.Issue) {
					i.Title = "please rename this"
					i.Labels = []string{"seed:done", "wontfix"}
					i.Closed = true
				})
			} else {
				if err := adapter.Update(ctx, id, mirror.Desired{Title: "please rename this", Body: desired.Body, Label: "seed:done", Closed: true}, []string{"wontfix"}); err != nil {
					t.Fatal(err)
				}
			}
			// The edit cannot move the ledger: nothing on the mirror side
			// holds a key, a ledger or an admission endpoint.
			if ledgerDigest(t, ld) != before || ledgerCount(t, ld) != count {
				t.Fatal("a mirror-side edit moved the ledger")
			}
			// The edit enters Seed only as a request, signed by the
			// enrolled standing-only service key, judged at admission.
			e, code := runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "c-1",
				"--origin", "mirror-"+name, "--kind", "mirror-edit", "--reference", "issues/"+id+" @ 0123456", "--summary", "rename c-1, close as wontfix")
			if code != 0 || !e.OK || e.Result["request"] == nil {
				t.Fatalf("the service key files the edit as a request: %d %+v", code, e)
			}
			if ledgerCount(t, ld) != count+1 {
				t.Fatalf("exactly one record, the request: %d", ledgerCount(t, ld))
			}
			if got := stateOf(); got != state {
				t.Fatalf("a request changes no lifecycle state: %q became %q", state, got)
			}
			// A direct coordination act by the same key refuses at
			// admission: standing files requests, and nothing else.
			// The service key is seed 32's (requestLedger).
			serviceKey := workerRawKey(32)
			for _, act := range [][]string{
				{"claim.taken", "c-1", "{}"},
				{"contract.cancelled", "c-1", `{"reason": "the mirror said so"}`},
				{"intent.filed", "c-2", `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`},
				{"request.answered", "c-1", `{"request": "` + e.Result["request"].(string) + `", "outcome": "declined", "reason": "no"}`},
			} {
				var oog *admit.OutOfGrantError
				if _, err := admitAppend(t, ld, serviceKey, act[0], act[1], act[2]); !errors.As(err, &oog) {
					t.Fatalf("the service key's %s refuses out of grant: %v", act[0], err)
				}
			}
			if e, code := runEnv(t, "request", "answer", "--ledger", ld, "--key", service, "--subject", "c-1",
				"--request", e.Result["request"].(string), "--outcome", "declined", "--reason", "no"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
				t.Fatalf("the service key answers nothing at the terminal either: %d %+v", code, e)
			}
			if ledgerCount(t, ld) != count+1 {
				t.Fatal("a refused act appended")
			}
			// The export restores the mirror from the projection: the
			// request moved the tip, so the stamp moves and every body
			// with it; the person's edits are overwritten, the foreign
			// label survives.
			rebuild()
			plan = export()
			if len(plan.Actions) != 1 || plan.Actions[0].Kind != mirror.ActionUpdate || plan.Actions[0].ID != id {
				t.Fatalf("one repair: %+v", plan)
			}
			why := strings.Join(plan.Actions[0].Why, "; ")
			for _, want := range []string{"title", "stale managed label seed:done", "should be open"} {
				if !strings.Contains(why, want) {
					t.Fatalf("the plan names the drift %q: %s", want, why)
				}
			}
			issues, err = adapter.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got := issues[0]
			if got.Title != "c-1" || got.Closed || !strings.Contains(got.Body, "position=") || strings.Contains(got.Body, desired.Body) {
				t.Fatalf("the projection wins and the stamp moved: %+v", got)
			}
			if strings.Join(got.Labels, ",") != "wontfix,seed:"+state {
				t.Fatalf("the managed label is the state, the foreign label survives: %v", got.Labels)
			}
			if plan = export(); len(plan.Actions) != 0 {
				t.Fatalf("converged: %+v", plan)
			}
		})
	}
}
