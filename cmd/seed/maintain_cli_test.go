package main

// The maintenance loop end to end (plans/os-8a5f14bb.md;
// spec/maintenance.md). Every drill here runs ONE pass against a
// real ledger with no scheduler and no wake channel, which is what
// "runnable unattended" has to mean if it is to be provable: a loop
// that needs a scheduler to be testable is one nobody can show runs
// unattended.
//
// The properties are read back OUT OF THE CHAIN rather than out of the
// verb's own report wherever the chain can carry them. A pass that
// says it reaped is not the same as a reap.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/maintain"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
)

// maintenanceStand is the pass's fixture: a real ledger with a
// maintenance-granted actor, one claimed contract, and an observation
// stream under the claim's own fence. asOf is fixed so the
// classification is decided by the fixture rather than by how long the
// test took to run.
type maintenanceStand struct {
	ld, src, obsDir, out, artifacts string
	priv                            string
	keys, fps                       map[string]string
	fence                           int
	asOf                            string
}

func maintenanceLedger(t *testing.T) *maintenanceStand {
	t.Helper()
	ld, src, base, specCommit, _, priv, _, keys, fps := offerLedger(t)
	_ = base
	offerFile(t, ld, priv, specCommit, "c-1")

	// The maintenance actor: enrolled and granted like anyone else,
	// which is the whole posture — no lane here has a private door.
	mPath, mPub, mFP := writeWorkerKey(t, 31)
	keys["maintenance"], fps["maintenance"] = mPath, mFP
	for _, step := range [][]string{
		{"actor.enrolled", mFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "maintenance"}`, mPub)},
		{"actor.granted", mFP, `{"capability": "maintenance"}`},
		{"actor.granted", mFP, `{"capability": "operator"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	fence, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	t.Cleanup(func() {
		// Published projection trees are locked (0555 directories), so
		// unlock before testing's own TempDir cleanup or RemoveAll
		// fails on an unprivileged runner. Registered AFTER the
		// TempDir above so it runs BEFORE that removal: cleanups are
		// LIFO.
		//
		// This is the established shape (cmd/seed/project_cli_test.go
		// carries it with the same comment), and omitting it is
		// invisible to anyone running the suite as root — which is why
		// this card's own criterion asks for an unprivileged run.
		_ = filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	return &maintenanceStand{
		ld: ld, src: src, obsDir: filepath.Join(dir, "obs"),
		out: out, artifacts: filepath.Join(dir, "artifacts"),
		priv: priv, keys: keys, fps: fps, fence: fence,
		asOf: "2026-09-01T12:00:00Z",
	}
}

// observe writes one line onto the claim's own per-fence stream at a
// declared instant.
func (m *maintenanceStand) observe(t *testing.T, ts string, count int) {
	t.Helper()
	if err := obs.Append(m.obsDir, m.fps["workerA"], obs.FormatFence(m.fence),
		obs.Line{TS: ts, Subject: "c-1", Step: "drill", Count: count}); err != nil {
		t.Fatal(err)
	}
}

// stale writes an observation old enough that the fixed as-of
// classifies the stream expired.
func (m *maintenanceStand) stale(t *testing.T) {
	t.Helper()
	m.observe(t, "2026-09-01T11:00:00Z", 1)
}

// interrupt appends the supervisor's ADMITTED run.interrupted on the
// active fence: the "someone asked" half of the reap's corroboration.
func (m *maintenanceStand) interrupt(t *testing.T) {
	t.Helper()
	if _, err := admitAppend(t, m.ld, workerRawKey(21), "run.interrupted", "c-1",
		fmt.Sprintf(`{"fence": "%d"}`, m.fence)); err != nil {
		t.Fatalf("the interrupt must admit: %v", err)
	}
}

func (m *maintenanceStand) run(t *testing.T, extra ...string) (ledgerEnv, int) {
	t.Helper()
	args := append([]string{"maintain", "run", "--ledger", m.ld, "--repo", m.src,
		"--key", m.keys["maintenance"], "--obs", m.obsDir, "--artifacts", m.artifacts,
		"--as-of", m.asOf}, extra...)
	return runEnv(t, args...)
}

func report(t *testing.T, e ledgerEnv) maintain.Report {
	t.Helper()
	body, err := json.Marshal(e.Result)
	if err != nil {
		t.Fatal(err)
	}
	var rep maintain.Report
	if err := json.Unmarshal(body, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

// conformance: acceptance criterion 1 — one pass reaps a claim that is
// BOTH classified expired and carrying an unanswered admitted
// run.interrupted, and leaves a claim.reaped packet whose findings
// record the ignored interrupt (spec/executors.md's force path).
// Asserted by folding the chain back, not by reading the report.
func TestMaintainReapsAnUnansweredInterrupt(t *testing.T) {
	m := maintenanceLedger(t)
	m.stale(t)
	m.interrupt(t)

	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Reaped) != 1 || rep.Reaped[0].Subject != "c-1" {
		t.Fatalf("the pass must reap the corroborated claim: %+v (skipped %+v)", rep.Reaped, rep.Skipped)
	}
	if rep.Reaped[0].Fence != m.fence {
		t.Errorf("the reap names the fence it closed: %d, want %d", rep.Reaped[0].Fence, m.fence)
	}

	// The chain's own account. A reap that only the report knows about
	// is a report, not a reap.
	st, failEnv := loadVerdictState(m.ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify after the pass: %+v", failEnv)
	}
	s, _ := st.fold.State("c-1")
	if s.State != "ready" || s.Claim != nil {
		t.Fatalf("a reap returns the subject to ready with no standing claim: %+v", s)
	}
	var reaped *packet.Packet
	for _, rec := range st.records {
		if rec.Event.Verb == "claim.reaped" && rec.Event.Subject == "c-1" {
			p, err := packet.FromPayload("c-1", rec.Event.Payload)
			if err != nil {
				t.Fatalf("the reap's packet must parse: %v", err)
			}
			reaped = p
		}
	}
	if reaped == nil {
		t.Fatal("the chain must carry the claim.reaped the report claims")
	}
	if len(reaped.Findings) != 1 || !strings.Contains(reaped.Findings[0].Tried, "run.interrupted") {
		t.Errorf("the packet's findings record the ignored interrupt: %+v", reaped.Findings)
	}
	if reaped.Base != packet.ZeroRange {
		t.Errorf("no pushed work is known, so the resume coordinate is zero-length: %q", reaped.Base)
	}
}

// conformance: acceptance criterion 2 — a no_data stream is never
// reaped, HOWEVER OLD the claim, and the refusal says why. Drilled
// through the real pass, not only the pure rule, because a wiring that
// classified an empty stream as expired would satisfy the unit drill
// and reap live work here.
func TestMaintainNeverReapsAnEmptyStream(t *testing.T) {
	m := maintenanceLedger(t)
	m.interrupt(t) // corroboration stands; the evidence still does not
	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Reaped) != 0 {
		t.Fatalf("an empty stream carries no reap path at all: %+v", rep.Reaped)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].State != string(obs.NoData) {
		t.Fatalf("the skip must be reported with its classification: %+v", rep.Skipped)
	}
	if !strings.Contains(rep.Skipped[0].Because, "nothing at all") {
		t.Errorf("the refusal says why: %q", rep.Skipped[0].Because)
	}
}

// conformance: acceptance criterion 3 — each half alone, against the
// real boundary. The raw case is the one that matters most: a
// run.interrupted an unprivileged key pushed straight onto the ref
// folds as a fact and is visible to any naive scan, and it must
// corroborate NOTHING, because admit.InterruptRequested counts only
// interrupts that passed the boundary at their own position.
func TestMaintainCorroborationHalvesAlone(t *testing.T) {
	t.Run("expired with no request does not reap", func(t *testing.T) {
		m := maintenanceLedger(t)
		m.stale(t)
		e, code := m.run(t)
		if code != 0 {
			t.Fatalf("maintain run: %d %+v", code, e)
		}
		rep := report(t, e)
		if len(rep.Reaped) != 0 {
			t.Fatalf("silence alone must not reap: %+v", rep.Reaped)
		}
		if !strings.Contains(rep.Skipped[0].Because, "asked to stop") {
			t.Errorf("the refusal names the missing request: %q", rep.Skipped[0].Because)
		}
	})

	t.Run("a RAW interrupt corroborates nothing", func(t *testing.T) {
		m := maintenanceLedger(t)
		m.stale(t)
		// workerB holds claim, not supervise: the interrupt rule would
		// refuse it at the door, so it is pushed raw.
		rawAppend(t, m.ld, workerRawKey(23), "run.interrupted", "c-1",
			fmt.Sprintf(`{"fence": "%d"}`, m.fence))
		e, code := m.run(t)
		if code != 0 {
			t.Fatalf("maintain run: %d %+v", code, e)
		}
		rep := report(t, e)
		if len(rep.Reaped) != 0 {
			t.Fatalf("a raw interrupt is not a request the boundary carried: %+v", rep.Reaped)
		}
		if !strings.Contains(rep.Skipped[0].Because, "asked to stop") {
			t.Errorf("the refusal is the uncorroborated one: %q", rep.Skipped[0].Because)
		}
	})
}

// conformance: acceptance criterion 6 — the loop holds NO PRIVATE
// POWERS. Run with a maintenance-only key and the acts needing
// operator refuse at the boundary, reported rather than worked around.
// This is the difference between "audited as an ordinary actor" as a
// property and as a sentence in a summary.
func TestMaintainHoldsNoPrivatePowers(t *testing.T) {
	m := maintenanceLedger(t)
	m.stale(t)
	m.interrupt(t)
	// A second maintenance actor, granted maintenance and NOTHING else.
	path, pub, fp := writeWorkerKey(t, 32)
	for _, step := range [][]string{
		{"actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "maint-only"}`, pub)},
		{"actor.granted", fp, `{"capability": "maintenance"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", m.ld, "--key", m.priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	m.keys["maintenance"] = path
	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("a refused act is reported, not an error: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Reaped) != 0 {
		t.Fatalf("claim.reaped needs a grant this key does not hold: %+v", rep.Reaped)
	}
	if len(rep.Refusals) == 0 {
		t.Fatal("the refusal must be REPORTED: a loop that silently declines is one nobody can audit")
	}
	found := false
	for _, r := range rep.Refusals {
		if r.Verb == "claim.reaped" && strings.Contains(r.Reason, "not granted") {
			found = true
		}
	}
	if !found {
		t.Errorf("the boundary's own out-of-grant refusal must be carried out: %+v", rep.Refusals)
	}
}

// conformance: acceptance criterion 5 — a lint finding becomes a FILED
// DEFECT CONTRACT, never an escalation. An escalation freezes a
// contract and demands a human decision; a finding is work somebody
// should do, and the difference is the whole of D3.
func TestMaintainFilesDefectsAndRaisesNoEscalation(t *testing.T) {
	m := maintenanceLedger(t)
	m.divergeC2(t)
	m.pendingC3(t)
	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Findings) == 0 {
		t.Fatal("the fixture must PLANT a finding: a filing drill over a clean ledger asserts nothing")
	}
	if len(rep.Filed) == 0 {
		t.Fatalf("every finding files a defect contract: %+v", rep.Findings)
	}
	st, failEnv := loadVerdictState(m.ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify: %+v", failEnv)
	}
	// Raised BY THE LOOP specifically: another lane's escalation is
	// none of this drill's business, and scanning for the bare verb
	// would make the assertion fire on somebody else's act while
	// staying silent about which actor raised it.
	for _, rec := range st.records {
		if rec.Event.Verb == "escalation.raised" && rec.Event.Actor == m.fps["maintenance"] {
			t.Fatalf("a lint finding must never freeze a contract: the loop raised an escalation on %s", rec.Event.Subject)
		}
	}
	filed := rep.Filed[0]
	// The id is the DERIVED defect id, not the finding's subject. This
	// is what makes filing idempotent through the ledger's own
	// duplicate refusal — and it is also the assertion that catches a
	// filing path which reached the subject by some other route, such
	// as raising an escalation on it and returning that subject as the
	// "filed" id (review finding on #205).
	if want := maintain.DefectID(reconcile.Finding{Subject: filed.Subject, Class: filed.Class,
		Detail: findingDetail(rep, filed.Subject, filed.Class)}); filed.Filed != want {
		t.Errorf("a finding files under its derived id %q, got %q", want, filed.Filed)
	}
	if !strings.HasPrefix(filed.Filed, "d-") {
		t.Fatalf("a filed defect is recognizable as one: %q", filed.Filed)
	}
	s, ok := st.fold.State(filed.Filed)
	if !ok {
		t.Fatalf("the filed contract must exist in the fold: %s", filed.Filed)
	}
	if s.State != "backlog" {
		t.Errorf("a filed defect lands in the ordinary queue at backlog, awaiting a spec like any other intent — not frozen, which is what an escalation would have done: %q", s.State)
	}
	// The filed intent cites the finding's class and subject, so the
	// contract says what it is for rather than merely existing.
	cited := false
	for _, rec := range st.records {
		if rec.Event.Verb == "intent.filed" && rec.Event.Subject == filed.Filed {
			if strings.Contains(string(rec.Event.Payload), filed.Class) &&
				strings.Contains(string(rec.Event.Payload), filed.Subject) {
				cited = true
			}
		}
	}
	if !cited {
		t.Error("the filed contract must name the finding's class and subject")
	}
}

// divergeC2 plants a SECOND contract carrying evidence-grade
// divergence: a merge observed at a commit the repository does not
// have, citing a receipt the artifact store does not hold.
//
// It is planted by raw push rather than by driving the real chain,
// and that is the honest shape rather than a shortcut: divergence is
// precisely the state that arises OUTSIDE the admitted chain — a
// force-push after an observation, an artifact store pruned behind a
// verdict — so a fixture that could only produce it through the front
// door would be modeling a different thing. What the drill asserts is
// that the pass SEES it.
func (m *maintenanceStand) divergeC2(t *testing.T) {
	t.Helper()
	offerFile(t, m.ld, m.priv, strings.Repeat("a", 40), "c-2")
	fence, err := admitAppend(t, m.ld, workerRawKey(23), "claim.taken", "c-2", `{}`)
	if err != nil {
		t.Fatalf("c-2 claim: %v", err)
	}
	sub := rawAppend(t, m.ld, workerRawKey(23), "submission.made", "c-2", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["c-2"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fence, packet.ZeroRange))
	rawAppend(t, m.ld, workerRawKey(24), "verdict.rendered", "c-2", fmt.Sprintf(
		`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`,
		strings.Repeat("b", 64), sub))
	// A merged commit this repository has never had: the target-rewrite
	// case, which no record-derived class can see.
	rawAppend(t, m.ld, workerRawKey(21), "merge.observed", "c-2",
		fmt.Sprintf(`{"merged": %q, "pr": "pr/9"}`, strings.Repeat("c", 40)))
}

// conformance: acceptance criterion 3b — the pass reports an
// EVIDENCE-GRADE finding that internal/reconcile.Classify alone
// cannot see. This is D2.5 enforced rather than asserted: a
// maintenance pass built on the record-derived half only would come
// back green here, and would be omitting exactly the divergence
// reconciliation the charter asks this loop for.
func TestMaintainSeesEvidenceGradeDivergence(t *testing.T) {
	m := maintenanceLedger(t)
	m.divergeC2(t)
	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	classes := map[string]bool{}
	for _, f := range rep.Findings {
		classes[f.Class] = true
	}
	if !classes[reconcile.ClassTargetRewritten] {
		t.Fatalf("the pass must report the rewritten target: %+v", rep.Findings)
	}

	// And the control: the record-derived half, run alone over the same
	// ledger, does NOT carry it. Without this the drill would pass on a
	// pass that happened to report everything for some other reason,
	// and the claim "consumes the complete result" would be untested.
	st, failEnv := loadVerdictState(m.ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify: %+v", failEnv)
	}
	for _, f := range reconcile.Classify(st.records, st.fold) {
		if f.Class == reconcile.ClassTargetRewritten {
			t.Fatal("Classify must NOT carry the evidence-grade class, or this drill proves nothing about D2.5")
		}
	}
}

// conformance: acceptance criterion 5b — a checkpoint's snapshot is
// FETCHED AND VERIFIED by a fresh reader, which starts from it without
// replaying the chain.
//
// This is the drill the criterion turns on. Every other checkpoint
// assertion — that the event was signed, admitted, counted in the
// report — passes just as happily over a checkpoint nobody can start
// from, and that is exactly what the first draft of the plan would
// have shipped. A checkpoint nobody has ever started from is a claim,
// not a capability, and the only test that can tell those apart is one
// that starts from it.
func TestMaintainCheckpointIsStartableByAFreshReader(t *testing.T) {
	m := maintenanceLedger(t)
	e, code := m.run(t, "--out", m.out)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if rep.Checkpoint == nil {
		t.Fatal("a pass given an output directory checkpoints")
	}
	if len(rep.Rebuilt) == 0 {
		t.Error("the checkpoint attests to a rebuild that happened")
	}

	// The reader knows only what the CHAIN carries: it re-reads the
	// checkpoint event rather than trusting the report that produced
	// it, because a fresh reader has no report.
	st, failEnv := loadVerdictState(m.ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify: %+v", failEnv)
	}
	var cited *checkpoint.Checkpoint
	for _, rec := range st.records {
		if rec.Event.Verb != checkpoint.Verb {
			continue
		}
		c, err := checkpoint.Parse(rec.Event.Subject, rec.Event.Payload)
		if err != nil {
			t.Fatalf("the admitted checkpoint must parse under the format it declares: %v", err)
		}
		cited = c
	}
	if cited == nil {
		t.Fatal("the chain must carry the checkpoint the report claims")
	}

	// Fetch, verify against the hash the signed event carries, and read.
	body, err := artifact.Open(m.artifacts).Get(cited.Snapshot)
	if err != nil {
		t.Fatalf("the snapshot the checkpoint names must be RETRIEVABLE — a reader that cannot fetch it replays, which is what the checkpoint exists to spare it: %v", err)
	}
	if got := artifact.Digest(body); got != cited.Snapshot {
		t.Fatalf("the fetched snapshot must verify against the signed hash: %s != %s", got, cited.Snapshot)
	}
	snap, err := checkpoint.ReadSnapshot(body)
	if err != nil {
		t.Fatalf("the materialization must be readable under its declared format: %v", err)
	}
	if snap.Position != cited.Position {
		t.Errorf("the snapshot materializes the position the checkpoint names: %q != %q", snap.Position, cited.Position)
	}
	if len(snap.Files) == 0 {
		t.Fatal("a snapshot with no projection files is not a state anyone can start from")
	}

	// It really is the projection state: the reader starts from it
	// WITHOUT replaying, and gets what a rebuild would have given.
	//
	// The comparison is against the prefix the snapshot NAMES, not the
	// current tip. The checkpoint event is appended after its own
	// snapshot, so the tip is one record ahead by construction and the
	// report projection counts that very checkpoint. A drill that
	// compared against the tip would fail on correct behavior, which is
	// the shape that tempts you to "fix" working code.
	at, err := strconv.Atoi(snap.Position)
	if err != nil {
		t.Fatal(err)
	}
	if at >= len(st.records) {
		t.Fatalf("the snapshot names position %d beyond the chain's %d records", at, len(st.records))
	}
	prefix := st.records[:at]
	for _, p := range project.Default() {
		if _, ok := snap.Files[p.Name+"/"+p.Name+".json"]; !ok {
			continue
		}
		built, err := p.Build(prefix, project.Inputs{})
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range built {
			got, ok := snap.Files[p.Name+"/"+name]
			if !ok {
				t.Errorf("the snapshot is missing %s/%s", p.Name, name)
				continue
			}
			if string(got) != string(want) {
				t.Errorf("the snapshot's %s/%s differs from the derivation it claims to materialize", p.Name, name)
			}
		}
	}
}

// A checkpoint whose payload the boundary will not accept never
// becomes a checkpoint at all: an unversioned format, a snapshot that
// is not a digest, a location nothing can fetch, or a missing
// position. Before the checkpoint rule the boundary took any payload,
// so each of these would have been signed, admitted, and useless.
func TestCheckpointPayloadRefusals(t *testing.T) {
	m := maintenanceLedger(t)
	good := fmt.Sprintf(`{"format": %q, "snapshot": %q, "location": %q, "position": "1"}`,
		checkpoint.Format, strings.Repeat("d", 64), checkpoint.Location)
	for _, tc := range []struct{ name, payload, says string }{
		{"the pre-rule payload", `{"n": 1}`, "strict object"},
		{"an unknown format", strings.Replace(good, checkpoint.Format, "made-up/v9", 1), "versioned"},
		{"a snapshot that is not a digest",
			strings.Replace(good, strings.Repeat("d", 64), "not-a-digest", 1), "digest"},
		{"a location nothing can fetch",
			strings.Replace(good, checkpoint.Location, "somewhere", 1), "fetch"},
		{"no position", strings.Replace(good, `"position": "1"`, `"position": ""`, 1), "non-negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// admitAppendErr, not `ledger append`: the append verb is
			// the RAW dev seam and runs no rules at all, so a refusal
			// drill against it would pass over any payload whatsoever
			// and prove nothing about the boundary. This is the same
			// admit.Check the pre-receive hook and every admitted verb
			// run.
			_, err := admitAppendErr(m.ld, workerRawKey(31), checkpoint.Verb, "seed/0", tc.payload)
			if err == nil {
				t.Fatal("the boundary must refuse it")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal must say %q: %v", tc.says, err)
			}
		})
	}
	// And the good one lands, so the drill above is refusing for the
	// stated reason rather than because nothing can be checkpointed.
	if _, err := admitAppendErr(m.ld, workerRawKey(31), checkpoint.Verb, "seed/0", good); err != nil {
		t.Fatalf("a well-formed citation must admit: %v", err)
	}
}

// conformance: acceptance criterion 4 — the unsettled-run lint fires
// only once the subject has moved past the window that could still
// settle it, and NOT mid park or reap flow. Both directions are
// asserted: a drill that only checked the quiet case would pass on a
// lint that never fires at all, and one that only checked the loud
// case would pass on a lint that fires against every run in flight.
//
// The anchoring itself is internal/obligation's, deliberately
// (D2). What this drill pins is that the pass CONSUMES it rather than
// re-deriving a closed-without-settle predicate, which is the version
// that looks obviously right and files a spurious finding against
// every window still open.
func TestMaintainUnsettledRunIsPositionAnchored(t *testing.T) {
	m := maintenanceLedger(t)
	fence := fmt.Sprintf("%d", m.fence)
	e, code := runEnv(t, "ledger", "append", "--ledger", m.ld, "--key", m.keys["workerA"],
		"--verb", "budget.reserve", "--subject", "c-1", "--payload", `{"amount": "10", "fence": "`+fence+`"}`)
	if code != 0 {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	reservation := *e.Position
	if _, err := admitAppend(t, m.ld, workerRawKey(21), "run.started", "c-1",
		fmt.Sprintf(`{"fence": %q, "reservation": %q}`, fence, reservation)); err != nil {
		t.Fatalf("run.started: %v", err)
	}

	// Direction one: the window is still open, so the run can still
	// settle and nothing is owed yet.
	if unsettledFor(t, m, "c-1") {
		t.Fatal("a run inside its own live window is not unsettled: post-close settlement is valid, and flagging here files a finding against every run in flight")
	}

	// Direction two: the window closes and the subject moves on, so
	// the run can no longer settle and the lint fires.
	if _, err := admitAppend(t, m.ld, workerRawKey(22), "claim.released", "c-1",
		fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["c-1"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
			fence, packet.ZeroRange)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := admitAppend(t, m.ld, workerRawKey(23), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("the subsequent window is what anchors the flag: %v", err)
	}
	if !unsettledFor(t, m, "c-1") {
		t.Fatal("once a subsequent window has opened the run can never settle, and the lint must fire")
	}
}

// unsettledFor runs one pass and reports whether it found the
// unsettled-run class on the subject.
func unsettledFor(t *testing.T, m *maintenanceStand, subject string) bool {
	t.Helper()
	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	for _, f := range report(t, e).Findings {
		if f.Subject == subject && f.Class == reconcile.ClassRunUnsettled {
			return true
		}
	}
	return false
}

// conformance: the wedge half of D4's corroboration is judged by the
// fence rule's OWN terms (review finding on #205).
//
// A wedge.declared naming a STALE fence is refused at admission — "any
// citation present must match the active fence whoever signs" — and the
// first version of admit.WedgeDeclared never looked at the citation at
// all. It checked only that the claim at that position carried the
// fence being asked about, so a boundary-refused declaration
// corroborated a reap of a LIVE claim. That is the precise hole the
// derivation exists to close, reopened inside it.
func TestMaintainRefusesAWedgeCitingTheWrongFence(t *testing.T) {
	m := maintenanceLedger(t)
	m.stale(t)
	// Shape-valid, signed by a capable actor, at a position where the
	// active claim IS m.fence — and citing a different one.
	// Signed by an OPERATOR-capable key, which wedge.declared requires:
	// the first draft of this drill used the supervisor and passed
	// because the grant rule refused it, so the citation was never the
	// deciding factor. A drill that passes for the wrong reason is the
	// vacuity this card keeps guarding against.
	rawAppend(t, m.ld, workerRawKey(31), "wedge.declared", "c-1", fmt.Sprintf(
		`{"fence": "%d", "observed": %q, "count": 0, "since": %q}`,
		m.fence+1, m.asOf, m.asOf))

	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Reaped) != 0 {
		t.Fatalf("a wedge naming a fence other than the active one corroborates nothing: %+v", rep.Reaped)
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Because, "asked to stop") {
		t.Fatalf("the skip must be the uncorroborated one: %+v", rep.Skipped)
	}
}

// And the control: the SAME wedge citing the right fence does reap, so
// the drill above is refusing for the citation rather than because
// wedges never corroborate at all.
func TestMaintainReapsOnAWedgeCitingTheActiveFence(t *testing.T) {
	m := maintenanceLedger(t)
	m.stale(t)
	if _, err := admitAppend(t, m.ld, workerRawKey(31), "wedge.declared", "c-1", fmt.Sprintf(
		`{"fence": "%d", "observed": %q, "count": 0, "since": %q}`,
		m.fence, m.asOf, m.asOf)); err != nil {
		t.Fatalf("a correctly cited wedge must admit: %v", err)
	}
	e, code := m.run(t)
	if code != 0 {
		t.Fatalf("maintain run: %d %+v", code, e)
	}
	rep := report(t, e)
	if len(rep.Reaped) != 1 {
		t.Fatalf("an admitted wedge on the active fence corroborates: %+v (skipped %+v)", rep.Reaped, rep.Skipped)
	}
	if !strings.Contains(rep.Reaped[0].Because, "wedge.declared") {
		t.Errorf("the reap names the corroboration that decided it: %q", rep.Reaped[0].Because)
	}
}

// findingDetail recovers a finding's detail from the report, so the
// derived-id assertion recomputes the id from the same three parts the
// loop hashed rather than from a copy of them.
func findingDetail(rep maintain.Report, subject, class string) string {
	for _, f := range rep.Findings {
		if f.Subject == subject && f.Class == class {
			return f.Detail
		}
	}
	return ""
}

// pendingC3 leaves a contract in REVIEW carrying a finding: a pass
// verdict with no observed merge, which reconcile.Subject reports as
// unreconciled.
//
// Its state is the point. escalation.raised admits only from ready or
// review, so without such a subject a filing path that tried to raise
// an escalation would have its attempt refused and fall through to
// intent.filed, behaving identically and leaving every assertion green
// (review finding on #205 — that is exactly how a swallowed escalation
// attempt survived this drill). With c-3 present the attempt LANDS,
// and the actor scan fires.
func (m *maintenanceStand) pendingC3(t *testing.T) {
	t.Helper()
	offerFile(t, m.ld, m.priv, strings.Repeat("a", 40), "c-3")
	fence, err := admitAppend(t, m.ld, workerRawKey(23), "claim.taken", "c-3", `{}`)
	if err != nil {
		t.Fatalf("c-3 claim: %v", err)
	}
	sub := rawAppend(t, m.ld, workerRawKey(23), "submission.made", "c-3", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["c-3"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fence, packet.ZeroRange))
	rawAppend(t, m.ld, workerRawKey(24), "verdict.rendered", "c-3", fmt.Sprintf(
		`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`,
		strings.Repeat("b", 64), sub))
}
