# Platform parity

Status: normative for `next/**`. Charter authority: SEED-NEXT.md
III.I row 3 (a verb exists on the machine protocol iff it exists on
the CLI) and row 4 (platform parity documented and tested). Plan:
`plans/os-b55e5647.md`.

Parity is a set of tested statements, never an assertion: what each
platform can run is what its drills pass, and what it cannot is
named with the reason.

## The machine surface (normative)

`seed serve` is JSON-RPC 2.0 over stdio, one request per line on
stdin and one response per line on stdout, framed as
`machine-envelope/0`. Every method is a CLI verb and every CLI verb is
a method: both surfaces are drawn from one registry
(`cmd/seed/registry`), the CLI dispatching a group's run function and
the protocol invoking the same function with the request's params as
its argv. A method is `group.subverb` (`ledger.append`, `claim.take`)
or the bare group for a verb that takes flags alone (`situation`,
`doctor`); `seed serve --list` prints the set. The one named carve-out
is `serve` itself: a CLI verb that is the protocol, not a method of it.

Params are the verb's argv: an array of strings verbatim, or an object
whose keys become `--key value` flags (`true` a bare `--key`, an array
the flag repeated, a number its text) with the reserved key `args`
appended verbatim. The result is the `seed-envelope/0` the verb
rendered, byte for byte — the exit code, the machine code, the
position stamp and the affordances included — so a refusal is a
result carrying the failing envelope, never a transport error. JSON-RPC
errors are the transport's own: `-32700` for a line that is not JSON,
`-32600` for a request without `"jsonrpc": "2.0"` and a method,
`-32601` for a method the CLI lacks, `-32602` for params of the wrong
shape. A notification (no `id`) runs and is answered by silence. The
protocol reaches the ledger by no path the CLI does not and holds no
credential the CLI would not: a key, a ledger or a remote is a param,
as it is a flag. `plan`, the one verb that reads stdin, receives an
empty stream under the protocol and takes its body from a file.

`seed-admit serve` (the forge-hosted admission service) is unchanged
and separate: it speaks signed proposals to one ref over HTTP;
`seed serve` is the general verb surface over stdio. Neither adds
semantics; both consume the CLI's.

## Paths (normative)

Every filesystem path is built with `path/filepath`, never with a
literal `/`; slash-joined strings are for refs, URLs and
repository-relative names (a packet ref, a CODEOWNERS line, a registry
path), which git and the spec define with slashes on every platform.
The lint reads the call site: an `os` file call whose argument is
joined with a literal slash or with `path.Join` fails the gate.

## Line endings (normative)

The ledger is LF-only. Every canonical form — a record's JCS bytes, a
card's canonical bytes — is LF, and the segment reader keeps a
carriage return in the line so the record parser refuses it, naming
the carriage return: a segment mangled by `core.autocrlf`, an editor
or a transfer is refused at verification, never silently normalized,
because the bytes a signature covers are the bytes on disk. A
checkout on any platform must keep `next/**` ledgers and fixtures LF.

## Capabilities per platform (normative)

| platform | cooperative | enforced-forge-hosted | enforced-self-hosted |
|---|---|---|---|
| Linux | yes | yes | yes: a git server executes the pre-receive hook; the hook drills run here |
| macOS | yes | yes | yes: the same hook, the same drills |
| Windows | yes | yes | no: no server executes the pre-receive hook on a bare Windows checkout; host the ledger on a POSIX git server or run the other two postures |

`seed doctor` reports `platform`: the OS and architecture, each
posture with its availability and reason, the available list, and
the two rules above. A platform the drills have not run on lists the
enforced self-hosted posture as unavailable with that reason — never
by assertion.

## What Windows cannot do, by name (normative)

The Windows leg's first contact named these, each stated here rather
than papered over:

- **No pre-receive hook.** Git on a bare Windows checkout does not
  execute the hook the enforced self-hosted posture is built on, so
  every hook drill skips there with that reason, and the posture is
  listed unavailable.
- **No process groups.** The verdict runner kills the check's shell
  on timeout; children the shell spawned may outlive it. On POSIX the
  whole process group dies. The runner's `PATH` is the process's own
  there (a POSIX `sh` such as Git Bash is found on it or nowhere),
  where POSIX runs under a fixed minimal `PATH`.
- **File modes are not enforced.** Drills that observe a preserved or
  a refused mode skip with that reason; the ledger's HEAD is written
  with the same mode request everywhere.
- **Replacing an open file.** Windows refuses to rename over a file a
  concurrent reader holds. HEAD replacement retries briefly; an
  artifact put that lost a rename race to a rival that landed the
  same bytes is complete (the content is addressed by its digest).
- **No advisory file locks.** The client state dir is held by an
  exclusively created lock file with a bounded wait, and a lock left
  by a dead process is taken over after that bound; POSIX uses
  `flock`.
- **The suites need `core.autocrlf=false`.** A Windows runner's git
  converts by default; the matrix sets it globally before the suites,
  and the mode and umask drills (`internal/project/boundary_test.go`)
  are Unix-only by build constraint, since Windows has neither.
- **The CURRENT pointer is written writable.** POSIX locks it to
  `0444` after the write; Windows keeps it writable, since a file
  created read-only there carries the read-only attribute that would
  block the next publication's replacement.
- **The published projection tree is not write-locked.** POSIX
  publishes a tree read-only; on Windows the read-only attribute
  would block the tree's own replacement and enforces nothing
  against a writer with the directory's ACL, so publication there
  sets no modes.
- **Line endings.** Git on Windows converts by default; the
  repository's `.gitattributes` declares LF for every text file, the
  gitref client tells git `core.autocrlf=false` and `core.eol=lf` on
  every call, every test repository is configured the same, and the
  verifier refuses a carriage return regardless.

## Open Windows residuals (normative: named, not hidden)

- The small-team lessons drill (`TestSmallTeamPromotionDeliversLessonsAtClaimTime`)
  fails on Windows at a `git checkout -B` that reports the lesson
  mirror as locally modified in the drill's clone; the cause is not
  yet understood, the drill skips there naming this, and the row
  stays open until it runs green.
- The sealed-check drill (`TestSealEndToEndCLI`) does not observe the
  flipped sealed outcome on Windows (the recompute reports the sealed
  check green after the marker flip); the drill skips there naming
  this, and the row stays open until it runs green.
- The `cmd/seed` suite runs several times slower on the Windows
  runner, because it spawns git thousands of times and process
  creation there costs about ten times what it costs on Linux
  (measured at eight times slower, twenty minutes against two and a
  half, before the client's fetch and materialize paths shed two
  spawns per append: an ls-remote and a tar; the hardening's three
  config writes stay on git's own writer, plans/os-711b3028.md D1).
  The matrix
  answers in kind: the Windows leg runs the suite in three shards
  (every Nth top-level test by sorted name, so a new test lands in a
  shard without an edit), turns Defender's real-time scanning off for
  the run, and gives each shard forty minutes.

## Tested, not asserted (normative)

CI runs the Go suites on Linux, macOS and Windows (`platform` in
`.github/workflows/check-validate.yml`, the Windows leg in three
shards); the path lint and the CRLF drill run on each; `make check`
and the coverage gate run on Linux.
Every platform-gated skip names its reason (a lint over every test
source refuses a bare `t.Skip()`), so a platform never passes
vacuously. A posture is marked available on a platform when its
drills pass there.

## Conformance

- `cmd/seed/registry`: the table; `cmd/seed/catalog.go`: every verb;
  `cmd/seed/serve.go`: the transport.
- Drills: `TestRegistryMirrorsTheCLIVocabulary` (each group's subverbs
  are the dispatcher's own usage vocabulary, both directions; run
  dispatches through the registry alone), `TestProtocolMethodsAreTheCLIVerbs`
  (`serve --list` equals the registry's derivation; the transport is
  no method), `TestServeReturnsTheCLIEnvelopeVerbatim` (a corpus of
  read, write, refusing and flags-only verbs byte-identical through
  both surfaces; the transport errors; the notification),
  `TestFilesystemPathsUseFilepath`, `TestEverySkipNamesItsReason`,
  `TestCRLFSegmentIsRefusedNotNormalized`, `TestDoctorReportsThePlatform`,
  `internal/platform`'s posture table drill.
- III.L row 4 (per-verb policy governs the machine-protocol surface
  with attributable approvals; plans/os-8ecef90f.md): the surface
  authenticates nobody and consults the boundary alone, so the policy
  that governs a verb here is admission's and the only attribution an
  approval landed here can have is the chain's signature. The allow
  and deny modes are drilled here; the require-approval mode charter
  II.14 names landed in os-5781a026 (the bullet below), and the row
  reads `met` on all three modes. Drilled by
  `TestServeRefusesByTheSamePolicyAsTheCLI` (a verb the grant table
  refuses the caller, a filing the declaration's routing rule refuses
  and a claim its agent ceiling refuses each come back through `serve`
  as the failing envelope with the CLI's own code, and each admitted
  twin lands through the same surface) and
  `TestServeApprovalsAreAttributableToTheirSigner` (`decision.recorded`
  and `plan.approved` landed through `serve` read back through `serve`'s
  `ledger.show` with `actor` equal to the signing key's fingerprint at
  the reported position, the chain verifying afterward), both in
  `cmd/seed/serve_policy_test.go`.
  with attributable approvals; plans/os-5781a026.md): the surface
  authenticates nobody and consults the boundary alone, so the policy
  that governs a verb here is admission's, in its three modes, and the
  only attribution an approval landed here can have is the chain's
  signature. The require-approval mode ([`protocol.md`](protocol.md),
  "Per-verb approval") is drilled by
  `TestServeApprovalsAreAttributableAndAnsweredOnce` (the agent's
  request landed through `serve` reads back through `serve`'s
  `ledger.show` with `actor` equal to the requesting key, surfaces as
  `approval.pending` owed by the operator lane, a claim-granted key's
  grant refuses `out_of_grant`, the operator's grant reads back with the
  operator's fingerprint, a request is answered once, and the CLI
  refuses the same argv with the same codes) and
  `TestServeGovernsAnActByApproval` (against a remote, where claiming is
  legal: the agent's governed claim refuses `approval_required` naming
  the request to file and then the operator's turn, admits under the
  grant, spends it and refuses again; a contract under the floor and
  an undeclared deployment admit as today), both in
  `cmd/seed/approval_cli_test.go`.
