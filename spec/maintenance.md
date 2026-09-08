# Maintenance

The unattended maintenance loop (SEED-NEXT.md conformance III.J's
maintenance row; `docs/build-plan.md` Phase 9 item 3;
`plans/os-8a5f14bb.md`): one pass that reaps expired or wedged claims,
reconciles divergence, rebuilds projections, checkpoints, and lints,
**runnable unattended** and **audited as an ordinary actor**.

The lane's manifest is [`lanes/maintenance.json`](../lanes/maintenance.json).
Its `acts_through` is empty and that is the documented posture, not a
gap: `internal/lane` obliges only a lane holding `claim` to declare
acts, and this lane acts on the raw append seam. What it holds is
`maintenance` and `operator`.

## The pass

`seed maintain run` runs one pass in a fixed order: reap, observe,
return, re-offer, lint, file, rebuild, checkpoint. The observations
come before the lints so the lints read fresh facts, the return before
the filing so a returned subject is not also filed as a finding, the
re-offer right after the return so what the pass returned is claimable
before the pass ends, and the checkpoint is last because it attests to
the state the rest of the pass produced. The observe and return steps, and the return ceiling
that turns the fourth red return into an escalation, are
[`observations-forge.md`](observations-forge.md) (plans/os-0cd18799.md
D6, D7): with no `--forge` the observe step reports every observable
submission skipped with that reason, and a key holding `maintenance`
alone meets `out_of_grant` on both acts, reported like every refusal.

**Re-offer** (plans/os-29e2fef2.md D3). For every subject the pass
returned in this pass, it appends one `offer.published`: scoped to the
prior submitter's tuple where `internal/ranking.Resume` derives one
the holder can still take ([`ranking.md`](ranking.md) "Resume"), else
carrying the consumed offer's own tuple scope, never wider; with the
capabilities and tiers of the offer the returned claim consumed, or
`[claim]` and the subject's filed tier where none stood. The consumed
offer is derived with the listing's two predicates (authorized at its
own position, met by the claimant at the claim's), so a raw-pushed
offer lends nothing. `expires` is the append's own instant plus
`--reoffer-ttl` (default 24h): the pass takes the instant once and
signs the record at it, so the offer's expiry and its `ts` derive from
one reading and the offer cannot be born dead; not `--as-of`, which is
the classification instant and may be historical. One re-offer per
return, in the pass that returned it: a second pass over the same
subject returns nothing and re-offers nothing, and an expired re-offer
is the supervisor's to renew, because the pass returns work and does
not run the queue. The report's `reoffered` rows carry the subject,
the tuple and holder where one derived, and the expiry; a refusal is
reported like every other.

The decision logic is `internal/maintain` with its effects injected,
so every rule below is drillable without a ledger. A retry rule, a
reap rule, or a lint threshold living inside a CLI verb is a
correctness claim nothing can check.

**Why this is a CLI verb although `seed loop run` is not.** The worker
loop is a library with no verb because Seed does not own the work: the
work step is the caller's, and a verb would invite treating the CLI as
the agent ([`loop-verbs.md`](loop-verbs.md)). The maintenance lane's
work IS Seed's own. Reaping, reconciling, rebuilding and checkpointing
are defined acts over the ledger with no caller judgment inside them,
and there is no work step to supply.

**No private powers.** Every act the pass takes is signed with the
maintenance key and crosses the same `admit.Check` boundary as any
other actor's. A refused act is REPORTED, never retried and never
worked around: run the pass with a key holding only `maintenance` and
the acts needing `operator` refuse `out_of_grant` at the door. That is
what "audited as an ordinary actor" has to mean if it is to mean
anything.

**Wakeless.** No scheduler and no wake channel. The pass reads, acts,
and reports; a caller runs it whenever it likes. A loop that needed a
scheduler to be testable is one nobody could show runs unattended.

## The reap answers an unanswered request, never a timeout

This is the sharpest rule in the lane, and it is the one most easily
got wrong.

[`observations.md`](observations.md) declares the channel ephemeral and
lossy, and says plainly that **Seed holds no lease**: a claim stands
until a deliberate exit or a reap. So a dropped stream and dead work
look identical from outside, there is no expiry to elapse, and
**silence alone can never reap**.

The corroboration that makes a reap honest is a ledger fact that the
holder **was asked to stop and did not** — which is exactly the force
path [`executors.md`](executors.md) names this loop as the consumer of:
a worker that ignores its interrupt is killed and reaped, its findings
recording the ignored interrupt.

A reap therefore requires **both**:

1. an `expired` or `wedged` classification from `obs.Classify`
   ([`observations.md`](observations.md)), and
2. an admitted `run.interrupted` on the ACTIVE claim fence, or an
   admitted `wedge.declared` on it.

Both halves of (2) are judged by whether the fact passed the admission
boundary **at its own chain position** — `admit.InterruptRequested` and
`admit.WedgeDeclared`. A raw unprivileged interrupt corroborates
nothing, which is what stops silence plus a forged request from reaping
live work.

### Revocation is the third corroboration, and the only one the ledger supplies

There is one case where the chain itself — not the observation channel —
says the holder can no longer act: its key was **revoked**. From the
`actor.revoked` position on, every proposal by that key refuses at
admission, so a revoked holder can neither submit, release, park nor be
interrupted; its open claim can end no other way than a reap. So the
reap arm adds a third corroboration, `admit.RevokedHolder` (judged at
the revocation's own position, the `InterruptValid` posture, so a raw or
unprivileged revocation corroborates nothing, and a *suspension* — whose
standing can return — does not qualify):

3. the active claim's holder has a boundary-valid `actor.revoked`.

Because a revocation is a **ledger fact rather than a stream signal**, it
reaps in **every** classification state, `no_data` included — the one
place `no_data` is reaped, and only because the chain, not the channel,
corroborates it. The interrupt/wedge paths are unchanged: `no_data` with
no revocation is still never reaped, and a reap on a live, un-revoked
holder still needs (1) and (2). The maintenance pass reaps every open
claim a revocation orphans with a force packet whose finding names the
revocation, so the work is re-offered; `internal/obligation` surfaces
the same debt as a `claim.reap-owed` row, so a deployment running no
maintenance still sees the orphaned claim rather than a silent stall.

The "no deliberate exit followed" half needs no scan: every deliberate
exit leaves `in_progress`, and a reap is admissible only FROM
`in_progress` on that same fence, so a window still standing on the
interrupted fence has had no exit by construction.

**`no_data` carries no reap path whatever**, however old the claim. A
stream holding nothing looks exactly like a worker that died before its
first line AND exactly like a worker whose lossy channel dropped
everything, and corroboration does not rescue it. This is where the
instinct to reap is strongest and the evidence weakest.

**No heartbeat predicate is added.** Non-advancing observations are not
a heartbeat signature: a legitimate long-running step emits exactly
that shape, and the existing expiry/wedge classification already
distinguishes it.

So a reap means "someone asked, and nothing happened", not "long enough
has passed". That is the only corroboration a channel declared lossy
can support, and it is why there is no threshold here to tune.

**The reap's packet** is composed from what is known: acceptance from
the contract's specified criteria (or an honest statement that the fold
does not carry it), the **zero-length base range** because no pushed
work is known, and findings recording the ignored request and the
classification that decided it. `claim.reaped` is a claim-scoped event,
so the payload carries the fence citation beside the packet; a reap
that did not cite the active window would be refused, which is how a
reap aimed at a window that already closed cannot land on whatever
claim stands now.

**A settled race is the one reap the ledger itself corroborates.**
On a racing squad's contract ([`lifecycle.md`](lifecycle.md),
"Racing") the first verified success settles the contract while other
racers may still hold claims; from that position every act of theirs
but their own exit refuses at the boundary, so no observation is
needed to know their work is over. The pass reaps each settled-out
claim with `claim.reaped` citing its fence and a packet whose finding
names the settling position (`RaceReapPacket`), and reports it as
`settled`. On an `in_progress` racing contract each active claim is
classified on its own stream, holder and fence, and reaped on its own
corroboration.

## The lint set is closed

The classes are `internal/reconcile`'s, in two halves that this pass
consumes TOGETHER:

- the **record-derived** half (`reconcile.Classify`): merges without a
  verdict, skipped chain steps, unreconciled verdicts, unsealed
  above-trivial subjects, and the retroactive verdict, seal and
  override verifications;
- the **evidence-grade** half (`reconcile.Evidence`): a cited receipt
  that no longer retrieves, an observed merge that does not descend
  from the attested head, and a target ref rewritten after the
  observation.

The second half needs the artifact store and the repository, which
projection builds never read, and it is the half that sees divergence
with no record to derive it from. A pass built on the record-derived
half alone reports **clean** over a rewritten target: green, and
omitting exactly the divergence this loop is chartered to reconcile.
Both halves live in `internal/reconcile` for that reason — one
divergence surface, two callers, rather than two that drift.

This card adds exactly one class, `run_unsettled`, and **consumes** it
from `internal/obligation` rather than re-deriving it. The anchoring is
the whole subtlety: post-close settlement is valid, so the flag is
raised only once the subject has taken a subsequent claim window or
reached a terminal state. A closed-without-settle predicate written
fresh looks obviously right and files a spurious finding against every
park and reap in flight.

Phase 11 item 4 adds one more, `lesson_stale`
([`curation.md`](curation.md), "Bloat"): the latest admitted promotion
of a lesson path, unretired, expired at the pass's declared instant
for at least `--stale-after` (zero by default: on expiry itself). The
finding's subject is `<lesson path>@<promotion position>`, so the
defect's identity follows the promotion: one stale cycle files once
and the boundary refuses the duplicate on the next pass, a retired or
re-promoted lesson files nothing, and a re-promotion that expires in
its turn files new work under its own position. The loop never
retires or revalidates: it asks. The instant is the pass's `--as-of`,
the one clock a pass has.

Closed means a new lint lands by adding a class **with the spec that
pairs it to its fact**, the `factDischargers` precedent in
[`obligations.md`](obligations.md). An open-ended lint list would make
this loop a policy surface, which is what "audited as an ordinary
actor" denies.

## A finding files a defect contract, never an escalation

An escalation **freezes** a contract and demands a human decision
([`escalation.md`](escalation.md)); a lint finding is work somebody
should do. So each finding files an `intent.filed`, landing in the
ordinary queue at `backlog` like any other intent.

The consequence is worth stating rather than burying: **this loop can
create work, which is authority.** It is bounded by being attributable
(its own key, its own lane) and by filing nothing but contracts — it
cannot claim what it files, because it does not hold `claim`.

Filing is **idempotent through the ledger itself**: the defect's id is
a stable hash of the finding's class and subject, so a second pass over
the same standing finding re-files the same subject and the boundary
refuses the duplicate. The alternative is for the loop to remember what
it filed, and a maintenance loop that remembers is one that can forget.

## Checkpoints carry a snapshot a reader can start from

What a reader may trust, and the declaration that says so, is
[`checkpoints.md`](checkpoints.md); this section is the mechanism.

A checkpoint that carried only a signature over a projection hash buys
a fresh reader nothing it can spend: it could confirm that somebody
attested to a state it has no way to obtain, and would replay anyway.
So the charter requires the canonical materialization to be stored
retrievably, with its hash and location in the event under a specified,
versioned format.

`system.checkpoint`'s payload is therefore the strict object
`{format, snapshot, location, position}`:

- `format` is the versioned materialization format (`seed.projection.v1`).
  A reader that does not know a format **replays** rather than guessing
  at its layout, which is the safe direction.
- `snapshot` is the lowercase-hex sha256 digest of the materialized
  bytes, which is also its key in the artifact store.
- `location` is the store the snapshot is retrievable from (`artifact`
  in v0). A location nothing can fetch is the failure this payload
  exists to prevent.
- `position` is the ledger position the materialization was built at,
  which is what tells a reader where to resume.

The materialization is the published projection files at one verified
position, keyed `"<projection>/<file>"`. Map keys serialize in sorted
order and file bodies as base64, so the same files at the same position
always produce the same bytes and therefore the same digest: two
readers materializing the same prefix agree.

**Shape at the door, contents at the read.** Admission validates the
payload's shape and version — before this rule the boundary accepted
any payload at all, so a checkpoint could be signed, admitted, counted
in the report, and useless. It does **not** validate retrievability,
and cannot: `admit.Context` carries no artifact store, because
admission reads the ledger alone. The reader fetches the snapshot,
verifies it against the hash the signed checkpoint carries, and starts.
Saying which check lives where is the honest version of "validated at
admission".

The snapshot materializes the position it NAMES, and the checkpoint
event is appended after it, so a chain is always one record ahead of
its newest snapshot by construction. A reader comparing against the tip
rather than against the cited prefix would see a difference that is not
a divergence.

A checkpoint nobody has ever started from is a claim, not a capability,
and the only test that can tell those apart is one that starts from it.
