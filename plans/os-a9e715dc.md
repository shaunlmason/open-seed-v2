# Plan: next — the refusal journal tells a blind retry from a corrected one (os-a9e715dc)

`plans/os-16e55c11.md` D5 words the five-bar audit's guardrail bar as
"no refusal followed by a blind retry; every claim within its
ceiling". The ceiling clause is os-b5051f2e's (plan #322). The
blind-retry clause is not the chain's to show, since a refused append
never lands, and review on #322 (chatgpt-codex-connector) found it is
not the refusal journal's either as the journal stands: an entry keeps
actor, verb, subject, outcome, code and position but no digest of the
attempted payload, so an unchanged retry and a corrected retry write
indistinguishable lines, and the report's refusal rate aggregates them
further into counts. No surface measures the clause today, and
os-b5051f2e records it as explicitly unmet rather than routed. This
card owns the measurement. Tier: standard (one journal field, one
report subsection, the seams that write the journal). Deps: none on
`main`; os-b5051f2e's `simulation.md` wording is what D4 replaces, so
the task branch carries the replacement text and merges forward when
#323 lands.

## What the tree actually shows

- **The journal is one line per boundary attempt, both outcomes**
  (`internal/refusals`; `spec/refusals.md`): `{ts, position, actor,
  verb, subject, outcome, code?}`, written best-effort by
  `journalAttempt` (`cmd/seed/attempts.go`) at eight seams, two each
  in `loop.go` (the loop verbs' refusal and success renders),
  `ledger.go` (the raw seam), `offer.go` and `seal.go`; every seam
  holds the attempted payload in hand when it journals. Loads are
  strict with the torn-tail carve-out; the journal is a declared,
  digest-covered input of the report build.
- **The report's `refusals` section counts** (`internal/project`
  `refusalsSection`): refused and admitted, breakdowns by code and
  verb, the position span, the four-decimal rate; version "10"
  introduced it and the report's version register is at "17" (the
  adapters section, `projections.md`; `project.Report().Version`).
- **The blind retry is already defined** (`modes.md`, "Convergence"):
  the same act refusing with the same code on consecutive iterations
  from a position that did not advance; an admitting next attempt is
  the first convergence arm. The loop drills assert it from a
  transcript; nothing measures it from a deployment's journal.
- **The trajectory recorder reads the journal's refused lines** for a
  lane's decision points (`internal/trajectory`) and ignores fields it
  does not frame; the recorder's corpus is a separate artifact.
- **Nothing keys two attempts as the same act.** Two attempts of one
  act differ in `ts` and, after any append, in the tip they were
  judged against, so the record's own hash (`event.Hash`, canonical
  form including `prev`) can never match across a retry; a digest that
  identifies the act must exclude the boundary's coordinates.
- **The accelerated simulation journals nothing**: its throwaway
  deployment appends through the remote posture, which keeps no
  journal by declaration (`refusals.md`, "The seams"), so the clause
  is measured on local deployments' journals, never by the simulation.

## Design decisions (binding for this task)

- **D1 — the attempt digest is the act's, not the record's.**
  `refusals.AttemptDigest(actor, verb, subject, payload)` is the
  lowercase hex SHA-256 of the RFC 8785 canonical form of the object
  `{"actor", "verb", "subject", "payload"}`, the payload embedded as
  the JSON value it is (a non-object payload, which the boundary
  refuses at the shape rule, digests the raw bytes as a string).
  Excluded on purpose: `ts`, `prev` and `v`, the boundary's
  coordinates, which a retry changes by construction; two attempts of
  the same act by the same key digest alike wherever the tip stood.
  The entry gains `digest` (`json:"digest,omitempty"`), written on
  every new line. Load accepts a line with no digest, because journals
  written before the field existed are declared inputs that still
  load, and refuses a digest that is present and not 64 hex characters
  (`line N: digest is not a sha256`), the strictness every other field
  has.
- **D2 — the seams pass the payload.** `journalAttempt` gains the
  payload parameter and computes the digest; `loopSession.refuse` and
  `refuseAt` take the payload through, so the loop verbs' refusal
  renders digest the act they refused; the raw seam, `offer publish`
  and `seal create` hand over the payload they signed. A seam that
  cannot name a payload (none exists today) would journal without a
  digest rather than journal nothing: the rate's population must not
  shrink because a field is unknown.
- **D3 — the report counts blind retries, by the established
  definition.** `modes.md` already defines the blind retry the loop
  drills assert against: the same act refusing with the same code on
  consecutive iterations from a position that did not advance, while
  an admitting next attempt is the first convergence arm. The report
  reads the same thing from the journal: in `refusalsSection`, for
  each refused entry that carries a digest, the same actor's next
  journaled attempt on the same subject is a blind retry when its
  digest equals the refusal's, it is refused with the same code, and
  its stamped position equals the refusal's; each refused entry counts
  at most once. An admitted next attempt is convergence, a refusal
  with another code or from an advanced position is a new answer to a
  new state, and neither is blind. The section gains `blind_retries`
  (the count), `blind_retries_by_code` (keyed by the code, so the
  optimistic loop's `contention` spins read apart from a `fenced_out`
  or `invalid_transition` act re-sent unchanged) and `undigested` (the
  lines that carry no digest and so could not be judged). The report
  version moves from "17" to "18" (`projections.md`'s register; the
  four drills that pin the number move with it); the cache generation
  does not, since the cache mirrors the input-free report and never
  carries this section.
- **D4 — the audit's description names the count.** `simulation.md`'s
  guardrail bar replaces the "unmet and unmeasured" sentence with the
  clause's evidence: a refused append never lands, so the clause is
  measured by the report over the deployment's journal, and
  `blind_retries` is its count, zero on a run that respects the bar.
  `simulate.Audit` itself keeps reading records alone (III.R row 5's
  contract that the ledger justifies everything); the accelerated
  simulation journals nothing and cannot show the clause, and the
  description says so in one clause, so `promotion.md` (not this
  card's) is not contradicted.
- **D5 — the recorder is untouched.** The trajectory recorder frames
  refused lines by position and reads no digest; the corpus is not
  re-recorded (the journal's shape is not a frame's).
- **D6 — bounds.** The journal stays client-side, best-effort and
  lossy by declaration; no chain content changes, no protocol version
  bumps, no admission rule changes; the remote posture keeps journaling
  nothing. NOT `next/docs/promotion.md`, NOT `.seed/**`.

## Steps

1. `internal/refusals`: `AttemptDigest`, the `Digest` field, the load
   rule; drills for the digest's invariance across `ts` and tip and
   its sensitivity to the payload, and for the load rule.
2. `cmd/seed/attempts.go` and the eight seams; a terminal drill that
   refuses an act twice unchanged and once corrected and reads the
   journal's digests back.
3. `internal/project/report.go`: the three fields, the version;
   `refusals_report_test.go` gains the blind-retry arm (a refusal
   followed by the same digest counts, a corrected retry does not, an
   undigested line is counted as such, the by-code split).
4. Specs (`refusals.md`: the field, the rule, the section;
   `projections.md`: version "17"; `simulation.md`: D4), the
   conformance row's note if III.R row 5's note names the clause,
   `next/docs/progress.md`, `next/docs/decisions.md`,
   `memory/LEARNINGS.md`; receipt; evidence; review.

## File Scope

- `next/internal/refusals/refusals.go`, `next/internal/refusals/refusals_test.go`
- `next/cmd/seed/attempts.go`, `next/cmd/seed/loop.go`, `next/cmd/seed/ledger.go`, `next/cmd/seed/offer.go`, `next/cmd/seed/seal.go`
- `next/cmd/seed/attempts_cli_test.go` (new)
- `next/internal/project/report.go`, `next/internal/project/refusals_report_test.go`, `next/internal/project/*_test.go`
- `next/spec/refusals.md`, `next/spec/projections.md`, `next/spec/simulation.md`
- `next/spec/conformance.json`, `next/docs/generated/*`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-a9e715dc.json`

Nothing else. NOT `next/internal/simulate/**`, NOT
`next/internal/trajectory/**`, NOT `next/trajectories/**`, NOT
`next/docs/promotion.md`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **The digest identifies the act.** Two attempts of one act by one
   key digest alike across different instants and tips; a changed
   payload, verb, subject or actor digests differently; every seam
   writes it; a journal without it loads and a malformed one refuses
   naming the line.
2. **The report tells a blind retry from a corrected one.** A refusal
   followed by the same actor's same-digest attempt on the same
   subject, refused with the same code from the same position, counts
   as one blind retry under that code; a corrected retry, an admitted
   next attempt (convergence), a refusal with another code or one from
   an advanced position count none; an undigested line is reported as
   such; the report version reads "18".
3. **The audit's description names the count** as the clause's
   evidence, and says the simulation journals nothing; `seed docs
   check` clean.
4. `make check` green; no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- Every existing journal drill passes; the report's existing refusal
  counts, span and rate are byte-identical for a journal without
  digests; the trajectory corpus is untouched and its drills pass; the
  cache generation is unchanged; the remote posture journals nothing.

## Validation Commands

- Boundary: `cd next && go test ./internal/refusals/ ./internal/project/ -count=1`
- Boundary: `cd next && go test ./cmd/seed/ -run 'Attempt|Refusal|Trajectory|Report' -count=1`
- Retention: `make check` (exit checked separately from any pipe)

## Expected diff shape

Modified: `refusals.go` (the digest function, the field, the load
rule, roughly +50), `attempts.go` (the parameter, roughly +10), the
five seam files (a parameter each), `report.go` (three fields and the
pairing pass, roughly +50), the version register, three specs, the
docs. Added: `attempts_cli_test.go` (roughly 100 lines), drills in the
refusals and project packages (roughly 120 lines).
