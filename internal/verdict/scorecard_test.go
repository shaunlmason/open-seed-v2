package verdict

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/plan"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func scoreRepo(t *testing.T) (repo, commit string) {
	t.Helper()
	repo = t.TempDir()
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
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "the work")
	return repo, git("rev-parse", "HEAD")
}

func good(commit string) *Scorecard {
	return &Scorecard{Contract: "c-1", Submission: "12", Items: []ScoreItem{
		{ID: "tone", Score: "pass", Evidence: []string{"main.go @ " + commit + "#L1-L3"}, Uncertainty: "low", Note: "reads as the operator's"},
		{ID: "taste", Score: "pass", Evidence: []string{"transcript:0"}, Uncertainty: "low"},
	}}
}

// conformance: AC2 — the scorecard's validation names the part; the
// payload half carries the digest and the two enums, nothing else.
func TestScorecardValidationNamesThePart(t *testing.T) {
	repo, commit := scoreRepo(t)
	rubric := []plan.Item{{ID: "tone", Criterion: "a"}, {ID: "taste", Criterion: "b"}}
	receipt := &Receipt{Transcripts: []Transcript{{Cmd: "go test", Exit: 0}}}
	if err := Validate(good(commit), "c-1", 12, rubric, receipt, repo, nil); err != nil {
		t.Fatalf("the agreeing scorecard validates: %v", err)
	}
	bend := func(f func(s *Scorecard)) *Scorecard {
		s := good(commit)
		f(s)
		return s
	}
	for _, row := range []struct {
		name  string
		s     *Scorecard
		part  string
		names string
	}{
		{"another contract", bend(func(s *Scorecard) { s.Contract = "c-2" }), "contract", "c-2"},
		{"another submission", bend(func(s *Scorecard) { s.Submission = "13" }), "submission", "13"},
		{"a missing item", bend(func(s *Scorecard) { s.Items = s.Items[:1] }), "items", "taste"},
		{"an unknown item", bend(func(s *Scorecard) {
			s.Items = append(s.Items, ScoreItem{ID: "speed", Score: "pass", Evidence: []string{"transcript:0"}, Uncertainty: "low"})
		}), "items", "speed"},
		{"an item twice", bend(func(s *Scorecard) { s.Items = append(s.Items, s.Items[0]) }), "items", "twice"},
		{"a score outside the vocabulary", bend(func(s *Scorecard) { s.Items[0].Score = "ok" }), "items", "vocabulary"},
		{"an uncertainty outside the vocabulary", bend(func(s *Scorecard) { s.Items[0].Uncertainty = "medium" }), "items", "two values"},
		{"no evidence", bend(func(s *Scorecard) { s.Items[0].Evidence = nil }), "evidence", "cites no evidence"},
		{"prose as evidence", bend(func(s *Scorecard) { s.Items[0].Evidence = []string{"it just reads well"} }), "evidence", "neither"},
		{"a transcript the receipt lacks", bend(func(s *Scorecard) { s.Items[1].Evidence = []string{"transcript:1"} }), "evidence", "no transcript"},
		{"a path absent at its commit", bend(func(s *Scorecard) { s.Items[0].Evidence = []string{"missing.go @ " + commit} }), "evidence", "does not resolve"},
		{"an empty line range", bend(func(s *Scorecard) { s.Items[0].Evidence = []string{"main.go @ " + commit + "#L3-L1"} }), "evidence", "empty line range"},
		{"a note over the budget", bend(func(s *Scorecard) { s.Items[0].Note = strings.Repeat("x", NoteBudget+1) }), "note", "budget"},
	} {
		err := Validate(row.s, "c-1", 12, rubric, receipt, repo, nil)
		var se *ScorecardError
		if !errors.As(err, &se) || se.Part != row.part || !strings.Contains(err.Error(), row.names) {
			t.Fatalf("%s refuses at %s naming %q: %v", row.name, row.part, row.names, err)
		}
	}
	// Without a repository the anchored path is judged by grammar
	// alone; without a receipt no transcript resolves.
	if err := Validate(good(commit), "c-1", 12, rubric, receipt, "", nil); err != nil {
		t.Fatalf("no repository, grammar alone: %v", err)
	}
	if err := Validate(good(commit), "c-1", 12, rubric, nil, repo, nil); err == nil {
		t.Fatal("no receipt, no transcript to cite")
	}
	ref, err := good(commit).Ref()
	if err != nil || len(ref.Items) != 2 || ref.Items[0].ID != "tone" || ref.Items[0].Score != "pass" || ref.Items[0].Uncertainty != "low" || len(ref.Digest) != 64 {
		t.Fatalf("the payload half is the digest and the two enums: %+v %v", ref, err)
	}
	digest, _ := good(commit).Digest()
	if digest != ref.Digest {
		t.Fatal("the ref's digest is the artifact's")
	}
	if _, err := ParseScorecard([]byte(`{"contract": "c-1", "submission": "12", "items": [], "verdict": "pass"}`)); err == nil {
		t.Fatal("a smuggled holistic verdict refuses at the shape")
	}
}

// conformance: AC3 — the derivation over the payload's items.
func TestDerivationOverTheItems(t *testing.T) {
	low := func(id, score string) transition.ScoreItem {
		return transition.ScoreItem{ID: id, Score: score, Uncertainty: "low"}
	}
	for _, row := range []struct {
		name                string
		items               []transition.ScoreItem
		verdict, code, item string
	}{
		{"all pass at low", []transition.ScoreItem{low("a", "pass"), low("b", "pass")}, "pass", "", ""},
		{"a fail forbids pass", []transition.ScoreItem{low("a", "pass"), low("b", "fail")}, "fail", transition.CodeRubricRed, "b"},
		{"a high forbids both", []transition.ScoreItem{low("a", "pass"), {ID: "b", Score: "pass", Uncertainty: "high"}}, "", transition.CodeHumanVerdict, "b"},
		{"a high outranks a fail", []transition.ScoreItem{{ID: "a", Score: "fail", Uncertainty: "high"}, low("b", "fail")}, "", transition.CodeHumanVerdict, "a"},
		{"no items permit pass", nil, "pass", "", ""},
	} {
		v, code, item := transition.DeriveScores(row.items)
		if v != row.verdict || code != row.code || item != row.item {
			t.Fatalf("%s: got (%q, %q, %q), want (%q, %q, %q)", row.name, v, code, item, row.verdict, row.code, row.item)
		}
	}
}
