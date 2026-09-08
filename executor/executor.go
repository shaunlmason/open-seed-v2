// Package executor is the public executor-adapter interface (charter
// III.H "the adapter interface is public"; SEED-NEXT.md §II.9;
// plans/os-1dad487d.md; spec/executors.md): provision a
// workspace with its handoff packet, wake advisorily, meter usage
// onto the observation stream, and report the provisioned runtime
// tuple. The local worktree adapter is the first implementation;
// container, cloud-session, and enrolled-remote adapters are later
// phases' rows.
//
// Provisioning is fenced to the reservation gate: a run provisions
// only against an admitted run.started, which admission granted only
// against an open, valid budget reservation — reserve, start,
// provision, meter, settle. Disposal is the caller's contract:
// dispose only after the last confirmed, admitted synchronization
// (the disposability drill proves that discipline loses nothing
// admitted; the loss window is the observation lines after the last
// sync).
package executor

import (
	"errors"
	"fmt"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// Tuple is the runtime tuple (charter §II.5; internal/tuple), the
// configuration a qualification binds to. An adapter reports it twice
// (plans/os-8e53ffd9.md D3): Adapter.Tuple() is the STATIC, partial
// report before any provision, the fields the adapter controls; and
// Run.Tuple() is what it RESOLVED, checked inside Provision against
// the configuration the admitted run.started declared, before any
// execution is released.
type Tuple = tuple.Tuple

// ErrTupleMismatch refuses a provision whose resolved configuration
// differs from the one the admitted start declared: the ledger admitted
// a configuration and the adapter built another, and execution under a
// configuration the ledger did not admit is the thing III.E row 6
// forbids. Nothing is left behind: the check runs with the rollback
// armed.
var ErrTupleMismatch = errors.New("the provisioned runtime tuple differs from the one the admitted run.started declared — execution is released only under the configuration the ledger admitted (spec/qualification.md)")

// ProvisionSpec names everything a provision needs: the ledger the
// admitted run.started lives in, the repository and base revision
// the workspace detaches from, the contract subject, the executing
// actor and claim fence (the observation stream key), the handoff
// packet bytes, and the observation directory.
type ProvisionSpec struct {
	Ledger  string
	Repo    string
	Base    string
	Subject string
	Actor   string
	Fence   int
	Started int
	Packet  []byte
	ObsDir  string
	// Lessons is the surfacing set for the subject at claim time
	// (plans/os-96850e5a.md D6), written beside the packet as
	// lessons.json: a sibling file rather than a fifth packet key,
	// because the ledger packet's four-part shape refuses unknown
	// keys and the provisioned directory is the handoff, not the
	// fact. Nil writes an empty list.
	Lessons []byte
}

// Run is one provisioned execution run.
type Run interface {
	// Workspace is the provisioned working directory.
	Workspace() string
	// Tuple is the runtime tuple the adapter RESOLVED for this run:
	// what was actually provisioned, never what was declared.
	Tuple() Tuple
	// Meter appends one metered observation line to the run's
	// stream: usage rides the ephemeral channel and settles to the
	// ledger at run end via run.settled.
	Meter(units int, step string) error
	// Dispose removes the workspace. The caller disposes only after
	// the last confirmed, admitted synchronization.
	Dispose() error
}

// Adapter is the executor adapter: provision, wake, and the
// provisioned tuple. Wake is advisory transport only — its total
// failure costs latency, never correctness (the wakeless poll-only
// drill).
type Adapter interface {
	Provision(spec ProvisionSpec) (Run, error)
	Wake(actor string) error
	Tuple() Tuple
}

// ErrNoAdmittedStart refuses a provision whose spec cites no
// admitted run.started for its fence: no execution run provisions
// outside the reservation gate.
var ErrNoAdmittedStart = errors.New("no admitted run.started for this fence — a run provisions only inside the reservation gate (reserve, then run.started, then Provision)")

// LocalWorktree is the local worktree adapter: a detached git
// worktree as the workspace, the packet at .seed-run/packet.json,
// and metering onto the per-fence observation stream.
type LocalWorktree struct {
	// Resolve is the post-provision resolution seam: given the
	// configuration the admitted start declared and the workspace
	// just built, it returns what was ACTUALLY provisioned. Nil means
	// the local adapter's own resolution, which is honest about its
	// limit: a worktree cannot see which model a lane process will
	// call, so it resolves harness and environment from what it built
	// and takes principal, model and tool policy from the declaration.
	// Drills set it to an adapter that resolves something else and
	// must be refused.
	Resolve func(declared Tuple, workspace string) Tuple
}

// LocalHarness and LocalEnvironment are the two fields the local
// worktree adapter can resolve for itself.
const (
	LocalHarness     = "local-worktree/v0"
	LocalEnvironment = "detached-git-worktree"
)

// Tuple reports the static, partial configuration: the two fields this
// adapter controls, the other three left for the declaring caller.
func (LocalWorktree) Tuple() Tuple {
	return Tuple{Harness: LocalHarness, Environment: LocalEnvironment}
}

func (lw LocalWorktree) resolve(declared Tuple, workspace string) Tuple {
	if lw.Resolve != nil {
		return lw.Resolve(declared, workspace)
	}
	out := declared
	out.Harness = LocalHarness
	out.Environment = LocalEnvironment
	return out
}

// Wake is the documented no-op: the advisory channel that does
// nothing is the honest v0, and polling loses only latency.
func (LocalWorktree) Wake(string) error { return nil }

// Provision verifies the admitted run.started, creates the detached
// worktree, writes the packet, and ensures the observation stream
// directory. Git runs with fixed argument vectors; nothing is
// interpolated into a shell.
func (lw LocalWorktree) Provision(spec ProvisionSpec) (Run, error) {
	started, err := verifyStarted(spec)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "seed-run-")
	if err != nil {
		return nil, err
	}
	workspace := filepath.Join(dir, "ws")
	add := exec.Command("git", "-C", spec.Repo, "worktree", "add", "--detach", workspace, spec.Base)
	if out, err := add.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("worktree add: %v: %s", err, out)
	}
	// Every failure past the add rolls the worktree registration and
	// the allocation back (review finding on the task PR): a refused
	// provision leaks no checkout.
	rollback := func() {
		_ = exec.Command("git", "-C", spec.Repo, "worktree", "remove", "--force", workspace).Run()
		_ = os.RemoveAll(dir)
	}
	runDir := filepath.Join(workspace, ".seed-run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		rollback()
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, "packet.json"), spec.Packet, 0o644); err != nil {
		rollback()
		return nil, err
	}
	lessons := spec.Lessons
	if len(lessons) == 0 {
		lessons = []byte("[]\n")
	}
	if err := os.WriteFile(filepath.Join(runDir, "lessons.json"), lessons, 0o644); err != nil {
		rollback()
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(spec.ObsDir, spec.Actor), 0o755); err != nil {
		rollback()
		return nil, err
	}
	// The resolved-against-admitted check (plans/os-8e53ffd9.md D3),
	// with the rollback armed: a start that declared a configuration
	// releases execution only under that configuration. A start with
	// no declaration (a seed/1 chain) has nothing to check against and
	// the resolved value is still reported.
	var resolved Tuple
	if started.Tuple != nil {
		resolved = lw.resolve(*started.Tuple, workspace)
		if field, have, want, differs := resolved.Diff(*started.Tuple); differs {
			rollback()
			return nil, fmt.Errorf("%w: %s resolved to %q, the admitted start declared %q", ErrTupleMismatch, field, have, want)
		}
	} else {
		resolved = lw.resolve(Tuple{}, workspace)
	}
	return &localRun{spec: spec, workspace: workspace, tuple: resolved}, nil
}

// verifyStarted replays the ledger and requires the admitted
// run.started the spec cites, at its position, for this fence.
func verifyStarted(spec ProvisionSpec) (*transition.RunStartFact, error) {
	store, err := ledger.Open(spec.Ledger)
	if err != nil {
		return nil, err
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, err
	}
	var records []*event.Record
	if _, err := store.VerifyFromGenesis(resolve, ledger.WithObserver(func(pos int, r *event.Record) {
		records = append(records, r)
	})); err != nil {
		return nil, err
	}
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	s, ok := table.FoldRecords(records).State(spec.Subject)
	if !ok {
		return nil, ErrNoAdmittedStart
	}
	for i := range s.RunStarts {
		st := s.RunStarts[i]
		if st.Pos == spec.Started && st.Fence == spec.Fence &&
			admit.RunStartValid(records, table, spec.Subject, st) {
			// Fold presence is never proof of admission (review
			// finding on the task PR): the start must pass the same
			// position-accurate boundary the run rule enforces, or a
			// raw-pushed start would provision an unbudgeted
			// workspace.
			return &st, nil
		}
	}
	return nil, ErrNoAdmittedStart
}

type localRun struct {
	spec      ProvisionSpec
	workspace string
	tuple     Tuple
}

func (r *localRun) Workspace() string { return r.workspace }

// Tuple is what this run resolved to, never what its start declared.
func (r *localRun) Tuple() Tuple { return r.tuple }

func (r *localRun) Meter(units int, step string) error {
	return obs.Append(r.spec.ObsDir, r.spec.Actor, obs.FormatFence(r.spec.Fence), obs.Line{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Subject: r.spec.Subject,
		Step:    step,
		Units:   units,
	})
}

func (r *localRun) Dispose() error {
	remove := exec.Command("git", "-C", r.spec.Repo, "worktree", "remove", "--force", r.workspace)
	if out, err := remove.CombinedOutput(); err != nil {
		return fmt.Errorf("worktree remove: %v: %s", err, out)
	}
	return nil
}
