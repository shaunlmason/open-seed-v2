package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Export is v1's lossless export: `scripts/seed state export`, one
// document with the state ref's head commit and every file in its tree.
type Export struct {
	SchemaVersion string            `json:"schema_version"`
	Backend       string            `json:"backend"`
	Head          string            `json:"head"`
	Files         map[string]string `json:"files"`
}

// ReadExport reads the export as the command prints it (`{"document":
// {...}, ...}`) or as the document alone.
func ReadExport(path string) (*Export, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Document *Export `json:"document"`
	}
	if err := json.Unmarshal(b, &wrapped); err == nil && wrapped.Document != nil && wrapped.Document.Head != "" {
		return validateExport(wrapped.Document)
	}
	var doc Export
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("the export does not parse: %v", err)
	}
	return validateExport(&doc)
}

func validateExport(e *Export) (*Export, error) {
	if e.SchemaVersion != "1.0" {
		return nil, fmt.Errorf("the export's schema_version is %q; this build imports 1.0", e.SchemaVersion)
	}
	if e.Backend != "filecards" {
		return nil, fmt.Errorf("the export's backend is %q; this build imports filecards", e.Backend)
	}
	if len(e.Head) != 40 {
		return nil, errors.New("the export names no state commit in head")
	}
	if e.Files == nil {
		e.Files = map[string]string{}
	}
	return e, nil
}

// AnchorError is an export no anchor covers, or one whose content is
// not the anchored tree: refused before any transform.
type Refusal struct {
	Kind   string // "unanchored" or "export_mismatch"
	Detail string
}

func (e *Refusal) Error() string { return e.Kind + ": " + e.Detail }

// Anchor is what verification established.
type Anchor struct {
	Tag    string
	Commit string
}

func git(dir string, args ...string) (string, error) {
	out, err := gitRaw(dir, args...)
	return strings.TrimSpace(string(out)), err
}

// gitRaw is git's stdout verbatim, for blobs: a file's bytes are
// compared and stored exactly, never trimmed.
func gitRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// AnchorTags lists the source's seed-anchor tags, sorted by name (the
// names are timestamps, so the last is the newest).
func AnchorTags(source string) ([]string, error) {
	out, err := git(source, "tag", "-l", "seed-anchor/*")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range strings.Split(out, "\n") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	return tags, nil
}

// VerifyAnchor holds the export to the source history: its head must
// be the commit the anchor tag names or an ancestor of it, and every
// file must equal the blob at that path in the tree at head, with no
// file in the tree absent from the export. An empty tag means the
// newest anchor. Nothing here transforms anything.
func VerifyAnchor(e *Export, source, tag string) (*Anchor, error) {
	if tag == "" {
		tags, err := AnchorTags(source)
		if err != nil {
			return nil, err
		}
		if len(tags) == 0 {
			return nil, &Refusal{Kind: "unanchored", Detail: "the source carries no seed-anchor tag: an export nobody anchored is a document, not evidence"}
		}
		tag = tags[len(tags)-1]
	}
	commit, err := git(source, "rev-parse", "--verify", tag+"^{commit}")
	if err != nil {
		return nil, &Refusal{Kind: "unanchored", Detail: fmt.Sprintf("anchor %s does not resolve in the source: %v", tag, err)}
	}
	if e.Head != commit {
		if _, err := git(source, "merge-base", "--is-ancestor", e.Head, commit); err != nil {
			return nil, &Refusal{Kind: "unanchored", Detail: fmt.Sprintf("the export's head %.12s is neither %s (%.12s) nor an ancestor of it", e.Head, tag, commit)}
		}
	}
	listing, err := git(source, "ls-tree", "-r", "--name-only", e.Head)
	if err != nil {
		return nil, &Refusal{Kind: "unanchored", Detail: fmt.Sprintf("the export's head %.12s is not in the source: %v", e.Head, err)}
	}
	inTree := map[string]bool{}
	for _, p := range strings.Split(listing, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			inTree[p] = true
		}
	}
	paths := make([]string, 0, len(e.Files))
	for p := range e.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if !inTree[p] {
			return nil, &Refusal{Kind: "export_mismatch", Detail: fmt.Sprintf("%s is in the export and not in the anchored tree", p)}
		}
		blob, err := gitRaw(source, "show", e.Head+":"+p)
		if err != nil {
			return nil, &Refusal{Kind: "export_mismatch", Detail: fmt.Sprintf("%s cannot be read from the anchored tree: %v", p, err)}
		}
		// Byte for byte: a trailing newline more or less is a
		// different file.
		if string(blob) != e.Files[p] {
			return nil, &Refusal{Kind: "export_mismatch", Detail: fmt.Sprintf("%s differs from the anchored tree", p)}
		}
	}
	for p := range inTree {
		if _, ok := e.Files[p]; !ok {
			return nil, &Refusal{Kind: "export_mismatch", Detail: fmt.Sprintf("%s is in the anchored tree and not in the export", p)}
		}
	}
	return &Anchor{Tag: tag, Commit: commit}, nil
}
