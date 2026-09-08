# Plan: next — the erasure verb: erasure is an attributable event (os-db5cd353)

Charter III.A row 7 reads "Erasure obligations are honorable: erasing a
referenced artifact never breaks chain verification, and the erasure
is itself an attributable event." The first half is structural and
undrilled; the second is absent by construction: the protocol defines
no erasure verb, so erasing a sealed ciphertext leaves a maintenance
lint finding (`seal_evidence_missing`), which names neither who erased
nor when and cannot be verified by a fresh reader from the chain
alone. Found while verifying what the Phase 13 exit record can claim
(plans/os-d63c7441.md). The card names two courses: add the verb, or
revise the row. The autonomy contract prefers the charter's normative
text, and the charter wrote "attributable" deliberately, so this plan
adds the verb. Tier: standard (a new verb in the catalog, an admission
rule, a fold fact). Deps: none.

## What the tree actually shows

- **Erasure exists as a path and not as a fact.** The artifact store
  (`internal/artifact`) says "deleting one is the charter's erasure
  path, surfaced by the audit, never silence"; `seed seal audit`
  reports a missing ciphertext as `seal_evidence_missing`;
  `seed verdict render` refuses `seal_broken` on it. Nothing records
  the act, and no `Verb` constant matches eras/delet/redact.
- **Catalog growth is additive.** `next/spec/protocol.md`'s verb
  catalog says of `offer.*`, `curation.*` and `workflow.*`: "older
  validators refuse the unknown verb safely, so the protocol version
  does not bump", active from `seed/1`. A new fact verb follows that
  precedent; no `seed/8`.
- **The chain references artifacts by digest.** A subject's fold
  carries `Sealed.Commitment` (the sealed body's salted hash, a
  lowercase-hex sha256 the store keys the ciphertext by) and each
  `VerdictFact.Receipt` (the receipt's content digest, stored by
  `artifact.Store.Put`). Those are the references a contract's fold
  can vouch for; anything else the chain references lives in payloads
  the fold does not index by digest.
- **Facts beside the lifecycle have a home.** `request.filed` is a
  fact that changes no state, folded into `Fold.requests` with an
  accessor, judged by its own admission rule, appended by a loop verb
  in the shared transport shape, drafted by the affordance catalog
  from a probe view, and rowed in `actors.md`'s capability table and
  `keyring.AcceptedCapabilities`. The erasure verb takes every one of
  those seams.

## Design decisions (binding for this task)

- **D1 — `artifact.erased`, a fact.** Namespace `artifact.*`, one
  verb, additive catalog growth active from `seed/1` (below it the
  rule is inert, like every other fact). Strict payload
  `{"artifact": "<lowercase-hex sha256>", "reason": "<one line, at
  most 200 bytes>"}`; subject the contract whose fold references the
  artifact, or `system` for an artifact the operator attests is
  referenced elsewhere. The verb changes no lifecycle state.
- **D2 — the rule holds the reference and the once.** On a contract
  subject the digest must be that subject's sealed commitment or one
  of its verdicts' receipt digests, refused otherwise naming what the
  subject references (`erasure_unreferenced`, exit 3's family: a
  citation the chain does not hold); on `system` any well-formed
  digest admits, the operator's attestation being the reference. The
  tombstone is digest-wide: the store holds one object per digest, so
  an artifact two contracts reference is erased for both by one act,
  and an artifact erased once, on any subject, is not recorded again
  (`erased_already`, naming the position, signer and subject of the
  first record), since a second record would attribute an act that did
  nothing. Shape refusals are the strict-object refusals every fact
  verb has.
- **D3 — operator only.** `artifact.erased` accepts `operator` and
  nothing else: an erasure obligation is a governance act a human
  answers for, the `decision.recorded` posture, and no lane's loop
  erases. The keyring row, `actors.md`'s table and the completeness
  drills all gain the verb.
- **D4 — the fold keeps the fact and the audit cites it.** `Fold`
  gains `erasures` (position, timestamp, signer, subject, artifact,
  reason) with `Erasures()` and `Erasure(artifact)`, the digest-wide
  lookup, so a contract whose commitment was erased under another
  contract's record is attributed to that record rather than reported
  as missing evidence. `seed seal audit` reports a missing ciphertext
  whose commitment the fold holds an erasure for in a new `erased`
  list, naming the position,
  the signer and the reason, and counts it as no finding: an honored
  erasure leaves the audit clean, which is what "honorable" buys; a
  missing ciphertext with no erasure record stays
  `seal_evidence_missing`, the unattributed absence the row forbids.
  `seed verdict render` on an erased subject still refuses
  `seal_broken`, its message naming the erasure.
- **D5 — the verb erases after it records, and reports what it
  removed.** `seed artifact erase --ledger|--remote … --key --subject
  --artifact <digest> --reason <text> --repo <dir> [--artifacts <dir>]`
  appends the record through the loop seam and then removes the bytes
  the digest keys in the store (the sealed bucket, the content tree,
  or both), reporting `removed`; bytes already gone are reported as
  such, since the record is the attribution and an erasure after the
  fact is still an erasure. The order is deliberate, and the resume
  path is explicit: a run that dies between the append and the removal
  leaves a standing record with the bytes present, and the next run of
  the verb finds the record (on any subject) and finishes the removal
  without a second record, reporting `recorded: false` and the
  position it finished; a drill plants exactly that state (the record
  landed raw, the ciphertext left in place) and asserts the finish.
  Bytes gone with no record is the silence the row forbids.
  `artifact.Store` gains `Erase(digest)` for the two buckets.
- **D6 — the surfaces name it.** The affordance catalog gains the
  probe (the first of the subject's references, its sealed commitment
  then its verdicts' receipts, that no erasure tombstones; else a zero
  digest that the rule refuses as unreferenced, so the verb is drafted
  exactly while something remains erasable and never for an erased
  digest); the registry gains the `artifact` group; the generated
  capability document is regenerated; `protocol.md`'s catalog gains
  `artifact.*` and a short "Erasure" section; `sealed-checks.md`'s
  erasure paragraph and audit classes follow; `actors.md` gains the
  row.
- **D7 — the row flips with the drills as evidence.** III.A row 7 in
  `next/spec/conformance.json` moves to `met`, the first clause by the
  verify-after-erasure drill and the second by the attribution drills;
  `conformance.md` regenerated (the os-9ef9ab34 precedent).
- **D8 — bounds.** No protocol version bump, no transition-table row,
  no change to any existing verb's rule or shape, no change to
  `internal/keyring`'s standing semantics beyond the capability row,
  no change to the hook or the service (they run the rule set).
  NOT `.seed/**`.

## Steps

1. `internal/erasure` (the shape: `Verb`, `Parse`, `Render`,
   `MaxReasonBytes`); the fold fact and accessors in
   `internal/transition`; the `erasure` rule in `internal/admit`; the
   keyring row; the probe; `artifact.Store.Erase`.
2. `cmd/seed/artifact.go` (`artifact erase`) registered in the
   catalog; `seal audit`'s `erased` list; `verdict render`'s message.
3. Drills: `internal/admit/erasure_test.go` (shape, reference, once,
   grant, inert below seed/1, afforded where erasable);
   `internal/artifact` (Erase of each bucket and of nothing);
   `cmd/seed/artifact_cli_test.go` (seal, erase, the chain verifies,
   the ciphertext is gone, the audit names the erasure by position,
   signer and reason and is clean, an unattributed deletion on another
   subject stays `seal_evidence_missing`, render refuses naming the
   erasure, a run that died after the append is finished by the next
   run without a second record, a digest shared by two contracts is
   erased and attributed for both, a content artifact erased on
   system, a non-operator refuses `out_of_grant`, an unreferenced
   digest refuses); the completeness lists.
4. Specs (`protocol.md`, `sealed-checks.md`, `actors.md`), the
   generated docs, the conformance row and `conformance.md`,
   `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/erasure/*` (new)
- `next/internal/transition/transition.go`, `next/internal/transition/*_test.go`
- `next/internal/admit/admit.go`, `next/internal/admit/affordances.go`, `next/internal/admit/*_test.go`
- `next/internal/keyring/keyring.go`, `next/internal/keyring/keyring_test.go`
- `next/internal/artifact/artifact.go`, `next/internal/artifact/*_test.go`
- `next/cmd/seed/artifact.go` (new), `next/cmd/seed/artifact_cli_test.go` (new), `next/cmd/seed/catalog.go`, `next/cmd/seed/seal.go`, `next/cmd/seed/verdict.go`
- `next/internal/lane/lane.go` (the act map, if the registry drill needs it)
- `next/spec/protocol.md`, `next/spec/sealed-checks.md`, `next/spec/actors.md`
- `next/spec/conformance.json`, `next/docs/generated/*`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-db5cd353.json`

Nothing else. NOT `next/internal/version/**`, NOT
`next/spec/transitions.json`, NOT `next/cmd/seed-admit/**`, NOT
`next/lanes/**`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **Erasure never breaks verification.** After `seed artifact erase`
   removes a sealed ciphertext and a content artifact, `seed ledger
   verify` is green and the erasure records stand at the positions
   the verb reported.
2. **The erasure is attributable from the chain.** `seed seal audit`
   names the erased subject in `erased` with the record's position,
   signer and reason, reports no `seal_evidence_missing` for it, and
   is clean; a ciphertext deleted with no record stays
   `seal_evidence_missing`.
3. **The rule holds the reference, the once and the grant.** An
   unreferenced digest on a contract refuses by name; a second erasure
   refuses naming the first; a key without `operator` refuses
   `out_of_grant`; the shape refusals are strict; the verb is drafted
   in the affordances exactly where the subject references something.
4. **The row reads met.** III.A row 7 is `met` in the table; `seed
   docs check` is clean on the regenerated documents; the doctor's
   outstanding rows no longer list A.7.
5. `make check` green; no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- Every existing verb's rule and shape is unchanged; the transition
  table, the protocol version register and the lane manifests are
  byte-identical to `main`; the seal drills, the verdict drills, the
  affordance soundness and completeness drills and the keyring
  completeness drill pass with the verb added to their lists.

## Validation Commands

- Boundary: `cd next && go test ./internal/erasure/ ./internal/admit/ ./internal/transition/ ./internal/keyring/ ./internal/artifact/ -count=1`
- Boundary: `cd next && go test ./cmd/seed/ -run 'Artifact|Seal|Serve|Registry' -count=1`
- Retention: `make check` (exit checked separately from any pipe)

## Expected diff shape

Added: `internal/erasure/erasure.go` and its test (roughly 150 lines),
`cmd/seed/artifact.go` (roughly 120), `cmd/seed/artifact_cli_test.go`
(roughly 200), `internal/admit/erasure_test.go` (roughly 150).
Modified: `transition.go` (the fact, the fold arm, the accessors,
roughly +60), `admit.go` (one rule, roughly +50), `affordances.go`
(one probe and one view field), `keyring.go` (one case), `artifact.go`
(`Erase`), `seal.go` (the `erased` list), `verdict.go` (one message),
`catalog.go` (one group), the three specs, the conformance row, the
regenerated documents, the three docs files, the receipt.
