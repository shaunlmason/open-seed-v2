// Package artifact is the minimal content-addressed artifact store
// (plans/os-f6d2c267.md; SEED-NEXT.md Part II §8): bytes keyed by the
// lowercase-hex SHA-256 of their content, rooted on the filesystem.
// The build plan's git-addressed refs/seed/artifacts push is deferred
// (the observation-channel precedent: simplest channel first); the
// deferral is recorded in docs/decisions.md. Receipts are the
// first tenant; sealed-check ciphertexts join in 6.3.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Store is a filesystem-rooted content-addressed store.
type Store struct{ root string }

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Open returns the store rooted at dir (created on first put).
func Open(dir string) *Store { return &Store{root: dir} }

// Digest returns the store's key for b: lowercase-hex SHA-256.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Store) path(digest string) string {
	return filepath.Join(s.root, "sha256", digest)
}

// Put stores b and returns its digest. A concurrent put of the same
// content is idempotent: the write lands in a unique temp file and
// renames into place, so rivals cannot interleave partial bytes.
func (s *Store) Put(b []byte) (string, error) {
	digest := Digest(b)
	return digest, s.put(digest, b)
}

// put writes b under a digest already computed.
func (s *Store) put(digest string, b []byte) error {
	dst := s.path(digest)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "put-*")
	if err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		// A platform that refuses to replace a file another writer
		// holds (Windows) reports the rival's win as an error; the
		// content is addressed by its digest, so a rival that landed
		// the same bytes is this put done. The rival's rename may
		// still be in flight when this one is refused, so the read is
		// retried briefly before the put fails.
		for attempt := 0; attempt < 50; attempt++ {
			if have, rerr := os.ReadFile(dst); rerr == nil && Digest(have) == digest {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return fmt.Errorf("artifact store: %w", err)
	}
	return nil
}

// PutVerified stores b under the digest the caller expected, refusing
// content that hashes to anything else: the verified-on-arrival put a
// fetch across an organization boundary uses (plans/os-40ed0ca0.md
// D3), so what the store holds under a name is what was named.
func (s *Store) PutVerified(digest string, b []byte) error {
	if !digestRE.MatchString(digest) {
		return fmt.Errorf("artifact store: %q is not a lowercase-hex sha256 digest", digest)
	}
	if got := Digest(b); got != digest {
		return fmt.Errorf("artifact store: content hashes to %s, not the %s it was fetched as — refused on arrival", got, digest)
	}
	return s.put(digest, b)
}

// Get returns the bytes stored under digest, recomputing and checking
// the digest on the way out: a store whose disk content no longer
// hashes to its name is corrupt, and corrupt content must never be
// returned as if addressed.
func (s *Store) Get(digest string) ([]byte, error) {
	if !digestRE.MatchString(digest) {
		return nil, fmt.Errorf("artifact store: %q is not a lowercase-hex sha256 digest", digest)
	}
	b, err := os.ReadFile(s.path(digest))
	if err != nil {
		return nil, fmt.Errorf("artifact store: %w", err)
	}
	if got := Digest(b); got != digest {
		return nil, fmt.Errorf("artifact store: content under %s hashes to %s — the store is corrupt at that address", digest, got)
	}
	return b, nil
}

// The sealed bucket (plans/os-3128535a.md; spec/sealed-checks.md)
// sits beside the content-addressed tree: mutable ciphertext under the
// immutable commitment that keys it. Rotation overwrites the file in
// place and touches no history; deleting one is the charter's erasure
// path, surfaced by the audit, never silence. Content is NOT
// digest-checked on the way out — the commitment verifies the
// decrypted plaintext, not the ciphertext, which rotation rewrites.

func (s *Store) sealedPath(commitment string) string {
	return filepath.Join(s.root, "sealed", commitment+".age")
}

// PutSealed stores (or replaces) the ciphertext for a commitment.
func (s *Store) PutSealed(commitment string, b []byte) error {
	if !digestRE.MatchString(commitment) {
		return fmt.Errorf("artifact store: %q is not a lowercase-hex sha256 commitment", commitment)
	}
	dst := s.sealedPath(commitment)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "seal-*")
	if err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	return nil
}

// GetSealed returns the ciphertext stored for a commitment.
func (s *Store) GetSealed(commitment string) ([]byte, error) {
	if !digestRE.MatchString(commitment) {
		return nil, fmt.Errorf("artifact store: %q is not a lowercase-hex sha256 commitment", commitment)
	}
	b, err := os.ReadFile(s.sealedPath(commitment))
	if err != nil {
		return nil, fmt.Errorf("artifact store: %w", err)
	}
	return b, nil
}

// Erase removes the bytes a digest keys, in whichever buckets hold
// them: the sealed ciphertext under the commitment, the content under
// the digest, the raw-export sidecar a shape digest names, or any of
// them. It returns the buckets it emptied ("sealed",
// "content"), empty when nothing was stored, which is not an error:
// the erasure record is the attribution (plans/os-db5cd353.md D5), and
// an erasure after the fact is still an erasure. This is the charter's
// erasure path made a verb rather than a file deletion: the chain
// references the bytes by digest and never carries them, so removing
// them never breaks verification, and the caller records the act
// before calling here so the absence is never silence.
func (s *Store) Erase(digest string) ([]string, error) {
	if !digestRE.MatchString(digest) {
		return nil, fmt.Errorf("artifact store: %q is not a lowercase-hex sha256 digest", digest)
	}
	var removed []string
	for _, b := range []struct{ name, path string }{
		{"sealed", s.sealedPath(digest)},
		{"content", s.path(digest)},
		{"trace_raw", s.tracePath(digest)},
	} {
		if _, err := os.Stat(b.path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("artifact store: %w", err)
		}
		if err := os.Remove(b.path); err != nil {
			return removed, fmt.Errorf("artifact store: %w", err)
		}
		removed = append(removed, b.name)
	}
	return removed, nil
}

// The trace bucket (plans/os-7fc2ca38.md D5; spec/verdicts.md
// "Trace-shaped evidence") sits beside the content-addressed tree as
// the sealed bucket does: under a shape digest, the digest of the raw
// export the shape was normalized from. The shape is content the
// receipt binds and check verifies; the raw export is bulk the
// verifier stored beside it for a reader, content-addressed on its
// own, reachable only through this pointer, and erasable on its own
// digest without touching the shape. Nothing on the ledger or in a
// receipt names the raw digest, because it never reproduces.

func (s *Store) tracePath(shape string) string {
	return filepath.Join(s.root, "traces", shape)
}

// PutTraceRaw records raw as the sidecar of shape: both are digests of
// content the caller already put.
func (s *Store) PutTraceRaw(shape, raw string) error {
	if !digestRE.MatchString(shape) || !digestRE.MatchString(raw) {
		return fmt.Errorf("artifact store: %q -> %q is not a pair of lowercase-hex sha256 digests", shape, raw)
	}
	dst := s.tracePath(shape)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "trace-*")
	if err != nil {
		return fmt.Errorf("artifact store: %w", err)
	}
	if _, err := tmp.WriteString(raw + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("artifact store: %w", err)
	}
	return nil
}

// TraceRaw returns the raw export's digest recorded as the sidecar of
// shape, or "" with no error when no sidecar was recorded (a shape
// checks the same either way). The pointer names content; whether the
// content is still held is Get's answer.
func (s *Store) TraceRaw(shape string) (string, error) {
	if !digestRE.MatchString(shape) {
		return "", fmt.Errorf("artifact store: %q is not a lowercase-hex sha256 digest", shape)
	}
	b, err := os.ReadFile(s.tracePath(shape))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("artifact store: %w", err)
	}
	raw := strings.TrimSpace(string(b))
	if !digestRE.MatchString(raw) {
		return "", fmt.Errorf("artifact store: the sidecar under %s names %q, not a digest", shape, raw)
	}
	return raw, nil
}
