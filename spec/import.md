# Import: the predecessor's history as a genesis transform

> Charter: SEED-NEXT.md §II.17 ("Importing a predecessor is an adopter
> path … prior tamper-evidence is verified before conversion — and
> import refuses non-empty ledgers. Documented, two-command, drilled
> against a real predecessor fixture"), Appendix D.2 and D.3 ("the
> import boundary is the genesis transform, not per-system
> compatibility code in the core"), Part III.P row 4;
> [`plans/os-cf13fb51.md`](../plans/os-cf13fb51.md).

## Two commands

On the predecessor, anchor the state and export it; on Seed, import
the export into an empty ledger:

```sh
scripts/seed state anchor            # v1: tags the state head seed-anchor/<ts> and pushes
scripts/seed state export > export.json

seed import --from-open-seed export.json --source <clone> \
  --ledger <dir> --artifacts <dir> --key <operator-key> [--anchor <tag>] [--repo <dir>]
```

`--source` is a clone carrying the `seed-state` history and the
`seed-anchor/*` tags; `--repo` is the checkout the cited receipts and
plans are read from (default: `--source`). The import is one process
with no state but what it writes: an empty ledger becomes genesis, the
protocol upgrades to `seed/5`, one `system.imported` record, the
enrollments, the replayed history, and the suspensions; the artifact
store gains every body, handoff, receipt and plan the history cites,
and the manifest. The envelope names the anchor verified, the position
of `system.imported`, the manifest digest, the counts, the identities
and what could not be resolved.

## Anchors first

Nothing transforms until the export is held to the source history:

- the document is `{schema_version: "1.0", backend: "filecards", head,
  files}`, the shape the v1 command prints (wrapped in `document` or
  bare); another schema or backend is `unreadable` (exit 66), an
  input this build does not read;
- `head` must be the commit the anchor names (`--anchor`, or the
  newest `seed-anchor/*` tag in `--source`) or an ancestor of it, or
  the import refuses **`unanchored`** (exit 29): an export nobody
  anchored is a document, not evidence;
- every file in `files` must equal the blob at that path in the tree
  at `head`, with no file in the tree absent from the export, or the
  import refuses **`export_mismatch`** (exit 29) naming the path.

Both refusals are computed before any write, and the drill proves the
ledger directory is untouched by either.

## Genesis import

The target ledger must be empty (`ledger_not_empty`, exit 3, the
genesis refusal). The chain then reads, in order:

| position | record | signer |
| --- | --- | --- |
| 0 | `system.genesis` | the importing operator |
| 1–5 | `system.protocol.upgraded` to `seed/1` … `seed/5` | the operator |
| 6 | `system.imported` `{source: "open-seed", export_head, anchor, manifest}` | the operator |
| … | `actor.enrolled` and `actor.granted` per identity | the operator |
| … | the replayed history, every record admitted through `admit.Check` | the mapped identity |
| … | `actor.suspended` per import-generated identity | the operator |

`system.imported` is `seed/5`'s one addition ([`protocol.md`](protocol.md)):
operator-only, defined from `seed/5` (refused by version below it),
strict `{source, export_head, anchor, manifest}` with `export_head` a
full commit and `manifest` the artifact digest of the mapping
manifest, and admitted once per ledger: a second refuses. It sits
right after the upgrades because the verb is not defined before them,
and before the enrollments so that everything the import wrote is
after the record that says an import happened.

The replay is not a bulk write. Every record is built at the position
it will hold, signed by the mapped identity's key over the same `prev`
the boundary will see, and passed through `admit.Check` against the
context of everything before it — the judgment a live append meets.
A record the boundary refuses fails the import as the boundary's own
envelope, the message naming the export record the event came from;
a history Seed's rules would not admit is not imported by loosening
the rules. The whole chain is admitted in memory before the first
byte is written, then appended in one pass and verified from genesis.

## Provenance is asserted, and the assertion is explicit

v1 identities are names. The importer enrolls one identity per
distinct v1 actor name (run-log actors and card authors) with a key it
generates and holds in memory for the import only, kind from the
table (`shaunlmason` human, `seed-next-implementer` agent,
`seed-maintenance` service, …; a name the table does not know enrolls
as `agent` under its own name and the manifest marks it unknown; the
empty actor is `unattributed`, a service). Grants derive from the
run-log before replay, by rehearsal: the whole transform runs once
over a dry chain that folds the lifecycle without admission, so every
verb each identity will sign — the bridges included — is known, and
each identity is granted exactly the capabilities those verbs consume
(`dispatch` for filing, specifying and unblocking, `claim` for claim
acts, `observer` for observing a merge, `verdict` for a closer that
never claimed the card it closed) and nothing else. No generated key
is ever granted `operator`: the operator-only verbs
(`contract.cancelled`) and the card reconciliations are signed by the
importing operator's own key. One more identity, the import verifier
(a service named `import-verifier`, held apart from the predecessor's
names so an actor of that name stays its own identity), renders the
pass verdict on a card its v1 closer had claimed, since the
independence rule refuses a claimant's verdict and the import does not
manufacture an independence the name never had; it is enrolled only
when the rehearsal had it sign.

Every replayed record carries the entry's original `ts` and is signed
by the mapped identity, so fences, grants and the chain are replayed
under Seed's rules rather than narrated. After the replay every
import-generated identity is suspended, so no imported key holds a
standing a real operator did not grant. The manifest says the keys
were the importer's ([`actors.md`](actors.md): attribution is not
trust); they are never written anywhere.

## The transform is a table

[`import-open-seed.json`](import-open-seed.json), which
`internal/importer` embeds byte for byte (drilled), maps v1 card
states to Seed states, v1 run-log verbs to event families or to named
drops with their reason, v1 actor names to kinds, and the defaults
(`tier: trivial`, `budget: small`, `routing: core`) every imported
contract is filed with. A run-log verb with no row refuses
**`import_unmapped`** (exit 29) before any write; a drop is a row, and
no entry is skipped silently.

| v1 | Seed |
| --- | --- |
| `create` | `intent.filed` — the title as the intent, the routing the card's squad, the table's default tier and budget (`trivial`, `small`: see the bounds below) |
| `promote` | `contract.specified` with acceptance `{ref: "tasks/<id>.md @ <head>", executable: false}` |
| `plan-unblock` | `contract.unblocked`, then `contract.specified` again at `plans/<id>.md @ <commit>` when the plan file is readable from `--repo` (the file stored as an artifact) |
| `claim` | `claim.taken` |
| `release`, `transition` | the table's move from the current state to the target: `claim.released`, `claim.parked` and `submission.made` with the fence and a packet; `contract.blocked`, `contract.unblocked`, `contract.returned`; a release of a claim the actor did not hold is `claim.reaped` |
| `close`, `accept` to done | `verdict.rendered(pass)` → `merge.requested` → `merge.observed` |
| `cancel`, `close` to cancelled | `contract.cancelled`, signed by the operator |
| `comment` | `message.sent` about the card, the body an artifact (inline when short) |
| `attach-evidence`, `record-evidence` | an artifact: the card's evidence block (the receipt file stored beside it when the kind is `receipt`), or the entry itself when no block matches |
| `unblock` | `contract.unblocked` where v1 recorded a transition; an entry v1 marked `transitioned: false` changed nothing and becomes nothing, noted |
| `lease-renew`, `blocker_resolved`, `halt`, `state-resume`, `state-repair` | drops, each with its reason in the table (`blocker_resolved` is the dependency bookkeeping beside the unblock that records the transition; `decision.recorded`, which the plan named for it, answers a standing escalation no v1 card raised) |

A v1 move that does not fit Seed's table in one step is bridged along
the shortest lifecycle path (a claim on a card the log never filed is
filed and specified first; a cancel from `in_progress` releases
first), and the bridge is noted on the entry's disposition. A card
whose run-log leaves it in a state other than its frontmatter declares
is reconciled to the declared state by the operator, noted on the
card's disposition; the drill asserts that every imported contract
folds to the state its card declares.

**Packets.** Every claim exit carries the four-part packet
([`lifecycle.md`](lifecycle.md)), synthesized once per card from the
handoff's mechanical sections: the task line as the acceptance, the
workspace anchor as a zero-length base, the handoff as an anchored
ref, and a finding whose pointer is the handoff's artifact digest —
never invented prose. A card with no handoff carries a packet that
says so, anchored at the export head.

**The verdict.** v1 recorded no verdicts; its receipts are files the
run-log cites. A done card's chain is a pass verdict from the mapped
verifier over the stored receipt's digest when the card attached one,
and otherwise over an import note — an artifact saying the card
attached no receipt — with the disposition noted. The plan's D7 named
`merge.overridden` for this case; that verb overrules a standing fail
verdict ([`verdicts.md`](verdicts.md)) and nothing failed here, so the
honest record is a pass over a note that says what it is. `merged` is
the squash commit naming the card's pull request when `--repo` has it,
else the commit that added the card's receipt, else the export head,
listed under `unresolved` in the manifest and the envelope.

## Losslessness is a check

The mapping manifest (`seed-import-manifest/0`), an artifact whose
digest `system.imported` cites, gives every export record — each
run-log entry, each card, handoff and mail file — exactly one
disposition: the events it became (positions, verbs, signers), the
artifact it became, or the drop row it matched, with a note on
bridges, reconciliations and what did not resolve. It lists every
identity (name, kind, fingerprint, grants, enrolled and suspended
positions, whether the table knew the name) and the counts; the drill
asserts every record has one disposition and the counts agree. The
positions are exact: the transform runs twice, once citing a
placeholder digest to fix the positions and once citing the manifest's
real digest, and the import refuses if any position moved between the
two.

## The fixture

[`fixtures/import/open-seed/`](../fixtures/import/open-seed/README.md)
is this repository's own v1 state at a named anchor: `export.json`
and `seed-state.bundle`. The drill (`internal/importer/fixture_test.go`)
clones the bundle, imports the export into an empty ledger, verifies
from genesis, folds every contract to its card's declared state,
checks the manifest against the artifact store, and the synthetic
drills prove the refusals: a tampered export, a head no anchor covers,
a non-empty ledger, an unmapped verb. `make fixture-import` regenerates
the fixture from the live repository at the newest anchor.

## Bounds

**Every imported contract is `trivial`.** The plan's D4 named the
`standard` tier; above `trivial` the boundary's plan gate refuses a
claim on a contract with no approved plan record, and the tier's
independence level would refuse the verdicts, so a replayed history
at `standard` would not admit. The card's v1 priority and squad are
kept on the card artifact the acceptance cites, and the routing is
the squad. A deployment that wants imported contracts under its
guardrails re-specifies them after the import, as it would any
contract.

Only open-seed's export is understood; another predecessor is the
same table with different rows and the same engine, and no per-system
code enters the core (Appendix D.3). Imported contracts in `review`
or `in_progress` at export time replay to that state and are the
deployment's to finish or reap. Cards v1 let a name move without
holding the claim are signed by the holder's key (the exit) or become
`claim.reaped` (a release), noted. The import is a transform of what
v1 recorded, never a reconstruction of what it did not.

## Conformance mapping

- III.P row 4 "Migration from a predecessor is documented,
  two-command, and drilled against a real fixture" — the two commands
  above, the fixture, the drills, this document.
- Build plan §5 criterion 3 (the migration gate) — drilled against a
  real export of this repository's v1 state.
