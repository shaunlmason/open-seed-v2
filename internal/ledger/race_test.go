package ledger

// The writeHead contention drill (plans/os-c6fb95ee.md): poll-only
// readers Open the store while a supervisor appends — the designed
// multi-process posture — and every Open runs healHead, which
// rewrites a lagging HEAD when it lands in the mid-append window.
// With the shared temp path the reader's rename consumed the
// appender's temp and the append failed with ENOENT
// (TestGracefulPreemptionDrill's CI flake); per-writer unique temps
// make both renames atomic over their own files. The drill hammers
// the mechanism directly: the drill-level window is too narrow to
// hit reliably on fast disks, the store-level one is not.

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

func TestConcurrentAppendAndOpenRepair(t *testing.T) {
	priv := fixtureKey(t, 1)
	resolve := fixtureResolver(t, priv)
	dir := t.TempDir()
	s, err := Open(dir, WithClock(clockAt("2026-09-01T10:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// The reader half of the race: each Open runs healHead,
			// which rewrites HEAD when the segments are ahead of it.
			_, _ = Open(dir)
		}
	}()
	tip := event.EmptyHash
	const n = 400
	for i := 0; i < n; i++ {
		rec := signedRecord(t, priv, i, "2026-09-01T10:00:00Z", tip)
		if _, err := s.Append(rec, resolve); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("append %d under a concurrent reader: %v", i, err)
		}
		if tip, err = rec.Event.Hash(); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	// A reader descheduled between its behind-HEAD decision and its
	// rename can legally land a stale but stream-consistent HEAD as
	// the last write; the fix's guarantee is that the next Open
	// heals it, not that the last write wins (review finding on the
	// task PR). Exercise exactly that guarantee: heal, then verify.
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if rep, err := s.VerifyFromGenesis(resolve); err != nil || rep.Count != n || rep.Tip != tip {
		t.Fatalf("the chain verifies after the contention: %+v %v", rep, err)
	}
}

// The mode drill (review finding on the task PR): CreateTemp opens
// at 0600, and renaming that over HEAD would strip read access from
// poll-only readers running under another UID. writeHead restores
// the established mode: 0644 on first write, the operator's own
// mode thereafter.
func TestWriteHeadPreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows, so a preserved mode is not observable there (spec/platform.md)")
	}
	priv := fixtureKey(t, 1)
	resolve := fixtureResolver(t, priv)
	dir := t.TempDir()
	s, err := Open(dir, WithClock(clockAt("2026-09-01T10:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	rec := signedRecord(t, priv, 0, "2026-09-01T10:00:00Z", event.EmptyHash)
	if _, err := s.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(dir, "HEAD")
	if info, err := os.Stat(head); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("a fresh HEAD carries 0644: %v %v", info.Mode().Perm(), err)
	}
	if err := os.Chmod(head, 0o640); err != nil {
		t.Fatal(err)
	}
	tip, err := rec.Event.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rec = signedRecord(t, priv, 1, "2026-09-01T10:00:00Z", tip)
	if _, err := s.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(head); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("an established HEAD mode survives the rewrite: %v %v", info.Mode().Perm(), err)
	}
}
