package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const requestMarker = "REQUEST-SUMMARY-MARKER-7f3a"

// requestLedger is a local seed/7 ledger with a dispatch-granted
// dispatcher, a service key with standing only (the mirror), a
// claim-only worker, and c-1 specified.
func requestLedger(t *testing.T) (ld, root, dispatcher, service, claimer string) {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	ld = filepath.Join(dir, "ledger")
	if e, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatalf("init: %d %+v", code, e)
	}
	append := func(key, verb, subject, payload string) string {
		t.Helper()
		e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", key, "--verb", verb, "--subject", subject, "--payload", payload)
		if code != 0 || e.Position == nil {
			t.Fatalf("%s: %d %+v", verb, code, e)
		}
		return *e.Position
	}
	for _, v := range version.Supported()[1:] {
		append(priv, "system.protocol.upgraded", "system", `{"to": "`+v+`"}`)
	}
	dispatcher, dpub, dfp := writeWorkerKey(t, 31)
	service, spub, sfp := writeWorkerKey(t, 32)
	claimer, cpub, cfp := writeWorkerKey(t, 33)
	append(priv, "actor.enrolled", dfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "dispatcher"}`, dpub))
	append(priv, "actor.granted", dfp, `{"capability": "dispatch"}`)
	append(priv, "actor.enrolled", sfp, fmt.Sprintf(`{"key": %q, "kind": "service", "name": "mirror"}`, spub))
	append(priv, "actor.enrolled", cfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "worker"}`, cpub))
	append(priv, "actor.granted", cfp, `{"capability": "claim"}`)
	append(priv, "intent.filed", "c-1", `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	append(priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`)
	return ld, priv, dispatcher, service, claimer
}

// conformance: plans/os-48df10a2.md AC1, AC2 at the terminal — a
// service key files a request on system and on a contract, the
// shapes refuse, the situation read carries the notices and the
// obligation and never the summary, the answer needs dispatch and its
// intent, a request is answered once, and the report counts them.
func TestRequestVerbsAtTheTerminal(t *testing.T) {
	ld, root, dispatcher, service, claimer := requestLedger(t)
	e, code := runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "system",
		"--origin", "mirror-a", "--kind", "mirror-edit", "--reference", "cards/c-1.md @ 0123456", "--summary", requestMarker+" rename the card")
	if code != 0 || !e.OK || e.Result["request"] == nil {
		t.Fatalf("a service key files on system: %d %+v", code, e)
	}
	first := e.Result["request"].(string)
	e, code = runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "c-1",
		"--origin", "dash", "--kind", "dashboard-action", "--reference", strings.Repeat("a", 64), "--summary", "cancel it")
	if code != 0 || !e.OK {
		t.Fatalf("a request about a contract, on it: %d %+v", code, e)
	}
	second := e.Result["request"].(string)
	if e, code := runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "system",
		"--origin", "dash", "--kind", "wish", "--reference", "a @ 0123456", "--summary", "s"); code != 3 || e.Error == nil || e.Error.Code != "request_refused" {
		t.Fatalf("an unknown kind is request_refused: %d %+v", code, e)
	}
	if e, code := runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "system",
		"--origin", "dash", "--kind", "mirror-edit", "--reference", "please do this instead", "--summary", "s"); code != 3 || e.Error == nil || e.Error.Code != "request_refused" {
		t.Fatalf("a reference that is not one is request_refused: %d %+v", code, e)
	}
	if e, code := runEnv(t, "request", "file", "--ledger", ld, "--key", service, "--subject", "system", "--origin", "dash"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("the four fields are required: %d %+v", code, e)
	}
	// The situation read: notices and the obligation, never the summary.
	e, code = runEnv(t, "situation", "--ledger", ld, "--key", dispatcher)
	if code != 0 {
		t.Fatalf("situation: %d %+v", code, e)
	}
	body, _ := json.Marshal(e)
	if strings.Contains(string(body), requestMarker) {
		t.Fatalf("the situation read carries the summary: %s", body)
	}
	notices, _ := e.Result["requests"].([]any)
	if len(notices) != 2 {
		t.Fatalf("two notices: %+v", e.Result["requests"])
	}
	for _, n := range notices {
		row, _ := n.(map[string]any)
		for _, k := range []string{"origin", "kind", "subject", "at", "bytes", "age_seconds", "from"} {
			if row[k] == nil {
				t.Errorf("a notice carries %s: %+v", k, row)
			}
		}
	}
	owed := 0
	for _, o := range e.Result["obligations"].([]any) {
		row, _ := o.(map[string]any)
		if row["kind"] == "request.pending" && row["owed_by"] == "lane:dispatch" && row["age_seconds"] != nil {
			owed++
		}
	}
	if owed != 2 {
		t.Fatalf("one obligation per subject, owed to the dispatch lane with its age: %+v", e.Result["obligations"])
	}
	if e, code := runEnv(t, "situation", "--ledger", ld, "--key", dispatcher, "--subject", "c-1"); code != 0 || len(e.Result["requests"].([]any)) != 1 {
		t.Fatalf("the subject filter applies to the notices: %d %+v", code, e.Result["requests"])
	}
	// The answer: dispatch, on the request's subject, with its intent, once.
	if e, code := runEnv(t, "request", "answer", "--ledger", ld, "--key", claimer, "--subject", "system", "--request", first, "--outcome", "declined", "--reason", "no"); e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim-only key answers nothing: %d %+v", code, e)
	}
	if e, code := runEnv(t, "request", "answer", "--ledger", ld, "--key", dispatcher, "--subject", "system", "--request", first, "--outcome", "filed"); code != 3 || e.Error == nil || e.Error.Code != "request_refused" {
		t.Fatalf("filed without its intent: %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", dispatcher, "--verb", "intent.filed", "--subject", "c-2", "--payload", `{"intent": "the mirror's rename", "tier": "trivial", "budget": "small", "routing": "core"}`)
	if code != 0 || e.Position == nil {
		t.Fatalf("the dispatcher files the intent: %d %+v", code, e)
	}
	intent := *e.Position
	e, code = runEnv(t, "request", "answer", "--ledger", ld, "--key", dispatcher, "--subject", "system", "--request", first, "--outcome", "filed", "--intent", intent)
	if code != 0 || !e.OK || e.Result["answer"] == nil || e.Result["outcome"] != "filed" {
		t.Fatalf("the answer cites the intent: %d %+v", code, e)
	}
	if e, code := runEnv(t, "request", "answer", "--ledger", ld, "--key", dispatcher, "--subject", "system", "--request", first, "--outcome", "declined", "--reason", "again"); code != 3 || e.Error == nil || e.Error.Code != "request_refused" {
		t.Fatalf("a request is answered once: %d %+v", code, e)
	}
	if e, code := runEnv(t, "request", "answer", "--ledger", ld, "--key", root, "--subject", "c-1", "--request", second, "--outcome", "declined", "--reason", "not now"); code != 0 || !e.OK {
		t.Fatalf("the operator declines the second: %d %+v", code, e)
	}
	if e, code := runEnv(t, "situation", "--ledger", ld, "--key", dispatcher); code != 0 || len(e.Result["requests"].([]any)) != 0 {
		t.Fatalf("nothing pending after the answers: %d %+v", code, e.Result["requests"])
	}
	out := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out); code != 0 {
		t.Fatalf("project rebuild: %d %+v", code, e)
	}
	var report map[string]any
	carried := false
	_ = filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(b), requestMarker) {
			carried = true
		}
		if info.Name() == "report.json" && report == nil {
			_ = json.Unmarshal(b, &report)
		}
		return nil
	})
	sec, _ := report["requests"].(map[string]any)
	if sec == nil || sec["total"] != 2.0 || sec["unanswered"] != 0.0 {
		t.Fatalf("the report counts the requests: %+v", report["requests"])
	}
	if !carried {
		t.Error("the projections carry the payload verbatim, the summary included")
	}
}

func keyFromSeed(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

// federated stands up a remote rooted at its own key, upgraded to the
// given version, and returns the remote and its resolver.
func federated(t *testing.T, root ed25519.PrivateKey, upto string) (string, ledger.Resolver, string) {
	t.Helper()
	remote := bareRemote(t)
	resolve := seedRemoteGenesisAs(t, remote, root)
	active := version.Protocol
	for _, v := range version.Supported()[1:] {
		if active == upto {
			break
		}
		libAppendAs(t, remote, resolve, root, active, ledger.UpgradeVerb, "system", `{"to": "`+v+`"}`)
		active = v
	}
	return remote, resolve, active
}

const federationBase = `"guardrails": {"squads": {"core": {"default": "trivial", "max_agent": "standard"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}`

// conformance: plans/os-48df10a2.md AC4 — two ledgers with distinct
// roots, the org view over both, byte-identical on unchanged tips, a
// tampered remote reported and not folded, and the absence proof: the
// command takes no key and appends nothing anywhere.
func TestFederationReportReadsRemotesReadOnly(t *testing.T) {
	rootA, rootB := keyFromSeed(41), keyFromSeed(42)
	remoteA, resolveA, _ := federated(t, rootA, version.Seed7)
	libAppendAs(t, remoteA, resolveA, rootA, version.Seed7, "intent.filed", "c-1", `{"intent": "a", "tier": "trivial", "budget": "small", "routing": "core"}`)
	libAppendAs(t, remoteA, resolveA, rootA, version.Seed7, "request.filed", "system", `{"origin": "dash", "kind": "dashboard-action", "reference": "a/b @ 0123456", "summary": "s"}`)
	remoteB, resolveB, activeB := federated(t, rootB, version.Seed1)
	libAppendAs(t, remoteB, resolveB, rootB, activeB, "intent.filed", "c-1", `{"intent": "b", "tier": "trivial", "budget": "small", "routing": "core"}`)
	tipA, tipB := remoteTip(t, remoteA), remoteTip(t, remoteB)
	cfg := writeDeclaration(t, `{"posture": "cooperative", `+federationBase+`, "federation": {"remotes": [{"name": "alpha", "remote": `+jsonString(remoteA)+`, "ref": "refs/seed/ledger"}, {"name": "beta", "remote": `+jsonString(remoteB)+`}]}}`)
	state := filepath.Join(t.TempDir(), "state")
	if e, code := runEnv(t, "federation", "report", "--config", cfg, "--state", state, "--key", "anything"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("a federation command takes no key: %d %+v", code, e)
	}
	e, code := runEnv(t, "federation", "report", "--config", cfg, "--state", state)
	if code != 0 || !e.OK {
		t.Fatalf("federation report: %d %+v %+v", code, e, e.Error)
	}
	first, err := os.ReadFile(filepath.Join(state, "federation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Remotes []map[string]any `json:"remotes"`
		Totals  map[string]any   `json:"totals"`
	}
	if err := json.Unmarshal(first, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Remotes) != 2 || view.Totals["remotes"] != 2.0 || view.Totals["verified"] != 2.0 || view.Totals["contracts"] != 2.0 || view.Totals["requests_unanswered"] != 1.0 {
		t.Fatalf("the org view over both: %s", first)
	}
	if view.Remotes[0]["name"] != "alpha" || view.Remotes[0]["protocol"] != version.Seed7 || view.Remotes[0]["requests_unanswered"] != 1.0 || view.Remotes[0]["tip"] == "" {
		t.Fatalf("alpha's row: %+v", view.Remotes[0])
	}
	if view.Remotes[1]["name"] != "beta" || view.Remotes[1]["verified"] != true || view.Remotes[1]["ref"] != remoteRef {
		t.Fatalf("beta's row, with the default ref: %+v", view.Remotes[1])
	}
	if _, code := runEnv(t, "federation", "report", "--config", cfg, "--state", state); code != 0 {
		t.Fatal("the second report")
	}
	again, _ := os.ReadFile(filepath.Join(state, "federation.json"))
	if string(again) != string(first) {
		t.Fatalf("byte-identical on unchanged tips:\n%s\n%s", first, again)
	}
	// The absence proof: nothing appended anywhere.
	if remoteTip(t, remoteA) != tipA || remoteTip(t, remoteB) != tipB {
		t.Fatal("a federation read moved a remote's tip")
	}
	// A tampered remote: pushed raw around admission, it fails to
	// verify and is reported as such, never folded.
	client, err := gitref.NewClient(t.TempDir(), remoteB, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	if fetched, err := client.Fetch(); err != nil || fetched != tipB {
		t.Fatalf("fetch before the tamper: %q %v", fetched, err)
	}
	work := t.TempDir()
	if err := client.Materialize(tipB, work); err != nil {
		t.Fatal(err)
	}
	segments, _ := filepath.Glob(filepath.Join(work, "segments", "*.jsonl"))
	if len(segments) == 0 {
		t.Fatal("no segment materialized")
	}
	raw, _ := os.ReadFile(segments[0])
	if err := os.WriteFile(segments[0], []byte(strings.Replace(string(raw), `"intent":"b"`, `"intent":"B"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"intent":"b"`) {
		t.Fatalf("the segment carries the record to tamper: %s", raw)
	}
	commit, err := client.Commit(work, tipB, "tamper")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(commit); err != nil {
		t.Fatal(err)
	}
	e, code = runEnv(t, "federation", "report", "--config", cfg, "--state", state)
	if code != 0 || !e.OK {
		t.Fatalf("a report over a tampered remote still renders: %d %+v", code, e)
	}
	third, _ := os.ReadFile(filepath.Join(state, "federation.json"))
	if err := json.Unmarshal(third, &view); err != nil {
		t.Fatal(err)
	}
	if view.Remotes[1]["verified"] != false || view.Remotes[1]["error"] == nil || view.Remotes[1]["contracts_by_state"].(map[string]any)["filed"] != nil || view.Totals["verified"] != 1.0 || view.Totals["unverified"] != 1.0 || view.Totals["contracts"] != 1.0 {
		t.Fatalf("the tampered remote is reported and not folded: %s", third)
	}
	if view.Remotes[0]["verified"] != true {
		t.Fatalf("alpha still verifies: %+v", view.Remotes[0])
	}
}

// conformance: plans/os-48df10a2.md AC5 — a source contract proposed
// into a target as a cross-repo request by an ingress key with
// standing only, answered by the target's dispatcher with an intent
// citing it, the answer read back through the source's read remote,
// and nothing flowing back: the source's tip never moves and the
// ingress key can append nothing else.
func TestCrossRepoWorkEntersAsARequest(t *testing.T) {
	rootS, rootT := keyFromSeed(43), keyFromSeed(44)
	source, resolveS, activeS := federated(t, rootS, version.Seed1)
	libAppendAs(t, source, resolveS, rootS, activeS, "intent.filed", "c-9", `{"intent": "shared work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	tipS := remoteTip(t, source)
	target, resolveT, _ := federated(t, rootT, version.Seed7)
	ingress, ipub, ifp := writeWorkerKey(t, 51)
	dispatcher, dpub, dfp := writeWorkerKey(t, 52)
	libAppendAs(t, target, resolveT, rootT, version.Seed7, "actor.enrolled", ifp, fmt.Sprintf(`{"key": %q, "kind": "service", "name": "source-ingress"}`, ipub))
	libAppendAs(t, target, resolveT, rootT, version.Seed7, "actor.enrolled", dfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "dispatcher"}`, dpub))
	libAppendAs(t, target, resolveT, rootT, version.Seed7, "actor.granted", dfp, `{"capability": "dispatch"}`)
	dir := t.TempDir()
	e, code := runEnv(t, "request", "file", "--remote", target, "--state", filepath.Join(dir, "ingress"), "--key", ingress, "--subject", "system",
		"--origin", "source", "--kind", "cross-repo", "--reference", "source/c-9 @ "+tipS, "--summary", "c-9 proposes shared work")
	if code != 0 || !e.OK || e.Result["request"] == nil {
		t.Fatalf("the proposal enters the target as a request: %d %+v", code, e)
	}
	pos := e.Result["request"].(string)
	if e, code := runEnv(t, "ledger", "append", "--remote", target, "--state", filepath.Join(dir, "ingress2"), "--key", ingress, "--verb", "intent.filed", "--subject", "c-10", "--payload", `{"intent": "x", "tier": "trivial", "budget": "small", "routing": "core"}`); code == 0 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("the ingress key holds no capability: %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "append", "--remote", target, "--state", filepath.Join(dir, "d1"), "--key", dispatcher, "--verb", "intent.filed", "--subject", "c-10", "--payload", `{"intent": "shared work, from source c-9 (request `+pos+`)", "tier": "trivial", "budget": "small", "routing": "core"}`)
	if code != 0 || e.Position == nil {
		t.Fatalf("the target's dispatcher files: %d %+v", code, e)
	}
	if e, code := runEnv(t, "request", "answer", "--remote", target, "--state", filepath.Join(dir, "d2"), "--key", dispatcher, "--subject", "system", "--request", pos, "--outcome", "filed", "--intent", *e.Position); code != 0 || !e.OK {
		t.Fatalf("the answer cites the intent: %d %+v", code, e)
	}
	// The source reads the answer through its own read remote.
	cfg := writeDeclaration(t, `{"posture": "cooperative", `+federationBase+`, "federation": {"remotes": [{"name": "target", "remote": `+jsonString(target)+`}]}}`)
	state := filepath.Join(dir, "fed")
	if e, code := runEnv(t, "federation", "report", "--config", cfg, "--state", state); code != 0 || !e.OK {
		t.Fatalf("the source's read: %d %+v %+v", code, e, e.Error)
	}
	b, _ := os.ReadFile(filepath.Join(state, "federation.json"))
	var view struct {
		Remotes []map[string]any `json:"remotes"`
	}
	if err := json.Unmarshal(b, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Remotes) != 1 || view.Remotes[0]["requests_unanswered"] != 0.0 || view.Remotes[0]["verified"] != true {
		t.Fatalf("the target's answer is read, not written back: %s", b)
	}
	if remoteTip(t, source) != tipS {
		t.Fatal("something flowed back to the source")
	}
}
