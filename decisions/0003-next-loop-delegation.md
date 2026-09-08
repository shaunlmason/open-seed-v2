# 0003: v1-loop delegation for Seed (`next:`) cards

Status: recorded 2026-08-30 (autonomy-contract decision, docs/next-build-plan.md
preamble rule 3). Scope: coordination mechanics only; no v1 config or code
changes.

## Context

The Seed mandate (AGENTS.md "Implementing the next version") is designed to
proceed without human intervention, coordinated through the v1 loop. Three v1
mechanics have no agent-side path:

1. `backlog → ready` (`promote`) is operator-class; `task create` always
   lands in `backlog`; the dispatch workflow is inert (no API key) and
   maintenance does not promote.
2. `blocked → ready` for `plan:<pr>` entries (`plan-unblock`) is
   operator-class, run hourly by seed-maintenance; between ticks a merged
   plan leaves its card unclaimable.
3. The operators roster is `.seed/config.toml` (protected surface outside
   `next/**`), so rostering an agent identity would itself require the
   escalation the autonomy contract reserves for protected-surface changes.

The session running this work is the owner's own session, kicked off by the
owner's standing instruction ("file the Phase 0 cards and start"; mid-run:
"you may not be able to follow existing conventions from v1"), holding the
owner's push credential. v1's identity model is asserted, not authenticated
(design R10): enforcement is push access plus server-attributed gates.

## Decision

- Operator **queue-shaping verbs** for `next:` cards — `promote` of cards
  this workstream files, and `plan-unblock` only after the named plan PR is
  genuinely merged or closed on the forge — run under the session principal
  (`shaunlmason`), on the standing kickoff mandate.
- All **work verbs** (claim, transition, comment, attach-evidence,
  lease-renew) run under the agent actor `seed-next-implementer`.
- Judgment verbs stay untouched: `accept`/`reject`/`close` remain with the
  human owner and the seed-maintenance workflow; nothing merges without the
  human gate, and the D4.5 reviewer-identity check keeps verify red until a
  non-implementer approval exists.
- When the owner is actively merging in batches, implementation for a card
  whose plan PR is still open may be prepared and pushed as a **draft** task
  PR; CI's plan-at-merge-base rule still structurally orders the merges
  (verify cannot pass before the plan lands), so the plan gate is enforced
  where it is real.

## Consequences

- The loop runs unattended between human merge batches, which is the
  mandate's stated design goal.
- The run log attributes queue-shaping to the principal whose mandate it
  executes; if the owner wants finer attribution, rostering a dedicated
  dispatcher identity in `.seed/config.toml` supersedes this ADR (that
  change is theirs to make; an implementation PR will never touch it).
- Reversal is one operator verb (`deprioritize`, or closing the session);
  no state is unrecoverable.
