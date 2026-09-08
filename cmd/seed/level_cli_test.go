package main

// The independence levels at the CLI (plans/os-99829835.md AC5, AC6;
// spec/verdicts.md "Independence levels"): `verdict render`
// computes the achieved level from the window's declaration, its own
// declared tuple and the folded acceptance, refuses at usage without
// the flags on a tier the record cannot satisfy, refuses level_short
// with a same-family declaration, and writes the level; `verdict
// check` renders it; `reconcile` classifies what raw pushes claim.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func levelTuple(model string) string {
	return fmt.Sprintf(`{"principal": "acme", "harness": "local-worktree/v0", "model": %q, "tool_policy": "default", "environment": "detached-git-worktree"}`, model)
}

// levelLedger is offerLedger upgraded to seed/4, with a driver that
// files a contract at the given tier with a prose or an executable
// gated spec, claims it, reserves, declares a start under the base
// model, and submits: the window every level is computed against.
func levelLedger(t *testing.T) (ld, src, specCommit, rng, priv string, keys map[string]string, drive func(subject, tier string, executable bool) int) {
	t.Helper()
	ld, src, base, specCommit, head, priv, rootKey, keys, _ := offerLedger(t)
	// A sealer, so the tiers that require sealed checks can be
	// rendered: the level is computed after the unsealed refusal, and
	// every tier but trivial requires the seal. The human: a key with
	// a verdict grant beside operator standing (plans/os-2e34f66a.md
	// D4), which renders the critical tier over the verifier's
	// deferral; the root implemented these contracts, so L1 keeps it
	// out.
	sealKey, sealPub, sealFP := writeWorkerKey(t, 25)
	keys["sealer"] = sealKey
	humanKey, humanPub, humanFP := writeWorkerKey(t, 26)
	keys["human"] = humanKey
	for _, step := range [][]string{
		{"actor.enrolled", sealFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "sealer"}`, sealPub)},
		{"actor.granted", sealFP, `{"capability": "sealer"}`},
		{"actor.enrolled", humanFP, fmt.Sprintf(`{"key": %q, "kind": "human", "name": "reviewer"}`, humanPub)},
		{"actor.granted", humanFP, `{"capability": "verdict"}`},
		{"actor.granted", humanFP, `{"capability": "operator"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	checks := writeChecks(t, "# sealed", "true")
	for _, to := range []string{version.Seed2, version.Seed3, version.Seed4} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+to+`"}`); code != 0 {
			t.Fatalf("upgrade to %s: %d %+v", to, code, e)
		}
	}
	rng = base + ".." + head
	drive = func(subject, tier string, executable bool) int {
		t.Helper()
		spec := fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, specCommit)
		if executable {
			spec = fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)
		}
		rawAppendAt(t, ld, rootKey, version.Seed4, "intent.filed", subject, fmt.Sprintf(`{"intent": "drill", "tier": %q, "budget": "small", "routing": "core"}`, tier))
		rawAppendAt(t, ld, rootKey, version.Seed4, "contract.specified", subject, spec)
		if tier != "trivial" {
			if e, code := runEnv(t, "seal", "create", "--ledger", ld, "--subject", subject, "--repo", src,
				"--checks", checks, "--key", keys["sealer"]); code != 0 {
				t.Fatalf("seal create on %s: %d %+v", subject, code, e.Error)
			}
		}
		fence := rawAppendAt(t, ld, rootKey, version.Seed4, "claim.taken", subject, `{}`)
		res := rawAppendAt(t, ld, rootKey, version.Seed4, "budget.reserve", subject, fmt.Sprintf(`{"amount": "10", "fence": "%d"}`, fence))
		rawAppendAt(t, ld, workerRawKey(21), version.Seed4, "run.started", subject, fmt.Sprintf(`{"fence": "%d", "reservation": "%d", "tuple": %s}`, fence, res, levelTuple("fable/5.1")))
		return rawAppendAt(t, ld, rootKey, version.Seed4, "submission.made", subject, fmt.Sprintf(
			`{"fence": "%d", "packet": {"acceptance": ["%s ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, subject, rng))
	}
	return
}

// conformance: AC5 — render computes the level and refuses what the
// tier cannot get; check and the contracts view render it.
func TestVerdictRenderComputesTheLevel(t *testing.T) {
	ld, src, _, _, _, keys, drive := levelLedger(t)
	render := func(subject string, extra ...string) (ledgerEnv, int) {
		return runEnv(t, append([]string{"verdict", "render", "--ledger", ld, "--subject", subject, "--repo", src,
			"--key", keys["verifier"], "--verdict", "pass"}, extra...)...)
	}
	declare := func(model string) []string {
		return []string{"--principal", "acme", "--model", model, "--tool-policy", "default"}
	}

	drive("c-std", "standard", false)
	e, code := render("c-std")
	if code != 0 || e.Result["independence"] != "L1" {
		t.Fatalf("a prose standard contract with no declaration renders L1: %d %+v", code, e)
	}
	if e, code := runEnv(t, "verdict", "check", "--ledger", ld, "--subject", "c-std", "--repo", src, "--key", keys["verifier"]); code != 0 || e.Result["independence"] != "L1" {
		t.Fatalf("verdict check renders the recorded level: %d %+v", code, e)
	}

	drive("c-crit", "critical", false)
	e, code = render("c-crit")
	if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "--principal") || !strings.Contains(e.Error.Message, "L2") {
		t.Fatalf("a critical contract with no declaration refuses at usage naming the flags and the requirement: %d %+v", code, e)
	}
	e, code = render("c-crit", declare("fable/9.9")...)
	if code != 17 || e.Error == nil || e.Error.Code != "level_short" || !strings.Contains(e.Error.Message, "critical") {
		t.Fatalf("the same family on a critical contract refuses level_short: %d %+v", code, e)
	}
	e, code = render("c-crit", "--principal", "acme", "--model", "other/1")
	if code != 64 {
		t.Fatalf("a partial declaration refuses at usage: %d %+v", code, e)
	}
	// The critical tier's render is a human's (plans/os-2e34f66a.md
	// D4): the verifier refuses human_verdict whatever it declares,
	// defers the whole verdict with the receipt it computed, and the
	// human renders over that receipt, a key with operator standing
	// being no sealed-checks recipient.
	e, code = render("c-crit", declare("other/1")...)
	if code != 20 || e.Error == nil || e.Error.Code != "human_verdict" {
		t.Fatalf("a verdict-only key on a critical contract refuses human_verdict: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, "verdict", "defer", "--ledger", ld, "--subject", "c-crit", "--repo", src, "--key", keys["verifier"])
	if code != 0 || e.Result["owed_by"] != "lane:operator" || e.Result["receipt"] == nil {
		t.Fatalf("the verifier defers the critical verdict whole, citing its receipt: %d %+v", code, e)
	}
	e, code = runEnv(t, append([]string{"verdict", "render", "--ledger", ld, "--subject", "c-crit", "--repo", src,
		"--key", keys["human"], "--verdict", "pass"}, declare("other/1")...)...)
	if code != 0 || e.Result["independence"] != "L2" {
		t.Fatalf("the human renders L2 over the deferral's receipt: %d %+v", code, e)
	}

	drive("c-exec", "standard", true)
	e, code = render("c-exec")
	if code != 0 || e.Result["independence"] != "L3" {
		t.Fatalf("an executable gated spec renders L3 with no declaration: %d %+v", code, e)
	}

	// The contracts view carries the level beside the verdict.
	out := t.TempDir() + "/out"
	unlockForCleanup(t, out)
	if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out); code != 0 {
		t.Fatalf("project rebuild: %d %+v", code, e)
	}
	cur, code := runEnv(t, "project", "current", "--out", out, "--name", "contracts")
	if code != 0 {
		t.Fatalf("project current: %d %+v", code, cur)
	}
	b, err := os.ReadFile(filepath.Join(cur.Result["path"].(string), "contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	view := string(b)
	if !strings.Contains(view, `"independence": "L3"`) || !strings.Contains(view, `"independence": "L2"`) {
		t.Fatalf("the contracts view shows the recorded levels: %s", view)
	}
}

// conformance: AC6 — reconcile classifies a raw-pushed level the
// records do not support, one short of its tier, and an L3 whose
// receipt does not reproduce, and never a supported claim that meets
// the tier.
func TestReconcileClassifiesUnsupportedLevels(t *testing.T) {
	ld, src, _, _, _, keys, drive := levelLedger(t)
	verifier := workerRawKey(24)
	classes := func(subject string) map[string]float64 {
		e, code := runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", subject)
		if code != 0 {
			t.Fatalf("reconcile %s: %d %+v", subject, code, e)
		}
		return classesOf(t, e)
	}
	verdict := func(sub int, level, receipt, tup string) string {
		body := fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": %q`, receipt, sub, level)
		if tup != "" {
			body += `, "tuple": ` + tup
		}
		return body + `}`
	}
	bogus := strings.Repeat("ab", 32)

	sub := drive("c-fake", "standard", false)
	rawAppendAt(t, ld, verifier, version.Seed4, "verdict.rendered", "c-fake", verdict(sub, "L2", bogus, ""))
	if classes("c-fake")["independence_unverified"] != 1 {
		t.Fatal("L2 with no declaration classifies independence_unverified")
	}
	sub = drive("c-same", "standard", false)
	rawAppendAt(t, ld, verifier, version.Seed4, "verdict.rendered", "c-same", verdict(sub, "L2", bogus, levelTuple("fable/6.0")))
	if classes("c-same")["independence_unverified"] != 1 {
		t.Fatal("L2 with a same-family declaration classifies independence_unverified")
	}
	sub = drive("c-short", "critical", false)
	rawAppendAt(t, ld, verifier, version.Seed4, "verdict.rendered", "c-short", verdict(sub, "L1", bogus, ""))
	if classes("c-short")["independence_unverified"] != 1 {
		t.Fatal("a verdict short of its tier classifies independence_unverified")
	}
	// Trivial, so the subject is unsealed: a sealed subject's receipt
	// needs the sealer's key to recompute and is `verdict check
	// --key`'s to judge, so reconcile reproduces the unsealed ones.
	sub = drive("c-l3", "trivial", true)
	rawAppendAt(t, ld, verifier, version.Seed4, "verdict.rendered", "c-l3", verdict(sub, "L3", bogus, ""))
	if classes("c-l3")["independence_unverified"] != 1 {
		t.Fatal("an L3 whose receipt does not reproduce classifies independence_unverified at evidence grade")
	}
	sub = drive("c-ok", "standard", false)
	rawAppendAt(t, ld, verifier, version.Seed4, "verdict.rendered", "c-ok", verdict(sub, "L2", bogus, levelTuple("other/1")))
	if by := classes("c-ok"); by["independence_unverified"] != 0 {
		t.Fatalf("a supported claim that meets the tier classifies nothing: %v", by)
	}

	// A sealed L3 verdict recomputes only under an identity able to
	// unseal, and there is no silent partial verification: without
	// --key reconcile refuses naming the subject and the flag, under a
	// key that is no recipient it refuses not_recipient, and under the
	// verifier's key the bogus receipt classifies as the unsealed one
	// did.
	sub = drive("c-l3s", "standard", true)
	rawAppendAt(t, ld, verifier, version.Seed4, "verdict.rendered", "c-l3s", verdict(sub, "L3", bogus, ""))
	e, code := runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-l3s")
	if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "c-l3s") || !strings.Contains(e.Error.Message, "--key") {
		t.Fatalf("a sealed L3 verdict with no key refuses at usage naming the subject and --key: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-l3s", "--key", keys["workerA"])
	if code != 23 || e.Error == nil || e.Error.Code != "not_recipient" {
		t.Fatalf("a key that cannot unseal refuses not_recipient: %d %+v", code, e.Error)
	}
	e, code = runEnv(t, "reconcile", "--ledger", ld, "--repo", src, "--subject", "c-l3s", "--key", keys["verifier"])
	if code != 0 || classesOf(t, e)["independence_unverified"] != 1 {
		t.Fatalf("under an identity able to unseal, a sealed L3 whose receipt does not reproduce classifies independence_unverified: %d %+v", code, e)
	}

	// The maintenance loop grades the same evidence: the unsealed L3
	// classifies, and the sealed one is reported skipped with the
	// reason under a key that cannot open it, classified under one
	// that can.
	maintain := func(key string) ledgerEnv {
		t.Helper()
		e, code := runEnv(t, "maintain", "run", "--ledger", ld, "--repo", src, "--key", key, "--obs", t.TempDir())
		if code != 0 {
			t.Fatalf("maintain run: %d %+v", code, e)
		}
		return e
	}
	// subjectsOf maps each row's subject to its text (a finding's
	// detail, a skip's reason), keeping the rows whose named field
	// carries the wanted value.
	subjectsOf := func(rows any, key, want string) map[string]string {
		out := map[string]string{}
		list, _ := rows.([]any)
		for _, r := range list {
			m, _ := r.(map[string]any)
			s, _ := m["subject"].(string)
			if v, _ := m[key].(string); s == "" || (want != "" && v != want) {
				continue
			}
			text, _ := m["detail"].(string)
			if because, _ := m["because"].(string); because != "" {
				text = because
			}
			out[s] = text
		}
		return out
	}
	e = maintain(keys["workerA"])
	found := subjectsOf(e.Result["findings"], "class", "independence_unverified")
	if _, ok := found["c-l3"]; !ok {
		t.Fatalf("the maintenance pass reproduces the unsealed L3: %+v", e.Result["findings"])
	}
	if _, ok := found["c-l3s"]; ok {
		t.Fatalf("the sealed L3 the actor's key cannot open is not judged: %+v", e.Result["findings"])
	}
	if skipped := subjectsOf(e.Result["skipped"], "", ""); !strings.Contains(skipped["c-l3s"], "not reproduced") {
		t.Fatalf("the sealed L3 the actor's key cannot open is reported skipped with the reason: %+v", e.Result["skipped"])
	}
	e = maintain(keys["verifier"])
	found = subjectsOf(e.Result["findings"], "class", "independence_unverified")
	if _, ok := found["c-l3s"]; !ok {
		t.Fatalf("under an identity able to unseal the maintenance pass reproduces the sealed L3 too: %+v", e.Result["findings"])
	}
	if skipped := subjectsOf(e.Result["skipped"], "", ""); skipped["c-l3s"] != "" {
		t.Fatalf("nothing is skipped once the key opens the seal: %+v", e.Result["skipped"])
	}
}
