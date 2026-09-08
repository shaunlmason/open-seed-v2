# The flywheel: recurring shapes become workflows, through gates

> Charter: SEED-NEXT.md §12 ("Recurring trajectory shapes are detected
> from the ledger, drafted as deterministic workflows, validated in
> mock, proposed as PRs; the registry is protected, silent
> self-modification impossible. Conversion rate (recurring chores →
> merged workflows) is a tracked metric. Every chore an agent does
> twice becomes infrastructure — through gates"), Part III.K's flywheel
> row; [`docs/build-plan.md`](../docs/build-plan.md) Phase
> 11 item 5; [`plans/os-9075c308.md`](../plans/os-9075c308.md).

## A shape is record-derivable, and recurrence is counted, not judged

For every `done` subject the **shape** is the JCS form of `{routing,
acceptance_path, tier, sequence}`: the routing, tier and acceptance
path the fold carries, and `sequence` the subject's verbs in chain
order with positions, actors, payloads and instants dropped. Its id is
`s-<12 hex>` of that form. A shape is **recurring** when at least
`flywheel.RecurringAfter` done subjects fold to it, and the constant is
**`2`**: "every chore an agent does twice becomes infrastructure" is the
charter's figure, and the second done occurrence is the one that makes
it a chore. A drill pins the constant to this sentence as the tier
table is pinned to [`tiers.md`](tiers.md).

`seed flywheel shapes (--ledger <dir> | --remote <repo>)` lists every
shape with its occurrences (subject, the position of the merge
observation that made it done, the acceptance anchor, whether the
acceptance is gated) and whether it recurs.

Refused: detection from intent prose (similarity is a model's
judgment, and the record carries the structural fields the chore
actually repeats). Refused: detection from
[`trajectories.md`](trajectories.md)'s trajectories (a lane's decision
points; a chore is a contract's shape across lanes).

## The draft is a v1 workflow, deterministic, from the gated acceptance

> **Open item in this repository.** The target described below is a v1
> (open-seed) workflow, and this repository carries no `.seed` tree.
> The drafting and execution half therefore has no home here, and its
> drills skip; the detection half is unaffected. See
> [`../decisions/0008-flywheel-target.md`](../decisions/0008-flywheel-target.md).

`seed flywheel draft --shape <id> (--ledger|--remote) --repo <dir>
[--out <file>] [--validate]` writes a workflow named
`chore-<shape prefix>` in the v1 registry's schema
(`.seed/workflow-schema/workflow.schema.json`): `description` in the
schema's "Use when / NOT for" form from the routing and the acceptance
path; `inputs` **exactly the fields that vary across the occurrences**
(`goal`, the intent text; `anchor`, the acceptance commit); one `run`
step per validation command of the acceptance spec at its **gated**
anchor, in the spec's order, each command verbatim and each depending
on the previous; and one role step per judgment point the sequence
carries, lane to role (`plan.proposed` to `planner`, the claim span to
`implementer`, `verdict.rendered` to `reviewer` with the smoke
workflow's JSON verdict `output_format`), whose prompts are templates
over the inputs and the produced artifacts and nothing else, never the
intent text. Deterministic in both senses: every `run` is a command
the spec already ran, and two drafts of one shape are byte-identical
(a committed golden pins the bytes).

**Gated content only.** A shape with an occurrence whose acceptance is
not executable content behind review-gate evidence refuses `ungated`
(exit 19 `spec_unrunnable`): the drafter copies commands from gated
acceptance and from nowhere else.

**One command set per shape.** The shape cannot see the acceptance's
bytes (a digest in the shape would make `shapes` a repository read),
and occurrences that vary in `anchor` may carry different validation
commands at their gated anchors. The drafter reads the command list at
every occurrence's gated anchor and refuses `divergent` (exit 9
`classification_refused`), naming the two anchors whose lists differ
byte for byte, when they are not one list; the list every occurrence
ran is the draft's, so a workflow invoked with one anchor never runs
commands copied from another. Refused: a canonical pick among
differing lists (a chore whose gate changed is not one chore).

## Mock validation runs through the v1 engine, the one integration point

`--validate` stages the draft at its registry path,
`.seed/workflows/<name>.yaml`, in a detached worktree of the repository
(`git worktree add --detach`), because the engine resolves `workflow
run <name>` from the registry under the root it finds from the working
directory and takes no file argument; invokes `scripts/seed workflow
validate <staged file>` and `scripts/seed workflow run <name> --mock` as
subprocesses from inside that worktree (honoring `SEED_ENGINE`);
refuses the draft when either fails (exit 20 `checks_red`), naming the
stage, the failing step and the engine's finding verbatim, and the
owed act (`seed flywheel repair`); and removes the worktree
afterwards, run directory included. The caller's checkout is never
staged into and never gains a registry file. A name the registry
already holds at the repository's head refuses (exit 3
`invalid_transition`) before anything is staged.

The v1 surfaces touched: `scripts/seed workflow` invoked, the schema
read by a drill; nothing in v1 is modified. The engine-backed drills
run where the engine is (`make check` bootstraps it through
`validate.sh`; CI's `check` job) and, with neither `SEED_ENGINE` nor
the bootstrapped cache present (`flywheel.EngineAvailable`), skip **by
name** with the reason printed rather than passing; every next-side
derivation (the shape, the draft's bytes against the golden, the
proposal and merge rules, the report) is drilled with no engine, the
unprivileged run included. This is the one place the card's evidence
depends on a binary `next/` does not build, and the spec says so.

## The proposal is a PR and a fact, and the registry stays protected

`workflow.proposed` (a new `workflow.*` namespace, additive catalog
growth; `curate` alone, the proposal posture) is appended on the shape
id and carries `{"shape", "workflow": "<path @ commit>", "occurrences":
["<contract>@<done position>", …], "validated": {"run": "<mock run
id>"}, "repair"?: "<contract>@<verdict position>"}`. The boundary
holds it to the record: at least `RecurringAfter` distinct admitted
`done` occurrences, each folding to the named shape (recomputed at
admission), the path a file directly under `.seed/workflows/`, no
standing unmerged proposal for the shape, and the repair rule below;
each refusal names its gate (`proposal.shape`, `proposal.path`,
`proposal.occurrences`, `proposal.duplicate`, `proposal.repair_open`,
`proposal.repair`).

`seed flywheel propose --shape <id> (--ledger|--remote) --key <curate
key> --repo <dir>` drafts, validates, writes the validated draft on
branch `seed/flywheel-<shape>` through a temporary worktree (the
branch created from the repository's head or extended), appends the
fact citing the file at that commit, and prints the PR to open (head,
base, title); the PR is the harness's to open, as a plan PR is today.
**The tool never writes under `.seed/workflows/` on `main`**: the file
reaches the registry only through the PR the governance root reviews,
which is "silent self-modification impossible" as the tree already
enforces it for every protected path.

`workflow.merged` (`observer`, `operator`, the merge-observation row)
carries `{"workflow": "<path @ merged-commit>", "shape", "pr": "<pr @
merged-commit>"}` on the shape id and admits only over a standing
admitted proposal whose file it names (`merge.proposal`). The fold
binds a proposal or a merge only when it passed the boundary at its
own position, re-judged through the same checks, so a raw push binds
nothing and counts an anomaly.

## A failing step's repair is a bounded contract, and its patch is the PR

The charter's sentence, a role that completes within bounds and
proposes a patch as a PR, is met with what the tree already holds.
When the engine refuses a draft, `--validate` refuses as above,
appends nothing, and names the owed act. `seed flywheel repair --shape
<id> (--ledger|--remote) --key <dispatch key> --repo <dir>` re-drafts,
confirms the refusal, writes the draft and the repair acceptance on
`seed/flywheel-<shape>`, then files and specifies **one** contract the
way `seed eval file` does: `intent.filed` at tier `trivial` (no plan
and no seal for a workflow patch the governance root reviews at the
PR) with budget `small` (the bound: the class's capacity and the
one-run-per-window rule, so a repair that exhausts its window parks
like any contract and the shape stays unconverted) and the shape's
routing, and `contract.specified` citing
`next/flywheel/<shape>/accept.md @ <branch commit>`, executable and
gated at that commit, whose validation commands are exactly
`scripts/seed workflow validate .seed/workflows/<name>.yaml` and
`scripts/seed workflow run <name> --mock`, and whose prose quotes the
failing step and the engine's finding verbatim (content on the
branch, never in a payload). Under a key without `dispatch` the verb
refuses `out_of_grant` with nothing written or appended, reporting the
act as owed by the dispatcher (the `eval act` posture); a second
repair for a shape whose repair stands refuses.

The fold knows a repair contract by its acceptance path under
`transition.FlywheelRoot` (`next/flywheel`, the `EvalRoot` precedent),
which binds the subject to its shape from the record alone. The
implementer claims and works it as any contract, fixing the workflow
on the branch; the verifier's render runs the two commands in its
workspace clone, so the receipt reproduces the engine's answer.
`propose` on a shape whose repair contract is short of a passed
verdict refuses `proposal.repair_open`; once the verdict passes it
validates the branch's file **as it stands** (the staging above at the
branch's head, no regeneration over the fix) and admits citing the
contract in `repair`; the PR is the branch's, and its merge is observed
on the repair contract (`done`) and as `workflow.merged` alike.

Refused: retrying or editing the draft inside the drafter (silent
self-modification one step earlier); a repair marker on `intent.filed`
(the acceptance path already binds the subject, and a marker would
move the filing schema); a tier above `trivial` (a plan PR per failing
step is not "within bounds").

## The conversion rate is a report section

The report ([`projections.md`](projections.md)) gains `flywheel:
{recurring, proposed, merged, repairs: {filed, done},
conversion_rate}`: shapes recurring, shapes with an admitted proposal,
shapes with an admitted merge, the repair contracts filed and done,
and merged over recurring as a three-decimal string, null at zero;
the section null when no work subject exists. `seed flywheel status`
renders the same rows from the fold.

## Versioning

Two verbs under a new namespace are catalog growth (the `curation.*`
posture): no protocol bump, every existing chain verifies byte for
byte; the report version moves.

## Surfaces

- `seed flywheel shapes (--ledger <dir> | --remote <repo> [--ref
  <ref>] [--state <dir>])` — every shape, its occurrences, recurrence.
- `seed flywheel draft … --shape <id> --repo <dir> [--out <file>]
  [--validate]` — the draft (carried in the envelope without `--out`)
  and, validated, the mock run's id and steps.
- `seed flywheel propose … --key <path> --shape <id> --repo <dir>` —
  the branch write and the fact; the subject is the shape id, derived.
- `seed flywheel repair … --key <path> --shape <id> --repo <dir>` —
  the repair contract, under the dispatcher's key.
- `seed flywheel observe … --key <path> --shape <id> --merged <commit>
  --pr <pr>` — the merge observation, under the observer's key, citing
  the standing proposal's file at the merged commit.
- `seed flywheel status …` — the report's rows.

## Conformance mapping

- III.K row 8 ("the flywheel closes through gates: recurring shapes →
  drafted workflows → mock validation → PR; repair roles propose
  patches as PRs; conversion rate is tracked") and the Phase 11 exit
  line ("a real recurring chore in the fixture converts to a workflow
  through the gates"):
  the modes fixture's chore worked three times, its shape recurring at
  the second and the third adding an occurrence, the draft validated
  by the engine, proposed on its branch, observed merged, `1.000` in
  the report, the file absent from `main`; and with a break planted in
  one step, the mock run failing, the repair contract filed under the
  dispatcher's key, the implementer's fix passing its verdict, the
  proposal admitting citing it, the one merge closing both.
