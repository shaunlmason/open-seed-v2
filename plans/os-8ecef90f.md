# Plan: next — III.L row 4 drilled on the machine-protocol surface (os-8ecef90f)

Charter III.L row 4 reads "Per-verb policy governs the machine-protocol
surface with attributable approvals." The Phase 13 exit record's plan
(plans/os-d63c7441.md) judged the row against #273 while amending and
found that the drills do not say so: `cmd/seed/serve_test.go` carries
three parity assertions, which is III.I row 3, and none asserts a
policy per verb or an approval attributable to its signer on that
surface. The card's own determination (cm-9df0f462) is that the row is
structurally TRUE and needs a drill, not an implementation. This plan
is that drill. Tier: trivial in code (tests, one table row, docs),
plan-first because it flips a conformance row the promotion gate
reads. Deps: none.

## What the tree actually shows

- **One dispatch path.** `cmd/seed/serve.go` resolves a method through
  the same registry the CLI dispatches (`reg.Resolve`) and calls the
  group's own `Run` with the params as its argv, returning the
  seed-envelope it rendered verbatim as the JSON-RPC result; a refusal
  is a result carrying the failing envelope. There is no second
  admission path to govern, so whatever governs the CLI per verb
  governs the protocol per verb by construction.
- **Policy is per verb in `internal/admit`.** The grant table
  (`keyring.AcceptedCapabilities`, `next/spec/actors.md`) admits each
  governed verb from a named capability set and refuses the rest at
  exit 14 `out_of_grant`; the declaration-driven rules (`policyRules`)
  refuse `claim.taken` above the squad's agent ceiling (`tier_above_
  ceiling`) and `intent.filed` routed to an undeclared squad
  (`routing_unknown`), reading `Context.Declaration` from `--config`
  (`$SEED_CONFIG`, `./seed.json`) exactly as the CLI's remote and
  loop verbs do.
- **Approvals are signed records.** `decision.recorded` (operator only,
  the fourth no-fallback row) and `plan.approved` land as chain records
  whose `actor` is the signer's fingerprint; `ledger show --position`
  reads it back and `ledger verify` checks the signature. The protocol
  surface carries no transport identity of its own (no authentication
  at the framing, `next/spec/protocol.md` "The machine surface"), so
  the only attribution an approval through it can have is the one the
  chain records.
- **Nothing drills either claim on the serve surface.** The three
  existing tests hold vocabulary, method set and envelope parity.

## Design decisions (binding for this task)

- **D1 — a drill, not an implementation.** The row is met by showing,
  on the protocol surface, what the card says is already true: that a
  verb refused by policy is refused there with the boundary's code,
  that its admitted twin lands, and that an approval landed there is
  attributable to its signer from the chain alone. No line of
  `serve.go`, `internal/admit`, `internal/keyring` or the registry
  moves. If writing the drill found a divergence, that would be a
  defect card, not a widening of this one.
- **D2 — two drills, by name, so the exit record can cite them.**
  `TestServeRefusesByTheSamePolicyAsTheCLI` (`cmd/seed/
  serve_policy_test.go`) fires three refusals through `serve` and
  asserts each result is the failing envelope with the CLI's own code
  for the same argv: (a) a `claim`-granted agent key answering a
  raised escalation through `decision.record` refuses `out_of_grant`,
  the grant table being per verb; (b) `intent.filed` routed to an
  undeclared squad under `--config` refuses `routing_unknown`, the
  declaration's policy being per verb; (c) `claim take` by an
  agent-kind key on a `critical` contract under a declared `standard`
  ceiling refuses `tier_above_ceiling` on a remote (claiming is
  online-only), the policy that reads the roster's kind. Each has its
  admitted twin through the same surface: the operator's decision, the
  declared squad, a human-kind key's claim. `TestServeApprovalsAre
  AttributableToTheirSigner` lands `decision.recorded` and
  `plan.approved` through `serve`, reads each back through `serve`'s
  `ledger.show`, and asserts `actor` equals the signing key's
  fingerprint and not the raiser's, that the position the write
  reported is the position the record stands at, and that
  `ledger verify` is green afterward: a fresh reader attributes the
  approval from the chain, which is what the row's "attributable"
  buys.
- **D3 — the row moves to `partial`, not `met`, and names what is
  missing.** Charter §II.14 defines per-verb policy on the machine
  surface as "allow/deny/require-approval by actor and risk class;
  approvals as request events resolved attributably in an operator
  inbox" (review on #320). The drills above cover allow and deny (the
  grant table, the declaration's ceiling and routing rules) and show
  an approval attributable to its signer; nothing in the tree is a
  require-approval mode: no declaration names a verb that needs
  approval, no request event asks for one, and nothing consumes an
  approval at admission. `next/spec/conformance.json` III.L row 4
  therefore moves from `routed` to `partial`, evidence naming the two
  drills and #273, note naming the missing mode and the card that
  builds it, **os-5781a026** (`guardrails.approvals` in the
  declaration, `approval.requested`, `approval.granted` and
  `approval.denied` as additive catalog growth, the admission rule
  that refuses a governed act without an open granted approval and
  consumes one on the admitted act, `approval.pending` in the
  operator's inbox, the loop verbs, the machine-surface drill). That
  card flips the row to `met` on all three modes together;
  `next/docs/generated/conformance.md` is regenerated here for the
  `partial`. The precedent for moving a row outside an exit record is
  os-9ef9ab34 (#308).
- **D4 — the spec names the drills.** `next/spec/platform.md`'s
  conformance section, the machine surface's spec, gains the two
  drills and a III.L row 4 line, so the surface's own document says
  what governs it and where that is proven.
- **D5 — bounds.** Tests, one table row, one generated document, one
  spec paragraph, the three docs files, the receipt. NOT `serve.go`,
  NOT `internal/admit/**`, NOT `internal/keyring/**`, NOT the registry,
  NOT any other conformance row.

## Steps

1. D2's two drills in `next/cmd/seed/serve_policy_test.go`.
2. D3: the row in `next/spec/conformance.json`; `seed docs generate
   --root ..` to regenerate `conformance.md`; `seed docs check` clean.
3. D4's paragraph in `next/spec/platform.md`; `next/docs/progress.md`,
   `next/docs/decisions.md`, `memory/LEARNINGS.md`; receipt; evidence;
   review.

## File Scope

- `next/cmd/seed/serve_policy_test.go`
- `next/spec/conformance.json`, `next/docs/generated/conformance.md`
- `next/spec/platform.md`
- `next/docs/progress.md`, `next/docs/decisions.md`, `memory/*`
- `receipts/os-8ecef90f.json`

Nothing else. NOT `next/cmd/seed/*.go` outside `_test.go`, NOT
`next/internal/**`, NOT `next/lanes/**`, NOT `.seed/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. **Policy per verb on the protocol surface.** Through `serve`, a
   verb refused by the grant table, a filing refused by the
   declaration's routing rule, and a claim refused by the declaration's
   ceiling each return the failing envelope with the code the CLI
   returns for the same argv, and the admitted twin of each lands
   through the same surface.
2. **Approvals attributable from the chain.** An approval landed
   through `serve` reads back with `actor` equal to its signer's
   fingerprint at the position the write reported, and the chain
   verifies afterward.
3. **The row reads partial and says why.** III.L row 4 is `partial`
   in the table with the require-approval mode named as the missing
   third and os-5781a026 as its card; `seed docs check` is clean on the
   regenerated `conformance.md`.
4. `make check` green; no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- The three parity drills pass unchanged; `serve.go`, `internal/admit`
  and `internal/keyring` are byte-identical to `main`; every other
  conformance row keeps its status.

## Validation Commands

- Boundary: `cd next && go test ./cmd/seed/ -run 'Serve' -count=1`
- Boundary: `cd next && go test ./internal/conformance/ ./internal/docs/ -count=1`
- Retention: `make check` (exit checked separately from any pipe)

## Expected diff shape

Added: `serve_policy_test.go` (two drills, roughly 200 lines).
Modified: `conformance.json` (one row), `conformance.md`
(regenerated, one row), `platform.md` (one paragraph), the three docs
files, the receipt. No other file.
