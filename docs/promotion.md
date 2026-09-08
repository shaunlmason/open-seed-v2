# The promotion evidence packet

This is the document the operator reads at the promotion gate
(`docs/build-plan.md` §5; plans/os-98ce6f8a.md). The build plan
defines promotion as two human cutovers, self-hosting and then
distribution, gated by seven criteria, and says what agents do at the
gate: drive the work up to it, present the evidence, and stop. This
packet presents. It maps each criterion to the evidence on `main` by
drill name and file, states the criterion's status in a closed
vocabulary, writes the cutover and its rollback down, proposes the
shadow run as a protocol the operator can accept or amend, names the
two cutovers as the reserved escalations they are, and keeps the
ledger of the measurements charter III.R waits on. Nothing here
decides anything.

**How to read it.** Each numbered section is one of the build plan's
criteria, in its order. The section opens with `Status:` and one of
four words: `met` (the evidence on `main` satisfies the criterion),
`partial` (part of it does, and the section says which part is
missing), `not started` (nothing on `main` addresses it), `reserved`
(a decision the packet presents and cannot make). Every `met` or
`partial` criterion cites its evidence as rows of drill, file (under
`next/`) and the pull request that landed it; `internal/promotion`
holds every cited drill to the tree under `make check`
(`TestPacketCitesRealDrills`), so a citation the tree no longer
declares fails the build rather than standing at the gate. A status
that is not `met` carries a `Missing:` sentence; a `reserved` one
carries the `Question:` the operator answers.

**The criteria at a glance.**

| # | criterion | status |
|---|---|---|
| 1 | loop-completeness | met |
| 2 | lanes operable | met |
| 3 | migration proven | met |
| 4 | shadow run | met |
| 5 | cutover and rollback written down | met |
| 6 | core conformance | met |
| 7 | the compromised-actor drill green in CI before the cutover | met |

All seven criteria are `met`. Criterion 4 is met on build plan §5's
amended text, not on its original words: the operator's recorded
decision (`decisions/0004-shadow-run-substitution.md`, 2026-09-06)
amends the criterion to accept the credential-free accelerated
simulation in place of the live seven-day shadow run, names what the
substitution trades away (no live dual-run beside v1, no divergence
reconciliation, no real backlog, no real week, no escalations), and
carries its risk statement. Section 4 records the deviation. The two
cutovers remain the reserved escalations they are; the packet presents
them and stops.

What stands between this packet and the Self-hosting question is not
agent work. No Seed deployment for this repository exists: no
`seed.json` at the root, no `refs/seed/ledger` on the remote, no root
key at a genesis. Standing one up is the operator's, because the
autonomy contract reserves credentials and infrastructure to a human:
the declaration at the root, the operator's key as the governance root,
one enrolled key per lane, and the ledger ref on the existing remote, as
"The deployment" below spells out and as the declaration block there,
linted under `make check`, declares. The posture is the operator's
recorded choice, not the criteria's default:
`decisions/0005-cooperative-posture.md` (2026-09-08) amends build plan
§5's posture clause to `cooperative`, so the deployment needs no server,
no `pre-receive` hook and no admission service. That decision names what
the substitution trades away, chiefly the enforced-only guarantees of
III.B rows 1, 4 and 5 and with them criterion 7's live protection as
opposed to its implementation claim, and carries its risk statement. The
enforced postures stay available: moving to one later is a change of
`posture` in `seed.json` plus that posture's infrastructure, since the
three differ only in where one rule set runs. Nothing agent-side
remains open at this gate: every criterion's evidence is on `main`
(the last three cards, os-8ecef90f for III.L row 4, os-b5051f2e for
the audit's ceiling arm and os-db5cd353 for III.A row 7, merged as
#321, #323 and #325), and the next act is the operator's deployment,
then the operator's answer. The answer is not the flip: the flip is
the cutover pull request "The cutover and the rollback" describes,
merged in the order "The deployment" gives, after the import and the
declaration and the keys, and it is the escalated decision build plan
§5 reserves.

## 1. Loop-completeness

Status: met

A lane runs poll, claim, plan-gate, work, meter, submit, verdict,
merge-observe and a deliberate exit, plus escalation and messages,
entirely through Seed verbs, orienting from one position-stamped read
rather than hand-assembling ledger payloads and hand-computing fences.
Phase 9 item 5 landed the three parts: the obligations projection and
the situation read (#171), the loop verbs that derive every argument
the system can derive and refuse before signing what the boundary
would refuse after (#173, with #175's three post-merge fixes), and the
situation read carrying the caller's messages with "unread" derived
from the cited position (#211). The worker loop runs those verbs end
to end against a real ledger (#191), and the two mode fixtures drive
every lane through the whole loop on a remote with no wake channel
(#207). The lane fragments name the one read each lane orients from,
and `internal/lane` refuses a fragment that instructs a bare
heartbeat (#188).

| drill | file | PR |
|---|---|---|
| `TestLoopVerbsDeriveAndDischarge` | `cmd/seed/loop_cli_test.go` | #173 |
| `TestWorkerLoopRunsEndToEndAgainstARealLedger` | `cmd/seed/loop_e2e_test.go` | #191 |
| `TestSituationRead` | `cmd/seed/situation_cli_test.go` | #171 |
| `TestSituationSinceIsApplicable` | `cmd/seed/situation_cli_test.go` | #171 |
| `TestSituationCarriesTheCallersMessages` | `cmd/seed/message_cli_test.go` | #211 |
| `TestSevenActs` | `internal/loopverb/loopverb_test.go` | #188 |
| `TestSpecNamesTheSameActs` | `internal/loopverb/loopverb_test.go` | #188 |
| `TestOnlyTheExclusiveActIsRemoteOnly` | `internal/loopverb/loopverb_test.go` | #188 |
| `TestLoopSurfaceIsWakeless` | `internal/loop/loop_test.go` | #191 |
| `TestEscalationRaiseAndAnswerThroughTheCLI` | `cmd/seed/escalation_cli_test.go` | #200 |
| `TestSmallTeamModeReachesDone` | `cmd/seed/modes_e2e_test.go` | #207 |
| `TestFleetModeConvergesAndReachesDone` | `cmd/seed/modes_e2e_test.go` | #207 |

## 2. Lanes operable

Status: met

Phase 9 is complete and its exit record (`docs/progress.md`,
card os-e6cdb3d9) walks charter III.J row by row: the six lane role
fragments as grants plus conventions, ordered and validated (#188,
#212 making "six" enforced by name); the dispatcher's least-capability
posture as an allowlist and the injection conformance suite over
intents and tool output (#192), the mirror arm closed by Phase 13 item
4 (#270); the worker loop with exhaustion parking at the real budget
refusal (#191); escalation with packet, question and decision, and
the age and resolution latency derived from the chain (#200); the
maintenance loop runnable unattended and audited as an ordinary actor
(#205); and the small-team and fleet modes running the full loop in
CI with every refusal converging within one retry and the blind retry
pinned as the forbidden outcome (#207).

| drill | file | PR |
|---|---|---|
| `TestTheCharterSixAreClosed` | `internal/lane/lane_test.go` | #188 |
| `TestEveryRequiredCapabilityIsGrantedBySomeManifest` | `internal/lane/lane_test.go` | #212 |
| `TestFragmentInstructingABareHeartbeatIsAFinding` | `internal/lane/lane_test.go` | #188 |
| `TestDispatcherPostureIsAnAllowlist` | `internal/lane/lane_test.go` | #188 |
| `TestNoHostileTextWidensTheDispatcherSet` | `internal/admit/injection_test.go` | #192 |
| `TestNoHostileRequestWidensTheDispatcherSet` | `internal/admit/injection_request_test.go` | #270 |
| `TestWorkerLoopParksOnRealBudgetExhaustion` | `cmd/seed/loop_e2e_test.go` | #191 |
| `TestMaintainHoldsNoPrivatePowers` | `cmd/seed/maintain_cli_test.go` | #205 |
| `TestMaintainFilesDefectsAndRaisesNoEscalation` | `cmd/seed/maintain_cli_test.go` | #205 |
| `TestMaintainCheckpointIsStartableByAFreshReader` | `cmd/seed/maintain_cli_test.go` | #205 |
| `TestBlindRetryDetector` | `cmd/seed/modes_e2e_test.go` | #207 |
| `TestModeGrantsComeFromTheShippedManifests` | `cmd/seed/modes_e2e_test.go` | #212 |

## 3. Migration proven

Status: met

`seed import --from-open-seed` (Phase 12 item 5, #255) is drilled
against a real export of this repository's v1 state, not only a
synthetic one: `fixtures/import/open-seed/` is this repository's
export at `seed-anchor/20260908T083455Z` (2264 records, 1923 events,
620 artifacts, 188 named drops), and `TestRealFixtureImports` folds
every one of its contracts to the state its card holds. `make
fixture-import` regenerates the fixture from the live repository at
the newest anchor, so the drill stays real as the v1 history grows,
and the cutover procedure below re-imports at the final anchor rather
than trusting a snapshot. The four refusals precede every write.

**A snapshot cannot see live drift, and one rehearsal proved it.** On
2026-09-08 the whole procedure was run against this repository's *live*
export under a throwaway key, and the import refused
`import_unmapped` on three verbs the previous fixture predated:
`exempt-plan` (the operator's `plan_exempt` reason), `mail-send` and
`mail-ack`. All three are rare, three, one and one occurrences in the
whole run log, which is why an older snapshot missed them while CI
stayed green. The rows are added (`spec/import-open-seed.json` and
the embedded table, in parity) and the fixture is regenerated at the
anchor above, so the drills exercise them. The lesson generalizes past
this fix: **CI proves the migration against a snapshot and is
structurally blind to a v1 verb added after it**, so the regeneration
in the cutover order below is not hygiene, it is the only thing that
catches this class, and the rehearsal is worth repeating on the day.

The same rehearsal carried the procedure to the end and read clean:
preseed applied over the imported chain (`seed/6`, `seed/7`) and was
idempotent on a second run, `preseed check` reported nothing pending,
a lane key enrolled and was granted, `seed ledger verify` verified
1978 records from genesis, and `seed ledger audit` read all five bars
empty. It also corrected two steps of the written order that fail when
followed literally: `--ledger` must name a path that does **not**
exist, since a pre-created empty directory refuses `unavailable`; and
`actor.enrolled` takes the fingerprint as its subject with the raw
public key in the payload, which are different values.

| drill | file | PR |
|---|---|---|
| `TestRealFixtureImports` | `internal/importer/fixture_test.go` | #255 |
| `TestAnchorRefusalsPrecedeEveryWrite` | `internal/importer/drills_test.go` | #255 |
| `TestNonEmptyLedgerRefuses` | `internal/importer/drills_test.go` | #255 |
| `TestUnmappedVerbRefuses` | `internal/importer/drills_test.go` | #255 |
| `TestSyntheticPredecessorImports` | `internal/importer/drills_test.go` | #255 |
| `TestImportCommandEnvelopes` | `cmd/seed/import_cli_test.go` | #255 |

## 4. Shadow run

Status: met

Met on the criterion's amended text. Build plan §5 criterion 4 was
amended on 2026-09-06 by the operator's recorded decision,
`decisions/0004-shadow-run-substitution.md`, to accept the
credential-free accelerated simulation (`seed simulate`, `--days 7`,
`--intents 24`, `--posture enforced-self-hosted`) in place of the live
seven-day shadow run for the self-hosting cutover. The decision is the
operator's, made as the deviation it is under §5's own rule for a step
that trades away what the plan requires; this packet records it and
did not make it. The simulation drives a synthetic backlog through the
real boundary (admission, the `seed-admit` pre-receive hook on a local
bare remote, the ledger, every lane manifest, and the five-bar audit)
with zero credentials and a mock executor. The five-bar audit over the
simulated chain is clean: zero chain violations, zero lost updates,
zero silent abandonments, zero guardrail breaches, zero unreserved
spend; all 24 intents reached `done` in the accelerated seven-day
window. The decision records a fresh reading on the tree that carries
it, draw seed 20260906, with the same result.

What the substitution trades away, in the decision's words: the
simulation does not run a live dual-run beside v1 with divergence
reconciliation, files no card of this repository, does not run
unattended for a week on a real backlog, and raises no escalation.
The criterion's original words asked for those; the amended text
accepts their absence for this cutover and moves the five-bar audit
over the real chain to after the flip, its reading appended to
the divergence log at the end of this packet.
`decisions/0007-no-day-7-wait.md` (2026-09-08) then dropped the
seven-day wait 0004 had set and kept the audit, so it runs as the
flip's last step; that record names what the earlier reading gives up,
chiefly a defect that only appears under real load. The decision does not
touch charter III.R: the simulation measures none of its rows, so the
measurement ledger below is unchanged by it.

| drill | file | PR |
|---|---|---|
| `TestSimulateReachesDoneEnforced` | `cmd/seed/simulate_cli_test.go` | #272 |
| `TestSimulateAcceleratedBacklog` | `cmd/seed/simulate_cli_test.go` | #272 |
| `TestAuditCatchesSilentAbandonment` | `internal/simulate/audit_test.go` | #272 |

The live-shadow-run protocol the build plan originally named is
preserved in the section "The shadow run, as a protocol" below, as the
protocol the operator substituted; the protocol text stands as the
record of what was proposed and what was traded away, and remains
available to run on the deployment after the cutover if the audit
warrants it.

## 5. Cutover and rollback written down

Status: met

The build plan asks for three things written down: which entry point
flips when, what stays authoritative where during the window, and the
documented path back. The section "The cutover and the rollback"
below answers the three by name, and the packet's own drill holds the
section to that shape, so the criterion is met by this document
existing in the tree rather than by a claim about it.

| drill | file | PR |
|---|---|---|
| `TestPacketWritesTheCutoverDown` | `internal/promotion/promotion_test.go` | #294 |
| `TestPacketCitesRealDrills` | `internal/promotion/promotion_test.go` | #294 |
| `TestPacketDeclarationLints` | `cmd/seed/promotion_cli_test.go` | #327 |
| `TestPacketDeclarationInitializesUnderTheRootKey` | `cmd/seed/promotion_cli_test.go` | #327 |
| `TestPacketProcedureReachesTheFlip` | `cmd/seed/promotion_cli_test.go` | #327 |

## 6. Core conformance

Status: met

Phases 0 through 12 are complete and recorded, and the doctor reports
exactly which Phase 13 rows remain open: the conformance report
(os-83bc3d84, #289) checks in Part III as a table held to the
charter row for row, renders it under the docs drift gate, and gives
`seed doctor --repo .` a `conformance` section that counts the rows by
status, lists every row not yet met by pillar, row and status, sets
the enforced-only rows aside at the cooperative posture and names the
mixed rows there, and reports `complete` only when every applicable
row is met. The rows it lists as open today are Phase 13's — flipped
by the Phase 13 exit record (os-d63c7441) once III.R's measurements
exist, the promotion critical path in the build plan's own words —
and P.1, which os-53650015 records as partial: its tagged-release
clause is unmet until §5 step 2 cuts the first seed/v* release.

| drill | file | PR |
|---|---|---|
| `TestTableIsTheCharterRowForRow` | `internal/conformance/conformance_test.go` | #289 |
| `TestTableDriftFromTheCharterIsRefused` | `internal/conformance/conformance_test.go` | #289 |
| `TestVocabularyHolds` | `internal/conformance/conformance_test.go` | #289 |
| `TestAssessJudgesAtThePosture` | `internal/conformance/conformance_test.go` | #289 |
| `TestConformanceRendersFromTheTableWithoutAClock` | `internal/docs/docs_test.go` | #289 |
| `TestDoctorReportsConformanceAtThePosture` | `cmd/seed/doctor_test.go` | #289 |

Phases 0 through 12 each closed with an exit record that walks the
pillars its exit line names, row by row, with drills on `main` cited
by name (`docs/progress.md`). One drill per phase stands here as
the pointer into that record; the record carries the rest.

| drill | file | PR |
|---|---|---|
| `TestCorruptionsAreDetectedDistinctly` | `internal/ledger/corruption_test.go` | #79 |
| `TestHostileCorpusRefuses` | `internal/classify/classify_test.go` | #80 |
| `TestDrillRawAdversaryPerPosture` | `cmd/seed-admit/drill_test.go` | #99 |
| `TestDrillKillAndReplace` | `cmd/seed-admit/drill_test.go` | #99 |
| `TestDrillKeyRotation` | `cmd/seed-admit/rotation_test.go` | #104 |
| `TestRebuildByteIdenticalAndStamped` | `internal/project/project_test.go` | #117 |
| `TestClaimRaceStorm` | `internal/gitref/gitref_test.go` | #123 |
| `TestPacketResumeDrill` | `internal/packet/resume_test.go` | #124 |
| `TestPlanGateAboveTrivialTier` | `internal/admit/plan_test.go` | #126 |
| `TestSubjectClassifiesInducedDivergences` | `internal/reconcile/reconcile_test.go` | #137 |
| `TestWakelessPollOnlyRun` | `cmd/seed/offer_cli_test.go` | #145 |
| `TestReservationRaceAndStatus` | `cmd/seed/budget_cli_test.go` | #149 |
| `TestDisposabilityDrill` | `cmd/seed/run_cli_test.go` | #151 |
| `TestAffordanceRegressionClass` | `internal/admit/soundness_test.go` | #163 |
| `TestHaltRefusesEverythingButLift` | `internal/halt/halt_test.go` | #84 |
| `TestSmallTeamEvalQualifiesAndDisqualifiesThroughTheProductionMachinery` | `cmd/seed/modes_e2e_test.go` | #221 |
| `TestSmallTeamCriticalContractReachesDoneAtL2` | `cmd/seed/level_modes_e2e_test.go` | #233 |
| `TestSmallTeamRubricContractsReachDone` | `cmd/seed/rubric_modes_e2e_test.go` | #238 |
| `TestSmallTeamPromotionDeliversLessonsAtClaimTime` | `cmd/seed/lessons_e2e_test.go` | #235 |
| `TestPoisonsRefuseAtTheTerminal` | `cmd/seed/modes_e2e_test.go` | #236 |
| `TestEveryPoisonFailsAtBothEnds` | `internal/admit/poisoning_test.go` | #236 |
| `TestSmallTeamRetirementAndRevalidationAtClaimTime` | `cmd/seed/retirement_e2e_test.go` | #237 |
| `TestSmallTeamChoreWorkedThreeTimesConverts` | `cmd/seed/flywheel_e2e_test.go` | #240 |
| `TestServiceAgreesWithTheBoundaryAndTheHook` | `cmd/seed-admit/serve_test.go` | #252 |
| `TestGitHubAdapterReconciles` | `internal/protections/github_test.go` | #252 |
| `TestRunReMeasuresColdOnce` | `internal/perfgate/perfgate_test.go` | #253 |
| `TestInitPreseedIsIdempotentAndDriftRefuses` | `cmd/seed/preseed_cli_test.go` | #254 |
| `TestAgentCeilingReadsTheRosterKind` | `internal/admit/policy_test.go` | #254 |
| `TestGeneratedContentIsFromTheTables` | `internal/docs/docs_test.go` | #272 |
| `TestSimulateReachesDoneEnforced` | `cmd/seed/simulate_cli_test.go` | #272 |
| `TestSimulateAcceleratedBacklog` | `cmd/seed/simulate_cli_test.go` | #272 |
| `TestAuditCatchesSilentAbandonment` | `internal/simulate/audit_test.go` | #272 |

Phase 13 is not a precondition of promotion (build plan §5, "what is
not required"), and its items are on `main` regardless: request
ingress and federation (#270), the cross-organization boundary
(#279), the machine-protocol surface and the platform matrix (#273),
the Forgejo adapter (#281), the remaining executor adapters (#282),
tuple ranking (#286), racing (#269). Their drills are cited in the
conformance table, not repeated here.

## 7. The compromised-actor drill green in CI before the cutover

Status: met

Phase 12 item 1 (#250) is the release gate: `internal/redteam` asserts
the charter's §I.2 ceiling item by item against an enforced fixture
with a valid key, a credential and raw git, the ceiling and the
residuals pinned as tables both ways, and the code-ref rules proven
load-bearing. It runs under `make check`, which
`.github/workflows/check-validate.yml` runs on every push to `main`,
every pull request and every merge group, so no commit exists that
the drill has not gated, and the cutover commit will be one of them.
The consequence's second half, revocation reaping the revoked
holder's open claims on the revocation alone, landed in #267.

| drill | file | PR |
|---|---|---|
| `TestCeilingHoldsAtThePush` | `internal/redteam/redteam_test.go` | #250 |
| `TestOneDerivationLedgerAgrees` | `internal/redteam/redteam_test.go` | #250 |
| `TestCoverageBothWays` | `internal/redteam/redteam_test.go` | #250 |
| `TestResidualsArePinned` | `internal/redteam/redteam_test.go` | #250 |
| `TestTablesValidate` | `internal/redteam/redteam_test.go` | #250 |
| `TestCodeRefRulesAreLoadBearing` | `cmd/seed-admit/mutation_test.go` | #250 |
| `TestDrillCompromisedKeyCutPerPosture` | `cmd/seed-admit/rotation_test.go` | #104 |
| `TestRevokedHolderReapsOnTheRevocationAlone` | `internal/admit/revoked_test.go` | #267 |

## The shadow run, as a protocol

The protocol the operator substituted the accelerated simulation for
(section 4; `decisions/0004-shadow-run-substitution.md`), preserved
as the record of what was proposed and what was traded away. A
proposal for criterion 4, in the build plan's original words: "Seed
coordinates a declared slice of this repository's own cards beside v1
for a stated window, with any divergence reconciled and recorded."
Every line below is a default the operator can amend; none of it is
in force until the operator says so. This section presents; it does
not schedule. The deployment it describes is the one the cutover
needs regardless. No window opens and no slice is declared unless the
operator reinstates the live run, before or after the cutover, which
is what build plan §5 asks of the packet: present the evidence and
stop. The slice as proposed has gone stale since it was written
(os-a00d3f34 merged as #297, os-f262585a is in review) and would be
re-declared from the cards open on the day a window opened.

**The deployment.** What the cutover needs whether or not a window
runs, since the ledger authority moves to is this deployment either
way: a declaration for this repository at the root (`seed.json`,
`posture.DeclarationPath`, the file the doctor and the remote verbs
read, and the hook too where a posture installs one), in the shape
`seed init --preseed` reads
(`spec/postures.md`, "The preseed"). The proposed content, with
one required substitution: `governance.root` is the fingerprint of the
operator's root key, because `seed init --preseed` refuses
`preseed_drift` before genesis when the declared root is not the
initializing key (`cmd/seed/preseed.go`, `applyPreseed`), naming that
key's fingerprint in the refusal, so the block as printed initializes
nothing and the value to substitute is read off the refusal, or off
`seed init`'s `governance_root` on a throwaway ledger.
`TestPacketDeclarationLints` holds the block to
`seed preseed check`, and `TestPacketDeclarationInitializesUnderTheRootKey`
initializes it under a real key with the fingerprint substituted and
holds the unsubstituted block to that refusal, so what the operator
copies is one `make check` has both linted and initialized:

```json
{
  "posture": "cooperative",
  "protocol": "seed/7",
  "governance": {
    "root": "<the root key's fingerprint>",
    "owners": ["@shaunlmason"],
    "change_process": "pr+owner-review"
  },
  "protected": [
    "spec", "internal/admit", "internal/transition",
    "internal/keyring", "internal/verdict", "internal/seal",
    "internal/eval", "evals", "internal/curation",
    "knowledge/lessons", "lanes", "cmd/seed-admit",
    "cmd/covergate", "Makefile", ".github/workflows", "scripts"
  ],
  "checkpoints": {"trust": "signers"},
  "guardrails": {
    "squads": {"core": {"default": "standard", "max_agent": "standard"}},
    "paths": [
      {"prefix": "internal/admit", "min": "critical"},
      {"prefix": "spec/transitions.json", "min": "critical"}
    ]
  },
  "teams": {
    "squads": [{"name": "core", "lanes": ["dispatcher", "planner", "implementer", "verifier", "curator", "maintenance", "supervisor", "observer"]}]
  }
}
```

The ledger ref `refs/seed/ledger` lives on the remote the code already
lives on: at the cooperative posture decision 0005 records, every writer
self-validates through `internal/admit` before pushing and no
server-side component exists, so there is no bare repository to stand up
and no hook to install. `seed doctor` prints `posture.Consequence`
verbatim against this deployment, which is the intended standing
reminder and is not to be suppressed. The
genesis names the operator's key as the governance root; one key per
lane is enrolled for the identities that will act (the implementer
lane for the sessions that work cards, the dispatcher lane for the
session that files intents, the verifier lane for the reviewing
identity, the maintenance lane for the scheduled pass, an observer
for `merge.observed`).

**The order.** The deployment's ledger is written by the import, and
by nothing before it. `seed import --from-open-seed` is the genesis
transform and refuses every ledger that holds a record
(`ledger_not_empty`, exit 3; `internal/importer/importer.go`,
`spec/import.md`), so a ledger `seed init --preseed` has already
initialized cannot be imported into, and the declaration is applied
over the imported chain, never under it. A deployment standing before
the flip is therefore the declaration at the root, the root key and the
lane keys, with `refs/seed/ledger` empty on the existing remote: at the
cooperative posture decision 0005 records there is no hook to stand up
beside them;
at the flip, in this order: `scripts/seed state anchor` and
`scripts/seed state export` on v1; `seed import --from-open-seed
export.json --source <clone> --repo <checkout> --ledger <empty dir>
--artifacts <dir> --key <root key>`, which writes genesis under the
root key, the activations through `seed/5`, `system.imported`, the
enrollments and the whole history; `seed init --preseed seed.json
--ledger <that dir> --key <root key> --lanes lanes`, which holds
the chain to the declaration (the root is the importing key, or
`preseed_drift`) and appends exactly the activations the declaration
names beyond the import's, `seed/6` and `seed/7` today, and nothing on
a second run; `seed preseed check --config seed.json --ledger <that
dir>`, green with nothing pending; the lane keys enrolled and granted
by the root (`seed ledger append --verb actor.enrolled --subject <the
key's fingerprint> --payload '{"key": "<the raw 32-byte public key in
hex>", "kind": "agent", "name": "<lane>"}'`, then `actor.granted` with
`{"capability": "<grant>"}` on the same subject: the subject is the
fingerprint and the public key rides in the payload, and the two are
different values, so a run passing the fingerprint as the key, or the
key as the subject, refuses `chain_invalid`); and the ledger directory (`HEAD` and
`segments/*.jsonl`, the layout the guarded ref carries) committed as
the tree of `refs/seed/ledger` and pushed once. Under an enforced
posture the hook verifies the pushed chain from genesis at that push and
admits every append after it (`spec/admission.md`, "The ledger
half"); at the cooperative posture each writer runs that same
`internal/admit` rule set itself before pushing, and `seed ledger
verify` over the ref is what reads the chain back. A ledger a
shadow window wrote, had one run, is a shadow and not the target of
the import: it is kept for its record beside v1's frozen ref.
`TestPacketProcedureReachesTheFlip` runs that order through the CLI
against a predecessor fixture, with this block's root substituted, and
proves the reverse order refuses by name, so the procedure an operator
follows literally is one `make check` has followed. The real export of
this repository follows the same order (criterion 3's fixture: the
import writes 1344 records at `seed/5`, the declaration then appends
the two activations and reads unchanged, the check reads green).

**The slice.** This workstream's own `next:` cards open at the
window's start: the backlog cards the build plan's §3 names
(os-a00d3f34, os-7953612b, os-f17567a6), the coverage card
(os-f262585a) and the exit record (os-d63c7441). Each is filed on the
ledger as an intent whose subject is the v1 card id, so every
divergence is a diff between two records of one thing.

**The window.** Seven days from the ledger position the operator
records as the start, the length charter III.R row 5 asks for, ended
by a second recorded position.

**The dual-run rule.** v1 stays authoritative for every card in the
slice for the whole window. Every v1 transition on a sliced card is
mirrored to the ledger by the same actor in the same session:
`claim.taken` beside `seed task claim`, the plan's approval beside
the plan PR's merge, `submission.made` beside the task PR,
`merge.observed` by the observer when the PR merges, the deliberate
exits beside the card's park, release or review. The ledger's
projections are read on every wake, as the lane fragments say, and
never acted on alone.

**Divergence.** Once a day the folded contract states
(`contracts.json`) are diffed against the v1 card states of the slice
and the diff is appended to the log at the end of this packet: date,
position, card, v1 state, ledger state, the reconciliation. Every
divergence is reconciled toward v1 by an admitted act on the ledger,
never by editing history, and a divergence that recurs is a defect
card.

**The evidence at the end.** The five-bar audit over the real chain
(`simulate.Audit`, `internal/simulate/audit.go`, run over any ledger
an operator points at by `seed ledger audit`, os-7599c27d), the
report's `lanes` section for the two rates, the shape of every
`escalation.raised` payload, the receipts' independence on the
happy-path submissions. Each feeds the measurement ledger below
through a follow-up card that revises this packet.

## The cutover and the rollback

Criterion 5, in the build plan's three clauses.

### Which entry point flips when

Today `scripts/seed` (v1, the pinned engine) is the only coordination
entry point, and the build plan's ground rules keep it so "until
spin-out". The flip is one change, made only when the seven criteria
are all `met`, criterion 4 included. They are: build plan §5 counts
the cutover on all seven, and criterion 4 is met on §5's amended text
by the operator's recorded decision
(`decisions/0004-shadow-run-substitution.md`, section 4), so the flip
no longer waits on a shadow window; what it waits on is the
deployment and the operator's answer to the Self-hosting question,
and the decision binds the five-bar audit over the real chain at day
7 after the flip. The change itself: the root `AGENTS.md` section "How work
happens" is rewritten
around the Seed loop verbs (`seed situation`, `seed claim take`,
`seed submission make`, `seed claim release|park`, `seed escalation
raise`, `seed message read` and `seed message send`) and the lane
fragments under
`lanes/`, the Seed binary built into `next/bin/seed` becomes the
verb every role file names, and `scripts/seed task` is retired from
every role file and the dispatch and maintenance workflows in the
same pull request. That pull request is the cutover; its merge is the
moment authority moves, and it is the escalated decision the build
plan reserves. Before it merges, the v1 state ref is anchored one last
time (`scripts/seed state anchor`) and imported at that anchor into the
deployment's empty ledger (`seed import --from-open-seed`), in the
order "The deployment" gives: the import is the genesis transform, so
it is the first write to the ledger that becomes authoritative, the
declaration is applied over it by `seed init --preseed`, and a ledger
a shadow window wrote is kept for its record and is never the target
of the import. So the ledger holds the whole history at the flip.

### What stays authoritative where during the window

Until the cutover's merge, v1 is authoritative for every card; if a
shadow window is ever run (decision 0004 makes it optional, before or
after the cutover), the ledger it writes is a shadow that records the
slice and is read for orientation only. From the cutover's merge, the ledger is
authoritative for every contract filed after it and for every
imported contract, and v1's `seed-state` ref is frozen at its final
anchor and kept read-only for history, never written again. CI's
receipt gate (`receipt verify`, the plan-at-merge-base rule and the
reviewer-identity check) is unchanged through both: it is a property
of pull requests, not of the queue. The plan and task PR
conventions (`plans/<id>.md` merged first, `seed/<id>` never touching
`plans/**`) carry over unchanged, with the card id now a ledger
subject.

### The path back

The ledger is append-only, so rollback never rewrites it. The path
back is the flip reversed: the cutover pull request is reverted, the
frozen `seed-state` ref is unfrozen (its anchor tag is the position
to resume from), and every contract the ledger admitted after the
cutover that has no v1 card is listed from the contracts projection
(`seed project` at the rollback position) and filed as a card by the
dispatcher lane, with the ledger's positions cited in the card body.
Claims in flight on the ledger are released with packets, so the
work resumes from the packet on v1 exactly as a preempted worker
resumes. The ledger keeps running as a shadow, so the rollback loses
no history and the next cutover attempt starts from a longer record.

## The two cutovers are escalations

Neither cutover is autonomously decidable (build plan §5): spin-out
is the entry-point switch, and renaming the later publish does not
authorize the earlier authority switch. Agents drive the work up to
each gate, present this packet, and stop.

**Self-hosting.** Question: does this repository's own development move to Seed at the position the operator records as the start, on the terms in "The cutover and the rollback", with criterion 4 met by the substitution decision 0004 records?

Its preconditions, for putting the question, are seven criteria `met`
(the fourth on build plan §5's amended text, by the operator's
recorded decision `decisions/0004-shadow-run-substitution.md`), a
deployment standing at the cooperative posture decision 0005 records ("The
deployment"), the v1 state anchored and imported into that
deployment's ledger at the flip, and the compromised-actor drill green
on the commit that carries the cutover. The criteria are met on
`main`; the deployment is not stood up, and standing it up is the
operator's. An answer of yes is not the flip: the flip is the cutover
pull request, merged in the order "The deployment" gives (the v1
state anchored and exported, imported into the empty ledger under the
root key, the declaration applied by `seed init --preseed`, the
preseed check green, the lane keys enrolled and granted, the ledger
pushed once to the remote), and its merge is the escalated decision
build plan §5 reserves. The decision binds one act after the flip
that this packet records rather than waives: the five-bar audit over
the real chain, appended to the divergence log below, a red bar a
defect card that blocks retirement stage 4. It ran at day 7 as 0004
wrote it; `decisions/0007-no-day-7-wait.md` moved it to immediately
after the flip, keeping the check and dropping the wait.

**Distribution.** Question: does Seed become what new users clone, and from which repository?

Its preconditions are self-hosting held for a stated period without a
rollback, a released Seed binary with checksums and provenance
(charter III.P row 1's one residual today: the binary is built from
source and is not yet a released artifact), and a README a team that
has never spoken to the authors can adopt from in under an hour (III.R
row 7).

## The III.R measurement ledger

The conformance table (`spec/conformance.json`, os-83bc3d84)
routes each row of charter III.R to a measurement and says the row
flips to `met` when this packet records it. The operator's substitution (section 4) does not supply the
measurement for any III.R row: the simulation does not run
unattended for a week on a real backlog (R.5), it does not generate
escalations (R.4), it has no human reviewer (R.1–R.3), it does not
substitute for a quarter of real elapsed time (R.6), and it is
internal and synthetic, not an external adoption (R.7). Every row
therefore remains `not measured`. A follow-up card revises the
conformance table only when a real measurement exists. III.R stays
open by construction while any row is outstanding: the Phase 13 exit
record (plans/os-d63c7441.md) closes only when every row is met, and
none are yet. `not measured` is the packet re-deriving the frontier
toward promotion, not an omission in it.

| row | measure | surface | status |
|---|---|---|---|
| R.1 | one-sentence intents become routed contracts whose draft acceptance specs survive human review: the dispatcher's re-triage rate over the shadow window and a human-review sample of the draft acceptance specs it filed | `report.json` lanes.dispatcher.retriage_rate; the sample recorded in this packet | not measured |
| R.2 | planner plan PRs pass human review above 80% unedited and implementers reach verdict-passed submissions on the happy path: lanes.planner.unedited_rate above 0.800 and the receipts' independence on the window's happy-path submissions | `report.json` lanes.planner.unedited_rate; the verdict records' receipts | not measured |
| R.3 | the verifier lane holds quality alone on low tiers and humans review only high-tier plans: the tiers' independence levels and the verifiers' calibration agreement over the window | `spec/tiers.md` levels per tier; the calibration harness's agreement | not measured |
| R.4 | every escalation is one packet, one question and one decision, a transcript dump filed as a defect: the shape of every escalation.raised payload in the window | the chain's escalation.raised payloads through `seed decision` | not measured |
| R.5 | the system runs unattended for a week on a real backlog with zero chain violations, zero lost updates, zero silent abandonments, zero guardrail breaches and zero unreserved spend: the five-bar audit over the real chain at the window's end | `seed ledger audit` over the shadow ledger (`simulate.Audit`) | not measured |
| R.6 | the flywheel demonstrably compounds over a quarter: chore-to-workflow conversions, the packet-resume rate and the cost per contract over the quarter after the self-hosting cutover | `report.json` flywheel and knowledge sections; the budget records | not measured |
| R.7 | a team that has never spoken to the authors adopts from the README in under an hour on its own forge and reaches a verifier-passed, human-reviewed PR the same day: the first external adoption after the distribution step | the adopting team's report, recorded in this packet | not measured |

## The divergence log

Empty until the first entry decision 0004 binds: the five-bar audit
over the real chain after the cutover, taken as the flip's last step
per `decisions/0007-no-day-7-wait.md` rather than at day 7, and any
entry a live shadow window run after the cutover would add. Each entry:
date, position, card, v1 state, ledger state, the reconciliation; for
the audit, date, position, the five bars and their counts.
