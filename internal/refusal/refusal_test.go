package refusal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// One mapping for both binaries: every typed refusal lands on its exit
// and code, a service refusal is rendered as the service sent it, and
// the loop's own outcomes and the remote's answers keep their codes.
func TestEnvelopeMapsRefusals(t *testing.T) {
	pos := "9"
	for name, tc := range []struct {
		err  error
		exit int
		code string
	}{
		{&gitref.Refusal{Exit: 14, Code: "out_of_grant", Message: "no", Position: &pos}, 14, "out_of_grant"},
		{fmt.Errorf("wrapped: %w", &gitref.Refusal{Exit: 6, Code: "fenced_out", Message: "stale"}), 6, "fenced_out"},
		{&halt.HaltedError{}, envelope.ExitHalted, "halted"},
		{&admit.ClassificationError{}, envelope.ExitClassificationRef, "classification_refused"},
		{&admit.OutOfGrantError{}, envelope.ExitOutOfGrant, "out_of_grant"},
		{&admit.VerbInactiveError{}, envelope.ExitInvalidTransition, "invalid_transition"},
		{&transition.InvalidTransitionError{}, envelope.ExitInvalidTransition, "invalid_transition"},
		{&admit.BudgetError{Exhausted: true}, envelope.ExitBudgetExhausted, "budget_exhausted"},
		{&admit.ContentionError{}, envelope.ExitContention, "contention"},
		{&admit.FenceError{}, envelope.ExitFenced, "fenced_out"},
		{&admit.LevelShortError{}, envelope.ExitNotIndependent, "level_short"},
		{&admit.NotIndependentError{}, envelope.ExitNotIndependent, "not_independent"},
		{&admit.VerdictError{Code: "rubric_red"}, envelope.ExitChecksRed, "rubric_red"},
		{&approval.Error{Verb: approval.GrantedVerb}, envelope.ExitInvalidTransition, "approval_refused"},
		{&admit.ApprovalRequiredError{Verb: "claim.taken"}, envelope.ExitInvalidTransition, "approval_required"},
		{fmt.Errorf("wrapped: %w", &admit.Refusal{Rule: "require-approval", Err: &admit.ApprovalRequiredError{Verb: "claim.taken"}}), envelope.ExitInvalidTransition, "approval_required"},
		{&transition.PlanRequiredError{}, envelope.ExitPlanRequired, "plan_required"},
		{&ledger.Failure{Position: 2, Reason: "bad_prev", Detail: "x"}, envelope.ExitChainInvalid, "chain_invalid"},
		{&ledger.Failure{Position: 2, Reason: ledger.ReasonVersionUnsupported, Detail: "x"}, envelope.ExitVersionMismatch, ledger.ReasonVersionUnsupported},
		{ledger.ErrUnknownActor, envelope.ExitChainInvalid, "chain_invalid"},
		{gitref.ErrRetriesSpent, envelope.ExitContention, "contention"},
		{gitref.ErrRemoteRejected, envelope.ExitRemoteRejected, "remote_rejected"},
		{gitref.ErrHeadRegression, envelope.ExitHeadRegression, "head_regression"},
		{gitref.ErrUnavailable, envelope.ExitUnavailable, "unavailable"},
		{&admit.Refusal{}, envelope.ExitChainInvalid, "chain_invalid"},
		{errors.New("something else"), envelope.ExitUnavailable, "unavailable"},
	} {
		env := Envelope(tc.err)
		if env.Exit != tc.exit || env.Error == nil || env.Error.Code != tc.code {
			t.Errorf("case %d (%T): want %d %s, got %d %+v", name, tc.err, tc.exit, tc.code, env.Exit, env.Error)
		}
	}
	env := Envelope(&gitref.Refusal{Exit: 14, Code: "out_of_grant", Message: "no", Position: &pos})
	if env.Position == nil || *env.Position != "9" || env.Error.Message != "no" {
		t.Fatalf("a service refusal keeps its position and message: %+v", env)
	}
	if env := FailureEnvelope(&ledger.Failure{Position: 4, Reason: "bad_prev", Detail: "d"}); env.Position == nil || *env.Position != "4" || env.Error.Message != "position 4: bad_prev: d" {
		t.Fatalf("verification failures are stamped and narrated: %+v", env)
	}
	if env := StampTip(envelope.OK(nil), 0); env.Position != nil {
		t.Fatal("an empty chain is unstamped")
	}
	if env := StampTip(envelope.OK(nil), 3); env.Position == nil || *env.Position != "2" {
		t.Fatal("the stamp is the last position")
	}
}
