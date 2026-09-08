// Key handling: Ed25519 identities with OpenSSH acceptance at key load only
// (spec/protocol.md "Algorithms"). The OpenSSH display fingerprint
// (SHA256:<base64>) never appears on the wire; the protocol fingerprint is
// the lowercase-hex SHA-256 of the raw 32-byte public key.
package event

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// ErrEncryptedKey names the refusal for passphrase-protected private keys:
// v0 loads unencrypted keys only.
var ErrEncryptedKey = errors.New("private key is passphrase-protected — decrypt it first (v0 loads unencrypted ed25519 keys only)")

// Fingerprint is the protocol actor fingerprint: lowercase-hex SHA-256 of
// the raw 32-byte Ed25519 public key.
func Fingerprint(pub ed25519.PublicKey) (string, error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", fmt.Errorf("not an ed25519 public key (%d bytes)", len(pub))
	}
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:]), nil
}

// ParsePublicKey accepts an OpenSSH ed25519 public key (authorized_keys
// form) and returns the raw key.
func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	parsed, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("not an OpenSSH public key: %w", err)
	}
	ck, ok := parsed.(ssh.CryptoPublicKey)
	if !ok {
		return nil, fmt.Errorf("unsupported key container %s", parsed.Type())
	}
	pub, ok := ck.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key type %s is not ed25519", parsed.Type())
	}
	return pub, nil
}

// ParsePrivateKey accepts an unencrypted OpenSSH ed25519 private key and
// returns the raw key. Encrypted keys refuse with ErrEncryptedKey.
func ParsePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	raw, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, ErrEncryptedKey
		}
		return nil, fmt.Errorf("not an OpenSSH private key: %w", err)
	}
	switch k := raw.(type) {
	case ed25519.PrivateKey:
		return k, nil
	case *ed25519.PrivateKey:
		return *k, nil
	default:
		return nil, fmt.Errorf("private key type %T is not ed25519", raw)
	}
}
