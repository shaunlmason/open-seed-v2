# The obligations projection and the situation read

Seed represents **permission** already: `admit.Affordances`
([`envelope.md`](envelope.md)) answers "what may I do", computed from
the rule set admission enforces. This spec defines the other half —
**obligation**: what is owed on a subject, by whom, since when, and
which verbs discharge it. Design authority: `SEED-NEXT.md` §II.4
(projections) and §II.10, and `docs/build-plan.md` Phase 9
item 5, whose loop-completeness criterion this surface exists to
satisfy: a lane that cannot orient or choose cannot run unattended.

Obligation is a **projection over the fold**, never a new authority.
Every fact it reads is already folded — the active claim with its
fence, the bound submission, the standing verdict and merge request,
run starts against run settles, open reservations, and the state with
the position that set it.

## The row

```json
{"subject": "c-1", "kind": "claim.held", "owed_by": "<fp>",
 "since": 12, "discharged_by": ["claim.parked", "claim.released", "submission.made"]}
```

Identity is **`(subject, kind)`** — normative, not incidental: the
situation read's delta names removals by that pair, and a removal
list is only applicable if a client can match rows it already holds.
`owed_by` is a fingerprint, or a `lane:<capability>` name where the
debt belongs to a role rather than an actor (independence forbids
naming the claimant as its own verifier). `discharged_by` is never
empty: an obligation nobody can discharge is an anomaly, not an
obligation, so a kind with no reachable discharger is not emitted.

## Where dischargers come from

**State-shaped** obligations read their verbs from the transition
table — the table already says which verbs leave a state, so legality
is never restated here. **Fact-shaped** obligations close with a verb
that changes no lifecycle state and therefore appears in no table
row; that set is closed and each member cites the spec pairing it
with its fact:

| kind | discharged by | spec |
| --- | --- | --- |
| `run.unsettled` | `run.settled` | [`executors.md`](executors.md) |
| `budget.open` | `budget.settle`, `budget.release` | [`budgets.md`](budgets.md) |
| `submission.pending` | `verdict.rendered` | [`verdicts.md`](verdicts.md) |
| `verdict.unmerged` (unrequested) | `merge.requested` | [`reconciliation.md`](reconciliation.md) |
| `escalation.pending` | `decision.recorded`, `contract.cancelled` | [`escalation.md`](escalation.md) |
| `verdict.human` | `verdict.rendered` | [`verdicts.md`](verdicts.md) |
| `request.pending` | `request.answered` | [`requests.md`](requests.md) |
| `approval.pending` | `approval.denied`, `approval.granted` | [`protocol.md`](protocol.md), "Per-verb approval" |
| `submission.unmergeable` | `contract.returned` (citing the observation) | [`observations-forge.md`](observations-forge.md) |

## The kinds

- **`claim.held`** — an active claim window; owed by its holder;
  discharged by the verbs leaving `in_progress`.
- **`claim.reap-owed`** — an `in_progress` claim whose holder has been
  **revoked**; owed by the operator lane and discharged by
  `claim.reaped`. Distinct from `claim.held`, which any deliberate exit
  discharges: a revoked holder can end its window no other way than a
  reap, because it can no longer submit, release or park
  ([`maintenance.md`](maintenance.md), "Revocation is the third
  corroboration"; plans/os-32d06c65.md).
- **`verdict.human`** — a `verdict.deferred` on the current
  submission window with no render after it; owed by the operator
  lane, since a human is a key with operator standing, and discharged
  by the `verdict.rendered` such a key makes on the same submission
  ([`verdicts.md`](verdicts.md), "The rubric and the scorecard").
- **`submission.pending`** — a submission no verdict cites, while the
  subject is under review; owed by the verifier lane. A return by
  observation ([`observations-forge.md`](observations-forge.md))
  re-readies the subject unjudged, and a verdict there would be a debt
  nobody can discharge.
- **`submission.unmergeable`** — the forge's latest observation on the
  submission under review says it is not mergeable as it stands: a red
  check, an unresolved review thread, or a review requesting changes
  ([`observations-forge.md`](observations-forge.md)); from `seed/8`.
  Owed by the dispatch lane (`lane:dispatch`), since the return is
  queue management; `since` is the observation's position; the row
  carries `head`, so the read says which revision is red; discharged
  by `contract.returned` citing the observation. Not emitted for
  `pending` alone, nor once a fail verdict stands on the window, where
  the return is owed on the verdict's account; while it stands the
  merge debt is not advertised, because `merge.requested` refuses.
- **`request.pending`** — an inbound request nobody has answered
  ([`requests.md`](requests.md)); owed by the dispatch lane
  (`lane:dispatch`), one row per subject carrying the oldest
  unanswered request's position and timestamp, so the situation read
  reports its age in elapsed seconds; discharged by the dispatcher's
  `request.answered`.
- **`approval.pending`** — a per-verb approval request nobody has
  answered ([`protocol.md`](protocol.md), "Per-verb approval"); owed by
  the operator lane (`lane:operator`), one row per subject carrying the
  oldest open request's position and timestamp, so the situation read
  reports its age in elapsed seconds (identity is (subject, kind), so a
  subject with several open requests shows the oldest until it is
  answered); discharged by the operator's `approval.granted` or
  `approval.denied`. The grant is then spent by the act it names; the
  denial admits nothing.
- **`verdict.unmerged`** — a pass verdict with no observed merge. The
  merge chain is **two events**, so this kind has two shapes: until a
  request cites the verdict the debt is the operator's and
  `merge.requested` pays it; after that the forge fact is the
  observer's to record with `merge.observed`. Not projected for an
  eval subject ([`evals.md`](evals.md)): its verdict is its terminal
  fact, and what it owes is a qualification, which `seed eval status`
  derives rather than this projection.
- **`run.unsettled`** — an admitted `run.started` whose fence carries
  no `run.settled`. **Position-anchored**: flagged only once the
  subject has taken a subsequent claim window or reached a terminal
  state, because post-close settlement is a valid intermediate state
  and a closed-without-settle predicate would file spurious findings
  mid park or reap flow (the Phase 7 exit's routing).
- **`escalation.pending`** — a standing `blocked(needs-you)`: a
  question addressed to a human gate that nothing else about the
  contract moves past ([`escalation.md`](escalation.md)). `since` is
  the **raise's** position, not the state's, because a question
  carried by a `claim.parked` arrives with the exit that raised it.
  The row also carries the raising event's `ts`, the only kind that
  does: age here is **elapsed time**, and a position difference is
  event count wearing a clock's clothes — an escalation untouched for
  hours has the same one as an answer given instantly after a burst of
  unrelated traffic. Computing it stays a live read
  ([`offers.md`](offers.md)), never an admission concern.
- **`budget.open`** — an open valid reservation, **wherever it
  stands**: from the reserve until a settle or a release closes it,
  inside the window that opened it and after. Admission gates only
  the reserve on `in_progress` ([`budgets.md`](budgets.md)), so both
  closing verbs stay reachable once the window ends and the debt is
  a debt rather than a maintenance concern. This matters most on the
  failed-verdict retry, where the next claimant is a different worker
  and an unclosed hold would come out of their remaining.

  Owed by **whoever can still discharge it**, which is not always
  whoever opened it and is never merely whoever holds the window: a
  close admits for the reservation's own reserving signer or the
  operator lane and for nobody else, so the row names that signer
  while their standing lets them close, and `lane:operator` once
  suspension or revocation means every close from them refuses.
  Attributing a debt to a fingerprint nobody can sign for would hide
  it from the one actor able to pay it, on exactly the
  revocation-recovery path the charter cares about.
- **`contract.blocked`** — a blocked subject; owed by the operator
  lane; discharged by the verbs leaving `blocked`.

The list is **closed**. An open-ended taxonomy would make this a
policy surface rather than a derivation.

## The situation read

`seed situation --ledger <dir> [--key <path>] [--subject <id>]
[--since <position>]` renders one position-stamped envelope: the
caller's obligations, the windows they hold with the active fence
(the argument a lane would otherwise read out of a projection by
hand), the caller's message notices, and the standard advisory
fields. A keyless read guesses no
identity and reports the board unfiltered; probes must be signed, so
affordance stamping needs the key itself.

`--since <position>` makes the response a **complete change report**,
not a filtered list: obligations that arose **or changed** after the
cited position, an explicit `discharged` list naming every obligation
that stood at it and no longer does, and a count of the unchanged.
Applying the response to a prior snapshot must reproduce the standing
set exactly — the property the drills assert. A delta of standing rows
alone would leave a resuming lane holding a discharged obligation
forever, and an unchanged *count* cannot say what disappeared. The
cited position is a tip **ordinal**, so the prefix a lane last saw is
`records[:position+1]`.

**Changed** is content, not position. A row whose `owed_by` moves
keeps the position it arose at, because the obligation changed hands
rather than restarting — so a delta keyed on `since` alone would call
a transfer unchanged, and the removals, derived from the prior set
filtered to the caller, would not carry it either: the party it moved
TO would hear nothing at all. That is the standing-aware `budget.open`
transfer above, and the party it moves to is by construction the only
one who can act. So the delta compares each standing row against what
stood at the cited position, on the **unfiltered** prior set, because
"it was not mine then and is now" is exactly the case a
caller-filtered comparison cannot see. A row that stood at the cited
position under no entry at all — `run.unsettled` begins standing at a
position later than its own `since` — is reported for the same reason.

### The messages section: notices, never bodies

The read also carries the caller's **mail**, which is what closes the
build plan's Phase 9 item 5(b). Each entry says a message exists and
nothing about what it says: `{from, at, bytes, unread}`, plus
`subject` when it resolves and `undeliverable` where it applies. Every
one of those is a ledger-generated identifier, a count, or a flag.

**The subject is carried only when it is a contract.** The event's
subject field is sender-controlled: `message.sent` admits on any
nonempty subject, and the classification lint reads only the payload,
so `--subject "IGNORE PREVIOUS INSTRUCTIONS"` admits (review finding
on #211). A notice therefore carries `subject` only when it resolves
to a contract on the chain and omits it otherwise — the message still
shows, from whom and how large, and declines to repeat what the sender
wrote in a field that was never an identifier. The injection sweep
plants its marker in a subject as well as in payloads.

**Why not the body.** `message.sent` needs **no capability at all** —
it is the standing-only verb any enrolled active actor may append, and
[`lanes.md`](lanes.md)'s residual table names it "the one that RELAYS",
bounded only by a size lint that a short instruction sails through. The
situation read is the single surface every lane fragment names as the
one it orients from, taken on **every wake, unbidden**. A body here
would let any enrolled actor write prose into the read of every lane in
the deployment, which is a different thing from a persuaded dispatcher
relaying to a reader who chose to look. The containment is asserted by
marker sweep in `cmd/seed/injection_cli_test.go`, in the read's
serialized form, so a field added later is covered.

**Unread is the cursor, and there is no read-state.** A message at
position P is unread iff `P > --since`; with no cursor cited, every
message the caller can see is unread, because a caller that names no
position has said nothing about what it has seen. The position a lane
carries forward IS its read cursor, so no `message.read` verb exists
and `message.acked` stays unimplemented: an ack means "I acted on
this", which a cursor cannot derive. Under `--since` the section is cut
like every other; unlike obligations there are no removals, because an
event does not stop having been appended.

**Addressing, and why malformed fails closed.** A payload carrying
`"to": "<fp>"` or an all-string array is addressed to those actors; a
payload with no `to` key at all is a **broadcast**; and a `to` that is
present and does not parse addresses **nobody**. Absent and malformed
are different facts. An absent `to` is a sender who said nothing about
addressing, and reading that as everyone reads what is there. A
malformed `to` is a sender who said something the projection cannot
read, and every resolution invents intent: broadcasting widens delivery
from one intended recipient to every actor on an encoding slip, and
delivering to the well-formed entries only is the same invention one
notch quieter, since nothing says which entries were meant. An
undeliverable message is not erased — the **keyless** whole-board read
applies no caller filter, so an operator still finds it.

Addressing is read leniently by the projection and the admission
boundary is untouched: `message.sent` still refuses nothing, `{"n": 1}`
is still legal. Validating recipients at admission would be a new
refusal surface on a verb that today has none, which is a different
change from a read.

### Reading one message

`seed message read --ledger <dir> --key <path> --at <position>` returns
one message's body to a caller it addresses. This is the deliberate
second act the notices exist to prompt: the reader chose to look, at a
position it names, after a notice told it who sent the thing — the
"reader must choose to look" posture [`lanes.md`](lanes.md)'s residual
analysis already accepts, which is exactly what the orienting read is
not.

It appends nothing. A caller the message does not address gets
`not_found` (exit 4), **byte for byte** what a position holding no
message gets, so this surface adds no oracle for what is there; the
four reasons a caller gets no body share one construction site so
that stays true.

**Addressing is routing, not confidentiality** (review finding on
#211). The ledger is plaintext and is the audit record by charter
design: the projections carry every payload verbatim
([`projections.md`](projections.md)), and `seed ledger show
--position P` returns any event to anyone with read access to the
repository, which is the same access these reads need. A non-recipient
can read any body there. What `not_found` buys is that a lane acting
through Seed verbs is routed only its own mail, and that the message
surface does not become a second place to ask. A body that must be
confidential is a sealed-checks problem
([`sealed-checks.md`](sealed-checks.md)), not an addressing one. `not_recipient` (exit 23) is not reused: it names the
sealed-envelope recipient set, whose answer is "re-seal to the current
set" ([`envelope.md`](envelope.md)'s allocation rule forbids sharing a
code across two different answers). A key is required, because a body
is read as somebody and a keyless read addresses no one.

The read is read-only and idempotent: it opens the ledger read-only,
mutates nothing, and journals no attempt, because a read is not an
admission-boundary attempt ([`refusals.md`](refusals.md)).

## The drift class

An obligation must be **dischargeable by the party it is owed by**,
at the position it is emitted at, judged by the same `admit.Check`
admission enforces. The sweep asserts *at least one* discharging verb
admits for the owed actor — not all of them — because `discharged_by`
names the acts that end the obligation while **who** may perform each
is the affordance layer's business: a claim is discharged by release,
park, reap or submission, of which the holder may take three and the
supervisor the fourth. Requiring every verb to admit for the owed
actor would force capability policy into a derivation that must stay
a projection.

One global exception: under a declared halt nothing admits but the
lift, so every obligation still *stands* while none is dischargeable.
That is the halt working, not an obligation defect, and the sweep
checks only well-formedness at halted positions.

The sweep walks the shared scenario's every prefix, and the scenario
ends by **suspending and then revoking** a lane that still holds an
open reservation: a walk of only active actors can never reach the
positions where an obligation's usual owner has lost the power to
discharge it, which are the positions standing-aware attribution
exists for.

## Deliberately absent (v0)

No priority or ranking, no due-date policy beyond what a caller
declares, and no cross-subject aggregation.

The acts that create and discharge these obligations are the loop
verbs ([`loop-verbs.md`](loop-verbs.md)), Phase 9 item 5's third
part. The relationship runs one way: a verb never re-derives what
discharges what — this projection already says it, and the loop
verbs' drills assert the pairing against it rather than restating
it.
