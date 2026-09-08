package admit

// The curator poisoning drill (plans/os-e2f1ad23.md; charter §12,
// §16, III.K row 4): trajectories are untrusted inputs, and an
// attacker who can influence what agents experience can construct
// trajectories designed to teach the system something false. This
// suite is the injection suite's shape turned on the curation gates:
// a corpus of scripted poisons under testdata/poisoning/, each ending
// in an attempt to promote, each asserted to fail at BOTH ends (the
// refusal at its gate, and no lesson reaching a claim); a residual
// table naming what the boundary admits, each entry pinned; and
// coverage derived from the gate registry rather than authored for
// the drill.
//
// What it does NOT do, stated first: it does not test that a curator
// disbelieves hostile text. There is no model under next/, and a
// persuaded curator proposing a false claim over genuine support is a
// named residual, not a poison the boundary can refuse. It tests that
// constructing the support, the contest, the promotion or the file
// cannot get a false lesson in front of a worker.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// poison is one corpus entry: the gate it attacks (empty when the
// boundary refuses it outside the registry), the attack in the
// attacker's words, and what the drill expects: the verb that refuses
// and either the gate or a reason the refusal names.
type poison struct {
	Name   string `json:"name"`
	Gate   string `json:"gate"`
	Attack string `json:"attack"`
	Expect struct {
		Verb   string `json:"verb"`
		Gate   string `json:"gate"`
		Reason string `json:"reason"`
	} `json:"expect"`
}

// poisonResidual is one poison the boundary admits, with why, what it
// can inflict, and what stands in the way.
type poisonResidual struct {
	Name        string `json:"name"`
	WhyAdmitted string `json:"why_admitted"`
	Consequence string `json:"consequence"`
	InTheWay    string `json:"in_the_way"`
}

func poisoningDir() string { return filepath.Join("testdata", "poisoning") }

// loadPoisons reads the corpus; an empty corpus fails rather than
// passing every coverage check vacuously.
func loadPoisons(t *testing.T) []poison {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(poisoningDir(), "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Poisons []poison `json:"poisons"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Poisons) == 0 {
		t.Fatal("the poisoning corpus is empty: every coverage check below would pass vacuously")
	}
	seen := map[string]bool{}
	for _, p := range doc.Poisons {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Attack) == "" || strings.TrimSpace(p.Expect.Verb) == "" {
			t.Fatalf("poison %+v is not fully declared: a name, an attack and the verb expected to refuse", p)
		}
		if seen[p.Name] {
			t.Fatalf("poison %q is declared twice", p.Name)
		}
		seen[p.Name] = true
		if (p.Expect.Gate == "") == (p.Expect.Reason == "") {
			t.Fatalf("poison %q expects exactly one of a gate or a reason", p.Name)
		}
		if p.Gate != p.Expect.Gate {
			t.Fatalf("poison %q attacks gate %q and expects %q: the two name one gate", p.Name, p.Gate, p.Expect.Gate)
		}
	}
	return doc.Poisons
}

// loadPoisonResiduals reads the residual table; an empty table would
// mean every admitted poison is unnamed.
func loadPoisonResiduals(t *testing.T) []poisonResidual {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(poisoningDir(), "residuals.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Residuals []poisonResidual `json:"residuals"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Residuals) == 0 {
		t.Fatal("the residual table is empty: an admitted poison would go unnamed")
	}
	for _, r := range doc.Residuals {
		if strings.TrimSpace(r.WhyAdmitted) == "" || strings.TrimSpace(r.Consequence) == "" || strings.TrimSpace(r.InTheWay) == "" {
			t.Fatalf("residual %q is named but unexplained: why it is admitted, what it can inflict and what stands in the way", r.Name)
		}
	}
	return doc.Residuals
}

// specGateTable reads the gate table spec/curation.md carries:
// the rows whose first cell is a backticked gate name.
func specGateTable(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "curation.md"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cell := strings.TrimPrefix(line, "| `")
		name, _, ok := strings.Cut(cell, "`")
		if !ok || !strings.Contains(name, ".") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// poisonRun is what a script reports: the refusal the named verb met,
// the hypothesis subject the poison targets, and the contract a claim
// would take inside the poison's applies-when.
type poisonRun struct {
	st       *curationStand
	verb     string
	err      error
	subject  string
	selected string
}

// conformance: AC1 — coverage is derived. The gate set comes from
// curation.Gates(); it equals the spec's table both ways; every
// registered gate has a poison and every poison names a registered
// gate; every declared poison has a script and every script a
// declaration.
func TestPoisonCorpusCoversEveryRegisteredGate(t *testing.T) {
	registry := curation.Gates()
	if len(registry) == 0 {
		t.Fatal("the gate registry is empty: nothing to cover")
	}
	if spec := specGateTable(t); strings.Join(spec, ",") != strings.Join(registry, ",") {
		t.Fatalf("the spec's gate table and the registry differ:\n spec: %v\n registry: %v", spec, registry)
	}
	corpus := loadPoisons(t)
	byGate := map[string][]string{}
	for _, p := range corpus {
		if p.Gate == "" {
			continue
		}
		if _, ok := curation.GateDescription(p.Gate); !ok {
			t.Errorf("poison %q names gate %q, which no rule registers", p.Name, p.Gate)
		}
		byGate[p.Gate] = append(byGate[p.Gate], p.Name)
	}
	for _, g := range registry {
		if len(byGate[g]) == 0 {
			t.Errorf("gate %s has no poison: a gate nothing attacks is a gate nothing proves", g)
		}
	}
	for _, p := range corpus {
		if _, ok := poisonScripts[p.Name]; !ok {
			t.Errorf("poison %q is declared and has no script", p.Name)
		}
	}
	for name := range poisonScripts {
		found := false
		for _, p := range corpus {
			if p.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("script %q has no declaration in the corpus", name)
		}
	}
	// A gate planted at drill time with no poison must fail: coverage
	// is a claim the drill derives, not one it is told.
	planted := map[string][]string{}
	for g, ps := range byGate {
		planted[g] = ps
	}
	if len(planted) != len(registry) {
		t.Fatalf("%d gates covered of %d registered", len(planted), len(registry))
	}
}

// gateOf returns the GateError's gate, failing when the refusal is
// not one: every curation refusal names a registered gate.
func gateOfRefusal(t *testing.T, name string, err error) string {
	t.Helper()
	var ge *curation.GateError
	if !errors.As(err, &ge) {
		t.Fatalf("%s: the refusal is not a GateError: %v", name, err)
	}
	if _, ok := curation.GateDescription(ge.Gate); !ok {
		t.Fatalf("%s: refused at %q, which is not registered", name, ge.Gate)
	}
	return ge.Gate
}

// conformance: AC2 — every poison fails at both ends. The named verb
// refuses at the named gate (or names the reason); by the chain's end
// no lesson.promoted for the poisoned hypothesis stands admitted; and
// a claim on a contract the poison's applies-when selects carries no
// lesson for it. The last two are what keeps the drill red if a gate
// is rewritten so that the refusal moves.
func TestEveryPoisonFailsAtBothEnds(t *testing.T) {
	for _, p := range loadPoisons(t) {
		p := p
		t.Run(p.Name, func(t *testing.T) {
			script, ok := poisonScripts[p.Name]
			if !ok {
				t.Fatalf("no script for %s", p.Name)
			}
			run := script(t)
			if run.err == nil {
				t.Fatalf("%s: the attempt was ADMITTED at %s: %s", p.Name, run.verb, p.Attack)
			}
			if run.verb != p.Expect.Verb {
				t.Fatalf("%s: refused at verb %s, the corpus expects %s", p.Name, run.verb, p.Expect.Verb)
			}
			switch {
			case p.Expect.Gate != "":
				if got := gateOfRefusal(t, p.Name, run.err); got != p.Expect.Gate {
					t.Fatalf("%s: refused at gate %s, expected %s: %v", p.Name, got, p.Expect.Gate, run.err)
				}
			case p.Expect.Reason == "out of grant":
				var oog *OutOfGrantError
				if !errors.As(run.err, &oog) {
					t.Fatalf("%s: expected an out-of-grant refusal: %v", p.Name, run.err)
				}
			default:
				if !strings.Contains(strings.ToLower(run.err.Error()), p.Expect.Reason) {
					t.Fatalf("%s: the refusal does not name %q: %v", p.Name, p.Expect.Reason, run.err)
				}
			}
			if run.st == nil {
				// The file half judges a lesson the ledger promoted:
				// its other end is the store's lint under make check.
				return
			}
			ctx := run.st.ctx
			fold := curation.Fold(ctx.Records)
			if lessons := fold.LessonsOf(run.subject); len(lessons) != 0 {
				t.Fatalf("%s: a promotion for %s stands at the chain's end: %+v", p.Name, run.subject, lessons)
			}
			for _, l := range curation.Candidates(fold, ctx.Lifecycle, run.selected) {
				if c, _ := curation.ParseCitation(l.Hypothesis); c.Contract == run.subject {
					t.Fatalf("%s: a claim on %s would carry the poisoned lesson: %+v", p.Name, run.selected, l)
				}
			}
			if surfaced, _ := curation.Surfacing(ctx.Records, ctx.Lifecycle, "", run.selected, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); len(surfaced) != 0 {
				t.Fatalf("%s: the surfacing set for %s is not empty: %+v", p.Name, run.selected, surfaced)
			}
		})
	}
}

// conformance: AC3 — the residuals are pinned. Each entry has a drill
// asserting the poison IS admitted, in the residual's own words, so
// closing one fails the suite and forces the table to say what
// replaced it; an entry with no drill, or a drill with no entry, fails.
func TestPoisonResidualsArePinned(t *testing.T) {
	table := loadPoisonResiduals(t)
	named := map[string]poisonResidual{}
	for _, r := range table {
		named[r.Name] = r
	}
	for name := range residualDrills {
		if _, ok := named[name]; !ok {
			t.Errorf("residual drill %q has no entry in residuals.json", name)
		}
	}
	for _, r := range table {
		drill, ok := residualDrills[r.Name]
		if !ok {
			t.Errorf("residual %q is named and has no drill pinning it", r.Name)
			continue
		}
		t.Run(r.Name, drill)
	}
}

// residualDrills pin each admitted poison.
var residualDrills = map[string]func(t *testing.T){
	// A family with one holder admits support from that holder, and
	// the fold says so.
	"single-holder-family": func(t *testing.T) {
		st := curationFixture(t)
		single := "standard contracts retry"
		id := curation.HypothesisID(single, nil)
		body := st.proposalWith(single, `{"tier": "standard"}`, nil, cite("c-7", st.deadEnd7), cite("c-8", st.deadEnd8))
		if err := Check(st.ctx, draftV(t, st.curator, st.v, curation.HypothesisVerb, id, body, st.ctx.Tip)); err != nil {
			t.Fatalf("in a family with one holder the same-holder support admits: %v", err)
		}
		st.ctx = st.step(st.curator, st.v, curation.HypothesisVerb, id, body)
		if !curation.Fold(st.ctx.Records).SingleActorFamily(st.ctx.Records, st.ctx.Table, id) {
			t.Fatal("the fold records that the actor arm was waived")
		}
	},
	// Disjointness is per key: three keys one person operates satisfy
	// every arm, and nothing in the record says who holds them.
	"colluding-keys": func(t *testing.T) {
		st := curationFixture(t)
		if err := Check(st.ctx, draftV(t, st.curator, st.v, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)), st.ctx.Tip)); err != nil {
			t.Fatalf("two workers and a curator on distinct keys admit whoever operates them: %v", err)
		}
		ring, _, err := keyring.StateAt(st.ctx.Records)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []ed25519.PrivateKey{st.worker, st.worker2, st.curator} {
			a, ok := ring.Get(fpOf(t, key))
			if !ok || a.Kind != "agent" {
				t.Fatalf("the enrollment carries a kind the operator asserted and nothing binds it to a person: %+v", a)
			}
		}
	},
	// The boundary judges the support, never the claim's truth.
	"persuaded-curator": func(t *testing.T) {
		st := curationFixture(t)
		false_ := "the mirror is never the problem; always retry until it answers"
		id := curation.HypothesisID(false_, nil)
		body := st.proposalWith(false_, appliesCore, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2))
		if err := Check(st.ctx, draftV(t, st.curator, st.v, curation.HypothesisVerb, id, body, st.ctx.Tip)); err != nil {
			t.Fatalf("a false claim over genuine admitted observations admits: %v", err)
		}
	},
	// The boundary never reads the eval's definition: a pass on a
	// bound eval at a gated revision is survival whatever the
	// definition decides.
	"reviewed-vacuous-eval": func(t *testing.T) {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		anchor := curation.LessonsDir + "/retry.md @ 0123456"
		bound := st.evalRun(t, "eval-vacuous", cite(st.id, hp), anchor, "pass", nil)
		if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", bound), st.ctx.Tip)); err != nil {
			t.Fatalf("the boundary cannot tell a vacuous definition from a decisive one: %v", err)
		}
	},
	// A reworded claim is a new subject judged on its own support.
	"cosmetic-reproposal": func(t *testing.T) {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		st.ctx = st.step(st.curator, st.v, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6)))
		reworded := "retry the fetch a single time"
		id := curation.HypothesisID(reworded, nil)
		if id == st.id {
			t.Fatal("the reworded claim derives a new subject")
		}
		if err := Check(st.ctx, draftV(t, st.curator, st.v, curation.HypothesisVerb, id, st.proposalWith(reworded, appliesCore, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)), st.ctx.Tip)); err != nil {
			t.Fatalf("the reworded claim admits on the same support: %v", err)
		}
	},
}

// lintStand is a repository holding a lesson that agrees with its
// fact, its hypothesis and the repository: the file half's passing
// case, which each lint poison then bends in one place.
type lintStand struct {
	repo, path, body, anchor, planCommit string
	fact                                 curation.LessonFact
	h                                    *curation.HypothesisFact
	now                                  time.Time
	git                                  func(args ...string) string
}

func newLintStand(t *testing.T) *lintStand {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	for _, dir := range []string{curation.LessonsDir, "plans"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "plans", "x.md"), []byte("plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "plan")
	planCommit := git("rev-parse", "HEAD")
	id := curation.HypothesisID("retry once", nil)
	h := &curation.HypothesisFact{ID: id, Pos: 4, AppliesWhen: curation.AppliesWhen{Routing: "core"}, Support: []string{"c-1@4", "c-2@9"}, Provenance: []string{"plans/x.md @ " + planCommit}}
	body := "---\nhypothesis: " + id + "@4\napplies-when: {\"routing\": \"core\"}\nsupport: c-1@4, c-2@9\nprovenance: plans/x.md @ " + planCommit + "\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: knowledge\n---\n\n# Retry once\n"
	path := curation.LessonsDir + "/retry.md"
	if err := os.WriteFile(filepath.Join(repo, path), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "lesson")
	anchor := git("rev-parse", "HEAD")
	ls := &lintStand{repo: repo, path: path, body: body, anchor: anchor, planCommit: planCommit, h: h, git: git,
		now: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	ls.fact = curation.LessonFact{Lesson: path + " @ " + anchor, Hypothesis: id + "@4", Carrier: "knowledge", Digest: curation.Digest([]byte(body)),
		LastValidated: "2026-09-01T00:00:00Z", Expires: "2026-12-01T00:00:00Z"}
	if err := curation.LintFile(repo, []byte(body), ls.fact, h, ls.now); err != nil {
		t.Fatalf("the agreeing lesson lints: %v", err)
	}
	return ls
}

// promote commits a bent body as its own anchor on main and returns
// the fact a promotion of it would carry: the lint judges the bytes
// at the promoted anchor, so a bent file is a bent promotion.
func (ls *lintStand) promote(t *testing.T, body string) curation.LessonFact {
	t.Helper()
	if body == ls.body {
		return ls.fact
	}
	if err := os.WriteFile(filepath.Join(ls.repo, ls.path), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ls.git("commit", "--quiet", "-am", "bent")
	fact := ls.fact
	fact.Lesson = ls.path + " @ " + ls.git("rev-parse", "HEAD")
	fact.Digest = curation.Digest([]byte(body))
	return fact
}

// hardenGitRepo disables auto-gc in a fixture repository so a detached
// collector never outlives the test that made it (the fixture guard in
// internal/gitref sweeps every git init for this call).
func hardenGitRepo(t testing.TB, repo string) {
	t.Helper()
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"},
		{"core.autocrlf", "false"}, // the ledger is LF-only on every platform (spec/platform.md)
		{"core.eol", "lf"}} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("hardening %s (%s): %v %s", repo, kv[0], err, out)
		}
	}
}

// lintPoison runs the file half over a bent stand and reports the
// refusal as a lint attempt.
func lintPoison(t *testing.T, bend func(ls *lintStand) (body []byte, fact curation.LessonFact, h *curation.HypothesisFact, now time.Time)) *poisonRun {
	t.Helper()
	ls := newLintStand(t)
	body, fact, h, now := bend(ls)
	return &poisonRun{verb: "lint", err: curation.LintFile(ls.repo, body, fact, h, now)}
}

// withFrontmatter rewrites one frontmatter key in the stand's body.
func withFrontmatter(body, key, value string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, key+":") {
			line = key + ": " + value
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var _ = transition.VerdictRenderedVerb
