# decisions.md — Seed implementation decision log

One line per build-plan default exercised or overridden, linking the PR that
did it (docs/build-plan.md Phase 0 item 3). Decisions that needed more
than a line get an ADR under the repository's `decisions/` and a pointer
here. Newest last.

- 2026-08-30 — Phase 0 filed as **one card** (os-116ca9ac): items 1–3 are one
  coherent cluster sharing one exit criterion; later phases file one card per
  separable item. (PR #72)
- 2026-08-30 — Module path `github.com/shaunlmason/open-seed-v2`, pinned
  `go 1.25.0` for engine-toolchain parity (build-plan §0 "matches the
  existing engine"); no dependencies. (PR #72)
- 2026-08-30 — Exit codes: v1-inherited table (0/2/3/4/5/6/10/64) adopted
  verbatim; new-code allocation rule documented in `spec/envelope.md`
  (fixed default: reuse v1 conventions where semantics match). (PR #72)
- 2026-08-30 — Envelope schema identifier `seed-envelope/0`; spec split:
  `protocol.md` (canonical form, events, verbs) + `envelope.md` (envelope,
  exit codes), satisfying both Phase 0 item 2 and the fixed-defaults table
  row naming `spec/envelope.md`. (PR #72)
- 2026-08-30 — Genesis `prev` = SHA-256 of zero bytes (`e3b0c442…`); actor
  fingerprint = lowercase hex SHA-256 of the raw 32-byte Ed25519 public key;
  OpenSSH ed25519 keys accepted at key load only. (PR #72)
- 2026-08-30 — Wire encodings fixed for `seed/0` (review finding on #71):
  everything is lowercase hex (chain hash 64 chars, signature 128 chars,
  fingerprint 64 chars); the ledger record is the wrapper
  `{"event": …, "sig": …}` with canonicalization applying to the inner
  event only; OpenSSH `SHA256:<base64>` display form excluded from the
  wire. (PR #72)
- 2026-08-30 — Coverage gate (≥90% on `internal/...`) enforced from
  Phase 0, not Phase 1: enabling it early is strictly harder, never weaker;
  implemented via `-coverpkg=./internal/...` so CLI-level tests count toward
  internal coverage. (PR #72)
- 2026-08-30 — Go comments in `next/**` follow the engine's no-em-dash rule
  (spin-out parity: the code migrates to the engine's conventions);
  markdown under `next/` keeps the template repo's prose style. (PR #72)
- 2026-08-30 — `flavors/core-Makefile` refreshed alongside the `Makefile`
  edit: the validator enforces byte-identity (its refusal names the fix), so
  the mirror is a mechanical fan-out of the named integration point, not a
  second v1 surface. (PR #72)
- 2026-08-30 — Version semantics refined (review finding on #75): every
  event carries the version *active at its position*; `system.protocol.upgraded`
  is the last event of the old version; verification is against a declared
  supported-versions set, so valid older prefixes replay after upgrades;
  exit 10 covers discipline violations and unsupported versions only. (PR #72)
- 2026-08-30 — `check-next` output made byte-stable (CI finding on #72):
  tool output is captured and shown only on failure, because the
  flavor-test core-gate-independence check diffs `make check` output across
  runs and go's test timings and toolchain-download notices vary. (PR #72)
- 2026-08-30 — OpenSSH key fixtures are generated at test runtime
  (deterministic seed, `t.TempDir()`), never committed: forge push
  protection refuses private-key-shaped bytes even when synthetic, and the
  loaders only care about the wire format, which `ssh.MarshalPrivateKey`
  produces. Standing rule for every later fixture (verifier keyrings,
  sealed-check keys). (PR for os-aa146827)
- 2026-08-30 — Strict wire parsing (review findings on #76): records refuse
  unknown fields, duplicate keys at any level, and trailing data; hex
  encodings refuse uppercase rather than normalizing. Spec amended in the
  same PR (a normative addition beyond the plan's file scope, made under
  the review-fix allowance and noted here). (PR #76)
- 2026-08-30 — First module dependencies for 1.1: `github.com/gowebpki/jcs`
  v1.0.1 (RFC 8785 canonicalization; writing correct JCS by hand is
  subtle-risk with no upside) and `golang.org/x/crypto` (OpenSSH ed25519 key
  parsing); both boring, both pinned by go.sum. Ed25519/SHA-256 themselves
  stay stdlib. (PR for os-aa146827)
- 2026-08-30 — Ledger signature checks take a `Resolver` seam
  (fingerprint to public key): the keyring is Phase 3's projection, genesis
  bootstrap and tests inject fixture resolvers; verification stays pure and
  the keyring lands as a parameter, not a rework. (PR #79)
- 2026-08-30 — HEAD reconciliation reads the whole segment stream in v0
  (correctness first); the segment-seek optimization the plan sketches is
  deferred until the Phase 12 performance budgets exist to justify it.
  (PR #79)
- 2026-08-30 — Classification v0 bounds (plan os-d6f81ec6): canonical
  payload 4096 bytes, string 512 bytes, aggregate non-reference text 1024
  bytes, depth 8, array 64, base64 run 256; the anchor exemption requires
  a filename-alphabet path (no whitespace, 200 bytes max); pointers are
  RFC 6901-escaped; lint canonicalizes (JCS) before walking. (PR #80)
- 2026-08-30 — Genesis payload carries raw root keys hex-encoded and is
  the trust bootstrap (Resolver refuses fingerprint/key mismatches and
  signers outside the root); init refusal is exit 3 with machine code
  `ledger_not_empty`; smallest-home decision placed genesis in its own
  `internal/genesis`. (PR #83)
- 2026-08-30 — Halt state is a pure chain projection (State{Halted, By,
  Reason}; no flag file); malformed halt events are skipped in replay
  (admission refuses them at the boundary; replay must not wedge on
  history it did not admit); exit 7 allocated. (PR #84)
- 2026-08-30 — Exits 8 (`chain_invalid`) and 9 (`classification_refused`)
  allocated; error body stays {code, message} with "position N: reason:
  detail" rendered in, structured error data deferred to a versioned
  envelope bump (per #82 review). Reading records needed a public
  iterator: `ledger.Records` added as the smallest read surface (a plan
  gap, noted for review; genesis Bootstrap and `show` consume it).
  (PR #85)
- 2026-08-30 — gitref rides the git CLI (the engine's gitx pattern, no
  module deps); the loop verifies the fetched stream (full replay) before
  the head cache advances, correctness over speed until Phase 12 budgets;
  lost races surface as two rejection shapes (stale-parent non-fast-forward
  and mid-push ref-lock contention), both mapped to the same retry path.
  (PR #86)
- 2026-08-30 — Admission refusals wrap in `admit.Refusal` (rule name +
  `Unwrap` to the underlying typed error) so the exit map (7/8/9/10)
  survives composition; context is built by one verified replay
  (`ledger.WithObserver` feeds the halt projection); the version rule
  stamps refusals at the tip position. (PR #90)
- 2026-08-30 — Exits 11 `remote_rejected` and 12 `head_regression`
  allocated (lowest unused, one meaning per code): the remote's own
  admission refusal carries its reason verbatim, and a rollback is a
  freshness refusal, never misread as corrupt ledger data; loop
  exhaustion maps to exit 2 `contention`. The pre-flight's verified tip
  persists via the new `gitref.RecordVerifiedHead` before the loop runs,
  closing the fresh-client rollback window between the two fetches. The
  update-phase rejection shape ("failed to update ref") is a race marker
  again: the 2.2 CLI race drill showed a rival landing between
  advertisement and update produces it server-side, while hook declines
  keep surfacing as `ErrRemoteRejected`. `AppendLoop` grew a trailing
  variadic `ledger.VerifyOption` so per-attempt re-verification honors
  the caller's supported set. (PR #93)
- 2026-08-30 — seed-admit's division of labor: one full
  VerifyFromGenesis proves parse, linkage, signatures, actor resolution,
  version discipline, and upgrade schemas for every pushed record; the
  per-new-record admission pass then applies only what verification
  tolerates in history (halted, halt shapes, classification), and a
  record-level prefix check pins append-only-ness, since commit
  fast-forward alone would admit a descendant commit whose tree rewrites
  admitted records. Actor and version rules are not re-run per record:
  the completed replay already enforced them. (PR #94)
- 2026-08-30 — Drill fixtures select postures through the production
  declaration (`internal/posture.Load`), never a test-only switch; the
  cooperative half of the adversary drill asserts the landed raw push
  (the named consequence observable) beside the client's local refusal;
  kill-and-replace compares the hook's refusal lines byte-for-byte
  across a host rebuilt from a fresh mirror clone (replicated Git data
  only, so no host-local state can leak into the comparison). (PR #99)
- 2026-08-30 — Posture declaration is client/deployment state in a
  strict JSON file (never ledger content; the guarded ref's tree stays
  HEAD + segments); the charter's cooperative consequence lives as one
  exported constant that the doctor quotes verbatim (machine field and
  operator-facing stderr both); undeclared refuses at exit 4, invalid
  declarations at the newly allocated exit 13 `posture_invalid` (spec
  table row + envelope constant land in the same change, per the
  lowest-unused rule; exit 3 stays reserved for illegal transitions);
  the forge-hosted posture names its Phase 12 gap honestly. (PR #98)
- 2026-08-30 — v1-loop delegation for `next:` cards: operator queue verbs
  (`promote`; `plan-unblock` once the gate PR is genuinely merged) run under
  the session principal (`shaunlmason`), work verbs under
  `seed-next-implementer`; implementation may be prepared ahead of the plan
  merge as a draft PR when the owner is merging in batches (CI's
  plan-at-merge-base gate still orders the merges). ADR:
  `decisions/0003-next-loop-delegation.md`. (PR #72)
- 2026-08-30 — The keyring semantics activate behind a **`seed/1`
  boundary** (review finding on #97): they are a validation-rule change a
  conformant `seed/0` validator judges differently, so per protocol.md's
  own bump discipline `actor.*` records at `seed/0` positions stay inert
  and grandfathered, an `actor.*` draft at a `seed/0` tip refuses as an
  illegal verb, and `version.Supported()` ({seed/0, seed/1}) seeds every
  default supported set. `version.Protocol` (the genesis default) stays
  `seed/0` until a recorded decision moves it. (PR #100)
- 2026-08-30 — One transition function: `keyring.Advance` owns actor
  payload shapes, standing legality (re-enrollment reinstates a
  suspended actor; revocation is terminal; root liveness never leaves
  zero active roots — review finding on #97), and effects, consumed by
  verification replay (chain validity, `bad_actor_event` at the failing
  position) and admission preview alike. The interim root-only
  authorization for `actor.*` verbs is **admission policy** like halt
  and classification — verification tolerates it in history (the
  cooperative posture's named consequence) — so 3.2 swaps it for grant
  checks without another protocol bump; exit 14 `out_of_grant`
  allocated for the refusal the charter names. (PR #100)
- 2026-08-30 — Capability vocabulary v0 (plans/os-3979d48b.md): rows are
  **sets of accepted capabilities** per verb (any one admits), mirrored
  from the normative table in spec/actors.md and pinned by test;
  halt declare/lift, protocol upgrade, and actor.* accept `operator`
  only, `system.checkpoint` accepts `maintenance` or `operator` (the
  charter's maintenance loop must not hold operator authority);
  governance roots hold `operator` implicitly; grant-level withdrawal
  short of ending standing is deferred until the catalog grows a verb.
  The no-protocol-bump stance for admission-policy evolution (recorded
  at 3.1, argued in the plan's versioning section) is applied: the
  grant rule replaces root-only authorization at `seed/1` with no new
  version. (PR #102)
- 2026-08-30 — The revocation drill's shape (plans/os-d1f35a8c.md): the
  working era runs through the legitimate cooperative client against
  the enforced boundary, hostile eras push raw; standing-before-grants
  is proven by granting the doomed key a capability before revocation
  and asserting it confers nothing after; terminality and root liveness
  are asserted at the boundary, not only in the library. The Phase 3
  exit record claims only the III.E subset and enumerates every unmet
  criterion with its landing phase (per #103 review): honest conformance
  bookkeeping outranks a tidy exit line. (PR #104)
- 2026-08-30 — Projection publication scheme (plans/os-4d5cacff.md): a
  directory rename cannot atomically replace a non-empty directory and
  delete-then-rename opens the window atomicity forbids (review finding
  on #105), so publication is **immutable builds plus a pointer**:
  `<out>/<name>/builds/<position>-<tip12>/` trees named by the stamp
  (identical prefixes reproduce identical ids) with an atomically
  renamed `CURRENT` file; the pointer swaps only after the tree is
  complete; superseded builds and stray partials prune after the swap;
  a killed build leaves at worst an orphan. Stamp conventions:
  `projection.json` carries the verified record **count**, the CLI
  envelope stamps the tip's zero-based **index** (count-1), both stated
  in spec/projections.md so consumers never conflate them. (PR #109)
- 2026-08-30 — Roster candidates derive from the chain itself
  (plans/os-4d5cacff.md): the genesis payload's governance roots plus
  every enrollment subject, resolved through `keyring.StateAt` — every
  keyring entry appears, roots included (`root: true`, empty kind and
  name, per #105 review), and the projection stays a pure function of
  the records without adding a keyring iterator the plan's file scope
  did not name. (PR #109)
- 2026-08-30 — Build identity carries the derivation version; the
  superseded build survives one swap (review findings on #109). The id
  is `<position>-<tip12>-v<version>`: a projection whose build logic
  changes bumps its version and republishes at an unchanged tip, so
  the same-id discard can never preserve obsolete semantics; and the
  build CURRENT named before a swap is retained through the prune so a
  reader that resolved just before the swap still holds a complete
  tree (older builds and stray partials prune; losing two consecutive
  swaps means re-resolving). The stamp gains the version field, giving
  consumers the derivation identity beside the position. (PR #109)
- 2026-08-30 — Standard-projection semantics (plans/os-fecfb3f7.md): the
  v0 work classifier is the prefix rule (everything outside `system.*`
  and `actor.*` is work vocabulary) until Phase 5's transition table
  replaces it with explicit vocabulary; the queue publishes with
  `derivation: "none"` so an underived queue is machine-distinguishable
  from "nothing ready" (per #106 review); contract payloads are
  content-preserved but re-indented by the view (canonical bytes live
  only in the ledger, and the spec says so); exit 15 `stale` is
  allocated for the minimum-position demand, spec row and constant in
  one change per the allocation rule. Two position conventions coexist
  deliberately: stamps carry the verified count, tip-stamped envelopes
  carry the zero-based index; `seed project current` reports the count
  verbatim and the spec states both. (PR #111)
- 2026-08-30 — Consumer-verb refusal discipline (review findings on
  #111): `seed project current` resolves only registered projections
  (a name outside the registry is not_found whatever directories
  exist, which also keeps traversal components out of the path), and
  splits absence from damage: nothing published refuses 4 not_found,
  while a layout that exists but cannot resolve (unreadable or empty
  CURRENT, unreadable or unparseable stamp) refuses 5 unavailable, so
  automation branching on exit codes never mistakes a damaged
  publication for an unpublished one. The verb and the rebuild rows
  also surface the stamp's derivation version. (PR #111)
- 2026-08-30 — Refusal discipline round two (review findings on
  #117): absence means the projection's own directory is absent — a
  layout that exists without its CURRENT pointer is damage (5), since
  publication swaps CURRENT atomically and a published pointer never
  vanishes on its own; a stamp that parses but is incomplete or
  inconsistent (wrong name, empty version, tip contradicting
  position) is the same damage as an unparseable one, so a zero-value
  stamp cannot satisfy even --min-position 0; and the stale refusal
  stamps its envelope position with the observed count, per the
  envelope contract that every response computed at a verified
  position carries it structurally. (PR #117)
- 2026-08-30 — Write-boundary enforcement shape (plans/os-8d5e9c45.md,
  amended per #107 review): three layers — the vocabulary lint, the
  seam/write-separation lint (a non-test file importing the engine
  makes no os write-family calls; per-file because cmd/seed's importer
  and writers are legitimately different files of one package), and
  locked publication (0444 files, 0555 directories including the
  projection root, engine-only 0755 window around the swap). Both
  lints are one go/parser test in the engine suite, self-checked on
  planted fixtures, which is what "wired into check-next" means: the
  suite is the gate. Deletion becomes mode-walk-then-remove and the
  sanctioned recovery stays `seed project rebuild`, which runs the
  walk itself. Residual risks named in the spec instead of papered
  over: cross-file splits (closed by modes), direct syscall, and
  chmod-capable owners (uid 0 bypasses modes entirely, so the
  OS-refusal drills gate on an unprivileged runner; CI provides one).
  (PR #112)
- 2026-08-30 — Boundary hardening round two (review findings on #112):
  the output root itself locks 0555 between rebuilds because rename
  permission lives in the parent (a writable parent let a whole
  projection root be renamed away, evading both per-file lints); the
  window opens only after verification so refuse-before-write holds,
  and every return path relocks via defer, failed publications
  included (only a killed process leaves a window open); every
  published mode is set by explicit chmod so the process umask cannot
  weaken the protocol (WriteFile's mode argument is umask-masked).
  Renaming the output root from ITS parent stays outside the engine's
  ownership, equivalent to repointing --out, and is named residual in
  the spec. (PR #112)
- 2026-08-30 — Boundary hardening round three (review findings on
  #118): a partial window open rolls itself back — openDirs relocks
  the directories it already opened before surfacing the error, so a
  refusing directory (builds/ occupied by a regular file) never
  strands a projection root writable until some later successful
  rebuild; and the lint vocabulary derives from the engine's own
  declarations (CurrentFile, StampFile) instead of re-typed literals,
  with the unexported builds directory pinned behaviorally against a
  freshly published layout — unexported deliberately, since exporting
  it would hand non-engine code the very path piece the lint denies.
  (PR #118)
- 2026-08-30 — The cache is registered projection number six
  (plans/os-acc1ac78.md as amended by #110): modernc.org/sqlite
  v1.57.0 pinned; byte-determinism engineered by closing the variance
  sources (one connection, one ordered transaction, rollback journal,
  fixed page_size, no auto_vacuum/ANALYZE/AUTOINCREMENT) and enforced
  by the registry's existing byte-identical drill; the stamp table
  derives the same (name, position, tip, version) the engine stamps
  in projection.json (position = len(records), tip = last event hash,
  both provably equal the verification report); PRAGMA user_version
  carries the table-set generation. A same-id republish deliberately
  keeps the existing tree for readers that hold it, so tamper
  recovery is the documented deletion walk plus one rebuild; the
  locks are the anti-tamper layer, and the drill proves the poison is
  never an input. (PR #119)
- 2026-08-30 — The lifecycle contract is data (plans/os-d69a6c91.md):
  transitions.json is the normative copy with a byte-identical
  embedded twin (the classify.json precedent) and refuses to load
  unless self-validation passes — one initial state, one birth verb
  landing on it, no terminal outgoing rows, no duplicates, full
  reachability, no wedge, and the in_progress exits pinned to the
  four deliberate ones. Legality is admission policy at seed/1
  (seed/0 inert, the keyring precedent): the lifecycle rule folds
  the subject's history and refuses illegal (state, verb) pairs as
  exit 3 naming subject, state, and verb; completeness presence for
  intent.filed/contract.specified refuses at shape; capability lanes
  dispatch/claim gate every lifecycle verb with cancellation and
  merge.observed operator-only in v0. Raw-pushed illegal history
  verifies and surfaces as per-contract anomaly counts; the fold
  feeds contracts v2 (state + anomalies), queue v2 (transitions/1
  ready set, oldest first), and cache generation 2 — the derivation
  bump exercising Phase 4's version-in-identity machinery in anger.
  The hook's per-record context now carries the fold, so the shared
  rule set enforces at the boundary too. (PR #122)
- 2026-08-30 — Claims are fenced, structured, and online-only
  (plans/os-5dc16a7c.md): the fence is the admitted claim.taken
  position — derived, never asserted — cited as {"fence": "<n>"}; on
  a held subject the four deliberate exits must cite it, so must
  free events from the holder or any prior claimant (a reaped worker
  cannot demote itself to observer, the #114 review binding), any
  citation present must match the active fence whoever signs, and
  outside a claim window citing a fence refuses because fences die
  with their windows. The fence rule slots between grant and
  lifecycle per the charter's check order, so out_of_grant beats
  fenced_out beats invalid_transition, and citations are required
  only for the verbs the table allows out of the held state — an
  illegal jump stays exit 3, never a misleading 6. Contention is the
  lifecycle rule's structured exit-2 refusal naming holder and fence
  (a holder double-claiming loses too); the offline exclusive-verb
  refusal reuses exit 2's own phrase (exclusivity not granted) at
  the dev-tool seam, with the boundary enforcing regardless. The
  contracts view carries {holder, fence} while in_progress
  (Version 3), the cache mirrors it (generation 3), and the race
  storm drives six concurrent claimants through the full rule set
  against a real remote: one winner, structured losers, a verified
  chain. (PR #123)
- 2026-08-31 — Packets are the only executor interface, enforced
  (plans/os-b07b0f59.md): all four deliberate exits carry an inline
  four-part packet under the payload's packet key — acceptance
  non-empty, decisions structurally marked verified/asserted, the
  mandatory bare base range (zero-length for no work) with combined
  anchored refs, findings possibly empty but never absent — strict
  keys, 3072 canonical bytes fitting the 4096 payload cap, and
  packet_ref reserved for the Phase 6 artifact store. The packet
  rule runs between fence and lifecycle so a shape-invalid packet
  refuses before the transition applies; the classifier's reference
  exemption gained the bare commit-range form (exported predicates
  keep packet and classifier grammars from drifting); and the
  tolerant fold now counts raw-pushed fence violations and
  packetless exits as visible anomalies while still applying the
  exit, because skipping it would wedge the subject on a dead
  holder. Sufficiency is the A/B resume drill: B, a function of the
  packet alone in a fresh clone, completes the acceptance list,
  reproduces the artifacts from the anchors, and never re-tries the
  recorded dead end. (PR #124)
- 2026-08-31 — The spec gate is structural and universal
  (plans/os-73c00a50.md): contract.specified carries the structured
  acceptance object — commit-anchored ref naming one commit, the
  executable flag, gate evidence present iff executable and bound to
  the ref's exact revision by string equality (an unrelated merged
  PR vouches for nothing) — with no tier exemption: the
  provenance-gated trivial-tier relaxation waits for the tier
  system. request.* payloads structurally cannot carry executable or
  gate keys at any depth (outside text proposes, never arms), the
  fold records {ref, executable, gated} per subject with raw-pushed
  ungated content visibly anomalous and never marked gated, and the
  contracts view (Version 4) plus cache (generation 4) publish it so
  Phase 6's verifier reads "may this spec run?" from a projection.
  The same bump is the republish trigger for 5.3's fold change
  (raw-pushed fence violations and packetless exits now counted as
  anomalies): that change shipped in #124 without a derivation-version
  bump, so a prefix published under Version 3 from a ledger holding
  such raw records would rebuild to different bytes under the same
  build id and be discarded as a duplicate; Version 4 / generation 4
  re-key every such rebuild (#124 review). Gate-before-specified
  enforced here; gate-before-run named as Phase 6's half. (PR #125)
- 2026-08-31 — Plan-gating is ordering plus citation at admission,
  ancestry at the verdict (plans/os-16c1d142.md): the fold tracks
  the filed tier and plan.approved facts (plan.* verbs are facts,
  not transitions — the pinned four exits stand), and above the
  literal "trivial" tier a submission refuses exit 16 plan_required
  without an admitted approval AND the cited plan anchor; Phase 6's
  receipt (plan hash at merge-base) closes the
  implementation-before-approval window, named not silent. The
  falsifiable-plan lint is a pure reader of the repo's own plan
  shape with missing-retention the charter-quoted distinct finding;
  seed plan classify is the CI-invocable disjointness check refusing
  mixed change sets at exit 9, with forge-required wiring assigned
  to Phase 12's protections reconciler. plan.proposed rides the
  claim lane under the existing fence matrix; plan.approved is
  operator-attested v0 like merge.observed. (PR #126)
- 2026-08-31 — Coverage collection in check-next serializes package
  test binaries (-p 1), both Makefiles in lockstep: under the
  subprocess-heavy drills, concurrent test binaries can collide
  coverage counter files (same pid and second after heavy pid
  recycling), silently dropping one package from the merged profile
  and misreading totals far below truth (79-89% readings against a
  stable 91.1%). The meter now measures the same number every run;
  the gate itself is unchanged. (PR #125)
- 2026-08-31 — 5.3's post-merge review round hardened in place
  (os-b07b0f59 follow-up): packet array parts must be literal arrays
  (a null decodes into the same nil slice an absent key does and
  admitted past presence checks); the resume drill now drills what it
  claims — every ref resolves from its OWN anchor (the fixture makes
  the base and head disagree about the bytes, and the range form is
  exercised at the range head), A dies between acceptance items so B
  performs and durably lands genuinely unfinished work, and B's
  repository coordinate comes from the instantiation's durable
  config, never A's environment: the packet carries anchors into the
  instantiation, not the instantiation's address. The round's
  projection-version finding rides #125 (Version 4 / generation 4,
  noted there).
- 2026-08-31 — 5.2's post-merge review round (os-5dc16a7c follow-up):
  the fence rule's exclusive-verb bypass returned before citation
  validation, so a claim.taken on an unheld subject could smuggle an
  asserted or retired fence citation past the "outside in_progress,
  citing one refuses" rule the spec already states. The bypass now
  applies only on a held subject (where a rival claim is contention,
  the lifecycle rule's refusal); claimless citations on the claiming
  verb refuse exit 6 like any other. The round's fold finding was
  already closed by #124's anomaly counting, with apply-and-count
  over skip the recorded decision (a skipped exit wedges the subject
  on a dead holder).
- 2026-08-31 — 5.1's post-merge review round (os-d69a6c91 follow-up):
  the lifecycle fold now honors the seed/1 activation boundary the
  spec and admission already stated (records under seed/0 are
  grandfathered inert, the keyring.Applies posture). The fold
  filtered by verb name alone, so an upgraded ledger's pre-activation
  events would occupy states and make the real seed/1 filing refuse
  as a second birth; version discipline pins e.V to the version
  active at each position, so the filter is one record-level check.
  Every fold-consuming derivation bumps with the corrected fold
  (contracts 5, queue 3, cache 5): a published projection rebuilds to
  different bytes for such a ledger, and only a new version re-keys
  the build id so the correction republishes at an unchanged tip
  (#129 review, the #124-round principle).
- 2026-08-31 — Observations are declared inputs, never ambient
  (plans/os-2ff8dbf1.md): the v0 channel is per-executor JSONL
  streams keyed <actor>/<fence> under var/obs/ (the existing
  var/ ignore rule already covers the directory), unsigned and lossy
  by declaration; classification (live/expired/wedged/no_data) is a
  pure function of the active claim's own stream, a declared as_of,
  and the spec'd thresholds (900s/1800s), with no wall clock in any
  build. The engine grew one seam: Builders take Inputs, only the
  report declares consumption (Version "2"), and an input-bearing
  build keys its stamp and build id with the snapshot's RFC 8785
  digest (-i<digest12>) so changed inputs republish at an unchanged
  tip while input-free projections stay byte-identical by
  construction. progress.milestone admits strictly advancing counts
  at a 25-position minimum spacing (position-derived, never ts,
  which is metadata with no ordering authority); wedge.declared is
  an operator fact with presence-checked evidence; both are facts,
  never transitions. The ungoverned-verb test specimen moved from
  progress.milestone to message.sent, since milestones now carry a
  capability row.
- 2026-08-31 — The verdict pipeline's first half binds at admission
  and runs in declared isolation (plans/os-f6d2c267.md, task PR for
  os-f6d2c267). verdict.rendered is piped: a fact admitted only on
  review subjects, strict payload {verdict, receipt, submission,
  independence: "L1"}, bound to the fold-recorded submission
  {position, signer}, and refused exit 17 not_independent when the
  signer is any claimant or the bound submission's signer — the one
  capability row without an operator fallback (III.G: override is
  6.4's own verb; roots that judge hold explicit verdict grants).
  The verifier workspace is an origin-stripped local clone, never a
  git worktree (a worktree's .git link shares the parent's refs and
  objects, handing hostile spec commands update-ref reach back into
  the host; review finding on the plan), and the charter's "sandbox
  with declared, minimal capability" lands as a runner profile named
  in every receipt: v0 exec scrubs the environment, bounds each
  command with a process-group-killing timeout, and declares
  network: unrestricted honestly rather than pretending a boundary
  portable no-root Go cannot enforce; namespaced profiles slot in at
  the Phase 7 adapter seam. Receipts are JCS-canonical
  {contract, merge_base, head (full immutable SHAs, head must
  descend from merge-base), plan-at-merge-base | null, diff_sha256,
  files, transcripts (output digests, never inline bytes),
  environment}, stored content-addressed under var/artifacts
  (the git-addressed refs/seed/artifacts push stays deferred, the
  observation-channel precedent). Render derives the permissible
  verdict from the transcripts it just executed — pass over any
  nonzero exit refuses the new exit 20 checks_red naming the
  command, fail stays renderable, prose-only pass stays explicit
  judgment — because the verifier is the only party holding what it
  just ran; a raw-pushed pass-over-red goes red under seed verdict
  check, whose recompute-and-mismatch refusal took the new exit 21
  receipt_mismatch (both allocated per envelope.md's lowest-unused
  rule alongside the plan's 17/18/19; the transcript-gate and
  mismatch conditions surfaced in review and at implementation).
  Gate-before-run consumes the projection's gated flag: ungated
  executable content refuses 18 with nothing executed, and
  declared-executable content yielding no parseable commands
  refuses 19 — the command grammar is the plan grammar via the
  exported plan.Commands, so the verifier executes exactly what the
  lint reads. The contracts view is unchanged; surfacing verdicts in
  projections rides 6.2's divergence work.
- 2026-08-31 — The reconciliation chain is fully piped and divergence
  is a detected, surfaced state (plans/os-6cdc15be.md, task PR for
  os-6cdc15be). merge.requested admits only in review citing the
  admitted pass verdict by position (the chain-legality half of "a
  red verdict is unmergeable"; the implementer lockout half is
  6.4's), from the claim lane; merge.observed stays the table's one
  transition to done and deepens to the observer's forge fact
  {merged: full sha, pr}, admitted only behind the full chain (pass
  verdict, then a request citing it), from the new observer
  capability lane. The fold records the chain facts (latest verdict
  pass-or-fail for 6.4, latest request with its citation, and THE
  admitted observation, singular by construction since done is
  terminal; a raw-pushed skipped chain applies tolerantly and counts
  an anomaly like a packetless exit). Divergence detection is split
  by what each surface may read: internal/reconcile and the report's
  new reconciliation section carry the record-derivable classes
  (merge_without_verdict, chain_skipped, unreconciled — the last
  reported neutrally: no build carries a wall clock, so
  pending-versus-failed is Phase 9 maintenance's age judgment), and
  seed reconcile alone reads the artifact store and the repository
  for the evidence grades: attested-head reconciliation with honest
  ancestry cases (fast-forward and true merge commits clean;
  anything else, rebase and squash flows included by design in v0,
  surfaces attested_divergence as a state, not a fabrication
  verdict, with patch-equivalence reconciliation against the
  receipt's diff hash as the named successor) and target-rewrite
  detection by observing the target ref (v0: the checked-out default
  branch tip; a forge force-push writes no ledger event, so the
  reachability of the observed merge under the tip is the signal —
  review finding that replaced an unreachable two-observation
  design). Detection is a report at exit 0, never a refusal;
  projections surface the pipeline at contracts v6, report v3, and
  cache generation 5.
- 2026-08-31 — Sealed checks are a commitment with a pre-claim window,
  encrypted custody, and a verifier-boundary gate (os-3128535a,
  plans/os-3128535a.md; charter II §7, §8; III.F sealed rows;
  spec/sealed-checks.md). check.sealed is a fact admitted only
  in ready with NO prior claim.taken — the release path lands
  subjects back in ready, so state alone would launder
  post-implementation seals (review finding on plan #138) — one
  commitment per subject, and the fold records the fact only from
  that legal window (raw seals outside it are anomalies, never
  facts: they must not retroactively claim a pre-existence the
  ordering disproves). The commitment is SHA-256 over the JCS sealed
  envelope {salt, checks}, the 32-byte salt living only inside the
  ciphertext (publishing it would invite dictionary attacks on
  low-entropy check bodies), so commitment verification is a
  decrypting party's act, performed at every unseal; empty seals
  refuse at creation AND at unseal (a zero-check envelope would pass
  vacuously). Encryption rides the binding default filippo.io/age
  v1.2.1 with recipients derived from the verdict-granted ed25519
  keys as ssh-ed25519 age recipients (agessh) — "recipients = the
  verifier keyring" with no new key material; the cross-protocol use
  of one ed25519 key for verdict signatures and seal unwrapping is
  the documented v0 trade, dedicated X25519 enrollment the named
  successor. Ciphertext is mutable custody at
  var/artifacts/sealed/<commitment>.age (the ledger references
  it by the immutable commitment; deletion is the charter's erasure
  path, surfaced by the audit); rotation decrypts with a still-able
  identity and re-encrypts open subjects to the current keyring,
  writing no ledger events, terminal subjects skipped (exposure
  bounded to the compromise window). Authoring isolation binds three
  times: the sealer capability row has no operator fallback (the
  verdict-lane posture: operator stands in the claim lanes, so a
  fallback would put authoring and implementation on one capability
  and the audit could prove nothing — review finding), actor.granted
  refuses sealer/claim and sealer/operator co-holding both
  directions (a root's implicit operator standing included), and
  claim.taken refuses the seal author. Receipts gain commitment +
  sealed_transcripts via omitempty so every pre-6.3 receipt's
  canonical bytes and digest are unchanged (the compatibility twin
  of the views' explicit-null convention); verdict check on a sealed
  subject requires an unsealing identity and reruns the sealed
  commands inside recompute-and-mismatch (review finding: presence
  checking would let invented sealed transcripts check green).
  Render gates above-trivial unsealed subjects at exit 24; exits 22
  seal_broken and 23 not_recipient name the broken-seal and
  rotation-lag refusals. seed seal audit reads only the age header's
  recipient stanza tags (agessh four-byte fingerprints — an
  identification hint; the decrypt drills carry the cryptographic
  claim) and reports stale/foreign/missing at exit 0; reconcile
  gains the neutral unsealed class. Projections: contracts v7
  (sealed explicit-null), report v4, cache generation 6.
- 2026-08-31 — The red-verdict lockout binds authenticated fails to
  submissions, the return path resolves the review exit, and the
  override is a gated escape hatch (os-d2497eb7,
  plans/os-d2497eb7.md; charter II §8, III.G lockout and override
  rows). contract.returned (review to ready, a new table row) admits
  only citing a standing fail verdict on the current submission that
  passes the 6.2 verifier boundary; prior facts and the 6.3 seal
  survive the return (the commitment predates the FIRST claim, which
  is the proof that matters). The lockout refuses
  verdict.rendered(pass) at admission and seed verdict render (exit
  25 red_locked) while any authenticated fail cites the bound
  submission; the fold keeps the whole window's fails
  (SubmissionFails, cleared on each submission.made) precisely so a
  raw-pushed later verdict can never bury an authentic fail and
  unlock pass (review finding on plan #140: unauthenticated fails
  lock nothing and authorize nothing — a raw fail would otherwise be
  a denial-of-service lever). Fail restatements stay renderable.
  merge.overridden is the third no-fallback capability row (operator
  only): strict {reason, verdict} citing an admitted,
  boundary-validated fail on the current submission (review finding:
  without the citation gate the hatch was a general bypass of
  independent verification), one per submission window, folded as
  OverrideFact never a verdict. merge.requested cites exactly one of
  verdict or override; merge.observed accepts the override-backed
  path with both chain steps validating the override signer against
  the operator boundary; reconcile adds VerifyOverrides with
  override_unverified (position-accurate) and the neutral overridden
  class, and an override-backed done is never merge_without_verdict
  (the override is its sanctioned cover; a raw override still
  surfaces override_unverified beside it). The
  no-verifier-available emergency (overriding with no verdict at
  all) is deferred to the halt and governance paths. Projections:
  contracts v8 (override explicit-null), report v5, cache
  generation 7.
- 2026-08-31 — Offers are supervisor facts inviting claims, consumed
  by the claims they invite (os-c61c3392, plans/os-c61c3392.md;
  charter II §9, III.H rows 1–2). offer.published admits only on
  ready subjects from the new supervise capability or its operator
  fallback (no disjointness: an offer grants nothing — the claim it
  invites settles at admission like any claim); strict
  {eligibility{capabilities, tiers}, expires} payload with a
  deterministic born-dead refusal against the event's own ts, since
  admission never reads a wall clock. Liveness is fully derived,
  never stored: ready AND unexpired AND no applied claim.taken after
  the offer's position (review finding on plan #144: without the
  consumption term a taken offer resurrects when the subject
  re-readies inside its expiry window, rescheduling from stale
  supervisor intent — "claimed or expire" makes re-offering a fresh
  publication). No offer.expired/withdrawn verbs; no position
  throttle (publishing is lane-gated, unlike open-lane milestones).
  The tolerant fold records raw pushes as OfferFacts and seed offer
  list applies the laundering shape at the consuming surface
  (position-accurate supervise boundary via keyring replay), so
  foreign offers are inert: never listed, never authority. The
  wakeless poll-only drill runs the whole loop to done with no wake
  channel in existence, and the race drill's loser gets the
  structured contention refusal at admission. Verb-catalog growth is
  additive (no protocol bump). Projections: contracts v9 (offers +
  last_claim, omitempty so offer-free chains keep byte-identical
  views), report v6 (republish only), cache generation 8 (offers
  table + last_claim column). Review round on #145: operator
  standing satisfies every eligibility scope at the list surface
  (admission already lets the operator take any offered work, so it
  must be able to discover it), and the view serializes last_claim
  only beside offer facts, so ever-claimed offer-free subjects keep
  byte-identical v8 bodies; the cache column stays full-fidelity
  under its new generation.
- 2026-08-31 — Budgets are per-contract reservations checked and
  decremented at admission, with every derived surface applying the
  laundering shape (os-cecac5de, plans/os-cecac5de.md as amended by
  #148; charter II §9 "Budgets are reservations, not observations",
  III.H rows 3–5). Capacity comes from the filed budget class
  through the spec-pinned table (small 100, medium 1000, large
  10000; unknown class = zero capacity, never fudged); org/actor
  granularity and the per-adapter risk-limit surface stay named
  extension points for 7.3+. budget.reserve/settle/release are
  {claim, operator} facts admitted only in_progress; a reserve
  additionally requires the drafting signer to be the ACTIVE claim
  holder or operator (review finding on #147: prior claimants can
  cite the active fence, and treating them as boundary-valid would
  let a released worker consume the next holder's budget). The
  fold records reserves and close attempts as independent lists and
  NEVER mutates a reservation (second #147 finding: a fold-level
  close on any well-shaped raw fact would let a foreign release
  free capacity for over-spend and a foreign settle lock the owner
  out); effective closure is derived at every consuming surface as
  the first close by the reservation's own signer or operator,
  position-accurately. Remaining = capacity − open valid
  reservations − settled actuals; settle records TRUE actuals,
  overruns included. The race drill passes two 8-unit drafts
  through one pre-admission view of a 10-unit class and admits
  exactly one — the §II.9 argument executed. The spending-verb gate
  ships as an empty data table with an injection drill; the
  envelope budget block is Phase 8's and park-on-exhaustion 7.4's.
  Projections: contracts v10 (budget + reservations, omitempty so
  budget-inactive chains keep byte-identical views), report v7
  (republish only), cache generation 9 (reservations table + budget
  columns).
- 2026-08-31 — The executor adapter is a public package and execution
  is fenced to the reservation end to end (os-1dad487d,
  plans/os-1dad487d.md as amended in the #150 review round; charter
  II §9 executor prose, III.H rows 6–8). executor is the
  module's one non-internal package: Adapter
  (Provision/Wake/Tuple) and Run (Workspace/Meter/Dispose), with the
  local worktree adapter first (detached git worktree, fixed argv,
  packet at .seed-run/packet.json, Wake the documented no-op, Tuple
  the honest local-worktree/v0 stub until Phase 10). run.started is
  the spending-verb table's first entry — strict {fence,
  reservation}, in_progress only, the ACTIVE fence, an open valid
  reservation revalidated position-accurately, once per fence,
  {supervise, operator} — and Provision refuses without the admitted
  start it cites, so the bracket is reserve, start, provision,
  meter, settle, budget.settle. run.settled aggregates once per
  started fence (prior fences included: a run settles after its
  window closed); metering rides the observation channel via the
  additive units line field. One review-visible deviation from the
  plan's mechanism note: the plan said the run facts' fence field
  "doubles as the fence rule's citation", but the fence rule's
  active-fence semantics would refuse the prior-fence settles the
  plan REQUIRES, so run verbs are fence-rule-exempt and the run rule
  validates the reference against the applied claim positions
  instead — behavior preserved, mechanism corrected. The
  disposability drill SIGKILLs a real worker subprocess (test-binary
  re-exec) at a seeded randomized site after an admitted
  submission.made: the chain verifies, the contract completes
  elsewhere from the surviving ledger alone, the loss is exactly the
  post-sync observation lines, and disposing the dead workspace
  loses nothing admitted. Projections: contracts v11 (run facts,
  omitempty), report v8 (republish only), cache generation 10 (runs
  table).

## 2026-08-31 — 7.4 graceful preemption (os-0f718b4e, plan #153)

- The interrupt request is a ledger fact: run.interrupted, strict
  {fence}, admitted only in_progress on the ACTIVE claim fence, from
  {supervise, operator}, once per fence (raw duplicates fold as
  anomalies, the run-fact posture; a raw invalid interrupt neither
  blocks the legitimate supervisor's nor is one). The chain is the
  canonical channel — workers already poll it for liveness, so no
  marker files or adapter side-channels exist in v0. Deliberately
  NOT a spending verb: preemption is supervisory control and must
  not be budget-gated. Validity is position-accurate from birth
  (InterruptValid at records[:pos]; InterruptRequested the one
  shared derivation), applying the RunStartValid review lesson
  proactively and closing the raw-push denial-of-service shape:
  conforming workers park only for boundary-valid interrupts.
- Safe-point semantics are the worker contract, specified in
  executors.md's Preemption section: check at bounded intervals (at
  least once per metering/poll cycle), finish the current step,
  write the four-part packet, exit deliberately via claim.parked —
  park, not release, so blocked hands routing back to the dispatch
  lane. The executor public surface is untouched: the check is
  the worker's, exactly where the charter locates it.
- The force path yields an honest reap packet from what is known:
  SIGKILL the interrupt-ignoring worker, claim.reaped by
  dispatch/operator (acceptance from the specified criteria, base
  the zero-length range when no pushed work is known, findings
  recording the ignored interrupt and the kill), subject back to
  ready and immediately re-claimable; run.settled records the dead
  run afterward. B-style automatic timeout reaping stays Phase 9's
  maintenance loop, which presupposes exactly these semantics. Both
  paths drilled end-to-end with real subprocess workers completing
  elsewhere from the surviving ledger alone. Projections: contracts
  v12 (interrupts, omitempty), report v9 (republish only), cache
  generation 11 (interrupts table).

## 2026-08-31 — 8.1 affordance envelope (os-f5551001, plan #158)

- Affordances are computed by probing the real rule set: one signed
  draft per catalog verb through the same admit.Check admission
  enforces — one rule set, two consumers, zero exceptions (the
  signature rule included: probes are signed with the caller's key,
  so the API takes the private key, and a fingerprint-only surface
  cannot compute). The probe-payload table is synthesis data, never
  legality logic; templates fill from the live context (active
  fence, open reservation, bound submission, standing verdict) with
  placeholder positions where anchors are absent, so illegality is
  always the rules' judgment. Fence-optional verbs cite the active
  fence exactly when one stands, matching the fence matrix for
  holder and non-holder signers alike.
- No under-listing: every catalog verb has a synthesizer, pinned by
  a completeness test against the spec verb table, and the
  lifecycle walk asserts each verb listed somewhere legal. The one
  named carve-out is actor.enrolled, whose valid payload requires
  the queried subject's public key: fingerprints are hashes, no
  prober can derive it, and the enrollment surface knows its key
  out of band (spec sentence records it).
- Every append-path response stamps affordances and the budget
  block, success and refusal alike, via one shared helper; stamping
  degrades to the empty list rather than ever failing the verb.
  budget status gains --key (not --actor: probes must be signed).
  The budget block derives from the shared BudgetViewAt: reserved =
  open valid reservation sum, remaining = derived remaining.
- The walk corrected two of the plan's legality guesses against the
  actual rules (halt.lifted is admissible with no halt standing;
  contract.blocked's one source state is ready): the affordance
  computation reports the rules, so the drills assert the rules.

## 2026-08-31 — ledger writeHead race fix (os-c6fb95ee, plan #159)

- The store's HEAD rewrite used one shared HEAD.tmp path from two
  processes by design: the appender, and any poll-only reader whose
  Open landed mid-append and ran healHead's repair — the reader's
  rename consumed the appender's temp and the append failed with
  ENOENT (the TestGracefulPreemptionDrill CI flake, and in
  hindsight the #156 verify make-check red). writeHead now
  allocates per-writer unique temps via os.CreateTemp with
  best-effort removal on every failure path; renames stay atomic
  over their own files, both racers write forward-consistent heads,
  and a momentarily stale HEAD self-heals on the next Open, which
  is the repair path's job. No lockfile, no layout change.
- The regression test attacks the store directly (one goroutine
  appending 400 records, one Opening in a loop): the drill-level
  window is too narrow on fast disks, the store-level one is not —
  pre-fix it reproduces the exact ENOENT at the first contended
  append, post-fix every append succeeds and the chain verifies.

## 2026-08-31 — 8.2 affordance regression class (os-148d3ba1, plan #162)

- The III.I bug class is executable: TestAffordanceRegressionClass
  sweeps every prefix of the shared walk scenario (the walk's
  history extracted into walkScript, so the walk's stations and the
  sweep replay one script) and, for all seven enrolled-lane pairs,
  asserts sortedness, determinism, and that every listed verb
  re-admits through the enforcing Check when independently
  re-drafted at that position; failures name the class (position,
  lane, subject, verb, refusal). The sweep's probe-view derivation
  is a deliberate independent copy of the production one:
  divergence between the two fails the class, which is the drift
  signal the test exists to raise.
- The position stamp is pinned end to end at the CLI: success and
  refusal envelopes alike stamp the tip ordinal of the context
  their list was computed at (ctx.Count - 1; the plan review caught
  the off-by-one), and the stamped list equals an independent
  admit.Affordances recomputation at that context for the signing
  key on both paths. The drill's refusal half is the keyring
  preview's malformed-actor-event refusal, because that is the
  local append path's own refusal: the path runs the preview, not
  the full admission pipeline, so lane-invalid non-actor payloads
  append raw there by design and the fold-side validity families
  keep them inert.

## 2026-09-01 — 8.3 refusal-rate metric (os-edf73d66, plan #164)

- The metric's source is the attempts journal, attempts.jsonl beside
  the ledger: one strict JSONL line per admission-boundary attempt,
  BOTH outcomes, journaled best-effort (never failing the verb, the
  stamping posture) at every CLI seam that renders a
  position-stamped, signed envelope — ledger append's preview and
  chain-invalid refusals and its success (the lifecycle and budget
  verbs ride this seam), offer publish, verdict render, seal create,
  remote-refusal renders included. Responses without a stamped
  position are not boundary attempts; read surfaces never journal;
  the remote server-side boundary stays a named spec extension
  point. One population for the rate's numerator and denominator
  (the plan review's finding): the chain is never the denominator,
  and the section's span is positional context only.
- The journal enters the report as a declared input on the
  observations pattern: project rebuild --refusals, composing
  freely with --obs; Inputs.Digest now spans every declared family,
  each contributing its keys only when declared, so obs-only
  digests are unchanged; malformed journal lines refuse the build
  naming the line. Report v10 adds the nullable refusals section
  {inputs, refused, admitted, by_code, by_verb, span, rate}, rate
  fixed to four decimals ("0.0000" on an empty journal); the
  section echoes the journal's own digest while the stamp carries
  the full declared-inputs digest.
- The journal's durability posture (review finding on the task PR):
  a short write restores the previous length when the fragment is
  provably the file's tail (an ambiguous size under concurrent
  O_APPEND writers is left alone rather than risking a rival's
  line), and Load treats the terminating newline as the commit
  marker, ignoring a final unterminated fragment: a best-effort
  writer must never be able to poison the strict reader forever.
  Terminated malformed lines still refuse.
- Plan correction, recorded per D5: the plan's D3 guessed a cache
  generation bump with a conditional refusals key, but the cache
  mirrors the INPUT-FREE report (it builds from reportView, exactly
  why it never carries the observation section), so the refusals
  section never reaches it and no cache change ships. The
  observation precedent, not the reconciliation one, is the
  operative analogy: reconciliation is record-derived,
  refusals input-derived.

## 2026-09-01 — promotion (spin-out) defined (os-768361cc, plan #167)

- Promotion is two steps, self-hosting then distribution, and
  NEITHER cutover is autonomously decidable: the ground rules make
  v1 the entry point "until spin-out", so spin-out is the
  entry-point switch itself and the self-hosting cutover is the
  reserved escalation. The plan defines criteria and mechanics for
  both and authorizes neither.
- Promotion requires Phases 0 through 12; Phase 13 alone follows.
  The first draft of this amendment claimed Phases 10 and 11 were
  not blockers by reading "Phases 0-12 deliver core conformance" as
  permission to skip two of them. That is a misreading, recorded
  here rather than quietly corrected: the phrase describes what
  those phases deliver collectively, Phase 10's exit owns III.E and
  III.G and the III.O eval items, Phase 11's owns III.K, the
  core-conformance criterion demands every pillar's mechanisms, and
  Phase 12 declares deps: all. Promoting without them would hand
  real coordination to a system whose verdicts carry no calibration
  and whose grants carry no runtime qualification.
- The compromised-actor drill gates the SELF-HOSTING cutover, not
  distribution. The cutover is when real authority moves, so the
  drill proving the ceiling against a valid stolen key must precede
  it; nothing is at risk while Seed coordinates nothing. The first
  draft had this backwards, reasoning from visibility rather than
  from authority.
- Phase 9 gains item 5, the lane-facing surface: an obligations
  projection (what is owed, by whom, since when, discharged by which
  verbs, derived from the same tables admission enforces), a
  situation read with --since so a resuming lane pays for the delta
  rather than the world, and loop verbs that derive every derivable
  argument and refuse before signing what the tables would refuse
  after. The gap it closes is that Seed represents permission
  (affordances) but not obligation, so nothing answers what a lane
  owes; every fact needed is already folded, so this is a projection
  and never a new authority.

## 2026-09-01 — obligations projection and the situation read (os-52d5da3f, plan #170)

- Obligation is a projection over the fold, never a new authority:
  state-shaped kinds read discharged_by from the transition tables,
  and the closed fact-shaped set maps each remaining kind to the spec
  pairing it with its fact (run.settled, budget settle/release,
  verdict.rendered, merge.requested).
- The dischargeability sweep earned its place on first run by
  catching a real modeling error: verdict.unmerged advertised
  merge.observed while admission still refused it for want of a
  merge.requested. The merge chain is two events, so the kind now has
  two shapes — until a request cites the verdict the debt is the
  operator's and merge.requested pays it; after that the forge fact
  is the observer's. Same class the affordance sweep catches one
  level down, which is the argument for building the class with the
  surface rather than after it.
- The sweep asserts AT LEAST ONE discharging verb admits for the owed
  actor, not all of them: discharged_by names the acts that end the
  obligation, while who may perform each is the affordance layer's
  business (a claim is discharged by release, park, reap or
  submission, of which the holder may take three and the supervisor
  the fourth). Requiring all would force capability policy into a
  derivation that must stay a projection.
- Under a declared halt every obligation still stands and none is
  dischargeable; the sweep checks only well-formedness at halted
  positions, because that is the halt working rather than an
  obligation defect.
- --since cites a tip ORDINAL, so the prefix a resuming lane last saw
  is records[:position+1]. The first implementation used
  records[:position] and the round-trip drill caught it — the same
  off-by-one the envelope's position stamp turns on.
- Part (c), the loop verbs, is split to os-7e197768 for
  reviewability; item 5 stays one obligation with two landings.

## 2026-09-01 — loop verbs (os-7e197768, plan #172)

- The verbs are not sugar over `seed ledger append`, and the
  distinction is structural rather than stylistic: the raw seam
  consults the admission boundary NOT AT ALL, so a lane acting
  through it learns its act was illegal from a chain-level refusal
  after signing. Every loop verb runs the same `admit.Check`
  admission enforces before anything is signed into the chain, and
  renders the boundary's own error beside the caller's affordances.
  That is Phase 8's one-rule-set principle carried from legality to
  construction.
- An argument the system can compute is never asked for, because a
  value the boundary would refuse is not a choice being offered. The
  fence comes from the active window, the reservation from the shared
  budget view, the plan anchor from the approval (an approval admits
  ONE exact revision, so no other citation could be legal), and the
  resume range from the repository. What stays caller-supplied is
  what is a judgment: `--amount`, `--actuals`, and the packet's prose.
- Three failure shapes, three owners, and the split is the design:
  a MISSING fact refuses in the CLI naming what would establish it; an
  AMBIGUITY refuses in the CLI naming the candidates, because two open
  reservations are a spend decision the lane owns and a silent choice
  would make it for them; an ABSENT WINDOW is not a derivation failure
  at all — the key is omitted and the boundary refuses with its own
  account of the state, which is better than anything a derivation
  could say.
- On the remote path the derivation and the pre-flight read one
  materialized remote tip. A fence or reservation read from a stale
  local copy would be wrong under exactly the contention that makes
  claiming online-only, so `openRemoteSession` was extracted from
  `runLedgerAppendRemote` to share that single view rather than
  reconstruct it per verb.
- `claim take` refuses `--ledger` with the raw seam's own account of
  online-only claiming, extracted to one function: a lane must never
  meet two explanations of one rule.
- Derivation refusals are journaled like admission refusals. A lane
  that could not act is exactly the affordance gap the metric
  measures, and `by_code` keeps `usage` and `not_found`
  distinguishable from the boundary's codes.
- Remote successes carry no affordances and journal nothing: there is
  no local ledger to reopen at the landed tip or journal beside. That
  is the standing shape of every remote append, recorded as a
  deliberate absence rather than left implicit; giving the remote
  posture its own journal home is a client-state decision no card has
  made yet.

## 2026-09-01 — agent-ergonomics obligations for Phase 9 lanes (os-68ea0b2d, plan #174)

- A surface with no obligated consumer is a surface that may never be
  used. Item 5 landed the lane-facing surface; item 1 never said the
  lanes must USE it, so a worker written against the raw append seam,
  waking on a trusted event stream, emitting remembered heartbeats and
  retrying blind would have satisfied every word of item 1 and failed
  promotion criterion 1 on inspection rather than on a check. Each
  obligation is stated so a fixture or a validation check can fail on
  it, and each is a new consumer of an existing table rather than a
  new authority.
- Seed has no lease, so the card's "lease renewal rides every
  holder-signed verb" does not transfer as written. The transferable
  obligation is sharper: the observations liveness is classified from
  are emitted by the loop's own steps, so a working lane is a live
  lane by construction and an expired classification is true
  information rather than forgotten bookkeeping.
- That obligation is enforced by VOCABULARY, not by detection. The
  first draft asked for a lint flagging claim windows whose only
  observations are non-advancing; observations.md already classifies
  that stream as live then wedged, and a legitimate long-running step
  emits exactly it, so with no heartbeat discriminator the lint would
  either flag valid work or never fire. Withdrawn: the loop's verb set
  contains nothing whose only purpose is liveness, which a role
  fragment and a fixture can both be checked against.
- Item 5(b) is recorded PARTIALLY MET rather than the single-read
  contract being relaxed. The read is specified to carry unread
  messages and does not, so a lane told to orient from one read
  cannot learn it has mail without either a second read or trusting
  the pushed message, and the one-inbox doctrine forbids the latter.
  The remainder is named in item 5's own text, on the #157/#169
  precedent that a routing binds only where the phasing authority
  says it.
- "Unread" needs no stored read-state and gets no `message.read`
  verb: the position a lane carries forward IS its read cursor, so
  the messages new to it are those arisen after the cited `--since`
  position, which is the identity `--since` already has for
  obligations. The surface gains a section, not a concept.

- Two over-claims corrected on review, both about treating a signal as
  stronger than it is. (1) "Absence of observation is absence of work"
  is exactly the inference the lossy declaration forbids: the channel
  is ephemeral and lossy by charter §II.3, a dropped stream and dead
  work are indistinguishable from outside it, and `no_data` has no
  reap path at all. What loop-emitted observations buy is the removal
  of forgotten bookkeeping as a CAUSE of silence, which makes the
  classification better evidence without making it proof; the reap
  stays a judgment needing corroboration. (2) One-retry convergence as
  "admit or escalate" rejects correct behavior: in fleet mode a claim
  race means the loser re-orients and takes different work, and a
  fence invalidated by a concurrent reap says the same. The third arm
  is a refreshed read showing the act is no longer owed, and it is the
  common case rather than a loophole. What stays forbidden is the
  fourth outcome: a blind retry or a silent loop.
## 2026-09-01 — the loop verbs' three post-merge defects (os-9b3f3ef3, plan #179)

- A derived argument must be re-examined against every refreshed tip,
  and REFUSED on divergence rather than silently replaced. The
  optimistic loop re-fetches, re-signs and re-judges per attempt, but
  the payload was fixed at the call, so a rival reservation landing
  mid-flight left `budget settle` citing the one that was sole when
  the session opened. Admission accepted it, because the budget rule
  asks only that the citation exist, be valid and be unclosed: the
  sole-open check lives in the CLI, against the stale view. The
  command therefore made, silently, the exact choice
  `soleOpenReservation` exists to refuse.
- Re-deriving and PROCEEDING would have been the wrong fix. A value
  derived from a view that has since moved is not a better argument,
  it is a different decision: a second reservation makes the act
  ambiguous, and a reaped-and-re-taken window is an authorization the
  lane never gave. The check compares and refuses, naming what
  changed.
- No `internal/gitref` change was needed. The `Validate` callback
  already receives the refreshed store and the candidate record, so
  the recheck composes into the seam that exists; threading a
  payload-producing function into the transport would have taught it
  about argument derivation. One admission context per attempt now
  serves both the recheck and `admit.Check`, which also removes a
  duplicate replay.
- A refusal must keep the position it was COMPUTED at.
  `remoteFailureEnvelope` already stamped the refreshed position
  through `refusalAt`, and `refuse` then overwrote it with the
  session's opening tip while advertising affordances from before the
  race. The stamp is the concurrency signal the field exists for, so
  a stale one inverts its meaning. `refusalAt` now carries the view,
  and an envelope that already has a position keeps it.
- "Refuse before signing" is about the chain, not about `event.Sign`.
  The boundary cannot judge an unsigned record: the actor rule
  verifies the signature, and `admit.Affordances` signs one probe per
  catalog verb for that reason. A reviewer read the phrase as
  forbidding the signature itself, which would have made the merged
  implementation of the feature a violation of its own contract. The
  spec now says so plainly rather than leaving it to be re-derived
  from the code.
- The JSON value `null` unmarshals into a map with NO error and
  leaves it nil, so writing a derived base panicked. Every malformed
  packet the drills tried was an object, which is exactly why the
  class was missed; they now cover null, an array, a string and a
  number, with and without `--base`.
## 2026-09-01 — a reservation outlives its window (os-d6963652, plan #175)

- The `in_progress` gate moved from the budget verb FAMILY to
  `budget.reserve` alone. Reserving capacity for a window you do not
  hold is what that gate exists to prevent; closing a reservation
  honestly is wrong in no state, and the derivation half
  (`BudgetCloseValid`, landed in Phase 7) already validated a close by
  identity alone and asked nothing about a window. Admission was the
  accidental over-restriction, not the derivation.
- The harm was not "capacity leaks on done contracts". Windows end
  four ways, and one is a failing verdict returning the contract to
  the queue: the next claimant is a DIFFERENT worker, neither the
  reservation's signer nor the operator, so no party could close the
  hold and their own reserves came out of a remaining the previous
  attempt had silently reduced. A retry after a failed verdict was
  quietly poorer than a first attempt.
- Auto-closing a stranded reservation when its window ends was
  rejected: it would record zero spend for work that may have spent
  plenty, which is what the budget rule already refuses to do for
  unknown classes, and it would destroy the distinction between "we
  spent nothing" and "nobody said".
- The card's alternative — detect it in maintenance and reap it — was
  unimplementable on its own. With the gate in place NO act freed the
  capacity, for anyone, the maintenance lane included (it is audited
  as an ordinary actor precisely so it has no private powers).
  Detection without an admissible remedy is a report nobody can act
  on.
- The `budget.open` obligation drops its live-window restriction,
  because the restriction's reason is gone: the advertised
  dischargers are reachable again. Detection lands through the
  projection that already exists — no new lint, no build-plan change,
  and `seed situation` surfaces it to the party who can act.
- That party is **whoever can still discharge it**, never merely
  whoever holds the window. Admission closes a reservation for its own
  reserving signer or the operator lane and nobody else, so
  attributing the row to the current holder named a party admission
  refuses on any reservation the holder did not sign. And because
  `HasAnyCapability` is standing-aware, a suspended or revoked signer
  can no longer close: the row hands off to `lane:operator` there, or
  a keyed `seed situation` would filter the debt away from the one
  actor able to pay it, on exactly the revocation-recovery path the
  charter cares about.
- The three budget AFFORDANCE PROBES cited `"fence"` unconditionally,
  unlike every fence-optional verb's conditional `fenceKV()`. Outside
  a window that citation is refused by the FENCE rule, so removing the
  budget state gate alone would have left `admit.Affordances` hiding a
  now-legal close and the dischargeability sweep red at exactly the
  prefixes this card exists to fix. `budget.reserve` moved with them
  although it stays gated: its out-of-window probe was refused by the
  wrong rule, which is the drift class Phase 8 built the probes to
  expose.
- The proof is the CLASS, not a hand-picked case: the sweep walks
  every prefix of the shared scenario and now necessarily reaches
  positions where the window has ended. The walk was extended by
  SUSPENDING and then REVOKING the lane holding the open reservation,
  because a walk of only active actors can never reach the positions
  standing-aware ownership exists for. Both mutations — restoring the
  family-wide gate, and dropping the standing-aware owner — turn the
  sweep red.
- Supersedes one clause of the loop-verb decision above: an absent
  window is still not a derivation failure, but the boundary does not
  always REFUSE there. A close outside a window is legal and cites no
  fence, which is precisely why omitting the key beats inventing one.
## 2026-09-01 — show's not_found stamps the tip (os-fa69345e, plan #176)

- `ledger show --position <missing>` scanned every record and then
  returned `not_found` unstamped. The envelope rule already covered
  it: a refusal raised BEFORE a tip was ever read carries null, and
  this refusal read the whole chain. The missing stamp was not a
  fabricated position withheld, it was a known position discarded —
  so the fix is code, not spec, and restating the rule would imply it
  had been ambiguous.
- The count comes from the iteration already running, never a second
  `Tip()` call. `show` is the read surface that never writes and must
  stay cheap; recovering a number the scan already held would be the
  wrong fix even though it produces the same envelope.
- `stampTip` declines at a zero count, so an empty chain carries null
  for free and no branch has to say so.
- The `chain_invalid` branch stays UNSTAMPED, deliberately. A scan
  that failed partway established nothing trustworthy: the count it
  reached is records read before an error, not a statement about the
  chain, and stamping it would assert a position the failure
  disproves. A drill pins this so a later "consistency" pass does not
  extend the stamp there.

## 2026-09-01 — the six lane fragments (os-cf1c9688, plan #187)

- A role file is prose, and nothing checks prose. v1 ships four role
  documents and no validator, which is survivable where a human reads
  them and not survivable for a promotion criterion that asserts a
  property OF THE LANE — "runs entirely through Seed verbs, orienting
  from one position-stamped read" — that only the file's author ever
  verified. So the four obligations are DECLARED FIELDS, and every
  field is checked against an authority elsewhere in the tree.
- The relation between a lane's grants and an act is INTERSECTION, not
  containment. `AcceptedCapabilities` is an OR-set consumed through
  `HasAnyCapability`, and most worker verbs return {claim, operator}:
  requiring every accepted capability would have handed `operator` to
  five of six lanes to make validation pass, dissolving the separation
  the check exists to protect.
- The dispatcher's least-capability posture is an ALLOWLIST checked for
  exact equality, not a list of exclusions. A blocklist must be
  extended whenever a capability is added, and one nobody thought to
  exclude is admitted by default: an earlier draft checked "no
  authoring, verdict or sealing grant", which admits `operator`, the
  strongest capability in the keyring, on the lane that reads the most
  hostile input.
- `internal/loopverb` was EXTRACTED rather than assumed. The plan's
  first draft cited "cmd/seed's loop-verb table"; there was none, and
  the acts were case arms in package main, which nothing can import. A
  validator would have written the seven names down a second time,
  violating the plan's own criterion. The registry now has two
  consumers, the CLI dispatch and the validator, which is this card's
  thesis applied to itself. The extraction is inert: the existing loop
  drills pass unchanged.
- Manifests are JSON, not the YAML the plan named. `next/` carries no
  YAML parser and a deliberately small dependency set, and every other
  machine-read file there is JSON. The plan's REASON for separating
  declarations from prose — data a validator reads versus prose an
  agent reads — is served identically, so the format was the incidental
  half of that decision and adding a dependency to the successor's
  supply chain is the material half.
- The liveness obligation is conditional on running a loop, because
  four of the charter's six lanes perform no loop act at all: a
  verifier acts through verdict.rendered and a dispatcher through
  intent.filed. Requiring loop acts of all six would have forced four
  manifests to claim work they never do. It is not dodgeable by
  declaring no acts, because holding the `claim` capability means the
  lane claims and claiming IS a loop act, so the grant already
  declared decides whether the obligation applies.
- 1a settles the declaration's SHAPE and says so rather than implying
  more. A subset check compares two labels and cannot show the named
  step emits, because nothing executes here. 1c inherits the other
  half, recorded in progress.md so the split is deliberate rather than
  a gap found later.
- The lane surface carries no position stamp. A resolved role derives
  from checked-in files rather than from the ledger, so there is no
  position it could honestly cite — and nothing is written back, since
  a resolved role on disk would be the second copy the ordered
  fragment list exists to prevent.

## Phase 9 item 1c — the worker loop made executable (os-abb206c8)

- The loop is a **library**, not a CLI verb. Seed does not own the
  work: writing the code is the model's act, so the work step is
  supplied by the caller. `seed loop run` is deliberately deferred and
  named in the spec rather than quietly omitted, because it would
  invite treating the CLI as the agent, and the real consumers are
  item 4's fixtures, which drive this in CI with no model and no wake
  channel. A library is what those can drive.
- The loop **reimplements no verb**. Every act goes through one seam
  whose only implementation is the CLI's own dispatch. A second
  implementation of `claim.taken` inside the loop would consult the
  admission boundary not at all, and so could not answer a refusal
  with what IS legal — the whole reason the loop verbs exist.
- **The reads learned the remote posture, and the alternative was
  rejected on principle.** `claim take` refuses `--ledger` outright,
  while `offer list` and `situation` bound `--ledger` alone: in the
  only posture where a lane could claim, it could neither poll nor
  orient. Because the loop is a library in the same module, it could
  have read the remote-materialized store through internal packages
  and skipped the CLI. It must not: every manifest declares `seed
  situation …` as its orienting read, so orienting by internal call
  would make that declaration a fiction and reopen exactly the drift
  1a closed. The surfaces took the exclusive-or instead.
- **`SituationFlag` gained `Posture` beside `Required`** because the
  pair is an exclusive-or and a required-flag model cannot express
  one: naming neither and naming both must each refuse, for different
  reasons. The CLI drill derives both arms by perturbing a parsing
  baseline rather than reading required-ness off the declaration.
- **The worker's exhaustion point is `budget.reserve`, not the
  spending gate.** `transition.IsSpendingVerb` holds exactly
  `run.started`, admitted from {`supervise`, `operator`}; the
  implementer holds `claim`, so that gate is the executor's and no key
  this loop signs with can trip it. The build plan's phrase "a budget
  refusal at a spending gate" names the concept, and for this lane the
  concept lands on the reserve. Reaching for `InjectSpendingVerb` to
  manufacture a refusal the loop could trip would have drilled a path
  that exists only in the test.
- **Liveness is emitted as a side-effect of a declared act that
  SUCCEEDED**, keyed to the lane's own actor and the fence its
  orienting read reports. Three guards are each load-bearing: only
  acts named in `liveness_from` emit, so the declaration decides what
  happens; only a succeeded act emits, so a lane wedged at a boundary
  cannot look busiest when it is most stuck; and the key is the
  classifier's own, so a stream under any other key would be invisible
  to the reap while looking like liveness in a test. A write failure is
  swallowed deliberately: the channel is lossy by declaration, and a
  lane that abandoned real work over its telemetry disk would trade an
  authoritative act for a non-authoritative one.
- **The situation read now reports the acceptance anchor**, found by
  running the loop rather than by reading the spec: a lane's deliberate
  exit carries a packet, a packet's acceptance part is what a successor
  is judged against, and the read withheld it — so the lane could not
  write the exit its own contract requires.
- **Budget exhaustion refuses as `chain_invalid` and this is NOT fixed
  here** (carded `os-d03bde01`). No budget exit code exists, so the
  message carries the whole account while the code misleads: a
  successor reading `chain_invalid` would conclude the ledger is broken
  rather than the budget spent. An exit code is protocol surface and
  this card's scope guard does not open it, so the behavior is pinned
  by a characterization assertion that fails when it is closed.
- **A lost `claim take` is idle, not an error.** In fleet mode two
  workers racing it means the loser re-orients and takes different
  work; treating that as failure would manufacture an escalation storm
  out of ordinary contention.

## Phase 9 item 1b — the dispatcher's injection conformance suite (os-b779b4c7)

- **The suite does not test that hostile text is disbelieved**, and says
  so first. There is no model under `next/`, so "never obeyed" is not
  directly testable, and a corpus fed to code with no instructions would
  test the corpus rather than the system. It asserts that BELIEVING the
  text changes nothing, and names where that is false.
- **Reachability is derived from `admit.Affordances`, not from
  `keyring.AcceptedCapabilities`.** The latter is a capability index
  whose switch returns nil for the standing-only class, so filtering it
  omits `message.sent` — the one dispatcher-reachable act that relays.
  `Affordances` drafts a signed probe per catalog verb through the same
  `Check` pipeline admission enforces, so the answer comes from the
  boundary. Its catalog's completeness is enforced against the spec
  table in both directions, which is what makes "a verb added later
  fails this drill" true rather than hoped.
- **Residuals live in checked-in data with a reason and a consequence
  each**, and the drill fails both ways: an unnamed reachable verb fails,
  and a named verb the walk cannot reach fails as stale. A residual
  without a reason fails too — a list entry is not a finding.
- **The walk was extended rather than the table trimmed.** Three named
  residuals were unreachable by the first short walk, and shortening the
  list to match would have been a shorter list rather than a safer
  system. The walk now enrolls a verifier and carries a subject to a red
  verdict so `contract.returned` is genuinely probed.
- **Two residual descriptions were corrected BY the drills.**
  `claim.reaped` was described as having no precondition; the boundary
  refused twice, revealing a fence citation and a packet requirement.
  Both are freshness and attribution rather than authorization, and a
  persuaded dispatcher satisfies them by reading and writing, so the
  residual stands — but at its true shape, not the one prose asserted.
- **The projections carry every payload verbatim, and that is pinned
  rather than treated as a leak.** A projection that could not show what
  was appended would not be an audit view. It is recorded because the
  projections are what a mirror renders, so the unlanded `request.*`
  card inherits an input already carrying hostile text.
- **III.J's second row is reported two-thirds met.** Intents and tool
  output are covered; mirrors cannot be, because `request.*` has zero
  rows. Reporting the row closed would have been the easiest sentence in
  the spec and the least true.

## Phase 9 item 1c follow-up — four review fixes (os-378e44f3)

- **The actor is DERIVED from the key, and the parameter is gone.** The
  obvious repair was to cross-check a supplied actor and refuse a
  mismatch; that keeps a parameter whose only correct value is
  computable, so every caller gets a chance to be wrong and the check
  exists to catch them. Deriving removes the class.
- **Deriving once is still not enough**, and the plan's first draft
  claimed otherwise. The loop passes `--key <path>` and the CLI signs
  with what the path holds now, so a rotated key reopens the mismatch
  through the filesystem. The fingerprint is re-derived each iteration
  and a change **refuses** rather than being adopted. Rejected:
  pinning key material for the Driver's lifetime, which would require
  copying a private key to a second place on disk — a worse trade than
  refusing.
- **A claim reaped before the post-claim read is IDLE, not a park.**
  The window is gone, so there is nothing to exit; the reserve would
  refuse and the park after it would refuse for want of an active
  claim, turning ordinary contention into an error.
- **The fence comes from the act that opened it.** `claim take` returns
  the admitted position as its fence, and the loop adopts a fence any
  act's result names before observing. Generalized rather than
  special-cased, so it stays true if a second window-opening act
  appears. The gap it closes is the stall window: a worker that claims
  and then hangs before reserving is exactly what the expiry
  classification exists to catch, and its claim previously emitted
  nothing the classifier could see.
- **A packet file never outlives its purpose**, on all three paths: the
  verb consumed it, the write or close failed after creation
  (`writePacket` unlinks; no verb will run and no caller holds a path),
  or `CreateTemp` failed and there is nothing to remove. The second is
  the low-storage case, where leaking leaks when the host can least
  afford it.
- **Two drills were wrong and are corrected here.** The e2e liveness
  assertion hard-coded a `liveness_from` copied from a unit fixture and
  passed only because the fence defect meant `claim take` never
  emitted — a drill agreeing with the bug it should have caught. The
  fence drill's pre-claim read returned a window a pre-claim read
  cannot have, so reverting the fix did not fail it. The second was
  found ONLY because its mutation did not fail, which is the argument
  for mutation-testing every fix rather than trusting a green run.

- **The identity check lives in `act` AS WELL AS before polling, never
  instead of it** (review findings on #194 and #195). Checking once per
  iteration leaves the work step — the longest part of one — as a
  window where a rotation lands and the settle and exit then sign as a
  new identity: a window opened by one actor and closed by another, or
  left open because the close refused, which is the state the four
  deliberate exits exist to make impossible.

  **Both checks are load-bearing, for different paths.** `Poll` and
  `Orient` carry the cached actor, so the pre-poll check keeps the READ
  path honest; an idle driver never reaches an act at all, and without
  it a rotated worker would poll forever under an obsolete fingerprint
  and miss work granted only to its new identity. The per-act check
  keeps the WRITE path honest. An earlier wording of this entry said
  "in `act`, not at the top of `Step`", which would have licensed
  deleting the pre-poll one — recorded here because a decision record
  is a licence, and this one nearly issued the wrong permit twice.
- **A rotation mid-window still ATTEMPTS the deliberate exit** (review
  finding on #196). Returning the rotation error directly left the
  claim and its reservation open with no packet, which is the silent
  abandonment the exits exist to prevent — and the drill asserted that
  absence as correct, which was the mistake underneath the mistake.

  The exit attempt is exempt from the identity gate, or refusing it
  would guarantee the abandonment the gate exists to prevent. Nothing
  is weakened: the fence rule admits holder-signed events only from the
  holder, so a rotated key's exit refuses at the boundary rather than
  succeeding wrongly. The window is then genuinely stranded and the
  error says so, naming the reap — which is the maintenance lane's
  business (Phase 9 item 3), not something the loop can do for itself.
- **A failure-path drill must reach the failure.** The first version of
  the packet-unlink drill ran a successful `writePacket` and called the
  success-path cleanup, so removing both error-branch unlinks left it
  green — a test named for a branch it never entered. It now injects a
  failing writer through a seam added for that purpose, following the
  injection precedent `internal/transition` sets. This was the seventh
  instance this cycle of a drill agreeing with a convenient shape, and
  the only one added *in response to* a finding about the same class.
- **An escalation is a QUESTION carried by an act, and where it may be
  carried is decided by which state the act can LEAVE** (Phase 9 item
  2, plans/os-f781f0da.md). `lifecycle.md` pins the four deliberate
  exits from `in_progress` by self-validation and III.F depends on that
  set being closed, so nothing new may leave it: from `in_progress` an
  escalation rides `claim.parked`, which already carries the packet and
  the fence.

  A verb that **cannot** admit from `in_progress` opens none of that,
  which is why `escalation.raised` (`ready`, `review` → `blocked`) is
  not a fifth exit. The plan's first draft conflated the two and
  shipped the verifier as a known gap against the charter's "any lane
  can raise"; the review caught it. `packet.ExitVerbs` stays exactly
  four and is pinned against the table's `in_progress` outgoing set, so
  that existing drill is now the enforcement of this rule rather than a
  description of it.
- **Raising grants nothing; answering is a gate.** `escalation.raised`
  accepts five capabilities because the charter says any lane may
  raise, and breadth is safe for the `offer.published` reason: the
  contract leaves `blocked` only through the operator's
  `decision.recorded` or a citing cancellation, so a raiser can stop
  work and hand a human the decision, never move it, and cannot answer
  its own question. `decision.recorded` is the FOURTH no-fallback row —
  a `dispatch` fallback would let a machine lane answer a human gate.

  The residual is recorded rather than hidden: a persuaded lane can
  FREEZE a contract. That is denial of progress, not escalation of
  authority, and it is in `residuals.json` because the injection sweep
  refused to let the widening land silently.
- **Cancelling an escalated contract is an ANSWER and must cite the
  question.** Refusing the cancel would trap the contract with no
  operator path out, which is worse than the failure prevented. But an
  uncited cancel lets the subject reach a terminal state with the
  question neither cited nor answered: the obligation disappears and
  takes the audit link with it, so "nothing else moves until it is
  answered" would hold only by accident. The citation is what records
  that the decision taken was to cancel.
- **Age is elapsed time; positions order without measuring.** The
  plan's first draft said resolution latency was "the answer's position
  minus the raise's". That is event count wearing a clock's clothes: an
  escalation untouched for hours has the same position difference as
  one answered instantly after a burst of unrelated traffic. The
  `escalation.pending` row therefore carries the raising event's `ts`,
  and age is `now − ts` at a live read's own instant — `offers.md`'s
  posture, reused rather than reinvented: admission never reads a wall
  clock, a live read may. Drilled against both ledgers a position
  difference gets wrong, idle and busy.

- **The coverage gate verifies the NUMBER, not the collection**
  (os-cafba959). `cmd/go`'s `mergeCoverProfile` drops a package's
  profile fragment silently — twice over, when the fragment file is
  missing (*"Test did not create profile, which is OK"*) and when it is
  zero-length — with no error, no message, and `ok` still printed for
  every package. The merged total then reads far below truth on a tree
  that is fine, which presents as exactly what a real regression looks
  like.

  So the gate re-collects **only when the reading is below the
  threshold**, and decides from two readings. That is chosen over every
  structural alternative for one reason: **it cannot false-alarm**,
  because it engages only where the gate would already have failed. A
  healthy tree never pays for it and never trips over it.

  Rejected, and recorded so it is not re-litigated: counting
  contributions. Collecting into a pod directory (`-args
  -test.gocoverdir`) yields the identical number and a countable
  artifact — but stably 27 counter files for 28 test packages, because
  `internal/version`'s own binary emits none. An expectation with an
  unexplained exemption in it is a false-alarm generator, and a gate
  that cries wolf is worse than the bug.

  What it gives up, stated: a loss that still leaves the total ABOVE
  the gate goes unnoticed. That is the right thing to give up — the
  gate's job is the threshold, and a number understated but above the
  bar costs nothing.
- **The second reading must be COLD, and that is load-bearing.** `go
  test` caches a package's coverage contribution, so a warm re-run
  replays the lost profile **at the same number** (card os-4eaf8b13,
  folded in here and closed with it). A retry without the cache clean
  would make the gate MORE confident of a false regression than it is
  today. The drill asserts the effect ORDER — collect, clean, collect —
  rather than that a clean occurred, because a clean before the first
  reading or after the second would satisfy occurrence and protect
  nothing.
- **A refuted comment is deleted with the fix, not left beside it.**
  The Makefile blamed concurrent binaries colliding coverage counter
  files "at the same pid and second". The names carry **nanoseconds**,
  and a re-exec'd helper child cannot collide with its parent at all:
  `testing`'s `coverTearDown` gives a child with no `-test.gocoverdir`
  its own temp directory and deletes it — confirmed by experiment, for
  a child that exits cleanly and one that is killed. `-p 1` is kept and
  the reason restated honestly: serialized runs are what the measured
  behavior was established against, not evidence for the mechanism the
  old comment claimed.
- **The key-file TOCTOU closes at the SIGNING SITE, not by making two
  reads atomic** (os-9a89245c). `internal/loop` fingerprints the key
  before every act; the act then crosses the CLI seam as `--key
  <path>`, and `loopSigner` reopens that path independently, so an
  atomic replacement between the two reads is observed by only one of
  them. `--as <fingerprint>` moves the comparison onto the read the
  signature uses, which is the actual defect — nothing needs to be
  atomic across the seam once there is no second opinion to disagree
  with.

  Both shapes the review thread proposed were rejected and are recorded
  so they are not revisited: widening the verbs to carry key MATERIAL
  changes a protocol surface seven verbs share and makes a
  material-bearing seam out of a path-bearing one; copying the key to a
  Driver-private path puts private material in a second place on disk.
  Both buy a property a fingerprint comparison buys for one optional
  flag, and a fingerprint is public — it is the `actor` field of every
  record in the chain.
- **The last-ditch exit declares no identity, and that is the same
  exemption rather than a caveat on it.** `strand` calls
  `parkGated(…, lastDitch: true)` so the exit bypasses the loop's
  identity gate and reaches the **admission boundary**, where the fence
  rule gives the authoritative refusal. Appending `--as <cached actor>`
  there would reinstate that gate one layer lower, inside `loopSigner`:
  under a rotated key the exit would stop with `usage` at the seam
  instead of refusing `fenced_out` at the boundary, re-creating in a
  new place the blockage the strand path exists to remove. So
  `lastDitch` now means one thing in two layers.

  Rejected: passing the *currently observed* fingerprint on that act so
  the seam check "passes". It keeps a check nominally alive while
  changing its meaning from *the identity the loop declared* to
  *whatever is on disk right now* — the property the flag exists to
  deny, dressed as compliance.
- **A drill against a double cannot see a seam it never crosses.** The
  plan's first draft cited `internal/loop`'s existing rotation drills
  as the protection for the last-ditch exemption. They cannot provide
  it: those drills run against the `recorder` double, so `--as` never
  reaches `loopSigner` and they pass with the regression present. Both
  guard drills therefore live in `cmd/seed`, driving the real CLI, and
  the interleaving one rotates the key from inside a `loop.Verbs`
  wrapper — after the loop's check, before the CLI's read — so it sits
  in the window by construction rather than by timing. A race
  reproduced by sleeping passes green on a slower runner.

## Phase 9 item 3 — the unattended maintenance loop (os-8a5f14bb, plan #203)

- **A reap answers an unanswered request, never a timeout.** The plan's
  first draft named "the claim's own lease elapsed" as the fact
  corroborating silence. **There is no lease** — the word appears once
  in the whole `next/` spec tree, in the sentence denying it
  (`observations.md`: *"Seed holds no lease: a claim stands until a
  deliberate exit or a reap"*). Implementing it would have meant
  inventing lease semantics or picking an undeclared threshold.

  The corroboration that exists is better, and `executors.md` named
  this card as its consumer: the force path, where a worker that
  **ignores its interrupt** is killed and reaped. So a reap requires
  the `expired`/`wedged` classification **and** an admitted
  `run.interrupted` on the active fence, or an admitted
  `wedge.declared`. Both are judged by whether the fact passed the
  boundary at its own position (`admit.InterruptRequested`,
  `admit.WedgeDeclared`), so a raw unprivileged interrupt corroborates
  nothing.

  That changes what a reap MEANS: not "long enough has passed" but
  "someone asked, and nothing happened", which is the only
  corroboration a channel declared lossy can support — and it is why
  there is no threshold in this lane to tune.

- **`wedge.declared` needed its own derivation, and the reason is worth
  recording.** Unlike `run.interrupted` it is a FREE verb: no
  transition-table row, no fold fact, so there is nothing on
  `SubjectState` to consume and the records are the only place the
  declaration exists. `admit.WedgeDeclared` sits beside
  `InterruptRequested` rather than in `internal/maintain`, because "did
  this fact pass the boundary at its own position" is one question and
  belongs in the package that answers it for everything else.

- **`no_data` carries no reap path whatever**, however old the claim,
  and corroboration does not rescue it. A stream holding nothing looks
  exactly like a worker that died before its first line AND exactly
  like one whose lossy channel dropped everything. The drill plants the
  corroboration standing, because that is the case where an
  almost-right rule would reap.

- **The evidence-grade checks moved into `internal/reconcile`.** They
  lived unexported in `cmd/seed/reconcile.go` — attested-head
  reconciliation, target-rewrite detection, receipt retrievability —
  and they are the ones that see divergence with no record to derive it
  from. A maintenance pass built on `reconcile.Classify` alone reports
  **clean** over a rewritten target: green, and omitting exactly the
  divergence this loop is chartered to reconcile (review finding on
  #203). One implementation, two callers.

  The drill for it carries a CONTROL: it asserts the pass reports
  `target_rewritten` and that `Classify` over the same ledger does
  **not**. Without the control the drill would pass on a pass that
  reported everything for some other reason, and "consumes the complete
  result" would be untested.

- **A checkpoint persists a snapshot a fresh reader can start from.**
  "Checkpoint (signed)" would have let every acceptance criterion pass
  with an unusable checkpoint. The payload is now the strict
  `{format, snapshot, location, position}`, validated at admission,
  with the canonical materialization written to the artifact store
  first — so the event cannot name a location nothing can fetch.

  **Shape at the door, contents at the read**, and the split is forced
  rather than chosen: `admit.Context` carries no artifact store,
  because admission reads the ledger alone, so retrievability is not a
  fact admission can establish. The reader fetches, verifies against
  the signed hash, and starts. Saying which check lives where is the
  honest version of "validated at admission".

- **The unsettled-run lint is CONSUMED from `internal/obligation`**,
  never re-derived. The anchoring is the whole subtlety: post-close
  settlement is valid, so the flag rises only once the subject has
  taken a subsequent window or reached a terminal state. The mutation
  that replaces it with a closed-without-settle predicate looks
  obviously right and files a finding against every run in flight.

- **A finding files a defect contract, never an escalation.** An
  escalation freezes a contract and demands a human decision; a finding
  is work somebody should do. Consequence, stated rather than buried:
  this loop can create work, which is authority — bounded by being
  attributable and by filing nothing but contracts, since it cannot
  claim what it files.

  Filing is idempotent **through the ledger itself**: the defect id is
  a stable hash of class and subject, so a second pass re-files the
  same subject and the boundary refuses the duplicate. A maintenance
  loop that remembers what it filed is one that can forget.

- **`seed maintain run` is a verb although `seed loop run` was
  refused**, and the asymmetry is the argument rather than an exception
  to it. The worker loop has no verb because Seed does not own the
  work. The maintenance lane's work IS Seed's own: there is no work
  step to supply.

- **Refused acts are reported and the pass continues; broken effects
  stop it.** A refusal is the boundary doing its job and the rest of
  the pass is still worth running. A store that will not write or a
  rebuild that will not build stops the pass, because continuing would
  checkpoint a state that was never materialized.
## Phase 9 item 4 — small-team and fleet fixtures (os-6a08b166, plan #204)

- **The terminal reconciliation surface was missing, not merely
  local-only.** The plan's first draft carded `verdict render --remote`
  and stopped fleet mode at `submission.made`. Review established that
  small-team stopped short too, and that **`merge.requested` and
  `merge.observed` had no CLI verb at all** — only `ledger append`,
  which runs no rules. A lane cannot drive a loop through verbs that do
  not exist, so the surface came into scope: all three verbs now reach
  both postures, reusing the loop verbs' transport without joining
  `loopverb`'s catalog (that names the acts a LANE declares, and the
  merge chain is the work lane's and the observer's).

- **D6's posture split is void, and the reason is stronger than the one
  that retired it.** It said fleet needs remote (true) and small-team
  needs local because `verdict render` was local-only. This card fixed
  that — but small-team could never have run locally anyway: `claim
  take` is refused off the remote, and a claim is the loop's first act.
  So the mode is **purely the identity plan**, which is what the
  charter says it is; neither clause of III.J mentions transport.

- **Disjointness is proven against the case that threatens it.** The
  drill grants the implementing actor the `verdict` capability
  deliberately. Ungranted it refuses `out_of_grant` — capability
  absence, which `admit.go` is careful to call a different thing — and
  the drill would have proven only that a key without the grant cannot
  render. The charter's claim is that disjointness holds when one
  person runs everything, and such a principal can grant themselves
  everything. Granted, the key still refuses `not_independent`.

- **The middle arm is a lost race, and the rival is planted inside the
  window.** A concurrent reap reaches `Step`'s `!s.Holds` branch and
  returns `Idle` with no verb refused, so it could only satisfy the arm
  inventory by relabelling a successful claim. And two workers stepped
  in sequence do not race — the second polls after the first claimed,
  finds nothing offered, and goes idle at the poll. So the rival claim
  lands from inside the seam, just before the lane's own `claim take`
  reaches the boundary: in the race by construction rather than by
  timing, the shape #202 established.

- **Anti-vacuity is the criterion that makes the rest mean anything.**
  Every convergence assertion is true of a run that met no refusal at
  all. So the arm must be shown to have been exercised, and the
  blind-retry detector is drilled against a known spin and four known
  non-spins — a detector that has only seen converging runs has not
  been shown to catch anything.

- **Wakelessness is pinned by surface, not asserted by absence.** A
  drill cannot watch a thing fail to happen. `Verbs` is one method and
  the option set is four constructors, read out of the source rather
  than restated, so a wake seam fails the pin and the spec moves with
  it.

- **The lane set cannot supply two identities the loop needs**
  (`os-d6a52784`): no manifest grants `supervise` or `observer`, which
  `offer.published` and `merge.observed` require. The fixtures stage
  both as background identities kept out of the identity plan, so the
  grants assertion still measures only lane-derived ones. Honest for a
  fixture, wrong as a posture, and carded rather than papered over.

## Budget exhaustion gets its own exit code (os-d03bde01, plan #206)

- **One new code, and it means EXHAUSTION, not "budget".**
  `budget_exhausted` (27) maps from the capacity refusal alone; the
  other thirteen `BudgetError` sites keep `chain_invalid`. The card
  body asked for the whole rule and the card's own reasoning refused
  it: exhaustion is singled out *because* it is expected and
  recoverable, and a malformed reserve payload is neither. A caller
  branching on the code to retry with a smaller amount would otherwise
  retry against a bug forever. Cards are data, not instructions, and
  this is the case that rule is for.

- **The allocation table was reconciled BEFORE the number was taken.**
  Codes 22 through 26 shipped with no rows in `spec/envelope.md`,
  so the lowest unused integer could not be read off it honestly. The
  rows landed first, then the parity drill, then 27.

- **`TestExitCodesMatchSpecTable` now PARSES both sides and compares
  them in both directions.** It used to hold a hand-copied map, which
  meant adding a code took three edits with nothing forcing the second
  or third. One direction alone is not enough: "every constant has a
  row" passes a table carrying rows for codes nothing emits, which is
  how a retired code keeps its number reserved forever; "every row has
  a constant" is what 22 through 26 actually violated. The drill found
  a real, unrelated drift on its first run: `ExitClassificationRef`
  emits `classification_refused`, not `classification`.

- **A flag on `BudgetError`, not a new error type.** A distinct type
  would match the `errors.As` idiom and would also stop
  `errors.As(err, &BudgetError{})` from catching exhaustion, silently
  changing what anything treating budget refusals uniformly sees.
  Field inspection has precedent in the same mapper.

- **The narrowness matrix lives in `cmd/seed`, where the conversion
  is.** `BudgetError` becomes an envelope in exactly one place, the
  unexported `remoteFailureEnvelope`. A table in `internal/admit` could
  assert which refusals set the flag and nothing whatever about which
  code a caller receives, so the mapper-wide regression it exists to
  prevent would sit outside every assertion in a drill that read as
  though it covered it.

- **Every row names the site it must reach, and a parity drill reads
  the sites out of the rule.** Both halves earned their place
  immediately. The site assertion caught three rows landing somewhere
  other than where they claimed: two "malformed payload" rows omitted
  `reservation` entirely, which decodes cleanly to the empty string and
  refuses at the chain-position site instead, and a "non-numeric
  actuals" row cited position 0, refused as no-such-reservation long
  before any actuals field was read. The parity half caught the larger
  miss: the first matrix had twelve rows against thirteen sites and
  reached only eight of them, leaving the unknown class, the laundered
  reservation, the double close, the stranger's close, and the spending
  gate with no assertion at all about the code they return.

- **The characterization pin was removed, not inverted.** The old drill
  in `loop_e2e_test.go` existed to fail when this landed. What replaced
  it asserts the loop's exhaustion park carries `budget_exhausted`
  verbatim into the packet's findings, **read back from the ledger**
  rather than from the driver's own report: what the driver saw is not
  what a successor reads.

- **An exit is a family; the machine code can be finer, and the table
  governs both.** Review found that exit 22's new row claimed the exit
  was `seal_broken` while `cmd/seed/seal.go` also emits
  `seal_unauthorized` on it. The fact generalizes: four exits ship a
  second code today (`ledger_not_empty` on 3, `posture_undeclared` on
  4, `seal_unauthorized` on 22, `posture_unreadable` on 66), and every
  one is a narrower case of its exit rather than a different meaning.
  That is the shipped design rather than drift, so the table says it
  now, with the line drawn where it belongs: a condition a caller must
  act on **differently** takes its own exit, which is exactly why
  budget exhaustion did not stay a refinement of `chain_invalid`.

  The drill missed it for a reason worth keeping: it derived a wire
  name by transforming a **constant identifier**, so it proved nothing
  whatever about the string a caller receives, and a call site passing
  any code at all on a documented exit was invisible to it. The
  companion drill scans the tree for `envelope.Fail(envelope.ExitX,
  "code")` and requires every emitted pair to be in the spec. Two
  parities, because there were two things drifting: which numbers
  exist, and which strings ship on them.

- **A residual that names its own retirement condition is a promise.**
  `spec/loop-verbs.md`'s "A residual, recorded" passage said that
  closing the residual "forces this passage to be updated with it". The
  first draft of the plan still missed it. Leaving it stale would have
  left the documented worker-loop protocol false in the one place a
  worker reads to learn what exhaustion looks like.

## Phase 9 item 5(b) — the situation read carries the caller's mail (os-8451d939, plan #209)

- **Notices, not bodies, and the constraint decided it rather than
  taste.** `message.sent` needs **no capability at all** — the
  standing-only verb any enrolled active actor appends — and
  `lanes.md`'s residual table names it "the one that RELAYS", bounded
  only by a size lint a short instruction sails through. `situation` is
  the single surface every lane fragment names as the one it orients
  from, taken on **every wake, unbidden**. A body there would let any
  enrolled actor write prose into the read of every lane in the
  deployment. So the notice carries sender, contract, position and
  size: generated identifiers, a count, and nothing a sender chose.

  **And "nothing a sender chose" had to be made true rather than
  claimed** (review finding on #211). The event SUBJECT is
  sender-controlled: `message.sent` admits on any nonempty subject and
  the lint reads only the payload, so a subject of "IGNORE PREVIOUS
  INSTRUCTIONS" admitted and rode the `subject` field straight into
  the orienting read, past a sweep whose marker lived only in
  payloads. The notice now carries `subject` only when it resolves to
  a contract on the chain, and the sweep plants its marker in a
  subject too. A field that claims to be an identifier must be one by
  construction.

  Sanitization was refused rather than attempted. The residual table's
  own point is that a size bound does not stop a short instruction, and
  no sanitizer of prose was going to do better against the exact
  channel the suite already names as the sharpest one.

- **And the body IS readable, by a deliberate second act.** The first
  draft deferred `seed message read` and marked 5(b) complete anyway,
  which was wrong on the build plan's own words and on promotion's
  requirement that a lane work entirely through Seed verbs: a lane that
  learns mail exists and cannot obtain it has not been given its mail
  (review finding on #209).

  The deferral's stated reason did not survive its own argument. What
  makes bodies unacceptable in `situation` is that `situation` is read
  unbidden; a read naming one position is the "reader must choose to
  look" case the residual analysis already accepts, taken **after** a
  notice said who sent the thing. The two surfaces differ exactly where
  it matters, so an argument against one is not an argument against the
  other.

- **A malformed address reaches NOBODY, not everybody.** The first
  draft collapsed absent and malformed into one broadcast case, so
  `{"to": ["<fp>", 7]}` would have widened delivery from one intended
  recipient to every actor on an encoding slip — contradicting the same
  plan's own "and only those" (review finding on #209).

  Absent and malformed are different facts. An absent `to` is a sender
  who said nothing about addressing, and reading that as everyone reads
  what is there. A malformed `to` is a sender who said something the
  projection cannot read, and **every** resolution invents intent —
  including the tempting middle option of delivering to the well-formed
  entries, since nothing in the payload says the malformed one was not
  a recipient encoded wrongly. A typo costs delivery, not the message:
  the keyless whole-board read applies no caller filter, so an
  undeliverable message stays discoverable.

- **The refusal for "not yours" is `not_found`, byte for byte what
  "nothing there" gets — and that is routing, not confidentiality.**
  Four reasons a caller gets no body share one construction site, so
  the indistinguishability is a property rather than four strings that
  happen to match today. What it is NOT is a secrecy guarantee (review
  finding on #211): the ledger is plaintext and the audit record by
  charter design, the projections carry every payload verbatim, and
  `seed ledger show` returns any event to anyone with repository read
  access. Encrypting bodies to recipients was refused as the answer,
  because it would contradict the ledger being the record; the spec
  now says addressing routes and does not conceal, and points a body
  that must be confidential at sealed checks. `not_recipient` (exit
  23) was NOT reused: it names the sealed-envelope recipient set, whose
  answer is "re-seal to the current set", and sharing a code across two
  different answers is what `envelope.md`'s allocation rule forbids —
  the rule os-d03bde01 had just finished drilling.

- **Unread is the cursor and nothing else.** No `message.read` verb, no
  stored read-state, `message.acked` still unimplemented. The position
  a lane carries forward IS its read cursor; a verb recording that
  someone looked would hand it a second cursor to disagree with the one
  it already has. An ack means "I acted on this", which a cursor cannot
  derive, and that is why the charter keeps it separate.
## The shipped role set could not publish an offer or observe a merge (os-d6a52784, plan #210)

- **Neither two new lanes nor two grants on existing ones, and the
  charter decided it.** §II.11 is a closed enumeration — "Six lanes",
  numbered one through six, repeated in the glossary, cited by
  `lanes.md`'s opening line and by III.J's own row — so a seventh edits
  a normative list by implication. And both missing parts already exist
  in the charter outside that list: the supervisor is §II.9, its own
  section, and the observer is §8's governed observer. They are not
  missing lanes; they are roles the charter kept out of the work loop
  because neither takes work. Granting `supervise` to the dispatcher
  would have been the same error smaller: that is the lane defined as
  reading the most untrusted text.

- **A required `kind`, never defaulted, and the six validated by name.**
  Defaulting would have let the six existing manifests keep passing
  while silently acquiring a claim nobody wrote. Making it required
  meant editing the six to say `"kind": "lane"`, which was the point:
  the enumeration became a property of the files. And without the
  by-name check, the previous decision would have made it trivial to
  add a seventh lane by dropping a file in a directory — the charter's
  closed enumeration enforced by nothing. The COMPLETE half (all six
  present) lives in the shipped-set drill rather than in `Validate`,
  because a fixture validating one manifest in isolation is not a
  deployment missing five lanes.

- **`sealer` rides the verifier, not a third role** (review finding on
  #210). `check.sealed` accepts `[sealer]` alone — no operator row —
  and nothing granted it, so the coverage drill as first planned was
  unsatisfiable. The charter's isolation requirement is specifically
  from *implementation* grants (§7), and `sealed-checks.md`'s rule
  forbids `claim` and `operator` by name, permitting `verdict`. The
  positive argument is stronger: the check bodies are encrypted to the
  verifier keyring, so a separate authoring identity would be one that
  cannot read back what it wrote. Nothing was concentrated that was not
  already — a compromised verifier key already decrypts every seal.

- **`kind` governs the mode fixtures, or the closure is prose** (review
  finding on #210). `fleetPlan` ranged over every manifest, so the two
  role files would have provisioned supervisor and observer as fleet
  lanes seven and eight while the plan declared them non-lanes. And the
  fixtures had staged both as identities the test invented — honest
  when no manifest granted the capabilities, and once the manifests
  existed, continuing to invent them would have left the fixtures
  asserting exactly what they asserted before the gap closed. The roles
  are provisioned from their manifests now, and the fleet is drilled to
  be exactly six.

- **Completeness lives in the PRODUCTION path, not in a test** (review
  finding on #212). The first draft kept "all six present" in the
  shipped-set unit test so that single-manifest fixtures stayed green,
  and so `seed lane validate --lanes <dir missing planner.json>`
  certified the set with `lanes: 5`. That protected the tests and not
  the directory an operator supplies. `Validate`, which the CLI calls,
  now checks that each charter lane is present exactly once;
  `ValidateEach` carries the per-manifest rules for the fixtures. The
  split is the reviewer's own suggestion, and the reviewer's exact
  case is a CLI drill.

- **The coverage drill reads the capability table's source, and
  `operator` does not count.** A hand-listed drill cannot notice a verb
  it was never told about, which is how the gap survived a phase; the
  verb literals come out of `keyring.AcceptedCapabilities` itself. And
  `operator` satisfies everything by construction, so counting it would
  let the maintenance lane paper over every future gap — the shape of
  the bug rather than the fix. Both halves proved load-bearing by
  paired mutation: with `supervise` removed from the manifest the drill
  goes red, and with the drill also counting `operator` it wrongly goes
  green; with `sealer` removed it goes red, and with the drill reading a
  hand list it wrongly goes green.

- **The fold and the keyring gate on a named list, `version.Activated`,
  not on `== seed/1`** (os-8e53ffd9). The lifecycle fold skipped every
  record whose version was not exactly `seed/1`, so at `seed/2` no
  claim, reservation or offer folded at all and the first qualification
  drill refused "no open valid reservation" before it reached the run
  rule. The gate the plan named for the keyring ("true for `seed/1` and
  later") is the same gate the fold needs, so it lives once in
  `internal/version`: `Activated(v)` is `seed/1` or `seed/2`, a list
  rather than an ordering, so a version this build has not registered
  activates nothing however it would sort (the `Applies("seed/9")` pin
  stays false). `tuple.Applies` is the narrower gate for what `seed/2`
  added on top.

- **Tuples live beside the string view of grants, not in place of
  it** (os-8e53ffd9, D2 refined). The plan retyped `Entry.Grants` as
  `[]Grant{Capability, Tuple}` with `Grants()` keeping the string view.
  Ten non-test readers use `Grants` as `[]string`, and every one of
  them cares only about capabilities; the tuple set is read at exactly
  one site (the run rule) and one surface (`offer list`). So `Grants`
  stays `[]string`, and the set is a sibling `Tuples map[capability]
  []Tuple` with `GrantTuples(actor, capability)` as the one accessor,
  returning a copy. The drill that a qualified grant never drops out
  of the string view is the same drill either shape needs.

- **A seed/1-only validator refuses an upgraded chain at the first
  seed/2 record, not at the upgrade record** (os-8e53ffd9, AC2c
  refined). The plan said "at the upgrade record with
  `version_mismatch`". The verifier's standing mechanism is that the
  upgrade event is the last event of the old version and is itself
  valid; the NEXT record is the first judged under the new version, and
  that is where a build not supporting it refuses, as
  `version_unsupported` in the `version_mismatch` exit family. Moving
  the refusal onto the upgrade record would change `internal/ledger`,
  which the plan's scope guard keeps out of this card, and would buy
  nothing: either way the refusal names the version and never the
  grant, which is the property the criterion exists for. The drill pins
  the position and the reason as they are.

- **`seed run start` is a CLI verb over `run.started`, not a registered
  loop act** (os-8e53ffd9, D9 applied). The loop-verb registry is the
  worker lane's vocabulary: what a `claim` lane declares in
  `acts_through` and what the loop driver's act gate admits.
  `run.started` is the supervisor's, and `loop-verbs.md`'s own table
  says a `claim` lane cannot reach it. Registering it would have let a
  worker manifest declare it and the driver perform it, and would have
  touched `internal/loopverb` and the lane manifests, which the plan's
  file scope does not name. It reuses the loop verbs' transport, signer,
  session and derivations (`activeFence`, `soleOpenReservation`, which
  now names the citing act in its refusal) and refuses an unknown
  subverb the way `maintain` and `merge` do.

- **A raw-pushed start's declaration is re-judged where its fence and
  reservation are** (review finding on #216). `RunStartValid` decoded
  the tuple as optional and checked only signer, fence and reservation,
  so a raw `seed/2` start with no tuple, a malformed one, or a drifting
  one counted as admitted: `Provision` would have skipped the
  resolved-tuple comparison on a nil declaration, and the
  one-run-per-window check would have let it block the legitimate
  start. The decode (`declaredTuple`) and the set rule (`tupleDrift`)
  are now one function each, called by the run rule at the tip and by
  `RunStartValid` at the record's own prefix under the record's own
  version. Same shape as the earlier "fold presence is never proof of
  admission" finding, one field later.

- **A version gate on a field reads presence, not value** (review
  finding on #216). The offer rule refused `tuples` before `seed/2` only
  when the decoded slice was non-empty, so `"tuples": []` passed on a
  `seed/1` chain while a `seed/1` validator, which strictly decodes
  eligibility as `{capabilities, tiers}`, refuses it: two admission
  points disagreeing on a record still labeled `seed/1`, which is
  exactly what the bump exists to prevent. The field is decoded raw and
  its presence is the gate; its value is parsed after.

- **A malformed scope folds to nothing, never to a wider one** (review
  finding on #216). The offer fold dropped unparseable tuple members
  and kept the offer, so an all-malformed scope became an UNSCOPED
  offer every eligible worker saw. Both the offer fold and the
  run-start fold now treat a malformed tuple as a malformed payload:
  no fact, one anomaly, the `run.settled` posture the tree already
  had.

- **`next/seed` is untracked and ignored; the build is unchanged**
  (os-a487b3b5). The 12 MB binary was committed twice as a side effect
  of a hand-run `go build ./cmd/seed` from the module root (#135, #137)
  and then had to be reverted before every commit in every worktree
  where a build ran. `check-next` runs `go build ./...`, which with a
  multi-package pattern discards every executable, so there was no
  output path to point it at; the fix is the index removal and one
  ignore rule beside `bin/`, and the file stays on disk in existing
  checkouts as an ignored one.

## The tier vocabulary (os-be12ac16, plan #219)

- **The plan's review threads bind the task even though the plan
  merged over them** (os-be12ac16). #219 merged with three bot findings
  unresolved. Each is answered here rather than left to rot: they were
  findings about the design this PR implements.

- **No protocol bump: the filing check is admission policy, and
  admission is not chain validity** (os-be12ac16, Codex on #219). The
  finding argued that refusing `tier: "wizard"` at admission while an
  older validator accepts the record is a validation disagreement that
  `protocol.md` says bumps the version. It conflates the two seams the
  tree keeps apart: verification tolerates an unknown tier in history
  in every build, old and new (the fold keeps the value as filed, and
  every reader takes the strictest row), so no two validators disagree
  on whether a chain is VALID; they disagree only on what the
  cooperative boundary will PROPOSE, which is the halt, classification
  and capability precedent `actors.md` records as bump-free. A bump
  would have claimed that a `seed/3` chain carrying a raw-pushed
  `wizard` is corrupt, which it is not.

- **Admission-path budget fixtures move to a member; raw-pushed ones
  stay** (os-be12ac16, Codex on #219). The plan's file scope named
  only tiers outside the vocabulary; D3 validates budgets at the same
  site, so every fixture that files `budget: "s"` THROUGH admission
  (`remote_test`, `seal_cli_test`, the `seed-admit` hook drills, and
  the gitref race storm, whose clients re-validate every draft against
  the refreshed tip) moves to `small`, while the ones that append on
  the library seam keep
  their unknown values, which is the tolerant-fold coverage the raw
  seam exists to give. `ten`, the budget drills' injected class, is a
  table member for the test's duration and files unchanged.

- **The mis-tiering residual stays pinned; the vocabulary narrows it,
  it does not close it** (os-be12ac16, Codex on #219, D5 refined).
  The plan said the residual is closed and the pin replaced. What the
  vocabulary closes is the unknown-value hole; a dispatcher persuaded
  to file the VALID value `trivial` still files a contract the plan
  gate and the sealed-checks lint exempt, because nothing yet attests
  who may make that filing. `plans.md` already names that as "until
  tier provenance lands". So the replaced drill asserts both halves:
  `wizard` refuses naming the three tiers, and `trivial` from the
  persuaded dispatcher still files. `lanes.md`'s residual row says
  what narrowed it and who owns the rest.

- **`Tier(name) (TierRow, bool)` is the accessor; `TierGates(name)` is
  what the sites read** (os-be12ac16, Copilot on #219). The plan's
  mutation list named `Tier()` returning the trivial row for an unknown
  name; the accessor is `Tier(name)`, and the sites read through
  `TierGates`, which applies the strictest-row rule once so no site
  re-derives it. The mutation that matters is the one named: `Tier`
  handing an unknown name the trivial row, which `TierGates` then
  relays, and the plan-gate and unsealed drills catch.

- **Three authority sites, derived from the tree** (os-be12ac16). The
  card said two; `cmd/seed/verdict.go`'s `unsealed` refusal is the
  third, and it now reads the table like the other two, with drills
  for `critical` and a raw-pushed unknown tier at each.

- **Budget classes are named in capacity order** (os-be12ac16). The
  refusal says `small, medium, large`, the order the spec table and
  the criterion use, rather than alphabetical.

- **A non-string tier or budget refuses; a decode failure never skips
  the check** (os-be12ac16, review finding on #222). The first cut
  decoded the two fields into a string-valued struct and ran the
  vocabulary checks only when the decode succeeded, so `"tier": 1`
  passed presence, failed the decode, and was admitted with no check at
  all, after which the tolerant fold read an empty tier. Each field is
  now decoded on its own from the presence map, and a value that is not
  a JSON string refuses as a vocabulary refusal naming the raw value.
  Drilled for a numeric tier, an object tier, an array budget and a
  boolean budget.

- **No protocol bump for the filing check, restated on the task PR**
  (os-be12ac16, review finding on #222). The same argument as on the
  plan PR, answered above: verification does not run the completeness
  or vocabulary checks (the raw-pushed `budget: "s"` fixtures in the
  project and reconcile drills verify unchanged after this card), so no
  two validators disagree on chain validity; only the cooperative
  boundary refuses, which `actors.md` records as bump-free by design.

## Phase 10 item 2 — eval contracts and the qualification verbs (os-03e47abb, plan #217)

- **A key holding no eval lane refuses `out_of_grant`; a lane key is
  owed the other lane's acts** (os-03e47abb, AC7 refined). The plan's
  criterion says the supervisor performs its subset and reports the
  rest as owed by the other lane, and that "a key holding none refuses
  each `out_of_grant` with nothing appended, reported, never retried".
  Both read literally: `seed eval act` classifies by the key. One
  holding `supervise`, `dispatch` or `operator` performs what its
  grants admit and reports the rest under `owed` with the lane that
  owns it, exit 0; one holding none of the three gets every act under
  `refused` with code `out_of_grant`, nothing signed, exit 14. A
  verifier running the act is the drilled case. Attempting the append
  and letting the boundary refuse would have produced the same rows at
  the cost of a session per act.

- **Each act is one derivation at one instant** (os-03e47abb, AC5
  refined). A spot-check the dispatcher files becomes `ready`, and the
  offer it then owes is the supervisor's. The criterion phrases the
  two as separate invocations ("under the dispatcher's key files and
  specifies … reporting the offer as owed by the supervisor, and under
  the supervisor's key publishes it"), and the implementation keeps it
  so rather than looping to a fixpoint inside one act: `Due` is read
  once, at the declared `--as-of`, and what the performed acts make
  due next surfaces on the next act. A fixpoint loop would have had
  one invocation sign acts the derivation it reported never listed.

- **The eval marker is refused at an earlier tip, not merely unread**
  (os-03e47abb, D8 refined). The plan gates the field in the fold at
  `seed/3`. Admission gates it too, on presence read raw, for the
  reason item 1 gated the offer's `tuples`: a `seed/2` validator's fold
  would read a marked filing as an ordinary contract, so admitting one
  at a `seed/2` tip would have two validators agree the chain is valid
  and disagree on what the contract is. The refusal names the version.

- **A duplicate disqualification refuses as "nothing to disqualify"**
  (os-03e47abb, D4 refined). The rule's one-verdict-one-consequence
  check is reached only for a tuple still admissible; the keyring's
  preview runs first in the grant rule and finds the tuple already
  removed. Same outcome, earlier rule, and the message is the more
  useful one: the actor holds no admissible grant citing that tuple.

- **`authenticPass` reads the fold's latest verdict, so a later raw
  verdict shadows an earlier authenticated pass** (os-03e47abb). The
  fold keeps one verdict per subject, the latest, and the rule
  authenticates that one. A raw-pushed pass by the implementer after a
  real pass therefore makes the mint refuse rather than admit: the
  shadowing fails closed, which is the direction that matters, and a
  chain carrying such a push is what `verdict_unverified` surfaces.

- **The recomputation seam is `verdict.InputFor`, and sealed evals
  mint nothing yet** (os-03e47abb, step 6). `seed verdict check`'s
  input assembly moved from `cmd/seed` into the package so `Due` calls
  the same function; the CLI delegates. A subject carrying a sealed
  commitment is noted `receipt_unchecked` rather than unsealed, because
  the derivation holds no identity to unseal with; every shipped eval
  is trivial-tier and unsealed, and the note names the gap.

- **The fixture returns a stand, not a context** (os-03e47abb, review
  of the admit drills). The item 1 drills return `ctx` and a `step`
  closure and reassign `ctx` from each step's return. The eval fixture
  drives several subjects from inside closures the test never sees
  return values from, and the first draft read a stale context two
  subjects behind. The fixture now returns a struct whose `ctx` every
  step rewrites and every assertion reads through.

- **A qualification grants `claim` and nothing else** (os-03e47abb,
  review finding on #221). The keyring decoded any non-empty capability
  and added it to the string view of grants, so a `supervise` key could
  have minted `operator` or `verdict` standing for a holder through a
  green eval, escaping the supervisor's own boundary. The plan fixed the
  field to `claim`; the keyring now refuses anything else as chain
  validity, and the drill rows name `operator`, `verdict` and `dispatch`.

- **The verifier boundary is replayed to the verdict's own position**
  (os-03e47abb, review finding on #221, D2 made literal). `authenticPass`
  and the fail path read the TIP keyring, so a raw-pushed verdict from
  an ungranted key became authentic once the key was granted `verdict`
  later, and a legitimate verdict stopped qualifying anything once its
  signer was suspended. Both are now judged at `fact.Pos`
  (`verdictBoundaryAt`, the `VerifyVerdicts` replay), with a drill for
  each direction: a later grant does not reach back, and a later
  suspension does not unmake a pass. The red-verdict lockout keeps its
  tip-keyring reading, which is a different question (whether a fail
  stands NOW), untouched by this card.

- **The marker is bound to the named definition: by path at the
  boundary, by anchor in the derivation** (os-03e47abb, review finding
  on #221). The qualification rule required only that the contract
  carry a marker, so a dispatch key could file an "eval" with a
  trivially green gated acceptance of its own, let a real verifier
  produce a real receipt, and have the supervisor's `eval act` mint.
  The boundary reads no repository, so it binds what it can: the
  acceptance spec must be the named definition's fixture
  (`evals/<name>/fixture/…`, executable and gated). `Due` binds
  the rest: the ref must equal the shipped definition's `Anchor.Ref`
  at its reviewed commit, or the contract is noted `unbound` and
  neither offered, minted from nor disqualified from (a fake eval that
  fails must not disqualify everyone either). Spot-check filings are
  bound by construction, since `File` writes the anchor. `EvalRoot`
  moved to `internal/transition` so the boundary and the eval package
  read one layout.

## The client's private git dir arms auto-gc in production (os-711b3028, plan #224)

- **Every construction, not only init (D1).** `NewClient` writes the
  three keys after the init-or-stat on every open. Refused: writing
  only when absent, because a stat-and-branch can drift from what the
  drill asserts and three idempotent config writes cost less than the
  branch. Refused: a `GIT_CONFIG_GLOBAL` in production, because the
  invoking process's global config is the operator's and the engine
  writes nothing there. A write that fails is `NewClient`'s error,
  named by key: a git that cannot configure its own repository cannot
  be trusted to fetch from it either.
- **Two sites, not one (D2).** The card named the client; the tree has
  the verifier's per-run clone too, whose `Cleanup` runs right after
  the checkout that arms the collector. The keys are written between
  the clone and the checkout, repository-locally, so no config outside
  the workspace is consulted or written.
- **The drills read `--local` (D3).** `TestClientGitDirHasNoAutoGC`
  passed before this card for the wrong reason: the process-wide
  global `TestMain` installs satisfied `git config --get`. It now reads
  the repository's own scope; an older-build drill stages a bare dir
  with the keys unset, asserts the hardening on the no-init path, and
  asserts a second construction leaves the config bytes unchanged; a
  workspace drill reads the clone's own scope. The test-side hardening
  and the fixture guard of os-c4e8b57a stay as they were.
- **No production failure was observed**, only the test-side one; the
  two spec sentences say what the engine promises (nothing it made
  mutates after it exits), not that a failure happened.
- **`GIT_CONFIG` is scrubbed and the target is named (review finding
  on #232).** The variable selects the file `git config` reads and
  writes: an unqualified write under it lands in the operator's
  selected file, and `--local` under it refuses with "only one config
  file at a time" rather than overriding it (probed, not assumed). Both
  sites therefore write with `--local` AND run without the variable,
  and a drill at each plants `GIT_CONFIG`, asserts the repository's own
  config carries the keys, and asserts the selected file was never
  written. The scrub is the load-bearing half: dropping it makes
  `NewClient` and `NewWorkspace` fail under the variable; `--local` is
  the explicit target whose removal the drill cannot see once the
  environment is clean, kept because a write that names its file reads
  as what it is.
## Phase 10 item 3 — independence levels (os-99829835, plan #223)

- **The L3 reproduction opens sealed subjects under a key, and never
  skips one silently** (os-99829835, D5 refined; review findings on the
  task PR). The evidence-grade half recomputes an L3 verdict's receipt
  from the verifier's own input seam and classifies
  `independence_unverified` when the digest differs from the cited one.
  A sealed subject's receipt includes its sealed transcripts, which
  recompute only under a recipient key, and the first draft skipped
  sealed subjects on that ground, which excluded every `standard` and
  `critical` contract from the one evidence-grade check. Now
  `reconcile.Reproduction` carries the chain, the fold, an `Unseal`
  hook and a `NotAttempted` report: `seed reconcile --key` opens sealed
  subjects and, meeting a sealed L3 verdict it cannot open, refuses the
  run naming the subject (`usage` with no key, the unseal envelope
  otherwise), the `verdict check` posture of no silent partial
  verification; the maintenance loop opens them under the maintenance
  actor's key and reports what that key cannot open as a skip with the
  reason. The loop passes its records and fold too: the first draft's
  `Evidence` wrapper handed a nil fold and so disabled the reproduction
  for every unattended pass.

- **The modes drills stage the seal through the library** (os-99829835,
  D6 refined). `critical` requires sealed checks, `seal create` reads a
  ledger directory, and both modes run on the remote posture (the loop's
  first act is online-only). The seal is BACKGROUND under the fixture's
  own rule: the envelope is built and encrypted to the eligible
  recipients through `internal/seal`, the ciphertext lands in the
  repository's artifact store, and the commitment is appended on the
  raw seam signed by the sealer-capable verifier, so `sealAuthorized`
  still passes it; nothing the drills assert comes from it. The plan
  approval is staged the same way, and it anchors `accept.md` at the
  spec commit with the submissions ranging from there, because the
  receipt binds the approved plan's bytes at the merge-base and the
  fixture repository holds no `plans/` tree. That `seal create` has no
  remote posture is an existing residual, noted here rather than
  widened in this PR.

- **The declaration is all three flags or none, and `level_short`
  precedes the lockout and the sealed gate.** `seed verdict render`
  refuses a partial declaration at usage; `harness` and `environment`
  come from the local adapter's constants, the `run start` posture. The
  level is computed as soon as the subject and the declaration are
  known, before the red-verdict lockout and the `unsealed` refusal, so
  a verifier learns what its tier requires of it before anything
  about the subject's checks. A declaration on a pre-`seed/4` chain
  refuses at usage naming the version.

- **L2 reads the window's ADMITTED start.** `submissionDeclaration`
  finds the bound submission's window and re-judges its `run.started`
  through `RunStartValid`: fold presence is not admission, so a
  raw-pushed start carrying a tuple the holder's grants do not cite is
  no declaration, and L2 cannot be manufactured by pushing a start. The
  fold records a malformed verdict `tuple` as an anomaly and no fact.

- **`LevelsApply` is `seed/4` exactly; `EvalApplies` became the list.**
  The plan's D4: the newest gate is an equality until the next version
  lands, at which point it becomes a named list, which is the lesson
  `EvalApplies` teaches in this PR. The next register entry owes
  `LevelsApply` the same edit.
## Phase 11 item 1 — the staged curation stores (os-f30ee0d3, plan #226)

- **The curator's reachable set is three verbs, not two** (os-f30ee0d3,
  AC5 refined). The plan named the proposal and the raise; the boundary
  derives one more, `message.sent`, which any enrolled active key
  appends and which the dispatcher's residual table already names as
  the relay. The curator's residual table names all three, the
  reachability drill pins the set in both directions, and the relay
  row repeats the dispatcher's bound (the classification lint's 512
  bytes per string). Listing two would have made the drill assert what
  the boundary does not do.

- **The window and the fence are the fence rule's; the curation rule
  refuses the non-holder** (os-f30ee0d3, AC1 refined). A dead end
  always cites a fence, so on an unclaimed contract the fence rule,
  which runs before the curation rule per the charter's check order,
  has already refused it as `fenced_out` ("no claim is active"), and a
  stale citation too. The first draft re-derived both in the curation
  rule, which made two refusals for one fact; what the fence rule lets
  through, a claim key that is not the holder citing the right fence,
  is the curation rule's one refusal. The drill asserts the refusal
  the boundary actually gives at each row.

- **Citations are re-judged from the record, at their own prefix**
  (os-f30ee0d3, D1). `curation.ObservationAt` folds the prefix before
  the cited position and requires the record to be the holder's (a
  prior claimant's, for exits) dead end or finding-bearing exit citing
  the fence active there; `curation.HypothesisValid` requires the
  cited proposal's signer to have held `curate` at that prefix and its
  support to pass there. Fold presence is never proof of admission
  (`RunStartValid`'s posture), so a raw-pushed dead end supports
  nothing and a raw-pushed proposal promotes nothing: no stage skips,
  by citation, even against a hostile writer on the raw seam.

- **The probes for derived subjects sign on the derived subject.** The
  affordance catalog probes every verb on the subject the caller asks
  about; a hypothesis lives on the id its claim derives and a
  promotion on the hypothesis it cites, so those two probes are signed
  on their derived subject (`probeSubjects`) and cite what the record
  holds (`curationProbes`: two admitted observations on two distinct
  non-failed contracts, the latest admitted hypothesis), or, holding
  none, what the rules refuse. The proposal therefore appears in the
  curator's orientation read exactly when it is legal, and the
  injection suite's reachable-set derivation keeps working over one
  catalog.

- **A parked contract is blocked, so the ready contract the curator
  raises on is one never claimed.** The plan's AC5 says "a `ready` or
  `review` contract"; the fixture's parked contract folds `blocked`
  (a park asks the queue to reconsider), which refuses the raise from
  every raiser alike. The drill raises on a specified, never-claimed
  contract and asserts parity with the dispatcher on the held and the
  blocked one.

## Phase 11 item 2 — the promotion gate, the contested state, delivery (os-96850e5a, plan #228)

- **The projection's `surfaces` is the record half only** (os-96850e5a,
  D7 refined). The plan asked the `knowledge` projection to carry per
  lesson `surfaces: true|false` with the reason. A projection build is
  input-free and holds no repository, so the repository half of
  surfacing (the anchor's ancestry and the file's digest) cannot be
  computed there; the projection renders the record half (promoted and
  not contested, the reason `contested` when false) and the readers
  that hold a repository (`claim take --repo`, `seed situation --repo`,
  `Provision`, `seed reconcile`) apply the rest. The spec says which
  half is whose.

- **A lint refusal is `lint_refused` under `checks_red`**
  (os-96850e5a, D4 refined). The file half's refusal is a gate gone
  red on content rather than on a command, and the plan allocates no
  exit; `checks_red`'s family ("the verdict derives from what was
  checked, and a red check forbids pass") is the one it belongs to, so
  the refinement rides exit 20 with the gate in its message, beside
  `carrier_absent` for the render's bound-eval refusal.

- **The lint reads the fact for the file's repository-relative path**
  (os-96850e5a, D4 refined). `seed knowledge lint <file>` must know
  which promotion the file is, and a file names nothing but its
  frontmatter's hypothesis; the verb resolves the file's path relative
  to `--repo` and picks the admitted promotion on that hypothesis whose
  anchor path equals it, refusing `not_found` for a file outside the
  store or one no promotion cites. The frontmatter is judged against
  THAT fact, so a lesson file cannot vouch for itself.

- **The eval id keeps its derivation for unbound filings**
  (os-96850e5a, D5 refined). The bound marker joins the subject hash
  only when present, so every existing eval subject, fixture and
  drill is unchanged, and an eval filed for one candidate and one for
  another are two contracts.

- **`seed reconcile --subject <h-id>` walks the promotions.** A
  hypothesis subject is not a contract, so the fold-state lookup that
  refuses an unknown contract would have refused it; the verb treats a
  hypothesis-shaped subject as a request for its lesson findings alone.

**Review fixes from item 1, carried into item 2.** The item 1 PR's
review found four places where a curation check read a folded fact as
an admitted one; the fixes (`WindowAdmitted`, `FailedAt`, the
admitted-only fold with `AdmittedProposalBefore`, `UnderLessonsDir`)
are in item 1's branch and ported here unchanged, with the same drills
against the item 2 fixture (a grantless stranger key raw-pushing a
claim, a dead end, a proposal and a pass). The contest and the
promotion gates read the admitted fold, so an unadmitted proposal can
be neither contested nor promoted.

**Review fixes on the task PR: the fold re-judges contests and
promotions, the lint judges the anchored bytes, the claim derives its
lessons at the landing tip.** Four findings, one root: a fact the fold
read as admitted because it was well-shaped. (1) A raw-pushed contest
moved the stage to contested and disabled a legitimate lesson on every
delivery surface; the fold now binds a contest only when
`curation.ContestValid` passes at its position (the signer held
`curate`, the citations pass `CheckContest`). (2) A raw-pushed
promotion bound in the fold and surfaced once its file resolved; the
fold now binds a promotion only when `curation.PromotionValid` passes
at its position, and `CheckPromotion` is the ONE implementation the
boundary and the fold share, which moved the adversarial arm and the
L1 pass authentication (`curation.AuthenticPass`, the same rule
`FailedAt` replays) out of `admit` into `curation`, so the two cannot
disagree by construction. Phase 10 item 3's levels landed on main
while this was in review, and the promotion's pass authentication
gained the level rule with them: `admit` installs its `levelBoundary`
into `curation.PassLevelCheck` at init, so the fold's promotion replay
applies the same level rule the verdict rule and the merge chain
apply, from seed/4, with no second copy of `LevelAchieved`; a pass
pushed past the verdict boundary at a level the record does not
support is not survival, and the drill pins both the installation and
the refusal. (3) `LintFile` validated the caller's
working-tree body and hashed the anchored bytes separately, so a valid
frontmatter in a later edit could stand in for the invalid promoted
one; the lint now reads the bytes at the anchor first, refuses at
`lint.digest` when the working file differs from them, and judges
those bytes alone. (4) `claim take --remote` derived the surfacing set
at the session's opening view and reported it after a retry against a
moved tip; the act now carries a derivation that recomputes the set
against every refreshed view (the payload holds nothing derived, so
the re-derivation cannot diverge and only refreshes the result), and
the response reports the set at the tip the claim landed on.
## Phase 11 item 3 — the poisoning drill (os-e2f1ad23, plan #229)

**The corpus is a JSON declaration and the scripts are Go, joined both
ways.** The plan's D1 names the shape; the choice worth recording is
that a poison's expectation is either a registered gate or a reason
string, never both, and that the reasons are the three refusals the
boundary raises outside the registry (out of grant, the grant's
disjointness, the acceptance's gate). A poison expecting a gate is
judged by `GateError.Gate`; one expecting a reason by the error's text,
except out of grant, which is judged by type so the message can change.

**The lint poisons have one end.** The file half judges a lesson the
ledger already promoted; its "achieved" end is the file passing `seed
knowledge lint` under `make check`, which gates the lesson PR. The
drill asserts the refusal at the gate and says so, rather than
pretending a claim-time check the delivery does not make.

**A residual that closed while the drill was being written.**
Scripting the promotion poisons showed that a promotion pushed past the
boundary bound in the fold and was a delivery candidate, and the same
held for a contest. Both were named as this drill's sixth residual for
a day, then fixed where the plan's D5 says such a thing is fixed: in
the gate's own package, by item 2's review (the fold re-judges contests
and promotions through the same `CheckContest` and `CheckPromotion`
the boundary runs, which moved the adversarial arm into `curation`).
The residual became two poisons, `raw-pushed-promotion` and
`raw-pushed-contest`, each pushing the refused fact past the boundary
and asserting the fold bound nothing.

**The CLI arm reads the hypothesis position from `knowledge show`.**
The propose envelope stamps the tip, not the fact's position; the
lessons e2e drill already reads the fold's view, and the arm does the
same rather than deriving a position from the tip.

## Phase 11 item 4 — expiry, retirement, dead-end applicability, bloat (os-0d537fbd)

**Expiry is a derivation at an instant, at-or-past.** `curation.Expired`
compares the declared instant with the fact's `expires` and calls the
lesson expired at the second its stamp names; no `lesson.expired` fact
exists, and admission never reads a clock: the reads that surface
lessons (`claim take`, `seed situation`, the projection) take the
instant as a declared input, the offer-liveness posture.

**The fold keeps the latest promotion per path, keyed by path.**
`State.Lessons` and `State.Retired` are keyed by the lesson path, not
the anchor: a revalidation is a new anchor for the same path, and the
questions the readers ask (is this path retired, what stands for it)
are questions about the path. The previous anchor-keyed map with a
same-path sweep was replaced when the first drill asked
`fold.Lessons[path]`.

**The revalidation order is judged in one forward pass.** Promotion
validity now has two halves: the arms (`checkPromotionArms`, every gate
but the order) and the order against the latest admitted promotion of
the path, which `LatestPromotionBefore` finds by judging each earlier
promotion through the arms and the order against the latest it has
admitted so far. The first cut asked `PromotionValid` recursively for
every earlier promotion and turned exponential in the number of
promotions of a path, which the item 2 refold drill
(`TestFoldingManyPromotionsNeverRefolds`) caught at once; the same
shape closes the retirement's standing check (`RetirementStanding`).

**A second retirement over a standing one refuses.** D2 names the
gate for a non-latest promotion; a retirement over a standing
retirement of the same promotion is refused at the same gate
(`retirement.promotion`), since the promotion it would revoke no
longer stands. Otherwise a second `expired` after a `regression` would
silently rewrite the standing reason.

**The retirement probe prefers the queried subject's own promotion.**
The affordance list's retirement probe cites the queried hypothesis's
standing promotion where one stands, else the first unretired path, so
the list on a hypothesis answers for that hypothesis and still lists
the verb for a contract subject the way the promotion probe does.

**The dedup lint names the unpromoted file as the duplicate.** With
the fold given, the file the admitted promotion cites is the original
and any other file citing the hypothesis is the duplicate; without the
fold (the poison drill's bare stand), the first by name. The first cut
called the alphabetically later file the duplicate, which named the
promoted file when the duplicate sorted first.

**The stale finding's threshold is inclusive.** `--stale-after 0`
files on expiry itself, consistent with `Expired`'s at-or-past; a
positive threshold files once the instant is at or past expiry plus
the threshold.

**The knowledge projection declares input consumption.** An instant is
an input, and a build at another instant is another build; the stale
flags ride the declared observation inputs' `as_of`, the one family
that carries one, so a bare `AsOf` with no declared family declares
nothing and the build id never collides across instants.

## Phase 10 item 4 — rubric verdicts, the human verdict, calibration (os-2e34f66a)

**With a rubric, the verdict is the derivation's, both ways.** D3
names the pass rule (every item pass at low), the fail item forbidding
pass, and the high item forbidding both. The implementation adds the
symmetric half: `fail` over a scorecard whose every item passes at low
refuses too. Without it a raw fail with an all-pass scorecard would be
authentic, and AC5's "the lockout ignores a raw fail whose items are
all pass" could not hold; with it every boundary asks one question of
a scored verdict, whether it equals what its own items derive.

**The human renders over the deferral's receipt.** D4 makes a human a
key with operator standing. Sealed checks encrypt to verdict keys
disjoint from claim and operator (`sealed-checks.md`), so such a key
is never a recipient and can compute no receipt on any sealed
subject, which is every tier above trivial. Rather than loosen the
recipient rule (authoring isolation is its whole point), the deferral
carries the receipt the machine verifier computed (`{"receipt",
"submission", "scorecard"?, "items"?}`), and `seed verdict render`
from a key with operator standing over a standing deferral retrieves
that receipt intact from the store instead of recomputing; it
validates its own scorecard against it and cites the same digest, so
`verdict check` and reconcile recompute it under a capable key as for
any verdict. On a human-review tier the whole verdict therefore
defers (no rubric, no items) and the deferral is the machine's one
act, which D4 already said in words.

**A calibration cites its verdict whichever way it went.** The gold
may score an item `fail`, so an agreeing verifier renders `fail`; the
verdict qualification cites the calibration's authenticated verdict,
pass or fail, where a claim qualification cites the pass that proved
the configuration. The drill's first cut asked for a pass and refused
its own mint.

**Drift disqualifies tuple-wide under `verdict`, and files once.** The
defect's id is the maintenance loop's shape (class and contract
hashed), derived in the eval package rather than imported from
`maintain` to keep the dependency one-directional; the derivation
skips a defect the fold already holds, so a second pass owes nothing
even before the boundary refuses the duplicate.

**Drift in the modes fixture is drilled at the terminal.** AC7's
"drifted verifier refused until re-calibrated" runs on a local ledger
in `calibration_cli_test.go` through the same verbs the modes drive
(`eval file`, `eval act --gold`, `verdict render`); the small-team
drill carries the rubric and deferred contracts to done. The modes
fixture would need a second full eval work cycle per calibration to
say nothing the terminal drill does not.

### Review findings on the task PR (os-2e34f66a)

**A verdict qualification cites a calibration and nothing else.** The
boundary held the qualification to a bound eval with an authenticated
tuple-declaring verdict, and an ordinary eval satisfies both; a
supervisor could mint verdict authority from a green ordinary eval
with no gold and no agreement anywhere. `EvalInfo` had no kind, and
the boundary cannot read the definition. The filing now says: `seed
eval file` writes `kind: calibration` into the marker (a `seed/4`
field, neither absent nor `calibration` refusing), the fold carries
it, and the qualification rule for `verdict` requires it. The
derivation notes `kind_unmarked` on a calibration filed without it
rather than owing a mint the boundary refuses.

**The first failed calibration closes the bridge.** Drift was
tuple-wide over the actors whose `verdict` grants cited the tuple, and
a verifier holding `verdict` by a bare grant (every shipped verifier
does) cited none: its first failed calibration disqualified nobody,
and the empty-set bridge kept admitting the configuration that had
just drifted. The keyring admits, for `verdict` alone, a
disqualification of a tuple a bare-grant verifier never had cited, as
the act that closes the bridge (cited, empty set), and the derivation
owes it for the verifier that rendered. `claim` keeps the refusal:
item 2's bridge is the never-qualified worker's, and an eval's fail
was never meant to close it.

**`verdict check` reads both artifacts.** It retrieved the receipt and
recomputed it, and never read the scorecard the same verdict cites, so
a deleted or altered scorecard checked green until someone ran
reconcile. It now runs the same check reconcile classifies as
`scorecard_unverified` and refuses `receipt_mismatch` naming the
scorecard.

**A calibration the derivation cannot score is not offered.** The
offer act preceded the gold lookup, so `eval act` without `--gold`
offered a ready calibration whose verdict nothing could compare. The
gold is looked for first; without it the note is the whole of what is
owed.

## Phase 10 item 5 — the trajectory-prefix harness and the lane metrics (os-6bd9ffff, plan #227)

**The frame is subject-scoped, and owed means owed on the subject.**
The plan's D1 says the frame is the subject's folded state, the
actor's affordances on the subject and "the obligation kinds owed to
the actor there". Read as the rows the situation would list for the
actor across every subject, the frame of a point on c-2 would change
whenever an unrelated contract's obligation moved, and `frame_changed`
would fire on chain edits that never touched the decision. The frame
is therefore the situation's rows restricted to the point's subject
(the lane-capability rule mirrored from the situation read and pinned
against it at the terminal), and a point's frame moves only when the
subject it was decided on moved.

**Refused points are judged by their frame alone.** The plan's five
point classes were written as if every class applied to every point.
The first corpus recording showed why two of them cannot: the
dispatcher's one refused attempt is a `claim park`, an act its
manifest never declared and its grants never reached, which is exactly
why it was refused, and `act_undeclared` fired on the corpus the day
it was recorded. `act_undeclared`, `act_ungranted` and
`act_inadmissible` hold admitted points to the configuration and the
boundary; a refused point diverges only as `frame_changed`, the same
frame meaning the boundary presents the same choice and the recorded
refusal standing as what it answered. Stated in `trajectories.md`.

**`ContextOver` rather than a store per prefix.** A frame at every
prefix needs an admission context at every prefix. Replaying each
prefix into a fresh store and calling `ContextAt` is quadratic in disk
writes and, worse, a second derivation; `admit.ContextOver(records)`
builds the same context from an already-verified prefix (the genesis
resolver, the keyring, the halt state, the active version and the
fold, computed from the records) and is pinned against `ContextAt`
position for position on the affordance walk's chain. Verification
stays the caller's: a prefix of a verified chain is verified.

**The corpus is local and the claim goes through the library seam.**
The recorder scenario runs against a local ledger because the journal
is written beside one and the remote posture keeps none; `claim take`
is the one CLI act refused offline (exclusivity is granted at
admission, online only), so the scenario claims through the same
`admit.Check` and append the CLI's remote path runs, and every other
act goes through the CLI's boundary verbs. The corpus is
byte-identical across machines because every key is derived from a
fixed seed, the frames carry no instant, and the scenario fixes
positions and verbs.

**The report is version 13, not 12.** Phase 11 item 4 landed while
this card was in flight and moved the report to 12 (the retired and
stale counts); the lanes section takes 13 and the merge of main
records why.

**`seed plan lint` prints no digest.** The plan's D5 calls the digest
"the figure `seed plan lint` prints"; lint prints `{plan, falsifiable}`
and the retention set keeps lint and classify unchanged, so the digest
is derived by `plan propose|approve` from the repository at the anchor
and shown in their envelopes, and lint is left as it was.

**Every shipped lane acts.** The plan expected the curator to record a
configuration-only trajectory "until its proposal grant lands"; Phase
11 items 1 and 2 landed it, so the scenario drives the curator too
(a proposal over two holders' dead ends, and its duplicate refused),
and the configuration-only path is proved on a fresh ledger where the
observer signed nothing, rather than on a lane the tree has since
given acts.


**Review findings (Codex), fixed on the task PR.** Replay judged a
point's prefix bound on `position+1` for a refused point, so a
trajectory parsed with a refused point at the largest position the
parser admits overflowed the sum past the guard and into the slice;
the bound is now judged on the position itself (a refused point needs
one record more than an admitted one, so it must sit strictly below
the tip), and a hostile trajectory replays as `frame_changed` rather
than a crash. And `plan approve` accepted any non-blank `--pr`, so a
bare name could satisfy the above-trivial submission gate without the
merge commit the approval observes; the CLI refuses a non-anchor as
usage before the session opens, and the shape rule refuses `pr` that
is not `<pr> @ <merged-commit>` at every version, the fixtures having
carried the anchor form throughout.

- 2026-09-03 — the compromised-actor drill (os-465e356e) extends the
  Phase 2 `seed-admit` hook rather than shipping a second boundary.
  The reviewed plan's first revision claimed no server-side hook
  existed and proposed `seed hook pre-receive`; the premise was false
  (`cmd/seed-admit` is the reference deployment, installed by the
  Phase 2 drills), and a second hook would have given the release gate
  a boundary deployments do not use while leaving two security-critical
  implementations free to drift. The hook gains a code-ref half beside
  its ledger half, both derived from the repository it already guards;
  `internal/admit` is untouched (the ledger half consumes the rule set
  as it stands). The pusher's identity is the transport's assertion
  (`SEED_PUSHER`), which the reference deployment's ssh forced command
  derives from the authenticated key; forging it is credential theft,
  outside §I.2. The protected surface (`posture.Config.Protected`) is
  the governance root's alone, not the operator's, so the maintenance
  lane's operator key cannot rewrite the gates it is judged by.
## Phase 11 item 5 — flywheel v0 (os-9075c308, plan #231)

**The report version is "14", not the plan's "12".** Item 4
(os-0d537fbd) took "12" for the `retired` and `stale` counts while this
card was planned, and Phase 10 item 5 (os-6bd9ffff) took "13" for the
`lanes` section while it was in review, so the flywheel section moves
the version twice past the plan's figure. The plan named the next free
number at planning time; the number belongs to the derivation, not to
the item, and the merge of `main` that collided the two is where this
one was settled.

**A flywheel gate refuses at exit 3.** `flywheel.Error` names its gate
(`proposal.shape`, `proposal.path`, `proposal.occurrences`,
`proposal.duplicate`, `proposal.repair_open`, `proposal.repair`,
`merge.proposal`) in the message and rides `invalid_transition`: the
act is an illegal step at this position of the record, which is that
family's meaning, and no new exit is allocated (`envelope.md`'s
allocation rule). `name_taken` is the one refinement, at exit 3, since
the caller's next move is to look at the file that holds the name.

**The fold re-judges the grant, not only the record gates.** The first
cut of `flywheel.Fold` re-ran `CheckProposal` and `CheckMerge` at each
position and bound a well-formed proposal pushed raw under a `claim`
key; the admit drill's raw-push arm caught it. The fold now reads the
keyring at the position and counts a signer without an accepted grant
as an anomaly, the curation fold's posture exactly.

**The proposal probe cites a passed repair.** With a repair contract
passed, the boundary requires the proposal to cite it, so an
affordance probe that cited nothing was refused and the verb vanished
from the curator's list at the one moment it is owed. `flywheelProbes`
carries the citation; an open repair leaves the probe refused, as the
proposal is. `flywheel.Repairs` is the one derivation the boundary,
the probe and the CLI read.

**The mock run binds every declared input to a placeholder.** The v1
engine refuses `workflow run --mock` when a declared required input is
not supplied, so the validation and the repair's acceptance command
both pass `--input <name>=placeholder` per declared input; the names
are read from the staged file (flow and block layouts), since a repair
may have rewritten it.

**The boundary runs before the branch write.** `flywheel propose`
drafts the proposal as it would stand at the base and runs
`admit.Check` over it before staging or writing: a claim key, a
standing proposal or an open repair refuse with nothing written. The
first cut wrote the branch and let the append refuse, leaving a branch
behind a refusal.

**A sixth surface, `seed flywheel observe`.** The plan listed five
subverbs and left `workflow.merged` to "observation"; the raw seam runs
no rules, so the observer needs a boundary-checked act. `observe`
derives the standing proposal's path and cites it at the merged
commit, the `merge observe` posture.

**A seed instantiation is four paths, and the fixture vendors the
engine.** The engine's mock run reads `.seed/`, `scripts/seed`,
`scripts/seed-harness` and `scripts/harness/`; the first fixture
omitted the dispatcher and every role step failed with "harness mock
failed". The verdict runner's scrubbed environment has no HOME, so a
fixture whose acceptance commands are the engine's two verbs writes a
`vendor <path>` line into its own `engine.lock`, the shim's air-gapped
resolution; `flywheel.EnginePath` resolves the path the way the shim
does.

**The planted break is the harness, not the draft.** A deterministic
draft has no step of its own to break: its run steps are the
acceptance's commands (recorded, never executed, in mock) and its role
steps stub from schemas the drafter fixes. The end-to-end break is a
mock adapter that fails on the verdict step, and the repair is the
harness restored on the branch, which is what a real repair of a
generated workflow looks like: the environment or the file, fixed
where the PR lives.

**The walk's completeness check lists the new verbs pre-upgrade.**
`TestAffordancesWalk` accepts `workflow.*` because the root lists every
verb at `seed/0`, where no keyring applies, the same way it accepts the
`curation.*` verbs; the curator's reach over a recurring shape is
pinned by the flywheel admit drill and joined into the curator residual
drill's sweep instead.

## Phase 12 item 2 — the forge-hosted admission service and the protections reconciler (os-5c8a312c, plan #244)

**Exit 28 `drift` is allocated, and the service's race answer is a
refinement of contention.** The plan's first cut reported protections
drift under exit 3; review pointed at the allocation rule (a distinct
condition takes the next unused integer), so `drift` is 28 — a declared
desired state and an observed one differ — with `protections_drift`
its first refinement and the later declared-versus-observed
comparisons (#247's `preseed_drift`, #249's `docs_drift`) refining the
same family. The service answers a stale proposal with HTTP 409 and the
envelope `contention`/`non_fast_forward`: contention at the ref rather
than at a claim, and the one refusal the proposer's loop answers by
re-linking.

**The refusal-to-envelope mapping moved out of `cmd/seed` into
`internal/refusal`.** The plan's file scope did not name a new package
for it, but the service must answer with the very code the CLI would
print, and a mapping that lives in one binary is a mapping the other
re-derives. `cmd/seed`'s `remoteFailureEnvelope`, `failureEnvelope` and
`stampTip` delegate; the flywheel's gate error (#240) joined the shared
mapping during the merge. The hook's `admitUpdate` wraps with `%w` so
the typed refusal reaches the mapping; its stderr text is unchanged.

**Bypass identities are the forge's actor forms, not logins.** The plan
said "the forge login or app slug"; GitHub's rulesets take actors by
type and numeric id, and resolving a slug would add a lookup the core
has no business making. `identity` is therefore `app:<id>`,
`team:<id>`, `deploy-key` or `org-admin` under the GitHub adapter, any
other form refusing by name before anything is written. The doctor
prints it as declared.

**The ledger ref lives in the branch namespace under the forge-hosted
posture** (`refs/heads/seed-ledger` by default): forges protect
branches and tags and nothing under `refs/seed/*`, which is why v1's
ledger was a branch. The hook posture keeps `refs/seed/ledger`; the
remote verbs swap the default for the declaration's branch under the
third posture and honor an explicit `--ref` as given.

**The declaration is found in a fixed order and its absence changes
nothing.** `--config`, else `$SEED_CONFIG`, else `./seed.json` when it
exists, else no declaration — today's behavior byte for byte. An
explicitly named absent declaration refuses `posture_undeclared`; one
that exists and does not parse refuses `posture_invalid` before any
transport, never a silent fallback to pushing.

**The service persists a verified head; the proposing client does not
persist the commit it proposed.** The service records each admitted
candidate as its verified head, so a rewritten remote refuses at its
next fetch (the monotonic-head rule at the service). The client leaves
its persisted head at the tip it fetched: the service's commit is not
in the client's git dir until the next fetch verifies it, and
persisting an object it does not hold would turn the monotonic rule
into a self-inflicted regression.

**`Protected` mirrors #241's shape.** Field, tag, `Parse`,
`DeclarationPath`, `ProtectedSurface()`, `Protects()` and the entry
validation were agreed with item 1's implementation so whichever lands
second merges as a no-op; CODEOWNERS renders from `ProtectedSurface()`,
which includes the declaration itself by construction.

**The fixture's forge asserts identity through `SEED_PUSHER`.** A bare
repository on a local path has no notion of who pushes, so the
fixture's ruleset stand-in reads an environment variable the drill sets
around the service's pushes and clears around the actor's. It is a
model of the forge's rule and the spec says so; production sets nothing.

**The hook builds each record's context with the CLI's constructor,
and a representative history is the drill that proves it.** Pushing a
generated history (`internal/history`: the loop's ten records per
contract, reservations and run brackets included) through the hook
refused `run.started` at the budget rule: the hook assembled its
per-record context by hand (count, tip, keyring, halt, fold) and never
set `Records`, which the budget rule's validity replays read, so a
record the cooperative client admitted was refused at the server — and
would have been refused at the service, which reuses the hook's
judgment. `admitUpdate` now calls `admit.ContextOver(records[:i])` for
every record after genesis, the constructor the trajectory harness
already shares with the CLI, and `TestHookAndServiceAdmitTheRepresentativeHistory`
pushes the history through both. `internal/history` lands with this
card rather than item 3's (which needs it for its budgets) because the
drill that found the gap is this card's; item 3 inherits it.

## Phase 12 item 3 — checkpoint trust, the replay-equals-genesis proof, and performance budgets (os-7508ab9e, plan #246)

**The trust basis is a file in the output root, not a field in every
stamp.** The plan said every published stamp would carry the basis; a
stamp that differs between a checkpoint start and a genesis replay
breaks the byte-identical proof the reader exists to pass, and the
engine's build id derives from the stamp. `<out>/basis.json` carries
the basis instead, written by `seed project start` and removed by a
full rebuild; consumers read it beside the stamp.

**What `signers` skips is exactly the prefix's signature checks.**
`ledger.WithTrustedPrefix` still parses, links, hashes, holds the
version discipline and folds the keyring for every record, verifies
signatures from the trusted position on, and holds the chain hash at
the trusted position to the attested tip. There is no incremental fold:
a `signers` deployment saves the signature work and the trust
question, not the fold, and the spec says so.

**A checkpoint is cross-checked before anything is published.** The
snapshot's files must equal the reader's own derivation at the cited
position; a lying checkpoint is `checkpoint_mismatch` (exit 21's
family) rather than a projection root nobody can explain.

**The perf gate runs under `make check-next` with ceilings at three
times the measured value, and the attempts ratio is held to the
single-ref expectation.** `n` writers under one optimistic ref cost
about `n/2` attempts each; the ceiling holds that shape rather than
pretending contention is constant, and sharded intake stays the
backlog's answer to III.C row 4's "pathologically". The gate re-measures
cold once on a miss, the coverage gate's rule, and the loader refuses a
ceiling with no provenance.

**The undeclared choice is reported, not narrated.** The doctor's
stderr is for the consequences the charter wants in front of an
operator; an unmade `checkpoints.trust` is a machine field
(`undeclared: true`) and `seed project start` is what refuses it.

## Phase 12 item 4 — the preseed, agent-only guardrails, and the protected surface in config (os-0d4f2af3, plan #247)

**The declaration-driven rules are admission policy, never chain
validity, and they ride the context.** `admit.Context` gains a
`Declaration` set by `WithDeclaration`; the ceiling and routing rules
are no-ops without one. The CLI passes the declaration it finds
(`--config`, `$SEED_CONFIG`, `./seed.json`) into every context it
builds; the hook's read of `seed.json` at the default branch's tip
arrives with #241's implementation, and until it merges the hook judges
the two rules with no declaration, which is today's behavior.

**The ceiling reads the roster's kind, not the lane.** A service key is
ceilinged like an agent; a human key is not; the tier order the ceiling
compares by is stated once in `transition.TierOrder` beside the table
rather than derived from the table's booleans.

**The path floor is enforced at the plan lint and the render, not at
admission.** A path is a fact about a repository; admission reads the
ledger and the declaration alone. Both readers share `floorShortfalls`
so the words are the same.

**The capability audit's claim is the derivable one.** The first draft
asserted "no lane grants operator"; the shipped `maintenance` lane
does, by Phase 9's design. The audit now derives the operator-holding
set and holds it to exactly `maintenance`, and the surface's own
protection is #241's root-only rule, referenced rather than restated.

**`seed init --preseed` resolves one kind of drift and refuses the
rest.** A declared protocol above the chain's is answered by appending
the missing activations, since that is what init is for; a different
root or a lower protocol is drift the file must fix, because history is
never edited to match a declaration.

**The doctor reports the blocks; it does not narrate them.** `preseed`
in the result says which blocks are declared; nothing new is printed to
stderr, which stays for the consequences the charter wants in front of
an operator.
- 2026-09-03 — the reap arm (os-32d06c65) threads revocation through the
  maintenance loop's corroboration, not an admission gate. Implementing
  surfaced that claim.reaped admission never consulted
  InterruptValid/WedgeDeclared — the corroboration is Corroborate +
  Reapable, filled from internal/admit. So admit.RevokedHolder feeds a
  new Corroboration.Revoked, Reapable reaps a revoked holder in every
  classification state (no_data included, because a revocation is a
  ledger fact not silence), and no admission rule on claim.reaped
  changes. The plan (#262) was amended to match before implementation.
## The cache carries the event's ts (os-74ce2261, plan #260)

**Verbatim beside parsed.** The envelope's `ts` string is the record
and is stored as written; `ts_unix` is the instant it names, parsed as
RFC 3339 with optional fractional seconds, because a range over the
text mis-orders mixed precision (a review finding on the plan). An
unparseable `ts` folds NULL rather than a guessed instant, queryable as
such and counted under the cache's `ts_unparsed` report key (a review
finding: the lifecycle fold's anomaly count is the lifecycle's, and the
cache does not borrow it). Phase 12 item 4 (#254) took generation 13 for `by_kind` and landed
first, so this card re-bumps to generation 14 with its pins and its
migration, as both PRs said whichever landed second would.
## `ledger show` stamps its failing position (os-37fcf7c6, plan #259)

**The stamp is where the response was computed, not the chain's
length.** `plans/os-fa69345e.md` D3 left `show`'s `chain_invalid`
unstamped and pinned it as a tripwire so the change would be a
decision. The decision: the envelope spec defines the stamp as the
position the response was computed at, a scan that read records
`0..p-1` and failed at `p` was computed at `p`, and `verify` already
stamps that position on the same chain; `show` now does the same, the
tripwire is inverted, and null keeps its one meaning — a refusal
raised before any position was read.

## Phase 12 item 5 — migration from open-seed, drilled against a real export (os-cf13fb51, plan #248)

**A done card without a receipt maps through a pass verdict over an
import note, not `merge.overridden`.** The plan's D7 named the
override as "the honest verb for a done the chain cannot justify";
the boundary admits `merge.overridden` only over a standing fail
verdict on the current submission, and nothing failed on these cards.
An override citing a verdict nobody rendered would be the dishonest
record. The verdict cites an artifact saying "none recorded in the
export", the disposition says so, and the count is in the manifest
(81 of 106 done cards in the fixture).

**Grants derive from the run-log before replay, by rehearsal.** A
static table from v1 verb to capability missed the bridges (a claim
on a card the log never filed is filed and specified first, which
needs `dispatch`), and a first draft that replayed with every
capability granted to learn the verbs cost a third of the wall-clock
in the admission fold. The rehearsal is the same transform over a dry
chain that folds the lifecycle without admission and without the
keyring — cheap, exact, bridges included — and each identity is
granted the capabilities the verbs it signed consume. The manifest
lists what each holds.

**Every imported contract is `trivial`, routed to its squad.** D4
named `standard`; above `trivial` the plan gate refuses a claim with
no `plan.approved` record and the tier's independence level refuses
the verdicts, so a `standard` replay would not admit, and admission is
not loosened for an import. The routing is the card's squad, as D4
said. `unblock` is a row to `contract.unblocked` where v1 recorded a
transition, as D4 said; `blocker_resolved` stays a named drop, since
`decision.recorded` answers a standing escalation and no v1 card
raised one — the unblock that follows it records the transition.

**No generated key holds operator; the importing operator signs what
only an operator may.** `contract.cancelled` and the reconciliation of
a card to its declared state are the operator's records, and the
`import-verifier` service identity renders the verdict where the v1
closer had claimed the card. Both are stated on the disposition.

**`ledger.AppendAll` is a scope addition.** `Append` reconciles the
store per call, so writing 1,345 admitted records took longer than
admitting them. The batch append checks exactly what the single one
checks (resolver, signature, `prev` chaining) and writes one segment;
it is not a raw write, and the chain is verified from genesis after
it. `internal/ledger` was not in the plan's file scope; the change is
sixty lines and named here.

**Exit 29 `import_refused`, not 8 and 21.** The plan routed
`unanchored` under `chain_invalid` and `export_mismatch` under
`receipt_mismatch`; the plan PR's review held that a refusal computed
before any chain exists is neither, and the build plan's routing
allocated a family for the import's pre-write refusals. The three
refinements are `unanchored`, `export_mismatch`, `import_unmapped`.

**The fixture is the anchored tree, written with `--at-anchor`.** The
v1 state head had moved one commit past the newest anchor when the
fixture was taken, and anchoring pushes a tag, an outward act this
task does not perform. `regenerate.sh` refuses by default unless the
head is the newest anchor and derives the export document from the
anchored tree with `--at-anchor`; the document is the tree the v1
command would have printed at that head, which is what the import
verifies.

**`merged` resolves from the repository or is listed unresolved.** The
squash commit naming the card's pull request, else the commit that
added the card's receipt, else the export head with the card under
`unresolved` in the manifest and the envelope (four early cards in the
fixture). Nothing is invented; what stands in is named.

**Evidence matches by kind and instant, and the entry itself is the
artifact otherwise.** v1 wrote the run-log entry and the card's block
from one clock; 98 of 155 evidence entries match within ten seconds,
the rest have no block of their kind left on the card (pruned or
rewritten) and become the entry as an artifact, noted. The match is
not loosened to raise the number.
- 2026-09-03: covergate's first reading runs package test binaries in
  parallel (go's default); only the cold re-collection a low reading
  triggers keeps `-p 1`. The 2026-08-31 serialization was measured
  against lossy parallel merges, and the re-collection rule since made
  a low first reading harmless: it is re-read serially before it can
  fail the gate. The parallel pass can therefore only decide pass,
  never fail, and it takes the coverage collection from 5m21s to about
  2m30s on a 4-core host, which was most of every `make check` in CI.
  (CI/CD performance PR)

## Phase 12 item 6 — docs generation, the handbook, and simulation mode (os-16e55c11, plan #249)

**The generated docs read the same tables the machinery does, and the
exit-codes doc parses the envelope's own constants.** `internal/docs`
renders lifecycle/capabilities/exit-codes/per-lane from `transitions.json`,
`admit.CatalogVerbs()` + `keyring.AcceptedCapabilities`, the parsed
`envelope` `Exit*`/`Code*` constants, and `lane.Resolve`. There is no
runtime enumeration of Go constants, and inventing a hand-kept exit list
is the drift the card forbids, so the generator parses the envelope
source with `go/ast` — a planted constant change fails `docs check`
(`docs_drift`, a new refinement `envelope.CodeDocsDrift` of exit 28). The
refinement codes become exported `Code*` constants: that is the
"refinement registry" the plan names, enumerable from the same parse and
cited by the `Fail` call sites.

**The reference decider is partial — the frame does not determine the
loop's act.** D4 posits a decider that re-runs at recorded points and
agrees with the corpus. But the recorded corpus's loop-act points are not
a function of `trajectory.Frame`: identical frames (implementer pos44 and
pos53) recorded different acts, because the loop's per-iteration choice
depends on internal state the frame does not carry — the very reason #239
recorded frames rather than deciders. A stateless decider cannot
reproduce the corpus, and re-recording it would break the five existing
classes' planted-row drills (a retention requirement). So `decider.Scripted`
decides only where the frame determines the act (a claimable contract in
`ready` is taken) and abstains elsewhere; `replay --decider scripted`
passes on the existing corpus (agree where determinate, abstain where
not) and `choice_diverged` catches a decider that would choose a
different act at a determinate point — the behavioral regression III.O
row 5 wanted, without regenerating the corpus.

**`claim take` is remote-only, so both simulation postures run on a bare
remote.** The plan's D3 split the postures as "a bare remote for
enforced-self-hosted; a local ledger otherwise", but the loop's first act
refuses off a remote (an exclusive verb, online-only). So cooperative and
enforced both build a bare remote; enforced additionally installs the
`seed-admit` pre-receive hook. And the base clock defaults to the real
wall time, because admission stamps each event's ts with it and an
offer's expiry must sit past that — the accelerated `--days` instant
feeds only the reporting surfaces.

**Only planner and implementer are loop lanes.** The plan's "runs every
lane's loop through loop.Driver" is imprecise: only those two declare
`acts_through`. The simulation drives them through `internal/loop` and
the other six lanes/roles through their ordinary CLI verbs, all through
the one credential-free `loopVerbs` seam.

## Phase 13 item 6 — the machine-protocol surface and platform parity (os-b55e5647, plan #261)

**The transport is the one carve-out from "exists iff".** `serve` is a
CLI verb and the protocol itself; a `serve` method would start a
server on the stream that carries it. The registry marks it, the
method set omits it, and the drill pins that it is the only such row.

**The protocol runs the CLI's run function and forwards its stdout.**
Identical semantics are a property of the call, not of a promise: the
JSON-RPC layer builds an argv from the params, runs the registered
function with buffers for its streams, and returns the envelope bytes
it rendered. A verb that reads stdin (`plan`) gets an empty stream
under the protocol, since stdin is the transport's; it takes a file.

**Params map to flags by name.** An object's keys become `--key
value` (booleans bare, arrays repeated, numbers as text), sorted, with
`args` appended verbatim; an array is argv itself. Nothing is
interpreted: the flag parser the CLI already has is the only parser.

**The registry is held to the usage lines.** The drill parses each
group's own "requires a subverb: …" text and compares it with the
registered subverbs in both directions, and refuses a `case` branch in
`run`: the table cannot drift from the dispatchers because the
dispatchers' words are the reference.

**The CRLF refusal lives in the reader.** The plan scoped the change
to `cmd/seed` and a platform package, but a carriage return can only
be refused where lines are read: the segment scanner (`internal/ledger`)
now keeps a carriage return and the record parser (`internal/event`)
refuses it. Two small edits outside the listed scope, each required by
D4's "refuses rather than silently normalizes".

**The Windows matrix is the only Windows run, so it owns two fixes
from main.** The simulate gate drills used a path under `/proc` as a
root nothing can be created under; Windows creates it, and the
deployment reached genesis with no key and panicked. The drills now
use a path beneath a regular file, which every platform refuses. The
artifact store's concurrent-put drill raced eight renames onto one
digest; Windows refuses a rename while a rival's is in flight, and the
read that should have recognized the rival's identical bytes ran
before those bytes landed. The read is now retried briefly. Two edits
outside the listed scope, each visible only on this PR's platform
matrix.

**The path lint reads call sites.** Slash-joined strings are legitimate
for refs, URLs and repository-relative names, so a lint over every
concatenation would refuse the spec's own vocabulary; the lint flags
an `os` file call whose argument is joined with a literal slash or
`path.Join`, which is exactly what a platform would break.

**The line-ending rule reaches git itself.** The first Windows run
refused every committed fixture and every materialized ledger — git
had converted them on checkout and archive, and the new LF-only rule
did its job. So the repository root gained a `.gitattributes`
declaring LF for text (outside the plan's file scope, the smallest
change that makes the rule true on a checkout), the gitref client
tells git `core.autocrlf=false` and `core.eol=lf` on every call, and
every `hardenGitRepo` copy in the tests pins the same two keys.

**Platform code is two files, not a build tag on a package.** The
verdict runner's process group and its kill moved to
`workspace_unix.go` and `workspace_windows.go`; the rest of the
runner is shared, and the Windows file says what it cannot do.

**Windows is a matrix leg, not a sentence.** The workflow runs the Go
suites on the three platforms with `fail-fast: false`; the doctor
names the postures each can run; the enforced self-hosted posture is
unavailable on Windows with the reason, since no server executes the
hook on a bare checkout.
## Phase 13 item 1 — racing mode (os-56bee171, plan #256)

**A racing claim is a fact on an `in_progress` subject, never a table
row.** The first cut widened `claim.taken`'s origin to `in_progress`
and `submission.made`'s to `review`; the table's self-validation
refused it at once — the in_progress exits are exactly the four
deliberate exits, and a claim from in_progress is not an exit. The
charter's invariant was right: racing changes who may hold, not what
a state may become. So the fold applies a second claim as a claim
fact and a racer's exit as a claim-scoped fact, the table is untouched,
and the widened judgment lives in the lifecycle rule beside contention,
gated by `seed/6` so a `seed/5` validator refuses by version.

**Two exits move the state, the rest move a claim.** The first
submission enters review (the other racers keep working there), and
the last racer's departure with no submission yet re-readies or blocks
the subject as the table says; every other exit closes its own claim.
After settlement every exit is claim-scoped, which is what lets a
settled-out racer leave with its packet and the reaper reap on a done
subject.

**The lockout is per submission.** A fail on one racer's submission
locks that racer's pass out and touches no other; the merge chain
cites the newest verdict on its own submission, so a racer's later
fail cannot shadow another's earlier pass.

**The reaper's third corroboration is the ledger.** A settled race
needs no observation stream to reap: nothing a settled-out racer does
can land but its exit, so the maintenance pass reaps each remaining
claim with a packet naming the settlement, reported as `settled`.

## Phase 13 item 4 — the request ingress, federation and cross-repo work (os-48df10a2, plan #257)

**`seed/7`, not the plan's `seed/6`.** The plan was written before
racing landed and named `seed/6` for the request verbs; racing took
`seed/6` first (13.1), and a version is a named list a validator
either registers or not, so the ingress is `seed/7` and the register
says why. Nothing else in the plan moved.

**A request is a fact beside the lifecycle, not a row.** Both verbs
change no state, so `transitions.json` gained nothing and the fold
keeps request facts the way it keeps the verdict and the escalation:
position, signer, subject, origin, kind, and the answer. The rule
holds the shapes and the citations; standing is the keyring's and
dispatch is the grant rule's, as everywhere.

**The subject is derived, never chosen.** `about` names the contract
the proposal concerns and the record's subject must agree with it, or
be `system` when it names none; a subject that resolves to nothing
refuses. That is what keeps hostile text off the notice's subject
field without a second oracle: the situation read carries a subject
only because admission already held it to a contract or to `system`.

**One obligation row per subject.** Obligation identity is
(subject, kind), so two pending requests on `system` are one
`request.pending` row carrying the oldest; the situation read lists
every request as its own notice. Splitting the identity for one kind
would have made the delta's removal names ambiguous for every other.

**`request answer` is a lane act, not a loop act.** The loop is seven
acts by spec and by test, and the dispatcher runs no worker loop; the
lane validator gained a closed set of lane acts held to the same
grant intersection, and a lane declaring only those owes no liveness
source. The dispatcher's manifest names it, the trajectory corpus was
re-recorded on purpose, and the frame's act check is unchanged.

**The report's version did not move.** The `requests` section is
present only when the prefix carries a request, and no chain before
`seed/7` can carry one, so every existing build is byte-identical
without a republish; the projections spec records the section under
version 15 with that reasoning.

**The federation read has no declaration in the loop.** `seed
federation report` opens each remote with the gitref client alone —
fetch, materialize, verify from the remote's own genesis with the
resolver that genesis bootstraps — rather than through the loop's
session opener, which would have applied this deployment's
declaration (its ledger ref under the forge-hosted posture, its
proposer) to a foreign ledger. No key crosses, and the command has no
key flag to cross with.

## Phase 13 item 2 — the remaining executor adapters (os-083112ac, plan #274)

**The budget posture is a per-adapter fact, and the safe default is a
risk limit.** `executor.Described` is optional so the public `Adapter`
interface is unchanged; an adapter that does not state its posture is
treated as a risk limit, never enforced by default — the honest
assumption when a substrate has not proven it can be stopped
synchronously. Local worktree and container are `enforced` (the
supervisor kills them); cloud session and remote worker are
`risk-limit` (a provider or a remote process may spend past the
reservation before the interrupt lands). `report.json` and `doctor`
report the posture per adapter; neither averages the two.

**A credential is an env-var name, never a value; the remote worker
pulls.** The `executors` block names the container runtime/image, the
cloud endpoint and a credential env-var NAME, and the enrolled workers;
the lint refuses a token-shaped credential, so a secret never reaches
the tree. The remote-worker adapter opens no connection — it puts the
packet in the artifact store and appends the pickup line the enrolled
service actor reads, keeping §II.9's no-inbound rule. The container
adapter runs credential-free in CI through `executor/fakeoci`.
## Phase 13 item 3 — the Forgejo forge adapter (os-ad610334, plan #275)

**The pull-request rule is unexpressible on Forgejo, so the whole
requirement is manual — not partially applied.** Forgejo's branch
protection has no conversation-thread-resolution gate and no
code-owner-review gate, and the reconciler reconciles by rule type
against a parameter-exact `differences()`. There is no way to apply the
approvals Forgejo *can* express while not-drifting on the resolution and
owner gates it cannot, so rather than half-apply the rule and read the
protection back as compliant on gates it does not enforce — a false
compliance the mutation drill forbids — the whole pull-request rule lands
in `State.Unexpressible` and `plan` reports it manual. Required approvals
are still set best-effort through the API; the operator sets the review
gates in Forgejo's settings and records that they did. `Desired` is
untouched: one decision table, two forges held to it.

**The `--forge` default stays `snapshot`, and the forge is declared, not
defaulted from the flag.** Six existing protections drills rely on
`--forge` defaulting to the credential-free `snapshot` arm, so that
default is retained. The forge a deployment runs is named in
`admission.forge` (default `github`, so every existing declaration keeps
its meaning) with `admission.api` (required under `forgejo`, which has no
public API); `seed doctor` reports it and `--forge github|forgejo`
selects the live arm, reading `--api`/the token env per forge.
## Phase 13 item 5 — the A2A-shaped cross-organization boundary (os-40ed0ca0, plan #258)

**The card's inputs live in the declaration.** The plan derives the
card from `teams` and `guardrails` and names an ingress; the
declaration had no ingress to name, so it gained a strict `boundary`
block (`accepts`, `ingress`), and the same block binds admission: a
kind the card does not accept the `boundary` rule refuses at
`request.filed`. One source of truth for what is offered, read by the
render and by the boundary.

**CI diffs content, not signatures.** The checked-in card is signed
by an operator key CI does not hold, so `seed boundary check`
re-renders the content from the declaration and compares canonical
bytes without the signature; the signature is verified only when a
public key is given. The fixture deployment's card is signed by a
throwaway key kept out of the tree — the gate is that the card says
what the declaration says.

**Canonical bytes without a JCS library.** The card's value domain is
strings and arrays, so a compact encoding with sorted members and no
HTML escaping is JCS-canonical for it; the event canonicalizer stays
the events' own. **Corrected by os-f11601e0** (see "The card's
canonical form is RFC 8785" below): the premise is false, Go escapes
U+2028 and U+2029 where JCS emits them literally, and `Card.check`
admits both in a name.

**The task view carries no contract id.** The plan's field list is
the request's position, its answer's, the state and the artifact
digests; a contract id would be a position into the target's
internals dressed as a name, so the view derives the state through
the answer's intent and never says which contract.

**Serve is a handler, tested as one.** The surface is an
`http.Handler` over a re-read clone, an artifact store and the card
bytes, so the drill mounts it on `httptest` and drives the lifecycle
through the same reads a stranger's `boundary tasks` makes; the
listening command wraps the handler and refuses an unsigned card
before it binds.

**Claiming stays online-only.** The drill's target lanes claim through
the library against the clone, as the offer drills do, rather than
loosening the CLI's online-only rule for a test.

## Phase 13 item 7 — tuple ranking as supervisor policy (os-c7554f18, plan #276)

**The bootstrap is the one place policy yields.** D2 says an unscoped
offer is the supervisor's explicit choice and never a fallback, and
`--strongest` refuses on an empty ranking. `eval.Due`'s offers cannot
refuse: a first eval on a fresh chain is how any configuration
qualifies, and a derivation that owed no offer would leave the ranking
empty forever. So `Due` leaves that one offer unscoped and notes
`ranking_empty` on it, the report saying in so many words that policy
had nothing to say. The verb's refusal and the derivation's note are
the same fact told to the two callers who can act on it.

**A re-test names its own configuration.** D2's "the top of the claim
ranking" would send a spot check of tuple T to whoever holds the
strongest tuple, whose run would declare its own configuration and
qualify that rather than re-testing T. The filing already names the
tuple under test, so its offer carries exactly that tuple; the ranking
scopes first evals only. The spec's table row says so.

**The ranking reads records, not the keyring's list.** The keyring's
`Qualifications` carry no chain position, and "latest fact" is a
position question (two facts in one hour's `ts` still have an order),
so the derivation reads the qualification verbs off the verified
prefix by position and uses the keyring for standing and the
admissible set alone. No accessor was added to the keyring.

**`strongest` sits on the planner section, absent rather than null.**
The by-kind split repeats the planner figures per roster kind; a
`strongest` on every copy would say the same thing several times or
nothing several times. The field is on `lanes.planner` with
`omitempty`, so a chain that never carried a scoped offer builds a
report without it, and the version bump (16) is what republishes.

**The doctor takes `--ledger` for this one section.** The doctor read
the declaration alone; naming the top tuple needs a chain. The flag
is optional and the section absent without it, so every existing
invocation and its output are unchanged.

**`envelope.md` takes one row the plan's scope did not name.** D2's
refusal needed a code; `ranking_empty` refines exit 4 `not_found`
(the policy's answer does not exist), and the refinement table in
`envelope.md` is where every such code is recorded, so the row went
there beside `trust_undeclared`. No new exit number.
## The conformance report (os-83bc3d84, plan #287)

**The table transcribes the records; it does not judge.** Every
status in `conformance.json` is what a phase exit record already
states for the row, with the record's own citations as evidence, and
Phase 13's rows stay `open` until that phase's record flips them. The
alternative, a table that derives statuses from tests by name, would
make a test's existence stand for a criterion's satisfaction, which
is the substitution the exit records exist to refuse.

**The charter is parsed, not copied.** The drill reads Part III out of
`SEED-NEXT.md` itself and holds the table to it row for row in both
directions, so the table cannot drift from the charter and the charter
cannot grow a row the table does not judge. The row text is kept
verbatim in the table anyway, so a reader of the JSON sees the
criterion without opening the charter.

**Complete means every row met.** The plan's first draft called Part
III complete when no row was `open`, which would have reported a
knowingly non-conformant tree complete while rows stayed `partial` or
`routed` (review finding on the plan PR; the charter admits a
conformance claim only when every criterion holds). The doctor now
lists every row not met, by status, and reports `complete` only when
none remains. An enforced-only row cannot be met at the cooperative
posture, so the doctor sets those rows aside there as the
documentation the charter asks a cooperative deployment to keep; and
because the charter defines conformance at the declared posture, a
cooperative deployment whose applicable rows all hold is complete at
its posture (review finding on the task PR; the plan's D4 had read
that as never complete, and the charter is the authority). The build
plan's exit line reads the report at the enforced posture, where
every row applies.

**A marker inside a row is a clause, not the row.** III.A row 4 and
III.L row 5 carry the enforced-only marker on one clause among
several that apply everywhere (review finding on the plan PR); a
posture model that set the whole row aside at the cooperative posture
would suppress requirements that hold there. Such rows are `mixed`:
judged at every posture, and named at the cooperative one so a reader
knows which clause is not exercised.

**III.R is routed, not abandoned.** Its seven rows are outcomes
promotion measures (build plan §5), and a table that left them `open`
forever would make the Phase 13 exit line unachievable (review
finding on the plan PR). Each row is `routed` to the promotion
evidence that measures it, the note naming the measure and its owner:
the report's lanes, flywheel and knowledge sections, the tiers'
independence levels and the calibration agreement, the shadow run's
ledger audit, the first external team's outcome. The rows flip to
`met` when the promotion evidence card records the measurement, which
is why the build plan's critical path puts the shadow run and the two
cutovers before Phase 13's exit.


## The seventh race shape (os-5063e8ba)

**A bad_prev at or beyond the append is re-linked, below it is
surfaced.** The storm's refusal named the position one beyond the
client's own append: the pushed tree held a record the client's store
had not reconciled, so the client's record sat a position later than
it expected. The shape is therefore "at or beyond", not "at"; a
`bad_prev` below the append is the fetched prefix failing at the
hook, which the client's own verification should have caught, and
stays the refusal it is.

**The evidence is kept before the retry, both halves of it.** Under
`refused/<commit>/` the client keeps the rejected commit's own tree,
materialized from the commit object (the bytes the hook judged),
beside the work directory the attempt built it from and the hook's
message (review finding on the plan: if the record the hook found
came from the client's persistent index, only the committed tree
carries it, and the difference between the halves is the diagnosis).
The state dir is where a writer's own history already lives; a temp
dir would be gone with the attempt. `--keep` on both binaries keeps
the storm around it.

**The midnight drill does not reproduce the tree.** Two writers
racing across an injected segment split land every append and
re-link zero times, three runs of three. The drill stays as the
regression for the one condition the failing storm had; the cause
is open, and the seventh shape means the next occurrence costs a
re-link and leaves a directory to read rather than a lost append.

## seed ledger audit (os-7599c27d)

**The audit reads what the replay verified, not a second scan.** The
records handed to the bars are collected by `ledger.WithObserver`
during `VerifyFromGenesis` (review finding on the plan): a writer
appending between the replay and a rescan would give the audit
records the envelope's count and tip do not cover, and a count that
disagrees with the report refuses `unavailable` rather than auditing
a chain the position does not name.

**The refusal carries the lists in its message.** The plan asked for
the audit result beside the error so a reader needs no second call;
the envelope's `result` and `error` are exclusive by its shape, so
the message names each violated bar with the records it names
(`silent_abandonments [c-1]`), the same lists a clean audit renders as
fields. No second field, no second envelope.

**The lost-updates bar is unreachable through the verb, by design.**
An empty chain has no genesis and fails verification first; D1 puts
verification first so a bar is never read over an unverified chain,
and the drill asserts `chain_invalid` there rather than inventing an
audit of nothing.

**The drills append past admission with the root key.** The dev tool
self-validates (the cooperative posture's rule), so a planted
violation cannot go through it; the drills sign with the governance
root and append through the library, which is the raw-push posture
the bars exist to catch, and the chain still verifies because the
root signed.

**The packet's clause waits for the packet.** `docs/promotion.md`
is on #294, not on `main`, so the one clause the plan names lands in
this branch's merge-forward once the packet merges, never by stacking
a task PR on another.

## The contention benchmark at target scale (os-a00d3f34)

**One gate at two sizes, not a second tool.** `perfgate.Budgets`
already carries the history size and the writer count beside the
ceilings, `Load` already refuses a ceiling without provenance, and the
storm already asserts no lost or doubled update at any count; the
scale run is a second budget file read by the same `cmd/perfgate`,
and `internal/perfgate`'s code changes by nothing. A second measurer
would have been a second definition of the same metric.

**200 writers, the same history, one actor each.** Hundreds means
the plural, and 200 is the smallest such count; the history stays at
the per-PR profile's 40 contracts so the admission, replay and
rebuild ceilings are copied rather than re-derived and the two
readings are about the same chain. A review finding on the plan
caught the storm signing every writer's append with the governance
root's key, one actor issuing 200 writes; the charter's row counts
actors and an actor is a keypair, so the history now enrolls one
agent key per writer (`dispatch`, the grant `intent.filed` accepts),
the storm signs with it, and the measurer refuses a landed chain
whose storm records carry fewer distinct actors than writers. The
per-PR storm's 24 writers are 24 actors by the same change.

**The schedule is read-only and its red is the signal.** The workflow
declares `contents: read` alone, uploads the reading whether the gate
passed or not, and pages nobody; `TestTreeWorkflowsHaveNoScheduledWriters`
holds every scheduled workflow in the tree but v1's maintenance lane
(whose write is the state ref's anchor) to that permission, so the
lint the doctor already runs on a declaration now runs on the tree
under `make check`.

**The conformance row stays partial; the run is cited for what it
shows.** The plan quoted the row as "without lost updates"; the
charter's row reads "without unrelated writes racing each other's
admissions pathologically" (review finding on the task PR), and a
single-ref storm of two hundred unrelated appends is that pathology
held to a ceiling, not its absence. The charter outranks the plan, so
this card makes the run exist and states its claim against the row's
own words: hundreds of enrolled actors at the target scale with no
lost or doubled update, tracked in CI. The clause the row states is
sharded intake's (III.B row 6, os-7953612b), and the Phase 13 exit
record cites the first green run without flipping C.4 on it.

**The benchmark found what it exists to find.** Of three 200-writer
storms on the implementing host signed with one key, two landed every
writer and one refused one writer at the hook with `bad_prev` (a
pushed chain whose new record cited a stale tip), which the loop
classified as policy and did not retry; the run crossed midnight UTC
and was the only one whose chain spanned two segments. The failure is
carded as a defect (os-5063e8ba) with the evidence and the
reproduction (`seed perf run --writers 200 --history 40`), not
absorbed into a ceiling, because a lost append is the thing the row
forbids. The ceilings come from the two later runs with one enrolled
actor per writer (2065826 and 2071708 ms, 66.8 and 67.3 attempts per
append, 200 distinct actors in each landed chain); the one-key runs
(970948 and 955154 ms) are superseded, recorded in the provenance for
what they measured.

## The promotion evidence packet (os-98ce6f8a)

**The packet is a document with a parser, not a projection.** The
promotion gate reads prose the operator can amend; what it needs from
the machinery is the assurance that the prose cites what the tree
holds. `internal/promotion` parses the packet strictly (the seven
criteria in order, one status each from a closed vocabulary, evidence
rows in one shape, a question on every reserved decision, a missing
sentence on every criterion that is not met, seven ledger rows) and
`Check` holds every cited drill to its cited file under `make check`.
No projection, no verb, no admission change.

**Criterion 5 is met by the packet itself.** The build plan asks for
the cutover and its rollback written down in three clauses; the packet
writes them, and its own drill holds the section to that shape, so the
evidence row for the criterion cites the packet's drill rather than a
claim about a document elsewhere.

**Criterion 6 cites what has merged.** The doctor's conformance
section is os-83bc3d84's (#289); while it was in review the packet
said `partial` and cited the exit records alone, and it moved to
`met` citing #289's drills the day the report merged, never before.

**The shadow run runs at the posture the criteria name; the server
is the question.** Build plan §5 evaluates every criterion at the
enforced self-hosted posture and reserves the two cutovers, not the
shadow run (review finding on the plan: a packet offering the other
postures as options could leave criterion 4 without valid evidence).
So the protocol runs under enforced self-hosted admission, a POSIX
git server executing the hook and hosting the ledger ref alone, and
the one reserved thing is that server, which this repository's forge
cannot be. Forge-hosted (a deployment and a credential the autonomy
contract reserves) and cooperative (forfeiting the invariant the run
must demonstrate) are stated for what they are: a shadow run under
either produces no criterion-4 evidence and would be the supervised
deviation the build plan says must be accepted as such.

**The III.R ledger records measures, not readings.** Every row is
`not measured`: no measurement exists before the shadow run, and a
packet that reported one would be inventing it. Follow-up cards revise
the rows and flip the conformance table with the packet as evidence.

## A permission is met by abstention (os-9ef9ab34)

**Complete admits no unclaimed rows.** `conformance.Assess` reports
`complete: true` only when every applicable row is `met`, with no
exemption for a permission, and `plans/os-d63c7441.md` D3 closes
Phase 13 only when the doctor says complete. III.B row 6 sat `open`
with the note "not claimed: MAY", so complete was unreachable
forever: no exit record would have flipped it either, since the row
is Phase 12's and the Phase 13 record's flips are scoped to Phase
13's rows. The deadlock was invisible because each half looked
reasonable alone.

**The charter's clause is conditional.** "Admission may shard
proposal intake without changing semantics" binds a system that
shards; this one does not, so it changes no semantics by
construction, and the row's second half, "ordering remains solely the
admitted chain", is III.A row 3's claim, met since Phase 1. The row
is therefore met on what holds today: a single intake path and
ordering derived from admitted ancestry. A system that later shards
re-earns the row by showing semantics unchanged.

**The guard is the phrase, not the idea.** The drill refuses any row
left outstanding whose note excuses the criterion as unclaimed, which
is the exact shape that produced the deadlock. It deliberately does
not sweep for "optional": III.H row 7 says the meter is
caller-optional while naming where the rest lives, and that is a
routed row doing its job, not a row parked forever.

**Abstention is a property of the tree, so the drill reads the tree.**
Reading the committed row's status and note proves only that the
table still says what it said: the tree could start sharding and the
doctor would keep reporting complete without the semantics-equivalence
evidence the note demands. So a second drill reads the admission
surface and holds it to what the evidence claims. Sharded intake
cannot be built without a per-shard admission context or a second rule
set, since a shard applying the same rules over the same whole prefix
is not a shard, and both are visible in the syntax: the drill names
the exported functions returning `*Context` (exactly `ContextAt` and
`ContextOver`, both whole-chain) and returning `[]Rule` (exactly
`Default`), and refuses any `admit.Context*` the boundary reaches for
beyond those. Planting either shape fails all three arms. The row's
evidence cites the drill, so a reader arriving at the table finds what
defends it.

## The audit's vocabulary (os-b86dab4c)

**A bar counts what the protocol emits, or it never fires.** The
unreserved-spend bar counted `budget.reserved`; the protocol emits
`budget.reserve`. Every drill agreed with the typo because the bar's
own fixture repeated it, and nothing else exercised the bar: the
simulation's deployment never files a reservation, and no drill asserts
its audit is clean. So the defect was invisible until `seed ledger
audit` (os-7599c27d) pointed the bar at a real chain, where a covered
run reads as unreserved spend.

**The fix is to stop restating the protocol.** The bars name the
constants `transition` publishes, so a rename fails to compile rather
than mis-counting; the deliberate exits come from `transition.IsExit`
rather than the audit's third copy of the same four verbs; and the set
of verbs the bars read is held to `admit.CatalogVerbs()` by a drill,
so a verb the boundary never drafts fails the suite whatever put it
there.

**The authority is the catalog, not the lifecycle table.** The plan
first named `transition.Table.Verbs()`. That list is the lifecycle
subset: it carries `offer.published` and `claim.taken` but neither
`budget.reserve` nor `run.started`, so the guard written against it
failed on two verbs the protocol does define. The amendment (#304)
records the correction rather than quietly widening the drill.

## The unreserved-spend bar judges the run's citation (os-88df7ab2)

**A start is fenced to the reservation it named, not to any open
one.** The bar kept a boolean set by any `budget.reserve` and cleared
only by the next claim, so a settled or released reservation went on
covering later spend, and a reservation admission would have refused
counted like an admitted one. A start cites one reservation
(`RunStartFact.Reservation`) and `admit.RunStartValid` judges that
citation at the start's own position, so the bar asks that predicate
rather than keeping state of its own. Asking "was some reservation
open" is weaker than the protocol: a start citing a closed or absent
reservation passes it whenever an unrelated reservation is open, which
is the fencing the bar exists to check (review finding on plan #309).

**A start the fold could not place is unfenced, not unjudged.**
`transition` records a `RunStartFact` only where the payload named a
fence and a reservation it could read. Counting `run.started` records
against those facts is what keeps a malformed raw start visible;
without it the bar would close only half the hole, since the threat
model is a chain pushed past the boundary.

**The fixtures had to become admission-grade, and that is the finding
under the finding.** Every synthetic `{}` chain in the drills read as
unreserved spend under the corrected rule, correctly: no admission
would have taken them. The covered arm now audits a chain
`internal/history.Generate` wrote, which carries reservations, runs
and settles under enrolled lane keys. A bar that judges admission can
only be exercised by chains admission would take.

**The cost is a number, not a claim.** Each `RunStartValid` replays
the keyring and the table over the start's prefix and derives a budget
view there, so the audit is not linear in the chain. The drill audits
a 40-contract chain of the shadow window's shape and records the
reading: 410 records in about 130 ms, far inside the ten-second
ceiling the plan set, so no memoization was added.

## The guardrail bar counts what admission guards (os-aaec6a3c)

**A bar reports what the boundary refuses.** The bar named any
`claim.taken` that did not follow an `offer.published`. Admission holds
no such rule: its claim arms are authoring isolation and the lifecycle
transition, and `LiveOffers` has only two callers, both scheduling
reads. So a chain the boundary took reported guardrail breaches, which
is how `internal/history`'s admission-grade fixture tripped the bar on
every subject while os-88df7ab2 was being implemented.

**The charter's §II.9 binds the supervisor, not the claim.** "Offers
that get claimed or expire" and "the claim settles at admission"
describe the scheduling model, and III.H's wakeless poll-only proof
turns on workers claiming what they find. The other reading, that the
offer is a precondition and admission has a gap, was rejected: it
would invalidate the perf history, the migration fixture and every
drill that claims without offering, and it is written into the plan so
a reviewer can take it in one place.

**Correcting a bar is not emptying it.** `GuardrailBreaches` had
exactly one source, so dropping the offer rule alone would have left a
bar that can never fire and a III.R row 5 guarantee no drill can
break. Admission does guard the claim path, and the guard is
chain-visible: it refuses a claim whose actor sealed that subject's
checks, and the fold carries `Sealed.Signer` and `Sealed.Pos`. The bar
counts that instead, so it keeps a violation a drill can plant.

**The clauses the bar cannot carry, said once instead of implied.**
`plans/os-16e55c11.md` D5 worded the bar as "no refusal followed by a
blind retry; every claim within its ceiling once item 4 lands", and
neither clause was ever the offer rule, so removing that rule loses no
coverage: there was none. Neither is reachable here. The ceiling is
admission policy, not chain validity: the rule reads
`Context.Declaration` from `seed.json`, so a claim above the ceiling
folds as filed and the chain verifies byte for byte, while
`simulate.Audit` takes records alone, which is III.R row 5's own
contract that the ledger justifies everything. Two byte-identical
chains would be compliant or not depending on a file the ledger does
not carry. The blind-retry clause is further out of reach: a refused
append never lands, so a refusal followed by a retry leaves only the
retry. Both are carded as os-b5051f2e, which owns the choice between
threading the declaration through the verb and correcting the row's
wording. Writing this down is the point: the offer rule most likely
grew from a bar whose contract named something else and whose
something else was unreachable.

## The machine surface is governed by the boundary it dispatches to (os-8ecef90f)

**A drill, not an implementation.** III.L row 4 asks that per-verb
policy govern the machine-protocol surface with attributable
approvals. `serve` resolves a method through the registry the CLI
dispatches and runs the CLI's own run function, so there is no second
admission path to govern: the policy per verb is admission's (the
grant table, the declaration's ceiling and routing rules), and an
approval landed there is a chain record signed by its actor. What was
missing was a drill saying so on that surface, since #273's tests are
parity assertions. Two drills were added and nothing else moved.

**Refusals are held to the boundary's code, and their twins land.** A
drill that only asserted a refusal could pass on a broken surface that
refuses everything. Each refusal fired through `serve` (`out_of_grant`
on `decision.record` from a claim-granted key, `routing_unknown` on a
filing to an undeclared squad, `tier_above_ceiling` on an agent's claim
above the declared ceiling) is compared with the CLI's code for the
same argv, and the admitted twin (the operator's answer, the declared
squad, a human-kind key's claim) lands through the same surface.

**Attribution is the signature.** The protocol framing authenticates
nobody (`protocol.md`, "The machine surface"), so the only attribution
an approval through it can carry is the chain's: `decision.recorded`
and `plan.approved` are read back through `serve`'s own `ledger.show`
at the position the write reported, with `actor` the signing key's
fingerprint, and the chain verifies afterward. A fresh reader
attributes the approval from the chain alone, which is what the row's
word buys.

**The row moves to partial, not met (review on #320).** Charter II.14
defines per-verb policy as allow, deny and require-approval, with
approvals as request events resolved attributably in an operator
inbox. The drills here cover allow and deny; the tree has no
require-approval mode at all, so flipping the row on them would have
recorded a mode nobody built. The row's note names the missing third
and os-5781a026, the card that builds it and flips the row on all
three together. The precedent for moving a row outside an exit record
is os-9ef9ab34 (#308).
## The audit's ceiling arm reads the declaration admission read (os-b5051f2e)

**A bar over the chain cannot see a file beside it, so the audit takes
the file.** The ceiling is admission policy, not chain validity: a
claim above it folds as filed and the chain verifies byte for byte.
Rather than leave III.R row 5's "every claim within its ceiling"
unmeasurable, `simulate.AuditUnder(records, declaration)` judges the
ceiling arm under a declaration the caller gives, mirroring admission's
rule arm for arm (the kind from the keyring replayed to the claim's
position, the tier and squad from the fold, the ceiling from the
declaration, fail-closed on a ceiling outside the vocabulary, silence
for a human key, an undeclared squad or no declaration).
`Audit(records)` is `AuditUnder(records, nil)`, so every existing
reading, the simulation's included, is unchanged.

**The verb reads the declaration by the one lookup that exists.**
`seed ledger audit --config` goes through `deploymentDeclaration`, the
remote verbs' `--config`, `$SEED_CONFIG`, `./seed.json` rule, so an
audit and the admission it judges read one file by one rule; a
reading names the declaration it was judged under, or `null`, so a
policy the lookup found implicitly is explicit in the envelope; a
declaration that exists and does not parse refuses `posture_invalid`
before any bar.

**The blind-retry clause is unmet, and says so.** A refused append
never lands, so no bar over the chain can see a refusal or its retry;
and the refusal journal cannot tell a blind retry from a corrected one
as it stands (an entry keeps no digest of the attempted payload), so
routing the clause there would have marked it covered by a surface
that cannot measure it (review on #322). `simulation.md` names the
clause unmet and cites os-a9e715dc, which owns the measurement. The
charter's row is untouched; the bar's description shrank rather than
grew.

**One policy epoch per reading, and the simulation reads its own.**
The ledger carries no declaration digest, so the arm judges every
claim under the one declaration given and names it; a deployment
whose guardrails changed audits each epoch with the file it ran
under. The seven-day simulation, the audit that is supposed to show
every claim within its ceiling, now declares a ceiling of its own
(the core squad's agents at `trivial`), pushes every claim under it
from the cooperative client and audits under the same file, reporting
`declared: true`, so the arm is live in the conformance run rather
than silent there (review on #322). The enforced hook reads no
declaration today, so the flag states what the audit read, not what
the boundary refused; the hook's reading is card os-0f924157's (review
on #323).

**A chain whose roster does not replay is silence, not a guess.** The
arm needs a kind to judge; where the keyring cannot be replayed it
reports nothing, which is admission's `Keyring == nil` posture, and
verification refuses such a chain before the audit reads it anyway.
## Erasure is a verb, so the absence has an author (os-db5cd353)

**The charter's word decides between the two courses.** The card
offered adding an erasure verb or revising III.A row 7 to say
"surfaced" instead of "attributable". The autonomy contract prefers
the charter's normative text and the charter wrote "attributable"
deliberately: a lint finding that a ciphertext is gone names neither
who erased it nor when, and cannot be verified by a fresh reader from
the chain. So the verb was added.

**Additive catalog growth, no version bump.** `protocol.md` says of
`offer.*`, `curation.*` and `workflow.*` that older validators refuse
an unknown verb safely, so the version does not bump; `artifact.erased`
is the same kind of fact, active from `seed/1`, judged by its own
admission rule and inert below it.

**The reference is the contract's own.** On a contract subject the
digest must be one that subject's fold references (its sealed
commitment or a verdict's receipt): the fold indexes those by digest,
so admission can hold the citation. Every other artifact the chain
cites lives in a payload no fold indexes, and for those the operator
erases on `system`, its attestation being the reference. A stricter
rule would need a digest index over every payload, which is a
projection's job and not admission's.

**Operator only, once, digest-wide.** An erasure obligation is a
governance act a human answers for, the `decision.recorded` posture,
and no lane's loop erases. The store holds one object per digest, so
an artifact two contracts reference is erased for both by one act
(review on #324): the tombstone is looked up by digest wherever it was
recorded, the audit attributes a shared commitment's absence to the
one record, and an artifact erased once is not recorded again. A run
that dies between the append and the removal leaves a standing record
with the bytes present, and the next run finishes the removal without
a second record; the drill plants exactly that state. The affordance
probe offers the first reference no tombstone covers, so the verb is
drafted while something remains erasable and never for an erased
digest.

**An honored erasure is not a finding.** The seal audit lists an erased
ciphertext with its attribution and stays clean; only a deletion with
no record is `seal_evidence_missing`. That is the row's whole point:
the absence is honorable exactly when the chain says who and why.

**Record first, then remove.** `seed artifact erase` appends the record
and then empties the store, because a record with the bytes still
present is a promise the next run keeps, while bytes gone with no
record is the silence the row forbids.

**A tombstone attributes only what passed the boundary, and a removal
that fails is a refusal (review on #325).** The raw `ledger append`
seam lets any standing key land a well-shaped `artifact.erased`, and
the fold keeps it, as it keeps every fact. Three consumers took the
fold's word for it: the seal audit honored the tombstone by digest and
reported clean, the once rule refused the operator's own record behind
it, and the verb's resume path finished a removal under whichever
record stood, for whichever key asked. The tree's answer to the same
shape elsewhere (`RunStartValid`, `sealAuthorized`) is to replay the
keyring at the record's own position, so `admit.ErasureValid` does
that, `admit.Erasure` is the digest-wide lookup narrowed to the records
that passed the boundary, and every consumer reads through it. The
resume path also holds the caller to the grant a fresh append holds,
since finishing a removal is the same governance act as recording it.
And a removal the store refuses after the record landed is
`erasure_incomplete` (exit 5) naming the position, never a success
with a note: the shared commit renders the envelope it lands, so the
verb renders through a buffer and replaces a success whose removal
failed, which keeps `loop.go` untouched and the journal's admitted
outcome true.
## The packet names what stands between it and the question (os-4fde2bdf)

**A gate document says what the gate waits on.** The packet's section
4 was honest about the substitution and the rest of the packet still
spoke in the shadow window's tense, so a reader could not tell what
remained. The gate paragraph now says it: no Seed deployment for this
repository exists, standing one up is the operator's because the
autonomy contract reserves credentials and infrastructure to a human,
and nothing agent-side remains once the three cards in review merge.

**The question is asked at a position the operator records.** Under
the substitution no window closes, so the Self-hosting question's
anchor moved from "the position the shadow window closed" to "the
position the operator records as the start", and its preconditions
name the substitution rather than pretend a seventh `met`. Both
cutovers stay questions the parser models; neither is answered.

**The proposed declaration is held to the tree.** A block the
operator copies at the cutover should be one CI has linted, so
`TestPacketDeclarationLints` runs `seed preseed check` on the packet's
block and fails on a planted unknown tier. The block's home moved from
a `next/deploy/` file to the root `seed.json`, which is where
`postures.md` says a deployment declares and where the doctor, the
remote verbs and the hook read.

**The frontier is re-derived from the cards.** Five merged cards read
`done` with their PRs; the three in review are named with the rows
they flip and the doctor's count once they merge; the operator's
decision is stated once, in order: the deployment, then the answer.

**The review findings on the task PR, three, each verified before it
was fixed.** The Self-hosting preconditions read as if the cutover
could follow the answer with criterion 4 `partial`; build plan §5
counts the cutover on seven criteria `met` and puts the shadow run
before it on the critical path, and a packet cannot waive a
prerequisite its governing plan binds. The question is still put (D2),
and the packet now says in the gate paragraph, the cutover section and
the preconditions that the flip waits on criterion 4 `met`: by the
shadow run on the deployment, or by an amendment of §5 the operator
records as the deviation it is, neither of them the packet's to make.
The procedure had `seed init --preseed` write the deployment's ledger
and `seed import --from-open-seed` write it again; the import is the
genesis transform and refuses a ledger holding any record (exit 3
`ledger_not_empty`, reproduced against this repository's real export),
so the order is now the import first into an empty ledger, then the
declaration over the imported chain (`init --preseed` appends exactly
`seed/6` and `seed/7` over the import's `seed/5` and nothing on a
second run, the check reads green), then the lane keys, then the push
to the hook; `TestPacketProcedureReachesTheFlip` follows that order
through the CLI and proves the reverse refuses by name. The block's
`governance.root` read `declared-at-init`, which `applyPreseed`
refuses `preseed_drift` before genesis because no fingerprint equals
it, and the lint alone never reaches the comparison; the value is now
the spec's own placeholder, named as the one required substitution,
and `TestPacketDeclarationInitializesUnderTheRootKey` initializes the
substituted block under a real key and holds the unsubstituted one to
the refusal.
## The release is the operator's act; the workflow is the agent's (os-2e46aa2f)

**Dispatch-only.** Build plan §5 reserves publishing to a human, and
the CI-identity lint holds scheduled writers to read-only, so the Seed
release workflow triggers on `workflow_dispatch` alone with the version
as its one input. Nothing in the tree can cut a release on its own.

**The tag is minted at HEAD, in its own namespace.** The engine
repository's discipline: the workflow tags the commit it builds, so
the tag and the released commit cannot disagree and no contributor
needs tag-push rights. Seed's tags are `seed/v<version>`, apart from
the template's `v*` releases (immutable under the tag rule) and the
`seed-anchor/*` state anchors, so a Seed tag can never be read as a
template release.

**A plain matrix build, no goreleaser.** goreleaser derives the version
from an unprefixed semver tag and its monorepo prefix is a paid
feature; the namespace above is cheaper to keep in twenty lines of
bash than to work around. `internal/version.Version` is a var so the
`-ldflags -X` stamp lands; from source it stays the pre-release
default, and a drill pins both.

**Every action hash-pinned, and the namespace protected before it is
used (review on #328).** The job holds OIDC, attestation and write
privileges, so nothing executable in it resolves through a mutable
tag: `actions/checkout` and `actions/attest-build-provenance` are
pinned to commit SHAs with the tag in a trailing comment, and the
drill refuses any `uses:` that is not a local path or a 40-hex SHA.
The hook already refuses an update or deletion of any tag, but the
forge reconciler's `seed-release-tags` ruleset named `refs/tags/v*`
alone, so under the forge-hosted posture a released `seed/v*` tag
could have been retargeted; the ruleset now names both namespaces,
the Forgejo adapter keeps one tag protection per pattern, and the
release drill asserts the namespace it tags is one the desired state
protects.

**Attested, and verifiable from the handbook.** `checksums.txt` is
attested with `actions/attest-build-provenance`, the attestation the
engine ships, and the handbook says how an adopter verifies an archive
before running it, which is the half of III.R row 7 that a README can
carry before any release exists.

**The tag follows the build, the draft follows the attestation, and the
privileges stay on the default branch (review on #329).** Four findings
on the task PR, each a refinement inside the decisions above. The tag is
pushed only once the six archives and `checksums.txt` exist, so a failed
build strands no tag; a re-run for the same version on the same commit
resumes the cut (the tag is kept, a draft left behind is replaced),
while a tag at another commit or a release already published refuses,
which is the "cut once" the second decision meant. The release is
created as a draft, attested, and published last, so a failed
attestation leaves a draft nobody can download rather than a public
release whose notes promise a provenance it lacks. The version is
validated against semver.org's grammar as a POSIX extended regular
expression, no leading zeroes and nothing trailing, in place of a shell
glob that let `1.2.3foo` and `1.2.3/foo` through; the drill reads the
expression out of the workflow and drives it. And because any branch's
edited copy of a dispatch-only workflow can be dispatched with the copy's
own `contents: write` and OIDC grants, the job runs in the `seed-release`
environment and only from the default branch: the environment's
deployment branch policy is the operator's precondition, set in the
forge's settings before the first dispatch and named in the handbook
(the reconciler models rulesets, not environments, so it is not desired
state), and the ref guard restates it in the file as defense in depth.
The same review found the Forgejo adapter reading one whitelist for both
tag protections; it now compares each pattern's whitelist, so a weakened
`seed/v*` protection beside a compliant `v*` is drift and Apply repairs
it.
## Require-approval per verb (os-5781a026)

**The policy is declared by verb, kind and floor, and read as policy.**
`guardrails.approvals` follows the ceiling's precedent to the letter:
`Context.Declaration` supplies it, a nil declaration makes the rule a
no-op, a raw-pushed governed act folds as filed and every chain
verifies byte for byte. The alternative, a chain-validity rule that
refuses a governed act at verification, was rejected for the reason
`tiers.md` records: two byte-identical chains would be compliant or not
depending on a file the ledger does not carry.

**Three verbs, not a field on the escalation.** Catalog growth is
additive (older validators refuse an unknown verb safely), while a new
optional field on `escalation.raised`'s strict payload would bump the
protocol version. The approval is its own family, and the escalation
channel is byte-identical to `main`.

**A grant is spent by one act.** The first record on the grant's
subject whose verb and actor it names consumes it at that record's
position, in the fold, so a second act needs a second request and no
grant lingers as a standing permission. The offer's consumption by the
claim that takes it is the precedent; the fold's consumption reads
activated records only, so a grandfathered act spends nothing.

**The entry selects by actor and by kind (review on #330).** An entry
reaches the fingerprints `actors` names and the keys whose roster kind
`kinds` names, and every non-human kind when it names neither, so one
agent can be allowed while another of the same kind needs an approval:
the charter's "by actor" is the individual keypair. Allow and deny by
actor already exist at the grant table, so the entry is the
require-approval mode's selector and carries no mode field.

**A governance root's unasserted kind is reached by no kind selector.**
The rule reads the roster's kind; a key enrolled by genesis carries
none, and it is the operator every approval is answered by, so a kind
selector never reaches it; its fingerprint can, if a deployment says
so.

**An open grant is re-judged before it is trusted (review on #330).**
The tolerant fold records any well-shaped raw push, so a grant signed
by a key without operator standing would otherwise open a governed act
through the fold: the laundering the tree's countermeasure exists for
(`memory/LEARNINGS.md`; the `RunStartValid` posture). `ApprovalValid`
re-parses the request and the answer at their positions and replays
the keyring to the answer's position, holding the answerer to active
operator standing and the named actor to enrollment; a grant that fails
is a fact that authorizes nothing, and the affordances, running the
same rule, do not list the act. The fold stays keyring-agnostic, so a
raw non-operator answer still marks the request answered in the fold;
under enforcement it never lands, and under the cooperative posture
the chain is advisory against a hostile credential by the charter's
own words.

**The transport's non-ledger methods stay ungoverned (review on
#330).** `serve` authenticates nobody, and a method that appends no
record (`docs.generate`, `protections.apply`, `boundary.fetch`) runs
with the caller's own local credentials outside the trust boundary and
carries no actor, so a policy at method dispatch would govern nobody
attributably and could not be answered in an operator inbox. The policy
governs the verbs, the acts a key signs, which is what "by actor" can
mean; D1 validating against the affordance catalog is that reading made
explicit.

**A floor outside the vocabulary fails closed** (the ceiling's
posture), and a birth's floor reads the filing's tier, since the
contract the act creates has no state yet.

**One inbox row per subject, not per request (D5 refined).** The plan
said one `approval.pending` row per open request. The projection's
identity is (subject, kind): the situation read's delta names
removals by that pair and keys its standing set by it, so two rows of
one kind on one subject would misreport in the delta. The row is one
per subject carrying the oldest open request's position and timestamp,
exactly `request.pending`'s shape, and the next request surfaces when
the oldest is answered. Recorded here rather than silently: a reviewer
holding the task PR to D5 should find the reason in one place.

**The machine-surface drill is two halves.** Claiming is online-only
on the local path, so the governed act (a `claim.taken`) is drilled
against a bare remote, skipped on Windows with the platform spec's
reason; the request, the inbox, the attributable answers, the
answered-once rule at the derivation and the shapes at the door are
drilled on a local ledger everywhere. The boundary's own answered-once
refusal (`approval_refused`) is the remote half's, because the raw
seam (`ledger append --ledger`) runs no admission by design.
## The refusal journal tells a blind retry from a corrected one (os-a9e715dc)

**The digest is the act's, not the record's.** A record's hash covers
`ts` and `prev`, which every retry changes by construction, so it can
never match across one. The journal digests
`{actor, verb, subject, payload}` in canonical form and nothing else:
two attempts of one act by one key digest alike wherever the tip
stood, and any change to what the actor asked for digests apart. The
version is excluded with the coordinates, since the same act re-signed
after an upgrade is the same act.

**A journal from before the field still loads.** The journal is a
declared input and loads are strict, but a field added later cannot
retroactively make old journals garbage; a missing digest loads and is
counted as `undigested`, a present one that is no sha256 refuses
naming the line, the strictness every other field has.

**The count reads the established definition (review on #332).**
`modes.md` already defines the blind retry the loop drills assert
against: the same act refusing with the same code on consecutive
iterations from a position that did not advance, while an admitting
next attempt is the first convergence arm. The first draft counted any
same-digest next attempt, which would have labelled a valid
convergence (the optimistic loop re-sending the same act after a
contention refusal and landing it on the moved tip) as blind. The
report reads the definition from the journal: the same actor's next
attempt on the same subject, same digest, refused with the same code,
stamped at the same position; a later same-digest attempt after a
different act in between is a new decision, and each refusal counts
at most once. The split by code stays, so a contention spin reads
apart from a `fenced_out` act re-sent unchanged.

**The report version is "18" (review on #332).** The plan's first
draft read the register at "16" and named "17", which the adapters
section already holds; rebuilding a digest-free journal under an
unchanged version would have produced different bytes under the same
build identity.

**A derivation refusal journals without a digest rather than not at
all.** Two loop verbs refuse before a payload exists (a budget close
that cannot find its reservation, a run start that cannot derive its
tuple); their lines keep the rate's population whole and are counted
undigested.

**The audit keeps reading records alone.** III.R row 5's contract is
that the ledger justifies everything, so `simulate.Audit` does not
read the journal; the bar's description names the report's count as
the clause's evidence, and says the accelerated simulation cannot show
it because its deployment appends through the remote posture, which
keeps no journal by declaration.

## Knowledge search: an advisory read, reimplemented (os-405d3b20)

The claim-time delivery is an exact predicate match, and that is
right: a lesson a worker is shown must be auditable to the fold. It
leaves the other question unanswered, the one a worker has mid-task:
has anyone hit this before, what did the decision log say. The
surfaces for that were the whole store, the dead ends by contract and
`docs/decisions.md` end to end. The retrieval idea in
okf-agent-memory (github.com/okf-memory/okf-agent-memory) answers it
without a vendor: local lexical BM25 over a git-native markdown store.

- **The idea is adopted; the format is rejected.** OKF's `generated`
  and `verified` are labels a file gives itself, its
  search-before-write a convention, its `stale_after` an unenforced
  field. The pipeline has four stages with distinct storage and a
  gate between each, drilled against poisoning, and a lesson reaches
  the store by PR. Adopting the format would replace an enforced
  pipeline with a convention, the posture Part III refuses. Recorded
  as a standing rejection in `curation.md` "Search" (III.Q row 4).
- **Reimplemented, not imported.** The scorer is two hundred lines;
  the corpus derivation is the part that matters and no external index
  knows how to do it: the surfacing set at an instant, verified
  against the repository and read at the anchor, the standing dead
  ends from the fold. An import would have added a dependency to the
  trust surface for the part that is trivial.
- **Never a delivery path.** `seed knowledge search` ranks and points;
  the envelope says `advisory`; contested, retired and expired lessons
  are as absent as at claim time; without `--repo` no lesson is
  indexed and every candidate is `lessons_unresolved`, the delivery
  posture exactly. The predicate remains the only claim-time surface.
- **No knobs.** k1 1.2 and b 0.75, the Lucene idf, lowercase
  alphanumeric runs of two or more runes, no stemming and no stop
  list; ties break on kind then id. One derivation whose ranking is a
  function of the corpus and the query alone, so two readers of one
  tip see one ranking.
- **Docs are named, never discovered.** `--doc <path>` under `--repo`,
  a clean relative path (the store's own rule), split by level-one and
  level-two heading into sections with a `<path>#<slug>` id. The verb
  does not walk the tree: what a reader searches is what the reader
  said, and an adopter's decision log is wherever they keep it.
- **Flags first, the query last.** The flag package's shape and every
  other verb's; a query of no searchable term refuses at usage.

## Trace-shaped evidence (os-7fc2ca38)

The build plan's §3 entry put "the artifact-store digest of the
exported spans" in the receipt. It cannot go there: a receipt must
recompute from a fresh run to the cited digest, and a raw trace export
never does (ids and timestamps differ every time). The plan's D5
corrected it, and the implementation holds to the correction.

- **The variable, not a flag.** `SEED_TRACE_EXPORT` is set by the
  profile for every command, visible and sealed, and read after each
  exits. A flag on the spec would have made the harness's behavior a
  spec author's claim; the variable makes it the runner's fact, and a
  harness that ignores it is indistinguishable from one that ran
  before the variable existed.
- **Declared keys, allowlist not denylist.** A denylist of "known
  nondeterministic" keys would have let the next per-run key through
  and broken reproduction on the first real harness. The spec declares
  what reproduces; the gate reviews the declaration; an undeclared
  run id stays out.
- **One root per orphan, sorted.** A span whose parent is absent from
  the export is a root rather than a refusal: file exporters truncate,
  and a shape that refuses on a truncated export would bind nothing on
  exactly the runs a verifier most wants to cite.
- **The sidecar is a pointer, not a slot.** The raw export is
  content-addressed like everything else; `traces/<shape>` holds its
  digest. Erasing the raw digest through `seed artifact erase` removes
  the export and leaves the pointer dangling, which `TraceRaw` reports
  as a digest `Get` cannot produce; erasing the shape digest removes
  the pointer with the shape. No named mutable slot to rotate.
- **No protocol bump.** `verdict.rendered` is a strict object and a
  new optional field would be `seed/8` for a reference the receipt's
  shape digest already covers. The receipt schema grows two omitempty
  fields, the rule `commitment` and `sealed_transcripts` set.
- **No cache table.** The projection build carries no artifact store
  by design (admission has none either); III.G row 10's query joins
  through `verdict_receipt` as #119 made it. `seed verdict traces` is
  the read surface, over the store.
- **`defer` stays where it was.** The verdict group's registry row
  lists `receipt, render, check, traces`; `defer` was unlisted before
  this card and this card does not re-home it.

## The admission random walk: an instrument that explores (os-21bf939f)

The III.I class ran over every prefix of one hand-written walk, and
every generator in the tree walks a scripted shape, so rule
interactions at positions no script reaches were tested only where
someone thought of the case. The walk (plans/os-21bf939f.md) draws
instead: a lane, a subject and a move at every position, the listed
move re-drafted the way the sweep re-drafts and checked, the
forbidden move drafted by a lane holding the capability and expected
to refuse on the table's ground. What it found on its first runs, and
what was decided about each:

- **The sweep's re-draft copy lacked the escalation anchors.** The
  first eight walks failed the III.I class at once: `decision.recorded`
  listed for root on an escalated contract, re-drafted by
  `probeViewAt` as an answer citing escalation "", refused. The copy
  is deliberately independent of the production view so drift between
  them fails the class; here the drift was the copy's, missing an
  anchor the scenario never needed. Fixed in the copy (the standing
  question's position and first option, from the fold, as the
  production view derives them). A test-helper change under D7: no
  production surface moved.
- **Verb-scoped rules refuse ahead of the lifecycle rule, by the rule
  set's order.** The plan's transition oracle asked for the lifecycle
  rule's own error on every forbidden draft. The rule set runs fence
  (position 862) and escalation (982) before lifecycle (2340), so a
  forbidden exit drafted with the catalog's `"fence": "0"` on a
  subject holding no claim refuses as a fence citing nothing active,
  and `escalation.raised` while a question stands, or
  `decision.recorded` where none does, refuses as the escalation
  rule's. Each is a truthful refusal on a ground the illegal state
  implies; none masks the transition. The oracle accepts exactly those,
  enumerated by type and condition (`FenceError` with `Active < 0` on a
  draft that cites a fence; `EscalationError` on the two verbs that
  rule scopes) and fails any other earlier refusal as the named
  refusal-ordering class, so a new case is classified by a human
  rather than absorbed. The lifecycle rule's `ContentionError` on an
  exclusive verb counts as reached: it is that rule's refusal of a
  second claim, the ceiling's claim clause.
- **Reached is counted per run, not per walk.** At the fast size a
  walk's six forbidden draws can all land on the fence and escalation
  rules by chance (seed 20260906001 at 24 steps did), which says
  nothing about the instrument. The run must reach the lifecycle rule
  somewhere; a walk need not.
- **Two holders of the walk's own.** The shared scenario revokes its
  holder at seed/3, so a walk started from its end had no
  claim-capable lane. Root enrolls and grants two holders in the
  preamble, through the same admit path as every step; two rather than
  one puts contention within the draw.
- **The walk lives in `admit_test`, behind an export shim.** The end
  oracle is `simulate.Audit`, and that package imports `admit`, so the
  walker cannot sit in the internal test package (an import cycle). It
  sits in the external one and reads the shared scenario, the sweep's
  re-draft and the fixtures through `export_test.go`; three consumers
  of one scenario and one re-draft, no copy of either.
- **The epilogue closes what the walk left open, through the listed
  path.** The audit's abandonment bar reads an open claim window at the
  chain's end as silent abandonment, and a walk stops mid-window by
  construction. The walk lifts a standing halt and releases every
  active claim by its holder before the audit, each as a listed verb
  re-drafted and checked, so the epilogue is itself III.I evidence and
  the walk's exits are the deliberate ones the lease clause names.
- **Sizes.** Eight walks of forty-eight steps measured about eleven
  seconds locally, over the plan's five; the steps were halved first,
  as D6 orders, and eight by twenty-four measures about five. The
  scheduled size is two hundred by ninety-six, about three seconds a
  walk on the implementing host, as one step on the weekly
  `perf-scale` job. Sixty walks of ninety-six steps ran green locally
  before the sizes were fixed.
- **Nothing in production moved.** No walk found a forbidden verb
  admitted, a fold disagreeing with the table, a non-lifecycle verb
  moving a state, a nondeterministic listing, or a red audit bar. The
  findings were the instrument's own, above.

## The forge says the submission is not mergeable (os-0cd18799)

Not a plan item: a §3 backlog card against charter §II.4 (`check.observed`,
named there and in the verb catalog and built nowhere), §II.8, §II.11 and
§II.13. The gap: after `submission.made` nothing was owed between the pull
request opening and the merge being observed, the verifier never read the
forge, and the return cited a fail verdict alone, so an agent opened the
pull request and stopped. What landed (`spec/observations-forge.md`):

- **The fact is the observer's, and it binds to the head.**
  `check.observed` admits on review subjects from `seed/8` with `{pr, head,
  checks, unresolved_threads?, review}`; the head must equal the tail of
  the submission packet's `base` range, so a stale poll never stands for
  the head being judged. Literals and a count only: the reader carries no
  thread body, comment, check output or reviewer name, and the injection
  sweep plants its marker in every field the fakes serve.
- **Change is the admission condition.** An unchanged observation refuses
  naming the standing one, at the boundary and before signing in `seed
  check observe`, so the ledger's share is bounded by change rather than
  by polling cadence.
- **Absent is not zero.** Forgejo has no thread resolution, so its
  observation carries no thread count rather than none; the forges page
  names it as it names the pull-request rule.
- **No checks is green.** A head the forge lists no checks for is green,
  the v1 engine's `checksGate` posture: nothing failed and nothing runs.
  Pending is neither red nor green and owes nothing.
- **The return by observation records no lockout.** The v1 `reject` edge
  blacklists the author; a red pull request wants its author back, so the
  return adds no fail and no rejected fact and the prior submitter is the
  natural next claimant. The table row is unchanged; the citation rule
  widened to exactly one of `verdict` or `observation`, the latest
  observation only.
- **`merge.requested` refuses while red on both citation paths.** The
  override overrules a verdict and says nothing about the forge; a chain
  branch protection would hold must not sit `unreconciled`.
- **The verdict is owed only under review.** The drift sweep found the
  first draft advertising `submission.pending` on a subject a return by
  observation had re-readied unjudged, a debt nobody could discharge, and
  `verdict.unmerged` beside a red observation while `merge.requested`
  refused. Both are suppressed by derivation, not by a special case in the
  sweep.
- **The pass re-reads between observe and return.** `maintain.Deps`
  gains `Observe`, `Refresh` and `ReturnCeiling`; the return reads the
  fresh fold so it cites the observation the pass just recorded, rather
  than widening `Append` to return positions.
- **The ceiling is a count on the chain.** `Returns` on the fold records
  what each return cited; the fourth observation-cited return under the
  default of three is `escalation.raised` with the submission packet's
  base, the observation's position and head, the count, and three
  answers. Rounds are counted, never timed.
- **Named follow-ups, not built here.** A forge webhook as an advisory
  wake adapter (§II.9); preferring the prior submitter's tuple for the
  re-offer, supervisor ranking policy (Phase 13 item 7), filed as
  os-29e2fef2.

## III.F row 12: the graph beside the lifecycle (os-f0ae2cdf)

Plan #341 (`plans/os-f0ae2cdf.md`) bound eight decisions; the task PR
exercised them as written and refined three at the seams the plan left
to implementation.

- **The fold judges each fact at its own position, and the boundary
  reuses the fold.** D3 asked for one trustworthy graph; the task has
  `topology.Fold` replay the keyring and the lifecycle to every
  relation's own prefix and `Graph.Apply` serve both the admission rule
  (the candidate against the admitted graph) and the fold (each
  admitted fact in turn). A raw-pushed edge therefore never reaches
  the cycle check as an edge: it is an anomaly before any later fact
  is judged. The cost is a prefix fold per relation fact, paid only on
  chains that carry one (`topology.Carries` is the fast path).
- **The claim boundary refuses with its own code.** D4 said
  `claim.taken` consumes the predicate; `not_effectively_ready` (exit
  3) names the unresolved dependencies and the holding ancestors so a
  worker reading the refusal knows why the queue hid the subject. A
  subject that is not `ready` at all keeps the table's refusal: the
  graph judges claimability, never legality.
- **`seed offer wake` registers no channel.** D6 reuses
  `executor.Adapter.Wake`, and every shipped adapter's `Wake` is the
  documented no-op because workers pull. The CLI pass therefore
  reports the delta and its candidates and says `channels: 0`; the
  drills drive `offers.Bridge` with a recording channel, which is the
  seam a deployment with a real channel plugs into. No per-actor
  adapter registry was invented for a channel nobody has.
- **The graph renders only where the chain carries it.** D8 named the
  fields; the task gates every surface on the fold carrying a trusted
  relation (the report also on an anomaly), the knowledge and
  requests sections' posture, so relation-free chains keep
  byte-identical bodies and only the version in the build id moves.
  The queue's derivation string moves with the semantics
  (`transitions/1+topology/1`), the old constant kept for the bump
  drills.
- **Versions: contracts "15", queue "4", report "19", cache "15" at
  schema generation 13.** Seven pins moved in the projection drills
  (the four report pins, the racing view's contracts pin, the cache
  time drill's stamp, and the lifecycle views' build ids).
- **Eligibility left `cmd/seed`.** Step 3 asked for one shared helper;
  `internal/offers` now holds `Eligible`, `Authorized` and `Live`, and
  the report's private copy of the authorization check reads the
  shared one, so the poll, the bridge and the report cannot drift.
- **`Fold.Milestone` is exported.** D7's rollup sums the descendants'
  admitted high-water counts, which the fold held unexported; the
  accessor is the one addition to `internal/transition`, beside no
  change to the table.
- **The lane trajectory corpus is re-recorded.** The dispatcher's
  affordance frame at every position now carries the relation verbs
  (and the maintenance lane's replay reads the dispatcher's), so the
  committed trajectories differed from the recorder's scenario by
  exactly that frame change; `go test ./cmd/seed -run Corpus -update`
  re-recorded `trajectories/lanes/{dispatcher,maintenance}.json`
  on purpose, the scenario and the posture unchanged.

## Verdict defer on the machine surface (os-ef2e3134)

The gap os-7fc2ca38's last bullet recorded: the dispatcher routed
`verdict defer` while the registry row and the usage line omitted it,
so `seed serve` derived a method set without the human-verdict
deferral and a lane over the machine surface had no way to route a
low-confidence item to a human (III.I). One word in the row, one in
each usage string, in the dispatcher's order; the vocabulary drill
holds row and line equal in both directions, and
`TestVerdictDeferExposed` names the method. No flag, refusal or
posture of the verb changed, and no spec text: the method set is
derived from the catalog, never listed.

## The append loop enumerated, and the model kept honest by replay (os-07e6e76c)

The append protocol's evidence was executions: two appenders in the
race drill, twenty-four in the perf gate, three rollback drills. The
model (plans/os-07e6e76c.md) enumerates instead: the loop's own steps
as an alphabet, every interleaving for small N walked depth-first and
memoized, five properties at every state, and the model's traces
replayed through the real client so the model is evidence about the
tree rather than about itself. What was decided and found:

- **Acceptance is by ancestry, not equality (review finding on the
  plan PR, #356).** The plan's first land rule required the remote to
  still stand at the fetched tip. A plain `git push` and the hook's ref
  rule both test `merge-base --is-ancestor old new`, and `Client.Push`
  uses no force-with-lease, so the client lands whenever the fetched
  tip descends from the current one. Under an enforced remote the rules
  coincide; under a cooperative rollback they differ, and the model
  carries the difference: an in-flight push on a pre-rollback tip lands
  and heals the truncated line (the healing shape), and a fresh writer
  landing on the rolled-back tip first forks the remote (the fork
  shape). Both shapes are reachable in the cooperative configuration
  and both are replayed by name.
- **The named shapes are classified by role, not by the chain's shape
  (review finding on the task PR, #359).** The first fork predicate
  accepted any terminal chain that left the old line behind, and in a
  two-writer configuration the first such trace had no in-flight
  writer at all: one writer had landed and finished before the
  rollback, the other was the fresh writer, and the replayed "fork"
  exercised neither the lost race nor the regression refusal the plan
  describes. The plan's sentence names three roles (the writer whose
  landing the rollback erases, the in-flight writer on that line, the
  fresh writer on the rolled-back tip), so the cooperative
  configuration carries three writers with two attempts, a deviation
  from D5's "two writers with two attempts" forced by D3's own P3, and
  the predicates read the roles: healing is a writer that fetched the
  old line before the rollback and landed on it after; the fork is a
  writer that lost a race after the rollback and then ended in head
  regression, with the terminal chain off its persisted head. The
  model marks post-rollback landings and post-rollback lost races per
  writer for that purpose. In the three-writer configuration 108 of
  312 terminal states heal and 12 fork, and the replayed fork trace
  carries the `ErrNonFastForward` then `ErrHeadRegression` sequence.
- **The first terminal P3 check was wrong, and the enumeration said
  so.** It read "a writer whose persisted head is off the terminal
  chain cannot have landed" and fired on the fork trace, where the
  first writer landed before the rollback and its line was abandoned
  by it. The property is about landings after the rollback: those must
  sit on the terminal chain, and a writer whose pre-rollback line was
  healed must be on it too. The model now marks post-rollback landings
  and checks exactly that. A model-side finding, fixed in the model.
- **Outcomes are the loop's errors.** The model's vocabulary is landed,
  `ErrNonFastForward` for a lost race (a stale tree folded in, since
  the loop retries both alike), `ErrRetriesSpent`, `halt.HaltedError`
  from re-validation, `ErrHeadRegression` from the fetch; the replay
  asserts each with `errors.Is` or `errors.As`.
- **The replay reaches `attempt` in-package.** No production seam was
  added: the replay drives `Fetch` and the unexported `attempt`, the
  function `AppendLoop` iterates, one real client per writer against a
  private copy of one staged bare remote (genesis plus the upgrade to
  seed/1), the rollback as `git update-ref` as the existing drills do
  it. Every step's outcome class, every landed record's `prev` against
  the tip its client fetched, and the final chain's order matched the
  model on every trace.
- **The A >= N lemma holds and races.** With one draft per writer, no
  halt and an enforced remote, a writer loses at most one race to each
  rival, so A = N never spends a writer: none of 4 (2x2), 36 (3x3) or
  576 (4x4) terminal states does. At A = N-1 the counterexample exists:
  2 of 4 terminal states at 2x1, 12 of 36 at 3x2.
- **Sizes.** The four fast-gate configurations walk 18, 251, 223 and
  1,151 states (4, 36, 32 and 312 terminal) in under fifty
  milliseconds; the scheduled 4x4 walks 7,020 states (576 terminal) in
  a third of a second, so enumeration is never the cost. The replay
  is: eight sampled traces, the healing and fork among them and the
  rest dealt round-robin across the three replay configurations and
  spread evenly within each, take about two seconds locally; every
  trace (348) takes about 133, which is the scheduled size on the
  weekly `perf-scale` job. On the task PR's CI the Windows leg that
  carries `internal/gitref` ran the whole package in 98.5 seconds
  (the platform matrix runs packages without `-v`, so the replay's own
  share is not separable there; the package was already the
  git-heaviest in the tree before this card); the Ubuntu and macOS
  legs finished the package inside their usual bounds.
- **Nothing in production moved.** No interleaving lost an update,
  landed a record off the fetched tip, moved a head backward, landed a
  normal record after a halt, or failed to terminate, and no replayed
  step disagreed with the loop.

## The projection boundary has three sides (os-b45c308d)

Plan #350 charters Phase 13 item 8: III.D rows 5, 6 and 7 at the
projection boundary. What landed (`spec/projections.md`
"Components" and "The mirror", `spec/external-facts.md`):

- **The mirror is a component, not a verb.** `seed-mirror` reads the
  contracts build `seed project current` resolved (the envelope, then
  the view inside the named build: the write-boundary lint forbids the
  published layout's vocabulary outside the engine, and the consumer
  verb is where freshness is demanded anyway) and writes a forge's
  issues through one adapter interface; it imports no Seed internal at all, so
  the authority lint's claim about it is about an executable's import
  closure, not a naming convention. The marker is base64 of the subject
  so an opaque subject cannot escape the comment, and the planner
  refuses a duplicate, an undecodable marker, or a marker naming a
  subject the projection does not hold, rather than choosing.
- **The classes are derived, never listed.** The export path is the
  sealed registry and what it contains; the write path is the packages
  owning an append, a commit, a push or a proposal and every call site
  of one. A component is a `main` under `cmd/`. Both classes come out of
  `go/parser` over the tree with no `x/tools` dependency, the
  write-boundary lint's precedent, and the real-tree walk asserts both
  classes non-empty so the lint is proving something.
- **The plan's D5 was already on `main`.** The plan describes
  `check.observed` as a new verb at the next protocol version with
  `{name, status, commit, reference}`; #351 (os-0cd18799) had landed
  the verb at `seed/8` with `{pr, head, checks, unresolved_threads?,
  review}` between the plan's writing and this card's claim. A second
  verb wearing the same name would be a design violation, so this card
  pins the landed verb into the catalog and proves the plan's
  properties against it: observer or operator, the strict shape, the
  version boundary (`TestCheckObservedNeedsSeed8`), no transition row,
  no discharge, no verdict. The plan's "pending emits no fact" arm is
  not adopted either: the landed verb records pending as the forge's
  word, and the obligation drill already pins that pending alone owes
  nothing.
- **An observation narrows, never widens.** The plan's AC6 says the
  fact changes no obligation; on the tree a red `check.observed` raises
  `submission.unmergeable` and makes `merge.requested` refuse, which is
  the charter's checks gate. The invariance drill pins the exact shape
  instead: a green observation leaves state, every lane's affordances,
  obligations, budget, submission and verdict identical to a chain
  without it; a red one adds exactly the unmergeable debt and the
  return that discharges it, and legalizes nothing. That is what
  "nothing treats an observation as control" means here.
- **GET only, with one named variance.** GitHub exposes review-thread
  resolution only through GraphQL, a POST. The source sends one paged
  `query` through a helper that refuses any operation not beginning
  with `query`; the lint allows POST in that helper alone and counts
  exactly one in the package. Forgejo's thread count stays nil.
- **The readers moved, the mutators stayed.** `protections.Observer`,
  `Observation`, `PRNumber` and the three readers are now
  `externalfact.Source`, `Observation`, `PRNumber`, `GitHub`, `Forgejo`
  and `Snapshot`; `externalfact.Open` replaces the CLI's forge switch,
  returning `NoCredential` (exit 5, naming the variable) apart from
  usage errors. Protection reconciliation is untouched.
- **The raw seam stays raw.** `seed ledger append` without `--remote`
  appends without judging grants, the cooperative posture's documented
  consequence, so the drill proves the service key's direct acts refuse
  through admission (`admit.Check`) and the loop transport
  (`request answer`), not through the raw seam.
- **Named follow-ups, not built here.** A `seed-mirror` build in the
  release workflow (a protected path); an `Observation` field on the
  contracts view, should the mirror ever need check state in a label.
## os-29e2fef2: prefer the prior submitter's tuple when re-offering a contract returned on the forge's word

Plan: `plans/os-29e2fef2.md` (#358). Decisions taken while implementing,
each inside the plan's autonomy contract:

- **The predicates the plan lifted already lived in `internal/offers`.**
  D3 named `ranking.OfferAuthorized` and `ranking.Eligible` as lifts of
  the two private copies; #352 had lifted them to `offers.Authorized`
  and `offers.Eligible` before this task started, and `offer list`, the
  report and the bridge already read them. `ranking.Resume` reads those
  two rather than adding a third copy; `internal/project/report.go`
  and `cmd/seed/offer.go` are untouched on that account.
- **`seed/9` restores the runtime-tuple semantics.** `tuple.Applies`
  names seed/2 through seed/4 and was not extended when seed/5 through
  seed/8 were registered, so on the forge loop's seed/8 chain no grant
  could cite a tuple, no `run.started` could declare one and no offer
  could scope by one: the resumption could never derive there. The
  first cut extended the list in place; the review found (correctly)
  that this re-judges records the shipped binaries already admitted
  tupleless at seed/5 through seed/8, which the protocol forbids, and
  exceeds D6. So seed/5 through seed/8 keep their recorded judgment
  and `seed/9` restores the semantics, registered in `version` and the
  protocol register with the usual two-validators-disagree trigger,
  every later gate (evals, levels, import, racing, requests, forge
  checks) naming it too. This is a protocol change D6 did not
  foresee, taken here rather than escalated because the plan's purpose
  is unmet without it and the bump is the tree's routine shape for a
  change in judgment; the forge stand and the resumption drills run
  at seed/9. `internal/tuple/` and `internal/version/` are
  the files outside the plan's scope list, for that reason.
- **The window derives even when the tuple does not.** `Resume`
  returns the fence, holder and consumed offer whenever a return by
  observation stands, and `ok` only when the tuple also derives, so
  the re-offer's capabilities and tiers come from the consumed offer
  in the undeclared case too, as D3 asks.
- **No tuple derived keeps the consumed offer's own tuple scope.** D3
  says both "unscoped by tuple otherwise" and "never wider than the
  consumed offer's"; where the consumed offer was tuple-scoped and the
  prior tuple does not derive, the re-offer carries the consumed
  offer's tuple set unchanged, so the second rule holds and the first
  applies to an unscoped consumed offer.
- **`Refresh` returns the records too.** The re-offer reads the return
  the pass just appended, and the resumption reads records as well as
  the fold; the one refresh seam now yields both, rather than a second
  seam.
- **The run's signer is not matched.** `run.started` is the
  supervisor's act on the holder's window (the set rule reads the
  holder's grants, never the signer's), so the resumption takes the
  first start at the fence, the admitted once-per-fence one.
- **The maintenance-only-key clause of AC3 is drilled at the effect
  seam.** A key holding `maintenance` alone is refused on the return
  first, so the pass never reaches a re-offer to have refused; the
  refusal-is-reported posture is drilled in `internal/maintain` with an
  `AppendAt` that refuses, and the CLI drill keeps the return's
  refusal.
- **The reclaim in AC4 is the admitted `claim.taken`.** `claim take`
  is remote-only by design; the forge stand is a local ledger, so the
  loop drill reclaims through the same admitted append the existing
  forge drills use, the claim boundary unchanged.
## os-873b5153: the interleaving explorer over reconciliation and racing

Plan: `plans/os-873b5153.md` (#366). Decisions taken while implementing,
each inside the plan's autonomy contract:

- **The models enumerate over real records, so the replay is the model.**
  D2 and D5 describe an abstract model plus a separate replay of its
  terminal traces through `admit.Check`. Both models here take the
  shorter road: every step drafts a real signed record, offers it to the
  real `admit.Check` against a real staged ledger, and applies it only
  if admission takes it, with every fact a property reads coming from
  the real fold and, in the reconciliation model, from the real
  `reconcile.Classify`. A refused step is a no-op that still consumes
  depth, so the walk explores what the boundary allows. This removes the
  gap D5's replay exists to close rather than closing it after the fact:
  there is no second implementation of the rules to drift. The forge
  steps keep D5's stated asymmetry, having no ledger counterpart.
- **The class partition is pinned by parsing the package's own source.**
  D3 asks that a class added to `internal/reconcile` and to no set fail
  the test. Rather than adding an exported `AllClasses()` to production
  for a test's benefit, `TestClassPartitionIsExhaustive` reads the
  package's `Class*` constant declarations out of its source and checks
  the three sets cover them exactly. The trust surface stays as it was.
- **Sizes (D7).** The fast gate runs the reconciliation model at depth 4
  (852 terminal states across its two staging variants, about 1.9s) and
  the racing model at depth 7 (2,007 terminal states, 70 of them
  settled, about 2.5s): 4.4s for the pair, inside the plan's five-second
  budget. `perf-scale.yml` runs depth 6 (7,068 terminal states, about
  20s) and race-depth 9 (22,599 terminal states, 1,162 settled, about
  33s) weekly.
- **A real divergence, fixed under D8: the settlement was unreachable on
  a racing subject.** The racing enumeration could not settle a race at
  all. On a racing subject a rival racer is still holding when the
  winner's submission reaches review, so the fence rule required both
  `merge.requested` and `merge.observed` to cite that rival's fence,
  and the chain rule pins both payloads as strict objects with no slot
  to carry one in. Every settled-out fact §II.6 rests on was therefore
  unreachable through the front door; the existing racing tests missed
  it because their fixtures append their chains straight to the store
  and only `Check` the assertions they name. The fix is one exemption in
  the fence rule, beside the run facts' existing one: the merge chain is
  subject-scoped, never claim-scoped, so the fence rule leaves it to the
  chain rule. On an exclusive subject nothing changes, the submission
  that reaches review having already closed the only window.
  `TestSettlementAdmitsWhileARivalRacerHolds` keeps the enumeration's
  trace as the named regression, and asserts that a holder's own
  unfenced act still refuses, so the exemption stays the chain's alone.
  One existing assertion moved with it: a `fence` key on a
  `merge.requested` payload now refuses as the strict shape rather than
  as a fence complaint, which is what it is.
- **A second finding, in the model rather than the code: what a
  settlement rests on is the position its request cited.** At race-depth
  9 the enumeration produced `claim(r0) claim(r1) submit(r0)
  verdict.pass(r0) submit(r1) request verdict.fail(r1) settle`: an
  authentic settlement followed by a fail on the LOSER's submission.
  Reading the subject's singular verdict fact calls that trace
  unverified. The property now reads the position the `merge.requested`
  cited, which is what admission itself enforces.
- **The fold counted the same fence the boundary had stopped counting
  (review on #370).** Exempting the merge chain in admission alone left
  the tolerant fold's own fence-anomaly counter demanding the rival
  racer's citation, so every admitted settlement on a racing subject
  folded with an anomaly and `reconcile.Classify` reported
  `merge_without_verdict` on a chain the boundary had just taken: one
  rule, two consumers, disagreeing (§II.10). The predicate is now
  `transition.IsMergeChain`, read by both.
- **What the chain rests on is the verdict its request CITED, in the
  fold and the classifier too (review on #370).** `FoldRecords`'
  `passChain` and `reconcile.Subject` both read the singular latest
  verdict, so the racing trace the racing model's P2 already accounts
  for (a fail on the loser's submission after the winner's pass was
  requested) reached `done` with an anomaly and a false finding.
  `SubjectState.CitedPass` resolves the cited position against the
  verdicts the fold kept, and the classifier splits `anyPass` from
  `citedPass` so its two findings keep the meanings they had: no pass at
  all is a merge without a verdict, a pass the chain did not run through
  is a skipped link. `VerdictFacts` reads the singular fact when the
  list is empty, so a hand-built state (a classifier table test) is
  judged as it was before the list existed.
  `TestSettlementSurvivesTheLosersLateFail` pins the trace by hand,
  since the fast gate's race-depth 7 does not reach a nine-step trace.
- **P1's admitted arm requires an admitted trace (review on #370).** It
  accepted `seenRaw` as a substitute, which would have hidden a
  front-door regression that left an admitted-reachable class reachable
  only by forgery.
- **`independence_unverified`'s record half is reached rather than
  excepted (review on #370).** A fifth raw step drafts the verifier's
  pass declaring `L2` where the records support `L1`; admission requires
  the declared level to equal the achieved one, so it only ever lands
  raw, and `VerifyVerdicts` re-judges it from the same facts. All nine
  classes in the two ledger-reachable sets are now produced by a trace.
- **The reap is taken, not composed (review on #370).** P4 built a
  packet and called it a reap. Each settled terminal state now signs the
  maintenance lane's `claim.reaped`, puts it through the real admission,
  appends it, and refolds: the claims are gone because the reaper
  removed them, and the subject stayed `done`. It runs from the terminal
  state rather than as a step in the alphabet, which keeps the
  enumeration exhaustive at a usable depth.
- **P3's non-exit probe is `progress.milestone`, not `submission.made`.**
  `transition.IsExit` counts a submission as an exit, since it closes
  the window, and the `race_settled` rule lets a settled-out racer take
  its own exits. The arm that checks what a settled-out racer may no
  longer do needs a plainly non-exit act.


## The card's canonical form is RFC 8785, and the gate is a content gate (os-f11601e0)

Plan #368 closes two gaps a read-only review found in the boundary that
landed with Phase 13 item 5 (#279, `plans/os-40ed0ca0.md`). What
landed (`spec/boundary.md` "The capability card"):

- **The hand-rolled canonicalizer is retired, correcting a recorded
  decision.** "Canonical bytes without a JCS library" reasoned that a
  compact encoding with sorted members and no HTML escaping is
  JCS-canonical for a value domain of strings and arrays. It is not:
  Go's `encoding/json` escapes U+2028 and U+2029 unconditionally, JCS
  emits them literally, and `Card.check` constrains `Name` only to be
  non-empty, so a deployment name carrying either character was signed
  over bytes no independent JCS verifier reconstructs. `Canonical()`
  now routes through `jcs.Transform`, the same transform
  `event.Canonical` signs over: one canonicalizer for the tree, and a
  dependency already in `go.mod`. The correction is written beside the
  original entry rather than replacing it, because the reasoning is
  worth keeping visible next to why it was wrong.
- **The regression class is "two canonicalizers disagree", so the
  drill is a property.** `TestCanonicalIsRFC8785` compares
  `Card.Canonical()` against an independent JCS transform over a corpus
  that includes the two separators in a name, in the ingress and in a
  squad name, plus HTML-significant runes, non-ASCII, and an emoji
  outside the BMP. A future hand-rolled optimization fails there by
  name. The property was run against the old body first and fails at
  exactly the separator cases, so it is not vacuous.
- **The switch is proven byte-for-byte, not assumed.** The checked-in
  `boundary/card.json` canonicalizes to an unchanged 323 bytes
  (sha256 `b9ba3490…`), pinned as a fixed vector, so no signature
  anywhere is invalidated. The test asserts *bytes*, not that the
  signature verifies, and says why in the file: that card's key is a
  throwaway kept out of the tree, so nothing here can verify it.
- **The reader's half is its own verb.** `boundary check` requires
  `--config` and `--name`: it is the publisher's verb, comparing a card
  against the declaration that renders it. A stranger who fetched a
  card from someone else's `/card` has neither, so the spec's "a reader
  verifies against the operator key it was given out of band" was
  reachable by no verb this build shipped. `seed boundary verify --card
  <file> --pubkey <hex> | --pubkey-file <path>` takes no declaration
  and no name, and reports the signer. Its refusals are `card_refused`,
  never `card_drift`: drift is the publisher's finding that its own
  card is stale, and a reader is not in a position to make it.
- **The key never comes from the card, and no flag lets it.** A card
  carrying the key that signed it would prove nothing, so both verbs
  take the key only from `--pubkey` or `--pubkey-file`, and naming both
  at once is a usage error rather than a precedence rule.
- **The Makefile gate is unchanged, and the envelope stops implying
  otherwise.** An earlier draft proposed pinning a public key beside
  the card so the gate could verify a signature. It is unexecutable:
  the fixture card's key is gone and cannot be derived from a
  fingerprint or a signature, so no agent can make that card
  verifiable. `make check` therefore keeps proving what it can prove
  with no key, that the card says what the declaration renders, and the
  success envelope now carries a note saying exactly that beside its
  `"verified": false`.
- **A key pinned in-tree is a trust anchor or it is nothing.**
  `boundary/` is on neither the declaration's `protected` list nor
  `CODEOWNERS`, so a key checked in there could be swapped together
  with the card in one non-owner change and a gate reading it would
  still pass. The constraint is recorded in the spec for whoever
  rotates: the key file joins both lists in the change that introduces
  it.
- **Rotation is the operator's act, filed not performed.** Minting this
  deployment's operator identity, re-signing the card and checking in
  the public half is an operator decision, not an implementer's. It is
  filed as its own card with the D1 constraint attached rather than
  done on the operator's behalf.

## `seed message send`: mail gets a verb rather than a documented append form

The promotion packet's cutover left one gap open by name: mail was "the
one loop act without a verb of its own", and the cutover pull request
was to settle it either by naming the `seed ledger append --verb
message.sent` form or by adding the verb. This adds the verb, and the
reason is the addressing contract rather than convenience.

- **`to` has three states at the reading end, and the third is a trap.**
  `project.AddressedTo` resolves an absent `to` to everyone, a string or
  all-string array to those actors, and a `to` that is present and does
  not parse to **nobody**, deliberately, because every other reading
  invents intent. A hand-assembled payload can land in that third state
  by mistyping JSON, and the failure is silent: the message admits, and
  reaches no one. The verb writes an all-string array or omits `to`
  entirely, so a sender cannot reach the undeliverable state at all.
- **A recipient that could not be a fingerprint is refused before the
  append.** Addressing resolves against fingerprints, so `--to
  shaunlmason` would admit and deliver to nobody. It refuses `usage`
  instead, naming the form, and the tip does not move.
- **Nothing else is duplicated.** The verb assembles a payload and
  delegates to `ledger append`, so the classification lint that bounds
  the body's size, both postures, the declaration and the persisted
  verified head are the same code. A second append path would be a
  second place for those to drift.

No read-state is introduced: this is the sending half only, and
`message.acked` stays unimplemented for the reason `obligations.md`
gives, that the position a lane carries forward is already its cursor.
