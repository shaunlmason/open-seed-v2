# The attempts journal and the refusal-rate metric

Charter III.I row 4: refusal rates are tracked as an affordance-gap
metric — a rising rate signals an affordance gap, not agent error
(SEED-NEXT.md §II.10). Refusals never reach the chain: admission
refuses before anything is written, so the metric has a recorded
source only if the refusing boundary records it. This spec defines
that source (the attempts journal), how it enters the report build
(a declared input, the observations pattern of
[`observations.md`](observations.md)), and the section it produces.

## The journal

`attempts.jsonl` lives in the ledger directory, beside `segments/`
and `HEAD`. It is operator-local telemetry: never synced by gitref,
never read by verification, never part of the chain. One JSON line
per admission-boundary attempt, **both outcomes** — a rate needs its
numerator and denominator drawn from one population, so admissions
are journaled exactly where refusals are (plan review finding on
os-edf73d66: a refusals-only journal forces the denominator onto the
chain, where a refusal-bounded span miscounts and other actors'
admissions pollute an operator-local numerator).

```json
{"ts": "…", "position": "12", "actor": "<fp>", "verb": "claim.taken",
 "subject": "c-1", "outcome": "refused", "code": "fenced",
 "digest": "<sha256 of the act>"}
```

- `ts`: the journaling process's RFC3339 UTC instant.
- `position`: the tip-ordinal position the response envelope
  stamped, verbatim (decimal string).
- `actor`: the signing key's fingerprint.
- `outcome`: `admitted` or `refused`.
- `code`: the envelope's machine code — present exactly on refusals.
- `digest`: the identity of the **act** attempted
  (plans/os-a9e715dc.md D1): the lowercase hex SHA-256 of the RFC 8785
  canonical form of `{"actor", "verb", "subject", "payload"}`, the
  payload embedded as the JSON value it is (a non-object payload,
  which the boundary refuses at the shape rule, as a string). `ts`,
  `prev` and the version are excluded on purpose: they are the
  boundary's coordinates and a retry changes them by construction, so
  a record's own hash can never match across one, while two attempts
  of the same act by the same key digest alike wherever the tip stood.
  This is what lets a reader tell a **blind retry** (the same act
  re-sent unchanged after a refusal) from a corrected one. Written on
  every line the seams below journal; absent on lines written before
  the field existed, which still load; a present digest that is no
  sha256 refuses the load naming the line.

Writes are best-effort and never fail or slow the verb they ride
(the affordance-stamping posture): any error is swallowed, and a
short write restores the previous file length when the fragment is
provably the tail, so a full disk cannot leave a fragment for the
next append to glue onto. Reads are
strict: the journal is a *declared input*, so a malformed line
refuses the build that declares it, naming the line — garbage is the
declarer's error, never silently skipped telemetry. The one carve-out
is the commit-marker rule: the terminating newline is what commits a
line, so a final unterminated fragment (a torn short write, a crash
mid-append) is an uncommitted attempt and is ignored, never counted
and never fatal — a best-effort writer must not be able to poison
every future build of the journal it feeds.

## The seams

A response journals its attempt when it stamps a position and was
signed: the CLI's admission-boundary surfaces, success and refusal
renders alike. Wired today: `ledger append` (its keyring-preview and
chain-invalid refusals, and its success; the budget and lifecycle
verbs ride this seam), `offer publish`, `verdict render`, and
`seal create` (each including their remote-refusal renders).
Responses without a stamped position (usage errors, store-level
failures before a tip was read) are not boundary attempts and
journal nothing; read surfaces (`ledger show`/`verify`,
`budget status`, `offer list`) never journal. The remote server-side
admission boundary is a named extension point: the report builds
locally, and the local journal is the local truth.

## The journal's second reader

The trajectory recorder ([`trajectories.md`](trajectories.md)) reads
the journal beside a local ledger for the refused half of a lane's
decision points: each refused line the lane's key journaled becomes a
point framed at `records[:p+1]`, the stamp being the last record of
the view the boundary judged the attempt against. The recorder reads
refusals only (the chain is the record of what was admitted), skips
and counts lines from other actors and lines stamped beyond the tip,
and refuses a journal that does not load, the same strictness as the
report build: a torn journal would silently omit decision points. The
seams above are therefore also the seams a trajectory can see; a
refusal a read surface never journals is a decision point the harness
cannot record.

## The declared input and the report section

`seed project rebuild --refusals <file>` loads the journal and
declares it to the build; it composes freely with the observation
family's `--obs`/`--as-of`. The declared-inputs digest covers every
declared family (each contributes its keys only when declared, so an
obs-only digest is unchanged by this spec existing), keys the build
id's input segment, and stamps the projection
([`projections.md`](projections.md)).

The report gains a nullable `refusals` section — null on builds that
declare no journal, absence of data stated, never fabricated:

```json
"refusals": {
  "inputs": {"digest": "<journal digest>", "entries": 101},
  "refused": 1, "admitted": 100,
  "by_code": {"fenced": 1}, "by_verb": {"claim.taken": 1},
  "span": {"from": 10, "to": 11},
  "rate": "0.0099",
  "blind_retries": 0, "blind_retries_by_code": {}, "undigested": 0
}
```

- `inputs.digest` is the journal's own RFC 8785 identity; the
  stamp's `inputs` field carries the full declared-inputs digest.
- `refused`/`admitted` are the journal's outcome counts: one
  population. **The chain is never the denominator.**
- `by_code`/`by_verb` break down refusals only.
- `span` is the min/max stamped position across all attempts —
  positional context, never an input to the rate; null on an empty
  journal.
- `rate` is refused/(refused+admitted) rendered to exactly four
  decimals (`0.0000` when the journal is empty), deterministic
  bytes; consumers needing more precision divide the counts.
- `blind_retries` is III.R row 5's blind-retry clause measured
  (plans/os-a9e715dc.md D3; [`simulation.md`](simulation.md), the
  guardrail bar), by the definition [`modes.md`](modes.md) gives the
  loop drills: the same act refusing with the same code on consecutive
  attempts from a position that did not advance. For each refused line
  that carries a digest, the same actor's **next** journaled attempt on
  the same subject, in journal order, is a blind retry when its digest
  equals the refusal's, it is refused with the same code, and its
  stamped position equals the refusal's; each refusal counts at most
  once. An admitted next attempt is convergence (the first arm), and a
  refusal with another code or from an advanced position is a new
  answer to a new state; neither is blind. `blind_retries_by_code`
  splits the count by the code, so the spins the optimistic loop makes
  on `contention` read apart from a `fenced_out` or
  `invalid_transition` act re-sent unchanged; `undigested` is the lines
  that carry no digest and so could not be judged (a journal from
  before the field existed). Report version "18" carries the three
  ([`projections.md`](projections.md)).

The cache mirrors the input-free report and therefore never carries
this section, exactly as it never carries the observation section.

## Deliberately absent (v0)

No rotation, no thresholds, no trend windows, no alarms, no
promotion into packets: those are the curator's and later phases'
work. v0 is counts, a span, and one honest rate.
