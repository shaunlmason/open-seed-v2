# Tiers: the vocabulary the tier system declares against

> Design authority: SEED-NEXT.md glossary (a tier is "an autonomy level
> gating what may happen without a plan or an operator"), §II.14
> (declared in checked-in config), and the charter's own words for the
> ends of the range: "trivial", "low tiers", "high-tier", "high-
> consequence contracts require the level their tier declares". Build
> plan Phase 10 (the tier system item 3 adds its column to); plan
> `plans/os-be12ac16.md`. Implemented by `internal/transition`'s tier
> table, its filing check, and the three authority sites that read it.

Before this spec the filed tier was presence-only data: `intent.filed`
required the field non-empty and read no value, and one string,
`"trivial"`, carried authority at three sites that each compared
against the constant. Every other value failed safe, so the residual
was one string wide; a dispatcher persuaded to file `wizard` shipped a
contract that every gate then treated as strictly as any. What was
missing was the **vocabulary**: what a tier may be, and what each one
answers at each gate, declared once.

## The table

| tier | plan required | sealed checks required | human review | independence |
|---|---|---|---|---|
| `trivial` | `no` | `no` | `no` | `L1` |
| `standard` | `yes` | `yes` | `no` | `L1` |
| `critical` | `yes` | `yes` | `yes` | `L2` |

Mirrored in `internal/transition` as the tier table with
`Tier(name) (TierRow, bool)`, and pinned by a test that parses the rows
above exactly as the budget class pin parses
[`budgets.md`](budgets.md), so the two cannot drift one-sidedly.
`trivial` is the charter's term for the tier whose contracts submit
without a plan and carry no sealed checks; `standard` is the ordinary
contract, the one every gate applies to; `critical` is the charter's
"high-consequence" tier, the one humans review.

The `human review` column is **consumed by the human-verdict routing**
(Phase 10 item 4, [`verdicts.md`](verdicts.md), "The rubric and the
scorecard"): on a `yes` tier `verdict.rendered` admits only from a key
holding a `verdict` grant beside operator standing, from the first
render, and the machine verifier's one act is the deferral that hands
the human its receipt; declaring it as a column is what makes "per
tier" a table rather than three scattered string comparisons. The `independence`
column is consumed: it is the minimum level a verdict on the tier must
achieve ([`verdicts.md`](verdicts.md), "Independence levels"), read by
the verdict rule, the merge chain's reapplication, `seed verdict
render` and reconcile through `TierGates`, and satisfied by any
achieved level at or above it (`L1` < `L2` < `L3`).

**The strictest-row rule.** An unknown tier has no row, and every
reader of a missing row takes the strictest column: plan required,
sealed checks required, human review, independence `L3`. This is the budget class posture
(an unknown class has no capacity) applied to authority: absent
knowledge is never fudged into a relaxation. It is also why a
raw-pushed record carrying an unknown tier changes nothing: it folds
as filed and is judged exactly as it was before the vocabulary
existed.

Refused shapes, by name: an ordered scalar (`L0`–`L2`, integers), since
the gates are the meaning and a table names each gate's answer where an
ordering makes every reader re-derive it; and v1's `L1`/`L2` autonomy
tiers, which are per-squad agent autonomy in `.seed/guardrails.yaml`
where the charter's tier is per-contract.

## The filing check

`intent.filed` validates `tier` against the table at admission,
**exactly**: the value must be a member byte for byte (`Trivial`,
`trivial `, `TRIVIAL` and `standard-ish` refuse; the empty string still
refuses as incomplete). The refusal is `VocabularyError{Verb, Subject,
Field, Value, Known}`, rendered in the completeness family
(`IncompleteError`'s exit and wire code), and it names the field, the
value and the known members: a refusal says what IS legal.

The budget class gets the same check at the same site. The class table
in [`budgets.md`](budgets.md) already existed and was pinned, but a
contract filed with `"budget": "bespoke"` was discovered unreservable
only by the worker who claimed it; now it refuses at filing, naming
`small`, `medium` and `large`. One field over was the same hole.

`routing` is not validated: it names a squad, squads are the
deployment's, and no table in `next/**` can know them. That residual
stays named in [`lanes.md`](lanes.md).

**Admission policy, not chain validity.** A raw-pushed unknown tier or
class folds as filed and takes the strictest reading at every site, so
no chain changes meaning, every existing chain verifies byte for byte,
and no protocol version bumps: this is the halt, classification and
capability precedent ([`protocol.md`](protocol.md)), a check the
cooperative boundary makes and verification tolerates in history.

## The three authority sites

Derived from the tree rather than the card, which named two:

| site | column read |
|---|---|
| `Fold.CheckPlanGate` (the submission gate, [`plans.md`](plans.md)) | `plan required` |
| the reconcile `unsealed` lint ([`reconciliation.md`](reconciliation.md)) | `sealed checks required` |
| `seed verdict render`'s `unsealed` refusal, exit 24 ([`sealed-checks.md`](sealed-checks.md)) | `sealed checks required` |

Each reads the row and fails safe on a missing one. Their behavior on
`trivial` and on `standard` is exactly what it was on `trivial` and on
anything else; on an unknown string it is what it was too, by the
strictest-row rule.

## What remains, and what item 3 adds

The vocabulary closes the unknown-value hole: a persuaded dispatcher
cannot file a tier the system does not know. It does not close
**mis-tiering**: a dispatcher persuaded to file the valid value
`trivial` still files a contract the plan gate and the sealed-checks
lint exempt, because `trivial` is a legitimate filing and nothing yet
attests who is entitled to make it. That residual is
[`plans.md`](plans.md)'s "until tier provenance lands", it stays pinned
by a characterization drill in the injection suite, and its owner is
tier provenance, not this table.

Phase 10 item 3 added the `independence` column (`L1`/`L2`/`L3` per
tier) and widened [`verdicts.md`](verdicts.md)'s level vocabulary: this
spec declares the requirement, and the verdict rule enforces it.
[`acceptance.md`](acceptance.md)'s provenance-gated relaxation for
trusted trivial-tier specs waits on origin provenance, which is not a
tier question.

## The vocabulary's order, and the guardrails that read it

The three tiers are ordered from least to most consequential —
`trivial`, `standard`, `critical` — and `transition.TierOrder` states
that order beside the table. Two readers consume it as a deployment's
declaration says ([`postures.md`](postures.md), "The preseed"): the
agent claim ceiling at admission (`max_agent` per squad; a claim above
it refuses `tier_above_ceiling`) and the path floor at the plan lint and
the render (`min` per prefix; a contract below it refuses
`under_tiered`). The `routing` residual named above is closed by the
same declaration's `teams`: with one, an unknown squad refuses
`routing_unknown`; without one, routing stays unvalidated, which is
what a deployment that never declared its squads asked for.
