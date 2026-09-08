// Package flywheel is flywheel v0 (SEED-NEXT.md §12; charter III.K's
// flywheel row; plans/os-9075c308.md): recurring contract shapes are
// detected from the ledger, drafted as deterministic v1 workflows from
// the gated acceptance's own commands, validated in mock through the
// v1 engine, proposed as PRs by the curator's grant, and observed
// merged; the conversion rate is a report section. Every chore an
// agent does twice becomes infrastructure, through gates: the tool
// never writes under the registry on main, and the registry is
// reached only through the PR the governance root reviews.
//
// The package carries no engine and no model. A shape is
// record-derivable and recurrence is counted, not judged (D1); the
// draft is a pure function of the shape and the command lists at the
// occurrences' gated anchors (D2); validation is the v1 engine invoked
// as a subprocess from a staging worktree (D3); the proposal and the
// merge are facts the boundary holds to the record (D4); a failing
// step's repair is a bounded contract filed by the dispatcher (D7).
package flywheel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/plan"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// The two verbs, catalog growth under a new namespace (D4, D8).
const (
	ProposedVerb = "workflow.proposed"
	MergedVerb   = "workflow.merged"
)

// RecurringAfter is the charter's figure: "every chore an agent does
// twice becomes infrastructure", so the second done occurrence is the
// one that makes a shape a chore (D1). spec/flywheel.md states
// the same figure and a drill pins the two together.
const RecurringAfter = 2

// RegistryDir is the v1 workflow registry, relative to the repository:
// the one place a proposal's path may point, and the one place this
// package never writes on main.
const RegistryDir = ".seed/workflows"

// Occurrence is one done contract folding to a shape: the subject,
// the position of the merge observation that made it done, and the
// acceptance anchor the draft reads at.
type Occurrence struct {
	Subject string `json:"subject"`
	Done    int    `json:"done"`
	Ref     string `json:"ref"`
	Anchor  string `json:"anchor"`
	// Intent is the filed intent text, the one field a draft may take
	// as an input; never copied into a prompt.
	Intent string `json:"-"`
	// Gated reports executable content with review-gate evidence: the
	// only content the drafter reads.
	Gated bool `json:"gated"`
}

// Shape is one recurring-or-not contract shape: the JCS form of
// {routing, acceptance_path, tier, sequence} identifies it, and its
// done occurrences count its recurrence.
type Shape struct {
	ID             string       `json:"id"`
	Routing        string       `json:"routing"`
	AcceptancePath string       `json:"acceptance_path"`
	Tier           string       `json:"tier"`
	Sequence       []string     `json:"sequence"`
	Occurrences    []Occurrence `json:"occurrences"`
}

// Recurring reports whether the shape is a chore: RecurringAfter or
// more done occurrences.
func (s Shape) Recurring() bool { return len(s.Occurrences) >= RecurringAfter }

// Name is the workflow name a draft of the shape takes, chore-<shape
// prefix>, kebab-case as the registry demands.
func (s Shape) Name() string { return "chore-" + strings.TrimPrefix(s.ID, "s-")[:8] }

// Cite is an occurrence's citation, "<contract>@<done position>".
func (o Occurrence) Cite() string { return fmt.Sprintf("%s@%d", o.Subject, o.Done) }

// shapeID is the sha256 of the JCS form, twelve hex characters under
// the s- prefix.
func shapeID(routing, path, tier string, sequence []string) string {
	if sequence == nil {
		sequence = []string{}
	}
	b, _ := json.Marshal(map[string]any{"routing": routing, "acceptance_path": path, "tier": tier, "sequence": sequence})
	canonical, err := jcs.Transform(b)
	if err != nil {
		canonical = b
	}
	sum := sha256.Sum256(canonical)
	return "s-" + hex.EncodeToString(sum[:])[:12]
}

// anchorParts splits "<path> @ <commit>".
func anchorParts(anchor string) (path, commit string, ok bool) {
	path, commit, ok = strings.Cut(anchor, " @ ")
	return strings.TrimSpace(path), strings.TrimSpace(commit), ok && path != "" && commit != ""
}

// isWork reports a verb that belongs to a contract's own history: the
// system and actor namespaces are the chain's, not a subject's.
func isWork(verb string) bool {
	return !strings.HasPrefix(verb, "system.") && !strings.HasPrefix(verb, "actor.")
}

// MergeObservedVerb is the observation that makes a contract done: the
// verb whose admission a counted occurrence must have passed.
const MergeObservedVerb = "merge.observed"

// authenticDone reports that a subject's completion is the boundary's
// and not a raw push's (review finding on the task PR): the lifecycle
// fold is tolerant by design, so a chain of transition-legal events
// pushed past the boundary reaches "done" without any capability check
// ever running, and two such chains would otherwise manufacture a
// chore. A counted occurrence therefore needs the merge observation
// signed by a key the row accepts at that position, over an authentic
// pass verdict: a granted verifier, disjoint from the implementer, at
// the level the record supports (curation.AuthenticPass, the same
// derivation the promotion gate and the reconciler read).
func authenticDone(records []*event.Record, table *transition.Table, subject string, s transition.SubjectState) bool {
	if s.Merged == nil || s.Merged.Pos < 0 || s.Merged.Pos >= len(records) {
		return false
	}
	e := &records[s.Merged.Pos].Event
	if e.Verb != MergeObservedVerb || e.Subject != subject || !keyring.Applies(e.V) {
		return false
	}
	ring, _, err := keyring.StateAt(records[:s.Merged.Pos])
	if err != nil || ring == nil || !ring.HasAnyCapability(e.Actor, keyring.AcceptedCapabilities(MergeObservedVerb)) {
		return false
	}
	return curation.AuthenticPass(records, table, subject, s) != nil
}

// Shapes derives every shape from the record (D1): for every done
// subject whose completion is authentic the routing, tier and
// acceptance path from the fold and the subject's verbs in chain order
// with positions, actors, payloads and instants dropped. Shapes are
// listed in the order their first occurrence was filed; occurrences
// within a shape in the order they were done.
func Shapes(records []*event.Record, fold *transition.Fold) []Shape {
	table, terr := transition.Default()
	sequences := map[string][]string{}
	intents := map[string]string{}
	for _, rec := range records {
		e := &rec.Event
		if !keyring.Applies(e.V) || !isWork(e.Verb) {
			continue
		}
		sequences[e.Subject] = append(sequences[e.Subject], e.Verb)
		if e.Verb == "intent.filed" {
			var filed struct {
				Intent string `json:"intent"`
			}
			if json.Unmarshal(e.Payload, &filed) == nil {
				intents[e.Subject] = filed.Intent
			}
		}
	}
	var out []Shape
	index := map[string]int{}
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok || s.State != "done" || s.Acceptance == nil || s.Merged == nil {
			continue
		}
		if terr != nil || !authenticDone(records, table, subject, s) {
			continue
		}
		path, commit, ok := anchorParts(s.Acceptance.Ref)
		if !ok {
			continue
		}
		seq := append([]string(nil), sequences[subject]...)
		id := shapeID(s.Routing, path, s.Tier, seq)
		occ := Occurrence{Subject: subject, Done: s.Merged.Pos, Ref: s.Acceptance.Ref, Anchor: commit, Intent: intents[subject],
			Gated: s.Acceptance.Executable && s.Acceptance.Gated}
		i, seen := index[id]
		if !seen {
			index[id] = len(out)
			out = append(out, Shape{ID: id, Routing: s.Routing, AcceptancePath: path, Tier: s.Tier, Sequence: seq})
			i = len(out) - 1
		}
		out[i].Occurrences = append(out[i].Occurrences, occ)
	}
	for i := range out {
		sort.SliceStable(out[i].Occurrences, func(a, b int) bool { return out[i].Occurrences[a].Done < out[i].Occurrences[b].Done })
	}
	return out
}

// Find returns the shape with the id, if any.
func Find(shapes []Shape, id string) (Shape, bool) {
	for _, s := range shapes {
		if s.ID == id {
			return s, true
		}
	}
	return Shape{}, false
}

// Draft is a drafted workflow: the registry name, the file bytes, the
// commands the run steps carry, and the inputs the occurrences vary in.
type Draft struct {
	Name     string
	Shape    string
	Bytes    []byte
	Commands []string
	Inputs   []string
}

// Path is the draft's registry path.
func (d *Draft) Path() string { return RegistryDir + "/" + d.Name + ".yaml" }

// UngatedError refuses a draft over an occurrence whose acceptance is
// not executable content behind review-gate evidence: the drafter
// reads gated content only (D2).
type UngatedError struct {
	Shape, Subject, Ref string
}

func (e *UngatedError) Error() string {
	return fmt.Sprintf("shape %s is ungated: occurrence %s cites %q, which is not executable content with review-gate evidence — a draft copies commands from gated acceptance only (spec/flywheel.md)", e.Shape, e.Subject, e.Ref)
}

// DivergentError refuses a draft over occurrences whose gated anchors
// carry different command lists: a chore whose gate changed is not
// one chore (D2).
type DivergentError struct {
	Shape, A, B string
}

func (e *DivergentError) Error() string {
	return fmt.Sprintf("shape %s is divergent: the validation commands at %q and at %q differ, and a workflow invoked with one anchor must never run commands copied from another (spec/flywheel.md)", e.Shape, e.A, e.B)
}

// AnchorError refuses a draft whose occurrence anchor the repository
// does not resolve.
type AnchorError struct {
	Shape, Ref string
	Err        error
}

func (e *AnchorError) Error() string {
	return fmt.Sprintf("shape %s: acceptance %q does not resolve in the repository: %v", e.Shape, e.Ref, e.Err)
}

func gitShow(repo, commit, path string) ([]byte, error) {
	out, err := exec.Command("git", "-C", repo, "show", commit+":"+path).Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DraftWorkflow drafts the shape's workflow (D2): one run step per
// validation command of the acceptance spec at its gated anchor, in
// the spec's order, each depending on the previous; one role step per
// judgment point the sequence carries (plan.proposed to planner, the
// claim span to implementer, verdict.rendered to reviewer); inputs
// exactly the fields that vary across the occurrences (goal, the
// intent text; anchor, the acceptance commit); prompts templates over
// the inputs and the produced artifacts and nothing else. Every
// occurrence's gated anchor must carry one command list, byte for
// byte, or the draft refuses divergent. Two drafts of one shape are
// byte-identical.
func DraftWorkflow(shape Shape, repo string) (*Draft, error) {
	if len(shape.Occurrences) == 0 {
		return nil, fmt.Errorf("shape %s has no done occurrence to draft from", shape.ID)
	}
	var commands []string
	first := ""
	goals, anchors := map[string]bool{}, map[string]bool{}
	for i, occ := range shape.Occurrences {
		if !occ.Gated {
			return nil, &UngatedError{Shape: shape.ID, Subject: occ.Subject, Ref: occ.Ref}
		}
		body, err := gitShow(repo, occ.Anchor, shape.AcceptancePath)
		if err != nil {
			return nil, &AnchorError{Shape: shape.ID, Ref: occ.Ref, Err: err}
		}
		cmds := plan.Commands(body)
		if i == 0 {
			commands, first = cmds, occ.Ref
		} else if strings.Join(cmds, "\x00") != strings.Join(commands, "\x00") {
			return nil, &DivergentError{Shape: shape.ID, A: first, B: occ.Ref}
		}
		goals[occ.Intent] = true
		anchors[occ.Anchor] = true
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("shape %s: the acceptance at %s carries no validation command, so there is no deterministic step to draft", shape.ID, shape.AcceptancePath)
	}
	var inputs []string
	if len(goals) > 1 {
		inputs = append(inputs, "goal")
	}
	if len(anchors) > 1 {
		inputs = append(inputs, "anchor")
	}
	return &Draft{Name: shape.Name(), Shape: shape.ID, Bytes: render(shape, commands, inputs), Commands: commands, Inputs: inputs}, nil
}

// has reports whether the sequence carries the verb.
func (s Shape) has(verb string) bool {
	for _, v := range s.Sequence {
		if v == verb {
			return true
		}
	}
	return false
}

// yamlSingle quotes a scalar in single quotes, the one YAML form in
// which nothing but the quote itself is special, so a command is
// carried verbatim.
func yamlSingle(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// render writes the workflow text. The layout is fixed and the only
// variable parts are the shape's fields, the commands and the inputs,
// which is what makes two drafts of one shape byte-identical.
func render(shape Shape, commands, inputs []string) []byte {
	var b strings.Builder
	anchorRef := "the acceptance spec at its recorded anchor"
	if contains(inputs, "anchor") {
		anchorRef = "the acceptance spec at {{inputs.anchor}}"
	}
	goalRef := "the recurring chore"
	if contains(inputs, "goal") {
		goalRef = "{{inputs.goal}}"
	}
	fmt.Fprintf(&b, "# Drafted by seed flywheel from shape %s: %d done contract(s) routed %s, accepted by %s.\n", shape.ID, len(shape.Occurrences), shape.Routing, shape.AcceptancePath)
	b.WriteString("# The run steps are the acceptance spec's own validation commands, verbatim and in order; nothing here was invented.\n")
	b.WriteString("schema_version: \"1\"\n")
	fmt.Fprintf(&b, "name: %s\n", shape.Name())
	b.WriteString("description: |\n")
	fmt.Fprintf(&b, "  Use when: a contract routed %s at tier %s is accepted by %s and has recurred.\n", shape.Routing, shape.Tier, shape.AcceptancePath)
	b.WriteString("  NOT for: work whose acceptance is another spec, or a first occurrence.\n")
	if len(inputs) > 0 {
		b.WriteString("inputs:\n")
		for _, in := range inputs {
			switch in {
			case "goal":
				b.WriteString("  - {name: goal, type: string, required: true, description: \"The intent text of this occurrence\"}\n")
			case "anchor":
				b.WriteString("  - {name: anchor, type: string, required: true, description: \"The acceptance spec's commit for this occurrence\"}\n")
			}
		}
	}
	b.WriteString("defaults: {harness: claude, model: sonnet}\n")
	b.WriteString("budgets: {max_wall_clock_minutes: 60, max_step_retries: 1}\n")
	b.WriteString("steps:\n")
	last := ""
	if shape.has(transition.PlanProposedVerb) {
		b.WriteString("  - id: plan\n    role: planner\n    tools: readonly\n")
		fmt.Fprintf(&b, "    prompt: \"Draft a plan for %s against %s.\"\n", goalRef, anchorRef)
		b.WriteString("    produces: [{name: plan, file: artifacts/plan.md}]\n")
		last = "plan"
	}
	if shape.has("claim.taken") {
		b.WriteString("  - id: implement\n    role: implementer\n    tools: coding\n")
		if last != "" {
			fmt.Fprintf(&b, "    depends_on: [%s]\n    consumes: [%s]\n", last, last)
			fmt.Fprintf(&b, "    prompt: \"Implement the plan at {{output.%s.path}} for %s so that %s passes; summarize the change.\"\n", last, goalRef, anchorRef)
		} else {
			fmt.Fprintf(&b, "    prompt: \"Implement %s so that %s passes; summarize the change.\"\n", goalRef, anchorRef)
		}
		b.WriteString("    produces: [{name: change, file: artifacts/change.md}]\n")
		last = "implement"
	}
	for i, cmd := range commands {
		id := fmt.Sprintf("check-%d", i+1)
		fmt.Fprintf(&b, "  - id: %s\n    run: %s\n", id, yamlSingle(cmd))
		if last != "" {
			fmt.Fprintf(&b, "    depends_on: [%s]\n", last)
		}
		last = id
	}
	if shape.has(transition.VerdictRenderedVerb) {
		b.WriteString("  - id: verdict\n    role: reviewer\n    tools: readonly\n")
		fmt.Fprintf(&b, "    depends_on: [%s]\n", last)
		if shape.has("claim.taken") {
			b.WriteString("    consumes: [change]\n")
			fmt.Fprintf(&b, "    prompt: \"Review {{output.change.path}} against %s and emit a JSON verdict.\"\n", anchorRef)
		} else {
			fmt.Fprintf(&b, "    prompt: \"Review the checks against %s and emit a JSON verdict.\"\n", anchorRef)
		}
		b.WriteString("    output_format: {type: object, required: [verdict], properties: {verdict: {type: string}}}\n")
		b.WriteString("    produces: [{name: verdict, file: artifacts/verdict.json}]\n")
	}
	return []byte(b.String())
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// EngineAvailable reports whether the v1 engine can be invoked from
// the repository: SEED_ENGINE names an executable, or the bootstrap
// shim's cache holds the pinned release for this platform. When it
// cannot, reason says why, for a drill that skips by name (D3).
func EngineAvailable(repo string) (reason string, ok bool) {
	_, reason, ok = EnginePath(repo)
	return reason, ok
}

// EnginePath resolves the engine binary the shim would exec from the
// repository, the way the shim resolves it: SEED_ENGINE first, then
// the bootstrap cache under XDG_CACHE_HOME or the home directory at
// the lock's pinned version. A fixture that must run the engine under
// a scrubbed environment vendors this path into its own lock.
func EnginePath(repo string) (path, reason string, ok bool) {
	if p := os.Getenv("SEED_ENGINE"); p != "" {
		if info, err := os.Stat(p); err == nil && executable(info) {
			return p, "", true
		}
		return "", "SEED_ENGINE=" + p + " is not executable", false
	}
	if _, err := os.Stat(filepath.Join(repo, "scripts", "seed")); err != nil {
		return "", "the repository has no scripts/seed shim", false
	}
	lock, err := os.ReadFile(filepath.Join(repo, ".seed", "engine.lock"))
	if err != nil {
		return "", "no .seed/engine.lock in the repository", false
	}
	version := ""
	for _, line := range strings.Split(string(lock), "\n") {
		if strings.HasPrefix(line, "version ") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "version "))
		}
	}
	if version == "" {
		return "", "engine.lock names no version", false
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "no home directory for the engine cache", false
		}
		cache = filepath.Join(home, ".cache")
	}
	bin := filepath.Join(cache, "open-seed", "engine", version, runtime.GOOS+"_"+runtime.GOARCH, "seed")
	if info, err := os.Stat(bin); err != nil || !executable(info) {
		return "", "the pinned engine " + version + " is not in the bootstrap cache (" + bin + ") and SEED_ENGINE is unset", false
	}
	return bin, "", true
}

// Validation is what the engine answered: the mock run's id and the
// steps it recorded.
type Validation struct {
	Name  string   `json:"name"`
	RunID string   `json:"run"`
	Steps []string `json:"steps"`
}

// EngineError is the engine's refusal, verbatim: which stage refused
// (validate or mock), the failing step, and the finding.
type EngineError struct {
	Name, Stage, Step, Finding string
}

func (e *EngineError) Error() string {
	step := e.Step
	if step == "" {
		step = "(no step named)"
	}
	return fmt.Sprintf("the engine refused %s at %s: step %s: %s", e.Name, e.Stage, step, e.Finding)
}

// NameTakenError refuses a draft whose name the registry already holds
// at the base: a second workflow cannot take an existing name.
type NameTakenError struct {
	Name, Commit string
}

func (e *NameTakenError) Error() string {
	return fmt.Sprintf("the registry already holds %s/%s.yaml at %s: a drafted workflow takes no existing name (spec/flywheel.md)", RegistryDir, e.Name, e.Commit)
}

// NameTaken reports whether the registry at the commit holds the name.
func NameTaken(repo, commit, name string) bool {
	return exec.Command("git", "-C", repo, "cat-file", "-e", commit+":"+RegistryDir+"/"+name+".yaml").Run() == nil
}

// ValidateDraft stages the draft at its registry path in a detached
// worktree of the repository at base and runs the engine there (D3):
// the caller's checkout is never staged into and never gains a
// registry file, and the worktree and the run directory are removed
// afterwards.
func ValidateDraft(repo, base string, d *Draft) (*Validation, error) {
	if NameTaken(repo, base, d.Name) {
		return nil, &NameTakenError{Name: d.Name, Commit: base}
	}
	dir, cleanup, err := stage(repo, base)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := os.MkdirAll(filepath.Join(dir, RegistryDir), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, d.Path()), d.Bytes, 0o644); err != nil {
		return nil, err
	}
	return engine(repo, dir, d.Name)
}

// ValidateAt validates the workflow AS IT STANDS at the commit (D7's
// repaired branch): staged at that commit, no regeneration, the name
// checked against base.
func ValidateAt(repo, base, commit, name string) (*Validation, error) {
	if NameTaken(repo, base, name) {
		return nil, &NameTakenError{Name: name, Commit: base}
	}
	dir, cleanup, err := stage(repo, commit)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(dir, RegistryDir, name+".yaml")); err != nil {
		return nil, fmt.Errorf("%s holds no %s/%s.yaml to validate", commit, RegistryDir, name)
	}
	return engine(repo, dir, name)
}

// stage adds a detached worktree of the repository at the commit.
func stage(repo, commit string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "seed-flywheel-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", dir).Run()
		_ = exec.Command("git", "-C", repo, "worktree", "prune").Run()
		_ = os.RemoveAll(dir)
	}
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "--detach", dir, commit).CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("staging worktree: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return dir, cleanup, nil
}

var stepQuoted = regexp.MustCompile(`step "([^"]+)"`)

// inputName matches one declared input's name inside the inputs
// block, in the flow form the drafter writes ("- {name: goal, ...}")
// and the block form a repair may rewrite it to ("- name: goal").
var inputName = regexp.MustCompile(`^\s*-\s*\{?\s*name:\s*"?([A-Za-z0-9_.-]+)"?`)

// declaredInputs lists the names under the workflow's top-level
// inputs key, by line: the file is the drafter's own layout or a
// repair's edit of it, and the engine validated it before the run.
func declaredInputs(body []byte) []string {
	var names []string
	inBlock := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "inputs:") {
			inBlock = true
			continue
		}
		if inBlock && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			inBlock = false
		}
		if !inBlock {
			continue
		}
		if m := inputName.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

// engine runs the two v1 verbs from inside the staging worktree and
// parses their envelopes. The mock run's directory lives under the
// repository's common git dir and is removed afterwards.
func engine(repo, dir, name string) (*Validation, error) {
	shim := filepath.Join(dir, "scripts", "seed")
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(shim, args...)
		cmd.Dir = dir
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		if out.Len() == 0 && err != nil {
			return nil, fmt.Errorf("engine %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
		}
		return out.Bytes(), nil
	}
	vb, err := run("workflow", "validate", RegistryDir+"/"+name+".yaml")
	if err != nil {
		return nil, err
	}
	var v struct {
		OK        bool `json:"ok"`
		Workflows []struct {
			Findings []struct {
				Rule int    `json:"rule"`
				Msg  string `json:"msg"`
			} `json:"findings"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal(vb, &v); err != nil {
		return nil, fmt.Errorf("engine validate: unparseable envelope: %v: %s", err, strings.TrimSpace(string(vb)))
	}
	if !v.OK {
		finding := "the engine refused the workflow"
		step := ""
		for _, w := range v.Workflows {
			for _, f := range w.Findings {
				finding = fmt.Sprintf("rule %d: %s", f.Rule, f.Msg)
				if m := stepQuoted.FindStringSubmatch(f.Msg); m != nil {
					step = m[1]
				}
				break
			}
		}
		return nil, &EngineError{Name: name, Stage: "validate", Step: step, Finding: finding}
	}
	// The mock run refuses a workflow whose declared inputs are not
	// supplied, so each declared input is bound to a placeholder: the
	// run executes nothing, and the placeholders reach no command.
	args := []string{"workflow", "run", name, "--mock"}
	body, err := os.ReadFile(filepath.Join(dir, RegistryDir, name+".yaml"))
	if err != nil {
		return nil, err
	}
	for _, in := range declaredInputs(body) {
		args = append(args, "--input", in+"=placeholder")
	}
	rb, err := run(args...)
	if err != nil {
		return nil, err
	}
	var r struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		RunID   string `json:"run_id"`
		RunDir  string `json:"run_dir"`
		Message string `json:"message"`
		Steps   map[string]struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(rb, &r); err != nil {
		return nil, fmt.Errorf("engine run: unparseable envelope: %v: %s", err, strings.TrimSpace(string(rb)))
	}
	if r.RunDir != "" {
		_ = os.RemoveAll(r.RunDir)
	}
	steps := make([]string, 0, len(r.Steps))
	for id := range r.Steps {
		steps = append(steps, id)
	}
	sort.Strings(steps)
	if !r.OK || r.Status != "succeeded" {
		step, finding := "", "the mock run did not succeed: "+r.Status
		if r.Message != "" {
			finding = "the mock run refused: " + r.Message
		}
		for _, id := range steps {
			if st := r.Steps[id]; st.Status == "failed" {
				step, finding = id, st.Note
				break
			}
		}
		return nil, &EngineError{Name: name, Stage: "mock", Step: step, Finding: finding}
	}
	return &Validation{Name: name, RunID: r.RunID, Steps: steps}, nil
}

// The repair contract (D7): filed by the dispatcher when the engine
// refuses a draft, at the trivial tier with the small budget, its
// acceptance under next/flywheel/<shape>/ on the branch quoting the
// failing step and the finding verbatim and running exactly the two
// engine commands.

// RepairSubject is the repair contract's id for a shape.
func RepairSubject(shape Shape) string { return "repair-" + strings.TrimPrefix(shape.Name(), "chore-") }

// RepairAcceptancePath is where the repair's acceptance lives.
func RepairAcceptancePath(shapeID string) string {
	return transition.FlywheelRoot + "/" + shapeID + "/accept.md"
}

// RepairCommands are the acceptance's validation commands: the engine's
// two verbs on the workflow as it stands, the mock run binding each
// declared input to a placeholder exactly as the validation does.
func RepairCommands(name string, inputs []string) []string {
	run := "scripts/seed workflow run " + name + " --mock"
	for _, in := range inputs {
		run += " --input " + in + "=placeholder"
	}
	return []string{
		"scripts/seed workflow validate " + RegistryDir + "/" + name + ".yaml",
		run,
	}
}

// RepairAcceptance is the acceptance spec's content: the failing step
// and the finding verbatim, then the two commands.
func RepairAcceptance(d *Draft, e *EngineError) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Repair %s\n\n", d.Name)
	fmt.Fprintf(&b, "The drafted workflow `%s` (shape %s) was refused by the engine at %s.\n\n", d.Path(), d.Shape, e.Stage)
	step := e.Step
	if step == "" {
		step = "(no step named)"
	}
	fmt.Fprintf(&b, "- Failing step: `%s`\n- Finding: %s\n\n", step, e.Finding)
	b.WriteString("Fix the workflow on this branch so both commands below pass. Do not widen it: the run steps are the acceptance spec's own commands and stay so.\n\n")
	b.WriteString("## Validation Commands\n\n")
	for _, c := range RepairCommands(d.Name, d.Inputs) {
		fmt.Fprintf(&b, "- `%s`\n", c)
	}
	return []byte(b.String())
}

// RepairFiling shapes the repair contract's two payloads at the branch
// commit that carries the draft and the acceptance: trivial tier (no
// plan, no seal for a workflow patch the governance root reviews at
// the PR), small budget (the bound), the shape's routing; the
// acceptance executable and gated at the branch commit.
func RepairFiling(shape Shape, d *Draft, e *EngineError, branch, commit string) (intent, spec []byte) {
	intent, _ = json.Marshal(map[string]any{
		"intent":  fmt.Sprintf("repair %s: the engine refused the drafted workflow for shape %s at %s (%s)", d.Name, shape.ID, e.Stage, e.Finding),
		"tier":    transition.TrivialTier,
		"budget":  "small",
		"routing": shape.Routing,
	})
	spec, _ = json.Marshal(map[string]any{
		"acceptance": map[string]any{
			"ref":        RepairAcceptancePath(shape.ID) + " @ " + commit,
			"executable": true,
			"gate":       branch + " @ " + commit,
		},
	})
	return intent, spec
}

// IsRepair reports whether the subject is a repair contract, and for
// which shape, from its acceptance path alone.
func IsRepair(s transition.SubjectState) (shapeID string, ok bool) {
	if s.Acceptance == nil {
		return "", false
	}
	path, _, ok := anchorParts(s.Acceptance.Ref)
	if !ok || !strings.HasPrefix(path, transition.FlywheelRoot+"/") {
		return "", false
	}
	rest := strings.TrimPrefix(path, transition.FlywheelRoot+"/")
	shapeID, _, ok = strings.Cut(rest, "/")
	return shapeID, ok && strings.HasPrefix(shapeID, "s-")
}

// Proposal is workflow.proposed's payload (D4).
type Proposal struct {
	Shape       string   `json:"shape"`
	Workflow    string   `json:"workflow"`
	Occurrences []string `json:"occurrences"`
	Validated   struct {
		Run string `json:"run"`
	} `json:"validated"`
	Repair string `json:"repair,omitempty"`
}

// Merge is workflow.merged's payload (D4).
type Merge struct {
	Workflow string `json:"workflow"`
	Shape    string `json:"shape"`
	PR       string `json:"pr"`
}

// Error is a flywheel refusal naming the gate that refused.
type Error struct {
	Gate, Verb, Subject, Reason string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s on %s refused at gate %s: %s (spec/flywheel.md)", e.Verb, e.Subject, e.Gate, e.Reason)
}

// The gates.
const (
	GateShape       = "proposal.shape"
	GatePath        = "proposal.path"
	GateOccurrences = "proposal.occurrences"
	GateDuplicate   = "proposal.duplicate"
	GateRepairOpen  = "proposal.repair_open"
	GateRepairCited = "proposal.repair"
	GateMerge       = "merge.proposal"
)

func strict(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// ParseProposal decodes and shape-checks a proposal on its subject,
// the shape id.
func ParseProposal(subject string, raw []byte) (*Proposal, error) {
	var p Proposal
	if err := strict(raw, &p); err != nil {
		return nil, &Error{Gate: GateShape, Verb: ProposedVerb, Subject: subject, Reason: "the payload is the strict object {shape, workflow, occurrences, validated: {run}, repair?}: " + err.Error()}
	}
	if p.Shape == "" || p.Shape != subject {
		return nil, &Error{Gate: GateShape, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("the proposal names shape %q on subject %q: a proposal is appended on its shape", p.Shape, subject)}
	}
	if _, _, ok := anchorParts(p.Workflow); !ok {
		return nil, &Error{Gate: GatePath, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("workflow %q is not \"<path> @ <commit>\"", p.Workflow)}
	}
	if strings.TrimSpace(p.Validated.Run) == "" {
		return nil, &Error{Gate: GateShape, Verb: ProposedVerb, Subject: subject, Reason: "validated.run names the mock run that accepted the draft"}
	}
	if len(p.Occurrences) == 0 {
		return nil, &Error{Gate: GateOccurrences, Verb: ProposedVerb, Subject: subject, Reason: "a proposal cites the done occurrences it was drafted from"}
	}
	for _, c := range p.Occurrences {
		if _, _, ok := parseCite(c); !ok {
			return nil, &Error{Gate: GateOccurrences, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("occurrence %q is not \"<contract>@<position>\"", c)}
		}
	}
	if p.Repair != "" {
		if _, _, ok := parseCite(p.Repair); !ok {
			return nil, &Error{Gate: GateRepairCited, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("repair %q is not \"<contract>@<position>\"", p.Repair)}
		}
	}
	return &p, nil
}

// ParseMerge decodes and shape-checks a merge observation on its
// subject, the shape id.
func ParseMerge(subject string, raw []byte) (*Merge, error) {
	var m Merge
	if err := strict(raw, &m); err != nil {
		return nil, &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: "the payload is the strict object {workflow, shape, pr}: " + err.Error()}
	}
	if m.Shape == "" || m.Shape != subject {
		return nil, &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: fmt.Sprintf("the observation names shape %q on subject %q", m.Shape, subject)}
	}
	if _, _, ok := anchorParts(m.Workflow); !ok {
		return nil, &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: fmt.Sprintf("workflow %q is not \"<path> @ <merged-commit>\"", m.Workflow)}
	}
	_, prCommit, ok := anchorParts(m.PR)
	if !ok {
		return nil, &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: fmt.Sprintf("pr %q is not \"<pr> @ <merged-commit>\"", m.PR)}
	}
	// One observation names one merge, so its two anchors name one
	// revision (review finding on the task PR): a file anchored at one
	// commit and a PR at another describes no merge that happened.
	if _, fileCommit, _ := anchorParts(m.Workflow); fileCommit != prCommit {
		return nil, &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: fmt.Sprintf("the workflow is anchored at %s and the pr at %s: one observation names one merged revision", fileCommit, prCommit)}
	}
	return &m, nil
}

func parseCite(s string) (contract string, pos int, ok bool) {
	contract, p, found := strings.Cut(s, "@")
	if !found || contract == "" {
		return "", 0, false
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 0 {
		return "", 0, false
	}
	return contract, n, true
}

// CheckProposal holds a proposal to the record (D4): at least
// RecurringAfter distinct admitted done occurrences, each folding to
// the named shape (recomputed from the record); a path under the
// registry; no standing unmerged proposal for the shape; and no
// repair contract short of a passed verdict, a passed one being cited.
func CheckProposal(records []*event.Record, fold *transition.Fold, subject string, p *Proposal) error {
	path, _, _ := anchorParts(p.Workflow)
	if !strings.HasPrefix(path, RegistryDir+"/") || !strings.HasSuffix(path, ".yaml") || strings.Contains(strings.TrimPrefix(path, RegistryDir+"/"), "/") {
		return &Error{Gate: GatePath, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("workflow path %q is not a file directly under %s/", path, RegistryDir)}
	}
	shape, ok := Find(Shapes(records, fold), p.Shape)
	if !ok {
		return &Error{Gate: GateOccurrences, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("no done contract folds to shape %s", p.Shape)}
	}
	byCite := map[string]bool{}
	for _, occ := range shape.Occurrences {
		byCite[occ.Cite()] = true
	}
	seen := map[string]bool{}
	for _, c := range p.Occurrences {
		if seen[c] {
			return &Error{Gate: GateOccurrences, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("occurrence %s is cited twice: recurrence counts distinct done contracts", c)}
		}
		seen[c] = true
		if !byCite[c] {
			contract, _, _ := parseCite(c)
			reason := fmt.Sprintf("occurrence %s is not a done contract folding to shape %s", c, p.Shape)
			if s, ok := fold.State(contract); ok && s.State != "done" {
				reason = fmt.Sprintf("occurrence %s is %s, not done: only done contracts recur", c, s.State)
			}
			return &Error{Gate: GateOccurrences, Verb: ProposedVerb, Subject: subject, Reason: reason}
		}
	}
	if len(seen) < RecurringAfter {
		return &Error{Gate: GateOccurrences, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("%d occurrence(s) cited and a shape recurs at %d: a chore is what an agent did twice", len(seen), RecurringAfter)}
	}
	if standing, ok := Fold(records).Standing(p.Shape); ok {
		return &Error{Gate: GateDuplicate, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("a proposal for shape %s stands unmerged at position %d", p.Shape, standing.Pos)}
	}
	open, passed := Repairs(records, fold, p.Shape)
	if len(open) > 0 {
		return &Error{Gate: GateRepairOpen, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("repair contract %s stands short of a passed verdict: the implementer's fix passes its verdict before the proposal cites it", open[0])}
	}
	if p.Repair == "" && len(passed) > 0 {
		return &Error{Gate: GateRepairCited, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("repair contract %s passed its verdict and the proposal does not cite it", passed[0].Subject)}
	}
	if p.Repair != "" {
		contract, pos, _ := parseCite(p.Repair)
		cited := false
		for _, r := range passed {
			if r.Subject == contract && r.Verdict == pos {
				cited = true
			}
		}
		if !cited {
			return &Error{Gate: GateRepairCited, Verb: ProposedVerb, Subject: subject, Reason: fmt.Sprintf("repair %s is not a repair contract for shape %s with a passed verdict at that position", p.Repair, p.Shape)}
		}
	}
	return nil
}

// Repair is a repair contract in the fold: its subject and, once
// passed, the position of its verdict.
type Repair struct {
	Subject string
	Verdict int
}

// Cite is the citation a proposal carries for a passed repair.
func (r Repair) Cite() string { return fmt.Sprintf("%s@%d", r.Subject, r.Verdict) }

// Repairs lists the shape's repair contracts, by their acceptance
// path: open ones short of an AUTHENTIC passed verdict, in subject
// order, and passed ones with their verdict positions. The verdict is
// re-judged at its own position (review finding on the task PR): a
// transition-legal pass raw-pushed by an ungranted or implementing key
// populates the tolerant fold's Verdict, and citing it would clear
// proposal.repair_open without the verifier grant, the independence
// rule or the scoring the boundary applies. A repair carrying such a
// verdict stays open, which is the safe direction.
func Repairs(records []*event.Record, fold *transition.Fold, shape string) (open []string, passed []Repair) {
	table, terr := transition.Default()
	for _, contract := range fold.Subjects() {
		s, ok := fold.State(contract)
		if !ok {
			continue
		}
		if id, isRepair := IsRepair(s); !isRepair || id != shape {
			continue
		}
		if terr == nil && curation.AuthenticPass(records, table, contract, s) != nil {
			passed = append(passed, Repair{Subject: contract, Verdict: s.Verdict.Pos})
		} else {
			open = append(open, contract)
		}
	}
	return open, passed
}

// CheckMerge holds a merge observation to the record: an admitted
// proposal for the shape stands, and the observed file is the one it
// proposed.
func CheckMerge(records []*event.Record, fold *transition.Fold, subject string, m *Merge) error {
	standing, ok := Fold(records).Standing(m.Shape)
	if !ok {
		return &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: fmt.Sprintf("no unmerged proposal stands for shape %s: a merge observation cites an admitted proposal", m.Shape)}
	}
	if path, _, _ := anchorParts(m.Workflow); path != standing.Path() {
		return &Error{Gate: GateMerge, Verb: MergedVerb, Subject: subject, Reason: fmt.Sprintf("the observed file %q is not the proposed %q", path, standing.Path())}
	}
	return nil
}

// ProposalFact is an admitted proposal in the fold.
type ProposalFact struct {
	Pos   int
	Actor string
	Proposal
}

// Path is the proposed file's path.
func (f ProposalFact) Path() string {
	path, _, _ := anchorParts(f.Workflow)
	return path
}

// MergeFact is an admitted merge observation in the fold.
type MergeFact struct {
	Pos   int
	Actor string
	Merge
}

// State is the flywheel fold: proposals and merges by shape, in chain
// order, with the raw facts the boundary would have refused counted as
// anomalies and never bound.
type State struct {
	Proposals map[string][]ProposalFact
	Merges    map[string][]MergeFact
	Anomalies int
	order     []string
}

// Shapes returns the shape ids that carry a fact, in first-fact order.
func (s *State) Shapes() []string { return append([]string(nil), s.order...) }

// Standing returns the shape's latest proposal not yet observed merged.
func (s *State) Standing(shape string) (ProposalFact, bool) {
	ps := s.Proposals[shape]
	if len(ps) == 0 {
		return ProposalFact{}, false
	}
	last := ps[len(ps)-1]
	for _, m := range s.Merges[shape] {
		if m.Pos > last.Pos {
			return ProposalFact{}, false
		}
	}
	return last, true
}

// Merged reports whether the shape has an admitted merge observation.
func (s *State) Merged(shape string) bool { return len(s.Merges[shape]) > 0 }

// Fold binds the flywheel facts: a proposal or a merge folds only when
// it passed the boundary at its own position, re-judged through the
// same checks (the curation fold's posture): the signer held an
// accepted grant in the keyring at that position, and the record
// gates admit. A raw push binds nothing.
func Fold(records []*event.Record) *State {
	st := &State{Proposals: map[string][]ProposalFact{}, Merges: map[string][]MergeFact{}}
	table, terr := transition.Default()
	note := func(shape string) {
		if !contains(st.order, shape) {
			st.order = append(st.order, shape)
		}
	}
	for pos, rec := range records {
		e := &rec.Event
		if !keyring.Applies(e.V) || (e.Verb != ProposedVerb && e.Verb != MergedVerb) {
			continue
		}
		if terr != nil {
			st.Anomalies++
			continue
		}
		prefix := records[:pos]
		ring, _, err := keyring.StateAt(prefix)
		if err != nil || ring == nil || !ring.HasAnyCapability(e.Actor, keyring.AcceptedCapabilities(e.Verb)) {
			st.Anomalies++
			continue
		}
		fold := table.FoldRecords(prefix)
		switch e.Verb {
		case ProposedVerb:
			p, err := ParseProposal(e.Subject, e.Payload)
			if err != nil || CheckProposal(prefix, fold, e.Subject, p) != nil {
				st.Anomalies++
				continue
			}
			st.Proposals[p.Shape] = append(st.Proposals[p.Shape], ProposalFact{Pos: pos, Actor: e.Actor, Proposal: *p})
			note(p.Shape)
		case MergedVerb:
			m, err := ParseMerge(e.Subject, e.Payload)
			if err != nil || CheckMerge(prefix, fold, e.Subject, m) != nil {
				st.Anomalies++
				continue
			}
			st.Merges[m.Shape] = append(st.Merges[m.Shape], MergeFact{Pos: pos, Actor: e.Actor, Merge: *m})
			note(m.Shape)
		}
	}
	return st
}

// Any reports whether the fold carries a flywheel fact.
func (s *State) Any() bool { return len(s.order) > 0 }

// Metrics is the report's flywheel section, derived from the record
// alone (D5): shapes recurring, proposed and merged, the repair
// contracts filed and done, and merged over recurring.
type Metrics struct {
	Recurring int `json:"recurring"`
	Proposed  int `json:"proposed"`
	Merged    int `json:"merged"`
	Repairs   struct {
		Filed int `json:"filed"`
		Done  int `json:"done"`
	} `json:"repairs"`
	ConversionRate *string `json:"conversion_rate"`
}

// Derive computes the metrics over the record.
func Derive(records []*event.Record, fold *transition.Fold) Metrics {
	var m Metrics
	st := Fold(records)
	for _, shape := range Shapes(records, fold) {
		if !shape.Recurring() {
			continue
		}
		m.Recurring++
		if len(st.Proposals[shape.ID]) > 0 {
			m.Proposed++
		}
		if st.Merged(shape.ID) {
			m.Merged++
		}
	}
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok {
			continue
		}
		if _, isRepair := IsRepair(s); !isRepair {
			continue
		}
		m.Repairs.Filed++
		if s.State == "done" {
			m.Repairs.Done++
		}
	}
	if m.Recurring > 0 {
		rate := fmt.Sprintf("%.3f", float64(m.Merged)/float64(m.Recurring))
		m.ConversionRate = &rate
	}
	return m
}

// executable reports whether a file is runnable on this platform:
// the mode bits where the platform has them, existence as a regular
// file where it has not (spec/platform.md).
func executable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return info.Mode().IsRegular()
	}
	return info.Mode()&0o111 != 0
}
