# forges.md — the per-forge capability table

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> §II.15, III.N row 2. Plan:
> [`plans/os-ad610334.md`](../plans/os-ad610334.md) (Phase 13 item 3).

The core loop runs on any git remote supporting the declared admission
posture; the forge extras — branch and tag protection, the identity that
writes the ledger branch, and the observer's merge facts — are behind
adapters held to one decision table. `Desired` (in `internal/protections`)
derives the four rulesets once, and every forge is held to it. What a
forge cannot express is **named**, never dropped: it appears in the
reconcile report as a manual change the operator applies by hand.

## Declaring the forge

The `admission` block names the forge and its API:

```json
"admission": { "forge": "forgejo", "api": "https://forge.example", "identity": "seed-bot", "...": "..." }
```

- `forge` is `github` (the default when absent, so every existing
  declaration keeps its meaning) or `forgejo`.
- `api` is the forge's API base URL. GitHub's public API is the default;
  Forgejo has none, so a Forgejo declaration must name its instance.
- `identity` is a forge login. The token is never in the declaration; it
  is read from an environment variable (`--token-env`, default
  `GITHUB_TOKEN` for GitHub and `FORGEJO_TOKEN` for Forgejo).

`seed doctor` reports the declared forge; `seed protections plan|apply
--forge github|forgejo` reconciles it (`--forge snapshot` is the
credential-free arm CI and the drills run).

## The four rulesets, per forge

| Ruleset | Desired | GitHub | Forgejo |
|---|---|---|---|
| **ledger branch** | deletion + non-fast-forward + update, sole writer = the admission identity | one ruleset, bypass actor = the identity | a branch protection: deletion and non-fast-forward ride the protection; the sole writer is the push whitelist (`enable_push:false`, `push_whitelist_usernames:[identity]`) |
| **default branch** | deletion + non-fast-forward + required reviews (with thread resolution and code-owner) + required checks | one ruleset expressing all of it | a branch protection: deletion, non-fast-forward and required status checks are expressed; **the pull-request requirement is unexpressible** (see below) |
| **contract branches** (`seed/*`) | deletion + non-fast-forward | one ruleset | a branch protection over the `seed/*` glob |
| **release tags** (`v*` and `seed/v*`, the template's and Seed's; plans/os-2e46aa2f.md) | deletion + non-fast-forward + update, sole writer = the identity | one ruleset, target tag, both patterns | one tag protection per pattern with `whitelist_usernames:[identity]`, every pattern's whitelist read and compared (a widened or emptied one is drift) |

## What Forgejo cannot express

Forgejo's branch protection has **no conversation-thread-resolution gate
and no code-owner-review gate**, and the reconciler reconciles by rule
type against a parameter-exact comparison. So the whole **pull-request
rule** on the default branch is reported **manual**: the operator sets
required approvals, thread resolution and code-owner review in Forgejo's
branch settings and records that they did. Required approvals are still
set best-effort through the API, but the reconciler will not read the
protection back as compliant on gates Forgejo does not enforce — a false
compliance the mutation drill forbids. This is the one row where the two
forges diverge, and it is named in `State.Unexpressible`, so a `plan`
shows it and a deployment cannot skip it silently.

Everything else — the ledger branch's sole writer, the contract-branch
and release-tag protections, the required status checks — is expressed
identically on both forges, from the one `Desired` table.

## The observer

`merge.observed`'s fact is `{merged, pr}`, forge-neutral. `seed merge
observe --forge github|forgejo|snapshot --pr <ref>` fills `merged` from
the forge's pull-request state (`merge_commit_sha` + `merged`), refusing
an unmerged pull request — the ledger fact records a merge, never an
intention. The snapshot arm answers the same shape from a file, so the
drills run credential-free.

From `seed/8` the observer also answers `Checks(pr)`, what the forge
says about the pull request's head
([`observations-forge.md`](observations-forge.md)): the head sha, the
combined check state, the unresolved review threads and the review
state, forge-neutral. GitHub reads the pull request, its check runs,
the GraphQL review-thread query (paged) and its reviews; Forgejo reads
the pull request, the commit's combined status and its reviews, and
its thread count is **nil**, the thread-resolution gap above named in
the observation rather than reported as zero. The snapshot answers the
same shape from the file's `head`, `checks`, `unresolved_threads` and
`review` beside the merge fields. `seed check observe --forge
github|forgejo|snapshot` and the maintenance pass's `--forge` read it.

Both readers live in `internal/externalfact` behind the read-only
`Source` interface ([`external-facts.md`](external-facts.md)), a
different package from the mutating protections adapters above: the
observation component holds a token that reads and issues GET alone
(one named GraphQL query on GitHub), and a lint pins that it stays so.

## The mirror exporters

The issue mirror ([`projections.md`](projections.md), "The mirror") has
its own GitHub and Forgejo adapters under `mirror`, one-way from
the published contracts projection to the forge's issues, opened only
by `seed-mirror` and only through the sealed registry. They share no
code with the protections adapters or the observation sources: the
authority lint keeps the export path and every coordination write path
in different executables. Every registered exporter passes the same
conformance suite, and the drills run against in-process fakes.

## Conformance

- III.N row 2 "at least one non-primary forge is supported by adapters"
  — `internal/protections/forgejo.go`, drilled against a fake Forgejo API
  (`TestForgejoAdapterReconciles`, `TestForgejoObserver`).
- §II.15 "forge extras are adapters" — one `Forge` interface, one
  read-only `externalfact.Source`, one mirror `Adapter`, two forges
  plus the snapshot arm on each, one `Desired` table.
- III.D rows 5 and 6 — the mirror exporters and the component
  boundary, [`projections.md`](projections.md).
- Credential-free CI, live drills opt-in: the fakes run always; a live
  Forgejo drill runs when `SEED_FORGEJO_URL`, `SEED_FORGEJO_REPO` and
  `FORGEJO_TOKEN` are set and skips with the reason named otherwise.
