# os-0d4f2af3 — Phase 12 item 4: the preseed, agent-only guardrails, and the protected surface in config

Build plan Phase 12 item 4: one declarative file bootstraps a new
adoption — config, guardrails, teams, protections desired-state and the
declared admission posture — idempotently and CI-verifiably (charter
§II.17 "Preseed", Appendix D.1, III.P row 3). The Phase 10 exit record
routes three more rows here: the guardrails include agent-only ones read
off the roster's `kind` and the report's lane rates split by kind (III.E
row 9); the config enumerates the protected surface and names the
governance root and its change process (III.G row 9, III.L row 2); and
III.L row 1 (tiers gate un-planned action per squad and per path) is
the row the guardrails block answers.

## What the tree actually shows

- **The declaration exists and three PRs are growing it.** `seed.json`
  at the repository root is `posture.Config` (`{posture}`), read by the
  doctor and, since #241, by the hook from the default branch's tip
  (`protected: [...]`); #244 adds `admission` and #246 adds
  `checkpoints`. Every extension decodes strictly and refuses unknown
  fields. The preseed is therefore not a new file: it is this one,
  completed.
- **Genesis takes a key and nothing else.** `seed init --ledger <dir>
  --key <operator key>` writes a genesis naming that key as the
  governance root at `seed/0`; the later protocol versions
  (`version.Activated` lists `seed/1`–`seed/4`) are activated by
  operator events the fixtures append by hand.
- **Teams are the deployment's, and nothing declares them.**
  `intent.filed`'s `routing` "names a squad, squads are the
  deployment's, and no table in `next/**` can know them", a residual
  `tiers.md` and `lanes.md` name. Lanes are declared (`next/lanes/*.json`,
  eight manifests: six lanes and two roles) and validated by
  `internal/lane`.
- **Tiers are a vocabulary, not a guardrail.** `tiers.md` declares
  `trivial`, `standard`, `critical` with plan/sealed/human-review/
  independence columns; nothing limits what tier an actor may claim,
  and nothing reads the roster's `kind` (III.E row 9's unmet half).
- **The protected surface is partly enumerated, nowhere declared
  complete.** #241's `protected` is a list the hook enforces on the
  default branch; no spec says what MUST be in it, no check refuses an
  omission, and the governance root is named only by genesis, its
  change process by nothing.
- **v1's shapes are the predecessor's, not a template.** `.seed/config.toml`
  (operators roster, backend), `.seed/guardrails.yaml` (autonomy tiers
  `L1`–`L3`, protected paths, budgets) show what a deployment needed to
  say; the vocabulary is Seed's.

## Design decisions (binding for this task)

**D1. The preseed is `seed.json`, completed — one file, one struct.**
`posture.Config` gains four blocks beside `posture`, `admission`,
`protected` and `checkpoints`: `protocol` (the version the deployment
activates through, `seed/4` today), `governance` (`root`: the root
key's fingerprint or public key; `owners`: the forge identities that
review the protected surface; `change_process`: the fixed value
`pr+owner-review`, the only process the charter names), `guardrails`
(D3) and `teams` (D4). Strict decoding as today; the spec (`postures.md`
gains a "Declaration" section, or `next/spec/preseed.md` if #244 has
not merged first — the plan names both and the task PR takes whichever
avoids a conflict) is the schema of record. Refused: a second file
beside `seed.json`; a YAML dialect — the declaration is JSON because
the hook and the doctor already parse it strictly.

**D2. `seed init --preseed` is idempotent, and drift refuses.** The
first run writes genesis naming `governance.root`, activates the
protocol versions up to `protocol` in order under the root key, and
prints what it appended. A second run appends nothing and exits 0 with
`unchanged: true`. A preseed that disagrees with the chain it meets —
a different root, a lower protocol than the active one, a posture the
chain's history contradicts — refuses `preseed_drift` — a refinement of exit 28 `drift`, the code
#244 allocates for a declared state and an observed one that differ —
naming the field and both values; init never edits history to match a
file. `seed preseed check --ledger <dir>
--config seed.json` is the same comparison with no writes, exit 0 or
`preseed_drift`, and `make check-next` runs it against the fixture
deployment so the file is CI-verified; the doctor reports the
declaration's blocks. Refused: defaults filled in for absent blocks —
an absent `guardrails` or `teams` block means undeclared, reported as
such, never a silent permissive default.

**D3. Guardrails: tiers per squad and per path, and the agent-only
ceiling that reads `kind`.** `guardrails: {squads: {<squad>: {default:
<tier>, max_agent: <tier>}}, paths: [{prefix, min: <tier>}]}` over the
`tiers.md` vocabulary, validated byte for byte as `tier` is at filing.
Two consumers, both real: (a) **the claim ceiling at admission** — a
`claim.taken` signed by a key whose roster `kind` is `agent` on a
contract whose tier is above its squad's `max_agent` refuses at exit 3
with the refinement `tier_above_ceiling`, naming the kind, the squad,
the tier and the ceiling; a `human` key is not ceilinged (the charter's
"agent-only guardrails read the distinction"), and `service` is
treated as `agent`. The check reads the declaration as admission
policy, not chain validity (`tiers.md`'s precedent, and #241's
`protected`): the cooperative client reads `seed.json` from its
working tree, the hook reads it at the default branch's tip, and a
chain never changes meaning or fails verification because of it. (b)
**the path floor at the plan gate** — `seed plan lint --config
seed.json` refuses a plan whose file scope touches a `paths` prefix
while its card's tier is below the prefix's `min`, and the verifier's
render reads the same floor and refuses `under_tiered` (exit 18's
family: content whose gate is short). Refused: a guardrail nobody
enforces; a ceiling that reads the signer's lane rather than its
`kind`.

**D4. Teams close the routing residual.** `teams: {squads: [{name,
lanes: [<manifest names>]}]}`; a manifest name must exist under
`next/lanes/` (`lane.Load` is the authority), and `intent.filed`'s
`routing` is validated against the declared squad names at admission
under the same policy-not-validity rule — the residual `tiers.md` names
closes, and `lanes.md` says so. Refused: keys in the preseed — enrollment
stays the operator's signed act after init (Appendix D.1's order), and
a file that carried keys would be a roster nothing signed.

**D5. The protected surface is enumerated in config, and the spec says
what complete means.** `protected` (#241's list) stays the field; the
spec lists the members the charter requires, as concrete prefixes the
check compares by string: `next/spec/` (the transition spec and every
normative table), `next/internal/admit/`, `next/internal/transition/`,
`next/internal/keyring/` (the standing and capability rules),
`next/internal/verdict/`, `next/internal/seal/`, `next/internal/eval/`,
`next/evals/` (verifier code, rubrics and the sealed-check machinery),
`next/internal/curation/`, `next/knowledge/lessons/` (the curator's
gates and the policy stage), `next/lanes/` (role definitions),
`next/cmd/seed-admit/`, `next/cmd/covergate/`, `Makefile`,
`.github/workflows/`, `scripts/` (the check pipeline's own
definitions), and `seed.json` itself (the supervisor's policy lives in
its `guardrails` block; the sealed keyring is a recipient set derived
from ledger grants and has no path). `preseed check` refuses a
declaration whose list omits any of them with `preseed_incomplete`, a
refinement of exit 13 `posture_invalid` (a judgment on the
declaration's content, distinct from drift against a chain), naming the
missing member. `governance.root` must be the chain's genesis root or
a later admitted root; `owners` render into the `CODEOWNERS` #244
writes. **The capability audit in CI** is a drill that derives, from
the shipped manifests and the keyring rules, that no lane or role grant
reaches the protected surface. The shipped set already breaks the
naive form of that claim: `next/lanes/maintenance.json` grants
`operator`, and #241's code-ref rule lets operator standing
fast-forward the default branch — so a maintenance key, an agent's,
could write the surface. The audit therefore requires, and this card
lands, the refinement of #241's rule that the charter's sentence
("changed only by the governance root") states: a default-branch
update touching a `protected` prefix is admitted for **root standing**
only (a genesis-named root or a key the keyring marks root), and
operator standing suffices for the rest of the default branch. With
that rule the audit's claim is derivable: no manifest grants root, the
one manifest granting `operator` (maintenance) is named in the audit's
output as the operator-holding role that the rule keeps off the
surface, and a manifest that later grants root fails by name. If #241's
implementation lands the refinement first, this card inherits it; the
plan says so. The
test-content residual (ordinary test content outside the surface stays
in an implementer's write scope; diff-vs-plan review and sealed checks
are the mitigations) is written in the spec in those words.

**D6. Human/agent metrics.** The report's `lanes` section (version 13)
gains `by_kind`: the re-triage and unedited-approval rates computed
per signer kind from the roster the fold already folds, so an operator
can see whether the agents or the humans are the ones re-triaging;
report version bumps (to 15: #240 took 14 for the `flywheel` section)
and the cache republishes under its rule.
The refusal-rate section is unchanged (an affordance-gap metric, not a
lane one).

**D7. Bounds.** No new capability, verb or protocol version. The
ceiling applies at `claim.taken` only (offers and plans are upstream of
a claim and the claim is where an agent takes authority). Per-path
floors are enforced at the plan lint and the render, not at admission,
because a path is a fact about a repository and admission reads the
ledger and the declaration alone; the spec says so. Budget classes
stay `budgets.md`'s table; the preseed does not redeclare them.

## Steps

1. `internal/posture`: the four blocks, strict; `Declaration` helpers
   the boundary and the doctor share.
2. `internal/admit`: the claim ceiling and the routing check as
   declaration-driven policy rules (a nil declaration means no check,
   the pre-preseed behavior every existing drill runs under).
3. `cmd/seed/main.go` (`init --preseed`, idempotence, drift),
   `cmd/seed/preseed.go` (new: `preseed check`), `cmd/seed/doctor.go`.
4. `internal/plan` / `cmd/seed/plan.go`: the path floor at lint;
   `cmd/seed/verdict.go`: `under_tiered`.
5. `internal/project`: `by_kind` in the report's `lanes` section
   (version 15).
6. The capability audit drill; the idempotence, drift, ceiling
   (agent refused, human admitted, service as agent), routing, path
   floor, and `by_kind` drills; mutation evidence.
7. Specs (`postures.md` or `preseed.md`, `tiers.md`, `lanes.md`,
   `plans.md`, `projections.md`, `envelope.md` for `preseed_drift` under
   exit 28, `preseed_incomplete` under 13, `tier_above_ceiling` under 3,
   `under_tiered` under 18), `next/docs/progress.md`,
   `next/docs/decisions.md`, `memory/LEARNINGS.md`, the Makefile
   `check-next` line for `preseed check`; receipt; evidence; review.

## File Scope

- `next/internal/posture/**`, `next/internal/admit/**`,
  `next/internal/plan/**`, `next/internal/project/**`,
  `next/internal/lane/**` (the manifest-name lookup only)
- `next/cmd/seed/main.go`, `next/cmd/seed/preseed.go` (new),
  `next/cmd/seed/doctor.go`, `next/cmd/seed/plan.go`,
  `next/cmd/seed/verdict.go`, and drills
- `next/fixtures/**` (the fixture deployment's `seed.json`)
- `Makefile` (the one `check-next` line)
- `next/spec/postures.md` or `next/spec/preseed.md`, `next/spec/tiers.md`,
  `next/spec/lanes.md`, `next/spec/plans.md`, `next/spec/projections.md`,
  `next/spec/envelope.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-0d4f2af3.json`

Nothing outside `next/**` except the Makefile line and the work-product
files above. NOT `.seed/**`, NOT `scripts/**`, NOT
`.github/workflows/**`, NOT `next/spec/transitions.json`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **One file bootstraps a deployment.** `seed init --preseed` on an
   empty ledger writes genesis naming the declared root and activates
   the declared protocol; the doctor reports every block; `preseed
   check` exits 0.
2. **Idempotent, and drift refuses.** A second `init --preseed` appends
   nothing (`unchanged: true`); a different root, a lower protocol, and
   a contradicted posture each refuse `preseed_drift` naming the field
   and both values, with the chain untouched; `preseed check` reports
   the same; `make check-next` runs it on the fixture deployment.
3. **The agent ceiling reads `kind`.** An agent key claiming above its
   squad's `max_agent` refuses `tier_above_ceiling` at the boundary, the
   hook and the CLI; a human key claims the same contract; a service
   key is ceilinged; with no `guardrails` block nothing is ceilinged
   and the doctor says the block is undeclared.
4. **Routing is declared.** `intent.filed` with a `routing` outside
   `teams.squads` refuses under the declaration and admits without one;
   a squad naming a manifest that does not exist refuses at `preseed
   check`.
5. **The path floor.** `plan lint --config` refuses a plan touching a
   floored prefix below its tier; the render refuses `under_tiered`
   on the same contract; both admit at or above the floor.
6. **The protected surface is complete or refused.** `preseed check`
   refuses a `protected` list missing a required member
   (`preseed_incomplete`, naming it); a default-branch update touching
   a protected prefix refuses at the hook for operator standing and
   admits for root standing; the capability audit drill passes on the
   shipped manifests, names `maintenance` as the operator-holding role
   the rule keeps off the surface, and fails on a planted manifest
   granting root.
7. **`by_kind` rates** appear in report version 15, null over nothing,
   and split the existing totals exactly.
8. **Mutation evidence.** Each fails a drill: a permissive default for
   an absent block; init editing history to match the file; the ceiling
   reading the lane instead of `kind`; the human ceilinged; routing
   validated as chain validity (an existing chain failing verification);
   a required member dropped from the spec's list; `by_kind` summing
   to something other than the total.
9. `make check` green with coverage measured cold, three readings above
   the gate; the suites pass unprivileged; no model identifiers in any
   committed artifact.

**Retention set (existing, shown unharmed):**

- Every existing drill runs with no declaration and passes unchanged:
  a nil declaration is exactly today's behavior at the boundary, the
  hook, the lint and the render.
- Every pre-existing chain verifies byte for byte; no new verb,
  transition row, capability or protocol version; projections other
  than the report are byte-identical, and the report's version bump
  follows `projections.md`'s rule.
- `seed init` without `--preseed` behaves as before.

## Validation Commands

- Boundary: `cd next && go test ./internal/posture/ ./internal/admit/ ./internal/plan/ ./internal/project/ ./cmd/seed/ ./cmd/seed-admit/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

New: `next/cmd/seed/preseed.go`, the fixture deployment's `seed.json`,
possibly `next/spec/preseed.md`. Modified: `next/internal/posture/`,
`next/internal/admit/` (two declaration-driven rules),
`next/internal/plan/`, `next/internal/project/` (report version 15),
`next/cmd/seed/main.go`, `doctor.go`, `plan.go`, `verdict.go`, one
`Makefile` line, six specs, the three docs files, the receipt. No
`.seed/`, `scripts/`, workflow or transition-table change.
