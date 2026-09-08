package docs

import (
	"os"
	"path/filepath"
	"sort"
)

// Check regenerates every governed document and compares it to the
// committed file under root, returning the repo-relative paths that
// differ (a hand edit, a stale generated file, or a table change the
// committed output has not caught). An empty result is clean.
func Check(root string) ([]string, error) {
	want, err := Generate(root)
	if err != nil {
		return nil, err
	}
	drift := map[string]bool{}
	for rel, content := range want {
		got, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(got) != content {
			drift[rel] = true
		}
	}
	// A committed generated file no longer produced is drift too: the
	// generator dropped it (a removed lane), so the stale file lingers.
	genRoot := filepath.Join(root, GenDir)
	_ = filepath.Walk(genRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		if _, ok := want[rel]; !ok {
			drift[rel] = true
		}
		return nil
	})
	out := make([]string, 0, len(drift))
	for rel := range drift {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

// Write regenerates every governed document and writes it under root,
// creating directories as needed and removing any stale generated file
// the generator no longer produces. It returns the repo-relative paths
// written, sorted.
func Write(root string) ([]string, error) {
	want, err := Generate(root)
	if err != nil {
		return nil, err
	}
	// Remove stale files first so a dropped doc does not linger.
	genRoot := filepath.Join(root, GenDir)
	_ = filepath.Walk(genRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		if _, ok := want[rel]; !ok {
			_ = os.Remove(p)
		}
		return nil
	})
	written := make([]string, 0, len(want))
	for rel, content := range want {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return nil, err
		}
		written = append(written, rel)
	}
	sort.Strings(written)
	return written, nil
}
