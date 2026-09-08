package simulate

// The guardrail bar's ceiling arm (plans/os-b5051f2e.md D1, D2, D6):
// the ceiling is admission policy read from the declaration, never
// carried by the chain, so the audit judges it only under a
// declaration the caller gives, and then exactly as admission does.
// These chains carry a real genesis and roster so the keyring replays
// and a claim's signer has a kind to read.

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func rosterKey(t *testing.T, first byte) (pubHex, fp string) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	k := ed25519.NewKeyFromSeed(seed)
	pub := k.Public().(ed25519.PublicKey)
	f, err := event.Fingerprint(pub)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(pub), f
}

func recAt(v, verb, subject, actor, payload string) *event.Record {
	return &event.Record{Event: event.Event{V: v, Verb: verb, Subject: subject, Actor: actor, Payload: []byte(payload)}}
}

// rosterChain is a chain with a genesis naming a root, the upgrade to
// seed/1, and an agent-kind and a human-kind key enrolled with claim.
// The audit reads verbs, subjects, actors and payloads, never
// signatures, so the records after genesis are unsigned.
func rosterChain(t *testing.T) (records []*event.Record, root, agent, human string) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	gen, err := genesis.Build(ed25519.NewKeyFromSeed(seed), nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	root = gen.Event.Actor
	agentPub, agentFP := rosterKey(t, 31)
	humanPub, humanFP := rosterKey(t, 32)
	records = []*event.Record{
		gen,
		recAt(version.Protocol, ledger.UpgradeVerb, "system", root, `{"to": "`+version.Seed1+`"}`),
		recAt(version.Seed1, "actor.enrolled", agentFP, root, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "agent"}`, agentPub)),
		recAt(version.Seed1, "actor.granted", agentFP, root, `{"capability": "claim"}`),
		recAt(version.Seed1, "actor.enrolled", humanFP, root, fmt.Sprintf(`{"key": %q, "kind": "human", "name": "alice"}`, humanPub)),
		recAt(version.Seed1, "actor.granted", humanFP, root, `{"capability": "claim"}`),
	}
	return records, root, agentFP, humanFP
}

func filed(root, subject, tier, routing string) []*event.Record {
	return []*event.Record{
		recAt(version.Seed1, "intent.filed", subject, root, fmt.Sprintf(`{"intent": "x", "tier": %q, "budget": "small", "routing": %q}`, tier, routing)),
		recAt(version.Seed1, "contract.specified", subject, root, `{"acceptance": {"ref": "specs/x.md @ abc1234", "executable": false}}`),
	}
}

func declared(t *testing.T, raw string) *posture.Config {
	t.Helper()
	cfg, err := posture.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("declaration: %v", err)
	}
	return cfg
}

const standardCeiling = `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`

// conformance: III.R row 5 (zero guardrail breaches, every claim within
// its ceiling) — under a declaration the guardrail bar mirrors
// admission's ceiling rule arm for arm: an agent-kind key's claim on a
// contract above its squad's agent ceiling is a breach that names the
// subject, kind, key, tier, position, squad and ceiling; a human key,
// a claim at the ceiling, an undeclared squad, an absent guardrails
// block and no declaration are silence; a ceiling outside the tier
// vocabulary fails closed, as admission does (plans/os-b5051f2e.md
// D1, D2, AC1).
func TestCeilingArmMirrorsAdmission(t *testing.T) {
	base, root, agent, human := rosterChain(t)
	chain := func(tail ...*event.Record) []*event.Record {
		return append(append([]*event.Record(nil), base...), tail...)
	}
	above := chain(append(filed(root, "c-1", "critical", "core"), recAt(version.Seed1, ClaimTakenVerb, "c-1", agent, "{}"))...)
	pos := len(above) - 1

	a := AuditUnder(above, declared(t, standardCeiling))
	if len(a.GuardrailBreaches) != 1 {
		t.Fatalf("an agent above the ceiling is one breach: %+v", a.GuardrailBreaches)
	}
	want := fmt.Sprintf("c-1: agent key %s claimed a critical contract at position %d above the core squad's agent ceiling standard", agent, pos)
	if a.GuardrailBreaches[0] != want {
		t.Fatalf("the evidence names subject, kind, key, tier, position, squad and ceiling:\n%s\n%s", a.GuardrailBreaches[0], want)
	}
	if a.Clean {
		t.Fatal("a chain with a ceiling breach is not clean")
	}
	// No declaration: the records-only audit, byte for byte.
	if a := Audit(above); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("with no declaration nothing is ceilinged: %+v", a.GuardrailBreaches)
	}
	if a := AuditUnder(above, nil); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("a nil declaration is the records-only audit: %+v", a.GuardrailBreaches)
	}
	// A human key is not ceilinged.
	byHuman := chain(append(filed(root, "c-2", "critical", "core"), recAt(version.Seed1, ClaimTakenVerb, "c-2", human, "{}"))...)
	if a := AuditUnder(byHuman, declared(t, standardCeiling)); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("a human key is not ceilinged: %+v", a.GuardrailBreaches)
	}
	// A contract at the ceiling admits.
	atCeiling := chain(append(filed(root, "c-3", "standard", "core"), recAt(version.Seed1, ClaimTakenVerb, "c-3", agent, "{}"))...)
	if a := AuditUnder(atCeiling, declared(t, standardCeiling)); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("a contract at the ceiling is within it: %+v", a.GuardrailBreaches)
	}
	// An undeclared squad has no ceiling; an absent guardrails block
	// ceilings nobody.
	elsewhere := chain(append(filed(root, "c-4", "critical", "other"), recAt(version.Seed1, ClaimTakenVerb, "c-4", agent, "{}"))...)
	if a := AuditUnder(elsewhere, declared(t, standardCeiling)); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("an undeclared squad has no ceiling: %+v", a.GuardrailBreaches)
	}
	noBlock := declared(t, `{"posture": "cooperative", "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`)
	if a := AuditUnder(above, noBlock); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("no guardrails block, no ceiling: %+v", a.GuardrailBreaches)
	}
	// A ceiling outside the vocabulary fails closed, as at admission.
	bogus := declared(t, `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "sky"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`)
	a = AuditUnder(atCeiling, bogus)
	if len(a.GuardrailBreaches) != 1 || !strings.Contains(a.GuardrailBreaches[0], `whose agent ceiling "sky" is not a tier`) {
		t.Fatalf("a ceiling outside the vocabulary fails closed by name: %+v", a.GuardrailBreaches)
	}
	// A chain whose roster does not replay yields no kinds: the arm is
	// silent rather than guessing, admission's Keyring == nil posture.
	unrooted := append(filed(root, "c-5", "critical", "core"), recAt(version.Seed1, ClaimTakenVerb, "c-5", agent, "{}"))
	if a := AuditUnder(unrooted, declared(t, standardCeiling)); len(a.GuardrailBreaches) != 0 {
		t.Fatalf("no roster, no kind to judge: %+v", a.GuardrailBreaches)
	}
}

// conformance: III.R row 5 — an admission-grade chain audits clean
// under a declaration whose ceiling covers its tiers, and the cost of
// the reading is a number (plans/os-b5051f2e.md D6, AC2): one keyring
// replay and one fold, then a lookup per claim.
func TestAdmittedChainAuditsCleanUnderItsDeclaration(t *testing.T) {
	n := 3
	if !testing.Short() {
		n = 40
	}
	records := admittedChain(t, n)
	start := time.Now()
	a := AuditUnder(records, declared(t, standardCeiling))
	elapsed := time.Since(start)
	t.Logf("audited %d records under a declaration in %s", len(records), elapsed.Round(time.Millisecond))
	if len(a.GuardrailBreaches) != 0 {
		t.Fatalf("the history's agents claim trivial contracts under a standard ceiling: %v", a.GuardrailBreaches)
	}
	if !a.Clean {
		t.Fatalf("an admitted chain is clean under its declaration: %+v", a)
	}
}
