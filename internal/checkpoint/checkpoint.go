// Package checkpoint is the shape of a checkpoint's snapshot citation
// (SEED-NEXT.md §II checkpoints; plans/os-8a5f14bb.md D4.5).
//
// A checkpoint that carries only a signature over a projection hash
// buys a fresh reader nothing it can spend: it can confirm that
// somebody attested to a state it has no way to obtain, and is left
// doing the full replay the checkpoint exists to spare it. So the
// charter requires the canonical materialization to be stored
// retrievably, with its hash and location in the event under a
// specified, versioned format.
//
// This package holds that payload and nothing else, mirroring
// internal/packet and internal/escalation: the admission boundary
// validates the SHAPE here, and the reader verifies the snapshot
// against it. Those are deliberately different checks in different
// places, because admission reads the ledger alone: admit.Context
// carries no artifact store, so retrievability is not a fact
// admission can establish. Shape at the door, contents at the read.
package checkpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

// Verb is the checkpoint event. It mirrors keyring's private constant
// the way internal/project's does, and the same parity drill applies.
const Verb = "system.checkpoint"

// Format is the versioned materialization format this build writes and
// reads. It is a value in the payload rather than a protocol version
// because the snapshot's serialization can move without the protocol
// moving: a reader that does not know a format refuses to start from
// it and replays instead, which is the safe direction.
const Format = "seed.projection.v1"

// Location is the v0 store a snapshot is retrievable from. The field
// exists so a deployment publishing snapshots elsewhere has somewhere
// to say so; v0 accepts exactly this one and refuses the rest, because
// a location nothing can fetch is the failure this payload exists to
// prevent.
const Location = "artifact"

// Checkpoint is the strict payload: {format, snapshot, location,
// position}. Position is the ledger position the materialization was
// built at, which is what tells a reader where to resume.
type Checkpoint struct {
	Format   string `json:"format"`
	Snapshot string `json:"snapshot"`
	Location string `json:"location"`
	Position string `json:"position"`
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Error is a malformed checkpoint payload, named the way the other
// payload packages name theirs so the refusal reads the same.
type Error struct {
	Subject string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("checkpoint on %s: %s", e.Subject, e.Reason)
}

// Parse validates the payload's shape and returns it. It is the whole
// admission-side contract: a versioned format this build knows, a
// sha256 digest, a location that can be fetched, and a non-negative
// position. What it deliberately does NOT check is whether the
// snapshot is actually there — see the package comment.
func Parse(subject string, raw []byte) (*Checkpoint, error) {
	var c Checkpoint
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, &Error{Subject: subject,
			Reason: fmt.Sprintf("the payload is the strict object {format, snapshot, location, position}: %v", err)}
	}
	if c.Format != Format {
		return nil, &Error{Subject: subject,
			Reason: fmt.Sprintf("format %q is not %q — the materialization format is specified and versioned, and a reader that does not know a format replays rather than guessing at it", c.Format, Format)}
	}
	if !digestRE.MatchString(c.Snapshot) {
		return nil, &Error{Subject: subject,
			Reason: fmt.Sprintf("snapshot %q is not a lowercase-hex sha256 digest — the checkpoint event carries the snapshot's hash so a reader can verify what it fetched", c.Snapshot)}
	}
	if c.Location != Location {
		return nil, &Error{Subject: subject,
			Reason: fmt.Sprintf("location %q is not %q — a snapshot nothing can fetch leaves the reader replaying, which is what the checkpoint exists to spare it", c.Location, Location)}
	}
	pos, err := strconv.Atoi(strings.TrimSpace(c.Position))
	if err != nil || pos < 0 {
		return nil, &Error{Subject: subject,
			Reason: fmt.Sprintf("position %q is not a non-negative integer — a snapshot that does not say what position it materializes cannot be resumed from", c.Position)}
	}
	return &c, nil
}

// Payload renders the strict payload for a snapshot at a position.
func Payload(snapshot string, position int) ([]byte, error) {
	return json.Marshal(Checkpoint{
		Format:   Format,
		Snapshot: snapshot,
		Location: Location,
		Position: strconv.Itoa(position),
	})
}

// At returns the checkpoint's position as an integer. Parse has
// already established it parses.
func (c *Checkpoint) At() int {
	pos, _ := strconv.Atoi(strings.TrimSpace(c.Position))
	return pos
}

// Snapshot is the canonical materialization the checkpoint points at:
// the published projection files at one verified position, keyed
// "<projection>/<file>". Go marshals map keys in sorted order and
// []byte as base64, so the same files at the same position always
// serialize to the same bytes and therefore the same digest. That
// determinism is the whole point: two readers materializing the same
// prefix agree, and a reader can check what it fetched against the
// hash the signed checkpoint carries.
type Snapshot struct {
	Format   string            `json:"format"`
	Position string            `json:"position"`
	Tip      string            `json:"tip"`
	Files    map[string][]byte `json:"files"`
}

// Materialize renders the canonical snapshot bytes.
func Materialize(position int, tip string, files map[string][]byte) ([]byte, error) {
	if files == nil {
		files = map[string][]byte{}
	}
	return json.Marshal(Snapshot{
		Format:   Format,
		Position: strconv.Itoa(position),
		Tip:      tip,
		Files:    files,
	})
}

// ReadSnapshot parses materialized bytes back, refusing a format this
// build does not know rather than guessing at its layout.
func ReadSnapshot(b []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("the snapshot does not parse: %w", err)
	}
	if s.Format != Format {
		return nil, fmt.Errorf("the snapshot's format %q is not %q — replay rather than starting from a materialization this build cannot read", s.Format, Format)
	}
	return &s, nil
}

// Cited is the newest admitted checkpoint a reader may start from,
// with where it sits in the chain and who signed it.
type Cited struct {
	Checkpoint *Checkpoint
	Position   int    // the checkpoint record's own position in the chain
	Signer     string // the signing fingerprint
}

// Capable reports whether the signer may checkpoint at the prefix
// before the record: the maintenance or operator capability, or root
// standing (the system.checkpoint row, spec/actors.md). The
// keyring at a seed/0 prefix is unseeded, and there the genesis root is
// the only capable signer.
func Capable(prefix []*event.Record, signer string) bool {
	ring, active, err := keyring.StateAt(prefix)
	if err != nil {
		return false
	}
	if ring.IsActiveRoot(signer) {
		return true
	}
	if !keyring.Applies(active) {
		return false
	}
	e, ok := ring.Get(signer)
	if !ok || e.Standing != keyring.StandingActive {
		return false
	}
	return ring.HasAnyCapability(signer, []string{keyring.CapMaintenance, keyring.CapOperator})
}

// Latest finds the newest system.checkpoint record whose payload
// parses and whose signer was capable at its own position; nil when
// the chain carries none. A checkpoint by an incapable signer is not
// a checkpoint a reader may start from, whatever its payload says.
func Latest(records []*event.Record) *Cited {
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.Event.Verb != Verb {
			continue
		}
		c, err := Parse(rec.Event.Subject, rec.Event.Payload)
		if err != nil {
			continue
		}
		if !Capable(records[:i], rec.Event.Actor) {
			continue
		}
		return &Cited{Checkpoint: c, Position: i, Signer: rec.Event.Actor}
	}
	return nil
}
