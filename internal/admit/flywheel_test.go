package admit

// The flywheel at the boundary (plans/os-9075c308.md D4;
// spec/flywheel.md): workflow.proposed is the curator's act over
// a recurring shape and nothing else's, held to the record at every
// gate; workflow.merged is the observer's, over the standing proposal;
// raw pushes bind nothing; the repair contract is the dispatcher's
// filing and the proposal cites it once passed. The stand drives
// every done contract through the boundary, so the occurrences a
// proposal cites are admitted facts.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	choreSpec = `{"acceptance": {"ref": "accept.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`
	otherSpec = `{"acceptance": {"ref": "other.md @ abc1234", "executable": true, "gate": "pr/1 @ abc1234"}}`
	sha40     = "efefefefefefefefefefefefefefefefefefefef"
)

type flywheelStand struct {
	root, worker, curator, observer, verifier, dispatcher, stranger ed25519.PrivateKey
	v                                                               string
	ctx                                                             *Context
	step                                                            func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context
	done                                                            map[string]int
}

func flywheelFixture(t *testing.T) *flywheelStand {
	t.Helper()
	store, resolve, root := seededStore(t)
	st := &flywheelStand{root: root, v: version.Seed3, done: map[string]int{}}
	st.worker, st.curator, st.observer, st.verifier, st.dispatcher, st.stranger =
		fixtureKey(t, 41), fixtureKey(t, 42), fixtureKey(t, 43), fixtureKey(t, 44), fixtureKey(t, 45), fixtureKey(t, 46)
	keys := []ed25519.PrivateKey{root, st.worker, st.curator, st.observer, st.verifier, st.dispatcher, st.stranger}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range keys {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	appendSigned(t, store, loose, root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	appendSignedV(t, store, loose, root, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	appendSignedV(t, store, loose, root, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	st.step = func(priv ed25519.PrivateKey, v, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, v, verb, subject, payload)
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for _, e := range []struct {
		key  ed25519.PrivateKey
		name string
		cap  string
	}{
		{st.worker, "worker", keyring.CapClaim}, {st.curator, "curator", keyring.CapCurate},
		{st.observer, "observer", keyring.CapObserver}, {st.verifier, "verifier", keyring.CapVerdict},
		{st.dispatcher, "dispatcher", keyring.CapDispatch},
	} {
		st.ctx = st.step(root, st.v, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
		st.ctx = st.step(root, st.v, keyring.VerbGranted, fpOf(t, e.key), `{"capability": "`+e.cap+`"}`)
	}
	st.ctx = st.step(root, st.v, keyring.VerbEnrolled, fpOf(t, st.stranger), enrollBody(t, st.stranger, "agent", "stranger"))
	return st
}

// admit checks the act at the boundary, appends it, and returns its
// position.
func (st *flywheelStand) admit(t *testing.T, priv ed25519.PrivateKey, verb, subject, payload string) int {
	t.Helper()
	if err := Check(st.ctx, draftV(t, priv, st.v, verb, subject, payload, st.ctx.Tip)); err != nil {
		t.Fatalf("%s on %s admits: %v", verb, subject, err)
	}
	pos := st.ctx.Count
	st.ctx = st.step(priv, st.v, verb, subject, payload)
	return pos
}

// refuse asserts the boundary refuses the act and returns the refusal.
func (st *flywheelStand) refuse(t *testing.T, priv ed25519.PrivateKey, verb, subject, payload string) error {
	t.Helper()
	err := Check(st.ctx, draftV(t, priv, st.v, verb, subject, payload, st.ctx.Tip))
	if err == nil {
		t.Fatalf("%s on %s must refuse", verb, subject)
	}
	return err
}

// finish drives a specified subject to done through the boundary.
func (st *flywheelStand) finish(t *testing.T, subject string) int {
	t.Helper()
	st.admit(t, st.worker, "claim.taken", subject, `{}`)
	st.admit(t, st.worker, "submission.made", subject, `{"fence": "`+activeFence(t, st.ctx, subject)+`", "packet": `+minPacket+`}`)
	st.admit(t, st.verifier, "verdict.rendered", subject, `{"verdict": "pass", "receipt": "`+zeros64+`", "submission": "`+submissionOf(t, st.ctx, subject)+`", "independence": "L1"}`)
	st.admit(t, st.worker, "merge.requested", subject, `{"verdict": "`+verdictOf(t, st.ctx, subject)+`"}`)
	pos := st.admit(t, st.observer, "merge.observed", subject, observedBody(sha40))
	st.done[subject] = pos
	return pos
}

// chore files, specifies and finishes one contract of the chore's
// shape (routing core, accept.md, trivial).
func (st *flywheelStand) chore(t *testing.T, subject, intent string) int {
	t.Helper()
	st.admit(t, st.root, "intent.filed", subject, fmt.Sprintf(`{"intent": %q, "tier": "trivial", "budget": "small", "routing": "core"}`, intent))
	st.admit(t, st.root, "contract.specified", subject, choreSpec)
	return st.finish(t, subject)
}

// failed files a contract of the chore's shape and fails it.
func (st *flywheelStand) failed(t *testing.T, subject string) {
	t.Helper()
	st.admit(t, st.root, "intent.filed", subject, trivialFiling)
	st.admit(t, st.root, "contract.specified", subject, choreSpec)
	st.admit(t, st.worker, "claim.taken", subject, `{}`)
	st.admit(t, st.worker, "submission.made", subject, `{"fence": "`+activeFence(t, st.ctx, subject)+`", "packet": `+minPacket+`}`)
	st.admit(t, st.verifier, "verdict.rendered", subject, `{"verdict": "fail", "receipt": "`+zeros64+`", "submission": "`+submissionOf(t, st.ctx, subject)+`", "independence": "L1"}`)
}

func (st *flywheelStand) shape(t *testing.T) flywheel.Shape {
	t.Helper()
	for _, s := range flywheel.Shapes(st.ctx.Records, st.ctx.Lifecycle) {
		if s.AcceptancePath == "accept.md" {
			return s
		}
	}
	t.Fatal("no chore shape in the record")
	return flywheel.Shape{}
}

// proposal is a shape-valid proposal for the shape, citing all its
// occurrences.
func (st *flywheelStand) proposal(shape flywheel.Shape, run, repair string) string {
	var cites []string
	for _, o := range shape.Occurrences {
		cites = append(cites, fmt.Sprintf("%q", o.Cite()))
	}
	rep := ""
	if repair != "" {
		rep = fmt.Sprintf(`, "repair": %q`, repair)
	}
	return fmt.Sprintf(`{"shape": %q, "workflow": %q, "occurrences": [%s], "validated": {"run": %q}%s}`,
		shape.ID, flywheel.RegistryDir+"/"+shape.Name()+".yaml @ "+sha40, strings.Join(cites, ", "), run, rep)
}

func (st *flywheelStand) merge(shape flywheel.Shape, path string) string {
	return fmt.Sprintf(`{"workflow": %q, "shape": %q, "pr": "pr/7 @ %s"}`, path+" @ "+sha40, shape.ID, sha40)
}

func (st *flywheelStand) lanes() map[string]ed25519.PrivateKey {
	return map[string]ed25519.PrivateKey{"root": st.root, "worker": st.worker, "curator": st.curator, "observer": st.observer,
		"verifier": st.verifier, "dispatcher": st.dispatcher, "stranger": st.stranger}
}

func listsVerb(ctx *Context, key ed25519.PrivateKey, subject, verb string) bool {
	return slices.Contains(Affordances(ctx, key, subject), verb)
}

// flywheelGate returns the gate of a flywheel refusal, or "" when the
// refusal is another rule's (the grant's, typically).
func flywheelGate(err error) string {
	var e *flywheel.Error
	if errors.As(err, &e) {
		return e.Gate
	}
	return ""
}

// flywheelCuratorReach is the curator's affordance set at the position
// where a shape recurs: the curator residual drill unions it into the
// reach it pins against the residual table.
func flywheelCuratorReach(t *testing.T) []string {
	t.Helper()
	st := flywheelFixture(t)
	st.chore(t, "c-1", "fix the check")
	st.chore(t, "c-2", "fix the check again")
	shape := st.shape(t)
	out := Affordances(st.ctx, st.curator, shape.ID)
	for _, verb := range Affordances(st.ctx, st.curator, "c-1") {
		if !slices.Contains(out, verb) {
			out = append(out, verb)
		}
	}
	return out
}

// conformance: AC5, D4 — the proposal is the curator's affordance
// exactly when a shape recurs with no proposal standing; every other
// lane, the root included, is refused at the grant; a raw push by
// another key folds as an anomaly and reserves nothing; once a
// proposal stands the merge observation is the observer's and only
// the proposed file admits; after the merge the shape can be proposed
// again and a second merge cannot be observed.
func TestFlywheelProposalIsTheCuratorsOverARecurringShapeAndTheMergeTheObservers(t *testing.T) {
	st := flywheelFixture(t)
	st.chore(t, "c-1", "fix the check")
	for name, key := range st.lanes() {
		if listsVerb(st.ctx, key, "c-1", flywheel.ProposedVerb) {
			t.Fatalf("one done contract is no chore, and %s lists the proposal", name)
		}
	}
	st.chore(t, "c-2", "fix the check again")
	shape := st.shape(t)
	if !shape.Recurring() || len(shape.Occurrences) != 2 || shape.Occurrences[0].Done != st.done["c-1"] || shape.Occurrences[1].Done != st.done["c-2"] {
		t.Fatalf("two admitted done contracts recur, cited at their merge observations: %+v", shape)
	}
	for _, subject := range []string{"c-1", shape.ID, "anything"} {
		if !listsVerb(st.ctx, st.curator, subject, flywheel.ProposedVerb) {
			t.Fatalf("the curator lists the proposal on the recurring shape when asked on %s", subject)
		}
	}
	for name, key := range st.lanes() {
		if name != "curator" && listsVerb(st.ctx, key, shape.ID, flywheel.ProposedVerb) {
			t.Fatalf("%s lists the proposal: curate alone reaches it", name)
		}
		if listsVerb(st.ctx, key, shape.ID, flywheel.MergedVerb) {
			t.Fatalf("%s lists the merge with no proposal standing", name)
		}
	}
	good := st.proposal(shape, "wf-1", "")
	for name, key := range st.lanes() {
		if name == "curator" {
			continue
		}
		if err := st.refuse(t, key, flywheel.ProposedVerb, shape.ID, good); flywheelGate(err) != "" {
			t.Fatalf("%s is refused at the grant, before any flywheel gate: %v", name, err)
		}
	}
	// A raw push by the worker, well-shaped: an anomaly, no reservation.
	st.ctx = st.step(st.worker, st.v, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-raw", ""))
	if f := flywheel.Fold(st.ctx.Records); f.Anomalies != 1 || f.Any() {
		t.Fatalf("a raw proposal folds as an anomaly and binds nothing: %+v", f)
	}
	if !listsVerb(st.ctx, st.curator, shape.ID, flywheel.ProposedVerb) {
		t.Fatal("a raw push reserves nothing: the curator still lists the proposal")
	}
	pPos := st.admit(t, st.curator, flywheel.ProposedVerb, shape.ID, good)
	standing, ok := flywheel.Fold(st.ctx.Records).Standing(shape.ID)
	if !ok || standing.Pos != pPos || standing.Actor != fpOf(t, st.curator) {
		t.Fatalf("the admitted proposal stands at its position under the curator's key: %+v", standing)
	}
	if listsVerb(st.ctx, st.curator, shape.ID, flywheel.ProposedVerb) {
		t.Fatal("a standing proposal is not proposed again")
	}
	if err := st.refuse(t, st.curator, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-2", "")); flywheelGate(err) != flywheel.GateDuplicate {
		t.Fatalf("a second proposal refuses as a duplicate: %v", err)
	}
	// The observer's, with the operator fallback merge.observed has:
	// the root holds operator implicitly, and nobody else lists it.
	for name, key := range st.lanes() {
		if name == "observer" || name == "root" {
			continue
		}
		if listsVerb(st.ctx, key, shape.ID, flywheel.MergedVerb) {
			t.Fatalf("%s lists the merge: the observation is the observer's", name)
		}
	}
	if !listsVerb(st.ctx, st.root, shape.ID, flywheel.MergedVerb) {
		t.Fatal("the root's implicit operator reaches the merge observation, as it reaches merge.observed")
	}
	for _, subject := range []string{shape.ID, "c-1"} {
		if !listsVerb(st.ctx, st.observer, subject, flywheel.MergedVerb) {
			t.Fatalf("the observer lists the merge over the standing proposal when asked on %s", subject)
		}
	}
	if err := st.refuse(t, st.observer, flywheel.MergedVerb, shape.ID, st.merge(shape, flywheel.RegistryDir+"/elsewhere.yaml")); flywheelGate(err) != flywheel.GateMerge {
		t.Fatalf("only the proposed file's merge is observable: %v", err)
	}
	if err := st.refuse(t, st.worker, flywheel.MergedVerb, shape.ID, st.merge(shape, standing.Path())); flywheelGate(err) != "" {
		t.Fatalf("the worker is refused at the grant: %v", err)
	}
	mPos := st.admit(t, st.observer, flywheel.MergedVerb, shape.ID, st.merge(shape, standing.Path()))
	f := flywheel.Fold(st.ctx.Records)
	if !f.Merged(shape.ID) || f.Merges[shape.ID][0].Pos != mPos || f.Anomalies != 1 {
		t.Fatalf("the merge binds: %+v", f)
	}
	if _, ok := f.Standing(shape.ID); ok {
		t.Fatal("a merged proposal no longer stands")
	}
	for name, key := range st.lanes() {
		if listsVerb(st.ctx, key, shape.ID, flywheel.MergedVerb) {
			t.Fatalf("%s lists a second merge with nothing standing", name)
		}
	}
	if !listsVerb(st.ctx, st.curator, shape.ID, flywheel.ProposedVerb) {
		t.Fatal("after the merge the shape can be proposed again")
	}
	if err := st.refuse(t, st.observer, flywheel.MergedVerb, shape.ID, st.merge(shape, standing.Path())); flywheelGate(err) != flywheel.GateMerge || !strings.Contains(err.Error(), "no unmerged proposal stands") {
		t.Fatalf("nothing stands to merge, and the refusal says so: %v", err)
	}
	m := flywheel.Derive(st.ctx.Records, st.ctx.Lifecycle)
	if m.Recurring != 1 || m.Proposed != 1 || m.Merged != 1 || m.ConversionRate == nil || *m.ConversionRate != "1.000" {
		t.Fatalf("one chore, proposed and merged: %+v", m)
	}
}

// conformance: AC5, D4 — every proposal gate refuses at the boundary
// with its name: the subject is the shape, the file lives directly
// under the registry, the occurrences are distinct admitted done
// contracts of the shape and at least two of them, and the payload is
// strict.
func TestFlywheelProposalGatesRefuseAtTheBoundary(t *testing.T) {
	st := flywheelFixture(t)
	st.chore(t, "c-1", "fix the check")
	st.chore(t, "c-2", "fix the check again")
	st.admit(t, st.root, "intent.filed", "c-3", trivialFiling)
	st.admit(t, st.root, "contract.specified", "c-3", otherSpec)
	st.finish(t, "c-3")
	st.failed(t, "c-x")
	shape := st.shape(t)
	other, _ := flywheel.Find(flywheel.Shapes(st.ctx.Records, st.ctx.Lifecycle), "")
	for _, s := range flywheel.Shapes(st.ctx.Records, st.ctx.Lifecycle) {
		if s.AcceptancePath == "other.md" {
			other = s
		}
	}
	if other.Recurring() || len(other.Occurrences) != 1 {
		t.Fatalf("the other shape occurs once: %+v", other)
	}
	if err := Check(st.ctx, draftV(t, st.curator, st.v, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-1", ""), st.ctx.Tip)); err != nil {
		t.Fatalf("the well-formed proposal admits: %v", err)
	}
	cites := []string{shape.Occurrences[0].Cite(), shape.Occurrences[1].Cite()}
	path := flywheel.RegistryDir + "/" + shape.Name() + ".yaml @ " + sha40
	body := func(subjectShape, workflow string, occ []string) string {
		var q []string
		for _, c := range occ {
			q = append(q, fmt.Sprintf("%q", c))
		}
		return fmt.Sprintf(`{"shape": %q, "workflow": %q, "occurrences": [%s], "validated": {"run": "wf-1"}}`, subjectShape, workflow, strings.Join(q, ", "))
	}
	for name, c := range map[string]struct {
		subject, payload, gate string
	}{
		"on a contract":    {"c-1", body(shape.ID, path, cites), flywheel.GateShape},
		"another shape":    {shape.ID, body(other.ID, path, cites), flywheel.GateShape},
		"unknown field":    {shape.ID, `{"shape": "` + shape.ID + `", "workflow": "` + path + `", "occurrences": ["` + cites[0] + `", "` + cites[1] + `"], "validated": {"run": "wf-1"}, "extra": true}`, flywheel.GateShape},
		"no run":           {shape.ID, `{"shape": "` + shape.ID + `", "workflow": "` + path + `", "occurrences": ["` + cites[0] + `", "` + cites[1] + `"], "validated": {"run": ""}}`, flywheel.GateShape},
		"path not anchor":  {shape.ID, body(shape.ID, flywheel.RegistryDir+"/x.yaml", cites), flywheel.GatePath},
		"path outside":     {shape.ID, body(shape.ID, "workflows/x.yaml @ "+sha40, cites), flywheel.GatePath},
		"path nested":      {shape.ID, body(shape.ID, flywheel.RegistryDir+"/a/x.yaml @ "+sha40, cites), flywheel.GatePath},
		"one occurrence":   {shape.ID, body(shape.ID, path, cites[:1]), flywheel.GateOccurrences},
		"none":             {shape.ID, body(shape.ID, path, nil), flywheel.GateOccurrences},
		"twice":            {shape.ID, body(shape.ID, path, []string{cites[0], cites[0]}), flywheel.GateOccurrences},
		"other shape's":    {shape.ID, body(shape.ID, path, []string{cites[0], other.Occurrences[0].Cite()}), flywheel.GateOccurrences},
		"failed contract":  {shape.ID, body(shape.ID, path, []string{cites[0], fmt.Sprintf("c-x@%d", st.ctx.Count-1)}), flywheel.GateOccurrences},
		"wrong position":   {shape.ID, body(shape.ID, path, []string{cites[0], "c-2@1"}), flywheel.GateOccurrences},
		"unknown contract": {shape.ID, body(shape.ID, path, []string{cites[0], "c-9@9"}), flywheel.GateOccurrences},
		"malformed cite":   {shape.ID, body(shape.ID, path, []string{cites[0], "c-2"}), flywheel.GateOccurrences},
		"repair not real":  {shape.ID, `{"shape": "` + shape.ID + `", "workflow": "` + path + `", "occurrences": ["` + cites[0] + `", "` + cites[1] + `"], "validated": {"run": "wf-1"}, "repair": "c-1@2"}`, flywheel.GateRepairCited},
	} {
		err := st.refuse(t, st.curator, flywheel.ProposedVerb, c.subject, c.payload)
		if g := flywheelGate(err); g != c.gate {
			t.Fatalf("%s: refused at %q, want %q: %v", name, g, c.gate, err)
		}
	}
	// The other shape, once: not proposable at all.
	if err := st.refuse(t, st.curator, flywheel.ProposedVerb, other.ID, st.proposal(other, "wf-1", "")); flywheelGate(err) != flywheel.GateOccurrences {
		t.Fatalf("a shape done once is no chore: %v", err)
	}
	// The merge on the other shape, with nothing standing: refused.
	if err := st.refuse(t, st.observer, flywheel.MergedVerb, other.ID, st.merge(other, flywheel.RegistryDir+"/x.yaml")); flywheelGate(err) != flywheel.GateMerge || !strings.Contains(err.Error(), "no unmerged proposal stands") {
		t.Fatalf("a merge cites a standing proposal, and the refusal says so: %v", err)
	}
	if err := st.refuse(t, st.observer, flywheel.MergedVerb, "c-1", st.merge(shape, flywheel.RegistryDir+"/x.yaml")); flywheelGate(err) != flywheel.GateMerge {
		t.Fatalf("a merge is appended on its shape: %v", err)
	}
}

// conformance: AC7, D7 — the repair contract is the dispatcher's
// filing (a claim or curate key is refused at the grant), blocks the
// proposal while short of a passed verdict, and once passed must be
// cited at its verdict position; the metrics count it filed and done.
func TestFlywheelRepairIsTheDispatchersAndTheProposalCitesItOncePassed(t *testing.T) {
	st := flywheelFixture(t)
	st.chore(t, "c-1", "fix the check")
	st.chore(t, "c-2", "fix the check again")
	shape := st.shape(t)
	d := &flywheel.Draft{Name: shape.Name(), Shape: shape.ID}
	refused := &flywheel.EngineError{Name: d.Name, Stage: "mock", Step: "verdict", Finding: "step verdict produce verdict violates its schema"}
	intent, spec := flywheel.RepairFiling(shape, d, refused, "seed/flywheel-"+shape.ID, "abc1234")
	subject := flywheel.RepairSubject(shape)
	for name, key := range map[string]ed25519.PrivateKey{"worker": st.worker, "curator": st.curator, "observer": st.observer, "stranger": st.stranger} {
		if err := st.refuse(t, key, "intent.filed", subject, string(intent)); flywheelGate(err) != "" {
			t.Fatalf("%s cannot file the repair: refused at the grant: %v", name, err)
		}
	}
	st.admit(t, st.dispatcher, "intent.filed", subject, string(intent))
	st.admit(t, st.dispatcher, "contract.specified", subject, string(spec))
	state, ok := st.ctx.Lifecycle.State(subject)
	if !ok || state.State != "ready" || state.Tier != "trivial" || !state.Acceptance.Executable || !state.Acceptance.Gated {
		t.Fatalf("the repair is a ready trivial contract with a gated executable acceptance: %+v", state)
	}
	if id, isRepair := flywheel.IsRepair(state); !isRepair || id != shape.ID {
		t.Fatalf("the repair is recognized for its shape: %s %v", id, isRepair)
	}
	if err := st.refuse(t, st.curator, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-1", "")); flywheelGate(err) != flywheel.GateRepairOpen {
		t.Fatalf("an open repair blocks the proposal: %v", err)
	}
	if listsVerb(st.ctx, st.curator, shape.ID, flywheel.ProposedVerb) {
		t.Fatal("with a repair open the proposal is not an affordance")
	}
	m := flywheel.Derive(st.ctx.Records, st.ctx.Lifecycle)
	if m.Repairs.Filed != 1 || m.Repairs.Done != 0 || m.Proposed != 0 {
		t.Fatalf("one repair filed: %+v", m)
	}
	// The implementer's fix passes its verdict.
	st.admit(t, st.worker, "claim.taken", subject, `{}`)
	st.admit(t, st.worker, "submission.made", subject, `{"fence": "`+activeFence(t, st.ctx, subject)+`", "packet": `+minPacket+`}`)
	vPos := st.admit(t, st.verifier, "verdict.rendered", subject, `{"verdict": "pass", "receipt": "`+zeros64+`", "submission": "`+submissionOf(t, st.ctx, subject)+`", "independence": "L1"}`)
	if err := st.refuse(t, st.curator, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-1", "")); flywheelGate(err) != flywheel.GateRepairCited {
		t.Fatalf("a passed repair must be cited: %v", err)
	}
	if err := st.refuse(t, st.curator, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-1", fmt.Sprintf("%s@%d", subject, vPos-1))); flywheelGate(err) != flywheel.GateRepairCited {
		t.Fatalf("the citation is the verdict's position: %v", err)
	}
	cite := fmt.Sprintf("%s@%d", subject, vPos)
	// The probe cites the passed repair, so the affordance returns.
	if !listsVerb(st.ctx, st.curator, shape.ID, flywheel.ProposedVerb) {
		t.Fatal("with the repair passed the proposal is an affordance again")
	}
	pPos := st.admit(t, st.curator, flywheel.ProposedVerb, shape.ID, st.proposal(shape, "wf-2", cite))
	standing, ok := flywheel.Fold(st.ctx.Records).Standing(shape.ID)
	if !ok || standing.Pos != pPos || standing.Repair != cite {
		t.Fatalf("the proposal stands citing the repair: %+v", standing)
	}
	// The repair's own merge counts it done.
	st.admit(t, st.worker, "merge.requested", subject, `{"verdict": "`+verdictOf(t, st.ctx, subject)+`"}`)
	st.admit(t, st.observer, "merge.observed", subject, observedBody(sha40))
	m = flywheel.Derive(st.ctx.Records, st.ctx.Lifecycle)
	if m.Repairs.Filed != 1 || m.Repairs.Done != 1 || m.Proposed != 1 || m.Recurring != 1 {
		t.Fatalf("the repair is done and the chore proposed: %+v", m)
	}
}
