package main

// The offer drills end-to-end (plans/os-c61c3392.md;
// spec/offers.md): the wakeless poll-only run through the whole
// loop, the duplicate-scheduling race settling at admission, and the
// list surface's liveness and eligibility filters — expiry,
// consumption, tier and capability scoping, and foreign-offer
// inertness.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// admitAppend runs the online claim sequence locally: context at the
// tip, the full admission Check, then the append with the context's
// resolver — the same authority the --remote path enforces, in one
// synchronous process. It returns the admission refusal instead of
// failing, so the race drill can assert the loser's contention.
func admitAppend(t *testing.T, ld string, key ed25519.PrivateKey, verb, subject, payload string) (int, error) {
	t.Helper()
	store, err := ledger.Open(ld)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := admit.ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: ctx.Active, TS: time.Now().UTC().Format(time.RFC3339), Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: ctx.Tip,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := admit.Check(ctx, rec); err != nil {
		return -1, err
	}
	pos, err := store.Append(rec, ctx.Resolve)
	if err != nil {
		t.Fatalf("admitted append %s: %v", verb, err)
	}
	return pos, nil
}

func workerRawKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func offerLedger(t *testing.T) (ld, src, base, specCommit, head, priv string, rootKey ed25519.PrivateKey, keys map[string]string, fps map[string]string) {
	t.Helper()
	dir, priv, _ := writeKeys(t)
	ld = filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head = verdictRepo(t)
	keys, fps = map[string]string{}, map[string]string{}
	pubs := map[string]string{}
	for name, first := range map[string]byte{"supervisor": 21, "workerA": 22, "workerB": 23, "verifier": 24} {
		path, pub, fp := writeWorkerKey(t, first)
		keys[name], pubs[name], fps[name] = path, pub, fp
	}
	steps := [][]string{
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed1 + `"}`},
	}
	for _, name := range []string{"supervisor", "workerA", "workerB", "verifier"} {
		steps = append(steps, []string{"actor.enrolled", fps[name],
			fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, pubs[name], name)})
	}
	for name, cap := range map[string]string{"supervisor": "supervise", "workerA": "claim", "workerB": "claim", "verifier": "verdict"} {
		steps = append(steps, []string{"actor.granted", fps[name], `{"capability": "` + cap + `"}`})
	}
	for _, step := range steps {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey = ed25519.NewKeyFromSeed(seed)
	return
}

func offerFile(t *testing.T, ld, priv, specCommit, subject string) {
	t.Helper()
	for _, step := range [][]string{
		{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": true, "gate": "pr/6 @ %s"}}`, specCommit, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", subject, "--payload", step[1]); code != 0 {
			t.Fatalf("%s %s: %d %+v", subject, step[0], code, e)
		}
	}
}

func listOffers(t *testing.T, ld, actor string, now string) []any {
	t.Helper()
	args := []string{"offer", "list", "--ledger", ld, "--actor", actor}
	if now != "" {
		args = append(args, "--now", now)
	}
	e, code := runEnv(t, args...)
	if code != 0 {
		t.Fatalf("offer list: %d %+v", code, e)
	}
	offers, _ := e.Result["offers"].([]any)
	return offers
}

// conformance: III.H — the scheduling model is offers-and-claims:
// eligibility-scoped offers, workers pull and claim, claims settle at
// admission, and wake is advisory transport whose total failure costs
// only latency — this run has no wake channel at all, only polling,
// and reaches done. Duplicate scheduling is impossible because
// exclusivity settles at admission: the race's loser gets the
// structured contention refusal.
func TestWakelessPollOnlyRun(t *testing.T) {
	ld, src, base, _, head, priv, _, keys, fps := offerLedgerAndSubject(t, "c-1")
	rng := base + ".." + head

	// The worker's poll finds the published offer with no wake ever
	// sent: eligibility-scoped to the claim capability and the
	// contract's trivial tier.
	if e, code := runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-1",
		"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z",
		"--capability", "claim", "--tier", "trivial"); code != 0 {
		t.Fatalf("the supervisor's offer publishes: %d %+v", code, e)
	}
	offers := listOffers(t, ld, fps["workerA"], "")
	if len(offers) != 1 {
		t.Fatalf("the polling worker sees the offer: %+v", offers)
	}
	if row, _ := offers[0].(map[string]any); row["subject"] != "c-1" || row["tier"] != "trivial" {
		t.Fatalf("the row names the subject and its tier: %+v", offers[0])
	}

	// Pull and claim: the claim settles at admission like any claim
	// (the CLI holds exclusive verbs to the online path, so the drill
	// runs the same admission sequence through the library).
	fencePos, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("the poller's claim admits: %v", err)
	}
	fence := fmt.Sprintf("%d", fencePos)

	// The rest of the loop runs to done on polling alone.
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "submission.made", "--subject", "c-1", "--payload", fmt.Sprintf(
			`{"fence": "%s", "packet": {"acceptance": ["c-1 ok"], "decisions": [], "base": %q, "refs": [], "findings": []}}`, fence, rng))
	if code != 0 {
		t.Fatalf("submission: %d %+v", code, e)
	}
	e, code = runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src,
		"--key", keys["verifier"], "--verdict", "pass")
	if code != 0 {
		t.Fatalf("verdict: %d %+v", code, e)
	}
	verdictPos := *e.Position
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "merge.requested", "--subject", "c-1", "--payload", `{"verdict": "`+verdictPos+`"}`); code != 0 {
		t.Fatalf("request: %d %+v", code, e)
	}
	if e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
		"--verb", "merge.observed", "--subject", "c-1", "--payload", `{"merged": "`+head+`", "pr": "pr/1"}`); code != 0 {
		t.Fatalf("observed: %d %+v", code, e)
	}

	// Done subjects list nothing and take no new offers.
	if offers := listOffers(t, ld, fps["workerA"], ""); len(offers) != 0 {
		t.Fatalf("a done subject lists no offers: %+v", offers)
	}
	if e, code = runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-1",
		"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z"); code != 3 {
		t.Fatalf("an offer on a done subject refuses as an illegal transition: %d %+v", code, e)
	}
}

func offerLedgerAndSubject(t *testing.T, subject string) (ld, src, base, specCommit, head, priv string, rootKey ed25519.PrivateKey, keys, fps map[string]string) {
	t.Helper()
	ld, src, base, specCommit, head, priv, rootKey, keys, fps = offerLedger(t)
	offerFile(t, ld, priv, specCommit, subject)
	return
}

// conformance: III.H — offers expire, there are no assignments to
// orphan, and duplicate scheduling is impossible because exclusivity
// settles at admission.
func TestOfferRaceExpiryAndFilters(t *testing.T) {
	ld, _, base, specCommit, head, priv, rootKey, keys, fps := offerLedgerAndSubject(t, "c-1")
	rng := base + ".." + head

	// The race: both workers see one offer, both claim, exactly one
	// admits, and the loser gets the structured contention refusal.
	if e, code := runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-1",
		"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z", "--capability", "claim"); code != 0 {
		t.Fatalf("publish: %d %+v", code, e)
	}
	for _, w := range []string{"workerA", "workerB"} {
		if n := len(listOffers(t, ld, fps[w], "")); n != 1 {
			t.Fatalf("%s sees the offer before the race: %d", w, n)
		}
	}
	if _, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("the first claim wins: %v", err)
	}
	var ce *admit.ContentionError
	if _, err := admitAppend(t, ld, workerRawKey(23), "claim.taken", "c-1", `{}`); err == nil || !errors.As(err, &ce) {
		t.Fatalf("the second claim loses at admission with the structured contention refusal: %v", err)
	}

	// The re-ready regression: release re-readies the subject inside
	// the expiry window, and the taken offer stays consumed while a
	// fresh publication lists.
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "claim.released", "--subject", "c-1", "--payload",
		fmt.Sprintf(`{"fence": %q, "packet": {"acceptance": ["untouched"], "decisions": [], "base": %q, "refs": [], "findings": []}}`,
			claimFence(t, ld, "c-1"), rng)); code != 0 {
		t.Fatalf("release: %d %+v", code, e)
	}
	if offers := listOffers(t, ld, fps["workerB"], ""); len(offers) != 0 {
		t.Fatalf("the taken offer never resurrects on re-ready: %+v", offers)
	}
	if e, code := runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-1",
		"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z"); code != 0 {
		t.Fatalf("a fresh offer on the re-readied subject admits: %d %+v", code, e)
	}
	if offers := listOffers(t, ld, fps["workerB"], ""); len(offers) != 1 {
		t.Fatalf("the fresh publication lists: %+v", offers)
	}

	// Expiry is a listing predicate: beyond the expiry instant the
	// offer hides, and the subject stays claimable — offers never
	// gate claims, so nothing orphans.
	if offers := listOffers(t, ld, fps["workerB"], "2027-06-01T00:00:00Z"); len(offers) != 0 {
		t.Fatalf("an expired offer hides: %+v", offers)
	}
	if _, err := admitAppend(t, ld, workerRawKey(23), "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("a claim with no live offer still admits: %v", err)
	}

	// Eligibility scoping on a second subject: capability and tier
	// mismatches hide the offer from the wrong workers, unknown
	// actors see nothing, and a raw-pushed foreign offer is inert.
	offerFile(t, ld, priv, specCommit, "c-2")
	if e, code := runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-2",
		"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z", "--capability", "verdict"); code != 0 {
		t.Fatalf("verdict-scoped publish: %d %+v", code, e)
	}
	if offers := listOffers(t, ld, fps["workerB"], ""); len(offers) != 0 {
		t.Fatalf("a claim-only worker never sees a verdict-scoped offer: %+v", offers)
	}
	if offers := listOffers(t, ld, fps["verifier"], ""); len(offers) != 1 {
		t.Fatalf("the verdict-granted key sees it: %+v", offers)
	}
	// Operator standing satisfies every scope (a root's implicit
	// operator included): admission lets the operator take any
	// offered work, so the polling surface must let it discover it.
	rootFP, err := event.Fingerprint(rootKey.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if offers := listOffers(t, ld, rootFP, ""); len(offers) != 1 {
		t.Fatalf("the root's implicit operator sees every scope: %+v", offers)
	}
	if e, code := runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-2",
		"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z", "--tier", "standard"); code != 0 {
		t.Fatalf("tier-scoped publish: %d %+v", code, e)
	}
	if offers := listOffers(t, ld, fps["workerB"], ""); len(offers) != 0 {
		t.Fatalf("a trivial contract never matches a standard-scoped offer: %+v", offers)
	}
	if offers := listOffers(t, ld, "deadbeef", ""); len(offers) != 0 {
		t.Fatalf("an unknown actor sees an empty list: %+v", offers)
	}

	// Born-dead offers refuse deterministically at publish.
	if e, code := runEnv(t, "offer", "publish", "--ledger", ld, "--subject", "c-2",
		"--key", keys["supervisor"], "--expires", "2020-01-01T00:00:00Z"); code == 0 {
		t.Fatalf("a born-dead offer must refuse: %+v", e)
	}

	// The foreign offer: well-shaped, raw-pushed by a plain worker
	// key holding no supervise standing — it folds as a fact and is
	// inert at the consuming surface.
	plainSeed := make([]byte, ed25519.SeedSize)
	plainSeed[0] = 22 // workerA: enrolled, active, claim-granted, never supervise
	rawAppend(t, ld, ed25519.NewKeyFromSeed(plainSeed), "offer.published", "c-2",
		`{"eligibility": {}, "expires": "2027-01-01T00:00:00Z"}`)
	if offers := listOffers(t, ld, fps["workerB"], ""); len(offers) != 0 {
		t.Fatalf("a foreign offer is inert — never listed, never authority: %+v", offers)
	}
}

// claimFence reads the active claim's fence for the subject from the
// contracts projection surface the fold exposes through offer list's
// loader.
func claimFence(t *testing.T, ld, subject string) string {
	t.Helper()
	st, failEnv := loadVerdictState(ld)
	if failEnv != nil {
		t.Fatalf("load: %+v", failEnv)
	}
	s, ok := st.fold.State(subject)
	if !ok || s.Claim == nil {
		t.Fatalf("no active claim on %s", subject)
	}
	return fmt.Sprintf("%d", s.Claim.Fence)
}

// conformance: plans/os-8e53ffd9.md D6 and AC5 — an offer may name the
// runtime tuples it wants (III.J row 3's "strongest tuples by policy",
// as a scheduling INPUT the supervisor writes): a qualified worker sees
// it only if one of its cited tuples is named, an offer naming none
// shows to every eligible worker, and the loop's poll agrees with the
// listing because it IS the listing.
func TestOfferTuplesScopeQualifiedWorkers(t *testing.T) {
	ld, _, _, specCommit, priv, rootKey, keys, fps, _ := qualifiedLedger(t)
	rootAppend(t, ld, priv, "actor.granted", fps["workerB"],
		`{"capability": "claim", "tuple": `+drillTuple(map[string]string{"model": "fable/5.2"})+`}`)
	offerFile(t, ld, priv, specCommit, "c-1")
	publish := func(subject string, tuples ...string) (ledgerEnv, int) {
		t.Helper()
		args := []string{"offer", "publish", "--ledger", ld, "--subject", subject,
			"--key", keys["supervisor"], "--expires", "2027-01-01T00:00:00Z"}
		for _, tu := range tuples {
			args = append(args, "--tuple", tu)
		}
		return runEnv(t, args...)
	}
	rootFP, err := event.Fingerprint(rootKey.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	count := func(actor string) int {
		t.Helper()
		return len(listOffers(t, ld, actor, ""))
	}

	// One tuple named: workerA (cites it) sees the offer; workerB
	// (cites another) and the verifier (cites none) do not; the root's
	// implicit operator satisfies every scope, tuples included, for the
	// same reason it satisfies capabilities.
	if e, code := publish("c-1", drillTuple(nil)); code != 0 {
		t.Fatalf("publish with a tuple: %d %+v", code, e)
	}
	for name, want := range map[string]struct {
		actor string
		n     int
	}{"workerA": {fps["workerA"], 1}, "workerB": {fps["workerB"], 0}, "verifier": {fps["verifier"], 0}, "root": {rootFP, 1}} {
		if got := count(want.actor); got != want.n {
			t.Fatalf("%s sees %d tuple-scoped offers, want %d", name, got, want.n)
		}
	}
	rows := listOffers(t, ld, fps["workerA"], "")
	if tuples, _ := rows[0].(map[string]any)["tuples"].([]any); len(tuples) != 1 {
		t.Fatalf("the listing carries the offer's tuples: %+v", rows[0])
	}

	// An offer naming none shows to every eligible worker.
	if e, code := publish("c-1"); code != 0 {
		t.Fatalf("publish without tuples: %d %+v", code, e)
	}
	if count(fps["workerA"]) != 2 || count(fps["workerB"]) != 1 || count(fps["verifier"]) != 1 {
		t.Fatalf("an unscoped offer shows to every eligible worker: A %d B %d verifier %d",
			count(fps["workerA"]), count(fps["workerB"]), count(fps["verifier"]))
	}

	// Two named: both qualified workers see it.
	offerFile(t, ld, priv, specCommit, "c-2")
	if e, code := publish("c-2", drillTuple(nil), drillTuple(map[string]string{"model": "fable/5.2"})); code != 0 {
		t.Fatalf("publish with two tuples: %d %+v", code, e)
	}
	if count(fps["workerA"]) != 3 || count(fps["workerB"]) != 2 {
		t.Fatalf("an offer naming two configurations shows to a worker citing either: A %d B %d",
			count(fps["workerA"]), count(fps["workerB"]))
	}

	// The loop's poll agrees with the listing: it consumes offer list
	// rather than reinventing eligibility, so what workerB polls is
	// exactly what workerB lists.
	d, err := loop.New(implementerManifest(t), loopVerbs{}, []string{"--ledger", ld}, keys["workerB"],
		loop.WorkFunc(func(string, loop.Situation) (int, error) { return 0, nil }), loop.WithBase("a..a"))
	if err != nil {
		t.Fatal(err)
	}
	polled, res := d.Poll()
	if res.Refused() {
		t.Fatalf("poll refused: %+v", res)
	}
	var listed []string
	for _, r := range listOffers(t, ld, fps["workerB"], "") {
		listed = append(listed, r.(map[string]any)["subject"].(string))
	}
	if strings.Join(polled, ",") != strings.Join(listed, ",") {
		t.Fatalf("the poll and the listing disagree: polled %v, listed %v", polled, listed)
	}

	// A raw-pushed offer whose every tuple member is malformed is
	// listed to NOBODY (review finding on the task PR): a malformed
	// scope folds to nothing, never to an unscoped offer.
	beforeA, beforeB := count(fps["workerA"]), count(fps["workerB"])
	rawAppendAt(t, ld, workerRawKey(21), version.Seed2, "offer.published", "c-2",
		`{"eligibility": {"tuples": [{"principal": "x"}]}, "expires": "2027-01-01T00:00:00Z"}`)
	if count(fps["workerA"]) != beforeA || count(fps["workerB"]) != beforeB || count(fps["verifier"]) != 1 {
		t.Fatalf("a malformed tuple scope widens nothing: A %d B %d verifier %d", count(fps["workerA"]), count(fps["workerB"]), count(fps["verifier"]))
	}

	// A malformed --tuple refuses as usage at the door, before anything
	// is signed.
	if e, code := publish("c-2", `{"principal": "x"}`); code != 64 || e.Error == nil || !strings.Contains(e.Error.Message, "tuple") {
		t.Fatalf("a malformed --tuple refuses as usage naming the shape: %d %+v", code, e.Error)
	}

	// On a chain that never upgraded, the boundary refuses the scope
	// by version: the pre-flight carries its refusal.
	ld1, _, _, _, _, _, _, keys1, _ := offerLedgerAndSubject(t, "c-1")
	e, code := runEnv(t, "offer", "publish", "--ledger", ld1, "--subject", "c-1", "--key", keys1["supervisor"],
		"--expires", "2027-01-01T00:00:00Z", "--tuple", drillTuple(nil))
	if code == 0 || e.Error == nil || !strings.Contains(e.Error.Message, "tuple semantics activate at "+version.Seed2) {
		t.Fatalf("a tuple scope on a seed/1 chain refuses by version: %d %+v", code, e.Error)
	}
}
