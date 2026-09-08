package main

// conformance: plans/os-9075c308.md AC6 — in small-team mode the
// fixture chore worked three times through the production loop
// converts: the shape recurs at the second occurrence and the third
// adds an occurrence only; the curator's proposal validates the draft
// through the engine and lands it on the shape's branch, never main;
// the observer's observation merges it; the report reads 1.000.

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
)

func TestSmallTeamChoreWorkedThreeTimesConverts(t *testing.T) {
	m := buildMode(t, append(append([]identity{}, smallTeam...),
		identity{lane: "curator", actor: "curator", seed: 55},
		identity{lane: "dispatcher", actor: "dispatch", seed: 53}))
	instantiateSource(t, m.src)
	requireEngineCLI(t, m.src)
	main := gitAt(t, m.src, "rev-parse", "main")
	gated := fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/1 @ %s"}}`, m.spec, m.spec)

	// chore files and offers the contract (background), then the loop
	// claims, works and submits, the verifier renders over the
	// acceptance's own commands, and the holder's request is observed
	// merged: every occurrence is admitted history.
	chore := func(subject string, n int) {
		t.Helper()
		m.appendRaw("intent.filed", subject, fmt.Sprintf(`{"intent": "chore, take %d", "tier": "trivial", "budget": "small", "routing": "core"}`, n))
		m.appendRaw("contract.specified", subject, gated)
		offer := fmt.Sprintf(`{"eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}, "expires": %q}`, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
		if e, code := runEnv(t, "ledger", "append", "--remote", m.remote, "--state", m.state, "--key", m.keys["supervisor"],
			"--verb", "offer.published", "--subject", subject, "--payload", offer); code != 0 {
			t.Fatalf("offer: %d %s", code, envText(e))
		}
		d, err := loop.New(implementerManifest(t), loopVerbs{}, m.posture(), m.keys["impl"],
			loop.WorkFunc(func(string, loop.Situation) (int, error) { return 1, nil }), loop.WithBase(m.base+".."+m.head))
		if err != nil {
			t.Fatal(err)
		}
		step, err := d.Step(5)
		if err != nil || step.Outcome != loop.Submitted || step.Subject != subject {
			t.Fatalf("the loop claims, works and submits %s: %+v %v", subject, step, err)
		}
		if e, code := runEnv(t, append(append([]string{"verdict", "render"}, m.posture()...),
			"--subject", subject, "--repo", m.src, "--key", m.keys["verify"], "--verdict", "pass")...); code != 0 {
			t.Fatalf("verdict render: %d %s", code, envText(e))
		}
		if e, code := runEnv(t, append(append([]string{"merge", "request"}, m.posture()...),
			"--subject", subject, "--key", m.keys["impl"])...); code != 0 {
			t.Fatalf("merge request: %d %s", code, envText(e))
		}
		if e, code := runEnv(t, append(append([]string{"merge", "observe"}, m.posture()...),
			"--subject", subject, "--key", m.keys["observer"], "--merged", m.head, "--pr", "pr/"+subject)...); code != 0 {
			t.Fatalf("merge observe: %d %s", code, envText(e))
		}
	}
	shapes := func() []map[string]any {
		t.Helper()
		e, code := runEnv(t, append([]string{"flywheel", "shapes"}, m.posture()...)...)
		if code != 0 {
			t.Fatalf("flywheel shapes: %d %s", code, envText(e))
		}
		var out []map[string]any
		for _, row := range e.Result["shapes"].([]any) {
			out = append(out, row.(map[string]any))
		}
		return out
	}
	status := func() map[string]any {
		t.Helper()
		e, code := runEnv(t, append([]string{"flywheel", "status"}, m.posture()...)...)
		if code != 0 {
			t.Fatalf("flywheel status: %d %s", code, envText(e))
		}
		return e.Result["flywheel"].(map[string]any)
	}

	chore("c-1", 1)
	if rows := shapes(); len(rows) != 1 || rows[0]["recurring"] != false {
		t.Fatalf("once is no chore: %+v", rows)
	}
	chore("c-2", 2)
	rows := shapes()
	if len(rows) != 1 || rows[0]["recurring"] != true || rows[0]["count"].(float64) != 2 {
		t.Fatalf("the shape recurs at the second occurrence: %+v", rows)
	}
	chore("c-3", 3)
	rows = shapes()
	if len(rows) != 1 || rows[0]["count"].(float64) != 3 {
		t.Fatalf("the third adds an occurrence only: %+v", rows)
	}
	shape := rows[0]["id"].(string)
	name := rows[0]["name"].(string)
	if st := status(); st["recurring"].(float64) != 1 || st["proposed"].(float64) != 0 || st["conversion_rate"] != "0.000" {
		t.Fatalf("one recurring shape, unconverted: %+v", st)
	}

	// The curator's proposal: validated through the engine, on the
	// branch; the observer's observation converts it.
	e, code := runEnv(t, append([]string{"flywheel", "propose"}, m.posture()...)...)
	if code != envelope.ExitUsage {
		t.Fatalf("propose without its arguments is a usage refusal: %d %s", code, envText(e))
	}
	e, code = runEnv(t, append(append([]string{"flywheel", "propose"}, m.posture()...), "--key", m.keys["curator"], "--shape", shape, "--repo", m.src)...)
	if code != 0 || !e.OK {
		t.Fatalf("the curator proposes: %d %s", code, envText(e))
	}
	branch := "seed/flywheel-" + shape
	commit := e.Result["branch_head"].(string)
	occurrences := 0
	if e.Result["branch"] != branch || gitAt(t, m.src, "rev-parse", branch) != commit {
		t.Fatalf("the draft lands on the shape's branch: %+v", e.Result)
	}
	if gitAt(t, m.src, "rev-parse", "main") != main {
		t.Fatal("main did not move")
	}
	if out, err := exec.Command("git", "-C", m.src, "cat-file", "-e", "main:"+flywheel.RegistryDir+"/"+name+".yaml").CombinedOutput(); err == nil {
		t.Fatalf("the draft file is absent from main: %s", out)
	}
	if body := gitAt(t, m.src, "show", commit+":"+flywheel.RegistryDir+"/"+name+".yaml"); !strings.Contains(body, "3 done contract(s)") {
		t.Fatalf("the branch's draft was drafted from the three occurrences: %s", body)
	}
	for _, row := range shapes() {
		occurrences += len(row["occurrences"].([]any))
	}
	if occurrences != 3 {
		t.Fatalf("three occurrences: %d", occurrences)
	}
	if st := status(); st["proposed"].(float64) != 1 || st["merged"].(float64) != 0 {
		t.Fatalf("proposed, unmerged: %+v", st)
	}
	if e, code := runEnv(t, append(append([]string{"flywheel", "observe"}, m.posture()...), "--key", m.keys["observer"], "--shape", shape, "--merged", commit, "--pr", "pr/9")...); code != 0 || !e.OK {
		t.Fatalf("the observer observes the merge: %d %s", code, envText(e))
	}
	if st := status(); st["merged"].(float64) != 1 || st["conversion_rate"] != "1.000" {
		t.Fatalf("converted: %+v", st)
	}
}
