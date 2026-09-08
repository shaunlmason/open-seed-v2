package perfgate

import (
	"crypto/ed25519"
	"runtime"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/history"
)

// conformance: III.C row 4 — hundreds of concurrent actors are hundreds
// of keypairs (plans/os-a00d3f34.md D1, AC1): the history enrolls one
// agent key per writer with the grant intent.filed accepts, the storm
// signs each writer's append with its own key, and the measurer holds
// the landed chain to as many distinct actors as writers. A storm of
// three lands three actors through the loop with no hook, and a
// history asked for writers enrolls them distinctly.
func TestStormLandsOneActorPerWriter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the storm spawns git against a bare remote; the drills run on POSIX")
	}
	res, err := history.Generate(history.Spec{Seed: 1, Contracts: 1, Dir: t.TempDir(), Writers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Keys.Writers) != 3 || res.Records != history.Preamble+3*2+history.RecordsPerContract {
		t.Fatalf("three writers enrolled and granted: %d keys, %d records", len(res.Keys.Writers), res.Records)
	}
	seen := map[string]bool{}
	for _, k := range res.Keys.Writers {
		fp, _ := event.Fingerprint(k.Public().(ed25519.PublicKey))
		if seen[fp] {
			t.Fatal("two writers share a key")
		}
		seen[fp] = true
		if _, ok := res.Resolve(fp); !ok {
			t.Fatalf("the writer %s resolves", fp)
		}
	}
	reading, err := Measurer{Seed: 1, Contracts: 1, Writers: 3, Samples: 1}.Measure()
	if err != nil {
		t.Fatalf("a three-writer storm lands every writer from its own key: %v", err)
	}
	for _, m := range Required() {
		if _, ok := reading[m]; !ok {
			t.Errorf("the reading carries %s", m)
		}
	}
	if reading[MetricAttempts] < 1 {
		t.Fatalf("every landed append cost at least one attempt: %v", reading[MetricAttempts])
	}
}
