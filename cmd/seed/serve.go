package main

// The machine-protocol surface (plans/os-b55e5647.md D1, D3, D5;
// spec/protocol.md "The machine surface"; charter III.I row 3):
// `seed serve` speaks JSON-RPC 2.0, one request per line over stdin
// and one response per line over stdout, a method per CLI verb drawn
// from the same registry the CLI dispatches. A method invocation runs
// the CLI's own run function with the params as its argv and returns
// the seed-envelope it rendered, verbatim, as the JSON-RPC result — a
// refusal is a result carrying the failing envelope, never a transport
// error, so a machine caller reads the same structured code the exit
// status carries. Transport errors are JSON-RPC's own: a line that is
// not a request, a method the registry lacks, params of the wrong
// shape. The framing is machine-envelope/0; the payload stays
// seed-envelope/0.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/shaunlmason/open-seed-v2/cmd/seed/registry"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
)

// MachineEnvelope names the transport framing: JSON-RPC 2.0 lines
// whose result is a seed-envelope/0.
const MachineEnvelope = "machine-envelope/0"

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	list := fs.Bool("list", false, "print the method set and exit, as an envelope")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "serve [--list]: JSON-RPC 2.0 over stdin/stdout, one request per line"), stdout, stderr)
	}
	reg := catalog(strings.NewReader(""))
	if *list {
		return render(envelope.OK(map[string]any{"transport": MachineEnvelope, "methods": reg.Methods()}), stdout, stderr)
	}
	return serveLines(reg, os.Stdin, stdout)
}

// serveLines answers every request line until the stream ends. A
// notification (no id) runs and answers nothing, as JSON-RPC says.
func serveLines(reg *registry.Registry, in io.Reader, out io.Writer) int {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		resp, reply := handleRequest(reg, line)
		if !reply {
			continue
		}
		b, err := json.Marshal(resp)
		if err != nil {
			b, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcInternalError, Message: err.Error()}})
		}
		_, _ = out.Write(append(b, '\n'))
	}
	return 0
}

// handleRequest answers one line: the response and whether one is
// owed (a notification is not).
func handleRequest(reg *registry.Registry, line []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: "parse error: " + err.Error()}}, true
	}
	id := req.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	fail := func(code int, msg string, data any) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}}, !notification
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return fail(rpcInvalidRequest, "invalid request: jsonrpc \"2.0\" and a method are required", nil)
	}
	g, prefix, ok := reg.Resolve(req.Method)
	if !ok {
		return fail(rpcMethodNotFound, fmt.Sprintf("method %q is not one the CLI has", req.Method), map[string]any{"transport": MachineEnvelope})
	}
	argv, err := paramsToArgv(req.Params)
	if err != nil {
		return fail(rpcInvalidParams, "invalid params: "+err.Error(), nil)
	}
	var stdout, stderr bytes.Buffer
	g.Run(append(prefix, argv...), &stdout, &stderr)
	body := bytes.TrimSpace(stdout.Bytes())
	if !json.Valid(body) {
		return fail(rpcInternalError, "the verb rendered no envelope", map[string]any{"stdout": string(body), "stderr": stderr.String()})
	}
	if notification {
		return rpcResponse{}, false
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(body)}, true
}

// paramsToArgv turns JSON-RPC params into the argv the CLI's run
// function takes. Absent params is no argument; an array of strings
// is argv verbatim; an object maps each key to a `--key value` flag
// (true to a bare `--key`, an array to the flag repeated), with the
// reserved key "args" appended verbatim after the flags. Anything
// else is invalid params.
func paramsToArgv(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("params are an array of strings or an object of flags")
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if k != "args" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var argv []string
	for _, k := range keys {
		v := obj[k]
		var s string
		var b bool
		var arr []string
		switch {
		case json.Unmarshal(v, &s) == nil:
			argv = append(argv, "--"+k, s)
		case json.Unmarshal(v, &b) == nil:
			if b {
				argv = append(argv, "--"+k)
			}
		case json.Unmarshal(v, &arr) == nil:
			for _, item := range arr {
				argv = append(argv, "--"+k, item)
			}
		default:
			var n json.Number
			dec := json.NewDecoder(bytes.NewReader(v))
			dec.UseNumber()
			if err := dec.Decode(&n); err != nil {
				return nil, fmt.Errorf("flag %q is a string, a boolean, a number or an array of strings", k)
			}
			argv = append(argv, "--"+k, n.String())
		}
	}
	if rest, ok := obj["args"]; ok {
		var tail []string
		if err := json.Unmarshal(rest, &tail); err != nil {
			return nil, fmt.Errorf("args is an array of strings")
		}
		argv = append(argv, tail...)
	}
	return argv, nil
}
