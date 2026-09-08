package maintain

// The re-offer's drills (plans/os-29e2fef2.md D3, AC3, AC5), pure on
// purpose: the scope the pass renders from a returned subject's state
// and its resumption, the one instant the payload and the record
// share, and the step's own posture (only what this pass returned,
// refusals reported, never a re-offer of someone else's return).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func drillTuple(model string) tuple.Tuple {
	return tuple.Tuple{Principal: "acme", Harness: "local-worktree/v0", Model: model, ToolPolicy: "default", Environment: "detached-git-worktree"}
}

type offerPayload struct {
	Eligibility struct {
		Capabilities []string      `json:"capabilities"`
		Tiers        []string      `json:"tiers"`
		Tuples       []tuple.Tuple `json:"tuples"`
	} `json:"eligibility"`
	Expires string `json:"expires"`
}

func decodeOffer(t *testing.T, payload []byte) offerPayload {
	t.Helper()
	var p offerPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("the re-offer must be the offers.md payload: %v", err)
	}
	return p
}

// conformance: AC3, AC5 — the scope: the consumed offer's capabilities
// and tiers, or [claim] and the filed tier; the prior tuple alone
// where it derived, the consumed offer's own tuple scope otherwise
// (never wider than what the claim consumed); expires at the instant
// plus the ttl exactly.
func TestReofferRendersTheScope(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	prior := drillTuple("lineage/1")
	other := drillTuple("lineage/2")
	ready := transition.SubjectState{State: "ready", Tier: "standard"}
	consumed := &transition.OfferFact{Pos: 4, Capabilities: []string{"claim"}, Tiers: []string{"standard", "trivial"}}
	scopedConsumed := &transition.OfferFact{Pos: 4, Capabilities: []string{"claim"}, Tuples: []tuple.Tuple{prior, other}}
	for name, tc := range map[string]struct {
		r      ranking.Resumption
		caps   []string
		tiers  []string
		tuples []tuple.Tuple
		holder string
	}{
		"no offer stood, no tuple derived":                                     {ranking.Resumption{}, []string{"claim"}, []string{"standard"}, nil, ""},
		"the consumed offer's capabilities and tiers ride along":               {ranking.Resumption{Offer: consumed}, []string{"claim"}, []string{"standard", "trivial"}, nil, ""},
		"the prior tuple scopes the re-offer":                                  {ranking.Resumption{Offer: consumed, Tuple: &prior, Holder: "aa"}, []string{"claim"}, []string{"standard", "trivial"}, []tuple.Tuple{prior}, "aa"},
		"the prior tuple narrows a tuple-scoped consumed offer":                {ranking.Resumption{Offer: scopedConsumed, Tuple: &prior, Holder: "aa"}, []string{"claim"}, nil, []tuple.Tuple{prior}, "aa"},
		"no tuple derived keeps the consumed offer's tuple scope, never wider": {ranking.Resumption{Offer: scopedConsumed}, []string{"claim"}, nil, []tuple.Tuple{prior, other}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			payload, row, err := Reoffer("c-1", ready, tc.r, 24*time.Hour, at)
			if err != nil {
				t.Fatal(err)
			}
			p := decodeOffer(t, payload)
			if strings.Join(p.Eligibility.Capabilities, ",") != strings.Join(tc.caps, ",") || strings.Join(p.Eligibility.Tiers, ",") != strings.Join(tc.tiers, ",") {
				t.Fatalf("capabilities %v tiers %v, want %v %v", p.Eligibility.Capabilities, p.Eligibility.Tiers, tc.caps, tc.tiers)
			}
			if len(p.Eligibility.Tuples) != len(tc.tuples) {
				t.Fatalf("tuples %v, want %v", p.Eligibility.Tuples, tc.tuples)
			}
			for i := range tc.tuples {
				if !p.Eligibility.Tuples[i].Equal(tc.tuples[i]) {
					t.Fatalf("tuple %d is %v, want %v", i, p.Eligibility.Tuples[i], tc.tuples[i])
				}
			}
			if p.Expires != "2026-09-04T12:00:00Z" || row.Expires != p.Expires {
				t.Fatalf("expires is the instant plus the ttl: %s / %s", p.Expires, row.Expires)
			}
			if row.Subject != "c-1" || row.Holder != tc.holder || (tc.tuples != nil && tc.r.Tuple != nil) != (row.Tuple != nil) {
				t.Fatalf("the row names the subject, the holder and the tuple: %+v", row)
			}
		})
	}
}

// conformance: AC5 — a non-positive ttl and a subject that is not
// ready refuse rather than render an offer admission would refuse or
// a re-offer on nothing.
func TestReofferRefuses(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	if _, _, err := Reoffer("c-1", transition.SubjectState{State: "ready"}, ranking.Resumption{}, 0, at); err == nil || !strings.Contains(err.Error(), "positive ttl") {
		t.Fatalf("a zero ttl is a born-dead offer: %v", err)
	}
	if _, _, err := Reoffer("c-1", transition.SubjectState{State: "review"}, ranking.Resumption{}, time.Hour, at); err == nil || !strings.Contains(err.Error(), "ready") {
		t.Fatalf("a re-offer invites claims on a ready subject: %v", err)
	}
}

// reofferChain is a seed/8 chain with one contract returned on the
// forge's word, folded by the table, the shape the step reads after
// the return step appended the return.
func reofferChain(t *testing.T) ([]*event.Record, *transition.Fold) {
	t.Helper()
	rec := func(verb, subject, actor, payload string) *event.Record {
		return &event.Record{Event: event.Event{V: version.Seed8, TS: "2026-09-03T00:00:00Z", Actor: actor, Verb: verb, Subject: subject, Payload: json.RawMessage(payload)}}
	}
	head := strings.Repeat("b", 40)
	packet := `{"acceptance": ["c-1"], "decisions": [], "base": "` + strings.Repeat("a", 40) + ".." + head + `", "refs": [], "findings": []}`
	records := []*event.Record{
		rec("intent.filed", "c-1", "root", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`),
		rec("contract.specified", "c-1", "root", `{"acceptance": {"ref": "spec.md @ 0123456789abcdef", "executable": false}}`),
		rec("claim.taken", "c-1", "alice", `{}`),
		rec("submission.made", "c-1", "alice", `{"fence": "2", "packet": `+packet+`, "pr": "pr/1"}`),
		rec(transition.CheckObservedVerb, "c-1", "observer", `{"pr": "pr/1", "head": "`+head+`", "checks": "red", "review": "none"}`),
		rec(transition.ContractReturnedVerb, "c-1", "dispatcher", `{"observation": "4"}`),
	}
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	return records, table.FoldRecords(records)
}

// conformance: AC3, AC5 — the step re-offers what this pass returned
// and nothing else: the instant handed to AppendAt is the one the
// payload's expires was computed from; a refusal is reported, never
// retried; a subject the pass did not return is not re-offered; a
// return that is not the latest on the chain is skipped with the
// reason.
func TestReofferStepUsesOneInstantAndReportsRefusals(t *testing.T) {
	records, fold := reofferChain(t)
	at := time.Date(2026, 9, 3, 6, 30, 15, 999, time.UTC)
	var got struct {
		at      time.Time
		verb    string
		payload []byte
	}
	base := Deps{Records: records, Fold: fold, Obs: &obs.Snapshot{}, Now: at,
		Instant:    func() time.Time { return at },
		ReofferTTL: 90 * time.Minute,
		AppendAt: func(when time.Time, verb, subject string, payload []byte) error {
			got.at, got.verb, got.payload = when, verb, payload
			return nil
		},
	}
	// The step reads the report's returned rows: plant the return the
	// pass "made" on this chain.
	rep := Report{Returned: []Returned{{Subject: "c-1", Observation: 4}}, Reoffered: []Reoffered{}, Skipped: []Skip{}, Refusals: []Refusal{}}
	if err := base.reoffer(&rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Reoffered) != 1 || rep.Reoffered[0].Subject != "c-1" || rep.Reoffered[0].Tuple != nil || got.verb != transition.OfferPublishedVerb {
		t.Fatalf("one re-offer, unscoped by tuple (the window declared none): %+v %+v", rep.Reoffered, rep.Refusals)
	}
	p := decodeOffer(t, got.payload)
	if !got.at.Equal(at.Truncate(time.Second)) || p.Expires != got.at.Add(90*time.Minute).Format(time.RFC3339) {
		t.Fatalf("the record's instant and the payload's expires derive from one reading: %s vs %s", got.at, p.Expires)
	}
	if strings.Join(p.Eligibility.Capabilities, ",") != "claim" || strings.Join(p.Eligibility.Tiers, ",") != "trivial" {
		t.Fatalf("no offer stood, so [claim] and the filed tier: %+v", p.Eligibility)
	}

	// A refusal is reported, never retried.
	refused := base
	calls := 0
	refused.AppendAt = func(time.Time, string, string, []byte) error { calls++; return errors.New("out_of_grant: drill") }
	rep = Report{Returned: []Returned{{Subject: "c-1", Observation: 4}}, Reoffered: []Reoffered{}, Skipped: []Skip{}, Refusals: []Refusal{}}
	if err := refused.reoffer(&rep); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(rep.Reoffered) != 0 || len(rep.Refusals) != 1 || rep.Refusals[0].Verb != transition.OfferPublishedVerb || !strings.Contains(rep.Refusals[0].Reason, "out_of_grant") {
		t.Fatalf("the refusal is reported once: %d %+v %+v", calls, rep.Reoffered, rep.Refusals)
	}

	// Nothing returned, nothing re-offered, however ready the subject.
	quiet := base
	quiet.AppendAt = func(time.Time, string, string, []byte) error {
		t.Fatal("a subject the pass did not return is not re-offered")
		return nil
	}
	rep = Report{Returned: []Returned{}, Reoffered: []Reoffered{}, Skipped: []Skip{}, Refusals: []Refusal{}}
	if err := quiet.reoffer(&rep); err != nil || len(rep.Reoffered) != 0 {
		t.Fatalf("no return, no re-offer: %v %+v", err, rep.Reoffered)
	}

	// A return that is not the chain's latest is someone else's.
	rep = Report{Returned: []Returned{{Subject: "c-1", Observation: 1}}, Reoffered: []Reoffered{}, Skipped: []Skip{}, Refusals: []Refusal{}}
	if err := quiet.reoffer(&rep); err != nil || len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Because, "does not cite the observation at position 1") {
		t.Fatalf("a return this pass did not make is skipped by name: %v %+v", err, rep.Skipped)
	}

	// The refreshed view is the one read (the return landed after the
	// opening fold): with an opening fold that has no return and a
	// refresh that does, the re-offer still derives.
	stale := base
	stale.Records, stale.Fold = records[:4], func() *transition.Fold { tb, _ := transition.Default(); return tb.FoldRecords(records[:4]) }()
	stale.Refresh = func() ([]*event.Record, *transition.Fold, []obligation.Row, error) { return records, fold, nil, nil }
	rep = Report{Returned: []Returned{{Subject: "c-1", Observation: 4}}, Reoffered: []Reoffered{}, Skipped: []Skip{}, Refusals: []Refusal{}}
	if err := stale.reoffer(&rep); err != nil || len(rep.Reoffered) != 1 {
		t.Fatalf("the step reads the refreshed view: %v %+v %+v", err, rep.Reoffered, rep.Skipped)
	}
}
