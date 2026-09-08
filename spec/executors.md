# Executors

The executor adapter contract (SEED-NEXT.md §II.9, charter III.H
rows 6–8; plans/os-1dad487d.md): adapters implement **provision**
(workspace + packet), **wake** (advisory channel), and **meter**
(observation-stream usage, settled to the ledger at run end), and
report the **runtime tuple** actually provisioned. The interface is
public — package `executor`, the module's one non-internal
package — and the local worktree adapter is the first
implementation; container, cloud-session, and enrolled-remote
adapters are later phases' rows. The tuple is
[`qualification.md`](qualification.md)'s five-field object
(`internal/tuple`), reported twice: `Adapter.Tuple()` is the static,
partial report of the fields the adapter controls, and `Run.Tuple()`
is what a provision RESOLVED, checked against the admitted
declaration before execution is released.

## The spend bracket

No execution path is unmetered, and no run provisions outside the
reservation gate:

1. `budget.reserve` — capacity checked and decremented at admission
   ([`budgets.md`](budgets.md));
2. `run.started` — the **spending-verb table's first entry**: strict
   `{"fence": "<position>", "reservation": "<position>"}` at `seed/1`
   and strict `{"fence": "<position>", "reservation": "<position>",
   "tuple": {…}}` from `seed/2`, where `tuple` is the runtime tuple
   the run DECLARES ([`qualification.md`](qualification.md): required
   there, refused before it); admitted only while the subject is
   `in_progress`, citing the ACTIVE claim fence and an open, **valid**
   reservation (the budget rule's spending gate applies through the
   table, and the run rule revalidates the specific citation
   position-accurately — the laundering shape); once per fence, from
   {`supervise`, `operator`}; and, from `seed/2`, only under a
   configuration the CLAIM HOLDER's `claim` grants cite, or under any
   if they cite none (the set rule): a declaration differing from every
   cited tuple in any field refuses `out_of_grant`. `seed run start`
   is the verb: it derives the fence and reservation, fills `harness`
   and `environment` from the adapter, takes `--principal`, `--model`
   and `--tool-policy` from the caller, and pre-flights through
   admission;
3. **Provision** — refuses without the admitted `run.started` the
   spec cites for its fence; and, where that start declared a tuple,
   compares the tuple the adapter RESOLVED against it field by field
   and refuses `ErrTupleMismatch` with full rollback (no worktree
   registration, no allocation) on any difference, so no workspace is
   handed out for a configuration the ledger did not admit;
4. **Meter** — usage lines on the per-fence observation stream
   ([`observations.md`](observations.md), the `units` field);
5. `run.settled` — the once-per-fence aggregate: strict
   `{"fence": "<position>", "units": "<non-negative integer>",
   "lines": "<non-negative integer>"}`, admitted only citing an
   applied claim fence (current or prior — a run can settle after
   its window closed) that carries an admitted start, from
   {`supervise`, `operator`}. Telemetry, never authority;
6. `budget.settle` — the authoritative actuals record on the
   reservation ([`budgets.md`](budgets.md)).

The tolerant fold records raw-pushed run facts as independent lists;
a fact citing a fence that is no applied claim position counts an
anomaly, never a fact. Nothing trusts run facts for authority in v0;
Phase 10's qualification applies the position-accurate boundary when
it does.

## The local worktree adapter

Provision creates a **detached git worktree** of the contract
repository at the packet's base revision, writes the handoff packet
to `.seed-run/packet.json` inside the workspace, writes the surfacing
lessons the caller derived at claim time to `.seed-run/lessons.json`
beside it (an empty list when nothing matches; a sibling file rather
than a fifth packet key, because the ledger packet's four-part shape
refuses unknown keys and the provisioned directory is the handoff, not
the fact — [`curation.md`](curation.md)), and ensures the
per-run observation directory. Git runs with fixed argument vectors;
nothing is interpolated into a shell. Wake is the documented no-op
returning nil: the wakeless poll-only drill proved polling loses
only latency, and an advisory channel that does nothing is the
honest v0. Dispose removes the worktree.

The local adapter's static report is `harness: "local-worktree/v0"`
and `environment: "detached-git-worktree"`, the two fields it
controls; principal, model and tool policy it cannot know, and it
never invents them. Its resolved tuple is those two from what it
built plus the three from the admitted declaration, and it says so: a
worktree cannot see which model a lane process will call. `Resolve` is
the post-provision seam a drill sets to an adapter that resolves
something else, which must be refused with nothing left on disk; a
container or cloud adapter resolves all five and inherits the check.

## Disposal and the loss window

**Disposal begins only after the last confirmed, admitted
synchronization** — a caller contract, drilled rather than enforced
in code. The disposability drill SIGKILLs a real worker process at a
randomized site after an admitted sync, with no graceful path, and
proves: every admitted fact survives (the chain verifies), the
contract completes elsewhere from the surviving ledger alone, and
the loss is **exactly the observation lines after the last admitted
synchronization** — the declared loss window, stated honestly. The
observation stream is lossy by design ([`observations.md`](observations.md));
anything that must survive rides the ledger.

## Preemption: graceful-first, force as the drilled fallback

> Authority: SEED-NEXT.md §II.9 preemption prose; charter III.H's
> preemption row; plan `plans/os-0f718b4e.md`.

**The interrupt request is a ledger fact.** `run.interrupted` —
strict payload `{"fence": "<position>"}` — admits only while the
subject is `in_progress`, only citing the ACTIVE claim fence, from
{`supervise`, `operator`}, once per fence (a second refuses; raw
duplicates fold as anomalies, the run-fact posture). The chain is
the canonical channel: workers already poll it for liveness (the
wakeless drill), so no marker files or adapter side-channels exist
in v0 — an adapter MAY later accelerate delivery the way wake
accelerates scheduling, advisorily. `run.interrupted` is **not** a
spending verb: preemption is supervisory control and is never
budget-gated.

**Safe-point semantics are the worker contract.** A conforming
worker checks for a boundary-valid interrupt on its active fence at
bounded intervals — at least once per metering/poll cycle, the
interval its own declared sync cadence — via the one shared
derivation (`admit.InterruptRequested`, which counts only
interrupts that passed the admission boundary at their own chain
position: a raw unprivileged interrupt parks no one). On observing
one, the worker finishes its current step (the safe point), writes
its four-part packet, and exits deliberately via `claim.parked`
([`packets.md`](packets.md); [`lifecycle.md`](lifecycle.md)) — park,
not release: preemption is the supervisor's "stop now", and
`blocked` hands routing back to the dispatch lane
(`contract.unblocked` → ready → the next claim resumes from the
packet).

**The force path still yields a packet.** A worker that ignores its
interrupt is killed (the disposability posture: nothing admitted is
lost), and the dispatch or operator lane reaps it — `claim.reaped`
with an honest four-part packet composed **from what is known**:
acceptance from the contract's specified criteria, `base` as the
zero-length range when no pushed work is known, `findings`
recording the ignored interrupt and the kill. The subject returns
to ready, immediately re-claimable, and the contract completes
elsewhere from the surviving ledger alone; `run.settled` records
the dead run's actuals afterward (a run settles after its window
closes). B-style automatic timeout reaping is the Phase 9
maintenance loop's job; it presupposes exactly these semantics, and
[`maintenance.md`](maintenance.md) is where they landed: an automatic
reap requires an admitted `run.interrupted` on the active fence (or a
`wedge.declared`) BESIDE the expiry classification, because the
observation channel is lossy and silence alone can never reap.

## The remaining substrates (Phase 13 item 2)

Beside the local worktree, three adapters implement the same public
`executor.Adapter`/`Run` contract (no method added), each resolving all
five tuple fields and refusing `ErrTupleMismatch` when the substrate
resolves anything the admitted `run.started` did not declare:

| Adapter | Harness | Environment | Budget |
|---|---|---|---|
| **container** | `container/v0` | the image digest (or `fake-oci:<digest>` under the in-process fake) | **enforced** — the supervisor stops the container |
| **cloud-session** | `cloud-session/v0` | the provider-reported runtime | **risk-limit** — a provider may bill past the reservation before the close lands |
| **remote-worker** | `remote-worker/v0` | the worker's enrolled environment | **risk-limit** — a remote process may spend past the reservation before the interrupt reaches it |

### The budget posture is honest, per adapter

`executor.Described` (optional, so the public interface is unchanged)
states whether the substrate can be stopped synchronously. An adapter
that does not implement it is treated as a **risk limit** — the safe
default. `seed doctor` lists every adapter this build provisions through
with its posture, and `report.json`'s `adapters` section names, per
adapter, the runs started under its harness and its budget posture. A
cloud or remote adapter never claims enforcement.

### The declaration names the substrates, never a secret

The `executors` block declares `container: {runtime: docker|podman|fake,
image}`, `cloud: {endpoint, credential}` and `remote: {workers:
[{name, environment}]}`. A **credential is the NAME of an environment
variable**, read at provision time; the lint refuses a token-shaped
value, so a secret pasted where a name belongs never reaches the tree.
`seed run start --adapter container|cloud-session|remote-worker` reads
the block; an undeclared adapter is refused by name.

### Credential-free CI

The container adapter provisions through `executor/fakeoci` (an
in-process OCI runtime) when the declared runtime is `fake`, so the
drills run with no runtime and no credentials. A declared `docker` or
`podman` that is not on PATH refuses by name rather than falling back:
a runtime absent in CI is a named reason, never a silent substitution.
The cloud provider answers `GET /runtime` with the runtime its sessions
report, so the adapter's static tuple carries the environment before a
session opens. The cloud and
remote adapters drill against an in-process fake provider and a fake
worker. `merge.observed` and the spend-bracket verbs are unchanged; no
executor takes inbound connectivity — the remote worker pulls.
