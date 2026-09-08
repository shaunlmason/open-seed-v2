package posture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seed.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// conformance: III posture item — every deployment declares one of the
// three charter postures; everything else refuses.
func TestLoadRoundTripsAndRefuses(t *testing.T) {
	for _, p := range []Posture{EnforcedSelfHosted, Cooperative} {
		cfg, err := Load(write(t, `{"posture": "`+string(p)+`"}`))
		if err != nil || cfg.Posture != p {
			t.Errorf("%s must round-trip, got %+v %v", p, cfg, err)
		}
	}
	// The third posture round-trips with the block it requires
	// (TestAdmissionBlockIsPostureGated holds it to that).
	if cfg, err := Load(write(t, forgeHosted)); err != nil || cfg.Posture != EnforcedForgeHosted {
		t.Errorf("%s must round-trip with its block, got %+v %v", EnforcedForgeHosted, cfg, err)
	}

	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); !errors.Is(err, ErrUndeclared) {
		t.Errorf("missing declaration must be ErrUndeclared, got %v", err)
	}
	// A declaration that exists but cannot be read is an operational
	// failure, never a content judgment.
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrUnreadable) {
		t.Errorf("an unreadable declaration must be ErrUnreadable, got %v", err)
	}
	for name, content := range map[string]string{
		"unknown posture": `{"posture": "anarchy"}`,
		"unknown field":   `{"posture": "cooperative", "mode": "yolo"}`,
		"trailing data":   `{"posture": "cooperative"} {}`,
		"not json":        `posture=cooperative`,
		"empty object":    `{}`,
	} {
		_, err := Load(write(t, content))
		if err == nil || errors.Is(err, ErrUndeclared) || errors.Is(err, ErrUnreadable) {
			t.Errorf("%s must refuse with a parse/validity error, got %v", name, err)
			continue
		}
		if !strings.Contains(err.Error(), "valid postures") {
			t.Errorf("%s must name the valid postures, got %v", name, err)
		}
	}
}

func TestPostureProperties(t *testing.T) {
	if !EnforcedSelfHosted.Enforced() || !EnforcedForgeHosted.Enforced() || Cooperative.Enforced() {
		t.Fatal("enforcement classification is wrong")
	}
	if Posture("anarchy").Valid() {
		t.Fatal("unknown postures must not validate")
	}
	for _, phrase := range []string{"does not hold", "advisory against a hostile credential", "§I.2"} {
		if !strings.Contains(Consequence, phrase) {
			t.Errorf("the named consequence must keep the charter phrase %q", phrase)
		}
	}
}

const forgeHosted = `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://127.0.0.1:8437", "identity": "seed-admission[bot]", "ledger_ref": "refs/heads/seed-ledger", "checks": ["check", "verify"], "reviews": 1, "owners": ["@governance-root"]}}`

// conformance: III.B — the third posture is declared with the block
// the service and the reconciler read (plans/os-5c8a312c.md D3, D5):
// required under enforced-forge-hosted, refused under the others, and
// strict about the endpoint, the identity and the branch namespace.
func TestAdmissionBlockIsPostureGated(t *testing.T) {
	cfg, err := Load(write(t, forgeHosted))
	if err != nil {
		t.Fatalf("a complete forge-hosted declaration must load: %v", err)
	}
	if cfg.Admission == nil || cfg.Admission.Endpoint != "http://127.0.0.1:8437" || cfg.Admission.Identity != "seed-admission[bot]" || cfg.LedgerRef() != "refs/heads/seed-ledger" || cfg.Admission.Reviews != 1 || len(cfg.Admission.Checks) != 2 || len(cfg.Admission.Owners) != 1 {
		t.Fatalf("the block must round-trip, got %+v", cfg.Admission)
	}
	cfg, err = Load(write(t, `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "https://admit.example", "identity": "bot"}}`))
	if err != nil || cfg.LedgerRef() != DefaultLedgerRef {
		t.Fatalf("an unnamed ledger ref defaults to the branch %s, got %q %v", DefaultLedgerRef, cfg.LedgerRef(), err)
	}
	for _, p := range []Posture{EnforcedSelfHosted, Cooperative} {
		cfg, err := Load(write(t, `{"posture": "`+string(p)+`"}`))
		if err != nil || cfg.LedgerRef() != "" {
			t.Fatalf("%s carries no ledger ref of its own, got %q %v", p, cfg.LedgerRef(), err)
		}
	}
	for name, content := range map[string]string{
		"missing block":     `{"posture": "enforced-forge-hosted"}`,
		"block elsewhere":   `{"posture": "cooperative", "admission": {"endpoint": "http://x", "identity": "bot"}}`,
		"no endpoint":       `{"posture": "enforced-forge-hosted", "admission": {"identity": "bot"}}`,
		"bad scheme":        `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "ftp://x", "identity": "bot"}}`,
		"no identity":       `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": " "}}`,
		"custom namespace":  `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": "bot", "ledger_ref": "refs/seed/ledger"}}`,
		"negative reviews":  `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": "bot", "reviews": -1}}`,
		"empty check":       `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": "bot", "checks": [""]}}`,
		"empty owner":       `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": "bot", "owners": [" "]}}`,
		"unknown sub-field": `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://x", "identity": "bot", "token": "secret"}}`,
		"protected slash":   `{"posture": "cooperative", "protected": ["/Makefile"]}`,
		"protected dotdot":  `{"posture": "cooperative", "protected": ["../x"]}`,
		"protected unclean": `{"posture": "cooperative", "protected": ["/spec"]}`,
		"protected empty":   `{"posture": "cooperative", "protected": [" "]}`,
	} {
		_, err := Load(write(t, content))
		if err == nil || errors.Is(err, ErrUndeclared) || errors.Is(err, ErrUnreadable) {
			t.Errorf("%s must refuse with a validity error, got %v", name, err)
		}
	}
	if _, err := Load(write(t, `{"posture": "enforced-forge-hosted"}`)); err == nil || !strings.Contains(err.Error(), "admission block") {
		t.Errorf("the missing block must be named, got %v", err)
	}
}

// conformance: III.L — the protected surface is enumerated in config
// (plans/os-465e356e.md D1): the declaration carries it as clean
// repository-relative prefixes, its own path is protected by
// construction, and membership is exact-or-under.
func TestProtectedSurfaceIsDeclaredAndSelfIncluding(t *testing.T) {
	cfg, err := Load(write(t, `{"posture": "enforced-self-hosted", "protected": ["Makefile", ".github/workflows/", "spec/transitions.json", "Makefile"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/workflows", "Makefile", "seed.json", "spec/transitions.json"}
	if got := cfg.ProtectedSurface(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("surface %v, want %v (deduplicated, sorted, self-including)", got, want)
	}
	for p, want := range map[string]bool{
		"Makefile":                    true,
		"seed.json":                   true,
		".github/workflows/check.yml": true,
		".github/workflows":           true,
		"spec/transitions.json":       true,
		"spec/transitions.json.bak":   false,
		"Makefile.old":                false,
		"docs/seed.json":              false,
		"spec/lanes.md":               false,
		".github/workflowsX/a.yml":    false,
		"internal/redteam/x_test.go":  false,
	} {
		if got := cfg.Protects(p); got != want {
			t.Errorf("Protects(%q) = %v, want %v", p, got, want)
		}
	}
	// A declaration without the field protects exactly its own path.
	bare, err := Load(write(t, `{"posture": "enforced-self-hosted"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := bare.ProtectedSurface(); len(got) != 1 || got[0] != DeclarationPath || !bare.Protects(DeclarationPath) || bare.Protects("Makefile") {
		t.Fatalf("an unlisted surface is the declaration alone, got %v", got)
	}
	for name, content := range map[string]string{
		"empty entry":     `{"posture": "cooperative", "protected": [""]}`,
		"absolute":        `{"posture": "cooperative", "protected": ["/etc"]}`,
		"escaping":        `{"posture": "cooperative", "protected": ["../x"]}`,
		"dot":             `{"posture": "cooperative", "protected": ["."]}`,
		"unclean":         `{"posture": "cooperative", "protected": ["a//b"]}`,
		"backslash":       `{"posture": "cooperative", "protected": ["a\\b"]}`,
		"not a list":      `{"posture": "cooperative", "protected": "Makefile"}`,
		"not strings":     `{"posture": "cooperative", "protected": [1]}`,
		"unknown sibling": `{"posture": "cooperative", "protected": [], "protect": []}`,
	} {
		if _, err := Load(write(t, content)); err == nil || !strings.Contains(err.Error(), "valid postures") {
			t.Errorf("%s must refuse naming the valid postures, got %v", name, err)
		}
	}
	// Parse is Load without the file: the same strictness on bytes.
	if _, err := Parse([]byte(`{"posture": "anarchy"}`)); err == nil {
		t.Fatal("Parse must apply Load's validity check")
	}
}

// conformance: plans/os-56bee171.md AC1 — the racing block is an
// explicit opt-in that says both things or refuses: two or more
// racers, a cost sentence in the operator's words; an absent block
// is no racing.
func TestRacingBlockSaysBothOrRefuses(t *testing.T) {
	base := `{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"%s}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}]}}`
	cfg, err := Parse([]byte(fmt.Sprintf(base, `, "racing": {"racers": 2, "cost": "two runs per contract"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if racers, cost, ok := cfg.RacingFor("core"); !ok || racers != 2 || cost != "two runs per contract" {
		t.Fatalf("the block reads back: %d %q %v", racers, cost, ok)
	}
	if _, _, ok := cfg.RacingFor("other"); ok {
		t.Fatal("an undeclared squad does not race")
	}
	plain, err := Parse([]byte(fmt.Sprintf(base, "")))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := plain.RacingFor("core"); ok {
		t.Fatal("an absent block is no racing")
	}
	for name, block := range map[string]string{
		"one racer":     `, "racing": {"racers": 1, "cost": "one is exclusivity"}`,
		"no cost":       `, "racing": {"racers": 3, "cost": " "}`,
		"unknown field": `, "racing": {"racers": 2, "cost": "x", "mode": "fast"}`,
	} {
		if _, err := Parse([]byte(fmt.Sprintf(base, block))); err == nil {
			t.Errorf("%s must refuse", name)
		}
	}
}

// conformance: plans/os-48df10a2.md AC4 — the federation block is
// strict and read-only: names one token and unique, remotes named,
// refs full ref names or absent for the default, and no field that
// could name a key or a write.
func TestFederationBlockIsStrictAndReadOnly(t *testing.T) {
	base := `{"posture": "cooperative", "federation": {"remotes": [%s]}}`
	cfg, err := Parse([]byte(fmt.Sprintf(base, `{"name": "alpha", "remote": "git@x:a.git", "ref": "refs/seed/ledger"}, {"name": "beta", "remote": "/srv/b.git"}`)))
	if err != nil || cfg.Federation == nil || len(cfg.Federation.Remotes) != 2 || cfg.Federation.Remotes[1].Ref != "" {
		t.Fatalf("two remotes, the second on the default ref: %v %+v", err, cfg)
	}
	for name, remotes := range map[string]string{
		"blank name":   `{"name": " ", "remote": "x"}`,
		"slash name":   `{"name": "a/b", "remote": "x"}`,
		"duplicate":    `{"name": "a", "remote": "x"}, {"name": "a", "remote": "y"}`,
		"no remote":    `{"name": "a", "remote": ""}`,
		"short ref":    `{"name": "a", "remote": "x", "ref": "seed-ledger"}`,
		"a key field":  `{"name": "a", "remote": "x", "key": "/etc/key"}`,
		"a write flag": `{"name": "a", "remote": "x", "write": true}`,
	} {
		if _, err := Parse([]byte(fmt.Sprintf(base, remotes))); err == nil {
			t.Errorf("%s: the block parsed", name)
		}
	}
}
