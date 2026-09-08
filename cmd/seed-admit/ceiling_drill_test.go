package main

// The enforced boundary's declaration arm (os-0f924157, the review
// finding on #323): the hook reads the deployment declaration at the
// default branch's tip, so the ceiling refuses at the enforced
// boundary, not only at the cooperative client. The drills: the raw
// above-ceiling push refused at the hook while the cooperative client
// under --config self-refuses the same claim (the postures differ only
// in WHERE the rule runs), the at-ceiling control admitted at the
// hook, and a broken declaration refusing the ledger push and the code
// push alike, repaired by an operator.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// commitDeclaration places seed.json on the remote's default branch,
// the way the deployment stands it (the simulate-side analogue of the
// redteam fixture's stageCode): one commit on the branch the remote's
// HEAD points at, fast-forwarding it. It returns the branch name.
func commitDeclaration(t *testing.T, remote, pusher, decl string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "symbolic-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("read the default branch: %v", err)
	}
	full := strings.TrimSpace(string(out))
	branch := strings.TrimPrefix(full, "refs/heads/")

	work := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	hardenGitRepo(t, work)
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	base := git("ls-remote", "-q", remote, full)
	if base != "" {
		git("fetch", "-q", remote, full)
		git("checkout", "-q", "FETCH_HEAD")
	} else {
		git("checkout", "-q", "--orphan", branch)
	}
	if err := os.WriteFile(filepath.Join(work, posture.DeclarationPath), []byte(decl+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "--allow-empty", "-m", "declaration")
	push := exec.Command("git", "-C", work, "push", "-q", remote, "HEAD:"+full)
	push.Env = append(os.Environ(), pusherEnv+"="+pusher)
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("commit the declaration: %v %s", err, out)
	}
	return branch
}

// conformance: III.R row 5's hook arm (os-0f924157 D4.1) — the ceiling
// refuses at the enforced boundary. The deployment declares its squad's
// agent ceiling at trivial on the default branch; a raw above-ceiling
// claim.taken lands at the hooked remote and is refused there, ref
// unmoved, with the ceiling's message; the cooperative client under
// --config self-refuses the same claim before any push; the
// at-ceiling control admits at the hook.
func TestDrillCeilingAtTheEnforcedBoundary(t *testing.T) {
	remote := guardedRemote(t)
	if out, err := exec.Command("git", "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("pin the default branch: %v %s", err, out)
	}
	root := fixtureKey(t)
	agent := altKey(t, 21)
	rootFP, agentFP := fpFor(t, root), fpFor(t, agent)
	resolve0 := seedGenesis(t, remote)
	resolveAll := anyResolver(t, root, agent)

	raw := func(priv ed25519.PrivateKey, v, verb, subject, payload string) {
		t.Helper()
		err := craftPush(t, remote, resolveAll, func(dir string, store *ledger.Store) {
			appendRaw(t, store, resolveAll, signedBy(t, priv, v, verb, subject, payload, tipOf(t, store)))
		})
		if err != nil {
			t.Fatalf("staging %s %s refused by the hook: %v", verb, subject, err)
		}
	}

	// The ledger stands far enough for the ceiling to have something to
	// read: seed/1, an agent-kind key with claim, a contract at the
	// ceiling's tier (the control) and one above it. Staged before the
	// declaration stands, so the policy rules are no-ops during staging
	// and the keyring they build is exactly what the ceiling reads.
	raw(root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	raw(root, version.Seed1, "actor.enrolled", agentFP, enrollFor(t, agent, "agent", "impl"))
	raw(root, version.Seed1, "actor.granted", agentFP, `{"capability": "claim"}`)
	for _, c := range []struct {
		subject, tier string
	}{{"c-above", "standard"}, {"c-at", "trivial"}} {
		raw(root, version.Seed1, "intent.filed", c.subject, `{"intent": "work", "tier": "`+c.tier+`", "budget": "small", "routing": "core"}`)
		raw(root, version.Seed1, "contract.specified", c.subject, `{"acceptance": {"ref": "ACCEPT.md @ `+anchor40+`", "executable": false}}`)
	}

	// The declaration stands on the default branch: the ceiling at
	// trivial for squad core, whose lanes the contracts route to.
	commitDeclaration(t, remote, rootFP, `{"posture": "enforced-self-hosted", "guardrails": {"squads": {"core": {"default": "trivial", "max_agent": "trivial"}}}, "teams": {"squads": [{"name": "core", "lanes": ["impl"]}]}, "protected": ["Makefile"]}`)

	before := remoteTip(t, remote)
	// The raw above-ceiling claim: the hook's ceiling rule must refuse
	// it, with its message, ref unmoved.
	err := craftPush(t, remote, resolveAll, func(dir string, store *ledger.Store) {
		appendRaw(t, store, resolveAll, signedBy(t, agent, version.Seed1, "claim.taken", "c-above", `{}`, tipOf(t, store)))
	})
	if err == nil {
		t.Fatal("the raw above-ceiling claim admitted at the enforced boundary: the hook reads no declaration")
	}
	if !strings.Contains(ruleLine(err.Error()), "agent ceiling is trivial") {
		t.Fatalf("the hook's refusal is the ceiling's message, got: %v", err)
	}
	if after := remoteTip(t, remote); after != before {
		t.Fatalf("the refused claim moved the ref: %s -> %s", before, after)
	}

	// The cooperative client under --config self-refuses the SAME
	// claim before any push: the postures differ only in WHERE the
	// rule runs.
	cfg, err := posture.Parse([]byte(`{"posture": "cooperative", "guardrails": {"squads": {"core": {"default": "trivial", "max_agent": "trivial"}}}, "teams": {"squads": [{"name": "core", "lanes": ["impl"]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := gitref.NewClient(t.TempDir(), remote, guardedRef)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.AppendLoop(gitref.Draft{
		V: version.Seed1, TS: "2026-09-01T02:30:00Z", Actor: agentFP,
		Verb: "claim.taken", Subject: "c-above", Payload: json.RawMessage(`{}`),
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, agent) }, resolve0, admit.Validate(admit.WithDeclaration(cfg)), 3)
	var ce *admit.CeilingError
	if !errors.As(err, &ce) {
		t.Fatalf("the cooperative client self-refuses the above-ceiling claim with the ceiling's rule, got %v", err)
	}

	// The at-ceiling control admits at the hook: the declaration is
	// read, not a no-op, and it does not over-refuse.
	if err := craftPush(t, remote, resolveAll, func(dir string, store *ledger.Store) {
		appendRaw(t, store, resolveAll, signedBy(t, agent, version.Seed1, "claim.taken", "c-at", `{}`, tipOf(t, store)))
	}); err != nil {
		t.Fatalf("the at-ceiling control admits at the hook: %v", err)
	}
}

// conformance: D2 — a broken declaration splits the boundary into no
// halves: the ledger push and the code push both refuse, naming the
// file, and an operator's repair re-opens both.
func TestDrillBrokenDeclarationRefusesBothHalves(t *testing.T) {
	remote := guardedRemote(t)
	if out, err := exec.Command("git", "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("pin the default branch: %v %s", err, out)
	}
	root := fixtureKey(t)
	op := altKey(t, 23)
	rootFP, opFP := fpFor(t, root), fpFor(t, op)
	resolve0 := seedGenesis(t, remote)
	resolveBoth := anyResolver(t, root, op)

	raw := func(priv ed25519.PrivateKey, v, verb, subject, payload string) {
		t.Helper()
		err := craftPush(t, remote, resolveBoth, func(dir string, store *ledger.Store) {
			appendRaw(t, store, resolveBoth, signedBy(t, priv, v, verb, subject, payload, tipOf(t, store)))
		})
		if err != nil {
			t.Fatalf("staging %s %s refused by the hook: %v", verb, subject, err)
		}
	}

	// The ledger stands far enough for a code push to have standing to
	// judge: seed/1 and an operator key. Staged before the declaration
	// stands, so no policy rule has a declaration to read yet.
	raw(root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	raw(root, version.Seed1, "actor.enrolled", opFP, enrollFor(t, op, "agent", "op"))
	raw(root, version.Seed1, "actor.granted", opFP, `{"capability": "operator"}`)
	for _, c := range []string{"c-0001", "c-0002"} {
		raw(root, version.Seed1, "intent.filed", c, `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`)
		raw(root, version.Seed1, "contract.specified", c, `{"acceptance": {"ref": "ACCEPT.md @ `+anchor40+`", "executable": false}}`)
	}
	// A claim authorizes the contract branch the code push rides.
	raw(op, version.Seed1, "claim.taken", "c-0001", `{}`)

	broken := `{"posture": "enforced-self-hosted", "guardrails": {"squads": {"core": {"default": "trivial", "max_agent": ""}}}}`
	commitDeclaration(t, remote, rootFP, broken)

	// The ledger half fails closed, naming the file.
	before := remoteTip(t, remote)
	err := craftPush(t, remote, resolve0, func(dir string, store *ledger.Store) {
		appendRaw(t, store, resolve0, signedBy(t, root, version.Seed1, "message.sent", "c-0001", `{"n": 1}`, tipOf(t, store)))
	})
	if err == nil || !strings.Contains(ruleLine(err.Error()), posture.DeclarationPath) {
		t.Fatalf("the ledger half fails closed on the broken declaration, naming the file: %v", err)
	}
	if after := remoteTip(t, remote); after != before {
		t.Fatal("the refused push moved the ref")
	}
	// The code half refuses on the same broken declaration: an operator
	// (non-root) contract-branch push touching no protected path still
	// refuses, because the half cannot tell what the surface is.
	out, cerr := pushCode(t, remote, opFP, "refs/heads/seed/c-0001", false, "", map[string]string{"work.txt": "w\n"})
	if cerr == nil || !strings.Contains(out, posture.DeclarationPath) {
		t.Fatalf("the code half refuses on the same broken declaration: %v %s", cerr, out)
	}
	// The repair re-opens both halves.
	commitDeclaration(t, remote, rootFP, `{"posture": "enforced-self-hosted"}`)
	if err := craftPush(t, remote, resolve0, func(dir string, store *ledger.Store) {
		appendRaw(t, store, resolve0, signedBy(t, root, version.Seed1, "message.sent", "c-0002", `{"n": 2}`, tipOf(t, store)))
	}); err != nil {
		t.Fatalf("the repaired declaration re-opens the ledger half: %v", err)
	}
	if out, cerr := pushCode(t, remote, opFP, "refs/heads/seed/c-0001", false, "", map[string]string{"work.txt": "w\n"}); cerr != nil {
		t.Fatalf("the repaired declaration re-opens the code half: %v %s", cerr, out)
	}
}
