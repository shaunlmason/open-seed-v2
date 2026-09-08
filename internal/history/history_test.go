package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// Two runs from one seed produce one chain, byte for byte, with the
// declared record count; a different seed produces different keys.
func TestGenerateIsDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	ra, err := Generate(Spec{Seed: 7, Contracts: 3, Dir: a})
	if err != nil {
		t.Fatal(err)
	}
	rb, err := Generate(Spec{Seed: 7, Contracts: 3, Dir: b})
	if err != nil {
		t.Fatal(err)
	}
	if ra.Records != Preamble+3*RecordsPerContract || ra.Records != rb.Records || ra.Tip != rb.Tip {
		t.Fatalf("one seed, one chain: %+v vs %+v", ra, rb)
	}
	sa, _ := os.ReadDir(filepath.Join(a, "segments"))
	for _, f := range sa {
		x, _ := os.ReadFile(filepath.Join(a, "segments", f.Name()))
		y, err := os.ReadFile(filepath.Join(b, "segments", f.Name()))
		if err != nil || string(x) != string(y) {
			t.Fatalf("segment %s differs across runs", f.Name())
		}
	}
	rc, err := Generate(Spec{Seed: 8, Contracts: 3, Dir: t.TempDir()})
	if err != nil || rc.Tip == ra.Tip {
		t.Fatalf("a different seed is a different chain: %v", err)
	}
	if _, err := Generate(Spec{Seed: 1, Contracts: -1, Dir: t.TempDir()}); err == nil {
		t.Fatal("a negative size refuses")
	}
}

// The generated chain verifies from genesis and every record admits at
// the boundary at its own position: the history is representative
// because it is what admission would have admitted.
func TestGeneratedHistoryAdmits(t *testing.T) {
	dir := t.TempDir()
	res, err := Generate(Spec{Seed: 3, Contracts: 2, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := store.VerifyFromGenesis(res.Resolve)
	if err != nil || rep.Count != res.Records {
		t.Fatalf("the chain must verify: %+v %v", rep, err)
	}
	var records []*event.Record
	_ = store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil })
	replay := t.TempDir()
	work, err := ledger.Open(replay)
	if err != nil {
		t.Fatal(err)
	}
	for i, rec := range records {
		if i > 0 {
			ctx, err := admit.ContextAt(work)
			if err != nil {
				t.Fatal(err)
			}
			if err := admit.Check(ctx, rec); err != nil {
				t.Fatalf("record %d (%s on %s) must admit: %v", i, rec.Event.Verb, rec.Event.Subject, err)
			}
		}
		if _, err := work.Append(rec, res.Resolve); err != nil {
			t.Fatal(err)
		}
	}
}
