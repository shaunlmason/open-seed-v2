package admit_test

// The admission random walk (plans/os-21bf939f.md; charter III.I row 2
// and the §I.2 ceiling as it lands in admission): the regression-class
// sweep checks listed-implies-admits at every prefix of one hand-written
// walk, and every generator in the tree walks a scripted shape, so rule
// interactions at positions no script reaches were tested only where
// someone thought of the case. This walker explores. From the shared
// scenario it draws a lane, a subject and a move at every position: a
// listed verb, re-drafted independently and run through the enforcing
// Check, or a verb the transition table forbids from the subject's
// state, drafted by a lane that holds its capability. Four oracles per
// step (plan D3), the five-bar audit at the end of every walk, and a
// coverage map that names how the walk holds every prohibition clause
// of the ceiling table (D4). Prefix truncation is the shrinker (D5): a
// failure is positioned, so the step script up to the failing position
// is the minimal reproduction, and the seed replays it byte for byte.
//
// Sizes (D6): the fast gate runs -walks=8 -steps=24, measured at about
// five seconds locally (8 by 48 measured eleven, so the steps were
// halved first, as the plan orders); the weekly schedule runs
// -walks=200 -steps=96 (.github/workflows/perf-scale.yml), about three
// seconds a walk.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"math/rand/v2"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/redteam"
	"github.com/shaunlmason/open-seed-v2/internal/simulate"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

var (
	walkCount = flag.Int("walks", 8, "random walks per run (plans/os-21bf939f.md D6)")
	walkSteps = flag.Int("steps", 24, "draws per walk")
	walkSeed  = flag.Int64("seed", 20260906, "base seed; walk w runs on seed*1000+w")
)

const (
	// One draw in freshOdds opens a fresh contract id; one draw in
	// forbiddenOdds is a forbidden move (D2).
	freshOdds     = 8
	forbiddenOdds = 4
	// A walk that admits nothing in stallLimit consecutive draws is a
	// broken instrument, not a quiet chain (D2).
	stallLimit = 32
	// The walker's own lanes, enrolled by root in the preamble: the
	// scenario revokes its holder before the walk starts, and two
	// claim-capable keys put contention in reach.
	walkerA, walkerB = "walker-a", "walker-b"
)

// ceilingCoverage names how the walk holds each prohibition clause of
// the ceiling table (D4). The test asserts the map and the table agree
// both ways, so a clause added to the table fails here until someone
// says how the walk holds it.
var ceilingCoverage = map[string]string{
	"claim":       "predicate: no claim.taken admitted while the subject held an active claim",
	"approve":     "predicate: every merge.requested cites a verdict signed by a verdict-granted key that did not sign the submission",
	"gates":       "predicate: every operator-only verb is signed by the root key",
	"transition":  "oracle: a forbidden move refuses with the lifecycle rule's error; the audit's chain-violations bar",
	"spend":       "audit: the unreserved-spend bar",
	"lease":       "audit: the silent-abandonment bar, after the walker's own deliberate exits",
	"rewrite":     "structural: the records are held in memory and never rewritten; the hook drill's",
	"impersonate": "structural: every draft is signed by the key it names; the hook drill's",
}

func TestAdmissionRandomWalk(t *testing.T) {
	ceiling, err := redteam.LoadCeiling("../redteam/testdata/ceiling.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, cl := range ceiling.Clauses {
		if cl.Kind == redteam.Prohibition {
			if _, named := ceilingCoverage[cl.ID]; !named {
				t.Errorf("ceiling clause %q is a prohibition the walk's coverage map does not name", cl.ID)
			}
		}
	}
	for id := range ceilingCoverage {
		if _, known := ceiling.Clause(id); !known {
			t.Errorf("the coverage map names %q, which the ceiling table does not carry", id)
		}
	}

	records, keys := admit.WalkScenario(t)
	if audit := simulate.Audit(records); !audit.Clean {
		t.Fatalf("the shared scenario itself fails the five-bar audit: %+v", audit)
	}
	keys[walkerA] = admit.FixtureKey(t, 7)
	keys[walkerB] = admit.FixtureKey(t, 8)

	// reached is summed over the run: the transition oracle must be
	// exercised somewhere in it, and at the fast size one walk's
	// handful of forbidden draws can all land on the fence and
	// escalation rules by chance.
	reached := 0
	for w := 0; w < *walkCount; w++ {
		seed := *walkSeed*1000 + int64(w)
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			reached += newWalker(t, seed, records, keys).run(*walkSteps)
		})
	}
	if reached == 0 {
		t.Fatalf("no forbidden draw in %d walks reached the lifecycle rule; the transition oracle was not exercised", *walkCount)
	}
}

type walker struct {
	t       *testing.T
	seed    int64
	rng     *rand.Rand
	records []*event.Record
	ctx     *admit.Context
	table   *transition.Table
	keys    map[string]ed25519.PrivateKey
	lanes   []string
	fps     map[string]string
	byFP    map[string]string
	clock   time.Time
	script  []string
	fresh   int
	stall   int
	// reached counts the forbidden draws whose refusal came from the
	// lifecycle rule: a walk that never gets there has not exercised
	// the transition oracle.
	reached int
}

func newWalker(t *testing.T, seed int64, scenario []*event.Record, keys map[string]ed25519.PrivateKey) *walker {
	t.Helper()
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	// The scenario slice is shared across walks: cap it so the first
	// append copies rather than writing into a neighbour's tail.
	records := scenario[:len(scenario):len(scenario)]
	ctx, err := admit.ContextOver(records)
	if err != nil {
		t.Fatal(err)
	}
	last, err := time.Parse(time.RFC3339, records[len(records)-1].Event.TS)
	if err != nil {
		t.Fatalf("the scenario's last timestamp: %v", err)
	}
	w := &walker{
		t: t, seed: seed, rng: rand.New(rand.NewPCG(uint64(seed), uint64(seed)^0x9e3779b97f4a7c15)),
		records: records, ctx: ctx, table: table, keys: keys,
		fps: map[string]string{}, byFP: map[string]string{}, clock: last,
	}
	for name, key := range keys {
		fp := admit.FingerprintOf(t, key)
		w.fps[name] = fp
		w.byFP[fp] = name
	}
	// The playing lanes: root, the scenario's still-standing lanes, and
	// the walker's two holders. The scenario's revoked holder stays out
	// of the draw; a revoked key lists nothing and would only cost draws.
	w.lanes = []string{"root", "supervisor", "verifier", "sealer", "observer", walkerA, walkerB}
	return w
}

func (w *walker) fail(format string, args ...any) {
	w.t.Helper()
	w.t.Fatalf("seed %d at position %d: %s\nreplay: go test ./internal/admit -run TestAdmissionRandomWalk -args -walks=1 -seed=%d -steps=%d\nscript:\n%s",
		w.seed, len(w.records), fmt.Sprintf(format, args...), w.seed/1000, *walkSteps, strings.Join(w.script, "\n"))
}

func (w *walker) note(format string, args ...any) {
	w.script = append(w.script, fmt.Sprintf("%4d  ", len(w.records))+fmt.Sprintf(format, args...))
}

func (w *walker) state(subject string) string {
	if s, ok := w.ctx.Lifecycle.State(subject); ok {
		return s.State
	}
	return ""
}

func (w *walker) tick() time.Time {
	w.clock = w.clock.Add(time.Second)
	return w.clock
}

// admit runs the enforcing Check and, on admission, appends the record
// and rebuilds the context from the records.
func (w *walker) admit(rec *event.Record) error {
	if err := admit.Check(w.ctx, rec); err != nil {
		return err
	}
	w.records = append(w.records, rec)
	ctx, err := admit.ContextOver(w.records)
	if err != nil {
		w.fail("rebuilding the context after %s on %s: %v", rec.Event.Verb, rec.Event.Subject, err)
	}
	w.ctx = ctx
	return nil
}

func (w *walker) sign(lane, verb, subject, payload string) *event.Record {
	w.t.Helper()
	rec, err := event.Sign(event.Event{
		V: w.ctx.Active, TS: w.tick().UTC().Format(time.RFC3339), Actor: w.fps[lane],
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: w.ctx.Tip,
	}, w.keys[lane])
	if err != nil {
		w.t.Fatal(err)
	}
	return rec
}

// run plays the walk and returns how many forbidden draws reached the
// lifecycle rule.
func (w *walker) run(steps int) int {
	w.t.Helper()
	// Preamble: root enrolls and grants the walker's two holders.
	for _, name := range []string{walkerA, walkerB} {
		for _, step := range []struct{ verb, body string }{
			{keyring.VerbEnrolled, admit.EnrollBody(w.t, w.keys[name], "agent", name)},
			{keyring.VerbGranted, `{"capability": "` + keyring.CapClaim + `"}`},
		} {
			if err := w.admit(w.sign("root", step.verb, w.fps[name], step.body)); err != nil {
				w.fail("preamble: %s for %s refused: %v", step.verb, name, err)
			}
		}
	}
	for i := 0; i < steps; i++ {
		subject := w.drawSubject()
		lane := w.lanes[w.rng.IntN(len(w.lanes))]
		if w.ctx.Halt.Halted {
			// Under halt only root can act (the lift), so the draw
			// goes to root rather than spending the stall budget on
			// lanes the boundary refuses by design.
			lane = "root"
		}
		if w.rng.IntN(forbiddenOdds) == 0 && !w.ctx.Halt.Halted && subject != "system" {
			w.forbiddenMove(subject)
			continue
		}
		w.listedMove(lane, subject)
	}
	w.epilogue()
	if audit := simulate.Audit(w.records); !audit.Clean {
		w.fail("five-bar audit red: chain %v, lost %v, abandoned %v, guardrails %v, unreserved %v",
			audit.ChainViolations, audit.LostUpdates, audit.SilentAbandonments, audit.GuardrailBreaches, audit.UnreservedSpend)
	}
	w.ceilingPredicates()
	return w.reached
}

func (w *walker) drawSubject() string {
	if w.rng.IntN(freshOdds) == 0 {
		w.fresh++
		return fmt.Sprintf("w-%d", w.fresh)
	}
	subjects := append(w.ctx.Lifecycle.Subjects(), "system")
	return subjects[w.rng.IntN(len(subjects))]
}

// listedMove is the III.I oracle over a random position: whatever the
// affordance computation lists for the lane on the subject must admit
// when re-drafted independently, the listing must be deterministic,
// and the fold must land where the table says (D3 a, c, d).
func (w *walker) listedMove(lane, subject string) {
	w.t.Helper()
	key := w.keys[lane]
	listed := admit.Affordances(w.ctx, key, subject)
	if again := admit.Affordances(w.ctx, key, subject); !slices.Equal(listed, again) {
		w.fail("III.I class: nondeterministic affordances for %s on %s: %v vs %v", lane, subject, listed, again)
	}
	listed = slices.DeleteFunc(slices.Clone(listed), func(v string) bool { return v == ledger.UpgradeVerb })
	if len(listed) == 0 {
		w.stall++
		if w.stall >= stallLimit {
			w.fail("the walk admitted nothing in %d consecutive draws (last: %s on %s)", stallLimit, lane, subject)
		}
		return
	}
	verb := listed[w.rng.IntN(len(listed))]
	rec, ok := admit.Redraft(w.t, w.ctx, key, subject, verb, w.tick())
	if !ok {
		w.fail("III.I class: %s listed for %s on %s but absent from the catalog", verb, lane, subject)
	}
	on := rec.Event.Subject
	before := w.state(on)
	if err := w.admit(rec); err != nil {
		w.note("listed    %-10s %-30s %-8s REFUSED", lane, verb, on)
		w.fail("III.I regression class: %s listed for %s on %s but refused at admission: %v", verb, lane, on, err)
	}
	w.note("listed    %-10s %-30s %-8s admitted (%q -> %q)", lane, verb, on, before, w.state(on))
	w.stall = 0
	after := w.state(on)
	if w.table.IsLifecycleVerb(verb) {
		to, err := w.table.Check(on, before, verb)
		if err != nil {
			w.fail("fold class: %s admitted on %s from %q but the table refuses it: %v", verb, on, before, err)
		}
		if after != to {
			w.fail("fold class: %s on %s from %q folded to %q, the table says %q", verb, on, before, after, to)
		}
	} else if after != before {
		w.fail("fold class: %s is not a lifecycle verb but moved %s from %q to %q", verb, on, before, after)
	}
}

// forbiddenMove is the transition oracle (D3 b): a lifecycle verb the
// table forbids from the subject's state, drafted by a lane that holds
// its capability, must refuse, and the refusal must be the lifecycle
// rule's own, or one of the verb-scoped rules the rule set runs ahead
// of it, each enumerated here with the ground it refuses on: the fence
// rule on a draft citing a fence where none is active, the escalation
// rule on its two verbs, a second question while one stands or an
// answer where none does (recorded in docs/decisions.md). Any other refusal is a refusal-ordering
// finding, named as the class so a human classifies it.
func (w *walker) forbiddenMove(subject string) {
	w.t.Helper()
	current := w.state(subject)
	type candidate struct{ lane, verb string }
	var candidates []candidate
	verbs := append(w.table.Verbs(), w.table.BirthVerb())
	sort.Strings(verbs)
	verbs = slices.Compact(verbs)
	for _, verb := range verbs {
		if verb == ledger.UpgradeVerb {
			continue
		}
		if _, err := w.table.Check(subject, current, verb); err == nil {
			continue
		}
		accepted := keyring.AcceptedCapabilities(verb)
		for _, lane := range w.lanes {
			if w.ctx.Keyring.HasAnyCapability(w.fps[lane], accepted) {
				candidates = append(candidates, candidate{lane, verb})
			}
		}
	}
	if len(candidates) == 0 {
		return
	}
	c := candidates[w.rng.IntN(len(candidates))]
	rec, ok := admit.Redraft(w.t, w.ctx, w.keys[c.lane], subject, c.verb, w.tick())
	if !ok {
		w.fail("transition class: %s is a table verb the catalog does not draft", c.verb)
	}
	if rec.Event.Subject != subject {
		return
	}
	err := admit.Check(w.ctx, rec)
	w.note("forbidden %-10s %-30s %-8s from %q: %v", c.lane, c.verb, subject, current, err)
	if err == nil {
		w.fail("transition class: %s by %s admitted on %s from state %q, which the table forbids", c.verb, c.lane, subject, current)
	}
	var invalid *transition.InvalidTransitionError
	var contention *admit.ContentionError
	var fence *admit.FenceError
	var escalation *admit.EscalationError
	switch {
	case errors.As(err, &invalid):
		w.reached++
	case errors.As(err, &contention) && w.table.Exclusive(c.verb):
		// The lifecycle rule's own refusal of a second claim: the
		// ceiling's claim clause, at the same rule.
		w.reached++
	case errors.As(err, &fence) && fence.Active < 0 && citesFence(rec):
		// The fence rule ahead of the lifecycle rule, on a draft that
		// cites a fence where none is active.
	case errors.As(err, &escalation) && (c.verb == "escalation.raised" || c.verb == "decision.recorded"):
		// The escalation rule ahead of the lifecycle rule, on the two
		// verbs it scopes: a second question while one stands, or an
		// answer where no question stands, is refused as such.
	default:
		w.fail("transition class: %s by %s on %s from %q refused by another rule first: %v", c.verb, c.lane, subject, current, err)
	}
}

func citesFence(rec *event.Record) bool {
	var p map[string]json.RawMessage
	if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
		return false
	}
	_, has := p["fence"]
	return has
}

// epilogue closes what the walk left open, through the same listed
// path as every other step: a halt is lifted, and every active claim is
// released by its holder. The audit's abandonment bar then judges the
// walk's exits, all of them deliberate.
func (w *walker) epilogue() {
	w.t.Helper()
	if w.ctx.Halt.Halted {
		rec, _ := admit.Redraft(w.t, w.ctx, w.keys["root"], "system", halt.LiftVerb, w.tick())
		if err := w.admit(rec); err != nil {
			w.fail("epilogue: lifting the halt refused: %v", err)
		}
		w.note("epilogue  root       %-30s system   admitted", halt.LiftVerb)
	}
	for _, subject := range w.ctx.Lifecycle.Subjects() {
		s, _ := w.ctx.Lifecycle.State(subject)
		if s.Claim == nil {
			continue
		}
		lane, known := w.byFP[s.Claim.Holder]
		if !known {
			w.fail("epilogue: %s is held by %s, a key the walk does not know", subject, s.Claim.Holder)
		}
		if listed := admit.Affordances(w.ctx, w.keys[lane], subject); !slices.Contains(listed, "claim.released") {
			w.fail("III.I class: %s holds %s at fence %d but claim.released is not listed: %v", lane, subject, s.Claim.Fence, listed)
		}
		rec, _ := admit.Redraft(w.t, w.ctx, w.keys[lane], subject, "claim.released", w.tick())
		if err := w.admit(rec); err != nil {
			w.fail("III.I regression class: claim.released listed for %s on %s but refused at admission: %v", lane, subject, err)
		}
		w.note("epilogue  %-10s %-30s %-8s admitted", lane, "claim.released", subject)
	}
}

// ceilingPredicates are the D4 clauses the audit does not carry, read
// over the final records.
func (w *walker) ceilingPredicates() {
	w.t.Helper()
	root := w.fps["root"]
	for i, rec := range w.records {
		e := rec.Event
		// gates: an operator-only verb signed by any key but root.
		if accepted := keyring.AcceptedCapabilities(e.Verb); slices.Equal(accepted, []string{keyring.CapOperator}) && e.Actor != root {
			w.fail("ceiling gates: position %d %s on %s signed by %s, not root", i, e.Verb, e.Subject, w.byFP[e.Actor])
		}
		switch e.Verb {
		case "claim.taken":
			// claim: no claim.taken while the subject held an active claim.
			if s, ok := w.table.FoldRecords(w.records[:i]).State(e.Subject); ok && s.Claim != nil {
				w.fail("ceiling claim: position %d claim.taken on %s while %s held fence %d", i, e.Subject, w.byFP[s.Claim.Holder], s.Claim.Fence)
			}
		case transition.MergeRequestedVerb:
			// approve: the cited verdict is a verdict-granted key's,
			// and not the submission signer's.
			var p struct {
				Verdict string `json:"verdict"`
			}
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				w.fail("ceiling approve: position %d merge.requested payload: %v", i, err)
			}
			vp, err := strconv.Atoi(p.Verdict)
			if err != nil || vp < 0 || vp >= i {
				w.fail("ceiling approve: position %d merge.requested cites verdict %q", i, p.Verdict)
			}
			verdict := w.records[vp].Event
			if verdict.Verb != transition.VerdictRenderedVerb || verdict.Subject != e.Subject {
				w.fail("ceiling approve: position %d merge.requested on %s cites position %d, which is %s on %s", i, e.Subject, vp, verdict.Verb, verdict.Subject)
			}
			ring, _, err := keyring.StateAt(w.records[:vp+1])
			if err != nil {
				w.fail("ceiling approve: keyring at position %d: %v", vp, err)
			}
			if !ring.HasAnyCapability(verdict.Actor, []string{keyring.CapVerdict}) {
				w.fail("ceiling approve: the verdict at position %d is signed by %s, which held no verdict grant there", vp, w.byFP[verdict.Actor])
			}
			var vpay struct {
				Submission string `json:"submission"`
			}
			if err := json.Unmarshal(verdict.Payload, &vpay); err != nil {
				w.fail("ceiling approve: verdict payload at position %d: %v", vp, err)
			}
			sp, err := strconv.Atoi(vpay.Submission)
			if err != nil || sp < 0 || sp >= vp {
				w.fail("ceiling approve: the verdict at position %d cites submission %q", vp, vpay.Submission)
			}
			if w.records[sp].Event.Actor == verdict.Actor {
				w.fail("ceiling approve: the verdict at position %d and the submission at %d share a signer, %s", vp, sp, w.byFP[verdict.Actor])
			}
		}
	}
}
