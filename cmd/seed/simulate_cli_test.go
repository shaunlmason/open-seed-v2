package main

// Simulation reaches done, credential-free, through the real boundary
// (plans/os-16e55c11.md D3, AC3): every synthetic intent is driven to
// done under both postures with a mock executor, no forge, no model and
// no network beyond a local bare git remote, and the ledger audit is
// clean. This drives the whole system, so it lives here where loopVerbs
// (the in-process CLI seam) does.

import (
	"strconv"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
)

func simDone(t *testing.T, posture string, intents int) {
	t.Helper()
	e, code := runEnv(t, "simulate", "--lanes", "../../lanes",
		"--intents", strconv.Itoa(intents), "--posture", posture, "--work", t.TempDir())
	if code != envelope.ExitOK || !e.OK {
		t.Fatalf("simulate %s must succeed, got exit %d: %+v", posture, code, e.Error)
	}
	done, _ := e.Result["done"].(float64)
	if int(done) != intents {
		t.Fatalf("%s: every intent must reach done, got %v/%d", posture, done, intents)
	}
	audit, ok := e.Result["audit"].(map[string]any)
	if !ok {
		t.Fatalf("%s: the report must carry the audit", posture)
	}
	if clean, _ := audit["clean"].(bool); !clean {
		t.Fatalf("%s: the ledger audit must be clean, got %+v", posture, audit)
	}
	// The guardrail bar's ceiling arm judged under the deployment's
	// declaration (plans/os-b5051f2e.md D5): the run admitted every
	// claim under a declared ceiling and the audit read the same file.
	if declared, _ := audit["declared"].(bool); !declared {
		t.Fatalf("%s: the audit must judge under the deployment's declaration, got %+v", posture, audit)
	}
	results, _ := e.Result["results"].([]any)
	for _, r := range results {
		m, _ := r.(map[string]any)
		if s, _ := m["state"].(string); s != "done" {
			t.Errorf("%s: a contract ended at %q, not done", posture, s)
		}
	}
}

func TestSimulateReachesDoneCooperative(t *testing.T) {
	simDone(t, "cooperative", 2)
}

func TestSimulateAcceleratedBacklog(t *testing.T) {
	// The seven-day accelerated backlog (D5) reaches done with a clean
	// audit, the reporting instant advancing while admission reads no
	// clock.
	e, code := runEnv(t, "simulate", "--lanes", "../../lanes", "--intents", "3",
		"--days", "7", "--posture", "cooperative", "--work", t.TempDir())
	if code != envelope.ExitOK || !e.OK {
		t.Fatalf("accelerated simulate must succeed, got %d: %+v", code, e.Error)
	}
	if days, _ := e.Result["days"].(float64); int(days) != 7 {
		t.Fatalf("the report must carry the day span, got %v", e.Result["days"])
	}
	if done, _ := e.Result["done"].(float64); int(done) != 3 {
		t.Fatalf("every intent must reach done over the backlog, got %v", done)
	}
	audit, _ := e.Result["audit"].(map[string]any)
	if clean, _ := audit["clean"].(bool); !clean {
		t.Fatalf("the seven-day audit must be clean, got %+v", audit)
	}
}

func TestSimulateReachesDoneEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("enforced posture builds the seed-admit hook; skipped under -short")
	}
	simDone(t, "enforced-self-hosted", 1)
}
