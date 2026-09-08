package main

// The preseed at the terminal (plans/os-0d4f2af3.md D1, D2, D3, D5):
// one file bootstraps a deployment idempotently, drift refuses by name,
// the file is checked with no ledger, the plan lint holds a file scope
// to the path floors, and the doctor reports the blocks.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

func preseedFile(t *testing.T, root, protocol string, extra string) string {
	t.Helper()
	body := `{"posture": "cooperative", "protocol": "` + protocol + `", "governance": {"root": "` + root + `", "owners": ["@root"], "change_process": "pr+owner-review"}, "protected": ["spec", "internal/admit", "internal/transition", "internal/keyring", "internal/verdict", "internal/seal", "internal/eval", "evals", "internal/curation", "knowledge/lessons", "lanes", "cmd/seed-admit", "cmd/covergate", "Makefile", ".github/workflows", "scripts"], "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}, "paths": [{"prefix": "internal/admit", "min": "critical"}]}, "teams": {"squads": [{"name": "core", "lanes": ["implementer", "verifier"]}]}` + extra + `}`
	return writeDeclaration(t, body)
}

// conformance: III.P row 3 — one declarative file bootstraps a
// deployment idempotently and CI-verifiably: the first run writes
// genesis and the declared activations, the second appends nothing,
// and a declaration the chain contradicts refuses `preseed_drift`
// naming the field and both values with the chain untouched.
func TestInitPreseedIsIdempotentAndDriftRefuses(t *testing.T) {
	_, priv, _ := writeKeys(t)
	privBytes, _ := os.ReadFile(priv)
	signer, err := event.ParsePrivateKey(privBytes)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	cfg := preseedFile(t, fp, "seed/2", "")
	ledgerDir := filepath.Join(t.TempDir(), "ledger")

	e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", "../../lanes")
	if code != 0 || !e.OK || e.Result["unchanged"] != false || *e.Position != "2" {
		msg := ""
		if e.Error != nil {
			msg = e.Error.Message
		}
		t.Fatalf("the first run writes genesis and two activations: %d %+v %s", code, e, msg)
	}
	appended, _ := e.Result["appended"].([]any)
	if len(appended) != 3 || appended[0] != "system.genesis" || !strings.HasSuffix(appended[2].(string), "seed/2") {
		t.Fatalf("genesis, then seed/1 and seed/2 in order: %v", appended)
	}
	e, code = runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", "../../lanes")
	if code != 0 || e.Result["unchanged"] != true || *e.Position != "2" {
		t.Fatalf("the second run appends nothing: %d %+v", code, e)
	}
	e, code = runEnv(t, "preseed", "check", "--config", cfg, "--ledger", ledgerDir, "--lanes", "../../lanes")
	if code != 0 || !e.OK || *e.Position != "2" {
		t.Fatalf("check agrees with init: %d %+v", code, e)
	}

	// A higher protocol declared later: check names the pending
	// activation as drift, init appends exactly it.
	higher := preseedFile(t, fp, "seed/3", "")
	e, code = runEnv(t, "preseed", "check", "--config", higher, "--ledger", ledgerDir, "--lanes", "../../lanes")
	if code != 28 || e.Error.Code != "preseed_drift" || !strings.Contains(e.Error.Message, "seed/3") {
		t.Fatalf("a pending activation is drift the check names: %d %+v", code, e)
	}
	e, code = runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", higher, "--lanes", "../../lanes")
	if code != 0 || len(e.Result["appended"].([]any)) != 1 || *e.Position != "3" {
		t.Fatalf("init appends the one missing activation: %d %+v", code, e)
	}

	// A lower protocol than the chain's, and a different root: drift,
	// the chain untouched.
	for name, decl := range map[string]string{
		"lower protocol": preseedFile(t, fp, "seed/1", ""),
		"other root":     preseedFile(t, strings.Repeat("ab", 32), "seed/3", ""),
	} {
		e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", decl, "--lanes", "../../lanes")
		if code != 28 || e.Error == nil || e.Error.Code != "preseed_drift" || !strings.Contains(e.Error.Message, "the declaration says") {
			t.Fatalf("%s must refuse as drift naming both values: %d %+v", name, code, e)
		}
		if e2, _ := runEnv(t, "ledger", "verify", "--ledger", ledgerDir); e2.Result["count"].(float64) != 4 {
			t.Fatalf("%s: the chain is untouched by a refused preseed: %+v", name, e2)
		}
	}
	// A fresh ledger under a root the key is not: refused before genesis.
	other := preseedFile(t, strings.Repeat("cd", 32), "seed/1", "")
	empty := filepath.Join(t.TempDir(), "ledger")
	if e, code := runEnv(t, "init", "--ledger", empty, "--key", priv, "--preseed", other, "--lanes", "../../lanes"); code != 28 || e.Error.Code != "preseed_drift" {
		t.Fatalf("a root that is not the initializing key refuses: %d %+v", code, e)
	}
	if _, err := os.Stat(filepath.Join(empty, "HEAD")); err == nil {
		if e, _ := runEnv(t, "ledger", "verify", "--ledger", empty); e.OK && e.Result["count"].(float64) != 0 {
			t.Fatal("nothing is written under a refused root")
		}
	}
}

// conformance: III.L rows 1 and 2 — the declaration is checked with no
// ledger: an unknown tier, a lane that is no manifest, a guardrail for
// an undeclared squad, and a protected surface missing a required
// member each refuse by name; the fixture deployment's declaration
// passes, which is what the CI line asserts.
func TestPreseedCheckLintsTheFile(t *testing.T) {
	fp := strings.Repeat("ef", 32)
	if e, code := runEnv(t, "preseed", "check", "--config", preseedFile(t, fp, "seed/4", ""), "--lanes", "../../lanes"); code != 0 || !e.OK || e.Result["ledger"] != nil {
		t.Fatalf("a complete declaration lints clean with no ledger: %d %+v", code, e)
	}
	if e, code := runEnv(t, "preseed", "check", "--config", "../../fixtures/deployment/seed.json", "--lanes", "../../lanes"); code != 0 || !e.OK {
		t.Fatalf("the fixture deployment lints clean: %d %+v", code, e)
	}
	for name, body := range map[string]string{
		"unknown tier":       `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "huge", "max_agent": "standard"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`,
		"no such manifest":   `{"posture": "cooperative", "teams": {"squads": [{"name": "core", "lanes": ["wizard"]}]}}`,
		"undeclared squad":   `{"posture": "cooperative", "guardrails": {"squads": {"ops": {"default": "trivial", "max_agent": "trivial"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`,
		"incomplete surface": `{"posture": "cooperative", "protected": ["Makefile"]}`,
		"unknown protocol":   `{"posture": "cooperative", "protocol": "seed/10"}`,
	} {
		e, code := runEnv(t, "preseed", "check", "--config", writeDeclaration(t, body), "--lanes", "../../lanes")
		if code != 13 || e.Error == nil || e.Error.Code != "preseed_incomplete" {
			t.Errorf("%s must refuse preseed_incomplete, got %d %+v", name, code, e)
		}
	}
	for name, body := range map[string]string{
		"bad change process":  `{"posture": "cooperative", "governance": {"root": "x", "change_process": "vibes"}}`,
		"duplicate squad":     `{"posture": "cooperative", "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}, {"name": "core", "lanes": ["verifier"]}]}}`,
		"squad without lanes": `{"posture": "cooperative", "teams": {"squads": [{"name": "core", "lanes": []}]}}`,
	} {
		e, code := runEnv(t, "preseed", "check", "--config", writeDeclaration(t, body), "--lanes", "../../lanes")
		if code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" {
			t.Errorf("%s must refuse at parse, got %d %+v", name, code, e)
		}
	}
	if e, code := runEnv(t, "preseed", "audit"); code != 64 || e.Error.Code != "usage" {
		t.Fatalf("unknown subverb: %d %+v", code, e)
	}
}

// conformance: III.L row 1 — tiers per path: a plan whose file scope
// touches a floored prefix at a tier below the floor refuses
// `under_tiered`; at or above the floor it passes; with no declaration
// the lint is what it was.
func TestPlanLintHoldsTheScopeToThePathFloors(t *testing.T) {
	plan := filepath.Join(t.TempDir(), "plan.md")
	body := "# Plan: touch the boundary\n\n## Acceptance Criteria\n\n**Boundary set (new, shown working):**\n\n- the rule refuses\n\n**Retention set (existing, shown unharmed):**\n\n- every suite passes\n\n## Validation Commands\n\n- Boundary: go test ./internal/admit/...\n- Retention: make check\n\n## Expected diff shape\n\nOne rule, one drill.\n\n## File Scope\n\n- `internal/admit/admit.go`\n- `docs/progress.md`\n"
	if err := os.WriteFile(plan, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := preseedFile(t, strings.Repeat("ab", 32), "seed/4", "")
	e, code := runEnv(t, "plan", "lint", plan)
	if code != 0 || !e.OK {
		t.Fatalf("the plan lints as before with no declaration: %d %+v", code, e)
	}
	e, code = runEnv(t, "plan", "lint", plan, "--config", cfg, "--tier", "standard")
	if code != 18 || e.Error == nil || e.Error.Code != "under_tiered" || !strings.Contains(e.Error.Message, "internal/admit/admit.go needs critical") {
		t.Fatalf("below the floor refuses naming the path and tiers: %d %+v", code, e)
	}
	e, code = runEnv(t, "plan", "lint", plan, "--config", cfg, "--tier", "critical")
	if code != 0 || !e.OK || e.Result["tier"] != "critical" {
		t.Fatalf("at the floor passes: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "lint", plan, "--config", cfg); code != 64 || e.Error.Code != "usage" {
		t.Fatalf("--config needs --tier: %d %+v", code, e)
	}
}

// The doctor reports every preseed block as declared or undeclared.
func TestDoctorReportsThePreseedBlocks(t *testing.T) {
	cfg := preseedFile(t, strings.Repeat("ab", 32), "seed/4", "")
	e, code, _ := runEnvErr(t, "doctor", "--config", cfg)
	if code != 0 || !e.OK {
		t.Fatalf("doctor: %d %+v", code, e)
	}
	pre, _ := e.Result["preseed"].(map[string]any)
	if pre["protocol"] != "seed/4" || pre["governance"] != true || pre["guardrails"] != true || pre["teams"] != true {
		t.Fatalf("the doctor reports the blocks: %+v", e.Result)
	}
	var probe map[string]any
	_ = json.Unmarshal([]byte(`{}`), &probe)
}

// conformance: plans/os-0d4f2af3.md D2 and AC2, the review findings on
// the task PR — the no-write comparison reports an empty chain as
// drift; an explicitly empty protected surface is held to the required
// members; a guardrail squad with no teams block is undeclared; a
// forge-hosted declaration whose admission identity the chain has
// suspended is the posture the chain contradicts; and the comparison
// reads a chain verified from genesis, never a merely parsed one.
func TestPreseedCheckHoldsTheChainAndTheFile(t *testing.T) {
	_, priv, _ := writeKeys(t)
	fp := signerFingerprint(t, priv)
	cfg := preseedFile(t, fp, "seed/4", "")
	empty := filepath.Join(t.TempDir(), "ledger")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "preseed", "check", "--config", cfg, "--ledger", empty, "--lanes", "../../lanes"); code != 28 || e.Error == nil || e.Error.Code != "preseed_drift" || !strings.Contains(e.Error.Message, "empty") {
		t.Fatalf("an empty chain is drift, not a match: %d %+v", code, e)
	}
	for name, body := range map[string]string{
		"an explicitly empty surface":     `{"posture": "cooperative", "governance": {"root": "` + fp + `", "owners": ["@root"], "change_process": "pr+owner-review"}, "protected": []}`,
		"a guardrail squad with no teams": `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}}}`,
	} {
		e, code := runEnv(t, "preseed", "check", "--config", writeDeclaration(t, body), "--lanes", "../../lanes")
		if code != 13 || e.Error == nil || e.Error.Code != "preseed_incomplete" {
			t.Fatalf("%s refuses preseed_incomplete: %d %+v", name, code, e)
		}
	}
	// The contradicted posture: a chain that suspended the declared
	// admission identity.
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	if e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", "../../lanes"); code != 0 {
		t.Fatalf("init: %d %+v", code, e)
	}
	_, servicePub, serviceFP := writeWorkerKey(t, 41)
	for _, step := range [][3]string{
		{"actor.enrolled", serviceFP, fmt.Sprintf(`{"key": %q, "kind": "service", "name": "admission"}`, servicePub)},
		{"actor.suspended", serviceFP, `{"reason": "rotated"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ledgerDir, "--key", priv, "--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	forge := writeDeclaration(t, `{"posture": "enforced-forge-hosted", "protocol": "seed/4", "admission": {"endpoint": "http://127.0.0.1:1", "identity": "`+serviceFP+`"}}`)
	e, code := runEnv(t, "preseed", "check", "--config", forge, "--ledger", ledgerDir, "--lanes", "../../lanes")
	if code != 28 || e.Error == nil || e.Error.Code != "preseed_drift" || !strings.Contains(e.Error.Message, "posture") || !strings.Contains(e.Error.Message, "suspended") {
		t.Fatalf("a suspended admission identity contradicts the forge-hosted posture: %d %+v", code, e)
	}
	// A tampered chain is chain trouble before any comparison.
	segments, err := filepath.Glob(filepath.Join(ledgerDir, "segments", "*.jsonl"))
	if err != nil || len(segments) == 0 {
		t.Fatal("no segment")
	}
	b, err := os.ReadFile(segments[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(segments[0], []byte(strings.Replace(string(b), `"rotated"`, `"rotatex"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "preseed", "check", "--config", cfg, "--ledger", ledgerDir, "--lanes", "../../lanes"); code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" {
		t.Fatalf("a chain that does not verify is chain_invalid, never compared: %d %+v", code, e)
	}
	if e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", "../../lanes"); code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" {
		t.Fatalf("init --preseed never extends a chain that does not verify: %d %+v", code, e)
	}
}

// conformance: plans/os-0d4f2af3.md D3, the review findings — the
// floor reads a tier in the vocabulary (an unknown or absent tier
// would rank above every floor), scope tokens compare in canonical
// form, and a token that is not repository-relative fails closed.
func TestPlanLintTierAndScopeAreHeldToTheFloors(t *testing.T) {
	_, priv, _ := writeKeys(t)
	fp := signerFingerprint(t, priv)
	cfg := preseedFile(t, fp, "seed/4", "")
	plan := func(scope string) string {
		p := filepath.Join(t.TempDir(), "plan.md")
		body := "# plan\n\n## Boundary set\n\n- `TestX` (new, shown working)\n\n## Retention set\n\n- `TestY` (existing, shown unharmed)\n\n## Validation Commands\n\n- Boundary: `go test ./...`\n- Retention: `go test ./...`\n\n## Expected diff shape\n\n- one file\n\n## File Scope\n\n- " + scope + "\n"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if e, code := runEnv(t, "plan", "lint", plan("`internal/admit/admit.go`"), "--config", cfg); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("--config without --tier is usage, not a bypass: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "lint", plan("`internal/admit/admit.go`"), "--config", cfg, "--tier", "anything"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("an unknown tier is usage, not a bypass: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "lint", plan("`./internal/admit/admit.go`"), "--config", cfg, "--tier", "standard"); code != 18 || e.Error == nil || e.Error.Code != "under_tiered" {
		t.Fatalf("a ./-prefixed token is the same path and stays floored: %d %+v", code, e)
	}
	if e, code := runEnv(t, "plan", "lint", plan("`../elsewhere/admit.go`"), "--config", cfg, "--tier", "critical"); code != 18 || e.Error == nil || e.Error.Code != "under_tiered" || !strings.Contains(e.Error.Message, "not a repository-relative path") {
		t.Fatalf("a parent token fails closed: %d %+v", code, e)
	}
}

// conformance: the review finding on the task PR — a verb with no
// --config of its own still refuses a malformed default declaration
// rather than continuing without one.
func TestMalformedDefaultDeclarationRefusesEveryVerb(t *testing.T) {
	_, priv, _ := writeKeys(t)
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	if e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv); code != 0 {
		t.Fatalf("init: %d %+v", code, e)
	}
	t.Setenv("SEED_CONFIG", writeDeclaration(t, `{"posture": "anarchy"}`))
	if e, code := runEnv(t, "offer", "publish", "--ledger", ledgerDir, "--key", priv, "--subject", "c-1", "--expires", "2027-01-01T00:00:00Z"); code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" {
		t.Fatalf("offer publish honors the strict lookup: %d %+v", code, e)
	}
}
