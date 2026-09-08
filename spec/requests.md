# Requests: the ingress, read remotes, and cross-repo work

Status: normative for `next/**` from `seed/7`. Charter authority:
SEED-NEXT.md §II.15 (mirrors and dashboards propose, the ledger
decides), III.J row 2 (embedded instructions in intents, mirrors and
tool output cannot widen a lane's reachable set), III.N row 4
(federation as uniform read remotes, cross-repo work as a proposal).
Plan: `plans/os-48df10a2.md`.

A projection surface — a mirror, a dashboard, another deployment —
never writes the ledger. What it has is a proposal, and a proposal
enters the ledger by exactly one door: `request.filed`, a fact that
changes no state and grants nothing, owed an answer by the dispatch
lane. The dispatcher's answer is its own act under its own allowlist:
an intent it files for the proposal, or a decline with its reason. No
lane obeys a request.

## The two verbs (normative)

Both are defined from `seed/7` ([`protocol.md`](protocol.md)'s
register); a `seed/6` validator's unknown-verb arm fails a chain
carrying either, and at `seed/6` positions both stay unknown under a
`seed/7` validator too.

**`request.filed`** — the strict object

```json
{"origin": "<token>", "kind": "<kind>", "reference": "<ref>", "summary": "<≤200 bytes>", "about": "<contract id, optional>"}
```

- `origin` names the surface or remote the proposal came from: a
  mirror or dashboard name, or a federation remote's name in the
  filing deployment's declaration. One token, no whitespace.
- `kind` is one of `mirror-edit`, `dashboard-action`, `cross-repo`.
- `reference` names what was proposed and never carries it: a
  commit-anchored ref (`path @ commit`, the mirror edit's content in
  the mirror's own history) or an artifact digest. The grammar is
  [`packets.md`](packets.md)'s reference exemption, so
  a "reference" with prose in it is not a reference and the shape
  refuses it.
- `summary` is one line, non-empty, at most 200 bytes: a notice of a
  proposal, never the proposal.
- `about` is the contract the proposal concerns, when it concerns one.
  The record's subject is `about` when present, else `system`; a
  subject that agrees with neither refuses, and an `about` that
  resolves to no contract on the chain refuses.

**`request.answered`** — the strict object

```json
{"request": "<position>", "outcome": "filed" | "declined", "intent": "<position, with filed>", "reason": "<text, with declined>"}
```

- `request` is the chain position of a `request.filed`; the answer's
  subject is the request's.
- `filed` cites `intent`, the position of the `intent.filed` the
  dispatcher appended for the proposal, admitted after the request;
  `declined` carries `reason`. Each outcome refuses the other's field.
- A request is answered once: a second answer refuses, naming the
  first.

Neither verb changes a lifecycle state ([`lifecycle.md`](lifecycle.md)'s
table has no row for them; they are facts the fold keeps beside the
lifecycle, position, signer, subject, origin, kind, and the answer).

## Standing and grants (normative)

`request.filed` needs active standing only, like `message.sent`
([`actors.md`](actors.md)'s vocabulary has no row for it): the ingress
identity — a mirror's or dashboard's service key, a federation
remote's ingress key enrolled as a service in the target — holds no
capability, and attribution is not trust. `request.answered` needs
`dispatch` (or `operator`). A request answered by a claim-only key
refuses at the grant rule.

## The obligation and the reads (normative)

An unanswered request is the obligation kind `request.pending`
([`obligations.md`](obligations.md)), owed by `lane:dispatch`, one row
per subject carrying the oldest unanswered request's position and
timestamp, discharged by `request.answered`.

`seed situation` carries every unanswered request as a NOTICE under
`requests`: origin, kind, subject, position, size in bytes, filer,
timestamp and age in elapsed seconds — never the summary. The summary
is text a surface outside the ledger wrote, and the situation read is
the surface every lane orients from; the body is fetched deliberately
by position (`seed ledger show --position`). The projections carry the
payload verbatim, as they carry everything.

`report.json` gains `requests` ([`projections.md`](projections.md)):
the total, the unanswered count, counts by kind and by outcome
(`pending` for the unanswered), and the mean answer latency in elapsed
seconds over the answered ones — present only when the prefix carries
a request, so every projection of a chain carrying none is
byte-identical to what it was.

## The mirror arm (normative)

III.J row 2's residual — "intents and tool output are covered, mirrors
are not" — closes here: the injection corpus is fired at the ingress
(every corpus file planted in `summary`, `reference`, `origin` and the
subject), the dispatcher's reachable set with pending hostile requests
is exactly its pinned set, the situation read carries the marker
nowhere, and `request.answered` over a hostile request yields only
what the dispatcher's allowlist already permitted.
[`lanes.md`](lanes.md) records the row as met.

III.D row 5's other half joins here (Phase 13 item 8,
[`projections.md`](projections.md) "The mirror"): the mirror is a
separate projection-only component with no key, no ledger and no
admission endpoint, and its per-exporter drill files a mirror-side
edit through this ingress with the enrolled standing-only service key,
proves the request admitted and changed no lifecycle state, proves the
same key's direct coordination acts refuse out of grant, and restores
the mirror from the projection.

## Federation: uniform read remotes (normative)

A deployment reads other ledgers; it never writes them and nothing
writes it but its own admission. The declaration
([`postures.md`](postures.md)) gains a strict, read-only block:

```json
{"federation": {"remotes": [{"name": "<token>", "remote": "<git remote>", "ref": "<ledger ref, optional>"}]}}
```

`seed federation report --config <file> --state <dir>` fetches every
remote's ledger ref with the gitref client into `<dir>/remotes/<name>`,
verifies each chain from its own genesis under its own keyring (the
resolver is the remote chain's genesis; no key crosses), folds each,
and writes `<dir>/federation.json`: per remote the name, remote, ref,
whether it verified (and the refusal when not), tip, count, protocol,
halt state, contracts by state, open escalations and unanswered
requests, and the org totals over the verified remotes. Byte-identical
on the same tips. A remote that does not verify is reported as such
and never folded. The command takes no key.

Refused by construction: a super-ledger; any verb whose subject or
payload names another ledger (the verb catalog, the keyring vocabulary
and the capability audit contain nothing that does); a federation
command that takes a key; a federation read that appends anywhere.

## Cross-repo work (normative)

A source deployment's contract is proposed to a target as
`request.filed` with `kind: cross-repo`, `origin` the source's name in
the target's federation block, and `reference`
`<source-name>/<contract> @ <source tip>`; the ingress key is enrolled
in the target as a service with no capability. The target's dispatcher
answers as for any request, filing an intent that cites the request.
Nothing flows back: the source reads the target's answer through its
own read remote. Phase 13 item 5's agent-to-agent boundary is built on
this request and that read.

## Refusals

| exit | reason | when |
|---|---|---|
| 3 | `request_refused` | the shape, the subject, or the citation is wrong: an unknown kind, a reference that is not one, a summary over 200 bytes or a body, an `about` no contract resolves, an answer to no request or to an answered one, `filed` without its intent or `declined` without its reason, an intent not admitted after the request |
| 3 | `invalid_transition` | either verb on a chain before `seed/7` (the version arm) |
| 5 | `out_of_grant` | `request.answered` by a key without `dispatch` |

## Conformance

- `internal/request`: the shapes (`ParseFiled`, `ParseAnswered`).
- `internal/transition`: the facts (`Fold.Requests`, `Fold.RequestAt`).
- `internal/admit`: the `request` rule; the affordance catalog lists
  both verbs and the probes are judged by the rule.
- `internal/keyring`: `request.answered` → `dispatch`, `operator`.
- `internal/obligation`: `request.pending` owed to `lane:dispatch`.
- `internal/project`: the report section; `cmd/seed`: `request file`,
  `request answer`, `federation report`, the situation notices.
- Drills: the boundary (both shapes, the version arm, standing and
  dispatch, the subject that resolves to nothing, the second answer,
  `filed` without `intent`), the obligation and the notice, the
  corpus's request arm at the boundary and at the terminal, two
  federated ledgers with a tampered remote reported and not folded and
  no append in the state dir, and the cross-repo proposal answered by
  the target's dispatcher with a citing intent.
