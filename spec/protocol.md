# protocol.md — Seed wire protocol, version `seed/0`

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §1 (the ledger) and Appendix B (wire-level sketch);
> [`docs/build-plan.md`](../docs/build-plan.md) §1 fixed defaults.
> This file is the versioned canonical-form spec the charter's Appendix B
> defers to; the genesis event names the protocol version that binds it.
> Envelope shape and exit codes live in [`envelope.md`](envelope.md).

## Protocol version

- The protocol version is the string `seed/0`.
- Genesis names it; **every event carries the version active at its
  position** in its `v` field.
- **Active version**: the version a chain runs at, starting as the version
  genesis names and changing only at a `system.protocol.upgraded` event.
  The upgrade event is the last event of the old version: it carries the
  old version in `v` and names the new version in its payload; every later
  event carries the new version.
- **Verification across history**: an implementation declares the set of
  versions it supports. Replay accepts a chain when every event's `v`
  equals the version active at that event's position and every active
  version is in the supported set. The version-mismatch refusal (exit 10,
  see `envelope.md`) is reserved for an event whose `v` differs from the
  active-at-position version, or an active version the implementation does
  not support; a valid older prefix under a supported older version always
  verifies. It never guesses.
- **Admission**: proposals must carry the current active version; anything
  else refuses with the same distinct code.
- **Bump discipline**: `seed/N` increments on any change to the canonical
  form, the hash or signature algorithms, verb semantics, or validation rules
  that a conformant `seed/N-1` validator would judge differently. A bump
  lands as a PR editing this file plus the `system.protocol.upgraded` event.
  Additive verb-catalog growth that older validators safely refuse as
  unknown does not bump the version.
- **Version register**:
  - `seed/0` — the genesis default: chain form, halt, upgrade schemas,
    payload classification.
  - `seed/1` — activates the actor keyring semantics
    ([`actors.md`](actors.md)): `actor.*` payload schemas, standing
    transitions (root liveness included), and standing-aware signature
    resolution. A `seed/0` validator imposes no actor schema, so it
    judges these records differently — hence the bump; `actor.*` records
    at `seed/0` positions stay inert and grandfathered, and a new
    `actor.*` proposal at a `seed/0` tip refuses until the deployment
    upgrades.
  - `seed/2` — activates the runtime-tuple semantics
    ([`qualification.md`](qualification.md)): `actor.granted` may cite
    a `tuple`, `run.started` must declare one, an offer may scope by
    `tuples`, and drift refuses as `out_of_grant`. A `seed/1` validator
    strictly decodes `actor.granted` as `{capability}` and so fails a
    tuple-bearing grant at its position, which is exactly a validation
    rule the two would judge differently — hence the bump. At `seed/1`
    positions the field stays unknown-and-refused under a `seed/2`
    validator too, so every existing chain verifies byte for byte; a
    `seed/1`-only validator refuses an upgraded chain at the first
    `seed/2` record by version, never by misjudging the grant.
  - `seed/3` — activates the eval semantics ([`evals.md`](evals.md)):
    `intent.filed` may carry the `eval` marker, and `actor.qualified`
    and `actor.disqualified` are defined. Actor payload shapes are
    chain validity ([`actors.md`](actors.md)), so a `seed/2` validator's
    unknown-verb arm fails a chain carrying either verb as
    `bad_actor_event` at its position while this validator accepts it:
    the two judge the same record differently, hence the bump, which
    makes a `seed/2`-only validator refuse an upgraded chain at the
    first `seed/3` record by version rather than as corruption. At
    `seed/2` positions both verbs stay unknown-and-refused under a
    `seed/3` validator too, and the marker is neither read by the fold
    nor admitted at the boundary, so every existing chain verifies byte
    for byte.
  - `seed/4` — activates the independence levels
    ([`verdicts.md`](verdicts.md), "Independence levels"):
    `verdict.rendered`'s `independence` widens from the literal `L1`
    to the ordered vocabulary `L1`, `L2`, `L3`, the verdict may declare
    the verifier's `tuple`, and the tier table's `independence` column
    is enforced at the verdict boundary and reapplied along the merge
    chain. A `seed/3` validator's strict verdict decode refuses the
    `tuple` field and any literal but `L1`, so the two judge a `seed/4`
    verdict differently, hence the bump, which makes a `seed/3`-only
    validator refuse an upgraded chain at the first `seed/4` record by
    version rather than by misjudging a level. At `seed/3` positions
    the literal `L1` alone admits under a `seed/4` validator too, and
    the eval semantics of `seed/3` hold at `seed/4` alike (the gate is
    a named list), so every existing chain verifies byte for byte.
    Phase 10 item 4 adds to the entry (plans/os-2e34f66a.md D6):
    `verdict.rendered`'s optional `scorecard`, the `verdict.deferred`
    verb, and `actor.qualified`/`actor.disqualified` for capability
    `verdict`, each refused at a `seed/3` position by version
    ([`verdicts.md`](verdicts.md), [`evals.md`](evals.md)).
    Two later additions ride the same entry (plans/os-6bd9ffff.md D4,
    D5, D7; [`trajectories.md`](trajectories.md)): `contract.specified`
    gains the `ready` origin, the dispatcher's re-specification of an
    unclaimed contract ([`lifecycle.md`](lifecycle.md)), which a
    `seed/3` validator's table refuses as an illegal transition where
    this one applies it; and `plan.proposed` and `plan.approved`
    carry the plan's content `digest` ([`plans.md`](plans.md)), which
    a `seed/3` validator's shape has no field for. Each refuses at a
    `seed/3` position by version, naming `seed/4`, and the fold reads
    each at `seed/4` positions only, so every existing chain verifies
    byte for byte.
  - `seed/5` — activates the predecessor import
    ([`import.md`](import.md)): `system.imported` `{source,
    export_head, anchor, manifest}`, operator-only, admitted once per
    ledger, the provenance record a genesis import writes right after
    the upgrades. A `seed/4` validator's unknown-verb arm fails a chain
    carrying it, so the two judge a `seed/5` record differently, hence
    the bump, which makes a `seed/4`-only validator refuse an upgraded
    chain at the first `seed/5` record by version rather than as
    corruption. At `seed/4` positions the verb stays unknown-and-refused
    under a `seed/5` validator too, and nothing else changes, so every
    existing chain verifies byte for byte.
  - `seed/6` — activates racing mode ([`lifecycle.md`](lifecycle.md),
    "Racing"): on a racing squad's contract `claim.taken` admits while
    `in_progress` below the declared cap, each racer with its own
    fence, and a racer's exit is claim-scoped. A `seed/5` validator's
    lifecycle rule refuses the second claim as contention and its fold
    applies nothing, so the two judge a `seed/6` racing record
    differently, hence the bump, which makes a `seed/5`-only validator
    refuse an upgraded chain at the first racing record by version
    rather than by misjudging a race. At `seed/5` positions a second
    claim stays contention under a `seed/6` validator too, no table row
    changes, and every existing chain verifies byte for byte.
  - `seed/7` — activates the request ingress ([`requests.md`](requests.md)):
    `request.filed`, the one verb a proposal from a projection surface,
    a dashboard or another deployment enters the ledger by, a fact that
    changes no state and grants nothing, and `request.answered`, the
    dispatcher's close of one. A `seed/6` validator's unknown-verb arm
    fails a chain carrying either, so the two judge a `seed/7` record
    differently, hence the bump, which makes a `seed/6`-only validator
    refuse an upgraded chain at the first request by version rather
    than as corruption. At `seed/6` positions both verbs stay
    unknown-and-refused under a `seed/7` validator too, no table row
    changes, and every existing chain verifies byte for byte.
  - `seed/8` — activates the forge observation
    ([`observations-forge.md`](observations-forge.md)):
    `check.observed`, the observer's record of what the forge's checks
    and review threads say about the head under review, a fact on
    review subjects that changes no state; `submission.made`'s
    optional `pr`; and `contract.returned`'s second citation, a red
    observation beside a fail verdict. A `seed/7` validator's
    unknown-verb arm fails a chain carrying the fact and its strict
    return decode refuses the citation, so the two judge a `seed/8`
    record differently, hence the bump, which makes a `seed/7`-only
    validator refuse an upgraded chain at the first observation by
    version rather than as corruption. At `seed/7` positions the fact
    stays unknown-and-refused under a `seed/8` validator too, a return
    cites a verdict only, the `pr` field is not read, no table row
    changes, and every existing chain verifies byte for byte.
  - `seed/9`: restores the runtime-tuple semantics
    ([`ranking.md`](ranking.md) "Resume"; plans/os-29e2fef2.md). The
    named list behind `seed/2`'s semantics was not extended when
    `seed/5` through `seed/8` were registered, so the binaries that
    shipped those versions admitted a tupleless grant, start and offer
    at those positions, and a tuple-bearing one refused; those
    positions keep that recorded judgment. From `seed/9` `actor.granted`
    may cite a `tuple`, `run.started` must declare one and an offer may
    scope by `tuples` again, which the resumption the forge loop
    re-offers by needs. A `seed/8` validator strictly decodes a
    `seed/9` tuple-bearing grant as `{capability}` and fails it, so the
    two judge a `seed/9` record differently, hence the bump, which makes
    a `seed/8`-only validator refuse an upgraded chain at the first
    `seed/9` record by version rather than as corruption. At `seed/8`
    positions the field stays unknown-and-refused under a `seed/9`
    validator too, no new verb, no table row changes, and every
    existing chain verifies byte for byte.

## The machine surface

`seed serve` ([`platform.md`](platform.md)) is the machine protocol:
JSON-RPC 2.0 over stdio, framed as `machine-envelope/0`, a method per
CLI verb drawn from the one registry the CLI dispatches, each
invocation running the CLI's own run function and returning its
`seed-envelope/0` verbatim as the result. It adds no verb, no
semantics and no path to the ledger; the framing is versioned apart
from the envelope so the verb semantics never fork.

## The cross-organization boundary

The boundary ([`boundary.md`](boundary.md)) is not a protocol version:
it appends nothing and defines no verb. It is a read surface over a
verified clone — the signed capability card, the five-state task view
derived from the chain, artifacts by digest — and the one write across
it is the request ingress this register activates at `seed/7`.

## Canonical event form

Events are JSON objects canonicalized per **RFC 8785 (JCS)**. The canonical
bytes of an event are the JCS serialization of exactly these fields:

| field | type | meaning |
|---|---|---|
| `v` | string | protocol version (`seed/0`) |
| `ts` | string | RFC 3339 UTC timestamp; recorded for humans, **never an ordering authority** |
| `actor` | string | the acting identity's key fingerprint (below) |
| `verb` | string | `namespace.verb` from the catalog below |
| `subject` | string | contract id, actor id, or `system` |
| `payload` | object | verb-specific, schema-validated; coordination facts and references only (data classification, charter §II.1) |
| `prev` | string | chain hash of the preceding event's canonical form |

- **Signature**: `sig` is the Ed25519 signature (RFC 8032) over the
  canonical (JCS) bytes of the event **including `prev`**, encoded as
  **lowercase hex** (64 bytes, 128 hex characters). `sig` is carried
  alongside the canonical form (it is not part of the signed bytes).
- **Chain hash**: the SHA-256 of the same canonical bytes, encoded as
  **lowercase hex** (32 bytes, 64 hex characters). The next event's `prev`
  cites it.
- **Ledger record**: what storage and transport carry is the wrapper
  `{"event": <event object>, "sig": "<hex>"}`. Canonicalization and hashing
  apply to the inner `event` object alone, never the wrapper, so wrapper
  field order and whitespace are irrelevant to identity. Verifiers MUST
  recompute the JCS bytes from the parsed event; they never trust stored
  byte sequences.
- **Strict parsing (one accepted wire form)**: parsers MUST refuse a record
  carrying unknown fields in the wrapper or the event object, a duplicate
  key at any level (payload included), or trailing data after the record.
  Otherwise a record could carry correctly signed core fields plus unsigned
  material that survives in storage while escaping canonicalization, schema
  validation, and the classification lint; and duplicate keys let two
  parsers disagree about the same bytes.
- **Encodings, uniformly**: every hash, fingerprint, and signature in this
  protocol is lowercase hex with no prefix; uppercase hex is refused, not
  normalized, so exactly one encoding of a value is accepted on the wire.
  Base64 and multibase forms are not used anywhere.
- **Genesis**: the first event's `prev` is the **empty hash**: the SHA-256 of
  zero bytes, `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
  Genesis (`system.genesis`) names the initial governance root (operator
  keys) and the protocol version.

## Algorithms

- **Hash**: SHA-256 everywhere (chain, commitments, receipts), lowercase hex
  encoding.
- **Signatures**: Ed25519. OpenSSH `ed25519` public/private keys are accepted
  at key load so operators reuse existing keys; internally a key is the raw
  32-byte public key.
- **Actor fingerprint**: the lowercase hex SHA-256 of the raw 32-byte Ed25519
  public key (64 hex characters). Display surfaces may shorten it; events
  always carry the full fingerprint. The OpenSSH display form
  (`SHA256:<base64>`) is **not** used in events or projections: OpenSSH
  acceptance is a key-loading affordance only (the loader parses the OpenSSH
  ed25519 key format down to the raw 32-byte public key, and fingerprints
  that).

## Ordering

Order is **admitted ancestry**: the chain of `prev` hashes on the
authoritative ledger ref. No writer asserts a global sequence number; where
an ordinal is useful (projections, citations) it is derived at admission or
during projection, never proposed. (Charter §II.1; conformance III.A.)

## Storage reference (non-normative)

The reference deployment stores the ledger on the git ref `refs/seed/ledger`
as JSONL segments, one file per UTC day under `ledger/segments/`, each line
one ledger record (`{"event": …, "sig": …}`), with a `HEAD` record carrying
the tip hash; the artifact store rides `refs/seed/artifacts` (git-addressed)
with a filesystem fallback. These are
build-plan fixed defaults for the reference implementation, not protocol
requirements; any storage satisfying the canonical form, ordering, and
admission rules conforms. The client's private transport git dir under
its state directory carries auto-gc disabled, written on every open
rather than only at init, so a state dir an older build created is
hardened the first time a newer one opens it and no repository the
engine creates for itself is mutated by a collector after the process
that armed it has exited.

## Verb namespace catalog

Copied from charter Appendix B; payload schemas land with the phases that
implement them (schema files will sit beside this spec and be linted at
admission).

- `system.*` — `genesis`, `halt.declared`, `halt.lifted`, `checkpoint`,
  `protocol.upgraded`, and from `seed/5` `imported` (the predecessor
  import's provenance record, once per ledger; [`import.md`](import.md)).
- `actor.*` — `enrolled`, `granted`, `suspended`, `revoked`, `qualified`
  (cites eval results and the runtime tuple) and its inverse
  `disqualified`, both from `seed/3` ([`evals.md`](evals.md)). Payload
  schemas and standing semantics: [`actors.md`](actors.md), active from
  `seed/1`.
- `intent.*` / `contract.*` — `intent.filed` (from `seed/3` optionally
  carrying the `eval` marker, [`evals.md`](evals.md)),
  `contract.specified` (acceptance spec gate passed; sealed
  commitment), `contract.blocked`, `contract.cancelled` ….
- `claim.*` — `taken` (carries fence), `released`, `parked`, `reaped`
  (packet ref), `wedge.declared`.
- `plan.*` — `proposed`, `approved` (observation of the plan PR merge).
- `progress.*` — `milestone` (coarse; bounded frequency).
- `submission.*` — `made` (branch, evidence refs; from `seed/8` the
  optional `pr` the forge observation binds to,
  [`observations-forge.md`](observations-forge.md)).
- `verdict.*` — `rendered` (pass/fail, receipt, independence level
  achieved: `L1` alone before `seed/4`, `L1`/`L2`/`L3` with the
  verifier's declared tuple from it, and from `seed/4` the optional
  `scorecard`, the rubric's derivation-bearing half,
  [`verdicts.md`](verdicts.md)); `deferred` (from `seed/4`: the
  human-verdict deferral, the receipt and the items the verifier
  could not judge, on the bound submission).
- `merge.*` / `check.*` — `requested`, `observed` (external-fact
  observations; `check.observed` from `seed/8`, the forge's word on
  the head under review, [`observations-forge.md`](observations-forge.md);
  the closed inventory of every external-fact verb and what each may
  do is [`external-facts.md`](external-facts.md)).
- `offer.*` — `published` (the supervisor's eligibility-scoped,
  expiring invitation to claim; a fact, never a transition —
  [`offers.md`](offers.md), active from `seed/1`. Catalog growth here
  is additive: older validators refuse the unknown verb safely, so
  the protocol version does not bump).
- `budget.*` — `reserve`, `settle`, `release`.
- `run.*` — `started` (the spending gate; declares the runtime tuple
  from `seed/2`, [`qualification.md`](qualification.md)), `settled`
  (aggregate metering), `interrupted`.
- `message.*` — `sent`, `acked`.
- `request.*` — inbound proposals from projection surfaces (mirror edits,
  dashboard actions).
- `approval.*` — `requested` (the request an actor's governed act waits
  on: the verb, the actor that will act, one line of reason; a fact
  that changes no state and grants nothing), `granted` and `denied`
  (the operator's attributable answers, citing the request), the
  require-approval mode of per-verb policy ("Per-verb approval" below;
  charter §II.14), additive catalog growth active from `seed/1`.
- `curation.*` — `deadend.recorded` (the holder's candidate
  observation, on the contract), `hypothesis.proposed` (the curator's,
  on the subject its claim and exceptions derive),
  `hypothesis.contested` (the curator's held-out counter-evidence, on
  the hypothesis), `lesson.promoted` (the PR observation, on the
  hypothesis, citing the adversarial evaluation it survived),
  `lesson.retired` (the observation that a promotion is revoked, on the
  hypothesis, by regression, supersession or expiry; the evidence
  stays), `deadend.retired` and `deadend.unretired` (the curator's
  judgment that a dead end's environment moved, on the contract), all
  seven from [`curation.md`](curation.md) as additive catalog growth,
  active from `seed/1`.
- `workflow.*` — `workflow.proposed` (the curator's proposal of a
  drafted, mock-validated workflow for a recurring shape, on the shape
  id its occurrences derive, citing the file, the occurrences and the
  validating run) and `workflow.merged` (the observation that the
  proposal's PR landed the file in the registry, on the shape, citing
  the file and the PR), both from [`flywheel.md`](flywheel.md) as
  additive catalog growth, active from `seed/1`.
- `dependency.*`, `hierarchy.*`, `goal.*` — `dependency.linked`,
  `dependency.unlinked`, `hierarchy.parented`, `goal.aligned` (the
  relation facts beside the lifecycle: what a contract requires, what
  it belongs under, and the commit-anchored mission it serves; facts
  on a known open contract under the dispatch grant, never
  transitions — [`topology.md`](topology.md)), additive catalog
  growth, active from `seed/1`.
- `artifact.*` — `erased` (the operator's signed record that an
  artifact the chain references by digest was erased, on the contract
  whose fold references it or on `system`; the section below), additive
  catalog growth, active from `seed/1`.

## Erasure

The chain references bodies by hash and never carries them (the data
classification below), so erasing the bytes an artifact digest keys
never breaks verification: that is the first half of charter III.A row
7, and it is structural. The second half, "the erasure is itself an
attributable event", is **`artifact.erased`** (plans/os-db5cd353.md):

- **Shape.** The strict object `{"artifact": "<lowercase-hex
  sha256>", "reason": "<one line, at most 200 bytes>"}`
  (`internal/erasure`): the digest the chain references the artifact
  by, and the obligation honored, never a body.
- **Subject.** The contract whose fold references the digest, which
  admission holds: the subject's sealed commitment
  ([`sealed-checks.md`](sealed-checks.md)) or one of its verdicts'
  receipt digests ([`verdicts.md`](verdicts.md)); an unreferenced
  digest refuses `erasure_refused` (exit 3) naming what the contract
  does reference. On `system` any well-formed digest admits: the
  operator's attestation is the reference, for an artifact a payload
  cites that no contract's fold indexes by digest.
- **Once, digest-wide.** The store holds one object per digest, so an
  artifact two contracts reference is erased for both by one act: the
  tombstone is looked up by digest wherever it was recorded, a shared
  commitment's absence is attributed to that record on every contract
  that references it, and an artifact erased once is not recorded
  again; the refusal names the position, signer and subject of the
  first record, since a second would attribute an act that did
  nothing. A tombstone counts only when it passed the boundary: fold
  presence is never proof of admission, so a well-shaped record the
  raw seam landed under a key without the grant is kept by the fold
  and honored by nothing (`admit.ErasureValid` replays the keyring at
  the record's own position, as the seal's own authorization is
  checked); it neither blocks the operator's record nor attributes an
  absence.
- **Grant.** `operator` only ([`actors.md`](actors.md)): an erasure
  obligation is a governance act a human answers for, the
  `decision.recorded` posture; no lane's loop erases.
- **A fact.** The record changes no lifecycle state; the fold keeps it
  (`Fold.Erasures`, `Fold.Erasure(artifact)`), and every consumer reads
  it through `admit.Erasure`, the same lookup narrowed to the records
  that passed the boundary. The seal audit is one: a missing ciphertext
  whose commitment the chain holds an admitted erasure for is an
  honored erasure, listed with its position, signer and reason and no
  finding, while one with no record, or with only a record the
  boundary would have refused, stays `seal_evidence_missing`, the
  unattributed absence the row forbids.
- **The verb records before it removes.** `seed artifact erase
  --subject <contract|system> --artifact <digest> --reason <text>
  --repo <dir>` appends the record through the loop seam, then empties
  the store's buckets under the digest (the sealed ciphertext, the
  content, or both) and reports `removed`; an erasure that already
  stands, on any subject, is finished rather than re-recorded
  (`recorded: false`, the position it finished), which is also the
  resume path for a run that died between the append and the removal.
  The resume holds the grant the record took: a key without `operator`
  refuses `out_of_grant` under a standing record exactly as at a fresh
  append, before any removal. A removal the store refuses after the
  record landed, or stood, is `erasure_incomplete` (exit 5,
  [`envelope.md`](envelope.md)): the message names the position the
  record stands at and what was removed so far, nothing is recorded
  twice, and the next run finishes. The order is the point: a record
  with the bytes still present is a promise the next run keeps, and
  bytes gone with no record is the silence the row forbids.

## Per-verb approval

Per-verb policy on the machine-protocol surface has three modes
(charter §II.14, III.L row 4): **allow** and **deny** are the grant
table ([`actors.md`](actors.md), "Capabilities") and the declaration's
ceiling and routing rules ([`postures.md`](postures.md), "The
guardrails are enforced"); **require-approval** is this section
(plans/os-5781a026.md).

**The declaration names the verbs.** `guardrails.approvals` is a list
of `{"verb", "actors"?, "kinds"?, "min_tier"?}`: an act of `verb` by a
key the entry reaches on a contract whose tier is at or above
`min_tier` (absent means every tier; a `system` subject has no tier
and is governed whenever the verb is; a birth's tier is its filing's)
requires an open grant. An entry reaches the fingerprints `actors`
names and the keys whose roster kind `kinds` names, and every
non-human kind when it names neither, so the policy varies by actor
(one agent allowed while another of the same kind needs an approval)
and by risk class (the kind and the tier). A key with no asserted kind
(a governance root) is reached by no kind selector, only by its
fingerprint. A floor outside the tier vocabulary fails closed, the
ceiling's posture. `seed preseed check` holds each entry to the
catalog's verbs, fingerprint shape, the roster kinds and the tier
vocabulary (`preseed_incomplete`). The three approval verbs are never
governed. Policy, never chain validity: no declaration, no rule; a
raw-pushed governed act folds as filed and every chain verifies byte
for byte.

**Three fact verbs, additive from `seed/1`.** `approval.requested`
`{"verb", "actor", "reason"}` on the contract the act concerns or on
`system`: the actor named is the key that will act, the requester
whoever files it (the actor itself in the loop's case, or a lane on
its behalf), active standing only, like `request.filed`, since asking
grants nothing; the verb a catalog verb that is not an approval verb,
the actor an enrolled key, the reason one line of at most 200 bytes.
The subject is a contract the chain knows or `system`, with one
exception the policy forces: a request for `intent.filed` is on the
contract the filing would create, which the chain does not know until
the governed filing succeeds, because the grant is found on the
governed act's own subject and a birth's subject is the contract it
creates; every other verb's request on an unknown contract refuses.
`approval.granted` `{"request"}` and `approval.denied` `{"request",
"reason"}`, operator only with no fallback (`decision.recorded`'s
row), cite an `approval.requested` on the same subject not yet
answered; a request is answered once. The shapes and citations hold
regardless of declaration (`approval_refused`, exit 3), so a
raw-pushed grant citing nothing folds as an anomaly and admits nothing.

**The fold keeps the facts and spends the grants.** A request is open
once granted and until the first record on its subject naming its verb
and actor, which spends it at that record's position: one approval
admits one act, and a second act needs a second request. An unanswered
or denied request admits nothing.

**The admission rule.** A record whose verb the declaration governs,
by a key the entry reaches, on a subject at or above the floor,
refuses `approval_required` (exit 3, beside `tier_above_ceiling`)
unless a valid open grant names the verb and the actor; the message
names the request to file (`seed approval request --subject <s> --verb
<v> --reason <why>`) and, once one stands, the grant that answers it
(`seed approval grant --subject <s> --request <position>`). Valid is
the laundering countermeasure the tree fixes the shape of
(`admit.ApprovalValid`, the `RunStartValid` posture): the tolerant fold
records any well-shaped raw push, so before an open grant is trusted
its request is re-parsed at its position, its answer re-parsed at its,
and the keyring replayed to the answer's position to hold the answerer
to active operator standing and the named actor to enrollment; a grant
that fails any of these is a fact that authorizes nothing, and the act
refuses as if none stood. The verbs `seed approval request | grant | deny` are loop
verbs in the shared transport shape (the actor defaults to the
requesting key; the request to the oldest open one on the subject,
refused as a choice when several stand), the group `approval` joins
the registry, and `serve` carries the three as methods.

**The inbox.** An unanswered request is `approval.pending` in the
obligations projection ([`obligations.md`](obligations.md)), owed by
`lane:operator`, one row per subject carrying the oldest open request's
position and timestamp, discharged by either answer; the situation
read carries it with its age. The affordances draft the request for
any standing key on a subject the chain knows, the two answers for the
operator while a request is pending, and a governed act for its actor
exactly while an open grant stands.

## Data classification (summary)

Payloads carry **coordination facts and references, not content bodies**:
hashes and paths for artifacts, receipts, transcripts, and any prose beyond
short structured fields. Bulk or expirable content lives in the artifact
store with an erasure path, referenced by hash; erasing a referenced
artifact never breaks chain verification. The payload lint that enforces
this at admission lands in Phase 1 with a hostile fixture corpus
(conformance III.A).
