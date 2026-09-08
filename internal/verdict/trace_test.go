package verdict

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// emitScript writes one OTLP/JSON export to SEED_TRACE_EXPORT with
// ids, a status message and an undeclared run id that differ per
// process: two roots (one carrying an error child), the harness run
// the drills compare across computations.
const emitScript = `#!/bin/sh
id=$$
cat > "$SEED_TRACE_EXPORT" <<EOT
{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc"}}]},"scopeSpans":[{"spans":[
{"traceId":"t1$id","spanId":"a$id","name":"sdk.execution","kind":1,"startTimeUnixNano":"$id","attributes":[{"key":"sdk.run_id","value":{"stringValue":"run-$id"}}]},
{"traceId":"t1$id","spanId":"b$id","parentSpanId":"a$id","name":"WorkspaceService.list","kind":1,"status":{"code":2,"message":"boom $id"},"attributes":[{"key":"herdr.outcome","value":{"stringValue":"failure"}},{"key":"herdr.reason","value":{"stringValue":"malformed_json"}},{"key":"sdk.run_id","value":{"stringValue":"run-$id"}}]},
{"traceId":"t2$id","spanId":"c$id","name":"PopupService.close","kind":1,"status":{"code":1},"attributes":[{"key":"herdr.outcome","value":{"stringValue":"success"}}]}
]}]}]}
EOT
`

// tracedRepo is repo with the emitting script and four specs at the
// spec commit: accept.md declares two attribute keys, bare.md declares
// none, dup.md declares one twice, and mal.md writes a non-JSON export.
func tracedRepo(t *testing.T) (dir, base, spec, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		return gitOut(t, dir, args...)
	}
	run("init", "--quiet", "-b", "main")
	hardenGitRepo(t, dir)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hello.txt", "hello\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "base")
	base = strings.TrimSpace(run("rev-parse", "HEAD"))
	write("emit.sh", emitScript)
	commands := "## Validation Commands\n\n- Boundary: `sh emit.sh`\n- `test -f hello.txt`\n\n"
	write("accept.md", "# Acceptance\n\n"+commands+"## Trace attributes\n\n- `herdr.outcome`\n- herdr.reason\n")
	write("bare.md", "# Bare\n\n"+commands)
	write("dup.md", "# Dup\n\n"+commands+"## Trace attributes\n\n- herdr.outcome\n- herdr.outcome\n")
	write("mal.md", "# Malformed\n\n## Validation Commands\n\n- Boundary: `printf 'not json' > \"$SEED_TRACE_EXPORT\"`\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "spec")
	spec = strings.TrimSpace(run("rev-parse", "HEAD"))
	write("hello.txt", "changed\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "head")
	head = strings.TrimSpace(run("rev-parse", "HEAD"))
	return dir, base, spec, head
}

// conformance: III.G row 5 via plans/os-7fc2ca38.md D2, D3, D4 — a
// command that writes an export to SEED_TRACE_EXPORT yields one receipt
// entry with the shape digest and counts; a second computation from a
// fresh workspace, with fresh ids, times and run id, reproduces the
// receipt digest; the export path is outside the inventory; a sealed
// command's export lands in sealed_traces; a spec with no declaration
// retains no attributes; a duplicate key refuses spec_unrunnable; a
// malformed export binds nothing and says so; a receipt without traces
// carries neither field.
func TestReceiptBindsTraceShape(t *testing.T) {
	dir, base, spec, head := tracedRepo(t)
	in := Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gated("accept.md", spec)}
	r1, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Transcripts) != 2 || r1.Transcripts[0].Exit != 0 {
		t.Fatalf("both commands ran green: %+v", r1.Transcripts)
	}
	if len(r1.Traces) != 1 || r1.Traces[0].Transcript != 0 || len(r1.Traces[0].ShapeSHA256) != 64 || r1.Traces[0].Malformed {
		t.Fatalf("one entry for the emitting command: %+v", r1.Traces)
	}
	if r1.Traces[0].Spans != 3 || r1.Traces[0].Errors != 1 {
		t.Fatalf("counts derive from the shape: %+v", r1.Traces[0])
	}
	if len(r1.SealedTraces) != 0 || len(r1.Evidence) != 1 || len(r1.Evidence[0].Raw) == 0 {
		t.Fatalf("the evidence carries the shape and the raw export: %d sealed, %d evidence", len(r1.SealedTraces), len(r1.Evidence))
	}
	shape := string(r1.ShapeBytes(false, 0))
	for _, stripped := range []string{"run-", "boom", "traceId", "spanId", "startTime", "sdk.run_id", "service.name"} {
		if strings.Contains(shape, stripped) {
			t.Fatalf("the shape strips %q: %s", stripped, shape)
		}
	}
	if !strings.Contains(shape, `"herdr.reason":"malformed_json"`) || !strings.Contains(shape, `"status":"error"`) {
		t.Fatalf("the shape keeps declared attributes and the status code: %s", shape)
	}
	for _, f := range r1.Files {
		if strings.Contains(f, "traces") {
			t.Fatalf("the export path is outside the inventory: %v", r1.Files)
		}
	}
	d1, _ := r1.Digest()
	r2, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := r2.Digest()
	if d1 != d2 {
		c1, _ := r1.Canonical()
		c2, _ := r2.Canonical()
		t.Fatalf("a fresh run with fresh ids reproduces the receipt:\n%s\n%s", c1, c2)
	}
	c1, _ := r1.Canonical()
	if !strings.Contains(string(c1), `"traces":[{"errors":1,"shape_sha256":"`) || strings.Contains(string(c1), "sealed_traces") {
		t.Fatalf("the canonical form carries traces and no sealed_traces: %s", c1)
	}
	// Round trip through JSON keeps the entries.
	var back Receipt
	if err := json.Unmarshal(c1, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Traces) != 1 || back.Traces[0] != r1.Traces[0] {
		t.Fatalf("entries survive a round trip: %+v vs %+v", back.Traces, r1.Traces)
	}
	if e := back.TraceEntry(false, 0); e == nil || e.ShapeSHA256 != r1.Traces[0].ShapeSHA256 {
		t.Fatalf("TraceEntry finds transcript 0: %+v", e)
	}
	if back.TraceEntry(false, 1) != nil || back.TraceEntry(true, 0) != nil || back.ShapeBytes(false, 0) != nil {
		t.Fatal("no entry for the silent command, none sealed, and no shape bytes on a receipt read back")
	}

	// The sealed half: the same script as a sealed check binds into
	// sealed_traces with a different shape (the sealed flag is part of
	// it), under the visible spec's declaration.
	sealed := in
	sealed.Sealed = &SealedInput{Commitment: strings.Repeat("ab", 32), Checks: []string{"sh emit.sh"}}
	rs, err := Compute(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.SealedTraces) != 1 || rs.SealedTraces[0].Transcript != 0 || rs.SealedTraces[0].ShapeSHA256 == rs.Traces[0].ShapeSHA256 {
		t.Fatalf("the sealed command's export lands in sealed_traces with its own shape: %+v", rs.SealedTraces)
	}
	if rs.SealedTraces[0].Spans != 3 || rs.ShapeBytes(true, 0) == nil {
		t.Fatalf("the sealed shape is the same tree: %+v", rs.SealedTraces[0])
	}
	cs, _ := rs.Canonical()
	if !strings.Contains(string(cs), `"sealed_traces":[{"errors":1`) {
		t.Fatalf("sealed_traces in the canonical form: %s", cs)
	}

	// No declaration: name, kind and status alone reproduce.
	rb, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gatedAt("bare.md", spec)})
	if err != nil {
		t.Fatal(err)
	}
	if bare := string(rb.ShapeBytes(false, 0)); strings.Contains(bare, "herdr") || !strings.Contains(bare, `"attributes":{}`) {
		t.Fatalf("an undeclared spec retains no attributes: %s", bare)
	}
	if rb.Traces[0].ShapeSHA256 == r1.Traces[0].ShapeSHA256 {
		t.Fatal("the declaration is part of the shape")
	}

	// A duplicate key refuses spec_unrunnable naming the part.
	var su *SpecUnrunnableError
	if _, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gatedAt("dup.md", spec)}); !errors.As(err, &su) || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a duplicate declared key refuses spec_unrunnable naming it, got %v", err)
	}

	// A malformed export is a fact, not a refusal, and reproduces.
	rm, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gatedAt("mal.md", spec)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rm.Traces) != 1 || !rm.Traces[0].Malformed || rm.Traces[0].ShapeSHA256 != "" || len(rm.Evidence) != 0 {
		t.Fatalf("a malformed export binds nothing and says so: %+v", rm.Traces)
	}
	cm, _ := rm.Canonical()
	if !strings.Contains(string(cm), `"traces":[{"malformed":true,"transcript":0}]`) {
		t.Fatalf("the malformed entry is exactly {transcript, malformed}: %s", cm)
	}
	rm2, _ := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gatedAt("mal.md", spec)})
	dm, _ := rm.Digest()
	dm2, _ := rm2.Digest()
	if dm != dm2 {
		t.Fatal("a malformed entry reproduces")
	}

	// A run whose commands write nothing carries neither field: every
	// earlier receipt's canonical bytes are unchanged.
	pdir, pbase, pspec, phead := repo(t)
	rp, err := Compute(Input{RepoDir: pdir, Contract: "c-1", Base: pbase + ".." + phead, Acceptance: gated("accept.md", pspec)})
	if err != nil {
		t.Fatal(err)
	}
	cp, _ := rp.Canonical()
	if strings.Contains(string(cp), "traces") || rp.Traces != nil || rp.Evidence != nil {
		t.Fatalf("a receipt without exports carries no trace field: %s", cp)
	}
}

// conformance: plans/os-7fc2ca38.md D6 — a scorecard cites a span by
// its path in the shape the run just produced, or in the stored shape
// when the receipt was read back; a path beyond the tree, a transcript
// with no entry, a sealed citation against a visible transcript and a
// malformed entry each refuse naming the citation.
func TestScorecardCitesTraceSpansInPackage(t *testing.T) {
	dir, base, spec, head := tracedRepo(t)
	r, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gated("accept.md", spec)})
	if err != nil {
		t.Fatal(err)
	}
	transcripts := len(r.Transcripts)
	// Two roots sort by canonical bytes; whichever is first, its
	// path resolves and 0.0 resolves only under the root with a child.
	if err := resolveEvidence("trace:0/0", transcripts, "", r, nil); err != nil {
		t.Fatalf("the first root resolves: %v", err)
	}
	if err := resolveEvidence("trace:0/1", transcripts, "", r, nil); err != nil {
		t.Fatalf("the second root resolves: %v", err)
	}
	okChild := resolveEvidence("trace:0/0.0", transcripts, "", r, nil) == nil
	okOther := resolveEvidence("trace:0/1.0", transcripts, "", r, nil) == nil
	if okChild == okOther {
		t.Fatal("exactly one root carries the child")
	}
	for _, bad := range []struct{ ev, why string }{
		{"trace:0/2", "no span"},
		{"trace:0/0.0.0", "no span"},
		{"trace:1/0", "no trace entry"},
		{"trace:7/0", "no trace entry"},
		{"sealed-trace:0/0", "no trace entry"},
		{"trace:0/", "neither"},
		{"trace:0/a", "neither"},
		{"trace:/0", "neither"},
	} {
		err := resolveEvidence(bad.ev, transcripts, "", r, nil)
		if err == nil || !strings.Contains(err.Error(), bad.why) || !strings.Contains(err.Error(), bad.ev) {
			t.Fatalf("%q refuses naming itself and %q, got %v", bad.ev, bad.why, err)
		}
	}
	// A receipt read back carries no shape bytes: with no store the
	// citation cannot resolve, with a store holding the shape it does.
	canon, _ := r.Canonical()
	var back Receipt
	if err := json.Unmarshal(canon, &back); err != nil {
		t.Fatal(err)
	}
	if err := resolveEvidence("trace:0/0", transcripts, "", &back, nil); err == nil || !strings.Contains(err.Error(), "no store") {
		t.Fatalf("a read-back receipt needs the store: %v", err)
	}
	store := openStore(t)
	if _, err := store.Put(r.ShapeBytes(false, 0)); err != nil {
		t.Fatal(err)
	}
	if err := resolveEvidence("trace:0/0", transcripts, "", &back, store); err != nil {
		t.Fatalf("the stored shape resolves the citation: %v", err)
	}
	if err := resolveEvidence("trace:0/0", transcripts, "", &back, openStore(t)); err == nil || !strings.Contains(err.Error(), "not retrievable") {
		t.Fatalf("a store without the shape refuses naming it: %v", err)
	}
	// A malformed entry cannot be cited.
	rm, _ := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gatedAt("mal.md", spec)})
	if err := resolveEvidence("trace:0/0", 1, "", rm, nil); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("a malformed export cannot be cited: %v", err)
	}
	// The receipt-less case names the citation too.
	if err := resolveEvidence("trace:0/0", 0, "", nil, nil); err == nil {
		t.Fatal("no receipt, no citation")
	}
}

func openStore(t *testing.T) *artifact.Store {
	t.Helper()
	return artifact.Open(filepath.Join(t.TempDir(), "artifacts"))
}

// gatedAt is gated for a spec other than accept.md.
func gatedAt(path, commit string) *transition.AcceptanceInfo {
	return &transition.AcceptanceInfo{Ref: path + " @ " + commit, Executable: true, Gated: true}
}
