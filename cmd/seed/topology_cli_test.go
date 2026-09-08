package main

// The four named drills of III.F row 12 (plans/os-f0ae2cdf.md; spec/
// topology.md "Drills"): the dependency cascade, the hold cascade, the
// initiative rollup and the goal-ancestry warning, each driven through
// the CLI's checked path and read back from the queue, the poll, the
// claim boundary, the contracts view, the report and the cache. The
// wakeless run (#145, TestWakelessPollOnlyRun) stays untouched as the
// correctness path's evidence; these build on it.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/offers"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
)

// topologyStand is the offer ledger with helpers for the graph drills.
type topologyStand struct {
	t          *testing.T
	ld, src    string
	base, head string
	specCommit string
	priv       string
	keys, fps  map[string]string
}

func newTopologyStand(t *testing.T) *topologyStand {
	t.Helper()
	ld, src, base, specCommit, head, priv, _, keys, fps := offerLedger(t)
	return &topologyStand{t: t, ld: ld, src: src, base: base, head: head, specCommit: specCommit, priv: priv, keys: keys, fps: fps}
}

func (s *topologyStand) file(subjects ...string) {
	s.t.Helper()
	for _, subject := range subjects {
		offerFile(s.t, s.ld, s.priv, s.specCommit, subject)
	}
}

func (s *topologyStand) append(key, verb, subject, payload string) string {
	s.t.Helper()
	e, code := runEnv(s.t, "ledger", "append", "--ledger", s.ld, "--key", key, "--verb", verb, "--subject", subject, "--payload", payload)
	if code != 0 {
		s.t.Fatalf("%s on %s: %d %+v", verb, subject, code, e.Error)
	}
	return *e.Position
}

func (s *topologyStand) relate(sub, subject, flag, target string) (ledgerEnv, int) {
	s.t.Helper()
	return runEnv(s.t, "topology", sub, "--ledger", s.ld, "--key", s.priv, "--subject", subject, "--"+flag, target)
}

func (s *topologyStand) must(sub, subject, flag, target string) {
	s.t.Helper()
	if e, code := s.relate(sub, subject, flag, target); code != 0 {
		s.t.Fatalf("topology %s %s --%s %s: %d %+v", sub, subject, flag, target, code, e.Error)
	}
}

func (s *topologyStand) publish(subject string) {
	s.t.Helper()
	if e, code := runEnv(s.t, "offer", "publish", "--ledger", s.ld, "--subject", subject, "--key", s.keys["supervisor"],
		"--expires", "2027-01-01T00:00:00Z", "--capability", "claim", "--tier", "trivial"); code != 0 {
		s.t.Fatalf("offer on %s: %d %+v", subject, code, e.Error)
	}
}

// ctx is the admission context at the tip: records, table, keyring.
func (s *topologyStand) ctx() *admit.Context {
	s.t.Helper()
	store, err := ledger.Open(s.ld)
	if err != nil {
		s.t.Fatal(err)
	}
	c, err := admit.ContextAt(store)
	if err != nil {
		s.t.Fatal(err)
	}
	return c
}

func (s *topologyStand) position() int { return s.ctx().Count }

// rebuild publishes every projection and returns the out dir.
func (s *topologyStand) rebuild() string {
	s.t.Helper()
	out := filepath.Join(s.t.TempDir(), "out")
	unlockForCleanup(s.t, out)
	if e, code := runEnv(s.t, "project", "rebuild", "--ledger", s.ld, "--out", out); code != 0 {
		s.t.Fatalf("project rebuild: %d %+v", code, e.Error)
	}
	return out
}

func (s *topologyStand) viewBytes(out, name string) []byte {
	s.t.Helper()
	cur, code := runEnv(s.t, "project", "current", "--out", out, "--name", name)
	if code != 0 {
		s.t.Fatalf("project current %s: %d %+v", name, code, cur.Error)
	}
	file := name + ".json"
	if name == "cache" {
		file = project.CacheFile
	}
	b, err := os.ReadFile(filepath.Join(cur.Result["path"].(string), file))
	if err != nil {
		s.t.Fatal(err)
	}
	return b
}

func (s *topologyStand) contracts(out string) map[string]map[string]any {
	s.t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal(s.viewBytes(out, "contracts"), &entries); err != nil {
		s.t.Fatal(err)
	}
	by := map[string]map[string]any{}
	for _, e := range entries {
		by[e["subject"].(string)] = e
	}
	return by
}

func (s *topologyStand) queue(out string) []string {
	s.t.Helper()
	var q project.QueueView
	if err := json.Unmarshal(s.viewBytes(out, "queue"), &q); err != nil {
		s.t.Fatal(err)
	}
	subjects := []string{}
	for _, e := range q.Ready {
		subjects = append(subjects, e.Subject)
	}
	return subjects
}

func (s *topologyStand) report(out string) map[string]any {
	s.t.Helper()
	var r map[string]any
	if err := json.Unmarshal(s.viewBytes(out, "report"), &r); err != nil {
		s.t.Fatal(err)
	}
	return r
}

func (s *topologyStand) cacheQuery(out, query string, args ...any) string {
	s.t.Helper()
	build, err := project.Current(out, "cache")
	if err != nil {
		s.t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(build, project.CacheFile)+"?mode=ro&immutable=1")
	if err != nil {
		s.t.Fatal(err)
	}
	defer db.Close()
	var v sql.NullString
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		s.t.Fatalf("%s: %v", query, err)
	}
	return v.String
}

func topologyOf(t *testing.T, entry map[string]any) map[string]any {
	t.Helper()
	tv, _ := entry["topology"].(map[string]any)
	if tv == nil {
		t.Fatalf("no topology on %v", entry["subject"])
	}
	return tv
}

func viewState(entry map[string]any) string {
	st, _ := entry["state"].(string)
	return st
}

func strs(v any) []string {
	out := []string{}
	list, _ := v.([]any)
	for _, x := range list {
		out = append(out, x.(string))
	}
	return out
}

type recordingChannel struct{ woken []string }

func (r *recordingChannel) Wake(actor string) error {
	r.woken = append(r.woken, actor)
	return nil
}

// conformance: III.F row 12 (dependency half) — TestDependencyCascadeWakesAndPolls:
// a ready dependent with a live offer is absent from the queue and the
// poll while a required contract is open; the required contract
// reaches a terminal state; the dependent appears at that exact
// prefix, its claim admits, a recording wake channel is called for the
// eligible actor, and the same run with no channel reaches the claim
// by polling. A second dependency keeps it out until every requirement
// is terminal. Builds on TestWakelessPollOnlyRun (#145).
func TestDependencyCascadeWakesAndPolls(t *testing.T) {
	s := newTopologyStand(t)
	s.file("c-1", "c-2", "c-3")
	s.must("depend", "c-1", "requires", "c-2")
	s.must("depend", "c-1", "requires", "c-3")
	s.publish("c-1")
	// Hidden while a requirement is open: the poll, the queue and the
	// claim boundary agree.
	if rows := listOffers(t, s.ld, s.fps["workerA"], ""); len(rows) != 0 {
		t.Fatalf("a waiting dependent lists no offer: %+v", rows)
	}
	out := s.rebuild()
	if q := s.queue(out); !reflect.DeepEqual(q, []string{"c-2", "c-3"}) {
		t.Fatalf("the queue hides the dependent and lists its requirements: %v", q)
	}
	tv := topologyOf(t, s.contracts(out)["c-1"])
	if tv["effective_ready"] != false || !reflect.DeepEqual(strs(tv["unresolved"]), []string{"c-2", "c-3"}) || viewState(s.contracts(out)["c-1"]) != "ready" {
		t.Fatalf("the view names the wait and keeps the base state: %+v", tv)
	}
	var notReady *topology.NotReadyError
	if _, err := admitAppend(t, s.ld, workerRawKey(22), "claim.taken", "c-1", `{}`); !errors.As(err, &notReady) {
		t.Fatalf("a claim on the waiting dependent refuses not_effectively_ready: %v", err)
	}
	// One requirement closes: still out, since the other is open.
	s.append(s.priv, "contract.cancelled", "c-2", `{}`)
	if rows := listOffers(t, s.ld, s.fps["workerA"], ""); len(rows) != 0 {
		t.Fatalf("a second dependency keeps it out: %+v", rows)
	}
	cursor := s.position()
	// The last requirement reaches done through the ordinary path.
	fencePos, err := admitAppend(t, s.ld, workerRawKey(23), "claim.taken", "c-3", `{}`)
	if err != nil {
		t.Fatalf("c-3 claims: %v", err)
	}
	s.append(s.keys["workerB"], "submission.made", "c-3", fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["c-3 ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fencePos, s.base+".."+s.head))
	e, code := runEnv(t, "verdict", "render", "--ledger", s.ld, "--subject", "c-3", "--repo", s.src, "--key", s.keys["verifier"], "--verdict", "pass")
	if code != 0 {
		t.Fatalf("verdict: %d %+v", code, e.Error)
	}
	s.append(s.keys["workerB"], "merge.requested", "c-3", `{"verdict": "`+*e.Position+`"}`)
	if rows := listOffers(t, s.ld, s.fps["workerA"], ""); len(rows) != 0 {
		t.Fatalf("review is not terminal: %+v", rows)
	}
	s.append(s.priv, "merge.observed", "c-3", `{"merged": "`+s.head+`", "pr": "pr/1"}`)
	// At that exact prefix the dependent is claimable everywhere.
	if rows := listOffers(t, s.ld, s.fps["workerA"], ""); len(rows) != 1 || rows[0].(map[string]any)["subject"] != "c-1" {
		t.Fatalf("the poll lists the dependent once every requirement is terminal: %+v", rows)
	}
	out = s.rebuild()
	if q := s.queue(out); !reflect.DeepEqual(q, []string{"c-1"}) {
		t.Fatalf("the queue lists the dependent: %v", q)
	}
	tv = topologyOf(t, s.contracts(out)["c-1"])
	if tv["effective_ready"] != true || len(strs(tv["unresolved"])) != 0 {
		t.Fatalf("no dependency is unresolved: %+v", tv)
	}
	// The wake bridge over the delta: the eligible worker with a
	// channel is woken; without one, a candidate and nothing more; the
	// CLI pass names the delta and registers no channel.
	c := s.ctx()
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	ch := &recordingChannel{}
	res := offers.Bridge(c.Records, c.Table, c.Keyring, now, cursor, offers.Channels{s.fps["workerA"]: ch})
	if !reflect.DeepEqual(res.BecameReady, []string{"c-1"}) || len(res.Woken) != 1 || res.Woken[0].Actor != s.fps["workerA"] || !reflect.DeepEqual(ch.woken, []string{s.fps["workerA"]}) {
		t.Fatalf("the recording channel is called once for the eligible actor: %+v", res)
	}
	res = offers.Bridge(c.Records, c.Table, c.Keyring, now, cursor, nil)
	if len(res.Candidates) != 1 || res.Candidates[0].Subject != "c-1" || len(res.Woken) != 0 {
		t.Fatalf("no channel: a candidate, no wake: %+v", res)
	}
	e, code = runEnv(t, "offer", "wake", "--ledger", s.ld, "--since", fmt.Sprintf("%d", cursor), "--now", now.Format(time.RFC3339))
	if code != 0 || !reflect.DeepEqual(strs(e.Result["became_ready"]), []string{"c-1"}) || e.Result["channels"] != 0.0 {
		t.Fatalf("the CLI pass names the delta and wakes nobody: %d %+v", code, e.Result)
	}
	if e, code := runEnv(t, "offer", "wake", "--ledger", s.ld); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("wake without --since refuses at usage: %d %+v", code, e.Error)
	}
	// The same run with no channel reaches the claim by polling.
	if _, err := admitAppend(t, s.ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("the poller's claim admits with no wake ever sent: %v", err)
	}
	if st, _ := s.ctx().Lifecycle.State("c-1"); st.State != "in_progress" {
		t.Fatalf("the claim landed: %+v", st)
	}
	// The CLI verbs refuse at usage and at the boundary naming the part.
	if e, code := runEnv(t, "topology", "depend", "--ledger", s.ld, "--key", s.priv, "--subject", "c-2"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("a missing target refuses at usage: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "topology", "align", "--ledger", s.ld, "--key", s.priv, "--subject", "c-2", "--mission", "do better"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("a mission that is not an anchor refuses at usage: %d %+v", code, e.Error)
	}
	if e, code := s.relate("depend", "c-2", "requires", "c-1"); code != 3 || e.Error == nil || e.Error.Code != "topology_refused" {
		t.Fatalf("a terminal source refuses topology_refused: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "topology", "nope"); code != 64 || e.Error == nil {
		t.Fatalf("an unknown subverb refuses at usage: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "topology", "undepend", "--ledger", s.ld, "--key", s.keys["workerA"], "--subject", "c-3", "--requires", "c-1"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim key refuses out of grant: %d %+v", code, e.Error)
	}
}

// conformance: III.F row 12 (hold half) — TestHoldCascadeSuppressesWakeUntilReleased:
// in a three-level tree, blocking the root makes both otherwise-ready
// descendants name the root in held_by and disappear from claims, the
// queue and the poll; closing a dependency while the hold remains
// emits no wake; unblocking the root exposes each eligible descendant
// and invokes one recording wake; their base lifecycle states never
// changed.
func TestHoldCascadeSuppressesWakeUntilReleased(t *testing.T) {
	s := newTopologyStand(t)
	s.file("root", "mid", "leaf", "dep")
	s.must("parent", "mid", "parent", "root")
	s.must("parent", "leaf", "parent", "mid")
	s.must("depend", "leaf", "requires", "dep")
	s.publish("mid")
	s.publish("leaf")
	s.append(s.priv, "contract.blocked", "root", `{}`)
	if rows := listOffers(t, s.ld, s.fps["workerA"], ""); len(rows) != 0 {
		t.Fatalf("held descendants list nothing: %+v", rows)
	}
	out := s.rebuild()
	if q := s.queue(out); !reflect.DeepEqual(q, []string{"dep"}) {
		t.Fatalf("the queue holds mid and leaf: %v", q)
	}
	views := s.contracts(out)
	for _, c := range []string{"mid", "leaf"} {
		tv := topologyOf(t, views[c])
		if !reflect.DeepEqual(strs(tv["held_by"]), []string{"root"}) || tv["effective_ready"] != false || viewState(views[c]) != "ready" {
			t.Fatalf("%s names the root as its hold and keeps ready: %+v", c, tv)
		}
	}
	var notReady *topology.NotReadyError
	if _, err := admitAppend(t, s.ld, workerRawKey(22), "claim.taken", "mid", `{}`); !errors.As(err, &notReady) || len(notReady.HeldBy) != 1 {
		t.Fatalf("a claim under the hold refuses naming it: %v", err)
	}
	// Closing leaf's dependency while the hold remains emits no wake.
	cursor := s.position()
	s.append(s.priv, "contract.cancelled", "dep", `{}`)
	c := s.ctx()
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	ch := &recordingChannel{}
	if res := offers.Bridge(c.Records, c.Table, c.Keyring, now, cursor, offers.Channels{s.fps["workerA"]: ch}); len(res.BecameReady) != 0 || len(res.Woken) != 0 || len(ch.woken) != 0 {
		t.Fatalf("a held subject produces no wake candidate: %+v", res)
	}
	// Lifting the hold exposes both, one wake each.
	cursor = s.position()
	s.append(s.priv, "contract.unblocked", "root", `{}`)
	c = s.ctx()
	res := offers.Bridge(c.Records, c.Table, c.Keyring, now, cursor, offers.Channels{s.fps["workerA"]: ch})
	// The root itself returns to ready with the unblock; it carries no
	// offer, so the two descendants are the candidates and the wakes.
	if !reflect.DeepEqual(res.BecameReady, []string{"root", "mid", "leaf"}) || len(res.Candidates) != 2 || len(res.Woken) != 2 || len(ch.woken) != 2 {
		t.Fatalf("both descendants become ready at the unblock and are woken once each: %+v", res)
	}
	if rows := listOffers(t, s.ld, s.fps["workerA"], ""); len(rows) != 2 {
		t.Fatalf("the poll lists both: %+v", rows)
	}
	out = s.rebuild()
	if q := s.queue(out); !reflect.DeepEqual(q, []string{"mid", "leaf", "root"}) {
		t.Fatalf("the queue lists the tree: %v", q)
	}
	views = s.contracts(out)
	for _, c := range []string{"mid", "leaf"} {
		if st := viewState(views[c]); st != "ready" {
			t.Fatalf("%s's base state never changed: %s", c, st)
		}
		if tv := topologyOf(t, views[c]); len(strs(tv["held_by"])) != 0 || tv["effective_ready"] != true {
			t.Fatalf("%s is free: %+v", c, tv)
		}
	}
	if _, err := admitAppend(t, s.ld, workerRawKey(22), "claim.taken", "leaf", `{}`); err != nil {
		t.Fatalf("the freed leaf claims: %v", err)
	}
}

// conformance: III.F row 12 (rollup half) — TestInitiativeRollupRendersDescendants:
// a two-level initiative with ready, held, dependency-waiting, done
// and cancelled descendants plus milestone facts renders the exact
// transitive state counts and milestone sum in the contracts view, the
// report and the cache; rebuilding the same prefix is byte-identical;
// the initiative itself is not counted as its descendant.
func TestInitiativeRollupRendersDescendants(t *testing.T) {
	s := newTopologyStand(t)
	s.file("init", "a", "b", "c", "d", "e", "f", "x")
	for _, child := range []string{"a", "c", "e", "f", "x"} {
		s.must("parent", child, "parent", "init")
	}
	s.must("parent", "b", "parent", "x")
	s.must("depend", "c", "requires", "d")
	s.append(s.priv, "contract.blocked", "x", `{}`)
	s.append(s.priv, "contract.cancelled", "e", `{}`)
	// a is claimed and reports a milestone; f is driven to done after
	// its own milestone.
	fenceA, err := admitAppend(t, s.ld, workerRawKey(22), "claim.taken", "a", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	s.append(s.keys["workerA"], "progress.milestone", "a", fmt.Sprintf(`{"fence": "%d", "count": 3, "step": "half"}`, fenceA))
	fenceF, err := admitAppend(t, s.ld, workerRawKey(23), "claim.taken", "f", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	s.append(s.keys["workerB"], "progress.milestone", "f", fmt.Sprintf(`{"fence": "%d", "count": 2, "step": "start"}`, fenceF))
	s.append(s.keys["workerB"], "submission.made", "f", fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["f ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fenceF, s.base+".."+s.head))
	e, code := runEnv(t, "verdict", "render", "--ledger", s.ld, "--subject", "f", "--repo", s.src, "--key", s.keys["verifier"], "--verdict", "pass")
	if code != 0 {
		t.Fatalf("verdict: %d %+v", code, e.Error)
	}
	s.append(s.keys["workerB"], "merge.requested", "f", `{"verdict": "`+*e.Position+`"}`)
	s.append(s.priv, "merge.observed", "f", `{"merged": "`+s.head+`", "pr": "pr/1"}`)

	out := s.rebuild()
	views := s.contracts(out)
	rollup, _ := topologyOf(t, views["init"])["rollup"].(map[string]any)
	want := map[string]any{
		"descendants": 6.0, "terminal": 2.0, "effective_ready": 0.0, "dependency_waiting": 1.0, "held": 1.0, "milestones": 5.0,
		"by_state": map[string]any{"in_progress": 1.0, "ready": 2.0, "blocked": 1.0, "cancelled": 1.0, "done": 1.0},
	}
	if !reflect.DeepEqual(rollup, want) {
		t.Fatalf("the initiative's rollup is exact and excludes itself:\n got %+v\nwant %+v", rollup, want)
	}
	if _, has := topologyOf(t, views["a"])["rollup"]; has {
		t.Fatal("a childless contract is no initiative")
	}
	xRollup, _ := topologyOf(t, views["x"])["rollup"].(map[string]any)
	if xRollup["descendants"] != 1.0 || xRollup["held"] != 1.0 {
		t.Fatalf("x's rollup counts b as held: %+v", xRollup)
	}
	rep, _ := s.report(out)["topology"].(map[string]any)
	inits, _ := rep["initiatives"].([]any)
	if len(inits) != 2 || inits[0].(map[string]any)["subject"] != "init" || inits[1].(map[string]any)["subject"] != "x" || !reflect.DeepEqual(inits[0].(map[string]any)["rollup"], want) {
		t.Fatalf("the report lists both initiatives with the same rollup: %+v", inits)
	}
	// The cache mirrors the same derivation.
	var cached map[string]any
	if err := json.Unmarshal([]byte(s.cacheQuery(out, `SELECT rollup FROM topology_state WHERE subject = 'init'`)), &cached); err != nil || !reflect.DeepEqual(cached, want) {
		t.Fatalf("the cache's rollup is the view's: %v %+v", err, cached)
	}
	if v := s.cacheQuery(out, `SELECT initiative FROM topology_state WHERE subject = 'a'`); v != "0" {
		t.Fatalf("a is no initiative in the cache: %q", v)
	}
	var cachedReport map[string]any
	if err := json.Unmarshal([]byte(s.cacheQuery(out, `SELECT value FROM report WHERE key = 'topology'`)), &cachedReport); err != nil || !reflect.DeepEqual(cachedReport, rep) {
		t.Fatalf("the cache's report section is the view's: %v", err)
	}
	if v := s.cacheQuery(out, `SELECT COUNT(*) FROM relations`); v != "7" {
		t.Fatalf("seven relation rows (six parents, one link): %q", v)
	}
	// Rebuilding the same prefix is byte-identical, cache included.
	out2 := s.rebuild()
	for _, name := range []string{"contracts", "queue", "report", "cache"} {
		if !bytes.Equal(s.viewBytes(out, name), s.viewBytes(out2, name)) {
			t.Fatalf("%s is not byte-identical across rebuilds", name)
		}
	}
}

// conformance: III.F row 12 (ancestry half) — TestGoalAncestryWarnsOnlyOpenUnanchoredWork:
// an open child inheriting a commit-anchored mission from an ancestor
// does not warn; an open orphan and a tree with no aligned ancestor do
// warn with their traversed chains; terminal unanchored work does not
// warn; a raw-pushed out-of-grant or cyclic relation stays anomalous
// and cannot silence the warning.
func TestGoalAncestryWarnsOnlyOpenUnanchoredWork(t *testing.T) {
	s := newTopologyStand(t)
	s.file("root", "child", "orphan", "top", "under", "gone")
	mission := "docs/mission.md @ " + s.specCommit
	s.must("align", "root", "mission", mission)
	s.must("parent", "child", "parent", "root")
	s.must("parent", "under", "parent", "top")
	s.append(s.priv, "contract.cancelled", "gone", `{}`)
	out := s.rebuild()
	views := s.contracts(out)
	if tv := topologyOf(t, views["child"]); tv["goal_ancestry_warning"] != nil || tv["mission_anchor"] != mission || tv["mission_from"] != "root" {
		t.Fatalf("the child inherits the mission and does not warn: %+v", tv)
	}
	if tv := topologyOf(t, views["root"]); tv["goal_ancestry_warning"] != nil || tv["mission_from"] != "root" || tv["mission"].(map[string]any)["target"] != mission {
		t.Fatalf("the root carries its own mission: %+v", tv)
	}
	for c, chain := range map[string][]string{"orphan": {"orphan"}, "top": {"top"}, "under": {"under", "top"}} {
		w, _ := topologyOf(t, views[c])["goal_ancestry_warning"].(map[string]any)
		if w == nil || !reflect.DeepEqual(strs(w["ancestry"]), chain) {
			t.Fatalf("%s warns with its traversed chain: %+v", c, w)
		}
	}
	if tv := topologyOf(t, views["gone"]); tv["goal_ancestry_warning"] != nil {
		t.Fatalf("terminal unanchored work does not warn: %+v", tv)
	}
	rep, _ := s.report(out)["topology"].(map[string]any)
	warned := []string{}
	for _, w := range rep["goal_ancestry_warnings"].([]any) {
		warned = append(warned, w.(map[string]any)["subject"].(string))
	}
	if !reflect.DeepEqual(warned, []string{"orphan", "top", "under"}) {
		t.Fatalf("the report warns on exactly the open unanchored work: %v", warned)
	}
	if v := s.cacheQuery(out, `SELECT ancestry FROM goal_ancestry_warnings WHERE subject = 'under'`); v != `["under","top"]` {
		t.Fatalf("the cache carries the chain: %q", v)
	}
	if v := s.cacheQuery(out, `SELECT COUNT(*) FROM goal_ancestry_warnings`); v != "3" {
		t.Fatalf("three warnings cached: %q", v)
	}
	// A raw-pushed mission under a claim key, and a raw-pushed cycle
	// under the operator's own key, are anomalies: the orphan still
	// warns, top keeps no parent, and the report names both.
	c := s.ctx()
	rawAppendAt(t, s.ld, workerRawKey(22), c.Active, topology.AlignVerb, "orphan", `{"mission": "`+mission+`"}`)
	rawAppendAt(t, s.ld, workerRawKey(22), c.Active, topology.ParentVerb, "orphan", `{"parent": "root"}`)
	out = s.rebuild()
	views = s.contracts(out)
	if w, _ := topologyOf(t, views["orphan"])["goal_ancestry_warning"].(map[string]any); w == nil {
		t.Fatal("a raw-pushed out-of-grant mission or parent silences nothing")
	}
	rep, _ = s.report(out)["topology"].(map[string]any)
	if anomalies, _ := rep["anomalies"].([]any); len(anomalies) != 2 {
		t.Fatalf("the report names the two anomalies: %+v", anomalies)
	}
	if v := s.cacheQuery(out, `SELECT COUNT(*) FROM topology_anomalies`); v != "2" {
		t.Fatalf("the cache mirrors the anomalies: %q", v)
	}
	// A cycle pushed raw under the operator's key is judged at its
	// position too: it is an anomaly, not an edge.
	if e, code := s.relate("parent", "top", "parent", "under"); code != 3 || e.Error == nil || e.Error.Code != "topology_refused" {
		t.Fatalf("the boundary refuses the cycle: %d %+v", code, e.Error)
	}
	s.append(s.priv, "contract.cancelled", "orphan", `{}`)
	out = s.rebuild()
	rep, _ = s.report(out)["topology"].(map[string]any)
	warned = warned[:0]
	for _, w := range rep["goal_ancestry_warnings"].([]any) {
		warned = append(warned, w.(map[string]any)["subject"].(string))
	}
	if !reflect.DeepEqual(warned, []string{"top", "under"}) {
		t.Fatalf("cancelled work leaves the warnings: %v", warned)
	}
}
