package main

// The governed-docs surface (plans/os-16e55c11.md D1): `docs generate`
// renders the lifecycle, capability, exit-code and per-lane documents
// from the tables the machinery reads and writes them under
// docs/generated/; `docs check` regenerates and diffs, failing
// docs_drift (a refinement of exit 28) when the committed output no
// longer matches the table it came from. `docs check` then holds every
// relative markdown citation in the tree to the tree (card
// os-5fe43832), failing broken_citation on the same exit when a
// document names a path that is not there.
//
// Read/write of checked-in files, not the ledger: like `lane`, it
// opens no ledger, journals no attempt and carries no position stamp —
// the docs derive from the source tree, not from a chain position.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/docs"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
)

func runDocs(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "docs requires a subverb: generate or check"), stdout, stderr)
	}
	switch args[0] {
	case "generate":
		return runDocsGenerate(args[1:], stdout, stderr)
	case "check":
		return runDocsCheck(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown docs subverb %q — generate or check", args[0])), stdout, stderr)
}

func runDocsGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docs generate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "repository root the generated docs are written under")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "docs generate takes [--root <dir>]"), stdout, stderr)
	}
	written, err := docs.Write(*root)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	list := make([]any, len(written))
	for i, w := range written {
		list[i] = w
	}
	return render(envelope.OK(map[string]any{"written": list, "count": len(written)}), stdout, stderr)
}

func runDocsCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docs check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "repository root the generated docs are checked against")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "docs check takes [--root <dir>]"), stdout, stderr)
	}
	drift, err := docs.Check(*root)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if len(drift) > 0 {
		return render(envelope.Fail(envelope.ExitDrift, envelope.CodeDocsDrift,
			fmt.Sprintf("generated docs are stale — run `seed docs generate`: %s", strings.Join(drift, ", "))), stdout, stderr)
	}
	// The citation stage (card os-5fe43832) runs after the drift stage
	// and never instead of it: a stale generated document is the older
	// failure and the cheaper fix, and reporting a citation inside a
	// file the generator is about to rewrite would name a line that is
	// already gone.
	held, broken, err := docs.CheckCitations(*root)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if len(broken) > 0 {
		named := make([]string, len(broken))
		for i, b := range broken {
			named[i] = b.String()
		}
		return render(envelope.Fail(envelope.ExitDrift, envelope.CodeBrokenCitation,
			fmt.Sprintf("broken citations: %s", strings.Join(named, "; "))), stdout, stderr)
	}
	return render(envelope.OK(map[string]any{"checked": true, "drift": []any{}, "citations": held}), stdout, stderr)
}
