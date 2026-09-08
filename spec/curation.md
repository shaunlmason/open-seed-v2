# Curation: the staged learning pipeline's ledger half

> Design authority: SEED-NEXT.md §II.12 ("A poisoning-resistant
> pipeline": trajectories are untrusted inputs; the pipeline is staged,
> with distinct storage and distinct gates between stages; workers
> append candidate observations only, and promotion authority is not
> grantable to implementing lanes; dead ends record failure condition
> and environment; hypotheses carry applies-when conditions, support
> from more than one non-failed trajectory and more than one actor
> where the family allows it, exceptions and provenance; validation
> runs an adversarial evaluation for behavior-changing lessons;
> conflicting evidence is a first-class contested state, never
> averaged, and contested lessons do not surface) and conformance
> III.K rows 1, 2, 3, 4 and 5 and III.I row 5. Build plan Phase 11 items
> 1 and 2; plans `plans/os-f30ee0d3.md` and `plans/os-96850e5a.md`.
> Implemented by `internal/curation` (the shapes, the id derivation,
> the predicate, the gate registry, the fold, the surfacing set, the
> lint), the keyring's `curate` capability and its rows, the admission
> rule `curation`, the `knowledge` projection, `seed knowledge`, the
> bound eval marker and the claim-time delivery.

Before this spec the catalog named four `curation.*` verbs and the tree
implemented none; the curator held no grant; and stage one already
existed unnamed, as the packet's `findings`. This spec names the
stages, gives three of them a ledger fact with its own gate, and puts
the proposal behind a grant nothing implementing can hold.

## The four stages

| stage | storage | fact | who appends |
|---|---|---|---|
| observations | the ledger, on the contract | the packet's `findings` ([`packets.md`](packets.md)) and **`curation.deadend.recorded`**; **`curation.deadend.retired`** and **`curation.deadend.unretired`**, the curator's judgment that the environment moved | the window's holder (`claim`), `operator`; the two environment acts **`curate`** alone |
| hypotheses | the ledger, on a hypothesis subject | **`curation.hypothesis.proposed`**, **`curation.hypothesis.contested`** | **`curate`** alone, no operator fallback |
| validated lessons | `knowledge/lessons/<id>.md`, merged by PR, and the ledger | **`curation.lesson.promoted`** (the PR observation), **`curation.lesson.retired`** (the observation that the promotion is revoked) | `observer`, `operator` |
| policy | role, skill and workflow patches on the protected surface, by PR | none in this item | the governance root, through gates |

**No stage skips, by citation.** A hypothesis cites the observations it
rests on and the boundary re-judges each from the record; a promotion
cites the hypothesis it promotes and the boundary re-judges that the
citation passed. A lesson that cites no admitted hypothesis folds
`unbound`, never as a lesson. Fold presence is never proof of admission
(the `RunStartValid` posture), and neither is a tolerant lifecycle fold:
a raw-pushed dead end outside a window supports nothing; a dead end
inside a window that a grantless key's raw `claim.taken` opened supports
nothing either, since the window is re-judged at its fence (the claim on
the subject, signed by its holder, who held `claim` at that prefix:
`curation.WindowAdmitted`); a raw-pushed proposal neither promotes nor
reserves its subject (an unadmitted proposal folds as an anomaly, so the
duplicate rule, the contest and the promotion read admission, never
presence); and a raw-pushed pass clears no authenticated fail.

Refused: a lessons store in the ledger. A lesson is a document humans
and lanes read, it changes behavior when it lands, and the charter
routes promotion through a PR precisely so that rollback is a revert;
the ledger carries the observation of that merge, the `plan.approved`
posture. `lesson.retired`, refused by item 1 as a verb with no reader,
landed with item 4 once it had three: the surfacing set, the
projection and the maintenance loop ("Expiry, retirement and
applicability" below).

## The facts

**`curation.deadend.recorded`**, on a contract, carries the strict
object `{"fence", "tried", "outcome", "condition", "environment",
"pointer"?}`: the packet finding's shape plus the charter's failure
condition and environment. Every field non-empty; the fence the active
claim window's; the pointer, if any, an anchored path (`"<path> @
<commit>"`, never a bare path). Admitted only from the window's holder
inside its window: outside a window, from a non-holder, or citing a
stale fence it refuses naming the part. It is a candidate and nothing
more, since it has no field a conclusion could live in.

**`curation.hypothesis.proposed`**, on subject **`h-<12 hex>`**
derived from the claim AND its exceptions (the first twelve hex digits
of the SHA-256 of the whitespace-normalized claim text joined with the
sorted, normalized exceptions, `curation.HypothesisID`), carries
`{"claim", "applies_when": {…}, "support": ["<contract>@<position>",
…], "exceptions": [...], "provenance": ["<path @ commit>", …]}`. A
subject not derived from the claim and exceptions refuses; one claim
with one exception set derives one subject, so a re-proposal that
changes nothing refuses as a duplicate at the boundary rather than
accumulating (the earliest proposal that passed the grant and the
support is the admitted one; a raw-pushed proposal that passed no
boundary reserves nothing, `curation.AdmittedProposalBefore`), and one
that adds an exception (the road out of a contest, below) is a new
subject.

**The predicate.** `applies_when` is an object over record-derivable
fields, at least one of `routing` (equal to the subject's intent
routing, which the fold now reads), `tier` (equal to the subject's
tier) and `paths` (a non-empty list; one entry must prefix the
subject's acceptance ref path); present fields conjoin; an empty
object, an unknown field, an empty `paths` or a non-string value
refuses naming the part. One implementation (`curation.AppliesWhen`,
`Selects`) serves the boundary, the lint and the delivery, so a
claim-time match is evaluated from the fold alone.

**The support floor**: at least two citations, each an admitted
observation (a deliberate exit whose packet carries at least one
finding, or a dead end, signed by the window's holder and citing the
fence active at its own prefix), on at least two distinct contracts,
none of which stands failed: its latest AUTHENTICATED verdict a fail
(`curation.FailedAt`: a verdict counts when its signer held `verdict` at
the verdict's own position and was neither the bound submission's
signer nor a claimant, past or present, so a raw-pushed pass by an
implementing or grantless key clears nothing). That is the
structural minimum the charter makes non-promotable to skip: a worker
promoting its own single run is refused by construction.

**The actor arm.** The holders of the cited observations must be two
or more distinct keys **where the family allows it**: the family is
the set of contracts the applies-when selects in the record, computed
from the selection and never from the support set, and it allows
when those contracts have two or more distinct holders over the chain
(the current holder and every prior claimant, both record facts). A
family with one holder admits support from that one holder, and the
fold records `single_actor_family` on the hypothesis so the projection
says why the arm was not applied; a family with two holders refuses a
two-contract support from one of them naming the arm.

**`curation.hypothesis.contested`**, on the hypothesis subject,
carries `{"hypothesis": "<h-id>@<position>", "evidence":
["<contract>@<position>", …], "reason"}`: the curator's attributable
judgment that held-out evidence contradicts the claim, never an
average. The boundary requires the cited hypothesis to be an admitted
proposal on this subject, and each evidence to be an admitted
observation on a contract the applies-when selects that is NOT in the
support set: held-out evidence, by construction. The fold moves the
hypothesis to `contested` from `proposed` or `promoted` only for a
contest that passed that same boundary at its own position
(`curation.ContestValid`: the signer held `curate` there and the
citations pass `CheckContest` there), so a contest raw-pushed by a key
holding no `curate`, or citing fabricated or support-set evidence, is
an anomaly that moves nothing and disables no lesson; a contested
hypothesis refuses `lesson.promoted`, and a promoted lesson whose
hypothesis is contested stops surfacing while its file and every fact
remain, which is "evidence kept". A contested hypothesis is never
edited or averaged back: the road out is a new proposal whose claim or
exceptions differ, citing the counter-evidence as an exception and
judged on its own support. `seed knowledge validate --hypothesis
<h@position>` is the held-out listing: the observations on selected
contracts outside the support set, for the curator to judge, then
contest or promote. **The residual, stated:** no machine decides that
an outcome contradicts a claim; a contest is a curator's attributable
judgment over evidence the record already holds.

**`curation.lesson.promoted`**, on the hypothesis subject, carries
`{"lesson": "knowledge/lessons/<id>.md @ <commit>", "hypothesis":
"<h-id>@<position>", "pr": "<pr> @ <merged-commit>", "carrier",
"adversarial": {"eval": "<name>", "verdict": "<position>"},
"last_validated", "expires", "digest"}`: anchored paths, the lesson
under the one store (a clean relative path prefixed by
`knowledge/lessons/`, `curation.UnderLessonsDir`: a path that
climbs out through `..` matches the anchor grammar and refuses at
`promotion.shape`), the cited hypothesis this subject, `carrier` one
of `knowledge`, `role`, `skill`, `workflow`, `harness` (where the
lesson lands), the two stamps RFC3339 with `expires` after
`last_validated` (so expiry is derivable from the fold without the
file), and `digest` the sha256 of the lesson file's bytes at the
anchor, which every reader verifies before surfacing.

**The promotion gate has a ledger half and a file half.** The ledger
half, at admission: the cited hypothesis is an admitted proposal on
this subject (re-judged from the record: the signer held `curate`
there, the support passed there, the claim was not already proposed)
and not contested at the tip; its support still satisfies the arms;
`carrier` is a member; the stamps are present and ordered; and
`adversarial` is REQUIRED for every carrier: its verdict is an
authenticated pass (the signer held `verdict` at the verdict's own
position, was no implementer of the eval, and, from `seed/4`, recorded
the independence level the record supports at or above the tier's:
the verdict rule's own level check, installed into the curation
package by `admit` at init so the fold's replay applies it too),
replayed at its own position, on a subject whose
`eval` marker names that eval AND binds it to this hypothesis and to
this lesson anchor ([`evals.md`](evals.md)), filed after the
hypothesis's position. One implementation, `curation.CheckPromotion`,
judges the ledger half at the boundary and in the fold: a promotion
binds to its hypothesis only when it passed that check at its own
position with a signer holding a capability the promotion accepts
(`curation.PromotionValid`), so a promotion raw-pushed past the
boundary folds `unbound` and surfaces nowhere. Every promoted lesson
is behavior-changing, because promotion is what puts it in front of a
worker at claim time;
a `knowledge` carrier says where the lesson lands, never that it is
harmless, so no carrier is exempt. The file half, `seed knowledge
lint <file> --ledger <dir> --repo <dir> [--now <RFC3339>]`: the
frontmatter names its hypothesis, applies-when, support, provenance,
`last-validated`, `expires` and `carrier`
(`knowledge/lessons/README.md`) and agrees with the fact and the
hypothesis; the file's bytes at the anchor hash to the fact's
`digest`, the working file IS those bytes (the lint reads the anchored
revision first and judges it alone, so a valid frontmatter in a later
or uncommitted edit cannot stand in for the promoted one: a later edit
refuses at `lint.digest` until it is promoted in its turn), and the
anchor commit is an ancestor of the repository's head (the promotion
PR merged); every provenance anchor resolves in
the repository at its commit; `last-validated` is not after the
declared instant and `expires` is after it. A drill applies the
presence lint to every file in the store, so `make check` refuses a
shipped lesson that fails it: the PR gate the charter routes promotion
through, on the surface the tree already gates that way.

**The gate registry.** Every refusal of the dead end's shape and
holder checks, of the proposal, contest and promotion rules and of the
lint's file half is a `curation.GateError` naming a gate registered in
`curation.Gates()` at init (`<rule>.<part>`), and the constructor
refuses an unregistered name. The registry is the one authority the
poisoning drill (item 3) enumerates to derive its coverage, pinned to
this table in both directions, so a gate added here without a poison
there is a red test rather than a silent gap.

| gate | what it checks |
|---|---|
| `applies_when.shape` | the predicate names at least one record-derivable field, each well-formed |
| `contest.evidence` | every evidence citation is an admitted observation |
| `contest.held_out` | evidence lies outside the support set |
| `contest.hypothesis` | the contest cites an admitted hypothesis on its own subject |
| `contest.selected` | evidence lies on a contract the applies-when selects |
| `contest.shape` | the contest's payload shape and fields |
| `deadend.holder` | the dead end is the window holder's own |
| `deadend.shape` | the dead end's payload shape and fields |
| `deadend_retirement.deadend` | the citation is an admitted dead end on this subject |
| `deadend_retirement.environment` | the environment differs from the one the previous act named |
| `deadend_retirement.shape` | the dead-end retirement's payload shape and fields |
| `deadend_retirement.standing` | a retirement needs no standing retirement, an un-retirement needs one |
| `lint.ancestry` | the anchor commit is an ancestor of the repository's head |
| `lint.applies_when` | the frontmatter's applies-when parses and equals the hypothesis's |
| `lint.carrier` | the frontmatter's carrier equals the fact's |
| `lint.digest` | the file's bytes at the anchor hash to the fact's digest |
| `lint.duplicate` | one lesson file per hypothesis in the store |
| `lint.frontmatter` | the lesson file opens with the frontmatter block and its keys |
| `lint.hypothesis` | the frontmatter cites the fact's hypothesis |
| `lint.provenance` | every provenance anchor resolves in the repository at its commit |
| `lint.stamps` | last-validated is not after the declared instant, expires is after it, and both equal the fact's |
| `lint.structure` | the frontmatter carries exactly the known keys and the body the README's sections, in order |
| `lint.support` | the frontmatter's support equals the hypothesis's |
| `promotion.adversarial` | the adversarial evaluation is an authenticated pass bound to this hypothesis and this lesson anchor, filed after the hypothesis |
| `promotion.carrier` | the carrier is a member |
| `promotion.contested` | a contested hypothesis is not promotable |
| `promotion.digest` | the digest is the lesson file's sha256 |
| `promotion.hypothesis` | the promotion cites an admitted hypothesis on its own subject |
| `promotion.revalidation` | a re-promotion of a path carries a last_validated after the previous admitted promotion's |
| `promotion.shape` | the promotion's payload shape and fields |
| `promotion.stamps` | last_validated and expires are RFC3339 and ordered |
| `promotion.support` | the hypothesis's support still satisfies the arms at promotion |
| `proposal.shape` | the proposal's payload shape and fields |
| `proposal.subject` | the subject is derived from the claim and its exceptions |
| `retirement.promotion` | the retirement cites the latest admitted promotion of its path |
| `retirement.reason` | pr rides regression alone, superseded_by rides superseded alone, expired carries neither |
| `retirement.shape` | the retirement's payload shape and fields |
| `retirement.superseded_by` | superseded_by names a later admitted promotion, never the retired one |
| `support.actors` | two distinct holders where the family allows it |
| `support.duplicate` | one claim is proposed once |
| `support.failed` | no cited contract stands failed |
| `support.floor` | at least two observations on two distinct contracts |
| `support.observation` | every citation is an admitted observation |


## Expiry, retirement and applicability

**Expiry is derived, never a fact** (plans/os-0d537fbd.md D1). At a
declared instant a promoted lesson is `expired` when the instant is at
or past its `expires` stamp (`curation.Expired`, at-or-past, so a lesson
is expired at the second its stamp names); an expired lesson leaves
the surfacing set and is flagged `stale` wherever the store is shown at
an instant. **Revalidation is a re-promotion.** The curator runs the
hypothesis against the held-out evidence again (`seed knowledge
validate`), the file's `last-validated` and `expires` move forward in
a PR, and the observer records a new `lesson.promoted` for the same
path at the new anchor, through the whole gate again (a fresh survival
bound to the new anchor included). The fold keeps the **latest admitted
promotion per lesson path** (`State.Lessons`, keyed by path), and a
re-promotion whose `last_validated` is not after the previous admitted
promotion's refuses at `promotion.revalidation` naming both, so the
latest promotion per path is always the most recently validated
(`curation.LatestPromotionBefore`: one forward pass, every earlier
promotion judged through the arms and the order against the latest it
admitted so far, never a refold per promotion). No
`lesson.revalidated` verb: a stamp that moved is a file that changed,
and a file that changed is a PR.

**Retirement is an observation, and the evidence stays** (D2).
**`curation.lesson.retired`**, on the hypothesis subject, accepted by
`observer` and `operator` (the promotion's own row), carries
`{"lesson": "<path @ promoted-commit>", "hypothesis": "<h@position>",
"reason": "regression" | "superseded" | "expired", "pr"?, "superseded_by"?}`,
strictly decoded (an unknown key refuses at `retirement.shape`). The two
optional fields are each REQUIRED by exactly one reason and FORBIDDEN by
the others (`retirement.reason`): `regression` requires `pr`, the
revert's merge, which is the charter's one command observed (a
promotion was a PR, so its rollback is `git revert` of the merge, and
the ledger carries the observation that it happened); `superseded`
requires `superseded_by`, the position of an admitted `lesson.promoted`
later than the retired promotion and not the retired promotion itself
(`retirement.superseded_by`: the reviewer of that promotion judged the
supersession, the record checks the citation is a real, later
promotion, necessarily of another path, since a later promotion of the
same path already superseded the old one in the fold); `expired`
requires neither, the stamp the fold already holds being the evidence
(and admission reads no clock, so the boundary does not judge that the
stamp has passed: the observer's act is the observation). The boundary
requires the cited promotion to be the latest admitted promotion of its
path and unretired (`retirement.promotion`: a superseded one is already
gone, and a second retirement over a standing one names it). The fold
moves the path to `Retired`; a retired lesson never surfaces; its file
at the promoted anchor, its hypothesis, its support and every
observation remain, which is "revokes conclusions and keeps evidence".
A retired lesson comes back only by a new promotion of the path through
the gate, which clears the retirement.

**Dead ends retire and un-retire on the environment, by a curator's
attributable act** (D3). **`curation.deadend.retired`** and
**`curation.deadend.unretired`**, on the contract subject, `curate`
alone, carry `{"deadend": "<contract>@<position>", "environment":
"<the environment now>", "reason"}`. The boundary requires the citation
to be an admitted dead end on that subject
(`deadend_retirement.deadend`; another contract's dead end refuses at
the shape gate, since the act is a fact on the contract the dead end
was recorded on), and an environment that CHANGED
(`deadend_retirement.environment`): a retirement's `environment` must
differ from the dead end's recorded one (it no longer applies because
the environment moved), and an un-retirement's from the standing
retirement's declared one (the environment moved again), so neither
act admits in the environment the previous act named; a retirement
needs no standing retirement and an un-retirement needs one
(`deadend_retirement.standing`). Both comparisons are exact string
equality, the one comparison applicability uses: **a dead end applies
to a run whose declared tuple environment equals the dead end's
`environment`** and it is not retired (`DeadEndFact.Applies`). The
fold flags (`retired`, `retired_environment`, `retired_at`) and never
deletes; the held-out listing (`seed knowledge validate`) excludes
retired dead ends and says so, and with `--environment <e>` reports
each selected contract's dead ends with the environment, the retired
flag and whether it applies to `e`. No automatic retirement or
un-retirement, and no environment predicate beyond equality with the
run's declared tuple.

## Bloat: staleness flags, dedup and structure

The `knowledge` projection takes a declared instant (`Inputs.AsOf`,
the observation section's posture; it declares input consumption since
version "3", an instant being an input) and flags `stale` on every
expired lesson, counting them in `stages.stale` beside
`stages.retired`; with no instant declared it flags nothing and says so
(`staleness: "undeclared: …"`, once the chain holds a lesson), so a
build without an instant never reads as "nothing is stale". `seed
knowledge show [--now <RFC3339>]` renders the same view: the stage,
the flags and the reason per lesson (retired with its reason and the
evidence its reason carries; contested; expired at the instant). `seed
knowledge lint` gains the bloat half before the gate's file half:
**structure** (`lint.structure`: the frontmatter carries exactly the
known keys, and the body carries the README's sections `## Claim`,
`## Evidence`, `## Applies when` in that order) and **dedup**
(`lint.duplicate`: the store the file sits in holds one file per
hypothesis; two files citing one hypothesis refuse naming the
duplicate, the file the admitted promotion does not cite; a
revalidation keeps its path, so it is never a duplicate; two
hypotheses whose claim and exceptions canonicalize equal are one id
already). A drill applies both to the shipped store under `make
check`. **The maintenance loop notices what nobody revalidated**
(D5): the lint `lesson_stale` ([`maintenance.md`](maintenance.md),
[`reconciliation.md`](reconciliation.md)) files a defect contract for
a lesson expired at the loop's declared instant for at least
`--stale-after` with no later promotion or retirement, idempotently
through the ledger; the finding's subject is `<lesson path>@<promotion
position>`, so one stale cycle files once and a re-promoted lesson
whose new promotion expires in its turn files new work. The loop never
retires: it asks.

## Delivery

The surfacing set (`curation.Surfacing`) is the latest admitted
promotion per lesson path (the fold binds only what passed the
promotion boundary at its own position) whose hypothesis is not
contested by an admitted contest, which is not retired, which is not
expired at the read's instant (`claim take --now`, `seed situation
--now`; the wall clock otherwise; admission reads no clock, the
offer-liveness posture), whose applies-when selects the subject, AND
whose fact resolves in the repository the reader holds:
the anchor commit is an ancestor of the repository's head (the
promotion PR merged) and the file's bytes at the anchor hash to the
fact's `digest`. A fact that does not resolve is reported as
`lessons_unresolved` (anchor and reason) and never surfaces, so an
attested promotion naming a missing, unmerged or altered file reaches
no worker; `seed reconcile` classifies the same facts
`lesson_unverified` at evidence grade
([`reconciliation.md`](reconciliation.md)). Three surfaces carry the
set from that one derivation: the envelope of `claim take --repo
<dir>` gains `lessons` (present on every claim, empty when nothing
matches; without `--repo` nothing surfaces and `lessons_unverified`
counts what a repository would have verified,
[`loop-verbs.md`](loop-verbs.md)), derived from the view the claim is
judged against and re-derived against every refreshed view the
optimistic loop retries at, so the set reported is the one at the tip
the claim landed on, never the one the session opened at (a promotion
or a contest landing mid-flight changes what the claim receives); the provisioned handoff, where
`Provision` writes the set to `.seed-run/lessons.json` beside
`packet.json` ([`executors.md`](executors.md)); and the orienting
read, `seed situation --repo <dir>`, which lists the same rows per
held subject and without `--repo` reports only the count as
`lessons_unverified`, naming the flag. Contested lessons appear on
none of them.

## Search: an advisory lexical read

Delivery answers one question, exactly and auditably: which promoted
lessons select THIS subject. A worker with a question mid-task (has
anyone hit this before? what did the decision log say about
lockfiles?) had no surface for it short of reading the store, the
dead ends by contract and the decision log top to bottom, which at
this repository's size is several thousand lines the packet never
carried. `seed knowledge search … <query>...` is that surface: BM25
(`curation.Index`, k1 1.2, b 0.75, the Lucene idf, tokens the
lowercase runs of letters and digits of two or more runes, no
stemming and no stop list) over three kinds of document, ranked by
score with ties broken on kind then id, so the ranking is a function
of the corpus and the query alone.

The corpus is what the pipeline already holds and nothing else.
**Lessons**: the surfacing set at the read's instant, subject-less
(promoted, uncontested, unretired, unexpired: `CandidatesAt` with no
subject), each verified against `--repo` exactly as delivery verifies
it and read at its anchor, so the indexed text is the reviewed bytes
and never the working tree's; a candidate that does not resolve is
reported as `lessons_unresolved` with the reason and never indexed,
and without `--repo` no lesson is indexed and every candidate is
unresolved, the delivery posture (`LessonDocuments`). **Dead ends**:
every standing one in the fold, a retired one staying out as the
held-out listing keeps it out (`DeadEndDocuments`). **Docs**: the
sections, by level-one or level-two heading, of every markdown file
`--doc` names under `--repo` (`SectionDocuments`; a path that is not
clean and relative refuses at usage, the store's own rule). A hit
carries its rank, score, kind, id (the lesson anchor, the
`<contract>@<position>`, or `<path>#<heading slug>`), title, the
first body line a query term matched, and the terms that matched; the
envelope counts the corpus by kind, echoes the query, its distinct
terms and the instant (`--now`, else the wall clock), and says it is
advisory.

**Never a delivery path.** The search changes nothing a claim
receives: the applies-when predicate remains the only claim-time
surface, contested, retired and expired lessons are as absent here as
they are there, and a hit is a pointer the reader follows, not a
lesson the reader was shown. Text from either side is data: a query
is tokens, a document is bytes at an anchor, and neither is executed
or obeyed.

**Source, adopted and rejected.** The retrieval idea is
okf-agent-memory's (github.com/okf-memory/okf-agent-memory: local
lexical BM25 over a git-native markdown store, no embeddings, no
vendor, progressive disclosure against context bloat), recorded here
per III.Q row 4. Its Open Knowledge Format and its convention were
not adopted, and the rejection is a standing one: OKF's `generated`
versus `verified` is a frontmatter label an agent sets on itself, its
search-before-write is a convention, and its `stale_after` is a field
nobody enforces; the pipeline above has four stages with distinct
storage and a gate between each, enforced at the boundary and drilled
against poisoning, and a lesson reaches the store through a PR, never
a label. Reimplemented rather than imported: the corpus is derived
from the fold and verified against the repository, which no external
index knows to do, and the scorer is two hundred lines against a
dependency on the trust surface.

## The adversarial evaluation

A counter-trajectory is an eval definition under `evals/<name>/`
whose fixture is the situation the lesson claims to improve,
constructed so that the known verdict fails if the lesson is wrong.
It is filed with `seed eval file --for-lesson <h@position> --carrier
<path @ commit>`, and the filing's marker grows to `{"name",
"tuple"?, "lesson", "carrier"}` ([`evals.md`](evals.md)): the
hypothesis and the exact candidate revision the eval is for, on the
record at filing. It is worked in a workspace that includes the
carrier revision and judged by the verifier, and `seed verdict render`
on a subject whose marker names a carrier refuses `carrier_absent`
(under `checks_red`) unless the carrier commit is an ancestor of the
submission head: the lesson was actually applied. The promotion
boundary then requires the cited verdict's subject to carry a marker
whose `lesson` equals the promoted hypothesis and whose `carrier`
equals the promoted `lesson` anchor, so a pass on an eval filed for
another hypothesis, another revision, or nothing in particular is not
survival. Nothing else runs: the machinery is Phase 10 item 2's, and
no definition is shipped for this; the drills build theirs.

## The grant

`curate` accepts `curation.hypothesis.proposed` and nothing else does:
the fifth no-fallback row, beside `verdict.rendered`, `check.sealed`,
`decision.recorded` and `merge.overridden`. Governance roots hold
`operator` implicitly, and `operator` already reaches `claim.taken`,
the deliberate exits and `curation.deadend.recorded`, so an operator
fallback on the proposal would let one key write a trajectory's
observations and then conclude from them without ever holding two
conflicting grants: the poisoning boundary is real only if the
proposal is reachable through `curate` alone.

`curate` cannot be granted to a key holding `claim` or `operator` (a
root's implicit operator standing included), nor `claim` or `operator`
to a key holding `curate`: the sealer rule one capability over, against
both lanes it names, chain validity at the position. A worker promoting
its own runs, and a root concluding from its own, are refused at the
grant, not at the proposal. The curator manifest holds `curate` and
nothing else; `escalation.raised` accepts it too, the charter's "any
lane can raise" reaching the one lane it did not
([`escalation.md`](escalation.md)).

The curator's reachable set at the boundary is the proposal, the
contest, the two dead-end environment acts, the raise, the workflow
proposal over a recurring shape ([`flywheel.md`](flywheel.md)), and
`message.sent`, which any enrolled active key appends (the relay the
injection suite names for the dispatcher); the injection suite derives
the set from `admit.Affordances` and pins it. The retirement is the
observer's and the operator's, never the curator's: the promotion's
own row, since the retirement is the observation that a promotion is
revoked.

## Versioning

The seven verbs are catalog growth under an existing namespace, the
`offer.published` precedent: an older validator refuses the unknown
verb safely, admission policy shapes them, the fold counts a malformed
raw fact as an anomaly, and every existing chain verifies byte for
byte. No protocol bump: for item 4's three (plans/os-0d537fbd.md D7),
verification takes a verb it has no rule for on active standing (the
standing-only class `message.sent` belongs to; `lesson.retired`'s name
in the catalog gave it no rule at verification), so a chain carrying
them raw-pushed verifies identically under the build before the card
and under it, and only admission's answer changed; a drill raw-pushes
the three and asserts the chain verifies and the fold counts three
anomalies.

## Surfaces

- `seed knowledge deadend (--ledger <dir> | --remote <repo> [--ref
  <ref>] [--state <dir>]) --key <path> --subject <id> --tried <text>
  --outcome <text> --condition <text> --environment <text> [--pointer
  <anchor>]` — the holder's dead end, the fence derived from the active
  window like the loop verbs; no window refuses `invalid_transition`.
- `seed knowledge propose … --key <path> --claim <text> --applies-when
  <json> --support <contract>@<position>… [--provenance <anchor>]…
  [--exception <text>]…` — the curator's proposal, the subject derived
  from the claim and the exceptions (never given); fewer than two
  citations or a predicate that is no object refuse at usage naming
  the part.
- `seed knowledge validate (--ledger <dir> | --remote <repo>)
  --hypothesis <h-id>@<position> [--environment <e>]` — the held-out
  listing (retired dead ends excluded, and the envelope says so) and
  the selected contracts' dead ends with the environment, the retired
  flag and, with `--environment`, whether each applies to `e`.
- `seed knowledge contest … --key <path> --hypothesis
  <h-id>@<position> --evidence <contract>@<position>… --reason <text>`
  — the curator's contest.
- `seed knowledge promote … --key <path> --lesson <path @ commit>
  --hypothesis <h-id>@<position> --pr <pr @ commit> --repo <dir>
  --carrier <c> --adversarial <eval>@<verdict position>
  --last-validated <RFC3339> --expires <RFC3339>` — the observer's
  promotion, the subject the cited hypothesis's, the digest read from
  the file at its anchor in the repository (never typed).
- `seed knowledge retire … --key <path> --lesson <path @ commit>
  --hypothesis <h-id>@<position> --reason <regression|superseded|expired>
  [--pr <pr @ commit>] [--superseded-by <position>]` — the observer's
  retirement, the subject the cited hypothesis's; the reason's field
  pairing is judged at usage before the boundary judges the citation.
- `seed knowledge deadend retire … --key <path> --deadend
  <contract>@<position> --environment <e> --reason <text>` and `seed
  knowledge deadend unretire …` — the curator's environment acts, the
  subject the cited contract.
- `seed knowledge lint <file> (--ledger <dir> | --remote <repo>) --repo
  <dir> [--now <RFC3339>]` — the bloat half (structure, then the
  store's dedup) and the gate's file half; a refusal is `lint_refused`
  naming the gate.
- `seed knowledge show (--ledger <dir> | --remote <repo>) [--now
  <RFC3339>]` — the fold: the stage counts, dead ends by contract with
  their flags, hypotheses with their stage (`proposed`, `promoted`,
  `contested`), lessons with `surfaces`, `stale`, `retired` and the
  reason, the standing retirements, the unbound promotions; `as_of`
  with `--now`, else `staleness` saying the instant is undeclared.
- `seed knowledge search (--ledger <dir> | --remote <repo>) [--repo <dir>]
  [--now <RFC3339>] [--limit <n>] [--doc <path>]... <query>...` —
  the advisory lexical read ("Search" above): BM25 over the verified
  surfacing set at the instant, the standing dead ends and the named
  docs' sections; `hits` ranked, `documents` counted by kind,
  `lessons_unresolved` as delivery reports them; never a delivery path.
- `seed maintain run … [--stale-after <duration>]` — the `lesson_stale`
  lint at the pass's declared instant ([`maintenance.md`](maintenance.md)).
- The `knowledge` projection (`knowledge.json`, version 3) publishes the
  same view at the declared inputs' instant
  ([`projections.md`](projections.md)); the report (version 12)
  carries a `knowledge` section counting the stages, the retired and
  the stale, when the chain holds any curation fact.

## The poisoning drill

III.K row 4 asks that "trajectories are treated as untrusted inputs;
the poisoning drill fails to achieve promotion in CI". Phase 11 item 3
(`plans/os-e2f1ad23.md`) answers it in the injection suite's shape
([`lanes.md`](lanes.md)), and the first thing to say is what it does
**not** do.

**It does not test that a curator disbelieves hostile text.** There is
no model under `next/`, and a `curate` key persuaded by what it reads
proposes a false claim over genuine support that every gate admits: the
boundary judges the support, never the claim's truth. That is a named
residual below, not a poison the boundary can refuse. What the drill
tests is that CONSTRUCTING the support, the contest, the promotion or
the file cannot get a false lesson in front of a worker.

### Every poison fails at both ends

`internal/admit/testdata/poisoning/corpus.json` declares each poison:
`{"name", "gate", "attack", "expect": {"verb", "gate" | "reason"}}`, and
`internal/admit/poisoning_test.go` carries one script per name, a chain
built the fixture's way (workers' dead ends and packet findings, a
grantless stranger's raw pushes, curators' proposals and contests,
observers' promotions, verifiers' eval passes) that ends in an attempt
to promote. For every poison the drill asserts three things: the named
verb refuses at the named gate (or names the reason, for the refusals
the boundary raises outside the registry: out of grant, the grant's
disjointness, the acceptance's gate); no `lesson.promoted` for the
poisoned hypothesis stands admitted by the chain's end; and a claim on
a contract the poison's applies-when selects carries no lesson for it
(`Candidates` and `Surfacing` both empty for the subject). The last two
are the charter's sentence made a test: a gate rewritten so that the
refusal moves keeps the drill red until the lesson is still kept from
the worker. The lint poisons (`lint.*`) bend the file half in one place
each and assert the refusal at its gate; their other end is the store's
lint under `make check`, which is what gates the lesson PR.

### Coverage is derived, never authored

Every refusal of the proposal, contest and promotion rules and of the
lint's file half is a `curation.GateError` naming a gate registered in
`curation.Gates()` at init, and the constructor refuses an unregistered
name, so a refusal without a gate does not exist. The drill enumerates
that registry, pins it to the gate table above in both directions, and
fails on a registered gate no poison names, on a poison naming no
registered gate, on a declared poison with no script, on a script with
no declaration, on an empty corpus and on an empty residual table. A
gate added to the rules without a corpus entry is therefore a red test,
which is what makes "full coverage" a claim the drill derives rather
than one it is told. At landing the corpus holds forty-seven poisons over
the thirty-two gates: among them `single-success`, `self-replay`,
`forged-support`, `grantless-window`, `failed-support`,
`worker-proposes`, `worker-granted-curate`, `root-proposes`,
`predicate-everything`, `predicate-unknown-field`,
`unchanged-reproposal`, `held-out-forgery`, `contested-promotion`,
`promotion-traversal`, `promotion-of-raw-proposal`,
`smuggled-role-lesson`, `raw-pass`, `borrowed-pass`, `stale-pass`,
`pass-at-another-position`, `fail-as-survival`, `support-failed-later`,
`contested-surfacing`, `raw-pushed-promotion`, `raw-pushed-contest`,
`ungated-eval`, `fabricated-provenance`, `frontmatter-drift`,
`unmerged-anchor`, `stamps-unreviewed`. The two raw-pushed poisons push the refused fact
past the boundary anyway and assert the fold re-judged it: the
promotion binds nothing and the contest moves nothing, because the
fold runs `PromotionValid` and `ContestValid` at each fact's own
position.

### The residuals, named

`residuals.json` in the same directory names each poison the boundary
ADMITS, with why, what an attacker can inflict and what stands in the
way, each pinned by a characterization drill asserting the poison is
admitted, so closing one fails the suite and forces the table to say
what replaced it.

| residual | what stands in the way |
| --- | --- |
| `single-holder-family` | a family the applies-when selects with one holder cannot supply two, so support from that holder admits (the charter's "where the family allows") and the fold records `single_actor_family`; the adversarial evaluation for every carrier and the lesson PR's reviewer stand behind it |
| `colluding-keys` | disjointness is per key: two workers and a curator on distinct keys satisfy every arm whoever operates them; enrollment is the operator's act and `kind` an assertion the operator makes |
| `persuaded-curator` | a `curate` key proposing a false claim over genuine observations satisfies every gate; the contest from another `curate` key, the adversarial evaluation and the reviewer stand behind it, and there is no model to test the persuasion itself |
| `reviewed-vacuous-eval` | `seed eval check` refuses a vacuous definition, but a reviewer can merge one, and the anchor rule then binds a pass on it to the gated revision; the review gates the definition |
| `cosmetic-reproposal` | a reworded claim is a new subject judged on its own support; the contest follows the observations, which are the same ones |

### The CLI arm

`worker-proposes` and `smuggled-role-lesson` also run through `seed
knowledge propose` and `seed knowledge promote` in the small-team
fixture (`cmd/seed/modes_e2e_test.go`), proving the refusal reaches an
operator's terminal with the code the boundary gave (`out_of_grant`;
the gate's name in the refusal), and that the next claim on a matching
contract carries no lesson. The lint poisons promote each bent file as
its own anchor, since the file half judges the promoted bytes.

### A poison that gets through is a defect in the gate

If constructing a poison finds a gate that admits it, the fix lands in
the gate's own package with the poison as its regression drill, and
`docs/decisions.md` records the row it changed; the corpus never
accommodates the gate. Item 1's review found four such gaps before
this drill landed (a raw window, a raw pass, a raw proposal reserving
its subject, a traversal path), and item 2's review found two more
while this drill was being written (a raw-pushed promotion bound in
the fold and surfaced; a raw-pushed contest disabled a lesson); each
is a poison here, and the second pair began as a residual of this
drill before the fold learned to re-judge both facts.

## The flywheel

The curator's second output beside lessons is the workflow proposal
([`flywheel.md`](flywheel.md)): a shape of done work that recurs
(routing, gated acceptance path, tier, verb sequence, seen `2` or more
times) is drafted into a v1 workflow from its gated acceptance
commands, validated through the v1 engine's mock run, and proposed as
a PR the curator cannot merge, on the same propose-everything,
approve-nothing posture the promotion path holds. The proposal
(`workflow.proposed`) and the merge observation (`workflow.merged`)
are curation-adjacent facts on a derived subject, the shape id, and
the same fold-time re-judgment that keeps a raw-pushed promotion out
of the knowledge section keeps a raw-pushed proposal out of the
flywheel's.

## Conformance mapping

- III.K row 1 (online lanes append evidence only; conclusion-writing is
  grant-gated to the curator's proposal path; workers append candidate
  observations, never promoted lessons): the dead end's window rule and
  the proposal's `curate`-only row with its grant-level disjointness,
  drilled per refusal at the boundary and through `seed knowledge`.
- III.K row 2 (the pipeline is staged with distinct storage and gates;
  no stage skips): the four stages above, the support floor, the
  promotion's re-judged citation and the `unbound` fold, drilled at the
  boundary and in the projection.
- III.K row 3 (promotion requires applies-when conditions; support
  from more than one non-failed trajectory and more than one actor
  where the family allows; provenance links; a last-validated stamp;
  and, for behavior-changing lessons, survival of an adversarial
  evaluation against constructed counter-trajectories): the predicate,
  the actor arm, the promotion gate's two halves and the bound eval,
  drilled per refusal at the boundary and through `seed knowledge`.
- III.K row 4 (trajectories are treated as untrusted inputs; the
  poisoning drill fails to achieve promotion in CI): the poisoning
  drill above, its corpus derived from the gate registry, every poison
  asserted to fail at both ends, the residuals named and pinned, and
  the CLI arm in the modes fixture; `make check` runs it.
- III.K row 5 (conflicting evidence is a first-class contested state,
  never silently averaged; contested lessons do not surface): the
  contest, the fold's stage and the surfacing set's exclusion,
  drilled at the boundary, in the projection and on every delivery
  surface.
- III.K row 6 (every promoted lesson carries a last-validated stamp
  and an expiry-for-revalidation; retirement revokes conclusions and
  keeps evidence; a promoted lesson implicated in a regression rolls
  back by reverting its PR, one command because it was a PR): the
  stamps on the fact, expiry derived at the read's instant,
  revalidation as a re-promotion under `promotion.revalidation`,
  `lesson.retired` with its three reasons and the fold that keeps the
  file, the hypothesis and the observations, the revert observed as
  `regression` with its `pr`; drilled at the boundary, in the
  projection, at the terminal and end to end in the modes fixture
  (the revert observed, the claim after carrying nothing).
- III.K row 7 (dead ends record failure condition and environment, and
  can be un-retired when the environment changes; the curator checks
  dead-end applicability, not just lesson applicability):
  `deadend.retired` and `deadend.unretired` on an environment that
  changed, applicability by string equality with the run's declared
  environment, `seed knowledge validate --environment` reporting it;
  drilled at the boundary and at the terminal.
- III.K row 9 (knowledge bloat is managed: dedup with provenance,
  staleness flags, structure lint): `lint.duplicate` and
  `lint.structure` under `seed knowledge lint` and `make check`, the
  `stale` flags at the projection's declared instant, and the
  `lesson_stale` maintenance finding; routed here by
  `docs/build-plan.md` Phase 11 item 4.
- III.I row 5 (knowledge shown at the right moment): the surfacing set
  on `claim take`, in the provisioned handoff and in the orienting
  read, verified against the repository before anything surfaces, at
  the instant the read declares.
