package project_test

// The report's flywheel section (plans/os-9075c308.md D5;
// spec/flywheel.md): null on an empty ledger, the conversion
// rows once work subjects exist, derived from the record alone; the
// version moved with the section so an already-published prefix
// republishes with it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func recordsOf(t *testing.T, dir string) []*event.Record {
	t.Helper()
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var records []*event.Record
	if err := store.Records(func(pos int, rec *event.Record) error {
		records = append(records, rec)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestReportFlywheelSectionDerivesFromTheRecord(t *testing.T) {
	root, worker, verifier, observer, curator := pKey(t, 1), pKey(t, 2), pKey(t, 3), pKey(t, 4), pKey(t, 5)
	dir, resolve, add := fixtureChain(t, root, worker, verifier, observer, curator)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "claim"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, verifier), enrollJSON(t, verifier, "agent", "verifier"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, verifier), `{"capability": "verdict"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, observer), enrollJSON(t, observer, "agent", "observer"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, observer), `{"capability": "observer"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, curator), enrollJSON(t, curator, "agent", "curator"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, curator), `{"capability": "curate"}`)

	// No work subject: the section is null and the version is the
	// section's.
	out := lockedTempOut(t, "empty")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if report := currentView(t, out, "report"); !strings.Contains(report, `"flywheel": null`) {
		t.Fatalf("no work subject, a null flywheel section: %s", report)
	}
	if project.Report().Version != "19" {
		t.Fatalf("the report's version moves with its sections (15: the lanes section's by_kind split, plans/os-0d4f2af3.md D6; 16: the planner's strongest, plans/os-c7554f18.md D3; 17: the adapters section, plans/os-083112ac.md; 18: the blind-retry counts, plans/os-a9e715dc.md D3): %s", project.Report().Version)
	}

	// Two done contracts of one shape: recurring, unproposed. The
	// positions are the chain's: the enrollment ends at 9, so the
	// first filing is 10.
	packet := `{"acceptance": ["done"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	pos := 10
	done := func(subject string) int {
		add(root, version.Seed1, "intent.filed", subject, `{"intent": "chore", "tier": "trivial", "budget": "small", "routing": "core"}`)
		add(root, version.Seed1, "contract.specified", subject, `{"acceptance": {"ref": "accept.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`)
		add(worker, version.Seed1, "claim.taken", subject, `{}`)
		add(worker, version.Seed1, "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, pos+2, packet))
		add(verifier, version.Seed1, "verdict.rendered", subject, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("ab", 32), pos+3))
		add(worker, version.Seed1, "merge.requested", subject, fmt.Sprintf(`{"verdict": "%d"}`, pos+4))
		add(observer, version.Seed1, "merge.observed", subject, `{"merged": "`+strings.Repeat("ef", 20)+`", "pr": "pr/1"}`)
		pos += 7
		return pos - 1
	}
	d1, d2 := done("c-1"), done("c-2")
	out2 := lockedTempOut(t, "recurring")
	if _, err := project.Rebuild(dir, out2, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	report := currentView(t, out2, "report")
	for _, want := range []string{`"recurring": 1`, `"proposed": 0`, `"merged": 0`, `"filed": 0`, `"done": 0`, `"conversion_rate": "0.000"`} {
		if !strings.Contains(report, want) {
			t.Fatalf("the flywheel section carries %s: %s", want, report)
		}
	}

	// The curator's proposal and the observer's merge: converted; a raw
	// proposal under the worker's key binds nothing.
	records := recordsOf(t, dir)
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	shapes := flywheel.Shapes(records, table.FoldRecords(records))
	if len(shapes) != 1 || !shapes[0].Recurring() || shapes[0].Occurrences[1].Done != d2 {
		t.Fatalf("one recurring shape with its occurrences at the observations: %+v", shapes)
	}
	shape := shapes[0]
	workflow := ".seed/workflows/" + shape.Name() + ".yaml @ " + strings.Repeat("ab", 20)
	add(curator, version.Seed1, flywheel.ProposedVerb, shape.ID, fmt.Sprintf(`{"shape": %q, "workflow": %q, "occurrences": ["c-1@%d", "c-2@%d"], "validated": {"run": "wf-1"}}`, shape.ID, workflow, d1, d2))
	add(worker, version.Seed1, flywheel.ProposedVerb, shape.ID, fmt.Sprintf(`{"shape": %q, "workflow": ".seed/workflows/x.yaml @ %s", "occurrences": ["c-1@%d", "c-2@%d"], "validated": {"run": "wf-raw"}}`, shape.ID, strings.Repeat("ab", 20), d1, d2))
	add(observer, version.Seed1, flywheel.MergedVerb, shape.ID, fmt.Sprintf(`{"workflow": %q, "shape": %q, "pr": "pr/9 @ %s"}`, workflow, shape.ID, strings.Repeat("ab", 20)))
	out3 := lockedTempOut(t, "converted")
	if _, err := project.Rebuild(dir, out3, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	report = currentView(t, out3, "report")
	for _, want := range []string{`"recurring": 1`, `"proposed": 1`, `"merged": 1`, `"conversion_rate": "1.000"`} {
		if !strings.Contains(report, want) {
			t.Fatalf("the converted chore counts: %s", report)
		}
	}
	if !strings.Contains(currentView(t, out, "report"), `"position": 10`) {
		t.Fatal("the earlier build is the earlier prefix's")
	}
}
