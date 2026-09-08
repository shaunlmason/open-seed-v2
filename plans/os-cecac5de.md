# Plan: next Phase 7.2 — budget reservations (os-cecac5de)

Implements `docs/next-build-plan.md` Phase 7 item 2: `budget.reserve`
/ `settle` / `release`, with reservations checked and decremented at
admission and the reservation race drill. Design authority:
SEED-NEXT.md §II.9 ("Budgets are reservations, not observations
(normative)": after-the-fact metering cannot enforce a cap — two
workers can each observe 10 remaining and each spend 8 — so spending
requires an admitted `budget.reserve`, checked and decremented at
admission, the one place with a serialized view; execution runs
fenced against the reservation; settle or release records actuals
from adapter metering; where a provider cannot be stopped
synchronously the budget is honestly a risk limit, never fudged),
§II.6 (a contract carries tier, budget, and routing at filing — the
`budget` field is already required by the completeness table), the
admission order ("check budget reservation where the verb spends"),
and charter III.H rows 3–5. The `budget.*` verbs are already in the
protocol catalog: no catalog growth this time. Adapter metering, the
risk-limit declaration's per-adapter values, the envelope's
`{reserved, remaining}` block, and park-on-exhaustion behavior land
with 7.3, Phase 8, and 7.4 respectively; this task builds the
reservation machinery they consume.

## Design decisions (binding for this task)

- **D1 — capacity comes from the filed class, via a data table.**
  The `budget` field intent.filed already requires is a class
  (`small` in every fixture). A spec-pinned table maps classes to
  integer capacity units: `small` 100, `medium` 1000, `large` 10000
  (abstract cost units in v0; 7.3's metering gives them adapter
  meaning). The table is data in `next/spec/budgets.md`, mirrored as
  a map in code and pinned by a parsing test, the actors.md
  capability-table pattern. A contract whose class is missing from
  the table has zero reservable capacity: reserves refuse — absent
  knowledge is never fudged into capacity. Scope is per-contract in
  v0; the charter's org/actor granularity is a named extension point
  in the spec, not emitted machinery.
- **D2 — the verbs.** All three are facts on the contract subject,
  admitted only while the subject is `in_progress` (execution is
  fenced against the reservation, and spend happens inside a claim
  window). Capability row {claim, operator} for all three; the fence
  rule already forces the holder's citation on a held subject.
  Strict payloads: reserve `{"amount": "<positive integer>",
  "fence": …}`; settle `{"reservation": "<position>", "actuals":
  "<non-negative integer>", "fence": …}`; release `{"reservation":
  "<position>", "fence": …}`. Settle records TRUE actuals even above
  the reserved amount (an overrun is recorded and surfaced, never
  clamped); release closes a reservation with zero actuals.
- **D3 — admission is the serialized view.** A new `budget` rule
  (after the offer rule) computes, for a reserve, remaining =
  capacity − Σ open boundary-valid reservations − Σ boundary-valid
  settled actuals, and refuses `amount > remaining` with a
  structured refusal naming both numbers. Settle and release must
  cite an existing, still-open reservation on the subject and
  revalidate it per D4. Concurrent over-spend is structurally
  impossible because the second draft admits against the tip that
  already carries the first — the claim-race shape, drilled.
- **D4 — the laundering shape, on both sides, with derivation not
  mutation.** The tolerant fold records any well-shaped budget fact
  (raw pushes included) as an independent fact: reservations and
  close attempts, in chain order, and a close attempt NEVER mutates
  the reservation it cites. Effective state is derived at every
  consuming surface (the admission computation, the citation check,
  `seed budget status`), position-accurately per the
  VerifySeals/offerAuthorized replay pattern: a reservation is
  **valid** only when its signer was the operator lane or THE
  active claim holder at that position — prior claimants are
  excluded, since a released worker reserving under the next
  holder's window would consume a budget it no longer works under —
  and a reservation is **effectively closed** only by its first
  **valid close**: one whose signer is that reservation's own
  reserving signer or the operator lane at the close's position.
  Consequences, all drilled: a raw foreign reserve consumes no
  capacity (no denial-of-service by raw push); a raw foreign
  release frees no capacity (no over-spend by laundered close); a
  raw foreign settle neither closes the reservation nor locks the
  owner out — the owner's later settle is still the effective
  closure; and a released prior claimant cannot reserve against the
  active holder's window even while citing the active fence the
  fence rule requires of it.
- **D5 — spending verbs are a table, empty in v0.** The charter's
  "spending verbs require an admitted budget.reserve" lands as a
  data table of spending verbs in the budgets spec (the
  contract-is-data posture); the budget rule refuses any listed verb
  on a subject with no open boundary-valid reservation. The table
  ships empty — 7.3's metering fills it — and the mechanism is
  drilled through a test-injected table so the gate exists before
  its first customer.
- **D6 — fold, projections, CLI.** The birth fold keeps the filed
  `budget` class beside Tier; SubjectState gains
  `Reservations []ReservationFact{Pos, Signer, Amount}` and
  `BudgetCloses []CloseFact{Pos, Signer, Reservation, Kind,
  Actuals}` as independent fact lists (facts persist, nothing
  erased, nothing mutated); shared derivation helpers compute
  validity, effective closure, and remaining per D4 for the admit
  rule, the CLI, and the projections alike. Contracts view "10"
  serializes a
  `budget` object (class, capacity, remaining) plus reservations,
  all omitempty so budget-inactive chains keep byte-identical "9"
  bodies; report "7" (republish only); cache generation 9 adds a
  `reservations` table and budget columns. CLI: `seed budget status
  --ledger --subject [--table <path>]` (read-only: class, capacity,
  open reservations, settled actuals, remaining) — reserve, settle,
  and release append through `seed ledger append` and the library
  admission path like every claim-lane fact; no new exit codes
  (insufficient-capacity refusals ride the established
  shape-refusal mapping).

## Steps

1. **Spec.** `next/spec/budgets.md`: the reservation model (§II.9
   quoted posture), the class table (the normative data the code
   mirrors), payload schemas, the in_progress window, the
   boundary-validity rule for capacity counting, the empty spending
   verb table and its refusal semantics, overrun honesty, the
   org/actor granularity and per-adapter risk-limit declaration as
   named extension points (7.3/Phase 8 pointers). Update
   `next/spec/lifecycle.md`'s fact-verb window sentence and
   `next/spec/actors.md`'s capability table (three rows, {claim,
   operator}).
2. **Keyring.** `AcceptedCapabilities` rows for the three verbs;
   vocabulary and completeness test rows; actors.md pin.
3. **Fold.** Budget class captured at birth; ReservationFact and
   CloseFact capture for well-shaped reserve/settle/release in
   chain order as independent lists — a close attempt never mutates
   the reservation it cites (effective closure is D4's derivation);
   malformed payloads fold to nothing; a settle/release citing a
   position that is no reservation on the subject folds as an
   anomaly, the raw-override posture.
4. **Class table in code.** A small budgets package or transition
   addition holding the class→capacity map and the spending-verb
   set, both mirroring the spec by parsing test (the actors.md
   pattern); capacity lookup and remaining computation as pure
   functions the admit rule and the CLI share.
5. **Admission.** The `budget` rule: verb-gated on the three verbs
   plus the spending-verb table; strict payload decode; in_progress
   window; positive integer amounts; a reserve additionally requires
   the drafting signer to be the active claim holder or operator
   NOW (the fence rule alone would let a prior claimant through);
   the D3 remaining computation with the D4 validity filter;
   settle/release citation validation (cites a valid reservation,
   not yet effectively closed, and the drafting signer is that
   reservation's owner or operator). Check previews it
   cooperatively.
6. **CLI.** `next/cmd/seed/budget.go`: `seed budget status` per D6.
7. **Drills** (package tests + `budget_cli_test.go`), each naming
   III.H: the **reservation race** — two concurrent reserves of 8
   against capacity 10 through the library admission path, first
   admits, second refuses insufficient with both numbers
   (III.H row 3, `// conformance: III.H — concurrent over-spend
   structurally impossible`); **lifecycle** — reserve outside
   in_progress refuses, reserve over remaining refuses, settle
   closes and frees nothing (actuals consume), release frees,
   top-up reserves coexist, overrun settle records actuals above
   reserved and shrinks remaining accordingly; **laundering** — a
   raw foreign reserve consumes no capacity, a raw foreign release
   frees none (remaining unchanged, no over-spend opened), a raw
   foreign settle does not close the reservation and the owner's
   later settle still admits as the effective closure, and a
   released prior claimant's reserve refuses while the active
   holder's admits (all facts fold, all invalid ones inert);
   **spending gate** — an injected spending verb refuses without an
   open reservation and admits with one; **unknown class** — zero
   capacity, reserve refuses; fold and status-surface assertions;
   projection coverage for the new fields.
8. **Docs.** `next/docs/progress.md` 7.2 row and frontier;
   `next/docs/decisions.md` one dated entry for D1–D6;
   `memory/LEARNINGS.md` only if implementation surfaces a durable
   insight.

## File Scope

- `next/spec/budgets.md` (new), `next/spec/actors.md`,
  `next/spec/lifecycle.md`
- `next/internal/keyring/keyring.go` (+ tests)
- `next/internal/transition/transition.go` (+ tests, class table +
  reservation fold)
- `next/internal/admit/admit.go` (+ tests)
- `next/internal/project/contracts.go`, `report.go`, `cache.go`
  (+ fixtures/tests)
- `next/cmd/seed/budget.go` (new), `next/cmd/seed/budget_cli_test.go`
  (new), `next/cmd/seed/main.go` (wiring)
- `next/docs/progress.md`, `next/docs/decisions.md`,
  `memory/LEARNINGS.md` (conditional)

## Acceptance Criteria

**Boundary set (new, shown working):**

- The race drill: two concurrent 8-unit reserves against a 10-unit
  class admit exactly once; the loser's refusal names amount and
  remaining; no interleaving reaches combined spend above capacity.
- Reserve admits only from the claim holder or operator during
  in_progress, within remaining; settle records actuals (overrun
  included) and closes; release closes free; both refuse citations
  of missing, closed, or boundary-invalid reservations.
- A raw-pushed foreign reserve folds but consumes no capacity; a
  foreign release frees none; a foreign settle closes nothing and
  the owner's later settle still admits; a released prior
  claimant's reserve refuses; an injected spending verb refuses
  without an open reservation and admits with one; an unknown class
  yields zero capacity.
- `seed budget status` reports class, capacity, open reservations,
  settled actuals, and remaining, agreeing with the admission
  computation; contracts "10" serializes budget facts omitempty
  with budget-inactive views byte-identical to "9" bodies.

**Retention set (existing, shown unharmed):**

- `make check` green; coverage ≥90%; every earlier drill (offers,
  lockout, seals, verdicts, reconcile) passes unmodified.
- No new exit codes, no envelope changes (the budget block is
  Phase 8's), no v1 surface changes; the task PR never touches
  `plans/**`.

## Validation Commands

- Boundary: `cd next && go test ./... -run 'Budget|Reservation' -count=1`
- Retention: `make check`

## Expected diff shape

One new spec file, one new CLI file with its test, and targeted
additions to keyring, transition, admit, and the three projection
files with fixtures; two docs files. No deletions, no `.seed/**`, no
`plans/**` in the task PR.
