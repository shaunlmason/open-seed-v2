package main

// The recorder scenario and the trajectory corpus (plans/os-6bd9ffff.md
// D3, AC3): one local ledger driven through every shipped lane's own
// acts, each lane with at least one admitted and one refused attempt
// so both arms of the recorder are exercised; the corpus under
// trajectories/lanes is what the scenario records, one file per
// manifest in lanes, reproduced byte for byte by the drill
// (`go test ./cmd/seed -run Corpus -update` re-records it) and
// replayed green against the shipped configuration.

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

var updateCorpus = flag.Bool("update", false, "re-record trajectories/lanes from the recorder scenario")

// corpusDir is the committed corpus, beside the lanes it covers.
func corpusDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "trajectories", "lanes"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// laneSeeds fixes one deterministic key per shipped manifest: the
// corpus is byte-identical across recordings only because every
// fingerprint in it is.
var laneSeeds = map[string]byte{
	"dispatcher": 31, "planner": 32, "implementer": 33, "verifier": 34,
	"curator": 35, "maintenance": 36, "supervisor": 37, "observer": 38,
}

type recorderStand struct {
	ld, root, src, base, spec, head string
	anchor1, anchor2                string
	keys, fps                       map[string]string
	packet                          string
}

// recorderRepo is the scenario's source repository: the verdict
// fixture's shape (a base, the specs, a head) with the plans committed
// at the base, where the receipt's ancestry binding reads the approved
// plan, and c-3's plan revised at the head so its approval is edited.
func recorderRepo(t *testing.T) (dir, base, spec, head, anchor1, anchor2 string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, dir)
	write("hello.txt", "hello\n")
	write("plans/c-2.md", "# Plan c-2\n\nthe planner's decomposition\n")
	write("plans/c-3.md", "# Plan c-3\n\nthe planner's decomposition\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "base")
	base = git("rev-parse", "HEAD")
	write("accept.md", "# Green\n\n## Validation Commands\n\n- Boundary: `printf ok`\n")
	write("red.md", "# Red\n\n## Validation Commands\n\n- Boundary: `false`\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "specs")
	spec = git("rev-parse", "HEAD")
	write("hello.txt", "changed\n")
	write("plans/c-3.md", "# Plan c-3\n\nedited in review\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "head")
	head = git("rev-parse", "HEAD")
	return dir, base, spec, head, "plans/c-2.md @ " + base, "plans/c-3.md @ " + head
}

// buildRecorderStand provisions the local ledger: the chain at seed/4,
// every shipped manifest enrolled and granted from its own file (the
// modes fixture's D2 posture), a source repository for the verdicts
// and a plan repository at two revisions.
func buildRecorderStand(t *testing.T) *recorderStand {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	st := &recorderStand{ld: filepath.Join(dir, "ledger"), root: priv, keys: map[string]string{}, fps: map[string]string{}}
	if _, code := runEnv(t, "init", "--ledger", st.ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	st.src, st.base, st.spec, st.head, st.anchor1, st.anchor2 = recorderRepo(t)
	st.rootAppend(t, "system.protocol.upgraded", "system", `{"to": "`+version.Seed1+`"}`)
	upgradeLedgerTo(t, st.ld, priv, version.Seed4)
	for _, m := range mustLoad(t) {
		seed, ok := laneSeeds[m.Lane]
		if !ok {
			t.Fatalf("manifest %q has no recorder seed: add one so the corpus covers it rather than skipping it", m.Lane)
		}
		path, pub, fp := writeWorkerKey(t, seed)
		st.keys[m.Lane], st.fps[m.Lane] = path, fp
		st.rootAppend(t, "actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, m.Lane))
		for _, g := range m.Grants {
			st.rootAppend(t, "actor.granted", fp, `{"capability": "`+g+`"}`)
		}
	}
	st.packet = filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(st.packet, []byte(fmt.Sprintf(`{"acceptance": ["resume from here"], "decisions": [], "base": %q, "refs": [], "findings": []}`, st.base+".."+st.head)), 0o644); err != nil {
		t.Fatal(err)
	}
	return st
}

func (st *recorderStand) rootAppend(t *testing.T, verb, subject, payload string) {
	t.Helper()
	if e, code := runEnv(t, "ledger", "append", "--ledger", st.ld, "--key", st.root, "--verb", verb, "--subject", subject, "--payload", payload); code != 0 {
		t.Fatalf("%s %s: %d %+v", verb, subject, code, e)
	}
}

// as runs one CLI act as a lane against the local ledger.
func (st *recorderStand) as(t *testing.T, laneName string, args ...string) (ledgerEnv, int) {
	t.Helper()
	full := append([]string{args[0], args[1], "--ledger", st.ld, "--key", st.keys[laneName]}, args[2:]...)
	return runEnv(t, full...)
}

// admitted asserts the act landed.
func (st *recorderStand) admitted(t *testing.T, laneName string, args ...string) ledgerEnv {
	t.Helper()
	e, code := st.as(t, laneName, args...)
	if code != 0 || !e.OK {
		msg := ""
		if e.Error != nil {
			msg = e.Error.Code + ": " + e.Error.Message
		}
		t.Fatalf("%s: %v must admit: %d %s %+v", laneName, args, code, msg, e.Result)
	}
	return e
}

// refused asserts the act was refused at a stamped position, which is
// what makes it a journaled decision point.
func (st *recorderStand) refused(t *testing.T, laneName string, args ...string) ledgerEnv {
	t.Helper()
	e, code := st.as(t, laneName, args...)
	if code == 0 || e.Error == nil || e.Position == nil {
		msg := ""
		if e.Error != nil {
			msg = e.Error.Code + ": " + e.Error.Message
		}
		t.Fatalf("%s: %v must refuse at a stamped position: %d %s %+v", laneName, args, code, msg, e.Result)
	}
	return e
}

func (st *recorderStand) claim(t *testing.T, laneName, subject string) {
	t.Helper()
	if _, err := admitAppend(t, st.ld, workerRawKey(laneSeeds[laneName]), "claim.taken", subject, `{}`); err != nil {
		t.Fatalf("%s claims %s: %v", laneName, subject, err)
	}
}

func (st *recorderStand) deadEndPosition(t *testing.T, subject string) int {
	t.Helper()
	e, code := runEnv(t, "knowledge", "show", "--ledger", st.ld)
	if code != 0 {
		t.Fatalf("knowledge show: %d %+v", code, e)
	}
	ends, _ := e.Result["dead_ends"].(map[string]any)
	list, _ := ends[subject].([]any)
	if len(list) == 0 {
		t.Fatalf("no dead end on %s: %+v", subject, e.Result)
	}
	pos, _ := list[0].(map[string]any)["position"].(float64)
	return int(pos)
}

// driveRecorderScenario is the one history every corpus file is
// recorded from. Every lane performs its own acts through the CLI's
// boundary verbs (claims through the library seam, the one act the
// CLI refuses offline) and attempts one act the boundary refuses, so
// the journal arm records a refusal for each. Positions and verbs are
// fixed by this function; the frames carry no instant; so the corpus
// is byte-identical across recordings (D3).
func driveRecorderScenario(t *testing.T) *recorderStand {
	t.Helper()
	st := buildRecorderStand(t)
	filed := `{"intent": "recorder drill", "tier": "trivial", "budget": "small", "routing": "core"}`
	acceptance := fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, st.spec)

	// The dispatcher files and specifies five contracts, then revises
	// its triage of c-1 (the seed/4 row).
	for _, subject := range []string{"c-1", "c-2", "c-3", "c-4", "c-5"} {
		st.admitted(t, "dispatcher", "ledger", "append", "--verb", "intent.filed", "--subject", subject, "--payload", filed)
		st.admitted(t, "dispatcher", "ledger", "append", "--verb", "contract.specified", "--subject", subject, "--payload", acceptance)
	}
	st.admitted(t, "dispatcher", "ledger", "append", "--verb", "contract.specified", "--subject", "c-1",
		"--payload", fmt.Sprintf(`{"acceptance": {"ref": "red.md @ %s", "executable": false}}`, st.spec))
	st.refused(t, "dispatcher", "claim", "park", "--subject", "c-1", "--packet", st.packet)

	// The planner claims, proposes and releases c-2 and c-3; the
	// operator approves c-2 unedited and c-3 at a revised plan.
	st.claim(t, "planner", "c-2")
	st.admitted(t, "planner", "plan", "propose", "--subject", "c-2", "--plan", st.anchor1, "--repo", st.src)
	st.admitted(t, "planner", "claim", "release", "--subject", "c-2", "--packet", st.packet)
	if e, code := runEnv(t, "plan", "approve", "--ledger", st.ld, "--key", st.root, "--subject", "c-2", "--plan", st.anchor1, "--pr", "pr/2 @ "+st.spec, "--repo", st.src); code != 0 {
		t.Fatalf("approve c-2: %d %+v", code, e)
	}
	st.claim(t, "planner", "c-3")
	st.admitted(t, "planner", "plan", "propose", "--subject", "c-3", "--plan", "plans/c-3.md @ "+st.base, "--repo", st.src)
	st.admitted(t, "planner", "knowledge", "deadend", "--subject", "c-3", "--tried", "decomposing by module",
		"--outcome", "the modules share one table", "--condition", "the table is the contract", "--environment", "ci-runner/v0")
	st.admitted(t, "planner", "claim", "release", "--subject", "c-3", "--packet", st.packet)
	if e, code := runEnv(t, "plan", "approve", "--ledger", st.ld, "--key", st.root, "--subject", "c-3", "--plan", st.anchor2, "--pr", "pr/3 @ "+st.spec, "--repo", st.src); code != 0 {
		t.Fatalf("approve c-3: %d %+v", code, e)
	}
	st.refused(t, "planner", "claim", "release", "--subject", "c-1", "--packet", st.packet)

	// The implementer works c-2 to a submission with a reservation, a
	// dead end and a supervisor-started run inside the window.
	st.claim(t, "implementer", "c-2")
	st.admitted(t, "implementer", "budget", "reserve", "--subject", "c-2", "--amount", "2")
	st.admitted(t, "implementer", "knowledge", "deadend", "--subject", "c-2", "--tried", "retrying the fetch",
		"--outcome", "the mirror timed out", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")
	st.admitted(t, "supervisor", "run", "start", "--subject", "c-2", "--principal", "acme", "--model", "fam/v1", "--tool-policy", "default")
	st.refused(t, "supervisor", "offer", "publish", "--subject", "c-2", "--expires", "2027-01-01T00:00:00Z")
	st.admitted(t, "supervisor", "offer", "publish", "--subject", "c-5", "--expires", "2027-01-01T00:00:00Z")
	st.admitted(t, "implementer", "submission", "make", "--subject", "c-2", "--packet", st.packet)

	// The verifier passes c-2 and fails c-4; the observer lands c-2.
	st.admitted(t, "verifier", "verdict", "render", "--subject", "c-2", "--repo", st.src, "--verdict", "pass")
	st.admitted(t, "implementer", "merge", "request", "--subject", "c-2")
	st.admitted(t, "observer", "merge", "observe", "--subject", "c-2", "--merged", st.head, "--pr", "pr/2")
	st.claim(t, "implementer", "c-4")
	st.admitted(t, "implementer", "submission", "make", "--subject", "c-4", "--packet", st.packet)
	st.admitted(t, "verifier", "verdict", "render", "--subject", "c-4", "--repo", st.src, "--verdict", "fail")
	st.refused(t, "observer", "merge", "observe", "--subject", "c-4", "--merged", st.head, "--pr", "pr/4")

	// c-5: claimed by the implementer, reached for by the verifier and
	// the maintenance actor, reaped by the latter.
	st.claim(t, "implementer", "c-5")
	st.refused(t, "verifier", "submission", "make", "--subject", "c-5", "--packet", st.packet)
	st.admitted(t, "maintenance", "ledger", "append", "--verb", "claim.reaped", "--subject", "c-5",
		"--payload", fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["reaped"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, claimFence(t, st.ld, "c-5"), st.base+".."+st.head))

	// c-3: a dead end, an over-reservation, a park.
	st.claim(t, "implementer", "c-3")
	st.admitted(t, "implementer", "knowledge", "deadend", "--subject", "c-3", "--tried", "warming the mirror",
		"--outcome", "the mirror stayed cold", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")
	st.refused(t, "implementer", "budget", "reserve", "--subject", "c-3", "--amount", "1000000")
	st.admitted(t, "implementer", "claim", "park", "--subject", "c-3", "--packet", st.packet)

	// The curator proposes over two holders' dead ends (the planner's
	// on c-3, the implementer's on c-2), and again; the
	// maintenance actor, whose operator standing reaches every queue
	// act, reaches for the one grant with no fallback and is refused.
	claim := "record the mirror's temperature before retrying the fetch"
	propose := []string{"knowledge", "propose", "--claim", claim, "--applies-when", `{"routing": "core"}`,
		"--support", fmt.Sprintf("c-2@%d", st.deadEndPosition(t, "c-2")), "--support", fmt.Sprintf("c-3@%d", st.deadEndPosition(t, "c-3"))}
	st.admitted(t, "curator", propose...)
	st.refused(t, "curator", propose...)
	st.refused(t, "maintenance", propose...)
	return st
}

// record records one lane's trajectory through the CLI and returns
// the file's bytes and the envelope.
func (st *recorderStand) record(t *testing.T, laneName, lanesDir string) ([]byte, ledgerEnv) {
	t.Helper()
	out := filepath.Join(t.TempDir(), laneName+".json")
	e, code := runEnv(t, "trajectory", "record", "--ledger", st.ld, "--key", st.keys[laneName], "--lane", laneName, "--lanes", lanesDir, "--out", out)
	if code != 0 {
		t.Fatalf("record %s: %d %+v", laneName, code, e)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b, e
}

func (st *recorderStand) replay(t *testing.T, laneName, file, lanesDir string) (ledgerEnv, int) {
	t.Helper()
	return runEnv(t, "trajectory", "replay", file, "--ledger", st.ld, "--key", st.keys[laneName], "--lanes", lanesDir)
}

func writeTemp(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// copyDir copies the shipped lanes so a drill can plant a change.
func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func plantManifest(t *testing.T, dir, laneName string, edit func(m map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, laneName+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// conformance: AC3 — the corpus holds exactly one file per manifest in
// lanes, the set derived from the directory; every lane has at
// least one admitted and one refused point (a lane with none would be
// reported as configuration-only); and the recorder drill reproduces
// every file byte for byte from the rebuilt scenario.
func TestTrajectoryCorpusIsTheRecorderScenario(t *testing.T) {
	st := driveRecorderScenario(t)
	lanes := shippedLanes(t)
	corpus := corpusDir(t)
	if *updateCorpus {
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	expected := map[string]bool{}
	for _, m := range mustLoad(t) {
		expected[m.Lane+".json"] = true
		b, e := st.record(t, m.Lane, lanes)
		admitted, _ := e.Result["admitted"].(float64)
		refused, _ := e.Result["refused"].(float64)
		if admitted == 0 && refused == 0 {
			t.Logf("%s: configuration-only (no act in the tree): digests recorded, no points", m.Lane)
		}
		if admitted < 1 || refused < 1 {
			t.Errorf("%s: every shipped lane acts in this tree, so its trajectory carries at least one admitted and one refused point: %v admitted, %v refused", m.Lane, admitted, refused)
		}
		if e.Result["actor"] != st.fps[m.Lane] {
			t.Errorf("%s: the trajectory is the lane's own: %v", m.Lane, e.Result["actor"])
		}
		path := filepath.Join(corpus, m.Lane+".json")
		if *updateCorpus {
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: the corpus lacks the file the scenario records (run with -update to record it): %v", m.Lane, err)
		}
		if string(committed) != string(b) {
			t.Errorf("%s: the committed trajectory differs from the recorded one: the scenario, the boundary or the configuration moved; re-record on purpose with -update", m.Lane)
		}
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !expected[e.Name()] {
			t.Errorf("the corpus carries %s, which no shipped manifest names", e.Name())
		}
	}
	if len(entries) != len(expected) {
		t.Errorf("the corpus holds %d files for %d manifests", len(entries), len(expected))
	}
}

// conformance: AC3, AC2 — every corpus file replays green against the
// shipped configuration over the rebuilt chain, every point same and
// both digests equal; the planted rows fail it with the named classes:
// a manifest without submission make, a manifest without claim, a
// manifest whose orients_from alone changed, a fragment with one
// added line; and a configuration-only lane diverges on its manifest
// digest.
func TestTrajectoryCorpusReplaysGreenAndPlantedRowsDiverge(t *testing.T) {
	st := driveRecorderScenario(t)
	lanes := shippedLanes(t)
	corpus := corpusDir(t)
	files := map[string]string{}
	for _, m := range mustLoad(t) {
		file := filepath.Join(corpus, m.Lane+".json")
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("%s: %v", m.Lane, err)
		}
		files[m.Lane] = file
		e, code := st.replay(t, m.Lane, file, lanes)
		points, _ := e.Result["points"].(float64)
		same, _ := e.Result["same"].(float64)
		if code != 0 || !e.OK || points != same || e.Result["manifest_changed"] != false || e.Result["posture_changed"] != false {
			t.Fatalf("%s: the corpus replays green against the shipped configuration: %d %+v", m.Lane, code, e)
		}
	}

	diverges := func(laneName, dir, class string) ledgerEnv {
		t.Helper()
		e, code := st.replay(t, laneName, files[laneName], dir)
		if code != 26 || e.Error == nil || e.Error.Code != "trajectory_diverged" || !strings.Contains(e.Error.Message, class) {
			t.Fatalf("%s: a planted %s refuses 26 trajectory_diverged naming it: %d %+v", laneName, class, code, e)
		}
		return e
	}
	sameCount := func(e ledgerEnv) (float64, float64) {
		points, _ := e.Result["points"].(float64)
		same, _ := e.Result["same"].(float64)
		return points, same
	}

	// A manifest without submission make: the implementer's submission
	// points are undeclared.
	dir := copyDir(t, lanes)
	plantManifest(t, dir, "implementer", func(m map[string]any) {
		m["acts_through"] = []any{"claim take", "budget reserve", "budget settle", "budget release", "claim release", "claim park"}
		m["liveness_from"] = []any{"claim take", "budget settle"}
	})
	e := diverges("implementer", dir, "act_undeclared")
	if points, same := sameCount(e); same == 0 || same >= points {
		t.Fatalf("only the submission points are undeclared: %+v", e.Result)
	}

	// A manifest without claim: every admitted planner act is
	// ungranted; its one refused attempt is judged by its frame.
	dir = copyDir(t, lanes)
	plantManifest(t, dir, "planner", func(m map[string]any) { m["grants"] = []any{} })
	e = diverges("planner", dir, "act_ungranted")
	if _, same := sameCount(e); same != 1 {
		t.Fatalf("with no grants nothing the planner did is granted, and the refusal keeps its frame: %+v", e.Result)
	}

	// orients_from alone: manifest_changed, every point same.
	dir = copyDir(t, lanes)
	plantManifest(t, dir, "observer", func(m map[string]any) {
		m["orients_from"] = "seed situation --ledger <dir> --key <key> --since <position>"
	})
	e = diverges("observer", dir, "manifest digest")
	if points, same := sameCount(e); same != points || e.Result["manifest_changed"] != true || e.Result["posture_changed"] != false {
		t.Fatalf("an orients_from edit is manifest_changed with every point same: %+v", e.Result)
	}

	// One added line in a shared fragment: posture_changed for every
	// lane composed from it, every point same.
	dir = copyDir(t, lanes)
	frag := filepath.Join(dir, lane.FragmentDir, "common", "one-inbox.md")
	b, err := os.ReadFile(frag)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frag, append(b, "\nOne more line.\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	e = diverges("verifier", dir, "posture digest")
	if points, same := sameCount(e); same != points || e.Result["posture_changed"] != true || e.Result["manifest_changed"] != false {
		t.Fatalf("a fragment edit is posture_changed with every point same: %+v", e.Result)
	}

	// A configuration-only lane: a fresh ledger where the observer
	// signed nothing records no points and still diverges on its
	// manifest digest.
	dir2, priv, _ := writeKeys(t)
	ld := filepath.Join(dir2, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+version.Seed1+`"}`); code != 0 {
		t.Fatalf("upgrade: %d %+v", code, e)
	}
	out := filepath.Join(t.TempDir(), "observer.json")
	e, code := runEnv(t, "trajectory", "record", "--ledger", ld, "--key", st.keys["observer"], "--lane", "observer", "--lanes", lanes, "--out", out)
	if code != 0 || e.Result["points"] != float64(0) {
		t.Fatalf("a lane that signed nothing records a configuration-only trajectory: %d %+v", code, e)
	}
	if e, code := runEnv(t, "trajectory", "replay", out, "--ledger", ld, "--key", st.keys["observer"], "--lanes", lanes); code != 0 {
		t.Fatalf("unchanged: green: %d %+v", code, e)
	}
	dir = copyDir(t, lanes)
	plantManifest(t, dir, "observer", func(m map[string]any) { m["summary"] = "changed" })
	if e, code := runEnv(t, "trajectory", "replay", out, "--ledger", ld, "--key", st.keys["observer"], "--lanes", dir); code != 26 || e.Error == nil || e.Error.Code != "trajectory_diverged" || e.Result["manifest_changed"] != true {
		t.Fatalf("a configuration-only lane still diverges on its manifest: %d %+v", code, e)
	}
}

// conformance: III.J row 3 and the report — the recorder scenario's
// own chain carries one re-specification among five specified
// contracts and two measured approvals, one unedited and one edited,
// so the report's lanes section reads 0.200 and 0.500 over it.
func TestRecorderScenarioReportsTheLaneMetrics(t *testing.T) {
	st := driveRecorderScenario(t)
	out := filepath.Join(t.TempDir(), "views")
	unlockForCleanup(t, out)
	if e, code := runEnv(t, "project", "rebuild", "--ledger", st.ld, "--out", out); code != 0 {
		t.Fatalf("rebuild: %d %+v", code, e)
	}
	cur, err := os.ReadFile(filepath.Join(out, "report", "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(filepath.Join(out, "report", "builds", strings.TrimSpace(string(cur)), "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"specified": 5`, `"respecified": 1`, `"retriage_rate": "0.200"`, `"approvals": 2`, `"unedited": 1`, `"edited": 1`, `"unmeasured": 0`, `"unedited_rate": "0.500"`} {
		if !strings.Contains(string(report), want) {
			t.Errorf("the report carries %s: %s", want, report)
		}
	}
}

// conformance: the trajectory verbs' envelopes: usage names the shape;
// a missing journal is an empty one; a torn journal refuses as
// unreadable; a key that is not the recorded actor's refuses; replay
// of an unparseable file refuses.
func TestTrajectoryVerbEnvelopes(t *testing.T) {
	st := driveRecorderScenario(t)
	lanes := shippedLanes(t)
	for _, args := range [][]string{
		{"trajectory"},
		{"trajectory", "sing"},
		{"trajectory", "record", "--ledger", st.ld},
		{"trajectory", "record", "--ledger", st.ld, "--key", st.keys["planner"]},
		{"trajectory", "replay", "--ledger", st.ld, "--key", st.keys["planner"]},
	} {
		if e, code := runEnv(t, args...); code != 64 || e.Error == nil || e.Error.Code != "usage" {
			t.Fatalf("%v: usage: %d %+v", args, code, e)
		}
	}
	b, e := st.record(t, "planner", lanes)
	if e.Result["out"] == nil || e.Position == nil {
		t.Fatalf("the record envelope names the file and stamps the tip: %+v", e)
	}
	// Without --out the trajectory rides the envelope.
	if e, code := runEnv(t, "trajectory", "record", "--ledger", st.ld, "--key", st.keys["planner"], "--lane", "planner", "--lanes", lanes); code != 0 || e.Result["trajectory"] == nil {
		t.Fatalf("without --out the trajectory is carried: %d %+v", code, e)
	}
	// Another lane's key cannot replay the planner's trajectory.
	file := writeTemp(t, "planner.json", b)
	if e, code := runEnv(t, "trajectory", "replay", file, "--ledger", st.ld, "--key", st.keys["implementer"], "--lanes", lanes); code != 5 || e.Error == nil || !strings.Contains(e.Error.Message, "its own key") {
		t.Fatalf("a lane replays its own trajectory with its own key: %d %+v", code, e)
	}
	// An unparseable file refuses as unreadable.
	bad := writeTemp(t, "bad.json", []byte(`{"lane": "planner"}`))
	if e, code := runEnv(t, "trajectory", "replay", bad, "--ledger", st.ld, "--key", st.keys["planner"], "--lanes", lanes); code != 66 || e.Error == nil || e.Error.Code != "unreadable" {
		t.Fatalf("a malformed trajectory refuses unreadable: %d %+v", code, e)
	}
	// A torn journal refuses the recording rather than omitting points.
	journal := filepath.Join(st.ld, "attempts.jsonl")
	orig, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, append([]byte("garbage\n"), orig...), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "trajectory", "record", "--ledger", st.ld, "--key", st.keys["planner"], "--lane", "planner", "--lanes", lanes); code != 66 || e.Error == nil || e.Error.Code != "unreadable" {
		t.Fatalf("a journal that does not load refuses: %d %+v", code, e)
	}
	// A missing journal is an empty one: the admitted points remain.
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "trajectory", "record", "--ledger", st.ld, "--key", st.keys["planner"], "--lane", "planner", "--lanes", lanes)
	if code != 0 || e.Result["refused"] != float64(0) || e.Result["admitted"] == float64(0) {
		t.Fatalf("without a journal the chain's points remain: %d %+v", code, e)
	}
}

// conformance: AC1's owed arm at the terminal — the frame's owed kinds
// are the situation read's rows for the actor on the subject: the
// implementer's last point on c-3 (the park) is framed inside its own
// window, where the situation owes it the claim.
func TestTrajectoryFrameOwedMatchesTheSituation(t *testing.T) {
	st := driveRecorderScenario(t)
	b, _ := st.record(t, "implementer", shippedLanes(t))
	var traj struct {
		Points []struct {
			Verb    string `json:"verb"`
			Subject string `json:"subject"`
			Frame   struct {
				Owed []string `json:"owed"`
			} `json:"frame"`
		} `json:"points"`
	}
	if err := json.Unmarshal(b, &traj); err != nil {
		t.Fatal(err)
	}
	var owedAtPark []string
	for _, p := range traj.Points {
		if p.Verb == "claim.parked" && p.Subject == "c-3" {
			owedAtPark = p.Frame.Owed
		}
	}
	if len(owedAtPark) == 0 {
		t.Fatalf("the park was decided inside the implementer's window, where a claim is owed: %+v", traj.Points)
	}
	// The situation at the park's prefix cannot be read after the park
	// landed, so the drill compares against the kinds the read gives on
	// the subject the implementer still holds: c-5 was reaped, c-2
	// landed, and the parked c-3 owes nothing to the holder now; the
	// kinds owed inside a window are the same on every held subject,
	// so a fresh claim on c-1 reads them.
	st.claim(t, "implementer", "c-1")
	e, code := runEnv(t, "situation", "--ledger", st.ld, "--key", st.keys["implementer"], "--subject", "c-1")
	if code != 0 {
		t.Fatalf("situation: %d %+v", code, e)
	}
	rows, _ := e.Result["obligations"].([]any)
	var kinds []string
	for _, r := range rows {
		kind, _ := r.(map[string]any)["kind"].(string)
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	if strings.Join(kinds, ",") != strings.Join(owedAtPark, ",") {
		t.Fatalf("the frame's owed kinds are the situation's rows: frame %v, situation %v", owedAtPark, kinds)
	}
}

var _ ed25519.PrivateKey
