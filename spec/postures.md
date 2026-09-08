# Postures: the three admission deployments, the declaration, the service, and the protections

> Charter: SEED-NEXT.md §II.2 ("Postures": every deployment MUST
> declare which of three named admission deployments it runs), §II.14
> ("Forge-side protections as declared desired-state, reconciled by
> command"), Part III.B rows 1, 3 and 4, III.L row 5, III.P row 2;
> [`plans/os-5c8a312c.md`](../plans/os-5c8a312c.md). The
> enforced-self-hosted deployment itself — the `seed-admit` hook, its
> code-ref half and the `SEED_PUSHER` identity — is
> [`admission.md`](admission.md)'s and is referenced here, not restated.

## The three postures, and where the rules run

One rule set, three places it runs. `internal/admit` is imported by the
cooperative client, by the pre-receive hook, and by the admission
service; the postures differ in **where** the rules run, never in
**which** rules run, and the one-derivation drill holds all three to
the same answer on the same corpus (below).

| posture | who writes the ledger ref | what refuses a hostile credential | the honest consequence |
|---|---|---|---|
| `enforced-self-hosted` | anyone with push access, through the `seed-admit` pre-receive hook, which is the ref's gate | the hook, on the git server | the charter's reference deployment; [`admission.md`](admission.md) |
| `enforced-forge-hosted` | the admission identity alone: `seed-admit serve`, which actors **propose** to | the forge's protections, which make that identity the branch's sole writer, and the service's judgment | server hooks are not needed; the forge's ruleset is trusted infrastructure (§I.2 assumes it uncompromised) |
| `cooperative` | every writer, self-validating | nothing on the server | `posture.Consequence`, printed by the doctor verbatim: the security invariant does not hold |

Every deployment declares one in `seed.json` at the repository root
(`posture.DeclarationPath`), read by the doctor and the remote verbs
from the working tree and by the hook from the default branch's tip.
The declaration is deployment state, never ledger content.

## The declaration

```json
{
  "posture": "enforced-forge-hosted",
  "admission": {
    "endpoint": "https://admit.example.org",
    "identity": "app:4242",
    "ledger_ref": "refs/heads/seed-ledger",
    "checks": ["check", "verify"],
    "reviews": 1,
    "owners": ["@org/governance"]
  },
  "protected": ["spec/", "Makefile", ".github/workflows/"]
}
```

Strict: unknown fields, trailing data and unknown postures refuse
(`posture_invalid`, exit 13). The `admission` block is **required**
under `enforced-forge-hosted` and **refused** under the other two — a
block nothing consults is a declaration that lies about its shape.
Within it: `endpoint` is an http(s) URL the service answers on;
`identity` names the forge identity the service's credential belongs
to, in the form the forge adapter takes (below); `ledger_ref` is a
**branch** (`refs/heads/...`, default `refs/heads/seed-ledger`),
because forges protect branches and tags and nothing under
`refs/seed/*` — the hook posture keeps `refs/seed/ledger` untouched, and
the two are one parameter of every ledger seam, never two code paths;
`checks` and `reviews` are the default branch's required status checks
and review count; `owners` are the forge identities that review the
protected surface. `protected` is the protected surface as
repository-relative path prefixes ([`admission.md`](admission.md));
`ProtectedSurface()` always includes the declaration itself.

## The preseed

One declarative file bootstraps a deployment (charter §II.17, Appendix
D.1; [`plans/os-0d4f2af3.md`](../plans/os-0d4f2af3.md)): the
declaration above, completed. Four blocks join `posture`, `admission`,
`protected` and `checkpoints`, each undeclared when absent and never
defaulted:

```json
{
  "protocol": "seed/4",
  "governance": {"root": "<the root key's fingerprint>", "owners": ["@org/governance"], "change_process": "pr+owner-review"},
  "guardrails": {
    "squads": {"core": {"default": "standard", "max_agent": "standard"}},
    "paths": [{"prefix": "internal/admit", "min": "critical"}]
  },
  "teams": {"squads": [{"name": "core", "lanes": ["dispatcher", "planner", "implementer", "verifier", "curator", "maintenance", "supervisor", "observer"]}]}
}
```

`protocol` is the version the deployment activates through (the
register in [`protocol.md`](protocol.md)); `governance` names the root
and the one change process the charter names for the protected
surface; `guardrails` are tiers per squad (`default`, and `max_agent`,
the highest tier an agent-kind key may claim) and per path (`min`, the
tier a contract touching the prefix files at); `teams` are the squads
`routing` names and the manifests each runs. Keys stay out of the file:
enrollment is the operator's signed act after init, in Appendix D.1's
order.

**`seed init --preseed seed.json --ledger <dir> --key <root key>`** is
idempotent. On an empty ledger it writes genesis naming the key as the
root — refusing `preseed_drift` before genesis when `governance.root`
names another — and activates every protocol version up to `protocol`
in order under the root. A second run appends nothing (`unchanged:
true`). A chain the file contradicts refuses `preseed_drift` (exit 28
`drift`) naming the field and both values, the chain untouched: a
different root, a protocol below the chain's. A declared protocol above
the chain's is the one drift init resolves, by appending exactly the
missing activations; `seed preseed check` names it as pending.

**`seed preseed check --config seed.json [--ledger <dir>] [--lanes
<dir>]`** is the same comparison with no writes, and without `--ledger`
it lints the file alone — what `make check-next` runs against the
fixture deployment's declaration: tiers in the vocabulary
([`tiers.md`](tiers.md)), guardrail squads among the declared teams,
team lanes among the shipped manifests, the protocol in the register,
and the protected surface complete. **Complete** means every member the
charter requires is on it (`RequiredProtected`, compared by prefix):
`spec`, `internal/admit`, `internal/transition`,
`internal/keyring`, `internal/verdict`, `internal/seal`,
`internal/eval`, `evals`, `internal/curation`,
`knowledge/lessons`, `lanes`, `cmd/seed-admit`,
`cmd/covergate`, `Makefile`, `.github/workflows`, `scripts`, and
the declaration itself by construction. The supervisor's policy lives
in this file's `guardrails` block; the sealed keyring is a recipient
set derived from ledger grants and has no path. An omission refuses
`preseed_incomplete` (exit 13 `posture_invalid`: a judgment on the
declaration's content, distinct from drift against a chain) naming the
member.

**The guardrails are enforced, not prose.** The agent claim ceiling is
an admission rule (`policyRules`, `internal/admit`): a `claim.taken`
signed by a key whose roster kind is `agent` or `service` on a
contract whose tier is above its squad's `max_agent` refuses
`tier_above_ceiling` (exit 3) naming the kind, the squad, the tier and
the ceiling; a `human` key is not ceilinged — the charter's agent-only
guardrail reading the distinction the roster records (III.E row 9). The
routing rule holds `intent.filed`'s `routing` to the declared squads
(`routing_unknown`, exit 3), closing the residual [`tiers.md`](tiers.md)
and [`lanes.md`](lanes.md) named. Both read the declaration as
**admission policy, not chain validity** — the tiers precedent: the
cooperative client reads `seed.json` from its working tree (`--config`,
`$SEED_CONFIG`, `./seed.json`), the hook reads it at the default
branch's tip, a raw-pushed record that would have refused folds as
filed, every chain verifies byte for byte, and no protocol version
bumps. With no declaration both rules are no-ops: today's behavior. A
declaration that is present but does not parse is not "no
declaration": the hook fails closed, refusing the ledger push and the
code push alike (the code half already refuses agent code pushes until
an operator repairs it), so one broken declaration cannot split the
boundary into two (card os-0f924157).

**The path floor is enforced at the plan lint and at the render**, never
at admission, because a path is a fact about a repository and admission
reads the ledger and the declaration alone. `seed plan lint <file>
--config seed.json --tier <tier>` holds the plan's "File Scope" section
to the floors ([`plans.md`](plans.md)); `seed verdict render --config
seed.json` holds the receipt's changed files to them; both refuse
`under_tiered` (exit 18 `ungated`'s family: content whose gate is short)
naming the path and the two tiers, with the same words.

**The capability audit** (`TestCapabilityAuditOfTheShippedManifests`,
`cmd/seed`) derives from the shipped manifests that the manifests
holding `operator` — the standing the hook lets update the default
branch — are exactly `maintenance`, and fails by name on a planted
second holder; the protected surface itself is the governance root's
alone under the hook's rule ([`admission.md`](admission.md)). The
test-content residual stands in the charter's words: ordinary test
content outside the surface remains in an implementer's write scope,
and diff-versus-plan review plus sealed checks are the mitigations.

**Human/agent metrics.** The report's `lanes` section (version 15,
[`projections.md`](projections.md)) splits the re-triage and
unedited-approval figures by the acting key's roster kind (`by_kind`),
so an operator can see whether the agents or the humans are the ones
re-triaging.

The doctor reports every block as declared or not (`preseed`).

**The racing block.** A squad's `guardrails.squads.<name>.racing`
(`{"racers": N, "cost": "…"}`) is its explicit opt-in to racing mode
([`lifecycle.md`](lifecycle.md), "Racing"): `racers` two or more,
`cost` a non-empty sentence in the operator's words. Both are held at
load (`posture_invalid`) and at `seed preseed check`
(`preseed_incomplete`); an absent block is exclusivity, never a
default race. The boundary reads the block through the same
declaration the ceiling and routing rules read.

**The federation block.** `federation.remotes` is the list of other
ledgers this deployment reads ([`requests.md`](requests.md),
"Federation"): each `{"name", "remote", "ref"}`, the name one token
unique in the list, the remote non-empty, the ref optional
(`refs/seed/ledger` when absent). Strict and read-only: the block
names what `seed federation report` fetches and verifies under each
remote's own keyring; nothing in the declaration lets a remote write
here or this deployment write there. Held at load (`posture_invalid`).

**The boundary block.** `boundary` is `{"accepts": [kinds],
"ingress": "<remote or endpoint>"}` ([`boundary.md`](boundary.md)):
the request kinds this deployment takes through its ingress and what
a request is filed through — what the capability card publishes and
what the `boundary` admission rule holds `request.filed` to. Strict:
at least one distinct kind, a non-empty ingress; held at load
(`posture_invalid`). Absent, the deployment publishes no card and
every request kind admits as before. The card itself is checked in at
`boundary/card.json` and `make check` re-renders and diffs it.

**The approvals block.** `guardrails.approvals` is the require-approval
mode of per-verb policy ([`protocol.md`](protocol.md), "Per-verb
approval"; charter §II.14): a list of `{"verb", "actors"?, "kinds"?,
"min_tier"?}` entries, one per verb, each naming the key fingerprints
and the roster kinds it reaches (neither: every non-human kind) and
the tier floor (absent: every tier) at and above which an act of the
verb needs an open `approval.granted`. The shape is held at load
(`posture_invalid`: a verb twice, an empty verb, an actor or a kind
twice); the vocabularies at `seed preseed check` (`preseed_incomplete`:
a verb outside the catalog or one of the three approval verbs, an actor
that is no fingerprint, a kind outside `human`, `agent`, `service`, a
tier outside the vocabulary).
The boundary reads the block through the same declaration the ceiling
and routing rules read, as policy: `approval_required` (exit 3) names
the request to file, and no chain changes meaning because of it.

## The proposal protocol

Under the forge-hosted posture an actor's credential can fetch the
ledger branch and cannot push it. The client's append loop is unchanged
in every step but the last: fetch, materialize, verify from genesis,
re-link, re-sign, self-validate through `admit.Check` — and then
**propose** instead of push (`gitref.Proposer`, wired by
`openRemoteSession` from the declaration). The cooperative half is what
every posture keeps; only the write moves.

`POST /propose` with `{"ref": "<ledger ref>", "records": [<record>, …]}`
in [`protocol.md`](protocol.md)'s record encoding, in chain order,
already linked to the tip the proposer fetched and signed by the actor.
The answer is the `seed-envelope/0` the CLI would have printed — the
boundary's own code, message and position stamp — and the HTTP status
is transport:

| status | meaning | the proposer's move |
|---|---|---|
| 200 | admitted: `result.position`, `result.commit`, `result.appended` (the hashes), `result.verbs` | done |
| 409 | the ref moved: the proposal links to a tip that is no longer the tip (`contention`, refined `non_fast_forward`) | re-link and propose again — `AppendLoop`'s own retry |
| 422 | refused: the envelope carries the refusing rule's exit and code (`classification_refused`, `out_of_grant`, `halted`, `chain_invalid`, …) | render it; the same code the hook posture would have given |
| 503 | the service cannot reach or write the remote (`unavailable`, `remote_rejected`, `head_regression`) | a deployment problem, never the proposer's |

`GET /healthz` is the probe: `{remote, ref, position, tip}` for the last
proposal the instance admitted (`position` null before any), read by
`seed doctor --probe`. No authentication of proposers at the transport:
the record's signature is the authentication, exactly as at the hook.

The service (`seed-admit serve --remote <repo> [--ref <branch>]
[--state <dir>] [--listen <addr>] [--announce <file>]`) is the same
binary as the hook. For each proposal it fetches the tip into its clone,
materializes it, refuses a stale `prev` as the race it is before
anything is appended, appends the records onto the materialized copy,
commits the candidate in its own git dir, judges old→candidate with the
very `admitUpdate` the hook runs, and pushes only what was admitted,
fast-forward, under the identity its git credential carries. It signs
nothing (records are actor-signed; a re-signing service would erase the
one thing that makes a race detectable), retries nothing (a lost push
race is 409 too), and keeps no state but its clone and its persisted
verified head: a replacement instance from a fresh state dir judges
every proposal the same (the kill-and-replace drill). Proposals through
one instance are serialized; two instances race at the remote like any
two writers. An empty ledger branch admits a genesis proposal only,
which is how a forge-hosted deployment is initialized: nobody but the
service can write the branch, so genesis goes through the door too.

## Protections as declared desired state

The reconciler derives the forge's desired state from the declaration
and the charter's rules, with no second source
(`internal/protections.Desired`):

| ruleset | refs | rules |
|---|---|---|
| `seed-ledger` | the declared ledger branch | updates by the admission identity alone; no force-push; no deletion |
| `seed-default-branch` | the repository's default branch, **read from `HEAD`**, never declared | the declared `checks` required; `reviews` approving reviews; review-thread resolution; code-owner review when `owners` are declared; no force-push; no deletion |
| `seed-contract-branches` | `refs/heads/seed/*` | no force-push; no deletion |
| `seed-release-tags` | `refs/tags/v*` and `refs/tags/seed/v*` (the template's releases and Seed's, plans/os-2e46aa2f.md) | create-only: no update, no deletion |

Beside the rulesets: `CODEOWNERS` rendered from the protected surface
(every prefix, the declaration itself included, owned by `owners`) and
written to the working tree for a reviewed PR, never pushed; and the
CI-identity lint, which names any workflow under `.github/workflows`
that is scheduled and grants itself `contents: write` — the job the
charter says may never push to the default branch. Foreign rulesets are
left alone; a Seed-named ruleset the declaration no longer wants is a
deletion.

`seed protections plan --config seed.json --forge snapshot --snapshot
<file> [--repo <dir>]` reads the forge and names every difference by
kind and ruleset: exit 0 with `drift: 0`, else **exit 28 `drift`** (a
new code: a declared desired state and an observed state differ, the
message naming each difference) refined `protections_drift`, so a CI
job can gate on it. `apply` performs the plan, writes `CODEOWNERS`, and
re-reads to a fresh plan. A rule the forge declares it cannot express is
reported as `manual` with the click it needs and counted separately
from drift — never dropped — which is the charter's "risk-limit honesty
where enforcement is impossible" as a line in an envelope.

Two forge adapters. `snapshot` reads and writes the forge's state as a
JSON file: the credential-free arm CI and the drills run, and the shape
a deployment records its forge's state in by hand. `github` speaks to
the REST rulesets API with `net/http` and a token from `$GITHUB_TOKEN`
(`--token-env` names another variable); bypass identities take the
actor forms the API takes and nothing the core resolves — `app:<id>`,
`team:<id>`, `deploy-key`, `org-admin` — and any other form refuses by
name before anything is written. The `Forge` interface is what
non-primary forges (Phase 13) implement.

## The doctor

Under `enforced-forge-hosted` the doctor reports the block (endpoint,
identity, ledger ref, checks, reviews, owners), probes the service when
asked (`--probe`: unavailable by name when nothing answers, a warning
when the service serves a ref the declaration does not name), and
reports protections drift against a snapshot (`--current <file>
[--repo <dir>]`, exit 28 when any). The gap sentence that stood here
until the service landed is gone with the gap. The cooperative
consequence is unchanged.

## What the drills prove

- **The service admits, refuses and reports the race** (`cmd/seed-admit`,
  `TestServiceAdmitsRefusesAndTheForgeRefusesTheActor`,
  `TestServiceReportsTheRaceAndTheLoopRetries`,
  `TestServiceProposalShape`): a valid proposal advances the branch by
  the service's hand alone; a hostile one refuses with the boundary's
  own code stamped at the tip and the branch unmoved; the actor's direct
  push is refused by the forge's rule (III.B's attempted-direct-push
  row under this posture); a stale `prev` is 409 and the proposer's loop
  lands second; the proposal endpoint is strict.
- **One derivation, three callers**
  (`TestServiceAgreesWithTheBoundaryAndTheHook`): every row of the
  shared adversary table yields the same code through the service and
  the in-process boundary, and the hook names the same rule.
- **Statelessness** (`TestServiceKillAndReplace`): a replacement
  instance from a fresh state dir judges every row identically, admits,
  and the chain verifies from genesis.
- **The client is posture-driven** (`cmd/seed`,
  `TestForgeHostedRemoteVerbsPropose`,
  `TestOtherPosturesIgnoreTheAdmissionBlock`): every remote verb
  proposes under the declaration and pushes under the others; a dead
  endpoint is unavailable by name; an invalid or explicitly absent
  declaration refuses before any transport.
- **The reconciler** (`internal/protections`, `cmd/seed`):
  desired state from the declaration; plan before apply; manual rules
  named; the stray deleted and the foreign left alone; CODEOWNERS
  written and drifting; the scheduled writer found; the GitHub adapter
  creating, updating by id, deleting, with the token required.

## The fixture's model of the forge

A bare repository on a local path has no notion of who pushes, so the
fixture's forge is a bare remote with **no admission hook** whose
pre-receive refuses any update to the ledger branch unless
`SEED_PUSHER` names the admission identity — a stand-in for the forge's
ruleset, labeled as such in the drill. In production the forge
authenticates the service's credential and nothing sets `SEED_PUSHER`.

## Residuals, stated

- The forge posture cannot know which key holds a claim: contract-branch
  exclusivity stays Seed's fence at admission (and the hook posture's
  code-ref half); on the forge, contract branches are protected from
  force-push and deletion only.
- Hosting, TLS and the credential the service pushes with are the
  deployment's. Give the service a git credential that may write the
  ledger branch and nothing else; never an actor key.
- Only the GitHub adapter ships. The identity forms are the API's; a
  login that is not an installed app, a team, a deploy key or the
  organization's admins is not an actor GitHub's rulesets can bypass
  with, and the adapter says so rather than guessing.

## Conformance mapping

- III.B row 1 (the validator the ledger ref's sole writer, enforced
  only) — the service under the forge's sole-writer rule, drilled with
  the actor's direct push refused.
- III.B row 3 (all three postures implemented; every deployment
  declares; cooperative's consequence stated) — the declaration, the
  service, the doctor.
- III.B row 4 (actor credentials cannot write the ledger ref directly,
  verified by an attempted direct push) — this posture's arm of the
  drill, beside the hook's.
- III.L row 4 (per-verb policy governs the machine-protocol surface
  with attributable approvals) — the approvals block, the third mode
  beside the grant table and the ceiling and routing rules;
  `TestPreseedCheckLintsTheApprovalsBlock` (`cmd/seed`) for the lint.
- III.L row 5 (forge protections declared and reconciled: required
  checks, admission-only ledger writes, immutable tags; scheduled/CI
  identities least-privilege) — `seed protections`, the four rulesets,
  the CI-identity lint.
- III.P row 2 (the validator ships in hook and service form, both
  stateless, both rebuildable from a clone) — `seed-admit` and
  `seed-admit serve`, the two kill-and-replace drills.

## The declared forge (Phase 13 item 3)

The enforced-forge-hosted `admission` block names its forge:
`admission.forge` (`github` default, or `forgejo`) and `admission.api`
(the forge's API base; required under `forgejo`). See
[`forges.md`](forges.md) for the per-forge capability table.
