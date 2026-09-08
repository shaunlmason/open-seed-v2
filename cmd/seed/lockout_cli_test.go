package main

// The red-verdict lockout and override end-to-end
// (plans/os-d2497eb7.md): render refuses pass at exit 25 over an
// authenticated fail and unlocks after return + resubmission; the
// override-backed chain reaches done and reconcile reports it by name
// (overridden); a raw-pushed override surfaces override_unverified.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// rawAppend signs and appends one record with any key through the
// library, resolving the signer loosely: the raw-push posture the
// laundering drills need (the genesis resolver knows root keys only).
func rawAppend(t *testing.T, ld string, key ed25519.PrivateKey, verb, subject, payload string) int {
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
	loose := func(f string) (ed25519.PublicKey, bool) {
		if f == fp {
			return key.Public().(ed25519.PublicKey), true
		}
		return resolve(f)
	}
	if _, err := store.Append(rec, loose); err != nil {
		t.Fatalf("raw append %s: %v", verb, err)
	}
	return count
}

func TestRedLockoutAndOverrideCLI(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head := verdictRepo(t)
	vKey, vPub, vFP := writeWorkerKey(t, 9)
	_, pPub, pFP := writeWorkerKey(t, 13)
	for _, step := range [][]string{
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed1 + `"}`},
		{"actor.enrolled", vFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, vPub)},
		{"actor.enrolled", pFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "plain"}`, pPub)},
		{"actor.granted", vFP, `{"capability": "verdict"}`},
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
	plainSeed := make([]byte, ed25519.SeedSize)
	plainSeed[0] = 13
	plainKey := ed25519.NewKeyFromSeed(plainSeed)
	rng := base + ".." + head
	file := func(subject string) {
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
	}

	// c-1: the lockout arc. The verifier renders fail; pass refuses at
	// 25 naming the fail; a return and a fresh cycle unlock it.
	file("c-1")
	e, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", vKey, "--verdict", "fail")
	if code != 0 {
		t.Fatalf("fail renders over green checks — the verifier's judgment: %d %+v", code, e)
	}
	failPos, err := strconv.Atoi(*e.Position)
	if err != nil {
		t.Fatal(err)
	}
	if e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", vKey, "--verdict", "pass"); code != 25 {
		t.Fatalf("pass over the authenticated fail refuses at 25 red_locked: %d %+v", code, e)
	}
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "contract.returned", "--subject", "c-1", "--payload", fmt.Sprintf(`{"verdict": "%d"}`, failPos)); code != 0 {
		t.Fatalf("the operator's cited return admits: %d %+v", code, e)
	}
	fencePos := verdictLibAppend(t, ld, rootKey, "claim.taken", "c-1", `{}`)
	verdictLibAppend(t, ld, rootKey, "submission.made", "c-1", fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["fixed"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fencePos, rng))
	if e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", vKey, "--verdict", "pass"); code != 0 {
		t.Fatalf("a new submission unlocks pass: %d %+v", code, e)
	}

	// c-2: the override-backed chain reaches done, and reconcile
	// reports it by name — never as merge_without_verdict.
	file("c-2")
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-2", "--repo", src,
		"--key", vKey, "--verdict", "fail")
	if code != 0 {
		t.Fatalf("c-2 fail: %d %+v", code, e)
	}
	failPos2, _ := strconv.Atoi(*e.Position)
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.overridden", "--subject", "c-2", "--payload",
		fmt.Sprintf(`{"reason": "verifier environment broken, hand-validated", "verdict": "%d"}`, failPos2)); code != 0 {
		t.Fatalf("the operator's override admits: %d %+v", code, e)
	}
	overridePos, _ := strconv.Atoi(*e.Position)
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.requested", "--subject", "c-2", "--payload", fmt.Sprintf(`{"override": "%d"}`, overridePos)); code != 0 {
		t.Fatalf("the override-backed request admits: %d %+v", code, e)
	}
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.observed", "--subject", "c-2", "--payload", `{"merged": "`+head+`", "pr": "pr/9"}`); code != 0 {
		t.Fatalf("the observed chain lands done through the override: %d %+v", code, e)
	}
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-2")
	if code != 0 {
		t.Fatalf("reconcile: %d %+v", code, e)
	}
	if by := classesOf(t, e); by["overridden"] != 1 || by["merge_without_verdict"] != 0 || by["override_unverified"] != 0 {
		t.Fatalf("the override chain is surfaced by name, never as a divergence: %+v", e.Result)
	}

	// c-3: a raw-pushed override chain — the fold records it, and
	// reconcile surfaces override_unverified.
	file("c-3")
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-3", "--repo", src,
		"--key", vKey, "--verdict", "fail")
	if code != 0 {
		t.Fatalf("c-3 fail: %d %+v", code, e)
	}
	failPos3, _ := strconv.Atoi(*e.Position)
	rawOverride := rawAppend(t, ld, plainKey, "merge.overridden", "c-3", fmt.Sprintf(`{"reason": "laundered", "verdict": "%d"}`, failPos3))
	verdictLibAppend(t, ld, rootKey, "merge.requested", "c-3", fmt.Sprintf(`{"override": "%d"}`, rawOverride))
	verdictLibAppend(t, ld, rootKey, "merge.observed", "c-3", `{"merged": "`+head+`", "pr": "pr/10"}`)
	e, _ = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-3")
	if by := classesOf(t, e); by["override_unverified"] != 1 {
		t.Fatalf("the raw override surfaces override_unverified: %+v", e.Result)
	}
}
