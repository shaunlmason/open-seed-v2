package ledger

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

func parseSingleRecord(t *testing.T, s string) *event.Record {
	t.Helper()
	rec, err := event.ParseRecord([]byte(strings.TrimSpace(s)))
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// resign keeps the event but signs it with a different key: a valid-shaped
// signature from the wrong signer.
func resign(rec *event.Record, key ed25519.PrivateKey) (string, error) {
	forged, err := event.Sign(rec.Event, key)
	if err != nil {
		return "", err
	}
	line, err := forged.Marshal()
	if err != nil {
		return "", err
	}
	return string(line), nil
}

// copyFixture clones testdata/valid-chain into a temp store dir.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dst, "segments"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join("testdata", "valid-chain")
	entries, err := os.ReadDir(filepath.Join(src, "segments"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, "segments", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, "segments", e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	head, err := os.ReadFile(filepath.Join(src, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "HEAD"), head, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func segmentPath(t *testing.T, dir, name string) string {
	t.Helper()
	return filepath.Join(dir, "segments", name)
}

func rewriteFile(t *testing.T, path string, fn func(string) string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fn(string(b))), 0o644); err != nil {
		t.Fatal(err)
	}
}

// conformance: III.A — any mutation within retained history (reorder,
// rewrite, forgery, interior deletion) is detected by chain verification,
// each with a distinct reason naming the failing position.
func TestCommittedFixtureVerifies(t *testing.T) {
	dir := copyFixture(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv := fixtureKey(t, 1)
	rep, err := s.VerifyFromGenesis(fixtureResolver(t, priv))
	if err != nil {
		t.Fatalf("committed fixture must verify: %v", err)
	}
	if rep.Count != 4 {
		t.Fatalf("fixture has %d events, want 4", rep.Count)
	}
}

func TestCorruptionsAreDetectedDistinctly(t *testing.T) {
	priv := fixtureKey(t, 1)
	stranger := fixtureKey(t, 9)

	cases := map[string]struct {
		corrupt  func(t *testing.T, dir string)
		reason   string
		position int
	}{
		"reorder": {
			corrupt: func(t *testing.T, dir string) {
				rewriteFile(t, segmentPath(t, dir, "2026-09-01.jsonl"), func(s string) string {
					lines := strings.Split(strings.TrimSpace(s), "\n")
					if len(lines) != 2 {
						t.Fatalf("day-1 segment has %d lines, want 2", len(lines))
					}
					return lines[1] + "\n" + lines[0] + "\n"
				})
			},
			reason: ReasonBadPrev, position: 0,
		},
		"rewrite-payload": {
			corrupt: func(t *testing.T, dir string) {
				rewriteFile(t, segmentPath(t, dir, "2026-09-01.jsonl"), func(s string) string {
					lines := strings.Split(strings.TrimSpace(s), "\n")
					replaced := strings.Replace(lines[1], `"n":1`, `"n":7`, 1)
					if replaced == lines[1] {
						t.Fatal("payload rewrite did not change the line — fixture drifted")
					}
					lines[1] = replaced
					return strings.Join(lines, "\n") + "\n"
				})
			},
			reason: ReasonBadSignature, position: 1,
		},
		"forged-signature": {
			corrupt: func(t *testing.T, dir string) {
				// Re-sign the last event with a different key: the shape is
				// valid, the signer is not the actor the event names.
				dst := segmentPath(t, dir, "2026-09-03.jsonl")
				rewriteFile(t, dst, func(s string) string {
					rec := parseSingleRecord(t, s)
					forged, err := resign(rec, stranger)
					if err != nil {
						t.Fatal(err)
					}
					return forged
				})
			},
			reason: ReasonBadSignature, position: 3,
		},
		"bad-prev": {
			corrupt: func(t *testing.T, dir string) {
				rewriteFile(t, segmentPath(t, dir, "2026-09-02.jsonl"), func(s string) string {
					rec := parseSingleRecord(t, s)
					rec.Event.Prev = strings.Repeat("0", 64)
					line, err := rec.Marshal()
					if err != nil {
						t.Fatal(err)
					}
					return string(line)
				})
			},
			reason: ReasonBadPrev, position: 2,
		},
		"interior-truncation": {
			corrupt: func(t *testing.T, dir string) {
				if err := os.Remove(segmentPath(t, dir, "2026-09-02.jsonl")); err != nil {
					t.Fatal(err)
				}
			},
			reason: ReasonBadPrev, position: 2,
		},
		"tail-truncation": {
			corrupt: func(t *testing.T, dir string) {
				if err := os.Remove(segmentPath(t, dir, "2026-09-03.jsonl")); err != nil {
					t.Fatal(err)
				}
			},
			reason: ReasonHeadWrong, position: 3,
		},
		"head-mismatch": {
			corrupt: func(t *testing.T, dir string) {
				rewriteFile(t, filepath.Join(dir, "HEAD"), func(s string) string {
					return strings.Replace(s, `"tip":"`, `"tip":"beef`, 1)
				})
			},
			reason: ReasonHeadWrong, position: 4,
		},
		"payload-not-object": {
			corrupt: func(t *testing.T, dir string) {
				rewriteFile(t, segmentPath(t, dir, "2026-09-02.jsonl"), func(s string) string {
					out := strings.Replace(s, `"payload":{"n":2}`, `"payload":"a body"`, 1)
					if out == s {
						t.Fatal("payload rewrite did not apply — fixture drifted")
					}
					return out
				})
			},
			reason: ReasonBadPayload, position: 2,
		},
		"garbage-line": {
			corrupt: func(t *testing.T, dir string) {
				rewriteFile(t, segmentPath(t, dir, "2026-09-03.jsonl"), func(s string) string {
					return "not a record\n"
				})
			},
			reason: ReasonBadParse, position: 3,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := copyFixture(t)
			tc.corrupt(t, dir)
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.VerifyFromGenesis(fixtureResolver(t, priv))
			var fail *Failure
			if !errors.As(err, &fail) {
				t.Fatalf("corruption %q must fail verification, got %v", name, err)
			}
			if fail.Reason != tc.reason || fail.Position != tc.position {
				t.Fatalf("corruption %q: got %s@%d, want %s@%d (%s)",
					name, fail.Reason, fail.Position, tc.reason, tc.position, fail.Detail)
			}
		})
	}
}
