package simulate

// Fault injection: a loop.Verbs seam that refuses every act reaches the
// error paths of append1, build, stageContract and drive without a real
// deployment, so the simulation's refusal handling is covered.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
)

// refuse answers every verb with a refusal.
type refuse struct{}

func (refuse) Run(args ...string) loop.Result {
	return loop.Result{Exit: 5, OK: false, Code: "unavailable", Message: "injected refusal"}
}

const lanesRel = "../../lanes"

func TestBuildFailsWhenTheBoundaryRefuses(t *testing.T) {
	// The remote and genesis land (real git), then the first admitted
	// setup fact refuses: build surfaces it rather than a half-built
	// deployment.
	_, err := build(Config{LanesDir: lanesRel, Verbs: refuse{}, WorkDir: t.TempDir(), Now: time.Now()})
	if err == nil {
		t.Fatal("build must fail when the boundary refuses the setup facts")
	}
}

func TestAppend1SurfacesRefusal(t *testing.T) {
	d := &deployment{remote: "r", state: "s", verbs: refuse{}}
	if err := d.append1("k", "actor.enrolled", "fp", "{}"); err == nil {
		t.Error("append1 must return the boundary's refusal")
	}
}

func TestStageContractSurfacesRefusal(t *testing.T) {
	d := &deployment{
		remote: "r", state: "s", verbs: refuse{},
		keys: map[string]string{"dispatcher": "d", "supervisor": "s"},
		now:  time.Now(),
	}
	if err := d.stageContract("c-1", repo{spec: "abc"}, time.Now()); err == nil {
		t.Error("stageContract must surface a refusal")
	}
}

func TestDriveSurfacesRefusal(t *testing.T) {
	// A real manifest and repo, a refusing seam: the implementer loop
	// cannot submit, and drive reports it.
	dir := t.TempDir()
	d := &deployment{
		dir: dir, remote: "r", state: "s", verbs: refuse{},
		keys:     map[string]string{"implementer": mustKey(t, dir, "impl", 20)},
		manifest: mustManifests(t),
	}
	if err := d.drive("c-1", repo{src: dir, base: "aaa", head: "bbb"}); err == nil {
		t.Error("drive must fail when the loop cannot act")
	}
}

func mustKey(t *testing.T, dir, name string, seed byte) string {
	t.Helper()
	p, _, _, err := keyAt(dir, name, seed)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustManifests(t *testing.T) map[string]lane.Manifest {
	t.Helper()
	ms, err := lane.Load(lanesRel)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]lane.Manifest{}
	for _, x := range ms {
		m[x.Lane] = x
	}
	return m
}

func TestRunSurfacesBuildFailure(t *testing.T) {
	// Run reports the build failure rather than a partial report.
	if _, err := Run(Config{LanesDir: lanesRel, Verbs: refuse{}, Intents: 1, WorkDir: t.TempDir()}); err == nil {
		t.Error("Run must fail when the deployment cannot be built")
	}
}

func TestCurateAndMaintainTolerateRefusal(t *testing.T) {
	// curate and maintain ignore a refusal (there may be nothing to
	// propose or reap): they run without returning it.
	d := &deployment{
		remote: "r", state: "s", verbs: refuse{},
		keys: map[string]string{"curator": "c", "maintenance": "m"},
		now:  time.Now(),
	}
	d.curate()
	d.maintain(time.Now())
}

// okThenFail admits the first n acts, then refuses — so build reaches
// its enroll loop before failing.
type okThenFail struct {
	n int
	c int
}

func (s *okThenFail) Run(args ...string) loop.Result {
	s.c++
	if s.c <= s.n {
		return loop.Result{Exit: 0, OK: true}
	}
	return loop.Result{Exit: 5, OK: false, Code: "unavailable", Message: "injected"}
}

func TestBuildFailsInTheEnrollLoop(t *testing.T) {
	// The upgrade admits, the first enrollment refuses: build surfaces it
	// from inside the enroll loop.
	_, err := build(Config{LanesDir: lanesRel, Verbs: &okThenFail{n: 1}, WorkDir: t.TempDir(), Now: time.Now()})
	if err == nil {
		t.Fatal("build must fail when an enrollment is refused")
	}
}

func TestInstallHookFailsWithoutSource(t *testing.T) {
	// The hook cannot build without the module source: installHook
	// surfaces the build failure.
	if err := installHook(t.TempDir(), filepath.Join(t.TempDir(), "no-such-next")); err == nil {
		t.Error("installHook must fail when seed-admit cannot be built")
	}
}

func TestBuildRepoFailsOnBadDir(t *testing.T) {
	d := &deployment{dir: filepath.Join("/proc", "nonexistent-xyz")}
	if _, err := d.buildRepo("c-1", shipped[0]); err == nil {
		t.Error("buildRepo must fail when its working directory cannot be created")
	}
}

func TestStageContractHorizonWithPastArrival(t *testing.T) {
	// An arrival instant in the past: the offer's expiry must still sit
	// past the real event ts, so the horizon takes the wall-clock branch
	// rather than the arrival. The dispatcher's two facts admit, the
	// offer is reached, then refused — exercising the horizon branch.
	d := &deployment{
		remote: "r", state: "s", verbs: &okThenFail{n: 2},
		keys: map[string]string{"dispatcher": "d", "supervisor": "s"},
		now:  time.Now(),
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := d.stageContract("c-1", repo{spec: "abc"}, past); err == nil {
		t.Error("stageContract must surface the offer refusal after the horizon is computed")
	}
}
