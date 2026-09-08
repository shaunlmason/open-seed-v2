package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/history"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// auditLedger initialises a ledger and raw-appends the verbs for one
// subject with the governance root's key through the library, past
// admission on purpose (the dev tool self-validates, as the
// cooperative posture must): the audit reads what the chain holds,
// and a bar is a claim about the chain, not about what the boundary
// would have admitted. The root signs so the chain still verifies.
func auditLedger(t *testing.T, verbs ...string) (string, string) {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if e, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 || !e.OK {
		t.Fatalf("init: %d %+v", code, e)
	}
	// The chain speaks seed/1 before any lifecycle record, as every
	// deployment does; the upgrade goes through the dev tool.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+version.Seed1+`"}`); code != 0 || !e.OK {
		t.Fatalf("upgrade: %d %+v", code, e)
	}
	for _, v := range verbs {
		rawAppend(t, ld, fixturePriv(t), v, "c-1", `{}`)
	}
	return ld, priv
}

// happyPath drives one subject to a deliberate exit. Every record is
// raw-appended with a {} payload, so its run.started cites no
// reservation the fold can read and the unreserved-spend bar names it
// (plans/os-88df7ab2.md D7): a start nothing fenced is spend nothing
// fenced, whatever verbs precede it. The covered arm is
// TestAdmittedChainAuditsCleanThroughTheVerb, which audits a chain
// admission actually took.
var happyPath = []string{"intent.filed", "contract.specified", "offer.published", "claim.taken", "budget.reserve", "run.started", "submission.made"}

// conformance: III.R row 5 — the five-bar audit over any ledger
// (plans/os-7599c27d.md AC1, AC2). A subject driven cleanly to a
// deliberate exit audits clean, every list empty, stamped at the tip;
// each plantable violation refuses 28 audit_violated naming its bar
// and the record.
func TestLedgerAuditCleanAndEachBar(t *testing.T) {
	ld, _ := auditLedger(t, happyPath...)
	e, code := runEnv(t, "ledger", "audit", "--ledger", ld)
	// The shape-reading bars stay quiet on this chain; the
	// unreserved-spend bar names its unfenced start (D7), so the verb
	// refuses and the refusal names that bar alone.
	if code != 28 || e.Error == nil || e.Error.Code != "audit_violated" {
		t.Fatalf("a raw start that cites no reservation is refused: %d %+v", code, e)
	}
	if !strings.Contains(e.Error.Message, "unreserved_spend [c-1]") {
		t.Fatalf("the refusal names the unfenced start: %q", e.Error.Message)
	}
	for _, bar := range []string{"chain_violations", "lost_updates", "silent_abandonments", "guardrail_breaches"} {
		if strings.Contains(e.Error.Message, bar) {
			t.Errorf("%s must stay quiet on this chain: %q", bar, e.Error.Message)
		}
	}
	if e.Position == nil || *e.Position != "8" {
		t.Fatalf("the audit is stamped at the tip it audited: %+v", e)
	}

	cases := []struct {
		name  string
		verbs []string
		bar   string
	}{
		{"an unclosed window", []string{"intent.filed", "contract.specified", "offer.published", "claim.taken", "budget.reserve"}, "silent_abandonments"},
		{"an unreserved run", []string{"intent.filed", "contract.specified", "offer.published", "claim.taken", "run.started", "submission.made"}, "unreserved_spend"},
		// The guardrail bar's case is the sealed author claiming the
		// subject it sealed, which admission refuses; an unoffered
		// claim is not a breach, because admission takes one
		// (plans/os-aaec6a3c.md D1). The raw appends here carry no
		// readable seal, so that arm is drilled at the library in
		// internal/simulate, and this table keeps the bars a raw chain
		// can show.
		{"an illegal transition", []string{"claim.taken", "intent.filed"}, "chain_violations"},
	}
	for _, c := range cases {
		ld, _ := auditLedger(t, c.verbs...)
		e, code := runEnv(t, "ledger", "audit", "--ledger", ld)
		if code != 28 || e.Error == nil || e.Error.Code != "audit_violated" {
			t.Fatalf("%s: must refuse 28 audit_violated, got %d %+v", c.name, code, e)
		}
		if !strings.Contains(e.Error.Message, c.bar) {
			t.Errorf("%s: the message names the bar %s: %q", c.name, c.bar, e.Error.Message)
		}
		if c.bar != "chain_violations" && !strings.Contains(e.Error.Message, "c-1") {
			t.Errorf("%s: the message names the record: %q", c.name, e.Error.Message)
		}
		if e.Position == nil {
			t.Errorf("%s: the refusal is stamped at the tip audited", c.name)
		}
	}
}

// conformance: plans/os-7599c27d.md AC3 — verification comes first: a
// corrupted segment and a ledger with no genesis both refuse
// chain_invalid with no audit fields, and a usage slip is a usage
// refusal.
func TestLedgerAuditVerifiesBeforeAnyBar(t *testing.T) {
	ld, _ := auditLedger(t, "intent.filed")
	segs, err := filepath.Glob(filepath.Join(ld, "segments", "*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("segments: %v %v", segs, err)
	}
	b, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	corrupted := strings.Replace(string(b), `"subject":"c-1"`, `"subject":"c-9"`, 1)
	if corrupted == string(b) {
		t.Fatal("corruption did not apply")
	}
	if err := os.WriteFile(segs[0], []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "ledger", "audit", "--ledger", ld)
	if code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" || e.Result != nil {
		t.Fatalf("a corrupted chain refuses chain_invalid before any bar: %d %+v", code, e)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	e, code = runEnv(t, "ledger", "audit", "--ledger", empty)
	if code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" {
		t.Fatalf("a ledger with no genesis refuses chain_invalid: %d %+v", code, e)
	}
	if _, code = runEnv(t, "ledger", "audit"); code != 64 {
		t.Fatalf("audit without --ledger is a usage refusal, got %d", code)
	}
	if e, code := runEnv(t, "ledger", "audit", "--ledger", ld, "--supported", "seed/9"); code != 10 || e.Error == nil {
		t.Fatalf("the supported-version discipline is verify's: %d %+v", code, e)
	}
}

// conformance: plans/os-7599c27d.md D6 — a refusal naming several
// records names them in one order on every run. The audit fills the
// silent-abandonment list from map iteration; three windows left
// open, appended in reverse, come back sorted five times over.
func TestLedgerAuditRefusalOrderIsDeterministic(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if e, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 || !e.OK {
		t.Fatalf("init: %d %+v", code, e)
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+version.Seed1+`"}`); code != 0 || !e.OK {
		t.Fatalf("upgrade: %d %+v", code, e)
	}
	for _, subject := range []string{"c-3", "c-2", "c-1"} {
		for _, v := range []string{"intent.filed", "contract.specified", "offer.published", "claim.taken"} {
			rawAppend(t, ld, fixturePriv(t), v, subject, `{}`)
		}
	}
	for i := 0; i < 5; i++ {
		e, code := runEnv(t, "ledger", "audit", "--ledger", ld)
		if code != 28 || e.Error == nil || e.Error.Code != "audit_violated" {
			t.Fatalf("run %d: must refuse 28 audit_violated, got %d %+v", i, code, e)
		}
		msg := e.Error.Message
		if !strings.Contains(msg, "silent_abandonments") || !(strings.Index(msg, "c-1") < strings.Index(msg, "c-2") && strings.Index(msg, "c-2") < strings.Index(msg, "c-3")) {
			t.Fatalf("run %d: the records are named in one order: %q", i, msg)
		}
	}
}

// conformance: plans/os-88df7ab2.md D1, D7 — the covered arm through
// the verb. A chain admission actually took carries a fence and a
// cited reservation on every start, so the unreserved-spend bar names
// nobody. The guardrail bar does name subjects here, because the
// history stages a claim without publishing an offer and that bar
// requires one while admission does not; the disagreement is carded
// on its own and this drill asserts only the bar this card moves.
func TestAdmittedChainAuditsCleanThroughTheVerb(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ledger")
	if _, err := history.Generate(history.Spec{Seed: 11, Contracts: 2, Dir: dir}); err != nil {
		t.Fatalf("generating an admitted chain: %v", err)
	}
	e, code := runEnv(t, "ledger", "audit", "--ledger", dir)
	if code == 0 {
		if list, ok := e.Result["unreserved_spend"].([]any); !ok || len(list) != 0 {
			t.Fatalf("an admitted chain's runs are fenced: %v", e.Result["unreserved_spend"])
		}
		return
	}
	if e.Error == nil || e.Error.Code != "audit_violated" {
		t.Fatalf("an admitted chain audits or refuses by a bar, not by an error: %d %+v", code, e)
	}
	if strings.Contains(e.Error.Message, "unreserved_spend") {
		t.Fatalf("an admitted chain's runs cite open reservations: %q", e.Error.Message)
	}
}

// ceilingDeclaration is a deployment whose core squad ceilings agents
// at standard; the chain below claims a critical contract with an
// agent-kind key.
const ceilingDeclaration = `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`

// conformance: III.R row 5 (every claim within its ceiling) — the
// verb reads the declaration by the remote verbs' shared lookup and
// judges the guardrail bar's ceiling arm under it (plans/os-b5051f2e.md
// D3, AC3): --config turns a clean reading into audit_violated naming
// the agent's claim above the ceiling, the same chain without a
// declaration is clean, a reading names the declaration it was judged
// under or null, $SEED_CONFIG is honored, and a declaration that
// exists and does not parse refuses posture_invalid before any bar.
func TestLedgerAuditReadsTheDeclaration(t *testing.T) {
	ld, priv := auditLedger(t)
	agentKey := workerRawKey(61)
	agentPub := agentKey.Public().(ed25519.PublicKey)
	agentFP, err := event.Fingerprint(agentPub)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct{ verb, subject, payload string }{
		{"actor.enrolled", agentFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "agent"}`, hex.EncodeToString(agentPub))},
		{"actor.granted", agentFP, `{"capability": "claim"}`},
		{"intent.filed", "c-1", `{"intent": "the big one", "tier": "critical", "budget": "small", "routing": "core"}`},
		{"contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", s.verb, "--subject", s.subject, "--payload", s.payload); code != 0 || !e.OK {
			t.Fatalf("%s: %d %+v", s.verb, code, e)
		}
	}
	// The claim and its deliberate exit land raw under the agent's key:
	// admission under the declaration would have refused the claim, and
	// a raw push past the boundary is the case the bar exists for.
	claimPos := rawAppend(t, ld, agentKey, "claim.taken", "c-1", `{}`)
	rawAppend(t, ld, agentKey, "claim.released", "c-1", `{}`)

	// No declaration: the records-only reading, clean, naming none.
	e, code := runEnv(t, "ledger", "audit", "--ledger", ld)
	if code != 0 || !e.OK || e.Result["declaration"] != nil {
		t.Fatalf("with no declaration the chain is clean and the reading names none: %d %+v", code, e)
	}

	// Under the declaration the same chain breaks the guardrail bar,
	// by name, and no other bar.
	decl := writeDeclaration(t, ceilingDeclaration)
	e, code = runEnv(t, "ledger", "audit", "--ledger", ld, "--config", decl)
	if code != 28 || e.Error == nil || e.Error.Code != "audit_violated" {
		t.Fatalf("the agent's claim above the ceiling is refused under the declaration: %d %+v", code, e)
	}
	want := fmt.Sprintf("guardrail_breaches [c-1: agent key %s claimed a critical contract at position %d above the core squad's agent ceiling standard]", agentFP, claimPos)
	if !strings.Contains(e.Error.Message, want) {
		t.Fatalf("the refusal names the subject, kind, key, tier, position, squad and ceiling:\n%s\n%s", e.Error.Message, want)
	}
	for _, bar := range []string{"chain_violations", "lost_updates", "silent_abandonments", "unreserved_spend"} {
		if strings.Contains(e.Error.Message, bar) {
			t.Errorf("%s stays quiet on this chain: %q", bar, e.Error.Message)
		}
	}

	// A ceiling that covers the tier reads clean and names the
	// declaration it was judged under.
	wide := writeDeclaration(t, strings.Replace(ceilingDeclaration, `"max_agent": "standard"`, `"max_agent": "critical"`, 1))
	e, code = runEnv(t, "ledger", "audit", "--ledger", ld, "--config", wide)
	if code != 0 || !e.OK || e.Result["declaration"] != wide {
		t.Fatalf("a covering ceiling reads clean and the reading names its declaration: %d %+v", code, e)
	}

	// $SEED_CONFIG is the shared lookup's second step.
	t.Setenv("SEED_CONFIG", decl)
	e, code = runEnv(t, "ledger", "audit", "--ledger", ld)
	if code != 28 || e.Error == nil || !strings.Contains(e.Error.Message, "guardrail_breaches [c-1: agent key") {
		t.Fatalf("$SEED_CONFIG is honored: %d %+v", code, e)
	}
	t.Setenv("SEED_CONFIG", "")

	// A declaration that exists and does not parse refuses before any
	// bar is read, as the remote verbs refuse before any transport.
	bad := writeDeclaration(t, `{"posture": "nonsense"}`)
	e, code = runEnv(t, "ledger", "audit", "--ledger", ld, "--config", bad)
	if code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" || e.Result != nil {
		t.Fatalf("a malformed declaration refuses posture_invalid before any bar: %d %+v", code, e)
	}
}
