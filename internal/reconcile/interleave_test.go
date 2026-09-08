package reconcile_test

// The reconciliation interleaving check (plans/os-873b5153.md; SEED-NEXT.md
// §II.8): done is a verdict AND a reconciliation across two systems that
// share no transaction, so divergence is a first-class detected state
// rather than an accident. This enumerates every interleaving of the
// verdict chain, an operator override, a forge moving on its own
// schedule, and a raw-push adversary, then asks the real classifier what
// it sees.
//
// One deviation from the plan, recorded in docs/decisions.md: the
// plan (D2, D5) described an abstract model plus a separate replay
// through admit.Check. This enumerates over REAL records with the real
// admission and the real classifier instead, collapsing the two. That is
// strictly stronger, because a model and its replay can disagree and
// these cannot: every non-raw step is admitted by admit.Check as it is
// taken, and every classification is reconcile.Classify's own. The cost
// is speed, which the sizes below absorb.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/explore"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// The three sets the plan's preamble names, partitioning EMITTING PATHS
// rather than class names (review on #366). independence_unverified
// appears twice on purpose: VerifyVerdicts emits it from records, and
// EvidenceAt emits it separately for a non-reproducing L3 receipt.
// depth is the fast gate's size (plans/os-873b5153.md D7): 4 steps per
// trace is 852 terminal states across the two variants in about 1.9s,
// which keeps this model and the racing one beside it inside the five
// seconds the plan budgets for the pair. perf-scale.yml runs depth 6
// (7,068 terminal states, about 20s) weekly.
var depth = flag.Int("depth", 4, "steps per reconciliation trace (plans/os-873b5153.md D7)")

var (
	// admittedReachable: a fully admitted interleaving produces these.
	admittedReachable = []string{
		reconcile.ClassUnreconciled,
		reconcile.ClassOverridden,
		reconcile.ClassUnsealed,
	}
	// rawPushReachable: admission refuses the states these describe, so
	// only a raw push reaches them. They exist to surface raw-pushed
	// history, which is why the model carries a raw-push step at all.
	rawPushReachable = []string{
		reconcile.ClassMergeWithoutVerdict,
		reconcile.ClassChainSkipped,
		reconcile.ClassVerdictUnverified,
		reconcile.ClassSealUnverified,
		reconcile.ClassOverrideUnverified,
		reconcile.ClassIndependenceUnverified, // the VerifyVerdicts half
	}
	// evidenceGrade: these need an artifact store and a repository a
	// ledger interleaving does not produce. Out of scope here, named so
	// the partition is exhaustive rather than silently short.
	evidenceGrade = []string{
		reconcile.ClassEvidenceMissing,
		reconcile.ClassAttestedDivergence,
		reconcile.ClassTargetRewritten,
		reconcile.ClassScorecardUnverified,
		reconcile.ClassLessonUnverified,
		reconcile.ClassLessonStale,
		reconcile.ClassIndependenceUnverified, // the EvidenceAt half
	}
	// consumed: translated from internal/obligation, never derived here.
	consumed = []string{reconcile.ClassRunUnsettled}
)

// conformance: the partition is exhaustive over the package's own class
// constants. A class added to internal/reconcile and to no set fails
// here, which is what keeps the sets honest as the package grows.
func TestClassPartitionIsExhaustive(t *testing.T) {
	named := map[string]bool{}
	for _, set := range [][]string{admittedReachable, rawPushReachable, evidenceGrade, consumed} {
		for _, c := range set {
			named[c] = true
		}
	}
	declared := declaredClasses(t)
	for _, c := range declared {
		if !named[c] {
			t.Errorf("class %q belongs to no set: name its emitting path in the plan's partition (admitted-reachable, raw-push-reachable, evidence-grade or consumed)", c)
		}
	}
	for c := range named {
		if !slices.Contains(declared, c) {
			t.Errorf("the partition names %q, which internal/reconcile does not declare", c)
		}
	}
}

// step is one move. raw marks the steps that bypass admission.
type step struct {
	name string
	raw  bool
}

func (s step) String() string {
	if s.raw {
		return "raw:" + s.name
	}
	return s.name
}

// rstate is one state: the ledger records so far, plus the forge's own
// position. The records ARE the state, so nothing here re-implements
// the fold; every read goes through the real one.
type rstate struct {
	h        *harness
	records  []*event.Record
	usedRaw  bool
	forgeHas bool // the forge merged the branch
	steps    int
}

func (s *rstate) Key() string {
	var b strings.Builder
	for _, r := range s.records[s.h.base:] {
		// The payload belongs in the key, not just the verb and the
		// signer: a pass verdict and a fail verdict are the same verb by
		// the same lane, and a key that cannot tell them apart prunes
		// every fail branch as a duplicate of the pass one. The first
		// draft did exactly that and lost the whole override path, which
		// is what explore.State.Key warns about.
		fmt.Fprintf(&b, "%s/%s/%x;", r.Event.Verb, r.Event.Actor[:6], sha256.Sum256(r.Event.Payload))
	}
	// The depth counter belongs in the key because Terminal() reads it:
	// without it a step that admission refuses (a no-op on the records)
	// memoizes back onto its own predecessor and the walk never reaches
	// the terminal state that trace would have had. The first draft
	// omitted it and silently lost every trace whose tail was refusals.
	fmt.Fprintf(&b, "|raw=%v forge=%v depth=%d", s.usedRaw, s.forgeHas, s.steps)
	return b.String()
}

func (s *rstate) Terminal() bool { return s.steps >= s.h.depth }

func (s *rstate) fold() *transition.Fold { return s.h.table.FoldRecords(s.records) }

func (s *rstate) subject() transition.SubjectState {
	st, _ := s.fold().State(contractID)
	return st
}

// Enabled lists the moves. Each is a verb some lane may draft; whether
// it ADMITS is decided in Apply by admit.Check, which is the point.
func (s *rstate) Enabled() []step {
	if s.Terminal() {
		return nil
	}
	out := []step{
		{name: "verdict.pass"},
		{name: "verdict.fail"},
		{name: transition.MergeRequestedVerb},
		{name: "merge.requested.override"},
		{name: transition.MergeObservedVerb},
		{name: transition.MergeOverriddenVerb},
		{name: transition.CheckSealedVerb},
		{name: "forge.merge"},
	}
	// The raw-push adversary, once per trace: a compromised ledger-ref
	// credential appending without admission. Bounding it to one keeps
	// the space small while still reaching every class that needs it.
	if !s.usedRaw {
		out = append(out,
			step{name: transition.MergeObservedVerb, raw: true},
			step{name: "verdict.pass.byholder", raw: true},
			step{name: transition.CheckSealedVerb, raw: true},
			step{name: transition.MergeOverriddenVerb, raw: true},
			step{name: "verdict.pass.overdeclared", raw: true},
		)
	}
	return out
}

func (s *rstate) clone() *rstate {
	c := *s
	c.records = s.records[:len(s.records):len(s.records)]
	c.steps = s.steps + 1
	return &c
}

// Apply drafts the step's record and, unless the step is raw, requires
// admission to accept it. A refused non-raw step is a no-op that still
// consumes depth, so the walk explores what the boundary allows rather
// than what a model imagines it allows.
func (s *rstate) Apply(st step) *rstate {
	n := s.clone()
	if st.name == "forge.merge" {
		n.forgeHas = true
		return n
	}
	rec, ok := s.h.draft(s, st)
	if !ok {
		return n
	}
	if st.raw {
		n.usedRaw = true
		n.records = append(n.records, rec)
		return n
	}
	ctx, err := admit.ContextOver(s.records)
	if err != nil {
		return n
	}
	if admit.Check(ctx, rec) != nil {
		return n
	}
	n.records = append(n.records, rec)
	return n
}

// ---------------------------------------------------------------------
// The harness: one staged ledger the whole enumeration branches from.
// ---------------------------------------------------------------------

const contractID = "c-1"

// harness holds the staged prefix and the lane keys. Staging happens
// once per run; every state shares the prefix and appends its own tail.
type harness struct {
	t      *testing.T
	table  *transition.Table
	keys   map[string]ed25519.PrivateKey
	fps    map[string]string
	base   int // length of the staged prefix
	depth  int // steps per trace
	prefix []*event.Record
}

// lanes are the identities the staging enrolls, each with the one grant
// its steps need. The sealer is disjoint from the claim lane because
// admission's authoring-isolation rule requires it.
var lanes = []struct{ name, cap string }{
	{"holder", keyring.CapClaim},
	{"verifier", keyring.CapVerdict},
	{"sealer", keyring.CapSealer},
	{"observer", keyring.CapObserver},
}

// stage builds the prefix: genesis, the upgrade to the active protocol,
// the lanes enrolled and granted, then one standard-tier contract filed,
// specified, claimed and submitted, so it stands in review with a
// submission bound and every later verb in reach.
// stage builds the prefix. plantRawSeal inserts a holder-signed
// check.sealed while the contract is still in its legal seal window
// (ready, no claimant, first seal), bypassing admission the way a
// compromised ledger-ref credential would.
//
// That variant exists because seal_unverified is otherwise unreachable
// from this model, and for a structural reason worth stating: the fold
// records a Sealed fact ONLY from that window, so a seal planted later
// is counted as an anomaly and never becomes the fact VerifySeals
// reads. Reaching the class therefore requires the raw push to land
// before the claim, which is before any step this alphabet takes.
func stage(t *testing.T, depth int, plantRawSeal bool) *harness {
	t.Helper()
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, table: table, depth: depth,
		keys: map[string]ed25519.PrivateKey{}, fps: map[string]string{}}
	h.keys["root"] = fixtureKey(1)
	for i, l := range lanes {
		h.keys[l.name] = fixtureKey(byte(i + 2))
	}
	for name, k := range h.keys {
		fp, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		h.fps[name] = fp
	}
	gen, err := genesis.Build(h.keys["root"], nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	h.prefix = []*event.Record{gen}
	add := func(lane, v, verb, subject, payload string) {
		t.Helper()
		ctx, err := admit.ContextOver(h.prefix)
		if err != nil {
			t.Fatalf("staging context: %v", err)
		}
		rec, err := event.Sign(event.Event{
			V: v, TS: h.ts(len(h.prefix)), Actor: h.fps[lane], Verb: verb,
			Subject: subject, Payload: json.RawMessage(payload), Prev: ctx.Tip,
		}, h.keys[lane])
		if err != nil {
			t.Fatal(err)
		}
		if err := admit.Check(ctx, rec); err != nil {
			t.Fatalf("staging %s by %s: %v", verb, lane, err)
		}
		h.prefix = append(h.prefix, rec)
	}
	// Up to the active protocol, one upgrade at a time.
	for i, v := range version.Supported() {
		if i == 0 {
			continue
		}
		add("root", version.Supported()[i-1], ledger.UpgradeVerb, "system", `{"to": "`+v+`"}`)
	}
	active := version.Supported()[len(version.Supported())-1]
	for _, l := range lanes {
		pub := h.keys[l.name].Public().(ed25519.PublicKey)
		add("root", active, keyring.VerbEnrolled, h.fps[l.name],
			fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), l.name))
		add("root", active, keyring.VerbGranted, h.fps[l.name], `{"capability": "`+l.cap+`"}`)
	}
	// A standard-tier contract: standard requires sealed checks, which is
	// what makes the unsealed class reachable at all (spec/tiers.md).
	add("root", active, "intent.filed", contractID,
		`{"intent": "reconciliation model", "tier": "standard", "budget": "small", "routing": "core"}`)
	add("root", active, "contract.specified", contractID,
		`{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`)
	if plantRawSeal {
		// Signed by the observer, which holds neither the sealer grant
		// VerifySeals checks for nor the claim the contract needs next.
		// The claim lane cannot be used here, and finding out why was
		// worth the detour: with a holder-authored seal in the chain,
		// admission refuses the holder's own claim.taken for authoring
		// isolation, so the raw push is caught at the next admitted step
		// rather than riding along. VerifySeals is the surface for the
		// case that does ride along.
		h.plant("observer", active, transition.CheckSealedVerb, contractID,
			fmt.Sprintf(`{"commitment": %q}`, zeros64))
	}
	add("holder", active, "claim.taken", contractID, `{}`)
	fence := len(h.prefix) - 1
	// The plan gate: above the trivial tier a submission needs an
	// approved plan on the subject, so the staging clears it the way a
	// real contract does rather than dropping to trivial, which would
	// also drop the sealed-checks requirement the unsealed class needs.
	planRef := `"plan": "plans/model.md @ ` + zeros40 + `"`
	add("holder", active, "plan.proposed", contractID,
		fmt.Sprintf(`{"fence": "%d", "digest": %q, %s}`, fence, zeros64, planRef))
	add("root", active, "plan.approved", contractID,
		fmt.Sprintf(`{"digest": %q, %s, "pr": "pr/1 @ %s"}`, zeros64, planRef, zeros40))
	add("holder", active, "submission.made", contractID,
		fmt.Sprintf(`{"fence": "%d", %s, "packet": %s}`, fence, planRef, minPacket))
	h.base = len(h.prefix)
	return h
}

// plant appends a record WITHOUT admission, as a raw push does.
func (h *harness) plant(lane, v, verb, subject, payload string) {
	h.t.Helper()
	last := h.prefix[len(h.prefix)-1]
	tip, err := last.Event.Hash()
	if err != nil {
		h.t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: v, TS: h.ts(len(h.prefix)), Actor: h.fps[lane], Verb: verb,
		Subject: subject, Payload: json.RawMessage(payload), Prev: tip,
	}, h.keys[lane])
	if err != nil {
		h.t.Fatal(err)
	}
	h.prefix = append(h.prefix, rec)
}

func (h *harness) ts(i int) string {
	return time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
}

func (h *harness) active() string {
	return version.Supported()[len(version.Supported())-1]
}

// draft builds the record one step proposes, reading its citations from
// the real fold so a step cites what the protocol says it must.
func (h *harness) draft(s *rstate, st step) (*event.Record, bool) {
	sub := s.subject()
	lane, verb, payload := "", st.name, ""
	switch st.name {
	case "verdict.pass", "verdict.fail":
		if sub.Submission == nil {
			return nil, false
		}
		outcome := "pass"
		if st.name == "verdict.fail" {
			outcome = "fail"
		}
		lane, verb = "verifier", transition.VerdictRenderedVerb
		payload = fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": "L1"}`,
			outcome, zeros64, sub.Submission.Pos)
	case "verdict.pass.overdeclared":
		// The record half of independence_unverified: a pass by the
		// verifier declaring a level the records do not support. The
		// boundary requires the declared level to equal the achieved
		// one, so this only ever reaches the chain raw, and
		// VerifyVerdicts re-judges it from the same facts.
		if sub.Submission == nil {
			return nil, false
		}
		lane, verb = "verifier", transition.VerdictRenderedVerb
		payload = fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L2"}`,
			zeros64, sub.Submission.Pos)
	case "verdict.pass.byholder":
		// The raw-push laundering setup: a pass verdict signed by the
		// implementing key, which admission refuses and which
		// VerifyVerdicts is built to surface.
		if sub.Submission == nil {
			return nil, false
		}
		lane, verb = "holder", transition.VerdictRenderedVerb
		payload = fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`,
			zeros64, sub.Submission.Pos)
	case transition.MergeRequestedVerb:
		if sub.Verdict == nil {
			return nil, false
		}
		lane = "holder"
		payload = fmt.Sprintf(`{"verdict": "%d"}`, sub.Verdict.Pos)
	case "merge.requested.override":
		// The override path: `overridden` needs the request to cite the
		// OVERRIDE, not a verdict, so the sanctioned substitute chain is
		// its own step rather than a variant of the verdict one.
		if sub.Override == nil {
			return nil, false
		}
		lane, verb = "holder", transition.MergeRequestedVerb
		payload = fmt.Sprintf(`{"override": "%d"}`, sub.Override.Pos)
	case transition.MergeObservedVerb:
		lane = "observer"
		payload = fmt.Sprintf(`{"merged": %q, "pr": "model"}`, zeros40)
	case transition.MergeOverriddenVerb:
		lane = "root"
		if st.raw {
			// override_unverified is about an override whose signer held
			// no operator standing at its position, so the raw variant is
			// signed by a lane that has none.
			lane = "holder"
		}
		if sub.Verdict != nil {
			payload = fmt.Sprintf(`{"reason": "model", "verdict": "%d"}`, sub.Verdict.Pos)
		} else {
			payload = `{"reason": "model", "verdict": "0"}`
		}
	case transition.CheckSealedVerb:
		lane, payload = "sealer", fmt.Sprintf(`{"commitment": %q}`, zeros64)
		if st.raw {
			// A seal by the claim lane: authoring isolation refused, and
			// VerifySeals is what surfaces it in raw-pushed history.
			lane = "holder"
		}
	default:
		return nil, false
	}
	rec, err := event.Sign(event.Event{
		V: h.active(), TS: h.ts(len(s.records)), Actor: h.fps[lane], Verb: verb,
		Subject: contractID, Payload: json.RawMessage(payload), Prev: s.tip(),
	}, h.keys[lane])
	if err != nil {
		h.t.Fatal(err)
	}
	return rec, true
}

func (s *rstate) tip() string {
	h, err := s.records[len(s.records)-1].Event.Hash()
	if err != nil {
		s.h.t.Fatal(err)
	}
	return h
}

const (
	zeros64   = "0000000000000000000000000000000000000000000000000000000000000000"
	zeros40   = "0000000000000000000000000000000000000000"
	minPacket = `{"acceptance": ["done"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
)

func fixtureKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

// declaredClasses reads the package's own source for its Class<Name>
// constants, so the partition above is pinned to what the package
// actually declares rather than to what someone remembered. This is the
// same shape as the affordance catalog's specCatalogVerbs pin: drift
// between the declaration and the list is a test failure. Parsing the
// source keeps the production surface untouched (plans/os-873b5153.md
// D8), which an exported AllClasses() for testability would not.
func declaredClasses(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\s*(?:const\s+)?Class[A-Za-z]+\s*=\s*"([a-z_]+)"`)
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no Class constants found: the pin cannot verify a partition it cannot read")
	}
	slices.Sort(out)
	return out
}

// ---------------------------------------------------------------------
// The properties (plans/os-873b5153.md D3).
// ---------------------------------------------------------------------

// conformance: charter §II.8 (plans/os-873b5153.md D3). Every
// interleaving of the verdict chain, the override, the forge and the
// raw-push adversary is enumerated; the real classifier says what each
// terminal chain diverges on; and four properties hold.
func TestReconciliationInterleavings(t *testing.T) {
	seenAdmitted := map[string]bool{} // class -> reached by a fully admitted trace
	seenRaw := map[string]bool{}      // class -> reached by a trace that used raw push
	terminals := 0
	for _, variant := range []struct {
		name  string
		plant bool
	}{{"clean", false}, {"raw-seal-planted", true}} {
		runVariant(t, variant.name, variant.plant, seenAdmitted, seenRaw, &terminals)
	}

	t.Logf("reconciliation: %d terminal states across both variants; admitted-reached %v; raw-reached %v",
		terminals, sortedKeys(seenAdmitted), sortedKeys(seenRaw))

	// P1: reachability by path. The failure names which set it expected,
	// because "unreachable" means something different in each.
	for _, c := range admittedReachable {
		// A raw trace reaching the class is no substitute (review on
		// #370): the claim is that admission itself can produce it, and
		// accepting seenRaw here would hide a front-door regression that
		// left the class reachable only by forgery.
		if !seenAdmitted[c] {
			t.Errorf("P1: %q is admitted-reachable by the partition but no fully admitted trace produced it", c)
		}
	}
	for _, c := range rawPushReachable {
		if !seenRaw[c] {
			t.Errorf("P1: %q is raw-push-reachable by the partition but no raw trace produced it", c)
		}
	}
}

// admittedHere reports whether the record would admit at the position
// just before it in the state's own chain.
func admittedHere(t *testing.T, s *rstate, rec *event.Record) bool {
	t.Helper()
	for i, r := range s.records {
		if r == rec {
			ctx, err := admit.ContextOver(s.records[:i])
			if err != nil {
				return false
			}
			return admit.Check(ctx, rec) == nil
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func traceText(path []step) string {
	parts := make([]string, len(path))
	for i, s := range path {
		parts[i] = s.String()
	}
	return strings.Join(parts, " ")
}

// runVariant walks one staging variant, folding its reachability into
// the shared sets so P1 is judged over both.
func runVariant(t *testing.T, name string, plantSeal bool, seenAdmitted, seenRaw map[string]bool, terminals *int) {
	t.Helper()
	h := stage(t, *depth, plantSeal)
	// A planted raw seal means every state of this variant already
	// carries raw-pushed history, so its findings are raw-reached.
	planted := plantSeal
	res, err := explore.Walk[*rstate, step](&rstate{h: h, records: h.prefix}, explore.Hooks[*rstate, step]{
		AtState: func(s *rstate, path []step) {
			// Reachability is a property of any reachable state, not only
			// a terminal one: a later step can heal a divergence
			// (unreconciled becomes reconciled once the merge is
			// observed), so sampling terminals alone under-reports.
			for _, f := range reconcile.Classify(s.records, s.fold()) {
				if s.usedRaw || planted {
					seenRaw[f.Class] = true
				} else {
					seenAdmitted[f.Class] = true
				}
			}
			// P3: a raw-pushed verdict that fails the verifier boundary is
			// never laundered by an ADMITTED later step. The raw-pushed
			// state exists and the classifier surfaces it; what the front
			// door must refuse is building a clean chain on top.
			if !s.usedRaw {
				return
			}
			sub := s.subject()
			if sub.Verdict == nil || sub.Verdict.Verdict != "pass" {
				return
			}
			if sub.Verdict.Signer != h.fps["holder"] {
				return
			}
			// Laundering is about what a step RELIES ON, not about what
			// follows it in the chain. Two earlier drafts of this check
			// were too broad: the first flagged a merge.requested
			// admitted BEFORE the raw push, the second flagged one that
			// took the override-backed path and never cited the raw
			// verdict at all. The property is precise: a merge step that
			// rests on a verdict failing the verifier boundary must not
			// admit.
			for i := sub.Verdict.Pos + 1; i < len(s.records); i++ {
				r := s.records[i]
				var p struct {
					Verdict  string `json:"verdict"`
					Override string `json:"override"`
				}
				switch r.Event.Verb {
				case transition.MergeRequestedVerb:
					if json.Unmarshal(r.Event.Payload, &p) != nil {
						continue
					}
					// An override-backed request rests on the override and
					// its own backing fail verdict, not on this one.
					if strings.TrimSpace(p.Override) != "" {
						continue
					}
					if p.Verdict != fmt.Sprintf("%d", sub.Verdict.Pos) {
						continue
					}
				case transition.MergeObservedVerb:
					// The observation reads the fold rather than citing:
					// it rests on this verdict unless the chain ran
					// through an override instead.
					if sub.Override != nil && sub.Requested != nil && sub.Requested.CitedOverride == sub.Override.Pos {
						continue
					}
				default:
					continue
				}
				if admittedHere(t, s, r) {
					t.Fatalf("P3: %s at position %d admitted resting on the implementing key's pass verdict at position %d — laundering the raw push into a clean chain\n%s",
						r.Event.Verb, i, sub.Verdict.Pos, traceText(path))
				}
			}
		},
		AtTerminal: func(s *rstate, path []step) {
			*terminals++
			fold := s.fold()
			findings := reconcile.Classify(s.records, fold)
			for _, f := range findings {
				// P2: every finding carries a class the partition names.
				if !slices.Contains(declaredClasses(t), f.Class) {
					t.Fatalf("P2: the classifier returned class %q, which the package does not declare\n%s", f.Class, traceText(path))
				}
			}
			// P4: done is reachable only behind an authentic pass verdict
			// or a cited override, ON A FULLY ADMITTED TRACE. A raw push
			// can plant done with neither, and that is precisely what
			// merge_without_verdict reports rather than prevents.
			if !s.usedRaw {
				sub, ok := fold.State(contractID)
				if ok && sub.State == "done" {
					pass := sub.CitedPass()
					override := sub.Override != nil && sub.Requested != nil && sub.Requested.CitedOverride == sub.Override.Pos
					if !pass && !override {
						t.Fatalf("P4: an admitted trace reached done with neither an authentic pass verdict nor a cited override\n%s", traceText(path))
					}
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("  variant %s: %d states, %d terminal", name, res.States, res.Terminals())
}
