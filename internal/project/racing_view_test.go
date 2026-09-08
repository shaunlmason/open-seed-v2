package project_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: plans/os-56bee171.md AC4 — the contracts view carries the
// race: the racers while it runs, the settling position and the
// settled-out claims after, and nothing on a subject that never raced
// (so every existing view is byte-identical).
func TestContractsViewCarriesTheRace(t *testing.T) {
	rec := func(verb, subject, actor, payload string) *event.Record {
		return &event.Record{Event: event.Event{V: version.Seed6, TS: "2026-09-03T00:00:00Z", Actor: actor, Verb: verb, Subject: subject, Payload: json.RawMessage(payload)}}
	}
	packet := `{"acceptance": ["resume"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	records := []*event.Record{
		rec("intent.filed", "c-1", "root", `{"intent": "race", "tier": "trivial", "budget": "small", "routing": "core"}`),
		rec("contract.specified", "c-1", "root", `{"acceptance": {"ref": "spec.md @ 0123456789abcdef", "executable": false}}`),
		rec("claim.taken", "c-1", "alice", `{}`),
		rec("claim.taken", "c-1", "bob", `{}`),
		rec("intent.filed", "c-2", "root", `{"intent": "plain", "tier": "trivial", "budget": "small", "routing": "core"}`),
	}
	view := func(recs []*event.Record) map[string]any {
		files, err := project.Contracts().Build(recs, project.Inputs{})
		if err != nil {
			t.Fatal(err)
		}
		var out []map[string]any
		if err := json.Unmarshal(files["contracts.json"], &out); err != nil {
			t.Fatal(err)
		}
		bySubject := map[string]any{}
		for _, c := range out {
			bySubject[c["subject"].(string)] = c["racing"]
		}
		return bySubject
	}
	running := view(records)
	race, _ := running["c-1"].(map[string]any)
	racers, _ := race["racers"].([]any)
	if len(racers) != 2 || race["settled_at"] != nil {
		t.Fatalf("a running race lists both racers and no settlement: %+v", race)
	}
	if running["c-2"] != nil {
		t.Fatalf("a subject that never raced carries no racing object: %+v", running["c-2"])
	}
	settled := append(records,
		rec("submission.made", "c-1", "alice", `{"fence": "2", "packet": `+packet+`}`),
		rec("verdict.rendered", "c-1", "verifier", `{"verdict": "pass", "receipt": "`+strings.Repeat("0", 64)+`", "submission": "5", "independence": "L1"}`),
		rec("merge.requested", "c-1", "alice", `{"verdict": "6"}`),
		rec("merge.observed", "c-1", "root", `{"merged": "`+strings.Repeat("0", 40)+`", "pr": "pr/1"}`),
	)
	after := view(settled)
	race, _ = after["c-1"].(map[string]any)
	out, _ := race["settled_out"].([]any)
	if race["settled_at"] != "8" || len(out) != 1 || len(race["racers"].([]any)) != 0 {
		t.Fatalf("after settlement the view names the position and the settled-out claim: %+v", race)
	}
	// A subject that raced and whose racers all left keeps its racing
	// object, empty, so a reader can tell it raced; one that never
	// raced carries none (above).
	left := append(records,
		rec("claim.released", "c-1", "alice", `{"fence": "2", "packet": `+packet+`}`),
		rec("claim.released", "c-1", "bob", `{"fence": "3", "packet": `+packet+`}`),
	)
	if race, _ := view(left)["c-1"].(map[string]any); race == nil || len(race["racers"].([]any)) != 0 || race["settled_at"] != nil {
		t.Fatalf("after every racer left the object stands, empty and unsettled: %+v", race)
	}
	if project.Contracts().Version != "15" {
		t.Fatalf("the contracts view is version 14 with the racing object, got %s", project.Contracts().Version)
	}
}
