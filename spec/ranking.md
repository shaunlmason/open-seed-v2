# Ranking: the strongest tuples by policy

Status: normative for `next/**` (plans/os-c7554f18.md; build plan Phase
13 item 7; charter III.J row 3 "the planner lane receives the strongest
tuples by policy", §II.9's scheduling inputs). Last verified: Phase 13.

The qualification set ([`qualification.md`](qualification.md)) says
which configurations an actor may run under; it is unranked, and every
qualified tuple is equal in an offer. This document adds the policy
that orders them: a **ranking**, derived from the chain's own eval
facts, that the supervisor writes into offers as the `tuples`
scheduling input [`offers.md`](offers.md) defined. It is policy, never
admission: nothing here refuses a record, a tuple's absence from the
list changes nothing about what the boundary admits, and the set rule
at `run.started` is unchanged.

## The derivation

`internal/ranking.Derive` reads the verified prefix and its keyring
and nothing else: every applied `actor.qualified` and
`actor.disqualified` in chain order (the capability, tuple, contract
and verdict each cites, at the record's own position and `ts`), and
the keyring's admissible set and standing per actor. Per capability
(`claim`, qualified by evals; `verdict`, qualified by calibrations) it
yields the ordered entries: the tuple, its score, the `ts` of its
latest pass, its active holders, the evidence in chain order, and the
agreement figure when the gold refined it. The same chain derives the
same bytes; an unrelated append changes nothing.

## The policy table

The table is the policy. `internal/ranking.Rules` states the same rows
in the same words, and a drill holds the two together, so the policy
cannot change in the code without this table or here without the code.

| rule | policy |
|---|---|
| `score` | the count of qualifying evidence since the tuple last held: the first pass is the mint, later passes are spot checks, and a verdict tuple's passes are calibrations |
| `tie` | the latest pass's ts, newer first, then the tuple's canonical JSON, lower first |
| `excluded` | a tuple whose latest fact is a disqualification, or whose every holder is suspended or revoked: absent, not last |
| `agreement` | with the gold supplied, verdict tuples of equal score order by their mean calibration agreement, higher first, unrefined entries after; without it the field is null and the ranking says so |
| `instant` | the declared instant, never a clock: the projection derives at the latest qualification fact's ts, the verbs at --as-of or the offer's own instant |

Read plainly: a tuple's evidence is every `actor.qualified` citing it
since its latest `actor.disqualified` (a disqualification resets the
count, and a tuple whose latest fact is one does not appear); the
first of those is its mint, the rest its spot checks (for `verdict`
tuples every one is a calibration); more evidence ranks first, then
the newer latest pass, then the canonical JSON of the five fields in
their declared order. A tuple no active actor holds in its admissible
set does not appear either: suspension removes a holder, revocation
removes it for good, and re-enrollment brings a suspended holder's
tuple back with its evidence intact. Refused by construction: a clock
read (the instant is declared), a weight the table does not state, a
suspended or disqualified tuple anywhere in the list.

**Agreement.** The gold scorecards live outside the tree
([`evals.md`](evals.md) "Calibration"). When a derivation is handed
them, each calibration pass is scored against its gold with the same
`Agreement` figure the eval derivation uses, the entry carries the mean
over its scored passes as a three-decimal string, and `verdict` tuples
of equal score order by it; the ranking says `"agreement_refined":
true`. Without gold every `agreement` is `null`, the flag is `false`,
and the ranking is by passes alone. The `claim` ranking is never
refined: evals have no gold.

## The offer input

`seed offer publish --strongest <n> --capability <c>` fills the offer's
`tuples` scope with the top `n` of the ranking for that capability,
derived at the offer's own instant from the ledger the supervisor
publishes into, and **refuses** `ranking_empty` (exit 4) when nothing
ranks: an unscoped offer is the supervisor's explicit choice, never a
fallback, so widening happens by omitting the flag, not by policy
running out. `--strongest` and `--tuple` are two ways to write the
same scope and refuse together; `--strongest` takes exactly one
`--capability`, and that capability is `claim`: the scope is matched
against the taker's claim grants ([`offers.md`](offers.md)), so a
verdict tuple named there would be one no claimer cites, and the verb
refuses it as usage. The verdict ranking is read by the projection and
the doctor, for the verifier a supervisor calibrates next. The payload is
unchanged from [`offers.md`](offers.md): the scope is the input Phase
10 item 1 defined, now filled by policy, and admission judges it by the
existing scope rule.

`seed eval act` publishes by the same policy: the offer `Due` owes a
ready eval carries the tuple under re-test when the filing names one
(a spot check or calibration tests the configuration it was filed
for, so the offer names that configuration and no other), and
otherwise the top of the `claim` ranking; on an empty ranking the
offer is unscoped, the bootstrap a first eval needs, and the report
notes `ranking_empty` so the choice is visible. Nothing else `Due`
owes changes.

## Resume: the prior submitter's tuple

The ranking is per capability and never reads a subject's history.
One subject-shaped question sits beside it (plans/os-29e2fef2.md;
[`observations-forge.md`](observations-forge.md)): a contract returned
on the forge's word records no lockout, so the prior submitter is the
natural next claimant, and it already holds the branch, the threads
and the context. `internal/ranking.Resume` derives its configuration
from the chain: the subject's latest applied `contract.returned`; when
it cited an observation, the submission it returned, the claim window
that submission's `fence` names, the tuple the window's admitted
`run.started` declared, and the holder that took the window. It yields
the tuple, the holder, the positions and the **consumed offer**: the
latest offer before the claim whose signer held the supervise boundary
at its own position and whose scopes the holder met there, the two
predicates the listing applies ([`offers.md`](offers.md) "Foreign
offers are inert"), so a raw-pushed offer nobody granted never lends
its scope. The start read is the first at the fence that the run rule's own
derivation (`admit.RunStartValid`) accepts, since the tolerant fold
keeps a raw-pushed start too. Record-derived like the ranking, never
a clock; no admission rule reads it; the policy table above is
unchanged, because the preference is per subject, not a ranking rule.
A declared tuple needs a chain where tuple semantics apply: `seed/2`
through `seed/4`, and again from `seed/9`
([`protocol.md`](protocol.md)); a window opened at `seed/5` through
`seed/8` declared none, and the resumption says so.

It **refuses by name** when any link is missing: the latest return was
the verifier's (a fail verdict routes to whoever is strongest, not to
whoever failed), no return stands, the window carried no admitted
`run.started` or one that declared no tuple, the holder is suspended
or revoked, or the holder's admissible `claim` grant no longer cites
the tuple. The last is the listing's own test, `Eligible` against an
offer scoped `{capabilities: [claim], tuples: [tuple]}` at the
subject's tier: `actor.disqualified` removes a tuple while the actor
stays active, and a preference for such a tuple would be an offer its
worker cannot see, the stall this derivation exists to remove.

`seed offer publish --resume` fills the `tuples` scope with the
resumption's tuple, alone or beside `--strongest <n>` and `--tuple`
(a set, the prior tuple first in the payload, a duplicate folded), and
refuses `resume_empty` (exit 4, the `ranking_empty` posture) naming
the missing link when nothing derives: an unscoped offer stays the
supervisor's explicit choice. The result names the resumption
(`tuple`, `holder`, `return`). The payload is unchanged from
[`offers.md`](offers.md); admission judges it by the existing scope
rule. The maintenance pass publishes by the same derivation on every
subject it returned ([`maintenance.md`](maintenance.md) "Re-offer").

## The projection, the doctor, the report

- **`ranking`** (`ranking.json`, version 1): the derivation at the
  latest qualification fact's `ts` (never the tip's: an unrelated
  append changes nothing) — `as_of` (empty until a qualification
  exists), `agreement_refined` (always `false`: the projection reads
  no gold, which is outside the tree), and `capabilities` with both
  capabilities present, each an ordered list of entries.
  Byte-identical on the same evidence; a chain carrying no
  qualification builds two empty lists.
- **`seed doctor --ledger <dir>`** names the top tuple per capability
  under `ranking` (`null` where nothing ranks); without `--ledger` the
  section is absent, since the doctor otherwise reads the declaration
  alone.
- The report's `lanes.planner` gains `strongest`: the `tuples` scope of
  the latest applied `offer.published` that carries one, absent when no
  offer has (the input the planner lane last received by policy).

## Conformance mapping

- III.J row 3 "the planner lane receives the strongest tuples by
  policy": the ranking as the record-derived policy table, `--strongest`
  and `Due`'s offers carrying its top, the projection and the doctor
  making it visible, drilled at the derivation (order, exclusion,
  determinism, agreement), at the verbs and at the projection; the
  metrics half of the row is Phase 10 item 5's.
- §II.9 "executor cost class (mechanical work to cheap tuples, hard
  work to strong ones)": the supervisor chooses `n` and the capability
  per offer; the ranking says which tuples are strong, the offer says
  which work they get.
