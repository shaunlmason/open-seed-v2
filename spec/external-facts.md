# external-facts.md: the one vocabulary of facts owned elsewhere

> Status: v0, normative for `next/**`. Authority:
> [`SEED-NEXT.md`](../SEED-NEXT.md) §II.4 "External facts" (mirrors
> of external authorities enter the ledger as observations), §II.11
> (the observer records what an external authority did), §II.15
> (mirrors and dashboards propose, the ledger decides); conformance
> III.D rows 5, 6 and 7. Plan:
> [`plans/os-b45c308d.md`](../plans/os-b45c308d.md). Implemented by
> `internal/externalfact` (the catalog and the read-only sources),
> `internal/keyring` (the signer rows), `internal/admit` (the rules),
> `internal/authoritylint` (the component boundary), `mirror` and
> `seed-mirror` (the export side), and `seed request file` (the ingress).

## The claim

Two systems hold facts about one piece of work: the ledger, which
decides, and an external authority (a forge, a reviewer, a merged pull
request), which acts on its own terms. The ledger never pretends to
control the second. What crosses from it is an **observation**: a
signed record that something happened elsewhere, recorded by a
governed identity holding the observer capability (or the operator,
who stands in every lane but the no-fallback rows), judged at
admission like any other event, and treated by every consumer as a
fact with a bounded consequence, never as a verdict, a grant, or a
command.

This file is the closed inventory of those verbs. `internal/externalfact.Catalog`
mirrors the table below row for row and the pin runs both ways: a
catalogued verb absent from this table, a table row absent from the
catalog, a row whose signer set differs from the keyring's, or a verb
the keyring accepts for `observer` that the table does not list, each
fails a test.

## The table (normative)

| verb | external authority | signers | source | only ledger consequence | since |
|---|---|---|---|---|---|
| `check.observed` | the forge's checks and review on the head under review | `observer`, `operator` | `seed check observe --forge` through the read-only source, or given by hand | the subject's standing observation; while red on the submission's head, `merge.requested` refuses and the dispatch lane owes `submission.unmergeable`, which only `contract.returned` discharges; never a verdict, a transition or a discharge ([`observations-forge.md`](observations-forge.md)) | `seed/8` |
| `curation.lesson.promoted` | the lesson pull request's merge | `observer`, `operator` | the merged pull request the payload anchors | the hypothesis it cites is promoted for the knowledge projection; no lifecycle state changes ([`curation.md`](curation.md)) | `seed/1` |
| `curation.lesson.retired` | the revert's merge, a later promotion, or the expiry the promotion declared | `observer`, `operator` | the merged revert or the superseding promotion the payload cites | the cited promotion is revoked for the knowledge projection; no lifecycle state changes ([`curation.md`](curation.md)) | `seed/1` |
| `merge.observed` | the forge's merge of the pull request | `observer`, `operator` | `seed merge observe --forge` through the read-only source, refusing an unmerged pull request, or given by hand | review to done, and only behind an independently valid pass or override and a citing `merge.requested`; a merge observed without them is a reconciliation divergence, never a laundered verdict ([`reconciliation.md`](reconciliation.md)) | `seed/1` |
| `plan.approved` | the plan pull request's merge, a gate a human holds | `operator` | the merged plan pull request the payload anchors | the plan the tier requires is on record; no lifecycle state changes ([`plans.md`](plans.md)) | `seed/1` |
| `workflow.merged` | the workflow pull request's merge | `observer`, `operator` | the merged pull request the payload anchors | the proposal it cites is registered for the flywheel; no lifecycle state changes ([`flywheel.md`](flywheel.md)) | `seed/1` |

`plan.approved` stays operator-only: the plan gate is one a human
holds, and giving the observer lane a fallback there would let a
machine lane attest a human's review. The other five are the
`merge.observed` posture.

## Outside the table, and why

A verb is an external fact only when the thing it asserts is owned by
a system the ledger does not control. These are deliberately not in
the table:

- **Verdicts** (`verdict.rendered`, `verdict.deferred`): the verifier's
  own judgment of the acceptance spec, made in its own workspace
  ([`verdicts.md`](verdicts.md)). The forge never renders one.
- **Requests** (`request.filed`, `request.answered`): a proposal from
  a projection surface and the dispatcher's answer
  ([`requests.md`](requests.md)). The proposal asserts nothing about
  the world; it asks.
- **Offers, claims, budget facts, run facts, checkpoints**: internal
  scheduling and metering, every signer a lane the ledger governs.
- **Erasure** (`artifact.erased`): a governance act a human answers
  for, not an observation.
- **Sealed checks** (`check.sealed`): the sealer's commitment, made
  before the work; the forge's word on it comes back as
  `check.observed`.

## Observations are not control (normative)

For every row:

1. **Admission enforces the signer set.** The keyring row is the
   table's; a key holding none of the capabilities refuses at exit 14
   `out_of_grant`. A consumer that reads a fact re-judges the signer at
   the record's own position (the keyring replayed there), so a fact
   whose signer lost standing later stays admitted and a fact appended
   raw by an ungoverned key is never a fact.
2. **No observation is a transition, a verdict, or a discharge.**
   `check.observed` has no row in the transition table, appears in no
   obligation's discharger set, satisfies no acceptance command and
   creates no verdict. `merge.observed` is the one observation that
   moves a subject (review to done), and only behind a pass or
   override the boundary validated and a `merge.requested` citing it;
   observed without them it is `chain_skipped` or
   `merge_without_verdict`, a divergence the reconciliation surface
   names and nothing repairs by accident.
3. **An observation narrows, never widens.** A red `check.observed`
   makes `merge.requested` refuse and raises a debt the dispatch lane
   owes; a green one legalizes nothing, discharges nothing, and leaves
   the subject's affordances, obligations, budget, submission and
   verdict exactly as a chain without it. The invariance drill pins
   both directions.
4. **No observation performs what it reports.** The observer reads
   through the read-only source and appends a record; it never merges,
   reruns, cancels, labels or protects. The source lint pins the
   reading side, the authority lint the component.

## Sources (normative)

`externalfact.Source` is the one interface the observer reads a forge
through: `Merged(pr)` and `Checks(pr)`, nothing else. The GitHub and
Forgejo sources hold a token that reads, issue HTTP GET alone, and
live in a package that imports neither the mirror nor the protections
adapters, so the observation component holds no mutating client.

**One named variance.** GitHub exposes review-thread resolution only
through GraphQL, which is a POST. The GitHub source sends exactly one
operation there, a paged `query` over the pull request's review
threads, through a helper that refuses any operation not beginning
with `query`; the lint allows that POST in that helper alone and the
suite proves a planted mutation refuses before it is sent. Forgejo has
no thread resolution and the observation says so (nil, never zero,
[`forges.md`](forges.md)).

The snapshot source answers the same shape from a file, so every
drill runs credential-free. Tokens come from an environment variable
named on the command line (`--token-env`), never from a flag, a
declaration or a fixture.

## The observation-control lint (normative)

`internal/externalfact`'s production files are parsed under `go test`:
no `os/exec` import; no import of `mirror` or
`internal/protections`; no HTTP method but GET outside the GraphQL
helper, and no method but POST inside it; no HTTP method string
literal. The lint self-checks against a planted POST and a planted
mutator import, and the runtime helper refuses a planted mutation.

## Conformance mapping

- III.D row 7 "External facts enter only as observations by governed
  observers; nothing treats an observation as control": the table's
  signer rows enforced at admission (`TestExternalFactCatalogPins`),
  the invariance drill (`TestGovernedObservationIsNotControl`), the
  governed-observation suite over fake GitHub and Forgejo APIs
  recording every request (`TestGovernedObservationOverTheForges`),
  and the source lint (`TestObservationControlLint`).
- III.D rows 5 and 6: the export side and the component boundary,
  [`projections.md`](projections.md) "Components" and "The mirror".
