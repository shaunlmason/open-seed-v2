package admit

// The relation facts' admission and the claim boundary's read
// (plans/os-f0ae2cdf.md D2, D3, D4, D5; spec/topology.md).

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// topologyKeys are the fixture's identities: the root signer
// (operator), a dispatcher, a claim worker, a sealer and a plain
// standing key.
type topologyKeys struct {
	signer, dispatcher, worker, sealer, plain ed25519.PrivateKey
}

// topologyFixture seeds a store at seed/1 with the keys enrolled and
// granted, files and specifies c-1, and returns a step that appends a
// record signed by ANY fixture key directly to the store (the raw
// seam, past the boundary) and re-reads the context.
func topologyFixture(t *testing.T) (*Context, topologyKeys, func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := topologyKeys{signer: signer, dispatcher: fixtureKey(t, 9), worker: fixtureKey(t, 2), sealer: fixtureKey(t, 7), plain: fixtureKey(t, 6)}
	all := []ed25519.PrivateKey{k.signer, k.dispatcher, k.worker, k.sealer, k.plain}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range all {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, e := range []struct {
		key  ed25519.PrivateKey
		name string
	}{{k.dispatcher, "dispatcher"}, {k.worker, "worker"}, {k.sealer, "sealer"}, {k.plain, "plain"}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
	}
	for _, g := range []struct {
		key ed25519.PrivateKey
		cap string
	}{{k.dispatcher, keyring.CapDispatch}, {k.worker, keyring.CapClaim}, {k.sealer, keyring.CapSealer}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, g.key), `{"capability": "`+g.cap+`"}`)
	}
	step := func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ctx := step(signer, version.Seed1, "intent.filed", "c-1", filedBody)
	ctx = step(signer, version.Seed1, "contract.specified", "c-1", specBody)
	return ctx, k, step
}

// conformance: III.F row 12 (supporting boundary) — TestTopologyRelationBoundary:
// each fact admits for dispatch and operator and refuses a plain
// worker, an unknown or terminal source, an unknown target, a self
// edge or cycle, a malformed or unknown-field payload, a duplicate
// link and an absent unlink; a claim drafted against an unresolved
// dependency or an inherited hold refuses just as the queue and the
// poll hide it; and a raw-pushed relation that never passed the
// boundary is an anomaly that shapes nothing.
func TestTopologyRelationBoundary(t *testing.T) {
	ctx, k, step := topologyFixture(t)
	dispatcher := k.dispatcher
	for _, c := range []string{"c-2", "c-3", "c-p"} {
		ctx = step(k.signer, version.Seed1, "intent.filed", c, filedBody)
		ctx = step(k.signer, version.Seed1, "contract.specified", c, specBody)
	}
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-done", filedBody)
	ctx = step(k.signer, version.Seed1, "contract.cancelled", "c-done", `{}`)

	check := func(priv ed25519.PrivateKey, verb, subject, body string) error {
		return Check(ctx, draftV(t, priv, version.Seed1, verb, subject, body, ctx.Tip))
	}
	var refusal *topology.Error
	var grant *OutOfGrantError
	// The grants: dispatch and operator admit; claim and plain refuse
	// out of grant before any graph logic.
	for _, verb := range topology.Verbs {
		body := `{"requires": "c-2"}`
		switch verb {
		case topology.ParentVerb:
			body = `{"parent": "c-p"}`
		case topology.AlignVerb:
			body = `{"mission": "docs/mission.md @ 0123456"}`
		}
		for name, priv := range map[string]ed25519.PrivateKey{"claim": k.worker, "plain": k.plain, "sealer": k.sealer} {
			if err := check(priv, verb, "c-1", body); !errors.As(err, &grant) {
				t.Fatalf("%s: %s refuses out of grant: %v", verb, name, err)
			}
		}
		if verb == topology.UndependVerb {
			continue
		}
		for name, priv := range map[string]ed25519.PrivateKey{"dispatch": dispatcher, "operator": k.signer} {
			if err := check(priv, verb, "c-1", body); err != nil {
				t.Fatalf("%s: %s admits: %v", verb, name, err)
			}
		}
	}
	// The shape and the graph rule, each refusal naming its part.
	for name, in := range map[string]struct{ verb, subject, body, want string }{
		"unknown field":   {topology.DependVerb, "c-1", `{"requires": "c-2", "why": "x"}`, "strict object {requires}"},
		"empty target":    {topology.ParentVerb, "c-1", `{"parent": ""}`, "parent names"},
		"mission prose":   {topology.AlignVerb, "c-1", `{"mission": "do better"}`, "not a commit-anchored reference"},
		"unknown source":  {topology.DependVerb, "c-none", `{"requires": "c-2"}`, "no contract by that id"},
		"terminal source": {topology.DependVerb, "c-done", `{"requires": "c-2"}`, "the contract is cancelled"},
		"unknown target":  {topology.DependVerb, "c-1", `{"requires": "c-none"}`, "c-none is no contract"},
		"self edge":       {topology.ParentVerb, "c-1", `{"parent": "c-1"}`, "cannot require or contain itself"},
		"absent unlink":   {topology.UndependVerb, "c-1", `{"requires": "c-2"}`, "does not require c-2"},
		"wrong type":      {topology.DependVerb, "c-1", `{"requires": 7}`, "strict object {requires}"},
	} {
		if err := check(dispatcher, in.verb, in.subject, in.body); !errors.As(err, &refusal) || !strings.Contains(err.Error(), in.want) {
			t.Errorf("%s refuses naming %q: %v", name, in.want, err)
		}
	}
	// A terminal target is a satisfied dependency, admitted.
	if err := check(dispatcher, topology.DependVerb, "c-1", `{"requires": "c-done"}`); err != nil {
		t.Fatalf("a terminal target is legal: %v", err)
	}
	// Land a link and a parent; the duplicate and the cycles refuse
	// against the admitted graph.
	ctx = step(dispatcher, version.Seed1, topology.DependVerb, "c-1", `{"requires": "c-2"}`)
	ctx = step(dispatcher, version.Seed1, topology.ParentVerb, "c-1", `{"parent": "c-p"}`)
	if err := check(dispatcher, topology.DependVerb, "c-1", `{"requires": "c-2"}`); !errors.As(err, &refusal) || !strings.Contains(err.Error(), "already requires") {
		t.Fatalf("a duplicate link refuses: %v", err)
	}
	if err := check(dispatcher, topology.DependVerb, "c-2", `{"requires": "c-1"}`); !errors.As(err, &refusal) || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("a dependency cycle refuses: %v", err)
	}
	if err := check(dispatcher, topology.ParentVerb, "c-p", `{"parent": "c-1"}`); !errors.As(err, &refusal) || !strings.Contains(err.Error(), "hierarchy cycle") {
		t.Fatalf("a hierarchy cycle refuses: %v", err)
	}
	if err := check(dispatcher, topology.UndependVerb, "c-1", `{"requires": "c-2"}`); err != nil {
		t.Fatalf("an existing link unlinks: %v", err)
	}
	// The facts moved no lifecycle state.
	if s, _ := ctx.Lifecycle.State("c-1"); s.State != "ready" || s.Anomalies != 0 {
		t.Fatalf("a relation is a fact, never a transition: %+v", s)
	}

	// The claim boundary: c-1 waits on c-2, so the worker's claim
	// refuses not_effectively_ready naming c-2; the table's own
	// refusals stay the table's.
	var notReady *topology.NotReadyError
	err := check(k.worker, "claim.taken", "c-1", `{}`)
	if !errors.As(err, &notReady) || !strings.Contains(err.Error(), "waits on c-2") || len(notReady.HeldBy) != 0 {
		t.Fatalf("a claim on a waiting dependent refuses naming the dependency: %v", err)
	}
	if err := check(k.worker, "claim.taken", "c-2", `{}`); err != nil {
		t.Fatalf("the requirement itself is claimable: %v", err)
	}
	if err := check(k.worker, "claim.taken", "c-done", `{}`); errors.As(err, &notReady) || err == nil {
		t.Fatalf("a terminal subject is the table's refusal, not the graph's: %v", err)
	}
	// The hold: c-p blocked holds c-1 even once c-2 closes.
	ctx = step(k.signer, version.Seed1, "contract.cancelled", "c-2", `{}`)
	if err := check(k.worker, "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("every dependency terminal makes the dependent claimable: %v", err)
	}
	ctx = step(dispatcher, version.Seed1, "contract.blocked", "c-p", `{}`)
	err = check(k.worker, "claim.taken", "c-1", `{}`)
	if !errors.As(err, &notReady) || !strings.Contains(err.Error(), "held by c-p") || len(notReady.Unresolved) != 0 {
		t.Fatalf("a claim under a blocked ancestor refuses naming the hold: %v", err)
	}
	if s, _ := ctx.Lifecycle.State("c-1"); s.State != "ready" {
		t.Fatalf("the hold rewrote no descendant: %+v", s)
	}
	ctx = step(dispatcher, version.Seed1, "contract.unblocked", "c-p", `{}`)
	if err := check(k.worker, "claim.taken", "c-1", `{}`); err != nil {
		t.Fatalf("lifting the hold exposes the descendant: %v", err)
	}

	// Laundering: a well-shaped link the raw seam lands under a key
	// with no grant is kept by nothing. c-3 requires c-1 by a plain
	// key's raw push; c-3 stays claimable and the fold names the
	// anomaly.
	ctx = step(k.plain, version.Seed1, topology.DependVerb, "c-3", `{"requires": "c-1"}`)
	d := Topology(ctx)
	if !d.EffectiveReady("c-3") || len(d.Graph.Requires("c-3")) != 0 {
		t.Fatal("an out-of-grant relation hides no work")
	}
	if len(d.Graph.Anomalies) != 1 || d.Graph.Anomalies[0].Subject != "c-3" || !strings.Contains(d.Graph.Anomalies[0].Reason, "held none of") {
		t.Fatalf("the fold names the anomaly: %+v", d.Graph.Anomalies)
	}
	if err := check(k.worker, "claim.taken", "c-3", `{}`); err != nil {
		t.Fatalf("the laundered link refuses no claim: %v", err)
	}
	// A raw-pushed cycle under the dispatcher's own key is judged at
	// its position too: c-p under c-1 would close a cycle, so it is an
	// anomaly and c-1 keeps c-p as its parent.
	ctx = step(dispatcher, version.Seed1, topology.ParentVerb, "c-p", `{"parent": "c-1"}`)
	d = Topology(ctx)
	if p, ok := d.Graph.Parent("c-p"); ok || len(d.Graph.Anomalies) != 2 || p.Target != "" {
		t.Fatalf("a raw cycle is an anomaly, not an edge: %+v %+v", p, d.Graph.Anomalies)
	}
	// Nil contexts derive as empty.
	if Topology(nil).Graph.Any() {
		t.Fatal("a nil context derives nothing")
	}
}

// conformance: III.F row 12; III.I (affordances are computed from the
// same rule set) — the relation verbs are drafted for the dispatcher
// exactly where the rule would admit them: a link and a parent while
// another open contract exists, an unlink only while a link stands,
// the mission on any open contract, and none for a claim-only key.
func TestTopologyIsAffordedWhereARelationIsLegal(t *testing.T) {
	ctx, k, step := topologyFixture(t)
	dispatcher := k.dispatcher
	listed := func(priv ed25519.PrivateKey, subject, verb string) bool {
		for _, v := range Affordances(ctx, priv, subject) {
			if v == verb {
				return true
			}
		}
		return false
	}
	// c-1 alone: no other contract to link or parent to, so only the
	// mission is drafted.
	if listed(dispatcher, "c-1", topology.DependVerb) || listed(dispatcher, "c-1", topology.ParentVerb) || listed(dispatcher, "c-1", topology.UndependVerb) {
		t.Fatal("with no other open contract nothing is linkable")
	}
	if !listed(dispatcher, "c-1", topology.AlignVerb) {
		t.Fatal("the mission is drafted on an open contract")
	}
	ctx = step(k.signer, version.Seed1, "intent.filed", "c-2", filedBody)
	if !listed(dispatcher, "c-1", topology.DependVerb) || !listed(dispatcher, "c-1", topology.ParentVerb) || listed(dispatcher, "c-1", topology.UndependVerb) {
		t.Fatal("another open contract makes the link and the parent draftable, not yet the unlink")
	}
	ctx = step(dispatcher, version.Seed1, topology.DependVerb, "c-1", `{"requires": "c-2"}`)
	if !listed(dispatcher, "c-1", topology.UndependVerb) {
		t.Fatal("a standing link makes the unlink draftable")
	}
	for _, verb := range topology.Verbs {
		if listed(k.worker, "c-1", verb) {
			t.Fatalf("%s is never drafted for a claim-only key", verb)
		}
	}
}
