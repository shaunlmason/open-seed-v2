package main

// The serve side of the declaration read (os-0f924157 D3, AC3): the
// service's proposal dry-run refuses the SAME above-ceiling claim its
// hook would and admits an at-ceiling one, because `judge` fetches the
// deployment's default branch into the mirror before it runs the hook's
// `admitUpdate` over it. Without that fetch the mirror's default branch
// is unborn, the declaration read finds nothing, and the judge admits
// what the hook on the remote refuses — the one-sided enforcement the
// declaration exists to close.

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/propose"
)

// conformance: AC3 — serve judges what the hook would, under the
// declaration: after the explicit default-branch fetch the service's
// dry-run refuses the above-ceiling claim its hook would and admits an
// at-ceiling proposal.
func TestServiceCeilingRefusesUnderTheDeclaration(t *testing.T) {
	d, _ := newForgeDeployment(t)
	remote := d.remote

	// Pin the remote's default branch and stand the declaration there, as
	// the operator (the genesis root, which the code half admits
	// without the protected-surface check).
	if out, err := gitSymbolicRef(remote, "refs/heads/main"); err != nil {
		t.Fatalf("pin the default branch: %v %s", err, out)
	}
	priv := fixtureKey(t)
	rootFP := fpFor(t, priv)
	commitDeclaration(t, remote, rootFP, `{"posture": "enforced-self-hosted", "guardrails": {"squads": {"core": {"default": "trivial", "max_agent": "trivial"}}}, "teams": {"squads": [{"name": "core", "lanes": ["impl"]}]}, "protected": ["Makefile"]}`)

	// Bring the ledger to seed/1 with an agent-kind claimer and a
	// contract at and above the ceiling, each through the service.
	agent := altKey(t, 21)
	agentFP := fpFor(t, agent)
	stage := func(v, verb, subject, payload string) {
		t.Helper()
		store, _ := materializedTip(t, remote)
		rec := signedBy(t, priv, v, verb, subject, payload, tipOf(t, store))
		var perr error
		asAdmission(t, func() { _, perr = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{rec}) })
		if perr != nil {
			t.Fatalf("staging %s %s through the service: %v", verb, subject, perr)
		}
	}
	stage("seed/0", "system.protocol.upgraded", "system", `{"to": "seed/1"}`)
	stage("seed/1", "actor.enrolled", agentFP, enrollFor(t, agent, "agent", "impl"))
	stage("seed/1", "actor.granted", agentFP, `{"capability": "claim"}`)
	for _, st := range []struct {
		subject, tier string
	}{{"c-above", "standard"}, {"c-at", "trivial"}} {
		stage("seed/1", "intent.filed", st.subject, fmt.Sprintf(`{"intent": "work on %s", "tier": %q, "budget": "small", "routing": "core"}`, st.subject, st.tier))
		stage("seed/1", "contract.specified", st.subject, `{"acceptance": {"ref": "ACCEPT.md @ `+anchor40+`", "executable": false}}`)
	}

	// The above-ceiling claim: the service's dry-run refuses it with the
	// ceiling's own message — the mirror read the declaration, so the
	// judge and the hook agree.
	storeAbove, _ := materializedTip(t, remote)
	above := signedBy(t, agent, "seed/1", "claim.taken", "c-above", `{}`, tipOf(t, storeAbove))
	var perr error
	asAdmission(t, func() { _, perr = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{above}) })
	ref, ok := propose.IsRefusal(perr)
	if !ok || ref.Code != "tier_above_ceiling" {
		t.Fatalf("the service refuses the above-ceiling claim with the ceiling's code, got %v (code %s)", perr, ref.Code)
	}
	if !strings.Contains(ref.Message, "agent ceiling") {
		t.Fatalf("the ceiling's message is the one the hook gives, got %q", ref.Message)
	}
	if after := forgeTip(t, remote); after == "" {
		t.Fatal("the guarded ref vanished")
	}

	// The at-ceiling control admits through the same service: the
	// declaration is read, not a no-op, and it does not over-refuse.
	storeAt, _ := materializedTip(t, remote)
	at := signedBy(t, agent, "seed/1", "claim.taken", "c-at", `{}`, tipOf(t, storeAt))
	var res *gitref.Result
	asAdmission(t, func() { res, perr = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{at}) })
	if perr != nil || res == nil {
		t.Fatalf("the at-ceiling proposal admits through the service: %v", perr)
	}
}

// conformance: the transport arm of D3 (review, P1) — a deployment the
// service reaches only as a URL (file:// here, the same code an SSH/HTTPS
// spelling runs) reads its declaration the same way. A broken declaration
// on the default branch must refuse the proposal: the old code's
// `--git-dir <url>` resolution could not read the remote at all and
// would have admitted it.
func TestServiceReadsTheDeclarationThroughTheTransport(t *testing.T) {
	remote := forgeRemote(t)
	if out, err := gitSymbolicRef(remote, "refs/heads/main"); err != nil {
		t.Fatalf("pin the default branch: %v %s", err, out)
	}
	priv := fixtureKey(t)
	rootFP := fpFor(t, priv)
	commitDeclaration(t, remote, rootFP, `{"posture": "enforced-self-hosted"}`)

	d := startService(t, "file://"+remote)
	asAdmission(t, func() {
		rec, err := genesis.Build(priv, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.client.Propose(posture.DefaultLedgerRef, []*event.Record{rec}); err != nil {
			t.Fatalf("genesis through the URL service: %v", err)
		}
	})
	// Break the declaration AFTER genesis: the judge then reads it as it
	// stands and must fail closed.
	commitDeclaration(t, remote, rootFP, `{"posture": "enforced-self-hosted", "guardrails": {"squads": {"core": {"default": "trivial", "max_agent": ""}}}}`)

	store, _ := materializedTip(t, remote)
	rec := signed(t, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store))
	var perr error
	asAdmission(t, func() { _, perr = d.client.Propose(posture.DefaultLedgerRef, []*event.Record{rec}) })
	if perr == nil || !strings.Contains(perr.Error(), posture.DeclarationPath) {
		t.Fatalf("a URL deployment's broken declaration refuses the proposal, naming the file: %v", perr)
	}
}

// gitSymbolicRef sets (or reads, when target is empty) a bare repo's HEAD
// symref and returns git's combined output and error.
func gitSymbolicRef(remote, target string) (string, error) {
	args := []string{"--git-dir", remote, "symbolic-ref", "HEAD"}
	if target != "" {
		args = append(args, target)
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}
