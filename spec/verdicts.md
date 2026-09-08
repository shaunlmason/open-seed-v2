# verdicts.md — the verdict pipeline's first half

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §8 "The verdict pipeline" and §6 (the verifier executes specs
> in a sandbox with declared, minimal capability); conformance III.G
> rows 3–5; [`docs/build-plan.md`](../docs/build-plan.md)
> Phase 6 item 1; plan `plans/os-f6d2c267.md`. Implemented by
> `internal/verdict`, `internal/artifact`, the `verdict` admission
> rule, and `seed verdict receipt|render|check`. The reconciliation
> chain and divergence detection landed with 6.2
> ([`reconciliation.md`](reconciliation.md));
> sealed checks 6.3; red-verdict lockout and the operator override verb
> 6.4; rubrics and L2/L3 are Phase 10/11.

## The event

`verdict.rendered` is a **fact, not a transition**: it admits only on a
subject whose folded state is `review` (elsewhere it refuses exit 3,
the illegal-verb-in-state posture), changes no state, and `done` still
arrives only through `merge.observed`, behind the full chain rule
([`reconciliation.md`](reconciliation.md)). Its payload is strict:

```json
{"verdict": "pass" | "fail", "receipt": "<sha256 hex>", "submission": "<position>", "independence": "L1" | "L2" | "L3", "tuple"?: {…}}
```

Unknown keys refuse; `verdict` admits only the two literals;
`independence` is the level the record supports, exactly (the section
below): the literal `"L1"` alone before `seed/4`, the ordered
vocabulary from it. `tuple`, from `seed/4`, is the verifier's declared
runtime configuration ([`qualification.md`](qualification.md)'s five
fields, strict), present when the verifier declared one and refused
before `seed/4`. `submission` is the chain position of the
`submission.made` that put the subject in its current `review` state
(the fold records it): a verdict is bound to the submission it judges,
and citing any other position refuses. `receipt` is the SHA-256 digest
of the receipt's JCS bytes (below).

## Independence (L1) at admission

The **implementing-key set** of a contract is every fingerprint that
ever signed a `claim.taken` on it (the current holder and every prior
claimant, the fold's claim facts) plus the signer of the bound
`submission.made`. A `verdict.rendered` whose signer is in that set
refuses exit **17 `not_independent`**, naming the fingerprint and its
implementing role. This is deliberately distinct from exit 14
`out_of_grant`: the signer may hold a perfectly good `verdict` grant
and still be disqualified on this one contract — capability is global,
independence is per-contract.

The capability row is `verdict.rendered` → `verdict` **only**, with no
operator fallback (the one such row in `actors.md`): III.G names
operator override its own attributable verb, never a disguised verdict,
and that verb lands with 6.4. A governance root that wants to judge
holds an explicit `verdict` grant, and the independence check applies
to it like any signer.

Because `review` is outside `in_progress`, no fence is active: a
verdict citing one refuses under the established fence rule (a fence
dies with its claim window).

## Independence levels (L1, L2, L3) from `seed/4`

The charter's levels (§7, "Independence is failure-domain separation")
are **ordered**, achieved is **computed from the record, never
asserted**, and the tier table declares the minimum per tier
([`tiers.md`](tiers.md), the `independence` column: `trivial` L1,
`standard` L1, `critical` L2, the strictest row L3). Any achieved level
at or above the requirement satisfies it, which is III.G's "L2 or L3"
for high-consequence tiers. From `seed/4` (plans/os-99829835.md):

- **L1** holds for every admitted verdict: the key and workspace
  disjointness above.
- **L2** holds when the verifier's declared `tuple` differs from the
  claim window's admitted `run.started` declaration in model
  **provider or family**, or in **harness name**: the charter's
  "different model family or provider, different harness image".
  Principal, tool policy, environment and versions do not count; a
  three-part model (`<provider>/<family>/<version>`) compares provider
  and family, a two-part one family alone, so two providers serving
  one family through one harness are L2. A window with no admitted
  declaration supports no L2.
- **L3** holds when the record proves deterministic-first verification
  on a distinct path: the acceptance is executable and gated, and the
  cited receipt **reproduces**, `verdict check`'s recomputation from
  the verifier's own checkout yielding the cited digest. That is the
  record-backed determinism predicate: a nondeterministic command, a
  model call that answers differently, or an evidence path the
  implementer could shape yields a different transcript digest and
  fails to reproduce. Distinct acquisition is the workspace rule
  above. The boundary checks the executable-and-gated half (it runs
  nothing); recomputation and reconcile's evidence grade check the
  reproduction. What the record cannot prove is stated: the `exec`
  profile declares the network rather than denying it, and a
  deterministic command that still reaches a model is caught only when
  the model disagrees with itself; the denying profile is Phase 13's
  adapter.

**The verifier declares its tuple at render, as the supervisor declares
the worker's at start.** `seed verdict render --principal <p> --model
<m> --tool-policy <t>` fills `harness` and `environment` from the
workspace adapter it verifies in and never invents the three it cannot
know. The declaration is a declaration, not a report: what the
verifier ran as is what its operator says, exactly as the worker's is;
the qualification machinery that would prove it is
[`evals.md`](evals.md) applied to verifier keys, Phase 10 item 4's.

**Enforced at admission, at render, and along the merge chain.** The
verdict rule requires `independence` to be a member of the vocabulary
and to **equal** the level the record supports (a claim below the
computed level would underreport the audit data the level exists to
produce; one above it asserts what the record does not show), and the
level to satisfy the tier's requirement, read through `TierGates`. A
level short of the tier refuses exit **17 `not_independent`** with the
refining code **`level_short`** ([`envelope.md`](envelope.md)): the
family's answer, "this verifier cannot judge this contract", stands,
and the word says it is the configuration rather than the key. `seed
verdict render` computes the achieved level from the same facts before
drafting, so the client never drafts a doomed verdict, and on a tier
the record cannot satisfy without a declaration it refuses at usage
naming the three flags. The fold records the level and the tuple, and
**the merge chain reapplies both**: the boundary `merge.requested`,
`merge.observed`, `contract.returned` and the red-verdict lockout
consult refuses a folded verdict whose recorded level the record does
not support or which is short of the subject's tier, so a raw-pushed
`critical` verdict at `L1` cannot be laundered into `done`;
[`reconciliation.md`](reconciliation.md)'s `independence_unverified`
surfaces the same two conditions and the evidence-grade reproduction,
a sealed subject's under an identity able to unseal (`seed reconcile
--key`, the maintenance actor's key), never skipped silently.

Before `seed/4` the level is the literal `L1` and a declaration has
nowhere to go; a `seed/3` chain keeps `seed/3`'s judgment
([`protocol.md`](protocol.md)).

## The verifier workspace

The verifier executes in **clean per-run isolation**: a detached local
clone of the repository at the submission head under a fresh unique
temp dir, with the origin remote removed and objects **copied, never
hard-linked** (`--no-hardlinks`: a same-filesystem local clone
otherwise hard-links loose object files, so a hostile spec command
overwriting one through the shared inode would corrupt the parent's
object store) — and deliberately not a `git worktree` checkout, whose
`.git` link shares the parent repository's refs and object store and
would hand a hostile spec command `git update-ref` reach back into the
host. Both holes are drilled. Parallel runs never collide (unique
dirs); cleanup fires pass or fail. The clone carries auto-gc disabled
in its own config from the moment it exists (`gc.auto`,
`gc.autoDetach`, `receive.autoGC`, written between the clone and the
checkout), so nothing the engine made mutates after the engine exits:
a collector git detaches after a checkout would otherwise race the
cleanup that follows it.

The charter's "sandbox with declared, minimal capability" lands as a
**runner capability profile, declared in the receipt**. v0 ships the
`exec` profile: every command runs via `sh -c` inside the workspace
with a scrubbed environment (an explicit minimal `PATH`; `HOME`,
`TMPDIR`, and `GIT_*` pointed inside the workspace; nothing inherited
from the invoking process), no path back to the parent repository, and
a per-command wall-clock timeout with process-group kill. The profile
declares `network: unrestricted` honestly — portable no-root execution
cannot deny the network, and a pretended boundary would be worse than a
declared one. The runner is an interface: a namespaced or containerized
profile slots in at the executor-adapter seam (build plan Phase 7
item 3, Phase 12 hardening) without touching verdict logic, and every
receipt names the profile its transcripts ran under.

The profile sets one more variable per command, **`SEED_TRACE_EXPORT`**
(plans/os-7fc2ca38.md D2): `<per-run root>/traces/<n>.json` for
visible command `n` and `<per-run root>/sealed-traces/<n>.json` for
sealed command `n`, where `n` is the command's index in the receipt's
transcript list, the same `n` a scorecard's `transcript:<n>` names. A
harness that attaches a trace to its run writes one OTLP/JSON
`ExportTraceServiceRequest` there and the receipt binds its shape
("Trace-shaped evidence" below); a harness that ignores the variable
writes nothing, binds nothing, and loses only the richer evidence.
The path is outside the clone, so `diff_sha256` and `files` never see
it, and inside the per-run root, so cleanup removes it pass or fail.
The profile's name stays `exec`: a transcript of a command that
ignores the variable is byte-identical to one run without it.

**The verifier's inputs are enumerable and exclusively self-executed or
self-read** (III.G row 4): the submission packet's anchors are used
only to *name* the range; every hash, diff, inventory, and transcript
is recomputed from the verifier's own checkout and the ledger. Nothing
in the receipt repeats an implementer claim.

## The receipt

The receipt is JCS-canonicalized JSON; its digest is the SHA-256 of the
canonical bytes; the body is stored content-addressed in the artifact
store (`internal/artifact`, filesystem-rooted under
`var/artifacts/sha256/<digest>`; the build plan's git-addressed
`refs/seed/artifacts` push is deferred, recorded in the decision log).

```json
{
  "contract": "<subject>",
  "merge_base": "<full sha>",
  "head": "<full sha>",
  "plan": {"path": "<path>", "sha256": "<hex>"} | null,
  "diff_sha256": "<hex>",
  "files": ["<changed path>", ...],
  "transcripts": [{"cmd": "<command>", "exit": 0, "output_sha256": "<hex>", "output_bytes": 0}, ...],
  "environment": {"os": "<GOOS>", "arch": "<GOARCH>", "go": "<version>", "runner": "exec"},
  "commitment": "<hex, sealed subjects only>",
  "sealed_transcripts": [{"cmd": "...", "exit": 0, "output_sha256": "<hex>", "output_bytes": 0}, ...],
  "traces": [{"transcript": 0, "shape_sha256": "<hex>", "spans": 3, "errors": 1} | {"transcript": 2, "malformed": true}, ...],
  "sealed_traces": [...]
}
```

`merge_base` and `head` resolve from the submission packet's mandatory
`base` range (`packets.md`) to **full commit SHAs, stored immutably**;
a head that does not descend from its merge-base refuses before
checkout. A verdict attests exactly the `{merge_base, head,
diff_sha256}` triple and nothing else — never a ref or branch name.
Branches are forge state the ledger cannot see: "the merge landed code
other than the attested head" is precisely the verdict/merge
divergence 6.2's reconciliation detects by comparing `merge.observed`'s
forge fact against the attested head. `plan` is the approved plan blob
hashed **at the merge-base** (null for a planless trivial-tier
contract); `diff_sha256` and `files` recompute from `git diff` in the
workspace; transcripts carry each command, its exit, and the digest and
byte count of its combined output — never inline bytes, so receipts
stay bounded. **Verification recomputes everything from the submission
head and fails on mismatch** — a first-class verb (`seed verdict
check`, refusing exit **21 `receipt_mismatch`**), not only a test.
Check verifies two things and names what failed: the cited artifact is
retrievable intact from the store (the evidence a verdict points at
must survive verbatim), and the fresh recomputation reproduces the
cited digest. It runs in **every post-submission state**, not only
`review`: reconciliation needs it exactly after `merge.observed` has
moved the contract on, and the fold retains the bound submission —
only `receipt` and `render` stay review-gated.

On a **sealed** subject ([`sealed-checks.md`](sealed-checks.md)) the
receipt gains the charter's "visible and sealed check transcripts":
`commitment` is the ledger's salted hash the run unsealed against, and
`sealed_transcripts` the sealed commands' outcomes, run under the same
profile in the same workspace; both are omitted on unsealed subjects,
so every pre-6.3 receipt's canonical bytes and digest are unchanged.
The recompute-and-mismatch guarantee covers them: `verdict check` on a
sealed subject requires `--key` with an identity able to unseal,
decrypts, verifies the commitment, and reruns the sealed commands into
the recomputation — invented sealed transcripts in a raw-pushed
receipt recompute differently and fail at exit 21, an identity outside
the recipient set refuses exit **23 `not_recipient`**, and a broken
seal (missing or tampered ciphertext, commitment mismatch, or an
empty-checks envelope) refuses exit **22 `seal_broken`**. There is no
silent partial verification of a sealed subject. `verdict receipt`
stays the visible-half preview; the render is the authoritative
sealed run.

## Trace-shaped evidence

A test harness that attaches a trace to its run (a span tree whose
spans carry a status and structured outcome attributes) has more to
say than one output digest holds: which span failed, and how. The
receipt binds that structure without breaking the rule that makes a
receipt evidence, because only the part that reproduces enters it
(plans/os-7fc2ca38.md D2 to D8; charter §II.1, §II.8, III.G rows 5
and 8; build plan §3 "Borrowed from practice").

**The contract is a file.** The runner sets `SEED_TRACE_EXPORT` per
command (the profile, above); the harness writes an OTLP/JSON export
there, the format every OpenTelemetry SDK's file exporter emits and a
shell script can write by hand. Seed reads the documented span fields
(`resourceSpans[].scopeSpans[].spans[]`, in proto3 JSON or snake_case
names) with `encoding/json` and imports no OpenTelemetry package
(`internal/traceshape`).

**The shape is what reproduces.** From one export, spans group by
trace id and form a tree by parent id; a span whose parent is absent
from the export is a root, and a trace with orphaned spans yields one
root per orphan. A node is `{"name", "kind", "status", "attributes",
"children"}`: `kind` the span kind as a word (`unspecified`,
`internal`, `server`, `client`, `producer`, `consumer`), `status` the
status code as `unset`, `ok` or `error`, `attributes` the **declared**
keys only with each value rendered to its plain JSON counterpart, and
`children` sorted by their own JCS bytes; roots sort the same way. The
shape document is `{"transcript": n, "sealed"?: true, "traces":
[...]}`, JCS-canonicalized; `shape_sha256` is the SHA-256 of those
bytes. Stripped by construction: trace, span and parent ids, start and
end times, durations, events, links, resource, scope, the status
message, dropped counts and every undeclared attribute. Declared keys
come from the acceptance spec's `## Trace attributes` section
([`acceptance.md`](acceptance.md)), read at the anchor exactly as the
commands and the rubric are (`plan.TraceAttributes`); an absent
section declares nothing, so name, kind and status alone reproduce; a
duplicate, empty or whitespace-bearing key refuses at receipt as
`spec_unrunnable`. Because the spec merges through the acceptance
gate, an implementer cannot widen the retained set without review, and
a per-run key such as a run id stays undeclared or the receipt stops
reproducing, which is the honest outcome.

**The receipt gains `traces` and `sealed_traces`**, both omitted when
no command wrote an export, so every earlier receipt's canonical bytes
and digest are unchanged. An entry is `{"transcript": n,
"shape_sha256", "spans", "errors"}`, the counts derived from the shape
(`errors` counting nodes whose status is `error`). An export that
exists but does not parse yields `{"transcript": n, "malformed":
true}` and nothing else: a fact the receipt records, with no effect on
pass or fail (exit codes decide that), surfaced by `seed verdict
traces` and uncitable. Reproduction needs no new predicate: `verdict
check` recomputes the receipt from its own run, the shape digest is
part of the canonical bytes, and the whole-receipt comparison at exit
21 covers it; L3's "reproduces" gains coverage, not a rule.

**The shape is an artifact; the raw export is its sidecar.** The
verifier stores the shape document content-addressed (its digest is
`shape_sha256`) and the raw export content-addressed on its own, with
a pointer under `traces/<shape_sha256>` naming the raw digest
(`internal/artifact`, the sealed bucket's sibling). The raw digest
enters neither the receipt nor the ledger: it never reproduces (ids
and timestamps differ on every run), so it cannot live in a receipt
that must, and `verdict.rendered`'s payload is a strict object, so an
optional field there would be a protocol bump for a reference the
shape already covers. `verdict check` verifies that every entry's
shape retrieves intact, the scorecard's rule one artifact over, and
never reads the sidecar; `seed reconcile` grades a shape the store
lost `evidence_missing` wherever the verdict stands. `seed artifact
erase` of the raw digest removes the export a reader could open and
costs a check nothing; erasing the shape digest removes the shape and
its pointer, and the check is red until the evidence is restored.
Check never writes to the store.

**A span is cited by its path in the shape.** The scorecard evidence
grammar gains `trace:<n>/<path>` and `sealed-trace:<n>/<path>`, the
path dot-separated child indexes from the root list down: `trace:2/0`
is the first root of transcript 2's shape, `trace:2/0.3.1` its fourth
child's second child. A citation resolves only when the receipt
carries the entry, the entry bound a shape, the shape is at hand (the
run just produced it) or retrieves intact from the store, and the path
names a node; anything else refuses at render naming the citation, as
an unknown transcript does. Because children sort by canonical bytes,
the path is as reproducible as the digest.

**One read verb.** `seed verdict traces --ledger <dir> --subject <id>
--repo <dir> (or --artifacts <dir>) [--receipt <digest>]
[--transcript <n>]` renders each entry's tree with every node's
citation path, kind, status and declared attributes, names malformed
entries and missing shapes, and carries the raw export's digest where
the sidecar stands. It reads the receipt the latest rendered verdict
cites, else the standing deferral's, else the one `--receipt` names,
and refuses `not_found` with none. This is the surface a verifier
scores a rubric from. III.G row 10's query is unchanged: the cache's
`verdict_receipt` digest retrieves the receipt, whose entries retrieve
the shapes; the projection build carries no artifact store, so no
cache table is added.

**Not an observability subsystem.** No collector, no exporter, no
live stream, no change to the observation channel or to `run.settled`:
§II.18 forbids a second run log, and a trace is evidence a verdict
cites, not a signal a supervisor reads. Any exporter or collector a
deployment runs is an adapter detail; the receipt sees a file.

## Gate-before-run

Before any command runs, the verifier reads the contract's folded
acceptance (`acceptance.md`): `executable: true` without gate evidence
refuses exit **18 `ungated`** with nothing executed — the
gate-before-run half III.F row 1 assigns to verdicts, consuming the
projection's `gated` flag. `executable: true` whose gated body yields
no parseable commands refuses exit **19 `spec_unrunnable`**: the
declaration promised runnable content, the body carries none, and
silence must never decide — a vacuous pass is not a pass.
`executable: false` runs nothing and the receipt carries an empty
transcript list.

The command grammar is the plan grammar: the acceptance body's
"validation commands" marked section, extracted by the same walk
`internal/plan` lints (`plan.Commands`), one transcript entry per
command line.

## Rendering derives from the transcripts

`seed verdict render` computes the receipt fresh and **derives the
permissible verdict from the transcripts it just executed**: any
nonzero transcript exit forbids `--verdict pass`, refusing exit **20
`checks_red`** with the failing command named; `fail` is always
renderable; a prose-only spec
(no transcripts) leaves pass or fail the verifier's explicit judgment.
The enforcement seam is the render verb because the verifier is the
only party holding the transcripts it just ran — admission can neither
re-run commands nor read a verifier-local artifact store (the
cooperative-posture precedent: the client refuses to draft doomed
work). A raw-pushed pass-over-red is not silent: `seed verdict check`
recomputes from the submission head and goes red, and that mismatch is
6.2 reconciliation input.

**The red-verdict lockout** (6.4, plans/os-d2497eb7.md): once an
**authenticated** fail verdict judges the bound submission, rendering
`pass` refuses — at admission and at render, exit **25
`red_locked`** — until a **new submission** arrives through
`contract.returned`, a fresh claim, and a resubmission
([`lifecycle.md`](lifecycle.md)). Only boundary-validated fails lock
(verdict grant plus implementing-key disjointness): the tolerant fold
records any well-shaped raw verdict, so the lockout scans the whole
submission window and an unauthenticated fail locks nothing,
authorizes nothing, and surfaces as `verdict_unverified`. Fail
restatements stay renderable, and no implementer-held lane can clear
the lockout. The operator's escape hatch is `merge.overridden`
([`reconciliation.md`](reconciliation.md)): it clears the merge path,
never `verdict.rendered(pass)` — an override substitutes for a
verdict, it does not manufacture one. Sealed checks ride the same rule: a red
sealed transcript forbids pass exactly like a visible one, and render
on an **above-trivial subject with no commitment refuses exit 24
`unsealed`** — the "contracts carry sealed checks" gate, enforced at
the verifier boundary where checks run; the trivial tier is exempt
(`sealed-checks.md`).

## The rubric and the scorecard

Acceptance that cannot be a command (tone, judgment, taste) is a
**rubric the verifier scores item by item with cited evidence and
explicit uncertainty, never a single holistic score** (SEED-NEXT.md
§7; plans/os-2e34f66a.md D1 to D4). The rubric is a section of the
acceptance spec, `## Rubric`, whose bullets are items `- <id>:
<criterion>` ([`acceptance.md`](acceptance.md), "The rubric"), read at
the anchor exactly as the commands are (`plan.Rubric`); a spec may
carry both sections, gate-before-run covers the rubric as it covers
the commands, and a rubric with a duplicate, empty or non-slug id
refuses at render as `spec_unrunnable`.

**The scorecard is an artifact the verdict cites; the
derivation-bearing half travels in the signed payload.** The
verifier's scoring is one JCS-canonical scorecard in the artifact
store, `{"contract", "submission", "items": [{"id", "score":
"pass"|"fail", "evidence": ["<path> @ <commit>#L<a>-L<b>" |
"transcript:<n>" | "trace:<n>/<path>" | "sealed-trace:<n>/<path>",
…], "uncertainty": "low"|"high", "note"?}]}`, and
`verdict.rendered` gains optional **`scorecard`** from `seed/4`:
`{"digest", "items": [{"id", "score", "uncertainty"}]}`, the
artifact's digest and, per item, exactly the two enums the derivation
reads. Evidence and notes are bulk and stay in the artifact; ids and
two enums are coordination facts, on the record so every boundary that
consumes a verdict reapplies the derivation from the record alone
(admission carries no artifact store by design). `seed verdict render
--scorecard <file>` validates it against the rubric and the receipt:
every rubric item scored exactly once, an unknown id refuses, every
item cites at least one evidence reference (an anchored path resolving
in the repository at its commit, a transcript the receipt carries, or
a span in a shape the receipt binds, never prose), `note` within the
classification budget (512 bytes),
`uncertainty` two values because the charter asks for explicit
uncertainty and a routing decision, and three would invite the middle.
A spec with a rubric renders only over a scorecard; a scorecard on a
spec without one refuses at usage.

**Render derives the verdict from the scorecard exactly as from the
transcripts** (`transition.DeriveScores`): `pass` requires every item
`pass` at `low`; a `fail` item forbids `pass`, refusing exit **20
`rubric_red`** naming the item, and leaves `fail` renderable; an item
at `high` forbids BOTH verdicts, refusing exit **20 `human_verdict`**
naming the item, since low confidence routes to a human; and with a
rubric the verdict IS the derivation's, so `fail` over a scorecard
whose every item passes refuses too. The admission rule reapplies the
same derivation to the payload's items (a verdict whose own items
refute it never lands), and **`verdictBoundary` reapplies it** beside
the grant, the disjointness and the level, so a folded verdict that
fails its own derivation authenticates nothing: `merge.requested` and
`merge.observed` citing it refuse, and the red-verdict lockout does
not count it. That is the record-derivable half. The artifact half is
`seed reconcile`'s: a cited scorecard that does not retrieve, or whose
stored items disagree with the payload's, classifies
**`scorecard_unverified`** ([`reconciliation.md`](reconciliation.md)),
`receipt_mismatch`'s posture one artifact over. **The residual,
stated:** a verifier that signs a self-consistent false record (every
item `pass` in the payload and the artifact, over work that deserved
none) is the verifier's lie, which L1, calibration
([`evals.md`](evals.md), "Calibration") and reconcile answer, as they
answer a false pass over a red receipt today.

**The human verdict is a deferral fact and an operator-standing
verifier key, never an escalation.** `verdict.deferred` (catalog
growth under `verdict.*`, `seed/4`) is a fact admitted in `review` by
a `verdict` key under L1, changing no state, carrying `{"receipt",
"submission", "scorecard"?, "items"?}`: the receipt the verifier
computed, the bound submission, and where the spec carries a rubric
its scorecard and the ids it scored at `high`. It creates the
obligation **`verdict.human`** owed by the operator lane
([`obligations.md`](obligations.md)), and **a human is a key with
operator standing**: the tree's one structural proxy for a person,
the standing `decision.recorded` demands, where enrollment `kind` is
an assertion that decides nothing. After a deferral, and on a tier
whose `human review` column is `yes` ([`tiers.md`](tiers.md)) from the
first render, `verdict.rendered` on that submission admits only from
a signer holding an explicit `verdict` grant AND operator standing (a
governance root's implicit standing or an explicit `operator` grant),
under L1 like any verifier; a `verdict`-only key, the deferring one
included, refuses `human_verdict`, at render and at admission, and
raw-pushed authenticates nothing for the merge chain
(`verdictBoundary` reapplies the standing). `verdict.rendered`'s
accepted set stays `verdict` alone: operator standing is a second
requirement on these submissions, never a fallback, so III.G's "no
disguised verdict" holds. On a human-review tier the deferral is the
only machine act and may carry no items: the whole verdict defers, the
charter's "humans review only high-tier work" made structural.

**The human renders over the deferral's receipt.** Sealed checks
encrypt to verdict keys disjoint from `claim` and `operator`
([`sealed-checks.md`](sealed-checks.md)), so a key with operator
standing is never a recipient and can compute no receipt on a sealed
subject. The machine verifier computes the receipt at `seed verdict
defer`, stores it and cites it; the human's `seed verdict render`
retrieves that receipt intact from the store rather than recomputing,
validates its own scorecard against the rubric and that receipt, and
cites the same digest, so `seed verdict check` and reconcile recompute
it under a capable key as for any verdict. `seed verdict check` also
retrieves the cited scorecard and holds its stored items to the
payload's, the same check reconcile classifies as
`scorecard_unverified`, refusing `receipt_mismatch` over a store that
lost or altered it and reporting `scorecard: verified` otherwise
(review finding on the task PR). One deferral per window: a
second refuses, and so does one over a submission already judged; a
new submission clears it.

Refused: routing through `escalation.raised`. An escalation freezes
the contract and its answer returns the subject to `ready`, so the
verdict could never follow the decision on the submission it judged;
a deferral leaves the subject in `review` for the human's render.
`seed verdict defer --scorecard` appends the deferral; `seed
situation` surfaces the debt.

## Visibility

The contracts view is unchanged by 6.1: surfacing verdicts, submissions,
and divergence in projections rides 6.2's reconciliation work. The fold
records `submission {position, signer}` per contract for the binding
and disjointness checks above.

## Conformance mapping

- III.G row 3 (verdict-granted keys provably disjoint from every
  implementing key; override its own verb) — the `verdict` capability
  row, the L1 independence rule with exit 17, the operator-fallback
  omission; the override verb itself is 6.4.
- III.G row 6 (independence levels L1–L3 defined, declared per tier,
  enforced at verdict time, recorded in the verdict; high-consequence
  tiers require L2 or L3) — the levels section above, the tier table's
  `independence` column, the verdict rule's equality and tier checks
  with `level_short`, the fold's recorded level and tuple, the merge
  chain's reapplication, and `independence_unverified`.
- III.G row 8 (qualitative residue: acceptance that cannot be a
  command is a rubric the verifier scores item by item with cited
  evidence and explicit uncertainty, never a single holistic score;
  low-confidence items route to human verdict; rubric calibration runs
  against a human-scored gold set with automatic authority suspension
  on drift) — the rubric and the scorecard above, `rubric_red` and
  `human_verdict`, the deferral and the operator-standing render, the
  boundary's reapplication, `scorecard_unverified`, and calibration
  ([`evals.md`](evals.md)); drilled at the boundary, in the fold, at
  the terminal and end to end in the modes fixture.
- III.O row 2 (verifier calibration: scheduled sampling against a
  human gold set; automatic authority suspension on drift; defect
  contract filed) — calibration definitions with the gold held outside
  the tree, agreement against the spec-pinned floor, the `verdict`
  qualification and its tuple-wide disqualification, the dispatcher's
  defect filing, and the spot-check aging verdict qualifications
  ([`evals.md`](evals.md), "Calibration").
- III.G row 4 (clean per-run isolation; parallel verdicts never
  collide; cleanup fires pass or fail; enumerable, self-executed
  inputs) — the workspace, the runner profile, and their drills.
- III.G row 5 (receipts bind contract id, plan hash at merge-base,
  diff hash, inventory, transcripts, environment fingerprint;
  verification recomputes and fails on mismatch) — the receipt schema
  and `seed verdict check`; sealed-check transcripts join in 6.3; the
  trace-shaped entries (os-7fc2ca38) bind what a harness's trace
  export reproduces, and check verifies their shapes beside the
  receipt.
- III.G row 8, the cited-evidence clause (os-7fc2ca38) — a scorecard
  cites a span by its path in the shape, and `seed verdict traces` is
  the surface the paths are read from.
- III.G rows 1–2 (the reconciliation chain and divergence) — 6.2, with
  the verdict's immutable head attestation as its comparison anchor.
- Part II §6 (sandbox with declared, minimal capability) — the runner
  profile, declared per receipt, with the stronger boundary at the
  Phase 7 adapter seam.

## Rendering reaches both postures

`seed verdict render` takes `--ledger` or `--remote`. It was local-only
until Phase 9 item 4, which meant a fleet's verifier lane could not act
against the shared ledger its workers claim on: the terminal half of
the contract lifecycle had no reachable surface in the deployment the
charter's fleet mode describes ([`modes.md`](modes.md)).

The payload's `submission` is a chain POSITION, so on the remote path
it is re-derived against each refreshed tip and REFUSED on a change
rather than re-pointed — a verdict bound to whatever submission happens
to be current is the laundering shape, and binding is the whole point
of the field. The receipt is not re-derived: it is content-derived from
the repository rather than from the ledger view, so a moving tip cannot
change it.
