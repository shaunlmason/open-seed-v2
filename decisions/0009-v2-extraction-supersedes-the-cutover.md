# 0009: the v2 extraction supersedes the in-place cutover

Status: recorded 2026-09-08 by the operator (repository owner), who
directed the extraction. Scope: the route retirement takes. Amends
[`0006`](0006-v1-retirement.md) and the plan in
`docs/v1-retirement.md`; touches no charter text and no conformance row.

## Context

`0006` planned retirement as the tail of build plan §5's self-hosting
cutover: authority moves to a Seed deployment inside this repository,
then v1 is deleted. Everything up to the flip was built and verified,
including a full rehearsal against this repository's real v1 state.
The flip itself never happened, because it needs a governance root key
the operator had not minted.

The operator then took a different route: Seed was extracted into
**shaunlmason/open-seed-v2** as an independent repository. That route
needs no key, because no authority moves inside this repository at all.
The successor simply stands somewhere else.

## Decision

The extraction is the route. Concretely:

- **Stage 3, the cutover, is moot.** There is no ledger deployment in
  this repository and there will not be one. `seed.json`, the import,
  the preseed and the enrolments are not performed here.
- **Stage 4 executes now.** Its precondition was a standing successor,
  and that precondition is met by open-seed-v2 rather than by a flip.
- **`next/` is deleted too.** It is the successor's code and now lives
  in open-seed-v2. Keeping a second copy here would let the two diverge
  with no gate to catch it, which is worse than deleting it.

## What it trades away

- **The migration is not run.** No v1 card history is imported into a
  ledger, so the coordination record ends where it ends rather than
  continuing into Seed. The record is not lost: the `seed-state` ref and
  every `seed-anchor/*` tag are kept permanently, and the import fixture
  in open-seed-v2 holds the drilled export. What is given up is
  continuity of *coordination*, not of history.
- **The cutover's evidence goes unused.** The rehearsal, the corrected
  flip order and the runbook remain in this repository's history as the
  record of a route not taken. The defect that rehearsal found (three
  unmapped run-log verbs) is fixed in the successor's transform table
  and still matters to anyone who imports later.
- **This repository stops being able to coordinate itself.** After stage
  4 there is no card queue, no receipt gate and no maintenance pass
  here. That is the intended end state: the work moved.

## Risk statement

- Deleting `next/` is safe **only because** open-seed-v2 carries it and
  is green: `make check` there passes with the suite at 91.0% coverage
  against a 90% gate. Verified before the deletion, not assumed.
- The deletion is one commit and is revertible. The history it removes
  stays in this repository's git history.
- The frozen `seed-state` ref and its anchor tags are untouched by a
  file deletion; they are refs, and stock git still reads them.

## What this does not change

- **Charter and Part III** are the successor's concern now, unchanged.
- **The engine repository.** `shaunlmason/open-seed-engine` is archived,
  not deleted, per the plan's stage 5. Its releases are the provenance
  the frozen history was produced under.
- **The records.** `decisions/`, `plans/`, `receipts/`, `memory/` and
  `docs/research/` stay. A record is not retired because its subject is.
