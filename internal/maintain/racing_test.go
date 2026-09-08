package maintain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: plans/os-56bee171.md AC3 (the reaper) — the pass reaps
// every claim a settled race left active, with a packet naming the
// settlement, and classifies each active claim of an unsettled race on
// its own stream; the fold below is the lifecycle's own over seed/6
// records (the boundary's admission of the reap is drilled in
// internal/admit).
func TestPassReapsSettledOutRacers(t *testing.T) {
	rec := func(verb, subject, actor, payload string) *event.Record {
		return &event.Record{Event: event.Event{V: version.Seed6, TS: "2026-09-03T00:00:00Z", Actor: actor, Verb: verb, Subject: subject, Payload: json.RawMessage(payload)}}
	}
	packet := `{"acceptance": ["resume"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	records := []*event.Record{
		rec("intent.filed", "c-1", "root", `{"intent": "race", "tier": "trivial", "budget": "small", "routing": "core"}`),
		rec("contract.specified", "c-1", "root", `{"acceptance": {"ref": "spec.md @ 0123456789abcdef", "executable": false}}`),
		rec("claim.taken", "c-1", "alice", `{}`),
		rec("claim.taken", "c-1", "bob", `{}`),
		rec("submission.made", "c-1", "alice", `{"fence": "2", "packet": `+packet+`}`),
		rec("verdict.rendered", "c-1", "verifier", `{"verdict": "pass", "receipt": "`+strings.Repeat("0", 64)+`", "submission": "4", "independence": "L1"}`),
		rec("merge.requested", "c-1", "alice", `{"verdict": "5"}`),
		rec("merge.observed", "c-1", "root", `{"merged": "`+strings.Repeat("0", 40)+`", "pr": "pr/1"}`),
	}
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	fold := table.FoldRecords(records)
	s, ok := fold.State("c-1")
	if !ok || s.State != "done" || s.RaceSettled == nil || len(s.Claims) != 1 || s.Claims[0].Holder != "bob" {
		t.Fatalf("done with bob settled-out: %+v", s)
	}
	var appended []string
	rep, err := Run(Deps{
		Fold: fold, Obs: &obs.Snapshot{}, Now: time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC),
		Append: func(verb, subject string, payload []byte) error {
			appended = append(appended, verb+" "+subject+" "+string(payload))
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Reaped) != 1 || rep.Reaped[0].Holder != "bob" || rep.Reaped[0].Fence != 3 || rep.Reaped[0].State != "settled" {
		t.Fatalf("one settled reap of bob's claim: %+v", rep.Reaped)
	}
	if len(appended) != 1 || !strings.HasPrefix(appended[0], "claim.reaped c-1 ") || !strings.Contains(appended[0], `"fence":"3"`) || !strings.Contains(appended[0], "settled at position 7") {
		t.Fatalf("the reap cites bob's fence and names the settlement: %v", appended)
	}
	if !strings.Contains(rep.Reaped[0].Because, "settled at position 7") {
		t.Fatalf("the report says why: %q", rep.Reaped[0].Because)
	}
	// An unsettled race: both claims active, each classified on its
	// own stream — with no observations neither is reaped, and both
	// are reported skipped.
	open := table.FoldRecords(records[:4])
	rep, err = Run(Deps{Fold: open, Obs: &obs.Snapshot{}, Now: time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC), Append: func(string, string, []byte) error { t.Fatal("nothing to reap"); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 2 {
		t.Fatalf("each racer's claim is judged on its own stream: %+v", rep.Skipped)
	}
}
