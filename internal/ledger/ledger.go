// Package ledger implements the append-only chain store over
// internal/event (charter Part II section 1; plans/os-ead12024.md).
// Layout is the build-plan fixed default: JSONL segments, one file per UTC
// day under segments/, and a HEAD record carrying the tip hash. The
// segment stream is authoritative and HEAD is a derived cache of it:
// Append reconciles the true tip from the stream before checking prev, so
// a crash between the durable segment line and the HEAD rename heals on
// the next use instead of forking the chain.
package ledger

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

// Resolver maps an actor fingerprint to its public key. The keyring
// projection supplies this from Phase 3 on; genesis bootstrap and tests
// supply fixture resolvers.
type Resolver func(fingerprint string) (ed25519.PublicKey, bool)

// Typed refusals, mapped to envelope codes by later phases.
var (
	ErrUnknownActor = errors.New("actor fingerprint not in the keyring")
	ErrWrongPrev    = errors.New("prev does not cite the current tip")
	ErrEmptyRecord  = errors.New("nil record")
	ErrNotEmpty     = errors.New("ledger is not empty — genesis writes into empty stores only")
)

const (
	segmentsDir = "segments"
	headFile    = "HEAD"
)

// Head is the derived cache of the segment stream's end.
type Head struct {
	Tip     string `json:"tip"`
	Count   int    `json:"count"`
	Segment string `json:"segment"`
}

// Store is a ledger checkout rooted at a directory.
type Store struct {
	dir string
	now func() time.Time
}

// Option configures a Store.
type Option func(*Store)

// WithClock injects the append clock (tests; the default is time.Now).
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// Open prepares a store rooted at dir, creating the layout if absent.
func Open(dir string, opts ...Option) (*Store, error) {
	s := &Store{dir: dir, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	if err := os.MkdirAll(filepath.Join(dir, segmentsDir), 0o755); err != nil {
		return nil, err
	}
	s.healHead()
	return s, nil
}

// OpenReadOnly prepares a store view that never writes: no layout
// creation, no head healing. Commands that promise not to change the
// ledger by inspecting it (seed ledger show) use this path, so a missing
// or behind HEAD is reported as found rather than repaired. The directory
// must already hold a ledger layout.
func OpenReadOnly(dir string) (*Store, error) {
	if _, err := os.Stat(filepath.Join(dir, segmentsDir)); err != nil {
		return nil, err
	}
	return &Store{dir: dir, now: time.Now}, nil
}

// healHead reconciles the crash window at open: when the segment stream
// extends past a missing or consistently-behind HEAD (the durable line
// landed, the rename did not), the cache is rewritten from the stream,
// which is the truth. Best-effort by design: an unparseable stream or a
// HEAD that contradicts the stream is left untouched for verification to
// report; healing never papers over corruption.
func (s *Store) healHead() {
	tip := event.EmptyHash
	count := 0
	lastSegment := ""
	claimed := map[int]string{0: event.EmptyHash}
	err := s.scan(func(pos int, segment string, line []byte) error {
		rec, perr := event.ParseRecord(line)
		if perr != nil {
			return perr
		}
		h, herr := rec.Event.Hash()
		if herr != nil {
			return herr
		}
		tip, count, lastSegment = h, pos+1, segment
		claimed[count] = h
		return nil
	})
	if err != nil || count == 0 {
		return
	}
	head, exists, err := s.ReadHead()
	if err != nil {
		return
	}
	if exists {
		if head.Count >= count {
			return
		}
		if want, ok := claimed[head.Count]; !ok || head.Tip != want {
			return
		}
	}
	_ = s.writeHead(Head{Tip: tip, Count: count, Segment: lastSegment})
}

// segmentNames returns the segment file names in read order.
func (s *Store) segmentNames() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, segmentsDir))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// scan streams every record in order, calling fn with the zero-based
// position, the segment name, and the raw line. fn returning an error
// stops the scan.
func (s *Store) scan(fn func(pos int, segment string, line []byte) error) error {
	names, err := s.segmentNames()
	if err != nil {
		return err
	}
	pos := 0
	for _, name := range names {
		f, err := os.Open(filepath.Join(s.dir, segmentsDir, name))
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(f)
		sc.Split(scanLinesKeepCR)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			// Blank lines and stray spaces are tolerated; a carriage
			// return is not trimmed, so a CRLF-mangled segment is
			// refused by the parser rather than normalized here.
			line := strings.Trim(sc.Text(), " \t")
			if line == "" {
				continue
			}
			if err := fn(pos, name, []byte(line)); err != nil {
				f.Close()
				return err
			}
			pos++
		}
		if err := sc.Err(); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

// reconcile derives the true tip from the segment stream: the hash of the
// last record, the total count, and the last segment name. An empty stream
// reports the empty hash at count zero. HEAD is never consulted for the
// answer, only healed from it.
func (s *Store) reconcile() (tip string, count int, lastSegment string, err error) {
	tip = event.EmptyHash
	err = s.scan(func(pos int, segment string, line []byte) error {
		rec, perr := event.ParseRecord(line)
		if perr != nil {
			return fmt.Errorf("position %d (%s): %w", pos, segment, perr)
		}
		h, herr := rec.Event.Hash()
		if herr != nil {
			return fmt.Errorf("position %d (%s): %w", pos, segment, herr)
		}
		tip, count, lastSegment = h, pos+1, segment
		return nil
	})
	return tip, count, lastSegment, err
}

// Tip reports the reconciled tip hash and count.
func (s *Store) Tip() (string, int, error) {
	tip, count, _, err := s.reconcile()
	return tip, count, err
}

// segmentForAppend picks the file a new record lands in: the append day's
// name, unless an existing segment sorts later (a clock regression), in
// which case the newest existing segment keeps growing so segment names
// never regress and read order stays linear (plans/os-ead12024.md step 1).
func (s *Store) segmentForAppend(newest string) string {
	today := s.now().UTC().Format("2006-01-02") + ".jsonl"
	if newest != "" && newest > today {
		return newest
	}
	return today
}

// Append verifies the record against the resolver, checks prev against the
// reconciled tip, writes the line durably, then rewrites HEAD atomically.
// It returns the record's zero-based position.
func (s *Store) Append(rec *event.Record, resolve Resolver) (int, error) {
	if rec == nil {
		return 0, ErrEmptyRecord
	}
	pub, ok := resolve(rec.Event.Actor)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrUnknownActor, rec.Event.Actor)
	}
	if err := rec.Verify(pub); err != nil {
		return 0, err
	}
	tip, count, lastSegment, err := s.reconcile()
	if err != nil {
		return 0, err
	}
	if rec.Event.Prev != tip {
		return 0, fmt.Errorf("%w: prev %.12s, tip %.12s", ErrWrongPrev, rec.Event.Prev, tip)
	}
	line, err := rec.Marshal()
	if err != nil {
		return 0, err
	}
	segment := s.segmentForAppend(lastSegment)
	f, err := os.OpenFile(filepath.Join(s.dir, segmentsDir, segment), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	newTip, err := rec.Event.Hash()
	if err != nil {
		return 0, err
	}
	if err := s.writeHead(Head{Tip: newTip, Count: count + 1, Segment: segment}); err != nil {
		return 0, err
	}
	return count, nil
}

// writeHead rewrites HEAD atomically (temp + rename). The temp name
// is unique per writer (plans/os-c6fb95ee.md): writeHead runs from
// two processes at once by design — the appender, and any poll-only
// reader whose Open lands in the mid-append window and fires the
// HEAD repair — and a shared temp path let the reader's rename
// consume the appender's temp, failing its rename with ENOENT.
// Interleaving stays safe under unique names: both racers write
// forward-consistent heads, each rename is atomic over its own
// file, and a momentarily stale HEAD self-heals on the next Open.
// AppendAll appends a chain of records in one pass: each verified
// against the resolver and chained to the one before it exactly as
// Append checks, the store reconciled once rather than per record, one
// segment written and synced, the head advanced once. The predecessor
// import (internal/importer) writes a thousand admitted records and
// Append's per-call rescan made that quadratic; nothing here is a
// write that Append would refuse. It returns the position of the first
// record appended.
func (s *Store) AppendAll(records []*event.Record, resolve Resolver) (int, error) {
	if len(records) == 0 {
		return 0, ErrEmptyRecord
	}
	tip, count, lastSegment, err := s.reconcile()
	if err != nil {
		return 0, err
	}
	first := count
	lines := make([][]byte, 0, len(records))
	for i, rec := range records {
		if rec == nil {
			return 0, ErrEmptyRecord
		}
		pub, ok := resolve(rec.Event.Actor)
		if !ok {
			return 0, fmt.Errorf("record %d: %w: %s", i, ErrUnknownActor, rec.Event.Actor)
		}
		if err := rec.Verify(pub); err != nil {
			return 0, fmt.Errorf("record %d: %w", i, err)
		}
		if rec.Event.Prev != tip {
			return 0, fmt.Errorf("record %d: %w: prev %.12s, tip %.12s", i, ErrWrongPrev, rec.Event.Prev, tip)
		}
		line, err := rec.Marshal()
		if err != nil {
			return 0, err
		}
		if tip, err = rec.Event.Hash(); err != nil {
			return 0, err
		}
		lines = append(lines, line)
	}
	segment := s.segmentForAppend(lastSegment)
	f, err := os.OpenFile(filepath.Join(s.dir, segmentsDir, segment), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	for _, line := range lines {
		if _, err := f.Write(line); err != nil {
			f.Close()
			return 0, err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	if err := s.writeHead(Head{Tip: tip, Count: count + len(records), Segment: segment}); err != nil {
		return 0, err
	}
	return first, nil
}

func (s *Store) writeHead(h Head) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, headFile+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// CreateTemp opens its file at 0600; restore the established
	// HEAD mode before the rename (review finding on the task PR),
	// so a shared directory's poll-only readers keep read access:
	// an existing HEAD keeps whatever mode the operator set, and a
	// first write gets 0644, the mode the segment writes use and
	// the pre-fix WriteFile used here.
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(filepath.Join(s.dir, headFile)); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := renameRetrying(tmp, filepath.Join(s.dir, headFile)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ReadHead returns the cached HEAD record; ok is false when none exists.
func (s *Store) ReadHead() (Head, bool, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, headFile))
	if errors.Is(err, os.ErrNotExist) {
		return Head{}, false, nil
	}
	if err != nil {
		return Head{}, false, err
	}
	var h Head
	if err := json.Unmarshal(b, &h); err != nil {
		return Head{}, false, fmt.Errorf("HEAD does not parse: %w", err)
	}
	return h, true, nil
}

// Records streams every parsed record in order. It is the read surface the
// CLI verbs and the genesis bootstrap consume; parse failures surface with
// their position.
func (s *Store) Records(fn func(pos int, rec *event.Record) error) error {
	return s.scan(func(pos int, segment string, line []byte) error {
		rec, err := event.ParseRecord(line)
		if err != nil {
			return fmt.Errorf("position %d (%s): %w", pos, segment, err)
		}
		return fn(pos, rec)
	})
}

// scanLinesKeepCR splits on LF alone and keeps any carriage return in
// the line, so a CRLF-mangled segment reaches the record parser and is
// refused there (spec/platform.md): bufio.ScanLines would strip
// the CR and silently normalize the bytes a signature covers.
func scanLinesKeepCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// renameRetrying replaces dst with src, retrying briefly when the
// platform refuses to replace a file a concurrent reader holds open
// (Windows reports a sharing violation where POSIX renames over the
// open file); the last error stands after the retries.
func renameRetrying(src, dst string) error {
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}
