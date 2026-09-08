# Seed — A Coordination Substrate for Agent Fleets

> **Document status.** This is the founding charter of Seed: vision (Part I), design
> (Part II), conformance criteria (Part III), and appendices (glossary, wire-level
> sketch, lineage, adoption). It is written to stand alone — implementable and adoptable
> by a reader with no prior context. **The name is Seed** — settled by the project owner
> (2026-08-30), replacing the provisional "Keel": everything grows from seed, and the
> successor of open-seed inherits the name it was always growing toward.
>
> *Incubation note*: this document currently lives in the open-seed repository (as
> `SEED-NEXT.md`, its former name) while the system it describes is designed. Within
> open-seed, design authority remains `docs/design-options.md` until adoption through
> that document's own process; Seed's lineage from open-seed is recorded in Appendix C.
> Once spun out, this charter is amended only by PR under the governance root it defines
> (§II.14), and every amendment records its rationale.
>
> *Conformance language*: **MUST**, **SHOULD**, and **MAY** are used in the RFC sense.
> Part II distinguishes the **protocol** (normative — what any conformant implementation
> MUST provide) from the **reference deployment** (one concrete set of choices, marked
> "Reference:", that satisfies it). Part III defines what "conformant" means, tied to
> the admission postures and the drill suite.

---

## Part I — Vision

### 1. The problem, stated from zero

A software organization wants to run most of its engineering through fleets of AI agents.
The agents are capable but unreliable in specific, well-documented ways: they claim
completion without proof, they drift from instructions, they collide with each other,
they forget everything between sessions, and they can be manipulated by the content they
read. Worse: any individual agent must be assumed *compromisable* — by prompt injection,
by a stolen credential, by a poisoned dependency. The organization cannot accept losing
determinism (what happened must be knowable), reviewability (a human must be able to
check any of it), or ownership (the coordination state must belong to the org, forkable
and portable, hostage to no vendor's database).

Every system in this space chooses where truth lives, who is allowed to say "done," and
**what a fully compromised participant can make the organization believe**. Get those
wrong and no amount of agent capability saves you; get them right and even mediocre
agents compound.

### 2. The security invariant

Seed is built around one test, and the architecture is not settled until it passes:

> An enrolled implementer is fully compromised. It holds its legitimate private key and
> its legitimate git credential. It can invoke arbitrary git commands, forge its own
> client, modify anything its OS account reaches, and emit arbitrary syntactically valid
> events. It cannot compromise another actor's key or the trusted git infrastructure.
>
> **Then:** it can spend only its reserved budget; modify only its authorized code
> surface; produce signed lies attributed to itself; and submit work for independent
> verification. It **cannot** claim unauthorized work, approve itself, rewrite history,
> alter gates, impersonate another actor, exceed its lease, or cause an invalid state
> transition to enter authoritative history.

Three properties this invariant forces us to keep distinct, because they are different
things: **tamper evidence** (you can detect that history was altered) is not
**authorization** (invalid history cannot enter); **serialization** (git orders the
pushes) is not **consensus** (the winning push was valid); and **attribution** (this key
signed it) is not **trust** (the principal behind the key meant it, or is who the roster
says). The design below never substitutes one for another.

### 3. Ten first principles

Each is a bounded property with an explicit edge, not an absolute — and the edges are
stated on purpose, because over-claiming is how coordination systems rot.

1. **History is the authoritative coordination record.** An append-only, hash-chained,
   signed log of events is the single source of truth *for coordination state*: what work
   exists, who holds it, what was decided, what was verified, what was spent. Source
   code, build artifacts, secrets, external-world facts, and provider billing live in
   their own systems; the ledger records signed **observations** of them, and never
   pretends observation is control.

2. **One authority for coordination state; everything else is a projection.** Exactly
   one ledger is authoritative per repository. Every other coordination surface — queue
   views, dashboards, mirrors, caches — is a one-way, rebuildable, disposable projection.
   Inbound edits from any projection are requests. Bidirectional synchronization is
   forbidden: a second writable copy is a second authority in waiting, and keeping two
   authorities honest is a consensus problem no integration team should be running by
   accident. External systems remain authoritative for *their own* facts (a merge, a CI
   result, an invoice); the ledger holds observations of those facts, reconciled
   explicitly (§II.8).

3. **Admission is the boundary.** Validity is enforced where events enter authoritative
   history, by a trusted, minimal, stateless **admission boundary** — not by every writer
   promising to behave. Actors propose; admission validates and orders; the ledger
   records; projections interpret; executors execute; verifiers judge. Without this,
   "the ledger is the authority" quietly means "the git server is the authority," and
   every protocol rule is advisory to anyone with a push credential.

4. **Verification is the product.** The bottleneck of every agent loop is the verifier,
   not the model. Work is born as a contract — intent plus machine-checkable acceptance —
   and "done" is a verdict rendered by an independent verifier. The model may propose
   "done"; it can never approve it. Independence means separated *failure domains*, not
   merely separated keys (§II.8).

5. **Identity is attributable, never assumed trustworthy.** Every actor is a keypair and
   every event is signed — which proves possession of a key, and nothing more. The design
   distinguishes identity (the key), credential (what the key can reach), principal (who
   or what is behind it), and runtime (the model, harness, and environment actually
   executing) — and it qualifies and constrains the *runtime configuration*, not the bare
   key (§II.5). A signature attributes; the threat model decides what attribution is
   worth.

6. **Compute is disposable after durable synchronization points.** Any executor must be
   destroyable *after its last confirmed, admitted synchronization* with zero loss of
   coordination state. Work in flight since that point can be lost; the design bounds
   that loss window (frequent cheap sync, bounded findings in packets) instead of
   pretending it is zero.

7. **Affordances over refusals.** The system tells every actor what it may legally do
   next, computed from the spec, the state, and the actor's standing, before the actor
   acts. Refusals remain — admission refuses invalid events regardless of what any
   envelope suggested — but an actor that reads its envelope should rarely meet one.

8. **Learning is a governed, adversarially-hardened loop.** Evidence is collected
   online; lessons are distilled offline with provenance; and every promotion passes
   gates that assume the underlying trajectories may have been *constructed by an
   attacker*. A compounding mechanism that can be poisoned compounds poison; the curator
   pipeline is therefore staged, minimum-supported, expiring, and rollback-able (§II.12).

9. **Data is never instructions — as a defense-in-depth posture, not a solved problem.**
   Content flowing through the system is evidence to be judged, never instructions to be
   obeyed. No fencing makes an LLM immune to adversarial text in its context; therefore
   the *primary* control is capability restriction (a reader of untrusted content holds
   the least standing capability), and provenance typing, delimiter fencing, strict-shape
   interpolation, sandboxing, and network policy are layered on top (§II.14).

10. **Every gate has a governance root; nothing modifies its own gate.** No agent key
    holds write capability to the validators, verifiers, thresholds, keyrings, or
    admission rules that judge its own work. Self-modification is not eliminated — it is
    *routed*: someone can change the protected surface, and the design names exactly who
    (the governance root: operator keys plus owner review), through exactly which
    process. Humans hold gates, not queues: intent, high-consequence plans, the
    protected surface, and escalations — each escalation one packet, one question, one
    decision.

### 4. The end state

A repository where a human states an intent in one sentence; a dispatcher turns it into
contracts with acceptance checks; a supervisor publishes offers to a roster of enrolled,
qualified, cost-metered agents across whatever compute is cheapest that hour; planners
open falsifiable plans; implementers ship diffs from disposable machines; verifiers
render signed verdicts in separated failure domains; a curator distills the week's
trajectories into lessons and deterministic workflows through poisoning-resistant gates;
an admission boundary guarantees that none of it — not even from a compromised
participant — enters history invalidly; and at any moment, one command over the ledger
reconstructs exactly what happened, in what order, by whose key, at what cost, and on
whose authority. All of it open source, forkable, with no server holding any state the
repository cannot regenerate.

---

## Part II — Design

### 1. The Ledger

The ledger is the authoritative coordination record: an append-only sequence of events on
a dedicated git ref (**Reference:** `refs/seed/ledger`), replicated wherever the
repository is replicated.

**Event structure (normative).** Every event MUST carry:

- `ts` — timestamp (recorded for humans; never an ordering authority).
- `actor` — the acting identity's key fingerprint.
- `verb` — what happened (`intent.filed`, `claim.taken`, `verdict.rendered`, …;
  Appendix B).
- `subject` — what it happened to.
- `payload` — verb-specific structured data, schema-validated (see data classification).
- `prev` — hash of the preceding event's canonical form.
- `sig` — the actor's signature over the canonical form (which includes `prev`).

**Ordering (normative).** Order is defined by admitted ancestry — the chain of `prev`
hashes on the authoritative ref. There is no writer-assigned global sequence number: in
a multi-writer optimistic design, a client-computed sequence is an extra invariant every
concurrent writer must get right and gains nothing over ancestry. Where an ordinal is
useful (projections, citations), it is *derived* — assigned at admission or computed
during projection — never asserted by the proposer.

**Data classification (normative).** The ledger is immutable and replicated, so what
enters it is a permanent commitment. Payloads MUST carry **coordination facts and
references, not content bodies**: hashes and paths for artifacts, receipts, transcripts,
and any user-authored or model-derived prose beyond short structured fields. Bulk and
expirable content — logs, transcripts, attachments, anything that could carry personal
data, secrets, or material someone may later be obliged to erase — lives in an addressed
**artifact store** with an erasure path, referenced by hash. A payload lint enforces the
classification at admission; erasing a referenced artifact never breaks chain
verification, and the erasure is itself an attributable event.

**What the chain proves — and what it cannot.** Chain verification proves the
*integrity of retained history*: any reorder, rewrite, interior deletion, or forgery
breaks a hash or a signature. It cannot prove *freshness*: a ledger truncated or rolled
back to a valid earlier tip is a perfectly verifying prefix, indistinguishable in
isolation from the legitimate current ledger. Freshness therefore has its own layered
mechanisms: every append implicitly **witnesses** the tip it extends, so a rollback
diverges from every other actor's next proposal and from every clone that saw the later
tip — divergence surfaces on the next fetch; every client persists the newest verified
head it has seen and **refuses head regression** (monotonic-head rule); enforced
remotes refuse non-fast-forward updates of the ledger ref; and signed checkpoints
anchor known-good positions. The honest residual is stated, not hidden: a **fresh clone
with no prior head** can verify integrity but can bound staleness only by the newest
checkpoint or witnessed head it obtains out-of-band (from an operator, another actor,
or a published attestation such as a release tag). Deployments needing stronger
fresh-clone freshness publish head attestations through such a channel.

**Checkpoints.** Periodic `checkpoint` events embed the hash of the canonical projection
state at that point, **signed by the maintenance or operator identity**. What a
checkpoint buys is explicit: a reader who has *once* verified a checkpoint against full
replay (or who explicitly trusts the checkpoint signer set — a declared trust choice,
never a default) may start from it. The checkpointed **snapshot itself is
retrievable**: the canonical materialization is written to the artifact store, and the
checkpoint event carries its hash and location, so a fresh reader fetches the snapshot,
verifies it against the signed checkpoint, and starts — without first rebuilding the
very state the checkpoint was meant to spare it. The materialization format is
specified and versioned; a fresh clone's verification obligations are documented, not
implied. Checkpoints never truncate retained history.

**Genesis and halt.** A ledger begins with a signed `genesis` event naming the initial
governance root (operator keys) and the protocol version. `halt.declared` stops
admission of all events except an operator's `halt.lifted` — enforced *at the admission
boundary* (§2), which is what makes it real: a writer on a pre-halt tip, or one that
bypasses the client entirely, is stopped where events enter history, not by its own good
manners.

**Why git.** Offline-capable reads and drafts, replication by the same channels as the
code, host-agnostic storage, inspectable with universal tooling. The ledger *data* is
portable git data forever, and inherits the repository's backup, access-control, and
disaster-recovery story. What git alone does not provide is enforcement — which is the
next section, and the honest edge of "a team that can host a git remote can host the
system": the remote must also run (or front) the admission validator.

### 2. The Admission Boundary

The admission boundary is the trusted component that makes every protocol rule real. It
is deliberately tiny: a **stateless validator** with no unique durable state — everything
it knows is derived from the ledger and the repository, and a replacement instance
rebuilds from a clone.

**What it does (normative).** For every proposed event (or batch), admission MUST:
verify the signature against the enrolled keyring; check the actor's capability grants
cover the verb; check the fence against the current claim; check the transition against
the spec; check schema and data classification; check protocol version; check halt
state; check budget reservation where the verb spends (§9); then admit atomically onto
the authoritative ref — or refuse with a structured, attributable rejection. Admission
is where ordering happens: the admitted chain *is* the order.

**Postures.** Admission is a role with three named deployments; every deployment MUST
declare which it runs:

- **Enforced, self-hosted** — **Reference:** a git server-side receive hook on the
  ledger ref. The remote refuses any push whose events fail validation; force-updates
  and deletions of the ref are refused outright.
- **Enforced, forge-hosted** — where server hooks aren't available, the ledger ref is
  written only by a dedicated admission identity (a small service or serverless endpoint
  that actors propose to), and forge protections make that identity the ref's sole
  writer. The service holds no durable state.
- **Cooperative** — no server-side enforcement; every writer self-validates. Supported
  for small high-trust teams, but it is a **declared mode with a named consequence**:
  the security invariant (§I.2) does not hold, protocol rules are advisory against a
  hostile credential, and the preflight tool and README MUST say so in plain words.
  Cooperative mode is a stance you choose, never a default you fall into.

**What this changes elsewhere.** Halt, fences, capability checks, and transition
legality stop being claims about client behavior and become properties of admitted
history. The git credential story also sharpens: an actor's credential allows
*proposing* to the ledger and pushing to its own authorized code branches — it never
allows writing the ledger ref directly in enforced modes.

### 3. Streams: the hot path and the ledger

A single ref appended by every actor for every event is a global mutex: with hundreds of
workers, heartbeats and per-run metering would force every unrelated verdict to race the
same tip. The remedy is a classification of traffic, not a second database:

- **Coordination facts** — contract lifecycle, claims, parks, reaps, verdicts,
  enrollment, grants, budget reservations and settlements, promotions — are **admitted
  ledger events**. Low frequency, high value, globally ordered.
- **Observations** — liveness, progress, fine-grained metering — are **ephemeral
  streams**: written to a non-authoritative channel (a per-executor observation ref, a
  local socket to the supervisor, or the adapter's own telemetry), read by the
  supervisor and projections, and *summarized* into ledger facts at material
  transitions: `claim.taken`, `progress.milestone` (coarse, bounded frequency),
  `wedge.declared`, `run.settled` (aggregate metering at run end), `park`/`reap`.

Liveness is thus a signal, not a ledger write: a claimant's heartbeat-with-progress flows
on the observation channel every N seconds at near-zero cost; the *ledger* learns about
liveness only when something durable happens. Progress is measured by **monotonic
progress counts** (a completed-item counter that must advance), never by file
modification time — a looping worker rewriting a file looks alive by mtime, and a
legitimate long-running step looks dead. Lease semantics: expiry (no observations) and
wedging (observations without progress advancement) are distinct, visible conditions.
Observation channels are lossy by declaration: losing every observation stream loses no
coordination state.

Where a single admitted ref still contends at scale, admission MAY shard (per-squad or
per-class proposal queues merged by the admission point) — an implementation freedom the
design permits because ordering authority already lives at admission, not in writers.

### 4. Projections

A projection is a deterministic function from a ledger prefix (plus, where declared,
observation streams) to a view. All projections share five properties (normative):
derived (never written directly), stamped (with the ledger position they were built at),
rebuildable (deletion loses nothing), read-only outward, and non-authoritative (no
decision may prefer a projection over the ledger; staleness is visible, never silent).

**Standard projections**: the ready queue (filtered by actor eligibility), contract
detail, actor view, report, a local database cache for single-machine read throughput,
the dashboard, and external mirrors (issue trackers and anything else someone writes an
exporter for).

**Inbound flow (normative).** Input arriving at a projection surface — an edited mirror
issue, a dashboard button — enters the system exclusively as a *request event* proposed
by a governed identity, validated at admission like everything else. There is no path by
which an external system's database writes coordination state, and no component holds
both an export path and a coordination write path.

**External facts.** Mirrors of external authorities (forge merge state, CI results)
enter the ledger as **observations** (`merge.observed`, `check.observed`) recorded by
governed observers — the ledger never pretends to control what it can only watch (§8).

**Why there are no pluggable authorities.** Seed deliberately has no notion of an
external system (issue tracker, project-management SaaS) acting as the coordination
authority. Supporting one requires capability negotiation, emulated atomicity, and a
permanent two-way reconciliation burden — a consensus problem in integration clothing.
Exporters and request adapters deliver everything such integrations actually need at a
fraction of the complexity, and a local cache projection delivers machine-local speed
without a second authority.

### 5. Identity, threat model, and qualification

**Four things, kept distinct.** An **identity** is a keypair. A **credential** is what
that key can reach (git scopes, API tokens). A **principal** is who or what is behind it
(a person, a team's bot, a vendor's runtime). A **runtime** is the actual executing
configuration — model and version, harness and version, tool policy, environment image.
A signature proves the identity signed; it proves nothing about the principal's intent
or the runtime's integrity. Every claim the system makes is scoped to what signatures
actually prove; everything beyond that is threat-modeled, not assumed.

**Enrollment.** `actor.enrolled` carries the public key, the *asserted* kind (`human`,
`agent`, `service` — an assertion by the enrolling operator, not a cryptographic fact,
and documented as such), and display name. Agent enrollment requires an identity plus a
scoped git credential; no inbound connectivity or registration server. Key rotation and
revocation are events; a revoked key's history remains attributed to it; revocation is
drilled (§16).

**Capabilities.** Grants are events: claim rights by squad and tier, verdict rights,
operator rights, curation-proposal rights. Admission checks grants on every verb.
Operator-only verbs refuse non-operator keys structurally.

**Qualification binds to the runtime, not the key (normative).** What passes an eval is
not "an agent" — it is a **configuration tuple**: principal, harness and version, model
family and version, tool policy, environment profile. Grants cite the qualified tuple;
an actor invoking a materially different configuration than its grant cites is out of
grant, and configuration is part of what executor adapters report (§9). Spot-check evals
re-test active tuples; drifted or failing tuples get grants suspended by the supervisor
— an attributable event, no operator ceremony.

**The compromised-actor consequence.** Under §I.2, a stolen key + credential is assumed.
What bounds the damage: admission (no invalid transitions, no impersonation — other keys
are uncompromised), capability scoping (only its authorized surface), budget reservation
(only its reserved spend), verification (its lies are submitted, not believed), and
revocation (detection ends it). The design accepts that the compromised actor can
produce signed garbage attributed to itself — and makes that the *ceiling* of what it
can do.

### 6. Work contracts

The unit of work is a **contract**: intent plus machine-checkable acceptance, plus tier,
budget, and routing.

**Birth.** A contract is created by `intent.filed` and becomes claimable only when it
carries: intent prose (data, never instructions); an **acceptance spec** (below); tier,
budget, and routing.

**Acceptance specs are privileged code (normative).** An acceptance spec ultimately
causes command execution on a verifier. It is therefore a protected artifact: spec
authorship above the trivial tier passes the same review gate as code; a mirror-derived
intent can *propose* acceptance content but a dispatcher's draft becomes executable only
through that gate; and the verifier executes specs in a sandbox with declared, minimal
capability. No text that arrived from outside the trust boundary becomes an executed
command without a gate between.

**Sealed checks** (above the trivial tier) — a designed subsystem, not a phrase:

- **Commitment**: the ledger records a commitment (hash of the canonical plaintext plus
  salt) proving the checks predate implementation.
- **Confidentiality**: the check body is encrypted to the *current verifier keyring* and
  stored in the artifact store (mutable ciphertext, immutable commitment) — so keyring
  rotation re-encrypts without touching history, and a compromised verifier key triggers
  rotation plus re-encryption, with exposure bounded to contracts open during the
  compromise window.
- **Authoring isolation**: sealed checks are authored under a grant disjoint from
  implementation grants, and the authoring channel is fenced from implementer-visible
  surfaces.
- **Honest scope**: sealed checks are **defense-in-depth against specification gaming,
  not a structural solution to it**. An implementer can still infer likely checks,
  overfit the visible spec, or exploit verifier weaknesses. The load-bearing defenses
  remain independent evidence, invariant and property-based tests, adversarial cases,
  and human review where the spec itself is incomplete; sealed checks raise the cost of
  aiming at the proxy.

**Lifecycle.** States — `backlog`, `ready`, `in_progress`, `review`, `done`, `blocked`,
`cancelled`; claim is the ready→in_progress *transition*, not a state — are the
projection vocabulary of the event stream, with transition rules as self-validating data
enforced at admission. Every exit from `in_progress` is deliberate (submit, release,
park, reap); silent abandonment is impossible by construction.

**Claims and the offline boundary (normative).** `claim.taken` carries a fence; every
subsequent event on that contract from that claimant cites it; stale fences are refused
at admission. Claiming is **online-only**: exclusivity is a property granted at
admission, and two offline actors "claiming" the same contract have not claimed anything
— they have drafted proposals, one of which will lose *after* both did the work. The
offline boundary is therefore explicit: reading, planning, drafting, and continuing work
on an already-admitted claim are fully offline-capable; verbs that take exclusivity or
spend reservation are not — unless a squad explicitly opts into **racing mode**
(duplicate execution tolerated, first *verified* success settles — a deliberate
compute-for-latency trade, declared per squad, never an accident of connectivity).

**Plans as falsifiable change contracts.** Above the trivial tier, claiming an unplanned
contract authorizes planning only; a plan is a file merged through its own single-file
PR, and it MUST be *falsifiable*: it names the boundary set (what is broken and will be
shown fixed), the retention set (what works and will be shown unharmed), the validation
commands for both, and the expected shape of the diff. A plan without a retention check
does not lint. Plan PRs and implementation PRs are structurally disjoint, enforced by CI
classification.

**Handoff packets (normative).** The packet is the *only* interface between executors.
Four bounded, mechanical parts: (1) acceptance criteria; (2) settled decisions and
constraints, **each marked verified or asserted** (an unmarked assertion shields
upstream errors from review); (3) artifact references, **commit-anchored**
(`path @ commit` or ref + range — bare paths assume a shared filesystem that disposable
executors don't have), including the diff vs. merge-base or a range producing it; (4)
**investigation findings** — the negative knowledge a successor must not rediscover:
approaches tried and why they failed, hypotheses ruled out, with pointers into durable
artifacts where they exist. Packets are written on every deliberate exit and every reap,
size-bounded, shape-linted, and their sufficiency is drilled (§16) — including the
assertion that a successor does *not* re-try recorded dead ends.

### 7. Dependencies, routing, and escalation

Dependencies cascade: closing a contract unblocks dependents; plan merges unpark
plan-blocked contracts; every unblock emits a wake through the affected party's adapter
**where a wake channel exists** — an accelerator per §9, never the correctness path: a
poll-only worker discovers the unblock from the queue, losing only latency. Holds cascade down subtrees and suppress wakeups; initiative trees roll up
progress. Priorities and squads route; ready-queues filter by eligibility; goal-ancestry
warnings fire when open work cannot trace to a stated mission. Escalation is
`blocked(needs-you)`: an event addressed to a human gate carrying the packet, the
question, and the minimal decision — never a transcript.

### 8. The verdict pipeline

**Done is a verdict — and a reconciliation.** An implementer's strongest act is
`submission.made`. A verifier — verdict grant, key-disjoint from every implementing key
on this contract — checks out the submission in a clean per-run workspace, runs the
visible spec, unseals and runs the sealed checks, recomputes the receipt (plan hash at
merge-base, diff hash, changed-file inventory, transcripts, environment fingerprint),
and renders a signed `verdict.rendered`.

Because the verdict and the merge live in **two systems that share no transaction** (the
ledger and the forge), "done" is an explicit reconciliation, never a pretended atomic
step:

```
verdict.rendered(pass) → merge.requested → merge.observed → done
```

Each arrow is its own event; `merge.observed` is an observation of forge fact by a
governed observer; divergence (verdict passed but merge failed; merge happened without a
verdict; force-push detected on the target) is a first-class detected state that
maintenance surfaces and reconciles — never a silent assumption. A red verdict is
unmergeable via required checks and locks the implementer out of self-approval paths.

**Independence is failure-domain separation (normative).** Distinct keys are necessary
and insufficient: implementer and verifier can share a model, a poisoned dependency, a
prompt template, a runner image — and same-model re-reading of the same text adds no
information. The design defines **independence levels**, declared per tier: **L1** —
distinct keys and workspaces (minimum, always); **L2** — distinct runtime tuples
(different model family or provider, different harness image); **L3** — deterministic
verification first (tests, assertions, diffs) with model judgment only in the residue,
plus distinct evidence-acquisition paths. High-consequence contracts require the level
their tier declares, and the verdict records which level was actually achieved.

**Qualitative residue.** Acceptance that cannot be a command (tone, judgment, taste) is
a rubric the verifier scores item-by-item with cited evidence and explicit uncertainty —
never a single holistic score; low-confidence items route to human verdict; rubric
calibration runs against a human-scored gold set with automatic authority suspension on
drift (§16).

**Protected, with a governance root.** Verifier code, rubrics, thresholds, the sealed
keyring, and the admission rules live on the protected surface. No agent key can modify
the gates that judge its own lane — and the surface *can* be changed, by the governance
root (operator keys + owner review), through PRs, visibly. Self-modification is routed,
not denied.

### 9. The supervisor

**Offers, not assignments; pull with advisory wake (normative).** The supervisor
**publishes offers** (eligibility-scoped: this contract, these tiers, these tuples, this
budget reservation available); workers **pull and claim**; and the claim settles at
admission — first valid claim wins, exactly like any claim. "Wake" is advisory transport
("offers exist for you"), never the grant itself; a worker that never receives a wake
and simply polls loses nothing but latency. Failover and duplicate-scheduling semantics
follow: there is no assignment to orphan, only offers that get claimed or expire. No
coordination feature requires inbound connectivity to any executor.

**Scheduling inputs**: priority, tier, grants and qualification tuples, remaining
budget, concurrency caps, and executor cost class (mechanical work to cheap tuples, hard
work to strong ones).

**Budgets are reservations, not observations (normative).** After-the-fact metering
cannot enforce a cap: two workers can each observe $10 remaining and each spend $8.
Spending verbs therefore require an admitted `budget.reserve` (checked and decremented
at admission — the one place with a serialized view), execution runs fenced against the
reservation, and `budget.settle` (or release) records actuals from adapter metering.
Where a provider cannot be stopped synchronously, the budget is honestly a **risk limit,
not a guarantee** — declared per adapter, surfaced in the report, never fudged.
Remaining reservation is visible to the worker in its envelope, enabling budget-aware
strategy; exhaustion produces a deliberate park with packet.

**Executor adapters** implement provision (workspace + packet), wake (advisory channel),
and meter (observation-stream usage, settled to the ledger at run end), and report the
runtime tuple actually provisioned (qualification depends on it, §5). Substrates: local
worktree, container, cloud agent session, ephemeral VM, enrolled remote worker.
Disposal begins only after the last confirmed, admitted synchronization.

**Preemption** is graceful-first: interrupt at a safe point → park with packet;
force-kill is the fallback and still yields a reap packet. Safe-point semantics (the
executor checks for interrupts at bounded intervals and exits deliberately) are part of
the worker contract, because reaping a live worker and B-style timeout fallbacks
presuppose them.

### 10. Affordances

Every verb response is an **affordance envelope**: the result, plus the verbs currently
legal for this actor on this subject, computed from the same spec and rules admission
enforces — one rule set, two consumers (advisory computation in the envelope,
authoritative enforcement at admission), zero drift by construction. Envelopes are
versioned, schema-stable JSON with structured errors and meaningful exit codes, and
carry the ledger position they were computed at, so a concurrent change is detectable
rather than mysterious. The CLI is the complete interface; a machine-protocol surface
(e.g. MCP) exposes the same verbs with identical semantics. Refusal rates are tracked —
a rising rate signals an affordance gap, not agent error. Relevant promoted knowledge
(lessons whose applies-when matches) surfaces in the packet and envelope at claim time,
because knowledge nobody is shown at the right moment is knowledge that doesn't exist.

### 11. Lanes

Six lanes; each a role (grants + conventions), not a binary; any qualified tuple can
staff any lane it holds grants for; every lane's inputs and outputs are events.

1. **Dispatcher** — converts intents and mirror requests into routed contracts with
   *draft* acceptance specs (executable only after the spec gate, §6). Touches the most
   untrusted text, so it runs with least standing capability and its input handling
   passes the injection conformance suite.
2. **Planner** — falsifiable plan PRs; strongest tuples by policy; unedited-approval
   rate tracked (a wrong decomposition poisons everything downstream, so plan quality is
   the first place capability is spent).
3. **Implementer** — claim, observation-stream progress, minimal diffs, deliberate
   exits, evidence gathered before submission.
4. **Verifier** — verdicts at the declared independence level; never an implementing key
   on the same contract.
5. **Curator** — the offline learning loop (§12); proposes everything, approves nothing.
6. **Maintenance** — reaps (expired and wedged), reconciles verdict/merge divergence,
   rebuilds projections, runs lints and checkpoints, files defect contracts; unattended,
   scheduled, and itself fully audited as an ordinary actor.

**Escalation.** Any lane can raise `blocked(needs-you)`: packet + question + minimal
decision; the supervisor routes it, the report surfaces it with age, and nothing else
about the contract moves until it is answered. A human's unit of interruption is one
decision, never a transcript.

### 12. The curator and the flywheel

**Evidence first.** Online lanes append evidence and never conclusions; the write
boundary is a grant.

**A poisoning-resistant pipeline (normative).** Trajectories are *untrusted inputs* — an
attacker who can influence what agents experience can construct trajectories designed to
teach the system something false. The pipeline is therefore staged, with distinct
storage and distinct gates between stages:

```
observations → hypotheses → validated lessons → policy
```

- **Hypotheses** are the curator's cross-trajectory comparisons: candidate lessons with
  applies-when conditions, supporting contract ids, exceptions, provenance. Minimum
  support: more than one non-failed trajectory, from more than one actor where the
  family allows it; a single accidental success is structurally non-promotable. Workers
  append **candidate observations only** — a worker promoting its own single run would
  violate the support rule by construction, so promotion authority is not grantable to
  implementing lanes.
- **Validation** runs the hypothesis against held-out evidence and, for
  behavior-changing lessons, an adversarial evaluation: does the lesson survive
  deliberately constructed counter-trajectories? Conflicting evidence is a first-class
  contested state (never silently averaged; contested lessons do not surface in
  envelopes).
- **Promotion** is a PR through the normal gates, routed to the correct carrier
  (knowledge doc / role or skill patch / workflow / harness change), each on the
  protected surface where behavior-changing. The curator proposes; gates dispose.
- **Expiry and rollback**: every promoted lesson carries a last-validated stamp and an
  expiry-for-revalidation; retirement revokes conclusions and keeps evidence; a promoted
  lesson implicated in a regression rolls back by reverting its PR — one command,
  because it was a PR.

**Dead ends** record failure condition and environment, and can be un-retired when the
environment changes; the curator checks dead-end applicability, not just lesson
applicability.

**The workflow flywheel.** Recurring trajectory shapes are detected from the ledger,
drafted as deterministic workflows, validated in mock, proposed as PRs; a failing step's
repair role completes within bounds and proposes the workflow patch as a PR — the
registry is protected, silent self-modification impossible. Conversion rate (recurring
chores → merged workflows) is a tracked metric. Every chore an agent does twice becomes
infrastructure — through gates.

### 13. Workflows and determinism

The workflow engine runs DAGs of steps (AI, command, gate-only, loop groups) with typed
inputs, schema-enforced outputs, dependency waves, triggers, retries, failure policy,
wall-clock budgets, and checkpoint/resume that refuses mixed-graph resumes. Gates cover
the trust boundary: approval (pause → response → resume), review (verdict loop with
remediation and max revisions), checks (CI green + zero unresolved threads via forge
adapters). **Mock mode is total** — zero credentials, zero side effects, schema-valid
stubs, recorded-not-executed commands, auto-passed gates — so every workflow is testable
in CI. Validation is exhaustive (schema, ids, acyclicity, closures, token lint, loop
rules) and refuses invalid graphs. Secrets are vault-indirect, resolved at run time,
never frozen into definitions, never echoed to logs. Definitions change only by PR; the
registry is on the protected surface. The engine is deliberately DAG + gates:
statechart semantics (hierarchical/history states) are rejected as expressiveness the
validation suite would have to chase, revisited only if a real workflow cannot be
expressed.

### 14. Guardrails and governance

- **Tiers** gate what an actor may do without a plan or an operator, declared per-squad
  and per-path in checked-in config.
- **The protected surface** is enumerated in config — transition spec, admission rules,
  guardrails, verifier code/rubrics, sealed keyring, curator gates, role definitions,
  supervisor policy, the check pipeline's own definitions (build entrypoints, CI
  workflow files, tooling scripts) — changed only by the governance root through PR +
  owner review, and write-denied to every agent key whose work it gates; a capability
  audit proving disjointness runs in CI. The residual is stated honestly: ordinary test
  *content* outside the protected surface remains in an implementer's write scope, and
  diff-vs-plan review plus sealed checks are the mitigations.
- **Data/instruction defense-in-depth**, concretely: capability restriction first (the
  reader of untrusted content is the least-privileged actor in the system); provenance
  typing on prompt channels (trusted instructions vs. quoted content, structurally
  distinguished in prompt assembly); unforgeable delimiters on interpolated prose;
  strict-shape validation on anything interpolated into commands; sandboxing and network
  policy on executors; secret isolation (credentials never in any context window). The
  documentation says what this does *not* solve — a model can still be persuaded by
  adversarial text it reads — which is why capability bounds, not fencing, carry the
  invariant.
- **Budgets**: reservation-based (§9), real-spend settled, org/actor/contract
  granularity, risk-limit honesty where enforcement is impossible.
- **Per-verb policy** on the machine-protocol surface: allow/deny/require-approval by
  actor and risk class; approvals as request events resolved attributably in an operator
  inbox.
- **Forge-side protections as declared desired-state**, reconciled by command: branch
  protection with required checks, ledger ref writable only by admission (enforced
  modes), immutable release tags. Scheduled/CI identities are least-privilege; ledger
  admission runs under a dedicated identity; no scheduled job can push to the default
  branch.
- **Boundary + retention for process changes**: any change to a role file, guardrail,
  rubric, or workflow names the failing case it fixes and demonstrates prior cases still
  pass. A fix validated only against its trigger case is not accepted.

### 15. Interoperability and federation

Any coding harness staffs any lane — the contract is files, a CLI, and envelopes, never
a vendor SDK. Per-harness configuration fans out from single sources; drift fails CI;
fan-outs are never hand-edited. **Lifecycle documentation is generated from the
transition spec** through the same fan-out, so prose about the state machine
structurally cannot drift from the machine; design docs carry `last-verified` stamps
with a staleness lint. The core loop runs on any git remote that can host the declared
admission posture; forge extras (PR gates, protections reconciler, mirrors) are
adapters, with at least one non-primary forge supported. Cross-organization
collaboration is opaque (capability cards, a task lifecycle state machine,
artifacts-only exchange — A2A-shaped); prompts, reasoning, and internals never cross a
trust boundary. Federation is projection-only: one ledger per repository, org-level
views read across ledgers (remotes addressed uniformly), cross-repo work enters as
request events into the target ledger; there is no super-ledger, because there is no
super-authority. Models are registry data and scheduling inputs, never architecture.

### 16. Evaluation, drills, and the red team

Evaluation infrastructure is part of the system, not tooling around it — qualification,
calibration, and the security invariant all depend on it. This includes evaluating
*changes to the system itself*: a role file, guardrail, or rubric change is validated by
replayable evidence, not only by review.

- **Eval contracts**: synthetic work with known verdicts, fixture repos, run through the
  production machinery; passing gates grants for the *tuple* that ran them; scheduled
  spot-checks re-test active tuples.
- **Verifier calibration**: scheduled sampling against a human gold set; automatic
  authority suspension on drift; defect contract filed.
- **The compromised-actor drill**: a red-team harness plays the §I.2 adversary — valid
  key, valid credential, arbitrary git and event forgery — against a fixture deployment
  in enforced mode, and asserts the invariant's ceiling: no unauthorized claim, no
  self-approval, no history rewrite, no gate modification, no impersonation, no
  over-lease, no invalid transition admitted, no spend beyond reservation. **This drill
  is the architecture's definition of done, and it runs in CI.**
- **Standing drills**: full projection rebuild from genesis; checkpoint verification;
  packet-resume with randomized executor kills (asserting recorded dead ends are not
  re-tried); claim race storms converging with no lost updates; halt enforcement across
  all writer paths *including a client-bypassing raw-git writer* (enforced mode); key
  revocation with sealed-keyring rotation; verdict/merge divergence reconciliation;
  data-classification lint against a hostile payload corpus; budget reservation races;
  curator poisoning (constructed trajectories fail to achieve promotion).
- **Trajectory-prefix regression** for lane behavior: recorded decision points (e.g.,
  "about to declare done without running checks") replay against lane configurations to
  catch behavioral regressions in role or prompt changes.
- **Simulation mode**: the whole system runs end-to-end against synthetic intents with
  mock executors and zero credentials.

### 17. Distribution, supply chain, adoption

- **Template + pinned engine.** Adopting is clone-and-init from a tagged release; the
  engine is a pinned release fetched by a bootstrap shim, checksum-verified from a
  checked-in lock, cached outside the repository, never committed; vendored paths serve
  air-gapped use. Engine upgrades verify checksums, preflight protocol compatibility,
  refuse downgrades, and rewrite the lock atomically.
- **Everything hash-pinned.** Engine, skills, workflow dependencies; nothing executes
  from an unpinned source.
- **The admission validator ships with the engine** in hook form and as a deployable
  stateless artifact (service form); both rebuild entirely from a clone.
- **Preseed.** One declarative file bootstraps a new adoption — config, guardrails,
  teams, protections desired-state, and the declared admission posture — idempotently
  and CI-verifiably.
- **Genesis first.** A new deployment starts from a signed genesis event; that is the
  primary story. **Importing a predecessor** (Appendix D) is an adopter path: a lossless
  export from the prior system transforms to a genesis ledger — historical records
  become events, prior tamper-evidence is verified before conversion — and import
  refuses non-empty ledgers. Documented, two-command, drilled against a real predecessor
  fixture.
- **Boring install.** One command, no telemetry, no account, no network beyond the
  pinned artifact fetch.

### 18. Deliberately absent

Absence is design; proposals to add these must argue against the reason, not against
forgetfulness:

| Absent | Why |
|---|---|
| Pluggable external authorities | A second writable authority is the root of every sync pathology — a consensus problem in integration clothing. Exporters + request adapters cover the real needs. |
| A separate mail subsystem | A message is an event; threading, unread counts, acks are projections. |
| A separate tamper-evidence mechanism | The signed hash chain is the tamper evidence; signed checkpoints are the recovery points. |
| A separate run log / audit log | The ledger is both. |
| A machine-local authoritative store | The cache projection provides the throughput without a second authority. |
| Client-assigned global sequence numbers | Ordering authority is admitted ancestry; ordinals are derived, not asserted. |
| High-frequency ephemera on the authoritative ref | Liveness and fine metering are observation streams, summarized into ledger facts at material transitions. |
| Terminal-multiplexer (or any wake-channel) coupling | Wake channels are adapter details; no coordination feature assumes any of them. |
| Statechart workflow semantics | DAG + gates covers observed use; chased expressiveness is a cost. |
| Model-vendor coupling anywhere | Models are scheduling inputs and qualification-tuple fields, never architecture. |
| A hosted control plane with unique state | The repository is the control plane. The admission validator is trusted but stateless — everything it knows rebuilds from a clone. |

---

## Part III — Conformance

An implementation is **Seed-conformant at a declared admission posture** when the
criteria below hold and the drill suite (§II.16) passes; criteria are met only when
implemented, tested, documented, and **enforceable** (by admission, lint, CI, drill, or
protocol — not by convention alone). Cooperative-posture deployments MUST additionally
document which criteria (marked *enforced-only*) do not hold for them.

### A. The Ledger

- [ ] Every coordination fact is an event on a single per-repo ledger ref; no second
      writable store of coordination state exists anywhere in the system.
- [ ] Events carry timestamp, actor fingerprint, verb, subject, schema-valid payload,
      previous-event hash, and a signature over the canonical form; the entire chain
      verifies from genesis with one command.
- [ ] Ordering authority is admitted ancestry; no writer asserts a global sequence
      number; ordinals used by projections and citations are derived.
- [ ] Any mutation *within* retained history — reorder, rewrite, interior deletion,
      forgery — is detected by chain verification and (*enforced-only*) refused at the
      remote; the drill proves both with corrupted fixtures and a raw-git adversary.
- [ ] Rollback to a valid earlier tip is treated honestly as a freshness problem, not
      claimed as chain-detectable: clients persist and enforce head monotonicity;
      witness divergence surfaces a rollback on the next fetch by any actor or clone
      that saw the later tip; enforced remotes refuse non-fast-forward; the rollback
      drill proves detection through each path; and the fresh-clone staleness bound
      (newest out-of-band checkpoint or head attestation) is documented.
- [ ] Payload data classification is enforced at admission: coordination facts and
      references only; content bodies live in an addressed artifact store with a
      documented erasure path, referenced by hash; the hostile-payload lint corpus
      passes.
- [ ] Erasure obligations are honorable: erasing a referenced artifact never breaks
      chain verification, and the erasure is itself an attributable event.
- [ ] Checkpoints are signed by maintenance/operator identities, embed the projection
      state hash, and reference a retrievable canonical snapshot in the artifact store
      (fetch → verify against the signed hash → start); the materialization format is
      specified and versioned; a fresh clone's verification
      obligations (full replay once, or explicit trust in the signer set) are documented
      and the choice is declared, not defaulted; replay-from-checkpoint equals
      replay-from-genesis in CI.
- [ ] A genesis event names the governance root and protocol version; version mismatch
      refuses with a distinct exit code.
- [ ] `halt.declared` stops admission of everything except an operator's `halt.lifted`,
      enforced at the admission boundary; the halt drill includes a client-bypassing
      writer (*enforced-only*).
- [ ] Reads, drafts, planning, and work on admitted claims are offline-capable;
      exclusivity-taking and reservation-spending verbs are online-only and documented
      as such.
- [ ] Ledger performance is budgeted and tracked in CI: admission latency, replay time,
      projection rebuild time, against a representative history.

### B. The Admission Boundary

- [ ] A stateless admission validator exists and (*enforced-only*) is the only writer of
      the ledger ref; it validates signature, capability, fence, transition, schema,
      data classification, protocol version, halt state, and budget reservation before
      atomic admission, and refuses with structured, attributable rejections.
- [ ] The validator holds no unique durable state: a replacement instance rebuilds
      entirely from a clone, proven by a kill-and-replace drill.
- [ ] All three admission postures are implemented; every deployment declares its
      posture; cooperative mode's consequences are stated by the preflight tool and the
      README in plain words; no deployment lands in cooperative mode by default.
- [ ] (*enforced-only*) Actor git credentials cannot write the ledger ref directly —
      verified by an attempted direct push in the drill.
- [ ] (*enforced-only*) The compromised-actor drill passes in CI: a valid-key,
      valid-credential adversary with raw git access cannot claim unauthorized work,
      approve itself, rewrite history, alter gates, impersonate another actor, exceed
      its lease, exceed its reservation, or admit an invalid transition.
- [ ] Admission may shard proposal intake without changing semantics; ordering remains
      solely the admitted chain.

### C. Streams and the hot path

- [ ] Traffic is classified: coordination facts are admitted events; liveness, progress,
      and fine-grained metering ride non-authoritative observation channels.
- [ ] Progress liveness is measured by monotonic progress counts, never file
      modification time; long-running steps carry a declared in-step state; the ledger
      records only material transitions (claim, coarse milestones, wedge declaration,
      run settlement, park/reap).
- [ ] Expiry (no observations) and wedging (observations without progress advancement)
      are distinct, visible conditions with distinct reap heuristics, drilled.
- [ ] A contention benchmark demonstrates the target scale (hundreds of concurrent
      actors) without unrelated writes racing each other's admissions pathologically;
      tracked in CI.
- [ ] Observation channels are lossy by declaration: losing every observation stream
      loses no coordination state.

### D. Projections

- [ ] Every read surface is a deterministic function of a ledger prefix (plus declared
      observation inputs), stamped with its build position, and rebuildable
      byte-identically with one command.
- [ ] No code path writes a projection directly or treats one as authoritative; the
      write-boundary lint enforces it.
- [ ] Staleness is visible everywhere projected state is shown; consumers can demand a
      minimum position.
- [ ] The cache projection delivers single-machine read throughput with zero authority
      (mid-operation deletion loses nothing).
- [ ] External mirrors are one-way exporters; mirror-side edits arrive only as request
      events from governed identities, validated at admission; a conformance suite
      passes per exporter/adapter.
- [ ] Bidirectional synchronization is structurally impossible: no component holds both
      an export path and a coordination write path.
- [ ] External facts enter only as observations by governed observers; nothing treats an
      observation as control.
- [ ] The rebuild-everything-from-genesis drill runs green in CI.

### E. Identity, threat model, qualification

- [ ] Identity, credential, principal, and runtime are distinct concepts in the schemas
      and the threat model; no user-facing claim exceeds what signatures prove.
- [ ] Every actor is a keypair; enrollment, grants, suspension, revocation are events;
      the keyring is a projection; admission verifies signatures on every proposal.
- [ ] Enrolled kind (human/agent/service) is documented as an operator assertion;
      nothing security-relevant assumes kind is cryptographically proven.
- [ ] Agent enrollment requires exactly an identity plus a scoped credential; no inbound
      connectivity or registration server.
- [ ] Grants are capability data checked at admission; out-of-grant verbs are refused
      structurally; operator-only verbs refuse non-operator keys; no agent key can
      approve its own work into done — by key disjointness.
- [ ] Qualification binds to the runtime tuple (principal, harness+version, model
      family+version, tool policy, environment profile); grants cite tuples; adapters
      report the provisioned tuple; materially drifted tuples are out of grant.
- [ ] Scheduled spot-check evals re-test active tuples; failures suspend grants
      attributably without operator ceremony.
- [ ] Key rotation and revocation are drilled: revocation ends the key's standing, reaps
      its claims, preserves its history's attribution, and (for verifier keys) triggers
      sealed-check keyring rotation.
- [ ] Humans and machines are distinguished in the roster; agent-only guardrails and
      human/agent metrics read the distinction.

### F. Work contracts and lifecycle

- [ ] A contract cannot leave draft without an acceptance spec; the spec's executable
      content passes a review gate before it can run anywhere; specs are protected
      artifacts.
- [ ] Text originating outside the trust boundary can propose but never directly become
      executable acceptance content; the gate between is structural and tested against a
      hostile corpus.
- [ ] Above the trivial tier, contracts carry sealed checks: ledger commitment (salted
      hash) proving pre-existence; ciphertext encrypted to the current verifier keyring
      in the artifact store; keyring rotation re-encrypts without touching history;
      authoring grants disjoint from implementation grants; a capability audit proves no
      implementer path can decrypt.
- [ ] Sealed-check documentation states their honest scope (defense-in-depth against
      specification gaming, not a structural solution) and names the load-bearing
      defenses beside them.
- [ ] The lifecycle vocabulary and transition rules are self-validating data enforced at
      admission; claim is a transition, not a state.
- [ ] Claims are exclusive with fencing, granted only at admission (online); stale
      fences refuse with distinct codes; contention returns structured envelopes.
- [ ] Racing mode exists only as an explicit per-squad opt-in with its compute cost
      stated; offline "claiming" is impossible by construction.
- [ ] Every exit from in_progress is deliberate; every involuntary exit leaves a packet;
      silent abandonment is impossible.
- [ ] Packets contain exactly four bounded parts — acceptance criteria;
      verified/asserted-marked settled decisions; commit-anchored artifact references
      (including the diff vs. merge-base or a range producing it); investigation
      findings — shape-linted and size-bounded.
- [ ] Packet sufficiency is drilled: a fresh executor completes a killed executor's
      contract from the packet alone, including not re-trying recorded dead ends
      (asserted by the drill).
- [ ] Plans are falsifiable: boundary set, retention set, validation commands for both,
      expected diff shape; missing retention fails lint; plan and implementation PRs are
      structurally disjoint.
- [ ] Dependencies cascade, with advisory wakes where a channel exists (polling
      remains the correctness path, consistent with the wakeless run in H); holds
      cascade with suppression; initiative rollups render; goal ancestry warns.

### G. Verdicts and evidence

- [ ] "Done" is reachable only through the reconciliation chain
      `verdict.rendered(pass) → merge.requested → merge.observed → done`; each step is
      its own event; no code path collapses them.
- [ ] Verdict/merge divergence (verdict without merge, merge without verdict, target
      force-push) is a detected, surfaced, reconciled state — drilled by inducing each
      divergence in CI.
- [ ] Verdicts are signed by verdict-granted keys provably disjoint from every
      implementing key on the contract; operator override is its own attributable verb,
      never a disguised verdict.
- [ ] The verifier executes in clean per-run isolation; parallel verdicts never collide;
      cleanup fires pass or fail; the verifier's verdict inputs are enumerable and
      exclusively self-executed or self-read (no implementer-claims channel).
- [ ] Receipts bind contract id, plan hash at merge-base, diff hash, changed-file
      inventory, visible and sealed check transcripts, and environment fingerprint;
      verification recomputes everything from the submission head and fails on mismatch.
- [ ] Independence levels L1–L3 are defined, declared per tier, enforced at verdict
      time, and recorded in the verdict; high-consequence tiers require runtime-tuple
      separation (L2) or deterministic-first verification (L3).
- [ ] A red verdict is unmergeable and locks the implementer out of self-approval until
      a new submission.
- [ ] Rubric verdicts score per-item with cited evidence and explicit uncertainty;
      low-confidence items route to human verdict; calibration runs against a human gold
      set with automatic authority suspension on drift.
- [ ] Verifier code, rubrics, thresholds, sealed keyring, and admission rules are on the
      protected surface; the governance root and its change process are named in config;
      the capability audit proves agent-key disjointness in CI.
- [ ] Evidence, receipts, and verdicts are queryable by contract, actor, time, and
      outcome.

### H. Supervisor and execution

- [ ] The scheduling model is offers-and-claims: eligibility-scoped offers; workers pull
      and claim; claims settle at admission; wake is advisory transport whose total
      failure costs only latency — proven by a wakeless (poll-only) CI run.
- [ ] Offers expire; there are no assignments to orphan; duplicate scheduling is
      impossible because exclusivity settles at admission.
- [ ] Spending verbs require an admitted `budget.reserve`; execution is fenced to the
      reservation; `budget.settle`/release records actuals; concurrent over-spend
      against one budget is structurally impossible for reservable resources — drilled.
- [ ] Where a substrate cannot stop spend synchronously, the budget is documented and
      surfaced as a risk limit, not a guarantee, per adapter.
- [ ] Remaining reservation is visible in the worker's envelope; exhaustion produces a
      deliberate park with packet, never a vanished worker.
- [ ] Executor adapters implement provision/wake/meter, report the provisioned runtime
      tuple, and exist for at least: local worktree, container, cloud session, enrolled
      remote worker; the adapter interface is public.
- [ ] Metering flows on observation channels and settles to the ledger at run end; no
      execution path is unmetered.
- [ ] Disposability after last admitted synchronization is drilled with randomized
      kills; the loss window is bounded by declared sync policy and stated honestly.
- [ ] Preemption is graceful-first with specified safe-point semantics in the worker
      contract; force-kill still yields a reap packet.
- [ ] Scheduling inputs include cost class and qualification tuples; routing is data.

### I. Affordances and the actor interface

- [ ] Every verb response is a versioned, schema-stable envelope with structured errors
      and meaningful exit codes, includes the verbs currently legal for this actor on
      this subject, and carries the ledger position it was computed at.
- [ ] The affordance computation and admission enforcement consume the same rule set;
      an affordance-listed verb refused at admission for legality (absent a concurrent
      event, detectable via the position stamp) is a bug class with a regression test.
- [ ] The CLI is the complete interface; the machine-protocol surface exposes identical
      semantics; platform parity (including Windows) is documented and tested.
- [ ] Refusal rates are tracked as an affordance-gap metric.
- [ ] Matching promoted lessons surface in packets and envelopes at claim time.

### J. Lanes

- [ ] Role definitions exist for all six lanes as grants + conventions, composable from
      ordered fragments, resolved and checked by validation.
- [ ] The dispatcher runs with least standing capability and passes the injection
      conformance suite (hostile corpus: embedded instructions in intents, mirrors, and
      tool output are quoted as data, never obeyed).
- [ ] Dispatcher re-triage rate and planner unedited-approval rate are tracked; the
      planner lane receives the strongest tuples by policy.
- [ ] Maintenance runs green unattended — reaps, divergence reconciliation, projection
      rebuilds, lints, checkpoints — and is audited as an ordinary actor.
- [ ] Escalations carry packet + question + minimal decision; waiting escalations
      surface with age; resolution latency is tracked.
- [ ] Small-team mode (one *principal* operating a minimal set of actor identities —
      at least an implementing actor and a distinct verifying actor, so verdict key
      disjointness holds even when one person runs everything) and fleet mode
      (disjoint actors per lane) both run the full loop in CI.

### K. Curation, memory, and the flywheel

- [ ] Online lanes append evidence only; conclusion-writing is grant-gated to the
      curator's proposal path; workers append candidate observations, never promoted
      lessons.
- [ ] The pipeline is staged with distinct storage and gates: observations → hypotheses
      → validated lessons → policy; no stage skips.
- [ ] Promotion requires applies-when conditions; support from >1 non-failed trajectory
      (and >1 actor where the family allows); provenance links; last-validated stamp;
      and — for behavior-changing lessons — survival of an adversarial evaluation
      against constructed counter-trajectories.
- [ ] Trajectories are treated as untrusted inputs; the poisoning drill fails to achieve
      promotion in CI.
- [ ] Conflicting evidence is a first-class contested state, never silently averaged;
      contested lessons do not surface in envelopes.
- [ ] Lessons expire for revalidation; retirement revokes conclusions and keeps
      evidence; a lesson implicated in a regression rolls back by reverting its PR.
- [ ] Dead ends carry failure condition and environment and can be un-retired on
      environment change.
- [ ] The flywheel closes through gates: recurring shapes → drafted workflows → mock
      validation → PR; repair roles propose patches as PRs; conversion rate is tracked.
- [ ] Knowledge bloat is managed: dedup with provenance, staleness flags, structure
      lint.

### L. Guardrails and governance

- [ ] Tiers gate un-planned/un-operator'd action per-squad and per-path.
- [ ] The protected surface is enumerated in config, includes the admission rules and
      the check pipeline's own definitions, is changed only by the named governance root
      via PR + owner review, and is write-denied to every agent key it gates —
      capability audit in CI; the test-content residual is documented with its
      mitigations.
- [ ] Data/instruction defense is layered and documented with its limits: least
      capability for untrusted-content readers (primary), provenance-typed prompt
      channels, unforgeable delimiters, strict-shape command interpolation, sandboxing,
      network policy, secret isolation; the hostile corpus passes on every release.
- [ ] Per-verb policy governs the machine-protocol surface with attributable approvals.
- [ ] Forge protections are declared desired-state and reconciled: required checks,
      admission-only ledger writes (*enforced-only*), immutable tags; scheduled/CI
      identities are least-privilege.
- [ ] Process changes pass boundary + retention, lint-checked for declared sets.

### M. Workflows

- [ ] DAG engine with typed inputs, schema-enforced outputs, waves, triggers, retries,
      failure policy, budgets, and safe resume; approval/review/check gates; total mock
      mode; exhaustive validation; vault-indirect secrets; PR-only definition changes on
      a protected registry.

### N. Interoperability and federation

- [ ] Harness fan-outs from single sources with drift-failing CI; lifecycle docs
      generated from the transition spec; `last-verified` stamps with staleness lint.
- [ ] The core loop runs on any git remote supporting the declared admission posture;
      at least one non-primary forge is supported by adapters.
- [ ] Cross-org collaboration is opaque (capability cards, task lifecycle,
      artifacts-only).
- [ ] Federation is projection-only: uniform read remotes, request-event ingress, no
      cross-ledger write path — proven by its absence.
- [ ] No worker-contract dependency on any model vendor.

### O. Evaluation infrastructure

- [ ] Eval contracts run through production machinery against fixture repos and gate
      tuple qualification; spot-checks run scheduled.
- [ ] Verifier calibration runs scheduled with automatic authority suspension.
- [ ] (*enforced-only*) The compromised-actor drill passes in CI on every release —
      the architecture's own definition of done.
- [ ] Standing drills run in CI: projection rebuild, checkpoint verification,
      packet-resume with dead-end assertions, claim race storms, halt (including
      raw-git bypass where enforced), key revocation with keyring rotation,
      verdict/merge divergence, data-classification hostile corpus, budget reservation
      races, curator poisoning.
- [ ] Trajectory-prefix regression covers lane decision points; simulation mode runs
      the whole system credential-free end-to-end.

### P. Distribution, supply chain, adoption

- [ ] Clone-and-init adoption from tagged releases; three-way template upgrades with
      rollback; pinned checksum-verified engine, never committed, air-gap paths;
      checksum/protocol/downgrade-safe engine upgrades; everything executable
      hash-pinned.
- [ ] The admission validator ships in hook and service form, both stateless, both
      rebuildable from a clone.
- [ ] Preseed bootstraps config, guardrails, teams, protections, and the declared
      admission posture in one idempotent, CI-verified file.
- [ ] Predecessor import is a drilled two-command path: lossless export → genesis
      import (refusing non-empty ledgers), source tamper-evidence verified before
      conversion, against a real predecessor fixture in CI.
- [ ] Install is boring: one command, no telemetry, no account.

### Q. Quality, docs, community

- [ ] One fast backpressure command stays green on the default branch.
- [ ] Reference-implementation coverage is measured with a stated floor; scripts and
      hooks carry smoke tests; conformance suites run per exporter/adapter.
- [ ] Docs are governed: operator handbook, generated worker docs, stamped design docs.
- [ ] Adopted ideas are traced to sources and rejections recorded with reasons; §II.18's
      absences are standing rejections proposals must argue against.
- [ ] Decisions are recorded and binding; the authority order is stated.
- [ ] Governance is fork-friendly: permissive license, no CLA, no open-core split,
      stated explicitly.
- [ ] Self-hosting is total once bootstrapped: the system coordinates its own
      development, and every feature ships only after coordinating its own delivery.

### R. The autonomy end-state

- [ ] One-sentence intents become routed contracts whose draft acceptance specs survive
      human review unedited in the majority of cases.
- [ ] Planner plan-PRs pass human review >80% unedited; implementers reach
      verdict-passed submissions unassisted on the happy path.
- [ ] The verifier lane holds quality alone on low tiers; humans review only high-tier
      plans, the protected surface, and escalations.
- [ ] Every escalation is one packet + one question + one decision; transcript-dumping
      is a defect.
- [ ] The system runs unattended for a week on a real backlog with zero chain
      violations, zero lost updates, zero silent abandonments, zero guardrail breaches,
      zero unreserved spend — and the ledger alone reconstructs and justifies
      everything.
- [ ] The flywheel demonstrably compounds over a quarter (chore→workflow conversion,
      packet-resume success, cost per contract), from ledger metrics alone.
- [ ] A team that has never spoken to the authors adopts from the README in under an
      hour, on their forge, with their declared admission posture, and reaches their
      first agent-shipped, verifier-passed, human-reviewed PR the same day.

---

## Appendix A — Glossary

- **Actor** — any enrolled participant: human, agent, or service; identified by a
  keypair.
- **Admission (boundary)** — the trusted stateless validator through which every event
  enters authoritative history (§II.2).
- **Affordance envelope** — a verb response carrying the result plus the verbs currently
  legal for this actor on this subject (§II.10).
- **Artifact store** — addressed storage for content bodies (transcripts, attachments,
  sealed-check ciphertext), referenced from the ledger by hash, with erasure paths.
- **Contract** — the unit of work: intent + acceptance spec + tier/budget/routing
  (§II.6).
- **Fence** — the token bound to a claim; every subsequent event on the claimed contract
  must cite it; staleness refuses at admission.
- **Gate** — a named enforcement point where progress requires an approval outside the
  acting key's control. The charter's gates, concretely: the **spec gate** (review
  approval before acceptance-spec content is executable, §II.6); the **plan gate**
  (plan PR merged before implementation above the trivial tier, §II.6); the **merge
  gate** (forge required checks including the verdict, §II.8); the **promotion gate**
  (PR review on curator proposals, §II.12); the **approval/review/check gates** inside
  workflows (§II.13); and the **governance gate** (owner review on the protected
  surface, §II.14). "Gate", unqualified, always means one of these.
- **Governance root** — the named identities and process (operator keys + owner review)
  that may change the protected surface.
- **Halt** — an operator-declared stop on all admission until lifted.
- **Lane** — a role in the work loop: dispatcher, planner, implementer, verifier,
  curator, maintenance (§II.11).
- **Ledger** — the append-only, hash-chained, signed event log; the authoritative
  coordination record (§II.1).
- **Observation** — (a) a high-frequency ephemeral signal (liveness, metering) on a
  non-authoritative channel (§II.3); (b) a ledger event recording an external system's
  fact (`merge.observed`) without claiming control of it.
- **Offer** — a supervisor-published, eligibility-scoped invitation to claim (§II.9).
- **Packet (handoff packet)** — the four-part bounded interface between executors
  (§II.6).
- **Posture** — a deployment's declared admission mode: enforced self-hosted, enforced
  forge-hosted, or cooperative (§II.2).
- **Projection** — a derived, stamped, rebuildable, non-authoritative view of the ledger
  (§II.4).
- **Protected surface** — the enumerated set of files and rules only the governance root
  may change (§II.14).
- **Racing mode** — per-squad opt-in allowing duplicate execution with first-verified
  settlement (§II.6).
- **Runtime tuple** — the qualified configuration: principal, harness+version, model
  family+version, tool policy, environment profile (§II.5).
- **Sealed checks** — acceptance checks committed in the ledger but encrypted to the
  verifier keyring (§II.6).
- **Squad** — a routing and policy grouping of contracts and actors.
- **Tier** — an autonomy level gating what may happen without a plan or an operator.
- **Verdict** — a verifier's signed pass/fail with receipt; the only path toward done
  (§II.8).

## Appendix B — Wire-level sketch

This appendix sketches the normative shapes; the exact canonical serialization (field
order, encoding, hashing and signature algorithms) is fixed by the versioned protocol
spec that accompanies the reference implementation, and the protocol version in the
genesis event names it.

**Canonical event form (signed):**

```json
{
  "v": "<protocol version>",
  "ts": "<RFC3339>",
  "actor": "<key fingerprint>",
  "verb": "<namespace.verb>",
  "subject": "<contract id | actor id | 'system'>",
  "payload": { "<verb-specific, schema-validated>": "…" },
  "prev": "<hash of previous event's canonical form>"
}
```

`sig` is computed over the canonical form and carried alongside. The chain hash covers
the canonical form including `prev`; the genesis event's `prev` is the empty hash.

**Core verb catalog (namespaces):**

- `system.*` — `genesis`, `halt.declared`, `halt.lifted`, `checkpoint`,
  `protocol.upgraded`.
- `actor.*` — `enrolled`, `granted`, `suspended`, `revoked`, `qualified` (cites eval
  results and the runtime tuple).
- `intent.*` / `contract.*` — `intent.filed`, `contract.specified` (acceptance spec
  gate passed; sealed commitment), `contract.blocked`, `contract.cancelled` …
- `claim.*` — `taken` (carries fence), `released`, `parked`, `reaped` (packet ref),
  `wedge.declared`.
- `plan.*` — `proposed`, `approved` (observation of the plan PR merge).
- `progress.*` — `milestone` (coarse; bounded frequency).
- `submission.*` — `made` (branch, evidence refs).
- `verdict.*` — `rendered` (pass/fail, receipt, independence level achieved).
- `merge.*` / `check.*` — `requested`, `observed` (external-fact observations).
- `budget.*` — `reserve`, `settle`, `release`.
- `run.*` — `settled` (aggregate metering).
- `message.*` — `sent`, `acked`.
- `request.*` — inbound proposals from projection surfaces (mirror edits, dashboard
  actions).
- `curation.*` — `hypothesis.proposed`, `lesson.promoted` (PR observation),
  `lesson.retired`, `deadend.recorded`.

**Affordance envelope (every verb response):**

```json
{
  "v": "<envelope version>",
  "ok": true,
  "result": { "…": "…" },
  "error": null,
  "position": "<ledger position this was computed at>",
  "affordances": ["verb", "verb", "…"],
  "budget": { "reserved": "…", "remaining": "…" },
  "exit": 0
}
```

Structured errors replace `result` with `error` (machine-branchable code + human
message) and a distinct exit code; contention, stale-fence, version-mismatch, halt, and
out-of-grant each have their own.

**Packet shape:** four parts per §II.6, size-bounded, with `decisions[].status ∈
{verified, asserted}` and `refs[]` entries of the form `{path, commit|range, kind}`.

## Appendix C — Prior art and lineage

Seed is the successor design to **open-seed** (github.com/shaunlmason/open-seed) — and
its heir by name: everything grows from seed. It succeeds a
git-native multi-agent orchestration template, and inherits its production-validated
mechanisms: the synchronous claim/fence protocol with structured contention exit codes;
plan-before-implementation gating with single-file plan PRs structurally disjoint from
task PRs; reviewer-distinct-from-implementer done-gating; protected-path governance
(CODEOWNERS + branch protection + CI validators); handoff packets on every involuntary
exit; mock-total workflow testing; hash-pinned engine distribution with bootstrap
verification; and total dogfooding. Where Seed differs from open-seed, the differences
were argued in open-seed's research corpus (docs/research/01–12) and two design
proposals (one authority with one-way projections; pull-based supervision with
disposability-after-push), then hardened by two adversarial reviews — the first of which
contributed the admission boundary and the compromised-actor invariant, the second the
falsification discipline (divergences recorded beside convergences).

External sources that shaped specific mechanisms:

- Anthropic Frontier Red Team, *Patterns and Problems in Emerging Multiagent Systems*
  (2026) — homogeneous convergence (the "18 of 30 agents, same branch name" result),
  goal-conflict sabotage, and the coordinated-swarm cost/coverage trade.
- Cemri et al., *Why Do Multi-Agent LLM Systems Fail?* (MAST taxonomy, 2025) — the
  three failure classes, especially missing task verification, that the verdict
  pipeline answers.
- Bojie Li, *AI Agents in Depth* (2026) — verification as the loop's bottleneck; the
  dual online/offline learning loop and its three safety boundaries; hidden tests and
  their honest limits; the information-gain criterion for multi-agent value; boundary +
  retention sets.
- Google/Linux Foundation, the **A2A protocol** — opaque cross-organization
  collaboration: tasks and artifacts cross trust boundaries, internals never do.
- The event-sourcing and optimistic-concurrency literature generally; git itself as the
  replication and storage substrate.

## Appendix D — Adoption paths

1. **Genesis (primary).** Clone the template at a tagged release; run init with a
   preseed file declaring config, guardrails, teams, protections desired-state, and the
   admission posture; the signed genesis event names the governance root; enroll actors;
   file the first intent.
2. **Import from open-seed.** Export the predecessor state losslessly; verify its
   tamper-evidence (anchors) against its own history; transform — cards become
   contracts, run-log entries become events, receipts become verdict/receipt records,
   mail becomes message events; import into an empty ledger (import refuses non-empty).
   Two commands, documented, drilled in CI against a fixture.
3. **Import from elsewhere.** Any system that can produce a structured export of work
   items and history can be transformed by the same route; the import boundary is the
   genesis transform, not per-system compatibility code in the core.
