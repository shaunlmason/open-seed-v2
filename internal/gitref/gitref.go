// Package gitref rides the ledger on a git ref (charter Part II section 1
// Reference deployment; plans/os-62e2aa1d.md): a small exec seam around the
// git CLI (the engine's gitx pattern, no module dependencies) plus the
// optimistic append loop that makes the ref safely multi-writer. The loop
// re-links and re-signs on every retry (prev is inside the signed form, so
// a signing closure is the only way to move a draft onto a fresh tip), and
// the client persists its newest verified remote head, refusing head
// regression (charter III.A freshness; the monotonic-head rule).
package gitref

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

// Typed refusals: each is a distinct condition callers branch on.
var (
	ErrNonFastForward = errors.New("push rejected: non-fast-forward (another writer won the race)")
	ErrRemoteRejected = errors.New("push rejected by the remote (a policy refusal, not a race)")
	ErrUnavailable    = errors.New("remote unreachable")
	ErrHeadRegression = errors.New("fetched tip regresses the persisted verified head — refusing (monotonic-head rule)")
	ErrRetriesSpent   = errors.New("append retries exhausted without winning the race")
)

// Client is one actor's view of a remote ledger ref: a private git dir for
// fetches and commits, a head cache dir for the persisted verified heads
// (client state, never ledger state), and the remote+ref coordinates.
type Client struct {
	Remote   string
	Ref      string
	gitDir   string
	cacheDir string
	clock    func() time.Time // nil: the ledger's own clock; tests inject a segment day
	proposer Proposer
}

// Proposer lands signed records on the remote ref through a third
// party instead of the client's own push: the admission service under
// the enforced-forge-hosted posture (plans/os-5c8a312c.md D3). The
// records arrive already linked and signed; a proposer relays, never
// re-signs.
type Proposer interface {
	Propose(ref string, recs []*event.Record) (*Result, error)
}

// Refusal is a proposal the service refused with the boundary's own
// envelope. Exit, Code and Message are the service's verbatim, so a
// proposer renders exactly the code the hook posture would have
// produced for the same record; it unwraps to ErrRemoteRejected for
// callers that only ask whether the remote said no.
type Refusal struct {
	Exit     int
	Code     string
	Message  string
	Position *string
}

func (r *Refusal) Error() string { return fmt.Sprintf("%s: %s", r.Code, r.Message) }
func (r *Refusal) Unwrap() error { return ErrRemoteRejected }

// WithProposer makes the append loop propose instead of push. Every
// step before the last — fetch, materialize, verify, re-link, re-sign,
// validate — is unchanged: the cooperative half is what every posture
// keeps, and only the write moves to the service.
func (c *Client) WithProposer(p Proposer) *Client {
	c.proposer = p
	return c
}

// GitDir is the client's private git dir: the objects it fetched and
// the commits it built, which the admission service judges in place.
func (c *Client) GitDir() string { return c.gitDir }

// WithClock names the clock the per-attempt store stamps its segment
// files with (ledger.WithClock); the loop's own behavior does not read
// it. Tests use it to drive two writers across a midnight, the one
// condition the 200-writer storm that lost an append had
// (plans/os-5063e8ba.md D4).
func (c *Client) WithClock(now func() time.Time) *Client {
	c.clock = now
	return c
}

// RefusedDir is where an attempt keeps the tree a hook refused as
// bad_prev at or beyond the position it appended (plans/os-5063e8ba.md
// D2): <state dir>/refused/<commit>/ holds the committed tree and the
// hook's message, so the next occurrence is a directory to read.
func (c *Client) RefusedDir() string { return filepath.Join(filepath.Dir(c.gitDir), "refused") }

// NewClient prepares the client's git dir under stateDir and uses
// stateDir/heads as the persisted-head cache.
func NewClient(stateDir, remote, ref string) (*Client, error) {
	gitDir := filepath.Join(stateDir, "gitdir")
	cacheDir := filepath.Join(stateDir, "heads")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if _, err := runGit("", "init", "-q", "--bare", gitDir); err != nil {
			return nil, err
		}
	}
	if err := hardenGitDir(gitDir); err != nil {
		return nil, err
	}
	return &Client{Remote: remote, Ref: ref, gitDir: gitDir, cacheDir: cacheDir}, nil
}

// noAutoGC is the repository-local configuration every git dir the
// engine creates for itself carries (plans/os-711b3028.md D1, D2):
// the client's private transport dir and the verifier's per-run
// clone are ephemeral, engine-owned state, and a collector git
// detaches after a fetch, a receive or a checkout would mutate them
// after the process that armed it has exited. A caller removing its
// state dir (a CI job cleaning a workspace) then hits the same
// directory-not-empty failure the drills hit under t.TempDir.
var noAutoGC = [][2]string{
	{"gc.auto", "0"},
	{"gc.autoDetach", "false"},
	{"receive.autoGC", "false"},
}

// hardenGitDir writes noAutoGC into the git dir on every construction,
// not only at init: a state dir an older build created is hardened the
// first time a new build opens it, and three idempotent config writes
// cost less than a stat-and-branch that could drift from what the
// drill asserts. A write that fails is the client's error, named by
// key: a git that cannot configure its own repository cannot be
// trusted to fetch from it either. The writes run git's own writer,
// not an in-process parser of the existing file: a partial parser
// drifts from git's resolution (a concatenated value like auto = "0"1
// reads as 01 in git, not 0), and the drift would skip the very write
// that disarms the collector (plans/os-711b3028.md D1; review on
// #298). Three spawns per construction is the price of that
// guarantee; the suite's other spawn cuts land on the fetch and the
// unpack (spec/platform.md).
func hardenGitDir(gitDir string) error {
	for _, kv := range noAutoGC {
		cmd := exec.Command("git", "--git-dir", gitDir, "config", "--local", kv[0], kv[1])
		cmd.Env = withoutGitConfigSelection(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("harden %s: git config: %w: %s", kv[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// withoutGitConfigSelection drops GIT_CONFIG from an environment. The
// variable selects the file `git config` reads and writes: an
// unqualified write under it lands in whatever file the operator
// named, and `--local` under it refuses ("only one config file at a
// time") rather than overriding it (review finding on #232). The
// hardening therefore names its target explicitly AND runs without
// the variable, so the repository's own config is the only file it
// touches and a file the operator selected is never mutated by Seed.
// internal/verdict carries the same filter for its workspace clone.
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

func runGit(gitDir string, args ...string) (string, error) {
	// The ledger is LF-only (spec/platform.md): git is told so on
	// every call, so a checkout or archive on a platform whose
	// default converts line endings never hands the verifier bytes
	// the signatures do not cover.
	full := append([]string{"-c", "core.autocrlf=false", "-c", "core.eol=lf"}, args...)
	if gitDir != "" {
		full = append([]string{"--git-dir", gitDir, "-c", "core.autocrlf=false", "-c", "core.eol=lf"}, args...)
	}
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

const localTracking = "refs/seed/fetched"

// headKey is the cache file for this remote+ref.
func (c *Client) headKey() string {
	sum := sha256.Sum256([]byte(c.Remote + "\x00" + c.Ref))
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:16]))
}

// RecordVerifiedHead persists commit as the newest verified remote
// head: the caller's statement that it fetched and fully verified this
// tip. From then on Fetch refuses anything that regresses it (the
// monotonic-head rule), including later in the same invocation, before
// AppendLoop's own persistence kicks in (plans/os-895bf828.md step 1).
func (c *Client) RecordVerifiedHead(commit string) error {
	return c.persistHead(commit)
}

// PersistedHead returns the last verified remote commit, if any.
func (c *Client) PersistedHead() (string, bool, error) {
	b, err := os.ReadFile(c.headKey())
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(b)), true, nil
}

func (c *Client) persistHead(commit string) error {
	tmp := c.headKey() + ".tmp"
	if err := os.WriteFile(tmp, []byte(commit+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.headKey())
}

// Fetch pulls the remote ref into the local tracking ref and enforces the
// monotonic-head rule: a fetched tip that does not contain the persisted
// verified head refuses with ErrHeadRegression naming both commits. An
// absent remote ref yields an empty commit id (a fresh ledger).
//
// The common path is one git process: the fetch itself, with the tip
// read back from the tracking ref it wrote. The ls-remote that used to
// precede every fetch is spent only when the fetch refuses, to tell an
// absent ref (a fresh ledger) from an unreachable remote. Each spawn is
// cheap on Linux and dear on Windows, and every append pays this path
// at least once (spec/platform.md, the cmd/seed residual).
func (c *Client) Fetch() (commit string, err error) {
	if _, fetchErr := runGit(c.gitDir, "fetch", "-q", "--force", c.Remote, c.Ref+":"+localTracking); fetchErr != nil {
		out, lsErr := runGit(c.gitDir, "ls-remote", c.Remote, c.Ref)
		if lsErr != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, lsErr)
		}
		if len(strings.Fields(out)) != 0 {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, fetchErr)
		}
		// An absent ref is a fresh ledger only for a client that never
		// verified a head. After a verified head, a vanished ref is the
		// deepest possible regression: refusing here keeps AppendLoop from
		// pushing a brand-new root over deleted history (#86 review).
		persisted, have, err := c.PersistedHead()
		if err != nil {
			return "", err
		}
		if have {
			return "", fmt.Errorf("%w: persisted %.12s, remote ref vanished", ErrHeadRegression, persisted)
		}
		return "", nil
	}
	tip, err := c.trackingTip()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	persisted, have, err := c.PersistedHead()
	if err != nil {
		return "", err
	}
	if have && persisted != tip {
		if _, err := runGit(c.gitDir, "merge-base", "--is-ancestor", persisted, tip); err != nil {
			return "", fmt.Errorf("%w: persisted %.12s, fetched %.12s", ErrHeadRegression, persisted, tip)
		}
	}
	return tip, nil
}

// trackingTip reads the commit the tracking ref names. A fetch writes
// the ref loose, so the common path is one file read and no process;
// a ref stored any other way (a packed or reftable store) is resolved
// by git itself.
func (c *Client) trackingTip() (string, error) {
	if b, err := os.ReadFile(filepath.Join(c.gitDir, filepath.FromSlash(localTracking))); err == nil {
		if id := strings.TrimSpace(string(b)); isObjectID(id) {
			return id, nil
		}
	}
	out, err := runGit(c.gitDir, "rev-parse", "--verify", localTracking)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// isObjectID reports whether s is a full hex object id (SHA-1 or SHA-256).
func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// Materialize extracts the fetched ledger tree into dir (which must be
// empty or absent). An empty commit id materializes an empty dir. The
// archive git writes is read by this process: the tar that used to
// unpack it was a second spawn per materialization, and every append
// materializes at least once.
func (c *Client) Materialize(commit, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if commit == "" {
		return nil
	}
	archive := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "--git-dir", c.gitDir, "archive", "--format=tar", commit)
	var stderr strings.Builder
	archive.Stderr = &stderr
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		return err
	}
	extractErr := untar(pipe, dir)
	// Drain what is left so git can exit whatever the extraction did.
	io.Copy(io.Discard, pipe)
	if err := archive.Wait(); err != nil {
		return fmt.Errorf("git archive %.12s: %w: %s", commit, err, strings.TrimSpace(stderr.String()))
	}
	if extractErr != nil {
		return fmt.Errorf("git archive %.12s: %w", commit, extractErr)
	}
	return nil
}

// untar unpacks a tar stream under dir: directories, regular files
// with their permission bits, and symbolic links, which is what a git
// tree holds. An entry that would land outside dir is refused by name.
func untar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		if rel, err := filepath.Rel(dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("entry %q escapes the target directory", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("entry %q has unsupported type %q", hdr.Name, hdr.Typeflag)
		}
	}
}

// CommitAndPush commits dir's tree on top of parent and pushes it to the
// remote ref. A non-fast-forward rejection returns ErrNonFastForward;
// other push failures return ErrUnavailable.
func (c *Client) CommitAndPush(dir, parent, message string) (string, error) {
	commit, err := c.Commit(dir, parent, message)
	if err != nil {
		return "", err
	}
	if err := c.Push(commit); err != nil {
		return "", err
	}
	return commit, nil
}

// Commit writes dir's tree as a commit on top of parent in the client's
// git dir and returns its id, pushing nothing: the admission service
// judges a candidate this way before deciding whether it is pushed at
// all (plans/os-5c8a312c.md D1).
func (c *Client) Commit(dir, parent, message string) (string, error) {
	if _, err := runGit(c.gitDir, "-c", "core.bare=false", "--work-tree", dir, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := runGit(c.gitDir, "write-tree")
	if err != nil {
		return "", err
	}
	commitArgs := []string{"commit-tree", strings.TrimSpace(tree), "-m", message}
	if parent != "" {
		commitArgs = append(commitArgs, "-p", parent)
	}
	cmd := exec.Command("git", append([]string{"--git-dir", c.gitDir, "-c", "core.autocrlf=false", "-c", "core.eol=lf",
		"-c", "user.name=seed-ledger", "-c", "user.email=ledger@seed"}, commitArgs...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w: %s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// Push updates the remote ref to commit, fast-forward only. A lost race
// returns ErrNonFastForward, a policy rejection ErrRemoteRejected, and
// anything else ErrUnavailable.
func (c *Client) Push(commit string) error {
	pushOut, err := runGit(c.gitDir, "push", "--porcelain", c.Remote, commit+":"+c.Ref)
	if err != nil {
		combined := pushOut + " " + err.Error()
		// Only a lost race retries, and races have specific shapes: the
		// stale-parent rejection (reasons "non-fast-forward", "fetch
		// first", "stale info"), server-side receive contention ("failed
		// to lock", and its update-phase shape "failed to update ref",
		// which a rival landing between advertisement and update produces
		// on the receiving side), and mid-push ref-lock contention
		// ("cannot lock ref ... but expected") when the other writer
		// lands between our fetch and our update. Any other rejection
		// (a pre-receive policy hook declining, say) is a refusal to
		// surface, never to retry (#86 review; the update-phase shape
		// was observed in the 2.2 CLI race drill).
		race := strings.Contains(combined, "non-fast-forward") ||
			strings.Contains(combined, "fetch first") ||
			strings.Contains(combined, "stale info") ||
			strings.Contains(combined, "failed to lock") ||
			strings.Contains(combined, "failed to update ref") ||
			(strings.Contains(combined, "cannot lock ref") && strings.Contains(combined, "but expected"))
		if race {
			return ErrNonFastForward
		}
		if strings.Contains(combined, "[remote rejected]") || strings.Contains(combined, "[rejected]") {
			return fmt.Errorf("%w: %s", ErrRemoteRejected, strings.TrimSpace(pushOut))
		}
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}
