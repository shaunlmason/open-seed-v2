# acceptance.md — acceptance specs are privileged code

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §6 "Acceptance specs are privileged code (normative)";
> [`docs/build-plan.md`](../docs/build-plan.md) Phase 5 item 4;
> plan `plans/os-73c00a50.md`. Implemented by
> `internal/transition.ParseAcceptance` and the completeness stage of
> the `lifecycle` admission rule.

## The spec body is a repo artifact, referenced

A spec ultimately causes command execution on a verifier, so its body
merges through **the same review gate as code** and the contract
carries a commit-anchored reference — never inline prose that
executes. `contract.specified`'s payload deepens from 5.1's presence
rule to the structured field:

```json
{"acceptance": {"ref": "<path> @ <commit>", "executable": bool, "gate": "<pr> @ <merged-commit>"}}
```

Strict keys; `ref` uses the established combined anchor form
(classifier-exempt) naming exactly one commit; `executable` declares
whether the spec body contains runnable content, and the declaration
is explicit: only the literal booleans admit. An absent or null
marker refuses — it decodes indistinguishably from a declared
`false`, and silence must never decide whether content is armed.

## The rubric

Acceptance that cannot be a command is a **`## Rubric`** section of
the same spec body (plans/os-2e34f66a.md D1): bullets `- <id>:
<criterion>`, the id a slug unique within the spec. It is the residue
the spec could not make a command, and it merges through the same gate
as the commands: gate-before-run covers it, `plan.Rubric` reads it
exactly as `plan.Commands` reads "Validation commands", a spec may
carry both, and a spec with a rubric renders only over a scorecard the
verifier scores item by item ([`verdicts.md`](verdicts.md), "The
rubric and the scorecard"). A duplicate, empty or non-slug id refuses
at render as `spec_unrunnable`: a rubric that cannot be scored item by
item cannot decide.

## Trace attributes

A spec whose commands run a harness that attaches a trace to its run
may carry a **`## Trace attributes`** section (plans/os-7fc2ca38.md
D3): bullets `- <key>`, one attribute key each, the key one token
unique within the spec. It declares which span attributes the
receipt's trace shape retains ([`verdicts.md`](verdicts.md),
"Trace-shaped evidence"); everything undeclared is stripped, so only
what the spec names can enter a receipt that must reproduce. An absent
section declares nothing, which is valid: name, kind and status alone
reproduce. It merges through the same gate as the commands and the
rubric, read at the anchor by `plan.TraceAttributes` exactly as
`plan.Rubric` reads its section, so an implementer cannot widen what
reproduces without review, and a per-run key such as a run id stays
undeclared or the receipt stops reproducing. A duplicate, empty or
whitespace-bearing key refuses at receipt as `spec_unrunnable`: a
declaration the verifier cannot read cannot bound what reproduces.

## The gate rule: no tier exemption

`executable: true` REQUIRES `gate`, **at every tier**. The charter's
"above the trivial tier" clause relieves authorship *review process*,
but its "no text from outside the trust boundary becomes an executed
command without a gate between" carries no tier exception — and
without origin provenance, a trivial contract spawned from a
`request.*` proposal would let outside text be copied into the
referenced artifact and armed gateless. Requiring the gate
universally is stricter than the charter and therefore safe; the
relaxation for trusted-authored trivial-tier specs requires real
origin provenance, which the tier vocabulary ([`tiers.md`](tiers.md))
does not supply, and lands with that provenance, never before. `executable: false` (prose-only criteria) carries no gate —
present **iff** required.

**Gate evidence binds to the acceptance revision**: the merged commit
in `gate` MUST equal the commit in `ref` — the spec is referenced
exactly at the gate PR's merge commit, checkable as string equality
with no repository access. A mismatch refuses naming both commits: an
unrelated merged PR vouches for nothing. `gate` is an observation of
forge fact (the `plan.approved` posture); v0 validates its shape, and
reconciling it against the forge is Phase 6/8 observation machinery.

**The split, stated honestly**: this stage enforces
**gate-before-specified**. **Gate-before-run** — the verifier
refusing to execute ungated content — lands with verdicts (Phase 6),
which consumes the projection's `gated` flag below. III.F row 1's
"before it can run anywhere" is carried by that half.

## Outside text can propose, never arm

The catalog's `request.*` events are the inbound-proposal surface: a
request may carry proposed acceptance *prose*, but its payload
structurally cannot carry `executable` or `gate` keys at any depth —
proposals are data by construction, and only a `dispatch`/`operator`
actor's `contract.specified` with gate evidence arms content
(conformance III.F row 2, drilled against smuggling payloads;
imperative prose in proposals stays under the classification budget).

## Visibility and tolerance

The contracts view carries `acceptance: {ref, executable, gated}` per
specified entry (`gated` = gate evidence present or not required), so
"may this spec run?" is a projection read. Raw-pushed ungated or
malformed acceptance verifies — admission policy, not chain
validity — counts in the visible `anomalies`, and is never marked
gated.

## Conformance mapping

- III.F row 1 "A contract cannot leave draft without an acceptance
  spec; the spec's executable content passes a review gate before it
  can run anywhere; specs are protected artifacts" — the structured
  rule at `contract.specified` (gate-before-specified) here; the
  run-side half explicitly Phase 6's.
- III.F row 2 "Text originating outside the trust boundary can
  propose but never directly become executable acceptance content;
  the gate between is structural and tested against a hostile
  corpus" — the proposal shape rule and its smuggling drills.
