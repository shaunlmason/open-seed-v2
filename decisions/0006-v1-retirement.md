# 0006: open-seed v1 is retired after the self-hosting cutover

Status: recorded 2026-09-08 by the operator (repository owner). Scope: a
step after build plan §5's self-hosting cutover. Adds
`docs/v1-retirement.md` as that step's plan; touches no charter text and
no conformance row.

## Context

The build plan's autonomy contract reserves "changes to the protected
surface outside `next/**`" to the operator, and §5 reserves both
cutovers. Neither reservation covers what happens to v1 *after* authority
moves, because the plan never contemplated removing it: §0's ground rules
say "the v1 template keeps working, untouched, throughout," and the
packet's cutover freezes the `seed-state` ref read-only for its history
rather than deleting anything.

Two facts now make that silence the wrong default. v1 has no external
users: nothing outside this repository clones the template or execs the
pinned engine. And this repository is v1's one remaining user, through
the pull request gate, the hourly maintenance cron and the card queue on
the `seed-state` ref, all of which the self-hosting cutover replaces.
After the cutover, v1 is code that nothing runs, carrying a control
surface, an engine pin, a plugin, a flavors tree and a full documentation
set that every future reader must be told to ignore.

[`0005`](0005-cooperative-posture.md) removes the last obstacle by
choosing a posture whose deployment needs no infrastructure, so the
cutover no longer waits on a server that would have to be provisioned for
a system already scheduled for removal.

## Decision

v1 is retired. `docs/v1-retirement.md` is the plan: five stages, an
inventory of what is deleted, what is edited and what is kept
permanently, gated on the self-hosting cutover and on the day-7 audit
that `0004` binds.

This is the escalation the autonomy contract reserves, answered. Agents
may take the retirement stages as ordinary cards on the strength of this
record; each stage's own pull request remains reviewable, and no stage
skips the loop's gates.

## What it trades away

- **The template product.** open-seed's stated purpose is a template
  repository other projects instantiate, with flavors, a plugin, a
  marketplace entry and a handbook. Retirement ends that product. Seed's
  distribution step is a separate escalation with its own preconditions,
  and until it is taken there is nothing for a new user to clone.
- **The degradation ladder.** v1's contract is "files, not an app,"
  workable by one human with no engine installed. Seed is a Go binary
  and a signed chain. Whatever graceful degradation Seed offers is
  Seed's to state; v1's is withdrawn.
- **The rollback window.** The packet's "The path back" survives the
  cutover and dies at retirement stage 4, when the shim that reads the
  frozen ref is deleted. This is why the plan gates stage 4 on the day-7
  audit reading clean rather than on the cutover merging.
- **The engine repository as a live project.** `open-seed-engine` is
  archived, not deleted. Its releases stay readable, because they are the
  provenance the frozen history was produced under.

## Risk statement

- **A defect found after stage 4 cannot be answered by returning to v1.**
  Mitigation: the day-7 audit precedes stage 4, and the audit's five bars
  are the same measurement the shadow run would have supplied. A red bar
  defers stage 4 rather than filing a defect against it.
- **The frozen history could become unreadable.** Mitigation: it is
  ordinary git. The `seed-state` ref and every `seed-anchor/*` tag are
  kept permanently and readable by stock git; nothing about reading them
  needs the shim. `next/fixtures/import/open-seed/` additionally holds
  the export the migration was drilled against.
- **The pull request gate could lose coverage during the transition.**
  Mitigation: the plan's stage 1 moves only the two gate steps with
  ledger-free Seed equivalents and leaves the receipt and reviewer
  identity steps on v1 until stage 3 replaces them. The gate is never
  without an evidence check.
- **Documentation could be deleted while still cited.** Mitigation: stage
  2 pins the inventory to the tree under `make check`, so a path that
  moved or a citation that still resolves is found before the deletion,
  not by a broken link afterward.

## What this does not change

- **The cutover remains the reserved escalation.** This record schedules
  what follows it. It does not answer the Self-hosting question or merge
  the cutover pull request.
- **The distribution cutover.** Untouched, and not required for
  retirement. Retiring v1 and distributing Seed are independent after the
  flip.
- **The charter and Part III.** No conformance obligation is removed.
  III.R's rows stay measured against the live deployment.
- **The work products.** `plans/`, `receipts/`, `memory/`, `decisions/`
  and `docs/research/` are kept. A record is not retired because its
  subject is.

## Consequences

- `docs/v1-retirement.md` joins the charter, the build plan and the
  frontier as a document agents read; the frontier gains the retirement
  stages after the cutover's line.
- Stages 1 and 2 are claimable now, before the cutover. Stages 3 through
  5 are gated on it.
- Reversal: before stage 4 merges, this record can be reversed by
  reverting the cutover per the packet's "The path back". After stage 4
  it cannot be reversed, only re-implemented.
