# The loop verbs

`seed situation` ([`obligations.md`](obligations.md)) answers *what is
true for me now*. This spec defines the other half of the lane's
surface: the **acts** it takes. Design authority: `SEED-NEXT.md` §II.9
and §II.10, and `docs/build-plan.md` Phase 9 item 5, whose
loop-completeness criterion this surface exists to satisfy — a lane
that cannot act without hand-assembling protocol arguments is not
running unattended, it is being driven.

The gap these verbs close is not verbosity. `seed ledger append` is
the raw seam and stays exactly as it is: it signs at the tip and
appends, **consulting the admission boundary not at all**. A lane
acting through it learns that its act was illegal from a chain-level
refusal, after signing, instead of from the boundary that would have
explained it. Two principles follow, and they are the whole design.

## Derive every argument the system already holds

An argument the system can compute is never asked for, because a
value the boundary would refuse is not a choice the caller is being
offered — it is an invitation to be wrong.

| Derived | From | Because |
| --- | --- | --- |
| the fence citation | the active claim window in the fold | a holder-signed event citing anything else is refused anyway |
| the reservation a close cites | the single open valid reservation in the shared budget view | the view is the one admission judges against |
| the plan anchor a submission cites | the approved `plan.approved` on the subject | an approval admits ONE exact revision, so no other value could be legal |
| the resume range | the repository at `--repo`: `HEAD`, and the merge-base against `origin/HEAD` | git already holds both facts |

What stays caller-supplied is what is a **judgment**, not a lookup:
`--amount`, `--actuals`, and the packet's own prose.

On the remote path a derived value is **re-examined against every
refreshed tip** inside the optimistic loop, and the act is refused if
the derivation no longer yields what was drafted. It is never
silently replaced: a value derived from a view that has since moved
is not a better argument, it is a **different decision**. A second
reservation appearing makes the act ambiguous, and refusing to choose
is the whole point of deriving it; a window reaped and re-taken is an
authorization the lane never gave.

Where a derivation cannot be made, the shape of the failure decides
who explains it:

- **A missing fact** refuses here, naming what would establish it
  ("no open valid reservation stands on c-1 — `seed budget reserve
  --amount <n>` establishes it").
- **An ambiguity** refuses here too, naming the candidates and
  declining to pick: two open reservations are a spend decision the
  lane owns, and a silent choice would make it for them.
- **An absent window** is not a derivation failure at all: the key is
  simply omitted, and the boundary decides on its own account of the
  state, which is better than anything a derivation could say. It
  does not always refuse: a budget close outside a window is legal
  and cites no fence ([`budgets.md`](budgets.md)), which is precisely
  why omitting the key beats inventing one.

## Refuse before signing, and say what IS legal

Every verb drafts its record, runs the **same `admit.Check`**
admission enforces, and on refusal renders the boundary's own typed
error **beside the caller's current affordances on that subject**,
computed from the same view the refusal was computed at. Nothing is
appended and nothing enters the chain.

**"Before signing" is about the chain, not about `event.Sign`.** The
boundary cannot judge an unsigned record at all: the actor rule
verifies the signature, so signing is how a caller ASKS the boundary
a question. `admit.Affordances` signs one probe per catalog verb for
exactly that reason, and every verb here signs its draft before
checking it. What the phrase forbids is a record reaching the chain
before the rule set has judged it. A signature over a record that
never leaves memory costs nothing, leaves no trace, and is the only
way to get an answer.

On the remote path the same holds across retries: the optimistic loop
re-links, re-signs and re-judges per attempt, and pushes only after
the judgement passes, so a draft the boundary refuses never reaches
the remote however many times it was signed along the way.

This is Phase 8's principle — one rule set, enforcement and
advertisement — carried from legality to construction. It is also why
these verbs are not sugar: the raw seam structurally cannot do it.

## The surface

`<noun> <verb>`, matching `offer publish`, `verdict render`, `seal
create`, `budget status`. Seven acts, one spelling each, no aliases,
chosen because together they close poll → claim → work → meter →
submit → deliberate exit:

| Act | Verb | Derives | Takes |
| --- | --- | --- | --- |
| `claim take` | `claim.taken` | — | `--now <RFC3339>` optional (default: the wall clock; admission reads no clock): the instant the lessons' expiry is read at, an expired or retired lesson never surfacing ([`curation.md`](curation.md)); `--repo <dir>` optional: the surfacing lessons are verified against it and returned as `lessons` (`[{lesson, hypothesis, applies_when, carrier, digest}]`, present on every claim, empty when nothing matches), the facts that do not resolve as `lessons_unresolved`; without it `lessons` is empty and `lessons_unverified` counts what a repository would have verified ([`curation.md`](curation.md)) |
| `claim release` | `claim.released` | fence | `--packet` |
| `claim park` | `claim.parked` | fence | `--packet`, `--question`/`--option` |
| `submission make` | `submission.made` | fence, plan anchor | `--packet` |
| `budget reserve` | `budget.reserve` | fence | `--amount` |
| `budget settle` | `budget.settle` | fence, reservation | `--actuals` |
| `budget release` | `budget.release` | fence, reservation | — |

Each also takes an optional **`--as <fingerprint>`**: the identity the
caller declares it is acting as, compared against the key **at the
signing site**. `internal/loop` fingerprints the key file before every
act, and the CLI reopens that path independently, so a replacement
between those two reads is observed by only one of them; comparing
where the signature is taken closes it, because check and signature
then see the same bytes from the same read. Optional, because these
verbs are also reachable by hand and an operator acting once has no
loop to race with; a fingerprint is public (it is the `actor` field of
every record) so carrying it costs no confidentiality.

**The last-ditch exit carries none**, and that is the same exemption
`lastDitch` already names rather than a caveat on it: `strand` attempts
the exit precisely so it reaches the admission boundary, where the
fence rule gives the authoritative refusal. Passing the cached actor
there would reinstate the identity gate one layer lower and stop the
exit at the seam instead.

Each takes `--ledger` **xor** `--remote` (with `--ref` and `--state`),
exactly as `ledger append` does and through the same client
machinery, because a lane in any real posture works against a remote
ref. On the remote path the derivation and the pre-flight read the
**same materialized remote tip**: a fence read from a stale local
copy would be wrong under exactly the contention that makes claiming
online-only.

**`claim take` is remote-only.** `Table.Exclusive` marks
`claim.taken` alone, exclusivity is granted at the push round-trip,
and two offline actors claiming one contract have claimed nothing.
The verb refuses `--ledger` with the one account the raw seam already
gives, never a second explanation of one rule.

### The question a park may also ask

`claim park` alone takes `--question` and repeatable `--option
<id>=<text>`, because from `in_progress` an escalation rides the park:
nothing new may leave that state, and the park already carries the
packet and the fence ([`escalation.md`](escalation.md)). `claim
release` **refuses** those flags rather than ignoring them, so a
question written on the wrong verb is a refusal and never a silently
dropped one. Elsewhere the raise is its own verb, `escalation raise`.

Like the packet, the question is validated **at the door** — a
malformed one refuses `usage` before a session opens, rather than
costing a remote round-trip to be told the same thing by the boundary.

### The packet

The exits take `--packet <file>` and validate the four-part shape
([`packets.md`](packets.md)) **at the door**, before a session is
opened, so a malformed packet never becomes a signed record. The
`base` range may come from the file, from `--base`, or from `--repo`;
a file and a flag that disagree refuse rather than have a winner
picked by precedence.

### The envelope

Unchanged ([`envelope.md`](envelope.md)): position stamp,
affordances, budget block, and a journaled attempt at these
admission-boundary seams ([`refusals.md`](refusals.md)). Success
envelopes stay terse — teaching text lives in refusals — except where
the response names a position the caller would otherwise have to look
up: a take names the **fence** it established, a reserve names the
**reservation id** its close will cite.

Derivation refusals are journaled like admission refusals. A lane
that could not act is exactly the affordance gap the metric measures,
and the journal's `by_code` breakdown keeps `usage` and `not_found`
distinguishable from the boundary's own codes.

## Deliberately absent (v0)

- **No new authority.** Every act is a verb the tables already carry;
  nothing here can admit what admission refuses.
- **No orchestration and no retries.** These are acts, not a loop.
  The loop that sequences them landed as `internal/loop` (Phase 9 item
  1c) and is described below; it holds no authority these verbs do not.
- **No state outside the ledger.** Nothing is cached between
  invocations; every derivation is recomputed from the authoritative
  view.
- **No filing or specification verbs.** Those stay operator acts on
  the raw seam until a later card widens the set.
- **On the remote path, no success affordances and no journal.**
  There is no local ledger to reopen at the landed tip and none to
  journal beside; refusals there still carry affordances, computed
  from the materialized tip the act was judged against. Giving the
  remote posture its own journal home is a client-state decision this
  card does not make.

## The loop that sequences them

`internal/loop` (plan `plans/os-abb206c8.md`) is the worker lane's
loop made executable. It is a **library, not a CLI verb**: Seed does not
own the work, so the work step is supplied by the caller, and the
consumers are Phase 9 item 4's small-team and fleet fixtures, which
drive it in CI with no model and no wake channel.

`seed loop run` is **deliberately absent**. It would invite treating the
CLI as the agent, when the work step is the caller's and always was. It
can land later, on evidence, if something outside a test wants it.

The loop reimplements no verb. Every act goes through one seam whose
implementation is this CLI's own dispatch, so a refusal reaches the loop
with the boundary's account rather than a second admission check's guess
at it. It never falls back to `ledger append`: a refusal it cannot act
on is escalation's business (item 2), not a reason to reach past the
boundary that gave it.

### The sequence

Poll (`offer list`) → orient (`situation`, carrying the last position
forward as `--since`) → `claim take` → `budget reserve` → work →
`budget settle` → `submission make`, or `claim park` when a refusal
stops it after the window opened.

The spend bracket is **reserve, work, settle** rather than work then
meter: no execution path is unmetered ([`executors.md`](executors.md)),
so capacity is committed before the work it pays for.

### The act gate

The loop resolves its lane manifest at construction and performs only
the acts that manifest declares in `acts_through`, refusing anything
else **before it is signed**. "The manifest describes the loop" is
therefore enforced rather than coincidental: editing one without the
other fails.

### Exhaustion is the reserve, not the spending gate

A worker's exhaustion point is `budget.reserve` refusing on capacity,
and this distinction is load-bearing rather than pedantic:

| | verb | admitted from | reachable by a `claim` lane |
|---|---|---|---|
| the spending gate | `run.started` | `supervise`, `operator` | **no** |
| capacity exhaustion | `budget.reserve` | `claim`, `operator` | yes |

`transition.IsSpendingVerb` holds exactly `run.started`, which is the
**executor's** act ([`executors.md`](executors.md), step 2 of the spend
bracket). No key a worker loop signs with can trip it. So the build
plan's phrase "a budget refusal at a spending gate" names the concept —
the point where a lane is told it cannot spend — and for this lane that
point is the reserve.

The exit is `claim park` with its four-part packet. The findings carry
the refusal's **`code` and `message` verbatim**, and the acceptance part
is the contract's own anchor as the orienting read reports it: what a
successor is judged against is the contract's, never the lane's
paraphrase.

**The code the packet carries is the budget's, not the chain's.**
Exhaustion refuses under `budget_exhausted` (exit 27,
[`envelope.md`](envelope.md)), so the finding a successor reads names
the condition it can act on: the class is spent, and a caller that
asks for less can proceed. It used to refuse under the generic
`chain_invalid` (exit 8), which told a successor the ledger was broken
when only the budget was; the message carried the whole account, but
nothing above the message could tell exhaustion from a malformed
payload.

The code is deliberately narrower than the rule that emits it. The
budget rule has fourteen refusal sites and exactly one of them is
exhaustion; the other thirteen — a malformed payload, a non-positive
amount, a wrong signer, a class the table does not know, a citation
that resolves to no reservation, a double close, a release carrying
actuals — keep `chain_invalid`, because each is a bug in the caller
and none of them is answered by asking for less. A code that covered
the whole rule would read as "retry smaller" to a lane that had
signed with the wrong key.

### The lane's identity is its key's

The loop derives its actor by fingerprinting the key it signs with;
there is no parameter to supply one. A loop told to poll as one actor
while signing as another would select work under one identity's
eligibility, act under a second, and write liveness under a third —
and the classifier, which keys the observation stream by the holder,
would read silence from a worker that was working.

Deriving once is not enough. The loop passes `--key <path>` and the CLI
signs with whatever that path holds **now**, so a key rotated under a
running loop reopens the same mismatch through the filesystem. The
fingerprint is therefore re-derived and compared **before every act**,
not once per iteration: the work step is the longest thing in an
iteration and the likeliest moment for a rotation to land, and a check
only at the top would let the settle and the exit sign as a new
identity. A window opened by one actor and closed by another — or left
open because the close refused — is exactly the state the deliberate
exits exist to make impossible.

A change **refuses**. Adopting it silently would produce that state
rather than prevent it, so stopping is correct; and rotation being a
real operational event, the refusal names it and says to restart the
loop.

**A refusal from inside an open window still attempts the exit.**
Refusing is not returning. An error raised *after* the window opened
leaves a claim and its reservation standing, and returning it directly
would be the silent abandonment those exits exist to prevent — so the
loop attempts `claim park` anyway, carrying the cause as the packet's
findings, and reports only then.

That attempt is the one act **exempt from the identity gate**. Refusing
it there would guarantee the abandonment the gate exists to prevent, and
nothing is weakened by letting it through: the fence rule is the actual
protection, admitting holder-signed events only from the holder, so a
rotated key's exit refuses **at the boundary** rather than succeeding
wrongly.

Under a rotation, then, the attempt is expected to fail and the window
is genuinely stranded: the predecessor's fence cannot be cited by the
successor's key, no key the loop can reach closes it, and recovery is
the maintenance lane's reap ([`maintenance.md`](maintenance.md)). The error
says exactly
that — the window is left OPEN and needs a reap — and carries both
refusals, the one that stopped the work and the one that stopped the
exit.

This is the bound on *every involuntary exit leaves a packet*: what the
loop owes is the **attempt**, which it always makes, and not the
landing, which is not always in its gift. Where the exit does land the
packet carries the cause, exactly as an ordinary park would.

### Contention is ordinary

A lost `claim take` is not an error. In fleet mode two workers racing it
means the loser re-orients and takes different work, and treating that
as a failure would manufacture an escalation storm out of ordinary
contention. The iteration ends idle, with no window owed an exit.

The same holds one step later. A claim **reaped between its own push
and the loop's next read** leaves a window that is already gone: the
refreshed position-stamped read shows no window, which is the build
plan's middle convergence arm reached exactly as written — *the act is
no longer owed*. The iteration ends idle rather than reserving against
a claim it does not hold and then failing to park what it never had.

This does not weaken the rule that every path out of an **open** window
is a deliberate exit. The read is what establishes the window is not
open, and acting on a refreshed authoritative read is the opposite of
abandoning a claim.
