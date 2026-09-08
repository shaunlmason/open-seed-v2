# Plan: next — the worker loop made executable, and its liveness proven (os-abb206c8)

Phase 9 item 1, third of three cards (1a landed as #188; 1b, the
dispatcher's injection corpus, is still uncarded). This is the one
promotion criterion 1 actually needs:

> A lane runs poll → claim → plan-gate → work → meter → submit →
> verdict → merge-observe → deliberate exit, plus escalation and
> messages, **entirely through Seed verbs**, orienting from one
> position-stamped read rather than hand-assembling ledger payloads
> and hand-computing fences.

A lane that cannot run that loop cannot run unattended, whatever the
conformance report says.

## What 1a proved, and the half it could not

1a made every lane's obligations **declared fields** checked against
tables that already exist. It closed three of the four obligations
honestly and stopped short on the third, which is recorded in
`next/spec/lanes.md` and `next/docs/progress.md` rather than papered
over:

> A subset check compares two labels. It cannot show the named step
> actually emits, because nothing executes here: manifests are data,
> and the loop that emits is Phase 9 item 1c's.

So this card inherits a specific debt. It is not "add drills"; it is
**make the declaration true and then prove it**, which is only
possible once something runs.

## Design decisions (binding for this task)

- **D1 — the loop is a LIBRARY, not a CLI verb.** `next/internal/loop`
  exposes a driver whose *work* step is supplied by the caller. Seed
  does not own the work: writing the code is the model's act, and a
  loop that tried to own it would either embed a model or pretend the
  work is a subprocess call.

  A `seed loop run` verb is **deliberately deferred and named here**.
  It would invite treating the CLI as the agent, and the real consumer
  of this loop is item 4's small-team and fleet fixtures, which drive
  it in CI with no model and no wake channel. A library is what those
  fixtures can drive; a verb is sugar that can land later, on evidence,
  once something outside a test wants it.

- **D2 — the loop reads its own manifest and acts only through the
  acts that manifest declares.** This is the close of 1a's loop, and
  it is the decision that makes this card more than plumbing.

  `internal/lane` already holds, for the implementer lane, an
  `acts_through` list and a `liveness_from` list. The loop resolves
  its manifest at construction and refuses — as a programming error,
  loudly, before anything is signed — to perform an act the manifest
  does not declare. Two things follow that were previously hopes:

  1. "The manifest describes the loop" becomes **enforced** rather
     than coincidental. Editing the manifest without editing the loop,
     or the reverse, fails.
  2. The liveness obligation gains its missing half: the steps that
     emit are exactly the steps `liveness_from` names, because the
     loop reads that list rather than carrying its own.

- **D3 — poll is `offer list`, orient is `situation`, and both must
  learn the remote posture, which is a scope increase this plan takes
  deliberately.** `cmd/seed/offer.go` already calls itself "the
  worker's poll": eligibility-scoped, expiring invitations with
  liveness derived and never stored. The loop consumes it for *what may
  I claim*, and the single position-stamped read for *what is true for
  me now*, carrying the position forward as `--since`. No third source
  of truth, and no projection read by hand.

  **The posture does not currently line up, and the first draft of this
  plan did not notice** (review finding on this PR). The loop verbs take
  `--ledger` XOR `--remote`, and `runClaimTake` refuses `--ledger`
  outright — `claim.taken` is the one exclusive act, and only the push
  round-trip can order two rivals. But `offer list` and `situation` bind
  `--ledger` alone. So in the only posture where the loop can claim,
  the loop can neither poll nor orient: it would read a local copy that
  nothing refreshes and call its position authoritative.

  Two ways out, and the choice matters. The loop is a library (D1), so
  it *could* read the remote-materialized store through internal
  packages and skip the CLI. **It must not**, because D2 says the loop
  acts only through what its manifest declares, and every manifest
  declares `seed situation …` as its orienting read. A loop that
  orients by calling internal code makes that declaration a fiction and
  quietly reopens the drift 1a closed.

  So the surfaces learn the posture: `offer list` and `situation` gain
  `--remote`/`--ref` on the same XOR the loop verbs already use,
  reusing `openRemoteSession` rather than growing a second remote path.
  `lane.SituationFlags` gains `remote` and its required-ness becomes the
  XOR rather than a single required flag, and the shipped manifests'
  `orients_from` says which posture they orient in. This widens the file
  scope into `cmd/seed` and `next/lanes/**`; the alternative is a drill
  that cannot establish the loop it claims to prove.

- **D4 — exhaustion parking is refusal-driven, and the worker's
  exhaustion point is `budget.reserve`, NOT the spending gate.** The
  first draft said "a budget refusal at a spending gate", echoing the
  build plan's phrase without checking which gate a worker can actually
  reach (review finding on this PR). It cannot reach that one:

  - The literal spending gate is `transition.IsSpendingVerb`, whose
    table holds exactly `run.started`, and `run.started` is admitted
    from `{supervise, operator}` (`next/spec/executors.md`, step 2).
    The implementer holds `claim`. The gate is the **executor's**, and
    no key this loop signs with can trip it.
  - What the worker reaches is `budget.reserve` refusing on capacity:
    `amount N exceeds remaining M of capacity C` — the same
    `admit.BudgetError` type, raised at the one place capacity is
    "checked and decremented at admission".

  So the build plan's phrase names the *concept* — the point where the
  loop is told it cannot spend — and for the worker lane that point is
  the reserve. This is recorded as a decision rather than smoothed
  over, because the distinction is exactly what makes the drill
  non-vacuous: `InjectBudgetClass` shrinks the class capacity and
  therefore produces a **reservation-capacity** refusal, and a plan that
  called that "the spending gate" would ship a green drill for a path
  it never touched.

  The exit is `claim park` with its four-part packet (the III.H row the
  Phase 7 exit routes here, consuming Phase 8's envelope budget block).
  The packet's findings carry the refusal's **`code` and `message`
  verbatim**, matching what the build plan binds on escalation: the
  next worker, or the human, is given the boundary's own account rather
  than the lane's paraphrase of it. Acceptance comes from the
  contract's spec; base is the resume range the loop verbs already
  derive, or the zero-length range when nothing was pushed, which is
  the shape the force-preemption reap already uses.

- **D5 — liveness is PROVEN, not declared, and this is the inherited
  obligation.** Two drills, and neither is satisfiable by a label
  comparison:

  1. Running the loop's declared liveness steps **advances the
     observation stream keyed to the lane's actor and fence** — the
     same stream `internal/obs` classifies as live, expired or wedged.
     Asserted by sampling the stream before and after, keyed exactly
     as the classifier keys it, so a stream written under the wrong
     actor or fence fails.
  2. The loop **reaches no liveness-only surface**. Checked as a
     property rather than a spelling: the loop's act set is its
     manifest's `acts_through` (D2), and every emission is a
     side-effect of one of those acts, so there is no path on which
     the loop emits without working. A drill asserts the loop never
     calls the observation surface outside a declared act.

- **D6 — no model, and the drills say so.** The work step in every
  drill is a deterministic function. The established pattern for
  driving a real worker in CI is the test-binary re-exec already used
  by `cmd/seed/preempt_cli_test.go`, and this card reuses it rather
  than inventing a harness. Where a drill needs the loop to fail a
  spending gate, it injects a budget class small enough to exhaust
  (`transition.InjectBudgetClass`, as the budget drills do).

- **D7 — the loop never widens its own authority.** It signs with one
  key, holds whatever grants that key holds, and every act goes
  through the loop verbs' pre-flight. It does not fall back to
  `ledger append` when a loop verb refuses; a refusal it cannot act on
  is escalation's business (item 2) and, until that lands, a returned
  error naming the refusal. The one-inbox doctrine holds: the loop
  acts on its read, never on a wake.

- **D8 — scope guard, as widened by D3.** No new ledger verb, no new
  projection, no admission change, no change to the loop verbs'
  derivations, and no new remote machinery — `--remote` on the two read
  surfaces reuses `openRemoteSession` exactly as the loop verbs do. No
  escalation (item 2), no maintenance (item 3), no fixtures (item 4),
  no injection corpus (1b). This card makes one lane's loop run and
  proves what 1a could not.

  The widening is bounded and stated: two commands gain a flag pair
  they should always have had, and `internal/lane` follows because it
  validates the read those commands expose. Nothing else in `cmd/seed`
  is touched.

## Steps

1. The remote read posture (D3): `--remote`/`--ref` on `offer list`
   and `situation` under the loop verbs' existing XOR, `SituationFlags`
   updated to the XOR shape, and the shipped manifests' `orients_from`
   naming the posture they orient in. This lands FIRST: the loop cannot
   be drilled until its poll and orient run where its claim does.
2. `next/internal/loop` — the driver: construction from a lane
   manifest and a key, the step sequence (poll, orient, claim, work,
   meter, submit or park), and the act gate of D2. The work step is an
   interface the caller implements.
3. Exhaustion parking (D4): the `budget.reserve` capacity-refusal path,
   the packet assembled from what is known, and the `claim park` exit
   through the loop verb.
4. Drills (`internal/loop`): the happy path end to end against a real
   ledger in the remote posture; the exhaustion path parking with the
   refusal in its findings; an act outside the manifest refused; and
   D5's two liveness drills.
5. `next/spec/lanes.md`, `next/spec/loop-verbs.md` and `next/spec/offers.md`
   — what the loop is, that it reads its manifest, that liveness is now
   proven rather than declared, and the read surfaces' posture; strike
   the "1c inherits" note in `lanes.md` and replace it with what was
   done.
6. `next/docs/progress.md`, `next/docs/decisions.md`, memory; receipt;
   evidence; review.

## File Scope

- `next/internal/loop/*.go` and its drills
- `next/cmd/seed/offer.go`, `next/cmd/seed/situation.go` — `--remote`
  and `--ref` under the existing XOR (D3), plus their CLI drills
- `next/internal/lane/lane.go` — `SituationFlags` gains `remote` and
  the required-ness becomes the XOR; plus any accessor the loop needs
- `next/lanes/*.json` — `orients_from` names the posture
- `next/spec/lanes.md`, `next/spec/loop-verbs.md`, `next/spec/offers.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-abb206c8.json`

## Acceptance Criteria

1. A worker loop runs poll → orient → claim → work → meter → submit
   against a real ledger **in one posture end to end**, entirely
   through the item 5(c) loop verbs and `offer list`, with no
   `ledger append` on any path and no local-copy read standing in for
   the ledger the claim lands against.
2. The loop orients from ONE position-stamped read and carries the
   position forward as `--since`.
3. `offer list` and `situation` accept `--remote`/`--ref` under the
   same XOR the loop verbs use, refusing both-or-neither as `usage`;
   `lane.SituationFlags` describes that XOR, pinned to the CLI by the
   drill 1a established, so the two cannot drift.
4. The loop performs no act its lane manifest does not declare in
   `acts_through`, and a drill proves the gate by attempting one.
5. A `budget.reserve` capacity refusal parks the claim with a packet
   whose findings carry the refusal's `code` and `message` verbatim;
   the subject lands in `blocked` and the window closes deliberately.
   The drill asserts the refusal it exercises is the capacity one, by
   its message, so it cannot silently become a different refusal.
6. The spec records that the `run.started` spending gate is the
   executor's and unreachable by a `claim`-capability loop, so the
   distinction survives this card rather than living in a PR thread.
7. Running the loop advances the observation stream **keyed to the
   lane's actor and fence**, asserted by sampling that exact stream;
   and the loop reaches no liveness-only surface, asserted as a
   property of its act set rather than as a spelling.
8. `next/spec/lanes.md`'s "what this does not establish" note is
   replaced by what this card established; no obligation is left
   asserted-but-unproven without naming who inherits it.
9. No new verb, projection, or admission change; no `seed loop run`
   verb (deferred by D1 and named in the spec).
10. `make check` green, coverage gate ≥90% held.

## Validation Commands

```sh
cd next && gofmt -l . && go vet ./... && go build ./...
cd next && go test ./internal/loop/ ./internal/lane/ ./cmd/seed/ -count=1
cd next && go test ./internal/loop/ -count=5
make check
```

## Expected diff shape

One new internal package with its driver and drills, a flag pair added
to two existing commands, three spec passages, and the work-product
files. Roughly +850/-60 lines, all under `next/**`. The edits to
existing code are the remote posture on the two read surfaces (D3),
`SituationFlags` following it, the manifests' declared read, and the
spec notes.

## A risk worth naming now

The loop is the first thing in Seed that RUNS unattended, and its
drills are the first that could hang rather than fail. Every wait in
them polls a condition against a bounded deadline — the pattern
`os-a95db3f5` established for the preemption drills — and no drill
stands a fixed sleep in for "the worker got there". A loop drill that
sleeps is a loop drill that will flake on a loaded runner, and this
repository has now paid that cost twice.
