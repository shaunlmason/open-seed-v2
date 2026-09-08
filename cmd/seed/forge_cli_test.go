package main

// The forge-hosted posture at the terminal (plans/os-5c8a312c.md D3,
// D8): with seed.json declaring enforced-forge-hosted, every remote
// verb proposes to the admission service instead of pushing, the
// declaration's branch is the ledger ref, the actor's own credential
// cannot write that branch, and the other postures see no change.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/propose"
)

const forgeIdentity = "seed-admission[bot]"

var (
	admitBinOnce sync.Once
	admitBinPath string
	admitBinErr  error
)

// admitServiceBinary builds seed-admit once per test binary: the
// service is the OTHER binary, and the drill drives it as a process
// the way a deployment would.
func admitServiceBinary(t *testing.T) string {
	t.Helper()
	admitBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "seed-admit-bin-*")
		if err != nil {
			admitBinErr = err
			return
		}
		admitBinPath = filepath.Join(dir, "seed-admit"+exeSuffix())
		if out, err := exec.Command("go", "build", "-o", admitBinPath, "../seed-admit").CombinedOutput(); err != nil {
			admitBinErr = errors.New(string(out))
		}
	})
	if admitBinErr != nil {
		t.Fatalf("building seed-admit: %v", admitBinErr)
	}
	return admitBinPath
}

// forgeRemote models a forge: a bare repository with no admission hook
// whose pre-receive lets the admission identity alone write the ledger
// branch (the ruleset the reconciler declares), asserted through
// SEED_PUSHER because a local path has no other notion of who pushes.
func forgeRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "forge.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("bare init: %v %s", err, out)
	}
	hardenGitRepo(t, dir)
	ruleset := "#!/bin/sh\nwhile read old new ref; do\n  if [ \"$ref\" = \"" + posture.DefaultLedgerRef + "\" ] && [ \"$SEED_PUSHER\" != \"" + forgeIdentity + "\" ]; then\n    echo \"ruleset: " + posture.DefaultLedgerRef + " is writable by " + forgeIdentity + " alone\" >&2; exit 1\n  fi\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "hooks", "pre-receive"), []byte(ruleset), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startForgeService runs seed-admit serve over the remote under the
// admission identity and returns its endpoint.
func startForgeService(t *testing.T, remote string) string {
	t.Helper()
	announce := filepath.Join(t.TempDir(), "endpoint")
	cmd := exec.Command(admitServiceBinary(t), "serve", "--remote", remote, "--announce", announce, "--state", t.TempDir())
	cmd.Env = append(os.Environ(), "SEED_PUSHER="+forgeIdentity)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for i := 0; i < 250; i++ {
		if b, err := os.ReadFile(announce); err == nil && strings.HasPrefix(string(b), "http://") {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the service never announced its endpoint")
	return ""
}

// seedForgeGenesis proposes genesis through the service: an empty
// ledger admits a genesis proposal only, and nobody but the service
// can write the branch.
func seedForgeGenesis(t *testing.T, endpoint string) ledger.Resolver {
	t.Helper()
	priv := fixturePriv(t)
	rec, err := genesis.Build(priv, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(rec.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := propose.New(endpoint).Propose(posture.DefaultLedgerRef, []*event.Record{rec}); err != nil || res.Position != 0 {
		t.Fatalf("genesis through the service: %+v %v", res, err)
	}
	return resolve
}

func forgeTip(t *testing.T, remote string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "--quiet", "--verify", posture.DefaultLedgerRef).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeDeclaration(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seed.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// conformance: III.B — under the forge-hosted declaration the remote
// verbs propose instead of push: the append lands through the service
// on the declaration's branch, the refusal a self-validating client
// meets carries the boundary's code, the actor's credential cannot
// write the branch directly, and a dead endpoint is unavailable by
// name rather than a silent fallback to pushing.
func TestForgeHostedRemoteVerbsPropose(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := forgeRemote(t)
	endpoint := startForgeService(t, remote)
	resolve := seedForgeGenesis(t, endpoint)
	cfg := writeDeclaration(t, `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "`+endpoint+`", "identity": "`+forgeIdentity+`", "checks": ["check"], "reviews": 1, "owners": ["@root"]}}`)

	before := forgeTip(t, remote)
	e, code := runEnv(t, "ledger", "append", "--remote", remote, "--config", cfg, "--state", filepath.Join(dir, "state"),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "1" {
		t.Fatalf("the append must land through the service: %d %+v", code, e)
	}
	if e.Result["commit"] != forgeTip(t, remote) || forgeTip(t, remote) == before {
		t.Fatalf("the service's commit must be the branch tip: %+v vs %s", e.Result, forgeTip(t, remote))
	}

	// The read surfaces follow the declaration's branch without --ref.
	e, code = runEnv(t, "situation", "--remote", remote, "--config", cfg, "--state", t.TempDir(), "--key", priv)
	if code != 0 || !e.OK || e.Position == nil || *e.Position != "1" {
		t.Fatalf("the situation read must orient on the declaration's branch: %d %+v", code, e)
	}

	// A hostile draft: the self-validating client refuses with the
	// boundary's code, and had it skipped that, the service would have
	// answered with the same one (cmd/seed-admit's one-derivation drill).
	after := forgeTip(t, remote)
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
	e, code = runEnv(t, "ledger", "append", "--remote", remote, "--config", cfg, "--state", t.TempDir(),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0002", "--payload", hostile)
	if code != 9 || e.Error == nil || e.Error.Code != "classification_refused" || forgeTip(t, remote) != after {
		t.Fatalf("a hostile draft refuses at 9 with the branch unmoved, got %d %+v", code, e)
	}

	// The actor's own credential cannot write the branch: a raw push
	// under this process (no admission identity) is refused by the
	// forge's rule. The service, not the actor, is the writer.
	c, err := gitref.NewClient(t.TempDir(), remote, posture.DefaultLedgerRef)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := event.Fingerprint(fixturePriv(t).Public().(ed25519.PublicKey))
	_, err = c.AppendLoop(gitref.Draft{
		V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
		Verb: "message.sent", Subject: "c-0003", Payload: json.RawMessage(`{"n": 3}`),
	}, func(ev event.Event) (*event.Record, error) { return event.Sign(ev, fixturePriv(t)) }, resolve, nil, 3)
	if !errors.Is(err, gitref.ErrRemoteRejected) || forgeTip(t, remote) != after {
		t.Fatalf("the actor's direct push must be refused by the forge's rule, got %v", err)
	}

	// A declaration naming a service nobody runs: unavailable, named.
	dead := writeDeclaration(t, `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://127.0.0.1:1", "identity": "`+forgeIdentity+`"}}`)
	e, code = runEnv(t, "ledger", "append", "--remote", remote, "--config", dead, "--state", t.TempDir(),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0004", "--payload", `{"n": 4}`)
	if code != 5 || e.Error == nil || e.Error.Code != "unavailable" || !strings.Contains(e.Error.Message, "127.0.0.1:1") || forgeTip(t, remote) != after {
		t.Fatalf("a dead endpoint is unavailable by name, never a push: %d %+v", code, e)
	}

	// A declaration that does not parse refuses before any transport.
	broken := writeDeclaration(t, `{"posture": "enforced-forge-hosted"}`)
	e, code = runEnv(t, "ledger", "append", "--remote", remote, "--config", broken, "--state", t.TempDir(),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0004", "--payload", `{"n": 4}`)
	if code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" {
		t.Fatalf("an invalid declaration refuses at 13, got %d %+v", code, e)
	}
	e, code = runEnv(t, "ledger", "append", "--remote", remote, "--config", filepath.Join(t.TempDir(), "absent.json"), "--state", t.TempDir(),
		"--key", priv, "--verb", "message.sent", "--subject", "c-0004", "--payload", `{"n": 4}`)
	if code != 4 || e.Error == nil || e.Error.Code != "posture_undeclared" {
		t.Fatalf("an explicitly named absent declaration refuses at 4, got %d %+v", code, e)
	}
}

// The other postures see no change from the declaration: a cooperative
// or self-hosted deployment's remote append pushes as before, and no
// declaration at all is today's behavior byte for byte.
func TestOtherPosturesIgnoreTheAdmissionBlock(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	for _, p := range []string{"cooperative", "enforced-self-hosted"} {
		remote := bareRemote(t)
		seedRemoteGenesis(t, remote)
		cfg := writeDeclaration(t, `{"posture": "`+p+`"}`)
		e, code := runEnv(t, "ledger", "append", "--remote", remote, "--config", cfg, "--state", filepath.Join(dir, "state-"+p),
			"--key", priv, "--verb", "message.sent", "--subject", "c-0001", "--payload", `{"n": 1}`)
		if code != 0 || !e.OK || *e.Position != "1" || e.Result["commit"] != remoteTip(t, remote) {
			t.Fatalf("%s: the append must push as before: %d %+v", p, code, e)
		}
	}
}

// exeSuffix is the platform's executable suffix: a built binary
// without it is not runnable on Windows (spec/platform.md).
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
