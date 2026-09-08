// The verifier workspace and the profiled runner (plans/os-f6d2c267.md;
// SEED-NEXT.md Part II §8 "clean per-run workspace" and §6 "the
// verifier executes specs in a sandbox with declared, minimal
// capability"; conformance III.G row 4). The workspace is a detached
// local clone with the origin remote removed, deliberately not a git
// worktree: a worktree checkout shares the parent repository's refs and
// object store through its .git link, handing any hostile spec command
// git update-ref reach back into the host repo (review finding on the
// plan). The runner is an interface-shaped seam: v0 ships the exec
// profile, and a namespaced or containerized profile slots in at the
// executor-adapter seam (build plan Phase 7 item 3) without touching
// verdict logic.

package verdict

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
)

// Workspace is one verdict run's isolated checkout: root holds the
// clone (repo/), a scratch home (home/), and a scratch tmp (tmp/), all
// removed together by Cleanup whatever the run's outcome.
type Workspace struct {
	root string
	// Repo is the checkout directory commands run in.
	Repo string
	home string
	tmp  string
	// traces and sealedTraces hold the exports commands write to
	// SEED_TRACE_EXPORT (plans/os-7fc2ca38.md D2): outside the clone,
	// so the diff and the inventory never see them, and inside the
	// per-run root, so cleanup removes them pass or fail.
	traces       string
	sealedTraces string
}

// NewWorkspace clones repoDir at head into a fresh per-run root. The
// clone is detached at exactly the submission head and its origin
// remote is removed, so nothing in the workspace names a path back to
// the parent repository.
func NewWorkspace(repoDir, head string) (*Workspace, error) {
	root, err := os.MkdirTemp("", "seed-verdict-*")
	if err != nil {
		return nil, fmt.Errorf("verdict workspace: %w", err)
	}
	ws := &Workspace{root: root, Repo: filepath.Join(root, "repo"), home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"),
		traces: filepath.Join(root, "traces"), sealedTraces: filepath.Join(root, "sealed-traces")}
	for _, d := range []string{ws.home, ws.tmp, ws.traces, ws.sealedTraces} {
		if err := os.Mkdir(d, 0o755); err != nil {
			ws.Cleanup()
			return nil, fmt.Errorf("verdict workspace: %w", err)
		}
	}
	steps := [][]string{
		// --no-hardlinks: a same-filesystem local clone otherwise
		// hard-links loose object files, so a hostile spec command
		// overwriting one through the shared inode would corrupt the
		// parent repository's object store despite the removed origin
		// (review finding on the task PR). Copied objects make the
		// isolation real; the drill corrupts every clone-side object
		// and asserts the parent still verifies.
		{"clone", "--quiet", "--no-checkout", "--no-hardlinks", repoDir, ws.Repo},
		// The clone carries auto-gc disabled before anything runs in
		// it (plans/os-711b3028.md D2): a checkout arms git's
		// collector, and this clone's Cleanup runs right after the
		// commands that arm it, which is exactly the race a detached
		// gc loses against a directory removal. Repository-local, so
		// no config outside the workspace is consulted or written.
		{"-C", ws.Repo, "config", "--local", "gc.auto", "0"},
		{"-C", ws.Repo, "config", "--local", "gc.autoDetach", "false"},
		{"-C", ws.Repo, "config", "--local", "receive.autoGC", "false"},
		{"-C", ws.Repo, "checkout", "--quiet", "--detach", head},
		{"-C", ws.Repo, "remote", "remove", "origin"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		// GIT_CONFIG would redirect the config writes to a file the
		// operator selected, or make --local refuse; the clone's own
		// config is the only file this workspace may touch
		// (internal/gitref's withoutGitConfigSelection, same finding).
		cmd.Env = withoutGitConfigSelection(os.Environ())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			ws.Cleanup()
			return nil, fmt.Errorf("verdict workspace: git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
		}
	}
	return ws, nil
}

// Cleanup removes the whole per-run root. It is safe to call twice and
// runs on the failure path exactly as on success: cleanup fires pass
// or fail (III.G row 4).
func (w *Workspace) Cleanup() {
	if w.root != "" {
		os.RemoveAll(w.root)
	}
}

// git runs a read-side git command inside the workspace clone and
// returns its stdout. This is the verifier's own reading, never a spec
// command: it runs with the invoking environment, not the profile.
func (w *Workspace) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", w.Repo}, args...)...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// RunnerProfile names a declared capability profile; the receipt
// records which profile its transcripts ran under.
const ExecProfile = "exec"

// Runner executes spec commands under the declared v0 exec profile:
// scrubbed environment (explicit minimal PATH; HOME and TMPDIR inside
// the per-run root; nothing inherited), per-command wall-clock timeout
// with process-group kill, network honestly unrestricted.
type Runner struct {
	// Timeout bounds each command; zero means DefaultTimeout.
	Timeout time.Duration
}

// DefaultTimeout is the per-command wall-clock bound.
const DefaultTimeout = 10 * time.Minute

// Transcript is one executed command's receipt entry: the command, its
// exit (negative when the profile killed it at the timeout), and the
// digest and byte count of its combined output — never inline bytes,
// so receipts stay bounded.
type Transcript struct {
	Cmd          string `json:"cmd"`
	Exit         int    `json:"exit"`
	OutputSHA256 string `json:"output_sha256"`
	OutputBytes  int    `json:"output_bytes"`
}

// TraceExportVar is the environment variable the exec profile sets
// per command (plans/os-7fc2ca38.md D2): the path a harness that
// attaches a trace to its run writes one OTLP/JSON export to. A
// harness that ignores it loses only the richer evidence, and its
// transcript is byte-identical to one run without the variable.
const TraceExportVar = "SEED_TRACE_EXPORT"

// tracePath is the export path for command n of the visible or the
// sealed list.
func (w *Workspace) tracePath(n int, sealed bool) string {
	dir := w.traces
	if sealed {
		dir = w.sealedTraces
	}
	return filepath.Join(dir, fmt.Sprintf("%d.json", n))
}

// Run executes one spec command in the workspace under the exec
// profile and returns its transcript. The command's exit never aborts
// the run: a red check is a fact the receipt records and the render
// rule consumes.
func (r Runner) Run(ws *Workspace, command string) Transcript {
	t, _, _ := r.RunTraced(ws, command, "")
	return t
}

// RunTraced is Run with SEED_TRACE_EXPORT set to export (an empty
// export sets nothing); it returns the export's bytes and true when
// the command wrote one. The bytes are what the caller normalizes;
// the file itself stays in the per-run root for cleanup.
func (r Runner) RunTraced(ws *Workspace, command, export string) (Transcript, []byte, bool) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = ws.Repo
	cmd.Env = []string{
		"PATH=" + runnerPath(),
		"HOME=" + ws.home,
		"TMPDIR=" + ws.tmp,
		"GIT_CONFIG_NOSYSTEM=1",
		"LANG=C",
	}
	if export != "" {
		// Forward slashes: the value is read by a shell, and a Windows
		// sh takes a drive path with either separator while a
		// backslash inside a double-quoted redirection is an escape.
		cmd.Env = append(cmd.Env, TraceExportVar+"="+filepath.ToSlash(export))
	}
	// The process group and its kill are platform code
	// (workspace_unix.go, workspace_windows.go; spec/platform.md):
	// a hanging pipeline dies with its children where the platform
	// has process groups, and with its shell where it has not.
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	out, err := cmd.CombinedOutput()
	t := Transcript{Cmd: command, OutputSHA256: artifact.Digest(out), OutputBytes: len(out)}
	switch {
	case err == nil:
		t.Exit = 0
	case ctx.Err() != nil:
		t.Exit = -1
	default:
		if ee, ok := err.(*exec.ExitError); ok {
			t.Exit = ee.ExitCode()
		} else {
			t.Exit = -1
		}
	}
	if export != "" {
		if raw, rerr := os.ReadFile(export); rerr == nil {
			return t, raw, true
		}
	}
	return t, nil, false
}

// withoutGitConfigSelection drops GIT_CONFIG, the variable that
// selects the file `git config` reads and writes, so the workspace's
// hardening lands in the clone's own config and never in a file the
// operator named (review finding on #232). The same filter lives in
// internal/gitref for the client's dir; two lines are cheaper than an
// import between a transport client and the verifier's workspace.
func withoutGitConfigSelection(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_CONFIG=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
