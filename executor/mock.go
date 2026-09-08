package executor

// Mock is the simulation adapter (plans/os-16e55c11.md D3): a
// credential-free executor that provisions a throwaway workspace,
// reports a synthetic tuple no forge or model backs, and records
// metered usage the loop's budget settle reads — so `seed simulate`
// runs the whole executor path end to end with zero credentials and no
// network. Unlike LocalWorktree it does not replay the ledger to fence
// the workspace to an admitted run.started: the simulation admits its
// own run.started through the boundary before provisioning, and the
// mock is not an execution surface a real run would trust.

import (
	"os"
	"path/filepath"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

// MockName is the adapter name `run start --adapter mock` selects.
const MockName = "mock"

// MockHarness and MockEnvironment are the mock's static report — the two
// tuple fields an adapter knows. They name the simulation plainly so no
// receipt reads as a real run.
const (
	MockHarness     = "mock/v0"
	MockEnvironment = "simulated"
)

// Mock is a zero-value-usable Adapter.
type Mock struct{}

// Tuple is the mock's static report: harness and environment only, the
// two fields an adapter can honestly know.
func (Mock) Tuple() Tuple {
	return Tuple{Harness: MockHarness, Environment: MockEnvironment}
}

// Wake is a no-op: the simulation drives the loop synchronously.
func (Mock) Wake(string) error { return nil }

// Provision creates a throwaway workspace and returns a Run that meters
// into the observation trail. It does not fence to a ledger run.started
// (LocalWorktree's verifyStarted); the simulation's own boundary admits
// the run before this is called.
func (Mock) Provision(spec ProvisionSpec) (Run, error) {
	ws, err := os.MkdirTemp("", "seed-mock-run-")
	if err != nil {
		return nil, err
	}
	if len(spec.Packet) > 0 {
		if err := os.MkdirAll(filepath.Join(ws, ".seed-run"), 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(ws, ".seed-run", "packet.json"), spec.Packet, 0o644)
		}
	}
	t := Mock{}.Tuple()
	t.Principal = spec.Actor
	return &mockRun{spec: spec, workspace: ws, tuple: t}, nil
}

type mockRun struct {
	spec      ProvisionSpec
	workspace string
	tuple     Tuple
}

func (r *mockRun) Workspace() string { return r.workspace }
func (r *mockRun) Tuple() Tuple      { return r.tuple }

// Meter records synthetic usage in the same observation trail a real run
// writes, so budget settle and the simulation's spend audit read a real
// metered figure.
func (r *mockRun) Meter(units int, step string) error {
	if r.spec.ObsDir == "" {
		return nil
	}
	return obs.Append(r.spec.ObsDir, r.spec.Actor, obs.FormatFence(r.spec.Fence), obs.Line{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Subject: r.spec.Subject,
		Step:    step,
		Units:   units,
	})
}

func (r *mockRun) Dispose() error { return os.RemoveAll(r.workspace) }
