package admit

// The expiry, retirement and dead-end drills (plans/os-0d537fbd.md
// AC1, AC2, AC3): every admit and refuse row of D1, D2 and D3 at the
// boundary, and the fold, the surfacing set and the affordance list
// behind them.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

const (
	retryPath    = curation.LessonsDir + "/retry.md"
	retryAnchor2 = retryPath + " @ 1234567"
)

func lists(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// conformance: AC1 — expiry is derived at an instant (at or past
// expires), and revalidation is a re-promotion of the same path that
// admits only with last_validated moved forward and then stands as the
// path's latest promotion.
func TestExpiryAndRevalidation(t *testing.T) {
	st := curationFixture(t)
	hp, pp, _ := promoted(t, st)
	fold := curation.Fold(st.ctx.Records)
	if l, ok := fold.Lessons[retryPath]; !ok || l.Pos != pp {
		t.Fatalf("the promotion stands as the path's latest: %+v", fold.Lessons)
	}
	expires := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		at       time.Time
		surfaces bool
	}{
		{expires.Add(-time.Second), true},
		{expires, false},
		{expires.Add(time.Hour), false},
	} {
		got := len(curation.CandidatesAt(fold, st.ctx.Lifecycle, "c-4", row.at)) == 1
		if got != row.surfaces {
			t.Fatalf("at %s the lesson surfaces=%v, want %v", row.at.Format(time.RFC3339), got, row.surfaces)
		}
		if curation.Expired(fold.Lessons[retryPath], row.at) == row.surfaces {
			t.Fatalf("at %s expired disagrees with the surfacing set", row.at.Format(time.RFC3339))
		}
	}
	if len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 1 {
		t.Fatal("the instant-free candidates read every unretired promotion")
	}
	if !lists(Affordances(st.ctx, st.observer, st.id), curation.RetireVerb) {
		t.Fatal("a standing promotion makes the retirement an affordance for the observer")
	}
	// The stamps unmoved, or moved backwards: refused naming both.
	for i, stale := range []string{"2026-09-01T00:00:00Z", "2026-08-01T00:00:00Z"} {
		err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, revalidation(t, st, hp, fmt.Sprintf("eval-stale-%d", i), stale, "2027-03-01T00:00:00Z"), st.ctx.Tip))
		if gate(t, err) != curation.GatePromotionOrder || !strings.Contains(err.Error(), fmt.Sprintf("position %d", pp)) || !strings.Contains(err.Error(), stale) {
			t.Fatalf("a re-promotion whose last_validated is %s refuses at %s naming both: %v", stale, curation.GatePromotionOrder, err)
		}
	}
	fresh := revalidation(t, st, hp, "eval-fresh", "2026-12-15T00:00:00Z", "2027-03-01T00:00:00Z")
	if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, fresh, st.ctx.Tip)); err != nil {
		t.Fatalf("a revalidation with the stamps moved forward admits: %v", err)
	}
	rp := st.ctx.Count
	st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, fresh)
	fold = curation.Fold(st.ctx.Records)
	if l := fold.Lessons[retryPath]; l.Pos != rp || l.LastValidated != "2026-12-15T00:00:00Z" || l.Lesson != retryAnchor2 {
		t.Fatalf("the revalidation is the path's latest promotion: %+v", l)
	}
	if got := fold.LessonsOf(st.id); len(got) != 1 || got[0].Pos != rp {
		t.Fatalf("one promotion per path stands in the fold: %+v", got)
	}
	if len(curation.CandidatesAt(fold, st.ctx.Lifecycle, "c-4", expires.Add(time.Hour))) != 1 {
		t.Fatal("the revalidated lesson surfaces past the old expiry")
	}
	if len(curation.CandidatesAt(fold, st.ctx.Lifecycle, "c-4", time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))) != 0 {
		t.Fatal("the revalidated lesson expires at its own stamp")
	}
	// The latest before a position is position-accurate: before the
	// revalidation the first promotion stood.
	if prev, ok := curation.LatestPromotionBefore(st.ctx.Records, st.ctx.Table, retryAnchor2, rp); !ok || prev.Pos != pp {
		t.Fatalf("the latest promotion before %d is %d: %+v %v", rp, pp, prev, ok)
	}
	if prev, ok := curation.LatestPromotionBefore(st.ctx.Records, st.ctx.Table, retryAnchor2, rp+1); !ok || prev.Pos != rp {
		t.Fatalf("the latest promotion after the revalidation is the revalidation: %+v %v", prev, ok)
	}
}

// conformance: AC2 — the retirement rows: the three reasons admit from
// the observer with exactly their field; every other pairing, an
// unknown key or reason, a citation that is not the latest admitted
// promotion, a superseded_by that is no later promotion, and a curate
// key refuse; a retired lesson never surfaces while its file,
// hypothesis and observations stay in the fold; a second retirement
// refuses; a new promotion of the path brings it back.
func TestRetirementRows(t *testing.T) {
	st := curationFixture(t)
	hp, pp, anchor := promoted(t, st)
	hyp := cite(st.id, hp)
	// A second path, promoted, for the superseded rows.
	other := "retry twice"
	otherID := curation.HypothesisID(other, nil)
	op := st.ctx.Count
	st.ctx = st.step(st.curator, st.v, curation.HypothesisVerb, otherID, st.proposalWith(other, appliesCore, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	otherAnchor := curation.LessonsDir + "/retry-twice.md @ 0123456"
	otherBound := st.evalRun(t, "eval-other", cite(otherID, op), otherAnchor, "pass", nil)
	otherBody := strings.Replace(lessonBody(otherID, op, "fix-the-check", otherBound), "/retry.md @ 0123456", "/retry-twice.md @ 0123456", 1)
	if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, otherID, otherBody, st.ctx.Tip)); err != nil {
		t.Fatalf("the second path promotes: %v", err)
	}
	opp := st.ctx.Count
	st.ctx = st.step(st.observer, st.v, curation.LessonVerb, otherID, otherBody)

	try := func(key ed25519.PrivateKey, body string) error {
		return Check(st.ctx, draftV(t, key, st.v, curation.RetireVerb, st.id, body, st.ctx.Tip))
	}
	pr := `, "pr": "pr/10 @ 89abcde"`
	by := func(pos int) string { return fmt.Sprintf(`, "superseded_by": "%d"`, pos) }
	for _, row := range []struct{ name, body string }{
		{"regression with the revert's pr", retireBody(anchor, hyp, "regression", pr)},
		{"superseded by a later promotion of another path", retireBody(anchor, hyp, "superseded", by(opp))},
		{"expired with neither field", retireBody(anchor, hyp, "expired", "")},
	} {
		if err := try(st.observer, row.body); err != nil {
			t.Errorf("%s admits: %v", row.name, err)
		}
	}
	for _, row := range []struct{ name, body, gate string }{
		{"regression without pr", retireBody(anchor, hyp, "regression", ""), curation.GateRetireReason},
		{"pr on superseded", retireBody(anchor, hyp, "superseded", by(opp)+pr), curation.GateRetireReason},
		{"pr on expired", retireBody(anchor, hyp, "expired", pr), curation.GateRetireReason},
		{"superseded without superseded_by", retireBody(anchor, hyp, "superseded", ""), curation.GateRetireReason},
		{"superseded_by on regression", retireBody(anchor, hyp, "regression", pr+by(opp)), curation.GateRetireReason},
		{"superseded_by on expired", retireBody(anchor, hyp, "expired", by(opp)), curation.GateRetireReason},
		{"an unknown reason", retireBody(anchor, hyp, "rewritten", ""), curation.GateRetireReason},
		{"superseded_by naming no promotion", retireBody(anchor, hyp, "superseded", by(opp+1)), curation.GateRetireSuperseded},
		{"superseded_by naming an earlier position", retireBody(anchor, hyp, "superseded", by(pp-1)), curation.GateRetireSuperseded},
		{"superseded_by naming the retired promotion", retireBody(anchor, hyp, "superseded", by(pp)), curation.GateRetireSuperseded},
		{"an unknown key", retireBody(anchor, hyp, "expired", `, "note": "x"`), curation.GateRetireShape},
		{"a malformed pr", retireBody(anchor, hyp, "regression", `, "pr": "pr/10"`), curation.GateRetireShape},
		{"a malformed superseded_by", retireBody(anchor, hyp, "superseded", `, "superseded_by": "later"`), curation.GateRetireShape},
		{"a hypothesis on another subject", retireBody(anchor, cite(otherID, op), "expired", ""), curation.GateRetireShape},
		{"a lesson outside the store", retireBody("plans/x.md @ 0123456", hyp, "expired", ""), curation.GateRetireShape},
		{"the wrong hypothesis position", retireBody(anchor, cite(st.id, hp+1), "expired", ""), curation.GateRetirePromotion},
		{"another path's promotion", retireBody(otherAnchor, hyp, "expired", ""), curation.GateRetirePromotion},
	} {
		if got := gate(t, try(st.observer, row.body)); got != row.gate {
			t.Errorf("%s refuses at %s, got %s", row.name, row.gate, got)
		}
	}
	var incomplete *transition.IncompleteError
	if err := try(st.observer, `{"lesson": "`+anchor+`", "reason": "expired"}`); !errors.As(err, &incomplete) || len(incomplete.Missing) != 1 || incomplete.Missing[0] != "hypothesis" {
		t.Fatalf("a retirement missing its hypothesis is incomplete naming it: %v", err)
	}
	var oog *OutOfGrantError
	for _, key := range []ed25519.PrivateKey{st.curator, st.worker, st.verifier} {
		if err := try(key, retireBody(anchor, hyp, "expired", "")); !errors.As(err, &oog) {
			t.Fatalf("a key holding no observer or operator is out of grant for the retirement: %v", err)
		}
	}

	// Retire it. The conclusion goes, the evidence stays.
	body := retireBody(anchor, hyp, "expired", "")
	rpos := st.ctx.Count
	st.ctx = st.step(st.observer, st.v, curation.RetireVerb, st.id, body)
	fold := curation.Fold(st.ctx.Records)
	if r, ok := fold.Retired[retryPath]; !ok || r.Pos != rpos || r.Reason != "expired" || r.Hypothesis != hyp {
		t.Fatalf("the retirement folds on the path: %+v", fold.Retired)
	}
	if l, ok := fold.Lessons[retryPath]; !ok || l.Pos != pp {
		t.Fatalf("the retired promotion stays in the fold as evidence: %+v", fold.Lessons)
	}
	if _, ok := fold.Hypothesis(st.id); !ok || len(fold.DeadEnds["c-1"]) == 0 {
		t.Fatal("the hypothesis and the observations stay")
	}
	if fold.Anomalies != 0 {
		t.Fatalf("an admitted retirement is no anomaly: %d", fold.Anomalies)
	}
	for _, at := range []time.Time{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)} {
		for _, l := range curation.CandidatesAt(fold, st.ctx.Lifecycle, "c-4", at) {
			if c, _ := curation.ParseCitation(l.Hypothesis); c.Contract == st.id {
				t.Fatalf("a retired lesson surfaces at %s: %+v", at.Format(time.RFC3339), l)
			}
		}
	}
	for _, l := range curation.Candidates(fold, st.ctx.Lifecycle, "c-4") {
		if c, _ := curation.ParseCitation(l.Hypothesis); c.Contract == st.id {
			t.Fatalf("a retired lesson is a candidate: %+v", l)
		}
	}
	if len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 1 {
		t.Fatal("the other path's lesson still surfaces")
	}
	if got := gate(t, try(st.observer, body)); got != curation.GateRetirePromotion {
		t.Fatalf("a second retirement refuses at %s: %s", curation.GateRetirePromotion, got)
	}
	if got := gate(t, try(st.observer, retireBody(anchor, hyp, "regression", pr))); got != curation.GateRetirePromotion {
		t.Fatalf("a retirement for another reason over a standing one refuses at %s: %s", curation.GateRetirePromotion, got)
	}

	// A new promotion of the path through the gate brings it back.
	fresh := revalidation(t, st, hp, "eval-fresh", "2026-12-15T00:00:00Z", "2027-03-01T00:00:00Z")
	if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, fresh, st.ctx.Tip)); err != nil {
		t.Fatalf("a new promotion of a retired path admits: %v", err)
	}
	rp := st.ctx.Count
	st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, fresh)
	fold = curation.Fold(st.ctx.Records)
	if _, ok := fold.Retired[retryPath]; ok {
		t.Fatal("a new promotion clears the standing retirement")
	}
	if l := fold.Lessons[retryPath]; l.Pos != rp {
		t.Fatalf("the new promotion is the path's latest: %+v", l)
	}
	back := false
	for _, l := range curation.CandidatesAt(fold, st.ctx.Lifecycle, "c-4", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		if l.Pos == rp {
			back = true
		}
	}
	if !back {
		t.Fatal("the re-promoted lesson surfaces again")
	}
	// The old anchor is no longer the latest: a retirement citing it
	// refuses; one citing the new anchor admits.
	if got := gate(t, try(st.observer, retireBody(anchor, hyp, "expired", ""))); got != curation.GateRetirePromotion {
		t.Fatalf("a retirement of a non-latest promotion refuses at %s: %s", curation.GateRetirePromotion, got)
	}
	if err := try(st.observer, retireBody(retryAnchor2, hyp, "regression", pr)); err != nil {
		t.Fatalf("a retirement of the latest promotion admits: %v", err)
	}
}

// conformance: AC3 — dead ends retire and un-retire on the environment
// by the curator's act: the citation is an admitted dead end on this
// subject, the environment differs from the one the previous act
// named, a retirement needs no standing retirement and an
// un-retirement needs one; the fold flags and never deletes; the
// held-out listing skips a retired dead end.
func TestDeadEndRetirementRows(t *testing.T) {
	st := curationFixture(t)
	st.admitHypothesis(t)
	de := cite("c-1", st.deadEnd1b)
	body := func(deadend, env string) string {
		return fmt.Sprintf(`{"deadend": %q, "environment": %q, "reason": "the environment moved"}`, deadend, env)
	}
	try := func(key ed25519.PrivateKey, verb, subject, payload string) error {
		return Check(st.ctx, draftV(t, key, st.v, verb, subject, payload, st.ctx.Tip))
	}
	retire, unretire := curation.DeadEndRetireVerb, curation.DeadEndUnretireVerb
	if err := try(st.curator, retire, "c-1", body(de, "moved")); err != nil {
		t.Fatalf("a retirement citing an admitted dead end in another environment admits: %v", err)
	}
	for _, row := range []struct{ name, verb, subject, payload, gate string }{
		{"a park is no dead end", retire, "c-2", body(cite("c-2", st.park2), "moved"), curation.GateDeadEndRetireCited},
		{"a position past the tip", retire, "c-1", body(cite("c-1", 99999), "moved"), curation.GateDeadEndRetireCited},
		{"another contract's dead end", retire, "c-1", body(cite("c-6", st.deadEnd6), "moved"), curation.GateDeadEndRetireShape},
		{"a malformed citation", retire, "c-1", body("c-1", "moved"), curation.GateDeadEndRetireShape},
		{"an unknown key", retire, "c-1", `{"deadend": "` + de + `", "environment": "moved", "reason": "x", "delete": true}`, curation.GateDeadEndRetireShape},
		{"the recorded environment", retire, "c-1", body(de, "w"), curation.GateDeadEndRetireEnv},
		{"an un-retirement with nothing standing", unretire, "c-1", body(de, "moved"), curation.GateDeadEndRetireStand},
	} {
		if got := gate(t, try(st.curator, row.verb, row.subject, row.payload)); got != row.gate {
			t.Errorf("%s refuses at %s, got %s", row.name, row.gate, got)
		}
	}
	var incomplete *transition.IncompleteError
	if err := try(st.curator, retire, "c-1", `{"deadend": "`+de+`", "reason": "x"}`); !errors.As(err, &incomplete) || len(incomplete.Missing) != 1 || incomplete.Missing[0] != "environment" {
		t.Fatalf("a retirement without its environment is incomplete naming it: %v", err)
	}
	var oog *OutOfGrantError
	for _, key := range []ed25519.PrivateKey{st.worker, st.observer, st.verifier, st.dispatcher} {
		if err := try(key, retire, "c-1", body(de, "moved")); !errors.As(err, &oog) {
			t.Fatalf("a key holding no curate is out of grant for the dead-end retirement: %v", err)
		}
	}
	if verbs := Affordances(st.ctx, st.curator, "c-1"); !lists(verbs, retire) || lists(verbs, unretire) {
		t.Fatalf("with no retirement standing the curator's affordances on c-1 list the retirement alone: %v", verbs)
	}
	fold := curation.Fold(st.ctx.Records)
	h, _ := fold.Hypothesis(st.id)
	held := func() []string {
		var out []string
		for _, o := range curation.HeldOut(st.ctx.Records, st.ctx.Table, st.ctx.Lifecycle, h) {
			out = append(out, cite(o.Contract, o.Position))
		}
		return out
	}
	if !lists(held(), de) {
		t.Fatalf("the unretired dead end is held-out evidence: %v", held())
	}

	// Retire it: flagged, never deleted.
	rpos := st.ctx.Count
	st.ctx = st.step(st.curator, st.v, retire, "c-1", body(de, "moved"))
	fold = curation.Fold(st.ctx.Records)
	var flagged *curation.DeadEndFact
	for i := range fold.DeadEnds["c-1"] {
		if fold.DeadEnds["c-1"][i].Pos == st.deadEnd1b {
			flagged = &fold.DeadEnds["c-1"][i]
		}
	}
	if flagged == nil || !flagged.Retired || flagged.RetiredEnvironment != "moved" || flagged.RetiredAt == nil || *flagged.RetiredAt != rpos {
		t.Fatalf("the retirement flags the dead end in the fold: %+v", flagged)
	}
	if flagged.Environment != "w" || flagged.Tried == "" {
		t.Fatalf("the dead end's own evidence stays: %+v", flagged)
	}
	if flagged.Applies("w") || flagged.Applies("moved") {
		t.Fatal("a retired dead end applies nowhere")
	}
	if lists(held(), de) {
		t.Fatalf("a retired dead end leaves the held-out listing: %v", held())
	}
	if lists(held(), cite("c-6", st.deadEnd6)) == false {
		t.Fatalf("the other dead ends stay held out: %v", held())
	}
	if got := gate(t, try(st.curator, retire, "c-1", body(de, "moved-again"))); got != curation.GateDeadEndRetireStand {
		t.Fatalf("a second retirement refuses at %s: %s", curation.GateDeadEndRetireStand, got)
	}
	if got := gate(t, try(st.curator, unretire, "c-1", body(de, "moved"))); got != curation.GateDeadEndRetireEnv {
		t.Fatalf("an un-retirement in the retirement's environment refuses at %s: %s", curation.GateDeadEndRetireEnv, got)
	}
	if verbs := Affordances(st.ctx, st.curator, "c-1"); !lists(verbs, unretire) {
		t.Fatalf("over a standing retirement the curator's affordances on c-1 list the un-retirement: %v", verbs)
	}
	if err := try(st.curator, unretire, "c-1", body(de, "moved-again")); err != nil {
		t.Fatalf("an un-retirement in another environment admits: %v", err)
	}
	if err := try(st.curator, unretire, "c-1", body(de, "w")); err != nil {
		t.Fatalf("the dead end's own environment is not the standing retirement's: %v", err)
	}
	var oog2 *OutOfGrantError
	if err := try(st.worker, unretire, "c-1", body(de, "moved-again")); !errors.As(err, &oog2) {
		t.Fatalf("a claim key is out of grant for the un-retirement: %v", err)
	}
	upos := st.ctx.Count
	st.ctx = st.step(st.curator, st.v, unretire, "c-1", body(de, "moved-again"))
	fold = curation.Fold(st.ctx.Records)
	flagged = nil
	for i := range fold.DeadEnds["c-1"] {
		if fold.DeadEnds["c-1"][i].Pos == st.deadEnd1b {
			flagged = &fold.DeadEnds["c-1"][i]
		}
	}
	if flagged == nil || flagged.Retired || flagged.RetiredEnvironment != "moved-again" || *flagged.RetiredAt != upos {
		t.Fatalf("the un-retirement clears the flag and names its environment: %+v", flagged)
	}
	if !flagged.Applies("w") || flagged.Applies("moved-again") {
		t.Fatal("an un-retired dead end applies in its recorded environment again, by string equality")
	}
	if !lists(held(), de) {
		t.Fatalf("an un-retired dead end is held out again: %v", held())
	}
	if got := gate(t, try(st.curator, retire, "c-1", body(de, "moved-again"))); got != curation.GateDeadEndRetireEnv {
		t.Fatalf("a retirement in the un-retirement's environment refuses at %s: %s", curation.GateDeadEndRetireEnv, got)
	}
	if err := try(st.curator, retire, "c-1", body(de, "moved")); err != nil {
		t.Fatalf("a retirement in yet another environment admits: %v", err)
	}
	if got := gate(t, try(st.curator, unretire, "c-1", body(de, "moved"))); got != curation.GateDeadEndRetireStand {
		t.Fatalf("with nothing standing the un-retirement refuses at %s: %s", curation.GateDeadEndRetireStand, got)
	}
	if fold.Anomalies != 0 {
		t.Fatalf("admitted acts are no anomalies: %d", fold.Anomalies)
	}
}

// conformance: AC2 — the retirement is an affordance exactly while a
// promotion stands unretired: listed after the promotion, gone after
// the retirement, back after the re-promotion, on the same rule set
// admission enforces.
func TestRetirementAffordanceFollowsTheStandingPromotion(t *testing.T) {
	st := curationFixture(t)
	hp, _, anchor := promoted(t, st)
	if !lists(Affordances(st.ctx, st.observer, st.id), curation.RetireVerb) {
		t.Fatal("a standing promotion lists the retirement for the observer")
	}
	if lists(Affordances(st.ctx, st.curator, st.id), curation.RetireVerb) {
		t.Fatal("the curator holds no grant the retirement accepts")
	}
	st.ctx = st.step(st.observer, st.v, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "expired", ""))
	if lists(Affordances(st.ctx, st.observer, st.id), curation.RetireVerb) {
		t.Fatal("a retired promotion offers no second retirement")
	}
	st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, revalidation(t, st, hp, "eval-back", "2026-12-15T00:00:00Z", "2027-03-01T00:00:00Z"))
	if !lists(Affordances(st.ctx, st.observer, st.id), curation.RetireVerb) {
		t.Fatal("a new promotion of the path lists the retirement again")
	}
}

// conformance: the refold class again (review finding on the task
// PR): folding many alternating dead-end retirements and
// un-retirements is linear passes, never a replay per act, so a dead
// end whose environment moved a few dozen times still folds at once.
func TestFoldingManyDeadEndActsNeverReplays(t *testing.T) {
	st := curationFixture(t)
	de := cite("c-1", st.deadEnd1b)
	for i := 0; i < 24; i++ {
		verb := curation.DeadEndRetireVerb
		if i%2 == 1 {
			verb = curation.DeadEndUnretireVerb
		}
		st.ctx = st.step(st.curator, st.v, verb, "c-1", fmt.Sprintf(`{"deadend": %q, "environment": "env-%d", "reason": "moved"}`, de, i))
	}
	done := make(chan *curation.State, 1)
	go func() { done <- curation.Fold(st.ctx.Records) }()
	select {
	case fold := <-done:
		var flagged *curation.DeadEndFact
		for i := range fold.DeadEnds["c-1"] {
			if fold.DeadEnds["c-1"][i].Pos == st.deadEnd1b {
				flagged = &fold.DeadEnds["c-1"][i]
			}
		}
		if flagged == nil || flagged.Retired || flagged.RetiredEnvironment != "env-23" || fold.Anomalies != 0 {
			t.Fatalf("twenty-four alternating acts all admit and the last stands: %+v anomalies %d", flagged, fold.Anomalies)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("folding twenty-four dead-end acts did not finish in twenty seconds: the replay recurses")
	}
	if got := gate(t, Check(st.ctx, draftV(t, st.curator, st.v, curation.DeadEndRetireVerb, "c-1", fmt.Sprintf(`{"deadend": %q, "environment": "env-23", "reason": "moved"}`, de), st.ctx.Tip))); got != curation.GateDeadEndRetireEnv {
		t.Fatalf("the boundary judges the twenty-fifth act against the standing state: %s", got)
	}
}
