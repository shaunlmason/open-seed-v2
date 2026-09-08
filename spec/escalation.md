# escalation.md — blocked(needs-you)

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §7 ("Escalation is `blocked(needs-you)`: an event addressed to a
> human gate carrying the packet, the question, and the minimal decision —
> never a transcript") and §11; [`docs/build-plan.md`](../docs/build-plan.md)
> Phase 9 item 2; plan `plans/os-f781f0da.md`. Implemented by
> `internal/escalation` and the `escalation` admission rule.

## A human's unit of interruption is one decision

An escalation is **one packet, one question, one decision**. The
packet is the context a successor would need anyway
([`packets.md`](packets.md)); the question is what is being asked; the
options are the closed set the answer may come from. Nothing else is
carried, and transcript-dumping is a refusal rather than a style note:
packet prose and question prose both fall under the classification
lint's aggregate free-text budget.

## Where an escalation enters the lifecycle

The distinction that matters is not "a new verb versus a qualifier" —
it is **which state a verb can leave**.

[`lifecycle.md`](lifecycle.md) pins the four deliberate exits from
`in_progress` by self-validation, and III.F ("every exit from
`in_progress` is deliberate … silent abandonment is impossible by
construction") depends on that set being **closed**. So nothing new
may leave `in_progress`, and there an escalation rides `claim.parked`,
which already carries the packet, already requires the active fence,
and already lands in `blocked`.

A verb that **cannot** admit from `in_progress` opens none of that.
So:

| from | verb | carries |
| --- | --- | --- |
| `in_progress` | `claim.parked` | fence, packet, `escalation` |
| `ready`, `review` | `escalation.raised` | packet, `escalation` |
| `blocked` | `decision.recorded` | the cited position and the chosen id |

`packet.ExitVerbs` therefore stays exactly four and is pinned against
the table's `in_progress` outgoing set, which makes that existing
drill the enforcement of this rule rather than a description of it.

**The packet travels on both raise forms; the fence travels on only
one.** A raise from `ready` or `review` has no work to hand off, which
the packet schema already spells: `base` is the unambiguous
zero-length range `"<mb>..<mb>"`, `refs` and `findings` may be empty,
and `acceptance` is the contract's own anchor. And outside
`in_progress` there is no active fence — none is required and citing
one refuses, because a fence dies with its claim window.

## Any lane may raise; only a human answers

`escalation.raised` accepts `claim`, `dispatch`, `verdict`,
`supervise` and `operator` ([`actors.md`](actors.md)), because the
charter says any lane can raise `blocked(needs-you)`. Breadth is safe
because **raising grants nothing** — the `offer.published` argument. A
raised contract leaves `blocked` only through the operator's
`decision.recorded` or a citing cancellation, so a raiser can stop
work and hand a human the decision, never move it. A raiser cannot
answer its own question.

`decision.recorded` accepts **`operator` and nothing else**: the
fourth no-fallback row, after `verdict.rendered`, `check.sealed` and
`merge.overridden`. A `dispatch` fallback would let a machine lane
answer a human gate, which is the exact inversion of §I.3's "humans
hold gates, not queues".

**The curator raises too, since it gained its proposal grant.** Until
Phase 11 item 1 the curator held no ledger-writing grant at all, so
the charter's "any lane can raise" did not reach it, and this
paragraph recorded the tension for the moment the curator gained its
curation-proposal rights: granting an escalation row to a lane holding
no other write would have made freezing contracts its single write
authority. That moment came with `curate`
([`curation.md`](curation.md)): the raise row now accepts it, sixth
beside the five above, and breadth stays safe by the same
`offer.published` argument. The curator's residual is exactly every
raiser's: freezing, attributable, reversible (review finding on #200,
answered by plans/os-f30ee0d3.md D2).

The residual is recorded rather than hidden: a persuaded lane holding
any of those capabilities can **freeze** a contract. That is denial of
progress, not escalation of authority, it is attributable (the raise
carries the raiser's fingerprint and its question verbatim), and it is
`internal/admit/testdata/injection/residuals.json`'s entry for the
verb.

## Nothing else moves until it is answered

While a question stands:

- **`contract.unblocked` refuses.** `blocked` has exactly two
  machine-visible exits; this closes the one a machine holds, so the
  charter's "nothing else about the contract moves until it is
  answered" is structural rather than hoped for. The refusal names the
  standing position and both verbs that discharge it.
- **A second question refuses.** Asking a different question over the
  top of the first is exactly the thing being excluded.
- **`contract.cancelled` stays legal, and must cite the escalation.**
  Refusing it would trap the contract with no operator path out, which
  is a worse failure than the one being prevented — cancelling *is* a
  human decision. But an uncited cancel would let the subject reach a
  terminal state with the question neither cited nor answered: the
  obligation would simply disappear, taking the audit link with it. So
  the citation is what records that the decision taken was to cancel.

Answering returns the subject to `ready`, exactly as
`contract.returned` already does from `review`, with prior facts — the
submission and any standing verdict — persisting as history.

## Age is elapsed time, never a position difference

A standing question surfaces as an obligation
([`obligations.md`](obligations.md)):

| kind | owed by | discharged by |
| --- | --- | --- |
| `escalation.pending` | `lane:operator` | `contract.cancelled`, `decision.recorded` |

Its `since` is the **raise's** position, not the state's, because a
question carried by a `claim.parked` arrives with the exit that raised
it and a reader needs the position that asked.

The row also carries the raising event's `ts`, and that is
load-bearing. **Positions are ordinals: they order, they do not
measure.** An escalation untouched for hours has the same position
difference as one answered instantly after a burst of unrelated
traffic, so latency derived from `since` would be event count wearing
a clock's clothes.

The discipline is [`offers.md`](offers.md)'s, reused rather than
reinvented:

- **Admission never reads a wall clock.** Every comparison admission
  makes is between fields inside the event, which is why a born-dead
  offer refuses deterministically. Nothing about an escalation's age
  is therefore an admission concern.
- **A live read may.** Offer liveness takes `--now`, defaulting to the
  wall clock, because listing is a live read. Escalation age is the
  same kind of read.

So age is `now − ts` at the read's instant, and **resolution latency
is `answer.ts − raise.ts`** — both from the chain, computed by whoever
reports, stored nowhere.

Derivable is not the same as surfaced, and the answer closes that gap
itself: `seed decision record` reports `resolved_after_seconds` on the
act that resolves the question. It has to be there rather than in a
later read, because answering **clears** the standing fact — a reader
afterwards has no pair left to subtract without walking the chain. An
aggregate over many escalations is a reporting surface this card does
not build (`seed report` is deliberately absent, above); the metric
each answer needs is emitted where the fact is.

## Deliberately absent (v0)

- **No routing.** The supervisor routing of §7's dependency cascade is
  a later item; this card delivers the channel, not its delivery.
- **No `seed report` verb.** `situation` already renders the
  obligations projection, and a second copy of one derivation is the
  failure `obligations.md` exists to prevent.
- **No escalation from `in_progress` except through the park**, and no
  fifth exit: see above.
- **No "other — say what" option.** It re-admits the open question
  through the door built to exclude it, and every escalation would
  grow one. A lane that cannot state two candidate answers has not
  reduced its problem far enough to spend a human's attention on it;
  the correct routing for open-ended work is an intent.
