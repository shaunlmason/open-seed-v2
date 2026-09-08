// The knowledge verbs (plans/os-f30ee0d3.md D4; spec/curation.md):
// the curation facts driven against a real ledger with the fence and
// the hypothesis id derived, and the fold rendered. No loop act: the
// worker loop's exits already carry findings, and a standalone dead end
// is the holder's deliberate extra, like plan propose. Retirement, the
// dead-end acts, applicability, the bloat lints and the flags at an
// instant are plans/os-0d537fbd.md D2 to D4.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)

func runKnowledge(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "knowledge requires a subverb: deadend [retire | unretire] | propose | validate | contest | promote | retire | lint | show | search"), stdout, stderr)
	}
	switch args[0] {
	case "deadend":
		if len(args) > 1 && (args[1] == "retire" || args[1] == "unretire") {
			return runKnowledgeDeadEndRetire(args[1], args[2:], stdout, stderr)
		}
		return runKnowledgeDeadEnd(args[1:], stdout, stderr)
	case "retire":
		return runKnowledgeRetire(args[1:], stdout, stderr)
	case "propose":
		return runKnowledgePropose(args[1:], stdout, stderr)
	case "validate":
		return runKnowledgeValidate(args[1:], stdout, stderr)
	case "contest":
		return runKnowledgeContest(args[1:], stdout, stderr)
	case "promote":
		return runKnowledgePromote(args[1:], stdout, stderr)
	case "lint":
		return runKnowledgeLint(args[1:], stdout, stderr)
	case "show":
		return runKnowledgeShow(args[1:], stdout, stderr)
	case "search":
		return runKnowledgeSearch(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("unknown knowledge subverb %q: deadend [retire | unretire] | propose | validate | contest | promote | retire | lint | show | search", args[0])), stdout, stderr)
}

// runKnowledgeDeadEndRetire is the curator's act on a dead end whose
// environment moved (plans/os-0d537fbd.md D3): retire, or un-retire
// over a standing retirement, on the dead end's own contract.
func runKnowledgeDeadEndRetire(name string, args []string, stdout, stderr io.Writer) int {
	verb := curation.DeadEndRetireVerb
	if name == "unretire" {
		verb = curation.DeadEndUnretireVerb
	}
	fs := flag.NewFlagSet("knowledge deadend "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	deadend := fs.String("deadend", "", "the admitted dead end, <contract>@<position>")
	environment := fs.String("environment", "", "the environment now, which must differ from the one the previous act named")
	reason := fs.String("reason", "", "why the environment's move changes what the dead end says")
	err := fs.Parse(args)
	extra := ""
	for _, req := range []struct{ name, v string }{{"--deadend", *deadend}, {"--environment", *environment}, {"--reason", *reason}} {
		if err == nil && strings.TrimSpace(req.v) == "" {
			extra = req.name + " <text>"
			break
		}
	}
	if failEnv := knowledgeUsage("knowledge deadend "+name, f, err, fs.NArg(), extra); failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	c, ok := curation.ParseCitation(*deadend)
	if !ok {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--deadend %q is not <contract>@<position>", *deadend)), stdout, stderr)
	}
	subject := c.Contract
	payload := mustJSON(curation.DeadEndRetirement{DeadEnd: *deadend, Environment: *environment, Reason: *reason})
	if _, perr := curation.ParseDeadEndRetirement(verb, subject, payload); perr != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", perr.Error()), stdout, stderr)
	}
	signer, failEnv := loopSigner(*f.keyPath, *f.as)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	*f.subject = subject
	return ls.commit(f, loopAct{verb: verb, payload: payload,
		resultAt: func(int) map[string]any {
			return map[string]any{"subject": subject, "deadend": *deadend, "environment": *environment}
		}}, signer, stdout, stderr)
}

// runKnowledgeRetire is the retirement observation (plans/os-0d537fbd.md
// D2): the standing promotion revoked by regression (the revert's
// merge), supersession (a later promotion) or expiry, the evidence
// staying where it is.
func runKnowledgeRetire(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge retire", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	lesson := fs.String("lesson", "", "the retired promotion's lesson, \"knowledge/lessons/<id>.md @ <commit>\"")
	hypothesis := fs.String("hypothesis", "", "the promotion's hypothesis, <h-id>@<position>")
	reason := fs.String("reason", "", "why: "+strings.Join(curation.RetireReasons, " | "))
	pr := fs.String("pr", "", "regression only: the revert's merge, \"<pr> @ <merged-commit>\"")
	supersededBy := fs.String("superseded-by", "", "superseded only: the position of the later admitted promotion")
	err := fs.Parse(args)
	extra := ""
	switch {
	case err != nil:
	case strings.TrimSpace(*lesson) == "":
		extra = "--lesson <path @ commit>"
	case strings.TrimSpace(*hypothesis) == "":
		extra = "--hypothesis <h-id>@<position>"
	case strings.TrimSpace(*reason) == "":
		extra = "--reason <" + strings.Join(curation.RetireReasons, "|") + ">"
	}
	if failEnv := knowledgeUsage("knowledge retire", f, err, fs.NArg(), extra); failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	cit, ok := curation.ParseCitation(*hypothesis)
	if !ok || !curation.IsHypothesisSubject(cit.Contract) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--hypothesis %q is not <h-id>@<position>", *hypothesis)), stdout, stderr)
	}
	subject := cit.Contract
	payload := mustJSON(curation.Retirement{Lesson: *lesson, Hypothesis: *hypothesis, Reason: *reason, PR: *pr, SupersededBy: *supersededBy})
	if _, perr := curation.ParseRetirement(subject, payload); perr != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", perr.Error()), stdout, stderr)
	}
	signer, failEnv := loopSigner(*f.keyPath, *f.as)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	*f.subject = subject
	return ls.commit(f, loopAct{verb: curation.RetireVerb, payload: payload,
		resultAt: func(int) map[string]any {
			return map[string]any{"subject": subject, "lesson": *lesson, "reason": *reason}
		}}, signer, stdout, stderr)
}

// listFlag collects a repeatable string flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// knowledgeUsage is the transport half's refusal for the verbs whose
// subject is derived rather than given.
func knowledgeUsage(name string, f *loopFlags, parseErr error, narg int, extra string) *envelope.Envelope {
	if parseErr == nil && (*f.dir == "") != (*f.remote == "") && *f.keyPath != "" && *f.subject == "" && narg == 0 && extra == "" {
		return nil
	}
	msg := fmt.Sprintf("%s requires --ledger or --remote (not both) and --key <path>; the subject is derived, never given", name)
	if extra != "" {
		msg += ", " + extra
	}
	return envelope.Fail(envelope.ExitUsage, "usage", msg)
}

func runKnowledgeDeadEnd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge deadend", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	tried := fs.String("tried", "", "the approach that failed")
	outcome := fs.String("outcome", "", "why it failed")
	condition := fs.String("condition", "", "the failure condition: what was true when it failed")
	environment := fs.String("environment", "", "the environment it failed in")
	pointer := fs.String("pointer", "", "an anchored artifact (\"<path> @ <commit>\"), optional")
	err := fs.Parse(args)
	extra := ""
	for _, req := range []struct{ name, v string }{{"--tried", *tried}, {"--outcome", *outcome}, {"--condition", *condition}, {"--environment", *environment}} {
		if err == nil && strings.TrimSpace(req.v) == "" {
			extra = req.name + " <text>"
			break
		}
	}
	if failEnv := f.usage("knowledge deadend", err, fs.NArg(), extra); failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	signer, failEnv := loopSigner(*f.keyPath, *f.as)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	subject := *f.subject
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		fence, ok := activeFence(ctx, subject)
		if !ok {
			return nil, envelope.Fail(envelope.ExitInvalidTransition, "invalid_transition",
				fmt.Sprintf("no claim window is open on %s — a dead end is recorded by the holder inside its window (spec/curation.md)", subject))
		}
		d := curation.DeadEnd{Fence: fence, Tried: *tried, Outcome: *outcome, Condition: *condition, Environment: *environment, Pointer: *pointer}
		if _, perr := curation.ParseDeadEnd(subject, mustJSON(d)); perr != nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", perr.Error())
		}
		return mustJSON(d), nil
	}
	payload, failEnv := derive(ls.ctx)
	if failEnv != nil {
		return render(stampTip(failEnv, ls.ctx.Count), stdout, stderr)
	}
	return ls.commit(f, loopAct{verb: curation.DeadEndVerb, payload: payload, derive: derive, resultAt: terse(subject)}, signer, stdout, stderr)
}

func runKnowledgePropose(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge propose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	claim := fs.String("claim", "", "the hypothesis, as a claim")
	applies := fs.String("applies-when", "", "the predicate the claim holds under, the strict JSON object {routing?, tier?, paths?}")
	var support, provenance, exceptions listFlag
	fs.Var(&support, "support", "an admitted observation, <contract>@<position> (repeatable; at least two on two contracts)")
	fs.Var(&provenance, "provenance", "an anchored artifact, \"<path> @ <commit>\" (repeatable)")
	fs.Var(&exceptions, "exception", "a known exception to the claim (repeatable)")
	err := fs.Parse(args)
	extra := ""
	switch {
	case err != nil:
	case strings.TrimSpace(*claim) == "":
		extra = "--claim <text>"
	case strings.TrimSpace(*applies) == "":
		extra = "--applies-when <text>"
	case len(support) < curation.SupportMinimum:
		extra = fmt.Sprintf("at least %d --support citations on %d distinct contracts (a single run is never promotable)", curation.SupportMinimum, curation.SupportMinimum)
	}
	if failEnv := knowledgeUsage("knowledge propose", f, err, fs.NArg(), extra); failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	if exceptions == nil {
		exceptions = listFlag{}
	}
	if provenance == nil {
		provenance = listFlag{}
	}
	subject := curation.HypothesisID(*claim, exceptions)
	h := curation.Hypothesis{Claim: *claim, AppliesWhen: json.RawMessage(*applies), Support: support, Exceptions: exceptions, Provenance: provenance}
	if !json.Valid(h.AppliesWhen) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "--applies-when is the strict JSON object {routing?, tier?, paths?}"), stdout, stderr)
	}
	payload := mustJSON(h)
	if _, perr := curation.ParseHypothesis(subject, payload); perr != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", perr.Error()), stdout, stderr)
	}
	signer, failEnv := loopSigner(*f.keyPath, *f.as)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	*f.subject = subject
	return ls.commit(f, loopAct{verb: curation.HypothesisVerb, payload: payload,
		resultAt: func(int) map[string]any { return map[string]any{"subject": subject, "hypothesis": subject} }}, signer, stdout, stderr)
}

func runKnowledgePromote(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge promote", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	lesson := fs.String("lesson", "", "the merged lesson file, \"knowledge/lessons/<id>.md @ <commit>\"")
	hypothesis := fs.String("hypothesis", "", "the admitted hypothesis, <h-id>@<position>")
	pr := fs.String("pr", "", "the merged PR, \"<pr> @ <merged-commit>\"")
	repo := fs.String("repo", "", "the repository the lesson merged in: the digest is read from the file at its anchor")
	carrier := fs.String("carrier", "", "where the lesson lands: "+strings.Join(curation.Carriers, " | "))
	adversarial := fs.String("adversarial", "", "the counter-trajectory it survived, <eval>@<verdict position>")
	lastValidated := fs.String("last-validated", "", "RFC3339: restates the reviewed file's last-validated (read from the frontmatter at the anchor; refuses when it disagrees)")
	expires := fs.String("expires", "", "RFC3339: restates the reviewed file's expires (read from the frontmatter at the anchor; refuses when it disagrees)")
	err := fs.Parse(args)
	extra := ""
	switch {
	case err != nil:
	case strings.TrimSpace(*lesson) == "":
		extra = "--lesson <path @ commit>"
	case strings.TrimSpace(*hypothesis) == "":
		extra = "--hypothesis <h-id>@<position>"
	case strings.TrimSpace(*pr) == "":
		extra = "--pr <pr @ commit>"
	case strings.TrimSpace(*repo) == "":
		extra = "--repo <dir>"
	case strings.TrimSpace(*carrier) == "":
		extra = "--carrier <" + strings.Join(curation.Carriers, "|") + ">"
	case strings.TrimSpace(*adversarial) == "":
		extra = "--adversarial <eval>@<verdict position>"
	}
	if failEnv := knowledgeUsage("knowledge promote", f, err, fs.NArg(), extra); failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	cit, ok := curation.ParseCitation(*hypothesis)
	if !ok || !curation.IsHypothesisSubject(cit.Contract) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--hypothesis %q is not <h-id>@<position>", *hypothesis)), stdout, stderr)
	}
	evalName, verdictPos, ok := strings.Cut(*adversarial, "@")
	if !ok || evalName == "" || verdictPos == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--adversarial %q is not <eval>@<verdict position>", *adversarial)), stdout, stderr)
	}
	path, commit, ok := curation.AnchorParts(*lesson)
	if !ok {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--lesson %q is not \"<path> @ <commit>\"", *lesson)), stdout, stderr)
	}
	at, gerr := exec.Command("git", "-C", *repo, "show", commit+":"+path).Output()
	if gerr != nil {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("the lesson %s does not exist at its anchor in %s: the digest is read from the merged file, never typed", *lesson, *repo)), stdout, stderr)
	}
	// The lifecycle stamps are the reviewed file's (review finding on
	// the item 3 PR): read from the frontmatter at the anchor, never
	// typed; a flag may restate them and refuses when it disagrees, so
	// the fact never carries dates nobody reviewed.
	fm, ferr := curation.Frontmatter(at)
	if ferr != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("the lesson at %s has no readable frontmatter: %v", *lesson, ferr)), stdout, stderr)
	}
	for _, st := range []struct{ flag, key, given string }{{"--last-validated", "last-validated", *lastValidated}, {"--expires", "expires", *expires}} {
		if strings.TrimSpace(st.given) != "" && strings.TrimSpace(st.given) != fm[st.key] {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("%s %s disagrees with the reviewed file's %s %s at %s: the stamps are the merged frontmatter's, and a promotion recording others would carry dates nobody reviewed", st.flag, st.given, st.key, fm[st.key], *lesson)), stdout, stderr)
		}
	}
	subject := cit.Contract
	l := curation.Lesson{Lesson: *lesson, Hypothesis: *hypothesis, PR: *pr, Carrier: *carrier,
		Adversarial:   &curation.Adversarial{Eval: evalName, Verdict: verdictPos},
		LastValidated: fm["last-validated"], Expires: fm["expires"], Digest: curation.Digest(at)}
	payload := mustJSON(l)
	if _, perr := curation.ParseLesson(subject, payload); perr != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", perr.Error()), stdout, stderr)
	}
	signer, failEnv := loopSigner(*f.keyPath, *f.as)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	*f.subject = subject
	return ls.commit(f, loopAct{verb: curation.LessonVerb, payload: payload,
		resultAt: func(int) map[string]any { return map[string]any{"subject": subject, "lesson": *lesson} }}, signer, stdout, stderr)
}

// runKnowledgeValidate is the held-out listing (plans/os-96850e5a.md
// D3): the admitted observations on contracts the hypothesis's
// predicate selects that are outside its support set, for the curator
// to judge, then contest or promote. No machine decides that an
// outcome contradicts a claim. With --environment it also reports each
// selected contract's dead ends with their applicability
// (plans/os-0d537fbd.md D3): string equality with the environment
// given, the retired flag, and the held-out listing excluding retired
// dead ends.
func runKnowledgeValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	hypothesis := fs.String("hypothesis", "", "the admitted hypothesis, <h-id>@<position>")
	environment := fs.String("environment", "", "the environment a run would declare: each dead end is marked applicable to it or not, by string equality")
	if err := fs.Parse(args); err != nil || (*f.dir == "") == (*f.remote == "") || *hypothesis == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "knowledge validate requires --ledger <dir> or --remote <repo> (not both) and --hypothesis <h-id>@<position> [--environment <e>]"), stdout, stderr)
	}
	cit, ok := curation.ParseCitation(*hypothesis)
	if !ok || !curation.IsHypothesisSubject(cit.Contract) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--hypothesis %q is not <h-id>@<position>", *hypothesis)), stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	st := curation.Fold(ls.ctx.Records)
	h, ok := st.Hypothesis(cit.Contract)
	if !ok || h.Pos != cit.Position {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no hypothesis %s in the fold", *hypothesis)), ls.ctx.Count), stdout, stderr)
	}
	rows := []map[string]any{}
	for _, o := range curation.HeldOut(ls.ctx.Records, ls.ctx.Table, ls.ctx.Lifecycle, h) {
		rows = append(rows, map[string]any{"contract": o.Contract, "position": fmt.Sprintf("%d", o.Position), "verb": o.Verb, "holder": o.Holder})
	}
	deadEnds := []map[string]any{}
	for _, id := range ls.ctx.Lifecycle.Subjects() {
		if s, ok := ls.ctx.Lifecycle.State(id); !ok || !h.AppliesWhen.Selects(s) {
			continue
		}
		for _, d := range st.DeadEnds[id] {
			row := map[string]any{"contract": id, "position": fmt.Sprintf("%d", d.Pos), "environment": d.Environment, "retired": d.Retired}
			if d.Retired {
				row["retired_environment"] = d.RetiredEnvironment
			}
			if *environment != "" {
				row["applies"] = d.Applies(*environment)
			}
			deadEnds = append(deadEnds, row)
		}
	}
	out := map[string]any{"hypothesis": *hypothesis, "stage": h.Stage, "support": h.Support, "held_out": rows,
		"held_out_excludes": "retired dead ends", "dead_ends": deadEnds}
	if *environment != "" {
		out["environment"] = *environment
	}
	return render(stampTip(envelope.OK(out), ls.ctx.Count), stdout, stderr)
}

func runKnowledgeContest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge contest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	hypothesis := fs.String("hypothesis", "", "the admitted hypothesis, <h-id>@<position>")
	reason := fs.String("reason", "", "why the evidence contradicts the claim")
	var evidence listFlag
	fs.Var(&evidence, "evidence", "a held-out observation, <contract>@<position> (repeatable)")
	err := fs.Parse(args)
	extra := ""
	switch {
	case err != nil:
	case strings.TrimSpace(*hypothesis) == "":
		extra = "--hypothesis <h-id>@<position>"
	case len(evidence) == 0:
		extra = "at least one --evidence <contract>@<position>"
	case strings.TrimSpace(*reason) == "":
		extra = "--reason <text>"
	}
	if failEnv := knowledgeUsage("knowledge contest", f, err, fs.NArg(), extra); failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	cit, ok := curation.ParseCitation(*hypothesis)
	if !ok || !curation.IsHypothesisSubject(cit.Contract) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--hypothesis %q is not <h-id>@<position>", *hypothesis)), stdout, stderr)
	}
	subject := cit.Contract
	payload := mustJSON(curation.Contest{Hypothesis: *hypothesis, Evidence: evidence, Reason: *reason})
	if _, perr := curation.ParseContest(subject, payload); perr != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", perr.Error()), stdout, stderr)
	}
	signer, failEnv := loopSigner(*f.keyPath, *f.as)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	*f.subject = subject
	return ls.commit(f, loopAct{verb: curation.ContestVerb, payload: payload,
		resultAt: func(int) map[string]any { return map[string]any{"subject": subject, "hypothesis": *hypothesis} }}, signer, stdout, stderr)
}

// runKnowledgeLint is the promotion gate's file half
// (plans/os-96850e5a.md D4): the lesson file against its fact, its
// hypothesis and the repository, at the declared instant; then the
// bloat half (plans/os-0d537fbd.md D4): the file's structure, and the
// store the file sits in holding one file per hypothesis.
func runKnowledgeLint(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge lint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	repo := fs.String("repo", "", "the repository the lesson merged in")
	nowFlag := fs.String("now", "", "RFC3339: the instant the stamps are judged at (default: now)")
	if err := fs.Parse(args); err != nil || (*f.dir == "") == (*f.remote == "") || *repo == "" || fs.NArg() != 1 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "knowledge lint <file> requires --ledger <dir> or --remote <repo> (not both) and --repo <dir> [--now <RFC3339>]"), stdout, stderr)
	}
	now := time.Now().UTC()
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--now %q is not RFC3339", *nowFlag)), stdout, stderr)
		}
		now = parsed.UTC()
	}
	file := fs.Arg(0)
	body, err := os.ReadFile(file)
	if err != nil {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("cannot read %s: %v", file, err)), stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	fm, err := curation.Frontmatter(body)
	if err != nil {
		return render(stampTip(envelope.Fail(envelope.ExitChecksRed, "lint_refused", err.Error()), ls.ctx.Count), stdout, stderr)
	}
	st := curation.Fold(ls.ctx.Records)
	cit, ok := curation.ParseCitation(fm["hypothesis"])
	h, found := st.Hypothesis(cit.Contract)
	if !ok || !found || h.Pos != cit.Position {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("the frontmatter cites hypothesis %q, which the fold does not hold", fm["hypothesis"])), ls.ctx.Count), stdout, stderr)
	}
	var fact *curation.LessonFact
	rel := relativeTo(*repo, file)
	for _, l := range st.LessonsOf(cit.Contract) {
		if p, _, _ := curation.AnchorParts(l.Lesson); p == rel {
			fact = &l
		}
	}
	if fact == nil {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no admitted promotion cites %s: the file half judges a lesson the ledger promoted", rel)), ls.ctx.Count), stdout, stderr)
	}
	// The bloat half first, since it needs no repository: the file's
	// structure and the store's one-file-per-hypothesis; then the
	// gate's file half against the fact, the hypothesis and the
	// repository at the declared instant.
	for _, lint := range []func() error{
		func() error { return curation.LintStructure(body) },
		func() error { return curation.LintDuplicates(filepath.Dir(file), st) },
		func() error { return curation.LintFile(*repo, body, *fact, h, now) },
	} {
		if err := lint(); err != nil {
			var ge *curation.GateError
			if errors.As(err, &ge) {
				return render(stampTip(envelope.Fail(envelope.ExitChecksRed, "lint_refused", err.Error()), ls.ctx.Count), stdout, stderr)
			}
			return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), ls.ctx.Count), stdout, stderr)
		}
	}
	return render(stampTip(envelope.OK(map[string]any{"lesson": fact.Lesson, "hypothesis": fact.Hypothesis, "carrier": fact.Carrier, "digest": fact.Digest,
		"lint": "ok", "structure": "ok", "dedup": "ok", "store": filepath.Dir(file)}), ls.ctx.Count), stdout, stderr)
}

// relativeTo is the file's repository-relative path when it lies
// under the repository, else the path as given.
func relativeTo(repo, file string) string {
	absRepo, err1 := filepath.Abs(repo)
	absFile, err2 := filepath.Abs(file)
	if err1 != nil || err2 != nil {
		return file
	}
	rel, err := filepath.Rel(absRepo, absFile)
	if err != nil || strings.HasPrefix(rel, "..") {
		return file
	}
	return filepath.ToSlash(rel)
}

// runKnowledgeShow renders the knowledge view at the tip: with --now,
// at that instant, every expired lesson flagged stale; without it,
// nothing flagged and the view saying so (plans/os-0d537fbd.md D4).
func runKnowledgeShow(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	nowFlag := fs.String("now", "", "RFC3339: the instant staleness is judged at (default: none declared, nothing flagged)")
	if err := fs.Parse(args); err != nil || (*f.dir == "") == (*f.remote == "") || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "knowledge show requires --ledger <dir> or --remote <repo> (not both) [--now <RFC3339>]"), stdout, stderr)
	}
	var at *time.Time
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--now %q is not RFC3339", *nowFlag)), stdout, stderr)
		}
		utc := parsed.UTC()
		at = &utc
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	view := project.DeriveKnowledgeAt(ls.ctx.Records, at)
	var out map[string]any
	if err := json.Unmarshal(mustJSON(view), &out); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	return render(stampTip(envelope.OK(out), ls.ctx.Count), stdout, stderr)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// runKnowledgeSearch is the advisory lexical read (spec/curation.md
// "Search"): BM25 over the surfacing set at the instant (verified
// against --repo, the delivery posture: without it no lesson is
// indexed and every candidate is reported unresolved), the standing
// dead ends in the fold, and the sections of every --doc named under
// the repository. Ranked, never delivered: nothing here changes what a
// claim receives.
func runKnowledgeSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("knowledge search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	repo := fs.String("repo", "", "repository the lessons are verified against and the docs are read under (default: none, so no lesson is indexed)")
	nowFlag := fs.String("now", "", "RFC3339: the instant expiry is judged at (default: the wall clock)")
	limit := fs.Int("limit", 10, "at most this many hits (0: every hit)")
	var docs listFlag
	fs.Var(&docs, "doc", "a markdown file under --repo to index by section (repeatable)")
	const usage = "knowledge search requires --ledger <dir> or --remote <repo> (not both) [--repo <dir>] [--now <RFC3339>] [--limit <n>] [--doc <path>]... then the query, one or more words"
	if err := fs.Parse(args); err != nil || (*f.dir == "") == (*f.remote == "") || fs.NArg() == 0 || *limit < 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", usage), stdout, stderr)
	}
	query := strings.Join(fs.Args(), " ")
	terms := curation.QueryTerms(query)
	if len(terms) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "the query carries no searchable term (two or more letters or digits)"), stdout, stderr)
	}
	if len(docs) > 0 && *repo == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "--doc names a path under --repo, which is not given"), stdout, stderr)
	}
	at := time.Now().UTC()
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--now %q is not RFC3339", *nowFlag)), stdout, stderr)
		}
		at = parsed.UTC()
	}
	var sections []curation.Document
	for _, d := range docs {
		if !curation.CleanRelative(d) {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--doc %q is not a clean relative path under the repository", d)), stdout, stderr)
		}
		b, err := os.ReadFile(filepath.Join(*repo, filepath.FromSlash(d)))
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--doc %s is not readable under %s: %v", d, *repo, err)), stdout, stderr)
		}
		sections = append(sections, curation.SectionDocuments(d, string(b))...)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer ls.done()
	st := curation.Fold(ls.ctx.Records)
	lessons, unresolved := curation.LessonDocuments(st, *repo, at)
	deadEnds := curation.DeadEndDocuments(st)
	corpus := append(append(lessons, deadEnds...), sections...)
	hits := curation.NewIndex(corpus).Search(query, *limit)
	out := map[string]any{
		"query":     query,
		"terms":     terms,
		"as_of":     at.Format(time.RFC3339),
		"documents": map[string]int{curation.SearchKindLesson: len(lessons), curation.SearchKindDeadEnd: len(deadEnds), curation.SearchKindDoc: len(sections)},
		"hits":      hits,
		// The same rows delivery reports: a candidate the repository
		// does not verify is named, never indexed.
		"lessons_unresolved": unresolved,
		"advisory":           "ranked by lexical match; a lesson surfaces at claim time by its applies-when alone",
	}
	return render(stampTip(envelope.OK(out), ls.ctx.Count), stdout, stderr)
}
