package posture

// The executors block (plans/os-083112ac.md D3): the runtime and image,
// the cloud endpoint and a credential env-var NAME, the enrolled
// workers — and a token-shaped credential is refused.

import "testing"

func TestExecutorsBlockParses(t *testing.T) {
	cfg, err := Parse([]byte(`{"posture": "cooperative", "executors": {"container": {"runtime": "fake", "image": "example/runner:1"}, "cloud": {"endpoint": "https://cloud.example", "credential": "SEED_CLOUD_TOKEN"}, "remote": {"workers": [{"name": "worker-1", "environment": "enrolled/v0"}]}}}`))
	if err != nil {
		t.Fatalf("a well-formed executors block must parse: %v", err)
	}
	if cfg.Executors == nil || cfg.Executors.Container.Runtime != "fake" || cfg.Executors.Cloud.Credential != "SEED_CLOUD_TOKEN" {
		t.Fatal("the executors block must be read")
	}
}

func TestExecutorsRefusesASecret(t *testing.T) {
	// A token pasted where the credential env-var name belongs is refused.
	_, err := Parse([]byte(`{"posture": "cooperative", "executors": {"cloud": {"endpoint": "https://c.example", "credential": "ghp_aBcD1234567890xyzTOKENvalue=="}}}`))
	if err == nil {
		t.Fatal("a token-shaped credential must be refused: the secret lives in the environment, not the tree")
	}
}

func TestExecutorsStrictRuntime(t *testing.T) {
	if _, err := Parse([]byte(`{"posture": "cooperative", "executors": {"container": {"runtime": "kubernetes", "image": "x"}}}`)); err == nil {
		t.Error("an unknown container runtime must be refused")
	}
	if _, err := Parse([]byte(`{"posture": "cooperative", "executors": {"cloud": {"endpoint": "not-a-url", "credential": "T"}}}`)); err == nil {
		t.Error("a non-URL cloud endpoint must be refused")
	}
	if _, err := Parse([]byte(`{"posture": "cooperative", "executors": {"remote": {"workers": [{"name": "w"}]}}}`)); err == nil {
		t.Error("a worker without an environment must be refused")
	}
}
