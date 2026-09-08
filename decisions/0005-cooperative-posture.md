# 0005: the self-hosting cutover runs at the cooperative posture

Status: recorded 2026-09-08 by the operator (repository owner). Scope:
build plan §5's posture precondition only. Amends
`docs/next-build-plan.md` §5; touches no charter text, no code, and no
conformance row.

## Context

Build plan §5 opens its criteria with a posture clause: "Promotion to
self-hosting is met when, **at the enforced self-hosted posture**,"
followed by the seven criteria. All seven read `met` in the packet
(`next/docs/promotion.md`). What stands between the packet and the flip
is the deployment, which the autonomy contract reserves to the operator.

At `enforced-self-hosted` that deployment is a bare git repository on a
server the operator runs, with the `seed-admit` binary as its
`pre-receive` hook, carrying `refs/seed/ledger`. At
`enforced-forge-hosted` it is GitHub branch protections plus a hosted
`seed-admit serve` endpoint. At `cooperative` it is neither: the ledger
ref lives on the forge the code already lives on, every writer
self-validates through the same `internal/admit` rule set before
pushing, and no server-side component exists.

The three postures differ in **where** the rules run, never in **which**
rules run: `internal/admit` is imported by the cooperative client, by the
hook and by the service, and the one-derivation drill holds all three to
the same answer on the same corpus (`next/spec/postures.md`). Charter
III.B row 3 requires that all three be implemented, that every deployment
declare which it runs, and that cooperative's consequences be stated in
plain words. They are: `posture.Consequence` is printed verbatim by
`seed doctor`.

The operator's reason for choosing it is the one the row anticipates:
this repository has no external users, no credential is shared with any
actor outside the owner's own sessions, and the infrastructure the
enforced postures need is the single thing keeping v1 alive as the
coordination entry point.

## Decision

The self-hosting cutover is made at the `cooperative` posture. Build plan
§5's posture clause is amended to say so and to cite this record. The
seven criteria are unchanged and are read at that posture.

This is a deviation from the plan as written, accepted by a human as the
deviation it is, under §5's own rule for a step that trades away what the
plan requires. It is not presented as consistent with the original
phasing, and it is a larger deviation than
[`0004`](0004-shadow-run-substitution.md)'s: 0004 substituted the
evidence for one criterion, and this substitutes the security posture all
seven are read at.

## What it trades away

The charter's §I.2 security invariant does not hold at this posture. In
the conformance table's own terms, the rows marked `enforced-only` state
guarantees that a cooperative deployment does not provide, whatever their
implementation status:

- **III.B row 1's enforced half.** No admission validator is the ledger
  ref's sole writer. Every credential with push access can write the ref
  directly.
- **III.B row 4.** An actor's git credential *can* write the ledger ref
  directly, which the enforced drill exists to prove impossible.
- **III.B row 5, and with it build plan §5 criterion 7.** The
  compromised-actor drill stays green in CI, because it runs the enforced
  posture in its fixtures. What it demonstrates does not transfer to this
  deployment: a valid-key adversary with raw git access is refused by the
  drill's hook and would not be refused by this deployment. Criterion 7 is
  satisfied as an implementation claim, not as a live protection.

The rows themselves stay `met`. They are claims about what Seed
implements, and this record changes what this repository deploys, not
what Seed implements. No conformance row is edited, and no row's evidence
is weakened.

## Risk statement

The residual risk is bounded by who holds credentials, and that bound is
the whole basis for accepting it:

- Push access to this repository is the operator's alone. A hostile
  writer would already be a compromised owner credential, against which
  the enforced postures protect the ledger but not the code the ledger
  coordinates.
- The chain remains append-only, signed, and verifiable from genesis by
  every reader. Cooperative removes the *refusal*, not the *detection*:
  `seed ledger verify` still finds a forged signature, a bad `prev`, a
  reorder or a rewrite, and `seed ledger audit`'s five bars still read
  over the real chain.
- The day-7 audit that `0004` binds runs unchanged, and reads the same
  chain it would have read at an enforced posture.
- Reversal is cheap and needs no migration. Moving to an enforced posture
  later is a change of `posture` in `seed.json` plus the infrastructure
  that posture names; the ledger, its history and the rule set are
  untouched, because the postures differ only in where the rules run.

## What this does not change

- **The criteria.** All seven still gate the cutover, on §5's text as
  amended by this record and by `0004`. This record answers none of them.
- **The cutover is still the reserved escalation.** This record chooses a
  posture. It does not answer the Self-hosting question, stand up the
  deployment, or merge the cutover pull request.
- **The declaration is still required.** Cooperative is a *declared*
  posture, never a default: `posture.Load` refuses an undeclared
  deployment (`ErrUndeclared`), and III.B row 3's "no deployment lands in
  cooperative mode by default" is met by this record being an explicit
  choice with its consequence recorded.
- **Charter III.R.** Unaffected. Its rows stay `not measured` until a real
  deployment measures them.

## Consequences

- The deployment reduces to: `seed.json` at the repository root declaring
  `cooperative`, the operator's key as governance root, one enrolled key
  per acting lane, and the ledger ref pushed to the existing remote. No
  server, no hook, no service, no new credentials.
- The packet's "The deployment" section and its proposed declaration block
  are restated at this posture. The `admission` block stays absent, as the
  spec requires under any posture but `enforced-forge-hosted`.
- `seed doctor` prints `posture.Consequence` verbatim on every run against
  this deployment. That output is the intended standing reminder, and is
  not to be suppressed.
- This unblocks [`0006`](0006-v1-retirement.md): the infrastructure
  requirement was the sole remaining obstacle to retiring v1.
