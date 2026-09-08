package project_test

// The projection-engine drills (plans/os-4d5cacff.md step 5;
// conformance III.D core): byte-identical one-command rebuild, stamps
// equal to the verification report, interrupted publication, refusals
// before anything is written, and a ledger the engine cannot touch.
// External test package: the fixtures need internal/genesis, which the
// engine deliberately does not import.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func pKey(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func pFP(t testing.TB, priv ed25519.PrivateKey) string {
	t.Helper()
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func enrollJSON(t testing.TB, priv ed25519.PrivateKey, kind, name string) string {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf(`{"key": %q, "kind": %q, "name": %q}`, hex.EncodeToString(pub), kind, name)
}

// fixtureChain writes a genesis-rooted chain into a fresh ledger dir
// and returns the dir, the root resolver, and an appender.
func fixtureChain(t *testing.T, root ed25519.PrivateKey, extra ...ed25519.PrivateKey) (string, ledger.Resolver, func(priv ed25519.PrivateKey, v, verb, subject, payload string)) {
	t.Helper()
	dir := t.TempDir()
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	g, err := genesis.Init(store, root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(g)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(g.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	ring := map[string]ed25519.PublicKey{}
	for _, p := range append(extra, root) {
		ring[pFP(t, p)] = p.Public().(ed25519.PublicKey)
	}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		pub, ok := ring[fp]
		return pub, ok
	}
	add := func(priv ed25519.PrivateKey, v, verb, subject, payload string) {
		t.Helper()
		tip, _, err := store.Tip()
		if err != nil {
			t.Fatal(err)
		}
		rec, err := event.Sign(event.Event{
			V: v, TS: "2026-09-01T01:00:00Z", Actor: pFP(t, priv),
			Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip,
		}, priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Append(rec, loose); err != nil {
			t.Fatal(err)
		}
	}
	return dir, resolve, add
}

// treeHash hashes every file under root (path and content), a stable
// fingerprint of the whole tree.
func treeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\x00%x\x00", filepath.ToSlash(rel), sha256.Sum256(b))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func lifecycleChain(t *testing.T) (string, ledger.Resolver, ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "maintenance"}`)
	add(worker, version.Seed1, "message.sent", "c-0001", `{"n": 1}`)
	return dir, resolve, root, worker
}

// conformance: III.D — one command rebuilds from genesis; deleting the
// output first changes nothing; repeat builds are byte-identical; the
// stamp equals the verification report; growing the chain changes both.
func TestRebuildByteIdenticalAndStamped(t *testing.T) {
	dir, resolve, root, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")

	results, err := project.Rebuild(dir, out, project.Default(), resolve)
	if err != nil || len(results) != 10 {
		t.Fatalf("rebuild: %+v %v", results, err)
	}
	if results[0].Name != "roster" || results[0].Position != 5 {
		t.Fatalf("stamp result wrong: %+v", results[0])
	}
	for _, r := range results[1:] {
		// Every registered view is stamped by the one verification
		// report the rebuild ran.
		if r.Position != results[0].Position || r.Tip != results[0].Tip {
			t.Fatalf("stamps must agree across projections: %+v vs %+v", r, results[0])
		}
	}
	first := treeHash(t, out)

	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if treeHash(t, out) != first {
		t.Fatal("a repeat rebuild over the same prefix must be byte-identical")
	}
	unlockAndRemove(t, out)
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if treeHash(t, out) != first {
		t.Fatal("deletion loses nothing: the rebuilt tree must be byte-identical")
	}

	// The published stamp matches the verification report exactly.
	cur, err := project.Current(out, "roster")
	if err != nil {
		t.Fatal(err)
	}
	var stamp project.Stamp
	b, err := os.ReadFile(filepath.Join(cur, project.StampFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &stamp); err != nil {
		t.Fatal(err)
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil || stamp.Position != rep.Count || stamp.Tip != rep.Tip || stamp.Name != "roster" || stamp.Version != "1" {
		t.Fatalf("stamp %+v must equal the verification report %+v (%v)", stamp, rep, err)
	}

	// Growing the chain moves the stamp, the build id, and the tree.
	st2, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tip, _, err := st2.Tip()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: version.Seed1, TS: "2026-09-01T02:00:00Z", Actor: pFP(t, root),
		Verb: "message.sent", Subject: "c-0002", Payload: json.RawMessage(`{"n": 2}`), Prev: tip,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st2.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	results, err = project.Rebuild(dir, out, project.Default(), resolve)
	if err != nil || results[0].Position != 6 {
		t.Fatalf("grown rebuild: %+v %v", results, err)
	}
	if treeHash(t, out) == first {
		t.Fatal("a grown chain must change the published tree")
	}
	// The build the swap superseded is retained for readers that
	// resolved CURRENT just before it; only older builds prune.
	prevBuild := filepath.Join(out, "roster", "builds", fmt.Sprintf("%08d-%.12s-v1", rep.Count, rep.Tip))
	if _, err := os.Stat(filepath.Join(prevBuild, project.RosterFile)); err != nil {
		t.Fatalf("the immediately superseded build must survive the swap: %v", err)
	}
	if cur2, err := project.Current(out, "roster"); err != nil || cur2 == prevBuild {
		t.Fatalf("CURRENT must name the new build: %s (%v)", cur2, err)
	}
}

// A projection whose derivation changes at an unchanged ledger tip
// republishes under a new version-bearing id; the same-id discard can
// never preserve obsolete semantics (review finding on #109).
func TestSemanticsChangeRepublishes(t *testing.T) {
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "projections")
	probe := func(ver, body string) []project.Projection {
		return []project.Projection{{Name: "probe", Version: ver, Build: func([]*event.Record, project.Inputs) (map[string][]byte, error) {
			return map[string][]byte{"probe.json": []byte(body + "\n")}, nil
		}}}
	}
	if _, err := project.Rebuild(dir, out, probe("1", "old-derivation"), resolve); err != nil {
		t.Fatal(err)
	}
	cur1, err := project.Current(out, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.Rebuild(dir, out, probe("2", "new-derivation"), resolve); err != nil {
		t.Fatal(err)
	}
	cur2, err := project.Current(out, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if cur2 == cur1 {
		t.Fatal("a bumped derivation version must publish a new build id at the same tip")
	}
	b, err := os.ReadFile(filepath.Join(cur2, "probe.json"))
	if err != nil || string(b) != "new-derivation\n" {
		t.Fatalf("the new derivation's output must be live: %q (%v)", b, err)
	}
	var stamp project.Stamp
	sb, err := os.ReadFile(filepath.Join(cur2, project.StampFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sb, &stamp); err != nil || stamp.Version != "2" || stamp.Position != 5 {
		t.Fatalf("the stamp must carry the new derivation version at the same position: %+v (%v)", stamp, err)
	}
	if _, err := os.Stat(filepath.Join(cur1, "probe.json")); err != nil {
		t.Fatalf("the superseded derivation's build is retained for in-flight readers: %v", err)
	}
	// The same derivation rebuilt is the same-id byte-identical case;
	// retention keeps only the current and immediately superseded
	// builds, so the v1 build ages out here while the resolved tree
	// stays identical.
	before := treeHash(t, cur2)
	if _, err := project.Rebuild(dir, out, probe("2", "new-derivation"), resolve); err != nil {
		t.Fatal(err)
	}
	cur3, err := project.Current(out, "probe")
	if err != nil || cur3 != cur2 {
		t.Fatalf("an unchanged derivation at an unchanged tip keeps its build id: %s (%v)", cur3, err)
	}
	if treeHash(t, cur3) != before {
		t.Fatal("an unchanged derivation at an unchanged tip must republish byte-identically")
	}
}

// conformance: III.D + plans/os-4d5cacff.md step 2 — the roster carries
// every keyring entry: genesis roots (root true, empty kind) and the
// full lifecycle standing; a root-only ledger is never empty.
func TestRosterLifecycleAndRootOnly(t *testing.T) {
	dir, resolve, root, worker := lifecycleChain(t)
	out := lockedTempOut(t, "p")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	readRoster := func() []project.RosterEntry {
		t.Helper()
		cur, err := project.Current(out, "roster")
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(cur, project.RosterFile))
		if err != nil {
			t.Fatal(err)
		}
		var entries []project.RosterEntry
		if err := json.Unmarshal(b, &entries); err != nil {
			t.Fatal(err)
		}
		return entries
	}
	entries := readRoster()
	if len(entries) != 2 {
		t.Fatalf("roster must carry the root and the worker, got %+v", entries)
	}
	if e := entries[0]; e.Fingerprint != pFP(t, root) || !e.Root || e.Kind != "" || e.Standing != string(keyring.StandingActive) {
		t.Fatalf("the genesis root entry is wrong: %+v", e)
	}
	if e := entries[1]; e.Fingerprint != pFP(t, worker) || e.Root || e.Kind != "agent" || len(e.Grants) != 1 || e.Grants[0] != "maintenance" {
		t.Fatalf("the worker entry is wrong: %+v", e)
	}

	// Standing changes surface on rebuild.
	st, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tip, _, err := st.Tip()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: version.Seed1, TS: "2026-09-01T02:00:00Z", Actor: pFP(t, root),
		Verb: keyring.VerbRevoked, Subject: pFP(t, worker), Payload: json.RawMessage(`{"reason": "drill"}`), Prev: tip,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if entries := readRoster(); entries[1].Standing != string(keyring.StandingRevoked) {
		t.Fatalf("the revocation must surface in the roster, got %+v", entries[1])
	}

	// A root-only, freshly initialized ledger yields the genesis roots.
	dir2, resolve2, _ := fixtureChain(t, pKey(t, 3))
	out2 := lockedTempOut(t, "p2")
	if _, err := project.Rebuild(dir2, out2, project.Default(), resolve2); err != nil {
		t.Fatal(err)
	}
	cur, err := project.Current(out2, "roster")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cur, project.RosterFile))
	if err != nil {
		t.Fatal(err)
	}
	var only []project.RosterEntry
	if err := json.Unmarshal(b, &only); err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || !only[0].Root || only[0].Standing != string(keyring.StandingActive) {
		t.Fatalf("a root-only ledger must yield its genesis root, got %+v", only)
	}
}

// conformance: III.D read-only outward — refusals happen before
// anything is written (stale-HEAD fixture included), overlapping paths
// refuse in both directions, and a successful build leaves the
// complete ledger tree byte-for-byte untouched.
func TestRefusalsAndLedgerImmutability(t *testing.T) {
	dir, resolve, root, _ := lifecycleChain(t)
	_ = root

	// Success case: the whole ledger tree is untouched.
	before := treeHash(t, dir)
	out := lockedTempOut(t, "p")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if treeHash(t, dir) != before {
		t.Fatal("a build must leave the ledger tree byte-for-byte untouched")
	}

	// Overlap refuses in both directions, before anything is created.
	if _, err := project.Rebuild(dir, filepath.Join(dir, "projections"), project.Default(), resolve); err == nil {
		t.Fatal("an output inside the ledger must refuse")
	}
	if _, err := project.Rebuild(dir, filepath.Dir(dir), project.Default(), resolve); err == nil {
		t.Fatal("a ledger inside the output must refuse")
	}
	if _, err := os.Stat(filepath.Join(dir, "projections")); !os.IsNotExist(err) {
		t.Fatal("the overlap refusal must create nothing")
	}

	// A stale-HEAD checkout refuses with nothing written and nothing
	// healed: OpenReadOnly cannot perform the ordinary open's repair.
	headPath := filepath.Join(dir, "HEAD")
	stale, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tip, _, err := st.Tip()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: version.Seed1, TS: "2026-09-01T02:00:00Z", Actor: pFP(t, pKey(t, 1)),
		Verb: "message.sent", Subject: "c-0009", Payload: json.RawMessage(`{"n": 9}`), Prev: tip,
	}, pKey(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headPath, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	staleTree := treeHash(t, dir)
	out2 := lockedTempOut(t, "p2")
	if _, err := project.Rebuild(dir, out2, project.Default(), resolve); err == nil {
		t.Fatal("a stale-HEAD ledger must refuse the build")
	}
	if _, statErr := os.Stat(out2); !os.IsNotExist(statErr) {
		t.Fatal("a refused build must write nothing")
	}
	if treeHash(t, dir) != staleTree {
		t.Fatal("a refused build must not heal or otherwise touch the ledger")
	}
}

// conformance: III.D — interrupted publication: with a stray partial
// build and an older CURRENT in place, readers resolve a complete view
// at every point, and the rebuild lands the new pointer only with a
// complete tree, pruning the leftovers.
func TestInterruptedPublication(t *testing.T) {
	dir, resolve, _, _ := lifecycleChain(t)
	out := lockedTempOut(t, "p")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	oldCur, err := project.Current(out, "roster")
	if err != nil {
		t.Fatal(err)
	}
	oldStamp, err := os.ReadFile(filepath.Join(oldCur, project.StampFile))
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a build killed mid-write: the engine's window still
	// open (a crash never reaches the relock), a partial tree beside
	// the active one, CURRENT still naming the old complete build.
	for _, d := range []string{out, filepath.Join(out, "roster"), filepath.Join(out, "roster", "builds")} {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	partial := filepath.Join(out, "roster", "builds", "99999999-deadbeef0000.partial")
	if err := os.MkdirAll(partial, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "half.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cur, err := project.Current(out, "roster"); err != nil || cur != oldCur {
		t.Fatalf("readers must still resolve the old complete build, got %s (%v)", cur, err)
	}
	if b, err := os.ReadFile(filepath.Join(oldCur, project.StampFile)); err != nil || string(b) != string(oldStamp) {
		t.Fatal("the old build must remain complete under the stray partial")
	}

	// The next rebuild publishes cleanly and prunes the leftovers.
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("publication must prune stray partial builds")
	}
	cur, err := project.Current(out, "roster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cur, project.StampFile)); err != nil {
		t.Fatal("CURRENT must resolve to a complete tree")
	}
	entries, err := os.ReadDir(filepath.Join(out, "roster", "builds"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("superseded builds must prune, got %d (%v)", len(entries), err)
	}
	if !strings.Contains(cur, entries[0].Name()) {
		t.Fatal("the surviving build must be the one CURRENT names")
	}
}
