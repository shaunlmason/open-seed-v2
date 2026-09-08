# Modes

Small-team mode and fleet mode (SEED-NEXT.md conformance III.J's
closing row; `docs/build-plan.md` Phase 9 item 4;
`plans/os-6a08b166.md`): the two deployments Seed claims to run, both
driven end to end in CI.

> *"Small-team mode (one **principal** operating a minimal set of actor
> identities — at least an implementing actor and a distinct verifying
> actor, so verdict key disjointness holds even when one person runs
> everything) and fleet mode (disjoint actors per lane) both run the
> full loop in CI."*

## The mode is the identity plan, and nothing else

Both modes run on the **remote** posture, and neither is a transport
choice:

- **Fleet needs the remote** because contention between two workers
  racing `claim take` needs the optimistic push loop.
  `ledger.Store.Append` has none: it compares the signed `Prev` against
  the reconciled tip and returns `ErrWrongPrev`, full stop. A local
  fleet would produce hard append errors rather than ordinary
  contention that converges.
- **Small-team cannot run locally at all.** `claim take` is refused off
  the remote — *"an exclusive verb and claiming is online-only:
  exclusivity is granted at admission, so it needs `--remote`; two
  offline actors claiming the same contract have not claimed anything"*
  — and a claim is the loop's first act.

So the modes differ **only** in which identities exist, which is what
the charter says they are. Both are built by one function from one
identity plan: two drills that mean to be the same run drift, and a
difference that drifts in is invisible.

**The plan provisions grants from the SHIPPED lane manifests**
([`lanes/`](../lanes)), never from a table the fixture keeps. A
manifest that stops describing its lane therefore fails in the
fixtures, when the key it provisions stops being able to act.

## The full loop

Both modes drive the same chain, each step its own admitted event:

`claim.taken` → `budget.reserve` → work → `budget.settle` →
`submission.made` → `verdict.rendered(pass)` → `merge.requested` →
`merge.observed` → **done**.

The terminal three had no CLI verb before Phase 9 item 4 —
`merge.requested` and `merge.observed` existed only through `ledger
append`, the raw dev seam that runs no rules, and `verdict render` was
local-only. All three now reach both postures
([`reconciliation.md`](reconciliation.md), [`verdicts.md`](verdicts.md)).

**Setup may use the raw seam; every asserted property is produced by an
admitted act.** Enrolments, grants, intents, specs and offers are
background facts. A `verdict.rendered` written with `ledger append` is
signed, folded, and indistinguishable from an admitted one — and
proves nothing about the verdict rule, because no rule ran.

## Verdict key disjointness is proven against the case that threatens it

Small-team mode's whole point is that disjointness holds **when one
person runs everything**, and a principal running everything can grant
themselves everything. So the drill grants the implementing actor the
`verdict` capability and shows it is refused anyway:

- ungranted, that key refuses `out_of_grant` — **capability absence**,
  which is a different property and would have proven only that a key
  without the grant cannot render;
- granted, it refuses `not_independent`, naming the claimant or the
  bound submission's signer.

The distinct verifying key is admitted on the same subject, so the
refusal is about identity rather than about rendering being broken.

## Convergence: three arms, and the forbidden fourth

Every refusal a lane meets converges **within one retry**, in one of
three ways: an admitting act on its next attempt, a refreshed
position-stamped read showing the act is no longer owed, or an
escalation carrying that refusal's `code` and `message`. What is
forbidden is the fourth: a blind retry, a silent loop, or a bare
"failed".

`loop.Driver.Step` never retries — one iteration attempts each act
once and ends `Submitted`, `Parked` or `Idle` — so convergence is a
property of **consecutive iterations**, asserted from a transcript. The
recording wrapper wraps the REAL verbs and only observes: instrumenting
a loop by replacing the boundary it is being tested against proves
nothing about that boundary.

**The middle arm is planted as a LOST `claim take` RACE.** A concurrent
reap between a claim's push and the next read reaches `Step`'s
`!s.Holds(subject)` branch and returns `Idle` with **no verb refused at
all** — the claim admitted, and the window then vanished. Since every
arm answers a refusal, that setup could only be counted by relabelling
a successful claim as refusal convergence.

And two workers stepped one after another do not race: the second
polls after the first has claimed, finds nothing offered, and goes idle
at the poll with no refusal. So the rival claim is planted **inside the
window**, from the seam, just before the lane's own `claim take`
reaches the boundary — in the race by construction rather than by
timing, because a race reproduced by sleeping passes green on a slower
runner.

**The blind-retry detector** is the assertion that matters: the same
act refusing with the same code on consecutive iterations from a
position that did not advance. Same act, same knowledge, same answer is
a lane that learned nothing and tried again. It is drilled against a
known spin and four known non-spins, because a detector that has only
seen converging runs has not been shown to catch anything.

**Anti-vacuity.** Every convergence assertion above is true of a run
that met no refusal at all, which is exactly how these fixtures could
ship proving nothing. So the arm must be shown to have been
**exercised**, and the drill fails when it was not.

## Wakeless

Neither mode wires a wake channel of any kind, and that is pinned by
**surface** rather than asserted by absence: a drill cannot watch a
thing fail to happen. `internal/loop`'s `Verbs` is the single method
`Run`, and its options are exactly `WithSince`, `WithRepo`, `WithBase`
and `WithObservations` — a wake channel needs one of those seams, so
adding one fails the pin and this spec has to move with it. That is
what makes the one-inbox doctrine structural rather than hoped for.

## A gap the modes surfaced

The shipped lane set grants neither `supervise` nor `observer`, while
`offer.published` accepts only `supervise` or `operator` and
`merge.observed` only `observer` or `operator`. So a deployment
assembled purely from the six shipped lanes can neither publish the
offer its own workers poll for nor record the merge that ends the loop;
only the maintenance lane reaches either, through `operator`, which is
not its job.

The fixtures stage a supervisor and an observer as **background**
identities, outside the identity plan, so the grants assertion keeps
measuring only lane-derived ones. That is honest for a fixture and
wrong as a shipped posture, and it is carded (`os-d6a52784`) rather
than papered over: `lanes/**` is not this card's to change.

## The third posture in the fixture

Both modes run on the remote posture, and until Phase 12 item 2 the
only enforcing remote a fixture could build was the hook's. The
forge-hosted posture has a fixture too ([`postures.md`](postures.md),
"The fixture's model of the forge"): a bare remote with no admission
hook whose pre-receive lets the admission identity alone write the
ledger branch, and `seed-admit serve` as a subprocess the drills
propose to. Under a `seed.json` declaring `enforced-forge-hosted`, the
same CLI verbs the modes drive propose instead of push
(`cmd/seed/forge_cli_test.go`); the modes fixtures themselves keep the
hook posture, because what they prove is the identity plan, not the
transport.
