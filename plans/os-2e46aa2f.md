# Plan: next — the Seed release workflow (os-2e46aa2f)

Build plan §5 names the distribution step's preconditions: self-hosting
held for a stated period, "a released Seed binary with checksums and
provenance (charter III.P row 1's one residual today: the binary is
built from source and is not yet a released artifact)", and a README a
team can adopt from. Cutting the release is the operator's act at the
distribution step, and the autonomy contract reserves publishing to a
human; the workflow that cuts it is agent work, and none exists. This
card adds it and cuts nothing. Tier: standard (a workflow on the
protected surface, owner review regardless). Deps: none.

## What the tree actually shows

- **No release path for Seed.** `next/internal/version.Version` is the
  constant `0.0.0-dev`, "pre-release until spin-out cuts the first
  tagged release"; `seed version` prints it; nothing builds or
  publishes the binaries. III.P row 1 is `met` with that residual named
  in its note.
- **The engine repository shows the discipline.** open-seed-engine's
  `release` workflow mints the tag itself at HEAD in-runner, so the tag
  and the released commit cannot disagree and no contributor needs
  tag-push rights; goreleaser builds and publishes;
  `actions/attest-build-provenance` attests `checksums.txt`.
- **Tags are already namespaced.** The template releases under
  `refs/tags/v*` (immutable under the protections' `seed-release-tags`
  rule and v1's tag rule), and the v1 state anchors under
  `refs/tags/seed-anchor/*`. Seed's releases need a namespace of their
  own so a Seed tag can never be read as a template release.
- **The CI-identity lint binds scheduled writers.**
  `TestTreeWorkflowsHaveNoScheduledWriters` holds every scheduled
  workflow to `contents: read` outside v1's maintenance lane; a
  dispatch-only workflow may write.

## Design decisions (binding for this task)

- **D1 — dispatch only, the operator's hand.** `seed-release.yml`
  triggers on `workflow_dispatch` alone, with one input, `version`
  (semver without the `v`). No schedule, no push trigger, no tag-push
  trigger: a release is the distribution step's act and a human runs
  it. Permissions `contents: write` (the tag and the release),
  `id-token: write` and `attestations: write` (the provenance).
- **D2 — the tag is minted at HEAD, in the `seed/v` namespace.** The
  workflow refuses a version that is not semver or whose tag already
  exists, tags the checked-out HEAD as `seed/v<version>` under the
  actions identity, and pushes the tag; the released commit and the tag
  cannot disagree.
- **D3 — a plain matrix build, no goreleaser.** `go build` from `next/`
  for linux, darwin and windows on amd64 and arm64 with `-trimpath` and
  `-ldflags "-s -w -X …/internal/version.Version=<version>"`, two
  binaries per target (`seed`, `seed-admit`), one archive per target
  (`.tar.gz`, `.zip` on windows) named `seed_<version>_<os>_<arch>`,
  and `checksums.txt` of sha256 sums. goreleaser's tag handling assumes
  an unprefixed semver tag and its monorepo prefix is a paid feature,
  so the namespace D2 needs is cheaper to keep in twenty lines of bash
  than to work around. `internal/version.Version` becomes a `var` so
  the stamp lands; its default stays `0.0.0-dev`.
- **D4 — published and attested, every action hash-pinned.** `gh
  release create` on the tag with the archives and `checksums.txt`,
  then `actions/attest-build-provenance` over `checksums.txt`, the same
  attestation the engine ships. Every third-party action the workflow
  uses is pinned to a full commit SHA with the tag it resolves from in
  a trailing comment (charter §II.17: everything executable
  hash-pinned; a job holding OIDC, attestation and write privileges
  must not resolve its provenance step through a mutable tag), and the
  drill refuses any `uses:` that is not a local path or a 40-hex SHA.
  The release notes name the commit and the protocol version
  register's newest entry.
- **D5 — held by a drill.** `TestSeedReleaseWorkflowIsDispatchOnly`
  (`internal/protections/tree_workflows_test.go`) reads the workflow
  and asserts: the only trigger is `workflow_dispatch`; the permissions
  are exactly the three D1 names; the tag namespace is `seed/v`; the
  attestation step is present; and `LintWorkflows` reports nothing for
  it. A planted `schedule:` trigger with `contents: write` is a finding.
- **D6 — the docs say where the binary comes from.** The handbook's
  Install section names the release (the archive, the checksum, the
  attestation to verify) beside `go run ./next/cmd/seed`; the packet's
  Distribution precondition is unchanged in wording and stays unmet
  until a release exists, which this card does not cut.
- **D7 — bounds.** No release is cut, no tag is pushed, `VERSION` and
  `.seed/engine.lock` are untouched (they are the template's and the
  engine's), the template's `v*` tags and the anchors are untouched,
  no other workflow changes.
- **D8 — the namespace is protected before it is used.** The hook
  posture already refuses an update or deletion of any tag
  (`cmd/seed-admit/coderef.go`, every `refs/tags/*`), but the forge
  reconciler's `seed-release-tags` ruleset named `refs/tags/v*` alone,
  so under the forge-hosted posture a `seed/v*` tag would inherit no
  immutability and a credential with ordinary tag-write access could
  retarget a released tag, breaking the release, source and provenance
  binding §II.14 requires. The ruleset gains `refs/tags/seed/v*`
  (`internal/protections.Desired`), the reconciler drills and the
  `postures.md` table follow, and the release drill asserts the
  namespace it tags is one the desired state protects.

## Steps

1. D1 to D4 in `.github/workflows/seed-release.yml`; `Version` a var.
2. D5's drill; `scripts/validate.sh`'s YAML parse and `make check`
   green.
3. D6 in `next/docs/handbook.md`; `next/docs/progress.md`,
   `next/docs/decisions.md`, `memory/LEARNINGS.md`; receipt; evidence;
   review.

## File Scope

- `.github/workflows/seed-release.yml` (new)
- `next/internal/version/version.go`, `next/internal/version/version_test.go`
- `next/internal/protections/protections.go`, `next/internal/protections/*_test.go`
- `next/spec/postures.md` (the rulesets table)
- `next/docs/handbook.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-2e46aa2f.json`

Nothing else. NOT `.github/workflows/*.yml` other than the new file,
NOT `VERSION`, NOT `.seed/**`, NOT `next/spec/**`, NOT
`next/docs/promotion.md`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. The workflow exists, triggers on `workflow_dispatch` alone, mints
   `seed/v<version>` at HEAD, builds the six targets with the version
   stamped, publishes the archives and `checksums.txt` as a release,
   and attests provenance with every third-party action pinned to a
   commit SHA; `TestSeedReleaseWorkflowIsDispatchOnly` holds each of
   those to the file, fails on a planted schedule and on a planted tag
   reference, and asserts the tag namespace is one the reconciled
   desired state protects.
2. `seed version` still prints `0.0.0-dev` from source, and a build
   with the ldflag prints the stamped version (drilled).
3. The handbook's Install section names the release and how to verify
   it.
4. `make check` green (the YAML parse in `validate`, the CI-identity
   lint); no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- Every other workflow is byte-identical to `main`; the template's
  release path and the engine pin are untouched; III.P row 1's status
  and note are unchanged (the residual closes at the distribution
  step, not here).

## Validation Commands

- Boundary: `cd next && go test ./internal/protections/ ./internal/version/ -count=1`
- Boundary: `sh scripts/validate.sh`
- Retention: `make check` (exit checked separately from any pipe)

## Expected diff shape

Added: `seed-release.yml` (roughly 90 lines). Modified: `version.go`
(one keyword), `tree_workflows_test.go` (one drill, roughly +60),
`handbook.md` (one paragraph), the three docs files, the receipt. No
other file.
