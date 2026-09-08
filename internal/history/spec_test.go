package history

// Generate's argument and store refusals (plans/os-f262585a.md D1). The
// generated history is the perf gate's subject and the migration and
// audit drills' fixture, so a Spec it cannot honour has to refuse
// rather than write a shorter chain than the caller asked for.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRefusesANegativeCount(t *testing.T) {
	for _, tc := range []struct {
		spec Spec
		want string
	}{
		{Spec{Seed: 1, Contracts: -1, Dir: t.TempDir()}, "contracts must be zero or more"},
		{Spec{Seed: 1, Contracts: 1, Writers: -1, Dir: t.TempDir()}, "writers must be zero or more"},
	} {
		res, err := Generate(tc.spec)
		if err == nil {
			t.Fatalf("%+v must be refused", tc.spec)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("the refusal names the field: %v", err)
		}
		if res != nil {
			t.Error("a refused Spec returns no result")
		}
	}
}

func TestGenerateWithNoContractsIsThePreambleAlone(t *testing.T) {
	// Zero contracts is a legal Spec, not an empty one: the genesis and
	// the enrolments still land, which is what the migration fixture's
	// smallest case needs.
	res, err := Generate(Spec{Seed: 3, Contracts: 0, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Records != Preamble {
		t.Errorf("no contracts leaves the preamble alone, want %d records, got %d", Preamble, res.Records)
	}
	if res.Keys.Root == nil || res.Resolve == nil {
		t.Error("the keys and the resolver come back even with no contracts")
	}
}

func TestGenerateRefusesALedgerItCannotOpen(t *testing.T) {
	// A file where the ledger directory belongs: the store refuses, and
	// Generate must surface that rather than carry on against nothing.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(Spec{Seed: 1, Contracts: 1, Dir: file}); err == nil {
		t.Fatal("a ledger directory that cannot be opened must refuse")
	}
}

func TestWritersEnrolOneKeyEach(t *testing.T) {
	res, err := Generate(Spec{Seed: 9, Contracts: 1, Writers: 4, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Keys.Writers) != 4 {
		t.Fatalf("a storm of 4 writers is 4 keypairs, got %d", len(res.Keys.Writers))
	}
	seen := map[string]bool{}
	for _, w := range res.Keys.Writers {
		fp := fpOf(w)
		if seen[fp] {
			t.Fatalf("two writers share the fingerprint %s: the storm would not be concurrent actors", fp)
		}
		seen[fp] = true
		if _, ok := res.Resolve(fp); !ok {
			t.Errorf("writer %s does not resolve, so nothing it signed would verify", fp)
		}
	}
}
