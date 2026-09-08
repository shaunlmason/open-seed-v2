package admit

// The qualification verbs at the boundary (plans/os-03e47abb.md D2,
// D4, D6, D8; spec/evals.md): every refusal its own row with
// nothing appended; the eval marker refused before seed/3; the set
// rule's exemption on eval subjects; and the closed bridge after a
// disqualification.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const evalFiled = `{"intent": "eval fix-the-check: the check is green", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "fix-the-check"}}`

// evalSpec is the named definition's own fixture at a gated revision:
// what binds the marker to a definition at the boundary (by path; the
// derivation binds it to the reviewed anchor).
const evalSpec = `{"acceptance": {"ref": "evals/fix-the-check/fixture/accept.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`

// evalSpecFor is evalSpec for a definition of another name.
func evalSpecFor(name string) string {
	return `{"acceptance": {"ref": "evals/` + name + `/fixture/accept.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`
}

// The fixture's records all carry draftV's ts; a mint dated there is
// not before the verdict it cites, and one a second earlier is.
const fixtureTS = "2026-09-01T01:00:00Z"

type evalKeys struct {
	signer, holder, supervisor, verifier, other ed25519.PrivateKey
}

// draftTS is draftV with a caller-chosen ts, for the ordering rows.
func draftTS(t *testing.T, priv ed25519.PrivateKey, v, ts, verb, subject, payload, prev string) *event.Record {
	t.Helper()
	rec, err := event.Sign(event.Event{
		V: v, TS: ts, Actor: fpOf(t, priv),
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: prev,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func mintBody(contract, tup string, verdictPos int) string {
	return fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d"}`, tup, contract, verdictPos)
}

func dropBody(contract, tup string, verdictPos int) string {
	return fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d", "reason": "the eval failed under this configuration"}`, tup, contract, verdictPos)
}

// evalFixture is a seed/3 chain with a holder, a supervisor, a
// verifier and a second claim-capable actor; an ordinary contract c-1
// specified and unclaimed; and two evals judged through the production
// verbs under the base tuple: e-1 passed, e-2 failed. It returns the
// context, the cast, the step closure, a drive closure that takes an
// eval through claim, reservation, declared start and submission
// (returning the submission position), and the two verdict positions.
type evalStand struct {
	ctx   *Context
	keys  evalKeys
	step  stepFn
	drive func(subject string) int
	pass  int
	fail  int
}

func evalFixture(t *testing.T) *evalStand {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := evalKeys{signer: signer, holder: fixtureKey(t, 2), supervisor: fixtureKey(t, 11), verifier: fixtureKey(t, 3), other: fixtureKey(t, 5)}
	st := &evalStand{keys: k}
	all := []ed25519.PrivateKey{k.signer, k.holder, k.supervisor, k.verifier, k.other}
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
		cap  string
	}{{k.holder, "holder", keyring.CapClaim}, {k.supervisor, "supervisor", keyring.CapSupervise}, {k.verifier, "verifier", keyring.CapVerdict}, {k.other, "other", keyring.CapClaim}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, e.key), `{"capability": "`+e.cap+`"}`)
	}
	step := func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		st.ctx = c
		return c
	}
	st.step = step
	step(signer, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	step(signer, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	step(signer, version.Seed3, "intent.filed", "c-1", filedBody)
	step(signer, version.Seed3, "contract.specified", "c-1", specBody)
	drive := func(subject string) int {
		t.Helper()
		step(signer, version.Seed3, "intent.filed", subject, evalFiled)
		step(signer, version.Seed3, "contract.specified", subject, evalSpec)
		step(k.holder, version.Seed3, "claim.taken", subject, `{}`)
		fence := fenceOf(t, st.ctx, subject)
		step(k.holder, version.Seed3, "budget.reserve", subject, reserveBody("10", fence))
		res := fmt.Sprintf("%d", st.ctx.Count-1)
		step(k.supervisor, version.Seed3, "run.started", subject, startBodyAt(fence, res, tupleJSON(t, nil)))
		subPos := st.ctx.Count
		step(k.holder, version.Seed3, "submission.made", subject, submissionBody(fence))
		return subPos
	}
	st.drive = drive
	sub1 := drive("e-1")
	st.pass = st.ctx.Count
	step(k.verifier, version.Seed3, "verdict.rendered", "e-1", verdictBody("pass", sub1))
	sub2 := drive("e-2")
	st.fail = st.ctx.Count
	step(k.verifier, version.Seed3, "verdict.rendered", "e-2", verdictBody("fail", sub2))
	return st
}

// chainRefusal asserts a ChainError naming the given phrase.
func chainRefusal(t *testing.T, name string, err error, want string) {
	t.Helper()
	var ce *transition.ChainError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: refuses as a chain refusal naming %q, got %v", name, want, err)
	}
}

// conformance: AC3 — actor.qualified refuses at admission, each its
// own row with nothing appended: a non-eval subject; a verdict that is
// not boundary-authenticated (raw pushed by a key without verdict, or
// by the implementer); a fail verdict; a tuple differing from the
// window's declaration in any one field; a subject that is not the
// window's holder; a signer holding neither supervise nor operator; a
// ts earlier than the cited verdict's; a second mint citing the same
// verdict. The clean mint admits and folds into the admissible set.
func TestQualifiedRefusesEachRowAtTheBoundary(t *testing.T) {
	st := evalFixture(t)
	k, passPos, failPos := st.keys, st.pass, st.fail
	holder, supervisor := fpOf(t, k.holder), fpOf(t, k.supervisor)
	base := tupleJSON(t, nil)
	clean := mintBody("e-1", base, passPos)
	propose := func(priv ed25519.PrivateKey, ts, subject, body string) error {
		t.Helper()
		return Check(st.ctx, draftTS(t, priv, version.Seed3, ts, keyring.VerbQualified, subject, body, st.ctx.Tip))
	}
	before := st.ctx.Count

	if err := propose(k.supervisor, fixtureTS, holder, clean); err != nil {
		t.Fatalf("the clean mint admits: the supervisor qualifies the holder for the configuration the run declared, citing the authenticated pass: %v", err)
	}
	chainRefusal(t, "non-eval subject", propose(k.supervisor, fixtureTS, holder, mintBody("c-1", base, passPos)), "not an eval")
	chainRefusal(t, "fail verdict", propose(k.supervisor, fixtureTS, holder, mintBody("e-2", base, failPos)), "not the eval's authenticated pass")
	chainRefusal(t, "a position that is no verdict", propose(k.supervisor, fixtureTS, holder, mintBody("e-1", base, passPos-1)), "not the eval's authenticated pass")
	chainRefusal(t, "a position off the chain", propose(k.supervisor, fixtureTS, holder, mintBody("e-1", base, st.ctx.Count+5)), "not on the chain")
	for _, field := range tuple.Fields() {
		err := propose(k.supervisor, fixtureTS, holder, mintBody("e-1", tupleJSON(t, map[string]string{field: "drifted"}), passPos))
		chainRefusal(t, field, err, "differs from the configuration the run declared")
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: the refusal names the field: %v", field, err)
		}
	}
	chainRefusal(t, "not the holder", propose(k.supervisor, fixtureTS, supervisor, clean), "not the holder of the window")
	for name, priv := range map[string]ed25519.PrivateKey{"the holder": k.holder, "the verifier": k.verifier, "another worker": k.other} {
		var oog *OutOfGrantError
		if err := propose(priv, fixtureTS, holder, clean); !errors.As(err, &oog) || oog.Drift != nil {
			t.Fatalf("%s signing: a signer holding neither supervise nor operator is out of grant: %v", name, err)
		}
	}
	chainRefusal(t, "ts before the verdict", propose(k.supervisor, "2026-09-01T00:59:59Z", holder, clean), "precedes the cited verdict")
	if st.ctx.Count != before {
		t.Fatal("a refused proposal appends nothing")
	}

	// An eval in name only: the marker with any other acceptance spec
	// (an ordinary gateless spec here) is no eval, and its authentic
	// pass qualifies nobody (review finding on the task PR).
	st.step(k.signer, version.Seed3, "intent.filed", "e-fake", evalFiled)
	st.step(k.signer, version.Seed3, "contract.specified", "e-fake", specBody)
	st.step(k.holder, version.Seed3, "claim.taken", "e-fake", `{}`)
	fakeFence := fenceOf(t, st.ctx, "e-fake")
	st.step(k.holder, version.Seed3, "budget.reserve", "e-fake", reserveBody("10", fakeFence))
	st.step(k.supervisor, version.Seed3, "run.started", "e-fake", startBodyAt(fakeFence, fmt.Sprintf("%d", st.ctx.Count-1), base))
	fakeSub := st.ctx.Count
	st.step(k.holder, version.Seed3, "submission.made", "e-fake", submissionBody(fakeFence))
	st.step(k.verifier, version.Seed3, "verdict.rendered", "e-fake", verdictBody("pass", fakeSub))
	chainRefusal(t, "an eval in name only", propose(k.supervisor, fixtureTS, holder, mintBody("e-fake", base, st.ctx.Count-1)), "not that definition's fixture")

	// Not boundary-authenticated: e-3 judged by raw pushes. First by a
	// key holding no verdict grant, then by the implementer itself; the
	// fold carries each as the latest verdict and neither qualifies.
	sub3 := st.drive("e-3")
	st.step(k.other, version.Seed3, "verdict.rendered", "e-3", verdictBody("pass", sub3))
	chainRefusal(t, "raw pass by an unverdicted key", propose(k.supervisor, fixtureTS, holder, mintBody("e-3", base, st.ctx.Count-1)), "not the eval's authenticated pass")
	st.step(k.holder, version.Seed3, "verdict.rendered", "e-3", verdictBody("pass", sub3))
	chainRefusal(t, "raw pass by the implementer", propose(k.supervisor, fixtureTS, holder, mintBody("e-3", base, st.ctx.Count-1)), "not the eval's authenticated pass")

	// Authority is replayed to the verdict's own position (D2): the
	// unverdicted key's raw pass on e-3 does not become authentic when
	// the key is granted verdict afterwards.
	st.step(k.signer, version.Seed3, keyring.VerbGranted, fpOf(t, k.other), `{"capability": "verdict"}`)
	st.step(k.other, version.Seed3, "verdict.rendered", "e-3", verdictBody("pass", sub3))
	chainRefusal(t, "a raw pass authenticated only at the tip", propose(k.supervisor, fixtureTS, holder, mintBody("e-3", base, st.ctx.Count-2)), "not the eval's authenticated pass")

	// The clean mint lands; a second citing the same verdict refuses,
	// and the admissible set holds the configuration.
	// And the converse: the verifier suspended after rendering does not
	// unmake the pass it rendered while granted.
	st.step(k.signer, version.Seed3, keyring.VerbSuspended, fpOf(t, k.verifier), `{"reason": "rotation"}`)
	st.step(k.supervisor, version.Seed3, keyring.VerbQualified, holder, clean)
	chainRefusal(t, "a second mint citing the same verdict", propose(k.supervisor, fixtureTS, holder, clean), "already cited")
	if cited := st.ctx.Keyring.GrantTuples(holder, keyring.CapClaim); len(cited) != 1 || cited[0].Model != baseTuple["model"] {
		t.Fatalf("the mint folds into the holder's admissible set: %+v", cited)
	}
}

// conformance: AC4's boundary rows, AC6 and D6 — actor.disqualified
// refuses a tuple not currently admissible, a pass verdict, a non-eval
// subject and an unentitled signer; run.started on an eval subject
// admits any declared tuple while the set rule stands on an ordinary
// one; and after the disqualification empties a once-cited set the
// bridge does not reopen.
func TestDisqualifiedRowsExemptionAndTheClosedBridge(t *testing.T) {
	st := evalFixture(t)
	k, passPos, failPos := st.keys, st.pass, st.fail
	holder := fpOf(t, k.holder)
	base := tupleJSON(t, nil)
	drifted := tupleJSON(t, map[string]string{"model": "other/9"})
	st.step(k.supervisor, version.Seed3, keyring.VerbQualified, holder, mintBody("e-1", base, passPos))

	// Item 1's rule fed by a mint rather than a hand: on the ordinary
	// contract the qualified configuration admits and a drifted one is
	// out of grant naming the holder.
	st.step(k.holder, version.Seed3, "claim.taken", "c-1", `{}`)
	cFence := fenceOf(t, st.ctx, "c-1")
	st.step(k.holder, version.Seed3, "budget.reserve", "c-1", reserveBody("10", cFence))
	cRes := fmt.Sprintf("%d", st.ctx.Count-1)
	start := func(subject, fence, res, tup string) error {
		t.Helper()
		return Check(st.ctx, draftV(t, k.supervisor, version.Seed3, "run.started", subject, startBodyAt(fence, res, tup), st.ctx.Tip))
	}
	var oog *OutOfGrantError
	if err := start("c-1", cFence, cRes, drifted); !errors.As(err, &oog) || oog.Drift == nil || oog.Drift.Holder != holder || oog.Drift.Field != "model" {
		t.Fatalf("on an ordinary contract a drifted start is out of grant against the minted set: %v", err)
	}
	if err := start("c-1", cFence, cRes, base); err != nil {
		t.Fatalf("the qualified configuration admits: %v", err)
	}

	// AC6: on an eval subject any declared configuration admits: the
	// eval is what proves a configuration, so the set cannot gate it.
	st.step(k.signer, version.Seed3, "intent.filed", "e-3", evalFiled)
	st.step(k.signer, version.Seed3, "contract.specified", "e-3", evalSpec)
	st.step(k.holder, version.Seed3, "claim.taken", "e-3", `{}`)
	eFence := fenceOf(t, st.ctx, "e-3")
	st.step(k.holder, version.Seed3, "budget.reserve", "e-3", reserveBody("10", eFence))
	eRes := fmt.Sprintf("%d", st.ctx.Count-1)
	if err := start("e-3", eFence, eRes, drifted); err != nil {
		t.Fatalf("on an eval subject a configuration the set does not cite admits: %v", err)
	}

	// The disqualification rows.
	propose := func(priv ed25519.PrivateKey, subject, body string) error {
		t.Helper()
		return Check(st.ctx, draftV(t, priv, version.Seed3, keyring.VerbDisqualified, subject, body, st.ctx.Tip))
	}
	before := st.ctx.Count
	if err := propose(k.supervisor, holder, dropBody("e-2", base, failPos)); err != nil {
		t.Fatalf("the clean disqualification admits: %v", err)
	}
	if err := propose(k.supervisor, holder, dropBody("e-2", drifted, failPos)); err == nil || !strings.Contains(err.Error(), "nothing to disqualify") {
		t.Fatalf("a tuple not currently admissible refuses by name: %v", err)
	}
	chainRefusal(t, "pass verdict", propose(k.supervisor, holder, dropBody("e-1", base, passPos)), "not an authenticated fail")
	chainRefusal(t, "non-eval subject", propose(k.supervisor, holder, dropBody("c-1", base, failPos)), "not an eval")
	chainRefusal(t, "ts before the verdict", Check(st.ctx, draftTS(t, k.supervisor, version.Seed3, "2026-09-01T00:59:59Z", keyring.VerbDisqualified, holder, dropBody("e-2", base, failPos), st.ctx.Tip)), "precedes the cited verdict")
	if err := propose(k.holder, holder, dropBody("e-2", base, failPos)); !errors.As(err, &oog) {
		t.Fatalf("an unentitled signer is out of grant: %v", err)
	}
	if st.ctx.Count != before {
		t.Fatal("a refused proposal appends nothing")
	}

	// The disqualification lands: the set is empty and was once cited,
	// so on the ordinary contract nothing admits, the cited
	// configuration included, and the refusal says why. On the eval the
	// exemption still holds.
	st.step(k.supervisor, version.Seed3, keyring.VerbDisqualified, holder, dropBody("e-2", base, failPos))
	if err := propose(k.supervisor, holder, dropBody("e-2", base, failPos)); err == nil || !strings.Contains(err.Error(), "nothing to disqualify") {
		t.Fatalf("a second disqualification finds nothing admissible to remove: %v", err)
	}
	for name, tup := range map[string]string{"the disqualified configuration": base, "any other": drifted} {
		err := start("c-1", cFence, cRes, tup)
		if !errors.As(err, &oog) || oog.Drift == nil || len(oog.Drift.Cited) != 0 || !strings.Contains(err.Error(), "every cited configuration is disqualified") {
			t.Fatalf("%s: the bridge does not reopen for a once-cited holder: %v", name, err)
		}
	}
	if err := start("e-3", eFence, eRes, base); err != nil {
		t.Fatalf("a disqualified configuration still runs an eval, which is how it re-qualifies (D6): %v", err)
	}
}

// conformance: D8 — the eval marker on intent.filed is defined at
// seed/3; a filing carrying it at an earlier tip refuses naming the
// version rather than admitting as an ordinary contract a seed/2
// validator's fold would read it as.
func TestEvalMarkerIsRefusedBeforeSeed3(t *testing.T) {
	ctx, k, step := runFixture(t)
	for _, v := range []string{version.Seed1, version.Seed2} {
		if v == version.Seed2 {
			ctx = step(k.signer, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
		}
		chainRefusal(t, v, Check(ctx, draftV(t, k.signer, v, "intent.filed", "e-1", evalFiled, ctx.Tip)), "eval semantics activate at "+version.Seed3)
		if err := Check(ctx, draftV(t, k.signer, v, "intent.filed", "c-9", filedBody, ctx.Tip)); err != nil {
			t.Fatalf("%s: an ordinary filing admits as before: %v", v, err)
		}
	}
	ctx = step(k.signer, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	if err := Check(ctx, draftV(t, k.signer, version.Seed3, "intent.filed", "e-1", evalFiled, ctx.Tip)); err != nil {
		t.Fatalf("at seed/3 the marker admits: %v", err)
	}
}
