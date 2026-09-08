// Package envelope renders the affordance envelope: the versioned,
// schema-stable JSON shape every verb response uses (charter Part II
// section 10 and Appendix B; spec/envelope.md is the normative field
// and exit-code table). Exit codes reuse v1 semantics where they match
// (build-plan fixed default); new codes are allocated in the spec table,
// never ad hoc in code.
package envelope

import (
	"encoding/json"
	"io"
)

// V is the envelope schema version carried in every response.
const V = "seed-envelope/0"

// Exit codes: the v1-inherited allocations. The table in
// spec/envelope.md is authoritative; these constants mirror it.
const (
	ExitOK                = 0
	ExitContention        = 2
	ExitInvalidTransition = 3
	ExitNotFound          = 4
	ExitUnavailable       = 5
	ExitFenced            = 6
	ExitHalted            = 7
	ExitChainInvalid      = 8
	ExitClassificationRef = 9
	ExitVersionMismatch   = 10
	ExitRemoteRejected    = 11
	ExitHeadRegression    = 12
	ExitPostureInvalid    = 13
	ExitOutOfGrant        = 14
	ExitStale             = 15
	ExitPlanRequired      = 16
	ExitNotIndependent    = 17
	ExitUngated           = 18
	ExitSpecUnrunnable    = 19
	ExitChecksRed         = 20
	ExitReceiptMismatch   = 21
	// The sealed-checks refusals (plans/os-3128535a.md;
	// spec/sealed-checks.md): a broken seal (missing ciphertext,
	// commitment mismatch, or an empty-checks envelope), an identity
	// outside the recipient set (rotation lag), and an above-trivial
	// subject with no commitment at the verifier boundary.
	ExitSealBroken   = 22
	ExitNotRecipient = 23
	ExitUnsealed     = 24
	// The red-verdict lockout (plans/os-d2497eb7.md): rendering pass
	// over a submission an authenticated fail already judged refuses
	// until a new submission.
	ExitRedLocked = 25
	// The lane-validation refusal (plans/os-cf1c9688.md;
	// spec/lanes.md): a checked-in lane manifest makes a claim
	// the tables refuse — a grant outside the vocabulary, an act whose
	// accepted capabilities the lane does not hold, a liveness source
	// that is not a work step, a missing fragment. Distinct from
	// posture_invalid, which judges the deployment's posture
	// declaration rather than a role definition.
	ExitLaneInvalid = 26
	// ExitBudgetExhausted is capacity exhaustion at budget.reserve
	// (plans/os-d03bde01.md): a first-class, EXPECTED, recoverable
	// condition in the reservation model, and the one budget refusal a
	// caller can act on by asking for less. It is deliberately narrow:
	// the rule's other thirteen refusals - malformed payloads, wrong
	// signers, unknown classes, double closes, the laundering refusal -
	// keep chain_invalid, because a caller that retried with a smaller
	// amount against a malformed payload would retry forever.
	ExitBudgetExhausted = 27
	// ExitDrift: a declared desired state and an observed state differ
	// (plans/os-5c8a312c.md D6): the forge's protections against the
	// declaration first, and every later declared-versus-observed
	// comparison as a refinement, the message naming each difference.
	ExitDrift = 28
	// CodeDocsDrift refines ExitDrift for the governed-docs generator
	// (plans/os-16e55c11.md D1): a committed generated document differs
	// from what `seed docs generate` now renders from the table.
	CodeDocsDrift = "docs_drift"
	// CodeBrokenCitation refines ExitDrift for the citation stage of
	// `docs check` (card os-5fe43832): a document names a relative path
	// the tree does not hold, or one that leaves the tree. It shares
	// ExitDrift because it is the same declared-versus-observed
	// comparison the base code generalizes, the document being the
	// declaration and the tree the observation, and it is a distinct
	// code because the fix is a different one: regenerating the docs
	// cannot repair a citation.
	CodeBrokenCitation = "broken_citation"
	// ExitImportRefused is the predecessor import refusing before any
	// write (plans/os-cf13fb51.md D1, D4): unanchored, export_mismatch,
	// import_unmapped.
	ExitImportRefused = 29
	ExitUsage         = 64
	ExitUnreadable    = 66
)

// Error is the machine-branchable half of a refusal: a stable code to
// branch on and a human message to read.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Budget mirrors the reservation block. It stays nil until budget
// reservations land (build plan Phase 7).
type Budget struct {
	Reserved  string `json:"reserved"`
	Remaining string `json:"remaining"`
}

// Envelope is the one response shape (charter Appendix B). Result and Error
// are mutually exclusive. Position is the ledger position the response was
// computed at: null until the ledger lands (Phase 1), then always stamped.
type Envelope struct {
	V           string         `json:"v"`
	OK          bool           `json:"ok"`
	Result      map[string]any `json:"result"`
	Error       *Error         `json:"error"`
	Position    *string        `json:"position"`
	Affordances []string       `json:"affordances"`
	Budget      *Budget        `json:"budget"`
	Exit        int            `json:"exit"`
}

// OK builds a success envelope for result.
func OK(result map[string]any) *Envelope {
	return &Envelope{V: V, OK: true, Result: result, Affordances: []string{}, Exit: ExitOK}
}

// Fail builds a refusal envelope with the given exit code and error code.
func Fail(exit int, code, message string) *Envelope {
	return &Envelope{V: V, OK: false, Error: &Error{Code: code, Message: message}, Affordances: []string{}, Exit: exit}
}

// Render writes the envelope as a single JSON line.
func (e *Envelope) Render(w io.Writer) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
