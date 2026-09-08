// The evidence-grade half of divergence detection
// (plans/os-8a5f14bb.md D2.5). These checks read the artifact store
// and the repository, which projection builds never do, and they are
// the ones that see divergence with no record to derive it from: a
// receipt that no longer retrieves, an observed merge that does not
// descend from the head a verdict attested, a target ref rewritten
// after the observation.
//
// They lived unexported in cmd/seed until the maintenance loop needed
// them too. A second copy would have meant two divergence surfaces
// that drift, and a maintenance pass built on the record-derived half
// alone would report clean over exactly the divergence the charter
// asks it to reconcile — green, and wrong (review finding on #203).
// One implementation, two callers.
package reconcile

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
)

// Evidence runs the checks a projection build cannot: the cited
// receipt must be retrievable intact, the observed merge must relate
// to the attested head by clean ancestry, and the merged commit must
// still sit under the target tip.
func Evidence(id string, s transition.SubjectState, store *artifact.Store, repo string) []Finding {
	return EvidenceAt(id, s, store, repo, Reproduction{})
}

// Reproduction is what the L3 reproduction needs beyond the store and
// the repository (plans/os-99829835.md D5): the chain and its fold,
// which the verifier's input seam reads; Unseal, which opens a sealed
// subject's checks under an identity able to unseal, because a sealed
// receipt recomputes only with its sealed transcripts; and
// NotAttempted, told of a sealed L3 verdict the caller could not open,
// so that a reproduction is never skipped silently (review finding on
// the task PR). With no records or fold the grade covers everything
// but the reproduction.
type Reproduction struct {
	Records      []*event.Record
	Fold         *transition.Fold
	Unseal       func(s transition.SubjectState) (*verdict.SealedInput, error)
	NotAttempted func(subject, why string)
}

func (r Reproduction) notAttempted(subject, why string) {
	if r.NotAttempted != nil {
		r.NotAttempted(subject, why)
	}
}

// EvidenceAt is Evidence with the reproduction's inputs in hand:
// `seed reconcile` and the maintenance loop pass the chain, its fold
// and the unseal hook their key allows.
func EvidenceAt(id string, s transition.SubjectState, store *artifact.Store, repo string, rep Reproduction) []Finding {
	var out []Finding
	// The L3 reproduction (plans/os-99829835.md D1, D5): a verdict that
	// recorded deterministic-first verification cites a receipt that
	// recomputes from the verifier's own checkout to the cited digest;
	// one that does not is an L3 the evidence does not support. A
	// sealed subject's receipt carries its sealed transcripts, so it
	// recomputes only under an identity able to unseal: the caller's
	// hook opens it, and a subject the hook cannot open is reported
	// rather than passed over.
	if s.Verdict != nil && s.Verdict.Levels && s.Verdict.Independence == string(transition.L3) && rep.Records != nil && rep.Fold != nil {
		var sealed *verdict.SealedInput
		attempt := true
		if s.Sealed != nil {
			if rep.Unseal == nil {
				attempt = false
				rep.notAttempted(id, "the subject carries sealed checks and no identity able to unseal was supplied")
			} else if in, err := rep.Unseal(s); err != nil {
				attempt = false
				rep.notAttempted(id, err.Error())
			} else {
				sealed = in
			}
		}
		if attempt {
			digest, err := reproduce(rep.Records, rep.Fold, s, id, repo, sealed)
			if err != nil || digest != s.Verdict.Receipt {
				why := "the recomputed receipt digest differs from the cited " + s.Verdict.Receipt
				if err != nil {
					why = err.Error()
				}
				out = append(out, Finding{Subject: id, Class: ClassIndependenceUnverified,
					Detail: fmt.Sprintf("the verdict at position %d records L3 and its receipt does not reproduce from the verifier's own checkout: %s — deterministic-first verification is what the reproduction proves", s.Verdict.Pos, why)})
			}
		}
	}
	// The scorecard's evidence-grade half (plans/os-2e34f66a.md D3)
	// needs no merge: the cited artifact is graded wherever the
	// verdict stands.
	if s.Verdict != nil && s.Verdict.Scorecard != nil && store != nil {
		if f := ScorecardAt(id, s.Verdict, store); f != nil {
			out = append(out, *f)
		}
	}
	// The trace shapes the receipt binds (plans/os-7fc2ca38.md D5, D6)
	// are graded wherever the verdict stands, like the scorecard: a
	// shape the store lost is evidence a citation points at and cannot
	// reach.
	if s.Verdict != nil && store != nil {
		out = append(out, TracesAt(id, s.Verdict, store)...)
	}
	if s.Merged == nil || s.Merged.SHA == "" || s.Verdict == nil {
		// Record-derivable classes already cover chains this
		// incomplete; there is no evidence to grade.
		return out
	}
	merged := s.Merged.SHA
	// The attested comparison needs the receipt; the target check
	// below does not, so lost evidence never hides a rewritten target
	// when multiple failures coexist (review finding on #137).
	attestedHead := ""
	if body, err := store.Get(s.Verdict.Receipt); err != nil {
		out = append(out, Finding{Subject: id, Class: ClassEvidenceMissing,
			Detail: fmt.Sprintf("the receipt %s cited by the verdict at position %d is not retrievable intact: %v — the evidence a verdict points at must survive verbatim", s.Verdict.Receipt, s.Verdict.Pos, err)})
	} else {
		var rcpt struct {
			Head string `json:"head"`
		}
		if jerr := json.Unmarshal(body, &rcpt); jerr != nil || rcpt.Head == "" {
			out = append(out, Finding{Subject: id, Class: ClassEvidenceMissing,
				Detail: fmt.Sprintf("the stored receipt %s carries no attested head", s.Verdict.Receipt)})
		} else {
			attestedHead = rcpt.Head
		}
	}
	// Clean ancestry: fast-forward (observed == attested) or a true
	// merge commit (attested head an ancestor of the observed sha).
	// Anything else is a surfaced state, not a fabrication verdict:
	// rebase and squash flows land here by design in v0.
	if attestedHead != "" && merged != attestedHead && !gitAncestor(repo, attestedHead, merged) {
		out = append(out, Finding{Subject: id, Class: ClassAttestedDivergence,
			Detail: fmt.Sprintf("the observed merge %s is neither the attested head %s nor its descendant (receipt %s) — rebase and squash flows surface here until patch-equivalence reconciliation lands", merged, attestedHead, s.Verdict.Receipt)})
	}
	// Target rewrite: the merged commit must resolve and still sit
	// under the target tip (v0: the repository's checked-out default
	// branch). A force-push that dropped it is the charter's
	// detected-divergence case.
	if !gitResolves(repo, merged) || !gitAncestor(repo, merged, "HEAD") {
		out = append(out, Finding{Subject: id, Class: ClassTargetRewritten,
			Detail: fmt.Sprintf("the observed merge %s no longer resolves under the target tip — the target ref was rewritten after the observation at position %d", merged, s.Merged.Pos)})
	}
	return out
}

func gitResolves(repo, rev string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", rev+"^{commit}").Run() == nil
}

func gitAncestor(repo, ancestor, descendant string) bool {
	return exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", ancestor, descendant).Run() == nil
}

// Lessons is the evidence-grade check over the curation stores
// (plans/os-96850e5a.md D6): every promoted, uncontested lesson whose
// fact does not resolve in the repository is lesson_unverified, on the
// hypothesis subject, with the reason.
func Lessons(records []*event.Record, fold *transition.Fold, repo string, at time.Time) []Finding {
	var out []Finding
	_, unresolved := curation.Surfacing(records, fold, repo, "", at)
	for _, u := range unresolved {
		c, _ := curation.ParseCitation(u.Hypothesis)
		out = append(out, Finding{Subject: c.Contract, Class: ClassLessonUnverified,
			Detail: fmt.Sprintf("the promotion of %s does not resolve in the repository: %s — a fact a worker would be handed must verify before it surfaces", u.Lesson, u.Reason)})
	}
	return out
}

// ScorecardAt is the evidence-grade half of the scorecard rule
// (plans/os-2e34f66a.md D3): the cited scorecard retrieves intact and
// its items (id, score, uncertainty) equal the payload's, in order.
// The record half, the derivation over the payload's items, is every
// boundary's and classifies nothing here.
func ScorecardAt(id string, v *transition.VerdictFact, store *artifact.Store) *Finding {
	body, err := store.Get(v.Scorecard.Digest)
	if err != nil {
		return &Finding{Subject: id, Class: ClassScorecardUnverified,
			Detail: fmt.Sprintf("the scorecard %s cited by the verdict at position %d is not retrievable intact: %v — the evidence a verdict points at must survive verbatim", v.Scorecard.Digest, v.Pos, err)}
	}
	var stored struct {
		Items []transition.ScoreItem `json:"items"`
	}
	if err := json.Unmarshal(body, &stored); err != nil {
		return &Finding{Subject: id, Class: ClassScorecardUnverified,
			Detail: fmt.Sprintf("the scorecard %s cited by the verdict at position %d does not parse: %v", v.Scorecard.Digest, v.Pos, err)}
	}
	if why := ScoresDiffer(v.Scorecard.Items, stored.Items); why != "" {
		return &Finding{Subject: id, Class: ClassScorecardUnverified,
			Detail: fmt.Sprintf("the verdict at position %d carries items its stored scorecard %s does not: %s — the payload's items are what every boundary derived from, and the artifact is what the verifier scored", v.Pos, v.Scorecard.Digest, why)}
	}
	return nil
}

// ScoresDiffer names the first disagreement between the payload's
// items and the artifact's, or "" when they agree.
func ScoresDiffer(payload, stored []transition.ScoreItem) string {
	if len(payload) != len(stored) {
		return fmt.Sprintf("the payload scores %d items, the artifact %d", len(payload), len(stored))
	}
	for i := range payload {
		p, a := payload[i], stored[i]
		if p.ID != a.ID || p.Score != a.Score || p.Uncertainty != a.Uncertainty {
			return fmt.Sprintf("item %d is %s/%s/%s in the payload and %s/%s/%s in the artifact", i, p.ID, p.Score, p.Uncertainty, a.ID, a.Score, a.Uncertainty)
		}
	}
	return ""
}

// LessonsStale lists the lessons stale at the instant: the latest
// admitted promotion of each path, unretired, expired for at least
// staleAfter (plans/os-0d537fbd.md D5). The loop never retires or
// revalidates; the finding asks for one or the other.
func LessonsStale(st *curation.State, now time.Time, staleAfter time.Duration) []Finding {
	var out []Finding
	paths := make([]string, 0, len(st.Lessons))
	for path := range st.Lessons {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		l := st.Lessons[path]
		if st.RetiredPath(path) || !curation.Expired(l, now) {
			continue
		}
		if ex, err := time.Parse(time.RFC3339, l.Expires); err == nil && now.Sub(ex) < staleAfter {
			continue
		}
		out = append(out, Finding{Subject: fmt.Sprintf("%s@%d", path, l.Pos), Class: ClassLessonStale,
			Detail: fmt.Sprintf("the promotion at position %d of %s expired at %s and nobody revalidated or retired it — a revalidation (the stamps moved forward in a PR, then a new lesson.promoted for the path) or a lesson.retired with reason expired", l.Pos, l.Lesson, l.Expires)})
	}
	return out
}

// reproduce recomputes the receipt for the subject's bound submission
// exactly as `seed verdict check` does, through the verifier's own
// input seam, and returns its digest.
func reproduce(records []*event.Record, fold *transition.Fold, s transition.SubjectState, subject, repo string, sealed *verdict.SealedInput) (string, error) {
	in, err := verdict.InputFor(records, fold, s, subject, repo, 0)
	if err != nil {
		return "", err
	}
	in.Sealed = sealed
	r, err := verdict.Compute(in)
	if err != nil {
		return "", err
	}
	return r.Digest()
}

// TracesAt is the evidence-grade half of the trace rule
// (plans/os-7fc2ca38.md D5): every shape the cited receipt binds
// retrieves intact. A receipt that itself does not retrieve is graded
// where the merge chain grades it and yields nothing here; the raw
// sidecar is never graded, since erasing it is the erasure path the
// shape was designed to survive.
func TracesAt(id string, v *transition.VerdictFact, store *artifact.Store) []Finding {
	body, err := store.Get(v.Receipt)
	if err != nil {
		return nil
	}
	var r verdict.Receipt
	if err := json.Unmarshal(body, &r); err != nil {
		return nil
	}
	var out []Finding
	for _, list := range []struct {
		which   string
		entries []verdict.TraceEntry
	}{{"transcript", r.Traces}, {"sealed transcript", r.SealedTraces}} {
		for _, e := range list.entries {
			if e.Malformed {
				continue
			}
			if _, err := store.Get(e.ShapeSHA256); err != nil {
				out = append(out, Finding{Subject: id, Class: ClassEvidenceMissing,
					Detail: fmt.Sprintf("the trace shape %s bound by receipt %s for %s %d (verdict at position %d) is not retrievable intact: %v — the evidence a citation points at must survive verbatim", e.ShapeSHA256, v.Receipt, list.which, e.Transcript, v.Pos, err)})
			}
		}
	}
	return out
}
