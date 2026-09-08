package main

// The loop-verb drills (plans/os-7e197768.md steps 4 and 5): each act
// through its happy path with the derived argument ABSENT from the
// flags, the refusal path carrying the boundary's own error beside a
// non-empty affordance list, the ambiguous close naming its
// candidates, a malformed packet refused before anything is signed,
// and each verb paired with what the obligations projection says it
// does — creation, discharge, or both, read back through seed
// situation rather than asserted as a rule of thumb.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// writePacket files a four-part packet, optionally without its base:
// the part the loop verbs complete from --base or from --repo.
func writePacket(t *testing.T, base string) string {
	t.Helper()
	parts := map[string]any{
		"acceptance": []string{"the drill resumes"},
		"decisions":  []map[string]string{},
		"refs":       []string{},
		"findings":   []map[string]string{},
	}
	if base != "" {
		parts["base"] = base
	}
	b, err := json.Marshal(parts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// chainCount reads the ledger's length, the assertion that a refused
// act appended nothing.
func chainCount(t *testing.T, ld string) int {
	t.Helper()
	e, code := runEnv(t, "ledger", "verify", "--ledger", ld)
	if code != 0 {
		t.Fatalf("verify: %d %+v", code, e)
	}
	n, ok := e.Result["count"].(float64)
	if !ok {
		t.Fatalf("verify names no count: %+v", e.Result)
	}
	return int(n)
}

// payloadAt reads one record's payload back out of the chain: how a
// derivation is asserted to have LANDED, not merely admitted.
func payloadAt(t *testing.T, ld string, pos int) map[string]any {
	t.Helper()
	e, code := runEnv(t, "ledger", "show", "--ledger", ld, "--position", fmt.Sprintf("%d", pos))
	if code != 0 {
		t.Fatalf("show %d: %d %+v", pos, code, e)
	}
	ev, ok := e.Result["event"].(map[string]any)
	if !ok {
		t.Fatalf("show names no event: %+v", e.Result)
	}
	p, ok := ev["payload"].(map[string]any)
	if !ok {
		t.Fatalf("show names no payload: %+v", ev)
	}
	return p
}

// conformance: III.I — the loop verbs derive every argument the
// system holds (the fence from the active window, the reservation
// from the shared budget view) and each act is paired with what the
// obligations projection says it does.
func TestLoopVerbsDeriveAndDischarge(t *testing.T) {
	ld, _, base, _, head, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	rng := base + ".." + head
	worker := keys["workerA"]

	fence, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatal(err)
	}

	// Reserve CREATES budget.open. No --fence flag exists: the
	// citation comes from the active window.
	e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", worker, "--subject", "c-1", "--amount", "10")
	if code != 0 || !e.OK {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	r1 := fmt.Sprint(e.Result["reservation"])
	if r1 == "" || r1 == "<nil>" {
		t.Fatalf("a reserve names the reservation its close will cite: %+v", e.Result)
	}
	if p := payloadAt(t, ld, chainCount(t, ld)-1); fmt.Sprint(p["fence"]) != fmt.Sprintf("%d", fence) {
		t.Fatalf("the derived fence must LAND, not merely admit: %+v", p)
	}
	_, s, _ := situationOf(t, "--ledger", ld, "--key", worker)
	if !kindsOf(s.Obligations)["budget.open"] {
		t.Fatalf("reserve creates budget.open: %+v", s.Obligations)
	}

	// Settle DISCHARGES it, and derives the reservation: no flag
	// names it, and the landed payload cites the reserve's position.
	e, code = runEnv(t, "budget", "settle", "--ledger", ld, "--key", worker, "--subject", "c-1", "--actuals", "4")
	if code != 0 || !e.OK || fmt.Sprint(e.Result["reservation"]) != r1 {
		t.Fatalf("settle derives the sole open reservation: %d %+v", code, e)
	}
	if p := payloadAt(t, ld, chainCount(t, ld)-1); fmt.Sprint(p["reservation"]) != r1 {
		t.Fatalf("the derived reservation must land: %+v", p)
	}
	_, s, _ = situationOf(t, "--ledger", ld, "--key", worker)
	if kindsOf(s.Obligations)["budget.open"] {
		t.Fatalf("settle discharges budget.open: %+v", s.Obligations)
	}

	// Release is the other close, and refuses to record spend.
	if e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", worker, "--subject", "c-1", "--amount", "5"); code != 0 {
		t.Fatalf("second reserve: %d %+v", code, e)
	}
	if e, code := runEnv(t, "budget", "release", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--actuals", "3"); code != envelopeUsageExit || !strings.Contains(e.Error.Message, "budget settle") {
		t.Fatalf("release records no spend and says which verb does: %d %+v", code, e)
	}
	if e, code := runEnv(t, "budget", "release", "--ledger", ld, "--key", worker, "--subject", "c-1"); code != 0 || !e.OK {
		t.Fatalf("release: %d %+v", code, e)
	}
	_, s, _ = situationOf(t, "--ledger", ld, "--key", worker)
	if kindsOf(s.Obligations)["budget.open"] {
		t.Fatalf("release discharges budget.open: %+v", s.Obligations)
	}

	// claim release DISCHARGES claim.held and re-readies the subject.
	pkt := writePacket(t, "")
	e, code = runEnv(t, "claim", "release", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--packet", pkt, "--base", rng)
	if code != 0 || !e.OK {
		t.Fatalf("claim release: %d %+v", code, e)
	}
	if p := payloadAt(t, ld, chainCount(t, ld)-1); fmt.Sprint(p["fence"]) != fmt.Sprintf("%d", fence) {
		t.Fatalf("the exit cites the derived fence: %+v", p)
	}
	_, s, _ = situationOf(t, "--ledger", ld, "--key", worker)
	if kindsOf(s.Obligations)["claim.held"] {
		t.Fatalf("release discharges claim.held: %+v", s.Obligations)
	}

	// claim park is the other exit: it discharges claim.held too, and
	// leaves the contract blocked.
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "claim", "park", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--packet", pkt, "--base", rng); code != 0 || !e.OK {
		t.Fatalf("claim park: %d %+v", code, e)
	}
	_, s, _ = situationOf(t, "--ledger", ld, "--key", worker)
	if kindsOf(s.Obligations)["claim.held"] {
		t.Fatalf("park discharges claim.held: %+v", s.Obligations)
	}

	// submission make does BOTH: it discharges the holder's
	// claim.held and raises submission.pending for the verifier lane.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "contract.unblocked", "--subject", "c-1", "--payload", `{}`); code != 0 {
		t.Fatalf("unblock: %d %+v", code, e)
	}
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "submission", "make", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--packet", pkt, "--base", rng); code != 0 || !e.OK {
		t.Fatalf("submission make: %d %+v", code, e)
	}
	_, s, _ = situationOf(t, "--ledger", ld, "--key", worker)
	if kindsOf(s.Obligations)["claim.held"] {
		t.Fatalf("the submission discharges the holder's claim.held: %+v", s.Obligations)
	}
	_, vs, _ := situationOf(t, "--ledger", ld, "--key", keys["verifier"])
	if !kindsOf(vs.Obligations)["submission.pending"] {
		t.Fatalf("the submission raises submission.pending for the verifier lane: %+v", vs.Obligations)
	}
}

// envelopeUsageExit is the CLI's "your invocation cannot be resolved"
// code, which the ambiguous and contradictory invocations take.
const envelopeUsageExit = 64

// conformance: III.I — a refusal answers "not that" and "then what
// may I do?" in one envelope: the boundary's own error beside the
// caller's current affordances, with nothing appended.
func TestLoopRefusalCarriesAffordances(t *testing.T) {
	ld, _, _, _, _, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	before := chainCount(t, ld)

	// The small class carries 100 units, so this reserve is refused
	// by the boundary itself.
	e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", worker, "--subject", "c-1", "--amount", "1000000")
	if code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "exceeds remaining") {
		t.Fatalf("the boundary's own error must reach the caller: %d %+v", code, e)
	}
	if len(e.Affordances) == 0 {
		t.Fatalf("a refusal says what IS legal: %+v", e)
	}
	if !contains(e.Affordances, "budget.reserve") {
		t.Fatalf("the affordances are the caller's on this subject: %+v", e.Affordances)
	}
	if e.Position == nil {
		t.Fatalf("a refusal at the boundary is position-stamped: %+v", e)
	}
	if after := chainCount(t, ld); after != before {
		t.Fatalf("a refused act appends nothing: %d then %d", before, after)
	}

	// The refusal is journaled with the envelope's own code: a lane
	// that cannot act is exactly the affordance gap the metric
	// measures (spec/refusals.md).
	if !journalHas(t, ld, "budget.reserve", "refused") {
		t.Fatal("the refused attempt is journaled")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// journalHas reports whether the attempts journal carries a line for
// the verb with the outcome.
func journalHas(t *testing.T, ld, verb, outcome string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ld, "attempts.jsonl"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var e struct{ Verb, Outcome string }
		if json.Unmarshal([]byte(line), &e) == nil && e.Verb == verb && e.Outcome == outcome {
			return true
		}
	}
	return false
}

// conformance: III.I — an underivable argument refuses naming the
// missing fact and what establishes it; an ambiguous one names the
// candidates rather than choosing.
func TestLoopReservationDerivationRefusals(t *testing.T) {
	ld, _, _, _, _, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}

	// None: the refusal names what would establish one.
	e, code := runEnv(t, "budget", "settle", "--ledger", ld, "--key", worker, "--subject", "c-1", "--actuals", "1")
	if code != 4 || e.Error == nil || !strings.Contains(e.Error.Message, "budget reserve") {
		t.Fatalf("a missing fact names what establishes it: %d %+v", code, e)
	}

	// Two: the refusal names both candidates and declines to choose.
	var positions []string
	for _, amount := range []string{"3", "4"} {
		e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", worker, "--subject", "c-1", "--amount", amount)
		if code != 0 {
			t.Fatalf("reserve %s: %d %+v", amount, code, e)
		}
		positions = append(positions, fmt.Sprint(e.Result["reservation"]))
	}
	before := chainCount(t, ld)
	e, code = runEnv(t, "budget", "settle", "--ledger", ld, "--key", worker, "--subject", "c-1", "--actuals", "1")
	if code != envelopeUsageExit || e.Error == nil {
		t.Fatalf("an ambiguous close refuses: %d %+v", code, e)
	}
	for _, pos := range positions {
		if !strings.Contains(e.Error.Message, "position "+pos) {
			t.Fatalf("the refusal names candidate %s: %q", pos, e.Error.Message)
		}
	}
	if after := chainCount(t, ld); after != before {
		t.Fatalf("an ambiguous close appends nothing: %d then %d", before, after)
	}
}

// conformance: III.F — a malformed packet is refused at the door, so
// it never becomes a signed record; and the base range is completed
// from --base or from the repository, never invented.
func TestLoopPacketValidatedBeforeSigning(t *testing.T) {
	ld, _, base, _, head, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	before := chainCount(t, ld)

	// A packet missing a part refuses naming the part.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"acceptance": ["x"], "decisions": [], "base": "a..b", "refs": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "claim", "release", "--ledger", ld, "--key", worker, "--subject", "c-1", "--packet", bad)
	if code != envelopeUsageExit || e.Error == nil || !strings.Contains(e.Error.Message, "findings") {
		t.Fatalf("a malformed packet refuses naming the part: %d %+v", code, e)
	}
	if after := chainCount(t, ld); after != before {
		t.Fatalf("a malformed packet never becomes a record: %d then %d", before, after)
	}

	// A base named nowhere refuses saying so, rather than inventing
	// a range.
	e, code = runEnv(t, "claim", "release", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--packet", writePacket(t, ""))
	if code != envelopeUsageExit || e.Error == nil || !strings.Contains(e.Error.Message, "base") {
		t.Fatalf("an absent base refuses naming it: %d %+v", code, e)
	}

	// A packet and a flag that disagree refuse rather than have a
	// winner picked for them.
	e, code = runEnv(t, "claim", "release", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--packet", writePacket(t, base+".."+head), "--base", "dead..beef")
	if code != envelopeUsageExit || e.Error == nil || !strings.Contains(e.Error.Message, "one range") {
		t.Fatalf("a disagreement refuses: %d %+v", code, e)
	}
	if after := chainCount(t, ld); after != before {
		t.Fatalf("nothing was appended by any refusal: %d then %d", before, after)
	}
}

// clonedRepo makes a clone whose origin/HEAD git itself set, with one
// commit beyond the default branch: the shape --repo derives from.
func clonedRepo(t *testing.T) (dir, want string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	git := func(where string, args ...string) string {
		t.Helper()
		full := append([]string{"-C", where, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", origin).CombinedOutput(); err != nil {
		t.Fatalf("bare: %v %s", err, out)
	}
	hardenGitRepo(t, origin)
	if out, err := exec.Command("git", "init", "-q", "-b", "main", seed).CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	hardenGitRepo(t, seed)
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(seed, "add", ".")
	git(seed, "commit", "--quiet", "-m", "base")
	git(seed, "push", "--quiet", origin, "main")
	dir = filepath.Join(root, "work")
	if out, err := exec.Command("git", "clone", "-q", origin, dir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	hardenGitRepo(t, dir)
	mb := git(dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(dir, "add", ".")
	git(dir, "commit", "--quiet", "-m", "work")
	return dir, mb + ".." + git(dir, "rev-parse", "HEAD")
}

// conformance: III.I — the resume range is a fact the repository
// holds, so --repo derives it and the derived range lands verbatim.
func TestLoopBaseDerivedFromRepo(t *testing.T) {
	ld, _, _, _, _, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	repo, want := clonedRepo(t)
	if e, code := runEnv(t, "claim", "release", "--ledger", ld, "--key", worker, "--subject", "c-1",
		"--packet", writePacket(t, ""), "--repo", repo); code != 0 || !e.OK {
		t.Fatalf("release with a derived base: %d %+v", code, e)
	}
	p := payloadAt(t, ld, chainCount(t, ld)-1)
	pkt, ok := p["packet"].(map[string]any)
	if !ok || fmt.Sprint(pkt["base"]) != want {
		t.Fatalf("the derived range lands verbatim, wanted %q: %+v", want, p)
	}
}

// materializeRemote checks the remote ledger out locally so the read
// surfaces can be pointed at it.
func materializeRemote(t *testing.T, remote string) string {
	t.Helper()
	c, err := gitref.NewClient(t.TempDir(), remote, remoteRef)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := c.Materialize(tip, work); err != nil {
		t.Fatal(err)
	}
	return work
}

// conformance: III.F — claiming is online-only: the exclusive verb
// refuses the local path with the one account the raw seam gives,
// takes the claim through the remote, names the fence it established,
// and the rival's claim loses at admission.
func TestClaimTakeIsRemoteOnly(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	remote := bareRemote(t)
	resolve := seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "seed/1"}`)
	state := filepath.Join(dir, "state")
	wkey, wpub, wfp := writeWorkerKey(t, 31)
	rkey, rpub, rfp := writeWorkerKey(t, 32)

	for _, s := range []struct{ key, verb, subject, payload string }{
		{priv, "actor.enrolled", wfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "worker"}`, wpub)},
		{priv, "actor.granted", wfp, `{"capability": "claim"}`},
		{priv, "actor.enrolled", rfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "rival"}`, rpub)},
		{priv, "actor.granted", rfp, `{"capability": "claim"}`},
		{priv, "intent.filed", "c-1", `{"intent": "fix", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
			"--key", s.key, "--verb", s.verb, "--subject", s.subject, "--payload", s.payload); code != 0 {
			t.Fatalf("%s: %d %+v", s.verb, code, e)
		}
	}

	// The local path refuses with the online-only account.
	e, code := runEnv(t, "claim", "take", "--ledger", filepath.Join(dir, "nowhere"), "--key", wkey, "--subject", "c-1")
	if code != 2 || e.Error == nil || !strings.Contains(e.Error.Message, "online-only") {
		t.Fatalf("claim take refuses the local path for the online-only reason: %d %+v", code, e)
	}

	// The remote path takes it, and the response names the fence
	// every holder-signed event that follows must cite.
	e, code = runEnv(t, "claim", "take", "--remote", remote, "--state", state, "--key", wkey, "--subject", "c-1")
	if code != 0 || !e.OK {
		t.Fatalf("claim take: %d %+v", code, e)
	}
	fence := fmt.Sprint(e.Result["fence"])
	if fence == "" || fence == "<nil>" {
		t.Fatalf("the take names the fence it established: %+v", e.Result)
	}

	// The claim CREATES claim.held for the holder, read back off the
	// materialized remote.
	work := materializeRemote(t, remote)
	_, s, _ := situationOf(t, "--ledger", work, "--key", wkey)
	if !kindsOf(s.Obligations)["claim.held"] {
		t.Fatalf("claim take creates claim.held: %+v", s.Obligations)
	}
	if len(s.Windows) != 1 || fmt.Sprint(s.Windows[0]["fence"]) != fence {
		t.Fatalf("the window carries the fence the take named: %+v", s.Windows)
	}

	// The rival loses at admission, and the refusal carries its
	// affordances on the subject.
	before := remoteTip(t, remote)
	e, code = runEnv(t, "claim", "take", "--remote", remote, "--state", filepath.Join(dir, "rival"),
		"--key", rkey, "--subject", "c-1")
	if code != 2 || e.Error == nil {
		t.Fatalf("the rival's claim loses with the structured contention refusal: %d %+v", code, e)
	}
	if after := remoteTip(t, remote); after != before {
		t.Fatalf("a refused claim pushes nothing")
	}

	// The remote path carries the loop's other acts too, deriving
	// the fence from the materialized remote tip rather than from
	// any local copy.
	if e, code := runEnv(t, "budget", "reserve", "--remote", remote, "--state", state,
		"--key", wkey, "--subject", "c-1", "--amount", "7"); code != 0 || !e.OK {
		t.Fatalf("remote reserve: %d %+v", code, e)
	}
	if e, code := runEnv(t, "budget", "settle", "--remote", remote, "--state", state,
		"--key", wkey, "--subject", "c-1", "--actuals", "2"); code != 0 || !e.OK {
		t.Fatalf("remote settle derives the reservation from the remote tip: %d %+v", code, e)
	}
	if e, code := runEnv(t, "submission", "make", "--remote", remote, "--state", state,
		"--key", wkey, "--subject", "c-1", "--packet", writePacket(t, "1234567..7654321")); code != 0 || !e.OK {
		t.Fatalf("remote submission: %d %+v", code, e)
	}
	work = materializeRemote(t, remote)
	_, s, _ = situationOf(t, "--ledger", work, "--key", wkey)
	if kindsOf(s.Obligations)["claim.held"] || kindsOf(s.Obligations)["budget.open"] {
		t.Fatalf("the remote loop discharges what it opened: %+v", s.Obligations)
	}
}

// conformance: III.I — every loop verb states the whole shared shape
// when its invocation cannot be resolved.
func TestLoopUsageRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"claim take without a subject", []string{"claim", "take", "--remote", "r"}, "--subject"},
		{"claim release without a packet", []string{"claim", "release", "--ledger", "l", "--key", "k", "--subject", "c"}, "--packet"},
		{"budget reserve without an amount", []string{"budget", "reserve", "--ledger", "l", "--key", "k", "--subject", "c"}, "--amount"},
		{"budget settle without actuals", []string{"budget", "settle", "--ledger", "l", "--key", "k", "--subject", "c"}, "--actuals"},
		{"both transports", []string{"claim", "park", "--ledger", "l", "--remote", "r", "--key", "k", "--subject", "c", "--packet", "p"}, "not both"},
		{"unknown claim subverb", []string{"claim", "steal"}, "take, release, or park"},
		{"unknown budget subverb", []string{"budget", "burn"}, "status, reserve, settle, or release"},
		{"unknown submission subverb", []string{"submission", "revoke"}, "make"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, code := runEnv(t, tc.args...)
			if code != envelopeUsageExit || e.Error == nil || !strings.Contains(e.Error.Message, tc.want) {
				t.Fatalf("wanted a usage refusal naming %q: %d %+v", tc.want, code, e)
			}
		})
	}
}

// loopRemote stands up a remote with one claimed subject and the
// worker enrolled, returning the remote, the state dir, the worker's
// key path and the resolver for library-side rival appends.
func loopRemote(t *testing.T) (remote, state, wkey string, resolve ledger.Resolver) {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	remote = bareRemote(t)
	resolve = seedRemoteGenesis(t, remote)
	libAppend(t, remote, resolve, "seed/0", ledger.UpgradeVerb, "system", `{"to": "seed/1"}`)
	state = filepath.Join(dir, "state")
	var wpub, wfp string
	wkey, wpub, wfp = writeWorkerKey(t, 41)
	for _, s := range []struct{ key, verb, subject, payload string }{
		{priv, "actor.enrolled", wfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "worker"}`, wpub)},
		{priv, "actor.granted", wfp, `{"capability": "claim"}`},
		{priv, "intent.filed", "c-1", `{"intent": "fix", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{priv, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--remote", remote, "--state", state,
			"--key", s.key, "--verb", s.verb, "--subject", s.subject, "--payload", s.payload); code != 0 {
			t.Fatalf("%s: %d %+v", s.verb, code, e)
		}
	}
	if e, code := runEnv(t, "claim", "take", "--remote", remote, "--state", state,
		"--key", wkey, "--subject", "c-1"); code != 0 {
		t.Fatalf("claim take: %d %+v", code, e)
	}
	return remote, state, wkey, resolve
}

// conformance: III.I — a derived argument is re-examined against the
// refreshed tip inside the optimistic loop. A rival reservation
// landing mid-flight makes the drafted citation ambiguous rather than
// merely stale, and the act is REFUSED naming both candidates: the
// value is never silently replaced, because a different value is a
// different decision (plans/os-9b3f3ef3.md D1).
func TestRemoteDerivationDivergenceRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rival lands through a pre-receive hook, which needs a POSIX git server (spec/platform.md)")
	}
	remote, state, wkey, resolve := loopRemote(t)

	// One open reservation: the value `budget settle` will derive.
	if e, code := runEnv(t, "budget", "reserve", "--remote", remote, "--state", state,
		"--key", wkey, "--subject", "c-1", "--amount", "5"); code != 0 {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	// A SECOND reservation, staged and rewound: the hook lands it when
	// the settle's first push attempt arrives, so the session drafts
	// against one reservation and the retry sees two.
	rival := buildRivalReserve(t, remote, resolve)
	installRivalHook(t, remote, rival)

	before := remoteTip(t, remote)
	e, code := runEnv(t, "budget", "settle", "--remote", remote, "--state", state,
		"--key", wkey, "--subject", "c-1", "--actuals", "2")
	if code == 0 {
		t.Fatalf("a settle whose sole reservation stopped being sole must refuse, not close one: %+v", e)
	}
	if e.Error == nil || !strings.Contains(e.Error.Message, "open reservations") {
		t.Fatalf("the refusal must name the ambiguity soleOpenReservation exists to refuse: %d %+v", code, e)
	}
	// Both candidates named, and nothing of ours landed: the rival's
	// own append is the only advance.
	for _, want := range []string{"position"} {
		if !strings.Contains(e.Error.Message, want) {
			t.Fatalf("the refusal must name the candidates: %q", e.Error.Message)
		}
	}
	if after := remoteTip(t, remote); after == before {
		t.Fatal("fixture: the rival must have landed, or the race never happened")
	}
	if settled := chainHasVerb(t, remote, resolve, "budget.settle"); settled {
		t.Fatal("a refused settle must append nothing")
	}

	// D3: the refusal is stamped at the view it was COMPUTED at, not
	// at the tip the session opened against. remoteFailureEnvelope
	// stamps the refreshed position through refusalAt, and refuse used
	// to overwrite it with the session's own count.
	if e.Position == nil {
		t.Fatal("a remote refusal must carry a position")
	}
	fresh := remoteState(t, remote).count - 1
	if *e.Position != fmt.Sprintf("%d", fresh) {
		t.Fatalf("the refusal must be stamped at the refreshed tip %d, got %s: a stale stamp is the concurrency signal the field exists for, inverted", fresh, *e.Position)
	}
}

// buildRivalReserve stages one further budget.reserve on c-1 and
// rewinds, returning the commit for the hook to replay.
func buildRivalReserve(t *testing.T, remote string, resolve ledger.Resolver) []string {
	t.Helper()
	base := remoteTip(t, remote)
	// The operator lane may reserve on a claimed subject, so the
	// governance root can play the rival without a second worker.
	libAppend(t, remote, resolve, version.Seed1, transition.BudgetReserveVerb, "c-1",
		fmt.Sprintf(`{"amount": "3", "fence": "%s"}`, remoteFence(t, remote, resolve)))
	rival := remoteTip(t, remote)
	if out, err := exec.Command("git", "--git-dir", remote, "update-ref", remoteRef, base).CombinedOutput(); err != nil {
		t.Fatalf("rewind: %v %s", err, out)
	}
	return []string{rival}
}

// remoteState materializes the remote ledger and folds it, the read
// the drills use to ask what the authoritative chain now says.
func remoteState(t *testing.T, remote string) *verdictState {
	t.Helper()
	st, failEnv := loadVerdictState(materializeRemote(t, remote))
	if failEnv != nil {
		t.Fatalf("materialized remote does not load: %+v", failEnv)
	}
	return st
}

// remoteFence is the active claim fence on c-1 at the remote tip.
func remoteFence(t *testing.T, remote string, _ ledger.Resolver) string {
	t.Helper()
	st := remoteState(t, remote)
	s, ok := st.fold.State("c-1")
	if !ok || s.Claim == nil {
		t.Fatal("fixture: c-1 holds no claim")
	}
	return fmt.Sprintf("%d", s.Claim.Fence)
}

// chainHasVerb reports whether the remote chain carries the verb.
func chainHasVerb(t *testing.T, remote string, _ ledger.Resolver, verb string) bool {
	t.Helper()
	for _, rec := range remoteState(t, remote).records {
		if rec.Event.Verb == verb {
			return true
		}
	}
	return false
}

// conformance: III.I — a malformed packet is refused with the
// documented usage envelope and never terminates the CLI. The JSON
// value null is the case the object-only malformed-packet drills
// missed: it unmarshals into a nil map with no error, and writing a
// derived base into a nil map panics.
func TestLoopPacketRootMustBeAnObject(t *testing.T) {
	ld, _, base, _, head, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatal(err)
	}
	before := chainCount(t, ld)
	for _, body := range []string{`null`, `[]`, `"x"`, `3`} {
		path := filepath.Join(t.TempDir(), "packet.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		// With --base is the combination that panicked: the derived
		// range is written into the map before the shape is checked.
		for _, extra := range [][]string{{}, {"--base", base + ".." + head}} {
			args := append([]string{"claim", "release", "--ledger", ld, "--key", worker,
				"--subject", "c-1", "--packet", path}, extra...)
			e, code := runEnv(t, args...)
			if code != envelopeUsageExit || e.Error == nil {
				t.Fatalf("--packet %s (%v) must refuse with the usage envelope: %d %+v", body, extra, code, e)
			}
		}
	}
	if after := chainCount(t, ld); after != before {
		t.Fatalf("no malformed packet may reach the chain: %d then %d", before, after)
	}
}
