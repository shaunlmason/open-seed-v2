package verdict

// InputFor is the one construction of a receipt computation's input
// from the fold (plans/os-03e47abb.md step 6): seed verdict render and
// check build theirs here, and so does the qualification derivation
// that recomputes a cited receipt before minting. A second copy of
// "which submission, which range, which plan, which acceptance" would
// be a second place for the two to disagree.

import (
	"errors"
	"fmt"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// ErrNoSubmission refuses an input for a subject with no bound
// submission: there is no range to attest.
var ErrNoSubmission = errors.New("no bound submission recorded")

// InputFor builds the Input for the subject's bound submission: the
// packet's mandatory range, the approved plan anchor where one stands,
// and the fold's view of the acceptance. The subject must exist in the
// fold; the caller decides what its state must be.
func InputFor(records []*event.Record, fold *transition.Fold, s transition.SubjectState, subject, repo string, timeout time.Duration) (Input, error) {
	if s.Submission == nil || s.Submission.Pos < 0 || s.Submission.Pos >= len(records) {
		return Input{}, fmt.Errorf("%w for %s", ErrNoSubmission, subject)
	}
	sub := records[s.Submission.Pos]
	p, err := packet.FromPayload(subject, sub.Event.Payload)
	if err != nil {
		return Input{}, err
	}
	anchor, _ := fold.PlanApproved(subject)
	return Input{
		RepoDir:    repo,
		Contract:   subject,
		Base:       p.Base,
		PlanAnchor: anchor,
		Acceptance: s.Acceptance,
		Runner:     Runner{Timeout: timeout},
	}, nil
}
