package loop

// The loop drills (plans/os-abb206c8.md). The point of every case here
// is that it fails when the defense is removed rather than when the
// test is edited: the act gate is derived from a manifest this package
// does not author, and the liveness property is checked against what
// the loop actually invoked rather than against a spelling.

import (
	"crypto/ed25519"
	"encoding/pem"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"os"
	"path/filepath"
	"slices"

	"golang.org/x/crypto/ssh"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
)

// recorder is the verb seam as a double: it records every invocation
// and answers from a scripted table, so a drill can assert on what the
// loop DID rather than on what it meant to do.
type recorder struct {
	calls  [][]string
	answer func(args []string) Result
	// packets holds each invocation's packet body, read WHILE the call
	// is in flight. The loop removes the file as soon as the verb has
	// consumed it, so a drill that read the path afterwards would be
	// asserting against the leak rather than against the packet.
	packets map[string]string
}

func (r *recorder) Run(args ...string) Result {
	r.calls = append(r.calls, args)
	if path := flagValue(args, "--packet"); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			if r.packets == nil {
				r.packets = map[string]string{}
			}
			r.packets[verb(args)] = string(b)
		}
	}
	if r.answer != nil {
		return r.answer(args)
	}
	return Result{Exit: 0, OK: true}
}

// verb is the "<group> <sub>" of one recorded call, or the read's name.
func verb(args []string) string {
	if len(args) >= 2 && !strings.HasPrefix(args[1], "-") {
		return args[0] + " " + args[1]
	}
	if len(args) >= 1 {
		return args[0]
	}
	return ""
}

// argsFor returns the recorded argument vector for a verb, so a drill
// can assert on what an act DECLARED, not merely that it happened.
func (r *recorder) argsFor(want string) ([]string, bool) {
	for _, c := range r.calls {
		if verb(c) == want {
			return c, true
		}
	}
	return nil, false
}

func (r *recorder) verbs() []string {
	var out []string
	for _, c := range r.calls {
		out = append(out, verb(c))
	}
	return out
}

// implementer is the shipped lane's shape: the acts a worker performs
// and the liveness its work steps emit.
func implementer() lane.Manifest {
	return lane.Manifest{
		Lane:         "implementer",
		Summary:      "takes a card to a PR",
		Grants:       []string{keyring.CapClaim},
		OrientsFrom:  "seed situation --remote <repo> --key <key> --since <position>",
		ActsThrough:  []string{"claim take", "budget reserve", "budget settle", "submission make", "claim park"},
		LivenessFrom: []string{"budget settle"},
		Inbox:        "wakes on push, convinces on the read",
		Fragments:    []string{"a.md"},
	}
}

func newDriver(t *testing.T, m lane.Manifest, r *recorder) *Driver {
	t.Helper()
	d, err := New(m, r, []string{"--remote", "/repo"}, testKey(t),
		WorkFunc(func(string, Situation) (int, error) { return 3, nil }),
		WithBase("abc1234..abc1234"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// offers answers a poll with one subject and a situation holding it.
func offers(subject string, override func(args []string) (Result, bool)) func([]string) Result {
	return func(args []string) Result {
		if override != nil {
			if res, ok := override(args); ok {
				return res
			}
		}
		switch verb(args) {
		case "offer list":
			return Result{OK: true, Position: "10", Result: map[string]any{
				"offers": []any{map[string]any{"subject": subject}}}}
		case "situation":
			return Result{OK: true, Position: "11", Result: map[string]any{
				"windows": []any{map[string]any{"subject": subject, "fence": "9"}}}}
		}
		return Result{OK: true, Position: "12"}
	}
}

// conformance: D2 — the loop performs no act its manifest does not
// declare, and the refusal comes BEFORE anything is signed. The gate is
// what makes "the manifest describes the loop" enforced rather than
// coincidental: editing one without the other now fails.
func TestActGateRefusesAnUndeclaredAct(t *testing.T) {
	m := implementer()
	m.ActsThrough = []string{"claim take", "budget reserve", "budget settle", "submission make", "claim park"}
	r := &recorder{}
	d := newDriver(t, m, r)

	// Declared: reaches the seam.
	if _, err := d.Act("claim take", "c-1"); err != nil {
		t.Fatalf("a declared act must reach the seam: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("the declared act must have been invoked once, saw %v", r.verbs())
	}

	// Undeclared: refuses, and NOTHING reached the seam. A gate that
	// let the call through and judged it afterwards would have signed
	// the act it was meant to prevent.
	m2 := implementer()
	m2.ActsThrough = []string{"claim take"}
	r2 := &recorder{}
	d2 := newDriver(t, m2, r2)
	_, err := d2.Act("budget reserve", "c-1")
	if !errors.Is(err, ErrUndeclaredAct) {
		t.Fatalf("an undeclared act must refuse as ErrUndeclaredAct, got %v", err)
	}
	if len(r2.calls) != 0 {
		t.Fatalf("nothing may reach the seam on a refused act, saw %v", r2.verbs())
	}
	if !strings.Contains(err.Error(), "claim take") {
		t.Errorf("the refusal names what the lane DOES act through, so the caller can fix it: %v", err)
	}

	// And a name that is no loop act at all refuses the same way,
	// against the registry rather than against a list kept here.
	if _, err := d.Act("claim yeet", "c-1"); !errors.Is(err, ErrUndeclaredAct) {
		t.Errorf("a name outside the loop-verb registry must refuse: %v", err)
	}
}

// conformance: D4/AC5 — a budget refusal at the worker's spending gate
// parks the claim, and the packet's findings carry the refusal's code
// and message VERBATIM. The drill asserts the exact strings, so a lane
// that paraphrased its boundary would fail here.
func TestExhaustionParksCarryingTheRefusalVerbatim(t *testing.T) {
	// The double answers with the shape the REAL boundary produces,
	// verified against it in cmd/seed's end-to-end drill. A double that
	// invented a friendlier code would teach this package a boundary
	// that does not exist, which is the same vacuity the act gate and
	// the liveness property are written to avoid.
	const code = "chain_invalid"
	const message = "admission refused by rule budget: budget on c-1 refused: amount 100 exceeds remaining 4 of capacity 100 — reservations are checked and decremented at admission, the serialized view (spec/budgets.md)"
	var packetPath string
	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		switch verb(args) {
		case "budget reserve":
			return Result{Exit: 8, OK: false, Code: code, Message: message}, true
		case "claim park":
			for i, a := range args {
				if a == "--packet" && i+1 < len(args) {
					packetPath = args[i+1]
				}
			}
			return Result{OK: true, Position: "13"}, true
		}
		return Result{}, false
	})
	d := newDriver(t, implementer(), r)

	step, err := d.Step(100)
	if err != nil {
		t.Fatalf("parking is a deliberate exit, not an error: %v", err)
	}
	if step.Outcome != Parked || step.Subject != "c-1" {
		t.Fatalf("a refused reserve must park the window: %s %s", step.Outcome, step.Subject)
	}
	if packetPath == "" {
		t.Fatal("the park must carry a packet: every deliberate exit does")
	}
	body := r.packets["claim park"]
	if body == "" {
		t.Fatal("the park's packet was not captured in flight")
	}
	if !strings.Contains(body, code) {
		t.Errorf("the packet's findings must carry the refusal's code %q verbatim: %s", code, body)
	}
	if !strings.Contains(body, message) {
		t.Errorf("the packet's findings must carry the refusal's message verbatim: %s", body)
	}
	if !strings.Contains(body, "budget reserve") {
		t.Errorf("the finding names the step that was tried: %s", body)
	}

	// The window closed, and it closed through the loop verb rather
	// than by abandonment.
	if got := r.verbs(); got[len(got)-1] != "claim park" {
		t.Errorf("the last act must be the deliberate exit, saw %v", got)
	}
}

// conformance: D5 arm 2 — the loop reaches no liveness-only surface.
// Checked as a PROPERTY of what it invoked, not as a spelling: every
// call the loop made is either one of the reads it orients from or an
// act its manifest declares, so there is no path on which it emits
// without working.
func TestLoopReachesNoLivenessOnlySurface(t *testing.T) {
	r := &recorder{}
	m := implementer()
	r.answer = offers("c-1", nil)
	d := newDriver(t, m, r)
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}

	reads := map[string]bool{"offer list": true, "situation": true}
	declared := map[string]bool{}
	for _, a := range m.ActsThrough {
		declared[a] = true
	}
	if len(r.calls) == 0 {
		t.Fatal("this drill is vacuous unless the loop actually ran")
	}
	for _, got := range r.verbs() {
		if reads[got] || declared[got] {
			continue
		}
		t.Errorf("the loop invoked %q, which is neither a read it orients from nor an act %q declares: "+
			"the loop's vocabulary contains NO verb whose only purpose is to report liveness", got, m.Lane)
	}
	// Named explicitly, because it is the one the charter forbids.
	for _, got := range r.verbs() {
		if strings.HasPrefix(got, "obs ") {
			t.Errorf("the loop reached the observation surface directly (%q): liveness rides the work", got)
		}
	}
}

// conformance: the loop orients from ONE position-stamped read and
// carries the position forward as --since, so a resuming lane pays for
// the delta rather than reconstructing the world.
func TestOrientCarriesThePositionForward(t *testing.T) {
	r := &recorder{}
	r.answer = offers("c-1", nil)
	d := newDriver(t, implementer(), r)
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	var situations [][]string
	for _, c := range r.calls {
		if verb(c) == "situation" {
			situations = append(situations, c)
		}
	}
	if len(situations) < 2 {
		t.Fatalf("the loop orients before and after claiming, saw %d reads", len(situations))
	}
	if hasFlag(situations[0], "--since") {
		t.Error("the first read of a fresh loop has no position to resume from")
	}
	if !hasFlag(situations[1], "--since") {
		t.Errorf("the second read carries the first's position forward: %v", situations[1])
	}
	if got := flagValue(situations[1], "--since"); got != "11" {
		t.Errorf("--since must be the position the LAST read stamped, got %q", got)
	}
}

// conformance: construction refuses a lane that cannot run a loop, so
// the failure lands at build time rather than mid-window.
func TestConstructionRefusesALaneThatCannotLoop(t *testing.T) {
	r := &recorder{}
	work := WorkFunc(func(string, Situation) (int, error) { return 0, nil })
	for name, mutate := range map[string]func(*lane.Manifest){
		"a lane declaring no acts":     func(m *lane.Manifest) { m.ActsThrough = nil },
		"a lane declaring no liveness": func(m *lane.Manifest) { m.LivenessFrom = nil },
	} {
		m := implementer()
		mutate(&m)
		if _, err := New(m, r, []string{"--remote", "/r"}, testKey(t), work, WithBase("a..a")); err == nil {
			t.Errorf("%s must refuse at construction", name)
		}
	}
	m := implementer()
	if _, err := New(m, r, []string{"--remote", "/r"}, testKey(t), work); err == nil {
		t.Error("a loop with no resume coordinate must refuse: every deliberate exit carries a packet")
	}
	if _, err := New(m, r, nil, testKey(t), work, WithBase("a..a")); err == nil {
		t.Error("a loop with no posture must refuse: it would have no ledger to orient against")
	}
	if _, err := New(m, r, []string{"--remote", "/r"}, testKey(t), nil, WithBase("a..a")); err == nil {
		t.Error("a loop with no work step is a heartbeat, which the charter forbids")
	}
}

// conformance: a lost claim race is ordinary, not an error. In fleet
// mode two workers racing claim take means the loser re-orients and
// takes different work; treating that as a failure would manufacture an
// escalation storm out of contention.
func TestLostClaimIsIdleNotAnError(t *testing.T) {
	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "claim take" {
			return Result{Exit: 2, OK: false, Code: "contention", Message: "fence 4"}, true
		}
		return Result{}, false
	})
	d := newDriver(t, implementer(), r)
	step, err := d.Step(5)
	if err != nil {
		t.Fatalf("losing a race is ordinary lane behavior: %v", err)
	}
	if step.Outcome != Idle {
		t.Fatalf("a lost claim leaves no window to exit, so the iteration is idle: %s", step.Outcome)
	}
	for _, got := range r.verbs() {
		if got == "claim park" {
			t.Error("nothing was claimed, so nothing is owed a deliberate exit")
		}
	}
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// conformance: D5 arm 1 / AC7 — running the loop's declared liveness
// steps ADVANCES the observation stream keyed to the lane's actor and
// fence. This is the half 1a could not establish: it compared two
// labels and said so. Here the stream is sampled exactly as
// internal/obs keys it, so a stream written under the wrong actor or
// the wrong fence fails rather than passing on a technicality.
func TestLivenessRidesTheWorkAndIsKeyedToActorAndFence(t *testing.T) {
	const fence = "9"
	obsDir := t.TempDir()
	r := &recorder{answer: offers("c-1", nil)}
	m := implementer()
	keyPath, actor := testKeyAndFingerprint(t)
	d, err := New(m, r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 3, nil }),
		WithBase("abc1234..abc1234"), WithObservations(obsDir))
	if err != nil {
		t.Fatal(err)
	}

	before := streamLines(t, obsDir, actor, fence)
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	after := streamLines(t, obsDir, actor, fence)
	if after <= before {
		t.Fatalf("running the loop must advance the stream at %s/%s/%s.jsonl: %d then %d",
			obsDir, actor, fence, before, after)
	}

	// Keyed to THIS lane's actor and fence, and nothing else. A stream
	// under another key is invisible to the classifier, so writing one
	// would look like liveness here while being silence to the reaper.
	if n := streamLines(t, obsDir, actor, "0"); n != 0 {
		t.Errorf("the stream is keyed by the window's fence, found %d lines under fence 0", n)
	}
	if n := streamLines(t, obsDir, "SHA256:someone-else", fence); n != 0 {
		t.Errorf("the stream is keyed by the lane's own actor, found %d lines under another", n)
	}

	// And exactly the DECLARED steps emit. budget settle is the
	// implementer's liveness source; claim take and submission make are
	// acts it performs without being liveness sources, so a loop
	// emitting per-act rather than per-declared-act would overcount.
	lines := streamBody(t, obsDir, actor, fence)
	for _, a := range m.ActsThrough {
		want := slices.Contains(m.LivenessFrom, a)
		if got := strings.Contains(lines, `"step":"`+a+`"`); got != want {
			t.Errorf("act %q: liveness_from says emits=%v, the stream says %v: %s", a, want, got, lines)
		}
	}
}

// conformance: a REFUSED act emits nothing. Otherwise a lane wedged at
// a boundary would look busiest exactly when it is most stuck, and the
// classification the maintenance reap depends on would be reading
// failure as progress.
func TestARefusedActEmitsNoLiveness(t *testing.T) {
	obsDir := t.TempDir()
	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "budget settle" {
			return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "no open reservation"}, true
		}
		return Result{}, false
	})
	m := implementer()
	keyPath, actor := testKeyAndFingerprint(t)
	d, err := New(m, r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }),
		WithBase("a..a"), WithObservations(obsDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	if n := streamLines(t, obsDir, actor, "9"); n != 0 {
		t.Errorf("the only liveness source refused, so the stream must hold nothing: %d lines", n)
	}
}

func streamBody(t *testing.T, dir, actor, fence string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, actor, fence+".jsonl"))
	if err != nil {
		return ""
	}
	return string(b)
}

func streamLines(t *testing.T, dir, actor, fence string) int {
	t.Helper()
	body := strings.TrimSpace(streamBody(t, dir, actor, fence))
	if body == "" {
		return 0
	}
	return len(strings.Split(body, "\n"))
}

// conformance: every path out of an OPEN window is a deliberate exit.
// The loop must never return leaving a claim standing, whichever step
// stopped it, because an abandoned window is what makes an expiry
// ambiguous: silence then means either dead work or a forgetful worker,
// and the maintenance reap cannot tell them apart.
func TestEveryFailureAfterTheClaimExitsDeliberately(t *testing.T) {
	for name, tc := range map[string]struct {
		fail string
		work func(string, Situation) (int, error)
	}{
		"the work step itself fails": {
			work: func(string, Situation) (int, error) { return 2, errors.New("the build did not converge") },
		},
		"the settle refuses":     {fail: "budget settle"},
		"the submission refuses": {fail: "submission make"},
	} {
		r := &recorder{}
		r.answer = offers("c-1", func(args []string) (Result, bool) {
			if tc.fail != "" && verb(args) == tc.fail {
				return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "refused at " + tc.fail}, true
			}
			return Result{}, false
		})
		work := tc.work
		if work == nil {
			work = func(string, Situation) (int, error) { return 1, nil }
		}
		d, err := New(implementer(), r, []string{"--remote", "/repo"}, testKey(t),
			WorkFunc(work), WithBase("a..a"))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil {
			t.Errorf("%s: parking is the deliberate exit, not an error: %v", name, err)
			continue
		}
		if step.Outcome != Parked {
			t.Errorf("%s: the window was open, so it must be parked: %s", name, step.Outcome)
		}
		if got := r.verbs(); got[len(got)-1] != "claim park" {
			t.Errorf("%s: the last act must be the deliberate exit, saw %v", name, got)
		}
	}
}

// conformance: a settle runs before ANY exit, including a failed one. A
// reservation nobody closes comes out of the next claimant's remaining,
// and that claimant is a different worker: a failed attempt must not
// leave the retry quietly poorer than the first.
func TestAFailedWorkStepStillSettlesItsReservation(t *testing.T) {
	r := &recorder{answer: offers("c-1", nil)}
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, testKey(t),
		WorkFunc(func(string, Situation) (int, error) { return 4, errors.New("no") }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	var settled []string
	for _, c := range r.calls {
		if verb(c) == "budget settle" {
			settled = c
		}
	}
	if settled == nil {
		t.Fatal("a reservation must be closed even when the work failed")
	}
	if got := flagValue(settled, "--actuals"); got != "4" {
		t.Errorf("the settle records what the step actually cost, got %q", got)
	}
	if i := slices.Index(r.verbs(), "budget settle"); i > slices.Index(r.verbs(), "claim park") {
		t.Error("the settle comes before the exit, not after it")
	}
}

// conformance: a park that itself refuses is an ERROR, not a quiet
// return. The window is still open and the lane knows it, so the one
// thing it must not do is report a tidy outcome.
func TestAParkThatRefusesIsAnError(t *testing.T) {
	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		switch verb(args) {
		case "budget reserve":
			return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "exhausted"}, true
		case "claim park":
			return Result{Exit: 6, OK: false, Code: "fenced_out", Message: "fence 9 is not current"}, true
		}
		return Result{}, false
	})
	d := newDriver(t, implementer(), r)
	step, err := d.Step(5)
	if err == nil {
		t.Fatal("a window that could not be parked must not report success")
	}
	if !strings.Contains(err.Error(), "exhausted") || !strings.Contains(err.Error(), "fence 9") {
		t.Errorf("the error names BOTH refusals: what stopped the work and what stopped the exit: %v", err)
	}
	if step.Outcome != Parked || step.Step != "budget reserve" {
		t.Errorf("the result still reports what was attempted: %+v", step)
	}
}

// conformance: a refused poll or orient is an error rather than an idle
// tick. A lane that cannot read cannot honestly conclude there is no
// work, and reporting idle would turn a broken ledger into a quiet loop.
func TestARefusedReadIsNotAnIdleTick(t *testing.T) {
	for name, failing := range map[string]string{"poll": "offer list", "orient": "situation"} {
		r := &recorder{}
		r.answer = offers("c-1", func(args []string) (Result, bool) {
			if verb(args) == failing {
				return Result{Exit: 5, OK: false, Code: "unavailable", Message: "the ledger is unreachable"}, true
			}
			return Result{}, false
		})
		d := newDriver(t, implementer(), r)
		step, err := d.Step(5)
		if err == nil {
			t.Errorf("%s: a refused read must not pass for an empty queue", name)
		}
		if step.Outcome != Idle {
			t.Errorf("%s: nothing was claimed, so nothing is owed an exit: %s", name, step.Outcome)
		}
		for _, got := range r.verbs() {
			if strings.HasPrefix(got, "claim ") {
				t.Errorf("%s: the loop must not act on a read it could not make: %v", name, r.verbs())
			}
		}
	}
}

// conformance: Situation is what the read said, and its accessors
// report exactly that. Holds is how the loop knows its claim landed
// without inferring it from the claim's own exit code.
func TestSituationReportsWhatTheReadSaid(t *testing.T) {
	s := Situation{Windows: []map[string]any{
		{"subject": "c-1", "fence": "9", "acceptance": "accept.md @ abc1234"},
		{"subject": "c-2", "fence": "3"},
	}}
	if !s.Holds("c-1") || !s.Holds("c-2") {
		t.Error("a window in the read is a window held")
	}
	if s.Holds("c-9") {
		t.Error("a subject with no window is not held")
	}
	if got := s.Acceptance("c-1"); got != "accept.md @ abc1234" {
		t.Errorf("the acceptance anchor comes from the read: %q", got)
	}
	if got := s.Acceptance("c-2"); got != "" {
		t.Errorf("a window without an anchor reports none rather than guessing: %q", got)
	}
	if got := (Situation{}).Acceptance("c-1"); got != "" {
		t.Errorf("an empty read reports no anchor: %q", got)
	}
}

// conformance: a resuming lane starts from the position it was given,
// so its first read pays for the delta rather than the world.
func TestWithSinceResumesFromTheGivenPosition(t *testing.T) {
	r := &recorder{answer: offers("c-1", nil)}
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, testKey(t),
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }),
		WithBase("a..a"), WithSince("42"))
	if err != nil {
		t.Fatal(err)
	}
	if _, res := d.Orient("c-1"); res.Refused() {
		t.Fatal("orient refused")
	}
	if got := flagValue(r.calls[0], "--since"); got != "42" {
		t.Errorf("a primed loop resumes from its given position, sent --since %q", got)
	}
}

// conformance: given a repo, the exit packet's resume range is derived
// by the loop verbs rather than stated by the loop, which is what keeps
// it right as the work advances.
func TestWithRepoDefersTheRangeToTheLoopVerbs(t *testing.T) {
	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "budget reserve" {
			return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "exhausted"}, true
		}
		return Result{}, false
	})
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, testKey(t),
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithRepo("/work"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	var park []string
	for _, c := range r.calls {
		if verb(c) == "claim park" {
			park = c
		}
	}
	if got := flagValue(park, "--repo"); got != "/work" {
		t.Errorf("the park names the repo the range is derived from, got %q", got)
	}
	if hasFlag(park, "--base") {
		t.Error("a loop given a repo must not also state a base: the verbs derive it")
	}
	body := r.packets["claim park"]
	if strings.Contains(body, `"base"`) {
		t.Errorf("the packet leaves base to the derivation: %s", body)
	}
}

// conformance: a refusal with no account still produces a packet whose
// finding says something a successor can use. An empty outcome would
// fail packet validation, and a lane that crashed there would leave the
// window it was trying to close standing open.
func TestARefusalWithoutAnAccountStillYieldsAFinding(t *testing.T) {
	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "budget reserve" {
			return Result{Exit: 1, OK: false}, true
		}
		return Result{}, false
	})
	d := newDriver(t, implementer(), r)
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.calls {
		if verb(c) == "claim park" {
			body := r.packets["claim park"]
			if !strings.Contains(body, "refused without an account") {
				t.Errorf("a silent refusal is itself the finding: %s", body)
			}
			return
		}
	}
	t.Fatal("the window must still be parked")
}

// conformance: an empty queue is an idle tick with no window opened and
// nothing owed an exit. This is the common case in a quiet fleet, and a
// loop that treated it as an error would turn a healthy idle system
// into an alarm.
func TestAnEmptyQueueIsIdle(t *testing.T) {
	r := &recorder{answer: func(args []string) Result {
		if verb(args) == "offer list" {
			return Result{OK: true, Position: "10", Result: map[string]any{"offers": []any{}}}
		}
		return Result{OK: true, Position: "10"}
	}}
	d := newDriver(t, implementer(), r)
	step, err := d.Step(5)
	if err != nil {
		t.Fatalf("an empty queue is not a failure: %v", err)
	}
	if step.Outcome != Idle || step.Subject != "" {
		t.Fatalf("nothing was offered, so nothing was claimed: %+v", step)
	}
	if got := r.verbs(); len(got) != 1 {
		t.Errorf("a loop with nothing to claim reads once and stops, saw %v", got)
	}
}

// conformance: a lane whose read never reported a window still writes a
// packet with an acceptance part, because a packet without one fails
// validation and would leave the window it was closing standing open.
// The honest value says the anchor is unread, which is itself the
// successor's first problem.
func TestAPacketWithoutAReadAnchorSaysSo(t *testing.T) {
	r := &recorder{}
	r.answer = func(args []string) Result {
		switch verb(args) {
		case "offer list":
			return Result{OK: true, Position: "10", Result: map[string]any{
				"offers": []any{map[string]any{"subject": "c-1"}}}}
		case "situation":
			// A window WITHOUT an acceptance field: the lane holds the
			// claim and can see it, but the read does not report what it
			// is judged against. (A read with no window at all is a
			// different case entirely — the claim is gone, and the loop
			// re-orients rather than parking.)
			return Result{OK: true, Position: "11", Result: map[string]any{
				"windows": []any{map[string]any{"subject": "c-1", "fence": "9"}}}}
		case "budget reserve":
			return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "exhausted"}
		}
		return Result{OK: true, Position: "12"}
	}
	d := newDriver(t, implementer(), r)
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.calls {
		if verb(c) == "claim park" {
			body := r.packets["claim park"]
			if !strings.Contains(body, "did not report") {
				t.Errorf("the packet must say the anchor was unread rather than invent one: %s", body)
			}
			return
		}
	}
	t.Fatal("the window must be parked")
}

// conformance: a work step reporting negative units settles zero rather
// than a negative, which would make the budget view read as if capacity
// had been returned that was never spent.
func TestNegativeUnitsSettleAsZero(t *testing.T) {
	r := &recorder{answer: offers("c-1", nil)}
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, testKey(t),
		WorkFunc(func(string, Situation) (int, error) { return -5, nil }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.calls {
		if verb(c) == "budget settle" {
			if got := flagValue(c, "--actuals"); got != "0" {
				t.Errorf("negative units settle as zero, got %q", got)
			}
			return
		}
	}
	t.Fatal("the reservation must be settled")
}

// conformance: the verb seam is required, and the loop-verb registry is
// the authority for what a group and subverb resolve to, so an act's
// argv is built from the registry rather than from a string split here.
func TestActArgvComesFromTheRegistry(t *testing.T) {
	r := &recorder{}
	keyPath, _ := testKeyAndFingerprint(t)
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Act("budget reserve", "c-1", "--amount", "5"); err != nil {
		t.Fatal(err)
	}
	got := r.calls[0]
	if got[0] != "budget" || got[1] != "reserve" {
		t.Fatalf("the group and subverb come from the registry: %v", got)
	}
	if flagValue(got, "--subject") != "c-1" || flagValue(got, "--key") != keyPath {
		t.Errorf("every act carries its subject and the lane's one key: %v", got)
	}
	if flagValue(got, "--remote") != "/repo" {
		t.Errorf("every act runs in the loop's posture: %v", got)
	}
	if flagValue(got, "--amount") != "5" {
		t.Errorf("per-verb arguments pass through: %v", got)
	}
}

// conformance: a loop with no verb seam refuses at construction. It is
// the one dependency that cannot be defaulted: a loop that could not
// act would poll, orient, and then silently do nothing, which is the
// shape of a lane that looks alive and accomplishes nothing.
func TestALoopWithNoSeamRefuses(t *testing.T) {
	_, err := New(implementer(), nil, []string{"--remote", "/r"}, testKey(t),
		WorkFunc(func(string, Situation) (int, error) { return 0, nil }), WithBase("a..a"))
	if err == nil {
		t.Fatal("a loop with no verb seam can perform no act and must refuse")
	}
	if !strings.Contains(err.Error(), "no verb seam") {
		t.Errorf("the refusal says which dependency is missing: %v", err)
	}
}

// conformance: a packet that cannot be WRITTEN must not be reported as
// a park. The window is still open, and a loop that swallowed the write
// failure would leave a claim standing while believing it had exited
// deliberately — the exact ambiguity the four deliberate exits exist to
// remove.
func TestAPacketThatCannotBeWrittenIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows, so an unwritable directory is not observable there (spec/platform.md)")
	}
	// A TMPDIR that is not a directory: os.CreateTemp fails, and there
	// is no packet to name.
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", notADir)

	r := &recorder{}
	r.answer = offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "budget reserve" {
			return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "exhausted"}, true
		}
		return Result{}, false
	})
	d := newDriver(t, implementer(), r)
	step, err := d.Step(5)
	if err == nil {
		t.Fatal("a park whose packet could not be written must not report success")
	}
	if step.Outcome != Parked || step.Step != "budget reserve" {
		t.Errorf("the result still reports what was attempted: %+v", step)
	}
	for _, got := range r.verbs() {
		if got == "claim park" {
			t.Error("no park may be attempted without the packet it is required to carry")
		}
	}
}

// conformance: the act gate guards EVERY step, not only the first. A
// manifest that declares the claim but not the acts the loop needs
// afterwards fails at the step that needs them, which is the honest
// place: the lane really can claim, and really cannot meter, and the
// error says which.
func TestTheActGateGuardsEveryStepOfTheSequence(t *testing.T) {
	full := []string{"claim take", "budget reserve", "budget settle", "submission make", "claim park"}
	for _, missing := range []string{"claim take", "budget reserve", "budget settle", "submission make"} {
		m := implementer()
		m.ActsThrough = nil
		for _, a := range full {
			if a != missing {
				m.ActsThrough = append(m.ActsThrough, a)
			}
		}
		if missing == "budget settle" {
			// liveness_from must name an act the lane still performs,
			// or construction refuses for a different reason entirely.
			m.LivenessFrom = []string{"budget reserve"}
		}
		r := &recorder{answer: offers("c-1", nil)}
		d, err := New(m, r, []string{"--remote", "/repo"}, testKey(t),
			WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithBase("a..a"))
		if err != nil {
			t.Fatalf("%s: construction: %v", missing, err)
		}
		step, err := d.Step(5)
		if !errors.Is(err, ErrUndeclaredAct) {
			t.Errorf("a lane not declaring %q must fail at the step needing it, got %v", missing, err)
			continue
		}
		if step.Step != missing {
			t.Errorf("the result names the step that refused: want %q, got %q", missing, step.Step)
		}
		for _, got := range r.verbs() {
			if got == missing {
				t.Errorf("%s: the undeclared act must never reach the seam: %v", missing, r.verbs())
			}
		}
	}
}

// conformance: an orient that refuses AFTER the claim landed parks the
// window rather than returning. The lane holds a claim it can no longer
// see, which is precisely when abandoning it would be worst.
func TestARefusedOrientAfterTheClaimParks(t *testing.T) {
	orients := 0
	r := &recorder{}
	r.answer = func(args []string) Result {
		switch verb(args) {
		case "offer list":
			return Result{OK: true, Position: "10", Result: map[string]any{
				"offers": []any{map[string]any{"subject": "c-1"}}}}
		case "situation":
			orients++
			if orients > 1 {
				return Result{Exit: 5, OK: false, Code: "unavailable", Message: "the ledger went away"}
			}
			return Result{OK: true, Position: "11", Result: map[string]any{"windows": []any{}}}
		}
		return Result{OK: true, Position: "12"}
	}
	d := newDriver(t, implementer(), r)
	step, err := d.Step(5)
	if err != nil {
		t.Fatalf("parking is the deliberate exit: %v", err)
	}
	if step.Outcome != Parked || step.Step != "orient" {
		t.Fatalf("a refused post-claim orient must park, naming the step: %+v", step)
	}
	if got := r.verbs(); got[len(got)-1] != "claim park" {
		t.Errorf("the window must close through the loop verb: %v", got)
	}
}

// conformance: the lane's identity is DERIVED from its signing key and
// cannot be supplied beside it. A loop told to poll as one actor while
// signing as another would select work under one identity's
// eligibility, act under a second, and write liveness under a third —
// and the classifier, which keys the stream by the holder, would read
// silence from a worker that was working (review finding on #191).
//
// The drill is that the two agree by CONSTRUCTION: there is no
// parameter to disagree through, and the fingerprint the loop polls
// with is the one its key produces.
func TestTheActorIsDerivedFromTheSigningKey(t *testing.T) {
	r := &recorder{answer: offers("c-1", nil)}
	keyPath, want := testKeyAndFingerprint(t)
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, res := d.Poll(); res.Refused() {
		t.Fatal("poll refused")
	}
	if got := flagValue(r.calls[0], "--actor"); got != want {
		t.Errorf("the poll's actor must be the signing key's fingerprint: polled as %q, key is %q",
			got, want)
	}
	// The poll is eligibility-scoped by fingerprint and takes no key;
	// the acts sign with one. The pairing that matters is that the
	// fingerprint above came from the key below, which is what
	// derivation guarantees and a supplied actor could not.
	if _, err := d.Act("claim take", "c-1"); err != nil {
		t.Fatal(err)
	}
	act := r.calls[len(r.calls)-1]
	if flagValue(act, "--key") != keyPath {
		t.Errorf("the acts sign with the key the actor was derived from: %v", act)
	}

	// And a key the loop cannot read or parse refuses at construction,
	// rather than later, mid-window.
	work := WorkFunc(func(string, Situation) (int, error) { return 0, nil })
	if _, err := New(implementer(), r, []string{"--remote", "/r"}, "", work, WithBase("a..a")); err == nil {
		t.Error("a loop with no key must refuse")
	}
	if _, err := New(implementer(), r, []string{"--remote", "/r"},
		filepath.Join(t.TempDir(), "absent"), work, WithBase("a..a")); err == nil {
		t.Error("a key the loop cannot read must refuse at construction")
	}
	junk := filepath.Join(t.TempDir(), "junk")
	if err := os.WriteFile(junk, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(implementer(), r, []string{"--remote", "/r"}, junk, work, WithBase("a..a")); err == nil {
		t.Error("a key the loop cannot parse must refuse at construction")
	}
}

// conformance: a malformed window row is skipped rather than crashing
// the read. The situation surface is trusted to be well-formed, but a
// lane that panicked on one bad row would take an ordinary schema
// change as a total outage.
func TestAMalformedWindowRowIsSkipped(t *testing.T) {
	r := &recorder{answer: func(args []string) Result {
		if verb(args) == "situation" {
			return Result{OK: true, Position: "11", Result: map[string]any{
				"windows": []any{"not a row", map[string]any{"subject": "c-1", "fence": "9"}}}}
		}
		return Result{OK: true, Position: "10"}
	}}
	d := newDriver(t, implementer(), r)
	s, res := d.Orient("c-1")
	if res.Refused() {
		t.Fatal("one bad row must not refuse the whole read")
	}
	if len(s.Windows) != 1 || !s.Holds("c-1") {
		t.Fatalf("the well-formed rows survive: %+v", s.Windows)
	}
}

// testKey writes a deterministic OpenSSH ed25519 private key and
// returns its path. Drills sign with a real key because the loop's
// identity is derived from one: a fixture that let a test name an
// arbitrary actor would re-open exactly the gap that derivation closed.
func testKey(t *testing.T) string {
	t.Helper()
	path, _ := testKeyAndFingerprint(t)
	return path
}

func testKeyAndFingerprint(t *testing.T) (string, string) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 3)
	}
	k := ed25519.NewKeyFromSeed(seed)
	block, err := ssh.MarshalPrivateKey(k, "loop-fixture")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return path, fp
}

// conformance: a claim reaped between the take's push and the refreshed
// read leaves nothing to exit. The build plan's middle convergence arm
// is exactly this — a refreshed position-stamped read showing the act is
// no longer owed — and treating it as a failure would manufacture an
// escalation storm out of ordinary fleet contention (review finding on
// #191).
func TestAClaimReapedBeforeTheReadIsIdleNotAnError(t *testing.T) {
	orients := 0
	r := &recorder{}
	r.answer = func(args []string) Result {
		switch verb(args) {
		case "offer list":
			return Result{OK: true, Position: "10", Result: map[string]any{
				"offers": []any{map[string]any{"subject": "c-1"}}}}
		case "situation":
			orients++
			// The post-claim read shows no window: someone reaped it.
			return Result{OK: true, Position: "11", Result: map[string]any{"windows": []any{}}}
		case "claim take":
			return Result{OK: true, Position: "12", Result: map[string]any{"fence": "12"}}
		}
		return Result{OK: true, Position: "13"}
	}
	d := newDriver(t, implementer(), r)
	step, err := d.Step(5)
	if err != nil {
		t.Fatalf("a reaped claim is ordinary contention, not an error: %v", err)
	}
	if step.Outcome != Idle {
		t.Fatalf("the refreshed read proved the act is no longer owed, so the iteration is idle: %+v", step)
	}
	if orients < 2 {
		t.Fatal("this drill is vacuous unless the post-claim read actually happened")
	}
	for _, got := range r.verbs() {
		if got == "budget reserve" || got == "claim park" {
			t.Errorf("nothing is held, so nothing may be spent against it or exited: %v", r.verbs())
		}
	}
}

// conformance: claim take is a declared liveness source in the shipped
// implementer manifest, and it must be observable under the fence IT
// opened. Emitting under the previous window's fence, or dropping it for
// want of one, leaves the claim's own liveness invisible to the
// classifier exactly when a worker is most likely to stall — between
// taking work and starting it (review finding on #191).
func TestTheClaimIsObservableUnderTheFenceItOpened(t *testing.T) {
	obsDir := t.TempDir()
	keyPath, actor := testKeyAndFingerprint(t)
	const opened = "12"
	orients := 0
	r := &recorder{}
	r.answer = func(args []string) Result {
		switch verb(args) {
		case "offer list":
			return Result{OK: true, Position: "10", Result: map[string]any{
				"offers": []any{map[string]any{"subject": "c-1"}}}}
		case "claim take":
			return Result{OK: true, Position: opened, Result: map[string]any{
				"subject": "c-1", "fence": opened}}
		case "situation":
			orients++
			if orients == 1 {
				// The PRE-claim read holds no window, because there is
				// none yet. This is what makes the drill non-vacuous:
				// if the fence could only come from a read, it would be
				// empty at claim time and the claim's own liveness would
				// be dropped. The first version of this drill returned a
				// window here and passed without the fix.
				return Result{OK: true, Position: "11", Result: map[string]any{"windows": []any{}}}
			}
			return Result{OK: true, Position: "13", Result: map[string]any{
				"windows": []any{map[string]any{"subject": "c-1", "fence": opened}}}}
		}
		return Result{OK: true, Position: "14"}
	}
	m := implementer()
	m.LivenessFrom = []string{"claim take", "budget settle"}
	d, err := New(m, r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }),
		WithBase("a..a"), WithObservations(obsDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(5); err != nil {
		t.Fatal(err)
	}
	body := streamBody(t, obsDir, actor, opened)
	if !strings.Contains(body, `"step":"claim take"`) {
		t.Errorf("the claim must be observable under the fence it opened (%s/%s/%s.jsonl): %q",
			obsDir, actor, opened, body)
	}
	if n := streamLines(t, obsDir, actor, ""); n != 0 {
		t.Errorf("no observation may land under an empty fence key: %d lines", n)
	}
}

// conformance: the packet a verb consumed is removed once it has. An
// unattended lane runs indefinitely, so one file left per iteration is a
// slow leak of the host's temporary storage and of the packets
// themselves, which are work-product content rather than scratch
// (review finding on #191).
func TestPacketFilesDoNotOutliveTheirVerb(t *testing.T) {
	for name, answer := range map[string]func([]string) (Result, bool){
		"a submission": nil,
		"a park": func(args []string) (Result, bool) {
			if verb(args) == "budget reserve" {
				return Result{Exit: 8, OK: false, Code: "chain_invalid", Message: "exhausted"}, true
			}
			return Result{}, false
		},
	} {
		r := &recorder{answer: offers("c-1", answer)}
		d := newDriver(t, implementer(), r)
		if _, err := d.Step(5); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		named := 0
		for _, c := range r.calls {
			path := flagValue(c, "--packet")
			if path == "" {
				continue
			}
			named++
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("%s: %s outlived the verb that consumed it (%v)", name, path, err)
			}
		}
		if named == 0 {
			t.Errorf("%s: this drill is vacuous unless a packet was actually written", name)
		}
	}
}

// conformance: a key rotated under a running Driver refuses at the next
// iteration rather than being adopted. Deriving the actor at
// construction closes the mismatch a parameter could carry; it does not
// close the one the filesystem can, because the loop passes --key
// <path> and the CLI signs with whatever that path holds now (review
// finding on #193).
//
// The refusal is the point, not the detection: silently switching
// identity mid-loop would leave a window held by one actor and worked
// by another, which is a worse state than stopping.
func TestARotatedKeyRefusesRatherThanBeingAdopted(t *testing.T) {
	r := &recorder{answer: offers("c-1", nil)}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	writeSeededKey(t, keyPath, 3)
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	// One clean iteration, so the drill is not passing because nothing
	// ever worked.
	if _, err := d.Step(5); err != nil {
		t.Fatalf("the first iteration must run: %v", err)
	}
	before := len(r.calls)

	// Rotate: the same path, different key material.
	writeSeededKey(t, keyPath, 9)
	step, err := d.Step(5)
	if !errors.Is(err, ErrKeyRotated) {
		t.Fatalf("a rotated key must refuse as ErrKeyRotated, got %v", err)
	}
	if step.Outcome != Idle || step.Step != "identity" {
		t.Errorf("the refusal names the identity check and claims nothing: %+v", step)
	}
	if len(r.calls) != before {
		t.Errorf("the refusal comes BEFORE polling: %d calls made after rotation", len(r.calls)-before)
	}
	if !strings.Contains(err.Error(), "restart the loop") {
		t.Errorf("the refusal tells the operator what to do about it: %v", err)
	}

	// The caller-driven path refuses too: stepping by hand is not
	// permission to sign under a key the loop never derived from.
	if _, err := d.Act("claim take", "c-1"); !errors.Is(err, ErrKeyRotated) {
		t.Errorf("Act must apply the same check, got %v", err)
	}
}

// conformance: a packet created whose write then fails is unlinked by
// writePacket itself. No verb will run and no caller holds a path, so
// cleanup after the call cannot reach it — and this is precisely the
// low-storage case, where leaking leaks when the host can least afford
// it (review finding on #193).
func TestAPacketCreatedThenFailedIsUnlinked(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	r := &recorder{}
	d := newDriver(t, implementer(), r)

	// The failure is INJECTED, so the branch is genuinely reached. The
	// first version of this drill ran a successful writePacket and then
	// called the success-path cleanup, which left both error-branch
	// unlinks unprotected: removing them kept it green (review finding
	// on #194). A drill for a failure path has to fail.
	body := map[string]any{
		"acceptance": []string{"x"}, "decisions": []any{},
		"refs": []string{}, "findings": []map[string]string{},
	}
	for name, mode := range map[string]string{
		"the write fails": "write",
		"the close fails": "close",
	} {
		var created string
		restore := InjectPacketWriter(func() (packetFile, error) {
			f, err := os.CreateTemp(tmp, "seed-loop-packet-*.json")
			if err != nil {
				return nil, err
			}
			created = f.Name()
			return &faultyPacketFile{File: f, mode: mode}, nil
		})
		_, _, err := d.writePacket(body)
		restore()
		if err == nil {
			t.Errorf("%s: writePacket must refuse", name)
			continue
		}
		if created == "" {
			t.Errorf("%s: the drill needs the file to have been created first", name)
			continue
		}
		if _, statErr := os.Stat(created); !os.IsNotExist(statErr) {
			t.Errorf("%s: writePacket must unlink the file it created (%s): %v", name, created, statErr)
		}
	}

	// Nothing left behind at all: a leaked file here has no path anyone
	// still holds, which is why the sweep is by directory.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "seed-loop-packet-") {
			t.Errorf("a packet file was left behind: %s", e.Name())
		}
	}

	// The success path still cleans up through its returned closer.
	args, cleanup, err := d.writePacket(body)
	if err != nil {
		t.Fatal(err)
	}
	ok := flagValue(args, "--packet")
	if _, err := os.Stat(ok); err != nil {
		t.Fatalf("the packet exists until its cleanup runs: %v", err)
	}
	cleanup()
	if _, err := os.Stat(ok); !os.IsNotExist(err) {
		t.Errorf("cleanup must unlink the packet: %v", err)
	}

	// A body that cannot be marshalled fails before any file exists, so
	// there is nothing to unlink and nothing to leak.
	if _, _, err := d.writePacket(map[string]any{"bad": make(chan int)}); err == nil {
		t.Error("an unmarshallable packet must refuse")
	}
}

// faultyPacketFile fails deterministically at a named point after the
// file exists, which is the only way to reach writePacket's
// post-creation unlinks.
type faultyPacketFile struct {
	*os.File
	mode string
}

func (f *faultyPacketFile) Write(b []byte) (int, error) {
	if f.mode == "write" {
		return 0, errors.New("injected write failure")
	}
	return f.File.Write(b)
}

func (f *faultyPacketFile) Close() error {
	if f.mode == "close" {
		_ = f.File.Close()
		return errors.New("injected close failure")
	}
	return f.File.Close()
}

// writeSeededKey writes a deterministic OpenSSH ed25519 key at path.
func writeSeededKey(t *testing.T, path string, first byte) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i) + first
	}
	block, err := ssh.MarshalPrivateKey(ed25519.NewKeyFromSeed(seed), "loop-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
}

// conformance: a key rotated DURING the work step stops the iteration
// at the next act, rather than letting a new identity settle or exit a
// window the old one opened (review finding on #194). The single check
// at the top of Step could not see this: Work.Do is the longest thing
// in an iteration and the likeliest moment for a rotation to land.
//
// The outcome asserted is deliberately not "it recovers": a window
// opened by one actor and closed by another, or left open because the
// close refused, is exactly the state the deliberate exits exist to
// make impossible. Stopping is the correct behavior.
func TestAKeyRotatedDuringTheWorkStepStopsAtTheNextAct(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	writeSeededKey(t, keyPath, 3)

	// The double refuses the exit the way the real boundary would: the
	// fence rule admits holder-signed events only from the holder, so a
	// rotated key cannot park the window its predecessor opened. A
	// double that let the park succeed would test a case production
	// never reaches.
	r := &recorder{answer: offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "claim park" {
			return Result{Exit: 6, OK: false, Code: "fenced_out",
				Message: "event on c-1 must cite the active fence 12 held by the original actor"}, true
		}
		return Result{}, false
	})}
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) {
			// The rotation lands mid-iteration, after claim take has
			// already opened a window under the original identity.
			writeSeededKey(t, keyPath, 9)
			return 4, nil
		}), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	step, err := d.Step(5)
	if !errors.Is(err, ErrKeyRotated) {
		t.Fatalf("the act after the work step must refuse as ErrKeyRotated, got %v", err)
	}
	if step.Step != "budget settle" {
		t.Errorf("the refusal names the act it stopped at: %+v", step)
	}

	// The window was opened, and the loop ATTEMPTED its deliberate exit
	// rather than returning with a claim standing. The first version of
	// this drill asserted the opposite — that no park reached the seam —
	// which encoded silent abandonment as correct behavior (review
	// finding on #196). It is not: a window left open with no packet is
	// exactly what the four deliberate exits exist to prevent.
	got := r.verbs()
	if !slices.Contains(got, "claim take") {
		t.Fatalf("this drill is vacuous unless the window was actually opened: %v", got)
	}
	if !slices.Contains(got, "claim park") {
		t.Errorf("an open window must have its exit ATTEMPTED even when the attempt cannot succeed: %v", got)
	}
	// The work-signing acts do not run: they would sign as an identity
	// the window does not belong to.
	for _, after := range []string{"budget settle", "submission make"} {
		if slices.Contains(got, after) {
			t.Errorf("%q reached the seam under a rotated key: %v", after, got)
		}
	}
	// And the error says the window is open and needs a reap, rather
	// than reading as a tidy stop.
	if !strings.Contains(err.Error(), "left OPEN") || !strings.Contains(err.Error(), "reap") {
		t.Errorf("the error must say the window is stranded and needs a reap: %v", err)
	}
}

// conformance: an IDLE driver still catches a rotation. This is the
// case the per-act check cannot cover and the pre-poll check exists
// for: a driver whose poll returns no subjects never reaches act at
// all, so a rotated worker would otherwise poll forever under the
// obsolete fingerprint and miss work granted only to its new identity
// (review finding on #195).
//
// Poll carries the cached actor, which is exactly why the read path
// needs its own check rather than inheriting the write path's.
func TestAnIdleDriverStillCatchesARotation(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	writeSeededKey(t, keyPath, 3)

	// An empty queue: Poll succeeds and offers nothing, so no act runs.
	r := &recorder{answer: func(args []string) Result {
		if verb(args) == "offer list" {
			return Result{OK: true, Position: "10", Result: map[string]any{"offers": []any{}}}
		}
		return Result{OK: true, Position: "10"}
	}}
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if step, err := d.Step(5); err != nil || step.Outcome != Idle {
		t.Fatalf("the drill needs a genuinely idle iteration first: %+v %v", step, err)
	}
	for _, got := range r.verbs() {
		if got != "offer list" {
			t.Fatalf("this drill is vacuous unless the iteration stopped at the poll: %v", r.verbs())
		}
	}

	writeSeededKey(t, keyPath, 9)
	step, err := d.Step(5)
	if !errors.Is(err, ErrKeyRotated) {
		t.Fatalf("an idle driver must still be told its key rotated, got %v", err)
	}
	if step.Step != "identity" {
		t.Errorf("the refusal names the identity check: %+v", step)
	}
}

// conformance: every ORDINARY act declares the identity it is acting
// as, and the last-ditch exit declares none (plans/os-9a89245c.md).
//
// The sweep is over the act CATALOG rather than a hand-listed subset,
// so an act added later without --as fails here rather than shipping
// a signing site with no declared identity.
func TestEveryOrdinaryActDeclaresItsIdentity(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	writeSeededKey(t, keyPath, 3)
	fp, err := actorOf(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// EVERY declared act is invoked directly, not merely those one
	// iteration happens to reach. A sweep that skipped unobserved acts
	// would pass with a future act added without --as, which is the
	// guarantee criterion 5 actually asks for — and the first version
	// of this drill did exactly that: a Step exercises four of the
	// implementer's seven acts, so three were never checked (review
	// finding on #202).
	// EVERY act in the CATALOG, performed directly. Two narrower
	// sweeps were wrong before this one, and both would have shipped
	// the guarantee criterion 5 asks for while not providing it
	// (review finding on #202):
	//
	//   - sweeping only the acts one Step happens to reach checked
	//     four of them;
	//   - sweeping the acts this file's implementer() fixture declares
	//     checked five, while the SHIPPED manifest declares seven.
	//
	// --as is attached in actGated, not by any manifest, so the
	// property belongs to the catalog: an act added to loopverb later
	// must fail here whatever lane happens to declare it.
	all := implementer()
	all.ActsThrough = loopverb.Names()
	r := &recorder{}
	d, err := New(all, r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) { return 1, nil }), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loopverb.Names()) == 0 {
		t.Fatal("the catalog must not be empty")
	}
	for _, name := range loopverb.Names() {
		if _, err := d.act(name, "c-1"); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		args, ran := r.argsFor(name)
		if !ran {
			t.Fatalf("%q did not reach the seam", name)
		}
		if got := flagValue(args, "--as"); got != fp {
			t.Errorf("%q must declare --as %s, got %q — every catalog act declares the identity it acts as", name, fp, got)
		}
	}
}

// The last-ditch exit carries NO --as on the strand path.
//
// This drill runs against the DOUBLE, so it proves only what the loop
// EMITS. That the omission actually lets the act reach the admission
// boundary is a different claim, and a double cannot make it: the
// recorder manufactures the refusal without loopSigner or admission
// running at all. cmd/seed carries the real-CLI drill for that
// (review finding on #202), and this one is named for what it covers.
func TestTheLastDitchExitDeclaresNoIdentity(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	writeSeededKey(t, keyPath, 3)

	r := &recorder{answer: offers("c-1", func(args []string) (Result, bool) {
		if verb(args) == "claim park" {
			return Result{Exit: 6, OK: false, Code: "fenced_out",
				Message: "event on c-1 must cite the active fence held by the original actor"}, true
		}
		return Result{}, false
	})}
	d, err := New(implementer(), r, []string{"--remote", "/repo"}, keyPath,
		WorkFunc(func(string, Situation) (int, error) {
			writeSeededKey(t, keyPath, 9)
			return 4, nil
		}), WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(5); !errors.Is(err, ErrKeyRotated) {
		t.Fatalf("the drill needs the rotation path: %v", err)
	}
	park, ok := r.argsFor("claim park")
	if !ok {
		t.Fatal("this drill is vacuous unless the exit was attempted")
	}
	if got := flagValue(park, "--as"); got != "" {
		t.Errorf("the last-ditch exit must declare no identity, got --as %q: passing one would stop it at the SEAM "+
			"with usage instead of at the boundary with the fence rule's own refusal", got)
	}
	// The ordinary act that opened the window did declare one, so the
	// exemption is scoped rather than a blanket opt-out.
	if take, ok := r.argsFor("claim take"); ok && flagValue(take, "--as") == "" {
		t.Error("claim take is an ordinary act and must declare its identity")
	}
}

// conformance: D5 — the loop is WAKELESS by construction, and that is
// pinned by SURFACE rather than asserted by absence.
//
// "Runs with no wake channel at all" is a claim about something not
// being there, and a drill cannot watch a thing fail to happen. What
// it can do is pin the seams through which a wake would have to
// arrive: Verbs is the loop's only way to reach the world, and the
// options are its only configuration. A wake channel needs one of
// them, so adding one fails here and the spec has to move with it —
// which is what makes the one-inbox doctrine structural rather than
// hoped for.
func TestLoopSurfaceIsWakeless(t *testing.T) {
	vt := reflect.TypeOf((*Verbs)(nil)).Elem()
	if vt.NumMethod() != 1 || vt.Method(0).Name != "Run" {
		var got []string
		for i := 0; i < vt.NumMethod(); i++ {
			got = append(got, vt.Method(i).Name)
		}
		t.Fatalf("Verbs is the single method Run — a second seam is where a wake channel would arrive: %v", got)
	}
	if sig := vt.Method(0).Type; sig.NumIn() != 1 || !sig.IsVariadic() {
		t.Errorf("Run takes only the act's arguments: %v", sig)
	}

	// The exported option set: each is a value the caller supplies
	// once, never a channel the loop listens on.
	want := map[string]bool{"WithSince": true, "WithRepo": true, "WithBase": true, "WithObservations": true}
	for _, name := range optionNames(t) {
		if !want[name] {
			t.Errorf("option %q is new: an option carrying a wake seam makes the loop wakeful, and "+
				"spec/lanes.md's one-inbox doctrine would have to be amended with it", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("option %q went missing; this pin lists the loop's whole configuration surface", name)
	}
}

// optionNames reads the package's exported With* constructors out of
// its own source. Deriving the list rather than restating it is what
// stops the pin drifting: a new option appears here whether or not
// anyone remembered this test.
func optionNames(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(line, "func With") {
			continue
		}
		name := line[len("func "):]
		if i := strings.IndexByte(name, '('); i > 0 {
			out = append(out, name[:i])
		}
	}
	if len(out) == 0 {
		t.Fatal("the pin found no options at all, so it is measuring nothing")
	}
	return out
}
