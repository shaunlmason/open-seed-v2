package main

// The preemption drills end-to-end (plans/os-0f718b4e.md;
// spec/executors.md's Preemption section): graceful-first — a
// polling worker observes the admitted interrupt at a safe point and
// parks deliberately with its packet, while a raw unprivileged
// interrupt parks no one — and the force path, where an
// interrupt-ignoring worker is killed and the reap still yields an
// honest packet, the subject returning to ready and completing
// elsewhere.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/executor"
	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// interruptRequested is the worker-side poll: replay the chain and
// ask the one shared derivation whether a boundary-valid interrupt
// stands for the fence. Raw unprivileged interrupts request nothing.
func interruptRequested(ld, subject string, fence int) (bool, error) {
	store, err := ledger.Open(ld)
	if err != nil {
		return false, err
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return false, err
	}
	var records []*event.Record
	if _, err := store.VerifyFromGenesis(resolve, ledger.WithObserver(func(pos int, r *event.Record) {
		records = append(records, r)
	})); err != nil {
		return false, err
	}
	table, err := transition.Default()
	if err != nil {
		return false, err
	}
	s, ok := table.FoldRecords(records).State(subject)
	if !ok {
		return false, nil
	}
	return admit.InterruptRequested(records, table, subject, s, fence), nil
}

// TestHelperPollingWorker is the conforming worker (the test-binary
// re-exec pattern): it meters and checks for a boundary-valid
// interrupt each cycle — its bounded interval — and on observing one
// finishes the cycle, parks with its packet, and exits deliberately.
func TestHelperPollingWorker(t *testing.T) {
	if os.Getenv("SEED_PREEMPT_HELPER") != "1" {
		t.Skip("helper process, spawned by the graceful preemption drill")
	}
	ld := os.Getenv("SEED_PREEMPT_LEDGER")
	subject := os.Getenv("SEED_PREEMPT_SUBJECT")
	obsDir := os.Getenv("SEED_PREEMPT_OBS")
	actor := os.Getenv("SEED_PREEMPT_ACTOR")
	fence, _ := strconv.Atoi(os.Getenv("SEED_PREEMPT_FENCE"))
	seedByte, _ := strconv.Atoi(os.Getenv("SEED_PREEMPT_KEY"))
	rng := os.Getenv("SEED_PREEMPT_RNG")
	key := workerRawKey(byte(seedByte))
	for {
		_ = obs.Append(obsDir, actor, fmt.Sprintf("%d", fence), obs.Line{
			TS: time.Now().UTC().Format(time.RFC3339), Subject: subject, Step: "work", Units: 1,
		})
		if req, err := interruptRequested(ld, subject, fence); err == nil && req {
			if _, err := admitAppendErr(ld, key, "claim.parked", subject, fmt.Sprintf(
				`{"fence": "%d", "packet": {"acceptance": ["%s resumes from this packet"], "decisions": [], "base": %q, "refs": [], "findings": [{"tried": "the run under fence %d", "outcome": "preempted by an admitted interrupt at a safe point"}]}}`,
				fence, subject, rng, fence)); err != nil {
				os.Exit(7)
			}
			os.Exit(0)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestHelperDeafWorker never polls: the non-conforming worker the
// force path exists for. It meters until killed.
func TestHelperDeafWorker(t *testing.T) {
	if os.Getenv("SEED_PREEMPT_DEAF") != "1" {
		t.Skip("helper process, spawned by the force preemption drill")
	}
	subject := os.Getenv("SEED_PREEMPT_SUBJECT")
	obsDir := os.Getenv("SEED_PREEMPT_OBS")
	actor := os.Getenv("SEED_PREEMPT_ACTOR")
	fence := os.Getenv("SEED_PREEMPT_FENCE")
	for {
		_ = obs.Append(obsDir, actor, fence, obs.Line{
			TS: time.Now().UTC().Format(time.RFC3339), Subject: subject, Step: "deaf", Units: 1,
		})
		time.Sleep(20 * time.Millisecond)
	}
}

// bracketToRun stands up the full spend bracket on c-1 and provisions
// the workspace: claim (workerA), reserve, run.started (supervisor),
// Provision. Shared by both preemption drills.
func bracketToRun(t *testing.T) (ld, src, base, rng, fence string, fencePos int, priv string, keys, fps map[string]string, run executor.Run, obsDir string) {
	t.Helper()
	var specCommit, head string
	ld, src, base, specCommit, head, priv, _, keys, fps = offerLedger(t)
	rng = base + ".." + head
	offerFile(t, ld, priv, specCommit, "c-1")
	var err error
	fencePos, err = admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	fence = fmt.Sprintf("%d", fencePos)
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "budget.reserve", "--subject", "c-1", "--payload", `{"amount": "10", "fence": "`+fence+`"}`)
	if code != 0 {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	reservation := *e.Position
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.started", "--subject", "c-1", "--payload",
		`{"fence": "`+fence+`", "reservation": "`+reservation+`"}`); code != 0 {
		t.Fatalf("run.started: %d %+v", code, e)
	}
	started, _ := strconv.Atoi(*e.Position)
	obsDir = filepath.Join(t.TempDir(), "obs")
	var lw executor.LocalWorktree
	run, err = lw.Provision(executor.ProvisionSpec{
		Ledger: ld, Repo: src, Base: base, Subject: "c-1", Actor: fps["workerA"],
		Fence: fencePos, Started: started, Packet: []byte(`{"drill": "packet"}`), ObsDir: obsDir,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	return
}

// stateOf folds the surviving ledger and returns c-1's state, failing
// the drill if the chain no longer verifies.
func stateOf(t *testing.T, ld string) transition.SubjectState {
	t.Helper()
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify: %+v", failEnv)
	}
	s, _ := st.fold.State("c-1")
	return s
}

// completesElsewhere drives the re-claimed contract to done from the
// surviving ledger alone: claim (workerB), submission, verdict pass,
// request, observed — none of it needs the preempted worker.
func completesElsewhere(t *testing.T, ld, src, rng, priv string, keys map[string]string) {
	t.Helper()
	fence2Pos, err := admitAppend(t, ld, workerRawKey(23), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("the subject must be re-claimable: %v", err)
	}
	if _, err := admitAppend(t, ld, workerRawKey(23), "submission.made", "c-1", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["c-1 ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fence2Pos, rng)); err != nil {
		t.Fatalf("the successor submits from the packet: %v", err)
	}
	e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", keys["verifier"], "--verdict", "pass")
	if code != 0 {
		t.Fatalf("verdict: %d %+v", code, e)
	}
	verdictPos := *e.Position
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.requested", "--subject", "c-1", "--payload", `{"verdict": "`+verdictPos+`"}`); code != 0 {
		t.Fatalf("request: %d %+v", code, e)
	}
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.observed", "--subject", "c-1", "--payload", `{"merged": "`+rngHead(rng)+`", "pr": "pr/8"}`); code != 0 {
		t.Fatalf("observed: %d %+v", code, e)
	}
}

// rngHead extracts the head commit from a "<base>..<head>" range.
func rngHead(rng string) string {
	for i := 0; i+1 < len(rng); i++ {
		if rng[i] == '.' && rng[i+1] == '.' {
			return rng[i+2:]
		}
	}
	return rng
}

// pollUntil waits for a CONDITION against a bounded deadline at a short
// interval (plans/os-a95db3f5.md D4). The deadline is a failure bound,
// never a pacing device: on a fast runner this returns in one interval,
// and a slow one only delays the proof rather than falsifying it. The
// parked-state loop below already had this shape; the two fixed windows
// this card replaces were the copies that got it wrong, and asserted
// "within N milliseconds" where they meant "at all".
func pollUntil(cond func() bool) bool {
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return cond()
}

// obsLines is the length of one worker's observation stream, keyed to
// its actor and fence: the only liveness evidence these drills have,
// and what both waits below are actually about. A line still being
// written parses as nothing and is skipped, so this can under-report by
// one, never over-report: every wait here is written to be sound in
// that direction.
func obsLines(obsDir, actor, fence string) int {
	snap, err := obs.Load(obsDir)
	if err != nil {
		return 0
	}
	stream, ok := snap.StreamFor(actor, fence)
	if !ok {
		return 0
	}
	return len(stream.Lines)
}

// conformance: III.H — preemption is graceful-first with specified
// safe-point semantics in the worker contract: the admitted interrupt
// parks a conforming worker deliberately with its packet, a raw
// unprivileged interrupt parks no one, and the contract completes
// elsewhere from the packet.
func TestGracefulPreemptionDrill(t *testing.T) {
	ld, src, _, rng, fence, fencePos, priv, keys, fps, run, obsDir := bracketToRun(t)

	cmd := exec.Command(os.Args[0], "-test.run", "TestHelperPollingWorker$", "-test.v")
	cmd.Dir = run.Workspace()
	cmd.Env = append(os.Environ(),
		"SEED_PREEMPT_HELPER=1", "SEED_PREEMPT_LEDGER="+ld, "SEED_PREEMPT_SUBJECT=c-1",
		"SEED_PREEMPT_OBS="+obsDir, "SEED_PREEMPT_ACTOR="+fps["workerA"],
		"SEED_PREEMPT_FENCE="+fence, "SEED_PREEMPT_KEY=22", "SEED_PREEMPT_RNG="+rng)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// The DoS shape: a raw unprivileged interrupt (workerB holds only
	// the claim lane) folds but parks no one.
	rawAppend(t, ld, workerRawKey(23), "run.interrupted", "c-1", `{"fence": "`+fence+`"}`)

	// A HANDSHAKE, not a settle window (plans/os-a95db3f5.md D3). A
	// duration here could pass for the wrong reason — nothing parked
	// because nothing had booted — and a growth assertion across it
	// would flake on a descheduled worker, which is the very failure
	// this card fixes.
	//
	// The helper's cycle is L1 → C1 → sleep → L2 → C2 → sleep → …:
	// each line is followed by an interrupt check. Sample the stream as
	// the raw append returns, at a moment when the raw event is already
	// in the ledger. Line n0+1 completes after that sample, so the
	// check C(n0+1) that follows it reads a ledger CONTAINING the raw
	// event; the appearance of line n0+2 proves C(n0+1) ran to
	// completion and declined to park. Two lines, and only then the
	// assertion.
	n0 := obsLines(obsDir, fps["workerA"], fence)
	if !pollUntil(func() bool { return obsLines(obsDir, fps["workerA"], fence) >= n0+2 }) {
		_ = cmd.Process.Kill()
		t.Fatalf("the worker never completed a cycle over the raw interrupt: stream stuck at %d", n0)
	}
	if s := stateOf(t, ld); s.State != "in_progress" {
		_ = cmd.Process.Kill()
		t.Fatalf("a raw unprivileged interrupt must park no one: %s", s.State)
	}

	// The supervisor's admitted interrupt parks the worker at its
	// next safe point — a deliberate exit, never a kill.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.interrupted", "--subject", "c-1", "--payload", `{"fence": "`+fence+`"}`); code != 0 {
		_ = cmd.Process.Kill()
		t.Fatalf("run.interrupted: %d %+v", code, e)
	}
	// The loop this one was: pollUntil is its extraction, so a future
	// drill copies the right pattern rather than one of the two fixed
	// windows this card removed.
	parked := pollUntil(func() bool {
		st, failEnv := loadVerdictState(ld)
		if failEnv != nil {
			return false
		}
		s, ok := st.fold.State("c-1")
		return ok && s.State == "blocked"
	})
	if !parked {
		_ = cmd.Process.Kill()
		t.Fatal("the polling worker never parked on the admitted interrupt")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the park is a deliberate exit 0, never a kill: %v", err)
	}

	// The parked contract routes onward: unblock, re-claim, complete.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "contract.unblocked", "--subject", "c-1", "--payload", `{}`); code != 0 {
		t.Fatalf("unblock: %d %+v", code, e)
	}
	completesElsewhere(t, ld, src, rng, priv, keys)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.settled", "--subject", "c-1", "--payload",
		`{"fence": "`+fence+`", "units": "8", "lines": "6"}`); code != 0 {
		t.Fatalf("the parked run settles on its prior fence: %d %+v", code, e)
	}

	// The worker's metered lines exist; nothing admitted is lost.
	snap, err := obs.Load(obsDir)
	if err != nil {
		t.Fatal(err)
	}
	if stream, ok := snap.StreamFor(fps["workerA"], fmt.Sprintf("%d", fencePos)); !ok || len(stream.Lines) == 0 {
		t.Fatalf("the stream holds the worker's lines: %+v", stream)
	}
	if err := run.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if s := stateOf(t, ld); s.State != "done" {
		t.Fatalf("the contract completed elsewhere: %s", s.State)
	}
}

// conformance: III.H — force-kill still yields a reap packet: an
// interrupt-ignoring worker is killed, the dispatch lane reaps with
// an honest packet from what is known, and the subject returns to
// ready and completes elsewhere.
func TestForcePreemptionDrill(t *testing.T) {
	ld, src, base, rng, fence, _, priv, keys, fps, run, obsDir := bracketToRun(t)

	cmd := exec.Command(os.Args[0], "-test.run", "TestHelperDeafWorker$", "-test.v")
	cmd.Dir = run.Workspace()
	cmd.Env = append(os.Environ(),
		"SEED_PREEMPT_DEAF=1", "SEED_PREEMPT_SUBJECT=c-1",
		"SEED_PREEMPT_OBS="+obsDir, "SEED_PREEMPT_ACTOR="+fps["workerA"],
		"SEED_PREEMPT_FENCE="+fence)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Establish the worker is UP before asserting anything about it
	// (plans/os-a95db3f5.md D1). The old fixed window sampled the
	// world before there was a worker to sample, so a subprocess that
	// booted slower than the window failed a system that was working.
	live := func() bool { return obsLines(obsDir, fps["workerA"], fence) > 0 }
	if !pollUntil(live) {
		_ = cmd.Process.Kill()
		t.Fatal("the deaf worker never emitted its first observation")
	}

	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.interrupted", "--subject", "c-1", "--payload", `{"fence": "`+fence+`"}`); code != 0 {
		_ = cmd.Process.Kill()
		t.Fatalf("run.interrupted: %d %+v", code, e)
	}

	// The worker ignores it: still metering PAST the interrupt, the
	// subject still in_progress. Polled for growth, never for elapsed
	// time (D2) — "kept metering past the interrupt" is what this
	// asserts, and no duration expresses it.
	before := obsLines(obsDir, fps["workerA"], fence)
	if !pollUntil(func() bool { return obsLines(obsDir, fps["workerA"], fence) > before }) {
		_ = cmd.Process.Kill()
		t.Fatalf("the deaf worker must keep metering past the interrupt: stream stuck at %d", before)
	}
	if s := stateOf(t, ld); s.State != "in_progress" {
		_ = cmd.Process.Kill()
		t.Fatalf("nothing parked the deaf worker: %s", s.State)
	}

	// The force path: kill, then reap with the packet composed from
	// what is known — no pushed work, so the zero-length range.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "claim.reaped", "--subject", "c-1", "--payload", fmt.Sprintf(
			`{"fence": %q, "packet": {"acceptance": ["c-1 resumes from this packet"], "decisions": [{"decision": "reaped after an ignored interrupt", "basis": "verified"}], "base": "%s..%s", "refs": [], "findings": [{"tried": "graceful preemption via run.interrupted", "outcome": "the worker never reached a safe point and was killed"}]}}`,
			fence, base, base)); code != 0 {
		t.Fatalf("claim.reaped: %d %+v", code, e)
	}
	if s := stateOf(t, ld); s.State != "ready" {
		t.Fatalf("a reaped subject returns to ready: %s", s.State)
	}

	completesElsewhere(t, ld, src, rng, priv, keys)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.settled", "--subject", "c-1", "--payload",
		`{"fence": "`+fence+`", "units": "5", "lines": "4"}`); code != 0 {
		t.Fatalf("the dead run settles on its prior fence: %d %+v", code, e)
	}
	if err := run.Dispose(); err != nil {
		t.Fatalf("dispose loses nothing admitted: %v", err)
	}
	if s := stateOf(t, ld); s.State != "done" {
		t.Fatalf("the contract completed elsewhere: %s", s.State)
	}
}
