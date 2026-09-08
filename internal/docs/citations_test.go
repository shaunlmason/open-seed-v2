package docs

// The citation drills (card os-5fe43832): every relative inline link in
// the tree resolves, a broken one is named with its file, line and
// target, and the two regions a link must not be read from (fenced
// blocks and inline code spans) are masked before anything is read.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree writes a map of repo-relative path to content into a fresh
// directory and returns its root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// held runs the check and fails the test on an error.
func held(t *testing.T, root string) (int, []BrokenCitation) {
	t.Helper()
	n, broken, err := CheckCitations(root)
	if err != nil {
		t.Fatalf("CheckCitations: %v", err)
	}
	return n, broken
}

// TestRepoCitationsHold is the gate itself: the committed tree names
// nothing it does not hold. The count assertion is the other half, and
// the load-bearing one: a walk that stopped finding documents would
// report zero findings and pass, so a gate that checks nothing must not
// read as a gate that passed.
func TestRepoCitationsHold(t *testing.T) {
	n, broken := held(t, repoRoot)
	if len(broken) != 0 {
		t.Fatalf("the tree does not hold %d citation(s): %v", len(broken), broken)
	}
	if n < 100 {
		t.Fatalf("held only %d citations: the walk is not reaching the tree's documents", n)
	}
}

func TestBrokenTargetIsNamed(t *testing.T) {
	root := tree(t, map[string]string{
		"docs/a.md": "intro\n\nsee [the plan](plan.md) for more\n",
	})
	n, broken := held(t, root)
	if n != 1 || len(broken) != 1 {
		t.Fatalf("held %d, broken %d: want one of each", n, len(broken))
	}
	got := broken[0]
	if got.File != "docs/a.md" || got.Line != 3 || got.Target != "plan.md" {
		t.Fatalf("finding does not name the citation: %+v", got)
	}
	if got.Reason != reasonMissing {
		t.Fatalf("reason = %q, want %q", got.Reason, reasonMissing)
	}
	if s := got.String(); !strings.Contains(s, "docs/a.md:3") || !strings.Contains(s, "plan.md") {
		t.Fatalf("String does not name file, line and target: %s", s)
	}
}

// A target resolves against the document's own directory, never the
// root: this is the failure that put four broken links into
// docs/CONTRIBUTING-AGENTS.md, where targets were written root-relative
// from inside docs/.
func TestTargetResolvesAgainstTheDocument(t *testing.T) {
	root := tree(t, map[string]string{
		"docs/a.md":         "[x](docs/b.md) and [y](b.md)\n",
		"docs/b.md":         "b\n",
		"docs/docs/keep.md": "keep\n",
	})
	_, broken := held(t, root)
	if len(broken) != 1 || broken[0].Target != "docs/b.md" {
		t.Fatalf("want the root-relative target refused and the sibling held, got %v", broken)
	}
}

func TestFencedBlockIsNotRead(t *testing.T) {
	for _, fence := range []string{"```", "~~~", "````", "   ```"} {
		src := "before\n" + fence + "\n[x](missing.md)\n" + strings.TrimLeft(fence, " ") + "\nafter\n"
		root := tree(t, map[string]string{"a.md": src})
		n, broken := held(t, root)
		if n != 0 || len(broken) != 0 {
			t.Fatalf("fence %q: held %d, broken %v: a link inside a fence is not a link", fence, n, broken)
		}
	}
}

// The false positive that a naive sweep reports: a regex in prose is
// link-shaped, and it sits inside a code span.
func TestCodeSpanIsNotRead(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "Names must match `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, max 64 chars.\n",
	})
	n, broken := held(t, root)
	if n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: a regex in a code span is not a citation", n, broken)
	}
}

// A code span opened by a run of n backticks closes on a run of exactly
// n, so a span that quotes backticks masks its whole body.
func TestMultiBacktickSpanIsNotRead(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "the literal ``[x](missing.md)`` stays prose\n",
	})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v", n, broken)
	}
}

// A backtick run with no closer on its line is literal text, exactly as
// a renderer treats it, so a link beside it is still read.
func TestUnclosedCodeSpanLeavesTheLineReadable(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "a stray ` tick and [x](missing.md)\n",
	})
	if _, broken := held(t, root); len(broken) != 1 {
		t.Fatalf("want the link read past an unclosed span, got %v", broken)
	}
}

func TestUnclosedFenceMasksToEndOfFile(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "before\n```\n[x](missing.md)\nstill inside\n",
	})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: an unclosed fence runs to the end", n, broken)
	}
}

// Masking preserves newlines, so a finding after a fenced block still
// names the line the reader will open.
func TestLineNumbersSurviveMasking(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "one\n```\ntwo\n```\nfour\n[x](missing.md)\n",
	})
	_, broken := held(t, root)
	if len(broken) != 1 || broken[0].Line != 6 {
		t.Fatalf("want line 6, got %v", broken)
	}
}

func TestDestinationsOutOfScopeAreSkipped(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": strings.Join([]string{
			"[h](https://example.com/missing.md)",
			"[p](http://example.com/x)",
			"[m](mailto:someone@example.com)",
			"[f](#a-heading)",
			"[r](/absolute.md)",
			"[e]()",
		}, "\n") + "\n",
	})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: none of these name a path in the tree", n, broken)
	}
}

func TestFragmentAndQueryAreStripped(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "[x](b.md#section) [y](b.md?raw=1)\n",
		"b.md": "b\n",
	})
	if n, broken := held(t, root); n != 2 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: the anchor is stripped, the file resolves", n, broken)
	}
}

func TestPercentEncodingIsUndone(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md":         "[x](a%20b.md)\n",
		"a b.md":       "spaces\n",
		"unrelated.md": "u\n",
	})
	if _, broken := held(t, root); len(broken) != 0 {
		t.Fatalf("percent-encoded target must resolve, got %v", broken)
	}
}

func TestAngleBracketDestination(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md":   "[x](<a b.md>) and [y](<missing file.md>)\n",
		"a b.md": "spaces\n",
	})
	_, broken := held(t, root)
	if len(broken) != 1 || broken[0].Target != "missing file.md" {
		t.Fatalf("want only the missing bracketed target refused, got %v", broken)
	}
}

func TestUnterminatedAngleBracketIsSkipped(t *testing.T) {
	root := tree(t, map[string]string{"a.md": "[x](<unterminated)\n"})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v", n, broken)
	}
}

func TestTitleAfterDestination(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "[x](b.md \"the title\")\n",
		"b.md": "b\n",
	})
	if n, broken := held(t, root); n != 1 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: a title is not part of the destination", n, broken)
	}
}

func TestDirectoryTargetHolds(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md":       "[the specs](spec)\n",
		"spec/an.md": "x\n",
	})
	if _, broken := held(t, root); len(broken) != 0 {
		t.Fatalf("a link to a directory is held, got %v", broken)
	}
}

// The confinement os-98ce6f8a's learning names: filepath.Join cleans
// ".." into a read outside the tree the gate promises, so a target that
// leaves root is refused rather than resolved against the machine.
func TestTargetLeavingTheTreeIsRefused(t *testing.T) {
	root := tree(t, map[string]string{"docs/a.md": "[x](../../etc/passwd)\n"})
	_, broken := held(t, root)
	if len(broken) != 1 || broken[0].Reason != reasonEscapes {
		t.Fatalf("want the escaping target refused as such, got %v", broken)
	}
}

func TestSkippedDirectoriesAreNotWalked(t *testing.T) {
	root := tree(t, map[string]string{
		"keep.md":                  "[x](keep.md)\n",
		".git/notes.md":            "[x](missing.md)\n",
		"node_modules/pkg/read.md": "[x](missing.md)\n",
	})
	n, broken := held(t, root)
	if len(broken) != 0 {
		t.Fatalf("vendored and git-internal documents are not the repository's: %v", broken)
	}
	if n != 1 {
		t.Fatalf("held %d, want the one document the repository owns", n)
	}
}

// plans/ is governed by a separate single-file gate, so this one does
// not read it: refusing a citation there would demand a fix no branch
// carrying this gate is allowed to make (review finding on #305).
func TestTopLevelPlansAreGovernedElsewhere(t *testing.T) {
	root := tree(t, map[string]string{
		"plans/os-1.md": "[x](gone.md)\n",
		"keep.md":       "[x](keep.md)\n",
	})
	n, broken := held(t, root)
	if len(broken) != 0 {
		t.Fatalf("plans/ is not this gate's surface: %v", broken)
	}
	if n != 1 {
		t.Fatalf("held %d, want only the document outside plans/", n)
	}
}

// The exemption is the governed surface, named by its position, not any
// directory that happens to be called plans.
func TestANestedPlansDirectoryIsStillRead(t *testing.T) {
	root := tree(t, map[string]string{
		"pkg/testdata/plans/a.md": "[x](gone.md)\n",
	})
	if _, broken := held(t, root); len(broken) != 1 {
		t.Fatalf("a nested plans/ is ordinary content, got %v", broken)
	}
}

func TestNonMarkdownIsNotRead(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "[x](missing.md)\n"})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v", n, broken)
	}
}

func TestFindingsAreSortedByFileThenLine(t *testing.T) {
	root := tree(t, map[string]string{
		"b.md": "[x](gone.md)\n",
		"a.md": "one\n[x](gone.md)\n[y](also-gone.md)\n",
	})
	_, broken := held(t, root)
	if len(broken) != 3 {
		t.Fatalf("want three findings, got %v", broken)
	}
	want := []string{"a.md:2", "a.md:3", "b.md:1"}
	for i, w := range want {
		if !strings.HasPrefix(broken[i].String(), w) {
			t.Fatalf("finding %d = %s, want it to start %s", i, broken[i], w)
		}
	}
}

func TestMissingRootIsAnError(t *testing.T) {
	if _, _, err := CheckCitations(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a root that does not exist must be an error, not a clean result")
	}
}

// Markdown permits balanced parentheses in a destination, so a
// character class that excluded them would drop the citation and let a
// broken link through green (review finding on #305).
func TestBalancedParenthesesInADestination(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md":         "[here](API_(v2).md) and [gone](API_(v3).md)\n",
		"API_(v2).md":  "v2\n",
		"unrelated.md": "u\n",
	})
	n, broken := held(t, root)
	if n != 2 {
		t.Fatalf("held %d: both parenthesised destinations are citations", n)
	}
	if len(broken) != 1 || broken[0].Target != "API_(v3).md" {
		t.Fatalf("want only the missing one refused, got %v", broken)
	}
}

// A backslash escapes the parenthesis after it, so the depth count is
// not thrown off and the target names the file rather than the markdown
// that quoted it.
func TestEscapedParenthesisInADestination(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md":   "[x](a\\(1.md)\n",
		"a(1.md": "one\n",
	})
	if n, broken := held(t, root); n != 1 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: the escaped target resolves", n, broken)
	}
}

// A destination that never closes on its line is not read: the scan
// stops at the newline rather than running to the end of the document.
func TestUnclosedDestinationIsNotRead(t *testing.T) {
	root := tree(t, map[string]string{"a.md": "[x](missing.md\nnext line)\n"})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v", n, broken)
	}
}

// A malformed percent escape is not a path the tree can hold, and
// decoding must not swallow it: the raw target is kept and refused.
func TestMalformedPercentEscapeKeepsTheRawTarget(t *testing.T) {
	root := tree(t, map[string]string{"a.md": "[x](a%zz.md)\n"})
	_, broken := held(t, root)
	if len(broken) != 1 || broken[0].Target != "a%zz.md" {
		t.Fatalf("want the raw target refused, got %v", broken)
	}
}

// A backtick run of the wrong length does not close a span: the scan
// steps over it and keeps looking for a run of exactly the opener's.
func TestWrongLengthRunDoesNotCloseASpan(t *testing.T) {
	root := tree(t, map[string]string{
		"a.md": "`a``[x](missing.md)` outside\n",
	})
	if n, broken := held(t, root); n != 0 || len(broken) != 0 {
		t.Fatalf("held %d, broken %v: the span runs to the matching run", n, broken)
	}
}

// A document the walk finds but cannot read stops the gate rather than
// passing it: a citation nobody read is not a citation that held.
func TestUnreadableDocumentIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "absent.md"), filepath.Join(root, "a.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := CheckCitations(root); err == nil {
		t.Fatal("an unreadable document must be an error, not a clean result")
	}
}
