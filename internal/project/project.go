// Package project is the projection engine (docs/build-plan.md
// Phase 4 item 1; plans/os-4d5cacff.md; SEED-NEXT.md Part II
// "Projections"): every projection is a deterministic function from the
// verified record prefix to a view that is derived, stamped,
// rebuildable, read-only outward, and non-authoritative. The engine
// opens the ledger read-only (it is structurally incapable of the
// healing writes the ordinary open performs), verifies from genesis
// before writing anything, and publishes each projection as immutable
// build directories plus an atomically replaced CURRENT pointer, since
// a directory rename cannot replace a non-empty target and deleting
// first would open the very window atomicity forbids (review finding
// on #105).
package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
)

// Inputs is the declared non-chain input a rebuild may carry
// (spec/observations.md; spec/refusals.md): the loaded
// observation snapshot with the as_of instant classification is
// computed at and the classification thresholds, and the attempts
// journal behind the report's refusal-rate section. Everything here
// is declared by the caller, never read from a wall clock or an
// ambient path, so an input-bearing build stays a pure function of
// (records, inputs). The families declare independently: either,
// both, or neither may be present.
type Inputs struct {
	Obs        *obs.Snapshot
	AsOf       time.Time
	Thresholds obs.Thresholds
	Refusals   *refusals.Journal
}

// Declared reports whether any input family is present; only then do
// the digest, the stamp's inputs field, and the build id's input
// segment apply.
func (in Inputs) Declared() bool {
	return in.Obs != nil || in.Refusals != nil
}

// Digest is the declared-inputs identity: the RFC 8785 digest over
// EVERY declared input (the snapshot with as_of and both thresholds,
// and the attempts journal), because any of them changes the
// section bytes, and an identity covering only part would let a
// rebuild with different inputs be discarded as a same-id duplicate,
// leaving stale sections permanently live in the published view.
// Each family contributes its keys only when declared, so an
// obs-only digest is unchanged by this field existing.
func (in Inputs) Digest() (string, error) {
	fields := map[string]any{}
	if in.Obs != nil {
		snapDigest, err := in.Obs.Digest()
		if err != nil {
			return "", err
		}
		fields["obs"] = snapDigest
		fields["as_of"] = in.AsOf.UTC().Format(time.RFC3339)
		fields["expiry_after_seconds"] = int(in.Thresholds.ExpiryAfter / time.Second)
		fields["wedge_after_seconds"] = int(in.Thresholds.WedgeAfter / time.Second)
	}
	if in.Refusals != nil {
		journalDigest, err := in.Refusals.Digest()
		if err != nil {
			return "", err
		}
		fields["refusals"] = journalDigest
	}
	b, err := json.Marshal(fields)
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

// Builder is one projection: a pure function from the verified record
// prefix and the declared inputs to named output files
// (slash-separated relative paths). The engine hands zero Inputs to a
// projection that does not declare consumption, so an input-free view
// is byte-identical with and without inputs by construction.
type Builder func(records []*event.Record, in Inputs) (map[string][]byte, error)

// Projection pairs a name with its builder. Version identifies the
// builder's derivation semantics, not its input: bump it when Build's
// logic changes so one ledger prefix republishes under a new build id
// rather than being discarded as a same-id duplicate. Empty means "1".
// Inputs declares that the builder consumes declared inputs; only
// then do the snapshot digest, the stamp's inputs field, and the
// build id's input segment apply.
type Projection struct {
	Name    string
	Version string
	Inputs  bool
	Build   Builder
}

// Stamp is the staleness surface every build carries
// (spec/projections.md): the exact verified position the view was
// built at, the derivation version that built it, and, for an
// input-bearing build, the declared-inputs digest that keyed it.
// Consumers read it; the engine never hides it.
type Stamp struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
	Tip      string `json:"tip"`
	Version  string `json:"version"`
	Inputs   string `json:"inputs,omitempty"`
}

// The published layout under <out>/<name>/: immutable build trees and
// the pointer naming the active one.
const (
	StampFile   = "projection.json"
	CurrentFile = "CURRENT"
	buildsDir   = "builds"
)

// Default returns the registered projections. Later phases append
// (standard projections 4.2, the cache 4.3); registration is data.
func Default() []Projection {
	return []Projection{Roster(), Contracts(), Queue(), Actors(), Report(), Obligations(), Knowledge(), Boundary(), Cache(), Ranking()}
}

// ErrOverlap refuses a ledger/output path overlap in either direction:
// a projection target must never coincide with authoritative state
// (review finding on #105).
var ErrOverlap = errors.New("the projection output must not overlap the ledger")

// canonical returns the cleaned absolute path with symlinks resolved
// over the longest existing ancestor, so a link cannot smuggle one path
// inside the other.
func canonical(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	probe := abs
	for {
		if r, err := filepath.EvalSymlinks(probe); err == nil {
			rest := strings.TrimPrefix(abs, probe)
			return filepath.Clean(r + rest), nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return abs, nil
		}
		probe = parent
	}
}

func within(inner, outer string) bool {
	rel, err := filepath.Rel(outer, inner)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// CheckOverlap refuses when the ledger directory and the output root
// overlap in either direction, before anything is created.
func CheckOverlap(ledgerDir, outDir string) error {
	l, err := canonical(ledgerDir)
	if err != nil {
		return err
	}
	o, err := canonical(outDir)
	if err != nil {
		return err
	}
	if within(l, o) || within(o, l) {
		return fmt.Errorf("%w (ledger %s, out %s)", ErrOverlap, l, o)
	}
	return nil
}

// Result reports one rebuilt projection.
type Result struct {
	Name     string
	Position int
	Tip      string
	Version  string
}

// Rebuild is the one-command rebuild without declared inputs: every
// projection builds from the verified records alone.
func Rebuild(ledgerDir, outDir string, projections []Projection, resolve ledger.Resolver, vopts ...ledger.VerifyOption) ([]Result, error) {
	return RebuildWith(ledgerDir, outDir, projections, resolve, Inputs{}, vopts...)
}

// RebuildWith is the one-command rebuild: refuse overlapping paths,
// open the ledger read-only, verify from genesis (a failure refuses
// before anything is written), run every registered projection over
// the verified records, and publish each tree. The build id derives
// from the stamp, so identical prefixes reproduce identical trees,
// CURRENT included: deleting the output loses nothing. A projection
// that declares Inputs and receives an observation snapshot keys its
// build id and stamp with the declared-inputs digest (snapshot,
// as_of, thresholds together), so any changed input at an unchanged
// tip republishes under a new id; input-free projections ignore the
// inputs entirely.
func RebuildWith(ledgerDir, outDir string, projections []Projection, resolve ledger.Resolver, in Inputs, vopts ...ledger.VerifyOption) (results []Result, err error) {
	if err := CheckOverlap(ledgerDir, outDir); err != nil {
		return nil, err
	}
	store, err := ledger.OpenReadOnly(ledgerDir)
	if err != nil {
		return nil, err
	}
	var records []*event.Record
	vopts = append(vopts, ledger.WithObserver(func(pos int, rec *event.Record) {
		records = append(records, rec)
	}))
	rep, err := store.VerifyFromGenesis(resolve, vopts...)
	if err != nil {
		return nil, err
	}
	// The output root itself locks between rebuilds: rename permission
	// on a projection root lives in its parent, so a writable parent
	// would let a whole root be renamed away however the path was
	// obtained (review finding on #112). The window opens only after
	// verification, keeping refuse-before-write intact, and every
	// return path relocks.
	if err := openDirs(outDir); err != nil {
		return nil, err
	}
	defer func() {
		if cerr := setMode(outDir, 0o555); err == nil && cerr != nil {
			err = cerr
		}
	}()
	results = make([]Result, 0, len(projections))
	for _, p := range projections {
		ver := p.Version
		if ver == "" {
			ver = "1"
		}
		// The build id derives from (position, tip, version): the
		// verified prefix AND the derivation semantics. A projection
		// whose Build logic changes bumps Version, so the same ledger
		// tip republishes under a new id instead of being discarded
		// as a same-id duplicate (review finding on #109). An
		// input-bearing build appends the declared-inputs digest as a
		// fourth segment for the same reason at an unchanged tip
		// (review finding on #121).
		buildID := fmt.Sprintf("%08d-%.12s-v%s", rep.Count, rep.Tip, ver)
		bin := Inputs{}
		digest := ""
		if p.Inputs && in.Declared() {
			bin = in
			digest, err = in.Digest()
			if err != nil {
				return nil, fmt.Errorf("projection %s: inputs digest: %v", p.Name, err)
			}
			buildID = fmt.Sprintf("%s-i%.12s", buildID, digest)
		}
		files, err := p.Build(records, bin)
		if err != nil {
			return nil, fmt.Errorf("projection %s: %v", p.Name, err)
		}
		stamp, err := json.Marshal(Stamp{Name: p.Name, Position: rep.Count, Tip: rep.Tip, Version: ver, Inputs: digest})
		if err != nil {
			return nil, err
		}
		if files == nil {
			files = map[string][]byte{}
		}
		files[StampFile] = append(stamp, '\n')
		if err := publish(filepath.Join(outDir, p.Name), buildID, files); err != nil {
			return nil, fmt.Errorf("projection %s: %v", p.Name, err)
		}
		results = append(results, Result{Name: p.Name, Position: rep.Count, Tip: rep.Tip, Version: ver})
	}
	return results, nil
}

// Published trees are locked (plans/os-8d5e9c45.md): files 0444 and
// directories 0555, the projection root included, so rename-over,
// unlink-plus-recreate, and new-entry creation fail at the operating
// system for every non-engine code path, however a published path was
// obtained. The engine opens a write window (chmod 0755) on exactly
// the two directories its swap must touch and closes it after; a
// crash inside the window leaves at worst writable directories and an
// orphan partial, never a broken view, and the next rebuild relocks
// everything.

// openDirs makes directories writable for the engine's own swap,
// creating them on first publication. Modes are set by explicit chmod
// in every case: MkdirAll's mode argument is weakened by the process
// umask, and the lock protocol must not be (review finding on #112).
// A partial open rolls itself back (review finding on #118): if a
// later directory refuses — builds/ occupied by a regular file, say —
// the ones already opened relock before the error surfaces, so a
// failed open never leaves a projection root writable. The rollback
// is best-effort: the open's own error is the one to surface.
func openDirs(dirs ...string) error {
	var opened []string
	relock := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = setMode(opened[i], 0o555)
		}
	}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if err := setMode(dir, 0o755); err != nil {
				relock()
				return err
			}
			opened = append(opened, dir)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			relock()
			return err
		}
		if err := setMode(dir, 0o755); err != nil {
			relock()
			return err
		}
		opened = append(opened, dir)
	}
	return nil
}

// closeWindow relocks the swap directories.
func closeWindow(root, builds string) error {
	if err := setMode(builds, 0o555); err != nil {
		return err
	}
	return setMode(root, 0o555)
}

// lockTree locks a completed build tree: files 0444, directories 0555,
// children before parents.
func lockTree(path string) error {
	var dirs []string
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		return setMode(p, 0o444)
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := setMode(dirs[i], 0o555); err != nil {
			return err
		}
	}
	return nil
}

// unlockDirs is the mode walk that makes a locked tree removable: every
// directory back to 0755 (files need no unlock; unlinking needs only
// parent-directory write). It is also the documented recovery walk for
// deleting projection output by hand; `seed project rebuild` runs it
// itself.
func unlockDirs(path string) error {
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return setMode(p, 0o755)
		}
		return nil
	})
}

// removeLocked removes a possibly locked tree or file.
func removeLocked(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := unlockDirs(path); err != nil {
			return err
		}
	}
	return os.RemoveAll(path)
}

// publish writes one immutable, complete, locked build tree and only
// then swaps CURRENT to it; superseded builds and stray partials prune
// after the swap. A reader always resolves CURRENT to a complete tree.
// Every return path after the window opens relocks it, a failed
// publication included (review finding on #112); only a killed process
// leaves an open window and at worst an orphan partial, and the next
// rebuild relocks.
func publish(root, buildID string, files map[string][]byte) (err error) {
	buildRoot := filepath.Join(root, buildsDir, buildID)
	builds := filepath.Join(root, buildsDir)
	if err := openDirs(root, builds); err != nil {
		return err
	}
	defer func() {
		if cerr := closeWindow(root, builds); err == nil {
			err = cerr
		}
	}()
	tmp := buildRoot + ".partial"
	if err := removeLocked(tmp); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		dst := filepath.Join(tmp, filepath.FromSlash(n))
		if !within(dst, tmp) {
			return fmt.Errorf("projection file %q escapes its build directory", n)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, files[n], 0o644); err != nil {
			return err
		}
	}
	if err := lockTree(tmp); err != nil {
		return err
	}
	if _, err := os.Stat(buildRoot); err == nil {
		// The same prefix rebuilt: determinism makes the existing tree
		// byte-identical to the one just built, and a reader may be on
		// it, so keep it and discard the duplicate.
		if err := removeLocked(tmp); err != nil {
			return err
		}
	} else if err := os.Rename(tmp, buildRoot); err != nil {
		return err
	}
	cur := filepath.Join(root, CurrentFile)
	// The build CURRENT named before this swap is retained through the
	// prune: a reader that resolved CURRENT just before the swap must
	// find a complete tree at the path it holds (review finding on
	// #109). Everything older, and stray partials, prune; a reader
	// that loses the race to two consecutive swaps re-resolves.
	prev := ""
	if b, err := os.ReadFile(cur); err == nil {
		prev = strings.TrimSpace(string(b))
	}
	tmpCur := cur + ".tmp"
	if err := os.Remove(tmpCur); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Written writable, then locked by mode: a file created read-only
	// on Windows carries the read-only attribute, which would block the
	// next publication's replacement of the pointer.
	if err := os.WriteFile(tmpCur, []byte(buildID+"\n"), 0o644); err != nil {
		return err
	}
	// WriteFile's mode is weakened by the process umask; the published
	// pointer must be exactly 0444 (review finding on #112).
	if err := setMode(tmpCur, 0o444); err != nil {
		return err
	}
	if err := renameReplacing(tmpCur, cur); err != nil {
		return err
	}
	entries, err := os.ReadDir(builds)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() != buildID && e.Name() != prev {
			if err := removeLocked(filepath.Join(builds, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Current resolves a projection's active build directory from its
// CURRENT pointer.
func Current(outDir, name string) (string, error) {
	root := filepath.Join(outDir, name)
	b, err := os.ReadFile(filepath.Join(root, CurrentFile))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", fmt.Errorf("projection %s has an empty CURRENT pointer", name)
	}
	return filepath.Join(root, buildsDir, id), nil
}

// renameReplacing swaps a pointer file into place, retrying briefly
// where the platform refuses to replace a file a concurrent reader
// holds open (Windows; spec/platform.md). POSIX renames over the
// open file on the first try.
func renameReplacing(src, dst string) error {
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}
