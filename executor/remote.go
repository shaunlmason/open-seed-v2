package executor

// RemoteWorker is the enrolled-remote-worker adapter (plans/os-083112ac.md
// D1, D5): it NEVER connects to the worker — inbound connectivity to an
// executor is refused (§II.9). It puts the packet in the artifact store
// and appends a pickup line to the per-fence observation stream; the
// enrolled worker — an actor of kind `service` whose name matches the
// declaration — pulls both from the ledger and the store it already
// reads, meters on the same stream, and disposes itself. Its budget is a
// RISK LIMIT: a remote process may spend past the reservation before the
// interrupt reaches it.

import (
	"fmt"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

// RemoteHarness is the versioned name the remote-worker adapter resolves.
const RemoteHarness = "remote-worker/v0"

// RemoteWorker hands work to an enrolled worker through the ledger and
// the artifact store. WorkerName is the enrolled service actor's name and
// Environment is its declared enrolled environment string (the tuple
// field the adapter resolves).
type RemoteWorker struct {
	ArtifactDir string
	WorkerName  string
	Environment string
}

// Tuple reports the one field the adapter controls statically.
// Tuple reports the harness and the enrolled worker's declared
// environment, which is what a provision resolves to.
func (c RemoteWorker) Tuple() Tuple {
	return Tuple{Harness: RemoteHarness, Environment: c.Environment}
}

// Wake is the documented no-op: the worker pulls, so nothing is pushed.
func (RemoteWorker) Wake(string) error { return nil }

// Describe reports the risk-limit posture.
func (RemoteWorker) Describe() Description {
	return Description{Name: "remote-worker", Harness: RemoteHarness, Budget: BudgetRiskLimit,
		Reason: "a remote process may spend past the reservation before the interrupt reaches it, so the budget is a risk limit"}
}

// Provision verifies the admitted start, stores the packet, and appends
// the pickup the worker reads. It opens no connection.
func (c RemoteWorker) Provision(spec ProvisionSpec) (Run, error) {
	started, err := verifyStarted(spec)
	if err != nil {
		return nil, err
	}
	if c.WorkerName == "" || c.Environment == "" {
		return nil, fmt.Errorf("the remote-worker adapter needs the enrolled worker's name and environment")
	}
	if c.ArtifactDir == "" {
		return nil, fmt.Errorf("the remote-worker adapter needs the artifact store")
	}
	// The resolved-against-admitted check runs BEFORE any side effect, so
	// a refused provision stores no packet and appends no pickup — the
	// local adapter's "a refused provision leaks nothing" principle.
	resolve := func(declared Tuple) Tuple {
		out := declared
		out.Harness = RemoteHarness
		out.Environment = c.Environment
		return out
	}
	var resolved Tuple
	if started.Tuple != nil {
		resolved = resolve(*started.Tuple)
		if field, have, want, differs := resolved.Diff(*started.Tuple); differs {
			return nil, fmt.Errorf("%w: %s resolved to %q, the admitted start declared %q", ErrTupleMismatch, field, have, want)
		}
	} else {
		resolved = resolve(Tuple{})
	}
	store := artifact.Open(c.ArtifactDir)
	digest, err := store.Put(spec.Packet)
	if err != nil {
		return nil, fmt.Errorf("storing the packet for the worker: %w", err)
	}
	// The pickup: the worker reads the packet digest and the base from
	// the stream it already orients on. Nothing is pushed to the worker.
	if err := c.line(spec, fmt.Sprintf("pickup packet=%s base=%s worker=%s", digest, spec.Base, c.WorkerName), 0); err != nil {
		return nil, err
	}
	return &remoteRun{adapter: c, spec: spec, digest: digest, tuple: resolved}, nil
}

func (c RemoteWorker) line(spec ProvisionSpec, step string, units int) error {
	return obs.Append(spec.ObsDir, spec.Actor, obs.FormatFence(spec.Fence), obs.Line{
		TS: time.Now().UTC().Format(time.RFC3339), Subject: spec.Subject, Step: step, Units: units,
	})
}

type remoteRun struct {
	adapter RemoteWorker
	spec    ProvisionSpec
	digest  string
	tuple   Tuple
}

// Workspace names the worker, not a local checkout: the work runs on the
// enrolled remote worker.
func (r *remoteRun) Workspace() string { return "remote-worker:" + r.adapter.WorkerName }
func (r *remoteRun) Tuple() Tuple      { return r.tuple }

// Meter records a metered line; on a real deployment the worker's own
// meter lines arrive on this same stream.
func (r *remoteRun) Meter(units int, step string) error {
	return r.adapter.line(r.spec, step, units)
}

// Dispose records the disposed line; the worker tears itself down.
func (r *remoteRun) Dispose() error {
	return r.adapter.line(r.spec, "disposed worker="+r.adapter.WorkerName, 0)
}
