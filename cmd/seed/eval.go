// The eval surface (plans/os-03e47abb.md; spec/evals.md): list the
// shipped definitions, check one (fixture red, solution green through
// the verifier's own runner), file one as an ordinary contract marked
// as an eval, report what the chain owes at a declared instant, and
// act on it. seed eval act is the wakeless pass the supervisor and the
// dispatcher run under their own keys: it performs the subset of the
// derivation their grants admit and reports the rest as owed by the
// other lane. The maintenance pass gains nothing.

package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/eval"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
)

const evalSubverbs = "list, check, file, status, or act"

func runEval(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "eval requires a subverb: "+evalSubverbs), stdout, stderr)
	}
	switch args[0] {
	case "list":
		return runEvalList(args[1:], stdout, stderr)
	case "check":
		return runEvalCheck(args[1:], stdout, stderr)
	case "file":
		return runEvalFile(args[1:], stdout, stderr)
	case "status":
		return runEvalDue(args[1:], false, stdout, stderr)
	case "act":
		return runEvalDue(args[1:], true, stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown eval subverb %q — %s", args[0], evalSubverbs)), stdout, stderr)
}

// evalFailEnvelope maps the eval package's refusals onto the verdict
// pipeline's families: a definition not at a reviewed revision is
// ungated content (18), a vacuous eval decides nothing (19, the
// eval_vacuous refinement), a red solution is checks red (20).
func evalFailEnvelope(err error) *envelope.Envelope {
	switch {
	case errors.Is(err, eval.ErrNotReviewed):
		return envelope.Fail(envelope.ExitUngated, "ungated", err.Error())
	case errors.Is(err, eval.ErrVacuous):
		return envelope.Fail(envelope.ExitSpecUnrunnable, "eval_vacuous", err.Error())
	case errors.Is(err, eval.ErrSolutionRed):
		return envelope.Fail(envelope.ExitChecksRed, "checks_red", err.Error())
	}
	var su *verdict.SpecUnrunnableError
	if errors.As(err, &su) {
		return envelope.Fail(envelope.ExitSpecUnrunnable, "spec_unrunnable", err.Error())
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}

func runEvalList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "repository the definitions live in")
	if err := fs.Parse(args); err != nil || *repo == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "eval list requires --repo <dir>"), stdout, stderr)
	}
	defs, err := eval.Load(*repo)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	rows := []map[string]any{}
	for _, d := range defs {
		row := map[string]any{"name": d.Name, "summary": d.Summary, "tier": d.Tier, "acceptance": d.Acceptance}
		if a, err := eval.AnchorOf(*repo, d); err != nil {
			row["reviewed"] = false
			row["because"] = err.Error()
		} else {
			row["reviewed"] = true
			row["ref"] = a.Ref(d)
			row["gate"] = a.Gate()
		}
		rows = append(rows, row)
	}
	return render(envelope.OK(map[string]any{"evals": rows}), stdout, stderr)
}

func runEvalCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "repository the definition lives in")
	name := fs.String("eval", "", "the definition to check")
	timeout := fs.Duration("timeout", 0, "per-command wall-clock bound (default 10m)")
	if err := fs.Parse(args); err != nil || *repo == "" || *name == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "eval check requires --repo <dir> --eval <name> [--timeout <dur>]"), stdout, stderr)
	}
	defs, err := eval.Load(*repo)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	def, ok := eval.Find(defs, *name)
	if !ok {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no eval %q under %s", *name, eval.Root)), stdout, stderr)
	}
	anchor, err := eval.AnchorOf(*repo, def)
	if err != nil {
		return render(evalFailEnvelope(err), stdout, stderr)
	}
	rep, err := eval.Check(*repo, def, anchor, verdict.Runner{Timeout: *timeout})
	result := map[string]any{
		"eval": def.Name, "anchor": map[string]string{"commit": anchor.Commit, "pr": anchor.PR, "ref": anchor.Ref(def), "gate": anchor.Gate()},
		"commands": rep.Commands, "fixture": rep.Fixture, "solution": rep.Solution,
	}
	if err != nil {
		env := evalFailEnvelope(err)
		env.Result = result
		return render(env, stdout, stderr)
	}
	return render(envelope.OK(result), stdout, stderr)
}

// evalFlags is the transport half the ledger-touching eval verbs share:
// the loop verbs' own, minus the subject, which these derive.
func bindEvalFlags(fs *flag.FlagSet, needKey bool) (*loopFlags, func(parseErr error, narg int) *envelope.Envelope) {
	subject := ""
	f := &loopFlags{
		dir:       fs.String("ledger", "", "ledger directory"),
		remote:    fs.String("remote", "", "remote ledger repository"),
		refName:   fs.String("ref", "refs/seed/ledger", "remote ledger ref"),
		stateDir:  fs.String("state", "", "client state dir for the persisted verified head (default: user cache)"),
		keyPath:   fs.String("key", "", "OpenSSH ed25519 private key of the acting lane"),
		subject:   &subject,
		supported: fs.String("supported", "", "comma-separated supported protocol versions (default: this build's)"),
		as:        fs.String("as", "", "fingerprint the --key must have: refuses if it changed under the caller"),
	}
	usage := func(parseErr error, narg int) *envelope.Envelope {
		if parseErr == nil && (*f.dir == "") != (*f.remote == "") && (!needKey || *f.keyPath != "") && narg == 0 {
			return nil
		}
		want := "--ledger <dir> or --remote <repo> (not both)"
		if needKey {
			want += ", --key <path>"
		}
		return envelope.Fail(envelope.ExitUsage, "usage", "eval "+fs.Name()+" requires "+want+" and --repo <dir>")
	}
	return f, usage
}

// evalAppend signs one act against a fresh view and pushes it through
// the same admission every other actor crosses: local store or the
// optimistic remote loop. It returns the landed position.
func evalAppend(f *loopFlags, signer ed25519.PrivateKey, verb, subject, payload string) (int, *envelope.Envelope) {
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return -1, failEnv
	}
	defer ls.done()
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return -1, envelope.Fail(envelope.ExitUsage, "usage", err.Error())
	}
	rec, err := event.Sign(event.Event{
		V: ls.ctx.Active, TS: time.Now().UTC().Format(time.RFC3339), Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: ls.ctx.Tip,
	}, signer)
	if err != nil {
		return -1, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot sign %s: %v", verb, err))
	}
	if err := admit.Check(ls.ctx, rec); err != nil {
		return -1, stampTip(remoteFailureEnvelope(err), ls.ctx.Count)
	}
	if ls.remote != nil {
		_, res, err := ls.remote.pushDraft(verb, subject, payload, signer, fp, nil)
		if err != nil {
			return -1, remoteFailureEnvelope(err)
		}
		return res.Position, nil
	}
	pos, err := ls.store.Append(rec, ls.ctx.Resolve)
	if err != nil {
		return -1, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
	}
	return pos, nil
}

func runEvalFile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("file", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f, usage := bindEvalFlags(fs, true)
	repo := fs.String("repo", "", "repository the definition lives in")
	name := fs.String("eval", "", "the definition to file")
	tupleFlag := fs.String("tuple", "", "the configuration under re-test (a spot-check), as the strict JSON object")
	forLesson := fs.String("for-lesson", "", "the hypothesis this eval is a counter-trajectory for, <h-id>@<position>")
	carrier := fs.String("carrier", "", "the candidate revision the eval runs against, \"<path> @ <commit>\"")
	parseErr := fs.Parse(args)
	if parseErr == nil && (*forLesson == "") != (*carrier == "") {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "--for-lesson and --carrier bind an eval to a candidate together: both or neither"), stdout, stderr)
	}
	if env := usage(parseErr, fs.NArg()); env != nil || *repo == "" || *name == "" {
		if env == nil {
			env = usage(errors.New("missing"), 0)
		}
		return render(env, stdout, stderr)
	}
	var tu *tuple.Tuple
	if *tupleFlag != "" {
		t, err := tuple.Parse([]byte(*tupleFlag))
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--tuple: %v", err)), stdout, stderr)
		}
		tu = &t
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defs, err := eval.Load(*repo)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	def, ok := eval.Find(defs, *name)
	if !ok {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no eval %q under %s", *name, eval.Root)), stdout, stderr)
	}
	anchor, err := eval.AnchorOf(*repo, def)
	if err != nil {
		return render(evalFailEnvelope(err), stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	prior := eval.Prior(ls.ctx.Lifecycle, def.Name, tu)
	ls.done()
	filing, err := eval.FileBound(def, anchor, tu, prior, *forLesson, *carrier)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	var positions []int
	for _, step := range []struct{ verb, payload string }{{"intent.filed", string(filing.Intent)}, {"contract.specified", string(filing.Spec)}} {
		pos, failEnv := evalAppend(f, signer, step.verb, filing.Subject, step.payload)
		if failEnv != nil {
			failEnv.Result = map[string]any{"subject": filing.Subject, "appended": positions, "refused": step.verb}
			return render(failEnv, stdout, stderr)
		}
		positions = append(positions, pos)
	}
	return render(envelope.OK(map[string]any{
		"subject": filing.Subject, "eval": def.Name, "ref": anchor.Ref(def), "gate": anchor.Gate(), "appended": positions,
	}), stdout, stderr)
}

// runEvalDue is status (report) and act (perform): one derivation,
// two verbs, so what act does is exactly what status said was owed.
func runEvalDue(args []string, perform bool, stdout, stderr io.Writer) int {
	name := "status"
	if perform {
		name = "act"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f, usage := bindEvalFlags(fs, perform)
	repo := fs.String("repo", "", "repository the evals and submissions live in")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	asOf := fs.String("as-of", "", "declared instant (RFC3339; defaults to now)")
	after := fs.Duration("spot-check-after", 168*time.Hour, "re-test a qualification older than this (0 disables)")
	goldDir := fs.String("gold", "", "directory of gold scorecards, <name>.json per calibration definition, held outside the tree")
	timeout := fs.Duration("timeout", 0, "per-command wall-clock bound for receipt recomputation (default 10m)")
	parseErr := fs.Parse(args)
	if env := usage(parseErr, fs.NArg()); env != nil || *repo == "" {
		if env == nil {
			env = usage(errors.New("missing"), 0)
		}
		return render(env, stdout, stderr)
	}
	now := time.Now().UTC()
	if *asOf != "" {
		parsed, err := time.Parse(time.RFC3339, *asOf)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--as-of %q is not RFC3339: %v", *asOf, err)), stdout, stderr)
		}
		now = parsed.UTC()
	}
	var signer ed25519.PrivateKey
	if perform {
		var env *envelope.Envelope
		if signer, env = loopSigner(*f.keyPath, *f.as); env != nil {
			return render(env, stdout, stderr)
		}
	}
	defs, err := eval.Load(*repo)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	gold, err := eval.LoadGold(*goldDir, defs)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--gold: %v", err)), stdout, stderr)
	}
	ls, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	rep := eval.Due(eval.Inputs{
		Ctx: ls.ctx, Ring: ls.ctx.Keyring, Store: artifact.Open(artifactsDir(*artifacts, *repo)), Repo: *repo,
		Now: now, After: *after, Evals: defs, Timeout: *timeout, Gold: gold,
	})
	count := ls.ctx.Count
	ls.done()
	result := map[string]any{"as_of": now.Format(time.RFC3339), "notes": rep.Notes}
	if !perform {
		result["owed"] = rep.Acts
		return render(stampTip(envelope.OK(result), count), stdout, stderr)
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	var performed, refused, owed []map[string]any
	// The capability check reads the view the derivation read; an act
	// the key's grants do not admit is reported as owed by its lane,
	// never attempted, so nothing is signed that the boundary would
	// refuse at the door.
	ringLs, failEnv := openLoopSession(f)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ring := ringLs.ctx.Keyring
	ringLs.done()
	// A key holding no eval lane at all (neither supervise, dispatch
	// nor operator) is not "the other lane": every act refuses
	// out_of_grant, reported, nothing signed, nothing retried (AC7).
	holdsALane := ring != nil && ring.HasAnyCapability(fp, []string{eval.LaneSupervise, eval.LaneDispatch, keyring.CapOperator})
	for _, act := range rep.Acts {
		if ring == nil || !ring.HasAnyCapability(fp, keyring.AcceptedCapabilities(act.Verb)) {
			if !holdsALane {
				refused = append(refused, map[string]any{"kind": act.Kind, "verb": act.Verb, "subject": act.Subject,
					"code": "out_of_grant", "message": fmt.Sprintf("%s accepts %v; this key holds none of the eval lanes", act.Verb, keyring.AcceptedCapabilities(act.Verb))})
				continue
			}
			owed = append(owed, map[string]any{"kind": act.Kind, "verb": act.Verb, "subject": act.Subject, "lane": act.Lane, "because": act.Because})
			continue
		}
		pos, failEnv := evalAppend(f, signer, act.Verb, act.Subject, act.Payload)
		if failEnv != nil {
			refused = append(refused, map[string]any{"kind": act.Kind, "verb": act.Verb, "subject": act.Subject,
				"code": failEnv.Error.Code, "message": failEnv.Error.Message})
			continue
		}
		count = pos + 1
		performed = append(performed, map[string]any{"kind": act.Kind, "verb": act.Verb, "subject": act.Subject, "position": fmt.Sprintf("%d", pos), "because": act.Because})
	}
	if performed == nil {
		performed = []map[string]any{}
	}
	if refused == nil {
		refused = []map[string]any{}
	}
	if owed == nil {
		owed = []map[string]any{}
	}
	result["performed"], result["refused"], result["owed"] = performed, refused, owed
	result["actor"] = fp
	if len(refused) > 0 && !holdsALane {
		env := envelope.Fail(envelope.ExitOutOfGrant, "out_of_grant",
			fmt.Sprintf("%d owed acts, none of which this key's grants admit: seed eval act performs the subset the supervise, dispatch or operator lanes own, and this key holds none", len(refused)))
		env.Result = result
		return render(stampTip(env, count), stdout, stderr)
	}
	if len(refused) > 0 {
		env := envelope.Fail(envelope.ExitChainInvalid, "chain_invalid",
			fmt.Sprintf("%d of %d owed acts refused at the boundary; see refused", len(refused), len(rep.Acts)))
		env.Result = result
		return render(stampTip(env, count), stdout, stderr)
	}
	return render(stampTip(envelope.OK(result), count), stdout, stderr)
}
