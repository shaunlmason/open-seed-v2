# Evals: synthetic work with a known verdict

> Design authority: SEED-NEXT.md §5 (qualification binds to the runtime
> tuple; spot-check evals re-test active tuples; failing tuples get
> grants suspended by the supervisor, attributably), §16 (eval
> contracts: synthetic work with known verdicts, fixture repos, run
> through the production machinery; passing gates grants for the
> *tuple* that ran them; scheduled spot-checks); conformance III.E
> ("scheduled spot-check evals re-test active tuples; failures suspend
> grants attributably without operator ceremony") and III.O ("eval
> contracts run through production machinery against fixture repos and
> gate tuple qualification; spot-checks run scheduled"). Build plan
> Phase 10 item 2; plan `plans/os-03e47abb.md`. Implemented by
> `internal/eval`, the two verbs in `internal/keyring`, the
> qualification rule in `internal/admit`, and `seed eval`.

Item 1 ([`qualification.md`](qualification.md)) gave grants somewhere to
put the runtime tuple, the start somewhere to declare it, and the
boundary the rule that compares them. It left the set of cited tuples
to be written by hand. This spec is what writes it: an **eval** is an
ordinary contract whose verdict is known in advance, and the
consequence of that verdict is a grant or its withdrawal for the
configuration that ran it. Nothing here is a second lifecycle. An eval
is filed, offered, claimed, run, submitted and judged by the same verbs
and the same rules as any contract, and the one thing added is what the
verdict *means*.

## A definition

A definition lives at `evals/<name>/` in this repository:

- `eval.json` — `{"name", "summary", "tier", "acceptance"}`; `name`
  MUST equal the directory, and `acceptance` is the repository-relative
  path of the acceptance spec, which MUST live under the definition's
  `fixture/`.
- `fixture/` — the files the eval is worked in: the acceptance spec
  ([`acceptance.md`](acceptance.md)'s executable form) and whatever it
  checks.
- `solution/` — the reference solution: the files a correct submission
  changes, laid over `fixture/`. It MUST be non-empty.

A definition that does not say what it is files nothing: `eval.Load`
refuses a misnamed, tier-less, summary-less, misplaced-acceptance or
solution-less definition by name. `seed eval list --repo <dir>` renders
what loads and whether each is at a reviewed revision.

**The anchor is read from the repository, never declared.** A
definition is filed at its last reviewed revision: the last commit
touching its directory, whose subject carries the merged pull request
number a squash merge leaves (`… (#N)`). The filing's acceptance `ref`
is `<acceptance path> @ <that commit>` and its `gate` is
`pr/N @ <that commit>`: the same commit on both sides, which is what
[`acceptance.md`](acceptance.md)'s equality rule demands, and both are
facts about a review that happened rather than assertions. A definition
whose last commit carries no pull request number, or which has
uncommitted changes under its directory, is **not at a reviewed
revision** and refuses `ungated` (exit 18): armed content that has not
been through the gate runs nowhere.

**The known verdict is proven, never asserted.** `seed eval check
--repo <dir> --eval <name>` takes a verifier workspace at the anchor
([`verdicts.md`](verdicts.md)'s clean per-run checkout, removed either
way), runs the acceptance commands through the verifier's own runner
and requires them RED, overlays `solution/` onto `fixture/` and runs
them again and requires them GREEN. An eval whose unsolved fixture
already passes decides nothing and refuses **`eval_vacuous`**, a
refinement of exit 19 `spec_unrunnable` ([`envelope.md`](envelope.md)):
the family's answer, "this spec cannot decide", is already right, and
the extra word says which way. A solution that stays red cannot
reproduce the known verdict and refuses `checks_red` (exit 20). An
acceptance spec with no parseable commands refuses `spec_unrunnable` as
it would at render.

## An eval is an ordinary contract

`seed eval file (--ledger <dir> | --remote <repo> --state <dir>) --repo
<dir> --eval <name> --key <path> [--tuple <json>]` appends two records:

- `intent.filed` with the ordinary `intent`, `tier` (the definition's),
  `budget` and `routing`, plus the marker **`eval`**:
  `{"name": "<definition>"}`, or `{"name", "tuple": {…}}` when the
  filing is a spot-check of one configuration. The tuple is advisory:
  it names what the re-test is *for*, and the run declares what
  actually ran. From Phase 11 item 2 the marker may be **bound**:
  `{"name", "tuple"?, "lesson": "<h-id>@<position>", "carrier":
  "<path> @ <commit>"}`, both fields or neither, filed with `seed eval
  file --for-lesson --carrier` ([`curation.md`](curation.md)): the
  eval is a counter-trajectory constructed against a candidate lesson
  at an exact revision, and the promotion boundary requires the pass
  it cites to carry exactly that binding. A bound subject renders only
  when the carrier commit is an ancestor of the submission head, else
  `seed verdict render` refuses **`carrier_absent`** (a refinement
  under `checks_red`, [`envelope.md`](envelope.md)): a
  counter-trajectory judged without the candidate applied proves
  nothing about it.
- `contract.specified` with `{"acceptance": {"ref", "executable":
  true, "gate"}}` at the anchor.

The subject id is `eval-<12 hex>` derived from the definition's name,
the tuple under re-test and the count of evals already filed for that
pair, so a second filing for a satisfied window gets a new id and a
duplicate in the same window refuses at the boundary.

**The marker activates at `seed/3`** ([`protocol.md`](protocol.md)). The
lifecycle fold reads it into `SubjectState.Eval` (`{Name, Tuple}`) at
`seed/3` positions only, dropping and counting an advisory tuple that
does not parse rather than folding a partial configuration; at an
earlier tip admission refuses a filing that carries it, naming the
version, so a filing a `seed/2` validator's fold would silently read as
an ordinary contract is never admitted.

From there the lifecycle is the lifecycle. **No transition row moves.**
The supervisor's offer (`offer.published`, never scoped by `tuples`: an
eval is how a configuration proves itself, so no set may gate who takes
it), the worker's claim and reservation, the supervisor's `run.started`
declaring the worker's tuple, the submission, and the verdict under L1
independence with a receipt, all as [`lifecycle.md`](lifecycle.md),
[`offers.md`](offers.md), [`executors.md`](executors.md) and
[`verdicts.md`](verdicts.md) say. Two things differ, and both are
consequences of what an eval is:

- **The set rule is exempt on eval subjects.** `run.started` on an eval
  admits any declared tuple, a disqualified one included; on a non-eval
  subject the set rule stands unchanged. The eval is what a
  configuration must pass to enter the set, so the set cannot be what
  gates the eval.
- **The verdict is the eval's terminal fact.** No merge is owed:
  `verdict.unmerged` is not projected for an eval subject
  ([`obligations.md`](obligations.md)) and `unreconciled` is not
  classified for one ([`reconciliation.md`](reconciliation.md)). The
  consequence of the verdict is the qualification below, not a merge.

## The consequences: `actor.qualified` and `actor.disqualified`

Two actor verbs, the catalog's `actor.qualified` ("cites eval results
and the runtime tuple") and its inverse, both defined at `seed/3`
([`actors.md`](actors.md)):

| verb | subject | payload (strict) |
|---|---|---|
| `actor.qualified` | an enrolled fingerprint | `{"capability", "tuple", "contract", "verdict"}` |
| `actor.disqualified` | an enrolled fingerprint | `{"capability", "tuple", "contract", "verdict", "reason"}` |

`capability` is **`claim`**, or from `seed/4` **`verdict`** for a
calibration ("Calibration" below), and nothing else: an eval proves a
configuration for work or for judgment, never another authority, so a
supervise key cannot mint operator standing through a green eval. A
`verdict` qualification cites the calibration's verdict, names the
verifier that rendered it and the tuple the render declared, never a
window's holder or a window's declaration. `tuple`
is the strict five-field runtime tuple; `contract` is the eval subject;
`verdict` is the cited verdict's chain position as a string; `reason`
is required on a disqualification and refused on a qualification (the
cited verdict is the reason). Both accept
`supervise` or `operator`: the capability table's first actor rows the
supervisor holds, because §5 makes suspension the supervisor's
attributable act with no operator ceremony, and operator stays the
standing override.

**In the keyring** a qualification is a grant with evidence: it grants
the capability if absent, adds the tuple to the holder's admissible set
(`GrantTuples`), marks the capability as ever cited, and records
`{capability, tuple, contract, verdict, ts}` with the event's own `ts`.
The sealer disjointness a grant obeys applies. A disqualification
removes the tuple from the admissible set, refusing when the actor holds
no admissible grant citing it ("nothing to disqualify"), keeps the mark,
and records the same with its reason. At a `seed/2` position either verb
fails verification as `bad_actor_event` at its position, exactly as a
`seed/2` validator fails it.

**At the boundary** the qualification rule reads the lifecycle, and
every refusal is its own row with nothing appended:

- the cited contract is an eval, and it is **bound** to the definition
  it names: its acceptance spec is that definition's own fixture
  (`evals/<name>/fixture/…`), executable and gated. The boundary
  binds by path, since it reads no repository; the derivation below
  binds to the reviewed anchor. A contract that carries the marker with
  any other spec is an eval in name only, and its verdict, however
  green, qualifies nobody;
- `actor.qualified` cites the eval's **authenticated pass**: the latest
  verdict is a pass whose signer held a verdict grant **at the
  verdict's own position** (the keyring replayed there, so a raw-pushed
  pass from an ungranted key does not become authentic when the key is
  granted later, and a legitimate pass does not stop being one when its
  signer is suspended afterwards) and was no implementing key;
  `actor.disqualified` cites a **fail** on the eval authenticated the
  same way;
- the claim window the verdict's submission cites carries an admitted
  `run.started` declaring exactly the cited tuple, all five fields: a
  qualification is for the configuration that ran, never another;
- for `actor.qualified` the subject is that window's **holder**: the
  supervisor declares, the holder executes, and the holder is who the
  eval qualifies;
- the record's `ts` is not before the cited verdict's;
- no earlier record on the actor cites the same verdict with the same
  kind: one verdict, one consequence;
- the signer holds `supervise` or `operator`, else `out_of_grant`.

**A mint needs a recomputed receipt.** Admission cannot tell an
invented digest from a real one ([`verdicts.md`](verdicts.md)), so the
derivation that owes a mint retrieves the cited receipt from the
artifact store and recomputes it from the submission head with the same
function `seed verdict check` runs, and mints only when the digest
reproduces and every transcript exited zero. A receipt that does not
retrieve, does not reproduce, or carries a red transcript mints nothing,
and the derivation says which by name (`receipt_missing`,
`receipt_mismatch`, `checks_red`; `receipt_unchecked` when it had no
store or repository to recompute against, or the subject carries sealed
checks it does not unseal).

**A disqualification is tuple-wide.** An authenticated fail under T
owes one `actor.disqualified` for EVERY actor whose admissible set holds
T, each citing the verdict: a configuration that failed is not one
configuration per holder.

**The bridge does not reopen.** [`qualification.md`](qualification.md)'s
set rule reads an empty set as the bridge. Refined here: an empty set
that was **ever cited** admits nothing, and the refusal says every cited
configuration is disqualified. An actor whose only cited configuration
failed must pass an eval again, which the exemption above lets it do.

## Calibration

A **calibration** is an eval for verifier keys (plans/os-2e34f66a.md
D5; the charter's "rubric calibration runs against a human-scored gold
set with automatic authority suspension on drift"). Its `eval.json`
carries `"kind": "calibration"` and `"calibration": {"gold":
"sha256:<digest>", "floor"?}`: the digest of the human-scored gold
scorecard (`{"items": [{"id", "score"}]}`, JCS-canonical), its
`fixture/` a rubric spec ([`acceptance.md`](acceptance.md), "The
rubric"), its `solution/` as any eval's; `seed eval check` proves the
fixture red and the solution green as for any definition. **The gold
is never in the repository**: the verifier under calibration works in
a clone of the whole tree, so a checked-in gold would be readable from
its workspace, and a drifting verifier could copy it and always meet
the floor. The operator lane holds the gold outside the tree and
supplies it to the derivation (`seed eval status|act --gold <dir>`,
one `<name>.json` per calibration definition). With none supplied the
contract owes nothing and the derivation notes `gold_missing`; a gold
whose canonical digest is not the definition's commitment refuses to
score, `gold_mismatch`, and performs nothing. Neither offers the
calibration either (review finding on the task PR): a ready
calibration whose gold the derivation cannot see is not offered, so
`seed eval act` never dispatches work whose verdict nothing could
compare, and the note is what is owed.

A calibration's filing marks itself: `seed eval file` writes `"kind":
"calibration"` into the eval marker (`{name, tuple?, lesson?,
carrier?, kind?}`, the kind a `seed/4` field, neither absent nor
`calibration` refusing), the fold carries it, and the boundary holds a
`verdict` qualification to it: a qualification citing an ordinary
eval's verdict refuses, because an ordinary eval proves a
configuration for work and says nothing about the verifier's judgment,
so citing one would mint verdict authority past the calibration gate
(review finding on the task PR). A calibration filed without the mark
owes nothing and the derivation notes `kind_unmarked`.

Filed with `seed eval file` and worked like any eval (the solution
applied, the submission made), a calibration is judged by the verifier
under calibration, rendering with its scorecard
([`verdicts.md`](verdicts.md), "The rubric and the scorecard"), and
`eval.Due` compares the payload's items to the gold item by item,
whichever way the verdict went. **Agreement** is the fraction of gold
items the verifier scored the same at `low` uncertainty; `high` is not
agreement, since the verifier declined to decide. **The floor is
policy on this spec surface, never a runtime argument**: the floor is
`0.8`; `eval.CalibrationFloor` mirrors it and a drill pins the two as
the tier table is pinned to [`tiers.md`](tiers.md); a definition may
raise it (`"floor"`) and never lower it (a definition below the
spec's floor refuses at `Load`); nothing an invoker of `seed eval
act` passes can move it. At or above the floor `Due` owes
`actor.qualified` for capability **`verdict`** on the verifier's
declared tuple; below it, **drift**: `actor.disqualified` for
`verdict` on that tuple, tuple-wide (every verifier holding it) and
always the verifier that rendered, even on its first calibration when
nothing cites its tuple yet: a verifier holding `verdict` by a bare
grant renders under the bridge, the keyring admits that one
disqualification as the act that closes it (`EverCited`, an empty
admissible set), and the verifier renders nothing declared until a
calibration passes again (review finding on the task PR), and
a defect contract filed by the dispatcher naming the contract and the
disagreeing items, `intent.filed` under the stable id
`d-<sha256(calibration_drift, contract)[:16]>` (the maintenance loop's
idempotent shape: filed once per contract, and the boundary refuses
the duplicate). That is **automatic authority suspension on drift**:
the set rule applies to `verdict.rendered`'s declared tuple exactly as
to `run.started`'s ([`qualification.md`](qualification.md), "The set
rule at render"), so the verifier's renders under the drifted
configuration refuse `out_of_grant` until a calibration passes again,
while a calibration eval itself still admits any declared tuple, being
where a configuration proves itself. Spot-checks age `verdict`
qualifications exactly as `claim` ones, re-filing the calibration at
the interval.

Refused: `actor.suspended` from a machine lane. Suspension ends a key's
standing across every contract and is operator-only; withdrawing the
`verdict` set for one configuration is the shape item 2 built, and it
re-qualifies by the same road. **No calibration definition is
shipped**: one whose gold the tree carries is the leak above, one
whose gold the implementer discards is unusable; a deployment commits
the definition with the digest and holds the gold, and the drills
build theirs in temporary repositories with the gold outside them.

## What the chain owes: `seed eval status` and `seed eval act`

`eval.Due(now, after)` derives, at a DECLARED instant, the acts the
chain owes and the lane each is owed by. It first binds every marked
contract to its definition **at the reviewed anchor**: the acceptance
ref must equal the shipped definition's `Anchor.Ref`, executable and
gated; a contract that names an eval and cites anything else, or names
a definition the repository does not ship, is noted `unbound` and
neither offered, minted from nor disqualified from. Then:

| act | when | lane |
|---|---|---|
| `offer.published` | an eval is `ready` with no live offer (expires a day past `now`; scoped by policy, [`ranking.md`](ranking.md): the configuration under re-test when the filing names one, else the strongest `claim` tuple, else unscoped with a `ranking_empty` note) | `supervise` |
| `actor.qualified` | an authenticated pass whose window declared a tuple, not yet cited, whose receipt recomputes green | `supervise` |
| `actor.disqualified` | an authenticated fail, for every holder of the declared tuple not yet cited | `supervise` |
| `intent.filed` + `contract.specified` | a spot-check is due (below) | `dispatch` |

**Spot-checks age from the record, against the declared instant.** For
each admissible `(actor, tuple)` whose latest qualification is older
than `after` (`--spot-check-after`, default 168h; `0` disables), the
derivation files a re-test of the eval that qualification cites, naming
the tuple, unless an eval naming that tuple is already open. Age is the
qualification record's own `ts` measured against `--as-of` (default
now), never a clock read inside the derivation, so a run replays; a
qualification dated **after** the declared instant is due at once and
noted as an anomaly, because a lie about time cannot postpone a
re-test. A spot-check taken and failed drives the disqualification; one
taken and passed mints a new qualification whose `ts` is what the next
window measures from. A tuple granted by hand with no qualification has
no eval to re-run.

`seed eval status (posture) --repo <dir> [--artifacts <dir>] [--as-of
<rfc3339>] [--spot-check-after <dur>] [--timeout <dur>]` renders the
derivation: `owed` and `notes`. **`seed eval act`**, the same flags plus
`--key`, performs the subset of `Due` its key's grants admit and reports
the rest as `owed` by their lane: the supervisor offers, mints and
disqualifies; the dispatcher files and specifies; the operator does all.
A key holding no eval lane at all refuses every act `out_of_grant` (exit
14) with nothing appended and never retried. An act the boundary
refuses is reported under `refused` and the invocation exits
`chain_invalid`. Each act is one derivation at one instant: what the
performed acts make due next (a filed spot-check's offer, say) surfaces
on the next. `seed maintain run` is unchanged and appends none of
these; the acts are the supervisor's and the dispatcher's, attributably.

## Exit codes

No new exit. `ungated` (18) for a definition not at a reviewed
revision; `spec_unrunnable` (19) with the `eval_vacuous` refinement;
`checks_red` (20); `not_found` (4) for an unknown definition;
`out_of_grant` (14) for a key holding no eval lane; `chain_invalid`
(8) for a qualification the boundary refuses.

## Deferred, by name

Verifier calibration and authority suspension for verifiers (item
4), independence levels per tier (item 3), re-triage and unedited-
approval rates (item 5), and container or cloud adapters. Ranking of
tuples, deferred here as D9, landed with Phase 13 item 7
([`ranking.md`](ranking.md)): the offers `Due` owes carry the
strongest by policy. A receipt
carrying sealed checks is not unsealed by the derivation, so an eval
above the trivial tier mints nothing until that seam exists.

## Conformance mapping

- III.E "Scheduled spot-check evals re-test active tuples; failures
  suspend grants attributably without operator ceremony": `Due`'s
  spot-checks and tuple-wide disqualifications, performed by the
  supervisor's `seed eval act` under its own grant; drilled on a local
  ledger and in small-team mode against a remote.
- III.O row 1 "Eval contracts run through production machinery against
  fixture repos and gate tuple qualification; spot-checks run
  scheduled": the shipped `fix-the-check` definition, `seed eval check`
  proving its verdict, and the end-to-end drill through the production
  verbs with the mint gated on a recomputed receipt. "Scheduled" is
  what a Routine invoking `seed eval act` at an interval provides; the
  derivation is what makes such a run replayable.
- III.E row 6 (out of grant on drift): item 1's rule, now fed by mints
  rather than hand grants, with the closed-bridge refinement.
