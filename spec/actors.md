# actors.md — actor events and the keyring projection

> The enumeration is generated: [`../docs/generated/capabilities.md`](../docs/generated/capabilities.md) renders each verb's accepted-capability set from the table this file explains; `seed docs check` holds them equal.

> Status: v0, normative for `next/**` from protocol version **`seed/1`**.
> Authority: [`SEED-NEXT.md`](../SEED-NEXT.md) Part II "Enrollment" and
> "Capabilities"; [`docs/build-plan.md`](../docs/build-plan.md)
> Phase 3 item 1; plan `plans/os-52a2d688.md`. The verb catalog lives in
> [`protocol.md`](protocol.md); this file owns the payload schemas and
> standing semantics, implemented in one transition function
> (`internal/keyring`) consumed by both verification and admission.

## Activation: the `seed/1` boundary

Everything below activates for records whose position is under protocol
version `seed/1` (reached via `system.protocol.upgraded`). Records at
`seed/0` positions are **grandfathered**: `actor.*` events there are
inert — no keyring effect, no payload judgment — so every chain that
verified before `seed/1` existed still verifies (the bump discipline in
`protocol.md`: these semantics are exactly a validation-rule change a
`seed/0` validator would judge differently). New `actor.*` proposals at a
`seed/0` tip refuse as a verb illegal in this state (exit 3) until the
deployment upgrades.

The same posture gives `seed/2` its boundary
([`qualification.md`](qualification.md)): because these payload shapes
are chain validity, a field added to one is a rule a `seed/1` validator
judges differently, so `actor.granted`'s optional `tuple` activates at
`seed/2` positions only and stays unknown-and-refused at `seed/1`
positions under every conformant validator.

## The keyring

The keyring is a **projection**: seeded from the genesis payload's
`governance_root` (those keys are roots, standing active) and advanced by
the events below, never stored. From `seed/1`, signature resolution is
standing-aware at every position: a key resolves only while its standing
is active, so a key signs only between its enrollment (or genesis) and
its suspension or revocation — and records signed before a standing
change keep verifying, which is what keeps a revoked key's history
attributed to it.

## Payload schemas (strict: unknown fields refuse)

- **`actor.enrolled`** — subject: the enrolled key's fingerprint.
  Payload `{"key": "<64-char hex of the raw 32-byte Ed25519 public
  key>", "kind": "human" | "agent" | "service", "name": "<non-empty
  display name>"}`. The subject MUST equal the fingerprint of `key`.
  `kind` is **an assertion by the enrolling operator, not a
  cryptographic fact** (SEED-NEXT.md Part II), and nothing
  security-relevant may assume otherwise.
- **`actor.granted`** — subject: an enrolled fingerprint. Payload
  `{"capability": "<non-empty>"}`, and from `seed/2` optionally
  `{"capability": "<non-empty>", "tuple": {…}}` where `tuple` is the
  strict five-field runtime tuple of
  [`qualification.md`](qualification.md): a malformed one (a missing
  field, an empty string, an unknown field, a non-string, `null`) fails
  verification at its position as `bad_actor_event`, and a grant with
  no tuple folds exactly as before. Grants accumulate as capability
  data; admission checks them per verb from Phase 3.2. The tuples a
  holder's `claim` grants cite form the SET the qualification rule
  reads at `run.started`; an actor with none is unqualified (the
  bridge) and admits any run.
- **`actor.suspended`** / **`actor.revoked`** — subject: an enrolled
  fingerprint. Payload `{"reason": "<non-empty>"}`.
- **`actor.qualified`** / **`actor.disqualified`** — subject: an
  enrolled fingerprint; defined at `seed/3` positions only
  ([`evals.md`](evals.md)), and at `seed/1` or `seed/2` positions
  unknown-and-refused at the position as `bad_actor_event`. Payload
  `{"capability": "claim", "tuple": {…}, "contract": "<eval
  subject>", "verdict": "<chain position>"}`, plus `"reason":
  "<non-empty>"` required on a disqualification and refused on a
  qualification (the cited verdict is the reason); `capability` is
  `claim` and nothing else, since an eval proves a configuration for
  work, never another authority. A qualification is a
  grant with evidence: it grants the capability if absent, adds the
  tuple to the admissible set the qualification rule reads, and marks
  the capability as ever cited; a disqualification removes the tuple
  and refuses when the actor holds no admissible grant citing it. The
  cross-references (the eval, the authenticated verdict, the window's
  declaration, the holder, the `ts` ordering, the duplicate) are
  admission policy in the qualification rule; the shape and standing
  legality here are chain validity like every actor verb's.

## Standing transitions

| event | precondition | effect |
|---|---|---|
| `enrolled` (new key) | fingerprint unknown | standing active |
| `enrolled` (suspended key) | standing suspended | reinstated: standing active, kind/name updated |
| `enrolled` (active key) | — | **refuses** (already enrolled) |
| `enrolled`/`suspended`/`granted` (revoked key) | — | **refuses**: revocation is terminal |
| `granted` | subject enrolled, not revoked, and the grant keeps sealer disjoint from claim and operator in both directions (a root's implicit operator standing included; sealed-checks.md), and curate likewise (curation.md) | capability appended |
| `suspended` | subject active | standing suspended |
| `revoked` | subject not already revoked | standing revoked |

A revocation also **reaps the revoked holder's open claims**: from the
`actor.revoked` position on the holder can no longer act, so the
maintenance pass reaps its `in_progress` window on the revocation alone
(`admit.RevokedHolder`; [`maintenance.md`](maintenance.md); plans/os-32d06c65.md), re-offering the work.

**Root liveness.** Suspending or revoking a governance root refuses when
it would leave zero active roots: no admitted transition may leave the
deployment without a key admission accepts `actor.*` from. Root rotation
beyond that guard is genesis-level governance, outside these events.

## Import-generated identities

A predecessor import ([`import.md`](import.md)) enrolls one identity
per distinct v1 actor name with a key the importer generated and held
in memory for that import only, kind from the transform table, grants
derived from the run-log before replay, and suspends every one of them
after the replay. The rule above — attribution is not trust, kind is
an operator's assertion — is what makes this honest: the imported
chain attributes each record to the name v1 recorded, under a key
nobody but the importer ever held, and the mapping manifest
`system.imported` cites says so. No import-generated key is ever
granted `operator`; what only an operator may sign, the importing
operator's own key signs. A suspended import identity can be
reinstated by `actor.enrolled` like any other suspended key, which is
an operator's deliberate act, never the import's.

## Capabilities

Grants are events (`actor.granted`) checked at admission on every verb
(SEED-NEXT.md Part II "Capabilities"). The normative vocabulary maps
each governed verb to the **set of capabilities any one of which
admits** (mirrored by `internal/keyring.AcceptedCapabilities`, pinned by
test); a verb with no row needs active standing only. Governance roots
hold `operator` implicitly — the genesis trust anchor a deployment's
first grants must come from. Only active standing counts: a suspended
or revoked actor holds nothing, and grant-level withdrawal short of
ending standing is deferred until the catalog grows a verb for it.

| verb | accepted capabilities |
|---|---|
| `system.halt.declared` | `operator` |
| `system.halt.lifted` | `operator` (the charter: only an operator's lift may append) |
| `system.protocol.upgraded` | `operator` |
| `system.checkpoint` | `maintenance`, `operator` (the charter names checkpoints as signed by the maintenance actor or an operator) |
| `system.imported` | `operator` (the predecessor import's provenance record, from seed/5, once per ledger; [import.md](import.md)) |
| `actor.*` (enrolled, granted, suspended, revoked) | `operator` |
| `actor.qualified`, `actor.disqualified` | `supervise`, `operator` (the first non-operator actor rows: SEED-NEXT.md §5 makes suspension of a failing configuration the supervisor's attributable act with no operator ceremony, and a mint is the same act with the opposite sign; operator stays the standing override, evals.md) |
| `intent.filed` | `dispatch`, `operator` |
| `request.answered` | `dispatch`, `operator` (the dispatcher's close of an inbound request, from seed/7; the filing verb itself has no row — standing only, like a message — since a proposal's ingress identity holds no capability; [requests.md](requests.md)) |
| `artifact.erased` | `operator` (the erasure obligation is a governance act a human answers for, the decision.recorded posture; the record names the digest and the obligation, on the contract whose fold references the artifact or on system, once; [protocol.md](protocol.md), Erasure) |
| `contract.specified` | `dispatch`, `operator` |
| `contract.blocked` | `dispatch`, `operator` |
| `contract.unblocked` | `dispatch`, `operator` |
| `claim.reaped` | `dispatch`, `operator` (reaping is queue management, not worker self-service) |
| `dependency.linked` | `dispatch`, `operator` (the relation facts are queue shaping: a dependency, a parent and a mission anchor decide what is claimable and what is held, on a known open contract, and grant nothing, so the standard operator fallback stands; [topology.md](topology.md)) |
| `dependency.unlinked` | `dispatch`, `operator` (the link's reverse; [topology.md](topology.md)) |
| `hierarchy.parented` | `dispatch`, `operator` (the parent, replaced by a repeat; [topology.md](topology.md)) |
| `goal.aligned` | `dispatch`, `operator` (the mission anchor, replaced by a repeat; [topology.md](topology.md)) |
| `claim.taken` | `claim`, `operator` |
| `claim.released` | `claim`, `operator` |
| `claim.parked` | `claim`, `operator` |
| `submission.made` | `claim`, `operator` |
| `contract.cancelled` | `operator` (until a real need appears) |
| `escalation.raised` | `claim`, `dispatch`, `verdict`, `supervise`, `operator`, `curate` (any lane may raise blocked(needs-you) per the charter, and breadth is safe because raising a question grants nothing: the offer.published argument. A raised contract leaves blocked only through decision.recorded or a citing cancellation, so a raiser can stop work and hand a human the decision, never move it, escalation.md; the curator joined the row with its proposal grant, curation.md) |
| `decision.recorded` | `operator` (the fourth no-fallback row: the charter names escalations a gate humans hold, so a dispatch fallback would let a machine lane answer a human gate, escalation.md) |
| `approval.granted` | `operator` (the per-verb approval's grant, from seed/1 as additive catalog growth: an approval is a gate a human holds, the decision.recorded posture, and a machine-lane fallback would let the governed lane answer for itself; the request verb itself has no row — standing only, like request.filed — since asking grants nothing; [protocol.md](protocol.md), "Per-verb approval") |
| `approval.denied` | `operator` (the same row's other answer) |
| `plan.proposed` | `claim`, `operator` (the claim holder plans; the fence matrix applies) |
| `plan.approved` | `operator` (an external-fact observation, the merge.observed posture) |
| `progress.milestone` | `claim`, `operator` (the claim lane's coarse summarization fact; the fence matrix applies) |
| `wedge.declared` | `operator` (operator judgment in v0; the maintenance lane inherits it later) |
| `merge.requested` | `claim`, `operator` (asking for the merge is the work lane's act; the payload cites the pass verdict, reconciliation.md) |
| `merge.observed` | `observer`, `operator` (the observer lane records forge fact behind the full chain rule, reconciliation.md) |
| `check.observed` | `observer`, `operator` (the observer lane records what the forge's checks and review threads say about the head under review, from seed/8: a fact on review subjects, the merge.observed posture, observations-forge.md) |
| `verdict.deferred` | `verdict` (the human-verdict deferral, the verifier's own act naming what it could not judge; the same no-fallback row, and after it a render on the submission needs operator standing beside the verdict grant, verdicts.md) |
| `verdict.rendered` | `verdict` (deliberately no operator fallback: III.G names operator override its own attributable verb, never a disguised verdict — that verb is 6.4's; a governance root that judges holds an explicit verdict grant, and L1 independence applies to every signer, verdicts.md) |
| `check.sealed` | `sealer` (the second no-fallback row: operator already stands in the claim and submission lanes, so an operator fallback here would put authoring and implementation authority on one capability and the capability audit could prove nothing, sealed-checks.md) |
| `contract.returned` | `dispatch`, `operator` (returning a fail-verdicted contract to the queue is queue management; the payload cites the authenticated red verdict, lifecycle.md) |
| `merge.overridden` | `operator` (the third no-fallback row: the charter names the override its own attributable verb, never a disguised verdict — it admits only over a standing, boundary-validated fail on the current submission, reconciliation.md) |
| `offer.published` | `supervise`, `operator` (the supervisor lane: an offer invites claims and grants nothing — the claim it invites settles at admission like any claim — so the standard operator fallback stands, offers.md) |
| `budget.reserve` | `claim`, `operator` (the claim lane reserves inside its window; the budget rule further pins reserves to the ACTIVE holder, budgets.md) |
| `budget.settle` | `claim`, `operator` (closes are the reservation owner's or the operator's; the budget rule pins the owner, budgets.md) |
| `budget.release` | `claim`, `operator` (same boundary as settle, with zero actuals, budgets.md) |
| `run.started` | `supervise`, `operator` (the gated spend initiation that fences a run to its reservation before any executor provisions; the spending-verb table's first entry, executors.md) |
| `run.settled` | `supervise`, `operator` (the once-per-fence metering aggregate at run end; telemetry, never authority — budget.settle carries the actuals, executors.md) |
| `run.interrupted` | `supervise`, `operator` (the safe-point preemption request, once per active fence; conforming workers poll it and park deliberately with their packet — executors.md's Preemption section) |
| `curation.deadend.recorded` | `claim`, `operator` (the window holder's candidate observation: failure condition and environment beside what was tried, inside the window, the fence matrix applying; a candidate has no field a conclusion could live in, curation.md) |
| `curation.hypothesis.proposed` | `curate` (the fifth no-fallback row: operator already reaches claim.taken and the deliberate exits, so an operator fallback would let one key write a trajectory's observations and then conclude from them; the proposal is reachable through curate alone, and curate is disjoint from claim and operator at the grant, curation.md) |
| `curation.hypothesis.contested` | `curate` (the contest is the curator's attributable judgment over held-out evidence the record already holds, the proposal's own no-fallback posture, curation.md) |
| `curation.lesson.promoted` | `observer`, `operator` (the observation that a lesson file landed by PR, citing the admitted hypothesis it promotes: the merge.observed posture, curation.md) |
| `workflow.proposed` | `curate` (the flywheel's proposal, the curator's posture of proposing everything and approving nothing: a drafted, mock-validated workflow proposed as a PR, flywheel.md) |
| `workflow.merged` | `observer`, `operator` (the observation that the workflow file landed in the registry by PR, citing the admitted proposal: the merge.observed posture, flywheel.md) |
| `curation.lesson.retired` | `observer`, `operator` (the observation that a promotion is revoked: the revert's merge for a regression, a later promotion for a supersession, the stamp for an expiry; the promotion's own row, curation.md) |
| `curation.deadend.retired` | `curate` (the curator's attributable judgment that a dead end's environment moved, citing the dead end on its contract; the evidence stays, curation.md) |
| `curation.deadend.unretired` | `curate` (the same judgment in the other direction, over a standing retirement, curation.md) |

A signer holding none of a verb's accepted capabilities refuses at exit
14 `out_of_grant` (`envelope.md`), the message naming the accepted set.
The rows accepting `observer` are exactly the external-fact inventory
in [`external-facts.md`](external-facts.md), pinned both ways: a verb
cannot join the observer's row without joining that table.
Later phases append rows (claim rights by squad and tier, verdict
rights, curation-proposal rights) when their verbs land.

**Authorization is admission policy, not chain validity**: like the
halt gate and payload classification, verification tolerates it in
history — one of the cooperative posture's named consequences — so the
vocabulary evolves without protocol bumps (the versioning stance
recorded in plans/os-3979d48b.md). Transition legality and payload
shapes above, by contrast, are chain validity: an event violating them
fails verification at its position (`bad_actor_event`), whichever
posture admitted it.

## Conformance mapping

- III.E "every actor is a keypair; enrollment, grants, suspension,
  revocation are events; the keyring is a projection; admission verifies
  signatures on every proposal" — `internal/keyring` + the verification
  and admission wiring, drilled in `internal/ledger`, `internal/admit`,
  `cmd/seed-admit`, and `cmd/seed` tests.
- III.E "enrolled kind is documented as an operator assertion" — this
  file plus the package doc.
- III.E revocation drill (standing ends; history stays attributed) —
  Phase 3.3 turns the verification tests here into the charter drill.
