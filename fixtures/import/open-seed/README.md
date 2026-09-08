# The open-seed import fixture

This directory is this repository's own v1 state at a named anchor
(`plans/os-cf13fb51.md` D6; `next/spec/import.md`): what the two
commands of the migration path consume in CI, with no network.

| file | what |
| --- | --- |
| `export.json` | the v1 store as one lossless document: `{document: {schema_version, backend, head, files}}`, the shape `scripts/seed state export` prints, at the commit `seed-anchor/20260903T014125Z` names |
| `seed-state.bundle` | a `git bundle` of the `seed-state` history with every `seed-anchor/*` tag up to that anchor, so the import verifies the export against the anchored tree offline |

## The two commands

On the predecessor (v1), anchor and export:

```sh
scripts/seed state anchor          # tags the state head as seed-anchor/<ts> and pushes
scripts/seed state export > export.json
```

On Seed, import into an empty ledger:

```sh
seed import --from-open-seed export.json --source <clone> \
  --ledger <dir> --artifacts <dir> --key <operator-key> [--anchor <tag>] [--repo <dir>]
```

`--source` is any clone carrying the `seed-state` history and the
anchor tags (a clone of `seed-state.bundle` is one); `--repo` is the
checkout the cited receipts and plans are read from (default:
`--source`). The drill (`internal/importer/fixture_test.go`) runs the
second command against the first's output: it clones the bundle,
imports `export.json`, verifies the chain from genesis, folds every
contract to the state its card declares, checks the manifest's
losslessness, and proves the four refusals.

## Regenerating

`make fixture-import` runs `regenerate.sh`: it requires the newest
`seed-anchor/*` tag to name the current `seed-state` head (anchor
first, then regenerate) and writes both files from the live
repository, so the gate stays real as the v1 history grows. With
`--at-anchor` it writes the export document from the anchored tree
instead, for a head that has moved past the newest anchor; the
document is the same tree the v1 command would have exported at that
instant, which is what the import verifies.

This fixture was written with `--at-anchor` at
`seed-anchor/20260903T014125Z` (commit `897a5c95…`).
