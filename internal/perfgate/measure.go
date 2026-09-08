package perfgate

// The measurements (plans/os-7508ab9e.md D6): each metric against the
// representative history at the budget file's declared size, so a
// reading and its ceiling are about the same chain.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/history"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Measurer runs the benchmarks: the history at Contracts, Writers
// concurrent appenders in the storm, and the hook binary the storm's
// remote enforces with (built by the caller; empty means no hook).
type Measurer struct {
	Seed      int64
	Contracts int
	Writers   int
	HookBin   string
	Samples   int    // admission checks averaged; 0 means 20
	Keep      string // a directory the work dir is kept under; empty removes it (plans/os-5063e8ba.md D3)
}

// MetricRelinked is the storm's count of pushes a hook refused as
// bad_prev at or beyond the writer's own append and the loop re-linked
// (plans/os-5063e8ba.md D2): a count in the reading, never a budget,
// so Required does not name it.
const MetricRelinked = "relinked"

// Measure runs every metric once and returns the reading.
func (m Measurer) Measure() (Reading, error) {
	var work string
	if m.Keep != "" {
		if err := os.MkdirAll(m.Keep, 0o755); err != nil {
			return nil, err
		}
		w, err := os.MkdirTemp(m.Keep, "seed-perf-*")
		if err != nil {
			return nil, err
		}
		work = w
	} else {
		w, err := os.MkdirTemp("", "seed-perf-*")
		if err != nil {
			return nil, err
		}
		work = w
		defer os.RemoveAll(work)
	}
	ledgerDir := filepath.Join(work, "ledger")
	writers := m.Writers
	if writers <= 0 {
		writers = 1
	}
	// One enrolled actor per writer (plans/os-a00d3f34.md D1): the
	// storm's concurrent actors are keypairs, not one key used N times.
	res, err := history.Generate(history.Spec{Seed: m.Seed, Contracts: m.Contracts, Dir: ledgerDir, Writers: writers})
	if err != nil {
		return nil, fmt.Errorf("generating the history: %w", err)
	}
	r := Reading{}

	// Replay: one full verification from genesis.
	store, err := ledger.OpenReadOnly(ledgerDir)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	if _, err := store.VerifyFromGenesis(res.Resolve); err != nil {
		return nil, fmt.Errorf("the history must verify: %w", err)
	}
	r[MetricReplay] = ms(time.Since(start))

	// Admission latency: the context at the tip built once (the
	// cooperative client's prologue), then one admit.Check per sample
	// for a fresh draft, averaged.
	ctx, err := admit.ContextAt(store)
	if err != nil {
		return nil, err
	}
	samples := m.Samples
	if samples <= 0 {
		samples = 20
	}
	fp, _ := event.Fingerprint(res.Keys.Root.Public().(ed25519.PublicKey))
	var total time.Duration
	for i := 0; i < samples; i++ {
		rec, err := event.Sign(event.Event{V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fp, Verb: "intent.filed", Subject: fmt.Sprintf("p-%04d", i), Payload: json.RawMessage(`{"intent": "perf", "tier": "trivial", "budget": "small", "routing": "core"}`), Prev: ctx.Tip}, res.Keys.Root)
		if err != nil {
			return nil, err
		}
		t0 := time.Now()
		if err := admit.Check(ctx, rec); err != nil {
			return nil, fmt.Errorf("a fresh draft must admit at the tip: %w", err)
		}
		total += time.Since(t0)
	}
	r[MetricAdmission] = ms(total / time.Duration(samples))

	// Rebuild: every registered projection published from the chain.
	rebuilt, err := rebuildMS(ledgerDir, filepath.Join(work, "out"), res.Resolve)
	if err != nil {
		return nil, fmt.Errorf("rebuild: %w", err)
	}
	r[MetricRebuild] = rebuilt

	// Contention: Writers concurrent appenders against one bare
	// remote seeded with the history, the hook installed when given,
	// each landing one append through the optimistic loop; the wall
	// time and the attempts each landed append cost.
	wall, ratio, relinked, err := m.storm(work, ledgerDir, res)
	if err != nil {
		return nil, err
	}
	r[MetricContention] = ms(wall)
	r[MetricAttempts] = ratio
	r[MetricRelinked] = float64(relinked)
	return r, nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func (m Measurer) storm(work, ledgerDir string, res *history.Result) (time.Duration, float64, int64, error) {
	remote := filepath.Join(work, "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		return 0, 0, 0, fmt.Errorf("bare init: %v %s", err, out)
	}
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"}} {
		if out, err := exec.Command("git", "-C", remote, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			return 0, 0, 0, fmt.Errorf("hardening: %v %s", err, out)
		}
	}
	if m.HookBin != "" {
		shim := "#!/bin/sh\nexec " + m.HookBin + "\n"
		if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(shim), 0o755); err != nil {
			return 0, 0, 0, err
		}
	}
	// Seed the remote with the history in one push.
	seeder, err := gitref.NewClient(filepath.Join(work, "seeder"), remote, "refs/seed/ledger")
	if err != nil {
		return 0, 0, 0, err
	}
	if _, err := seeder.CommitAndPush(ledgerDir, "", "ledger: history"); err != nil {
		return 0, 0, 0, fmt.Errorf("seeding the remote: %w", err)
	}
	writers := m.Writers
	if writers <= 0 {
		writers = 1
	}
	if len(res.Keys.Writers) < writers {
		return 0, 0, 0, fmt.Errorf("the history enrolled %d writer keys for %d writers", len(res.Keys.Writers), writers)
	}
	var attempts int64
	var relinked int64
	var failed int64
	var errMu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			c, err := gitref.NewClient(filepath.Join(work, fmt.Sprintf("writer-%03d", w)), remote, "refs/seed/ledger")
			if err != nil {
				atomic.AddInt64(&failed, 1)
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("writer %d: %w", w, err)
				}
				errMu.Unlock()
				return
			}
			key := res.Keys.Writers[w]
			fp, _ := event.Fingerprint(key.Public().(ed25519.PublicKey))
			out, err := c.AppendLoop(gitref.Draft{
				V: version.Seed1, TS: "2026-09-02T00:00:00Z", Actor: fp,
				Verb: "intent.filed", Subject: fmt.Sprintf("s-%04d", w),
				Payload: json.RawMessage(`{"intent": "storm", "tier": "trivial", "budget": "small", "routing": "core"}`),
			}, func(e event.Event) (*event.Record, error) { return event.Sign(e, key) }, res.Resolve, admit.Validate(), writers*4)
			if err != nil {
				atomic.AddInt64(&failed, 1)
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("writer %d: %w", w, err)
				}
				errMu.Unlock()
				return
			}
			atomic.AddInt64(&attempts, int64(out.Attempts))
			atomic.AddInt64(&relinked, int64(out.Relinked))
		}(w)
	}
	wg.Wait()
	wall := time.Since(start)
	if failed > 0 {
		// A writer fails for more reasons than a lost update (a hook
		// refusal, a git error); the first failure is named so the
		// storm's verdict is a diagnosis, not a guess.
		return 0, 0, 0, fmt.Errorf("%d of %d writers failed to land; the first: %v", failed, writers, firstErr)
	}
	c, err := gitref.NewClient(filepath.Join(work, "reader"), remote, "refs/seed/ledger")
	if err != nil {
		return 0, 0, 0, err
	}
	tip, err := c.Fetch()
	if err != nil {
		return 0, 0, 0, err
	}
	dir := filepath.Join(work, "final")
	if err := c.Materialize(tip, dir); err != nil {
		return 0, 0, 0, err
	}
	final, err := ledger.OpenReadOnly(dir)
	if err != nil {
		return 0, 0, 0, err
	}
	// Every append landed exactly once, and each from its own actor
	// (plans/os-a00d3f34.md D1): the storm's records are signed by as
	// many distinct keys as there were writers.
	actors := map[string]bool{}
	rep, err := final.VerifyFromGenesis(res.Resolve, ledger.WithObserver(func(_ int, rec *event.Record) {
		if rec.Event.Verb == "intent.filed" && strings.HasPrefix(rec.Event.Subject, "s-") {
			actors[rec.Event.Actor] = true
		}
	}))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("the stormed chain must verify: %w", err)
	}
	if rep.Count != res.Records+writers {
		return 0, 0, 0, fmt.Errorf("the chain holds %d records, %d expected: a lost or doubled update", rep.Count, res.Records+writers)
	}
	if len(actors) != writers {
		return 0, 0, 0, fmt.Errorf("the storm's records carry %d distinct actors for %d writers", len(actors), writers)
	}
	return wall, float64(attempts) / float64(writers), relinked, nil
}
