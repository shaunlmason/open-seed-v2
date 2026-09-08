package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

func writePosture(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seed.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runDoctorEnv(t *testing.T, args ...string) (ledgerEnv, int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"doctor"}, args...), &out, &errOut)
	var e ledgerEnv
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, out.String())
	}
	return e, code, errOut.String()
}

// conformance: III posture item — the preflight tool states the posture
// and, for cooperative, the named consequence verbatim and in plain
// words in front of the operator.
func TestDoctorStatesPostures(t *testing.T) {
	e, code, errOut := runDoctorEnv(t, "--config", writePosture(t, `{"posture": "cooperative"}`))
	if code != 0 || !e.OK || e.Result["posture"] != "cooperative" || e.Result["enforced"] != false {
		t.Fatalf("cooperative doctor failed: %d %+v", code, e)
	}
	if e.Result["consequence"] != posture.Consequence {
		t.Fatalf("the consequence must ride verbatim, got %q", e.Result["consequence"])
	}
	for _, phrase := range []string{"does not hold", "advisory against a hostile credential"} {
		if !strings.Contains(errOut, phrase) {
			t.Fatalf("the operator-facing output must say %q, got %q", phrase, errOut)
		}
	}

	e, code, errOut = runDoctorEnv(t, "--config", writePosture(t, `{"posture": "enforced-self-hosted"}`))
	if code != 0 || e.Result["posture"] != "enforced-self-hosted" || e.Result["enforced"] != true {
		t.Fatalf("enforced doctor failed: %d %+v", code, e)
	}
	if _, ok := e.Result["consequence"]; ok || errOut != "" {
		t.Fatalf("enforced posture carries no cooperative consequence, got %+v %q", e.Result, errOut)
	}

	// The third posture reports its deployment (plans/os-5c8a312c.md
	// D7): endpoint, identity and the ledger branch, and no gap sentence.
	e, code, errOut = runDoctorEnv(t, "--config", writePosture(t, `{"posture": "enforced-forge-hosted", "admission": {"endpoint": "http://127.0.0.1:1", "identity": "seed-admission[bot]", "checks": ["check"], "reviews": 1, "owners": ["@root"]}}`))
	if code != 0 || e.Result["enforced"] != true {
		t.Fatalf("forge-hosted doctor failed: %d %+v", code, e)
	}
	adm, _ := e.Result["admission"].(map[string]any)
	if adm["endpoint"] != "http://127.0.0.1:1" || adm["identity"] != "seed-admission[bot]" || adm["ledger_ref"] != posture.DefaultLedgerRef {
		t.Fatalf("the doctor must report the admission block, got %+v", e.Result)
	}
	if _, ok := e.Result["gap"]; ok {
		t.Fatalf("the forge-hosted gap sentence retired with the gap, got %+v", e.Result)
	}
	for _, phrase := range []string{"proposals go to http://127.0.0.1:1", posture.DefaultLedgerRef, "seed-admission[bot] alone"} {
		if !strings.Contains(errOut, phrase) {
			t.Fatalf("the operator-facing output must say %q, got %q", phrase, errOut)
		}
	}
	// Without its block the posture is a declaration that names a
	// service nothing can reach: refused at 13 and named.
	e, code, _ = runDoctorEnv(t, "--config", writePosture(t, `{"posture": "enforced-forge-hosted"}`))
	if code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" || !strings.Contains(e.Error.Message, "admission block") {
		t.Fatalf("a forge-hosted declaration without its block must exit 13 naming the block, got %d %+v", code, e)
	}
}

func TestDoctorRefusals(t *testing.T) {
	e, code, _ := runDoctorEnv(t, "--config", filepath.Join(t.TempDir(), "absent.json"))
	if code != 4 || e.Error == nil || e.Error.Code != "posture_undeclared" || !strings.Contains(e.Error.Message, "MUST declare") {
		t.Fatalf("undeclared deployment must exit 4 with the charter's wording, got %d %+v", code, e)
	}
	for name, content := range map[string]string{
		"unknown posture": `{"posture": "anarchy"}`,
		"unknown field":   `{"posture": "cooperative", "mode": "yolo"}`,
	} {
		e, code, _ := runDoctorEnv(t, "--config", writePosture(t, content))
		if code != 13 || e.Error == nil || e.Error.Code != "posture_invalid" {
			t.Fatalf("%s must exit 13 posture_invalid, got %d %+v", name, code, e)
		}
		if !strings.Contains(e.Error.Message, "valid postures") {
			t.Fatalf("%s must name the valid postures, got %+v", name, e.Error)
		}
	}
	// A config that exists but cannot be read (here: a directory) is an
	// operational failure at exit 66, never a content judgment.
	e, code, _ = runDoctorEnv(t, "--config", t.TempDir())
	if code != 66 || e.Error == nil || e.Error.Code != "posture_unreadable" {
		t.Fatalf("an unreadable config must exit 66 posture_unreadable, got %d %+v", code, e)
	}
	if _, code, _ := runDoctorEnv(t, "extra-arg"); code != 64 {
		t.Fatal("stray arguments are a usage error")
	}
}

// conformance: plans/os-83bc3d84.md D4, AC4 — the doctor reports the
// Part III table at the declared posture: counts, the open rows by
// pillar and row, complete only when nothing is open at an enforced
// posture; the cooperative posture sets the enforced-only rows aside
// and is never complete; a tree without the table has no section; a
// table that drifted from the charter is an operational failure.
func TestDoctorReportsConformanceAtThePosture(t *testing.T) {
	repo := "../.."
	e, code, _ := runDoctorEnv(t, "--config", writePosture(t, `{"posture": "enforced-self-hosted"}`), "--repo", repo)
	if code != 0 {
		t.Fatalf("doctor: %d %+v", code, e)
	}
	section, ok := e.Result["conformance"].(map[string]any)
	if !ok {
		t.Fatalf("the repository carries the table, so the doctor reports it: %+v", e.Result)
	}
	counts, _ := section["counts"].(map[string]any)
	total := 0.0
	for _, k := range []string{"met", "partial", "routed", "open"} {
		v, _ := counts[k].(float64)
		total += v
	}
	if total != 128 {
		t.Fatalf("every charter row is counted at the enforced posture: %v", counts)
	}
	if section["complete"] != false || section["because"] == "" {
		t.Fatalf("Phase 13 rows are open and III.R is routed, so Part III is not yet complete, and the doctor says why: %+v", section)
	}
	outstanding, _ := section["outstanding_rows"].([]any)
	if len(outstanding) == 0 {
		t.Fatalf("the rows not yet met are named: %+v", section)
	}
	statuses := map[string]bool{}
	for _, row := range outstanding {
		r, _ := row.(map[string]any)
		if r["id"] == "" || r["status"] == "" {
			t.Fatalf("an outstanding row carries its id and status: %+v", r)
		}
		statuses[r["status"].(string)] = true
	}
	if !statuses["open"] || !statuses["routed"] {
		t.Fatalf("open and routed rows are both outstanding: %v", statuses)
	}

	// Cooperative: the enforced-only rows are set aside, and Part III
	// cannot be complete here.
	e, _, _ = runDoctorEnv(t, "--config", writePosture(t, `{"posture": "cooperative"}`), "--repo", repo)
	section, _ = e.Result["conformance"].(map[string]any)
	na, _ := section["not_applicable_here"].([]any)
	mixed, _ := section["mixed_here"].([]any)
	if len(na) == 0 || len(mixed) == 0 || section["complete"] != false || !strings.Contains(section["because"].(string), "cooperative") {
		t.Fatalf("cooperative: enforced-only rows are documented as not holding, mixed rows are named, and with rows outstanding the table is not complete here either: %+v", section)
	}

	// A tree without the table has no section; a tree whose table
	// drifted from the charter fails unavailable.
	e, _, _ = runDoctorEnv(t, "--config", writePosture(t, `{"posture": "cooperative"}`), "--repo", t.TempDir())
	if _, ok := e.Result["conformance"]; ok {
		t.Fatal("no table, no section")
	}
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	charter, _ := os.ReadFile(filepath.Join(repo, "SEED-NEXT.md"))
	if err := os.WriteFile(filepath.Join(broken, "SEED-NEXT.md"), charter, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "spec", "conformance.json"), []byte(`{"pillars": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code, _ := runDoctorEnv(t, "--config", writePosture(t, `{"posture": "cooperative"}`), "--repo", broken); code != 5 || e.Error == nil || !strings.Contains(e.Error.Message, "conformance table") {
		t.Fatalf("a table that is not the charter is an operational failure: %d %+v", code, e)
	}
}
