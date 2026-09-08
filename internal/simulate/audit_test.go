package simulate

// The audit reconstructs the five bars from the ledger alone
// (plans/os-16e55c11.md D5, AC5/AC6): a clean chain passes every bar,
// and each bar's violation, planted once, is named. These craft the
// records directly — the audit reads the chain, not signatures — so a
// bar can be exercised without a full deployment.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func rec(verb, subject string) *event.Record {
	return &event.Record{Event: event.Event{Verb: verb, Subject: subject, Payload: []byte("{}")}}
}

// happy is a subject driven cleanly to a submission (a deliberate exit).
func happy(subject string) []*event.Record {
	return []*event.Record{
		rec("intent.filed", subject),
		rec("contract.specified", subject),
		rec("offer.published", subject),
		rec("claim.taken", subject),
		rec(transition.BudgetReserveVerb, subject),
		rec("run.started", subject),
		rec("submission.made", subject),
	}
}

// A synthetic happy path passes every bar that reads the chain's
// shape. It does not pass the unreserved-spend bar, and must not: its
// run.started carries no fence and cites no reservation, so nothing
// fenced it (plans/os-88df7ab2.md D7). The covered arm moved to
// TestAdmittedChainAuditsClean, which audits a chain admission took.
func TestAuditCleanRun(t *testing.T) {
	a := Audit(happy("c-1"))
	if len(a.SilentAbandonments) != 0 || len(a.GuardrailBreaches) != 0 || len(a.ChainViolations) != 0 || len(a.LostUpdates) != 0 {
		t.Fatalf("a clean chain must pass every bar that reads its shape: %+v", a)
	}
	if len(a.UnreservedSpend) != 1 || a.UnreservedSpend[0] != "c-1" {
		t.Fatalf("a start that cites no reservation is unfenced spend: %+v", a)
	}
}

func TestAuditCatchesSilentAbandonment(t *testing.T) {
	// A claim taken and never closed by a deliberate exit.
	recs := []*event.Record{
		rec("intent.filed", "c-1"),
		rec("contract.specified", "c-1"),
		rec("offer.published", "c-1"),
		rec("claim.taken", "c-1"),
		rec(transition.BudgetReserveVerb, "c-1"),
	}
	a := Audit(recs)
	if len(a.SilentAbandonments) != 1 || a.SilentAbandonments[0] != "c-1" {
		t.Fatalf("an unclosed window must be a silent abandonment, got %+v", a.SilentAbandonments)
	}
	if a.Clean {
		t.Fatal("a run with a silent abandonment is not clean")
	}
}

func TestAuditCatchesUnreservedSpend(t *testing.T) {
	// run.started with no covering reservation.
	recs := []*event.Record{
		rec("intent.filed", "c-1"),
		rec("contract.specified", "c-1"),
		rec("offer.published", "c-1"),
		rec("claim.taken", "c-1"),
		rec("run.started", "c-1"),
		rec("submission.made", "c-1"),
	}
	a := Audit(recs)
	if len(a.UnreservedSpend) != 1 || a.UnreservedSpend[0] != "c-1" {
		t.Fatalf("a run.started with no reservation must be unreserved spend, got %+v", a.UnreservedSpend)
	}
}

func TestAuditCatchesChainViolation(t *testing.T) {
	// claim.taken with no prior contract: an illegal transition from the
	// empty state.
	recs := []*event.Record{rec("claim.taken", "c-9")}
	a := Audit(recs)
	if len(a.ChainViolations) == 0 {
		t.Fatal("an illegal transition must be a chain violation")
	}
}

func TestAuditCatchesEmptyChain(t *testing.T) {
	a := Audit(nil)
	if len(a.LostUpdates) == 0 {
		t.Fatal("an empty chain is a lost-everything")
	}
}

// conformance: plans/os-aaec6a3c.md D1, D4 — an unoffered claim is not
// a guardrail breach. The scheduling model publishes offers
// (SEED-NEXT.md II.9) but admission does not require one: its claim
// arms are authoring isolation and the lifecycle transition, and
// nothing there reads the subject's offers, so a chain the boundary
// would take must not be reported as a breach. internal/history's
// generated chains claim without offering and pass the seed-admit
// hook, which is how this was found (#311).
func TestUnofferedClaimIsNotAGuardrailBreach(t *testing.T) {
	recs := []*event.Record{
		rec("intent.filed", "c-1"),
		rec("contract.specified", "c-1"),
		rec("claim.taken", "c-1"),
		rec(transition.BudgetReserveVerb, "c-1"),
		rec("submission.made", "c-1"),
	}
	if a := Audit(recs); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("admission takes an unoffered claim, so the bar must not name it: %+v", a.GuardrailBreaches)
	}
}

func TestCatalogIsDeterministicInSeed(t *testing.T) {
	a := catalog(0)
	b := catalog(0)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatal("catalog must be non-empty and stable")
	}
	for i := range a {
		if a[i].name != b[i].name {
			t.Fatal("catalog(seed) must be deterministic")
		}
	}
	// A different seed rotates the draw.
	if catalog(1)[0].name == catalog(0)[0].name && len(shipped) > 1 {
		t.Error("a different seed should rotate the first intent")
	}
}

func TestAuditMidChainViolationAndMultiSubject(t *testing.T) {
	// A subject that reaches review then illegally takes a claim again:
	// foldAll catches the mid-chain illegal transition. A second, clean
	// subject in the same chain is unaffected.
	recs := append(happy("c-1"),
		rec("intent.filed", "c-2"),
		rec("contract.specified", "c-2"),
		rec("claim.taken", "c-2"), // illegal: c-2 has no offer path here, and this is from ready
	)
	// Make c-2's claim illegal by placing it with no prior 'ready' via a
	// duplicate birth.
	recs = append(recs, rec("intent.filed", "c-1")) // birth on an existing subject: illegal
	a := Audit(recs)
	if len(a.ChainViolations) == 0 {
		t.Fatal("a second birth on an existing subject is a chain violation")
	}
}

// conformance: III.R row 5 — the bars read the protocol's vocabulary
// (plans/os-b86dab4c.md D3). Every verb the audit switches on is a verb
// the transition table defines: a bar that counts a verb the protocol
// never emits never fires, which is exactly how the unreserved-spend
// bar came to count "budget.reserved" against a protocol that emits
// budget.reserve, silently passing a fixture that repeated the typo.
func TestAuditedVerbsAreTheProtocols(t *testing.T) {
	// admit.CatalogVerbs is the authority: it is every verb the
	// boundary drafts, which is what "a verb the protocol emits"
	// means. The transition table's Verbs() is the lifecycle subset
	// and does not carry budget.reserve or run.started, so holding
	// the audit to it would fail on verbs the protocol does define.
	defined := map[string]bool{}
	for _, v := range admit.CatalogVerbs() {
		defined[v] = true
	}
	if len(defined) == 0 {
		t.Fatal("the catalog drafts no verbs: this drill would pass vacuously")
	}
	for _, v := range AuditedVerbs {
		if !defined[v] {
			t.Errorf("the audit counts %q, which the boundary never drafts", v)
		}
	}
	// The guard has teeth: a verb the table does not define fails it.
	if defined["budget.reserved"] {
		t.Fatal("budget.reserved must not be a protocol verb; this drill's mutation would prove nothing")
	}
	// And every exit the audit relies on is the protocol's own.
	for _, v := range []string{"submission.made", "claim.released", "claim.parked", "claim.reaped"} {
		if !transition.IsExit(v) || !defined[v] {
			t.Errorf("%q must be a deliberate exit the boundary drafts", v)
		}
	}
}

// conformance: plans/os-b86dab4c.md D4, AC1 and AC2 — the regression
// the old drills could not fail, both arms. A run covered by the
// protocol's reservation audits clean; the same chain with the verb
// the protocol does not define reads as unreserved spend, naming the
// subject. Reverting D1 fails the first arm.
// The verb's name is necessary but not sufficient: a chain naming the
// protocol's budget.reserve with no readable citation is still
// unfenced, and the covered arm is TestAdmittedChainAuditsClean
// (plans/os-88df7ab2.md D1, D7).
func TestUnreservedSpendCountsTheProtocolsReservation(t *testing.T) {
	named := []*event.Record{
		rec("intent.filed", "c-1"),
		rec("contract.specified", "c-1"),
		rec("offer.published", "c-1"),
		rec("claim.taken", "c-1"),
		rec(transition.BudgetReserveVerb, "c-1"),
		rec("run.started", "c-1"),
		rec("submission.made", "c-1"),
	}
	if a := Audit(named); len(a.UnreservedSpend) != 1 || a.UnreservedSpend[0] != "c-1" {
		t.Fatalf("naming %s without a citation the fold can read does not fence a run: %+v", transition.BudgetReserveVerb, a)
	}
	stale := []*event.Record{
		rec("intent.filed", "c-2"),
		rec("contract.specified", "c-2"),
		rec("offer.published", "c-2"),
		rec("claim.taken", "c-2"),
		rec("budget.reserved", "c-2"),
		rec("run.started", "c-2"),
		rec("submission.made", "c-2"),
	}
	a := Audit(stale)
	if len(a.UnreservedSpend) != 1 || a.UnreservedSpend[0] != "c-2" {
		t.Fatalf("a chain whose only reservation is a verb the protocol does not define is unreserved spend: %+v", a)
	}
	if a.Clean {
		t.Fatal("a run with unreserved spend is not clean")
	}
}

// recBy is rec with an actor and a payload: the guardrail arm reads
// both, since it compares a claim's signer against the key that sealed
// the subject.
func recBy(verb, subject, actor, payload string) *event.Record {
	// The version matters here, unlike in rec: this drill's records are
	// read through the fold, which skips a record whose version it
	// cannot place.
	return &event.Record{Event: event.Event{V: version.Seed1, Verb: verb, Subject: subject, Actor: actor, Payload: []byte(payload)}}
}

// conformance: plans/os-aaec6a3c.md D1, D4 — the guardrail the
// boundary does enforce on the claim path, and the reason this bar is
// corrected rather than emptied. Admission refuses a claim.taken whose
// actor sealed that subject's checks, so a chain holding one was
// pushed past the boundary and the bar names it.
func TestSealedAuthorClaimIsAGuardrailBreach(t *testing.T) {
	const sealer = "fp-sealer"
	sealed := []*event.Record{
		recBy("intent.filed", "c-1", "fp-dispatcher", `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`),
		recBy("contract.specified", "c-1", "fp-dispatcher", `{"acceptance": {"ref": "specs/x.md @ abc1234", "executable": false}}`),
		recBy(transition.CheckSealedVerb, "c-1", sealer, `{"commitment": "sha256:abcd"}`),
		recBy("claim.taken", "c-1", sealer, "{}"),
	}
	a := Audit(sealed)
	if len(a.GuardrailBreaches) != 1 || a.GuardrailBreaches[0] != "c-1" {
		t.Fatalf("the key that sealed the checks may not claim the subject: %+v", a)
	}
	if a.Clean {
		t.Fatal("a chain with a guardrail breach is not clean")
	}

	// A different claimant is the ordinary case and stays quiet.
	other := []*event.Record{
		recBy("intent.filed", "c-2", "fp-dispatcher", `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`),
		recBy("contract.specified", "c-2", "fp-dispatcher", `{"acceptance": {"ref": "specs/x.md @ abc1234", "executable": false}}`),
		recBy(transition.CheckSealedVerb, "c-2", sealer, `{"commitment": "sha256:abcd"}`),
		recBy("claim.taken", "c-2", "fp-implementer", "{}"),
	}
	if a := Audit(other); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("a claim by a key that sealed nothing is no breach: %+v", a.GuardrailBreaches)
	}
}
