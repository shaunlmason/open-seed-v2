package project_test

// The standard-projection drills (plans/os-fecfb3f7.md step 7;
// conformance III.D): the v0 work classifier and contract grouping,
// the queue's honest emptiness, actor histories surviving revocation,
// and report facts equal to the fixture chain's.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// readView rebuilds nothing: it resolves the published build and
// unmarshals one view file.
func readView(t *testing.T, out, name, file string, into any) {
	t.Helper()
	build, err := project.Current(out, name)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(build, file))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatal(err)
	}
}

func rebuildAll(t *testing.T, dir string, resolve ledger.Resolver) string {
	t.Helper()
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestContractsClassifierAndGrouping(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, "task.opened", "c-A", `{"title": "first"}`)
	add(worker, version.Seed1, "task.opened", "c-B", `{"title": "second"}`)
	add(worker, version.Seed1, "task.note", "c-A", `{"n": 1}`)
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "maintenance"}`)
	add(root, version.Seed1, "task.done", "c-B", `{}`)
	out := rebuildAll(t, dir, resolve)

	var entries []project.ContractEntry
	readView(t, out, "contracts", project.ContractsFile, &entries)
	if len(entries) != 2 || entries[0].Subject != "c-A" || entries[1].Subject != "c-B" {
		t.Fatalf("subjects must group in first-appearance order: %+v", entries)
	}
	a, b := entries[0], entries[1]
	if a.FirstPosition != 3 || a.LastPosition != 5 || len(a.Events) != 2 {
		t.Fatalf("c-A stream wrong: %+v", a)
	}
	if a.Events[0].Actor != pFP(t, root) || a.Events[1].Actor != pFP(t, worker) {
		t.Fatalf("actors must be signer fingerprints: %+v", a.Events)
	}
	var got, want bytes.Buffer
	if err := json.Compact(&got, a.Events[1].Payload); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&want, []byte(`{"n": 1}`)); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		// The view re-indents; the JSON content must be unchanged
		// (canonical bytes live only in the ledger, per the spec).
		t.Fatalf("payload content must be preserved: %q", a.Events[1].Payload)
	}
	if b.FirstPosition != 4 || b.LastPosition != 7 || b.Events[1].Verb != "task.done" {
		t.Fatalf("c-B stream wrong: %+v", b)
	}
	for _, e := range append(a.Events, b.Events...) {
		// The classifier: governance vocabulary never appears in a
		// contract stream.
		if e.Verb == ledger.UpgradeVerb || keyring.IsActorVerb(e.Verb) {
			t.Fatalf("governance event leaked into contracts: %+v", e)
		}
	}

	// A chain with no work vocabulary yields an empty array, not a
	// missing file.
	dir2, resolve2, _ := fixtureChain(t, pKey(t, 3))
	out2 := rebuildAll(t, dir2, resolve2)
	var empty []project.ContractEntry
	readView(t, out2, "contracts", project.ContractsFile, &empty)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("a work-free chain must publish an empty array: %+v", empty)
	}
}

func TestQueueEmptyWhenNothingIsReady(t *testing.T) {
	// The derivation is the transition table's (plans/os-d69a6c91.md,
	// retiring the v0 "none" marker as the 4.2 spec promised): on
	// fixtures carrying no ready subject, ready is empty because
	// nothing is ready, and the marker says which derivation decided.
	dir, resolve, _, _ := lifecycleChain(t)
	rootOnlyDir, rootOnlyResolve, _ := fixtureChain(t, pKey(t, 7))
	for _, fx := range []struct {
		dir string
		res ledger.Resolver
	}{{dir, resolve}, {rootOnlyDir, rootOnlyResolve}} {
		out := rebuildAll(t, fx.dir, fx.res)
		var q project.QueueView
		readView(t, out, "queue", project.QueueFile, &q)
		if q.SchemaVersion != project.QueueSchemaVersion || q.Derivation != project.QueueDerivationEffective {
			t.Fatalf("the queue must name the transition derivation: %+v", q)
		}
		if q.Ready == nil || len(q.Ready) != 0 {
			t.Fatalf("nothing on these fixtures is ready; ready must be empty, not absent: %+v", q)
		}
	}
}

func TestActorsHistorySurvivesRevocation(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "maintenance"}`)
	add(worker, version.Seed1, "message.sent", "c-0001", `{"n": 1}`)
	add(root, version.Seed1, keyring.VerbRevoked, pFP(t, worker), `{"reason": "drill"}`)
	out := rebuildAll(t, dir, resolve)

	var actors []project.ActorEntry
	readView(t, out, "actors", project.ActorsFile, &actors)
	if len(actors) != 2 {
		t.Fatalf("root and worker must both appear: %+v", actors)
	}
	r, w := actors[0], actors[1]
	if !r.Root || r.Fingerprint != pFP(t, root) || r.Standing != "active" {
		t.Fatalf("root entry wrong: %+v", r)
	}
	if len(r.StandingHistory) != 0 {
		t.Fatalf("no actor.* event targets the genesis root here: %+v", r.StandingHistory)
	}
	if len(r.Signed) != 5 || r.Signed[0].Verb != "system.genesis" {
		t.Fatalf("the root signed everything but the milestone: %+v", r.Signed)
	}
	if w.Standing != "revoked" || w.Kind != "agent" || w.Name != "worker" {
		t.Fatalf("worker entry wrong: %+v", w)
	}
	if len(w.Grants) != 1 || w.Grants[0] != "maintenance" {
		t.Fatalf("the grant stays visible history on the entry: %+v", w.Grants)
	}
	wantHist := []project.StandingEvent{
		{Position: 2, Verb: keyring.VerbEnrolled, Acting: pFP(t, root)},
		{Position: 3, Verb: keyring.VerbGranted, Acting: pFP(t, root)},
		{Position: 5, Verb: keyring.VerbRevoked, Acting: pFP(t, root)},
	}
	if len(w.StandingHistory) != len(wantHist) {
		t.Fatalf("standing history wrong: %+v", w.StandingHistory)
	}
	for i, want := range wantHist {
		if w.StandingHistory[i] != want {
			t.Fatalf("standing history [%d] = %+v, want %+v", i, w.StandingHistory[i], want)
		}
	}
	if len(w.Signed) != 1 || w.Signed[0] != (project.SignedEvent{Position: 4, Verb: "message.sent", Subject: "c-0001"}) {
		t.Fatalf("the revoked key's attribution must survive: %+v", w.Signed)
	}

	// Cross-view agreement: the roster shows the same ended standing
	// and grants for the same fingerprint.
	var roster []project.RosterEntry
	readView(t, out, "roster", project.RosterFile, &roster)
	if roster[1].Fingerprint != w.Fingerprint || roster[1].Standing != "revoked" || roster[1].Grants[0] != "maintenance" {
		t.Fatalf("roster and actor view disagree: %+v vs %+v", roster[1], w)
	}
}

func TestReportFactsMatchTheChain(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(worker, version.Seed1, "message.sent", "c-0001", `{"n": 1}`)
	add(root, version.Seed1, "system.checkpoint", "system", `{"note": "drill"}`)
	add(root, version.Seed1, keyring.VerbRevoked, pFP(t, worker), `{"reason": "drill"}`)
	out := rebuildAll(t, dir, resolve)

	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	var stamp project.Stamp
	readView(t, out, "report", project.StampFile, &stamp)
	if rep.Chain.Position != 6 || rep.Chain.Position != stamp.Position || rep.Chain.Tip != stamp.Tip {
		t.Fatalf("the chain section must equal the stamp: %+v vs %+v", rep.Chain, stamp)
	}
	if rep.Chain.ActiveVersion != version.Seed1 {
		t.Fatalf("active version wrong: %+v", rep.Chain)
	}
	if rep.Actors.Total != 2 || rep.Actors.Roots != 1 ||
		rep.Actors.ByStanding["active"] != 1 || rep.Actors.ByStanding["revoked"] != 1 {
		t.Fatalf("actor counts wrong: %+v", rep.Actors)
	}
	if rep.Halt.Halted || rep.Halt.DeclaredPosition != nil {
		t.Fatalf("an unhalted chain must not carry a declaration: %+v", rep.Halt)
	}
	if rep.Checkpoints.Count != 1 || rep.Checkpoints.LastPosition == nil || *rep.Checkpoints.LastPosition != 4 {
		t.Fatalf("checkpoint facts wrong: %+v", rep.Checkpoints)
	}
	if rep.Contracts.Subjects != 1 || rep.Contracts.Events != 1 {
		t.Fatalf("contract counts wrong: %+v", rep.Contracts)
	}

	// A halted chain reports the declaring position and actor.
	hDir, hResolve, hAdd := fixtureChain(t, pKey(t, 5))
	hRoot := pKey(t, 5)
	hAdd(hRoot, version.Protocol, halt.DeclareVerb, "system", `{"reason": "incident"}`)
	hOut := rebuildAll(t, hDir, hResolve)
	var hRep project.ReportView
	readView(t, hOut, "report", project.ReportFile, &hRep)
	if !hRep.Halt.Halted || hRep.Halt.DeclaredPosition == nil || *hRep.Halt.DeclaredPosition != 1 || hRep.Halt.By != pFP(t, hRoot) {
		t.Fatalf("halt facts wrong: %+v", hRep.Halt)
	}

	// The root-only ledger: one active root, nothing else.
	rDir, rResolve, _ := fixtureChain(t, pKey(t, 6))
	rOut := rebuildAll(t, rDir, rResolve)
	var rRep project.ReportView
	readView(t, rOut, "report", project.ReportFile, &rRep)
	if rRep.Actors.Total != 1 || rRep.Actors.Roots != 1 || rRep.Actors.ByStanding["active"] != 1 {
		t.Fatalf("root-only actor counts wrong: %+v", rRep.Actors)
	}
	if rRep.Contracts.Subjects != 0 || rRep.Contracts.Events != 0 || rRep.Checkpoints.Count != 0 {
		t.Fatalf("root-only work counts wrong: %+v", rRep)
	}
}
