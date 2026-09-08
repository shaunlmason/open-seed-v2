package simulate

// The enforced boundary's ceiling end-to-end (os-0f924157 D4.3, the #323
// reproduction inverting): under the enforced self-hosted posture the
// deployment commits its declaration on the default branch, and a RAW
// above-ceiling claim — one the cooperative client would have self-refused
// before any push — now refuses at the installed hook. The at-ceiling
// control admits at the same boundary. This is the posture's own
// end-to-end story: the hook reads the declaration and the ceiling is live
// at the enforced boundary, not only at the cooperative client.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// realVerbs is a loop.Verbs that lands a real remote append: it reads the
// --key, fetches the guarded ref, reads the version active at the tip, and
// pushes a signed draft through the hook with NO client-side validation
// (validate nil), so build's setup facts pass the boundary the way the
// cooperative posture takes them. The boundary's refusal, if any, is the
// hook's and surfaces as a non-OK result.
type realVerbs struct{}

func (realVerbs) Run(args ...string) loop.Result {
	var remote, state, key, verb, subject, payload string
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			continue
		}
		switch args[i] {
		case "--remote":
			remote = args[i+1]
		case "--state":
			state = args[i+1]
		case "--key":
			key = args[i+1]
		case "--verb":
			verb = args[i+1]
		case "--subject":
			subject = args[i+1]
		case "--payload":
			payload = args[i+1]
		}
	}
	fail := func(code, msg string) loop.Result {
		return loop.Result{Exit: 5, OK: false, Code: code, Message: msg}
	}
	kb, err := os.ReadFile(key)
	if err != nil {
		return fail("usage", err.Error())
	}
	signer, err := event.ParsePrivateKey(kb)
	if err != nil {
		return fail("usage", err.Error())
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return fail("usage", err.Error())
	}
	c, err := gitref.NewClient(filepath.Join(state, "rv"), remote, ledgerRef)
	if err != nil {
		return fail("unavailable", err.Error())
	}
	// The version active at the tip, which only a replay can name, plus
	// the bootstrap resolver the append signs against.
	tip, err := c.Fetch()
	if err != nil {
		return fail("unavailable", err.Error())
	}
	active := version.Protocol
	var resolve ledger.Resolver
	if tip != "" {
		dir, err := os.MkdirTemp("", "rv-active-*")
		if err != nil {
			return fail("unavailable", err.Error())
		}
		defer os.RemoveAll(dir)
		if err := c.Materialize(tip, dir); err != nil {
			return fail("unavailable", err.Error())
		}
		store, err := ledger.Open(dir)
		if err != nil {
			return fail("unavailable", err.Error())
		}
		resolve, _, err = genesis.Bootstrap(store)
		if err != nil {
			return fail("unavailable", err.Error())
		}
		rep, err := store.VerifyFromGenesis(resolve)
		if err != nil {
			return fail("unavailable", err.Error())
		}
		active = rep.ActiveVersion
	}
	res, err := c.AppendLoop(gitref.Draft{
		V: active, TS: time.Now().UTC().Format(time.RFC3339), Actor: fp,
		Verb: verb, Subject: subject, Payload: []byte(payload),
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 3)
	if err != nil {
		return fail("refused", err.Error())
	}
	return loop.Result{Exit: 0, OK: true, Position: fmt.Sprint(res.Position)}
}

// conformance: D4.3 — the enforced boundary refuses a raw above-ceiling
// claim, the at-ceiling control admits at the same boundary.
func TestEnforcedBoundaryRefusesAboveCeilingClaim(t *testing.T) {
	if exec.Command("git", "--version").Run() != nil {
		t.Skip("the enforced hook needs git")
	}
	d, err := build(Config{
		LanesDir: lanesRel,
		Verbs:    realVerbs{},
		WorkDir:  t.TempDir(),
		Now:      time.Now(),
		Enforced: true,
	})
	if err != nil {
		t.Fatalf("build the enforced deployment: %v", err)
	}

	// File one contract above the core squad's trivial ceiling (standard)
	// and one at it (trivial), through the boundary, as the operator.
	anchor := "0123456789abcdef0123456789abcdef01234567"
	for _, st := range []struct{ verb, subject, payload string }{
		{"intent.filed", "c-above", `{"intent": "work on c-above", "tier": "standard", "budget": "small", "routing": "core"}`},
		{"contract.specified", "c-above", `{"acceptance": {"ref": "ACCEPT.md @ ` + anchor + `", "executable": false}}`},
		{"intent.filed", "c-at", `{"intent": "work on c-at", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{"contract.specified", "c-at", `{"acceptance": {"ref": "ACCEPT.md @ ` + anchor + `", "executable": false}}`},
	} {
		if err := d.append1(d.opKey, st.verb, st.subject, st.payload); err != nil {
			t.Fatalf("staging %s %s: %v", st.verb, st.subject, err)
		}
	}

	// The implementer is the deployment's agent lane with a claim grant,
	// ceilinged at trivial by the declaration on the default branch.
	implKeyPath, ok := d.keys["implementer"]
	if !ok {
		t.Fatal("the deployment has no implementer lane to claim with")
	}
	kb, err := os.ReadFile(implKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	impl, err := event.ParsePrivateKey(kb)
	if err != nil {
		t.Fatal(err)
	}
	implFP, err := event.Fingerprint(impl.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}

	// A raw claim: signed by the implementer, pushed with no client-side
	// validation, so only the hook judges it.
	rawClaim := func(subject string) error {
		c, err := gitref.NewClient(filepath.Join(d.dir, "raw"), d.remote, ledgerRef)
		if err != nil {
			return err
		}
		tip, err := c.Fetch()
		if err != nil {
			return err
		}
		active := version.Seed1
		var resolve ledger.Resolver
		if tip != "" {
			dir, err := os.MkdirTemp("", "raw-active-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			if err := c.Materialize(tip, dir); err != nil {
				return err
			}
			store, err := ledger.Open(dir)
			if err != nil {
				return err
			}
			resolve, _, err = genesis.Bootstrap(store)
			if err != nil {
				return err
			}
			if rep, err := store.VerifyFromGenesis(resolve); err == nil {
				active = rep.ActiveVersion
			}
		}
		_, err = c.AppendLoop(gitref.Draft{
			V: active, TS: time.Now().UTC().Format(time.RFC3339), Actor: implFP,
			Verb: "claim.taken", Subject: subject, Payload: []byte(`{}`),
		}, func(e event.Event) (*event.Record, error) { return event.Sign(e, impl) }, resolve, nil, 3)
		return err
	}

	// The raw above-ceiling claim refuses at the hook, the ref unmoved.
	before := guardedTip(d.remote)
	if err := rawClaim("c-above"); err == nil {
		t.Fatal("the raw above-ceiling claim admitted at the enforced boundary: the hook reads no declaration")
	}
	if after := guardedTip(d.remote); after != before {
		t.Fatalf("the refused claim moved the guarded ref: %s -> %s", before, after)
	}

	// The at-ceiling control admits at the same boundary: the declaration
	// is read, not a no-op, and it does not over-refuse.
	if err := rawClaim("c-at"); err != nil {
		t.Fatalf("the at-ceiling control admits at the enforced boundary: %v", err)
	}
}

// guardedTip returns a remote's guarded-ref commit, empty when absent.
func guardedTip(remote string) string {
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "--quiet", "--verify", ledgerRef).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
