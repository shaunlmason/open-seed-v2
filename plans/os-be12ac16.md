# Plan: next — validate the filed tier against a vocabulary, the tier table the tier system declares against (os-be12ac16)

The card, found by the injection conformance suite (os-b779b4c7) and
pinned there: *"The filed tier is presence-only data. intent.filed
requires the field non-empty and validates the VALUE no further. One
value carries authority … a dispatcher persuaded by 'this is routine,
file it as trivial' ships a contract past two gates."* Its scope: *"the
tier vocabulary, validated at intent.filed, with the two authority sites
reading from it; update next/spec/lanes.md's residual table and remove
the characterization pin."* The charter's tier is *"an autonomy level
gating what may happen without a plan or an operator"* (glossary;
§II.14 "declared per-squad and per-path in checked-in config"), and
Phase 10 item 3 declares independence levels PER TIER, so this card
builds the table item 3 adds its column to.

## What the tree actually shows

Measured, not assumed:

- **`intent.filed` validates `tier` for presence only.**
  `transition.CheckCompleteness` requires `intent`, `tier`, `budget`,
  `routing` non-empty and reads no value; the fold keeps `Tier` as
  filed. `"wizard"` files.
- **THREE authority sites read the one string, not two.** The card
  names the plan gate and the sealed-checks lint; the tree also has
  `cmd/seed/verdict.go`, where `render` refuses `unsealed` (exit 24)
  when `s.Sealed == nil && s.Tier != TrivialTier`. All three test
  `== "trivial"` against the constant, so every other value fails safe
  (plan required, lint fires, render refuses), which is why the residual
  is one string wide and not open-ended.
- **The budget class already has the shape this card needs, half
  built.** `budgets.md` carries a normative class table (`small`,
  `medium`, `large`) mirrored in `transition.budgetClasses` and pinned
  by `TestBudgetClassTableMirrorsSpec`, which parses the spec's rows; an
  unknown class has zero capacity, so reserves against it refuse. But
  the class is NOT validated at filing either: `"budget": "bespoke"`
  files, and the contract is discovered unreservable only when a worker
  holds it. Same residual, one field over.
- **The vocabulary's only named member is `trivial`.** The charter
  never lists the tiers; it says "trivial", "low tiers", "high-tier",
  "high-consequence contracts require the level their tier declares",
  and that the verifier holds quality alone on low tiers while humans
  review only high-tier work. Fixtures also file `weighty` (an offer
  drill's tier scope), which nothing defines.
- **`verdicts.md` waits on this table**: `independence` admits only
  `"L1"` "until Phase 10 declares levels per tier"; `acceptance.md`'s
  trivial-tier relaxation "lands with the tier system"; `lanes.md`'s
  residual table names Phase 10's tier system as the owner; and the
  injection suite's `TestTierIsPresenceOnlyDataAndTrivialExemptsThePlanGate`
  asserts the current behavior so that closing it fails the suite.
- **The maintenance pass files defects at `tier: trivial`**, so the
  vocabulary must keep `trivial` filable by a `dispatch`/`operator` key
  with no ceremony.

## Design decisions (binding for this task)

- **D1 — the vocabulary is a normative TABLE, three tiers, declared
  per tier what the tree already reads per tier.** New
  `next/spec/tiers.md`:

  | tier | plan required | sealed checks required | human review |
  |---|---|---|---|
  | `trivial` | no | no | no |
  | `standard` | yes | yes | no |
  | `critical` | yes | yes | yes |

  Mirrored in `internal/transition` as `tierTable` with `Tier(name)
  (TierRow, bool)`, and pinned by a test that parses the spec's rows
  exactly as `TestBudgetClassTableMirrorsSpec` does, so the two cannot
  drift one-sidedly. `TrivialTier` stays as the distinguished constant
  the maintenance pass and the fixtures name. The three names are the
  charter's own words made concrete: `trivial` is its term; `standard`
  is the ordinary contract, the one every gate applies to; `critical`
  is "high-consequence", the tier humans review. The `human review`
  column is DECLARED here and CONSUMED by nobody yet: it is the row
  item 3 and the verdict pipeline's human-verdict routing read, and
  declaring it now is what makes "per tier" a table rather than three
  scattered string comparisons. An unknown tier has no row, and every
  reader of a missing row takes the strictest column (plan required,
  sealed checks required, human review), the `BudgetCapacity` posture:
  absent knowledge is never fudged into a relaxation.

  Refused: an ordered scalar (`L0`–`L2`, or integers). The charter's
  tiers "gate what may happen without a plan or an operator": the
  gates are the meaning, and a table names each gate's answer per
  tier where an ordering would make every reader re-derive it.
  Refused: reusing v1's `L1`/`L2` autonomy tiers. Those are per-squad
  agent autonomy in `.seed/guardrails.yaml`; the charter's tier is
  per-contract, and one word for two axes is the confusion the
  glossary exists to prevent.

- **D2 — `intent.filed` validates the tier against the table at
  admission, exactly.** `CheckCompleteness("intent.filed")` gains the
  vocabulary check after presence: the value must be a table member,
  byte for byte (`Trivial`, `trivial `, `TRIVIAL` refuse). The refusal
  is a new `transition.VocabularyError{Verb, Subject, Field, Value,
  Known}` rendered in the completeness family (the same exit and wire
  code `IncompleteError` carries, `invalid_transition`'s shape
  refusals), naming the field, the value and the known members: the
  tree's rule that a refusal names what IS legal. Admission policy, not
  chain validity: a raw-pushed unknown tier still folds as filed and
  every reader takes the strictest row, so no chain changes meaning
  and no version bumps.

- **D3 — the budget class gets the same check at the same site.** The
  table exists, the pin exists, and the residual is identical: a
  contract filed with `"budget": "bespoke"` cannot be reserved against
  and is discovered stuck by the worker who claims it. `CheckCompleteness`
  validates `budget` against `BudgetCapacity`'s table with the same
  `VocabularyError`. One field over is still the same hole, and a
  reviewer asking why the tier is checked against the table beside it
  and the budget is not would be right.

  Refused: validating `routing`. It names a squad, squads are the
  deployment's (`.seed/teams/`), and no table in `next/**` can know
  them; that residual stays named in `lanes.md`.

- **D4 — the three authority sites read the table, and fail safe on
  a missing row.** `CheckPlanGate` reads `plan required`; the reconcile
  `unsealed` lint and `verdict render`'s `unsealed` refusal read
  `sealed checks required`. Their behavior on `trivial` and on
  `standard` is exactly today's on `trivial` and on anything else; on
  an unknown tier (a raw push) it is today's too, by the strictest-row
  rule. `critical` behaves as `standard` at every site this card
  touches; its `human review` column waits for its reader.

- **D5 — the residual is closed and the pin is replaced, not
  deleted.** `TestTierIsPresenceOnlyDataAndTrivialExemptsThePlanGate`
  becomes `TestTierIsValidatedAgainstTheVocabulary`: the persuaded
  dispatcher's `"wizard"` refuses at filing naming the three tiers; the
  exemption reads the table; the budget half refuses `bespoke` the
  same way. `lanes.md`'s residual row records what replaced it, and
  names the residual that remains (`routing`).

- **D6 — what item 3 adds, stated.** The `independence` column
  (`L1`/`L2`/`L3` per tier) and `verdicts.md`'s level vocabulary are
  Phase 10 item 3's; this card adds no level and changes no verdict
  rule. `acceptance.md`'s provenance-gated relaxation for trusted
  trivial-tier specs still waits on origin provenance, which is not a
  tier question, and stays as written.

- **D7 — scope guard.** No transition row moves, no exit code or wire
  code is allocated (the refusal rides the completeness family), no
  version bump, no CLI verb. Fixtures that file `weighty` move to a
  table member.

## Steps

1. `next/spec/tiers.md` (new): the table, the strictest-row rule, the
   filing check, what item 3 adds.
2. `next/internal/transition/` — `tierTable`, `Tier(name)`, the
   `TierRow` reads at `CheckPlanGate`; `VocabularyError`;
   `CheckCompleteness` validates `tier` and `budget`; the spec-mirror
   pin for tiers beside the budget one.
3. `next/internal/reconcile/` and `next/cmd/seed/verdict.go` — the two
   sealed-checks readers consult the row.
4. `next/internal/admit/injection_test.go` — the pin replaced (D5);
   drills for the refusals at admission (exact-string rows, the
   `bespoke` budget, the known members named in the message).
5. Fixtures filing `weighty` move to a member; every fixture that
   files an intent still admits (they all file `trivial`/`small`).
6. `next/spec/lanes.md` (the residual row), `next/spec/lifecycle.md`
   (completeness now names the vocabularies), `next/spec/budgets.md`
   (the class is validated at filing), `next/spec/verdicts.md` and
   `next/spec/acceptance.md` (one sentence each pointing at
   `tiers.md`).
7. `next/docs/progress.md` (Phase 10, recorded beside item 3),
   `next/docs/decisions.md`, `memory/LEARNINGS.md` (the card said two
   sites; the tree had three: derive the count); receipt; evidence;
   review.

## File Scope

- `next/internal/transition/**`, `next/internal/reconcile/**`,
  `next/cmd/seed/verdict.go`, `next/internal/admit/injection_test.go`,
  and every `_test.go` under `next/` that files a tier outside the
  vocabulary
- `next/spec/tiers.md` (new), `next/spec/lanes.md`,
  `next/spec/lifecycle.md`, `next/spec/budgets.md`,
  `next/spec/verdicts.md`, `next/spec/acceptance.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-be12ac16.json`

Nothing outside `next/**` except the work-product files above. NOT
`next/spec/transitions.json`, NOT `next/internal/envelope/**`, NOT
`next/internal/version/**`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. `intent.filed` with `tier` outside the table refuses at admission
   with nothing appended, naming the field, the value and the three
   members; drilled for `wizard`, `Trivial`, `trivial `, `TRIVIAL`,
   `standard-ish` and the empty string (which still refuses as
   incomplete). Each member files.
2. `intent.filed` with `budget` outside the class table refuses the
   same way, naming `small`, `medium`, `large`; each member files.
3. `CheckPlanGate` exempts `trivial` and requires a plan for `standard`,
   `critical` and any unknown string; the reconcile `unsealed` lint and
   `verdict render`'s `unsealed` refusal fire for `standard`, `critical`
   and any unknown string and not for `trivial`, the three sites drilled
   against the table rather than the constant.
4. The spec-mirror pin parses `tiers.md`'s rows and fails when the code
   table and the spec disagree in either direction (a planted extra
   spec row, a planted extra code row), the `TestBudgetClassTableMirrorsSpec`
   shape.
5. The injection suite's characterization pin is replaced by the closed
   residual's drill, and `lanes.md`'s residual row says what replaced it.
6. **Mutation evidence.** Each must fail a drill: the filing check
   comparing case-insensitively; the filing check skipping `budget`;
   `Tier()` returning the trivial row for an unknown name; any one of
   the three authority sites reading the constant instead of the table;
   the spec pin reading a hand list instead of the spec.

**Retention set (existing, shown unharmed):**

- Every fixture that files an intent still admits (they file `trivial`
  and `small`); the maintenance pass's defect filing at `trivial` is
  unchanged; the affordance probe still lists `intent.filed`.
- Raw-pushed intents with unknown tiers fold as they did and take the
  strictest reading at every site: no chain changes meaning, every
  pre-existing fixture chain verifies byte for byte.
- The plan gate's citation rules, the sealed-checks commitment rules,
  `verdicts.md`'s `independence: "L1"` and every exit code are
  untouched.
- `make check` green with coverage measured **cold**, at least three
  readings above the gate, and the suites pass **unprivileged** under
  `setpriv --reuid=65534`.

## Validation Commands

- Boundary: `cd next && go test ./internal/transition/ ./internal/reconcile/ ./internal/admit/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1` and `make check`
  (exit checked separately from any pipe; three cold readings).

## Expected diff shape

One new spec file and one table with its accessor in
`internal/transition`; one new error type; two lines in
`CheckCompleteness`; three one-line reads at the authority sites; one
spec-mirror test; one replaced characterization drill and its residual
row; one-sentence pointers in four specs; fixtures moved off `weighty`;
docs. No new verb, exit, code or version; no transition row moves; no
`plans/**` in the task PR.
