package executor

// The container adapter's pure surface and its in-process OCI runtime
// (plans/os-083112ac.md D2, D4); the full bracket (Provision against an
// admitted run.started, the credential-free container, disposability and
// preemption) runs in cmd/seed with the ledger fixtures.

import (
	"testing"

	"github.com/shaunlmason/open-seed-v2/executor/fakeoci"
)

func TestContainerSurface(t *testing.T) {
	c := Container{Runtime: fakeoci.New(), Image: "example/runner:1", Fake: true}
	if got := c.Tuple(); got.Harness != ContainerHarness || got.Principal != "" || got.Complete() {
		t.Fatalf("the container adapter's static report is its harness alone: %+v", got)
	}
	if err := c.Wake("anyone"); err != nil {
		t.Fatalf("wake is the advisory no-op: %v", err)
	}
	if d := c.Describe(); d.Budget != BudgetEnforced || d.Harness != ContainerHarness {
		t.Fatalf("the container's budget is enforced (the supervisor stops it): %+v", d)
	}
	// A provision without a verifiable ledger refuses — no run outside
	// the reservation gate.
	if _, err := c.Provision(ProvisionSpec{Ledger: t.TempDir()}); err == nil {
		t.Fatal("a container provision without an admitted start must refuse")
	}
	// A missing runtime refuses rather than panicking.
	if _, err := (Container{Image: "x"}).Provision(ProvisionSpec{Ledger: t.TempDir()}); err == nil {
		t.Fatal("a container adapter with no runtime must refuse")
	}
}

func TestDescribeOfDefaultsToRiskLimit(t *testing.T) {
	// An adapter that does not state its posture is treated as a risk
	// limit — the safe assumption, never enforced by default.
	if d := DescribeOf("mystery", plainAdapter{}); d.Budget != BudgetRiskLimit {
		t.Fatalf("an undescribed adapter defaults to a risk limit, got %q", d.Budget)
	}
	// A described adapter reports its own posture.
	if d := DescribeOf("local-worktree", LocalWorktree{}); d.Budget != BudgetEnforced {
		t.Fatalf("the local adapter is enforced, got %q", d.Budget)
	}
}

// plainAdapter implements Adapter but not Described.
type plainAdapter struct{}

func (plainAdapter) Provision(ProvisionSpec) (Run, error) { return nil, nil }
func (plainAdapter) Wake(string) error                    { return nil }
func (plainAdapter) Tuple() Tuple                         { return Tuple{Harness: "mystery/v0"} }

func TestFakeOCIRuntime(t *testing.T) {
	rt := fakeoci.New()
	id, digest, err := rt.Start("example/runner:1", t.TempDir())
	if err != nil || id == "" || digest == "" {
		t.Fatalf("start returns an id and a digest: %q %q %v", id, digest, err)
	}
	if got, _ := rt.Inspect(id); got != digest {
		t.Errorf("inspect returns the started digest")
	}
	if digest != fakeoci.Digest("example/runner:1") {
		t.Errorf("the digest is deterministic in the image reference")
	}
	if err := rt.Stop(id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := rt.Remove(id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := rt.Inspect(id); err == nil {
		t.Error("a removed container no longer inspects")
	}
	if _, _, err := rt.Start("", t.TempDir()); err == nil {
		t.Error("start needs an image reference")
	}
}
