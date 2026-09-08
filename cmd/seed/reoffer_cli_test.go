package main

// The re-offer loop at the terminal (plans/os-29e2fef2.md AC2, AC3,
// AC4): over the forge stand, a red return is followed by a re-offer
// the prior configuration's worker lists and another does not, the
// consumed offer's tiers ride along while a raw-pushed foreign offer
// lends nothing, the offer's expiry is the record's own instant plus
// the ttl, a window with no declared tuple yields a re-offer unscoped
// by tuple, a second pass re-offers nothing, and the loop closes to
// done with no offer published by hand after the first; and `offer
// publish --resume` scopes by the same derivation, alone or beside
// --strongest and --tuple, refusing resume_empty by name.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// reofferStand is the forge stand with workerA granted a claim tuple,
// the supervisor's offer published, a foreign offer raw-pushed by
// workerB's key after it, and workerA's window opened with a run
// declaring the tuple and a submission naming pr/1.
type reofferStand struct {
	*forgeStand
	tuple string
}

func reofferLedger(t *testing.T) *reofferStand {
	t.Helper()
	m := forgeLedger(t)
	r := &reofferStand{forgeStand: m, tuple: drillTuple(nil)}
	rootAppend(t, m.ld, m.priv, "actor.granted", m.fps["workerA"], `{"capability": "claim", "tuple": `+r.tuple+`}`)
	if e, code := runEnv(t, "offer", "publish", "--ledger", m.ld, "--subject", "c-1", "--key", m.keys["supervisor"],
		"--expires", "2027-01-01T00:00:00Z", "--capability", "claim", "--tier", "trivial"); code != 0 {
		t.Fatalf("the supervisor's offer: %d %+v", code, e)
	}
	// A well-shaped offer by a key holding no supervise standing folds
	// and is inert everywhere (offers.md "Foreign offers are inert").
	rawAppendAt(t, m.ld, workerRawKey(23), version.Seed9, "offer.published", "c-1", `{"eligibility": {"tiers": ["huge"]}, "expires": "2027-01-01T00:00:00Z"}`)
	r.open(t)
	return r
}

// open claims c-1 by workerA, reserves, starts a run declaring the
// tuple, and submits naming pr/1.
func (r *reofferStand) open(t *testing.T) {
	t.Helper()
	fence, reservation := openWindow(t, r.ld, 22, r.keys["workerA"], "c-1")
	// A start raw-pushed by a key holding no run lane, declaring
	// another configuration, folds before the legitimate one and must
	// never be the one resumed (offers.md's laundering posture).
	rawAppendAt(t, r.ld, workerRawKey(23), version.Seed9, "run.started", "c-1",
		fmt.Sprintf(`{"fence": %q, "reservation": %q, "tuple": %s}`, fence, reservation, drillTuple(map[string]string{"model": "lineage/raw"})))
	// The run is the supervisor's act on the holder's window; the
	// declared tuple is judged against the holder's admissible set.
	if _, err := admitAppend(t, r.ld, workerRawKey(21), "run.started", "c-1",
		fmt.Sprintf(`{"fence": %q, "reservation": %q, "tuple": %s}`, fence, reservation, r.tuple)); err != nil {
		t.Fatalf("run.started declaring the tuple: %v", err)
	}
	if e, code := runEnv(t, "submission", "make", "--ledger", r.ld, "--key", r.keys["workerA"],
		"--subject", "c-1", "--packet", r.packet, "--pr", "pr/1"); code != 0 {
		t.Fatalf("submission make --pr: %d %+v", code, e)
	}
}

// lastOffer reads the latest offer.published on the chain: its ts and
// its payload.
func (r *reofferStand) lastOffer(t *testing.T) (ts string, payload map[string]any) {
	t.Helper()
	st := r.state(t)
	for i := len(st.records) - 1; i >= 0; i-- {
		rec := st.records[i]
		if rec.Event.Verb != "offer.published" {
			continue
		}
		if err := json.Unmarshal(rec.Event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		return rec.Event.TS, payload
	}
	t.Fatal("no offer on the chain")
	return "", nil
}

func tuplesOf(t *testing.T, row map[string]any) []tuple.Tuple {
	t.Helper()
	raw, _ := json.Marshal(row["tuples"])
	var out []tuple.Tuple
	if string(raw) != "null" {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// conformance: AC3, AC4 — one pass over a red snapshot returns the
// submission and re-offers it to the prior configuration with the
// consumed offer's scopes, expiring at the record's own ts plus the
// ttl; the prior worker lists it and another does not; a second pass
// re-offers nothing; the loop closes to done with no offer published
// by hand after the first.
func TestMaintainReoffersWhatItReturnedToThePriorConfiguration(t *testing.T) {
	r := reofferLedger(t)
	want := parseTuple(t, r.tuple)
	r.forge(t, "red", 1, "none")
	e, code := r.run(t, r.keys["maintenance"], "--forge", "snapshot", "--snapshot", r.snapshot, "--reoffer-ttl", "2h")
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Returned) != 1 || len(rep.Reoffered) != 1 || rep.Reoffered[0].Subject != "c-1" || rep.Reoffered[0].Holder != r.fps["workerA"] {
		t.Fatalf("the pass returns and re-offers c-1 to workerA: %+v %+v (refusals %+v)", rep.Returned, rep.Reoffered, rep.Refusals)
	}
	if rep.Reoffered[0].Tuple == nil || !rep.Reoffered[0].Tuple.Equal(want) {
		t.Fatalf("the re-offer is scoped to the declared tuple: %+v", rep.Reoffered[0])
	}
	ts, payload := r.lastOffer(t)
	at, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatal(err)
	}
	if payload["expires"] != at.Add(2*time.Hour).Format(time.RFC3339) || rep.Reoffered[0].Expires != payload["expires"] {
		t.Fatalf("expires is the record's own ts plus the ttl: ts %s, expires %v, reported %s", ts, payload["expires"], rep.Reoffered[0].Expires)
	}
	elig, _ := payload["eligibility"].(map[string]any)
	if fmt.Sprint(elig["capabilities"]) != "[claim]" || fmt.Sprint(elig["tiers"]) != "[trivial]" {
		t.Fatalf("the consumed offer's capabilities and tiers ride along, the foreign offer's never: %+v", elig)
	}
	if got := tuplesOf(t, elig); len(got) != 1 || !got[0].Equal(want) {
		t.Fatalf("the scope is the prior tuple alone: %+v", elig)
	}
	rows := listOffers(t, r.ld, r.fps["workerA"], "")
	if len(rows) != 1 {
		t.Fatalf("the prior worker's poll lists the re-offer: %+v", rows)
	}
	if got := tuplesOf(t, rows[0].(map[string]any)); len(got) != 1 || !got[0].Equal(want) {
		t.Fatalf("the row shows why: %+v", rows[0])
	}
	if got := listOffers(t, r.ld, r.fps["workerB"], ""); len(got) != 0 {
		t.Fatalf("a worker holding another configuration does not see it: %+v", got)
	}

	// A second pass over the same subject: the observation is
	// unchanged, nothing is returned, nothing re-offered.
	e, code = r.run(t, r.keys["maintenance"], "--forge", "snapshot", "--snapshot", r.snapshot)
	if code != 0 {
		t.Fatalf("second pass: %d %+v", code, e)
	}
	if rep = report(t, e); len(rep.Returned) != 0 || len(rep.Reoffered) != 0 {
		t.Fatalf("one re-offer per return: %+v %+v", rep.Returned, rep.Reoffered)
	}

	// The prior worker takes the re-offer, resubmits, the forge says
	// green, and the unchanged chain completes to done.
	r.open(t)
	r.forge(t, "green", 0, "approved")
	e, code = r.run(t, r.keys["maintenance"], "--forge", "snapshot", "--snapshot", r.snapshot)
	if code != 0 {
		t.Fatalf("green pass: %d %+v", code, e)
	}
	if rep = report(t, e); len(rep.Observed) != 1 || len(rep.Returned) != 0 || len(rep.Reoffered) != 0 {
		t.Fatalf("green returns and re-offers nothing: %+v %+v %+v", rep.Observed, rep.Returned, rep.Reoffered)
	}
	if e, code := runEnv(t, "verdict", "render", "--ledger", r.ld, "--subject", "c-1",
		"--repo", r.src, "--key", r.keys["verifier"], "--verdict", "pass"); code != 0 {
		t.Fatalf("verdict render: %d %+v", code, e)
	}
	if e, code := runEnv(t, "merge", "request", "--ledger", r.ld, "--key", r.keys["workerA"], "--subject", "c-1"); code != 0 {
		t.Fatalf("merge request: %d %+v", code, e)
	}
	if e, code := runEnv(t, "merge", "observe", "--ledger", r.ld, "--key", r.keys["observer"],
		"--subject", "c-1", "--merged", r.head, "--pr", "pr/1"); code != 0 {
		t.Fatalf("merge observe: %d %+v", code, e)
	}
	if s, _ := r.state(t).fold.State("c-1"); s.State != "done" {
		t.Fatalf("the loop ends at done, got %q", s.State)
	}
	// Exactly one offer was published by hand; every other one is the
	// pass's.
	hand := 0
	for _, rec := range r.state(t).records {
		if rec.Event.Verb == "offer.published" && rec.Event.Actor == r.fps["supervisor"] {
			hand++
		}
	}
	if hand != 1 {
		t.Fatalf("no offer published by hand after the first: %d", hand)
	}
}

// conformance: AC3 — a window that declared no tuple (the forge stand's
// own claim, no run.started, no offer) yields a re-offer unscoped by
// tuple, [claim] and the filed tier, that every worker lists; and a
// bad ttl is usage.
func TestMaintainReofferWithoutADeclaredTupleIsUnscoped(t *testing.T) {
	m := forgeLedger(t)
	m.submit(t)
	m.forge(t, "red", 0, "changes_requested")
	if _, code := m.run(t, m.keys["maintenance"], "--forge", "snapshot", "--snapshot", m.snapshot, "--reoffer-ttl", "0s"); code != 64 {
		t.Fatalf("a non-positive ttl is usage: %d", code)
	}
	e, code := m.run(t, m.keys["maintenance"], "--forge", "snapshot", "--snapshot", m.snapshot)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Returned) != 1 || len(rep.Reoffered) != 1 || rep.Reoffered[0].Tuple != nil || rep.Reoffered[0].Holder != "" {
		t.Fatalf("an undeclared window re-offers unscoped by tuple: %+v %+v (refusals %+v)", rep.Returned, rep.Reoffered, rep.Refusals)
	}
	st := m.state(t)
	last := st.records[len(st.records)-1]
	if last.Event.Verb != "offer.published" || !strings.Contains(string(last.Event.Payload), `"capabilities":["claim"]`) || !strings.Contains(string(last.Event.Payload), `"tiers":["trivial"]`) || strings.Contains(string(last.Event.Payload), "tuples") {
		t.Fatalf("[claim] and the filed tier, no tuple scope: %s %s", last.Event.Verb, last.Event.Payload)
	}
	for _, w := range []string{"workerA", "workerB"} {
		if rows := listOffers(t, m.ld, m.fps[w], ""); len(rows) != 1 {
			t.Fatalf("%s lists the unscoped re-offer: %+v", w, rows)
		}
	}
}

// conformance: AC2 — offer publish --resume scopes by the resumption,
// alone or beside --strongest and --tuple (the prior tuple first,
// duplicates folded), the prior worker lists it and another does not,
// and resume_empty names the missing link and appends nothing: no
// return, a disqualified tuple, a verdict return.
func TestOfferPublishResumeScopesToThePriorSubmitter(t *testing.T) {
	r := reofferLedger(t)
	want := parseTuple(t, r.tuple)
	publish := func(extra ...string) (ledgerEnv, int) {
		t.Helper()
		args := append([]string{"offer", "publish", "--ledger", r.ld, "--subject", "c-1",
			"--key", r.keys["supervisor"], "--expires", "2027-01-01T00:00:00Z", "--capability", "claim"}, extra...)
		return runEnv(t, args...)
	}
	// Under review, nothing was returned: resume_empty, by name.
	count := r.state(t).count
	if e, code := publish("--resume"); code != 4 || e.Error == nil || e.Error.Code != "resume_empty" || !strings.Contains(e.Error.Message, "carries no return") {
		t.Fatalf("no return, nothing to resume: %d %+v", code, e)
	}
	if r.state(t).count != count {
		t.Fatal("a refused --resume appends nothing")
	}

	// The red return, through the pass.
	r.forge(t, "red", 1, "none")
	if e, code := r.run(t, r.keys["maintenance"], "--forge", "snapshot", "--snapshot", r.snapshot); code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	e, code := publish("--resume")
	if code != 0 {
		t.Fatalf("publish --resume: %d %+v", code, e)
	}
	res, _ := e.Result["resumption"].(map[string]any)
	if res["holder"] != r.fps["workerA"] || res["return"] == nil {
		t.Fatalf("the result names the resumption: %+v", e.Result)
	}
	_, payload := r.lastOffer(t)
	elig, _ := payload["eligibility"].(map[string]any)
	if got := tuplesOf(t, elig); len(got) != 1 || !got[0].Equal(want) {
		t.Fatalf("the scope is the prior tuple: %+v", elig)
	}
	if rows := listOffers(t, r.ld, r.fps["workerA"], ""); len(rows) != 2 {
		t.Fatalf("the prior worker lists the pass's re-offer and the supervisor's: %+v", rows)
	}
	if rows := listOffers(t, r.ld, r.fps["workerB"], ""); len(rows) != 0 {
		t.Fatalf("another configuration sees neither: %+v", rows)
	}

	// Beside --strongest: workerB's configuration ranks, and the scope
	// carries the prior tuple first and the ranked one after.
	other := drillTuple(map[string]string{"model": "lineage/2"})
	rootAppend(t, r.ld, r.priv, "actor.granted", r.fps["workerB"], `{"capability": "claim", "tuple": `+other+`}`)
	rootAppend(t, r.ld, r.priv, "actor.qualified", r.fps["workerB"], `{"capability": "claim", "tuple": `+other+`, "contract": "e-1", "verdict": "3"}`)
	if e, code := publish("--resume", "--strongest", "1"); code != 0 {
		t.Fatalf("publish --resume --strongest: %d %+v", code, e)
	}
	_, payload = r.lastOffer(t)
	elig, _ = payload["eligibility"].(map[string]any)
	if got := tuplesOf(t, elig); len(got) != 2 || !got[0].Equal(want) || !got[1].Equal(parseTuple(t, other)) {
		t.Fatalf("the prior tuple leads, the ranked one follows: %+v", elig)
	}
	// Beside --tuple: a hand-written member after the prior one, and
	// the prior one named by hand folds into one.
	third := drillTuple(map[string]string{"model": "lineage/3"})
	if e, code := publish("--resume", "--tuple", third, "--tuple", r.tuple); code != 0 {
		t.Fatalf("publish --resume --tuple: %d %+v", code, e)
	}
	_, payload = r.lastOffer(t)
	elig, _ = payload["eligibility"].(map[string]any)
	if got := tuplesOf(t, elig); len(got) != 2 || !got[0].Equal(want) || !got[1].Equal(parseTuple(t, third)) {
		t.Fatalf("the prior tuple once, then the hand-written one: %+v", elig)
	}

	// The prior tuple disqualified while workerA stays active: the
	// preference would be an offer its worker cannot see, so it
	// refuses by name.
	rootAppend(t, r.ld, r.priv, "actor.qualified", r.fps["workerA"], `{"capability": "claim", "tuple": `+r.tuple+`, "contract": "e-1", "verdict": "3"}`)
	rootAppend(t, r.ld, r.priv, "actor.disqualified", r.fps["workerA"], `{"capability": "claim", "tuple": `+r.tuple+`, "contract": "e-1", "verdict": "3", "reason": "the eval failed"}`)
	if e, code := publish("--resume"); code != 4 || e.Error == nil || e.Error.Code != "resume_empty" || !strings.Contains(e.Error.Message, "does not cite it") {
		t.Fatalf("a disqualified tuple is not preferred: %d %+v", code, e)
	}
	if rows := listOffers(t, r.ld, r.fps["workerA"], ""); len(rows) != 0 {
		t.Fatalf("the disqualified worker lists none of the scoped offers, which is why: %+v", rows)
	}
}

// conformance: AC1 at the terminal — a return by verdict routes by the
// ranking, never back to the configuration that failed.
func TestOfferPublishResumeRefusesAVerdictReturn(t *testing.T) {
	r := reofferLedger(t)
	if e, code := runEnv(t, "verdict", "render", "--ledger", r.ld, "--subject", "c-1",
		"--repo", r.src, "--key", r.keys["verifier"], "--verdict", "fail"); code != 0 {
		t.Fatalf("verdict render fail: %d %+v", code, e)
	}
	s, _ := r.state(t).fold.State("c-1")
	if s.Verdict == nil {
		t.Fatal("the fail verdict stands")
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", r.ld, "--key", r.keys["dispatcher"],
		"--verb", "contract.returned", "--subject", "c-1", "--payload", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos)); code != 0 {
		t.Fatalf("contract.returned by verdict: %d %+v", code, e)
	}
	e, code := runEnv(t, "offer", "publish", "--ledger", r.ld, "--subject", "c-1", "--key", r.keys["supervisor"],
		"--expires", "2027-01-01T00:00:00Z", "--capability", "claim", "--resume")
	if code != 4 || e.Error == nil || e.Error.Code != "resume_empty" || !strings.Contains(e.Error.Message, "cited a verdict") {
		t.Fatalf("a verdict return resumes nothing: %d %+v", code, e)
	}
}

func parseTuple(t *testing.T, raw string) tuple.Tuple {
	t.Helper()
	tu, err := tuple.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return tu
}
