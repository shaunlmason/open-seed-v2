package main

// `seed project start` and the doctor's checkpoint-trust report at the
// terminal (plans/os-7508ab9e.md D1, D2).

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/history"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// checkpointedHistory generates a history, checkpoints it at `at` under
// the root, and returns the ledger and artifact dirs.
func checkpointedHistory(t *testing.T, contracts, at int) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ledger")
	res, err := history.Generate(history.Spec{Seed: 21, Contracts: contracts, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var records []*event.Record
	_ = store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil })
	files := map[string][]byte{}
	for _, p := range project.Default() {
		built, err := p.Build(records[:at], project.Inputs{})
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range built {
			files[p.Name+"/"+name] = body
		}
	}
	tipAt, _ := records[at-1].Event.Hash()
	body, _ := checkpoint.Materialize(at, tipAt, files)
	artifacts := t.TempDir()
	digest, err := artifact.Open(artifacts).Put(body)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := checkpoint.Payload(digest, at)
	fp, _ := event.Fingerprint(res.Keys.Root.Public().(ed25519.PublicKey))
	tip, _, _ := store.Tip()
	rec, err := event.Sign(event.Event{V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fp, Verb: checkpoint.Verb, Subject: "seed/0", Payload: json.RawMessage(payload), Prev: tip}, res.Keys.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, res.Resolve); err != nil {
		t.Fatal(err)
	}
	return dir, artifacts
}

func unlockedOut(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "out")
	t.Cleanup(func() {
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	return dir
}

// conformance: III.A — the choice is declared, not defaulted: with no
// block the reader refuses `trust_undeclared`; under `replay` it names
// the rebuild and does nothing; under `signers` it starts from the
// capable checkpoint, verifying the suffix and trusting the prefix,
// and the doctor reports the declaration either way.
func TestProjectStartFollowsTheDeclaration(t *testing.T) {
	ledgerDir, artifacts := checkpointedHistory(t, 2, history.Preamble+history.RecordsPerContract)

	undeclared := writeDeclaration(t, `{"posture": "cooperative"}`)
	e, code, _ := runEnvErr(t, "project", "start", "--ledger", ledgerDir, "--artifacts", artifacts, "--out", unlockedOut(t), "--config", undeclared)
	if code != 4 || e.Error == nil || e.Error.Code != "trust_undeclared" || !strings.Contains(e.Error.Message, "declared, never defaulted") {
		t.Fatalf("no declaration, no start: %d %+v", code, e)
	}
	e, code, _ = runEnvErr(t, "doctor", "--config", undeclared)
	cp, _ := e.Result["checkpoints"].(map[string]any)
	if code != 0 || cp["undeclared"] != true || cp["trust"] != nil {
		t.Fatalf("the doctor reports the choice as unmade: %d %+v", code, e)
	}

	replay := writeDeclaration(t, `{"posture": "cooperative", "checkpoints": {"trust": "replay"}}`)
	e, code, _ = runEnvErr(t, "project", "start", "--ledger", ledgerDir, "--artifacts", artifacts, "--out", unlockedOut(t), "--config", replay)
	if code != 0 || !e.OK || e.Result["action"] != "replay" {
		t.Fatalf("replay names the rebuild and does nothing: %d %+v", code, e)
	}
	e, code, _ = runEnvErr(t, "doctor", "--config", replay)
	if cp, _ := e.Result["checkpoints"].(map[string]any); code != 0 || cp["trust"] != "replay" {
		t.Fatalf("the doctor reports replay: %d %+v", code, e)
	}

	signers := writeDeclaration(t, `{"posture": "cooperative", "checkpoints": {"trust": "signers"}}`)
	out := unlockedOut(t)
	e, code, _ = runEnvErr(t, "project", "start", "--ledger", ledgerDir, "--artifacts", artifacts, "--out", out, "--config", signers)
	at := history.Preamble + history.RecordsPerContract
	if code != 0 || !e.OK || e.Result["trust"] != "signers" || e.Result["from"] != float64(at) || e.Result["trusted"] != float64(at) || e.Result["verified"].(float64) < 1 {
		t.Fatalf("signers starts from the checkpoint: %d %+v", code, e)
	}
	basis, ok, err := project.ReadBasis(out)
	if err != nil || !ok || basis.Position != at {
		t.Fatalf("the basis is written: %+v %v %v", basis, ok, err)
	}
	// A full rebuild into the same root rests on the chain alone.
	e, code, _ = runEnvErr(t, "project", "rebuild", "--ledger", ledgerDir, "--out", out)
	if code != 0 {
		t.Fatalf("rebuild: %d %+v", code, e)
	}
	if _, ok, _ := project.ReadBasis(out); ok {
		t.Fatal("a replay clears the basis")
	}

	if e, code, _ := runEnvErr(t, "project", "start", "--ledger", ledgerDir, "--out", out); code != 64 || e.Error.Code != "usage" {
		t.Fatalf("start needs the artifact store: %d %+v", code, e)
	}
	if _, code, _ := runEnvErr(t, "doctor", "--config", writeDeclaration(t, `{"posture": "cooperative", "checkpoints": {"trust": "maybe"}}`)); code != 13 {
		t.Fatalf("an unknown trust value refuses at 13, got %d", code)
	}
}
