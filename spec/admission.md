# The enforced admission deployment (`seed-admit`)

The reference deployment of the enforced self-hosted posture
([`modes.md`](modes.md), [`postures.md`](postures.md) once it lands):
a git server-side `pre-receive` hook on the ledger ref. `cmd/seed-admit`
is that hook. It is **stateless by construction** — every decision is
rebuilt from the repository it guards, so a replacement instance rebuilds
from a clone ([`SEED-NEXT.md`](../SEED-NEXT.md) §II.2) — and it imports
the same `internal/admit` rule set the cooperative client runs, so the
postures differ in where the rules run, never in which rules run.

The hook has two halves. The **ledger half** guards the ledger ref
(`refs/seed/ledger`); the **code half** guards every other ref. A push
touching both is judged half by half, and because `git` applies no ref
updates when a `pre-receive` hook exits non-zero, any refusal fails the
whole push atomically.

## The ledger half

For the guarded ref, the hook replays the pushed stream through one
`VerifyFromGenesis` (parse, linkage, signatures, actor resolution,
version discipline, upgrade schemas) and then runs the admission-only
rules — the checks a full verification deliberately tolerates in history
— over the records beyond the previously admitted tip, selected by
exclusion so rules future phases add flow through automatically. A
record-level prefix check pins append-only-ness: the previously admitted
range must be a byte-identical record prefix, so a descendant commit
whose tree rewrites or drops an admitted record refuses even though its
commit graph fast-forwards. A deletion and a non-fast-forward update of
the ledger ref refuse outright, and the guarded ref's tree carries only
the ledger layout (`HEAD` and `segments/*.jsonl`).

## The code half

The code half is the part [`SEED-NEXT.md`](../SEED-NEXT.md) §II.2
names when it says an actor's credential "allows proposing to the ledger
and pushing to its own authorized code branches — it never allows writing
the ledger ref directly in enforced modes." It decides from two things,
both of which the hook already has or the transport asserts, and neither
of which it reads from the pushed tree:

- **The ledger at the guarded ref's current tip.** Standing comes from
  the keyring projection; claim holders come from the lifecycle fold —
  derived exactly as the ledger half derives them.
- **The pusher's identity**, `SEED_PUSHER`: the actor's key fingerprint.
  In the reference deployment the ssh forced command derives it from the
  authenticated key — the fixed defaults make an actor's signing key and
  its credential one ed25519 key, so the fingerprint is derived, never
  configured. Forging it is stealing another actor's credential, which
  the security invariant places outside the adversary
  ([`SEED-NEXT.md`](../SEED-NEXT.md) §I.2). A push asserting no
  identity is refused on every code ref.

The rules:

| ref | rule |
|---|---|
| the default branch (`HEAD`'s symref, so no config names it) | fast-forwards for operator standing only; force-update and deletion refuse for everyone |
| a contract branch `refs/heads/seed/<contract>` | any update from the actor holding the **active claim** on `<contract>`, and nothing from anyone else — authorized by the claim, so it closes when the window closes; a revoked or suspended holder's branch closes with its standing |
| tags | created by operator standing only; an update or deletion of an existing tag refuses for everyone (immutable release tags, §II.14) |
| any other ref | operator standing creates or fast-forwards it; an agent credential touches nothing else |
| **any ref** | a commit touching a path on the **protected surface** refuses unless the pusher is the governance root — the surface is changed only by the governance root (§II.14), so an operator (the maintenance lane holds `operator` and is an agent key) may advance the default branch but never write the surface |

The protected surface is the declaration's `protected` list plus the
declaration's own path (`seed.json`), read from the **default branch's
current tip** — never from the pushed commits, or an attacker would
unprotect the list by rewriting it in the same push. A protected commit
is one this push introduces (reachable from the new tip, not from the old
tip, not from the default branch), so merging the operator's default
branch into a contract branch carries the surface without introducing it.
A declaration that does not parse refuses agent pushes until an operator
repairs it.

## What the deployment proves, and what it does not

The compromised-actor drill ([`redteam.md`](redteam.md)) plays the §I.2
adversary against this deployment and asserts the ceiling clause by
clause. The bound is stated there: the drill proves the
enforced-self-hosted posture. Cooperative mode has no server-side
enforcement by declaration, and enforced-forge-hosted is the admission
service ([`postures.md`](postures.md)); neither is this hook.
