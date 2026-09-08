package project_test

// The knowledge projection and the report's section
// (plans/os-f30ee0d3.md AC6, step 8): the stages published from the
// fold, and the report byte-identical on a chain carrying no curation
// fact.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func currentView(t *testing.T, out, name string) string {
	t.Helper()
	cur, err := os.ReadFile(filepath.Join(out, name, "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, name, "builds", strings.TrimSpace(string(cur)), name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKnowledgeProjectionPublishesTheStages(t *testing.T) {
	root, worker, curator := pKey(t, 1), pKey(t, 2), pKey(t, 5)
	dir, resolve, add := fixtureChain(t, root, worker, curator)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "claim"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, curator), enrollJSON(t, curator, "agent", "curator"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, curator), `{"capability": "curate"}`)

	// Before any curation fact the report carries no knowledge
	// section, so builds of such chains stay byte-identical.
	out := lockedTempOut(t, "before")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(currentView(t, out, "report"), `"knowledge"`) {
		t.Fatal("a chain with no curation fact gets no knowledge section")
	}
	if view := currentView(t, out, "knowledge"); !strings.Contains(view, `"observations": 0`) || !strings.Contains(view, `"hypotheses": []`) {
		t.Fatalf("the empty view counts zero and renders empty lists: %s", view)
	}

	for _, subject := range []string{"c-1", "c-2"} {
		add(root, version.Seed1, "intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
		add(root, version.Seed1, "contract.specified", subject, `{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`)
		add(worker, version.Seed1, "claim.taken", subject, `{}`)
	}
	// The claims stand at 8 and 11, the dead ends at 12 and 13: the
	// projection's fold re-judges each citation, so the fixture's
	// observations are real ones inside admitted windows.
	add(worker, version.Seed1, curation.DeadEndVerb, "c-1", `{"fence": "8", "tried": "x", "outcome": "y", "condition": "z", "environment": "w"}`)
	add(worker, version.Seed1, curation.DeadEndVerb, "c-2", `{"fence": "11", "tried": "x", "outcome": "y", "condition": "z", "environment": "w"}`)
	claim := "retry once"
	id := curation.HypothesisID(claim, nil)
	add(curator, version.Seed1, curation.HypothesisVerb, id, fmt.Sprintf(`{"claim": %q, "applies_when": {"routing": "core"}, "support": ["c-1@12", "c-2@13"], "exceptions": [], "provenance": []}`, claim))
	add(worker, version.Seed1, curation.HypothesisVerb, "h-000000000000", `{"claim": "x"}`)
	add(root, version.Seed1, curation.LessonVerb, "h-ffffffffffff", `{"lesson": "knowledge/lessons/x.md @ 0123456", "hypothesis": "h-ffffffffffff@3", "pr": "pr/1 @ 0123456", "carrier": "knowledge", "adversarial": {"eval": "e", "verdict": "1"}, "last_validated": "2026-09-01T00:00:00Z", "expires": "2026-12-01T00:00:00Z", "digest": "`+strings.Repeat("a", 64)+`"}`)
	// c-3 at 17..19 with a dead end at 20: the held-out observation
	// the contest cites (the fold re-judges the contest, so evidence
	// from the support set would move nothing).
	add(root, version.Seed1, "intent.filed", "c-3", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-3", `{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-3", `{}`)
	add(worker, version.Seed1, curation.DeadEndVerb, "c-3", `{"fence": "19", "tried": "x", "outcome": "y", "condition": "z", "environment": "w"}`)
	other := "retry twice"
	otherID := curation.HypothesisID(other, nil)
	add(curator, version.Seed1, curation.HypothesisVerb, otherID, fmt.Sprintf(`{"claim": %q, "applies_when": {"routing": "core"}, "support": ["c-1@12", "c-2@13"], "exceptions": [], "provenance": []}`, other))
	add(curator, version.Seed1, curation.ContestVerb, otherID, fmt.Sprintf(`{"hypothesis": "%s@21", "evidence": ["c-3@20"], "reason": "no"}`, otherID))
	// A contest raw-pushed by the worker, citing the support set:
	// shape-valid, never admitted, an anomaly rather than a stage.
	add(worker, version.Seed1, curation.ContestVerb, id, fmt.Sprintf(`{"hypothesis": "%s@14", "evidence": ["c-1@12"], "reason": "no"}`, id))

	out2 := lockedTempOut(t, "after")
	if _, err := project.Rebuild(dir, out2, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	view := currentView(t, out2, "knowledge")
	for _, want := range []string{`"observations": 3`, `"hypotheses": 2`, `"promoted": 0`, `"contested": 1`, `"lessons": 0`, `"unbound": 1`, `"anomalies": 2`, `"id": "` + id + `"`, `"stage": "proposed"`, `"stage": "contested"`, `"single_actor_family": true`} {
		if !strings.Contains(view, want) {
			t.Errorf("the knowledge view carries %s: %s", want, view)
		}
	}
	// The report's derivation version moved with the section, so an
	// already-published prefix republishes with it rather than keeping
	// a same-id tree without it (review finding on the item 3 PR).
	if v := project.Report().Version; v != "19" {
		t.Fatalf("the report's version names the knowledge section (11, its retired and stale counts at 12), the lanes section after them (13), by_kind (15), the planner's strongest (16), the adapters section (17) and the blind-retry counts (18): %s", v)
	}
	report := currentView(t, out2, "report")
	if !strings.Contains(report, `"knowledge"`) || !strings.Contains(report, `"hypotheses": 2`) || !strings.Contains(report, `"contested": 1`) {
		t.Fatalf("the report counts the stages once a curation fact stands: %s", report)
	}
	if !strings.Contains(currentView(t, out, "report"), `"position": 6`) {
		t.Fatal("the earlier build is the earlier prefix's")
	}
}
