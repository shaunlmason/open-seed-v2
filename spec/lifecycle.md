# lifecycle.md — the contract lifecycle

> The enumeration is generated: [`../docs/generated/lifecycle.md`](../docs/generated/lifecycle.md) renders the states and every transition from the table this file explains; `seed docs check` holds them equal.

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §6 "Lifecycle" and §8 (done is a verdict and a reconciliation);
> [`docs/build-plan.md`](../docs/build-plan.md) Phase 5 item 1;
> plan `plans/os-d69a6c91.md`. Implemented by `internal/transition`
> and the `lifecycle` admission rule.

## The table is the contract

The lifecycle vocabulary and transition rules are **self-validating
data enforced at admission** (conformance III.F). The normative table
is [`transitions.json`](transitions.json), embedded byte-identically in
`internal/transition` (the `classify.json` precedent, pinned by test);
this file quotes it, and a drill pins the quotation to the real table
(the docs fan-out that generates lifecycle prose from the table is a
later phase's machinery):

```json
{
  "schema_version": "1",
  "states": [
    {"name": "backlog", "initial": true},
    {"name": "ready"},
    {"name": "in_progress"},
    {"name": "review"},
    {"name": "blocked"},
    {"name": "done", "terminal": true},
    {"name": "cancelled", "terminal": true}
  ],
  "transitions": [
    {"verb": "intent.filed", "from": null, "to": "backlog"},
    {"verb": "contract.specified", "from": ["backlog", "ready"], "to": "ready"},
    {"verb": "claim.taken", "from": ["ready"], "to": "in_progress", "exclusive": true},
    {"verb": "submission.made", "from": ["in_progress"], "to": "review"},
    {"verb": "claim.released", "from": ["in_progress"], "to": "ready"},
    {"verb": "claim.parked", "from": ["in_progress"], "to": "blocked"},
    {"verb": "claim.reaped", "from": ["in_progress"], "to": "ready"},
    {"verb": "merge.observed", "from": ["review"], "to": "done"},
    {"verb": "contract.returned", "from": ["review"], "to": "ready"},
    {"verb": "contract.blocked", "from": ["ready"], "to": "blocked"},
    {"verb": "contract.unblocked", "from": ["blocked"], "to": "ready"},
    {"verb": "contract.cancelled", "from": ["backlog", "ready", "blocked", "review"], "to": "cancelled"},
    {"verb": "escalation.raised", "from": ["ready", "review"], "to": "blocked"},
    {"verb": "decision.recorded", "from": ["blocked"], "to": "ready"}
  ]
}
```

Hand-written conditionals that re-derive what the table says are a
design violation: legality comes from the parsed table, everywhere.

## Vocabulary

The verbs are the charter's Appendix catalog names, verbatim. There is
**no "promote" verb**: `contract.specified` ("acceptance spec gate
passed") *is* the `backlog`→`ready` transition, so claimability and
specification are one transition by construction. One addition rides
the catalog's explicit `contract.*` ellipsis: `contract.unblocked`
(`blocked`→`ready`), the inverse the state machine requires, named
here as a catalog extension.

**Claim is a transition, not a state**: `claim.taken` moves
`ready`→`in_progress` with its fence mechanics below. Leaving
`in_progress` happens **only** through the four deliberate exits —
`submission.made`, `claim.released`, `claim.parked`, `claim.reaped` —
and the self-validation pins that set, so silent abandonment is
impossible by construction; `contract.cancelled` deliberately has no
`in_progress` source. **Every deliberate exit carries a four-part
handoff packet** ([`packets.md`](packets.md)), refused at admission
without one. Preemption rides these exits: a `run.interrupted` on
the active fence asks the worker to take the `claim.parked` exit at
its next safe point, and an ignored interrupt ends in `claim.reaped`
([`executors.md`](executors.md)'s Preemption section). **Done is reached only through
`merge.observed`**, the final observation of the §8 reconciliation
chain (`verdict.rendered(pass) → merge.requested → merge.observed`);
`verdict.rendered` is piped as of 6.1 — a fact admitted only on
`review` subjects under L1 independence ([`verdicts.md`](verdicts.md)),
changing no state — and 6.2 pipes the rest: `merge.requested` admits
only in `review` citing the pass verdict, and `merge.observed` admits
only behind the full chain rule, recording the merged commit
([`reconciliation.md`](reconciliation.md)). 6.3 adds the pre-claim
fact: `check.sealed` admits only while the subject is in `ready` with
no prior claim, committing the sealed checks before implementation
begins ([`sealed-checks.md`](sealed-checks.md)). 6.4 resolves what was
Phase 6's named extension point: **`contract.returned`**
(`review` → `ready`) is the failed verdict's return path, admitted
only citing a standing, boundary-validated **fail** verdict on the
current submission (`{"verdict": "<position>"}`, the
`dispatch`/`operator` lanes) — nobody yanks an in-review contract
whose verdict is pass or pending, and a raw-pushed fail authorizes
nothing. The subject re-enters `ready` for a fresh
claim → submission → verdict cycle; prior facts, the sealed
commitment included, persist as history. From `seed/8` the return
cites exactly one of the fail verdict or a standing red
`check.observed` on the head under review, the forge's word that the
submission is not mergeable, and a return by observation records no
lockout ([`observations-forge.md`](observations-forge.md)); the row
is unchanged.

## Claims and fences

**The fence is derived, never asserted** (plans/os-5dc16a7c.md): it
is the chain position of the active `claim.taken` record, established
when the claim admits — ordering authority is admitted ancestry, so a
client cannot mint a fence, only cite one, as a payload field
`{"fence": "<position>"}` (string, the envelope position convention).

**Who must cite it**, on a subject whose folded state is
`in_progress`: every deliberate exit (`submission.made`,
`claim.released`, `claim.parked`, and `claim.reaped`, which names the
claim it kills); and every free-stream event signed by the current
holder **or by any prior claimant of the subject** — a reaped or
released worker's delayed observation must not slip through
fence-free and contaminate progress or later expiry/wedge math, so a
prior claimant cannot demote itself to observer (review finding on
#114). Any citation present must match the **active** fence, whoever
signs; a prior claimant citing the active fence is explicitly
acknowledging the current claim. Free events from signers who have
never claimed the subject are genuine observations and pass without a
fence. A missing-but-required or stale citation refuses exit 6
`fenced_out`, naming the cited fence, the active fence, and the
holder. **Outside `in_progress` there is no active fence**: none is
required, citing one refuses (a fence dies with its claim window),
and claim-scoped contamination is impossible because nothing outside
a claim window carries a fence. A re-claim mints a new fence; the old
one is stale everywhere.

**Contention is its own refusal**: `claim.taken` on an `in_progress`
subject refuses exit 2 `contention` with a structured envelope naming
the holding fingerprint and the active fence position (the position
the claim was taken at) — the loser learns who holds and since when.
All other illegal lifecycle jumps stay exit 3.

**Claiming is online-only**: the transition row for `claim.taken`
carries `"exclusive": true` (self-validation refuses an exclusive
birth verb), and exclusivity is a property granted at admission —
only the push round-trip can order two rivals, so two offline actors
claiming the same contract have not claimed anything. The cooperative
client refuses to draft an exclusive verb through the local dev-tool
append (exit 2, quoting this posture); the boundary enforces
exclusivity regardless — the client rule prevents drafting doomed
work. Reading, planning, and continuing an admitted claim stay fully
offline. **Racing mode** lifts the exclusivity per squad, by declaration; see
"Racing" below.

`progress.milestone` and `wedge.declared` are
**facts, not transitions** (`observations.md`): a milestone is the
claim lane's coarse, monotonic, position-throttled summary and a
declared wedge records the visible condition durably; neither
changes state, and the pinned four `in_progress` exits stand.

## Racing

Charter §II.6 and III.F row 7: exclusivity is the rule, and a squad
may opt out of it explicitly, with its compute cost stated —
duplicate execution tolerated, the first *verified* success settling
the contract (plans/os-56bee171.md; `seed/6`).

**The opt-in is declared.** `seed.json`'s
`guardrails.squads.<name>.racing` is `{"racers": N, "cost": "<the
operator's sentence>"}`: the most claims the squad tolerates on one
contract at once (two or more) and the compute-for-latency trade in
the operator's words, both required or the declaration refuses
`posture_invalid` ([`postures.md`](postures.md)). An absent block is
no racing. No verb, flag or environment opts a contract in.

**A racing claim admits beside the holders, each with its own
fence.** At `seed/6`, `claim.taken` on an `in_progress` contract whose
squad races admits while the contract holds fewer active claims than
`racers` and the claimant holds none of them; the record is a claim
like any other — exclusive in the table's sense, granted at admission,
never offline — and its fence is its own position. Without the opt-in,
or at the cap, the second claim refuses exit 2 `contention` as it
always did, at the cap naming every holder and fence. The fold keeps
every active claim (`Claims`) beside the singular `Claim`, which is the
first of them, so every reader of the singular fact reads what it read
before. The fence rule reads the active fences: a holder cites its own
fence and no other (`fenced_out` otherwise); a prior claimant cites any
active one.

**A racer's exit is claim-scoped, except two.** The table is untouched:
a second claim is a fact on an `in_progress` subject, not a row, and a
racer's `submission.made`, `claim.released`, `claim.parked` or
`claim.reaped` closes that racer's claim alone and moves no state —
except the first submission, which enters `review` (the other racers'
claims outlive it and they keep working), and the last racer's
departure with no submission yet, which the table moves as it always
has. The fold keeps every submission of the window (`Submissions`)
and every verdict (`Verdicts`) beside the singular facts.

**First verified success settles.** A verdict binds to the submission
it cites — any submission of the window — so verdicts on different
racers' submissions coexist, and a fail locks that submitter alone
(`verdicts.md`'s lockout, per submission). The merge chain cites the
newest verdict on its submission; the first `merge.observed` closes
the contract and every other active claim is **settled-out** from
that position: a settled-out racer's next act on the subject refuses
exit 3 `race_settled` naming the settlement, its own deliberate exit
still admits (a packet is never refused after the fact), and the
maintenance pass reaps what does not exit, with a packet naming the
settlement ([`maintenance.md`](maintenance.md)). A tie is impossible:
chain order is total.

**Spend is per racer and the race is visible.** Every racer reserves
against its own fence ([`budgets.md`](budgets.md)); the contracts view
carries a `racing` object (the racers, and after settlement the
settling position and the claims it settled out;
[`projections.md`](projections.md)); the claim response tells a racer
its squad races and what the operator said it costs, so the sentence
is in front of the lane that spends it.

**Offline claiming stays impossible by construction.** The table's
`exclusive` flag and the cooperative client's refusal to draft a claim
offline are untouched; racing is a judgment inside the lifecycle rule
beside contention, ordered by the push round-trip, never by a clock.

**Before `seed/6`** a second `claim.taken` on an `in_progress` subject
is contention at the boundary and an anomaly in the fold, as it always
was: the widened judgment is a named version's, so every existing chain
verifies byte for byte and a `seed/5` validator refuses an upgraded
chain at the first racing record by version rather than by
misjudging a race ([`protocol.md`](protocol.md)).

## Self-validation

The table refuses to load unless: every `from`/`to` names a declared
state; exactly one initial state and exactly one birth verb
(`from: null`) landing on it; terminal states have no outgoing rows;
no duplicate verb; every state is reachable from the initial state;
every non-terminal state reaches a terminal one (no wedge); the
`in_progress` outgoing set is exactly the four deliberate exits; and
an exclusive row must not be the birth verb. Each violation refuses
by name, drilled on planted tables.

## Completeness at the claimability transition

The charter's birth rule — a contract becomes claimable only when it
carries intent prose, an acceptance spec, and tier/budget/routing —
is enforced at the shape level: `intent.filed` must carry non-empty
`intent`, `tier`, `budget`, and `routing`, with `tier` a member of the
tier table and `budget` a member of the class table, byte for byte
([`tiers.md`](tiers.md), [`budgets.md`](budgets.md)), and from `seed/3`
may carry the optional **`eval`** marker `{"name", "tuple"?}` that
makes the contract an eval ([`evals.md`](evals.md)): folded to the
subject's `Eval`, refused at an earlier tip, and changing no row (an
eval's lifecycle is the lifecycle, its verdict its terminal fact, and
no merge is owed); `contract.specified` must
carry the **structured acceptance field** — commit-anchored `ref`,
the `executable` flag, and gate evidence bound to the ref's revision
for ALL executable content ([`acceptance.md`](acceptance.md)). The
sealed commitment lands with Phase 6. Completeness refusals are shape
refusals.

**Re-specification** ([`trajectories.md`](trajectories.md), "The two
lane-quality metrics"; plans/os-6bd9ffff.md D4): from `seed/4` the
table carries `ready` as a second origin for `contract.specified`, the
dispatcher revising its own draft acceptance of a contract nobody has
claimed. The row is gated by version, not only by state: before
`seed/4` a ready-origin specification refuses as an illegal transition
naming the version that activates it (`re-specification activates at
seed/4 and the chain is at seed/3`), because a validator of the
earlier version judges the record by its own table; every other origin
the table refuses stays the table's refusal, so a claimed, reviewed,
blocked or finished contract is out of reach. The shape rule is the
first specification's (the structured acceptance field, gate evidence
for executable content), the fold applies a re-specification at
`seed/4` positions only (an earlier one is a visible anomaly and the
first acceptance stands), and the subject's `Specifications` count
records how many applied. Not revised: `tier`, `budget` and `routing`
stay the intent's. The record now carries the dispatcher's re-triage,
which the report's `lanes` section counts ([`projections.md`](projections.md)).

## Capabilities

Every lifecycle verb is capability-gated (the rows live in
[`actors.md`](actors.md) and `keyring.AcceptedCapabilities`, pinned by
the spec-parsing test): `dispatch` manages the queue (`intent.filed`,
`contract.specified`, `contract.blocked`, `contract.unblocked`,
`claim.reaped`), `claim` is the worker set (`claim.taken`,
`claim.released`, `claim.parked`, `submission.made`), and
`contract.cancelled` / `merge.observed` are operator-only in v0
(Phase 6 adds the observer lane for `merge.observed`; cancellation
stays operator-gated until a real need appears). Reaping is queue
management, not worker self-service.

## Admission policy, visible anomalies

Lifecycle legality is **admission policy**, exactly like halt,
classification, and grants: admission refuses illegal transitions at
`seed/1` (records under `seed/0` are grandfathered inert, the keyring
precedent, and the fold honors the same boundary: pre-activation
records occupy no state, so an upgraded ledger's history cannot make
a real filing look like a second birth) with exit 3
`invalid_transition` naming subject, current state, and verb; a birth verb on an existing subject and a non-birth
verb on an unknown subject refuse the same way. Verification
tolerates illegal transitions in raw-pushed history — the cooperative
posture's named consequence — and the projection fold **skips them
visibly**: every contract entry carries an `anomalies` count, never
silence. Verbs outside the table stay facts, not transitions:
`progress.milestone` and `wedge.declared` admit under the
summarization boundary (`observations.md`), `verdict.rendered` admits
only on `review` subjects under L1 independence (`verdicts.md`), and
`merge.requested` admits only on `review` subjects citing the pass
verdict (`reconciliation.md`), and `check.sealed` admits only on
`ready` subjects with no prior claim (`sealed-checks.md`), and
`merge.overridden` admits only on `review` subjects over a validated
fail (`reconciliation.md`), and `offer.published` admits only on
`ready` subjects with an unexpired-at-admission expiry (`offers.md`),
and `budget.reserve` admits only on `in_progress` subjects inside the
claim window it spends under while its two closes admit wherever the
reservation they cite stands open (`budgets.md`), and `run.started`
admits only inside the active claim window against an open
reservation while `run.settled` cites any applied claim fence that
carries a start (`executors.md`) — the §8 chain is
fully piped and the §7 commitment window is pinned.

## Projections

The contracts view carries the folded `state` (null for a subject no
lifecycle event ever validly created) and `anomalies` per entry
(contracts `Version: "3"`, which also carries the claim object); the queue's derivation is the table's
ready set — `derivation: "transitions/1"`, listing subjects whose
folded state is `ready`, oldest first, `since_position` the position
that made them ready (queue `Version: "2"`) — retiring the v0
`"none"` marker exactly as [`projections.md`](projections.md)
promised. Every derivation change republishes under a new
version-bearing build id at an unchanged tip; the cache mirrors the
same derivations (`contract_state` with the claim columns, the
derived `queue` rows, schema generation 3). Since queue Version "4"
the derivation is `transitions/1+topology/1`
([`topology.md`](topology.md)): the table's ready set narrowed to the
effectively ready. A dependency that waits and an ancestor's hold are
derived from the relation facts beside the lifecycle and never mutate
a subject's folded state: the table stays the sole authority on what
a subject IS, the graph on whether it is claimable now.

## Conformance mapping

- III.F "The lifecycle vocabulary and transition rules are
  self-validating data enforced at admission; claim is a transition,
  not a state" — this task: the table, its self-validation drills,
  the `lifecycle` admission rule across the rule set, the seed-admit
  hook, and the CLI.
- III.F fences, packets, spec-gate content, plan-gating, verdicts —
  5.2 through 5.6 and Phase 6, per their plans; the review exits
  beyond `contract.cancelled` arrived with 6.4's `contract.returned`.
