// Command seed is the Seed CLI skeleton (docs/build-plan.md Phase 0):
// argument parsing, envelope emission, and exit codes, mirroring the
// engine's cmd/seed seam so later verbs land as cases, not rework. During
// incubation the binary is built to next/bin/ and invoked explicitly;
// scripts/seed (v1) remains the only coordination entry point until
// spin-out.
package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches one verb and renders exactly one envelope on stdout,
// returning the process exit code. The envelope carries the same code, so
// machine callers can branch without inspecting wait status.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "missing verb — try 'seed version'"), stdout, stderr)
	}
	// Dispatch is the registry (plans/os-b55e5647.md D2): the same
	// table the machine protocol serves, so the two surfaces cannot
	// disagree about which verbs exist.
	if g, ok := catalog(os.Stdin).Group(args[0]); ok {
		return g.Run(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("unknown verb %q — try 'seed version'", args[0])), stdout, stderr)
}

type repeatedFlag []string

func (r *repeatedFlag) String() string     { return fmt.Sprint([]string(*r)) }
func (r *repeatedFlag) Set(v string) error { *r = append(*r, v); return nil }

// runInit writes the signed genesis into an empty ledger: the operator's
// key signs and always joins the governance root; extra --operator public
// keys widen it. A non-empty ledger refuses with exit 3 (invalid
// transition) and the machine code ledger_not_empty.
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory (created if absent)")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the initializing operator")
	var operators repeatedFlag
	fs.Var(&operators, "operator", "OpenSSH ed25519 public key of an additional governance-root operator (repeatable)")
	preseed := fs.String("preseed", "", "deployment declaration to bootstrap from, idempotently (plans/os-0d4f2af3.md)")
	lanesDir := fs.String("lanes", "lanes", "lane manifests the preseed's teams are checked against (empty to skip)")
	if err := fs.Parse(args); err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	if *dir == "" || *keyPath == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "init requires --ledger <dir> and --key <openssh-ed25519-private> [--preseed <file>]"), stdout, stderr)
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err)), stdout, stderr)
	}
	var extras []ed25519.PublicKey
	for _, p := range operators {
		b, err := os.ReadFile(p)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --operator %s: %v", p, err)), stdout, stderr)
		}
		pub, err := event.ParsePublicKey(b)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--operator %s: %v", p, err)), stdout, stderr)
		}
		extras = append(extras, pub)
	}
	store, err := ledger.Open(*dir)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot open ledger dir: %v", err)), stdout, stderr)
	}
	if *preseed != "" {
		// The preseed path: genesis when empty, then the declared
		// protocol's activations, idempotently; a second run appends
		// nothing, and a chain the file contradicts refuses.
		cfg, failEnv := loadDeclarationFor(*preseed)
		if failEnv != nil {
			return render(failEnv, stdout, stderr)
		}
		if err := lintPreseed(cfg, *lanesDir); err != nil {
			return render(preseedFailEnvelope(err), stdout, stderr)
		}
		appended, err := applyPreseed(store, cfg, signer, extras, time.Now())
		if err != nil {
			env := preseedFailEnvelope(err)
			if _, count, terr := store.Tip(); terr == nil && count > 0 {
				env = stampTip(env, count)
			}
			return render(env, stdout, stderr)
		}
		_, count, err := store.Tip()
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
		env := envelope.OK(map[string]any{
			"preseed":   *preseed,
			"appended":  appended,
			"unchanged": len(appended) == 0,
			"protocol":  cfg.Protocol,
		})
		return render(stampTip(env, count), stdout, stderr)
	}
	rec, err := genesis.Init(store, signer, extras, time.Now())
	if errors.Is(err, ledger.ErrNotEmpty) {
		env := envelope.Fail(envelope.ExitInvalidTransition, "ledger_not_empty", err.Error())
		// The refusal is computed from ledger state, so it stamps the
		// position it observed (#83 review finding).
		if _, count, terr := store.Tip(); terr == nil && count > 0 {
			pos := fmt.Sprintf("%d", count-1)
			env.Position = &pos
		}
		return render(env, stdout, stderr)
	}
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	hash, err := rec.Event.Hash()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	payload, err := genesis.Parse(rec)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	roots := make([]string, 0, len(payload.GovernanceRoot))
	for _, rk := range payload.GovernanceRoot {
		roots = append(roots, rk.Fingerprint)
	}
	env := envelope.OK(map[string]any{
		"genesis":         hash,
		"protocol":        payload.Protocol,
		"governance_root": roots,
	})
	pos := "0"
	env.Position = &pos
	return render(env, stdout, stderr)
}

// render writes the envelope; a render failure is the one condition that
// cannot itself be an envelope, so it reports on stderr and exits 1.
func render(e *envelope.Envelope, stdout, stderr io.Writer) int {
	if err := e.Render(stdout); err != nil {
		fmt.Fprintf(stderr, "seed: envelope render failed: %v\n", err)
		return 1
	}
	return e.Exit
}
