package flywheel

// The flywheel drills (plans/os-9075c308.md; spec/flywheel.md):
// shapes are record-derivable and recurrence is counted (AC1); the
// draft is deterministic, copies gated commands only, and refuses
// divergent anchors (AC2); validation runs through the v1 engine from
// a staging worktree that leaves nothing behind (AC3, gated on the
// engine being present); the fold binds admitted facts only and the
// metrics derive from the record (AC5); the repair filing is the
// bounded contract the spec describes (AC7).

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/plan"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

var update = flag.Bool("update", false, "rewrite the golden drafts under testdata")

const (
	zeros40 = "0000000000000000000000000000000000000000"
	zeros64 = "0000000000000000000000000000000000000000000000000000000000000000"
	packet  = `{"acceptance": ["done"], "decisions": [], "base": "` + zeros40 + `..` + zeros40 + `", "refs": [], "findings": []}`
)

// chain is a genesis-rooted, signed ledger with the lanes enrolled and
// granted, read back as records: the fold re-judges every flywheel
// fact against the keyring at its position, so the chain carries one.
type chain struct {
	t       *testing.T
	store   *ledger.Store
	loose   ledger.Resolver
	keys    map[string]ed25519.PrivateKey
	records []*event.Record
}

var laneCaps = map[string]string{"worker": keyring.CapClaim, "curator": keyring.CapCurate, "observer": keyring.CapObserver,
	"verifier": keyring.CapVerdict, "dispatcher": keyring.CapDispatch}

var laneOrder = []string{"worker", "curator", "observer", "verifier", "dispatcher"}

func keyOf(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func fpOf(t testing.TB, priv ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func enrollJSON(t testing.TB, priv ed25519.PrivateKey, name string) string {
	t.Helper()
	return fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(priv.Public().(ed25519.PublicKey)), name)
}

func newChain(t *testing.T) *chain {
	t.Helper()
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &chain{t: t, store: store, keys: map[string]ed25519.PrivateKey{"root": keyOf(t, 1)}}
	for i, name := range laneOrder {
		c.keys[name] = keyOf(t, byte(10+i))
	}
	g, err := genesis.Init(store, c.keys["root"], nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(g)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(g.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	c.loose = func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range c.keys {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	c.reload()
	c.addV("root", version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, name := range laneOrder {
		c.add("root", keyring.VerbEnrolled, fpOf(t, c.keys[name]), enrollJSON(t, c.keys[name], name))
		c.add("root", keyring.VerbGranted, fpOf(t, c.keys[name]), `{"capability": "`+laneCaps[name]+`"}`)
	}
	return c
}

func (c *chain) reload() {
	c.t.Helper()
	c.records = nil
	if err := c.store.Records(func(pos int, rec *event.Record) error {
		c.records = append(c.records, rec)
		return nil
	}); err != nil {
		c.t.Fatal(err)
	}
}

// addV signs and appends one record by the named lane at the version
// and returns its position. No rule runs: the boundary is the admit
// package's, and this package's claim is about what the fold binds.
func (c *chain) addV(actor, v, verb, subject, payload string) int {
	c.t.Helper()
	tip, _, err := c.store.Tip()
	if err != nil {
		c.t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: v, TS: "2026-09-01T01:00:00Z", Actor: fpOf(c.t, c.keys[actor]), Verb: verb, Subject: subject,
		Payload: json.RawMessage(payload), Prev: tip,
	}, c.keys[actor])
	if err != nil {
		c.t.Fatal(err)
	}
	pos := len(c.records)
	if _, err := c.store.Append(rec, c.loose); err != nil {
		c.t.Fatal(err)
	}
	c.reload()
	return pos
}

func (c *chain) add(actor, verb, subject, payload string) int {
	return c.addV(actor, version.Seed1, verb, subject, payload)
}

func (c *chain) fp(actor string) string { return fpOf(c.t, c.keys[actor]) }

// spec is a contract.specified payload: executable content, gated
// when asked.
func spec(path, commit string, gated bool) string {
	if gated {
		return fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true, "gate": "pr/1 @ %s"}}`, path, commit, commit)
	}
	return fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true}}`, path, commit)
}

// done drives one contract to done: filed, specified, optionally
// planned, claimed, submitted, passed, requested, observed. Returns
// the position of the merge observation.
func (c *chain) done(id, intent, routing, tier, path, commit string, gated, planned bool) int {
	c.add("root", "intent.filed", id, fmt.Sprintf(`{"intent": %q, "tier": %q, "budget": "small", "routing": %q}`, intent, tier, routing))
	c.add("root", "contract.specified", id, spec(path, commit, gated))
	if planned {
		c.add("worker", transition.PlanProposedVerb, id, `{"plan": "plans/`+id+`.md @ `+zeros40+`"}`)
	}
	fence := c.add("worker", "claim.taken", id, `{}`)
	sub := c.add("worker", "submission.made", id, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
	verdict := c.add("verifier", transition.VerdictRenderedVerb, id, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, sub))
	c.add("worker", "merge.requested", id, fmt.Sprintf(`{"verdict": "%d"}`, verdict))
	return c.add("observer", "merge.observed", id, `{"merged": "`+zeros40+`", "pr": "pr/`+id+`"}`)
}

// failed drives one contract to a failed verdict.
func (c *chain) failed(id, routing, tier, path, commit string) {
	c.add("root", "intent.filed", id, fmt.Sprintf(`{"intent": "again", "tier": %q, "budget": "small", "routing": %q}`, tier, routing))
	c.add("root", "contract.specified", id, spec(path, commit, true))
	fence := c.add("worker", "claim.taken", id, `{}`)
	sub := c.add("worker", "submission.made", id, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
	c.add("verifier", transition.VerdictRenderedVerb, id, fmt.Sprintf(`{"verdict": "fail", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, sub))
}

func foldOf(t *testing.T, c *chain) *transition.Fold {
	t.Helper()
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	return table.FoldRecords(c.records)
}

// The chore's sequence: the trivial loop without a plan.
var choreSequence = []string{"intent.filed", "contract.specified", "claim.taken", "submission.made", transition.VerdictRenderedVerb, "merge.requested", "merge.observed"}

// pinnedShape is the id of {routing core, accept.md, trivial,
// choreSequence}: the JCS form is the contract, so the id is pinned
// as a literal, not recomputed through the function under test.
const pinnedShape = "s-7d016a5b521d"

// conformance: AC1 — shapes derive from the record alone: routing,
// acceptance path, tier and the verb sequence; two done contracts
// with those equal fold to one shape with two occurrences in done
// order; a different path, routing or sequence is another shape; a
// failed contract is no occurrence; the id is the JCS form's hash.
func TestShapesAreRecordDerivableAndRecurrenceIsCounted(t *testing.T) {
	c := newChain(t)
	d1 := c.done("c-1", "fix the check", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.failed("c-x", "core", "trivial", "accept.md", "aaaa111")
	d3 := c.done("c-3", "other", "core", "trivial", "other.md", "aaaa111", true, false)
	d2 := c.done("c-2", "fix the check again", "core", "trivial", "accept.md", "bbbb222", true, false)
	c.done("c-4", "planned", "core", "trivial", "accept.md", "aaaa111", true, true)
	c.done("c-5", "elsewhere", "docs", "trivial", "accept.md", "aaaa111", false, false)
	c.done("c-6", "bigger", "core", "standard", "accept.md", "aaaa111", true, false)
	c.add("root", "system.checkpoint", "system", `{}`)
	shapes := Shapes(c.records, foldOf(t, c))
	if len(shapes) != 5 {
		t.Fatalf("five shapes: %d %+v", len(shapes), shapes)
	}
	chore := shapes[0]
	if chore.ID != pinnedShape || chore.Routing != "core" || chore.AcceptancePath != "accept.md" || chore.Tier != "trivial" || strings.Join(chore.Sequence, ",") != strings.Join(choreSequence, ",") {
		t.Fatalf("the chore's shape is its fields and the pinned id: %+v", chore)
	}
	if !chore.Recurring() || len(chore.Occurrences) != 2 {
		t.Fatalf("two done contracts recur: %+v", chore.Occurrences)
	}
	a, b := chore.Occurrences[0], chore.Occurrences[1]
	if a.Subject != "c-1" || a.Done != d1 || b.Subject != "c-2" || b.Done != d2 || a.Anchor != "aaaa111" || b.Anchor != "bbbb222" || !a.Gated || !b.Gated || a.Intent != "fix the check" || a.Ref != "accept.md @ aaaa111" {
		t.Fatalf("occurrences in done order with their anchors and intents: %+v", chore.Occurrences)
	}
	if a.Cite() != fmt.Sprintf("c-1@%d", d1) {
		t.Fatalf("a citation is subject@done: %s", a.Cite())
	}
	if chore.Name() != "chore-"+pinnedShape[2:10] {
		t.Fatalf("the name is chore-<prefix>: %s", chore.Name())
	}
	other := shapes[1]
	if other.AcceptancePath != "other.md" || other.Recurring() || len(other.Occurrences) != 1 || other.Occurrences[0].Done != d3 {
		t.Fatalf("a different acceptance path is another shape, once: %+v", other)
	}
	planned := shapes[2]
	if len(planned.Sequence) != len(choreSequence)+1 || planned.ID == chore.ID || planned.Recurring() {
		t.Fatalf("a planned sequence is another shape: %+v", planned)
	}
	docs := shapes[3]
	if docs.Routing != "docs" || docs.Occurrences[0].Gated {
		t.Fatalf("routing distinguishes, and an ungated acceptance is recorded ungated: %+v", docs)
	}
	if bigger := shapes[4]; bigger.Tier != "standard" || bigger.ID == chore.ID || bigger.Recurring() {
		t.Fatalf("tier distinguishes: %+v", bigger)
	}
	for _, s := range shapes {
		if s.ID != shapeID(s.Routing, s.AcceptancePath, s.Tier, s.Sequence) {
			t.Fatalf("an id is its own form's hash: %+v", s)
		}
		for _, o := range s.Occurrences {
			if o.Subject == "c-x" {
				t.Fatal("a failed contract is no occurrence")
			}
		}
	}
	if _, ok := Find(shapes, "s-nope"); ok {
		t.Fatal("an unknown id is not found")
	}
	if s, ok := Find(shapes, pinnedShape); !ok || s.ID != pinnedShape {
		t.Fatal("the chore is found by id")
	}
	if shapeID("core", "a", "trivial", nil) != shapeID("core", "a", "trivial", []string{}) {
		t.Fatal("a nil sequence is the empty sequence")
	}
}

// conformance: review finding on the task PR — the lifecycle fold is
// tolerant, so a chore is counted from AUTHENTIC completions only: two
// chains of transition-legal events pushed past the boundary reach
// "done" and manufacture no shape, whether the verdict was rendered by
// a key holding no verdict grant, by the implementer itself, or the
// merge observed by a key the row does not accept.
func TestRawChainsManufactureNoChore(t *testing.T) {
	// The worker renders its own pass and observes its own merge: every
	// event is transition-legal and none of them passed a grant check.
	self := newChain(t)
	for _, id := range []string{"c-1", "c-2"} {
		self.add("root", "intent.filed", id, `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`)
		self.add("root", "contract.specified", id, spec("accept.md", "aaaa111", true))
		fence := self.add("worker", "claim.taken", id, `{}`)
		sub := self.add("worker", "submission.made", id, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
		v := self.add("worker", transition.VerdictRenderedVerb, id, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, sub))
		self.add("worker", "merge.requested", id, fmt.Sprintf(`{"verdict": "%d"}`, v))
		self.add("observer", "merge.observed", id, `{"merged": "`+zeros40+`", "pr": "pr/`+id+`"}`)
	}
	fold := foldOf(t, self)
	for _, id := range []string{"c-1", "c-2"} {
		if s, ok := fold.State(id); !ok || s.State != "done" {
			t.Fatalf("the tolerant fold does reach done on %s, which is why the check is needed: %+v", id, s)
		}
	}
	if shapes := Shapes(self.records, fold); len(shapes) != 0 {
		t.Fatalf("a self-rendered pass is no authentic completion, so it folds to no shape: %+v", shapes)
	}
	if m := Derive(self.records, fold); m.Recurring != 0 || m.ConversionRate != nil {
		t.Fatalf("and it counts toward no metric: %+v", m)
	}

	// The verdict is a granted verifier's, but the merge observation is
	// the worker's, which the row does not accept.
	obs := newChain(t)
	for _, id := range []string{"c-1", "c-2"} {
		obs.add("root", "intent.filed", id, `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`)
		obs.add("root", "contract.specified", id, spec("accept.md", "aaaa111", true))
		fence := obs.add("worker", "claim.taken", id, `{}`)
		sub := obs.add("worker", "submission.made", id, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
		v := obs.add("verifier", transition.VerdictRenderedVerb, id, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, sub))
		obs.add("worker", "merge.requested", id, fmt.Sprintf(`{"verdict": "%d"}`, v))
		obs.add("worker", "merge.observed", id, `{"merged": "`+zeros40+`", "pr": "pr/`+id+`"}`)
	}
	if shapes := Shapes(obs.records, foldOf(t, obs)); len(shapes) != 0 {
		t.Fatalf("a merge observed by a key the row does not accept is no authentic completion: %+v", shapes)
	}

	// The same chain with the observer's observation is one chore: the
	// drill is not passing because everything is refused.
	good := newChain(t)
	good.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	good.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	if shapes := Shapes(good.records, foldOf(t, good)); len(shapes) != 1 || !shapes[0].Recurring() {
		t.Fatalf("the admitted chain is the chore: %+v", shapes)
	}
}

// conformance: review finding on the task PR — a repair's pass is
// re-judged at its position, so a transition-legal pass raw-pushed by
// the implementer leaves the repair OPEN and the proposal refused;
// citing it is refused too.
func TestRawRepairVerdictLeavesTheRepairOpen(t *testing.T) {
	c := newChain(t)
	c.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	chore := Shapes(c.records, foldOf(t, c))[0]
	subject := RepairSubject(chore)
	c.add("dispatcher", "intent.filed", subject, fmt.Sprintf(`{"intent": "repair", "tier": "trivial", "budget": "small", "routing": %q}`, chore.Routing))
	c.add("dispatcher", "contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true, "gate": "seed/flywheel-%s @ %s"}}`, RepairAcceptancePath(chore.ID), zeros40, chore.ID, zeros40))
	fence := c.add("worker", "claim.taken", subject, `{}`)
	sub := c.add("worker", "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
	// The implementer renders its own pass: legal by the table, and no
	// verifier grant, no disjointness, nothing scored.
	raw := c.add("worker", transition.VerdictRenderedVerb, subject, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, sub))
	fold := foldOf(t, c)
	if s, _ := fold.State(subject); s.Verdict == nil || s.Verdict.Verdict != "pass" {
		t.Fatal("the tolerant fold does carry the raw pass, which is why the check is needed")
	}
	open, passed := Repairs(c.records, fold, chore.ID)
	if len(open) != 1 || open[0] != subject || len(passed) != 0 {
		t.Fatalf("a raw pass leaves the repair open: %v %v", open, passed)
	}
	if err := CheckProposal(c.records, fold, chore.ID, proposal(chore, "wf-1", "")); gateOf(t, err) != GateRepairOpen {
		t.Fatalf("and the proposal stays refused: %v", err)
	}
	if err := CheckProposal(c.records, fold, chore.ID, proposal(chore, "wf-1", fmt.Sprintf("%s@%d", subject, raw))); gateOf(t, err) != GateRepairOpen {
		t.Fatalf("citing it changes nothing: %v", err)
	}
	// The verifier's pass, at the same position in a second chain, is
	// authentic and clears it.
	ok := newChain(t)
	ok.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	ok.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	okSubject, verdict := ok.repair(chore, true)
	okFold := foldOf(t, ok)
	if _, passed := Repairs(ok.records, okFold, chore.ID); len(passed) != 1 || passed[0].Subject != okSubject || passed[0].Verdict != verdict {
		t.Fatalf("the verifier's pass is authentic: %v", passed)
	}
}

// conformance: AC1 — the recurrence figure is the charter's, pinned to
// the sentence in the spec as the tier table is pinned to tiers.md.
func TestRecurringAfterIsTheCharterFigure(t *testing.T) {
	if RecurringAfter != 2 {
		t.Fatalf("every chore an agent does twice becomes infrastructure: %d", RecurringAfter)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "spec", "flywheel.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("the constant is\n**`"+fmt.Sprint(RecurringAfter)+"`**")) {
		t.Fatalf("spec/flywheel.md states the constant %d beside the charter's sentence", RecurringAfter)
	}
	one := newChain(t)
	one.done("c-1", "once", "core", "trivial", "accept.md", "aaaa111", true, false)
	if s := Shapes(one.records, foldOf(t, one)); len(s) != 1 || s[0].Recurring() {
		t.Fatalf("one done contract is not a chore: %+v", s)
	}
}

// git runs one command in the fixture repository.
func gitIn(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// hardenGitRepo writes the no-auto-gc settings into a fixture
// repository's own config, before its first object, so no detached
// git process outlives the test that made the repository
// (plans/os-c4e8b57a.md D1: written, never passed as -c).
// internal/gitref/fixture_guard_test.go holds every creation site in
// next/** to it.
func hardenGitRepo(t *testing.T, repo string) {
	t.Helper()
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"}} {
		gitIn(t, repo, "config", kv[0], kv[1])
	}
}

func writeIn(t *testing.T, repo, name, content string) {
	t.Helper()
	full := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	acceptA = "# Green\n\n## Validation Commands\n\n- Boundary: `printf ok`\n- Retention: `test -f hello.txt`\n"
	acceptB = "# Green, later: the title changes, the commands do not\n\n## Validation Commands\n\n- Boundary: `printf ok`\n- Retention: `test -f hello.txt`\n"
	acceptC = "# Changed\n\n## Validation Commands\n\n- Boundary: `printf changed`\n"
)

// fixtureRepo is a repository whose acceptance spec stands at three
// commits: a and b carry the same commands (b differs in its title;
// inside the commands section every line is a command, so prose
// there would diverge), c a different list.
func fixtureRepo(t *testing.T) (repo, a, b, c string) {
	t.Helper()
	repo = t.TempDir()
	gitIn(t, repo, "init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	writeIn(t, repo, "hello.txt", "hello\n")
	writeIn(t, repo, "accept.md", acceptA)
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "a")
	a = gitIn(t, repo, "rev-parse", "HEAD")
	writeIn(t, repo, "accept.md", acceptB)
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "b")
	b = gitIn(t, repo, "rev-parse", "HEAD")
	writeIn(t, repo, "accept.md", acceptC)
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "c")
	c = gitIn(t, repo, "rev-parse", "HEAD")
	return repo, a, b, c
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden draft:\n%s", name, got)
	}
}

// conformance: AC2 — the draft is a pure function of the shape and
// the gated anchors' command lists: byte-identical across calls and
// across occurrences whose commands agree, one run step per command
// in order, each depending on the previous, the verdict step last;
// inputs exactly the fields that vary; the file is the golden text.
func TestDraftIsDeterministicAndCopiesGatedCommandsInOrder(t *testing.T) {
	repo, a, b, _ := fixtureRepo(t)
	c := newChain(t)
	c.done("c-1", "fix the check", "core", "trivial", "accept.md", a, true, false)
	c.done("c-2", "fix the check", "core", "trivial", "accept.md", a, true, false)
	shapes := Shapes(c.records, foldOf(t, c))
	d, err := DraftWorkflow(shapes[0], repo)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != shapes[0].Name() || d.Shape != shapes[0].ID || d.Path() != RegistryDir+"/"+d.Name+".yaml" {
		t.Fatalf("the draft names the shape and the registry path: %+v", d)
	}
	if strings.Join(d.Commands, "|") != "printf ok|test -f hello.txt" || len(d.Inputs) != 0 {
		t.Fatalf("the commands are the spec's, in order, and equal intents and anchors vary nothing: %+v", d)
	}
	again, err := DraftWorkflow(shapes[0], repo)
	if err != nil || !bytes.Equal(again.Bytes, d.Bytes) {
		t.Fatalf("two drafts of one shape are byte-identical: %v", err)
	}
	text := string(d.Bytes)
	for _, want := range []string{
		"name: " + d.Name + "\n",
		"  - id: implement\n    role: implementer\n",
		"  - id: check-1\n    run: 'printf ok'\n    depends_on: [implement]\n",
		"  - id: check-2\n    run: 'test -f hello.txt'\n    depends_on: [check-1]\n",
		"  - id: verdict\n    role: reviewer\n    tools: readonly\n    depends_on: [check-2]\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the draft lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "inputs:") || strings.Contains(text, "id: plan") {
		t.Fatalf("no inputs and no plan step for an unplanned shape with equal occurrences:\n%s", text)
	}
	golden(t, "chore.yaml", d.Bytes)

	// Varying intents and anchors: the same commands at a and b, two
	// intents, so both inputs appear and the prompts reference them;
	// the planned sequence gains the plan step the implementer
	// consumes.
	p := newChain(t)
	p.done("p-1", "fix the check", "core", "standard", "accept.md", a, true, true)
	p.done("p-2", "fix the other check", "core", "standard", "accept.md", b, true, true)
	ps := Shapes(p.records, foldOf(t, p))
	pd, err := DraftWorkflow(ps[0], repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pd.Inputs, ",") != "goal,anchor" {
		t.Fatalf("intents and anchors vary, so goal and anchor are the inputs: %v", pd.Inputs)
	}
	ptext := string(pd.Bytes)
	for _, want := range []string{
		"inputs:\n  - {name: goal,", "  - {name: anchor,",
		"  - id: plan\n    role: planner\n", "{{inputs.goal}}", "{{inputs.anchor}}",
		"    depends_on: [plan]\n    consumes: [plan]\n", "{{output.plan.path}}",
		"  - id: check-1\n    run: 'printf ok'\n    depends_on: [implement]\n",
	} {
		if !strings.Contains(ptext, want) {
			t.Fatalf("the planned draft lacks %q:\n%s", want, ptext)
		}
	}
	if strings.Contains(ptext, "fix the check") || strings.Contains(ptext, "fix the other check") {
		t.Fatal("an intent text is an input's value, never copied into a prompt")
	}
	golden(t, "chore-planned.yaml", pd.Bytes)

	// A command carrying a single quote is carried verbatim under
	// YAML's one escape.
	if yamlSingle("echo 'hi'") != "'echo ''hi'''" {
		t.Fatalf("single quotes double: %s", yamlSingle("echo 'hi'"))
	}
	// A spec-less shape refuses rather than drafting an empty run.
	empty := newChain(t)
	empty.done("e-1", "x", "core", "trivial", "hello.txt", a, true, false)
	empty.done("e-2", "x", "core", "trivial", "hello.txt", a, true, false)
	if _, err := DraftWorkflow(Shapes(empty.records, foldOf(t, empty))[0], repo); err == nil || !strings.Contains(err.Error(), "no validation command") {
		t.Fatalf("an acceptance without commands has no deterministic step to draft: %v", err)
	}
	if _, err := DraftWorkflow(Shape{ID: "s-none"}, repo); err == nil {
		t.Fatal("a shape without occurrences cannot be drafted")
	}
}

// conformance: AC2 — the drafter reads gated content only and refuses
// occurrences whose gated anchors disagree; an anchor the repository
// lacks is a refusal, not a guess.
func TestDraftRefusesUngatedDivergentAndMissingAnchors(t *testing.T) {
	repo, a, _, c := fixtureRepo(t)
	un := newChain(t)
	un.done("u-1", "x", "core", "trivial", "accept.md", a, true, false)
	un.done("u-2", "x", "core", "trivial", "accept.md", a, false, false)
	var ungated *UngatedError
	if _, err := DraftWorkflow(Shapes(un.records, foldOf(t, un))[0], repo); !errors.As(err, &ungated) || ungated.Subject != "u-2" {
		t.Fatalf("an ungated occurrence refuses by name: %v", err)
	}
	dv := newChain(t)
	dv.done("d-1", "x", "core", "trivial", "accept.md", a, true, false)
	dv.done("d-2", "x", "core", "trivial", "accept.md", c, true, false)
	var divergent *DivergentError
	if _, err := DraftWorkflow(Shapes(dv.records, foldOf(t, dv))[0], repo); !errors.As(err, &divergent) || divergent.A != "accept.md @ "+a || divergent.B != "accept.md @ "+c {
		t.Fatalf("different command lists at two anchors refuse divergent naming both: %v", err)
	}
	if !strings.Contains(divergent.Error(), "must never run commands copied from another") {
		t.Fatalf("the refusal says why: %v", divergent)
	}
	miss := newChain(t)
	miss.done("m-1", "x", "core", "trivial", "accept.md", a, true, false)
	miss.done("m-2", "x", "core", "trivial", "accept.md", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", true, false)
	var anchor *AnchorError
	if _, err := DraftWorkflow(Shapes(miss.records, foldOf(t, miss))[0], repo); !errors.As(err, &anchor) || !strings.Contains(anchor.Ref, "deadbeef") {
		t.Fatalf("an unresolvable anchor refuses: %v", err)
	}
}

// conformance: AC3 — the mock run binds every declared input to a
// placeholder, read from the file in both layouts a registry file
// can take.
func TestDeclaredInputsReadsFlowAndBlockForms(t *testing.T) {
	flow := "name: x\ninputs:\n  - {name: goal, type: string, required: true}\n  - {name: anchor, type: string}\nsteps:\n  - id: a\n    run: 'true'\n"
	block := "name: x\n# a comment\ninputs:\n  - name: goal\n    type: string\n  - name: \"anchor\"\n    type: string\nsteps:\n  - id: a\n    run: 'true'\n"
	for _, body := range []string{flow, block} {
		if got := declaredInputs([]byte(body)); strings.Join(got, ",") != "goal,anchor" {
			t.Fatalf("declared inputs %v from\n%s", got, body)
		}
	}
	if got := declaredInputs([]byte("name: x\nsteps:\n  - id: a\n    run: 'true'\n")); len(got) != 0 {
		t.Fatalf("no inputs block, no inputs: %v", got)
	}
}

// instantiate makes the fixture repository a seed instantiation: the
// v1 contract, the bootstrap shim, the harness dispatcher and the
// adapters (the mock among them), committed. These four paths are
// what the engine's mock run reads from the repository it runs in.
func instantiate(t *testing.T, repo string) {
	t.Helper()
	for _, rel := range []string{".seed", "scripts/seed", "scripts/seed-harness", "scripts/harness"} {
		copyPath(t, filepath.Join(sourceTree, rel), filepath.Join(repo, rel))
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "seed: instantiate")
}

func copyPath(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// sourceTree is the repository this package lives in: the shim and
// the engine lock that instantiate copies into a fixture come from
// it, so it answers whether the engine is available before any
// fixture exists.
var sourceTree = filepath.Join("..", "..")

// requireEngine skips by name when the v1 engine cannot be invoked
// (D3): the drill's claim is about the engine, so it is never faked.
// It reads the pin from the source tree and runs before any fixture
// is built: a skip that has already built a repository leaves a git
// process to race t.TempDir's cleanup, and on macOS it lost
// (os-222189a3), so a skip builds nothing.
func requireEngine(t *testing.T) {
	t.Helper()
	if reason, ok := EngineAvailable(sourceTree); !ok {
		t.Skipf("the v1 engine is not available: %s", reason)
	}
}

// conformance: AC3 — a draft is validated by the v1 engine's own
// validate and mock run from a detached staging worktree: the
// caller's checkout gains no registry file, the worktree and the run
// directory are gone afterwards, a taken name refuses before staging,
// the engine's refusal names the stage, the step and the finding, and
// the branch's file is validated as it stands.
func TestValidationRunsThroughTheEngineFromAStagingWorktree(t *testing.T) {
	requireEngine(t)
	repo, a, _, _ := fixtureRepo(t)
	instantiate(t, repo)
	if reason, ok := EngineAvailable(repo); !ok {
		t.Fatalf("the instantiated copy resolves no engine though the tree it was copied from does: %s", reason)
	}
	base := gitIn(t, repo, "rev-parse", "HEAD")
	c := newChain(t)
	c.done("c-1", "fix the check", "core", "trivial", "accept.md", a, true, false)
	c.done("c-2", "fix the other check", "core", "trivial", "accept.md", a, true, false)
	shape := Shapes(c.records, foldOf(t, c))[0]
	d, err := DraftWorkflow(shape, repo)
	if err != nil {
		t.Fatal(err)
	}
	if d.Inputs[0] != "goal" {
		t.Fatalf("the intents vary: %v", d.Inputs)
	}
	v, err := ValidateDraft(repo, base, d)
	if err != nil {
		t.Fatalf("the engine accepts the draft: %v", err)
	}
	if v.Name != d.Name || !strings.HasPrefix(v.RunID, "wf-") || strings.Join(v.Steps, ",") != "check-1,check-2,implement,verdict" {
		t.Fatalf("the validation names the mock run and its steps: %+v", v)
	}
	if _, err := os.Stat(filepath.Join(repo, d.Path())); !os.IsNotExist(err) {
		t.Fatal("the caller's checkout gains no registry file")
	}
	if wt := gitIn(t, repo, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("the staging worktree is removed: %s", wt)
	}
	if runs, _ := os.ReadDir(filepath.Join(repo, ".git", "seed-runs")); len(runs) != 0 {
		t.Fatalf("the mock run's directory is removed: %v", runs)
	}
	if gitIn(t, repo, "status", "--porcelain") != "" {
		t.Fatal("validation leaves the checkout clean")
	}

	// A taken name refuses before anything is staged.
	writeIn(t, repo, RegistryDir+"/"+d.Name+".yaml", "name: taken\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "take the name")
	taken := gitIn(t, repo, "rev-parse", "HEAD")
	var nameTaken *NameTakenError
	if _, err := ValidateDraft(repo, taken, d); !errors.As(err, &nameTaken) || nameTaken.Name != d.Name {
		t.Fatalf("a registry already holding the name refuses: %v", err)
	}
	if !NameTaken(repo, taken, d.Name) || NameTaken(repo, base, d.Name) {
		t.Fatal("NameTaken reads the registry at the commit")
	}

	// The engine's refusal at validate: an unknown dependency names
	// the step; at mock: an unsatisfiable produced schema fails the
	// step. Both are EngineErrors naming stage, step and finding.
	rename := func(name, from, to string) []byte {
		return []byte(strings.NewReplacer("name: "+d.Name+"\n", "name: "+name+"\n", from, to).Replace(string(d.Bytes)))
	}
	broken := &Draft{Name: "chore-broken", Shape: shape.ID, Bytes: rename("chore-broken", "depends_on: [check-1]", "depends_on: [nope]")}
	var refused *EngineError
	if _, err := ValidateDraft(repo, base, broken); !errors.As(err, &refused) || refused.Stage != "validate" || refused.Step != "check-2" || !strings.Contains(refused.Finding, "nope") {
		t.Fatalf("an invalid workflow refuses at validate naming the step: %v", err)
	}
	unstubbable := &Draft{Name: "chore-unstubbable", Shape: shape.ID, Bytes: rename("chore-unstubbable", "verdict: {type: string}", "verdict: {type: string, minLength: 4000}")}
	if _, err := ValidateDraft(repo, base, unstubbable); !errors.As(err, &refused) || refused.Stage != "mock" || refused.Step != "verdict" || refused.Finding == "" {
		t.Fatalf("a mock run that fails a step refuses at mock naming the step: %v", err)
	}
	if !strings.Contains(refused.Error(), "the engine refused chore-unstubbable at mock: step verdict:") {
		t.Fatalf("the refusal reads stage and step: %v", refused)
	}

	// The branch's file as it stands: committed on a branch, validated
	// at that commit without regeneration; a branch without the file
	// refuses.
	gitIn(t, repo, "checkout", "--quiet", "-b", "seed/flywheel-"+shape.ID, base)
	writeIn(t, repo, d.Path(), string(d.Bytes))
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "propose")
	head := gitIn(t, repo, "rev-parse", "HEAD")
	gitIn(t, repo, "checkout", "--quiet", "main")
	at, err := ValidateAt(repo, base, head, d.Name)
	if err != nil || at.Name != d.Name {
		t.Fatalf("the branch's file validates as it stands: %v", err)
	}
	if _, err := ValidateAt(repo, base, base, d.Name); err == nil || !strings.Contains(err.Error(), "holds no") {
		t.Fatalf("a commit without the file cannot be validated: %v", err)
	}
	if _, err := ValidateAt(repo, taken, head, d.Name); !errors.As(err, &nameTaken) {
		t.Fatalf("the name is checked against the base: %v", err)
	}
	if wt := gitIn(t, repo, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("every staging worktree is removed: %s", wt)
	}
}

// conformance: AC3 — the engine's presence is detected, not assumed:
// SEED_ENGINE names an executable or the shim's cache holds the
// pinned release; otherwise the reason is named for the skip.
func TestEngineAvailabilityIsDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executability is by existence on Windows, so a non-executable engine file is not observable there (spec/platform.md)")
	}
	bare := t.TempDir()
	t.Setenv("SEED_ENGINE", "")
	if reason, ok := EngineAvailable(bare); ok || !strings.Contains(reason, "scripts/seed") {
		t.Fatalf("a repository without the shim has no engine: %q %v", reason, ok)
	}
	writeIn(t, bare, "scripts/seed", "#!/bin/sh\n")
	if reason, ok := EngineAvailable(bare); ok || !strings.Contains(reason, "engine.lock") {
		t.Fatalf("a repository without the lock has no engine: %q %v", reason, ok)
	}
	writeIn(t, bare, ".seed/engine.lock", "repo x/y\n")
	if reason, ok := EngineAvailable(bare); ok || !strings.Contains(reason, "no version") {
		t.Fatalf("a lock naming no version has no engine: %q %v", reason, ok)
	}
	writeIn(t, bare, ".seed/engine.lock", "version v0.0.0-nonexistent\n")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if reason, ok := EngineAvailable(bare); ok || !strings.Contains(reason, "v0.0.0-nonexistent") {
		t.Fatalf("an empty cache has no engine and names the pin: %q %v", reason, ok)
	}
	notExec := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(notExec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEED_ENGINE", notExec)
	if reason, ok := EngineAvailable(bare); ok || !strings.Contains(reason, "not executable") {
		t.Fatalf("SEED_ENGINE must be executable: %q %v", reason, ok)
	}
	if err := os.Chmod(notExec, 0o755); err != nil {
		t.Fatal(err)
	}
	if reason, ok := EngineAvailable(bare); !ok {
		t.Fatalf("an executable SEED_ENGINE is the engine: %q", reason)
	}
}

// proposal is a shape-valid proposal for the chain's first shape.
func proposal(shape Shape, run string, repair string) *Proposal {
	p := &Proposal{Shape: shape.ID, Workflow: RegistryDir + "/" + shape.Name() + ".yaml @ " + zeros40, Repair: repair}
	for _, o := range shape.Occurrences {
		p.Occurrences = append(p.Occurrences, o.Cite())
	}
	p.Validated.Run = run
	return p
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gateOf(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("a flywheel refusal names its gate, got %v", err)
	}
	return e.Gate
}

// conformance: AC5 — the payloads are strict, the subject is the
// shape, the path is a file directly under the registry, the
// occurrences and the repair are citations.
func TestProposalAndMergeParseStrictly(t *testing.T) {
	good := `{"shape": "s-1", "workflow": ".seed/workflows/x.yaml @ abc", "occurrences": ["c-1@5", "c-2@9"], "validated": {"run": "wf-1"}}`
	if p, err := ParseProposal("s-1", []byte(good)); err != nil || p.Shape != "s-1" || len(p.Occurrences) != 2 || p.Validated.Run != "wf-1" {
		t.Fatalf("the good proposal parses: %v %+v", err, p)
	}
	for name, c := range map[string]struct{ subject, raw, gate string }{
		"unknown field": {"s-1", `{"shape": "s-1", "workflow": "a @ b", "occurrences": ["c-1@5"], "validated": {"run": "wf-1"}, "extra": 1}`, GateShape},
		"trailing":      {"s-1", good + ` {}`, GateShape},
		"off subject":   {"s-2", good, GateShape},
		"no run":        {"s-1", `{"shape": "s-1", "workflow": "a @ b", "occurrences": ["c-1@5"], "validated": {"run": " "}}`, GateShape},
		"bad path":      {"s-1", `{"shape": "s-1", "workflow": "x.yaml", "occurrences": ["c-1@5"], "validated": {"run": "wf-1"}}`, GatePath},
		"no occurrence": {"s-1", `{"shape": "s-1", "workflow": "a @ b", "occurrences": [], "validated": {"run": "wf-1"}}`, GateOccurrences},
		"bad cite":      {"s-1", `{"shape": "s-1", "workflow": "a @ b", "occurrences": ["c-1"], "validated": {"run": "wf-1"}}`, GateOccurrences},
		"negative cite": {"s-1", `{"shape": "s-1", "workflow": "a @ b", "occurrences": ["c-1@-1"], "validated": {"run": "wf-1"}}`, GateOccurrences},
		"bad repair":    {"s-1", `{"shape": "s-1", "workflow": "a @ b", "occurrences": ["c-1@5"], "validated": {"run": "wf-1"}, "repair": "r"}`, GateRepairCited},
	} {
		_, err := ParseProposal(c.subject, []byte(c.raw))
		if g := gateOf(t, err); g != c.gate {
			t.Fatalf("%s: gate %s, want %s (%v)", name, g, c.gate, err)
		}
		if !strings.Contains(err.Error(), "spec/flywheel.md") {
			t.Fatalf("%s: the refusal cites the spec: %v", name, err)
		}
	}
	goodMerge := `{"workflow": ".seed/workflows/x.yaml @ abc", "shape": "s-1", "pr": "pr/7 @ abc"}`
	if m, err := ParseMerge("s-1", []byte(goodMerge)); err != nil || m.PR != "pr/7 @ abc" {
		t.Fatalf("the good merge parses: %v", err)
	}
	for name, raw := range map[string]string{
		"unknown field": `{"workflow": "a @ b", "shape": "s-1", "pr": "p @ c", "x": 1}`,
		"off subject":   `{"workflow": "a @ b", "shape": "s-2", "pr": "p @ c"}`,
		"bad workflow":  `{"workflow": "a", "shape": "s-1", "pr": "p @ c"}`,
		"bad pr":        `{"workflow": "a @ b", "shape": "s-1", "pr": "p"}`,
		// One observation names one merged revision (review finding on
		// the task PR): a file at one commit and a PR at another
		// describes no merge that happened.
		"two revisions": `{"workflow": "a @ b", "shape": "s-1", "pr": "p @ c"}`,
	} {
		if _, err := ParseMerge("s-1", []byte(raw)); gateOf(t, err) != GateMerge {
			t.Fatalf("%s: refuses at the merge gate: %v", name, err)
		}
	}
}

// conformance: AC5, D4 — a proposal is held to the record: the path
// under the registry, the occurrences distinct done contracts folding
// to the shape, at least RecurringAfter of them, no standing proposal,
// and the repair contract's state; a merge cites the standing
// proposal's file.
func TestProposalAndMergeAreHeldToTheRecord(t *testing.T) {
	c := newChain(t)
	c.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.done("c-3", "y", "core", "trivial", "other.md", "aaaa111", true, false)
	c.failed("c-x", "core", "trivial", "accept.md", "aaaa111")
	c.add("root", "intent.filed", "c-open", `{"intent": "open", "tier": "trivial", "budget": "small", "routing": "core"}`)
	fold := foldOf(t, c)
	shapes := Shapes(c.records, fold)
	chore, other := shapes[0], shapes[1]
	check := func(p *Proposal) error { return CheckProposal(c.records, fold, p.Shape, p) }
	if err := check(proposal(chore, "wf-1", "")); err != nil {
		t.Fatalf("the well-formed proposal on the recurring shape admits: %v", err)
	}
	cases := map[string]struct {
		mutate func(p *Proposal)
		gate   string
		reason string
	}{
		"nested path":       {func(p *Proposal) { p.Workflow = RegistryDir + "/sub/x.yaml @ " + zeros40 }, GatePath, "directly under"},
		"outside registry":  {func(p *Proposal) { p.Workflow = "workflows/x.yaml @ " + zeros40 }, GatePath, "directly under"},
		"not yaml":          {func(p *Proposal) { p.Workflow = RegistryDir + "/x.yml @ " + zeros40 }, GatePath, "directly under"},
		"unknown shape":     {func(p *Proposal) { p.Shape = "s-nope" }, GateOccurrences, "no done contract folds"},
		"one occurrence":    {func(p *Proposal) { p.Occurrences = p.Occurrences[:1] }, GateOccurrences, "a chore is what an agent did twice"},
		"twice cited":       {func(p *Proposal) { p.Occurrences = []string{p.Occurrences[0], p.Occurrences[0]} }, GateOccurrences, "cited twice"},
		"other shape's":     {func(p *Proposal) { p.Occurrences[1] = other.Occurrences[0].Cite() }, GateOccurrences, "not a done contract folding to shape"},
		"failed contract":   {func(p *Proposal) { p.Occurrences[1] = "c-x@4" }, GateOccurrences, "not done"},
		"open contract":     {func(p *Proposal) { p.Occurrences[1] = "c-open@0" }, GateOccurrences, "not done"},
		"wrong position":    {func(p *Proposal) { p.Occurrences[1] = "c-2@1" }, GateOccurrences, "not a done contract folding to shape"},
		"unknown contract":  {func(p *Proposal) { p.Occurrences[1] = "c-9@1" }, GateOccurrences, "not a done contract folding to shape"},
		"repair not passed": {func(p *Proposal) { p.Repair = "c-1@3" }, GateRepairCited, "not a repair contract"},
	}
	for name, tc := range cases {
		p := proposal(chore, "wf-1", "")
		tc.mutate(p)
		err := check(p)
		if g := gateOf(t, err); g != tc.gate || !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("%s: gate %s %v, want %s containing %q", name, g, err, tc.gate, tc.reason)
		}
	}
	// A merge observation before any proposal has nothing to cite.
	m := &Merge{Workflow: RegistryDir + "/" + chore.Name() + ".yaml @ " + zeros40, Shape: chore.ID, PR: "pr/7 @ " + zeros40}
	if err := CheckMerge(c.records, fold, chore.ID, m); gateOf(t, err) != GateMerge || !strings.Contains(err.Error(), "no unmerged proposal stands") {
		t.Fatalf("no standing proposal, no merge, and the refusal says so: %v", err)
	}
	// The proposal appended: a second refuses as a duplicate, the
	// merge admits on the proposed file only, and after the merge a
	// new proposal admits again while a second merge does not.
	c.add("curator", ProposedVerb, chore.ID, mustJSON(t, proposal(chore, "wf-1", "")))
	fold = foldOf(t, c)
	if err := check(proposal(chore, "wf-2", "")); gateOf(t, err) != GateDuplicate {
		t.Fatalf("a standing proposal refuses another: %v", err)
	}
	wrong := *m
	wrong.Workflow = RegistryDir + "/elsewhere.yaml @ " + zeros40
	if err := CheckMerge(c.records, fold, chore.ID, &wrong); gateOf(t, err) != GateMerge || !strings.Contains(err.Error(), "not the proposed") {
		t.Fatalf("the observed file is the proposed one: %v", err)
	}
	if err := CheckMerge(c.records, fold, chore.ID, m); err != nil {
		t.Fatalf("the merge of the proposed file admits: %v", err)
	}
	c.add("observer", MergedVerb, chore.ID, mustJSON(t, m))
	fold = foldOf(t, c)
	if err := check(proposal(chore, "wf-2", "")); err != nil {
		t.Fatalf("after the merge a new proposal admits: %v", err)
	}
	if err := CheckMerge(c.records, fold, chore.ID, m); gateOf(t, err) != GateMerge || !strings.Contains(err.Error(), "no unmerged proposal stands") {
		t.Fatalf("a merged proposal is not standing, and the refusal says so: %v", err)
	}
}

// repairContract files a repair contract for the shape on the chain,
// optionally driving it to a passed verdict; returns the verdict
// position (or -1).
func (c *chain) repair(shape Shape, pass bool) (subject string, verdict int) {
	subject = RepairSubject(shape)
	c.add("dispatcher", "intent.filed", subject, fmt.Sprintf(`{"intent": "repair", "tier": "trivial", "budget": "small", "routing": %q}`, shape.Routing))
	c.add("dispatcher", "contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true, "gate": "seed/flywheel-%s @ %s"}}`, RepairAcceptancePath(shape.ID), zeros40, shape.ID, zeros40))
	if !pass {
		return subject, -1
	}
	fence := c.add("worker", "claim.taken", subject, `{}`)
	sub := c.add("worker", "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
	verdict = c.add("verifier", transition.VerdictRenderedVerb, subject, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, zeros64, sub))
	return subject, verdict
}

// conformance: AC7, D7 — a repair contract short of a passed verdict
// blocks the proposal; passed, the proposal must cite it at its
// verdict position; the filing is trivial tier, small budget, the
// shape's routing, its acceptance gated at the branch commit and its
// commands the engine's two verbs.
func TestRepairIsABoundedContractTheProposalCites(t *testing.T) {
	c := newChain(t)
	c.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	fold := foldOf(t, c)
	chore := Shapes(c.records, fold)[0]
	if RepairSubject(chore) != "repair-"+chore.ID[2:10] || RepairAcceptancePath(chore.ID) != transition.FlywheelRoot+"/"+chore.ID+"/accept.md" {
		t.Fatalf("the repair's subject and acceptance path derive from the shape: %s %s", RepairSubject(chore), RepairAcceptancePath(chore.ID))
	}
	subject, _ := c.repair(chore, false)
	fold = foldOf(t, c)
	if s, ok := fold.State(subject); !ok || s.State != "ready" {
		t.Fatalf("the repair contract is filed and ready: %+v", s)
	}
	if id, ok := IsRepair(mustState(t, fold, subject)); !ok || id != chore.ID {
		t.Fatalf("a repair is recognized by its acceptance path: %s %v", id, ok)
	}
	if _, ok := IsRepair(mustState(t, fold, "c-1")); ok {
		t.Fatal("a chore is no repair")
	}
	if _, ok := IsRepair(transition.SubjectState{}); ok {
		t.Fatal("no acceptance, no repair")
	}
	if err := CheckProposal(c.records, fold, chore.ID, proposal(chore, "wf-1", "")); gateOf(t, err) != GateRepairOpen || !strings.Contains(err.Error(), subject) {
		t.Fatalf("an open repair blocks the proposal by name: %v", err)
	}
	// Driven to a passed verdict: the proposal cites it, at the
	// verdict's position, or refuses.
	c2 := newChain(t)
	c2.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c2.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	subject2, verdict := c2.repair(chore, true)
	fold2 := foldOf(t, c2)
	if err := CheckProposal(c2.records, fold2, chore.ID, proposal(chore, "wf-1", "")); gateOf(t, err) != GateRepairCited || !strings.Contains(err.Error(), "does not cite") {
		t.Fatalf("a passed repair must be cited: %v", err)
	}
	if err := CheckProposal(c2.records, fold2, chore.ID, proposal(chore, "wf-1", fmt.Sprintf("%s@%d", subject2, verdict-1))); gateOf(t, err) != GateRepairCited {
		t.Fatalf("the citation is the verdict's position: %v", err)
	}
	if err := CheckProposal(c2.records, fold2, chore.ID, proposal(chore, "wf-1", fmt.Sprintf("%s@%d", subject2, verdict))); err != nil {
		t.Fatalf("the passed repair cited at its verdict admits: %v", err)
	}
	// The filing's shape.
	d := &Draft{Name: chore.Name(), Shape: chore.ID}
	e := &EngineError{Name: d.Name, Stage: "mock", Step: "verdict", Finding: "step verdict produce verdict violates its schema"}
	acceptance := RepairAcceptance(d, e)
	text := string(acceptance)
	for _, want := range []string{"- Failing step: `verdict`", "- Finding: step verdict produce verdict violates its schema", "## Validation Commands", d.Path()} {
		if !strings.Contains(text, want) {
			t.Fatalf("the acceptance quotes the failing step and the finding and names the commands: lacks %q\n%s", want, text)
		}
	}
	if cmds := plan.Commands(acceptance); strings.Join(cmds, "|") != strings.Join(RepairCommands(d.Name, nil), "|") || len(cmds) != 2 {
		t.Fatalf("the acceptance's commands are the engine's two verbs: %v", cmds)
	}
	withInputs := RepairCommands(d.Name, []string{"goal", "anchor"})
	if len(withInputs) != 2 || !strings.HasSuffix(withInputs[1], "--mock --input goal=placeholder --input anchor=placeholder") {
		t.Fatalf("the mock run binds the declared inputs as the validation does: %v", withInputs)
	}
	if !strings.Contains(string(RepairAcceptance(d, &EngineError{Stage: "validate"})), "(no step named)") {
		t.Fatal("a refusal naming no step says so")
	}
	intent, specBytes := RepairFiling(chore, d, e, "seed/flywheel-"+chore.ID, "abc1234")
	var filed struct {
		Intent, Tier, Budget, Routing string
	}
	if err := json.Unmarshal(intent, &filed); err != nil || filed.Tier != transition.TrivialTier || filed.Budget != "small" || filed.Routing != "core" || !strings.Contains(filed.Intent, chore.ID) || !strings.Contains(filed.Intent, e.Finding) {
		t.Fatalf("the filing is trivial, small, the shape's routing, naming the finding: %v %+v", err, filed)
	}
	var sp struct {
		Acceptance struct {
			Ref        string
			Executable bool
			Gate       string
		}
	}
	if err := json.Unmarshal(specBytes, &sp); err != nil || sp.Acceptance.Ref != RepairAcceptancePath(chore.ID)+" @ abc1234" || !sp.Acceptance.Executable || sp.Acceptance.Gate != "seed/flywheel-"+chore.ID+" @ abc1234" {
		t.Fatalf("the acceptance is executable and gated at the branch commit: %v %+v", err, sp)
	}
}

func mustState(t *testing.T, fold *transition.Fold, subject string) transition.SubjectState {
	t.Helper()
	s, ok := fold.State(subject)
	if !ok {
		t.Fatalf("no state for %s", subject)
	}
	return s
}

// conformance: AC5 — the fold binds a proposal or a merge only when it
// passed the boundary at its own position; raw facts count as
// anomalies and bind nothing; the metrics derive from the record.
func TestFoldBindsAdmittedFactsOnlyAndMetricsDerive(t *testing.T) {
	c := newChain(t)
	fold := foldOf(t, c)
	if m := Derive(c.records, fold); m.Recurring != 0 || m.ConversionRate != nil {
		t.Fatalf("an empty record has no rate: %+v", m)
	}
	c.done("c-1", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.done("c-2", "x", "core", "trivial", "accept.md", "aaaa111", true, false)
	c.done("c-3", "y", "core", "trivial", "other.md", "aaaa111", true, false)
	c.done("c-4", "y", "core", "trivial", "other.md", "aaaa111", true, false)
	fold = foldOf(t, c)
	shapes := Shapes(c.records, fold)
	chore, other := shapes[0], shapes[1]
	if m := Derive(c.records, fold); m.Recurring != 2 || m.Proposed != 0 || m.Merged != 0 || *m.ConversionRate != "0.000" {
		t.Fatalf("two recurring shapes, none proposed: %+v", m)
	}
	// A raw proposal: one occurrence, so the boundary would refuse;
	// the fold counts it and binds nothing.
	raw := proposal(chore, "wf-raw", "")
	raw.Occurrences = raw.Occurrences[:1]
	c.add("curator", ProposedVerb, chore.ID, mustJSON(t, raw))
	// A malformed one too, and a well-formed one under the worker's
	// key: the grant is re-judged at the position, so a raw push by a
	// key without curate binds nothing either.
	c.add("curator", ProposedVerb, chore.ID, `{"shape": "`+chore.ID+`"}`)
	c.add("worker", ProposedVerb, chore.ID, mustJSON(t, proposal(chore, "wf-worker", "")))
	st := Fold(c.records)
	if st.Anomalies != 3 || st.Any() || len(st.Proposals) != 0 {
		t.Fatalf("raw proposals are anomalies, never bound: %+v", st)
	}
	if _, ok := st.Standing(chore.ID); ok {
		t.Fatal("nothing stands")
	}
	good := proposal(chore, "wf-1", "")
	pPos := c.add("curator", ProposedVerb, chore.ID, mustJSON(t, good))
	st = Fold(c.records)
	standing, ok := st.Standing(chore.ID)
	if !ok || standing.Pos != pPos || standing.Actor != c.fp("curator") || standing.Path() != RegistryDir+"/"+chore.Name()+".yaml" || st.Anomalies != 3 {
		t.Fatalf("the admitted proposal stands at its position: %+v %v", standing, ok)
	}
	if strings.Join(st.Shapes(), ",") != chore.ID {
		t.Fatalf("the shapes with a fact, in order: %v", st.Shapes())
	}
	fold = foldOf(t, c)
	if m := Derive(c.records, fold); m.Proposed != 1 || m.Merged != 0 || *m.ConversionRate != "0.000" {
		t.Fatalf("one proposed: %+v", m)
	}
	// A raw merge on the other shape (no proposal) is an anomaly; the
	// admitted merge binds and clears the standing proposal.
	c.add("observer", MergedVerb, other.ID, mustJSON(t, &Merge{Workflow: RegistryDir + "/x.yaml @ " + zeros40, Shape: other.ID, PR: "pr/1 @ " + zeros40}))
	mPos := c.add("observer", MergedVerb, chore.ID, mustJSON(t, &Merge{Workflow: standing.Path() + " @ " + zeros40, Shape: chore.ID, PR: "pr/1 @ " + zeros40}))
	// A merge under the worker's key, well-formed, is an anomaly too.
	c.add("worker", MergedVerb, chore.ID, mustJSON(t, &Merge{Workflow: standing.Path() + " @ " + zeros40, Shape: chore.ID, PR: "pr/1 @ " + zeros40}))
	st = Fold(c.records)
	if st.Anomalies != 5 || !st.Merged(chore.ID) || st.Merged(other.ID) || st.Merges[chore.ID][0].Pos != mPos || len(st.Merges[chore.ID]) != 1 {
		t.Fatalf("the merge binds on the proposed shape only: %+v", st)
	}
	if _, ok := st.Standing(chore.ID); ok {
		t.Fatal("a merged proposal no longer stands")
	}
	fold = foldOf(t, c)
	m := Derive(c.records, fold)
	if m.Recurring != 2 || m.Proposed != 1 || m.Merged != 1 || *m.ConversionRate != "0.500" || m.Repairs.Filed != 0 {
		t.Fatalf("one of two converted: %+v", m)
	}
	// Repairs: filed counts, done counts.
	c.repair(other, false)
	fold = foldOf(t, c)
	if m := Derive(c.records, fold); m.Repairs.Filed != 1 || m.Repairs.Done != 0 {
		t.Fatalf("a filed repair counts: %+v", m)
	}
	// The JSON shape of the section.
	b, _ := json.Marshal(m)
	for _, key := range []string{`"recurring":2`, `"proposed":1`, `"merged":1`, `"repairs":{"filed":0,"done":0}`, `"conversion_rate":"0.500"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("the section lacks %s: %s", key, b)
		}
	}
	// A pre-keyring version carries no flywheel fact.
	old := []*event.Record{{Event: event.Event{V: "seed/0", Verb: ProposedVerb, Subject: "s-1", Payload: json.RawMessage(`{}`)}}}
	if st := Fold(old); st.Anomalies != 0 || st.Any() {
		t.Fatalf("seed/0 records are not the flywheel's: %+v", st)
	}
}
