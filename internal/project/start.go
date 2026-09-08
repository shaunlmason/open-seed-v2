package project

// Starting from a checkpoint (plans/os-7508ab9e.md D2; spec/checkpoints.md):
// the reader that a `signers` declaration permits. It parses the whole
// chain, folds the keyring to judge the checkpoint's signer at its own
// position, fetches and verifies the snapshot, holds the snapshot's
// position and tip to the chain, verifies linkage and signatures for
// the suffix only, rebuilds every registered projection over the full
// record list, and refuses to publish anything until the snapshot's
// files equal its own build at the cited position. What it skips is
// exactly the prefix's signature verification, and nothing else.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// BasisFile records, beside the projection roots, what a fresh
// reader's builds rest on: a full replay, or a checkpoint's word for
// the prefix. It lives in the output root rather than in every build's
// stamp so that a build from a checkpoint and a build from genesis
// stay byte-identical, which is the proof the reader exists to pass.
const BasisFile = "basis.json"

// Basis is the file's content.
type Basis struct {
	Trust      string `json:"trust"`            // "signers" or "replay"
	Checkpoint int    `json:"checkpoint"`       // the checkpoint record's position ("signers" only)
	Position   int    `json:"position"`         // the position the prefix was trusted up to
	Tip        string `json:"tip"`              // the attested chain hash at that position
	Signer     string `json:"signer,omitempty"` // who attested it
	Trusted    int    `json:"trusted"`          // records replayed without signature verification
	Verified   int    `json:"verified"`         // records verified in full
}

// ErrNoCheckpoint says the chain carries no checkpoint a reader may
// start from: none admitted, or none signed by a capable key.
var ErrNoCheckpoint = errors.New("the chain carries no checkpoint signed by a capable key: replay from genesis (`seed project rebuild`)")

// MismatchError is a checkpoint that does not describe the state it
// attests: the snapshot's digest, position, tip or a projection file
// disagrees with what the reader computes. The reader does not publish
// from it.
type MismatchError struct {
	What   string
	Detail string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("the checkpoint does not reproduce: %s — %s; replay from genesis instead", e.What, e.Detail)
}

// StartReport is what StartFromCheckpoint did.
type StartReport struct {
	Basis   Basis
	Results []Result
}

// StartFromCheckpoint publishes every projection under outDir starting
// from the newest capable checkpoint in ledgerDir, verifying the suffix
// in full and trusting the prefix on the checkpoint signer's word.
func StartFromCheckpoint(ledgerDir, artifactsDir, outDir string, projections []Projection, resolve ledger.Resolver, vopts ...ledger.VerifyOption) (*StartReport, error) {
	store, err := ledger.OpenReadOnly(ledgerDir)
	if err != nil {
		return nil, err
	}
	// Parsing is not verification: the records are read so the
	// keyring can be folded to judge the checkpoint's signer and the
	// snapshot cross-checked, nothing more yet.
	var records []*event.Record
	if err := store.Records(func(pos int, rec *event.Record) error {
		records = append(records, rec)
		return nil
	}); err != nil {
		return nil, err
	}
	cited := checkpoint.Latest(records)
	if cited == nil {
		return nil, ErrNoCheckpoint
	}
	body, err := artifact.Open(artifactsDir).Get(cited.Checkpoint.Snapshot)
	if err != nil {
		return nil, &MismatchError{What: "the snapshot is not retrievable", Detail: err.Error()}
	}
	if got := artifact.Digest(body); got != cited.Checkpoint.Snapshot {
		return nil, &MismatchError{What: "the snapshot's digest", Detail: fmt.Sprintf("fetched %s, the checkpoint attested %s", got, cited.Checkpoint.Snapshot)}
	}
	snap, err := checkpoint.ReadSnapshot(body)
	if err != nil {
		return nil, &MismatchError{What: "the snapshot's format", Detail: err.Error()}
	}
	at, err := strconv.Atoi(snap.Position)
	if err != nil || at != cited.Checkpoint.At() {
		return nil, &MismatchError{What: "the snapshot's position", Detail: fmt.Sprintf("the snapshot says %q, the checkpoint says %d", snap.Position, cited.Checkpoint.At())}
	}
	if at <= 0 || at > cited.Position {
		return nil, &MismatchError{What: "the snapshot's position", Detail: fmt.Sprintf("position %d is not a prefix before the checkpoint at %d", at, cited.Position)}
	}
	// The cross-check before anything is published: the snapshot's
	// files must equal this reader's own derivation at the cited
	// position, or the checkpoint attests a state the chain does not
	// support. It costs one fold and turns a lying checkpoint from an
	// invisible failure into a named one.
	prefix := records[:at]
	derived := map[string]bool{}
	for _, p := range projections {
		built, err := p.Build(prefix, Inputs{})
		if err != nil {
			return nil, fmt.Errorf("projection %s at %d: %v", p.Name, at, err)
		}
		// Every file this reader derives must be in the snapshot and
		// equal, whatever the projection names its files (the cache
		// publishes cache.db, not cache.json); a projection the
		// snapshot lacks altogether fails on its first file, so a
		// checkpoint cannot attest less than the derivation.
		for name, want := range built {
			derived[p.Name+"/"+name] = true
			got, ok := snap.Files[p.Name+"/"+name]
			if !ok {
				return nil, &MismatchError{What: "the snapshot's files", Detail: fmt.Sprintf("%s/%s is missing", p.Name, name)}
			}
			if string(got) != string(want) {
				return nil, &MismatchError{What: "the snapshot's files", Detail: fmt.Sprintf("%s/%s differs from the derivation at position %d", p.Name, name, at)}
			}
		}
	}
	// And nothing more: a file the derivation does not produce is a
	// state the chain does not support either.
	for name := range snap.Files {
		if !derived[name] {
			return nil, &MismatchError{What: "the snapshot's files", Detail: fmt.Sprintf("%s is in the snapshot and not in the derivation at position %d", name, at)}
		}
	}
	// The trusted-prefix replay: linkage, hashing, the version
	// discipline and the keyring for every record; signatures for the
	// suffix; the attested tip held at the trusted position. A
	// mismatch refuses before anything is written.
	if err := WriteBasis(outDir, nil); err != nil {
		return nil, err
	}
	vopts = append(vopts, ledger.WithTrustedPrefix(at, snap.Tip))
	results, err := RebuildWith(ledgerDir, outDir, projections, resolve, Inputs{}, vopts...)
	if err != nil {
		return nil, err
	}
	basis := Basis{Trust: "signers", Checkpoint: cited.Position, Position: at, Tip: snap.Tip, Signer: cited.Signer, Trusted: at, Verified: len(records) - at}
	if err := WriteBasis(outDir, &basis); err != nil {
		return nil, err
	}
	return &StartReport{Basis: basis, Results: results}, nil
}

// WriteBasis writes the basis file into the output root, or removes it
// when basis is nil (a full replay rests on nothing but the chain).
// The root is locked between rebuilds, so the write opens and relocks
// it the way the engine's own swap does.
func WriteBasis(outDir string, basis *Basis) error {
	path := filepath.Join(outDir, BasisFile)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := setMode(outDir, 0o755); err != nil {
		return err
	}
	defer func() { _ = setMode(outDir, 0o555) }()
	if basis == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(basis)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ReadBasis reads the basis file; ok is false when a replay wrote none.
func ReadBasis(outDir string) (*Basis, bool, error) {
	b, err := os.ReadFile(filepath.Join(outDir, BasisFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var basis Basis
	if err := json.Unmarshal(b, &basis); err != nil {
		return nil, false, fmt.Errorf("the basis file does not parse: %v", err)
	}
	return &basis, true, nil
}
