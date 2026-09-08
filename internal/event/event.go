// Package event implements the Seed canonical event form: the wire-level
// core of the ledger (charter Part II section 1, Appendix B). Encodings are
// normative in spec/protocol.md: JCS (RFC 8785) canonical bytes,
// lowercase-hex SHA-256 chain hashes, lowercase-hex Ed25519 signatures, and
// the {"event": ..., "sig": ...} ledger record wrapper. Verifiers recompute
// canonical bytes from parsed events; nothing trusts stored byte sequences.
package event

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gowebpki/jcs"
)

// EmptyHash is the genesis prev: the SHA-256 of zero bytes
// (spec/protocol.md, "Canonical event form").
const EmptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Typed refusal reasons, mapped to envelope error codes by later phases.
var (
	ErrBadSignature   = errors.New("signature does not verify over the canonical form")
	ErrBadSigEncoding = errors.New("sig is not 128 lowercase hex characters")
	ErrBadPayload     = errors.New("payload is not a JSON object")
)

// Event carries exactly the canonical fields of spec/protocol.md.
// Payload stays raw JSON: verb-specific schemas land with their phases, and
// canonicalization must not depend on Go-side typing.
type Event struct {
	V       string          `json:"v"`
	TS      string          `json:"ts"`
	Actor   string          `json:"actor"`
	Verb    string          `json:"verb"`
	Subject string          `json:"subject"`
	Payload json.RawMessage `json:"payload"`
	Prev    string          `json:"prev"`
}

// Canonical returns the RFC 8785 (JCS) bytes of the event object including
// prev. These are the signed bytes and the hashed bytes; they are computed
// fresh on every call, never cached from storage.
func (e *Event) Canonical() ([]byte, error) {
	if !payloadIsObject(e.Payload) {
		return nil, ErrBadPayload
	}
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

func payloadIsObject(p json.RawMessage) bool {
	for _, c := range p {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// Hash is the chain hash: lowercase-hex SHA-256 of the canonical bytes. The
// successor event's prev cites it.
func (e *Event) Hash() (string, error) {
	b, err := e.Canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Record is the ledger record wrapper {"event": ..., "sig": ...}. Wrapper
// field order and whitespace are irrelevant to identity: only the inner
// event's canonical bytes are signed and hashed.
type Record struct {
	Event Event  `json:"event"`
	Sig   string `json:"sig"`
}

// Sign returns a Record for e signed by priv: the Ed25519 signature over
// the canonical bytes, encoded as 128 lowercase hex characters.
func Sign(e Event, priv ed25519.PrivateKey) (*Record, error) {
	b, err := e.Canonical()
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, b)
	return &Record{Event: e, Sig: hex.EncodeToString(sig)}, nil
}

// Verify recomputes the canonical bytes from the parsed event and checks
// the record's signature against pub. The signature must be the one
// accepted wire form: exactly 128 lowercase hex characters (uppercase hex
// decodes to the same bytes, and two accepted encodings for one signature
// is a nonconformance admission must not allow).
func (r *Record) Verify(pub ed25519.PublicKey) error {
	if len(r.Sig) != 2*ed25519.SignatureSize || r.Sig != strings.ToLower(r.Sig) {
		return ErrBadSigEncoding
	}
	sig, err := hex.DecodeString(r.Sig)
	if err != nil {
		return ErrBadSigEncoding
	}
	b, err := r.Event.Canonical()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, b, sig) {
		return ErrBadSignature
	}
	return nil
}

// Marshal renders the record as one JSON line for JSONL segment storage.
func (r *Record) Marshal() ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ParseRecord parses one ledger record line strictly. Field order and
// whitespace do not matter (identity comes from recomputation, not bytes),
// but the shape does: unknown fields anywhere in the wrapper or the event,
// duplicate keys at any level (payload included), and trailing data all
// refuse. Without this, a record could carry correctly signed core fields
// plus unsigned extra material that survives in storage while escaping
// canonicalization, schema checks, and the classification lint.
func ParseRecord(line []byte) (*Record, error) {
	// The ledger is LF-only (spec/platform.md): a carriage return
	// in a line is CRLF mangling (core.autocrlf, an editor, a transfer)
	// or an injection, and either is refused rather than normalized,
	// since the canonical bytes a signature covers must be the bytes
	// on disk.
	if bytes.IndexByte(line, '\r') >= 0 {
		return nil, errors.New("ledger record does not parse: a carriage return in a ledger line — the ledger is LF-only, and a CRLF conversion is refused rather than normalized")
	}
	if err := rejectDuplicateKeys(line); err != nil {
		return nil, fmt.Errorf("ledger record does not parse: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var r Record
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("ledger record does not parse: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("ledger record does not parse: trailing data after the record object")
	}
	return &r, nil
}

// rejectDuplicateKeys walks the JSON token stream and refuses an object
// that states the same key twice at the same level. encoding/json silently
// keeps the last duplicate, so two parsers can disagree about the same
// bytes; one accepted wire form means duplicates are illegal everywhere.
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyTok.(string)
				if !ok {
					return fmt.Errorf("object key is %v, not a string", keyTok)
				}
				if _, dup := seen[key]; dup {
					return fmt.Errorf("duplicate key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
		}
		return nil
	}
	return walk()
}
