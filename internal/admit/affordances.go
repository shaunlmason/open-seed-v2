package admit

// The affordance computation (plans/os-f5551001.md; SEED-NEXT.md
// §II.10; charter III.I rows 1 and 2): the verbs currently legal for
// this actor on this subject, computed by drafting one SIGNED probe
// record per catalog verb and running the SAME Check pipeline
// admission enforces — one rule set, two consumers, zero exceptions,
// zero drift by construction. The catalog's payload synthesizers are
// synthesis data, never legality logic: they fill each verb's strict
// wire shape from the live context (the active fence, an open
// reservation, the bound submission, the standing verdict), and
// where an anchor is absent they fill a placeholder position, which
// the rules then refuse exactly as they would refuse the real
// append. Probes are drafted and checked, never appended, so
// computing affordances mutates nothing.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// probeView is the context snapshot the synthesizers fill templates
// from. Absent anchors hold "0": a well-formed citation the rules
// refuse, so illegality is judged by the rule set, never by the
// synthesizer.
type probeView struct {
	// The flywheel probes' citations (plans/os-9075c308.md): the
	// first recurring shape without a standing proposal and its
	// occurrences, and the first standing proposal's shape and path.
	flywheelShape, flywheelOccurrences, flywheelStanding, flywheelPath, flywheelRepair string
	// support, hypothesis and contest are the curation probes'
	// citations (curationProbes).
	support, hypothesis, contest string
	// retireLesson and retireHypothesis are the standing promotion the
	// retirement probe cites; deadEnd and retiredDeadEnd the dead-end
	// citations the retire and un-retire probes make on the queried
	// subject, each with the environment that act would name
	// (plans/os-0d537fbd.md D2, D3).
	retireLesson, retireHypothesis     string
	deadEnd, deadEndEnvironment        string
	retiredDeadEnd, retiredEnvironment string
	// version is the chain's active protocol version: a probe must
	// synthesize the payload shape THAT version admits, or the
	// affordance list would say a verb is unavailable because the
	// probe spoke the wrong dialect (plans/os-8e53ffd9.md).
	version string
	now     string
	expires string
	// observationHead is the head the submission under review names,
	// observationChecks a check state differing from the standing
	// observation's, and redObservation the standing red observation's
	// position when one stands on that head (plans/os-0cd18799.md).
	observationHead, observationChecks, redObservation, observationPR string
	fence                                                             string
	active                                                            bool
	reservation                                                       string
	submission                                                        string
	verdict                                                           string
	packet                                                            string
	escalation                                                        string
	// standing is whether a question stands right now: several
	// synthesizers must carry its citation only then.
	standing bool
	// choice is an id the STANDING question actually offers. A guess
	// would make a legal answer look unavailable: the probe is
	// judged by the same rule admission enforces, which refuses a
	// choice outside the set (review finding on #200).
	choice string
	// position is this view's own count, so a checkpoint probe cites
	// the position it would actually materialize rather than a
	// constant the rule would have to be loosened to accept.
	position string
	// qualify and disqualify are the qualification verbs' payloads
	// for the queried subject read as an ACTOR (plans/os-03e47abb.md):
	// the eval pass this actor's window earned and the configuration
	// it ran, or the eval fail whose configuration this actor still
	// holds. A citation the rules would refuse when none stands, so
	// the list says "you may mint this now" only when it is true.
	qualify    string
	disqualify string
	// requestAbout is the queried subject when it is a contract on
	// the chain, the `about` a request.filed probe on it must name;
	// request is the position of the first unanswered request on the
	// queried subject, "0" (the genesis position, never a request)
	// when none stands, so the answer probe is refused by the rule
	// rather than by a guess (plans/os-48df10a2.md).
	requestAbout string
	request      string
	// actor is the probing key's fingerprint, the actor an
	// approval.requested probe names as the one that will act;
	// approval is the position of the oldest unanswered approval
	// request on the queried subject, "0" when none stands, so the
	// grant and deny probes are refused by the rule rather than by a
	// guess (plans/os-5781a026.md D7).
	actor    string
	approval string
	// relative is another open contract on the chain, the target a
	// dependency.linked or hierarchy.parented probe on the queried
	// subject names, the queried subject itself when no other stands
	// (a self edge the rule refuses, so the verb is drafted exactly
	// where something is linkable); unlink is the first active
	// dependency of the queried subject, the queried subject itself
	// when it has none (an absent unlink the rule refuses)
	// (plans/os-f0ae2cdf.md).
	relative string
	unlink   string
	// erasable is the digest the queried subject's fold references
	// (its sealed commitment, else its latest verdict's receipt), the
	// artifact an artifact.erased probe on it names; a zero digest
	// when it references none, which the erasure rule refuses as
	// unreferenced, so the verb is drafted exactly where something is
	// erasable (plans/os-db5cd353.md D6).
	erasable string
}

// qualificationProbes derives the qualification verbs' payloads for
// the queried subject read as an actor: the first eval subject whose
// authenticated pass sits on a window this actor held, with the
// configuration that window's admitted start declared, and the first
// eval fail whose declared configuration this actor still holds. Where
// none stands the probe cites a contract and position the rules
// refuse, so illegality is judged by the rule set, never here.
// probeQualify and probeDisqualify are the citations that stand when
// no eval does: well-formed, and refused by the qualification rule.
const (
	probeQualify    = `{"capability": "claim", "tuple": ` + probeTuple + `, "contract": "probe", "verdict": "0"}`
	probeDisqualify = `{"capability": "claim", "tuple": ` + probeTuple + `, "contract": "probe", "verdict": "0", "reason": "probe"}`
)

func qualificationProbes(ctx *Context, actor string) (qualify, disqualify string) {
	qualify, disqualify = probeQualify, probeDisqualify
	if ctx == nil || ctx.Lifecycle == nil || ctx.Keyring == nil {
		return qualify, disqualify
	}
	foundQ, foundD := false, false
	for _, subject := range ctx.Lifecycle.Subjects() {
		s, ok := ctx.Lifecycle.State(subject)
		if !ok || s.Eval == nil {
			continue
		}
		if !foundQ {
			if fact := authenticPass(ctx, subject, s); fact != nil {
				if fence, holder, ok := submissionWindow(ctx.Records, subject, fact.Submission); ok && holder == actor {
					if declared := windowDeclaration(ctx, subject, s, fence); declared != nil {
						b, _ := json.Marshal(declared)
						qualify = fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d"}`, b, subject, fact.Pos)
						foundQ = true
					}
				}
			}
		}
		if !foundD {
			if fact := authenticFail(ctx, subject, s); fact != nil {
				if fence, _, ok := submissionWindow(ctx.Records, subject, fact.Submission); ok {
					if declared := windowDeclaration(ctx, subject, s, fence); declared != nil {
						for _, held := range ctx.Keyring.GrantTuples(actor, keyring.CapClaim) {
							if held.Equal(*declared) {
								b, _ := json.Marshal(declared)
								disqualify = fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": %q, "verdict": "%d", "reason": "the eval failed"}`, b, subject, fact.Pos)
								foundD = true
								break
							}
						}
					}
				}
			}
		}
	}
	return qualify, disqualify
}

// windowDeclaration is the tuple the admitted run.started in the
// window at fence declared, nil when no valid start declared one.
func windowDeclaration(ctx *Context, subject string, s transition.SubjectState, fence int) *tuple.Tuple {
	for i := range s.RunStarts {
		st := s.RunStarts[i]
		if st.Fence == fence && RunStartValid(ctx.Records, ctx.Table, subject, st) {
			return st.Tuple
		}
	}
	return nil
}

// fenceKV is the optional fence citation: on a held subject the
// fence rule requires holder-signed events to cite the active fence
// and lets anyone else cite it, while outside a window any citation
// refuses — so fence-optional verbs cite it exactly when one is
// active.
func (v *probeView) fenceKV() string {
	if !v.active {
		return ""
	}
	return `"fence": "` + v.fence + `", `
}

// flywheelProbes derive what the flywheel verbs' probes cite
// (plans/os-9075c308.md): the first recurring shape with no standing
// proposal and its first RecurringAfter occurrences, for the
// proposal; the first shape with a standing proposal and the proposed
// path, for the merge observation. Absent facts leave the probe's own
// placeholders, which the rule refuses.
func flywheelProbes(ctx *Context) (shape, occurrences, standing, path, repair string) {
	shape, occurrences, standing, path = "s-probe", "[]", "s-probe", flywheel.RegistryDir+"/probe.yaml"
	if ctx == nil || ctx.Lifecycle == nil {
		return
	}
	st := flywheel.Fold(ctx.Records)
	for _, s := range flywheel.Shapes(ctx.Records, ctx.Lifecycle) {
		if !s.Recurring() {
			continue
		}
		if fact, ok := st.Standing(s.ID); ok {
			if standing == "s-probe" {
				standing, path = s.ID, fact.Path()
			}
			continue
		}
		if shape == "s-probe" {
			var cites []string
			for _, occ := range s.Occurrences[:flywheel.RecurringAfter] {
				cites = append(cites, strconv.Quote(occ.Cite()))
			}
			shape, occurrences = s.ID, "["+strings.Join(cites, ", ")+"]"
			// A passed repair is cited, as the proposal must; an open
			// one leaves the probe refused, as the proposal is.
			if _, passed := flywheel.Repairs(ctx.Records, ctx.Lifecycle, s.ID); len(passed) > 0 {
				repair = passed[0].Cite()
			}
		}
	}
	return
}

// planDigestKV is the plan verbs' content digest, a seed/4 field
// (plans/os-6bd9ffff.md D5): required there and refused before it,
// so the probe carries it exactly at the versions whose shape rule
// demands it.
func (v *probeView) planDigestKV() string {
	if !version.LevelsApply(v.version) {
		return ""
	}
	return `"digest": "` + strings.Repeat("0", 64) + `", `
}

// probePacket is the minimal shape-valid four-part packet: non-empty
// acceptance, marked decisions (none), the zero-length base range,
// and honestly empty refs and findings (spec/packets.md).
const probePacket = `{"acceptance": ["probe"], "decisions": [], "base": "0000000000000000000000000000000000000000..0000000000000000000000000000000000000000", "refs": [], "findings": []}`

// probeClaim is the claim the hypothesis probe proposes; its subject is
// the id the claim derives, so the probe is signed where the rule
// looks. The two defaults are what the probes cite when the record
// holds nothing citable: shape-valid, and refused.
const (
	probeClaim      = "probe"
	probeSupport    = `["probe@0", "probe@0"]`
	probeHypothesis = "h-000000000000@0"
	probeContest    = `["probe@0"]`
	probeLesson     = curation.LessonsDir + "/probe.md @ 0000000000000000000000000000000000000000"
	probeDeadEnd    = "probe@0"
	probeEnv        = "probe"
)

func (v *probeView) retireLessonOr() string {
	if v.retireLesson == "" {
		return probeLesson
	}
	return v.retireLesson
}

func (v *probeView) retireHypothesisOr() string {
	if v.retireHypothesis == "" {
		return probeHypothesis
	}
	return v.retireHypothesis
}

func (v *probeView) deadEndOr() string {
	if v.deadEnd == "" {
		return probeDeadEnd
	}
	return v.deadEnd
}

func (v *probeView) retiredDeadEndOr() string {
	if v.retiredDeadEnd == "" {
		return probeDeadEnd
	}
	return v.retiredDeadEnd
}

func (v *probeView) deadEndEnvironmentOr() string {
	if v.deadEndEnvironment == "" {
		return probeEnv
	}
	return v.deadEndEnvironment
}

func (v *probeView) retiredEnvironmentOr() string {
	if v.retiredEnvironment == "" {
		return probeEnv
	}
	return v.retiredEnvironment
}

// movedFrom names an environment that differs from the one standing:
// the dead-end probes ask "could this dead end retire (or come back)
// here", which the rule admits only in an environment other than the
// one the previous act named.
func movedFrom(environment string) string {
	if environment != probeEnv {
		return probeEnv
	}
	return probeEnv + "-moved"
}

func (v *probeView) contestOr() string {
	if v.contest == "" {
		return probeContest
	}
	return v.contest
}

func (v *probeView) supportOr() string {
	if v.support == "" {
		return probeSupport
	}
	return v.support
}

func (v *probeView) hypothesisOr() string {
	if v.hypothesis == "" {
		return probeHypothesis
	}
	return v.hypothesis
}

// The flywheel probes' fallbacks: a view built without a context (the
// catalog completeness drill renders every probe on an empty view)
// still yields a shape-valid, refused probe.
func (v *probeView) flywheelShapeOr() string {
	if v.flywheelShape == "" {
		return "s-probe"
	}
	return v.flywheelShape
}

func (v *probeView) flywheelOccurrencesOr() string {
	if v.flywheelOccurrences == "" {
		return "[]"
	}
	return v.flywheelOccurrences
}

func (v *probeView) flywheelStandingOr() string {
	if v.flywheelStanding == "" {
		return "s-probe"
	}
	return v.flywheelStanding
}

func (v *probeView) flywheelPathOr() string {
	if v.flywheelPath == "" {
		return flywheel.RegistryDir + "/probe.yaml"
	}
	return v.flywheelPath
}

// flywheelRepairKV is the proposal probe's repair citation, present
// exactly when a passed repair stands for the probed shape.
func (v *probeView) flywheelRepairKV() string {
	if v.flywheelRepair == "" {
		return ""
	}
	return `, "repair": ` + strconv.Quote(v.flywheelRepair)
}

// curationProbes derives the citations the curation probes make: two
// admitted observations on two distinct non-failed contracts (the
// support a proposal needs), the latest admitted hypothesis (the
// citation a promotion needs), the first standing promotion with no
// standing retirement (the one a retirement revokes), and the queried
// subject's first unretired and first retired dead ends (the citations
// the dead-end acts make). Where the record holds none the probe cites
// what the rules refuse, so the affordance is invisible exactly when
// the act is not yet legal.
func curationProbes(ctx *Context, subject string, v *probeView) {
	v.support, v.hypothesis, v.contest = probeSupport, probeHypothesis, probeContest
	v.retireLesson, v.retireHypothesis = probeLesson, probeHypothesis
	v.deadEnd, v.deadEndEnvironment = probeDeadEnd, probeEnv
	v.retiredDeadEnd, v.retiredEnvironment = probeDeadEnd, probeEnv
	if ctx == nil || ctx.Table == nil || ctx.Lifecycle == nil {
		return
	}
	byContract := map[string]string{}
	var cited []string
	for pos, rec := range ctx.Records {
		e := &rec.Event
		if _, ok := byContract[e.Subject]; ok || len(cited) >= curation.SupportMinimum {
			continue
		}
		cit := curation.Citation{Contract: e.Subject, Position: pos}
		if _, ok := curation.ObservationAt(ctx.Records, ctx.Table, cit); !ok || curation.FailedAt(ctx.Records, ctx.Table, e.Subject) {
			continue
		}
		byContract[e.Subject] = fmt.Sprintf("%s@%d", e.Subject, pos)
		cited = append(cited, fmt.Sprintf("%q", byContract[e.Subject]))
	}
	if len(cited) >= curation.SupportMinimum {
		v.support = "[" + strings.Join(cited, ", ") + "]"
	}
	fold := curation.Fold(ctx.Records)
	for _, id := range fold.HypothesisIDs() {
		h, _ := fold.Hypothesis(id)
		if _, ok := curation.HypothesisValid(ctx.Records, ctx.Table, curation.Citation{Contract: id, Position: h.Pos}); ok {
			v.hypothesis = fmt.Sprintf("%s@%d", id, h.Pos)
			// The contest probe cites one held-out observation on a
			// selected contract, where the record holds one.
			if held := curation.HeldOut(ctx.Records, ctx.Table, ctx.Lifecycle, h); len(held) > 0 {
				v.contest = fmt.Sprintf(`["%s@%d"]`, held[0].Contract, held[0].Position)
			}
		}
	}
	paths := make([]string, 0, len(fold.Lessons))
	for path := range fold.Lessons {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	// The queried subject's own standing promotion first, where it
	// is a hypothesis with one; else the first unretired path.
	for _, own := range []bool{true, false} {
		for _, path := range paths {
			l := fold.Lessons[path]
			if c, _ := curation.ParseCitation(l.Hypothesis); fold.RetiredPath(path) || (own && c.Contract != subject) {
				continue
			}
			v.retireLesson, v.retireHypothesis = l.Lesson, l.Hypothesis
			break
		}
		if v.retireLesson != probeLesson {
			break
		}
	}
	unretired, retired := false, false
	for _, d := range fold.DeadEnds[subject] {
		switch {
		case !d.Retired && !unretired:
			v.deadEnd, v.deadEndEnvironment = fmt.Sprintf("%s@%d", subject, d.Pos), movedFrom(d.Environment)
			unretired = true
		case d.Retired && !retired:
			v.retiredDeadEnd, v.retiredEnvironment = fmt.Sprintf("%s@%d", subject, d.Pos), movedFrom(d.RetiredEnvironment)
			retired = true
		}
	}
}

// probeEscalation is the minimal shape-valid question: one sentence
// and the two-option floor a minimal decision needs
// (spec/escalation.md).
const probeEscalation = `{"question": "probe?", "options": [{"id": "a", "choice": "probe a"}, {"id": "b", "choice": "probe b"}]}`

// probeTuple is the configuration the run.started probe declares at
// seed/2. A probe asks "could a start be admitted here"; a holder whose
// qualified grants cite something else would refuse it as drift, which
// is the honest answer to that question for that holder.
const probeTuple = `{"principal": "probe", "harness": "probe/0", "model": "probe/0", "tool_policy": "probe", "environment": "probe"}`

// affordanceCatalog is every verb the envelope can list, each with
// its payload synthesizer. Completeness is pinned by test: a catalog
// verb without a synthesizer, or a spec-table verb missing from the
// catalog, is a test failure — absence from an envelope always means
// the probe was REFUSED at this position, never that synthesis was
// skipped. The one named carve-out is actor.enrolled, whose valid
// payload requires the queried subject's public key: fingerprints
// are hashes, so no prober can derive it, and the synthesizer's
// ephemeral key is refused by the keyring's subject binding on any
// subject whose key the caller does not hold (recorded decision;
// the enrollment surface knows its key out of band).
// probeSubjects names, for the verbs that live on a derived subject,
// the subject the probe is signed on instead of the caller's: the
// curation facts (a hypothesis on the id its claim derives, a
// promotion on the hypothesis it cites) are legal on no contract.
var probeSubjects = map[string]func(v *probeView) string{
	flywheel.ProposedVerb:          func(v *probeView) string { return v.flywheelShapeOr() },
	flywheel.MergedVerb:            func(v *probeView) string { return v.flywheelStandingOr() },
	"curation.hypothesis.proposed": func(v *probeView) string { return curation.HypothesisID(probeClaim, nil) },
	"curation.hypothesis.contested": func(v *probeView) string {
		h, _ := curation.ParseCitation(v.hypothesisOr())
		return h.Contract
	},
	"curation.lesson.promoted": func(v *probeView) string {
		h, _ := curation.ParseCitation(v.hypothesisOr())
		return h.Contract
	},
	"curation.lesson.retired": func(v *probeView) string {
		h, _ := curation.ParseCitation(v.retireHypothesisOr())
		return h.Contract
	},
}

// topologyProbes derives the relation probes' targets
// (plans/os-f0ae2cdf.md): the first other open contract in the fold's
// order for a link or a parent, and the queried subject's first active
// dependency for an unlink, both read through the graph that passed
// the boundary, so the verbs are drafted exactly where the rule would
// admit them.
func topologyProbes(ctx *Context, subject string, v *probeView) {
	if ctx.Lifecycle == nil || ctx.Table == nil {
		return
	}
	for _, other := range ctx.Lifecycle.Subjects() {
		if other == subject {
			continue
		}
		if s, ok := ctx.Lifecycle.State(other); ok && s.State != "" && !ctx.Table.Terminal(s.State) {
			v.relative = other
			break
		}
	}
	if links := Topology(ctx).Graph.Requires(subject); len(links) > 0 {
		v.unlink = links[0].Target
	}
}

var affordanceCatalog = []struct {
	verb  string
	synth func(v *probeView) string
}{
	{"system.halt.declared", func(v *probeView) string { return `{"reason": "probe"}` }},
	{"system.halt.lifted", func(v *probeView) string { return `{}` }},
	{"system.protocol.upgraded", func(v *probeView) string { return `{"to": "seed/1"}` }},
	{"system.imported", func(v *probeView) string {
		// A shape-valid provenance record (spec/import.md): the
		// source, a full export commit, an anchor tag and a manifest
		// digest. Afforded only to an operator on a seed/5 chain that
		// carries no import yet, which is what the rule judges.
		return `{"source": "open-seed", "export_head": "` + strings.Repeat("0", 40) + `", "anchor": "seed-anchor/probe", "manifest": "` + strings.Repeat("0", 64) + `"}`
	}},
	{"request.filed", func(v *probeView) string {
		// A shape-valid inbound proposal (spec/requests.md): a
		// surface name, a kind, an anchored reference and a bounded
		// summary, about the queried contract when it is one. Afforded
		// to any standing key on a seed/7 chain, which is what the
		// rule and the keyring judge.
		about := ""
		if v.requestAbout != "" {
			about = `, "about": "` + v.requestAbout + `"`
		}
		return `{"origin": "probe", "kind": "dashboard-action", "reference": "probe @ ` + strings.Repeat("0", 7) + `", "summary": "probe"` + about + `}`
	}},
	{topology.DependVerb, func(v *probeView) string {
		return `{"requires": "` + v.relative + `"}`
	}},
	{topology.UndependVerb, func(v *probeView) string {
		return `{"requires": "` + v.unlink + `"}`
	}},
	{topology.ParentVerb, func(v *probeView) string {
		return `{"parent": "` + v.relative + `"}`
	}},
	{topology.AlignVerb, func(v *probeView) string {
		return `{"mission": "probe/mission.md @ ` + strings.Repeat("0", 7) + `"}`
	}},
	{"artifact.erased", func(v *probeView) string {
		return `{"artifact": "` + v.erasable + `", "reason": "probe"}`
	}},
	{"request.answered", func(v *probeView) string {
		// The dispatcher's close of the first unanswered request on
		// the queried subject, declined with a reason: filed would
		// need an intent the probe cannot have appended.
		return `{"request": "` + v.request + `", "outcome": "declined", "reason": "probe"}`
	}},
	{"approval.requested", func(v *probeView) string {
		// A per-verb approval request (plans/os-5781a026.md D7):
		// the probing key as the actor and claim.taken as the verb,
		// a catalog verb every deployment has. Afforded to any
		// standing key on a subject the chain knows or on system,
		// which is what the rule and the keyring judge.
		return `{"verb": "claim.taken", "actor": "` + v.actor + `", "reason": "probe"}`
	}},
	{"approval.granted", func(v *probeView) string {
		// The operator's grant of the oldest open request on the
		// queried subject; a zero position when none stands, which
		// the rule refuses.
		return `{"request": "` + v.approval + `"}`
	}},
	{"approval.denied", func(v *probeView) string {
		return `{"request": "` + v.approval + `", "reason": "probe"}`
	}},
	{"system.checkpoint", func(v *probeView) string {
		// A shape-valid snapshot citation (spec/maintenance.md):
		// the versioned format, a well-formed digest, a fetchable
		// location, and this view's own position. The old `{"n": 1}`
		// stopped being admissible when the checkpoint rule landed,
		// and a synthesizer that no longer matches the rule makes a
		// LEGAL act invisible in the orientation read — the #200
		// failure, which is why the catalog is swept by test.
		return `{"format": "` + checkpoint.Format + `", "snapshot": "` +
			strings.Repeat("0", 64) + `", "location": "` + checkpoint.Location +
			`", "position": "` + v.position + `"}`
	}},
	{"actor.enrolled", func(v *probeView) string {
		pub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			return `{}`
		}
		return fmt.Sprintf(`{"key": "%x", "kind": "agent", "name": "probe"}`, []byte(pub))
	}},
	{"actor.granted", func(v *probeView) string { return `{"capability": "claim"}` }},
	{"actor.qualified", func(v *probeView) string {
		if v.qualify == "" {
			return probeQualify
		}
		return v.qualify
	}},
	{"actor.disqualified", func(v *probeView) string {
		if v.disqualify == "" {
			return probeDisqualify
		}
		return v.disqualify
	}},
	{"actor.suspended", func(v *probeView) string { return `{"reason": "probe"}` }},
	{"actor.revoked", func(v *probeView) string { return `{"reason": "probe"}` }},
	{"intent.filed", func(v *probeView) string {
		return `{"intent": "probe", "tier": "trivial", "budget": "small", "routing": "core"}`
	}},
	{"contract.specified", func(v *probeView) string {
		return `{"acceptance": {"ref": "accept.md @ 0000000000000000000000000000000000000000", "executable": true, "gate": "probe @ 0000000000000000000000000000000000000000"}}`
	}},
	{"contract.blocked", func(v *probeView) string { return `{}` }},
	{"contract.unblocked", func(v *probeView) string { return `{}` }},
	{"contract.cancelled", func(v *probeView) string {
		// Once a question stands, cancelling is legal only WITH the
		// citation, and it is one of the two documented ways to answer
		// the gate — so a bare {} would hide it precisely when it
		// matters (review finding on #200).
		if v.standing {
			return `{"escalation": "` + v.escalation + `"}`
		}
		return `{}`
	}},
	{"escalation.raised", func(v *probeView) string {
		return `{"packet": ` + v.packet + `, "escalation": ` + probeEscalation + `}`
	}},
	{"decision.recorded", func(v *probeView) string {
		return `{"escalation": "` + v.escalation + `", "choice": "` + v.choice + `"}`
	}},
	{"contract.returned", func(v *probeView) string {
		// The return cites what stands (plans/os-0cd18799.md D4): a
		// red observation where one stands on the head, else the
		// verdict. Either is one legal citation; the rule refuses the
		// other by name.
		if v.redObservation != "" {
			return `{"observation": "` + v.redObservation + `"}`
		}
		return `{"verdict": "` + v.verdict + `"}`
	}},
	{"check.observed", func(v *probeView) string {
		// The forge observation probe (plans/os-0cd18799.md D1) speaks
		// the head under review and a check state that DIFFERS from
		// the standing observation's, since an unchanged observation
		// refuses by design and the probe asks whether a new fact
		// would admit.
		return `{"pr": "` + v.observationPR + `", "head": "` + v.observationHead + `", "checks": "` + v.observationChecks + `", "review": "none"}`
	}},
	{"claim.taken", func(v *probeView) string { return `{}` }},
	{"claim.released", func(v *probeView) string {
		return `{"fence": "` + v.fence + `", "packet": ` + v.packet + `}`
	}},
	{"claim.parked", func(v *probeView) string {
		return `{"fence": "` + v.fence + `", "packet": ` + v.packet + `}`
	}},
	{"claim.reaped", func(v *probeView) string {
		return `{"fence": "` + v.fence + `", "packet": ` + v.packet + `}`
	}},
	{"submission.made", func(v *probeView) string {
		return `{"fence": "` + v.fence + `", "packet": ` + v.packet + `}`
	}},
	{"curation.deadend.recorded", func(v *probeView) string {
		return `{"fence": "` + v.fence + `", "tried": "probe", "outcome": "probe", "condition": "probe", "environment": "probe"}`
	}},
	{flywheel.ProposedVerb, func(v *probeView) string {
		return `{"shape": "` + v.flywheelShapeOr() + `", "workflow": "` + flywheel.RegistryDir + `/probe.yaml @ 0000000000000000000000000000000000000000", "occurrences": ` + v.flywheelOccurrencesOr() + `, "validated": {"run": "wf-probe"}` + v.flywheelRepairKV() + `}`
	}},
	{flywheel.MergedVerb, func(v *probeView) string {
		return `{"workflow": "` + v.flywheelPathOr() + ` @ 0000000000000000000000000000000000000000", "shape": "` + v.flywheelStandingOr() + `", "pr": "pr/0 @ 0000000000000000000000000000000000000000"}`
	}},
	{"curation.hypothesis.proposed", func(v *probeView) string {
		return `{"claim": "` + probeClaim + `", "applies_when": {"routing": "probe"}, "support": ` + v.supportOr() + `, "exceptions": [], "provenance": []}`
	}},
	{"curation.hypothesis.contested", func(v *probeView) string {
		return `{"hypothesis": "` + v.hypothesisOr() + `", "evidence": ` + v.contestOr() + `, "reason": "probe"}`
	}},
	{"curation.lesson.promoted", func(v *probeView) string {
		return `{"lesson": "` + curation.LessonsDir + `/probe.md @ 0000000000000000000000000000000000000000", "hypothesis": "` + v.hypothesisOr() + `", "pr": "pr/0 @ 0000000000000000000000000000000000000000", "carrier": "knowledge", "adversarial": {"eval": "probe", "verdict": "0"}, "last_validated": "2026-01-01T00:00:00Z", "expires": "2026-02-01T00:00:00Z", "digest": "` + strings.Repeat("0", 64) + `"}`
	}},
	// The retirement probe gives the reason the record alone can
	// judge: expired carries no field beyond the citation, and the
	// rule admits it wherever an unretired promotion stands. The
	// dead-end acts are facts on the queried subject, citing its own
	// dead ends in an environment other than the standing one.
	{"curation.lesson.retired", func(v *probeView) string {
		return `{"lesson": "` + v.retireLessonOr() + `", "hypothesis": "` + v.retireHypothesisOr() + `", "reason": "expired"}`
	}},
	{"curation.deadend.retired", func(v *probeView) string {
		return `{"deadend": "` + v.deadEndOr() + `", "environment": "` + v.deadEndEnvironmentOr() + `", "reason": "probe"}`
	}},
	{"curation.deadend.unretired", func(v *probeView) string {
		return `{"deadend": "` + v.retiredDeadEndOr() + `", "environment": "` + v.retiredEnvironmentOr() + `", "reason": "probe"}`
	}},
	{"plan.proposed", func(v *probeView) string {
		return `{` + v.fenceKV() + v.planDigestKV() + `"plan": "probe.md @ 0000000000000000000000000000000000000000"}`
	}},
	{"plan.approved", func(v *probeView) string {
		return `{` + v.fenceKV() + v.planDigestKV() + `"plan": "probe.md @ 0000000000000000000000000000000000000000", "pr": "pr/0 @ 0000000000000000000000000000000000000000"}`
	}},
	{"progress.milestone", func(v *probeView) string {
		return `{` + v.fenceKV() + `"count": 1, "step": "probe"}`
	}},
	{"wedge.declared", func(v *probeView) string {
		return `{` + v.fenceKV() + `"observed": "` + v.now + `", "count": 0, "since": "` + v.now + `"}`
	}},
	{"message.sent", func(v *probeView) string { return `{` + v.fenceKV() + `"n": 1}` }},
	{"offer.published", func(v *probeView) string {
		return `{"eligibility": {"capabilities": ["claim"], "tiers": []}, "expires": "` + v.expires + `"}`
	}},
	// The budget facts cite the fence CONDITIONALLY
	// (plans/os-d6963652.md D5): a reservation outlives its claim
	// window and closes wherever it stands, so an unconditional
	// citation would have the fence rule refuse the probe outside a
	// window and hide a legal close. Reserve joins them although its
	// own rule still refuses it there: refused by the right rule, so
	// the envelope answers "why not" honestly.
	{"budget.reserve", func(v *probeView) string {
		return `{` + v.fenceKV() + `"amount": "1"}`
	}},
	{"budget.settle", func(v *probeView) string {
		return `{` + v.fenceKV() + `"reservation": "` + v.reservation + `", "actuals": "1"}`
	}},
	{"budget.release", func(v *probeView) string {
		return `{` + v.fenceKV() + `"reservation": "` + v.reservation + `"}`
	}},
	{"run.started", func(v *probeView) string {
		if tuple.Applies(v.version) {
			return `{"fence": "` + v.fence + `", "reservation": "` + v.reservation + `", "tuple": ` + probeTuple + `}`
		}
		return `{"fence": "` + v.fence + `", "reservation": "` + v.reservation + `"}`
	}},
	{"run.settled", func(v *probeView) string {
		return `{"fence": "` + v.fence + `", "units": "0", "lines": "0"}`
	}},
	{"run.interrupted", func(v *probeView) string { return `{"fence": "` + v.fence + `"}` }},
	{"verdict.rendered", func(v *probeView) string {
		return `{"verdict": "pass", "receipt": "0000000000000000000000000000000000000000000000000000000000000000", "submission": "` + v.submission + `", "independence": "L1"}`
	}},
	// The deferral (plans/os-2e34f66a.md D4): a scorecard citation and
	// one deferred item, on the bound submission; refused before
	// seed/4 by version, and by the deferral rule where none is legal.
	{"verdict.deferred", func(v *probeView) string {
		return `{"scorecard": "0000000000000000000000000000000000000000000000000000000000000000", "submission": "` + v.submission + `", "items": ["probe"]}`
	}},
	{"check.sealed", func(v *probeView) string {
		return `{"commitment": "0000000000000000000000000000000000000000000000000000000000000000"}`
	}},
	{"merge.requested", func(v *probeView) string { return `{"verdict": "` + v.verdict + `"}` }},
	{"merge.observed", func(v *probeView) string {
		return `{"merged": "0000000000000000000000000000000000000000", "pr": "probe"}`
	}},
	{"merge.overridden", func(v *probeView) string {
		return `{"reason": "probe", "verdict": "` + v.verdict + `"}`
	}},
}

// Affordances lists the verbs currently legal for the signing key's
// actor on the subject, at the context's position. Each catalog verb
// is drafted with its synthesized payload, signed with the caller's
// key (the pipeline's actor rule verifies record signatures, so a
// fingerprint alone cannot probe), and run through the full Check;
// the verb is listed iff its probe admits. The result is sorted and
// deduplicated for schema stability. Errors never escape: a context
// that cannot be probed yields the empty list, because affordances
// are advisory and must not break the verb that carries them.
func Affordances(ctx *Context, key ed25519.PrivateKey, subject string) []string {
	if ctx == nil || len(key) != ed25519.PrivateKeySize || subject == "" {
		return []string{}
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		return []string{}
	}
	now := time.Now().UTC()
	v := &probeView{
		now:         now.Format(time.RFC3339),
		expires:     now.Add(time.Hour).Format(time.RFC3339),
		fence:       "0",
		reservation: "0",
		submission:  "0",
		verdict:     "0",
		packet:      probePacket,
		escalation:  "0",
		position:    fmt.Sprintf("%d", ctx.Count),
		request:     "0",
		actor:       fp,
		approval:    "0",
		erasable:    strings.Repeat("0", 64),
		relative:    subject,
		unlink:      subject,
		// A head the rule refuses where no submission stands: a
		// well-formed citation, so illegality is the rule set's.
		observationHead:   strings.Repeat("0", 40),
		observationChecks: "red",
		observationPR:     "probe",
	}
	v.version = ctx.Active
	v.qualify, v.disqualify = qualificationProbes(ctx, subject)
	topologyProbes(ctx, subject, v)
	curationProbes(ctx, subject, v)
	v.flywheelShape, v.flywheelOccurrences, v.flywheelStanding, v.flywheelPath, v.flywheelRepair = flywheelProbes(ctx)
	if ctx.Lifecycle != nil {
		for _, r := range ctx.Lifecycle.Requests() {
			if r.Subject == subject && r.Answered == nil {
				v.request = fmt.Sprintf("%d", r.Pos)
				break
			}
		}
		if pending := ctx.Lifecycle.PendingApprovals(subject); len(pending) > 0 {
			v.approval = fmt.Sprintf("%d", pending[0].Pos)
		}
		if s, ok := ctx.Lifecycle.State(subject); ok {
			v.requestAbout = subject
			// The first reference not yet erased, so the verb is
			// drafted exactly while something remains erasable and
			// never for a tombstoned digest (plans/os-db5cd353.md D6).
			candidates := []string{}
			if s.Sealed != nil {
				candidates = append(candidates, s.Sealed.Commitment)
			}
			for _, vf := range s.Verdicts {
				if vf.Receipt != "" {
					candidates = append(candidates, vf.Receipt)
				}
			}
			for _, d := range candidates {
				if _, erased := Erasure(ctx.Records, ctx.Lifecycle, d); !erased {
					v.erasable = d
					break
				}
			}
			if s.Claim != nil {
				v.fence = fmt.Sprintf("%d", s.Claim.Fence)
				v.active = true
			}
			if s.Submission != nil {
				v.submission = fmt.Sprintf("%d", s.Submission.Pos)
				if head, ok := submissionHead(ctx, subject, s); ok {
					v.observationHead = head
				}
				if s.Submission.PR != "" {
					v.observationPR = s.Submission.PR
				}
			}
			v.observationChecks = "red"
			if s.Observation != nil {
				if s.Observation.Checks == "red" {
					v.observationChecks = "green"
				}
				if s.Observation.Red() && s.Observation.Head == v.observationHead {
					v.redObservation = fmt.Sprintf("%d", s.Observation.Pos)
				}
			}
			if s.Verdict != nil {
				v.verdict = fmt.Sprintf("%d", s.Verdict.Pos)
			}
			if s.Escalation != nil {
				v.escalation = fmt.Sprintf("%d", s.Escalation.Pos)
				v.standing = true
				if len(s.Escalation.Options) > 0 {
					v.choice = s.Escalation.Options[0].ID
				}
			}
			view := BudgetViewAt(ctx.Records, ctx.Table, subject, s)
			if len(view.Open) > 0 {
				v.reservation = fmt.Sprintf("%d", view.Open[0].Pos)
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range affordanceCatalog {
		on := subject
		if derive, ok := probeSubjects[p.verb]; ok {
			if derived := derive(v); derived != "" {
				on = derived
			}
		}
		rec, err := event.Sign(event.Event{
			V: ctx.Active, TS: v.now, Actor: fp, Verb: p.verb, Subject: on,
			Payload: []byte(p.synth(v)), Prev: ctx.Tip,
		}, key)
		if err != nil {
			continue
		}
		if Check(ctx, rec) == nil && !seen[p.verb] {
			seen[p.verb] = true
			out = append(out, p.verb)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// BudgetBlock derives the envelope's {reserved, remaining} strings
// for the subject at the context's position, from the one shared
// budget derivation. ok is false on subjects carrying no budget
// facts, where the envelope keeps its null block.
func BudgetBlock(ctx *Context, subject string) (reserved, remaining string, ok bool) {
	if ctx == nil || ctx.Lifecycle == nil {
		return "", "", false
	}
	s, found := ctx.Lifecycle.State(subject)
	if !found || (len(s.Reservations) == 0 && len(s.BudgetCloses) == 0) {
		return "", "", false
	}
	view := BudgetViewAt(ctx.Records, ctx.Table, subject, s)
	open := 0
	for _, r := range view.Open {
		// Saturating, mirroring the budget arithmetic: presentation
		// must never wrap where the derivation would not.
		if open > int(^uint(0)>>1)-r.Amount {
			open = int(^uint(0) >> 1)
		} else {
			open += r.Amount
		}
	}
	return fmt.Sprintf("%d", open), fmt.Sprintf("%d", view.Remaining), true
}
