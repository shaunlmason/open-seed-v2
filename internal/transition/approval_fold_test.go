package transition

// The per-verb approval's facts as the fold keeps them
// (plans/os-5781a026.md D3): a request, its answer, and the one act
// that spends a grant.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func approvalRec(v, verb, subject, actor, payload string) *event.Record {
	return &event.Record{Event: event.Event{V: v, TS: "2026-09-04T00:00:00Z", Verb: verb, Subject: subject, Actor: actor, Payload: []byte(payload)}}
}

// conformance: plans/os-5781a026.md D3 — the fold keeps the facts and
// spends the grants: a request is open once granted and until the
// first record on its subject naming its verb and actor, which spends
// it at that position; a second act needs a second request; an
// unanswered or denied request admits nothing.
func approvalTable(t *testing.T) *Table {
	t.Helper()
	table, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestApprovalGrantsAreSpentOnce(t *testing.T) {
	table := approvalTable(t)
	const worker, root = "fp-worker", "fp-root"
	req := `{"verb": "claim.taken", "actor": "` + worker + `", "reason": "the drill asks"}`
	recs := []*event.Record{
		approvalRec(version.Seed1, "intent.filed", "c-1", root, `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`),
		approvalRec(version.Seed1, "contract.specified", "c-1", root, `{"acceptance": {"ref": "specs/x.md @ abc1234", "executable": false}}`),
		approvalRec(version.Seed1, approval.RequestedVerb, "c-1", worker, req),
	}
	f := table.FoldRecords(recs)
	a, ok := f.ApprovalAt(2)
	if !ok || a.Verb != "claim.taken" || a.Actor != worker || a.Requester != worker || a.Answered != nil || a.Open() {
		t.Fatalf("an unanswered request is kept and open for nothing: %+v %v", a, ok)
	}
	if _, open := f.OpenApproval("c-1", "claim.taken", worker); open {
		t.Fatal("an unanswered request admits nothing")
	}
	if p := f.PendingApprovals("c-1"); len(p) != 1 || p[0].Pos != 2 {
		t.Fatalf("the request is pending: %+v", p)
	}
	// The grant opens it; the claim spends it at the claim's position.
	recs = append(recs,
		approvalRec(version.Seed1, approval.GrantedVerb, "c-1", root, `{"request": "2"}`),
		approvalRec(version.Seed1, "claim.taken", "c-1", worker, `{}`),
	)
	f = table.FoldRecords(recs[:4])
	if a, _ := f.ApprovalAt(2); !a.Open() || !a.Granted || a.Answerer != root || *a.Answered != 3 {
		t.Fatalf("a granted request is open and names its answerer: %+v", a)
	}
	if _, open := f.OpenApproval("c-1", "claim.taken", worker); !open {
		t.Fatal("a granted request stands open for the act it names")
	}
	if _, open := f.OpenApproval("c-1", "claim.taken", root); open {
		t.Fatal("the grant names one actor")
	}
	if _, open := f.OpenApproval("c-1", "claim.released", worker); open {
		t.Fatal("the grant names one verb")
	}
	f = table.FoldRecords(recs)
	a, _ = f.ApprovalAt(2)
	if a.ConsumedAt == nil || *a.ConsumedAt != 4 || a.Open() {
		t.Fatalf("the act spends the grant at its own position: %+v", a)
	}
	if _, open := f.OpenApproval("c-1", "claim.taken", worker); open {
		t.Fatal("one grant admits one act")
	}
	if len(f.PendingApprovals("c-1")) != 0 {
		t.Fatal("an answered request is not pending")
	}
	// A denial answers and admits nothing; a second request on the
	// same subject is the pending one after the first is answered.
	recs = append(recs,
		approvalRec(version.Seed1, approval.RequestedVerb, "c-1", worker, req),
		approvalRec(version.Seed1, approval.DeniedVerb, "c-1", root, `{"request": "5", "reason": "not now"}`),
		approvalRec(version.Seed1, approval.RequestedVerb, "system", worker, `{"verb": "system.checkpoint", "actor": "`+worker+`", "reason": "r"}`),
	)
	f = table.FoldRecords(recs)
	if a, _ := f.ApprovalAt(5); a.Answered == nil || a.Granted || a.Open() {
		t.Fatalf("a denied request is answered and never open: %+v", a)
	}
	if _, open := f.OpenApproval("c-1", "claim.taken", worker); open {
		t.Fatal("a denial admits nothing")
	}
	if p := f.PendingApprovals("system"); len(p) != 1 || p[0].Pos != 7 {
		t.Fatalf("a request on system is pending there: %+v", p)
	}
	if all := f.Approvals(); len(all) != 3 || f.ApprovalAnomalies != 0 {
		t.Fatalf("three facts, no anomalies: %d %d", len(all), f.ApprovalAnomalies)
	}
}

// The tolerant fold: a malformed request, an answer citing nothing
// open, an answer on another subject, a second answer and a
// pre-activation record are anomalies or inert, never facts.
func TestApprovalAnomaliesAreCountedNotApplied(t *testing.T) {
	table := approvalTable(t)
	recs := []*event.Record{
		approvalRec("seed/0", approval.RequestedVerb, "system", "fp-w", `{"verb": "claim.taken", "actor": "fp-w", "reason": "before activation"}`),
		approvalRec(version.Seed1, approval.RequestedVerb, "system", "fp-w", `{"verb": "claim.taken"}`),
		approvalRec(version.Seed1, approval.GrantedVerb, "system", "fp-r", `{"request": "0"}`),
		approvalRec(version.Seed1, approval.RequestedVerb, "system", "fp-w", `{"verb": "claim.taken", "actor": "fp-w", "reason": "r"}`),
		approvalRec(version.Seed1, approval.GrantedVerb, "c-9", "fp-r", `{"request": "3"}`),
		approvalRec(version.Seed1, approval.GrantedVerb, "system", "fp-r", `{"request": "3"}`),
		approvalRec(version.Seed1, approval.DeniedVerb, "system", "fp-r", `{"request": "3", "reason": "twice"}`),
	}
	f := table.FoldRecords(recs)
	if len(f.Approvals()) != 1 {
		t.Fatalf("one fact: %+v", f.Approvals())
	}
	if f.ApprovalAnomalies != 4 {
		t.Fatalf("the malformed request, the answer to nothing, the answer on another subject and the second answer are four anomalies, got %d", f.ApprovalAnomalies)
	}
	if a, _ := f.ApprovalAt(3); !a.Open() || *a.Answered != 5 {
		t.Fatalf("the well-formed grant stands: %+v", a)
	}
	// Consumption reads activated records only: a grandfathered act
	// spends nothing.
	if _, ok := f.ApprovalAt(0); ok {
		t.Fatal("a pre-activation request is inert")
	}
}

// conformance: plans/os-5781a026.md D3; review finding on #331: a
// request for a birth is on the contract the filing creates, which the
// chain does not know yet: the fold keeps it as a fact on that subject
// with no anomaly, the grant opens it there, and the intent.filed that
// bears the subject into the lifecycle spends it at its own position.
func TestApprovalGrantForABirthIsSpentByTheFiling(t *testing.T) {
	table := approvalTable(t)
	const worker, root = "fp-worker", "fp-root"
	recs := []*event.Record{
		approvalRec(version.Seed1, approval.RequestedVerb, "c-3", worker, `{"verb": "intent.filed", "actor": "`+worker+`", "reason": "a governed birth"}`),
		approvalRec(version.Seed1, approval.GrantedVerb, "c-3", root, `{"request": "0"}`),
	}
	f := table.FoldRecords(recs)
	if _, known := f.State("c-3"); known {
		t.Fatal("a request and a grant bear no contract into the lifecycle")
	}
	if _, open := f.OpenApproval("c-3", "intent.filed", worker); !open || f.ApprovalAnomalies != 0 {
		t.Fatalf("the grant stands open on the contract the filing creates: %+v %d", f.Approvals(), f.ApprovalAnomalies)
	}
	recs = append(recs, approvalRec(version.Seed1, "intent.filed", "c-3", worker, `{"intent": "x", "tier": "standard", "budget": "small", "routing": "core"}`))
	f = table.FoldRecords(recs)
	if _, known := f.State("c-3"); !known {
		t.Fatal("the filing bears the contract into the lifecycle")
	}
	if a, _ := f.ApprovalAt(0); a.ConsumedAt == nil || *a.ConsumedAt != 2 || a.Open() {
		t.Fatalf("the filing spends the grant at its own position: %+v", a)
	}
	if _, open := f.OpenApproval("c-3", "intent.filed", worker); open {
		t.Fatal("one grant admits one birth")
	}
}
