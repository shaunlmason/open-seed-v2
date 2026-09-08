package main

// The read surfaces in the remote posture (plans/os-abb206c8.md D3).
// Before this, `offer list` and `situation` bound --ledger alone while
// `claim take` refused it, so in the only posture where a lane can
// claim it could neither poll nor orient. These drills are the proof
// that the loop's three steps now run against ONE view.

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// signerFingerprint is the acting key's fingerprint, read from the same
// private key the CLI signs with, so a drill cannot poll as one actor
// and act as another.
func signerFingerprint(t *testing.T, privPath string) string {
	t.Helper()
	b, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	k, err := event.ParsePrivateKey(b)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// conformance: promotion criterion 1 — a lane polls, orients and claims
// in one posture. The assertion that matters is the LAST one: a claim
// that only the remote round-trip can take is visible, with its fence,
// to the orienting read in the same posture.
func TestReadsFollowTheClaimIntoTheRemotePosture(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "seed/1"}`)
	state := filepath.Join(dir, "state")
	fp := signerFingerprint(t, priv)

	appendCLI := func(verb, subject, payload string) (ledgerEnv, int) {
		return runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
			"--key", priv, "--verb", verb, "--subject", subject, "--payload", payload)
	}
	if _, code := appendCLI("intent.filed", "c-1",
		`{"intent": "fix", "tier": "trivial", "budget": "small", "routing": "core"}`); code != 0 {
		t.Fatal("filing failed")
	}
	if _, code := appendCLI("contract.specified", "c-1",
		`{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`); code != 0 {
		t.Fatal("specification failed")
	}

	// Poll, in the posture the claim will land in.
	e, code := runEnv(t, "offer", "list", "--remote", remote, "--state", state, "--actor", fp)
	if code != 0 || !e.OK {
		t.Fatalf("offer list must run against a remote ledger: %d %+v", code, e)
	}

	// Orient, keyless and keyed, before the claim.
	e, code = runEnv(t, "situation", "--remote", remote, "--state", state)
	if code != 0 || !e.OK {
		t.Fatalf("situation must run against a remote ledger: %d %+v", code, e)
	}
	if e.Position == nil {
		t.Fatal("a remote read carries a position stamp: that IS what makes it an orienting read")
	}
	before := *e.Position

	// Claim: remote-only, because only the push round-trip orders two
	// rivals. This is the act the reads previously could not follow.
	if _, code := appendCLI("claim.taken", "c-1", `{}`); code != 0 {
		t.Fatal("claim failed")
	}

	e, code = runEnv(t, "situation", "--remote", remote, "--state", state,
		"--key", priv, "--subject", "c-1")
	if code != 0 || !e.OK {
		t.Fatalf("situation after the claim: %d %+v", code, e)
	}
	if *e.Position == before {
		t.Fatalf("the claim advanced the ledger, so the read's position must advance too: still %s", before)
	}
	windows, _ := e.Result["windows"].([]any)
	if len(windows) != 1 {
		t.Fatalf("the holder's own window must appear in its orienting read: %+v", e.Result)
	}
	w, _ := windows[0].(map[string]any)
	if w["subject"] != "c-1" || w["fence"] == "" || w["fence"] == nil {
		t.Fatalf("the window carries the fence every holder-signed event must cite: %+v", w)
	}

	// And the affordance stamp came from the same view, not a second
	// one: a read that stamped from elsewhere would be two reads
	// wearing one position.
	if len(e.Affordances) == 0 {
		t.Fatal("a keyed, subject-scoped remote read stamps affordances from its own view")
	}
}

// conformance: the posture is an exclusive-or on both surfaces, and the
// refusal says so. Checked on `offer list` too, because a flag pair
// added to one command and not the other is exactly the drift the
// shared binder exists to prevent.
func TestReadPostureIsExclusiveOnBothSurfaces(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	fp := signerFingerprint(t, priv)
	local := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", local, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"situation with neither", []string{"situation"}},
		{"situation with both", []string{"situation", "--ledger", local, "--remote", remote}},
		{"offer list with neither", []string{"offer", "list", "--actor", fp}},
		{"offer list with both", []string{"offer", "list", "--actor", fp, "--ledger", local, "--remote", remote}},
	} {
		e, code := runEnv(t, tc.args...)
		if code != 64 || e.Error == nil || e.Error.Code != "usage" {
			t.Errorf("%s must refuse as usage: %d %+v", tc.name, code, e)
			continue
		}
		if !strings.Contains(e.Error.Message, "--remote") || !strings.Contains(e.Error.Message, "not both") {
			t.Errorf("%s: the refusal must name the pair and its exclusivity: %s", tc.name, e.Error.Message)
		}
	}
	// The local arm still works, so the pair widened the surface rather
	// than replacing it.
	if _, code := runEnv(t, "situation", "--ledger", local); code != 0 {
		t.Error("the local posture must keep working: --remote is an addition, not a migration")
	}
	if _, code := runEnv(t, "offer", "list", "--ledger", local, "--actor", fp); code != 0 {
		t.Error("offer list must keep its local posture too")
	}
}
