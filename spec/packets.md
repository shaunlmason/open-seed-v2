# packets.md — four-part handoff packets

> Status: v0, normative for `next/**`. Authority: [`SEED-NEXT.md`](../SEED-NEXT.md)
> Part II §6 "Handoff packets (normative)"; [`docs/build-plan.md`](../docs/build-plan.md)
> Phase 5 item 3; plan `plans/os-b07b0f59.md`. Implemented by
> `internal/packet` and the `packet` admission rule.

## The packet is the only interface between executors

Every deliberate exit from `in_progress` carries a packet —
`submission.made`, `claim.released`, `claim.parked`, and
`claim.reaped` alike (the charter's "written on every deliberate exit
and every reap": a submission that fails verification returns the
contract to the pool, and the packet is what the next executor
resumes from; the reaper writes the reap packet from what is known,
so a force-kill still yields one). Silent abandonment is impossible
by construction: the exit verbs are the only way out of
`in_progress` ([`lifecycle.md`](lifecycle.md)) and each one refuses
without a shape-valid packet.

## Shape (v1): exactly four parts, all keys present

Inline JSON under the payload's `"packet"` key, beside the verb's
sibling fields (the fence citation; `submission.made`'s verifier
fields, which Phase 6 hardens):

```json
{
  "acceptance": ["<criterion>", "…"],
  "decisions":  [{"decision": "<text>", "basis": "verified|asserted"}],
  "base":       "<merge-base>..<head>",
  "refs":       ["<path> @ <commit>", "<ref> @ <range>"],
  "findings":   [{"tried": "<approach>", "outcome": "<why it failed>", "pointer": "<anchor>?"}]
}
```

1. **`acceptance`** — non-empty: a packet a successor cannot be
   judged against resumes nothing.
2. **`decisions`** — each STRUCTURALLY marked `verified` or
   `asserted`: an unmarked assertion is a shape violation, not a
   shield for upstream errors.
3. **`base` and `refs`** — the resume coordinates. `base` is a
   REQUIRED bare commit range `"<merge-base>..<head>"`, the
   diff-vs-merge-base always derivable; the no-work case is the
   unambiguous zero-length range `"<mb>..<mb>"`. `refs` entries are
   combined anchored strings (`"path @ commit"`, `"ref @ range"`) —
   bare paths refuse: they assume a shared filesystem disposable
   executors don't have. `refs` may be empty; `base` never.
4. **`findings`** — the negative knowledge a successor must not
   rediscover: `{tried, outcome, pointer?}`. May be empty (a trivial
   park has nothing to record and says so honestly); never absent.
   Findings are the curation pipeline's stage one, observations
   ([`curation.md`](curation.md)): a deliberate exit carrying at least
   one is an observation a hypothesis may cite, and
   `curation.deadend.recorded` is the same shape with the charter's
   failure condition and environment, recorded standalone inside the
   holder's window. The other direction, knowledge reaching a worker,
   rides beside the packet rather than in it: the provisioned handoff
   carries `.seed-run/lessons.json` next to `packet.json`
   ([`executors.md`](executors.md)), because the four-part shape
   refuses a fifth key and the ledger packet is the leaving worker's
   fact, not the arriving one's briefing.

Unknown keys refuse, at the top level and inside entries (the
wire-parsing precedent). `packet_ref` is RESERVED: the
artifact-store reference form lands with the artifact store
(Phase 6), and carrying it today refuses naming that phase, so the
migration adds a branch, not a reshape.

## Bounds: packets are findings, never transcripts

The packet's RFC 8785 canonical form is bounded at **3072 bytes**.
The arithmetic: the landed whole-payload cap is
`max_payload_bytes: 4096` ([`classify.json`](classify.json)), and the
packet must FIT it with room for the wrapper key and the verb's
sibling fields — an 8 KiB packet could never admit. An escalation is
one packet, one question, one decision — never a log.

Packet free text (`acceptance` entries, `decision`, `tried`,
`outcome`) falls under the classification lint's aggregate free-text
budget: packets are data, never instructions, and the hostile corpus
covers packet-shaped payloads. The anchor forms — combined refs and
the bare `base` range — are classifier-exempt references (the
exemption grammar extended to bare ranges with this change), so a
refs-heavy packet never spends its prose budget on coordinates.

## Admission and tolerance

The `packet` rule runs after the fence and before the transition
(a shape-invalid packet refuses before the transition applies), on
the four exit verbs, at `seed/1` (`seed/0` inert). Structural
violations are shape refusals naming the offending part; free-text
budget violations keep exit 9 `classification`. **No new exit
codes.** Raw-pushed packetless (or fence-violating) exits verify —
admission policy, not chain validity — and the tolerant fold applies
the exit (skipping it would wedge the subject on a dead holder)
while counting the violation in the contract's visible `anomalies`.

## Sufficiency is drilled, not asserted

The resume drill (conformance III.F): executor A completes the first
acceptance item in a real repository, records one verified decision,
one asserted decision, and one dead end, pushes, and is force-reaped
with the second item unfinished; executor B is a deterministic
function of the packet plus the instantiation's durable configuration
— the repository coordinate lives in the instantiation, never in the
packet, because anchors only mean something inside the instantiation
that recorded them — a fresh clone at the packet's anchors, no
transcript, no workspace reuse. The drill asserts B resolves every
ref from its OWN declared anchor (a commit anchor at that commit even
where the head disagrees, a range anchor at the range's head),
verifies A's finished item against its anchor, performs the
unfinished item and lands it on the remote, and never re-tries the
recorded dead end.

Escalation packets (`blocked(needs-you)` carrying packet + question
+ minimal decision) reuse this schema, landed in
[`escalation.md`](escalation.md): the question rides **beside** the
packet as a sibling payload key, never as a fifth part, and a raise
with no work to hand off uses the zero-length `base` range the schema
already spells.

## Conformance mapping

- III.F "Every exit from in_progress is deliberate; every
  involuntary exit leaves a packet; silent abandonment is
  impossible" — the exit-verb obligation plus `lifecycle.md`'s
  pinned four exits.
- III.F "Packets contain exactly four bounded parts … shape-linted
  and size-bounded" — the schema, the canonical bound, the planted
  shape drills.
- III.F "Packet sufficiency is drilled …" — the A/B resume drill in
  `internal/packet`.
