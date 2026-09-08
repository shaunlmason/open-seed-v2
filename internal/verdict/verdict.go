// Package verdict is the verdict pipeline's first half
// (plans/os-f6d2c267.md; SEED-NEXT.md Part II §8; conformance III.G
// rows 3-5; spec/verdicts.md is normative): the clean per-run
// verifier workspace, the profiled runner, and receipt computation.
// The verifier's inputs are enumerable and exclusively self-executed
// or self-read: the submission packet's anchors only name the range,
// and every hash, diff, inventory, and transcript is recomputed from
// the verifier's own checkout and the ledger. Admission-side checks
// (capability, L1 independence, submission binding) live in the admit
// rule set; this package is the run.
package verdict

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/plan"
	"github.com/shaunlmason/open-seed-v2/internal/traceshape"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// PlanRef is the approved plan blob hashed at the merge-base; nil in a
// receipt for a planless trivial-tier contract.
type PlanRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Environment is the receipt's environment fingerprint, the runner
// capability profile included: a verdict says what boundary it ran
// under (spec/verdicts.md).
type Environment struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Go     string `json:"go"`
	Runner string `json:"runner"`
}

// Receipt binds contract id, plan hash at merge-base, diff hash,
// changed-file inventory, transcripts, and environment fingerprint
// (III.G row 5). Its digest is the SHA-256 of the JCS bytes, and the
// verdict cites the digest.
type Receipt struct {
	Contract    string       `json:"contract"`
	MergeBase   string       `json:"merge_base"`
	Head        string       `json:"head"`
	Plan        *PlanRef     `json:"plan"`
	DiffSHA256  string       `json:"diff_sha256"`
	Files       []string     `json:"files"`
	Transcripts []Transcript `json:"transcripts"`
	Environment Environment  `json:"environment"`
	// The sealed half (plans/os-3128535a.md): the commitment the run
	// unsealed against and the sealed-check transcripts, the
	// charter's "visible and sealed check transcripts". Both omit
	// when the subject carries no seal, so every pre-6.3 receipt's
	// canonical bytes, and digest, are unchanged.
	Commitment        string       `json:"commitment,omitempty"`
	SealedTranscripts []Transcript `json:"sealed_transcripts,omitempty"`
	// The trace-shaped half (plans/os-7fc2ca38.md D4): one entry per
	// transcript whose command wrote an export to SEED_TRACE_EXPORT,
	// binding the digest of the export's normalized shape. Both omit
	// when no command wrote one, so every earlier receipt's canonical
	// bytes, and digest, are unchanged.
	Traces       []TraceEntry `json:"traces,omitempty"`
	SealedTraces []TraceEntry `json:"sealed_traces,omitempty"`
	// Evidence carries the shape documents and raw exports the run
	// produced, for the caller that stores them; it is not part of the
	// receipt's canonical form.
	Evidence []TraceEvidence `json:"-"`
}

// TraceEntry is one transcript's trace evidence in the receipt: the
// shape digest with its node and error counts, or the fact that the
// export was malformed and bound nothing.
type TraceEntry struct {
	Transcript  int
	ShapeSHA256 string
	Spans       int
	Errors      int
	Malformed   bool
}

type traceEntryJSON struct {
	Transcript  int    `json:"transcript"`
	ShapeSHA256 string `json:"shape_sha256,omitempty"`
	Spans       *int   `json:"spans,omitempty"`
	Errors      *int   `json:"errors,omitempty"`
	Malformed   bool   `json:"malformed,omitempty"`
}

// MarshalJSON renders {"transcript", "shape_sha256", "spans",
// "errors"} for a bound shape and {"transcript", "malformed": true}
// for a malformed export, and nothing else in either case.
func (e TraceEntry) MarshalJSON() ([]byte, error) {
	if e.Malformed {
		return json.Marshal(traceEntryJSON{Transcript: e.Transcript, Malformed: true})
	}
	spans, errors := e.Spans, e.Errors
	return json.Marshal(traceEntryJSON{Transcript: e.Transcript, ShapeSHA256: e.ShapeSHA256, Spans: &spans, Errors: &errors})
}

// UnmarshalJSON reads either form.
func (e *TraceEntry) UnmarshalJSON(b []byte) error {
	var j traceEntryJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*e = TraceEntry{Transcript: j.Transcript, ShapeSHA256: j.ShapeSHA256, Malformed: j.Malformed}
	if j.Spans != nil {
		e.Spans = *j.Spans
	}
	if j.Errors != nil {
		e.Errors = *j.Errors
	}
	return nil
}

// TraceEvidence is what one bound entry stores: the shape's canonical
// bytes (content-addressed under the entry's digest) and the raw
// export (content-addressed on its own, the shape's sidecar).
type TraceEvidence struct {
	Sealed     bool
	Transcript int
	Shape      []byte
	Raw        []byte
}

// TraceEntry returns the entry for transcript n of the visible or the
// sealed list, or nil.
func (r *Receipt) TraceEntry(sealed bool, n int) *TraceEntry {
	list := r.Traces
	if sealed {
		list = r.SealedTraces
	}
	for i := range list {
		if list[i].Transcript == n {
			return &list[i]
		}
	}
	return nil
}

// ShapeBytes returns the shape document the run produced for the
// entry, when this receipt is the one just computed; a receipt read
// back from the store carries none, and the caller retrieves the
// shape by digest instead.
func (r *Receipt) ShapeBytes(sealed bool, n int) []byte {
	for _, ev := range r.Evidence {
		if ev.Sealed == sealed && ev.Transcript == n {
			return ev.Shape
		}
	}
	return nil
}

// bindTrace normalizes one command's export into the receipt.
func (r *Receipt) bindTrace(n int, sealed bool, raw []byte, declared []string) {
	entry := TraceEntry{Transcript: n}
	shape, err := traceshape.Normalize(raw, n, sealed, declared)
	if err == nil {
		var canon []byte
		if canon, err = shape.Canonical(); err == nil {
			entry.ShapeSHA256 = artifact.Digest(canon)
			entry.Spans, entry.Errors = shape.Count()
			r.Evidence = append(r.Evidence, TraceEvidence{Sealed: sealed, Transcript: n, Shape: canon, Raw: raw})
		}
	}
	if err != nil {
		entry = TraceEntry{Transcript: n, Malformed: true}
	}
	if sealed {
		r.SealedTraces = append(r.SealedTraces, entry)
	} else {
		r.Traces = append(r.Traces, entry)
	}
}

// Canonical returns the receipt's RFC 8785 (JCS) bytes.
func (r *Receipt) Canonical() ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

// Digest returns the SHA-256 hex of the canonical bytes: the value a
// verdict.rendered payload cites.
func (r *Receipt) Digest() (string, error) {
	b, err := r.Canonical()
	if err != nil {
		return "", err
	}
	return artifact.Digest(b), nil
}

// RangeError refuses a submission range the verifier cannot attest:
// malformed, unresolvable, or a head that does not descend from its
// merge-base. It rides the shape-refusal mapping.
type RangeError struct {
	Contract string
	Base     string
	Reason   string
}

func (e *RangeError) Error() string {
	return fmt.Sprintf("submission range %q on %s: %s (spec/verdicts.md)", e.Base, e.Contract, e.Reason)
}

// UngatedError is the gate-before-run refusal (exit 18 ungated):
// declared-executable acceptance content without gate evidence never
// runs, and no verdict is rendered.
type UngatedError struct {
	Contract string
	Ref      string
}

func (e *UngatedError) Error() string {
	return fmt.Sprintf("acceptance spec %s on %s declares executable content without gate evidence — gate-before-run: nothing runs anywhere until a review gate vouches for the exact revision (spec/verdicts.md)", e.Ref, e.Contract)
}

// SpecUnrunnableError is the declared-armed-but-empty refusal (exit 19
// spec_unrunnable): the declaration promised runnable content and the
// body yields no parseable commands, so a vacuous pass must not exist.
type SpecUnrunnableError struct {
	Contract string
	Ref      string
	// Reason, when set, names a declaration the spec carries that the
	// parser refuses (a trace-attributes section, plans/os-7fc2ca38.md
	// D3); empty is the original no-commands refusal.
	Reason string
}

func (e *SpecUnrunnableError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("acceptance spec %s on %s: %s — a declaration the verifier cannot read cannot bound what reproduces, so the run refuses (spec/verdicts.md)", e.Ref, e.Contract, e.Reason)
	}
	return fmt.Sprintf("acceptance spec %s on %s declares executable content but its validation-commands section yields no parseable commands — silence must never decide, so the run refuses rather than passing vacuously (spec/verdicts.md)", e.Ref, e.Contract)
}

// Input names everything one receipt computation consumes. Base is the
// submission packet's mandatory range; PlanAnchor is the approved plan
// anchor ("path @ commit", empty for a planless trivial-tier
// contract); Acceptance is the fold's view of the contract's spec.
type Input struct {
	RepoDir    string
	Contract   string
	Base       string
	PlanAnchor string
	Acceptance *transition.AcceptanceInfo
	Runner     Runner
	// Sealed carries the decrypted sealed checks when the subject has
	// a commitment: the CLI unseals (it holds the identity and the
	// store) and the run executes. Nil for unsealed subjects.
	Sealed *SealedInput
}

// SealedInput is one unsealed check set: the commitment the plaintext
// verified against and the commands to run under the same profile as
// the visible checks.
type SealedInput struct {
	Commitment string
	Checks     []string
}

// anchorParts splits a combined anchor "path @ commit".
func anchorParts(anchor string) (path, commit string, ok bool) {
	path, commit, ok = strings.Cut(anchor, " @ ")
	if !ok || path == "" || commit == "" || strings.Contains(commit, "..") {
		return "", "", false
	}
	return path, commit, true
}

// Compute builds the receipt for one submission: resolve and check the
// range, check out the head in a clean per-run workspace, gate-check
// the acceptance spec, run its commands under the profile, and bind
// the result. Cleanup fires pass or fail.
func Compute(in Input) (*Receipt, error) {
	mbRef, headRef, ok := strings.Cut(in.Base, "..")
	if !ok || mbRef == "" || headRef == "" {
		return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: "not a <merge-base>..<head> range"}
	}
	ws, err := NewWorkspace(in.RepoDir, headRef)
	if err != nil {
		return nil, err
	}
	defer ws.Cleanup()
	return computeIn(ws, in, mbRef, headRef)
}

func computeIn(ws *Workspace, in Input, mbRef, headRef string) (*Receipt, error) {
	// Full immutable SHAs: a verdict attests exactly this triple, never
	// a ref name (review finding on the plan).
	mb, err := ws.git("rev-parse", "--verify", mbRef+"^{commit}")
	if err != nil {
		return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("merge-base does not resolve: %v", err)}
	}
	head, err := ws.git("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("head does not resolve: %v", err)}
	}
	mb, head = strings.TrimSpace(mb), strings.TrimSpace(head)
	if _, err := ws.git("merge-base", "--is-ancestor", mb, head); err != nil {
		return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("head %.12s does not descend from merge-base %.12s — the range must produce the diff the verdict attests", head, mb)}
	}
	r := &Receipt{
		Contract:    in.Contract,
		MergeBase:   mb,
		Head:        head,
		Files:       []string{},
		Transcripts: []Transcript{},
		Environment: Environment{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version(), Runner: ExecProfile},
	}
	if in.PlanAnchor != "" {
		path, _, ok := anchorParts(in.PlanAnchor)
		if !ok {
			return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("approved plan anchor %q is not \"path @ commit\"", in.PlanAnchor)}
		}
		// The plan hash binds at the merge-base: what the submission
		// was built against, not whatever revision the anchor names
		// (III.G row 5, D3).
		blob, err := ws.git("show", mb+":"+path)
		if err != nil {
			return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("approved plan %s does not exist at the merge-base: %v", path, err)}
		}
		r.Plan = &PlanRef{Path: path, SHA256: artifact.Digest([]byte(blob))}
	}
	diff, err := ws.git("diff", mb, head, "--")
	if err != nil {
		return nil, err
	}
	r.DiffSHA256 = artifact.Digest([]byte(diff))
	names, err := ws.git("diff", "--name-only", mb, head, "--")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(strings.TrimSpace(names), "\n") {
		if f != "" {
			r.Files = append(r.Files, f)
		}
	}
	var declared []string
	if in.Acceptance != nil && in.Acceptance.Executable {
		if !in.Acceptance.Gated {
			return nil, &UngatedError{Contract: in.Contract, Ref: in.Acceptance.Ref}
		}
		specPath, specCommit, ok := anchorParts(in.Acceptance.Ref)
		if !ok {
			return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("acceptance ref %q is not \"path @ commit\"", in.Acceptance.Ref)}
		}
		body, err := ws.git("show", specCommit+":"+specPath)
		if err != nil {
			return nil, &RangeError{Contract: in.Contract, Base: in.Base, Reason: fmt.Sprintf("acceptance spec %s does not resolve at its anchored commit: %v", in.Acceptance.Ref, err)}
		}
		cmds := plan.Commands([]byte(body))
		if len(cmds) == 0 {
			return nil, &SpecUnrunnableError{Contract: in.Contract, Ref: in.Acceptance.Ref}
		}
		// The declared attribute keys (plans/os-7fc2ca38.md D3), read
		// at the anchor exactly as the commands are: a declaration the
		// parser refuses is a spec that cannot bound what reproduces.
		if declared, err = plan.TraceAttributes([]byte(body)); err != nil {
			return nil, &SpecUnrunnableError{Contract: in.Contract, Ref: in.Acceptance.Ref, Reason: err.Error()}
		}
		for i, c := range cmds {
			tr, raw, wrote := in.Runner.RunTraced(ws, c, ws.tracePath(i, false))
			r.Transcripts = append(r.Transcripts, tr)
			if wrote {
				r.bindTrace(i, false, raw, declared)
			}
		}
	}
	if in.Sealed != nil {
		// The sealed checks run after the visible ones, under the same
		// profile, in the same workspace; their transcripts bind into
		// the receipt beside the commitment they were unsealed against.
		r.Commitment = in.Sealed.Commitment
		r.SealedTranscripts = []Transcript{}
		for i, c := range in.Sealed.Checks {
			tr, raw, wrote := in.Runner.RunTraced(ws, c, ws.tracePath(i, true))
			r.SealedTranscripts = append(r.SealedTranscripts, tr)
			if wrote {
				r.bindTrace(i, true, raw, declared)
			}
		}
	}
	return r, nil
}
