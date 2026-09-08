package main

// Expiry, retirement, the dead-end acts, the bloat lints and the stale
// finding at the terminal (plans/os-0d537fbd.md AC1 to AC5): one chain
// with an admitted promotion, then every verb this card adds driven
// through `seed knowledge`, `seed situation`, `seed maintain run` and
// the projection, at declared instants.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// retirementStand is the chain the drills below start from: two
// holders' dead ends, the curator's hypothesis over them, the lesson
// file at its anchor, the bound pass and the observer's promotion.
type retirementStand struct {
	ld, repo, planCommit, lessonCommit, body string
	keys                                     map[string]string
	curatorKey, observerKey, maintenanceKey  string
	id, cited, anchor                        string
	p1, p1b, p2, hpos, pass, pp              int
	rootKey                                  ed25519.PrivateKey
	git                                      func(args ...string) string
}

func newRetirementStand(t *testing.T) *retirementStand {
	t.Helper()
	ld, _, _, _, _, priv, rootKey, keys, _ := offerLedger(t)
	for _, to := range []string{version.Seed2, version.Seed3} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+to+`"}`); code != 0 {
			t.Fatalf("upgrade to %s: %d %+v", to, code, e)
		}
	}
	v := version.Seed3
	curatorKey, curatorPub, curatorFP := writeWorkerKey(t, 26)
	observerKey, observerPub, observerFP := writeWorkerKey(t, 27)
	maintenanceKey, maintenancePub, maintenanceFP := writeWorkerKey(t, 31)
	for _, step := range [][]string{
		{"actor.enrolled", curatorFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "curator"}`, curatorPub)},
		{"actor.granted", curatorFP, `{"capability": "curate"}`},
		{"actor.enrolled", observerFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "observer"}`, observerPub)},
		{"actor.granted", observerFP, `{"capability": "observer"}`},
		{"actor.enrolled", maintenanceFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "maintenance"}`, maintenancePub)},
		{"actor.granted", maintenanceFP, `{"capability": "maintenance"}`},
		{"actor.granted", maintenanceFP, `{"capability": "operator"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	open := func(subject string) {
		rawAppendAt(t, ld, rootKey, v, "intent.filed", subject, `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
		rawAppendAt(t, ld, rootKey, v, "contract.specified", subject, `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`)
	}
	open("c-1")
	open("c-2")
	rawAppendAt(t, ld, workerRawKey(22), v, "claim.taken", "c-1", `{}`)
	rawAppendAt(t, ld, workerRawKey(23), v, "claim.taken", "c-2", `{}`)
	deadend := func(key, subject string) int {
		t.Helper()
		e, code := runEnv(t, "knowledge", "deadend", "--ledger", ld, "--key", key, "--subject", subject,
			"--tried", "retrying the fetch", "--outcome", "the mirror timed out", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")
		if code != 0 || e.Position == nil {
			t.Fatalf("the dead end on %s: %d %+v", subject, code, e)
		}
		var pos int
		fmt.Sscanf(*e.Position, "%d", &pos)
		return pos
	}
	st := &retirementStand{ld: ld, keys: keys, curatorKey: curatorKey, observerKey: observerKey, maintenanceKey: maintenanceKey, rootKey: rootKey}
	st.p1 = deadend(keys["workerA"], "c-1")
	st.p2 = deadend(keys["workerB"], "c-2")
	st.p1b = deadend(keys["workerA"], "c-1")
	claim := "retry the fetch once when the mirror is cold"
	st.id = curation.HypothesisID(claim, nil)
	e, code := runEnv(t, "knowledge", "propose", "--ledger", ld, "--key", curatorKey, "--claim", claim, "--applies-when", `{"routing": "core"}`,
		"--support", fmt.Sprintf("c-1@%d", st.p1), "--support", fmt.Sprintf("c-2@%d", st.p2))
	if code != 0 || e.Position == nil {
		t.Fatalf("the curator proposes: %d %+v", code, e)
	}
	fmt.Sscanf(*e.Position, "%d", &st.hpos)
	st.cited = fmt.Sprintf("%s@%d", st.id, st.hpos)
	support := []string{fmt.Sprintf("c-1@%d", st.p1), fmt.Sprintf("c-2@%d", st.p2)}
	st.repo, st.planCommit, st.lessonCommit, st.body = lessonRepo(t, st.id, st.hpos, support)
	st.git = func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", st.repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	st.anchor = curation.LessonsDir + "/retry-when-cold.md @ " + st.lessonCommit
	st.pass = st.evalPass(t, "eval-1", st.anchor)
	st.pp = st.promote(t, st.anchor, st.pass)
	return st
}

// evalPass files a bound eval for the anchor and renders its pass,
// the raw way the knowledge drill does: the promotion's premise, not
// this card's subject.
func (st *retirementStand) evalPass(t *testing.T, subject, anchor string) int {
	t.Helper()
	v := version.Seed3
	rootKey := st.rootKey
	rawAppendAt(t, st.ld, rootKey, v, "intent.filed", subject, fmt.Sprintf(`{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check", "lesson": %q, "carrier": %q}}`, st.cited, anchor))
	rawAppendAt(t, st.ld, rootKey, v, "contract.specified", subject, `{"acceptance": {"ref": "evals/fix-the-check/fixture/accept.md @ 0123456", "executable": true, "gate": "pr/1 @ 0123456"}}`)
	fence := rawAppendAt(t, st.ld, workerRawKey(23), v, "claim.taken", subject, `{}`)
	sub := rawAppendAt(t, st.ld, workerRawKey(23), v, "submission.made", subject, fmt.Sprintf(`{"fence": "%d", "packet": {"acceptance": ["ok"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}}`, fence))
	return rawAppendAt(t, st.ld, workerRawKey(24), v, "verdict.rendered", subject, fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`, strings.Repeat("0", 64), sub))
}

// promote is the observer's promotion of the anchor citing the pass.
func (st *retirementStand) promote(t *testing.T, anchor string, pass int) int {
	t.Helper()
	e, code := st.promoteEnv(t, anchor, pass)
	if code != 0 || e.Position == nil {
		t.Fatalf("the observer promotes %s: %d %+v", anchor, code, e)
	}
	var pos int
	fmt.Sscanf(*e.Position, "%d", &pos)
	return pos
}

func (st *retirementStand) promoteEnv(t *testing.T, anchor string, pass int) (ledgerEnv, int) {
	t.Helper()
	return runEnv(t, "knowledge", "promote", "--ledger", st.ld, "--key", st.observerKey, "--lesson", anchor, "--hypothesis", st.cited,
		"--pr", "pr/7 @ "+st.lessonCommit, "--repo", st.repo, "--carrier", "knowledge", "--adversarial", fmt.Sprintf("fix-the-check@%d", pass))
}

// restamp commits the lesson file with the stamps moved and returns
// the new anchor: a revalidation's file half.
func (st *retirementStand) restamp(t *testing.T, lastValidated, expires string) string {
	t.Helper()
	body := strings.Replace(st.body, "last-validated: 2026-09-01T00:00:00Z", "last-validated: "+lastValidated, 1)
	body = strings.Replace(body, "expires: 2026-12-01T00:00:00Z", "expires: "+expires, 1)
	if err := os.WriteFile(filepath.Join(st.repo, curation.LessonsDir, "retry-when-cold.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st.git("commit", "--quiet", "-am", "knowledge: revalidated")
	return curation.LessonsDir + "/retry-when-cold.md @ " + st.git("rev-parse", "HEAD")
}

func (st *retirementStand) show(t *testing.T, now string) map[string]any {
	t.Helper()
	args := []string{"knowledge", "show", "--ledger", st.ld}
	if now != "" {
		args = append(args, "--now", now)
	}
	e, code := runEnv(t, args...)
	if code != 0 {
		t.Fatalf("knowledge show: %d %+v", code, e)
	}
	return e.Result
}

func (st *retirementStand) lessonRow(t *testing.T, view map[string]any, pos int) map[string]any {
	t.Helper()
	rows, _ := view["lessons"].([]any)
	for _, r := range rows {
		m, _ := r.(map[string]any)
		if p, _ := m["position"].(float64); int(p) == pos {
			return m
		}
	}
	t.Fatalf("no lesson at position %d: %+v", pos, view["lessons"])
	return nil
}

// situationLessons is the count of lessons the held window on c-1
// carries in the orienting read, at the instant.
func (st *retirementStand) situationLessons(t *testing.T, now string) int {
	t.Helper()
	args := []string{"situation", "--ledger", st.ld, "--key", st.keys["workerA"], "--repo", st.repo}
	if now != "" {
		args = append(args, "--now", now)
	}
	e, code := runEnv(t, args...)
	if code != 0 {
		t.Fatalf("situation: %d %+v", code, e)
	}
	ws, _ := e.Result["windows"].([]any)
	for _, w := range ws {
		m, _ := w.(map[string]any)
		if m["subject"] == "c-1" {
			ls, _ := m["lessons"].([]any)
			return len(ls)
		}
	}
	t.Fatalf("workerA holds c-1: %+v", e.Result)
	return -1
}

func (st *retirementStand) retire(t *testing.T, key, reason string, extra ...string) (ledgerEnv, int) {
	t.Helper()
	args := []string{"knowledge", "retire", "--ledger", st.ld, "--key", key, "--lesson", st.anchor, "--hypothesis", st.cited, "--reason", reason}
	return runEnv(t, append(args, extra...)...)
}

func (st *retirementStand) maintain(t *testing.T, asOf string, extra ...string) (ledgerEnv, int) {
	t.Helper()
	obsDir := filepath.Join(t.TempDir(), "obs")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"maintain", "run", "--ledger", st.ld, "--repo", st.repo, "--key", st.maintenanceKey, "--obs", obsDir,
		"--artifacts", filepath.Join(t.TempDir(), "artifacts"), "--as-of", asOf}
	return runEnv(t, append(args, extra...)...)
}

// conformance: AC1, AC2, AC5 — expiry at the terminal, the retirement
// rows, the revalidation, and the stale finding's lifecycle.
func TestRetirementAndRevalidationAtTheTerminal(t *testing.T) {
	st := newRetirementStand(t)
	// Expiry is derived at the read's instant: the view without one
	// flags nothing and says so; at the expiry the lesson is stale
	// and leaves the orienting read.
	view := st.show(t, "")
	if view["staleness"] == nil || view["as_of"] != nil || st.lessonRow(t, view, st.pp)["stale"] != nil {
		t.Fatalf("with no instant the view flags nothing and says so: %+v", view)
	}
	before := st.show(t, "2026-11-30T23:59:59Z")
	if row := st.lessonRow(t, before, st.pp); row["stale"] != nil || row["surfaces"] != true || before["as_of"] != "2026-11-30T23:59:59Z" {
		t.Fatalf("before the expiry the lesson surfaces: %+v", row)
	}
	at := st.show(t, "2026-12-01T00:00:00Z")
	if row := st.lessonRow(t, at, st.pp); row["stale"] != true || row["surfaces"] != false || !strings.Contains(fmt.Sprint(row["reason"]), "expired") {
		t.Fatalf("at the expiry the lesson is stale and does not surface: %+v", row)
	}
	if stages, _ := at["stages"].(map[string]any); stages["stale"] != 1.0 {
		t.Fatalf("the stages count the stale lesson: %+v", stages)
	}
	if st.situationLessons(t, "2026-10-01T00:00:00Z") != 1 || st.situationLessons(t, "2026-12-01T00:00:00Z") != 0 {
		t.Fatal("the orienting read reads the instant: the lesson before its expiry, nothing at it")
	}

	// The stale finding: filed once under the promotion's position,
	// refused as a duplicate on the second pass, not filed within the
	// threshold.
	e, code := st.maintain(t, "2026-12-02T00:00:00Z", "--stale-after", "48h")
	if code != 0 || len(report(t, e).Filed) != 0 {
		t.Fatalf("within the threshold nothing files: %d %+v", code, report(t, e).Filed)
	}
	e, code = st.maintain(t, "2027-01-01T00:00:00Z", "--stale-after", "48h")
	rep := report(t, e)
	subject := fmt.Sprintf("%s/retry-when-cold.md@%d", curation.LessonsDir, st.pp)
	if code != 0 || len(rep.Filed) != 1 || rep.Filed[0].Class != "lesson_stale" || rep.Filed[0].Subject != subject {
		t.Fatalf("past the threshold the loop files one defect under the promotion's position: %d %+v", code, rep.Filed)
	}
	e, code = st.maintain(t, "2027-01-02T00:00:00Z", "--stale-after", "48h")
	rep = report(t, e)
	if code != 0 || len(rep.Filed) != 0 || len(rep.Refusals) != 1 || rep.Refusals[0].Subject != subject {
		t.Fatalf("the second pass re-files the same id and the boundary refuses the duplicate: %d filed %+v refused %+v", code, rep.Filed, rep.Refusals)
	}
	if e, code := st.maintain(t, "2027-01-01T00:00:00Z", "--stale-after", "-1h"); code != 64 {
		t.Fatalf("a negative threshold refuses at usage: %d %+v", code, e.Error)
	}

	// The retirement rows at the terminal.
	if e, code := st.retire(t, st.curatorKey, "expired"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a curate key cannot retire: %d %+v", code, e.Error)
	}
	for _, row := range []struct {
		name  string
		args  []string
		names string
	}{
		{"regression without pr", []string{"regression"}, "retirement.reason"},
		{"pr on expired", []string{"expired", "--pr", "pr/8 @ " + st.lessonCommit}, "retirement.reason"},
		{"superseded without superseded_by", []string{"superseded"}, "retirement.reason"},
		{"superseded_by on regression", []string{"regression", "--pr", "pr/8 @ " + st.lessonCommit, "--superseded-by", "3"}, "retirement.reason"},
		{"an unknown reason", []string{"rewritten"}, "retirement.reason"},
		{"a malformed pr", []string{"regression", "--pr", "pr/8"}, "retirement.shape"},
		{"superseded_by naming no promotion", []string{"superseded", "--superseded-by", fmt.Sprint(st.pp + 1)}, "retirement.superseded_by"},
		{"superseded_by naming itself", []string{"superseded", "--superseded-by", fmt.Sprint(st.pp)}, "retirement.superseded_by"},
	} {
		e, code := st.retire(t, st.observerKey, row.args[0], row.args[1:]...)
		if code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, row.names) {
			t.Fatalf("%s refuses naming %s: %d %+v", row.name, row.names, code, e.Error)
		}
	}
	if e, code := runEnv(t, "knowledge", "retire", "--ledger", st.ld, "--key", st.observerKey, "--lesson", st.anchor, "--reason", "expired"); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "--hypothesis") {
		t.Fatalf("a missing hypothesis refuses at usage naming it: %d %+v", code, e.Error)
	}
	// The regression: the revert merged, observed by the observer.
	e, code = st.retire(t, st.observerKey, "regression", "--pr", "pr/8 @ "+st.lessonCommit)
	if code != 0 || e.Result["subject"] != st.id || e.Position == nil {
		t.Fatalf("the observer retires the regressed lesson citing the revert: %d %+v", code, e)
	}
	var rpos int
	fmt.Sscanf(*e.Position, "%d", &rpos)
	view = st.show(t, "2026-10-01T00:00:00Z")
	row := st.lessonRow(t, view, st.pp)
	if row["retired"] != true || row["surfaces"] != false || !strings.Contains(fmt.Sprint(row["reason"]), "regression") || !strings.Contains(fmt.Sprint(row["reason"]), "pr/8") {
		t.Fatalf("the retired lesson names its retirement and surfaces nowhere: %+v", row)
	}
	if stages, _ := view["stages"].(map[string]any); stages["retired"] != 1.0 || stages["lessons"] != 1.0 || stages["observations"] != 3.0 || stages["hypotheses"] != 1.0 {
		t.Fatalf("the evidence stays: %+v", stages)
	}
	if retired, _ := view["retired"].([]any); len(retired) != 1 || retired[0].(map[string]any)["position"] != float64(rpos) {
		t.Fatalf("the standing retirements are listed: %+v", view["retired"])
	}
	if st.situationLessons(t, "2026-10-01T00:00:00Z") != 0 {
		t.Fatal("a retired lesson reaches no orienting read, at any instant")
	}
	if e, code := st.retire(t, st.observerKey, "expired"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "retirement.promotion") {
		t.Fatalf("a second retirement refuses naming the standing one: %d %+v", code, e.Error)
	}
	// The maintenance loop files nothing for a retired lesson.
	if e, code := st.maintain(t, "2027-02-01T00:00:00Z"); code != 0 || len(report(t, e).Filed) != 0 || len(report(t, e).Refusals) != 0 {
		t.Fatalf("a retired lesson is nobody's stale work: %d %+v", code, report(t, e))
	}

	// Revalidation: the stamps unmoved refuse naming both promotions;
	// moved forward, the re-promotion admits, is the path's latest,
	// clears the retirement and surfaces again.
	stale := st.restamp(t, "2026-09-01T00:00:00Z", "2027-06-01T00:00:00Z")
	stalePass := st.evalPass(t, "eval-2", stale)
	if e, code := st.promoteEnv(t, stale, stalePass); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "promotion.revalidation") || !strings.Contains(e.Error.Message, fmt.Sprintf("position %d", st.pp)) {
		t.Fatalf("a re-promotion whose last_validated did not move refuses naming the previous promotion: %d %+v", code, e.Error)
	}
	fresh := st.restamp(t, "2026-12-15T00:00:00Z", "2027-06-01T00:00:00Z")
	freshPass := st.evalPass(t, "eval-3", fresh)
	rp := st.promote(t, fresh, freshPass)
	view = st.show(t, "2027-01-01T00:00:00Z")
	if stages, _ := view["stages"].(map[string]any); stages["retired"] != nil || stages["lessons"] != 1.0 || stages["stale"] != nil {
		t.Fatalf("the revalidation is the path's one promotion and clears the retirement: %+v", stages)
	}
	if row := st.lessonRow(t, view, rp); row["surfaces"] != true || row["lesson"] != fresh {
		t.Fatalf("the revalidated lesson surfaces at its new anchor: %+v", row)
	}
	if st.situationLessons(t, "2027-01-01T00:00:00Z") != 1 || st.situationLessons(t, "2027-06-01T00:00:00Z") != 0 {
		t.Fatal("the revalidated lesson surfaces until its own expiry")
	}
	// Retiring the old anchor now refuses (not the latest); the new
	// one retires as expired.
	if e, code := st.retire(t, st.observerKey, "expired"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "retirement.promotion") {
		t.Fatalf("a non-latest promotion cannot be retired: %d %+v", code, e.Error)
	}
	// The stale finding follows the promotion: nothing before the new
	// expiry, and NEW work under the new position after it.
	if e, code := st.maintain(t, "2027-01-01T00:00:00Z"); code != 0 || len(report(t, e).Filed) != 0 {
		t.Fatalf("a re-promoted lesson is not stale before its new expiry: %+v", report(t, e).Filed)
	}
	e, code = st.maintain(t, "2027-07-01T00:00:00Z")
	rep = report(t, e)
	if code != 0 || len(rep.Filed) != 1 || rep.Filed[0].Subject != fmt.Sprintf("%s/retry-when-cold.md@%d", curation.LessonsDir, rp) {
		t.Fatalf("the later promotion expiring files new work under its own position: %d %+v", code, rep)
	}
	// The projection publishes the flags at the declared instant and
	// the report counts them; without inputs the projection says the
	// instant is undeclared.
	out := filepath.Join(t.TempDir(), "out")
	unlockForCleanup(t, out)
	if e, code := runEnv(t, "project", "rebuild", "--ledger", st.ld, "--out", out); code != 0 {
		t.Fatalf("project rebuild: %d %+v", code, e)
	}
	cur, code := runEnv(t, "project", "current", "--out", out, "--name", "knowledge")
	if code != 0 {
		t.Fatalf("project current: %d %+v", code, cur)
	}
	b, err := os.ReadFile(filepath.Join(cur.Result["path"].(string), "knowledge.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"staleness": "undeclared`) || strings.Contains(string(b), `"stale": true`) {
		t.Fatalf("an input-free build flags nothing and says so: %s", b)
	}
}

// conformance: AC3 — the dead-end acts and applicability at the
// terminal: the listing marks each dead end applicable by string
// equality, shows the retired flag, and excludes retired ones from the
// held-out evidence.
func TestDeadEndRetirementAtTheTerminal(t *testing.T) {
	st := newRetirementStand(t)
	validate := func(environment string) (map[string]any, []any) {
		t.Helper()
		args := []string{"knowledge", "validate", "--ledger", st.ld, "--hypothesis", st.cited}
		if environment != "" {
			args = append(args, "--environment", environment)
		}
		e, code := runEnv(t, args...)
		if code != 0 {
			t.Fatalf("knowledge validate: %d %+v", code, e)
		}
		held, _ := e.Result["held_out"].([]any)
		return e.Result, held
	}
	deadEndRow := func(res map[string]any, pos int) map[string]any {
		t.Helper()
		rows, _ := res["dead_ends"].([]any)
		for _, r := range rows {
			m, _ := r.(map[string]any)
			if m["position"] == fmt.Sprint(pos) {
				return m
			}
		}
		t.Fatalf("no dead end at %d: %+v", pos, res["dead_ends"])
		return nil
	}
	res, held := validate("ci-runner/v0")
	if len(held) != 1 || held[0].(map[string]any)["position"] != fmt.Sprint(st.p1b) || res["held_out_excludes"] != "retired dead ends" {
		t.Fatalf("the held-out listing and its note: %+v", res)
	}
	for _, pos := range []int{st.p1, st.p1b, st.p2} {
		if row := deadEndRow(res, pos); row["applies"] != true || row["retired"] != false || row["environment"] != "ci-runner/v0" {
			t.Fatalf("every dead end applies in its recorded environment: %+v", row)
		}
	}
	res, _ = validate("CI-RUNNER/V0")
	if deadEndRow(res, st.p1)["applies"] != false {
		t.Fatal("applicability is string equality, case included")
	}
	res, _ = validate("")
	if _, has := deadEndRow(res, st.p1)["applies"]; has {
		t.Fatal("without --environment nothing is judged applicable")
	}
	act := func(name, key string, pos int, environment string) (ledgerEnv, int) {
		return runEnv(t, "knowledge", "deadend", name, "--ledger", st.ld, "--key", key, "--deadend", fmt.Sprintf("c-1@%d", pos),
			"--environment", environment, "--reason", "the runner image moved")
	}
	if e, code := act("retire", st.keys["workerA"], st.p1b, "ci-runner/v1"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim key cannot retire a dead end: %d %+v", code, e.Error)
	}
	if e, code := act("retire", st.curatorKey, st.p1b, "ci-runner/v0"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "deadend_retirement.environment") {
		t.Fatalf("the recorded environment refuses naming the gate: %d %+v", code, e.Error)
	}
	if e, code := act("unretire", st.curatorKey, st.p1b, "ci-runner/v1"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "deadend_retirement.standing") {
		t.Fatalf("an un-retirement with nothing standing refuses: %d %+v", code, e.Error)
	}
	if e, code := act("retire", st.curatorKey, st.hpos, "ci-runner/v1"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "deadend_retirement.deadend") {
		t.Fatalf("citing a position that is no dead end refuses: %d %+v", code, e.Error)
	}
	if e, code := runEnv(t, "knowledge", "deadend", "retire", "--ledger", st.ld, "--key", st.curatorKey, "--deadend", fmt.Sprintf("c-1@%d", st.p1b), "--reason", "x"); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "--environment") {
		t.Fatalf("a missing environment refuses at usage naming it: %d %+v", code, e.Error)
	}
	if e, code := act("retire", st.curatorKey, st.p1b, "ci-runner/v1"); code != 0 || e.Result["subject"] != "c-1" {
		t.Fatalf("the curator retires the dead end whose environment moved: %d %+v", code, e)
	}
	res, held = validate("ci-runner/v0")
	if len(held) != 0 {
		t.Fatalf("a retired dead end leaves the held-out listing: %+v", held)
	}
	if row := deadEndRow(res, st.p1b); row["retired"] != true || row["applies"] != false || row["retired_environment"] != "ci-runner/v1" {
		t.Fatalf("the listing shows the retired flag and no applicability: %+v", row)
	}
	if row := deadEndRow(res, st.p1); row["retired"] != false || row["applies"] != true {
		t.Fatalf("the other dead ends are untouched: %+v", row)
	}
	if e, code := act("retire", st.curatorKey, st.p1b, "ci-runner/v2"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "deadend_retirement.standing") {
		t.Fatalf("a second retirement refuses: %d %+v", code, e.Error)
	}
	if e, code := act("unretire", st.curatorKey, st.p1b, "ci-runner/v1"); code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "deadend_retirement.environment") {
		t.Fatalf("an un-retirement in the retirement's environment refuses: %d %+v", code, e.Error)
	}
	if e, code := act("unretire", st.curatorKey, st.p1b, "ci-runner/v2"); code != 0 {
		t.Fatalf("the environment moved again: the un-retirement admits: %d %+v", code, e.Error)
	}
	res, held = validate("ci-runner/v0")
	if len(held) != 1 || deadEndRow(res, st.p1b)["retired"] != false || deadEndRow(res, st.p1b)["applies"] != true {
		t.Fatalf("an un-retired dead end is held out and applies again: %+v", res)
	}
	if stages, _ := st.show(t, "")["stages"].(map[string]any); stages["observations"] != 3.0 {
		t.Fatalf("evidence kept: %+v", stages)
	}
}

// conformance: AC4 — the bloat lints at the terminal: a second file
// citing the hypothesis, a frontmatter key the contract does not name,
// a body missing a section, and the sections out of order refuse
// naming the part; the shipped store passes.
func TestBloatLintsAtTheTerminal(t *testing.T) {
	st := newRetirementStand(t)
	file := filepath.Join(st.repo, curation.LessonsDir, "retry-when-cold.md")
	lint := func(path string) (ledgerEnv, int) {
		return runEnv(t, "knowledge", "lint", "--ledger", st.ld, "--repo", st.repo, "--now", "2026-10-01T00:00:00Z", path)
	}
	if e, code := lint(file); code != 0 || e.Result["structure"] != "ok" || e.Result["dedup"] != "ok" || e.Result["lint"] != "ok" {
		t.Fatalf("the reviewed file passes every lint: %d %+v", code, e)
	}
	dup := filepath.Join(st.repo, curation.LessonsDir, "retry-again.md")
	if err := os.WriteFile(dup, []byte(st.body), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := lint(file); code != 20 || e.Error == nil || !strings.Contains(e.Error.Message, "lint.duplicate") || !strings.Contains(e.Error.Message, "retry-again.md is the duplicate") {
		t.Fatalf("two files citing one hypothesis refuse naming the duplicate: %d %+v", code, e.Error)
	}
	if err := os.Remove(dup); err != nil {
		t.Fatal(err)
	}
	// Structure is judged on the file given, before the digest: each
	// bent body refuses at lint.structure naming the part.
	bent := filepath.Join(st.repo, curation.LessonsDir, "retry-when-cold.md")
	for _, row := range []struct{ name, body, names string }{
		{"an unknown key", strings.Replace(st.body, "carrier: knowledge\n", "carrier: knowledge\nstage: promoted\n", 1), "stage"},
		{"a missing section", strings.Replace(st.body, "## Evidence\n\nThe support set.\n\n", "", 1), `"## Evidence"`},
		{"sections out of order", strings.Replace(st.body, "## Claim\n\nRetry the fetch once when the mirror is cold.\n\n## Evidence\n\nThe support set.\n", "## Evidence\n\nThe support set.\n\n## Claim\n\nRetry the fetch once when the mirror is cold.\n", 1), "out of order"},
	} {
		if err := os.WriteFile(bent, []byte(row.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if e, code := lint(bent); code != 20 || e.Error == nil || !strings.Contains(e.Error.Message, "lint.structure") || !strings.Contains(e.Error.Message, row.names) {
			t.Fatalf("%s refuses at lint.structure naming the part: %d %+v", row.name, code, e.Error)
		}
	}
	if err := os.WriteFile(bent, []byte(st.body), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := lint(file); code != 0 {
		t.Fatalf("restored, the file passes: %d %+v", code, e.Error)
	}
	// The shipped store: one README and no duplicates.
	if err := curation.LintDuplicates(filepath.Join("..", "..", curation.LessonsDir), nil); err != nil {
		t.Fatalf("the shipped store passes the dedup lint: %v", err)
	}
}

// retention: D7 — no bump. A chain carrying the three verbs raw-pushed
// under active standing by a key the verbs do not accept verifies, and
// the fold counts each an anomaly: verification takes a verb it has no
// rule for on standing alone, which is what an older validator did with
// lesson.retired already in the catalog; only admission's answer
// changed, and the surfacing set, the flags and the stages move for
// none of them.
func TestRawPushedRetirementsVerifyAndFoldAsAnomalies(t *testing.T) {
	st := newRetirementStand(t)
	v := version.Seed3
	before, code := runEnv(t, "ledger", "verify", "--ledger", st.ld)
	if code != 0 {
		t.Fatalf("the chain verifies: %d %+v", code, before)
	}
	for _, step := range []struct{ verb, subject, payload string }{
		{curation.RetireVerb, st.id, `{"lesson": "` + st.anchor + `", "hypothesis": "` + st.cited + `", "reason": "expired"}`},
		{curation.DeadEndRetireVerb, "c-1", fmt.Sprintf(`{"deadend": "c-1@%d", "environment": "moved", "reason": "x"}`, st.p1b)},
		{curation.DeadEndUnretireVerb, "c-1", fmt.Sprintf(`{"deadend": "c-1@%d", "environment": "moved-again", "reason": "x"}`, st.p1b)},
	} {
		rawAppendAt(t, st.ld, workerRawKey(22), v, step.verb, step.subject, step.payload)
	}
	after, code := runEnv(t, "ledger", "verify", "--ledger", st.ld)
	if code != 0 || after.Result["count"].(float64) != before.Result["count"].(float64)+3 {
		t.Fatalf("the chain carrying the three raw-pushed verbs verifies, three records longer: %d %+v", code, after)
	}
	view := st.show(t, "2026-10-01T00:00:00Z")
	if view["anomalies"] != 3.0 {
		t.Fatalf("each raw-pushed act is an anomaly, never a stage: %+v", view["anomalies"])
	}
	if stages, _ := view["stages"].(map[string]any); stages["retired"] != nil || stages["lessons"] != 1.0 {
		t.Fatalf("nothing retired: %+v", stages)
	}
	if row := st.lessonRow(t, view, st.pp); row["surfaces"] != true || row["retired"] != nil {
		t.Fatalf("the promotion stands and surfaces: %+v", row)
	}
	ends, _ := view["dead_ends"].(map[string]any)
	for _, d := range ends["c-1"].([]any) {
		if m, _ := d.(map[string]any); m["retired"] != nil {
			t.Fatalf("no dead end is flagged: %+v", m)
		}
	}
	if st.situationLessons(t, "2026-10-01T00:00:00Z") != 1 {
		t.Fatal("the orienting read still carries the lesson")
	}
}
