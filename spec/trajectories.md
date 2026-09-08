# Trajectories: the prefix regression harness for lane decision points

> Charter: SEED-NEXT.md §16 ("Trajectory-prefix regression for lane
> behavior: recorded decision points replay against lane
> configurations to catch behavioral regressions in role or prompt
> changes"), Part III.O row 5 (its recorded half; the simulation-mode
> half is Phase 12 item 6's), III.J row 3 (the two lane-quality metrics the
> harness makes meaningful); [`plans/os-6bd9ffff.md`](../plans/os-6bd9ffff.md).

## A trajectory is what the record already says a lane did

Every act a lane performs is a signed record at a position, and every
refused attempt is a position-stamped line in the attempts journal
([`refusals.md`](refusals.md)). Nothing is recorded that the system
did not already write: the harness reads the chain and the journal
beside it, never the observation stream (unsigned, lossy, liveness
only) and never a hook inside the worker loop.

A **point** is one decision: `{position, verb, act?, subject,
outcome, code?, frame}`.

- `outcome` is `admitted` (a record the lane's key signed) or
  `refused` (a journal line the lane's key was refused on); the
  journal's own admitted lines are not read, because the chain is the
  record of what was admitted.
- `act` is the loop-act spelling `internal/loopverb` gives the verb
  when it has one (`claim take`, `submission make`), the name a
  manifest declares in `acts_through`.
- `code` is the refusal's machine code, present exactly on refusals.
- `frame` is what the lane decided from: `{state, affordances, owed}`,
  the subject's folded lifecycle state (empty for a subject the
  lifecycle never created), the actor's affordances on the subject
  (`admit.Affordances`, the boundary's own probe pipeline), and the
  obligation kinds the situation read owes the actor on the subject
  ([`obligations.md`](obligations.md)), sorted. The frame carries no
  instant, so two recordings of one chain are byte-identical.

**The prefix rule.** The frame is derived at the prefix the lane
actually saw, and the stamp means two different things by outcome.
An admitted record at `p` was judged against everything before it, so
its frame is `records[:p]`; the first record's frame is the empty
chain. A refusal is stamped with the tip ordinal of the view the
boundary judged it against, the last record of that view, so a
refusal stamped `p` is framed at `records[:p+1]`: a refusal stamped at
the chain's last record sees the whole chain. An admitted record at
`p` precedes a refusal stamped `p`; refusals stamped alike keep the
journal's order.

A **trajectory** is `{lane, actor, manifest, posture, points}`,
JCS-canonical: `actor` the lane key's fingerprint, `manifest` the
sha256 of the manifest file's bytes, `posture` the sha256 of the
fragments resolved in declared order ([`lanes.md`](lanes.md)). The two
digests are the configuration under which the points were recorded,
including every field no point-level class reads (`orients_from`,
`liveness_from`, `inbox`, `summary`, the fragment list).

Journal lines that cannot be placed are skipped and counted, never
silently dropped: a refusal stamped beyond the chain's tip (a journal
that outran the ledger copy it is read beside) and a line another
actor journaled.

`seed trajectory record --ledger <dir> --key <key> --lane <name>
[--lanes <dir>] [--out <file>]` records the key's trajectory under the
lane's configuration: the chain verified from genesis, the journal
loaded beside it (a missing journal is an empty one; one that does
not load refuses as `unreadable`, the declared-input posture, because a
recording over a torn journal would silently omit decision points),
the canonical bytes written to `--out` or carried in the envelope, and
the counts (`admitted`, `refused`, `skipped`) stated. The local
posture only: the journal is written beside a local ledger and never
synced, and the remote posture keeps no journal.

## Replay is a frame diff plus a configuration check

`seed trajectory replay <file> --ledger <dir> --key <key> [--lanes
<dir>]` recomputes every point's frame over the chain as it stands now
and classifies each point exactly once, in this order:

| class | the condition |
| --- | --- |
| `same` | the recomputed frame equals the recorded one; for an admitted point the act is still declared, granted and afforded |
| `frame_changed` | the state, the affordances or the owed kinds differ: the boundary or the fold changed under the lane, or the chain did (a record inserted before the point; a chain too short to frame it) |
| `act_undeclared` | an admitted point's loop act is no longer in the manifest's `acts_through` |
| `act_ungranted` | the manifest's grants no longer intersect an admitted point's accepted capabilities ([`actors.md`](actors.md)); a verb needing standing only is never ungranted |
| `act_inadmissible` | an admitted point's verb is absent from the recomputed affordances. Its recorded frame lacked it too (a record past the boundary on the raw seam), so the frame is unchanged and the act is what diverges |

A refused point is judged by its frame alone: the same frame means the
boundary presents the same choice, and the recorded refusal stands as
what it answered. The two act classes hold admitted points to the
configuration, not refusals: a refused attempt may have reached for an
act the lane never declared or held, which is often why it was
refused, and a class that fired on such a point would fail the corpus
on the day it was recorded.

Beside the points, the configuration is judged once per trajectory:
**`manifest_changed`** (the manifest bytes' digest differs from the
recorded one) and **`posture_changed`** (the resolved fragments'
digest differs). Both are divergences, deliberately: a manifest or
fragment edit that touches a lane with N recorded decision points
fails the drill until the corpus is re-recorded on purpose. That is
what "catch behavioral regressions in role or prompt changes" can mean
in a tree with no model, and it is stated below as the residual it is.

Replay exits 0 iff every point is `same` and both digests match;
otherwise exit 26 `lane_invalid` refining **`trajectory_diverged`**
([`envelope.md`](envelope.md)), naming each divergent point with its
class and detail and each changed digest, the result carrying the
counts (`points`, `same`, `diverged`, `manifest_changed`,
`posture_changed`). A lane replays its own trajectory with its own
key: a fingerprint alone cannot probe the boundary, and another key's
affordances are another lane's frame, so a key that is not the
recorded actor's refuses.

## The corpus

`trajectories/lanes/<lane>.json` holds one trajectory per
manifest in `lanes/*.json`, the set derived from the directory
and never from a hand list. It is recorded by the recorder scenario
(`cmd/seed/trajectory_e2e_test.go`): one local ledger at `seed/4`
driven through every shipped lane's own acts, each lane through the
CLI's boundary verbs (claims through the library seam, the one act the
CLI refuses offline) and each with at least one refused attempt, so
both arms of the recorder are exercised for every lane. The
dispatcher files, specifies and re-specifies; the planner claims,
proposes, records a dead end and releases; the implementer reserves,
records a dead end, submits, requests the merge and parks; the
verifier passes and fails; the supervisor starts a run and publishes
an offer; the observer lands the merge; the maintenance actor reaps;
the curator proposes over two holders' dead ends.

Three drills hold the corpus:

- **completeness**: exactly one file per shipped manifest, no file
  for a manifest that does not exist, and every lane with at least one
  admitted and one refused point (a lane with no act in the tree would
  be reported as configuration-only, its digests recorded so a change
  to its configuration still diverges);
- **reproduction**: the scenario is rebuilt and every file reproduced
  byte for byte (`go test ./cmd/seed -run Corpus -update` re-records
  on purpose);
- **replay**: every file replays green against `lanes` over the
  rebuilt chain, and planted rows fail it with the named classes (a
  manifest without `submission make`, a manifest without `claim`, a
  manifest whose `orients_from` alone changed, a fragment with one
  added line, and a configuration-only lane whose manifest changed).

Determinism rests on the frame carrying no instant, the scenario
fixing positions and verbs, and every key being derived from a fixed
seed, so every fingerprint in the corpus is the same on every machine.

## What replay proves, and what it cannot

In the injection suite's words ([`lanes.md`](lanes.md)): no decider
re-runs at a point. A green replay proves that the configuration still
presents the same frame at every recorded decision point and still
permits the same act, not that a model would choose it. A role or
prompt change is caught as a changed posture or manifest, never as a
changed choice. Phase 12 item 6's simulation mode is the seam where a decider
plugs in and the second half of III.O row 5 lands; until then the
harness is the recorded half, stated as such.

## The two lane-quality metrics

III.J row 3 asks that the dispatcher's re-triage rate and the
planner's unedited-approval rate be tracked. Neither had a record to
read before this harness's card: the dispatcher had no act that
revised a triage, and the plan verbs carried anchors whose commits
differ across a squash merge even when the content is identical.

- **Re-specification** ([`lifecycle.md`](lifecycle.md)):
  `contract.specified` admits from `ready` at `seed/4`, the dispatcher
  revising its own triage of an unclaimed contract; the fold counts
  the subject's applied specifications.
- **Plan digests** ([`plans.md`](plans.md)): `plan.proposed` and
  `plan.approved` carry the plan's content digest at `seed/4`, and an
  approval is unedited iff its digest equals the first proposal's.
- **The report's `lanes` section** ([`projections.md`](projections.md)):
  `retriage_rate` over subjects with a specification, `unedited_rate`
  over measured approvals, null at a zero denominator, the section
  null when no work subject exists.

## Conformance mapping

- III.O row 5 "Trajectory-prefix regression covers lane decision
  points" — the recorded half: `internal/trajectory`, `seed trajectory
  record|replay`, the corpus and its three drills; the simulation-mode
  half is Phase 12 item 6's, named above.
- III.J row 3 "Dispatcher re-triage rate and planner unedited-approval
  rate are tracked" — the `ready` origin, the plan digests and the
  report's `lanes` section, each drilled at the boundary, in the fold
  and in the projection.

## The decider: the harness's second half (III.O row 5)

`internal/decider` supplies the missing half of III.O row 5: a `Decider`
re-runs at a recorded decision point and says which loop act it would
choose there. `trajectory replay --decider scripted` re-runs the
reference decider at every admitted loop-act point whose frame is
unchanged and whose act the configuration still permits, and reports
`choice_diverged` when the decider's definite choice differs from the
recorded act — the behavioral regression the five frame/configuration
classes cannot see (the configuration still presents the frame and
permits the act; the choice moved).

The reference decider is **partial by design**. The loop's per-iteration
act is not a function of the `Frame` — identical frames recorded
different acts, which is exactly why the harness records frames rather
than deciders — so the reference decides only where the frame determines
the act (a claimable contract in `ready` is taken) and abstains ("")
elsewhere. Replay with the scripted decider therefore passes on the
recorded corpus (agree where determinate, abstain where not) and catches
a decider that would choose a different act at a determinate point. The
five existing classes are unchanged.

- III.O row 5 (second half) "a decider plugs into the harness; a
  behavioral regression is caught" — `internal/decider`,
  `trajectory.ChoiceDiverged`, `ReplayWithDecider`, drilled in
  `internal/trajectory` and `internal/decider`.
