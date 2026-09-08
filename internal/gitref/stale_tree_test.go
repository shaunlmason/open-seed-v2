package gitref

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// refusingHook installs a pre-receive hook that declines the first push
// with the hook's own bad_prev shape at position n and admits every
// later one, counting invocations in a file: the exact refusal the
// 200-writer storm produced, planted without its unknown cause
// (plans/os-5063e8ba.md D4).
func refusingHook(t *testing.T, remote string, n int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server; a bare Windows checkout runs the cooperative or forge-hosted posture (spec/platform.md)")
	}
	return decliningHook(t, remote, fmt.Sprintf("seed-admit: rule verify: position %d: bad_prev: prev b692fec4bc38 does not cite tip 2492338b3552", n))
}

// decliningHook installs a pre-receive hook that declines the first
// push with the given message and admits every later one, counting
// invocations in a file.
func decliningHook(t *testing.T, remote, message string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server; a bare Windows checkout runs the cooperative or forge-hosted posture (spec/platform.md)")
	}
	counter := filepath.Join(t.TempDir(), "invocations")
	hook := fmt.Sprintf("#!/bin/sh\nif [ ! -f %[1]s ]; then\n  echo x > %[1]s\n  echo '%[2]s' >&2\n  exit 1\nfi\necho x >> %[1]s\nexit 0\n", counter, message)
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	return counter
}

// conformance: III.A, III.C row 4 — a hook's bad_prev at or beyond the
// position this client appended is the seventh race shape
// (plans/os-5063e8ba.md D1, D2, AC1): the loop re-links and lands on the
// next attempt, Relinked counts it, and the refused tree is kept under
// the client's state dir with the hook's message. The planted position
// is one beyond the append, the storm's exact offset.
func TestHookBadPrevAtOrBeyondOwnPositionRelinksAndKeepsTheTree(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	counter := refusingHook(t, remote, 2) // genesis at 0, this append at 1, the storm's offset of one beyond
	state := t.TempDir()
	c, err := NewClient(state, remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
	if err != nil {
		t.Fatalf("the loop must re-link and land: %v", err)
	}
	if res.Position != 1 || res.Attempts != 2 || res.Relinked != 1 {
		t.Fatalf("one re-link, landed on the second attempt at position 1: %+v", res)
	}
	if b, err := os.ReadFile(counter); err != nil || strings.Count(string(b), "x") != 2 {
		t.Fatalf("the hook saw the refused push and the re-linked one: %q %v", b, err)
	}
	kept, err := os.ReadDir(c.RefusedDir())
	if err != nil || len(kept) != 1 {
		t.Fatalf("exactly one refused tree is kept: %v %v", kept, err)
	}
	dir := filepath.Join(c.RefusedDir(), kept[0].Name())
	msg, err := os.ReadFile(filepath.Join(dir, "message.txt"))
	if err != nil || !strings.Contains(string(msg), "position 2: bad_prev") {
		t.Fatalf("the hook's message is kept beside the tree: %q %v", msg, err)
	}
	// Both halves of the evidence: the rejected commit's own tree (the
	// bytes the hook judged) and the work directory it was built from,
	// each readable as a ledger and, on this planted refusal, equal.
	for _, half := range []string{"commit", "worktree"} {
		store, err := ledger.Open(filepath.Join(dir, half))
		if err != nil {
			t.Fatal(err)
		}
		if _, count, err := store.Tip(); err != nil || count != 2 {
			t.Fatalf("the kept %s is the store as pushed (genesis and the append): count %d %v", half, count, err)
		}
	}
	commitHead, _ := os.ReadFile(filepath.Join(dir, "commit", "HEAD"))
	workHead, _ := os.ReadFile(filepath.Join(dir, "worktree", "HEAD"))
	if string(commitHead) != string(workHead) || len(commitHead) == 0 {
		t.Fatalf("the committed tree and the work tree agree on this refusal: %q vs %q", commitHead, workHead)
	}
	// The remote's chain verifies with the landed append.
	final, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := final.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := final.Materialize(tip, out); err != nil {
		t.Fatal(err)
	}
	rs, err := ledger.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := rs.VerifyFromGenesis(resolve); err != nil || rep.Count != 2 {
		t.Fatalf("the landed chain verifies with two records: %v %+v", err, rep)
	}
}

// conformance: plans/os-5063e8ba.md AC2 — a bad_prev below the appended
// position is the fetched prefix failing at the hook and stays a
// refusal to surface, with no retry and nothing kept.
func TestHookBadPrevBelowOwnPositionSurfaces(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	counter := refusingHook(t, remote, 0)
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
	if !errors.Is(err, ErrRemoteRejected) || errors.Is(err, ErrStaleTree) {
		t.Fatalf("a bad_prev below the append surfaces as the refusal it is: %v", err)
	}
	if b, err := os.ReadFile(counter); err != nil || strings.Count(string(b), "x") != 1 {
		t.Fatalf("no retry: hook ran %q (%v)", b, err)
	}
	if _, err := os.Stat(c.RefusedDir()); !os.IsNotExist(err) {
		t.Fatal("nothing is kept for a refusal that is not the seventh shape")
	}
	if staleTreeRefusal(errors.New("unrelated"), 1) || staleTreeRefusal(fmt.Errorf("%w: position x: bad_prev", ErrRemoteRejected), 1) {
		t.Fatal("only a parseable bad_prev position at or beyond the append is the shape")
	}
	if staleTreeRefusal(fmt.Errorf("%w: custom-hook: position 7: bad_prev", ErrRemoteRejected), 1) {
		t.Fatal("a bad_prev without the verifier's prefix is not the verifier's finding")
	}
}

// A policy hook that echoes the verifier's words without being the
// verifier (review finding on #300): "position N: bad_prev" at or
// beyond the append, but not under "seed-admit: rule verify:", is an
// unrelated refusal, surfaced without a retry and with nothing kept.
func TestHookEchoingBadPrevWithoutTheVerifierPrefixSurfaces(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	resolve := seedGenesis(t, remote, signer)
	counter := decliningHook(t, remote, "custom-hook: refused: position 9: bad_prev echoed from a pushed payload")
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.AppendLoop(milestoneDraft(t, signer, 1),
		func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 5)
	if !errors.Is(err, ErrRemoteRejected) || errors.Is(err, ErrStaleTree) {
		t.Fatalf("an echoed bad_prev surfaces as the refusal it is: %v", err)
	}
	if b, err := os.ReadFile(counter); err != nil || strings.Count(string(b), "x") != 1 {
		t.Fatalf("no retry: hook ran %q (%v)", b, err)
	}
	if _, err := os.Stat(c.RefusedDir()); !os.IsNotExist(err) {
		t.Fatal("nothing is kept for a refusal that is not the verifier's")
	}
}

// seedGenesisAt is seedGenesis with the seeding client's segment clock set.
func seedGenesisAt(t *testing.T, remote string, signer ed25519.PrivateKey, clock func() time.Time) ledger.Resolver {
	t.Helper()
	c, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	c.WithClock(clock)
	rec, err := genesis.Build(signer, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
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
	res, err := c.AppendLoop(Draft{
		V: rec.Event.V, TS: rec.Event.TS, Actor: rec.Event.Actor,
		Verb: rec.Event.Verb, Subject: rec.Event.Subject, Payload: rec.Event.Payload,
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 3)
	if err != nil || res.Position != 0 {
		t.Fatalf("genesis append: %+v %v", res, err)
	}
	return resolve
}

// conformance: III.A, III.C row 4 — two writers racing across a segment
// split (plans/os-5063e8ba.md D4, AC4): one stamps its segments before
// midnight and the other after, so the chain spans two files while
// both re-link, and every append lands in a chain that verifies from
// genesis. The 200-writer storm that lost an append crossed midnight;
// this drill is the shape at two writers, and its outcome is recorded
// in the decision log either way.
func TestTwoWritersAcrossMidnightLandAndVerify(t *testing.T) {
	remote := bareRemote(t)
	signer := fixtureKey(t, 1)
	clocks := []func() time.Time{
		func() time.Time { return time.Date(2026, 9, 3, 23, 59, 59, 0, time.UTC) },
		func() time.Time { return time.Date(2026, 9, 4, 0, 0, 1, 0, time.UTC) },
	}
	// The genesis lands in the earlier day's segment, as the storm's
	// history did, so the first append after midnight opens the split.
	resolve := seedGenesisAt(t, remote, signer, clocks[0])
	const perWriter = 6
	var wg sync.WaitGroup
	errs := make(chan error, 2*perWriter)
	relinked := make([]int, 2)
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			c, err := NewClient(t.TempDir(), remote, ref)
			if err != nil {
				errs <- err
				return
			}
			c.WithClock(clocks[w])
			for i := 0; i < perWriter; i++ {
				res, err := c.AppendLoop(milestoneDraft(t, signer, w*100+i),
					func(e event.Event) (*event.Record, error) { return event.Sign(e, signer) }, resolve, nil, 40)
				if err != nil {
					errs <- fmt.Errorf("writer %d append %d: %w", w, i, err)
					return
				}
				relinked[w] += res.Relinked
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	c, err := NewClient(t.TempDir(), remote, ref)
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
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil {
		t.Fatalf("the chain across the split must verify: %v", err)
	}
	if rep.Count != 1+2*perWriter {
		t.Fatalf("lost updates across the split: chain has %d events, want %d", rep.Count, 1+2*perWriter)
	}
	segs, _ := filepath.Glob(filepath.Join(dir, "segments", "*.jsonl"))
	if len(segs) < 2 {
		t.Fatalf("the drill must span two segment files, got %v", segs)
	}
	t.Logf("re-links across the split: %v", relinked)
}
