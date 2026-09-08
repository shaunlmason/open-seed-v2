# Plan: next — the guardrail bar requires an offer the boundary does not (os-aaec6a3c)

`simulate.Audit`'s guardrail-breach bar names any subject whose
`claim.taken` did not follow an `offer.published`: "claiming work the
supervisor never offered is a guardrail breach". Admission holds no
such rule, so a chain the boundary took reports breaches. Found while
implementing os-88df7ab2 (#311), whose covered-arm drills audit an
admission-grade chain from `internal/history.Generate` and saw the
guardrail bar fire on every subject in it. That card asserts only the
bar it moves and says why; this card settles the disagreement. Tier:
standard (it changes what a conformance bar counts, or what admission
accepts). Deps: none.

## What the tree actually shows

- **The bar is the only place the rule exists.** `internal/simulate/
  audit.go` tracks `offered` per subject and appends a guardrail
  breach when a claim arrives without it. Nothing else in `next/**`
  refuses or reports an unoffered claim.
- **Admission does not ask.** The rule set's `claim.taken` arms are
  authoring isolation (the key that sealed the subject's checks may
  not claim it) and the lifecycle transition; no rule consults the
  subject's offers. `SubjectState.LiveOffers` has exactly two callers,
  `cmd/seed/offer.go`'s listing and `internal/eval`'s
  ready-with-no-live-offers signal, both scheduling reads rather than
  admission.
- **The tree's own fixtures claim without offering.**
  `internal/history.Generate` stages `intent.filed`,
  `contract.specified` and `claim.taken` with no offer, and its chains
  verify and pass the `seed-admit` hook: they are the perf gate's
  history and the migration and start drills' fixture. So the bar
  reports breaches on the closest thing the tree has to a real chain.
- **The unoffered claim is the bar's only source.** `GuardrailBreaches`
  is appended in exactly one place, the `claim.taken` arm. So dropping
  the offer rule alone would leave a bar that can never fire, which is
  the failure this family keeps finding at the other end: a bar that
  counts nothing reports nothing, and III.R row 5 would carry a
  guarantee no drill can break (measured while planning).
- **Admission does guard the claim path, and the guard is chain-
  visible.** The rule set refuses a `claim.taken` whose actor sealed
  the subject's checks: "the key that sealed the subject's checks
  never implements against them". The fold already carries what that
  needs, `SubjectState.Sealed` with its `Pos` and `Signer`, so a
  raw-pushed claim by the sealing key is a guardrail breach the audit
  can name from the chain alone.
- **The charter makes offers the model, not a per-claim precondition.**
  §II.9 is normative about the supervisor: "the supervisor publishes
  offers; workers pull and claim; the claim settles at admission",
  and "there is no assignment to orphan, only offers that get claimed
  or expire". III.H row 1 states the same model and is proven by the
  wakeless poll-only run. Neither says an unoffered claim is refused,
  and III.R row 5's bar counts "guardrail breaches", which everywhere
  else in the tree means the guardrails the boundary enforces.

## Design decisions (binding for this task)

- **D1 — the bar counts what the boundary guards, not what it does
  not.** Two halves, and the second is why this is a correction
  rather than a deletion:
  - The offer rule goes. Admission takes an unoffered claim, so
    naming it a breach reports a violation the system does not hold.
  - The sealed-author rule arrives. Admission refuses a `claim.taken`
    whose actor sealed that subject's checks, and the fold carries
    `Sealed.Signer` and `Sealed.Pos`, so a raw-pushed claim by the
    sealing key is a breach the bar can name from the chain: the
    subject, with the sealing position, in the evidence list.
  The bar therefore keeps counting, and counts something the boundary
  actually enforces. This is the same correction os-b86dab4c made to
  the bar's verb names and os-88df7ab2 made to its budget model.
- **D2 — the rejected reading, stated so a reviewer can take it.**
  The alternative is that the offer IS a precondition and admission
  has a gap: then the fix is an admission rule refusing a claim that
  rides no live offer, and `internal/history` is writing chains the
  boundary should not take. That is rejected here because the charter
  binds the supervisor's scheduling model rather than the claim's
  admissibility, because the wakeless-poll proof (III.H) turns on
  workers claiming what they find rather than on the offer's
  presence, and because a new admission refusal would invalidate the
  perf history, the migration fixture and every drill that claims
  without offering, which is a blast radius no conformance bar's
  wording justifies. A reviewer who reads §II.9 the other way should
  say so on this PR: the whole change is one bar and its drills.
- **D3 — what replaces it, so the signal is not lost.** An unoffered
  claim is a scheduling anomaly worth seeing, and the tree already
  has the surface for it: `internal/eval`'s ready-with-no-live-offers
  read. The bar's removal is paired with one sentence in
  `next/spec/simulation.md` saying where the concern now lives, so a
  later reader does not restore the bar from memory.
- **D4 — the drills pin both halves, so the bar cannot go dead.**
  `TestAuditCatchesUnofferedClaim` inverts: a chain claiming without
  an offer is not a guardrail breach, and the drill says admission
  takes it. A new drill plants the sealed-author claim and asserts the
  bar names the subject, so the bar has a violation that fails when it
  is removed. A third audits an admission-grade chain from
  `internal/history` and asserts the guardrail list is empty, which is
  the case that found this card. Together they hold the bar to
  firing on what admission refuses and only that.
- **D6 — the contract's own words, and the clause this card cannot
  carry.** `plans/os-16e55c11.md` D5 words the bar as "zero guardrail
  breaches (no refusal followed by a blind retry; every claim within
  its ceiling once item 4 lands)". Neither clause is the offer rule
  the bar shipped, so removing that rule loses no ceiling coverage:
  the bar never had any. Nor can this card add it. Admission's
  `ceiling` policy rule reads `Context.Declaration`, the deployment
  declaration in `seed.json`, and `internal/admit` is explicit that
  this is "admission policy rather than chain validity ... so a chain
  never changes meaning because of a file beside it": a raw-pushed
  claim above the ceiling folds as filed and the chain still verifies
  byte for byte. `simulate.Audit` takes records only, which is III.R
  row 5's own contract ("the ledger alone reconstructs and justifies
  everything"), so two byte-identical chains are compliant or not
  depending on a file the ledger does not carry. Reaching the ceiling
  needs the declaration threaded through `simulate.Audit` and
  `cmd/seed/ledger.go`, both outside D5's bounds, so it is carded:
  **os-b5051f2e**, which also owns the blind-retry clause (a refused
  append never lands, so a refusal followed by a retry leaves only the
  retry in the chain, and no reader can see it). The sealed-author
  rule D1 adds is the guardrail on the claim path that IS a record
  fact, which is why it is the one this card can implement.
- **D5 — bounds.** No admission change, no transition-table change,
  no new verb, no change to the other four bars, and no change to
  `internal/history`. `seed ledger audit`'s envelope, exit codes and
  evidence lists are unchanged. III.R row 5's status does not move
  here: the row is met by the shadow run's evidence, not by this.

## Steps

1. D1 in `internal/simulate/audit.go`: the `offered` tracking and its
   breach removed, the sealed-author arm added, both reasons in the
   comment.
2. D4's two drills in `internal/simulate/audit_test.go` and the
   admitted-chain assertion beside os-88df7ab2's.
3. D3's sentence in `next/spec/simulation.md`;
   `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/simulate/audit.go`, `next/internal/simulate/audit_test.go`
- `next/spec/simulation.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-aaec6a3c.json`

Nothing else. NOT `next/internal/admit/**`, NOT
`next/internal/transition/**`, NOT `next/internal/history/**`, NOT
`next/internal/eval/**`, NOT `next/cmd/**`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **An admitted chain has no guardrail breach.** A chain from
   `internal/history.Generate` audits with `guardrail_breaches`
   empty, through `simulate.Audit`.
2. **The offer rule is gone, not weakened.** A chain claiming with no
   offer at all reports no guardrail breach, and the drill states that
   admission takes such a claim.
3. **The bar still fires on what admission refuses.** A raw claim by
   the key that sealed the subject's checks is named a guardrail
   breach, and removing the new arm fails that drill; the bar is never
   left counting nothing.
4. **The other bars are untouched.** The unreserved-spend,
   silent-abandonment, chain-violation and lost-update arms pass
   unchanged, `seed ledger audit`'s drills included.
5. `make check` green; no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- `admit`, `transition`, `history` and `eval` are byte-identical to
  `main`; the audit's four other bars keep their meaning and their
  evidence lists; the verb's envelope is unchanged.

## Validation Commands

- Boundary: `cd next && go test ./internal/simulate/ -count=1`
- Boundary: `cd next && go test ./cmd/seed/ -run 'LedgerAudit' -count=1`
- Retention: `make check` (exit checked separately from any pipe)

## Expected diff shape

Modified: `audit.go` (the `offered` field, one case and one append
removed), `audit_test.go` (one drill inverted, one added),
`simulation.md` (one bar's line and one sentence), the three docs
files, the receipt. Roughly +40/-25 lines. No other file.
