# os-cf13fb51 — Phase 12 item 5: migration from open-seed, drilled against a real export

Build plan Phase 12 item 5: `seed import --from-open-seed <export>` — v1
lossless export → verify anchors → transform (cards → contracts, run-log
entries → events, receipts → verdict records, mail → messages) →
genesis import refusing non-empty ledgers — drilled against a real v1
fixture. Charter §II.17 ("Importing a predecessor is an adopter path …
prior tamper-evidence is verified before conversion — and import refuses
non-empty ledgers. Documented, two-command, drilled against a real
predecessor fixture"), Appendix D.2 and D.3 ("the import boundary is the
genesis transform, not per-system compatibility code in the core"),
III.P row 4. This card carries promotion's migration gate (build plan
§5 criterion 3): drilled against a real export of THIS repository's v1
state, not only a fixture.

## What the tree actually shows

- **v1's export is one document.** `scripts/seed state export` dumps
  `{schema_version: "1.0", backend: "filecards", head: <seed-state
  commit>, files: {path: content}}`. Today's store: 137 `tasks/<id>.md`
  (YAML frontmatter — id, title, state, priority, created_at,
  updated_at, plus the body with `## Evidence ev-… (kind, actor, ts)`
  blocks), 110 `handoff/<id>.md` continuation packets, and
  `run-log.jsonl` with 1,169 attributed entries over eighteen verbs
  (`claim`, `transition`, `attach-evidence`, `create`, `promote`,
  `plan-unblock`, `close`, `lease-renew`, `comment`, `cancel`,
  `blocker_resolved`, `unblock`, `accept`, `halt`, `state-resume`,
  `state-repair`, `record-evidence`, `release`) by six actor names.
  Mailboxes appear under `mail/` when unpruned. `state import` loads an
  export into a fresh v1 store — the predecessor's own two-command
  path, which is the shape to mirror.
- **v1's tamper-evidence is the anchor tag.** `scripts/seed state
  anchor` tags the state head as `seed-anchor/<ts>` (three exist at
  planning: `…T212410Z`, `…T222604Z`, `…T232406Z`); the export's `head`
  is a commit on `seed-state`. Receipts and plans live in the
  repository (`receipts/<id>.json`, `plans/<id>.md`), not in the state
  ref, and the run-log's `attach-evidence` entries cite them by path.
- **v1 identities are names, not keys.** `actor` is an asserted string
  (`shaunlmason`, `seed-next-implementer`, `seed-maintenance`,
  `claude-session`, `claude`, `shim`); Seed's `actors.md` says
  attribution is not trust and kind is an operator's assertion. An
  import cannot manufacture the trust v1 never had, and must not
  pretend to.
- **Seed's side is complete enough to replay a history.** Genesis
  (`seed init`, `ledger_not_empty` at exit 3), the transition table
  and lifecycle verbs, claims with fences, four-part packets on every
  exit, `contract.specified`'s acceptance object, the verdict chain
  with receipts as artifacts, `message.sent` with `to`, escalations,
  and the classification lint that refuses content bodies in
  payloads (512 bytes per string) — an imported card body is a
  reference to an artifact, never a payload.

## Design decisions (binding for this task)

**D1. Two commands, and the second refuses before it transforms.**
`scripts/seed state export > export.json` (v1, unchanged) and
`seed import --from-open-seed export.json --source <clone> --ledger
<dir> --artifacts <dir> --key <importing operator key> [--anchor
<tag>]`. Before any transform: the export's schema and backend are
checked; `head` must be the commit the named anchor tag points at (or,
with no `--anchor`, the newest `seed-anchor/*` tag in `--source`'s
history, named in the output), or an ancestor of it on `seed-state`;
and every file in `files` must equal, byte for byte, the blob at that
path in the tree at `head`, with no file in the tree absent from the
export. A mismatch refuses `export_mismatch` under exit 21 (the
recomputation family: a cited state that does not reproduce) naming
the path; a `head` no anchor covers refuses `unanchored` under exit 8.
Refused: importing from the export alone — an export nobody anchored
is a document, not evidence.

**D2. Genesis import refuses a non-empty ledger, and the import is one
signed provenance record plus a replayed history.** The target ledger
must be empty (`ledger_not_empty`, exit 3, the existing refusal) or
hold exactly the genesis this same invocation wrote. Right after
genesis the importer appends `system.imported` (new, operator-only,
`seed/5`'s one addition): `{source: "open-seed", export_head, anchor,
manifest}` where `manifest` is the artifact digest of the mapping
manifest (D5). Then the history is replayed as ordinary events through
`admit.Check`, every one admitted or the import refused — a history
Seed's own rules would not admit is not imported by loosening the
rules. Refused: a bulk write that bypasses admission; a second
`system.imported` on a ledger that has one.

**D3. Provenance is asserted, and the assertion is explicit.** The
importer enrolls one Seed actor per distinct v1 actor name, with keys
it generates and holds for the import, kind from the mapping table
(`shaunlmason` human, `seed-next-implementer` agent, `seed-maintenance`
service, and so on; unknown names default to `agent` and are listed),
and grants sufficient for the verbs that name performs (derived from
the run-log before replay, minimal, and listed in the manifest). Every
imported event is signed by the mapped actor's key with the original
`ts`, so fences, grants and the chain are replayed under Seed's rules
rather than narrated; `actors.md`'s rule that attribution is not trust
is what makes this honest, and the manifest says the keys are the
importer's. After the replay the import appends `actor.suspended` for
every import-generated identity, so no imported key holds standing a
real operator did not grant. Refused: signing everything with the
importer's key (then the importer holds every claim and the replay
proves nothing); shipping the generated private keys anywhere but the
importer's temp dir.

**D4. The transform is a table, complete over the export's verbs, and
a drop is a row.** `next/spec/import-open-seed.json`: v1 card states →
Seed states; v1 run-log verbs → Seed events or a named drop with its
reason (`lease-renew` drops — Seed holds no lease; `state-repair`,
`halt`, `state-resume` drop with the entry recorded in the manifest;
`nudge`-class entries drop). `create` → `intent.filed` (intent: the
title; tier `standard`, budget `small`, routing the squad; overridable
per card by a labels map in the table); `promote` → `contract.specified`
with acceptance `{ref: "tasks/<id>.md@<head>", executable: false}`
(the card at the anchored head is a resolvable path in this
repository's own `seed-state` history) or the merged plan's path at
its merge commit where `plan-unblock` cites one; `claim` →
`claim.taken`; `release` → `claim.released` and every `transition`
out of `in_progress` → the matching deliberate exit with a packet
synthesized from the `handoff/<id>.md` mechanical sections (task, git
anchors; decisions and findings carrying the marker `imported` and the
handoff's artifact digest, never invented prose); `attach-evidence`
citing a receipt → the receipt file from `--source` stored as an
artifact and cited by `verdict.rendered(pass)` from a mapped verifier
identity, then `merge.requested` and `merge.observed` from the
evidence's PR number and merge commit, then done; `close` with
`cancelled` → `contract.cancelled`; `comment` and `mail/*` →
`message.sent` (body ≤ 512 bytes inline, else an artifact reference);
`blocker_resolved`/`unblock` → `decision.recorded`/`contract.unblocked`
as the table maps them. A run-log verb absent from the table refuses
`import_unmapped` (exit 3) before any write. Refused: silently
skipping an entry; a drop without a row.

**D5. Losslessness is a check, not a claim.** The mapping manifest (an
artifact, cited by `system.imported`) records, for every export
record — each run-log entry, card, handoff, mail — the event positions
it became, the artifact it became, or the drop row it matched; the
drill asserts every record has exactly one disposition and that the
count of dispositions equals the count of records. The receipt and
plan files copied from `--source` are stored as artifacts by digest,
so a reader can retrieve what a v1 card cited.

**D6. The fixture is this repository's own state, and the drill is in
CI.** `next/fixtures/import/open-seed/`: `export.json` taken with the
v1 command at a named anchor, and `seed-state.bundle` — a `git bundle`
of `seed-state` and the `seed-anchor/*` tags up to that anchor — so CI
verifies the real anchors with no network. The drill imports it into
an empty ledger, verifies from genesis, folds every contract to the
state the card's frontmatter declares (`done`/`cancelled`/`review`/…,
the mapping making that a computed equality), runs the losslessness
check, then proves the refusals: a tampered export (one card body
edited) refuses `export_mismatch` before any write; a `head` with no
anchor refuses `unanchored`; a non-empty ledger refuses
`ledger_not_empty`; an unmapped verb refuses `import_unmapped`. A
`make fixture-import` recipe regenerates the fixture from the live
repository so the gate stays real as v1 history grows.

**D7. Bounds.** Only open-seed's export is understood (Appendix D.3's
route for other systems is the same table with different rows, and no
per-system code enters the core). Imported contracts in `review` or
`in_progress` at export time replay to that state and are then the
deployment's to finish or reap. No attempt is made to reconstruct
verdict receipts v1 never recorded: a done card without a receipt maps
to done through `merge.overridden` by the importing operator with the
reason `imported: no receipt`, which is the honest verb for a done the
chain cannot justify. The two-command path is documented in the spec
and in the handbook item 6 writes.

## Steps

1. `internal/importer` (new): the export reader, the anchor
   verification against `--source`, the mapping table loader and its
   completeness check, the identity plan, the replay through
   `admit.Check`, the manifest.
2. `internal/transition` / `internal/admit`: `system.imported` at
   `seed/5` (operator-only, once per ledger), the `seed/5` register
   entry.
3. `cmd/seed/import.go` (new), `cmd/seed/main.go`.
4. `next/spec/import-open-seed.json` (the table), the fixture under
   `next/fixtures/import/open-seed/`, the `Makefile` recipe.
5. Drills D6; mutation evidence.
6. `next/spec/import.md` (new), `protocol.md` (`seed/5`, the verb),
   `actors.md` (import-generated identities, suspended after replay),
   `envelope.md` (`export_mismatch`, `unanchored`, `import_unmapped`),
   `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/importer/**` (new), `next/internal/transition/**`,
  `next/internal/admit/**`, `next/internal/version/**`
- `next/cmd/seed/import.go` (new), `next/cmd/seed/main.go`, and drills
- `next/spec/import-open-seed.json` (new), `next/fixtures/import/**` (new)
- `Makefile` (the `fixture-import` recipe)
- `next/spec/import.md` (new), `next/spec/protocol.md`,
  `next/spec/actors.md`, `next/spec/envelope.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-cf13fb51.json`

Nothing outside `next/**` except the Makefile recipe and the
work-product files above. NOT `.seed/**`, NOT `scripts/**` (the v1
export is used, never changed), NOT `.github/workflows/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **Anchors first.** A `head` no anchor covers refuses `unanchored`;
   an export whose content differs from the anchored tree refuses
   `export_mismatch` naming the path; both before any write.
2. **Genesis import.** A non-empty ledger refuses `ledger_not_empty`;
   an empty one gains genesis, one `system.imported` citing the
   manifest, the replayed history, and the suspensions of every
   import-generated identity; the chain verifies from genesis.
3. **The real fixture replays.** This repository's export imports in
   CI with every event admitted through `admit.Check`, every contract
   folding to the state its card declares, and every v1 actor name
   mapped to an enrolled identity of the declared kind.
4. **Lossless, checked.** Every export record has exactly one
   disposition in the manifest and the counts agree; every receipt and
   plan a card cited is retrievable from the artifact store by digest.
5. **The table is complete or the import refuses.** An unmapped verb
   refuses `import_unmapped`; every drop is a row with a reason.
6. **Two commands, documented.** The spec and the fixture's README
   show the exact two commands, and the drill runs the second against
   the first's output.
7. **Mutation evidence.** Each fails a drill: transforming before the
   anchor check; importing into a non-empty ledger; signing every
   event with the importer's key; an entry skipped without a row;
   import-generated identities left active; a receipt cited but not
   stored.
8. `make check` green with coverage measured cold, three readings above
   the gate; the suites pass unprivileged; no model identifiers in any
   committed artifact.

**Retention set (existing, shown unharmed):**

- Every pre-existing chain verifies byte for byte; `seed/5` is
  additive and `system.imported` refuses before it; no existing verb,
  transition row or capability changes; projections are byte-identical
  on chains this card does not create.
- `seed init` without an import behaves as before; the v1 `scripts/seed
  state export` and `state import` are untouched.

## Validation Commands

- Boundary: `cd next && go test ./internal/importer/ ./internal/transition/ ./internal/admit/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

New: `next/internal/importer/`, `next/cmd/seed/import.go`,
`next/spec/import-open-seed.json`, `next/spec/import.md`,
`next/fixtures/import/open-seed/` (export, bundle, README). Modified:
`next/internal/transition/`, `next/internal/admit/`,
`next/internal/version/` (`seed/5`), `next/cmd/seed/main.go`, one
`Makefile` recipe, three specs, the three docs files, the receipt. No
`.seed/`, `scripts/` or workflow change.
