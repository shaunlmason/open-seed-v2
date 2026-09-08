package maintain

// The reap rule's drills (plans/os-8a5f14bb.md D4). The rule is pure
// on purpose, so every case below is decided without a ledger: what a
// reap MEANS is a question about evidence, and evidence is the thing a
// test can hold in its hand.
//
// The conjunction is planted EACH HALF ALONE rather than only
// together. A test that only ever sees both halves cannot tell a
// conjunction from a coincidence, and the first draft of this rule
// named a second conjunct that did not exist at all (review finding on
// #203) — which is exactly the failure a both-halves-only drill would
// have waved through.

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func classified(state obs.State) obs.Classification {
	return obs.Classification{State: state, LastObservation: "2026-09-01T00:00:00Z",
		LastAdvance: "2026-09-01T00:00:00Z", Count: 3}
}

// conformance: acceptance criterion 2 — a no_data stream is NEVER
// reaped, however old the claim, and the refusal says why. This is
// where the instinct to reap is strongest and the evidence weakest: a
// stream holding nothing looks exactly like a worker that died before
// its first line AND exactly like a worker whose lossy channel dropped
// everything. Corroboration does not rescue it, so the case is drilled
// WITH the corroboration standing.
func TestNoDataIsNeverReaped(t *testing.T) {
	for _, corr := range []Corroboration{{}, {Interrupted: true}, {Wedged: true}, {Interrupted: true, Wedged: true}} {
		ok, because := Reapable(obs.Classification{State: obs.NoData}, corr)
		if ok {
			t.Fatalf("no_data must never reap, reaped with %+v", corr)
		}
		if !strings.Contains(because, "nothing at all") {
			t.Errorf("the refusal must name the absence of evidence, got %q", because)
		}
	}
}

// conformance: acceptance criterion 3 — the corroboration is a
// CONJUNCTION, and each half alone is planted.
func TestReapNeedsBothHalves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class obs.Classification
		corr  Corroboration
		want  bool
		says  string
	}{
		{"expired alone does not reap", classified(obs.Expired), Corroboration{}, false, "asked to stop"},
		{"wedged alone does not reap", classified(obs.Wedged), Corroboration{}, false, "asked to stop"},
		{"an unanswered interrupt on a LIVE stream does not reap",
			classified(obs.Live), Corroboration{Interrupted: true}, false, "live"},
		{"an unanswered wedge declaration on a live stream does not reap",
			classified(obs.Live), Corroboration{Wedged: true}, false, "live"},
		{"expired plus an unanswered interrupt reaps",
			classified(obs.Expired), Corroboration{Interrupted: true}, true, ""},
		{"wedged plus a wedge declaration reaps",
			classified(obs.Wedged), Corroboration{Wedged: true}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, because := Reapable(tc.class, tc.corr)
			if ok != tc.want {
				t.Fatalf("Reapable = %v, want %v (%q)", ok, tc.want, because)
			}
			if tc.says != "" && !strings.Contains(because, tc.says) {
				t.Errorf("the refusal must say %q, got %q", tc.says, because)
			}
		})
	}
}

// The refusal for an uncorroborated stale stream states the reason the
// rule exists, not merely that it refused: the channel is lossy by
// design, so silence is not a request nobody answered. A refusal that
// said only "not reaped" would leave an operator with no way to tell
// this from a bug.
func TestUncorroboratedRefusalNamesTheLossyChannel(t *testing.T) {
	_, because := Reapable(classified(obs.Expired), Corroboration{})
	for _, want := range []string{"asked to stop", "lossy"} {
		if !strings.Contains(because, want) {
			t.Errorf("the refusal must contain %q, got %q", want, because)
		}
	}
}

// conformance: acceptance criterion 1's packet half — the forced
// reap's packet is composed FROM WHAT IS KNOWN
// (spec/executors.md): acceptance from the contract's own spec,
// the zero-length base range because no pushed work is known, and
// findings recording the ignored request. Every part is asserted,
// because a packet that parses is not the same as a packet that says
// anything.
func TestReapPacketRecordsTheIgnoredRequest(t *testing.T) {
	s := transition.SubjectState{
		State:      "in_progress",
		Acceptance: &transition.AcceptanceInfo{Ref: "accept.md @ abc1234"},
	}
	body, err := ReapPacket(s, 7, classified(obs.Expired), Corroboration{Interrupted: true})
	if err != nil {
		t.Fatal(err)
	}
	// FromPayload, not Parse: claim.reaped is a claim-scoped event, so
	// the payload carries the fence citation the fence rule demands
	// AND the packet. Parsing only the inner packet would pass over a
	// payload the boundary refuses, which is how the first draft
	// shipped a reap that never landed.
	if fence := fenceOf(t, body); fence != "7" {
		t.Errorf("the reap cites the fence it closes: %q", fence)
	}
	p, err := packet.FromPayload("c-1", body)
	if err != nil {
		t.Fatalf("the reap's payload must satisfy the boundary that will admit it: %v", err)
	}
	if len(p.Acceptance) != 1 || p.Acceptance[0] != "accept.md @ abc1234" {
		t.Errorf("acceptance comes from the contract's specified criteria: %+v", p.Acceptance)
	}
	if p.Base != packet.ZeroRange {
		t.Errorf("no pushed work is known, so the range is zero-length: %q", p.Base)
	}
	if len(p.Findings) != 1 {
		t.Fatalf("the reap records exactly one finding: %+v", p.Findings)
	}
	if !strings.Contains(p.Findings[0].Tried, "run.interrupted") {
		t.Errorf("the finding must record the ignored interrupt: %q", p.Findings[0].Tried)
	}
	if !strings.Contains(p.Findings[0].Outcome, "expired") {
		t.Errorf("the finding must carry the classification that decided it: %q", p.Findings[0].Outcome)
	}
}

// A contract whose acceptance the fold does not carry gets the only
// honest packet available: one that says the anchor is unread. It must
// still parse, because a packet the boundary refuses leaves the window
// open — the reap would fail on exactly the subject that most needs it.
func TestReapPacketWithoutAcceptanceStillAdmits(t *testing.T) {
	body, err := ReapPacket(transition.SubjectState{State: "in_progress"}, 3,
		classified(obs.Wedged), Corroboration{Wedged: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := packet.FromPayload("c-1", body)
	if err != nil {
		t.Fatalf("an unknown acceptance must not make the packet inadmissible: %v", err)
	}
	if !strings.Contains(p.Acceptance[0], "does not carry") {
		t.Errorf("the packet says the anchor is unread rather than inventing one: %+v", p.Acceptance)
	}
	if !strings.Contains(p.Findings[0].Tried, "wedged") {
		t.Errorf("the finding names the corroboration that decided it: %q", p.Findings[0].Tried)
	}
}

// DefectID is stable across passes, which is what makes filing
// idempotent through the ledger's own duplicate refusal rather than
// through a memory the loop would have to keep.
func TestDefectIDIsStableAndDistinct(t *testing.T) {
	a := DefectID(reconcileFinding("c-1", "target_rewritten"))
	if a != DefectID(reconcileFinding("c-1", "target_rewritten")) {
		t.Error("the same finding must file under the same id, or every pass files a duplicate")
	}
	if a == DefectID(reconcileFinding("c-2", "target_rewritten")) ||
		a == DefectID(reconcileFinding("c-1", "unsettled")) {
		t.Error("a different subject or class is a different defect")
	}
	if !strings.HasPrefix(a, "d-") {
		t.Errorf("a filed defect is recognizable as one: %q", a)
	}
}

func reconcileFinding(subject, class string) reconcile.Finding {
	return reconcile.Finding{Subject: subject, Class: class, Detail: "drill"}
}

// fenceOf reads the citation out of a claim-scoped payload.
func fenceOf(t *testing.T, body []byte) string {
	t.Helper()
	var m struct {
		Fence string `json:"fence"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	return m.Fence
}

// The pass's effects are injected, so their FAILURES are drillable
// too — and how a maintenance loop behaves when an effect fails is
// exactly what an unattended loop's operator needs to be able to
// predict.
//
// The distinction the drills below pin: a refused ACT is reported and
// the pass continues, because a refusal is the boundary doing its job
// and the rest of the pass is still worth running. A broken EFFECT
// (an artifact store that will not write, a rebuild that will not
// build) stops the pass, because continuing would checkpoint a state
// that was never materialized.
func TestRefusedActsAreReportedAndThePassContinues(t *testing.T) {
	d := Deps{
		Now:  time.Now().UTC(),
		File: func(f reconcile.Finding) (string, error) { return "", errors.New("out of grant") },
	}
	rep, err := Run(d)
	if err != nil {
		t.Fatalf("a refused act is not a pass failure: %v", err)
	}
	if len(rep.Refusals) != 0 {
		t.Fatalf("with no findings there is nothing to file: %+v", rep.Refusals)
	}
	// Every part of the report is an explicit empty rather than null,
	// so a consumer can tell "nothing to do" from "did not run".
	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "null") {
		t.Errorf("a report with null parts cannot be told from one that never ran: %s", body)
	}
}

func TestABrokenEffectStopsThePass(t *testing.T) {
	boom := errors.New("the projection tree is unwritable")
	rep, err := Run(Deps{
		Now:     time.Now().UTC(),
		Rebuild: func() ([]string, error) { return nil, boom },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("a rebuild that cannot run stops the pass: %v", err)
	}
	if rep.Checkpoint != nil {
		t.Error("nothing is checkpointed after a rebuild that did not happen: the checkpoint attests to state the pass produced")
	}
}

// A checkpoint whose snapshot cannot be STORED never becomes a
// checkpoint: the snapshot is written first precisely so the event
// cannot name a location nothing can fetch.
func TestACheckpointWithoutAStoredSnapshotIsNotAppended(t *testing.T) {
	appended := 0
	_, err := Run(Deps{
		Now:         time.Now().UTC(),
		Store:       artifact.Open(filepath.Join(t.TempDir(), "missing", "\x00bad")),
		Append:      func(string, string, []byte) error { appended++; return nil },
		Materialize: func() ([]byte, int, error) { return []byte(`{"format":"x"}`), 1, nil },
	})
	if err == nil {
		t.Fatal("a snapshot that cannot be stored must stop the checkpoint")
	}
	if appended != 0 {
		t.Error("the event must not be appended before its snapshot is retrievable")
	}
}

// A REFUSED checkpoint is reported rather than raised: the boundary
// declining is ordinary, and the pass has already done its other work.
func TestARefusedCheckpointIsReported(t *testing.T) {
	rep, err := Run(Deps{
		Now:         time.Now().UTC(),
		Store:       artifact.Open(t.TempDir()),
		Append:      func(string, string, []byte) error { return errors.New("not granted") },
		Materialize: func() ([]byte, int, error) { return []byte(`{"format":"x"}`), 1, nil },
	})
	if err != nil {
		t.Fatalf("a refused checkpoint is not a pass failure: %v", err)
	}
	if rep.Checkpoint != nil {
		t.Error("a refused checkpoint is not reported as one that landed")
	}
	if len(rep.Refusals) != 1 || rep.Refusals[0].Verb != "system.checkpoint" {
		t.Fatalf("the refusal must be carried out: %+v", rep.Refusals)
	}
}

func TestAMaterializationFailureStopsThePass(t *testing.T) {
	boom := errors.New("cannot build the queue projection")
	if _, err := Run(Deps{
		Now:         time.Now().UTC(),
		Store:       artifact.Open(t.TempDir()),
		Append:      func(string, string, []byte) error { return nil },
		Materialize: func() ([]byte, int, error) { return nil, 0, boom },
	}); !errors.Is(err, boom) {
		t.Fatalf("a materialization that cannot run stops the pass: %v", err)
	}
}

// reapBecause names WHICH corroboration decided the reap, because a
// packet that said only "reaped" would leave the successor guessing
// between an ignored interrupt and a declared wedge.
func TestReapBecauseNamesTheCorroboration(t *testing.T) {
	if got := reapBecause(Corroboration{Interrupted: true}); !strings.Contains(got, "run.interrupted") {
		t.Errorf("an ignored interrupt is named: %q", got)
	}
	if got := reapBecause(Corroboration{Wedged: true}); !strings.Contains(got, "wedge.declared") {
		t.Errorf("a declared wedge is named: %q", got)
	}
}

// An unclassified stream state cannot reap. The switch is exhaustive
// over obs's classes today; the default exists so a class added later
// fails CLOSED rather than falling into the reap path by omission.
func TestAnUnknownClassificationDoesNotReap(t *testing.T) {
	ok, because := Reapable(obs.Classification{State: obs.State("invented")},
		Corroboration{Interrupted: true})
	if ok {
		t.Fatal("a class this rule does not know must not reap")
	}
	if !strings.Contains(because, "unclassified") {
		t.Errorf("the refusal names the unknown class: %q", because)
	}
}

// conformance: plans/os-32d06c65.md D5 — a revocation is a ledger fact,
// not a stream classification, so it reaps in EVERY state, no_data
// included: the one place no_data is reaped, and only because the chain
// corroborates it. reapBecause and the packet name the revocation.
func TestRevocationReapsInEveryState(t *testing.T) {
	for _, st := range []obs.State{obs.NoData, obs.Live, obs.Expired, obs.Wedged} {
		ok, because := Reapable(obs.Classification{State: st}, Corroboration{Revoked: true})
		if !ok {
			t.Fatalf("a revoked holder reaps in state %q, refused %q", st, because)
		}
	}
	if !(Corroboration{Revoked: true}).Stands() {
		t.Fatal("a revocation stands as corroboration")
	}
	if b := reapBecause(Corroboration{Revoked: true}); !strings.Contains(b, "revoked") {
		t.Errorf("reapBecause must name the revocation, got %q", b)
	}
	pkt, err := ReapPacket(transition.SubjectState{}, 3, obs.Classification{State: obs.NoData}, Corroboration{Revoked: true})
	if err != nil || !strings.Contains(string(pkt), "revoked") {
		t.Fatalf("the reap packet must name the revocation as the reason: %v %s", err, pkt)
	}
}

// The revoked-holder reap's packet names the revocation as the reason,
// cites the fence, and admits at the boundary even on a no_data stream —
// the one place no_data is reaped (plans/os-32d06c65.md D3, D5).
func TestReapPacketNamesTheRevocation(t *testing.T) {
	s := transition.SubjectState{State: "in_progress", Acceptance: &transition.AcceptanceInfo{Ref: "accept.md @ abc1234"}}
	body, err := ReapPacket(s, 5, classified(obs.NoData), Corroboration{Revoked: true})
	if err != nil {
		t.Fatal(err)
	}
	if fence := fenceOf(t, body); fence != "5" {
		t.Errorf("the revoked reap cites the fence it closes: %q", fence)
	}
	p, err := packet.FromPayload("c-1", body)
	if err != nil {
		t.Fatalf("the revoked reap's payload must admit at the boundary that closes the window: %v", err)
	}
	if !strings.Contains(p.Findings[0].Tried, "revoked") {
		t.Errorf("the finding names the revocation as the reason: %q", p.Findings[0].Tried)
	}
}
