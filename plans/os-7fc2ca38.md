# Plan: next: trace-shaped test evidence in the receipt (os-7fc2ca38)

A receipt transcript today binds a command, its exit, and the digest and
byte count of its combined output (`next/spec/verdicts.md`, "The
receipt"). A rubric item cites either an anchored path or `transcript:<n>`.
When a test harness attaches a trace to the run (a span tree whose spans
carry a status and structured outcome attributes), all of that structure
collapses into one output digest: a verifier cannot cite the failing span,
`verdict check` cannot tell a run that failed the same way from one that
failed differently, and the III.G evidence query stops at the transcript.
This task lets a receipt bind that structure without breaking the one rule
that makes receipts evidence: verification recomputes everything from the
submission head and fails on mismatch.

Design authority is `SEED-NEXT.md` §II.1 (data classification: bulk content
lives in the artifact store by hash, with an erasure path), §II.8 and III.G
rows 5, 8 and 10 (receipts bind transcripts; rubric items cite evidence;
evidence is queryable), §II.18 (no second run log; exporters are adapter
details), and `docs/next-build-plan.md` §3 "Borrowed from practice
(2026-09-06)". The build-plan entry says the receipt binds "the
artifact-store digest of the exported spans"; D5 below corrects that for
the reproduction rule and records why. No Part III row changes status.

## Design decisions (binding for this task)

- **D1. One task PR, no child cards, no row flips.** The runner contract,
  the shape, the receipt field, the citation form and the read verb are one
  feature: a citation is meaningless without the shape, and the shape is
  unverifiable without the receipt field. `os-7fc2ca38` is implemented in
  one `seed/os-7fc2ca38` task PR. III.G rows 5 and 8 stay `met` with their
  evidence extended by this PR's drills; row 10 stays `routed` exactly as
  the Phase 10 record left it. Nothing here is conformance-blocking.
- **D2. The file contract is one environment variable.** The `exec` runner
  profile sets `SEED_TRACE_EXPORT=<per-run root>/traces/<n>.json` for
  command `n`, where `n` is the command's index in the receipt's
  `transcripts` (the same `n` a scorecard's `transcript:<n>` names), and
  `<per-run root>/sealed-traces/<n>.json` for sealed command `n`. The path
  is outside the clone, so `diff_sha256` and `files` never see it, and
  inside the per-run root, so cleanup removes it pass or fail. A harness
  that honors the variable writes one OTLP/JSON
  `ExportTraceServiceRequest` (`resourceSpans[].scopeSpans[].spans[]`), the
  file format every OpenTelemetry SDK's file exporter emits and a harness
  can write by hand. Seed parses it with `encoding/json` over the
  documented span fields and imports no OpenTelemetry package: the contract
  is a file the harness writes, never a vendor SDK. A harness that ignores
  the variable produces no file and no entry, and loses only the richer
  evidence. The profile's name stays `exec`: transcripts of harnesses that
  ignore the variable are byte-identical to today's.
- **D3. The shape is defined normatively and is the only thing that
  reproduces.** From one export, spans group by `traceId`; each trace is a
  tree by `parentSpanId`, a span whose parent is absent from the export
  being a root. A node is `{"name", "kind", "status", "attributes",
  "children"}`: `kind` the OTLP enum rendered as a word (`unspecified`,
  `internal`, `server`, `client`, `producer`, `consumer`), `status` the
  status code as `unset`, `ok` or `error`, `attributes` the **declared**
  keys only, each OTLP `AnyValue` rendered to its canonical JSON scalar,
  array or object, and `children` sorted by their own JCS bytes. Traces
  sort by their JCS bytes. The shape document is `{"transcript": n,
  "traces": [...]}`, JCS-canonicalized; `shape_sha256` is the SHA-256 of
  those bytes. Stripped by construction: trace, span and parent ids,
  start and end times, durations, events, links, resource and scope,
  the status message, dropped-count fields, and every undeclared
  attribute. Declared keys come from a `## Trace attributes` section of
  the acceptance spec, one `- <key>` line per key, read by
  `plan.TraceAttributes` exactly as `plan.Rubric` reads `## Rubric`; an
  absent section declares nothing, so name, kind and status alone
  reproduce; a duplicate or empty key refuses at receipt as
  `spec_unrunnable`, like a bad rubric. Because the spec merges through
  the acceptance gate, an implementer cannot widen the retained set
  without review, and a per-run key such as a run id stays undeclared or
  the receipt stops reproducing, which is the honest outcome.
- **D4. The receipt gains `traces` and `sealed_traces`, both `omitempty`.**
  An entry is `{"transcript": n, "shape_sha256": "<hex>", "spans": k,
  "errors": e}` with `spans` the node count and `errors` the count of
  nodes whose status is `error`, both derived from the shape. An export
  that exists but does not parse yields `{"transcript": n, "malformed":
  true}` and nothing else: a fact the receipt records, with no effect on
  pass or fail (exit codes decide that, as today) and surfaced by the read
  verb. A receipt for a run that produced no export has neither field, so
  every existing receipt's canonical bytes and digest are unchanged, the
  same rule `commitment` and `sealed_transcripts` followed. Reproduction
  needs no new predicate: `verdict check` recomputes the receipt from its
  own run, the shape digest is part of the canonical bytes, and the
  existing whole-receipt comparison at exit 21 `receipt_mismatch` covers
  it. L3's "reproduces" gains coverage, not a new rule.
- **D5. The shape is an artifact; the raw export is its sidecar.** The
  verifier stores the shape document content-addressed (its digest is
  `shape_sha256`) and stores the raw export under the named slot
  `traces/<shape_sha256>/raw.json`, the pattern the sealed slot already
  uses. The raw export's own digest enters neither the receipt nor the
  ledger: it never reproduces (ids and timestamps differ on every run), so
  it cannot live in a receipt that must; and `verdict.rendered`'s payload
  is a strict object, so an optional field there would be a `seed/8` bump
  for a reference the shape already covers. `verdict check` verifies that
  each entry's shape artifact is retrievable intact, the rule the scorecard
  follows, and never reads the raw sidecar, so `seed artifact erase` of
  the sidecar (bulk content that may carry anything the harness logged)
  never breaks a check. Check never writes to the store.
- **D6. A span is cited by its path in the shape.** The scorecard evidence
  grammar gains `trace:<n>/<path>` and `sealed-trace:<n>/<path>`, where
  `<path>` is dot-separated child indexes from the trace list down:
  `trace:2/0` is the root of transcript 2's first trace, `trace:2/0.3.1`
  its fourth child's second child. `resolveEvidence` accepts a citation
  only when the receipt carries the entry, the shape retrieves intact, and
  the path resolves to a node; anything else refuses at render naming the
  citation, as an unknown transcript does today. Because the shape sorts
  children by canonical bytes, the path is as reproducible as the digest.
  Reconcile's scorecard re-check applies the same resolver, so a shape
  that no longer retrieves grades `evidence_missing` there.
- **D7. One read verb, no cache change.** `seed verdict traces --ledger
  <dir> --subject <id> --artifacts <dir> [--transcript <n>]` renders each
  entry's tree with every node's citation path, status and declared
  attributes, and names malformed entries; it reads the receipt the bound
  verdict cites (or, before a verdict, the receipt `verdict receipt`
  stored) and refuses with the existing codes when there is none. This is
  the surface a verifier scores a rubric from. III.G row 10's query stays
  what #119 made it: the cache's `verdict_receipt` digest retrieves the
  receipt, whose entries retrieve the shapes; the projection build carries
  no artifact store, so no cache table is added and no schema generation
  advances.
- **D8. Not an observability subsystem.** No collector, no exporter, no
  live stream, no change to the observation channel or to `run.settled`.
  The spec text says so in the §II.18 words, so the next reader does not
  file the collector this task deliberately omits.

## Steps

1. **Specify.** In `next/spec/verdicts.md`: the `SEED_TRACE_EXPORT`
   contract under the runner profile, the shape definition, the two receipt
   fields and the malformed entry, the shape artifact and its raw sidecar
   with the erasure rule, the citation grammar, the read verb, and the D8
   posture. In `next/spec/acceptance.md`: the `## Trace attributes`
   section beside `## Rubric`. `protocol.md`'s version and
   `transitions.json` are untouched.
2. **Parse and normalize.** Add `plan.TraceAttributes` beside
   `plan.Rubric`. Add the pure package `next/internal/traceshape`: OTLP/JSON
   parsing over the documented fields, tree building, normalization per
   D3, JCS bytes and digest, and path resolution per D6.
3. **Bind at the runner.** `verdict.Runner.Run` sets the variable and, after
   the command exits, reads the export if present; `verdict.Compute` folds
   the entries into `traces` and `sealed_traces`, reading declared keys from
   the spec. Store the shape and the raw sidecar in `storeReceipt`; extend
   `verdict check` to retrieve each shape intact.
4. **Cite.** Extend `resolveEvidence` and reconcile's scorecard re-check with
   the two citation forms.
5. **Render.** Add `seed verdict traces`, register it, and cover it in the
   handbook and registry tests the way sibling subverbs are.
6. **Drill and record.** Add the named drills below; append the decision
   log line, the progress line, `memory/*` entries and the receipt the
   normal task workflow requires. Flip no conformance row; extend rows 5
   and 8's evidence strings with this PR.

## Named conformance drills

- **`TestTraceShapeNormalizes` (`internal/traceshape`):** two exports of
  the same run with different ids, timestamps, durations, sibling order,
  an undeclared per-run attribute and a differing status message yield
  identical bytes and digest; a changed span name, kind, status code,
  declared attribute value, or tree position changes the digest; an orphan
  parent becomes a root; a malformed document reports malformed rather
  than an empty shape; a path resolves to the node it names and refuses
  beyond the tree.
- **`TestReceiptBindsTraceShape` (III.G row 5):** a spec with two commands,
  one honoring `SEED_TRACE_EXPORT` with a two-trace export carrying an
  `error` span and an undeclared run id, one writing nothing, yields one
  entry with the right `spans` and `errors`; a second `Compute` from a
  fresh workspace reproduces the receipt digest; the export path is absent
  from `files` and gone after cleanup; a sealed command's export lands in
  `sealed_traces`; a spec with no `## Trace attributes` section retains
  no attributes; a duplicate key refuses `spec_unrunnable`.
- **`TestTraceArtifactsStoredAndChecked` (III.G row 5, D5):** `verdict
  receipt` stores the shape and the raw sidecar; `verdict check` passes;
  erasing the sidecar leaves check passing; erasing the shape refuses at
  exit 21 naming the shape; a raw-pushed receipt with an invented `traces`
  entry recomputes differently and refuses at exit 21; a pre-trace receipt
  fixture's digest is byte-identical before and after this change.
- **`TestScorecardCitesTraceSpans` (III.G row 8, D6):** a rubric item citing
  `trace:0/0.1` renders; a path beyond the tree, a transcript with no
  entry, a malformed entry, and a `sealed-trace:` citation against a
  visible transcript each refuse at usage naming the citation; after
  erasing the shape, reconcile grades the verdict `evidence_missing`.
- **`TestVerdictTracesRenders` (D7):** the read verb renders the tree with
  citation paths, statuses and declared attributes, names a malformed
  entry, renders nothing for a pre-trace receipt, and refuses with the
  existing code when the subject carries no receipt.

## File Scope

- `next/spec/verdicts.md`, `next/spec/acceptance.md`
- `next/internal/traceshape/**` (new)
- `next/internal/plan/plan.go` and its tests
- `next/internal/verdict/verdict.go`, `workspace.go`, `scorecard.go`,
  `input.go` and their tests; `next/internal/artifact/artifact.go` for the
  named slot and its test
- `next/internal/reconcile/reconcile.go` and its tests, for the resolver
- `next/cmd/seed/verdict.go`, registry and handbook wiring, and the CLI
  drills above
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`, and
  `receipts/os-7fc2ca38.json`

Explicitly out of scope: `SEED-NEXT.md`, `docs/next-build-plan.md`,
`next/spec/protocol.md`'s version, `next/spec/transitions.json`,
`next/spec/conformance.json` statuses, the cache schema, the observation
channel, any collector or exporter, any v1 surface, and child cards.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. A command that writes an OTLP/JSON export to `SEED_TRACE_EXPORT` yields
   a receipt entry whose shape digest reproduces across runs that differ in
   ids, timestamps, durations, sibling order and undeclared attributes, and
   changes when a name, kind, status code, declared attribute or tree
   position changes.
2. The shape is retrievable from the artifact store by the receipt's
   digest; the raw export sits in its named sidecar slot; erasing the
   sidecar never affects `verdict check`; erasing the shape or inventing an
   entry refuses at exit 21.
3. A rubric item cites a span by path and renders; every malformed or
   unresolvable citation refuses at render naming it; reconcile grades a
   vanished shape `evidence_missing`.
4. `seed verdict traces` renders the tree a verifier scores from, and the
   contract is documented with the §II.18 posture stated.

**Retention set (existing, shown unharmed):**

- Every existing receipt fixture's canonical bytes and digest are unchanged;
  transcripts of commands that ignore the variable are byte-identical.
- `seed/0` through `seed/7` histories verify; no protocol bump, no
  `verdict.rendered` payload change, no new envelope code.
- Existing scorecard, sealed-check, independence and reconciliation drills
  pass unchanged; `make check` retains the coverage and generated-doc gates.
- No cache schema generation advances; no v1 surface and no `plans/**` file
  changes in the implementation PR.

## Validation Commands

- Boundary: `cd next && go test ./internal/traceshape/ ./internal/verdict/ ./internal/plan/ ./internal/artifact/ ./cmd/seed/ -run 'TraceShape|ReceiptBindsTrace|TraceArtifacts|ScorecardCitesTrace|VerdictTraces' -count=1`
- Boundary (reconcile): `cd next && go test ./internal/reconcile/ ./cmd/seed/ -run 'Reconcile|Evidence|ScorecardCitesTrace' -count=1`
- Retention: `cd next && go test ./internal/verdict/ ./internal/artifact/ ./internal/reconcile/ ./internal/admit/ ./internal/plan/ ./cmd/seed/ -run 'Receipt|Verdict|Scorecard|Seal|Artifact|Reconcile|Independence|Rubric|Protocol' -count=1`
- Retention: `make check`

## Expected diff shape

One new pure package with its drill, one section each in two spec files,
targeted additions to the runner, receipt, scorecard resolver, reconcile
re-check, artifact slot and one CLI subverb, plus the record artifacts.
Approximately 12 to 18 implementation files and 500 to 800 net new lines,
with no protocol, transition-table, conformance-status, cache, charter,
build-plan or plan-file edit in the task PR.
