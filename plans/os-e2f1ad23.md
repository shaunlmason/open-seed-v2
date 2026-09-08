# Plan: next — Phase 11 item 3, the poisoning drill: constructed trajectories fail to achieve promotion (os-e2f1ad23)

The build plan's Phase 11 item 3: *"Poisoning drill: constructed
trajectories fail to achieve promotion."* The charter (§12):
*"Trajectories are untrusted inputs — an attacker who can influence
what agents experience can construct trajectories designed to teach
the system something false"*; §16 lists *"curator poisoning
(constructed trajectories fail to achieve promotion)"* among the
security drills; III.K row 4: *"Trajectories are treated as untrusted
inputs; the poisoning drill fails to achieve promotion in CI."* The
Phase 11 exit line names it: *"poisoning drill green."*

This plan is written against item 1's stores (os-f30ee0d3, #226) and
item 2's gate (os-96850e5a, #228) and depends on both merging first.
It builds no gate; it attacks the ones they built and names what
they cannot stop.

## What the tree actually shows

Measured against `main` with #226 and #228 laid over it:

- **The gates exist, each drilled from its own side.** The proposal
  rule (two or more admitted observations on two or more distinct
  non-failed contracts; the actor arm where the family allows; the
  duplicate id; the applies-when predicate; `curate` alone, disjoint
  from `claim` and `operator`), the contest rule (held-out evidence
  only, `curate` alone), the promotion rule (an admitted, uncontested
  hypothesis; a carrier; for behavior-changing carriers an
  authenticated eval pass replayed at its position, on the marked
  subject, after the hypothesis), the file lint (frontmatter against
  the fact and the repository) and the delivery filter (contested
  never surfaces). Every one has refusal drills in its package, and
  every one is asked "does this rule refuse this row?", never "did
  the attacker get a lesson in front of a worker?".
- **The injection suite is the precedent for an attacker's-eye
  suite** (`lanes.md`): a fixture corpus under `testdata/injection/`,
  a residual table every reachable member must appear in, a
  characterization drill pinning each residual so that closing one
  fails the suite, and a completeness drill deriving the set from the
  boundary rather than from a hand list. Its first sentence is what
  it does NOT test, because there is no model under `next/`.
- **Evals refuse a vacuous counter-trajectory at `seed eval check`**
  (`eval_vacuous`: the fixture must be red and the solution green) and
  bind a definition to its reviewed anchor (`ungated` otherwise), but
  the promotion boundary sees only the authenticated pass; whether the
  definition was worth passing is the reviewer's.
- **Promotion "achieved" has two observable ends**: an admitted
  `lesson.promoted` for the hypothesis, and the lesson surfacing in a
  claim's `lessons`. A drill that asserts a refusal code asserts one
  path; a drill that asserts neither end was reached survives a gate
  being rewritten.

## Design decisions (binding for this task)

- **D1 — a poison is a scripted trajectory that ends in an attempt to
  promote, and the drill asserts the attempt failed at both ends.**
  `internal/admit/testdata/poisoning/corpus.json` declares each
  poison: `{"name", "gate", "attack", "expect": {"verb", "code" |
  "reason"}, "residual": false}`; `internal/admit/poisoning_test.go`
  carries one script per name (a chain built the `walkScript` way:
  workers' dead ends and packet findings, curators' proposals and
  contests, observers' promotions, verifiers' eval passes), and a
  drill refuses a declared poison with no script and a script with no
  declaration. For every poison the drill asserts three things: the
  named verb refuses at the named gate with the expected code or
  reason; no `lesson.promoted` for the poisoned hypothesis is admitted
  by the chain's end; and a claim on a contract the poison's
  applies-when selects carries no lesson for it. The last two are the
  charter's sentence made a test, and they are what keeps the drill
  red if a gate is rewritten so that the refusal moves.

- **D2 — the corpus covers every gate, and coverage is derived from
  the enforcement boundary, never authored for the drill.** Every
  refusal of the proposal, contest and promotion rules and of the
  lint's file half is a `curation.GateError` naming a gate registered
  in `curation.Gates()` at init (item 2's D4; created here if #228
  landed without it), and the constructor refuses an unregistered
  name, so a refusal without a gate does not exist. The drill
  enumerates that registry, the injection suite's move of deriving
  the set from the boundary rather than from a list, pins it to
  `curation.md`'s gate table in both directions, and fails on a
  registered gate no poison names, on a poison naming no registered
  gate, on a gate in the spec that no rule registers, and on a
  registered gate the spec does not name. A gate added to the rules
  without a corpus entry is therefore a red test, which is what makes
  "full coverage" a claim the drill derives rather than one it is
  told (review finding on #229). The corpus at landing, at least: `single-success` (one contract, two
  dead ends), `self-replay` (two contracts, one holder, in a family
  with two holders), `forged-support` (a position that is no
  observation; one on a failed contract; one beyond the tip),
  `worker-proposes` (a `claim` key), `worker-granted-curate` (the
  grant refused), `root-proposes` (a root holding no `curate`),
  `predicate-everything` (an empty applies-when) and
  `predicate-unknown-field`, `unchanged-reproposal` (a contested
  claim re-proposed with the same claim and exceptions),
  `held-out-forgery` (a contest citing support-set evidence),
  `contested-promotion` and `contested-surfacing`,
  `smuggled-role-lesson` (a `role` carrier with no adversarial pass),
  `raw-pass` (a pass a later-granted key raw-pushed), `borrowed-pass`
  (a pass on a subject without the marker, and one with another
  eval's), `stale-pass` (a pass filed before the hypothesis),
  `fail-as-survival`, `vacuous-eval` (a definition whose fixture is
  not red, refused at `seed eval check`, and one at an unreviewed
  revision, refused `ungated`), `fabricated-provenance` (an anchor the
  repository lacks) and `frontmatter-drift` (a file citing another
  hypothesis than the fact).

- **D3 — the residuals are named, pinned, and stated in the
  suite's own words.** `residuals.json` in the same directory, each
  entry with why it is admitted, what an attacker can inflict and
  what stands in the way, each pinned by a characterization drill
  that asserts the poison IS admitted, so closing one fails the suite
  and forces the table to say what replaced it. At landing:
  `single-holder-family` (a family with one holder admits support
  from that holder, the charter's "where the family allows"; an
  attacker holding the only worker key in a family proposes from its
  own two contracts; the adversarial eval stands in the way for
  behavior-changing carriers and the lesson PR's reviewer for
  knowledge ones); `colluding-keys` (disjointness is per key, and two
  workers and a curator on distinct keys can be one person; enrollment
  by the operator is what stands in the way, and `kind` is an
  assertion); `persuaded-curator` (a `curate` key persuaded by hostile
  text proposes a false claim over genuine support; the boundary
  admits it, the contest and the eval stand in the way, and there is
  no model to test); `reviewed-vacuous-eval` (a definition `seed eval
  check` refuses can still be merged by a reviewer, and the anchor
  rule then binds a pass to it; the review stands in the way);
  `cosmetic-reproposal` (a contested claim re-proposed with a reworded
  claim is a new subject judged on its own support).

- **D4 — the CLI arm.** At least two poisons run through `seed
  knowledge propose` and `seed knowledge promote` in the modes fixture
  (`worker-proposes`, `smuggled-role-lesson`), proving the refusal
  reaches an operator's terminal with the same code the boundary gave.

- **D5 — a poison that gets through is a defect in the gate, fixed
  there.** If constructing a poison finds a gate that admits it, the
  fix lands in the gate's own package with the poison as its
  regression drill and is recorded in `decisions.md` with the row it
  changed; the corpus never accommodates the gate.

- **D6 — specs.** `curation.md` gains "The poisoning drill" in the
  injection suite's shape (what it establishes, what it does not, the
  residual table, the gate table the corpus is pinned to); `lanes.md`'s
  conformance mapping records III.K row 4 as met by this drill.

- **D7 — scope guard.** No new verb, rule, field or exit; no change to
  the eval machinery; no expiry, retirement or rollback (item 4); no
  flywheel (item 5); no model; no change to the injection suite; no
  Makefile change (the drill is a Go test `make check` already runs).

## Steps

1. `next/internal/admit/testdata/poisoning/corpus.json` and
   `residuals.json` (new).
2. `next/internal/admit/poisoning_test.go` (new) — the registry
   enumeration and its spec pin, the corpus loader and the both-ways
   check, the scripts, the three-part assertion, the residual drills.
3. `next/internal/curation/` — `Gate`, `Gates()` and `GateError` if
   #228 landed without them, and the held-out and delivery arms the
   scripts need exposed for assertion.
4. `next/cmd/seed/modes_e2e_test.go` — the CLI arm.
5. Any gate fix D5 requires, in its package, with the poison as the
   drill.
6. Specs: `next/spec/curation.md`, `next/spec/lanes.md`.
7. `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/admit/poisoning_test.go` (new),
  `next/internal/admit/testdata/poisoning/**` (new),
  `next/internal/curation/**` (assertion seams only),
  `next/cmd/seed/modes_e2e_test.go`, and the package of any gate D5
  fixes
- `next/spec/curation.md`, `next/spec/lanes.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-e2f1ad23.json`

Nothing outside `next/**` except the work-product files above. NOT
`next/internal/eval/**`, NOT `next/internal/admit/testdata/injection/**`,
NOT `next/spec/transitions.json`, NOT `Makefile`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **Coverage is derived.** The gate set comes from
   `curation.Gates()`, the registry every curation refusal names; it
   equals `curation.md`'s table in both directions; every registered
   gate has at least one poison and every poison names a registered
   gate; a gate registered in a drill-planted rule with no poison
   fails, as does a declared poison with no script, a script with no
   declaration, an empty corpus and an empty residual table, each
   rather than passing vacuously.
2. **Every poison fails at both ends.** For each corpus entry the
   named verb refuses at the named gate with the expected code or
   reason, no `lesson.promoted` for the poisoned hypothesis is
   admitted, and a claim on a selected contract carries no lesson for
   it; the twenty-odd poisons D2 lists are present and red.
3. **The residuals are pinned.** Each `residuals.json` entry has a
   drill asserting the poison is admitted in the residual's own
   words; removing an entry, or closing a residual without updating
   the table, fails the suite.
4. **The CLI arm.** `worker-proposes` and `smuggled-role-lesson` run
   through `seed knowledge propose|promote` in the modes fixture and
   refuse with the boundary's code.
5. **Mutation evidence.** Each must turn a poison green and so fail
   the drill: the distinct-contract arm dropped; the actor arm
   dropped; the family computed from the support set; the duplicate
   check dropped; the predicate accepting an empty object; the contest
   accepting support-set evidence; a contested hypothesis promotable;
   `adversarial` optional for a `role` carrier; the pass not replayed
   at its position; the marker not checked; the ordering not checked;
   a fail accepted as survival; the delivery filter dropped; the lint
   skipping provenance. And three on the drill itself: the both-ways
   check dropped; coverage checked against the spec table instead of
   the registry; a refusal raised outside `GateError`.
6. `make check` green with coverage measured **cold**, at least three
   readings above the gate, and the suites pass **unprivileged** under
   `setpriv --reuid=65534 --regid=65534 --clear-groups`, which changes
   the real and effective UID and GID and clears supplementary groups
   so a root-group-readable path cannot mask a permission failure. The
   invocation is recorded in the task PR's evidence rather than under
   Validation Commands, because the receipt's run executes on a CI
   runner that cannot drop to another uid.

**Retention set (existing, shown unharmed):**

- The injection suite, its corpus and its residual table are
  untouched; item 1's and item 2's drills keep their assertions.
- No gate's behavior changes unless D5 recorded a defect, and then the
  change is the recorded row and nothing else; no version bump, no
  transition row, every fixture chain verifies byte for byte.

## Validation Commands

- Boundary: `cd next && go test ./internal/admit/ ./internal/curation/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

One test file with a gate table, a corpus loader, the scripts and the
residual drills; two fixture files; the CLI arm in the modes fixture;
two spec edits. No new verb, rule, field, exit or version; no
`plans/**` in the task PR.
