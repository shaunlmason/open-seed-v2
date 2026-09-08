// Package loopverb is the registry of the loop acts: the CLI spellings
// a lane uses to work, each paired with the ledger verb it appends and
// the arguments it derives rather than asks for
// (spec/loop-verbs.md; plans/os-cf1c9688.md D3a).
//
// It exists because the acts had no authority. They were `case` arms
// inside cmd/seed's runClaim, runSubmission and runBudget, in package
// main, which nothing else in the tree can import. Any second consumer
// (the lane validator is the first) would have had to write the seven
// names down again, and a list written twice is a list that drifts.
// This package is that authority, with the CLI dispatch and the
// validator as its two consumers: Phase 8's principle, one rule set and
// two consumers, applied to the loop's own vocabulary.
//
// It carries no policy. Whether an act ADMITS at a position is the
// admission boundary's answer, and which capabilities it accepts is the
// keyring's; this package only says which acts exist, what each
// appends, and what each derives.
package loopverb

import (
	"sort"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// The ledger verbs the claim and submission acts append. The budget
// acts reuse internal/transition's constants, which already exist
// because the admission rules name them; these four have no such
// constant because nothing outside the CLI referred to them until now.
const (
	ClaimTakenVerb     = "claim.taken"
	ClaimReleasedVerb  = "claim.released"
	ClaimParkedVerb    = "claim.parked"
	SubmissionMadeVerb = "submission.made"
)

// Act is one loop act: the group and subverb a lane types, the ledger
// verb it appends, and the arguments the act derives for the caller.
type Act struct {
	// Group is the top-level CLI verb: claim, submission, budget.
	Group string
	// Sub is the subverb under it: take, release, park, make, reserve,
	// settle, release.
	Sub string
	// Verb is the ledger verb the act appends.
	Verb string
	// Derives names the arguments the act computes rather than asks
	// for, in the spec's own words. Advisory: it documents the act for
	// a lane fragment and for `seed lane show`, and no code branches on
	// it.
	Derives []string
	// RemoteOnly marks an act that cannot run against a local ledger.
	// claim.taken is the table's one exclusive verb, and only the push
	// round-trip can order two rivals.
	RemoteOnly bool
}

// Name is the act as a lane declares and types it: "claim take".
func (a Act) Name() string { return a.Group + " " + a.Sub }

// acts is the registry. Order is the loop's own: poll and claim, work
// and meter, submit and exit.
var acts = []Act{
	{Group: "claim", Sub: "take", Verb: ClaimTakenVerb, RemoteOnly: true},
	{Group: "budget", Sub: "reserve", Verb: transition.BudgetReserveVerb, Derives: []string{"fence"}},
	{Group: "budget", Sub: "settle", Verb: transition.BudgetSettleVerb, Derives: []string{"fence", "reservation"}},
	{Group: "budget", Sub: "release", Verb: transition.BudgetReleaseVerb, Derives: []string{"fence", "reservation"}},
	{Group: "submission", Sub: "make", Verb: SubmissionMadeVerb, Derives: []string{"fence", "plan", "base"}},
	{Group: "claim", Sub: "release", Verb: ClaimReleasedVerb, Derives: []string{"fence", "base"}},
	{Group: "claim", Sub: "park", Verb: ClaimParkedVerb, Derives: []string{"fence", "base"}},
}

// Acts returns every loop act, in the loop's order.
func Acts() []Act {
	out := make([]Act, len(acts))
	copy(out, acts)
	return out
}

// Names returns every act's name, sorted, for a stable listing and for
// naming the alternatives in a refusal.
func Names() []string {
	out := make([]string, 0, len(acts))
	for _, a := range acts {
		out = append(out, a.Name())
	}
	sort.Strings(out)
	return out
}

// ByName resolves an act from the name a lane declares ("claim take").
func ByName(name string) (Act, bool) {
	for _, a := range acts {
		if a.Name() == name {
			return a, true
		}
	}
	return Act{}, false
}

// Lookup resolves an act from a group and subverb, the shape the CLI
// dispatch has in hand.
func Lookup(group, sub string) (Act, bool) {
	for _, a := range acts {
		if a.Group == group && a.Sub == sub {
			return a, true
		}
	}
	return Act{}, false
}

// Subverbs returns one group's subverbs in registry order: what the
// CLI names when it refuses an unknown one, so the message and the
// dispatch cannot disagree.
func Subverbs(group string) []string {
	var out []string
	for _, a := range acts {
		if a.Group == group {
			out = append(out, a.Sub)
		}
	}
	return out
}

// English joins alternatives the way a refusal reads them: "take,
// release, or park".
func English(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}
