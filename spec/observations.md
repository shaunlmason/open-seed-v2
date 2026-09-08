# observations.md — the ephemeral observation channel

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §5 "Traffic classification" and "Observations";
> [`docs/build-plan.md`](../docs/build-plan.md) Phase 5 item 6;
> plan `plans/os-2ff8dbf1.md`. Implemented by `internal/obs`, the
> report projection's observation section, and the `observation`
> admission rule.

## Traffic classification

Liveness, progress, and fine metering ride **ephemeral, lossy
streams** on a non-authoritative channel; they are **summarized into
ledger facts only at material transitions** (`claim.taken`,
`progress.milestone`, `wedge.declared`, deliberate exits;
`run.settled` lands with Phase 7 metering). Nothing on the channel
feeds an admission decision, and no coordination path reads it: the
ledger stays the coordination narrow waist.

## The channel and the line

The v0 channel is a per-executor directory of JSONL files,
`<dir>/<actor>/<fence>.jsonl` — one stream per claim fence, so one
enrolled key can drive several executor runs and a reaped
predecessor's heartbeats never blend into the current run's. The
default directory in an instantiation is `var/obs/`
(gitignored). Lines are unsigned by design: the stream is
non-authoritative by construction, nothing downstream trusts it for
a decision, and the ledger facts stay signed as always.

```json
{"ts": "<rfc3339>", "subject": "<contract>", "count": <int>, "step": "<state>", "units": <int, optional>}
```

`count` is the **monotonic completed-item counter**: progress is
measured by counts, never file modification time. `step` is the
**declared in-step state** for long-running work. `units` is metered
usage (plans/os-1dad487d.md; [`executors.md`](executors.md)): a
metering line is an ordinary line with units set, omitted when zero
so pre-metering streams and digests stay byte-identical; the
per-fence stream's units settle to the ledger at run end via
`run.settled`. `seed obs emit`
writes one line, creating the per-run file as needed.

## Classification: expiry vs. wedge

Classification is a pure function of the active claim's stream, a
**declared `as_of` instant**, and declared thresholds; no wall clock
is consulted, and it reads ONLY the stream keyed by the active
claim's holder and fence.

- **live**: an observation within `expiry_after` and the count
  advanced within `wedge_after`.
- **expired**: no observation within `expiry_after` — the
  no-observations condition. Reap heuristic: after a grace measured
  from the last observation, the quantity this classification already
  computes. Seed holds no lease: a claim stands until a deliberate
  exit or a reap, and the stream is the only liveness evidence there
  is. Which is why the automatic reap's rule
  ([`maintenance.md`](maintenance.md)) is not an age at all: with no
  lease to elapse and a channel declared lossy, it takes a ledger fact
  that the holder was ASKED to stop and did not. The worker loop emits its observations from its own steps
  ([`docs/build-plan.md`](../docs/build-plan.md) Phase 9
  item 1), which removes forgotten bookkeeping as a cause of silence
  and so makes this classification better evidence than it would
  otherwise be. It does not make silence PROOF: the channel is lossy
  by declaration, a dropped stream and dead work look identical from
  outside it, and this stays a heuristic for a judgment rather than a
  trigger.
- **wedged**: observations continue but the count last advanced more
  than `wedge_after` ago — observations without progress
  advancement. Reap heuristic: operator or maintenance judgment,
  with the handoff packet capturing the wedge.
- **no_data**: the active claim's stream holds nothing; absence of
  data is stated, never fabricated.

Lines stamped after `as_of` are invisible to classification: they did
not exist at the declared instant, so a clock-ahead executor cannot
classify itself live and a historical `as_of` sees only what was
there.

v0 operational defaults: `expiry_after` 900s, `wedge_after` 1800s,
overridable at the rebuild call. Executing reaps is the maintenance
lane's later item; v0 makes the conditions visible in the report.

## Declared inputs and the report

The observation snapshot is a **declared input** to `seed project
rebuild` (`--obs <dir> --as-of <rfc3339>` with threshold overrides):
the report's observation section echoes `as_of`, the thresholds, and
the **declared-inputs digest** — the RFC 8785 digest over the
snapshot's digest, `as_of`, and both thresholds together, because any
of them changes the classification — and the same digest keys the
report's stamp (`inputs`) and build id (fourth segment,
`-i<digest12>`), so ANY changed input at an unchanged tip republishes
under a new id (`projections.md`); an identity covering only the
snapshot would let a silent worker stay permanently live. An input-free rebuild publishes
`"observation": null`; every input-free projection is byte-identical
with and without inputs.

## Summarization facts at admission

`progress.milestone` — payload `{"count": <int>, "step": "<state>"}`,
capability {`claim`, `operator`}, the fence matrix applying on a
claimed subject. Admission enforces the count **strictly greater**
than the subject's last admitted milestone count, and **bounded
frequency** as a minimum spacing of **25 chain positions** since the
subject's latest milestone. Spacing is measured in positions, never
timestamps: `ts` is human-readable metadata with no ordering
authority, so a time rule would be signer-gameable and skew-prone,
while position spacing is admission-derived, replay-deterministic,
and bounds the protected quantity — the subject's share of ledger
volume.

`wedge.declared` — payload `{"observed": "<as_of>", "count": <int>,
"since": "<last-advance ts>"}`, presence-enforced, capability
{`operator`} in v0 (the maintenance lane inherits it later, the
merge.observed posture). It records the visible wedge condition
durably and **changes no state**: the claim exit remains a
deliberate exit or `claim.reaped`, packet included, and the 5.1
pinned invariant stands.

Both are facts, never transitions (`lifecycle.md`).

## Lossy by declaration

Losing every observation stream loses no coordination state:
deleting the whole channel changes no admission decision, no fold
state, and no drill outcome outside the report's observation
section, and a rebuild without declared inputs publishes a report
that says so instead of erroring. Drilled literally.

## Deferred, named

The optional per-actor ref push (`refs/seed/obs/<actor>`); the
supervisor/tailing loop; reap execution; `run.settled` (Phase 7
metering); signing of observation lines.

## Conformance mapping

- III.F "liveness, progress, and fine metering ride observation
  channels, summarized into ledger facts at material transitions" —
  the channel above plus the two admission-checked facts.
- III.F "expiry and wedging are distinct, visible conditions with
  distinct reap heuristics" — the classification section and the
  report's observation section, drilled as a truth table.
- III.F "observation channels are lossy by declaration" — the
  deletion drill.
