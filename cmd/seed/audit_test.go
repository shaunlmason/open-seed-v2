package main

// The capability audit (plans/os-0d4f2af3.md D5; charter §II.14, III.L
// row 2): derived from the shipped manifests and the protected-surface
// list rather than asserted, so a manifest that later grants what would
// write the surface fails by name.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// operatorHolders derives, from a manifest directory, the manifests
// that hold operator standing — the one capability the hook's rule lets
// update the default branch — sorted by name.
func operatorHolders(t *testing.T, dir string) []string {
	t.Helper()
	manifests, err := lane.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range manifests {
		for _, g := range m.Grants {
			if g == keyring.CapOperator {
				out = append(out, m.Lane)
			}
		}
	}
	sort.Strings(out)
	return out
}

// conformance: III.L row 2 — no lane grant reaches the protected
// surface. Under the hook's rule the surface is the governance root's
// alone (#241), while operator standing may update the rest of the
// default branch; so the audit derives the operator-holding manifests
// from the shipped set and holds the list to exactly `maintenance`,
// the lane whose pass reaps, reconciles and checkpoints — a second
// holder fails by name. The required members of the surface are the
// spec's list, clean and free of duplicates, and a declaration made of
// exactly that list is complete.
func TestCapabilityAuditOfTheShippedManifests(t *testing.T) {
	if got := operatorHolders(t, "../../lanes"); strings.Join(got, ",") != "maintenance" {
		t.Fatalf("the operator-holding manifests are exactly maintenance, got %v", got)
	}
	for _, must := range []string{"spec", "internal/admit", "internal/keyring", "lanes", "Makefile", ".github/workflows", "scripts"} {
		found := false
		for _, req := range RequiredProtected {
			if req == must {
				found = true
			}
		}
		if !found {
			t.Errorf("the required surface names %s (charter §II.14)", must)
		}
	}
	seen := map[string]bool{}
	for _, req := range RequiredProtected {
		if seen[req] || strings.HasPrefix(req, "/") || strings.HasSuffix(req, "/") || strings.Contains(req, "..") {
			t.Errorf("required member %q is unclean or repeated", req)
		}
		seen[req] = true
	}
	cfg, err := posture.Parse([]byte(`{"posture": "cooperative", "protected": ["` + strings.Join(RequiredProtected, `", "`) + `"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := lintPreseed(cfg, "../../lanes"); err != nil {
		t.Fatalf("the required list is itself a complete declaration: %v", err)
	}
	for _, req := range RequiredProtected {
		if !cfg.Protects(req+"/anything") && !strings.Contains(req, ".") {
			t.Errorf("%s must protect what is under it", req)
		}
	}

	// A planted lane granting operator fails by name.
	dir := t.TempDir()
	manifests, err := lane.Load("../../lanes")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range manifests {
		src, _ := os.ReadFile(filepath.Join("../../lanes", m.Lane+".json"))
		_ = os.WriteFile(filepath.Join(dir, m.Lane+".json"), src, 0o644)
	}
	_ = os.CopyFS(filepath.Join(dir, "fragments"), os.DirFS("../../lanes/fragments"))
	planted := `{"lane": "implementer", "kind": "lane", "summary": "planted", "grants": ["claim", "operator"], "orients_from": "seed situation --remote <repo> --key <key> --since <position>", "acts_through": ["claim take"], "liveness_from": ["claim take"], "inbox": "x", "fragments": ["lane/implementer.md"]}`
	if err := os.WriteFile(filepath.Join(dir, "implementer.json"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := operatorHolders(t, dir); strings.Join(got, ",") != "implementer,maintenance" {
		t.Fatalf("a planted operator grant is seen by name, got %v", got)
	}
}
