package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type env struct {
	V      string         `json:"v"`
	OK     bool           `json:"ok"`
	Result map[string]any `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Exit int `json:"exit"`
}

func drive(t *testing.T, args ...string) (env, int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
	var e env
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, out.String())
	}
	return e, code, out.String()
}

// conformance: III.I groundwork; the CLI is the complete interface and every
// verb response is a versioned envelope with a meaningful exit code.
func TestVersionVerb(t *testing.T) {
	e, code, raw := drive(t, "version")
	if code != 0 || e.Exit != 0 || !e.OK {
		t.Fatalf("version: code=%d envelope=%s", code, raw)
	}
	if e.V != "seed-envelope/0" {
		t.Fatalf("envelope version %q, want seed-envelope/0", e.V)
	}
	if e.Result["name"] != "seed" {
		t.Fatalf("result.name = %v, want seed", e.Result["name"])
	}
	if e.Result["protocol"] != "seed/0" {
		t.Fatalf("result.protocol = %v, want seed/0", e.Result["protocol"])
	}
	if e.Result["version"] == "" {
		t.Fatal("result.version must not be empty")
	}
	if strings.Count(raw, "\n") != 1 {
		t.Fatalf("expected exactly one envelope line, got %q", raw)
	}
}

func TestUnknownVerbIsUsage(t *testing.T) {
	e, code, raw := drive(t, "frobnicate")
	if code != 64 || e.Exit != 64 || e.OK {
		t.Fatalf("unknown verb: code=%d envelope=%s", code, raw)
	}
	if e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("unknown verb must refuse with error code usage, got %s", raw)
	}
	if !strings.Contains(e.Error.Message, "frobnicate") {
		t.Fatalf("refusal should name the verb, got %q", e.Error.Message)
	}
}

func TestVersionRefusesExtraOperands(t *testing.T) {
	for _, extra := range [][]string{{"version", "--bogus"}, {"version", "now"}, {"version", "-v", "x"}} {
		e, code, raw := drive(t, extra...)
		if code != 64 || e.Exit != 64 || e.OK || e.Error == nil || e.Error.Code != "usage" {
			t.Fatalf("version with extra operands %v must refuse with usage/64, got code=%d envelope=%s", extra[1:], code, raw)
		}
	}
}

func TestNoArgsIsUsage(t *testing.T) {
	e, code, _ := drive(t)
	if code != 64 || e.Exit != 64 || e.OK || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("missing verb must refuse with usage/64, got code=%d err=%+v", code, e.Error)
	}
}
