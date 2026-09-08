package eval_test

// Calibration (plans/os-2e34f66a.md D5): a definition committing to a
// gold scorecard held outside the tree, the floor pinned to the spec,
// agreement item by item, and what the chain owes when a verifier
// agrees, drifts, or is re-tested.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const goldJSON = `{"items": [{"id": "tone", "score": "pass"}, {"id": "taste", "score": "fail"}, {"id": "clarity", "score": "pass"}, {"id": "brevity", "score": "pass"}, {"id": "care", "score": "pass"}]}`

// calibrationJSON is a calibration definition committing to the gold.
func calibrationJSON(t *testing.T, name string, extra string) string {
	t.Helper()
	g, err := eval.ParseGold([]byte(goldJSON))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"name": %q, "summary": "planted", "tier": "trivial", "acceptance": "evals/%s/fixture/accept.md", "kind": "calibration", "calibration": {"gold": "sha256:%s"%s}}`, name, name, g.Digest, extra)
}

const rubricAccept = "# planted\n\n## Validation commands\n\n- `sh evals/%s/fixture/check.sh`\n\n## Rubric\n\n- tone: reads as the operator's\n- taste: the abstraction carries its weight\n- clarity: says one thing\n- brevity: says it once\n- care: handles the edge\n"

// plantCalibration plants a calibration definition at a reviewed
// revision, its gold held in a directory OUTSIDE the repository.
func plantCalibration(t *testing.T, repo, name, pr string) (goldDir string) {
	t.Helper()
	plant(t, repo, name, "evals: "+name+" (#"+pr+")", map[string]string{
		"eval.json":         calibrationJSON(t, name, ""),
		"fixture/accept.md": fmt.Sprintf(rubricAccept, name),
	})
	goldDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(goldDir, name+".json"), []byte(goldJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return goldDir
}

// conformance: AC6 — Load's rows, the floor's pin, the gold's supply.
func TestCalibrationDefinitionsLoadAndTheFloorIsTheSpecs(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "spec", "evals.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("the floor is\\s+`([0-9.]+)`").FindSubmatch(spec)
	if m == nil {
		t.Fatal("evals.md states the floor as \"the floor is `<figure>`\"")
	}
	if figure, _ := strconv.ParseFloat(string(m[1]), 64); figure != eval.CalibrationFloor {
		t.Fatalf("eval.CalibrationFloor %g mirrors evals.md's %s", eval.CalibrationFloor, m[1])
	}
	repo, _ := evalRepo(t)
	goldDir := plantCalibration(t, repo, "calib", "2")
	defs, err := eval.Load(repo)
	if err != nil {
		t.Fatalf("a committed calibration definition loads: %v", err)
	}
	def, ok := eval.Find(defs, "calib")
	if !ok || !def.IsCalibration() || def.Floor() != eval.CalibrationFloor {
		t.Fatalf("the definition is a calibration at the spec's floor: %+v", def)
	}
	gold, err := eval.LoadGold(goldDir, defs)
	if err != nil || len(gold["calib"].Items) != 5 || "sha256:"+gold["calib"].Digest != def.Calibration.Gold {
		t.Fatalf("the gold loads from outside the tree and matches the commitment: %+v %v", gold, err)
	}
	if none, err := eval.LoadGold(t.TempDir(), defs); err != nil || len(none) != 0 {
		t.Fatalf("a directory without the gold supplies nothing: %+v %v", none, err)
	}
	if err := os.WriteFile(filepath.Join(goldDir, "calib.json"), []byte(`{"items": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.LoadGold(goldDir, defs); err == nil || !strings.Contains(err.Error(), "at least one item") {
		t.Fatalf("a malformed gold refuses by name: %v", err)
	}
	for _, row := range []struct{ name, raw, names string }{
		{"no commitment", `{"items": [{"id": "a", "score": "pass"}], "verdict": "pass"}`, "strict object"},
		{"an empty id", `{"items": [{"id": "", "score": "pass"}]}`, "unique and non-empty"},
		{"a score outside the vocabulary", `{"items": [{"id": "a", "score": "ok"}]}`, "pass or fail"},
	} {
		if _, err := eval.ParseGold([]byte(row.raw)); err == nil || !strings.Contains(err.Error(), row.names) {
			t.Fatalf("%s refuses naming the part: %v", row.name, err)
		}
	}
	for _, row := range []struct{ name, evalJSON, names string }{
		{"a calibration with no commitment", `{"name": "bent", "summary": "planted", "tier": "trivial", "acceptance": "evals/bent/fixture/accept.md", "kind": "calibration"}`, "commits to its gold"},
		{"a floor below the spec's", calibrationJSON(t, "bent", `, "floor": 0.5`), "below the spec's"},
		{"a floor above one", calibrationJSON(t, "bent", `, "floor": 1.5`), "not a fraction"},
		{"an ordinary definition with a commitment", strings.Replace(calibrationJSON(t, "bent", ""), `"kind": "calibration", `, "", 1), "whose kind is not"},
		{"an unknown kind", strings.Replace(calibrationJSON(t, "bent", ""), `"kind": "calibration"`, `"kind": "audit"`, 1), "neither empty nor"},
	} {
		plant(t, repo, "bent", "evals: bent (#3)", map[string]string{"eval.json": row.evalJSON, "fixture/accept.md": fmt.Sprintf(rubricAccept, "bent")})
		if _, err := eval.Load(repo); err == nil || !strings.Contains(err.Error(), row.names) {
			t.Fatalf("%s refuses at Load naming the part: %v", row.name, err)
		}
	}
	plant(t, repo, "bent", "evals: bent raised (#4)", map[string]string{"eval.json": calibrationJSON(t, "bent", `, "floor": 0.9`), "fixture/accept.md": fmt.Sprintf(rubricAccept, "bent")})
	defs, err = eval.Load(repo)
	if err != nil {
		t.Fatalf("a raised floor loads: %v", err)
	}
	if raised, _ := eval.Find(defs, "bent"); raised.Floor() != 0.9 {
		t.Fatalf("a definition may raise the floor: %g", raised.Floor())
	}
	if list, _ := eval.Load(filepath.Join("..", "..")); func() bool {
		for _, d := range list {
			if d.IsCalibration() {
				return true
			}
		}
		return false
	}() {
		t.Fatal("no calibration definition is shipped: its gold would be either in the tree or lost")
	}
}

// conformance: AC6 — agreement counts the same score at low
// uncertainty; high is not agreement.
func TestAgreementCountsLowConfidenceMatches(t *testing.T) {
	g, _ := eval.ParseGold([]byte(goldJSON))
	scored := []transition.ScoreItem{
		{ID: "tone", Score: "pass", Uncertainty: "low"},
		{ID: "taste", Score: "fail", Uncertainty: "low"},
		{ID: "clarity", Score: "pass", Uncertainty: "high"},
		{ID: "brevity", Score: "fail", Uncertainty: "low"},
	}
	agreement, disagreeing := eval.Agreement(scored, g.Items)
	if agreement != 0.4 || strings.Join(disagreeing, ",") != "clarity,brevity,care" {
		t.Fatalf("two of five agree at low: %g %v", agreement, disagreeing)
	}
	if a, d := eval.Agreement(nil, nil); a != 0 || d != nil {
		t.Fatalf("no gold, no agreement: %g %v", a, d)
	}
}

// calibrationStand is a seed/4 stand whose verifier renders with a
// declared tuple and a scorecard.
type calibrationStand struct {
	*stand
}

func newCalibrationStand(t *testing.T) *calibrationStand {
	t.Helper()
	s := newStand(t)
	s.addAt("root", version.Seed3, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	return &calibrationStand{s}
}

func (s *calibrationStand) add4(who, verb, subject, payload string) int {
	s.t.Helper()
	return s.addAt(who, version.Seed4, "2026-09-01T01:00:00Z", verb, subject, payload)
}

// file4 is the stand's file at seed/4.
func (s *calibrationStand) file4(def eval.Definition, anchor eval.Anchor, prior int) string {
	s.t.Helper()
	f, err := eval.File(def, anchor, nil, prior)
	if err != nil {
		s.t.Fatal(err)
	}
	s.add4("root", "intent.filed", f.Subject, string(f.Intent))
	s.add4("root", "contract.specified", f.Subject, string(f.Spec))
	return f.Subject
}

// score renders the verifier's verdict on the eval with the declared
// tuple and the scorecard items given (score per gold item, all at
// low uncertainty unless marked "high").
func (s *calibrationStand) score(subject, holder, tup string, scores map[string]string) int {
	s.t.Helper()
	fence := s.add4(holder, "claim.taken", subject, `{}`)
	res := s.add4(holder, "budget.reserve", subject, fmt.Sprintf(`{"amount": "10", "fence": "%d"}`, fence))
	s.add4("supervisor", "run.started", subject, fmt.Sprintf(`{"fence": "%d", "reservation": "%d", "tuple": %s}`, fence, res, baseTupleJSON))
	sub := s.add4(holder, "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["done"], "decisions": [], "base": "abcdef0..abcdef0", "refs": [], "findings": []}}`, fence))
	var items []string
	verdict := "pass"
	for _, id := range []string{"tone", "taste", "clarity", "brevity", "care"} {
		score, unc := scores[id], "low"
		if strings.HasSuffix(score, "/high") {
			score, unc = strings.TrimSuffix(score, "/high"), "high"
		}
		if score == "fail" {
			verdict = "fail"
		}
		items = append(items, fmt.Sprintf(`{"id": %q, "score": %q, "uncertainty": %q}`, id, score, unc))
	}
	return s.add4("verifier", "verdict.rendered", subject, fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": "L3", "tuple": %s, "scorecard": {"digest": %q, "items": [%s]}}`,
		verdict, strings.Repeat("ab", 32), sub, tup, strings.Repeat("cd", 32), strings.Join(items, ", ")))
}

func (s *calibrationStand) due4(now time.Time, after time.Duration, defs []eval.Definition, repo string, gold map[string]eval.Gold) eval.Report {
	s.t.Helper()
	c := s.ctx()
	return eval.Due(eval.Inputs{Ctx: c, Ring: c.Keyring, Now: now, After: after, Evals: defs, Repo: repo, Gold: gold})
}

// conformance: AC6 — what a calibration owes: nothing with no gold
// (gold_missing) or a gold that is not the commitment (gold_mismatch);
// the verdict mint for the verifier's declared tuple at or above the
// floor; below it the tuple-wide disqualification and the dispatcher's
// defect filing naming the disagreeing items, once; a spot-check on
// the verdict qualification at the interval.
func TestCalibrationOwesMintsDriftAndTheDefect(t *testing.T) {
	repo, _ := evalRepo(t)
	goldDir := plantCalibration(t, repo, "calib", "2")
	defs, _ := eval.Load(repo)
	def, _ := eval.Find(defs, "calib")
	anchor, err := eval.AnchorOf(repo, def)
	if err != nil {
		t.Fatal(err)
	}
	gold, _ := eval.LoadGold(goldDir, defs)
	s := newCalibrationStand(t)
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	verifierTuple := `{"principal": "acme", "harness": "local-worktree/v0", "model": "other/1", "tool_policy": "default", "environment": "detached-git-worktree"}`

	e1 := s.file4(def, anchor, 0)
	agreeing := map[string]string{"tone": "pass", "taste": "fail", "clarity": "pass", "brevity": "pass", "care": "pass"}
	verdictPos := s.score(e1, "holderA", verifierTuple, agreeing)
	if rep := s.due4(now, 0, defs, repo, nil); len(rep.Acts) != 0 || noteKinds(rep.Notes) != "gold_missing" {
		t.Fatalf("with no gold nothing is owed and the note says so: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	wrong, _ := eval.ParseGold([]byte(`{"items": [{"id": "tone", "score": "fail"}]}`))
	if rep := s.due4(now, 0, defs, repo, map[string]eval.Gold{"calib": wrong}); len(rep.Acts) != 0 || noteKinds(rep.Notes) != "gold_mismatch" {
		t.Fatalf("a gold that is not the commitment scores nothing: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	rep := s.due4(now, 0, defs, repo, gold)
	if kinds(rep.Acts) != "mint:"+s.fps["verifier"] || rep.Acts[0].Verb != keyring.VerbQualified || rep.Acts[0].Lane != eval.LaneSupervise {
		t.Fatalf("full agreement owes the verifier's verdict mint: %s / %+v", kinds(rep.Acts), rep.Notes)
	}
	var mint map[string]any
	if err := json.Unmarshal([]byte(rep.Acts[0].Payload), &mint); err != nil {
		t.Fatal(err)
	}
	if mint["capability"] != keyring.CapVerdict || mint["contract"] != e1 || mint["verdict"] != fmt.Sprint(verdictPos) || mint["tuple"].(map[string]any)["model"] != "other/1" {
		t.Fatalf("the mint is for verdict, the eval, its verdict and the declared tuple: %+v", mint)
	}
	s.add4("supervisor", keyring.VerbQualified, s.fps["verifier"], rep.Acts[0].Payload)
	if rep := s.due4(now, 0, defs, repo, gold); len(rep.Acts) != 0 {
		t.Fatalf("one verdict, one consequence: %s", kinds(rep.Acts))
	}
	// Four of five at low is 0.8, the floor: still qualified (a high
	// item could not have rendered at all: the verdict would refuse
	// human_verdict, so the one disagreement is a confident one).
	// Three of five is drift.
	e2 := s.file4(def, anchor, 1)
	s.score(e2, "holderA", verifierTuple, map[string]string{"tone": "pass", "taste": "fail", "clarity": "pass", "brevity": "pass", "care": "fail"})
	rep = s.due4(now, 0, defs, repo, gold)
	if kinds(rep.Acts) != "mint:"+s.fps["verifier"] {
		t.Fatalf("agreement at the floor qualifies: %s / %+v", kinds(rep.Acts), rep.Notes)
	}
	s.add4("supervisor", keyring.VerbQualified, s.fps["verifier"], rep.Acts[0].Payload)
	// A second verifier granted the same configuration by hand: drift
	// is tuple-wide, so the disqualification names it too.
	otherKey := keyOf(7)
	otherPub := otherKey.Public().(ed25519.PublicKey)
	otherFP, _ := event.Fingerprint(otherPub)
	s.keys["verifierB"], s.fps["verifierB"] = otherKey, otherFP
	s.add4("root", keyring.VerbEnrolled, otherFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifierB"}`, hex.EncodeToString(otherPub)))
	s.add4("root", keyring.VerbGranted, otherFP, `{"capability": "verdict", "tuple": `+verifierTuple+`}`)
	e3 := s.file4(def, anchor, 2)
	driftPos := s.score(e3, "holderA", verifierTuple, map[string]string{"tone": "pass", "taste": "pass", "clarity": "pass", "brevity": "fail", "care": "pass"})
	rep = s.due4(now, 0, defs, repo, gold)
	got := kinds(rep.Acts)
	want1 := "disqualify:" + s.fps["verifier"] + " disqualify:" + otherFP + " defect:" + eval.DriftDefectID(e3)
	want2 := "disqualify:" + otherFP + " disqualify:" + s.fps["verifier"] + " defect:" + eval.DriftDefectID(e3)
	if got != want1 && got != want2 {
		t.Fatalf("drift owes the tuple-wide disqualification (every verifier holding the configuration) and the defect filing: %s", got)
	}
	if rep.Acts[0].Subject != s.fps["verifier"] {
		rep.Acts[0], rep.Acts[1] = rep.Acts[1], rep.Acts[0]
	}
	var drop map[string]any
	if err := json.Unmarshal([]byte(rep.Acts[0].Payload), &drop); err != nil {
		t.Fatal(err)
	}
	if drop["capability"] != keyring.CapVerdict || drop["verdict"] != fmt.Sprint(driftPos) || !strings.Contains(drop["reason"].(string), "taste, brevity") || rep.Acts[0].Lane != eval.LaneSupervise {
		t.Fatalf("the disqualification cites the fail and names the disagreeing items: %+v", drop)
	}
	if rep.Acts[2].Verb != "intent.filed" || rep.Acts[2].Lane != eval.LaneDispatch || !strings.Contains(rep.Acts[2].Payload, "taste, brevity") || !strings.Contains(rep.Acts[2].Payload, e3) {
		t.Fatalf("the defect filing is the dispatcher's, naming the contract and the items: %+v", rep.Acts[2])
	}
	s.add4("supervisor", keyring.VerbDisqualified, s.fps["verifier"], rep.Acts[0].Payload)
	s.add4("supervisor", keyring.VerbDisqualified, otherFP, rep.Acts[1].Payload)
	s.add4("root", "intent.filed", rep.Acts[2].Subject, rep.Acts[2].Payload)
	if rep := s.due4(now, 0, defs, repo, gold); len(rep.Acts) != 0 {
		t.Fatalf("the performed disqualifications and the filed defect are not owed again: %s", kinds(rep.Acts))
	}
	// Re-qualification by the same road, then the spot-check ages it.
	e4 := s.file4(def, anchor, 3)
	s.score(e4, "holderA", verifierTuple, agreeing)
	rep = s.due4(now, 0, defs, repo, gold)
	if kinds(rep.Acts) != "mint:"+s.fps["verifier"] {
		t.Fatalf("a passing calibration re-qualifies: %s", kinds(rep.Acts))
	}
	s.add4("supervisor", keyring.VerbQualified, s.fps["verifier"], rep.Acts[0].Payload)
	later := now.Add(8 * 24 * time.Hour)
	rep = s.due4(later, 7*24*time.Hour, defs, repo, gold)
	if !strings.Contains(kinds(rep.Acts), "spot-check:") || rep.Acts[0].Lane != eval.LaneDispatch || !strings.Contains(rep.Acts[0].Payload, "calib") {
		t.Fatalf("an aged verdict qualification owes a spot-check on the calibration: %s", kinds(rep.Acts))
	}
}

// conformance: review findings on the task PR — a ready calibration
// whose gold the derivation cannot see is not offered (the note is
// what is owed); a calibration filed without the marker's kind owes
// nothing but the kind_unmarked note; and a bare-grant verifier whose
// FIRST calibration drifts is disqualified for the configuration it
// rendered under, closing its bridge, beside the defect.
func TestCalibrationWithholdsOffersAndClosesTheBridgeOnFirstDrift(t *testing.T) {
	repo, _ := evalRepo(t)
	goldDir := plantCalibration(t, repo, "calib", "2")
	defs, _ := eval.Load(repo)
	def, _ := eval.Find(defs, "calib")
	anchor, err := eval.AnchorOf(repo, def)
	if err != nil {
		t.Fatal(err)
	}
	gold, _ := eval.LoadGold(goldDir, defs)
	s := newCalibrationStand(t)
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	verifierTuple := `{"principal": "acme", "harness": "local-worktree/v0", "model": "other/1", "tool_policy": "default", "environment": "detached-git-worktree"}`

	// Ready, no gold: no offer, one note.
	e1 := s.file4(def, anchor, 0)
	if rep := s.due4(now, 0, defs, repo, nil); len(rep.Acts) != 0 || noteKinds(rep.Notes) != "gold_missing" {
		t.Fatalf("a ready calibration without its gold is not offered: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	wrong, _ := eval.ParseGold([]byte(`{"items": [{"id": "tone", "score": "fail"}]}`))
	if rep := s.due4(now, 0, defs, repo, map[string]eval.Gold{"calib": wrong}); len(rep.Acts) != 0 || noteKinds(rep.Notes) != "gold_mismatch" {
		t.Fatalf("a gold that is not the commitment withholds the offer too: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	if rep := s.due4(now, 0, defs, repo, gold); kinds(rep.Acts) != "offer:"+e1 {
		t.Fatalf("with the gold the ready calibration is offered: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
	// The filing carries the mark.
	f, err := eval.File(def, anchor, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(f.Intent), `"kind":"calibration"`) && !strings.Contains(string(f.Intent), `"kind": "calibration"`) {
		t.Fatalf("seed eval file marks a calibration: %s", f.Intent)
	}

	// The first calibration drifts: the verifier holds verdict by a
	// bare grant, nothing cites its tuple, and the disqualification is
	// owed for it all the same, beside the defect.
	driftPos := s.score(e1, "holderA", verifierTuple, map[string]string{"tone": "pass", "taste": "pass", "clarity": "pass", "brevity": "fail", "care": "pass"})
	rep := s.due4(now, 0, defs, repo, gold)
	if got := kinds(rep.Acts); got != "disqualify:"+s.fps["verifier"]+" defect:"+eval.DriftDefectID(e1) {
		t.Fatalf("the first failed calibration disqualifies the verifier that rendered and files the defect: %s / %s", got, noteKinds(rep.Notes))
	}
	var drop map[string]any
	if err := json.Unmarshal([]byte(rep.Acts[0].Payload), &drop); err != nil {
		t.Fatal(err)
	}
	if drop["verdict"] != fmt.Sprint(driftPos) || drop["tuple"].(map[string]any)["model"] != "other/1" {
		t.Fatalf("the disqualification cites the drifted verdict and its tuple: %+v", drop)
	}
	// The keyring admits it and the bridge closes; the dispatcher files
	// the defect; nothing more is owed on that verdict.
	s.add4("supervisor", keyring.VerbDisqualified, s.fps["verifier"], rep.Acts[0].Payload)
	s.add4("root", "intent.filed", rep.Acts[1].Subject, rep.Acts[1].Payload)
	ring := s.ctx().Keyring
	if !ring.EverCited(s.fps["verifier"], keyring.CapVerdict) || len(ring.GrantTuples(s.fps["verifier"], keyring.CapVerdict)) != 0 {
		t.Fatal("after the first failed calibration the verifier's verdict set is empty and once cited")
	}
	if rep := s.due4(now, 0, defs, repo, gold); len(rep.Acts) != 0 {
		t.Fatalf("one verdict, one consequence: %s", kinds(rep.Acts))
	}

	// A calibration filed without the mark owes nothing but the note.
	unmarked, err := eval.File(def, anchor, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	intent := regexp.MustCompile(`,?\s*"kind":\s*"calibration",?`).ReplaceAllString(string(unmarked.Intent), "")
	if strings.Contains(intent, `"kind"`) || !json.Valid([]byte(intent)) {
		t.Fatalf("the drill could not strip the mark: %s", intent)
	}
	s.add4("root", "intent.filed", unmarked.Subject, intent)
	s.add4("root", "contract.specified", unmarked.Subject, string(unmarked.Spec))
	s.score(unmarked.Subject, "holderA", verifierTuple, map[string]string{"tone": "pass", "taste": "fail", "clarity": "pass", "brevity": "pass", "care": "pass"})
	if rep := s.due4(now, 0, defs, repo, gold); len(rep.Acts) != 0 || !strings.Contains(noteKinds(rep.Notes), "kind_unmarked") {
		t.Fatalf("an unmarked calibration owes nothing the boundary would admit, and says so: %s / %s", kinds(rep.Acts), noteKinds(rep.Notes))
	}
}
