# Seed

Coordination for multi-agent work on a **signed, append-only ledger**. One
admission rule set, one CLI, and a chain any reader can verify from genesis.

Seed is the successor to [open-seed](https://github.com/shaunlmason/open-seed),
extracted here as an independent repository. The design authority is
[`SEED-NEXT.md`](SEED-NEXT.md) (Part II normative, Part III the conformance
checklist); the build order is [`docs/build-plan.md`](docs/build-plan.md).

## What it is

- **A ledger, not a database.** Every act is a signed event linked to its
  predecessor. `seed ledger verify` checks the whole chain from genesis;
  corruption, reordering, a forged signature and a bad link are all detected
  with positioned reasons.
- **One rule set, three places it runs.** `internal/admit` is imported by the
  cooperative client, the `pre-receive` hook, and the admission service. The
  postures differ in *where* the rules run, never in *which* rules run.
- **Independence enforced, not encouraged.** A verdict signed by anyone in a
  contract's implementing-key set refuses at admission (exit 17), per contract.
- **Receipts that recompute.** A verdict binds `{merge_base, head, diff}` with
  the plan hashed at the merge-base; `seed verdict check` recomputes from the
  submission head and refuses on any divergence (exit 21).
- **Plans that are falsifiable.** A plan lints only if it names a boundary set,
  a retention check, validation commands for both, and an expected diff shape.

## Quickstart

```sh
go build -o bin/seed ./cmd/seed
bin/seed init --ledger /path/to/ledger --key ~/.ssh/id_ed25519   # signed genesis
bin/seed ledger verify --ledger /path/to/ledger
bin/seed doctor                                                   # posture and conformance
```

Deployments declare a posture in `seed.json` at the repository root. There is no
default: an undeclared deployment refuses. The `cooperative` posture needs no
server and states its own consequence out loud, that the security invariant does
not hold against a hostile credential.

## The loop

`seed situation` is the one read every lane orients from. From there:
`offer list`, `claim take`, `plan propose|approve`, `budget reserve|settle`,
`submission make`, `verdict render|check`, `merge request|observe`,
`claim release|park`, `escalation raise`, `message read|send`.

[`AGENTS.md`](AGENTS.md) is the working contract for agents; `docs/handbook.md`
is the longer tour.

## Migrating from open-seed

`seed import --from-open-seed` transforms a v1 export into a genesis chain,
losslessly, with every record given a disposition and a named drop for anything
deliberately not carried. It is drilled against a real export of the predecessor
repository, not a synthetic one.

## Development

```sh
make check
```

Build, vet, gofmt, the suite behind a 90% coverage gate, the performance
budgets, the fixture declaration, the boundary card, and a drift check that
holds the generated documents to the tables they came from.
