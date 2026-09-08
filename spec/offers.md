# Offers

The scheduling model (SEED-NEXT.md §II.9, charter III.H;
plans/os-c61c3392.md): **offers, not assignments**. The supervisor
publishes eligibility-scoped, expiring invitations to claim; workers
**pull and claim**; and the claim settles at admission — first valid
claim wins, exactly like any claim. An offer grants nothing and gates
nothing: a claim on a subject with no offer, or only consumed,
expired, or foreign offers, admits exactly as without offers. "Wake"
is advisory transport ("offers exist for you"), never the grant
itself; a worker that never receives a wake and simply polls
`seed offer list` loses nothing but latency, and the wakeless
poll-only drill in CI proves it. There is no assignment to orphan,
only offers that get **claimed or expire**; duplicate scheduling is
impossible because exclusivity settles at admission (the claim rule's
contention refusal), not at offer time.

## The verb

`offer.published` is a fact, never a transition. Payload, strict
(unknown fields refuse):

```json
{
  "eligibility": {
    "capabilities": ["claim"],
    "tiers": ["trivial"],
    "tuples": [{"principal": "acme", "harness": "local-worktree/v0",
                "model": "fable/5.1", "tool_policy": "default",
                "environment": "detached-git-worktree"}]
  },
  "expires": "2026-09-01T00:00:00Z"
}
```

- `eligibility` is required; its arrays are optional. Empty scopes
  mean unscoped: any active worker, any tier. `capabilities` names
  grants the taking worker must hold (all of them; `operator`
  standing satisfies every scope, a governance root's implicit
  operator included — scopes describe the taking lane, and admission
  already lets the operator act everywhere in it, so an operator can
  never claim work it cannot discover); `tiers` names the
  contract tiers the offer covers, matched against the subject's
  filed tier; `tuples` (from `seed/2`, each the strict runtime tuple
  of [`qualification.md`](qualification.md); refused before it)
  names the configurations the offer wants, and is met by a worker
  whose `claim` grants cite one of them, per field. A worker with no
  cited tuple, or with only others, is not among them, and operator
  standing satisfies this scope as it satisfies the rest. This is
  III.J row 3's "strongest tuples by policy" as a scheduling INPUT:
  the supervisor writes it by hand (`--tuple`) or by policy
  (`--strongest <n> --capability <c>`, the top `n` of the ranking
  [`ranking.md`](ranking.md) derives from the eval results at the
  offer's own instant, refusing `ranking_empty` when nothing ranks),
  and `seed eval act`'s offers carry it the same way. The field's
  PRESENCE is what the version gate reads: an explicit `"tuples": []`
  or `null` before `seed/2` refuses exactly as a populated list does,
  because a `seed/1` validator strictly decodes eligibility as
  `{capabilities, tiers}` and the two must agree on every `seed/1`
  record. A raw-pushed offer with a malformed member folds to
  **nothing** (an anomaly), never to an unscoped offer: a malformed
  policy must not widen into a broader one.
- `expires` is required RFC3339 and must lie **strictly after the
  event's own `ts`**: admission never reads a wall clock, so a
  born-dead offer refuses deterministically.

Admission: only while the subject folds to `ready`. A re-readied
subject (released, parked and unblocked, reaped, returned) is
offerable again **by a fresh publication**; multiple live offers on
one subject are legal. Capability: `supervise` or `operator`
([`actors.md`](actors.md)) — no disjointness constraints attach,
because an offer carries no authority the capability audit must
separate.

## Liveness: claimed or expire, fully derived

No `offer.expired` or `offer.withdrawn` verbs exist; nothing in the
fold is erased. An offer is **live** exactly when:

1. the subject currently folds to `ready`;
2. `expires` lies strictly after the liveness instant (`--now`,
   defaulting to the wall clock — listing is a live read, unlike
   admission);
3. no applied `claim.taken` landed on the subject after the offer's
   position — **a claim consumes every offer at or before it**, so a
   taken offer never resurrects when the subject re-readies inside
   its expiry window. Re-offering re-readied work carries current
   supervisor intent by construction, never a stale one.

The listing narrows the live set further by **effective readiness**
([`topology.md`](topology.md); plans/os-f0ae2cdf.md D4): a ready
subject that waits on an open dependency or sits under a blocked
ancestor lists nothing, its live offers kept folded and listed again
the moment it becomes effectively ready, with no fresh publication.
The advisory wake over that moment is `seed offer wake`, the bridge
described there; polling remains the correctness path.

## Foreign offers are inert

The tolerant fold records any well-shaped `offer.published`, raw
pushes included, exactly as it records verdicts and overrides. The
consuming surface applies the laundering countermeasure where the
fact is trusted: `seed offer list` replays the keyring to the offer's
own position and shows only offers whose signer held the
`supervise`/`operator` boundary **there**. A raw-pushed offer by any
other key folds as a fact and is inert: never listed, never
authority.

## Surfaces

- `seed offer publish --ledger <dir> --subject <id> --key <path>
  --expires <RFC3339> [--capability c]... [--tier t]... [--tuple
  <json>]...` — shapes the payload and appends through the same
  validated path as every append; each `--tuple` is parsed at the
  door with the parser admission applies, so a malformed one refuses
  as usage before anything is signed. `--strongest <n>` fills the
  scope by the ranking and `--resume` by the subject's own resumption
  ([`ranking.md`](ranking.md)); both refuse rather than widen when
  nothing derives. Refusals reuse the established admission exits; no
  new exit codes.
- `seed offer list (--ledger <dir> | --remote <repo> [--ref <ref>]
  [--state <dir>]) --actor <fingerprint> [--now <RFC3339>]` — the
  worker's poll: live offers whose eligibility the actor meets, with
  the subject's tier beside the scopes (`tuples` included, so a
  qualified worker can see what it was invited under). An inactive or
  unknown actor sees an empty list. The loop's poll consumes this
  listing rather than reinventing eligibility, so the two agree by
  construction.

  The posture pair is an exclusive-or, and the poll takes it for the
  same reason [`lanes.md`](lanes.md) gives for the orienting read:
  `claim take` is remote-only, so a poll that could only read a local
  directory would offer work in a view the claim does not land in.
  `offer publish` stays local: it is the supervisor's write, and this
  card widened what a worker READS.
- Projections ([`projections.md`](projections.md)): offer facts land
  in the contracts view (`offers`, with the `last_claim` consumption
  boundary) and the cache's `offers` table. The report carries no
  offer section: liveness is an instant-relative read, and the report
  is position-identified.
