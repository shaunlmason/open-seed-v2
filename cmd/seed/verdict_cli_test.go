package main

// The verdict verbs end-to-end (plans/os-f6d2c267.md): the two-key
// drill — the root files, specifies, claims, and submits; a second,
// verdict-granted key renders — plus the transcript-derived render
// rule (pass over a red check refuses exit 20), the
// recompute-and-mismatch check (exit 21), and the capability row
// (a root without a verdict grant refuses 14 before independence).
// The ungated and spec-unrunnable paths are library-drilled in
// internal/verdict: admission refuses arming ungated content at
// contract.specified, so only raw-pushed history can reach them.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// verdictRepo builds the source repository: a base commit, one commit
// carrying three acceptance specs (green, red-check, nondeterministic
// output), and a head commit changing a file.
func verdictRepo(t *testing.T) (dir, base, specCommit, head string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, dir)
	write("hello.txt", "hello\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "base")
	base = git("rev-parse", "HEAD")
	write("accept.md", "# Green\n\n## Validation Commands\n\n- Boundary: `printf ok`\n- `test -f hello.txt`\n")
	write("red.md", "# Red\n\n## Validation Commands\n\n- Boundary: `false`\n")
	write("nondet.md", "# Nondet\n\n## Validation Commands\n\n- Boundary: `head -c 8 /dev/urandom | od -An -tx1`\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "specs")
	specCommit = git("rev-parse", "HEAD")
	write("hello.txt", "changed\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "head")
	head = git("rev-parse", "HEAD")
	return dir, base, specCommit, head
}

// verdictLibAppend signs and appends one record with the given key directly
// through the library, the obs-test posture: the CLI refuses to draft
// exclusive verbs offline, and admitted history is what the drill
// needs. Returns the appended record's position.
func verdictLibAppend(t *testing.T, ld string, key ed25519.PrivateKey, verb, subject, payload string) int {
	t.Helper()
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	tip, count, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, resolve); err != nil {
		t.Fatalf("library append %s: %v", verb, err)
	}
	return count
}

func TestVerdictEndToEndCLI(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head := verdictRepo(t)
	vkey, vpub, vfp := writeWorkerKey(t, 9)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+version.Seed1+`"}`); code != 0 {
		t.Fatalf("upgrade failed: %d %+v", code, e)
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "actor.enrolled", "--subject", vfp, "--payload", fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, vpub)); code != 0 {
		t.Fatalf("verifier enrollment failed: %d %+v", code, e)
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "actor.granted", "--subject", vfp, "--payload", `{"capability": "verdict"}`); code != 0 {
		t.Fatalf("verdict grant failed: %d %+v", code, e)
	}

	// Three contracts sharing the range, differing in acceptance spec.
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)
	rng := base + ".." + head
	for _, c := range []struct{ id, spec string }{{"c-1", "accept.md"}, {"c-2", "nondet.md"}, {"c-3", "red.md"}} {
		for _, step := range [][]string{
			{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`},
			{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, c.spec, specCommit, specCommit)},
		} {
			if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
				"--verb", step[0], "--subject", c.id, "--payload", step[1]); code != 0 {
				t.Fatalf("%s %s failed: %d %+v", c.id, step[0], code, e)
			}
		}
		fencePos := verdictLibAppend(t, ld, rootKey, "claim.taken", c.id, `{}`)
		verdictLibAppend(t, ld, rootKey, "submission.made", c.id, fmt.Sprintf(
			`{"fence": "%d", "packet": {"acceptance": ["%s verified"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
			fencePos, c.id, rng))
	}

	// Receipt: computed, stored, green.
	e, code := runEnv(t, "verdict", "receipt", "--ledger", ld, "--subject", "c-1", "--repo", src)
	if code != 0 || !e.OK {
		t.Fatalf("verdict receipt failed: %d %+v", code, e)
	}
	digest, _ := e.Result["receipt"].(string)
	if len(digest) != 64 || e.Result["red"] != "0" || e.Result["transcripts"] != "2" {
		t.Fatalf("receipt summary wrong: %+v", e.Result)
	}
	if _, err := os.Stat(filepath.Join(src, "var", "artifacts", "sha256", digest)); err != nil {
		t.Fatalf("the receipt is stored content-addressed: %v", err)
	}

	// The implementer root holds no verdict grant: out_of_grant fires
	// before independence, naming the verdict-only row.
	if e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", priv, "--verdict", "pass"); code != 14 {
		t.Fatalf("a root without a verdict grant must refuse 14, got %d %+v", code, e)
	}

	// The disjoint verdict-granted key renders pass; check agrees.
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", vkey, "--verdict", "pass")
	if code != 0 || !e.OK || e.Result["verdict"] != "pass" {
		t.Fatalf("render by the disjoint verifier failed: %d %+v", code, e)
	}
	e, code = runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-1", "--repo", src)
	if code != 0 || !e.OK || e.Result["verdict"] != "pass" {
		t.Fatalf("check must agree with a fresh recomputation: %d %+v", code, e)
	}

	// The red-check contract: pass refuses exit 20 naming the failing
	// command; fail renders.
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-3", "--repo", src,
		"--key", vkey, "--verdict", "pass")
	if code != 20 || e.Error == nil || !strings.Contains(e.Error.Message, `"false"`) {
		t.Fatalf("pass over a red transcript must refuse checks_red naming the command: %d %+v", code, e)
	}
	if e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-3", "--repo", src,
		"--key", vkey, "--verdict", "fail"); code != 0 || e.Result["verdict"] != "fail" {
		t.Fatalf("fail stays renderable over red transcripts: %d %+v", code, e)
	}

	// The nondeterministic contract: render lands, recomputation
	// cannot reproduce the transcripts, check goes red naming digests.
	if e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-2", "--repo", src,
		"--key", vkey, "--verdict", "pass"); code != 0 {
		t.Fatalf("render on the nondeterministic spec: %d %+v", code, e)
	}
	e, code = runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-2", "--repo", src)
	if code != 21 || e.Error == nil || !strings.Contains(e.Error.Message, "recomputation") {
		t.Fatalf("a receipt that does not recompute must refuse receipt_mismatch: %d %+v", code, e)
	}

	// A verdict verb outside review refuses invalid_transition; a
	// subject with no rendered verdict refuses not_found on check.
	if e, code = runEnv(t, "verdict", "receipt", "--ledger", ld, "--subject", "c-none", "--repo", src); code != 3 {
		t.Fatalf("receipt on an unknown subject refuses 3, got %d %+v", code, e)
	}
	if e, code = runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-none", "--repo", src); code != 4 {
		t.Fatalf("check without a rendered verdict refuses 4, got %d %+v", code, e)
	}

	// Reconciliation outlives review: after merge.observed moves c-1
	// to done, check still recomputes against the fold's bound
	// submission (receipt/render stay review-gated).
	verdictLibAppend(t, ld, rootKey, "merge.observed", "c-1", `{}`)
	if e, code = runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-1", "--repo", src); code != 0 || e.Result["artifact"] != "verified" {
		t.Fatalf("check must work after the merge it reconciles: %d %+v", code, e)
	}
	if e, code = runEnv(t, "verdict", "receipt", "--ledger", ld, "--subject", "c-1", "--repo", src); code != 3 {
		t.Fatalf("receipt stays review-gated after done, got %d %+v", code, e)
	}

	// The stored receipt is retrievable evidence: a lost or corrupted
	// artifact makes check red however clean the recomputation.
	stored := filepath.Join(src, "var", "artifacts", "sha256", digest)
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	if e, code = runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-1", "--repo", src); code != 21 || !strings.Contains(e.Error.Message, "retrievable") {
		t.Fatalf("a missing stored receipt refuses receipt_mismatch: %d %+v", code, e)
	}
	if err := os.WriteFile(stored, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code = runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-1", "--repo", src); code != 21 {
		t.Fatalf("a corrupted stored receipt refuses receipt_mismatch: %d %+v", code, e)
	}
}

func TestVerdictCLIRefusalSurfaces(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head := verdictRepo(t)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+version.Seed1+`"}`); code != 0 {
		t.Fatalf("upgrade failed: %d %+v", code, e)
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)

	// Raw-pushed history arms nothing: an ungated executable spec can
	// only exist as a tolerated anomaly (admission refuses it at
	// contract.specified), and the verifier still refuses to run it.
	verdictLibAppend(t, ld, rootKey, "intent.filed", "c-raw", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	verdictLibAppend(t, ld, rootKey, "contract.specified", "c-raw", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true}}`, specCommit))
	fencePos := verdictLibAppend(t, ld, rootKey, "claim.taken", "c-raw", `{}`)
	verdictLibAppend(t, ld, rootKey, "submission.made", "c-raw", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["x"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
		fencePos, base+".."+head))
	e, code := runEnv(t, "verdict", "receipt", "--ledger", ld, "--subject", "c-raw", "--repo", src,
		"--artifacts", filepath.Join(t.TempDir(), "store"))
	if code != 18 || e.Error == nil || !strings.Contains(e.Error.Message, "gate") {
		t.Fatalf("ungated executable content refuses 18 at the CLI: %d %+v", code, e)
	}

	// Usage refusals: missing subverb, unknown subverb, missing flags,
	// and a bad verdict literal are all usage-class.
	for _, args := range [][]string{
		{"verdict"},
		{"verdict", "bogus"},
		{"verdict", "receipt", "--ledger", ld},
		{"verdict", "render", "--ledger", ld, "--subject", "c-raw", "--repo", src, "--key", priv, "--verdict", "maybe"},
		{"verdict", "check", "--subject", "c-raw"},
	} {
		if _, code := runEnv(t, args...); code != 64 {
			t.Fatalf("%v must refuse usage (64), got %d", args, code)
		}
	}
}
