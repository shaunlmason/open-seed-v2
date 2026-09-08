# progress.md — the Seed implementation frontier

> **Incubation record, superseded.** This frontier tracked Seed's implementation while it lived under `next/` in the predecessor, and it closed at the extraction. It describes the
> self-hosting cutover inside the predecessor repository, in which Seed
> would have taken over open-seed's own coordination. That cutover did not
> happen: Seed was extracted into this repository instead, which needs no
> governance root key because no authority moves inside the predecessor at
> all. See [`../decisions/0009-v2-extraction-supersedes-the-cutover.md`](../decisions/0009-v2-extraction-supersedes-the-cutover.md).
> Kept because the evidence in it is real and still bears on anyone
> importing v1 state, and because a route not taken is worth being able to
> read.

The single resume point (docs/build-plan.md §4): one line per plan item,
`phase.item — card id — PR — state`. A fresh agent resumes from this file
alone: read it top to bottom, verify the PR states it claims against the
forge, then take the frontier line's next action. Keep it truthful in every
PR that touches `next/**`; never start new work while it misstates the
frontier.

## Roster and conventions

- Implementer actor: `seed-next-implementer` (work verbs). Operator queue
  verbs for `next:` cards run under the session principal per
  `decisions/0003-next-loop-delegation.md`.
- Cards carry the `next:` title prefix; branches `seed/<card>` (task) and
  `seed/<card>-plan` (plan); receipts generated with `--run --write` before
  review.
- Phase gate: a phase starts when its dependencies' exit criteria are
  **merged** (docs/build-plan.md §2).

## Ledger

- 0.1 module scaffold + CI wiring (`make check-next`) — os-116ca9ac — plan
  PR #71, task PR #72 — **done** (merged; card closed)
- 0.2 spec skeleton (`spec/protocol.md`, `spec/envelope.md`) —
  os-116ca9ac — task PR #72 — **done** (merged; encodings and upgrade-safe
  version semantics per #71/#75 review)
- 0.3 decision log (`docs/decisions.md`; plus this frontier file) —
  os-116ca9ac — task PR #72 — **done** (merged)
- 1.1 event model + JCS + Ed25519 — os-aa146827 — plan PR #73, task PR
  #76 — **done** (merged; strict wire parsing + lowercase-only hex per
  review; card closed)
- 1.2 chain/segments/HEAD — os-ead12024 — plan PR #74 (merged, amended),
  task PR #79 — **done** (merged; card closed)
- 1.3 genesis via `seed init` — os-d636299d — plan PR #75 (merged,
  amended), task PR #83 — **done** (merged; genesis-payload version
  bootstrap + position-stamped init refusal per review; card closed)
- 1.4 push-race append loop — os-62e2aa1d — plan PR #81 (merged,
  amended: monotonic head + rollback drill), task PR #86 — **done**
  (merged; vanished-ref regression refusal + typed remote-rejection per
  review; card closed)
- 1.5 halt semantics in the rule set — os-bce3fb98 — plan PR #78 (merged,
  amended: exit code 7, reason-carrying state), task PR #84 — **done**
  (merged; halted refusal ordering + empty lift payload per review; card
  closed)
- 1.6 payload classification lint + hostile corpus — os-d6f81ec6 — plan
  PR #77 (merged, amended: aggregate free-text budget, embedded rules),
  task PR #80 — **done** (merged; narrowed anchor exemption + RFC 6901
  pointers per review; card closed)
- 1.7 CLI `seed ledger verify/append/show` — os-89412090 — plan PR #82
  (merged, amended: envelope v0 preserved, exit 9), task PR #85 —
  **done** (merged; position-stamped verify refusals, read-only show,
  active-version append per review; card closed)

**Phase 1 exit (charter III.A): met.** The chain verifies from genesis in
one command (`seed ledger verify`, #85); corrupted fixtures (reordered,
rewritten, forged-sig, bad-prev, lying HEAD, lying genesis) are detected
with positioned reasons (#79/#83/#85); the hostile classification corpus
passes (#80); the race drill (two concurrent appenders, no lost updates,
a real retry observed) is green on main (#86). This exit record is card
os-beac85e1's task PR (an administrative card, not a Phase 2 item).

## Phase 2 — Admission (docs/build-plan.md Phase 2; deps: 1 ✓)

- 2.1 admission rule set library (`internal/admit`) — os-3898f232 —
  plan PR #88 (merged, amended: ledger `WithObserver` + shared upgrade
  schema in scope), task PR #90 — **done** (merged; card closed)
- 2.2 cooperative posture (client self-validation) — os-895bf828 —
  plan PR #91 (merged, amended: gitref verify-option pass-through +
  update-phase race marker), task PR #93 — **done** (merged; state-dir
  lock + position-stamped refusals per review; card closed)
- 2.3 enforced posture, `seed-admit` pre-receive hook — os-d3591e09 —
  plan PR #92 (merged), task PR #94 — **done** (merged; tree-shape gate
  + exclusion-based rule selection per review; card closed)
- 2.4 posture declaration + `seed doctor` — os-3c72f93f — plan PR #95
  (merged, amended: exit 13 `posture_invalid` allocation per review),
  task PR #98 — **done** (merged; exit 66 `unreadable` for read
  failures + valid-postures wording per review; card closed)
- 2.5 admission drills (raw-git adversary; kill-and-replace) —
  os-028dda91 — plan PR #96 (merged, amended: fresh-clone
  kill-and-replace per review), task PR #99 — **done** (merged;
  shared adversary table under both postures + original host deleted
  with an independently installed hook copy, per review; card closed)

## Phase 3 — Identity and grants (docs/build-plan.md Phase 3; deps: 2)

- 3.1 actor events + keyring projection — os-52a2d688 — plan PR #97
  (merged, amended: root-liveness guard + seed/1 activation boundary
  per review), task PR #100 — **done** (merged; standing/grant rule
  split per review; card closed)
- 3.2 admission checks grants per verb — os-3979d48b — plan PR #101
  (merged, amended: versioning stance held, checkpoint accepts
  maintenance|operator, per review), task PR #102 — **done** (merged;
  vocabulary test parses the normative spec table per review; card
  closed)
- 3.3 key rotation/revocation drill — os-d1f35a8c — plan PR #103
  (merged, amended: exit record scoped to the III.E subset with the
  full unmet-remainder enumeration, per review), task PR #104 —
  **done** (merged; card closed)

**Phase 3 exit (the III.E subset docs/build-plan.md scopes): met.**
Signature and grant checks live in admission (#100: standing-aware
resolution behind the seed/1 boundary, root liveness, grandfathering;
#102: the capability vocabulary checked on every verb, operator-only
refusals structural, delegation via actor.granted proven end-to-end,
kind a drilled assertion), and the revocation drill is green (#104:
rotation with history attributed and every post-revocation proposal
refused at the rule set, the seed-admit boundary, and the CLI; the
compromised-key cut per posture with the cooperative consequence
observable; terminality, grants-die-with-standing, and root liveness
held at the boundary). Still-unmet III.E criteria, by landing phase:
implementer-disjoint self-approval and sealed-check keyring rotation on
verifier-key revocation (Phase 6); claim reaping on revocation (Phase
5); qualification tuples, grants citing them, and scheduled spot-check
suspension (Phase 10); the roster distinction consumed by agent-only
guardrails and human/agent metrics (with those surfaces, Phases 8 and
11).

**Phase 2 exit (charter III.B subset): met.** The validator is the
guarded ref's sole writer under enforced posture (#94: invalid-stream,
rewrite, truncation, force-update, and deletion refusals, tree shape
included); statelessness is drilled by kill-and-replace (#99: a fresh
host rebuilt from a bare clone makes identical decisions and the chain
verifies from genesis); the posture declaration prints the cooperative
consequence verbatim (#98); the raw-git adversary drill runs per posture
with the cooperative consequence made observable (#99); both postures
are selectable in fixtures through the shared declaration (#99).
Capability rules slot in at Phase 3, fences at Phase 5, reservations at
Phase 7, as added rules on the shared set.

## Phase 4 — Projections (docs/build-plan.md Phase 4; deps: 1 ✓)

- 4.1 projection engine (deterministic build, stamps, one-command
  rebuild) — os-4d5cacff — plan PR #105 (merged, amended: immutable
  builds + `CURRENT` pointer publication, roster includes genesis
  roots, overlap refusal, full-tree immutability and stale-`HEAD`
  fixtures, interrupted-publication drill, per review; second round:
  version-bearing build ids so derivation changes republish, and the
  superseded build survives one swap for in-flight readers), task PR
  #109 — **done** (merged; card closed)
- 4.2 standard projections (contract detail, ready queue, actor view,
  report skeleton, `seed project current`, exit 15 `stale`) —
  os-fecfb3f7 — plan PR #106 (merged, amended: the ready queue ships
  registered with `derivation: "none"` v0, per review), task PR
  #111 (stack-collapsed; its diff never reached main), re-landed as
  task PR #117 (merged, amended per review: registry-validated
  consumer verb, absence-4/damage-5 split refined so a layout
  missing its CURRENT pointer is damage, incomplete-stamp refusal,
  stale envelope position, unprivileged-cleanup test fix) —
  **done** (merged; card closed)
- 4.3 SQLite cache projection + mid-operation deletion drill —
  os-acc1ac78 — plan PR #108 (merged) + amendment PR #110 (merged:
  the cache is a registered projection, byte-identical like every
  view; the stamp table carries exactly the tree stamp's fields),
  task PR #119 — **done** (merged; card closed; Phase 4 complete)
- 4.4 write-boundary lint wired into check-next — os-8d5e9c45 — plan
  PR #107 (merged, amended: seam/write-separation lint + locked trees
  `0444`/`0555` with the engine unlock window, deletion via rebuild,
  per review), task PR #112 (stack-collapsed; its diff never reached
  main), re-landed as task PR #118 (merged, amended per review:
  openDirs partial-open rollback, lint vocabulary derived from the
  engine's declarations with a behavioral layout probe,
  unprivileged-cleanup lesson recorded) — **done** (merged; card
  closed)

## Phase 5 — Lifecycle, claims, packets (docs/build-plan.md Phase 5; deps: 3 ✓, 4 ✓)

- 5.1 transition table as data + lifecycle verbs — os-d69a6c91 —
  plan PR #113 (merged, amended: charter Appendix catalog vocabulary,
  merge.observed-only done, capability rows, completeness presence at
  the claimability transition, per review) — **done** (merged #122;
  card closed; the fold's seed/1 activation boundary rides follow-up
  #129, per the post-merge review round):
  transitions.json + self-validation, the lifecycle admission rule
  across rule set/hook/CLI, dispatch/claim capability lanes,
  contracts v2 (state + anomalies), queue v2 ("transitions/1"),
  cache generation 2, spec/lifecycle.md
- 5.2 claims with fences — os-5dc16a7c — plan PR #114 (merged,
  amended: prior claimants stay fenced, per review) — **done**
  (merged #123; card closed; the claimless-citation fence fix rides
  follow-up #128, per the post-merge round): the exclusive table
  flag, the claim fold (holder, fence, prior claimants), the fence
  rule between grant and lifecycle, structured contention, the
  online-only client seam, contracts v3 with the claim object, cache
  generation 3, the claim race storm and offline-boundary drills
- 5.3 four-part handoff packets — os-b07b0f59 — plan PR #115
  (merged, amended: packets on ALL four exits incl. submission; the
  3072 canonical bound fits the payload cap; the mandatory base
  range; combined anchors, per review) — **done** (merged #124;
  card closed; the resume-drill and packet-shape hardening landed
  via follow-up #127, per the post-merge round): internal/packet strict schema, the packet
  admission rule, the classifier's bare-range exemption, tolerant
  fold counting packetless/fence-violating exits, the A/B resume
  drill
- 5.4 acceptance-spec field + spec gate — os-73c00a50 — plan PR #116
  (merged, amended: no trivial-tier gate exemption; gate evidence
  bound to the acceptance revision, per review) — **done** (merged
  #125; card closed; the explicit executable marker and the -p 1
  coverage serialization landed in-PR per review): the structured acceptance field with
  the universal gate rule and ref/gate commit equality, the
  propose-vs-arm proposal shape rule, contracts v4 with the
  {ref, executable, gated} object, cache generation 4
- 5.5 falsifiable-plan lint + plan-gating — os-16c1d142 — plan PR
  #120 (merged, amended: two-layer plan binding with the Phase 6
  receipt closing the ancestry hole; classify ships as an invocable
  check, per review) — **done** (merged #126; card closed; the
  anchor-equality gate and labeled lint landed in-PR per review; the
  receipt's two plan-line rows go green with engine pin #130):
  internal/plan lint + classifier, seed plan lint/classify verbs,
  exit 16 plan_required, the submission plan gate over the fold's
  tier and plan.approved facts, plan.* capability rows
- 5.6 observation streams v0 + expiry vs. wedge — os-2ff8dbf1 — plan
  PR #121 (merged, amended per review: input-bearing build identity,
  fence-keyed streams, position throttle) — **done** (merged #131;
  card closed):
  internal/obs (fence-keyed JSONL streams, snapshot digest, pure
  classification), the engine Inputs seam with report Version "2"
  and the -i<digest12> build identity, milestone/wedge admission
  (monotonic counts, 25-position spacing, operator wedge facts),
  seed obs emit + rebuild declared inputs, spec/observations.md

**Phase 5 exit (the III.F subset docs/build-plan.md scopes): met.**
Met means the subset the plan's own Phase 5 item list and exit drills
assign to this phase, the scoped-exit posture of the Phase 2 and 3
records: the plan itself defers gate-before-run to Phase 6 (item 4's
named deferral), racing to the fixed-defaults backlog and Phase 13
(item 1 there names it as the III.F remainder), sealed checks to 6.3,
and the dependency-cascade row to the Phase 13 catch-all, so those
III.F rows are later phases' bound work, not this exit's.
The lifecycle vocabulary and
transition rules are self-validating data enforced at admission, and
claim is a transition, not a state (#122, with the fold honoring the
seed/1 activation boundary via #129). Claims are exclusive with
fencing granted only at admission: stale or missing citations refuse
exit 6 naming the cited fence, the active fence, and the holder;
contention returns the structured exit-2 envelope; prior claimants
stay fenced; and a claiming verb cannot smuggle a citation onto an
unheld subject (#123, #128). The claim race storm drill is green on
main, and offline claiming is impossible by construction — exclusive
verbs refuse the local append, drilled at the offline boundary
(#123). Every exit from `in_progress` is one of the four deliberate
exits and each carries a four-part, shape-linted, size-bounded
packet, so silent abandonment is impossible (#122, #124; null parts
refused via #127). Packet sufficiency is drilled end-to-end: a fresh
executor resumes from the packet alone, performs the recorded
unfinished work, lands it, and never re-tries recorded dead ends
(#124, hardened by #127). A contract cannot become claimable without
the structured acceptance field; executable content requires gate
evidence bound to the ref's revision at every tier, and outside text
can propose but never directly become acceptance content (#125).
Plans are falsifiable — boundary set, retention set, validation
commands, expected diff shape, missing retention fails lint — and
submission is plan-gated above the trivial tier with plan and task
PRs structurally disjoint (#126; the receipt's plan-line commands
execute under engine v0.15.1, pinned by #130). Observations v0 close
the phase: fence-keyed lossy streams, monotonic position-throttled
milestones, and expiry vs. wedge rendered as distinct states in the
report (#131). Still-unmet III.F criteria, by landing phase: sealed
checks and their honest-scope documentation (Phase 6, the exit
line's named carve-out); gate-before-run enforcement — the verifier
refusing ungated content (with verdicts, Phase 6); racing mode as
the per-squad opt-in (Phase 13 item 1); and the dependency-cascade
row (advisory wakes land with Phase 7's supervisor; holds,
initiative rollups, and goal-ancestry warnings have no named phase
item and fall to Phase 13's catch-all). This exit record is card
os-6e37b10e's task PR (an administrative card, not a Phase 6 item).

## Phase 6 — Verdict pipeline (docs/build-plan.md Phase 6; deps: 5 ✓)

- 6.1 submission binding, verifier workspace, receipt computation,
  verdict.rendered at L1 — os-f6d2c267 — plan PR #133 (merged,
  amended per review: sandbox runner profile with declared
  capability, immutable head binding, transcript-derived pass
  rule) — **done** (merged #135; card closed; the review round
  landed copied clone objects, the post-review check, and
  stored-evidence verification in-PR): internal/verdict (origin-stripped clone
  workspaces, the exec runner profile named in every receipt, JCS
  receipts with full immutable SHAs and plan-at-merge-base),
  internal/artifact, the verdict admission rule (review-only,
  submission binding, L1 independence exit 17) with the verdict-only
  capability row, gate-before-run exits 18/19, seed verdict
  receipt/render/check with the transcript-derived render rule
  (exit 20) and recompute-and-mismatch (exit 21), plan.Commands,
  spec/verdicts.md
- 6.2 reconciliation chain (merge.requested, merge.observed, done) +
  divergence drills — os-6cdc15be — plan PR #136 (merged, amended
  per review: target-ref observation replaces the unreachable
  two-sha signal; honest ancestry cases with attested_divergence as
  a surfaced state) — **done** (merged #137; card closed; the review
  round landed unlaunderable verdicts at both admitted chain steps
  plus reconcile.VerifyVerdicts/verdict_unverified, the
  coexisting-failure evidence walk, explicit-null chain fields, and
  the cache reconciliation row): merge.requested piped
  (pass-verdict citation, review-only, claim lane), merge.observed
  deepened to the observer's forge fact behind the full chain rule
  with the observer capability lane, fold chain facts, the
  internal/reconcile classifier (merge_without_verdict,
  chain_skipped, neutral unreconciled), contracts v6 / report v3
  reconciliation section / cache generation 5, and seed reconcile
  with the evidence-grade checks (attested_divergence,
  target_rewritten, evidence_missing) and the induced-divergence
  drills, spec/reconciliation.md
- 6.3 sealed checks (salted commitment, age-encrypted body, rotation
  re-encryption, capability audit) — os-3128535a — plan PR #138
  (merged, amended per review: the pre-claim window closes the
  release-path laundering hole, verdict check recomputes sealed
  transcripts, the sealer lane drops the operator fallback, empty
  seals refuse at both ends; the task-PR review round then pinned
  age to the plan's v1.2.1, filtered implementation-capable keys out
  of the recipient set, added the position-accurate authoring check
  at unseal plus the seal_unverified reconcile class, and validated
  the decrypted salt's shape) — **done** (merged #139; card
  closed): check.sealed
  admitted only in ready with no prior claim (one commitment per
  subject; raw seals outside the window fold as anomalies, never
  facts), the salted JCS envelope with the salt inside the
  ciphertext, internal/seal over filippo.io/age v1.2.1 (agessh
  recipients = the verdict-granted keys; the audit's header tag
  scan), the mutable sealed bucket keyed by commitment, the sealer
  capability with grant disjointness against claim and operator
  (root's implicit operator included) plus the seal-author claim
  refusal and the capability-audit decrypt drills, render's
  unseal-and-run with sealed transcripts in the receipt behind
  exits 22/23/24 (the above-trivial gate), check's sealed
  recompute-and-mismatch, seed seal create|rotate|audit (rotation
  re-encrypts open subjects without touching the ledger), the
  reconcile unsealed class, contracts v7 / report v4 / cache
  generation 6, spec/sealed-checks.md
- 6.4 red-verdict lockout + operator override verb — os-d2497eb7 —
  plan PR #140 (merged, amended per review: only boundary-validated
  fails lock or authorize, and the override requires a standing
  validated fail on the current submission — an escape hatch, never
  a bypass) — **done** (merged #141; card closed; the review round
  bound the lockout, the return, and the override to
  boundary-validated fails only, revalidated the override's citation
  at the chain steps, and taught the fold's anomaly check the
  override path): contract.returned resolves
  lifecycle.md's named extension point (review to ready, citing the
  authenticated fail, dispatch/operator lanes; prior facts and the
  seal survive); the lockout scans the whole submission window
  (SubmissionFails) so a raw later verdict never buries an authentic
  fail, refusing pass at admission and at render exit 25 red_locked
  until a new submission; merge.overridden as the operator-only
  attributable fact (strict reason + fail citation, one per window,
  folds as OverrideFact never a verdict) with the citation choice in
  merge.requested ({verdict} xor {override}) and the override-backed
  observed path, both steps validating the override signer;
  reconcile VerifyOverrides with the overridden (neutral, by name;
  override-backed done is never merge_without_verdict) and
  override_unverified classes; contracts v8 (override explicit-null),
  report v5, cache generation 7; lifecycle/verdicts/reconciliation/
  actors spec updates

**Phase 6 exit (the III.G subset docs/build-plan.md scopes): met.**
Met means the subset the plan's own Phase 6 item list and exit drills
assign to this phase, the scoped-exit posture of the Phase 2, 3, and
5 records: the plan itself defers both L2 runtime-tuple separation
and L3 deterministic-first verification (the per-tier level
declarations) and rubric-verdict calibration (per-item evidence, the
human gold set, drift-triggered authority suspension) to Phase 10,
whose exit names levels and calibration together, so those III.G
rows are Phase 10's bound work, not this exit's.
The exit line's three named drills are green on main: the
induced-divergence reconciliation drills (#137's
merge_without_verdict, chain_skipped, neutral unreconciled,
attested_divergence, target_rewritten, evidence_missing, and
verdict_unverified, extended by #139's unsealed and seal_unverified
and #141's overridden and override_unverified — every class induced
and detected); the receipt recompute-and-mismatch test (#135's exit
21, with #139 putting sealed transcripts inside the recompute
boundary so invented sealed outcomes fail the same way); and the
sealed-check audit (#139: sealer/claim and sealer/operator grant
disjointness both directions, the seal-author claim refusal, the
implementer-cannot-decrypt drills, the recipient-tag audit, and
rotation that touches no history). The III.G rows each merged PR
evidences: done is reachable only through the chain with each step
its own event and no code path collapsing them (#137; #141's
override-backed path stays uncollapsed); verdicts are signed by
verdict-granted keys provably disjoint from every implementing key,
and operator override is its own attributable verb, never a
disguised verdict (#135's L1 exit 17; #141's operator-only
merge.overridden folding as its own fact, surfaced by name);
the verifier executes in clean per-run isolation with cleanup firing
pass or fail and enumerable, exclusively self-executed inputs
(#135's origin-stripped workspaces); receipts bind contract id, plan
hash at merge-base, diff hash, changed-file inventory, visible and
sealed check transcripts, and environment fingerprint, and
verification recomputes everything from the submission head (#135,
#139); and a red verdict is unmergeable and locks the implementer
out of self-approval until a new submission (#137's chain-legality
half; #141's lockout exit 25 with contract.returned as the resolved
return path). The Phase 5 exit's named sealed-checks carve-out is
closed by #139, honest-scope documentation included. This exit
record is card os-600be59e's task PR (an administrative card, not a
Phase 6 item).

## Phase 7 — Supervisor, offers, budgets (docs/build-plan.md Phase 7; deps: 5 ✓)

- 7.1 offers (offer.published eligibility-scoped and expiring;
  workers pull and claim; the wakeless poll-only drill proving wake
  is advisory) — os-c61c3392 — **done** (merged #145; card closed;
  plan #144 with the #146 validation-command amendment; the
  supervise lane, claimed-or-expire liveness with the claim as
  consumption boundary, foreign offers inert at the list surface,
  the poll-only and race drills, and the review round's operator
  discoverability and last_claim byte-identity fixes; contracts v9 /
  report v6 / cache generation 8)
- 7.2 budgets (budget.reserve / settle / release; admission
  decrements reservations; the reservation race drill; per-adapter
  risk limits) — os-cecac5de — **done** (task PR #149 merged,
  against plan #147 as amended by #148; class-table capacity,
  holder-only reserves, derived closes with foreign facts inert on
  every surface, the two-drafts-one-view race drill, the empty
  spending gate, contracts v10 / report v7 / cache generation 9)
- 7.3 executor adapter interface + the local worktree adapter
  (provision / wake / meter / report-tuple; metering to the
  observation stream; run.settled aggregate) — os-1dad487d —
  **done** (task PR #151 merged against plan
  #150: the public executor package, the
  reserve/start/provision/meter/settle bracket with run.started
  filling the spending table, the SIGKILL disposability drill,
  contracts v11 / report v8 / cache generation 10; the merge raced
  the final review-round push, so two findings — Provision
  authenticating folded starts via the shared admit.RunStartValid,
  and worktree rollback on provisioning failure — landed in
  follow-up task PR #152, merged)
- 7.4 graceful preemption (safe-point park with packet; force reap
  packet) — os-0f718b4e — **done** (task PR #154 merged against
  plan #153; card closed: run.interrupted as a once-per-fence
  supervisory ledger fact with position-accurate validity shared by
  admission and polling workers, safe-point semantics specified as
  the worker contract, the graceful park drill and the force reap
  drill both completing elsewhere, contracts v12 / report v9 /
  cache generation 11; the review round hardened the validity
  helpers to re-run each verb's strict payload decode at the fact's
  own position, gated once-per-fence settles on boundary validity,
  and indexed the interrupts cache table)

**Phase 7 exit (the III.H subset docs/build-plan.md scopes): met.**
Met means the subset the plan's own Phase 7 item list and exit line
assign to this phase for the implemented adapter, the scoped-exit
posture of the Phase 2, 3, 5, and 6 records. The exit line's three
named drills are green on main: the wakeless poll-only run through
the whole loop (#145: wake is advisory transport whose total
failure costs only latency); the reservation race drill (#149: two
drafts admission-checked against one view, the second refusing at
append, so concurrent over-spend against one budget is structurally
impossible); and the disposability drill (#151: a real worker
subprocess SIGKILLed at a seeded randomized site after an admitted
synchronization, the chain verifying, the contract completing
elsewhere from the surviving ledger alone, and the loss window
exactly the post-sync observation lines, stated honestly). The
III.H rows the merged PRs evidence for the implemented adapter:
scheduling is offers-and-claims with eligibility-scoped, expiring
offers, workers pulling and claiming, and exclusivity settling at
admission so duplicate scheduling is impossible and no assignment
can orphan (#145); spending verbs require an admitted
budget.reserve with execution fenced to the reservation —
run.started fills the spending table, Provision refuses outside the
gate, and settle/release records actuals (#149, #151, with #152
authenticating folded starts position-accurately so fold presence
is never admission); metering flows on the observation channel and
settles to the ledger at run end via run.settled, drilled through
the full bracket (#151, #152) — with the row's universal half, "no
execution path is unmetered", recorded unmet at this exit, not
claimed: Meter is caller-optional and nothing structurally prevents
an unmetered caller of the public adapter interface, so what this
phase landed is the metered path and the drilled bracket, while the
universal half routes below (budget consumption stays enforced
upstream at the reservation gate either way, so an unmetered run
under-reports telemetry, never overspends); the adapter interface
is public with the local worktree adapter as its first
implementation (#151, #152); and
preemption is graceful-first with safe-point semantics specified as
the worker contract and a force kill still yielding an honest reap
packet (#154 — landed beyond the exit line's three named drills, so
the III.H preemption row closes with the phase rather than
waiting). The local worktree adapter can be stopped synchronously,
so the risk-limit posture spec/budgets.md declares has no
per-adapter declaration to make yet. The unmet III.H remainder
routes as obligations named in the landing phases' own build-plan
text: the container, cloud-session, and enrolled-remote adapters
with their per-adapter risk-limit declarations (Phase 13 item 2);
the remaining-reservation budget block in the worker's envelope
(Phase 8 item 1); exhaustion parking — the worker loop that answers
the budget gate's structured refusal by taking the 7.4 claim.parked
exit with its packet, consuming Phase 8's budget block — named in
Phase 9 item 1's worker-lane loop; the metering row's unmet
universal half in two named steps — detection as Phase 9 item 3's
unsettled-run lint (position-anchored: a started window lacking its
run.settled is flagged only once the subject takes a subsequent
claim window or reaches a terminal state), and the
full-conformance claim on Phase 13's exit line, which already
requires the named III.H criteria green; and scheduling inputs
completing with routing-as-data in the Phase 9 dispatcher and
qualification tuples in Phase 10 item 1 (cost classes exist today
in the filed budget class). This exit record is card os-c9e24032's
task PR (an administrative card, not a Phase 7 item).

## Phase 8 — Affordance envelopes (docs/build-plan.md Phase 8; deps: 5 ✓)

- 8.1 affordance envelope (affordances from the same internal/admit
  rule set; position; budget block; structured errors, exit codes)
  — os-f5551001 — **done** (#160 merged, card closed; plan #158 as
  amended:
  admit.Affordances probing every catalog verb with signed drafts
  through the enforcing Check, the completeness-pinned synthesizer
  table with the actor.enrolled carve-out, the budget block from
  BudgetViewAt, stamping on every append path success and refusal
  plus budget status --key, the lifecycle-walk and CLI stamp
  drills)
- 8.2 regression class: affordance-listed verb refused for legality
  at the same position = bug — os-148d3ba1 — **done** (#163 merged,
  card closed; plan #162: the walk's history extracted into the shared
  walkScript, the prefix-sweeping TestAffordanceRegressionClass
  re-drafting every listed verb through the enforcing Check for all
  seven enrolled-lane pairs at every position, and the CLI drill
  pinning that stamped position and stamped list agree with
  independent recomputation on success and refusal envelopes)
- 8.3 refusal-rate metric in the report — os-edf73d66 — **done**
  (#165 merged, card closed; plan #164 as amended: the attempts journal
  journaling both outcomes best-effort at every stamped
  admission-boundary seam, the journal as a declared digest-covered
  report input via --refusals, the nullable refusals section with
  one-population counts and the four-decimal rate, report v10,
  spec/refusals.md, and the D4 drills including the
  1-refusal-beside-100-admissions=0.0099 fixture; the review round
  hardened the journal against torn short writes — Note restores the
  previous length when the fragment is provably the tail, and Load
  treats the terminating newline as the commit marker so an
  uncommitted fragment cannot poison every future declaring build,
  while terminated lines stay strict)
- (out of item) ledger writeHead race fix — os-c6fb95ee — **done**
  (#161 merged, card closed: per-writer unique temps preserving the
  established HEAD mode, with the store-level contention regression
  test; the flake tripped TestGracefulPreemptionDrill on main)

**Phase 8 exit (the III.I subset docs/build-plan.md scopes):
met.** Met means the two criteria the plan's own exit line names,
the scoped-exit posture of the Phase 2, 3, 5, 6, and 7 records. The
same-rule-set property test is green on main (#163: every listed
verb independently re-drafted and run through the enforcing Check at
every prefix position of the shared walk scenario, for all seven
enrolled-lane pairs, with determinism and strictly ascending output
asserted, so a later split between computation and enforcement fails
as a named class rather than drifting), and the envelope schema is
stable and versioned (seed-envelope/0, its bump discipline stated in
spec/envelope.md, exit codes allocated in the spec table rather
than ad hoc). Charter III.I as a whole is NOT claimable at this
exit, so all five rows are walked rather than the two the exit line
scopes. Row 1 (versioned schema-stable envelope, structured errors,
meaningful exit codes, the verbs currently legal for this actor on
this subject, the position it was computed at): met by #160, with
three bounded carve-outs named rather than glossed — a response
carrying no actor-and-subject context (keyless read surfaces; probes
must be signed, so a fingerprint alone cannot compute a list) lists
the empty set, which is the honest answer where no such set exists;
actor.enrolled is listed only where the prober could supply the
subject's public key, which no fingerprint-holder can derive; and a
refusal that never opened a ledger carries a null position rather
than inventing one, which the spec sentence corrected in this PR now
states. Row 2 (one rule set for computation and enforcement;
listed-then-refused at the same position a bug class with a
regression test): met by #160's computation plus #163's sweep. Row 4
(refusal rates tracked as an affordance-gap metric): met by #165's
attempts journal — attempts, both outcomes, journaled best-effort at
every stamped admission-boundary seam — and report v10's refusals
section, whose rate draws numerator and denominator from that one
population rather than from the chain. Row 3 (the CLI is the
complete interface; a machine-protocol surface exposes identical
semantics; platform parity including Windows documented and tested)
and row 5 (matching promoted lessons surface in packets and
envelopes at claim time) are recorded UNMET, not claimed: no
machine-protocol surface and no platform-parity documentation or
test exist today, and row 5 presupposes the curator's promoted-lesson
store. Neither was routed anywhere in the build plan, so this task
extends the plan to name them in the landing phases' own text — row
3 as Phase 13's machine-protocol-and-parity item, with III.I added
to that phase's exit line, and row 5 in Phase 11 item 2's promotion
gate, beside the applies-when it already records. Full charter III.I
conformance is therefore claimable only once Phase 11 and Phase 13
both close. This exit record is card os-ef715d17's task PR (an
administrative card, not a Phase 8 item).

## Phase 9 — Lanes, escalation, maintenance (docs/build-plan.md Phase 9; deps: 6, 7, 8 ✓)

- 9.5 the lane-facing surface, parts (a) and (b) — os-52d5da3f —
  **done** (merged #171 against plan #170: internal/obligation
  deriving what is owed from the same fold admission enforces, with
  discharged_by read from the transition tables and a closed
  fact-shaped set for the verbs that change no state; the obligations
  projection registered alongside the others; seed situation
  rendering one position-stamped envelope whose --since is a complete
  change report with an explicit discharged list; and the
  dischargeability sweep over every prefix of the shared walk
  scenario, which caught a real modeling error on its first run)
- 9.5 part (c) loop verbs — os-7e197768 — **done** (merged #173
  against plan #172: the seven acts that close poll → claim → work →
  meter → submit → exit, each deriving the fence from the active
  window, the reservation from the shared budget view, the plan
  anchor from the approval, and the resume range from the
  repository, and each pre-flighting through the same admit.Check
  admission enforces so a refusal carries the boundary's own error
  beside the caller's affordances; claim take remote-only; the
  packet validated at the door; new spec/loop-verbs.md). The
  post-merge review found three defects, all live on main and carded
  as os-9b3f3ef3: derived arguments not re-derived across the
  optimistic retry, a remote refusal stamped from the stale view, and
  a JSON null packet panicking the CLI — fixed by that card's own task
  PR, which also records in `spec/loop-verbs.md` that a derived
  value is re-examined against the refreshed tip and refused rather
  than replaced.
- **9.5 part (b) is MET** (os-8451d939, plan #209): the situation read
  carries the caller's messages, with "unread" derived from the cited
  `--since` position rather than stored read-state, so no
  `message.read` verb exists and `message.acked` stays unimplemented.
  It carries **notices, not bodies** — sender, contract, position,
  size — because `message.sent` needs no capability at all and the
  orienting read is taken on every wake, unbidden; the injection sweep
  now plants a hostile message addressed to the reading worker, and
  both the addressed and broadcast paths through the filter. The body
  is reached by `seed message read --at <position>`, the deliberate
  second act, which appends nothing and refuses a non-recipient with
  the same `not_found` a position holding no message gets. A `to` that
  is present and does not parse addresses NOBODY rather than everybody
  (review finding on #209): broadcasting a malformed address widens
  delivery from one intended recipient to every actor on an encoding
  slip.
- 9.1 lane role fragments, dispatcher least-capability, injection
  corpus, worker-lane loop with exhaustion parking — its
  role-definition text binds four ergonomic obligations (os-68ea0b2d):
  one position-stamped read named in each fragment, the loop acting
  through the item 5(c) loop verbs rather than the raw append seam,
  liveness riding the loop's own steps with no heartbeat verb, and the
  one-inbox doctrine. Too large for one plan, so split three ways the
  way item 5 was:
  - 1a the six lane fragments — os-cf1c9688 — **done** (merged #188;
    card closed: lanes/** as manifests plus ordered prose
    fragments, the four obligations as DECLARED FIELDS rather than
    paragraphs, and internal/lane checking each against an authority
    elsewhere — capabilities against internal/keyring, acts against the
    new internal/loopverb registry, and a lane's grants against what
    keyring.AcceptedCapabilities says each act's verb accepts. Plus
    `seed lane list|show|validate` and exit 26 lane_invalid)
  - 1b dispatcher least-capability drilled by the injection
    conformance suite (hostile corpus) — os-b779b4c7 — **done**
    (merged #192; card closed). III.J's second row is now
    **two-thirds met**, and the spec says so rather than reporting it
    closed: intents and tool output are covered, mirrors are not,
    because request.* has zero rows in the transition table.
    - Reachability is derived from admit.Affordances, which drafts a
      signed probe per catalog verb through the same Check pipeline
      admission enforces. NOT from keyring.AcceptedCapabilities: that
      table returns nil for the standing-only class, so a capability
      filter silently omits message.sent.
    - Three residuals named and pinned by characterization drills:
      the filed tier's "trivial" exempting both the plan gate and the
      sealed-checks lint (carded os-be12ac16 for Phase 10's tier
      system); claim.reaped consulting no liveness evidence, its two
      preconditions being freshness and attribution rather than
      authorization; and message.sent needing NO capability at all,
      which is the one that relays.
    - Tool output cannot inject, structurally: verdict.Transcript
      hashes output bytes at the boundary and drops them.
    - The projections carry every payload verbatim by design, which is
      where the unlanded mirror arm will land: whichever card adds
      request.* inherits an input already carrying hostile text.
  - 1c the worker loop made executable with exhaustion parking —
    os-abb206c8 — **done** (merged #191; card closed). Four review
    findings arrived after it merged and land as os-378e44f3: the
    actor derived from the key rather than supplied beside it (and
    re-derived each iteration, since a rotated key reopens the same
    mismatch through the filesystem), a claim reaped before the
    post-claim read ending the iteration idle rather than erroring,
    the fence adopted from the act that opened it so the claim is
    observable under its own window, and packet temp files unlinked
    on every path. It inherited
    1a's unfinished half and closed it: the loop emits as a side-effect
    of a declared liveness act that SUCCEEDED, keyed to its own actor
    and the fence its orienting read reports, so `liveness_from` is now
    what decides what happens rather than a label compared to another
    label. Three things the work turned up:
    - **The posture did not line up.** `claim take` refuses `--ledger`
      outright, while `offer list` and `situation` bound `--ledger`
      alone: in the only posture where a lane can claim it could
      neither poll nor orient. Both reads took the exclusive-or, and
      lane.SituationFlags follows with Posture beside Required.
    - **Exhaustion is the reserve, not the spending gate.**
      IsSpendingVerb holds only run.started, admitted from {supervise,
      operator}; the implementer holds claim, so that gate is the
      executor's and unreachable here. The drill asserts the capacity
      refusal by its own message so it cannot become a different one.
    - **The situation read withheld the acceptance anchor**, so a lane
      could not write the packet its own exit requires. The window now
      reports it.
  Item 1 is complete with 1a (#188), 1c (#191) and 1b (#192), its
  four review findings landed as os-378e44f3, and the four ergonomic
  obligations are real rather than declared: the one position-stamped
  read each manifest names (since 1c, in the posture the lane can
  actually claim in), the loop verbs as the loop's acts (enforced by
  1c's act gate, which refuses anything `acts_through` does not
  declare), liveness riding the loop's own steps (emitted as a
  side-effect of a declared act that succeeded, keyed to the lane's
  actor and fence), and the one-inbox doctrine (the loop acts on its
  read, never on a wake).
- 9.2 escalation with packet, question and decision — os-f781f0da —
  **done** (#200 against plan #197): `escalation.raised` (`ready`,
  `review` -> `blocked`) and `decision.recorded` (`blocked` -> `ready`)
  are the two new rows; from `in_progress` an escalation rides
  `claim.parked`, because what `lifecycle.md` pins is the set of verbs
  that may LEAVE that state, so the four deliberate exits stay exactly
  four and their existing pinning test is now the enforcement of that
  rule. `spec/escalation.md` carries the design. Raising is broad
  because raising grants nothing; answering is the fourth no-fallback
  operator row. While a question stands, `contract.unblocked` refuses
  and `contract.cancelled` must cite the escalation it answers. An
  escalation answering a refusal carries that refusal's code and
  message, so the human is asked the boundary's own question. Waiting
  escalations surface as `escalation.pending` with the raising event's
  `ts`, because age is elapsed time and a position difference is event
  count wearing a clock's clothes.
- 9.3 unattended maintenance, whose lint list carries the Phase 7
  exit's unsettled-run detection — os-8a5f14bb — **done** (#205
  against plan #203): `seed maintain run`, one unattended pass (reap,
  lint, file, rebuild, checkpoint) with no scheduler and no wake
  channel. Three things about it are worth carrying forward rather
  than rediscovering. **The reap answers an unanswered request, never
  a timeout**: `observations.md` says Seed holds no lease, so there is
  no expiry to elapse, and the channel is declared lossy — a dropped
  stream and dead work look identical from outside; a reap therefore
  needs the `expired`/`wedged` classification BESIDE an admitted
  `run.interrupted` on the active fence (or a `wedge.declared`), which
  is exactly the force path `executors.md` named this loop as the
  consumer of, and `no_data` carries no reap path at all, however old
  the claim. 9.1's loop emitting observations from its own steps makes
  an expired classification better evidence, by removing forgotten
  bookkeeping as a cause of silence; it does NOT make silence proof,
  and no heartbeat predicate is added either, because non-advancing
  observations are a legitimate long-running step (os-68ea0b2d). **The
  pass consumes the COMPLETE reconcile result**: the evidence-grade
  checks — attested heads, rewritten targets, receipt retrievability —
  moved out of `cmd/seed` into `internal/reconcile`, because a pass
  built on the record-derived half alone reports clean over a
  rewritten target. **Checkpoints persist a snapshot a fresh reader
  starts from**, its hash and location in a versioned payload the
  boundary validates, the materialization written to the artifact
  store first: shape at the door, contents at the read, drilled by a
  round trip.
- 9.4 small-team and fleet fixtures — os-6a08b166 — **done** (#207
  against plan #204): both modes run the full loop to `done` on the
  remote posture, wakeless, with every convergence arm exercised —
  every refusal converges within one retry in one of three ways (an
  admitting act, a refreshed read showing the act is no longer owed,
  or an escalation carrying the refusal), the middle arm the common
  case (a fleet-mode claim race means the loser re-orients), and the
  forbidden fourth outcome — a blind retry or a silent loop — pinned
  by its own detector (os-68ea0b2d). Three things this card settled.
  **The terminal surface was missing, not merely local-only**:
  `merge.requested` and `merge.observed` had no CLI verb at all,
  existing only through `ledger append`, which runs no rules; all
  three terminal verbs (with `verdict render --remote`, the gap plan
  #204 found and carded as os-a00b6950, absorbed by this PR and the
  card cancelled) now reach both postures, so the chain's last steps
  are drivable by a lane. **The mode is purely the identity plan**:
  both modes run remote — fleet needs it for contention, and
  small-team could never have run locally, because `claim take` is
  refused off the remote and a claim is the loop's first act; neither
  clause of III.J mentions transport. **The role-grant gap**, below,
  which this card's fixtures surfaced.
- (out of item) the shipped lane set granted neither `supervise` nor
  `observer` nor — found in review, the worst of the three since it
  has no operator fallback — `sealer` — os-d6a52784 — **done** (#212
  against plan #210): the charter's six lanes (§II.11) are a closed
  enumeration, and both missing parts already exist in the charter
  outside it (the supervisor is §II.9 and the observer is §8's
  governed observer), so they ship as **roles**, manifests of a
  required `kind` beside the six, validated by the same code, refused
  by name if they claim to be a seventh lane; `sealer` rides the
  verifier, whose keyring the sealed bodies are already encrypted to.
  The mode fixtures provision both roles from their manifests rather
  than staging them as identities the test invented, which is what
  makes the closure real rather than reported. The drill whose
  absence let this reach Phase 9 reads the capability table's own
  source and, run against the pre-fix tree, names **six** ungranted
  verbs across the three capabilities — three more than the card had
  found.
- out-of-item: a reservation outlives its window — os-d6963652 —
  **done** (#182, merged, against plan #175: admission gated all three
  budget verbs on `in_progress`, so a reservation whose claim window
  ended could never be settled or released while `BudgetViewAt` kept
  counting it against capacity; the gate moves to `budget.reserve`
  alone, the `budget.open` obligation drops the live-window
  restriction its reason no longer supports and is attributed
  standing-awarely to whoever can still close it, and the three budget
  affordance probes move to the conditional fence citation without
  which the dischargeability sweep could not go green at the very
  prefixes the fix is for; the shared walk now ends by suspending and
  revoking the lane holding an open reservation)

**Phase 9 exit (charter III.J as docs/build-plan.md's exit line
scopes it): met.** Met means the three criteria the plan's own exit
line names, the scoped-exit posture of the Phase 2 through 8 records,
each backed by a drill on `main` that a reader can find by name.
*Both modes run the full loop in CI*: `TestSmallTeamModeReachesDone`
and `TestFleetModeConvergesAndReachesDone`
(`cmd/seed/modes_e2e_test.go`), remote posture, no wake channel, every
refusal converging within one retry, with `TestBlindRetryDetector`
pinning the forbidden fourth outcome, and since #212 both modes
provisioned from the shipped role set alone rather than from
identities the fixture invented. *Injection corpus green*: the eight
corpus files under `internal/admit/testdata/injection/` with
`TestNoHostileTextWidensTheDispatcherSet`, plus the containment sweep
`TestIntentProseReachesNoDownstreamReadAutomatically`, which since
#211 plants its marker in a message payload and a message subject as
well as in an intent. *Maintenance runs unattended in the fixture*:
`TestMaintainHoldsNoPrivatePowers`,
`TestMaintainFilesDefectsAndRaisesNoEscalation` and
`TestMaintainCheckpointIsStartableByAFreshReader`
(`cmd/seed/maintain_cli_test.go`), one pass, no scheduler, no wake,
audited as an ordinary actor. Every numbered item has a merged PR:
1 across #188, #191 and #192 (review fixes os-378e44f3); 2 in #200;
3 in #205; 4 in #207; 5(a) in #171, 5(c) in #173 (post-merge defects
os-9b3f3ef3), 5(b) in #211; and the role-grant gap item 4 found, in
#212. Charter III.J as a whole is walked rather than the three the
exit line scopes, and two of its six rows are recorded UNMET rather
than glossed. Row 1 (role definitions for all six lanes as grants +
conventions, ordered fragments, validated): met by #188, with #212
making "six" enforced by name rather than counted. Row 2 (dispatcher
least standing capability; injection suite over intents, mirrors and
tool output): **UNMET, not claimed** — the row is conjunctive and the
mirror arm cannot be met, because `request.*`, the family mirror edits
and dashboard actions enter by, has zero transition rows and the build
plan named neither the family nor the word "mirror" anywhere; intents
and tool output are covered by #192 and least standing capability by
#188's allowlist. Row 3 (dispatcher re-triage rate and planner
unedited-approval rate tracked; planner receives the strongest tuples
by policy): **UNMET, not claimed** — nothing in the tree computes
either rate, and "strongest tuples" presupposes Phase 10's tuple
system. Row 4 (maintenance unattended, audited as an ordinary actor):
met by #205, with `TestMaintainHoldsNoPrivatePowers` as the
audit-posture pin. Row 5 (escalations carry packet + question +
minimal decision; waiting ones surface with age; resolution latency
tracked): met by #200 — `escalation.pending` carries the raising
`ts`, and `seed decision record` reports `resolved_after_seconds`,
derived from the chain and stored nowhere. Row 6 (small-team and fleet
modes run the full loop in CI): met by #207 with #212. Both unmet rows
are routed in the build plan's own text by this record, the move the
Phase 8 record made for III.I: row 3 to Phase 10 items 1 and 5 and
Phase 10's exit line, and row 2's mirror arm to Phase 13 item 4 and
Phase 13's exit line beside III.I. Full charter III.J conformance is
therefore claimable only once Phase 10 and Phase 13 both close. Two
things this phase learned are worth one sentence each. Its frontier
line was wrong once, claiming the phase complete while item 5(b) had
no implementation and two other paragraphs of this file said so; the
exit record re-derives the item list from the build plan rather than
reading the summary, which is how the gap was found. And three cards
in a row found hand-listed counts wrong — the exit-code table and the
refusal-site matrix (os-d03bde01), the capability-coverage gap
(os-d6a52784, where the derived drill found six ungranted verbs
against a card that named two capabilities) — so when a criterion says
"all N", N is a claim a drill derives, never a number it is told
(`memory/LEARNINGS.md`). This exit record is card os-e6cdb3d9's task
PR (an administrative card, not a Phase 9 item).

## Phase 10 — Qualification and evaluation (docs/build-plan.md Phase 10; deps: 9 ✓)

- 10.1 runtime tuples in grants, reported by adapters, drift as
  out-of-grant — os-8e53ffd9 — **done** (#216 against plan #215: the
  five-field tuple spelled once in `internal/tuple`; `actor.granted`
  citing an optional `tuple` at `seed/2` positions only, the keyring
  keeping the set per capability beside the string view of grants;
  `run.started` declaring a required `tuple` from `seed/2` and refusing
  one before it; the set rule at admission against the CLAIM HOLDER's
  `claim` grants (empty = unqualified, the bridge; non-empty = equal
  one member per field, else `out_of_grant` with a `Drift` detail
  naming the holder, the field and both values, exit 14 unchanged);
  `seed run start` deriving fence and reservation, filling harness and
  environment from the adapter and never inventing principal, model or
  tool policy; `Run.Tuple()` as the resolved configuration and
  `Provision` refusing `ErrTupleMismatch` with rollback when the
  adapter resolves anything else; `offer.published` scoping by `tuples`
  with `offer list` and the loop's poll filtering on it; the `seed/2`
  register entry and `version.Activated` as the named list the fold and
  keyring gate on; `spec/qualification.md`. Review fixes: a
  raw-pushed start's declaration re-judged in `RunStartValid`, the
  offer's `tuples` gate reading presence, a malformed scope folding to
  nothing.)
- 10.2 eval contracts through the production machinery; grants cite
  passing tuples; spot-checks; suspension on failure — os-03e47abb —
  **done** (#221 against plan #217: `seed/3` in the register
  with `version.EvalApplies` gating the `eval` marker and the two verbs
  at `seed/3` exactly; `internal/eval` (definitions under
  `evals/<name>/`, the anchor read from the repository's last
  squash-merged commit touching the definition, `Check` proving the
  known verdict fixture-red-then-solution-green through the verifier's
  runner, `File`'s stable id, `Due`'s derivation of what the chain owes
  at a declared instant); the shipped `fix-the-check` eval;
  `intent.filed`'s optional `eval` marker folded to `SubjectState.Eval`
  and refused at an earlier tip; `actor.qualified` and
  `actor.disqualified` as the first non-operator actor rows, the
  keyring's admissible set and ever-cited mark, the qualification rule
  at admission (eval subject, authenticated verdict, the window's
  declaration on five fields, holder not signer, `ts` ordering, one
  verdict one consequence); the set rule exempt on eval subjects and
  the closed bridge for a once-cited empty set; mints gated on a
  recomputed receipt; tuple-wide disqualification; spot-checks aging
  from the record's own `ts` against `--as-of`; the eval terminal in
  obligations and reconciliation (D10); `seed eval list | check | file
  | status | act`, the acts performed by the lane that holds them;
  `spec/evals.md` and eight spec edits. Ranking waits on item 5's
  metrics; calibration is item 4.)
- 10.3 independence levels L2/L3 declared per tier and recorded in
  verdicts — os-99829835 — **done** (#233 against plan #223:
  `seed/4` activates the ordered level vocabulary, `verdict.rendered`'s
  `independence` widening from the literal `L1` to `L1`/`L2`/`L3` and
  optionally carrying the verifier's declared `tuple`; the tier table's
  `independence` column (`trivial` and `standard` L1, `critical` L2,
  the strictest row L3) is read through `TierGates`; the level is
  computed, never asserted — L2 when the verifier's declaration differs
  from the window's admitted `run.started` in model provider or family
  (the optional `<provider>/<family>/<version>` convention) or in
  harness name, L3 when the acceptance is executable and gated with the
  receipt's reproduction at evidence grade, L1 otherwise — and the
  claimed level must equal it (a shape refusal naming both) and satisfy
  the tier (`level_short`, exit 17's refinement); `verdictBoundary`
  reapplies both along the merge chain, so a raw-pushed `critical`
  verdict at `L1` authenticates nothing; `seed verdict render` takes
  `--principal`, `--model` and `--tool-policy` (all three or none, usage
  on a pre-`seed/4` chain), fills the adapter's two fields, computes the
  level before drafting and refuses at usage when a required
  declaration is missing; `verdict check` and the contracts view render
  the level; reconcile classifies `independence_unverified` (the record
  half, and the receipt's reproduction, sealed subjects under `--key`,
  the maintenance loop reporting what its key cannot open); the
  contracts projection republishes as version 13; `EvalApplies`
  is the named list `{seed/3, seed/4}`; both modes drive a `critical`
  contract to `done`, small-team at L2 with a second model family and
  fleet at L3 on an executable gated spec)
- (out of item) the tier vocabulary and its table, the residual item
  1b found — os-be12ac16 — **done** (#222 against plan #219:
  `spec/tiers.md` declares `trivial`, `standard` and `critical`
  with a plan-required, sealed-checks-required and human-review column
  each, mirrored by `transition.Tier(name)` and pinned against the spec
  in both directions; `intent.filed` validates `tier` and `budget`
  against their tables at admission byte for byte
  (`VocabularyError`, the completeness family, naming the members); the
  three authority sites (the plan gate, the reconcile `unsealed` lint,
  `verdict render`'s `unsealed` refusal; the card said two, the tree
  had three) read the table through `TierGates`, an unknown tier taking
  the strictest row; the injection suite's characterization pin is
  replaced by the vocabulary drill, with mis-tiering (filing the valid
  `trivial`) kept pinned as tier provenance's residual; item 3 added
  the `independence` column)
- 10.4 rubric verdicts (per-item, evidence-cited, uncertainty-marked);
  calibration harness with authority suspension on drift —
  os-2e34f66a — **done** (#238 against plan #225: the
  `## Rubric` section of the acceptance spec read at the anchor as the
  commands are (`plan.Rubric`, a duplicate or empty id refusing
  `spec_unrunnable`); the scorecard artifact (items with score,
  cited evidence, explicit two-valued uncertainty, a bounded note)
  validated against the rubric, the receipt and the repository, its
  derivation-bearing half on `verdict.rendered` as `scorecard`
  (`seed/4`); `transition.DeriveScores` at render (`rubric_red`,
  `human_verdict` under exit 20), at admission (a verdict whose own
  items refute it never lands) and along the merge chain
  (`scoreBoundary`); `verdict.deferred` from a verdict key under L1,
  citing the receipt it computed and the items at high uncertainty,
  the whole verdict deferring on a human-review tier, creating
  `verdict.human` owed by the operator lane; the human as a key with
  a verdict grant beside operator standing, rendering over the
  deferral's receipt because sealed checks encrypt to keys without
  operator standing; the tier table's `human review` column
  consumed; `scorecard_unverified` at evidence grade; calibration
  definitions (`kind: calibration`, the gold committed to by digest
  and held outside the tree, `--gold` supplying it, the floor pinned
  to `evals.md` and raisable never lowerable), agreement at low
  uncertainty, the `verdict` qualification for the verifier's
  declared tuple, drift's tuple-wide disqualification and the
  dispatcher's idempotent defect filing, the set rule at render
  (`out_of_grant` under a drifted configuration until re-calibrated),
  spot-checks aging verdict qualifications; `seed verdict render
  --scorecard`, `seed verdict defer`, `seed eval status|act --gold`;
  drilled at the boundary (every D3 and D4 row, the chain, the
  qualification, the set rule, `seed/3` refusing each), in the fold,
  the obligation, reconcile and eval packages, at the terminal
  (scorecards, the deferral and the human's render, the situation
  read's debt, lost scorecards classifying, the calibration lifecycle
  with drift and re-qualification) and end to end in small-team mode
  (a rubric contract and a deferred one reaching done); no shipped
  calibration definition; the verifier lane's summary and fragment;
  `verdicts.md`, `evals.md`, `qualification.md`, `obligations.md`,
  `reconciliation.md`, `acceptance.md`, `envelope.md`, `tiers.md`,
  `actors.md`, `protocol.md`, `lanes.md`)
- 10.5 trajectory-prefix regression harness; dispatcher re-triage rate
  and planner unedited-approval rate (III.J row 3's metrics half,
  routed here by the Phase 9 exit record) — os-6bd9ffff — **done**
  (#239 against plan #227: `internal/trajectory` records a
  lane's decision points at the frames it decided from, its admitted
  records at `records[:p]` and its refused journal lines at
  `records[:p+1]`, and replays them against the chain and the lane
  configuration as they stand, five point classes and two
  configuration classes, `seed trajectory record|replay` with exit 26
  refining `trajectory_diverged`; the corpus under
  `trajectories/lanes`, one file per shipped manifest, recorded by
  a scenario that drives every lane through its own acts and one
  refused attempt each, reproduced byte for byte and replayed green by
  drill, planted rows failing with the named classes;
  `contract.specified` gains the `ready` origin at `seed/4`
  (re-specification, refused by version before it, `Specifications` on
  the fold), the plan verbs carry the plan's content digest at `seed/4`
  (`seed plan propose|approve` derive it from the repository; the fold
  keeps the first proposal's and the approval's; unedited iff equal),
  and the report's `lanes` section (version 13) carries the re-triage
  and unedited-approval rates, null over nothing. III.J row 3 is met
  by the metrics half; III.O row 5's recorded half is met, its
  simulation-mode half is Phase 12 item 6's (the Phase 10 exit record
  corrected the spec's "Phase 13"), and the residual is stated in
  `trajectories.md`: no decider re-runs at a point, so replay proves
  the configuration still presents the same frame and permits the same
  act, not that a model would choose it.)

- out of item: the client's private git dir and the verifier's clone
  arm auto-gc in production — os-711b3028 — **done** (#232 against
  plan #224: `gitref.NewClient` writes `gc.auto=0`,
  `gc.autoDetach=false` and `receive.autoGC=false` into its git dir on
  every construction, so a state dir an older build created is
  hardened the first time a newer build opens it; `verdict.NewWorkspace`
  writes the same three keys into its clone between the clone and the
  checkout; the drills assert the repository-local property with `git
  config --local`, which the test binary's global config cannot
  satisfy, and an older-build drill proves the write happens on the
  no-init path and that a second open changes nothing).
**Phase 10 exit (docs/build-plan.md's exit line as this record
revises it: charter III.E tuples, III.G levels and calibration, III.O
eval items, III.J row 3's metrics half): met.** The revision comes
first because it is the record's one act of judgment. The exit line
as written named III.J row 3 whole, and the row's policy clause ("the
planner lane receives the strongest tuples by policy") is not met:
item 1 landed the offer's `tuples` scope as the scheduling input,
item 2's `evals.md` deferred the ranking policy by name, and no later
Phase 10 item re-homed it. Two honest courses exist from there —
hold the phase open until a ranking policy lands, or revise the
criterion in the plan's own text and say so — and this record takes
the second: holding Phase 10 open would block Phase 12, the release
gate and the migration promotion needs, on a quality policy that §5's
reasoning for what must precede cutover does not reach. So Phase 10
item 1's clause and Phase 10's exit line now say what landed and
where the policy went (Phase 13 item 7), the row itself is recorded
UNMET below, and what this record claims met is the revised line.
Each of its terms is backed by named drills on
`main`. *III.E tuples* (rows 6 and 7):
`TestRunStartDeclaresTheTupleAndDriftIsOutOfGrant`,
`TestDriftIsPerFieldAgainstTheHolder` and
`TestSmallTeamQualifiedWorkerIsOfferedAndHeldToItsConfiguration`
(#216); `TestDueOffersMintsAndDisqualifies`,
`TestDueSpotChecksAgeFromTheDeclaredInstant`,
`TestEvalLifecycleMintsDisqualifiesAndReTests` and
`TestSmallTeamEvalQualifiesAndDisqualifiesThroughTheProductionMachinery`
(#221). *III.G levels* (row 6): `TestLevelsAreOrderedAndTiered`,
`TestLevelClaimedMustEqualTheLevelSupported`,
`TestLevelShortOfTheTierRefuses`,
`TestLevelsAreReappliedAlongTheMergeChain`,
`TestSmallTeamCriticalContractReachesDoneAtL2` and
`TestFleetExecutableCriticalContractReachesDoneAtL3` (#233 over
#222's `TestTierTableMirrorsSpec`). *III.G calibration* (row 8):
`TestRubricVerdictsAtTheTerminal`, `TestHumanVerdictAndTheDeferral`,
`TestScorecardDerivationAtAdmission`,
`TestCalibrationQualifiesTheVerifierAndBindsItsRenders`,
`TestCalibrationOwesMintsDriftAndTheDefect` and
`TestSmallTeamRubricContractsReachDone` (#238). *III.O eval items*
(rows 1 and 2, and row 5's recorded half):
`TestEvalCheckProvesTheKnownVerdict` and
`TestCheckProvesTheKnownVerdict` (#221), the calibration drills above
(#238), `TestTrajectoryCorpusIsTheRecorderScenario`,
`TestTrajectoryCorpusReplaysGreenAndPlantedRowsDiverge` and
`TestReplayClassifiesEveryDivergence` (#239). *III.J row 3's metrics
half*: `TestReportLanesSection`,
`TestReportLanesRatesAreNullOverNothing`,
`TestRespecificationActivatesAtSeed4` and
`TestPlanDigestsThroughTheBoundary` (#239). Every numbered item has a
merged PR: 1 in #216, 2 in #221, 3 in #233 (over the tier
vocabulary's #222), 4 in #238, 5 in #239, and the out-of-item auto-gc
fix in #232.

III.E is walked in full rather than the two rows the exit line
scopes, the Phase 8 and 9 posture: every row is met by citation or
recorded UNMET and routed, never glossed and never a fraction. Row 1
(identity, credential, principal and runtime distinct; no claim
exceeds what signatures prove): met by #100, with #216 making
principal and runtime fields of the declared tuple, distinct from the
signing key. Row 2 (keypair actors, standing events, the keyring
projection, signatures on every proposal): met by #100 and #102. Row
3 (kind an operator assertion): met by #100. Row 4 (enrollment is an
identity plus a scoped credential; no inbound connectivity or
registration server): met by #100 — `actor.enrolled` carries the
public key and the operator's assertion and nothing else — with
#145's wakeless poll-only drill showing a worker needs no inbound
path. Row 5 (grants at admission; out-of-grant structural;
operator-only refusals; no self-approval by key disjointness): met by
#102, #135 and #137. Row 6 (qualification binds to the runtime tuple;
grants cite tuples; adapters report; drift is out of grant): met by
#216, with #221's mints feeding the same rule. Row 7 (scheduled
spot-checks re-test active tuples; failures suspend grants
attributably): met by #221 and, for verdict qualifications, #238;
"scheduled" is what a routine invoking `seed eval act` supplies, as
the maintenance pass's schedule is the deployment's. Row 8 (rotation
and revocation drilled: standing ends, **claims reaped**, attribution
preserved, sealed rotation for verifier keys): **UNMET, not claimed**
— standing and attribution by #104, sealed rotation by #139, and the
reap by nothing: the Phase 3 record routed it to Phase 5, Phase 5's
exit did not land it, and the maintenance reap (#205) needs an
interrupt answered by silence, which a revoked holder's `no_data`
stream never supplies. Routed to Phase 12 item 1 as its follow-up
card os-32d06c65: a reap corroborated by the revocation record alone,
the one case where the ledger rather than the observation channel
proves the holder can never exit its window. Row 9 (humans and
machines distinguished in the roster; agent-only guardrails and
human/agent metrics read it): **UNMET, not claimed** — distinguished
by #100, #117 and #119; read by nothing, the tier table's human-review
column (#238) routing to operator standing, not to kind; the Phase 3
record routed it to Phases 8 and 11 and neither consumed it. Routed
to Phase 12 item 4.

III.G is walked in full likewise. Rows 1 and 2 (the chain; divergence
induced and reconciled): met by #137, extended by #139 and #141. Row
3 (verdict keys disjoint from implementing keys; override its own
verb): met by #135 and #141. Row 4 (per-run isolation, no collisions,
cleanup pass or fail, enumerable self-executed inputs): met by #135.
Row 5 (receipts bind and recompute): met by #135 and #139. Row 6
(levels defined, declared per tier, enforced, recorded; L2 or L3 for
high-consequence tiers): met by #233 with #222. Row 7 (red lockout):
met by #141. Row 8 (rubric verdicts, the human deferral, calibration
with suspension on drift): met by #238, with its residual stated — no
calibration definition ships, because the gold set is held outside
the tree by design and committed to by digest. Row 9 (the protected
surface; the governance root and its change process named in config;
the capability audit in CI): **UNMET, not claimed** — the audit half
is met by #139, #102 and #234, and the config half by nothing: no
config under `next/` enumerates the protected surface or names the
root's change process; the root is named by genesis (#83) and
`next/**` is guarded today only by v1's guardrails as ordinary paths.
Routed to Phase 12 item 4 beside III.L row 2, which shares its
substance. Row 10 (evidence, receipts and verdicts queryable by
contract, actor, time and outcome): **UNMET, not claimed** —
contract, actor and outcome by #119's cache and the artifact store;
time by nothing, since no table carries the event's `ts` and position
is order, not a clock. A gap in the tree, so a card rather than a
line: os-74ce2261.

III.O and III.J row 3, walked. O.1 (eval contracts through the
production machinery gate qualification; spot-checks scheduled): met
by #221. O.2 (calibration with automatic suspension): met by #238.
O.3 (the compromised-actor drill in CI): Phase 12 item 1's, planned as
#241. O.4 (the standing drills in CI): met — projection rebuild #109,
checkpoint verification #205, packet-resume with dead-end assertions
#124 and #127, claim race storms #123, halt including the raw-git
bypass under enforced #84 and #99, key revocation with keyring
rotation #104 and #139, verdict/merge divergence #137, the hostile
classification corpus #80, budget reservation races #149, curator
poisoning #236, all under `make check`. O.5 (trajectory-prefix
regression; simulation mode credential-free end to end): **UNMET, not
claimed** — the recorded half by #239; simulation mode is Phase 12
item 6's by the build plan, and `trajectories.md` said "Phase 13's"
and "III.O row 3" (the drill's row) in three places each, corrected
by this record along with the two wrong III.G row numbers in
`verdicts.md` and `lanes.md` the walk found. J.3: **UNMET, not
claimed**, the metrics half met by #239 and the policy clause routed
to Phase 13 item 7 with the Phase 10 criterion revised to match, as
the opening says.

Two things this phase settled are worth carrying forward. A deferral
by name is only as good as its re-homing: item 2's `evals.md`
deferred tuple ranking "by name" and nothing in the phase picked it
up, so a deferral names the item that owns it or it names nobody. And
walking a pillar in full reaches rows the exit lines never mention:
III.C, III.L, III.M and III.Q are on no exit line in the build plan
at all, so this record puts them on Phase 12's, where `deps: all`
makes the walk possible. The routing this record makes in the build
plan's own text, the Phase 8 and 9 move: III.E row 8's reap arm to
Phase 12 item 1 (os-32d06c65); III.E row 9's consumer half and III.G
row 9's config half to Phase 12 item 4, with III.L on that exit line;
III.O row 5's simulation half to Phase 12 item 6; III.J row 3's policy
clause to a new Phase 13 item 7, with III.J row 3 on Phase 13's exit
line beside row 2; and the four unowned pillars to Phase 12's exit
line. Full III.E, III.G, III.O and III.J conformance is therefore
claimable only once Phase 12 and Phase 13 both close. This exit
record is card os-a026f5ea's task PR (an administrative card, not a
Phase 10 item).

## Phase 11 — Curation and the flywheel (docs/build-plan.md Phase 11; deps: 9 ✓)

- 11.1 staged curation stores (observations → hypotheses → validated
  lessons → policy) with grant-gated boundaries; workers append
  candidates only — os-f30ee0d3 — **done** (#234 against plan
  #226: `internal/curation` with the three facts, `curation.deadend.recorded`
  on the contract inside the holder's window (the packet finding's
  shape plus the charter's failure condition and environment),
  `curation.hypothesis.proposed` on `h-<12 hex>` derived from the
  claim, citing at least two admitted observations on two distinct
  non-failed contracts re-judged from the record, refusing a
  re-proposal as a duplicate, and `curation.lesson.promoted` on the
  hypothesis subject citing an admitted proposal, a promotion citing
  none folding `unbound`; the `curate` capability, the fifth
  no-fallback row, disjoint from `claim` and `operator` at the grant in
  both directions, a root included, held by the curator manifest and
  nothing else; the raise row's sixth capability; the `knowledge`
  projection and the report's `knowledge` section, present only when a
  curation fact stands; `seed knowledge deadend | propose | promote |
  show`; the lessons store `knowledge/lessons/` with its
  frontmatter contract and lint; the curator's reachable set derived
  from the boundary and pinned: the proposal, the raise and the
  standing-only relay; `spec/curation.md` and six spec edits).
- 11.2 promotion gate (≥2-trajectory support, applies-when,
  provenance, last-validated; adversarial evaluation; contested state;
  lessons at claim time) — os-96850e5a — **done** (#235 against
  plan #228: `applies_when` is a predicate over
  record-derivable fields (`routing`, which the fold now reads, `tier`,
  `paths`), one implementation for the boundary, the lint and the
  delivery; the support rule gains the actor arm, two distinct holders
  where the family the predicate selects has two, waived and recorded
  as `single_actor_family` where it has one; the hypothesis id derives
  from the claim and its exceptions, so an added exception is the road
  out of a contest; `curation.hypothesis.contested` from `curate`
  alone cites held-out observations on selected contracts outside the
  support set, moves the fold to `contested`, closes the promotion and
  removes the lesson from every delivery while keeping the file and
  the facts; `lesson.promoted` carries `carrier`, `adversarial`,
  `last_validated`, `expires` and `digest`, and its ledger half
  requires an uncontested admitted hypothesis whose support still
  passes and an authenticated pass on an eval filed after it with a
  marker bound to it and to this lesson anchor, for every carrier; the
  bound marker (`seed eval file --for-lesson --carrier`) and `seed
  verdict render`'s `carrier_absent`; `seed knowledge lint`, the
  gate's file half against the fact, the hypothesis and the
  repository; every refusal a `GateError` at a gate `curation.Gates()`
  registers; delivery from one derivation, verified against the
  repository (anchor ancestry, digest) on `claim take --repo`, in the
  provisioned `.seed-run/lessons.json` and in `seed situation --repo`,
  the unverified reported as such; `lesson_unverified` at evidence
  grade; the projection's `contested` stage and per-lesson
  `surfaces`).
- 11.3 poisoning drill — os-e2f1ad23 — **done** (#236 against plan
  #229: `internal/admit/testdata/poisoning/`
  declares forty-seven poisons over the thirty-two registered gates and
  five residuals; `poisoning_test.go` derives coverage from
  `curation.Gates()` pinned to the spec table both ways, scripts every
  poison to an attempt to promote and asserts it fails at both ends
  (the refusal at its gate, no admitted promotion, no lesson on a
  selected claim), pins every residual by a characterization drill,
  and refuses an empty corpus, an empty table, a declared poison with
  no script and a script with no declaration; the CLI arm runs
  `worker-proposes` and `smuggled-role-lesson` through `seed knowledge`
  in the small-team fixture; `curation.md` gains "The poisoning
  drill" and `lanes.md` records III.K row 4 as met)
- 11.4 expiry, retirement (evidence kept), rollback by revert; dead
  ends un-retired on environment change; staleness flags, dedup and
  structure lint (III.K rows 6, 7 and 9) — os-0d537fbd — **done**
  (#237 against plan #230, stacked on item 3's: expiry derived at a
  declared instant (`curation.Expired`, at-or-past) and never a fact;
  the fold keeps the latest admitted promotion per lesson path, keyed
  by path, and a re-promotion refuses at `promotion.revalidation`
  unless its `last_validated` moved forward (`LatestPromotionBefore`,
  one forward pass, never a refold); `curation.lesson.retired` from
  `observer` or `operator` with `regression` (the revert's `pr`),
  `superseded` (`superseded_by`, a later admitted promotion) or
  `expired` (neither), each field required by one reason and refused
  by the others, the cited promotion the latest of its path and
  unretired, the fold moving the path to `retired` while the file, the
  hypothesis and the observations stay, a new promotion clearing it;
  `curation.deadend.retired` and `unretired` from `curate` alone on an
  environment that differs from the one the previous act named, a
  retirement needing no standing one and an un-retirement needing one,
  applicability by string equality with the run's declared environment
  (`DeadEndFact.Applies`), the fold flagging and never deleting, the
  held-out listing excluding retired dead ends; the surfacing set's
  three new arms (latest per path, not retired, not expired at the
  read's instant: `claim take --now`, `seed situation --now`, the wall
  clock otherwise, admission reading no clock); the `knowledge`
  projection at the declared inputs' `as_of` (version 3, input
  consumption declared) flagging `stale` and listing the retirements,
  saying so when no instant is declared, the report at version 12
  counting both; `lint.structure` and `lint.duplicate` under `seed
  knowledge lint` and `make check`; the `lesson_stale` maintenance
  lint (`maintain run --stale-after`), the finding's subject
  `<path>@<promotion position>` so one stale cycle files once and a
  re-promotion that expires files new work; `seed knowledge retire`,
  `deadend retire | unretire`, `validate --environment`, `show --now`;
  eleven new gates, each with a poison in item 3's corpus; drilled at
  the boundary (every D1 to D3 row), in the fold, the projection, the
  reconcile and maintenance packages, at the terminal, and end to end
  in the modes fixture (the revert observed, the claim after carrying
  nothing, the revalidation surfacing again, the dead end retired and
  un-retired with the listing changing); `curation.md` gains "Expiry,
  retirement and applicability" and "Bloat" with III.K rows 6, 7 and 9
  in its mapping, `lanes.md` records them met, `protocol.md`,
  `actors.md`, `projections.md`, `maintenance.md`,
  `reconciliation.md` and `loop-verbs.md` follow, and the build plan
  names row 9 at Phase 11 item 4)
- 11.5 flywheel v0 — os-9075c308 — **done** (#240 against plan
  #231: `internal/flywheel` derives every done subject's shape from the
  record (the JCS form of routing, acceptance path, tier and verb
  sequence, `s-<12 hex>`), counts recurrence at `RecurringAfter` (2,
  pinned to the spec's sentence), drafts one deterministic v1 workflow
  per shape from the gated acceptance's own validation commands (one
  run step per command in order, the role steps for the sequence's
  judgment points, inputs exactly the varying fields, prompts over
  inputs and artifacts only; `ungated` and `divergent` refused; golden
  bytes committed), validates a draft through the v1 engine's
  `workflow validate` and `workflow run --mock` from a detached staging
  worktree that leaves nothing behind (the engine's refusal naming
  stage, step and finding; `name_taken` before staging; the drill
  skipping by name without the engine), folds `workflow.proposed`
  (`curate` alone, on the shape id, citing at least two distinct
  admitted done occurrences of the shape, a path directly under
  `.seed/workflows/`, no standing proposal, a passed repair cited and an
  open one refusing `repair_open`) and `workflow.merged` (`observer` or
  `operator`, citing the standing proposal's file) re-judged at their
  positions with the grant, and derives the report's `flywheel` section
  (version "14": recurring, proposed, merged, repairs filed and done,
  the rate over recurring); the repair contract (D7) filed under the
  dispatcher's key at `trivial` and `small` on the shape's routing with
  its acceptance under `next/flywheel/<shape>/` at the branch commit
  quoting the finding and carrying the engine's two commands, the
  proposal after its verdict validating the branch's file as it stands;
  `seed flywheel shapes | draft [--validate] | propose | repair |
  observe | status`, the proposal on `seed/flywheel-<shape>` and never
  on main; drilled in the package, at the boundary (every gate, the raw
  push, the curator's reach joined into the residual drill), in the
  keyring, the projection, the terminal, and end to end in the
  small-team fixture (the chore worked three times converts; the
  planted harness break is repaired on the branch, the verifier's
  render runs the two commands green, the proposal cites the contract
  and one merge closes both); `spec/flywheel.md` new, with
  `protocol.md`, `projections.md`, `lanes.md`, `curation.md`,
  `actors.md` and `envelope.md` following)

**Phase 11 exit (charter III.K as docs/build-plan.md's exit line
scopes it): met.** Met means the two criteria the plan's own exit line
names, the scoped-exit posture of the Phase 2 through 10 records, each
backed by a drill on `main` that a reader can find by name. *The
poisoning drill green*: `TestPoisonCorpusCoversEveryRegisteredGate`,
`TestEveryPoisonFailsAtBothEnds` and `TestPoisonResidualsArePinned`
(`internal/admit/poisoning_test.go`), the corpus derived from
`curation.Gates()` and pinned against the spec table both ways, every
poison scripted to a promotion attempt and asserted to fail at both
ends, five residuals pinned by characterization, plus the CLI arm
`TestPoisonsRefuseAtTheTerminal` (`cmd/seed/modes_e2e_test.go`) running
`worker-proposes` and `smuggled-role-lesson` through `seed knowledge` in
the small-team fixture. *A real recurring chore in the fixture converts
to a workflow through the gates*:
`TestSmallTeamChoreWorkedThreeTimesConverts`
(`cmd/seed/flywheel_e2e_test.go`), the chore worked three times, its
shape recurring at the second occurrence, the draft validated by the v1
engine's `workflow validate` and `workflow run --mock` from a staging
worktree, proposed on `seed/flywheel-<shape>` and never on `main`,
observed merged, the rate `1.000` in the report's `flywheel` section;
and with a break planted in one step, the mock run failing, the repair
contract filed under the dispatcher's key, the implementer's fix passing
its verdict, the proposal admitting citing it, and one merge closing
both. Every numbered item has a merged PR: 1 in #234, 2 in #235, 3 in
#236, 4 in #237, 5 in #240. Charter III.K as a whole is walked rather
than the two criteria the exit line scopes, and every row is met. Row 1
(online lanes append evidence only; conclusion-writing grant-gated to
the curator's proposal path): met by #234, `curation.deadend.recorded`
inside the holder's window and `curation.hypothesis.proposed` from
`curate` alone, disjoint from `claim` and `operator` at the grant in
both directions (`TestCuratorReadsAndCannotWrite`). Row 2 (the pipeline
staged with distinct storage and gates; no stage skips): met by #234's
three facts and the lessons store with #235's promotion requiring an
admitted, uncontested hypothesis whose support still passes, a
promotion citing nothing folding `unbound`
(`TestFoldRendersStagesAndCountsAnomalies`,
`TestKnowledgeVerbsDriveTheStages`). Row 3 (applies-when; support from
more than one non-failed trajectory and more than one actor where the
family allows; provenance; last-validated; adversarial evaluation for
behavior-changing lessons): met by #235, `applies_when` a predicate over
record-derivable fields, the support rule's actor arm with
`single_actor_family` recorded where waived, `carrier`, `adversarial`,
`last_validated` and `digest` on the fact, and the eval marker bound to
the lesson anchor (`TestAppliesWhenIsAPredicateOverRecordFields`,
`TestShapesRefuseAtRegisteredGates`). Row 4 (trajectories untrusted;
the poisoning drill fails to achieve promotion in CI): met by #236, the
exit line's first criterion, with #237 adding a poison per new gate so
the corpus stays derived. Row 5 (conflicting evidence a first-class
contested state, never averaged; contested lessons do not surface): met
by #235, `curation.hypothesis.contested` citing held-out observations,
the fold's `contested` stage, removal from every delivery with the file
and the facts kept (`TestSmallTeamPromotionDeliversLessonsAtClaimTime`).
Row 6 (expiry for revalidation; retirement keeping evidence; rollback by
reverting the PR): met by #237, expiry derived at a declared instant and
never a fact, `curation.lesson.retired` with `regression` carrying the
revert's `pr`, `superseded` and `expired`, the fold keeping file,
hypothesis and observations (`TestExpiryIsDerivedAtAnInstant`,
`TestRetirementShapesRefuseAtTheirGates`). Row 7 (dead ends carry
failure condition and environment, un-retired on environment change):
met by #234's dead-end shape and #237's `curation.deadend.retired` and
`unretired` on a changed environment, applicability by string equality
with the run's declared environment. Row 8 (the flywheel closes through
gates; repair roles propose patches as PRs; conversion rate tracked):
met by #240, the exit line's second criterion, with
`TestRawChainsManufactureNoChore` and
`TestRawRepairVerdictLeavesTheRepairOpen`
(`internal/flywheel/flywheel_test.go`) pinning that only
boundary-authentic completions and repair verdicts count. Row 9
(knowledge bloat managed: dedup with provenance, staleness flags,
structure lint): met by #237, `lint.duplicate` and `lint.structure`
under `seed knowledge lint` and `make check`, `stale` at the
projection's declared instant, the `lesson_stale` maintenance finding
(`TestStructureAndDedupLints`), the row routed to Phase 11 item 4 by the
build plan. The Phase 8 record's routing closes here: III.I row 5
(matching promoted lessons surface in packets and envelopes at claim
time) is met by #235, the surfacing set on `claim take --repo`, in the
provisioned `.seed-run/lessons.json` and in `seed situation --repo`,
verified against the repository and reported unverified where it is
not, so full III.I conformance now waits on Phase 13 item 6 alone. One
thing this phase learned twice is worth one sentence: item 1's curation
fold and item 5's flywheel fold each found that the lifecycle fold is
tolerant by design, so a folded `done` reads what the table permits and
never what the boundary admitted, and every consumer that counts
completions — the support rule, the promotion gate, the reconciler, the
flywheel's occurrences and its repairs — re-judges authenticity at the
record's own position through one derivation, `curation.AuthenticPass`
(`memory/LEARNINGS.md`). This exit record is card os-efb2a099's task PR
(an administrative card, not a Phase 11 item).

## Phase 13 — Conformance completion (docs/build-plan.md Phase 13; deps: 12)

- 13.1 racing mode as the per-squad opt-in with first-verified
  settlement (III.F row 7) — os-56bee171 — **done** (#269, merged,
  against plan #256, stacked on Phase 12 item 5, a draft until the
  Phase 12 exit record merges: the `racing` block on a squad's
  guardrails (`racers` two or more, `cost` in the operator's words,
  refused otherwise, absent is exclusivity); at `seed/6` a further
  `claim.taken` admits on an `in_progress` racing contract below the
  cap for a claimant holding none, its fence its own position,
  `contention` naming every holder at the cap; the fold's plural
  `Claims`, `Submissions` and `Verdicts` beside the singular facts;
  claim-scoped exits (every racer's exit but the first submission and
  the last departure moves no state, the table untouched); verdicts
  binding to the submission they cite with the lockout per submission;
  the merge chain citing the newest verdict on its submission;
  settlement at `merge.observed` with the other claims settled-out
  (`race_settled` on their next act, their own exit still admitted, the
  maintenance pass reaping the silent ones with a packet naming the
  settlement); the contracts view's `racing` object at version 14 and
  the claim response's racing note; drilled at the boundary (the
  opt-in, the cap, the fences, the exits, the settlement, the reap's
  admission), in the maintenance pass and at the declaration;
  `spec/lifecycle.md` "Racing" new, `postures.md`, `envelope.md`,
  `maintenance.md`, `budgets.md`, `projections.md` and `protocol.md`
  (`seed/6`) following)
- routed gap: the cache carries the event's `ts`, so evidence is
  queryable by time (charter III.G row 10, recorded UNMET at the Phase
  10 exit) — os-74ce2261 — **done** (#266, merged, against plan #260:
  `ts` verbatim and `ts_unix` parsed on every per-event table at cache
  generation 14, ranges over the integer since RFC 3339 mixes
  fractional precision, an unparseable `ts` NULL and counted, the
  time-range drill with the four names of the row in one query)
- routed gap: `ledger show`'s `chain_invalid` stamps the position it
  was computed at — os-37fcf7c6 — **done** (#265, merged, against plan
  #259: the scan's failing position stamped, the same stamp verify
  gives one corrupted chain, the D3 tripwire inverted on purpose, the
  envelope spec's null sentence sharpened to "before any position was
  read")
- 13.2 the remaining executor adapters — container, cloud session,
  enrolled remote worker (III.H) — os-083112ac — **draft in progress**
  (draft PR #282 against plan #274 per decisions/0003: executor.Described
  (optional; enforced|risk-limit), Container over an OCIRuntime with
  executor/fakeoci (credential-free) and OCICommand (docker/podman),
  CloudSession and RemoteWorker (risk-limit; the worker pulls, no
  inbound), the executors declaration block with the secret lint, seed
  run start --adapter wiring, the doctor's adapters list, spec/executors.md.
  Still to land: report.json's adapters section, the per-adapter
  disposability/preemption drills in cmd/seed, docs receipt. Awaits #274
  merge + a make-check window)
- 13.3 a non-primary forge adapter (Forgejo) for the forge extras (III.N
  row 2) — os-ad610334 — **done** (#281, merged, against plan
  #275 per decisions/0003: internal/protections/forgejo.go over Forgejo's
  Gitea-compatible branch/tag-protection API, held to the one Desired
  table; the Observer filling merge.observed's sha from either forge;
  admission.forge/api posture fields; seed protections/merge observe
  --forge forgejo; doctor's declared forge; spec/forges.md; drilled
  against a fake Forgejo. Reconciliation: the pull-request rule is
  Unexpressible on Forgejo — reported manual, not half-applied. Awaits
  #275 merge + a make-check window for the receipt)
- 13.4 mirrors and dashboards propose, federation as uniform read
  remotes, cross-repo work as a proposal (III.J row 2, III.N row 4,
  §II.15) — os-48df10a2 — **done** (#270, merged, against plan #257,
  stacked on 13.1: `request.filed`, the one door a surface's proposal
  enters by, strict `{origin, kind, reference, summary ≤ 200 bytes,
  about?}` on the contract `about` names or on `system`, a fact that
  changes no state and needs standing only; `request.answered`, the
  dispatcher's close citing the intent it filed or the reason it
  declined, once per request; both at `seed/7` (the plan said
  `seed/6`, which racing took first); the fold's request facts, the
  `request` rule, `request_refused` (exit 3); `request.pending` owed
  to `lane:dispatch` with its age; the situation read's `requests`
  notices (origin, kind, size, position, age, never the summary) and
  the report's `requests` section (present only when a request
  exists, so every existing build is byte-identical); the affordance
  catalog's two probes and the walk's seed/7 station; the dispatcher
  manifest's `request answer` as a lane act beside the loop acts, the
  corpus re-recorded on purpose; the injection corpus's request arm
  at the boundary and the terminal (III.J row 2 met in full, the lanes
  spec saying so); the declaration's `federation` block and `seed
  federation report` (fetch, verify under each remote's own keyring,
  fold, `federation.json` byte-identical on the same tips, a tampered
  remote reported and not folded, no key, nothing appended); the
  cross-repo drill (a source contract proposed into a target by an
  ingress key with standing only, answered with a citing intent,
  read back through the source's read remote, the source's tip never
  moving); `spec/requests.md` new, `protocol.md` (`seed/7`),
  `lanes.md`, `obligations.md`, `projections.md`, `postures.md`,
  `envelope.md`, `actors.md` following)
- 13.5 the A2A-shaped cross-organization boundary (III.N) —
  os-40ed0ca0 — **done** (#271, merged, against plan #258, stacked on
  13.4: `internal/boundary` — the capability card rendered from the
  declaration's new `boundary` block (the kinds accepted and the
  ingress), the squads and tiers by name, the artifact kinds, signed
  by the operator key over JCS-canonical bytes and verified against
  it, strict on parse, refusing an unsigned card or one naming an
  internal; the five-state task lifecycle derived from the target
  chain (`requested`, `declined`, `accepted`, `working`, `done`) with
  the pinned fields alone; the `boundary` projection (`tasks.json`,
  `tasks/<request>.json`, version 1, empty on chains without a
  cross-repo request); `artifact.PutVerified`; the `boundary`
  admission rule holding `request.filed` to the card's kinds; `seed
  boundary card | check | serve | tasks | fetch` — the read-only
  surface of exactly four routes, the card and the states
  credential-free, artifacts optionally behind a bearer, everything
  else `not_found`; the checked-in `boundary/card.json` for the
  fixture deployment, re-rendered and diffed by `make check`
  (`card_drift`); the two-organization drill with distinct roots
  (card verified, the request through the ingress, the states as the
  target's lanes drive the contract, the receipt fetched by digest
  and cited in the source's chain, opacity swept route by route) and
  the refusals; `spec/boundary.md` new, `envelope.md`,
  `projections.md`, `postures.md`, `protocol.md` following)

## Phase 13 — Conformance completion (docs/build-plan.md Phase 13; deps: 12)

- 13.6 the machine-protocol surface and platform parity (III.I rows
  3–4) — os-b55e5647 — **done** (#273, merged, against plan #261:
  `cmd/seed/registry`, the one table both surfaces are drawn from, and
  `catalog.go` registering every verb with its subverbs in the usage
  line's own words, the CLI's `run` dispatching through it alone;
  `seed serve`, JSON-RPC 2.0 over stdio framed as `machine-envelope/0`,
  a method per verb, params as argv or as flags, the CLI's own run
  function invoked and its envelope returned verbatim (a refusal a
  result, never a transport error), `serve --list` printing the method
  set, the transport the one named carve-out; `internal/platform` with
  the honest posture table per OS and the doctor's `platform` report;
  the LF-only ledger enforced (the segment scanner keeps a carriage
  return and the record parser refuses it); the drills — the registry
  against the usage vocabulary both ways, set equality of the
  surfaces, byte-identical envelopes through both over a corpus, the
  path lint at the call site, the skip-reason lint, the CRLF refusal,
  the doctor's report; the CI matrix on Linux, macOS and Windows;
  `spec/platform.md` new, `protocol.md` and `envelope.md`
  following)
- 13.7 tuple ranking as supervisor policy: eval results rank
  qualified tuples and the planner lane's offers carry the strongest
  (III.J row 3's policy clause, §II.9) — os-c7554f18 — **done**
  (#286, merged, against plan #276: `internal/ranking`, the record-derived
  policy table (score by qualifying evidence since the tuple last
  held, ties by the latest pass then the canonical JSON, disqualified
  and holder-less tuples absent, agreement refining the verdict
  ranking only with the gold supplied), pinned to `spec/ranking.md`
  by a drill that parses the table; `seed offer publish --strongest n
  --capability c` filling the `tuples` scope from the ranking at the
  offer's own instant and refusing `ranking_empty` (exit 4) rather
  than widening; `eval.Due`'s offers by policy (the configuration
  under re-test, else the strongest claim tuple, else unscoped with a
  `ranking_empty` note, the bootstrap); the `ranking` projection
  (version 1, both capabilities, byte-identical), `seed doctor
  --ledger` naming the top tuple per capability, the report's
  `lanes.planner.strongest` at version 16; drilled at the derivation,
  the verbs and the projection, with the mutation evidence D4 names;
  `ranking.md` new, `offers.md`, `qualification.md` (the deferral
  closed), `evals.md`, `projections.md` and `envelope.md` following)
- the promotion evidence packet (build plan §5: the seven self-hosting
  criteria mapped to evidence on `main`, the shadow run and the two
  cutovers named as the reserved decisions they are; feeds the Phase
  13 exit record) — os-98ce6f8a — **done** (#294, merged, against plan
  #291: `docs/promotion.md`, one section per criterion with a
  status from a closed vocabulary (`met`, `partial`, `not started`,
  `reserved`) and its evidence as drill, file and PR rows; criteria 1,
  2, 3, 5, 6 and 7 met (6 by the conformance report, #289, merged),
  4 reserved on one question, the POSIX git server that hosts the
  shadow ledger with the hook, the run itself at the enforced
  self-hosted posture the criteria name; `internal/promotion` parsing the packet
  strictly and holding every citation to the tree under `make check`
  (`TestPacketCitesRealDrills`, a planted bogus citation failing by
  name); criterion 5 met by writing the cutover and the rollback in
  the build plan's three clauses; the shadow-run protocol as a
  proposal (the declaration, the slice, the seven-day window, the
  dual-run rule, the daily divergence diff, the five-bar audit at the
  end); the III.R measurement ledger, seven rows, every one `not
  measured`; the divergence log, empty until the window opens; the
  handbook pointing at the packet)
- the conformance report (Phase 13's exit line: "the conformance report
  shows Part III complete at the enforced self-hosted posture"; the
  preamble's "the doctor reports which Phase 13 criteria remain open")
  — os-83bc3d84 — **done** (#289, merged, against plan #287:
  `spec/conformance.json`, the charter's 128 Part III rows
  verbatim with the status the exit records gave each (`met` with
  evidence, `partial` or `routed` with a note, `open`), the posture
  read off the enforced-only marker; `internal/conformance` parsing
  Part III out of `SEED-NEXT.md` and holding the table to it row for
  row in both directions, the vocabulary validated; `seed docs
  generate` rendering `docs/generated/conformance.md` under the
  drift gate; `seed doctor --repo` reporting the counts, every row not
  yet met by pillar, row and status, the enforced-only rows set aside
  at the cooperative posture and the mixed rows named there, and
  `complete` only when every row is met at an enforced posture; Phase
  13's rows `open` until its exit record flips them, III.R's routed
  to promotion's measures; `spec/conformance.md` new, the
  handbook following)
- 13.8 projection integration boundaries: mirror exporter conformance,
  no bidirectional component, and governed external-fact observations
  (III.D rows 5–7) — os-b45c308d — **done** (#361, merged, against plan
  #350: `mirror`, the projection-only issue mirror with its
  base64 marker, deterministic planner, sealed registry of GitHub,
  Forgejo and snapshot exporters, and `seed-mirror plan|apply` over
  the build `seed project current` resolved, a separate executable
  with no ledger, key or remote; one conformance
  suite iterating the registry plus the per-exporter
  export/edit/request/restore drill joined to item 4's ingress;
  `internal/authoritylint` deriving the export and coordination-write
  classes from each executable's import closure, self-checked against
  planted overlaps, and green over the tree; `internal/externalfact`,
  the closed external-fact catalog pinned both ways to
  `spec/external-facts.md` and the keyring, the forge readers
  moved out of the mutating protections package behind a read-only
  `Source` with the observation-control lint, the fake-forge
  governed-observation drill recording every request, and the
  admission invariance drill pinning that an observation narrows and
  never widens; D.5, D.6 and D.7 met; the plan's D5 second
  `check.observed` shape superseded by #351's landed verb, recorded in
  decisions)

## Phase 12 — Hardening, distribution, migration (docs/build-plan.md Phase 12; deps: all)

- 12.1 the compromised-actor drill and the release gate — os-465e356e
  — **merged** (#250 against plan #241: `internal/redteam` with the
  ceiling and residual tables asserted both ways, the hook's code-ref
  half, the protected surface restricted to the governance root, `make
  check` as the release gate; its follow-up os-32d06c65, revocation
  reaping the revoked holder's open claims, merged in #267; the two gap
  cards the phase filed, `ledger show` stamping the position its
  `chain_invalid` was computed at (os-37fcf7c6, #265) and the cache
  carrying the event's `ts` (os-74ce2261, #266), merged)
- 12.2 forge-hosted admission service (stateless, sole-writer) and the
  protections desired-state reconciler — os-5c8a312c — **merged** (#252 against plan #244: `seed-admit serve` as the service form of
  the one judgment — a proposal's records appended onto the
  materialized tip, the candidate committed in the service's own git
  dir, judged with the hook's `admitUpdate` (its typed refusals now
  surviving the wrap), pushed fast-forward under the service's identity,
  a stale `prev` answered as the race it is (409) before anything is
  appended, genesis admitted onto an empty branch; the proposal
  protocol speaking the envelope with the status as transport;
  `gitref.Proposer` and the `Commit`/`Push` split, the loop proposing
  instead of pushing in its last step only; `internal/refusal` as the
  one mapping from typed refusals to envelope codes, shared by the CLI
  and the service; `internal/propose` as the HTTP client; the
  `admission` block on the declaration (required under
  enforced-forge-hosted, refused elsewhere, the ledger ref a branch
  because forges protect branches and tags), `Protected` and the
  surface helpers in #241's shape, `Parse`; the remote verbs reading
  the declaration (`--config`, `$SEED_CONFIG`, `./seed.json`) and
  proposing under the third posture; `internal/protections` deriving
  the four rulesets, CODEOWNERS and the CI-identity lint from the
  declaration, `Diff` by name with manual rules for what a forge cannot
  express, the `snapshot` and `github` (REST rulesets) adapters; `seed
  protections plan | apply` with exit 28 `drift` refined
  `protections_drift`; the doctor's report, `--probe` and `--current`,
  the gap sentence retired; drilled at the service (admit, refuse with
  the boundary's code, the forge refusing the actor, the race and the
  loop's retry, the strict proposal, one derivation across service,
  boundary and hook, kill-and-replace, the entry point), at the terminal
  (the remote verbs under each posture, protections plan and apply, the
  doctor's probe and drift) and in the packages; `spec/postures.md`
  new, `envelope.md` and `modes.md` following)

- 12.3 checkpoint trust docs with the replay-equals-genesis CI proof;
  performance budgets tracked in CI; III.C row 4's contention benchmark
  — os-7508ab9e — **merged** (#253 against plan #246, stacked on
  item 2: `checkpoints.trust` on the declaration, `replay` or `signers`,
  an absent block undeclared and `seed project start` refusing
  `trust_undeclared` rather than choosing; `ledger.WithTrustedPrefix`
  replaying a trusted prefix without its signature checks and holding
  the attested tip at the trusted position; `checkpoint.Latest` with the
  capable-signer rule (maintenance, operator or a root at the
  checkpoint's own position); `project.StartFromCheckpoint` cross-
  checking the snapshot's files against its own derivation before
  publishing, then publishing builds byte-identical to a genesis
  replay's with `basis.json` beside them, a rebuild clearing it; the
  proof with teeth (a corrupted prefix signature caught by rebuild and
  not by start, a corrupted suffix by both) and the lying-checkpoint
  refusals; `internal/history`, the seeded representative chain
  admissible by construction (landed with item 2, whose hook drill it
  found a gap for); `internal/perfgate` and `cmd/perfgate` measuring
  admission latency, replay, rebuild and the storm's wall time and
  attempts ratio against `perf/budgets.json` with provenance,
  re-measuring cold once, under `make check-next` after coverage; `seed
  perf run`; the doctor's trust report; `spec/checkpoints.md` new,
  `maintenance.md`, `projections.md` and `envelope.md` following)

- 12.4 preseed (config, guardrails, teams, protections, posture)
  idempotent and CI-verified; agent-only guardrails and human/agent
  metrics reading `kind` (III.E row 9); the protected surface and the
  root's change process in config (III.G row 9, III.L row 2); tiers per
  squad and per path (III.L row 1) — os-0d4f2af3 — **merged** (#254 against plan #247, stacked on items 2 and 3: `seed.json` completed
  with `protocol`, `governance`, `guardrails` and `teams`, strict, each
  block undeclared when absent; `seed init --preseed` writing genesis
  and the declared activations idempotently and refusing
  `preseed_drift` on a root or protocol the chain contradicts, the
  chain untouched; `seed preseed check` linting the file alone
  (`preseed_incomplete` for a tier outside the vocabulary, a lane that
  is no manifest, an undeclared squad, a protected surface missing a
  required member) and comparing against a ledger; the agent claim
  ceiling and the routing rule as declaration-driven admission policy
  (`tier_above_ceiling`, `routing_unknown`; a human key not ceilinged;
  no declaration, no change), the declaration wired into every
  admission context the CLI builds; the path floor at the plan lint
  and the render (`under_tiered`, one derivation); `by_kind` lane rates
  (report 15, cache generation 13); the capability audit deriving the
  operator-holding manifests from the shipped set; the fixture
  deployment's declaration under `make check-next`; `postures.md`
  "The preseed", `tiers.md`, `lanes.md`, `plans.md`, `projections.md`
  and `envelope.md` following)

- 12.5 migration from open-seed, drilled against a real export of this
  repository — os-cf13fb51 — **merged** (#255 against plan #248,
  stacked on item 4: `seed import --from-open-seed <export> --source
  <clone> --ledger --artifacts --key [--anchor] [--repo]`, the second
  of the two commands; anchors first (`unanchored`, `export_mismatch`
  under exit 29 `import_refused`, both before any write); the genesis
  import (`ledger_not_empty`; genesis, the upgrades to `seed/5`,
  `system.imported` citing the manifest, the enrollments, the replayed
  history with every record admitted through `admit.Check` at the
  position it holds, the suspensions); one generated key per v1 actor
  name with grants derived from the run-log before replay, never
  operator, the importing operator signing what only an operator may
  and `import-verifier` rendering the verdict a claimant could not; the
  transform as `spec/import-open-seed.json` embedded byte for
  byte, drops as rows, `import_unmapped` for the rest; packets from the
  handoff's mechanical sections; done cards through the pass verdict
  over the stored receipt or an import note (D7's override path
  declined: it overrules a fail nobody rendered); the mapping manifest
  with one disposition per export record and exact positions from a
  two-pass replay; `ledger.AppendAll`, the one-pass batch write; the
  fixture `fixtures/import/open-seed/` (this repository's state at
  `seed-anchor/20260903T014125Z`, 251 files, 1214 run-log entries,
  imported as 1345 records in ~33 s) with `make fixture-import`; the
  drills (the real fixture folding every contract to its card's state,
  the synthetic predecessor, the four refusals, the CLI's envelopes, the
  spec mirror); `spec/import.md` new, `protocol.md` (`seed/5`),
  `envelope.md` (exit 29) and `actors.md` following)
- 12.6 docs generation (lifecycle/capabilities/exit-codes/per-lane from
  the tables, drift-checked), the operator handbook, and simulation mode
  — os-16e55c11 — **merged** (#272 against plan #249:
  `admit.CatalogVerbs()`, `internal/docs` + `seed docs generate/check`
  (`docs_drift` refining exit 28) + `make check-next`; `internal/decider`
  + `trajectory.ChoiceDiverged` + `trajectory replay --decider scripted`;
  `executor.Mock`, `internal/simulate` + `seed simulate [--days]
  [--posture]` driving every lane to done credential-free under both
  postures with the five-bar ledger audit; `spec/simulation.md`,
  the handbook, the committed `docs/generated/`. Reconciliations in
  decisions.md: the reference decider is partial because the frame does
  not determine the loop's per-iteration act; `claim take` is remote-only
  so both postures run on a bare remote)
**Phase 12 exit (charter III.B, III.O and III.P as
docs/build-plan.md's exit line scopes it, with III.C, III.L, III.M
and III.Q walked as the Phase 10 record routed them): met.** Met means
the three pillars the plan's own exit line names, the scoped-exit
posture of the Phase 2 through 11 records, each row backed by a drill
on `main` that a reader can find by name; the four pillars the Phase 10
record put on this line because no other exit line owned them are
walked the same way, and the rows they leave PARTIAL or UNMET are
routed by name, never glossed. Every numbered item has a merged PR: 1
in #250 (with the reap arm #267 it named), 2 in #252, 3 in #253, 4 in
#254, 5 in #255, 6 in #272, and the two gap cards the phase found (#265,
the `chain_invalid` position on `ledger show`; #266, the cache carrying
the event's `ts`) are merged. *III.B, the service posture.* Row 1 (the
stateless validator as the only writer): met by Phase 2's hook and
#252's service running one rule set, `TestServiceAgreesWithTheBoundaryAndTheHook`
and `TestServiceAdmitsRefusesAndTheForgeRefusesTheActor`
(`cmd/seed-admit/serve_test.go`) beside the hook's
`TestDrillRawAdversaryPerPosture` (`cmd/seed-admit/drill_test.go`).
Row 2 (no unique durable state; kill-and-replace): met by
`TestDrillKillAndReplace` and `TestServiceKillAndReplace`, the hook
and the service each rebuilt from a clone and judging the same
proposal the same way. Row 3 (three postures declared, the
cooperative posture's consequence stated, no default): met by
`spec/postures.md` and `posture.Load` refusing `ErrUndeclared`
where no posture is declared. Row 4 (actor credentials cannot write
the ledger ref): met by #250's code-ref arm
(`TestCodeRefProtectedSurface`, `TestCodeRefNoLedgerAndNoDeclaration`,
`cmd/seed-admit/coderef_test.go`) and #252's derived sole-writer
ruleset (`TestDesiredDerivesFromTheDeclaration`,
`internal/protections/protections_test.go`). Row 5 (the
compromised-actor drill in CI): met by #250, the exit line's first
criterion, `TestCeilingHoldsAtThePush`, `TestOneDerivationLedgerAgrees`,
`TestCoverageBothWays`, `TestResidualsArePinned` and
`TestTablesValidate` (`internal/redteam/redteam_test.go`), the ceiling
and the residuals pinned as tables and the code-ref rules proven
load-bearing by `TestCodeRefRulesAreLoadBearing`. Row 6 (sharding,
MAY): not claimed; the backlog names sharded intake as a true extra.
*III.O, the drill in CI.* Rows 1 and 2 were met at Phase 10 (#221,
#238) and are unchanged. Row 3 (the compromised-actor drill on every
release): met by #250 with the release gate being `make check` itself
(`plans/os-465e356e.md` D8, `spec/redteam.md`), which CI runs on
every push to `main`, every pull request and every merge group, so no
release exists that the drill has not gated. Row 4 (the standing
drills in CI): met, each drill a test under `make check` (`covergate`
runs every package with no short mode): projection rebuild
(`TestRebuildByteIdenticalAndStamped`, `internal/project`), checkpoint
verification (`TestMaintainCheckpointIsStartableByAFreshReader`,
`cmd/seed/maintain_cli_test.go`, and #253's cross-checked start),
packet-resume with dead-end assertions (`TestPacketResumeDrill`,
`internal/packet/resume_test.go`), claim race storms
(`TestClaimRaceStorm`, `internal/gitref`, and the perf gate's storm),
halt including the raw-git bypass (`TestHaltRefusesEverythingButLift`,
`internal/halt`; `TestDrillRawAdversaryPerPosture`), key revocation
with keyring rotation (`TestDrillKeyRotation`,
`TestDrillCompromisedKeyCutPerPosture`,
`cmd/seed-admit/rotation_test.go`), verdict/merge divergence
(`TestSubjectClassifiesInducedDivergences`, `internal/reconcile`), the
data-classification hostile corpus (`TestHostileCorpusRefuses`,
`internal/classify`; `TestNoHostileTextWidensTheDispatcherSet`,
`internal/admit/injection_test.go`), budget reservation races
(`TestReservationRaceAndStatus`, `cmd/seed/budget_cli_test.go`) and
curator poisoning (`TestEveryPoisonFailsAtBothEnds`,
`internal/admit/poisoning_test.go`). Row 5 (trajectory-prefix
regression and simulation mode): met by #239's recorded half and
#272's simulation, `TestSimulateReachesDoneCooperative`,
`TestSimulateReachesDoneEnforced` and `TestSimulateAcceleratedBacklog`
(`cmd/seed/simulate_cli_test.go`) driving every lane to done
credential-free under both postures with the ledger audit
(`TestAuditCatchesSilentAbandonment`, `TestAuditCatchesUnofferedClaim`,
`internal/simulate/audit_test.go`). *III.P, distribution and
migration.* Row 1 (tagged releases, three-way template upgrades with
rollback, the pinned checksum-verified engine never committed,
air-gap paths, hash-pinning throughout): partial. Every clause is met
by the distribution Seed ships inside — `scripts/seed` fetching the
engine pinned in `.seed/engine.lock` and verifying its checksum, `seed
engine upgrade` with rollback and the protocol preflight, `seed
template upgrade`'s three-way merge from recorded provenance, the
vendored engine for air-gapped machines, and `checksums.txt` with
provenance on every release — except the first, clone-and-init adoption
from tagged releases: Seed's own binary is built from source and no
seed/v* release has been cut, which is §5 step 2's cutover (what new
users clone) and belongs to promotion. The conformance table records
the row as partial accordingly (os-53650015). Row
2 (hook and service, both stateless and rebuildable): met, the two
kill-and-replace drills above. Row 3 (the preseed in one idempotent,
CI-verified file): met by #254, `TestInitPreseedIsIdempotentAndDriftRefuses`
(`cmd/seed/preseed_cli_test.go`) and `seed preseed check` on the
fixture deployment under `make check-next`. Row 4 (the predecessor
import, two commands, drilled against a real fixture): met by #255,
`TestRealFixtureImports` (`internal/importer/fixture_test.go`) folding
this repository's own export to its cards' states,
`TestAnchorRefusalsPrecedeEveryWrite`, `TestNonEmptyLedgerRefuses` and
`TestUnmappedVerbRefuses` (`internal/importer/drills_test.go`), and
`TestImportCommandEnvelopes` (`cmd/seed/import_cli_test.go`). Row 5
(install is one command, no telemetry, no account): met; `scripts/seed`
bootstraps and `seed init --preseed` seeds a deployment from one file,
and the tree makes no network call outside git and the forge: exactly
three non-test files under `next/` import `net/http`, the admission
service, the proposal client and the GitHub protections adapter.
*III.C, walked.* Rows 1, 2, 3 and 5 are met: Phase 4's observation
channels classified and lossy by declaration
(`spec/observations.md`), liveness by monotonic counts and
material transitions only (`TestMilestoneSummarizationBoundary`,
`TestWedgeDeclaredOperatorFact`, `internal/admit/observation_test.go`;
`TestLivenessRidesTheWorkAndIsKeyedToActorAndFence`,
`internal/loop/loop_test.go`), expiry and wedging distinct and drilled
(`TestMaintainRefusesAWedgeCitingTheWrongFence`,
`TestMaintainReapsOnAWedgeCitingTheActiveFence`,
`cmd/seed/maintain_cli_test.go`). Row 4 (the contention benchmark at
target scale, tracked in CI): PARTIAL. #253's perf gate measures the
storm's wall time and attempts ratio against `perf/budgets.json`
with provenance, re-measuring cold once (`TestRunReMeasuresColdOnce`,
`internal/perfgate`), and the storm runs 24 writers through the hook
on every `make check-next`; hundreds of concurrent actors are not
demonstrated per PR, because a storm at that scale costs minutes a
run. Routed: a scheduled run at the target scale, in the build plan's
backlog, walked again at Phase 13's exit. *III.L, walked.* Row 1
(tiers per squad and per path): met by #254,
`TestAgentCeilingReadsTheRosterKind`,
`TestRoutingIsHeldToTheDeclaredSquads` and
`TestUnknownCeilingFailsClosed` (`internal/admit/policy_test.go`),
`TestPlanLintHoldsTheScopeToThePathFloors` and
`TestVerdictRenderHoldsTheReceiptToThePathFloors` (`cmd/seed`). Row 2
(the protected surface in config, root-only, write-denied to every
agent key, audited in CI, the test-content residual documented): met
by #250's `TestCodeRefProtectedSurfaceIsRootOnly` and #254's
`governance` block with `TestCapabilityAuditOfTheShippedManifests`
(`cmd/seed/audit_test.go`), the residual named in `postures.md`. Row 3
(data/instruction defense layered, documented with limits, the corpus
on every release): met, the injection suite of Phase 9 and the
classification corpus both under the release gate. Row 4 (per-verb
policy governing the machine-protocol surface with attributable
approvals): UNMET, and routed unchanged to Phase 13 item 6
(os-b55e5647), where the machine-protocol surface lands; the Phase 13
record says whether that item meets the row or it stays routed. Row 5
(forge protections declared and reconciled, admission-only ledger
writes, immutable tags, least-privilege CI identities): met by #252,
`TestPlanAndApplyThroughTheSnapshot`,
`TestLintWorkflowsFindsScheduledWriters` and
`TestGitHubAdapterReconciles` (`internal/protections`), with
`TestCodeRefTagsAreImmutable` at the hook. Row 6 (process changes pass
boundary and retention, lint-checked): met by Phase 5's plan lint
(`TestPlanGateAboveTrivialTier`, `internal/admit/plan_test.go`;
`TestPlanLintTierAndScopeAreHeldToTheFloors`). *III.M, walked.* Row 1
(the DAG engine): met by the v1 engine Seed's flywheel drafts into
(#240), `scripts/seed workflow validate|run` with its typed edges,
gates, waves, budgets and the mock run, the drafted definitions
landing as PRs against a protected registry; two residuals named:
vault-indirect secrets are not implemented (the handbook resolves no
secret at run time), and which engine new users clone is §5 step 2's
decision. *III.Q, walked.* Row 1: met, `make check` green on `main` at
every merge. Row 2: met, `covergate` at a stated 90% floor over every
package, the v1 smoke targets under the same command, the backend
conformance suites and the forge adapter's reconciliation drill. Row
3 (docs governed): met by #272, the operator handbook and the
generated worker docs rendered from the tables admission reads
(`TestGeneratedContentIsFromTheTables`, `TestCheckCatchesHandEdit`,
`TestGeneratedDocsAreCommitted`, `internal/docs/docs_test.go`),
drift-gated under `make check-next`. Row 4: met, Appendix C,
`docs/decisions.md` and `design-options.md`. Row 5: met, the
authority order in `docs/CONTRIBUTING-AGENTS.md`, the decisions under
`decisions/`, the build plan's rubric. Row 6 (fork-friendly
governance): met, the MIT license, no contributor license agreement,
no open-core split, stated here as the repository's standing. Row 7
(self-hosting total once bootstrapped): PARTIAL; Seed coordinates
this repository's `next:` cards through v1's queue today, and total
self-hosting is §5 step 1's shadow run and the promotion criteria,
where it is routed, not claimed. The Phase 9 record's routing of III.J row 2 (the dispatcher with least standing capability, passing the injection conformance suite: embedded instructions in intents, mirrors and tool output quoted as data, never obeyed) to Phase 13 item 4 closes here, met: the dispatcher holds the dispatch grant alone, the suite on `main` pins that hostile text in intents, projections and tool output is carried verbatim and never widens the dispatcher's reachable set (`TestNoHostileTextWidensTheDispatcherSet`, `TestDispatcherReachableSetIsNamed`, `internal/admit/injection_test.go`; `TestHostileCorpusRefuses`, `internal/classify`; `TestProjectionsCarryPayloadsVerbatimIncludingHostileText`, `cmd/seed/injection_cli_test.go`), and #270 extended the corpus to the cross-repository ingress vector, `TestNoHostileRequestWidensTheDispatcherSet` (`internal/admit/injection_request_test.go`) proving a hostile `request.filed` from another repository is quoted as data and grants nothing; the same PR meets III.N row 4 (federation projection-only: uniform read remotes, request-event ingress, no cross-ledger write path, proven by its absence), which Phase 13's record walks in full. The Phase 8 record's routing of III.I row 3 (the CLI as the complete interface; the machine-protocol surface with identical semantics; platform parity including Windows, documented and tested) to Phase 13 item 6 stays open, the row PARTIAL: the first clause holds and is reinforced by #272, `seed docs generate` rendering the lifecycle, capability, exit-code and per-lane worker documents from the sources the machinery reads (the transition table, `admit.CatalogVerbs()` over the affordance catalog the boundary drafts from, the envelope package's exit constants, `lane.Resolve`), so the documented interface cannot drift from the enforced one, `seed docs check` failing `docs_drift` under `make check-next` (`TestGeneratedDocsAreCommitted`, `TestCheckCatchesHandEdit`, `internal/docs`; `TestDocsCheckCleanOnCommitted`, `TestDocsGenerateThenCheck`, `cmd/seed`); the second and third clauses, `seed serve` with identical semantics and the tested Linux/macOS/Windows matrix, are Phase 13 item 6's (os-b55e5647, #273, in review at this record's writing) to close.
The Phase 10 record's routing of III.C, III.L, III.M and III.Q to
this line closes here except the three rows named above, which carry
their own routings. One thing this phase learned is worth one
sentence: a stacked task PR costs a merge-forward and a fresh receipt
at every parent's merge, and a child merged into a stale base never
reaches `main`, so the rule is to retarget or re-land the moment a
parent merges (`memory/LEARNINGS.md`). This exit record is card
os-9e9ae30c's task PR (an administrative card, not a Phase 12 item),
written by the two implementing sessions, one voice.


## Defects the scale benchmark found (os-a00d3f34's storms)

- at 200 writers, one storm pushed a chain whose new record cited a
  stale tip (`bad_prev` at the hook, classified as policy and not
  retried), once in three runs, the one across midnight UTC —
  os-5063e8ba — **done** (#300 against plan #299: the seventh
  race shape, a hook's `bad_prev` at or beyond the position the
  client appended, re-linked from a fresh fetch and counted as
  `Result.Relinked`; the rejected commit's own tree beside the work
  directory it was built from and the hook's message, kept under the
  client's state dir (`refused/<commit>/`) before the retry; `Client.WithClock` as the test seam for a segment day;
  `seed perf run --keep` and `cmd/perfgate -keep` keeping a storm's
  work dir, `relinked` in the reading beside the five budgeted
  metrics; drilled with the planted refusal (re-links, lands, keeps
  the tree, counts one), a `bad_prev` below the append surfacing
  unchanged, the storm through the CLI with a planted refusal, and
  two writers across an injected midnight, which land every append
  and re-link zero times: the drill does not reproduce the storm's
  tree, so the cause stays open and the next occurrence is a
  directory to read; `checkpoints.md` "The seventh race shape")

## Shadow-run tooling (the promotion evidence packet's protocol, os-98ce6f8a)

- `seed ledger audit`, the five-bar audit over any ledger, the shadow
  run's evidence for charter III.R row 5 — os-7599c27d — **done** (#296
  against plan #295: verify from genesis first, an
  invalid chain refusing `chain_invalid` before any bar is read; the
  unchanged `simulate.Audit` over the verified records; one envelope
  with the five bars verbatim, stamped at the tip, `clean` on a clean
  chain and `audit_violated` refining exit 28 on a violated one, the
  message naming each bar with its records; `audit` in the registry
  and the usage message, so `seed serve` carries `ledger.audit`;
  drilled through the CLI with each plantable violation, the
  corrupted and the genesis-less chain refusing before any bar;
  `envelope.md`'s row and `simulation.md`'s sentence; the packet's
  clause naming the verb follows once #294 is on `main`)

## Backlog — true extras (docs/build-plan.md §3; filed when Phase 13's items were on `main`)

- the contention benchmark at target scale (III.C row 4, routed here
  by the Phase 12 exit record; the one conformance-relevant extra) —
  os-a00d3f34 — **done** (#297 against plan #293:
  `perf/budgets-scale.json`, 200 writers against the same
  40-contract history, each writer its own enrolled actor (the
  history enrolls a key per writer, the storm signs with it, the
  measurer holds the chain to as many actors as writers), the timing
  ceilings measured with those identities on the
  implementing host and the attempts ceiling at twice the loop's
  expected writers/2, provenance on every ceiling; the weekly
  read-only `perf-scale` workflow running the unchanged `cmd/perfgate`
  at that profile and attaching the reading pass or fail;
  `TestScaleProfileIsHundredsOfWriters` pinning the profile and
  `TestTreeWorkflowsHaveNoScheduledWriters` holding the tree's
  scheduled workflows to `contents: read` outside v1's maintenance
  lane; `checkpoints.md` "The scale profile"; the C.4 row stays
  `partial`: the run demonstrates the scale without lost updates,
  tracked in CI, and the row's "without unrelated writes racing each
  other's admissions pathologically" is what sharded intake
  (os-7953612b) would demonstrate, so the exit record cites the run
  for what it shows and flips nothing on it; the run is red on a lost
  append until os-5063e8ba's re-link lands, which is the carded
  defect, not a regression)
- the forge says the submission is not mergeable (charter §II.4's
  `check.observed`, §II.13's checks gate on the contract loop, III.D
  row 7) — os-0cd18799 — **done** (#351, merged, against plan #344:
  `check.observed` at `seed/8`, the observer's fact on the head under
  review bound to the submission's packet head and admitted only when
  it changes; `submission.made --pr`; the `submission.unmergeable`
  obligation owed by the dispatch lane with the head on the row;
  `contract.returned` citing the latest red observation with no
  lockout, `merge.requested` refusing while the forge says red;
  `Observer.Checks` over GitHub, Forgejo (thread count nil) and the
  snapshot; `seed check observe`; the maintenance pass's observe and
  return steps with the return ceiling escalating on the fourth red
  return; `spec/observations-forge.md`; the implementer fragment's
  red-return paragraph; the walk's `seed8`, `review-c6`, `observed-c6`
  and `returned-c6` stations; III.D row 7 met by construction and left
  for the exit record to flip; the ranking follow-up D8 names filed as
  os-29e2fef2)
- prefer the prior submitter's tuple when re-offering a contract
  returned on the forge's word (ranking policy, Phase 13 item 7;
  charter II.9, II.11), os-29e2fef2, **review** (task PR #363
  merged, card not yet closed; against plan #358: `ranking.Resume` deriving the returned window's declared
  tuple, holder and consumed offer from the chain, refusing by name
  after a verdict return, with no return, no admitted start, no
  declared tuple, a suspended or revoked holder, or a tuple the
  holder's admissible claim grant no longer cites; `seed offer publish
  --resume` beside `--strongest` and `--tuple`, `resume_empty` on exit
  4; the maintenance pass's re-offer step after the return, one per
  return, scoped to the prior tuple with the consumed offer's
  capabilities and tiers, its expiry and its record's `ts` from one
  instant through `Deps.Instant` and `Deps.AppendAt`, `--reoffer-ttl`;
  `seed/9` restoring the runtime-tuple semantics that seed/5 through
  seed/8 lost by omission, which the resumption on the forge loop's
  chain needs; the "Resume" section of `spec/ranking.md`, the
  re-offer sections of `observations-forge.md` and `maintenance.md`,
  the maintenance fragment's clause)
- sharded admission intake (III.B row 6, MAY) — os-7953612b —
  **backlog** (filed; a true extra whose absence conforms: the row is
  a permission, and os-9ef9ab34 marks it met by abstention, since the
  charter's clause binds a system that shards and this one does not.
  Until that card, the row sat `open` on the grounds that nobody had
  claimed it, which made `seed doctor`'s `complete: true` unreachable
  and would have blocked the Phase 13 exit record and promotion with
  it)
- dashboard tiers beyond the report — os-f17567a6 — **backlog**
  (filed; not conformance-blocking)
- the flywheel engine drill's skip path racing TempDir cleanup on
  macOS — os-222189a3 — **done** (#302 against plan #301; tests
  only)
- at 200 writers, one storm pushed a chain whose new record cited a
  stale tip (`bad_prev` at the hook, classified as policy and not
  retried), once in three runs, the one that crossed midnight UTC —
  os-5063e8ba — **done** (#300 against plan #299, merged;
  filed from the scale benchmark's second measurement; P1: a lost append is what III.C row 4 forbids; the
  drill first, then the fix, and the loop re-linking on a `bad_prev`
  whose cited tip is not the tip it fetched)

- a random-walk property test over admission (III.I over random
  positions, the §I.2 ceiling as oracles; the 2026-09-06
  formal-methods survey's first card, docs/build-plan.md §3) —
  os-21bf939f — **done** (#353 against plan #349, merged; card
  closed:
  `TestAdmissionRandomWalk` in `internal/admit`, a seeded walker
  from the shared scenario with four oracles a step and the five-bar
  audit plus a ceiling coverage map at the end of every walk; found
  the sweep's re-draft copy missing the escalation anchors, fixed in
  the copy; eight walks of twenty-four steps under `make check-next`,
  two hundred of ninety-six on the weekly `perf-scale` job)
- an exhaustive interleaving check of the append loop and halt (the
  survey's second card) — os-07e6e76c — **done** (#359 against plan
  #356, merged; card closed: `TestAppendInterleavings` and
  `TestAppendInterleavingReplay` in `internal/gitref`, a
  Go-native model of fetch, attempt and the cooperative rollback walked
  over every interleaving for small N with five properties, and its
  traces replayed through real clients over `Fetch` and `attempt`;
  acceptance by ancestry per the plan review; four configurations and
  eight replayed traces under `make check-next`, four by four and every
  trace on the weekly `perf-scale` job)
- the same explorer over the verdict-then-merge reconciliation and over
  racing mode (the follow-on plans/os-07e6e76c.md D7 names, filable
  once that explorer's shape was known) — os-873b5153 — **review**
  (#370 against plan #366; charter §II.8 and §II.6 racing, no Part III
  row: the walker extracted into `internal/explore` with the
  append-loop model moved onto it unchanged, a reconciliation model in
  `internal/reconcile` whose alphabet includes an operator
  override and a raw-push adversary, and a racing model in
  `internal/maintain`; both enumerate over real records through
  the real admission, so the replay is the model. Two findings, each
  from a step the walk could not take: the settlement was unreachable
  on a racing subject, the fence rule demanding a rival racer's fence
  from two payloads with no slot to carry one, fixed here with one
  exemption and the trace kept as a named regression; and a settlement
  rests on the position its request cited, not on the subject's last
  verdict. depth 4 and race-depth 7 under `make check-next`, depth 6
  and race-depth 9 on the weekly `perf-scale` job)

## A fixture that escaped the hardening guard (os-222189a3)

- the flywheel engine drill's skip path races `t.TempDir`'s cleanup on
  macOS (`unlinkat .../.git/objects: directory not empty`), reporting
  a skipped drill as a failed one on #290's macOS leg — os-222189a3 —
  **done** (#302 against plan #301: `requireEngine` reads the
  pin from the source tree, the same shim and lock `instantiate`
  copies, and runs as the drill's first statement, so a skip builds
  nothing: twelve git spawns on the skip path before, none after;
  `fixtureRepo` hardened right after `init`, written into the
  repository's config; the guard's alternation gains `gitIn(`, the
  helper name that had kept the fixture outside os-c4e8b57a's
  property while the guard passed; tests only)

## A bar that counted a verb the protocol does not emit (os-b86dab4c)

- the five-bar audit's unreserved-spend bar counted `budget.reserved`
  against a protocol that emits `budget.reserve` — os-b86dab4c —
  **done** (#306 against plan #304: the bars name the
  protocol's constants, the deliberate exits come from
  `transition.IsExit` instead of the audit's own copy, and
  `AuditedVerbs` is held to `admit.CatalogVerbs()` by a drill, which is
  the guard for the class; the fixtures and the CLI drill's histories
  move to the protocol's verb and a two-arm regression covers both
  sides. Found by review on #296 after it merged, so the fix is its own
  card rather than an amendment to a finished one; the first draft of
  the plan named `Table.Verbs()` as the authority and implementing it
  showed that is the lifecycle subset, carrying neither
  `budget.reserve` nor `run.started`)

## The coverage floor the receipt gate keeps drawing from (os-f262585a)

- `seed receipt verify --run` takes its own cold `make check`, and cold
  collection swings a few tenths, so at a 0.1 margin above the 90 gate a
  receipt green when written could verify red as a `receipt mismatch`
  — os-f262585a — **done** (#314 against plan #292: tests only,
  no gate, ceiling or behavior change. Measuring first showed the plan's
  premise had already moved: the tree read 90.99 cold, not the ~90.1 the
  plan describes, because os-ad610334 and the cards after it landed. The
  work still went in, for the margin the plan asks for and because the
  packages it names were genuinely thin: `internal/protections` 85.9 to
  91.3 (the GitHub adapter's bypass-identity grammar, its rule
  translation and Apply's refusals; Plan and Apply's CODEOWNERS and
  forge-refusal arms), `internal/artifact` 62.1 to 80.3 (every refusal
  the content-addressed store makes, and the failed-rename path),
  `internal/posture` 92.8 to 99.0 (the declaration's accessors and their
  undeclared answers, and the preseed validator's refusals),
  `internal/perfgate`'s gate to 100 (the cache it cannot clean and the
  second measurement that fails), `internal/history` 69.4 to 76.5, and
  `internal/imported` 70 to 100, which shipped with no drill at all
  while `-coverpkg` counted its statements.
  Ranking off each package's own tests would have spent the effort in
  the wrong places, since `cmd/seed`'s drills alone cover 72% of the
  internal tree; the targets came from the gate's own merged profile.
  Three readings on the shipped tree, all 91.5: covergate warm, and two
  explicitly cold `-p 1` collections with the cache cleaned before each,
  which is the plan's acceptance criterion and something covergate
  cannot supply, since its re-collection engages only below the gate)

## The audit's budget model (os-88df7ab2)

- the unreserved-spend bar counted a reservation's occurrence, not an
  open valid one — os-88df7ab2 — **done** (#311 against plan
  #309 with amendment #310: the bar asks `admit.RunStartValid` per
  folded start, so a run is fenced to the reservation it cited and not
  to whichever one happens to be open, and a start the fold could not
  place is unfenced by construction; the drills' synthetic `{}` chains
  gave way to an admission-grade chain from `internal/history`,
  because a bar that judges admission can only be exercised by chains
  admission would take; the cost is measured rather than asserted, 410
  records in about 130 ms against the plan's ten-second ceiling)

## The audit's guardrail bar (os-aaec6a3c)

- the bar required an offer the boundary does not — os-aaec6a3c —
  **done** (#313 against plan #312: admission's claim arms are
  authoring isolation and the lifecycle transition and none reads the
  subject's offers, so an unoffered claim is no breach and
  `internal/history`'s admission-grade chains stop tripping the bar;
  the bar is corrected rather than emptied, since that was its only
  source, and it now counts the guardrail admission does enforce, a
  claim by the key that sealed the subject's checks, which the fold
  makes chain-visible; found while implementing os-88df7ab2, #311)
- the bar carries neither clause `plans/os-16e55c11.md` D5 contracted
  for it: no ceiling arm, no blind-retry arm — os-b5051f2e —
  **done** (#323, merged, against plan #322: filed from review on #312,
  which read the offer rule's removal as losing ceiling coverage. The
  bar never had any: the ceiling is admission policy, read from
  `seed.json` through `Context.Declaration`, so a claim above it folds
  as filed and the chain verifies byte for byte, while `simulate.Audit`
  takes records alone. The card did both halves honestly:
  `simulate.AuditUnder(records, declaration)` gives the guardrail bar a
  ceiling arm that mirrors admission's rule arm for arm, the claiming
  key's kind from the keyring replayed to the claim's position, the
  tier and squad from the fold, the ceiling from the declaration,
  failing closed on a ceiling outside the vocabulary and silent for a
  human key, an undeclared squad or no declaration, the evidence naming
  subject, kind, key, tier, position, squad and ceiling; `seed ledger
  audit --config` reads the declaration by the remote verbs' shared
  lookup and names it in the reading; `Audit(records)` is unchanged
  for every caller; the simulation declares a ceiling of its own and
  audits under it, its reading saying `declared: true` (review on
  #322). The blind-retry clause is stated as unmet: a refused append
  never lands and the refusal journal keeps no attempt digest, so no
  surface measures it today, which `simulation.md`'s bar 4 now says,
  citing os-a9e715dc for the measurement. Drilled in both packages; an
  admission-grade 40-contract chain audits clean under a declaration
  in about 180 ms)

## The machine surface's policy, drilled (os-8ecef90f)

- III.L row 4, per-verb policy on the machine-protocol surface with
  attributable approvals — os-8ecef90f — **done** (#321, merged, against
  plan #320: tests only. The row was routed because #273's three
  `serve` tests are parity assertions, which is III.I row 3, and the
  card's determination was that the row is structurally true and needs
  a drill. `TestServeRefusesByTheSamePolicyAsTheCLI` fires a grant-table
  refusal, a routing refusal and an agent-ceiling refusal through
  `serve` and holds each to the CLI's code for the same argv, with each
  admitted twin landing through the same surface;
  `TestServeApprovalsAreAttributableToTheirSigner` reads
  `decision.recorded` and `plan.approved` back through `serve`'s
  `ledger.show` with the signing key's fingerprint as `actor` and the
  chain verifying afterward. Review on #320 (chatgpt-codex-connector)
  read charter II.14 back: per-verb policy is allow, deny and
  require-approval, and the tree has no require-approval mode, so the
  row moves to `partial` rather than met, its note naming the missing
  third and os-5781a026, the card that builds it and flips the row;
  `platform.md`'s conformance section names the drills; no non-test
  line moves)
## The erasure verb: erasure is an attributable event (os-db5cd353)

- III.A row 7, erasure obligations honorable and the erasure itself an
  attributable event — os-db5cd353 — **done** (#325, merged, against
  plan #324: the row's first clause was structural and undrilled, its
  second absent by construction, since the protocol defined no erasure
  verb and a deleted ciphertext left only a `seal_evidence_missing`
  finding naming neither who nor when. `artifact.erased` is a fact,
  additive catalog growth active from `seed/1` (the `offer.*` and
  `curation.*` precedent, no version bump), strict `{artifact,
  reason}`, on the contract whose fold references the digest (its
  sealed commitment or a verdict's receipt) or on `system`, refused
  when unreferenced and when already erased, `operator` only; the fold
  keeps it (`Fold.Erasures`, `Fold.Erasure`); `seed seal audit` lists an
  erased ciphertext under `erased` with position, signer and reason and
  stays clean while a deletion with no record stays
  `seal_evidence_missing`; `verdict render` names the erasure in its
  `seal_broken`; `seed artifact erase` records first, then empties the
  store's buckets under the digest (`artifact.Store.Erase`) and reports
  which, finishing rather than re-recording on a re-run; the affordance
  catalog drafts the verb exactly where the subject references
  something. A review round narrowed every consumer of the fact to
  records that passed the boundary (`admit.ErasureValid`, the keyring
  replayed at the record's position), held the resume path to the
  operator grant, and made a removal the store refuses
  `erasure_incomplete` rather than a success. Drilled in
  `internal/erasure`, `internal/artifact`, `internal/admit` and at the
  terminal; the row flips to met with the drills as evidence, the
  doctor's outstanding rows falling by one; `protocol.md` gains the
  `artifact.*` bullet and an "Erasure" section, `sealed-checks.md`,
  `actors.md` and `envelope.md` follow)
## The Seed release workflow (os-2e46aa2f)

- the distribution step's precondition, a released Seed binary with
  checksums and provenance (charter III.P row 1's residual) —
  os-2e46aa2f — **done** (#329, merged, against plan #328:
  `.github/workflows/seed-release.yml`, dispatch-only so a release stays
  the operator's act and the CI-identity lint's scheduled-writer rule
  is untouched; the tag `seed/v<version>` minted at HEAD in-runner, in a
  namespace apart from the template's `v*` and the `seed-anchor/*`
  anchors; `seed` and `seed-admit` built from `next/` for six targets
  with the version stamped into `internal/version` (now a var);
  archives, `checksums.txt`, a GitHub Release and
  `actions/attest-build-provenance`, every third-party action pinned to
  a commit SHA; the reconciler's `seed-release-tags` ruleset gains
  `refs/tags/seed/v*` so the namespace is immutable on the forge as it
  already is under the hook (review on #328); held by
  `TestSeedReleaseWorkflowIsDispatchOnly` beside the scheduled-writer
  drill and `TestVersionIsStampableAndPreReleaseFromSource`; the
  handbook's Install section names the release and how to verify one.
  Review on #329: the tag is pushed only after the archives and
  checksums exist and a same-commit re-run resumes the cut, the release
  is a draft until the attestation exists, the version is validated by
  semver.org's grammar, the job runs in the `seed-release` environment
  from the default branch alone (the deployment branch policy is the
  operator precondition the handbook names), and the Forgejo adapter
  compares every tag protection's whitelist, held by
  `TestForgejoComparesEveryTagWhitelist`. No release is cut: III.P row
  1's residual closes when the operator cuts the first at the
  distribution step)
## Require-approval per verb, III.L row 4's third mode (os-5781a026)

Review on #320 read charter §II.14 back against the tree: per-verb
policy on the machine-protocol surface is "allow/deny/require-approval
by actor and risk class; approvals as request events resolved
attributably in an operator inbox", and the tree had allow and deny
(the grant table; the declaration's ceiling and routing rules, drilled
on `serve` by os-8ecef90f) and no require-approval, so os-8ecef90f
moved the row to `partial` and routed the third mode to this card.
Plan #330; task PR the one this section lands in. What landed:

- **The declaration names the verbs.** `guardrails.approvals` is a
  list of `{verb, actors?, kinds?, min_tier?}` entries
  (`posture.ApprovalRule`, `Config.ApprovalRule(verb)`): an entry
  reaches the fingerprints and the roster kinds it names, every
  non-human kind when it names neither, so the policy varies by the
  individual keypair and by risk class; the shape is held at load, the
  vocabularies (catalog verbs, fingerprint shape, roster kinds, tiers)
  at `seed preseed check` (`preseed_incomplete`). A governance root's
  unasserted kind is reached by no kind selector; the three approval
  verbs are never governed; a floor outside the vocabulary fails closed.
- **Three fact verbs, additive from seed/1.** `approval.requested`
  (standing only, the actor named is the key that will act),
  `approval.granted` and `approval.denied` (operator only, no
  fallback), their shapes in `internal/approval`; the fold keeps
  `ApprovalFact`s and spends a grant at the first record on its subject
  naming its verb and actor, so one approval admits one act
  (`OpenApproval`, `PendingApprovals`, `ApprovalAt`).
- **Two rules.** `approval` in the core set holds the shapes and the
  citations regardless of declaration (`approval_refused`, exit 3);
  `require-approval` in the policy set refuses a governed act with no
  valid open grant (`approval_required`, exit 3) naming the request to
  file and, once one stands, the grant that answers it. Valid is
  `ApprovalValid`, the laundering countermeasure: the request and the
  answer re-parsed at their positions, the keyring replayed to the
  answer's position to hold the answerer to operator standing, so a
  raw-pushed grant signed without it folds as a fact and admits
  nothing.
- **The inbox and the loop.** `approval.pending` owed by
  `lane:operator`, one row per subject carrying the oldest open
  request; `seed approval request | grant | deny` in the shared
  transport shape (the actor defaults to the requesting key, the
  request to the oldest open one, several a choice); the group joins
  the registry so `serve` carries the three; the affordances draft the
  three where they are legal and list a governed act for its actor
  exactly while an open grant stands.
- **The row.** III.L row 4 reads `met` on all three modes, evidence
  naming the machine-surface drills here and os-8ecef90f's, note naming
  the modes; `conformance.md` regenerated; specs: `protocol.md`
  ("Per-verb approval" and the catalog line), `postures.md` ("The
  approvals block"; the conformance mapping), `actors.md` (the two
  rows), `envelope.md` (the two codes), `obligations.md` (the kind),
  `platform.md` (the conformance line).

The plan PR's review (chatgpt-codex-connector, three findings) shaped
D1, D4 and D9 before the plan merged: the actor selector, the
position-accurate grant check, and the reading that the transport's
non-ledger methods carry no actor and stay ungoverned; `decisions.md`
records each. One deviation from the plan, recorded there too: D5 said one
inbox row per open request, and the projection's identity is (subject,
kind) by the situation read's delta, so the row is one per subject
carrying the oldest open request, the `request.pending` shape.
## The refusal journal tells a blind retry from a corrected one (os-a9e715dc)

`plans/os-16e55c11.md` D5 words the five-bar audit's guardrail bar as
"no refusal followed by a blind retry; every claim within its
ceiling". The ceiling clause is os-b5051f2e's. Review on #322 found
the blind-retry clause measurable by no surface: a refused append
never lands, and the attempts journal kept no digest of the attempted
payload, so an unchanged retry and a corrected one wrote
indistinguishable lines. Plan #332. What landed:

- **The act digest.** `refusals.AttemptDigest` is the SHA-256 of the
  canonical `{actor, verb, subject, payload}`, excluding `ts`, `prev`
  and the version (the boundary's coordinates, which a retry changes by
  construction); the entry gains `digest`, written at every seam
  (`journalAttempt` takes the payload; the eight seams in `loop.go`,
  `ledger.go`, `offer.go` and `seal.go` pass it, a derivation refusal
  with no payload journals without one). A journal from before the
  field loads; a malformed digest refuses naming the line.
- **The count.** The report's `refusals` section gains
  `blind_retries`, by the definition `modes.md` gives the loop drills
  (a refusal followed by the same actor's next attempt on the same
  subject with the same digest, refused with the same code from the
  same position, once per refusal; an admitted next attempt is
  convergence, a refusal with another code or from an advanced
  position a new answer), `blind_retries_by_code` (by the code, so the
  optimistic loop's `contention` spins read apart from a `fenced_out`
  act re-sent unchanged) and `undigested`; report version "18" (the
  register was at "17", which the plan's review corrected).
- **The description.** `simulation.md`'s guardrail bar names the count
  as the clause's evidence, says the audit keeps reading records alone,
  and that the accelerated simulation journals nothing (remote posture)
  so the clause is read on local deployments' journals.

## The enforced hook reads the declaration at the default branch's tip (os-0f924157)

The review finding on #323 (os-b5051f2e's task PR): the enforced
self-hosted posture's server-side boundary never exercised its
guardrails — `seed-admit`'s ledger half built every admission context
with `admit.ContextOver(records[:i])` and no declaration, and the
installed pre-receive script passes no environment but `SEED_ADMIT_REF`
and `SEED_PUSHER`, so the ceiling and the other declaration-driven
policy rules were no-ops exactly at the posture that exists to enforce
them. Plan #334 (the plan PR's review added the serve fetch, the
corpus ceiling clause, and the one-derivation option). What landed:

- **The shared read.** `readDeclarationAt` (one owner, the same three
git invocations the code half used): the HEAD symref, the default
branch's tip, `<tip>:seed.json` via `git show`. `admitUpdate` reads it
once per push — the pre-push tip, the code half's own rule for mixed
pushes — and passes `admit.WithDeclaration` into `admit.ContextOver`
for every context it builds. A file present but unparseable fails
closed: the ledger half refuses the push (D2), the code half's own
message standing in for both, so one broken declaration cannot split
the boundary into two. No file, or an unborn default branch, is no
declaration: genesis and a fresh remote behave as before.
- **The serve fetch.** `judge` resolves the remote's HEAD symref and
fetches the default branch into the service's private mirror before
`admitUpdate`: the mirror's HEAD is an unborn symref, so a plain fetch
brings only the guarded ref and the judge would admit what the hook
refuses. Unborn means an empty deployment: the hook as before.
- **The drills, at the enforced boundary.** `cmd/seed-admit`: the raw
above-ceiling claim refused at the hook with the ceiling's message, ref
unmoved; the SAME claim through the cooperative seam under `--config`
self-refusing at the client (the postures differ only in WHERE the
rule runs); the at-ceiling control admitted at the hook; a broken
declaration refusing the ledger push and the code push alike, an
operator's repair re-opening both. `redteam`: the fixture's declaration
gains the core squad's trivial ceiling, the fixture files one ready
contract at `standard` (above it), and the corpus carries the
adversary's raw claim on it beside the contention primary; the
one-derivation invariant now hands the in-process side the same
`*posture.Config` the hook reads, so it holds under a live guardrail.
`simulate`: the deployment commits its declaration on the default
branch beside writing it beside the remote (the `--config` half), and
the end-to-end drill runs the raw above-ceiling claim at the installed
hook — the #323 reproduction inverting.
- **The spec.** `simulation.md`'s hedge ("the hook builds its contexts
without a declaration today … is card os-0f924157's") is tightened to
the affirmative: the hook, the `--config` client, and the audit each
read the declaration, and `declared` states what each of them read.
`postures.md`'s hook paragraph names the parse-failure refusal. The
ceiling rule itself is unchanged.

## Knowledge search: an advisory lexical read (os-405d3b20)

Not a plan item: a backlog card filed from a review of okf-agent-memory
(github.com/okf-memory/okf-agent-memory) against Phase 11. The
pipeline already exceeded the format on every axis the format cares
about; its one idea Seed lacked was retrieval. What landed
(`docs/decisions.md` "Knowledge search"):

- **`curation.Index`**: BM25 in `internal/curation/search.go` (k1
  1.2, b 0.75, the Lucene idf, lowercase alphanumeric tokens of two or
  more runes, no stemming, no stop list), ties on kind then id, a hit
  carrying rank, score, kind, id, title, the first body line a term
  matched and the matched terms.
- **The corpus**: `LessonDocuments` (the subject-less surfacing set at
  the instant, verified against the repository, read at the anchor,
  frontmatter stripped; the unresolved reported as delivery reports
  them), `DeadEndDocuments` (the fold's standing dead ends) and
  `SectionDocuments` (a named markdown file by level-one and
  level-two heading, `<path>#<slug>` ids).
- **`seed knowledge search (--ledger | --remote) [--repo] [--now]
  [--limit] [--doc]... <query>...`**: the envelope echoes the query,
  its terms and the instant, counts the corpus by kind, ranks the
  hits, carries `lessons_unresolved` and says `advisory`. Never a
  delivery path: the predicate stays the only claim-time surface.
- **Drills**: the scorer's ranking properties (tf saturation, idf,
  length normalization, ties, limit), the section split, the standing
  dead ends, the verified surfacing set against a real repository
  (working-tree edits invisible, unmerged anchors unresolved, expired
  and retired and contested out), the CLI's usage refusals, and the
  small-team end-to-end: the promoted lesson found by its words with
  `--repo`, unresolved without, gone after the contest.
- **Spec**: `curation.md` "Search" records the source, the adoption
  and the standing rejection of the OKF format (III.Q row 4).

## Trace-shaped test evidence in the receipt (os-7fc2ca38)

Not a plan item: the build plan §3 "Borrowed from practice
(2026-09-06)" card (#343; plan #347), from a test harness outside
this tree that attaches a trace to every run. A receipt transcript
bound one output digest; a span tree that names the failing span and
its outcome attributes collapsed into it. What landed
(`docs/decisions.md` "Trace-shaped evidence"):

- **The contract is one variable.** The `exec` profile sets
  `SEED_TRACE_EXPORT` per command (`verdict.Runner.RunTraced`); a
  harness writes an OTLP/JSON export there; Seed reads the documented
  span fields with `encoding/json` (`internal/traceshape`) and imports
  no OpenTelemetry package.
- **Only the shape reproduces.** Name, kind, status code, the keys a
  `## Trace attributes` section of the spec declares
  (`plan.TraceAttributes`), children sorted by canonical bytes; ids,
  times, durations, events, links, resource, scope, the status message
  and undeclared attributes stripped. The receipt gains `traces` and
  `sealed_traces`, omitted when nothing was written, so every earlier
  receipt's bytes are unchanged; a malformed export is
  `{transcript, malformed: true}`.
- **Shape as artifact, raw export as sidecar.** The shape under its
  digest, the raw export under its own with a pointer at
  `traces/<shape>` (`artifact.PutTraceRaw`, `TraceRaw`, an `Erase`
  bucket); no raw digest in the receipt or on the ledger, so no
  protocol bump. `verdict check` verifies the shapes and never the
  sidecar; `reconcile.TracesAt` grades a lost shape
  `evidence_missing`.
- **Citation by path.** `trace:<n>/<path>` and
  `sealed-trace:<n>/<path>` in the scorecard grammar
  (`verdict.Validate` now takes the store), resolving in the shape the
  run produced or the stored one.
- **`seed verdict traces`**: the read verb rendering every node with
  its citation path, kind, status and declared attributes; malformed
  and missing named; the raw digest where the sidecar stands.
- **Drills**: `TestTraceShapeNormalizes`, `TestReceiptBindsTraceShape`,
  `TestScorecardCitesTraceSpansInPackage`, `TestTraceAttributesSection`
  and the CLI stand `TestTraceArtifactsStoredAndChecked` (store,
  render, cite, check, erase the sidecar, erase the shape, reconcile,
  the invented entry, the silent spec, the malformed export).
- **Spec**: `verdicts.md` "Trace-shaped evidence" and the runner
  profile's variable, `acceptance.md` "Trace attributes"; III.G rows 5
  and 8 keep their status with the drills added to their evidence; row
  10 stays as the Phase 10 record left it.

## III.F row 12: dependencies cascade, holds cascade, rollups render, ancestry warns (os-f0ae2cdf)

The one Part III row the Phase 5 exit record routed to "Phase 13's
catch-all, which names no item", closed as one graph-derived feature
against plan #341 (`plans/os-f0ae2cdf.md`). The transition table stays
the sole authority on lifecycle legality and terminality
(`transitions.json` untouched); what a contract requires, belongs
under and serves are facts beside the lifecycle, and everything the
row asks for is derived. What landed (`docs/decisions.md`
"III.F row 12"):

- **The four facts**: `dependency.linked`, `dependency.unlinked`,
  `hierarchy.parented`, `goal.aligned`, additive catalog growth from
  `seed/1` under `dispatch` with the operator fallback, strict
  payloads, on a known open contract; the graph rule (known target,
  no self edge, no dependency or hierarchy cycle, links a set, parent
  and mission replaced) at the admission rule `topology`,
  `topology_refused` naming the part; `seed topology depend |
  undepend | parent | align` through the checked path; the
  dispatcher's `acts_through`; four residuals in the injection table.
- **One trustworthy fold** (`internal/topology`): each relation judged
  at its own position (version, the signer's grant with the keyring
  replayed there, shape, the graph rule against the lifecycle at that
  prefix); a fact that fails is a named anomaly that hides no work,
  holds no claim and lends no mission; admission and every projection
  read `admit.Topology`.
- **Effective readiness** (`ready`, every dependency terminal, no
  blocked ancestor) consumed by the claim boundary
  (`not_effectively_ready`), the queue (Version "4", derivation
  `transitions/1+topology/1`), the poll (`offers.Live`, the
  eligibility helper moved out of `cmd/seed` into `internal/offers`
  and shared with the report) and the wake delta; holds cascade as
  `held_by` with no descendant rewritten.
- **Advisory wakes**: `offers.Bridge` over a readiness delta from the
  supervisor's cursor, matching live authorized offers to eligible
  active actors and calling the adapter's `Wake` only through a
  registered channel, errors reported and never acted on; `seed offer
  wake --since` runs the pass and registers no channel (every shipped
  `Wake` is the documented no-op); `TestWakelessPollOnlyRun` retained
  unchanged.
- **Initiatives and goals**: exact transitive rollups (state counts,
  terminal, effectively ready, dependency-waiting, held, the milestone
  sum from the new `Fold.Milestone`) and goal-ancestry warnings for
  open work with no mission on itself or any ancestor, the mission's
  carrier rendered beside them.
- **Surfaces**: contracts Version "15" (`topology` per entry), report
  Version "19" (`topology`: initiatives, warnings, anomalies), cache
  Version "15" at schema generation 13 (four tables and the report
  key), each present only when the prefix carries a relation fact, so
  relation-free chains stay byte-identical.
- **Drills**: `TestDependencyCascadeWakesAndPolls`,
  `TestHoldCascadeSuppressesWakeUntilReleased`,
  `TestInitiativeRollupRendersDescendants`,
  `TestGoalAncestryWarnsOnlyOpenUnanchoredWork` (`cmd/seed`),
  `TestTopologyRelationBoundary` and the affordance drill
  (`internal/admit`), the fold, rule and derivation drills
  (`internal/topology`), the bridge drill (`internal/offers`).
- **Spec**: `spec/topology.md` (new); `protocol.md`, `actors.md`,
  `envelope.md`, `offers.md`, `lifecycle.md`, `projections.md` amended;
  `conformance.json` F.12 `met`, the generated capability, lane and
  conformance documents regenerated.

## Verdict defer on the machine surface (os-ef2e3134)

Not a plan item: the follow-up os-7fc2ca38's decision log left for its
own card (plan #355). `defer` joins the verdict group's registry row
and usage line, so `serve --list` carries `verdict.defer` and the
registry resolves it (III.I); `TestVerdictDeferExposed` holds it. No
verb behavior, spec or conformance change.

## The card's canonical form, and the reader's half of the boundary (os-f11601e0)

Not a plan item: two gaps a read-only review found in the
cross-organization boundary that landed with Phase 13 item 5 (#279,
os-40ed0ca0). Plan #368.

`Card.Canonical()` claimed JCS-canonical bytes and was `encoding/json`
with `SetEscapeHTML(false)`. Go escapes U+2028 and U+2029
unconditionally, JCS emits them literally, and `Card.check` constrains
a card's `name` only to be non-empty, so a deployment name carrying
either character was signed over bytes no independent verifier
reconstructs. It now routes through `jcs.Transform`, the transform
`internal/event` already signs over, pinned by a property test over a
corpus that includes both separators in a name, in the ingress and in a
squad name (`TestCanonicalIsRFC8785`) and by a fixed vector holding the
checked-in card's canonical bytes at an unchanged 323 bytes, sha256
`b9ba3490…` (`TestCheckedInCardCanonicalizesUnchanged`). The recorded
decision "Canonical bytes without a JCS library" is corrected in place
rather than quietly replaced.

`boundary.Verify` existed with no caller but an optional flag, and
`boundary check` requires the declaration that renders the card, which
a stranger does not have: the spec's reader was reachable by no verb.
`seed boundary verify --card <file> --pubkey <hex> | --pubkey-file
<path>` is that verb, taking no declaration and no name, refusing with
`card_refused` rather than `card_drift`
(`TestBoundaryVerifyReadsACardWithNoDeclaration`, over a
test-generated keypair, so the mechanism is proven without the
repository's absent key).

`Makefile` is unchanged and no conformance row moves. The fixture
card's signing key is a throwaway kept out of the tree, so nothing here
can verify it and no agent can make it verifiable; `make check` stays a
content gate and now says so, in the success envelope's note and in
`spec/boundary.md`. Minting the operator identity, re-signing, and
pinning the public half beside `protected` and `CODEOWNERS` is the
operator's act, filed as os-b3eeeabb.

## Frontier


Phases 0 through 5 are done and closed: every Phase 5 plan (#113–#116,
#120, #121), every implementation (5.1 #122 through 5.6 #131), the
three post-merge follow-ups (#127, #128, #129), and the engine
v0.15.1 receipt-runner pin (#130) are merged, and the Phase 5 exit
record above is card os-6e37b10e's task PR (#134, merged; card
closed). Phase 6 is done and closed: every plan (#133, #136, #138,
#140), every implementation (6.1 #135, 6.2 #137, 6.3 #139, 6.4
#141), and the exit record above (card os-600be59e's task PR) are
merged with every card closed. Phase 7 is done and closed: every
plan (#144/#146, #147/#148, #150, #153), every implementation (7.1
#145, 7.2 #149, 7.3 #151 with follow-up #152, 7.4 #154), and the
exit record above (card os-c9e24032's task PR) are merged with
every card closed. Phase 8 is done and closed: every plan (#158,
#162, #164), every implementation (8.1 #160, 8.2 #163, 8.3 #165),
the out-of-item ledger writeHead race fix (#161 against plan #159),
and the exit record above (card os-ef715d17's task PR) are merged
with every card closed. Phase 9 is done and closed: every plan (#170,
#172, #187, #189, #190, #197, #203, #204, #206, #209, #210), every
implementation (5(a) #171, 5(c) #173, 1a #188, 1c #191, 1b #192, 2
#200, 3 #205, 4 #207, 5(b) #211, the role-grant gap #212), the
out-of-item fixes (#175's task PR, os-9b3f3ef3, os-378e44f3, budget
exhaustion's exit code #208), and the exit record above (card
os-e6cdb3d9's task PR) are merged with every card closed.
Phase 10 is done and closed: every plan (#215, #217, #219, #223,
#224, #225, #227), every implementation (10.1 #216, 10.2 #221, the
tier vocabulary #222, 10.3 #233, 10.4 #238, 10.5 #239), the
out-of-item auto-gc fix (#232), and the exit record above (card
os-a026f5ea's task PR) are merged with every card closed.

Phase 11's five items are merged (#234, #235, #236, #237, #240 against
plans #226, #228, #229, #230, #231), so its exit criteria are met by
their items; the Phase 11 exit record above is card os-efb2a099's task
PR (this card), which closes the phase.

Phase 12's six items are merged (#250, #252, #253, #254, #255, #272
against plans #241, #244, #246, #247, #248, #249), with the reap arm
#267 and the two gap cards #265 and #266, so its exit criteria are met
by their items; the Phase 12 exit record above is card os-9e9ae30c's
task PR (this card), which closes the phase.

Phase 13 declares `deps: 12`, so its gate opened when the Phase 12
exit record merged (#284), and every numbered item has a merged PR: 1
in #269, 2 in #282, 3 in #281, 4 in #270, 5 in #279 (its
canonicalization follow-up os-1c284ba8 merged, #290), 6 in #273, 7
in #286, and item 8, added by plan #350 for III.D rows 5–7, is
os-b45c308d's task PR #361, merged; the two routed gaps (#265, #266)
are merged. The conformance report (os-83bc3d84, #289) is merged, so
the doctor reports the open rows; the promotion evidence packet
(os-98ce6f8a, plan #291 and task PR #294 both merged) is the other
thing the phase's exit line needs. The exit record (os-d63c7441, plan
#288 merged) is parked
on the packet: its plan writes the record only when the doctor
reports complete, which III.R's rows make reachable after the shadow
run and the cutovers, not before. The build plan's §3 backlog is
filed now that the items are exhausted: the target-scale contention
benchmark (os-a00d3f34, III.C row 4, the one conformance-relevant
extra; merged as #297 against plan #293, two clean 200-writer
readings and the defect it found carded as os-5063e8ba), sharded
intake (os-7953612b) and dashboard tiers (os-f17567a6); the shadow
run's tooling, `seed ledger audit` (os-7599c27d, merged as #296
against plan #295; its reservation-verb defect carded as os-b86dab4c,
plan #304); the coverage floor card (os-f262585a) is merged (#314
against plan #292), the tree measured cold at the reading its section
above records; the macOS cleanup race in the flywheel drill's skip path is
merged (#301, then #302; os-222189a3); the citation stage of `docs
check` (os-5fe43832) holds every relative markdown link in the tree to
the tree, filed after a sweep found seven that did not resolve, four of
them in `docs/CONTRIBUTING-AGENTS.md`, which III.Q row 5 cites as the
authority order's evidence, and merged as #305. Two of the seven are not
that branch's to repair: one is a regex in a code span, which the stage
masks rather than the document changes, and one is in
`plans/os-2e34f66a.md`, which a plan file's own single-file gate owns,
so the stage does not read `plans/` at all and the correction is carded
as os-0dba8c6a for a plan PR. III.L row 4's third mode, require-approval
per verb, is the section above (os-5781a026, plan #330): the row reads
`met` on all three modes once it and os-8ecef90f's drills are on
`main`.
as os-0dba8c6a for a plan PR. The guardrail bar's blind-retry clause,
which no surface could measure, is measured by the report over the
attempts journal (os-a9e715dc, plan #332; the section above).

**Two operator decisions on 2026-09-08 changed what the deployment
is and what follows it.**
`decisions/0005-cooperative-posture.md` amends build plan §5's posture
clause: the self-hosting cutover is made at the `cooperative` posture,
so the deployment is the declaration at the root, the root key, one
enrolled key per acting lane and the ledger ref on the existing remote,
with no server, no `pre-receive` hook and no admission service. The
seven criteria are unchanged and are read at that posture; the decision
names what it trades away (the enforced-only guarantees of III.B rows 1,
4 and 5, and with them criterion 7's live protection as opposed to its
implementation claim) and carries its risk statement. No conformance row
is edited. The packet is restated at that posture, and its declaration
block, still held by `TestPacketDeclarationLints`,
`TestPacketDeclarationInitializesUnderTheRootKey` and
`TestPacketProcedureReachesTheFlip`, now declares `cooperative` and
passes all three.
`decisions/0006-v1-retirement.md` adds a third step after the two
cutovers: v1 is retired, per `docs/v1-retirement.md`, in five stages
gated on the self-hosting cutover and on 0004's audit, which
`decisions/0007-no-day-7-wait.md` (2026-09-08) moved from day 7 to
immediately after the flip, keeping the check and dropping the wait, and
independent of distribution. Stages 1 and 2 are claimable before the
cutover and are the only frontier lines that do not wait on the
operator; stages 3 through 5 follow the flip. Stage 1 moved the pull
request gate's purity check to `seed plan classify` and dropped the
skills step, and **measured that `seed plan lint` is not a drop-in
replacement**: Seed's plan grammar is stricter and 100 of 166 plans in
`plans/`, including seven of the twelve most recently added, fail it, so
the plan lint port is a grammar migration deferred to stage 3 with the
conventions the cutover pull request rewrites. Stage 2 pinned the
deletion inventory to the tree (`internal/retirement`, under `make
check`), which caught four abbreviated workflow paths in the inventory's
first draft.

**The flip was rehearsed end to end on 2026-09-08 against this
repository's live v1 state, under a throwaway key, and it found a
defect that would have struck mid-flip.** The import refused
`import_unmapped` on `exempt-plan`, `mail-send` and `mail-ack`: three
run-log verbs postdating the fixture's 2026-09-03 anchor, with three,
one and one occurrences in the whole log, so every drill stayed green
while the live import could not run. Rows are added to
`spec/import-open-seed.json` and the embedded table in parity, and
the fixture is regenerated at `seed-anchor/20260908T083455Z` (2264
records, 1923 events, 620 artifacts, 188 named drops), so the drills
exercise them. The general lesson is recorded in the packet and in the
retirement plan's stage 3: CI proves the migration against a snapshot
and is structurally blind to a v1 verb added after it, so regenerating
and rehearsing before the flip is the only check that sees live drift.
The rest of the procedure read clean: preseed over the imported chain
appended `seed/6` and `seed/7` and was idempotent, `preseed check`
reported nothing pending, a lane key enrolled and was granted, `ledger
verify` verified 1978 records from genesis, and `ledger audit` read all
five bars empty. Two steps of the written order were corrected because
they fail when followed literally: `--ledger` must name a path that
does not exist, and `actor.enrolled` takes the fingerprint as subject
with the raw public key in the payload.

**The cutover's one open design question is settled.** The packet left
mail as "the one loop act without a verb of its own", to be resolved by
the cutover pull request either naming the `seed ledger append --verb
message.sent` form or adding the verb. `seed message send` adds it
(`docs/decisions.md`), because the addressing contract is the part
a hand-assembled payload gets wrong: `to` has three states at the
reading end and the malformed one delivers to nobody, silently, so the
verb writes an all-string array or omits `to` entirely and refuses a
recipient that could not be a fingerprint before appending anything.
Everything else delegates to `ledger append`, so the classification
lint, both postures, the declaration and the persisted head stay one
code path. The subverb is registered on the machine-protocol surface
too, which `TestRegistryMirrorsTheCLIVocabulary` enforces in both
directions. The cutover's `AGENTS.md` rewrite now names a closed verb
set.

**Next action: the operator's deployment, then the operator's
answer.** `docs/promotion.md` presents the seven criteria of
build plan §5, all `met`. Criterion 4 is met on §5's amended text: the
operator's recorded decision `decisions/0004-shadow-run-substitution.md`
(2026-09-06) amends the criterion to accept the credential-free
accelerated simulation in place of the live seven-day shadow run for
the self-hosting cutover, names what it trades away, carries its risk
statement, and moves the five-bar audit over the real chain to day 7
after the flip. That closes the loop os-f79bc5a0 (#316) opened and
os-4fde2bdf (#327) re-derived: the packet could record the substitution
but not amend the plan, so the criterion stood `partial` until the
operator amended §5 by a decision, which is what the decision does.
Every III.R row stays `not measured`, because the simulation raises no
escalation and runs no real backlog for a real week, and III.R is the
charter's, which no build plan decision amends. The last three
agent-side cards at the gate merged: os-8ecef90f (#321) drilled III.L
row 4 on the machine-protocol surface; os-b5051f2e (#323) gave the
five-bar audit's guardrail bar its ceiling arm; os-db5cd353 (#325)
added `artifact.erased` and flipped III.A row 7. One agent-side card
stood in review at the gate and has since merged, and it is not a
criterion: os-0f924157 (#335 against plan #334) closes #323's review
finding, the enforced `seed-admit`
hook reading the declaration at the default branch's tip, so the
ceiling refuses at the boundary and not only at the cooperative
client. The doctor reads 24 outstanding rows (III.F row 12, the
catch-all's one row, is `met` by os-f0ae2cdf, #352; III.D rows 5–7 by
Phase 13 item 8, os-b45c308d): the 17 remaining Phase 13 rows the exit
record flips, C.4 and Q.7 routed to the backlog's scale run and to
promotion, and III.R's seven, none of which an agent act can supply.
What stands between the packet and the Self-hosting question is the
deployment, which the autonomy contract reserves to the operator: at
the `cooperative` posture decision 0005 records, the ledger ref on the
existing remote with no hook and no service to stand up (the enforced
postures stay available, and moving to one later is a change of
`posture` in `seed.json` plus that posture's infrastructure, since the
three differ only in where one rule set runs), the operator's key as the
governance root, one enrolled key per lane,
declared in the block the packet's "The deployment" carries, with
`governance.root` the one substitution (the root key's fingerprint),
which `TestPacketDeclarationLints` holds to `seed preseed check` and
`TestPacketDeclarationInitializesUnderTheRootKey` initializes under a
real key. After the deployment: the operator's answer to the
Self-hosting question at the position they record; then the flip,
which no longer waits on a shadow window, because decision 0004
supplies criterion 4 and moves the audit to after the cutover, taken
as the flip's last step since `decisions/0007-no-day-7-wait.md` dropped
the seven-day wait (a red bar then is a defect card that blocks
retirement stage 4). At the flip, in the order
the packet's "The deployment" gives and `TestPacketProcedureReachesTheFlip`
follows: the v1 state anchored and imported into the deployment's
empty ledger (the import is the genesis transform and refuses a ledger
holding any record), the declaration applied over the imported chain
by `seed init --preseed`, the lane keys enrolled, the ledger pushed to
the hook, and the cutover PR the packet's "The cutover and the
rollback" writes down; and only then the Phase 13 exit record
(os-d63c7441, plan #288 with #315, blocked on this gate), whose plan
writes it when the doctor reports complete, which III.R makes
reachable after the cutovers, not before. The derivation, stated
rather than read off a summary: every Phase 13 item has a merged PR;
the exit line's remaining criterion, the conformance report showing
Part III complete,
cannot be met by any agent act, because the table routes III.R's rows
to measurements a real deployment supplies and the deployment needs
credentials and infrastructure only the operator can grant.

**The destination is promotion (spin-out)**, defined in
`docs/build-plan.md` §5 (merged #169, card os-768361cc) as two
steps — Seed coordinating this repository's own development, then
becoming what new users clone — with seven criteria, Phases 0
through 12 required and Phase 13 alone following, and **neither
cutover autonomously decidable**: spin-out IS the entry-point
switch, so both are escalations. That section is the authority; no
promotion criteria are restated here.

Of the two rows outside III.R that stood open on the tree's own
account, III.L row 4 is drilled for allow and deny by os-8ecef90f (plan
#320, task PR #321, merged) and stands `partial`, its require-approval
mode being os-5781a026's, and III.A row 7 is met by os-db5cd353 (plan
#324, task PR #325, merged), so the doctor reads 24 outstanding rows:
17 Phase 13 rows the exit record flips (III.F row 12 met by
os-f0ae2cdf, #352; III.D rows 5–7 by Phase 13 item 8, os-b45c308d),
C.4 and Q.7 routed to the backlog run and to promotion, and III.R's
seven, none of which an agent can supply. (This paragraph carried two overlapping versions of
itself after #338 merged; the merged reading is the one above.)
os-f0ae2cdf (plan #341, task PR #352) then meets III.F row 12, the
catch-all's one row, and the count reads 27.

The 2026-09-06 formal-methods survey (docs/build-plan.md §3)
filed two backlog cards, listed in the backlog section above:
both are done and closed: os-21bf939f, the admission random walk
(#353), and os-07e6e76c, the append-loop interleaving check (#359).
The follow-on the second card's plan names, the same explorer over the
verdict-then-merge reconciliation and over racing mode, is os-873b5153,
in review as #370 against plan #366.

If an open task PR is red or carries review feedback, drive it green
first — nothing merges out of order.
