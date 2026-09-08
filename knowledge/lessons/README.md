# Validated lessons

The third stage of the curation pipeline (SEED-NEXT.md §II.12;
`spec/curation.md`): one file per lesson, merged by PR, and never
written by a lane. A lesson reaches this directory only through a PR
the governance root reviews, and the ledger carries the observation of
that merge (`curation.lesson.promoted`, the observer's), citing the
admitted hypothesis it promotes. Rollback is a revert, because it was a
PR.

The store is empty until a hypothesis survives item 2's promotion gate;
this file is the contract every lesson file honors.

## Frontmatter

A lesson opens with a YAML frontmatter block naming, each with a
non-empty value:

```
---
hypothesis: h-0123456789ab@42
applies-when: the condition under which the claim holds
support: c-1@17, c-2@31
provenance: plans/os-xxxxxxxx.md @ 0123456
last-validated: 2026-09-02
expires: 2026-12-02
---
```

- `hypothesis` cites the admitted `curation.hypothesis.proposed` by
  subject and position: the stage the lesson was promoted from.
- `applies-when` is the condition the claim holds under, the field the
  claim-time surfacing reads.
- `support` lists the observations the hypothesis cited.
- `provenance` lists the anchored artifacts the lesson was validated
  against.
- `last-validated` and `expires` are the dates the expiry and
  revalidation machinery reads.

Item 1 lints the presence of these keys (`curation.Lint`, applied to
every file here by drill); item 2 lints their content against the
promotion gate; item 4 reads the dates, and lints the structure and
the store's dedup.

## Body

After the frontmatter, the body carries these sections, in this
order (`curation.LintStructure`):

```
## Claim
## Evidence
## Applies when
```

The frontmatter carries exactly the keys above and no other. The
store holds one file per hypothesis (`curation.LintDuplicates`):
revalidation moves a file's stamps forward in place, so a second file
citing the same hypothesis is a duplicate, and the lint names it.

## Expiry and retirement

A lesson is expired at an instant at or past its `expires`; an expired
lesson leaves the surfacing set and is flagged stale wherever the
store is shown. Revalidation is a PR moving `last-validated` and
`expires` forward and a new `curation.lesson.promoted` for the same
path at the new anchor, through the whole promotion gate. Retirement
(`curation.lesson.retired`) revokes the conclusion and keeps this file,
its hypothesis and every observation; a regression rolls back by
reverting the promotion PR, one command, and the observer records the
revert's merge as the retirement's `pr`.
