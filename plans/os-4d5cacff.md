# Plan: next Phase 4.1 — the projection engine (os-4d5cacff)

Implements [`docs/next-build-plan.md`](../docs/next-build-plan.md) Phase 4
item 1: the projection engine — deterministic build from a chain prefix,
a position stamp on every projection, and a one-command rebuild
(`seed project rebuild`). Design authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
Part II "Projections" — the five normative properties (derived, stamped,
rebuildable, read-only outward, non-authoritative) — and conformance
III.D's core rows: "every read surface is a deterministic function of a
ledger prefix … stamped with its build position, and rebuildable
byte-identically with one command". Deps: Phase 1 (merged); the engine
consumes Phase 3's keyring projection for its first real view. The
standard projections (4.2), the SQLite cache (4.3), and the
write-boundary lint (4.4) build on this seam.

## Steps

1. **The engine.** `next/internal/project`: a registry of named
   projections, each a pure function from the verified record prefix to
   a set of output files. One build path: open the ledger with
   `OpenReadOnly` (the engine must be incapable of the healing writes
   the ordinary open performs), verify from genesis (a verification
   failure is the build's failure, before anything is written), collect
   the records once, run every registered projection, and publish each
   tree **deterministically and atomically**. `os.Rename` cannot
   replace a non-empty directory, so publication is **immutable build
   directories plus an atomically replaced pointer** (review finding on
   #105): each build lands complete under
   `<out>/<name>/builds/<position>-<tip12>/` (the id derived from the
   stamp, so identical prefixes produce identical ids and trees), and a
   `CURRENT` file naming the active build is atomically renamed into
   place only after the tree is complete; superseded builds are pruned
   after the swap. A reader always resolves `CURRENT` to a complete
   tree; a killed build leaves at worst an orphan build directory,
   never a broken view. Every build tree carries a stamp file
   (`projection.json`: name, `position`, `tip`) — the staleness surface
   III.D requires. Deleting a projection's directory loses nothing: the
   next rebuild recreates it byte-identically, `CURRENT` included.
2. **The roster projection**, the engine's first consumer: the actor
   roster derived from Phase 3's keyring state (`keyring.StateAt`) —
   **every keyring entry, genesis governance roots included** (they are
   seeded directly from the genesis payload, so they carry `root: true`
   with empty kind and name, documented as genesis-seeded; a freshly
   initialized ledger therefore yields a non-empty roster), alongside
   every enrolled actor with kind, name, standing, root flag, and
   accumulated grants, serialized with sorted keys (review finding on
   #105). It exercises the engine against
   real chain semantics (enrollment, grants, suspension, revocation,
   rotation) and gives later phases (roster-reading metrics, Phase 9
   maintenance) their read surface. The standard work projections are
   4.2's; this task ships exactly one real view to prove the seam.
3. **The rebuild verb.** `seed project rebuild --ledger <dir>
   [--out <dir>]` (default `projections` in the working directory):
   verifies, builds every registered projection, and reports
   `{projections: [{name, position, tip}], out}` in the envelope. Both
   paths are canonicalized (absolute, symlinks resolved) before
   anything is created, and **any overlap between the ledger directory
   and the output root refuses as a usage error** — in either
   direction, so neither `--out` inside the ledger nor the ledger
   inside `--out` can ever make a projection target coincide with
   authoritative state (review finding on #105). A chain that fails
   verification refuses with the established chain-refusal shape
   before anything is written; nothing in the engine ever writes the
   ledger (read-only outward, asserted by test).
4. **Spec.** `next/spec/projections.md`: the five normative properties,
   the stamp schema, the engine contract (verified-prefix input,
   deterministic output, atomic replace), the rebuild command, and the
   conformance mapping — which III.D rows this engine satisfies
   (deterministic + stamped + one-command byte-identical rebuild) and
   which land later (write-boundary lint 4.4, cache 4.3, mirrors and
   observation inputs with their phases).
5. **Drills** (library + CLI): byte-identical rebuild — build, hash the
   output tree (`CURRENT` and build dirs included), delete it, rebuild,
   identical hashes; growing the chain changes the stamp, the build id,
   and the tree; two consecutive builds over the same prefix are
   byte-identical (map-order determinism); **interrupted publication**
   — with a pre-seeded partial build directory and an older `CURRENT`,
   a rebuild leaves readers a complete view at every point and lands
   the new pointer only with a complete tree; the stamp's position and
   tip equal the verification report's; the roster is correct across an
   enroll/grant/suspend/reinstate/revoke/rotation chain (Phase 3
   fixtures) **and non-empty on a root-only, freshly initialized
   ledger** (genesis roots present, `root: true`, empty kind); an
   unverifiable chain — a stale-HEAD fixture included — refuses with
   nothing written; **ledger immutability is asserted over the complete
   ledger tree byte-for-byte** before and after builds (tip-only
   comparison would miss a healing write to `HEAD`, hence
   `OpenReadOnly`); ledger/output overlap refuses in both directions
   before anything is created; CLI end-to-end incl. the refusal exits.

## File Scope

- `next/internal/project/**` (new package)
- `next/cmd/seed/**` (the `project rebuild` verb + tests)
- `next/spec/projections.md` (new)
- `next/docs/decisions.md`, `next/docs/progress.md`,
  `memory/LEARNINGS.md` (if lessons emerge)

## Acceptance Criteria

**Boundary set (new, shown working):**

- One command rebuilds every registered projection from genesis;
  deleting the output first changes nothing — the rebuilt tree is
  byte-identical (hashes compared), and repeat builds over the same
  prefix are byte-identical too.
- Every projection tree carries the stamp with the exact verified
  position and tip it was built at, equal to the verification report's.
- The roster projection reflects keyring standing correctly across the
  full Phase 3 lifecycle fixtures, and a root-only ledger yields the
  genesis roots (never an empty roster).
- Publication survives interruption: readers resolve `CURRENT` to a
  complete tree at every point, and a repeat rebuild over an existing
  output succeeds (no directory-rename limitation, no delete window).
- A chain that fails verification (stale-HEAD fixture included) refuses
  before anything is written; the complete ledger tree is byte-for-byte
  untouched by builds; and a ledger/output path overlap refuses in
  both directions before anything is created.

**Retention set (existing, shown unharmed):**

- All existing verb suites pass unchanged
  (`cd next && go test ./internal/... ./cmd/... -count=1`).
- The repo-wide gate stays green with the ≥90% coverage gate
  (`make check`).

## Validation Commands

- `make check-next`
- `make check`
- `cd next && go test ./internal/project/... -count=1`
- `cd next && go test ./internal/... ./cmd/... -count=1`
