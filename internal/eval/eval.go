// Package eval is the eval-contract machinery (plans/os-03e47abb.md;
// spec/evals.md; SEED-NEXT.md §II.16): shipped definitions under
// evals/<name>/, anchored in this repository at their last
// reviewed commit; the filing that turns one into an ordinary contract
// marked as an eval; the check that proves its known verdict (fixture
// red, solution green, through the verifier's own runner); and the
// derivation of what the chain owes at a declared instant: mints for
// passes whose receipts recompute green, disqualifications for fails,
// offers for waiting evals, and spot-checks for qualifications older
// than the policy interval. The derivation performs nothing: seed eval
// act signs what its key's grants admit.
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/plan"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Root is where definitions live, relative to the repository root:
// transition.EvalRoot, which the boundary reads the layout from.
const Root = transition.EvalRoot

// Applies reports whether the qualification verbs and the eval marker
// are defined at a record carrying version v: seed/3 exactly (D8).
func Applies(v string) bool { return version.EvalApplies(v) }

// Definition is one shipped eval: eval.json beside a fixture/ (the
// files the eval is worked in, the acceptance spec among them) and a
// solution/ (the files the reference solution changes).
type Definition struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Tier    string `json:"tier"`
	// Acceptance is the acceptance spec's repository-relative path,
	// under this definition's fixture/.
	Acceptance string `json:"acceptance"`
	// Kind is empty for an ordinary eval (a configuration proving
	// itself for work) or "calibration" (plans/os-2e34f66a.md D5): a
	// rubric spec whose known verdict is a human-scored gold
	// scorecard, committed to by digest and held outside the tree,
	// judged by the verifier under calibration.
	Kind        string       `json:"kind,omitempty"`
	Calibration *Calibration `json:"calibration,omitempty"`
}

// KindCalibration marks a calibration definition. It is the marker's
// kind too (transition.EvalKindCalibration): a filing carries it so the
// boundary can hold a verdict qualification to a calibration.
const KindCalibration = transition.EvalKindCalibration

// CalibrationFloor is the agreement a verifier must reach against the
// gold to qualify: policy on the protected spec surface
// (spec/evals.md states the figure; a drill pins the two), never
// a runtime argument. A definition may raise it and never lower it.
const CalibrationFloor = 0.8

// Calibration is a calibration definition's commitment: the gold
// scorecard's digest ("sha256:<hex>"), and an optional floor above the
// spec's.
type Calibration struct {
	Gold  string  `json:"gold"`
	Floor float64 `json:"floor,omitempty"`
}

// goldRE is the commitment's grammar.
var goldRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// IsCalibration reports whether the definition calibrates verifiers.
func (d Definition) IsCalibration() bool { return d.Kind == KindCalibration }

// Floor is the agreement the definition holds a verifier to: the
// spec's figure, or the definition's own where it raises it.
func (d Definition) Floor() float64 {
	if d.Calibration != nil && d.Calibration.Floor > CalibrationFloor {
		return d.Calibration.Floor
	}
	return CalibrationFloor
}

// Dir is the definition's repository-relative directory.
func (d Definition) Dir() string { return Root + "/" + d.Name }

// Load reads every definition under <repo>/evals, sorted by name,
// and refuses a malformed one by name: a definition is armed content,
// and one that does not say what it is files nothing.
func Load(repo string) ([]Definition, error) {
	entries, err := os.ReadDir(filepath.Join(repo, Root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Definition
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repo, Root, e.Name(), "eval.json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var d Definition
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("eval %s: eval.json: %v", e.Name(), err)
		}
		if d.Name != e.Name() {
			return nil, fmt.Errorf("eval %s: eval.json names %q; a definition is named by its directory", e.Name(), d.Name)
		}
		if strings.TrimSpace(d.Tier) == "" || strings.TrimSpace(d.Summary) == "" {
			return nil, fmt.Errorf("eval %s: eval.json needs a tier and a summary", d.Name)
		}
		if !strings.HasPrefix(d.Acceptance, d.Dir()+"/fixture/") {
			return nil, fmt.Errorf("eval %s: the acceptance spec must live under %s/fixture/, got %q", d.Name, d.Dir(), d.Acceptance)
		}
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(d.Acceptance))); err != nil {
			return nil, fmt.Errorf("eval %s: the acceptance spec %s is not in the tree: %v", d.Name, d.Acceptance, err)
		}
		if sol, err := os.ReadDir(filepath.Join(repo, Root, d.Name, "solution")); err != nil || len(sol) == 0 {
			return nil, fmt.Errorf("eval %s: solution/ must carry the reference solution's files", d.Name)
		}
		switch d.Kind {
		case "":
			if d.Calibration != nil {
				return nil, fmt.Errorf("eval %s: a calibration commitment on a definition whose kind is not %q", d.Name, KindCalibration)
			}
		case KindCalibration:
			if d.Calibration == nil || !goldRE.MatchString(d.Calibration.Gold) {
				return nil, fmt.Errorf("eval %s: a calibration definition commits to its gold scorecard as {\"calibration\": {\"gold\": \"sha256:<digest>\"}}", d.Name)
			}
			if d.Calibration.Floor != 0 && d.Calibration.Floor < CalibrationFloor {
				return nil, fmt.Errorf("eval %s: the declared floor %g is below the spec's %g: a definition may raise the floor and never lower it (spec/evals.md)", d.Name, d.Calibration.Floor, CalibrationFloor)
			}
			if d.Calibration.Floor > 1 {
				return nil, fmt.Errorf("eval %s: the declared floor %g is not a fraction", d.Name, d.Calibration.Floor)
			}
		default:
			return nil, fmt.Errorf("eval %s: kind %q is neither empty nor %q", d.Name, d.Kind, KindCalibration)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Find resolves a definition by name.
func Find(defs []Definition, name string) (Definition, bool) {
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return Definition{}, false
}

// Anchor is the reviewed revision an eval is filed at (D3): the last
// commit touching the definition and the PR whose squash merge landed
// it, both read from the repository. Ref and Gate name the SAME commit,
// which is what acceptance.md's equality rule demands, and both are
// facts about a review that happened.
type Anchor struct {
	Commit string
	PR     string
}

var prSubject = regexp.MustCompile(`\(#(\d+)\)\s*$`)

// ErrNotReviewed refuses a definition whose last commit is not a merged
// pull request, or which is dirty in the working tree: content that has
// not been through the gate arms nothing.
var ErrNotReviewed = errors.New("the eval definition is not at a reviewed revision")

// AnchorOf reads the definition's anchor from the repository.
func AnchorOf(repo string, def Definition) (Anchor, error) {
	dirty, err := git(repo, "status", "--porcelain", "--", def.Dir())
	if err != nil {
		return Anchor{}, err
	}
	if strings.TrimSpace(dirty) != "" {
		return Anchor{}, fmt.Errorf("%w: %s has uncommitted changes", ErrNotReviewed, def.Dir())
	}
	out, err := git(repo, "log", "-1", "--format=%H%x1f%s", "--", def.Dir())
	if err != nil {
		return Anchor{}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return Anchor{}, fmt.Errorf("%w: no commit touches %s", ErrNotReviewed, def.Dir())
	}
	commit, subject, _ := strings.Cut(out, "\x1f")
	m := prSubject.FindStringSubmatch(subject)
	if m == nil {
		return Anchor{}, fmt.Errorf("%w: the last commit touching %s (%.12s, %q) carries no merged pull request number in its subject", ErrNotReviewed, def.Dir(), commit, subject)
	}
	return Anchor{Commit: commit, PR: "pr/" + m[1]}, nil
}

// Ref is the acceptance anchor the filing cites.
func (a Anchor) Ref(def Definition) string { return def.Acceptance + " @ " + a.Commit }

// Gate is the gate evidence the filing cites: the merged review, at
// the ref's own commit.
func (a Anchor) Gate() string { return a.PR + " @ " + a.Commit }

func git(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ErrVacuous refuses an eval whose unsolved fixture already passes:
// it decides nothing, so a pass on it would qualify nobody
// (eval_vacuous, a refinement in spec_unrunnable's family).
var ErrVacuous = errors.New("the eval is vacuous: its unsolved fixture already passes every acceptance command, so it decides nothing")

// ErrSolutionRed refuses an eval whose reference solution stays red:
// the known verdict cannot be reproduced (checks_red).
var ErrSolutionRed = errors.New("the eval's reference solution stays red: its known verdict cannot be reproduced")

// CheckReport is what a check ran: the acceptance commands and their
// transcripts over the fixture and over the solution.
type CheckReport struct {
	Commands []string
	Fixture  []verdict.Transcript
	Solution []verdict.Transcript
}

// Check proves the known verdict: in a verifier workspace at the
// anchor it runs the acceptance commands and requires red, overlays
// the solution files and requires green. The workspace is removed
// either way.
func Check(repo string, def Definition, anchor Anchor, runner verdict.Runner) (CheckReport, error) {
	ws, err := verdict.NewWorkspace(repo, anchor.Commit)
	if err != nil {
		return CheckReport{}, err
	}
	defer ws.Cleanup()
	body, err := os.ReadFile(filepath.Join(ws.Repo, filepath.FromSlash(def.Acceptance)))
	if err != nil {
		return CheckReport{}, fmt.Errorf("eval %s: acceptance spec %s at %.12s: %v", def.Name, def.Acceptance, anchor.Commit, err)
	}
	rep := CheckReport{Commands: plan.Commands(body)}
	if len(rep.Commands) == 0 {
		return rep, &verdict.SpecUnrunnableError{Contract: def.Name, Ref: anchor.Ref(def)}
	}
	for _, c := range rep.Commands {
		rep.Fixture = append(rep.Fixture, runner.Run(ws, c))
	}
	if allGreen(rep.Fixture) {
		return rep, ErrVacuous
	}
	if err := overlay(filepath.Join(repo, Root, def.Name, "solution"), filepath.Join(ws.Repo, Root, def.Name, "fixture")); err != nil {
		return rep, err
	}
	for _, c := range rep.Commands {
		rep.Solution = append(rep.Solution, runner.Run(ws, c))
	}
	if !allGreen(rep.Solution) {
		return rep, ErrSolutionRed
	}
	return rep, nil
}

func allGreen(ts []verdict.Transcript) bool {
	for _, t := range ts {
		if t.Exit != 0 {
			return false
		}
	}
	return true
}

// overlay copies every file under from onto to, creating directories.
func overlay(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, info.Mode().Perm())
	})
}

// Filing is what turns a definition into a contract: the intent (with
// the eval marker) and the specification (the anchored acceptance and
// its gate), under a stable id derived from the eval, the tuple under
// re-test and the count of prior evals for that pair, so a second run
// in the same window refuses the duplicate at the boundary.
type Filing struct {
	Subject string
	Intent  []byte
	Spec    []byte
}

// File shapes the filing for a definition at its anchor; tu is the
// configuration under re-test on a spot-check, nil on a first eval.
func File(def Definition, anchor Anchor, tu *tuple.Tuple, prior int) (Filing, error) {
	return FileBound(def, anchor, tu, prior, "", "")
}

// FileBound is File with the marker bound to the hypothesis the eval
// is a counter-trajectory for and the candidate revision it runs
// against (plans/os-96850e5a.md D5): both or neither. The subject
// folds the binding in, so an eval filed for one candidate and one
// filed for another are two contracts.
func FileBound(def Definition, anchor Anchor, tu *tuple.Tuple, prior int, lesson, carrier string) (Filing, error) {
	if (lesson == "") != (carrier == "") {
		return Filing{}, errors.New("a bound eval names both the lesson and the carrier, or neither")
	}
	// An unbound eval keeps its id derivation exactly: the binding
	// joins the hash only when present, so every existing subject
	// and fixture is unchanged.
	key := fmt.Sprintf("%s\x00%s\x00%d", def.Name, tupleKey(tu), prior)
	if lesson != "" {
		key += fmt.Sprintf("\x00%s\x00%s", lesson, carrier)
	}
	sum := sha256.Sum256([]byte(key))
	id := "eval-" + hex.EncodeToString(sum[:6])
	marker := map[string]any{"name": def.Name}
	if def.IsCalibration() {
		marker["kind"] = KindCalibration
	}
	if tu != nil {
		marker["tuple"] = *tu
	}
	if lesson != "" {
		marker["lesson"] = lesson
		marker["carrier"] = carrier
	}
	intent, err := json.Marshal(map[string]any{
		"intent":  fmt.Sprintf("eval %s: %s", def.Name, def.Summary),
		"tier":    def.Tier,
		"budget":  "small",
		"routing": "core",
		"eval":    marker,
	})
	if err != nil {
		return Filing{}, err
	}
	spec, err := json.Marshal(map[string]any{
		"acceptance": map[string]any{"ref": anchor.Ref(def), "executable": true, "gate": anchor.Gate()},
	})
	if err != nil {
		return Filing{}, err
	}
	return Filing{Subject: id, Intent: intent, Spec: spec}, nil
}

func tupleKey(tu *tuple.Tuple) string {
	if tu == nil {
		return ""
	}
	b, _ := json.Marshal(tu)
	return string(b)
}

// The act kinds Due returns, and the lane each is owed by.
const (
	KindMint       = "mint"
	KindDisqualify = "disqualify"
	KindOffer      = "offer"
	KindSpotCheck  = "spot-check"
	// KindDefect is the defect filing drift owes (plans/os-2e34f66a.md
	// D5): the dispatcher's, naming the calibration contract and the
	// disagreeing items, idempotent through the ledger.
	KindDefect = "defect"

	LaneSupervise = keyring.CapSupervise
	LaneDispatch  = keyring.CapDispatch
)

// Act is one act the chain owes: the verb and payload to sign, on
// which subject, and the capability that owns it.
type Act struct {
	Kind    string `json:"kind"`
	Verb    string `json:"verb"`
	Subject string `json:"subject"`
	Payload string `json:"payload"`
	Lane    string `json:"lane"`
	Because string `json:"because"`
}

// Note is something Due looked at and did not turn into an act, by
// name: a receipt that did not recompute, a qualification dated in the
// future, a definition that is missing.
type Note struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

// Report is what one derivation found.
type Report struct {
	Acts  []Act  `json:"acts"`
	Notes []Note `json:"notes"`
}

// Inputs is everything Due reads. Now is the DECLARED instant, never a
// clock read, so a run replays; After is the spot-check interval (zero
// disables); Repo and Store are what the receipt recomputation needs.
type Inputs struct {
	Ctx     *admit.Context
	Ring    *keyring.State
	Store   *artifact.Store
	Repo    string
	Now     time.Time
	After   time.Duration
	Evals   []Definition
	Timeout time.Duration
	// OfferTTL is how long an offer Due publishes stays live; the
	// default is a day.
	OfferTTL time.Duration
	// Gold is the operator lane's gold scorecards by definition name
	// (plans/os-2e34f66a.md D5), held outside the tree and supplied to
	// the derivation; a calibration whose gold is absent owes nothing
	// and notes gold_missing.
	Gold map[string]Gold
}

// GoldItem is one human-scored rubric item.
type GoldItem struct {
	ID    string `json:"id"`
	Score string `json:"score"`
}

// Gold is a human-scored gold scorecard with the digest of its
// canonical bytes, the commitment a definition names.
type Gold struct {
	Items  []GoldItem `json:"items"`
	Digest string     `json:"-"`
}

// ParseGold decodes a gold scorecard strictly and digests its
// canonical (RFC 8785) bytes.
func ParseGold(raw []byte) (Gold, error) {
	var g struct {
		Items []GoldItem `json:"items"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return Gold{}, fmt.Errorf("a gold scorecard is the strict object {items: [{id, score}]}: %v", err)
	}
	if len(g.Items) == 0 {
		return Gold{}, errors.New("a gold scorecard scores at least one item")
	}
	seen := map[string]bool{}
	for _, it := range g.Items {
		if it.ID == "" || seen[it.ID] || (it.Score != "pass" && it.Score != "fail") {
			return Gold{}, fmt.Errorf("gold item %q: ids are unique and non-empty, scores pass or fail", it.ID)
		}
		seen[it.ID] = true
	}
	b, err := json.Marshal(g)
	if err != nil {
		return Gold{}, err
	}
	canonical, err := jcs.Transform(b)
	if err != nil {
		return Gold{}, err
	}
	sum := sha256.Sum256(canonical)
	return Gold{Items: g.Items, Digest: hex.EncodeToString(sum[:])}, nil
}

// LoadGold reads the gold scorecards a directory holds, one
// <name>.json per calibration definition; a definition with no file
// is simply absent, and a file that does not parse refuses by name.
func LoadGold(dir string, defs []Definition) (map[string]Gold, error) {
	out := map[string]Gold{}
	if dir == "" {
		return out, nil
	}
	for _, d := range defs {
		if !d.IsCalibration() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, d.Name+".json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		g, err := ParseGold(raw)
		if err != nil {
			return nil, fmt.Errorf("gold %s: %v", d.Name, err)
		}
		out[d.Name] = g
	}
	return out, nil
}

// Agreement is the fraction of gold items the verifier's payload items
// agree with (plans/os-2e34f66a.md D5): the same score at low
// uncertainty. High uncertainty is not agreement, since the verifier
// declined to decide. The disagreeing items are named.
func Agreement(scored []transition.ScoreItem, gold []GoldItem) (float64, []string) {
	byID := map[string]transition.ScoreItem{}
	for _, it := range scored {
		byID[it.ID] = it
	}
	agree := 0
	var disagreeing []string
	for _, g := range gold {
		it, ok := byID[g.ID]
		if ok && it.Score == g.Score && it.Uncertainty == "low" {
			agree++
			continue
		}
		disagreeing = append(disagreeing, g.ID)
	}
	if len(gold) == 0 {
		return 0, nil
	}
	return float64(agree) / float64(len(gold)), disagreeing
}

// DriftClass is the defect class drift files under.
const DriftClass = "calibration_drift"

// DriftDefectID is the stable id a drift defect files under: the
// maintenance loop's shape (class and subject hashed), so a second
// derivation re-files the same id and the boundary refuses the
// duplicate.
func DriftDefectID(contract string) string {
	sum := sha256.Sum256([]byte(DriftClass + "\x00" + contract))
	return "d-" + hex.EncodeToString(sum[:8])
}

// Due derives the acts owed at the declared instant (D5). Mints come
// only with a recomputed green receipt (D2); disqualifications are
// tuple-wide (D4); offers are never tuple-scoped (D6); spot-checks age
// from the qualification record's own ts, a future-dated one being due
// at once (D5).
func Due(in Inputs) Report {
	rep := Report{Acts: []Act{}, Notes: []Note{}}
	if in.Ctx == nil || in.Ctx.Lifecycle == nil || in.Ring == nil {
		return rep
	}
	ttl := in.OfferTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	fold := in.Ctx.Lifecycle
	// The ranking the offers carry (plans/os-c7554f18.md D2), derived
	// once at the declared instant from the same prefix, the gold
	// refining it when supplied.
	ranked, err := ranking.Derive(ranking.Inputs{Records: in.Ctx.Records, Ring: in.Ring, AsOf: in.Now.UTC().Format(time.RFC3339), Agreement: agreementFor(in)})
	if err != nil {
		rep.Notes = append(rep.Notes, Note{Kind: "ranking_unreadable", Subject: "system", Detail: err.Error()})
	}
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok || s.Eval == nil {
			continue
		}
		// The marker binds to the named definition at its reviewed
		// anchor (review finding on the task PR): a contract that
		// carries an eval name but not the definition's own acceptance
		// spec at the anchor is not an eval, and its verdict qualifies
		// or disqualifies nobody, however green or red.
		if note := bound(in, subject, s); note != nil {
			rep.Notes = append(rep.Notes, *note)
			continue
		}
		// A calibration the derivation cannot score is not offered
		// (review finding on the task PR): an offer would dispatch
		// work whose verdict nothing here can compare to the gold, so
		// the gold is looked for before anything is owed, and its
		// absence is the one note.
		def, isCalibration := Find(in.Evals, s.Eval.Name)
		isCalibration = isCalibration && def.IsCalibration()
		if isCalibration {
			if _, note := goldFor(in, def, subject); note != nil {
				rep.Notes = append(rep.Notes, *note)
				continue
			}
		}
		// Offers: a waiting eval with no live offer, scoped by policy
		// (plans/os-c7554f18.md D2; spec/ranking.md): a re-test
		// names the configuration under test, a first eval the top of
		// the claim ranking, and an empty ranking leaves the offer
		// unscoped, the bootstrap, and says so.
		if s.State == "ready" && len(s.LiveOffers(in.Now)) == 0 {
			eligibility := map[string]any{"capabilities": []string{keyring.CapClaim}, "tiers": []string{s.Tier}}
			because := "the eval is ready and no offer is live"
			switch {
			case s.Eval.Tuple != nil:
				eligibility["tuples"] = []tuple.Tuple{*s.Eval.Tuple}
				because += "; the offer names the configuration under re-test"
			case len(ranked.Capabilities[keyring.CapClaim]) > 0:
				eligibility["tuples"] = ranking.Top(ranked, keyring.CapClaim, 1)
				because += "; the offer carries the strongest claim tuple by policy"
			default:
				rep.Notes = append(rep.Notes, Note{Kind: "ranking_empty", Subject: subject,
					Detail: "no qualified claim tuple ranks at this instant, so the offer is unscoped: the first eval is how a configuration qualifies"})
			}
			payload, _ := json.Marshal(map[string]any{
				"eligibility": eligibility,
				"expires":     in.Now.Add(ttl).UTC().Format(time.RFC3339),
			})
			rep.Acts = append(rep.Acts, Act{Kind: KindOffer, Verb: transition.OfferPublishedVerb, Subject: subject,
				Payload: string(payload), Lane: LaneSupervise, Because: because})
		}
		// Calibration (plans/os-2e34f66a.md D5): the verifier's
		// scorecard against the gold, never the pass or fail.
		if isCalibration {
			rep = calibrate(in, def, subject, s, rep)
			continue
		}
		// Mints: an authenticated pass whose receipt recomputes green.
		if pass := admit.AuthenticPass(in.Ctx, subject, s); pass != nil {
			fence, holder, ok := admit.SubmissionWindow(in.Ctx.Records, subject, pass.Submission)
			declared := (*tuple.Tuple)(nil)
			if ok {
				declared = admit.WindowDeclaration(in.Ctx, subject, s, fence)
			}
			switch {
			case !ok || declared == nil:
				rep.Notes = append(rep.Notes, Note{Kind: "no_declaration", Subject: subject,
					Detail: "the pass verdict's window carries no admitted run.started declaring a tuple, so nothing qualifies"})
			case alreadyCited(in.Ring, holder, subject, pass.Pos, false):
				// minted already; nothing owed
			default:
				if note := recompute(in, subject, s, pass); note != nil {
					rep.Notes = append(rep.Notes, *note)
				} else {
					payload, _ := json.Marshal(map[string]any{
						"capability": keyring.CapClaim, "tuple": *declared, "contract": subject, "verdict": fmt.Sprintf("%d", pass.Pos),
					})
					rep.Acts = append(rep.Acts, Act{Kind: KindMint, Verb: keyring.VerbQualified, Subject: holder,
						Payload: string(payload), Lane: LaneSupervise,
						Because: fmt.Sprintf("the eval's pass at position %d recomputed green for the configuration the run declared", pass.Pos)})
				}
			}
		}
		// Disqualifications: an authenticated fail, tuple-wide.
		if fail := admit.AuthenticFail(in.Ctx, subject, s); fail != nil {
			fence, _, ok := admit.SubmissionWindow(in.Ctx.Records, subject, fail.Submission)
			declared := (*tuple.Tuple)(nil)
			if ok {
				declared = admit.WindowDeclaration(in.Ctx, subject, s, fence)
			}
			if declared == nil {
				rep.Notes = append(rep.Notes, Note{Kind: "no_declaration", Subject: subject,
					Detail: "the fail verdict's window carries no admitted run.started declaring a tuple, so nothing is disqualified"})
			} else {
				for _, actor := range in.Ring.Actors() {
					if !holds(in.Ring, actor, *declared) || alreadyCited(in.Ring, actor, subject, fail.Pos, true) {
						continue
					}
					payload, _ := json.Marshal(map[string]any{
						"capability": keyring.CapClaim, "tuple": *declared, "contract": subject, "verdict": fmt.Sprintf("%d", fail.Pos),
						"reason": fmt.Sprintf("the eval %s failed at position %d under this configuration", subject, fail.Pos),
					})
					rep.Acts = append(rep.Acts, Act{Kind: KindDisqualify, Verb: keyring.VerbDisqualified, Subject: actor,
						Payload: string(payload), Lane: LaneSupervise,
						Because: fmt.Sprintf("the eval's fail at position %d names a configuration this actor holds", fail.Pos)})
				}
			}
		}
	}
	if in.After > 0 {
		rep = spotChecks(in, rep)
	}
	return rep
}

// agreementFor answers the calibration agreement of a verdict by
// contract and position when the gold is supplied (plans/os-c7554f18.md
// D1): the same figure calibrate scores, so the ranking's refinement
// and the derivation's own judgment cannot disagree. Nil without gold,
// which is what leaves the ranking unrefined.
func agreementFor(in Inputs) func(contract string, verdict int) (float64, bool) {
	if len(in.Gold) == 0 {
		return nil
	}
	return func(contract string, verdict int) (float64, bool) {
		s, ok := in.Ctx.Lifecycle.State(contract)
		if !ok || s.Eval == nil {
			return 0, false
		}
		def, found := Find(in.Evals, s.Eval.Name)
		if !found || !def.IsCalibration() {
			return 0, false
		}
		gold, note := goldFor(in, def, contract)
		if note != nil {
			return 0, false
		}
		fact := admit.AuthenticVerdict(in.Ctx, contract, s)
		if fact == nil || fact.Pos != verdict || fact.Scorecard == nil {
			return 0, false
		}
		a, _ := Agreement(fact.Scorecard.Items, gold.Items)
		return a, true
	}
}

// bound checks that the eval's acceptance spec is the named
// definition's, at the definition's reviewed anchor: the acceptance ref
// the fold carries must equal Anchor.Ref for the shipped definition,
// executable and gated. Nil when bound; a note by name otherwise.
func bound(in Inputs, subject string, s transition.SubjectState) *Note {
	def, ok := Find(in.Evals, s.Eval.Name)
	if !ok {
		return &Note{Kind: "unbound", Subject: subject, Detail: fmt.Sprintf("the marker names eval %q, which is not among the shipped definitions", s.Eval.Name)}
	}
	if s.Acceptance == nil || !s.Acceptance.Executable || !s.Acceptance.Gated {
		return &Note{Kind: "unbound", Subject: subject, Detail: "the contract carries no executable, gated acceptance spec, so it is not the definition's eval"}
	}
	if in.Repo == "" {
		return &Note{Kind: "unbound", Subject: subject, Detail: "no repository to read the definition's reviewed anchor from; nothing is owed on an eval the derivation cannot bind"}
	}
	anchor, err := AnchorOf(in.Repo, def)
	if err != nil {
		return &Note{Kind: "unbound", Subject: subject, Detail: fmt.Sprintf("the definition's anchor cannot be read: %v", err)}
	}
	if s.Acceptance.Ref != anchor.Ref(def) {
		return &Note{Kind: "unbound", Subject: subject, Detail: fmt.Sprintf("the acceptance ref %q is not the definition's fixture at its reviewed anchor (%s): a contract may name an eval, but only the shipped definition's own spec at the reviewed commit is one", s.Acceptance.Ref, anchor.Ref(def))}
	}
	return nil
}

// recompute retrieves the cited receipt and recomputes it against the
// submission head with the same function seed verdict check runs; a
// nil return means it retrieved intact, reproduced the digest, and
// every transcript exited zero.
func recompute(in Inputs, subject string, s transition.SubjectState, pass *transition.VerdictFact) *Note {
	if in.Store == nil || in.Repo == "" {
		return &Note{Kind: "receipt_unchecked", Subject: subject, Detail: "no artifact store or repository to recompute the cited receipt against; nothing is minted on an unchecked receipt"}
	}
	if _, err := in.Store.Get(pass.Receipt); err != nil {
		return &Note{Kind: "receipt_missing", Subject: subject, Detail: fmt.Sprintf("the cited receipt %s is not retrievable intact: %v", pass.Receipt, err)}
	}
	if s.Sealed != nil {
		return &Note{Kind: "receipt_unchecked", Subject: subject, Detail: "the eval carries sealed checks, which this derivation does not unseal; nothing is minted on an unchecked receipt"}
	}
	input, err := verdict.InputFor(in.Ctx.Records, in.Ctx.Lifecycle, s, subject, in.Repo, in.Timeout)
	if err != nil {
		return &Note{Kind: "receipt_mismatch", Subject: subject, Detail: fmt.Sprintf("the submission cannot be recomputed: %v", err)}
	}
	r, err := verdict.Compute(input)
	if err != nil {
		return &Note{Kind: "receipt_mismatch", Subject: subject, Detail: fmt.Sprintf("recomputation failed: %v", err)}
	}
	digest, err := r.Digest()
	if err != nil {
		return &Note{Kind: "receipt_mismatch", Subject: subject, Detail: err.Error()}
	}
	if digest != pass.Receipt {
		return &Note{Kind: "receipt_mismatch", Subject: subject, Detail: fmt.Sprintf("the cited receipt %s does not reproduce from the submission head (recomputed %s)", pass.Receipt, digest)}
	}
	if !allGreen(r.Transcripts) || !allGreen(r.SealedTranscripts) {
		return &Note{Kind: "checks_red", Subject: subject, Detail: "the recomputed receipt carries a red transcript: a pass over red qualifies nobody"}
	}
	return nil
}

func holds(ring *keyring.State, actor string, t tuple.Tuple) bool {
	return holdsFor(ring, actor, keyring.CapClaim, t)
}

func holdsFor(ring *keyring.State, actor, capability string, t tuple.Tuple) bool {
	for _, have := range ring.GrantTuples(actor, capability) {
		if have.Equal(t) {
			return true
		}
	}
	return false
}

// calibrate derives what one calibration eval owes (plans/os-2e34f66a.md
// D5): with the gold supplied and matching the commitment, the
// verifier's authenticated verdict is compared item by item; agreement
// at or above the floor owes the verdict qualification for the
// verifier's declared tuple, below it the tuple-wide disqualification
// and the dispatcher's defect filing naming the disagreeing items.
func calibrate(in Inputs, def Definition, subject string, s transition.SubjectState, rep Report) Report {
	gold, note := goldFor(in, def, subject)
	if note != nil {
		rep.Notes = append(rep.Notes, *note)
		return rep
	}
	if s.Eval.Kind != KindCalibration {
		// The boundary holds a verdict qualification to a filing
		// marked as a calibration (review finding on the task PR),
		// so a calibration filed without the mark owes nothing the
		// boundary would admit: said, rather than owed and refused.
		rep.Notes = append(rep.Notes, Note{Kind: "kind_unmarked", Subject: subject,
			Detail: fmt.Sprintf("the filing's eval marker does not say %q, so its verdict qualifies nobody: file the calibration through seed eval file, which marks it", KindCalibration)})
		return rep
	}
	fact := admit.AuthenticVerdict(in.Ctx, subject, s)
	if fact == nil {
		return rep
	}
	switch {
	case fact.Tuple == nil:
		rep.Notes = append(rep.Notes, Note{Kind: "no_declaration", Subject: subject,
			Detail: "the verdict declares no tuple, so nothing qualifies: calibration is for the configuration the verifier rendered under"})
		return rep
	case fact.Scorecard == nil:
		rep.Notes = append(rep.Notes, Note{Kind: "no_scorecard", Subject: subject,
			Detail: "the verdict cites no scorecard, so there is nothing to compare to the gold"})
		return rep
	}
	agreement, disagreeing := Agreement(fact.Scorecard.Items, gold.Items)
	floor := def.Floor()
	verifier := fact.Signer
	if agreement >= floor {
		if !alreadyCited(in.Ring, verifier, subject, fact.Pos, false) {
			payload, _ := json.Marshal(map[string]any{
				"capability": keyring.CapVerdict, "tuple": *fact.Tuple, "contract": subject, "verdict": fmt.Sprintf("%d", fact.Pos),
			})
			rep.Acts = append(rep.Acts, Act{Kind: KindMint, Verb: keyring.VerbQualified, Subject: verifier,
				Payload: string(payload), Lane: LaneSupervise,
				Because: fmt.Sprintf("the calibration's verdict at position %d agrees with the gold on %.0f%% of items, at or above the floor %.0f%%", fact.Pos, agreement*100, floor*100)})
		}
		return rep
	}
	// Drift: tuple-wide, every verifier holding the configuration,
	// and the verifier that rendered even when nothing cites its
	// tuple yet (review finding on the task PR): a verifier holding
	// verdict by a bare grant renders under the bridge, and its first
	// failed calibration is what closes it.
	for _, actor := range in.Ring.Actors() {
		bridging := actor == verifier && in.Ring.HasAnyCapability(actor, []string{keyring.CapVerdict}) && !in.Ring.EverCited(actor, keyring.CapVerdict)
		if (!holdsFor(in.Ring, actor, keyring.CapVerdict, *fact.Tuple) && !bridging) || alreadyCited(in.Ring, actor, subject, fact.Pos, true) {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"capability": keyring.CapVerdict, "tuple": *fact.Tuple, "contract": subject, "verdict": fmt.Sprintf("%d", fact.Pos),
			"reason": fmt.Sprintf("the calibration %s drifted at position %d under this configuration: agreement %.0f%% below the floor %.0f%% on %s", subject, fact.Pos, agreement*100, floor*100, strings.Join(disagreeing, ", ")),
		})
		rep.Acts = append(rep.Acts, Act{Kind: KindDisqualify, Verb: keyring.VerbDisqualified, Subject: actor,
			Payload: string(payload), Lane: LaneSupervise,
			Because: fmt.Sprintf("the calibration's verdict at position %d drifted below the floor under a configuration this actor holds", fact.Pos)})
	}
	id := DriftDefectID(subject)
	if _, filed := in.Ctx.Lifecycle.State(id); !filed {
		payload, _ := json.Marshal(map[string]string{
			"intent":  fmt.Sprintf("defect %s on %s: the verifier %s drifted at position %d, agreement %.0f%% below the floor %.0f%%, disagreeing on %s", DriftClass, subject, verifier, fact.Pos, agreement*100, floor*100, strings.Join(disagreeing, ", ")),
			"tier":    "trivial",
			"budget":  "small",
			"routing": "core",
		})
		rep.Acts = append(rep.Acts, Act{Kind: KindDefect, Verb: "intent.filed", Subject: id, Payload: string(payload), Lane: LaneDispatch,
			Because: fmt.Sprintf("drift on %s files a defect naming the contract and the disagreeing items, once per contract", subject)})
	}
	return rep
}

// goldFor finds the supplied gold a calibration definition commits
// to, or the note saying why nothing can be scored: no gold supplied
// (gold_missing), or a gold that is not the commitment
// (gold_mismatch).
func goldFor(in Inputs, def Definition, subject string) (Gold, *Note) {
	gold, ok := in.Gold[def.Name]
	if !ok {
		return Gold{}, &Note{Kind: "gold_missing", Subject: subject,
			Detail: fmt.Sprintf("no gold scorecard was supplied for calibration %s (--gold <dir>/%s.json); nothing is owed on a calibration the derivation cannot score, and it is not offered", def.Name, def.Name)}
	}
	if "sha256:"+gold.Digest != def.Calibration.Gold {
		return Gold{}, &Note{Kind: "gold_mismatch", Subject: subject,
			Detail: fmt.Sprintf("the supplied gold's digest sha256:%s is not the definition's commitment %s; nothing is scored against a gold the tree did not commit to, and it is not offered", gold.Digest, def.Calibration.Gold)}
	}
	return gold, nil
}

func alreadyCited(ring *keyring.State, actor, contract string, verdictPos int, disqualified bool) bool {
	for _, q := range ring.Qualifications(actor) {
		if q.Contract == contract && q.Verdict == verdictPos && q.Disqualified == disqualified {
			return true
		}
	}
	return false
}

// spotChecks files a re-test for every admissible (actor, tuple) whose
// latest qualification is older than the interval and which no open
// eval already names.
func spotChecks(in Inputs, rep Report) Report {
	filed := map[string]bool{}
	for _, actor := range in.Ring.Actors() {
		for _, capability := range []string{keyring.CapClaim, keyring.CapVerdict} {
			rep = spotCheckFor(in, rep, filed, actor, capability)
		}
	}
	return rep
}

// spotCheckFor is one actor's spot checks for one capability: claim
// qualifications re-run their eval, verdict qualifications their
// calibration (plans/os-2e34f66a.md D5), aging alike.
func spotCheckFor(in Inputs, rep Report, filed map[string]bool, actor, capability string) Report {
	fold := in.Ctx.Lifecycle
	{
		for _, held := range in.Ring.GrantTuples(actor, capability) {
			var latest *keyring.Qualification
			for i := range in.Ring.Qualifications(actor) {
				q := in.Ring.Qualifications(actor)[i]
				if q.Capability == capability && !q.Disqualified && q.Tuple.Equal(held) {
					latest = &q
				}
			}
			if latest == nil {
				continue // granted by hand: no eval to re-run
			}
			ts, err := time.Parse(time.RFC3339, latest.TS)
			if err != nil {
				rep.Notes = append(rep.Notes, Note{Kind: "anomaly", Subject: actor, Detail: fmt.Sprintf("the qualification citing %s carries an unreadable ts %q; treated as due", latest.Contract, latest.TS)})
			} else if ts.After(in.Now) {
				rep.Notes = append(rep.Notes, Note{Kind: "anomaly", Subject: actor, Detail: fmt.Sprintf("the qualification citing %s is dated %s, after the declared instant %s; a lie about time cannot postpone a re-test, so it is due now", latest.Contract, latest.TS, in.Now.UTC().Format(time.RFC3339))})
			} else if in.Now.Sub(ts) <= in.After {
				continue
			}
			key := capability + ":" + tupleKey(&held)
			if filed[key] {
				continue
			}
			name, open, prior := evalsNaming(fold, latest.Contract, held)
			if name == "" {
				rep.Notes = append(rep.Notes, Note{Kind: "no_definition", Subject: actor, Detail: fmt.Sprintf("the qualification cites %s, which names no eval; nothing to re-run", latest.Contract)})
				continue
			}
			if open {
				continue
			}
			def, ok := Find(in.Evals, name)
			if !ok {
				rep.Notes = append(rep.Notes, Note{Kind: "no_definition", Subject: actor, Detail: fmt.Sprintf("eval %s is not among the shipped definitions", name)})
				continue
			}
			anchor, err := AnchorOf(in.Repo, def)
			if err != nil {
				rep.Notes = append(rep.Notes, Note{Kind: "not_reviewed", Subject: actor, Detail: err.Error()})
				continue
			}
			f, err := File(def, anchor, &held, prior)
			if err != nil {
				rep.Notes = append(rep.Notes, Note{Kind: "anomaly", Subject: actor, Detail: err.Error()})
				continue
			}
			filed[key] = true
			because := fmt.Sprintf("the qualification of %s citing %s is older than %s; re-testing the configuration", actor, latest.Contract, in.After)
			rep.Acts = append(rep.Acts,
				Act{Kind: KindSpotCheck, Verb: "intent.filed", Subject: f.Subject, Payload: string(f.Intent), Lane: LaneDispatch, Because: because},
				Act{Kind: KindSpotCheck, Verb: "contract.specified", Subject: f.Subject, Payload: string(f.Spec), Lane: LaneDispatch, Because: because})
		}
	}
	return rep
}

// Prior counts the evals already filed for the definition and the
// configuration under re-test (nil on a first eval): the count File's
// stable id derives from, so a satisfied window advances it and a
// second run in the same window refuses the duplicate.
func Prior(fold *transition.Fold, name string, tu *tuple.Tuple) int {
	n := 0
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok || s.Eval == nil || s.Eval.Name != name {
			continue
		}
		switch {
		case tu == nil && s.Eval.Tuple == nil:
			n++
		case tu != nil && s.Eval.Tuple != nil && s.Eval.Tuple.Equal(*tu):
			n++
		}
	}
	return n
}

// evalsNaming reads, for the eval a qualification cites, its
// definition's name, whether an eval naming this tuple is still open,
// and how many evals have named this (name, tuple) pair.
func evalsNaming(fold *transition.Fold, contract string, t tuple.Tuple) (name string, open bool, prior int) {
	if s, ok := fold.State(contract); ok && s.Eval != nil {
		name = s.Eval.Name
	}
	if name == "" {
		return "", false, 0
	}
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok || s.Eval == nil || s.Eval.Name != name || s.Eval.Tuple == nil || !s.Eval.Tuple.Equal(t) {
			continue
		}
		prior++
		if s.Verdict == nil && s.State != "done" && s.State != "cancelled" {
			open = true
		}
	}
	return name, open, prior
}
