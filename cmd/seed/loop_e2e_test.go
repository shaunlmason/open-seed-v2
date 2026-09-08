package main

// The worker loop end to end (plans/os-abb206c8.md AC1, AC2). This is
// the drill promotion criterion 1 actually asks for: a lane runs poll →
// orient → claim → meter → work → submit against a REAL ledger,
// entirely through the loop verbs and offer list, in the one posture
// where its claim can land — with no `ledger append` on any loop path
// and no model anywhere.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// remoteWorkLedger stands up a remote ledger with one enrolled worker,
// one specified contract, and a live offer it is eligible for.
func remoteWorkLedger(t *testing.T, subject string) (remote, state, workerKey, workerFP string) {
	t.Helper()
	dir, root, _ := writeKeys(t)
	remote = bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	state = filepath.Join(dir, "state")

	workerKey, workerPub, workerFP := writeWorkerKey(t, 22)
	supKey, supPub, supFP := writeWorkerKey(t, 21)
	rootAppend := func(verb, subj, payload string) {
		t.Helper()
		if e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
			"--key", root, "--verb", verb, "--subject", subj, "--payload", payload); code != 0 {
			t.Fatalf("%s %s: %d %+v", verb, subj, code, e)
		}
	}
	rootAppend("actor.enrolled", workerFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "workerA"}`, workerPub))
	rootAppend("actor.granted", workerFP, `{"capability": "claim"}`)
	rootAppend("actor.enrolled", supFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "supervisor"}`, supPub))
	rootAppend("actor.granted", supFP, `{"capability": "supervise"}`)
	rootAppend("intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	rootAppend("contract.specified", subject,
		`{"acceptance": {"ref": "accept.md @ abc1234", "executable": false}}`)

	// The offer: the supervisor's invitation, appended in the same
	// posture. `offer publish` is local-only and stays that way — this
	// card widened the READS, which is what a worker needs.
	offer, err := json.Marshal(map[string]any{
		"eligibility": map[string]any{"capabilities": []string{"claim"}, "tiers": []string{"trivial"}},
		"expires":     time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
		"--key", supKey, "--verb", "offer.published", "--subject", subject,
		"--payload", string(offer)); code != 0 {
		t.Fatalf("offer.published: %d %+v", code, e)
	}
	return remote, state, workerKey, workerFP
}

func implementerManifest(t *testing.T) lane.Manifest {
	t.Helper()
	for _, m := range mustLoad(t) {
		if m.Lane == "implementer" {
			return m
		}
	}
	t.Fatal("the shipped lane set must carry an implementer")
	return lane.Manifest{}
}

// conformance: promotion criterion 1 — the loop runs end to end
// through Seed verbs, orienting from one position-stamped read, and
// the manifest it reads is the SHIPPED one rather than a fixture, so
// this drill fails if lanes/implementer.json stops describing the
// loop that exists.
func TestWorkerLoopRunsEndToEndAgainstARealLedger(t *testing.T) {
	const subject = "c-1"
	remote, state, workerKey, workerFP := remoteWorkLedger(t, subject)
	obsDir := t.TempDir()

	worked := 0
	d, err := loop.New(implementerManifest(t), loopVerbs{},
		[]string{"--remote", remote, "--state", state}, workerKey,
		loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
			// The work step is a deterministic function: there is no
			// model here, and the drill says so (D6).
			worked++
			if !sit.Holds(s) {
				return 0, fmt.Errorf("the work step runs inside a held window, and the read says otherwise")
			}
			return 7, nil
		}),
		loop.WithBase("abc1234..abc1234"), loop.WithObservations(obsDir))
	if err != nil {
		t.Fatal(err)
	}

	step, err := d.Step(10)
	if err != nil {
		t.Fatalf("the loop must reach a deliberate exit: %v", err)
	}
	if step.Outcome != loop.Submitted || step.Subject != subject {
		t.Fatalf("the loop must submit on the offered contract: %s %s", step.Outcome, step.Subject)
	}
	if worked != 1 {
		t.Fatalf("the work step runs exactly once per iteration, ran %d", worked)
	}

	// The window closed on a submission, which the ledger's own read
	// reports rather than the loop's memory of what it did.
	e, code := runEnv(t, "situation", "--remote", remote, "--state", state,
		"--key", workerKey, "--subject", subject)
	if code != 0 {
		t.Fatalf("situation after the loop: %d %+v", code, e)
	}
	windows, _ := e.Result["windows"].([]any)
	if len(windows) != 0 {
		t.Fatalf("a submission closes the window, so none is held: %+v", windows)
	}

	// Liveness rode the work: the stream advanced under this lane's
	// own actor, keyed by the fence of the window it held.
	entries, err := filepath.Glob(filepath.Join(obsDir, workerFP, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("exactly one per-fence stream under this actor, found %v", entries)
	}
	// Exactly the acts the SHIPPED manifest declares emitted, derived
	// from that manifest rather than restated here. The earlier version
	// of this assertion hard-coded a shorter list copied from a unit
	// fixture, and passed only because a fence bug meant claim take
	// never emitted at all: a drill agreeing with the wrong list is how
	// the bug survived its own test (review finding on #191).
	body := streamFile(t, entries[0])
	m := implementerManifest(t)
	for _, act := range m.ActsThrough {
		want := slices.Contains(m.LivenessFrom, act)
		got := strings.Contains(body, `"step":"`+act+`"`)
		if got != want {
			t.Errorf("act %q: the manifest's liveness_from says emits=%v, the stream says %v: %s",
				act, want, got, body)
		}
	}
	if !strings.Contains(body, `"step":"claim take"`) {
		t.Error("the claim is a declared liveness source and must be observable under the fence it " +
			"opened: a worker is most likely to stall between taking work and starting it")
	}
}

// conformance: AC5 — exhaustion parks, against a real boundary. The
// refusal is produced by the ledger rather than by a double, and the
// packet it lands with carries that refusal's own account.
func TestWorkerLoopParksOnRealBudgetExhaustion(t *testing.T) {
	const subject = "c-2"
	remote, state, workerKey, _ := remoteWorkLedger(t, subject)

	d, err := loop.New(implementerManifest(t), loopVerbs{},
		[]string{"--remote", remote, "--state", state}, workerKey,
		loop.WorkFunc(func(string, loop.Situation) (int, error) {
			return 0, fmt.Errorf("the work step must not run: the reserve refused before it")
		}),
		loop.WithBase("abc1234..abc1234"))
	if err != nil {
		t.Fatal(err)
	}

	// The small class carries 100 units; asking for more is the
	// capacity refusal a worker actually meets. run.started is the
	// executor's spending gate and no key this loop signs with can
	// trip it, which is exactly why the reserve is the worker's.
	step, err := d.Step(101)
	if err != nil {
		t.Fatalf("parking is a deliberate exit, not an error: %v", err)
	}
	if step.Outcome != loop.Parked || step.Subject != subject {
		t.Fatalf("an exhausted reserve must park the window: %s %s", step.Outcome, step.Subject)
	}

	// The refusal exercised is the CAPACITY one, asserted by its own
	// message so it cannot silently become a different refusal. This is
	// the distinction the plan turns on: InjectBudgetClass and a small
	// class produce a reservation-capacity refusal, NOT a refusal from
	// the spending-gate branch, and a drill that did not check would
	// pass while touching a path the worker never walks.
	if step.Step != "budget reserve" {
		t.Fatalf("the worker's exhaustion point is the reserve, parked at %q instead", step.Step)
	}
	if !strings.Contains(step.Cause.Message, "exceeds remaining") {
		t.Fatalf("the refusal must be the capacity one (%q says otherwise): run.started is the executor's "+
			"spending gate and no key this loop signs with can trip it", step.Cause.Message)
	}
	// The residual this drill used to pin is closed (os-d03bde01):
	// exhaustion carries its own code, so a successor branches on
	// "budget_exhausted" instead of reading chain_invalid and
	// concluding the ledger is broken.
	if step.Cause.Code != "budget_exhausted" {
		t.Errorf("capacity exhaustion is the one budget refusal a caller answers by asking "+
			"for less, and it reports as %q: spec/envelope.md allocates exit %d for it",
			step.Cause.Code, envelope.ExitBudgetExhausted)
	}

	// What the DRIVER saw is not what the successor reads. The packet
	// on the chain is, so the code is asserted where a next worker
	// actually meets it: folded out of the admitted claim.parked,
	// verbatim, with the prefix the packet writer puts on it.
	var parked *packet.Packet
	for _, rec := range remoteState(t, remote).records {
		if rec.Event.Verb == "claim.parked" && rec.Event.Subject == subject {
			p, err := packet.FromPayload(subject, rec.Event.Payload)
			if err != nil {
				t.Fatalf("the park's packet must parse: %v", err)
			}
			parked = p
		}
	}
	if parked == nil {
		t.Fatal("the park the driver reports must stand on the chain as an admitted claim.parked")
	}
	if len(parked.Findings) != 1 {
		t.Fatalf("the park records the one thing it tried: %+v", parked.Findings)
	}
	if got := parked.Findings[0].Outcome; !strings.HasPrefix(got, "budget_exhausted: ") {
		t.Errorf("the finding carries the refusal's code VERBATIM, so a successor branches on it "+
			"without parsing prose: %q", got)
	}
	if !strings.Contains(parked.Findings[0].Outcome, "exceeds remaining") {
		t.Errorf("the code replaces nothing: the account travels with it: %q", parked.Findings[0].Outcome)
	}

	e, code := runEnv(t, "situation", "--remote", remote, "--state", state,
		"--key", workerKey, "--subject", subject)
	if code != 0 {
		t.Fatalf("situation after the park: %d %+v", code, e)
	}
	if windows, _ := e.Result["windows"].([]any); len(windows) != 0 {
		t.Fatalf("parking closes the window deliberately: %+v", windows)
	}
}

func streamFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the observation stream: %v", err)
	}
	return string(b)
}
