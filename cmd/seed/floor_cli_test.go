package main

// The path floor at the render (plans/os-0d4f2af3.md D3): a submission
// whose changed files touch a floored prefix while the contract sits
// below the floor is refused before any verdict is rendered, with the
// plan lint's words; without a declaration the render is what it was.

import (
	"crypto/ed25519"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: III.L row 1 — tiers per path at the verdict: the same
// floor the plan lint applies, applied to the receipt's inventory.
func TestVerdictRenderHoldsTheReceiptToThePathFloors(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head := verdictRepo(t)
	vkey, vpub, vfp := writeWorkerKey(t, 9)
	for _, step := range [][]string{
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed1 + `"}`},
		{"actor.enrolled", vfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, vpub)},
		{"actor.granted", vfp, `{"capability": "verdict"}`},
		{"intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{"contract.specified", "c-1", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s failed: %d %+v", step[0], code, e)
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)
	fencePos := verdictLibAppend(t, ld, rootKey, "claim.taken", "c-1", `{}`)
	verdictLibAppend(t, ld, rootKey, "submission.made", "c-1", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["c-1 verified"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fencePos, base+".."+head))

	// The submission changes hello.txt; a floor on it at critical, and
	// the contract is trivial.
	floored := writeDeclaration(t, `{"posture": "cooperative", "guardrails": {"paths": [{"prefix": "hello.txt", "min": "critical"}]}}`)
	e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src, "--key", vkey, "--verdict", "pass", "--config", floored)
	if code != 18 || e.Error == nil || e.Error.Code != "under_tiered" || !strings.Contains(e.Error.Message, "hello.txt needs critical, the contract is trivial") {
		t.Fatalf("a submission under a floor its tier falls short of refuses under_tiered, got %d %+v", code, e)
	}
	// A floor elsewhere does not bite; no declaration, no floor.
	elsewhere := writeDeclaration(t, `{"posture": "cooperative", "guardrails": {"paths": [{"prefix": "internal/admit", "min": "critical"}]}}`)
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src, "--key", vkey, "--verdict", "pass", "--config", elsewhere); code != 0 || !e.OK {
		t.Fatalf("a floor the submission does not touch does not bite: %d %+v", code, e)
	}
}
