package admit

// The affordance drills (plans/os-f5551001.md; charter III.I rows 1
// and 2): the catalog is complete against the spec verb table, the
// lifecycle walk lists every catalog verb somewhere legal (the
// completeness half) and never lists the curated illegal set (the
// soundness half), and the output is sorted and deterministic. The
// listed-iff-admits property is the computation itself — every probe
// runs the enforcing Check — so these drills pin the corpus item 2's
// regression-class harness generalizes.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// specCatalogVerbs mirrors the actors.md spec table plus the
// ungoverned active-standing verb, the same literal-list pinning the
// keyring completeness test uses: drift between this list, the
// catalog, and the spec table is a test failure.
var specCatalogVerbs = []string{
	"system.halt.declared", "system.halt.lifted", "system.protocol.upgraded",
	"system.checkpoint", "system.imported",
	"actor.enrolled", "actor.granted", "actor.suspended", "actor.revoked",
	"actor.qualified", "actor.disqualified",
	"intent.filed", "contract.specified", "contract.blocked",
	"contract.unblocked", "contract.cancelled", "contract.returned",
	"escalation.raised", "decision.recorded",
	"claim.taken", "claim.released", "claim.parked", "claim.reaped",
	"submission.made", "plan.proposed", "plan.approved",
	"progress.milestone", "wedge.declared",
	"offer.published", "budget.reserve", "budget.settle", "budget.release",
	"run.started", "run.settled", "run.interrupted",
	"verdict.rendered", "verdict.deferred", "check.sealed",
	"merge.requested", "merge.observed", "merge.overridden", "check.observed",
	"message.sent", "request.filed", "request.answered", "artifact.erased",
	"dependency.linked", "dependency.unlinked", "hierarchy.parented", "goal.aligned",
	"approval.requested", "approval.granted", "approval.denied",
	"curation.deadend.recorded", "curation.hypothesis.proposed", "curation.hypothesis.contested", "curation.lesson.promoted",
	"curation.lesson.retired", "curation.deadend.retired", "curation.deadend.unretired",
	"workflow.proposed", "workflow.merged",
}

func TestAffordanceCatalogCompleteness(t *testing.T) {
	inCatalog := map[string]bool{}
	for _, p := range affordanceCatalog {
		if p.synth == nil {
			t.Errorf("%s has no payload synthesizer", p.verb)
			continue
		}
		v := &probeView{now: "2026-09-01T00:00:00Z", expires: "2026-09-01T01:00:00Z",
			fence: "0", reservation: "0", submission: "0", verdict: "0", packet: probePacket}
		if body := p.synth(v); !json.Valid([]byte(body)) {
			t.Errorf("%s synthesizes invalid JSON: %s", p.verb, body)
		}
		inCatalog[p.verb] = true
	}
	for _, verb := range specCatalogVerbs {
		if !inCatalog[verb] {
			t.Errorf("%s is in the spec table but missing from the catalog", verb)
		}
	}
	if len(inCatalog) != len(specCatalogVerbs) {
		t.Errorf("catalog holds %d verbs, the spec list %d — an extra catalog verb needs a spec row", len(inCatalog), len(specCatalogVerbs))
	}
}

// walkStep is one append of the shared scenario script
// (plans/os-148d3ba1.md D2): the signing lane, the draft version,
// the verb, the subject, and the payload synthesized from the
// context standing at the step's position. station labels the
// position after the append for the walk's curated assertions; the
// regression-class sweep ignores it and checks every position.
type walkStep struct {
	lane    string
	v       string
	verb    string
	subject string
	payload func(t *testing.T, ctx *Context) string
	station string
}

// walkLanes returns the five capability lanes the scenario enrolls,
// keyed by name, on deterministic fixture keys.
func walkLanes(t *testing.T) map[string]ed25519.PrivateKey {
	return map[string]ed25519.PrivateKey{
		"holder":     fixtureKey(t, 2),
		"supervisor": fixtureKey(t, 3),
		"verifier":   fixtureKey(t, 4),
		"sealer":     fixtureKey(t, 5),
		"observer":   fixtureKey(t, 6),
	}
}

// walkResolver resolves the lane keys ahead of the keyring (the
// scenario enrolls them mid-script) and falls back to the genesis
// resolver for the root.
func walkResolver(t *testing.T, resolve ledger.Resolver, lanes map[string]ed25519.PrivateKey) ledger.Resolver {
	return func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range lanes {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
}

// walkScript is the one scenario history both the walk and the
// regression-class sweep replay: upgrade, enrollment, then birth,
// claim, spend, run, review, verdicts pass and fail, block, and
// halt — exactly the chain the walk drove before the extraction,
// with the enrollment order fixed for replay stability.
func walkScript(t *testing.T, lanes map[string]ed25519.PrivateKey) []walkStep {
	static := func(body string) func(*testing.T, *Context) string {
		return func(*testing.T, *Context) string { return body }
	}
	steps := []walkStep{
		{"root", "seed/0", ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed1 + `"}`), ""},
	}
	caps := map[string]string{
		"holder": keyring.CapClaim, "supervisor": keyring.CapSupervise,
		"verifier": keyring.CapVerdict, "sealer": keyring.CapSealer,
		"observer": keyring.CapObserver,
	}
	for _, lane := range []string{"holder", "supervisor", "verifier", "sealer", "observer"} {
		steps = append(steps,
			walkStep{"root", version.Seed1, keyring.VerbEnrolled, fpOf(t, lanes[lane]), static(enrollBody(t, lanes[lane], "agent", lane)), ""},
			walkStep{"root", version.Seed1, keyring.VerbGranted, fpOf(t, lanes[lane]), static(`{"capability": "` + caps[lane] + `"}`), ""},
		)
	}
	steps[len(steps)-1].station = "enrolled"
	return append(steps,
		walkStep{"root", version.Seed1, "intent.filed", "c-1", static(filedBody), "filed-c1"},
		walkStep{"root", version.Seed1, "contract.specified", "c-1", static(specBody), "ready-c1"},
		walkStep{"holder", version.Seed1, "claim.taken", "c-1", static(`{}`), "claimed-c1"},
		walkStep{"holder", version.Seed1, "budget.reserve", "c-1", func(t *testing.T, ctx *Context) string {
			return reserveBody("2", fenceOf(t, ctx, "c-1"))
		}, "reserved-c1"},
		walkStep{"supervisor", version.Seed1, "run.started", "c-1", func(t *testing.T, ctx *Context) string {
			return `{"fence": "` + fenceOf(t, ctx, "c-1") + `", "reservation": "` + reservationOf(t, ctx, "c-1") + `"}`
		}, "running-c1"},
		walkStep{"holder", version.Seed1, "submission.made", "c-1", func(t *testing.T, ctx *Context) string {
			return `{"fence": "` + fenceOf(t, ctx, "c-1") + `", "packet": ` + minPacket + `}`
		}, "review-c1"},
		walkStep{"verifier", version.Seed1, "verdict.rendered", "c-1", func(t *testing.T, ctx *Context) string {
			return `{"verdict": "pass", "receipt": "` + zeros64 + `", "submission": "` + submissionOf(t, ctx, "c-1") + `", "independence": "L1"}`
		}, "passed-c1"},
		walkStep{"root", version.Seed1, "merge.requested", "c-1", func(t *testing.T, ctx *Context) string {
			return `{"verdict": "` + verdictOf(t, ctx, "c-1") + `"}`
		}, "merged-c1"},
		walkStep{"root", version.Seed1, "intent.filed", "c-2", static(filedBody), ""},
		walkStep{"root", version.Seed1, "contract.specified", "c-2", static(specBody), ""},
		walkStep{"holder", version.Seed1, "claim.taken", "c-2", static(`{}`), ""},
		walkStep{"holder", version.Seed1, "submission.made", "c-2", func(t *testing.T, ctx *Context) string {
			return `{"fence": "` + fenceOf(t, ctx, "c-2") + `", "packet": ` + minPacket + `}`
		}, "review-c2"},
		walkStep{"verifier", version.Seed1, "verdict.rendered", "c-2", func(t *testing.T, ctx *Context) string {
			return `{"verdict": "fail", "receipt": "` + zeros64 + `", "submission": "` + submissionOf(t, ctx, "c-2") + `", "independence": "L1"}`
		}, "failed-c2"},
		walkStep{"root", version.Seed1, "intent.filed", "c-3", static(filedBody), ""},
		walkStep{"root", version.Seed1, "contract.specified", "c-3", static(specBody), "ready-c3"},
		walkStep{"root", version.Seed1, "contract.blocked", "c-3", static(`{}`), "blocked-c3"},
		walkStep{"root", version.Seed1, "system.halt.declared", "system", static(`{"reason": "walk"}`), "halted"},
		walkStep{"root", version.Seed1, "system.halt.lifted", "system", static(`{}`), "lifted"},
		// Qualification (plans/os-03e47abb.md): the chain reaches
		// seed/3, an eval passes on the holder's window and the
		// supervisor may mint; a second eval fails under the same
		// configuration and the supervisor may disqualify. The
		// remaining steps carry seed/3.
		walkStep{"root", version.Seed1, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed2 + `"}`), ""},
		walkStep{"root", version.Seed2, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed3 + `"}`), ""},
		walkStep{"root", version.Seed3, "intent.filed", "c-4", static(evalFiledBody), ""},
		walkStep{"root", version.Seed3, "contract.specified", "c-4", static(evalSpecFor("walk")), ""},
		walkStep{"holder", version.Seed3, "claim.taken", "c-4", static(`{}`), ""},
		walkStep{"holder", version.Seed3, "budget.reserve", "c-4", func(t *testing.T, ctx *Context) string {
			return reserveBody("2", fenceOf(t, ctx, "c-4"))
		}, ""},
		walkStep{"supervisor", version.Seed3, "run.started", "c-4", func(t *testing.T, ctx *Context) string {
			return startBodyAt(fenceOf(t, ctx, "c-4"), reservationOf(t, ctx, "c-4"), tupleJSON(t, nil))
		}, ""},
		walkStep{"holder", version.Seed3, "submission.made", "c-4", func(t *testing.T, ctx *Context) string {
			return `{"fence": "` + fenceOf(t, ctx, "c-4") + `", "packet": ` + minPacket + `}`
		}, ""},
		walkStep{"verifier", version.Seed3, "verdict.rendered", "c-4", func(t *testing.T, ctx *Context) string {
			return `{"verdict": "pass", "receipt": "` + zeros64 + `", "submission": "` + submissionOf(t, ctx, "c-4") + `", "independence": "L1"}`
		}, "eval-passed-c4"},
		walkStep{"supervisor", version.Seed3, keyring.VerbQualified, fpOf(t, lanes["holder"]), func(t *testing.T, ctx *Context) string {
			return `{"capability": "claim", "tuple": ` + tupleJSON(t, nil) + `, "contract": "c-4", "verdict": "` + verdictOf(t, ctx, "c-4") + `"}`
		}, "qualified"},
		walkStep{"root", version.Seed3, "intent.filed", "c-5", static(evalFiledBody), ""},
		walkStep{"root", version.Seed3, "contract.specified", "c-5", static(evalSpecFor("walk")), ""},
		walkStep{"holder", version.Seed3, "claim.taken", "c-5", static(`{}`), ""},
		walkStep{"holder", version.Seed3, "budget.reserve", "c-5", func(t *testing.T, ctx *Context) string {
			return reserveBody("2", fenceOf(t, ctx, "c-5"))
		}, ""},
		walkStep{"supervisor", version.Seed3, "run.started", "c-5", func(t *testing.T, ctx *Context) string {
			return startBodyAt(fenceOf(t, ctx, "c-5"), reservationOf(t, ctx, "c-5"), tupleJSON(t, nil))
		}, ""},
		walkStep{"holder", version.Seed3, "submission.made", "c-5", func(t *testing.T, ctx *Context) string {
			return `{"fence": "` + fenceOf(t, ctx, "c-5") + `", "packet": ` + minPacket + `}`
		}, ""},
		walkStep{"verifier", version.Seed3, "verdict.rendered", "c-5", func(t *testing.T, ctx *Context) string {
			return `{"verdict": "fail", "receipt": "` + zeros64 + `", "submission": "` + submissionOf(t, ctx, "c-5") + `", "independence": "L1"}`
		}, "eval-failed-c5"},
		walkStep{"supervisor", version.Seed3, keyring.VerbDisqualified, fpOf(t, lanes["holder"]), func(t *testing.T, ctx *Context) string {
			return `{"capability": "claim", "tuple": ` + tupleJSON(t, nil) + `, "contract": "c-5", "verdict": "` + verdictOf(t, ctx, "c-5") + `", "reason": "the eval failed"}`
		}, "disqualified"},
		// Standing ends last, on the lane whose reservation on c-1 is
		// still open (plans/os-d6963652.md D6): a walk of only active
		// actors can never reach the positions where an obligation's
		// usual owner has lost the power to discharge it, and those
		// are exactly the positions the standing-aware attribution
		// exists for.
		walkStep{"root", version.Seed3, keyring.VerbSuspended, fpOf(t, lanes["holder"]), static(`{"reason": "walk"}`), "suspended"},
		walkStep{"root", version.Seed3, keyring.VerbRevoked, fpOf(t, lanes["holder"]), static(`{"reason": "walk"}`), "revoked"},
		// seed/5: the predecessor import's provenance record is
		// afforded to the operator once, on a chain that carries none,
		// and to nobody after it is written (spec/import.md).
		walkStep{"root", version.Seed3, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed4 + `"}`), ""},
		walkStep{"root", version.Seed4, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed5 + `"}`), "seed5"},
		walkStep{"root", version.Seed5, "system.imported", "system", static(`{"source": "open-seed", "export_head": "` + strings.Repeat("0", 40) + `", "anchor": "seed-anchor/walk", "manifest": "` + zeros64 + `"}`), "imported"},
		// seed/7: the request ingress is afforded to any standing key
		// on system and on a contract, and the answer to a dispatch
		// or operator key while a request is pending, to nobody after
		// (spec/requests.md).
		walkStep{"root", version.Seed5, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed6 + `"}`), ""},
		walkStep{"root", version.Seed6, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed7 + `"}`), "seed7"},
		walkStep{"root", version.Seed7, "request.filed", "system", static(`{"origin": "walk", "kind": "dashboard-action", "reference": "walk @ 0123456", "summary": "walk"}`), "requested"},
		walkStep{"root", version.Seed7, "request.answered", "system", func(t *testing.T, ctx *Context) string {
			for _, r := range ctx.Lifecycle.Requests() {
				if r.Answered == nil {
					return fmt.Sprintf(`{"request": "%d", "outcome": "declined", "reason": "walk"}`, r.Pos)
				}
			}
			t.Fatal("no pending request to answer")
			return ""
		}, "answered"},
		// The per-verb approval (plans/os-5781a026.md D7): the request
		// is afforded to any standing key on a known subject, the two
		// answers to the operator while one is pending and to nobody
		// after, and the pending request is the operator's obligation.
		walkStep{"supervisor", version.Seed7, "approval.requested", "system", func(t *testing.T, ctx *Context) string {
			return `{"verb": "offer.published", "actor": "` + fpOf(t, lanes["supervisor"]) + `", "reason": "walk"}`
		}, "approval-requested"},
		walkStep{"root", version.Seed7, "approval.granted", "system", func(t *testing.T, ctx *Context) string {
			if p := ctx.Lifecycle.PendingApprovals("system"); len(p) > 0 {
				return fmt.Sprintf(`{"request": "%d"}`, p[0].Pos)
			}
			t.Fatal("no pending approval to grant")
			return ""
		}, "approval-granted"},
		// seed/8: the forge observation (plans/os-0cd18799.md). The
		// operator claims and submits c-6 naming its pull request and
		// a full-sha base range (the holder is revoked by now), the
		// observer records a red observation on the head, and the
		// operator returns the contract citing it: the observation is
		// afforded to the observer on a review subject, the return to
		// the dispatch lane while the observation stands red, and the
		// unmergeable obligation is dischargeable at every position.
		walkStep{"root", version.Seed7, ledger.UpgradeVerb, "system", static(`{"to": "` + version.Seed8 + `"}`), "seed8"},
		walkStep{"root", version.Seed8, "intent.filed", "c-6", static(filedBody), ""},
		walkStep{"root", version.Seed8, "contract.specified", "c-6", static(specBody), ""},
		walkStep{"root", version.Seed8, "claim.taken", "c-6", static(`{}`), ""},
		walkStep{"root", version.Seed8, "submission.made", "c-6", func(t *testing.T, ctx *Context) string {
			return `{"fence": "` + fenceOf(t, ctx, "c-6") + `", "pr": "pr/6", "packet": ` + walkForgePacket + `}`
		}, "review-c6"},
		walkStep{"observer", version.Seed8, "check.observed", "c-6", static(`{"pr": "pr/6", "head": "` + walkHead + `", "checks": "red", "unresolved_threads": 2, "review": "none"}`), "observed-c6"},
		walkStep{"root", version.Seed8, "contract.returned", "c-6", func(t *testing.T, ctx *Context) string {
			s, ok := ctx.Lifecycle.State("c-6")
			if !ok || s.Observation == nil {
				t.Fatal("no observation stands on c-6")
			}
			return fmt.Sprintf(`{"observation": "%d"}`, s.Observation.Pos)
		}, "returned-c6"},
	)
}

// walkHead is the head c-6's submission names, a full sha the forge
// observation binds to; the base is a distinct full sha.
const walkHead = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

const walkForgePacket = `{"acceptance": ["c-6"], "decisions": [], "base": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa..` + walkHead + `", "refs": [], "findings": []}`

// evalFiledBody is a filing marked as an eval (plans/os-03e47abb.md D1).
const evalFiledBody = `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": {"name": "walk"}}`

// runWalkStep synthesizes the step's payload from the standing
// context and appends the signed record.
func runWalkStep(t *testing.T, store *ledger.Store, resolve ledger.Resolver, keys map[string]ed25519.PrivateKey, s walkStep) {
	t.Helper()
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	appendSignedV(t, store, resolve, keys[s.lane], s.v, s.verb, s.subject, s.payload(t, ctx))
}

// conformance: III.I — the walk drives one chain through birth,
// claim, spend, run, review, verdicts pass and fail, block, and
// halt, querying affordances for each lane along the way: every
// catalog verb except the actor.enrolled carve-out is listed at a
// position where it is legal, the curated illegal set never appears,
// and the output is sorted and stable. The history is the shared
// walkScript; the assertions run at its labeled stations.
func TestAffordancesWalk(t *testing.T) {
	store, resolve, signer := seededStore(t)
	lanes := walkLanes(t)
	keys := map[string]ed25519.PrivateKey{"root": signer}
	for name, key := range lanes {
		keys[name] = key
	}
	loose := walkResolver(t, resolve, lanes)
	ctxAt := func() *Context {
		c, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	union := map[string]bool{}
	list := func(key ed25519.PrivateKey, subject string) []string {
		out := Affordances(ctxAt(), key, subject)
		if !slices.IsSorted(out) {
			t.Fatalf("affordances must be sorted: %v", out)
		}
		for _, verb := range out {
			union[verb] = true
		}
		return out
	}
	has := func(list []string, verb string) bool { return slices.Contains(list, verb) }

	// Pre-upgrade: the root can upgrade the protocol.
	if l := list(signer, "system"); !has(l, "system.protocol.upgraded") {
		t.Fatalf("the root lists the upgrade on the fresh chain: %v", l)
	}

	stations := map[string]func(){
		"enrolled": func() {
			// Actor management and the system surface, for the
			// operator; then birth: filing is listed before the
			// subject exists and nothing claim-shaped is.
			if l := list(signer, fpOf(t, keys["holder"])); !has(l, "actor.granted") || !has(l, "actor.suspended") || !has(l, "actor.revoked") {
				t.Fatalf("the operator lists actor management on an enrolled actor: %v", l)
			}
			sys := list(signer, "system")
			if !has(sys, "system.halt.declared") || !has(sys, "system.checkpoint") {
				t.Fatalf("the operator lists halt and checkpoint on system: %v", sys)
			}
			fresh := list(signer, "c-1")
			if !has(fresh, "intent.filed") {
				t.Fatalf("filing lists on a fresh subject: %v", fresh)
			}
			if has(fresh, "claim.taken") || has(fresh, "run.started") {
				t.Fatalf("claim and run must not list before birth: %v", fresh)
			}
		},
		"seed5": func() {
			if l := list(signer, "system"); !has(l, "system.imported") {
				t.Fatalf("at seed/5 the operator lists the import on an unimported chain: %v", l)
			}
			if l := list(keys["holder"], "system"); has(l, "system.imported") {
				t.Fatalf("a worker never lists the import: %v", l)
			}
		},
		"imported": func() {
			if l := list(signer, "system"); has(l, "system.imported") {
				t.Fatalf("an imported chain lists no second import: %v", l)
			}
		},
		"seed7": func() {
			if l := list(signer, "system"); !has(l, "request.filed") || has(l, "request.answered") {
				t.Fatalf("at seed/7 a standing key lists the filing on system and nothing to answer: %v", l)
			}
			if l := list(signer, "c-1"); !has(l, "request.filed") {
				t.Fatalf("the filing lists on a contract: %v", l)
			}
		},
		"requested": func() {
			if l := list(signer, "system"); !has(l, "request.answered") {
				t.Fatalf("a pending request lists the answer for the operator: %v", l)
			}
		},
		"answered": func() {
			if l := list(signer, "system"); has(l, "request.answered") {
				t.Fatalf("an answered request lists no second answer: %v", l)
			}
			if l := list(keys["supervisor"], "system"); !has(l, "approval.requested") || has(l, "approval.granted") {
				t.Fatalf("a standing key lists the approval request on system and never an answer: %v", l)
			}
			if l := list(signer, "system"); has(l, "approval.granted") || has(l, "approval.denied") {
				t.Fatalf("with nothing pending the operator lists no answer: %v", l)
			}
		},
		"approval-requested": func() {
			if l := list(signer, "system"); !has(l, "approval.granted") || !has(l, "approval.denied") {
				t.Fatalf("a pending approval lists both answers for the operator: %v", l)
			}
			if l := list(keys["supervisor"], "system"); has(l, "approval.granted") {
				t.Fatalf("the requester answers nothing: %v", l)
			}
		},
		"approval-granted": func() {
			if l := list(signer, "system"); has(l, "approval.granted") || has(l, "approval.denied") {
				t.Fatalf("an answered approval lists no second answer: %v", l)
			}
		},
		"seed8": func() {
			// A blocked subject is not under review: nothing to observe.
			if l := list(keys["observer"], "c-3"); has(l, "check.observed") {
				t.Fatalf("the observation lists only on a review subject: %v", l)
			}
		},
		"review-c6": func() {
			if l := list(keys["observer"], "c-6"); !has(l, "check.observed") {
				t.Fatalf("the observer lists the forge observation on the submission under review: %v", l)
			}
			if l := list(signer, "c-6"); !has(l, "check.observed") || has(l, "contract.returned") {
				t.Fatalf("the operator fallback lists the observation and no return before one stands: %v", l)
			}
			if l := list(keys["verifier"], "c-6"); has(l, "check.observed") {
				t.Fatalf("the verifier never observes the forge: %v", l)
			}
		},
		"observed-c6": func() {
			if l := list(signer, "c-6"); !has(l, "contract.returned") {
				t.Fatalf("a red observation lists the return for the dispatch lane: %v", l)
			}
			if l := list(keys["observer"], "c-6"); !has(l, "check.observed") {
				t.Fatalf("a differing observation still lists: %v", l)
			}
		},
		"returned-c6": func() {
			if l := list(signer, "c-6"); !has(l, "claim.taken") || has(l, "contract.returned") {
				t.Fatalf("the returned subject is ready again: %v", l)
			}
			if l := list(keys["observer"], "c-6"); has(l, "check.observed") {
				t.Fatalf("nothing to observe once the window closed: %v", l)
			}
		},
		"filed-c1": func() {
			if l := list(signer, "c-1"); !has(l, "contract.specified") {
				t.Fatalf("specification lists on a filed subject: %v", l)
			}
		},
		"ready-c1": func() {
			// Ready: the worker can claim, the supervisor can offer,
			// the sealer can commit, the operator can cancel; the run
			// verbs cannot fire yet.
			if l := list(keys["holder"], "c-1"); !has(l, "claim.taken") {
				t.Fatalf("the holder lists the claim at ready: %v", l)
			}
			if l := list(keys["supervisor"], "c-1"); !has(l, "offer.published") || has(l, "run.started") {
				t.Fatalf("the supervisor lists the offer and no run at ready: %v", l)
			}
			if l := list(keys["sealer"], "c-1"); !has(l, "check.sealed") {
				t.Fatalf("the sealer lists the commitment at ready: %v", l)
			}
			if l := list(signer, "c-1"); !has(l, "contract.cancelled") {
				t.Fatalf("the operator lists cancellation at ready: %v", l)
			}
		},
		"claimed-c1": func() {
			// Claimed: the holder's window opens; a second claim does
			// not list; the verifier has nothing to judge.
			held := list(keys["holder"], "c-1")
			for _, verb := range []string{"claim.released", "claim.parked", "submission.made", "plan.proposed", "progress.milestone", "budget.reserve", "message.sent"} {
				if !has(held, verb) {
					t.Fatalf("the holder lists %s inside the window: %v", verb, held)
				}
			}
			if has(held, "claim.taken") {
				t.Fatalf("a second claim must not list while held: %v", held)
			}
			if l := list(signer, "c-1"); !has(l, "claim.reaped") {
				t.Fatalf("the operator lists the reap on a held subject: %v", l)
			}
			if l := list(keys["verifier"], "c-1"); has(l, "verdict.rendered") {
				t.Fatalf("no submission stands, so the verdict must not list: %v", l)
			}
		},
		"reserved-c1": func() {
			// Spend: the closes and the supervisor's run open over the
			// reservation.
			if l := list(keys["holder"], "c-1"); !has(l, "budget.settle") || !has(l, "budget.release") {
				t.Fatalf("the holder lists the closes over an open reservation: %v", l)
			}
			sup := list(keys["supervisor"], "c-1")
			if !has(sup, "run.started") || !has(sup, "run.interrupted") {
				t.Fatalf("the supervisor lists the run verbs over an open reservation: %v", sup)
			}
		},
		"running-c1": func() {
			sup := list(keys["supervisor"], "c-1")
			if !has(sup, "run.settled") {
				t.Fatalf("the supervisor lists the settle after the start: %v", sup)
			}
			if has(sup, "run.started") {
				t.Fatalf("one run per window: a second start must not list: %v", sup)
			}
			if l := list(signer, "c-1"); !has(l, "plan.approved") {
				t.Fatalf("the operator lists plan approval: %v", l)
			}
		},
		"review-c1": func() {
			// Review: the verifier's window opens; the pass chain
			// follows.
			if l := list(keys["verifier"], "c-1"); !has(l, "verdict.rendered") {
				t.Fatalf("the verifier lists the verdict on review: %v", l)
			}
			if l := list(keys["holder"], "c-1"); has(l, "verdict.rendered") {
				t.Fatalf("the holder is no verifier — the verdict must not list for the claim lane: %v", l)
			}
		},
		"passed-c1": func() {
			if l := list(signer, "c-1"); !has(l, "merge.requested") {
				t.Fatalf("the operator lists the merge request over the pass verdict: %v", l)
			}
		},
		"merged-c1": func() {
			if l := list(keys["observer"], "c-1"); !has(l, "merge.observed") {
				t.Fatalf("the observer lists the forge fact after the request: %v", l)
			}
		},
		"review-c2": func() {
			// The fail path: return and override list only over a
			// standing fail.
			if l := list(signer, "c-2"); has(l, "contract.returned") || has(l, "merge.overridden") {
				t.Fatalf("no fail stands, so return and override must not list: %v", l)
			}
		},
		"failed-c2": func() {
			if l := list(signer, "c-2"); !has(l, "contract.returned") || !has(l, "merge.overridden") {
				t.Fatalf("the operator lists return and override over the fail: %v", l)
			}
			if l := list(signer, "c-2"); !has(l, "wedge.declared") {
				t.Fatalf("the operator lists the wedge: %v", l)
			}
		},
		"ready-c3": func() {
			// The blocked path (blocked's one source state is ready).
			if l := list(signer, "c-3"); !has(l, "contract.blocked") {
				t.Fatalf("the operator lists blocking at ready: %v", l)
			}
		},
		"blocked-c3": func() {
			if l := list(signer, "c-3"); !has(l, "contract.unblocked") {
				t.Fatalf("the operator lists unblocking on a blocked subject: %v", l)
			}
		},
		"halted": func() {
			// Halt: the work verbs stop listing, and the lift remains.
			if l := list(signer, "system"); !has(l, "system.halt.lifted") {
				t.Fatalf("the operator lists the lift under halt: %v", l)
			}
			if l := Affordances(ctxAt(), keys["holder"], "c-3"); len(l) != 0 {
				t.Fatalf("under halt the worker's list empties: %v", l)
			}
		},
		"lifted": func() {},
		"eval-passed-c4": func() {
			// The eval passed on the holder's window: the supervisor
			// lists the mint on the holder, and nobody else does (the
			// verifier holds no supervise; the holder cannot qualify
			// itself).
			if l := list(keys["supervisor"], fpOf(t, keys["holder"])); !has(l, "actor.qualified") || has(l, "actor.disqualified") {
				t.Fatalf("the supervisor lists the mint on the holder after the eval's pass: %v", l)
			}
			if l := list(keys["holder"], fpOf(t, keys["holder"])); has(l, "actor.qualified") {
				t.Fatalf("the holder cannot qualify itself: %v", l)
			}
			if l := list(keys["verifier"], fpOf(t, keys["holder"])); has(l, "actor.qualified") {
				t.Fatalf("the verifier holds no supervise: %v", l)
			}
		},
		"qualified": func() {
			// One verdict, one consequence: the mint is spent.
			if l := list(keys["supervisor"], fpOf(t, keys["holder"])); has(l, "actor.qualified") {
				t.Fatalf("a verdict already cited mints no second time: %v", l)
			}
		},
		"eval-failed-c5": func() {
			if l := list(keys["supervisor"], fpOf(t, keys["holder"])); !has(l, "actor.disqualified") {
				t.Fatalf("the supervisor lists the disqualification on a holder of the failed configuration: %v", l)
			}
		},
		"disqualified": func() {
			if l := list(keys["supervisor"], fpOf(t, keys["holder"])); has(l, "actor.disqualified") {
				t.Fatalf("a configuration no longer held cannot be disqualified again: %v", l)
			}
		},
		"suspended": func() {
			// A suspended lane holds nothing: HasAnyCapability is
			// standing-aware, so the grant rule refuses every verb and
			// the list empties.
			if l := Affordances(ctxAt(), keys["holder"], "c-1"); len(l) != 0 {
				t.Fatalf("a suspended actor lists nothing: %v", l)
			}
			// And the reservation it left open is still closeable, by
			// the one lane that can: outside any claim window, citing
			// no fence.
			if l := list(signer, "c-1"); !has(l, "budget.settle") || !has(l, "budget.release") {
				t.Fatalf("the operator lists the closes over the suspended lane's open reservation: %v", l)
			}
		},
		"revoked": func() {
			if l := Affordances(ctxAt(), keys["holder"], "c-1"); len(l) != 0 {
				t.Fatalf("a revoked actor lists nothing: %v", l)
			}
			if l := list(signer, "c-1"); !has(l, "budget.settle") {
				t.Fatalf("revocation is terminal for the actor, not for the reservation: %v", l)
			}
		},
	}
	for _, s := range walkScript(t, lanes) {
		runWalkStep(t, store, loose, keys, s)
		if s.station != "" {
			fn, ok := stations[s.station]
			if !ok {
				t.Fatalf("no station block for %q", s.station)
			}
			fn()
		}
	}

	// Determinism at the final position.
	a := Affordances(ctxAt(), keys["holder"], "c-1")
	b := Affordances(ctxAt(), keys["holder"], "c-1")
	if !slices.Equal(a, b) {
		t.Fatalf("affordances must be deterministic: %v vs %v", a, b)
	}

	// Completeness: every catalog verb except the actor.enrolled
	// carve-out (its valid payload needs the queried subject's public
	// key, which no prober holds) was listed somewhere legal.
	var missing []string
	for _, p := range affordanceCatalog {
		if p.verb == "actor.enrolled" {
			continue
		}
		if !union[p.verb] {
			missing = append(missing, p.verb)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("catalog verbs never listed anywhere legal across the walk: %v", missing)
	}
}

const zeros64 = "0000000000000000000000000000000000000000000000000000000000000000"

func reservationOf(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || len(s.Reservations) == 0 {
		t.Fatalf("no reservation on %s", subject)
	}
	view := BudgetViewAt(ctx.Records, ctx.Table, subject, s)
	if len(view.Open) == 0 {
		t.Fatalf("no open reservation on %s", subject)
	}
	return fmt.Sprintf("%d", view.Open[0].Pos)
}

func submissionOf(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Submission == nil {
		t.Fatalf("no submission on %s", subject)
	}
	return fmt.Sprintf("%d", s.Submission.Pos)
}

func verdictOf(t *testing.T, ctx *Context, subject string) string {
	t.Helper()
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Verdict == nil {
		t.Fatalf("no verdict on %s", subject)
	}
	return fmt.Sprintf("%d", s.Verdict.Pos)
}
