package main

// III.L row 4 on the machine-protocol surface (plans/os-8ecef90f.md):
// "Per-verb policy governs the machine-protocol surface with
// attributable approvals." The surface has one dispatch path (the
// registry, the CLI's own run functions) and no transport identity,
// so the policy that governs a verb here is the boundary's, and the
// only attribution an approval landed here can have is the chain's.
// The parity drills in serve_test.go say the surface returns the
// CLI's envelope; these say what that envelope is governed by.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// rpcArgv invokes a method through the protocol with argv params and
// decodes the seed-envelope it returns.
func rpcArgv(t *testing.T, method string, argv ...string) ledgerEnv {
	t.Helper()
	params, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"jsonrpc": "2.0", "id": 1, "method": %q, "params": %s}`, method, params)
	resp, reply := rpc(t, line)
	if !reply || resp.Error != nil {
		t.Fatalf("%s through the protocol: %+v", method, resp)
	}
	var e ledgerEnv
	if err := json.Unmarshal(resp.Result, &e); err != nil {
		t.Fatalf("%s: the result is not an envelope: %v %s", method, err, resp.Result)
	}
	return e
}

// refusedAlike asserts a request is refused through the protocol with
// the given code, and that the CLI refuses the same argv with the same
// code: one policy, two surfaces. A refusal appends nothing, so the
// second reading judges the same chain.
func refusedAlike(t *testing.T, method string, exit int, code string, argv ...string) {
	t.Helper()
	e := rpcArgv(t, method, argv...)
	if e.OK || e.Exit != exit || e.Error == nil || e.Error.Code != code {
		t.Fatalf("%s through the protocol refuses %d %s, got %+v", method, exit, code, e)
	}
	group, sub := splitMethod(method)
	cli, cliExit := runEnv(t, append([]string{group, sub}, argv...)...)
	if cliExit != exit || cli.Error == nil || cli.Error.Code != code {
		t.Fatalf("%s through the CLI refuses %d %s for the same argv, got %d %+v", method, exit, code, cliExit, cli)
	}
}

func splitMethod(method string) (group, sub string) {
	for i := 0; i < len(method); i++ {
		if method[i] == '.' {
			return method[:i], method[i+1:]
		}
	}
	return method, ""
}

// actorAt reads a record's signer through the protocol's ledger.show.
func actorAt(t *testing.T, ld, position string) (actor, verb string) {
	t.Helper()
	e := rpcArgv(t, "ledger.show", "--ledger", ld, "--position", position)
	if !e.OK {
		t.Fatalf("show %s: %+v", position, e)
	}
	ev, _ := e.Result["event"].(map[string]any)
	return fmt.Sprint(ev["actor"]), fmt.Sprint(ev["verb"])
}

// conformance: III.L row 4 (per-verb policy governs the machine-
// protocol surface) — through `serve`, a verb the grant table refuses
// the caller, a filing the declaration's routing rule refuses, and a
// claim the declaration's agent ceiling refuses each come back as the
// failing envelope with the code the CLI gives the same argv, and the
// admitted twin of each lands through the same surface. The policy is
// per verb (decision.record refuses a key that claim.take admits),
// per declaration (a squad the file does not name) and per roster
// kind (an agent above the ceiling refuses where a human does not),
// and none of it is the transport's: the surface authenticates nobody
// and consults the boundary alone.
func TestServeRefusesByTheSamePolicyAsTheCLI(t *testing.T) {
	// The grant table, on a local ledger: the worker holds claim, and
	// claim reaches escalation.raised but not decision.recorded.
	ld, _, _, _, _, priv, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]
	raiseOn(t, ld, worker, "c-1")
	answer := func(key string) []string {
		return []string{"--ledger", ld, "--key", key, "--subject", "c-1", "--choice", "a", "--because", "the drill decides"}
	}
	refusedAlike(t, "decision.record", 14, "out_of_grant", answer(worker)...)
	if e := rpcArgv(t, "decision.record", answer(priv)...); !e.OK {
		t.Fatalf("the operator's answer is the admitted twin: %+v", e)
	}

	// The declaration's rules, on a remote: claiming is online-only,
	// and the remote verbs read --config exactly as the CLI does.
	if runtime.GOOS == "windows" {
		t.Skip("the remote drills spawn git per act; the policy half above runs everywhere (spec/platform.md)")
	}
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	state := filepath.Join(t.TempDir(), "state")
	agentKey, agentPub, agentFP := writeWorkerKey(t, 51)
	humanKey, humanPub, humanFP := writeWorkerKey(t, 52)
	decl := writeDeclaration(t, `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`)
	appendRemote := func(key, verb, subject, payload string) ledgerEnv {
		t.Helper()
		return rpcArgv(t, "ledger.append", "--remote", remote, "--state", state, "--config", decl,
			"--key", key, "--verb", verb, "--subject", subject, "--payload", payload)
	}
	for _, s := range []struct{ verb, subject, payload string }{
		{"actor.enrolled", agentFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "agent"}`, agentPub)},
		{"actor.granted", agentFP, `{"capability": "claim"}`},
		{"actor.enrolled", humanFP, fmt.Sprintf(`{"key": %q, "kind": "human", "name": "alice"}`, humanPub)},
		{"actor.granted", humanFP, `{"capability": "claim"}`},
	} {
		if e := appendRemote(priv, s.verb, s.subject, s.payload); !e.OK {
			t.Fatalf("%s through the protocol: %+v", s.verb, e)
		}
	}
	// Routing: the declaration names core and nothing else.
	elsewhere := `{"intent": "elsewhere", "tier": "trivial", "budget": "small", "routing": "other"}`
	refusedAlike(t, "ledger.append", 3, "routing_unknown", "--remote", remote, "--state", state, "--config", decl,
		"--key", priv, "--verb", "intent.filed", "--subject", "c-9", "--payload", elsewhere)
	critical := `{"intent": "the big one", "tier": "critical", "budget": "small", "routing": "core"}`
	if e := appendRemote(priv, "intent.filed", "c-1", critical); !e.OK {
		t.Fatalf("the declared squad is the admitted twin: %+v", e)
	}
	if e := appendRemote(priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`); !e.OK {
		t.Fatalf("specify: %+v", e)
	}
	// The ceiling: the agent-kind key refuses on a critical contract
	// under a standard ceiling, and the human-kind key does not.
	take := func(key string) []string {
		return []string{"--remote", remote, "--state", state, "--config", decl, "--key", key, "--subject", "c-1"}
	}
	refusedAlike(t, "claim.take", 3, "tier_above_ceiling", take(agentKey)...)
	if e := rpcArgv(t, "claim.take", take(humanKey)...); !e.OK {
		t.Fatalf("a human key is not ceilinged, the admitted twin: %+v", e)
	}
}

// conformance: III.L row 4 (attributable approvals) — an approval
// landed through `serve` is a signed chain record: read back through
// the protocol's own ledger.show at the position the write reported,
// its actor is the signing key's fingerprint and never the raiser's
// or a transport identity (the surface has none), and the chain
// verifies afterward, so a fresh reader attributes the approval from
// the chain alone. Two approvals: the human gate's decision.recorded
// and the operator's plan.approved.
func TestServeApprovalsAreAttributableToTheirSigner(t *testing.T) {
	ld, _, _, _, _, priv, _, keys, fps := offerLedgerAndSubject(t, "c-1")
	upgradeLedgerTo(t, ld, priv, version.Seed4)
	worker := keys["workerA"]
	root, _ := actorAt(t, ld, "0")
	if root == fps["workerA"] {
		t.Fatal("the fixture's root and worker are distinct keys")
	}

	raiseOn(t, ld, worker, "c-1")
	e := rpcArgv(t, "decision.record", "--ledger", ld, "--key", priv, "--subject", "c-1", "--choice", "b", "--because", "the drill decides")
	if !e.OK || e.Position == nil {
		t.Fatalf("the decision lands through the protocol at a position: %+v", e)
	}
	if actor, verb := actorAt(t, ld, *e.Position); verb != "decision.recorded" || actor != root || actor == fps["workerA"] {
		t.Fatalf("the decision at %s is attributed to its signer %s, got %s %s", *e.Position, root, verb, actor)
	}

	repo, anchor1, _, _, _ := planRepo(t)
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	if e := rpcArgv(t, "plan.propose", "--ledger", ld, "--key", worker, "--subject", "c-1", "--plan", anchor1, "--repo", repo); !e.OK {
		t.Fatalf("the holder proposes: %+v", e)
	}
	e = rpcArgv(t, "plan.approve", "--ledger", ld, "--key", priv, "--subject", "c-1", "--plan", anchor1, "--pr", "pr/1 @ "+anchorCommit(anchor1), "--repo", repo)
	if !e.OK || e.Position == nil {
		t.Fatalf("the approval lands through the protocol at a position: %+v", e)
	}
	if actor, verb := actorAt(t, ld, *e.Position); verb != "plan.approved" || actor != root {
		t.Fatalf("the approval at %s is attributed to its signer %s, got %s %s", *e.Position, root, verb, actor)
	}
	if v := rpcArgv(t, "ledger.verify", "--ledger", ld); !v.OK {
		t.Fatalf("the chain carrying both approvals verifies: %+v", v)
	}
}

func anchorCommit(anchor string) string {
	for i := len(anchor) - 1; i >= 0; i-- {
		if anchor[i] == ' ' {
			return anchor[i+1:]
		}
	}
	return anchor
}
