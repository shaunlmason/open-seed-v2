package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func writeKeys(t *testing.T) (dir, priv, pub string) {
	t.Helper()
	dir = t.TempDir()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	k := ed25519.NewKeyFromSeed(seed)
	block, err := ssh.MarshalPrivateKey(k, "init-fixture")
	if err != nil {
		t.Fatal(err)
	}
	priv = filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(priv, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	seed[0] = 9
	k2 := ed25519.NewKeyFromSeed(seed)
	sshPub, err := ssh.NewPublicKey(k2.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	pub = filepath.Join(dir, "op.pub")
	if err := os.WriteFile(pub, ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, priv, pub
}

// conformance: III.A — a genesis event names the governance root and
// protocol version; init refuses non-empty ledgers; the response is a
// position-stamped envelope.
func TestInitVerb(t *testing.T) {
	dir, priv, pub := writeKeys(t)
	ledgerDir := filepath.Join(dir, "ledger")

	var out, errOut bytes.Buffer
	code := run([]string{"init", "--ledger", ledgerDir, "--key", priv, "--operator", pub}, &out, &errOut)
	if code != 0 {
		t.Fatalf("init failed: %d %s%s", code, out.String(), errOut.String())
	}
	var env struct {
		OK       bool           `json:"ok"`
		Result   map[string]any `json:"result"`
		Position *string        `json:"position"`
		Exit     int            `json:"exit"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.Exit != 0 {
		t.Fatalf("init envelope not ok: %s", out.String())
	}
	if env.Position == nil || *env.Position != "0" {
		t.Fatalf("init must stamp position 0, got %v", env.Position)
	}
	if env.Result["protocol"] != "seed/0" {
		t.Fatalf("genesis protocol = %v", env.Result["protocol"])
	}
	if roots, ok := env.Result["governance_root"].([]any); !ok || len(roots) != 2 {
		t.Fatalf("governance_root should name signer + operator, got %v", env.Result["governance_root"])
	}

	out.Reset()
	code = run([]string{"init", "--ledger", ledgerDir, "--key", priv}, &out, &errOut)
	if code != 3 {
		t.Fatalf("re-init must exit 3 (invalid transition), got %d: %s", code, out.String())
	}
	var refuse struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &refuse); err != nil {
		t.Fatal(err)
	}
	if refuse.Error.Code != "ledger_not_empty" {
		t.Fatalf("re-init error code = %q", refuse.Error.Code)
	}
	var refuseEnv struct {
		Position *string `json:"position"`
	}
	if err := json.Unmarshal(out.Bytes(), &refuseEnv); err != nil {
		t.Fatal(err)
	}
	if refuseEnv.Position == nil || *refuseEnv.Position != "0" {
		t.Fatalf("ledger-aware refusal must stamp the observed position, got %v", refuseEnv.Position)
	}
}

func TestInitUsageRefusals(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	cases := [][]string{
		{"init"},
		{"init", "--ledger", filepath.Join(dir, "x")},
		{"init", "--key", priv},
		{"init", "--ledger", filepath.Join(dir, "x"), "--key", filepath.Join(dir, "missing")},
		{"init", "--ledger", filepath.Join(dir, "x"), "--key", priv, "extra-operand"},
		{"init", "--bogus"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 64 {
			t.Errorf("%v must exit 64, got %d: %s", args, code, out.String())
		}
	}
}
