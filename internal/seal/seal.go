// Package seal is the sealed-checks subsystem's crypto seam
// (plans/os-3128535a.md; SEED-NEXT.md Part II §7; conformance III.F
// sealed rows; spec/sealed-checks.md is normative): the salted
// commitment over the canonical sealed envelope, encryption to the
// current verifier keyring, and the recipient-tag scan the audit uses.
// The commitment is what the ledger records; the ciphertext is mutable
// custody in the artifact store's sealed bucket. Recipients are the
// verdict-granted ed25519 keys wrapped as ssh-ed25519 age recipients
// (agessh), so "recipients = the verifier keyring" holds with no new
// key material enrolled; the same keys decrypt as identities. The
// cross-protocol use (one ed25519 key signs verdicts and unwraps
// seals) is the documented v0 trade; dedicated X25519 enrollment is
// the named successor if the boundary ever needs it.
package seal

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"github.com/gowebpki/jcs"
	"golang.org/x/crypto/ssh"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
)

// Envelope is the sealed plaintext: the salt lives only here, inside
// the ciphertext, so the commitment is verifiable exactly by the
// parties who can decrypt (publishing the salt would invite dictionary
// attacks on low-entropy check bodies).
type Envelope struct {
	Salt   string   `json:"salt"`
	Checks []string `json:"checks"`
}

// NewEnvelope wraps the checks with a fresh 32-byte salt. An empty
// check list refuses: an empty seal would mark a contract sealed while
// running zero secret checks, a vacuous pass (review finding on the
// 6.3 plan).
func NewEnvelope(checks []string) (*Envelope, error) {
	if len(checks) == 0 {
		return nil, errors.New("a sealed envelope needs at least one check — an empty seal would pass vacuously")
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return &Envelope{Salt: hex.EncodeToString(salt), Checks: checks}, nil
}

// Canonical returns the envelope's RFC 8785 (JCS) bytes: the exact
// bytes the commitment hashes and the ciphertext protects.
func (e *Envelope) Canonical() ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

// Commitment returns the SHA-256 hex of the canonical bytes: the value
// check.sealed records and the sealed bucket is keyed by.
func (e *Envelope) Commitment() (string, error) {
	b, err := e.Canonical()
	if err != nil {
		return "", err
	}
	return artifact.Digest(b), nil
}

// saltRE pins the salt's wire form: exactly the 32 random bytes
// NewEnvelope mints, hex-encoded. A raw-crafted envelope with a
// degenerate salt would quietly surrender the commitment's dictionary
// resistance (review finding on the task PR), so every unseal
// validates the shape, not just presence.
var saltRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseEnvelope reads decrypted plaintext back into an envelope and
// re-derives its commitment for verification. A zero-check envelope is
// refused here too: creation never writes one, so one that decrypts is
// raw-crafted, and it must not pass vacuously.
func ParseEnvelope(plaintext []byte) (*Envelope, string, error) {
	var e Envelope
	dec := json.NewDecoder(bytes.NewReader(plaintext))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return nil, "", fmt.Errorf("sealed plaintext is not the envelope {salt, checks}: %v", err)
	}
	if !saltRE.MatchString(e.Salt) {
		return nil, "", errors.New("sealed envelope's salt is not 32 bytes of lowercase hex — a degenerate salt surrenders the commitment's dictionary resistance")
	}
	if len(e.Checks) == 0 {
		return nil, "", errors.New("sealed envelope carries zero checks — an empty seal must not pass vacuously")
	}
	commitment, err := e.Commitment()
	if err != nil {
		return nil, "", err
	}
	return &e, commitment, nil
}

// Encrypt seals the plaintext to every recipient key: the current
// verifier keyring, as ed25519 public keys. Deterministic recipient
// order (the caller sorts, keyring.Granted already does).
func Encrypt(plaintext []byte, recipients []ed25519.PublicKey) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, errors.New("no recipients: the verifier keyring holds no verdict-granted key to encrypt to")
	}
	rs := make([]age.Recipient, 0, len(recipients))
	for _, pub := range recipients {
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return nil, err
		}
		r, err := agessh.NewEd25519Recipient(sshPub)
		if err != nil {
			return nil, err
		}
		rs = append(rs, r)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rs...)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// NotRecipientError says the identity cannot unwrap the ciphertext:
// the key is not among the current recipients, the rotation-lag case.
type NotRecipientError struct{ Err error }

func (e *NotRecipientError) Error() string {
	return fmt.Sprintf("this key cannot unseal the ciphertext — it is not among the seal's recipients (a rotated keyring needs seal rotate): %v", e.Err)
}

// Decrypt opens the ciphertext with one verifier identity.
func Decrypt(ciphertext []byte, identity ed25519.PrivateKey) ([]byte, error) {
	id, err := agessh.NewEd25519Identity(identity)
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), id)
	if err != nil {
		var nie *age.NoIdentityMatchError
		if errors.As(err, &nie) {
			return nil, &NotRecipientError{Err: err}
		}
		return nil, err
	}
	return io.ReadAll(r)
}

// Tag returns the ssh-ed25519 stanza tag agessh derives for the key:
// base64 (raw std) of the first four SHA-256 bytes of the ssh wire
// form. The audit compares header tags against keyring-derived tags,
// so recipients are checkable without any secret.
func Tag(pub ed25519.PublicKey) (string, error) {
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(sshPub.Marshal())
	return base64.RawStdEncoding.EncodeToString(sum[:4]), nil
}

// RecipientTags scans the age v1 header's ssh-ed25519 recipient
// stanzas and returns their tags, sorted. It reads only the documented
// stanza lines (up to the header's "---" MAC line); payloads stay
// opaque. Wrapped stanza-body lines are base64 and can never start
// with "->".
func RecipientTags(ciphertext []byte) ([]string, error) {
	sc := bufio.NewScanner(bytes.NewReader(ciphertext))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() || sc.Text() != "age-encryption.org/v1" {
		return nil, errors.New("not an age v1 ciphertext")
	}
	var tags []string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "---") {
			sort.Strings(tags)
			return tags, nil
		}
		if strings.HasPrefix(line, "-> ssh-ed25519 ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				tags = append(tags, fields[2])
			}
		}
	}
	return nil, errors.New("age header ends without its MAC line")
}
