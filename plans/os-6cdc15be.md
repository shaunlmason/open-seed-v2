# Plan: next Phase 6.2 — the reconciliation chain and divergence detection (os-6cdc15be)

Design authority: [`SEED-NEXT.md`](../SEED-NEXT.md) Part II §8 ("done is a
verdict — and a reconciliation": the verdict and the merge live in two
systems that share no transaction, so done is an explicit reconciliation,
never a pretended atomic step) and conformance III.G rows 1–2 (done only
through `verdict.rendered(pass) → merge.requested → merge.observed →
done`, each step its own event, no code path collapsing them; divergence
is a detected, surfaced, reconciled state, drilled by inducing each);
[`docs/next-build-plan.md`](../docs/next-build-plan.md) Phase 6 item 2.
6.1 (merged #135) left two named seams this card closes: surfacing
verdicts in projections, and the submit-old-head/merge-new-tip induced
drill comparing `merge.observed`'s forge fact against the verdict's
attested head. Red-verdict **lockout** (the implementer's approval-path
consequence) stays 6.4; sealed checks stay 6.3; the unattended
maintenance loop that will run these detections on a schedule is Phase 9
item 3 — this card ships detection as an invocable surface that loop
later drives.

## Design decisions (binding for this task)

1. **`merge.requested` is piped.** Strict payload
   `{"verdict": "<position>"}` citing the chain position of an admitted
   **pass** `verdict.rendered` on the same subject; admits only while
   the subject is in `review`; a fact, not a transition. Citing a fail
   verdict, a verdict on another subject, or a position that is not a
   verdict refuses — the chain-legality half of "a red verdict is
   unmergeable" lands here; the implementer *lockout* half is 6.4's.
   Capability: `claim` or `operator` (asking for a merge is the work
   lane's act; review carries no fence, so no citation applies).
2. **`merge.observed` deepens to the observer's forge fact.** Strict
   payload `{"merged": "<full sha>", "pr": "<ref>"}` (both
   presence-checked; `merged` is 40–64 lowercase hex — the fact worth
   recording is *which commit* the forge merged, the comparison anchor
   for divergence). The chain rule at admission: the subject carries an
   admitted pass verdict AND an admitted `merge.requested` citing it,
   in that order — done is reachable only through the full chain, and
   no code path collapses the steps (III.G row 1). Raw-pushed
   violations stay tolerated and anomaly-counted (the established
   posture), which is exactly what makes divergence detection
   necessary. Capability: the promised **observer lane** —
   `keyring.CapObserver` (`"observer"`), accepted set
   `[observer, operator]`, replacing the operator-only row actors.md
   marked "Phase 6 adds the observer lane".
3. **The fold records the chain facts** per subject: the latest
   verdict `{Pos, Verdict, Receipt, Signer}` (any verdict, pass or
   fail — 6.4's lockout will consult it), the latest
   `merge.requested` position and its cited verdict, and **the**
   admitted `merge.observed` `{Pos, SHA}` — singular by construction:
   the first valid observation moves the subject to terminal `done`,
   so a second can never admit, and a raw-pushed second stays an
   anomaly like any other illegal transition (review finding on this
   plan: a two-observation list is unreachable and must not be the
   rewrite signal).
4. **Divergence detection is a pure derivation split across two
   surfaces by what each may read.** `internal/reconcile` (new)
   classifies per subject from the fold alone:
   `merge_without_verdict` (done or an observed merge with no admitted
   pass verdict), `chain_skipped` (an observed merge with no admitted
   request citing the verdict), and `unreconciled` (a pass verdict
   with no observed merge yet — reported neutrally, never an
   accusation: with no wall clock in any build, "failed" versus
   "pending" is an age judgment that belongs to Phase 9's maintenance
   thresholds). The **evidence-grade** checks live only in the CLI
   (`seed reconcile`), which may read the artifact store and the
   repository — the two things builds never touch:
   - **Attested-head reconciliation.** Load the cited receipt exactly
     as `verdict check` does (`evidence_missing` when the store
     cannot produce it intact); the clean cases are observed sha ==
     attested head (fast-forward) or attested head an ancestor of the
     observed sha (a true merge commit). Anything else surfaces
     `attested_divergence` naming both shas and the receipt digest —
     deliberately a *surfaced state, not a fabrication verdict*:
     rebase and squash flows land here by design in v0 because the
     forge mints a new sha, and patch-equivalence reconciliation
     (comparing the merged change against the receipt's diff hash) is
     the named successor, recorded in the decision log.
   - **Target-rewrite detection** (the charter's force-push case,
     review finding on this plan: a forge force-push produces no
     ledger event, so detection must observe the target ref).
     Reconcile resolves the target as the repository's checked-out
     default branch tip (v0; a declared target ref can ride a later
     phase) and reports `target_rewritten` when the observed merged
     sha no longer resolves or is no longer an ancestor of that tip —
     drilled by landing the full chain, then force-moving the target
     past a rewritten history.
   Projection builds never read the artifact store or the repository:
   builds stay deterministic over records (plus 5.6's declared obs
   inputs), so the report carries the three record-derivable classes
   and names `seed reconcile` for the evidence-grade rest.
5. **Projections finally surface the pipeline** (6.1's named
   deferral). Contracts `Version: "6"`: each entry gains
   `verdict: {position, verdict, receipt} | null`,
   `requested: <position> | null`, and `merged: {position, sha} | null`
   (latest observation; the full list stays fold-internal). Report
   `Version: "3"`: a `reconciliation` section listing subjects with
   record-derivable divergences by class, plus counts. Cache schema
   generation 5 with the matching columns, `cacheVersion "6"`. Queue
   untouched. Every change republishes under new version-bearing build
   ids at an unchanged tip, the established discipline.
6. **`seed reconcile`** (CLI): `--ledger --repo [--subject]
   [--artifacts]` walks the fold (one subject or all), merges the
   record-derivable classes with the evidence-grade checks, and
   returns the divergence set as envelope data at exit 0 — detection
   is a report, not a refusal; refusals stay operational (usage,
   unreadable store, chain trouble). Drills assert on the envelope
   content; Phase 9's loop consumes the same output.
7. **Spec.** New `next/spec/reconciliation.md` (normative: both piped
   payloads, the chain rule, the divergence taxonomy, the
   record/evidence split, conformance mapping to III.G rows 1–2);
   `actors.md` rows for `merge.requested` and the observer lane;
   `lifecycle.md` updated where it calls `merge.requested` not yet
   piped; `verdicts.md` cross-referenced where it promised the
   divergence work to 6.2.

## Steps

1. **Spec first.** Write `next/spec/reconciliation.md`; update
   `actors.md` (the two rows, CapObserver vocabulary), `lifecycle.md`
   (the chain is fully piped; done still only via `merge.observed`),
   and `verdicts.md`'s cross-references.
2. **Keyring.** `CapObserver` plus the two accepted-capability rows;
   the spec-parsing test keeps code and table pinned.
3. **Fold + chain rule.** `internal/transition`: the chain facts of
   decision 3 and typed refusals (`ChainError` naming the missing or
   mismatched link, riding the established shape mapping);
   `internal/admit`: extend the verdict rule's family with the
   `merge.requested` and `merge.observed` checks (state gate, strict
   shapes, pass-verdict citation, request-before-observation).
   Drills: request citing a pass verdict admits; citing a fail
   verdict, a non-verdict position, another subject's verdict, or
   nothing refuses; observation with the full chain admits and folds
   to done; observation without verdict or without request refuses;
   both verbs refuse outside review; capability lanes (observer
   admits observation but not request; claim admits request but not
   observation; operator both; a plain enrolled key neither); fence
   citations refuse (review carries no fence); raw-pushed skipped
   links fold tolerated with anomalies counted.
4. **Reconcile core.** `internal/reconcile`: the per-subject
   classifier over fold facts; unit drills induce each
   record-derivable class from library-built histories, including
   the pinned neutrality of `unreconciled`.
5. **Projections.** Contracts v6, report v3 with the reconciliation
   section, cache generation 5 / version 6; the established
   republish-at-unchanged-tip and byte-identity drills extended to
   the new fields; the report's input-free byte-identity stays intact
   (reconciliation derives from records only).
6. **CLI.** `seed reconcile` per decision 6, plus the end-to-end
   drills: the full-chain path (submission → pass verdict →
   request → observation → done; reconcile clean); the
   submit-old-head/merge-new-tip induced drill (verdict attests head
   H1, observation records H2 outside the clean ancestry cases —
   reconcile surfaces `attested_divergence` with both shas); the
   target-rewrite drill (full chain to done, then the target ref
   force-moved past a rewritten history — reconcile names
   `target_rewritten`); a fast-forward and a true merge commit both
   reconcile clean; a deleted artifact yields `evidence_missing`,
   never silence; a raw-pushed merge-without-verdict history
   surfaces `merge_without_verdict` in both reconcile and the
   report.
7. **Docs.** progress.md ledger row and frontier; decisions.md
   entries (observer lane, payload shapes, the record/evidence
   split, neutrality of unreconciled); LEARNINGS/DEADENDS as earned.

## File Scope

- `next/spec/reconciliation.md` (new), `next/spec/actors.md`,
  `next/spec/lifecycle.md`, `next/spec/verdicts.md`
- `next/internal/reconcile/**` (new), `next/internal/transition/**`,
  `next/internal/admit/**`, `next/internal/keyring/**`
- `next/internal/project/**`
- `next/cmd/seed/**`
- `next/docs/progress.md`, `next/docs/decisions.md`,
  `memory/LEARNINGS.md`, `memory/DEADENDS.md` (as needed)

## Acceptance Criteria

**Boundary set (new, shown working):**

- The chain is enforced at admission: `merge.requested` admits only in
  review citing an admitted pass verdict (fail, non-verdict, wrong
  subject, and absent citations each refuse by name);
  `merge.observed` admits only with the pass verdict and the request
  in place, folds the subject to done, and records the merged sha;
  the capability lanes hold (observer/claim/operator as bound, plain
  standing refuses 14).
- Every record-derivable divergence class is induced and detected in
  both `internal/reconcile` and the report's reconciliation section:
  `merge_without_verdict`, `chain_skipped`, `unreconciled` (reported
  neutrally).
- The evidence-grade drills go end to end: the clean ancestry cases
  (fast-forward, true merge commit) reconcile clean; an observation
  outside them surfaces `attested_divergence` with both shas; a
  force-moved target ref yields `target_rewritten`; a lost or
  corrupted stored receipt yields `evidence_missing`, never a silent
  pass.
- Contracts v6 carries the verdict/requested/merged fields, report v3
  the reconciliation section, cache generation 5 the matching
  columns; derivation changes republish under new build ids at an
  unchanged tip, and input-free builds stay byte-identical.
- The full-chain CLI drill lands on done with a clean reconcile.

**Retention set (existing, shown unharmed):**

- Every pre-existing suite is green: `make check` (which runs
  `check-next`) passes at the coverage gate; no existing test is
  weakened, skipped, or deleted.
- 6.1's verdict surfaces are unchanged: receipt/render/check behavior,
  exits 17–21, and the verdict admission matrix all hold as landed.
- No v1 surface changes; the task PR never touches `plans/**`.

## Validation Commands

- Boundary: `cd next && go test ./internal/reconcile/... ./internal/transition/... ./internal/admit/... ./internal/keyring/... ./internal/project/... ./cmd/seed/... -count=1`
- Retention: `cd next && go test ./... -count=1` and `make check`
  (exit checked separately from any pipe).

## Expected diff shape

One new package (reconcile) and one new spec file with drills; fold
facts and two piped verbs in transition/admit with typed refusals; one
capability and two rows in keyring/actors; contracts/report/cache
version bumps with their republish drills; one CLI verb; docs. No new
exit codes; no deletions of existing tests; no changes to 6.1's
verdict verbs beyond the spec cross-references; no v1-surface edits;
no `plans/**` in the task PR.
