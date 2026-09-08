// Package obs is the v0 observation channel (plans/os-2ff8dbf1.md;
// SEED-NEXT.md Part II §5): liveness, progress, and fine metering ride
// ephemeral, lossy-by-declaration streams (per-executor JSONL files,
// one stream per claim fence), summarized into ledger facts only at
// material transitions. Losing every observation stream loses no
// coordination state: nothing here feeds an admission decision.
package obs

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gowebpki/jcs"
)

// Line is one observation: the monotonic completed-item counter and
// the charter's declared in-step state for long-running work. Unsigned
// by design: the stream is non-authoritative by construction.
type Line struct {
	TS      string `json:"ts"`
	Subject string `json:"subject"`
	Count   int    `json:"count"`
	Step    string `json:"step"`
	// Units is metered usage (plans/os-1dad487d.md): a metering line
	// is an ordinary observation line with units set. Omitted when
	// zero, so pre-metering streams and snapshot digests stay
	// byte-identical.
	Units int `json:"units,omitempty"`
}

// Append writes one observation line to the per-run stream
// <dir>/<actor>/<fence>.jsonl, creating it as needed. The stream is
// keyed by the claim fence: one enrolled key can drive several
// executor runs, and a reaped predecessor's heartbeats must never
// blend into the current run's.
func Append(dir, actor, fence string, l Line) error {
	runDir := filepath.Join(dir, actor)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(runDir, fence+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// Stream is one run's observations in file order.
type Stream struct {
	Actor string `json:"actor"`
	Fence string `json:"fence"`
	Lines []Line `json:"lines"`
}

// Snapshot is a declared observation input: every stream under the
// directory at load time, deterministically ordered.
type Snapshot struct {
	Streams []Stream `json:"streams"`
}

// Load reads a snapshot from the channel directory. A missing
// directory is the empty snapshot: the channel is lossy by
// declaration.
func Load(dir string) (*Snapshot, error) {
	snap := &Snapshot{Streams: []Stream{}}
	actors, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return snap, nil
		}
		return nil, err
	}
	for _, a := range actors {
		if !a.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, a.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			name := f.Name()
			if filepath.Ext(name) != ".jsonl" {
				continue
			}
			st := Stream{Actor: a.Name(), Fence: name[:len(name)-len(".jsonl")]}
			fh, err := os.Open(filepath.Join(dir, a.Name(), name))
			if err != nil {
				return nil, err
			}
			sc := bufio.NewScanner(fh)
			sc.Buffer(make([]byte, 1<<16), 1<<20)
			for sc.Scan() {
				var l Line
				if json.Unmarshal(sc.Bytes(), &l) == nil {
					st.Lines = append(st.Lines, l)
				}
			}
			err = sc.Err()
			fh.Close()
			if err != nil {
				return nil, err
			}
			snap.Streams = append(snap.Streams, st)
		}
	}
	sort.Slice(snap.Streams, func(i, j int) bool {
		if snap.Streams[i].Actor != snap.Streams[j].Actor {
			return snap.Streams[i].Actor < snap.Streams[j].Actor
		}
		return snap.Streams[i].Fence < snap.Streams[j].Fence
	})
	return snap, nil
}

// Digest is the sha256 of the snapshot's RFC 8785 canonical encoding:
// the declared-inputs identity component (the report's stamp and build
// id carry it, so changed inputs republish under a new id).
func (s *Snapshot) Digest() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	canonical, err := jcs.Transform(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// StreamFor returns the stream keyed by the actor and fence, if any:
// classification reads ONLY the active claim's stream, so a
// predecessor's observations under a dead fence can neither revive nor
// wedge the current claim.
func (s *Snapshot) StreamFor(actor, fence string) (Stream, bool) {
	for _, st := range s.Streams {
		if st.Actor == actor && st.Fence == fence {
			return st, true
		}
	}
	return Stream{}, false
}

// Thresholds are the classification ages, declared inputs with spec'd
// v0 defaults (spec/observations.md).
type Thresholds struct {
	ExpiryAfter time.Duration
	WedgeAfter  time.Duration
}

// DefaultThresholds are the v0 operational defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{ExpiryAfter: 900 * time.Second, WedgeAfter: 1800 * time.Second}
}

// State classifies one claim's liveness.
type State string

const (
	// Live: an observation within expiry_after and the count advanced
	// within wedge_after.
	Live State = "live"
	// Expired: no observation within expiry_after, the
	// no-observations condition. Reap heuristic: after grace on the
	// lease.
	Expired State = "expired"
	// Wedged: observations continue but the count last advanced more
	// than wedge_after ago: observations without progress
	// advancement. Reap heuristic: operator or maintenance judgment,
	// the packet capturing the wedge.
	Wedged State = "wedged"
	// NoData: the active claim's stream holds nothing at all; absence
	// of data is stated, never fabricated.
	NoData State = "no_data"
)

// Classification is the evidence beside the verdict.
type Classification struct {
	State           State  `json:"state"`
	LastObservation string `json:"last_observation,omitempty"`
	LastAdvance     string `json:"last_advance,omitempty"`
	Count           int    `json:"count"`
}

// Classify is a pure function of the active claim's stream, a declared
// as_of instant, and the thresholds: expiry and wedging are distinct,
// visible conditions, and no wall clock is consulted. Lines stamped
// after as_of are invisible: they did not exist at the declared
// instant, so a clock-ahead executor cannot classify itself live and
// a historical as_of sees only what was there.
func Classify(stream Stream, asOf time.Time, th Thresholds) Classification {
	c := Classification{State: NoData}
	var lastObs, lastAdvance time.Time
	count := -1
	for _, l := range stream.Lines {
		ts, err := time.Parse(time.RFC3339, l.TS)
		if err != nil || ts.After(asOf) {
			continue
		}
		if ts.After(lastObs) {
			lastObs = ts
		}
		if l.Count > count {
			count = l.Count
			if ts.After(lastAdvance) {
				lastAdvance = ts
			}
		}
	}
	if lastObs.IsZero() {
		return c
	}
	c.Count = count
	c.LastObservation = lastObs.UTC().Format(time.RFC3339)
	c.LastAdvance = lastAdvance.UTC().Format(time.RFC3339)
	switch {
	case asOf.Sub(lastObs) > th.ExpiryAfter:
		c.State = Expired
	case asOf.Sub(lastAdvance) > th.WedgeAfter:
		c.State = Wedged
	default:
		c.State = Live
	}
	return c
}

// FormatFence renders a fence position the way streams are keyed.
func FormatFence(fence int) string { return fmt.Sprintf("%d", fence) }
