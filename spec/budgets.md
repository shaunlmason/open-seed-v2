# Budgets

The reservation model (SEED-NEXT.md §II.9, charter III.H rows 3–5;
plans/os-cecac5de.md): **budgets are reservations, not
observations**. After-the-fact metering cannot enforce a cap — two
workers can each observe 10 remaining and each spend 8 — so spending
requires an admitted `budget.reserve`, **checked and decremented at
admission**, the one place with a serialized view: the second of two
racing drafts admits against the tip that already carries the first,
and concurrent over-spend against one budget is structurally
impossible. Execution runs fenced against the reservation;
`budget.settle` (or `budget.release`) records actuals from adapter
metering. Where a provider cannot be stopped synchronously, the
budget is honestly a **risk limit, not a guarantee** — declared per
adapter and surfaced when Phase 7.3's adapters land (a named
extension point, like the charter's org/actor budget granularity;
v0 budgets are per-contract).

## Capacity: the class table

A contract carries its budget **class** at filing (`intent.filed`
requires the `budget` field, charter §II.6). The normative class
table, mirrored in code and pinned by test:
The class is validated at filing: `intent.filed` refuses a value outside
this table naming the members (`VocabularyError`, the completeness
family; [`tiers.md`](tiers.md)), so a contract nobody could reserve
against is never filed rather than discovered stuck by the worker who
claims it.

| class | capacity (units) |
|---|---|
| `small` | `100` |
| `medium` | `1000` |
| `large` | `10000` |

Units are abstract in v0; Phase 7.3's metering gives them adapter
meaning. A class missing from this table has **zero reservable
capacity**: reserves against it refuse — absent knowledge is never
fudged into spendable units.

## The verbs

All three are facts on the contract subject, from the {`claim`,
`operator`} capability rows ([`actors.md`](actors.md)). The claim
window gates the **reserve** alone: capacity is committed for the
window that will spend it, so a reserve outside `in_progress`
refuses. A reservation **outlives** that window, and so does its
close: settling or releasing one is legal wherever it stands open,
in the window that opened it, in a later one, or in none.

This is not a permissiveness. Windows end four ways, and one of them
is a failing verdict returning the contract to the queue: the next
claimant is a **different** worker, who is neither the reservation's
signer nor the operator, and a hold nobody could close would come
out of their remaining. A gate on the verb family made a retry after
a failed attempt quietly poorer than the first attempt. Nothing else
about a close changes: the identity check below has always asked
only whose reservation it is, never which state the subject was in.

Strict payloads (unknown fields refuse; the `fence` field rides the
fence rule):

- `budget.reserve` `{"amount": "<positive integer>", "fence": …}` —
  additionally, the drafting signer must be the **active claim
  holder** or the operator lane: a prior claimant reserving under
  the next holder's window would consume a budget it no longer works
  under. Refuses when `amount` exceeds remaining, naming both
  numbers.
- `budget.settle` `{"reservation": "<position>", "actuals":
  "<non-negative integer>", "fence": …}` — records **true** actuals,
  overruns included (an overrun shrinks remaining below what was
  reserved; recorded, never clamped).
- `budget.release` `{"reservation": "<position>", "fence": …}` —
  closes with zero actuals. Both closes must cite a valid, not yet
  effectively closed reservation, and the drafting signer must be
  that reservation's own reserving signer or the operator lane.

The fence citation follows the ordinary rule and needs no exception:
inside a claim window a close cites that window's active fence
(required of the holder and of any prior claimant, legal for anyone
else); outside one, no fence exists, so a close cites none and a
citation refuses. A close in a LATER window cites the later fence,
not the one the reservation was opened under: a fence dies with its
claim.

## Derivation, not mutation

The tolerant fold records reserves and close attempts as independent
fact lists, raw pushes included; **nothing is mutated and nothing is
erased**. Every consuming surface (the admission computation,
`seed budget status`, the projections) derives, position-accurately:

- a reservation is **valid** only when its signer was the operator
  lane, or held the claim capability AND was the subject's active
  claim holder, at the reservation's own position — so a raw foreign
  reserve consumes no capacity;
- a reservation is **effectively closed** only by its first **valid
  close**: an attempt whose signer is the reservation's own
  reserving signer or the operator lane at the attempt's position —
  so a raw foreign release frees no capacity and a raw foreign
  settle neither closes the reservation nor blocks the owner's later
  settle, which remains the effective closure;
- **remaining** = capacity − Σ open valid reservations − Σ settled
  actuals of valid reservations.

A close citing a position that is no reservation on the subject
folds as an anomaly, never a fact.

## Spending verbs

The charter's "spending verbs require an admitted `budget.reserve`"
lands as a data table of spending verbs. Its first entry is
**`run.started`** ([`executors.md`](executors.md)): execution spend
initiates through it, so no run provisions outside the reservation
gate. The budget rule refuses any listed verb on
a subject with no open valid reservation; the gate is additionally
drilled through test injection in isolation.
Exhaustion produces a structured refusal the worker can act on, and
it is the code, not just the message, that says so:
`budget_exhausted` (exit 27, [`envelope.md`](envelope.md)) is
allocated for this one refusal out of the budget rule's fourteen, so
a lane can answer "the class is spent" by asking for less without
first parsing prose. The parking mechanics the park invokes — the `claim.parked` exit with
its packet at a safe point — landed with 7.4, the envelope's
`{reserved, remaining}` block is Phase 8's, and the worker-lane
loop that answers exhaustion by taking that exit is named in
docs/build-plan.md Phase 9 item 1.

**Per racer.** On a racing squad's contract ([`lifecycle.md`](lifecycle.md),
"Racing") each racer reserves against its own fence and settles its
own actuals; the subject's budget view sums the open reservations, and
a settled-out racer's reservation closes with its exit or its reap
like any other. Nothing is shared: the cost the squad declared is
paid once per racer, which is what the declaration's sentence says.

## Surfaces

- `seed budget status --ledger <dir> --subject <id>` — the derived
  view: class, capacity (when the class is known), open
  reservations, settled actuals, effective closes, remaining.
  Reserve, settle, and release append through `seed ledger append`
  and the library admission path. Refusals reuse the established
  admission exits with a single exception: capacity exhaustion at
  `budget.reserve` carries `budget_exhausted` (exit 27). Every
  other refusal in the rule — malformed payloads, wrong signers,
  unknown classes, bad citations, double closes — stays
  `chain_invalid`, since none of them is answered by reserving a
  smaller amount.
- Projections ([`projections.md`](projections.md)): the contracts
  view serializes the derived budget object and reservations only
  beside budget facts, so budget-inactive chains keep byte-identical
  bodies; the cache's generation adds a `reservations` table and
  budget columns (`budget_class` always; capacity and remaining only
  beside facts with a known class, NULL otherwise).
