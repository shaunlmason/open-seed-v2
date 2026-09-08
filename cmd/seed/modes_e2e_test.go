package main

// Small-team and fleet mode end to end (plans/os-6a08b166.md; Phase 9
// item 4; charter III.J's closing row).
//
// Both modes are ONE builder with two identity plans, because two
// hand-written drills that mean to be the same run drift and a
// difference that drifts in is invisible. The builder never carries
// its own capability table: it reads each lane's `grants` from the
// SHIPPED manifest in lanes/, so a manifest that stops describing
// its lane fails here, when the key it provisions stops being able to
// act.
//
// Neither mode wires a wake channel of any kind. That is not a setting
// to turn off — internal/loop has no wake seam at all — so the
// wakeless posture is pinned by surface rather than asserted by
// absence (TestLoopSurfaceIsWakeless).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// identity is one actor the mode provisions: which lane it plays, and
// the name it is enrolled under. The lane decides its grants.
type identity struct {
	lane  string
	actor string
	seed  byte
}

// smallTeam is the charter's floor: ONE principal operating a minimal
// set of identities — "at least an implementing actor and a distinct
// verifying actor, so verdict key disjointness holds even when one
// person runs everything".
//
// It is exactly the charter's floor and no more: two key files on one
// machine. The supervisor and observer the loop also needs are the
// charter's non-loop ROLES, provisioned from their own manifests in
// every mode (see buildMode).
var smallTeam = []identity{
	{lane: "implementer", actor: "impl", seed: 51},
	{lane: "verifier", actor: "verify", seed: 52},
}

// fleet is one identity per SHIPPED LANE, built from the manifest set
// rather than listed here. Filtered on kind (plans/os-d6a52784.md D7):
// a manifest of kind role is the charter's supervisor or observer,
// which invite and attest to work rather than take it, and a fleet
// that provisioned them as workers would have a key both inviting work
// and competing for it. The first draft ranged over every manifest, so
// adding the role files would have grown the fleet to eight lanes
// while the plan declared them non-lanes (review finding on #210).
func fleetPlan(t *testing.T) []identity {
	t.Helper()
	var out []identity
	seed := byte(60)
	for _, m := range mustLoad(t) {
		if m.Kind != lane.KindLane {
			continue
		}
		out = append(out, identity{lane: m.Lane, actor: m.Lane, seed: seed})
		seed++
	}
	if len(out) != len(lane.CharterLanes()) {
		t.Fatalf("the fleet is one identity per charter lane (%d), got %d", len(lane.CharterLanes()), len(out))
	}
	return out
}

// modeStand is a provisioned mode: the ledger in one posture, and the
// keys the plan named.
type modeStand struct {
	remote, state, ld     string
	src, base, spec, head string
	priv                  string
	// active is the protocol version the stand has upgraded to, so the
	// background facts the fixture stages carry the shape that version
	// admits (the plan approval's digest from seed/4).
	active    string
	keys, fps map[string]string
	grants    map[string][]string
	// appendRaw stages BACKGROUND facts on the raw seam. Setup may
	// use it (D3); nothing a drill asserts may come from it, because
	// `ledger append` runs no rules and a fact staged there proves
	// nothing about the boundary.
	appendRaw func(verb, subject, payload string)
}

// posture returns the args a verb takes to reach this stand's ledger.
func (m *modeStand) posture() []string {
	if m.remote != "" {
		return []string{"--remote", m.remote, "--state", m.state}
	}
	return []string{"--ledger", m.ld}
}

// buildMode provisions one mode. The grants come from the shipped lane
// manifests: this is the whole of D2, and it is why the builder takes
// a lane name rather than a capability list.
//
// BOTH modes run on the REMOTE posture, and the plan's D6 was wrong to
// split them by transport. It reasoned that fleet needs remote for
// contention (true) and small-team needs local to finish, because
// `verdict render` was local-only. This card gave that verb
// `--remote`, so the second half evaporated — and the deeper reason is
// that small-team could never have run locally at all: `claim take` is
// refused off the remote ("an exclusive verb and claiming is
// online-only — two offline actors claiming the same contract have not
// claimed anything"), and a claim is the loop's FIRST act.
//
// So the mode is purely the identity plan, which is what the charter
// says it is: "one principal operating a minimal set of actor
// identities" versus "disjoint actors per lane". Neither clause
// mentions transport.
func buildMode(t *testing.T, plan []identity) *modeStand {
	t.Helper()
	m := &modeStand{keys: map[string]string{}, fps: map[string]string{}, grants: map[string][]string{}}
	byLane := map[string]lane.Manifest{}
	for _, man := range mustLoad(t) {
		byLane[man.Lane] = man
	}

	dir, root, _ := writeKeys(t)
	m.priv = root
	m.remote = bareRemote(t)
	m.state = filepath.Join(dir, "state")
	m.src, m.base, m.spec, m.head = verdictRepo(t)
	resolve := seedRemoteGenesis(t, m.remote)
	libAppend(t, m.remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	appendRaw := func(verb, subject, payload string) {
		t.Helper()
		if e, code := runEnv(t, "ledger", "append", "--remote", m.remote, "--state", m.state,
			"--key", root, "--verb", verb, "--subject", subject, "--payload", payload); code != 0 {
			t.Fatalf("%s %s: %d %+v", verb, subject, code, e)
		}
	}
	m.appendRaw = appendRaw

	// The charter's non-loop ROLES, provisioned from their own
	// manifests in every mode (plans/os-d6a52784.md D7). offer.published
	// accepts supervise or operator and merge.observed accepts observer
	// or operator; the supervisor (§II.9) and the governed observer
	// (§8) are the parts the charter defines to hold them, outside the
	// six lanes. An earlier draft staged both as identities this
	// fixture invented, because no manifest granted either capability:
	// honest then, and once the manifests exist, continuing to invent
	// them would leave this fixture asserting exactly what it asserted
	// before the gap closed. Their grants are the MANIFESTS', like every
	// lane's, so the grants drill covers them too.
	roleSeeds := map[string]byte{"supervisor": 59, "observer": 58}
	for _, man := range mustLoad(t) {
		if man.Kind != lane.KindRole {
			continue
		}
		seed, ok := roleSeeds[man.Lane]
		if !ok {
			t.Fatalf("role %q shipped with no fixture seed: add one here rather than letting it be skipped", man.Lane)
		}
		path, pub, fp := writeWorkerKey(t, seed)
		m.keys[man.Lane], m.fps[man.Lane] = path, fp
		m.grants[man.Lane] = man.Grants
		appendRaw("actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, man.Lane))
		for _, g := range man.Grants {
			appendRaw("actor.granted", fp, `{"capability": "`+g+`"}`)
		}
	}

	for _, id := range plan {
		man, ok := byLane[id.lane]
		if !ok {
			t.Fatalf("the plan names lane %q, which lanes/ does not ship", id.lane)
		}
		path, pub, fp := writeWorkerKey(t, id.seed)
		m.keys[id.actor], m.fps[id.actor] = path, fp
		m.grants[id.actor] = man.Grants
		appendRaw("actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, id.actor))
		// The grants are the MANIFEST's, never a list this fixture
		// keeps. A lane holding none (the curator) is provisioned with
		// none: the case most likely to be wrong is the one a fixture
		// is most tempted to skip.
		for _, g := range man.Grants {
			appendRaw("actor.granted", fp, `{"capability": "`+g+`"}`)
		}
	}
	return m
}

// conformance: D2 — the builder reads the SHIPPED manifests, so a lane
// that stops describing itself fails here. Asserted directly: every
// provisioned actor's grants equal its manifest's, and the curator's
// emptiness is carried rather than skipped.
func TestModeGrantsComeFromTheShippedManifests(t *testing.T) {
	m := buildMode(t, fleetPlan(t))
	byLane := map[string][]string{}
	for _, man := range mustLoad(t) {
		byLane[man.Lane] = man.Grants
	}
	for _, id := range fleetPlan(t) {
		got, want := m.grants[id.actor], byLane[id.lane]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("lane %s: provisioned %v, manifest says %v", id.lane, got, want)
		}
	}
	if len(m.grants["curator"]) != 1 || m.grants["curator"][0] != keyring.CapCurate {
		t.Errorf("the curator holds the proposal grant and nothing else, and the fixture carries that: %v", m.grants["curator"])
	}
	// The roles came from MANIFESTS, not from this file (D7): their
	// grants equal what lanes/ ships, and they are not in the
	// fleet's identity plan.
	roles := 0
	for _, man := range mustLoad(t) {
		if man.Kind != lane.KindRole {
			continue
		}
		roles++
		if got := strings.Join(m.grants[man.Lane], ","); got != strings.Join(man.Grants, ",") {
			t.Errorf("role %s: provisioned %q, its manifest says %v", man.Lane, got, man.Grants)
		}
		for _, id := range fleetPlan(t) {
			if id.lane == man.Lane {
				t.Errorf("role %s is in the fleet's identity plan: a role invites or attests, it does not take work", man.Lane)
			}
		}
	}
	if roles != 2 {
		t.Errorf("the charter defines two non-loop roles (supervisor §II.9, observer §8), found %d", roles)
	}
	if got := len(fleetPlan(t)); got != 6 {
		t.Errorf("the fleet is exactly the charter's six lanes after the role files are added, got %d", got)
	}
}

// conformance: D2 — the lane with NO grants can read and cannot write.
// A fixture that skipped it would be hiding the case most likely to be
// wrong, and "reads" is asserted rather than assumed.
func TestCuratorReadsAndCannotWrite(t *testing.T) {
	m := buildMode(t, fleetPlan(t))
	args := append([]string{"situation"}, m.posture()...)
	if e, code := runEnv(t, append(args, "--key", m.keys["curator"])...); code != 0 {
		t.Fatalf("the curator must be able to READ: %d %+v", code, e)
	}
	// The writes refuse AT THE GRANT RULE, asserted by code rather
	// than by "it failed". The first draft of this drill also tried
	// `claim take`, which refuses with `contention` because claiming
	// is online-only — a refusal that has nothing to do with grants,
	// and would have made this drill pass whatever the curator held.
	//
	// `merge request` is deliberately NOT in this list: its citation
	// is derived client-side, so on a subject with no verdict it
	// refuses `not_found` before the grant rule is ever reached. That
	// is the cooperative posture working — derive what you need, and
	// say so when you cannot — but it means the act cannot testify
	// about grants here.
	for _, act := range [][]string{
		{"merge", "observe", "--subject", "c-1", "--merged", strings.Repeat("a", 40), "--pr", "pr/1"},
	} {
		call := append(append([]string{act[0], act[1]}, m.posture()...), "--key", m.keys["curator"])
		call = append(call, act[2:]...)
		e, code := runEnv(t, call...)
		if code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
			t.Errorf("%v must refuse OUT OF GRANT for a lane holding none, got %d %+v", act[:2], code, e.Error)
		}
	}
}

// contract stages one specified, offered contract. Setup rides the raw
// seam (D3): these are BACKGROUND facts, and nothing this file asserts
// is derived from them — every asserted property is produced by an
// admitted act.
func (m *modeStand) contract(t *testing.T, subject, supervisor string) {
	t.Helper()
	m.appendRaw("intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	m.appendRaw("contract.specified", subject,
		fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, m.spec))
	offer := fmt.Sprintf(`{"eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}, "expires": %q}`,
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	if e, code := runEnv(t, "ledger", "append", "--remote", m.remote, "--state", m.state,
		"--key", m.keys[supervisor], "--verb", "offer.published", "--subject", subject,
		"--payload", offer); code != 0 {
		t.Fatalf("offer: %d %+v", code, e)
	}
}

// conformance: acceptance criteria 1, 2 and 7 — small-team mode runs
// the FULL loop on one principal's minimal identity set and reaches
// `done`, and verdict key disjointness is proven by a REFUSAL rather
// than by the fixture having written two key files.
func TestSmallTeamModeReachesDone(t *testing.T) {
	m := buildMode(t, smallTeam)
	const subject = "c-1"
	m.contract(t, subject, "supervisor")

	// The worker half runs through internal/loop — the library exists
	// for exactly this — with NO wake channel of any kind.
	worked := 0
	d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl"],
		loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
			worked++
			if !sit.Holds(s) {
				return 0, fmt.Errorf("the work step runs inside a held window")
			}
			return 3, nil
		}), loop.WithBase(m.base+".."+m.head))
	if err != nil {
		t.Fatal(err)
	}
	step, err := d.Step(5)
	if err != nil {
		t.Fatalf("the loop must reach a deliberate exit: %v", err)
	}
	if step.Outcome != loop.Submitted || step.Subject != subject {
		t.Fatalf("the loop must submit: %s %s (%+v)", step.Outcome, step.Subject, step.Cause)
	}
	if worked != 1 {
		t.Fatalf("the work step runs once per iteration, ran %d", worked)
	}

	// Disjointness, PROVEN — and proven against the case that actually
	// threatens it.
	//
	// The implementing actor is granted `verdict` HERE, deliberately.
	// A principal running everything can grant themselves everything,
	// and the charter's claim is that disjointness holds anyway. Left
	// ungranted, this key would be refused `out_of_grant` — capability
	// absence, which `admit.go` is careful to call a different thing —
	// and the drill would have proven only that a key without the
	// grant cannot render. That is not the row.
	m.appendRaw("actor.granted", m.fps["impl"], `{"capability": "verdict"}`)

	e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
		"--subject", subject, "--repo", m.src, "--key", m.keys["impl"], "--verdict", "pass")...)
	if code != 17 || e.Error == nil || e.Error.Code != "not_independent" {
		t.Fatalf("the implementing key must be refused as NOT INDEPENDENT, got %d %+v", code, e.Error)
	}
	if !strings.Contains(e.Error.Message, "claimant") && !strings.Contains(e.Error.Message, "submission") {
		t.Errorf("the refusal must name why: %q", e.Error.Message)
	}
	if e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
		"--subject", subject, "--repo", m.src, "--key", m.keys["verify"], "--verdict", "pass")...); code != 0 {
		t.Fatalf("the distinct verifying key must be admitted: %d %+v", code, e)
	}

	// The terminal chain, each step its own admitted event.
	if e, code := runEnv(t, append(append([]string{"merge", "request"}, m.posture()...),
		"--subject", subject, "--key", m.keys["impl"])...); code != 0 {
		t.Fatalf("merge request: %d %+v", code, e)
	}
	if e, code := runEnv(t, append(append([]string{"merge", "observe"}, m.posture()...),
		"--subject", subject, "--key", m.keys["observer"], "--merged", m.head, "--pr", "pr/1")...); code != 0 {
		t.Fatalf("merge observe: %d %+v", code, e)
	}
	if got := m.stateOf(t, subject); got != "done" {
		t.Fatalf("the full loop ends at done, got %q", got)
	}
}

// stateOf folds the remote's own chain and reports the subject's
// state. It materializes the ref rather than reading a verb's report:
// when a chain of admitted acts is the thing under test, the chain is
// where the answer lives.
func (m *modeStand) stateOf(t *testing.T, subject string) string {
	t.Helper()
	c, err := gitref.NewClient(t.TempDir(), m.remote, "refs/seed/ledger")
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	st, failEnv := loadVerdictState(dir)
	if failEnv != nil {
		t.Fatalf("the remote chain must verify: %+v", failEnv)
	}
	s, ok := st.fold.State(subject)
	if !ok {
		t.Fatalf("no %s in the remote fold", subject)
	}
	return s.State
}

// attempt is one act the lane put through the seam, with the position
// the envelope was stamped at and the refusal code if any.
type attempt struct {
	iter     int
	act      string
	position string
	code     string
}

// transcript wraps the REAL verbs and only observes. Instrumenting a
// loop by replacing the boundary it is being tested against proves
// nothing about that boundary (#202), so this records and forwards.
type transcript struct {
	inner loop.Verbs
	iter  int
	log   []attempt
}

func (tr *transcript) Run(args ...string) loop.Result {
	res := tr.inner.Run(args...)
	act := args[0]
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		act += " " + args[1]
	}
	tr.log = append(tr.log, attempt{iter: tr.iter, act: act, position: res.Position, code: res.Code})
	return res
}

// refusals returns the attempts the boundary declined.
func (tr *transcript) refusals() []attempt {
	var out []attempt
	for _, a := range tr.log {
		if a.code != "" {
			out = append(out, a)
		}
	}
	return out
}

// blindRetry reports the first spin: the SAME act refusing with the
// SAME code on consecutive iterations from a position that did not
// advance. Same act, same knowledge, same answer is the fourth
// outcome the build plan forbids — a lane that learned nothing and
// tried again.
func (tr *transcript) blindRetry() (attempt, bool) {
	prev := map[string]attempt{}
	for _, a := range tr.refusals() {
		key := a.act + "\x00" + a.code
		if p, seen := prev[key]; seen && a.iter == p.iter+1 && a.position == p.position {
			return a, true
		}
		prev[key] = a
	}
	return attempt{}, false
}

// racing lands a RIVAL claim inside the window, just before the lane's
// own `claim take` reaches the boundary.
//
// Two workers stepped one after another do not race: the second polls
// after the first has claimed, finds nothing offered, and goes idle at
// the poll with no refusal at all. The middle arm answers a REFUSAL,
// so a drill built that way would have had no refusal to answer and
// would have counted an empty poll as convergence.
//
// Planting the rival inside the window puts the lane in the race BY
// CONSTRUCTION rather than by timing, which is the same reason #202's
// interleaving drill rotates a key from inside the seam: a race
// reproduced by sleeping passes green on a slower runner.
type racing struct {
	*transcript
	t     *testing.T
	plant func()
	fired bool
}

func (r *racing) Run(args ...string) loop.Result {
	if !r.fired && len(args) > 1 && args[0] == "claim" && args[1] == "take" {
		r.fired = true
		r.plant()
	}
	return r.transcript.Run(args...)
}

// conformance: acceptance criteria 3, 4 and 5 — fleet mode, one

// identity per shipped lane, two workers racing `claim take` against
// one remote, reaching `done`; every convergence arm exercised; and
// the blind-retry detector run over the whole transcript.
func TestFleetModeConvergesAndReachesDone(t *testing.T) {
	m := buildMode(t, fleetPlan(t))
	const subject = "c-1"
	m.contract(t, subject, "supervisor")

	// Two workers, both eligible, both polling the same offer. Only
	// one can hold the window: the loser's `claim take` is REFUSED for
	// contention, which is the refusal the middle arm answers.
	//
	// The planner lane is the second worker rather than a second
	// implementer, because it is the other shipped lane granted
	// `claim` — the fleet races the lanes it actually has.
	tr := &transcript{inner: loopVerbs{}}
	r := &racing{transcript: tr, t: t, plant: func() {
		// The rival: the OTHER shipped lane granted `claim`, taking
		// the window through the same admitted verb.
		if e, code := runEnv(t, append(append([]string{"claim", "take"}, m.posture()...),
			"--subject", subject, "--key", m.keys["planner"])...); code != 0 {
			t.Fatalf("the rival claim must land, or there is no race: %d %+v", code, e)
		}
	}}

	lost, err := m.stepWith(t, r, "implementer", subject)
	if err != nil {
		t.Fatalf("losing a race is not an error: %v", err)
	}
	if lost.Outcome != loop.Idle {
		t.Fatalf("the loser re-orients rather than parking what it never held: %s", lost.Outcome)
	}
	if lost.Step != "claim take" || lost.Cause.Code == "" {
		t.Fatalf("the losing iteration must END on a REFUSED claim, carrying it: step=%q cause=%+v",
			lost.Step, lost.Cause)
	}
	if lost.Cause.Code != "contention" {
		t.Errorf("a lost race is ordinary contention, not %q: %s", lost.Cause.Code, lost.Cause.Message)
	}

	// THE MIDDLE ARM: that refusal answered on the next iteration by a
	// refreshed position-stamped read showing the act is no longer
	// owed. The lane takes no different work here because the fixture
	// offers only one contract, which is the arm in its purest form.
	tr.iter++
	next, err := m.stepWith(t, tr, "implementer", subject)
	if err != nil {
		t.Fatalf("the next iteration must not error: %v", err)
	}
	if next.Outcome != loop.Idle {
		t.Fatalf("the refreshed read shows the work is no longer owed: %s (%+v)", next.Outcome, next.Cause)
	}
	if spun, found := tr.blindRetry(); found {
		t.Fatalf("a blind retry: %s refused %q twice from position %s with nothing learned",
			spun.act, spun.code, spun.position)
	}

	// ANTI-VACUITY: the arm must have been EXERCISED. Every assertion
	// above is true of a lane that met no refusal at all, which is
	// exactly how this whole fixture could ship proving nothing.
	armed := false
	for _, a := range tr.refusals() {
		if a.act == "claim take" && a.code == "contention" {
			armed = true
		}
	}
	if !armed {
		t.Fatal("the middle arm is UNEXERCISED: no claim was refused, so the refreshed read answered nothing")
	}

	// And the rival drives the contract the rest of the way to done,
	// which is the loop completing elsewhere from the shared ledger.
	m.finish(t, subject, "planner")
	if got := m.stateOf(t, subject); got != "done" {
		t.Fatalf("fleet mode ends at done, got %q", got)
	}
}

// finish drives a held contract through submission, verdict and the
// merge chain, each step its own admitted act.
func (m *modeStand) finish(t *testing.T, subject, holder string) {
	t.Helper()
	for _, act := range [][]string{
		{"budget", "reserve", "--amount", "3"},
		{"budget", "settle", "--actuals", "1"},
		{"submission", "make", "--packet", m.packet(t, subject), "--base", m.base + ".." + m.head},
	} {
		call := append(append([]string{act[0], act[1]}, m.posture()...),
			"--subject", subject, "--key", m.keys[holder])
		call = append(call, act[2:]...)
		if e, code := runEnv(t, call...); code != 0 {
			t.Fatalf("%v: %d %+v", act[:2], code, e)
		}
	}
	if e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
		"--subject", subject, "--repo", m.src, "--key", m.keys["verifier"], "--verdict", "pass")...); code != 0 {
		t.Fatalf("verdict render: %d %+v", code, e)
	}
	if e, code := runEnv(t, append(append([]string{"merge", "request"}, m.posture()...),
		"--subject", subject, "--key", m.keys[holder])...); code != 0 {
		t.Fatalf("merge request: %d %+v", code, e)
	}
	if e, code := runEnv(t, append(append([]string{"merge", "observe"}, m.posture()...),
		"--subject", subject, "--key", m.keys["observer"], "--merged", m.head, "--pr", "pr/2")...); code != 0 {
		t.Fatalf("merge observe: %d %+v", code, e)
	}
}

// step runs one loop iteration for a lane's actor through the given
// transcript.
func (m *modeStand) stepWith(t *testing.T, verbs loop.Verbs, laneName, subject string) (loop.StepResult, error) {
	t.Helper()
	var man lane.Manifest
	for _, l := range mustLoad(t) {
		if l.Lane == laneName {
			man = l
		}
	}
	d, err := loop.New(man, verbs, m.posture(), m.keys[laneName],
		loop.WorkFunc(func(string, loop.Situation) (int, error) { return 1, nil }),
		loop.WithBase(m.base+".."+m.head))
	if err != nil {
		t.Fatal(err)
	}
	return d.Step(3)
}

// packet writes the four-part handoff a deliberate exit carries. The
// submission verb takes a file, so the fixture supplies one rather
// than reaching for a flag that does not exist.
func (m *modeStand) packet(t *testing.T, subject string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packet.json")
	body := fmt.Sprintf(`{"acceptance": [%q], "decisions": [], "base": %q, "refs": [], "findings": []}`,
		"accept.md @ "+m.spec, m.base+".."+m.head)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// conformance: acceptance criterion 5 — the blind-retry detector
// detects. A detector that has only ever seen converging runs has not
// been shown to catch anything, so it is fed a known spin and a known
// non-spin directly.
func TestBlindRetryDetector(t *testing.T) {
	for _, tc := range []struct {
		name string
		log  []attempt
		spin bool
	}{
		{"the same act, same code, same position on consecutive iterations", []attempt{
			{iter: 1, act: "claim take", position: "9", code: "contention"},
			{iter: 2, act: "claim take", position: "9", code: "contention"},
		}, true},
		{"the position advanced, so the lane learned something", []attempt{
			{iter: 1, act: "claim take", position: "9", code: "contention"},
			{iter: 2, act: "claim take", position: "11", code: "contention"},
		}, false},
		{"a different code is a different refusal", []attempt{
			{iter: 1, act: "claim take", position: "9", code: "contention"},
			{iter: 2, act: "claim take", position: "9", code: "fenced_out"},
		}, false},
		{"not consecutive: something else happened between", []attempt{
			{iter: 1, act: "claim take", position: "9", code: "contention"},
			{iter: 3, act: "claim take", position: "9", code: "contention"},
		}, false},
		{"successes are not retries", []attempt{
			{iter: 1, act: "claim take", position: "9"},
			{iter: 2, act: "claim take", position: "9"},
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{log: tc.log}
			if _, got := tr.blindRetry(); got != tc.spin {
				t.Errorf("blindRetry = %v, want %v", got, tc.spin)
			}
		})
	}
}

// conformance: plans/os-8e53ffd9.md step 8 — in small-team mode the
// implementing actor can be QUALIFIED: its claim grant cites the
// configuration an eval will one day have passed, the supervisor's
// offers name the configurations they want, and the run the supervisor
// starts inside the worker's window is held to the holder's grant. A
// contract offered under a configuration the worker does not hold is
// unseen, so the loop idles; one offered under its own configuration is
// claimed, and inside that window a start declaring a different model
// is out of grant while the cited one admits.
func TestSmallTeamQualifiedWorkerIsOfferedAndHeldToItsConfiguration(t *testing.T) {
	m := buildMode(t, smallTeam)
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	m.appendRaw("actor.granted", m.fps["impl"], `{"capability": "claim", "tuple": `+drillTuple(nil)+`}`)
	offerUnder := func(subject, tup string) {
		t.Helper()
		m.appendRaw("intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
		m.appendRaw("contract.specified", subject,
			fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, m.spec))
		offer := fmt.Sprintf(`{"eligibility": {"capabilities": ["claim"], "tuples": [%s]}, "expires": %q}`,
			tup, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
		if e, code := runEnv(t, "ledger", "append", "--remote", m.remote, "--state", m.state,
			"--key", m.keys["supervisor"], "--verb", "offer.published", "--subject", subject,
			"--payload", offer); code != 0 {
			t.Fatalf("offer: %d %+v", code, e)
		}
	}

	refused, admitted := 0, 0
	d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl"],
		loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
			start := func(model string) (ledgerEnv, int) {
				return runEnv(t, append(append([]string{"run", "start"}, m.posture()...),
					"--key", m.keys["supervisor"], "--subject", s,
					"--principal", "acme", "--model", model, "--tool-policy", "default")...)
			}
			if e, code := start("fable/9.9"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
				return 0, fmt.Errorf("a start under a configuration the holder's grant does not cite is out of grant inside the window: %d %+v", code, e.Error)
			}
			refused++
			if e, code := start("fable/5.1"); code != 0 {
				return 0, fmt.Errorf("the cited configuration admits: %d %+v", code, e.Error)
			}
			admitted++
			return 2, nil
		}), loop.WithBase(m.base+".."+m.head))
	if err != nil {
		t.Fatal(err)
	}

	offerUnder("c-1", drillTuple(map[string]string{"model": "fable/9.9"}))
	step, err := d.Step(5)
	if err != nil || step.Outcome != loop.Idle {
		t.Fatalf("an offer naming a configuration the worker does not hold is unseen, so the loop idles: %+v %v", step, err)
	}

	offerUnder("c-2", drillTuple(nil))
	step, err = d.Step(5)
	if err != nil {
		t.Fatalf("the loop must reach a deliberate exit: %v", err)
	}
	if step.Outcome != loop.Submitted || step.Subject != "c-2" {
		t.Fatalf("the offer naming the worker's own configuration is claimed and submitted: %s %s (%+v)", step.Outcome, step.Subject, step.Cause)
	}
	if refused != 1 || admitted != 1 {
		t.Fatalf("inside the window the drifted start refused once and the cited one admitted once: %d %d", refused, admitted)
	}
}

// materialize folds the remote's own chain into a local ledger
// directory, for the verbs that read a ledger rather than a posture.
func (m *modeStand) materialize(t *testing.T) string {
	t.Helper()
	c, err := gitref.NewClient(t.TempDir(), m.remote, "refs/seed/ledger")
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ringOf replays the remote chain's keyring.
func (m *modeStand) ringOf(t *testing.T) *keyring.State {
	t.Helper()
	st, failEnv := loadVerdictState(m.materialize(t))
	if failEnv != nil {
		t.Fatalf("the remote chain must verify: %+v", failEnv)
	}
	ring, _, err := keyring.StateAt(st.records)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

// conformance: plans/os-03e47abb.md AC2, AC4, AC5 — in small-team mode
// an eval filed by the dispatcher runs through the production
// machinery end to end: the supervisor's act publishes the offer, the
// loop claims and reserves, the supervisor's start declares the
// worker's configuration, the work applies the solution in the
// worker's own clone (the loop deriving the submission range from it),
// an L1-disjoint verifier passes it with a receipt verdict check
// reproduces, and the supervisor's act mints the HOLDER's
// qualification for the DECLARED tuple. Afterwards an ordinary run
// under that tuple admits and one differing in a field is out of
// grant; a failed eval disqualifies the configuration and the bridge
// stays closed; a stale qualification owes the dispatcher a spot-check
// the supervisor then offers.
func TestSmallTeamEvalQualifiesAndDisqualifiesThroughTheProductionMachinery(t *testing.T) {
	m := buildMode(t, append(append([]identity{}, smallTeam...), identity{lane: "dispatcher", actor: "dispatch", seed: 53}))
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)

	// The shipped definitions land in the source repository under a
	// squash-merge subject: the reviewed revision every filing anchors.
	gitSrc := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", m.src, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	copyTree(t, filepath.Join("..", "..", "evals"), filepath.Join(m.src, eval.Root))
	gitSrc("add", ".")
	gitSrc("commit", "--quiet", "-m", "evals: the shipped definitions (#1)")
	anchor := gitSrc("rev-parse", "HEAD")

	// The worker's clone: the loop derives the submission range from
	// it, and the work step pushes its branch back so the verifier's
	// checkout can reach the head.
	clone := filepath.Join(t.TempDir(), "worker")
	if out, err := exec.Command("git", "clone", "--quiet", m.src, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	hardenGitRepo(t, clone)
	gitClone := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", clone, "-c", "user.name=impl", "-c", "user.email=impl@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	work := 0
	apply := func(fix bool) {
		t.Helper()
		work++
		branch := fmt.Sprintf("work-%d", work)
		gitClone("checkout", "--quiet", "-B", branch, anchor)
		if fix {
			b, err := os.ReadFile(filepath.Join(clone, eval.Root, "fix-the-check", "solution", "greet.sh"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(clone, eval.Root, "fix-the-check", "fixture", "greet.sh"), b, 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(filepath.Join(clone, eval.Root, "fix-the-check", "fixture", "NOTES"), []byte("tried\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitClone("add", ".")
		gitClone("commit", "--quiet", "-m", "the work")
		gitClone("push", "--quiet", "origin", branch)
	}

	act := func(who string, extra ...string) ledgerEnv {
		t.Helper()
		args := append(append([]string{"eval", "act"}, m.posture()...), "--repo", m.src, "--key", m.keys[who])
		e, code := runEnv(t, append(args, extra...)...)
		if code != 0 {
			t.Fatalf("eval act as %s: %d %+v", who, code, e.Error)
		}
		return e
	}
	performed := func(e ledgerEnv, want string) {
		t.Helper()
		rows, _ := e.Result["performed"].([]any)
		var got []string
		for _, r := range rows {
			got = append(got, fmt.Sprint(r.(map[string]any)["kind"]))
		}
		if strings.Join(got, " ") != want {
			t.Fatalf("performed %q, want %q (%+v)", strings.Join(got, " "), want, e.Result)
		}
	}
	file := func() string {
		t.Helper()
		e, code := runEnv(t, append(append([]string{"eval", "file"}, m.posture()...), "--repo", m.src, "--key", m.keys["dispatch"], "--eval", "fix-the-check")...)
		if code != 0 {
			t.Fatalf("the dispatcher files the eval: %d %+v", code, e.Error)
		}
		subject, _ := e.Result["subject"].(string)
		return subject
	}
	start := func(subject, model string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"run", "start"}, m.posture()...),
			"--key", m.keys["supervisor"], "--subject", subject,
			"--principal", "acme", "--model", model, "--tool-policy", "default")...)
	}
	// worker runs one loop iteration whose work step has the
	// supervisor declare the configuration and then applies (or does
	// not apply) the solution in the clone.
	worker := func(fix bool) string {
		t.Helper()
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl"],
			loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
				if e, code := start(s, "fable/5.1"); code != 0 {
					return 0, fmt.Errorf("the supervisor's start on the eval: %d %+v", code, e.Error)
				}
				apply(fix)
				return 2, nil
			}), loop.WithRepo(clone))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil || step.Outcome != loop.Submitted {
			t.Fatalf("the loop claims, works and submits: %+v %v", step, err)
		}
		return step.Subject
	}
	render := func(subject, verdict string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
			"--subject", subject, "--repo", m.src, "--key", m.keys["verify"], "--verdict", verdict)...)
	}

	// AC2: the eval end to end, and the mint.
	e1 := file()
	performed(act("supervisor"), "offer")
	if got := worker(true); got != e1 {
		t.Fatalf("the loop took the eval: %s", got)
	}
	if e, code := render(e1, "pass"); code != 0 {
		t.Fatalf("the disjoint verifier passes the solved eval: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "verdict", "check", "--ledger", m.materialize(t), "--subject", e1, "--repo", m.src); code != 0 {
		t.Fatalf("verdict check reproduces the receipt: %d %+v", code, e.Error)
	}
	performed(act("supervisor"), "mint")
	if cited := m.ringOf(t).GrantTuples(m.fps["impl"], keyring.CapClaim); len(cited) != 1 || cited[0].Model != "fable/5.1" || cited[0].Principal != "acme" {
		t.Fatalf("the mint qualifies the HOLDER for the DECLARED configuration: %+v", cited)
	}
	if owed, _ := act("supervisor").Result["owed"].([]any); len(owed) != 0 {
		t.Fatalf("one verdict, one consequence: %+v", owed)
	}

	// AC2's consequence on an ordinary contract, inside the worker's
	// own window: a start differing in one field is out of grant, the
	// cited configuration admits.
	ordinary := func(subject, model, wantCode, wantMessage string) {
		t.Helper()
		m.contract(t, subject, "supervisor")
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl"],
			loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
				e, code := start(s, "fable/9.9")
				if code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
					return 0, fmt.Errorf("a start under a configuration the set does not cite is out of grant: %d %+v", code, e.Error)
				}
				e, code = start(s, model)
				if fmt.Sprint(code) != wantCode || (wantMessage != "" && (e.Error == nil || !strings.Contains(e.Error.Message, wantMessage))) {
					return 0, fmt.Errorf("start under %s: want %s %q, got %d %+v", model, wantCode, wantMessage, code, e.Error)
				}
				return 2, nil
			}), loop.WithBase(m.base+".."+m.head))
		if err != nil {
			t.Fatal(err)
		}
		if step, err := d.Step(5); err != nil || step.Outcome != loop.Submitted || step.Subject != subject {
			t.Fatalf("%s: %+v %v", subject, step, err)
		}
	}
	ordinary("c-ord", "fable/5.1", "0", "")

	// AC4: the unsolved eval fails, the configuration is disqualified,
	// and the bridge does not reopen: on the next ordinary contract
	// even the once-cited configuration is out of grant.
	e2 := file()
	performed(act("supervisor"), "offer")
	worker(false)
	if e, code := render(e2, "pass"); code != 20 {
		t.Fatalf("pass is not renderable over red checks: %d %+v", code, e.Error)
	}
	if e, code := render(e2, "fail"); code != 0 {
		t.Fatalf("the fail renders: %d %+v", code, e.Error)
	}
	performed(act("supervisor"), "disqualify")
	ring := m.ringOf(t)
	if len(ring.GrantTuples(m.fps["impl"], keyring.CapClaim)) != 0 || !ring.EverCited(m.fps["impl"], keyring.CapClaim) {
		t.Fatal("the disqualification empties the set and leaves the mark")
	}
	ordinary("c-ord2", "fable/5.1", "14", "every cited configuration is disqualified")

	// A passing eval re-qualifies (D6), and AC5: past the interval the
	// dispatcher's act files and specifies the spot-check, reporting
	// its offer as the supervisor's, who publishes it.
	e3 := file()
	performed(act("supervisor"), "offer")
	worker(true)
	if e, code := render(e3, "pass"); code != 0 {
		t.Fatalf("the re-test passes: %d %+v", code, e.Error)
	}
	performed(act("supervisor"), "mint")
	later := time.Now().UTC().Add(25 * time.Hour).Format(time.RFC3339)
	e := act("dispatch", "--spot-check-after", "24h", "--as-of", later)
	performed(e, "spot-check spot-check")
	if owed, _ := e.Result["owed"].([]any); len(owed) != 0 {
		t.Fatalf("the dispatcher's act owes nothing further at that instant: %+v", owed)
	}
	e = act("supervisor", "--spot-check-after", "24h", "--as-of", later)
	performed(e, "offer")
	e = act("dispatch", "--spot-check-after", "24h", "--as-of", later)
	performed(e, "")
	if owed, _ := e.Result["owed"].([]any); len(owed) != 0 {
		t.Fatalf("with the spot-check open and offered nothing is owed: %+v", owed)
	}
}

// conformance: plans/os-e2f1ad23.md AC4, the CLI arm of the poisoning
// drill. Two poisons run through `seed knowledge propose` and `seed
// knowledge promote` in the small-team fixture, proving the refusal
// reaches an operator's terminal with the code the boundary gave:
// `worker-proposes` (a claim key proposing over its own admitted
// observations) and `smuggled-role-lesson` (a role carrier promoted
// citing a pass on an ordinary contract that carries no eval marker).
func TestPoisonsRefuseAtTheTerminal(t *testing.T) {
	m := buildMode(t, append(append([]identity{}, smallTeam...),
		identity{lane: "implementer", actor: "impl2", seed: 54},
		identity{lane: "curator", actor: "curator", seed: 55},
		identity{lane: "dispatcher", actor: "dispatch", seed: 53}))
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	m.appendRaw(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	gitSrc := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", m.src, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	anchor := gitSrc("rev-parse", "HEAD")
	deadEnd := func(actor, subject string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"knowledge", "deadend"}, m.posture()...), "--key", m.keys[actor], "--subject", subject,
			"--tried", "retrying the fetch", "--outcome", "the mirror timed out", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")...)
	}
	work := func(actor string) string {
		t.Helper()
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys[actor],
			loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
				if e, code := deadEnd(actor, s); code != 0 {
					return 0, fmt.Errorf("the holder's dead end: %d %+v", code, e.Error)
				}
				return 2, nil
			}), loop.WithBase(m.base+".."+m.head))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil || step.Outcome != loop.Submitted {
			t.Fatalf("the loop claims, works and submits: %+v %v", step, err)
		}
		return step.Subject
	}
	m.contract(t, "c-a", "supervisor")
	if got := work("impl"); got != "c-a" {
		t.Fatalf("impl took c-a: %s", got)
	}
	m.contract(t, "c-b", "supervisor")
	if got := work("impl2"); got != "c-b" {
		t.Fatalf("impl2 took c-b: %s", got)
	}
	// An ordinary pass on c-a: a verdict with no eval marker.
	if e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
		"--subject", "c-a", "--repo", m.src, "--key", m.keys["verify"], "--verdict", "pass")...); code != 0 {
		t.Fatalf("the verifier passes c-a: %d %+v", code, e.Error)
	}
	st, failEnv := loadVerdictState(m.materialize(t))
	if failEnv != nil {
		t.Fatalf("the remote chain must verify: %+v", failEnv)
	}
	plainState, _ := st.fold.State("c-a")
	if plainState.Verdict == nil {
		t.Fatal("the plain pass folded")
	}
	plainPass := plainState.Verdict.Pos
	dead := func(contract string) int {
		t.Helper()
		e, code := runEnv(t, append([]string{"knowledge", "show"}, m.posture()...)...)
		if code != 0 {
			t.Fatalf("knowledge show: %d %+v", code, e)
		}
		ends, _ := e.Result["dead_ends"].(map[string]any)
		list, _ := ends[contract].([]any)
		if len(list) == 0 {
			t.Fatalf("no dead end on %s: %+v", contract, e.Result)
		}
		pos, _ := list[0].(map[string]any)["position"].(float64)
		return int(pos)
	}
	pA, pB := dead("c-a"), dead("c-b")
	claim := "record the mirror's temperature before retrying the fetch"
	propose := func(key string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"knowledge", "propose"}, m.posture()...), "--key", m.keys[key],
			"--claim", claim, "--applies-when", `{"routing": "core"}`, "--support", fmt.Sprintf("c-a@%d", pA), "--support", fmt.Sprintf("c-b@%d", pB),
			"--provenance", "plans/os-e2f1ad23.md @ "+anchor)...)
	}
	// worker-proposes: the claim key that recorded the observations
	// cannot propose over them; the terminal reports out_of_grant.
	if e, code := propose("impl"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("worker-proposes refuses out of grant at the terminal: %d %+v", code, e.Error)
	}
	e, code := propose("curator")
	if code != 0 {
		t.Fatalf("the curator proposes: %d %+v", code, e.Error)
	}
	id, _ := e.Result["hypothesis"].(string)
	e, code = runEnv(t, append([]string{"knowledge", "show"}, m.posture()...)...)
	if code != 0 {
		t.Fatalf("knowledge show: %d %+v", code, e)
	}
	hyps, _ := e.Result["hypotheses"].([]any)
	if len(hyps) != 1 {
		t.Fatalf("one hypothesis stands: %+v", e.Result)
	}
	hposF, _ := hyps[0].(map[string]any)["position"].(float64)
	cited := fmt.Sprintf("%s@%d", id, int(hposF))
	// The candidate role lesson lands on main: the carrier.
	body := "---\nhypothesis: " + cited + "\napplies-when: {\"routing\": \"core\"}\nsupport: " + fmt.Sprintf("c-a@%d, c-b@%d", pA, pB) +
		"\nprovenance: plans/os-e2f1ad23.md @ " + anchor + "\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: role\n---\n\n# Record the mirror's temperature\n"
	if err := os.MkdirAll(filepath.Join(m.src, curation.LessonsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.src, curation.LessonsDir, "mirror.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitSrc("add", ".")
	gitSrc("commit", "--quiet", "-m", "knowledge: the mirror lesson (#2)")
	carrierCommit := gitSrc("rev-parse", "HEAD")
	carrier := curation.LessonsDir + "/mirror.md @ " + carrierCommit
	// smuggled-role-lesson: the observer promotes the role carrier
	// citing the plain pass as its adversarial evaluation.
	e, code = runEnv(t, append(append([]string{"knowledge", "promote"}, m.posture()...), "--key", m.keys["observer"],
		"--lesson", carrier, "--hypothesis", cited, "--pr", "pr/2 @ "+carrierCommit, "--repo", m.src, "--carrier", "role",
		"--adversarial", fmt.Sprintf("fix-the-check@%d", plainPass), "--last-validated", "2026-09-01T00:00:00Z", "--expires", "2026-12-01T00:00:00Z")...)
	if code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, curation.GatePromotionAdversary) {
		t.Fatalf("smuggled-role-lesson refuses at the terminal naming the gate: %d %+v", code, e.Error)
	}
	// Neither end reached: the fold carries no lesson and the next
	// claim on a matching contract receives none.
	m.contract(t, "c-c", "supervisor")
	e, code = runEnv(t, append(append([]string{"claim", "take"}, m.posture()...), "--key", m.keys["impl"], "--subject", "c-c", "--repo", m.src)...)
	if code != 0 {
		t.Fatalf("claim take: %d %+v", code, e.Error)
	}
	if lessons, _ := e.Result["lessons"].([]any); len(lessons) != 0 {
		t.Fatalf("the poisoned lesson reached a claim: %+v", lessons)
	}
}
