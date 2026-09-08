package executor

// Container is the containerised adapter (plans/os-083112ac.md D1, D4):
// it builds the same detached worktree the local adapter does, starts a
// container over it through an OCI runtime, and resolves harness and
// environment from what it built — harness "container/v0", environment
// the image digest (or "fake-oci:<digest>" under the in-process fake, so
// a report never mistakes it for a real image). The supervisor stops the
// container, so its budget is enforced. Credentials are never held: the
// image reference is declared, not a secret.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

// ContainerHarness is the versioned name the container adapter resolves.
const ContainerHarness = "container/v0"

// OCIRuntime is the small contract the container adapter drives: start a
// container over a bind-mounted workspace (returning its id and the
// image digest), stop it, remove it. `executor/fakeoci` implements it
// in-process for the credential-free drill.
type OCIRuntime interface {
	Start(image, workspace string) (id, digest string, err error)
	Stop(id string) error
	Remove(id string) error
}

// ImageResolver is the optional runtime capability that names an image's
// digest before any container starts, so the static tuple can report
// the environment a provision will resolve to (spec/qualification.md:
// run start fills environment from the adapter's static report).
type ImageResolver interface {
	ResolveImage(image string) (digest string, err error)
}

// Container provisions through an OCI runtime. Fake marks the in-process
// runtime so the resolved environment names it.
type Container struct {
	Runtime OCIRuntime
	Image   string
	Fake    bool
}

// Tuple reports the harness and, when the runtime can name the declared
// image's digest ahead of a start, the environment a provision resolves
// to. A runtime that cannot leaves the field empty, and run start
// refuses by naming it rather than inventing a value.
func (c Container) Tuple() Tuple {
	t := Tuple{Harness: ContainerHarness}
	if r, ok := c.Runtime.(ImageResolver); ok && c.Image != "" {
		if digest, err := r.ResolveImage(c.Image); err == nil && digest != "" {
			if c.Fake {
				digest = "fake-oci:" + digest
			}
			t.Environment = digest
		}
	}
	return t
}

// Wake is the documented no-op.
func (Container) Wake(string) error { return nil }

// Describe reports the enforced posture: the supervisor stops the
// container, so the reservation is a guarantee.
func (Container) Describe() Description {
	return Description{Name: "container", Harness: ContainerHarness, Budget: BudgetEnforced,
		Reason: "the supervisor stops the container, so the reservation is a guarantee"}
}

// Provision verifies the admitted start, builds the worktree, starts a
// container over it, and holds the resolved tuple to the admitted one.
func (c Container) Provision(spec ProvisionSpec) (Run, error) {
	if c.Runtime == nil {
		return nil, fmt.Errorf("the container adapter needs an OCI runtime")
	}
	started, err := verifyStarted(spec)
	if err != nil {
		return nil, err
	}
	dir, workspace, rollback, err := buildWorktree(spec)
	if err != nil {
		return nil, err
	}
	id, digest, err := c.Runtime.Start(c.Image, workspace)
	if err != nil {
		rollback()
		return nil, fmt.Errorf("starting the container: %w", err)
	}
	env := digest
	if c.Fake {
		env = "fake-oci:" + digest
	}
	resolve := func(declared Tuple) Tuple {
		out := declared
		out.Harness = ContainerHarness
		out.Environment = env
		return out
	}
	dispose := func() {
		_ = c.Runtime.Stop(id)
		_ = c.Runtime.Remove(id)
		rollback()
	}
	var resolved Tuple
	if started.Tuple != nil {
		resolved = resolve(*started.Tuple)
		if field, have, want, differs := resolved.Diff(*started.Tuple); differs {
			dispose()
			return nil, fmt.Errorf("%w: %s resolved to %q, the admitted start declared %q", ErrTupleMismatch, field, have, want)
		}
	} else {
		resolved = resolve(Tuple{})
	}
	return &containerRun{spec: spec, workspace: workspace, tuple: resolved, runtime: c.Runtime, id: id, dir: dir}, nil
}

type containerRun struct {
	spec      ProvisionSpec
	workspace string
	tuple     Tuple
	runtime   OCIRuntime
	id        string
	dir       string
}

func (r *containerRun) Workspace() string { return r.workspace }
func (r *containerRun) Tuple() Tuple      { return r.tuple }

func (r *containerRun) Meter(units int, step string) error {
	return obs.Append(r.spec.ObsDir, r.spec.Actor, obs.FormatFence(r.spec.Fence), obs.Line{
		TS: time.Now().UTC().Format(time.RFC3339), Subject: r.spec.Subject, Step: step, Units: units,
	})
}

// Dispose stops and removes the container, then removes the worktree.
func (r *containerRun) Dispose() error {
	var first error
	if err := r.runtime.Stop(r.id); err != nil {
		first = err
	}
	if err := r.runtime.Remove(r.id); err != nil && first == nil {
		first = err
	}
	remove := exec.Command("git", "-C", r.spec.Repo, "worktree", "remove", "--force", r.workspace)
	if out, err := remove.CombinedOutput(); err != nil && first == nil {
		first = fmt.Errorf("worktree remove: %v: %s", err, out)
	}
	_ = os.RemoveAll(r.dir)
	return first
}

// buildWorktree creates the detached worktree, writes the packet and
// lessons, and ensures the observation-stream directory — the shared
// substrate the local and container adapters both run over. It returns
// the allocation dir, the workspace, and a rollback that leaks nothing.
func buildWorktree(spec ProvisionSpec) (dir, workspace string, rollback func(), err error) {
	dir, err = os.MkdirTemp("", "seed-run-")
	if err != nil {
		return "", "", nil, err
	}
	workspace = filepath.Join(dir, "ws")
	add := exec.Command("git", "-C", spec.Repo, "worktree", "add", "--detach", workspace, spec.Base)
	if out, aerr := add.CombinedOutput(); aerr != nil {
		_ = os.RemoveAll(dir)
		return "", "", nil, fmt.Errorf("worktree add: %v: %s", aerr, out)
	}
	rollback = func() {
		_ = exec.Command("git", "-C", spec.Repo, "worktree", "remove", "--force", workspace).Run()
		_ = os.RemoveAll(dir)
	}
	runDir := filepath.Join(workspace, ".seed-run")
	if err = os.MkdirAll(runDir, 0o755); err != nil {
		rollback()
		return "", "", nil, err
	}
	if err = os.WriteFile(filepath.Join(runDir, "packet.json"), spec.Packet, 0o644); err != nil {
		rollback()
		return "", "", nil, err
	}
	lessons := spec.Lessons
	if len(lessons) == 0 {
		lessons = []byte("[]\n")
	}
	if err = os.WriteFile(filepath.Join(runDir, "lessons.json"), lessons, 0o644); err != nil {
		rollback()
		return "", "", nil, err
	}
	if err = os.MkdirAll(filepath.Join(spec.ObsDir, spec.Actor), 0o755); err != nil {
		rollback()
		return "", "", nil, err
	}
	return dir, workspace, rollback, nil
}
