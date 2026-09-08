package trajectory_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
	"github.com/shaunlmason/open-seed-v2/internal/trajectory"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	filed  = `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`
	spec   = `{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`
	packet = `{"acceptance": ["resume from here"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
)

func tKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func tFP(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// chain is a local ledger with a genesis and a loose resolver over the
// drill's keys: raw appends land without admission (the seam a raw
// push uses), admitted appends run the same admit.Check the boundary
// runs and fail the drill on refusal.
type chain struct {
	t     *testing.T
	dir   string
	store *ledger.Store
	loose ledger.Resolver
	keys  map[string]ed25519.PrivateKey
}

func newChain(t *testing.T) *chain {
	t.Helper()
	c := &chain{t: t, dir: t.TempDir(), keys: map[string]ed25519.PrivateKey{"root": tKey(1), "worker": tKey(2), "dispatcher": tKey(3), "verifier": tKey(4)}}
	store, err := ledger.Open(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	c.store = store
	g, err := genesis.Init(store, c.keys["root"], nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
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
	c.loose = func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range c.keys {
			if tFP(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	return c
}

func (c *chain) records() []*event.Record {
	c.t.Helper()
	ctx, err := admit.ContextAt(c.store)
	if err != nil {
		c.t.Fatal(err)
	}
	return ctx.Records
}

func (c *chain) draft(key ed25519.PrivateKey, verb, subject, payload string) (*event.Record, *admit.Context) {
	c.t.Helper()
	ctx, err := admit.ContextAt(c.store)
	if err != nil {
		c.t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{V: ctx.Active, TS: "2026-09-01T01:00:00Z", Actor: tFP(c.t, key), Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: ctx.Tip}, key)
	if err != nil {
		c.t.Fatal(err)
	}
	return rec, ctx
}

// raw appends past the boundary.
func (c *chain) raw(key ed25519.PrivateKey, verb, subject, payload string) int {
	c.t.Helper()
	rec, _ := c.draft(key, verb, subject, payload)
	pos, err := c.store.Append(rec, c.loose)
	if err != nil {
		c.t.Fatal(err)
	}
	return pos
}

// admitted appends through the boundary.
func (c *chain) admitted(key ed25519.PrivateKey, verb, subject, payload string) int {
	c.t.Helper()
	rec, ctx := c.draft(key, verb, subject, payload)
	if err := admit.Check(ctx, rec); err != nil {
		c.t.Fatalf("%s on %s refused: %v", verb, subject, err)
	}
	pos, err := c.store.Append(rec, ctx.Resolve)
	if err != nil {
		c.t.Fatal(err)
	}
	return pos
}

func (c *chain) fence(subject string) string {
	c.t.Helper()
	ctx, err := admit.ContextAt(c.store)
	if err != nil {
		c.t.Fatal(err)
	}
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Claim == nil {
		c.t.Fatalf("no claim on %s", subject)
	}
	return fmt.Sprintf("%d", s.Claim.Fence)
}

func enroll(t *testing.T, priv ed25519.PrivateKey, name string) string {
	t.Helper()
	return fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(priv.Public().(ed25519.PublicKey)), name)
}

// scenario builds the shared history: an upgrade, three enrolled
// lanes, one contract the worker claims, reserves against and
// submits. withExtra inserts a root grant of dispatch to the worker
// right after enrollment, the one-record insertion the frame_changed
// drill needs.
func scenario(t *testing.T, withExtra bool) *chain {
	t.Helper()
	c := newChain(t)
	root, worker, dispatcher, verifier := c.keys["root"], c.keys["worker"], c.keys["dispatcher"], c.keys["verifier"]
	c.admitted(root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for name, capability := range map[string]string{"worker": keyring.CapClaim, "dispatcher": keyring.CapDispatch, "verifier": keyring.CapVerdict} {
		c.admitted(root, keyring.VerbEnrolled, tFP(t, c.keys[name]), enroll(t, c.keys[name], name))
		c.admitted(root, keyring.VerbGranted, tFP(t, c.keys[name]), `{"capability": "`+capability+`"}`)
	}
	if withExtra {
		c.admitted(root, keyring.VerbGranted, tFP(t, worker), `{"capability": "`+keyring.CapDispatch+`"}`)
	}
	c.admitted(dispatcher, "intent.filed", "c-1", filed)
	c.admitted(dispatcher, "contract.specified", "c-1", spec)
	c.admitted(worker, "claim.taken", "c-1", `{}`)
	c.admitted(worker, transition.BudgetReserveVerb, "c-1", `{"amount": "2", "fence": "`+c.fence("c-1")+`"}`)
	c.admitted(worker, "submission.made", "c-1", `{"fence": "`+c.fence("c-1")+`", "packet": `+packet+`}`)
	_ = verifier
	return c
}

func shippedLanes(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "lanes"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// journalFor stamps refusals the way the CLI does: a refusal is
// stamped with the tip ordinal of the view that judged it, the last
// record's position.
func journalFor(entries ...refusals.Entry) *refusals.Journal {
	return &refusals.Journal{Entries: entries}
}

func refusedAt(pos int, actor, verb, subject, code string) refusals.Entry {
	return refusals.Entry{TS: "2026-09-01T02:00:00Z", Position: fmt.Sprintf("%d", pos), Actor: actor, Verb: verb, Subject: subject, Outcome: refusals.OutcomeRefused, Code: code}
}

// conformance: plans/os-6bd9ffff.md D1, AC1 — on a chain where the
// worker signs four records and journals two refusals, the trajectory
// has six points in position order; each frame equals the boundary's
// own derivation at the prefix (affordances from a store replayed to
// that prefix, state the fold's, owed the obligation rows); an
// admitted record is framed at records[:p] and a refusal at
// records[:p+1], so a refusal stamped at the last record sees the
// whole chain; lines beyond the tip or from another actor are skipped
// and counted; the digests equal the shipped files'; two recordings
// are byte-identical.
func TestRecordDerivesFramesAtThePrefixTheLaneSaw(t *testing.T) {
	c := scenario(t, false)
	worker, dispatcher := c.keys["worker"], c.keys["dispatcher"]
	records := c.records()
	last := len(records) - 1
	journal := journalFor(
		refusedAt(last, tFP(t, worker), "budget.reserve", "c-1", "fenced_out"),
		refusedAt(7, tFP(t, worker), "claim.parked", "c-1", "invalid_transition"),
		refusedAt(last, tFP(t, dispatcher), "claim.parked", "c-1", "out_of_grant"),
		refusedAt(last+1, tFP(t, worker), "claim.parked", "c-1", "invalid_transition"),
	)
	// An admitted line in the journal is the chain's business, never
	// a point of its own.
	journal.Entries = append(journal.Entries, refusals.Entry{TS: "2026-09-01T02:00:00Z", Position: "9", Actor: tFP(t, worker), Verb: "claim.taken", Subject: "c-1", Outcome: refusals.OutcomeAdmitted})

	traj, skipped, err := trajectory.Record(records, journal, worker, shippedLanes(t), "implementer")
	if err != nil {
		t.Fatal(err)
	}
	if skipped.OtherActor != 1 || skipped.BeyondTip != 1 {
		t.Fatalf("one line from another actor and one beyond the tip are skipped and counted: %+v", skipped)
	}
	var admitted, refused int
	for _, p := range traj.Points {
		if p.Outcome == trajectory.OutcomeAdmitted {
			admitted++
		} else {
			refused++
		}
	}
	if admitted != 3 || refused != 2 || len(traj.Points) != 5 {
		t.Fatalf("three admitted records and two refusals: %d + %d in %d points", admitted, refused, len(traj.Points))
	}
	for i := 1; i < len(traj.Points); i++ {
		a, b := traj.Points[i-1], traj.Points[i]
		if a.Position > b.Position || (a.Position == b.Position && a.Outcome == trajectory.OutcomeRefused && b.Outcome == trajectory.OutcomeAdmitted) {
			t.Fatalf("points are in position order, admitted before a refusal stamped alike: %+v then %+v", a, b)
		}
	}
	if traj.Actor != tFP(t, worker) || traj.Lane != "implementer" {
		t.Fatalf("the trajectory names the lane and the actor: %+v", traj)
	}
	var acts []string
	for _, p := range traj.Points {
		if p.Outcome == trajectory.OutcomeAdmitted {
			acts = append(acts, p.Act)
		}
	}
	if !slices.Equal(acts, []string{"claim take", "budget reserve", "submission make"}) {
		t.Fatalf("the loop-act spellings ride the points: %v", acts)
	}
	if traj.Points[0].Outcome != trajectory.OutcomeRefused || traj.Points[0].Position != 7 || traj.Points[0].Act != "claim park" {
		t.Fatalf("the refusal stamped at 7 precedes the claim at 10: %+v", traj.Points[0])
	}

	// Every frame is the boundary's own derivation at the prefix,
	// computed from a SECOND store replayed record by record: the
	// recorder's ContextOver and the boundary's ContextAt agree.
	replayed := t.TempDir()
	store2, err := ledger.Open(replayed)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := transition.Default()
	expectAt := func(prefix int, key ed25519.PrivateKey, subject string) trajectory.Frame {
		t.Helper()
		if prefix == 0 {
			return trajectory.Frame{Affordances: []string{}, Owed: []string{}}
		}
		ctx, err := admit.ContextAt(store2)
		if err != nil || ctx.Count != prefix {
			t.Fatalf("the second store must stand at %d: %d %v", prefix, ctx.Count, err)
		}
		f := trajectory.Frame{Affordances: admit.Affordances(ctx, key, subject), Owed: []string{}}
		if s, ok := table.FoldRecords(records[:prefix]).State(subject); ok {
			f.State = s.State
		}
		rows, err := project.DeriveObligations(records[:prefix])
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Subject == subject && row.OwedBy == tFP(t, key) && !slices.Contains(f.Owed, row.Kind) {
				f.Owed = append(f.Owed, row.Kind)
			}
		}
		return f
	}
	at := 0
	for _, p := range traj.Points {
		prefix := p.Position
		if p.Outcome == trajectory.OutcomeRefused {
			prefix = p.Position + 1
		}
		for at < prefix {
			if _, err := store2.Append(records[at], c.loose); err != nil {
				t.Fatal(err)
			}
			at++
		}
		want := expectAt(prefix, worker, p.Subject)
		if got, _ := json.Marshal(p.Frame); string(got) != string(mustJSON(t, want)) {
			t.Fatalf("point %+v: frame %s, the boundary's derivation at prefix %d is %s", p, got, prefix, mustJSON(t, want))
		}
	}
	// The refusal stamped at the last record saw the whole chain: its
	// frame reads the submitted state, which no admitted point's frame
	// (each before its own record) can.
	tail := traj.Points[len(traj.Points)-1]
	if tail.Outcome != trajectory.OutcomeRefused || tail.Position != last || tail.Frame.State != "review" {
		t.Fatalf("the refusal stamped at the last record sees the whole chain (review): %+v", tail)
	}
	if sub := traj.Points[len(traj.Points)-2]; sub.Verb != "submission.made" || sub.Frame.State != "in_progress" {
		t.Fatalf("the submission's own frame is the prefix before it (in_progress): %+v", sub)
	}

	// The digests are the shipped files'.
	raw, err := os.ReadFile(filepath.Join(shippedLanes(t), "implementer.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := lane.Load(shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	var man lane.Manifest
	for _, m := range ms {
		if m.Lane == "implementer" {
			man = m
		}
	}
	resolved, err := lane.Resolve(shippedLanes(t), man)
	if err != nil {
		t.Fatal(err)
	}
	if traj.Manifest != sha(raw) || traj.Posture != sha([]byte(resolved)) {
		t.Fatalf("the digests are the manifest bytes' and the resolved fragments': %s %s", traj.Manifest, traj.Posture)
	}

	// Determinism: two recordings are byte-identical, and the canonical
	// form parses back to itself.
	one, err := traj.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := trajectory.Record(records, journal, worker, shippedLanes(t), "implementer")
	if err != nil {
		t.Fatal(err)
	}
	two, err := again.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatal("two recordings of one chain are byte-identical")
	}
	parsed, err := trajectory.Parse(append(one, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	three, _ := parsed.Canonical()
	if string(three) != string(one) {
		t.Fatal("the canonical form round-trips through Parse")
	}
	if strings.Contains(string(one), `"ts"`) {
		t.Fatal("the frame carries no instant")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// conformance: D1 — the first record's frame is the empty chain: the
// root's trajectory, whose first point is the genesis at position 0,
// carries the empty frame there.
func TestFirstRecordFrameIsTheEmptyChain(t *testing.T) {
	c := scenario(t, false)
	traj, _, err := trajectory.Record(c.records(), nil, c.keys["root"], shippedLanes(t), "dispatcher")
	if err != nil {
		t.Fatal(err)
	}
	if len(traj.Points) == 0 || traj.Points[0].Position != 0 {
		t.Fatalf("the root signed the genesis at 0: %+v", traj.Points)
	}
	first := traj.Points[0].Frame
	if first.State != "" || len(first.Affordances) != 0 || len(first.Owed) != 0 {
		t.Fatalf("the genesis was decided from the empty chain: %+v", first)
	}
	if traj.Points[1].Frame.State != "" || len(traj.Points[1].Frame.Affordances) == 0 {
		t.Fatalf("the second point's frame is the one-record chain, where the root already acts: %+v", traj.Points[1].Frame)
	}
}

// copyLanes copies the shipped lanes directory so a drill can plant a
// change without touching the tree.
func copyLanes(t *testing.T) string {
	t.Helper()
	src := shippedLanes(t)
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// editManifest rewrites one field of a copied manifest through its
// JSON, so the file stays a manifest lane.Load accepts.
func editManifest(t *testing.T, dir, laneName string, edit func(m map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, laneName+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func classesOf(r *trajectory.Result) map[string]trajectory.Class {
	out := map[string]trajectory.Class{}
	for _, v := range r.Points {
		out[fmt.Sprintf("%s@%d", v.Verb, v.Position)] = v.Class
	}
	return out
}

// conformance: D2, AC2 — replay over the unchanged configuration and
// chain classifies every point same; a manifest whose acts_through
// drops the recorded act yields act_undeclared; grants dropped,
// act_ungranted; a record inserted before a point, frame_changed; a
// fragment edited, posture_changed with every point same; a manifest
// whose orients_from alone changed, manifest_changed with every point
// same; an admitted point whose verb the recomputed affordances lack,
// act_inadmissible.
func TestReplayClassifiesEveryDivergence(t *testing.T) {
	c := scenario(t, false)
	worker := c.keys["worker"]
	records := c.records()
	journal := journalFor(refusedAt(len(records)-1, tFP(t, worker), "budget.reserve", "c-1", "fenced_out"))
	traj, _, err := trajectory.Record(records, journal, worker, shippedLanes(t), "implementer")
	if err != nil {
		t.Fatal(err)
	}

	res, err := trajectory.Replay(traj, records, worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.Diverged() || len(res.Divergent()) != 0 || len(res.Points) != len(traj.Points) {
		t.Fatalf("unchanged configuration and chain: every point same, nothing diverged: %+v", res)
	}

	// acts_through without "submission make": the submission point is
	// undeclared, the rest same.
	dir := copyLanes(t)
	editManifest(t, dir, "implementer", func(m map[string]any) {
		m["acts_through"] = []any{"claim take", "budget reserve", "budget settle", "budget release", "claim release", "claim park"}
		m["liveness_from"] = []any{"claim take", "budget settle"}
	})
	res, err = trajectory.Replay(traj, records, worker, dir)
	if err != nil {
		t.Fatal(err)
	}
	classes := classesOf(res)
	if classes["submission.made@"+fmt.Sprint(traj.Points[2].Position)] != trajectory.ActUndeclared || classes["claim.taken@"+fmt.Sprint(traj.Points[0].Position)] != trajectory.Same {
		t.Fatalf("dropping the act from acts_through is act_undeclared for that point alone: %v", classes)
	}
	if !res.ManifestChanged || !res.Diverged() {
		t.Fatal("an edited manifest also changes the manifest digest")
	}

	// Grants dropped: every admitted point with a capability row is
	// ungranted; the refused point is judged by its frame alone.
	dir = copyLanes(t)
	editManifest(t, dir, "implementer", func(m map[string]any) { m["grants"] = []any{} })
	res, err = trajectory.Replay(traj, records, worker, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Points {
		want := trajectory.ActUngranted
		if v.Outcome == trajectory.OutcomeRefused {
			want = trajectory.Same
		}
		if v.Class != want {
			t.Fatalf("with no grants every admitted claim-lane act is ungranted and a refusal keeps its frame: %+v", v)
		}
	}

	// orients_from alone: manifest_changed, every point same.
	dir = copyLanes(t)
	editManifest(t, dir, "implementer", func(m map[string]any) {
		m["orients_from"] = "seed situation --ledger <dir> --key <key> --since <position>"
	})
	res, err = trajectory.Replay(traj, records, worker, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ManifestChanged || res.PostureChanged || len(res.Divergent()) != 0 || !res.Diverged() {
		t.Fatalf("an orients_from edit is manifest_changed with every point same: %+v", res)
	}

	// A fragment with one added line: posture_changed, every point same.
	dir = copyLanes(t)
	frag := filepath.Join(dir, lane.FragmentDir, "lane", "implementer.md")
	b, err := os.ReadFile(frag)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frag, append(b, "\nOne more line of role prose.\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = trajectory.Replay(traj, records, worker, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.PostureChanged || res.ManifestChanged || len(res.Divergent()) != 0 || !res.Diverged() {
		t.Fatalf("a fragment edit is posture_changed with every point same: %+v", res)
	}

	// A record inserted before the points: the chain with one extra
	// grant after enrollment, so every later frame's affordances differ.
	other := scenario(t, true)
	res, err = trajectory.Replay(traj, other.records(), worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Points {
		if v.Class != trajectory.FrameChanged || !strings.Contains(v.Detail, "affordances") {
			t.Fatalf("a record inserted before a point changes its frame: %+v", v)
		}
	}
	// A chain too short for a point is a changed frame too, saying so.
	res, err = trajectory.Replay(traj, records[:traj.Points[1].Position], worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := res.Points[len(res.Points)-1]; v.Class != trajectory.FrameChanged || !strings.Contains(v.Detail, "the chain ends") {
		t.Fatalf("a point beyond the chain's end cannot be framed: %+v", v)
	}
	// A refused point at the largest position a parse admits: the
	// bound is judged on the position, never on position+1, so the
	// sum cannot overflow past the guard into the slice (review
	// finding on the task PR).
	hostile := *traj
	hostile.Points = append(append([]trajectory.Point{}, traj.Points...),
		trajectory.Point{Position: math.MaxInt, Verb: "claim.taken", Subject: "c-1", Outcome: trajectory.OutcomeRefused})
	res, err = trajectory.Replay(&hostile, records, worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := res.Points[len(res.Points)-1]; v.Class != trajectory.FrameChanged || !strings.Contains(v.Detail, "the chain ends") {
		t.Fatalf("a refused point past the chain's end is a changed frame, never a panic: %+v", v)
	}
	// And a refused point exactly at the tip needs one record more
	// than the chain holds.
	atTip := *traj
	atTip.Points = append(append([]trajectory.Point{}, traj.Points...),
		trajectory.Point{Position: len(records), Verb: "claim.taken", Subject: "c-1", Outcome: trajectory.OutcomeRefused})
	if res, err := trajectory.Replay(&atTip, records, worker, shippedLanes(t)); err != nil || res.Points[len(res.Points)-1].Class != trajectory.FrameChanged {
		t.Fatalf("a refused point at the tip has no stamp record to frame from: %v %+v", err, res)
	}

	// act_inadmissible: a record the boundary would not have afforded
	// (a release of a submitted contract), past it on the raw seam by
	// a key the manifest does grant the verb to. Its recorded frame
	// lacks the verb, so replay finds the frame unchanged and the act
	// inadmissible rather than ungranted.
	c.raw(worker, "claim.released", "c-1", `{"packet": `+packet+`}`)
	records = c.records()
	traj, _, err = trajectory.Record(records, nil, worker, shippedLanes(t), "implementer")
	if err != nil {
		t.Fatal(err)
	}
	rawPoint := traj.Points[len(traj.Points)-1]
	if rawPoint.Verb != "claim.released" || slices.Contains(rawPoint.Frame.Affordances, "claim.released") {
		t.Fatalf("the raw record is recorded at a frame that does not afford it: %+v", rawPoint)
	}
	res, err = trajectory.Replay(traj, records, worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	if classes := classesOf(res); classes["claim.released@"+fmt.Sprint(rawPoint.Position)] != trajectory.ActInadmissible {
		t.Fatalf("an admitted point the affordances lack is act_inadmissible: %v", classes)
	}
	if !res.Diverged() {
		t.Fatal("act_inadmissible diverges")
	}
	// A raw record outside the manifest's grants is ungranted first:
	// the classes are ordered, and a point is classified exactly once.
	c.raw(worker, "intent.filed", "c-9", filed)
	records = c.records()
	traj, _, err = trajectory.Record(records, nil, worker, shippedLanes(t), "implementer")
	if err != nil {
		t.Fatal(err)
	}
	res, err = trajectory.Replay(traj, records, worker, shippedLanes(t))
	if err != nil {
		t.Fatal(err)
	}
	if classes := classesOf(res); classes["intent.filed@"+fmt.Sprint(len(records)-1)] != trajectory.ActUngranted {
		t.Fatalf("a raw act outside the grants is ungranted before it is inadmissible: %v", classes)
	}
}

// conformance: D2 — a lane replays its own trajectory with its own
// key: another key refuses rather than framing another lane's
// affordances as this one's; and a configuration-only trajectory (a
// lane that signed nothing) still diverges on its digests.
func TestReplayNeedsTheRecordedActorsKeyAndJudgesConfigurationAlone(t *testing.T) {
	c := scenario(t, false)
	records := c.records()
	traj, _, err := trajectory.Record(records, nil, c.keys["verifier"], shippedLanes(t), "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if len(traj.Points) != 0 {
		t.Fatalf("the verifier signed nothing: a configuration-only trajectory: %+v", traj.Points)
	}
	if _, err := trajectory.Replay(traj, records, c.keys["worker"], shippedLanes(t)); err == nil || !strings.Contains(err.Error(), "its own key") {
		t.Fatalf("another key refuses: %v", err)
	}
	res, err := trajectory.Replay(traj, records, c.keys["verifier"], shippedLanes(t))
	if err != nil || res.Diverged() {
		t.Fatalf("unchanged: green, %v %+v", err, res)
	}
	dir := copyLanes(t)
	editManifest(t, dir, "verifier", func(m map[string]any) { m["summary"] = "changed" })
	res, err = trajectory.Replay(traj, records, c.keys["verifier"], dir)
	if err != nil || !res.ManifestChanged || !res.Diverged() {
		t.Fatalf("a configuration-only trajectory diverges on its manifest digest: %v %+v", err, res)
	}
	if _, _, err := trajectory.Record(records, nil, c.keys["verifier"], shippedLanes(t), "nobody"); err == nil {
		t.Fatal("an unknown lane has no configuration to record under")
	}
}

// conformance: the committed form is a declared input, so Parse is
// strict: unknown fields, malformed digests and unknown outcomes
// refuse rather than replaying an empty or partial trajectory.
func TestParseIsStrict(t *testing.T) {
	good := `{"lane": "implementer", "actor": "aa", "manifest": "` + strings.Repeat("0", 64) + `", "posture": "` + strings.Repeat("1", 64) + `", "points": []}`
	if _, err := trajectory.Parse([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"lane": "implementer", "actor": "aa", "manifest": "x", "posture": "` + strings.Repeat("1", 64) + `", "points": []}`,
		`{"lane": "", "actor": "aa", "manifest": "` + strings.Repeat("0", 64) + `", "posture": "` + strings.Repeat("1", 64) + `", "points": []}`,
		`{"lane": "implementer", "actor": "aa", "manifest": "` + strings.Repeat("0", 64) + `", "posture": "` + strings.Repeat("1", 64) + `", "points": [], "extra": 1}`,
		`{"lane": "implementer", "actor": "aa", "manifest": "` + strings.Repeat("0", 64) + `", "posture": "` + strings.Repeat("1", 64) + `", "points": [{"position": 1, "verb": "x", "subject": "s", "outcome": "maybe", "frame": {"state": "", "affordances": [], "owed": []}}]}`,
		`{"lane": "implementer", "actor": "aa", "manifest": "` + strings.Repeat("0", 64) + `", "posture": "` + strings.Repeat("1", 64) + `", "points": [{"position": -1, "verb": "x", "subject": "s", "outcome": "admitted", "frame": {"state": "", "affordances": [], "owed": []}}]}`,
		good + `{}`,
	} {
		if _, err := trajectory.Parse([]byte(bad)); err == nil {
			t.Fatalf("must refuse: %s", bad)
		}
	}
	if _, err := trajectory.LoadConfiguration(shippedLanes(t), "implementer"); err != nil {
		t.Fatal(err)
	}
}
