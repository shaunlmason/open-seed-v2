// Package obligation derives what is OWED on each subject
// (plans/os-52d5da3f.md; docs/build-plan.md Phase 9 item 5).
// Seed represents permission already: admit.Affordances answers "what
// may I do", computed from the rule set admission enforces. Nothing
// answered "what is owed, by whom, since when, and which verbs
// discharge it", although every fact needed is folded already: the
// active claim with its fence, the bound submission, the standing
// verdict, run starts against run settles, open reservations, and the
// state itself with the position that set it.
//
// This package is a projection over that fold and never a new
// authority. It invents no legality: a state-shaped obligation reads
// its discharging verbs from the transition table, and the closed set
// of fact-shaped obligations (whose closing verb changes no lifecycle
// state and so appears in no table row) maps each to the spec that
// pairs it with its fact.
package obligation

import (
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// The obligation kinds. The list is closed on purpose: an open-ended
// taxonomy would make this projection a policy surface rather than a
// derivation (plans/os-52d5da3f.md D3).
const (
	// KindClaimHeld is an active claim window: the holder owes a
	// deliberate exit.
	KindClaimHeld = "claim.held"
	// KindSubmissionPending is a submission awaiting judgment: the
	// verifier lane owes a verdict.
	KindSubmissionPending = "submission.pending"
	// KindVerdictUnmerged is a pass verdict whose merge is not yet
	// observed.
	KindVerdictUnmerged = "verdict.unmerged"
	// KindRunUnsettled is an admitted run.started whose fence carries
	// no run.settled, flagged only once the window can no longer
	// settle it (position-anchored; the Phase 7 exit's
	// metering-detection obligation).
	KindRunUnsettled = "run.unsettled"
	// KindBudgetOpen is an open valid reservation: it stands from the
	// reserve until a settle or a release closes it, inside the window
	// that opened it and after (plans/os-d6963652.md D3).
	KindBudgetOpen = "budget.open"
	// KindContractBlocked is a blocked subject awaiting whoever the
	// block named.
	KindContractBlocked = "contract.blocked"
	// KindSubmissionUnmergeable is a submission the forge's latest
	// observation says is not mergeable as it stands: a red check, an
	// unresolved review thread, or a review requesting changes
	// (plans/os-0cd18799.md D3). Owed by the dispatch lane, since the
	// return is queue management, and discharged by contract.returned
	// citing the observation. Not emitted once a fail verdict stands,
	// because the return is then owed on the verdict's account.
	KindSubmissionUnmergeable = "submission.unmergeable"
	// KindEscalationPending is a standing blocked(needs-you): a
	// question addressed to a human gate that nothing else about the
	// contract moves past (plans/os-f781f0da.md). It is the narrower
	// sibling of KindContractBlocked, and both are emitted on an
	// escalated subject: the first says a human owes a decision, the
	// second that the contract is stopped.
	KindEscalationPending = "escalation.pending"
	// KindVerdictHuman is a human-verdict deferral nobody has rendered
	// over (plans/os-2e34f66a.md D4): owed by the operator lane, since
	// a human is a key with operator standing, and discharged by the
	// verdict.rendered such a key makes on the same submission.
	KindVerdictHuman = "verdict.human"
	// KindReapOwed is an in_progress claim whose holder has been revoked:
	// it can end no other way than a reap, because a revoked holder
	// cannot submit, release or park (plans/os-32d06c65.md D4). Distinct
	// from KindClaimHeld, which any deliberate exit discharges.
	KindReapOwed = "claim.reap-owed"
	// KindRequestPending is an inbound request nobody has answered
	// (plans/os-48df10a2.md D2): owed by the dispatch lane, one row per
	// subject carrying the oldest unanswered request's position and
	// timestamp, discharged by the dispatcher's request.answered.
	KindRequestPending = "request.pending"
	// KindApprovalPending is a per-verb approval request nobody has
	// answered (plans/os-5781a026.md D5): owed by the operator lane,
	// one row per subject carrying the oldest open request's position
	// and timestamp (identity is (Subject, Kind), so a subject with
	// several open requests shows the oldest until it is answered),
	// discharged by the operator's approval.granted or approval.denied.
	KindApprovalPending = "approval.pending"
)

// Lane names used where an obligation is owed by a role rather than
// by one fingerprint: independence forbids naming the claimant as the
// verifier, and a merge is observed by whoever holds the standing.
const (
	LaneVerifier   = "lane:verdict"
	LaneObserver   = "lane:observer"
	LaneSupervisor = "lane:supervise"
	LaneOperator   = "lane:operator"
	LaneDispatcher = "lane:dispatch"
)

// factDischargers is the closed set of fact-shaped obligations: their
// closing verb changes no lifecycle state, so it appears in no
// transition-table row and must be mapped from the spec that pairs it
// with its fact (review finding on the plan PR: a table-only
// derivation advertises no discharger at all for these).
var factDischargers = map[string][]string{
	// spec/executors.md: metering settles at run end.
	KindRunUnsettled: {"run.settled"},
	// spec/budgets.md: a reservation closes by settle or release.
	KindBudgetOpen: {"budget.settle", "budget.release"},
	// spec/verdicts.md: the verdict is a fact, not a transition.
	KindSubmissionPending: {"verdict.rendered"},
	// spec/observations-forge.md: the return cites the red
	// observation; the row exists in the table (review to ready) but
	// the kind is fact-shaped, since it arises from the observation and
	// not from the state.
	KindSubmissionUnmergeable: {"contract.returned"},
	// spec/escalation.md: both answers close the question, and
	// cancelling counts because it must cite the escalation it
	// closes — an answer of "this work should not happen".
	KindEscalationPending: {"contract.cancelled", "decision.recorded"},
	// spec/verdicts.md: the human's render answers the deferral.
	KindVerdictHuman: {"verdict.rendered"},
	// spec/requests.md: the dispatcher's answer closes a request.
	KindRequestPending: {"request.answered"},
	// spec/protocol.md "Per-verb approval": either answer closes
	// the request; the grant is then spent by the act, the denial
	// admits nothing.
	KindApprovalPending: {"approval.denied", "approval.granted"},
}

// mergeRequestVerb discharges a standing pass verdict that no merge
// request cites yet. The merge chain is two events, not one: the
// drift sweep caught the first draft advertising merge.observed while
// admission still refused it for want of a request, which is exactly
// the class the sweep exists to raise
// (spec/reconciliation.md: each chain step is its own event).
const mergeRequestVerb = "merge.requested"

// Row is one obligation. Identity is (Subject, Kind): the situation
// read's delta names removals by that pair, so it is normative rather
// than incidental (plans/os-52d5da3f.md D4).
type Row struct {
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	// OwedBy is a fingerprint, or a "lane:<capability>" name where the
	// obligation belongs to a role rather than one actor.
	OwedBy string `json:"owed_by"`
	// Since is the chain position the obligation arose at.
	Since int `json:"since"`
	// DischargedBy is every verb that discharges it, sorted. Never
	// empty: an obligation nobody can discharge is an anomaly, not an
	// obligation, so a kind with no reachable discharger is not
	// emitted at all.
	DischargedBy []string `json:"discharged_by"`
	// Head is the commit the obligation is about, present only on the
	// forge-observed kind (plans/os-0cd18799.md D3), so the situation
	// read says which revision is red.
	Head string `json:"head,omitempty"`
	// TS is the raising event's own timestamp, present only where the
	// obligation's age is meaningful in elapsed time. Positions order
	// without measuring: an escalation untouched for hours has the
	// same position difference as one answered instantly after a burst
	// of unrelated traffic, so latency derived from Since would be
	// event count wearing a clock's clothes. The reading surface
	// computes now minus TS at its own instant, never at admission
	// (spec/offers.md's live-read posture).
	TS string `json:"ts,omitempty"`
}

// stateDischargers reads the verbs that leave a state from the
// transition table, so legality is never restated here.
func stateDischargers(table *transition.Table, state string) []string {
	var out []string
	for _, verb := range table.Verbs() {
		if table.Allows(state, verb) {
			out = append(out, verb)
		}
	}
	sort.Strings(out)
	return out
}

// Deps are the derivations this projection READS rather than
// recomputing, so it stays a projection over one fold and never a
// second opinion.
type Deps struct {
	// BudgetOpen supplies the open valid reservations for a subject:
	// the caller passes the one shared budget derivation rather than
	// this package re-deriving validity.
	BudgetOpen func(subject string, s transition.SubjectState) []transition.ReservationFact
	// CanDischarge reports whether an actor may still perform ANY of
	// the named verbs. Standing is the keyring's authority, not this
	// package's, and ownership of a fact-shaped obligation follows who
	// can pay it (plans/os-d6963652.md D4). A nil predicate is "no
	// standing projection was supplied", which cannot establish that
	// anyone is unable, so the usual owner stands.
	CanDischarge func(actor string, verbs []string) bool
}

// able is the standing question with the nil case named once.
func (d Deps) able(actor string, verbs []string) bool {
	return d.CanDischarge == nil || d.CanDischarge(actor, verbs)
}

// Derive folds the records and returns every standing obligation, in
// a stable order (subject, then kind).
func Derive(records []*event.Record, table *transition.Table, deps Deps) []Row {
	fold := table.FoldRecords(records)
	// The keyring's standing is what tells the reap obligation from an
	// ordinary held claim: a revoked holder's open window owes a reap.
	ring, _, _ := keyring.StateAt(records)
	var rows []Row
	for _, subject := range fold.Subjects() {
		s, ok := fold.State(subject)
		if !ok {
			continue
		}
		rows = append(rows, subjectRows(subject, s, table, deps)...)
		if ring != nil && s.Claim != nil && s.State == "in_progress" {
			if e, ok := ring.Get(s.Claim.Holder); ok && e.Standing == keyring.StandingRevoked {
				rows = append(rows, Row{Subject: subject, Kind: KindReapOwed, OwedBy: LaneOperator, Since: s.Claim.Fence, DischargedBy: []string{"claim.reaped"}})
			}
		}
	}
	rows = append(rows, requestRows(fold)...)
	rows = append(rows, approvalRows(fold)...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Subject != rows[j].Subject {
			return rows[i].Subject < rows[j].Subject
		}
		return rows[i].Kind < rows[j].Kind
	})
	if rows == nil {
		rows = []Row{}
	}
	return rows
}

func subjectRows(subject string, s transition.SubjectState, table *transition.Table, deps Deps) []Row {
	var rows []Row
	// ts is empty except where an obligation's age is meaningful in
	// elapsed time; it is a parameter rather than a returned pointer
	// because a pointer into rows would dangle the moment the next
	// append reallocated.
	add := func(kind, owedBy string, since int, ts string, dischargers []string) {
		// Never advertise an empty discharging set: the sweep asserts
		// this, and emitting one would make the drift class pass
		// vacuously (review finding on the plan PR).
		if len(dischargers) == 0 {
			return
		}
		rows = append(rows, Row{Subject: subject, Kind: kind, OwedBy: owedBy, Since: since, TS: ts, DischargedBy: dischargers})
	}

	if s.Claim != nil {
		add(KindClaimHeld, s.Claim.Holder, s.Since, "", stateDischargers(table, s.State))
	}
	if s.State == "blocked" {
		add(KindContractBlocked, LaneOperator, s.Since, "", stateDischargers(table, s.State))
	}
	if s.Escalation != nil {
		// Since is the RAISE's position, not the state's: a question
		// carried by a claim.parked arrives with the exit that raised
		// it, and a later reader needs the position that asked, not
		// the one that blocked. The row also carries the raise's TS,
		// because age is elapsed time and a position measures nothing
		// (spec/escalation.md).
		add(KindEscalationPending, LaneOperator, s.Escalation.Pos, s.Escalation.TS, factDischargers[KindEscalationPending])
	}
	// A verdict is owed only while the subject is under review: a
	// return by observation (plans/os-0cd18799.md D4) re-readies the
	// subject with its submission unjudged, and a verdict there is a
	// debt nobody can discharge, which the drift sweep names.
	if s.State == "review" && s.Submission != nil && (s.Verdict == nil || s.Verdict.Submission != s.Submission.Pos) {
		add(KindSubmissionPending, LaneVerifier, s.Submission.Pos, "", factDischargers[KindSubmissionPending])
	}
	// The forge's latest word on the head under review says it is not
	// mergeable (plans/os-0cd18799.md D3): the dispatch lane owes the
	// return, unless a fail verdict already stands, in which case the
	// return is owed on the verdict's account and this row would name
	// the same debt twice.
	red := s.State == "review" && s.Observation != nil && s.Observation.Red() && len(s.SubmissionFails) == 0
	if red {
		rows = append(rows, Row{Subject: subject, Kind: KindSubmissionUnmergeable, OwedBy: LaneDispatcher, Since: s.Observation.Pos,
			Head: s.Observation.Head, DischargedBy: append([]string(nil), factDischargers[KindSubmissionUnmergeable]...)})
	}
	// A deferral on the current window with no render after it: the
	// verifier could not judge, and the debt moved to the human.
	if s.Deferred != nil && (s.Verdict == nil || s.Verdict.Pos < s.Deferred.Pos) {
		add(KindVerdictHuman, LaneOperator, s.Deferred.Pos, "", factDischargers[KindVerdictHuman])
	}
	// An eval's chain ends at its verdict (plans/os-03e47abb.md D10):
	// it is never merged, its consequence is a qualification or a
	// disqualification, and a merge owed forever would be a debt
	// nobody can pay.
	// While the forge says red, merge.requested refuses (D4), so the
	// merge debt is not advertised: the return is the debt that
	// stands, and an obligation nobody can discharge is an anomaly.
	if s.Verdict != nil && s.Verdict.Verdict == "pass" && s.Merged == nil && s.Eval == nil && !red {
		// One kind, two shapes, because the merge chain is two
		// events: until a request cites the verdict the debt is the
		// operator's and merge.requested pays it; after that the
		// forge fact is the observer's to record.
		if s.Requested == nil {
			add(KindVerdictUnmerged, LaneOperator, s.Verdict.Pos, "", []string{mergeRequestVerb})
		} else {
			add(KindVerdictUnmerged, LaneObserver, s.Requested.Pos, "", []string{"merge.observed"})
		}
	}
	// An open reservation is owed WHEREVER it stands (os-d6963652):
	// the earlier in_progress restriction existed only because
	// admission gated the closing verbs on the same state, so outside
	// the window the advertised dischargers were unreachable and the
	// row would have been an anomaly. Admission now gates only the
	// reserve, so the closes are reachable and the debt is an
	// obligation again — which matters most on the failed-verdict
	// retry, where the next claimant is a different worker and the
	// previous attempt's unclosed hold would silently tax them.
	//
	// The owner is whoever can still pay it, never whoever holds the
	// window: admission closes a reservation for its own reserving
	// signer or the operator lane and nobody else, so attributing the
	// row to the current holder named a party admission refuses on any
	// reservation the holder did not sign. The signer keeps it until
	// suspension or revocation means every close from them refuses,
	// and then the operator lane is the only party left; keying the
	// row to a fingerprint nobody can sign for would hide it from the
	// one actor able to act on it.
	if deps.BudgetOpen != nil {
		for _, r := range deps.BudgetOpen(subject, s) {
			owner := r.Signer
			if !deps.able(owner, factDischargers[KindBudgetOpen]) {
				owner = LaneOperator
			}
			add(KindBudgetOpen, owner, r.Pos, "", factDischargers[KindBudgetOpen])
			break
		}
	}
	for _, start := range s.RunStarts {
		if settled(s, start.Fence) || !runFlaggable(s, start.Fence) {
			continue
		}
		add(KindRunUnsettled, LaneSupervisor, start.Pos, "", factDischargers[KindRunUnsettled])
		break
	}
	return rows
}

func settled(s transition.SubjectState, fence int) bool {
	for _, r := range s.Runs {
		if r.Fence == fence {
			return true
		}
	}
	return false
}

// runFlaggable is the position anchor the Phase 7 exit named: an
// unsettled run is flagged only once the subject has taken a
// SUBSEQUENT claim window or reached a terminal state, because
// post-close settlement is a valid intermediate state and a
// closed-without-settle predicate would file spurious findings
// mid park or reap flow.
func runFlaggable(s transition.SubjectState, fence int) bool {
	if s.State == "done" || s.State == "cancelled" {
		return true
	}
	for f := range s.ClaimFences {
		if f > fence {
			return true
		}
	}
	return false
}

// requestRows is the unanswered requests as obligations on the
// dispatch lane (plans/os-48df10a2.md D2): one row per subject, since
// identity is (Subject, Kind), carrying the oldest unanswered request
// on it; the situation read lists every request as its own notice.
// The requests on system are owed like the ones on a contract.
func requestRows(fold *transition.Fold) []Row {
	var rows []Row
	seen := map[string]bool{}
	for _, r := range fold.Requests() {
		if r.Answered != nil || seen[r.Subject] {
			continue
		}
		seen[r.Subject] = true
		rows = append(rows, Row{
			Subject:      r.Subject,
			Kind:         KindRequestPending,
			OwedBy:       LaneDispatcher,
			Since:        r.Pos,
			TS:           r.TS,
			DischargedBy: append([]string(nil), factDischargers[KindRequestPending]...),
		})
	}
	return rows
}

// approvalRows is the unanswered approval requests as obligations on
// the operator lane (plans/os-5781a026.md D5): one row per subject,
// since identity is (Subject, Kind), carrying the oldest open request
// on it, so the request lands in the inbox the operator orients from
// with its age in elapsed time. A request on system is owed like one
// on a contract.
func approvalRows(fold *transition.Fold) []Row {
	var rows []Row
	seen := map[string]bool{}
	for _, a := range fold.Approvals() {
		if a.Answered != nil || seen[a.Subject] {
			continue
		}
		seen[a.Subject] = true
		rows = append(rows, Row{
			Subject:      a.Subject,
			Kind:         KindApprovalPending,
			OwedBy:       LaneOperator,
			Since:        a.Pos,
			TS:           a.TS,
			DischargedBy: append([]string(nil), factDischargers[KindApprovalPending]...),
		})
	}
	return rows
}
