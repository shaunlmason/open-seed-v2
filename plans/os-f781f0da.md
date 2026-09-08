# Plan: next — escalation with packet, question and decision (os-f781f0da)

Phase 9 item 2 of `docs/next-build-plan.md`: *"Escalation
(`blocked(needs-you)`) with packet + question + decision; report
surfaces age. An escalation raised in answer to a refusal carries that
refusal's `code` and `message` in its packet."*

The charter is unusually specific here, and most of the design work is
reading it rather than inventing:

- §II.7 — "Escalation is `blocked(needs-you)`: an event addressed to a
  human gate carrying the packet, the question, and the minimal
  decision — never a transcript."
- §II.11 — "Any lane can raise `blocked(needs-you)`: packet + question
  + minimal decision; the supervisor routes it, the report surfaces it
  with age, and **nothing else about the contract moves until it is
  answered**. A human's unit of interruption is one decision, never a
  transcript."
- §I.3 — "Humans hold gates, not queues: intent, high-consequence
  plans, the protected surface, and escalations."
- III — "Escalations carry packet + question + minimal decision;
  waiting escalations surface with age; resolution latency is
  tracked"; "Every escalation is one packet + one question + one
  decision; transcript-dumping is a defect."

`next/spec/packets.md` already points forward to this card: *"Escalation
packets (`blocked(needs-you)` carrying packet + question + minimal
decision) reuse this schema when the routing/escalation phase lands — a
forward pointer, not a gap."* This plan pays that pointer.

## Design decisions (binding for this task)

- **D1 — from `in_progress` the escalation rides `claim.parked`;
  everywhere else it is its own verb, `escalation.raised`.** The
  distinction that matters is not "new verb versus qualifier" — it is
  **which state the verb can leave**.

  `next/spec/lifecycle.md` pins the four deliberate exits from
  `in_progress` by self-validation, and III.F ("every exit from
  `in_progress` is deliberate … silent abandonment is impossible by
  construction") depends on that set being **closed**. So nothing new
  may leave `in_progress`, and there the escalation rides the park,
  which already carries the four-part packet, already requires the
  active fence, and already lands in `blocked`.

  A verb that **cannot** admit from `in_progress` opens none of that.
  `escalation.raised` is therefore added with
  `{"from": ["ready", "review"], "to": "blocked"}` — the four exits stay
  exactly four, and the charter's "**any** lane can raise
  `blocked(needs-you)`" (§II.11) is satisfied rather than deferred.

  This corrects the plan's first draft, which shipped the verifier as a
  known gap: a verifier holding a `review` subject could not raise one
  at all, because no `review` → `blocked` row existed (review finding on
  #197). Shipping a gap against a normative charter line was the wrong
  trade, and the workaround available to it was worse — rendering a
  **fail** verdict to reach `contract.returned` would launder an
  environmental problem into a judgement about the submission, which is
  precisely the laundering Phase 6 spent a card preventing.

  Capability: `claim`, `dispatch`, `verdict`, `supervise`, `operator`.
  Raising a question **grants nothing**, which is the `offer.published`
  argument: the contract can leave `blocked` only through the operator's
  `decision.recorded` or a citing cancellation (D3), so a raiser cannot
  move work, only stop it and hand a human the decision. The residual is
  that any of those lanes can freeze a contract; it is bounded by being
  attributable, by the operator's answer, and by there being no way to
  unfreeze one's own escalation.

  Answering from `review` returns the subject to `ready`, exactly as
  `contract.returned` already does, with prior facts — the submission
  and any standing verdict — persisting as history.

- **D2 — the ANSWER is its own operator-only verb: `decision.recorded`
  (`blocked` → `ready`).** It cannot ride `contract.unblocked`, which
  is `{dispatch, operator}` (`internal/keyring`): a machine lane
  answering a human gate is the exact inversion of §I.3, and a
  capability row is where that is said. So `decision.recorded` takes
  **operator and nothing else** — the FOURTH no-fallback row, after
  `verdict.rendered`, `check.sealed` and `merge.overridden`, and for
  the reason the third has one: the charter names the act attributable
  human judgment, and a fallback would quietly make it something
  else.

- **D3 — while an escalation stands unanswered, `contract.unblocked`
  REFUSES.** That is "nothing else about the contract moves until it is
  answered", made structural. `blocked` has exactly two machine-visible
  exits; this closes the one a machine holds. The refusal follows
  `next/spec/refusals.md` discipline: it names the standing
  escalation's position and the verb that discharges it, so the caller
  is told what IS legal.

  `contract.cancelled` stays legal, deliberately: it is already
  operator-only, and cancelling an escalated contract IS a human
  decision — refusing it would trap the contract with no operator path
  out, which is a worse failure than the one being prevented.

  But it must **cite the escalation it is answering**. The first draft
  left the cancel unconstrained, which let a subject reach a terminal
  state with the standing escalation neither cited nor answered: the
  obligation simply disappeared, taking the audit link with it, and
  "nothing else moves until it is answered" was satisfied only by
  accident (review finding on #197). So while an escalation stands,
  `contract.cancelled` admits only carrying
  `{"escalation": "<position>"}`, and that citation is what records that
  the decision taken was to cancel. The contract is never trapped, the
  obligation is discharged rather than dropped, and the chain still
  shows which question the cancellation answered.

  The lockout is derived from the fold at admission time, following the
  red-verdict lockout precedent (`plans/os-d2497eb7.md`,
  `internal/admit/lockout_test.go`), never from a stored flag.

- **D4 — the question and the decision are SIBLING payload fields, never
  a fifth packet part.** `packets.md` binds "exactly four parts, all
  keys present" and refuses unknown keys at the top level. So the raise
  carries the packet unchanged beside a new `escalation` object, and
  the answer carries its own fields.

  **The packet travels on both raise forms; the fence travels on only
  one.** Caught auditing this plan against its own D1 amendment rather
  than in review, and worth stating because the two halves come from
  different rules:

  - The **packet** is required on every raise. The charter's §II.7 says
    an escalation carries "the packet, the question, and the minimal
    decision", and `packets.md` already anticipates this exact reuse
    ("escalation packets … reuse this schema"). A raise from `ready` or
    `review` has no work to hand off, which the schema already has a
    spelling for: `base` is the unambiguous zero-length range
    `"<mb>..<mb>"`, `refs` and `findings` may be empty, and `acceptance`
    is the contract's own anchor.
  - The **fence** rides only the `claim.parked` form.
    `next/spec/lifecycle.md` is explicit that outside `in_progress`
    there is no active fence, none is required, and **citing one
    refuses** — a fence dies with its claim window. So an
    `escalation.raised` carrying a fence must refuse, and that is the
    landed rule holding rather than a new one.

  From `in_progress`, on `claim.parked`:

  ```json
  {"fence": "12", "packet": {…four parts…},
   "escalation": {"question": "<one sentence>",
                  "options": [{"id": "a", "choice": "<one sentence>"},
                              {"id": "b", "choice": "<one sentence>"}]}}
  ```

  From `ready` or `review`, on `escalation.raised` — same packet, same
  `escalation` object, no fence:

  ```json
  {"packet": {…four parts, base "<mb>..<mb>"…},
   "escalation": {"question": "…", "options": [{"id": "a", "choice": "…"},
                                               {"id": "b", "choice": "…"}]}}
  ```

  And the answer:

  ```json
  {"escalation": "<position>", "choice": "a", "because": "<one sentence>"}
  ```

- **D5 — `options` is REQUIRED, with at least two entries, and the
  answer must cite one of them by id.** This is the decision most worth
  arguing, because it is the one that makes "one decision, never a
  transcript" checkable rather than aspirational.

  A free-text question with no answer set is an invitation to design
  work, which is what §II.11's "minimal decision" excludes. Requiring a
  closed set forces the reducing work onto the lane, where it belongs:
  a lane that cannot state two candidate answers has not reduced its
  problem far enough to spend a human's attention on it.

  Cost, stated: a genuinely open question ("what should this be
  called?") cannot be escalated as such. The lane must either propose
  candidates or file an intent, which is the correct routing for
  open-ended work anyway. An "other — say what" escape hatch is
  **deliberately not offered**: it re-admits the open question through
  the door built to exclude it, and every escalation would grow one.

  Free text (`question`, `choice`, `because`) falls under the
  classification lint's aggregate free-text budget, exactly as packet
  prose does. Transcript-dumping is then a refusal rather than a
  style note.

- **D6 — waiting escalations surface as an OBLIGATION, and age is
  ELAPSED TIME, never a position difference.** The new fact-shaped kind:

  | kind | owed by | discharged by |
  | --- | --- | --- |
  | `escalation.pending` | `lane:operator` | `decision.recorded`, `contract.cancelled` |

  The plan's first draft said latency was "the answer's position minus
  the raise's". That is wrong, and wrong in both directions: positions
  are **ordinals**, so an escalation sitting untouched for hours reads
  as zero, and a burst of unrelated traffic makes an instant answer look
  old (review finding on #197). Positions order; they do not measure.

  Age comes from the raising event's own `ts`, and the discipline is
  already set by [`offers.md`](../next/spec/offers.md), reused rather
  than reinvented:

  - **Admission never reads a wall clock.** Every comparison admission
    makes is between fields inside the event, which is why a born-dead
    offer refuses deterministically. Nothing about the escalation's age
    is therefore an admission concern at all.
  - **A live read may.** Offer liveness takes `--now`, defaulting to the
    wall clock, precisely because "listing is a live read, unlike
    admission". Escalation age is the same kind of read.

  So the row carries the raise's `ts` beside its position; age is
  `now − ts` at the read's `--now`; and **resolution latency is
  `answer.ts − raise.ts`**, both from the chain, computed by whoever
  reports and stored nowhere. Positions stay for identity and for the
  `--since` delta, which is what they are for.

  Not chosen: a `seed report` verb. Nothing in the tree has one; the
  build plan's "report surfaces age" names an outcome, not a surface;
  and rendering a projection `situation` already renders would be a
  second copy of one derivation, which is the failure mode
  `obligations.md` exists to prevent.

- **D7 — one new loop verb (`decision record`) and two new flags on
  `claim park`.** The raise is `claim park --question <s> --option
  <id>=<s>` (repeatable); the answer is `seed decision record --subject
  <c> --choice <id> [--because <s>]`, deriving the escalation citation
  from the single standing escalation in the fold and **refusing on
  ambiguity or absence** rather than picking — the derivation discipline
  `next/spec/loop-verbs.md` already binds. `contract.blocked` keeps no
  loop verb: it is a dispatch act, and `loop-verbs.md` deliberately
  leaves filing and specification verbs on the raw seam.

- **D8 — the refusal an escalation answers is carried VERBATIM, and the
  existing rule is reused rather than copied.** `internal/loop` already
  puts a refusal's `code` and `message` unchanged into the park
  packet's `findings`. The escalation adds the question beside that
  packet; it does not restate, summarize or re-render the refusal. A
  second copy of the verbatim rule would be a second thing to drift.

- **D9 — scope guard.** No supervisor routing (§7's dependency cascade
  is a later item's work). No policy in `internal/loop` about WHEN to
  escalate: the loop gains the ability, the caller keeps the judgment,
  and item 4's fixtures supply the policy. No new exit codes, no new
  projection, no change to the four-part packet.

## Steps

1. `next/spec/transitions.json` + `next/spec/lifecycle.md` — two rows,
   `escalation.raised` (`["ready", "review"]` → `blocked`) and
   `decision.recorded` (`blocked` → `ready`), and the quotation a drill
   pins to the table. The four exits from `in_progress` are unchanged,
   and a drill asserts that neither new verb admits from it.
2. `next/internal/transition` — the embedded table moves with it.
3. `next/internal/keyring` + `next/spec/actors.md` — two capability
   rows: `escalation.raised` across the lanes that can hold standing on
   a subject, and `decision.recorded` operator-only, its no-fallback
   reason recorded in the form the three existing such rows use.
4. `next/internal/admit` — the escalation shape rule (question,
   options, the answer's citation), the standing-escalation lockout on
   `contract.unblocked`, and the citation `contract.cancelled` must
   carry while an escalation stands.
5. `next/internal/obligation` — the `escalation.pending` kind carrying
   the raise's `ts` beside its position; and `next/spec/obligations.md`'s
   fact-shaped table gains its row.
6. `next/cmd/seed` — `claim park --question/--option`, `escalation
   raise` for the non-claim states, `decision record` with its
   derivation, `situation`'s age at `--now`, and the affordance catalog
   entries.
7. `next/spec/escalation.md` — the new spec, and the forward pointer in
   `packets.md` becomes a link.
8. `next/docs/decisions.md`, `memory/LEARNINGS.md`; receipt; evidence;
   review.

## File Scope

- `next/spec/escalation.md` (new), `next/spec/transitions.json`,
  `next/spec/lifecycle.md`, `next/spec/obligations.md`,
  `next/spec/actors.md`, `next/spec/packets.md`
- `next/internal/{transition,keyring,admit,obligation}/**`
- `next/cmd/seed/**`
- `next/docs/decisions.md`, `memory/*`
- `receipts/os-f781f0da.json`

Nothing outside `next/**` except the work-product files above.

## Acceptance Criteria

1. A `claim.parked` carrying `escalation` admits, and the folded
   subject is distinguishable from an ordinarily-blocked one: the
   obligations projection emits `escalation.pending` for the first and
   not the second. A drill asserts both directions, so the kind cannot
   pass by being emitted always.
2. `escalation` with fewer than two options, with a missing `question`,
   or with a duplicate option id **refuses at admission**, naming the
   offending part. A drill plants each.
3. While an escalation stands, `contract.unblocked` refuses and the
   refusal names both the escalation's position and
   `decision.recorded`. `contract.cancelled` still admits **when it
   cites the escalation** and refuses when it does not — both asserted,
   because a lockout that traps the contract and a cancel that drops the
   obligation are the two failures this criterion exists to exclude.
4. `decision.recorded` admits from an operator key and **refuses from a
   dispatch key** citing capability, which is the whole of D2. A drill
   uses a real dispatch-capability actor, not an unenrolled one, so the
   refusal is attributable to the row rather than to standing.
5. `decision.recorded` citing a position that is not the standing
   escalation, or a `choice` id the escalation does not offer, refuses.
6. `seed decision record` derives the citation from the fold; with two
   standing escalations it refuses naming both candidates rather than
   choosing, and with none it refuses naming what would establish one.
7. An operator's `seed situation` lists the waiting escalation with the
   position that raised it **and its age at `--now`**, and the delta
   form (`--since`) reports its removal by `(subject, kind)` once
   answered.
8. **Age is elapsed time, drilled against both ledgers that break a
   position difference.** On an IDLE ledger — no events after the raise
   — age grows with `--now` and is non-zero. On a BUSY one — many
   unrelated events between raise and answer — the reported latency is
   unchanged by that traffic. A drill asserts each, so a position
   subtraction reintroduced later fails.
9. A verifier-capability key raises an escalation on a `review` subject
   and it admits; the same key's `escalation.raised` on an `in_progress`
   subject **refuses**, because nothing new may leave `in_progress`.
   Both asserted, so D1's distinction is enforced rather than described.
   An `escalation.raised` **carrying a fence refuses** (the landed
   outside-`in_progress` rule), and one carrying no packet refuses,
   with the zero-length `base` range accepted — the three together pin
   D4's split.
10. **Mutation evidence, per fix rather than in aggregate.** Each of
   these must fail its drill: deleting the lockout branch; relaxing the
   two-option minimum; widening `decision.recorded` to accept
   `CapDispatch`; dropping the `escalation.pending` row from the
   obligations kinds; dropping the citation requirement from
   `contract.cancelled`; adding `in_progress` to `escalation.raised`'s
   `from` set.
11. `make check` green with coverage measured **cold**, at least three
   readings above the 90% gate, and the suites pass unprivileged.

## Validation Commands

```sh
cd next && gofmt -l . && go vet ./... && go build ./...
cd next && go test ./internal/... -count=1
cd next && go test ./cmd/seed/ -count=1
make check
```

## Expected diff shape

Two table rows, two capability rows, one admission rule, one obligations
kind, three CLI surfaces, one new spec file and four amended ones, plus
drills. Roughly +850/-40 lines, all under `next/**` and the work-product
files.

## A risk worth naming now

D5 (options required, minimum two) is the decision most likely to be
wrong, and it is wrong in a way that is cheap to discover and expensive
to undo: if real escalations turn out to be open questions, the rule
converts a usable channel into an unusable one, and by then lanes will
have been written against it.

So the drill for D5 asserts the refusal, and this plan records the
alternative it rejected (an "other" escape hatch) with the reason,
rather than leaving a later reader to guess whether the constraint was
considered or merely inherited. Relaxing it later is a one-row change
to a shape rule; tightening it later is a break for every lane that
learned to send free text.

**A second risk, from this plan's own review.** Its first draft got
three things wrong in the same direction — it deferred the verifier's
route, left the cancel uncited, and measured age by subtracting
ordinals — and each was a case of taking the cheaper reading of a
normative line rather than the one the line actually requires. The
amendments above close all three. The general lesson is recorded in
`next/docs/decisions.md` with the fixes, because the pattern (a plan
that satisfies a charter sentence *by accident* rather than by
construction) is the one worth catching next time, not the three
instances.
