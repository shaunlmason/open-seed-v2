// Package protections is the forge's protections as declared desired
// state, reconciled by command (SEED-NEXT.md §II.14; plans/os-5c8a312c.md
// D5, D6). The desired state derives from the deployment declaration
// and the charter's rules with no second source; a Forge adapter reads
// what the forge has and applies a diff; anything the forge cannot
// express is reported by name as manual work, never dropped.
package protections

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// Targets a ruleset applies to.
const (
	TargetBranch = "branch"
	TargetTag    = "tag"
)

// Rule types, the forge-neutral vocabulary the adapters translate.
const (
	RuleDeletion       = "deletion"         // the ref cannot be deleted
	RuleNonFastForward = "non_fast_forward" // no force-push
	RuleUpdate         = "update"           // updates only by bypass actors
	RulePullRequest    = "pull_request"     // reviews and thread resolution before merge
	RuleStatusChecks   = "required_status_checks"
)

// The ruleset names the declaration derives; a forge reports them back
// by name so the diff is by name too.
const (
	RulesetLedger    = "seed-ledger"
	RulesetDefault   = "seed-default-branch"
	RulesetContracts = "seed-contract-branches"
	RulesetTags      = "seed-release-tags"
)

// Rule is one rule with its normalized parameters.
type Rule struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// Ruleset is one named protection over some refs.
type Ruleset struct {
	Name   string   `json:"name"`
	Target string   `json:"target"`
	Refs   []string `json:"refs"`
	Rules  []Rule   `json:"rules"`
	Bypass []string `json:"bypass,omitempty"` // identities that may update despite RuleUpdate
}

// State is what a forge holds (or should): rulesets by name, the
// default branch, and the rule types this forge cannot express.
type State struct {
	DefaultBranch string             `json:"default_branch"`
	Rulesets      map[string]Ruleset `json:"rulesets"`
	Unexpressible []string           `json:"unexpressible,omitempty"`
}

// Desired derives the forge's desired state from the declaration and
// the charter's rules: the ledger branch updatable by the admission
// identity alone with no force-push and no deletion; the default
// branch requiring the declared checks and reviews with conversation
// resolution, no force-push, no deletion; contract branches protected
// from force-push and deletion; release tags immutable. The default
// branch is read, never declared (#241's hook reads the same symref).
func Desired(cfg *posture.Config, defaultBranch string) (*State, error) {
	if cfg == nil || cfg.Posture != posture.EnforcedForgeHosted || cfg.Admission == nil {
		return nil, fmt.Errorf("protections derive from an %s declaration with its admission block", posture.EnforcedForgeHosted)
	}
	if defaultBranch == "" {
		return nil, errors.New("the default branch is read from the repository's HEAD and cannot be empty")
	}
	a := cfg.Admission
	checks := append([]string{}, a.Checks...)
	sort.Strings(checks)
	st := &State{DefaultBranch: defaultBranch, Rulesets: map[string]Ruleset{}}
	st.Rulesets[RulesetLedger] = Ruleset{
		Name: RulesetLedger, Target: TargetBranch, Refs: []string{cfg.LedgerRef()},
		Rules:  []Rule{{Type: RuleDeletion}, {Type: RuleNonFastForward}, {Type: RuleUpdate}},
		Bypass: []string{a.Identity},
	}
	defaultRules := []Rule{{Type: RuleDeletion}, {Type: RuleNonFastForward},
		{Type: RulePullRequest, Params: map[string]any{
			"required_approving_review_count":   a.Reviews,
			"required_review_thread_resolution": true,
			// Owners declared means CODEOWNERS is rendered, and the
			// review requirement lands on the protected surface only
			// if the forge asks the owners, not anyone.
			"require_code_owner_review": len(a.Owners) > 0,
		}}}
	if len(checks) > 0 {
		defaultRules = append(defaultRules, Rule{Type: RuleStatusChecks, Params: map[string]any{"contexts": checks}})
	}
	st.Rulesets[RulesetDefault] = Ruleset{
		Name: RulesetDefault, Target: TargetBranch, Refs: []string{"refs/heads/" + defaultBranch},
		Rules: defaultRules,
	}
	st.Rulesets[RulesetContracts] = Ruleset{
		Name: RulesetContracts, Target: TargetBranch, Refs: []string{"refs/heads/seed/*"},
		Rules: []Rule{{Type: RuleDeletion}, {Type: RuleNonFastForward}},
	}
	// The template's releases and Seed's (plans/os-2e46aa2f.md D8):
	// the hook refuses an update or deletion of any tag, and the forge
	// ruleset names both namespaces so neither posture lets a released
	// tag be retargeted.
	st.Rulesets[RulesetTags] = Ruleset{
		Name: RulesetTags, Target: TargetTag, Refs: []string{"refs/tags/v*", "refs/tags/seed/v*"},
		Rules: []Rule{{Type: RuleDeletion}, {Type: RuleNonFastForward}, {Type: RuleUpdate}},
	}
	return st, nil
}

// Change kinds a plan reports.
const (
	ChangeCreate = "create"
	ChangeUpdate = "update"
	ChangeDelete = "delete"
	ChangeManual = "manual" // the forge cannot express it; the click it needs is in Detail
)

// Change is one difference between desired and current.
type Change struct {
	Kind    string `json:"kind"`
	Ruleset string `json:"ruleset"`
	Detail  string `json:"detail"`
}

// Diff computes the changes that take current to desired. A desired
// ruleset carrying a rule the forge declares unexpressible becomes a
// manual change naming the rule, and the ruleset is compared without
// it so the rest still reconciles. Rulesets the forge holds under Seed's
// names but the declaration no longer wants are deletions; foreign
// rulesets are left alone.
func Diff(desired, current *State) []Change {
	var out []Change
	unexpressible := map[string]bool{}
	for _, u := range current.Unexpressible {
		unexpressible[u] = true
	}
	names := make([]string, 0, len(desired.Rulesets))
	for n := range desired.Rulesets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		want := desired.Rulesets[name]
		var expressible []Rule
		for _, r := range want.Rules {
			if unexpressible[r.Type] {
				out = append(out, Change{Kind: ChangeManual, Ruleset: name, Detail: fmt.Sprintf("the forge cannot express %s on %s: apply it by hand in the forge's settings and record that you did", r.Type, strings.Join(want.Refs, ","))})
				continue
			}
			expressible = append(expressible, r)
		}
		want.Rules = expressible
		have, ok := current.Rulesets[name]
		if !ok {
			out = append(out, Change{Kind: ChangeCreate, Ruleset: name, Detail: describe(want)})
			continue
		}
		if d := differences(want, have); len(d) > 0 {
			out = append(out, Change{Kind: ChangeUpdate, Ruleset: name, Detail: strings.Join(d, "; ")})
		}
	}
	extra := make([]string, 0)
	for name := range current.Rulesets {
		if _, ok := desired.Rulesets[name]; !ok && strings.HasPrefix(name, "seed-") {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		out = append(out, Change{Kind: ChangeDelete, Ruleset: name, Detail: "the declaration no longer wants it"})
	}
	return out
}

func describe(r Ruleset) string {
	types := make([]string, 0, len(r.Rules))
	for _, rule := range r.Rules {
		types = append(types, rule.Type)
	}
	s := fmt.Sprintf("%s over %s with %s", r.Target, strings.Join(r.Refs, ","), strings.Join(types, ","))
	if len(r.Bypass) > 0 {
		s += " bypassed by " + strings.Join(r.Bypass, ",")
	}
	return s
}

func differences(want, have Ruleset) []string {
	var d []string
	if want.Target != have.Target {
		d = append(d, fmt.Sprintf("target %s, forge has %s", want.Target, have.Target))
	}
	if !sameStrings(want.Refs, have.Refs) {
		d = append(d, fmt.Sprintf("refs %s, forge has %s", strings.Join(want.Refs, ","), strings.Join(have.Refs, ",")))
	}
	if !sameStrings(want.Bypass, have.Bypass) {
		d = append(d, fmt.Sprintf("bypass %s, forge has %s", strings.Join(want.Bypass, ","), strings.Join(have.Bypass, ",")))
	}
	wantRules := ruleIndex(want.Rules)
	haveRules := ruleIndex(have.Rules)
	for t, w := range wantRules {
		h, ok := haveRules[t]
		if !ok {
			d = append(d, "missing rule "+t)
			continue
		}
		if canon(w.Params) != canon(h.Params) {
			d = append(d, fmt.Sprintf("rule %s wants %s, forge has %s", t, canon(w.Params), canon(h.Params)))
		}
	}
	for t := range haveRules {
		if _, ok := wantRules[t]; !ok {
			d = append(d, "extra rule "+t)
		}
	}
	sort.Strings(d)
	return d
}

func ruleIndex(rules []Rule) map[string]Rule {
	m := map[string]Rule{}
	for _, r := range rules {
		m[r.Type] = r
	}
	return m
}

func canon(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// Forge is what the reconciler drives. Read returns the forge's
// current state (its rulesets by name, its default branch, and the rule
// types it cannot express); Apply performs the changes so that a
// following Read matches desired.
type Forge interface {
	Read() (*State, error)
	Apply(changes []Change, desired *State) error
}

// Snapshot is the credential-free adapter: the forge's state as a JSON
// file, which CI and the drills read and which Apply writes back. It is
// also the shape a deployment records the forge's state in by hand.
type Snapshot struct{ Path string }

func (s Snapshot) Read() (*State, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("reading the forge snapshot: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("the forge snapshot does not parse: %v", err)
	}
	if st.Rulesets == nil {
		st.Rulesets = map[string]Ruleset{}
	}
	return &st, nil
}

func (s Snapshot) Apply(changes []Change, desired *State) error {
	cur, err := s.Read()
	if err != nil {
		return err
	}
	unexpressible := map[string]bool{}
	for _, u := range cur.Unexpressible {
		unexpressible[u] = true
	}
	for _, c := range changes {
		switch c.Kind {
		case ChangeCreate, ChangeUpdate:
			want := desired.Rulesets[c.Ruleset]
			var rules []Rule
			for _, r := range want.Rules {
				if !unexpressible[r.Type] {
					rules = append(rules, r)
				}
			}
			want.Rules = rules
			cur.Rulesets[c.Ruleset] = want
		case ChangeDelete:
			delete(cur.Rulesets, c.Ruleset)
		case ChangeManual:
			// By definition not applied here: the plan named the click.
		}
	}
	b, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, append(b, '\n'), 0o644)
}

// CodeownersPath is where the rendering lands.
const CodeownersPath = ".github/CODEOWNERS"

// Codeowners renders the protected surface as a CODEOWNERS file: every
// declared prefix owned by the declaration's owners, so the forge's
// review requirement lands on exactly the surface the hook refuses for
// every non-root key. Written to the working tree for a reviewed PR,
// never pushed.
func Codeowners(cfg *posture.Config) (string, bool) {
	if cfg == nil || cfg.Admission == nil || len(cfg.Admission.Owners) == 0 {
		return "", false
	}
	owners := strings.Join(cfg.Admission.Owners, " ")
	prefixes := cfg.ProtectedSurface() // the declaration itself included, by construction
	var b strings.Builder
	b.WriteString("# Generated by `seed protections`: the protected surface (seed.json `protected`), owned by the governance root.\n")
	b.WriteString("# The admission hook refuses these paths for every key without root standing; the forge requires these reviews.\n")
	for _, p := range prefixes {
		b.WriteString("/" + strings.TrimSuffix(p, "/") + " " + owners + "\n")
	}
	return b.String(), true
}

// CodeownersDrift compares the rendering to the file in repoDir.
func CodeownersDrift(cfg *posture.Config, repoDir string) (want string, drift bool, err error) {
	want, ok := Codeowners(cfg)
	if !ok {
		return "", false, nil
	}
	have, err := os.ReadFile(filepath.Join(repoDir, CodeownersPath))
	if errors.Is(err, os.ErrNotExist) {
		return want, true, nil
	}
	if err != nil {
		return want, false, err
	}
	return want, string(have) != want, nil
}

// WriteCodeowners writes the rendering into the working tree.
func WriteCodeowners(cfg *posture.Config, repoDir string) (bool, error) {
	want, ok := Codeowners(cfg)
	if !ok {
		return false, nil
	}
	path := filepath.Join(repoDir, CodeownersPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(want), 0o644)
}

// Finding is one CI-identity lint result.
type Finding struct {
	File   string `json:"file"`
	Detail string `json:"detail"`
}

// LintWorkflows scans .github/workflows for a scheduled workflow that
// grants itself write access to contents: the charter says no
// scheduled job may push to the default branch, and a schedule trigger
// beside `contents: write` is that job. The scan is line-based on
// purpose — it names the file and the two lines, and a reviewer reads
// the workflow.
func LintWorkflows(repoDir string) ([]Finding, error) {
	dir := filepath.Join(repoDir, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		scheduled, writes := 0, 0
		sc := bufio.NewScanner(f)
		for n := 1; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "#") {
				continue
			}
			if line == "schedule:" && scheduled == 0 {
				scheduled = n
			}
			if strings.HasPrefix(line, "contents:") && strings.Contains(line, "write") && writes == 0 {
				writes = n
			}
		}
		f.Close()
		if scheduled > 0 && writes > 0 {
			out = append(out, Finding{File: filepath.Join(".github", "workflows", name), Detail: fmt.Sprintf("a scheduled workflow (line %d) with contents: write (line %d) can push to the default branch; scheduled identities are least-privilege", scheduled, writes)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}

// Report is what plan and apply return.
type Report struct {
	DefaultBranch  string    `json:"default_branch"`
	Changes        []Change  `json:"changes"`
	Manual         int       `json:"manual"`
	Codeowners     string    `json:"codeowners"` // "clean", "drift", "absent", or "n/a"
	Findings       []Finding `json:"ci_findings"`
	DriftCount     int       `json:"drift"`
	Applied        bool      `json:"applied"`
	CodeownersPath string    `json:"codeowners_path"`
}

// Plan reads the forge, derives the desired state from the declaration
// and the forge's default branch, and reports every difference: ruleset
// changes, manual rules, CODEOWNERS drift and CI-identity findings.
// Drift counts everything but manual rules, which are named separately
// so a plan with only manual work still reads as work.
func Plan(cfg *posture.Config, forge Forge, repoDir string) (*Report, *State, error) {
	current, err := forge.Read()
	if err != nil {
		return nil, nil, err
	}
	desired, err := Desired(cfg, current.DefaultBranch)
	if err != nil {
		return nil, nil, err
	}
	rep := &Report{DefaultBranch: current.DefaultBranch, Changes: Diff(desired, current), CodeownersPath: CodeownersPath, Codeowners: "n/a"}
	for _, c := range rep.Changes {
		if c.Kind == ChangeManual {
			rep.Manual++
		} else {
			rep.DriftCount++
		}
	}
	if repoDir != "" {
		_, drift, err := CodeownersDrift(cfg, repoDir)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := Codeowners(cfg); ok {
			rep.Codeowners = "clean"
			if drift {
				rep.Codeowners = "drift"
				rep.DriftCount++
			}
		}
		findings, err := LintWorkflows(repoDir)
		if err != nil {
			return nil, nil, err
		}
		rep.Findings = findings
		rep.DriftCount += len(findings)
	}
	if rep.Changes == nil {
		rep.Changes = []Change{}
	}
	if rep.Findings == nil {
		rep.Findings = []Finding{}
	}
	return rep, desired, nil
}

// Apply plans, performs the ruleset changes through the forge, writes
// CODEOWNERS into the working tree, and re-reads to a fresh plan; CI
// findings are reported, never edited (a workflow file is the
// protected surface's own).
func Apply(cfg *posture.Config, forge Forge, repoDir string) (*Report, error) {
	rep, desired, err := Plan(cfg, forge, repoDir)
	if err != nil {
		return nil, err
	}
	if err := forge.Apply(rep.Changes, desired); err != nil {
		return nil, err
	}
	if repoDir != "" {
		if _, err := WriteCodeowners(cfg, repoDir); err != nil {
			return nil, err
		}
	}
	after, _, err := Plan(cfg, forge, repoDir)
	if err != nil {
		return nil, err
	}
	after.Applied = true
	return after, nil
}
