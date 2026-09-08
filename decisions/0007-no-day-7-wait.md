# 0007: stage 4 does not wait seven days for the audit

Status: recorded 2026-09-08 by the operator (repository owner), reversing
the answer they gave earlier the same day. Scope: the timing of the
audit that [`0004`](0004-shadow-run-substitution.md) binds, and the gate
[`0006`](0006-v1-retirement.md) put on retirement stage 4. Amends both;
touches no charter text and no conformance row.

## Context

`0004` accepted the accelerated simulation in place of the live
seven-day shadow run, and moved the five-bar audit over the real chain
to **day 7 after the cutover**, treating the first week of real
operation as the measurement window the shadow run would have been.
`0006` then gated retirement stage 4, the deletion of v1, on that audit
reading clean, because stage 4's merge is what removes the shim that can
still drive v1.

Asked directly whether to keep that gate, the operator first kept it and
then reversed: the seven-day wait is not wanted.

## Decision

**The seven-day wait is dropped. The audit is not.**

`seed ledger audit` runs immediately after the cutover merges, over the
chain the import and the flip produced, and its reading is appended to
the packet's divergence log as `0004` requires. Stage 4 proceeds on a
clean reading rather than on a calendar. A red bar is still a defect
card and still blocks stage 4.

The audit costs seconds and its cost was never the wait. Keeping it
while dropping the delay preserves the check and removes what the
operator declined to pay for.

## What it trades away

Precisely one thing, and it is worth naming exactly, because it is
narrower than "the rollback window closes":

- **Not the history.** The frozen `seed-state` ref and every
  `seed-anchor/*` tag are kept permanently and are readable by stock git
  after stage 4. `next/fixtures/import/open-seed/` keeps the export the
  migration was drilled against. Stage 4 is an ordinary commit and is
  revertible, so the v1 files themselves can come back.
- **What is actually lost is learning time.** Waiting a week never made
  a rollback cheaper; it made one less likely to be needed, by giving
  real traffic a chance to surface a defect while v1 was still standing
  and the ledger held little the cards did not. Deleting immediately
  means any defect that would have appeared under a week of real use now
  appears after v1 is gone, against a ledger that has moved on, and
  reinstating v1 is a rebuild rather than a revert.
- **The audit proves less than it would have.** Run minutes after the
  flip, it reads a chain carrying the import and the declaration and
  almost no real operation. It still catches a broken import, a bad
  linkage or an unreserved spend in the imported history, which is the
  bulk of what the flip risks. It cannot catch a defect that only
  appears under load, because there has been none.

## Risk statement

The residual risk is that a loop-level defect lands after v1 is
unavailable to fall back to. It is bounded by three things the tree
already carries, and by one the operator accepts:

- The chain is append-only and verifiable from genesis. Nothing is lost
  or silently rewritten, whatever the defect.
- The whole flip was rehearsed end to end against this repository's real
  v1 state before any key was spent, and that rehearsal is what found
  the unmapped-verb defect. The import path is the part most exercised.
- Stage 4 is one commit and revertible; the history it removes is kept.
- Accepted: a defect surfacing days later is answered by fixing Seed
  forward, not by returning to v1. The operator has decided that is the
  right trade for this repository, which has no external users and no
  third party depending on either system.

## What this does not change

- **The audit still runs and still gates stage 4.** This changes when,
  not whether. A red bar blocks the deletion.
- **The cutover is still the reserved escalation**, and still needs the
  operator's key and their recorded answer. This record removes a wait,
  not a prerequisite: retirement is not reachable any sooner without
  the flip.
- **`0004`'s substitution stands.** The simulation still supplies
  criterion 4; only the follow-up audit's timing moves.
- **Charter III.R.** Unaffected, and still `not measured` until a real
  deployment measures it. If anything this record makes R.5, the week
  unattended on a real backlog, later rather than sooner.

## Consequences

- `docs/v1-retirement.md` stage 4 is gated on a clean audit reading
  taken after the flip, with no waiting period.
- The runbook's Part 3 becomes a step of the flip rather than a return
  visit a week later.
- Reversal: a later decision may reinstate a waiting period before stage
  4 at any time before stage 4 merges. After it merges there is nothing
  left to wait for.
