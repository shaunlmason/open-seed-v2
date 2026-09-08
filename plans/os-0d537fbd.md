# Plan: next — Phase 11 item 4, expiry for revalidation, retirement with evidence kept, rollback by revert; dead ends un-retired on environment change; staleness flags, dedup and structure lint (os-0d537fbd)

The build plan's Phase 11 item 4: *"Expiry, retirement (evidence
kept), rollback-by-revert."* The charter (§12): *"every promoted
lesson carries a last-validated stamp and an expiry-for-revalidation;
retirement revokes conclusions and keeps evidence; a promoted lesson
implicated in a regression rolls back by reverting its PR — one
command, because it was a PR"*; *"Dead ends record failure condition
and environment, and can be un-retired when the environment changes;
the curator checks dead-end applicability, not just lesson
applicability."* Conformance III.K rows 6 and 7 are the targets, and
row 9 (*"Knowledge bloat is managed: dedup with provenance, staleness
flags, structure lint"*) lands here too: no Phase 11 item names it,
the exit records' precedent is to route an unrouted row into the
landing item's own text, and staleness is expiry's other face.

This plan is written against item 1's stores (os-f30ee0d3, #226) and
item 2's gate and surfacing set (os-96850e5a, #228), and depends on
both merging first.

## What the tree actually shows

Measured against `main` with #226 and #228 laid over it:

- **The stamps exist and nobody reads them.** `lesson.promoted`
  carries `last_validated` and `expires` (the lint checks they agree
  with the frontmatter and are ordered), and the surfacing set is
  "promoted, not contested, selects the subject": a lesson past its
  expiry surfaces exactly as a fresh one.
- **`lesson.retired` is named in the catalog and implemented nowhere**
  (`protocol.md`; item 1 refused it as a verb with no reader). Nothing
  revokes a conclusion: a wrong lesson stays in the surfacing set
  until its hypothesis is contested, and a contest is a judgment over
  evidence, not the observation of a revert.
- **Promotion is a PR observation**, `{pr: "<pr @ merged-commit>"}`
  in the `merge.observed` posture, so a rollback already has its one
  command (`git revert` of the merge) and lacks only the observation
  that it happened.
- **Dead ends carry an `environment` string** the holder declares
  (`deadend.recorded`), and every run declares a tuple whose fourth
  field is its environment (`qualification.md`); nothing compares the
  two, nothing retires a dead end, and the curator's held-out listing
  (`seed knowledge validate`) lists dead ends without their
  applicability.
- **Delivery is a live read** (`claim take`, `seed situation`): reads
  may consult an instant, admission never does (the offer-liveness
  posture, `--now` in the drills). Projection builds take declared
  inputs and no wall clock.
- **The maintenance loop files defect contracts from a closed lint
  set**, idempotently (`maintenance.md`), and every lint is a
  record-derived predicate.

## Design decisions (binding for this task)

- **D1 — expiry is derived, never a fact, and revalidation is a
  re-promotion.** At a declared instant a promoted lesson is
  `expired` when the instant is at or past its `expires`; an expired
  lesson leaves the surfacing set and is flagged `stale` wherever the
  store is shown. Revalidation is the curator running the hypothesis
  against the held-out evidence again (`seed knowledge validate`),
  the file's `last-validated` and `expires` moving forward in a PR,
  and the observer recording a new `lesson.promoted` for the same
  path at the new anchor through the whole item 2 gate; the fold
  keeps the LATEST admitted promotion per lesson path, and a
  re-promotion refuses unless its `last_validated` is after the
  previous promotion's. No `lesson.revalidated` verb: a stamp that
  moved is a file that changed, and a file that changed is a PR.

- **D2 — retirement is an observation, and the evidence stays.**
  `lesson.retired` (the catalog's own name) carries `{"lesson":
  "<path @ promoted-commit>", "hypothesis": "<h@position>",
  "reason": "regression" | "superseded" | "expired", "pr"?: "<pr @
  merged-commit>", "superseded_by"?: "<position>"}`, strictly
  decoded (an unknown key refuses), accepted by `observer` and
  `operator` (the promotion's own row). The two optional fields are
  each REQUIRED by exactly one reason and FORBIDDEN by the others:
  `regression` requires `pr`, the revert's merge, which is the
  charter's one command observed; `superseded` requires
  `superseded_by`, the position of an admitted `lesson.promoted`
  later than the retired promotion and not the retired promotion
  itself (the reviewer of that promotion judged the supersession;
  the record checks the citation is a real, later promotion);
  `expired` requires neither, the stamp the fold already holds being
  the evidence. The boundary requires the cited promotion to be
  admitted and the latest for its path. The fold moves the lesson to `retired`; a
  retired lesson never surfaces; its file at the promoted anchor,
  its hypothesis, its support and every observation remain, which is
  "revokes conclusions and keeps evidence". A retired lesson comes
  back only by a new promotion through the gate.

- **D3 — dead ends retire and un-retire on the environment, by a
  curator's attributable act.** `deadend.retired` and
  `deadend.unretired` (`curation.*`, `curate` alone) on the contract
  subject carry `{"deadend": "<contract>@<position>", "environment":
  "<the environment now>", "reason"}`; the boundary requires the
  cited position to be an admitted dead end on that subject, and an
  environment that CHANGED: a retirement's `environment` must differ
  from the dead end's recorded one (it no longer applies because the
  environment moved), and an un-retirement's must differ from the
  standing retirement's declared one (the environment moved again),
  so neither act admits in the environment the previous act named,
  and un-retirement also requires the standing retirement. Both
  comparisons are exact string equality, the same comparison
  applicability uses. Applicability is the one
  comparison the record supports: a dead end applies to a run whose
  declared tuple environment equals the dead end's `environment`.
  `seed knowledge validate` gains dead-end applicability: for each
  dead end on a selected contract it reports the environment, whether
  it is retired, and whether it applies to `--environment <e>`; the
  held-out listing excludes retired dead ends and says so. Evidence
  kept: retirement flags, it never deletes.

- **D4 — staleness flags, dedup and structure lint.** The `knowledge`
  projection takes a declared instant (`Inputs.AsOf`, the observation
  section's posture) and flags `stale` on every expired lesson; with
  no instant declared it flags nothing and says so. `seed knowledge
  lint` gains dedup (two files citing one hypothesis refuse unless
  one path is the other's revalidation; two hypotheses whose claim
  and exceptions canonicalize equal are one id already, and the lint
  says which file is the duplicate) and structure (the frontmatter
  carries exactly the known keys; the body carries the sections the
  README names, in order). `seed knowledge show` renders the stage,
  the flag and the reason per lesson.

- **D5 — the maintenance loop notices what nobody revalidated.** A
  lint `lesson_stale`: a lesson expired at the loop's declared instant
  by more than `--stale-after` with no later promotion or retirement
  files a defect contract naming the lesson and asking for
  revalidation or retirement, idempotently through the ledger
  (`maintenance.md`'s closed set grows by one row). The finding's
  subject is `<lesson path>@<promotion position>`, so `DefectID`'s
  existing identity (class and subject) stays as it is while the
  cycle is in the subject: reruns within one stale cycle re-file the
  same id and are refused as duplicates, and a re-promoted lesson
  whose new promotion expires later files new work. The loop never
  retires: it asks.

- **D6 — delivery reads the instant.** The surfacing set becomes
  "latest promotion per path, not contested, not retired, not expired
  at the read's instant, selects the subject"; `claim take` and `seed
  situation` take `--now` in the drills and the wall clock otherwise,
  the offer-liveness posture, and admission still reads no clock.

- **D7 — versioning and the build plan.** Three verbs (one already
  named in the catalog) and their payload shapes are catalog growth
  under `curation.*`, admission policy in item 1's D5 posture, and
  no version bump: the bump discipline (`protocol.md`) looks at what
  a conformant older VALIDATOR judges differently, and verification
  takes a verb it has no rule for on active standing (the
  standing-only class `message.sent` belongs to; `lesson.retired`'s
  name in the catalog gives it no rule at verification today), while
  the curation fold counts a malformed raw fact as an anomaly rather
  than refusing the chain. A chain carrying the three verbs therefore
  verifies identically under the build before this card and under
  it, and only admission's answer changes, which is the tier card's
  and item 1's no-bump reasoning applied again; the retention set
  drills it rather than asserting it. The task PR adds one sentence
  to `docs/next-build-plan.md` Phase 11 item 4 naming III.K row 9,
  the exit records' move, so the routing is in the plan's own text.

- **D8 — scope guard.** No automatic retirement or un-retirement; no
  environment predicate beyond string equality with the run's
  declared tuple; no delivery of dead ends at claim time beyond the
  packet findings that already reach a resuming worker (named as
  item 5's or later); no flywheel; no policy-stage revert mechanics
  beyond the observation (the revert is git's); no model; no Makefile
  change; no change to `transitions.json`, `internal/loop` or
  `next/lanes`.

## Steps

1. `next/internal/curation/` — expiry at an instant, the latest
   promotion per path, `retired`, the dead-end flags and
   applicability, the surfacing set's new arms, dedup and structure in
   the lint, `stale`.
2. `next/internal/keyring/` — the rows for `lesson.retired`,
   `deadend.retired`, `deadend.unretired`.
3. `next/internal/admit/` — the re-promotion ordering; the retirement
   rule; the dead-end rules; affordance probes.
4. `next/internal/project/` — `Inputs.AsOf` on the `knowledge` build,
   `stale`; the report's count.
5. `next/internal/maintain/` — `lesson_stale`; `next/spec/maintenance.md`.
6. `next/cmd/seed/knowledge.go` — `retire`, `deadend retire|unretire`,
   `validate --environment`, `lint` dedup and structure, `show`
   flags; `loop.go` and `situation.go` — `--now` on delivery; `maintain
   run --stale-after`.
7. Drills: curation (expiry at instants, latest-per-path, the set's
   arms, dedup, structure, applicability), keyring, admit (every D1–D3
   refusal and admit row), project (`stale` with and without an
   instant), maintain (the filing, idempotent), `cmd/seed` (the
   subverbs, delivery at instants, a regression rollback end to end in
   the modes fixture: promote, observe the revert, the next claim
   carries nothing; a revalidation moving the stamps; a dead end
   retired and un-retired with the listing changing).
8. Specs: `next/spec/curation.md` (expiry, retirement, dead-end
   applicability, bloat), `protocol.md`, `projections.md`,
   `maintenance.md`, `loop-verbs.md`; `docs/next-build-plan.md` (the
   one sentence).
9. `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/curation/**`, `next/internal/keyring/**`,
  `next/internal/admit/**`, `next/internal/project/**`,
  `next/internal/maintain/**`, `next/cmd/seed/knowledge.go`,
  `next/cmd/seed/loop.go`, `next/cmd/seed/situation.go`,
  `next/cmd/seed/maintain.go` and their drills,
  `next/cmd/seed/modes_e2e_test.go`, `next/knowledge/**`
- `next/spec/curation.md`, `next/spec/protocol.md`,
  `next/spec/projections.md`, `next/spec/maintenance.md`,
  `next/spec/loop-verbs.md`
- `docs/next-build-plan.md` (one sentence in Phase 11 item 4)
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-0d537fbd.json`

Nothing outside `next/**` except the work-product files above and the
build-plan sentence. NOT `next/spec/transitions.json`, NOT
`next/internal/loop/**`, NOT `next/internal/eval/**`, NOT
`next/lanes/**`, NOT `Makefile`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **Expiry.** A promoted lesson surfaces at an instant before
   `expires` and not at one at or past it; the projection built with
   an instant flags it `stale` and built without one flags nothing and
   says so; a re-promotion of the same path with later stamps admits,
   becomes the latest and surfaces again; one with `last_validated`
   not after the previous refuses naming both.
2. **Retirement.** `lesson.retired` admits from an `observer` for
   `regression` with the revert's `pr`, for `superseded` with
   `superseded_by` naming a later admitted promotion, for `expired`
   on an expired lesson with neither field; it refuses `regression`
   without `pr`, `pr` on any other reason, `superseded` without
   `superseded_by`, `superseded_by` on any other reason,
   `superseded_by` naming a position that is no promotion, an earlier
   promotion, or the retired promotion itself, an unknown key, a
   non-latest promotion, an unknown reason, and from a `curate` key;
   a retired lesson never
   surfaces while its file, hypothesis and observations remain in the
   fold and the projection; a new promotion of the same path brings it
   back.
3. **Dead ends.** `deadend.retired` admits from `curate` citing an
   admitted dead end with an environment different from the recorded
   one, and refuses citing anything else, the recorded environment
   unchanged, a second time without an un-retire, and from a `claim`
   key; `deadend.unretired` admits over a standing retirement with an
   environment different from the retirement's, and refuses without a
   standing retirement or with the retirement's own environment;
   `seed
   knowledge validate --environment e` marks each dead end applicable
   or not by string equality with `e`, shows the retired flag, and
   excludes retired ones from the held-out listing.
4. **Bloat.** The lint refuses two files citing one hypothesis (naming
   the duplicate), a frontmatter with an unknown or missing key, and a
   body missing a named section or ordering them wrongly; the shipped
   store passes.
5. **Maintenance.** `seed maintain run --stale-after d` files one
   defect contract for a lesson expired longer than `d` with no later
   promotion or retirement, files nothing on a second run, files
   nothing for a retired or re-promoted lesson, and files a NEW defect
   when the re-promoted lesson's later promotion expires in its turn,
   because the finding's subject carries the promotion position.
6. **End to end.** In the modes fixture: a promotion surfaces at the
   next claim; the revert is observed and the claim after carries
   nothing; a revalidation moves the stamps and surfaces again; a dead
   end is retired, the listing drops it, un-retired, the listing shows
   it.
7. **Mutation evidence.** Each must fail a drill: expiry compared with
   `>` instead of `>=`; the surfacing set ignoring expiry; the fold
   keeping the first promotion per path; the re-promotion ordering
   dropped; `pr` optional for `regression`; retirement accepted on a
   non-latest promotion; a retired lesson surfacing; un-retire
   admitted without a retirement; the environment comparison dropped
   at retirement or at un-retirement; `superseded_by` unchecked or
   accepted on the wrong reason; applicability compared
   case-insensitively; the lint skipping dedup; `stale` flagged with no
   instant; the maintenance filing not idempotent; the stale finding's
   subject omitting the promotion position.
8. `make check` green with coverage measured **cold**, at least three
   readings above the gate, and the suites pass **unprivileged** under
   `setpriv --reuid=65534 --regid=65534 --clear-groups`.

**Retention set (existing, shown unharmed):**

- Items 1 and 2 keep their drills: the stores, the gate's two halves,
  the contest, the actor arm, delivery of an unexpired, uncontested
  lesson; the poisoning drill (item 3) stays green.
- Admission reads no clock: every boundary drill runs with no `--now`;
  `seed maintain run`'s existing lints and their idempotence are
  unchanged.
- No version bump; no transition row moves; every pre-existing fixture
  chain verifies byte for byte; a chain carrying the three new verbs,
  raw-pushed under active standing, verifies under the build before
  this card exactly as under it (the no-bump argument of D7 as a
  drill: the fixture chain is committed and verified by both); the
  existing projections' builds are byte-identical on chains that carry
  no curation fact.

## Validation Commands

- Boundary: `cd next && go test ./internal/curation/ ./internal/keyring/ ./internal/admit/ ./internal/project/ ./internal/maintain/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

Expiry as a derivation and latest-per-path in the fold; three verbs
with their rows and rules; the surfacing set's three new arms; dead-end
applicability and the flags; dedup and structure in the lint; one
projection input and flag; one maintenance lint; five subverbs and
flags. Specs: five edits and one build-plan sentence. No new exit,
version or transition row; no `plans/**` in the task PR.
