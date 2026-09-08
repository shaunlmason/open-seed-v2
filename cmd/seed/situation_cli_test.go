package main

// The situation drills (plans/os-52d5da3f.md D4): one
// position-stamped envelope carrying the caller's obligations and
// windows; --since is a COMPLETE change report whose discharged list
// lets a prior snapshot be brought forward exactly; and the read
// mutates nothing and journals nothing.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/refusals"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

type situationEnv struct {
	Actor       string           `json:"actor"`
	Obligations []map[string]any `json:"obligations"`
	Windows     []map[string]any `json:"windows"`
	Since       string           `json:"since"`
	Discharged  []map[string]any `json:"discharged"`
	Unchanged   string           `json:"unchanged"`
}

func situationOf(t *testing.T, args ...string) (ledgerEnv, situationEnv, int) {
	t.Helper()
	e, code := runEnv(t, append([]string{"situation"}, args...)...)
	var s situationEnv
	if e.Result != nil {
		b, err := json.Marshal(e.Result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
	}
	return e, s, code
}

func kindsOf(rows []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[fmt.Sprint(r["kind"])] = true
	}
	return out
}

func TestSituationRead(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, keys, fps := offerLedger(t)
	offerFile(t, ld, priv, specCommit, "c-1")

	// Before any claim the holder owes nothing and holds no window.
	_, s, code := situationOf(t, "--ledger", ld, "--key", keys["workerA"])
	if code != 0 {
		t.Fatalf("situation: %d", code)
	}
	if len(s.Obligations) != 0 || len(s.Windows) != 0 {
		t.Fatalf("an unclaimed board owes the worker nothing: %+v %+v", s.Obligations, s.Windows)
	}
	if s.Actor != fps["workerA"] {
		t.Fatalf("the response names the reading actor: %q", s.Actor)
	}

	// The claim creates the obligation and the window, and the
	// window carries the fence a lane would otherwise read out of a
	// projection by hand.
	fencePos, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	e, s, code := situationOf(t, "--ledger", ld, "--key", keys["workerA"])
	if code != 0 || e.Position == nil {
		t.Fatalf("situation after claim: %d %+v", code, e)
	}
	if !kindsOf(s.Obligations)["claim.held"] {
		t.Fatalf("the holder owes a deliberate exit: %+v", s.Obligations)
	}
	if len(s.Windows) != 1 || fmt.Sprint(s.Windows[0]["fence"]) != fmt.Sprintf("%d", fencePos) {
		t.Fatalf("the window carries the active fence: %+v", s.Windows)
	}
	for _, row := range s.Obligations {
		if v, ok := row["discharged_by"].([]any); !ok || len(v) == 0 {
			t.Fatalf("every obligation names what discharges it: %+v", row)
		}
	}

	// A different lane sees its own board, not the holder's.
	_, verifier, code := situationOf(t, "--ledger", ld, "--key", keys["verifier"])
	if code != 0 {
		t.Fatalf("verifier situation: %d", code)
	}
	if kindsOf(verifier.Obligations)["claim.held"] {
		t.Fatalf("the claim is not the verifier's debt: %+v", verifier.Obligations)
	}

	// The read mutates nothing and journals nothing: it is not an
	// admission-boundary attempt.
	before := journalLen(t, ld)
	if _, _, code := situationOf(t, "--ledger", ld, "--key", keys["workerA"]); code != 0 {
		t.Fatalf("repeat read: %d", code)
	}
	if after := journalLen(t, ld); after != before {
		t.Fatalf("a read journals nothing: %d -> %d", before, after)
	}
}

// conformance: --since is a complete change report — applying its
// obligations and discharged list to a prior snapshot must reproduce
// the standing set exactly, which a filtered list alone cannot do.
func TestSituationSinceIsApplicable(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, keys, _ := offerLedger(t)
	offerFile(t, ld, priv, specCommit, "c-1")
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	// The snapshot a resuming lane last saw, and the position it
	// stopped at.
	e, before, code := situationOf(t, "--ledger", ld, "--key", keys["workerA"])
	if code != 0 || e.Position == nil {
		t.Fatalf("snapshot: %d %+v", code, e)
	}
	at := *e.Position
	snapshot := kindsOf(before.Obligations)
	if !snapshot["claim.held"] {
		t.Fatalf("the snapshot holds the claim: %+v", before.Obligations)
	}

	// The holder releases: the obligation is discharged, so the delta
	// must SAY it is gone, not merely omit it.
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.released", "c-1", fmt.Sprintf(
		`{"fence": "%s", "packet": {"acceptance": ["done"], "decisions": [], "base": "0000000000000000000000000000000000000000..0000000000000000000000000000000000000000", "refs": [], "findings": []}}`,
		fmt.Sprint(before.Windows[0]["fence"]))); err != nil {
		t.Fatal(err)
	}
	_, delta, code := situationOf(t, "--ledger", ld, "--key", keys["workerA"], "--since", at)
	if code != 0 {
		t.Fatalf("delta: %d", code)
	}
	if len(delta.Discharged) == 0 {
		t.Fatalf("the delta names the removal: %+v", delta)
	}
	// Apply the response to the snapshot: add what arose, drop what
	// was discharged, and the result must equal the standing set.
	for _, row := range delta.Obligations {
		snapshot[fmt.Sprint(row["kind"])] = true
	}
	for _, row := range delta.Discharged {
		delete(snapshot, fmt.Sprint(row["kind"]))
	}
	_, full, code := situationOf(t, "--ledger", ld, "--key", keys["workerA"])
	if code != 0 {
		t.Fatalf("standing: %d", code)
	}
	standing := kindsOf(full.Obligations)
	if len(snapshot) != len(standing) {
		t.Fatalf("applying the delta must reproduce the standing set: applied %v, standing %v", snapshot, standing)
	}
	for kind := range standing {
		if !snapshot[kind] {
			t.Fatalf("applying the delta must reproduce the standing set: applied %v, standing %v", snapshot, standing)
		}
	}
}

// conformance: spec/obligations.md — the --since response is a
// COMPLETE change report, so a row whose OWNER moved must reach the
// party it moved TO. A budget.open transfers to lane:operator when its
// reserving signer is suspended (os-d6963652 D4), and the transfer
// changes no position: keyed on Since alone the operator's delta would
// call it unchanged, while the removals are derived from the prior set
// filtered to the caller, where it was never theirs. The debt would be
// invisible in the one mode a resuming lane is told to trust.
func TestSituationDeltaCarriesAnOwnerTransfer(t *testing.T) {
	restore := transition.InjectBudgetClass("ten", 10)
	defer restore()
	ld, _, _, specCommit, _, priv, _, keys, fps := offerLedger(t)
	for _, step := range [][]string{
		{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "ten", "routing": "core"}`},
		{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", "c-1", "--payload", step[1]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", keys["workerA"],
		"--subject", "c-1", "--amount", "4"); code != 0 || !e.OK {
		t.Fatalf("reserve: %d %+v", code, e)
	}

	// The operator's snapshot BEFORE the transfer: the reservation is
	// the signer's debt, so the operator is not carrying it.
	e, before, code := situationOf(t, "--ledger", ld, "--key", priv)
	if code != 0 || e.Position == nil {
		t.Fatalf("snapshot: %d %+v", code, e)
	}
	at := *e.Position
	if kindsOf(before.Obligations)["budget.open"] {
		t.Fatalf("an active signer's reservation is not the operator's debt: %+v", before.Obligations)
	}

	// Suspension moves it: every close from that signer now refuses,
	// and the operator lane is the only party left.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "actor.suspended", "--subject", fps["workerA"], "--payload", `{"reason": "drill"}`); code != 0 {
		t.Fatalf("suspend: %d %+v", code, e)
	}

	_, delta, code := situationOf(t, "--ledger", ld, "--key", priv, "--since", at)
	if code != 0 {
		t.Fatalf("delta: %d", code)
	}
	var transferred map[string]any
	for _, row := range delta.Obligations {
		if fmt.Sprint(row["kind"]) == "budget.open" {
			transferred = row
		}
	}
	if transferred == nil {
		t.Fatalf("the transfer must reach the operator's delta: %+v", delta)
	}
	if got := fmt.Sprint(transferred["owed_by"]); got != "lane:operator" {
		t.Fatalf("the transferred row names its new owner: %q", got)
	}
	// And it is reported despite arising BEFORE the cited position:
	// the obligation changed hands, it did not restart.
	var since, cited int
	if _, err := fmt.Sscanf(fmt.Sprint(transferred["since"]), "%d", &since); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Sscanf(at, "%d", &cited); err != nil {
		t.Fatal(err)
	}
	if since > cited {
		t.Fatalf("the drill is vacuous unless the row predates the cited position: since %d, --since %d", since, cited)
	}
}

func journalLen(t *testing.T, ld string) int {
	t.Helper()
	j, err := refusals.Load(filepath.Join(ld, refusals.File))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(j.Entries)
}
