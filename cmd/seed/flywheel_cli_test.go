package main

// The flywheel at the CLI (plans/os-9075c308.md; spec/flywheel.md):
// shapes, draft, propose, repair, observe and status through run(),
// the branch write that never touches main, the engine arm gated by
// name, and the repair flow from the planted break to the proposal
// that cites the passed contract.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// envText renders an envelope's refusal, or its result, for a failure.
func envText(e ledgerEnv) string {
	if e.Error != nil {
		return e.Error.Code + ": " + e.Error.Message
	}
	return fmt.Sprintf("%+v", e.Result)
}

func gitAt(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func copyInto(t *testing.T, from, to string) {
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

// instantiateSource makes the fixture repository a seed instantiation
// (the v1 contract, the shim, the harness dispatcher and adapters) and
// vendors the engine into its lock, so the shim finds the binary under
// the verdict runner's scrubbed environment as an air-gapped
// deployment would. Returns the new head.
func instantiateSource(t *testing.T, src string) string {
	t.Helper()
	root := filepath.Join("..", "..")
	// The flywheel's conversion target is a v1 workflow: drafted into
	// .seed/workflows, validated against v1's workflow schema, and run by
	// the pinned engine the shim execs (spec/flywheel.md, "The draft is a
	// v1 workflow"). This repository is the successor standing on its own
	// and carries no .seed tree, so the drills that need one cannot run
	// here. Giving the flywheel a Seed-native target is the open design
	// item; until it lands these skip rather than pretend
	// (decisions/0008-flywheel-target.md).
	if _, err := os.Stat(filepath.Join(root, ".seed")); err != nil {
		t.Skip("no v1 tree to instantiate: the flywheel's draft target is still a v1 workflow (decisions/0008-flywheel-target.md)")
	}
	for _, rel := range []string{".seed", "scripts/seed", "scripts/seed-harness", "scripts/harness"} {
		copyInto(t, filepath.Join(root, rel), filepath.Join(src, rel))
	}
	if bin, _, ok := flywheel.EnginePath(src); ok {
		lock := filepath.Join(src, ".seed", "engine.lock")
		b, err := os.ReadFile(lock)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lock, append(b, []byte("vendor "+bin+"\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitAt(t, src, "add", ".")
	gitAt(t, src, "commit", "--quiet", "-m", "seed: instantiate")
	return gitAt(t, src, "rev-parse", "HEAD")
}

// plantHarnessBreak makes the mock harness fail on the verdict step:
// the one break a deterministic draft can meet, since its steps are
// the acceptance's own commands and the roles' stubs.
func plantHarnessBreak(t *testing.T, src string) string {
	t.Helper()
	mock := filepath.Join(src, "scripts", "harness", "mock")
	if err := os.Rename(mock, mock+".orig"); err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\n# planted: the reviewer's harness is broken for the verdict step\nif [ \"${SEED_STEP:-}\" = \"verdict\" ]; then echo \"planted break at verdict\" >&2; exit 1; fi\nexec \"$(dirname \"$0\")/mock.orig\" \"$@\"\n"
	if err := os.WriteFile(mock, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	gitAt(t, src, "add", ".")
	gitAt(t, src, "commit", "--quiet", "-m", "plant: the verdict step's harness fails")
	return gitAt(t, src, "rev-parse", "HEAD")
}

type flywheelCLI struct {
	ld, priv, src, base, spec, head string
	keys, fps                       map[string]string
	rootKey                         ed25519.PrivateKey
}

// raw stages one background fact through the library under the root's
// key (the CLI refuses to draft exclusive verbs offline) and returns
// its position. Acts whose signer the flywheel re-judges (the verdict
// and the merge observation of a counted occurrence) are staged under
// their own lane's key by rawAs, never here: a chore is counted from
// admitted completions, so a fixture that signed them with the root
// would be staging exactly the raw chain the boundary refuses to count.
func (s *flywheelCLI) raw(t *testing.T, verb, subject, payload string) int {
	t.Helper()
	return verdictLibAppend(t, s.ld, s.rootKey, verb, subject, payload)
}

// rawAs stages one background fact under a named lane's key. It signs
// and appends through the library rather than `ledger append`, because
// claiming is online-only at the CLI and a local ledger has no remote
// to claim against; the resolver knows the fixture's own lanes.
func (s *flywheelCLI) rawAs(t *testing.T, lane, verb, subject, payload string) int {
	t.Helper()
	key := s.laneKey(t, lane)
	store, err := ledger.Open(s.ld)
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
		V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	genesisResolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, s.resolver(t, genesisResolve)); err != nil {
		t.Fatalf("library append %s by %s: %v", verb, lane, err)
	}
	return count
}

// resolver resolves the fixture's own lane keys, falling back to the
// genesis roster.
func (s *flywheelCLI) resolver(t *testing.T, fallback ledger.Resolver) ledger.Resolver {
	t.Helper()
	known := map[string]ed25519.PublicKey{}
	for lane := range s.keys {
		pub := s.laneKey(t, lane).Public().(ed25519.PublicKey)
		fp, err := event.Fingerprint(pub)
		if err != nil {
			t.Fatal(err)
		}
		known[fp] = pub
	}
	return func(fp string) (ed25519.PublicKey, bool) {
		if pub, ok := known[fp]; ok {
			return pub, true
		}
		return fallback(fp)
	}
}

// laneKey parses a provisioned lane's private key.
func (s *flywheelCLI) laneKey(t *testing.T, lane string) ed25519.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile(s.keys[lane])
	if err != nil {
		t.Fatal(err)
	}
	key, err := event.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// flywheelStandCLI is a local ledger with the lanes enrolled and an
// instantiated source repository; chores are staged on the raw seam
// as background facts, and every asserted property is produced by an
// admitted act or a read.
func flywheelStandCLI(t *testing.T) *flywheelCLI {
	t.Helper()
	ld, src, base, spec, _, priv, rootKey, keys, fps := offerLedger(t)
	for _, lane := range []struct {
		name string
		seed byte
		cap  string
	}{{"observer", 31, "observer"}, {"curator", 32, "curate"}, {"dispatcher", 33, "dispatch"}} {
		path, pub, fp := writeWorkerKey(t, lane.seed)
		keys[lane.name], fps[lane.name] = path, fp
		rootAppend(t, ld, priv, "actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, lane.name))
		rootAppend(t, ld, priv, "actor.granted", fp, `{"capability": "`+lane.cap+`"}`)
	}
	head := instantiateSource(t, src)
	return &flywheelCLI{ld: ld, priv: priv, src: src, base: base, spec: spec, head: head, keys: keys, fps: fps, rootKey: rootKey}
}

// chore stages one done contract on the raw seam: filed, specified
// with the gated executable acceptance at the spec commit, claimed,
// submitted, passed, requested, observed.
func (s *flywheelCLI) chore(t *testing.T, subject, intent, path string, gated bool) int {
	t.Helper()
	gate := fmt.Sprintf(`, "gate": "pr/1 @ %s"`, s.spec)
	if !gated {
		gate = ""
	}
	s.raw(t, "intent.filed", subject, fmt.Sprintf(`{"intent": %q, "tier": "trivial", "budget": "small", "routing": "core"}`, intent))
	s.raw(t, "contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true%s}}`, path, s.spec, gate))
	fence := s.rawAs(t, "workerA", "claim.taken", subject, `{}`)
	packet := fmt.Sprintf(`{"acceptance": ["%s @ %s"], "decisions": [], "base": "%s..%s", "refs": [], "findings": []}`, path, s.spec, s.base, s.head)
	sub := s.rawAs(t, "workerA", "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
	verdict := s.rawAs(t, "verifier", "verdict.rendered", subject, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("0", 64), sub))
	s.rawAs(t, "workerA", "merge.requested", subject, fmt.Sprintf(`{"verdict": "%d"}`, verdict))
	return s.rawAs(t, "observer", "merge.observed", subject, fmt.Sprintf(`{"merged": %q, "pr": "pr/%s"}`, s.head, subject))
}

func (s *flywheelCLI) shapes(t *testing.T) []map[string]any {
	t.Helper()
	e, code := runEnv(t, "flywheel", "shapes", "--ledger", s.ld)
	if code != 0 || !e.OK {
		t.Fatalf("flywheel shapes: %d %s", code, envText(e))
	}
	var out []map[string]any
	for _, row := range e.Result["shapes"].([]any) {
		out = append(out, row.(map[string]any))
	}
	return out
}

// choreShape returns the id of the shape accepted by the path.
func (s *flywheelCLI) choreShape(t *testing.T, path string) string {
	t.Helper()
	for _, row := range s.shapes(t) {
		if row["acceptance_path"] == path {
			return row["id"].(string)
		}
	}
	t.Fatalf("no shape accepted by %s", path)
	return ""
}

func (s *flywheelCLI) status(t *testing.T) map[string]any {
	t.Helper()
	e, code := runEnv(t, "flywheel", "status", "--ledger", s.ld)
	if code != 0 || !e.OK {
		t.Fatalf("flywheel status: %d %s", code, envText(e))
	}
	return e.Result["flywheel"].(map[string]any)
}

func requireEngineCLI(t *testing.T, src string) {
	t.Helper()
	if reason, ok := flywheel.EngineAvailable(src); !ok {
		t.Skipf("the v1 engine is not available: %s", reason)
	}
}

// conformance: AC1, AC2, AC5 — shapes lists the record's shapes with
// their recurrence; draft carries the deterministic workflow (or
// writes it with --out) and its commands; status renders the report's
// rows; refusals are the drafter's, by exit code.
func TestFlywheelShapesDraftAndStatusAtTheCLI(t *testing.T) {
	s := flywheelStandCLI(t)
	if st := s.status(t); st["recurring"].(float64) != 0 || st["conversion_rate"] != nil {
		t.Fatalf("no work subject, no rate: %+v", st)
	}
	s.chore(t, "c-1", "fix the check", "accept.md", true)
	if rows := s.shapes(t); len(rows) != 1 || rows[0]["recurring"] != false || rows[0]["count"].(float64) != 1 {
		t.Fatalf("one done contract, one shape, not recurring: %+v", rows)
	}
	d2 := s.chore(t, "c-2", "fix the check again", "accept.md", true)
	rows := s.shapes(t)
	if len(rows) != 1 || rows[0]["recurring"] != true || rows[0]["count"].(float64) != 2 || rows[0]["routing"] != "core" || rows[0]["tier"] != "trivial" {
		t.Fatalf("two done contracts recur: %+v", rows)
	}
	occ := rows[0]["occurrences"].([]any)
	if len(occ) != 2 || occ[1].(map[string]any)["done"].(float64) != float64(d2) || occ[1].(map[string]any)["gated"] != true {
		t.Fatalf("occurrences carry their observation positions and gating: %+v", occ)
	}
	e, code := runEnv(t, "flywheel", "shapes", "--ledger", s.ld)
	if code != 0 || e.Result["recurring"].(float64) != 1 || e.Result["recurring_after"].(float64) != float64(flywheel.RecurringAfter) {
		t.Fatalf("the envelope counts recurring shapes and states the figure: %+v", e.Result)
	}
	shape := rows[0]["id"].(string)
	if st := s.status(t); st["recurring"].(float64) != 1 || st["proposed"].(float64) != 0 || st["conversion_rate"] != "0.000" {
		t.Fatalf("status renders the report's rows: %+v", st)
	}

	e, code = runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src)
	if code != 0 || !e.OK {
		t.Fatalf("draft: %d %s", code, envText(e))
	}
	text, _ := e.Result["workflow"].(string)
	if e.Result["name"] != rows[0]["name"] || e.Result["path"] != flywheel.RegistryDir+"/"+rows[0]["name"].(string)+".yaml" || !strings.Contains(text, "name: "+rows[0]["name"].(string)+"\n") {
		t.Fatalf("the draft names the shape's workflow at its registry path: %+v", e.Result)
	}
	cmds := e.Result["commands"].([]any)
	if len(cmds) != 2 || cmds[0] != "printf ok" || cmds[1] != "test -f hello.txt" {
		t.Fatalf("the commands are the acceptance's, in order: %v", cmds)
	}
	if inputs := e.Result["inputs"].([]any); len(inputs) != 1 || inputs[0] != "goal" {
		t.Fatalf("the intents vary, so goal is the one input: %v", inputs)
	}
	out := filepath.Join(t.TempDir(), "draft.yaml")
	e, code = runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src, "--out", out)
	if code != 0 || e.Result["out"] != out || e.Result["workflow"] != nil {
		t.Fatalf("--out writes the file and drops it from the envelope: %d %+v", code, e.Result)
	}
	if b, err := os.ReadFile(out); err != nil || string(b) != text {
		t.Fatalf("the written draft is the carried draft: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.src, flywheel.RegistryDir, rows[0]["name"].(string)+".yaml")); !os.IsNotExist(err) {
		t.Fatal("drafting writes nothing into the repository")
	}
	if e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", "s-nope", "--repo", s.src); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("an unknown shape is not found: %d %s", code, envText(e))
	}
	for _, args := range [][]string{
		{"flywheel"},
		{"flywheel", "nope"},
		{"flywheel", "shapes"},
		{"flywheel", "draft", "--ledger", s.ld},
		{"flywheel", "propose", "--ledger", s.ld, "--shape", shape},
		{"flywheel", "repair", "--ledger", s.ld, "--key", s.keys["dispatcher"], "--repo", s.src},
		{"flywheel", "observe", "--ledger", s.ld, "--key", s.keys["observer"], "--shape", shape},
		{"flywheel", "status"},
	} {
		if e, code := runEnv(t, args...); code != envelope.ExitUsage || e.Error == nil || e.Error.Code != "usage" {
			t.Fatalf("%v: usage refusal, got %d %s", args, code, envText(e))
		}
	}

	// Ungated and divergent shapes refuse as the drafter does.
	s.chore(t, "c-3", "red", "red.md", false)
	s.chore(t, "c-4", "red", "red.md", false)
	if e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", s.choreShape(t, "red.md"), "--repo", s.src); code != 19 || e.Error == nil || e.Error.Code != "spec_unrunnable" {
		t.Fatalf("an ungated shape cannot be drafted from: %d %s", code, envText(e))
	}
	if err := os.WriteFile(filepath.Join(s.src, "accept.md"), []byte("# Changed\n\n## Validation Commands\n\n- Boundary: `printf changed`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, s.src, "add", ".")
	gitAt(t, s.src, "commit", "--quiet", "-m", "the gate changed")
	changed := gitAt(t, s.src, "rev-parse", "HEAD")
	s.raw(t, "intent.filed", "c-5", `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`)
	s.raw(t, "contract.specified", "c-5", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/1 @ %s"}}`, changed, changed))
	fence := s.rawAs(t, "workerA", "claim.taken", "c-5", `{}`)
	sub := s.rawAs(t, "workerA", "submission.made", "c-5", fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["x"], "decisions": [], "base": "%s..%s", "refs": [], "findings": []}}`, fence, s.base, changed))
	v := s.rawAs(t, "verifier", "verdict.rendered", "c-5", fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("0", 64), sub))
	s.rawAs(t, "workerA", "merge.requested", "c-5", fmt.Sprintf(`{"verdict": "%d"}`, v))
	s.rawAs(t, "observer", "merge.observed", "c-5", fmt.Sprintf(`{"merged": %q, "pr": "pr/5"}`, changed))
	if e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src); code != 9 || e.Error == nil || e.Error.Code != "classification_refused" || !strings.Contains(e.Error.Message, "divergent") {
		t.Fatalf("occurrences whose gates differ refuse divergent: %d %s", code, envText(e))
	}
}

// conformance: AC3, AC4, AC5 — with the engine, draft --validate runs
// the mock and leaves nothing behind; propose validates, writes the
// draft on seed/flywheel-<shape> (never main), appends the fact and
// names the PR; a second proposal and a claim key refuse; observe
// appends the merge from the observer's key and status converts.
func TestFlywheelProposeWritesTheBranchAndObserveConverts(t *testing.T) {
	s := flywheelStandCLI(t)
	requireEngineCLI(t, s.src)
	s.chore(t, "c-1", "fix the check", "accept.md", true)
	s.chore(t, "c-2", "fix the check again", "accept.md", true)
	shape := s.choreShape(t, "accept.md")
	name := "chore-" + shape[2:10]
	e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src, "--validate")
	if code != 0 || !e.OK {
		t.Fatalf("draft --validate: %d %s", code, envText(e))
	}
	validated := e.Result["validated"].(map[string]any)
	if run, _ := validated["run"].(string); !strings.HasPrefix(run, "wf-") || len(validated["steps"].([]any)) != 4 {
		t.Fatalf("the mock run and its steps: %+v", validated)
	}
	if _, err := os.Stat(filepath.Join(s.src, flywheel.RegistryDir, name+".yaml")); !os.IsNotExist(err) {
		t.Fatal("validation stages in a worktree, never the caller's checkout")
	}
	if wt := gitAt(t, s.src, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("the staging worktree is removed: %s", wt)
	}
	if gitAt(t, s.src, "status", "--porcelain") != "" {
		t.Fatal("the checkout stays clean")
	}
	main := gitAt(t, s.src, "rev-parse", "main")

	// The worker cannot propose; the curator can, once.
	if e, code := runEnv(t, "flywheel", "propose", "--ledger", s.ld, "--key", s.keys["workerA"], "--shape", shape, "--repo", s.src); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim key is out of grant: %d %s", code, envText(e))
	}
	if branches := gitAt(t, s.src, "branch", "--list", "seed/flywheel-"+shape); branches != "" {
		t.Fatalf("a refused proposal writes no branch: %s", branches)
	}
	e, code = runEnv(t, "flywheel", "propose", "--ledger", s.ld, "--key", s.keys["curator"], "--shape", shape, "--repo", s.src)
	if code != 0 || !e.OK || e.Position == nil {
		t.Fatalf("the curator proposes: %d %s", code, envText(e))
	}
	branch := "seed/flywheel-" + shape
	commit := e.Result["branch_head"].(string)
	if e.Result["branch"] != branch || e.Result["workflow"] != flywheel.RegistryDir+"/"+name+".yaml @ "+commit || e.Result["repair"] != "" {
		t.Fatalf("the fact cites the file on the branch: %+v", e.Result)
	}
	pr := e.Result["pr"].(map[string]any)
	if pr["head"] != branch || pr["base"] != "main" || !strings.Contains(pr["title"].(string), name) {
		t.Fatalf("the PR to open is named: %+v", pr)
	}
	if gitAt(t, s.src, "rev-parse", branch) != commit || gitAt(t, s.src, "rev-parse", "main") != main {
		t.Fatal("the branch holds the commit and main did not move")
	}
	if body := gitAt(t, s.src, "show", commit+":"+flywheel.RegistryDir+"/"+name+".yaml"); !strings.Contains(body, "name: "+name+"\n") {
		t.Fatalf("the branch carries the draft: %s", body)
	}
	if _, err := os.Stat(filepath.Join(s.src, flywheel.RegistryDir, name+".yaml")); !os.IsNotExist(err) {
		t.Fatal("the caller's checkout on main gains no registry file")
	}
	if wt := gitAt(t, s.src, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("the branch worktree is removed: %s", wt)
	}
	if st := s.status(t); st["proposed"].(float64) != 1 || st["merged"].(float64) != 0 || st["conversion_rate"] != "0.000" {
		t.Fatalf("one proposed, none merged: %+v", st)
	}
	if e, code := runEnv(t, "flywheel", "propose", "--ledger", s.ld, "--key", s.keys["curator"], "--shape", shape, "--repo", s.src); code != 3 || e.Error == nil || e.Error.Code != "invalid_transition" || !strings.Contains(e.Error.Message, flywheel.GateDuplicate) {
		t.Fatalf("a standing proposal refuses another at its gate: %d %s", code, envText(e))
	}
	// Repair with the engine green: nothing to repair.
	if e, code := runEnv(t, "flywheel", "repair", "--ledger", s.ld, "--key", s.keys["dispatcher"], "--shape", shape, "--repo", s.src); code != 3 || e.Error == nil || !strings.Contains(e.Error.Message, "nothing to repair") {
		t.Fatalf("a draft the engine accepts needs no repair: %d %s", code, envText(e))
	}

	// The merge: the worker cannot observe it, the observer can, once.
	merged := strings.Repeat("ab", 20)
	if e, code := runEnv(t, "flywheel", "observe", "--ledger", s.ld, "--key", s.keys["workerA"], "--shape", shape, "--merged", merged, "--pr", "pr/7"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("the observation is the observer's: %d %s", code, envText(e))
	}
	e, code = runEnv(t, "flywheel", "observe", "--ledger", s.ld, "--key", s.keys["observer"], "--shape", shape, "--merged", merged, "--pr", "pr/7")
	if code != 0 || !e.OK || e.Result["workflow"] != flywheel.RegistryDir+"/"+name+".yaml @ "+merged || e.Result["pr"] != "pr/7 @ "+merged {
		t.Fatalf("the observer cites the proposed file at the merged commit: %d %s", code, envText(e))
	}
	if st := s.status(t); st["merged"].(float64) != 1 || st["conversion_rate"] != "1.000" {
		t.Fatalf("converted: %+v", st)
	}
	if e, code := runEnv(t, "flywheel", "observe", "--ledger", s.ld, "--key", s.keys["observer"], "--shape", shape, "--merged", merged, "--pr", "pr/7"); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("nothing stands to observe twice: %d %s", code, envText(e))
	}
	// A taken name refuses before staging.
	if e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src, "--validate"); code != 0 {
		t.Fatalf("main still lacks the file, so the name is free: %d %s", code, envText(e))
	}
	gitAt(t, s.src, "merge", "--quiet", "--no-edit", branch)
	if e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src, "--validate"); code != 3 || e.Error == nil || e.Error.Code != "name_taken" {
		t.Fatalf("the registry holding the name refuses name_taken: %d %s", code, envText(e))
	}
}

// conformance: AC4 — the branch write refuses main and lands on the
// shape's branch through a worktree that is removed afterwards.
func TestFlywheelNeverWritesOnMain(t *testing.T) {
	src, _, _, head := verdictRepo(t)
	for _, branch := range []string{"main", "master", "seed/other", ""} {
		if _, err := writeOnBranch(src, branch, map[string][]byte{"x": []byte("x")}, "no"); err == nil {
			t.Fatalf("writing on %q must refuse", branch)
		}
	}
	if gitAt(t, src, "rev-parse", "HEAD") != head {
		t.Fatal("a refused write moves nothing")
	}
	commit, err := writeOnBranch(src, "seed/flywheel-s-1", map[string][]byte{flywheel.RegistryDir + "/x.yaml": []byte("name: x\n")}, "flywheel: x")
	if err != nil {
		t.Fatal(err)
	}
	if gitAt(t, src, "rev-parse", "seed/flywheel-s-1") != commit || gitAt(t, src, "rev-parse", "main") != head {
		t.Fatal("the branch holds the commit and main did not move")
	}
	if wt := gitAt(t, src, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Fatalf("the worktree is removed: %s", wt)
	}
	if _, err := os.Stat(filepath.Join(src, flywheel.RegistryDir)); !os.IsNotExist(err) {
		t.Fatal("the checkout gains no registry directory")
	}
	again, err := writeOnBranch(src, "seed/flywheel-s-1", map[string][]byte{"y": []byte("y\n")}, "flywheel: y")
	if err != nil || gitAt(t, src, "rev-parse", "seed/flywheel-s-1^") != commit {
		t.Fatalf("an existing branch is extended: %v %s", err, again)
	}
}

// conformance: AC6, AC7 — with a break planted in one step, the mock
// run fails naming the step and nothing is appended; the repair is the
// dispatcher's filing (a claim key is out of grant with nothing
// appended), trivial and small on the shape's routing, its acceptance
// at the branch commit quoting the finding and carrying the two
// commands; propose refuses repair_open until the verifier's render
// runs the two commands green on the fixed branch, then admits citing
// the contract and validating the branch's file as it stands; the
// report counts the repair filed and, at done, done.
func TestFlywheelRepairFromThePlantedBreakToTheCitedProposal(t *testing.T) {
	s := flywheelStandCLI(t)
	requireEngineCLI(t, s.src)
	s.head = plantHarnessBreak(t, s.src)
	s.chore(t, "c-1", "fix the check", "accept.md", true)
	s.chore(t, "c-2", "fix the check again", "accept.md", true)
	shape := s.choreShape(t, "accept.md")
	name := "chore-" + shape[2:10]
	branch := "seed/flywheel-" + shape
	count := func() int {
		e, code := runEnv(t, "ledger", "verify", "--ledger", s.ld)
		if code != 0 {
			t.Fatalf("verify: %d %s", code, envText(e))
		}
		return int(e.Result["count"].(float64))
	}
	before := count()
	e, code := runEnv(t, "flywheel", "draft", "--ledger", s.ld, "--shape", shape, "--repo", s.src, "--validate")
	if code != 20 || e.Error == nil || e.Error.Code != "checks_red" || !strings.Contains(e.Error.Message, "at mock: step verdict:") || !strings.Contains(e.Error.Message, "seed flywheel repair --shape "+shape) {
		t.Fatalf("the planted break refuses at the step, naming the owed act: %d %s", code, envText(e))
	}
	if e, code := runEnv(t, "flywheel", "propose", "--ledger", s.ld, "--key", s.keys["curator"], "--shape", shape, "--repo", s.src); code != 20 || e.Error == nil || e.Error.Code != "checks_red" {
		t.Fatalf("propose refuses the same way and appends nothing: %d %s", code, envText(e))
	}
	if count() != before {
		t.Fatal("a refused validation appends nothing")
	}
	if branches := gitAt(t, s.src, "branch", "--list", branch); branches != "" {
		t.Fatalf("a refused proposal writes no branch: %s", branches)
	}
	if e, code := runEnv(t, "flywheel", "repair", "--ledger", s.ld, "--key", s.keys["workerA"], "--shape", shape, "--repo", s.src); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("the repair is the dispatcher's filing: %d %s", code, envText(e))
	}
	if count() != before || gitAt(t, s.src, "branch", "--list", branch) != "" {
		t.Fatal("an out-of-grant repair writes and appends nothing")
	}
	e, code = runEnv(t, "flywheel", "repair", "--ledger", s.ld, "--key", s.keys["dispatcher"], "--shape", shape, "--repo", s.src)
	if code != 0 || !e.OK {
		t.Fatalf("the dispatcher files the repair: %d %s", code, envText(e))
	}
	subject := e.Result["subject"].(string)
	commit := e.Result["branch_head"].(string)
	acceptance := flywheel.RepairAcceptancePath(shape)
	if subject != "repair-"+shape[2:10] || e.Result["branch"] != branch || e.Result["acceptance"] != acceptance+" @ "+commit || len(e.Result["appended"].([]any)) != 2 {
		t.Fatalf("the repair's subject, branch and acceptance: %+v", e.Result)
	}
	finding := e.Result["finding"].(map[string]any)
	if finding["stage"] != "mock" || finding["step"] != "verdict" || finding["finding"] == "" {
		t.Fatalf("the finding names stage and step: %+v", finding)
	}
	if count() != before+2 {
		t.Fatal("the filing appends the intent and the specification")
	}
	body := gitAt(t, s.src, "show", commit+":"+acceptance)
	for _, want := range []string{"- Failing step: `verdict`", "- Finding: " + finding["finding"].(string), "scripts/seed workflow validate " + flywheel.RegistryDir + "/" + name + ".yaml", "scripts/seed workflow run " + name + " --mock --input goal=placeholder"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the acceptance quotes the finding and carries the two commands: lacks %q\n%s", want, body)
		}
	}
	if gitAt(t, s.src, "show", commit+":"+flywheel.RegistryDir+"/"+name+".yaml") == "" || gitAt(t, s.src, "rev-parse", branch) != commit {
		t.Fatal("the branch carries the draft and the acceptance at the commit")
	}
	e, code = runEnv(t, "situation", "--ledger", s.ld, "--key", s.keys["dispatcher"])
	if code != 0 {
		t.Fatalf("situation: %d %s", code, envText(e))
	}
	if st := s.status(t); st["repairs"].(map[string]any)["filed"].(float64) != 1 || st["repairs"].(map[string]any)["done"].(float64) != 0 || st["proposed"].(float64) != 0 {
		t.Fatalf("the repair counts filed: %+v", st)
	}
	if e, code := runEnv(t, "flywheel", "repair", "--ledger", s.ld, "--key", s.keys["dispatcher"], "--shape", shape, "--repo", s.src); code != 3 || e.Error == nil || !strings.Contains(e.Error.Message, "already stands") {
		t.Fatalf("one repair per shape: %d %s", code, envText(e))
	}
	if e, code := runEnv(t, "flywheel", "propose", "--ledger", s.ld, "--key", s.keys["curator"], "--shape", shape, "--repo", s.src); code != 3 || e.Error == nil || !strings.Contains(e.Error.Message, flywheel.GateRepairOpen) {
		t.Fatalf("a repair short of its verdict refuses the proposal: %d %s", code, envText(e))
	}

	// The implementer's fix on the branch: the harness restored. The
	// contract is claimed and submitted on the raw seam (background),
	// and the verifier's render runs the acceptance's two commands.
	fixDir := filepath.Join(t.TempDir(), "fix")
	gitAt(t, s.src, "worktree", "add", "--quiet", fixDir, branch)
	mock := filepath.Join(fixDir, "scripts", "harness", "mock")
	if err := os.Rename(mock+".orig", mock); err != nil {
		t.Fatal(err)
	}
	gitAt(t, fixDir, "add", "-A", ".")
	gitAt(t, fixDir, "commit", "--quiet", "-m", "repair: restore the reviewer's harness")
	fixed := gitAt(t, fixDir, "rev-parse", "HEAD")
	gitAt(t, s.src, "worktree", "remove", "--force", fixDir)
	fence := s.rawAs(t, "workerA", "claim.taken", subject, `{}`)
	packet := fmt.Sprintf(`{"acceptance": [%q], "decisions": [], "base": "%s..%s", "refs": [], "findings": []}`, acceptance+" @ "+commit, s.head, fixed)
	s.rawAs(t, "workerA", "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, packet))
	e, code = runEnv(t, "verdict", "render", "--ledger", s.ld, "--subject", subject, "--repo", s.src, "--key", s.keys["verifier"], "--verdict", "pass")
	if code != 0 || !e.OK {
		t.Fatalf("the verifier's render runs the two commands green on the fixed branch: %d %s", code, envText(e))
	}
	if e.Result["transcripts"] != "2" {
		t.Fatalf("exactly the two engine commands ran: %+v", e.Result)
	}
	verdictPos := *e.Position
	e, code = runEnv(t, "flywheel", "propose", "--ledger", s.ld, "--key", s.keys["curator"], "--shape", shape, "--repo", s.src)
	if code != 0 || !e.OK {
		t.Fatalf("with the repair passed the proposal admits: %d %s", code, envText(e))
	}
	if e.Result["repair"] != subject+"@"+verdictPos || e.Result["branch_head"] != fixed || e.Result["workflow"] != flywheel.RegistryDir+"/"+name+".yaml @ "+fixed {
		t.Fatalf("the proposal cites the repair and the branch's file as it stands: %+v", e.Result)
	}
	if gitAt(t, s.src, "rev-parse", branch) != fixed {
		t.Fatal("propose regenerates nothing over the fix")
	}
	if st := s.status(t); st["proposed"].(float64) != 1 || st["repairs"].(map[string]any)["done"].(float64) != 0 {
		t.Fatalf("proposed, the repair not yet done: %+v", st)
	}
	// One merge closes both: the repair contract's observation and
	// the workflow's.
	s.rawAs(t, "workerA", "merge.requested", subject, fmt.Sprintf(`{"verdict": "%s"}`, verdictPos))
	s.rawAs(t, "observer", "merge.observed", subject, fmt.Sprintf(`{"merged": %q, "pr": "pr/8"}`, fixed))
	if e, code := runEnv(t, "flywheel", "observe", "--ledger", s.ld, "--key", s.keys["observer"], "--shape", shape, "--merged", fixed, "--pr", "pr/8"); code != 0 || !e.OK {
		t.Fatalf("the merge is observed: %d %s", code, envText(e))
	}
	st := s.status(t)
	if st["merged"].(float64) != 1 || st["conversion_rate"] != "1.000" || st["repairs"].(map[string]any)["filed"].(float64) != 1 || st["repairs"].(map[string]any)["done"].(float64) != 1 {
		t.Fatalf("converted, the repair done: %+v", st)
	}
}
