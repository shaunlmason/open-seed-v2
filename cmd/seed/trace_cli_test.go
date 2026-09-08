package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// traceEmit writes one OTLP/JSON export to SEED_TRACE_EXPORT with a
// per-process id in every field the shape strips.
const traceEmit = `#!/bin/sh
id=$$
cat > "$SEED_TRACE_EXPORT" <<EOT
{"resourceSpans":[{"scopeSpans":[{"spans":[
{"traceId":"t1$id","spanId":"a$id","name":"sdk.execution","kind":1,"startTimeUnixNano":"$id","attributes":[{"key":"sdk.run_id","value":{"stringValue":"run-$id"}}]},
{"traceId":"t1$id","spanId":"b$id","parentSpanId":"a$id","name":"WorkspaceService.list","kind":1,"status":{"code":2,"message":"boom $id"},"attributes":[{"key":"herdr.outcome","value":{"stringValue":"failure"}},{"key":"herdr.reason","value":{"stringValue":"malformed_json"}}]},
{"traceId":"t2$id","spanId":"c$id","name":"PopupService.close","kind":1,"status":{"code":1}}
]}]}]}
EOT
`

// traceStand is one ledger at seed/4 with a verdict-granted verifier
// and a repository whose spec commit carries a traced, rubric-bearing
// spec (accept.md), a silent one (quiet.md) and a malformed-export one
// (mal.md).
type traceStand struct {
	ld, src, priv, vkey, base, specCommit, head string
	rootKey                                     ed25519.PrivateKey
	verifierRaw                                 ed25519.PrivateKey
}

func newTraceStand(t *testing.T) *traceStand {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", src, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, src)
	write("hello.txt", "hello\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "base")
	base := git("rev-parse", "HEAD")
	write("emit.sh", traceEmit)
	write("accept.md", "# Traced\n\n## Validation Commands\n\n- Boundary: `sh emit.sh`\n- `test -f hello.txt`\n\n## Trace attributes\n\n- herdr.outcome\n- herdr.reason\n\n## Rubric\n\n- taste: the failing span is the malformed response\n")
	write("quiet.md", "# Quiet\n\n## Validation Commands\n\n- Boundary: `printf ok`\n")
	write("mal.md", "# Malformed\n\n## Validation Commands\n\n- Boundary: `printf 'not json' > \"$SEED_TRACE_EXPORT\"`\n\n## Rubric\n\n- taste: judged\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "specs")
	specCommit := git("rev-parse", "HEAD")
	write("hello.txt", "changed\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "head")
	head := git("rev-parse", "HEAD")

	vkey, vpub, vfp := writeWorkerKey(t, 9)
	for _, step := range [][]string{
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed1 + `"}`},
		{"actor.enrolled", vfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, vpub)},
		{"actor.granted", vfp, `{"capability": "verdict"}`},
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed2 + `"}`},
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed3 + `"}`},
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed4 + `"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	verifierRaw := workerRawKey(9)
	if fp, _ := event.Fingerprint(verifierRaw.Public().(ed25519.PublicKey)); fp != vfp {
		t.Fatalf("workerRawKey(9) is the key writeWorkerKey(9) wrote: %s vs %s", fp, vfp)
	}
	return &traceStand{ld: ld, src: src, priv: priv, vkey: vkey, base: base, specCommit: specCommit, head: head,
		rootKey: ed25519.NewKeyFromSeed(seed), verifierRaw: verifierRaw}
}

// drive files a trivial contract on the spec and submits the range;
// returns the submission position.
func (st *traceStand) drive(t *testing.T, subject, spec string) int {
	t.Helper()
	rawAppendAt(t, st.ld, st.rootKey, version.Seed4, "intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	rawAppendAt(t, st.ld, st.rootKey, version.Seed4, "contract.specified", subject, fmt.Sprintf(`{"acceptance": {"ref": "%s @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, spec, st.specCommit, st.specCommit))
	fence := rawAppendAt(t, st.ld, st.rootKey, version.Seed4, "claim.taken", subject, `{}`)
	return rawAppendAt(t, st.ld, st.rootKey, version.Seed4, "submission.made", subject, fmt.Sprintf(
		`{"fence": "%d", "packet": {"acceptance": ["%s ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, subject, st.base+".."+st.head))
}

func (st *traceStand) storePath(digest string) string {
	return filepath.Join(st.src, "var", "artifacts", "sha256", digest)
}

func (st *traceStand) scorecard(t *testing.T, subject string, sub int, evidence string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scorecard.json")
	body := fmt.Sprintf(`{"contract": %q, "submission": "%d", "items": [{"id": "taste", "score": "pass", "evidence": [%q], "uncertainty": "low"}]}`, subject, sub, evidence)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// tracesOf reads the rows `verdict traces` rendered.
func tracesOf(t *testing.T, e ledgerEnv) []map[string]any {
	t.Helper()
	rows, _ := e.Result["traces"].([]any)
	out := []map[string]any{}
	for _, r := range rows {
		m, _ := r.(map[string]any)
		out = append(out, m)
	}
	return out
}

// conformance: III.G rows 5 and 8 via plans/os-7fc2ca38.md D5, D6, D7
// (TestTraceArtifactsStoredAndChecked, TestScorecardCitesTraceSpans and
// TestVerdictTracesRenders in one stand) — verdict receipt stores the
// shape under the receipt's digest and the raw export as its sidecar;
// verdict traces renders the tree with citation paths; a scorecard
// citing a span renders and every unresolvable citation refuses at
// usage naming it; verdict check verifies the shapes; erasing the raw
// sidecar leaves check green; erasing the shape refuses at exit 21
// naming it and reconcile grades evidence_missing; a raw-pushed receipt
// with an invented entry recomputes differently and refuses at 21; a
// silent spec's receipt renders no traces; a malformed export renders
// by name and cannot be cited; a subject with nothing cited refuses
// not_found.
func TestTraceArtifactsStoredAndChecked(t *testing.T) {
	st := newTraceStand(t)
	sub := st.drive(t, "c-1", "accept.md")

	// The receipt: one traced transcript, the shape and the raw export
	// stored beside it.
	e, code := runEnv(t, "verdict", "receipt", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src)
	if code != 0 || !e.OK || e.Result["traces"] != "1" || e.Result["transcripts"] != "2" {
		t.Fatalf("verdict receipt: %d %+v", code, e)
	}
	digest, _ := e.Result["receipt"].(string)
	body, err := os.ReadFile(st.storePath(digest))
	if err != nil {
		t.Fatal(err)
	}
	var r verdict.Receipt
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Traces) != 1 || r.Traces[0].Spans != 3 || r.Traces[0].Errors != 1 {
		t.Fatalf("the stored receipt binds one shape: %+v", r.Traces)
	}
	shape := r.Traces[0].ShapeSHA256
	store := artifact.Open(filepath.Join(st.src, "var", "artifacts"))
	shapeBody, err := store.Get(shape)
	if err != nil {
		t.Fatalf("the shape is stored under the receipt's digest: %v", err)
	}
	raw, err := store.TraceRaw(shape)
	if err != nil || raw == "" {
		t.Fatalf("the raw export is the shape's sidecar: %q %v", raw, err)
	}
	rawBody, err := store.Get(raw)
	if err != nil || !strings.Contains(string(rawBody), "run-") || strings.Contains(string(shapeBody), "run-") {
		t.Fatalf("the raw export keeps what the shape strips: %v", err)
	}

	// verdict traces renders the tree from the stored receipt.
	e, code = runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src, "--receipt", digest)
	if code != 0 || !e.OK || e.Result["receipt_from"] != "flag" {
		t.Fatalf("verdict traces before a verdict reads --receipt: %d %+v", code, e)
	}
	rows := tracesOf(t, e)
	if len(rows) != 1 || rows[0]["shape"] != shape || rows[0]["raw"] != raw || rows[0]["sealed"] != false {
		t.Fatalf("one row naming the shape and its sidecar: %+v", rows)
	}
	cite := ""
	nodes, _ := rows[0]["nodes"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("three nodes: %+v", nodes)
	}
	for _, n := range nodes {
		m := n.(map[string]any)
		if m["name"] == "WorkspaceService.list" {
			cite, _ = m["cite"].(string)
			attrs, _ := m["attributes"].(map[string]any)
			if m["status"] != "error" || m["depth"] != float64(1) || attrs["herdr.reason"] != "malformed_json" || attrs["sdk.run_id"] != nil {
				t.Fatalf("the failing span renders its status and declared attributes: %+v", m)
			}
		}
	}
	if !strings.HasPrefix(cite, "trace:0/") || !strings.Contains(cite, ".") {
		t.Fatalf("the failing span is cited as a child path: %q", cite)
	}

	// The scorecard cites the span; unresolvable citations refuse
	// naming themselves.
	for _, bad := range []string{"trace:0/9", "trace:0/" + strings.SplitN(cite, "/", 2)[1] + ".0", "trace:1/0", "sealed-trace:0/0", "trace:0/x"} {
		e, code := runEnv(t, "verdict", "render", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src, "--key", st.vkey, "--verdict", "pass", "--scorecard", st.scorecard(t, "c-1", sub, bad))
		if code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, bad) {
			t.Fatalf("citation %q refuses at usage naming itself: %d %+v", bad, code, e)
		}
	}
	e, code = runEnv(t, "verdict", "render", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src, "--key", st.vkey, "--verdict", "pass", "--scorecard", st.scorecard(t, "c-1", sub, cite))
	if code != 0 || !e.OK || e.Result["verdict"] != "pass" || e.Result["receipt"] != digest {
		t.Fatalf("render over a span citation: %d %+v", code, e)
	}
	e, code = runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src)
	if code != 0 || e.Result["receipt_from"] != "verdict" || len(tracesOf(t, e)) != 1 {
		t.Fatalf("verdict traces reads the verdict's receipt: %d %+v", code, e)
	}
	if e, code := runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src, "--transcript", "1"); code != 0 || len(tracesOf(t, e)) != 0 {
		t.Fatalf("--transcript narrows to one transcript: %d %+v", code, e)
	}

	// check verifies the shapes beside the receipt.
	e, code = runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src)
	if code != 0 || e.Result["traces"] != "verified" || e.Result["artifact"] != "verified" {
		t.Fatalf("check verifies the traces: %d %+v", code, e)
	}
	// Erasing the raw sidecar's content costs a check nothing.
	if err := os.Remove(st.storePath(raw)); err != nil {
		t.Fatal(err)
	}
	if e, code = runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src); code != 0 || e.Result["traces"] != "verified" {
		t.Fatalf("an erased raw export leaves check green: %d %+v", code, e)
	}
	e, _ = runEnv(t, "reconcile", "--ledger", st.ld, "--repo", st.src, "--subject", "c-1")
	if classesOf(t, e)["evidence_missing"] != 0 {
		t.Fatalf("an erased raw export is no missing evidence: %+v", e.Result)
	}
	// Erasing the shape is missing evidence: check red naming it,
	// reconcile grades evidence_missing.
	if err := os.Remove(st.storePath(shape)); err != nil {
		t.Fatal(err)
	}
	if e, code = runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src); code != 21 || e.Error == nil || !strings.Contains(e.Error.Message, shape) {
		t.Fatalf("an erased shape refuses receipt_mismatch naming it: %d %+v", code, e)
	}
	e, _ = runEnv(t, "reconcile", "--ledger", st.ld, "--repo", st.src, "--subject", "c-1")
	if classesOf(t, e)["evidence_missing"] != 1 {
		t.Fatalf("an erased shape surfaces evidence_missing: %+v", e.Result)
	}
	e, code = runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src)
	if code != 0 || tracesOf(t, e)[0]["shape_missing"] == nil {
		t.Fatalf("verdict traces names a missing shape: %d %+v", code, e)
	}
	// Erasing through the verb erases the shape's sidecar pointer too.
	if err := os.WriteFile(st.storePath(shape), shapeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code = runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src); code != 0 {
		t.Fatalf("the restored shape checks green again: %d %+v", code, e)
	}
	removed, err := store.Erase(shape)
	if err != nil || strings.Join(removed, ",") != "content,trace_raw" {
		t.Fatalf("erasing the shape digest empties its content and its sidecar pointer: %v %v", removed, err)
	}
	if err := os.WriteFile(st.storePath(shape), shapeBody, 0o644); err != nil {
		t.Fatal(err)
	}

	// A raw-pushed receipt with an invented entry: the invented shape
	// digest names stored content (the receipt itself), so retrieval
	// passes and the recomputation refuses.
	invented := strings.Replace(string(body), shape, digest, 1)
	inventedDigest, err := store.Put([]byte(invented))
	if err != nil {
		t.Fatal(err)
	}
	rawAppendAt(t, st.ld, st.verifierRaw, version.Seed4, "verdict.rendered", "c-1", fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`, inventedDigest, sub))
	if e, code = runEnv(t, "verdict", "check", "--ledger", st.ld, "--subject", "c-1", "--repo", st.src); code != 21 || !strings.Contains(e.Error.Message, "recomputation") {
		t.Fatalf("an invented entry recomputes differently: %d %+v", code, e)
	}

	// The silent spec: no traces anywhere.
	st.drive(t, "c-2", "quiet.md")
	e, code = runEnv(t, "verdict", "receipt", "--ledger", st.ld, "--subject", "c-2", "--repo", st.src)
	if code != 0 || e.Result["traces"] != nil {
		t.Fatalf("a silent spec's receipt carries no traces: %d %+v", code, e)
	}
	quiet, _ := e.Result["receipt"].(string)
	e, code = runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-2", "--repo", st.src, "--receipt", quiet)
	if code != 0 || len(tracesOf(t, e)) != 0 {
		t.Fatalf("verdict traces renders nothing for a pre-trace receipt: %d %+v", code, e)
	}
	if e, code = runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-2", "--repo", st.src); code != 4 {
		t.Fatalf("no verdict, no deferral, no --receipt: not_found, got %d %+v", code, e)
	}

	// The malformed export: named, and uncitable.
	malSub := st.drive(t, "c-3", "mal.md")
	e, code = runEnv(t, "verdict", "receipt", "--ledger", st.ld, "--subject", "c-3", "--repo", st.src)
	if code != 0 || e.Result["traces"] != "1" {
		t.Fatalf("a malformed export is an entry: %d %+v", code, e)
	}
	mal, _ := e.Result["receipt"].(string)
	e, code = runEnv(t, "verdict", "traces", "--ledger", st.ld, "--subject", "c-3", "--repo", st.src, "--receipt", mal)
	if code != 0 || len(tracesOf(t, e)) != 1 || tracesOf(t, e)[0]["malformed"] != true {
		t.Fatalf("verdict traces names a malformed entry: %d %+v", code, e)
	}
	if e, code = runEnv(t, "verdict", "render", "--ledger", st.ld, "--subject", "c-3", "--repo", st.src, "--key", st.vkey, "--verdict", "pass", "--scorecard", st.scorecard(t, "c-3", malSub, "trace:0/0")); code != 64 || !strings.Contains(e.Error.Message, "malformed") {
		t.Fatalf("a malformed export cannot be cited: %d %+v", code, e)
	}
}
