package main

// The executor drills end-to-end (plans/os-1dad487d.md;
// spec/executors.md): the spend bracket (reserve, start,
// provision, meter, settle), the local worktree adapter's surfaces,
// and the Phase 7 exit's remaining named drill — disposability under
// a real SIGKILL after an admitted synchronization, with the loss
// window exactly the observation lines after the last sync.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/executor"
	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// admitAppendErr is admitAppend without the testing.T: the helper process
// runs the same online admission sequence (context at the tip, full
// Check, append with the context resolver) and reports errors as
// values.
func admitAppendErr(ld string, key ed25519.PrivateKey, verb, subject, payload string) (int, error) {
	store, err := ledger.Open(ld)
	if err != nil {
		return -1, err
	}
	ctx, err := admit.ContextAt(store)
	if err != nil {
		return -1, err
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		return -1, err
	}
	rec, err := event.Sign(event.Event{
		V: ctx.Active, TS: time.Now().UTC().Format(time.RFC3339), Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: ctx.Tip,
	}, key)
	if err != nil {
		return -1, err
	}
	if err := admit.Check(ctx, rec); err != nil {
		return -1, err
	}
	return store.Append(rec, ctx.Resolve)
}

// TestHelperWorker is the killable worker process (the test-binary
// re-exec pattern): inside the provisioned workspace it meters, lands
// the admitted synchronization, and keeps metering until killed. It
// never cleans up — SIGKILL is the point.
func TestHelperWorker(t *testing.T) {
	if os.Getenv("SEED_RUN_HELPER") != "1" {
		t.Skip("helper process, spawned by the disposability drill")
	}
	ld := os.Getenv("SEED_RUN_LEDGER")
	subject := os.Getenv("SEED_RUN_SUBJECT")
	obsDir := os.Getenv("SEED_RUN_OBS")
	actor := os.Getenv("SEED_RUN_ACTOR")
	fence, _ := strconv.Atoi(os.Getenv("SEED_RUN_FENCE"))
	seedByte, _ := strconv.Atoi(os.Getenv("SEED_RUN_KEY"))
	rng := os.Getenv("SEED_RUN_RNG")
	key := workerRawKey(byte(seedByte))
	meter := func(units int, step string) {
		_ = obs.Append(obsDir, actor, fmt.Sprintf("%d", fence), obs.Line{
			TS: time.Now().UTC().Format(time.RFC3339), Subject: subject, Step: step, Units: units,
		})
	}
	meter(3, "warmup")
	if _, err := admitAppendErr(ld, key, "submission.made", subject, fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["%s ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fence, subject, rng)); err != nil {
		os.Exit(7)
	}
	for {
		meter(1, "post-sync")
		time.Sleep(20 * time.Millisecond)
	}
}

// conformance: III.H — disposability after the last admitted
// synchronization, drilled with a randomized SIGKILL of a real worker
// process: no admitted fact is lost, the contract completes elsewhere
// from the surviving ledger alone, and the loss window is exactly the
// observation lines after the last sync. Also the spend bracket:
// execution is fenced to the reservation (reserve, run.started,
// Provision) and metering settles to the ledger at run end.
func TestDisposabilityDrill(t *testing.T) {
	seed := time.Now().UnixNano()
	r := rand.New(rand.NewSource(seed))
	site := []string{"after-sync", "after-loss", "after-settled"}[r.Intn(3)]
	t.Logf("disposability drill: seed %d, kill site %s", seed, site)

	ld, src, base, specCommit, head, priv, _, keys, fps := offerLedger(t)
	rng := base + ".." + head
	offerFile(t, ld, priv, specCommit, "c-1")
	fencePos, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	fence := fmt.Sprintf("%d", fencePos)
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "budget.reserve", "--subject", "c-1", "--payload", `{"amount": "10", "fence": "`+fence+`"}`)
	if code != 0 {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	reservation := *e.Position

	// The bracket: Provision refuses before the admitted start.
	obsDir := filepath.Join(t.TempDir(), "obs")
	var lw executor.LocalWorktree
	spec := executor.ProvisionSpec{
		Ledger: ld, Repo: src, Base: base, Subject: "c-1", Actor: fps["workerA"],
		Fence: fencePos, Started: -1, Packet: []byte(`{"drill": "packet"}`), ObsDir: obsDir,
	}
	if _, err := lw.Provision(spec); !errors.Is(err, executor.ErrNoAdmittedStart) {
		t.Fatalf("Provision refuses without the admitted run.started: %v", err)
	}

	// Fold presence is never admission (review finding on the task
	// PR): raw-pushed starts fold into the list but fail the
	// position-accurate boundary — the holder lacks the verb's
	// capability, and a supervisor start citing no valid reservation
	// fails its citation — so Provision refuses both.
	rawStart := rawAppend(t, ld, workerRawKey(22), "run.started", "c-1",
		`{"fence": "`+fence+`", "reservation": "`+reservation+`"}`)
	spec.Started = rawStart
	if _, err := lw.Provision(spec); !errors.Is(err, executor.ErrNoAdmittedStart) {
		t.Fatalf("a raw holder-signed start provisions nothing: %v", err)
	}
	rawCite := rawAppend(t, ld, workerRawKey(21), "run.started", "c-1",
		`{"fence": "`+fence+`", "reservation": "999"}`)
	spec.Started = rawCite
	if _, err := lw.Provision(spec); !errors.Is(err, executor.ErrNoAdmittedStart) {
		t.Fatalf("a start citing no valid reservation provisions nothing: %v", err)
	}
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.started", "--subject", "c-1", "--payload",
		`{"fence": "`+fence+`", "reservation": "`+reservation+`"}`); code != 0 {
		t.Fatalf("run.started: %d %+v", code, e)
	}
	started, _ := strconv.Atoi(*e.Position)
	spec.Started = started

	// A refused provision leaks no checkout (review finding on the
	// task PR): fail provisioning after the worktree add — the
	// observation root is a file, so the stream directory cannot be
	// made — and the worktree registration rolls back.
	worktrees := func() int {
		out, err := exec.Command("git", "-C", src, "worktree", "list", "--porcelain").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(out), "worktree ")
	}
	before := worktrees()
	badObs := filepath.Join(t.TempDir(), "obs-as-file")
	if err := os.WriteFile(badObs, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	badSpec := spec
	badSpec.ObsDir = badObs
	if _, err := lw.Provision(badSpec); err == nil {
		t.Fatal("provisioning must fail when the observation root is a file")
	}
	if got := worktrees(); got != before {
		t.Fatalf("a failed provision rolls its worktree back: %d registered, want %d", got, before)
	}

	run, err := lw.Provision(spec)
	if err != nil {
		t.Fatalf("Provision against the admitted start: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(run.Workspace(), ".seed-run", "packet.json")); err != nil || string(b) != `{"drill": "packet"}` {
		t.Fatalf("the packet lands in the workspace: %v %q", err, b)
	}
	// The lessons ride beside the packet (plans/os-96850e5a.md D6): an
	// empty list when the caller derived none, the given bytes when
	// it did.
	if b, err := os.ReadFile(filepath.Join(run.Workspace(), ".seed-run", "lessons.json")); err != nil || string(b) != "[]\n" {
		t.Fatalf("an empty lessons.json lands beside the packet: %v %q", err, b)
	}
	if err := run.Dispose(); err != nil {
		t.Fatal(err)
	}
	withLessons := spec
	withLessons.Lessons = []byte(`[{"lesson": "knowledge/lessons/x.md @ 0123456"}]`)
	run, err = lw.Provision(withLessons)
	if err != nil {
		t.Fatalf("Provision with lessons: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(run.Workspace(), ".seed-run", "lessons.json")); err != nil || string(b) != string(withLessons.Lessons) {
		t.Fatalf("the derived lessons land beside the packet: %v %q", err, b)
	}
	if got := lw.Tuple(); got.Harness != executor.LocalHarness || got.Environment != executor.LocalEnvironment {
		t.Fatalf("the local adapter's static report: %+v", got)
	}
	if err := lw.Wake(fps["workerA"]); err != nil {
		t.Fatalf("wake is the advisory no-op: %v", err)
	}
	if err := run.Meter(2, "provisioned"); err != nil {
		t.Fatalf("meter: %v", err)
	}

	// The killable worker, running inside the workspace.
	cmd := exec.Command(os.Args[0], "-test.run", "TestHelperWorker$", "-test.v")
	cmd.Dir = run.Workspace()
	cmd.Env = append(os.Environ(),
		"SEED_RUN_HELPER=1", "SEED_RUN_LEDGER="+ld, "SEED_RUN_SUBJECT=c-1",
		"SEED_RUN_OBS="+obsDir, "SEED_RUN_ACTOR="+fps["workerA"],
		"SEED_RUN_FENCE="+fence, "SEED_RUN_KEY=22", "SEED_RUN_RNG="+rng)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	synced := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		st, failEnv := loadVerdictState(ld)
		if failEnv != nil {
			continue
		}
		if s, ok := st.fold.State("c-1"); ok && s.State == "review" {
			synced = true
			break
		}
	}
	if !synced {
		_ = cmd.Process.Kill()
		t.Fatal("the worker never landed the admitted synchronization")
	}
	if site == "after-loss" {
		time.Sleep(150 * time.Millisecond) // accumulate post-sync loss lines
	}
	if site == "after-settled" {
		if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
			"--verb", "run.settled", "--subject", "c-1", "--payload",
			`{"fence": "`+fence+`", "units": "6", "lines": "4"}`); code != 0 {
			t.Fatalf("run.settled before the kill: %d %+v", code, e)
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// Every admitted fact survives: the chain verifies from genesis.
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatalf("the chain must verify after the kill: %+v", failEnv)
	}
	if s, _ := st.fold.State("c-1"); s.State != "review" {
		t.Fatalf("the admitted synchronization survives: %s", s.State)
	}

	// The contract completes elsewhere, from the surviving ledger
	// alone: verdict, request, observed — none of it needs the dead
	// worker or its workspace.
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", keys["verifier"], "--verdict", "pass")
	if code != 0 {
		t.Fatalf("verdict after the kill: %d %+v", code, e)
	}
	verdictPos := *e.Position
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.requested", "--subject", "c-1", "--payload", `{"verdict": "`+verdictPos+`"}`); code != 0 {
		t.Fatalf("request after the kill: %d %+v", code, e)
	}
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.observed", "--subject", "c-1", "--payload", `{"merged": "`+head+`", "pr": "pr/7"}`); code != 0 {
		t.Fatalf("observed after the kill: %d %+v", code, e)
	}
	if site != "after-settled" {
		if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
			"--verb", "run.settled", "--subject", "c-1", "--payload",
			`{"fence": "`+fence+`", "units": "6", "lines": "4"}`); code != 0 {
			t.Fatalf("run.settled after the kill: %d %+v", code, e)
		}
	}

	// The loss window: the stream holds the worker's lines — the
	// warmup and the unsynchronized post-sync tail — and completion
	// needed none of them.
	snap, err := obs.Load(obsDir)
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := snap.StreamFor(fps["workerA"], fence)
	if !ok || len(stream.Lines) < 2 {
		t.Fatalf("the stream holds the metered lines: %+v", stream)
	}
	post := 0
	for _, l := range stream.Lines {
		if l.Step == "post-sync" {
			post++
		}
	}
	if post == 0 {
		t.Fatal("the post-sync loss lines exist in the stream and nowhere else")
	}

	// Disposing the dead run's workspace loses nothing admitted.
	if err := run.Dispose(); err != nil {
		t.Fatalf("dispose after the kill: %v", err)
	}
	if _, failEnv := loadVerdictState(ld); failEnv != nil {
		t.Fatalf("the chain verifies after disposal: %+v", failEnv)
	}
}

// drillBase is the configuration the qualification drills qualify
// workerA for: the local adapter's two fields, and the three a caller
// declares (plans/os-8e53ffd9.md D1, D3).
var drillBase = map[string]string{
	"principal": "acme", "harness": executor.LocalHarness, "model": "fable/5.1",
	"tool_policy": "default", "environment": executor.LocalEnvironment,
}

// drillTuple renders drillBase with the named fields overridden.
func drillTuple(override map[string]string) string {
	m := map[string]string{}
	for k, v := range drillBase {
		m[k] = v
	}
	for k, v := range override {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// rootAppend appends one record with the root key through the raw
// seam and returns its position.
func rootAppend(t *testing.T, ld, priv, verb, subject, payload string) int {
	t.Helper()
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", verb, "--subject", subject, "--payload", payload)
	if code != 0 {
		t.Fatalf("%s %s: %d %+v", verb, subject, code, e)
	}
	pos, err := strconv.Atoi(*e.Position)
	if err != nil {
		t.Fatal(err)
	}
	return pos
}

// qualifiedLedger is offerLedger carried to seed/2, with workerA's
// claim grant citing drillBase and workerB left on its bridge grant. It
// returns the position of the first seed/2 record, for the
// mixed-version replay (AC2c).
func qualifiedLedger(t *testing.T) (ld, src, base, specCommit, priv string, rootKey ed25519.PrivateKey, keys, fps map[string]string, firstSeed2 int) {
	t.Helper()
	ld, src, base, specCommit, _, priv, rootKey, keys, fps = offerLedger(t)
	rootAppend(t, ld, priv, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	firstSeed2 = rootAppend(t, ld, priv, "actor.granted", fps["workerA"], `{"capability": "claim", "tuple": `+drillTuple(nil)+`}`)
	return
}

// openWindow takes the claim on the subject with the worker's key and
// reserves ten units under it, returning the fence and the reservation
// position.
func openWindow(t *testing.T, ld string, worker byte, keyPath, subject string) (fence, reservation string) {
	t.Helper()
	fencePos, err := admitAppend(t, ld, workerRawKey(worker), "claim.taken", subject, `{}`)
	if err != nil {
		t.Fatalf("claim %s: %v", subject, err)
	}
	fence = fmt.Sprintf("%d", fencePos)
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keyPath,
		"--verb", "budget.reserve", "--subject", subject, "--payload", `{"amount": "10", "fence": "`+fence+`"}`)
	if code != 0 {
		t.Fatalf("reserve %s: %d %+v", subject, code, e)
	}
	return fence, *e.Position
}

// rawAppendAt is rawAppend at a named version: a record pushed past
// every boundary, so a verification drill has something to find.
func rawAppendAt(t *testing.T, ld string, key ed25519.PrivateKey, v, verb, subject, payload string) int {
	t.Helper()
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	tip, count, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: v, TS: "2026-09-02T00:00:00Z", Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, func(string) (ed25519.PublicKey, bool) { return key.Public().(ed25519.PublicKey), true }); err != nil {
		t.Fatalf("raw append %s: %v", verb, err)
	}
	return count
}

// assertDrift pins the drift refusal's shape: the out_of_grant family
// and exit (plans/os-8e53ffd9.md D4), a message naming the holder, the
// field, the declared value and the cited one, the position it was
// computed at, and the caller's affordances beside it.
func assertDrift(t *testing.T, e ledgerEnv, code int, holder, field, have, cited string) {
	t.Helper()
	if code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("%s: drift refuses out_of_grant (exit 14), got %d %+v", field, code, e.Error)
	}
	for _, want := range []string{holder, field, have, cited} {
		if !strings.Contains(e.Error.Message, want) {
			t.Fatalf("%s: the refusal names the holder, the field and both values, missing %q: %s", field, want, e.Error.Message)
		}
	}
	if e.Position == nil {
		t.Fatalf("%s: a refusal computed against the chain stamps its position", field)
	}
	if len(e.Affordances) == 0 {
		t.Fatalf("%s: the refusal carries the supervisor's affordances on the subject", field)
	}
}

// setField writes one named tuple field, for the lying adapters.
func setField(tu *executor.Tuple, field, value string) {
	switch field {
	case "principal":
		tu.Principal = value
	case "harness":
		tu.Harness = value
	case "model":
		tu.Model = value
	case "tool_policy":
		tu.ToolPolicy = value
	case "environment":
		tu.Environment = value
	}
}

// conformance: plans/os-8e53ffd9.md AC1, AC2, AC2b, AC3 — seed run
// start declares the runtime tuple, filling harness and environment
// from the adapter and never inventing the three fields it cannot
// know; a declaration differing from the CLAIM HOLDER's cited set in
// any one field is out of grant, per field, five rows against a real
// ledger; the match admits; an unqualified holder admits any; and
// Provision holds the adapter to the admitted declaration with nothing
// left behind on a mismatch.
func TestRunStartDeclaresTheTupleAndDriftIsOutOfGrant(t *testing.T) {
	ld, src, base, specCommit, priv, _, keys, fps, _ := qualifiedLedger(t)
	start := func(subject string, extra ...string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", subject}, extra...)...)
	}
	declare := func(override map[string]string) []string {
		m := map[string]string{"principal": drillBase["principal"], "model": drillBase["model"], "tool_policy": drillBase["tool_policy"]}
		for k, v := range override {
			m[k] = v
		}
		return []string{"--principal", m["principal"], "--model", m["model"], "--tool-policy", m["tool_policy"]}
	}
	offerFile(t, ld, priv, specCommit, "c-1")
	fence, reservation := openWindow(t, ld, 22, keys["workerA"], "c-1")

	// AC3: a field neither supplied nor derivable refuses as usage
	// naming ITS flag, beside the caller's affordances, and nothing is
	// appended.
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	before := st.count
	e, code := start("c-1", "--principal", "acme", "--model", "fable/5.1")
	if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "--tool-policy") || strings.Contains(e.Error.Message, "--principal,") {
		t.Fatalf("a missing declared field refuses as usage naming its flag: %d %+v", code, e.Error)
	}
	if len(e.Affordances) == 0 {
		t.Fatalf("the refusal carries the supervisor's affordances: %+v", e)
	}
	if st, _ = loadVerdictState(ld); st.count != before {
		t.Fatal("a usage refusal appends nothing")
	}

	// AC1, three rows through the flags the caller declares...
	for _, row := range []struct{ field, value string }{
		{"principal", "someone-else"}, {"model", "fable/6.0"}, {"tool_policy", "unrestricted"},
	} {
		e, code := start("c-1", declare(map[string]string{row.field: row.value})...)
		assertDrift(t, e, code, fps["workerA"], row.field, row.value, drillBase[row.field])
	}
	// ...and two through the holder's grant, because harness and
	// environment come from the adapter's report and the verb never
	// invents them: a worker whose claim grant cites another harness or
	// another environment is out of grant the moment the local adapter
	// declares its own.
	for _, row := range []struct {
		field, value, subject string
		seed                  byte
	}{{"harness", "container/v0", "c-2", 25}, {"environment", "cloud-sandbox", "c-3", 26}} {
		path, pub, fp := writeWorkerKey(t, row.seed)
		rootAppend(t, ld, priv, "actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, row.subject))
		rootAppend(t, ld, priv, "actor.granted", fp, `{"capability": "claim", "tuple": `+drillTuple(map[string]string{row.field: row.value})+`}`)
		offerFile(t, ld, priv, specCommit, row.subject)
		openWindow(t, ld, row.seed, path, row.subject)
		e, code := start(row.subject, declare(nil)...)
		assertDrift(t, e, code, fp, row.field, drillBase[row.field], row.value)
	}

	// The match admits, naming the reservation the run spends under and
	// the full configuration it declared, harness and environment
	// filled from the adapter.
	e, code = start("c-1", declare(nil)...)
	if code != 0 || e.Result["reservation"] != reservation {
		t.Fatalf("the cited configuration admits: %d %+v", code, e)
	}
	declared, _ := e.Result["tuple"].(map[string]any)
	for field, want := range drillBase {
		if declared[field] != want {
			t.Fatalf("the success reports the declared tuple, %s = %v, want %s", field, declared[field], want)
		}
	}
	started, err := strconv.Atoi(*e.Position)
	if err != nil {
		t.Fatal(err)
	}
	if e, code := start("c-1", declare(nil)...); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "one run per claim window") {
		t.Fatalf("a second start in the window refuses: %d %+v", code, e.Error)
	}

	// AC2b: Provision holds the adapter to the admitted declaration. An
	// adapter resolving any field differently is refused with the
	// rollback armed: no worktree registration survives.
	fencePos, _ := strconv.Atoi(fence)
	spec := executor.ProvisionSpec{
		Ledger: ld, Repo: src, Base: base, Subject: "c-1", Actor: fps["workerA"],
		Fence: fencePos, Started: started, Packet: []byte(`{"drill": "tuple"}`), ObsDir: filepath.Join(t.TempDir(), "obs"),
	}
	worktrees := func() int {
		out, err := exec.Command("git", "-C", src, "worktree", "list", "--porcelain").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(out), "worktree ")
	}
	registered := worktrees()
	for _, field := range tuple.Fields() {
		lying := executor.LocalWorktree{Resolve: func(declared executor.Tuple, _ string) executor.Tuple {
			setField(&declared, field, "resolved-otherwise")
			return declared
		}}
		_, err := lying.Provision(spec)
		if !errors.Is(err, executor.ErrTupleMismatch) || !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: an adapter resolving a different configuration is refused naming the field: %v", field, err)
		}
		if got := worktrees(); got != registered {
			t.Fatalf("%s: a refused provision leaves no worktree registration: %d, want %d", field, got, registered)
		}
	}
	// The honest local adapter resolves exactly its declaration, and
	// Run.Tuple() is the resolved value: complete, and equal to what the
	// admitted start declared.
	run, err := executor.LocalWorktree{}.Provision(spec)
	if err != nil {
		t.Fatalf("the local adapter's resolved tuple equals its declaration: %v", err)
	}
	if got := run.Tuple(); !got.Complete() || got.Principal != "acme" || got.Harness != executor.LocalHarness ||
		got.Model != "fable/5.1" || got.ToolPolicy != "default" || got.Environment != executor.LocalEnvironment {
		t.Fatalf("Run.Tuple() is the resolved configuration: %+v", got)
	}
	if err := run.Dispose(); err != nil {
		t.Fatal(err)
	}
	if got := worktrees(); got != registered {
		t.Fatalf("dispose removes the registration: %d, want %d", got, registered)
	}

	// AC2, the bridge: workerB's only claim grant cites no tuple, so the
	// set is empty and any declaration admits. Before it lands, a raw
	// seed/2 start with no declaration is pushed past the boundary
	// (review finding on the task PR): it is no admitted start, so it
	// provisions nothing and blocks nothing.
	offerFile(t, ld, priv, specCommit, "c-4")
	fence4, reservation4 := openWindow(t, ld, 23, keys["workerB"], "c-4")
	rawPos := rawAppendAt(t, ld, workerRawKey(21), version.Seed2, "run.started", "c-4",
		`{"fence": "`+fence4+`", "reservation": "`+reservation4+`"}`)
	fence4Pos, _ := strconv.Atoi(fence4)
	rawSpec := executor.ProvisionSpec{
		Ledger: ld, Repo: src, Base: base, Subject: "c-4", Actor: fps["workerB"],
		Fence: fence4Pos, Started: rawPos, Packet: []byte(`{}`), ObsDir: filepath.Join(t.TempDir(), "obs"),
	}
	if _, err := (executor.LocalWorktree{}).Provision(rawSpec); !errors.Is(err, executor.ErrNoAdmittedStart) {
		t.Fatalf("a raw seed/2 start with no declaration provisions nothing: %v", err)
	}
	if e, code := start("c-4", declare(map[string]string{"model": "anything/0", "principal": "anyone"})...); code != 0 {
		t.Fatalf("an unqualified holder admits any declared configuration, the raw start notwithstanding: %d %+v", code, e.Error)
	}
}

// conformance: plans/os-8e53ffd9.md D8 and AC3 — on a chain that never
// upgraded, a declaration refuses as usage naming the version and a
// bare start admits exactly as before, carrying no tuple; and the
// verb's own usage refusals.
func TestRunStartBeforeSeed2AndUsage(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	openWindow(t, ld, 22, keys["workerA"], "c-1")
	start := func(subject string, extra ...string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append([]string{"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", subject}, extra...)...)
	}
	if e, code := start("c-1", "--model", "fable/5.1"); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, version.Seed2) {
		t.Fatalf("a declaration on a seed/1 chain refuses as usage naming the version: %d %+v", code, e.Error)
	}
	e, code := start("c-1")
	if code != 0 || e.Result["tuple"] != nil {
		t.Fatalf("a bare start on a seed/1 chain admits as before, declaring nothing: %d %+v", code, e)
	}
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatal(failEnv)
	}
	if last := st.records[st.count-1].Event; last.Verb != "run.started" || strings.Contains(string(last.Payload), "tuple") {
		t.Fatalf("the seed/1 record carries the strict {fence, reservation}: %s %s", last.Verb, last.Payload)
	}

	// Usage: the subverb, the adapter, the transport, and a subject with
	// no open reservation to cite.
	for name, args := range map[string][]string{
		"no subverb":      {"run"},
		"unknown subverb": {"run", "stop"},
		"unknown adapter": {"run", "start", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-1", "--adapter", "cloud"},
		"no transport":    {"run", "start", "--key", keys["supervisor"], "--subject", "c-1"},
	} {
		if e, code := runEnv(t, args...); code != 64 || e.Error == nil {
			t.Fatalf("%s must refuse as usage: %d %+v", name, code, e)
		}
	}
	if e, code := runEnv(t, "run", "stop"); !strings.Contains(e.Error.Message, "unknown run subverb") || !strings.Contains(e.Error.Message, "start") {
		t.Fatalf("an unknown subverb names the one act: %d %s", code, e.Error.Message)
	}
	offerFile(t, ld, priv, specCommit, "c-2")
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-2", `{}`); err != nil {
		t.Fatal(err)
	}
	if e, code := start("c-2"); code != 4 || e.Error == nil || e.Error.Code != "not_found" || !strings.Contains(e.Error.Message, "a run start needs one") || !strings.Contains(e.Error.Message, "seed budget reserve") {
		t.Fatalf("a window with no open reservation refuses naming what would establish one: %d %+v", code, e.Error)
	}
}

// conformance: plans/os-8e53ffd9.md AC2c and AC4 — one chain, a grant
// with a tuple at a seed/2 position: this build verifies it; a build
// supporting only seed/1 refuses at the first seed/2 record by version,
// never by misjudging the grant. A malformed grant tuple is refused
// before it is written, and one pushed raw fails verification at its
// position as bad_actor_event.
func TestMixedVersionReplayAndMalformedGrants(t *testing.T) {
	ld, _, _, _, priv, rootKey, _, fps, firstSeed2 := qualifiedLedger(t)
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 {
		t.Fatalf("this build verifies the qualified chain: %d %+v", code, e)
	}
	e, code := runEnv(t, "ledger", "verify", "--ledger", ld, "--supported", version.Protocol+","+version.Seed1)
	if code == 0 || e.Error == nil || e.Error.Code != "version_unsupported" || e.Position == nil || *e.Position != fmt.Sprintf("%d", firstSeed2) {
		t.Fatalf("a seed/1-only build refuses at the first seed/2 record by version: %d %+v", code, e)
	}
	if strings.Contains(e.Error.Message, "tuple") || !strings.Contains(e.Error.Message, version.Seed2) {
		t.Fatalf("the refusal names the version, never the grant: %s", e.Error.Message)
	}

	for name, bad := range map[string]string{
		"missing field": `{"capability": "claim", "tuple": {"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p"}}`,
		"empty string":  `{"capability": "claim", "tuple": ` + drillTuple(map[string]string{"model": ""}) + `}`,
		"unknown field": `{"capability": "claim", "tuple": {"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e", "extra": "x"}}`,
		"non-string":    `{"capability": "claim", "tuple": {"principal": 1, "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e"}}`,
	} {
		e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", "actor.granted", "--subject", fps["workerB"], "--payload", bad)
		if code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "tuple") {
			t.Fatalf("%s: a malformed grant tuple is refused before it is written: %d %+v", name, code, e.Error)
		}
	}
	pos := rawAppendAt(t, ld, rootKey, version.Seed2, "actor.granted", fps["workerB"], `{"capability": "claim", "tuple": {"principal": "x"}}`)
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" ||
		!strings.Contains(e.Error.Message, "bad_actor_event") || !strings.Contains(e.Error.Message, fmt.Sprintf("position %d", pos)) || !strings.Contains(e.Error.Message, "tuple") {
		t.Fatalf("a raw malformed grant fails verification at its position as bad_actor_event: %d %+v at %v (want %d)", code, e.Error, e.Position, pos)
	}
}
