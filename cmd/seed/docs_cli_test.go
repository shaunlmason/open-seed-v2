package main

// The docs surface through the CLI (plans/os-16e55c11.md D1): check
// passes clean against the committed tree, a subverb is required, and a
// generate into a fresh root then check comes back clean. The renderer
// drills live in internal/docs; this pins the CLI wiring and the exit
// codes it returns.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
)

func TestDocsCheckCleanOnCommitted(t *testing.T) {
	// The committed generated tree matches what the generator renders.
	e, code := runEnv(t, "docs", "check", "--root", "../..")
	if code != envelope.ExitOK || !e.OK {
		t.Fatalf("docs check must pass on the committed tree, got exit %d code %v", code, e.Error)
	}
}

func TestDocsRequiresSubverb(t *testing.T) {
	e, code := runEnv(t, "docs")
	if code != envelope.ExitUsage || e.Error == nil {
		t.Fatalf("docs with no subverb must be a usage error, got %d", code)
	}
	if _, code := runEnv(t, "docs", "bogus"); code != envelope.ExitUsage {
		t.Fatalf("an unknown docs subverb must be a usage error, got %d", code)
	}
}

// mirrorRoot builds a temp repository root carrying the sources the
// generator reads, symlinked to the real ones, and returns its path.
func mirrorRoot(t *testing.T) string {
	t.Helper()
	real, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "next"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"lanes", "internal", "spec"} {
		if err := os.Symlink(filepath.Join(real, d), filepath.Join(tmp, d)); err != nil {
			t.Fatal(err)
		}
	}
	// The conformance rendering reads the charter beside the table.
	if err := os.Symlink(filepath.Join(real, "SEED-NEXT.md"), filepath.Join(tmp, "SEED-NEXT.md")); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestDocsGenerateThenCheck(t *testing.T) {
	// Mirror the sources into a temp root, generate, and check clean.
	tmp := mirrorRoot(t)
	if _, code := runEnv(t, "docs", "generate", "--root", tmp); code != envelope.ExitOK {
		t.Fatalf("docs generate must succeed, got %d", code)
	}
	e, code := runEnv(t, "docs", "check", "--root", tmp)
	if code != envelope.ExitOK || !e.OK {
		t.Fatalf("docs check must pass after generate, got exit %d code %v", code, e.Error)
	}
	// A hand edit is caught as docs_drift (exit 28).
	f := filepath.Join(tmp, "docs", "generated", "lifecycle.md")
	b, _ := os.ReadFile(f)
	os.WriteFile(f, append(b, []byte("x\n")...), 0o644)
	e, code = runEnv(t, "docs", "check", "--root", tmp)
	if code != envelope.ExitDrift || e.Error == nil || e.Error.Code != envelope.CodeDocsDrift {
		t.Fatalf("a hand edit must fail docs_drift (exit 28), got exit %d code %v", code, e.Error)
	}
}

// The citation stage through the CLI (card os-5fe43832): a document
// naming a path the tree does not hold fails broken_citation on exit
// 28, and the message names the file, the line and the target so the
// reader does not have to search for it.
func TestDocsCheckRefusesABrokenCitation(t *testing.T) {
	// The drift stage runs first and reads the real sources, so the
	// root is the same partial mirror the generate-then-check drill
	// uses; the broken citation is added on top of a clean tree.
	tmp := mirrorRoot(t)
	if _, code := runEnv(t, "docs", "generate", "--root", tmp); code != envelope.ExitOK {
		t.Fatalf("docs generate must succeed, got %d", code)
	}
	if err := os.WriteFile(filepath.Join(tmp, "readme.md"),
		[]byte("intro\n\nsee [the plan](plan.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "docs", "check", "--root", tmp)
	if code != envelope.ExitDrift || e.Error == nil || e.Error.Code != envelope.CodeBrokenCitation {
		t.Fatalf("a broken citation must fail broken_citation (exit 28), got exit %d code %v", code, e.Error)
	}
	for _, want := range []string{"readme.md:3", "plan.md"} {
		if !strings.Contains(e.Error.Message, want) {
			t.Fatalf("the refusal does not name %q: %s", want, e.Error.Message)
		}
	}
}

// A clean check reports how many citations it held, so an operator can
// see the stage did work rather than find nothing to do.
func TestDocsCheckReportsTheCitationsHeld(t *testing.T) {
	e, code := runEnv(t, "docs", "check", "--root", "../..")
	if code != envelope.ExitOK || !e.OK {
		t.Fatalf("docs check must pass on the committed tree, got exit %d code %v", code, e.Error)
	}
	held, ok := e.Result["citations"].(float64)
	if !ok || held < 100 {
		t.Fatalf("citations = %v: a clean check must report the citations it held", e.Result["citations"])
	}
}
