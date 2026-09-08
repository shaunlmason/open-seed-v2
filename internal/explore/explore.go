// Package explore is the interleaving explorer: a depth-first walk over
// every reachable state of a small concurrent model, memoized on a
// caller-supplied key, with a hook at every state and another at every
// terminal one (plans/os-873b5153.md D1).
//
// It was extracted from the append-loop model
// (plans/os-07e6e76c.md, internal/gitref/interleave_test.go),
// whose shape proved generic: only the alphabet and the properties
// were ever specific to that protocol. Three models now share this one
// walker rather than three copies of it drifting apart, which is the
// same reason the admission rule set has one implementation and two
// consumers (SEED-NEXT.md §II.10).
//
// The package holds no protocol knowledge and takes no dependencies
// beyond the standard library. It is production code by location and
// test-only in use: nothing outside a _test.go file calls it, which
// plans/os-873b5153.md D8 names as the one production addition that
// card makes.
package explore

import "fmt"

// State is one state of a model. Implementations are values the walker
// may hold across the walk, so Apply must return a NEW state rather
// than mutating the receiver: the walker keeps predecessors alive to
// report the path that reached a failure.
type State[S any, T any] interface {
	// Key identifies the state for memoization. Two states with the
	// same key are treated as the same state, so the key must carry
	// every field a property reads. A key that omits a field silently
	// prunes the interleavings that differ only in it.
	Key() string
	// Enabled lists the steps the model may take from here. A
	// non-terminal state with no enabled step is a modelling error the
	// walker reports rather than silently accepting as terminal.
	Enabled() []T
	// Apply returns the successor state for one step.
	Apply(T) S
	// Terminal reports whether the walk ends here.
	Terminal() bool
}

// Hooks are the caller's property checks. Both are optional.
//
// The path passed to either hook is valid for the duration of the call
// and no longer: the walker reuses its backing array across sibling
// branches. A hook that keeps a path (to report it later, say) must
// copy it. AtTerminal is the exception and receives a copy already,
// since the walker retains that one in Result.Traces.
type Hooks[S any, T any] struct {
	// AtState runs at every distinct state, with the path that first
	// reached it. Invariants that must hold everywhere go here.
	AtState func(s S, path []T)
	// AtTerminal runs at every terminal state, with its path. Whole-run
	// properties go here.
	AtTerminal func(s S, path []T)
}

// Result is what a walk observed.
type Result[S any, T any] struct {
	// States is the number of distinct states visited.
	States int
	// Traces and Finals are parallel: Traces[i] is the first path the
	// walk found to Finals[i]. Only terminal states are recorded.
	Traces [][]T
	Finals []S
}

// Terminals is the number of terminal states the walk reached.
func (r *Result[S, T]) Terminals() int { return len(r.Finals) }

// Walk explores every interleaving reachable from the initial state,
// depth-first, visiting each distinct state once. It returns an error
// only for a modelling fault it can detect itself (a non-terminal state
// with nothing enabled); property failures are the hooks' to report,
// which keeps this package free of any testing dependency.
func Walk[S State[S, T], T any](initial S, h Hooks[S, T]) (*Result[S, T], error) {
	res := &Result[S, T]{}
	seen := map[string]bool{}
	var err error
	var walk func(s S, path []T)
	walk = func(s S, path []T) {
		if err != nil {
			return
		}
		k := s.Key()
		if seen[k] {
			return
		}
		seen[k] = true
		res.States++
		if h.AtState != nil {
			h.AtState(s, path)
		}
		if s.Terminal() {
			trace := append([]T(nil), path...)
			res.Traces = append(res.Traces, trace)
			res.Finals = append(res.Finals, s)
			if h.AtTerminal != nil {
				h.AtTerminal(s, trace)
			}
			return
		}
		steps := s.Enabled()
		if len(steps) == 0 {
			err = fmt.Errorf("explore: a non-terminal state has no enabled step, so the walk cannot terminate this path: %s", k)
			return
		}
		for _, st := range steps {
			// The path slice is reused across siblings by append, so a
			// step must not be retained past its recursion; every
			// consumer that keeps a path copies it (Traces above, and
			// the hooks by convention).
			walk(s.Apply(st), append(path, st))
		}
	}
	walk(initial, nil)
	return res, err
}
