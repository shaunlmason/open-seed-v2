# Plan: next — III.F row 12 dependency and hierarchy derivations (os-f0ae2cdf)

Part III.F row 12 is not owned by the Phase 13 build list. The tree
currently has the lifecycle table, ready queue, offer polling, and the
wakeless poll-only run from #145, but it has no dependency or hierarchy
facts from which to derive a cascade, no inherited-hold calculation, no
initiative rollup, and no goal-ancestry warning. Consequently the row is
`routed` forever and Part III cannot become complete. This task closes the
whole row as one coherent graph-derived feature; it does not reinterpret
Phase 7's wake as a correctness path.

Design authority is `SEED-NEXT.md` Part II sections 4, 7, and 9 and Part
III.F row 12. The transition table remains the sole authority for lifecycle
legality and terminality. Graph state is folded from ledger facts, while
cascades, inherited holds, rollups, and warnings are projections: no
dependent or descendant is mutated to simulate a derived state.

## Design decisions (binding for this task)

- **D1 — one implementation task PR, no child cards.** The four missing
  clauses share one relation fold and one effective-readiness calculation.
  Splitting them would either duplicate that authority or leave F.12
  knowingly unflippable between child merges. After this plan merges,
  `os-f0ae2cdf` is implemented in one `seed/os-f0ae2cdf` task PR; this plan
  does not create child cards.
- **D2 — additive relation facts, not lifecycle transitions.** Add four
  dispatch/operator-authored facts, all on an existing nonterminal contract:
  `dependency.linked` and `dependency.unlinked`, each with strict payload
  `{"requires":"<subject>"}`; `hierarchy.parented` with
  `{"parent":"<subject>"}`; and `goal.aligned` with
  `{"mission":"<path> @ <commit>"}`. A repeated `hierarchy.parented`
  replaces the current parent. A repeated `goal.aligned` replaces the
  current mission anchor. Dependency links are a set: duplicate links and
  unlinking an absent edge refuse. Sources and referenced contract subjects
  must exist; self edges and dependency or parent cycles refuse against the
  candidate graph. The mission is a commit-anchored artifact reference, not
  prose in the immutable ledger. These are additive catalog verbs that an
  older validator safely refuses as unknown, so the protocol's stated
  additive-growth rule applies and there is no `seed/8` bump. None belongs
  in `next/spec/transitions.json`.
- **D3 — one trustworthy graph fold.** A new `next/internal/topology`
  package folds the active dependency set, latest parent, and latest mission
  anchor from a verified record prefix. It accepts the parsed transition
  table rather than copying state names, and replays the keyring to each
  relation fact's own position before trusting it. Thus a raw-pushed,
  malformed, out-of-grant, missing-target, or cyclic relation is inert and
  surfaced as a topology anomaly; it cannot hide work, block a claim, or
  forge mission ancestry. Admission and every consuming projection use this
  same fold. `next/internal/project/topology.go` is the projection adapter:
  queue, contracts, report, and cache consume its one deterministic derived
  view rather than independently walking edges.
- **D4 — effective readiness is derived; lifecycle state is retained.** A
  subject is effectively ready exactly when its transition-table fold is
  `ready`, every active dependency is in a state the parsed table marks
  terminal, and no ancestor's folded lifecycle state is `blocked`.
  `claim.taken` admission, `queue.json`, and `offer list` all consume this
  predicate. Closing a dependency (`done` or `cancelled`, because the table
  marks both terminal) therefore makes dependents visible without appending
  `contract.unblocked`; removing the last dependency does likewise. Existing
  offer facts may remain stored while a subject is ineffective, but listing
  hides them until the subject becomes effectively ready. Relation-free
  contracts retain today's claim, queue, and offer behavior.
- **D5 — holds cascade without rewriting descendants.** Every ancestor in
  lifecycle state `blocked` is an inherited hold, regardless of whether it
  arrived through `contract.blocked`, `claim.parked`,
  `escalation.raised`, or another table-defined route. The descendant keeps
  its own base lifecycle state, while the derived view names all `held_by`
  ancestors and excludes the descendant from claim admission, the ready
  queue, live offer polling, and wake candidates. When the last ancestor
  leaves `blocked`, otherwise-eligible descendants become effectively ready
  at that event's position. The transition table and its generated lifecycle
  document are not edited.
- **D6 — wakes accelerate the same delta and never decide it.** Reuse
  `executor.Adapter.Wake(actor)` and #145's `TestWakelessPollOnlyRun`; do not
  invent an assignment, inbound executor connection, or durable wake fact.
  A prefix-to-prefix readiness delta identifies subjects that became
  effectively ready. The supervisor bridge matches their live offers with
  active eligible actors using the same extracted offer-eligibility helper
  as `offer list`, and calls `Wake(actor)` only for actors whose registered
  adapter has a channel. No registered channel is a no-op; a wake error is
  reported to the caller but never rolls back, blocks, or alters the queue.
  A subject still under an inherited hold produces no wake candidate. The
  supervisor supplies its last observed position as the delta cursor; wakes
  are allowed to repeat after caller retry because they are advisory, while
  readiness remains idempotent and ledger-derived.
- **D7 — initiative and goal meaning.** Any contract with direct children is
  an initiative. Its rollup covers all transitive descendants (not the
  initiative itself) and renders exact integer counts by lifecycle state,
  terminal, effectively ready, dependency-waiting, and held, plus the sum of
  descendants' current admitted `progress.milestone` high-water counts; no
  percentage or wall-clock estimate is invented. Goal ancestry for an open
  (nonterminal) contract walks self then parents to the first valid
  `goal.aligned` mission anchor. No anchor yields a warning naming the
  subject and traversed ancestry; terminal work is omitted. A malformed or
  unauthorized raw fact cannot satisfy the walk. The mission anchor is
  rendered on the initiative that carries it, so the warning is falsifiable
  from the same view.
- **D8 — rendered surfaces and row closure.** Contracts add relation facts,
  unresolved dependencies, `effective_ready`, `held_by`, mission ancestry,
  and an initiative rollup where applicable. The report adds deterministic
  `initiatives`, `goal_ancestry_warnings`, and topology-anomaly sections.
  The queue derivation/version advances, and the SQLite cache mirrors the
  relation, effective-readiness, rollup, and warning fields/tables; all
  changed projection versions and the cache schema generation advance.
  `next/spec/conformance.json` F.12 becomes `met` only in the same
  implementation PR that lands every named drill below; its evidence cites
  that PR and #145, and the generated conformance page is regenerated.

## Steps

1. **Specify and author the facts.** Add `next/spec/topology.md`; extend the
   protocol and capability tables; add the four fact payload/admission rules,
   dispatch/operator capability rows, topology CLI verbs (`seed topology
   depend`, `undepend`, `parent`, and `align`), registry/machine-surface
   coverage, and the dispatcher manifest act set. The CLI appends through the
   ordinary checked admission path, never through a topology side store.
2. **Build the shared derivation.** Implement the tolerant, position-accurate
   relation fold and graph validation in `next/internal/topology`; expose the
   effective-readiness, inherited-hold, ancestry, initiative-rollup, anomaly,
   and prefix-delta results. Wire claim admission to the effective predicate
   while retaining transition-table state and error authority.
3. **Connect polling and advisory wakes.** Move offer eligibility out of
   `cmd/seed` into one shared helper, filter `offer list` with effective
   readiness, and add the supervisor bridge over readiness deltas and the
   existing adapter `Wake` seam. Preserve the no-channel path exactly.
4. **Render the graph.** Integrate the derivation into contracts, queue,
   report, and cache; bump their derivation/schema versions and update
   deterministic fixtures. Document every field and the fact that derived
   holds and dependency waits never mutate lifecycle state.
5. **Run the named drills.** Add the relation-boundary, dependency cascade,
   hold-suppression, initiative-rollup, and goal-ancestry drills below. Keep
   #145's wakeless test as retained evidence rather than rewriting it.
6. **Close the row.** Flip only III.F row 12 to `met`, regenerate generated
   docs, and record the decision/progress/evidence/receipt required by the
   normal task workflow. Do not add a Phase 13 catch-all or edit the charter.

## Named conformance drills

- **`TestDependencyCascadeWakesAndPolls` (III.F.12, dependency half):** a
  ready dependent with a live offer is absent from queue and offer polling
  while one required contract is open; the required contract reaches a
  table-terminal state; the dependent appears at that exact prefix, its
  claim admits, a recording wake channel is called for the eligible actor,
  and the same run with no channel reaches the claim by polling. A second
  dependency keeps it out until all requirements are terminal. This builds
  on, and does not replace, #145's full wakeless run.
- **`TestHoldCascadeSuppressesWakeUntilReleased` (III.F.12, hold half):** in
  a three-level tree, blocking the root makes both otherwise-ready
  descendants name the root in `held_by` and disappear from claims, queue,
  and offers. Closing a dependency while the hold remains emits no wake.
  Unblocking the root exposes each eligible descendant and invokes one
  recording wake; their base lifecycle states never changed.
- **`TestInitiativeRollupRendersDescendants` (III.F.12, rollup half):** a
  two-level initiative with ready, held, dependency-waiting, done, and
  cancelled descendants plus milestone facts renders the exact transitive
  state counts and milestone sum in contracts, report, and cache. Rebuilding
  the same prefix is byte-identical and the initiative itself is not counted
  as its descendant.
- **`TestGoalAncestryWarnsOnlyOpenUnanchoredWork` (III.F.12, ancestry half):**
  an open child inheriting a commit-anchored mission from an ancestor does
  not warn; an open orphan and a tree with no aligned ancestor do warn with
  their traversed chains; terminal unanchored work does not warn; a
  raw-pushed out-of-grant or cyclic relation stays anomalous and cannot
  silence the warning.
- **`TestTopologyRelationBoundary` (supporting boundary):** each fact admits
  for dispatch and operator and refuses a plain worker, unknown or terminal
  source, unknown target, self/cycle, malformed or unknown payload fields,
  duplicate link, and absent unlink. A claim drafted directly against an
  unresolved dependency or inherited hold refuses just as queue/list hide
  it.

## File Scope

- `next/spec/topology.md` (new), `next/spec/protocol.md`,
  `next/spec/actors.md`, `next/spec/offers.md`,
  `next/spec/projections.md`, `next/spec/lifecycle.md`
- `next/internal/topology/**` (new), `next/internal/keyring/**`,
  `next/internal/admit/**`, `next/internal/transition/**`
- `next/internal/project/topology.go` (new), and targeted queue, contracts,
  report, cache builders plus their fixtures/tests
- the shared offer-eligibility/supervisor-wake helper and tests under
  `next/internal/**`; existing `next/executor` adapter tests only as needed
  to exercise the already-public `Wake` seam
- `next/cmd/seed/topology.go` (new), `offer.go`, registry wiring, and focused
  CLI/conformance tests; `next/lanes/dispatcher.json`
- `next/spec/conformance.json`, `next/docs/generated/conformance.md`,
  `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`, and
  `receipts/os-f0ae2cdf.json`

Explicitly out of scope: `SEED-NEXT.md`, `docs/next-build-plan.md`,
`next/spec/transitions.json`, any v1 task/backend surface, a new wake
transport, and child cards.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. The four relation facts have strict, grant-checked admission and the raw-
   push laundering drill proves an unauthorized relation is inert at every
   consuming surface.
2. Closing the last dependency changes effective readiness without writing a
   synthetic lifecycle event; claim admission, queue, offer polling, and the
   wake delta agree at the same ledger prefix. A missing wake channel or a
   failed wake loses only latency.
3. A blocked ancestor suppresses descendant claims, queue rows, live offers,
   and wake candidates; lifting the final hold exposes the work without ever
   changing a descendant's folded lifecycle state.
4. Every initiative renders exact transitive rollups in contracts, report,
   and cache, and every open subject without a valid mission-bearing ancestor
   renders a goal-ancestry warning. The named drills fail if either surface is
   omitted or independently re-derived.
5. Projection builds remain deterministic, stamped, rebuildable, and read-
   only. F.12 is `met`, the rendered conformance table matches its source,
   and its evidence names all four drills plus #145's wakeless run.

**Retention set (existing, shown unharmed):**

- `next/spec/transitions.json` remains byte-identical; lifecycle legality,
  terminal flags, base folded states, and every deliberate-exit/fence drill
  continue to come from the table.
- `TestWakelessPollOnlyRun` passes unchanged. Independent contracts (no
  active dependencies and no blocked ancestor) retain today's claim, queue,
  offer, race, budget, and reconciliation behavior, and no wake becomes an
  assignment or admission grant.
- Existing `seed/0` through `seed/7` histories verify byte-for-byte; no
  protocol bump, canonical event form, envelope code, or unrelated
  projection changes.
- The projection registry's rebuild-twice, read-only publication, cache
  parity, and minimum-position drills remain green; `make check` retains the
  repository-wide coverage and generated-doc gates.
- No v1 surface and no `plans/**` file changes in the implementation PR.

## Validation Commands

- Boundary: `cd next && go test ./internal/topology/ ./internal/admit/ ./internal/project/ ./cmd/seed/ -run 'DependencyCascade|HoldCascade|InitiativeRollup|GoalAncestry|TopologyRelationBoundary' -count=1`
- Boundary (wake/poll): `cd next && go test ./cmd/seed/ ./executor/ -run 'DependencyCascadeWakesAndPolls|HoldCascadeSuppressesWakeUntilReleased|WakelessPollOnly|Wake' -count=1`
- Boundary (row/docs): `cd next && go test ./internal/conformance/ ./internal/docs/ ./cmd/seed/ -run 'Conformance|Charter|Docs' -count=1`
- Retention: `cd next && go test ./internal/transition/ ./internal/project/ ./internal/admit/ ./cmd/seed/ ./executor/ -run 'Lifecycle|Transition|Queue|Offer|Claim|Projection|Cache|Adapter|Wakeless' -count=1`
- Retention: `make check`

## Expected diff shape

One new topology spec, one new pure graph package, one small projection
adapter, one CLI file, and focused drill files; targeted additions to the
keyring/admission tables, offer eligibility, the four projection builders,
cache schema, dispatcher manifest, and their tests/fixtures; one conformance
row plus regenerated/record artifacts. Approximately 20–30 implementation
files and 700–1,100 net new lines, with no transition-table, charter,
build-plan, v1, or plan-file edit in the task PR.
