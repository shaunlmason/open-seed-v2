package project_test

// The obligations projection drills (plans/os-52d5da3f.md): the view
// publishes the standing rows, builds byte-identically, and stays
// input-free — the rows are a pure function of the verified prefix.

import (
	"bytes"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestObligationsProjection(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "claim"}`)
	add(root, version.Seed1, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`)

	out := lockedTempOut(t, "views")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	var view project.ObligationsView
	readView(t, out, "obligations", project.ObligationsFile, &view)
	if len(view.Obligations) != 0 {
		t.Fatalf("an unclaimed ready contract owes nothing: %+v", view.Obligations)
	}

	// The claim creates the obligation, owed by its holder, naming
	// what discharges it.
	add(worker, version.Seed1, "claim.taken", "c-1", `{}`)
	out2 := lockedTempOut(t, "views")
	if _, err := project.Rebuild(dir, out2, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	readView(t, out2, "obligations", project.ObligationsFile, &view)
	if len(view.Obligations) != 1 {
		t.Fatalf("the claim is the one standing obligation: %+v", view.Obligations)
	}
	row := view.Obligations[0]
	if row.Subject != "c-1" || row.Kind != "claim.held" || row.OwedBy != pFP(t, worker) {
		t.Fatalf("the row names subject, kind and holder: %+v", row)
	}
	if len(row.DischargedBy) == 0 {
		t.Fatalf("the row names what discharges it: %+v", row)
	}

	// Byte-identical for the same prefix: the projection discipline.
	out3 := lockedTempOut(t, "views")
	if _, err := project.Rebuild(dir, out3, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readRaw(t, out2, "obligations", project.ObligationsFile),
		readRaw(t, out3, "obligations", project.ObligationsFile)) {
		t.Fatal("identical prefixes must build identical obligation bytes")
	}
}
