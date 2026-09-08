package project_test

// The cache drills (plans/os-acc1ac78.md, amended per #110):
// byte-identical builds via the registry, view equivalence row set by
// row set, the stamp table equal to the tree stamp, mid-operation
// deletion losing nothing, zero authority, and the read-only consumer
// contract under locked publication.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/project"

	_ "modernc.org/sqlite"
)

func openCacheRO(t *testing.T, out string) (*sql.DB, string) {
	t.Helper()
	build, err := project.Current(out, "cache")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(build, project.CacheFile)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	return db, build
}

func one[T any](t *testing.T, db *sql.DB, query string, args ...any) T {
	t.Helper()
	var v T
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func TestCacheEqualsTheViews(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, "seed/0", "system.protocol.upgraded", "system", `{"to": "seed/1"}`)
	add(root, "seed/1", "actor.enrolled", pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, "seed/1", "actor.granted", pFP(t, worker), `{"capability": "maintenance"}`)
	add(worker, "seed/1", "task.note", "c-A", `{"n": 1}`)
	add(root, "seed/1", "actor.revoked", pFP(t, worker), `{"reason": "drill"}`)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	db, build := openCacheRO(t, out)
	defer db.Close()

	// The stamp table equals the tree stamp field-for-field.
	var stamp project.Stamp
	b, err := os.ReadFile(filepath.Join(build, project.StampFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &stamp); err != nil {
		t.Fatal(err)
	}
	row := db.QueryRow(`SELECT name, position, tip, version FROM stamp`)
	var name, tip, version string
	var position int
	if err := row.Scan(&name, &position, &tip, &version); err != nil {
		t.Fatal(err)
	}
	if name != stamp.Name || position != stamp.Position || tip != stamp.Tip || version != stamp.Version {
		t.Fatalf("stamp table %v/%v/%v/%v must equal projection.json %+v", name, position, tip, version, stamp)
	}
	if uv := one[int](t, db, `PRAGMA user_version`); uv != 13 {
		t.Fatalf("user_version must carry the cache schema generation, got %d", uv)
	}

	// Roster equivalence, the revoked worker included.
	if n := one[int](t, db, `SELECT COUNT(*) FROM roster`); n != 2 {
		t.Fatalf("roster rows: %d", n)
	}
	if standing := one[string](t, db, `SELECT standing FROM roster WHERE fingerprint = ?`, pFP(t, worker)); standing != "revoked" {
		t.Fatalf("worker standing: %s", standing)
	}
	if grants := one[string](t, db, `SELECT grants FROM roster WHERE fingerprint = ?`, pFP(t, worker)); grants != `["maintenance"]` {
		t.Fatalf("worker grants: %s", grants)
	}

	// Contract equivalence: the indexed per-subject lookup returns the
	// work stream with the payload content preserved.
	if n := one[int](t, db, `SELECT COUNT(*) FROM contracts WHERE subject = 'c-A'`); n != 1 {
		t.Fatalf("contract rows: %d", n)
	}
	if verb := one[string](t, db, `SELECT verb FROM contracts WHERE subject = 'c-A'`); verb != "task.note" {
		t.Fatalf("contract verb: %s", verb)
	}

	// Queue mirrors the v0 derivation marker with an empty ready set.
	if d := one[string](t, db, `SELECT derivation FROM queue_meta`); d != project.QueueDerivationEffective {
		t.Fatalf("queue derivation: %s", d)
	}
	if n := one[int](t, db, `SELECT COUNT(*) FROM queue`); n != 0 {
		t.Fatalf("queue rows: %d", n)
	}

	// Actor streams: the revoked key's attribution survives.
	if n := one[int](t, db, `SELECT COUNT(*) FROM actor_history WHERE fingerprint = ?`, pFP(t, worker)); n != 3 {
		t.Fatalf("worker standing history rows: %d", n)
	}
	if subj := one[string](t, db, `SELECT subject FROM actor_signed WHERE fingerprint = ? AND verb = 'task.note'`, pFP(t, worker)); subj != "c-A" {
		t.Fatalf("worker attribution: %s", subj)
	}

	// Report facts parse from the key-value rows.
	var chain struct {
		Position int    `json:"position"`
		Tip      string `json:"tip"`
	}
	if err := json.Unmarshal([]byte(one[string](t, db, `SELECT value FROM report WHERE key = 'chain'`)), &chain); err != nil {
		t.Fatal(err)
	}
	if chain.Position != stamp.Position || chain.Tip != stamp.Tip {
		t.Fatalf("report chain %+v must match the stamp %+v", chain, stamp)
	}
}

// conformance: III.D cache row — mid-operation deletion loses nothing.
func TestCacheMidOperationDeletionLosesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to delete a database another handle holds open, so a mid-operation deletion is not observable there (spec/platform.md)")
	}
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	ledgerBefore := treeHash(t, dir)
	db, build := openCacheRO(t, out)

	// Delete the cache's build tree while a read connection holds the
	// database open: POSIX keeps the inode, in-flight reads complete.
	if n := one[int](t, db, `SELECT COUNT(*) FROM roster`); n != 2 {
		t.Fatalf("pre-deletion read: %d", n)
	}
	// Removing a projection subtree needs the locked output root
	// writable too; the documented walk covers the whole root.
	if err := os.Chmod(out, 0o755); err != nil {
		t.Fatal(err)
	}
	unlockAndRemove(t, filepath.Join(out, "cache"))
	if n := one[int](t, db, `SELECT position FROM stamp`); n != 5 {
		t.Fatalf("read under deletion must complete from the open handle: %d", n)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(build); !os.IsNotExist(err) {
		t.Fatal("the build tree must be gone after close")
	}

	// The ledger is byte-unchanged and one rebuild restores the
	// identical database.
	if treeHash(t, dir) != ledgerBefore {
		t.Fatal("deletion must not touch the ledger")
	}
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	db2, build2 := openCacheRO(t, out)
	defer db2.Close()
	if build2 != build {
		t.Fatalf("the same prefix must republish the same build id: %s vs %s", build2, build)
	}
	if n := one[int](t, db2, `SELECT COUNT(*) FROM roster`); n != 2 {
		t.Fatalf("post-rebuild read: %d", n)
	}
}

// conformance: III.D cache row — zero authority: a poisoned copy never
// influences a rebuild, and the builder consumes records only.
func TestCacheZeroAuthority(t *testing.T) {
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	_, build := openCacheRO(t, out)
	published := filepath.Join(build, project.CacheFile)
	pristine, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}

	// Poison an unlocked copy and put it in the published place (the
	// unlock walk stands in for a chmod-capable actor; uid 0 in the
	// dev container can write regardless).
	if err := os.Chmod(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unlockWalk(filepath.Join(out, "cache")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(published, 0o644); err != nil {
		t.Fatal(err)
	}
	poisoned := append([]byte{}, pristine...)
	poisoned[len(poisoned)-1] ^= 0xFF
	if err := os.WriteFile(published, poisoned, 0o644); err != nil {
		t.Fatal(err)
	}

	// A same-id republish deliberately keeps the existing tree (a
	// reader may hold it; the locks, not the discard, are the
	// anti-tamper layer), so tamper recovery is the documented
	// deletion walk plus one rebuild: the pristine bytes come back
	// from the records alone, proving the poison was never an input.
	if err := os.Chmod(out, 0o755); err != nil {
		t.Fatal(err)
	}
	unlockAndRemove(t, filepath.Join(out, "cache"))
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(pristine) {
		t.Fatal("deletion plus rebuild must restore the derived truth over a poisoned cache")
	}
}
