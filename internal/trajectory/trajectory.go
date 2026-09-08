// Package trajectory is the trajectory-prefix regression harness for
// lane decision points (SEED-NEXT.md §16; charter III.O row 3;
// plans/os-6bd9ffff.md). A trajectory is what the record already says
// a lane did, at the frame it decided from: every signed record the
// lane's key appended and every refused attempt its key journaled,
// each paired with the subject's folded state, the actor's
// affordances on the subject and the obligation kinds owed to it,
// all derived at the prefix the lane actually saw. Replay recomputes
// those frames over the chain and the lane configuration as they
// stand now and classifies every point exactly once, so a change to
// the boundary, the fold, a manifest or a fragment that alters what
// a recorded decision point looked like fails a drill rather than
// passing unnoticed.
//
// What replay proves, and what it cannot: no decider re-runs at a
// point, so a green replay says the configuration still presents the
// same frame and still permits the same act, not that a model would
// choose it. That residual is stated in spec/trajectories.md in
// the injection suite's words; Phase 13's simulation mode is the seam
// where a decider plugs in.
package trajectory

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
)

// The two outcomes a point carries: a signed record on the chain, or
// a refused attempt in the journal. The journal's own admitted lines
// are not read: the chain is the record of what was admitted.
const (
	OutcomeAdmitted = refusals.OutcomeAdmitted
	OutcomeRefused  = refusals.OutcomeRefused
)

// Frame is what the lane decided from at the prefix before a point:
// the subject's folded lifecycle state (empty for a subject the
// lifecycle never created), the actor's affordances on the subject,
// and the obligation kinds owed to the actor on the subject, sorted.
// It carries no instant, so two recordings of one chain are
// byte-identical (D1).
type Frame struct {
	State       string   `json:"state"`
	Affordances []string `json:"affordances"`
	Owed        []string `json:"owed"`
}

// Point is one decision point: the position (an admitted record's
// own, or the tip ordinal a refusal was stamped with), the verb, the
// loop-act spelling when internal/loopverb has one, the subject, the
// outcome, the refusal's machine code, and the frame.
type Point struct {
	Position int    `json:"position"`
	Verb     string `json:"verb"`
	Act      string `json:"act,omitempty"`
	Subject  string `json:"subject"`
	Outcome  string `json:"outcome"`
	Code     string `json:"code,omitempty"`
	Frame    Frame  `json:"frame"`
}

// Trajectory is one lane's recorded decision points on one chain,
// under one configuration: the manifest's digest is the sha256 of the
// manifest bytes and the posture's the sha256 of the resolved
// fragments, so a configuration edit no point-level class reads
// (orients_from, liveness_from, inbox, summary, the fragment list)
// still diverges at replay.
type Trajectory struct {
	Lane     string  `json:"lane"`
	Actor    string  `json:"actor"`
	Manifest string  `json:"manifest"`
	Posture  string  `json:"posture"`
	Points   []Point `json:"points"`
}

// Skipped counts the journal lines a recording could not place: a
// refusal stamped beyond the chain's tip (a journal that outran the
// ledger copy it is read beside) and a line another actor journaled.
// Counted rather than silently dropped, so a recording says what it
// left out.
type Skipped struct {
	BeyondTip  int `json:"beyond_tip"`
	OtherActor int `json:"other_actor"`
}

// Configuration is a lane's manifest with the two digests replay
// compares.
type Configuration struct {
	Manifest lane.Manifest
	// ManifestDigest is the sha256 of the manifest file's bytes.
	ManifestDigest string
	// PostureDigest is the sha256 of the resolved fragments.
	PostureDigest string
}

// LoadConfiguration reads one lane's manifest from the lanes directory
// and computes both digests. The manifest set comes from lane.Load,
// so an undecodable manifest refuses here as it does everywhere.
func LoadConfiguration(lanesDir, laneName string) (*Configuration, error) {
	ms, err := lane.Load(lanesDir)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.Lane != laneName {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(lanesDir, laneName+".json"))
		if err != nil {
			return nil, err
		}
		resolved, err := lane.Resolve(lanesDir, m)
		if err != nil {
			return nil, err
		}
		return &Configuration{Manifest: m, ManifestDigest: digest(raw), PostureDigest: digest([]byte(resolved))}, nil
	}
	return nil, fmt.Errorf("lane %q has no manifest in %s", laneName, lanesDir)
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// actOf maps a ledger verb to its loop-act spelling, when it has one.
func actOf(verb string) string {
	for _, a := range loopverb.Acts() {
		if a.Verb == verb {
			return a.Name()
		}
	}
	return ""
}

// Record derives one lane's trajectory from a verified chain and the
// attempts journal read beside it (D1). Admitted points are the
// records the key signed, in position order, each framed at
// records[:p]: a record at p was judged against everything before
// it. Refused points are the key's refused journal lines, each framed
// at records[:p+1]: the stamp is the last record of the view the
// boundary judged the attempt against, so a refusal stamped at the
// chain's last record sees the whole chain. An admitted record at p
// precedes a refusal stamped p, and refusals stamped alike keep the
// journal's order.
func Record(records []*event.Record, journal *refusals.Journal, key ed25519.PrivateKey, lanesDir, laneName string) (*Trajectory, Skipped, error) {
	var skipped Skipped
	if len(key) != ed25519.PrivateKeySize {
		return nil, skipped, errors.New("recording needs the lane's own private key: a fingerprint alone cannot probe the boundary")
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, skipped, err
	}
	cfg, err := LoadConfiguration(lanesDir, laneName)
	if err != nil {
		return nil, skipped, err
	}
	t := &Trajectory{Lane: laneName, Actor: fp, Manifest: cfg.ManifestDigest, Posture: cfg.PostureDigest, Points: []Point{}}
	type ordered struct {
		p    Point
		rank int // admitted 0, refused 1: a refusal stamped p saw the record at p
		seq  int // journal order among refusals stamped alike
	}
	var pts []ordered
	for pos, rec := range records {
		if rec.Event.Actor != fp {
			continue
		}
		frame, err := frameAt(records[:pos], key, fp, rec.Event.Subject)
		if err != nil {
			return nil, skipped, fmt.Errorf("position %d: %w", pos, err)
		}
		pts = append(pts, ordered{p: Point{Position: pos, Verb: rec.Event.Verb, Act: actOf(rec.Event.Verb), Subject: rec.Event.Subject, Outcome: OutcomeAdmitted, Frame: frame}})
	}
	if journal != nil {
		for seq, e := range journal.Entries {
			if e.Outcome != OutcomeRefused {
				continue
			}
			if e.Actor != fp {
				skipped.OtherActor++
				continue
			}
			p, err := strconv.Atoi(e.Position)
			if err != nil || p < 0 || p >= len(records) {
				skipped.BeyondTip++
				continue
			}
			frame, err := frameAt(records[:p+1], key, fp, e.Subject)
			if err != nil {
				return nil, skipped, fmt.Errorf("refusal stamped %d: %w", p, err)
			}
			pts = append(pts, ordered{p: Point{Position: p, Verb: e.Verb, Act: actOf(e.Verb), Subject: e.Subject, Outcome: OutcomeRefused, Code: e.Code, Frame: frame}, rank: 1, seq: seq})
		}
	}
	sort.SliceStable(pts, func(i, j int) bool {
		if pts[i].p.Position != pts[j].p.Position {
			return pts[i].p.Position < pts[j].p.Position
		}
		if pts[i].rank != pts[j].rank {
			return pts[i].rank < pts[j].rank
		}
		return pts[i].seq < pts[j].seq
	})
	for _, o := range pts {
		t.Points = append(t.Points, o.p)
	}
	return t, skipped, nil
}

// frameAt derives the frame at one prefix. The empty prefix has no
// genesis and so no context: its frame is empty, the first record's
// own (D1).
func frameAt(prefix []*event.Record, key ed25519.PrivateKey, fp, subject string) (Frame, error) {
	frame := Frame{Affordances: []string{}, Owed: []string{}}
	if len(prefix) == 0 {
		return frame, nil
	}
	ctx, err := admit.ContextOver(prefix)
	if err != nil {
		return frame, err
	}
	if s, ok := ctx.Lifecycle.State(subject); ok {
		frame.State = s.State
	}
	frame.Affordances = admit.Affordances(ctx, key, subject)
	owed, err := owedKinds(prefix, ctx.Keyring, fp, subject)
	if err != nil {
		return frame, err
	}
	frame.Owed = owed
	return frame, nil
}

// laneCapabilities mirrors the situation read's rule for which
// lane-owed rows are the caller's: a row owed to a lane belongs to
// every key holding the lane's capability. Pinned against seed
// situation by drill, so the two readers cannot drift.
var laneCapabilities = map[string]string{
	obligation.LaneVerifier:   keyring.CapVerdict,
	obligation.LaneObserver:   keyring.CapObserver,
	obligation.LaneSupervisor: keyring.CapSupervise,
	obligation.LaneOperator:   keyring.CapOperator,
}

// owedKinds is the sorted, deduplicated set of obligation kinds owed
// to the actor on the subject at the prefix: the situation read's
// rows, restricted to the point's subject.
func owedKinds(prefix []*event.Record, ring *keyring.State, fp, subject string) ([]string, error) {
	rows, err := project.DeriveObligations(prefix)
	if err != nil {
		return nil, err
	}
	lanes := map[string]bool{}
	for name, capability := range laneCapabilities {
		if ring.HasAnyCapability(fp, []string{capability}) {
			lanes[name] = true
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, row := range rows {
		if row.Subject != subject || seen[row.Kind] {
			continue
		}
		if row.OwedBy != fp && !lanes[row.OwedBy] {
			continue
		}
		seen[row.Kind] = true
		out = append(out, row.Kind)
	}
	sort.Strings(out)
	return out, nil
}

// Canonical is the trajectory's RFC 8785 bytes: the committed form,
// byte-identical across recordings of one chain.
func (t *Trajectory) Canonical() ([]byte, error) {
	if t.Points == nil {
		t.Points = []Point{}
	}
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Parse decodes a trajectory strictly: every field known, both
// digests well-formed, every point carrying a known outcome and a
// non-negative position. A trajectory is a committed input, so
// garbage is the committer's error, never a silently empty replay.
func Parse(b []byte) (*Trajectory, error) {
	var t Trajectory
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("trajectory does not parse: %v", err)
	}
	if dec.More() {
		return nil, errors.New("trajectory carries trailing data")
	}
	if t.Lane == "" || t.Actor == "" {
		return nil, errors.New("trajectory names a lane and an actor")
	}
	if !digestRE.MatchString(t.Manifest) || !digestRE.MatchString(t.Posture) {
		return nil, errors.New("trajectory carries the manifest and posture digests, 64 lowercase hex characters each")
	}
	if t.Points == nil {
		t.Points = []Point{}
	}
	for i, p := range t.Points {
		if p.Outcome != OutcomeAdmitted && p.Outcome != OutcomeRefused {
			return nil, fmt.Errorf("point %d: outcome must be %q or %q, got %q", i, OutcomeAdmitted, OutcomeRefused, p.Outcome)
		}
		if p.Position < 0 || p.Verb == "" || p.Subject == "" {
			return nil, fmt.Errorf("point %d: a point names a non-negative position, a verb and a subject", i)
		}
	}
	return &t, nil
}

// Class is one of the replay's divergence classes: five judge a
// point and two judge the configuration once per trajectory (D2).
type Class string

const (
	// Same: the recomputed frame equals the recorded one, the act is
	// declared and granted, and an admitted act is still afforded.
	Same Class = "same"
	// FrameChanged: the state, the affordances or the owed kinds
	// differ. The boundary or the fold changed under the lane, or
	// the chain did.
	FrameChanged Class = "frame_changed"
	// ActUndeclared: the manifest's acts_through no longer names an
	// admitted point's loop act. Admitted points only: a refused
	// attempt may have reached for an act the lane never declared,
	// which is often why it was refused, and a class that fired on
	// such a point would fail the corpus on the day it was recorded.
	ActUndeclared Class = "act_undeclared"
	// ActUngranted: the manifest's grants no longer intersect an
	// admitted point's accepted capabilities. Admitted points only,
	// for the same reason.
	ActUngranted Class = "act_ungranted"
	// ActInadmissible: an admitted point's verb is absent from the
	// recomputed affordances. The recorded frame lacked it too (a
	// record the boundary would not have afforded, past it on the
	// raw seam), so the frame itself is unchanged.
	ActInadmissible Class = "act_inadmissible"
	// ManifestChanged: the manifest bytes' digest differs from the
	// recorded one.
	ManifestChanged Class = "manifest_changed"
	// PostureChanged: the resolved fragments' digest differs.
	PostureChanged Class = "posture_changed"
	// ChoiceDiverged: the frame is unchanged and the configuration still
	// declares, grants and affords the recorded act, but the decider
	// re-run at this point chooses a different act (plans/os-16e55c11.md
	// D4). It is the behavioral regression III.O row 5 asked for: the
	// choice moved though nothing in the configuration did.
	ChoiceDiverged Class = "choice_diverged"
)

// Decider chooses the loop act a lane would take at a frame, or "" when
// the frame does not determine the choice. ReplayWithDecider re-runs it
// at each recorded loop-act point.
type Decider func(frame Frame, m lane.Manifest) string

// Verdict is one point's replay: the point's identity and its class,
// with the detail that names what differed.
type Verdict struct {
	Position int    `json:"position"`
	Verb     string `json:"verb"`
	Subject  string `json:"subject"`
	Outcome  string `json:"outcome"`
	Class    Class  `json:"class"`
	Detail   string `json:"detail,omitempty"`
}

// Result is a whole replay: every point's verdict in trajectory
// order, and the configuration's two digests as recomputed, each
// flagged when it differs from the recorded one.
type Result struct {
	Lane            string    `json:"lane"`
	Points          []Verdict `json:"points"`
	Manifest        string    `json:"manifest"`
	Posture         string    `json:"posture"`
	ManifestChanged bool      `json:"manifest_changed"`
	PostureChanged  bool      `json:"posture_changed"`
}

// Divergent returns the verdicts whose class is not Same.
func (r *Result) Divergent() []Verdict {
	out := []Verdict{}
	for _, v := range r.Points {
		if v.Class != Same {
			out = append(out, v)
		}
	}
	return out
}

// Diverged reports whether any point or either digest diverged: the
// replay passes iff every point is Same and both digests match.
func (r *Result) Diverged() bool {
	return r.ManifestChanged || r.PostureChanged || len(r.Divergent()) > 0
}

// Replay recomputes every recorded point's frame over the chain and
// classifies it against the lane configuration as it stands now (D2).
// A refused point is judged by its frame alone: the same frame means
// the boundary presents the same choice, and the recorded refusal
// stands as what it answered. An admitted point is further held to the
// configuration (declared, granted) and to the boundary (afforded).
// The key must be the recorded actor's own: a fingerprint alone
// cannot probe the boundary, and another key's affordances are
// another lane's frame.
func Replay(t *Trajectory, records []*event.Record, key ed25519.PrivateKey, lanesDir string) (*Result, error) {
	return replayCore(t, records, key, lanesDir, nil)
}

// ReplayWithDecider replays as Replay does, and additionally re-runs the
// decider at each admitted loop-act point whose frame is unchanged and
// whose act the configuration still permits; a definite choice that
// differs from the recorded act is ChoiceDiverged (D4).
func ReplayWithDecider(t *Trajectory, records []*event.Record, key ed25519.PrivateKey, lanesDir string, dec func(Frame, lane.Manifest) string) (*Result, error) {
	return replayCore(t, records, key, lanesDir, dec)
}

func replayCore(t *Trajectory, records []*event.Record, key ed25519.PrivateKey, lanesDir string, dec func(Frame, lane.Manifest) string) (*Result, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("replay needs the lane's own private key: a fingerprint alone cannot probe the boundary")
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, err
	}
	if fp != t.Actor {
		return nil, fmt.Errorf("the key is %s and the trajectory is %s's: a lane replays its own trajectory with its own key", fp, t.Actor)
	}
	cfg, err := LoadConfiguration(lanesDir, t.Lane)
	if err != nil {
		return nil, err
	}
	r := &Result{Lane: t.Lane, Points: []Verdict{}, Manifest: cfg.ManifestDigest, Posture: cfg.PostureDigest,
		ManifestChanged: cfg.ManifestDigest != t.Manifest, PostureChanged: cfg.PostureDigest != t.Posture}
	for _, p := range t.Points {
		v := Verdict{Position: p.Position, Verb: p.Verb, Subject: p.Subject, Outcome: p.Outcome, Class: Same}
		// The prefix bound is checked on the position itself, never
		// on position+1: a refused point at the largest position a
		// parse admits would overflow the sum and slip past the guard
		// into the slice (review finding on the task PR).
		beyond := p.Position > len(records) || (p.Outcome == OutcomeRefused && p.Position >= len(records))
		if beyond {
			v.Class, v.Detail = FrameChanged, fmt.Sprintf("the chain ends at %d records, before the point's prefix at position %d", len(records), p.Position)
			r.Points = append(r.Points, v)
			continue
		}
		end := p.Position
		if p.Outcome == OutcomeRefused {
			end = p.Position + 1
		}
		frame, err := frameAt(records[:end], key, fp, p.Subject)
		if err != nil {
			return nil, fmt.Errorf("position %d: %w", p.Position, err)
		}
		admitted := p.Outcome == OutcomeAdmitted
		switch {
		case !frameEqual(frame, p.Frame):
			v.Class, v.Detail = FrameChanged, frameDiff(p.Frame, frame)
		case admitted && p.Act != "" && !slices.Contains(cfg.Manifest.ActsThrough, p.Act):
			v.Class, v.Detail = ActUndeclared, fmt.Sprintf("acts_through no longer names %q", p.Act)
		case admitted && ungranted(cfg.Manifest.Grants, p.Verb):
			v.Class, v.Detail = ActUngranted, fmt.Sprintf("grants %v no longer intersect %s's accepted capabilities %v", cfg.Manifest.Grants, p.Verb, keyring.AcceptedCapabilities(p.Verb))
		case admitted && !slices.Contains(frame.Affordances, p.Verb):
			v.Class, v.Detail = ActInadmissible, fmt.Sprintf("the boundary no longer affords %s at the point's prefix", p.Verb)
		}
		// The decider re-decision (D4): only on a point still Same —
		// frame unchanged and the configuration permitting the act — so
		// a divergence is the CHOICE moving, never the frame or the
		// configuration (those are the five classes above). A "" choice
		// abstains: the frame did not determine the act.
		if v.Class == Same && dec != nil && admitted && p.Act != "" {
			if choice := dec(frame, cfg.Manifest); choice != "" && choice != p.Act {
				v.Class = ChoiceDiverged
				v.Detail = fmt.Sprintf("the decider chose %q where the point recorded %q, the frame and the configuration unchanged", choice, p.Act)
			}
		}
		r.Points = append(r.Points, v)
	}
	return r, nil
}

// ungranted reports a verb whose accepted capabilities the grants do
// not reach. A verb with no capability row needs standing only, which
// no grant list can lose.
func ungranted(grants []string, verb string) bool {
	accepted := keyring.AcceptedCapabilities(verb)
	if len(accepted) == 0 {
		return false
	}
	for _, g := range grants {
		if slices.Contains(accepted, g) {
			return false
		}
	}
	return true
}

func frameEqual(a, b Frame) bool {
	return a.State == b.State && slices.Equal(a.Affordances, b.Affordances) && slices.Equal(a.Owed, b.Owed)
}

// frameDiff names which parts of the frame moved, recorded then
// recomputed.
func frameDiff(recorded, now Frame) string {
	var parts []string
	if recorded.State != now.State {
		parts = append(parts, fmt.Sprintf("state %q -> %q", recorded.State, now.State))
	}
	if !slices.Equal(recorded.Affordances, now.Affordances) {
		parts = append(parts, fmt.Sprintf("affordances %v -> %v", recorded.Affordances, now.Affordances))
	}
	if !slices.Equal(recorded.Owed, now.Owed) {
		parts = append(parts, fmt.Sprintf("owed %v -> %v", recorded.Owed, now.Owed))
	}
	return strings.Join(parts, "; ")
}
