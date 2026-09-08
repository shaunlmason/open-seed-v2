package main

// The Phase 2 admission drills (plans/os-028dda91.md; conformance
// III.B): the raw-git adversary run against both declared postures, and
// kill-and-replace proving the hook host holds no state worth keeping.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// deployment is a bare remote standing at a declared posture.
type deployment struct {
	remote  string
	posture posture.Posture
}

// newDeployment declares the posture in the production format and reads
// it back through internal/posture to decide enforcement, so fixtures
// and production share one declaration (the "both postures selectable
// in fixtures" exit criterion).
func newDeployment(t *testing.T, p posture.Posture) deployment {
	t.Helper()
	host := t.TempDir()
	remote := filepath.Join(host, "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare init: %v %s", err, out)
	}
	hardenGitRepo(t, remote)
	cfgPath := filepath.Join(host, "seed.json")
	if err := os.WriteFile(cfgPath, []byte(`{"posture": "`+string(p)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := posture.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Posture == posture.EnforcedSelfHosted {
		installHook(t, remote)
	}
	return deployment{remote: remote, posture: cfg.Posture}
}

func installHook(t *testing.T, remote string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server; a bare Windows checkout runs the cooperative or forge-hosted posture (spec/platform.md)")
	}
	shim := "#!/bin/sh\nexec " + hookBin + "\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
}

// conformance: III.B — the raw-git adversary drill, per posture. Under
// enforced, every invalid raw push refuses and the ref never moves.
// Under cooperative the same push lands: the charter's named consequence
// (posture.Consequence) made observable, while the cooperative client
// itself still refuses the same content locally.
func TestDrillRawAdversaryPerPosture(t *testing.T) {
	hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`

	for _, p := range []posture.Posture{posture.EnforcedSelfHosted, posture.Cooperative} {
		t.Run(string(p), func(t *testing.T) {
			d := newDeployment(t, p)
			resolve := seedGenesis(t, d.remote)
			before := remoteTip(t, d.remote)

			err := craftPush(t, d.remote, resolve, func(dir string, store *ledger.Store) {
				appendRaw(t, store, resolve, signed(t, "message.sent", "c-0001", hostile, tipOf(t, store)))
			})
			after := remoteTip(t, d.remote)
			if d.posture.Enforced() {
				if !errors.Is(err, gitref.ErrRemoteRejected) || after != before {
					t.Fatalf("enforced posture must refuse the raw adversary with the ref unmoved, got %v (tip %s -> %s)", err, before, after)
				}
			} else {
				if err != nil || after == before {
					t.Fatalf("under cooperative the raw push lands — %q — got %v (tip unchanged)", posture.Consequence, err)
				}
			}

			// The cooperating client refuses the same content locally,
			// whichever posture the remote runs.
			priv := fixtureKey(t)
			fp, ferr := event.Fingerprint(priv.Public().(ed25519.PublicKey))
			if ferr != nil {
				t.Fatal(ferr)
			}
			c, cerr := gitref.NewClient(t.TempDir(), d.remote, guardedRef)
			if cerr != nil {
				t.Fatal(cerr)
			}
			_, err = c.AppendLoop(gitref.Draft{
				V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
				Verb: "message.sent", Subject: "c-0002", Payload: json.RawMessage(hostile),
			}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, admit.Validate(), 3)
			var cls *admit.ClassificationError
			if !errors.As(err, &cls) {
				t.Fatalf("the cooperative client must self-refuse the hostile draft, got %v", err)
			}
		})
	}
}

// conformance: III.B statelessness — kill-and-replace: destroy the hook
// host, rebuild it from a fresh bare clone of the guarded repository,
// install a freshly built hook, and every decision comes out the same:
// the same refusals, an admitted valid append, and a green from-genesis
// verification.
func TestDrillKillAndReplace(t *testing.T) {
	d := newDeployment(t, posture.EnforcedSelfHosted)
	resolve := seedGenesis(t, d.remote)
	priv := fixtureKey(t)
	fp, _ := event.Fingerprint(priv.Public().(ed25519.PublicKey))

	appendValid := func(remote, subject string) error {
		c, err := gitref.NewClient(t.TempDir(), remote, guardedRef)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.AppendLoop(gitref.Draft{
			V: "seed/0", TS: "2026-09-01T03:00:00Z", Actor: fp,
			Verb: "message.sent", Subject: subject, Payload: json.RawMessage(`{"n": 1}`),
		}, func(e event.Event) (*event.Record, error) { return event.Sign(e, priv) }, resolve, admit.Validate(), 3)
		return err
	}
	if err := appendValid(d.remote, "c-0001"); err != nil {
		t.Fatalf("original host must admit a valid append: %v", err)
	}

	refusals := func(remote string) map[string]string {
		hostile := `{"transcript": "` + strings.Repeat("all work and no play ", 40) + `"}`
		got := map[string]string{}
		for name, mutate := range map[string]func(dir string, store *ledger.Store){
			"classification": func(dir string, store *ledger.Store) {
				appendRaw(t, store, resolve, signed(t, "message.sent", "c-0009", hostile, tipOf(t, store)))
			},
			"halted": func(dir string, store *ledger.Store) {
				appendRaw(t, store, resolve, signed(t, "system.halt.declared", "system", `{"reason": "drill"}`, tipOf(t, store)))
				appendRaw(t, store, resolve, signed(t, "message.sent", "c-0009", `{"n": 9}`, tipOf(t, store)))
			},
			"verify": func(dir string, store *ledger.Store) {
				appendRaw(t, store, resolve, signedV(t, "seed/9", "message.sent", "c-0009", `{"n": 9}`, tipOf(t, store)))
			},
		} {
			before := remoteTip(t, remote)
			err := craftPush(t, remote, resolve, mutate)
			if !errors.Is(err, gitref.ErrRemoteRejected) || remoteTip(t, remote) != before {
				t.Fatalf("%s must refuse on %s, got %v", name, remote, err)
			}
			got[name] = ruleLine(err.Error())
		}
		return got
	}
	originalDecisions := refusals(d.remote)

	// Kill the host: the replacement is a fresh mirror clone, so only
	// replicated Git data (refs and objects) crosses over — no repo-local
	// config, hooks, or cached validator state — plus a freshly installed
	// hook, nothing else. A mirror clone is what replicates the guarded
	// ref: a plain bare clone maps refs/heads/* only and would miss it.
	replacement := filepath.Join(t.TempDir(), "replacement.git")
	if out, err := exec.Command("git", "clone", "-q", "--mirror", d.remote, replacement).CombinedOutput(); err != nil {
		t.Fatalf("replace host: %v %s", err, out)
	}
	hardenGitRepo(t, replacement)
	installHook(t, replacement)

	replacementDecisions := refusals(replacement)
	for name, want := range originalDecisions {
		if replacementDecisions[name] != want {
			t.Fatalf("%s decision drifted across kill-and-replace: %q vs %q", name, want, replacementDecisions[name])
		}
	}
	if err := appendValid(replacement, "c-0002"); err != nil {
		t.Fatalf("replacement host must admit a valid append: %v", err)
	}
	c, err := gitref.NewClient(t.TempDir(), replacement, guardedRef)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil || rep.Count != 3 {
		t.Fatalf("replacement chain must verify from genesis: %+v %v", rep, err)
	}
}

// ruleLine extracts the hook's refusal line ("seed-admit: ...") from the
// porcelain-wrapped push error, the part that must not drift across a
// host replacement.
func ruleLine(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		if i := strings.Index(line, "seed-admit:"); i >= 0 {
			return strings.TrimSpace(line[i:])
		}
	}
	return msg
}
