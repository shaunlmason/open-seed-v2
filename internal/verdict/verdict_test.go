package verdict

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// repo builds a real source repository: a base commit, an acceptance
// spec commit, and a head commit changing a file. Returns the repo dir
// and the three full SHAs.
// hardenGitRepo disables every path that can spawn a git process
// outliving the test that created the repository
// (plans/os-c4e8b57a.md D1, D2). `t.TempDir` removes its tree
// recursively at cleanup, and a detached auto-gc still writing under
// it fails the removal AFTER the assertions passed: the worst shape of
// flake for an unattended loop, because the signal says "your change
// is broken" when the change is fine.
//
// The three settings are three different spawners, and are WRITTEN
// into the repository rather than passed as `git -c` flags: `-c`
// scopes a value to one invocation and writes nothing, so the later
// commits, and above all a bare remote's own receive-pack, would still
// run under stock auto-gc. `init` and `clone --bare` produce no
// objects of their own, so a config write on the next line is still
// before the first object and there is no window to lose.
func hardenGitRepo(t testing.TB, repo string) {
	t.Helper()
	for _, kv := range [][2]string{
		{"gc.auto", "0"},            // the heuristic itself
		{"gc.autoDetach", "false"},  // any gc that runs stays in the foreground
		{"receive.autoGC", "false"}, // the push path, which is the one that bit
		{"core.autocrlf", "false"},  // the ledger is LF-only on every platform (spec/platform.md)
		{"core.eol", "lf"},
	} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("hardening %s (%s): %v %s", repo, kv[0], err, out)
		}
	}
}

func repo(t *testing.T) (dir, base, spec, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		return gitOut(t, dir, args...)
	}
	run("init", "--quiet", "-b", "main")
	hardenGitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "base")
	base = strings.TrimSpace(run("rev-parse", "HEAD"))
	specBody := "# Acceptance\n\nCriteria prose.\n\n## Validation Commands\n\n" +
		"- Boundary: `printf ok`\n" +
		"- `test -f hello.txt`\n"
	if err := os.WriteFile(filepath.Join(dir, "accept.md"), []byte(specBody), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "spec")
	spec = strings.TrimSpace(run("rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "head")
	head = strings.TrimSpace(run("rev-parse", "HEAD"))
	return dir, base, spec, head
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	out, err := runGit(full)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func gated(spec, commit string) *transition.AcceptanceInfo {
	return &transition.AcceptanceInfo{Ref: "accept.md @ " + commit, Executable: true, Gated: true}
}

func TestReceiptDeterministicDigest(t *testing.T) {
	dir, base, spec, head := repo(t)
	in := Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gated("accept.md", spec)}
	r1, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := r1.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := r2.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("two computations of one submission must agree: %s vs %s", d1, d2)
	}
	if r1.MergeBase != base || r1.Head != head {
		t.Fatalf("receipt stores full immutable SHAs: %s..%s", r1.MergeBase, r1.Head)
	}
	if len(r1.Files) != 2 || r1.Files[0] != "accept.md" || r1.Files[1] != "hello.txt" {
		t.Fatalf("inventory recomputed from the checkout: %v", r1.Files)
	}
	if len(r1.Transcripts) != 2 || r1.Transcripts[0].Exit != 0 || r1.Transcripts[1].Exit != 0 {
		t.Fatalf("gated spec commands run with exits recorded: %+v", r1.Transcripts)
	}
	if r1.Environment.Runner != ExecProfile || r1.Environment.OS == "" || r1.Environment.Go == "" {
		t.Fatalf("environment fingerprint carries the runner profile: %+v", r1.Environment)
	}
	if r1.Plan != nil {
		t.Fatalf("a planless contract yields plan null, got %+v", r1.Plan)
	}
}

func TestReceiptTamperChangesDigest(t *testing.T) {
	dir, base, spec, head := repo(t)
	full, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gated("accept.md", spec)})
	if err != nil {
		t.Fatal(err)
	}
	// A swapped head (the spec commit instead of the submission head)
	// is a different attested triple: recompute-and-mismatch.
	swapped, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + spec, Acceptance: gated("accept.md", spec)})
	if err != nil {
		t.Fatal(err)
	}
	df, _ := full.Digest()
	ds, _ := swapped.Digest()
	if df == ds {
		t.Fatal("a different head must change the receipt digest")
	}
}

func TestNonDescendantHeadRefused(t *testing.T) {
	dir, base, _, head := repo(t)
	var re *RangeError
	if _, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: head + ".." + base}); !errors.As(err, &re) || !strings.Contains(re.Reason, "descend") {
		t.Fatalf("a head that does not descend from its merge-base refuses before checkout, got %v", err)
	}
	if _, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: "nonsense"}); !errors.As(err, &re) {
		t.Fatalf("a malformed range refuses, got %v", err)
	}
}

func TestPlanHashedAtMergeBase(t *testing.T) {
	dir, _, spec, head := repo(t)
	// The plan blob exists at the spec commit; anchor it there and use
	// the spec commit as merge-base so the blob resolves at mb.
	planPath := "plans/p.md"
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, planPath), []byte("plan body v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "--quiet", "-m", "plan")
	planCommit := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "--quiet", "-m", "work")
	newHead := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	r, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: planCommit + ".." + newHead,
		PlanAnchor: planPath + " @ " + planCommit, Acceptance: gated("accept.md", spec)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Plan == nil || r.Plan.Path != planPath || len(r.Plan.SHA256) != 64 {
		t.Fatalf("the approved plan is hashed at the merge-base: %+v", r.Plan)
	}
	// A plan that does not exist at the merge-base refuses: nothing
	// vouched for the submission's plan binding.
	var re *RangeError
	if _, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: spec + ".." + head,
		PlanAnchor: planPath + " @ " + planCommit}); !errors.As(err, &re) || !strings.Contains(re.Reason, "merge-base") {
		t.Fatalf("a plan absent at the merge-base refuses, got %v", err)
	}
	_ = head
}

func TestGateBeforeRun(t *testing.T) {
	dir, base, spec, head := repo(t)
	sentinel := filepath.Join(t.TempDir(), "executed")
	// The committed spec's commands are harmless; the drill plants a
	// destructive-looking command via a hostile ungated declaration:
	// with gate evidence absent nothing executes at all.
	var ug *UngatedError
	_, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head,
		Acceptance: &transition.AcceptanceInfo{Ref: "accept.md @ " + spec, Executable: true, Gated: false}})
	if !errors.As(err, &ug) {
		t.Fatalf("ungated executable content refuses exit-18 ungated, got %v", err)
	}
	if _, serr := os.Stat(sentinel); !os.IsNotExist(serr) {
		t.Fatal("nothing may execute on the ungated path")
	}
	// Declared executable, gated, but the body has no commands section:
	// declared-armed-but-empty refuses rather than passing vacuously.
	gitOut(t, dir, "checkout", "--quiet", "main")
	if err := os.WriteFile(filepath.Join(dir, "empty.md"), []byte("# Prose only\n\ncriteria without commands\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "--quiet", "-m", "empty spec")
	emptyCommit := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	var su *SpecUnrunnableError
	_, err = Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + emptyCommit,
		Acceptance: &transition.AcceptanceInfo{Ref: "empty.md @ " + emptyCommit, Executable: true, Gated: true}})
	if !errors.As(err, &su) {
		t.Fatalf("declared-executable content with no parseable commands refuses spec_unrunnable, got %v", err)
	}
	// Prose-only acceptance runs nothing and the receipt carries an
	// explicitly empty transcript list.
	r, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head,
		Acceptance: &transition.AcceptanceInfo{Ref: "accept.md @ " + spec, Executable: false, Gated: true}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Transcripts == nil || len(r.Transcripts) != 0 {
		t.Fatalf("prose-only acceptance yields an explicit empty transcript list, got %+v", r.Transcripts)
	}
}

// The per-run clone carries auto-gc disabled in its own config from
// the moment it exists (plans/os-711b3028.md D2, D3): read with
// --local, the scope the test binary's global config cannot satisfy,
// because the earlier drill read the effective value and passed for
// the wrong reason. Cleanup runs right after the checkout that arms
// git's collector, which is the race this write removes.
func TestWorkspaceCloneHasNoAutoGC(t *testing.T) {
	dir, _, _, head := repo(t)
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	for key, want := range map[string]string{
		"gc.auto":        "0",
		"gc.autoDetach":  "false",
		"receive.autoGC": "false",
	} {
		out, err := exec.Command("git", "-C", ws.Repo, "config", "--local", "--get", key).CombinedOutput()
		if err != nil {
			t.Fatalf("%s unset in the clone's own config: %v %s", key, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s = %q in the clone, want %q", key, got, want)
		}
	}
}

// The same GIT_CONFIG case for the clone (review finding on #232): the
// workspace hardens its own config under the variable and leaves the
// operator's selected file alone.
func TestWorkspaceHardensDespiteGitConfigSelection(t *testing.T) {
	dir, _, _, head := repo(t)
	external := filepath.Join(t.TempDir(), "operator-config")
	t.Setenv("GIT_CONFIG", external)
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatalf("NewWorkspace under GIT_CONFIG: %v", err)
	}
	defer ws.Cleanup()
	for key, want := range map[string]string{"gc.auto": "0", "gc.autoDetach": "false", "receive.autoGC": "false"} {
		cmd := exec.Command("git", "-C", ws.Repo, "config", "--local", "--get", key)
		cmd.Env = withoutGitConfigSelection(os.Environ())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s unset in the clone's own config under GIT_CONFIG: %v %s", key, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if b, err := os.ReadFile(external); err == nil {
		t.Fatalf("the operator's selected config file was written: %q", b)
	}
}

func TestRunnerProfileScrubsEnvironment(t *testing.T) {
	dir, base, _, head := repo(t)
	t.Setenv("SEED_TEST_SECRET", "hunter2")
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	tr := Runner{}.Run(ws, `printf '%s' "$SEED_TEST_SECRET"`)
	if tr.Exit != 0 || tr.OutputBytes != 0 {
		t.Fatalf("a planted invoking-environment secret must be invisible to spec commands: %+v", tr)
	}
	tr = Runner{}.Run(ws, `test "$HOME" != "" && test -d "$HOME"`)
	if tr.Exit != 0 {
		t.Fatalf("the profile provides a scratch HOME inside the run root: %+v", tr)
	}
	_ = base
}

func TestWorkspaceCannotReachParentRefs(t *testing.T) {
	dir, _, _, head := repo(t)
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	// No origin remote survives; a push has nowhere configured to go.
	tr := Runner{}.Run(ws, "git remote")
	if tr.Exit != 0 || tr.OutputBytes != 0 {
		t.Fatalf("the origin remote is removed: %+v", tr)
	}
	if tr := (Runner{}).Run(ws, "git push origin HEAD:refs/heads/pwned"); tr.Exit == 0 {
		t.Fatal("a push to origin must fail: no remote names the parent")
	}
	// A ref updated inside the clone stays inside the clone: the
	// workspace shares no ref store with the parent (the worktree hole
	// this design rejects).
	if tr := (Runner{}).Run(ws, "git update-ref refs/heads/pwned HEAD"); tr.Exit != 0 {
		t.Fatalf("updating a ref inside the clone is the clone's business: %+v", tr)
	}
	if out, err := runGit([]string{"-C", dir, "show-ref", "--verify", "refs/heads/pwned"}); err == nil {
		t.Fatalf("the parent repository must not see the clone's ref: %s", out)
	}
}

func TestRunnerTimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no process groups: the runner kills the shell alone and its children may hold the pipes (spec/platform.md)")
	}
	dir, _, _, head := repo(t)
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	start := time.Now()
	tr := Runner{Timeout: 200 * time.Millisecond}.Run(ws, "sleep 30")
	if tr.Exit == 0 {
		t.Fatal("a command killed at the timeout must not report success")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("the timeout kill must not wait out the child")
	}
}

func TestParallelWorkspacesShareNothing(t *testing.T) {
	dir, base, spec, head := repo(t)
	var wg sync.WaitGroup
	digests := make([]string, 4)
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gated("accept.md", spec)})
			if err != nil {
				errs[i] = err
				return
			}
			digests[i], errs[i] = r.Digest()
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("parallel run %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("parallel verdicts never collide and agree byte for byte: %v", digests)
		}
	}
}

func TestCleanupFiresPassAndFail(t *testing.T) {
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	dir, base, spec, head := repo(t)
	if _, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head, Acceptance: gated("accept.md", spec)}); err != nil {
		t.Fatal(err)
	}
	// The failure path: an ungated spec aborts after the workspace
	// exists; cleanup still fires.
	if _, err := Compute(Input{RepoDir: dir, Contract: "c-1", Base: base + ".." + head,
		Acceptance: &transition.AcceptanceInfo{Ref: "accept.md @ " + spec, Executable: true, Gated: false}}); err == nil {
		t.Fatal("the ungated path must refuse")
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "seed-verdict-") {
			t.Fatalf("cleanup fires pass or fail; %s survived", e.Name())
		}
	}
}

func TestWorkspaceCheckoutMatchesHead(t *testing.T) {
	dir, _, _, head := repo(t)
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	got, err := ws.git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != head {
		t.Fatalf("the workspace checks out exactly the submission head: %s vs %s", got, head)
	}
	// A dirty parent working tree never leaks: the clone reads only
	// committed content.
	if err := os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws2, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Cleanup()
	if _, err := os.Stat(filepath.Join(ws2.Repo, "uncommitted.txt")); !os.IsNotExist(err) {
		t.Fatal("uncommitted parent content must not appear in a clean checkout")
	}
}

// runGit is the tests' raw git runner for repositories outside any
// workspace (fixture setup and parent-side assertions).
func runGit(args []string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %v: %v: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

func TestRefusalMessagesNameTheirContracts(t *testing.T) {
	// The refusal prose is part of the operator surface: each names
	// the offending piece and points at the spec.
	msgs := []struct {
		err  error
		want []string
	}{
		{&UngatedError{Contract: "c-1", Ref: "a.md @ abc1234"}, []string{"c-1", "gate", "verdicts.md"}},
		{&SpecUnrunnableError{Contract: "c-1", Ref: "a.md @ abc1234"}, []string{"c-1", "parseable", "verdicts.md"}},
		{&RangeError{Contract: "c-1", Base: "x..y", Reason: "not a range"}, []string{"c-1", "x..y", "not a range"}},
	}
	for _, m := range msgs {
		for _, w := range m.want {
			if !strings.Contains(m.err.Error(), w) {
				t.Fatalf("%T message %q must mention %q", m.err, m.err.Error(), w)
			}
		}
	}
}

func TestCloneSharesNoObjectStorage(t *testing.T) {
	// A same-filesystem local clone must copy objects, never
	// hard-link them: with a shared inode, a hostile spec command
	// overwriting a clone-side object file would corrupt the parent
	// repository's object store despite the removed origin. The drill
	// makes every clone-side loose object writable garbage and then
	// asserts the parent still verifies from its own store.
	dir, _, _, head := repo(t)
	ws, err := NewWorkspace(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	tr := Runner{}.Run(ws, `chmod -R u+w .git/objects && find .git/objects -type f | while read -r f; do echo garbage > "$f"; done`)
	if tr.Exit != 0 {
		t.Fatalf("the corruption command itself must run: %+v", tr)
	}
	if _, err := runGit([]string{"-C", dir, "fsck", "--no-progress", "--strict"}); err != nil {
		t.Fatalf("the parent object store must survive clone-side corruption: %v", err)
	}
	if out, err := runGit([]string{"-C", dir, "cat-file", "-e", head + "^{commit}"}); err != nil {
		t.Fatalf("the parent must still serve the head commit: %v %s", err, out)
	}
}
