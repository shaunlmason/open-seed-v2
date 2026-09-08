// Package topology is the graph beside the lifecycle (plans/os-f0ae2cdf.md;
// SEED-NEXT.md II.4, II.7, II.9; III.F row 12): the dependency,
// hierarchy and goal relations between contracts, folded from four
// additive facts, and everything derived from them. The transition
// table stays the sole authority on lifecycle legality and terminality;
// nothing here moves a subject. Cascades, inherited holds, initiative
// rollups and goal-ancestry warnings are projections over the folded
// graph and the folded lifecycle, computed at a prefix and never
// written back.
//
// One trustworthy fold (D3). A relation fact is trusted only when it
// passed the boundary at its own position: the version was activated,
// the signer held a capability the verb accepts at the prefix it
// appended onto, the payload is strict, and the graph rule held against
// the lifecycle folded at that prefix. A raw-pushed fact that fails any
// of these is an anomaly, kept and named, and it shapes nothing: it
// cannot hide work behind a dependency, hold a claim under a forged
// parent, or lend mission ancestry. Admission and every consuming
// projection read the same fold, so the queue, the claim boundary, the
// poll and the report agree at every prefix.
package topology

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// The four relation facts (D2): additive catalog growth on an existing
// nonterminal contract subject, active from seed/1, under the dispatch
// and operator grants.
const (
	// DependVerb links the subject to a contract it requires.
	DependVerb = "dependency.linked"
	// UndependVerb removes an active link.
	UndependVerb = "dependency.unlinked"
	// ParentVerb sets (or replaces) the subject's parent.
	ParentVerb = "hierarchy.parented"
	// AlignVerb sets (or replaces) the subject's mission anchor.
	AlignVerb = "goal.aligned"
)

// Verbs is the relation catalog in declaration order.
var Verbs = []string{DependVerb, UndependVerb, ParentVerb, AlignVerb}

// IsRelationVerb reports whether the verb is one of the four.
func IsRelationVerb(verb string) bool {
	for _, v := range Verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// Depend is dependency.linked's and dependency.unlinked's strict payload.
type Depend struct {
	Requires string `json:"requires"`
}

// Parent is hierarchy.parented's strict payload.
type Parent struct {
	Parent string `json:"parent"`
}

// Align is goal.aligned's strict payload: the mission is a
// commit-anchored artifact reference, never prose in the ledger.
type Align struct {
	Mission string `json:"mission"`
}

// Error is a topology refusal: the shape, the source, the target, or
// the graph rule is wrong.
type Error struct {
	Verb    string
	Subject string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s on %s refused: %s (spec/topology.md)", e.Verb, e.Subject, e.Reason)
}

// anchorRE is the mission anchor grammar: "<path> @ <commit>", the
// lessons store's own.
var anchorRE = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200} @ [0-9a-f]{7,64}$`)

// IsAnchor reports whether s is a commit-anchored reference whose path
// is clean and relative, so it names a file under the repository and
// cannot climb out of it (the lessons store's own rule).
func IsAnchor(s string) bool {
	if !anchorRE.MatchString(s) {
		return false
	}
	p, _, _ := strings.Cut(s, " @ ")
	return path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../") && !strings.HasPrefix(p, "/")
}

func strict(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the payload")
	}
	return nil
}

// Parse decodes a relation fact's strict payload and returns its
// target: the required contract, the parent, or the mission anchor.
func Parse(verb, subject string, raw []byte) (string, error) {
	refuse := func(reason string) (string, error) {
		return "", &Error{Verb: verb, Subject: subject, Reason: reason}
	}
	switch verb {
	case DependVerb, UndependVerb:
		var p Depend
		if err := strict(raw, &p); err != nil {
			return refuse("the payload is the strict object {requires}: " + err.Error())
		}
		if strings.TrimSpace(p.Requires) == "" {
			return refuse("requires names the contract this subject depends on")
		}
		return p.Requires, nil
	case ParentVerb:
		var p Parent
		if err := strict(raw, &p); err != nil {
			return refuse("the payload is the strict object {parent}: " + err.Error())
		}
		if strings.TrimSpace(p.Parent) == "" {
			return refuse("parent names the contract this subject belongs under")
		}
		return p.Parent, nil
	case AlignVerb:
		var p Align
		if err := strict(raw, &p); err != nil {
			return refuse("the payload is the strict object {mission}: " + err.Error())
		}
		if !IsAnchor(p.Mission) {
			return refuse(fmt.Sprintf("mission %q is not a commit-anchored reference (\"<path> @ <commit>\"): the mission is an artifact the repository holds, never prose in the immutable ledger", p.Mission))
		}
		return p.Mission, nil
	}
	return refuse("not a relation verb")
}

// Relation is one folded relation: who recorded it, where, and what it
// names (a contract, or for a mission its anchor).
type Relation struct {
	Pos    int    `json:"position"`
	Actor  string `json:"actor"`
	Target string `json:"target"`
}

// Anomaly is a relation fact the fold kept but does not trust, with
// the reason: out of grant at its position, malformed, on an unknown
// or terminal source, naming an unknown target, a self edge, a cycle,
// a duplicate link or an absent unlink.
type Anomaly struct {
	Pos     int    `json:"position"`
	Verb    string `json:"verb"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// Graph is the folded relation set: the active dependency links per
// subject, the latest parent, the latest mission anchor, and the
// anomalies.
type Graph struct {
	requires  map[string]map[string]Relation
	parent    map[string]Relation
	mission   map[string]Relation
	order     []string
	seen      map[string]bool
	Anomalies []Anomaly
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{requires: map[string]map[string]Relation{}, parent: map[string]Relation{}, mission: map[string]Relation{}, seen: map[string]bool{}}
}

// Any reports whether the prefix carried a trusted relation.
func (g *Graph) Any() bool { return len(g.order) > 0 }

// Subjects is every subject a trusted relation named as its source,
// first-seen order.
func (g *Graph) Subjects() []string { return append([]string(nil), g.order...) }

func (g *Graph) note(subject string) {
	if !g.seen[subject] {
		g.seen[subject] = true
		g.order = append(g.order, subject)
	}
}

// Requires is the subject's active dependency links, by target.
func (g *Graph) Requires(subject string) []Relation {
	out := make([]Relation, 0, len(g.requires[subject]))
	for _, r := range g.requires[subject] {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// RequiredBy is every subject holding an active link to the target.
func (g *Graph) RequiredBy(target string) []string {
	var out []string
	for s, links := range g.requires {
		if _, ok := links[target]; ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Parent is the subject's latest parent, if any.
func (g *Graph) Parent(subject string) (Relation, bool) {
	r, ok := g.parent[subject]
	return r, ok
}

// Children is every subject whose latest parent is the given one.
func (g *Graph) Children(subject string) []string {
	var out []string
	for s, r := range g.parent {
		if r.Target == subject {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Mission is the subject's own latest mission anchor, if any.
func (g *Graph) Mission(subject string) (Relation, bool) {
	r, ok := g.mission[subject]
	return r, ok
}

// Ancestors walks the parent chain from the subject outward, nearest
// first. The fold refuses cycles, so the walk ends; a visited guard
// keeps it finite regardless.
func (g *Graph) Ancestors(subject string) []string {
	var out []string
	visited := map[string]bool{subject: true}
	cur := subject
	for {
		p, ok := g.parent[cur]
		if !ok || visited[p.Target] {
			return out
		}
		visited[p.Target] = true
		out = append(out, p.Target)
		cur = p.Target
	}
}

// dependsOn reports whether from transitively requires to.
func (g *Graph) dependsOn(from, to string) bool {
	visited := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		for t := range g.requires[cur] {
			if t == to {
				return true
			}
			stack = append(stack, t)
		}
	}
	return false
}

// Apply validates one relation fact against the graph and the
// lifecycle folded at the same prefix, and applies it. The rule (D2):
// the source is a known nonterminal contract; a contract target is
// known; no self edge; no dependency or parent cycle against the
// candidate graph; a link is a set member (a duplicate link and an
// absent unlink refuse); a parent or mission replaces the current one.
// Admission calls it with the candidate; the fold calls it with each
// admitted fact at its own position.
func (g *Graph) Apply(lifecycle *transition.Fold, table *transition.Table, pos int, e *event.Event) error {
	verb, subject := e.Verb, e.Subject
	refuse := func(reason string) error { return &Error{Verb: verb, Subject: subject, Reason: reason} }
	target, err := Parse(verb, subject, e.Payload)
	if err != nil {
		return err
	}
	if lifecycle == nil || table == nil {
		return refuse("no lifecycle is known to resolve the contracts")
	}
	known := func(s string) (string, bool) {
		st, ok := lifecycle.State(s)
		if !ok || st.State == "" {
			return "", false
		}
		return st.State, true
	}
	state, ok := known(subject)
	if !ok {
		return refuse("no contract by that id is on this chain: a relation is a fact on an existing contract")
	}
	if table.Terminal(state) {
		return refuse(fmt.Sprintf("the contract is %s: a relation is recorded on open work, and terminal work neither waits nor holds", state))
	}
	switch verb {
	case DependVerb, UndependVerb, ParentVerb:
		if target == subject {
			return refuse("a contract cannot require or contain itself")
		}
		if _, ok := known(target); !ok {
			return refuse(fmt.Sprintf("%s is no contract on this chain: a relation names work the ledger holds", target))
		}
	}
	switch verb {
	case DependVerb:
		if _, dup := g.requires[subject][target]; dup {
			return refuse(fmt.Sprintf("%s already requires %s: links are a set, and a second link would record a change that changes nothing", subject, target))
		}
		if g.dependsOn(target, subject) {
			return refuse(fmt.Sprintf("%s already requires %s, directly or through others: a dependency cycle would wait on itself forever", target, subject))
		}
		if g.requires[subject] == nil {
			g.requires[subject] = map[string]Relation{}
		}
		g.requires[subject][target] = Relation{Pos: pos, Actor: e.Actor, Target: target}
	case UndependVerb:
		if _, has := g.requires[subject][target]; !has {
			return refuse(fmt.Sprintf("%s does not require %s: there is no link to remove", subject, target))
		}
		delete(g.requires[subject], target)
		if len(g.requires[subject]) == 0 {
			delete(g.requires, subject)
		}
	case ParentVerb:
		for _, a := range g.Ancestors(target) {
			if a == subject {
				return refuse(fmt.Sprintf("%s is already under %s: a hierarchy cycle would contain itself", target, subject))
			}
		}
		g.parent[subject] = Relation{Pos: pos, Actor: e.Actor, Target: target}
	case AlignVerb:
		g.mission[subject] = Relation{Pos: pos, Actor: e.Actor, Target: target}
	}
	g.note(subject)
	return nil
}

// Carries reports whether the prefix holds any relation fact at all,
// trusted or not: the fast path every consumer takes before folding,
// so relation-free chains pay nothing.
func Carries(records []*event.Record) bool {
	for _, rec := range records {
		if IsRelationVerb(rec.Event.Verb) {
			return true
		}
	}
	return false
}

// Fold folds the relation facts of a verified prefix, judging each at
// its own position (D3): activated version, the signer's grant at that
// prefix (the keyring replayed there, the RunStartValid posture: fold
// presence is never proof of admission), the strict shape and the graph
// rule against the lifecycle folded at that prefix. A fact that fails
// is an anomaly; the graph is what passed.
func Fold(records []*event.Record, table *transition.Table) *Graph {
	g := New()
	if table == nil || !Carries(records) {
		return g
	}
	for pos, rec := range records {
		e := &rec.Event
		if !IsRelationVerb(e.Verb) {
			continue
		}
		anomaly := func(reason string) {
			g.Anomalies = append(g.Anomalies, Anomaly{Pos: pos, Verb: e.Verb, Subject: e.Subject, Reason: reason})
		}
		if !version.Activated(e.V) {
			anomaly("recorded before the lifecycle activated")
			continue
		}
		ring, _, err := keyring.StateAt(records[:pos])
		if err != nil || ring == nil || !ring.HasAnyCapability(e.Actor, keyring.AcceptedCapabilities(e.Verb)) {
			anomaly(fmt.Sprintf("%s held none of {%s} at position %d", e.Actor, strings.Join(keyring.AcceptedCapabilities(e.Verb), ", "), pos))
			continue
		}
		if err := g.Apply(table.FoldRecords(records[:pos]), table, pos, e); err != nil {
			anomaly(err.Error())
		}
	}
	return g
}

// Warning is a goal-ancestry warning (D7): an open contract with no
// valid mission anchor on itself or any ancestor, with the chain the
// walk traversed, self first.
type Warning struct {
	Subject  string   `json:"subject"`
	Ancestry []string `json:"ancestry"`
}

// Rollup is an initiative's transitive descendant summary (D7): exact
// integer counts, never a percentage or an estimate.
type Rollup struct {
	Descendants       int            `json:"descendants"`
	ByState           map[string]int `json:"by_state"`
	Terminal          int            `json:"terminal"`
	EffectiveReady    int            `json:"effective_ready"`
	DependencyWaiting int            `json:"dependency_waiting"`
	Held              int            `json:"held"`
	Milestones        int            `json:"milestones"`
}

// Derived is the graph read against a lifecycle fold at one prefix:
// effective readiness, inherited holds, ancestry, rollups and
// warnings, each a function of the two folds and nothing else.
type Derived struct {
	Graph *Graph
	fold  *transition.Fold
	table *transition.Table
}

// Derive pairs a folded graph with the lifecycle folded at the same
// prefix.
func Derive(g *Graph, fold *transition.Fold, table *transition.Table) *Derived {
	if g == nil {
		g = New()
	}
	return &Derived{Graph: g, fold: fold, table: table}
}

// DeriveRecords folds both from the records.
func DeriveRecords(records []*event.Record, table *transition.Table) *Derived {
	return Derive(Fold(records, table), table.FoldRecords(records), table)
}

func (d *Derived) state(subject string) (string, bool) {
	if d.fold == nil {
		return "", false
	}
	s, ok := d.fold.State(subject)
	if !ok || s.State == "" {
		return "", false
	}
	return s.State, true
}

// Open reports whether the subject is a known nonterminal contract.
func (d *Derived) Open(subject string) bool {
	st, ok := d.state(subject)
	return ok && d.table != nil && !d.table.Terminal(st)
}

// Unresolved is the subject's active dependencies whose lifecycle
// state the table does not mark terminal, sorted.
func (d *Derived) Unresolved(subject string) []string {
	out := []string{}
	for _, r := range d.Graph.Requires(subject) {
		st, ok := d.state(r.Target)
		if !ok || d.table == nil || !d.table.Terminal(st) {
			out = append(out, r.Target)
		}
	}
	return out
}

// HeldBy is every ancestor whose folded lifecycle state is blocked,
// nearest first (D5): an inherited hold, whatever route put the
// ancestor there.
func (d *Derived) HeldBy(subject string) []string {
	out := []string{}
	for _, a := range d.Graph.Ancestors(subject) {
		if st, ok := d.state(a); ok && st == "blocked" {
			out = append(out, a)
		}
	}
	return out
}

// EffectiveReady is the predicate every claimable-work surface
// consumes (D4): the subject folds to ready, every active dependency
// is terminal, and no ancestor is blocked. Relation-free subjects
// reduce to the fold's ready.
func (d *Derived) EffectiveReady(subject string) bool {
	st, ok := d.state(subject)
	if !ok || st != "ready" {
		return false
	}
	return len(d.Unresolved(subject)) == 0 && len(d.HeldBy(subject)) == 0
}

// Mission walks self then parents to the first mission anchor (D7),
// returning the anchor and the subject that carries it.
func (d *Derived) Mission(subject string) (anchor, from string, ok bool) {
	if r, has := d.Graph.Mission(subject); has {
		return r.Target, subject, true
	}
	for _, a := range d.Graph.Ancestors(subject) {
		if r, has := d.Graph.Mission(a); has {
			return r.Target, a, true
		}
	}
	return "", "", false
}

// Warning is the subject's goal-ancestry warning, if it is open work
// with no mission on itself or any ancestor; terminal work is omitted.
func (d *Derived) Warning(subject string) (Warning, bool) {
	if !d.Open(subject) {
		return Warning{}, false
	}
	if _, _, ok := d.Mission(subject); ok {
		return Warning{}, false
	}
	return Warning{Subject: subject, Ancestry: append([]string{subject}, d.Graph.Ancestors(subject)...)}, true
}

// Descendants is the subject's transitive children, sorted, the
// subject itself excluded.
func (d *Derived) Descendants(subject string) []string {
	var out []string
	visited := map[string]bool{subject: true}
	stack := []string{subject}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range d.Graph.Children(cur) {
			if visited[c] {
				continue
			}
			visited[c] = true
			out = append(out, c)
			stack = append(stack, c)
		}
	}
	sort.Strings(out)
	return out
}

// IsInitiative reports whether the subject has direct children.
func (d *Derived) IsInitiative(subject string) bool {
	return len(d.Graph.Children(subject)) > 0
}

// Rollup is the initiative's descendant summary, present only for a
// subject with children.
func (d *Derived) Rollup(subject string) (Rollup, bool) {
	if !d.IsInitiative(subject) {
		return Rollup{}, false
	}
	r := Rollup{ByState: map[string]int{}}
	for _, s := range d.Descendants(subject) {
		r.Descendants++
		st, ok := d.state(s)
		if !ok {
			r.ByState["unknown"]++
			continue
		}
		r.ByState[st]++
		if d.table != nil && d.table.Terminal(st) {
			r.Terminal++
		}
		if d.EffectiveReady(s) {
			r.EffectiveReady++
		}
		if st == "ready" && len(d.Unresolved(s)) > 0 {
			r.DependencyWaiting++
		}
		if len(d.HeldBy(s)) > 0 {
			r.Held++
		}
		if d.fold != nil {
			if count, _, has := d.fold.Milestone(s); has {
				r.Milestones += count
			}
		}
	}
	return r, true
}

// Initiatives is every subject with children, sorted.
func (d *Derived) Initiatives() []string {
	seen := map[string]bool{}
	var out []string
	for s := range d.Graph.parent {
		p := d.Graph.parent[s].Target
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Warnings is every goal-ancestry warning over the lifecycle's
// subjects, in the fold's first-appearance order.
func (d *Derived) Warnings() []Warning {
	out := []Warning{}
	if d.fold == nil {
		return out
	}
	for _, s := range d.fold.Subjects() {
		if w, ok := d.Warning(s); ok {
			out = append(out, w)
		}
	}
	return out
}

// Ready is every effectively ready subject in the fold's
// first-appearance order.
func (d *Derived) Ready() []string {
	out := []string{}
	if d.fold == nil {
		return out
	}
	for _, s := range d.fold.Subjects() {
		if d.EffectiveReady(s) {
			out = append(out, s)
		}
	}
	return out
}

// ReadinessDelta is every subject effectively ready after and not
// before (D6): what a prefix-to-prefix read found newly claimable, the
// candidates an advisory wake may name.
func ReadinessDelta(before, after *Derived) []string {
	was := map[string]bool{}
	if before != nil {
		for _, s := range before.Ready() {
			was[s] = true
		}
	}
	out := []string{}
	for _, s := range after.Ready() {
		if !was[s] {
			out = append(out, s)
		}
	}
	return out
}

// NotReadyError is the claim boundary's refusal (D4): the subject folds
// to ready and the table admits the claim, but a dependency is
// unresolved or an ancestor holds it, so it is not effectively ready.
// The queue, the poll and the wake delta hide the same subject for the
// same reason at the same prefix.
type NotReadyError struct {
	Subject    string
	Unresolved []string
	HeldBy     []string
}

func (e *NotReadyError) Error() string {
	var parts []string
	if len(e.Unresolved) > 0 {
		parts = append(parts, fmt.Sprintf("it waits on %s, which %s not terminal", strings.Join(e.Unresolved, ", "), plural(len(e.Unresolved), "is", "are")))
	}
	if len(e.HeldBy) > 0 {
		parts = append(parts, fmt.Sprintf("it is held by %s, %s blocked ancestor", strings.Join(e.HeldBy, ", "), plural(len(e.HeldBy), "a", "each a")))
	}
	return fmt.Sprintf("claim.taken on %s refused: the contract is ready but not effectively ready, %s; it leaves the queue and the poll for the same reason and returns to both when that changes (spec/topology.md)", e.Subject, strings.Join(parts, " and "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Check is the claim boundary's read (D4): nil when the subject is
// effectively ready or the prefix carries no relation at all (the
// fast path relation-free chains take), else the refusal naming what
// stands in the way.
func (d *Derived) Check(subject string) error {
	if !d.Graph.Any() || d.EffectiveReady(subject) {
		return nil
	}
	if st, ok := d.state(subject); !ok || st != "ready" {
		// Not ready at all: the table's refusal, not this one.
		return nil
	}
	return &NotReadyError{Subject: subject, Unresolved: d.Unresolved(subject), HeldBy: d.HeldBy(subject)}
}

// Lifecycle is the fold the derivation reads against.
func (d *Derived) Lifecycle() *transition.Fold { return d.fold }
