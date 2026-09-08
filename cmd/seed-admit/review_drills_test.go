package main

// The review's two fail-closed arms (os-0f924157 task PR review, both P1):
// the serve side must resolve the deployment's default branch through the
// transport (an SSH/HTTPS remote is not a `--git-dir` path), and the
// ledger half must refuse rather than judge with every declaration-driven
// rule disabled when the default branch cannot even be resolved.

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

func runGit(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

// bareOn pins a fresh bare remote's HEAD to the named branch, returns the
// remote, and leaves a first commit on the branch when the decl file is
// given (a branch with no object is indistinguishable from unborn over
// ls-remote, and the resolution's job is the case where the branch
// stands).
func bareOn(t *testing.T, branch, decl string) string {
	t.Helper()
	remote := t.TempDir() + "/" + branch + ".git"
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	hardenGitRepo(t, remote)
	if _, err := runGit("--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		t.Fatal(err)
	}
	if decl == "" {
		return remote
	}
	work := t.TempDir()
	if _, err := runGit("-C", work, "init", "-q", "-b", branch); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit("-C", work, "config", "user.email", "t@t"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit("-C", work, "config", "user.name", "t"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work+"/"+decl, []byte(`{"posture": "enforced-self-hosted"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit("-C", work, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit("-C", work, "commit", "-q", "--allow-empty", "-m", "declaration"); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit("-C", work, "push", "-q", remote, "HEAD:refs/heads/"+branch); err != nil {
		t.Fatalf("push: %v %s", err, out)
	}
	return remote
}

// conformance: the transport resolution distinguishes the three remote
// states — a symref resolves to the branch (the local and the transport
// cases), an unborn HEAD (an empty deployment) resolves to the
// no-declaration case, and a detached HEAD refuses: never the silent
// no-declaration the old code produced for a URL remote it could not
// read.
func TestDefaultBranchRemoteResolution(t *testing.T) {
	pop := bareOn(t, "main", "seed.json")
	if branch, err := defaultBranchRemote(pop); err != nil || branch != "refs/heads/main" {
		t.Fatalf("a symref remote resolves to its default branch: %q %v", branch, err)
	}
	// Through the transport a URL remote uses: file://, the same code the
	// SSH/HTTPS spelling runs.
	if branch, err := defaultBranchRemote("file://" + pop); err != nil || branch != "refs/heads/main" {
		t.Fatalf("a file:// remote resolves through the transport: %q %v", branch, err)
	}

	// An empty remote: unborn HEAD, the hook as before.
	empty := bareOn(t, "main", "")
	if branch, err := defaultBranchRemote(empty); err != nil || branch != "" {
		t.Fatalf("an unborn HEAD is the no-declaration case, not an error: %q %v", branch, err)
	}

	// A detached HEAD refuses: the mirror would otherwise be judged
	// without a declaration it could not have read.
	det := bareOn(t, "main", "seed.json")
	if out, err := runGit("--git-dir", det, "update-ref", "--no-deref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatalf("detach the HEAD: %v %s", err, out)
	}
	if _, err := defaultBranchRemote(det); err == nil {
		t.Fatal("a detached HEAD must not resolve to a branch")
	} else if !strings.Contains(err.Error(), "detached") {
		t.Fatalf("the refusal names the detached HEAD, got %v", err)
	}
}

// conformance: the ledger half and the code half agree when the default
// branch cannot be resolved — both refuse. A push judged with every
// declaration-driven rule disabled while the other half refuses the same
// state is the split boundary this change closes.
func TestLedgerHalfRefusesWhenTheDefaultBranchIsDetached(t *testing.T) {
	remote := guardedRemote(t)
	resolve := seedGenesis(t, remote)

	// Detach the guarded repository's HEAD: the default branch no longer
	// resolves, so the declaration cannot be read at all.
	tip := remoteTip(t, remote)
	if _, err := runGit("--git-dir", remote, "update-ref", "--no-deref", "HEAD", tip); err != nil {
		t.Fatalf("detach the HEAD: %v", err)
	}

	err := craftPush(t, remote, resolve, func(dir string, store *ledger.Store) {
		appendRaw(t, store, resolve, signed(t, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store)))
	})
	if err == nil || !strings.Contains(ruleLine(err.Error()), "repairs the repository's HEAD") {
		t.Fatalf("the ledger half refuses with an unresolvable default branch, naming it: %v", err)
	}
}
