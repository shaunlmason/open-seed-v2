package gitref

// The fixture-hardening guard (plans/os-c4e8b57a.md step 3, D5): the
// class this card fixes is "a git repository created under a
// t.TempDir that a detached git process can outlive", and a comment
// would not survive the next fixture. So the property is checked over
// the tree rather than over a list of names: every repository-creating
// call in a next/** test must be followed by hardening, and the guard
// asserts it found a plausible number of them, so a regex that quietly
// stops matching fails here instead of passing vacuously.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// creates matches the shapes the tree uses to make a repository: a
// direct exec.Command("git", …) and the per-package git/gitIn/run/
// gitOut helpers, each with "init" or "clone" among the arguments.
// Table entries built as []string{…} are the seed CLI's own `init`
// verb, not git's, and are excluded by name. A helper the alternation
// does not name escapes the property: the flywheel fixture's gitIn
// did, and its skip path lost the cleanup race (os-222189a3).
var creates = regexp.MustCompile(`(?:exec\.Command\("git"|\bgit\(|\bgitIn\(|\brun\(|\bgitOut\()[^\n]*"(?:init|clone)"`)

// minimumSites is a floor, not a count: it goes UP when fixtures are
// added and must never be silently satisfied by a broken pattern. It
// is deliberately below the true number so adding a fixture does not
// fail an unrelated PR, while a regex that matches nothing does.
const minimumSites = 10

func TestEveryFixtureRepositoryIsHardened(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("guard cannot find the next/ module root at %s: %v", root, err)
	}
	sites := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		rel, _ := filepath.Rel(root, path)
		for i, line := range lines {
			if strings.Contains(line, "[]string{") || !creates.MatchString(line) {
				continue
			}
			sites++
			// The hardening belongs immediately after creation, before
			// the first object: a window is a race against a race.
			// Six lines of slack covers an error check plus a brace.
			hardened := false
			for j := i + 1; j < len(lines) && j <= i+6; j++ {
				if strings.Contains(lines[j], "hardenGitRepo(") {
					hardened = true
					break
				}
			}
			if !hardened {
				t.Errorf("%s:%d creates a repository without hardenGitRepo: %s\n"+
					"    a detached auto-gc under a t.TempDir outlives the test that made it",
					rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sites < minimumSites {
		t.Fatalf("the guard found %d repository-creating sites, fewer than the %d floor — "+
			"the pattern stopped matching and this guard is passing vacuously", sites, minimumSites)
	}
}
