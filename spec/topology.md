# Topology: dependencies, hierarchy and goals beside the lifecycle

> Design authority: SEED-NEXT.md §II.4 (contracts), §II.7 (dependencies
> cascade; blocked descendants are held), §II.9 (offers and claims, wakes
> advisory) and Part III.F row 12 ("Dependencies cascade, with advisory
> wakes where a channel exists (polling remains the correctness path,
> consistent with the wakeless run in H); holds cascade with
> suppression; initiative rollups render; goal ancestry warns"). Plan
> `plans/os-f0ae2cdf.md`. Implemented by `internal/topology` (the
> facts, the fold, the graph rule, the derivations), the admission
> rule `topology` and the claim boundary's read (`admit.Topology`),
> `internal/offers` (eligibility, the live poll, the wake bridge),
> `internal/project/topology.go` (the projection adapter), `seed
> topology` and `seed offer wake`.

The transition table stays the sole authority on lifecycle legality and
terminality ([`lifecycle.md`](lifecycle.md)); nothing in this document
moves a subject. What a contract requires, what it belongs under, and
the mission it serves are **facts beside the lifecycle**, folded into a
graph, and everything the row asks for is **derived** from that graph
and the lifecycle fold at a prefix: a dependent that waits or a
descendant that is held keeps its own base state and simply is not
*effectively ready*; closing the last dependency or lifting the last
hold exposes the work at that event's position with no synthetic
lifecycle event written. `spec/transitions.json` is untouched.

## The four facts

Four verbs, additive catalog growth active from `seed/1`
([`protocol.md`](protocol.md), the `offer.published` precedent: an
older validator refuses the unknown verb safely, so the protocol
version does not bump). Each is a fact on an **existing nonterminal
contract subject**, under the `dispatch` grant with the standard
operator fallback ([`actors.md`](actors.md)): the relations are queue
shaping, the dispatcher's, and grant nothing.

| verb | strict payload | meaning |
|---|---|---|
| `dependency.linked` | `{"requires": "<subject>"}` | the subject requires the named contract; links are a set |
| `dependency.unlinked` | `{"requires": "<subject>"}` | the link is removed; an absent link refuses |
| `hierarchy.parented` | `{"parent": "<subject>"}` | the subject belongs under the named contract; a repeat replaces the current parent |
| `goal.aligned` | `{"mission": "<path> @ <commit>"}` | the subject serves the commit-anchored mission; a repeat replaces it |

**The graph rule** (`topology.Graph.Apply`, one implementation for the
boundary and the fold): the source is a contract the chain holds
whose folded state the table does not mark terminal (terminal work
neither waits nor holds); a contract target is one the chain holds
(any state: a terminal dependency is a satisfied one); no self edge;
no dependency cycle (the target does not already require the source,
directly or through others) and no hierarchy cycle (the source is not
already an ancestor of the target); a duplicate link and an unlink of
no link refuse. The mission is a **commit-anchored artifact
reference**, `"<path> @ <commit>"` with a clean relative path (the
lessons store's own anchor grammar), never prose: the ledger carries a
pointer to what the repository holds, and hostile text has no field to
ride in. Every refusal is `topology_refused` (exit 3,
[`envelope.md`](envelope.md)) naming the part.

## One trustworthy fold

The tolerant lifecycle fold keeps a well-shaped raw push, so a consumer
that trusts fold presence launders an unadmitted record. The graph is
therefore folded **judging each relation fact at its own position**
(`topology.Fold`; the `RunStartValid` posture: fold presence is never
proof of admission): the version activated, the signer holding a
capability the verb accepts against the prefix it appended onto (the
keyring replayed there), the strict shape, and the graph rule against
the lifecycle folded at that prefix. A fact that fails any of these is
a **topology anomaly**, kept with its position, verb, subject and
reason, and it shapes nothing: it cannot hide work behind a dependency,
hold a claim under a forged parent, silence a warning with a lent
mission, or plant an edge the cycle check would then honor. Admission
(`admit.Topology`), the queue, the poll, the wake bridge, the contracts
view, the report and the cache all read this one fold, so every
surface agrees at every prefix.

## Effective readiness (the dependency and hold halves)

A subject is **effectively ready** exactly when its folded lifecycle
state is `ready`, every active dependency is in a state the table marks
terminal (`done` or `cancelled`), and no ancestor's folded state is
`blocked` (`Derived.EffectiveReady`). Relation-free subjects reduce to
the table's ready. The predicate is consumed by:

- **the claim boundary**: `claim.taken` on a subject the table admits
  from `ready` refuses `not_effectively_ready` (exit 3) when the
  subject waits or is held, the message naming the unresolved
  dependencies and the holding ancestors; a subject that is not ready
  at all is the table's refusal, unchanged;
- **the queue** (`queue.json`, derivation `transitions/1+topology/1`,
  Version "4"): the ready set narrowed to the effectively ready,
  `since_position` still the position that made the subject ready;
- **the poll** (`seed offer list`, `offers.Live`): a subject that is
  not effectively ready lists nothing; its stored offers stay folded
  and list again the moment it becomes effectively ready, with no
  fresh publication;
- **the wake delta** (below).

**Holds cascade without rewriting descendants.** Every ancestor whose
folded state is `blocked` is an inherited hold, whatever route put it
there (`contract.blocked`, `claim.parked`, `escalation.raised`, or any
row the table adds). The descendant keeps its base state; the derived
view names every holding ancestor as `held_by`, nearest first, and the
four surfaces above exclude it. When the last ancestor leaves
`blocked`, otherwise-eligible descendants are effectively ready at
that event's position. Closing a dependency while a hold stands
changes nothing visible and produces no wake candidate.

## Advisory wakes

Polling is the correctness path ([`offers.md`](offers.md); the
wakeless poll-only drill). A wake accelerates the same readiness
delta and never decides it: `offers.Bridge` takes the supervisor's
last observed position as the delta cursor, derives the subjects
effectively ready at the tip and not at the cursor
(`topology.ReadinessDelta`), matches each to the active actors
eligible for a live, position-authorized offer on it (`offers.Eligible`,
`offers.Authorized`, the same helpers `seed offer list` reads), and
calls the executor adapter's `Wake(actor)` (`executor.Adapter`)
once per such actor **that has a registered channel**. No channel is a
no-op; a wake error is reported to the caller and never rolls back,
blocks, or alters the queue; a subject still held or waiting is no
candidate; wakes may repeat across caller retries, because they are
advisory and readiness stays ledger-derived and idempotent. `seed
offer wake (--ledger <dir> | --remote <repo>) --since <position> [--now
<RFC3339>]` runs the pass and reports `became_ready`, `candidates`
(subject and eligible actors) and `woken`; the CLI registers no
channel, since every shipped adapter's `Wake` is the documented no-op
and the worker pulls, so it wakes nobody and says `channels: 0`. No
assignment, no inbound executor connection and no durable wake fact
exist.

## Initiatives and goals (the rollup and ancestry halves)

A contract with direct children is an **initiative**. Its rollup
(`Derived.Rollup`) covers every transitive descendant, the initiative
itself excluded, as exact integers: `descendants`, `by_state`
(lifecycle state counts; `unknown` for a child the lifecycle never
created), `terminal`, `effective_ready`, `dependency_waiting` (ready
with an unresolved dependency), `held`, and `milestones`, the sum of
the descendants' admitted `progress.milestone` high-water counts
(`Fold.Milestone`). No percentage and no wall-clock estimate is
invented.

**Goal ancestry** for an open contract walks self, then parents, to
the first mission anchor (`Derived.Mission`), which is rendered with
the subject that carries it (`mission_from`), so a warning's absence
is falsifiable from the same view. An open contract with no anchor on
itself or any ancestor carries a **goal-ancestry warning** naming the
subject and the chain the walk traversed, self first; terminal work is
omitted; an anomalous relation lends no ancestry and silences nothing.

## Surfaces

- `seed topology depend | undepend | parent | align (--ledger <dir> |
  --remote <repo> [--ref <ref>] [--state <dir>]) --key <path> --subject
  <id> --requires <id> | --parent <id> | --mission <anchor>` — the four
  facts, appended through the ordinary checked admission path (the
  loop's transport shape), never a topology side store; the payload is
  shape-checked at usage before a session opens. The dispatcher lane
  declares them in `acts_through` ([`lanes.md`](lanes.md)).
- `seed offer list` — filtered by effective readiness (above); `seed
  offer wake` — the advisory pass.
- **contracts** (`contracts.json`, Version "15"): every entry gains
  `topology` when the prefix carries a trusted relation:
  `requires` (target, position, actor), `required_by`, `parent`,
  `children`, `mission` (the subject's own), `mission_anchor` and
  `mission_from` (the one that applies, own or inherited),
  `unresolved`, `effective_ready`, `held_by`, `goal_ancestry_warning`
  where open and unanchored, and `rollup` where an initiative. The
  contract's `state` beside it is the fold's and never rewritten.
- **queue** (`queue.json`, Version "4"): the effective derivation.
- **report** (`report.json`, Version "19"): a `topology` section with
  `initiatives` (subject, mission anchor and carrier, rollup),
  `goal_ancestry_warnings`, and `anomalies`, present when the prefix
  carries a relation fact, trusted or not.
- **cache** (`cache.db`, schema generation 13, Version "15"): the
  `relations` table (one row per active link, parent and mission, with
  `ts` and `ts_unix`), `topology_state` (effective readiness,
  unresolved and held-by sets as JSON, parent, mission anchor and
  carrier, the initiative flag and the rollup as JSON),
  `topology_anomalies`, `goal_ancestry_warnings`, the report's
  `topology` key, and the queue's effective derivation in
  `queue_meta` ([`projections.md`](projections.md)).

Every surface is present or populated only when the prefix carries a
relation fact, so builds of chains that carry none stay byte-identical
apart from the version in the build id; every projection build stays
deterministic, stamped, rebuildable and read-only.

## Drills

`TestDependencyCascadeWakesAndPolls` (the dependency half),
`TestHoldCascadeSuppressesWakeUntilReleased` (the hold half),
`TestInitiativeRollupRendersDescendants` (the rollup half),
`TestGoalAncestryWarnsOnlyOpenUnanchoredWork` (the ancestry half) and
`TestTopologyRelationBoundary` (the boundary) under `cmd/seed` and
`internal/admit`, beside `internal/topology`'s unit drills;
`TestWakelessPollOnlyRun` (#145) is retained unchanged as the
correctness path's evidence.

## Conformance mapping

III.F row 12 in full. The transition table, its generated lifecycle
document, the envelope codes, the canonical event form and every
`seed/0` through `seed/7` history are unchanged; the four verbs are
catalog growth under the additive rule.
