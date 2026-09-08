// Package simulate runs the whole system end to end against synthetic
// intents with a mock executor and zero credentials (SEED-NEXT.md
// §II.16; plans/os-16e55c11.md D3): a throwaway deployment under the
// declared posture, one identity per shipped lane and role, a seeded
// catalog of fix-the-check intents, and every lane driven through the
// real boundary — the loop lanes through internal/loop, the rest
// through the same CLI verbs an operator runs. No forge, no model, no
// network beyond a local bare git remote.
package simulate

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const ledgerRef = "refs/seed/ledger"

// deployment is a provisioned simulation: a bare remote seeded to
// seed/1 with one enrolled, granted identity per shipped lane and role.
type deployment struct {
	dir      string
	remote   string
	state    string
	config   string // the deployment's declaration (seed.json), read by every verb that takes --config
	lanesDir string
	verbs    loop.Verbs
	opPriv   ed25519.PrivateKey
	opKey    string
	keys     map[string]string // lane/role name -> private key path
	fps      map[string]string // lane/role name -> fingerprint
	grants   map[string][]string
	manifest map[string]lane.Manifest
	now      time.Time
}

// keyAt writes a deterministic ed25519 key in the SSH private-key form
// the CLI's --key reads, returning its path, public hex and fingerprint.
func keyAt(dir, name string, seed byte) (path, pubHex, fp string, err error) {
	s := make([]byte, ed25519.SeedSize)
	s[0] = seed
	k := ed25519.NewKeyFromSeed(s)
	block, err := ssh.MarshalPrivateKey(k, name)
	if err != nil {
		return "", "", "", err
	}
	path = filepath.Join(dir, name+"_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", "", "", err
	}
	pub := k.Public().(ed25519.PublicKey)
	f, err := event.Fingerprint(pub)
	if err != nil {
		return "", "", "", err
	}
	return path, hex.EncodeToString(pub), f, nil
}

// append1 lands one signed event through the CLI seam, returning an
// error carrying the boundary's refusal when it does not admit.
func (d *deployment) append1(keyPath, verb, subject, payload string) error {
	res := d.verbs.Run("ledger", "append", "--remote", d.remote, "--state", d.state, "--config", d.config,
		"--key", keyPath, "--verb", verb, "--subject", subject, "--payload", payload)
	if res.Exit != 0 || !res.OK {
		return fmt.Errorf("append %s %s refused: exit %d code %q: %s", verb, subject, res.Exit, res.Code, res.Message)
	}
	return nil
}

// build provisions the deployment: a hardened bare remote, genesis by
// the operator key, an upgrade to seed/1, and one enrolled+granted
// identity per shipped lane and role, the grants read from the
// manifests (never a list this package keeps).
func build(cfg Config) (*deployment, error) {
	dir, err := os.MkdirTemp(cfg.workRoot(), "seed-sim-")
	if err != nil {
		return nil, err
	}
	d := &deployment{
		dir:      dir,
		state:    filepath.Join(dir, "state"),
		lanesDir: cfg.LanesDir,
		verbs:    cfg.Verbs,
		keys:     map[string]string{},
		fps:      map[string]string{},
		grants:   map[string][]string{},
		manifest: map[string]lane.Manifest{},
		now:      cfg.now(),
	}
	ms, err := lane.Load(cfg.LanesDir)
	if err != nil {
		return nil, fmt.Errorf("load manifests: %w", err)
	}
	for _, m := range ms {
		d.manifest[m.Lane] = m
	}
	// The deployment declares its guardrails (plans/os-b5051f2e.md D5):
	// the core squad's agents are ceilinged at the tier the catalog
	// files at, so the ceiling rule is live at every claim the run
	// admits and the end-of-run audit judges under the same file. The
	// lanes are the shipped manifests, so the declaration is one
	// seed preseed check would take.
	d.config = filepath.Join(dir, "seed.json")
	lanes := make([]string, 0, len(ms))
	for _, m := range ms {
		lanes = append(lanes, m.Lane)
	}
	sort.Strings(lanes)
	decl, err := json.Marshal(map[string]any{
		"posture":    postureName(cfg.Enforced),
		"guardrails": map[string]any{"squads": map[string]any{"core": map[string]any{"default": "trivial", "max_agent": "trivial"}}},
		"teams":      map[string]any{"squads": []map[string]any{{"name": "core", "lanes": lanes}}},
	})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(d.config, decl, 0o644); err != nil {
		return nil, fmt.Errorf("write declaration: %w", err)
	}

	// The operator key is the genesis governance root.
	keydir := filepath.Join(dir, "keys")
	if err := os.MkdirAll(keydir, 0o755); err != nil {
		return nil, err
	}
	opPath, _, _, err := keyAt(keydir, "operator", 1)
	if err != nil {
		return nil, err
	}
	d.opKey = opPath
	s := make([]byte, ed25519.SeedSize)
	s[0] = 1
	d.opPriv = ed25519.NewKeyFromSeed(s)

	// The bare remote and its genesis.
	d.remote = filepath.Join(dir, "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", d.remote).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("bare init: %v: %s", err, out)
	}
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"}} {
		if out, err := exec.Command("git", "-C", d.remote, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("harden %s: %v: %s", kv[0], err, out)
		}
	}
	if cfg.Enforced {
		if err := installHook(d.remote, filepath.Dir(cfg.LanesDir)); err != nil {
			return nil, fmt.Errorf("install enforced hook: %w", err)
		}
	}
	if err := d.seedGenesis(); err != nil {
		return nil, err
	}

	// The declaration is deployment state whose canonical home is the
	// default branch's tip (postures.md): the hook reads it there, not
	// from the file beside the remote the cooperative client takes via
	// --config. Commit it as the operator (the governance root) so the
	// enforced boundary has the same ceiling to refuse against that the
	// cooperative half runs (os-0f924157 D4.3).
	if err := d.commitDeclaration(); err != nil {
		return nil, fmt.Errorf("commit the declaration: %w", err)
	}

	// Upgrade to seed/1 so the lifecycle verbs admit.
	if err := d.append1(d.opKey, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`); err != nil {
		return nil, err
	}

	// One identity per shipped lane and role; grants are the manifest's.
	seed := byte(10)
	for _, m := range ms {
		path, pub, fp, err := keyAt(keydir, m.Lane, seed)
		seed++
		if err != nil {
			return nil, err
		}
		d.keys[m.Lane], d.fps[m.Lane], d.grants[m.Lane] = path, fp, m.Grants
		if err := d.append1(d.opKey, keyring.VerbEnrolled, fp,
			fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pub, m.Lane)); err != nil {
			return nil, err
		}
		for _, g := range m.Grants {
			if err := d.append1(d.opKey, keyring.VerbGranted, fp, `{"capability": "`+g+`"}`); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// seedGenesis lands the operator-signed genesis on the remote through
// the gitref append loop — the same round-trip the CLI's --remote path
// runs, so the deployment is credential-free and network-free.
func (d *deployment) seedGenesis() error {
	c, err := gitref.NewClient(filepath.Join(d.dir, "genesis-work"), d.remote, ledgerRef)
	if err != nil {
		return err
	}
	rec, err := genesis.Build(d.opPriv, nil, d.now)
	if err != nil {
		return err
	}
	payload, err := genesis.Parse(rec)
	if err != nil {
		return err
	}
	resolve, err := payload.Resolver(rec.Event.Actor)
	if err != nil {
		return err
	}
	res, err := c.AppendLoop(gitref.Draft{
		V: rec.Event.V, TS: rec.Event.TS, Actor: rec.Event.Actor,
		Verb: rec.Event.Verb, Subject: rec.Event.Subject, Payload: rec.Event.Payload,
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, d.opPriv) }, resolve, nil, 3)
	if err != nil {
		return err
	}
	if res.Position != 0 {
		return fmt.Errorf("genesis landed at position %d, not 0", res.Position)
	}
	_ = ledger.Resolver(resolve)
	return nil
}

// commitDeclaration lands the deployment's seed.json on the default
// branch as the operator (the governance root, which the code half
// admits without the protected-surface check) so the enforced hook has
// the declaration to read at the default branch's tip — the same file
// the cooperative client takes via --config, now in its canonical home.
func (d *deployment) commitDeclaration() error {
	pub := d.opPriv.Public().(ed25519.PublicKey)
	opFP, err := event.Fingerprint(pub)
	if err != nil {
		return err
	}
	branchOut, err := exec.Command("git", "--git-dir", d.remote, "symbolic-ref", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("read the default branch: %w", err)
	}
	branch := strings.TrimSpace(string(branchOut))

	work := filepath.Join(d.dir, "decl-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	git := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v: %s", args, err, out)
		}
		return nil
	}
	if err := git("init", "-q"); err != nil {
		return err
	}
	if err := git("config", "user.email", "op@sim"); err != nil {
		return err
	}
	if err := git("config", "user.name", "operator"); err != nil {
		return err
	}
	body, err := os.ReadFile(d.config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, posture.DeclarationPath), body, 0o644); err != nil {
		return err
	}
	if err := git("add", "-A"); err != nil {
		return err
	}
	if err := git("commit", "-q", "--allow-empty", "-m", "the deployment declaration"); err != nil {
		return err
	}
	push := exec.Command("git", "-C", work, "push", "-q", d.remote, "HEAD:"+branch)
	push.Env = append(os.Environ(), "SEED_PUSHER="+opFP)
	if out, err := push.CombinedOutput(); err != nil {
		return fmt.Errorf("commit the declaration: %v: %s", err, out)
	}
	return nil
}
