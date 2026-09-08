package admit

// The declaration-driven policy rules (plans/os-0d4f2af3.md D3, D4):
// the agent claim ceiling reading the roster's kind, and routing held
// to the declared squads — admission policy, never chain validity.

import (
	"errors"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func declared(t *testing.T, body string) *posture.Config {
	t.Helper()
	cfg, err := posture.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const ceilingDeclaration = `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`

// conformance: III.E row 9 — the ceiling reads the roster's kind: an
// agent key refuses above its squad's ceiling, a human key claims the
// same contract, a service key is ceilinged, and with no declaration
// nothing is ceilinged.
func TestAgentCeilingReadsTheRosterKind(t *testing.T) {
	ctx, signer, worker, maintainer, step := grantFixture(t)
	human := fixtureKey(t, 7)
	ctx = step(signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, human), enrollBody(t, human, "human", "alice"))
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, maintainer), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, human), `{"capability": "`+keyring.CapClaim+`"}`)
	critical := `{"intent": "the big one", "tier": "critical", "budget": "small", "routing": "core"}`
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", critical)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)

	cfg := declared(t, ceilingDeclaration)
	with := *ctx
	with.Declaration = cfg
	// The agent (kind agent) above the ceiling refuses, by name.
	err := Check(&with, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip))
	var ce *CeilingError
	if !errors.As(err, &ce) || ce.Kind != "agent" || ce.Squad != "core" || ce.Tier != "critical" || ce.Ceiling != "standard" {
		t.Fatalf("an agent above the ceiling refuses naming kind, squad, tier and ceiling, got %v", err)
	}
	// The service key is ceilinged like an agent.
	if err := Check(&with, draftV(t, maintainer, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); !errors.As(err, &ce) || ce.Kind != "service" {
		t.Fatalf("a service key is ceilinged, got %v", err)
	}
	// The human is not.
	if err := Check(&with, draftV(t, human, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("a human key is not ceilinged: %v", err)
	}
	// No declaration: nothing is ceilinged — today's behavior.
	if err := Check(ctx, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("with no declaration the agent claims: %v", err)
	}
	// A contract at the ceiling admits; an undeclared squad is not
	// ceilinged.
	ctx = step(signer, version.Seed1, "intent.filed", "c-2", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-2", specBody)
	with = *ctx
	with.Declaration = cfg
	if err := Check(&with, draftV(t, worker, version.Seed1, "claim.taken", "c-2", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("a trivial contract is under the standard ceiling: %v", err)
	}
	// A declaration with no guardrails block ceilings nobody: absent
	// is undeclared, never a default.
	with.Declaration = declared(t, `{"posture": "cooperative", "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`)
	if err := Check(&with, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("no guardrails block, no ceiling: %v", err)
	}
	other := `{"intent": "elsewhere", "tier": "critical", "budget": "small", "routing": "other"}`
	noTeams := declared(t, `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}}}`)
	with.Declaration = noTeams
	ctx = step(signer, version.Seed1, "intent.filed", "c-3", other)
	ctx = step(signer, version.Seed1, "contract.specified", "c-3", specBody)
	with = *ctx
	with.Declaration = noTeams
	if err := Check(&with, draftV(t, worker, version.Seed1, "claim.taken", "c-3", `{}`, ctx.Tip)); err != nil {
		t.Fatalf("an undeclared squad has no ceiling: %v", err)
	}
}

// conformance: III.J — routing is validated against the declared
// squads as policy: an unknown squad refuses under the declaration and
// admits without one, and a chain carrying it verifies either way.
func TestRoutingIsHeldToTheDeclaredSquads(t *testing.T) {
	ctx, signer, _, _, _ := grantFixture(t)
	cfg := declared(t, ceilingDeclaration)
	with := *ctx
	with.Declaration = cfg
	other := `{"intent": "elsewhere", "tier": "trivial", "budget": "small", "routing": "other"}`
	err := Check(&with, draftV(t, signer, version.Seed1, "intent.filed", "c-9", other, ctx.Tip))
	var re *RoutingError
	if !errors.As(err, &re) || re.Routing != "other" || len(re.Squads) != 1 || re.Squads[0] != "core" {
		t.Fatalf("an undeclared squad refuses naming the declared ones, got %v", err)
	}
	if err := Check(&with, draftV(t, signer, version.Seed1, "intent.filed", "c-9", filedBody, ctx.Tip)); err != nil {
		t.Fatalf("a declared squad admits: %v", err)
	}
	if err := Check(ctx, draftV(t, signer, version.Seed1, "intent.filed", "c-9", other, ctx.Tip)); err != nil {
		t.Fatalf("with no declaration any squad admits: %v", err)
	}
	// Policy, not validity: the context with a declaration was built
	// over a chain that admitted an "other" routing before, and it
	// still builds.
	built, err := ContextOver(ctx.Records, WithDeclaration(cfg))
	if err != nil || built.Declaration != cfg {
		t.Fatalf("a declaration never makes a chain fail to build a context, and rides it: %v", err)
	}
}

// conformance: the review finding on the task PR — a ceiling outside
// the tier vocabulary fails closed for agent keys rather than ranking
// above every tier and admitting every claim.
func TestUnknownCeilingFailsClosed(t *testing.T) {
	ctx, signer, worker, _, step := grantFixture(t)
	ctx = step(signer, version.Seed1, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	ctx = step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	with := *ctx
	with.Declaration = declared(t, `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standrad"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`)
	err := Check(&with, draftV(t, worker, version.Seed1, "claim.taken", "c-1", `{}`, ctx.Tip))
	var ce *CeilingError
	if !errors.As(err, &ce) || !ce.Unknown || ce.Ceiling != "standrad" {
		t.Fatalf("a misspelled ceiling refuses the agent's claim, naming the ceiling, got %v", err)
	}
}
