# The cross-organization boundary

Status: normative for `next/**`. Charter authority: SEED-NEXT.md
III.N (federation and cross-organization work), §II.15 (surfaces
propose, the ledger decides), §II.18 (nothing without a fold and a
refusal). Plan: `plans/os-40ed0ca0.md`. Builds on
[`requests.md`](requests.md): the request ingress is the one write
across this boundary, and the federation read is its sibling for
organizations that share a forge.

Two organizations that share nothing — no forge, no key, no ledger —
still need to hand work across: one offers, the other proposes, the
work moves, an artifact comes back. The shape is A2A's (a published
capability card, a task lifecycle, artifacts by reference) and the
implementation is Seed's own: no A2A library, no JSON-RPC surface, no
negotiation. Discovery is out of band (a card's location and its
operator key are exchanged by people); payment, SLAs and identity
federation are absent by design.

## The capability card (normative)

`seed boundary card --config <declaration> --key <operator key>
--name <name> [--out <file>]` renders and signs `card.json`, the
strict object of exactly these fields (the pin `CardFields`):

- `name` — the deployment as other organizations know it;
- `protocol` — the declaration's `protocol`, else this build's
  newest registered version;
- `ingress` — `{kinds, through}`: the request kinds the deployment
  accepts and the remote or endpoint a request is filed through, from
  the declaration's `boundary` block ([`postures.md`](postures.md));
- `squads` — each declared squad's name and the tiers it accepts work
  at (the guardrails' `default` and `max_agent`, names only);
- `artifacts` — the kinds a task returns: `receipt`, `plan`, `body`,
  each by digest;
- `signer`, `signature` — the operator key's fingerprint and its
  ed25519 signature, lowercase hex, over the card's **RFC 8785 (JCS)**
  bytes without the signature.

The canonical form is RFC 8785 exactly, through the same transform the
event model signs over: one canonicalizer for the tree, so a signature
made here is one an independent JCS verifier reconstructs. A compact
`encoding/json` encoding with sorted members is *not* a substitute:
Go escapes U+2028 and U+2029 where JCS emits them literally, and a
card's `name` may carry either.

The card carries no fingerprint but the signer's, no lane manifest,
no fragment, no prompt, no budget, no position. Refused
(`card_refused`, exit 3): an unsigned card; a card with a field
outside the pin; a card whose text names an internal (a manifest, a
fragment, a prompt, a budget, a model, a lane path); an ingress kind
that is no request kind; an artifact kind a task does not publish.

**Publication is bound.** The card is checked in at
`boundary/card.json`, the file `seed boundary card` writes;
`seed boundary check --config <declaration> --name <name> [--card]
[--pubkey <hex> | --pubkey-file <path>]` re-renders the content from
the declaration and diffs it (`card_drift`, exit 28, on a stale card)
and, given the operator's public key, verifies the signature. The
boundary surface serves the same file at `/card`.

**The repository's gate is a content gate, and says so.** `make check`
runs `boundary check` over the fixture deployment with no key, so what
it proves is that the card says what the declaration renders: a
declaration that moved without its card fails, and nothing there checks
who signed the card. The success envelope reports `"verified": false`
and a note saying as much. This is deliberate: verifying a signature
needs a public key, and the tree holds none, because a card's key is
exchanged out of band by people rather than published beside the thing
it authenticates.

**A key pinned in-tree is a trust anchor or it is nothing.** Should a
deployment ever check its operator's public key in beside its card, the
key file joins the declaration's `protected` list and `CODEOWNERS` in
the same change that introduces it. A key on neither list can be
swapped together with the card in one non-owner change, and a gate
reading it would still pass: it would verify that the card matches
whichever key the last committer supplied, which is no property at all.

**The reader's half takes no declaration.** `seed boundary verify
--card <file> --pubkey <hex> | --pubkey-file <path>` parses a fetched
card, verifies its signature against the operator key given out of
band, and reports the signer. It takes no `--config` and no `--name`,
because a stranger holding a card from someone else's `/card` has
neither: `boundary check` is the publisher's verb, comparing a card
against the declaration that renders it, and only the publisher has
that declaration. A card that does not verify is `card_refused`
(exit 3), never `card_drift`: drift is the publisher's finding that its
own card is stale, and a reader is not in a position to make it. The
key never comes from the card, and no flag lets it: a card that carried
the key that signed it would prove nothing.

**The key is in whatever form the operator already has.** Both verbs
read `--pubkey` and `--pubkey-file` as either an OpenSSH ed25519
authorized-keys line, the form `ssh-keygen -y` writes and the one
[`protocol.md`](protocol.md) accepts at key load, or the bare hex the
card's own `signer` speaks. Naming both flags at once is a usage error
rather than a precedence rule, refused before the card or the
declaration is read, so a malformed invocation never comes back as
drift.

**The card's word binds the ingress.** A deployment that declares a
`boundary` block accepts the request kinds it names and no other: the
`boundary` admission rule refuses `request.filed` with any other kind
(`request_refused`), so what the card refuses the ingress refuses too.

## The task lifecycle (normative)

A cross-org task is a `cross-repo` request the source filed into the
target and the target's contract that answers it. Its state is derived
from the target chain by the target's own read and is one of five —
nothing finer, because finer is internals:

| state | when |
|---|---|
| `requested` | the request stands unanswered |
| `declined` | answered `declined` |
| `accepted` | answered `filed`, the intent's contract unclaimed |
| `working` | the contract is claimed, in progress or in review |
| `done` | the merge chain closed (`merge.observed`, or the contract done) |

The view of one task is the strict object of exactly these fields
(the pin `TaskFields`): `request` (the request's position), `answer`
(its answer's position, null while requested), `state`, `artifacts`
(the digests the contract published: the approved plan's, every
verdict's receipt). No actor, no fence, no payload, no contract id.

The target renders the view as the `boundary` projection
([`projections.md`](projections.md)): `tasks.json` (the index) and
`tasks/<request>.json`, one per cross-repo request; a chain carrying
none builds the index empty and nothing else.

## The surface (normative)

`seed boundary serve --ledger <clone> --artifacts <store> [--card
<file>] [--listen] [--announce] [--bearer]` is read-only, stateless
(a clone re-read on every request, an artifact store, the checked-in
card) and serves exactly the pinned routes (`Routes`):

| route | response |
|---|---|
| `GET /card` | the checked-in card, byte for byte |
| `GET /tasks` | the list of task views |
| `GET /tasks/{request}` | one task view, `not_found` otherwise |
| `GET /artifacts/{digest}` | the artifact's bytes, `not_found` when the store lacks it; `unauthorized` without the bearer when one is configured |

Anything else — the ledger, a segment, the declaration, a lane, a path
under `/artifacts` that is not a digest — is `not_found`, and any
method but GET is refused: the surface is no oracle for what exists
and takes no write. The card and the task states are credential-free;
artifacts may sit behind a bearer the operator gives out of band.

## The reads (normative)

`seed boundary tasks --remote <endpoint> [--request <position>]`
reads the task states as a stranger and refuses a view carrying a
field the pin does not list (`boundary_unpinned`, exit 3): a field the
other side added is a deliberate change to a pinned list, never a new
word a reader silently learns.

`seed boundary fetch --remote <endpoint> --digest <sha256>
--artifacts <dir> [--bearer]` fetches one artifact by digest and
stores it under that digest, verified on arrival
(`artifact.PutVerified`; `artifact_mismatch`, exit 3, when the bytes
hash to anything else). Fetching by path or by name is refused at the
flag. The source cites what it fetched by digest in its own chain
(a request's `reference`, a packet ref, a verdict's receipt), never by
copying content into a payload.

## Opacity (normative)

Opacity is a checked property: the drill enumerates every route and
every field of every response against the pins, and sweeps every
response for ledger material — every fingerprint on the chain, every
payload, every packet, the clone's path — and refuses to carry any.
The keyring vocabulary gains nothing: no capability is cross-org; a
foreign organization holds an ingress key with no capability in the
target and nothing at all in the source. There is no super-authority
in either direction.

## Refusals

| exit | reason | when |
|---|---|---|
| 3 | `card_refused` | an unsigned card, a field outside the pin, an internal named, a kind that is not one |
| 28 | `card_drift` | the checked-in card does not say what the declaration renders, or is absent |
| 3 | `boundary_unpinned` | a task view carrying a field the pin does not list |
| 3 | `artifact_mismatch` | fetched bytes that hash to something other than the digest they were fetched as |
| 4 | `not_found` | no task at that request; no artifact under that digest |
| 3 | `request_refused` | a request kind the deployment's card does not accept |

## Conformance

- `internal/boundary`: `Render`, `Sign`, `Verify`, `Parse`, `Check`,
  `Tasks`, `Sweep`, `FieldsOf`, the pins.
- `internal/project`: the `boundary` projection (version 1).
- `internal/artifact`: `PutVerified`.
- `internal/posture`: the `boundary` block; `internal/admit`: the
  `boundary` rule.
- `cmd/seed`: `boundary card | check | serve | tasks | fetch`.
- Drills: the two-organization drill (`TestTwoOrganizationsAcross
  TheBoundary`: distinct roots, the card verified, the request
  through the ingress, the five states as the target's lanes drive
  the contract, the receipt fetched by digest and cited, opacity
  route by route) and the refusals (`TestBoundaryRefusals`).
