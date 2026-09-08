package project

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/history"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// checkpointed builds a representative history, materializes the
// projection state at position `at`, stores the snapshot, and appends
// the checkpoint under signer (the root by default). It returns the
// ledger dir, the artifact dir and the resolver.
type fixture struct {
	ledger, artifacts string
	res               *history.Result
	records           []*event.Record
}

func checkpointed(t *testing.T, contracts, at int, signer ed25519.PrivateKey, tamper func(files map[string][]byte)) fixture {
	t.Helper()
	dir := t.TempDir()
	res, err := history.Generate(history.Spec{Seed: 11, Contracts: contracts, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var records []*event.Record
	if err := store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil }); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, p := range Default() {
		built, err := p.Build(records[:at], Inputs{})
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range built {
			files[p.Name+"/"+name] = body
		}
	}
	if tamper != nil {
		tamper(files)
	}
	tipAt, err := records[at-1].Event.Hash()
	if err != nil {
		t.Fatal(err)
	}
	body, err := checkpoint.Materialize(at, tipAt, files)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := t.TempDir()
	digest, err := artifact.Open(artifacts).Put(body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := checkpoint.Payload(digest, at)
	if err != nil {
		t.Fatal(err)
	}
	if signer == nil {
		signer = res.Keys.Root
	}
	fp, _ := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	tip, _, _ := store.Tip()
	rec, err := event.Sign(event.Event{V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fp, Verb: checkpoint.Verb, Subject: "seed/0", Payload: json.RawMessage(payload), Prev: tip}, signer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, res.Resolve); err != nil {
		t.Fatal(err)
	}
	// A little more history after the checkpoint, so the suffix is
	// real work and not just the checkpoint record.
	records = nil
	if err := store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil }); err != nil {
		t.Fatal(err)
	}
	return fixture{ledger: dir, artifacts: artifacts, res: res, records: records}
}

// outDir is a temp output root whose locked trees are unlocked at
// cleanup, the way the engine's own drills do it.
func outDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
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

// treeOf reads every published file under outDir except the basis.
func treeOf(t *testing.T, outDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(outDir, path)
		if rel == BasisFile {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// corruptSignature flips one hex digit of the record at pos in the
// segment files: the chain hash is unchanged (it excludes the
// signature), so only a signature check can notice.
func corruptSignature(t *testing.T, ledgerDir string, pos int) {
	t.Helper()
	segs, err := os.ReadDir(filepath.Join(ledgerDir, "segments"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, seg := range segs {
		path := filepath.Join(ledgerDir, "segments", seg.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		for i, line := range lines {
			if n == pos {
				var rec event.Record
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					t.Fatal(err)
				}
				sig := []byte(rec.Sig)
				if sig[0] == '0' {
					sig[0] = '1'
				} else {
					sig[0] = '0'
				}
				rec.Sig = string(sig)
				out, _ := json.Marshal(rec)
				lines[i] = string(out)
				if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			n++
		}
	}
	t.Fatalf("no record at position %d", pos)
}

// conformance: III.A — replay-from-checkpoint equals replay-from-
// genesis: every published build byte-identical, the basis file saying
// what was trusted and what was verified.
func TestStartFromCheckpointEqualsGenesis(t *testing.T) {
	f := checkpointed(t, 3, history.Preamble+history.RecordsPerContract*2, nil, nil)
	fromGenesis := outDir(t)
	if _, err := Rebuild(f.ledger, fromGenesis, Default(), f.res.Resolve); err != nil {
		t.Fatal(err)
	}
	fromCheckpoint := outDir(t)
	rep, err := StartFromCheckpoint(f.ledger, f.artifacts, fromCheckpoint, Default(), f.res.Resolve)
	if err != nil {
		t.Fatal(err)
	}
	at := history.Preamble + history.RecordsPerContract*2
	if rep.Basis.Trust != "signers" || rep.Basis.Position != at || rep.Basis.Trusted != at || rep.Basis.Verified != len(f.records)-at || rep.Basis.Checkpoint != len(f.records)-1 {
		t.Fatalf("the basis says what was trusted and verified: %+v", rep.Basis)
	}
	a, b := treeOf(t, fromGenesis), treeOf(t, fromCheckpoint)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("both roots publish the same files: %d vs %d", len(a), len(b))
	}
	for name, want := range a {
		if b[name] != want {
			t.Fatalf("%s differs between the genesis replay and the checkpoint start", name)
		}
	}
	basis, ok, err := ReadBasis(fromCheckpoint)
	if err != nil || !ok || basis.Tip != rep.Basis.Tip {
		t.Fatalf("the basis file is readable: %+v %v %v", basis, ok, err)
	}
	if _, ok, _ := ReadBasis(fromGenesis); ok {
		t.Fatal("a genesis replay rests on nothing but the chain")
	}
	// A later full rebuild into the checkpoint root clears the basis.
	if err := WriteBasis(fromCheckpoint, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := ReadBasis(fromCheckpoint); ok {
		t.Fatal("a replay removes the basis")
	}
}

// conformance: III.A — the proof has teeth: a prefix record whose
// signature is corrupted after the checkpoint (same hash, same chain)
// is caught by the genesis replay and NOT by the checkpoint start —
// which is exactly what "trust in the signer set" means — while a
// suffix record corrupted the same way is caught by both.
func TestStartFromCheckpointTrustsExactlyThePrefixSignatures(t *testing.T) {
	at := history.Preamble + history.RecordsPerContract
	f := checkpointed(t, 3, at, nil, nil)
	corruptSignature(t, f.ledger, 3) // inside the trusted prefix
	if _, err := Rebuild(f.ledger, outDir(t), Default(), f.res.Resolve); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("the genesis replay must catch the corrupted prefix signature, got %v", err)
	}
	if _, err := StartFromCheckpoint(f.ledger, f.artifacts, outDir(t), Default(), f.res.Resolve); err != nil {
		t.Fatalf("the checkpoint start trusts the prefix on the signer's word and must not notice: %v", err)
	}

	g := checkpointed(t, 3, at, nil, nil)
	corruptSignature(t, g.ledger, at+2) // after the trusted prefix
	if _, err := Rebuild(g.ledger, outDir(t), Default(), g.res.Resolve); err == nil {
		t.Fatal("the genesis replay must catch the corrupted suffix signature")
	}
	if _, err := StartFromCheckpoint(g.ledger, g.artifacts, outDir(t), Default(), g.res.Resolve); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("the checkpoint start verifies the suffix in full, got %v", err)
	}
}

// A checkpoint that lies — files that are not the derivation, a tip
// that is not the chain's, an incapable signer — is refused by name
// and nothing is published from it.
func TestStartFromCheckpointRefusesALyingCheckpoint(t *testing.T) {
	at := history.Preamble + history.RecordsPerContract
	f := checkpointed(t, 2, at, nil, func(files map[string][]byte) {
		for name := range files {
			if strings.HasSuffix(name, "/contracts.json") {
				files[name] = append(files[name], '\n')
			}
		}
	})
	out := outDir(t)
	var me *MismatchError
	if _, err := StartFromCheckpoint(f.ledger, f.artifacts, out, Default(), f.res.Resolve); !errors.As(err, &me) || me.What != "the snapshot's files" {
		t.Fatalf("a snapshot that is not the derivation is a mismatch, got %v", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatal("nothing is published from a lying checkpoint")
	}

	// An incapable signer: the holder's checkpoint is not one a reader
	// may start from, so the chain carries none.
	h := checkpointed(t, 2, at, nil, nil)
	holder := checkpointed(t, 2, at, h.res.Keys.Holder, nil)
	if _, err := StartFromCheckpoint(holder.ledger, holder.artifacts, outDir(t), Default(), holder.res.Resolve); !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("a holder's checkpoint is not capable, got %v", err)
	}
	if _, err := StartFromCheckpoint(t.TempDir(), t.TempDir(), outDir(t), Default(), f.res.Resolve); err == nil {
		t.Fatal("an empty ledger has no checkpoint")
	}

	// A snapshot whose tip is not the chain's hash at its position:
	// the trusted-prefix replay refuses before writing.
	k := checkpointed(t, 2, at, nil, nil)
	store, _ := ledger.Open(k.ledger)
	var records []*event.Record
	if err := store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil }); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, p := range Default() {
		built, _ := p.Build(records[:at], Inputs{})
		for name, body := range built {
			files[p.Name+"/"+name] = body
		}
	}
	body, _ := checkpoint.Materialize(at, strings.Repeat("ab", 32), files)
	digest, _ := artifact.Open(k.artifacts).Put(body)
	payload, _ := checkpoint.Payload(digest, at)
	fp, _ := event.Fingerprint(k.res.Keys.Root.Public().(ed25519.PublicKey))
	tip, _, _ := store.Tip()
	rec, _ := event.Sign(event.Event{V: version.Seed1, TS: "2026-09-02T00:00:01Z", Actor: fp, Verb: checkpoint.Verb, Subject: "seed/0", Payload: json.RawMessage(payload), Prev: tip}, k.res.Keys.Root)
	if _, err := store.Append(rec, k.res.Resolve); err != nil {
		t.Fatal(err)
	}
	out = outDir(t)
	var fail *ledger.Failure
	if _, err := StartFromCheckpoint(k.ledger, k.artifacts, out, Default(), k.res.Resolve); !errors.As(err, &fail) || fail.Reason != ledger.ReasonTrustMismatch {
		t.Fatalf("an attested tip the chain does not reach refuses as a trust mismatch, got %v", err)
	}
	if _, ok, _ := ReadBasis(out); ok {
		t.Fatal("no basis is written for a refused start")
	}
}
