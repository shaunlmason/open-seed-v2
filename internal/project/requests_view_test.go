package project_test

import (
	"encoding/json"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: plans/os-48df10a2.md AC2 (the report) and the retention
// set — report.json counts the requests by kind and outcome with the
// answer latency from the records' own timestamps, and carries no
// requests section at all on a chain without one, so every existing
// build is byte-identical.
func TestReportCountsRequests(t *testing.T) {
	rec := func(ts, verb, subject, actor, payload string) *event.Record {
		return &event.Record{Event: event.Event{V: version.Seed7, TS: ts, Actor: actor, Verb: verb, Subject: subject, Payload: json.RawMessage(payload)}}
	}
	base := []*event.Record{
		rec("2026-09-03T00:00:00Z", "intent.filed", "c-1", "root", `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`),
		rec("2026-09-03T00:00:01Z", "contract.specified", "c-1", "root", `{"acceptance": {"ref": "spec.md @ 0123456789abcdef", "executable": false}}`),
	}
	build := func(recs []*event.Record) map[string]any {
		files, err := project.Report().Build(recs, project.Inputs{})
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(files["report.json"], &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, present := build(base)["requests"]; present {
		t.Fatal("a chain without a request carries no requests section")
	}
	withRequests := append(base,
		rec("2026-09-03T00:01:00Z", "request.filed", "system", "mirror", `{"origin": "mirror-a", "kind": "mirror-edit", "reference": "cards/c-1.md @ 0123456", "summary": "edit"}`),
		rec("2026-09-03T00:01:30Z", "intent.filed", "c-2", "dispatcher", `{"intent": "from the mirror", "tier": "trivial", "budget": "small", "routing": "core"}`),
		rec("2026-09-03T00:02:00Z", "request.answered", "system", "dispatcher", `{"request": "2", "outcome": "filed", "intent": "3"}`),
		rec("2026-09-03T00:03:00Z", "request.filed", "c-1", "dash", `{"origin": "dash", "kind": "dashboard-action", "reference": "`+zeros64+`", "summary": "cancel it", "about": "c-1"}`),
	)
	sec, ok := build(withRequests)["requests"].(map[string]any)
	if !ok {
		t.Fatal("the requests section is present")
	}
	if sec["total"] != 2.0 || sec["unanswered"] != 1.0 {
		t.Errorf("total and unanswered: %+v", sec)
	}
	kinds, _ := sec["by_kind"].(map[string]any)
	outcomes, _ := sec["by_outcome"].(map[string]any)
	if kinds["mirror-edit"] != 1.0 || kinds["dashboard-action"] != 1.0 || outcomes["filed"] != 1.0 || outcomes["pending"] != 1.0 {
		t.Errorf("by kind and by outcome: %+v %+v", kinds, outcomes)
	}
	if sec["mean_answer_seconds"] != "60.0" {
		t.Errorf("the latency is elapsed time between the two records: %v", sec["mean_answer_seconds"])
	}
	if only := build(append(base, withRequests[5])); only["requests"].(map[string]any)["mean_answer_seconds"] != nil {
		t.Errorf("nothing answered: no latency, stated as null: %+v", only["requests"])
	}
}

const zeros64 = "0000000000000000000000000000000000000000000000000000000000000000"
