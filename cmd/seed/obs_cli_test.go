package main

// The observation verbs end-to-end (plans/os-2ff8dbf1.md): emit
// appends well-formed lines onto the per-run stream, rebuild with
// declared inputs publishes the observation section under an
// input-keyed build id, and the refusal exits hold (--obs without
// --as-of smuggles no wall clock).

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestObsEmitAndDeclaredInputRebuildCLI(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	for _, step := range [][]string{
		{"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "` + version.Seed1 + `"}`},
		{"--verb", "intent.filed", "--subject", "c-1", "--payload", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{"--verb", "contract.specified", "--subject", "c-1", "--payload", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`},
	} {
		args := append([]string{"ledger", "append", "--ledger", ld, "--key", priv}, step...)
		if e, code := runEnv(t, args...); code != 0 {
			t.Fatalf("append %s failed: %d %+v", step[1], code, e.Error)
		}
	}

	// Claiming is online-only at the CLI (the 5.2 offline boundary),
	// so the drill seeds the claim through the library, like any
	// admitted history would carry it. The root key is the writeKeys
	// fixture seed, so it signs and its fingerprint derives directly.
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)
	rootFP, err := event.Fingerprint(rootKey.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	tip, _, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: version.Seed1, TS: "2026-09-01T00:45:00Z", Actor: rootFP,
		Verb: "claim.taken", Subject: "c-1", Payload: json.RawMessage(`{}`), Prev: tip,
	}, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	obsDir := filepath.Join(t.TempDir(), "obs")
	for _, l := range [][]string{
		{"--count", "1", "--step", "clone", "--ts", "2026-09-01T00:50:00Z"},
		{"--count", "2", "--step", "build", "--ts", "2026-09-01T00:58:00Z"},
	} {
		args := append([]string{"obs", "emit", "--dir", obsDir, "--actor", rootFP, "--fence", "4", "--subject", "c-1"}, l...)
		if e, code := runEnv(t, args...); code != 0 || !e.OK {
			t.Fatalf("obs emit failed: %d %+v", code, e)
		}
	}
	snap, err := obs.Load(obsDir)
	if err != nil || len(snap.Streams) != 1 || len(snap.Streams[0].Lines) != 2 {
		t.Fatalf("emit must append well-formed lines: %v %+v", err, snap)
	}

	out := filepath.Join(t.TempDir(), "proj")
	t.Cleanup(func() { unlockForCleanup(t, out) })

	// --obs without --as-of refuses: no ambient clock enters a build.
	if e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out, "--obs", obsDir); code == 0 || !strings.Contains(e.Error.Message, "as-of") {
		t.Fatalf("--obs without --as-of must refuse usage, got %d %+v", code, e)
	}

	e, code := runEnv(t, "project", "rebuild", "--ledger", ld, "--out", out, "--obs", obsDir, "--as-of", "2026-09-01T01:00:00Z")
	if code != 0 || !e.OK {
		t.Fatalf("input-bearing rebuild failed: %d %+v", code, e)
	}
	cur, err := os.ReadFile(filepath.Join(out, "report", "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	asOf, err := time.Parse(time.RFC3339, "2026-09-01T01:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := (project.Inputs{Obs: snap, AsOf: asOf, Thresholds: obs.DefaultThresholds()}).Digest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(cur)), "-i"+digest[:12]) {
		t.Fatalf("the report build id must carry the inputs digest: %s", cur)
	}
	raw, err := os.ReadFile(filepath.Join(out, "report", "builds", strings.TrimSpace(string(cur)), project.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Observation == nil {
		t.Fatal("declared inputs must publish the observation section")
	}
	cls, ok := rep.Observation.Claims["c-1"]
	if !ok || cls.State != obs.Live || cls.Count != 2 {
		t.Fatalf("the active claim must classify from its stream: %+v", rep.Observation.Claims)
	}
}
