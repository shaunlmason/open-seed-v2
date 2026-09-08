# Plan: next — Phase 10 item 2, eval contracts through the production machinery; grants cite passing tuples; scheduled spot-checks; suspension on failure (os-03e47abb)

The build plan's Phase 10 item 2: *"Eval contracts against fixture repos
through the production machinery; grants cite passing tuples; scheduled
spot-checks; suspension on failure."* Charter §II.16 is the definition:
*"Eval contracts: synthetic work with known verdicts, fixture repos, run
through the production machinery; passing gates grants for the tuple
that ran them; scheduled spot-checks re-test active tuples."* §II.5 is
the consequence: *"Spot-check evals re-test active tuples; drifted or
failing tuples get grants suspended by the supervisor — an attributable
event, no operator ceremony."* Conformance III.E row 7 (spot-checks
suspend attributably) and III.O row 1 (eval contracts gate tuple
qualification; spot-checks run scheduled) are the targets.

This plan is written against item 1's surfaces as they stand in #216
(`plans/os-8e53ffd9.md`, `next/spec/qualification.md`) and depends on
that PR merging first; the card says so.

## What the tree actually shows

Measured, not assumed:

- **Item 1 gave the tuple somewhere to live and the rule that reads
  it.** `actor.granted` cites an optional `tuple` at `seed/2`; the
  keyring keeps the SET per capability (`GrantTuples`); `run.started`
  declares a tuple and the run rule refuses drift `out_of_grant` against
  the CLAIM HOLDER's set, the empty set being the bridge; `Provision`
  holds the adapter to the declaration; offers scope by `tuples`.
- **Nothing mints a grant from a verdict.** `actor.qualified` is
  cataloged ("cites eval results and the runtime tuple", `protocol.md`)
  and the keyring's `default:` arm refuses it by name, pointing at this
  item. The only way a tuple enters a grant today is an operator's hand.
- **Nothing suspends a grant.** `actor.suspended {reason}` suspends the
  KEY's standing, and every `actor.*` verb is accepted from `operator`
  alone (`AcceptedCapabilities`: `IsActorVerb → [operator]`). There is
  no grant-level or tuple-level suspension, and the supervisor role
  (`supervise`) can sign no actor event at all.
- **The verdict pipeline is the production machinery an eval must run
  through, and its authority is the RECEIPT, not the record.**
  `contract.specified` carries a commit-anchored executable acceptance
  whose `gate` names the merged PR at the SAME commit as the `ref`
  (`acceptance.md`'s equality rule); `seed verdict render --repo`
  executes the acceptance body's validation commands in a workspace at
  the submission head, derives the permissible verdict from the
  transcripts (`checks_red` forbids pass over red), and stores the
  receipt content-addressed; `verdict.Compute` recomputes it from the
  submission head, and `seed verdict check` refuses `receipt_mismatch`
  when the cited digest does not reproduce. Admission validates a
  `verdict.rendered`'s signer (verdict grant, L1 disjointness) and
  shape; it neither executes checks nor recomputes receipts, so a
  verdict-granted, disjoint key can append a pass citing an invented
  digest, and only recomputation exposes it. The lockout's
  `authenticFail(c, subject, s)` is the tree's one "boundary-validated
  verdict" derivation; nothing derives an authenticated PASS.
- **A judged contract left in `review` is a lint finding.**
  `reconcile.Subject` classes `!merged && passVerdict` as
  `unreconciled`, and the maintenance pass files a defect contract per
  finding; `internal/obligation` owes `merge.requested` after a pass.
  An eval that ends at its verdict must say so to both, or every judged
  eval files a defect and owes a merge forever.
- **Squash merges record the PR in the subject line** (`git log -1
  --format=%s -- plans/` reads `… (#215)`), so a definition's last
  reviewed commit AND the PR that reviewed it are both facts the
  repository holds.
- **The maintenance pass is the only unattended loop, and it is
  wakeless**, and its lane spec is explicit that its `operator` grant
  is not for another lane's work: the pass reaps, lints, files, rebuilds
  and checkpoints, "audited as an ordinary actor". The supervisor role
  is verbs (`offer publish`, `run start`), not a process.
- **Fixture repositories exist only as a test helper** (`verdictRepo`):
  nothing under `next/` ships an eval.
- **`intent.filed` is presence-checked, not strictly decoded**
  (`completeness`: `intent`, `tier`, `budget`, `routing`); the fold
  keeps `Tier` and nothing else from it. A new field is admission
  policy, not chain validity.
- **The set rule refuses exactly the run an eval needs.** A worker
  qualified for A that runs an eval under B is drifting by item 1's
  rule; a worker whose only tuple is disqualified could never re-test
  it. Evaluation is where a configuration proves itself, so the rule
  that qualification feeds cannot gate the act that mints it.

## Design decisions (binding for this task)

- **D1 — an eval contract is an ordinary contract, marked at filing,
  and everything downstream is the production machinery unchanged.**
  `intent.filed` gains an OPTIONAL `eval` object: `{"name": "<eval>",
  "tuple": {…} | absent}`. `name` names a shipped eval definition (D3);
  `tuple` is present on a spot-check and names the configuration under
  re-test, advisory. The fold keeps it as `SubjectState.Eval
  *EvalInfo`. From there the contract is specified, offered, claimed,
  reserved, started, worked, submitted and judged by exactly the verbs
  and rules every other contract crosses: the verdict is rendered by a
  verifier whose key is L1-disjoint from the implementer, the receipt is
  content-addressed and recomputable, and the lockout applies. "Through
  the production machinery" means there is no eval runner; there is a
  marker and a consequence.

  Refused: a separate eval pipeline or a `seed eval run` that executes
  the acceptance itself. An eval that does not cross the verifier
  boundary proves the machinery nothing and can be gamed by whoever
  runs it.

- **D2 — the consequence is `actor.qualified`, minted from a verdict
  whose receipt RECOMPUTES GREEN, citing what it read.** Subject: the
  actor's fingerprint. Payload, strict: `{"capability": "claim",
  "tuple": {…}, "contract": "<eval subject>", "verdict":
  "<position>"}`. Chain validity (keyring, the `actors.md` posture):
  shape, subject enrolled and not revoked, tuple strict; effect: the
  tuple joins the actor's admissible set for the capability, the
  capability joins `Grants` if absent (a qualification IS a grant with
  evidence), and the entry records the qualification's provenance
  (`{Tuple, Contract, Verdict, Pos, TS}`) for D5's derivation.
  Admission (policy): the signer holds `supervise` or `operator`; the
  cited contract folds with `Eval != nil`; the cited position is that
  subject's **authenticated pass** verdict (`authenticPass` beside
  `authenticFail`: verdict grant plus L1 disjointness, replayed at the
  verdict's own position); the `run.started` admitted in that verdict's
  submission window (`RunStartValid`) declared exactly `tuple`; that
  window's holder is the subject; the record's `ts` is not earlier than
  the cited verdict's; and no earlier `actor.qualified` cites the same
  verdict (the boundary's duplicate refusal is what makes minting
  idempotent). Each is a drilled refusal.

  **And admission is not enough** (review finding on this plan). A
  verdict-granted, disjoint key can append a syntactically valid pass
  citing a digest no run produced, and the boundary cannot tell. So the
  derivation that DECIDES to mint (D5's `Due`) retrieves the cited
  receipt from the artifact store, recomputes it against the submission
  head in the repository with the same `verdict.Compute` that `seed
  verdict check` runs, and mints only if the stored bytes retrieve
  intact, the recomputation reproduces the cited digest, and every
  transcript in it exited zero. A receipt that does not retrieve, does
  not reproduce, or carries a red transcript mints nothing and is
  REPORTED by name (`receipt_mismatch` is already the family). The
  verifier boundary is where checks run; the recomputation is how a
  second party confirms they ran. Admission keeps the checks it can
  make so a raw mint citing a fail, a non-eval, or the wrong tuple
  refuses at the boundary too, but the check that needs the repository
  lives where the repository is.

  Refused: minting from `verdict.rendered` directly (a verdict is the
  verifier's fact about a submission; a grant is a standing fact about
  an actor, signed by whoever owns qualification). Refused: a
  qualification for the SIGNER of the run (the supervisor declares; the
  holder executed; item 1's rule reads the holder and so does this).
  Refused: trusting the verdict record's digest without recomputing it
  (the receipt's guarantee is that verification recomputes everything
  from the submission head and fails on mismatch; a mint that skipped
  that would be the one consumer of a receipt that never checked it).

- **D3 — evals ship as reviewed files IN THIS REPOSITORY, and the
  fixture is the repository itself at the reviewed commit, so the gate
  cites a real merge.** `next/evals/<name>/` holds `eval.json`
  (`{"name", "summary", "tier": "trivial", "acceptance":
  "next/evals/<name>/fixture/accept.md"}`), `fixture/` (the files the
  eval is worked in, the acceptance spec among them), and `solution/`
  (the files the reference solution changes). There is no separate
  fixture repository and no materialized commit: the workspace a
  worker is provisioned into is a detached worktree of this repository
  at the eval's anchor, the local adapter's ordinary provision, and the
  work is edits under `next/evals/<name>/fixture/` submitted as an
  ordinary base..head range that is never merged.

  **The anchor is derived from the repository, never typed.**
  `eval.Anchor(repo, name)` reads the last commit touching
  `next/evals/<name>` and the PR number its squash-merge subject
  carries: `ref` is `next/evals/<name>/fixture/accept.md @ <that
  commit>` and `gate` is `pr/<n> @ <that commit>`, the same commit on
  both sides exactly as `acceptance.md`'s equality rule demands, and
  both are facts the repository holds about a review that happened. A
  definition whose last commit carries no PR number is not filable
  ("the definition's last commit is not a merged PR"): an eval is armed
  content, and content that has not been through the gate arms
  nothing. An uncommitted or dirty definition refuses the same way.

  **Known verdicts are proven, not asserted.** `seed eval check --repo
  <repo> --eval <name>` provisions a worktree at the anchor, runs the
  acceptance commands there through the verifier's own runner, and
  requires RED; applies `solution/` in a second commit on a throwaway
  branch, runs them again, and requires GREEN; the worktree is removed
  either way. An eval whose unsolved fixture already passes tests
  nothing and refuses `eval_vacuous`, a refinement inside exit 19's
  family (`spec_unrunnable`: the declaration promised something
  decidable and the body decides nothing); a solution that stays red
  refuses `checks_red` (20). One shipped eval, `fix-the-check`: a
  fixture whose `check.sh` fails until a one-line defect is fixed,
  chosen so the verifier's transcript grammar and the worker's edit are
  both real and neither needs a toolchain beyond `sh`.

  Refused (review finding on this plan): a fixture repository built by
  `git init` at run time, whose commit is not the merge commit of any
  review and whose `gate` would satisfy the equality check while
  presenting a commit nobody reviewed as a review fact. Refused: git
  bundles (binary, unreviewable). Refused: fixtures generated from
  templates (the ref must name one commit anyone can check out).

- **D4 — failure suspends the GRANT for that tuple, tuple-wide, by a
  verb of its own: `actor.disqualified`.** Payload, strict:
  `{"capability": "claim", "tuple": {…}, "contract": "<eval>",
  "verdict": "<position>", "reason": "<non-empty>"}`. Chain validity:
  shape, subject enrolled, tuple currently admissible for the capability
  (nothing to disqualify otherwise); effect: the tuple leaves the
  admissible set and the entry remembers it was cited. Admission: signer
  `supervise` or `operator`; the cited verdict is an **authenticated
  fail** on an eval subject whose window's `run.started` declared
  exactly `tuple`. A fail needs no recomputation to be acted on: a fail
  is always renderable and never needs a green transcript, and the
  consequence of believing a raw fail is a re-test, not a grant. The
  holder of that window need not be the subject: the charter qualifies
  configurations, not keys, and "failing tuples get grants suspended"
  reads as every grant citing the tuple. D5's derivation therefore
  disqualifies EVERY actor whose admissible set holds the failed tuple,
  one attributable event each, and the drill provisions two.

  **The bridge does not reopen.** Item 1's set rule reads the
  admissible set; this card refines it in one place: an actor that has
  EVER been cited for the capability and whose admissible set is now
  empty refuses every run `out_of_grant` ("every cited configuration is
  disqualified"), never bridges. `GrantTuples` returns admissible tuples
  only; a new `EverCited(actor, capability)` distinguishes the two empty
  sets. Re-qualification is a later `actor.qualified` for the same
  tuple citing a verdict after the disqualification; the keyring keeps
  the latest event per (capability, tuple).

  Refused: extending `actor.suspended` with a tuple. Capability would
  then depend on payload (a supervisor may suspend a grant, never a
  key), and `actor.suspended` keeps its one meaning. Refused: a
  disqualification that only touches the failing holder. The
  configuration failed; a second key running the same configuration is
  the same configuration.

- **D5 — "scheduled" means DERIVED FROM THE CHAIN, ANCHORED IN
  ATTESTED TIME, and PERFORMED BY THE LANES THAT OWN THE ACTS, through
  one wakeless verb.** `eval.Due(records, table, ring, store, repo,
  now, after)` returns the acts owed at the declared instant `now`:

  - a **mint** for every eval subject carrying an authenticated pass
    whose receipt recomputes green (D2) and whose run declared a tuple
    the holder is not yet qualified for by that verdict;
  - a **disqualification** for every admissible (actor, tuple) whose
    tuple an authenticated fail on an eval subject names (D4);
  - an **offer** for every eval subject in `ready` with no live offer;
  - a **spot-check** for every admissible (actor, tuple) whose latest
    qualification is older than `after` at `now`, with no open eval
    subject naming that tuple: a filing and a specification from the
    shared construction site (`eval.Filing`), under a stable id derived
    from the eval name, the tuple, and the count of prior evals for
    that pair, so a second run in the same window refuses the duplicate
    at the boundary and a satisfied window advances the count.

  **The time anchor is the qualification record's own `ts`** (review
  finding on this plan): a position is an ordinal and carries no
  elapsed time, so age is `now - qualified.TS`, the record's attested
  timestamp against the declared `--as-of`, which is exactly how offers
  already measure liveness (`expires` against the event's own `ts` at
  admission, against a declared `now` at listing). Two guards keep a
  lie about time from postponing a re-test: admission refuses an
  `actor.qualified` whose `ts` precedes the verdict it cites (D2), and
  a qualification whose `ts` is LATER than the declared `now` is
  treated as due immediately and reported as an anomaly, never as young.
  `after` defaults to `168h`; `0` disables spot-checks.

  **The performer is `seed eval act (--ledger|--remote …) --repo <repo>
  --key <path> [--as-of <ts>] [--spot-check-after <d>]`**, and it
  performs the subset of `Due` the key's grants admit, reporting the
  rest as owed by another lane: with a `supervise` key it mints,
  disqualifies and publishes offers; with a `dispatch` key it files and
  specifies spot-checks; an `operator` key does all; a key holding none
  refuses each `out_of_grant` at the door with nothing appended. It is
  wakeless like `seed maintain run`: it reads, acts and reports, and a
  caller runs it whenever it likes. Nothing is stored between runs;
  due-ness is recomputed from the chain and the declared instant, so a
  run replays.

  Refused (review finding on this plan): performing these acts inside
  `seed maintain run` under the maintenance lane's `operator` grant. The
  charter says the supervisor suspends, attributably; `maintenance.md`
  says the lane's operator grant is not for another lane's work; and an
  event admitted through maintenance's operator standing would be
  attributed to the wrong role. `seed eval act` is the supervisor's
  (and the dispatcher's) own pass, run by whoever operates those keys,
  and the maintenance pass gains nothing. Refused: a timer, a cron
  surface, or a wake channel. Refused: due-ness stored anywhere.

- **D6 — eval offers are never tuple-scoped, and the run rule exempts
  eval subjects from the set rule.** An eval for B exists to qualify
  workers NOT yet qualified for B, and a re-eval of a disqualified T is
  taken by a worker that cannot currently see a T-scoped offer; so the
  offer `Due` publishes is scoped by capability and tier only, and
  `run.started` on a subject with `Eval != nil` admits any declared
  tuple, disqualified ones included. The mint reads what the run
  DECLARED, never what the intent's advisory `tuple` said; a spot-check
  taken under a different configuration qualifies that configuration
  and leaves the checked one due.

- **D7 — `actor.qualified` and `actor.disqualified` are accepted from
  `[supervise, operator]`**, the first `actor.*` rows that are not
  operator-only; the scheduled signer is the supervisor's key (D5), so
  every such event is attributed to the role the charter names, with
  `operator` as the tree's standing human override.

- **D8 — `seed/3`, for the same reason item 1 needed `seed/2`.** The
  bump discipline exempts "additive verb-catalog growth that older
  validators safely refuse as unknown"; for lifecycle verbs that is what
  happens (the fold skips, admission refuses the proposal). For
  `actor.*` verbs it is not: `actors.md` makes actor payload shapes
  CHAIN VALIDITY, and a `seed/2` validator's `default:` arm fails a
  chain carrying `actor.qualified` as `bad_actor_event` at that
  position, while this validator accepts it: the two judge the same
  record differently, which is the discipline's own trigger, and item 1
  bumped to `seed/2` on exactly this reasoning (plans/os-8e53ffd9.md
  D8, review finding on #215). Either way a `seed/2` validator rejects
  the chain; the bump makes it reject by version (`version_unsupported`,
  a statement about the validator) rather than by `bad_actor_event` (a
  statement that the chain is corrupt). `version.Seed3` joins
  `Supported()` and `Activated`; `tuple.Applies` becomes true for
  `seed/2` and later (a named list); a new `eval.Applies(active)` gates
  the two verbs and the `eval` field at `seed/3` exactly. The
  mixed-version drill repeats: a `seed/2`-only build refuses an upgraded
  chain at the first `seed/3` record by version; existing chains verify
  byte for byte.

- **D9 — scope guard.** No ranking of tuples (offers' `tuples` scope
  stays the policy input); no calibration, gold set or authority
  suspension for VERIFIERS (item 4); no levels (item 3); no re-triage or
  approval rates (item 5); no container or cloud adapter; no change to
  `seed maintain run`. One exit-code refinement (`eval_vacuous` under
  19), no new exit. No transition row moves: an eval contract's
  lifecycle is the lifecycle.

- **D10 — an eval's chain ends at its verdict, and the tree's two
  consumers of "what is owed after a verdict" are told so.** An eval is
  never merged, so `internal/obligation` derives no `merge.requested`
  obligation on an `Eval` subject after its verdict, and
  `internal/reconcile` does not class a judged eval as `unreconciled`
  (the eval's verdict IS its terminal fact; the mint or the
  disqualification is its consequence). Both are measured by drill
  before and after: a judged eval left in `review` files no defect
  through the maintenance pass and owes nothing in `seed situation`.

## Steps

0. `next/internal/version/` — `Seed3`, `Supported()`, `Activated`;
   `next/internal/tuple/` — `Applies` for `seed/2` and later;
   `next/spec/protocol.md` — the `seed/3` register entry and the catalog
   rows for `actor.qualified` and `actor.disqualified` (D8).
1. `next/internal/eval/` (new) — `Definition` and `Load(dir)`;
   `Anchor(repo, name)` (last reviewed commit and PR from the
   repository); `Check(def, repo)` (worktree at the anchor; fixture red,
   solution green, through the verifier's runner); `Filing(def, anchor,
   tuple)` (the two payloads and the stable id); `Due(...)` (D5, with
   the receipt recomputation of D2); `Applies`.
2. `next/evals/fix-the-check/` (new) — `eval.json`, `fixture/`,
   `solution/`; `seed eval check` green on it once merged (the anchor
   needs the merge, so the drill that runs `check` against the shipped
   eval plants a committed copy in a fixture repository with a
   squash-style subject, and the post-merge run is the receipt's).
3. `next/internal/transition/` — `intent.filed`'s optional `eval`
   folded to `SubjectState.Eval`.
4. `next/internal/keyring/` — `actor.qualified` and `actor.disqualified`
   at `seed/3` positions; `Entry` gains the qualification record
   (`TS` included); `GrantTuples` returns the admissible set;
   `EverCited`; `Grants` unchanged in shape.
5. `next/internal/admit/` — `AcceptedCapabilities` rows for the two
   verbs; `authenticPass`; the two admission rules (D2, D4, the `ts`
   ordering included); the set rule's eval exemption and
   disqualified-empty refusal (D4, D6); affordance probes for the two
   verbs.
6. `next/internal/verdict/` — export the recomputation `seed verdict
   check` runs so `Due` calls the same function (a seam, no new
   behavior).
7. `next/internal/obligation/`, `next/internal/reconcile/` — the eval
   terminal (D10).
8. `next/cmd/seed/eval.go` (new) — `seed eval list | check | file |
   status | act`; `main.go` — the dispatch case; `next/lanes/
   supervisor.json` and `dispatcher.json` — summaries name the acts.
9. Drills: `internal/eval` (anchor derivation and its refusals, check,
   filing id, `Due` matrix against planted chains including the
   recomputation outcomes and the time guards); keyring (the verbs'
   shapes at `seed/2` and `seed/3`, admissible set, `EverCited`, latest
   wins); admit (every refusal in D2 and D4, the exemption, the
   disqualified-empty refusal, holder versus signer, `ts` ordering);
   obligation and reconcile (D10); `cmd/seed` (`eval check` on a
   planted eval, a vacuous one and a red-solution one; `eval act` under
   supervisor, dispatcher, operator and an unentitled key; the
   mixed-version replay); the modes fixture's end-to-end run (AC2, AC4,
   AC5).
10. Specs: new `next/spec/evals.md`; `qualification.md` (what item 2
    adds, replacing that section); `actors.md` (the two verbs, the
    first non-operator rows); `envelope.md` (the `eval_vacuous`
    refinement row); `lifecycle.md` (the `eval` field, the eval
    terminal); `obligations.md` and `reconciliation.md` (D10);
    `lanes.md` (the supervisor's and dispatcher's new acts).
11. `next/docs/progress.md`, `next/docs/decisions.md`,
    `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/version/**`, `next/internal/tuple/**`,
  `next/internal/eval/**` (new), `next/internal/keyring/**`,
  `next/internal/transition/**`, `next/internal/admit/**`,
  `next/internal/verdict/**` (the exported recomputation seam only),
  `next/internal/obligation/**`, `next/internal/reconcile/**`,
  `next/cmd/seed/eval.go` (new), `next/cmd/seed/eval_cli_test.go`
  (new), `next/cmd/seed/main.go`, `next/cmd/seed/modes_e2e_test.go`,
  `next/cmd/seed/run_cli_test.go` (the set-rule refinement's CLI
  rows), `next/cmd/seed/maintain_cli_test.go` (D10's no-defect drill),
  `next/evals/**` (new), `next/lanes/supervisor.json`,
  `next/lanes/dispatcher.json`
- `next/spec/evals.md` (new), `next/spec/qualification.md`,
  `next/spec/actors.md`, `next/spec/protocol.md`,
  `next/spec/envelope.md` (one refinement row), `next/spec/lifecycle.md`,
  `next/spec/obligations.md`, `next/spec/reconciliation.md`,
  `next/spec/lanes.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-03e47abb.json`

Nothing outside `next/**` except the work-product files above. NOT
`next/spec/transitions.json` (no row moves), NOT `next/internal/envelope/**`
(no exit allocated; the refinement is a spec row and an emitted code),
NOT `next/internal/maintain/**` or `next/cmd/seed/maintain.go` (the
maintenance pass gains nothing, D5), NOT `next/executor/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. `seed eval check` on a definition whose last commit is a squash
   merge reports the unsolved fixture RED and the solution GREEN through
   the verifier's own runner and leaves no worktree behind; a definition
   whose last commit carries no PR number refuses naming it; a dirty
   definition refuses; a planted eval whose fixture already passes
   refuses `eval_vacuous` (exit 19); one whose solution stays red
   refuses `checks_red` (exit 20). `seed eval file`'s `ref` and `gate`
   both name the anchor commit and `contract.specified` admits under
   `acceptance.md`'s equality rule unchanged.
2. In small-team mode, an eval contract filed by `seed eval file` under
   the dispatcher's key runs through the production machinery end to end
   (the supervisor's `eval act` publishes the offer, the loop claims and
   reserves, `run start` declares the worker's tuple, the work applies
   the solution under `next/evals/<name>/fixture/` in the provisioned
   worktree, the loop submits, an L1-disjoint verifier's `verdict render`
   passes with a receipt that `verdict check` reproduces), and the
   supervisor's `seed eval act` then mints `actor.qualified` for the
   HOLDER and the DECLARED tuple citing that verdict; afterwards a run
   on an ordinary contract under that tuple admits and under a tuple
   differing in one field refuses `out_of_grant`.
3. `actor.qualified` refuses at admission, each its own row with
   nothing appended: a non-eval subject; a verdict that is not
   boundary-authenticated (raw pushed by a key without `verdict`, or by
   the implementer); a fail verdict; a tuple differing from the window's
   declaration in any one field (five rows); a subject that is not the
   window's holder (the supervisor who signed the start, drilled
   explicitly); a signer holding neither `supervise` nor `operator`; a
   `ts` earlier than the cited verdict's; a second mint citing the same
   verdict. At a `seed/2` position the verb fails verification as
   `bad_actor_event`. And `Due` mints NOTHING, reporting why, when the
   cited receipt does not retrieve, does not reproduce (a raw pass
   citing an invented digest, appended by a verdict-granted disjoint
   key, is the drilled case), or carries a red transcript.
4. On an authenticated fail on an eval whose run declared T, the
   supervisor's `seed eval act` appends one `actor.disqualified` for
   EVERY actor whose admissible set holds T (two provisioned), each
   citing the verdict; afterwards a run under T on an ordinary contract
   refuses `out_of_grant` for each, an actor whose only cited tuple was
   T admits NOTHING (the bridge does not reopen), an actor also
   qualified for U still admits U, and a later passing eval under T
   re-qualifies. `actor.disqualified` refuses a tuple not currently
   admissible, a pass verdict, a non-eval subject, and an unentitled
   signer.
5. With `--spot-check-after 24h` and `--as-of` 25 hours past a
   qualification's `ts`, `seed eval act` under the dispatcher's key
   files and specifies exactly one spot-check for that (eval, tuple),
   reporting the offer as owed by the supervisor, and under the
   supervisor's key publishes it unscoped by tuple; a second run at the
   same instant appends nothing (the duplicate refuses at the boundary
   and is reported as a refusal, not an error); at 23 hours nothing is
   due; with an open eval naming the tuple nothing is due; with `0`
   nothing is ever due; a qualification whose `ts` is later than
   `--as-of` is due at once and reported as an anomaly. A spot-check
   taken and failed drives AC4; taken and passed advances the
   qualification so the next window is measured from the new `ts`.
6. `run.started` on an eval subject admits any declared tuple, a
   disqualified one included; on a non-eval subject the set rule stands
   (a mutation applying the exemption everywhere goes red).
7. `seed eval act` performs exactly the subset of `Due` its key's grants
   admit and reports the rest as owed by the other lane: the supervisor
   mints, disqualifies and offers; the dispatcher files and specifies;
   the operator does all; a key holding none refuses each `out_of_grant`
   with nothing appended, reported, never retried. `seed maintain run`
   is unchanged and appends none of these.
8. A judged eval left in `review` files no defect through `seed
   maintain run` and owes nothing in `seed situation` (D10), drilled
   against the pre-change behavior that files `unreconciled`.
9. A `seed/2`-only build refuses an upgraded chain at the first `seed/3`
   record by version, never by judging a qualification; this build
   verifies it; every pre-existing fixture chain verifies byte for byte.
10. **Mutation evidence.** Each must fail a drill: `authenticPass`
    accepting an unverified verdict; `Due` minting without recomputing
    the receipt; `Due` minting on a receipt with a red transcript; the
    mint reading the signer rather than the holder; the mint comparing
    the tuple on four fields; the duplicate-verdict refusal removed;
    `GrantTuples` returning disqualified tuples; `EverCited` returning
    false after a disqualification (the bridge reopening); the
    disqualification touching only the failing holder; the exemption
    applied to non-eval subjects; `Due` filing a spot-check with an open
    eval standing; `Due` measuring age from the wall clock instead of
    the declared `now`; `Due` treating a future-dated qualification as
    young; `Anchor` accepting a commit with no PR number; `eval check`
    skipping the red half; the `eval` field read at `seed/2`; the
    reconcile exclusion widened to every subject in review.
11. `make check` green with coverage measured **cold**, at least three
    readings above the gate, and the suites pass **unprivileged** under
    `setpriv --reuid=65534`.

**Retention set (existing, shown unharmed):**

- Item 1's rule stands on non-eval subjects: the five per-field drift
  rows, the bridge for a never-cited holder, the signer-versus-holder
  rows, `Provision`'s resolved-versus-admitted check and the offer
  `tuples` scope all keep their drills green unchanged.
- Every pre-existing fixture chain verifies byte for byte, and a
  `seed/1` or `seed/2` chain that never upgraded keeps its judgment on
  every verb (AC9).
- `seed maintain run` is unchanged in what it reaps, lints, files,
  rebuilds and checkpoints, and appends none of the new verbs (AC7);
  its no-private-powers drill stays green.
- `acceptance.md`'s gate-equality rule, the verdict pipeline's exits
  (18–25), `verdict check`'s recomputation and the red-verdict lockout
  are untouched; no transition row moves and no exit is allocated.
- The loop verbs, the lane validator and the shipped manifests keep
  their drills; `seed situation` and the projections carry no new
  obligation on non-eval subjects.

## Validation Commands

- Boundary: `cd next && go test ./internal/version/ ./internal/tuple/ ./internal/eval/ ./internal/keyring/ ./internal/transition/ ./internal/admit/ ./internal/verdict/ ./internal/obligation/ ./internal/reconcile/ ./cmd/seed/ -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1` and `make check`
  (exit checked separately from any pipe; three cold readings).

## Expected diff shape

One new package (`internal/eval`) and one new CLI verb group
(`seed eval`); two new actor verbs in the keyring and the capability
table, with `authenticPass` and two admission rules beside the run
rule's one-line exemption and one-line disqualified-empty refusal; one
optional field on `intent.filed` and its fold; the exported
recomputation seam in `internal/verdict`; the eval terminal in
`obligation` and `reconcile`; `Seed3` and the two `Applies` lists; one
shipped eval under `next/evals/`; two lane summaries. Specs: one new
file and edits to eight existing ones; one refinement row in
`envelope.md`. No change to `seed maintain run`, `internal/maintain`,
the executor, `transitions.json` or the envelope package; no
deletions of existing tests; no `plans/**` in the task PR.
