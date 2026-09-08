# Plan: next — the unattended maintenance loop (os-8a5f14bb)

Phase 9 item 3 of `docs/next-build-plan.md`: *"Maintenance loop: reap
expired/wedged, reconcile divergence, rebuild projections, checkpoint
(signed), lints — runnable unattended; audited as an ordinary actor."*

Two specs already name this lane's reap as **their** recovery path, so
this card is owed to code that has shipped:

- [`loop-verbs.md`](../next/spec/loop-verbs.md): a window stranded by a
  key rotation cannot be parked by the rotated key, because the fence
  rule admits holder-signed events only from the holder. *"Recovering it
  is the maintenance lane's reap."*
- [`escalation.md`](../next/spec/escalation.md): a contract frozen by a
  persuaded lane raising a question is named as what this lane's lints
  should surface.

## What already exists, and what this card must not rebuild

Measured against the tree, not assumed:

- **`internal/reconcile`** already carries a closed finding set —
  `Finding{Subject, Class, Detail}` with `Subject`, `VerifyVerdicts`,
  `VerifySeals`, `VerifyOverrides`, `Classify`. One of its own detail
  strings forward-references this card: *"pending or diverged is an age
  judgment for maintenance, not this classifier."*
- **But that is only the record-derived half.** The **evidence-grade**
  checks — attested-head reconciliation against the cited receipt,
  target-rewrite detection, receipt retrievability — live in the
  **unexported** `evidenceFindings` in `cmd/seed/reconcile.go`, and
  they are the ones that see divergence visible only through the
  repository or the artifact store. A maintenance pass built on
  `internal/reconcile` alone would pass while omitting exactly the
  divergence reconciliation the charter asks it for (review finding on
  #203).
- **`internal/obs.Classify`** gives `expired` / `wedged` / `no_data`
  against `Thresholds`, as `observations.md` defines them.
- **`internal/obligation`** already emits `run.unsettled`
  **position-anchored** exactly as the Phase 7 exit requires: flagged
  only once the subject has taken a subsequent claim window or reached
  a terminal state, because post-close settlement is a valid
  intermediate state.

So the lints are largely **built**. What is missing is a lane that runs
them unattended, decides what a finding becomes, and can reap.

**A correction, since the card body implied otherwise.**
`lanes/maintenance.json` declaring `"acts_through": []` is **not a
gap**. `internal/lane`'s validation obliges only a lane holding
`CapClaim` to declare acts; a lane acting through the raw append seam
declares none, and the maintenance lane holds `maintenance` and
`operator`. Its empty declaration is the documented posture.

## Design decisions (binding for this task)

- **D1 — `seed maintain run` is a CLI verb, and the reason `seed loop
  run` was refused does not apply here.** `internal/loop` is a library
  with no verb because **Seed does not own the work**: the work step is
  the caller's, and a CLI verb would invite treating the CLI as the
  agent.

  The maintenance lane's work is Seed's own. Reaping, reconciling,
  rebuilding and checkpointing are defined acts over the ledger with no
  caller judgment inside them — there is no work step to supply. So the
  argument that refused `seed loop run` is not an argument against this
  verb; it is what distinguishes them, and the plan records that rather
  than leaving the asymmetry to look like inconsistency.

  The decision logic still lives in `internal/maintain` with its
  effects injected, so it is drillable without a ledger — the
  `internal/covergate` shape (plan `plans/os-cafba959.md`).

- **D2 — the lint set is CLOSED, and this card adds exactly one.**
  `internal/reconcile`'s classes are the set; the addition is
  **unsettled-run detection**, consumed from `internal/obligation`'s
  `run.unsettled` rather than re-derived. Re-deriving it would put the
  position-anchoring in two places, and the anchoring is the whole
  subtlety: a closed-without-settle predicate files spurious findings
  mid park or reap flow.

  Closed means a new lint lands by adding a class **with the spec that
  pairs it to its fact**, the `factDischargers` precedent in
  `obligations.md`. An open-ended lint list would make this loop a
  policy surface, which is what "audited as an ordinary actor" denies.

- **D2.5 — the evidence-grade checks move into `internal/reconcile`,
  and both callers consume one implementation.** `evidenceFindings`
  already returns `reconcile.Finding`; it lives in `cmd/seed` only
  because that is where the artifact store and repository handles were
  wired. Moving it (taking `*artifact.Store` and the repo path as
  parameters, as it already does) gives `seed reconcile` and `seed
  maintain run` **one** divergence surface rather than two that can
  drift.

  This is the same principle `obligations.md` states for its own
  derivations: a second copy of one derivation is the failure the
  projection exists to prevent. A drill asserts the maintenance pass
  reports an evidence-grade finding — a rewritten target — that
  `internal/reconcile.Classify` alone cannot see, so "consumes the
  complete result" is enforced rather than asserted.

- **D3 — a lint finding becomes a FILED DEFECT CONTRACT, never an
  escalation.** The charter says maintenance "files defect contracts",
  and the distinction is real: an escalation **freezes** a contract and
  demands a human decision; a lint finding is work someone should do.
  Filing is `intent.filed`, which the maintenance lane can sign because
  it holds `operator`.

  Consequence, stated: the maintenance loop can therefore create work,
  which is authority. It is bounded by being attributable (its own key,
  its own lane) and by filing nothing but contracts — it cannot claim
  them, because it does not hold `CapClaim`.

- **D4 — a reap answers an UNANSWERED REQUEST, never a timeout.** This
  is the plan's sharpest constraint, and its first draft got the second
  half wrong in the worst available way (review finding on #203).

  The premise is right: `observations.md` declares the channel
  ephemeral and lossy, so a dropped stream and dead work look identical
  from outside, and silence alone can never reap. The first draft then
  named "the claim's own lease elapsed" as the corroborating fact.
  **There is no lease.** The word appears exactly once in the whole
  `next/` spec tree, in the sentence that denies it: *"Seed holds no
  lease: a claim stands until a deliberate exit or a reap"*
  (`observations.md`). Implementing that conjunct would have meant
  inventing lease semantics or picking an undeclared threshold — and
  the risk section worried the two facts might be *correlated* when one
  of them was not a fact at all.

  The corroboration that does exist is better, and
  [`executors.md`](../next/spec/executors.md) names this card as its
  consumer: *"B-style automatic timeout reaping is the Phase 9
  maintenance loop's job; it presupposes exactly these semantics."* The
  semantics are the force path: a worker that **ignores its interrupt**
  is killed and reaped, with the findings recording the ignored
  interrupt.

  So a reap requires **both**:
  1. an `expired` or `wedged` classification from `obs.Classify`, and
  2. a ledger fact that the holder **was asked to stop and did not** —
     an admitted `run.interrupted` on the active fence with no
     deliberate exit after it (`admit.InterruptRequested` already
     counts only interrupts that passed the boundary at their own
     position, so a raw unprivileged interrupt corroborates nothing),
     or an admitted `wedge.declared`.

  That is genuinely independent of the observation stream: one is an
  obs-channel classification, the other a chain fact at a known
  position. And it changes what a reap MEANS — not "long enough has
  passed" but "someone asked, and nothing happened", which is the only
  form of corroboration a lossy channel can support.

  `no_data` carries **no reap path whatever**, however old. A drill
  asserts that directly, because it is the case where the instinct to
  reap is strongest and the evidence weakest.

  **No heartbeat predicate is added.** Non-advancing observations are
  not a heartbeat signature: a legitimate long-running step emits
  exactly that shape, and the existing expiry/wedge classification
  already distinguishes it.

- **D4.5 — a checkpoint persists a snapshot a fresh reader can
  actually start from.** The first draft said "checkpoint (signed)" and
  stopped there, which would let every acceptance criterion pass with
  an **unusable** checkpoint (review finding on #203).

  The charter is specific: the checkpointed snapshot is stored
  retrievably, *"the checkpoint event carries its hash and location, so
  a fresh reader fetches the snapshot, verifies it against the signed
  checkpoint, and starts — without first rebuilding"*. So the
  checkpoint step must (a) write the canonical projection
  materialization to the artifact store (`internal/artifact`, which
  already exists and is already content-addressed), (b) carry that
  hash and location in the `system.checkpoint` payload under a
  **versioned** format, and (c) validate that payload at admission —
  today the boundary accepts an arbitrary checkpoint payload.

  Drilled by a **reader round trip**: fetch the snapshot named by a
  checkpoint, verify it against the signature, and start from it
  without replaying the chain. A checkpoint nobody has ever started
  from is a claim, not a capability.

- **D5 — "runnable unattended" is drilled WAKELESS.** No scheduler, no
  wake channel: the drill runs one pass against a real ledger and
  asserts what it did. Item 4's fixtures use the same posture, and a
  loop that needs a scheduler to be testable is one nobody can prove
  runs unattended.

- **D6 — audited as an ordinary actor, asserted rather than described.**
  Every act the loop takes is signed with the maintenance key and goes
  through the same admission boundary. A drill runs the loop with a key
  holding **only** `maintenance` and asserts the acts needing
  `operator` refuse `out_of_grant` — so "no private powers" is a
  property the boundary enforces, not a claim the summary makes.

- **D7 — scope guard.** No new lifecycle verb, no change to
  `obs.Classify`'s thresholds or classes, no change to the reconcile
  findings this card does not add. `claim.reaped` already exists and is
  already capability-gated; this card gives it a caller, not a new
  authority.

## Steps

1. `next/internal/maintain` — the pass: reap candidates, lint findings,
   projection rebuild, checkpoint, with the ledger effects injected.
2. `next/internal/maintain/maintain_test.go` — the drills of D4 and D6,
   including `no_data` refusing to reap and the operator-less key
   refusing.
3. `next/internal/reconcile` — the unsettled-run class, consumed from
   `internal/obligation`; and `evidenceFindings` moved here from
   `cmd/seed` so both callers share one divergence surface (D2.5).
4. `next/internal/artifact` + the checkpoint payload — the snapshot
   written retrievably, its hash and location carried in a versioned
   payload, validated at admission (D4.5).
5. `next/cmd/seed` — `maintain run`, its envelope, and the affordance
   catalog entries for any act it performs.
6. `next/spec/maintenance.md` (new) — the loop, the reap's corroboration
   rule, the closed lint set, what a finding becomes, and the
   checkpoint's snapshot contract.
7. `next/lanes/maintenance.json` — `acts_through` stays empty (it acts
   on the raw seam); the summary gains what the loop actually does.
8. `next/docs/decisions.md`, `memory/LEARNINGS.md`; receipt; evidence;
   review.

## File Scope

- `next/internal/maintain/**` (new), `next/internal/reconcile/**`
- `next/internal/admit/**` (the checkpoint payload rule, D4.5)
- `next/cmd/seed/**`
- `next/spec/maintenance.md` (new), `next/spec/observations.md` and
  `next/spec/executors.md` (the reap's corroboration rule,
  cross-referenced)
- `next/lanes/maintenance.json`
- `next/docs/decisions.md`, `next/docs/progress.md`, `memory/*`
- `receipts/os-8a5f14bb.json`

Nothing outside `next/**` except the work-product files above.

## Acceptance Criteria

1. One `maintain run` pass against a real ledger reaps a claim that
   is **both** classified `expired` and carrying an unanswered
   admitted `run.interrupted` on its active fence, and leaves a
   `claim.reaped` packet whose findings record the ignored interrupt
   (`executors.md`'s force path). Asserted by reading the chain back
   rather than by the verb's own report.
2. **A `no_data` stream is never reaped**, however old the claim, and
   the refusal says why. Drilled directly, because this is where the
   instinct to reap is strongest and the evidence weakest.
3. An `expired` classification **without** an unanswered request does
   not reap either, and an unanswered request without the
   classification does not reap: the corroboration is a conjunction,
   and a drill plants **each half alone**. A raw, boundary-refused
   `run.interrupted` corroborates nothing, asserted separately, because
   `admit.InterruptRequested` counting only admitted interrupts is what
   makes the conjunct meaningful.
3b. The maintenance pass reports an **evidence-grade** finding — a
   rewritten merge target — that `internal/reconcile.Classify` alone
   cannot see. That is D2.5 enforced: a pass built on the
   record-derived half only would be green here and wrong.
4. The unsettled-run lint fires only once the subject has taken a
   subsequent claim window or reached a terminal state, and **not**
   mid park or reap flow. Both directions asserted, so it cannot pass
   by never firing.
5. A lint finding files a defect contract, and the filed contract cites
   the finding's class and subject. No lint raises an escalation.
5b. A checkpoint's snapshot is **fetched and verified by a fresh
   reader**, which starts from it without replaying the chain. A
   checkpoint whose payload names no retrievable snapshot, or whose
   snapshot does not match the signed hash, refuses at admission.
6. **The loop holds no private powers**: run with a `maintenance`-only
   key, the acts needing `operator` refuse `out_of_grant` at the
   boundary. That is D6 enforced rather than described.
7. **Mutation evidence, per fix.** Each must fail a drill: allowing
   `no_data` to reap; dropping the unanswered-request conjunct;
   accepting a raw (boundary-refused) interrupt as corroboration;
   re-deriving the unsettled-run anchoring instead of consuming it;
   turning a lint finding into an escalation; dropping the
   evidence-grade findings from the pass; and accepting a checkpoint
   whose snapshot hash does not match.
8. `make check` green with coverage measured **cold**, at least three
   readings above the gate, and the suites pass unprivileged.

## Validation Commands

```sh
cd next && gofmt -l . && go vet ./... && go build ./...
cd next && go test ./internal/maintain/ ./internal/reconcile/ -count=1
cd next && go test ./... -count=1
make check
```

## Expected diff shape

One new package with its drills, one new lint class, one moved
function, one checkpoint payload rule, one CLI verb, one new spec and
three amended, plus the work-product files. Roughly +1200/-80 lines,
all under `next/**`.

## A risk worth naming now

D4's corroboration rule is still the decision most likely to be wrong,
and the first draft's version of this section is itself the warning.

It asked whether the two conjuncts might be **correlated** — both
derived from the same silence — and concluded they were not, reasoning
carefully about a fact that **did not exist**. The independence
argument was sound and the premise was invented. A risk section that
reasons well about the wrong thing reads exactly like one that reasons
well.

So the standing check is not "are these independent?" but "does each
conjunct name something the tree actually has?" For the current pair:
`obs.Classify` is in `internal/obs`, and `admit.InterruptRequested` is
in `internal/admit`, both with their own drills. The plan cites the
function, not the concept, precisely so the next reader can check the
premise in one grep rather than trusting the prose.

The independence question still stands and criterion 3 still plants
each half alone: a test that only ever sees both cannot tell a
conjunction from a coincidence.
