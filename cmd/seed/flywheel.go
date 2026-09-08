// The flywheel verbs (plans/os-9075c308.md; spec/flywheel.md):
// shapes lists the contract shapes the record derives and which of
// them recur; draft writes a shape's deterministic workflow and, with
// --validate, runs it through the v1 engine from a staging worktree;
// propose writes the validated draft on its own branch and appends
// workflow.proposed from the curator's key; repair files the bounded
// repair contract from the dispatcher's key when the engine refuses a
// draft; status renders the report's flywheel rows. Nothing here
// writes under the registry on main: the file reaches it through the
// PR the governance root reviews.

package main

import (
	"crypto/ed25519"
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
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/flywheel"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

func runFlywheel(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "flywheel requires a subverb: shapes, draft, propose, repair, observe, or status"), stdout, stderr)
	}
	switch args[0] {
	case "shapes":
		return runFlywheelShapes(args[1:], stdout, stderr)
	case "draft":
		return runFlywheelDraft(args[1:], stdout, stderr)
	case "propose":
		return runFlywheelPropose(args[1:], stdout, stderr)
	case "repair":
		return runFlywheelRepair(args[1:], stdout, stderr)
	case "observe":
		return runFlywheelObserve(args[1:], stdout, stderr)
	case "status":
		return runFlywheelStatus(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown flywheel subverb %q — shapes, draft, propose, repair, observe, or status", args[0])), stdout, stderr)
}

// runFlywheelObserve appends workflow.merged from the observer's key:
// the forge fact that the proposal's PR landed the file in the
// registry, citing the standing proposal's path at the merged commit.
func runFlywheelObserve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flywheel observe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	shapeID := fs.String("shape", "", "the shape whose proposal merged")
	merged := fs.String("merged", "", "the merged commit")
	pr := fs.String("pr", "", "the pull request that merged")
	parseErr := fs.Parse(args)
	if parseErr != nil || (*f.dir == "") == (*f.remote == "") || *f.keyPath == "" || fs.NArg() != 0 || *shapeID == "" || *merged == "" || *pr == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "flywheel observe requires --ledger or --remote (not both), --key <path>, --shape <id>, --merged <commit> and --pr <pr>"), stdout, stderr)
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	*f.subject = *shapeID
	standing, ok := flywheel.Fold(ls.ctx.Records).Standing(*shapeID)
	if !ok {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no unmerged proposal stands for shape %s: the observation cites an admitted proposal", *shapeID)), ls.ctx.Count), stdout, stderr)
	}
	m := flywheel.Merge{Workflow: standing.Path() + " @ " + *merged, Shape: *shapeID, PR: *pr + " @ " + *merged}
	payload := mustJSON(m)
	return ls.commit(f, loopAct{verb: flywheel.MergedVerb, payload: payload, resultAt: func(int) map[string]any {
		return map[string]any{"shape": *shapeID, "workflow": m.Workflow, "pr": m.PR, "proposal": standing.Pos}
	}}, signer, stdout, stderr)
}

// shapeRows renders shapes for an envelope.
func shapeRows(shapes []flywheel.Shape) []map[string]any {
	rows := []map[string]any{}
	for _, s := range shapes {
		occ := []map[string]any{}
		for _, o := range s.Occurrences {
			occ = append(occ, map[string]any{"subject": o.Subject, "done": o.Done, "ref": o.Ref, "gated": o.Gated})
		}
		rows = append(rows, map[string]any{
			"id": s.ID, "name": s.Name(), "routing": s.Routing, "acceptance_path": s.AcceptancePath, "tier": s.Tier,
			"sequence": s.Sequence, "occurrences": occ, "count": len(s.Occurrences), "recurring": s.Recurring(),
		})
	}
	return rows
}

func runFlywheelShapes(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flywheel shapes", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	posture := bindReadPosture(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || !posture.resolved() {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "flywheel shapes requires --ledger <dir> or --remote <repo> (not both)"), stdout, stderr)
	}
	st, _, done, failEnv := posture.open()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer done()
	shapes := flywheel.Shapes(st.records, st.fold)
	recurring := 0
	for _, s := range shapes {
		if s.Recurring() {
			recurring++
		}
	}
	return render(stampTip(envelope.OK(map[string]any{"shapes": shapeRows(shapes), "recurring": recurring, "recurring_after": flywheel.RecurringAfter}), st.count), stdout, stderr)
}

func runFlywheelStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flywheel status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	posture := bindReadPosture(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || !posture.resolved() {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "flywheel status requires --ledger <dir> or --remote <repo> (not both)"), stdout, stderr)
	}
	st, _, done, failEnv := posture.open()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer done()
	return render(stampTip(envelope.OK(map[string]any{"flywheel": flywheel.Derive(st.records, st.fold)}), st.count), stdout, stderr)
}

// draftEnvelope maps the drafter's refusals: an ungated occurrence is
// a spec that cannot be drafted from (19), divergent command lists a
// shape that is not one chore (9), an anchor the repository lacks not
// found (4).
func draftEnvelope(err error) *envelope.Envelope {
	var ungated *flywheel.UngatedError
	var divergent *flywheel.DivergentError
	var anchor *flywheel.AnchorError
	switch {
	case errors.As(err, &ungated):
		return envelope.Fail(envelope.ExitSpecUnrunnable, "spec_unrunnable", err.Error())
	case errors.As(err, &divergent):
		return envelope.Fail(envelope.ExitClassificationRef, "classification_refused", err.Error())
	case errors.As(err, &anchor):
		return envelope.Fail(envelope.ExitNotFound, "not_found", err.Error())
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}

// engineEnvelope maps the engine's answer: a taken name is an illegal
// step (3, refined name_taken: the registry already holds the name,
// so nothing is staged), the engine's refusal a check gone red on the
// draft (20), naming the stage, the step, the finding and the owed
// act.
func engineEnvelope(shape string, err error) *envelope.Envelope {
	var taken *flywheel.NameTakenError
	var refused *flywheel.EngineError
	switch {
	case errors.As(err, &taken):
		return envelope.Fail(envelope.ExitInvalidTransition, "name_taken", err.Error())
	case errors.As(err, &refused):
		return envelope.Fail(envelope.ExitChecksRed, "checks_red",
			fmt.Sprintf("%s — nothing appended; the owed act is the repair contract, filed under the dispatcher's key: seed flywheel repair --shape %s (spec/flywheel.md)", err.Error(), shape))
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}

// flywheelBranch is the branch a shape's workflow is proposed on.
func flywheelBranch(shape flywheel.Shape) string { return "seed/flywheel-" + shape.ID }

// writeOnBranch writes files on the shape's branch through a
// temporary worktree, never the caller's checkout and never main: the
// branch is created from the repository's head when absent and
// extended when present, the files committed, the worktree removed.
// Returns the commit the files stand at.
func writeOnBranch(repo, branch string, files map[string][]byte, message string) (string, error) {
	if branch == "main" || branch == "master" || !strings.HasPrefix(branch, "seed/flywheel-") {
		return "", fmt.Errorf("refusing to write on %s: a proposal lands on seed/flywheel-<shape> and reaches main only through its PR", branch)
	}
	base, err := gitOut(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "seed-flywheel-branch-*")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = gitRun(repo, "worktree", "remove", "--force", dir)
		_ = gitRun(repo, "worktree", "prune")
		_ = os.RemoveAll(dir)
	}()
	if _, err := gitOut(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		if err := gitRun(repo, "worktree", "add", dir, branch); err != nil {
			return "", err
		}
	} else if err := gitRun(repo, "worktree", "add", "-b", branch, dir, base); err != nil {
		return "", err
	}
	for path, body := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			return "", err
		}
		if err := gitRun(dir, "add", "--", path); err != nil {
			return "", err
		}
	}
	if err := gitRun(dir, "-c", "user.name=seed", "-c", "user.email=seed@flywheel.invalid", "commit", "--quiet", "--allow-empty", "-m", message); err != nil {
		return "", err
	}
	return gitOut(dir, "rev-parse", "HEAD")
}

// gitRun runs a git command whose success is silent, and returns its
// combined output in the error when it fails.
func gitRun(repo string, args ...string) error {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runFlywheelDraft(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flywheel draft", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	posture := bindReadPosture(fs)
	shapeID := fs.String("shape", "", "the shape to draft, from flywheel shapes")
	repo := fs.String("repo", "", "repository the acceptance specs are read from at their gated anchors")
	out := fs.String("out", "", "write the drafted workflow here (default: carried in the envelope)")
	validate := fs.Bool("validate", false, "stage the draft in a worktree and run the v1 engine's validate and mock run")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || !posture.resolved() || *shapeID == "" || *repo == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "flywheel draft requires (--ledger <dir> | --remote <repo>) --shape <id> --repo <dir> [--out <file>] [--validate]"), stdout, stderr)
	}
	st, _, done, failEnv := posture.open()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	defer done()
	shape, ok := flywheel.Find(flywheel.Shapes(st.records, st.fold), *shapeID)
	if !ok {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no done contract folds to shape %s", *shapeID)), st.count), stdout, stderr)
	}
	d, err := flywheel.DraftWorkflow(shape, *repo)
	if err != nil {
		return render(stampTip(draftEnvelope(err), st.count), stdout, stderr)
	}
	result := map[string]any{"shape": shape.ID, "name": d.Name, "path": d.Path(), "commands": d.Commands, "inputs": d.Inputs, "recurring": shape.Recurring()}
	if *out != "" {
		if err := os.WriteFile(*out, d.Bytes, 0o644); err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot write --out: %v", err)), stdout, stderr)
		}
		result["out"] = *out
	} else {
		result["workflow"] = string(d.Bytes)
	}
	if *validate {
		base, err := gitOut(*repo, "rev-parse", "HEAD")
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
		v, err := flywheel.ValidateDraft(*repo, base, d)
		if err != nil {
			env := engineEnvelope(shape.ID, err)
			env.Result = result
			return render(stampTip(env, st.count), stdout, stderr)
		}
		result["validated"] = map[string]any{"run": v.RunID, "steps": v.Steps}
	}
	return render(stampTip(envelope.OK(result), st.count), stdout, stderr)
}

// flywheelSession opens the loop seam for an act on a shape: the
// subject is the shape id, derived, never given.
func flywheelUsage(name string, f *loopFlags, parseErr error, narg int, shape, repo string) *envelope.Envelope {
	if parseErr == nil && (*f.dir == "") != (*f.remote == "") && *f.keyPath != "" && narg == 0 && shape != "" && repo != "" {
		return nil
	}
	return envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("%s requires --ledger or --remote (not both), --key <path>, --shape <id> and --repo <dir>", name))
}

// flywheelPrecheck drafts the proposal as it would stand at the base
// and runs the boundary over it, so a refusal names its gate before
// anything is validated or written.
func flywheelPrecheck(ls *loopSession, signer ed25519.PrivateKey, shape flywheel.Shape, d *flywheel.Draft, base string) *envelope.Envelope {
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return envelope.Fail(envelope.ExitUsage, "usage", err.Error())
	}
	p := flywheel.Proposal{Shape: shape.ID, Workflow: d.Path() + " @ " + base}
	for _, occ := range shape.Occurrences {
		p.Occurrences = append(p.Occurrences, occ.Cite())
	}
	p.Validated.Run = "wf-precheck"
	if _, passed := flywheel.Repairs(ls.ctx.Records, ls.ctx.Lifecycle, shape.ID); len(passed) > 0 {
		p.Repair = passed[0].Cite()
	}
	rec, err := event.Sign(event.Event{
		V: ls.ctx.Active, TS: time.Now().UTC().Format(time.RFC3339), Actor: fp,
		Verb: flywheel.ProposedVerb, Subject: shape.ID, Payload: mustJSON(p), Prev: ls.ctx.Tip,
	}, signer)
	if err != nil {
		return envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot sign the act: %v", err))
	}
	if err := admit.Check(ls.ctx, rec); err != nil {
		return stampTip(remoteFailureEnvelope(err), ls.ctx.Count)
	}
	return nil
}

// repairFor finds the shape's repair contract in the fold: the one
// filed, whether short of or past its verdict.
func repairFor(ctx *admit.Context, shape string) (subject string, passedAt int, found, passed bool) {
	open, done := flywheel.Repairs(ctx.Records, ctx.Lifecycle, shape)
	switch {
	case len(open) > 0:
		return open[0], 0, true, false
	case len(done) > 0:
		return done[0].Subject, done[0].Verdict, true, true
	}
	return "", 0, false, false
}

func runFlywheelPropose(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flywheel propose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	shapeID := fs.String("shape", "", "the shape to propose, from flywheel shapes")
	repo := fs.String("repo", "", "repository the draft is written to on seed/flywheel-<shape>")
	parseErr := fs.Parse(args)
	if env := flywheelUsage("flywheel propose", f, parseErr, fs.NArg(), *shapeID, *repo); env != nil {
		return render(env, stdout, stderr)
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	shape, ok := flywheel.Find(flywheel.Shapes(ls.ctx.Records, ls.ctx.Lifecycle), *shapeID)
	if !ok {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no done contract folds to shape %s", *shapeID)), ls.ctx.Count), stdout, stderr)
	}
	*f.subject = shape.ID
	base, err := gitOut(*repo, "rev-parse", "HEAD")
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	branch := flywheelBranch(shape)
	d, err := flywheel.DraftWorkflow(shape, *repo)
	if err != nil {
		return render(stampTip(draftEnvelope(err), ls.ctx.Count), stdout, stderr)
	}
	// The boundary first, on a provisional fact: a proposal the record
	// refuses (the grant, a standing proposal, an open repair) stages
	// nothing and writes no branch. The admitted fact is re-judged at
	// the append, where the tip may have moved.
	if env := flywheelPrecheck(ls, signer, shape, d, base); env != nil {
		return render(env, stdout, stderr)
	}
	// A repair contract for the shape decides which file is proposed:
	// short of a passed verdict, the boundary refuses (repair_open),
	// and the refusal is left to it so the record says why; past one,
	// the branch's file is validated AS IT STANDS, never regenerated
	// over the fix (D7).
	var v *flywheel.Validation
	commit := ""
	repairCite := ""
	subject, passedAt, found, passed := repairFor(ls.ctx, shape.ID)
	if found && passed {
		head, err := gitOut(*repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		if err != nil {
			return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("the repair contract %s passed and the branch %s does not exist in %s: the fix lands on the branch the repair filed against", subject, branch, *repo)), ls.ctx.Count), stdout, stderr)
		}
		v, err = flywheel.ValidateAt(*repo, base, head, d.Name)
		if err != nil {
			return render(stampTip(engineEnvelope(shape.ID, err), ls.ctx.Count), stdout, stderr)
		}
		commit, repairCite = head, fmt.Sprintf("%s@%d", subject, passedAt)
	} else if !found {
		v, err = flywheel.ValidateDraft(*repo, base, d)
		if err != nil {
			return render(stampTip(engineEnvelope(shape.ID, err), ls.ctx.Count), stdout, stderr)
		}
		commit, err = writeOnBranch(*repo, branch, map[string][]byte{d.Path(): d.Bytes},
			fmt.Sprintf("flywheel: propose %s for shape %s", d.Name, shape.ID))
		if err != nil {
			return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), ls.ctx.Count), stdout, stderr)
		}
	}
	var cites []string
	for _, occ := range shape.Occurrences {
		cites = append(cites, occ.Cite())
	}
	p := flywheel.Proposal{Shape: shape.ID, Occurrences: cites, Repair: repairCite}
	if v != nil {
		p.Validated.Run = v.RunID
		p.Workflow = d.Path() + " @ " + commit
	} else {
		// The repair stands short of its verdict: the boundary refuses
		// this proposal by name, appending nothing.
		p.Validated.Run = "wf-unvalidated"
		p.Workflow = d.Path() + " @ " + base
	}
	payload := mustJSON(p)
	return ls.commit(f, loopAct{verb: flywheel.ProposedVerb, payload: payload, resultAt: func(int) map[string]any {
		return map[string]any{
			"shape": shape.ID, "workflow": p.Workflow, "branch": branch, "branch_head": commit, "repair": repairCite,
			"pr": map[string]any{"head": branch, "base": "main", "title": fmt.Sprintf("flywheel: %s, the workflow for shape %s", d.Name, shape.ID)},
		}
	}}, signer, stdout, stderr)
}

func runFlywheelRepair(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flywheel repair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	shapeID := fs.String("shape", "", "the shape whose draft the engine refused")
	repo := fs.String("repo", "", "repository the draft and the acceptance are written to on seed/flywheel-<shape>")
	parseErr := fs.Parse(args)
	if env := flywheelUsage("flywheel repair", f, parseErr, fs.NArg(), *shapeID, *repo); env != nil {
		return render(env, stdout, stderr)
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	shape, ok := flywheel.Find(flywheel.Shapes(ls.ctx.Records, ls.ctx.Lifecycle), *shapeID)
	if !ok {
		return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no done contract folds to shape %s", *shapeID)), ls.ctx.Count), stdout, stderr)
	}
	// The filing is the dispatcher's: a key without dispatch refuses
	// here, before anything is written, naming the owed act (the eval
	// act posture).
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	if ls.ctx.Keyring == nil || !ls.ctx.Keyring.HasAnyCapability(fp, keyring.AcceptedCapabilities("intent.filed")) {
		return render(stampTip(envelope.Fail(envelope.ExitOutOfGrant, "out_of_grant",
			fmt.Sprintf("the repair contract is filed by the dispatcher (intent.filed accepts %v) and %s holds no such grant: nothing was written or appended; the act is owed by the dispatcher lane", keyring.AcceptedCapabilities("intent.filed"), fp)), ls.ctx.Count), stdout, stderr)
	}
	if subject, _, found, _ := repairFor(ls.ctx, shape.ID); found {
		return render(stampTip(envelope.Fail(envelope.ExitInvalidTransition, "invalid_transition",
			fmt.Sprintf("repair contract %s already stands for shape %s: one repair per shape until it passes", subject, shape.ID)), ls.ctx.Count), stdout, stderr)
	}
	base, err := gitOut(*repo, "rev-parse", "HEAD")
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	d, err := flywheel.DraftWorkflow(shape, *repo)
	if err != nil {
		return render(stampTip(draftEnvelope(err), ls.ctx.Count), stdout, stderr)
	}
	_, verr := flywheel.ValidateDraft(*repo, base, d)
	var refused *flywheel.EngineError
	if verr == nil {
		return render(stampTip(envelope.Fail(envelope.ExitInvalidTransition, "invalid_transition",
			fmt.Sprintf("the engine accepts the draft for shape %s: nothing to repair, propose it", shape.ID)), ls.ctx.Count), stdout, stderr)
	}
	if !errors.As(verr, &refused) {
		return render(stampTip(engineEnvelope(shape.ID, verr), ls.ctx.Count), stdout, stderr)
	}
	branch := flywheelBranch(shape)
	acceptance := flywheel.RepairAcceptance(d, refused)
	commit, err := writeOnBranch(*repo, branch, map[string][]byte{d.Path(): d.Bytes, flywheel.RepairAcceptancePath(shape.ID): acceptance},
		fmt.Sprintf("flywheel: repair %s for shape %s (%s)", d.Name, shape.ID, refused.Stage))
	if err != nil {
		return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), ls.ctx.Count), stdout, stderr)
	}
	intent, spec := flywheel.RepairFiling(shape, d, refused, branch, commit)
	subject := flywheel.RepairSubject(shape)
	var positions []int
	for _, step := range []struct{ verb, payload string }{{"intent.filed", string(intent)}, {"contract.specified", string(spec)}} {
		pos, failEnv := evalAppend(f, signer, step.verb, subject, step.payload)
		if failEnv != nil {
			failEnv.Result = map[string]any{"subject": subject, "appended": positions, "refused": step.verb, "branch": branch, "branch_head": commit}
			return render(failEnv, stdout, stderr)
		}
		positions = append(positions, pos)
	}
	return render(envelope.OK(map[string]any{
		"subject": subject, "shape": shape.ID, "branch": branch, "branch_head": commit, "appended": positions,
		"acceptance": flywheel.RepairAcceptancePath(shape.ID) + " @ " + commit,
		"finding":    map[string]any{"stage": refused.Stage, "step": refused.Step, "finding": refused.Finding},
	}), stdout, stderr)
}
