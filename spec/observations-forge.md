# observations-forge.md: the forge says the submission is not mergeable

> Status: v0, normative for `next/**` from `seed/8`. Authority:
> [`SEED-NEXT.md`](../SEED-NEXT.md) §II.4 "External facts" (mirrors
> of external authorities enter the ledger as observations,
> `merge.observed`, `check.observed`), §II.8 (the forge's required
> checks are the merge gate), §II.11 (the observer records what an
> external authority did; the maintenance lane runs unattended), §II.13
> (the checks gate: CI green plus zero unresolved threads via forge
> adapters); conformance III.D row 7. Plan:
> [`plans/os-0cd18799.md`](../plans/os-0cd18799.md). Implemented by
> `internal/transition` (the facts), `internal/admit` (the rules),
> `internal/obligation` (the kind), `internal/protections` (the
> readers), `internal/maintain` (the pass), and `seed check observe`.

## The gap this closes

After `submission.made` a contract sits in `review` with no obligation
owed between "the pull request is open" and "the merge is observed".
The verifier judges the acceptance spec's commands in its own workspace
and never reads the forge ([`verdicts.md`](verdicts.md)); a red CI run
or an unresolved review thread is invisible to the verdict. The forge
entered the ledger through one fact, `merge.observed`, which refuses an
unmerged pull request by design ([`forges.md`](forges.md)). The
transition table leaves `review` only through `merge.observed`,
`contract.returned`, `escalation.raised` and `contract.cancelled`, and
the return cited a fail verdict alone. So an agent opened the pull
request and stopped, and a human drove it to green by hand. This spec
makes "the forge says this submission is not mergeable" a fact the
observer records, a debt the dispatch lane owes, and a return the
maintenance pass performs, so the existing offer, claim and resubmit
machinery does the rest.

## `check.observed`

A **fact**, never a transition: it admits only while the subject folds
to `review` (elsewhere exit 3, the `verdict.rendered` posture), changes
no state, and is defined from `seed/8`. The strict payload:

```json
{"pr": "<ref>", "head": "<full sha>", "checks": "green" | "red" | "pending",
 "unresolved_threads": <int, optional>, "review": "approved" | "changes_requested" | "none"}
```

- `pr` is the `merge.observed` ref grammar (`pr/<n>` or `<n>`). Where
  the submission names a pull request (below) the observation must
  name the same one.
- `head` is a full lowercase-hex commit and must equal the head of the
  submission under review, the tail of its packet's `base` range
  ([`packets.md`](packets.md)). An observation of another head refuses
  naming both, so a stale poll never stands for the head being judged.
- `checks` is the forge's combined state over the head: `green` when
  every check run completed passing (success, neutral or skipped),
  `red` when any completed otherwise, `pending` while any still runs.
  A head the forge lists no checks for is `green`: nothing failed and
  nothing is running, the v1 engine's gate posture.
- `unresolved_threads` is the count of review threads not marked
  resolved, and **absent where the forge cannot say**: Forgejo has no
  thread resolution ([`forges.md`](forges.md)). Absent is not zero;
  the obligation below treats it as no thread debt the forge can
  report, and the forges page says so.
- `review` is the latest review state per reviewer, reduced: any
  reviewer still requesting changes is `changes_requested`, else any
  approval is `approved`, else `none`.

**No forge prose crosses.** The payload carries literals and a count:
no thread bodies, no comment text, no check output, no reviewer names.
Thread text is the hostile input the dispatcher's posture exists for
([`lanes.md`](lanes.md)), and the implementer reads it on the forge the
way `seed message read` works: the reader chooses to look. The
injection sweep plants its marker in every prose field the fake forges
serve and finds it in no payload, no obligation row and no situation
read.

**Change is the admission condition.** The observation must differ
from the standing one on the subject in at least one field; an
unchanged poll refuses naming the standing position, and `seed check
observe` refuses before signing on the same comparison (one rule set,
advertisement and enforcement). So the subject's share of the ledger is
bounded by change, never by polling cadence.

Capability: `observer`, `operator` (the `merge.observed` row,
[`actors.md`](actors.md)). The fold records the latest admitted
observation per subject and clears it on each `submission.made`: a new
window is a new head, and the forge's word on the old one binds
nothing.

## The submission names its pull request

`submission.made` gains an optional sibling field `pr` beside `fence`,
`packet` and `plan`, defined from `seed/8` and read by the fold at
`seed/8` positions only; `seed submission make --pr <ref>` writes it
and refuses before signing on an earlier chain. The maintenance pass
polls only submissions that name one. A submission without `pr` is
legal and simply never observed, so every existing chain keeps its
meaning. The pass does not resolve a pull request by branch name: a
forge fact the ledger did not record is not something the pass should
guess.

## `submission.unmergeable`

The obligation kind ([`obligations.md`](obligations.md)), derived when
the standing observation on the current submission window is **red**:
`checks: red`, or `unresolved_threads` above zero, or `review:
changes_requested`. Not emitted for `pending` alone, and not emitted
once a fail verdict stands on the window, because the return is then
owed on the verdict's account and the row would name the same debt
twice. Owed by `lane:dispatch`, since the return is queue management;
`since` is the observation's position; the row carries `head`, so the
situation read says which revision is red; discharged by
`contract.returned` citing the observation. While it stands the merge
debt (`verdict.unmerged`) is not advertised, because `merge.requested`
refuses (below) and an obligation nobody can discharge is an anomaly.
A verdict (`submission.pending`) is owed only while the subject is
under review: a return by observation re-readies it unjudged.

## The return by observation

`contract.returned` (`review` to `ready`, the table row unchanged)
cites **exactly one** of `{"verdict": <position>}` (unchanged: a
standing, boundary-validated fail on the current submission,
[`reconciliation.md`](reconciliation.md)) or `{"observation":
<position>}`: the **latest** observation on the subject, on the head
under review, satisfying the predicate above. A later green
observation supersedes a red one, so a return citing the superseded
red refuses by name, and a return citing a green observation refuses
because nobody yanks a green pull request. Capability unchanged
(`dispatch`, `operator`).

**A return by observation records no lockout.** The red-verdict
lockout keys on fail verdicts ([`verdicts.md`](verdicts.md)); a return
on the forge's word adds no `rejected` fact and no fail, so the prior
submitter is the natural next claimant rather than a blacklisted one.
This is the one place the design departs from the v1 template's
`reject` edge, which appends the author to a rejected list: a red pull
request wants its author back.

The fold records every applied return with what it cited
(`Returns`), so the ceiling below counts observation-cited returns on
the chain rather than remembering them.

**`merge.requested` refuses while the forge says red.** On either
citation path, a standing red observation on the head under review
refuses the request naming the observation: a chain that branch
protection would hold anyway must not sit `unreconciled` behind it. A
green observation supersedes and the request admits. `merge.overridden`
is untouched: it overrules a verdict and says nothing about the forge,
whose protections are the operator's to reconcile.

## The maintenance pass observes and returns

`seed maintain run` runs reap, **observe**, **return**, lint, file,
rebuild, checkpoint ([`maintenance.md`](maintenance.md)). Observations
come before the lints so the lints read fresh facts; the return before
the filing so a returned subject is not also filed as a finding; the
checkpoint last as before.

- **Observe**: for every `review` subject whose submission names a
  `pr`, read the forge (`--forge snapshot|github|forgejo`, the
  `merge observe` vocabulary) and append `check.observed` when the
  observation differs from the standing one. A subject the poll fails
  on, or whose observation is unchanged, is reported skipped with the
  reason. With no `--forge` every observable subject is reported
  skipped with that reason, never passed over silently, and CI runs
  that way.
- **Return**: for every `submission.unmergeable` row the **fresh** view
  carries (the pass re-reads the ledger between the two steps, so the
  return cites the observation it just recorded), append
  `contract.returned {observation}`, unless the ceiling holds.

Both verbs accept the maintenance key's `operator` standing; a key
holding `maintenance` alone meets `out_of_grant` at the door on both
and the refusals are reported, the pass's existing posture. Wakeless
as before: a forge webhook is an advisory wake adapter (§II.9) and out
of this spec's scope; the pass on its schedule is the correctness path.

## The re-offer

A return re-readies the subject with no live offer on it, and a
worker's poll lists nothing for a subject with no live offer, so the
loop above would stall after its first return until a supervisor
noticed. The pass therefore **re-offers what it returned**
(plans/os-29e2fef2.md D3; [`maintenance.md`](maintenance.md)
"Re-offer"): one `offer.published` per return, in the pass that
returned it, scoped to the prior submitter's tuple where the chain
derives one the holder can still take ([`ranking.md`](ranking.md)
"Resume"; a chain at `seed/9`, where a start declares its tuple
again) and carrying the capabilities and tiers of the offer the
returned claim consumed. The prior submitter's poll lists it and takes
it through the unchanged claim; another configuration's does not. A
window that declared no tuple re-offers unscoped by tuple. Nothing in
the return, the ceiling or the merge chain changes.

## The return ceiling

`--return-ceiling <n>` (default 3, a declared threshold in the
`expiry_after` shape, never a clock) bounds the observation-cited
returns one subject may carry. At the ceiling the pass appends
`escalation.raised` instead (`review` to `blocked`, a legal row): a
packet naming the standing observation's position and head and the
count, and one decision with three answers (raise the ceiling, cancel,
merge by hand). This is §II.13's "max revisions" on the contract loop,
and what stops a red pull request from cycling forever unattended.
Budget reservations bound spend per window as before, so the ceiling
bounds rounds, not money. Nothing on the subject moves until the
question is answered ([`escalation.md`](escalation.md)).

## Reading the forge

`protections.Observer` answers `Checks(pr)` beside `Merged(pr)`
([`forges.md`](forges.md)): GitHub from the pull request's head, its
check runs, the GraphQL review-thread query (paged past a hundred) and
its reviews; Forgejo from the pull request's head, the commit's
combined status and its reviews, with the thread count nil; the
snapshot arm from a file, so every drill is credential-free. The token
stays an environment variable named by `--token-env`. The readers never
write.

## Protocol

`seed/8` ([`protocol.md`](protocol.md)): a `seed/7` validator's
unknown-verb arm fails a chain carrying `check.observed` and its strict
return decode refuses the observation citation, so the two judge a
`seed/8` record differently, hence the bump. At `seed/7` positions the
fact stays unknown-and-refused under a `seed/8` validator, a return
cites a verdict only, and `submission.made`'s `pr` is not read.

## Conformance mapping

- III.D row 7 "External facts enter only as observations by governed
  observers; nothing treats an observation as control": the fact is
  the observer's, admitted on review subjects with no operator ceremony
  beyond the standing fallback; the return it authorizes is the
  dispatch lane's own act under the table's row, never the
  observation's effect; `TestCheckObservedAdmitsAndBinds`,
  `TestReturnedByObservation`, the walk's `review-c6`, `observed-c6`
  and `returned-c6` stations.
- III.G rows 1 and 2 retained: the merge chain is unchanged and
  `merge.requested`'s new refusal is one more link it holds;
  `TestMaintainObservesAndReturnsTheRedSubmission` completes the chain
  to `done` after a return, and
  `TestMaintainReoffersWhatItReturnedToThePriorConfiguration` closes
  the loop unattended: red, returned, re-offered, retaken by the prior
  configuration, green, merged, with no offer published by hand after
  the first.
- §II.13 "checks (CI green + zero unresolved threads via forge
  adapters)" on the contract loop: `TestGitHubChecks`,
  `TestForgejoChecks`, `TestSnapshotChecks`, and the pass's drills
  (`TestMaintainEscalatesAtTheReturnCeiling` for the ceiling).
- III.J row 2 (embedded instructions in tool output are quoted as
  data, never obeyed): the reader carries no prose and the sweep
  plants the marker in every field the fakes serve.
