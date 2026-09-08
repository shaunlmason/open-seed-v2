# Checkpoints: what a checkpoint attests, whom a reader trusts, and the proof

> Charter: SEED-NEXT.md §II.1 (checkpoints), Part III.A row 8 (the
> trust half: "a fresh clone's verification obligations (full replay
> once, or explicit trust in the signer set) are documented and the
> choice is declared, not defaulted; replay-from-checkpoint equals
> replay-from-genesis in CI") and row 12 (performance budgeted and
> tracked in CI), III.C row 4 (the contention benchmark);
> [`plans/os-7508ab9e.md`](../plans/os-7508ab9e.md). The mechanism
> — the snapshot, its format, the maintenance pass that writes it — is
> [`maintenance.md`](maintenance.md)'s ("Checkpoints carry a snapshot a
> reader can start from") and is referenced here, not restated.

## What a checkpoint attests

A `system.checkpoint` record, signed by a key holding `maintenance` or
`operator` (or a governance root; the row in [`actors.md`](actors.md)),
carries `{format, snapshot, location, position}`. The snapshot it names
is the published projection state at `position` under the versioned
format `seed.projection.v1`, together with the chain hash at that
position (`tip`). So a checkpoint attests exactly three things: that
the chain's first `position` records hash to `tip`; that the registered
projections derive the snapshot's files from those records; and that
the signer says so. It attests nothing about records after `position`.

**The hash does not cover the signature.** `Event.Hash` is SHA-256 over
the canonical event bytes, and a record's `sig` rides beside them. A
record whose signature never verified — or was altered after the fact —
has the same hash as an honest one, so an attested tip cannot tell them
apart. That is the whole content of trusting a signer set: the reader
trusts that admission verified what it admitted before the checkpoint
was cut. Under an enforced posture that is the boundary's promise;
under cooperative it is nobody's, and a `signers` declaration there
trusts every writer who ever pushed.

## The declaration, and why undeclared is not a default

`seed.json` carries `"checkpoints": {"trust": "replay" | "signers"}`.

| declaration | a fresh reader … | what it verifies | what it takes on trust |
|---|---|---|---|
| `replay` | verifies from genesis once (`seed project rebuild`) before it serves anything | every record: parse, linkage, hash, version discipline, keyring, signature | nothing but the chain |
| `signers` | may start from the newest checkpoint whose signer was capable at its own position (`seed project start`) | every record's parse, linkage, hash, version discipline and keyring fold; the suffix's signatures in full; the attested tip at the trusted position; the snapshot's files against its own derivation | the prefix's signatures, on the checkpoint signer's word |
| absent | refuses to start from a checkpoint (`trust_undeclared`, exit 4) and reports the choice as unmade | — | — |

An absent block is **undeclared**, never `replay` by default: the row
says the choice is declared, not defaulted, and a reader that quietly
replayed would be making the deployment's choice for it. The doctor
reports `checkpoints.trust` as declared, or `undeclared: true`.

## The reader: `seed project start`

Under `signers`, `seed project start --ledger <dir> --artifacts <dir>
--out <root> --config seed.json`:

1. parses every record — parsing is not verification — and folds the
   keyring to find the newest `system.checkpoint` whose payload parses
   and whose signer held `maintenance` or `operator`, or was a root, at
   the checkpoint's own position (`checkpoint.Latest`); a chain with
   none refuses `not_found` and names the rebuild;
2. fetches the snapshot from the artifact store and verifies its digest
   against the signed hash; reads it under its declared format; holds
   its position to the checkpoint's;
3. **cross-checks before publishing anything**: every projection file
   in the snapshot must equal the reader's own derivation at the cited
   position, or the checkpoint attests a state the chain does not
   support — `checkpoint_mismatch`, exit 21 (the recomputation family),
   naming the file. This costs one fold and turns a lying checkpoint
   from an invisible failure into a named one;
4. replays with the trusted prefix (`ledger.WithTrustedPrefix`): every
   record parsed, linked, hashed, held to the version discipline and
   folded into the keyring; signatures verified for records at or after
   the trusted position only; the chain hash at the trusted position
   held to the attested tip (`trusted_prefix_mismatch`, a `chain_invalid`
   reason, refuses before anything is written);
5. publishes every registered projection at the tip, byte-identical to
   a genesis replay's, and writes `<out>/basis.json`: `{trust,
   checkpoint, position, tip, signer, trusted, verified}`.

The basis lives in the output root rather than in every build's stamp
so that a build from a checkpoint and a build from genesis stay
byte-identical — which is the proof the reader exists to pass — while a
consumer can still see what the builds rest on. A full `seed project
rebuild` into the same root removes it: a replay rests on nothing but
the chain.

## The proof, with teeth

`TestStartFromCheckpointEqualsGenesis` (`internal/project`) builds the
representative history, checkpoints it, appends more, and publishes
from the checkpoint into one root and from genesis into another: every
published file byte-identical, the basis saying what was trusted and
what was verified. `TestStartFromCheckpointTrustsExactlyThePrefixSignatures`
is the part that makes the declaration mean something: a prefix
record's signature corrupted after the checkpoint (same hash, same
chain) is caught by the genesis replay and NOT by the checkpoint start,
while a suffix record corrupted the same way is caught by both. A proof
that hid the trade-off would be the wrong proof.
`TestStartFromCheckpointRefusesALyingCheckpoint` refuses a snapshot that
is not the derivation, an incapable signer's checkpoint, and an attested
tip the chain does not reach, publishing nothing from any of them.

## The staleness bound

A fresh clone is at best as fresh as the newest checkpoint or head
attestation it can reach (III.A row 5): a rollback to a valid earlier
tip is a freshness problem, not a chain-detectable one, and what bounds
it is the newest position some out-of-band witness — a checkpoint, a
verdict's head attestation, another clone's persisted head — can hold
the chain to. `seed project start` records the position it trusted up
to in the basis, which is the reader's own contribution to that bound.

## Performance budgets

III.A row 12 asks that admission latency, replay time and projection
rebuild time be budgeted and tracked in CI against a representative
history; III.C row 4 asks for a contention benchmark at scale. Both are
`perf/budgets.json` and the gate `make check-next` runs after
coverage (`cmd/perfgate`, `internal/perfgate`):

| metric | what is measured |
|---|---|
| `admission_ms` | one `admit.Check` of a fresh draft at the tip of the history, averaged over twenty |
| `replay_ms` | `VerifyFromGenesis` over the history |
| `rebuild_ms` | every registered projection published from the history |
| `contention_ms` | wall time of `writers` concurrent appenders each landing one append through the optimistic loop against a hook-enforced bare remote seeded with the history, with no lost or doubled update asserted |
| `attempts_ratio` | the storm's attempts per landed append |

The history is `internal/history`: a seeded, deterministic chain of
`history` contracts through the full loop — filed, specified, claimed,
reserved, run and settled, submitted, passed, merge requested and
observed — that the drills and the benchmarks share, admissible by
construction and held to the boundary by its own drill. Every ceiling
in the budget file carries its provenance (when it was set, against
what size, with what headroom); the loader refuses one without. The
gate measures once, and on a miss cleans up and measures again before
failing, the coverage gate's rule for the same reason: one noisy
reading must not fail a healthy tree, and a real regression fails
twice. A change that moves a number past its ceiling either fixes the
regression or raises the ceiling in the same PR with the reason in the
file. The last reading is written to `perf/last.json`
(gitignored) and `seed perf run` prints it as an envelope.

**The scale profile.** The same gate runs at two sizes
(plans/os-a00d3f34.md D1). `perf/budgets-scale.json` has the
shape of `budgets.json` with the same representative history and 200
writers, the smallest count that is hundreds; every writer is its own
enrolled actor (the history enrolls one agent key per writer with the
grant `intent.filed` accepts, and the storm signs each writer's append
with its key, holding the landed chain to as many distinct actors as
writers), since the charter's row counts actors and an actor is a
keypair. `.github/workflows/perf-scale.yml`
runs `cmd/perfgate -budgets perf/budgets-scale.json` weekly (and on
dispatch), read-only, attaching `perf/last-scale.json` as an artifact
whether the gate passed or not (a ceiling miss carries its reading; a
storm that loses a writer has no reading to attach, and its red is the
signal); a miss fails the run. The per-PR gate stays at 24 writers, because the
optimistic loop's cost is quadratic in writers (attempts per landed
append is writers/2) and a storm at hundreds costs tens of minutes a
run. `TestScaleProfileIsHundredsOfWriters` (`internal/perfgate`) holds
the profile at 200 writers or more, at least the per-PR history, and
an attempts ceiling of at least writers/2; `TestTreeWorkflowsHaveNoScheduledWriters`
(`internal/protections`) holds every scheduled workflow in the tree
but v1's maintenance lane (whose write is the state ref's anchor) to
`contents: read`.

The attempts ratio is stated for what it is: under one optimistic
ref, `n` writers racing each cost about `n/2` attempts, so the storm's
total work grows with the square of the writer count. That is the
single-ref design's honest shape, and it is exactly what the charter's
row rules out: III.C row 4 asks for hundreds of concurrent actors
"without unrelated writes racing each other's admissions
pathologically", and two hundred unrelated `intent.filed` appends
racing one ref is that pathology held to a ceiling, not its absence.
So the scheduled run demonstrates the scale (two hundred enrolled
actors, every append landed, no lost or doubled update) tracked in
CI, and bounds the pathology; it does not demonstrate the row's
clause, which sharded intake (III.B's MAY, the backlog's os-7953612b)
would, and the conformance table's C.4 row stays `partial` with this
run cited for what it shows. The budget holds the ratio to the
quadratic expectation rather than pretending it is constant.

**The seventh race shape, and `relinked`.** The optimistic loop retries
six push rejections as the races they are (a stale parent, receive
contention, a moved ref lock) and surfaces every other rejection as
a refusal. A hook refusing the pushed chain as `bad_prev` at or beyond
the position the client appended is the seventh (plans/os-5063e8ba.md
D1): the pushed tree cites a tip the remote does not hold there,
which is either a tip that moved in a way the loop did not see or a
tree the client built wrong, and re-linking from a fresh fetch is the
answer to both. Before the retry the client keeps the refused tree
and the hook's message under its state dir (`refused/<commit>/`), and
the storm reports the count as `relinked` in its reading, a number
beside the five budgeted metrics and never a budget: a clean storm
re-links zero times, and a run that re-links is a run whose refused
trees are worth reading. `--keep <dir>` on `cmd/perfgate` and `seed
perf run` keeps the storm's work dir (the remote, every writer's
state dir, the rebuild's projections, which stay locked read-only as
every published build is) for that reading. The 200-writer storm that found the
shape (one lost append across midnight UTC, os-5063e8ba) is the
reason both exist.

## Residuals, stated

- The reader folds every record and skips only the prefix's signature
  checks: there is no incremental fold. What a `signers` deployment
  saves is the signature work and the trust question, not the fold.
- The contention number is the hook posture's; the service posture's
  storm is a later addition to the same gate.
- The history is one shape at one size; a deployment's real history
  has other shapes, and the budget says which shape its ceilings were
  set against.

## Conformance mapping

- III.A row 8 (checkpoints signed, embedding the state hash,
  referencing a retrievable snapshot; the materialization format
  specified and versioned; a fresh clone's obligations documented and
  the choice declared, not defaulted; replay-from-checkpoint equals
  replay-from-genesis in CI) — `maintenance.md` for the first half;
  this document, the declaration, `seed project start` and the three
  drills for the second.
- III.A row 12 (ledger performance budgeted and tracked in CI:
  admission latency, replay time, projection rebuild time, against a
  representative history) — `perf/budgets.json`, `cmd/perfgate`
  under `make check-next`, `internal/history`.
- III.C row 4 (a contention benchmark at scale, tracked in CI) — the
  storm metrics, with the single-ref shape stated, at 24 writers on
  every `make check-next` and at the scale profile's 200 writers on
  the weekly `perf-scale` workflow (plans/os-a00d3f34.md).
