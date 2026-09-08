package packet_test

// The packet-resume drill (plans/os-b07b0f59.md; the Phase 5 exit
// criterion; conformance III.F "a fresh executor completes a killed
// executor's contract from the packet alone, including not re-trying
// recorded dead ends"). Executor A is a scripted harness: it completes
// the FIRST acceptance item in a real git repository, records one
// verified decision, one asserted decision, and one dead end, pushes,
// and is force-reaped with the second item unfinished. Executor B is a
// deterministic function of the packet plus the instantiation's durable
// configuration (the repository coordinate lives in the instantiation,
// never in the packet: anchors only mean something inside the
// instantiation that recorded them): a fresh clone at the packet's
// anchors, no transcript, no workspace reuse. B verifies the finished
// item from its anchors, PERFORMS the unfinished one, and lands it on
// the remote. Sufficiency is the assertion, not a vibe.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/packet"
)

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

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=drill", "GIT_AUTHOR_EMAIL=drill@example.invalid",
		"GIT_COMMITTER_NAME=drill", "GIT_COMMITTER_EMAIL=drill@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitShow reads a path's bytes at a revision, untrimmed: artifact bytes
// are the assertion, so content never rides the trimming helper, and a
// failure surfaces git's own stderr so the diagnosis is actionable.
func gitShow(t *testing.T, dir, spec string) []byte {
	t.Helper()
	cmd := exec.Command("git", "show", spec)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("git show %s: %v\n%s", spec, err, ee.Stderr)
		}
		t.Fatalf("git show %s: %v", spec, err)
	}
	return out
}

// instantiationConfig models the coordination side's durable record.
// It survives any executor's death by construction, and it is where
// the repository coordinate lives; a packet carries anchors into this
// repository, not the repository's own address.
type instantiationConfig struct {
	Remote string `json:"remote"`
}

// resumeAction is one line of B's action log: what B did, mechanically.
type resumeAction struct {
	Approach string
	Done     string
}

// resolveAnchored reads one packet ref from its OWN declared anchor: a
// commit anchor at that commit, a range anchor at the range's head.
// The checked-out worktree is never the source of truth, or a ref
// naming any commit but the head would silently lie.
func resolveAnchored(t *testing.T, clone, ref string) []byte {
	t.Helper()
	path, anchor, ok := strings.Cut(ref, " @ ")
	if !ok {
		t.Fatalf("packet ref %q is not anchored", ref)
	}
	at := anchor
	if _, rangeHead, isRange := strings.Cut(anchor, ".."); isRange {
		at = rangeHead
	}
	return gitShow(t, clone, at+":"+path)
}

// executorB is a deterministic function of the packet and the durable
// instantiation config alone: it clones fresh at the packet's anchors,
// skips every recorded dead end, verifies the acceptance items A
// finished, performs the ones A did not, and pushes the completed
// contract. It returns its action log plus the artifact bytes it
// resolved from the packet's anchors.
func executorB(t *testing.T, configPath string, p *packet.Packet) ([]resumeAction, map[string][]byte) {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg instantiationConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	clone := t.TempDir()
	git(t, ".", "clone", "-q", cfg.Remote, clone)
	hardenGitRepo(t, clone)
	_, head, ok := strings.Cut(p.Base, "..")
	if !ok {
		t.Fatalf("packet base is not a range: %s", p.Base)
	}
	git(t, clone, "checkout", "-q", "-B", "resume", head)

	// B's candidate approaches: the recorded dead ends are excluded
	// before anything runs: that is what findings are FOR.
	dead := map[string]bool{}
	for _, f := range p.Findings {
		dead[f.Tried] = true
	}
	approach := ""
	for _, cand := range []string{"approach-X", "approach-Y"} {
		if !dead[cand] {
			approach = cand
			break
		}
	}
	if approach == "" {
		t.Fatal("every candidate approach is a recorded dead end")
	}

	// Every ref resolves from its declared anchor before any work runs.
	artifacts := map[string][]byte{}
	for _, r := range p.Refs {
		artifacts[r] = resolveAnchored(t, clone, r)
	}

	// B works the acceptance list: verification before Done, and an
	// unfinished item is PERFORMED, never transcribed.
	var log []resumeAction
	for _, item := range p.Acceptance {
		switch {
		case strings.Contains(item, "manifest.txt"):
			// The unfinished item. Under the live approach, the
			// manifest is a pure function of the packet's refs.
			seen := map[string]bool{}
			var paths []string
			for _, r := range p.Refs {
				path, _, _ := strings.Cut(r, " @ ")
				if !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
			}
			sort.Strings(paths)
			content := strings.Join(paths, "\n") + "\n"
			if err := os.WriteFile(filepath.Join(clone, "manifest.txt"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, clone, "add", "manifest.txt")
			git(t, clone, "commit", "-q", "-m", "resume: manifest of packet ref paths")
			git(t, clone, "push", "-q", "origin", "HEAD:refs/heads/main")
			// Verified from the committed tree, not the intent.
			if got := gitShow(t, clone, "HEAD:manifest.txt"); string(got) != content {
				t.Fatalf("manifest verification failed: %q", got)
			}
			log = append(log, resumeAction{Approach: approach, Done: item})
		default:
			// A's finished item: verified against its anchor, the
			// checked-out bytes agreeing with the anchor-resolved ones.
			verified := false
			for _, r := range p.Refs {
				path, anchor, _ := strings.Cut(r, " @ ")
				if path != "artifact.txt" || strings.Contains(anchor, "..") {
					continue
				}
				wt, err := os.ReadFile(filepath.Join(clone, path))
				if err == nil && bytes.Equal(wt, artifacts[r]) {
					// Equality against the anchor-resolved bytes is
					// the whole check: a legitimately empty artifact
					// verifies too, and a missing file already failed
					// the read above.
					verified = true
				}
			}
			if !verified {
				t.Fatalf("item %q failed anchor verification", item)
			}
			log = append(log, resumeAction{Approach: approach, Done: item})
		}
	}
	return log, artifacts
}

func TestPacketResumeDrill(t *testing.T) {
	// A real code remote, distinct from the coordination ledger, and
	// the instantiation's durable config naming it: both outlive A.
	remote := t.TempDir()
	git(t, ".", "init", "-q", "--bare", remote)
	hardenGitRepo(t, remote)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "instantiation.json")
	cfg, err := json.Marshal(instantiationConfig{Remote: remote})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	workA := t.TempDir()
	git(t, ".", "clone", "-q", remote, workA)
	hardenGitRepo(t, workA)

	// Executor A: the base commit (the merge-base), then the work
	// commit, which both adds the artifact and CHANGES README.md so a
	// base-anchored ref and the head disagree about its bytes.
	if err := os.WriteFile(filepath.Join(workA, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, workA, "add", ".")
	git(t, workA, "commit", "-q", "-m", "base")
	mergeBase := git(t, workA, "rev-parse", "HEAD")
	artifact := []byte("executor A's result\n")
	if err := os.WriteFile(filepath.Join(workA, "artifact.txt"), artifact, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workA, "README.md"), []byte("amended\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, workA, "add", ".")
	git(t, workA, "commit", "-q", "-m", "work")
	head := git(t, workA, "rev-parse", "HEAD")
	git(t, workA, "push", "-q", "origin", "HEAD:refs/heads/main")

	// A is force-reaped BETWEEN its two acceptance items: the artifact
	// landed, the manifest never existed. The packet is everything A
	// leaves behind.
	reapPacket, err := json.Marshal(map[string]any{
		"acceptance": []string{
			"artifact.txt matches its packet anchor",
			"manifest.txt lists every packet ref path",
		},
		"decisions": []map[string]string{
			{"decision": "the artifact lives at the repository root", "basis": "verified"},
			{"decision": "no consumer reads it yet", "basis": "asserted"},
		},
		"base": mergeBase + ".." + head,
		"refs": []string{
			"artifact.txt @ " + head,
			"README.md @ " + mergeBase,
			"README.md @ " + mergeBase + ".." + head,
		},
		"findings": []map[string]string{{"tried": "approach-X", "outcome": "fails: the root layout rejects it"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, perr := packet.Parse("c-resume", reapPacket)
	if perr != nil {
		t.Fatalf("A's reap packet must be shape-valid: %v", perr)
	}

	// Executor A's workspace is gone, and the remote's tree at the kill
	// point holds no manifest: what B lands below is work, not
	// inheritance.
	if err := os.RemoveAll(workA); err != nil {
		t.Fatal(err)
	}
	pre := exec.Command("git", "show", "main:manifest.txt")
	pre.Dir = remote
	if out, err := pre.CombinedOutput(); err == nil {
		t.Fatalf("fixture broke: the manifest exists before B ran: %s", out)
	}

	log, artifacts := executorB(t, configPath, p)

	// B never re-tries the recorded dead end.
	for _, a := range log {
		if a.Approach == "approach-X" {
			t.Fatalf("B re-tried the recorded dead end: %+v", log)
		}
	}
	// B completed the acceptance list.
	done := map[string]bool{}
	for _, a := range log {
		done[a.Done] = true
	}
	for _, item := range p.Acceptance {
		if !done[item] {
			t.Fatalf("B must complete the acceptance list from the packet alone; missing %q in %+v", item, log)
		}
	}
	// Each ref resolved from its OWN anchor: the artifact at the head,
	// the base-anchored README at the base (the head disagrees), the
	// range-anchored README at the range's head.
	if got := artifacts["artifact.txt @ "+head]; !bytes.Equal(got, artifact) {
		t.Fatalf("B must reproduce A's artifact from its commit anchor, got %q", got)
	}
	if got := artifacts["README.md @ "+mergeBase]; string(got) != "base\n" {
		t.Fatalf("a base-anchored ref must resolve at its own commit, not the checkout, got %q", got)
	}
	if got := artifacts["README.md @ "+mergeBase+".."+head]; string(got) != "amended\n" {
		t.Fatalf("a range-anchored ref must resolve at the range's head, got %q", got)
	}
	// The unfinished item landed durably: the remote's main now carries
	// the manifest B built from the packet's refs.
	want := "README.md\nartifact.txt\n"
	if got := gitShow(t, remote, "main:manifest.txt"); string(got) != want {
		t.Fatalf("B must land the unfinished item on the remote, got %q want %q", got, want)
	}
}
