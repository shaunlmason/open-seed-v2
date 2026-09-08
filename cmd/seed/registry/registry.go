// Package registry is the one table both of Seed's surfaces are drawn
// from (plans/os-b55e5647.md D2; charter III.I row 3): every verb the
// CLI dispatches and every method the machine protocol serves is a
// row here, so "a verb exists on the protocol iff it exists on the
// CLI" is a property of one table rather than a discipline. The rows
// carry the CLI's own run functions; neither surface owns semantics.
package registry

import (
	"io"
	"sort"
	"strings"
)

// Run is a CLI group's entry point: the arguments after the group
// word, the streams, the exit code — the shape every cmd/seed run
// function has had since Phase 0.
type Run func(args []string, stdout, stderr io.Writer) int

// Group is one top-level verb. Subs lists its subverbs in the CLI's
// own words (empty for a verb that takes flags alone); Run is the
// group's dispatcher, which reads the subverb itself. Transport marks
// the machine surface's own entry (`serve`), which is a CLI verb and
// the protocol, never a method of it.
type Group struct {
	Name      string
	Subs      []string
	Run       Run
	Transport bool
}

// Registry is the table.
type Registry struct {
	groups []Group
	byName map[string]Group
}

// New builds a registry; a repeated group name is a programming error
// caught at construction.
func New(groups ...Group) *Registry {
	r := &Registry{byName: map[string]Group{}}
	for _, g := range groups {
		if _, dup := r.byName[g.Name]; dup {
			panic("registry: group " + g.Name + " registered twice")
		}
		r.groups = append(r.groups, g)
		r.byName[g.Name] = g
	}
	return r
}

// Groups returns the table in registration order.
func (r *Registry) Groups() []Group { return append([]Group(nil), r.groups...) }

// Group looks a top-level verb up.
func (r *Registry) Group(name string) (Group, bool) {
	g, ok := r.byName[name]
	return g, ok
}

// Method is the protocol name of a group and subverb: "ledger.append",
// or "situation" for a group that takes flags alone.
func Method(group, sub string) string {
	if sub == "" {
		return group
	}
	return group + "." + sub
}

// Methods lists every protocol method, sorted: one per subverb, one
// per flags-only group, none for the transport.
func (r *Registry) Methods() []string {
	var out []string
	for _, g := range r.groups {
		if g.Transport {
			continue
		}
		if len(g.Subs) == 0 {
			out = append(out, g.Name)
			continue
		}
		for _, s := range g.Subs {
			out = append(out, Method(g.Name, s))
		}
	}
	sort.Strings(out)
	return out
}

// Resolve maps a method to its group and the argv prefix the group's
// dispatcher expects (the subverb, when the group has them).
func (r *Registry) Resolve(method string) (Group, []string, bool) {
	group, sub, hasSub := strings.Cut(method, ".")
	g, ok := r.byName[group]
	if !ok || g.Transport {
		return Group{}, nil, false
	}
	if len(g.Subs) == 0 {
		if hasSub {
			return Group{}, nil, false
		}
		return g, nil, true
	}
	if !hasSub {
		return Group{}, nil, false
	}
	for _, s := range g.Subs {
		if s == sub {
			return g, []string{sub}, true
		}
	}
	return Group{}, nil, false
}
