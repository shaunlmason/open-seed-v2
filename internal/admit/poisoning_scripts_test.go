package admit

// The poison scripts (plans/os-e2f1ad23.md D1): one per corpus entry,
// each a chain built the fixture's way that ends in the attempt the
// corpus names. A script returns the refusal it met, the hypothesis
// subject it targeted, and the contract a claim would take inside the
// poison's applies-when.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// attempt drafts the act against the stand's tip and runs the boundary.
func attempt(t *testing.T, st *curationStand, key ed25519.PrivateKey, verb, subject, payload string) *poisonRun {
	t.Helper()
	err := Check(st.ctx, draftV(t, key, st.v, verb, subject, payload, st.ctx.Tip))
	return &poisonRun{st: st, verb: verb, err: err, subject: st.id, selected: "c-4"}
}

// worst returns the run whose refusal is missing, if any, so a script
// with several sub-attempts reports an admitted one over a refused one.
func worst(runs ...*poisonRun) *poisonRun {
	for _, r := range runs {
		if r.err == nil {
			return r
		}
	}
	return runs[0]
}

// admittedAndBound admits the stand's hypothesis and files a bound
// eval that passes: the promotion's legitimate premises, which the
// promotion poisons then bend.
func admittedAndBound(t *testing.T, st *curationStand) (hp, bound int, anchor string) {
	t.Helper()
	hp = st.admitHypothesis(t)
	anchor = curation.LessonsDir + "/retry.md @ 0123456"
	bound = st.evalRun(t, "eval-bound", cite(st.id, hp), anchor, "pass", nil)
	return hp, bound, anchor
}

// promoted admits the stand's hypothesis, files the bound pass and
// records the promotion the observer makes of it: the standing
// promotion the retirement and re-promotion poisons then attack.
func promoted(t *testing.T, st *curationStand) (hp, pp int, anchor string) {
	t.Helper()
	hp, bound, anchor := admittedAndBound(t, st)
	body := lessonBody(st.id, hp, "fix-the-check", bound)
	if err := Check(st.ctx, draftV(t, st.observer, st.v, curation.LessonVerb, st.id, body, st.ctx.Tip)); err != nil {
		t.Fatalf("the legitimate promotion admits: %v", err)
	}
	pp = st.ctx.Count
	st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, body)
	return hp, pp, anchor
}

// revalidation is the re-promotion of the stand's path at a new
// anchor with a fresh bound pass and the stamps the caller gives: the
// legitimate revalidation when they moved forward, the poison when
// they did not.
func revalidation(t *testing.T, st *curationStand, hp int, eval, lastValidated, expires string) string {
	t.Helper()
	anchor := curation.LessonsDir + "/retry.md @ 1234567"
	bound := st.evalRun(t, eval, cite(st.id, hp), anchor, "pass", nil)
	body := strings.Replace(lessonBody(st.id, hp, "fix-the-check", bound), "/retry.md @ 0123456", "/retry.md @ 1234567", 1)
	body = strings.Replace(body, "2026-09-01T00:00:00Z", lastValidated, 1)
	return strings.Replace(body, "2026-12-01T00:00:00Z", expires, 1)
}

// retireBody is the retirement payload: the promotion by its anchor
// and hypothesis, the reason, and whatever the script smuggles in.
func retireBody(anchor, hypothesis, reason, extra string) string {
	return fmt.Sprintf(`{"lesson": %q, "hypothesis": %q, "reason": %q%s}`, anchor, hypothesis, reason, extra)
}

// nothingPromoted points a run's ends at a hypothesis that has no
// promotion: the poisons that attack a standing promotion's
// retirement leave the legitimate lesson standing, which is the end
// they attack.
func nothingPromoted(r *poisonRun) *poisonRun {
	r.subject = curation.HypothesisID("nothing was promoted for this", nil)
	return r
}

var poisonScripts = map[string]func(t *testing.T) *poisonRun{
	"re-promotion-stale-stamps": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, _, _ := promoted(t, st)
		return nothingPromoted(attempt(t, st, st.observer, curation.LessonVerb, st.id, revalidation(t, st, hp, "eval-revalidated", "2026-09-01T00:00:00Z", "2027-03-01T00:00:00Z")))
	},
	"retirement-smuggled-field": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, _, anchor := promoted(t, st)
		return nothingPromoted(attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "expired", `, "lesson_body": "rewritten"`)))
	},
	"regression-without-revert": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, _, anchor := promoted(t, st)
		return nothingPromoted(worst(
			attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "regression", "")),
			attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "expired", `, "pr": "pr/10 @ 89abcde"`)),
			attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "rewritten", "")),
		))
	},
	"retire-superseded-promotion": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, _, anchor := promoted(t, st)
		st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, revalidation(t, st, hp, "eval-revalidated", "2026-10-01T00:00:00Z", "2027-03-01T00:00:00Z"))
		return nothingPromoted(attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "expired", "")))
	},
	"superseded-by-itself": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, pp, anchor := promoted(t, st)
		return nothingPromoted(worst(
			attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "superseded", fmt.Sprintf(`, "superseded_by": "%d"`, pp))),
			attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "superseded", fmt.Sprintf(`, "superseded_by": "%d"`, pp-1))),
			attempt(t, st, st.observer, curation.RetireVerb, st.id, retireBody(anchor, cite(st.id, hp), "superseded", fmt.Sprintf(`, "superseded_by": "%d"`, pp+1))),
		))
	},
	"deadend-retire-cross-contract": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.DeadEndRetireVerb, "c-1",
			`{"deadend": "`+cite("c-6", st.deadEnd6)+`", "environment": "moved", "reason": "the environment moved"}`)
	},
	"deadend-retire-phantom": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.DeadEndRetireVerb, "c-2",
			`{"deadend": "`+cite("c-2", st.park2)+`", "environment": "moved", "reason": "the environment moved"}`)
	},
	"deadend-retire-same-environment": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.DeadEndRetireVerb, "c-1",
			`{"deadend": "`+cite("c-1", st.deadEnd1)+`", "environment": "w", "reason": "nothing moved"}`)
	},
	"unretire-without-retirement": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.DeadEndUnretireVerb, "c-1",
			`{"deadend": "`+cite("c-1", st.deadEnd1)+`", "environment": "moved", "reason": "the environment moved"}`)
	},
	"duplicate-lesson": func(t *testing.T) *poisonRun {
		ls := newLintStand(t)
		dir := filepath.Join(ls.repo, curation.LessonsDir)
		if err := os.WriteFile(filepath.Join(dir, "retry-again.md"), []byte(ls.body), 0o644); err != nil {
			t.Fatal(err)
		}
		return &poisonRun{verb: "lint", err: curation.LintDuplicates(dir, nil)}
	},
	"frontmatter-smuggled-key": func(t *testing.T) *poisonRun {
		ls := newLintStand(t)
		structured := ls.body + "\n## Claim\n\nx\n\n## Evidence\n\ny\n\n## Applies when\n\nz\n"
		if err := curation.LintStructure([]byte(structured)); err != nil {
			t.Fatalf("the structured body lints: %v", err)
		}
		return worst(
			&poisonRun{verb: "lint", err: curation.LintStructure([]byte(strings.Replace(structured, "carrier: knowledge\n", "carrier: knowledge\nstage: promoted\n", 1)))},
			&poisonRun{verb: "lint", err: curation.LintStructure([]byte(strings.Replace(structured, "## Evidence\n\ny\n\n", "", 1)))},
			&poisonRun{verb: "lint", err: curation.LintStructure([]byte(strings.Replace(structured, "## Claim\n\nx\n\n## Evidence\n\ny\n", "## Evidence\n\ny\n\n## Claim\n\nx\n", 1)))},
		)
	},
	"deadend-bare-pointer": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.worker, curation.DeadEndVerb, "c-1",
			`{"fence": "`+activeFence(t, st.ctx, "c-1")+`", "tried": "x", "outcome": "y", "condition": "z", "environment": "w", "pointer": "notes.md"}`)
	},
	"deadend-not-holder": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.worker2, curation.DeadEndVerb, "c-1", deadEndBody(activeFence(t, st.ctx, "c-1")))
	},
	"proposal-smuggled-field": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		body := strings.Replace(st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)), `"exceptions"`, `"lesson": "`+curation.LessonsDir+`/retry.md @ 0123456", "exceptions"`, 1)
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, body)
	},
	"subject-forged": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		r := attempt(t, st, st.curator, curation.HypothesisVerb, "h-000000000000", st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
		r.subject = "h-000000000000"
		return r
	},
	"predicate-everything": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposalWith(st.claim, `{}`, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	},
	"predicate-unknown-field": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposalWith(st.claim, `{"routing": "core", "squad": "x"}`, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	},
	"unchanged-reproposal": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		st.ctx = st.step(st.curator, st.v, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6)))
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	},
	"forged-support": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return worst(
			attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1-3), cite("c-2", st.park2))),
			attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-5", st.raise5))),
			attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-2", 99999))),
		)
	},
	"grantless-window": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		rawClaim := st.ctx.Count
		st.ctx = st.step(st.stranger, st.v, "claim.taken", "c-4", `{}`)
		rawDeadEnd := st.ctx.Count
		st.ctx = st.step(st.stranger, st.v, curation.DeadEndVerb, "c-4", deadEndBody(fmt.Sprint(rawClaim)))
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-4", rawDeadEnd)))
	},
	"failed-support": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		st.ctx = st.step(st.worker, st.v, transition.VerdictRenderedVerb, "c-3",
			`{"verdict": "pass", "receipt": "`+strings.Repeat("0", 64)+`", "submission": "`+fmt.Sprint(st.submission3)+`", "independence": "L1"}`)
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-3", st.submission3)))
	},
	"single-success": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-1", st.deadEnd1b)))
	},
	"self-replay": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.curator, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-6", st.deadEnd6)))
	},
	"contest-smuggled-field": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		body := strings.Replace(contestBody(st, hp, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6)), `"reason"`, `"lesson": "x", "reason"`, 1)
		return attempt(t, st, st.curator, curation.ContestVerb, st.id, body)
	},
	"contest-phantom": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		return attempt(t, st, st.curator, curation.ContestVerb, st.id, contestBody(st, hp+1, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6)))
	},
	"contest-forged-evidence": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		return attempt(t, st, st.curator, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-5", st.raise5)))
	},
	"contest-unselected": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		return attempt(t, st, st.curator, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-9", st.deadEnd9)))
	},
	"held-out-forgery": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		return attempt(t, st, st.curator, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-1", st.deadEnd1)))
	},
	"promotion-traversal": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		body := strings.Replace(lessonBody(st.id, hp, "fix-the-check", bound), "/retry.md @ 0123456", "/../../retry.md @ 0123456", 1)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, body)
	},
	"promotion-of-raw-proposal": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		other := "retry twice"
		otherID := curation.HypothesisID(other, nil)
		rawPos := st.ctx.Count
		st.ctx = st.step(st.worker, st.v, curation.HypothesisVerb, otherID, st.proposalWith(other, appliesCore, nil, cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
		bound := st.evalRun(t, "eval-bound", cite(otherID, rawPos), curation.LessonsDir+"/retry.md @ 0123456", "pass", nil)
		r := attempt(t, st, st.observer, curation.LessonVerb, otherID, lessonBody(otherID, rawPos, "fix-the-check", bound))
		r.subject = otherID
		return r
	},
	"contested-promotion": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		st.ctx = st.step(st.curator, st.v, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6)))
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", bound))
	},
	"unknown-carrier": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, strings.Replace(lessonBody(st.id, hp, "fix-the-check", bound), `"knowledge"`, `"prompt"`, 1))
	},
	"stamps-unordered": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, strings.Replace(lessonBody(st.id, hp, "fix-the-check", bound), "2026-12-01", "2026-08-01", 1))
	},
	"digest-malformed": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, strings.Replace(lessonBody(st.id, hp, "fix-the-check", bound), strings.Repeat("a", 64), "not-a-digest", 1))
	},
	"smuggled-role-lesson": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		plain := plainPass(t, st)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, strings.Replace(lessonBody(st.id, hp, "fix-the-check", plain), `"knowledge"`, `"role"`, 1))
	},
	"raw-pass": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		anchor := curation.LessonsDir + "/retry.md @ 0123456"
		late := fixtureKey(t, 16)
		st.ctx = st.step(st.root, st.v, keyring.VerbEnrolled, fpOf(t, late), enrollBody(t, late, "agent", "late"))
		latePass := st.evalRun(t, "eval-late", cite(st.id, hp), anchor, "pass", late)
		st.ctx = st.step(st.root, st.v, keyring.VerbGranted, fpOf(t, late), `{"capability": "`+keyring.CapVerdict+`"}`)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", latePass))
	},
	"borrowed-pass": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		anchor := curation.LessonsDir + "/retry.md @ 0123456"
		otherBound := st.evalRun(t, "eval-other-hyp", cite(curation.HypothesisID("retry twice", nil), hp), anchor, "pass", nil)
		otherCarrier := st.evalRun(t, "eval-other-carrier", cite(st.id, hp), curation.LessonsDir+"/retry.md @ 9999999", "pass", nil)
		return worst(
			attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", otherBound)),
			attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", otherCarrier)),
		)
	},
	"stale-pass": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		anchor := curation.LessonsDir + "/retry.md @ 0123456"
		// The eval's five records land first, so the hypothesis will
		// stand five positions on; the marker binds that position.
		cited := st.ctx.Count + 5
		early := st.evalRun(t, "eval-early", cite(st.id, cited), anchor, "pass", nil)
		hp := st.admitHypothesis(t)
		if hp != cited {
			t.Fatalf("the early eval bound %d, the hypothesis stands at %d: the script's arithmetic drifted", cited, hp)
		}
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", early))
	},
	"fail-as-survival": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		failed := st.evalRun(t, "eval-failed", cite(st.id, hp), curation.LessonsDir+"/retry.md @ 0123456", "fail", nil)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", failed))
	},
	"support-failed-later": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		sub := st.ctx.Count
		st.ctx = st.step(st.worker, st.v, "submission.made", "c-1", `{"fence": "`+activeFence(t, st.ctx, "c-1")+`", "packet": `+findingPacket+`}`)
		st.ctx = st.step(st.verifier, st.v, transition.VerdictRenderedVerb, "c-1",
			`{"verdict": "fail", "receipt": "`+strings.Repeat("0", 64)+`", "submission": "`+fmt.Sprint(sub)+`", "independence": "L1"}`)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", bound))
	},
	"frontmatter-missing": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			bent := "# Retry once\n"
			return []byte(bent), ls.promote(t, bent), ls.h, ls.now
		})
	},
	"frontmatter-drift": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			bent := withFrontmatter(ls.body, "hypothesis", curation.HypothesisID("retry twice", nil)+"@4")
			return []byte(bent), ls.promote(t, bent), ls.h, ls.now
		})
	},
	"predicate-drift": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			bent := withFrontmatter(ls.body, "applies-when", `{"tier": "standard"}`)
			return []byte(bent), ls.promote(t, bent), ls.h, ls.now
		})
	},
	"support-drift": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			bent := withFrontmatter(ls.body, "support", "c-1@4, c-3@9")
			return []byte(bent), ls.promote(t, bent), ls.h, ls.now
		})
	},
	"fabricated-provenance": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			h := *ls.h
			h.Provenance = []string{"plans/never.md @ " + ls.planCommit}
			bent := withFrontmatter(ls.body, "provenance", "plans/never.md @ "+ls.planCommit)
			return []byte(bent), ls.promote(t, bent), &h, ls.now
		})
	},
	"digest-drift": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			fact := ls.fact
			fact.Digest = curation.Digest([]byte(ls.body + "\nrewritten\n"))
			return []byte(ls.body), fact, ls.h, ls.now
		})
	},
	"unmerged-anchor": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			ls.git("checkout", "--quiet", "-b", "side")
			side := ls.body + "\nside\n"
			if err := os.WriteFile(filepath.Join(ls.repo, ls.path), []byte(side), 0o644); err != nil {
				t.Fatal(err)
			}
			ls.git("commit", "--quiet", "-am", "side")
			commit := ls.git("rev-parse", "HEAD")
			ls.git("checkout", "--quiet", "main")
			fact := ls.fact
			fact.Lesson = ls.path + " @ " + commit
			fact.Digest = curation.Digest([]byte(side))
			return []byte(side), fact, ls.h, ls.now
		})
	},
	"stamps-future": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			return []byte(ls.body), ls.fact, ls.h, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		})
	},
	"carrier-drift": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			fact := ls.fact
			fact.Carrier = "role"
			return []byte(ls.body), fact, ls.h, ls.now
		})
	},
	"worker-proposes": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.worker, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	},
	"root-proposes": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		return attempt(t, st, st.root, curation.HypothesisVerb, st.id, st.proposal(cite("c-1", st.deadEnd1), cite("c-2", st.park2)))
	},
	"worker-granted-curate": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		r := attempt(t, st, st.root, keyring.VerbGranted, fpOf(t, st.worker), `{"capability": "`+keyring.CapCurate+`"}`)
		return r
	},
	"ungated-eval": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		marker := fmt.Sprintf(`{"name": "fix-the-check", "lesson": %q, "carrier": %q}`, cite(st.id, hp), curation.LessonsDir+"/retry.md @ 0123456")
		st.ctx = st.step(st.root, st.v, "intent.filed", "eval-ungated", `{"intent": "eval", "tier": "trivial", "budget": "small", "routing": "core", "eval": `+marker+`}`)
		return attempt(t, st, st.root, "contract.specified", "eval-ungated", `{"acceptance": {"ref": "evals/fix-the-check/fixture/accept.md @ abc1234", "executable": true}}`)
	},
	"raw-pushed-promotion": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp := st.admitHypothesis(t)
		plain := plainPass(t, st)
		body := strings.Replace(lessonBody(st.id, hp, "fix-the-check", plain), `"knowledge"`, `"role"`, 1)
		r := attempt(t, st, st.observer, curation.LessonVerb, st.id, body)
		// Pushed past the boundary anyway: the ends below must still
		// hold, so the fold re-judges it at its position.
		st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, body)
		return r
	},
	"raw-pushed-contest": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", bound))
		body := contestBody(st, hp, cite("c-1", st.deadEnd1))
		r := attempt(t, st, st.curator, curation.ContestVerb, st.id, body)
		st.ctx = st.step(st.curator, st.v, curation.ContestVerb, st.id, body)
		// The legitimate lesson keeps surfacing: the raw contest moved
		// nothing, which is the end this poison attacks.
		if fold := curation.Fold(st.ctx.Records); fold.Contested(st.id) || len(curation.Candidates(fold, st.ctx.Lifecycle, "c-4")) != 1 {
			t.Fatal("a raw-pushed contest disabled the lesson")
		}
		// The poisoned end is the contest's, not the lesson's: report the
		// promoted lesson's subject as untouched by pointing the ends at
		// a hypothesis that has none.
		r.subject = curation.HypothesisID("nothing was promoted for this", nil)
		return r
	},
	"pass-at-another-position": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		return attempt(t, st, st.observer, curation.LessonVerb, st.id, lessonBody(st.id, hp, "fix-the-check", bound-1))
	},
	"contested-surfacing": func(t *testing.T) *poisonRun {
		st := curationFixture(t)
		hp, bound, _ := admittedAndBound(t, st)
		good := lessonBody(st.id, hp, "fix-the-check", bound)
		st.ctx = st.step(st.observer, st.v, curation.LessonVerb, st.id, good)
		if len(curation.Candidates(curation.Fold(st.ctx.Records), st.ctx.Lifecycle, "c-4")) != 1 {
			t.Fatal("the promoted lesson surfaces before the contest")
		}
		st.ctx = st.step(st.curator, st.v, curation.ContestVerb, st.id, contestBody(st, hp, cite("c-1", st.deadEnd1b), cite("c-6", st.deadEnd6)))
		r := attempt(t, st, st.observer, curation.LessonVerb, st.id, good)
		// The lesson stands in the fold (evidence kept) and surfaces
		// nowhere: the ends read the candidates, not the promotion.
		r.subject = curation.HypothesisID("nothing surfaces for this", nil)
		if len(curation.Candidates(curation.Fold(st.ctx.Records), st.ctx.Lifecycle, "c-4")) != 0 {
			t.Fatal("a contested hypothesis's lesson surfaces")
		}
		return r
	},
	"stamps-unreviewed": func(t *testing.T) *poisonRun {
		return lintPoison(t, func(ls *lintStand) ([]byte, curation.LessonFact, *curation.HypothesisFact, time.Time) {
			fact := ls.fact
			fact.Expires = "2027-06-01T00:00:00Z"
			return []byte(ls.body), fact, ls.h, ls.now
		})
	},
}
