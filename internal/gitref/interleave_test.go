package gitref

// The exhaustive interleaving check of the append loop and halt
// (plans/os-07e6e76c.md; SEED-NEXT.md section II.1, ordering by admitted
// ancestry, freshness and the monotonic-head rule, genesis and halt;
// charter III.A row 7). The tree's evidence about the append protocol
// was executions: two appenders in the race drill, twenty-four in the
// perf gate, three rollback drills. This file enumerates. An abstract
// state machine whose alphabet is the loop's own steps (fetch, attempt,
// the cooperative rollback adversary; halt and lift as drafts) is
// walked depth-first over every interleaving for small N, memoized on
// state, with the plan's properties checked at every state and every
// terminal state. The model is kept honest by replaying its traces
// through the real client in interleave_replay_test.go.
//
// Sizes (plan D5): the fast gate enumerates the four configurations
// below; the weekly schedule adds four writers with four attempts
// (.github/workflows/perf-scale.yml). Times are recorded in
// docs/decisions.md.

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/explore"
)

var (
	modelWriters  = flag.Int("writers", 0, "enumerate one extra configuration of this many writers (0: the fast-gate set only)")
	modelAttempts = flag.Int("attempts", 0, "attempts per writer for the -writers configuration")
)

// draftKind is what a writer carries: a normal record, a halt, a lift.
type draftKind int

const (
	normalDraft draftKind = iota
	haltDraft
	liftDraft
)

func (k draftKind) String() string {
	return [...]string{"normal", "halt", "lift"}[k]
}

// outcome is how a writer's run ends, in the loop's own vocabulary
// (plan D6): landed, ErrRetriesSpent, halt.HaltedError from
// re-validation, ErrHeadRegression from the fetch.
type outcome int

const (
	pending outcome = iota
	landed
	retriesSpent
	haltedOut
	headRegression
)

func (o outcome) String() string {
	return [...]string{"pending", "landed", "retries-spent", "halted", "head-regression"}[o]
}

type phase int

const (
	idle phase = iota
	fetched
	done
)

// node is one commit on the remote: its parent, the writer that landed
// it, and what it carries. Node 0 is genesis. Nodes are append-only
// across a run; the remote's tip names which one is current, and a
// rollback moves the tip to an ancestor without deleting anything, so a
// later push by ancestry can bring the old line back (plan D2).
type node struct {
	parent int
	writer int
	kind   draftKind
}

type writer struct {
	kind      draftKind
	attempts  int
	phase     phase
	fetched   int
	persisted int // -1: none
	outcome   outcome
	landedAt  int // node id
	// afterRollback marks a landing that happened once the remote had
	// rolled back: the healing push, or a fresh writer on the
	// rolled-back tip. lostAfterRollback marks a race lost once the
	// remote had rolled back: the in-flight writer of the fork shape,
	// whose next fetch refuses regression.
	afterRollback     bool
	lostAfterRollback bool
}

// step is one atomic move of the model; a trace is a list of them.
type step struct {
	action string // fetch, attempt, rollback
	writer int
	to     int // rollback target node
}

func (s step) String() string {
	switch s.action {
	case "rollback":
		return fmt.Sprintf("rollback->%d", s.to)
	default:
		return fmt.Sprintf("%s(w%d)", s.action, s.writer)
	}
}

type state struct {
	nodes        []node
	tip          int
	enforced     bool
	rolledBack   bool
	rollbackFrom int
	rollbackTo   int
	writers      []writer
}

func (s *state) clone() *state {
	c := *s
	c.nodes = append([]node(nil), s.nodes...)
	c.writers = append([]writer(nil), s.writers...)
	return &c
}

func (s *state) Key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "t%d r%v %d>%d|", s.tip, s.rolledBack, s.rollbackFrom, s.rollbackTo)
	for _, n := range s.nodes {
		fmt.Fprintf(&b, "%d.%d.%d;", n.parent, n.writer, n.kind)
	}
	b.WriteString("|")
	for _, w := range s.writers {
		fmt.Fprintf(&b, "%d.%d.%d.%d.%d.%d.%d.%v.%v;", w.kind, w.attempts, w.phase, w.fetched, w.persisted, w.outcome, w.landedAt, w.afterRollback, w.lostAfterRollback)
	}
	return b.String()
}

// descends reports whether a is b or a descendant of b: the
// fast-forward test a plain git push and the hook's ref rule both make
// (merge-base --is-ancestor b a).
func (s *state) descends(a, b int) bool {
	for a != -1 {
		if a == b {
			return true
		}
		if a == 0 {
			return false
		}
		a = s.nodes[a].parent
	}
	return false
}

// ancestry lists the nodes from genesis to n, the chain a writer that
// fetched n sees.
func (s *state) ancestry(n int) []int {
	var out []int
	for n != -1 {
		out = append(out, n)
		if n == 0 {
			break
		}
		n = s.nodes[n].parent
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// haltedAt replays the halt state along the ancestry of n, as
// halt.StateAt replays it along the chain.
func (s *state) haltedAt(n int) bool {
	halted := false
	for _, id := range s.ancestry(n) {
		switch s.nodes[id].kind {
		case haltDraft:
			halted = true
		case liftDraft:
			halted = false
		}
	}
	return halted
}

func (s *state) Terminal() bool {
	for _, w := range s.writers {
		if w.phase != done {
			return false
		}
	}
	return true
}

// enabled lists the steps the model can take from s.
func (s *state) Enabled() []step {
	var out []step
	for i, w := range s.writers {
		switch w.phase {
		case idle:
			out = append(out, step{action: "fetch", writer: i})
		case fetched:
			out = append(out, step{action: "attempt", writer: i})
		}
	}
	if !s.enforced && !s.rolledBack && s.tip != 0 {
		for _, id := range s.ancestry(s.tip) {
			if id != s.tip {
				out = append(out, step{action: "rollback", to: id})
			}
		}
	}
	return out
}

// apply returns the successor state. It is the model's whole semantics,
// and every rule in it cites the loop it models.
func (s *state) Apply(st step) *state {
	n := s.clone()
	switch st.action {
	case "fetch":
		w := &n.writers[st.writer]
		// gitref.Client.Fetch: a tip that does not contain the
		// persisted verified head refuses with ErrHeadRegression.
		if w.persisted != -1 && !n.descends(n.tip, w.persisted) {
			w.outcome, w.phase = headRegression, done
			return n
		}
		w.fetched, w.phase = n.tip, fetched
	case "attempt":
		w := &n.writers[st.writer]
		// gitref.Client.attempt: re-validate against the fetched chain
		// (the halted rule refuses every non-lift draft, and AppendLoop
		// returns every non-race error), persist the fetched tip, then
		// push: a plain push lands when the fetched tip descends from
		// the remote's current tip, else ErrNonFastForward and a retry.
		if n.haltedAt(w.fetched) && w.kind != liftDraft {
			w.outcome, w.phase = haltedOut, done
			return n
		}
		w.persisted = w.fetched
		if n.descends(w.fetched, n.tip) {
			n.nodes = append(n.nodes, node{parent: w.fetched, writer: st.writer, kind: w.kind})
			n.tip = len(n.nodes) - 1
			w.persisted = n.tip
			w.landedAt = n.tip
			w.afterRollback = n.rolledBack
			w.outcome, w.phase = landed, done
			return n
		}
		w.attempts--
		if n.rolledBack {
			w.lostAfterRollback = true
		}
		if w.attempts == 0 {
			w.outcome, w.phase = retriesSpent, done
			return n
		}
		w.phase = idle
	case "rollback":
		// The cooperative-posture adversary: the remote's ref moved to
		// an ancestor by a writer that bypassed the client. An enforced
		// remote refuses this as non-fast-forward, so the step is never
		// enabled there.
		n.rolledBack = true
		n.rollbackFrom, n.rollbackTo = n.tip, st.to
		n.tip = st.to
	}
	return n
}

// config is one enumerated configuration.
type config struct {
	name     string
	kinds    []draftKind
	attempts int
	enforced bool
}

func (c config) initial() *state {
	s := &state{nodes: []node{{parent: -1, writer: -1}}, tip: 0, enforced: c.enforced}
	for _, k := range c.kinds {
		s.writers = append(s.writers, writer{kind: k, attempts: c.attempts, persisted: -1})
	}
	return s
}

// exploration is what the explorer returns for a configuration.
type exploration struct {
	states    int
	terminals int
	// traces holds one trace per terminal state, the first path the
	// depth-first walk found to it.
	traces [][]step
	finals []*state
	// Named shapes (plan P3), classified by the roles the plan names
	// rather than by the chain's shape alone (review finding on the
	// task PR): healing is an in-flight writer, fetched before the
	// rollback, landing after it and restoring the old line; the fork
	// is a fresh writer landing on the rolled-back tip first, after
	// which an in-flight writer loses the race and then refuses
	// regression at its re-fetch. Three roles, so the cooperative
	// configuration carries three writers.
	healing, fork []int // indexes into traces
	spent         int   // terminal states with a retries-spent writer
}

// explore walks every interleaving from the configuration's initial
// state, memoized on state, checking the properties at every state and
// every terminal state (plan D3). A property failure names the trace.
func walkModel(t *testing.T, c config) *exploration {
	t.Helper()
	ex := &exploration{}
	res, err := explore.Walk[*state, step](c.initial(), explore.Hooks[*state, step]{
		AtState: func(s *state, path []step) { checkInvariants(t, c, s, path) },
		AtTerminal: func(s *state, path []step) {
			checkTerminal(t, c, s, path)
			ex.traces = append(ex.traces, path)
			ex.finals = append(ex.finals, s)
			idx := len(ex.traces) - 1
			if s.isHealing() {
				ex.healing = append(ex.healing, idx)
			}
			if s.isFork() {
				ex.fork = append(ex.fork, idx)
			}
			for _, w := range s.writers {
				if w.outcome == retriesSpent {
					ex.spent++
					break
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	ex.states = res.States
	ex.terminals = res.Terminals()
	return ex
}

// isHealing: the remote rolled back, and a writer that fetched the old
// line before the rollback landed on it afterwards, so the terminal
// chain carries the rolled-back records again.
func (s *state) isHealing() bool {
	if !s.rolledBack || !s.descends(s.tip, s.rollbackFrom) {
		return false
	}
	for _, w := range s.writers {
		if w.outcome == landed && w.afterRollback && s.descends(w.fetched, s.rollbackFrom) {
			return true
		}
	}
	return false
}

// isFork: the remote rolled back, a fresh writer landed on the
// rolled-back tip so the terminal chain leaves the old line behind, and
// an in-flight writer of the old line lost a race after the rollback
// and then refused regression at its re-fetch.
func (s *state) isFork() bool {
	if !s.rolledBack || s.tip == s.rollbackTo || s.descends(s.tip, s.rollbackFrom) {
		return false
	}
	for _, w := range s.writers {
		if w.outcome == headRegression && w.lostAfterRollback && !s.descends(s.tip, w.persisted) {
			return true
		}
	}
	return false
}

func traceText(path []step) string {
	parts := make([]string, len(path))
	for i, s := range path {
		parts[i] = s.String()
	}
	return strings.Join(parts, " ")
}

// checkInvariants holds at every state: P3's monotonic head (a
// persisted head only ever moves to a descendant of itself, which the
// key records as the state, so the check reads each writer against the
// nodes), P5's outcome vocabulary, and the structural facts the later
// properties read.
func checkInvariants(t *testing.T, c config, s *state, path []step) {
	t.Helper()
	for i, w := range s.writers {
		if w.outcome == landed && !s.descends(w.persisted, w.landedAt) {
			t.Fatalf("%s: P3: w%d landed node %d but persists %d, not a descendant\n%s", c.name, i, w.landedAt, w.persisted, traceText(path))
		}
		if w.phase == done && w.outcome == pending {
			t.Fatalf("%s: P5: w%d is done with no outcome\n%s", c.name, i, traceText(path))
		}
		if w.outcome == retriesSpent && w.attempts != 0 {
			t.Fatalf("%s: P5: w%d spent its retries with %d attempts left\n%s", c.name, i, w.attempts, traceText(path))
		}
	}
	if s.enforced && s.rolledBack {
		t.Fatalf("%s: an enforced remote rolled back\n%s", c.name, traceText(path))
	}
}

// checkTerminal holds at every terminal state: P1 under an enforced
// remote, P4 on every chain, P5's exactly-one-outcome, and P3's
// refusal after a rollback.
func checkTerminal(t *testing.T, c config, s *state, path []step) {
	t.Helper()
	chain := s.ancestry(s.tip)
	if s.enforced {
		// P1: genesis plus every landed record once, in landing order.
		var expect []int
		for id := 1; id < len(s.nodes); id++ {
			expect = append(expect, id)
		}
		got := chain[1:]
		if fmt.Sprint(got) != fmt.Sprint(expect) {
			t.Fatalf("%s: P1: the terminal chain %v is not genesis plus every landed record %v\n%s", c.name, chain, expect, traceText(path))
		}
		for i, w := range s.writers {
			if w.outcome == landed && !s.descends(s.tip, w.landedAt) {
				t.Fatalf("%s: P1: w%d landed node %d, absent from the terminal chain %v\n%s", c.name, i, w.landedAt, chain, traceText(path))
			}
		}
	}
	// P4: no normal record follows a halt before a lift.
	halted := false
	for _, id := range chain[1:] {
		switch s.nodes[id].kind {
		case haltDraft:
			halted = true
		case liftDraft:
			halted = false
		case normalDraft:
			if halted {
				t.Fatalf("%s: P4: node %d landed after a halt with no lift, chain %v\n%s", c.name, id, chain, traceText(path))
			}
		}
	}
	// P5: every writer ended in exactly one outcome.
	for i, w := range s.writers {
		if w.outcome == pending {
			t.Fatalf("%s: P5: w%d has no outcome at a terminal state\n%s", c.name, i, traceText(path))
		}
	}
	// P3 after a rollback: a writer that landed once the remote had
	// rolled back did so on the terminal chain (the healing push, or
	// the fresh writer on the rolled-back tip); a writer whose
	// persisted head is off the terminal chain landed before the
	// rollback or not at all.
	if s.rolledBack {
		for i, w := range s.writers {
			if w.afterRollback && !s.descends(s.tip, w.landedAt) {
				t.Fatalf("%s: P3: w%d landed node %d after the rollback, off the terminal chain %v\n%s", c.name, i, w.landedAt, chain, traceText(path))
			}
			if w.outcome == landed && !w.afterRollback && !s.descends(s.tip, w.persisted) && s.descends(s.tip, s.rollbackFrom) {
				t.Fatalf("%s: P3: w%d landed before the rollback, its head %d is off the chain, yet the chain was healed %v\n%s", c.name, i, w.persisted, chain, traceText(path))
			}
		}
	}
}

// fastConfigs are the four fast-gate configurations (plan D5).
func fastConfigs() []config {
	return []config{
		{name: "2x2-enforced", kinds: []draftKind{normalDraft, normalDraft}, attempts: 2, enforced: true},
		{name: "3x3-enforced", kinds: []draftKind{normalDraft, normalDraft, normalDraft}, attempts: 3, enforced: true},
		{name: "2+halt-x3-enforced", kinds: []draftKind{normalDraft, normalDraft, haltDraft}, attempts: 3, enforced: true},
		{name: "3x2-cooperative-rollback", kinds: []draftKind{normalDraft, normalDraft, normalDraft}, attempts: 2, enforced: false},
	}
}

func uniformConfig(n, attempts int) config {
	kinds := make([]draftKind, n)
	return config{name: fmt.Sprintf("%dx%d-enforced", n, attempts), kinds: kinds, attempts: attempts, enforced: true}
}

// conformance: charter II.1 and III.A row 7 (plans/os-07e6e76c.md D3):
// every interleaving of the fast-gate configurations satisfies P1, P3,
// P4 and P5; the A >= N lemma holds and its counterexample at A = N-1
// exists; the healing and fork shapes of P3 are reachable.
func TestAppendInterleavings(t *testing.T) {
	configs := fastConfigs()
	if *modelWriters > 0 {
		configs = append(configs, uniformConfig(*modelWriters, *modelAttempts))
	}
	for _, c := range configs {
		ex := walkModel(t, c)
		t.Logf("%s: %d states, %d terminal states, %d healing, %d fork, %d spent", c.name, ex.states, ex.terminals, len(ex.healing), len(ex.fork), ex.spent)
		if ex.terminals == 0 {
			t.Fatalf("%s: no terminal state", c.name)
		}
		if c.enforced && len(c.kinds) == c.attempts && !hasKind(c.kinds, haltDraft) && ex.spent != 0 {
			t.Fatalf("%s: the A >= N lemma fails: %d terminal states spend a writer's retries", c.name, ex.spent)
		}
		if testing.Verbose() && !c.enforced {
			for _, i := range ex.healing[:min(2, len(ex.healing))] {
				t.Logf("%s: healing trace: %s", c.name, traceText(ex.traces[i]))
			}
			for _, i := range ex.fork[:min(2, len(ex.fork))] {
				t.Logf("%s: fork trace: %s", c.name, traceText(ex.traces[i]))
			}
		}
		if !c.enforced {
			if len(ex.healing) == 0 {
				t.Fatalf("%s: P3's healing shape (an in-flight push restores the rolled-back line) is unreachable", c.name)
			}
			if len(ex.fork) == 0 {
				t.Fatalf("%s: P3's fork shape (a fresh writer lands on the rolled-back tip first, an in-flight writer loses and then refuses regression) is unreachable", c.name)
			}
		}
	}
	// The lemma's counterexample: at A = N-1 some interleaving spends a
	// writer, or the model races nothing.
	for _, n := range []int{2, 3} {
		c := uniformConfig(n, n-1)
		ex := walkModel(t, c)
		t.Logf("%s: %d states, %d terminal states, %d spent", c.name, ex.states, ex.terminals, ex.spent)
		if ex.spent == 0 {
			t.Fatalf("%s: no interleaving spends a writer's retries at A = N-1; the model is not racing anything", c.name)
		}
	}
}

func hasKind(kinds []draftKind, k draftKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}
