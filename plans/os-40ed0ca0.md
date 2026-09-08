# os-40ed0ca0 — Phase 13 item 5: the A2A-shaped cross-organization boundary

Build plan Phase 13 item 5: the A2A-shaped cross-organization boundary
(III.N). Charter §II.15 ("Cross-organization collaboration is opaque
(capability cards, a task lifecycle state machine, artifacts-only
exchange — A2A-shaped); prompts, reasoning, and internals never cross
a trust boundary"), III.N row 3 ("Cross-org collaboration is opaque
(capability cards, task lifecycle, artifacts-only)"), Appendix C (the
A2A protocol as prior art: tasks and artifacts cross trust boundaries,
internals never do). Deps: Phase 12, and Phase 13 item 4
(`plans/os-48df10a2.md`): the request ingress and the read remotes are
what this boundary is built from.

## What the tree actually shows

- **After item 4, cross-repo work enters a target ledger as a
  request** (`request.filed`, kind `cross-repo`, from an ingress key
  with no capability) and the source reads the target's answer through
  a read remote; the org view (`seed federation report`) is a
  projection over verified chains. Both presume the two deployments
  share a forge and a git transport: a read remote is a ledger ref
  fetched and verified from its own genesis.
- **Nothing is published for a stranger.** A deployment's squads,
  lanes, tiers and the ingress it accepts live in `seed.json` and the
  lane manifests, readable by whoever can read the repository; there
  is no signed statement of what the deployment offers, and no way for
  an organization that cannot read the repository to learn it.
- **Artifacts are local files by digest** (`internal/artifact`:
  `Put`, `Get`, the sealed variants), retrievable by whoever has the
  store; the forge-hosted admission service (Phase 12 item 2) is the
  one HTTP surface the tree has, stateless, speaking envelopes.
- **Opacity is already the boundary's habit inside one deployment**:
  a message's body is read only by the deliberate second act, the
  situation read carries notices not prose, and the classification
  lint keeps content out of payloads. Across organizations the same
  rule must hold for everything: the other side sees offers, states
  and artifacts, never a ledger.

## Design decisions (binding for this task)

**D1. The capability card is a signed, published statement of what a
deployment offers, and nothing else.** `seed boundary card --config
seed.json --key <operator key>` renders `card.json`: the deployment's
name, its ingress (the request kinds it accepts and the endpoint or
remote a request is filed through), the squads and the tiers each
accepts work at (from `teams` and `guardrails`, the names only), the
artifact kinds it returns (receipts, plans, bodies by digest), the
protocol version, and a JCS-canonical signature by the operator key
over the card. The card carries no fingerprint but the signer's, no
lane manifest, no fragment, no prompt, no position. Its publication
location is bound: the card is checked in at `next/boundary/card.json`
(the file `seed boundary card` writes, which CI re-renders and diffs
so a stale card fails the gate) and the boundary surface serves that
same file at its card route; a reader verifies the signature against
the operator key it was given out of band. Refused:
a card that names internals (manifests, fragments, budgets, models);
an unsigned card.

**D2. The task lifecycle across the boundary is five states, derived,
never stored.** A cross-org task is a request the source filed into
the target (item 4's `cross-repo` request) and the target's contract
that answers it. Its boundary state is derived from the target chain
by the target's own read surface: `requested` (the request stands
unanswered), `declined` (answered `declined`), `accepted` (answered
`filed`, the intent unclaimed), `working` (claimed), `done` (the
merge chain closed) — nothing finer, because finer is internals. The
target renders it as `boundary/tasks/<request>.json` in its
projections (item 4's federation projection gains the boundary
view), and `seed boundary tasks --remote <target>` reads it as a
stranger would: the state, the positions of the request and its
answer, and the artifact digests the contract published, and no
other field. Refused: a state the other side could not observe from
its own read; any payload, actor or fence crossing.

**D3. Artifacts cross by digest, and only artifacts.** The target's
boundary surface serves an artifact by digest to a reader that names
the digest (`seed boundary serve`, read-only, credential-free for the
card and the task states, an optional bearer for artifacts, stateless:
a clone and an artifact store), and a receipt, a plan or a body is
what a task publishes (the digests in D2's task view). `seed boundary
fetch --remote <target> --digest <d> --artifacts <dir>` stores what it
fetched under the same digest, verified on arrival. The source cites
it by digest in its own chain (a packet ref, a verdict's receipt),
never by copying content into a payload. Refused: fetching by path or
by name; serving anything not in the artifact store; serving the
ledger.

**D4. Opacity is the invariant, proven at the surface.** The boundary
surface exposes exactly three reads (the checked-in card, tasks,
artifact by digest)
and one write (item 4's request, through the ingress the card names);
the drill enumerates every route and every field of every response
and pins the set, so a field added later is a deliberate change to a
pinned list; a response is swept for ledger material (fingerprints,
positions beyond the request's and the answer's, payloads, packet
text) and refuses to carry any. The keyring vocabulary gains nothing:
no capability is "cross-org"; a foreign organization holds an ingress
key with no capability in the target and nothing at all in the
source. Refused: a super-authority in either direction.

**D5. Two organizations, drilled.** Two ledgers with distinct roots in
one test: A publishes its card; B verifies it against A's operator
key, files a cross-repo request into A through A's ingress, watches
the task move `requested → accepted → working → done` through A's
boundary reads as A's lanes drive the contract, fetches the receipt by
digest, and cites it in B's own chain. Then the refusals: a task
read with a field the pin does not list, an artifact by path, a card
that names a manifest, an unsigned card, a request into a deployment
whose card does not accept the kind. Mutation evidence: a task view
carrying an actor fingerprint; the serve exposing the ledger ref; an
artifact served that the store does not hold; a card unsigned; a
request kind the card refuses that the ingress admits.

**D6. Bounds.** A2A is the shape, never a dependency: no A2A library,
no JSON-RPC surface (item 6's machine protocol is where a second
surface over the CLI's verbs lands), no agent-to-agent negotiation.
Discovery is out of band (the card's location and the operator key
are exchanged by people). Payment, SLAs and identity federation are
absent by design (§II.18's spirit: nothing without a fold and a
refusal).

## Steps

1. `internal/boundary` (new): the card (render, sign, verify), the
   task-state derivation over a verified chain, the response pins.
2. `internal/project`: the boundary view beside item 4's federation
   projection; `internal/artifact`: the verified-on-arrival put.
3. `cmd/seed`: `boundary card | tasks | fetch | serve`.
4. Drills D5; mutation evidence.
5. `next/spec/boundary.md` (new), `protocol.md`, `projections.md`,
   `postures.md` (the card's location), `envelope.md` (the surface's
   codes); `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/boundary/**` (new), `next/internal/project/**`,
  `next/internal/artifact/**`, `next/internal/posture/**`
- `next/cmd/seed/**`, `next/boundary/**` (the published card, new)
- `next/spec/boundary.md` (new), `next/spec/protocol.md`,
  `next/spec/projections.md`, `next/spec/postures.md`,
  `next/spec/envelope.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-40ed0ca0.json`

Nothing outside `next/**` except the work-product files above. NOT
`.seed/**`, NOT `scripts/**`, NOT `.github/workflows/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **The card says what is offered and nothing else.** It renders from
   the declaration, is signed by the operator key, verifies against
   it, and refuses to name internals.
2. **The task lifecycle is derived and five-stated.** A cross-org
   task moves `requested → accepted → working → done` (or `declined`)
   in the target's boundary view as the chain moves, and the view
   carries the pinned fields only.
3. **Artifacts cross by digest only.** A receipt fetched from the
   target verifies on arrival and is cited by digest in the source's
   chain; fetching by path or name refuses.
4. **Opacity is pinned.** Every route and field of the boundary
   surface is enumerated and pinned; a response is swept for ledger
   material and carries none; the keyring vocabulary is unchanged.
5. **Two organizations in CI.** D5's drill runs end to end with
   distinct roots and no shared key.
6. **Mutation evidence.** Each fails a drill: a fingerprint in a task
   view; the ledger ref served; an artifact served that the store
   lacks; an unsigned card; a request kind the card refuses that the
   ingress admits.
7. `make check` green with coverage measured cold, three readings
   above the gate; no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- Every pre-existing chain verifies byte by byte; every projection is
  byte-identical on chains carrying no cross-repo request; item 4's
  ingress, federation report and request obligations are unchanged;
  the admission service's routes are unchanged (the boundary surface
  is its own binary path, never a widening of admission).

## Validation Commands

- Boundary: `cd next && go test ./internal/boundary/ ./internal/project/ ./internal/artifact/ ./cmd/seed/ -run 'Boundary|Card|CrossOrg|Opacity|Federation' -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

New: `next/internal/boundary/`, `next/boundary/card.json`,
`next/spec/boundary.md`, the boundary verbs under `next/cmd/seed/`.
Modified: `next/internal/project/` (the boundary view),
`next/internal/artifact/` (verified put), `next/internal/posture/`
(the card's declared location), four specs, the three docs files, the
receipt. No `.seed/`, `scripts/` or workflow change.
