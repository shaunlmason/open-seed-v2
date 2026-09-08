# The compromised-actor drill

> "An enrolled implementer is fully compromised … it can spend only its
> reserved budget; modify only its authorized code surface; produce
> signed lies attributed to itself; and submit work for independent
> verification. It **cannot** claim unauthorized work, approve itself,
> rewrite history, alter gates, impersonate another actor, exceed its
> lease, or cause an invalid state transition to enter authoritative
> history." — [`SEED-NEXT.md`](../SEED-NEXT.md) §I.2

The charter calls this drill "the architecture's definition of done";
the build plan calls it the release gate and requires it green in CI
from here on ([`docs/build-plan.md`](../docs/build-plan.md)
Phase 12 item 1), and promotion (§5) requires it. `internal/redteam`
plays the §I.2 adversary against the enforced reference deployment
([`admission.md`](admission.md)) and asserts the ceiling clause by
clause at the push.

## The ceiling is a table with sides

`internal/redteam/testdata/ceiling.json` names all twelve §I.2 clauses
in the charter's own words — eight prohibitions and four permissions —
each with the Seed vocabulary it lands in and the side it is enforced
on. A clause carries the **ledger** side (admission's rule set through
the hook), the **code** side (the hook's code-ref half), or both. Three
clauses are two-sided: *rewrite history* (a ledger record vs. a
force-updated default branch or moved tag), *alter gates* (a self-grant
or lifted halt vs. a push to the default branch or a protected path),
and *exceed its lease* (an act citing a dead fence vs. a push to a
contract branch after its window closed). "Lease" is the claim window,
because Seed holds no lease ([`observations.md`](observations.md)).

Coverage is derived from the clause list and pinned **both ways**: every
`(clause, side)` target has exactly one primary corpus entry of matching
polarity, every corpus entry names a real target, and a two-sided clause
missing one side, an empty corpus, and an empty ceiling each fail.

## The adversary is a process constrained to git transport

The adversary holds a real enrolled key with a `claim` grant and a real
credential (its `SEED_PUSHER` identity), and runs `git` directly. It
never calls the honest CLI to perform an attack — an attack through
`seed claim take` proves the client refuses, which is not the claim —
and it never writes the bare repository's filesystem, because a writer
inside the server is the compromised infrastructure §I.2 excludes.
Forged events are signed with the compromised key and pushed with `git
push`, so the only thing between the attacker and the ref is the hook.

The fixture stages its ledger **honestly** through the client seam
(`admit.Validate` over `gitref.AppendLoop`), so every fact the adversary
later abuses passed the boundary; nothing is staged raw. Only the
enforced self-hosted posture builds: the harness refuses a cooperative
or forge-hosted fixture, because a drill that ran where the invariant
does not hold and reported green would assert the opposite of what the
charter says holds there.

## One derivation

Every ledger single-event entry is run through `admit.Check` in-process,
over the records it was judged against, and through the hook over a real
push; the two agree on every entry. So the boundary the drill exercises
and the boundary the client previews are provably the same rule set.

## The residuals

`internal/redteam/testdata/residuals.json` names where the adversary
succeeds within the ceiling, each pinned by a drill asserting the attack
**is** admitted (or, for the bounds, that the harness itself refuses):

- **the signed lie** — attribution is not trust; a false dead end is
  evidence attributed to the compromised key, and conclusion-writing is
  grant-gated ([`curation.md`](curation.md));
- **self-exhaustion** — a full-capacity reservation inside its own
  window is exactly what the permission grants; it strands its own
  contract's class, denial of its own progress, never of authority;
- **refusal-flooding** — every refused push costs a replay; the ref
  never moves, so it is denial of progress, not of authority;
- **test content** — the charter's own named residual (§II.14): ordinary
  test content outside the protected surface stays in write scope, with
  diff-vs-plan review and sealed checks as the mitigations;
- **the credential-to-identity binding** — the pusher's identity is the
  transport's assertion; a stolen credential buys the impersonated
  actor's contract branch and nothing on the signature-bound ledger;
- **the revocation reap** — a revoked key's proposals refuse at the push
  and its contract branch closes with its standing, but its still-open
  claim stands until reaped; the reap corroborated by the revocation
  record itself is carded (os-32d06c65, [`plans/os-465e356e.md`](../plans/os-465e356e.md)
  D9), not this card's;
- **the one-posture bound** — the drill proves the enforced-self-hosted
  posture alone.

## Conformance

- III.O "the compromised-actor drill passes in CI on every release —
  the architecture's own definition of done": met by
  `internal/redteam` (`TestCeilingHoldsAtThePush`,
  `TestOneDerivationLedgerAgrees`, `TestCoverageBothWays`,
  `TestResidualsArePinned`) and the code-ref half's drills in
  `cmd/seed-admit` (`TestCodeRefDefaultBranch`,
  `TestCodeRefContractBranchIsTheClaimHolders`,
  `TestCodeRefTagsAreImmutable`, `TestCodeRefProtectedSurface`,
  `TestCodeRefProtectedSurfaceIsRootOnly`,
  `TestCodeRefRulesAreLoadBearing`). The gate is `make check`, so CI runs
  it with the rest of the suite; there is no separate workflow job to
  skip.
