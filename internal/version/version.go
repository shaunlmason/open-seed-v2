// Package version carries the Seed binary's identity. Seed is the successor
// system chartered in SEED-NEXT.md, incubating under next/ per
// docs/build-plan.md section 0. The protocol version is the wire seam
// (charter Part II section 1): genesis names it, every event carries it, and
// a mismatch refuses with a distinct exit code.
package version

// Name is the binary name: the successor claims the name (SEED-NEXT.md).
const Name = "seed"

// Version is the module version: pre-release from source, and the
// released version when the release workflow stamps it
// (.github/workflows/seed-release.yml sets it with -ldflags -X at
// build time; plans/os-2e46aa2f.md D3). A var rather than a const for
// exactly that reason, and nothing else assigns it.
var Version = "0.0.0-dev"

// Protocol is the protocol version minted at genesis (build-plan fixed
// default). The bump discipline is recorded in spec/protocol.md.
const Protocol = "seed/0"

// Seed1 is the protocol version that activates the actor keyring
// semantics (spec/actors.md; plans/os-52a2d688.md). Chains reach it
// through system.protocol.upgraded; Protocol stays the genesis default
// until a recorded decision moves it.
const Seed1 = "seed/1"

// Seed2 is the protocol version that activates runtime-tuple semantics
// (spec/qualification.md; plans/os-8e53ffd9.md): actor.granted may
// cite a tuple, run.started must declare one, and drift refuses as
// out_of_grant. A seed/1 validator strictly decodes actor.granted as
// {capability} and so judges a tuple-bearing grant differently, which
// is spec/protocol.md's bump trigger; records at seed/1 positions
// keep seed/1's judgment.
const Seed2 = "seed/2"

// Seed3 is the protocol version that activates the qualification verbs
// (spec/evals.md; plans/os-03e47abb.md): actor.qualified mints a
// grant from an eval's verdict, actor.disqualified suspends one, and
// intent.filed may mark a contract as an eval. A seed/2 validator fails
// a chain carrying either verb as bad_actor_event (actor payload shapes
// are chain validity), which is the bump trigger; records at earlier
// positions keep their earlier judgment.
const Seed3 = "seed/3"

// Seed4 is the protocol version that activates the independence levels
// (spec/verdicts.md; plans/os-99829835.md): verdict.rendered's
// independence widens from the literal L1 to the ordered vocabulary
// {L1, L2, L3}, the verdict may declare the verifier's runtime tuple,
// and the tier table's independence column is enforced at the verdict
// boundary. A seed/3 validator's strict verdict decode refuses the
// tuple field and the wider vocabulary, which is the bump trigger;
// records at earlier positions keep their earlier judgment.
const Seed4 = "seed/4"

// Seed5 is the protocol version that activates the predecessor import
// (spec/import.md; plans/os-cf13fb51.md D2): system.imported, the
// operator-only provenance record a genesis import appends once,
// before the replayed history. A seed/4 validator has no row for the
// verb and refuses it as out of grant, so the two judge a seed/5 chain
// differently, which is spec/protocol.md's bump trigger; records
// at earlier positions keep their earlier judgment.
const Seed5 = "seed/5"

// Seed6 is the protocol version that activates racing mode
// (plans/os-56bee171.md; spec/lifecycle.md "Racing"): claim.taken
// admits from in_progress on a racing squad's contract and
// submission.made from review, each racer with its own fence; a seed/5
// validator's table refuses both origins by version rather than by
// misjudging a race.
const Seed6 = "seed/6"

// Seed7 is the protocol version that activates the request ingress
// (plans/os-48df10a2.md; spec/requests.md): request.filed, the
// one verb a proposal from outside the trust boundary enters by, and
// request.answered, the dispatcher's close. Both are undefined before
// it, so a seed/6 validator refuses an upgraded chain at the first
// request record by version.
const Seed7 = "seed/7"

// Seed8 is the protocol version that activates the forge observation
// (plans/os-0cd18799.md D1, D4, D9; spec/observations-forge.md):
// check.observed, the observer's record of what the forge's checks and
// review threads say about the submission under review, and
// contract.returned's second citation, a red observation beside a fail
// verdict. A seed/7 validator's unknown-verb arm fails a chain carrying
// the fact and its strict return decode refuses the citation, so the
// two judge a seed/8 record differently, which is the bump trigger;
// records at earlier positions keep their earlier judgment.
const Seed8 = "seed/8"

// Seed9 is the protocol version that restores the runtime-tuple
// semantics (plans/os-29e2fef2.md; spec/ranking.md "Resume"):
// tuple.Applies names seed/2 through seed/4 and was not extended when
// seed/5 through seed/8 were registered, so the binaries that shipped
// those versions admitted a tupleless grant, start and offer there.
// Those positions keep that judgment; from seed/9 a grant may cite a
// tuple, a start must declare one and an offer may scope by one again,
// which the resumption on the forge loop's chain needs. A seed/8
// validator strictly decodes a seed/9 tuple-bearing grant as
// {capability} and fails it, so the two judge a seed/9 record
// differently, which is the bump trigger.
const Seed9 = "seed/9"

// Supported lists the protocol versions this build verifies and appends:
// the genesis default plus every later version it implements. Verifiers
// and admission points seed their default supported sets from it, so a
// chain that upgrades to a version this build implements keeps verifying
// without per-caller configuration.
func Supported() []string {
	return []string{Protocol, Seed1, Seed2, Seed3, Seed4, Seed5, Seed6, Seed7, Seed8, Seed9}
}

// Activated reports whether the semantics seed/1 introduced (the actor
// keyring, the lifecycle fold, budgets, offers) are active at a record
// carrying version v: seed/1 and every later version this build
// registers. A named list, never an ordering, so a version this build
// has not registered activates nothing however it would sort
// (plans/os-8e53ffd9.md D8). tuple.Applies is the narrower gate for
// what seed/2 added on top.
func Activated(v string) bool {
	return v == Seed1 || v == Seed2 || v == Seed3 || v == Seed4 || v == Seed5 || v == Seed6 || v == Seed7 || v == Seed8 || v == Seed9
}

// EvalApplies reports whether the qualification verbs and the eval
// marker on intent.filed are defined at a record carrying version v:
// seed/3 and every later version this build registers, as a named
// list (plans/os-03e47abb.md D8; plans/os-99829835.md D4). It was an
// equality while seed/3 was the newest version, which memory/LEARNINGS.md
// predicted would close on seed/4; a version this build has not
// registered activates nothing however it would sort. It lives here
// rather than beside the eval package because the keyring and the fold
// gate on it and the eval derivation reads both.
func EvalApplies(v string) bool {
	return v == Seed3 || v == Seed4 || v == Seed5 || v == Seed6 || v == Seed7 || v == Seed8 || v == Seed9
}

// LevelsApply reports whether the independence levels and the verdict's
// declared tuple are defined at a record carrying version v: seed/4
// exactly (plans/os-99829835.md D4), the item 2 posture for a field a
// seed/3 validator's strict verdict decode would refuse. A later
// version joins this list when it is registered, never by ordering.
// The same gate carries what seed/4 added beside the levels: the
// contract.specified ready origin (re-specification) and the plan
// verbs' content digest (plans/os-6bd9ffff.md D4, D5, D7), each a
// row or field a seed/3 validator judges differently.
func LevelsApply(v string) bool {
	return v == Seed4 || v == Seed5 || v == Seed6 || v == Seed7 || v == Seed8 || v == Seed9
}

// ImportApplies reports whether system.imported is defined at a record
// carrying version v: seed/5 exactly today, a named list of one
// (plans/os-cf13fb51.md D2), so a version this build has not registered
// activates nothing however it would sort.
func ImportApplies(v string) bool {
	return v == Seed5 || v == Seed6 || v == Seed7 || v == Seed8 || v == Seed9
}

// RacingApplies reports whether racing mode's widened origins
// (claim.taken from in_progress, submission.made from review) are
// defined at a record carrying version v: seed/6, as a named list.
func RacingApplies(v string) bool { return v == Seed6 || v == Seed7 || v == Seed8 || v == Seed9 }

// RequestsApply reports whether the request ingress verbs are defined
// at a record carrying version v: seed/7 and later, as a named list.
func RequestsApply(v string) bool { return v == Seed7 || v == Seed8 || v == Seed9 }

// ForgeChecksApply reports whether the forge observation is defined at
// a record carrying version v: check.observed, the pr field on
// submission.made, and contract.returned's observation citation
// (plans/os-0cd18799.md D9). seed/8 exactly today, a named list of one.
func ForgeChecksApply(v string) bool { return v == Seed8 || v == Seed9 }
