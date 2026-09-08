package redteam

// The fixture deployment (plans/os-465e356e.md D1, D3, D6): a bare
// remote with the production hook installed, the deployment declaration
// committed on the default branch, and a ledger staged HONESTLY through
// the client seam far enough that the adversary has something to attack
// — a governance root, the lanes it needs beside it, and contracts in
// every state the ceiling names. Staging goes through the same
// admission the CLI runs (admit.Validate over gitref.AppendLoop), so
// every fact the adversary later abuses passed the boundary; nothing is
// staged raw.

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	// GuardedRef is the ledger ref the hook guards.
	GuardedRef = "refs/seed/ledger"
	// DefaultBranch is the fixture's default branch; the hook reads it
	// from the repository's HEAD symref, so the fixture pins it there.
	DefaultBranch = "refs/heads/main"
	// PusherEnv carries the transport's identity assertion into the
	// hook (D2). One spelling, shared with cmd/seed-admit by contract.
	PusherEnv = "SEED_PUSHER"
	// Anchor is a well-formed commit anchor for acceptance refs and
	// packet ranges; admission checks shape, and the drill's contracts
	// are prose-only (trivial tier), so no repository stands behind it.
	Anchor = "0123456789abcdef0123456789abcdef01234567"

	// The contracts the staging leaves behind.
	ContractHeld     = "c-adv"     // held by the adversary, in_progress
	ContractReview   = "c-sub"     // submitted by the adversary, in review
	ContractReleased = "c-old"     // held then released by the adversary: ready, with a stale fence
	ContractPeer     = "c-peer"    // held by the peer worker
	ContractReady    = "c-ready"   // ready, unheld
	ContractBacklog  = "c-backlog" // filed, never specified
	ContractAbove    = "c-above"   // ready, unheld, at the tier ABOVE the squad's agent ceiling (D4.2)
)

// Protected is the surface the fixture's declaration protects, beside
// the declaration's own path.
var Protected = []string{"Makefile", ".github/workflows/", "spec/transitions.json"}

// CeilingBlock is the guardrail FRAGMENT the fixture's declaration
// splices in beside its posture and protected surface (os-0f924157
// D4.2): the core squad's agents ceilinged at trivial — the tier every
// fixture contract is filed at except ContractAbove, which rides
// standard and is refused at the ceiling. The lanes are the same ones
// the declaration's teams block routes to, so the ceiling is declared on
// the lane the contracts actually use. Inner "key": value pairs, not a
// full object: stageCode wraps it in the declaration's own braces.
const CeilingBlock = `"guardrails": {"squads": {"core": {"default": "trivial", "max_agent": "trivial"}}}, "teams": {"squads": [{"name": "core", "lanes": ["dispatch", "supervise", "verdict", "observer", "claim"]}]}`

// Identity is one enrolled actor.
type Identity struct {
	Name string
	Key  ed25519.PrivateKey
	FP   string
}

// Fixture is the deployment.
type Fixture struct {
	Dir, Remote, HookBin                                        string
	Root, Dispatch, Supervise, Verify, Observe, Peer, Adversary *Identity
	// Fence holds the admitted claim positions: the adversary's on
	// ContractHeld and ContractReview, its stale one on
	// ContractReleased, the peer's on ContractPeer.
	Fence map[string]int
	// Submission is the position of the adversary's admitted
	// submission on ContractReview.
	Submission int
	// Active is the protocol version every staged and attacking event
	// carries.
	Active string

	keys  map[string]ed25519.PublicKey
	clock time.Time
}

// New builds the fixture. Only the enforced self-hosted posture builds
// (D6): the drill cannot report green for a posture where the invariant
// does not hold by declaration, so the harness refuses the others.
func New(dir, hookBin string, p posture.Posture) (*Fixture, error) {
	if p != posture.EnforcedSelfHosted {
		return nil, fmt.Errorf("the compromised-actor drill runs against the enforced self-hosted posture only: %q %s", p, postureWhy(p))
	}
	if hookBin == "" {
		return nil, errors.New("no hook binary: the fixture enforces with the production seed-admit, built by the caller")
	}
	f := &Fixture{
		Dir: dir, Remote: filepath.Join(dir, "remote.git"), HookBin: hookBin,
		Fence: map[string]int{}, Active: version.Seed4,
		keys:  map[string]ed25519.PublicKey{},
		clock: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := f.initRemote(); err != nil {
		return nil, err
	}
	if err := f.stageIdentities(); err != nil {
		return nil, err
	}
	if err := f.stageLedger(); err != nil {
		return nil, err
	}
	if err := f.stageCode(); err != nil {
		return nil, err
	}
	return f, nil
}

func postureWhy(p posture.Posture) string {
	switch p {
	case posture.Cooperative:
		return "has no server-side enforcement by declaration (" + posture.Consequence + ")"
	case posture.EnforcedForgeHosted:
		return "is Phase 12 item 2's admission service (" + posture.ForgeHostedGap + ")"
	}
	return "is not a Seed posture"
}

// hardenGitRepo disables the three auto-gc paths on a repository, right
// after creation and before its first object, so a detached gc under a
// t.TempDir cannot outlive the test that made it (the repo-wide fixture
// guard, internal/gitref/fixture_guard_test.go, requires the call by
// name at every creation site).
func hardenGitRepo(repo string) error {
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"},
		{"core.autocrlf", "false"}, // the ledger is LF-only on every platform (spec/platform.md)
		{"core.eol", "lf"}} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			return fmt.Errorf("hardening %s: %v %s", kv[0], err, out)
		}
	}
	return nil
}

func (f *Fixture) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--git-dir", f.Remote}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// initRemote creates the bare remote, hardens it against detached gc,
// pins the default branch, and installs the hook.
func (f *Fixture) initRemote() error {
	if out, err := exec.Command("git", "init", "-q", "--bare", f.Remote).CombinedOutput(); err != nil {
		return fmt.Errorf("bare init: %v %s", err, out)
	}
	if err := hardenGitRepo(f.Remote); err != nil {
		return err
	}
	if out, err := f.git("symbolic-ref", "HEAD", DefaultBranch); err != nil {
		return fmt.Errorf("symbolic-ref: %v %s", err, out)
	}
	shim := "#!/bin/sh\nexec " + f.HookBin + "\n"
	return os.WriteFile(filepath.Join(f.Remote, "hooks", "pre-receive"), []byte(shim), 0o755)
}

func (f *Fixture) identity(name string, seed byte) (*Identity, error) {
	raw := make([]byte, ed25519.SeedSize)
	raw[0] = seed
	raw[1] = 0xad
	key := ed25519.NewKeyFromSeed(raw)
	pub := key.Public().(ed25519.PublicKey)
	fp, err := event.Fingerprint(pub)
	if err != nil {
		return nil, err
	}
	f.keys[fp] = pub
	return &Identity{Name: name, Key: key, FP: fp}, nil
}

func (f *Fixture) stageIdentities() error {
	var err error
	for _, id := range []struct {
		dst  **Identity
		name string
		seed byte
	}{
		{&f.Root, "root", 1}, {&f.Dispatch, "dispatcher", 2}, {&f.Supervise, "supervisor", 3},
		{&f.Verify, "verifier", 4}, {&f.Observe, "observer", 5}, {&f.Peer, "peer", 6}, {&f.Adversary, "adversary", 7},
	} {
		if *id.dst, err = f.identity(id.name, id.seed); err != nil {
			return err
		}
	}
	return nil
}

// Resolver resolves every fixture key: the append-time signature check
// for staged events, and the adversary's LOCAL append of a forged
// event (the hook's replay resolves through the admitted keyring, which
// is what refuses an impersonation).
func (f *Fixture) Resolver() ledger.Resolver {
	return func(fp string) (ed25519.PublicKey, bool) {
		pub, ok := f.keys[fp]
		return pub, ok
	}
}

// TS returns the next timestamp: staging and attacks share one
// monotonic clock, so an event's ts never precedes its predecessor's.
func (f *Fixture) TS() string {
	f.clock = f.clock.Add(time.Minute)
	return f.clock.Format(time.RFC3339)
}

// Append lands one event honestly: through the client seam, validated
// by the same admission the CLI runs, pushed through the hook. Returns
// the admitted position.
func (f *Fixture) Append(id *Identity, v, verb, subject, payload string) (int, error) {
	c, err := gitref.NewClient(filepath.Join(f.Dir, "state-"+id.Name), f.Remote, GuardedRef)
	if err != nil {
		return 0, err
	}
	res, err := c.AppendLoop(gitref.Draft{
		V: v, TS: f.TS(), Actor: id.FP, Verb: verb, Subject: subject, Payload: []byte(payload),
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, id.Key) }, f.Resolver(), admit.Validate(), 3)
	if err != nil {
		return 0, fmt.Errorf("staging %s %s as %s: %w", verb, subject, id.Name, err)
	}
	return res.Position, nil
}

// stageLedger lands the genesis, the upgrades to the active version,
// the enrollments and grants, and the contracts in every state the
// ceiling names.
func (f *Fixture) stageLedger() error {
	// Genesis: the root is the trust anchor; its record is built by the
	// genesis package and pushed through the hook like every other.
	rec, err := genesis.Build(f.Root.Key, nil, f.clock)
	if err != nil {
		return err
	}
	c, err := gitref.NewClient(filepath.Join(f.Dir, "state-genesis"), f.Remote, GuardedRef)
	if err != nil {
		return err
	}
	if _, err := c.AppendLoop(gitref.Draft{
		V: rec.Event.V, TS: rec.Event.TS, Actor: rec.Event.Actor,
		Verb: rec.Event.Verb, Subject: rec.Event.Subject, Payload: rec.Event.Payload,
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, f.Root.Key) }, f.Resolver(), nil, 3); err != nil {
		return fmt.Errorf("genesis: %w", err)
	}
	// The upgrades, one version at a time, each carried at the version
	// active before it.
	from := version.Protocol
	for _, to := range []string{version.Seed1, version.Seed2, version.Seed3, version.Seed4} {
		if _, err := f.Append(f.Root, from, ledger.UpgradeVerb, "system", `{"to": "`+to+`"}`); err != nil {
			return err
		}
		from = to
	}
	// The lanes: each enrolled and granted by the root.
	for _, g := range []struct {
		id  *Identity
		cap string
	}{
		{f.Dispatch, "dispatch"}, {f.Supervise, "supervise"}, {f.Verify, "verdict"},
		{f.Observe, "observer"}, {f.Peer, "claim"}, {f.Adversary, "claim"},
	} {
		pub := hex.EncodeToString(g.id.Key.Public().(ed25519.PublicKey))
		if _, err := f.Append(f.Root, f.Active, "actor.enrolled", g.id.FP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, g.id.Name)); err != nil {
			return err
		}
		if _, err := f.Append(f.Root, f.Active, "actor.granted", g.id.FP, `{"capability": "`+g.cap+`"}`); err != nil {
			return err
		}
	}
	// The contracts: filed and specified by the dispatcher at the
	// trivial tier (prose-only acceptance, so no plan gate and no
	// sealed checks stand between the adversary and the verbs the
	// ceiling is about).
	for _, cid := range []string{ContractHeld, ContractReview, ContractReleased, ContractPeer, ContractReady, ContractBacklog, ContractAbove} {
		// ContractAbove rides standard, the tier ABOVE the fixture squad's
		// trivial agent ceiling (D4.2): ready and unheld, so the adversary
		// can claim it and be refused by the ceiling at the boundary.
		tier := "trivial"
		if cid == ContractAbove {
			tier = "standard"
		}
		if _, err := f.Append(f.Dispatch, f.Active, "intent.filed", cid, fmt.Sprintf(`{"intent": "work on %s", "tier": %q, "budget": "small", "routing": "core"}`, cid, tier)); err != nil {
			return err
		}
		if cid == ContractBacklog {
			continue
		}
		if _, err := f.Append(f.Dispatch, f.Active, "contract.specified", cid, `{"acceptance": {"ref": "ACCEPT.md @ `+Anchor+`", "executable": false}}`); err != nil {
			return err
		}
	}
	// The claims.
	for _, cl := range []struct {
		id  *Identity
		cid string
	}{{f.Adversary, ContractHeld}, {f.Adversary, ContractReview}, {f.Adversary, ContractReleased}, {f.Peer, ContractPeer}} {
		pos, err := f.Append(cl.id, f.Active, "claim.taken", cl.cid, `{}`)
		if err != nil {
			return err
		}
		f.Fence[cl.cid] = pos
	}
	// The adversary submits one contract (review, for the self-approval
	// attacks) and releases another (ready with a stale fence, for the
	// lease attacks).
	pos, err := f.Append(f.Adversary, f.Active, "submission.made", ContractReview, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.Fence[ContractReview], Packet("submitted for verification")))
	if err != nil {
		return err
	}
	f.Submission = pos
	if _, err := f.Append(f.Adversary, f.Active, "claim.released", ContractReleased, fmt.Sprintf(`{"fence": "%d", "packet": %s}`, f.Fence[ContractReleased], Packet("released"))); err != nil {
		return err
	}
	return nil
}

// Packet is a shape-valid four-part handoff packet with the given
// acceptance line and a zero-length base range.
func Packet(acceptance string) string {
	return fmt.Sprintf(`{"acceptance": [%q], "decisions": [], "base": "%s..%s", "refs": [], "findings": []}`, acceptance, Anchor, Anchor)
}

// stageCode commits the declaration on the default branch as the root
// (operator standing), tags the initial revision, and has the peer push
// its own contract branch, so the adversary has a default branch, a tag
// and another actor's branch to attack. The declaration carries the
// guardrail ceiling (CeilingBlock) beside the protected surface, so the
// enforced boundary has a live ceiling to refuse against — the one the
// hook now reads (os-0f924157 D1).
func (f *Fixture) stageCode() error {
	decl := fmt.Sprintf(`{"posture": %q, %s, "protected": [%s]}`, posture.EnforcedSelfHosted, CeilingBlock, quoteAll(Protected))
	files := map[string]string{
		posture.DeclarationPath: decl + "\n",
		"README.md":             "# fixture\n",
		"Makefile":              "check:\n\t@true\n",
		"spec/transitions.json": "{}\n",
	}
	if out, err := PushCode(f.Remote, f.Root.FP, DefaultBranch, false, "", files, f.Dir); err != nil {
		return fmt.Errorf("staging the default branch: %v\n%s", err, out)
	}
	if out, err := PushCode(f.Remote, f.Root.FP, "refs/tags/v0", false, DefaultBranch, nil, f.Dir); err != nil {
		return fmt.Errorf("staging the tag: %v\n%s", err, out)
	}
	if out, err := PushCode(f.Remote, f.Peer.FP, "refs/heads/seed/"+ContractPeer, false, DefaultBranch, map[string]string{"peer.txt": "peer's work\n"}, f.Dir); err != nil {
		return fmt.Errorf("staging the peer's branch: %v\n%s", err, out)
	}
	return nil
}

func quoteAll(ss []string) string {
	var q []string
	for _, s := range ss {
		q = append(q, fmt.Sprintf("%q", s))
	}
	return strings.Join(q, ", ")
}

// PushCode commits files in a fresh working repository (on top of base
// when given, fetched from the remote) and pushes HEAD to ref as
// pusher, the way any credential holder's git does. It returns git's
// combined output and error; the hook's refusal, if any, is in the
// output. An empty pusher asserts no identity.
func PushCode(remote, pusher, ref string, force bool, base string, files map[string]string, scratch string) (string, error) {
	work, err := os.MkdirTemp(scratch, "push-*")
	if err != nil {
		return "", err
	}
	run := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", work}, args...)...).CombinedOutput()
		return string(out), err
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "gc.auto", "0"}, {"config", "gc.autoDetach", "false"},
		{"config", "user.email", "adversary@fixture"}, {"config", "user.name", pusher},
	} {
		if out, err := run(args...); err != nil {
			return out, err
		}
	}
	if base != "" {
		if out, err := run("fetch", "-q", remote, base); err != nil {
			return out, err
		}
		if out, err := run("checkout", "-q", "FETCH_HEAD"); err != nil {
			return out, err
		}
	}
	for name, body := range files {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	if out, err := run("add", "-A"); err != nil {
		return out, err
	}
	if out, err := run("commit", "-q", "--allow-empty", "-m", "push to "+ref); err != nil {
		return out, err
	}
	args := []string{"-C", work, "push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, remote, "HEAD:"+ref)
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), PusherEnv+"="+pusher)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RefTip returns a remote ref's commit, empty when absent.
func (f *Fixture) RefTip(ref string) string {
	out, err := f.git("rev-parse", "--quiet", "--verify", ref)
	if err != nil {
		return ""
	}
	return out
}

// Refs snapshots every ref on the remote: what a refused push must
// leave untouched.
func (f *Fixture) Refs() string {
	out, _ := f.git("for-each-ref", "--format=%(refname) %(objectname)")
	return out
}

// Declaration reads the deployment declaration off the remote's default
// branch tip the way the hook does (os-0f924157 D1): the HEAD symref's
// branch, its tip, and <tip>:seed.json via git show, parsed. The
// one-derivation invariant hands this same *posture.Config to
// admit.ContextOver so the in-process side reads what the hook reads.
func (f *Fixture) Declaration() (*posture.Config, error) {
	branch, err := f.git("symbolic-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	tip, err := f.git("rev-parse", "--quiet", "--verify", branch+"^{commit}")
	if err != nil {
		return nil, nil // unborn default branch: no declaration
	}
	body, err := f.git("show", tip+":"+posture.DeclarationPath)
	if err != nil {
		return nil, nil // the branch carries no declaration file
	}
	return posture.Parse([]byte(body))
}

// Records fetches the guarded ref and returns its verified records.
func (f *Fixture) Records() ([]*event.Record, error) {
	c, err := gitref.NewClient(filepath.Join(f.Dir, "state-reader"), f.Remote, GuardedRef)
	if err != nil {
		return nil, err
	}
	tip, err := c.Fetch()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(f.Dir, "records-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := c.Materialize(tip, dir); err != nil {
		return nil, err
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		return nil, err
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, err
	}
	var records []*event.Record
	if _, err := store.VerifyFromGenesis(resolve, ledger.WithObserver(func(pos int, rec *event.Record) {
		records = append(records, rec)
	})); err != nil {
		return nil, err
	}
	return records, nil
}
