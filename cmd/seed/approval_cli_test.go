package main

// III.L row 4's third mode on the machine-protocol surface
// (plans/os-5781a026.md D8): require-approval per verb, the request
// in the operator's inbox, the operator's attributable grant, the act
// that spends it, all through `serve`, with the CLI giving the same
// codes for the same argv.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// approvalRPC invokes a method through the protocol with argv params
// and decodes the seed-envelope it returns.
func approvalRPC(t *testing.T, method string, argv ...string) ledgerEnv {
	t.Helper()
	params, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	resp, reply := rpc(t, fmt.Sprintf(`{"jsonrpc": "2.0", "id": 1, "method": %q, "params": %s}`, method, params))
	if !reply || resp.Error != nil {
		t.Fatalf("%s through the protocol: %+v", method, resp)
	}
	var e ledgerEnv
	if err := json.Unmarshal(resp.Result, &e); err != nil {
		t.Fatalf("%s: the result is not an envelope: %v %s", method, err, resp.Result)
	}
	return e
}

// approvalRefusedAlike asserts a request is refused through the
// protocol with the code, and that the CLI refuses the same argv with
// the same code: one policy, two surfaces. A refusal appends nothing,
// so the second reading judges the same chain. It returns the
// protocol's envelope for the message.
func approvalRefusedAlike(t *testing.T, method string, exit int, code string, argv ...string) ledgerEnv {
	t.Helper()
	e := approvalRPC(t, method, argv...)
	if e.OK || e.Exit != exit || e.Error == nil || e.Error.Code != code {
		t.Fatalf("%s through the protocol refuses %d %s, got %+v", method, exit, code, e)
	}
	group, sub, _ := strings.Cut(method, ".")
	cli, cliExit := runEnv(t, append([]string{group, sub}, argv...)...)
	if cliExit != exit || cli.Error == nil || cli.Error.Code != code {
		t.Fatalf("%s through the CLI refuses %d %s for the same argv, got %d %+v", method, exit, code, cliExit, cli)
	}
	return e
}

// approvalActorAt reads a record's signer and verb through the
// protocol's ledger.show.
func approvalActorAt(t *testing.T, ld, position string) (actor, verb string) {
	t.Helper()
	e := approvalRPC(t, "ledger.show", "--ledger", ld, "--position", position)
	if !e.OK {
		t.Fatalf("show %s: %+v", position, e)
	}
	ev, _ := e.Result["event"].(map[string]any)
	return fmt.Sprint(ev["actor"]), fmt.Sprint(ev["verb"])
}

// approvalDeclaration governs claim.taken from the standard tier for
// every non-human kind.
const approvalDeclaration = `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "min_tier": "standard"}]}}`

// approvalLedger is a local seed/1 ledger with an agent-kind worker
// holding claim and c-1 specified at the standard tier.
func approvalLedger(t *testing.T) (ld, root, rootFP, worker, workerFP string) {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	ld = filepath.Join(dir, "ledger")
	if e, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatalf("init: %d %+v", code, e)
	}
	append := func(key, verb, subject, payload string) {
		t.Helper()
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", key, "--verb", verb, "--subject", subject, "--payload", payload); code != 0 {
			t.Fatalf("%s: %d %+v", verb, code, e)
		}
	}
	append(priv, "system.protocol.upgraded", "system", `{"to": "`+version.Seed1+`"}`)
	worker, wpub, wfp := writeWorkerKey(t, 41)
	append(priv, "actor.enrolled", wfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "worker"}`, wpub))
	append(priv, "actor.granted", wfp, `{"capability": "claim"}`)
	append(priv, "intent.filed", "c-1", `{"intent": "work", "tier": "standard", "budget": "small", "routing": "core"}`)
	append(priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`)
	rootFP, _ = approvalActorAt(t, ld, "0")
	return ld, priv, rootFP, worker, wfp
}

// conformance: III.L row 4 (attributable approvals; plans/os-5781a026.md
// D8, AC2) — through `serve` on a local ledger: the agent's request
// lands on standing alone naming itself as the actor, surfaces as
// approval.pending owed by the operator lane with its age, and is
// attributed to its signer; a claim-granted key's grant refuses
// out_of_grant; the operator's grant lands citing the request and
// reads back with the operator's fingerprint as actor; a denial says
// why; a request is answered once; two open requests make the
// derivation a choice; the shapes refuse at the door; and the CLI
// gives the same codes for the same argv.
func TestServeApprovalsAreAttributableAndAnsweredOnce(t *testing.T) {
	ld, root, rootFP, worker, workerFP := approvalLedger(t)
	e := approvalRPC(t, "approval.request", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "claim.taken", "--reason", "the drill needs the window")
	if !e.OK || e.Result["actor"] != workerFP || e.Result["verb"] != "claim.taken" || e.Result["approval"] == nil {
		t.Fatalf("the agent's request lands naming itself as the actor: %+v", e)
	}
	requested := e.Result["approval"].(string)
	if actor, verb := approvalActorAt(t, ld, requested); verb != "approval.requested" || actor != workerFP {
		t.Fatalf("the request at %s is attributed to its signer: %s %s", requested, verb, actor)
	}
	// The inbox: the situation read carries the obligation, owed by
	// the operator lane, with its age.
	e = approvalRPC(t, "situation", "--ledger", ld, "--key", root)
	if !e.OK {
		t.Fatalf("situation: %+v", e)
	}
	owed := 0
	for _, o := range e.Result["obligations"].([]any) {
		row, _ := o.(map[string]any)
		if row["kind"] == "approval.pending" && row["subject"] == "c-1" && row["owed_by"] == "lane:operator" && row["age_seconds"] != nil {
			owed++
		}
	}
	if owed != 1 {
		t.Fatalf("the request is the operator's obligation with its age: %+v", e.Result["obligations"])
	}
	// The answers: operator only, attributable to the signer.
	approvalRefusedAlike(t, "approval.grant", 14, "out_of_grant", "--ledger", ld, "--key", worker, "--subject", "c-1")
	e = approvalRPC(t, "approval.grant", "--ledger", ld, "--key", root, "--subject", "c-1")
	if !e.OK || e.Result["request"] != requested || e.Position == nil || e.Result["granted"] == nil {
		t.Fatalf("the operator's grant lands citing the request: %+v", e)
	}
	if actor, verb := approvalActorAt(t, ld, e.Result["granted"].(string)); verb != "approval.granted" || actor != rootFP || actor == workerFP {
		t.Fatalf("the grant is attributed to the operator that signed it: %s %s", verb, actor)
	}
	approvalRefusedAlike(t, "approval.grant", 4, "not_found", "--ledger", ld, "--key", root, "--subject", "c-1")
	if e := approvalRPC(t, "situation", "--ledger", ld, "--key", root, "--subject", "c-1"); !e.OK || len(e.Result["obligations"].([]any)) != 0 {
		t.Fatalf("an answered request owes nothing: %+v", e.Result["obligations"])
	}
	// A denial says why, and the request is answered once.
	e = approvalRPC(t, "approval.request", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "claim.taken", "--reason", "again")
	if !e.OK {
		t.Fatalf("a second request: %+v", e)
	}
	second := e.Result["approval"].(string)
	approvalRefusedAlike(t, "approval.deny", 64, "usage", "--ledger", ld, "--key", root, "--subject", "c-1")
	e = approvalRPC(t, "approval.deny", "--ledger", ld, "--key", root, "--subject", "c-1", "--reason", "not this window")
	if !e.OK || e.Result["request"] != second || e.Result["denied"] == nil {
		t.Fatalf("the operator's denial lands: %+v", e)
	}
	approvalRefusedAlike(t, "approval.grant", 4, "not_found", "--ledger", ld, "--key", root, "--subject", "c-1", "--request", second)
	// The raw seam runs no admission: a second answer pushed past it
	// folds as an anomaly, owes nothing, and the chain still verifies
	// (the boundary's refusal is the remote drill's).
	if e := approvalRPC(t, "ledger.append", "--ledger", ld, "--key", root, "--verb", "approval.granted", "--subject", "c-1", "--payload", `{"request": "`+second+`"}`); !e.OK {
		t.Fatalf("the raw seam appends: %+v", e)
	}
	if e := approvalRPC(t, "situation", "--ledger", ld, "--key", root, "--subject", "c-1"); !e.OK || len(e.Result["obligations"].([]any)) != 0 {
		t.Fatalf("a raw second answer changes nothing: %+v", e.Result["obligations"])
	}
	// Two open requests make the derivation a choice; --request picks.
	var third string
	for i := 0; i < 2; i++ {
		e := approvalRPC(t, "approval.request", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "claim.taken", "--reason", "one of two")
		if !e.OK {
			t.Fatalf("request %d: %+v", i, e)
		}
		if i == 0 {
			third = e.Result["approval"].(string)
		}
	}
	e = approvalRefusedAlike(t, "approval.grant", 64, "usage", "--ledger", ld, "--key", root, "--subject", "c-1")
	if !strings.Contains(e.Error.Message, "--request <position>") {
		t.Fatalf("the ambiguity names the flag: %s", e.Error.Message)
	}
	if e := approvalRPC(t, "approval.grant", "--ledger", ld, "--key", root, "--subject", "c-1", "--request", third); !e.OK || e.Result["request"] != third {
		t.Fatalf("--request picks among several: %+v", e)
	}
	// The shape refuses at the door, before a session opens; the
	// catalog and the roster are the boundary's to judge.
	approvalRefusedAlike(t, "approval.request", 64, "usage", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "approval.granted", "--reason", "self")
	approvalRefusedAlike(t, "approval.request", 64, "usage", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "claim.taken")
	approvalRefusedAlike(t, "approval.request", 3, "approval_refused", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "claim.wished", "--reason", "no such verb")
	approvalRefusedAlike(t, "approval.request", 3, "approval_refused", "--ledger", ld, "--key", worker, "--subject", "c-1", "--verb", "claim.taken", "--for", "fp-nobody", "--reason", "no such key")
	if e, code := runEnv(t, "approval", "wish"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("unknown subverb: %d %+v", code, e)
	}
	if v := approvalRPC(t, "ledger.verify", "--ledger", ld); !v.OK {
		t.Fatalf("the chain carrying every approval fact verifies: %+v", v)
	}
}

// conformance: III.L row 4 (require-approval, the third mode; plans/
// os-5781a026.md D8, AC1, AC3) — through `serve` against a remote,
// where claiming is legal: the agent's governed claim refuses
// approval_required naming the request to file, then the operator's
// turn once the request stands; the operator's grant lands; the act
// admits and spends the grant; the same act refuses again; a trivial
// contract under the floor and an undeclared deployment admit as
// today; and the CLI gives the same codes for the same argv.
func TestServeGovernsAnActByApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the remote drills spawn git per act; the local half runs everywhere (spec/platform.md)")
	}
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	_, priv, _ := writeKeys(t)
	state := filepath.Join(t.TempDir(), "state")
	agentKey, agentPub, agentFP := writeWorkerKey(t, 53)
	decl := writeDeclaration(t, approvalDeclaration)
	on := func(key, subject string, config bool) []string {
		argv := []string{"--remote", remote, "--state", state, "--key", key, "--subject", subject}
		if config {
			argv = append(argv, "--config", decl)
		}
		return argv
	}
	appendRemote := func(key, verb, subject, payload string) {
		t.Helper()
		if e := approvalRPC(t, "ledger.append", "--remote", remote, "--state", state, "--config", decl,
			"--key", key, "--verb", verb, "--subject", subject, "--payload", payload); !e.OK {
			t.Fatalf("%s through the protocol: %+v", verb, e)
		}
	}
	appendRemote(priv, "actor.enrolled", agentFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "agent"}`, agentPub))
	appendRemote(priv, "actor.granted", agentFP, `{"capability": "claim"}`)
	appendRemote(priv, "intent.filed", "c-1", `{"intent": "the middle one", "tier": "standard", "budget": "small", "routing": "core"}`)
	appendRemote(priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`)
	appendRemote(priv, "intent.filed", "c-2", `{"intent": "the small one", "tier": "trivial", "budget": "small", "routing": "core"}`)
	appendRemote(priv, "contract.specified", "c-2", `{"acceptance": {"ref": "specs/c2.md @ abc1234", "executable": false}}`)

	e := approvalRefusedAlike(t, "claim.take", 3, "approval_required", on(agentKey, "c-1", true)...)
	if !strings.Contains(e.Error.Message, "seed approval request --subject c-1 --verb claim.taken") {
		t.Fatalf("the refusal names the request to file: %s", e.Error.Message)
	}
	e = approvalRPC(t, "approval.request", append(on(agentKey, "c-1", true), "--verb", "claim.taken", "--reason", "the drill needs the window")...)
	if !e.OK || e.Result["actor"] != agentFP {
		t.Fatalf("the agent's request lands through the remote: %+v", e)
	}
	requested := e.Result["approval"].(string)
	e = approvalRefusedAlike(t, "claim.take", 3, "approval_required", on(agentKey, "c-1", true)...)
	if !strings.Contains(e.Error.Message, "seed approval grant --subject c-1 --request "+requested) {
		t.Fatalf("a pending request names the operator's turn: %s", e.Error.Message)
	}
	if e := approvalRPC(t, "approval.grant", on(priv, "c-1", true)...); !e.OK || e.Result["request"] != requested {
		t.Fatalf("the operator's grant lands through the remote: %+v", e)
	}
	// The boundary holds the citation: a second answer to the same
	// request refuses at the remote, where admission runs.
	approvalRefusedAlike(t, "ledger.append", 3, "approval_refused", "--remote", remote, "--state", state, "--config", decl,
		"--key", priv, "--verb", "approval.granted", "--subject", "c-1", "--payload", `{"request": "`+requested+`"}`)
	// The act admits under the grant and spends it: after the window
	// closes, the same act refuses again.
	if e := approvalRPC(t, "claim.take", on(agentKey, "c-1", true)...); !e.OK {
		t.Fatalf("the granted act admits: %+v", e)
	}
	if e := approvalRPC(t, "claim.release", append(on(agentKey, "c-1", true), "--packet", writePacket(t, "aaaaaaa..bbbbbbb"))...); !e.OK {
		t.Fatalf("release: %+v", e)
	}
	approvalRefusedAlike(t, "claim.take", 3, "approval_required", on(agentKey, "c-1", true)...)
	// Under the floor, and with no declaration: today's behavior.
	if e := approvalRPC(t, "claim.take", on(agentKey, "c-2", true)...); !e.OK {
		t.Fatalf("a trivial contract is under the standard floor: %+v", e)
	}
	if e := approvalRPC(t, "claim.take", on(agentKey, "c-1", false)...); !e.OK {
		t.Fatalf("with no declaration the act admits, policy never chain validity: %+v", e)
	}
}

// conformance: plans/os-5781a026.md AC4 — the declaration is linted:
// an approvals entry naming a verb outside the catalog, an approval
// verb, a kind outside the roster vocabulary or a tier outside the
// tier vocabulary refuses preseed_incomplete by name; a well-formed
// block lints clean; the shapes refuse at parse.
func TestPreseedCheckLintsTheApprovalsBlock(t *testing.T) {
	clean := `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "kinds": ["agent", "service"], "min_tier": "standard"}, {"verb": "merge.requested", "actors": ["` + strings.Repeat("ab", 32) + `"]}]}}`
	if e, code := runEnv(t, "preseed", "check", "--config", writeDeclaration(t, clean), "--lanes", "../../lanes"); code != 0 || !e.OK {
		t.Fatalf("a well-formed approvals block lints clean: %d %+v", code, e)
	}
	for name, tc := range map[string]struct{ body, names string }{
		"no catalog verb":  {`{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.wished"}]}}`, "claim.wished"},
		"an approval verb": {`{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "approval.granted"}]}}`, "approval.granted"},
		"no roster kind":   {`{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "kinds": ["robot"]}]}}`, "robot"},
		"no fingerprint":   {`{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "actors": ["alice"]}]}}`, "alice"},
		"no such tier":     {`{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "min_tier": "huge"}]}}`, "huge"},
	} {
		e, code := runEnv(t, "preseed", "check", "--config", writeDeclaration(t, tc.body), "--lanes", "../../lanes")
		if code != 13 || e.Error == nil || e.Error.Code != "preseed_incomplete" || !strings.Contains(e.Error.Message, tc.names) {
			t.Errorf("%s must refuse preseed_incomplete naming %s, got %d %+v", name, tc.names, code, e)
		}
	}
	for name, body := range map[string]string{
		"a verb twice":   `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken"}, {"verb": "claim.taken"}]}}`,
		"an empty verb":  `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": ""}]}}`,
		"a kind twice":   `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "kinds": ["agent", "agent"]}]}}`,
		"an actor twice": `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "actors": ["` + strings.Repeat("ab", 32) + `", "` + strings.Repeat("ab", 32) + `"]}]}}`,
		"a spaced floor": `{"posture": "cooperative", "guardrails": {"approvals": [{"verb": "claim.taken", "min_tier": "stan dard"}]}}`,
	} {
		e, code := runEnv(t, "preseed", "check", "--config", writeDeclaration(t, body), "--lanes", "../../lanes")
		if code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" {
			t.Errorf("%s must refuse at parse, got %d %+v", name, code, e)
		}
	}
}
