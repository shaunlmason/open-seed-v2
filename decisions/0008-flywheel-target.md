# 0008: the flywheel's conversion target is an open item in the successor repository

Status: recorded 2026-09-08, at the extraction of this repository from
open-seed. Scope: `internal/flywheel` and `spec/flywheel.md`. Records a
known gap; decides nothing about how it closes.

## Context

The flywheel converts a recurring chore into a deterministic workflow,
and the charter counts conversions as a compounding metric (III.O).
Its **draft target is explicitly a v1 workflow**: `spec/flywheel.md`
says so by name, drafts are written under `.seed/workflows`, validated
against v1's `.seed/workflow-schema/workflow.schema.json`, and executed
by the pinned engine binary the v1 shim resolves from
`.seed/engine.lock` (`flywheel.RegistryDir`, `flywheel.EnginePath`).

That was coherent while Seed incubated inside open-seed, where a v1
tree was always one directory up. This repository is the successor
standing on its own and carries no `.seed` tree, so the drills that
instantiate one cannot run here.

## Decision

The gap is recorded, not papered over. The four drills that need a v1
tree (`TestFlywheelShapesDraftAndStatusAtTheCLI`,
`TestFlywheelProposeWritesTheBranchAndObserveConverts`,
`TestFlywheelRepairFromThePlantedBreakToTheCitedProposal`,
`TestSmallTeamChoreWorkedThreeTimesConverts`) **skip with a reason
naming this record** rather than being deleted or made to pass against
a vendored copy of v1.

Vendoring v1's schema and engine into this tree was rejected: the whole
point of the extraction is that the successor does not depend on the
predecessor, and a vendored `.seed` would reintroduce exactly the
dependency the move removes.

## What is and is not affected

- **Unaffected:** the detection half. Reading recurring shapes from the
  ledger, the chore metrics, the conversion rate the report derives,
  and everything in `internal/flywheel` that does not resolve an engine
  path, all still run and are still covered.
- **Affected:** the drafting and execution half, from `flywheel draft`
  through the propose-and-observe conversion, which has no target in
  this repository.

## What closing it needs

A Seed-native workflow surface: a schema this repository owns, a
registry path that is not `.seed/`, and an executor the boundary can
run. That is a design question with a charter dimension (III.O counts
conversions, so what counts as converted has to be answerable), and it
is deliberately left open here rather than settled in a commit whose
subject is an extraction.

Until then, `spec/flywheel.md`'s "The draft is a v1 workflow" section
describes a target this repository does not have, and says so.
