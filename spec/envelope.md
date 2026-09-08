# envelope.md — the affordance envelope and exit codes

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §10 and Appendix B; [`docs/build-plan.md`](../docs/build-plan.md)
> §1 fixed defaults (exit-code continuity with v1). Envelope schema version:
> **`seed-envelope/0`**, carried in every response's `v` field and bumped by
> the same discipline as the protocol version (a field addition, removal, or
> rename is a schema change).

## Shape

Every verb response is exactly one JSON line:

```json
{
  "v": "seed-envelope/0",
  "ok": true,
  "result": { "…": "…" },
  "error": null,
  "position": "<ledger position computed at, or null>",
  "affordances": ["verb", "…"],
  "budget": { "reserved": "…", "remaining": "…" },
  "exit": 0
}
```

- `result` and `error` are mutually exclusive: a refusal replaces `result`
  with `error` (`{code, message}`: a stable machine-branchable code and a
  human message) and a distinct exit code. These codes are also what the
  attempts journal records per refused attempt for the report's
  refusal-rate metric ([`refusals.md`](refusals.md)); the envelope
  schema itself is unchanged by that metric.
- `position` is the ledger position the response was computed at, so a
  concurrent change is detectable rather than mysterious (charter §II.10).
  Every response that reached the ledger stamps it; a refusal raised before a
  position was ever read (a malformed invocation, an unreadable ledger
  directory) carries `null` rather than inventing a position, because a
  fabricated stamp would break exactly the detection this field exists for. The attempts journal
  turns on the same distinction: an unstamped response is not an
  admission-boundary attempt ([`refusals.md`](refusals.md)).
  A scan that read some records and failed at the next was computed at the
  position it failed at and stamps it, as `seed ledger verify` and `seed
  ledger show` both do (plans/os-37fcf7c6.md).
- `affordances` lists the verbs currently legal **for this actor on this
  subject**, computed by drafting one signed probe per catalog verb and
  running the same rule set admission enforces (one rule set, two
  consumers, zero exceptions; `admit.Affordances`, landed in Phase 8.1
  with its lifecycle-walk and catalog-completeness drills; item 2's
  regression-class harness generalizes them). Every append-path response
  stamps the list for its signing actor and subject at the stamped
  position, refusals included: the refusal plus what IS legal is the
  envelope's point, and the loop verbs
  ([`loop-verbs.md`](loop-verbs.md)) are where that point does the
  most work, pre-flighting an act through the same rule set and
  answering "not that" and "then what may I do?" in one envelope. A
  computation failure degrades to the empty
  list, never to a failed verb. Responses lacking a ledger, signing
  key, or subject (e.g. `version`, `doctor`, keyless read surfaces:
  probes must be signed, so a fingerprint alone cannot compute) carry
  the empty list. One named carve-out: `actor.enrolled` is listed only
  where the prober could supply the subject's public key, which no
  fingerprint-holder can derive — the enrollment surface knows its key
  out of band.
- `budget` is the reservation block, derived from the shared budget view
  (`admit.BudgetBlock`): `reserved` sums the open valid reservations,
  `remaining` is the derived remaining, both decimal strings; `null` on
  subjects carrying no budget facts. `seed budget status` accepts
  `--key` to stamp affordances beside the block it already reports.
- `exit` duplicates the process exit code so machine callers can branch on
  the envelope alone.

## Exit codes

The build-plan fixed default: **reuse v1 conventions where semantics match**,
for tooling continuity. Inherited allocations:

| code | name | meaning |
|---|---|---|
| 0 | `ok` | verb succeeded |
| 2 | `contention` | claim contention or author lockout: exclusivity not granted — a rival `claim.taken` on a held contract returns the holding fingerprint and the active fence position (the loser learns who holds and since when), and an exclusive verb drafted offline refuses here too: claiming is online-only (`lifecycle.md`) |
| 3 | `invalid_transition` | transition absent from the spec tables, or verb illegal in this state — the contract-lifecycle refusals included (`lifecycle.md`): an illegal (state, verb) pair, a birth verb on an existing subject, a non-birth verb on an unknown one, each naming subject, current state, and verb |
| 4 | `not_found` | subject does not resolve |
| 5 | `unavailable` | authoritative remote or backend unreachable |
| 6 | `fenced_out` | stale or missing fence (claim token): on a held contract the deliberate exits and every event from the holder or a prior claimant must cite the active fence as `{"fence": "<position>"}`; the refusal names the cited fence, the active fence, and the holder (`lifecycle.md`) |
| 7 | `halted` | admission is halted (`system.halt.declared`); only an operator's `system.halt.lifted` may append |
| 8 | `chain_invalid` | ledger verification failed (parse, linkage, signature, actor, or HEAD trouble); the error message carries `position N: <reason>: <detail>` |
| 9 | `classification_refused` | payload failed the data-classification lint; the error message joins the violations' pointers and rules |
| 10 | `version_mismatch` | protocol or envelope version mismatch |
| 11 | `remote_rejected` | the remote's own admission refused the push (e.g. a `pre-receive` policy hook declined); the error message carries the remote's reason verbatim |
| 12 | `head_regression` | the remote serves a chain that regresses this client's persisted verified head (rollback or vanished ref): a freshness refusal, distinct from failed chain verification |
| 13 | `posture_invalid` | the deployment's posture declaration is malformed or names an unknown posture; every deployment MUST declare exactly one of the three charter postures (SEED-NEXT.md Part II "Postures"), and the message names the valid ones — distinct from `not_found`, which covers a deployment with no declaration at all |
| 14 | `out_of_grant` | the actor holds none of the capabilities the verb accepts (charter Part II "Capabilities"): a structural authorization refusal, distinct from a signature that fails to verify (`chain_invalid`). The accepted-capability sets per verb are the normative table in `actors.md`; governance roots hold `operator` implicitly, and the message names the accepted set |
| 15 | `stale` | a projection consumer demanded a minimum build position (`--min-position`) and the resolved build's stamp sits below it (charter III.D: staleness is visible and demandable); the message names the stamped and demanded positions. A freshness judgment on a successfully resolved view — distinct from `not_found` (no published build at all) and from `head_regression` (the authoritative chain itself went backwards) |
| 16 | `plan_required` | the submission names a contract above the trivial tier with no approved plan (or no cited plan anchor): claiming an unplanned contract authorizes planning only, so go plan — a merged plan PR observed as `plan.approved`, and the submission citing the plan anchor it implements (`plans.md`) |
| 17 | `not_independent` | the verdict signer is an implementing key on this contract (a claimant, past or present, or the bound submission's signer): L1 independence is failure-domain separation checked at admission, and holding a verdict grant does not cure being disqualified on the contract being judged — distinct from `out_of_grant`, which is capability absence (`verdicts.md`) |
| 18 | `ungated` | the verifier refuses to execute declared-executable acceptance content whose gate evidence is absent: gate-before-run, the half `acceptance.md` assigns to verdicts — nothing runs, no verdict is rendered (`verdicts.md`) |
| 19 | `spec_unrunnable` | declared-executable acceptance content yields no parseable commands: the declaration promised runnable content and the body carries none, an inconsistency only run time can see — silence must never decide, so the run refuses rather than passing vacuously (`verdicts.md`) |
| 20 | `checks_red` | rendering `pass` refused: the verifier derives the permissible verdict from the transcripts it just executed, and at least one visible check exited nonzero — `fail` stays renderable, and the message names the failing command (`verdicts.md`) |
| 21 | `receipt_mismatch` | recomputation from the submission head does not reproduce a cited receipt digest: the stored receipt, the range, or the repository's content has diverged from what the verdict attested — the recompute-and-mismatch refusal, and 6.2 reconciliation input; the message names both digests (`verdicts.md`) |
| 22 | `seal_broken` | a sealed-checks envelope does not open: missing ciphertext, a commitment that does not match the body, or an envelope carrying no checks. The commitment is the contract, so a body that fails it is not a weaker seal but a different document (`sealed-checks.md`) |
| 23 | `not_recipient` | the identity opening a sealed envelope is outside its recipient set, which rotation lag produces routinely: the sealer encrypted to a key set this actor is no longer (or not yet) in, and re-sealing to the current set is the fix, never a bypass (`sealed-checks.md`) |
| 24 | `unsealed` | an above-trivial contract reaches the verifier with no sealed-checks commitment: contracts above the trivial tier carry sealed checks, sealed before the first claim, and render refuses rather than judging against criteria the implementer could read (`sealed-checks.md`) |
| 25 | `red_locked` | rendering `pass` over a submission an authenticated `fail` already judged: a red verdict locks pass out until a NEW submission (`contract.returned`, re-claim, resubmit). Fail restatements stay admissible, and only boundary-validated fails lock (`verdicts.md`) |
| 26 | `lane_invalid` | a checked-in lane manifest makes a claim the tables refuse: a grant outside the vocabulary, an act whose accepted capabilities the lane does not hold, a liveness source that is not a work step, a missing fragment. Distinct from `posture_invalid`, which judges a deployment's posture declaration rather than a role definition (`lanes.md`) |
| 27 | `budget_exhausted` | a reservation asks for more than the class has left: capacity is checked and decremented at admission, so exhaustion is a first-class, EXPECTED, recoverable condition rather than chain trouble, and the worker loop's exhaustion park carries this code into its packet for the next worker to read. Narrow by design: the budget rule's other refusals (malformed payload, wrong signer, unknown class, double close, laundering) stay `chain_invalid`, because a caller that answers this code by asking for less must not also be answering a bug (`budgets.md`) |
| 28 | `drift` | a declared desired state and an observed state differ: the forge's protections against the deployment declaration first (`protections_drift`), and every later declared-versus-observed comparison as a refinement; the message names each difference, so a CI job gates on the exit and an operator reads what to do (`postures.md`) |
| 29 | `import_refused` | the predecessor import refusing before any write: the export's anchor does not hold (`unanchored`), its content is not the anchored tree (`export_mismatch`), or the transform table has no row for something the export carries (`import_unmapped`); the message names the tag, the path or the verb, so the operator anchors, re-exports or adds the row (`import.md`) |
| 64 | `usage` | CLI usage error (EX_USAGE); never a verb result |
| 66 | `unreadable` | an input the invocation names exists but cannot be opened or read (EX_NOINPUT: a directory, denied permissions, an I/O failure): an operational failure in the usage class, distinct from a judgment on the content (`posture_invalid`) and from a missing declaration (`not_found`) |

**Allocation rule for new codes**: a new condition takes the lowest unused
integer in 7–63, lands as a PR editing this table before any code emits it,
and is mirrored by a constant in `internal/envelope`. Codes 64 and
above stay reserved for CLI-usage-class errors. Distinct refusals the
charter names (halt, out-of-grant, classification refusal, …) get their own
codes when the refusing rule lands; nothing shares a code with a different
meaning.

**Exits are families; the machine code can be finer.** The `exit` answers
"what kind of thing happened", which is what a caller branches on; the
`code` string names the specific condition inside that family. Eight exits
ship with a second code today, and each of them is a NARROWER case of its
exit rather than a different meaning, so none of them is the sharing the
rule above forbids:

| exit | refining code | the narrower condition |
|---|---|---|
| 3 | `ledger_not_empty` | `seed init` against a ledger that already has a genesis: an illegal transition whose one useful detail is *which* illegal one, since the caller's next move is to point at a different directory rather than to re-read the tables |
| 3 | `name_taken` | `seed flywheel draft --validate`, `propose` or `repair` on a shape whose workflow name the registry already holds at the repository's head: an illegal step whose one useful detail is that nothing was staged, since a drafted workflow takes no existing name and the caller's next move is to look at the file that holds it (`flywheel.md`) |
| 4 | `posture_undeclared` | the posture declaration the invocation needs does not exist: a `not_found` whose subject is a config file rather than a ledger subject, and the only `not_found` a caller answers by writing a file |
| 17 | `level_short` | the verdict's achieved independence level is short of the level its tier requires (`tiers.md`'s `independence` column): the family's answer, "this verifier cannot judge this contract", stands, and the word says it is the configuration rather than the key (`verdicts.md`) |
| 20 | `carrier_absent` | `seed verdict render` on an eval bound to a candidate lesson whose carrier commit is not an ancestor of the submission head: the transcripts may be green, and the verdict still cannot be rendered, because the thing under test was never applied (evals.md) |
| 20 | `rubric_red` | `seed verdict render --verdict pass` over a scorecard scoring an item `fail`: the verdict derives from the scorecard as from the transcripts, and a failing item forbids pass while fail stays renderable (`verdicts.md`) |
| 20 | `human_verdict` | `seed verdict render` over a scorecard item at `high` uncertainty (neither verdict is renderable; `seed verdict defer` routes it to a human), or from a key without operator standing on a human-review tier or after a deferral: the render is a human's (`verdicts.md`) |
| 20 | `lint_refused` | `seed knowledge lint` on a lesson file that fails the promotion gate's file half at a named gate (curation.md): the file, the fact and the repository disagree, which is a check gone red on content rather than on a command |
| 19 | `eval_vacuous` | `seed eval check` on a definition whose unsolved fixture already passes every acceptance command: the spec cannot decide, which is the family's answer, and the extra word says which way, since a pass on such an eval would qualify nobody (`evals.md`) |
| 26 | `trajectory_diverged` | `seed trajectory replay` on a recorded trajectory that no longer replays green: a point whose frame, declaration, grant or admissibility moved, or a manifest or posture digest that differs from the recorded one. The family's answer stands, the checked-in lane configuration no longer supports a claim about it, and the word says the claim was a recorded decision point rather than a table row (`trajectories.md`) |
| 22 | `seal_unauthorized` | a seal whose signer held no sealer grant at the seal's own position, or which cites a position outside the verified chain: like a broken seal in that it will not be unsealed, unlike one in that the ciphertext is fine and the authoring boundary is what failed (`seed reconcile` surfaces the same condition as `seal_unverified`) |
| 66 | `posture_unreadable` | the posture declaration exists and cannot be read: the same operational failure as `unreadable`, narrowed to the one input whose absence is separately reportable as `posture_undeclared` |
| 28 | `protections_drift` | `seed protections plan`, or `seed doctor --current`, over a forge whose rulesets, CODEOWNERS or scheduled workflows differ from what the declaration derives: the family's answer, "the declared state is not the observed one", with the surface named (`postures.md`) |
| 2 | `non_fast_forward` | the admission service's answer to a proposal linked to a tip that is no longer the tip (HTTP 409): contention at the ref rather than at a claim, and the one refusal the proposer's own loop answers by re-linking rather than by reading anything (`postures.md`) |
| 4 | `ranking_empty` | `seed offer publish --strongest` when no qualified tuple ranks for the named capability at the offer's instant: a `not_found` whose subject is the policy's answer, so an unscoped offer stays the supervisor's explicit choice and never policy running out ([`ranking.md`](ranking.md)) |
| 4 | `resume_empty` | `seed offer publish --resume` when no configuration derives for the subject's latest return by observation (no return, a verdict return, no declared tuple, a holder that is suspended, revoked or no longer cites the tuple): the `ranking_empty` posture for the per-subject preference, naming the missing link, so the offer is scoped by hand or unscoped by explicit choice ([`ranking.md`](ranking.md) "Resume") |
| 4 | `trust_undeclared` | `seed project start` under a declaration with no `checkpoints.trust` block: a `not_found` whose subject is a config field — the one choice the charter says is declared, not defaulted — and the only `not_found` a caller answers by declaring `replay` or `signers` (`checkpoints.md`) |
| 21 | `checkpoint_mismatch` | `seed project start` over a checkpoint whose snapshot is not retrievable, whose digest, position or files do not reproduce from the chain at the cited position: the family's answer, a cited state that recomputation does not reproduce, and the word says the citation was a checkpoint rather than a receipt (`checkpoints.md`) |
| 28 | `preseed_drift` | `seed init --preseed` or `seed preseed check --ledger` over a chain the declaration contradicts — another governance root, a protocol below the chain's, or activations the chain has not made: the family's answer, a declared state and an observed one that differ, with the field and both values named (`postures.md`, "The preseed") |
| 29 | `unanchored` | `seed import` over an export whose `head` is neither the commit the anchor tag names nor an ancestor of it, over a source with no `seed-anchor/*` tag, or with an `--anchor` that does not resolve: the family's answer, refused before any transform, and the word says which precondition failed (`import.md`) |
| 29 | `export_mismatch` | `seed import` over an export whose files are not the anchored tree — a path that differs, one the tree lacks, one the export lacks: the family's answer with the path named, refused before any write (`import.md`) |
| 29 | `import_unmapped` | `seed import` over an export carrying a run-log verb or a card state the transform table has no row for: the family's answer with the verb or state named, since a drop is a row and no entry is skipped silently (`import.md`) |
| 13 | `preseed_incomplete` | a declaration missing something the charter requires it to say — a required member of the protected surface, a tier outside the vocabulary, a lane that is no manifest, an undeclared squad under a guardrail: a judgment on the declaration's content, like `posture_invalid`, and distinct from drift against a chain (`postures.md`) |
| 3 | `tier_above_ceiling` | `claim.taken` by an agent- or service-kind key on a contract above its squad's declared `max_agent`: an illegal step at this position under this deployment's guardrails, naming the kind, the squad, the tier and the ceiling; a human key is not ceilinged (`postures.md`) |
| 3 | `race_settled` | an act on a contract by a racer whose claim outlived the race's settlement (`lifecycle.md`, "Racing"): the first verified success closed the contract at the named position, so an illegal step by construction, naming the settlement, the actor and its fence; the racer's own deliberate exit still admits, and `contention` at a racing squad's cap names every holder |
| 3 | `routing_unknown` | `intent.filed` whose `routing` names no declared squad: the residual `tiers.md` named, closed by the declaration's teams (`postures.md`) |
| 28 | `audit_violated` | `seed ledger audit` over a chain that verifies and breaks one of the five bars (`simulation.md`, "The five-bar audit"): a `drift` between the bar the chain is held to and the chain it holds, the message naming each violated bar with the records it names, stamped at the tip audited; a chain that does not verify refuses `chain_invalid` before any bar is read (plans/os-7599c27d.md D2). |
| 3 | `request_refused` | a request verb whose shape, subject or citation is wrong ([`requests.md`](requests.md)): an unknown kind, a reference that is not one, a summary over 200 bytes or a body in the payload, an `about` no contract resolves, an answer to no request or to an answered one, `filed` without its intent or `declined` without its reason, an intent not admitted after the request |
| 3 | `approval_refused` | an approval verb whose shape or citation is wrong ([`protocol.md`](protocol.md), "Per-verb approval"): a request naming no catalog verb, an approval verb, an unenrolled actor or a contract the chain does not know (a request for `intent.filed` alone names the contract its filing would create), a reason over one line or 200 bytes, an answer citing no request, another subject's request or an answered one, a grant with a reason, a denial without one |
| 3 | `approval_required` | an act the declaration's `guardrails.approvals` governs for this actor (by fingerprint or roster kind) at or above the floor, with no valid open `approval.granted` naming the verb and the actor (a grant whose signer lacked operator standing at its position authorizes nothing): an illegal step at this position under this deployment's guardrails, `tier_above_ceiling`'s family, the message naming the request to file and, once one stands, the grant that answers it (`postures.md`, "The approvals block") |
| 3 | `erasure_refused` | `artifact.erased` whose shape, subject, reference or once is wrong ([`protocol.md`](protocol.md), "Erasure"): a digest that is not one, an empty or over-long reason, a contract the chain does not hold, a digest the contract's fold does not reference, or an artifact already erased there |
| 5 | `erasure_incomplete` | `seed artifact erase` whose record landed, or already stood, and whose removal the artifact store refused: the family's answer, a backend that would not take the write, with the position the record stands at and what was removed so far named; the record is the attribution, and the next run finishes the removal without a second record ([`protocol.md`](protocol.md), "Erasure") |
| 3 | `topology_refused` | a relation fact whose shape, source, target or graph rule is wrong ([`topology.md`](topology.md)): an unknown field, an empty target, a mission that is not a commit-anchored reference, a source the chain does not hold or a terminal one, a target the chain does not hold, a self edge, a dependency or hierarchy cycle, a link that already stands, or an unlink of a link that does not |
| 3 | `not_effectively_ready` | `claim.taken` on a contract that folds to ready and is not effectively ready ([`topology.md`](topology.md)): it waits on a dependency the table does not mark terminal, or an ancestor holds it in `blocked`; the message names both sets, and the queue and the poll hide the same subject for the same reason at the same prefix |
| 3 | `card_refused` | a capability card that is unsigned, carries a field outside the pin, names an internal, or names a kind that is not one ([`boundary.md`](boundary.md)) |
| 28 | `card_drift` | the checked-in card does not say what the declaration renders, or is absent: re-render it with `seed boundary card` ([`boundary.md`](boundary.md)) |
| 3 | `boundary_unpinned` | a task view read across the boundary carrying a field the pin does not list ([`boundary.md`](boundary.md)) |
| 3 | `artifact_mismatch` | bytes fetched across the boundary that hash to something other than the digest they were fetched as ([`boundary.md`](boundary.md)) |
| 3 | `not_merged` | `seed merge observe --pr N --forge <kind>` when the forge reports the pull request is not merged: `merge.observed` records a merge the forge confirms, not an intention, so recording one for an unmerged pull request is the illegal step, the pull request named ([`forges.md`](forges.md)) |
| 3 | `unchanged` | `seed check observe` when the observation says exactly what the standing `check.observed` already says: an unchanged poll appends nothing, so recording it is the illegal step, the standing position named ([`observations-forge.md`](observations-forge.md)) |
| 18 | `under_tiered` | `seed plan lint --config --tier` on a file scope, or `seed verdict render --config` on a receipt's changed files, touching a floored prefix at a tier below its floor: content whose gate is short, the family's answer, with the path and the two tiers named (`postures.md`, `plans.md`) |

A refinement is not a way around the allocation rule. A condition a caller
must act on **differently** takes its own exit, which is precisely why
budget exhaustion did not stay a refinement of `chain_invalid`
(`budgets.md`): the answer to it is to reserve less, and the answer to a
malformed payload is not. A refinement is for when the family's answer is
already right and the extra word only says which case it was.

## Deferred: structured error data

Error stays exactly `{code, message}` in `seed-envelope/0`. Rich failure
data (reason and position fields, per-violation pointer lists) is rendered
deterministically into `message`; promoting it to structured fields is a
schema change that lands only with a versioned envelope bump, when a
machine consumer needs it.

## Conformance mapping

- III.I: "Every verb response is a versioned, schema-stable envelope with
  structured errors and meaningful exit codes" — enforced for every verb the
  CLI grows by `internal/envelope` (schema-stability test pins the
  serialized field set) and the CLI tests in `cmd/seed`.
- III.I "affordance computation and admission enforcement consume the same
  rule set" — Phase 8, against `internal/admit`.

## Refinements added in Phase 12 item 6

- `docs_drift` refines exit 28 `drift` (`envelope.CodeDocsDrift`): a
  committed generated document differs from what `seed docs generate`
  renders from its table. See [`simulation.md`](simulation.md).
- `choice_diverged` is a trajectory replay divergence class, not an exit
  code: it travels in the replay result and shares the `trajectory_diverged`
  refusal (exit 26 `lane_invalid`). See [`trajectories.md`](trajectories.md).

## Components outside the envelope

`seed-mirror` (Phase 13 item 8, [`projections.md`](projections.md)) is
a separate executable that never touches a ledger, so it speaks no
envelope of its own: it consumes the one `seed project current`
printed and prints one JSON document (`exporter`, `plan`, and on
`apply` the `applied` result and any `error`) and exits 0, 1 on a
failure at the forge or in the projection, or 64 on usage. The
authority lint keeps it that way by construction: a component with no
coordination write path has no admission refusal to render.

## The machine framing

Under `seed serve` ([`platform.md`](platform.md)) an envelope travels
as the `result` of a JSON-RPC 2.0 response, byte for byte: the
framing is `machine-envelope/0`, versioned apart from
`seed-envelope/0`, and a refusal stays a result carrying the failing
envelope (its `exit` and `code` intact) rather than a transport error.
Transport errors are JSON-RPC's own codes and name no verb outcome.
