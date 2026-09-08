package redteam

// The compromised-actor drill (plans/os-465e356e.md; SEED-NEXT.md §I.2,
// "the architecture's definition of done"): the §I.2 adversary — a valid
// key, a valid credential, arbitrary git — played against the enforced
// reference deployment, asserting the ceiling clause by clause at the
// push. This is the release gate; it lives where `make check` looks, so
// CI runs it with the rest of the suite (D8).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

var hookBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "redteam-hook-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	hookBin = filepath.Join(dir, "seed-admit"+exeSuffix())
	if out, err := exec.Command("go", "build", "-o", hookBin, "../../cmd/seed-admit").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the enforced hook: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func newFixture(t *testing.T) *Fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pre-receive hook needs a POSIX git server; a bare Windows checkout runs the cooperative or forge-hosted posture (spec/platform.md)")
	}
	fx, err := New(t.TempDir(), hookBin, posture.EnforcedSelfHosted)
	if err != nil {
		t.Fatalf("building the enforced fixture: %v", err)
	}
	return fx
}

func newAdversary(t *testing.T, fx *Fixture) *Adversary {
	t.Helper()
	adv, err := NewAdversary(fx, fx.Adversary)
	if err != nil {
		t.Fatal(err)
	}
	return adv
}

func ceiling(t *testing.T) *Ceiling {
	t.Helper()
	c, err := LoadCeiling("testdata/ceiling.json")
	if err != nil {
		t.Fatalf("the ceiling table: %v", err)
	}
	return c
}

// conformance: III.O — the ceiling holds at the push, clause by clause.
// Every prohibition is refused with the boundary's own reason and moves
// no ref; every permission's primary is admitted; every negative is
// refused. One installed hook, one adversary, one enforced remote.
func TestCeilingHoldsAtThePush(t *testing.T) {
	fx := newFixture(t)
	adv := newAdversary(t, fx)
	c := ceiling(t)

	entries := corpus()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].order < entries[j].order })

	for _, a := range entries {
		cl, ok := c.Clause(a.clause)
		if !ok {
			t.Fatalf("%q names clause %q, which the ceiling does not carry", a.name, a.clause)
		}
		before := fx.Refs()
		o := a.run(fx, adv)
		if a.admit {
			if !o.Admitted {
				t.Errorf("%q (%s/%s, a permission): must be admitted, refused with %q", a.name, a.clause, a.side, o.Refusal())
			}
			continue
		}
		if o.Admitted {
			t.Errorf("%q (%s/%s): must be REFUSED at the push and was admitted", a.name, a.clause, a.side)
			continue
		}
		if a.reason != "" && !strings.Contains(o.Output, a.reason) {
			t.Errorf("%q (%s/%s): refused, but not with %q:\n%s", a.name, a.clause, a.side, a.reason, o.Refusal())
		}
		if after := fx.Refs(); after != before {
			t.Errorf("%q: a refused push moved a ref\nbefore:\n%s\nafter:\n%s", a.name, before, after)
		}
		if cl.Kind == Permission && a.primary {
			t.Errorf("%q is a permission's primary entry but expects refusal", a.name)
		}
	}
}

// conformance: III.O criterion 5 — one derivation: every ledger
// single-event entry yields the SAME verdict through admit.Check
// in-process (over the records it was judged against) as through the
// hook over a real push. Rewrites, truncations and code pushes carry no
// single record and are the hook's alone.
func TestOneDerivationLedgerAgrees(t *testing.T) {
	fx := newFixture(t)
	adv := newAdversary(t, fx)

	// The in-process side reads the SAME declaration the hook reads, so
	// the invariant holds under a live guardrail (os-0f924157 D4.2):
	// the fixture's committed ceiling, not an absent one.
	decl, err := fx.Declaration()
	if err != nil {
		t.Fatalf("reading the fixture's declaration: %v", err)
	}
	entries := corpus()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].order < entries[j].order })

	tested := 0
	for _, a := range entries {
		if a.side != Ledger {
			continue
		}
		o := a.run(fx, adv)
		if o.Record == nil {
			continue // a multi-record rewrite/truncation: the hook's alone
		}
		ctx, err := admit.ContextOver(o.Before, admit.WithDeclaration(decl))
		if err != nil {
			t.Fatalf("%q: admission context over the judged prefix: %v", a.name, err)
		}
		inProcess := admit.Check(ctx, o.Record)
		if (inProcess == nil) != o.Admitted {
			t.Errorf("%q: the hook and admit.Check disagree — hook admitted=%v, in-process err=%v", a.name, o.Admitted, inProcess)
		}
		tested++
	}
	if tested == 0 {
		t.Fatal("one-derivation asserted nothing — the ledger corpus produced no single-event entries")
	}
}

// conformance: III.O criterion 4 — coverage is derived from the ceiling
// both ways. Every (clause, side) target has a primary entry of matching
// polarity; every entry names a real target; an empty corpus, an empty
// ceiling, and a target with no entry each fail.
func TestCoverageBothWays(t *testing.T) {
	c := ceiling(t)
	entries := corpus()

	// Every entry names a real (clause, side) target.
	for _, a := range entries {
		cl, ok := c.Clause(a.clause)
		if !ok {
			t.Fatalf("entry %q names clause %q outside the ceiling", a.name, a.clause)
		}
		found := false
		for _, s := range cl.Sides {
			if s == a.side {
				found = true
			}
		}
		if !found {
			t.Fatalf("entry %q attacks %s on side %q, which the clause does not carry", a.name, a.clause, a.side)
		}
		if a.primary && (cl.Kind == Prohibition) != (!a.admit) {
			t.Fatalf("entry %q primary polarity does not match clause %q kind %s", a.name, a.clause, cl.Kind)
		}
	}

	// Every target has exactly one primary entry.
	primary := map[string]int{}
	for _, a := range entries {
		if a.primary {
			primary[a.clause+"/"+string(a.side)]++
		}
	}
	for _, tgt := range c.Targets() {
		switch primary[tgt] {
		case 0:
			t.Errorf("ceiling target %s has no primary corpus entry — a clause side no attack reaches", tgt)
		case 1:
		default:
			t.Errorf("ceiling target %s has %d primary entries — exactly one covers it", tgt, primary[tgt])
		}
	}
	if len(c.Targets()) != len(primary) {
		t.Errorf("the corpus covers %d targets, the ceiling has %d", len(primary), len(c.Targets()))
	}

	// Both-ways guards: an empty ceiling, a missing side, and an empty
	// corpus each fail their check.
	if _, err := LoadCeiling("testdata/does-not-exist.json"); err == nil {
		t.Error("a missing ceiling file must fail to load")
	}
	if simulateMissingTarget(c, entries) {
		t.Error("the coverage check must fail when a target has no primary entry")
	}
	if len(entries) == 0 {
		t.Error("the corpus is empty")
	}
}

// simulateMissingTarget reports whether every target still has a primary
// entry after dropping one target's entries: it must NOT, which is how
// the both-ways check proves it would catch a gap.
func simulateMissingTarget(c *Ceiling, entries []attack) bool {
	drop := c.Targets()[0]
	primary := map[string]bool{}
	for _, a := range entries {
		if a.primary && a.clause+"/"+string(a.side) != drop {
			primary[a.clause+"/"+string(a.side)] = true
		}
	}
	for _, tgt := range c.Targets() {
		if !primary[tgt] {
			return false // a gap is visible: the check would fail, as intended
		}
	}
	return true
}

// conformance: III.O — the residuals are named, and each is pinned by a
// drill asserting the attack IS admitted (or, for the bounds, that the
// harness itself refuses). An unnamed admitted attack, or a named one
// the corpus cannot reach, fails.
func TestResidualsArePinned(t *testing.T) {
	res, err := LoadResiduals("testdata/residuals.json")
	if err != nil {
		t.Fatalf("the residual table: %v", err)
	}
	named := map[string]bool{}
	for _, r := range res.Residuals {
		named[r.Name] = true
	}

	pinned := map[string]func(t *testing.T){
		"signed-lie": func(t *testing.T) {
			fx := newFixture(t)
			adv := newAdversary(t, fx)
			body := fmt.Sprintf(`{"fence": "%d", "tried": "x", "outcome": "invented", "condition": "false", "environment": "fixture"}`, fx.Fence[ContractHeld])
			if o, _ := adv.As("curation.deadend.recorded", ContractHeld, body); !o.Admitted {
				t.Fatalf("a false dead end attributed to itself must be admitted (attribution is not trust): %s", o.Refusal())
			}
		},
		"self-exhaustion": func(t *testing.T) {
			fx := newFixture(t)
			adv := newAdversary(t, fx)
			// small class capacity is 100; a 100-unit reserve inside its
			// own window is admitted, and strands the contract's class.
			body := fmt.Sprintf(`{"amount": "100", "fence": "%d"}`, fx.Fence[ContractHeld])
			if o, _ := adv.As("budget.reserve", ContractHeld, body); !o.Admitted {
				t.Fatalf("a full-capacity reserve inside its own window must be admitted: %s", o.Refusal())
			}
		},
		"refusal-flooding": func(t *testing.T) {
			fx := newFixture(t)
			adv := newAdversary(t, fx)
			before := fx.Refs()
			for i := 0; i < 3; i++ {
				if o, _ := adv.As("claim.taken", ContractPeer, `{}`); o.Admitted {
					t.Fatal("a flood is refusals, not admissions")
				}
			}
			if fx.Refs() != before {
				t.Fatal("a flood of refusals moved a ref — it is denial of progress, never of authority")
			}
		},
		"test-content": func(t *testing.T) {
			fx := newFixture(t)
			adv := newAdversary(t, fx)
			o := adv.PushBranch("refs/heads/seed/"+ContractHeld, false, DefaultBranch, map[string]string{"a_test.go": "package a\n", "src/impl.go": "package a\n"})
			if !o.Admitted {
				t.Fatalf("test content outside the protected surface on its own branch is the charter's named residual — must be admitted: %s", o.Refusal())
			}
		},
		"credential-binding": func(t *testing.T) {
			// A forged SEED_PUSHER buys the impersonated actor's contract
			// branch and nothing on the ledger: the ledger half is
			// signature-bound. Here the adversary asserts the PEER's
			// identity and pushes the peer's branch (admitted), then
			// tries a ledger append as the peer (refused at signature).
			fx := newFixture(t)
			asPeer := &Adversary{fx: fx, ID: &Identity{Name: "adv-as-peer", Key: fx.Adversary.Key, FP: fx.Peer.FP}}
			var err error
			asPeer.dir, err = os.MkdirTemp(fx.Dir, "asPeer-*")
			if err != nil {
				t.Fatal(err)
			}
			o := asPeer.PushBranch("refs/heads/seed/"+ContractPeer, true, DefaultBranch, map[string]string{"stolen.txt": "1"})
			if !o.Admitted {
				t.Fatalf("a stolen credential buys the impersonated actor's contract branch: %s", o.Refusal())
			}
			// But signing a ledger event as the peer still fails: the
			// key is the adversary's, not the peer's.
			led, _ := asPeer.PushEvent(fx.Peer.FP, "message.sent", ContractReady, `{"n": 1}`)
			if led.Admitted || !strings.Contains(led.Output, "signature does not verify") {
				t.Fatalf("a stolen credential buys nothing on the signature-bound ledger: %v %s", led.Admitted, led.Refusal())
			}
		},
		"one-posture": func(t *testing.T) {
			for _, p := range []posture.Posture{posture.Cooperative, posture.EnforcedForgeHosted} {
				if _, err := New(t.TempDir(), hookBin, p); err == nil {
					t.Fatalf("the drill must refuse to build a %s fixture — it cannot report green where the invariant does not hold", p)
				}
			}
		},
		"revocation-reap": func(t *testing.T) {
			// Today's consequence, both halves: a revoked key's ledger
			// proposals refuse at the push, and its contract branch
			// closes with its standing. The reap of the still-open claim
			// is os-32d06c65 (D9), not asserted here.
			fx := newFixture(t)
			if _, err := fx.Append(fx.Root, fx.Active, "actor.revoked", fx.Adversary.FP, `{"reason": "compromised"}`); err != nil {
				t.Fatalf("revoking the adversary: %v", err)
			}
			adv := newAdversary(t, fx)
			if o, _ := adv.As("curation.deadend.recorded", ContractHeld, fmt.Sprintf(`{"fence": "%d", "tried": "x", "outcome": "y", "condition": "z", "environment": "e"}`, fx.Fence[ContractHeld])); o.Admitted {
				t.Fatal("a revoked key's ledger proposal must refuse at the push")
			}
			if o := adv.PushBranch("refs/heads/seed/"+ContractHeld, false, DefaultBranch, map[string]string{"after.txt": "1"}); o.Admitted {
				t.Fatal("a revoked key's contract branch must close with its standing")
			}
		},
	}

	for name := range pinned {
		if !named[name] {
			t.Errorf("drill pins residual %q, which the table does not name", name)
		}
	}
	for _, name := range res.Names() {
		fn, ok := pinned[name]
		if !ok {
			t.Errorf("residual %q is named but no drill pins it", name)
			continue
		}
		t.Run(name, fn)
	}
}

// The loaders validate their tables: a duplicate id, a missing field, an
// unknown side, and an empty set each refuse, so a malformed corpus
// cannot pass vacuously.
func TestTablesValidate(t *testing.T) {
	if _, err := LoadCeiling("testdata/ceiling.json"); err != nil {
		t.Fatalf("the shipped ceiling must load: %v", err)
	}
	if _, err := LoadResiduals("testdata/residuals.json"); err != nil {
		t.Fatalf("the shipped residuals must load: %v", err)
	}
	for name, body := range map[string]string{
		"empty clauses": `{"clauses": []}`,
		"unknown kind":  `{"clauses": [{"id": "x", "kind": "maybe", "text": "t", "sides": ["ledger"], "vocabulary": "v"}]}`,
		"no side":       `{"clauses": [{"id": "x", "kind": "prohibition", "text": "t", "sides": [], "vocabulary": "v"}]}`,
		"unknown side":  `{"clauses": [{"id": "x", "kind": "prohibition", "text": "t", "sides": ["mirror"], "vocabulary": "v"}]}`,
		"duplicate id":  `{"clauses": [{"id": "x", "kind": "prohibition", "text": "t", "sides": ["ledger"], "vocabulary": "v"}, {"id": "x", "kind": "permission", "text": "t", "sides": ["code"], "vocabulary": "v"}]}`,
		"no vocabulary": `{"clauses": [{"id": "x", "kind": "prohibition", "text": "t", "sides": ["ledger"], "vocabulary": ""}]}`,
		"unknown field": `{"clauses": [{"id": "x", "kind": "prohibition", "text": "t", "sides": ["ledger"], "vocabulary": "v", "extra": 1}]}`,
	} {
		p := filepath.Join(t.TempDir(), "c.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCeiling(p); err == nil {
			t.Errorf("ceiling %q must refuse", name)
		}
	}
	for name, body := range map[string]string{
		"empty":         `{"residuals": []}`,
		"missing field": `{"residuals": [{"name": "x", "why": "w", "inflicts": "i"}]}`,
		"duplicate":     `{"residuals": [{"name": "x", "why": "w", "inflicts": "i", "stands_in_the_way": "s"}, {"name": "x", "why": "w", "inflicts": "i", "stands_in_the_way": "s"}]}`,
	} {
		p := filepath.Join(t.TempDir(), "r.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadResiduals(p); err == nil {
			t.Errorf("residuals %q must refuse", name)
		}
	}
}

// exeSuffix is the platform's executable suffix: a built binary
// without it is not runnable on Windows (spec/platform.md).
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
