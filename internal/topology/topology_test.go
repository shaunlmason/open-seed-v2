package topology

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	filedBody = `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`
	specBody  = `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`
)

func testKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func signed(t *testing.T, key ed25519.PrivateKey, v, verb, subject, payload string) *event.Record {
	t.Helper()
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{V: v, TS: "2026-09-02T00:00:00Z", Actor: fp, Verb: verb, Subject: subject,
		Payload: json.RawMessage(payload), Prev: strings.Repeat("0", 64)}, key)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// stand is a hand-built lifecycle: every named contract filed and
// specified (ready), then the listed extra lifecycle steps applied.
type stand struct {
	t       *testing.T
	table   *transition.Table
	records []*event.Record
	key     ed25519.PrivateKey
	graph   *Graph
}

func newStand(t *testing.T, contracts ...string) *stand {
	t.Helper()
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	s := &stand{t: t, table: table, key: testKey(1), graph: New()}
	for _, c := range contracts {
		s.step("intent.filed", c, filedBody)
		s.step("contract.specified", c, specBody)
	}
	return s
}

func (s *stand) step(verb, subject, payload string) {
	s.t.Helper()
	s.records = append(s.records, signed(s.t, s.key, version.Seed1, verb, subject, payload))
}

func (s *stand) fold() *transition.Fold { return s.table.FoldRecords(s.records) }

// relate applies a relation through the graph rule against the
// current lifecycle, the way admission judges a candidate.
func (s *stand) relate(verb, subject, payload string) error {
	s.t.Helper()
	rec := signed(s.t, s.key, version.Seed1, verb, subject, payload)
	if err := s.graph.Apply(s.fold(), s.table, len(s.records), &rec.Event); err != nil {
		return err
	}
	s.records = append(s.records, rec)
	return nil
}

func (s *stand) must(verb, subject, payload string) {
	s.t.Helper()
	if err := s.relate(verb, subject, payload); err != nil {
		s.t.Fatalf("%s on %s: %v", verb, subject, err)
	}
}

func (s *stand) derived() *Derived { return Derive(s.graph, s.fold(), s.table) }

func refused(t *testing.T, err error, want string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(err.Error(), want) {
		t.Fatalf("refuses naming %q: %v", want, err)
	}
}

// conformance: III.F row 12 (supporting boundary) — the four facts
// are strict in shape and the mission is an anchor, never prose.
func TestParseIsStrict(t *testing.T) {
	for _, verb := range []string{DependVerb, UndependVerb} {
		if got, err := Parse(verb, "c-1", []byte(`{"requires": "c-2"}`)); err != nil || got != "c-2" {
			t.Fatalf("%s parses its target: %q %v", verb, got, err)
		}
	}
	if got, err := Parse(ParentVerb, "c-1", []byte(`{"parent": "p"}`)); err != nil || got != "p" {
		t.Fatalf("parent parses: %q %v", got, err)
	}
	if got, err := Parse(AlignVerb, "c-1", []byte(`{"mission": "docs/mission.md @ 0123456"}`)); err != nil || got != "docs/mission.md @ 0123456" {
		t.Fatalf("align parses the anchor: %q %v", got, err)
	}
	for name, in := range map[string]struct{ verb, body, want string }{
		"unknown field":   {DependVerb, `{"requires": "c-2", "why": "x"}`, "strict object {requires}"},
		"trailing data":   {DependVerb, `{"requires": "c-2"} {}`, "trailing data"},
		"empty requires":  {UndependVerb, `{"requires": " "}`, "requires names"},
		"empty parent":    {ParentVerb, `{"parent": ""}`, "parent names"},
		"wrong type":      {ParentVerb, `{"parent": 7}`, "strict object {parent}"},
		"mission prose":   {AlignVerb, `{"mission": "make it better"}`, "not a commit-anchored reference"},
		"mission extra":   {AlignVerb, `{"mission": "m.md @ 0123456", "note": "x"}`, "strict object {mission}"},
		"not a relation":  {"claim.taken", `{}`, "not a relation verb"},
		"self-describing": {AlignVerb, `{"mission": "m.md @ 0123456"}` + "\n" + `1`, "trailing data"},
	} {
		_, err := Parse(in.verb, "c-1", []byte(in.body))
		refused(t, err, in.want)
		if name == "not a relation" && IsRelationVerb(in.verb) {
			t.Fatal("claim.taken is no relation verb")
		}
	}
	if !IsRelationVerb(AlignVerb) || !reflect.DeepEqual(Verbs, []string{DependVerb, UndependVerb, ParentVerb, AlignVerb}) {
		t.Fatalf("the catalog is the four: %v", Verbs)
	}
	if IsAnchor("../x @ 0123456") || IsAnchor("x @ 01234") || !IsAnchor("a/b.md @ "+strings.Repeat("a", 40)) {
		t.Fatal("the anchor grammar is the store's")
	}
}

// conformance: III.F row 12 (supporting boundary) — the graph rule:
// known nonterminal source, known target, no self edge, no cycle,
// links a set, parent and mission replaced.
func TestApplyHoldsTheGraphRule(t *testing.T) {
	s := newStand(t, "a", "b", "c", "p")
	s.step("intent.filed", "backlog-only", filedBody)
	s.step("contract.cancelled", "c", `{}`)
	refused(t, s.relate(DependVerb, "nope", `{"requires": "a"}`), "no contract by that id")
	refused(t, s.relate(DependVerb, "c", `{"requires": "a"}`), "the contract is cancelled")
	refused(t, s.relate(DependVerb, "a", `{"requires": "a"}`), "cannot require or contain itself")
	refused(t, s.relate(ParentVerb, "a", `{"parent": "a"}`), "cannot require or contain itself")
	refused(t, s.relate(DependVerb, "a", `{"requires": "ghost"}`), "ghost is no contract")
	refused(t, s.relate(ParentVerb, "a", `{"parent": "ghost"}`), "ghost is no contract")
	refused(t, s.relate(UndependVerb, "a", `{"requires": "b"}`), "does not require b")
	// A backlog subject is open and may relate; a terminal target is a
	// legal dependency, already satisfied.
	s.must(DependVerb, "backlog-only", `{"requires": "c"}`)
	s.must(DependVerb, "a", `{"requires": "b"}`)
	refused(t, s.relate(DependVerb, "a", `{"requires": "b"}`), "already requires b")
	refused(t, s.relate(DependVerb, "b", `{"requires": "a"}`), "a dependency cycle")
	s.must(DependVerb, "b", `{"requires": "p"}`)
	refused(t, s.relate(DependVerb, "p", `{"requires": "a"}`), "a dependency cycle")
	if got := s.graph.Requires("a"); len(got) != 1 || got[0].Target != "b" || got[0].Pos != 11 {
		t.Fatalf("the link carries its target and position: %+v", got)
	}
	if got := s.graph.RequiredBy("b"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("required-by is the reverse index: %v", got)
	}
	s.must(UndependVerb, "a", `{"requires": "b"}`)
	if len(s.graph.Requires("a")) != 0 || len(s.graph.RequiredBy("b")) != 0 {
		t.Fatal("an unlink removes the edge")
	}
	if _, has := s.graph.requires["a"]; has {
		t.Fatal("an emptied link set is dropped")
	}
	s.must(DependVerb, "a", `{"requires": "b"}`)
	// Hierarchy: a under p, b under a; p under b would cycle; a re-parent replaces.
	s.must(ParentVerb, "a", `{"parent": "p"}`)
	s.must(ParentVerb, "b", `{"parent": "a"}`)
	refused(t, s.relate(ParentVerb, "p", `{"parent": "b"}`), "a hierarchy cycle")
	if got := s.graph.Ancestors("b"); !reflect.DeepEqual(got, []string{"a", "p"}) {
		t.Fatalf("ancestors nearest first: %v", got)
	}
	if got := s.graph.Children("p"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("children: %v", got)
	}
	s.must(ParentVerb, "b", `{"parent": "p"}`)
	if p, _ := s.graph.Parent("b"); p.Target != "p" {
		t.Fatalf("a repeated parent replaces: %+v", p)
	}
	if got := s.graph.Children("p"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("children after the move: %v", got)
	}
	// Mission: set and replaced.
	s.must(AlignVerb, "p", `{"mission": "docs/mission.md @ 0123456"}`)
	s.must(AlignVerb, "p", `{"mission": "docs/mission.md @ 0123457"}`)
	if m, ok := s.graph.Mission("p"); !ok || m.Target != "docs/mission.md @ 0123457" {
		t.Fatalf("a repeated mission replaces: %+v", m)
	}
	if !s.graph.Any() || !reflect.DeepEqual(s.graph.Subjects(), []string{"backlog-only", "a", "b", "p"}) {
		t.Fatalf("subjects first-seen: %v", s.graph.Subjects())
	}
	// The lifecycle fold keeps the relation facts out of its anomalies:
	// they are beside the lifecycle, not refused by it.
	for _, c := range []string{"a", "b", "p"} {
		if st, _ := s.fold().State(c); st.Anomalies != 0 || st.State != "ready" {
			t.Fatalf("%s keeps its base state with no anomaly: %+v", c, st)
		}
	}
	// A graph rule needs a lifecycle to resolve against.
	rec := signed(t, s.key, version.Seed1, DependVerb, "a", `{"requires": "b"}`)
	refused(t, New().Apply(nil, s.table, 0, &rec.Event), "no lifecycle")
}

// conformance: III.F row 12 (dependency, hold, rollup and ancestry
// halves, derived) — effective readiness, inherited holds, mission
// ancestry, warnings and rollups are functions of the two folds.
func TestDerivedReadsBothFolds(t *testing.T) {
	s := newStand(t, "root", "mid", "leaf", "dep", "dep2", "lone")
	s.must(ParentVerb, "mid", `{"parent": "root"}`)
	s.must(ParentVerb, "leaf", `{"parent": "mid"}`)
	s.must(DependVerb, "leaf", `{"requires": "dep"}`)
	s.must(DependVerb, "leaf", `{"requires": "dep2"}`)
	s.must(AlignVerb, "root", `{"mission": "docs/mission.md @ 0123456"}`)
	d := s.derived()
	if !d.EffectiveReady("mid") || !d.EffectiveReady("dep") || !d.EffectiveReady("lone") {
		t.Fatal("relation-free and dependency-free ready subjects are effectively ready")
	}
	if d.EffectiveReady("leaf") || !reflect.DeepEqual(d.Unresolved("leaf"), []string{"dep", "dep2"}) {
		t.Fatalf("open dependencies keep leaf out: %v", d.Unresolved("leaf"))
	}
	// One dependency closes (cancelled is terminal); the other holds.
	s.step("contract.cancelled", "dep", `{}`)
	d = s.derived()
	if d.EffectiveReady("leaf") || !reflect.DeepEqual(d.Unresolved("leaf"), []string{"dep2"}) {
		t.Fatalf("a second dependency keeps leaf out: %v", d.Unresolved("leaf"))
	}
	// The last closes through done: claim, submit, verdict, merge is
	// long; cancel is the same terminality to the table.
	before := d
	s.step("contract.cancelled", "dep2", `{}`)
	d = s.derived()
	if !d.EffectiveReady("leaf") || len(d.Unresolved("leaf")) != 0 {
		t.Fatal("every dependency terminal makes leaf effectively ready with no lifecycle event")
	}
	if got := ReadinessDelta(before, d); !reflect.DeepEqual(got, []string{"leaf"}) {
		t.Fatalf("the delta names what became ready: %v", got)
	}
	if got := ReadinessDelta(nil, d); !reflect.DeepEqual(got, []string{"root", "mid", "leaf", "lone"}) {
		t.Fatalf("a nil before is every ready subject, fold order: %v", got)
	}
	if st, _ := s.fold().State("leaf"); st.State != "ready" || st.Since != 5 {
		t.Fatalf("leaf's base state never moved: %+v", st)
	}
	// A hold on the root cascades to every descendant; base states stay.
	s.step("contract.blocked", "root", `{}`)
	d = s.derived()
	if !reflect.DeepEqual(d.HeldBy("leaf"), []string{"root"}) || !reflect.DeepEqual(d.HeldBy("mid"), []string{"root"}) || len(d.HeldBy("root")) != 0 {
		t.Fatalf("held_by names the blocked ancestor: %v %v", d.HeldBy("leaf"), d.HeldBy("mid"))
	}
	if d.EffectiveReady("leaf") || d.EffectiveReady("mid") || !d.EffectiveReady("lone") {
		t.Fatal("a held descendant is not effectively ready; unrelated work is")
	}
	if st, _ := s.fold().State("leaf"); st.State != "ready" {
		t.Fatalf("the hold rewrote no descendant: %+v", st)
	}
	if got := d.Ready(); !reflect.DeepEqual(got, []string{"lone"}) {
		t.Fatalf("ready is the effectively ready set: %v", got)
	}
	// Mission ancestry: leaf inherits root's through mid; lone has none.
	if anchor, from, ok := d.Mission("leaf"); !ok || from != "root" || anchor != "docs/mission.md @ 0123456" {
		t.Fatalf("leaf inherits the mission: %q %q %v", anchor, from, ok)
	}
	if _, ok := d.Warning("leaf"); ok {
		t.Fatal("an inheriting child does not warn")
	}
	if w, ok := d.Warning("lone"); !ok || !reflect.DeepEqual(w.Ancestry, []string{"lone"}) {
		t.Fatalf("an open orphan warns with its chain: %+v %v", w, ok)
	}
	if _, ok := d.Warning("dep"); ok {
		t.Fatal("terminal unanchored work does not warn")
	}
	if _, ok := d.Warning("ghost"); ok {
		t.Fatal("an unknown subject does not warn")
	}
	if got := d.Warnings(); len(got) != 1 || got[0].Subject != "lone" {
		t.Fatalf("warnings over the fold: %+v", got)
	}
	// Rollups: root's descendants are mid and leaf; the initiative
	// itself is not counted; milestones sum.
	s.step("contract.unblocked", "root", `{}`)
	s.step(transition.MilestoneVerb, "leaf", `{"count": 3, "step": "half"}`)
	s.step(transition.MilestoneVerb, "mid", `{"count": 2, "step": "start"}`)
	d = s.derived()
	r, ok := d.Rollup("root")
	if !ok || r.Descendants != 2 || r.ByState["ready"] != 2 || r.Terminal != 0 || r.EffectiveReady != 2 || r.DependencyWaiting != 0 || r.Held != 0 || r.Milestones != 5 {
		t.Fatalf("root's rollup: %+v", r)
	}
	if _, ok := d.Rollup("leaf"); ok {
		t.Fatal("a childless subject is no initiative")
	}
	if got := d.Initiatives(); !reflect.DeepEqual(got, []string{"mid", "root"}) {
		t.Fatalf("initiatives sorted: %v", got)
	}
	if got := d.Descendants("root"); !reflect.DeepEqual(got, []string{"leaf", "mid"}) {
		t.Fatalf("descendants sorted, self excluded: %v", got)
	}
	// A waiting and a held descendant count as such.
	s.must(DependVerb, "leaf", `{"requires": "lone"}`)
	s.step("contract.blocked", "mid", `{}`)
	d = s.derived()
	r, _ = d.Rollup("root")
	if r.ByState["blocked"] != 1 || r.DependencyWaiting != 1 || r.Held != 1 || r.EffectiveReady != 0 {
		t.Fatalf("waiting and held: %+v", r)
	}
	// An unknown descendant subject counts as unknown rather than crashing.
	d2 := Derive(s.graph, nil, s.table)
	if r, ok := d2.Rollup("root"); !ok || r.ByState["unknown"] != 2 || d2.Open("root") || d2.EffectiveReady("root") {
		t.Fatalf("no lifecycle: everything is unknown: %+v", r)
	}
	if len(d2.Warnings()) != 0 || len(d2.Ready()) != 0 || len(d2.Unresolved("leaf")) != 3 {
		t.Fatal("no lifecycle: nothing warns, nothing is ready, every dependency is unresolved")
	}
	if Derive(nil, nil, nil).Graph == nil {
		t.Fatal("a nil graph derives as empty")
	}
}

// conformance: III.F row 12 (supporting boundary) — a fact that never
// passed the boundary is an anomaly, kept and named, shaping nothing:
// recorded before activation, or by a key with no grant at its
// position.
func TestFoldTrustsOnlyWhatPassedTheBoundary(t *testing.T) {
	s := newStand(t, "a", "b")
	if Carries(s.records) || Fold(s.records, s.table).Any() {
		t.Fatal("a relation-free prefix carries and folds nothing")
	}
	s.records = append(s.records, signed(t, s.key, version.Protocol, DependVerb, "a", `{"requires": "b"}`))
	s.records = append(s.records, signed(t, s.key, version.Seed1, DependVerb, "a", `{"requires": "b"}`))
	if !Carries(s.records) {
		t.Fatal("the prefix carries a relation fact")
	}
	g := Fold(s.records, s.table)
	if g.Any() || len(g.Anomalies) != 2 {
		t.Fatalf("no keyring stands, so nothing is trusted: %+v", g.Anomalies)
	}
	if !strings.Contains(g.Anomalies[0].Reason, "before the lifecycle activated") || !strings.Contains(g.Anomalies[1].Reason, "held none of {dispatch, operator}") {
		t.Fatalf("anomalies name why: %+v", g.Anomalies)
	}
	if g.Anomalies[1].Pos != 5 || g.Anomalies[1].Verb != DependVerb || g.Anomalies[1].Subject != "a" {
		t.Fatalf("an anomaly names its position, verb and subject: %+v", g.Anomalies[1])
	}
	d := DeriveRecords(s.records, s.table)
	if !d.EffectiveReady("a") || len(d.Unresolved("a")) != 0 {
		t.Fatal("an untrusted link hides no work")
	}
	if Fold(s.records, nil).Any() {
		t.Fatal("no table, no fold")
	}
}
