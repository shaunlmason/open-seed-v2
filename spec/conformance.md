# Conformance: Part III as a table

Status: normative for `next/**` (plans/os-83bc3d84.md; build plan Phase
13's exit line: "the conformance report shows Part III complete at the
enforced self-hosted posture"; charter III.Q row 3). Last verified:
Phase 13.

The charter's Part III is the list of what conformance means: eighteen
lettered pillars, A through R, of checkbox rows. The phase exit records
in `docs/progress.md` walk those rows, phase by phase, and say for
each whether the tree meets it and by what drill. This document names
the table that carries those verdicts in machine form, the rendering
that publishes it, and the doctor's sentence about it. The table is a
transcription of the records, never a judgment of its own: a row's
status changes when an exit record says so, and the record is the
evidence the row cites.

## The table

`spec/conformance.json` is the strict object `{"pillars":
[{"id", "title", "rows": [{"row", "text", "posture", "status",
"phase", "evidence", "note"}]}]}`:

- `id` and `title` are the charter's lettered heading; `row` counts
  from 1 within the pillar; `text` is the charter's row, verbatim
  after whitespace normalization (continuation lines joined by one
  space).
- `posture` is `any`, `enforced-only` or `mixed`, read off the row's
  `(*enforced-only*)` marker: a marker opening the row marks the whole
  row enforced-only, judged at the enforced postures alone; a marker
  inside the row qualifies one clause, and the row is `mixed`, judged
  at every posture with the doctor naming it at the cooperative one,
  where that clause is not exercised (III.A row 4 requires mutation
  detection everywhere and remote refusal enforced-only; III.L row 5
  makes only admission-only ledger writes enforced-only).
- `status` is exactly one of `met`, `partial`, `routed`, `open`. A
  `met` row names its `evidence` (pull requests and test names, as the
  record cites them); a `partial` or `routed` row's `note` names where
  the rest lives (a phase, an item, the backlog, promotion); an `open`
  row is one no record has met, and its note may say which item lands
  it. III.R's rows are `routed` to the promotion evidence that measures
  each (build plan §5): the note names the measure (the report's lanes,
  flywheel and knowledge sections, the tiers' independence levels, the
  calibration agreement, the shadow run's ledger audit, the first
  external team's outcome) and the row flips to `met` when the
  promotion evidence card records it, so an outcome pillar has a
  closure path rather than a permanent exception.
- `phase` is the phase whose exit record judged the row, as its
  number; empty where none has.

`internal/conformance` parses Part III out of `SEED-NEXT.md` itself
(the `### <letter>. <title>` headings, the `- [ ]` rows and their
six-space continuation lines) and holds the table to it in both
directions: the same pillars in the same order with the same titles,
the same rows with the same text and posture. A row the charter has
that the table lacks, a row the table has that the charter lacks, a
reworded row, a posture that is not the charter's, a status outside
the vocabulary, a `met` without evidence or a `partial` or `routed`
without a note each refuse by name, and the generator refuses to
render a table that does not hold.

## The rendering

`seed docs generate` writes `docs/generated/conformance.md` from
the table: a summary of every pillar's rows by status, then each
pillar's rows with status, phase, the charter's text, the evidence and
the note. `seed docs check` regenerates and diffs it like the other
governed documents ([`simulation.md`](simulation.md), "docs"), failing
`docs_drift` under `make check-next` when the committed file no longer
matches the table. The rendering carries no date and no clock: the
table is the source and the exit records are the evidence.

## The doctor

`seed doctor` reports `conformance` whenever the source tree it reads
(`--repo`, default the working directory) carries the table:

```json
"conformance": {"counts": {"met": 0, "partial": 0, "routed": 0, "open": 0},
                "outstanding_rows": [{"id": "N.2", "status": "open", "phase": "", "posture": "any", "note": "Phase 13 item 3"}],
                "not_applicable_here": ["B.4", "B.5", "O.3"],
                "mixed_here": ["A.4", "A.10", "B.1", "L.5"],
                "complete": false,
                "because": "..."}
```

The counts are over the rows judged at the declared posture. The
charter admits a conformance claim only when every criterion holds,
so `complete` is true exactly when every row judged at the declared
posture is `met`: `open`, `partial` and `routed` rows are all
`outstanding_rows`, each with its status and note, and that list is
what the build plan's Phase 13 preamble asks for, which criteria
remain open by pillar and row with the phase that last judged them.
At the cooperative posture the enforced-only rows are set aside as
`not_applicable_here`, which is the documentation the charter's Part
III preamble requires of a cooperative deployment (the criteria that
do not hold for it), the mixed rows are judged and named under
`mixed_here`, and `complete` is true when every applicable row is met:
the charter defines conformance at the declared posture, so a
cooperative deployment whose applicable rows all hold is complete at
its posture, with the enforced-only rows documented rather than held
against it. The build plan's Phase 13 exit line reads the report at
the enforced self-hosted posture, where every row applies. A table that does not hold against the charter is an
operational failure (`unavailable`, exit 5), never a silently absent
section; a tree without the table has no section.

## Conformance mapping

- III.Q row 3 "docs governed: operator handbook, generated worker docs,
  stamped design docs": the table rendered by the same generator and
  gated by the same drift check as the lifecycle, capability, exit-code
  and lane documents.
- Build plan Phase 13, exit line and preamble: `seed doctor`'s
  `complete` and `outstanding_rows` at the declared posture, and the Phase 13
  exit record flipping the rows it meets with itself as the evidence.
