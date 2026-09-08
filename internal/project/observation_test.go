package project_test

// The declared-inputs drills (plans/os-2ff8dbf1.md): an input-free
// rebuild reports observation: null; declared inputs key the report's
// stamp and build id with the snapshot digest while every input-free
// projection stays byte-identical; classification reads only the
// active claim's fence-keyed stream; changed inputs at an unchanged
// tip republish under a new id; and the channel is lossy by
// declaration.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func readCurrent(t *testing.T, out, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, name, "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestReportDeclaredInputs(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir, resolve, add := fixtureChain(t, root, worker)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "claim"}`)
	add(root, version.Seed1, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-1", `{}`)

	// Input-free: observation is null, stated not fabricated, and the
	// build id carries no input segment.
	out1 := lockedTempOut(t, "views")
	if _, err := project.Rebuild(dir, out1, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	var rep project.ReportView
	readView(t, out1, "report", project.ReportFile, &rep)
	if rep.Observation != nil {
		t.Fatalf("an input-free report must state observation: null, got %+v", rep.Observation)
	}
	if cur := readCurrent(t, out1, "report"); strings.Contains(cur, "-i") {
		t.Fatalf("an input-free build id carries no input segment: %s", cur)
	}

	// The channel: a live stream under the active claim's fence and a
	// ghost stream under a dead fence that must not leak in. The
	// claim.taken above landed at position 6.
	obsDir := filepath.Join(t.TempDir(), "obs")
	wfp := pFP(t, worker)
	for _, l := range []struct {
		fence string
		line  obs.Line
	}{
		{"6", obs.Line{TS: "2026-09-01T00:58:00Z", Subject: "c-1", Count: 1, Step: "build"}},
		{"2", obs.Line{TS: "2026-09-01T00:59:00Z", Subject: "c-1", Count: 99, Step: "ghost"}},
	} {
		if err := obs.Append(obsDir, wfp, l.fence, l.line); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := obs.Load(obsDir)
	if err != nil {
		t.Fatal(err)
	}
	in := project.Inputs{Obs: snap, AsOf: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC), Thresholds: obs.DefaultThresholds()}
	out2 := lockedTempOut(t, "views")
	if _, err := project.RebuildWith(dir, out2, project.Default(), resolve, in); err != nil {
		t.Fatal(err)
	}
	readView(t, out2, "report", project.ReportFile, &rep)
	if rep.Observation == nil {
		t.Fatal("declared inputs must produce the observation section")
	}
	digest, err := in.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Observation.Inputs.Digest != digest || rep.Observation.Inputs.AsOf != "2026-09-01T01:00:00Z" ||
		rep.Observation.Inputs.ExpiryAfterSeconds != 900 || rep.Observation.Inputs.WedgeAfterSeconds != 1800 {
		t.Fatalf("the section must echo its declared inputs: %+v", rep.Observation.Inputs)
	}
	cls, ok := rep.Observation.Claims["c-1"]
	if !ok || cls.State != obs.Live || cls.Count != 1 {
		t.Fatalf("classification must read the fence-keyed stream only (ghost count 99 must not leak): %+v", rep.Observation.Claims)
	}
	var stamp project.Stamp
	readView(t, out2, "report", project.StampFile, &stamp)
	if stamp.Inputs != digest {
		t.Fatalf("the stamp must carry the inputs digest: %+v", stamp)
	}
	cur2 := readCurrent(t, out2, "report")
	if !strings.HasSuffix(cur2, "-i"+digest[:12]) {
		t.Fatalf("the input-bearing id must end in the digest segment: %s", cur2)
	}

	// Every input-free projection is byte-identical with and without
	// inputs: same id, same tree, no inputs stamp.
	for _, name := range []string{"roster", "contracts", "queue", "actors", "cache"} {
		c1, c2 := readCurrent(t, out1, name), readCurrent(t, out2, name)
		if c1 != c2 {
			t.Fatalf("%s: an input-free projection must keep its id (%s vs %s)", name, c1, c2)
		}
		if treeHash(t, filepath.Join(out1, name)) != treeHash(t, filepath.Join(out2, name)) {
			t.Fatalf("%s: an input-free projection must be byte-identical with and without inputs", name)
		}
	}

	// Changed inputs at the same tip republish the report under a new
	// id; the earlier build stays retained beside it.
	if err := obs.Append(obsDir, wfp, "6", obs.Line{TS: "2026-09-01T00:59:30Z", Subject: "c-1", Count: 2, Step: "test"}); err != nil {
		t.Fatal(err)
	}
	snap2, err := obs.Load(obsDir)
	if err != nil {
		t.Fatal(err)
	}
	in.Obs = snap2
	if _, err := project.RebuildWith(dir, out2, project.Default(), resolve, in); err != nil {
		t.Fatal(err)
	}
	digest2, err := in.Digest()
	if err != nil {
		t.Fatal(err)
	}
	cur3 := readCurrent(t, out2, "report")
	if cur3 == cur2 || !strings.HasSuffix(cur3, "-i"+digest2[:12]) {
		t.Fatalf("changed inputs at the same tip must republish under a new id: %s vs %s", cur2, cur3)
	}
	if _, err := os.Stat(filepath.Join(out2, "report", "builds", cur2)); err != nil {
		t.Fatalf("the earlier input-bearing build must survive: %v", err)
	}

	// The identity covers EVERY declared input: the same snapshot at a
	// later as_of republishes too, or a silent worker would stay
	// permanently live under the stale id.
	in.AsOf = in.AsOf.Add(30 * time.Minute)
	if _, err := project.RebuildWith(dir, out2, project.Default(), resolve, in); err != nil {
		t.Fatal(err)
	}
	digest3, err := in.Digest()
	if err != nil {
		t.Fatal(err)
	}
	cur4 := readCurrent(t, out2, "report")
	if cur4 == cur3 || !strings.HasSuffix(cur4, "-i"+digest3[:12]) {
		t.Fatalf("a changed as_of alone must republish under a new id: %s vs %s", cur3, cur4)
	}

	// Lossy by declaration: delete the whole channel; the loader
	// yields the empty snapshot, and an input-free rebuild of the
	// same chain still publishes, saying so.
	if err := os.RemoveAll(obsDir); err != nil {
		t.Fatal(err)
	}
	empty, err := obs.Load(obsDir)
	if err != nil || len(empty.Streams) != 0 {
		t.Fatalf("a deleted channel is the empty snapshot: %v %+v", err, empty)
	}
	out3 := lockedTempOut(t, "views")
	if _, err := project.Rebuild(dir, out3, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	readView(t, out3, "report", project.ReportFile, &rep)
	if rep.Observation != nil {
		t.Fatal("losing every stream loses no coordination state, and the report says so")
	}
}
