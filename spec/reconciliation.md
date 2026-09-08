# reconciliation.md — done is a verdict, and a reconciliation

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §8 (the verdict and the merge live in two systems that share
> no transaction, so "done" is an explicit reconciliation, never a
> pretended atomic step) and conformance III.G rows 1–2;
> [`docs/build-plan.md`](../docs/build-plan.md) Phase 6
> item 2; plan `plans/os-6cdc15be.md`. Implemented by the chain checks
> in `internal/transition`/`internal/admit`, the classifier in
> `internal/reconcile`, and `seed reconcile`. Red-verdict lockout is
> 6.4; sealed checks 6.3; the unattended loop that runs these
> detections on a schedule is Phase 9's maintenance.

## The chain

```
verdict.rendered(pass) → merge.requested → merge.observed → done
```

Each arrow is its own event and no code path collapses them
(III.G row 1). `verdict.rendered` was piped in 6.1
([`verdicts.md`](verdicts.md)); this stage pipes the rest.

**`merge.requested`** is a fact, not a transition: strict payload

```json
{"verdict": "<position>"}
```

citing the chain position of an admitted **pass** `verdict.rendered`
on the same subject, admitted only while the subject is in `review`.
Citing a fail verdict, a non-verdict position, or another subject's
verdict refuses by name — the chain-legality half of "a red verdict is
unmergeable"; the implementer-lockout half lands with 6.4. Capability:
`claim` or `operator` (asking for the merge is the work lane's act);
review carries no fence, so a citation refuses under the established
fence rule.

**`merge.observed`** stays the one transition to `done`
(`review` → `done`, the table row) and deepens to the observer's
forge fact: strict payload

```json
{"merged": "<full sha>", "pr": "<ref>"}
```

both presence-checked, `merged` 40–64 lowercase hex — the fact worth
recording is *which commit* the forge merged, the comparison anchor
for every divergence check below. The chain rule at admission: the
subject carries an admitted pass verdict AND an admitted
`merge.requested` citing it, in that order. Capability: the observer
lane — `observer` or `operator` (`actors.md`; the fold records the
single admitted observation `{position, sha}`, singular by
construction: the first valid observation lands on terminal `done`,
so a second can never admit, and raw-pushed extras stay anomalies).

**The override path** (6.4, plans/os-d2497eb7.md): the operator's
attributable substitute for a pass verdict, never a disguised one.
`merge.overridden` is a fact admitted only on `review` subjects from
the `operator` lane (the third no-fallback capability row), strict
payload `{"reason": "<nonempty>", "verdict": "<position>"}` citing a
standing, boundary-validated **fail** verdict on the current
submission — no verdict, a pass verdict, a stale-submission fail, and
a laundered fail each refuse by name, so the hatch overrules a red
verdict and never routes around independent verification. One
override per submission window. `merge.requested` then cites exactly
one of `{"verdict"}` or `{"override"}`, and `merge.observed` accepts
an admitted override plus a request citing it exactly as it accepts a
pass verdict plus its citation — each step its own event, nothing
collapsed. Both chain steps revalidate everything a raw-pushed
override could skip: the signer's operator standing AND the cited
fail against the current window and the verifier boundary (a
well-shaped raw override by an operator-capable key folds, and
trusting it unrevalidated would turn the hatch into a bypass).
Reconciliation surfaces a chain as `overridden` (neutral, by name)
only when the request actually cites the override — an override
beside a skipped chain stays divergence — with
`override_unverified` for a raw-pushed override whose signer,
replayed to its own position, held no operator standing.

**A raw-pushed verdict is not launderable.** In cooperative history
any active signer can plant a `verdict.rendered`, and the fold records
it like any fact — so both admitted chain steps additionally validate
the cited verdict's *signer* against the verifier boundary: it must
hold the verdict capability (checked at the tip keyring, the v0
approximation of standing at the verdict's own position; `seed
reconcile` replays position-accurately) and be disjoint from the
implementing keys. Either failure refuses by name: the chain the
verifier never judged is not the chain.

Raw-pushed chain violations verify and fold tolerated with anomalies
counted, the established posture — which is exactly what makes
divergence *detection* necessary rather than assumed impossible.

## Divergence: detected, surfaced, reconciled

Detection is split across two surfaces by what each may read.

**Record-derivable classes** — pure functions of the fold, computed by
`internal/reconcile` and surfaced in the report's reconciliation
section (projection builds read records and 5.6's declared obs inputs,
never the artifact store or the repository):

| class | condition |
|---|---|
| `merge_without_verdict` | done, or an observed merge, with no admitted pass verdict |
| `chain_skipped` | an observed merge with no admitted `merge.requested` citing the verdict |
| `unreconciled` | a pass verdict with no observed merge yet; not classified for an eval subject, whose verdict is its terminal fact ([`evals.md`](evals.md)) |
| `verdict_unverified` | a folded verdict whose signer, replayed to the verdict's own position, held no verdict grant or was an implementing key — a raw-pushed verdict that never passed the verifier boundary |
| `lesson_unverified` | a promoted, uncontested lesson whose fact does not resolve in the repository the reader holds (the anchor commit is not an ancestor of the head, or the file's bytes at the anchor do not hash to the fact's `digest`), on the hypothesis subject; evidence grade, since it needs the repository; such a fact never surfaces to a worker ([`curation.md`](curation.md)) |
| `scorecard_unverified` | a folded verdict citing a scorecard the artifact store does not hold intact, or whose stored items (id, score, uncertainty) disagree with the payload's; evidence grade, since it needs the store; the record half (a payload whose own items refute its verdict) is refused at every boundary and classifies nothing here ([`verdicts.md`](verdicts.md)) |
| `lesson_stale` | the latest admitted promotion of a lesson path, unretired, expired at the maintenance pass's declared instant for at least its stale-after threshold, on the subject `<path>@<promotion position>`; record-derived at a declared instant, computed by the maintenance loop alone ([`maintenance.md`](maintenance.md), [`curation.md`](curation.md)) |
| `independence_unverified` | a folded verdict (from `seed/4`) whose recorded level the records do not support (L2 with no declaration or a same-provider, same-family, same-harness one; L3 on a prose-only or ungated spec), or which is short of its subject's tier; at evidence grade (`seed reconcile` and the maintenance loop, which hold the repository and the store), an L3 whose receipt does not reproduce from the verifier's own checkout, a sealed subject's under an identity able to unseal ([`verdicts.md`](verdicts.md), "Independence levels") |
| `overridden` | the merge chain ran through an operator override — the sanctioned alternative, surfaced neutrally and by name, never as a divergence |
| `override_unverified` | a folded override whose signer, replayed to its own position, held no operator standing — a raw-pushed override that substitutes for nothing |

`unreconciled` is reported **neutrally**, never as an accusation: no
build carries a wall clock, so "failed" versus "pending" is an age
judgment that belongs to Phase 9's maintenance thresholds.

**Evidence-grade checks** — `seed reconcile` and the maintenance loop
only, which may read the artifact store and the repository:

- **The L3 reproduction.** A verdict recording `L3` cites a receipt
  that must recompute from the verifier's own input seam to the cited
  digest; one that does not is `independence_unverified`. A sealed
  subject's receipt carries its sealed transcripts and recomputes only
  under an identity able to unseal, and there is no silent partial
  verification (`verdict check`'s posture): `seed reconcile` opens it
  under `--key` and, meeting a sealed L3 verdict with no key or a key
  that is no recipient, refuses the run naming the subject and what it
  needs (`usage`, `not_recipient`); the maintenance loop opens it under
  the maintenance actor's key and reports a subject that key cannot
  open as skipped with the reason, never as clean.

- **Attested-head reconciliation.** The cited receipt is loaded from
  the artifact store exactly as `verdict check` loads it
  (`evidence_missing` when the store cannot produce it intact — lost
  evidence is a finding, never silence). The clean cases are observed
  sha == attested head (fast-forward) and attested head an ancestor of
  the observed sha (a true merge commit). Anything else surfaces
  `attested_divergence` naming both shas and the receipt digest — a
  *surfaced state, not a fabrication verdict*: rebase and squash flows
  land here by design in v0 because the forge mints a new sha, and
  patch-equivalence reconciliation (comparing the merged change
  against the receipt's diff hash) is the named successor, recorded in
  the decision log.
- **Target-rewrite detection** (the charter's force-push case). A
  forge force-push writes no ledger event, so detection observes the
  target ref: reconcile resolves the target as the repository's
  checked-out default branch tip (v0; a declared target ref can ride a
  later phase) and reports `target_rewritten` when the observed merged
  sha no longer resolves or is no longer an ancestor of that tip.

`seed reconcile --ledger <dir> --repo <dir> [--subject <id>]
[--artifacts <dir>] [--key <path>]` walks one subject or every folded
subject (a hypothesis subject walks its promotions instead), merges
both kinds, and returns the divergence set as envelope data at exit 0:
detection is a report, not a refusal — refusals stay operational
(usage, unreadable store, chain trouble, a sealed L3 verdict `--key`
was not given for). Phase 9's loop consumes the
same output.

## Visibility

Contracts `Version: "6"`: each entry carries
`verdict: {position, verdict, receipt} | null`,
`requested: <position> | null`, and `merged: {position, sha} | null`.
Report `Version: "3"`: the `reconciliation` section lists subjects
with record-derivable divergences by class, with counts, and names
`seed reconcile` for the evidence-grade rest. The cache mirrors the
new columns at schema generation 5. Every derivation change
republishes under a new version-bearing build id at an unchanged tip.

## Conformance mapping

- III.G row 1 (done only through the chain; each step its own event;
  no collapsing) — the chain rule at admission, the pinned
  review→done table row, and the full-chain drill.
- III.G row 2 (verdict/merge divergence is a detected, surfaced,
  reconciled state, drilled by inducing each) — the divergence
  taxonomy above with its induced drills: merge-without-verdict and
  chain-skipped from raw-pushed histories, unreconciled from a
  stalled chain, attested divergence from an observation outside the
  clean ancestry cases, target rewrite from a force-moved target ref,
  and evidence-missing from a lost receipt.
- Part II §8 ("maintenance surfaces and reconciles") — surfacing
  lands here as the report section and the reconcile verb; the
  scheduled, unattended runner is Phase 9 item 3.

## The chain's surface

`seed merge request` and `seed merge observe` reach both postures
(`--ledger` or `--remote`), routed through the same optimistic push
loop the loop verbs use. Before Phase 9 item 4 they had no CLI verb at
all and existed only through `ledger append`, the raw dev seam that
runs no rules: the chain's terminal steps had no admitted surface a
lane could drive ([`modes.md`](modes.md)).

The request's `verdict` citation is a chain POSITION, so it is
**derived** and re-examined against each refreshed tip. A citation the
refreshed view disagrees with REFUSES rather than being re-pointed: a
different citation is a different decision, not a better argument. The
observation carries no derived citation — `{merged, pr}` are the
caller's observations of the forge, and the request it follows is
checked against the fold rather than named in the payload.

Who signs which step is the keyring's: asking for the merge accepts
`claim` or `operator` (the work lane's act), observing it accepts
`observer` or `operator`.

## The forge's word before the merge (plans/os-0cd18799.md)

From `seed/8` the observer records what the forge says about the head
under review as `check.observed`, and `contract.returned` cites a red
observation as well as a fail verdict: `{"verdict": <position>}` or
`{"observation": <position>}`, exactly one, the observation the latest
on the subject and red, with no lockout recorded. `merge.requested`
refuses on either citation path while a red observation stands on the
head under review, naming it: a chain that branch protection would
hold anyway must not sit `unreconciled` behind it. A later green
observation supersedes. `merge.overridden` is untouched. The fact, the
obligation, the return and the maintenance pass that performs it are
[`observations-forge.md`](observations-forge.md).

## Merge facts from the forge (Phase 13 item 3)

`merge.observed`'s `{merged, pr}` is forge-neutral. `seed merge observe
--forge github|forgejo|snapshot --pr <ref>` fills `merged` from the
forge's pull-request state and refuses an unmerged PR; the ledger fact is
unchanged. See [`forges.md`](forges.md).
