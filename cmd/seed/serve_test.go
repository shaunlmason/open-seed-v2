package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/cmd/seed/registry"
)

// usageSubverbs reads a group's subverb vocabulary out of the usage
// line the CLI prints when the group is called bare: the CLI's own
// words, so the registry is held to what the dispatcher actually
// accepts rather than to a second list.
func usageSubverbs(t *testing.T, group string) []string {
	t.Helper()
	e, _ := runEnv(t, group)
	if e.Error == nil {
		return nil
	}
	msg := e.Error.Message
	i := strings.Index(msg, "subverb")
	if i < 0 {
		return nil
	}
	rest := msg[i+len("subverb"):]
	rest = strings.TrimLeft(rest, ": ")
	// Nested alternatives ("deadend [retire | unretire]") belong to
	// the subverb before the bracket, not to the group.
	rest = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(rest, "")
	rest = strings.ReplaceAll(rest, " or ", ",")
	rest = strings.ReplaceAll(rest, "|", ",")
	var out []string
	for _, tok := range strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' }) {
		tok = strings.TrimSpace(tok)
		if tok != "" && regexp.MustCompile(`^[a-z]+$`).MatchString(tok) {
			out = append(out, tok)
		}
	}
	sort.Strings(out)
	return out
}

// conformance: plans/os-b55e5647.md AC1 (exists iff) — the registry's
// subverbs are the dispatcher's own vocabulary in both directions: what
// the usage line names is registered, what is registered the
// dispatcher accepts (a bare subverb fails on its flags, never as an
// unknown subverb), and a flags-only group names no subverb at all.
func TestRegistryMirrorsTheCLIVocabulary(t *testing.T) {
	reg := catalog(strings.NewReader(""))
	seen := map[string]bool{}
	for _, g := range reg.Groups() {
		if seen[g.Name] {
			t.Fatalf("%s registered twice", g.Name)
		}
		seen[g.Name] = true
		if g.Transport {
			continue
		}
		spoken := usageSubverbs(t, g.Name)
		registered := append([]string(nil), g.Subs...)
		sort.Strings(registered)
		if strings.Join(spoken, ",") != strings.Join(registered, ",") {
			t.Errorf("%s: the usage line names %v, the registry %v", g.Name, spoken, registered)
		}
		for _, sub := range g.Subs {
			e, code := runEnv(t, g.Name, sub)
			if code == 0 {
				continue
			}
			if e.Error != nil && strings.Contains(e.Error.Message, "unknown") && strings.Contains(e.Error.Message, "subverb") {
				t.Errorf("%s %s is registered but the dispatcher calls it unknown: %s", g.Name, sub, e.Error.Message)
			}
		}
	}
	// The dispatcher has no case of its own: main.go's run reaches a
	// verb through the registry alone, so a verb added as a switch
	// case would be a CLI-only verb, which the row forbids.
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	runStart := strings.Index(body, "func run(")
	runEnd := strings.Index(body[runStart:], "\n}\n") + runStart
	if runStart < 0 || strings.Contains(body[runStart:runEnd], `case "`) {
		t.Fatal("run dispatches through the registry alone; a case branch would be a verb the protocol cannot see")
	}
	// Every top-level verb the CLI answers is registered: an
	// unregistered word is the one usage refusal that names no group.
	for _, word := range []string{"wander", "ledgerx"} {
		if e, code := runEnv(t, word); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "unknown verb") {
			t.Fatalf("an unregistered verb is unknown: %d %+v", code, e)
		}
	}
}

// conformance: plans/os-b55e5647.md AC1, AC3 — the protocol's method
// set is the CLI's verb set: `serve --list` and the registry derive
// the same sorted list, one method per subverb and one per flags-only
// verb, and the transport itself is the single named carve-out, a CLI
// verb that is the protocol rather than a method of it.
func TestProtocolMethodsAreTheCLIVerbs(t *testing.T) {
	reg := catalog(strings.NewReader(""))
	want := []string{}
	for _, g := range reg.Groups() {
		if g.Transport {
			if g.Name != "serve" {
				t.Fatalf("the one transport verb is serve, got %s", g.Name)
			}
			continue
		}
		if len(g.Subs) == 0 {
			want = append(want, g.Name)
			continue
		}
		for _, s := range g.Subs {
			want = append(want, registry.Method(g.Name, s))
		}
	}
	sort.Strings(want)
	if got := reg.Methods(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Methods derives from the rows: %v vs %v", got, want)
	}
	e, code := runEnv(t, "serve", "--list")
	if code != 0 || e.Result["transport"] != MachineEnvelope {
		t.Fatalf("serve --list: %d %+v", code, e)
	}
	listed := []string{}
	for _, m := range e.Result["methods"].([]any) {
		listed = append(listed, m.(string))
	}
	if strings.Join(listed, ",") != strings.Join(want, ",") {
		t.Fatalf("serve --list names the registry's methods: %v", listed)
	}
	for _, m := range listed {
		if m == "serve" || strings.HasPrefix(m, "serve.") {
			t.Fatal("the transport is no method of itself")
		}
		if _, _, ok := reg.Resolve(m); !ok {
			t.Fatalf("%s is listed but does not resolve", m)
		}
	}
	for _, bad := range []string{"serve", "ledger", "ledger.appendx", "situation.read", "nothing.here", ""} {
		if _, _, ok := reg.Resolve(bad); ok {
			t.Fatalf("%q resolved", bad)
		}
	}
	if e, code := runEnv(t, "serve", "--list", "extra"); code != 64 {
		t.Fatalf("serve takes --list alone: %d %+v", code, e)
	}
}

func rpc(t *testing.T, line string) (rpcResponse, bool) {
	t.Helper()
	return handleRequest(catalog(strings.NewReader("")), []byte(line))
}

// conformance: plans/os-b55e5647.md AC2, AC3 — a method invocation is
// the CLI's own run, and the result is the envelope the CLI would have
// rendered, byte for byte: a read, a write, a refusal (carried as a
// result with its exit and code, never a transport error), and a
// flags-only verb; params as argv or as flags; transport errors for a
// line that is not a request, a method the CLI lacks, and params of
// the wrong shape; a notification answered by silence.
func TestServeReturnsTheCLIEnvelopeVerbatim(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	cli := func(args ...string) []byte {
		t.Helper()
		var out, errOut bytes.Buffer
		run(args, &out, &errOut)
		return bytes.TrimSpace(out.Bytes())
	}
	cliInit := cli("init", "--ledger", ld, "--key", priv)
	if !bytes.Contains(cliInit, []byte(`"ok":true`)) {
		t.Fatalf("init: %s", cliInit)
	}
	corpus := []struct {
		name   string
		argv   []string
		method string
		params string
	}{
		{"a flags-only verb", []string{"version"}, "version", ``},
		{"a read", []string{"ledger", "verify", "--ledger", ld}, "ledger.verify", `{"ledger": ` + jsonString(ld) + `}`},
		{"an audit", []string{"ledger", "audit", "--ledger", ld}, "ledger.audit", `{"ledger": ` + jsonString(ld) + `}`},
		{"a read with argv params", []string{"situation", "--ledger", ld}, "situation", `["--ledger", ` + jsonString(ld) + `]`},
		{"a write", []string{"ledger", "append", "--ledger", ld, "--key", priv, "--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "seed/1"}`}, "ledger.append",
			`{"ledger": ` + jsonString(ld) + `, "key": ` + jsonString(priv) + `, "verb": "system.protocol.upgraded", "subject": "system", "payload": "{\"to\": \"seed/1\"}"}`},
		{"a refusal", []string{"claim", "take", "--ledger", ld, "--key", priv, "--subject", "c-1"}, "claim.take", `{"ledger": ` + jsonString(ld) + `, "key": ` + jsonString(priv) + `, "subject": "c-1"}`},
		{"a usage refusal", []string{"ledger", "show"}, "ledger.show", ``},
	}
	for _, c := range corpus {
		// The write lands once: the CLI appends and the protocol's
		// identical append refuses on the moved tip, so for it the
		// protocol runs first on a fresh copy of the same state.
		if c.name == "a write" {
			resp, _ := rpc(t, `{"jsonrpc": "2.0", "id": 7, "method": "`+c.method+`", "params": `+c.params+`}`)
			if resp.Error != nil || !bytes.Contains(resp.Result, []byte(`"ok":true`)) || !bytes.Contains(resp.Result, []byte(`"position":"1"`)) {
				t.Fatalf("%s through the protocol: %+v %s", c.name, resp.Error, resp.Result)
			}
			continue
		}
		want := cli(c.argv...)
		var line string
		if c.params == "" {
			line = `{"jsonrpc": "2.0", "id": "req-1", "method": "` + c.method + `"}`
		} else {
			line = `{"jsonrpc": "2.0", "id": "req-1", "method": "` + c.method + `", "params": ` + c.params + `}`
		}
		resp, reply := rpc(t, line)
		if !reply || resp.Error != nil || string(resp.ID) != `"req-1"` {
			t.Fatalf("%s: %+v", c.name, resp)
		}
		if string(resp.Result) != string(want) {
			t.Fatalf("%s: the protocol's result is not the CLI's envelope:\n%s\n%s", c.name, resp.Result, want)
		}
	}
	// The refusal above carried its exit and code in the envelope.
	resp, _ := rpc(t, `{"jsonrpc": "2.0", "id": 1, "method": "claim.take", "params": {"ledger": `+jsonString(ld)+`, "key": `+jsonString(priv)+`, "subject": "c-1"}}`)
	var env struct {
		OK    bool `json:"ok"`
		Exit  int  `json:"exit"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Result, &env); err != nil || env.OK || env.Exit != 2 || env.Error.Code != "contention" {
		t.Fatalf("a refusal is a result carrying the failing envelope: %s", resp.Result)
	}
	// Transport errors, each JSON-RPC's own, and the notification.
	for name, tc := range map[string]struct {
		line string
		code int
	}{
		"not json":              {`{"jsonrpc": "2.0",`, rpcParseError},
		"wrong version":         {`{"jsonrpc": "1.0", "id": 1, "method": "version"}`, rpcInvalidRequest},
		"no method":             {`{"jsonrpc": "2.0", "id": 1}`, rpcInvalidRequest},
		"unknown method":        {`{"jsonrpc": "2.0", "id": 1, "method": "ledger.melt"}`, rpcMethodNotFound},
		"the transport":         {`{"jsonrpc": "2.0", "id": 1, "method": "serve"}`, rpcMethodNotFound},
		"params of a number":    {`{"jsonrpc": "2.0", "id": 1, "method": "version", "params": 3}`, rpcInvalidParams},
		"params with an object": {`{"jsonrpc": "2.0", "id": 1, "method": "version", "params": {"x": {"y": 1}}}`, rpcInvalidParams},
	} {
		resp, reply := rpc(t, tc.line)
		if !reply || resp.Error == nil || resp.Error.Code != tc.code {
			t.Errorf("%s: %+v", name, resp)
		}
	}
	if _, reply := rpc(t, `{"jsonrpc": "2.0", "method": "version"}`); reply {
		t.Fatal("a notification is answered by silence")
	}
	// The line loop: two requests and a blank line in, two responses out.
	var out bytes.Buffer
	in := `{"jsonrpc": "2.0", "id": 1, "method": "version"}` + "\n\n" + `{"jsonrpc": "2.0", "id": 2, "method": "nothing"}` + "\n"
	if code := serveLines(catalog(strings.NewReader("")), strings.NewReader(in), &out); code != 0 {
		t.Fatalf("serveLines: %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"result"`) || !strings.Contains(lines[1], `"error"`) {
		t.Fatalf("two responses: %q", lines)
	}
	// Flags from an object: booleans, numbers and repeated values.
	argv, err := paramsToArgv(json.RawMessage(`{"ledger": "x", "verbose": true, "quiet": false, "n": 3, "operator": ["a", "b"], "args": ["extra"]}`))
	if err != nil || strings.Join(argv, " ") != "--ledger x --n 3 --operator a --operator b --verbose extra" {
		t.Fatalf("params to argv: %v %v", argv, err)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// conformance: III.I via plans/os-ef2e3134.md D1, D2 — the human-verdict
// deferral is a protocol method: `serve --list` carries verdict.defer,
// the registry resolves it to the verdict group with the defer argv
// prefix, and invoking it with no flags refuses at usage naming the
// verb's own flags, never an unknown subverb. Before this card the
// dispatcher routed defer while the row omitted it, so the machine
// surface could not route a low-confidence item to a human.
func TestVerdictDeferExposed(t *testing.T) {
	e, code := runEnv(t, "serve", "--list")
	if code != 0 {
		t.Fatalf("serve --list: %d %+v", code, e)
	}
	listed := false
	for _, m := range e.Result["methods"].([]any) {
		if m == "verdict.defer" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("verdict.defer is a protocol method: %v", e.Result["methods"])
	}
	g, argv, ok := catalog(strings.NewReader("")).Resolve("verdict.defer")
	if !ok || g.Name != "verdict" || strings.Join(argv, " ") != "defer" {
		t.Fatalf("the registry resolves verdict.defer to the verdict group with the defer prefix: %v %q %v", ok, g.Name, argv)
	}
	e, code = runEnv(t, "verdict", "defer")
	if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "verdict defer") || strings.Contains(e.Error.Message, "unknown") {
		t.Fatalf("verdict defer with no flags refuses at usage naming its own flags: %d %+v", code, e)
	}
	if spoken := usageSubverbs(t, "verdict"); strings.Join(spoken, ",") != "check,defer,receipt,render,traces" {
		t.Fatalf("the usage line names defer beside the other four: %v", spoken)
	}
}
