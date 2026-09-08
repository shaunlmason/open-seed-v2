package verdict

// The scorecard (plans/os-2e34f66a.md D2): the verifier's item-by-item
// scoring of a rubric, an artifact the verdict cites by digest, with
// the evidence in the artifact and the derivation-bearing half (ids,
// scores, uncertainties) in the signed payload.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/plan"
	"github.com/shaunlmason/open-seed-v2/internal/traceshape"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// NoteBudget bounds a scorecard note: the classification lint's
// per-string budget, since a note is prose beside a coordination fact.
const NoteBudget = 512

// ScoreItem is one scored rubric item: the id, the score, the evidence
// (anchored paths or transcript indices, never prose), the verifier's
// explicit uncertainty, and an optional bounded note.
type ScoreItem struct {
	ID          string   `json:"id"`
	Score       string   `json:"score"`
	Evidence    []string `json:"evidence"`
	Uncertainty string   `json:"uncertainty"`
	Note        string   `json:"note,omitempty"`
}

// Scorecard is the artifact: the contract, the submission position it
// scores, and every rubric item exactly once.
type Scorecard struct {
	Contract   string      `json:"contract"`
	Submission string      `json:"submission"`
	Items      []ScoreItem `json:"items"`
}

// Canonical returns the scorecard's RFC 8785 (JCS) bytes.
func (s *Scorecard) Canonical() ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

// Digest is the sha256 of the canonical bytes, what the verdict cites.
func (s *Scorecard) Digest() (string, error) {
	b, err := s.Canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Ref is the payload half: the digest and, per item, exactly the two
// enums the derivation reads.
func (s *Scorecard) Ref() (transition.ScorecardRef, error) {
	digest, err := s.Digest()
	if err != nil {
		return transition.ScorecardRef{}, err
	}
	ref := transition.ScorecardRef{Digest: digest}
	for _, it := range s.Items {
		ref.Items = append(ref.Items, transition.ScoreItem{ID: it.ID, Score: it.Score, Uncertainty: it.Uncertainty})
	}
	return ref, nil
}

// ScorecardError names the part of a scorecard that refuses.
type ScorecardError struct {
	Part   string
	Reason string
}

func (e *ScorecardError) Error() string { return fmt.Sprintf("scorecard %s: %s", e.Part, e.Reason) }

// evidenceRE is an anchored path with an optional line range:
// "<path> @ <commit>#L<a>-L<b>".
var evidenceRE = regexp.MustCompile(`^(.+?) @ ([0-9a-f]{7,40})(?:#L(\d+)-L(\d+))?$`)

// ParseScorecard decodes a scorecard strictly.
func ParseScorecard(raw []byte) (*Scorecard, error) {
	var s Scorecard
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, &ScorecardError{Part: "shape", Reason: "the scorecard is the strict object {contract, submission, items: [{id, score, evidence, uncertainty, note?}]}: " + err.Error()}
	}
	return &s, nil
}

// Validate judges a scorecard against the rubric it scores and the
// receipt whose transcripts it may cite (D2): every rubric item scored
// exactly once, no unknown id, scores and uncertainties from their two
// vocabularies, at least one evidence reference per item, each an
// anchored path (resolving in the repository at its commit when one is
// given) or a transcript index the receipt carries, and every note
// within the budget. The contract and submission must be the ones
// under judgment.
func Validate(s *Scorecard, contract string, submission int, rubric []plan.Item, r *Receipt, repo string, store *artifact.Store) error {
	if s.Contract != contract {
		return &ScorecardError{Part: "contract", Reason: fmt.Sprintf("%q is not the contract under judgment (%s)", s.Contract, contract)}
	}
	if s.Submission != strconv.Itoa(submission) {
		return &ScorecardError{Part: "submission", Reason: fmt.Sprintf("%q is not the bound submission (%d)", s.Submission, submission)}
	}
	known := map[string]bool{}
	for _, it := range rubric {
		known[it.ID] = true
	}
	scored := map[string]bool{}
	transcripts := 0
	if r != nil {
		transcripts = len(r.Transcripts) + len(r.SealedTranscripts)
	}
	for _, it := range s.Items {
		if !known[it.ID] {
			return &ScorecardError{Part: "items", Reason: fmt.Sprintf("item %q is not in the rubric", it.ID)}
		}
		if scored[it.ID] {
			return &ScorecardError{Part: "items", Reason: fmt.Sprintf("item %q is scored twice: every item is scored exactly once", it.ID)}
		}
		scored[it.ID] = true
		if it.Score != "pass" && it.Score != "fail" {
			return &ScorecardError{Part: "items", Reason: fmt.Sprintf("item %q scores %q: the vocabulary is pass, fail", it.ID, it.Score)}
		}
		if it.Uncertainty != "low" && it.Uncertainty != "high" {
			return &ScorecardError{Part: "items", Reason: fmt.Sprintf("item %q carries uncertainty %q: the vocabulary is low, high, and a routing decision needs two values", it.ID, it.Uncertainty)}
		}
		if len(it.Evidence) == 0 {
			return &ScorecardError{Part: "evidence", Reason: fmt.Sprintf("item %q cites no evidence: every item cites an anchored path or a transcript, never prose", it.ID)}
		}
		for _, ev := range it.Evidence {
			if err := resolveEvidence(ev, transcripts, repo, r, store); err != nil {
				return &ScorecardError{Part: "evidence", Reason: fmt.Sprintf("item %q: %v", it.ID, err)}
			}
		}
		if len(it.Note) > NoteBudget {
			return &ScorecardError{Part: "note", Reason: fmt.Sprintf("item %q carries a note of %d bytes over the %d-byte budget: a note is prose beside a fact, not the evidence", it.ID, len(it.Note), NoteBudget)}
		}
	}
	for _, it := range rubric {
		if !scored[it.ID] {
			return &ScorecardError{Part: "items", Reason: fmt.Sprintf("item %q is not scored: every rubric item is scored exactly once", it.ID)}
		}
	}
	return nil
}

// traceRE is a span citation (plans/os-7fc2ca38.md D6):
// "trace:<n>/<path>" or "sealed-trace:<n>/<path>", the path
// dot-separated child indexes into transcript n's shape.
var traceRE = regexp.MustCompile(`^(sealed-)?trace:(\d+)/(\d+(?:\.\d+)*)$`)

// resolveEvidence accepts "transcript:<n>" for n the receipt carries,
// a span citation resolving in the shape the receipt binds for that
// transcript (the shape the run just produced, or the stored one when
// a store is given), or an anchored path, resolving the path at the
// commit in the repository when one is given.
func resolveEvidence(ev string, transcripts int, repo string, r *Receipt, store *artifact.Store) error {
	if m := traceRE.FindStringSubmatch(ev); m != nil {
		return resolveTrace(ev, m[1] != "", m[2], m[3], r, store)
	}
	if strings.HasPrefix(ev, "transcript:") {
		n, err := strconv.Atoi(strings.TrimPrefix(ev, "transcript:"))
		if err != nil || n < 0 || n >= transcripts {
			return fmt.Errorf("evidence %q names no transcript the receipt carries (%d)", ev, transcripts)
		}
		return nil
	}
	m := evidenceRE.FindStringSubmatch(ev)
	if m == nil {
		return fmt.Errorf("evidence %q is neither \"transcript:<n>\" nor \"<path> @ <commit>[#L<a>-L<b>]\"", ev)
	}
	if m[3] != "" {
		a, _ := strconv.Atoi(m[3])
		b, _ := strconv.Atoi(m[4])
		if a < 1 || b < a {
			return fmt.Errorf("evidence %q carries an empty line range", ev)
		}
	}
	if repo != "" {
		if err := exec.Command("git", "-C", repo, "cat-file", "-e", m[2]+":"+m[1]).Run(); err != nil {
			return fmt.Errorf("evidence %q does not resolve in the repository at its commit", ev)
		}
	}
	return nil
}

// resolveTrace resolves one span citation: the receipt carries an
// entry for the transcript, the entry bound a shape (a malformed
// export bound nothing), the shape is at hand or retrieves intact, and
// the path names a node in it.
func resolveTrace(ev string, sealed bool, index, path string, r *Receipt, store *artifact.Store) error {
	n, err := strconv.Atoi(index)
	if err != nil || r == nil {
		return fmt.Errorf("evidence %q names no trace entry the receipt carries", ev)
	}
	entry := r.TraceEntry(sealed, n)
	if entry == nil {
		return fmt.Errorf("evidence %q names no trace entry the receipt carries: the command wrote no export", ev)
	}
	if entry.Malformed {
		return fmt.Errorf("evidence %q cites a malformed export: the receipt bound no shape for it", ev)
	}
	body := r.ShapeBytes(sealed, n)
	if body == nil {
		if store == nil {
			return fmt.Errorf("evidence %q: the shape %s is not at hand and no store was given", ev, entry.ShapeSHA256)
		}
		if body, err = store.Get(entry.ShapeSHA256); err != nil {
			return fmt.Errorf("evidence %q: the shape %s is not retrievable intact: %v", ev, entry.ShapeSHA256, err)
		}
	}
	shape, err := traceshape.Parse(body)
	if err != nil {
		return fmt.Errorf("evidence %q: %v", ev, err)
	}
	if _, err := shape.Resolve(path); err != nil {
		return fmt.Errorf("evidence %q names no span in the shape: %v", ev, err)
	}
	return nil
}
