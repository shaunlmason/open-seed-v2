# sealed-checks.md — the commitment predates the implementation

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §7 ("Sealed checks — a designed subsystem, not a phrase")
> and §8 (the verifier "unseals and runs the sealed checks");
> conformance III.F sealed rows;
> [`docs/build-plan.md`](../docs/build-plan.md) Phase 6
> item 3 and the binding default "Sealed-check encryption:
> `filippo.io/age` (X25519 recipients = verifier keyring)"; plan
> `plans/os-3128535a.md`. Implemented by `internal/seal`, the sealed
> bucket in `internal/artifact`, the seal rule in `internal/admit`,
> the fold's sealed fact, the unseal path in `seed verdict
> render`/`check`, and `seed seal create|rotate|audit`.

## Honest scope

Sealed checks are **defense-in-depth against specification gaming,
not a structural solution to it**. An implementer can still infer
likely checks, overfit the visible spec, or exploit verifier
weaknesses. The load-bearing defenses remain independent evidence,
invariant and property-based tests, adversarial cases, and human
review where the spec itself is incomplete; sealed checks raise the
cost of aiming at the proxy. Every other guarantee below is read
inside that frame.

## The commitment

`check.sealed` is a **fact, not a transition**: strict payload

```json
{"commitment": "<64 lowercase hex>"}
```

admitted only while the subject is in `ready` **with no prior
`claim.taken`** — a subject back in `ready` after `claim.released` or
`claim.reaped` refuses by name, because implementation already began
and a commitment appended then proves nothing. With both halves, the
admitted position ordering *is* the proof that the checks predate
implementation. One commitment per subject: a second refuses (change
sealed checks by cancel and re-file; rotation re-encrypts, never
re-commits). Raw-pushed seals verify tolerated, but the fold records
the fact **only from its legal window** (ready, no prior claim, first
seal); outside it they are anomalies, never facts — a raw seal must
not retroactively claim a pre-existence the ordering disproves. The
window rule cannot see grants, so a raw seal planted *inside* the
window by a sealer-less key still folds — and is refused at use: the
unseal path replays the keyring to the seal's own position and
refuses a signer that held no sealer grant there
(`seal_unauthorized`, exit 22), and `seed reconcile` surfaces the
same condition as the record-derivable `seal_unverified` class — the
verdict-laundering countermeasure, replayed against seals.

The commitment is the SHA-256 of the JCS canonical bytes of the
sealed envelope

```json
{"salt": "<64 hex>", "checks": ["<command>", ...]}
```

The 32-byte random salt lives **only inside the ciphertext**:
publishing it would invite dictionary attacks on low-entropy check
bodies, so the commitment is verifiable exactly by the parties who
can decrypt, and is verified at every unseal — shape included: a
decrypted salt that is not exactly 64 lowercase hex refuses, since a
degenerate raw-crafted salt would quietly surrender the dictionary
resistance the commitment promises. An envelope with zero
checks never exists cooperatively (`seal create` refuses a checks
file that parses to no commands) and never passes raw-crafted (an
unsealed zero-check envelope is a broken seal at the verifier): an
empty seal would mark the contract sealed while running zero secret
checks, a vacuous pass.

## Confidentiality and custody

The body is encrypted with `filippo.io/age` to **the eligible
verifier keyring**: every active key holding the `verdict` capability
**and no implementation authority** — a key also holding claim or
operator (a root's implicit operator standing included) still renders
verdicts under L1 but is never a recipient, because it could decrypt
every open contract's checks and then claim one, and the capability
audit's invariant is that no implementer path can decrypt. Eligible
keys are wrapped as ssh-ed25519 age recipients derived from the
enrolled ed25519 public keys (`agessh`), so "recipients = verifier
keyring" holds with no new key material enrolled; the same keys
decrypt as identities.
The cross-protocol use (one ed25519 key signs verdicts and unwraps
seals) is the documented v0 trade; dedicated X25519 enrollment is the
named successor if the boundary ever needs it.

Ciphertext lives at `var/artifacts/sealed/<commitment>.age` — a
named, **mutable** bucket beside the content-addressed store,
referenced from the ledger only by the immutable commitment. Content
is not digest-checked on the way out: the commitment verifies the
decrypted plaintext, not the ciphertext, which rotation rewrites.
Deleting a ciphertext is the charter's erasure path, and from
os-db5cd353 the path is a verb: `seed artifact erase` records
`artifact.erased` on the subject, citing the commitment and the
obligation honored, and then removes the file
([`protocol.md`](protocol.md), "Erasure"). The audit surfaces the
absence either way, never silence: an erasure the chain records is
listed with its position, signer and reason, and one it does not is a
finding.

**Rotation** (`seed seal rotate`) re-encrypts every *open* sealed
subject to the current verifier keyring: decrypt with a still-able
identity, re-encrypt, overwrite in place. It writes no ledger events
and changes no commitment — the keyring change that motivated it is
already in the ledger. Terminal subjects are skipped: exposure is
bounded to contracts open during a compromise window.

## Authoring isolation

Three enforcement layers, each drilled:

1. **Grant disjointness.** `check.sealed` accepts the `sealer`
   capability only — no operator fallback (the verdict lane's
   posture: operator already stands in the claim and submission
   lanes, and an operator row here would put authoring and
   implementation authority on one capability). At `actor.granted`,
   sealer cannot join a key holding claim or operator (a governance
   root's implicit operator standing included), and neither can join
   a key holding sealer.
2. **Per-subject.** `claim.taken` refuses when the claiming key
   authored that subject's seal — the raw-history backstop the grant
   rule cannot see.
3. **The capability audit.** The test suite proves the cryptographic
   half: an implementing identity cannot decrypt the ciphertext (age
   refuses it as a non-recipient), a verifier identity can, and no
   envelope or refusal path emits the plaintext or salt.

## The verifier unseals and runs

When the fold carries a commitment, `seed verdict render` loads the
ciphertext, decrypts with `--key` as an age identity, recomputes the
commitment from the plaintext, and runs the sealed checks through the
6.1 runner profile in the same workspace; the receipt gains
`commitment` and `sealed_transcripts` ([`verdicts.md`](verdicts.md)).
A red sealed check forbids pass exactly like a visible one (exit 20).
The refusals, by name:

| exit | code | when |
|---|---|---|
| 22 | `seal_broken` / `seal_unauthorized` | ciphertext missing or unreadable, commitment or salt-shape mismatch, a zero-check envelope, or a seal whose signer held no sealer grant at its position |
| 23 | `not_recipient` | the identity cannot unwrap the ciphertext (rotation lag; the refusal names `seal rotate`) |
| 24 | `unsealed` | render on an above-trivial subject with no commitment — the "contracts carry sealed checks" gate at the verifier boundary; the trivial tier is exempt |

`seed verdict check` holds the full recompute-and-mismatch guarantee
on sealed subjects: it requires an unsealing identity, reruns the
sealed commands, and includes them in the recomputed receipt, so
invented sealed transcripts fail at exit 21 and there is no silent
partial verification.

## The audit

`seed seal audit` is detection at exit 0, the reconcile posture: per
open sealed subject, the ciphertext is present
(`seal_evidence_missing` otherwise) and the age header's ssh-ed25519
recipient tags match the current verifier keyring in both directions
— `recipients_stale` when a current verifier cannot unseal (naming
rotate), `recipient_foreign` when a stanza matches no current
verifier. The scan reads only the documented age v1 recipient stanza
lines; payloads stay opaque. Tags are agessh's four-byte key
fingerprints — an identification hint, not a proof of exclusivity;
the capability audit's decrypt drills carry the cryptographic claim.

An erased ciphertext is the one absence that is not a finding: when
the chain holds an `artifact.erased` for the subject's commitment
whose signer held the operator grant at the record's own position
(plans/os-db5cd353.md D4; `admit.ErasureValid`, the keyring replayed
there as it is for the seal's own authorization), the audit lists the
subject under `erased` with the record's position, signer, reason and
timestamp, and stays clean; a ciphertext deleted with no record, or
with only a record the boundary would have refused, stays
`seal_evidence_missing`. A render on an erased subject still refuses
`seal_broken`, its message naming the admitted erasure rather than an
absence.

Record-side, `internal/reconcile` surfaces two classes in `seed
reconcile` and the report: the neutral `unsealed` (an above-trivial
subject at or past `in_progress` with no commitment; the hard gate
stays render's exit 24) and `seal_unverified` (a folded seal whose
signer, replayed position-accurately, held no sealer grant — the seal
render refuses to unseal).

## Visibility

Contracts `Version: "7"`: each entry carries
`sealed: {position, commitment} | null` (explicit null, the chain
convention). Report `Version: "4"` counts `unsealed` among the
record-derivable classes. The cache mirrors the columns at schema
generation 6. Every derivation change republishes under a new
version-bearing build id at an unchanged tip.

## Conformance mapping

- III.F "contracts carry sealed checks" — the commitment fact and its
  pre-claim window, the ciphertext custody, render's exit 24 gate,
  and the `unsealed` surfacing.
- III.F "keyring rotation re-encrypts without touching history" — the
  rotate drill: ledger position unchanged, commitment unchanged, the
  revoked identity locked out, the current keyring able to unseal.
- III.F "authoring grants disjoint from implementation grants; a
  capability audit proves no implementer path can decrypt" — the
  grant-disjointness refusals both directions, the seal author's
  claim refusal, and the decrypt drills.
- III.F "documentation states their honest scope and names the
  load-bearing defenses" — the Honest scope section above.
- Part II §8 ("unseals and runs the sealed checks … visible and
  sealed check transcripts") — the render path and the receipt's
  sealed half, inside the recompute-and-mismatch boundary.
