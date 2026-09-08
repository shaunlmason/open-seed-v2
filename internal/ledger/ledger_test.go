package ledger

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

func fixtureKey(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func fingerprintOf(t testing.TB, priv ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func fixtureResolver(t testing.TB, privs ...ed25519.PrivateKey) Resolver {
	t.Helper()
	ring := map[string]ed25519.PublicKey{}
	for _, p := range privs {
		ring[fingerprintOf(t, p)] = p.Public().(ed25519.PublicKey)
	}
	return func(fp string) (ed25519.PublicKey, bool) {
		pub, ok := ring[fp]
		return pub, ok
	}
}

// signedRecord builds a record for the fixture actor with the given prev.
func signedRecord(t testing.TB, priv ed25519.PrivateKey, n int, ts, prev string) *event.Record {
	t.Helper()
	e := event.Event{
		V:       "seed/0",
		TS:      ts,
		Actor:   fingerprintOf(t, priv),
		Verb:    "message.sent",
		Subject: "c-0001",
		Payload: json.RawMessage(fmt.Sprintf(`{"n": %d}`, n)),
		Prev:    prev,
	}
	rec, err := event.Sign(e, priv)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func clockAt(day string) func() time.Time {
	ts, err := time.Parse(time.RFC3339, day)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return ts }
}

// conformance: III.A — ordering authority is admitted ancestry; positions
// are derived from the stream, never asserted by a writer.
func TestAppendTipAndDerivedPositions(t *testing.T) {
	priv := fixtureKey(t, 1)
	resolve := fixtureResolver(t, priv)
	s, err := Open(t.TempDir(), WithClock(clockAt("2026-09-01T10:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	tip := event.EmptyHash
	for i := 0; i < 3; i++ {
		rec := signedRecord(t, priv, i, "2026-09-01T10:00:00Z", tip)
		pos, err := s.Append(rec, resolve)
		if err != nil {
			t.Fatal(err)
		}
		if pos != i {
			t.Fatalf("append %d returned position %d", i, pos)
		}
		tip, err = rec.Event.Hash()
		if err != nil {
			t.Fatal(err)
		}
	}
	gotTip, count, err := s.Tip()
	if err != nil || gotTip != tip || count != 3 {
		t.Fatalf("Tip() = %s/%d/%v, want %s/3", gotTip, count, err, tip)
	}
	head, ok, err := s.ReadHead()
	if err != nil || !ok || head.Tip != tip || head.Count != 3 {
		t.Fatalf("HEAD = %+v ok=%v err=%v, want tip %s count 3", head, ok, err, tip)
	}
	rep, err := s.VerifyFromGenesis(resolve)
	if err != nil || rep.Count != 3 || rep.Tip != tip {
		t.Fatalf("verify = %+v, %v", rep, err)
	}
}

func TestAppendRefusals(t *testing.T) {
	priv := fixtureKey(t, 1)
	stranger := fixtureKey(t, 9)
	resolve := fixtureResolver(t, priv)
	s, err := Open(t.TempDir(), WithClock(clockAt("2026-09-01T10:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(nil, resolve); !errors.Is(err, ErrEmptyRecord) {
		t.Fatalf("nil record: %v", err)
	}
	unknown := signedRecord(t, stranger, 0, "2026-09-01T10:00:00Z", event.EmptyHash)
	if _, err := s.Append(unknown, resolve); !errors.Is(err, ErrUnknownActor) {
		t.Fatalf("unknown actor must refuse, got %v", err)
	}
	tampered := signedRecord(t, priv, 0, "2026-09-01T10:00:00Z", event.EmptyHash)
	tampered.Sig = strings.Repeat("00", 64)
	if _, err := s.Append(tampered, resolve); !errors.Is(err, event.ErrBadSignature) {
		t.Fatalf("bad signature must refuse, got %v", err)
	}
	good := signedRecord(t, priv, 0, "2026-09-01T10:00:00Z", event.EmptyHash)
	if _, err := s.Append(good, resolve); err != nil {
		t.Fatal(err)
	}
	stale := signedRecord(t, priv, 1, "2026-09-01T10:00:00Z", event.EmptyHash)
	if _, err := s.Append(stale, resolve); !errors.Is(err, ErrWrongPrev) {
		t.Fatalf("stale prev must refuse with ErrWrongPrev, got %v", err)
	}
}

func TestUTCDayBoundarySplitsSegments(t *testing.T) {
	priv := fixtureKey(t, 1)
	resolve := fixtureResolver(t, priv)
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	days := []string{"2026-09-01T23:59:00Z", "2026-09-01T23:59:30Z", "2026-09-02T00:00:30Z"}
	tip := event.EmptyHash
	for i, day := range days {
		s.now = clockAt(day)
		rec := signedRecord(t, priv, i, day, tip)
		if _, err := s.Append(rec, resolve); err != nil {
			t.Fatal(err)
		}
		tip, _ = rec.Event.Hash()
	}
	names, err := s.segmentNames()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-09-01.jsonl", "2026-09-02.jsonl"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("segments = %v, want %v", names, want)
	}
	if rep, err := s.VerifyFromGenesis(resolve); err != nil || rep.Count != 3 {
		t.Fatalf("multi-day chain must verify: %+v %v", rep, err)
	}
}

// conformance: III.A groundwork — segment names never regress; a clock
// rollback cannot reorder the read stream (plans/os-ead12024.md step 1).
func TestClockRegressionStaysInNewestSegment(t *testing.T) {
	priv := fixtureKey(t, 1)
	resolve := fixtureResolver(t, priv)
	s, err := Open(t.TempDir(), WithClock(clockAt("2026-09-02T01:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	first := signedRecord(t, priv, 0, "2026-09-02T01:00:00Z", event.EmptyHash)
	if _, err := s.Append(first, resolve); err != nil {
		t.Fatal(err)
	}
	s.now = clockAt("2026-09-01T12:00:00Z") // clock moved backward a day
	tip, _ := first.Event.Hash()
	second := signedRecord(t, priv, 1, "2026-09-01T12:00:00Z", tip)
	if _, err := s.Append(second, resolve); err != nil {
		t.Fatal(err)
	}
	names, err := s.segmentNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "2026-09-02.jsonl" {
		t.Fatalf("regressed-clock append must stay in newest segment, got %v", names)
	}
	if rep, err := s.VerifyFromGenesis(resolve); err != nil || rep.Count != 2 {
		t.Fatalf("chain must stay linear under clock regression: %+v %v", rep, err)
	}
}

// conformance: III.F groundwork — a crash between the durable segment line
// and the HEAD rename heals on the next use; no fork, no lost update
// (plans/os-ead12024.md step 2).
func TestCrashWindowBetweenLineAndHeadHeals(t *testing.T) {
	priv := fixtureKey(t, 1)
	resolve := fixtureResolver(t, priv)
	dir := t.TempDir()
	s, err := Open(dir, WithClock(clockAt("2026-09-01T10:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	a := signedRecord(t, priv, 0, "2026-09-01T10:00:00Z", event.EmptyHash)
	if _, err := s.Append(a, resolve); err != nil {
		t.Fatal(err)
	}
	tipA, _ := a.Event.Hash()

	// Simulate the crash: the next record's line becomes durable, but the
	// process dies before HEAD is renamed.
	b := signedRecord(t, priv, 1, "2026-09-01T10:01:00Z", tipA)
	line, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	seg := filepath.Join(dir, "segments", "2026-09-01.jsonl")
	f, err := os.OpenFile(seg, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(line); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// The stale HEAD is visible and distinct in verification.
	if _, err := s.VerifyFromGenesis(resolve); err == nil {
		t.Fatal("stale HEAD must be reported")
	} else {
		var fail *Failure
		if !errors.As(err, &fail) || fail.Reason != ReasonHeadBehind {
			t.Fatalf("want %s, got %v", ReasonHeadBehind, err)
		}
	}
	// Tip reconciles from the stream, not the stale cache.
	tipB, _ := b.Event.Hash()
	if tip, count, err := s.Tip(); err != nil || tip != tipB || count != 2 {
		t.Fatalf("Tip must reconcile past stale HEAD: %s/%d/%v want %s/2", tip, count, err, tipB)
	}
	// The next append extends the healed stream and rewrites HEAD.
	c := signedRecord(t, priv, 2, "2026-09-01T10:02:00Z", tipB)
	pos, err := s.Append(c, resolve)
	if err != nil || pos != 2 {
		t.Fatalf("append after crash window: pos=%d err=%v", pos, err)
	}
	if rep, err := s.VerifyFromGenesis(resolve); err != nil || rep.Count != 3 {
		t.Fatalf("healed chain must verify: %+v %v", rep, err)
	}
}

func TestHeadAtomicityMechanics(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.writeHead(Head{Tip: "abc", Count: 1, Segment: "x.jsonl"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "HEAD.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp HEAD must not survive the rename")
	}
	h, ok, err := s.ReadHead()
	if err != nil || !ok || h.Tip != "abc" || h.Count != 1 {
		t.Fatalf("ReadHead = %+v %v %v", h, ok, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadHead(); err == nil {
		t.Fatal("garbage HEAD must refuse to parse")
	}
}
