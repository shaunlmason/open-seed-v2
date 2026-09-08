# Plan: next — Phase 10 item 1, runtime tuples in grants, reported by adapters, drift as out-of-grant (os-8e53ffd9)

The build plan's Phase 10 item 1: *"Runtime tuples in enrollment/grants;
adapters report provisioned tuple; drift = out-of-grant."* Charter III.E
row 6 is the target, and §II.5 is the normative sentence: *"Grants cite
the qualified tuple; an actor invoking a materially different
configuration than its grant cites is out of grant, and configuration
is part of what executor adapters report (§9)."* The Phase 9 exit
record (#214) routes the first half of III.J row 3 here too: *"the
planner lane receives the strongest tuples by policy."*

## What the tree actually shows

Measured, not assumed:

- **The charter fixes the tuple's five fields** (§II.5, glossary):
  principal, harness+version, model family+version, tool policy,
  environment profile. Nothing in `next/**` carries any of them.
- **`executor.Tuple` is `{Runtime string}`**, the honest v0 stub
  `local-worktree/v0`, and `Adapter.Tuple()` is **called by nothing
  outside the executor package's own tests**. So "adapters report the
  provisioned tuple" is true of the interface and false of the chain.
- **No `cmd/seed` verb emits `run.started` or `run.settled`.** Both
  reach the chain only through `ledger append`, the raw seam that runs
  no rules; the fixtures append them by hand. There is no `seed run`
  verb at all.
- **`run.started` is strictly `{fence, reservation}`** and
  `RunStartFact` is `{Pos, Signer, Fence, Reservation}`: no
  configuration anywhere on a run.
- **`actor.granted` is strictly `{capability}`** and the keyring's
  `Entry` is `{Key, Kind, Name, Standing, Root, Grants []string}`: a
  grant cannot cite anything.
- **`actor.qualified` is cataloged and refused**: `protocol.md` says it
  "cites eval results and the runtime tuple", `actors.md` says
  "undefined until qualification lands (Phase 10)", and the keyring's
  `default:` arm refuses it by name.
- **`out_of_grant` is capability absence only** (`OutOfGrantError{Actor,
  Verb, Accepted}`), and the tree already records that capability
  absence is a different property from disjointness (os-6a08b166).

## Design decisions (binding for this task)

- **D1 — the tuple is the charter's five fields, as one strict object,
  spelled once.** `{"principal": …, "harness": "<name>/<version>",
  "model": "<family>/<version>", "tool_policy": …, "environment": …}`,
  every field a non-empty string, unknown fields refused. It lives in a
  new package `next/internal/tuple` (`Tuple`, `Parse`, `Equal`) and
  `executor.Tuple` becomes that type. The local worktree adapter
  reports what it can honestly know: `harness: "local-worktree/v0"`,
  `environment: "<the workspace's provenance>"`, and the three fields
  it cannot know — principal, model, tool policy — come from the run's
  declaration (D3), never invented by the adapter.

  Refused: keeping `Runtime string` and packing five things into it.
  Drift is a per-field comparison (D4), and a string with five things
  in it cannot say which one moved.

- **D2 — grants cite the tuple; enrollment does not; and the FIRST
  qualified grant retires the bridge for that actor.** `actor.granted`
  gains an OPTIONAL `tuple` (D1's object). The keyring's `Entry` stores
  grants as `[]Grant{Capability, Tuple *Tuple}` instead of `[]string`,
  with `Grants()` keeping the old string view for every existing caller
  and a new `GrantTuples(actor, capability) []Tuple` — plural, because
  grants accumulate and a singular accessor would be ambiguous the
  moment an actor held two.

  Optional is the bridge, and it is stated rather than smuggled: an
  actor NONE of whose grants for the capability cite a tuple is
  **unqualified** and admits exactly as today, so no existing chain,
  fixture or deployment changes meaning. Phase 10 item 2 is what makes
  evals mint qualified grants; this card gives the grant somewhere to
  put one.

  **The matching rule, since grants only accumulate** (review finding
  on #215): the tree has no grant-level withdrawal, suspension and
  re-enrollment preserve grants, and a rule that let any tuple-less
  grant admit any run would mean a qualified grant appended later could
  never constrain a worker enrolled before it. So the rule reads the
  SET. At `run.started`, take every tuple cited by the claim holder's
  `claim` grants: if the set is empty the holder is unqualified and the
  run admits; if it is non-empty the run's tuple must EQUAL one member
  (D1's per-field equality), else drift (D4). Consequences, each
  drilled: the first qualified grant makes qualification authoritative
  for that actor with no migration verb and no withdrawal — the bridge
  grant is not removed, it simply stops being the only thing consulted;
  a second qualified grant ADDS an admissible configuration rather than
  replacing the first; and a tuple-less grant appended AFTER a qualified
  one changes nothing, because the set is what is read.

  Refused: retiring the bridge by a global switch. A deployment
  qualifies actors one at a time as evals pass, and a switch that
  flips everyone at once would leave every not-yet-evaluated actor
  unable to run.

  Enrollment stays `{key, kind, name}`. The build plan says
  "enrollment/grants"; the charter says "grants cite the qualified
  tuple" and never mentions enrollment. Qualification binds to the
  runtime, not the key (§II.5's own heading), and an enrollment is the
  key. A tuple on the enrollment would say a key IS a configuration,
  which is the conflation the charter is at pains to refuse.

- **D3 — the adapter's report reaches the chain on `run.started`,
  through a verb.** `run.started`'s payload gains a REQUIRED `tuple`.
  And there is a verb to carry it: **`seed run start --subject <id>
  --key <path> [--adapter local-worktree] [--principal …] [--model …]
  [--tool-policy …]`**, the supervisor's act, deriving the fence from
  the active window and the reservation from the shared budget view
  exactly as the loop verbs do, filling `harness` and `environment`
  from `Adapter.Tuple()`, and pre-flighting through `admit.Check` so a
  refusal carries the boundary's own error.

  Required, not optional, because a run with no declared configuration
  is a run nothing can qualify, and a spend that qualification cannot
  see is the thing III.E row 6 forbids. Existing fixtures that append
  `run.started` raw gain the field; there are eleven such appends and
  the receipt counts them.

  **And the declaration is checked against what the adapter actually
  provisioned, before any execution is released** (review finding on
  #215). `run.started` carries the caller's DECLARATION, admitted before
  provisioning; on its own that is a promise, not a report. So the
  adapter's report is a second, post-provision value: `Run` gains
  `Tuple() tuple.Tuple`, the configuration the adapter RESOLVED, and
  `Provision` — which already reads the chain and finds the admitted
  start for the spec's fence (`verifyStarted`) — compares the resolved
  tuple against the admitted one field by field and refuses
  `ErrTupleMismatch` with full rollback on any difference, so no
  workspace is ever handed to a caller for a configuration the ledger
  did not admit. The comparison is the adapter's, inside `Provision`,
  because that is the one place the resolved value exists and execution
  has not yet started.

  The v0 local adapter's resolved tuple is `harness` and `environment`
  from what it built plus the three fields from the spec, and it says
  so: it provisions a worktree, and a worktree cannot see which model a
  lane process will call. That limit is honest and it is recorded; what
  this card ships is the CHECK and its seam, drilled with a fake adapter
  that resolves a different model and is refused with nothing left on
  disk. A container or cloud adapter (Phase 13) resolves all five and
  inherits the check unchanged.

  Refused: reporting the tuple on `run.settled`. Settlement is telemetry
  after the fact ("never authority", `executors.md`); drift must refuse
  BEFORE the spend, and the spending verb is `run.started`.

  Refused: a new `run.provisioned` verb. The adapter provisions only
  against an admitted `run.started` for its fence (`executors.md` step
  3), so the start IS the moment the configuration is committed to.

- **D4 — drift is a per-field inequality between the run's tuple and
  the CLAIM HOLDER's qualified grant, refused as `out_of_grant`.** At
  `run.started` admission: take the subject's active claim holder; take
  that holder's `claim` grant; if it cites a tuple and any of the five
  fields differs from the run's declared tuple, refuse
  `OutOfGrantError` with a new `Drift` detail naming the holder, the
  field, the cited value and the provisioned value.

  The holder, not the signer. The supervisor signs `run.started`; the
  work executes under the holder's key, and the charter's sentence is
  about "an actor invoking a materially different configuration than
  its grant cites" — the invoking actor is the one whose window the
  run spends. A drill plants the mismatch on the signer's grant and the
  match on the holder's, and must admit; and the reverse must refuse.

  "Materially different" is every field, in v0. The charter leaves
  "materially" to policy; the honest v0 has no policy and says so:
  any difference is drift, and a later card that wants tolerance (a
  patch-version bump) owns the decision. Drilled per field, five rows.

  The code stays `out_of_grant` (exit 14): the charter names this
  refusal "out of grant" in those words, and `envelope.md`'s rule is
  that a refinement inside a family keeps the family's exit.
  `OutOfGrantError` gains the `Drift` field so the message can say
  which; the wire code does not change.

- **D5 — `actor.qualified` stays undefined here.** It "cites eval
  results" (`protocol.md`), and eval results are Phase 10 item 2. This
  card gives grants a tuple field; item 2 is what mints a grant from a
  passing eval. Defining the verb now would define a fact with nothing
  to cite.

- **D6 — III.J row 3's "strongest tuples by policy" lands as a
  scheduling INPUT, not a scheduler.** `offer.published`'s eligibility
  already scopes offers by capability and tier; it gains an optional
  `tuples` list (D1 objects), and `offer list` / the loop's poll filter
  a qualified worker by it: a worker whose `claim` grant cites a tuple
  sees an offer only if its tuple is in the offer's list, or the offer
  names none. That is the whole of "by policy" the tree can honestly
  hold: the supervisor writes the policy into the offer. A policy
  engine deciding which tuples are "strongest" is item 2's eval results
  turned into offers, and is not built here.

- **D7 — the tier card (os-be12ac16) stays separate.** It is the tier
  vocabulary at `intent.filed`, which Phase 10 item 3 (levels declared
  per tier) owns; nothing in item 1 reads a tier.

- **D8 — `seed/2`, because a `seed/1` validator judges a tuple-bearing
  grant differently** (review finding on #215). `actors.md` makes actor
  payload shapes CHAIN VALIDITY, strictly decoded, so a `seed/1`
  validator fails a grant carrying `tuple` at its position
  (`bad_actor_event`) while this card's validator accepts it — exactly
  the "validation rules that a conformant `seed/N-1` validator would
  judge differently" `protocol.md`'s bump discipline names. The tuple
  semantics therefore activate at **`seed/2`**, on the pattern
  `actors.md` set for `seed/1`: `version.Seed2`, added to `Supported()`;
  a gate (`tuple.Applies(active)`) beside `keyring.Applies`, which
  itself becomes true for `seed/1` and later so actor semantics stay on;
  at `seed/1` positions a grant's `tuple` field stays unknown-and-refused
  and `run.started` does not require one, so every existing chain
  verifies byte-for-byte as before; at `seed/2` positions the grant
  field is accepted, `run.started` requires `tuple`, and D4 applies.
  `protocol.md`'s version register gains the `seed/2` entry, the bump
  lands as this PR editing that file plus the `system.protocol.upgraded
  {"to": "seed/2"}` event, and fixtures that exercise tuples upgrade to
  `seed/2` after `seed/1`.

  A mixed-version drill pins the disagreement the finding describes
  and shows it resolved: one chain, a grant with a tuple appended at a
  `seed/2` position, verified by a build supporting only `seed/1` (which
  refuses at the upgrade record with `version_mismatch`, never by
  misjudging the grant) and by this build (which verifies).

- **D9 — scope guard.** No verb is added to the ledger vocabulary
  (`run.started` and `actor.granted` grow a field each; `seed run start`
  is a CLI verb over an existing ledger verb, like `claim take`). No
  transition row moves. No exit code is allocated. `actor.enrolled` is
  untouched.

## Steps

0. `next/internal/version/` — `Seed2` and `Supported()`;
   `next/spec/protocol.md` — the `seed/2` register entry (D8).
1. `next/internal/tuple/` — `Tuple`, `Parse` (strict), `Equal`,
   `Diff` (the first differing field), `Applies(active)`; unit-drilled
   including every malformed shape.
2. `next/executor/executor.go` — `Tuple` becomes `tuple.Tuple`; the
   local adapter's static report carries `harness` and `environment`
   and leaves the other three for the caller; `Run.Tuple()` reports the
   RESOLVED configuration; `Provision` compares it to the admitted start
   and refuses `ErrTupleMismatch` with rollback (D3); a fake adapter
   drills the refusal.
3. `next/internal/keyring/keyring.go` — `actor.granted` accepts optional
   `tuple` at `seed/2` positions only; `Entry` stores
   `[]Grant{Capability, Tuple}`; `Grants()` keeps the string view;
   `GrantTuples(actor, capability) []Tuple` (D2); `Applies` true for
   `seed/1` and later.
4. `next/internal/transition/transition.go` — `RunStartFact` gains
   `Tuple`; the fold reads it.
5. `next/internal/admit/admit.go` — at `seed/2`, `run.started` requires
   `tuple`; the drift check (D2's set rule, D4's per-field refusal) in
   the run rule, refusing `OutOfGrantError{…, Drift}`.
6. `next/cmd/seed/run.go` (new) — `seed run start`, deriving fence and
   reservation, filling the tuple from the adapter and flags,
   pre-flighting through `admit.Check`; `run_cli_test.go` gains its
   drills.
7. `next/internal/admit/affordances.go` — the `run.started` probe
   carries a tuple so the affordance sweep stays green.
8. Offers: `offer.published` accepts optional `tuples`; `offer list` and
   the loop's poll filter on it (D6); the modes fixture provisions its
   workers with qualified grants and proves a mismatched tuple is
   refused at `run.started` and unseen at `offer list`.
9. Every fixture that appends `run.started` raw gains the field, and
   every fixture that exercises tuples upgrades to `seed/2` after
   `seed/1`; the mixed-version replay drill (D8).
10. Specs: new `next/spec/qualification.md` (D1–D6, the bridge, the
    per-field drift rule, what item 2 adds); `actors.md` (the grant
    payload, `actor.qualified` still undefined and why);
    `executors.md` (the tuple's five fields, the start carries it, the
    stub retired); `offers.md` (the `tuples` eligibility); `lanes.md`
    (the residual table's tier row unchanged — it is item 3's).
11. `next/docs/progress.md` (Phase 10 opened, item 1 recorded),
    `next/docs/decisions.md`, `memory/LEARNINGS.md`; receipt; evidence;
    review.

## File Scope

- `next/internal/version/**`, `next/internal/tuple/**` (new),
  `next/executor/**`,
  `next/internal/keyring/**`, `next/internal/transition/**`,
  `next/internal/admit/**`, `next/cmd/seed/run.go` (new),
  `next/cmd/seed/run_cli_test.go`, `next/cmd/seed/offer.go`,
  `next/cmd/seed/offer_cli_test.go`, `next/cmd/seed/modes_e2e_test.go`,
  `next/internal/loop/**` (the poll filter only), and every `_test.go`
  that appends `run.started` raw
- `next/spec/qualification.md` (new), `next/spec/protocol.md` (the
  `seed/2` register entry), `next/spec/actors.md`,
  `next/spec/executors.md`, `next/spec/offers.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-8e53ffd9.json`

Nothing outside `next/**` except the work-product files above. In
particular NOT `next/spec/transitions.json` and NOT
`next/internal/envelope/**`: D9 says no row moves and no code is
allocated, and those are where a violation would land.

## Acceptance Criteria

1. A `run.started` whose tuple differs from the claim holder's
   qualified grant in any ONE of the five fields refuses `out_of_grant`
   (exit 14) naming the holder, the field, and both values; drilled per
   field, five rows, through `seed run start` against a real ledger.
2. A matching tuple admits; a holder NONE of whose `claim` grants cite
   a tuple admits any run (the bridge); a mismatch planted on the
   SIGNER's grant with a match on the holder's admits, and the reverse
   refuses — so the check is shown to read the holder. The set rule
   (D2), each its own row: a holder with a bridge grant AND a later
   qualified grant refuses a non-matching run (the bridge is retired);
   a holder with two qualified grants admits either tuple and refuses a
   third; a tuple-less grant appended after a qualified one changes
   nothing.
2b. An adapter whose resolved tuple differs from the admitted start in
   any field is refused at `Provision` with `ErrTupleMismatch` and
   leaves no workspace and no worktree registration behind; the local
   adapter's resolved tuple equals its declaration; `Run.Tuple()` is
   what was resolved, not what was declared.
2c. A grant carrying `tuple` at a `seed/1` position fails verification
   as `bad_actor_event` under this build too (the field activates at
   `seed/2`); at a `seed/2` position it verifies. A build supporting
   only `seed/1` refuses the chain at the `seed/2` upgrade record with
   `version_mismatch`, never by misjudging the grant. Existing chains
   with no upgrade verify byte-for-byte as before.
3. `seed run start` fills `harness` and `environment` from
   `Adapter.Tuple()`, never invents principal, model or tool policy,
   refuses (usage) when a required field is neither supplied nor
   derivable, and pre-flights so the envelope carries the boundary's
   own refusal beside the caller's affordances.
4. `actor.granted` with a malformed tuple (a missing field, an empty
   string, an unknown field, a non-string) fails verification at its
   position as `bad_actor_event`, the chain-validity posture
   `actors.md` gives payload shapes; a grant with no tuple folds as
   today.
5. `offer list` hides an offer naming tuples from a qualified worker
   whose tuple is not among them, shows it to a worker whose tuple is,
   and shows an offer naming none to every eligible worker; the loop's
   poll agrees with the listing.
6. `actor.qualified` is still refused by name, and the refusal message
   still points at item 2.
7. **Mutation evidence.** Each must fail a drill: drift comparing
   against the signer instead of the holder; the comparison skipping
   any one field; the set rule consulting only the FIRST grant; `tuple`
   made optional on `run.started` at `seed/2`; the adapter inventing a
   model; `Provision` skipping the resolved-vs-admitted comparison;
   `Grants()` losing a qualified grant from the string view; the offer
   filter passing a tuple-naming offer to a non-listed worker; the
   drift refusal emitted under a code other than `out_of_grant`; and
   the tuple gate applied at `seed/1`.
8. `make check` green with coverage measured **cold**, at least three
   readings above the gate, and the suites pass **unprivileged** under
   `setpriv --reuid=65534`.

## Validation Commands

```sh
cd next && gofmt -l . && go vet ./... && go build ./...
cd next && go test ./internal/version/ ./internal/tuple/ ./internal/keyring/ ./internal/admit/ ./internal/ledger/ ./executor/ ./cmd/seed/ -count=1
cd next && go test ./... -count=1
make check
```
