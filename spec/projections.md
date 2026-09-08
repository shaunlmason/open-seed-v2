# projections.md — the projection engine

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II "Projections"; [`docs/build-plan.md`](../docs/build-plan.md)
> Phase 4; plan `plans/os-4d5cacff.md`. Implemented by
> `internal/project` and the `seed project rebuild` verb.

## The five properties (normative)

Every projection is **derived** (never written directly), **stamped**
(with the ledger position it was built at), **rebuildable** (deletion
loses nothing; the rebuilt tree is byte-identical), **read-only
outward** (a build never writes the ledger), and **non-authoritative**
(no decision may prefer a projection over the ledger; staleness is
visible, never silent).

## The engine contract

- **Input**: the verified record prefix. The engine opens the ledger
  with the read-only open (it is structurally incapable of the healing
  writes the ordinary open performs) and verifies from genesis; a
  verification failure — a stale `HEAD` included — refuses the build
  before anything is written.
- **Determinism**: a projection is a pure function of the records;
  outputs are written in sorted order with no timestamps, so one prefix
  always produces one byte-identical tree.
- **Path safety**: the ledger directory and the output root are
  canonicalized, and any overlap in either direction refuses as a
  usage error before anything is created — a projection target can
  never coincide with authoritative state.

## Publication: immutable builds plus a pointer

A directory rename cannot atomically replace a non-empty directory, and
delete-then-rename opens the very window atomicity forbids, so
publication is versioned:

```
<out>/<name>/CURRENT                                 the active build id (atomically renamed into place)
<out>/<name>/builds/<position>-<tip12>-v<version>/   immutable, complete build trees
```

The build id derives from the stamp — the verified position and tip
**and the projection's derivation version** — so one prefix under one
derivation reproduces one identical id and tree, `CURRENT` included,
which keeps the byte-identical rebuild drill meaningful; and a
projection whose build logic changes (Phase 5 replacing the queue's
derivation, say) bumps its version and republishes under a new id
instead of being discarded as a same-id duplicate. The pointer is
swapped only once the tree is complete. After the swap, the build the
pointer named immediately before is **retained** — a reader that
resolved `CURRENT` just before the swap must find a complete tree at
the path it holds — while everything older, and stray partials,
prune; a reader that loses the race to two consecutive swaps
re-resolves `CURRENT`. A killed build leaves at worst an orphan
directory.

## The stamp

Every build tree carries `projection.json`:

```json
{"name": "<projection>", "position": <verified record count>, "tip": "<chain tip hash>", "version": "<derivation version>"}
```

The stamp is the staleness surface: consumers read it, may demand a
minimum position, and never treat the view as authoritative. The
`seed project rebuild` envelope reports the same values per projection.

**Declared inputs** (`observations.md`): a projection that declares
input consumption and receives an observation snapshot carries the
declared-inputs digest — computed over the snapshot, `as_of`, and
the thresholds together — in the stamp (`"inputs": "<digest>"`,
omitted otherwise) and appends it to the build id as a fourth
segment, `<position>-<tip12>-v<version>-i<digest12>`, so ANY changed
input at an unchanged tip republishes under a new id instead of
being discarded as a same-id duplicate. Input-free projections never carry
the field or the segment and are byte-identical with and without
inputs by construction.

## Registered projections

Candidate actors derive from the chain itself (the genesis payload's
governance roots plus every enrollment subject, first-appearance
order); every actor-bearing view keys off that one derivation. The v0
**work classifier** is a prefix rule: verbs outside the `system.*` and
`actor.*` governance namespaces are work vocabulary, keyed by subject;
Phase 5's transition table replaces the rule with explicit vocabulary.

- **`roster`** (`roster.json`): every keyring entry — genesis
  governance roots included, appearing with `root: true` and empty
  kind/name (they are seeded from the genesis payload, not enrolled) —
  plus every enrolled actor with kind, name, standing, root flag, and
  accumulated grants, in first-appearance chain order.
- **`contracts`** (`contracts.json`): every work subject in
  first-appearance order — `{subject, first_position, last_position,
  events: [{position, verb, actor, payload}]}` with the signer
  fingerprint as `actor` and the payload's JSON content unchanged
  (the view re-indents for readability; canonical bytes live only in
  the ledger). Each entry carries the transition table's folded
  `state` (null for a subject no lifecycle event ever validly
  created) and an `anomalies` count — lifecycle events the table
  refused, tolerated in raw-pushed history per the cooperative
  posture, skipped by the fold, surfaced here, never silent — and,
  while a subject is `in_progress`, a `claim` object
  `{holder, fence}` (the holder's fingerprint and the admitted
  `claim.taken` position, string form), absent outside a claim
  window, so contention answers and stale-fence refusals are
  independently checkable against the published view — and, once
  specified, an `acceptance` object `{ref, executable, gated}` so
  "may this spec run?" is a projection read (`acceptance.md`)
  (Version "4"; `lifecycle.md`).
  One file, not per-subject files (subjects are opaque strings; the
  cache is the lookup-throughput surface). An empty chain yields an
  empty array, not a missing file. Version 14 adds the
  `racing` object on a subject that raced (plans/os-56bee171.md D4):
  `racers` (the active claims by fence order), and after settlement
  `settled_at` and `settled_out`; absent on every other subject, so
  chains without a race are byte-identical. Version 15 adds the
  `topology` object (plans/os-f0ae2cdf.md D8; [`topology.md`](topology.md)):
  the relations, the unresolved dependencies, `effective_ready`,
  `held_by`, the mission anchor and its carrier, the goal-ancestry
  warning and the rollup, present only when the prefix carries a
  trusted relation fact, so relation-free chains stay byte-identical.

- **`queue`** (`queue.json`): the claimable-work surface —
  `{schema_version: "1", derivation, ready: […]}`, entries carrying
  `{subject, since_position}`. The derivation was
  `"transitions/1"` (`lifecycle.md`; Version "2"): `ready` lists the
  subjects whose folded lifecycle state is `ready`, oldest first,
  `since_position` the chain position that made each ready. The v0
  `"none"` marker is retired exactly as promised — a consumer MUST
  NOT treat an underived queue as meaning "nothing to do", and the
  marker names which derivation decided. Version "4" narrows the set
  under the derivation `"transitions/1+topology/1"`
  (plans/os-f0ae2cdf.md D4; [`topology.md`](topology.md)): the
  effectively ready, those with every active dependency terminal and
  no blocked ancestor, read from the topology fold; relation-free
  chains list the same set as before.
- **`actors`** (`actors.json`): the per-actor drill-down — the roster
  fields plus `standing_history` (each `actor.*` event on the subject:
  position, verb, acting signer) and `signed` (position, verb, subject
  of every record the fingerprint signed). The attribution surface: a
  revoked key keeps its full history here while the roster shows the
  ended standing.
- **`report`** (`report.json`): the operational summary — `chain`
  (position, tip, active version), `actors` (counts by standing,
  roots, total), `halt` (halted flag; declaring position and actor
  while halted), `checkpoints` (count; last position when any exist),
  `contracts` (subject and event counts), and `observation`
  (Version "2"; `observations.md`): null on an input-free build, else
  the declared inputs echoed (`as_of`, digest, thresholds) plus one
  expiry-vs-wedge classification per active claim, read only from the
  claim's own fence-keyed stream, and `refusals` (report version
  "10"; [`refusals.md`](refusals.md); version "11" adds the `knowledge`
  section and its `contested` count, [`curation.md`](curation.md), so
  an unchanged tip republishes with the section; version "12" its
  `retired` and `stale` counts, the latter at the declared instant;
  version "15" carries `requests` when the prefix holds a request
  ([`requests.md`](requests.md), from `seed/7`: total, unanswered, by
  kind, by outcome, mean answer latency in elapsed seconds), a
  section absent from every chain that carries none, so no version
  moves for it (no chain before `seed/7` can hold one);
  version "15" splits `lanes` by the acting key's roster kind
  (`by_kind`: a specification under its appender's kind, an approval
  under its approver's, the kind read from the keyring at the record's
  own position; [`postures.md`](postures.md), "The preseed"), and the
  cache moves to generation 13 with it; version "16" adds `strongest`
  to `lanes.planner`, the `tuples` scope of the latest applied offer
  that carries one ([`ranking.md`](ranking.md)), absent when none has;
  version "18" adds `blind_retries`, `blind_retries_by_code` and
  `undigested` to `refusals` ([`refusals.md`](refusals.md);
  plans/os-a9e715dc.md D3), which only a build declaring a journal
  carries, so an input-free build's bytes move by the version alone;
  version "13" adds `lanes`, [`trajectories.md`](trajectories.md):
  `dispatcher` `{specified, respecified, retriage_rate}`, subjects
  with one or more applied specifications, those with two or more,
  and their ratio, and `planner` `{approvals, unedited, edited,
  unmeasured, unedited_rate}`, approvals split by whether the
  approval's digest equals the first proposal's, the rate over the
  measured ones; rates are three-decimal strings, null at a zero
  denominator, and the section is null when no work subject exists,
  the reconciliation section's posture; record-derivable from the
  fold alone, no new projection registered;
  version "14" the `flywheel` section, [`flywheel.md`](flywheel.md):
  null on an empty ledger, else the recurring, proposed and merged
  shape counts, the repair contracts filed and done, and the
  conversion rate):
  null unless a rebuild declares
  an attempts journal, else the affordance-gap metric — outcome
  counts over the journal's one population, refusal breakdowns, the
  stamped-position span as context, and the four-decimal rate. The
  report is the one
  input-consuming projection; a remaining section needing later facts
  (budgets) stays a named extension point, not emitted empty. Offer
  facts land in the contracts view and the cache's `offers` table
  ([`offers.md`](offers.md)); the report carries no offer section —
  liveness is an instant-relative read, and the report is
  position-identified.

- **`obligations`** (`obligations.json`): what is OWED on each
  subject — kind, owed-by, the position it arose at, and the verbs
  that discharge it, derived from the same fold admission enforces
  and never a new authority ([`obligations.md`](obligations.md)). The
  situation read consumes the same derivation at the ledger tip.

- **`knowledge`** (`knowledge.json`, version 3): the curation
  pipeline's stages ([`curation.md`](curation.md)): the stage counts
  (observations, hypotheses, promoted, contested, lessons, unbound,
  and `retired` and `stale` when non-zero), dead ends by contract with
  their retirement flags, hypotheses with their stage (`proposed`,
  `promoted`, `contested`) and `single_actor_family` where the actor
  arm was waived, contests by hypothesis, the latest promotion per
  lesson path each with `surfaces`, `stale`, `retired` and the reason
  when it does not surface (the record half: promoted, not contested,
  not retired, not expired at the declared instant; the repository
  half, ancestry and digest, is the reader's, since a build holds no
  repository), the standing retirements, the unbound promotions and
  the anomaly count, from the curation fold. The projection declares
  input consumption since version 3: the declared observation inputs'
  `as_of` is the instant staleness is judged at, echoed as `as_of`;
  with no instant declared nothing is flagged and `staleness` says so
  (both fields present only once the chain holds a lesson). `seed
  knowledge show [--now]` renders the same derivation at the tip. The
  report carries a `knowledge` section with the stage counts when the
  chain holds any curation fact, so builds of chains that hold none
  stay byte-identical.

- **`ranking`** (`ranking.json`, version 1): the strongest qualified
  tuples per capability ([`ranking.md`](ranking.md)): `as_of` (the
  latest qualification fact's `ts`, empty until one exists, never the
  tip's, so an unrelated append changes nothing), `agreement_refined` (always `false`: the gold is
  outside the tree), and `capabilities` with `claim` and `verdict`
  each an ordered list of entries (tuple, score, latest, holders,
  evidence, agreement). Input-free and byte-identical on the same
  chain; a chain carrying no qualification builds two empty lists.
  `seed doctor --ledger` and `seed offer publish --strongest` read the
  same derivation at the tip.

- **`cache`** (`cache.db`): the single-machine read-throughput
  surface — one SQLite database mirroring the views (`roster`,
  `contracts`, `offers`, and `reservations` indexed by subject,
  `queue` + `queue_meta`,
  `actor_history`/`actor_signed` indexed by fingerprint, `report`
  key-values), every per-event table carrying the envelope's `ts`
  verbatim beside `ts_unix`, the instant it names as nanoseconds since
  the epoch (NULL when the string does not parse, such rows queryable
  by that NULL and counted under the `report` table's `ts_unparsed`
  key), so evidence is
  queryable by contract, actor, time and outcome in one query (charter
  III.G row 10; plans/os-74ce2261.md) — a range compares `ts_unix`,
  never the text, since RFC 3339 mixes fractional precision — plus a
  one-row `stamp` table carrying **exactly** the
  tree stamp's fields (name, position, tip, version), so a pure-SQL
  consumer demands a minimum position with one query:
  `SELECT position >= :min FROM stamp`. The database is the API:
  consumers open it read-only (`mode=ro&immutable=1`); a writable
  open is a programming error, and the locked publication refuses
  in-place writes mechanically. Builds are **byte-identical** like
  every view: the builder closes SQLite's variance sources (one
  connection, one ordered transaction, rollback journal — never
  WAL, whose headers embed salts — fixed `page_size`, no
  `auto_vacuum`, no `ANALYZE`, no `AUTOINCREMENT`) against the
  `go.sum`-pinned driver, and the registry's byte-identical drill
  enforces it; the database's own schema generation is stamped via
  `PRAGMA user_version`, bumping with the table set. The builder
  assembles the file in an engine-owned temp directory and hands the
  engine bytes (coordination-scale ledgers make that cheap; a
  streaming seam is deliberately deferred until a real ledger
  outgrows it). Tamper recovery is the documented deletion walk plus
  one rebuild — a same-id republish deliberately keeps the existing
  tree for readers that hold it; the locks, not the discard, are the
  anti-tamper layer.

The write-boundary lint is 4.4's.
Registration is data: later phases append.

**`boundary`** (version "1"; [`boundary.md`](boundary.md)): the
cross-organization task view — `tasks.json`, the index, and
`tasks/<request>.json` per `cross-repo` request, each the pinned
fields alone (`request`, `answer`, `state`, `artifacts`), the state
one of five derived from the chain. A chain carrying no cross-repo
request builds the index empty and nothing else, so no other
projection moves for it.

## The consumer verb and staleness

`seed project current --name <projection> [--out <dir>]
[--min-position <n>]` resolves `CURRENT`, reads the build's stamp,
and reports `{name, position, tip, version, path}`. Two position
conventions coexist and are both normative: the **stamp** (and this
verb's envelope) carries the verified record **count**; the **rebuild
envelope** stamps the tip's zero-based **index** (count-1), the
CLI-wide tip convention. Consumers demanding freshness pass
`--min-position`: a stamp below the demand refuses with exit 15
`stale`, naming the stamped and demanded positions — charter III.D's
"consumers can demand a minimum position", made scriptable. The stale
refusal is computed at a verified stamp, so its envelope carries that
stamp's position like any post-ledger response (`spec/envelope.md`):
machine consumers detecting staleness read the observed position
structurally, not out of the message text. Only **registered**
projections resolve: a name outside the registry refuses exit 4
`not_found` whatever directories exist under the output root (which
also keeps traversal components out of the path), and a registered
name with nothing published refuses 4 the same way — absence meaning
the projection's own directory does not exist; a published layout
that exists but cannot be resolved — a missing, unreadable, or empty
`CURRENT` (publication swaps `CURRENT` atomically, so a layout
without its pointer is a damaged publication, not an unpublished
one), an unreadable, unparseable, or incomplete stamp (wrong name,
empty version, a tip inconsistent with its position) — refuses
exit 5 `unavailable`, an operational failure, never mistaken for an
unpublished projection. The verb takes no `--ledger` flag: it is
structurally a consumer and cannot touch authoritative state; no
refusal creates or modifies anything.

## Write boundary

III.D's "no code path writes a projection directly" is enforced in
three layers; the boundary is **code-path discipline**, which is what
that row claims — not tamper-proofing against a root-privileged actor.

1. **The vocabulary lint (Lint A).** No non-test Go file outside
   `internal/project` may contain the publication vocabulary
   literals (exactly `"CURRENT"`, `"projection.json"`, `"builds"`):
   nobody constructs projection paths by hand. Test files are exempt
   (drills read the layout to assert it; reading is not a violation).
2. **Seam/write separation (Lint B).** A non-test file outside the
   engine that imports the engine must contain no `os` write-family
   calls (`WriteFile`, `Create`, `OpenFile`, `Rename`, `Remove`,
   `RemoveAll`, `Mkdir`, `MkdirAll`, `Chmod`, `Truncate`, `Link`,
   `Symlink`): the file that can obtain a published path (the engine
   returns real paths by design; views exist to be found and read)
   is a file that cannot write one. Both lints live in one
   `go/parser` test in the engine's own suite — a test in the suite
   is wired into `check-next` by construction — and both are
   self-checked against planted fixtures, so a detector that fails to
   fire is itself a test failure.
3. **Locked publication.** Published trees carry `0444` files and
   `0555` directories — the projection root **and the output root
   itself** included, since rename permission lives in the parent and
   a writable parent would let a whole projection root be renamed
   away — so rename-over, unlink-plus-recreate, in-tree creation,
   `CURRENT` repointing, and root renames all fail at the operating
   system for every non-engine code path, however the path was
   obtained. The engine opens a write window (`0755`) on exactly the
   output root, the projection root, and `builds/` for its own swap
   (after verification, keeping refuse-before-write intact) and every
   return path relocks, failed publications included — a *partial*
   open rolls itself back too, so a directory that refuses to open
   (say `builds/` occupied by a regular file) never strands the ones
   already opened writable; every published
   mode is set by explicit `chmod`, so the process umask cannot
   weaken the protocol. Only a killed process leaves an open window —
   at worst writable directories and an orphan partial, never a
   broken view — and the next rebuild relocks everything.

**Deletion.** With directories read-only, a bare `rm -rf` needs the
mode walk first (`chmod 0755` every directory under the output root,
then remove); `seed project rebuild` runs the same walk itself, so
the sanctioned one-command recovery is unchanged: rebuild.

**Residual risk, named.** The lints bind single files: deliberately
splitting seam access and writes across files, or writing through
`syscall` directly, evades them — the locked trees stop the former,
root renames included. File modes stop no process that may `chmod`
(uid 0 bypasses permission checks entirely), so mode-refusal drills
require an unprivileged runner, which CI provides; and the output
root's own parent belongs to the invoker's filesystem, so renaming
the output root itself is outside the engine's ownership — it is
equivalent to repointing `--out`, and consumers that name the root
by configuration are unaffected by what modes cannot reach.
Authority safety does not rest on any of this: projections stay
non-authoritative, stamped, and rebuildable whatever happens to the
files.

## Conformance mapping

- III.D "every read surface is a deterministic function of a ledger
  prefix … stamped with its build position, and rebuildable
  byte-identically with one command" — the engine + `seed project
  rebuild`, drilled in `internal/project`.
- III.D "no code path writes a projection directly …; the
  write-boundary lint enforces it" — the three layers above ("Write
  boundary"), implemented as code-path discipline: both lints
  self-checked and green over the tree, locked publication drilled
  with refused replacement operations and bytes-unchanged assertions.
- III.D "Staleness is visible everywhere projected state is shown;
  consumers can demand a minimum position" — every view is stamped and
  `seed project current --min-position` refuses stale builds with exit
  15, drilled end-to-end.
- III.D cache row — "The cache projection delivers single-machine
  read throughput with zero authority (mid-operation deletion loses
  nothing)" — the `cache` projection above, drilled: indexed lookups
  equal the views, deletion under an open read handle loses nothing
  (the ledger byte-unchanged, one rebuild republishing the identical
  build), and a poisoned copy never feeds a rebuild.
- III.D "External mirrors are one-way exporters; mirror-side edits
  arrive only as request events from governed identities, validated at
  admission; a conformance suite passes per exporter/adapter" — Phase
  13 item 8: "The mirror" below, `TestMirrorExporterSuite` over every
  registered exporter and `TestMirrorExportAndRequestIngressPerExporter`
  joining the export to [`requests.md`](requests.md)'s ingress.
- III.D "Bidirectional synchronization is structurally impossible: no
  component holds both an export path and a coordination write path"
  — Phase 13 item 8: "Components" below, `internal/authoritylint`
  (`TestAuthorityBoundaryHoldsInTheTree`, self-checked by
  `TestAuthorityBoundarySelfCheck`).
- III.D "External facts enter only as observations by governed
  observers; nothing treats an observation as control" — Phase 13 item
  8: [`external-facts.md`](external-facts.md).
- The CI rebuild-everything drill — with its phase.

## The basis file

`<out>/basis.json` says what a root's builds rest on when they were
published by `seed project start` under a `signers` declaration
([`checkpoints.md`](checkpoints.md)): the trust, the checkpoint's
position, the position trusted up to and its attested tip, the signer,
and how many records were trusted and how many verified. It lives in
the root rather than in every build's stamp so that a build from a
checkpoint and a build from genesis stay byte-identical; a full
`project rebuild` into the same root removes it, because a replay rests
on nothing but the chain. Consumers that care read it beside the stamp.

## The adapters section (Phase 13 item 2)

The report gains a `topology` section (version 19; plans/os-f0ae2cdf.md
D8; [`topology.md`](topology.md)): `initiatives` with their rollups,
`goal_ancestry_warnings` over open unanchored work, and the relation
`anomalies` the fold kept but does not trust, present only when the
prefix carries a relation fact. The cache (Version "15", schema
generation 13) mirrors it: the `relations`, `topology_state`,
`topology_anomalies` and `goal_ancestry_warnings` tables, the
report's `topology` key, and the queue's effective derivation in
`queue_meta`.

The report gains an `adapters` section (version 17): per executor
substrate, the runs started under its harness and its budget posture
(`enforced` or `risk-limit`), derived from the `run.started` tuples the
fold holds. Present only when the prefix carries a `run.started` whose
tuple names a harness (a `seed/2` or later start); a `seed/1` start
carries no tuple and adds nothing, so chains without one stay
byte-identical. A cloud or remote adapter
never reads `enforced`, and an unknown harness defaults to a risk limit.
See [`executors.md`](executors.md).

## Components (Phase 13 item 8)

A **component** is a deployable executable: a `main` package under
`cmd/`. Its **closure** is the transitive import graph of
production packages within the module, parsed from source. Two classes
are derived from the closure, never listed by hand:

- the **export path**: the sealed mirror registry (`mirror`) and
  every package under it, the only construction path `seed-mirror`
  opens an exporter through;
- the **coordination write path**: the packages owning a ledger append,
  a git-ref commit or push, or a proposal primitive
  (`internal/ledger`, `internal/gitref`, `internal/propose`), and every
  package with a call site of one, seen through aliased imports.

III.D row 6 is then a falsifiable structural claim, and
`internal/authoritylint` makes it under `go test`: **no component's
closure holds both classes.** `seed-mirror` is on the export path and
holds no writer; `seed` and `seed-admit` are writers and do not import
the registry. The lint also refuses an export package importing a
writer, a type declaring the adapter method set outside the registry
(an exporter the suite never ran), and an export component defining a
Seed-side flag (`--ledger`, `--key`, `--remote`, `--config`, `--as`,
`--propose`, `--admission`). It self-checks against synthetic package
graphs: a clean split passes with both classes derived non-empty, and
each planted overlap fails by name. Bidirectional synchronization would
need one executable to hold both, which the lint turns red before it
ships.

## The mirror (Phase 13 item 8)

`seed-mirror plan|apply --current <file> --forge github|forgejo|snapshot`
reads the `contracts` build the consumer verb resolved (`--current` is
the envelope `seed project current --name contracts` printed, naming
the published build's path, position, tip and version; the mirror
reads the view inside it and resolves no layout itself, so the
engine's vocabulary stays the engine's and freshness is demanded at
the consumer verb) and exports one issue per
contract with a valid lifecycle state: title the opaque subject, body
exactly two markers, one managed `seed:<state>` label; `done` and
`cancelled` closed, every other state open. The marker is
`<!-- seed-mirror: <base64 of the subject's UTF-8 bytes> -->`, so no
subject can terminate or re-open the comment, and decoding is the only
parse path; the second marker carries the stamp's `position` and
`tip`, so a reader can name which projection the mirror reflects.

The plan is sorted by subject and byte-deterministic. Unmanaged issues
and foreign labels are untouched; a managed issue whose title, body,
managed label or open/closed state drifted is overwritten from the
projection, foreign labels kept; a subject with no issue is created; a
duplicate marker, a marker that does not decode, or a marker naming a
subject the projection does not hold refuses rather than choosing an
authority by accident. Applying a plan twice is a no-op the second
time, because the second plan is empty. An adapter failure is reported
with the exporter's name and the failed action, and the Seed side is
bytes on disk the component never opened.

GitHub, Forgejo and the file-backed snapshot are the three registered
exporters, behind one adapter interface (`Name`, `List`, `Create`,
`Update`, nothing else); the registry is sealed, so an exporter the
conformance suite never ran cannot be opened. Labels are repository
objects on both forges, so an exporter defines a managed label through
the label API before an issue names it (Forgejo by id, GitHub by name,
a person's label defined meanwhile re-read rather than duplicated): a
transport variance, not a contract one. Every forge call is bounded by
a timeout, so an unanswered connection cannot hang an apply. Tokens come from `SEED_MIRROR_GITHUB_TOKEN`
or `SEED_MIRROR_FORGEJO_TOKEN` (or `--token-env`), never a flag, the
projection, the plan, the output or a fixture.

The mirror has no way back in. A person's edit at the forge is
overwritten by the next export; what it has is a proposal, and that
enters Seed by exactly one door, `seed request file` with an enrolled
standing-only service key ([`requests.md`](requests.md)), a different
component. The per-exporter drill proves the round trip: export,
edit, the ledger byte-unchanged, the request admitted and changing no
lifecycle state, a direct coordination act by the service key refused
out of grant, and the export restoring the mirror.

