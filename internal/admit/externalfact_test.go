package admit

// The invariance drill (plans/os-b45c308d.md D6, AC6;
// spec/external-facts.md "Observations are not control"): a
// green check.observed leaves lifecycle state, every lane's
// affordances, the obligations, the budget view, the submission and
// the verdict exactly as a chain without it; a red one narrows and
// never widens, adding exactly the unmergeable debt and refusing
// merge.requested; neither creates a verdict, and the fact has no
// transition-table row. A planted consumer that turned a passing
// check into a verdict or a merge would fail here by name.

import (
	"crypto/ed25519"
	"fmt"
	"slices"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

type invariantView struct {
	state, verdict, submission, claim string
	affordances                       map[string][]string
	obligations                       []string
	budget                            string
}

func (f *forgeFixture) view(t *testing.T, keys map[string]ed25519.PrivateKey) invariantView {
	t.Helper()
	s, ok := f.ctx.Lifecycle.State("c-1")
	if !ok {
		t.Fatal("c-1 is folded")
	}
	v := invariantView{state: s.State, verdict: fmt.Sprint(s.Verdict), submission: fmt.Sprint(s.Submission), claim: fmt.Sprint(s.Claim), affordances: map[string][]string{}}
	for name, key := range keys {
		v.affordances[name] = Affordances(f.ctx, key, "c-1")
	}
	for _, row := range obligation.Derive(f.ctx.Records, f.ctx.Table, obligation.Deps{}) {
		if row.Subject == "c-1" {
			v.obligations = append(v.obligations, row.Kind)
		}
	}
	v.budget = fmt.Sprint(BudgetViewAt(f.ctx.Records, f.ctx.Table, "c-1", s))
	return v
}

func TestGovernedObservationIsNotControl(t *testing.T) {
	f := newForgeFixture(t, version.Seed8)
	keys := map[string]ed25519.PrivateKey{"operator": f.signer, "worker": f.worker, "verifier": f.verifier, "dispatcher": f.dispatcher, "observer": f.viewer}
	if slices.Contains(f.ctx.Table.Verbs(), transition.CheckObservedVerb) {
		t.Fatal("check.observed has no transition-table row")
	}
	before := f.view(t, keys)
	// Green: nothing changes but the standing observation.
	f.step(f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "green", 0, "approved"))
	after := f.view(t, keys)
	s, _ := f.ctx.Lifecycle.State("c-1")
	if s.Observation == nil || s.Observation.Red() {
		t.Fatalf("the green observation stands: %+v", s.Observation)
	}
	if after.state != before.state || after.verdict != before.verdict || after.submission != before.submission || after.claim != before.claim || after.budget != before.budget {
		t.Fatalf("a green observation changes no lifecycle, verdict, submission, claim or budget state:\n%+v\n%+v", before, after)
	}
	if !slices.Equal(after.obligations, before.obligations) {
		t.Fatalf("a green observation discharges and raises nothing: %v vs %v", before.obligations, after.obligations)
	}
	for name := range keys {
		if !slices.Equal(after.affordances[name], before.affordances[name]) {
			t.Fatalf("a green observation widens no lane's affordances (%s): %v vs %v", name, before.affordances[name], after.affordances[name])
		}
	}
	if s.Verdict != nil {
		t.Fatal("a passing check is not a verdict")
	}
	if list := after.affordances["worker"]; slices.Contains(list, transition.MergeRequestedVerb) {
		t.Fatalf("a passing check legalizes no merge request: %v", list)
	}
	// Red: narrows, never widens. Exactly the unmergeable debt is
	// added, the worker's merge request is refused by the observation
	// once a pass verdict would otherwise allow it, and nothing else
	// moves.
	f.step(f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "red", 0, "none"))
	red := f.view(t, keys)
	if red.state != before.state || red.verdict != before.verdict || red.submission != before.submission || red.claim != before.claim || red.budget != before.budget {
		t.Fatalf("a red observation changes no lifecycle, verdict, submission, claim or budget state:\n%+v\n%+v", before, red)
	}
	added := []string{}
	for _, k := range red.obligations {
		if !slices.Contains(before.obligations, k) {
			added = append(added, k)
		}
	}
	if !slices.Equal(added, []string{obligation.KindSubmissionUnmergeable}) {
		t.Fatalf("a red observation raises exactly the unmergeable debt: %v (before %v, after %v)", added, before.obligations, red.obligations)
	}
	for _, k := range before.obligations {
		if !slices.Contains(red.obligations, k) && k != obligation.KindVerdictUnmerged {
			t.Fatalf("a red observation discharges nothing: %s vanished", k)
		}
	}
	// The one affordance a red observation adds is the return that
	// cites it, for the lanes that may return (dispatch, operator):
	// the debt's own discharge, and nothing that moves the work
	// forward.
	for name := range keys {
		for _, verb := range red.affordances[name] {
			if slices.Contains(before.affordances[name], verb) {
				continue
			}
			if verb != transition.ContractReturnedVerb || (name != "dispatcher" && name != "operator") {
				t.Fatalf("a red observation widens no lane's affordances beyond the citing return (%s gained %s)", name, verb)
			}
		}
	}
	// With a pass verdict on the submission, the merge request that
	// the verdict alone would allow is refused by the red observation,
	// and legal again once green supersedes: the one consequence, and
	// it only narrows.
	f.step(f.verifier, transition.VerdictRenderedVerb, "c-1", verdictBody("pass", f.submission))
	s, _ = f.ctx.Lifecycle.State("c-1")
	forgeRefusal(t, f.check(t, f.worker, transition.MergeRequestedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos)), "red pull request")
	f.step(f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "green", 0, "approved"))
	if err := f.check(t, f.worker, transition.MergeRequestedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos)); err != nil {
		t.Fatalf("green supersedes and the verdict, not the observation, carries the request: %v", err)
	}
}
