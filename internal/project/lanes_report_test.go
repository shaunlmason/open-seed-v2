package project_test

import (
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	lanesDigestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lanesDigestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// conformance: plans/os-6bd9ffff.md D6, AC6 — on a chain with four
// specified contracts of which one was re-specified, and three
// approvals of which one is unedited, one edited and one before
// seed/4: retriage_rate "0.250", unedited_rate "0.500", unmeasured 1;
// the section is null when no work subject exists; version "12"
// republishes existing prefixes.
func TestReportLanesSection(t *testing.T) {
	root := pKey(t, 1)
	dir, resolve, add := fixtureChain(t, root)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	add(root, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)

	// No work subject yet: the section is null, stated rather than
	// fabricated as zeros.
	out := lockedTempOut(t, "empty")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	if rep.Lanes != nil {
		t.Fatalf("no work subject: the lanes section is null, got %+v", rep.Lanes)
	}
	if !strings.Contains(currentView(t, out, "report"), `"lanes": null`) {
		t.Fatal("the null section is stated in the view")
	}

	filed := `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`
	spec := `{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`
	// c-3 is planned and approved at seed/3, where the plan verbs carry
	// no digest: its approval is unmeasured.
	add(root, version.Seed3, "intent.filed", "c-3", filed)
	add(root, version.Seed3, "contract.specified", "c-3", spec)
	add(root, version.Seed3, "plan.proposed", "c-3", `{"plan": "plans/c-3.md @ abc1234"}`)
	add(root, version.Seed3, "plan.approved", "c-3", `{"plan": "plans/c-3.md @ abc1234", "pr": "pr/3 @ abc1234"}`)
	add(root, version.Seed3, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	for _, subject := range []string{"c-1", "c-2", "c-4"} {
		add(root, version.Seed4, "intent.filed", subject, filed)
		add(root, version.Seed4, "contract.specified", subject, spec)
	}
	// c-1 re-specified once (the dispatcher revising its triage), and
	// approved unedited; c-2 approved with a different digest: edited.
	add(root, version.Seed4, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/other.md @ def5678", "executable": false}}`)
	add(root, version.Seed4, "plan.proposed", "c-1", `{"plan": "plans/c-1.md @ abc1234", "digest": "`+lanesDigestA+`"}`)
	add(root, version.Seed4, "plan.approved", "c-1", `{"plan": "plans/c-1.md @ abc1234", "pr": "pr/1 @ abc1234", "digest": "`+lanesDigestA+`"}`)
	add(root, version.Seed4, "plan.proposed", "c-2", `{"plan": "plans/c-2.md @ abc1234", "digest": "`+lanesDigestA+`"}`)
	add(root, version.Seed4, "plan.approved", "c-2", `{"plan": "plans/c-2.md @ def5678", "pr": "pr/2 @ def5678", "digest": "`+lanesDigestB+`"}`)

	out2 := lockedTempOut(t, "lanes")
	if _, err := project.Rebuild(dir, out2, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	readView(t, out2, "report", project.ReportFile, &rep)
	if rep.Lanes == nil {
		t.Fatal("work subjects exist: the lanes section is present")
	}
	d, p := rep.Lanes.Dispatcher, rep.Lanes.Planner
	if d.Specified != 4 || d.Respecified != 1 || d.RetriageRate == nil || *d.RetriageRate != "0.250" {
		t.Fatalf("dispatcher: 4 specified, 1 re-specified, rate 0.250; got %+v", d)
	}
	if p.Approvals != 3 || p.Unedited != 1 || p.Edited != 1 || p.Unmeasured != 1 || p.UneditedRate == nil || *p.UneditedRate != "0.500" {
		t.Fatalf("planner: 3 approvals, 1 unedited, 1 edited, 1 unmeasured, rate 0.500; got %+v", p)
	}
	view := currentView(t, out2, "report")
	for _, want := range []string{`"retriage_rate": "0.250"`, `"unedited_rate": "0.500"`, `"unmeasured": 1`} {
		if !strings.Contains(view, want) {
			t.Errorf("the report carries %s: %s", want, view)
		}
	}
	// The version moves with each section: "13" was this one's, and
	// the flywheel section (plans/os-9075c308.md D5) took "14" when
	// the two cards met in the merge. The pin is on the derivation's
	// current version, never on the number this section introduced.
	if v := project.Report().Version; v != "19" {
		t.Fatalf("the report's version moves with its sections: %s", v)
	}
	// The by_kind split (plans/os-0d4f2af3.md D6) partitions the
	// totals: every figure sums across kinds to the section's own.
	if len(rep.Lanes.ByKind) == 0 {
		t.Fatal("work subjects exist: the by_kind split is present")
	}
	var sumSpec, sumRespec, sumApprovals, sumUnedited, sumEdited, sumUnmeasured int
	for _, k := range rep.Lanes.ByKind {
		sumSpec += k.Dispatcher.Specified
		sumRespec += k.Dispatcher.Respecified
		sumApprovals += k.Planner.Approvals
		sumUnedited += k.Planner.Unedited
		sumEdited += k.Planner.Edited
		sumUnmeasured += k.Planner.Unmeasured
	}
	if sumSpec != d.Specified || sumRespec != d.Respecified || sumApprovals != p.Approvals || sumUnedited != p.Unedited || sumEdited != p.Edited || sumUnmeasured != p.Unmeasured {
		t.Fatalf("by_kind sums to the totals: %+v vs %+v %+v", rep.Lanes.ByKind, d, p)
	}

	// A specified set with no approval, and an approval set with no
	// measured member, each leave their rate null: a rate over nothing
	// is not zero.
	add(root, version.Seed4, "plan.proposed", "c-4", `{"plan": "plans/c-4.md @ abc1234", "digest": "`+lanesDigestA+`"}`)
	out3 := lockedTempOut(t, "more")
	if _, err := project.Rebuild(dir, out3, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	readView(t, out3, "report", project.ReportFile, &rep)
	if rep.Lanes.Planner.Approvals != 3 {
		t.Fatalf("a proposal without an approval is not an approval: %+v", rep.Lanes.Planner)
	}
}

// conformance: D6 — the rates are null at a zero denominator, never
// "0.000": a chain whose only work subject was filed and never
// specified or planned.
func TestReportLanesRatesAreNullOverNothing(t *testing.T) {
	root := pKey(t, 1)
	dir, resolve, add := fixtureChain(t, root)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	out := lockedTempOut(t, "null")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	if rep.Lanes == nil || rep.Lanes.Dispatcher.RetriageRate != nil || rep.Lanes.Planner.UneditedRate != nil {
		t.Fatalf("both rates are null over nothing: %+v", rep.Lanes)
	}
	if view := currentView(t, out, "report"); !strings.Contains(view, `"retriage_rate": null`) || !strings.Contains(view, `"unedited_rate": null`) {
		t.Fatalf("the null rates are stated: %s", view)
	}
}
