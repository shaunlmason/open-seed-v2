package executor

// The cloud-session and remote-worker adapters' surface and risk-limit
// posture (plans/os-083112ac.md D2, D5); the full bracket runs in
// cmd/seed with the ledger and fake provider/worker fixtures.

import "testing"

func TestCloudSessionSurface(t *testing.T) {
	c := CloudSession{Endpoint: "https://cloud.example", Credential: "SEED_CLOUD_TOKEN"}
	if got := c.Tuple(); got.Harness != CloudHarness || got.Complete() {
		t.Fatalf("the cloud adapter's static report is its harness alone: %+v", got)
	}
	if d := c.Describe(); d.Budget != BudgetRiskLimit {
		t.Fatalf("the cloud session is a risk limit (a provider may bill past the reservation): %+v", d)
	}
	if err := c.Wake("anyone"); err != nil {
		t.Fatalf("wake is a no-op: %v", err)
	}
	if _, err := c.Provision(ProvisionSpec{Ledger: t.TempDir()}); err == nil {
		t.Fatal("a cloud provision without an admitted start must refuse")
	}
}

func TestRemoteWorkerSurface(t *testing.T) {
	r := RemoteWorker{ArtifactDir: t.TempDir(), WorkerName: "worker-1", Environment: "enrolled-runtime/v0"}
	if got := r.Tuple(); got.Harness != RemoteHarness || got.Complete() {
		t.Fatalf("the remote-worker adapter's static report is its harness alone: %+v", got)
	}
	if d := r.Describe(); d.Budget != BudgetRiskLimit {
		t.Fatalf("the remote worker is a risk limit: %+v", d)
	}
	if err := r.Wake("anyone"); err != nil {
		t.Fatalf("wake is a no-op (the worker pulls): %v", err)
	}
	if _, err := r.Provision(ProvisionSpec{Ledger: t.TempDir()}); err == nil {
		t.Fatal("a remote provision without an admitted start must refuse")
	}
	// Missing worker identity refuses rather than provisioning anonymously.
	if _, err := (RemoteWorker{ArtifactDir: t.TempDir()}).Provision(ProvisionSpec{Ledger: t.TempDir()}); err == nil {
		t.Fatal("a remote-worker adapter with no enrolled worker must refuse")
	}
}

func TestAllRiskLimitAdaptersDescribeHonestly(t *testing.T) {
	// The two remote substrates are risk limits; the two local ones are
	// enforced. A report that let a cloud or remote adapter claim
	// enforcement would fudge the budget.
	enforced := []Adapter{LocalWorktree{}, Container{Runtime: nil}}
	for _, a := range enforced {
		if DescribeOf("x", a).Budget != BudgetEnforced {
			t.Errorf("%T must be enforced", a)
		}
	}
	limits := []Adapter{CloudSession{}, RemoteWorker{}}
	for _, a := range limits {
		if DescribeOf("x", a).Budget != BudgetRiskLimit {
			t.Errorf("%T must be a risk limit, never enforced", a)
		}
	}
}
