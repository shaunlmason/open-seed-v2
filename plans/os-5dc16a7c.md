# Plan: next Phase 5.2 — claims with fences (os-5dc16a7c)

Implements [`docs/next-build-plan.md`](../docs/next-build-plan.md) Phase 5
item 2: "Claims with fences; online-only claiming; stale-fence refusal;
contention envelope." Design authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
Part II §6 "Claims and the offline boundary" (`claim.taken` carries a
fence; every subsequent event on that contract from that claimant cites
it; stale fences are refused at admission; claiming is **online-only**:
exclusivity is a property granted at admission), §5 "The admission
boundary" (the normative check order runs capability → **fence** →
transition), and conformance III.F: "Claims are exclusive with fencing,
granted only at admission (online); stale fences refuse with distinct
codes; contention returns structured envelopes" plus "Racing mode
exists only as an explicit per-squad opt-in …; offline 'claiming' is
impossible by construction." Deps: 5.1 (plan #113; the transition
table and lifecycle vocabulary this builds on); implementation stacks
on 5.1's branch once it exists, after the Phase 4 stack merges.

## Design decisions (binding for this task)

- **The fence is derived, never asserted**: it is the chain position
  (zero-based, the CLI-wide index convention) of the **active**
  `claim.taken` record, established by admission when the claim
  admits — ordering authority is admitted ancestry, so a client
  cannot mint a fence, only cite one. Subsequent events cite it as a
  payload field `{"fence": "<position>"}` (string, matching the
  envelope position convention).
- **Who must cite it** (the fence rule, slotted between `grant` and
  `lifecycle` per the charter's order): on a subject whose folded
  state is `in_progress` — (a) every **lifecycle** event (the four
  deliberate exits, and `claim.reaped` from any authorized signer,
  which must name the claim it kills); (b) every **free-stream**
  event signed by the current holder **or by any prior claimant of
  this subject** ("every subsequent event on that contract from that
  claimant cites it" binds the claimant whose claim it was, not only
  whoever holds now — a reaped or released worker's delayed
  `progress.milestone` must not slip through as an observation and
  contaminate progress or later expiry/wedge math; review finding on
  #114). The fold therefore tracks the subject's claimant history,
  and any citation present must match the **active** fence, whoever
  signs. Free events from signers who have never claimed this
  subject are genuine observations and pass without a fence; a prior
  claimant citing the active fence is explicitly acknowledging the
  current claim, which is exactly the assertion a late event needs
  to make. A missing-but-required or stale citation refuses **exit 6
  `fenced_out`** (the existing v1-continuity row: "stale or missing
  fence (claim token)"), naming the cited fence, the active fence,
  and the holder. Outside `in_progress` there is no active fence:
  free events carry no citation and none is required — claim-scoped
  contamination is impossible because nothing outside a claim window
  carries a fence (stated in the spec).
- **Contention is its own refusal, not a generic illegal
  transition**: `claim.taken` on an `in_progress` subject refuses
  **exit 2 `contention`** (existing row: "exclusivity not granted")
  with a structured envelope naming the holding fingerprint, the
  active fence position, and the position the claim was taken at —
  the loser learns who holds and since when. All other illegal
  lifecycle jumps stay exit 3 per 5.1.
- **Online-only claiming is data plus a client seam**: the transition
  table row for `claim.taken` gains `"exclusive": true` (schema
  extension validated by the 5.1 self-validation: an exclusive verb
  must be a table verb). The cooperative client (`internal/gitref`)
  refuses to draft or locally append an exclusive verb outside the
  push round-trip: exclusivity verbs refuse without a live remote,
  with a message quoting the charter's posture ("two offline actors
  claiming the same contract have not claimed anything"). Reading,
  planning, and continuing an admitted claim stay fully offline.
- **Racing mode is deferred entirely** — the build plan's binding
  defaults table says so ("Deferred entirely to post-plan backlog");
  the spec states the deferral and the offline-impossibility posture
  in plain words. `wedge.declared` stays with 5.6 (expiry-vs-wedge in
  the report), named as an extension point.
- **Visibility**: the contracts view gains a `claim` object on
  `in_progress` entries — `{holder, fence}` from the fold (contracts
  `Version: "3"`), so contention answers and stale-fence refusals are
  independently checkable against the published view.
- **No new exit codes**: 2 and 6 exist with exactly matching
  semantics (v1-continuity default); their `envelope.md` prose gains
  the claim-specific detail.

## Steps

1. **Table schema** (`next/spec/transitions.json` +
   `internal/transition`): the optional `exclusive` row flag;
   self-validation extends (exclusive rows must be verbs of the
   table; `claim.taken` is marked). The fold gains the claim facts:
   `StateAt` returns (state, anomalies, claim, priorClaimants) where
   claim is `{Holder, Fence}` for `in_progress` subjects (the
   position of the active `claim.taken` and its signer), cleared by
   every exit, and priorClaimants is the set of fingerprints that
   have ever held a claim on this subject (the fence rule's
   who-must-cite input; review finding on #114).
2. **The fence rule** (`internal/admit`, between `grant` and
   `lifecycle`): derive the subject's claim from the fold; enforce
   the citation matrix above; typed `FenceError{Subject, Cited,
   Active, Holder}` → exit 6. The contention special case in the
   lifecycle rule: `claim.taken` on `in_progress` → typed
   `ContentionError{Subject, Holder, Fence}` → exit 2. Hook and CLI
   inherit through the shared rule set; envelope mapping adds the two
   typed cases.
3. **Online-only** (`internal/gitref` + CLI): the append loop refuses
   exclusive verbs in any local/offline mode (the dev-tool local
   append included) with a distinct refusal; the push round-trip is
   the only path that can admit them. The refusal is client-side
   posture (the boundary enforces exclusivity regardless; the client
   rule prevents drafting doomed work), stated as such in the spec.
4. **Projections** (`internal/project`): contracts entries gain
   `claim: {holder, fence}` while `in_progress` (Version "3");
   absent otherwise. Queue and actor views unchanged.
5. **Spec**: `lifecycle.md` gains the "Claims and fences" section
   (fence derivation, citation matrix, contention semantics, the
   online-only boundary and its client seam, racing-mode deferral,
   wedge extension point); `envelope.md` rows 2 and 6 gain the
   claim-specific prose; `projections.md` updates the contracts
   schema.
6. **Drills**:
   - *Fence lifecycle*: claim → cite → exit; a second claim after
     release gets a **new** fence and the old one refuses stale;
     reap cites the fence it kills; a missing fence on the holder's
     milestone refuses 6; a never-claimed signer's free observation
     passes without a fence.
   - *Prior claimants stay fenced* (review finding on #114): A claims
     and is reaped, B claims; A's delayed milestone citing A's old
     fence refuses 6 stale, and A's milestone with no citation
     refuses 6 missing (a prior claimant cannot demote itself to
     observer); the same event citing B's active fence admits; after
     B releases (no active claim), A's fence-free milestone passes as
     a plain observation, pinning the claim-scoped boundary.
   - *Contention*: `claim.taken` on a held contract refuses 2 with
     holder, fence, and since-position in the envelope, at the rule
     set, the hook, and the CLI.
   - *Claim race storm* (the Phase 5 exit drill, extending the 1.4
     race harness): N concurrent claimants against a real remote;
     exactly one claim admits, every loser receives a structured
     contention envelope, the chain verifies from genesis, and no
     update is lost.
   - *Offline boundary* (the Phase 5 exit criterion): exclusive verbs
     refuse without a remote through the cooperative client; reading
     and continuing an admitted claim offline still work.
   - *Tolerant fold*: raw-pushed history with a fence violation
     verifies; the anomaly surfaces in the contracts view; the fold's
     claim facts stay coherent.
   - *Projection*: the contracts `claim` object matches the fold
     fixture-by-fixture and disappears on exit; the version bump
     republishes at an unchanged tip.

## File Scope

- `next/spec/transitions.json`, `next/spec/lifecycle.md`,
  `next/spec/envelope.md`, `next/spec/projections.md` (extend)
- `next/internal/transition/**` (exclusive flag, claim fold)
- `next/internal/admit/**` (fence rule, contention case)
- `next/internal/gitref/**` (online-only refusal)
- `next/internal/envelope/**` (typed-error mapping only; no new codes)
- `next/internal/project/**` (contracts v3)
- `next/cmd/seed/**`, `next/cmd/seed-admit/**` (drills, envelope cases)
- `next/docs/decisions.md`, `next/docs/progress.md`,
  `memory/LEARNINGS.md` (if lessons emerge)

## Acceptance Criteria

**Boundary set (new, shown working):**

- A claim's fence is the admitted `claim.taken` position; the holder's
  subsequent events and every deliberate exit must cite it; missing or
  stale citations refuse exit 6 naming cited, active, and holder;
  non-holder observations pass.
- `claim.taken` on a held contract refuses exit 2 with a structured
  envelope naming holder, fence, and since-position — at the rule set,
  the seed-admit hook, and the CLI.
- The race storm admits exactly one of N concurrent claims against a
  real remote, losers all receive structured contention, the chain
  verifies from genesis, and re-claim after release mints a new fence
  that retires the old one.
- Exclusive verbs refuse through the cooperative client without a
  live remote; offline reading and continuation of an admitted claim
  are unaffected.
- The contracts view shows `{holder, fence}` exactly while a subject
  is `in_progress`, republished under a new version-bearing build id
  at an unchanged tip.

**Retention set (existing, shown unharmed):**

- All existing verb suites pass unchanged
  (`cd next && go test ./internal/... ./cmd/... -count=1`).
- The repo-wide gate stays green with the ≥90% coverage gate
  (`make check`).

## Validation Commands

- `make check-next`
- `make check`
- `cd next && go test ./internal/transition/... ./internal/admit/... ./internal/gitref/... -count=1`
- `cd next && go test ./internal/... ./cmd/... -count=1`
