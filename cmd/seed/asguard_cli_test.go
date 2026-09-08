package main

// The declared-identity guard (plans/os-9a89245c.md). internal/loop
// fingerprints the key file before every act; the act then crosses
// this seam as --key <path> and loopSigner reopens the same path
// independently, so a replacement between those two reads is observed
// by only one of them. --as moves the check onto the read the
// SIGNATURE uses.
//
// Both drills live here rather than in internal/loop, and the reason
// is load-bearing: that package's rotation drills run against the
// recorder DOUBLE, so --as never reaches loopSigner in them and they
// would pass with either regression present.

import (
	"crypto/ed25519"
	"encoding/pem"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
)

// replaceKeyAt overwrites an existing key file in place, which is what
// a rotation does: the path stays, the bytes behind it change.
func replaceKeyAt(t *testing.T, path string, first byte) string {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	k := ed25519.NewKeyFromSeed(seed)
	block, err := ssh.MarshalPrivateKey(k, "rotated-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// rotatingVerbs replaces the key file at the exact moment the window
// is open: AFTER the loop's checkIdentity has run and BEFORE the CLI
// reopens the path. The seam between those two reads IS the race, so
// the drill sits inside it by construction rather than by timing — a
// race reproduced by sleeping passes green on a slower runner (D5).
type rotatingVerbs struct {
	inner loop.Verbs
	t     *testing.T
	path  string
	on    string
	seed  byte
	fired bool
	// raced is the result of the very call the rotation landed inside.
	// It is what the drill asserts on: the loop reacts to the refusal
	// afterwards (its own checkIdentity catches the rotation at the
	// NEXT act), so the final error names that, not the seam. The
	// signing site's refusal is only visible here.
	raced loop.Result
}

func (r *rotatingVerbs) Run(args ...string) loop.Result {
	name := ""
	if len(args) >= 2 {
		name = args[0] + " " + args[1]
	}
	if name == r.on && !r.fired {
		r.fired = true
		replaceKeyAt(r.t, r.path, r.seed)
		r.raced = r.inner.Run(args...)
		return r.raced
	}
	return r.inner.Run(args...)
}

// conformance: a key replaced INSIDE the window between the loop's
// check and the CLI's read refuses at the signing site, rather than
// signing as an identity the loop never declared.
func TestAKeyReplacedInsideTheWindowRefusesAtTheSigningSite(t *testing.T) {
	const subject = "c-1"
	// The REMOTE posture, because claim take is remote-only: a loop
	// that cannot open a window has no window to be raced inside.
	remote, state, workerKey, workerFP := remoteWorkLedger(t, subject)

	// The loop's own check passes (it reads the file first); the
	// wrapper then swaps the bytes before the CLI reads them, which is
	// exactly the interval the race lives in.
	r := &rotatingVerbs{inner: loopVerbs{}, t: t, path: workerKey, on: "budget reserve", seed: 99}
	d, err := loop.New(implementerManifest(t), r,
		[]string{"--remote", remote, "--state", state}, workerKey,
		loop.WorkFunc(func(string, loop.Situation) (int, error) { return 7, nil }),
		loop.WithBase("abc1234..abc1234"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(10); err == nil {
		t.Fatal("the iteration must not complete: the key changed under it")
	}
	if !r.fired {
		t.Fatal("this drill is vacuous unless the key was actually replaced inside the window")
	}
	// The assertion that matters: the act whose signing read saw the
	// NEW bytes refused AT THE SIGNING SITE, rather than signing as an
	// identity the loop never declared. Without --as it would have
	// signed happily, and only the loop's next checkIdentity would
	// have noticed — one act too late.
	if r.raced.Code != "usage" {
		t.Fatalf("the raced act must refuse at the seam as usage, got %+v", r.raced)
	}
	for _, want := range []string{workerFP, "changed under the caller"} {
		if !strings.Contains(r.raced.Message, want) {
			t.Errorf("the refusal must name %q: %q", want, r.raced.Message)
		}
	}
}

// The guard's own table at the seam: a matching fingerprint admits, a
// mismatched one refuses usage naming both, and an absent one checks
// nothing — the verbs stay reachable by hand, and an operator acting
// once has no loop to race with (D2).
func TestTheDeclaredIdentityGuard(t *testing.T) {
	ld, _, _, _, _, _, _, keys, fps := offerLedgerAndSubject(t, "c-1")
	keyPath, declared := keys["workerA"], fps["workerA"]

	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}

	// Matching: admits.
	e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", keyPath,
		"--subject", "c-1", "--amount", "10", "--as", declared)
	if code != 0 || !e.OK {
		t.Fatalf("a declared identity that matches the key admits: %d %+v", code, e)
	}

	// Absent: no check at all.
	if e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", keyPath,
		"--subject", "c-1", "--amount", "10"); code != 0 || !e.OK {
		t.Fatalf("no --as means no check: %d %+v", code, e)
	}

	// Mismatched: refuses at the seam, naming both fingerprints.
	before := chainCount(t, ld)
	rotated := replaceKeyAt(t, keyPath, 99)
	e, code = runEnv(t, "budget", "reserve", "--ledger", ld, "--key", keyPath,
		"--subject", "c-1", "--amount", "10", "--as", declared)
	if code == 0 || e.OK {
		t.Fatalf("a key without the declared fingerprint must refuse: %d %+v", code, e)
	}
	if e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("it is a caller error at the seam, not a boundary refusal: %+v", e)
	}
	for _, want := range []string{declared, rotated} {
		if !strings.Contains(e.Error.Message, want) {
			t.Errorf("the refusal names both fingerprints (missing %s): %q", want, e.Error.Message)
		}
	}
	if chainCount(t, ld) != before {
		t.Fatalf("the refused act signs nothing: %d then %d", before, chainCount(t, ld))
	}
}

// conformance, criterion 6: the last-ditch exit reaches the ADMISSION
// BOUNDARY under a rotated key and refuses there — fenced_out, because
// the new key is not the holder — rather than stopping at the seam with
// usage.
//
// This has to run against the real CLI. internal/loop's version of it
// runs against the recorder double, which manufactures the refusal
// without loopSigner or admission running at all, so a regression that
// rejected the no---as last-ditch invocation would leave that one green
// (review finding on #202).
func TestTheLastDitchExitReachesTheBoundaryUnderARotatedKey(t *testing.T) {
	const subject = "c-1"
	remote, state, workerKey, _ := remoteWorkLedger(t, subject)

	// Rotate at the settle: the window is open by then, so the strand
	// path runs and attempts claim park under the NEW key.
	r := &rotatingVerbs{inner: loopVerbs{}, t: t, path: workerKey, on: "budget settle", seed: 99}
	d, err := loop.New(implementerManifest(t), r,
		[]string{"--remote", remote, "--state", state}, workerKey,
		loop.WorkFunc(func(string, loop.Situation) (int, error) { return 7, nil }),
		loop.WithBase("abc1234..abc1234"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Step(10)
	if err == nil {
		t.Fatal("the iteration must not complete: the key changed under it")
	}
	if !r.fired {
		t.Fatal("this drill is vacuous unless the key was replaced inside the window")
	}
	// The error names the stranded window, which is what strand
	// reports once its exit attempt has failed at the boundary.
	if !strings.Contains(err.Error(), "left OPEN") || !strings.Contains(err.Error(), "reap") {
		t.Fatalf("the loop must report the window stranded and needing a reap: %v", err)
	}
	// And the exit was refused by the BOUNDARY, not by the seam. That
	// is the property: a usage refusal would mean --as had been
	// attached to the last-ditch act and stopped it one layer too
	// early, never reaching admission at all.
	//
	// WHICH boundary rule fires is a fixture detail, not the
	// conformance point, so it is deliberately not pinned: this
	// fixture enrols only workerA and a supervisor, so the rotated key
	// is unenrolled and the actor rule answers first. Rotating to an
	// enrolled non-holder with claim standing would reach the fence
	// rule instead. Both are admission; the seam is what must not
	// answer.
	if !strings.Contains(err.Error(), "admission refused by rule") {
		t.Errorf("the last-ditch exit must reach the boundary and carry its refusal: %v", err)
	}
	if strings.Contains(err.Error(), "park refused (usage)") {
		t.Errorf("the exit stopped at the seam instead of the boundary: %v", err)
	}
}
