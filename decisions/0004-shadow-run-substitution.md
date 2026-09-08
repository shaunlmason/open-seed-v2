# 0004: the accelerated simulation stands in for the shadow run at the self-hosting cutover

Status: recorded 2026-09-06 by the operator (repository owner), in the
owner's own session. Scope: build plan §5 criterion 4 only. Amends
`docs/next-build-plan.md` §5; touches no charter text, no code, and no
conformance row.

## Context

Build plan §5 gates the self-hosting cutover on seven criteria. Six are
`met` on `main`. Criterion 4, the shadow run, reads: "Seed coordinates a
declared slice of this repository's own cards beside v1 for a stated
window, with any divergence reconciled and recorded." The packet
(`next/docs/promotion.md`) records it `partial`: the operator accepted
the credential-free accelerated simulation in its place (#316), review
held that a packet cannot amend the plan it presents, and the packet was
re-derived (#327) to say the criterion stays `partial` until one of two
things exists, neither of them agent work: the live shadow run on a real
deployment, or an amendment of §5 by a recorded decision of the
operator.

The live run needs a deployment at the enforced self-hosted posture (a
git server the operator runs with `seed-admit` as its `pre-receive`
hook, a root key, one key per lane) standing for seven days beside v1
while agents mirror every v1 transition on a declared slice to the
ledger and diff the two daily. The deployment is also what the cutover
itself needs, so the live run costs a week of dual-running the same
infrastructure the flip needs anyway, on a slice that has since gone
stale (the packet's proposed slice named cards that have merged).

The simulation (`seed simulate --lanes next/lanes --intents 24 --days 7
--posture enforced-self-hosted`) drives a synthetic backlog through the
real boundary: admission, the `seed-admit` hook on a local bare remote,
the ledger, every lane manifest, and the five-bar audit, with zero
credentials and a mock executor. It runs in seconds. A fresh reading on
this commit's tree (draw seed 20260906): 24 of 24 intents reached `done`
over the accelerated seven days, the audit clean and declared, all five
bars empty (chain violations, lost updates, silent abandonments,
guardrail breaches, unreserved spend). The drills
`TestSimulateReachesDoneEnforced`, `TestSimulateAcceleratedBacklog` and
`TestAuditCatchesSilentAbandonment` hold that reading under `make
check`.

## Decision

The operator accepts the accelerated simulation as criterion 4's
evidence for the self-hosting cutover. Build plan §5 criterion 4 is
amended to say so and to cite this record. The criterion reads `met` in
the packet on the amended text, not on its original words.

This is a deviation from the plan as written, accepted by a human as the
deviation it is, under §5's own rule for a step that trades away what
the plan requires. It is not presented as consistent with the original
phasing.

## What it trades away

The simulation supplies what the shadow run would have supplied for the
mechanisms: the boundary admits and refuses, the lanes reach `done`
through the real verbs, the audit reads clean over a real chain. It does
not supply what the shadow run would have supplied about this
repository:

- **Divergence reconciliation.** No diff of Seed's folded contract
  states against v1's card states on real cards, so no evidence that the
  two systems agree about the same work before authority moves.
- **A real backlog and real actors.** The intents are synthetic, the
  executor a mock, the pull requests absent; nothing exercises the lane
  fragments against this repository's actual cards, plans and CI gates.
- **A real week.** The clock is accelerated. Lease expiry, maintenance
  ticks, checkpoint cadence and operator absence over seven wall-clock
  days are not observed.
- **Escalations.** The simulation raises none, so the packet, question
  and decision shape is exercised only by its own drills.

## Risk statement

Defects the shadow run would have surfaced before authority moved will
surface after it, on the ledger that is authoritative. The mitigations
are the ones the tree already carries, and the rollback:

- The ledger is append-only and the path back is written down
  (`next/docs/promotion.md`, "The path back"): revert the cutover pull
  request, unfreeze v1's `seed-state` ref at its final anchor, file any
  ledger-only contracts back as v1 cards from the contracts projection.
  No history is lost in either direction.
- The compromised-actor drill is green in CI (criterion 7), the import
  is drilled against this repository's real v1 export (criterion 3), the
  200-writer contention benchmark read clean twice (#297), and the
  procedure from empty ledger to flip is followed by a test
  (`TestPacketProcedureReachesTheFlip`).
- The first seven days after the cutover are treated as the measurement
  window the shadow run would have been: the five-bar audit (`seed
  ledger audit`) runs over the real chain at day 7 and its reading is
  appended to the packet's divergence log; a red bar is a defect card
  and a candidate for the rollback, not a note.

## What this does not change

- **Charter III.R** is not the build plan's to amend. Every III.R row
  stays `not measured` until a real deployment measures it; the
  simulation measures none. The Phase 13 exit record stays parked on
  those rows as before.
- **The two cutovers remain reserved escalations.** This record supplies
  criterion 4. It does not answer the Self-hosting question, stand up
  the deployment, or merge the cutover pull request; each is a separate
  operator act in the order the packet's "The deployment" gives.
- **The live protocol stays written down** in the packet as the record
  of what was proposed and traded away, and remains available to run on
  the deployment after the cutover if the day-7 audit warrants it.

## Consequences

- The packet's criteria table reads seven `met`. What stands between the
  packet and the flip is the deployment and the operator's recorded
  answer, both reserved to the operator.
- The critical path in §5 loses one node: the shadow run no longer
  precedes the cutover; the day-7 audit follows it.
- Reversal: a later decision may reinstate the live run before the
  distribution step, whose own preconditions (self-hosting held for a
  stated period without a rollback) this record does not touch.
