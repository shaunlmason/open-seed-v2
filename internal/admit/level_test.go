package admit

// The independence levels at the verdict boundary (plans/os-99829835.md
// D1–D4; spec/verdicts.md "Independence levels"): from seed/4 the
// claimed level must equal the level the record supports and satisfy
// the tier, a level short of the tier refuses as level_short, the
// merge chain and the red-verdict lockout reapply both, and a seed/3
// chain keeps seed/3's judgment.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

type levelKeys struct {
	signer, holder, supervisor, verifier, human ed25519.PrivateKey
}

// levelStand is a chain at the given top version with a holder, a
// supervisor and a verifier, and a drive closure that takes a contract
// filed with the given intent and spec bodies through claim,
// reservation, a declared start under the given tuple, and submission.
type levelStand struct {
	ctx   *Context
	keys  levelKeys
	top   string
	step  stepFn
	drive func(subject, filed, spec, tup string) int
}

func levelFixture(t *testing.T, top string) *levelStand {
	t.Helper()
	store, resolve, signer := seededStore(t)
	k := levelKeys{signer: signer, holder: fixtureKey(t, 2), supervisor: fixtureKey(t, 11), verifier: fixtureKey(t, 3), human: fixtureKey(t, 4)}
	st := &levelStand{keys: k, top: top}
	all := []ed25519.PrivateKey{k.signer, k.holder, k.supervisor, k.verifier, k.human}
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
	}{{k.holder, "holder", keyring.CapClaim}, {k.supervisor, "supervisor", keyring.CapSupervise}, {k.verifier, "verifier", keyring.CapVerdict}, {k.human, "human", keyring.CapVerdict}} {
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
		appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, e.key), `{"capability": "`+e.cap+`"}`)
	}
	// The human: a verdict grant AND operator standing, the tree's
	// structural proxy for a person (plans/os-2e34f66a.md D4), which
	// a human-review tier's render needs.
	appendSignedV(t, store, loose, signer, version.Seed1, keyring.VerbGranted, fpOf(t, k.human), `{"capability": "`+keyring.CapOperator+`"}`)
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
	if top == version.Seed4 {
		step(signer, version.Seed3, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	}
	st.drive = func(subject, filed, spec, tup string) int {
		t.Helper()
		step(signer, top, "intent.filed", subject, filed)
		step(signer, top, "contract.specified", subject, spec)
		step(k.holder, top, "claim.taken", subject, `{}`)
		fence := fenceOf(t, st.ctx, subject)
		step(k.holder, top, "budget.reserve", subject, reserveBody("10", fence))
		res := fmt.Sprintf("%d", st.ctx.Count-1)
		step(k.supervisor, top, "run.started", subject, startBodyAt(fence, res, tup))
		subPos := st.ctx.Count
		step(k.holder, top, "submission.made", subject, submissionBody(fence))
		return subPos
	}
	return st
}

func filedTier(tier string) string {
	return fmt.Sprintf(`{"intent": "fix the thing", "tier": %q, "budget": "small", "routing": "core"}`, tier)
}

const (
	execGatedSpec = `{"acceptance": {"ref": "specs/x.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`
	execOpenSpec  = `{"acceptance": {"ref": "specs/x.md @ abc1234", "executable": true}}`
)

// levelBody is a verdict payload at the given level, with the
// verifier's declared tuple when tup is non-empty.
func levelBody(verdict string, subPos int, level, tup string) string {
	body := fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": %q`, verdict, testReceipt, subPos, level)
	if tup != "" {
		body += `, "tuple": ` + tup
	}
	return body + `}`
}

func (st *levelStand) render(t *testing.T, subject, body string) error {
	t.Helper()
	return st.renderAs(t, st.keys.verifier, subject, body)
}

func (st *levelStand) renderAs(t *testing.T, key ed25519.PrivateKey, subject, body string) error {
	t.Helper()
	return Check(st.ctx, draftV(t, key, st.top, transition.VerdictRenderedVerb, subject, body, st.ctx.Tip))
}

func verdictRefusal(t *testing.T, name string, err error, want string) {
	t.Helper()
	var ve *VerdictError
	if !errors.As(err, &ve) || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: refuses as a verdict refusal naming %q, got %v", name, want, err)
	}
}

// conformance: AC2, AC3 — the vocabulary, and the claimed level equal
// to the level the record supports, row by row.
func TestLevelClaimedMustEqualTheLevelSupported(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	base := tupleJSON(t, nil)
	sub := st.drive("c-std", filedTier("standard"), specBody, base)
	other := func(over map[string]string) string { return tupleJSON(t, over) }

	verdictRefusal(t, "L0", st.render(t, "c-std", levelBody("pass", sub, "L0", "")), "not a level")
	verdictRefusal(t, "lowercase", st.render(t, "c-std", levelBody("pass", sub, "l1", "")), "not a level")
	verdictRefusal(t, "malformed tuple", st.render(t, "c-std", levelBody("pass", sub, "L2", `{"principal": "x"}`)), "declared tuple")
	verdictRefusal(t, "L2 with no tuple", st.render(t, "c-std", levelBody("pass", sub, "L2", "")), "records support L1")
	verdictRefusal(t, "L2 equal to the window", st.render(t, "c-std", levelBody("pass", sub, "L2", base)), "records support L1")
	for _, field := range []string{"principal", "tool_policy", "environment"} {
		verdictRefusal(t, "L2 differing only in "+field, st.render(t, "c-std", levelBody("pass", sub, "L2", other(map[string]string{field: "elsewhere"}))), "records support L1")
	}
	verdictRefusal(t, "L2 differing only in model version", st.render(t, "c-std", levelBody("pass", sub, "L2", other(map[string]string{"model": "fable/9.9"}))), "records support L1")
	verdictRefusal(t, "L2 differing only in harness version", st.render(t, "c-std", levelBody("pass", sub, "L2", other(map[string]string{"harness": "local-worktree/v9"}))), "records support L1")
	verdictRefusal(t, "L3 on a prose spec", st.render(t, "c-std", levelBody("pass", sub, "L3", "")), "records support L1")
	verdictRefusal(t, "L1 claimed where the record supports L2", st.render(t, "c-std", levelBody("pass", sub, "L1", other(map[string]string{"model": "other/1"}))), "records support L2")
	if err := st.render(t, "c-std", levelBody("pass", sub, "L2", other(map[string]string{"model": "other/1"}))); err != nil {
		t.Fatalf("a different model family is L2: %v", err)
	}
	if err := st.render(t, "c-std", levelBody("pass", sub, "L2", other(map[string]string{"harness": "container/v0"}))); err != nil {
		t.Fatalf("a different harness name is L2: %v", err)
	}
	if err := st.render(t, "c-std", levelBody("pass", sub, "L1", "")); err != nil {
		t.Fatalf("L1 with no declaration on a standard prose contract admits: %v", err)
	}

	// Provider separation: a three-part model on both sides compares
	// provider and family; a provider named on one side only does not.
	subP := st.drive("c-prov", filedTier("standard"), specBody, other(map[string]string{"model": "acme/fable/5.1"}))
	if err := st.render(t, "c-prov", levelBody("pass", subP, "L2", other(map[string]string{"model": "zed/fable/5.1"}))); err != nil {
		t.Fatalf("a different provider of the same family is L2: %v", err)
	}
	verdictRefusal(t, "same provider, newer version", st.render(t, "c-prov", levelBody("pass", subP, "L2", other(map[string]string{"model": "acme/fable/6.0"}))), "records support L1")
	verdictRefusal(t, "provider on one side only", st.render(t, "c-prov", levelBody("pass", subP, "L2", other(map[string]string{"model": "fable/5.1"}))), "records support L1")

	// L3: executable and gated, the reproduction half left to
	// recomputation; ungated executable content supports no L3.
	subX := st.drive("c-exec", filedTier("standard"), execGatedSpec, base)
	if err := st.render(t, "c-exec", levelBody("pass", subX, "L3", base)); err != nil {
		t.Fatalf("L3 on an executable gated spec with the same tuple admits: %v", err)
	}
	if err := st.render(t, "c-exec", levelBody("pass", subX, "L3", "")); err != nil {
		t.Fatalf("L3 needs no declaration: %v", err)
	}
	verdictRefusal(t, "L1 where the record supports L3", st.render(t, "c-exec", levelBody("pass", subX, "L1", "")), "records support L3")
	verdictRefusal(t, "L2 where the record supports L3", st.render(t, "c-exec", levelBody("pass", subX, "L2", other(map[string]string{"model": "other/1"}))), "records support L3")
	subU := st.drive("c-open", filedTier("standard"), execOpenSpec, base)
	verdictRefusal(t, "L3 on ungated executable content", st.render(t, "c-open", levelBody("pass", subU, "L3", "")), "records support L1")
}

// conformance: AC4 — the tier's requirement, read through TierGates,
// with the strictest row for an unknown tier.
func TestLevelShortOfTheTierRefuses(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	base := tupleJSON(t, nil)
	other := tupleJSON(t, map[string]string{"model": "other/1"})
	subC := st.drive("c-crit", filedTier("critical"), specBody, base)
	var lse *LevelShortError
	err := st.render(t, "c-crit", levelBody("pass", subC, "L1", ""))
	if !errors.As(err, &lse) || lse.Tier != "critical" || lse.Required != transition.L2 || lse.Achieved != transition.L1 {
		t.Fatalf("L1 on critical refuses level_short naming the tier, the requirement and the achieved level: %v", err)
	}
	if !strings.Contains(err.Error(), "critical") || !strings.Contains(err.Error(), "L2") || !strings.Contains(err.Error(), "L1") {
		t.Fatalf("the refusal names all three: %v", err)
	}
	// critical and the unknown tier require human review from Phase
	// 10 item 4 (plans/os-2e34f66a.md D4): the human renders there.
	if err := st.renderAs(t, st.keys.human, "c-crit", levelBody("pass", subC, "L2", other)); err != nil {
		t.Fatalf("L2 satisfies critical: %v", err)
	}
	subW := st.drive("c-wiz", filedTier("wizard"), specBody, base)
	err = st.renderAs(t, st.keys.human, "c-wiz", levelBody("pass", subW, "L2", other))
	if !errors.As(err, &lse) || lse.Required != transition.L3 {
		t.Fatalf("an unknown tier requires L3, the strictest row: %v", err)
	}
	subWX := st.drive("c-wizx", filedTier("wizard"), execGatedSpec, base)
	if err := st.renderAs(t, st.keys.human, "c-wizx", levelBody("pass", subWX, "L3", "")); err != nil {
		t.Fatalf("L3 satisfies every tier, the unknown one included: %v", err)
	}
	subT := st.drive("c-triv", filedTier("trivial"), specBody, base)
	if err := st.render(t, "c-triv", levelBody("pass", subT, "L1", "")); err != nil {
		t.Fatalf("L1 satisfies trivial: %v", err)
	}
}

// conformance: AC6 — the merge chain reapplies the level and the tier:
// a raw-pushed critical verdict at L1, and one claiming a level the
// record does not support, authenticate nothing; the red-verdict
// lockout ignores a tier-short raw fail exactly as an ungranted one.
func TestLevelsAreReappliedAlongTheMergeChain(t *testing.T) {
	st := levelFixture(t, version.Seed4)
	k := st.keys
	base := tupleJSON(t, nil)
	other := tupleJSON(t, map[string]string{"model": "other/1"})
	sub := st.drive("c-crit", filedTier("critical"), specBody, base)
	request := func(pos int) error {
		return Check(st.ctx, draftV(t, k.holder, version.Seed4, "merge.requested", "c-crit", fmt.Sprintf(`{"verdict": "%d"}`, pos), st.ctx.Tip))
	}
	// A raw pass at L1 on critical: supported, short of the tier.
	shortPos := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "c-crit", levelBody("pass", sub, "L1", ""))
	chainRefusal(t, "tier-short raw pass", request(shortPos), "short of the tier")
	// A raw pass claiming L2 with no declaration: unsupported.
	fakePos := st.ctx.Count
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "c-crit", levelBody("pass", sub, "L2", ""))
	chainRefusal(t, "unsupported raw L2", request(fakePos), "does not support")
	// A raw tier-short fail does not lock pass out: a proper L2 pass
	// admits, and the request citing it admits.
	st.step(k.verifier, version.Seed4, transition.VerdictRenderedVerb, "c-crit", levelBody("fail", sub, "L1", ""))
	if err := st.renderAs(t, k.human, "c-crit", levelBody("pass", sub, "L2", other)); err != nil {
		t.Fatalf("a tier-short raw fail locks nothing: %v", err)
	}
	passPos := st.ctx.Count
	st.step(k.human, version.Seed4, transition.VerdictRenderedVerb, "c-crit", levelBody("pass", sub, "L2", other))
	if err := request(passPos); err != nil {
		t.Fatalf("the request citing the authenticated L2 pass admits: %v", err)
	}
}

// conformance: AC2 — before seed/4 the literal L1 alone, no tuple, and
// the refusal names the version.
func TestLevelsAndTheDeclarationWaitForSeed4(t *testing.T) {
	st := levelFixture(t, version.Seed3)
	base := tupleJSON(t, nil)
	sub := st.drive("c-std", filedTier("standard"), specBody, base)
	verdictRefusal(t, "L2 at seed/3", st.render(t, "c-std", levelBody("pass", sub, "L2", "")), version.Seed4)
	verdictRefusal(t, "L3 at seed/3", st.render(t, "c-std", levelBody("pass", sub, "L3", "")), version.Seed4)
	verdictRefusal(t, "a tuple at seed/3", st.render(t, "c-std", levelBody("pass", sub, "L1", base)), version.Seed4)
	if err := st.render(t, "c-std", levelBody("pass", sub, "L1", "")); err != nil {
		t.Fatalf("the literal L1 admits at seed/3 exactly as before: %v", err)
	}
	// An executable gated spec at seed/3 still records the literal:
	// the level is seed/4's, and seed/3's judgment stands.
	subX := st.drive("c-exec", filedTier("standard"), execGatedSpec, base)
	if err := st.render(t, "c-exec", levelBody("pass", subX, "L1", "")); err != nil {
		t.Fatalf("seed/3 keeps its judgment on an executable spec: %v", err)
	}
}
