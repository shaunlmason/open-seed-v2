package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: plans/os-56bee171.md AC4 and AC5 — the race is visible
// at claim time (a racer is told its squad races and what the operator
// said it costs), a third racer refuses at the cap naming both holders,
// and claiming stays online-only: the local path refuses a racing
// claim exactly as it refuses any exclusive verb.
func TestRacingIsVisibleAtClaimTime(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	active := "seed/0"
	for _, v := range []string{version.Seed1, version.Seed2, version.Seed3, version.Seed4, version.Seed5, version.Seed6} {
		libAppend(t, remote, resolve, active, ledger.UpgradeVerb, "system", `{"to": "`+v+`"}`)
		active = v
	}
	state := filepath.Join(dir, "state")
	keys := map[string]string{}
	steps := []struct{ verb, subject, payload string }{}
	for i, seed := range []byte{21, 22, 23} {
		path, pub, fp := writeWorkerKey(t, seed)
		keys[fmt.Sprintf("r%d", i)] = path
		steps = append(steps,
			struct{ verb, subject, payload string }{"actor.enrolled", fp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "racer-%d"}`, pub, i)},
			struct{ verb, subject, payload string }{"actor.granted", fp, `{"capability": "claim"}`})
	}
	steps = append(steps,
		struct{ verb, subject, payload string }{"intent.filed", "c-1", `{"intent": "race", "tier": "trivial", "budget": "small", "routing": "core"}`},
		struct{ verb, subject, payload string }{"contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`})
	for _, s := range steps {
		if e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state, "--key", priv, "--verb", s.verb, "--subject", s.subject, "--payload", s.payload); code != 0 {
			t.Fatalf("%s: %d %+v", s.verb, code, e)
		}
	}
	cfg := writeDeclaration(t, `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "trivial", "max_agent": "standard", "racing": {"racers": 2, "cost": "two runs per contract, the loser written off"}}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`)
	// Online-only, racing or not.
	if e, code := runEnv(t, "claim", "take", "--ledger", filepath.Join(dir, "nowhere"), "--key", keys["r0"], "--subject", "c-1", "--config", cfg); code != 2 || e.Error == nil || !strings.Contains(e.Error.Message, "online-only") {
		t.Fatalf("a racing claim is online-only like any claim: %d %+v", code, e)
	}
	for _, name := range []string{"r0", "r1"} {
		e, code := runEnv(t, "claim", "take", "--remote", remote, "--state", filepath.Join(dir, name), "--key", keys[name], "--subject", "c-1", "--config", cfg)
		if code != 0 || !e.OK {
			t.Fatalf("%s claims under the opt-in: %d %+v", name, code, e)
		}
		racing, _ := e.Result["racing"].(map[string]any)
		if racing["racers"] != float64(2) || !strings.Contains(fmt.Sprint(racing["cost"]), "two runs") {
			t.Fatalf("%s is told the race and its cost at claim time: %+v", name, e.Result)
		}
	}
	if e, code := runEnv(t, "claim", "take", "--remote", remote, "--state", filepath.Join(dir, "r2"), "--key", keys["r2"], "--subject", "c-1", "--config", cfg); code != 2 || e.Error == nil || e.Error.Code != "contention" || !strings.Contains(e.Error.Message, "racing cap of 2") {
		t.Fatalf("a third racer refuses at the cap naming both holders: %d %+v", code, e)
	}
	// Without the declaration the same second claim is plain contention.
	if e, code := runEnv(t, "claim", "take", "--remote", remote, "--state", filepath.Join(dir, "plain"), "--key", keys["r2"], "--subject", "c-1"); code != 2 || e.Error == nil || e.Error.Code != "contention" || strings.Contains(e.Error.Message, "racing cap") {
		t.Fatalf("without the opt-in the refusal is contention as it always was: %d %+v", code, e)
	}
}
