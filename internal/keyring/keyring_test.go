package keyring_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func key(t testing.TB, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func fp(t testing.TB, priv ed25519.PrivateKey) string {
	t.Helper()
	f, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func rec(t testing.TB, priv ed25519.PrivateKey, verb, subject, payload string) *event.Record {
	t.Helper()
	r, err := event.Sign(event.Event{
		V: version.Seed1, TS: "2026-09-01T00:00:00Z", Actor: fp(t, priv),
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: event.EmptyHash,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func enrollPayload(t testing.TB, priv ed25519.PrivateKey, kind, name string) string {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf(`{"key": %q, "kind": %q, "name": %q}`, hex.EncodeToString(pub), kind, name)
}

func seeded(t testing.TB, root ed25519.PrivateKey, extra ...ed25519.PublicKey) *keyring.State {
	t.Helper()
	g, err := genesis.Build(root, extra, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := keyring.New()
	s.SeedGenesis(g)
	if !s.Seeded() {
		t.Fatal("genesis must seed the keyring")
	}
	return s
}

// conformance: III.E — every actor is a keypair and the keyring is a
// projection: the genesis governance root seeds it and only active
// standing resolves.
func TestSeedAndStandingResolution(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	s := seeded(t, root)
	if _, ok := s.Resolve(fp(t, root)); !ok {
		t.Fatal("the governance root must resolve")
	}
	if !s.IsActiveRoot(fp(t, root)) {
		t.Fatal("the governance root must be an active root")
	}
	if _, ok := s.Resolve(fp(t, worker)); ok {
		t.Fatal("an unenrolled key must not resolve")
	}

	if err := s.Advance(rec(t, root, "actor.enrolled", fp(t, worker), enrollPayload(t, worker, "agent", "worker"))); err != nil {
		t.Fatalf("enrollment must apply: %v", err)
	}
	if _, ok := s.Resolve(fp(t, worker)); !ok {
		t.Fatal("an enrolled key must resolve")
	}
	if s.IsActiveRoot(fp(t, worker)) {
		t.Fatal("an enrolled worker is not a governance root")
	}
	e, ok := s.Get(fp(t, worker))
	if !ok || e.Kind != "agent" || e.Name != "worker" || e.Standing != keyring.StandingActive {
		t.Fatalf("enrollment entry wrong: %+v", e)
	}
}

// The standing lifecycle: suspension pauses resolution, re-enrollment
// reinstates, revocation is terminal (recorded decisions,
// plans/os-52a2d688.md).
func TestStandingLifecycle(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	s := seeded(t, root)
	wfp := fp(t, worker)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	refuse := func(err error, phrase string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), phrase) {
			t.Fatalf("want refusal containing %q, got %v", phrase, err)
		}
	}

	must(s.Advance(rec(t, root, "actor.enrolled", wfp, enrollPayload(t, worker, "agent", "worker"))))
	refuse(s.Advance(rec(t, root, "actor.enrolled", wfp, enrollPayload(t, worker, "agent", "worker"))), "already enrolled")

	must(s.Advance(rec(t, root, "actor.suspended", wfp, `{"reason": "drift"}`)))
	if _, ok := s.Resolve(wfp); ok {
		t.Fatal("a suspended key must not resolve")
	}
	refuse(s.Advance(rec(t, root, "actor.suspended", wfp, `{"reason": "again"}`)), "already suspended")
	must(s.Advance(rec(t, root, "actor.granted", wfp, `{"capability": "claim:core"}`)))

	must(s.Advance(rec(t, root, "actor.enrolled", wfp, enrollPayload(t, worker, "agent", "worker-2"))))
	if e, _ := s.Get(wfp); e.Standing != keyring.StandingActive || e.Name != "worker-2" {
		t.Fatalf("re-enrollment must reinstate, got %+v", e)
	}
	if e, _ := s.Get(wfp); len(e.Grants) != 1 || e.Grants[0] != "claim:core" {
		t.Fatalf("grants must accumulate as data, got %+v", e.Grants)
	}

	must(s.Advance(rec(t, root, "actor.revoked", wfp, `{"reason": "compromise"}`)))
	if _, ok := s.Resolve(wfp); ok {
		t.Fatal("a revoked key must not resolve")
	}
	refuse(s.Advance(rec(t, root, "actor.enrolled", wfp, enrollPayload(t, worker, "agent", "worker"))), "terminal")
	refuse(s.Advance(rec(t, root, "actor.suspended", wfp, `{"reason": "x"}`)), "terminal")
	refuse(s.Advance(rec(t, root, "actor.granted", wfp, `{"capability": "x"}`)), "terminal")
}

// Strict payload shapes: unknown fields, trailing data, malformed keys,
// bad kinds, and subject mismatches all refuse through the one
// transition function.
func TestAdvanceShapeRefusals(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	wpub := hex.EncodeToString(worker.Public().(ed25519.PublicKey))
	wfp := fp(t, worker)
	// A payload with trailing data cannot be signed at all (JCS
	// canonicalization refuses it), so that strict-parse branch is
	// unrepresentable in a record; every representable malformation is
	// here.
	cases := map[string]struct{ verb, subject, payload string }{
		"unknown field":    {"actor.enrolled", wfp, `{"key": "` + wpub + `", "kind": "agent", "name": "w", "mode": "x"}`},
		"bad key hex":      {"actor.enrolled", wfp, `{"key": "zz", "kind": "agent", "name": "w"}`},
		"bad kind":         {"actor.enrolled", wfp, `{"key": "` + wpub + `", "kind": "robot", "name": "w"}`},
		"empty name":       {"actor.enrolled", wfp, `{"key": "` + wpub + `", "kind": "agent", "name": ""}`},
		"subject mismatch": {"actor.enrolled", "c-0001", `{"key": "` + wpub + `", "kind": "agent", "name": "w"}`},
		"empty capability": {"actor.granted", wfp, `{"capability": ""}`},
		"empty reason":     {"actor.suspended", wfp, `{"reason": ""}`},
		"unknown verb":     {"actor.qualified", wfp, `{"tuple": "x"}`},
		"not enrolled":     {"actor.revoked", "c-9999", `{"reason": "x"}`},
	}
	for name, c := range cases {
		s := seeded(t, root)
		if err := s.Advance(rec(t, root, c.verb, c.subject, c.payload)); err == nil {
			t.Errorf("%s must refuse", name)
		}
	}
}

// Root liveness (review finding on #97): no admitted transition may
// leave the keyring without an active governance root.
func TestRootLiveness(t *testing.T) {
	root1, root2 := key(t, 1), key(t, 3)
	s := seeded(t, root1)
	if err := s.Advance(rec(t, root1, "actor.revoked", fp(t, root1), `{"reason": "x"}`)); err == nil || !strings.Contains(err.Error(), "root liveness") {
		t.Fatalf("revoking the only root must refuse with root liveness, got %v", err)
	}
	if err := s.Advance(rec(t, root1, "actor.suspended", fp(t, root1), `{"reason": "x"}`)); err == nil {
		t.Fatal("suspending the only root must refuse")
	}

	s = seeded(t, root1, root2.Public().(ed25519.PublicKey))
	if err := s.Advance(rec(t, root1, "actor.suspended", fp(t, root1), `{"reason": "rotate"}`)); err != nil {
		t.Fatalf("suspending one of two roots must apply: %v", err)
	}
	if err := s.Advance(rec(t, root2, "actor.revoked", fp(t, root2), `{"reason": "x"}`)); err == nil {
		t.Fatal("ending the last remaining active root must refuse")
	}
	if err := s.Advance(rec(t, root2, "actor.enrolled", fp(t, root1), enrollPayload(t, root1, "human", "op"))); err != nil {
		t.Fatalf("re-enrolling a suspended root must reinstate it: %v", err)
	}
	if !s.IsActiveRoot(fp(t, root1)) {
		t.Fatal("a reinstated root keeps its root standing")
	}
	if err := s.Advance(rec(t, root2, "actor.revoked", fp(t, root2), `{"reason": "rotate"}`)); err != nil {
		t.Fatalf("revoking a root with another active must apply: %v", err)
	}
}

// StateAt tracks the protocol version the way verification does: actor
// events at seed/0 positions stay inert (the grandfathering boundary),
// and the upgrade verb switches enforcement on. The upgrade and genesis
// verb literals keyring mirrors are exercised against the real
// ledger.UpgradeVerb and genesis.Verb here, so a drift fails this test.
func TestStateAtVersionBoundary(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	g, err := genesis.Build(root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	seed0 := func(verb, subject, payload string) *event.Record {
		r, err := event.Sign(event.Event{
			V: version.Protocol, TS: "2026-09-01T00:00:00Z", Actor: fp(t, root),
			Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: event.EmptyHash,
		}, root)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	junk := seed0("actor.enrolled", "c-0001", `{"garbage": true}`)
	up := seed0(ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	enroll := rec(t, root, "actor.enrolled", fp(t, worker), enrollPayload(t, worker, "agent", "worker"))

	s, active, err := keyring.StateAt([]*event.Record{g, junk, up, enroll})
	if err != nil {
		t.Fatalf("grandfathered junk must stay inert: %v", err)
	}
	if active != version.Seed1 {
		t.Fatalf("StateAt must track the upgrade, got %q", active)
	}
	if _, ok := s.Resolve(fp(t, worker)); !ok {
		t.Fatal("the post-upgrade enrollment must apply")
	}
	if _, ok := s.Get("c-0001"); ok {
		t.Fatal("the seed/0 junk event must have no keyring effect")
	}

	bad := rec(t, root, "actor.enrolled", "c-0002", `{"garbage": true}`)
	if _, _, err := keyring.StateAt([]*event.Record{g, up, bad}); err == nil || !strings.Contains(err.Error(), "position 2") {
		t.Fatalf("an illegal actor event at a seed/1 position must fail with its position, got %v", err)
	}
}

// Preview is admission's dry run: it refuses what Advance would refuse
// and never mutates the real state.
func TestCloneAndPreviewIsolation(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	s := seeded(t, root)
	good := rec(t, root, "actor.enrolled", fp(t, worker), enrollPayload(t, worker, "agent", "worker"))
	if err := s.Preview(good); err != nil {
		t.Fatalf("preview of a legal event must pass: %v", err)
	}
	if _, ok := s.Resolve(fp(t, worker)); ok {
		t.Fatal("preview must not mutate the state")
	}
	if err := s.Preview(rec(t, root, "actor.revoked", fp(t, root), `{"reason": "x"}`)); err == nil {
		t.Fatal("preview must refuse what Advance refuses")
	}

	c := s.Clone()
	if err := c.Advance(good); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Resolve(fp(t, worker)); ok {
		t.Fatal("advancing a clone must not touch the original")
	}
}

func TestUnseededKeyringRefusesActorEvents(t *testing.T) {
	root := key(t, 1)
	s := keyring.New()
	if s.Seeded() {
		t.Fatal("a fresh keyring is unseeded")
	}
	err := s.Advance(rec(t, root, "actor.enrolled", fp(t, root), enrollPayload(t, root, "human", "op")))
	if err == nil || !strings.Contains(err.Error(), "governance root") {
		t.Fatalf("an unseeded keyring must refuse actor events, got %v", err)
	}
	if err := s.Advance(rec(t, root, "message.sent", "c-0001", `{"n": 1}`)); err != nil {
		t.Fatalf("non-actor verbs no-op even unseeded: %v", err)
	}
}

func TestAppliesAtSeed1AndLater(t *testing.T) {
	// A named list, never an ordering: a version this build has not
	// registered applies nothing, however it sorts (plans/os-8e53ffd9.md D8).
	if keyring.Applies(version.Protocol) || !keyring.Applies(version.Seed1) || !keyring.Applies(version.Seed2) || !keyring.Applies(version.Seed3) || keyring.Applies("seed/10") {
		t.Fatal("keyring semantics activate at seed/1 and stay on at every later registered version")
	}
	if !keyring.IsActorVerb("actor.enrolled") || keyring.IsActorVerb("message.sent") {
		t.Fatal("actor verb detection is namespace-based")
	}
}

// specVocabulary parses the normative capability table out of
// spec/actors.md ("## Capabilities"): backticked tokens are the
// data (prose asides are not), and the actor.* row covers every
// lifecycle verb. Parsing the spec, rather than keeping a second
// hard-coded table, makes a one-sided change to either side fail
// (review finding on #102).
func specVocabulary(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "actors.md"))
	if err != nil {
		t.Fatal(err)
	}
	tick := regexp.MustCompile("`([^`]+)`")
	rows := map[string][]string{}
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.HasPrefix(line, "## Capabilities")
			continue
		}
		if !in || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		verbs := tick.FindAllStringSubmatch(cells[1], -1)
		caps := tick.FindAllStringSubmatch(cells[2], -1)
		if len(verbs) == 0 || len(caps) == 0 {
			continue
		}
		var cs []string
		for _, m := range caps {
			cs = append(cs, m[1])
		}
		if verbs[0][1] == "actor.*" {
			for _, v := range []string{keyring.VerbEnrolled, keyring.VerbGranted, keyring.VerbSuspended, keyring.VerbRevoked} {
				rows[v] = cs
			}
			continue
		}
		rows[verbs[0][1]] = cs
	}
	if len(rows) == 0 {
		t.Fatal("no capability rows parsed from spec/actors.md")
	}
	return rows
}

// conformance: III.E — the capability vocabulary is pinned to the
// normative table in spec/actors.md by parsing it: the spec and
// the code cannot drift apart one-sidedly. Verb literals keyring
// mirrors stay pinned to the owning packages.
func TestCapabilityVocabulary(t *testing.T) {
	if halt.DeclareVerb != "system.halt.declared" || halt.LiftVerb != "system.halt.lifted" || ledger.UpgradeVerb != "system.protocol.upgraded" {
		t.Fatal("keyring's mirrored verb literals drifted from the owning packages")
	}
	spec := specVocabulary(t)
	for verb, caps := range spec {
		if got := keyring.AcceptedCapabilities(verb); fmt.Sprint(got) != fmt.Sprint(caps) {
			t.Errorf("%s accepts %v in code, the spec table says %v", verb, got, caps)
		}
	}
	// Completeness: every verb the code governs appears in the spec
	// table, so removing a spec row without changing the code fails too.
	for _, verb := range []string{
		"system.halt.declared", "system.halt.lifted", "system.protocol.upgraded",
		"system.checkpoint", "system.imported", keyring.VerbEnrolled, keyring.VerbGranted,
		keyring.VerbSuspended, keyring.VerbRevoked,
		"intent.filed", "contract.specified", "contract.blocked",
		"contract.unblocked", "contract.cancelled", "claim.taken",
		"claim.released", "claim.parked", "claim.reaped",
		"submission.made", "merge.observed", "check.observed", "plan.proposed", "plan.approved",
		"merge.requested", "verdict.rendered", "check.sealed",
		"contract.returned", "merge.overridden", "offer.published", "request.answered",
		"approval.granted", "approval.denied",
		"artifact.erased",
		"dependency.linked", "dependency.unlinked", "hierarchy.parented", "goal.aligned",
		"budget.reserve", "budget.settle", "budget.release",
		"run.started", "run.settled", "run.interrupted",
		"curation.deadend.recorded", "curation.hypothesis.proposed", "curation.hypothesis.contested", "curation.lesson.promoted",
	} {
		if _, ok := spec[verb]; !ok {
			t.Errorf("%s is governed by code but missing from the spec table", verb)
		}
	}
	// Ungoverned verbs need active standing only, on both sides.
	for _, verb := range []string{"message.sent", "system.genesis", "request.filed", "approval.requested"} {
		if got := keyring.AcceptedCapabilities(verb); got != nil {
			t.Errorf("%s must need active standing only, got %v", verb, got)
		}
		if _, ok := spec[verb]; ok {
			t.Errorf("%s must not appear in the spec table", verb)
		}
	}
}

func TestHasAnyCapability(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	s := seeded(t, root)
	ops := []string{keyring.CapOperator}
	if !s.HasAnyCapability(fp(t, root), ops) {
		t.Fatal("a governance root holds operator implicitly")
	}
	if s.HasAnyCapability(fp(t, worker), ops) {
		t.Fatal("an unenrolled key holds nothing")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Advance(rec(t, root, "actor.enrolled", fp(t, worker), enrollPayload(t, worker, "agent", "w"))))
	if s.HasAnyCapability(fp(t, worker), ops) {
		t.Fatal("enrollment alone grants no capabilities")
	}
	must(s.Advance(rec(t, root, "actor.granted", fp(t, worker), `{"capability": "maintenance"}`)))
	if !s.HasAnyCapability(fp(t, worker), []string{keyring.CapMaintenance, keyring.CapOperator}) {
		t.Fatal("a granted capability must satisfy an accepting set")
	}
	if s.HasAnyCapability(fp(t, worker), ops) {
		t.Fatal("maintenance is not operator")
	}
	must(s.Advance(rec(t, root, "actor.suspended", fp(t, worker), `{"reason": "x"}`)))
	if s.HasAnyCapability(fp(t, worker), []string{keyring.CapMaintenance}) {
		t.Fatal("a suspended actor holds nothing")
	}
}

// recAt is rec at a named protocol version.
func recAt(t testing.TB, priv ed25519.PrivateKey, v, verb, subject, payload string) *event.Record {
	t.Helper()
	r, err := event.Sign(event.Event{
		V: v, TS: "2026-09-01T00:00:00Z", Actor: fp(t, priv),
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: event.EmptyHash,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const qualifiedTuple = `{"principal": "acme", "harness": "local-worktree/v0", "model": "fable/5.1", "tool_policy": "default", "environment": "detached-git-worktree"}`

// conformance: plans/os-8e53ffd9.md D2, D8 and AC4 — actor.granted
// accepts an optional tuple at seed/2 positions only. At a seed/1
// position the field is unknown and refused with its position, the
// chain-validity posture actors.md gives payload shapes; at seed/2 it
// is accepted, the set holds it, and the string view of grants still
// carries the capability. Malformed tuples refuse by shape.
func TestGrantTupleActivatesAtSeed2(t *testing.T) {
	root, worker := key(t, 1), key(t, 2)
	g, err := genesis.Build(root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	wfp := fp(t, worker)
	up1 := recAt(t, root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	enroll := rec(t, root, "actor.enrolled", wfp, enrollPayload(t, worker, "agent", "worker"))
	up2 := rec(t, root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	grant := func(v, body string) *event.Record { return recAt(t, root, v, "actor.granted", wfp, body) }
	qualified := `{"capability": "claim", "tuple": ` + qualifiedTuple + `}`

	if _, _, err := keyring.StateAt([]*event.Record{g, up1, enroll, grant(version.Seed1, qualified)}); err == nil || !strings.Contains(err.Error(), "position 3") {
		t.Fatalf("a grant carrying tuple at a seed/1 position fails at its position under this build too: %v", err)
	}

	s, active, err := keyring.StateAt([]*event.Record{g, up1, enroll, up2, grant(version.Seed2, qualified)})
	if err != nil || active != version.Seed2 {
		t.Fatalf("at a seed/2 position the field is accepted: %v (%s)", err, active)
	}
	cited := s.GrantTuples(wfp, keyring.CapClaim)
	if len(cited) != 1 || cited[0].Model != "fable/5.1" || cited[0].Harness != "local-worktree/v0" {
		t.Fatalf("the set holds the cited tuple: %+v", cited)
	}
	// The string view never loses a qualified grant: this is the
	// worker's ONLY claim grant, and every existing caller reads
	// capabilities through it.
	if !s.HasAnyCapability(wfp, []string{keyring.CapClaim}) {
		t.Fatal("a qualified grant still grants the capability in the string view")
	}
	if e, ok := s.Get(wfp); !ok || len(e.Grants) != 1 || e.Grants[0] != keyring.CapClaim {
		t.Fatalf("Grants keeps the string view: %+v", e.Grants)
	}
	if s.GrantTuples(wfp, keyring.CapVerdict) != nil || s.GrantTuples("deadbeef", keyring.CapClaim) != nil {
		t.Fatal("a set is per capability and per actor; unknown ones are empty")
	}
	// The accessor returns a copy, and a clone carries the set.
	cited[0].Model = "mutated"
	if s.GrantTuples(wfp, keyring.CapClaim)[0].Model != "fable/5.1" {
		t.Fatal("GrantTuples returns a copy the caller cannot write through")
	}
	if c := s.Clone().GrantTuples(wfp, keyring.CapClaim); len(c) != 1 || c[0].Model != "fable/5.1" {
		t.Fatalf("Clone carries the tuple set: %+v", c)
	}

	// A grant with no tuple at seed/2 folds as before, and adds nothing
	// to the set; a second qualified grant accumulates.
	s, _, err = keyring.StateAt([]*event.Record{g, up1, enroll, up2, grant(version.Seed2, `{"capability": "claim"}`)})
	if err != nil || len(s.GrantTuples(wfp, keyring.CapClaim)) != 0 || !s.HasAnyCapability(wfp, []string{keyring.CapClaim}) {
		t.Fatalf("a tuple-less grant at seed/2 folds as today: %v %+v", err, s.GrantTuples(wfp, keyring.CapClaim))
	}
	s, _, err = keyring.StateAt([]*event.Record{g, up1, enroll, up2, grant(version.Seed2, qualified),
		grant(version.Seed2, strings.Replace(qualified, "fable/5.1", "fable/5.2", 1))})
	if err != nil || len(s.GrantTuples(wfp, keyring.CapClaim)) != 2 {
		t.Fatalf("grants accumulate into the set: %v %+v", err, s.GrantTuples(wfp, keyring.CapClaim))
	}

	// Malformed tuples (AC4): each fails at its position as an actor
	// event, never folds as a grant without a tuple.
	for name, bad := range map[string]string{
		"missing field": `{"capability": "claim", "tuple": {"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p"}}`,
		"empty string":  `{"capability": "claim", "tuple": {"principal": "", "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e"}}`,
		"unknown field": `{"capability": "claim", "tuple": {"principal": "acme", "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e", "extra": 1}}`,
		"non-string":    `{"capability": "claim", "tuple": {"principal": 7, "harness": "h/1", "model": "m/1", "tool_policy": "p", "environment": "e"}}`,
		"not an object": `{"capability": "claim", "tuple": "acme"}`,
		"null":          `{"capability": "claim", "tuple": null}`,
	} {
		_, _, err := keyring.StateAt([]*event.Record{g, up1, enroll, up2, grant(version.Seed2, bad)})
		if err == nil || !strings.Contains(err.Error(), "position 4") {
			t.Errorf("%s: a malformed tuple fails verification at its position: %v", name, err)
		}
	}
}
