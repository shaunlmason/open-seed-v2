package main

// The reconcile verb end-to-end (plans/os-6cdc15be.md): the full chain
// reconciles clean; the induced divergences each surface by class —
// submit-old-head/merge-new-tip as attested_divergence, the
// force-moved target as target_rewritten, a lost receipt as
// evidence_missing, and raw-pushed skipped links as the
// record-derivable classes; detection is a report at exit 0.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// chainTo drives one contract from filing through a rendered pass
// verdict via the CLI, then requests and observes the merge through
// the library (admitted history is what the drill needs). Returns the
// receipt digest the verdict cited.
func chainTo(t *testing.T, ld, priv, src, vkey string, rootKey ed25519.PrivateKey, subject, specCommit, rng, mergedSHA string) string {
	t.Helper()
	for _, step := range [][]string{
		{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", subject, "--payload", step[1]); code != 0 {
			t.Fatalf("%s %s: %d %+v", subject, step[0], code, e)
		}
	}
	fencePos := verdictLibAppend(t, ld, rootKey, "claim.taken", subject, `{}`)
	verdictLibAppend(t, ld, rootKey, "submission.made", subject, fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["%s ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fencePos, subject, rng))
	e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", subject, "--repo", src,
		"--key", vkey, "--verdict", "pass")
	if code != 0 || !e.OK {
		t.Fatalf("render on %s: %d %+v", subject, code, e)
	}
	digest, _ := e.Result["receipt"].(string)
	// stampTip publishes the appended record's own position.
	verdictPos, err := strconv.Atoi(*e.Position)
	if err != nil {
		t.Fatal(err)
	}
	verdictLibAppend(t, ld, rootKey, "merge.requested", subject, fmt.Sprintf(`{"verdict": "%d"}`, verdictPos))
	if mergedSHA != "" {
		verdictLibAppend(t, ld, rootKey, "merge.observed", subject, fmt.Sprintf(`{"merged": %q, "pr": "pr/7"}`, mergedSHA))
	}
	return digest
}

func classesOf(t *testing.T, e ledgerEnv) map[string]float64 {
	t.Helper()
	by, _ := e.Result["by_class"].(map[string]any)
	out := map[string]float64{}
	for k, v := range by {
		f, _ := v.(float64)
		out[k] = f
	}
	return out
}

func TestReconcileEndToEndCLI(t *testing.T) {
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
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)
	rng := base + ".." + head

	// c-1: the clean fast-forward chain (observed == attested head).
	digest := chainTo(t, ld, priv, src, vkey, rootKey, "c-1", specCommit, rng, head)
	e, code := runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-1")
	if code != 0 || e.Result["clean"] != "true" {
		t.Fatalf("the full chain reconciles clean: %d %+v", code, e)
	}

	// c-2: submit-old-head/merge-new-tip — the observation names a
	// commit that is not the attested head nor its descendant.
	chainTo(t, ld, priv, src, vkey, rootKey, "c-2", specCommit, rng, specCommit)
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-2")
	if code != 0 || classesOf(t, e)["attested_divergence"] != 1 {
		t.Fatalf("an observation off the attested head surfaces attested_divergence: %+v", e.Result)
	}

	// c-3: verdict with no observed merge yet — neutral unreconciled.
	chainTo(t, ld, priv, src, vkey, rootKey, "c-3", specCommit, rng, "")
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-3")
	if classesOf(t, e)["unreconciled"] != 1 {
		t.Fatalf("a stalled chain surfaces unreconciled: %+v", e.Result)
	}

	// c-4: a raw-pushed observation with no verdict at all.
	verdictLibAppend(t, ld, rootKey, "intent.filed", "c-4", `{"intent": "raw", "tier": "trivial", "budget": "s", "routing": "core"}`)
	verdictLibAppend(t, ld, rootKey, "contract.specified", "c-4", `{"acceptance": {"ref": "accept.md @ `+specCommit+`", "executable": false}}`)
	fencePos := verdictLibAppend(t, ld, rootKey, "claim.taken", "c-4", `{}`)
	verdictLibAppend(t, ld, rootKey, "submission.made", "c-4", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["x"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fencePos, rng))
	verdictLibAppend(t, ld, rootKey, "merge.observed", "c-4", fmt.Sprintf(`{"merged": %q, "pr": "pr/8"}`, head))
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-4")
	if classesOf(t, e)["merge_without_verdict"] != 1 {
		t.Fatalf("a raw-pushed skipped chain surfaces merge_without_verdict: %+v", e.Result)
	}

	// The lost receipt: c-1's stored evidence disappears, and the
	// check goes from clean to evidence_missing, never silence.
	stored := filepath.Join(src, "var", "artifacts", "sha256", digest)
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-1")
	if classesOf(t, e)["evidence_missing"] != 1 {
		t.Fatalf("a lost receipt surfaces evidence_missing: %+v", e.Result)
	}

	// The force-moved target: rewrite the default branch past the
	// merged commit, and c-2's observation no longer sits under the
	// tip — the charter's force-push divergence, detected by
	// observing the target ref.
	if out, err := exec.Command("git", "-C", src, "reset", "--hard", "--quiet", base).CombinedOutput(); err != nil {
		t.Fatalf("force-move: %v %s", err, out)
	}
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-2")
	if classesOf(t, e)["target_rewritten"] != 1 {
		t.Fatalf("a rewritten target surfaces target_rewritten: %+v", e.Result)
	}

	// Coexisting failures never mask each other: c-1 lost its receipt
	// AND the target moved past its merge, and both classes surface
	// (review finding on this PR).
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-1")
	if by := classesOf(t, e); by["evidence_missing"] != 1 || by["target_rewritten"] != 1 {
		t.Fatalf("lost evidence must not hide a rewritten target: %+v", e.Result)
	}

	// The laundered chain: a raw-pushed verdict by the implementing
	// root, then facts-complete request and observation. The chain
	// looks whole to the fold, and the retroactive boundary check
	// surfaces it as verdict_unverified.
	verdictLibAppend(t, ld, rootKey, "intent.filed", "c-5", `{"intent": "launder", "tier": "trivial", "budget": "s", "routing": "core"}`)
	verdictLibAppend(t, ld, rootKey, "contract.specified", "c-5", `{"acceptance": {"ref": "accept.md @ `+specCommit+`", "executable": false}}`)
	fencePos = verdictLibAppend(t, ld, rootKey, "claim.taken", "c-5", `{}`)
	subPos := verdictLibAppend(t, ld, rootKey, "submission.made", "c-5", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["x"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fencePos, rng))
	rawVerdict := verdictLibAppend(t, ld, rootKey, "verdict.rendered", "c-5", fmt.Sprintf(
		`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`, digest, subPos))
	verdictLibAppend(t, ld, rootKey, "merge.requested", "c-5", fmt.Sprintf(`{"verdict": "%d"}`, rawVerdict))
	verdictLibAppend(t, ld, rootKey, "merge.observed", "c-5", fmt.Sprintf(`{"merged": %q, "pr": "pr/9"}`, head))
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-5")
	if classesOf(t, e)["verdict_unverified"] != 1 {
		t.Fatalf("the laundered chain surfaces verdict_unverified: %+v", e.Result)
	}

	// The whole-fold walk merges every class and stays a report at
	// exit 0: detection is data, not a refusal.
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", src)
	if code != 0 || e.Result["clean"] != "false" {
		t.Fatalf("the full walk reports divergences at exit 0: %d %+v", code, e)
	}
}
