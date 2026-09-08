# lanes.md — role definitions whose claims are decidable

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §11 "Lanes" and Part III.J; [`docs/build-plan.md`](../docs/build-plan.md)
> Phase 9 item 1; plan `plans/os-cf1c9688.md`. Implemented by
> `internal/lane`, `internal/loopverb`, `lanes/**`, and the
> `seed lane` read surface.

## What a lane is

Six lanes, each a **role** — grants plus conventions — and never a
binary: any qualified tuple can staff any lane it holds grants for.
The charter's six (§II.11) are `dispatcher`, `planner`, `implementer`,
`verifier`, `curator` and `maintenance`.

A lane is **a manifest plus an ordered list of prose fragments**:

- `lanes/<lane>.json` — the declarations, read by a validator.
- `lanes/fragments/**` — the conventions, read by an agent.

`seed lane show <name>` resolves the fragments by concatenating them
**in the order the manifest declares**, and `seed lane list` names the
six lanes and the two roles beside them.

## Six lanes, and two roles that are not lanes

`lanes/` holds eight manifests, and the number is not a
contradiction of the charter's six. Every manifest declares a **`kind`**:
`lane` for one of §II.11's closed enumeration, `role` for a part the
charter defines **outside** the work loop. The field is required rather
than defaulted, so the six say what they are in their own files and the
enumeration is a property of the manifests rather than of this
sentence.

The two roles are the charter's own. The **supervisor** is §II.9, its
own section: it publishes offers and initiates, settles and preempts
runs, and it never takes work; from `seed/3` it also mints
qualifications from eval passes, disqualifies the configurations eval
fails name, and publishes the offers waiting evals need, all through
`seed eval act` under its own `supervise` grant
([`evals.md`](evals.md)). The **dispatcher** gains the matching act on
the same surface: filing and specifying the spot-check evals a stale
qualification owes, which is queue management like any filing. The **observer** is §8's governed
observer: it records what an external authority did — the merge that
ends a contract's loop, the checks a forge ran — and can do nothing
else, which is what makes its observations trustworthy. Neither holds
`claim`; neither appears in a fleet as a worker.

They exist as manifests because the capability table required them and
nothing supplied them (`plans/os-d6a52784.md`). `offer.published`
accepts `[supervise, operator]`, `merge.observed` accepts `[observer,
operator]`, the three `run.*` verbs accept `[supervise, operator]`, and
`check.sealed` accepts **`[sealer]` alone** — and before this card no
manifest granted `supervise`, `observer` or `sealer`. A deployment
assembled from the shipped set could neither publish the offer its own
workers poll for nor record the merge that ends the loop, and for
sealed checks had no escape hatch at all, `sealer` being disjoint from
`claim` and `operator` at admission. `sealer` now rides the verifier:
the charter's isolation requirement is from *implementation* grants
(§7), and the check bodies are already encrypted to the verifier
keyring, so a separate authoring identity would be one that cannot read
back what it wrote.

Two things enforce the shape. `internal/lane` refuses a manifest of kind
`lane` whose name is not one of the six, citing §II.11, so a directory
anyone can drop a file into does not become the place a normative
enumeration is edited by implication; the shipped-set drill asserts all
six are present. And a drill reads the verb literals out of
`keyring.AcceptedCapabilities`' own source and requires every
capability they accept — **`operator` excluded**, since it satisfies
everything by construction — to be granted by some shipped manifest.
Run against the pre-fix tree it names six verbs across the three
capabilities, three more than the card that filed the gap had found;
that is the argument for deriving the list rather than writing it
down.

## Why the split, and why the order is declared

Composability is the point. The one-inbox doctrine, the orienting read,
the deliberate-exit rule and the liveness rule are written **once** and
composed by every lane that needs them, so a change to a shared
convention cannot drift between six copies. A convention that appears
in six files is a convention with six versions.

The order is **declared, never inferred**. III.J says *ordered*
fragments, and a resolution whose order comes from a directory listing
is a resolution that changes when someone renames a file.

Manifests are data and fragments are prose, kept in separate files
rather than as frontmatter on one: frontmatter would make a fragment
both a document and a record, and the ordered-composition rule would
then have to say whose frontmatter wins. A fragment is only ever prose,
so the merge question never arises.

Manifests are **JSON**, like every other machine-read file under
`next/` (the port schema, the projections, the packets). The plan
proposed YAML for readability; `next/` carries no YAML parser and a
deliberately small dependency set, and the reason the plan gave for
separating declarations from prose is served identically by JSON.

## The four obligations, as fields

`docs/build-plan.md` Phase 9 item 1 binds four obligations onto
every lane's fragment. They are **declared fields**, not paragraphs,
because a validator can find a field and cannot find a paragraph. Prose
may restate them; validation reads the fields.

| field | obligation |
| --- | --- |
| `orients_from` | the single position-stamped read on wake, in the posture the lane works in |
| `acts_through` | the loop acts the lane performs, never the raw append seam |
| `liveness_from` | which of the lane's own work steps emit observations |
| `inbox` | push channels wake, position-stamped reads convince |

## Every check consults an authority elsewhere

This is the whole design. `internal/lane` holds **no policy**: a
hand-written list of capability or verb names inside it would be the
drift it exists to prevent.

| check | authority |
| --- | --- |
| `grants` name real capabilities | `keyring.Capabilities()` |
| the lane's grants **intersect** what each act's verb accepts | `keyring.AcceptedCapabilities` |
| `acts_through` names real loop acts | `internal/loopverb` |
| `orients_from` is the situation read, with flags it takes and exactly one posture arm | `lane.SituationFlags`, pinned to the CLI by drill |
| fragments exist, and none is orphaned | the filesystem, against the manifests |
| resolution is deterministic | resolving twice, byte-compared |

**The relation is intersection, not containment.**
`AcceptedCapabilities` returns an OR-set — the grant rule consumes it
through `HasAnyCapability`, so any one of the returned capabilities
admits the verb, and most worker verbs return `{claim, operator}`.
Requiring a lane to hold *every* accepted capability would hand
`operator` to every lane that claims or spends, dissolving the
capability separation the charter is built on in the name of checking
it.

## Liveness rides the work

`liveness_from` names the lane's own steps whose execution emits
observations, and every entry must appear in `acts_through`: an
observation is only ever emitted **by a step that does work**. A lane
wanting a bare heartbeat must declare a work verb it does not perform,
and the capability check then asks it for the grant.

The obligation is **conditional on running a loop, and not dodgeable by
declining to**. Four of the six lanes perform no loop act — a verifier
acts through `verdict.rendered` (from `seed/4` declaring its own
runtime tuple beside the level it achieved,
[`verdicts.md`](verdicts.md)), a dispatcher through `intent.filed`,
neither of which is a loop act — so requiring loop acts of all six
would force four manifests to claim work they never do. Instead: a lane
that declares acts must declare where its liveness comes from, and a
lane cannot escape by declaring none, because **holding the `claim`
capability means it claims, and claiming is a loop act**. The grant it
already declares decides whether the obligation applies.

**And the declaration is now enforced, not merely compared.** 1a could
only check two labels against each other and said so here: it could not
show the named step actually emits, because nothing executed. Phase 9
item 1c's loop (`internal/loop`, [`loop-verbs.md`](loop-verbs.md))
closes that:

- The loop emits an observation as a **side-effect of a declared
  liveness act that succeeded** — never as a step of its own. The set it
  emits for IS `liveness_from`, read from the manifest at run time
  rather than carried as a second list, so an act named there emits and
  an act not named there does not.
- The stream is keyed to the lane's own actor and to **the fence its
  orienting read reports**, which is exactly how `internal/obs` keys
  what it classifies. A stream written under any other actor or fence
  is invisible to the classifier and therefore useless as liveness, so
  the drills sample that exact key rather than merely counting lines.
- A **refused** act emits nothing. Otherwise a lane wedged at a
  boundary would look busiest exactly when it is most stuck, and the
  classification the maintenance reap depends on would read failure as
  progress.
- The loop reaches no liveness-only surface, asserted as a property of
  what it invoked: every call it makes is either one of the two reads it
  orients from or an act its manifest declares.

Each of those is mutation-checked — emitting per act rather than per
declared act, keying by a constant fence, and emitting on a refused act
each fail their drill.

Fragment prose is additionally swept for an instruction to run a bare
`seed obs emit`, since a fragment could tell an agent to heartbeat
without declaring it. That sweep **is** a spelling rule and is treated
as one: a second line of defence, never the argument.

## One posture, or the read is not the view

`orients_from` names **exactly one** of `--ledger` or `--remote`, and
validation enforces the exclusive-or rather than a single required flag.
The reason is not symmetry:

`claim take` is **remote-only**. `claim.taken` is the one exclusive act,
and only the push round-trip can order two rivals, so the local path
refuses it outright. Until Phase 9 item 1c, `offer list` and `situation`
bound `--ledger` alone — so in the only posture where a lane could
claim, it could neither poll nor orient. A loop there would read a local
copy that nothing refreshes and call its position authoritative, which
is precisely the staleness the exclusive act exists to prevent.

Both surfaces now take the pair, and the six shipped manifests declare
the remote posture: the one a lane that claims can actually orient in.
A read naming neither has no ledger to derive from; one naming both
cannot say which view its position stamps. Both refuse.

The loop could instead have read the remote-materialized store through
internal packages, since it is a library in the same module. It must
not: every manifest declares `seed situation …` as its orienting read,
and a loop that oriented by internal call would make that declaration a
fiction and reopen the drift these manifests exist to close.

## The dispatcher's posture is an allowlist

The dispatcher touches the most untrusted text in the system and runs
with least standing capability. Its manifest declares its permitted
grants and validation checks the set is **exactly** that allowlist —
`dispatch` alone in v0.

A blocklist would be the wrong shape: it must be extended every time a
capability is added, and one nobody thought to exclude is admitted by
default. That is not hypothetical — an earlier draft checked "holds no
authoring, verdict or sealing grant", which admits `operator`, the
strongest capability in the keyring.

That is the **standing** half of III.J's second row. The input-handling
half is below.

## The injection conformance suite

III.J's second row asks that "embedded instructions in intents, mirrors,
and tool output are quoted as data, never obeyed". Phase 9 item 1b
(`plans/os-b779b4c7.md`) answers it, and the first thing to say is what
it does **not** do.

**It does not test that hostile text is disbelieved.** There is no model
under `next/`; "never obeyed" is a claim about an agent's behavior, and
a corpus of `IGNORE PREVIOUS INSTRUCTIONS` strings fed to code that
never had instructions would test the corpus rather than the system. The
charter names the way out in the same paragraph that lists the controls:

> a model can still be persuaded by adversarial text it reads — which is
> why capability bounds, not fencing, carry the invariant.

So the suite asserts that **believing the text changes nothing**, and
names exactly where that is false.

### Reachability is derived from the boundary

The dispatcher's reachable act set comes from `admit.Affordances`, which
drafts one signed probe per catalog verb and runs the same `Check`
pipeline admission enforces. Every member must appear in
`internal/admit/testdata/injection/residuals.json` with why it is
admitted and what a persuaded lane could inflict; an unnamed one fails,
and a named one the walk cannot reach fails as stale.

**Not** from `keyring.AcceptedCapabilities`. That table is a capability
index, not a reachability oracle: its switch falls through to `nil` for
the standing-only class, so a capability filter silently omits
`message.sent` — the one dispatcher-reachable act that relays. The
distinction is the difference between a suite that finds the sharpest
residual and one that cannot see it.

### The residuals, named

| act | what a persuaded dispatcher can inflict |
| --- | --- |
| `intent.filed` | **narrowed by os-be12ac16** ([`tiers.md`](tiers.md)): the filed `tier` and `budget` are now validated against their tables at admission, so a persuaded dispatcher cannot file a value the system does not know, and the three authority sites read the table rather than the constant. What remains is **mis-tiering**: `"trivial"` is a legitimate filing that exempts the plan gate and the sealed-checks lint, and nothing yet attests who may make it. Owner: tier provenance ([`plans.md`](plans.md)'s "until tier provenance lands"), still pinned by a characterization drill. `routing` names a squad the deployment owns and is not validated here |
| `contract.specified` from `ready` | **re-specification** (plans/os-6bd9ffff.md D4; [`lifecycle.md`](lifecycle.md)): from `seed/4` a persuaded dispatcher can rewrite an unclaimed contract's draft acceptance. The reach widens, the bound does not: the acceptance gate binds the rewrite exactly as the first draft (executable content needs gate evidence), a claimed, reviewed, blocked or finished contract is out of reach by the table, `tier`, `budget` and `routing` stay the intent's, and the record carries the revision (the fold's `Specifications`, the report's re-triage rate), so a rewrite is visible as one rather than as the first draft. The reachability drill derives the widening from the boundary rather than being told. Owner: tier provenance, for the fields this cannot revise |
| `claim.reaped` | admitted on a live claim with no liveness evidence consulted at all. Its two preconditions are freshness and attribution, not authorization: the fence citation (readable from any position-stamped read) and a packet. Owner: Phase 9 item 3 |
| `message.sent` | **no capability at all** — standing-only, so any enrolled active actor appends it. This is the one that RELAYS. Bounded by the classification lint at 512 bytes per string, which is a SIZE bound: the sixty-byte instruction that matters sails through. Since os-8451d939 it reaches one more surface, and deliberately only as far as a NOTICE: `seed situation` reports that mail exists (sender, contract, position, size) and carries no payload text, because the orienting read is taken on every wake unbidden. The body is reached by `seed message read --at <position>`, which is the reader choosing to look ([`obligations.md`](obligations.md)) |

Each is pinned by a characterization drill asserting the behavior in the
residual's own words, so closing one fails the suite and forces this
passage to be updated with what replaced it.

### What the suite establishes, and what it does not

**Tool output cannot inject.** This is a structural proof rather than a
drill of behavior: `verdict.Transcript` holds `Cmd`, `Exit`,
`OutputSHA256` and `OutputBytes`, so output bytes are hashed at the
boundary and dropped. No adversarial text can traverse a channel that
does not exist. The command string itself is carried verbatim, and the
drill says so: what is proven is that *output* is not carried, not that
the transcript is text-free.

**Worker-facing reads carry no hostile prose.** `situation` and `offer
list` are swept by marker, in their serialized form so a field added
later is covered. The sweep covers two sources now, not one: an
intent's free text, and a `message.sent` payload addressed to the
reading worker — which is the sharper arm, since an intent needs a
persuaded dispatcher and a message needs only an enrolled actor. Both
addressed and broadcast messages are planted, because they reach the
caller through different paths in the filter.

**The projections carry every payload verbatim, by design** — a
projection that could not show what was appended would not be an audit
view. This is where the mirror arm lands: the projections are what a
dashboard or mirror renders, so whichever card lands `request.*`
inherits an input that already carries hostile text verbatim. Pinned by
its own drill rather than left implicit.

**The mirror arm is met from `seed/7`** ([`requests.md`](requests.md)).
`request.filed` — the protocol's inbound-proposal family for mirror
edits, dashboard actions and cross-repo work — is the one door a
surface's proposal enters by: a fact, standing only, changing no state.
The corpus is fired at it (`TestNoHostileRequestWidensTheDispatcherSet`
in the admit suite, the terminal arm in `cmd/seed`): every corpus
file planted in `summary`, in `reference` (a valid ref form with
hostile text is not a reference, so the shape refuses it), in
`origin` and in the subject; the dispatcher's reachable set with
pending hostile requests is exactly its pinned set; the situation read
carries the marker nowhere (the notice is origin, kind, size,
position and age, never the summary); and `request.answered` over a
hostile request yields only what the dispatcher's allowlist already
permitted. III.J's second row is therefore **met in full**: intents,
tool output and mirrors.

**The vocabulary has an honest edge.** The sweep covers
`admit.affordanceCatalog`, whose completeness against the spec table is
enforced in both directions. `message.acked` and `request.*` are named
in [`protocol.md`](protocol.md) but appear in neither, being
unimplemented, so the suite cannot speak about them. The `curation.*`
edge closed with Phase 11 items 1 and 2: the four implemented verbs
are in the catalog and the table, and the curator's own reachable set
(the proposal, the contest, the raise, the standing-only relay) is
derived from the boundary and pinned against its residual table
exactly as the dispatcher's is ([`curation.md`](curation.md)). The
`workflow.*` edge closed with Phase 11 item 5: both verbs are in the
catalog and the table, the proposal reachable by the curator alone and
the merge by the observer, and the curator's residual table names the
proposal ([`flywheel.md`](flywheel.md)).

## The loop-verb registry

`internal/loopverb` is the one authority for which loop acts exist and
what each appends, with two consumers: the CLI dispatch and this
validator. It existed nowhere before — the acts were `case` arms inside
`cmd/seed`, in package `main`, which nothing can import — so any second
consumer would have had to write the seven names down again.

It carries no policy. Whether an act admits at a position is the
admission boundary's answer; which capabilities it accepts is the
keyring's.

## Surfaces

- `seed lane list [--lanes <dir>]` — the six, with grants and fragment counts.
- `seed lane show <name> [--lanes <dir>]` — the declarations plus the resolved prose.
- `seed lane validate [--lanes <dir>]` — every check; findings name the lane, the field, and what refused.

The reads a lane orients through take the posture pair as well:
`seed situation` and `seed offer list` each accept `--ledger <dir>` xor
`--remote <repo> [--ref <ref>] [--state <dir>]`.

All three are read-only and idempotent: they open no ledger, mutate
nothing, and journal no attempt, because a read is not an
admission-boundary attempt ([`refusals.md`](refusals.md)). They carry
**no position stamp**: a resolved role derives from checked-in files
rather than from the ledger, so there is no position they could
honestly cite, and nothing is ever written back — a resolved role on
disk would be a second copy that can go stale, which is the failure the
ordered fragment list prevents.

### Exit code

| code | name | meaning |
|---|---|---|
| 26 | `lane_invalid` | a checked-in lane manifest makes a claim the tables refuse: a grant outside the vocabulary, an act whose accepted capabilities the lane does not hold, an empty or non-work liveness source, a missing or orphaned fragment, a dispatcher grant outside its allowlist. Distinct from `posture_invalid`, which judges a deployment's posture declaration rather than a role definition |

## Conformance mapping

- III.J "Role definitions exist for all six lanes as grants +
  conventions, composable from ordered fragments, resolved and checked
  by validation" — `lanes/**`, `lane.Resolve`, `lane.Validate`,
  and the drill asserting the shipped set validates clean.
- III.J "The dispatcher runs with least standing capability and passes
  the injection conformance suite" — the allowlist above for the
  standing half, drilled against `operator` explicitly; the suite above
  for the input-handling half, **met in full** from `seed/7`: intents,
  tool output and mirrors, the last through the corpus fired at
  `request.filed` ([`requests.md`](requests.md)). The remaining
  residuals are named and pinned rather than closed.
- III.J row 3 "Dispatcher re-triage rate and planner unedited-approval
  rate are tracked" — met by [`trajectories.md`](trajectories.md)'s
  metrics half: the `ready` origin of `contract.specified` at `seed/4`
  ([`lifecycle.md`](lifecycle.md)), the plan verbs' content digest
  ([`plans.md`](plans.md)) and the report's `lanes` section
  ([`projections.md`](projections.md)); the harness half, III.O row 3's
  recorded decision points replayed against these manifests, is the
  same spec's, with its residual (no decider re-runs at a point) named
  in this suite's words.
- III.K row 4 "Trajectories are treated as untrusted inputs; the
  poisoning drill fails to achieve promotion in CI" — met by the
  curator poisoning drill ([`curation.md`](curation.md), "The
  poisoning drill"): this suite's shape turned on the curation gates,
  a corpus of scripted poisons derived from the gate registry, each
  asserted to fail at both ends, five residuals named and pinned, and a
  CLI arm in the modes fixture; `make check` runs it.
- III.K rows 6, 7 and 9 (the stamps and expiry-for-revalidation,
  retirement keeping evidence, rollback by revert; dead ends
  un-retired on environment change and dead-end applicability;
  knowledge bloat managed) — met by Phase 11 item 4
  ([`curation.md`](curation.md), "Expiry, retirement and
  applicability" and "Bloat"), the mapping there naming each drill.
- III.K row 8 (the flywheel closes through gates: recurring shapes,
  drafted workflows, mock validation, PR; repair roles propose patches
  as PRs; conversion rate tracked) — met by Phase 11 item 5
  ([`flywheel.md`](flywheel.md), "Conformance mapping"): the shape
  from the record, the draft from gated acceptance commands, the v1
  engine's validate and mock run from a staging worktree, the proposal
  on its branch and never main, the repair contract under the
  dispatcher's key with its patch on the same branch, and the report's
  `flywheel` section.

- III.G row 7 and III.O row 2 (rubric verdicts scored item by item
  with cited evidence and explicit uncertainty; low-confidence items
  routed to a human verdict; calibration against a human-scored gold
  set with automatic authority suspension on drift and a defect
  filed) — met by Phase 10 item 4 ([`verdicts.md`](verdicts.md), "The
  rubric and the scorecard"; [`evals.md`](evals.md), "Calibration"):
  the verifier lane's summary and fragment carry the scorecard and
  the deferral.

- III.O row 3 (the compromised-actor drill passes in CI — the
  architecture's own definition of done) — met by Phase 12 item 1
  ([`redteam.md`](redteam.md), "Conformance"): the §I.2 adversary
  played against the enforced reference deployment
  ([`admission.md`](admission.md)), the ceiling asserted clause by
  clause at the push with coverage derived both ways, one derivation
  shared with `admit.Check`, and the residuals named and pinned. The
  gate is `make check`, so CI runs it with the rest of the suite.

## A gap the mode fixtures surfaced

No shipped manifest grants `supervise` or `observer`, while
`offer.published` accepts only `supervise` or `operator` and
`merge.observed` only `observer` or `operator`. A deployment assembled
purely from these six lanes can therefore neither publish the offer its
own workers poll for nor record the merge that ends the loop; only the
maintenance lane reaches either, through `operator`, which is not its
job. Carded as `os-d6a52784`; [`modes.md`](modes.md) records how the
fixtures work around it and why that is a fixture's licence rather than
a posture.

## Routing is declared

`routing` names a squad, and squads are the deployment's — a residual
this document carried since the tier vocabulary landed. The preseed's
`teams` block ([`postures.md`](postures.md)) closes it: `intent.filed`
under a declaration refuses a squad the deployment did not declare
(`routing_unknown`), as admission policy rather than chain validity,
and each declared squad names the manifests it runs, checked against
the shipped set by `seed preseed check`.
