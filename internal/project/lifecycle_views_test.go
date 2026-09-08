package project_test

// The transition-table projection drills (plans/os-d69a6c91.md step 7
// "Projections" and "Tolerant fold"): the queue lists exactly the
// ready subjects with correct since_position, the contracts view
// carries the folded state and a visible anomaly count for raw-pushed
// illegal history, both republish under version-bearing v2 build ids,
// and the cache mirrors the same derivations.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestLifecycleViews(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	// c-A runs filed → specified → taken and leaves the queue.
	add(root, version.Seed1, "intent.filed", "c-A", `{"intent": "fix", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-A", `{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-A", `{}`)
	// c-B stops at specified and is the one ready subject.
	add(root, version.Seed1, "intent.filed", "c-B", `{"intent": "add", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-B", `{"acceptance": {"ref": "specs/b.md @ def1234", "executable": false}}`)
	// c-C carries a raw-pushed illegal claim (backlog is unclaimable):
	// tolerated in history, skipped by the fold, counted visibly.
	add(root, version.Seed1, "intent.filed", "c-C", `{"intent": "try", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(worker, version.Seed1, "claim.taken", "c-C", `{}`)
	// c-D never enters the lifecycle: a free work verb only.
	add(worker, version.Seed1, "task.note", "c-D", `{"n": 1}`)
	// c-E carries a raw-pushed ungated executable spec: tolerated in
	// history, visibly anomalous, and never marked gated.
	add(root, version.Seed1, "intent.filed", "c-E", `{"intent": "arm", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-E", `{"acceptance": {"ref": "specs/e.sh @ abc1234", "executable": true}}`)
	out := rebuildAll(t, dir, resolve)

	// The queue: exactly c-B, since the position of its specification.
	var q project.QueueView
	readView(t, out, "queue", project.QueueFile, &q)
	if q.Derivation != project.QueueDerivationEffective {
		t.Fatalf("queue derivation: %+v", q)
	}
	if len(q.Ready) != 2 || q.Ready[0].Subject != "c-B" || q.Ready[0].SincePosition != 7 ||
		q.Ready[1].Subject != "c-E" || q.Ready[1].SincePosition != 12 {
		t.Fatalf("the queue must list exactly the ready subjects oldest first: %+v", q.Ready)
	}

	// The contracts view: folded states and the visible anomaly count.
	var entries []project.ContractEntry
	readView(t, out, "contracts", project.ContractsFile, &entries)
	bys := map[string]project.ContractEntry{}
	for _, e := range entries {
		bys[e.Subject] = e
	}
	wantState := func(subject, state string, anomalies int) {
		t.Helper()
		e, ok := bys[subject]
		if !ok {
			t.Fatalf("missing contract entry %s", subject)
		}
		if state == "" {
			if e.State != nil {
				t.Fatalf("%s must have a null state, got %q", subject, *e.State)
			}
		} else if e.State == nil || *e.State != state {
			t.Fatalf("%s state: want %q got %v", subject, state, e.State)
		}
		if e.Anomalies != anomalies {
			t.Fatalf("%s anomalies: want %d got %d", subject, anomalies, e.Anomalies)
		}
	}
	wantState("c-A", "in_progress", 0)
	wantState("c-B", "ready", 0)
	wantState("c-C", "backlog", 1)
	wantState("c-D", "", 0)
	wantState("c-E", "ready", 1)
	if a := bys["c-E"].Acceptance; a == nil || !a.Executable || a.Gated {
		t.Fatalf("raw ungated executable content must surface ungated: %+v", a)
	}

	// The claim object rides exactly the in_progress entry: holder and
	// fence (the admitted claim.taken position, string form), absent
	// everywhere else (plans/os-5dc16a7c.md).
	if c := bys["c-A"].Claim; c == nil || c.Holder != pFP(t, worker) || c.Fence != "5" {
		t.Fatalf("c-A must carry the claim object {holder, fence}: %+v", c)
	}
	for _, subject := range []string{"c-B", "c-C", "c-D"} {
		if bys[subject].Claim != nil {
			t.Fatalf("%s is not in_progress and must carry no claim", subject)
		}
	}

	// The acceptance field rides every specified subject: prose-only
	// specs read gated (no gate required); unspecified subjects carry
	// none (plans/os-73c00a50.md).
	if a := bys["c-A"].Acceptance; a == nil || a.Ref != "specs/a.md @ abc1234" || a.Executable || !a.Gated {
		t.Fatalf("c-A acceptance wrong: %+v", a)
	}
	if a := bys["c-C"].Acceptance; a != nil {
		t.Fatalf("an unspecified subject carries no acceptance: %+v", a)
	}

	// The derivation bumps are in the identity (Phase 4's
	// version-in-identity machinery on real derivation changes): the
	// queue is at v2 (5.1), contracts and cache at v4 (5.2's claim
	// object, 5.4's acceptance field).
	for name, want := range map[string]string{"queue": "-v4", "contracts": "-v15", "cache": "-v15"} {
		b, err := os.ReadFile(filepath.Join(out, name, "CURRENT"))
		if err != nil {
			t.Fatal(err)
		}
		if id := strings.TrimSpace(string(b)); !strings.HasSuffix(id, want) {
			t.Fatalf("%s must publish under a %s build id, got %s", name, want, id)
		}
	}

	// The cache mirrors both derivations.
	db, _ := openCacheRO(t, out)
	defer db.Close()
	if n := one[int](t, db, `SELECT COUNT(*) FROM queue`); n != 2 {
		t.Fatalf("cache queue rows: %d", n)
	}
	if since := one[int](t, db, `SELECT since_position FROM queue WHERE subject = 'c-B'`); since != 7 {
		t.Fatalf("cache queue since: %d", since)
	}
	if st := one[string](t, db, `SELECT state FROM contract_state WHERE subject = 'c-A'`); st != "in_progress" {
		t.Fatalf("cache c-A state: %s", st)
	}
	if n := one[int](t, db, `SELECT anomalies FROM contract_state WHERE subject = 'c-C'`); n != 1 {
		t.Fatalf("cache c-C anomalies: %d", n)
	}
	if h := one[string](t, db, `SELECT holder FROM contract_state WHERE subject = 'c-A'`); h != pFP(t, worker) {
		t.Fatalf("cache c-A holder: %s", h)
	}
	if f := one[string](t, db, `SELECT fence FROM contract_state WHERE subject = 'c-A'`); f != "5" {
		t.Fatalf("cache c-A fence: %s", f)
	}
	var noClaim sql.NullString
	if err := db.QueryRow(`SELECT holder FROM contract_state WHERE subject = 'c-B'`).Scan(&noClaim); err != nil {
		t.Fatal(err)
	}
	if noClaim.Valid {
		t.Fatalf("no claim window, no holder: %q", noClaim.String)
	}
	var null sql.NullString
	if err := db.QueryRow(`SELECT state FROM contract_state WHERE subject = 'c-D'`).Scan(&null); err != nil {
		t.Fatal(err)
	}
	if null.Valid {
		t.Fatalf("a subject the lifecycle never created must mirror a NULL state, got %q", null.String)
	}
	if r := one[string](t, db, `SELECT acc_ref FROM contract_state WHERE subject = 'c-A'`); r != "specs/a.md @ abc1234" {
		t.Fatalf("cache c-A acceptance ref: %s", r)
	}
	if g := one[int](t, db, `SELECT acc_gated FROM contract_state WHERE subject = 'c-A'`); g != 1 {
		t.Fatalf("cache c-A gated: %d", g)
	}
}
