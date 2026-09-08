package admit

// The seams the qualification derivation reads (plans/os-03e47abb.md
// D5; internal/eval): the boundary-validated verdicts, the window a
// submission closed, and the configuration its admitted start
// declared. Exported thinly over the rule set's own helpers so the
// derivation and the admission rule read one implementation.

import (
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
)

// AuthenticPass is the subject's latest admitted verdict when it is a
// pass rendered by a key that held a verdict grant at the verdict's own
// position and is disjoint from the implementer; nil otherwise. The
// admission-side half of D2: the mint decision also recomputes the
// receipt.
func AuthenticPass(c *Context, subject string, s transition.SubjectState) *transition.VerdictFact {
	return authenticPass(c, subject, s)
}

// AuthenticFail is a fail verdict on the subject's current submission
// window whose signer passed the verifier boundary at the verdict's
// own position, nil when none stands.
func AuthenticFail(c *Context, subject string, s transition.SubjectState) *transition.VerdictFact {
	return qualifiedFail(c, subject, s)
}

// SubmissionWindow is the claim window the submission at that position
// closed: its fence and its holder, both record facts.
func SubmissionWindow(records []*event.Record, subject string, submission int) (fence int, holder string, ok bool) {
	return submissionWindow(records, subject, submission)
}

// WindowDeclaration is the tuple the admitted run.started in the
// window at fence declared, nil when no valid start declared one.
func WindowDeclaration(c *Context, subject string, s transition.SubjectState, fence int) *tuple.Tuple {
	return windowDeclaration(c, subject, s, fence)
}
