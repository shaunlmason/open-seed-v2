// Package externalfact holds the one closed vocabulary of external
// facts (plans/os-b45c308d.md D4; SEED-NEXT.md §II.4, III.D row 7;
// spec/external-facts.md): every protocol event whose payload
// asserts something an external authority did, the governed signer
// class that may record it, where it is read from, and the only
// ledger consequence it may have. The catalog and the spec table are
// pinned both ways by test, every row's signer set is enforced at
// admission through the keyring's capability rows, and nothing here
// is control: an observation records what happened elsewhere and
// never performs it.
package externalfact

import "sort"

// Fact is one catalogued external-fact verb.
type Fact struct {
	// Verb is the protocol verb.
	Verb string
	// Authority names the external system whose act the fact asserts.
	Authority string
	// Signers is the capability set any one of which admits the verb,
	// the keyring row, in the keyring's order.
	Signers []string
	// Source is how the fact is read before it is recorded.
	Source string
	// Consequence is the only ledger-side effect the fact may have; a
	// fact with none says so.
	Consequence string
	// Since is the protocol version the verb is defined from.
	Since string
}

// Catalog is the closed external-fact vocabulary, sorted by verb.
var Catalog = []Fact{
	{
		Verb:        "check.observed",
		Authority:   "the forge's checks and review on the head under review",
		Signers:     []string{"observer", "operator"},
		Source:      "`seed check observe --forge` through the read-only source, or given by hand",
		Consequence: "the subject's standing observation; while red on the submission's head, `merge.requested` refuses and the dispatch lane owes `submission.unmergeable`, which only `contract.returned` discharges; never a verdict, a transition or a discharge",
		Since:       "seed/8",
	},
	{
		Verb:        "curation.lesson.promoted",
		Authority:   "the lesson pull request's merge",
		Signers:     []string{"observer", "operator"},
		Source:      "the merged pull request the payload anchors",
		Consequence: "the hypothesis it cites is promoted for the knowledge projection; no lifecycle state changes",
		Since:       "seed/1",
	},
	{
		Verb:        "curation.lesson.retired",
		Authority:   "the revert's merge, a later promotion, or the expiry the promotion declared",
		Signers:     []string{"observer", "operator"},
		Source:      "the merged revert or the superseding promotion the payload cites",
		Consequence: "the cited promotion is revoked for the knowledge projection; no lifecycle state changes",
		Since:       "seed/1",
	},
	{
		Verb:        "merge.observed",
		Authority:   "the forge's merge of the pull request",
		Signers:     []string{"observer", "operator"},
		Source:      "`seed merge observe --forge` through the read-only source, refusing an unmerged pull request, or given by hand",
		Consequence: "review to done, and only behind an independently valid pass or override and a citing `merge.requested`; a merge observed without them is a reconciliation divergence, never a laundered verdict",
		Since:       "seed/1",
	},
	{
		Verb:        "plan.approved",
		Authority:   "the plan pull request's merge, a gate a human holds",
		Signers:     []string{"operator"},
		Source:      "the merged plan pull request the payload anchors",
		Consequence: "the plan the tier requires is on record; no lifecycle state changes",
		Since:       "seed/1",
	},
	{
		Verb:        "workflow.merged",
		Authority:   "the workflow pull request's merge",
		Signers:     []string{"observer", "operator"},
		Source:      "the merged pull request the payload anchors",
		Consequence: "the proposal it cites is registered for the flywheel; no lifecycle state changes",
		Since:       "seed/1",
	},
}

// Lookup finds a catalogued verb.
func Lookup(verb string) (Fact, bool) {
	for _, f := range Catalog {
		if f.Verb == verb {
			return f, true
		}
	}
	return Fact{}, false
}

// Verbs lists the catalogued verbs, sorted.
func Verbs() []string {
	out := make([]string, 0, len(Catalog))
	for _, f := range Catalog {
		out = append(out, f.Verb)
	}
	sort.Strings(out)
	return out
}
