# simulation.md — simulation mode and generated docs

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> §II.16, III.O row 5, III.Q row 3, III.R row 7. Plan:
> [`plans/os-16e55c11.md`](../plans/os-16e55c11.md) (Phase 12 item 6).

## Generated documents

`seed docs generate` renders four documents under
[`../docs/generated/`](../docs/generated/) from the tables the machinery
reads, never from prose: `lifecycle.md` (states and every transition from
`transitions.json`), `capabilities.md` (the affordance catalog's verbs
paired with the keyring's accepted-capability sets), `exit-codes.md` (the
`envelope` package's own `Exit*` and refinement-code constants), and
`lanes/<lane>.md` (each manifest resolved through `lane.Resolve`). The
output is committed; `seed docs check` regenerates and diffs, exit 0 or
`docs_drift` (a refinement of exit 28 `drift`), and `make check-next`
runs it. The generated table is the authority for each enumeration; the
hand-written specs keep their explanations and point at it.

The capability document reads `admit.CatalogVerbs()` — the one verb list
the affordance computation itself drafts from — so a documented verb
cannot diverge from a drafted one.

## Simulation mode

`seed simulate` builds a throwaway deployment under the declared posture,
files synthetic intents from a shipped fix-the-check catalog, and drives
every lane to `done` through the real boundary with a mock executor and
**zero credentials** — no forge, no model, no network beyond a local
bare git remote. It exists so §II.16's claim is a command an adopter runs
rather than a promise.

- **The seam is the CLI's own dispatch** (`loopVerbs`): the simulation
  runs exactly the verbs an operator would. The loop lanes (planner,
  implementer) run through `internal/loop`; the other lanes and roles act
  through their ordinary verbs (`ledger append`, `offer publish`,
  `verdict render`, `merge observe`, `knowledge propose`, `maintain run`).
- **Both postures.** Cooperative validates client-side on the push;
  enforced-self-hosted installs the `seed-admit` pre-receive hook on the
  bare remote. `claim take` is remote-only, so both run on a remote.
- **The executor is `executor.Mock`**: a throwaway workspace, synthetic
  metered usage, a tuple no forge or model backs.

## The accelerated clock

`--days d` spreads the backlog's arrivals across `d` days and advances a
declared reporting instant, threaded into offer expiry and the
maintenance pass's `--as-of`. Admission reads no clock: each event is
stamped with the real wall clock, and the simulated instant feeds only
the clock-reading surfaces. The clock is a declared input, never a fact
the boundary reads.

## The five-bar audit

At the end the run audits III.R's bar from the ledger alone — never from
its own bookkeeping. Each bar names the records that violate it; a clean
run leaves every list empty:

1. **Chain violations** — the admitted chain folds without an illegal
   transition.
2. **Lost updates** — the materialized chain is non-empty and contiguous.
3. **Silent abandonments** — every `in_progress` window ended by one of
   the four deliberate exits.
4. **Guardrail breaches** — every `claim.taken` respects the guardrails
   admission enforces on the claim path: the key that sealed a
   subject's checks never claims it, and, **under the deployment's
   declaration when the audit is given one**, an agent-kind key never
   claims a contract above its squad's agent ceiling. The ceiling is
   admission policy read from `seed.json` and never carried by the
   chain, so the arm mirrors admission's rule only under a declaration
   (`simulate.AuditUnder`; `seed ledger audit --config`, below): the
   claiming key's kind from the keyring replayed to the claim's
   position, the tier and squad from the fold, the ceiling from the
   declaration; a ceiling outside the tier vocabulary is a breach, as
   admission fails closed there; a human key, an undeclared squad or
   no declaration is silence (plans/os-b5051f2e.md D1). An unoffered
   claim is **not** a breach, because admission takes one: the
   scheduling model publishes offers (`SEED-NEXT.md` §II.9) but no
   admission rule reads them, and a bar must report what the boundary
   refuses. The scheduling concern (work ready with no live offer) is
   `internal/eval`'s read, not this bar's (plans/os-aaec6a3c.md D1,
   D3). The bar's other contracted
   clause, **no refusal followed by a blind retry**
   (plans/os-16e55c11.md D5), is not the chain's to show, since a
   refused append never lands and a refusal and its retry leave only
   the retry in the chain; it is measured by the report over the
   deployment's attempts journal ([`refusals.md`](refusals.md)): every
   journaled attempt carries the digest of the act, and the report's
   `blind_retries` counts a refusal followed by the same actor's
   same-digest attempt on the same subject refused with the same code
   from a position that did not advance, the blind retry
   [`modes.md`](modes.md) defines, by the code, so a run that respects
   the clause reads zero there (plans/os-a9e715dc.md D3, D4). `simulate.Audit` itself keeps
   reading records alone, this row's contract that the ledger
   justifies everything, and the accelerated simulation cannot show
   the clause: its throwaway deployment appends through the remote
   posture, which keeps no journal by declaration, so the clause is
   read on local deployments' journals.
5. **Unreserved spend** — every `run.started` is fenced to the
   reservation it cited. The bar asks admission's own predicate,
   `admit.RunStartValid`, which judges the start's cited reservation
   at the start's own position: the strict payload, the fence against
   the active claim, that reservation's validity, and that it was not
   already closed there. A start the fold could not place cited
   nothing checkable and is unfenced by construction. Asking instead
   whether some reservation was open is weaker than the protocol,
   since a start citing a closed or absent reservation would pass
   while an unrelated one stands open (plans/os-88df7ab2.md D1, D7).

The same audit runs over any ledger through `seed ledger audit
--ledger <dir> [--config <declaration>]` (plans/os-7599c27d.md): the
chain is verified from genesis first, then the five bars are read from
the verified records, a clean chain answering with every list empty
and a violated bar refusing `audit_violated` (exit 28) naming the bar
and the records. The declaration is found by the remote verbs' own
lookup (`--config`, else `$SEED_CONFIG`, else `./seed.json` when
present, else none), so an audit and the admission it judges read one
file by one rule; a clean reading names the declaration it was judged
under (`declaration`, or `null`), and a declaration that exists and
does not parse refuses `posture_invalid` before any bar
(plans/os-b5051f2e.md D3). The arm judges every claim under the one
declaration given, which is the caller's assertion about the policy
epoch: the ledger carries no declaration digest, so a deployment whose
guardrails changed after claims landed audits each epoch with the
declaration it ran under, and the reading names which it was judged
under. The simulation declares a ceiling of its own (the core squad's
agents at `trivial`, the tier its catalog files at) and audits under
it, so its clean reading says `declared: true`: the ceiling was among
the things the audit read at the end, and among what the cooperative
client read at every claim it pushed (plans/os-b5051f2e.md D5).
`declared` states what the audit, the client, and the hook each read:
under enforced-self-hosted the `seed-admit` pre-receive hook builds
its admission contexts WITH the declaration, read at the default
branch's tip as [`postures.md`](postures.md) describes, so a raw
above-ceiling push refuses at the boundary and the audit names the
same rule (card os-0f924157, closing the review finding on #323). A
deployment that commits its declaration on the default branch — which
the simulation does, beside writing it beside the remote for the
`--config` client — gives the hook, the cooperative client, and the
end-of-run audit one file to read. This is how the shadow run's real
chain is measured against charter III.R row 5
(`docs/promotion.md`).

Each bar's violation, planted once, is caught by name (the drills in
`internal/simulate`).

## Conformance

- §II.16 "the whole system runs end to end against synthetic intents with
  mock executors and zero credentials" — `seed simulate`, `internal/simulate`,
  `executor.Mock`, drilled to `done` under both postures with the network
  and model absent.
- III.O row 5 (second half) "a decider plugs into the trajectory-prefix
  harness" — `internal/decider` and `trajectory replay --decider scripted`
  (see [`trajectories.md`](trajectories.md)).
- III.Q row 3 "docs are governed: operator handbook, generated worker
  docs, stamped design docs" — `seed docs generate`/`check`, the committed
  `generated/`, and [`../docs/handbook.md`](../docs/handbook.md).
- III.R row 7 (adopt from the README in under an hour) — the handbook is
  written against that bar; its commands are drilled.
- Phase 12 exit "the fixture organization runs a week-long simulated
  backlog (accelerated clock) meeting III.R's zero-violation bar" —
  `seed simulate --days 7`, the five-bar audit.
