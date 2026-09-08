# Plan: next — Phase 11 item 1, staged curation stores (observations → hypotheses → validated lessons → policy) with grant-gated boundaries; workers append candidates only (os-f30ee0d3)

The build plan's Phase 11 item 1: *"Staged pipeline stores
(observations → hypotheses → validated → policy) with grant-gated
boundaries; workers append candidates only."* The charter's definition
(§12, "A poisoning-resistant pipeline"): *"Trajectories are untrusted
inputs … The pipeline is therefore staged, with distinct storage and
distinct gates between stages"*; *"Workers append candidate
observations only — a worker promoting its own single run would
violate the support rule by construction, so promotion authority is
not grantable to implementing lanes"*; *"Dead ends record failure
condition and environment."* Conformance III.K rows 1 and 2 ("online
lanes append evidence only; conclusion-writing is grant-gated to the
curator's proposal path"; "the pipeline is staged with distinct
storage and gates … no stage skips") are the targets. Items 2 through
5 (the promotion gate, the poisoning drill, expiry and rollback, the
flywheel) build on these stores.

Phase 11 depends on Phase 9 only; this plan is written against `main`
and touches nothing Phase 10's open cards touch.

## What the tree actually shows

Measured, not assumed:

- **The catalog names four `curation.*` verbs and implements none.**
  `protocol.md` lists `hypothesis.proposed`, `lesson.promoted`,
  `lesson.retired` and `deadend.recorded`; `lanes.md` records them as
  appearing in neither the affordance catalog nor the capability
  table; `transitions.json` has zero rows for them; the keyring's
  capability comment anticipates "curation-proposal rights".
- **The curator holds no grant.** `next/lanes/curator.json` declares
  `"grants": []` and `"acts_through": []`; its fragment says so is
  the design in v0. `escalation.md` records that the raise row is the
  one to revisit when the curator gains its proposal rights.
- **Stage 1 already exists, unnamed.** Every deliberate exit carries a
  packet whose `findings` are "the negative knowledge a successor must
  not rediscover: `{tried, outcome, pointer?}`" (`packets.md`), on the
  ledger, by the worker, inside its window. What it lacks is the
  charter's failure condition and environment, and a name.
- **Non-lifecycle facts fold two ways.** Facts on a contract subject
  fold onto `SubjectState` (offers, verdicts, seals); facts on their
  own subjects fold in their own package (`internal/keyring` over
  `actor.*`). The fold ignores verbs it does not know, and raw-pushed
  malformed facts count as anomalies, never as facts.
- **Grant disjointness has a precedent.** `sealer` cannot be granted
  to a key holding `claim` or `operator`, and the reverse, because
  sealed checks are authored under a grant disjoint from
  implementation (`sealerDisjoint`). The lane validator requires every
  capability the table accepts (operator excluded) to be granted by
  some shipped manifest.
- **Projections are a registered list** (`project.Default()`:
  roster, contracts, queue, actors, report, obligations, cache), each
  an immutable build behind a pointer (`projections.md`).
- **v1's memory is two append-only files** (`memory/LEARNINGS.md`,
  `memory/DEADENDS.md`), v1 surfaces this card does not touch.

## Design decisions (binding for this task)

- **D1 — four stages, three of them with ledger facts, each with its
  own gate.**

  | stage | storage | fact | who appends |
  |---|---|---|---|
  | observations | the ledger, on the contract | packet `findings` (existing) and **`deadend.recorded`** | the window's holder (`claim`), `operator` |
  | hypotheses | the ledger, on a hypothesis subject | **`hypothesis.proposed`** | **`curate`** alone, no operator fallback |
  | validated lessons | `next/knowledge/lessons/<id>.md`, merged by PR, and the ledger | **`lesson.promoted`** (the PR observation) | `observer`, `operator` |
  | policy | role, skill and workflow patches on the protected surface, by PR | none in this item | the governance root, through gates |

  `deadend.recorded` on a contract subject carries `{"fence", "tried",
  "outcome", "condition", "environment", "pointer"?}`: the packet
  finding's shape plus the charter's failure condition and
  environment, inside the holder's window (the fence matrix), a
  candidate and nothing more, since it has no field a conclusion could
  live in. `hypothesis.proposed` on subject `h-<12 hex>` (derived from
  the canonical claim text, so a re-proposal of the same claim refuses
  as a duplicate at the boundary) carries `{"claim", "applies_when",
  "support": ["<contract>@<position>", …], "exceptions": [...],
  "provenance": ["<path @ commit>", …]}`; the boundary requires
  `support` to cite at least two admitted observation facts (a packet
  finding's exit or a `deadend.recorded`) on at least two distinct
  contracts, none of which is failed (a standing fail verdict with no
  later pass), and records the citing actors. That is the structural
  minimum the charter makes non-promotable to skip; the full promotion
  gate (more than one actor where the family allows, provenance,
  last-validated, the adversarial evaluation) is item 2's, on top of
  this shape. `lesson.promoted` carries `{"lesson": "<path @
  commit>", "hypothesis": "<h-id>@<position>", "pr": "<pr @
  merged-commit>"}` and refuses unless the cited position is an
  admitted `hypothesis.proposed` on that subject: **no stage skips**,
  by citation. The lesson file's frontmatter names its hypothesis,
  applies-when, support, provenance, `last-validated` and `expires`
  (the fields item 4 reads); item 1 lints the frontmatter's presence,
  item 2 its content.

  Refused: a lessons store in the ledger. A lesson is a document
  humans and lanes read, it changes behavior when it lands, and the
  charter routes promotion through a PR precisely so that rollback is
  a revert; the ledger carries the observation of that merge, the
  `plan.approved` posture. Refused: `lesson.retired` here; retirement
  and expiry are item 4, and a verb with no reader is a verb nobody
  drills.

- **D2 — `curate` is the proposal grant, disjoint from
  implementation, with no operator fallback.** A new capability
  `curate` accepts `hypothesis.proposed`, and nothing else does: the
  fifth no-fallback row, beside `verdict.rendered`, `check.sealed`,
  `decision.recorded` and `merge.overridden`. Governance roots hold
  `operator` implicitly, and `operator` already reaches `claim.taken`,
  `claim.parked` and `deadend.recorded`, so an operator fallback on
  the proposal would let one key write a trajectory's observations and
  then conclude from them without ever holding two conflicting grants;
  the poisoning boundary is real only if the proposal is reachable
  through `curate` alone (review finding on #226). `curate` cannot be
  granted to a key holding `claim` or `operator` (a root's implicit
  operator standing included), nor `claim` or `operator` to a key
  holding `curate`: the `sealerDisjoint` rule one capability over,
  against both lanes it names. A worker promoting its own runs, and a
  root concluding from its own, are refused at the grant, not at the
  proposal. The curator manifest gains `curate`, its summary changes
  from "holds no ledger-writing grant" to "proposes hypotheses and
  promotes nothing", and the lane audit that requires every accepted
  capability to be granted by a shipped manifest stays green by that
  edit.

  **The raise row gains `curate`.** `escalation.raised` accepts
  `claim`, `dispatch`, `verdict`, `supervise`, `operator` and now
  `curate`: the charter says every lane can raise `blocked(needs-you)`,
  and `escalation.md` reserved this row for exactly the moment the
  curator gained a proposal grant. Breadth stays safe by the
  `offer.published` argument (raising grants nothing, and a raised
  contract leaves `blocked` only through the operator), so the
  curator's residual is the one every raiser already has: freezing,
  attributable, reversible. The affordance catalog's raise probe and
  the injection suite's reachable sets carry the sixth capability.

- **D3 — one fold and one projection.** `internal/curation` folds the
  three facts (dead ends by contract; hypotheses by id with claim,
  support, proposer and position; lessons by path with hypothesis, PR
  and position), tolerantly, counting a malformed raw fact as an
  anomaly and folding a lesson that cites no admitted hypothesis as
  `unbound` rather than as a lesson. A registered **`knowledge`**
  projection (`knowledge.json`) renders the stages and each
  hypothesis's stage (`proposed`, `promoted`); `contested` and
  `retired` arrive with items 2 and 4. The report gains a knowledge
  section that counts the stages.

- **D4 — the CLI is `seed knowledge`.** `seed knowledge deadend
  --subject <id> --tried … --outcome … --condition … --environment …
  [--pointer <anchor>]` (the holder, deriving the fence like the loop
  verbs); `seed knowledge propose --claim … --applies-when … --support
  <contract@position>… --provenance <anchor>… [--exception …]` (the
  curator); `seed knowledge promote --lesson <path @ commit>
  --hypothesis <h@position> --pr <pr @ commit>` (the observer); `seed
  knowledge show (--ledger|--remote)` renders the fold. No loop act:
  the worker loop's exits already carry findings, and a standalone
  dead end is the holder's deliberate extra, like `plan propose`.

- **D5 — no version bump, additive catalog growth.** The three verbs
  are catalog growth under an existing namespace with the
  `offer.published` precedent: an older validator refuses the unknown
  verb safely, admission policy shapes them, and every existing chain
  verifies byte for byte.

- **D6 — scope guard.** No promotion gate beyond the structural
  minimum, no adversarial evaluation, no contested state (item 2); no
  poisoning drill beyond the single-contract refusal this item's shape
  gives for free (item 3); no expiry, retirement or rollback (item 4);
  no flywheel (item 5); no surfacing of lessons in packets or envelopes
  at claim time (item 2's routed III.I row 5); no policy-stage fact; no
  change to the escalation channel beyond the raise row's sixth
  capability, none to `transitions.json` or to the v1 memory files.

## Steps

1. `next/internal/curation/` (new) — the three payload shapes, the
   hypothesis id derivation, the fold, `unbound`.
2. `next/internal/keyring/` — `CapCurate`, the accepted-capability rows
   for the three verbs (the proposal's `curate` alone), the raise
   row's sixth capability, `curateDisjoint` against `claim` and
   `operator` in both directions.
3. `next/internal/admit/` — the `curation` rule (window and fence for
   dead ends; support citations, distinct non-failed contracts and the
   duplicate for hypotheses; the cited hypothesis for promotions);
   affordance probes for the three verbs and the raise probe from a
   `curate` key.
4. `next/internal/project/` — the `knowledge` projection and the
   report's section; `next/spec/projections.md`.
5. `next/cmd/seed/knowledge.go` (new) — the four subverbs; `main.go`.
6. `next/lanes/curator.json` and `fragments/lane/curator.md` — the
   grant and the posture; `next/lanes/observer.json` — the summary
   names the promotion observation.
7. `next/knowledge/lessons/README.md` (new) — the frontmatter contract
   and an empty store; the frontmatter lint.
8. Drills: curation (shapes, ids, the fold, anomalies, `unbound`),
   keyring (the rows, the disjointness both ways), admit (every D1 and
   D2 refusal row; a single-contract hypothesis refuses; a hypothesis
   citing a failed contract refuses; a promotion citing no hypothesis
   refuses; the dispatcher cannot reach the verbs), project (the
   projection and the section), `cmd/seed` (the four subverbs against a
   real ledger, the dead end inside and outside a window, the curator
   proposing from two contracts, the observer promoting), the lane
   audit (the curator's grant), the injection suite's reachable set.
9. Specs: new `next/spec/curation.md`; `actors.md` (the capability and
   its rows, the disjointness), `lanes.md` (the curator's grant, the
   vocabulary edge closed for `curation.*`), `packets.md` (findings are
   stage-one observations), `protocol.md`, `projections.md`,
   `escalation.md` (the raise row gains `curate`; the reserved
   paragraph is answered).
10. `next/docs/progress.md` (Phase 11 opened), `next/docs/decisions.md`,
    `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/curation/**` (new), `next/internal/keyring/**`,
  `next/internal/admit/**`, `next/internal/project/**`,
  `next/cmd/seed/knowledge.go` (new), `next/cmd/seed/main.go` and the
  drills, `next/lanes/curator.json`, `next/lanes/observer.json`,
  `next/lanes/fragments/lane/curator.md`, `next/knowledge/**` (new)
- `next/spec/curation.md` (new), `next/spec/actors.md`,
  `next/spec/lanes.md`, `next/spec/packets.md`, `next/spec/protocol.md`,
  `next/spec/projections.md`, `next/spec/escalation.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-f30ee0d3.json`

Nothing outside `next/**` except the work-product files above. NOT
`next/spec/transitions.json`, NOT `next/internal/transition/**` beyond
reading the fold, NOT `next/internal/version/**`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. `deadend.recorded` admits from the window's holder citing the active
   fence with all five fields non-empty and an optional anchored
   pointer; it refuses outside a window, from a non-holder, with a
   stale fence, with a missing field, or with a bare-path pointer,
   each naming the part; a `dispatch`-only key cannot reach it.
2. `hypothesis.proposed` admits from a `curate` key on subject
   `h-<12 hex>` derived from the claim, citing two admitted
   observations on two distinct non-failed contracts; it refuses from
   a `claim` key (`out_of_grant`), on a subject not derived from the
   claim, citing one contract, citing two facts on one contract, citing
   a position that is no observation, citing a failed contract, and as
   a duplicate of an admitted claim; the accepted set is `[curate]`
   alone: a governance root's implicit operator standing does not
   reach it and a root refuses `out_of_grant`.
3. `curate` refuses to be granted to a key holding `claim` or
   `operator` (a root included), and `claim` or `operator` to a key
   holding `curate`, each as chain validity at the position; the
   curator manifest grants `curate` and the lane audit is green.
4. `lesson.promoted` admits from an `observer` key citing an admitted
   `hypothesis.proposed` by subject and position with an anchored
   lesson path and a `pr @ commit`; it refuses citing a position that is
   no hypothesis, a mismatched subject, a bare path, or from a `curate`
   key; a raw-pushed promotion citing no hypothesis folds `unbound`.
5. `escalation.raised` admits from a `curate` key on a `ready` or
   `review` contract with the packet and the escalation, and refuses
   from it elsewhere exactly as from any raiser; the affordance
   catalog carries the row, and the injection suite's reachable set
   for the curator is `hypothesis.proposed` and `escalation.raised`,
   nothing else.
6. The fold renders dead ends by contract, hypotheses with their stage,
   and lessons; a malformed raw fact counts an anomaly; the `knowledge`
   projection publishes it and the report's section counts the stages.
7. `seed knowledge deadend|propose|promote|show` drive the three facts
   against a real ledger with the fence and the id derived, refusing at
   usage what the boundary would refuse.
8. **Mutation evidence.** Each must fail a drill: the support minimum
   read as one; the distinct-contract check dropped; the failed-contract
   check dropped; the promotion admitted with no cited hypothesis; an
   operator fallback restored on the proposal row; the disjointness
   dropped in either direction or against `operator`; the raise row
   without `curate`; the dead end admitted outside the window; the fold
   reading a lifecycle verb as a curation fact; the projection omitting
   a stage.
9. `make check` green with coverage measured **cold**, at least three
   readings above the gate, and the suites pass **unprivileged** under
   `setpriv --reuid=65534 --regid=65534 --clear-groups`.

**Retention set (existing, shown unharmed):**

- Every pre-existing fixture chain verifies byte for byte; no version
  bumps; no transition row moves; the four deliberate exits and their
  packets are unchanged.
- The escalation raise row's existing five capabilities keep their
  drills and every other row of the channel is unchanged; the
  injection suite's reachable set for the dispatcher gains nothing.
- The existing projections' builds are byte-identical on chains that
  carry no curation fact; the lane audit's existing rows stay green.
- The v1 memory files are untouched.

## Validation Commands

- Boundary: `cd next && go test ./internal/curation/ ./internal/keyring/ ./internal/admit/ ./internal/project/ ./internal/lane/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

One new package with three payload shapes and a fold; one capability
with three accepted-capability rows (the proposal's `curate` alone),
the raise row widened by one, and one disjointness rule against `claim`
and `operator`; one
admission rule with three arms and three affordance probes; one
projection and one report section; one CLI verb group with four
subverbs; the curator's grant and fragment; an empty lessons store with
its README. Specs: one new file and six edits. No new exit, version or
transition row; no `plans/**` in the task PR.
