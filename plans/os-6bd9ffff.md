# Plan: next — Phase 10 item 5, trajectory-prefix regression harness for lane decision points; dispatcher re-triage rate and planner unedited-approval rate (os-6bd9ffff)

The build plan's Phase 10 item 5: *"Trajectory-prefix regression
harness for lane decision points; dispatcher re-triage rate and
planner unedited-approval rate tracked (III.J row 3, routed here by
the Phase 9 exit record: both are lane-quality metrics that are
meaningless without this harness)."* The charter's definitions: §16,
*"Trajectory-prefix regression for lane behavior: recorded decision
points (e.g., 'about to declare done without running checks') replay
against lane configurations to catch behavioral regressions in role
or prompt changes"*; §9, the dispatcher *"converts intents and mirror
requests into routed contracts with draft acceptance specs"* and the
planner's *"unedited-approval rate tracked (a wrong decomposition
poisons everything downstream)"*; III.J row 3, *"Dispatcher re-triage
rate and planner unedited-approval rate are tracked"*; III.O row 3,
*"Trajectory-prefix regression covers lane decision points"* (its
simulation-mode half is Phase 13's). The Phase 9 exit record routed
row 3's metrics here and said plainly that nothing in the tree
computes either rate.

This plan is written against the `seed/4` register entry items 3 and 4
create (#223, #225) and depends on whichever of them lands first for
the version gate; it touches nothing else those cards touch.

## What the tree actually shows

Measured, not assumed:

- **Decision points already have a record, in two places.** Every act
  a lane performs is a signed ledger record at a position, and every
  refused attempt is a position-stamped line in the attempts journal
  (`internal/refusals`: instant, position, actor, verb, subject,
  outcome, code), written beside the ledger by every boundary verb.
  The stamp is a tip ordinal, and it means two different things by
  outcome: an admitted record is stamped at its own position, while a
  refusal is stamped at the last record of the view the boundary
  judged it against (`stampTip(view.Count)`), so the chain the lane
  saw is `records[:p]` for the one and `records[:p+1]` for the other.
  The worker loop (`internal/loop.Step`) is a fixed sequence of named
  decisions (poll, orient, claim take, reserve, work, settle, submit;
  park on any refusal), so each act is a decision point with a step
  name; the loop's own observation lines are liveness, unsigned and
  lossy by declaration, and nothing downstream may read them for a
  decision.
- **The frame a lane decided from is derivable at any prefix.**
  `admit.ContextAt` over `records[:n]`, `admit.Affordances` (the
  boundary's own probe pipeline, deterministic given the chain: no
  admission rule reads a wall clock), the subject's folded state and
  the obligation rows owed to the actor (`internal/obligation`, the
  situation read's `owedToMe`). The affordance walk already replays
  one scenario at every prefix (`walkScript`, the regression-class
  sweep); nothing records a real lane's trajectory or replays one
  against a manifest.
- **A lane configuration is a manifest plus its resolved fragments.**
  `next/lanes/<lane>.json` (`grants`, `acts_through`, `liveness_from`)
  and `lane.Resolve` over `fragments/`; `seed lane validate` checks
  them against the tables, never against behavior.
- **Neither metric has a record to read.** `contract.specified` admits
  from `backlog` only (the table's one row), so a dispatcher that got
  a triage wrong has no act that revises it; `plan.proposed` and
  `plan.approved` carry anchors (`<path> @ <commit>`) whose commits
  differ across a squash merge even when the content is identical, so
  "unedited" is not derivable from them. `seed plan` has `lint` and
  `classify` and no verb that appends either fact.
- **The report projection is the record-derivable metrics surface**
  (version "10": the refusals section over the declared journal,
  reconciliation by class, the observation section), and projection
  builds never read the repository or the artifact store.
- **There is no model under `next/`.** The injection suite's posture
  (`lanes.md`: the suite asserts that believing the text changes
  nothing, and names exactly where that is false) is the honest frame
  for any harness that claims to cover "behavior".

## Design decisions (binding for this task)

- **D1 — a trajectory is what the record already says a lane did, at
  the frame it decided from.** `internal/trajectory` (new).
  `Point{position, verb, act, subject, outcome, code, frame}`, where
  `outcome` is `admitted` (a signed record) or `refused` (a journal
  line), `act` the loop-act spelling `internal/loopverb` gives the
  verb when it has one, and `Frame{state, affordances, owed}` is the
  subject's folded state, the actor's affordances on the subject and
  the obligation kinds owed to the actor there, all at the prefix
  before the point. `Trajectory{lane, actor, manifest, posture,
  points}` is JCS-canonical, `manifest` the sha256 of the manifest
  bytes and `posture` the sha256 of the resolved fragments.
  `Record(records, journal, key, lanesDir, lane)` takes the actor's
  admitted records in position order and its refused journal entries
  at their stamped positions, and derives each frame from the prefix
  the lane actually saw: `ContextAt(records[:p])` for an admitted
  record at `p` (it was judged against everything before it), and
  `ContextAt(records[:p+1])` for a refusal stamped `p` (the stamp is
  the last record of the view it was judged against, so a refusal
  stamped at the chain's last record sees the whole chain). A journal
  line at a position beyond the tip, or from another actor, is skipped
  and counted. The frame carries no instant, so two recordings of one
  chain are byte-identical.

  Refused: recording from the observation stream (unsigned, lossy,
  liveness only). Refused: a recorder hook inside `internal/loop`: the
  loop stays a library that owns nothing but the work step, and the
  record and the journal are already written by every boundary verb.

- **D2 — replay is a frame diff plus a configuration check, and it
  says what it cannot see.** `Replay(t, records, key, lanesDir)`
  classifies every point exactly once: `same`; `frame_changed` (state,
  affordances or owed differ: the boundary or the fold changed under
  the lane); `act_undeclared` (the manifest's `acts_through` no longer
  names the point's act); `act_ungranted` (the manifest's grants no
  longer intersect the verb's accepted capabilities); `act_inadmissible`
  (an admitted point's verb is absent from the recomputed affordances).
  Beside the points it classifies the configuration once per
  trajectory: **`manifest_changed`** (the manifest bytes' digest
  differs from the recorded one, which catches `orients_from`,
  `liveness_from`, `inbox`, `summary` and the fragment list, the
  fields no point-level class reads) and **`posture_changed`** (the
  resolved fragments' digest differs). `seed trajectory replay <file>
  --ledger <dir> --key <key> --lanes <dir>` exits 0 iff every point is
  `same` and both configuration digests match, and otherwise exit 26
  `lane_invalid` refining **`trajectory_diverged`**, naming each
  divergent point and class and each changed digest. A lane replays its own trajectory
  with its own key, because a fingerprint alone cannot probe the
  boundary (`admit.Affordances`).

  `manifest_changed` and `posture_changed` are divergences,
  deliberately: a manifest or fragment edit that touches a lane with N
  recorded decision points fails the drill until the corpus is
  re-recorded on purpose, which is what "catch behavioral
  regressions in role or prompt changes" can mean in a tree with no
  model. The residual is stated in `trajectories.md` in the injection
  suite's words: no decider re-runs at a point, so replay proves that
  the configuration still presents the same frame and still permits
  the same act, not that a model would choose it; Phase 13's
  simulation mode is the seam where a decider plugs in.

- **D3 — the corpus covers every shipped manifest, recorded from a
  scenario that drives each one.** `next/trajectories/lanes/<lane>.json`,
  one file per manifest in `next/lanes/*.json`, the set derived from
  the directory and never from a hand list. The small-team fixture
  provisions only the implementer and the verifier, so a dedicated
  recorder scenario in `cmd/seed` drives every lane and role through
  its own acts: the dispatcher's filings and one re-specification
  (D4), the planner's claim, proposal and release, the implementer's
  loop through a submission and a park, the verifier's pass and fail,
  the supervisor's offer and run start, the observer's merge
  observation, the maintenance actor's reap and filing, each with at
  least one refused attempt so the journal arm is exercised. A lane
  with no act in the tree (the curator until its proposal grant lands)
  records an empty trajectory whose manifest and posture digests are
  still recorded, so a change to its configuration still diverges
  (configuration-only coverage, named as such in the drill's output
  rather than silently accepted). The recorder drill rebuilds the
  scenario, records, and compares byte for byte to the committed
  corpus (`-update` re-records); the replay drill replays every file
  against `next/lanes` over the rebuilt chain and requires every point
  `same` and every digest equal; planted rows prove the classes (a
  manifest without `submission make`, a manifest without `claim`, a
  manifest whose `orients_from` alone changed, a fragment with one
  added line, a chain with one extra record before a point, a boundary
  that no longer affords a recorded act). Determinism rests on the
  frame carrying no instant and the scenario fixing positions and
  verbs.

- **D4 — re-specification: `contract.specified` gains the `ready`
  origin at `seed/4`.** The row becomes `{"verb": "contract.specified",
  "from": ["backlog", "ready"], "to": "ready"}` in `transitions.json`
  and the mirrored `table.json`. The lifecycle rule refuses the `ready`
  origin before `seed/4` (naming the version: re-specification
  activates at `seed/4`), the fold applies a re-specification's
  acceptance at `seed/4` positions only, and the shape rule is the
  first specification's (structured acceptance, gate evidence for
  executable content). Every other state refuses by the table, so a
  claimed, reviewed, blocked or finished contract is out of reach; a
  `claim` key refuses `out_of_grant`. `SubjectState.Specifications`
  counts admitted specifications. Re-triage is the dispatcher revising
  its own triage, and the record now carries it. Not revised: `tier`,
  `budget` and `routing` stay the intent's (tier provenance is the
  named owner of that residual). The injection suite's residual table
  gains the row: a persuaded dispatcher can re-specify an unclaimed
  contract's draft; the spec gate still binds executable content, and
  the reachability drill derives the widening rather than being told.

- **D5 — the plan verbs carry a content digest at `seed/4`.**
  `plan.proposed` becomes `{"plan", "digest"}` and `plan.approved`
  `{"plan", "pr", "digest"}`, `digest` the sha256 of the plan bytes at
  the anchor (the figure `seed plan lint` prints), REQUIRED from
  `seed/4` and refused before it. New `seed plan propose --plan <path
  @ commit> --repo <dir>` and `seed plan approve --plan … --pr … --repo
  <dir>` derive the digest from the repository at the anchor and
  refuse an anchor it lacks. The fold keeps, per subject, the FIRST
  admitted proposal's digest and the approval's. An approval is
  **unedited** iff its digest equals the first proposal's: the
  planner's original decomposition survived review, by anyone, planner
  included, because the charter's figure is "plan-PRs pass human review
  unedited". An approval before `seed/4` is `unmeasured`, never
  guessed.

- **D6 — the report gains a `lanes` section, version "11".**
  `{"dispatcher": {"specified", "respecified", "retriage_rate"},
  "planner": {"approvals", "unedited", "edited", "unmeasured",
  "unedited_rate"}}`: `respecified` counts subjects with two or more
  admitted specifications over subjects with one or more; `unedited`
  and `edited` over measured approvals; rates as three-decimal strings,
  null at a zero denominator; the section null when no work subject
  exists (the reconciliation section's posture). Record-derivable from
  the fold alone; `seed project build` publishes it and no new
  projection is registered.

- **D7 — versioning.** The `ready` origin and the plan digests
  activate at `seed/4` behind the gate item 3 names
  (`version.LevelsApply`, created here if this card lands first and
  documented as what `seed/4` added); a `seed/3` validator refuses
  each at its position by version, and every existing chain verifies
  byte for byte. The table change is recorded in `lifecycle.md` beside
  the version rule that gates it.

- **D8 — scope guard.** No model call, no decider, no simulation mode
  (Phase 13); no change to `internal/loop`; the recorder never reads
  the observation stream; no forge read anywhere; no new projection;
  no revision of `tier`, `budget` or `routing`; no change to the
  escalation channel; the shipped manifests and fragments are
  unchanged (the harness reads them); no new exit code.

## Steps

0. `next/internal/version/` — the `seed/4` gate shared with items 3
   and 4.
1. `next/internal/transition/` — the row in `table.json`;
   `Specifications`; the re-specification fold at `seed/4`; the first
   proposal's and the approval's digests in the fold;
   `CheckPlanEventShape` taking the version.
2. `next/internal/admit/` — the `ready`-origin version gate in the
   lifecycle rule; the digest shape at `seed/4`; the residual entry in
   `testdata/injection/residuals.json`.
3. `next/internal/trajectory/` (new) — `Point`, `Frame`,
   `Trajectory`, `Record`, `Replay`, the six classes.
4. `next/internal/project/` — `ReportLanes`; version "11".
5. `next/internal/envelope/` — `trajectory_diverged` under 26.
6. `next/cmd/seed/trajectory.go` (new) — `record`, `replay`;
   `next/cmd/seed/plan.go` — `propose`, `approve`; `main.go`.
7. `next/cmd/seed/trajectory_e2e_test.go` (new) — the recorder
   scenario driving every manifest; `next/trajectories/lanes/*.json`
   (new) — the corpus, one file per shipped manifest.
8. Drills: transition (the row both ways, the fold at and before
   `seed/4`, the digests), admit (re-specification at and before
   `seed/4`, on every non-ready state, from a `claim` key; digests
   refused before and required at `seed/4`; the reachability drill
   green with the new residual), trajectory (a scenario with admitted
   and refused points, every class, two recordings byte-identical,
   the two prefix rules, the skipped-and-counted lines), project (the
   section, the rates, null, the version), `cmd/seed` (record and
   replay envelopes, the exit 26 refinement, `plan propose|approve`,
   the corpus completeness against the lanes directory, the corpus
   drills and the planted rows).
9. Specs: new `next/spec/trajectories.md`; `lifecycle.md` (the row and
   its gate), `transitions.json`, `plans.md` (the digest, unedited),
   `projections.md` (the section), `lanes.md` (III.J row 3 in the
   conformance mapping; the residual table), `protocol.md` (the
   `seed/4` entry's text), `envelope.md` (the refinement row),
   `refusals.md` (the journal's second reader).
10. `next/docs/progress.md` (10.5; the III.J row 3 line), `next/docs/
    decisions.md`, `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/version/**`, `next/internal/transition/**`,
  `next/internal/admit/**`, `next/internal/trajectory/**` (new),
  `next/internal/project/**`, `next/internal/envelope/**`,
  `next/cmd/seed/trajectory.go` (new), `next/cmd/seed/plan.go`,
  `next/cmd/seed/main.go` and their drills,
  `next/cmd/seed/trajectory_e2e_test.go` (new: the recorder scenario
  and the corpus drills), `next/trajectories/**` (new)
- `next/spec/trajectories.md` (new), `next/spec/lifecycle.md`,
  `next/spec/transitions.json`, `next/spec/plans.md`,
  `next/spec/projections.md`, `next/spec/lanes.md`,
  `next/spec/protocol.md`, `next/spec/envelope.md`,
  `next/spec/refusals.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-6bd9ffff.json`

Nothing outside `next/**` except the work-product files above. NOT
`next/internal/loop/**`, NOT `next/internal/obs/**`, NOT
`next/lanes/**`, NOT `next/internal/escalation/**`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **Record.** On a chain where one lane's key signs N records and
   journals M refusals, the trajectory has N+M points in position
   order; each frame equals the boundary's own derivation at the
   prefix (affordances equal `admit.Affordances` there, state the
   fold's, owed the situation's rows); an admitted record's frame is
   derived at `records[:p]` and a refusal's at `records[:p+1]`, so a
   refusal stamped at the chain's last record sees the whole chain and
   the first record's frame is the empty chain; a journal line beyond
   the tip, or from another actor, is skipped and counted; the
   manifest and posture digests equal those of the shipped files; two
   recordings are byte-identical.
2. **Replay.** Unchanged configuration and chain: every point `same`,
   exit 0. A manifest whose `acts_through` drops the recorded act:
   `act_undeclared`; grants dropped: `act_ungranted`; a record inserted
   before a point: `frame_changed`; a fragment edited: `posture_changed`;
   a manifest whose `orients_from` alone changed: `manifest_changed`
   with every point still `same`; an admitted point whose verb the
   recomputed affordances lack: `act_inadmissible`. Any divergence
   exits 26 with refining code `trajectory_diverged` naming each
   divergent point and class and each changed digest.
3. **Corpus.** `next/trajectories/lanes/` holds exactly one file per
   manifest in `next/lanes/*.json`, the set derived from the
   directory; every lane and role with an act in the tree has at
   least one admitted and one refused point, and a lane with none is
   reported as configuration-only; the recorder drill rebuilds the
   scenario and reproduces every file byte for byte; the replay drill
   replays every file against `next/lanes` with every point `same` and
   both digests equal; the planted-manifest, planted-`orients_from`
   and planted-fragment rows fail it with the named classes, for a
   configuration-only lane included.
4. **Re-specification.** At `seed/4` a `dispatch` key re-specifies a
   `ready` contract: the new acceptance folds, `Specifications` reads
   2; before `seed/4` it refuses naming the version; on `in_progress`,
   `review`, `blocked` and `done` it refuses by the table; from a
   `claim` key `out_of_grant`; the residual entry exists and the
   reachability drill is green.
5. **Plan digests.** At `seed/4` a proposal or approval without
   `digest` is incomplete; before `seed/4` one carrying it refuses
   naming the version; `seed plan propose|approve` derive the digest
   from the anchor and refuse an anchor the repository lacks; the
   fold keeps the first proposal's digest across a second proposal.
6. **Report.** On a chain with four specified contracts of which one
   was re-specified, and three approvals of which one is unedited, one
   edited and one pre-`seed/4`: `retriage_rate` "0.250",
   `unedited_rate` "0.500", `unmeasured` 1; null at no work subject;
   version "11" republishes existing prefixes.
7. **Mutation evidence.** Each must fail a drill: the recorder skipping
   refused attempts; a refusal's prefix sliced like an admitted
   record's; the frame omitting the affordances; the frame carrying an
   instant; replay ignoring the posture digest; replay ignoring the
   manifest digest; replay ignoring `acts_through`; replay ignoring
   the grants; the corpus completeness read from a hand list of
   lanes; the `ready`
   origin admitted before `seed/4`; the fold applying a
   re-specification before `seed/4`; `Specifications` counting the
   first specification only; the digest accepted before `seed/4`; the
   digest optional at `seed/4`; unedited compared to the LAST proposal;
   the report version not bumped; the residual entry removed.
8. `make check` green with coverage measured **cold**, at least three
   readings above the gate, and the suites pass **unprivileged** under
   `setpriv --reuid=65534 --regid=65534 --clear-groups`.

**Retention set (existing, shown unharmed):**

- Every pre-existing fixture chain verifies byte for byte; every other
  row of the table is unchanged; the loop's drills, the observation
  channel and `seed lane validate` are untouched.
- The injection suite's dispatcher reachable set gains exactly the
  `ready`-origin specification and nothing else; every existing
  residual keeps its characterization drill.
- The report's existing sections are byte-identical on chains that
  carry no re-specification and no digest; `seed plan lint` and
  `classify` are unchanged; the shipped manifests and fragments are
  unchanged.

## Validation Commands

- Boundary: `cd next && go test ./internal/trajectory/ ./internal/transition/ ./internal/admit/ ./internal/project/ ./internal/envelope/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

One new package with three types, a recorder with its two prefix
rules, a replayer with five point classes and two configuration
classes; one table row with its version gate and fold; one counter and
two digests on the fold; one report section and a version bump; one
refinement code; two CLI verbs and two plan subverbs; one committed
corpus of one file per shipped manifest with its recorder scenario
and replay drills; one residual entry. Specs:
one new file and eight edits. No new exit, projection or manifest
change; no `plans/**` in the task PR.
