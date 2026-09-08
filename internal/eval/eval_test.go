package eval_test

// The eval package drills (plans/os-03e47abb.md; spec/evals.md):
// definitions load and refuse by name; the anchor is read from the
// repository and refuses an unreviewed or dirty definition; the check
// proves the known verdict through the verifier's own runner; the
// filing's id is stable in the eval, the tuple under re-test and the
// prior count; and Due derives what the chain owes at the DECLARED
// instant, with the time guards D5 names.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const baseTupleJSON = `{"principal": "acme", "harness": "local-worktree/v0", "model": "fable/5.1", "tool_policy": "default", "environment": "detached-git-worktree"}`

func baseTuple(t *testing.T) tuple.Tuple {
	t.Helper()
	tu, err := tuple.Parse([]byte(baseTupleJSON))
	if err != nil {
		t.Fatal(err)
	}
	return tu
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// hardenGitRepo disables auto-gc on a fixture repository so no detached
// gc outlives the test that made it (plans/os-c4e8b57a.md D1, D5).
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

// evalRepo is a repository carrying the shipped definitions under a
// squash-merge subject; it returns the repository and the anchor.
func evalRepo(t *testing.T) (repo, anchor string) {
	t.Helper()
	repo = t.TempDir()
	git(t, repo, "init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("eval fixture repository\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "--quiet", "-m", "base")
	copyTree(t, filepath.Join("..", "..", "evals"), filepath.Join(repo, eval.Root))
	git(t, repo, "add", ".")
	git(t, repo, "commit", "--quiet", "-m", "evals: the shipped definitions (#1)")
	return repo, git(t, repo, "rev-parse", "HEAD")
}

func copyTree(t *testing.T, from, to string) {
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
		return os.WriteFile(dst, b, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// plant writes a definition beside the shipped one and commits it
// under the given subject; files maps path to body under the eval's
// directory, with sensible defaults for what it omits.
func plant(t *testing.T, repo, name, subject string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(repo, eval.Root, name)
	all := map[string]string{
		"eval.json":         fmt.Sprintf(`{"name": %q, "summary": "planted", "tier": "trivial", "acceptance": "evals/%s/fixture/accept.md"}`, name, name),
		"fixture/greet.sh":  "#!/bin/sh\nprintf 'hello, wrold\\n'\n",
		"fixture/check.sh":  "#!/bin/sh\nout=$(sh \"$(dirname \"$0\")/greet.sh\")\n[ \"$out\" = \"hello, world\" ] || exit 1\n",
		"fixture/accept.md": "# planted\n\n## Validation commands\n\n- `sh evals/" + name + "/fixture/check.sh`\n",
		"solution/greet.sh": "#!/bin/sh\nprintf 'hello, world\\n'\n",
	}
	for k, v := range files {
		all[k] = v
	}
	for path, body := range all {
		if body == "" {
			continue // omitted on purpose
		}
		p := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := files["solution/greet.sh"]; ok && files["solution/greet.sh"] == "" {
		if err := os.MkdirAll(filepath.Join(dir, "solution"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", "-A", ".")
	git(t, repo, "commit", "--quiet", "--allow-empty", "-m", subject)
}

func TestLoadValidatesDefinitions(t *testing.T) {
	repo, _ := evalRepo(t)
	defs, err := eval.Load(repo)
	if err != nil || len(defs) != 1 || defs[0].Name != "fix-the-check" || defs[0].Tier != "trivial" || defs[0].Dir() != "evals/fix-the-check" {
		t.Fatalf("the shipped definition loads: %v %+v", err, defs)
	}
	if !strings.HasPrefix(defs[0].Acceptance, defs[0].Dir()+"/fixture/") {
		t.Fatalf("the acceptance spec lives under the fixture: %s", defs[0].Acceptance)
	}
	if _, ok := eval.Find(defs, "fix-the-check"); !ok {
		t.Fatal("Find resolves by name")
	}
	if _, ok := eval.Find(defs, "nope"); ok {
		t.Fatal("Find refuses an unknown name")
	}
	if defs, err := eval.Load(t.TempDir()); err != nil || defs != nil {
		t.Fatalf("a repository with no definitions loads none: %v %+v", err, defs)
	}
	// A directory without eval.json is not a definition; each
	// malformed one refuses by name.
	if err := os.MkdirAll(filepath.Join(repo, eval.Root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if defs, err := eval.Load(repo); err != nil || len(defs) != 1 {
		t.Fatalf("a directory with no eval.json is skipped: %v %+v", err, defs)
	}
	for name, files := range map[string]map[string]string{
		"misnamed":           {"eval.json": `{"name": "other", "summary": "s", "tier": "trivial", "acceptance": "evals/misnamed/fixture/accept.md"}`},
		"no-tier":            {"eval.json": `{"name": "no-tier", "summary": "s", "acceptance": "evals/no-tier/fixture/accept.md"}`},
		"acceptance-outside": {"eval.json": `{"name": "acceptance-outside", "summary": "s", "tier": "trivial", "acceptance": "accept.md"}`},
		"acceptance-missing": {"eval.json": `{"name": "acceptance-missing", "summary": "s", "tier": "trivial", "acceptance": "evals/acceptance-missing/fixture/nope.md"}`},
		"no-solution":        {"solution/greet.sh": ""},
	} {
		plant(t, repo, name, "evals: "+name+" (#9)", files)
		_, err := eval.Load(repo)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: a malformed definition refuses by name: %v", name, err)
		}
		if err := os.RemoveAll(filepath.Join(repo, eval.Root, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnchorReadsTheReviewedRevision(t *testing.T) {
	repo, commit := evalRepo(t)
	defs, _ := eval.Load(repo)
	def := defs[0]
	a, err := eval.AnchorOf(repo, def)
	if err != nil || a.Commit != commit || a.PR != "pr/1" {
		t.Fatalf("the anchor is the last commit touching the definition and its merged PR: %+v %v", a, err)
	}
	if a.Ref(def) != def.Acceptance+" @ "+commit || a.Gate() != "pr/1 @ "+commit {
		t.Fatalf("ref and gate name the same commit: %s / %s", a.Ref(def), a.Gate())
	}
	// A later commit elsewhere does not move the anchor.
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "--quiet", "-am", "docs: unrelated (#2)")
	if a2, err := eval.AnchorOf(repo, def); err != nil || a2 != a {
		t.Fatalf("an unrelated commit leaves the anchor where it was: %+v %v", a2, err)
	}
	// A commit touching the definition with no PR number is not a
	// reviewed revision; the refusal names the definition.
	plant(t, repo, "unreviewed", "evals: unreviewed, no pull request", nil)
	if _, err := eval.AnchorOf(repo, eval.Definition{Name: "unreviewed", Acceptance: "evals/unreviewed/fixture/accept.md"}); !errors.Is(err, eval.ErrNotReviewed) || !strings.Contains(err.Error(), "no merged pull request number") {
		t.Fatalf("no PR number: %v", err)
	}
	// Dirt under the definition refuses too.
	if err := os.WriteFile(filepath.Join(repo, def.Dir(), "fixture", "NOTES"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.AnchorOf(repo, def); !errors.Is(err, eval.ErrNotReviewed) || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("a dirty definition is not at a reviewed revision: %v", err)
	}
}

func TestCheckProvesTheKnownVerdict(t *testing.T) {
	repo, commit := evalRepo(t)
	defs, _ := eval.Load(repo)
	def := defs[0]
	anchor := eval.Anchor{Commit: commit, PR: "pr/1"}
	runner := verdict.Runner{Timeout: time.Minute}
	rep, err := eval.Check(repo, def, anchor, runner)
	if err != nil {
		t.Fatalf("the shipped eval checks clean: %v (%+v)", err, rep)
	}
	if len(rep.Commands) != 1 || len(rep.Fixture) != 1 || rep.Fixture[0].Exit == 0 || len(rep.Solution) != 1 || rep.Solution[0].Exit != 0 {
		t.Fatalf("the fixture is red and the solution green through the runner: %+v", rep)
	}
	if wts := git(t, repo, "worktree", "list"); strings.Count(wts, "\n") != 0 {
		t.Fatalf("the check leaves no worktree behind: %s", wts)
	}
	for name, c := range map[string]struct {
		files map[string]string
		want  error
	}{
		"vacuous":    {map[string]string{"fixture/greet.sh": "#!/bin/sh\nprintf 'hello, world\\n'\n"}, eval.ErrVacuous},
		"stays-red":  {map[string]string{"solution/greet.sh": "#!/bin/sh\nprintf 'hello, wrold\\n'\n"}, eval.ErrSolutionRed},
		"unrunnable": {map[string]string{"fixture/accept.md": "# nothing\n\nprose only\n"}, &verdict.SpecUnrunnableError{}},
	} {
		plant(t, repo, name, "evals: "+name+" (#3)", c.files)
		defs, err := eval.Load(repo)
		if err != nil {
			t.Fatal(err)
		}
		def, _ := eval.Find(defs, name)
		a, err := eval.AnchorOf(repo, def)
		if err != nil {
			t.Fatal(err)
		}
		_, err = eval.Check(repo, def, a, runner)
		var unrunnable *verdict.SpecUnrunnableError
		switch {
		case name == "unrunnable" && !errors.As(err, &unrunnable):
			t.Fatalf("%s: a spec with no commands refuses spec_unrunnable: %v", name, err)
		case name != "unrunnable" && !errors.Is(err, c.want):
			t.Fatalf("%s: want %v, got %v", name, c.want, err)
		}
	}
}

func TestFileDerivesAStableId(t *testing.T) {
	def := eval.Definition{Name: "fix-the-check", Summary: "the check is green", Tier: "trivial", Acceptance: "evals/fix-the-check/fixture/accept.md"}
	anchor := eval.Anchor{Commit: strings.Repeat("a", 40), PR: "pr/7"}
	tu := baseTuple(t)
	first, err := eval.File(def, anchor, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := eval.File(def, anchor, nil, 0)
	if first.Subject != again.Subject || !strings.HasPrefix(first.Subject, "eval-") || len(first.Subject) != len("eval-")+12 {
		t.Fatalf("the id is a stable function of eval, tuple and prior count: %s / %s", first.Subject, again.Subject)
	}
	next, _ := eval.File(def, anchor, nil, 1)
	spot, _ := eval.File(def, anchor, &tu, 0)
	if next.Subject == first.Subject || spot.Subject == first.Subject || spot.Subject == next.Subject {
		t.Fatalf("the prior count and the tuple each change the id: %s %s %s", first.Subject, next.Subject, spot.Subject)
	}
	var intent map[string]any
	if err := json.Unmarshal(spot.Intent, &intent); err != nil {
		t.Fatal(err)
	}
	marker, _ := intent["eval"].(map[string]any)
	if intent["tier"] != "trivial" || intent["routing"] != "core" || marker["name"] != "fix-the-check" || marker["tuple"].(map[string]any)["model"] != "fable/5.1" {
		t.Fatalf("the intent carries the tier and the eval marker with the configuration under re-test: %s", spot.Intent)
	}
	var spec struct {
		Acceptance struct {
			Ref        string `json:"ref"`
			Executable bool   `json:"executable"`
			Gate       string `json:"gate"`
		} `json:"acceptance"`
	}
	if err := json.Unmarshal(spot.Spec, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Acceptance.Ref != anchor.Ref(def) || spec.Acceptance.Gate != anchor.Gate() || !spec.Acceptance.Executable {
		t.Fatalf("the specification anchors ref and gate at the same commit, executable: %s", spot.Spec)
	}
}

// stand is a seed/3 chain with a root, two claim holders, a supervisor
// and a verifier, appended to on the raw seam: Due reads the fold and
// the keyring, which are what a raw record establishes, and the
// boundary's own judgment is replayed where Due consults it.
type stand struct {
	t       *testing.T
	store   *ledger.Store
	resolve ledger.Resolver
	keys    map[string]ed25519.PrivateKey
	fps     map[string]string
}

func keyOf(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func newStand(t *testing.T) *stand {
	t.Helper()
	s := &stand{t: t, keys: map[string]ed25519.PrivateKey{}, fps: map[string]string{}}
	for name, first := range map[string]byte{"root": 1, "holderA": 2, "holderB": 5, "supervisor": 11, "verifier": 3} {
		s.keys[name] = keyOf(first)
		fp, err := event.Fingerprint(s.keys[name].Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		s.fps[name] = fp
	}
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.store = store
	g, err := genesis.Build(s.keys["root"], nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
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
	s.resolve = func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range s.keys {
			if f, _ := event.Fingerprint(p.Public().(ed25519.PublicKey)); f == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	if _, err := store.Append(g, s.resolve); err != nil {
		t.Fatal(err)
	}
	s.addAt("root", version.Protocol, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, e := range []struct{ name, capability string }{{"holderA", keyring.CapClaim}, {"holderB", keyring.CapClaim}, {"supervisor", keyring.CapSupervise}, {"verifier", keyring.CapVerdict}} {
		pub := s.keys[e.name].Public().(ed25519.PublicKey)
		s.addAt("root", version.Seed1, "2026-09-01T00:00:00Z", keyring.VerbEnrolled, s.fps[e.name], fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), e.name))
		s.addAt("root", version.Seed1, "2026-09-01T00:00:00Z", keyring.VerbGranted, s.fps[e.name], `{"capability": "`+e.capability+`"}`)
	}
	s.addAt("root", version.Seed1, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	s.addAt("root", version.Seed2, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	return s
}

func (s *stand) addAt(who, v, ts, verb, subject, payload string) int {
	s.t.Helper()
	tip, count, err := s.store.Tip()
	if err != nil {
		s.t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{V: v, TS: ts, Actor: s.fps[who], Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip}, s.keys[who])
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.store.Append(rec, s.resolve); err != nil {
		s.t.Fatalf("%s %s: %v", verb, subject, err)
	}
	return count
}

func (s *stand) add(who, verb, subject, payload string) int {
	s.t.Helper()
	return s.addAt(who, version.Seed3, "2026-09-01T01:00:00Z", verb, subject, payload)
}

func (s *stand) ctx() *admit.Context {
	s.t.Helper()
	c, err := admit.ContextAt(s.store)
	if err != nil {
		s.t.Fatal(err)
	}
	return c
}

// file appends an eval's filing and specification and returns its id.
func (s *stand) file(def eval.Definition, anchor eval.Anchor, tu *tuple.Tuple, prior int) string {
	s.t.Helper()
	f, err := eval.File(def, anchor, tu, prior)
	if err != nil {
		s.t.Fatal(err)
	}
	s.add("root", "intent.filed", f.Subject, string(f.Intent))
	s.add("root", "contract.specified", f.Subject, string(f.Spec))
	return f.Subject
}

// judge takes an eval through claim, reservation, the supervisor's
// declared start, submission and the verifier's verdict, returning the
// verdict position.
func (s *stand) judge(subject, holder, verdictOutcome string) int {
	s.t.Helper()
	fence := s.add(holder, "claim.taken", subject, `{}`)
	res := s.add(holder, "budget.reserve", subject, fmt.Sprintf(`{"amount": "10", "fence": "%d"}`, fence))
	s.add("supervisor", "run.started", subject, fmt.Sprintf(`{"fence": "%d", "reservation": "%d", "tuple": %s}`, fence, res, baseTupleJSON))
	sub := s.add(holder, "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["done"], "decisions": [], "base": "abcdef0..abcdef0", "refs": [], "findings": []}}`, fence))
	return s.add("verifier", "verdict.rendered", subject, fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": "L1"}`, verdictOutcome, strings.Repeat("ab", 32), sub))
}

func (s *stand) due(now time.Time, after time.Duration, defs []eval.Definition, repo string) eval.Report {
	s.t.Helper()
	c := s.ctx()
	return eval.Due(eval.Inputs{Ctx: c, Ring: c.Keyring, Now: now, After: after, Evals: defs, Repo: repo})
}

func kinds(acts []eval.Act) string {
	var out []string
	for _, a := range acts {
		out = append(out, a.Kind+":"+a.Subject)
	}
	return strings.Join(out, " ")
}

func noteKinds(notes []eval.Note) string {
	var out []string
	for _, n := range notes {
		out = append(out, n.Kind)
	}
	return strings.Join(out, " ")
}

// conformance: D5 (offers), D2 (nothing minted on an unchecked
// receipt), D4 (tuple-wide disqualification), and the one-verdict-
// one-consequence guard.
func TestDueOffersMintsAndDisqualifies(t *testing.T) {
	repo, commit := evalRepo(t)
	defs, _ := eval.Load(repo)
	anchor := eval.Anchor{Commit: commit, PR: "pr/1"}
	s := newStand(t)
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	e1 := s.file(defs[0], anchor, nil, 0)
	rep := s.due(now, 0, defs, repo)
	if kinds(rep.Acts) != "offer:"+e1 || rep.Acts[0].Lane != eval.LaneSupervise || rep.Acts[0].Verb != "offer.published" {
		t.Fatalf("a ready eval with no live offer owes the supervisor an offer: %+v", rep.Acts)
	}
	var offer struct {
		Eligibility map[string]any `json:"eligibility"`
		Expires     string         `json:"expires"`
	}
	if err := json.Unmarshal([]byte(rep.Acts[0].Payload), &offer); err != nil {
		t.Fatal(err)
	}
	if offer.Expires != now.Add(24*time.Hour).Format(time.RFC3339) || offer.Eligibility["tuples"] != nil {
		t.Fatalf("the offer expires a day past the declared instant and is never tuple-scoped: %s", rep.Acts[0].Payload)
	}
	s.add("supervisor", "offer.published", e1, rep.Acts[0].Payload)
	if rep := s.due(now, 0, defs, repo); len(rep.Acts) != 0 {
		t.Fatalf("with a live offer nothing is owed: %s", kinds(rep.Acts))
	}
	if rep := s.due(now.Add(48*time.Hour), 0, defs, repo); kinds(rep.Acts) != "offer:"+e1 {
		t.Fatalf("an expired offer is owed again at a later instant: %s", kinds(rep.Acts))
	}

	// A pass whose receipt nothing can recompute (no store) mints
	// nothing, and says so.
	passPos := s.judge(e1, "holderA", "pass")
	rep = s.due(now, 0, defs, repo)
	if len(rep.Acts) != 0 || noteKinds(rep.Notes) != "receipt_unchecked" {
		t.Fatalf("nothing is minted on an unchecked receipt: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	// Once the mint stands, nothing is owed and nothing noted.
	s.add("root", keyring.VerbQualified, s.fps["holderA"], fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d"}`, baseTupleJSON, e1, passPos))
	if rep := s.due(now, 0, defs, repo); len(rep.Acts) != 0 || len(rep.Notes) != 0 {
		t.Fatalf("one verdict, one consequence: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}

	// A fail disqualifies EVERY holder of the configuration: holderA
	// (minted) and holderB (granted by hand), each citing the verdict.
	s.add("root", keyring.VerbGranted, s.fps["holderB"], `{"capability": "claim", "tuple": `+baseTupleJSON+`}`)
	e2 := s.file(defs[0], anchor, nil, 1)
	failPos := s.judge(e2, "holderA", "fail")
	rep = s.due(now, 0, defs, repo)
	got := kinds(rep.Acts)
	if got != "disqualify:"+s.fps["holderA"]+" disqualify:"+s.fps["holderB"] && got != "disqualify:"+s.fps["holderB"]+" disqualify:"+s.fps["holderA"] {
		t.Fatalf("a fail disqualifies every holder of the configuration: %s", got)
	}
	var drop map[string]any
	if err := json.Unmarshal([]byte(rep.Acts[0].Payload), &drop); err != nil {
		t.Fatal(err)
	}
	if drop["contract"] != e2 || drop["verdict"] != fmt.Sprintf("%d", failPos) || drop["reason"] == "" || rep.Acts[0].Lane != eval.LaneSupervise {
		t.Fatalf("each disqualification cites the eval and the fail with a reason: %+v", drop)
	}
	s.add("root", keyring.VerbDisqualified, s.fps["holderA"], rep.Acts[0].Payload)
	if rep := s.due(now, 0, defs, repo); kinds(rep.Acts) != "disqualify:"+s.fps["holderB"] {
		t.Fatalf("a performed disqualification is not owed again: %s", kinds(rep.Acts))
	}

	// An eval in name only owes nothing: the marker with a foreign
	// acceptance spec, or naming a definition the repository does not
	// ship, is noted unbound and neither offered, minted nor
	// disqualified from (review finding on the task PR).
	s.add("root", "intent.filed", "e-fake", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check"}}`)
	s.add("root", "contract.specified", "e-fake", `{"acceptance": {"ref": "accept.md @ `+commit+`", "executable": true, "gate": "pr/1 @ `+commit+`"}}`)
	s.add("root", "intent.filed", "e-unknown", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "nope"}}`)
	s.add("root", "contract.specified", "e-unknown", `{"acceptance": {"ref": "evals/nope/fixture/accept.md @ `+commit+`", "executable": true, "gate": "pr/1 @ `+commit+`"}}`)
	s.add("root", keyring.VerbDisqualified, s.fps["holderB"], rep.Acts[0].Payload)
	rep = s.due(now, 0, defs, repo)
	if kinds(rep.Acts) != "" || noteKinds(rep.Notes) != "unbound unbound" {
		t.Fatalf("an eval in name only is noted unbound and owes nothing: %s / %+v", kinds(rep.Acts), rep.Notes)
	}
	s.judge("e-fake", "holderB", "pass")
	if rep := s.due(now, 0, defs, repo); kinds(rep.Acts) != "" || !strings.Contains(fmt.Sprint(rep.Notes), "not the definition's fixture at its reviewed anchor") {
		t.Fatalf("a pass on an eval in name only mints nothing: %s / %+v", kinds(rep.Acts), rep.Notes)
	}

}

// conformance: D5 — spot-checks age from the qualification record's
// own ts against the DECLARED instant: younger than the interval,
// nothing; older, the dispatcher's filing and specification naming the
// configuration; zero disables; an open eval naming the tuple defers;
// a future-dated qualification is due at once and noted; a hand grant
// with no qualification has no eval to re-run.
func TestDueSpotChecksAgeFromTheDeclaredInstant(t *testing.T) {
	repo, commit := evalRepo(t)
	defs, _ := eval.Load(repo)
	anchor := eval.Anchor{Commit: commit, PR: "pr/1"}
	s := newStand(t)
	tu := baseTuple(t)

	e1 := s.file(defs[0], anchor, nil, 0)
	passPos := s.judge(e1, "holderA", "pass")
	minted := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s.addAt("root", version.Seed3, minted.Format(time.RFC3339), keyring.VerbQualified, s.fps["holderA"],
		fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d"}`, baseTupleJSON, e1, passPos))
	s.add("root", keyring.VerbGranted, s.fps["holderB"], `{"capability": "claim", "tuple": `+baseTupleJSON+`}`)
	day := 24 * time.Hour

	if rep := s.due(minted.Add(23*time.Hour), day, defs, repo); len(rep.Acts) != 0 || len(rep.Notes) != 0 {
		t.Fatalf("younger than the interval nothing is due: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	rep := s.due(minted.Add(25*time.Hour), day, defs, repo)
	if len(rep.Acts) != 2 || rep.Acts[0].Kind != eval.KindSpotCheck || rep.Acts[0].Verb != "intent.filed" || rep.Acts[1].Verb != "contract.specified" || rep.Acts[0].Lane != eval.LaneDispatch || rep.Acts[0].Subject != rep.Acts[1].Subject {
		t.Fatalf("older, the dispatcher owes one spot-check filing and specification: %+v", rep.Acts)
	}
	var intent struct {
		Eval struct {
			Name  string      `json:"name"`
			Tuple tuple.Tuple `json:"tuple"`
		} `json:"eval"`
	}
	if err := json.Unmarshal([]byte(rep.Acts[0].Payload), &intent); err != nil {
		t.Fatal(err)
	}
	if intent.Eval.Name != "fix-the-check" || !intent.Eval.Tuple.Equal(tu) {
		t.Fatalf("the spot-check names the eval and the configuration under re-test: %s", rep.Acts[0].Payload)
	}
	want, _ := eval.File(defs[0], anchor, &tu, 0)
	if rep.Acts[0].Subject != want.Subject {
		t.Fatalf("the spot-check's id is File's for (eval, tuple, prior 0): %s vs %s", rep.Acts[0].Subject, want.Subject)
	}
	if rep := s.due(minted.Add(25*time.Hour), 0, defs, repo); len(rep.Acts) != 0 {
		t.Fatalf("zero disables spot-checks: %s", kinds(rep.Acts))
	}
	// A qualification dated after the declared instant is due at once,
	// and the lie about time is noted.
	rep = s.due(minted.Add(-time.Hour), day, defs, repo)
	if len(rep.Acts) != 2 || !strings.Contains(fmt.Sprint(rep.Notes), "cannot postpone") {
		t.Fatalf("a future-dated qualification is due now and noted: %+v / %+v", rep.Acts, rep.Notes)
	}
	// With the spot-check filed and open, nothing more is owed for the
	// configuration; judged and passed, the new qualification's ts is
	// what the next window measures from.
	spot := s.file(defs[0], anchor, &tu, 0)
	if spot != want.Subject {
		t.Fatalf("Prior counts the evals already filed for the pair: %s vs %s", spot, want.Subject)
	}
	if c := s.ctx(); eval.Prior(c.Lifecycle, "fix-the-check", &tu) != 1 || eval.Prior(c.Lifecycle, "fix-the-check", nil) != 1 {
		t.Fatalf("Prior counts per (eval, tuple) pair: %d / %d", eval.Prior(c.Lifecycle, "fix-the-check", &tu), eval.Prior(c.Lifecycle, "fix-the-check", nil))
	}
	rep = s.due(minted.Add(25*time.Hour), day, defs, repo)
	if kinds(rep.Acts) != "offer:"+spot {
		t.Fatalf("with a spot-check open only its offer is owed: %s", kinds(rep.Acts))
	}
	spotPass := s.judge(spot, "holderA", "pass")
	later := minted.Add(30 * time.Hour)
	s.addAt("root", version.Seed3, later.Format(time.RFC3339), keyring.VerbQualified, s.fps["holderA"],
		fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d"}`, baseTupleJSON, spot, spotPass))
	if rep := s.due(later.Add(23*time.Hour), day, defs, repo); len(rep.Acts) != 0 {
		t.Fatalf("the satisfied window advances to the new ts: %s", kinds(rep.Acts))
	}
	rep = s.due(later.Add(25*time.Hour), day, defs, repo)
	if len(rep.Acts) != 2 || rep.Acts[0].Subject == spot {
		t.Fatalf("past the new window a fresh spot-check (prior 1) is owed: %+v", rep.Acts)
	}
	// A definition the repository no longer ships is noted, not filed.
	if rep := s.due(later.Add(25*time.Hour), day, nil, repo); len(rep.Acts) != 0 || !strings.Contains(noteKinds(rep.Notes), "no_definition") {
		t.Fatalf("a missing definition is a note, and every eval naming it is unbound: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
}

// conformance: plans/os-c7554f18.md D2 — the offers Due owes carry a
// tuples scope by policy: a first eval on an empty ranking goes out
// unscoped and the report says so; once a configuration is qualified
// the first eval's offer carries the strongest claim tuple; a re-test
// names the configuration under test and no other.
func TestDueOffersCarryTheStrongestByPolicy(t *testing.T) {
	repo, commit := evalRepo(t)
	defs, _ := eval.Load(repo)
	anchor := eval.Anchor{Commit: commit, PR: "pr/1"}
	s := newStand(t)
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	type offer struct {
		Eligibility struct {
			Tuples []tuple.Tuple `json:"tuples"`
		} `json:"eligibility"`
	}
	decode := func(act eval.Act) offer {
		t.Helper()
		var o offer
		if err := json.Unmarshal([]byte(act.Payload), &o); err != nil {
			t.Fatal(err)
		}
		return o
	}

	e1 := s.file(defs[0], anchor, nil, 0)
	rep := s.due(now, 0, defs, repo)
	if kinds(rep.Acts) != "offer:"+e1 || len(decode(rep.Acts[0]).Eligibility.Tuples) != 0 || noteKinds(rep.Notes) != "ranking_empty" {
		t.Fatalf("an empty ranking leaves the first eval's offer unscoped and notes it: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}

	// holderA's configuration qualified by hand-appended fact: the
	// ranking has a top, and the next first eval's offer carries it.
	s.add("root", keyring.VerbQualified, s.fps["holderA"], fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "3"}`, baseTupleJSON, e1))
	s.add("root", keyring.VerbGranted, s.fps["holderA"], `{"capability": "claim", "tuple": `+baseTupleJSON+`}`)
	rep = s.due(now, 0, defs, repo)
	o := decode(rep.Acts[0])
	if kinds(rep.Acts) != "offer:"+e1 || len(o.Eligibility.Tuples) != 1 || !o.Eligibility.Tuples[0].Equal(baseTuple(t)) || len(rep.Notes) != 0 || !strings.Contains(rep.Acts[0].Because, "strongest") {
		t.Fatalf("the offer carries the top of the claim ranking by policy: %+v / %+v", rep.Acts, rep.Notes)
	}

	// A re-test names the configuration it was filed for, whatever
	// the ranking says.
	other := baseTuple(t)
	other.Environment = "other-environment"
	e2 := s.file(defs[0], anchor, &other, 1)
	rep = s.due(now, 0, defs, repo)
	var named []tuple.Tuple
	for _, act := range rep.Acts {
		if act.Subject == e2 {
			named = decode(act).Eligibility.Tuples
			if !strings.Contains(act.Because, "under re-test") {
				t.Fatalf("a re-test's offer says it names the configuration under test: %s", act.Because)
			}
		}
	}
	if len(named) != 1 || !named[0].Equal(other) {
		t.Fatalf("a re-test's offer names its own configuration: %+v", named)
	}
}
