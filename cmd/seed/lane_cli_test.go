package main

// The lane surface end to end (plans/os-cf1c9688.md): the six shipped
// manifests validate clean, resolution is ordered and stable through
// the CLI, and the two places that could drift from an authority
// elsewhere are pinned by their own drills.

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
)

// shippedLanes is the checked-in set this repository ships.
func shippedLanes(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "lanes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the shipped lanes directory is missing: %v", err)
	}
	return dir
}

// conformance: III.J — role definitions exist for all six lanes and
// are CHECKED. A green validate on the shipped set is the assertion
// that every declaration in it is answerable by the tables the system
// already enforces.
func TestShippedLanesValidate(t *testing.T) {
	dir := shippedLanes(t)
	e, code := runEnv(t, "lane", "validate", "--lanes", dir)
	if code != 0 || !e.OK {
		t.Fatalf("the shipped lanes must validate clean: %d %+v", code, e)
	}
	// The COMPLETE half of the closed enumeration (plans/os-d6a52784.md
	// D3): internal/lane refuses a seventh lane by name, and this drill,
	// over the shipped set, asserts all six are present. The two roles
	// the charter defines outside the loop are counted apart, so "eight
	// manifests" never reads as "eight lanes".
	if e.Result["lanes"] != "6" || e.Result["roles"] != "2" {
		t.Fatalf("the charter names six lanes (SEED-NEXT.md §II.11) and two non-loop roles (§II.9, §8): %+v", e.Result)
	}

	e, code = runEnv(t, "lane", "list", "--lanes", dir)
	if code != 0 {
		t.Fatalf("lane list: %d %+v", code, e)
	}
	rows, _ := e.Result["lanes"].([]any)
	byKind := map[string][]string{}
	for _, r := range rows {
		m, _ := r.(map[string]any)
		byKind[m["kind"].(string)] = append(byKind[m["kind"].(string)], m["lane"].(string))
	}
	for k := range byKind {
		slices.Sort(byKind[k])
	}
	want := slices.Clone(lane.CharterLanes())
	slices.Sort(want)
	if !slices.Equal(byKind[lane.KindLane], want) {
		t.Fatalf("the six lanes are the charter's six, all present: %v, want %v", byKind[lane.KindLane], want)
	}
	if !slices.Equal(byKind[lane.KindRole], []string{"observer", "supervisor"}) {
		t.Fatalf("the two roles are the charter's supervisor and observer: %v", byKind[lane.KindRole])
	}
}

// conformance: `seed lane show` resolves the ordered fragments and is
// byte-identical across runs; a lane that does not exist is not_found.
func TestLaneShowResolvesAndIsStable(t *testing.T) {
	dir := shippedLanes(t)
	first, code := runEnv(t, "lane", "show", "implementer", "--lanes", dir)
	if code != 0 || !first.OK {
		t.Fatalf("lane show: %d %+v", code, first)
	}
	body, _ := first.Result["resolved"].(string)
	if body == "" {
		t.Fatalf("show resolves the fragments: %+v", first.Result)
	}
	// The lane-specific fragment leads, and a shared convention that
	// every lane composes follows it: that IS the composition.
	if !strings.Contains(body, "# Implementer") {
		t.Fatalf("the lane's own fragment resolves first: %q", body[:min(80, len(body))])
	}
	if !strings.Contains(body, "one-inbox doctrine") {
		t.Fatal("a shared fragment must resolve into every lane that composes it")
	}
	second, _ := runEnv(t, "lane", "show", "implementer", "--lanes", dir)
	if second.Result["resolved"] != first.Result["resolved"] {
		t.Fatal("resolution must be byte-identical across runs")
	}
	if e, code := runEnv(t, "lane", "show", "wizard", "--lanes", dir); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("an unknown lane is not_found: %d %+v", code, e)
	}
}

// conformance: a broken manifest refuses with the lane_invalid code
// and carries the findings, so a caller branches on the code and reads
// the rows rather than parsing prose.
func TestLaneValidateRefusesWithFindings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, lane.FragmentDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "planner.json"), []byte(`{
  "lane": "planner",
  "kind": "lane",
  "summary": "s",
  "grants": ["wizard"],
  "orients_from": "seed situation --key <key>",
  "acts_through": [],
  "liveness_from": [],
  "inbox": "i",
  "fragments": []
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "lane", "validate", "--lanes", dir)
	if code != 26 || e.Error == nil || e.Error.Code != "lane_invalid" {
		t.Fatalf("a broken manifest exits 26 lane_invalid: %d %+v", code, e)
	}
	rows, _ := e.Result["findings"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the refusal carries its findings: %+v", e.Result)
	}
}

// conformance: internal/lane holds the situation read's flag set
// because package main is not importable, so this drill is what keeps
// the two from drifting. Adding a flag to `seed situation` without
// telling the validator fails HERE, which is the whole reason the
// duplication is tolerable.
func TestSituationFlagsAgreeWithTheCLI(t *testing.T) {
	// The real surface, bound the way runSituation binds it: the
	// posture pair comes from bindReadPosture, so calling it here is
	// what keeps this drill honest rather than restating its flags in
	// a literal that can drift from it.
	fs := flag.NewFlagSet("situation", flag.ContinueOnError)
	bindReadPosture(fs)
	fs.String("key", "", "")
	fs.String("subject", "", "")
	fs.String("since", "", "")
	var actual []string
	fs.VisitAll(func(f *flag.Flag) { actual = append(actual, f.Name) })
	var declared []string
	for _, f := range lane.SituationFlags() {
		declared = append(declared, f.Name)
	}
	slices.Sort(actual)
	slices.Sort(declared)
	if !slices.Equal(actual, declared) {
		t.Fatalf("internal/lane validates orients_from against %v, the situation read takes %v", declared, actual)
	}

	// The surface's DEMANDS, derived by asking it rather than read off
	// the declaration. A drill that only iterates the flags already
	// marked required is vacuous when the mark is removed — the same
	// failure shape as the finding that prompted it — so each demand is
	// discovered by building an invocation that breaks it and observing
	// whether the surface refuses as `usage` before the read begins.
	dir := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := map[string]string{
		"ledger": dir, "remote": dir, "ref": "refs/seed/ledger", "state": t.TempDir(),
		"supported": "seed/1", "key": keyPath, "subject": "c-1", "since": "0",
	}
	// A baseline that parses: one posture arm and every optional flag.
	// Each case below perturbs exactly one thing about it.
	baseline := func(skip map[string]bool, add ...string) []string {
		args := []string{"situation"}
		for _, f := range lane.SituationFlags() {
			if skip[f.Name] || (f.Posture && f.Name != "ledger") {
				continue
			}
			args = append(args, "--"+f.Name, value[f.Name])
		}
		return append(args, add...)
	}
	refusesAsUsage := func(args []string) bool {
		e, _ := runEnv(t, args...)
		return e.Error != nil && e.Error.Code == "usage"
	}

	// Unconditional requirements: omit one at a time.
	for _, f := range lane.SituationFlags() {
		if f.Posture {
			continue
		}
		if got := refusesAsUsage(baseline(map[string]bool{f.Name: true})); got != f.Required {
			t.Errorf("internal/lane declares --%s required=%v, the situation read behaves as required=%v",
				f.Name, f.Required, got)
		}
	}

	// The posture pair is an exclusive-or, and BOTH arms are checked:
	// naming neither and naming both must each refuse, while naming
	// exactly one must get past parsing. A model with a single required
	// flag cannot express this, which is why it is derived here rather
	// than asserted from the declaration.
	var postureNames []string
	for _, f := range lane.SituationFlags() {
		if f.Posture {
			postureNames = append(postureNames, f.Name)
		}
	}
	if len(postureNames) < 2 {
		t.Fatalf("this drill needs the posture pair, internal/lane declares %v", postureNames)
	}
	neither := map[string]bool{}
	for _, name := range postureNames {
		neither[name] = true
	}
	if !refusesAsUsage(baseline(neither)) {
		t.Error("naming no posture must refuse as usage: the read would have no ledger to derive from")
	}
	both := baseline(nil)
	for _, name := range postureNames {
		if name != "ledger" {
			both = append(both, "--"+name, value[name])
		}
	}
	if !refusesAsUsage(both) {
		t.Error("naming both postures must refuse as usage: the read could not say which view it stamped")
	}
	for _, name := range postureNames {
		skip := map[string]bool{}
		for _, other := range postureNames {
			if other != name {
				skip[other] = true
			}
		}
		args := []string{"situation"}
		for _, f := range lane.SituationFlags() {
			if f.Posture && f.Name != name {
				continue
			}
			args = append(args, "--"+f.Name, value[f.Name])
		}
		if refusesAsUsage(args) {
			t.Errorf("naming exactly --%s must get past parsing: it is one arm of the posture pair", name)
		}
	}

	// And every shipped manifest's read is one the surface accepts in
	// SHAPE: real flags, no missing unconditional requirement, and
	// exactly one posture arm.
	required := map[string]bool{}
	for _, f := range lane.SituationFlags() {
		if f.Required {
			required[f.Name] = true
		}
	}
	for _, m := range mustLoad(t) {
		cited := map[string]bool{}
		for _, tok := range strings.Fields(m.OrientsFrom) {
			if !strings.HasPrefix(tok, "--") {
				continue
			}
			name := strings.TrimPrefix(tok, "--")
			cited[name] = true
			if !slices.Contains(declared, name) {
				t.Errorf("%s orients from %q, which names an unknown flag", m.Lane, m.OrientsFrom)
			}
		}
		for name := range required {
			if !cited[name] {
				t.Errorf("%s orients from %q, which omits the required --%s and would exit 64",
					m.Lane, m.OrientsFrom, name)
			}
		}
		named := 0
		for _, name := range postureNames {
			if cited[name] {
				named++
			}
		}
		if named != 1 {
			t.Errorf("%s orients from %q, which names %d of the posture pair %v: the surface takes exactly one",
				m.Lane, m.OrientsFrom, named, postureNames)
		}
	}
}

// conformance: the loop-verb registry has exactly two consumers, and
// this is the drill that proves the CLI is one of them. Every act the
// registry names must be reachable as a CLI subverb, and the dispatch
// must refuse an unknown one by naming the registry's own list.
func TestLoopVerbRegistryDrivesTheCLI(t *testing.T) {
	for _, act := range loopverb.Acts() {
		e, code := runEnv(t, act.Group, act.Sub)
		// Reached the act and refused on its own flags, never on an
		// unknown subverb: that is what "the dispatch reads the
		// registry" means from outside.
		if code == 0 {
			t.Fatalf("%s with no flags must refuse: %+v", act.Name(), e)
		}
		if e.Error != nil && strings.Contains(e.Error.Message, "unknown") {
			t.Fatalf("%s is a registered act, so the dispatch must reach it: %s", act.Name(), e.Error.Message)
		}
	}
	// Every group refuses an unknown subverb the same way and names
	// the registry's own alternatives. Checked per GROUP rather than
	// once: the submission dispatch compared against a literal "make"
	// while claim and budget resolved through the registry, and a
	// single-group drill missed it (review finding on this PR).
	for _, group := range []string{"claim", "budget", "submission"} {
		e, code := runEnv(t, group, "yeet")
		if code != 64 || e.Error == nil {
			t.Fatalf("%s yeet must refuse as usage: %d %+v", group, code, e)
		}
		if !strings.Contains(e.Error.Message, "unknown "+group+" subverb") {
			t.Errorf("%s must refuse an unknown subverb as unknown, so the registry is the authority: %s",
				group, e.Error.Message)
		}
		for _, sub := range loopverb.Subverbs(group) {
			if !strings.Contains(e.Error.Message, sub) {
				t.Errorf("%s: the refusal names the registry's own alternatives, missing %q: %s",
					group, sub, e.Error.Message)
			}
		}
	}
}

func mustLoad(t *testing.T) []lane.Manifest {
	t.Helper()
	ms, err := lane.Load(shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// conformance: the reviewer's exact case (review finding on #212) —
// `seed lane validate` on a copy of the shipped set with planner.json
// removed refuses with lane_invalid and names the absent lane, rather
// than certifying "lanes: 5". This runs through the CLI so it is the
// path an operator's --lanes directory actually takes.
func TestLaneValidateRefusesASetMissingACharterLane(t *testing.T) {
	src := shippedLanes(t)
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "planner.json")); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "lane", "validate", "--lanes", dir)
	if code != 26 || e.Error == nil || e.Error.Code != "lane_invalid" {
		t.Fatalf("a set missing a charter lane exits 26 lane_invalid, got %d %+v", code, e)
	}
	rows, _ := e.Result["findings"].([]any)
	named := false
	for _, r := range rows {
		f, _ := r.(map[string]any)
		if f["lane"] == "planner" && f["field"] == "kind" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the finding names the absent lane: %+v", rows)
	}
	if e.Result["lanes"] != "5" {
		t.Errorf("the count is honest about what was found: %+v", e.Result)
	}
}
