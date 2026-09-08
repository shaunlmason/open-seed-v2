package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ledgerEnv struct {
	OK     bool           `json:"ok"`
	Result map[string]any `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Position    *string  `json:"position"`
	Affordances []string `json:"affordances"`
	Budget      *struct {
		Reserved  string `json:"reserved"`
		Remaining string `json:"remaining"`
	} `json:"budget"`
	Exit int `json:"exit"`
}

func runEnv(t *testing.T, args ...string) (ledgerEnv, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	var e ledgerEnv
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatalf("not an envelope: %v\n%s%s", err, out.String(), errOut.String())
	}
	return e, code
}

// conformance: III.A — the entire chain verifies from genesis with one
// command; III.I — every verb response is a position-stamped envelope with
// meaningful exit codes.
func TestLedgerRoundTrip(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")

	if e, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 || !e.OK {
		t.Fatalf("init failed: %d %+v", code, e)
	}
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "1" {
		t.Fatalf("append failed: %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "verify", "--ledger", ld)
	if code != 0 || !e.OK {
		t.Fatalf("verify failed: %d %+v", code, e)
	}
	if e.Result["count"].(float64) != 2 || e.Position == nil || *e.Position != "1" {
		t.Fatalf("verify result wrong: %+v", e)
	}
	e, code = runEnv(t, "ledger", "show", "--ledger", ld)
	if code != 0 || !e.OK || e.Result["count"].(float64) != 2 {
		t.Fatalf("show summary failed: %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "show", "--ledger", ld, "--position", "1")
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "1" {
		t.Fatalf("show record failed: %d %+v", code, e)
	}
	ev := e.Result["event"].(map[string]any)
	if ev["verb"] != "message.sent" {
		t.Fatalf("show returned wrong record: %+v", ev)
	}
	if _, code = runEnv(t, "ledger", "show", "--ledger", ld, "--position", "9"); code != 4 {
		t.Fatalf("show at a missing position must exit 4, got %d", code)
	}
}

// conformance: III.A — corruption refuses with exit 8 naming reason and
// position; version troubles refuse with exit 10 (plans/os-89412090.md).
func TestLedgerVerifyFailureExitCodes(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	if _, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`); code != 0 {
		t.Fatal("append failed")
	}

	segs, err := filepath.Glob(filepath.Join(ld, "segments", "*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatal("no segments")
	}
	b, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt a payload byte: exit 8 with reason and position in the message.
	corrupted := strings.Replace(string(b), `"n":1`, `"n":9`, 1)
	if corrupted == string(b) {
		t.Fatal("corruption did not apply")
	}
	if err := os.WriteFile(segs[0], []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "ledger", "verify", "--ledger", ld)
	if code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" {
		t.Fatalf("corrupted chain must exit 8 chain_invalid, got %d %+v", code, e)
	}
	if !strings.Contains(e.Error.Message, "position 1") || !strings.Contains(e.Error.Message, "bad_signature") {
		t.Fatalf("failure message must carry position and reason, got %q", e.Error.Message)
	}
	if e.Position == nil || *e.Position != "1" {
		t.Fatalf("verification refusals are position-stamped envelopes, got %+v", e.Position)
	}

	// Restore, then declare only an unsupported set: exit 10, stamped at
	// the position the refusal was computed at.
	if err := os.WriteFile(segs[0], b, 0o644); err != nil {
		t.Fatal(err)
	}
	e, code = runEnv(t, "ledger", "verify", "--ledger", ld, "--supported", "seed/9")
	if code != 10 || e.Error == nil || e.Error.Code != "version_unsupported" {
		t.Fatalf("unsupported version must exit 10 version_unsupported, got %d %+v", code, e)
	}
	if e.Position == nil || *e.Position != "0" {
		t.Fatalf("version refusals are position-stamped envelopes, got %+v", e.Position)
	}

	// A ledger not starting with genesis: exit 8.
	other := filepath.Join(dir, "not-genesis")
	if err := os.MkdirAll(filepath.Join(other, "segments"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if err := os.WriteFile(filepath.Join(other, "segments", "x.jsonl"), []byte(lines[len(lines)-1]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code = runEnv(t, "ledger", "verify", "--ledger", other)
	if code != 8 || e.Error == nil || !strings.Contains(e.Error.Message, "system.genesis") {
		t.Fatalf("genesis-less chain must exit 8 naming the genesis rule, got %d %+v", code, e)
	}
}

// conformance: III.A classification — a hostile payload refuses at exit 9
// with the lint's pointers in the message, before anything is written.
func TestLedgerAppendClassificationRefusal(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-0001", "--payload", hostile)
	if code != 9 || e.Error == nil || e.Error.Code != "classification_refused" {
		t.Fatalf("hostile payload must exit 9 classification_refused, got %d %+v", code, e)
	}
	if !strings.Contains(e.Error.Message, "/transcript") {
		t.Fatalf("refusal must carry the violation pointer, got %q", e.Error.Message)
	}
	if e2, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 || e2.Result["count"].(float64) != 1 {
		t.Fatalf("refused append must write nothing: %d %+v", code, e2)
	}
}

// conformance: plans/os-89412090.md boundary set — show never writes:
// inspecting a ledger in the crash window must not heal HEAD (#85 review).
func TestLedgerShowNeverWrites(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	if _, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`); code != 0 {
		t.Fatal("append failed")
	}
	headPath := filepath.Join(ld, "HEAD")
	if err := os.Remove(headPath); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "ledger", "show", "--ledger", ld)
	if code != 0 || !e.OK || e.Result["count"].(float64) != 2 {
		t.Fatalf("show must read the crash-window ledger, got %d %+v", code, e)
	}
	if _, ok := e.Result["head"]; ok {
		t.Fatalf("show must report the missing HEAD as found, got %+v", e.Result)
	}
	if _, code := runEnv(t, "ledger", "show", "--ledger", ld, "--position", "0"); code != 0 {
		t.Fatal("show --position failed")
	}
	if _, err := os.Stat(headPath); !os.IsNotExist(err) {
		t.Fatalf("show healed HEAD: stat says %v", err)
	}
	if e, code := runEnv(t, "ledger", "show", "--ledger", filepath.Join(dir, "absent")); code != 5 || e.Error == nil || e.Error.Code != "unavailable" {
		t.Fatalf("show on a missing ledger must exit 5 unavailable (and create nothing), got %d %+v", code, e)
	}
	if _, err := os.Stat(filepath.Join(dir, "absent")); !os.IsNotExist(err) {
		t.Fatalf("show created the missing ledger dir: %v", err)
	}
}

// conformance: spec/envelope.md "Position stamping" — every
// response that REACHED the ledger stamps it, and a not_found from
// show --position reached it hardest: the scan visited every record
// and therefore established the count (plans/os-fa69345e.md). Without
// the stamp, not_found on a chain of length N is indistinguishable
// from not_found on a chain of length 10,000 where the position never
// will exist, which is the one refusal where a concurrent append is
// most likely.
func TestLedgerShowNotFoundStampsTheTip(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	if _, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`); code != 0 {
		t.Fatal("append failed")
	}

	// Two records: positions 0 and 1, tip ordinal 1. Asking for the
	// position an append would land at next refuses, stamped, so the
	// caller can tell "not yet" from "never".
	e, code := runEnv(t, "ledger", "show", "--ledger", ld, "--position", "2")
	if code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("a missing position exits 4 not_found: %d %+v", code, e)
	}
	if e.Error.Message != "no record at position 2" {
		t.Fatalf("the message is unchanged: %q", e.Error.Message)
	}
	if e.Position == nil || *e.Position != "1" {
		t.Fatalf("the refusal carries the tip the scan established: %v", e.Position)
	}
	// Far past the tip is the same refusal with the same stamp: the
	// stamp reports the chain, never the request.
	if e, _ := runEnv(t, "ledger", "show", "--ledger", ld, "--position", "10000"); e.Position == nil || *e.Position != "1" {
		t.Fatalf("the stamp is the tip, not a function of the asked position: %v", e.Position)
	}

	// A record that IS there still carries the requested position.
	e, code = runEnv(t, "ledger", "show", "--ledger", ld, "--position", "0")
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "0" {
		t.Fatalf("a found record carries its own position: %d %+v", code, e.Position)
	}

	// chain_invalid is STAMPED with the failing position
	// (plans/os-37fcf7c6.md D1, the deliberate inversion of
	// plans/os-fa69345e.md D3's tripwire): the scan read position 0
	// and failed at 1, so the response was computed at 1, and verify
	// stamps the same chain the same way. Null is reserved for a
	// refusal raised before any position was read.
	segs, err := filepath.Glob(filepath.Join(ld, "segments", "*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatal("no segments")
	}
	b, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two records in one segment, got %d", len(lines))
	}
	lines[1] = `{"event": {`
	if err := os.WriteFile(segs[0], []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code = runEnv(t, "ledger", "show", "--ledger", ld, "--position", "2")
	if code != 8 || e.Error == nil || e.Error.Code != "chain_invalid" {
		t.Fatalf("an unparsable record exits 8 chain_invalid: %d %+v", code, e)
	}
	if e.Position == nil || *e.Position != "1" {
		t.Fatalf("a failed scan stamps the position it failed at: %s", stampOf(e.Position))
	}
	v, vcode := runEnv(t, "ledger", "verify", "--ledger", ld)
	if vcode != 8 || v.Position == nil || *v.Position != *e.Position {
		t.Fatalf("show and verify stamp one corrupted chain alike: show %s, verify %d %s", stampOf(e.Position), vcode, stampOf(v.Position))
	}
}

// conformance: spec/envelope.md — an empty chain establishes no
// position, so the same refusal carries null rather than inventing
// one. The helper declines at zero, so no branch has to say so.
func TestLedgerShowNotFoundOnAnEmptyChain(t *testing.T) {
	ld := filepath.Join(t.TempDir(), "ledger")
	if err := os.MkdirAll(filepath.Join(ld, "segments"), 0o755); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "ledger", "show", "--ledger", ld, "--position", "0")
	if code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("an empty chain has no position 0: %d %+v", code, e)
	}
	if e.Position != nil {
		t.Fatalf("an empty chain establishes no position: %v", *e.Position)
	}
}

// conformance: spec/protocol.md "Protocol version" — an append signs
// at the version active at the tip, and refuses when that version is
// outside the build's supported set (#85 review).
func TestLedgerAppendFollowsActiveVersion(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	// The upgrade event is the last event of the old version.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "seed/10"}`); code != 0 {
		t.Fatalf("upgrade append failed: %d %+v", code, e)
	}
	// A build supporting only seed/0 must refuse to grow the upgraded
	// chain, before writing anything.
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
	if code != 10 || e.Error == nil || e.Error.Code != "version_unsupported" {
		t.Fatalf("append past an upgrade must refuse at exit 10, got %d %+v", code, e)
	}
	if e.Position == nil || *e.Position != "1" {
		t.Fatalf("the refusal is stamped at the tip, got %+v", e.Position)
	}
	// A build supporting the new version signs at it.
	e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--supported", "seed/0,seed/10",
		"--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "2" {
		t.Fatalf("append with the upgraded set failed: %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "show", "--ledger", ld, "--position", "2")
	if code != 0 || !e.OK {
		t.Fatal("show failed")
	}
	if v := e.Result["event"].(map[string]any)["v"]; v != "seed/10" {
		t.Fatalf("the appended event must carry the active version, got %v", v)
	}
	// The grown chain is coherent: green under the upgraded set, version
	// trouble at the exact position under the old set.
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld, "--supported", "seed/0,seed/10"); code != 0 || e.Result["count"].(float64) != 3 {
		t.Fatalf("upgraded chain must verify under its set: %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "verify", "--ledger", ld)
	if code != 10 || e.Position == nil || *e.Position != "2" {
		t.Fatalf("old build must refuse the upgraded suffix at its position, got %d %+v", code, e)
	}
}

func TestLedgerUsageRefusals(t *testing.T) {
	for _, args := range [][]string{
		{"ledger"},
		{"ledger", "frobnicate"},
		{"ledger", "verify"},
		{"ledger", "append", "--ledger", "x"},
		{"ledger", "show"},
		{"ledger", "verify", "--ledger", "x", "extra"},
	} {
		if _, code := runEnv(t, args...); code != 64 {
			t.Errorf("%v must exit 64, got %d", args, code)
		}
	}
}

// stampOf prints an envelope's position for a failure message: the
// value, or <nil>, never the pointer.
func stampOf(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
