package importer

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// conformance: plans/os-cf13fb51.md AC4 — the chain refuses a payload
// that is not JSON before signing, names a refused position with the
// export record it came from, and upgrades only to a version this
// build registers; Run refuses a named anchor that does not resolve, a
// table without the rows the run-log needs, and a ledger path that is
// a file, each before any write.
func TestChainAndRunEdges(t *testing.T) {
	op, err := newSigner(operatorKey(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	c, err := newChain(op, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.append(op, "intent.filed", "c-1", []byte("{nope"), now, "run-log#1"); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("a payload that is not JSON: %v", err)
	}
	if err := c.upgradeTo(op, "seed/99", now); err == nil || !strings.Contains(err.Error(), "not one this build registers") {
		t.Errorf("an unregistered version: %v", err)
	}
	refused := &RefusedError{Position: 3, Verb: "claim.taken", Subject: "c-1", Record: "run-log#2", Err: errors.New("held")}
	if !strings.Contains(refused.Error(), "run-log#2") || !errors.Is(refused, refused.Err) {
		t.Errorf("a refusal names the record and unwraps: %v", refused)
	}

	v := newV1Source(t, storeFiles())
	e := v.export(t)
	base := Options{Export: e, Source: v.dir, LedgerDir: filepath.Join(t.TempDir(), "ledger"), ArtifactsDir: filepath.Join(t.TempDir(), "art"), Operator: operatorKey(t), Now: now}
	named := base
	named.AnchorTag = "seed-anchor/absent"
	if _, err := Run(named); refusalKind(err) != "unanchored" {
		t.Errorf("a named anchor that does not resolve: %v", err)
	}
	table, err := DefaultTable()
	if err != nil {
		t.Fatal(err)
	}
	thin := *table
	thin.Verbs = map[string]VerbRow{}
	for verb, row := range table.Verbs {
		if verb != "lease-renew" {
			thin.Verbs[verb] = row
		}
	}
	sparse := base
	sparse.Table, sparse.TableBytes = &thin, []byte("{}")
	if _, err := Run(sparse); refusalKind(err) != "import_unmapped" {
		t.Errorf("a table without a row the run-log needs: %v", err)
	}
	onFile := base
	onFile.LedgerDir = filepath.Join(v.dir, "run-log.jsonl")
	if _, err := Run(onFile); err == nil {
		t.Error("a ledger path that is a file")
	}
	if !ledgerEmpty(t, base.LedgerDir) {
		t.Error("the refusals wrote nothing")
	}
}

// conformance: plans/os-cf13fb51.md AC2 — the rows the first drills
// left unexercised: a release is claim.released, and the operator's
// state repair and halt resume are named drops, each a disposition.
func TestReleaseAndOperatorRowsImport(t *testing.T) {
	files := storeFiles()
	files["run-log.jsonl"] += strings.Join([]string{
		`{"actor":"bob","data":{"lease_expires":"2026-01-01T03:00:00Z"},"task":"os-000002","ts":"2026-01-01T00:06:00Z","verb":"claim"}`,
		`{"actor":"bob","data":{"to":"ready","transitioned":true},"task":"os-000002","ts":"2026-01-01T00:07:00Z","verb":"release"}`,
		`{"actor":"alice","data":{"reason":"ref rewound"},"ts":"2026-01-01T00:08:00Z","verb":"state-repair"}`,
		`{"actor":"alice","data":{},"ts":"2026-01-01T00:09:00Z","verb":"state-resume"}`,
	}, "\n") + "\n"
	v := newV1Source(t, files)
	res, err := importInto(t, v, v.export(t), filepath.Join(t.TempDir(), "ledger"))
	if err != nil {
		t.Fatal(err)
	}
	verbs := map[string]int{}
	drops := 0
	for _, d := range res.Manifest.Records {
		if d.Drop != "" {
			drops++
		}
		for _, ev := range d.Events {
			verbs[ev.Verb]++
		}
	}
	if verbs["claim.released"] == 0 {
		t.Errorf("the release row is claim.released: %v", verbs)
	}
	if drops < 3 {
		t.Errorf("the lease renewal, the repair and the resume are named drops: %d", drops)
	}
}
