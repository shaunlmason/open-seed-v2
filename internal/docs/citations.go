package docs

// The citation stage of `seed docs check` (card os-5fe43832): a
// relative markdown link naming a path the tree does not hold renders
// broken on the forge, and until now nothing caught it. A sweep on
// 2026-09-04 found seven such links, four of them in
// docs/CONTRIBUTING-AGENTS.md, which III.Q row 5 cites as the authority
// order's evidence.
//
// internal/promotion.Check is the prior art this generalizes: it holds
// one document's citations to the tree, in one machine-readable shape.
// This holds every inline link in every markdown file under root, with
// the same confinement, because filepath.Join cleans ".." into a read
// outside the tree the gate promises.
//
// Two bounds are deliberate. Code spans and fenced blocks are masked
// before any link is read, because a regex in prose (`^[a-z0-9]([a-z0-9
// -]*[a-z0-9])?$`) is link-shaped and is not a link. External URLs are
// out of scope: resolving one needs the network, and this gate runs
// inside `make check`, which is bound to be offline and deterministic.

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Citation is one inline markdown link to a path, as the document wrote
// it: the target keeps its written form so a refusal can be matched
// against the source by eye.
type Citation struct {
	File   string // repo-relative path of the document that wrote it
	Line   int    // 1-indexed line the link starts on
	Target string // the link destination, fragment and query stripped
}

// BrokenCitation is one citation the tree does not hold.
type BrokenCitation struct {
	Citation
	Reason string
}

func (b BrokenCitation) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", b.File, b.Line, b.Target, b.Reason)
}

// The two reasons a citation is broken. They are distinct because they
// call for different fixes: a wrong target is a typo, a target that
// leaves the tree is a document reaching for something the gate cannot
// vouch for.
const (
	reasonMissing = "the tree holds no such path"
	reasonEscapes = "the target leaves the tree"
)

// skipDirs are the directories a documentation sweep must not descend
// into at any depth: git's own object store, and any vendored
// dependency tree, whose markdown documents the repository does not own
// and cannot fix.
var skipDirs = map[string]bool{".git": true, "node_modules": true}

// governedElsewhere are the top-level directories this gate does not
// read, because the repository governs their contents through a
// separate single-file gate. AGENTS.md binds a plan file to change only
// through its own plan PR, touching that one file and nothing else, so
// a whole-tree gate refusing a citation under plans/ would be
// unsatisfiable: no branch that may carry the fix may also carry the
// gate, and no branch that carries the gate may carry the fix. A gate
// whose only remedy breaks another rule is a mis-scoped gate, not a
// strict one. Dangling citations under plans/ stay real and stay
// carded; they are repaired by a plan PR, which is the surface that
// owns them.
var governedElsewhere = map[string]bool{"plans": true}

// CheckCitations walks every markdown document under root and holds each
// relative inline link to the tree: the target resolves against the
// document's own directory, stays under root, and names something that
// exists. It returns how many citations were held and the ones that
// failed, sorted by file then line. An empty finding list is clean.
//
// The count is returned because a gate that checks nothing passes: a
// caller that asserts it stays above zero notices a walk that stopped
// finding documents, which no assertion on the findings can catch.
func CheckCitations(root string) (int, []BrokenCitation, error) {
	var broken []BrokenCitation
	held := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			// Scoped to the top level on purpose: a directory named
			// plans nested inside a package's testdata is ordinary
			// content, not the governed surface.
			if at, rerr := filepath.Rel(root, p); rerr == nil && governedElsewhere[filepath.ToSlash(at)] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		n, found := holdFile(root, filepath.Dir(p), filepath.ToSlash(rel), src)
		held += n
		broken = append(broken, found...)
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	sort.Slice(broken, func(i, j int) bool {
		a, b := broken[i], broken[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Target < b.Target
	})
	return held, broken, nil
}

// linkOpenRE matches a link label and the opening parenthesis of its
// destination. The destination itself is scanned rather than matched,
// because markdown permits balanced parentheses inside one and a
// character class that excluded them would silently drop the citation.
var linkOpenRE = regexp.MustCompile(`\[[^\[\]]*\]\(`)

// destEnd returns the offset of the parenthesis closing a destination
// that starts at from, and whether one was found. Depth is counted, so
// `API_(v2).md` is read whole, and a backslash escapes the character
// after it. The scan stops at a newline: a destination that does not
// close on its own line is not a destination this gate reads.
func destEnd(b []byte, from int) (int, bool) {
	depth := 1
	for i := from; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '\n':
			return 0, false
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// holdFile reads every citation in one document and returns how many it
// held and the ones the tree does not hold. dir is the document's own
// directory (targets resolve against it); rel names it for a refusal.
func holdFile(root, dir, rel string, src []byte) (int, []BrokenCitation) {
	masked := mask(src)
	held := 0
	var broken []BrokenCitation
	for _, m := range linkOpenRE.FindAllIndex(masked, -1) {
		shut, ok := destEnd(masked, m[1])
		if !ok {
			continue
		}
		target := destination(string(masked[m[1]:shut]))
		if !isRelative(target) {
			continue
		}
		target = pathOf(target)
		if target == "" {
			continue
		}
		held++
		c := Citation{File: rel, Line: lineAt(masked, m[0]), Target: target}
		abs := filepath.Join(dir, filepath.FromSlash(target))
		inside, ierr := filepath.Rel(root, abs)
		if ierr != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			broken = append(broken, BrokenCitation{Citation: c, Reason: reasonEscapes})
			continue
		}
		if _, serr := os.Stat(abs); serr != nil {
			broken = append(broken, BrokenCitation{Citation: c, Reason: reasonMissing})
		}
	}
	return held, broken
}

// destination extracts the link target from what stood between the
// parentheses: an angle-bracketed destination, or everything up to the
// whitespace that would begin a title. A backslash-escaped parenthesis
// becomes the character it names, so the target matches the file on
// disk rather than the markdown that quoted it.
func destination(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "<") {
		if i := strings.IndexByte(s, '>'); i > 0 {
			return unescapeParens(s[1:i])
		}
		return ""
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	return unescapeParens(s)
}

// unescapeParens undoes the two escapes destEnd honours.
func unescapeParens(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	return strings.NewReplacer(`\(`, "(", `\)`, ")").Replace(s)
}

// schemeRE matches an absolute URI's scheme, which takes the target out
// of scope: this gate resolves paths in the tree, never the network.
var schemeRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)

// isRelative reports whether a destination names a path in the tree. A
// bare fragment addresses the document itself, and a root-absolute path
// names a root this gate is not given, so neither is held.
func isRelative(t string) bool {
	switch {
	case t == "", strings.HasPrefix(t, "#"), strings.HasPrefix(t, "/"):
		return false
	case schemeRE.MatchString(t):
		return false
	}
	return true
}

// pathOf strips the fragment and query a link may carry and undoes
// percent-encoding, so `spec.md#anchor` and `a%20b.md` resolve to the
// files they name. Anchors themselves are not verified in this stage.
func pathOf(t string) string {
	if i := strings.IndexAny(t, "#?"); i >= 0 {
		t = t[:i]
	}
	if u, err := url.PathUnescape(t); err == nil {
		return u
	}
	return t
}

// lineAt maps a byte offset in the masked source to its 1-indexed line.
// Masking preserves newlines exactly so the two agree.
func lineAt(src []byte, off int) int {
	return 1 + bytes.Count(src[:off], []byte{'\n'})
}

// fenceRE matches the opening of a fenced code block: up to three
// spaces of indent, then a run of at least three backticks or tildes.
var fenceRE = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// mask returns a copy of src with every region a link must not be read
// from overwritten by spaces: fenced code blocks and inline code spans.
// Newlines survive, so a byte offset in the result still names the line
// it came from, and the copy's length is the source's.
func mask(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)
	var fence []byte
	for pos := 0; pos <= len(out); {
		lineEnd := len(out)
		nl := bytes.IndexByte(out[pos:], '\n')
		if nl >= 0 {
			lineEnd = pos + nl
		}
		line := out[pos:lineEnd]
		switch {
		case fence != nil:
			if closesFence(line, fence) {
				fence = nil
			}
			blank(out, pos, lineEnd)
		case fenceRE.Match(line):
			fence = append([]byte(nil), fenceRE.FindSubmatch(line)[1]...)
			blank(out, pos, lineEnd)
		default:
			maskSpans(out, pos, lineEnd)
		}
		if nl < 0 {
			break
		}
		pos = lineEnd + 1
	}
	return out
}

// closesFence reports whether a line closes an open fence: a run of the
// same character, at least as long as the opener, and nothing after it.
func closesFence(line, fence []byte) bool {
	t := bytes.TrimLeft(line, " ")
	n := 0
	for n < len(t) && t[n] == fence[0] {
		n++
	}
	return n >= len(fence) && len(bytes.TrimSpace(t[n:])) == 0
}

// maskSpans overwrites the inline code spans in one line. A run of n
// backticks opens a span that the next run of exactly n closes; a run
// with no closer on the line is literal text and is left alone, which
// is what a reader's markdown renderer does with it.
func maskSpans(b []byte, from, to int) {
	for i := from; i < to; {
		if b[i] != '`' {
			i++
			continue
		}
		open := runEnd(b, i, to)
		shut := closerAt(b, open, to, open-i)
		if shut < 0 {
			i = open
			continue
		}
		blank(b, i, shut)
		i = shut
	}
}

// runEnd returns the offset just past the backtick run starting at i.
func runEnd(b []byte, i, to int) int {
	for i < to && b[i] == '`' {
		i++
	}
	return i
}

// closerAt finds the end of the first backtick run of exactly n after
// from, or -1 when the line holds none.
func closerAt(b []byte, from, to, n int) int {
	for k := from; k < to; {
		if b[k] != '`' {
			k++
			continue
		}
		end := runEnd(b, k, to)
		if end-k == n {
			return end
		}
		k = end
	}
	return -1
}

// blank overwrites a range with spaces, leaving newlines in place so
// line numbers computed over the masked source stay true.
func blank(b []byte, from, to int) {
	for i := from; i < to && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}
