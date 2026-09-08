package simulate

// Error paths a real deployment never takes, reached without one: a work
// root that cannot hold a temp dir, a state dir the gitref client cannot
// initialize, a remote whose hooks dir cannot receive the pre-receive
// hook, Run's intent default, and the state of a subject the chain never
// mentions.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/loop"
)

// blockedRoot returns a path beneath a regular file: no platform can
// create a directory under it, so MkdirAll and MkdirTemp both refuse
// (a path under /proc is ordinary on Windows, where it once let the
// deployment reach genesis with no key).
func blockedRoot(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(file, "nonexistent-seed-sim")
}

func TestRunDefaultsIntentsThenSurfacesBuildFailure(t *testing.T) {
	// A non-positive intent count is defaulted to one before the build,
	// whose refusal Run still reports.
	if _, err := Run(Config{LanesDir: lanesRel, Verbs: refuse{}, Intents: 0, WorkDir: t.TempDir()}); err == nil {
		t.Error("Run must report the build failure after defaulting the intent count")
	}
}

func TestBuildFailsWhenTheWorkRootCannotHoldATempDir(t *testing.T) {
	if _, err := build(Config{LanesDir: lanesRel, Verbs: refuse{}, WorkDir: blockedRoot(t), Now: time.Now()}); err == nil {
		t.Error("build must fail when its temp dir cannot be created under the work root")
	}
}

func TestClientStateDirFailuresSurface(t *testing.T) {
	// The read-work and genesis-work state dirs live under the
	// deployment dir; when that cannot be created, materialize and
	// seedGenesis surface the client's error.
	d := &deployment{dir: blockedRoot(t), remote: "r"}
	if _, err := d.materialize(); err == nil {
		t.Error("materialize must fail when the read-work dir cannot be created")
	}
	if err := d.seedGenesis(); err == nil {
		t.Error("seedGenesis must fail when the genesis-work dir cannot be created")
	}
}

func TestInstallHookFailsWhenTheRemoteCannotTakeIt(t *testing.T) {
	nextDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// hooks is a file: the hooks dir cannot be created.
	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "hooks"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installHook(remote, nextDir); err == nil {
		t.Error("installHook must fail when hooks/ cannot be created")
	}
	// pre-receive is a directory: the hook script cannot be written.
	remote = t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "hooks", "pre-receive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installHook(remote, nextDir); err == nil {
		t.Error("installHook must fail when hooks/pre-receive cannot be written")
	}
}

func TestStateOfAnUnmentionedSubjectIsAbsent(t *testing.T) {
	// An always-admitting seam builds the deployment with only genesis
	// on the real remote: a subject the chain never names is absent,
	// not unknown.
	d, err := build(Config{LanesDir: lanesRel, Verbs: &okThenFail{n: 1 << 20}, WorkDir: t.TempDir(), Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.stateOf("c-1"); got != "absent" {
		t.Errorf("stateOf an unmentioned subject = %q, want absent", got)
	}
}

// refuseOn admits every act except one whose args carry the named
// verb, so a run fails at a chosen step after a real build.
type refuseOn struct{ verb string }

func (s refuseOn) Run(args ...string) loop.Result {
	for _, a := range args {
		if a == s.verb {
			return loop.Result{Exit: 5, OK: false, Code: "unavailable", Message: "injected refusal of " + s.verb}
		}
	}
	return loop.Result{Exit: 0, OK: true}
}

func TestRunSurfacesStageRefusal(t *testing.T) {
	// The build lands, the contract's specification is refused: Run
	// reports the stage failure for that subject.
	_, err := Run(Config{LanesDir: lanesRel, Verbs: refuseOn{"contract.specified"}, Intents: 1, WorkDir: t.TempDir(), Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "stage") {
		t.Errorf("Run must surface the stage refusal, got %v", err)
	}
}

func TestRunSurfacesDriveFailure(t *testing.T) {
	// Every act admits but nothing real lands on the remote, so the
	// implementer never submits: Run reports the drive failure.
	_, err := Run(Config{LanesDir: lanesRel, Verbs: refuseOn{"no-such-verb"}, Intents: 1, WorkDir: t.TempDir(), Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "drive") {
		t.Errorf("Run must surface the drive failure, got %v", err)
	}
}
