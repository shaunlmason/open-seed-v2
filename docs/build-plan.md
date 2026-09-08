# build-plan.md — Implementing Seed (SEED-NEXT)

> **Scope and authority.** This plan governs sequencing and per-phase acceptance for
> implementing **Seed**, the system chartered in [`SEED-NEXT.md`](../SEED-NEXT.md). For everything under `next/**`, the charter is the design authority and this
> plan is the build order — the same relationship `docs/design-options.md` and
> `docs/build-plan.md` have for the v1 template. The charter's Part II is normative;
> its Part III is the conformance checklist every phase's exit criteria cite. Where this
> plan sets a default the charter leaves open, the default is binding until a recorded
> decision changes it.
>
> **Autonomy contract.** This plan exists so agents can implement Seed **without human
> intervention**. Every foreseeable decision has a default below. When you hit one that
> doesn't: (1) prefer the charter's normative text; (2) prefer the smallest reversible
> choice consistent with it; (3) record the choice in `decisions/` inside your task PR
> and continue. Escalate to a human (card → `blocked(needs-you)`) **only** for:
> amendments to the charter itself, changes to the protected surface outside `next/**`,
> renaming Seed, publishing/spin-out, or granting new credentials. Everything else you
> decide.

## 0. Ground rules

- **Where work happens.** All Seed implementation lives under `next/` in this
  repository. Nothing under `next/**` may modify v1 surfaces (`.seed/**`,
  `scripts/seed*`, the root template files) except the explicitly listed integration
  points: the `Makefile` (`check-next` target, Phase 0) and this docs tree. The v1
  template keeps working, untouched, throughout.
- **How work is coordinated.** Dogfood the existing loop: file cards via
  `scripts/seed task` (one card per work item below, `next:` title prefix), claim
  before working, plan-first above L1 (`plans/<task-id>.md` via plan PR), implement in
  a worktree on `seed/<task-id>`, attach evidence, `make check` green, move to review.
  The rules in AGENTS.md apply unchanged.
- **Language and layout.** Go (matches the existing engine and its toolchain), one
  module at `next/`: `cmd/seed/` (CLI), `internal/...` (packages),
  `spec/` (protocol schemas + versioned canonical-form spec), `fixtures/`
  (drill fixtures), `docs/` (implementation notes). Binary name: `seed` — the
  successor claims the name. During incubation it is built into `next/bin/` and invoked
  explicitly (`go run ./cmd/seed`, or `next/bin/seed`), never installed on PATH:
  `scripts/seed` (v1) remains the only coordination entry point for doing the work
  until spin-out.
- **Quality bar.** Table-driven unit tests per package; every charter drill lands as an
  automated test the phase it becomes possible; ≥90% statement coverage for
  `internal/...` from Phase 1 on; `make check` stays green on main at every merge.
  Conformance mapping: each test that satisfies a Part III criterion names it in a
  comment (`// conformance: III.A — ordering is admitted ancestry`).
- **Commit hygiene.** Small scoped commits; no model identifiers in any committed
  artifact; PR bodies state which plan item and which conformance criteria the change
  advances.

## 1. Fixed defaults (binding until a recorded decision changes them)

| Decision | Default | Rationale |
|---|---|---|
| Signature scheme | Ed25519; accept OpenSSH `ed25519` keys so operators reuse existing keys | Ubiquitous, small, fast |
| Hash | SHA-256 everywhere (chain, commitments, receipts) | Boring, universal |
| Canonical event form | JSON per RFC 8785 (JCS); `sig` over the JCS bytes of the event including `prev` | Deterministic without a custom codec |
| Protocol version | `seed/0` in genesis; bump discipline recorded in `spec/` | Versioned from day one |
| Ledger ref | `refs/seed/ledger`; artifact store at `refs/seed/artifacts` (git-addressed) with filesystem fallback | Charter Reference deployment |
| Segments | JSONL, one file per UTC day under `ledger/segments/`, `HEAD` record with tip hash | Simple, streamable |
| Sealed-check encryption | `filippo.io/age` (X25519 recipients = verifier keyring) | Audited, minimal |
| Observation channel | v0: per-executor file under `var/obs/` (gitignored) + optional `refs/seed/obs/<actor>` push; supervisor tails both | Lossy-by-declaration is the contract, so simplest channel first |
| SQLite cache | `modernc.org/sqlite` (no cgo) | Portable builds |
| Admission postures | Cooperative validation library first (same rule set), then enforced self-hosted (`pre-receive` hook binary) in Phase 2; forge-hosted service deferred to Phase 12 | Shared-rule-set requirement drives the order |
| Racing mode | Deferred entirely to post-plan backlog | Opt-in feature, not core |
| Envelope exit codes | Reuse v1 conventions where semantics match (2 contention, 6 fence, 10 version); new codes documented in `spec/envelope.md` | Continuity for tooling |

## 2. Phases

Each phase is one or more cards. **Exit criteria are the phase's acceptance test** —
cite them as evidence when moving cards to review. A phase may start when its
dependencies' exit criteria are merged, not before. Within a phase, items are ordered.

### Phase 0 — Workspace and spec skeleton  *(deps: none)*

1. Scaffold `next/` module, CLI skeleton (`seed version`), CI wiring: `make check-next`
   (build + vet + test + coverage gate) and hook it into `make check`.
2. Write `spec/protocol.md` v0: canonical form (JCS), event fields, hash/signature
   algorithms, verb namespace catalog copied from charter Appendix B, envelope shape,
   exit codes.
3. Create `docs/decisions.md` (implementation decision log — one line per default
   exercised or overridden, linking the PR).

*Exit:* `make check` green with `check-next` included; spec doc exists and matches
charter Appendix B; a decision log exists.

### Phase 1 — Ledger core  *(deps: 0)*

1. Event model + JCS canonicalization + Ed25519 sign/verify (`internal/event`).
2. Chain: append, verify-from-genesis, `prev` linkage; segment storage; `HEAD`.
3. Genesis (`seed init` writes signed genesis naming governance-root keys + `seed/0`).
4. Push-race append loop against a git remote: fetch → re-validate → re-link → push;
   losing events that fail re-validation are reported, never silently re-appended.
5. Halt semantics in the validation rule set (`halt.declared`/`halt.lifted`).
6. Payload data classification lint (references-not-bodies) with a hostile fixture
   corpus.
7. CLI: `seed ledger verify`, `seed ledger append` (dev tool), `seed ledger show`.

*Exit:* charter III.A items — chain verifies from genesis in one command; corrupted
fixtures (reordered, rewritten, forged-sig, bad-prev) all detected; classification
corpus passes; race drill (two concurrent appenders, no lost updates) green.

### Phase 2 — Admission  *(deps: 1)*

1. Validation rule set as a pure library (`internal/admit`): signature vs. keyring,
   capability, fence, transition, schema, classification, protocol version, halt,
   reservation — one rule set, importable by client, hook, and (later) service.
2. Cooperative posture: client self-validates via the library before pushing.
3. Enforced self-hosted posture: `seed-admit` `pre-receive` hook binary refusing
   invalid pushes, force-updates, and deletions of the ledger ref; stateless
   (rebuilds context from the repo it guards).
4. Posture declaration in config; `seed doctor` states the posture and, for
   cooperative, prints the named consequence verbatim.
5. Drills: raw-git adversary (direct push of invalid events) refused under enforced;
   kill-and-replace the hook host, revalidate.

*Exit:* the III.B subset implementable at this point — validator exists and is the
ledger ref's sole writer under enforced posture; statelessness (kill-and-replace
drill); posture declaration with cooperative consequences printed; direct-push refusal
drill; both postures selectable in fixtures — with the rule set covering the checks
that exist so far (signature, schema, classification, protocol version, halt).
Capability rules slot in at Phase 3, fences at Phase 5, budget reservations at
Phase 7, and the full III.B including the compromised-actor drill closes at Phase 12;
the rule-set library is built so those land as added rules, not rework.

### Phase 3 — Identity and grants  *(deps: 2)*

1. `actor.enrolled` / `granted` / `suspended` / `revoked`; keyring projection.
2. Admission checks grants per verb; operator-only verbs; kind documented as
   assertion.
3. Key rotation/revocation drill: revoke → standing ends → history stays attributed.

*Exit:* charter III.E items except qualification tuples (Phase 10) — signature +
grant checks live in admission; revocation drill green.

### Phase 4 — Projections  *(deps: 1; grants integration after 3)*

1. Projection engine: deterministic build from prefix, position stamp, one-command
   rebuild (`seed project rebuild`).
2. Standard projections: contract detail, ready queue (eligibility-filtered after
   Phase 5), actor view, report skeleton.
3. SQLite cache projection; mid-operation deletion drill.
4. Write-boundary lint: nothing outside the projection engine writes projection dirs.

*Exit:* charter III.D core — byte-identical rebuild drill; stamps everywhere; lint
wired into `check-next`.

### Phase 5 — Contracts, claims, packets  *(deps: 3, 4)*

1. Transition table as data (`spec/transitions.json`), self-validating, loaded by
   the admission rule set; lifecycle verbs.
2. Claims with fences; online-only claiming; stale-fence refusal; contention envelope.
3. Four-part packets (acceptance criteria; verified/asserted decisions; commit-anchored
   refs incl. diff-vs-merge-base range; investigation findings) — schema, size bound,
   lint; written on release/park/reap.
4. Acceptance-spec field on contracts; the spec-gate flag (executable content requires
   a gate event before any run) — enforcement lands with verdicts (Phase 6).
5. Plans: falsifiable-plan lint (boundary set, retention set, commands, diff shape);
   plan-gating rule above the trivial tier.
6. Observation streams v0 + monotonic progress counts; expiry vs. wedge detection in
   the report.

*Exit:* charter III.F items except sealed checks — claim race storm drill; packet
lint + a packet-resume fixture drill (fresh actor completes from packet alone,
asserting recorded dead ends are not re-tried); offline-boundary tests (exclusivity
verbs refuse without remote).

### Phase 6 — Verdict pipeline  *(deps: 5)*

1. `submission.made`; verifier workspace (clean per-run checkout); receipt computation
   (plan hash at merge-base, diff hash, inventory, transcripts, environment
   fingerprint); `verdict.rendered` signed by verdict-granted, implementer-disjoint
   key (L1 independence).
2. Reconciliation chain: `merge.requested`, `merge.observed` (observer records forge
   fact), `done`; divergence detection in maintenance; induced-divergence drills.
3. Sealed checks: commitment event (salted SHA-256), age-encrypted body in the
   artifact store, keyring rotation re-encryption, authoring-grant disjointness;
   capability audit test proving no implementer path decrypts.
4. Red-verdict lockout; operator override as its own verb.

*Exit:* charter III.G minus L2/L3 levels and rubric calibration (Phase 10/11) — the
reconciliation drills, receipt recompute-and-mismatch test, sealed-check audit.

### Phase 7 — Supervisor, offers, budgets  *(deps: 5)*

1. Offers (`offer.published`, eligibility-scoped, expiring); workers pull and claim;
   wakeless poll-only test proving wake is advisory.
2. `budget.reserve` / `settle` / `release`; admission decrements reservations;
   reservation race drill (concurrent over-spend impossible); risk-limit declaration
   per adapter.
3. Executor adapter interface (provision / wake / meter / report-tuple); **local
   worktree adapter** first; metering to observation stream, `run.settled` aggregate.
4. Graceful preemption: safe-point interrupt → park with packet; force path → reap
   packet.

*Exit:* charter III.H items for the implemented adapter — poll-only run, reservation
race drill, disposability drill (randomized kill after sync; complete elsewhere).

### Phase 8 — Affordance envelopes  *(deps: 5)*

1. Envelope with `affordances` (computed from the same `internal/admit` rule set),
   `position`, budget block, structured errors, exit codes per spec.
2. Regression test class: affordance-listed verb refused for legality at the same
   position = bug.
3. Refusal-rate metric in the report.

*Exit:* charter III.I — same-rule-set property test; envelope schema stable and
versioned.

### Phase 9 — Lanes, escalation, maintenance  *(deps: 6, 7, 8)*

1. Role definitions (grants + conventions) for the six lanes as fragments; dispatcher
   least-capability posture; injection conformance suite (hostile corpus) against
   dispatcher input handling. The worker lane's role definition includes its loop
   (poll, claim, work, meter, sync, deliberate exit), and exhaustion parking is part
   of that loop: a budget refusal at a spending gate triggers the `claim.parked` exit
   with packet (the III.H row the Phase 7 exit routes here), consuming Phase 8's
   envelope budget block. Four obligations bind every lane's fragment, each a new
   consumer of a table that already exists rather than a new authority. **One
   position-stamped read on wake:** the fragment NAMES the single read it orients
   from (`seed situation --key <its key> [--since <last position>]`) and the
   position it carries forward, and fragment validation checks that the declaration
   is present and names a real surface — promotion criterion 1's "orienting from one
   position-stamped read" made a property of the role file rather than of the agent
   writing it. **The loop acts through the loop verbs** of item 5(c) (`claim
   take|release|park`, `submission make`, `budget reserve|settle|release`), never
   the raw append seam, which consults the admission boundary not at all and so
   cannot answer a refusal with what IS legal. **Liveness rides the work:** the
   observations the expiry/wedge classification reads are emitted by the loop's own
   steps (the metered run, the milestone at a real step, the sync), so a working
   lane is a live lane by construction; the loop's vocabulary contains NO verb whose
   only purpose is to report liveness, and a bare heartbeat is forbidden here rather
   than merely discouraged. **The one-inbox doctrine:** push channels wake,
   position-stamped reads convince — no lane treats a wake, an event or a message as
   a fact about the world, only as a hint to read.
2. Escalation (`blocked(needs-you)`) with packet + question + decision; report
   surfaces age. An escalation raised in answer to a refusal carries that refusal's
   `code` and `message` in its packet, so the question a human is asked is the
   boundary's own account rather than a lane's paraphrase of it.
3. Maintenance loop: reap expired/wedged, reconcile divergence, rebuild projections,
   checkpoint (signed), lints — runnable unattended; audited as an ordinary actor.
   Lints include unsettled-run detection: a claim window carrying an admitted
   `run.started` whose `run.settled` is still missing once the subject has taken a
   subsequent claim window or reached a terminal state is flagged (the Phase 7 exit's
   metering-detection obligation; post-close settlement is a valid intermediate
   state, so the condition is position-anchored, never mid park/reap flow). Item 1's
   loop emitting its observations from its own steps makes an `expired`
   classification BETTER EVIDENCE, because it removes forgotten bookkeeping as a
   cause of silence: a lane that is working is a lane that is observing. It does
   NOT make silence proof, and the reap stays a judgment rather than an inference.
   The channel is ephemeral and lossy by declaration (charter §II.3,
   `spec/observations.md`), so a dropped stream and dead work look identical
   from the outside, and `no_data` — the stream that holds nothing at all —
   carries no reap path whatever. Reaping therefore requires corroboration beyond
   silence, which the maintenance lane's design must supply; what this obligation
   buys is that the silence it corroborates is not an artifact of a worker
   forgetting to speak. No heartbeat predicate is added either: non-advancing
   observations are NOT a heartbeat signature, since a legitimate long-running step
   emits exactly that shape, which the existing expiry/wedge classification already
   distinguishes.
4. Small-team mode and fleet mode end-to-end fixtures. Both run with **no wake
   channel at all** (the one-inbox doctrine asserted rather than asserted about),
   and every refusal a lane meets in them converges within one retry, in one of
   three ways: an admitting act on its next attempt, a refreshed position-stamped
   read showing the act is no longer owed, or an escalation carrying that
   refusal's `code` and `message`. The middle arm is not a loophole but the
   common case: in fleet mode two workers racing `claim take` means the loser
   legitimately re-orients and takes different work, and a fence invalidated by a
   concurrent reap says the same thing — requiring admission or escalation there
   would either reject correct lane behavior or manufacture an escalation storm
   out of ordinary contention. What is forbidden is the fourth outcome: a blind
   retry, a silent loop, or a bare "failed". A refusal a correct lane can do
   nothing with is a bug here, exactly as a listed-but-refused verb is one in
   Phase 8.
5. **The lane-facing surface.** Lanes are defined here but nothing defines the
   surface they act through: today the CLI is the raw protocol seam (`ledger append
   --verb claim.taken --payload …`, fences read out of a projection by hand,
   four-part packets hand-built). Three parts, one deliverable. (a) An **obligations
   projection**: per subject and actor, what is owed, since which position, under
   which clock, and which verbs discharge it — a projection like every other
   (deterministic, byte-identical, position-stamped, rebuildable,
   non-authoritative), deriving `discharged_by` from the transition tables so it
   invents no legality; an obligation whose discharging verb is refused at the same
   position is the III.I row-2 bug class one level up, and carries the same
   regression-class treatment. (b) A **situation read**: what is true for me now —
   standing obligations with clocks, active windows and fences, unread messages,
   budget headroom — with `--since <position>` returning only what changed, so a
   resuming lane pays for the delta rather than reconstructing the world. Part (b) is
   **partially met**: the obligations, windows and budget block landed, and the
   unread messages did not, so the read a lane is told to orient from cannot yet
   tell it that it has mail. (b) is complete only when `seed situation` carries the
   messages addressed to the caller, with "unread" derived from the cited `--since`
   position rather than from stored read-state — the position a lane carries forward
   IS its read cursor, so no `message.read` verb is introduced and the surface gains
   a section rather than a concept. (c) **Loop
   verbs** that derive every argument the system can derive (the fence from the
   active window, the reservation id from the budget view, the base range from the
   repository) and refuse before signing what the tables would refuse after, naming
   the act that would succeed. This is Phase 8's principle one level up — one rule
   set, enforcement and advertisement — applied to situation and argument
   construction, and it is what promotion's loop-completeness criterion (§5) needs:
   a lane that cannot orient or choose cannot run unattended, whatever the
   conformance report says.

*Exit:* charter III.J — both modes run the full loop in CI; injection corpus green;
maintenance runs unattended in the fixture. This phase carries promotion's
lanes-operable and loop-completeness gates (§5).

### Phase 10 — Qualification and evaluation  *(deps: 9)*

1. Runtime tuples in enrollment/grants; adapters report provisioned tuple; drift =
   out-of-grant; the offer's `tuples` scope as the scheduling input the supervisor
   writes (III.J row 3's "strongest tuples by policy", routed here by the Phase 9 exit
   record; the ranking policy that fills it from eval results is Phase 13 item 7's,
   the Phase 10 exit record's revision of this clause).
2. Eval contracts against fixture repos through the production machinery; grants cite
   passing tuples; scheduled spot-checks; suspension on failure.
3. Independence levels L2/L3 declared per tier and recorded in verdicts.
4. Rubric verdicts (per-item, evidence-cited, uncertainty-marked); calibration harness
   against a gold set with authority suspension on drift.
5. Trajectory-prefix regression harness for lane decision points; dispatcher re-triage
   rate and planner unedited-approval rate tracked (III.J row 3, routed here by the
   Phase 9 exit record: both are lane-quality metrics that are meaningless without this
   harness).

*Exit:* charter III.E (tuples) + III.G (levels, calibration) + III.O eval items + III.J
row 3's metrics half (its policy clause is Phase 13 item 7's, by the Phase 10 exit
record's revision).

### Phase 11 — Curator and flywheel  *(deps: 9)*

1. Staged pipeline stores (observations → hypotheses → validated → policy) with
   grant-gated boundaries; workers append candidates only.
2. Promotion gate: ≥2-trajectory support, applies-when, provenance, last-validated;
   adversarial evaluation for behavior-changing lessons; contested state. The
   applies-when carries its delivery moment: a promoted lesson whose applies-when
   matches the claimed subject surfaces in the claim packet and in the response
   envelope at claim time (the charter III.I row the Phase 8 exit routes here —
   knowledge nobody is shown at the right moment is knowledge that does not exist).
3. Poisoning drill: constructed trajectories fail to achieve promotion.
4. Expiry, retirement (evidence kept), rollback-by-revert. III.K row 9 (knowledge
   bloat managed: dedup with provenance, staleness flags, structure lint) lands here
   too, staleness being expiry's other face (plans/os-0d537fbd.md).
5. Flywheel v0: recurring-shape detection from the ledger → drafted workflow → mock
   validation → proposal PR.

*Exit:* charter III.K — poisoning drill green; a real recurring chore in the fixture
converts to a workflow through the gates.

### Phase 12 — Hardening, distribution, migration  *(deps: all)*

1. **Compromised-actor drill**: red-team harness with valid key + credential + raw git
   against an enforced fixture; asserts the §I.2 ceiling item by item. This is the
   release gate — it runs in CI from here on. The consequence's second half is owed
   beside the ceiling: revoking the compromised key reaps its open claims **on the
   revocation alone** — the record proves the holder can never exit its window, the one
   case where the ledger rather than the observation channel corroborates a reap — with
   packets, so the work is re-offered (III.E row 8's reap arm, routed here by the Phase
   10 exit record). A reap corroborated by the revocation record is a lifecycle change
   with its own boundary rule and drills, so it lands as item 1's follow-up card
   os-32d06c65 rather than inside the drill, which asserts today's consequence (a
   revoked key's proposals refuse at the push).
2. Forge-hosted admission service (stateless, sole-writer pattern) + protections
   desired-state reconciler.
3. Checkpoint trust docs + replay-equals-genesis CI proof; performance budgets
   (admission latency, replay, rebuild) tracked.
4. Preseed file (config, guardrails, teams, protections, posture) — idempotent init.
   The guardrails it declares include the agent-only ones, read off the roster's
   `kind`, and the report's lane rates split by kind (III.E row 9, routed here by the
   Phase 10 exit record); the config it declares enumerates the protected surface and
   names the governance root and its change process (III.G row 9 and III.L row 2,
   routed likewise).
5. **Migration**: `seed import --from-open-seed <export>` — v1 lossless export → verify
   anchors → transform (cards→contracts, run-log→events, receipts→verdict records,
   mail→messages) → genesis import refusing non-empty ledgers; drilled against a real
   v1 fixture.
6. Docs generation: lifecycle prose from `transitions.json`; operator handbook;
   simulation mode (credential-free end-to-end; III.O row 5's second half, the seam
   where a decider plugs into the trajectory harness — routed here by the Phase 10 exit
   record).

This phase carries promotion's migration gate (item 5) and, in item 1, the drill
that must be green before the self-hosting cutover (§5).

*Exit:* charter III.B (service posture), III.O (the compromised-actor drill in CI;
simulation mode closing row 5), III.P complete save row 1's tagged-release clause (the
first `seed/v*` release is §5 step 2's; the Phase 12 exit record walks the row partial,
os-53650015), and the rows the Phase 10 exit record
routes here (III.E rows 8 and 9, III.G row 9); the pillars no earlier exit line owns —
III.C, III.L, III.M and III.Q — are walked at this exit, each row met by citation or
routed to Phase 13; the fixture organization runs a week-long simulated backlog
(accelerated clock) meeting III.R's zero-violation bar.

### Phase 13 — Conformance completion  *(deps: 12)*

The items Part III requires that Phases 0–12 deliberately defer. **Full Part III
conformance is claimable only after this phase**; Phases 0–12 deliver core conformance
(every pillar's mechanisms at the enforced self-hosted posture with the local-worktree
adapter), and the doctor reports which Phase 13 criteria remain open until then.

1. Racing mode as the per-squad opt-in with first-verified settlement (III.F).
2. Remaining executor adapters: container, cloud agent session, enrolled remote
   worker (III.H).
3. Non-primary forge adapter for the forge extras (III.N).
4. Federation read remotes and cross-repo request ingress (III.N), and the `request.*`
   rows that mirror edits and dashboard actions enter by, closing the injection corpus's
   mirror arm (III.J row 2, routed here by the Phase 9 exit record: the corpus already
   sweeps the projections a mirror renders from, so what the arm waits on is the verb).
5. A2A-shaped cross-organization boundary (III.N).
6. Machine-protocol surface exposing the CLI's verbs with identical semantics, and
   platform parity (including Windows) documented and tested (III.I — the row the
   Phase 8 exit routes here; the CLI stays the complete interface, so this adds a
   second surface over the same rule set, never a second semantics).
7. Tuple ranking as supervisor policy: eval results (mints, spot-checks, calibration
   agreement) rank qualified tuples, and the planner lane's offers carry the strongest
   into the `tuples` scope Phase 10 item 1 landed as the scheduling input (III.J row 3's
   policy clause, routed here by the Phase 10 exit record: Phase 10 item 2 deferred
   ranking by name and no later item re-homed it).
8. Projection integration boundaries: mirror exporter conformance, no bidirectional
   component, and governed external-fact observations (III.D rows 5–7). A separate
   projection-only mirror component with a sealed exporter registry and one conformance
   suite every registered exporter runs, joined to item 4's request ingress; an
   authority lint deriving from each executable's import closure that no component
   holds both an export path and a coordination write path; and a closed external-fact
   catalog pinned to its spec table, with the forge observation sources read-only and
   drilled never to act as control.

*Exit:* the named III.F/III.H/III.I/III.N criteria green, III.J rows 2 and 3 beside
III.I, and III.D rows 5–7 by item 8; the conformance report shows Part III complete at
the enforced self-hosted posture.

## 3. Backlog (true extras, not conformance-blocking)

Dashboard tiers beyond the report (no Part III criterion requires them) and sharded
admission intake (III.B says MAY). File cards when Phase 13 is exhausted.
The contention benchmark at target scale (III.C row 4): the per-PR storm stays at
24 writers under the perf gate; a scheduled run with hundreds of writers, tracked
against the same budgets, is a card for when Phase 13 is exhausted (the Phase 12
exit record routes the row here).

**Borrowed from surveyed tools (2026-09-05).** Two later surveys turned up three
Seed ideas worth a card each. None changes a Part III row; each is filed against
the charter section it extends and stays adapter- or record-shaped so the
deliberately-absent table (§II.18) holds. Sources: [haksolot/ank](https://github.com/haksolot/ank)
(binding decisions embedded in the repo, criteria frozen by hash at claim) and
[herdrdev/herdr](https://github.com/herdrdev/herdr) (a terminal multiplexer that
knows agent state and exposes a CLI plus socket for agents to drive each other).

- **Ratified decision records, path-scoped, surfaced at claim** (ank's ADR-with-
  constraint). Seed marks settled decisions in packets as verified or asserted
  (§II.6) and surfaces promoted *lessons* by applies-when (§II.10, §II.12), but a
  human-ratified decision with a binding constraint and a path scope has no
  first-class record of its own. The card: a `decision.ratified` record whose
  applies-when reuses the curation predicate (`internal/curation` already parses
  `{routing?, tier?, paths?}`), surfaced in the envelope and packet at claim time
  beside matching lessons, and marked *verified* in packets by construction since
  it is ledger-anchored. Distinct from a lesson: no support threshold, no expiry,
  ratification is a gate act not a curator act.
- **Ratification anchor and a drift row in `doctor`** (ank's anchor). Record at
  ratification the SHA-256 of the normalized constraint text plus its scope, and
  give `seed doctor` a row that flags a decision whose current carrier text no
  longer matches its anchor. Tamper evidence for the ledger already exists (§II.1);
  this extends it to the prose a decision points at, which lives outside the ledger
  and can drift silently. Cheap, and depends on the previous card.
- **A herdr wake adapter** (§II.9 executor adapters: provision, wake, meter).
  Advisory wake over `herdr agent prompt <name>`; herdr's pane state (`herdr agent
  wait <name> --until blocked`) metered onto the observation stream as *liveness
  only*, a wedge signal for the supervisor's preemption path (§II.9), never an
  escalation. Escalation is a ledger event (§II.7: `blocked(needs-you)` carries the
  packet, the question, and the minimal decision), so the adapter surfaces
  *admitted* escalations to the operator's pane over the same wake channel and
  mints none from what a terminal shows. Adapter-only by charter rule: the §II.18
  row forbids any coordination feature assuming a multiplexer, so a worker with no
  herdr loses latency and nothing else. Mock-total like every adapter (§II.13).
A fourth idea from the same survey, a herdr lifecycle shim under
`.seed/hooks/shims/herdr/`, is **not this plan's to schedule**: the ground rules
(§0) limit Seed's v1 integration points to the Makefile and this docs tree, and
`docs/design-options.md` D6 assigns the remaining lifecycle shims to the v1/v2
stream, whose roadmap is cards on the state ref (`docs/build-plan.md`, Phase 7).
It is filed there as card os-a7b5dffb under that authority and protected-path
review; it is listed here only so the survey's trail is complete.

**Borrowed from practice (2026-09-06).** One idea from a project outside this
tree: test harnesses that attach a trace to every test run (a trace id and a span
tree carrying structured outcome attributes such as `outcome=failure`,
`reason=malformed_json`) proved reusable across projects once the emitting
primitives were nailed down, because the contract is a file the harness writes,
never a vendor SDK. Filed against the charter section it extends; record-shaped;
no Part III row changes.

- **Trace-shaped test evidence in the receipt** (§II.8 receipts; III.G "distinct
  evidence-acquisition paths"). Today a receipt transcript binds a command, its
  exit, and the digest of its combined output (`spec/verdicts.md`). The card
  adds an optional `traces` entry per transcript: the trace id, the artifact-store
  digest of the spans the harness exported to a declared path, and the digest of
  the trace's **normalized shape** (the span tree by name, kind, status, and
  declared attributes, with ids, timestamps, and durations stripped). The shape
  digest is what `verdict check` recomputes and what L3's "reproduces" predicate
  compares, since raw spans never reproduce byte-for-byte; the raw export is
  bulk content and lives in the artifact store by hash per §II.1's data
  classification, with the erasure path. Three rules the charter already states
  fix the rest: the verifier's own run produces the trace (III.G, "no
  implementer-claims channel"), so an implementer-attached trace is a claim and
  never a receipt input; rubric verdicts cite evidence per item, so a span
  becomes citable as `trace:<id>/span:<id>`; and the III.G evidence query joins
  verdict outcome to span status and outcome attributes. Not an observability
  subsystem: §II.18 forbids a second run log, so any exporter or collector is an
  adapter detail on the observation channel, and a harness that emits no trace
  loses only the richer evidence. Source: the HerdrTestServer harness in
  herdr-sdk-development and its runSdkLab trace (2026-09-05).

Phase 13 is exhausted on the frontier (`docs/progress.md`), so these are
filable now; none blocks promotion (§5) and none may be scheduled ahead of an
open Phase 13 row.

**Borrowed from a formal-methods survey (2026-09-06).** A survey asked whether Lean 4
belongs in Seed and answered no: a proof would be about a model of `internal/admit`,
with no extraction to the Go rule set that enforces, so the model and the code would
be two implementations and the bugs that matter live in the gap between them. Two
cheaper instruments cover the same ground from inside the one toolchain the ground
rules (§0) name. They test different layers and overlap only at racing mode, so both
are filed; the second depends on the first for its generator and predicates. Neither
changes a Part III row; the same filable-now rule applies.

- **A random-walk property test over admission** (os-21bf939f; charter III.I and the
  §I.2 ceiling as it lands in admission). The III.I sweep checks listed-implies-admits
  at every prefix of one hand-written walk, and every generator in the tree walks a
  scripted shape, so rule interactions at positions no script reaches are tested only
  where someone thought of the case. The card: a seeded walker that reads a random
  lane's affordances at each position and drafts a listed verb or, some fraction of
  the time, a verb `transitions.json` forbids there; four oracles (listed admits,
  forbidden refuses with the transition rule's reason, the fold follows the table's
  `to`, and the finished chain passes the five-bar audit and the red-team clause
  table read as predicates). Shrinking is prefix truncation, so no dependency is
  added; the iteration count splits between `make check-next` and the scheduled run
  the way the perf storm does.
- **An exhaustive interleaving check of the append loop and halt** (os-07e6e76c;
  charter §II.1 freshness and halt, III.A row 7). The race drill and the perf storm
  are executions, not enumerations. The card: an abstract state machine of fetch,
  attempt, push and halt with an explorer that walks every interleaving for small N,
  asserting no lost update, `prev` equals the tip at admission, the persisted head
  never regresses, halt is refused at the boundary, and bounded termination. The
  toolchain default is Go-native, under `make check-next` at small N; TLA+ is the
  fallback if the model outgrows it, kept under `spec/` with a `last-verified`
  stamp and off the fast gate. The verdict-then-merge reconciliation and racing mode
  are the named follow-on once the explorer's shape is known.

**Borrowed from surveyed tools (2026-09-06, pigeon).** One idea from
[pigeonlabsHQ/pigeon](https://github.com/pigeonlabsHQ/pigeon), a v0.1 delegated-authority
credential (the "Pigeon Pass"): an Ed25519-signed chain of grants in which every child is
provably narrower than its parent (capability subset, trailing-wildcard resource
narrowing, a closed constraint set whose `rate` and `count` are metered against every
ancestor, expiry no later than the parent's), and a verifier that fails closed on
anything it cannot prove. Seed already holds the ledger half of this: grants are admitted
events checked per verb (§II.5), verdict and sealer keys are provably disjoint from
implementing keys, and a stolen key's ceiling is signed garbage attributed to itself
(§I.2). What Seed lacks is the same discipline at the **tool edge**: the tuple's
`tool_policy` is an opaque string (`spec/qualification.md`), and "least standing
capability" (§II.10) is a lane's grant set, not a bound on the side effects its runtime
performs on behalf of one contract. One card, and not a dependency: the reference is
Python, one commit old, with in-memory replay and usage stores; its canonical form is
its own rather than JCS; and its verifier proves that each link's issuer signed but never
that the leaf subject holds its key, so a pass is a bearer credential in practice and the
harness must bind it.

- **Tool-edge authority passes over a declared policy map** (§II.5 capability scoping,
  §II.9 executor adapters, §II.10 least standing capability). At wake, the executor
  adapter mints a short-lived pass for the run and a tool proxy it runs outside the agent
  process (the container and cloud adapters; the local adapter declares it cannot, as it
  does for the tuple) verifies before each side effect and refuses on denial; a harness
  that spawns subagents hands each an attenuated child, so a spawned worker holds a subset
  of its parent's edge authority by construction. **The card's first item is the mapping,
  because the pass's inputs do not exist yet.** `tool_policy` is an opaque tuple string;
  a contract's `routing` is one squad name (`internal/transition`); branch names are forge
  state the ledger cannot see (`spec/verdicts.md`); and Seed holds no lease at all —
  a claim stands until a deliberate exit or a reap (`spec/observations.md`). So the
  capability set, the resource set and the TTL come from a **declared tool-policy map**
  keyed by the tuple's `tool_policy` value, beside the lane manifest's grants, read the
  way the claim ceiling and the routing rule already read the declaration: **admission
  policy, not chain validity** (`spec/postures.md`), by the cooperative client from
  its working tree and by the hook at the default branch's tip. The subject is the run's
  key and the contract, squad and tuple it was minted for; the adapter re-mints once per
  metering/poll cycle (`spec/executors.md`), so the pass dies with the run's silence
  rather than pretending to a deadline the ledger does not record. Three rules keep the
  ledger the one authority (§I.3, §II.18): the pass is a projection of the declaration
  plus the chain, never presented at admission, so `actor.granted` stays the only
  capability data admission reads; the pass's operation cap is a **declared number, not
  the budget reservation** — reservation units are abstract until Phase 7.3 metering gives
  them adapter meaning (`spec/budgets.md`), so converting one to the other would deny
  a run with budget left or spend past it, and the two stay separate until units mean
  something; and denials are metered onto the observation stream as `tool.refused` (§II.9
  telemetry, never an escalation) and surface in the packet and the supervisor's report,
  **never in a receipt** — a verifier's inputs are exclusively self-executed or self-read
  and reproducible from the submission head (`spec/verdicts.md`), and a proxy refusal
  during implementation is an implementer claim on a channel declared lossy. Format:
  pigeon's SPEC §4 to §9 in Go under `internal/pass`, with its `fixtures/` as the
  conformance corpus, so a pass minted here verifies under its MCP middleware unchanged;
  the credential never enters the ledger, so the JCS default (§1) governs nothing it
  touches, recorded as one decision-log line. Mock-total like every adapter (§II.13): a
  harness with no proxy loses the bound and nothing else, which is the honest v0 and the
  reason this is a backlog extra.

Two uses the survey considered and does not file. A pass as the cross-organization
ingress credential: `spec/requests.md` enrolls the ingress key as a service holding
`dispatch`, and the `boundary` rule already refuses request kinds the card does not name,
so a second gate on the same question is the second authority §II.18 forbids. Pass-shaped
grants at admission: `actor.granted` carries no expiry and a grant's standing ends only
with its key's, which the survey notes as a real gap (`spec/actors.md` defers
grant-level withdrawal "until the catalog grows a verb for it"), but a lease on grants is
a chain-validity rule and so a protocol bump under `spec/protocol.md`'s discipline: a
card for that decision, not for this format.

## 4. Progress tracking

Maintain `docs/progress.md`: one line per plan item — `phase.item — card id —
PR — state`. Update it in every task PR touching `next/**`. It is the single place a
fresh agent reads to find the frontier; keep it truthful before starting new work.

## 5. Promotion (spin-out)

The ground rules say `scripts/seed` (v1) "remains the only coordination entry point
for doing the work **until spin-out**", and the autonomy contract lists
publishing/spin-out among the few escalations. This section says what spin-out is,
what must be true first, and who decides.

**Two steps.** *Self-hosting*: this repository's own development coordinates on
Seed, with v1 retained read-only for its history. *Distribution*: Seed becomes what
new users clone. *The retirement of v1 itself is the predecessor repository's concern and is planned there (shaunlmason/open-seed, `docs/v1-retirement.md`); this repository is the successor standing on its own.* **Neither cutover is
autonomously decidable.** Spin-out *is* the
entry-point switch, so the self-hosting cutover is itself the reserved escalation —
renaming the later publish does not authorize the earlier authority switch. Agents
drive the work up to each gate, present the evidence, and stop.

**Criteria.** Promotion to self-hosting is met when, at the enforced self-hosted
posture:
*Amended 2026-09-08 by
[`decisions/0005-cooperative-posture.md`](../decisions/0005-cooperative-posture.md):
the self-hosting cutover is made at the `cooperative` posture instead. The seven
criteria below are unchanged and are read at that posture. The decision names what
the substitution trades away (the enforced-only guarantees of III.B rows 1, 4 and 5,
and with them criterion 7's live protection as opposed to its implementation claim),
carries its risk statement, and records that no conformance row is edited. This is a
recorded deviation, larger than 0004's, and not a reading of the original words.*

1. **Loop-completeness.** A lane runs poll → claim → plan-gate → work → meter →
   submit → verdict → merge-observe → deliberate exit, plus escalation and messages,
   entirely through Seed verbs, orienting from one position-stamped read rather than
   hand-assembling ledger payloads and hand-computing fences (Phase 9 item 5).
2. **Lanes operable.** Phase 9 complete: role fragments, dispatcher
   least-capability, injection corpus green, worker loop with exhaustion parking,
   escalation with packet/question/decision, maintenance runnable unattended.
3. **Migration proven.** `seed import --from-open-seed` (Phase 12 item 5) drilled
   against a real export of *this* repository's v1 state, not only a fixture.
4. **Shadow run.** Seed coordinates a declared slice of this repository's own cards
   beside v1 for a stated window, with any divergence reconciled and recorded.
   *Amended 2026-09-06 by
   [`decisions/0004-shadow-run-substitution.md`](../decisions/0004-shadow-run-substitution.md):
   for the self-hosting cutover the operator accepts the credential-free accelerated
   simulation (`seed simulate --days 7 --intents 24 --posture enforced-self-hosted`,
   five-bar audit clean) in this criterion's place. The decision names what the
   substitution trades away, carries its risk statement, and moves the five-bar audit
   over the real chain to after the cutover. The live protocol stays written
   down in the packet; this is a recorded deviation, not a reading of the original
   words. Further amended 2026-09-08 by
   [`decisions/0007-no-day-7-wait.md`](../decisions/0007-no-day-7-wait.md): that
   audit was to run at day 7, and now runs as the flip's last step. The check is
   kept, the wait is dropped, and 0007 names what the earlier reading gives up.*
5. **Cutover and rollback written down.** Which entry point flips when, what stays
   authoritative where during the window, and the documented path back.
6. **Core conformance.** Phases 0 through 12 complete, so every pillar's mechanisms
   stand, with `doctor` reporting exactly which Phase 13 rows remain open.
7. **The compromised-actor drill green in CI** (Phase 12 item 1) **before the
   cutover, not after.** The cutover is when real authority moves to Seed, so the
   drill demonstrating the §I.2 ceiling against a valid stolen key must precede it;
   nothing is at risk while Seed coordinates nothing.

**What is not required.** Phase 13 alone follows promotion — the plan already says
full Part III conformance is claimable only after that phase, with `doctor`
reporting the open rows meanwhile. Phases 10 and 11 **are** required: "Phases 0–12
deliver core conformance" describes what those phases deliver collectively, Phase
10's exit owns III.E and III.G and the III.O eval items, Phase 11's owns III.K,
criterion 6 demands every pillar's mechanisms, and Phase 12 declares `deps: all`.
Promoting without them would hand real coordination to a system whose verdicts
carry no calibration and whose grants carry no runtime qualification. If an earlier
*supervised* step is ever wanted — Seed coordinating real work while humans remain
at every merge gate — it is a distinct milestone that must name what it trades away
(III.E tuples, III.G calibration, III.K curation), carry its own risk statement, and
be accepted by a human as the deviation it is, never presented as consistent with
this phasing.

**Critical path.** Phase 9 (including item 5) → Phases 10 and 11, which both declare
`deps: 9` and therefore run in parallel with each other → Phase 12 in full →
criterion 4's evidence (the shadow run, or the accelerated simulation decision 0004
accepts in its place) → the escalated self-hosting cutover → the escalated
distribution step. Phase 13 follows. Promotion is gated by four more phases, not one.

This section is the plan's, not the charter's: it schedules and defines a milestone
and is never itself a Part III criterion.
