package obligation

// The approval.pending row (plans/os-5781a026.md D5): an unanswered
// per-verb approval request is owed by the operator lane, one row per
// subject carrying the oldest open request's position and timestamp,
// discharged by either answer, gone once answered.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func approvalRec(verb, subject, actor, ts, payload string) *event.Record {
	return &event.Record{Event: event.Event{V: version.Seed1, TS: ts, Verb: verb, Subject: subject, Actor: actor, Payload: []byte(payload)}}
}

func TestApprovalPendingIsOwedByTheOperatorUntilAnswered(t *testing.T) {
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	req := `{"verb": "claim.taken", "actor": "fp-w", "reason": "r"}`
	recs := []*event.Record{
		approvalRec(approval.RequestedVerb, "system", "fp-w", "2026-09-04T00:00:01Z", req),
		approvalRec(approval.RequestedVerb, "system", "fp-w", "2026-09-04T00:00:02Z", req),
		approvalRec(approval.RequestedVerb, "c-1", "fp-w", "2026-09-04T00:00:03Z", req),
	}
	rows := Derive(recs, table, Deps{})
	var pending []Row
	for _, r := range rows {
		if r.Kind == KindApprovalPending {
			pending = append(pending, r)
		}
	}
	if len(pending) != 2 {
		t.Fatalf("one row per subject: %+v", rows)
	}
	for _, r := range pending {
		if r.OwedBy != LaneOperator || len(r.DischargedBy) != 2 || r.DischargedBy[0] != approval.DeniedVerb || r.DischargedBy[1] != approval.GrantedVerb {
			t.Fatalf("owed by the operator lane, discharged by either answer: %+v", r)
		}
	}
	// c-1 sorts first; system's row carries the OLDEST request.
	if pending[0].Subject != "c-1" || pending[0].Since != 2 || pending[0].TS != "2026-09-04T00:00:03Z" {
		t.Fatalf("the contract's row: %+v", pending[0])
	}
	if pending[1].Subject != "system" || pending[1].Since != 0 || pending[1].TS != "2026-09-04T00:00:01Z" {
		t.Fatalf("the oldest open request on system: %+v", pending[1])
	}
	// Answering the oldest moves the row to the next; answering both
	// clears it; a grant spent by an act owes nothing further.
	recs = append(recs, approvalRec(approval.GrantedVerb, "system", "fp-r", "2026-09-04T00:00:04Z", `{"request": "0"}`))
	rows = Derive(recs, table, Deps{})
	found := false
	for _, r := range rows {
		if r.Kind == KindApprovalPending && r.Subject == "system" {
			found = true
			if r.Since != 1 {
				t.Fatalf("the next open request: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("the second request still owes")
	}
	recs = append(recs, approvalRec(approval.DeniedVerb, "system", "fp-r", "2026-09-04T00:00:05Z", `{"request": "1", "reason": "no"}`))
	for _, r := range Derive(recs, table, Deps{}) {
		if r.Kind == KindApprovalPending && r.Subject == "system" {
			t.Fatalf("an answered request owes nothing: %+v", r)
		}
	}
}
