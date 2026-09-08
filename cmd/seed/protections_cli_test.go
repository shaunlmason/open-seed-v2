package main

// `seed protections plan | apply` and the doctor's forge-hosted flags
// at the terminal (plans/os-5c8a312c.md D6, D7).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/protections"
)

func runEnvErr(t *testing.T, args ...string) (ledgerEnv, int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	var e ledgerEnv
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, out.String())
	}
	return e, code, errOut.String()
}

func writeForgeSnapshot(t *testing.T, st protections.State) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "forge.json")
	b, _ := json.Marshal(st)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const forgeDeclaration = `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://127.0.0.1:1", "identity": "app:4242", "checks": ["check"], "reviews": 1, "owners": ["@root"]}, "protected": ["spec/", "Makefile"]}`

// conformance: III.L — protections are declared desired state
// reconciled by command: plan names the drift and exits 28, apply
// performs it and re-reads clean with CODEOWNERS written, and the
// plan refuses to run for any posture but the forge-hosted one.
func TestProtectionsPlanAndApply(t *testing.T) {
	cfg := writeDeclaration(t, forgeDeclaration)
	repo := t.TempDir()
	snap := writeForgeSnapshot(t, protections.State{DefaultBranch: "main", Unexpressible: []string{protections.RuleUpdate}})

	e, code, errOut := runEnvErr(t, "protections", "plan", "--config", cfg, "--snapshot", snap, "--repo", repo)
	if code != 28 || e.Error == nil || e.Error.Code != "protections_drift" || !strings.Contains(e.Error.Message, "create seed-ledger") || !strings.Contains(e.Error.Message, "CODEOWNERS drift") {
		t.Fatalf("drift exits 28 naming each difference, got %d %+v", code, e)
	}
	if !strings.Contains(errOut, "manual seed-ledger") || !strings.Contains(errOut, "by hand") {
		t.Fatalf("a rule the forge cannot express is named as manual work on stderr, got %q", errOut)
	}

	e, code, _ = runEnvErr(t, "protections", "apply", "--config", cfg, "--snapshot", snap, "--repo", repo)
	if code != 0 || !e.OK || e.Result["drift"].(float64) != 0 || e.Result["manual"].(float64) != 2 || e.Result["applied"] != true || e.Result["codeowners"] != "clean" {
		t.Fatalf("apply re-reads clean with the manual rules still named, got %d %+v", code, e)
	}
	own, err := os.ReadFile(filepath.Join(repo, protections.CodeownersPath))
	if err != nil || !strings.Contains(string(own), "/spec @root") || !strings.Contains(string(own), "/seed.json @root") {
		t.Fatalf("CODEOWNERS is written for a reviewed PR: %q %v", own, err)
	}
	e, code, _ = runEnvErr(t, "protections", "plan", "--config", cfg, "--snapshot", snap, "--repo", repo)
	if code != 0 || !e.OK || e.Result["default_branch"] != "main" {
		t.Fatalf("a clean forge plans clean at 0, got %d %+v", code, e)
	}

	// Refusals: the wrong posture, a missing snapshot, an unknown forge,
	// a missing declaration, and the github forge without a token.
	coop := writeDeclaration(t, `{"posture": "cooperative"}`)
	if e, code, _ := runEnvErr(t, "protections", "plan", "--config", coop, "--snapshot", snap); code != 13 || e.Error.Code != "posture_invalid" {
		t.Fatalf("protections reconcile the forge-hosted posture only, got %d %+v", code, e)
	}
	if e, code, _ := runEnvErr(t, "protections", "plan", "--config", cfg); code != 64 || e.Error.Code != "usage" {
		t.Fatalf("the snapshot forge needs its file, got %d %+v", code, e)
	}
	if e, code, _ := runEnvErr(t, "protections", "plan", "--config", cfg, "--forge", "gitea"); code != 64 || e.Error.Code != "usage" {
		t.Fatalf("an unknown forge is a usage error, got %d %+v", code, e)
	}
	if e, code, _ := runEnvErr(t, "protections", "plan", "--config", filepath.Join(t.TempDir(), "absent.json"), "--snapshot", snap); code != 4 || e.Error.Code != "posture_undeclared" {
		t.Fatalf("no declaration, no plan, got %d %+v", code, e)
	}
	t.Setenv("SEED_TEST_NO_TOKEN", "")
	if e, code, _ := runEnvErr(t, "protections", "plan", "--config", cfg, "--forge", "github", "--github", "o/r", "--token-env", "SEED_TEST_NO_TOKEN"); code != 5 || e.Error.Code != "unavailable" || !strings.Contains(e.Error.Message, "SEED_TEST_NO_TOKEN") {
		t.Fatalf("the github forge without a token is unavailable by name, got %d %+v", code, e)
	}
	if e, code, _ := runEnvErr(t, "protections", "audit"); code != 64 || e.Error.Code != "usage" {
		t.Fatalf("unknown subverb, got %d %+v", code, e)
	}
}

// conformance: III.B — the doctor reports the forge-hosted deployment
// it can see: the service's health when asked to probe (unavailable by
// name when nothing answers), and protections drift against a
// snapshot, exiting 28 so a preflight can gate on it.
func TestDoctorProbesAndReportsDrift(t *testing.T) {
	remote := forgeRemote(t)
	endpoint := startForgeService(t, remote)
	seedForgeGenesis(t, endpoint)
	cfg := writeDeclaration(t, `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "`+endpoint+`", "identity": "`+forgeIdentity+`", "checks": ["check"], "reviews": 1, "owners": ["@root"]}}`)

	e, code, _ := runEnvErr(t, "doctor", "--config", cfg, "--probe")
	if code != 0 || !e.OK {
		t.Fatalf("the probe against a live service passes: %d %+v", code, e)
	}
	svc, _ := e.Result["service"].(map[string]any)
	if svc["remote"] != remote || svc["position"] != float64(0) || svc["tip"] != forgeTip(t, remote) {
		t.Fatalf("the probe reports what the service serves and last admitted, got %+v", e.Result)
	}
	dead := writeDeclaration(t, forgeDeclaration)
	if e, code, _ := runEnvErr(t, "doctor", "--config", dead, "--probe"); code != 5 || e.Error.Code != "unavailable" || !strings.Contains(e.Error.Message, "127.0.0.1:1") {
		t.Fatalf("a dead service fails the probe by name, got %d %+v", code, e)
	}

	snap := writeForgeSnapshot(t, protections.State{DefaultBranch: "main"})
	e, code, errOut := runEnvErr(t, "doctor", "--config", dead, "--current", snap)
	if code != 28 || e.Error.Code != "protections_drift" || !strings.Contains(errOut, "seed protections plan") {
		t.Fatalf("drift against the snapshot exits 28 and names the verb, got %d %+v %q", code, e, errOut)
	}
	if e, code, _ := runEnvErr(t, "protections", "apply", "--config", dead, "--snapshot", snap); code != 0 {
		t.Fatalf("apply: %d %+v", code, e)
	}
	e, code, _ = runEnvErr(t, "doctor", "--config", dead, "--current", snap)
	if code != 0 || !e.OK || e.Result["protections"].(map[string]any)["drift"] != float64(0) {
		t.Fatalf("a reconciled forge is clean at the doctor, got %d %+v", code, e)
	}
}

func TestProtectionsForgejoNeedsInstanceAndToken(t *testing.T) {
	// A forgejo declaration name its instance; the CLI needs the api and
	// a token, refusing clearly when either is absent.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "seed.json")
	if err := os.WriteFile(cfg, []byte(`{"posture": "enforced-forge-hosted", "admission": {"endpoint": "https://admit.example", "identity": "seed-bot", "checks": ["verify"], "reviews": 1, "forge": "forgejo", "api": "https://forge.example"}, "protected": ["Makefile"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// No token in $FORGEJO_TOKEN → unavailable, naming the env var.
	e, code, _ := runEnvErr(t, "protections", "plan", "--config", cfg, "--forge", "forgejo", "--github", "o/r", "--token-env", "SEED_TEST_NO_FORGEJO")
	if code != 5 || e.Error == nil || e.Error.Code != "unavailable" || !strings.Contains(e.Error.Message, "SEED_TEST_NO_FORGEJO") {
		t.Fatalf("forgejo without a token must be unavailable naming the env var, got %d %+v", code, e.Error)
	}
	// The doctor reports the declared forge.
	d, code := runEnv(t, "doctor", "--config", cfg)
	if code != 0 {
		t.Fatalf("doctor must read a forgejo declaration, got %d", code)
	}
	adm, _ := d.Result["admission"].(map[string]any)
	if adm["forge"] != "forgejo" || adm["api"] != "https://forge.example" {
		t.Fatalf("doctor must report the declared forge and api, got %+v", adm)
	}
}
