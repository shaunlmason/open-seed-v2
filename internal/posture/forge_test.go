package posture

// The declared forge (plans/os-ad610334.md D3): admission.forge and
// admission.api are strict and default to GitHub, so every existing
// declaration keeps its meaning; Forgejo must name its instance.

import "testing"

func forgeDecl(t *testing.T, forge, api string) (*Config, error) {
	t.Helper()
	body := `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "https://admit.example", "identity": "seed-bot", "checks": ["verify"], "reviews": 1`
	if forge != "" {
		body += `, "forge": "` + forge + `"`
	}
	if api != "" {
		body += `, "api": "` + api + `"`
	}
	body += `}}`
	return Parse([]byte(body))
}

func TestForgeDefaultsToGitHub(t *testing.T) {
	cfg, err := forgeDecl(t, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Admission.ForgeKind(); got != ForgeGitHub {
		t.Errorf("absent forge defaults to github, got %q", got)
	}
}

func TestForgejoNamesItsInstance(t *testing.T) {
	if _, err := forgeDecl(t, "forgejo", ""); err == nil {
		t.Error("forgejo without an api must be refused")
	}
	cfg, err := forgeDecl(t, "forgejo", "https://forge.example")
	if err != nil {
		t.Fatalf("forgejo with an api must parse: %v", err)
	}
	if cfg.Admission.ForgeKind() != ForgeForgejo {
		t.Error("the declared forgejo forge must resolve")
	}
}

func TestForgeIsStrict(t *testing.T) {
	if _, err := forgeDecl(t, "gitlab", "https://x.example"); err == nil {
		t.Error("an unknown forge must be refused")
	}
	if _, err := forgeDecl(t, "github", "not-a-url"); err == nil {
		t.Error("a non-URL api must be refused")
	}
}

func TestNoTokenInTheDeclaration(t *testing.T) {
	_, err := Parse([]byte(`{"posture": "enforced-forge-hosted", "admission": {"endpoint": "https://a.example", "identity": "bot", "checks": ["v"], "reviews": 1, "forge": "forgejo", "api": "https://f.example", "token": "secret"}}`))
	if err == nil {
		t.Fatal("a token in the declaration must be refused: secrets never live in the tree")
	}
}
