// Package refusal maps the boundary's typed refusals onto envelope
// exits and codes in one place (plans/os-5c8a312c.md D2): the CLI
// rendered this mapping alone until the admission service needed to
// answer proposals with the very code the CLI would have printed, and
// a mapping that lives in one binary is a mapping the other binary
// re-derives, which is how two postures come to disagree on a code.
package refusal

import (
	"errors"
	"fmt"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/erasure"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/request"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// FailureEnvelope maps a verification failure to its exit code: version
// discipline troubles refuse at 10, everything else at 8, with the
// deterministic "position N: reason: detail" message. The envelope's
// position field carries the failing position too: ledger-aware refusals
// are stamped like every other ledger-aware response, not only narrated.
func FailureEnvelope(fail *ledger.Failure) *envelope.Envelope {
	msg := fmt.Sprintf("position %d: %s: %s", fail.Position, fail.Reason, fail.Detail)
	var env *envelope.Envelope
	switch fail.Reason {
	case ledger.ReasonVersionMismatch, ledger.ReasonVersionUnsupported:
		env = envelope.Fail(envelope.ExitVersionMismatch, fail.Reason, msg)
	default:
		env = envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", msg)
	}
	pos := fmt.Sprintf("%d", fail.Position)
	env.Position = &pos
	return env
}

// StampTip stamps the envelope with the position of the last record of
// a chain holding count records; an empty chain stays unstamped.
func StampTip(env *envelope.Envelope, count int) *envelope.Envelope {
	if count > 0 {
		pos := fmt.Sprintf("%d", count-1)
		env.Position = &pos
	}
	return env
}

// Envelope maps a refusal from the append path — admission's typed
// errors, the loop's own outcomes, the remote's answers — to the
// envelope the caller renders (plans/os-895bf828.md step 2): admission
// refusals keep their typed exits, the loop's outcomes land on
// contention (2), the remote's policy rejection on 11 with the reason
// verbatim, head regression on 12, and the rest on unavailable (5). A
// service refusal (*gitref.Refusal) is rendered as the service sent it:
// the boundary's own code, not a generic remote rejection.
func Envelope(err error) *envelope.Envelope {
	var sr *gitref.Refusal
	if errors.As(err, &sr) {
		env := envelope.Fail(sr.Exit, sr.Code, sr.Message)
		env.Position = sr.Position
		return env
	}
	var herr *halt.HaltedError
	if errors.As(err, &herr) {
		return envelope.Fail(envelope.ExitHalted, "halted", err.Error())
	}
	var cerr *admit.ClassificationError
	if errors.As(err, &cerr) {
		return envelope.Fail(envelope.ExitClassificationRef, "classification_refused", err.Error())
	}
	var oog *admit.OutOfGrantError
	if errors.As(err, &oog) {
		return envelope.Fail(envelope.ExitOutOfGrant, "out_of_grant", err.Error())
	}
	var vin *admit.VerbInactiveError
	if errors.As(err, &vin) {
		return envelope.Fail(envelope.ExitInvalidTransition, "invalid_transition", err.Error())
	}
	// The declaration-driven policy refusals (plans/os-0d4f2af3.md):
	// illegal steps at this position under this deployment's
	// guardrails and teams, named so the caller knows which file
	// refused.
	var settled *admit.RaceSettledError
	if errors.As(err, &settled) {
		return envelope.Fail(envelope.ExitInvalidTransition, "race_settled", err.Error())
	}
	// A request refusal (plans/os-48df10a2.md; spec/requests.md):
	// the shape, the subject or the citation is wrong.
	var req *request.Error
	if errors.As(err, &req) {
		return envelope.Fail(envelope.ExitInvalidTransition, "request_refused", err.Error())
	}
	// The per-verb approval (plans/os-5781a026.md D2, D4): the
	// shapes and citations of the three verbs, and the policy refusal
	// of a governed act with no open grant.
	var apr *approval.Error
	if errors.As(err, &apr) {
		return envelope.Fail(envelope.ExitInvalidTransition, "approval_refused", err.Error())
	}
	var need *admit.ApprovalRequiredError
	if errors.As(err, &need) {
		return envelope.Fail(envelope.ExitInvalidTransition, "approval_required", err.Error())
	}
	var eras *erasure.Error
	if errors.As(err, &eras) {
		return envelope.Fail(envelope.ExitInvalidTransition, "erasure_refused", err.Error())
	}
	// The relation facts and the claim boundary's effective-readiness
	// read (plans/os-f0ae2cdf.md; spec/topology.md).
	var topo *topology.Error
	if errors.As(err, &topo) {
		return envelope.Fail(envelope.ExitInvalidTransition, "topology_refused", err.Error())
	}
	var notReady *topology.NotReadyError
	if errors.As(err, &notReady) {
		return envelope.Fail(envelope.ExitInvalidTransition, "not_effectively_ready", err.Error())
	}
	var ceil *admit.CeilingError
	if errors.As(err, &ceil) {
		return envelope.Fail(envelope.ExitInvalidTransition, "tier_above_ceiling", err.Error())
	}
	var route *admit.RoutingError
	if errors.As(err, &route) {
		return envelope.Fail(envelope.ExitInvalidTransition, "routing_unknown", err.Error())
	}

	// A flywheel gate (spec/flywheel.md) is an illegal step at
	// this position, and the message names the gate.
	var fwe *flywheel.Error
	if errors.As(err, &fwe) {
		return envelope.Fail(envelope.ExitInvalidTransition, "invalid_transition", err.Error())
	}
	var itr *transition.InvalidTransitionError
	if errors.As(err, &itr) {
		return envelope.Fail(envelope.ExitInvalidTransition, "invalid_transition", err.Error())
	}
	var be *admit.BudgetError
	if errors.As(err, &be) && be.Exhausted {
		// Exhaustion only. Every other budget refusal falls through to
		// the catch-all below and keeps chain_invalid: a caller that
		// answers this code by asking for less must not also be
		// answering a malformed payload (plans/os-d03bde01.md D1).
		return envelope.Fail(envelope.ExitBudgetExhausted, "budget_exhausted", err.Error())
	}
	var ce *admit.ContentionError
	if errors.As(err, &ce) {
		return envelope.Fail(envelope.ExitContention, "contention", err.Error())
	}
	var fe *admit.FenceError
	if errors.As(err, &fe) {
		return envelope.Fail(envelope.ExitFenced, "fenced_out", err.Error())
	}
	var lse *admit.LevelShortError
	if errors.As(err, &lse) {
		// The family's exit with the refining code (spec/envelope.md).
		return envelope.Fail(envelope.ExitNotIndependent, "level_short", err.Error())
	}
	var nie *admit.NotIndependentError
	if errors.As(err, &nie) {
		return envelope.Fail(envelope.ExitNotIndependent, "not_independent", err.Error())
	}
	var ve *admit.VerdictError
	if errors.As(err, &ve) && ve.Code != "" {
		// The rubric derivation's refinements under checks_red
		// (plans/os-2e34f66a.md D3; spec/envelope.md).
		return envelope.Fail(envelope.ExitChecksRed, ve.Code, err.Error())
	}
	var pre *transition.PlanRequiredError
	if errors.As(err, &pre) {
		return envelope.Fail(envelope.ExitPlanRequired, "plan_required", err.Error())
	}
	var fail *ledger.Failure
	if errors.As(err, &fail) {
		return FailureEnvelope(fail)
	}
	switch {
	case errors.Is(err, ledger.ErrUnknownActor):
		return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
	case errors.Is(err, gitref.ErrRetriesSpent):
		return envelope.Fail(envelope.ExitContention, "contention", err.Error())
	case errors.Is(err, gitref.ErrRemoteRejected):
		return envelope.Fail(envelope.ExitRemoteRejected, "remote_rejected", err.Error())
	case errors.Is(err, gitref.ErrHeadRegression):
		return envelope.Fail(envelope.ExitHeadRegression, "head_regression", err.Error())
	case errors.Is(err, gitref.ErrUnavailable):
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	var ref *admit.Refusal
	if errors.As(err, &ref) {
		return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}
