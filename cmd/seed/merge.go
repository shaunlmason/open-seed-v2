// The merge chain's two verbs (plans/os-6a08b166.md D6.5;
// spec/reconciliation.md): `seed merge request` and `seed merge
// observe`, on BOTH postures.
//
// Before this they had no CLI verb at all. They existed only through
// `ledger append`, which is the raw dev seam and runs no rules, so a
// lane could not drive the chain's terminal steps through an admitted
// surface and a fixture using them asserted nothing. A charter row
// asking both modes to "run the full loop" cannot be met through verbs
// that do not exist.
//
// Both reuse the loop verbs' transport — one admission check, one
// optimistic push loop, one refusal shape — without joining the loop's
// act catalog: `loopverb` names the acts a LANE declares in its
// manifest, and the merge chain is the observer's and operator's work,
// not a claim-holder's.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/externalfact"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func runMerge(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"merge requires a subverb: request or observe"), stdout, stderr)
	}
	switch args[0] {
	case "request":
		return runMergeRequest(args[1:], stdout, stderr)
	case "observe":
		return runMergeObserve(args[1:], stdout, stderr)
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown merge subverb %q — the chain has two steps: request, observe", args[0])), stdout, stderr)
	}
}

// runMergeRequest cites the standing pass verdict (or an operator
// override) and asks for the merge.
//
// The citation is DERIVED by default, and that is what makes the
// recheck matter: the payload names a chain POSITION, so a tip that
// moves between drafting and landing can leave the act pointing at a
// different verdict than the one the caller read. `recheckDerivation`
// re-derives against each refreshed view and REFUSES on a change
// rather than re-pointing, because a different citation is a different
// decision, not a better argument (plans/os-9b3f3ef3.md D1).
func runMergeRequest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	override := fs.String("override", "", "cite an operator override at this position instead of the verdict")
	parseErr := fs.Parse(args)
	if env := f.usage("merge request", parseErr, fs.NArg(), ""); env != nil {
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

	subject := *f.subject
	var derive func(ctx *admit.Context) ([]byte, *envelope.Envelope)
	if *override != "" {
		// An explicitly cited override is the caller's own value, not
		// a derived one: nothing recomputes it, so nothing can
		// diverge, and derive stays nil.
		if _, err := strconv.Atoi(*override); err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage",
				fmt.Sprintf("--override %q is not a chain position", *override)), stdout, stderr)
		}
	} else {
		derive = func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
			return verdictCitation(ctx, subject)
		}
	}
	var payload []byte
	if derive != nil {
		payload, env = derive(ls.ctx)
		if env != nil {
			return render(env, stdout, stderr)
		}
	} else {
		payload, _ = json.Marshal(map[string]string{"override": *override})
	}
	return ls.commit(f, loopAct{verb: transition.MergeRequestedVerb, payload: payload, derive: derive,
		resultAt: terse(subject)}, signer, stdout, stderr)
}

// verdictCitation reads the standing verdict's position out of the
// fold. It refuses rather than guessing: a request that cited nothing
// would be refused at the boundary anyway, and refusing here says
// which of the two reasons applies.
func verdictCitation(ctx *admit.Context, subject string) ([]byte, *envelope.Envelope) {
	if ctx == nil || ctx.Lifecycle == nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", "no lifecycle view to derive the verdict citation from")
	}
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Verdict == nil {
		return nil, envelope.Fail(envelope.ExitNotFound, "not_found",
			fmt.Sprintf("no verdict stands on %s — the merge chain starts at verdict.rendered(pass), and a request cites the verdict it merges", subject))
	}
	b, err := json.Marshal(map[string]string{"verdict": strconv.Itoa(s.Verdict.Pos)})
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	return b, nil
}

// runMergeObserve records the forge fact: the merge landed, at this
// commit, through this pull request.
//
// Its payload carries NO derived citation — `{merged, pr}` are the
// caller's own observations of the forge, and the request it follows
// is checked by the boundary against the fold rather than named in the
// payload. So `derive` is nil and there is nothing for the recheck to
// guard, which is worth stating because the plan predicted otherwise:
// it named "the observation's cited request" as a third derived value,
// and the rule cites no request at all.
func runMergeObserve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge observe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	merged := fs.String("merged", "", "the merge commit observed on the target (or filled from --forge)")
	pr := fs.String("pr", "", "the pull request the merge landed through")
	forgeKind := fs.String("forge", "", "fill --merged from the forge: snapshot | github | forgejo")
	github := fs.String("github", "", "owner/name of the repository (github or forgejo forge)")
	api := fs.String("api", "", "forge API base URL")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "environment variable holding the forge token")
	snapshot := fs.String("snapshot", "", "pull-request snapshot file (snapshot forge)")
	parseErr := fs.Parse(args)
	missing := ""
	if *pr == "" || (*merged == "" && *forgeKind == "") {
		missing = "and --pr <ref> with either --merged <sha> or --forge <kind> (the forge fact, recorded as observed)"
	}
	if env := f.usage("merge observe", parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	if *merged == "" && *forgeKind != "" {
		obs, env := mergeObserver(*forgeKind, *github, *api, *tokenEnv, *snapshot)
		if env != nil {
			return render(env, stdout, stderr)
		}
		sha, isMerged, err := obs.Merged(*pr)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("reading %s from the %s forge: %v", *pr, *forgeKind, err)), stdout, stderr)
		}
		if !isMerged {
			return render(envelope.Fail(envelope.ExitInvalidTransition, "not_merged", fmt.Sprintf("%s is not merged on the forge: merge.observed records a merge, not an intention", *pr)), stdout, stderr)
		}
		*merged = sha
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
	payload, err := json.Marshal(map[string]string{"merged": *merged, "pr": *pr})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	return ls.commit(f, loopAct{verb: transition.MergeObservedVerb, payload: payload,
		resultAt: terse(*f.subject)}, signer, stdout, stderr)
}

// mergeObserver opens the read-only source merge observe, check
// observe and the maintenance pass fill forge facts from
// (plans/os-ad610334.md D4; plans/os-b45c308d.md D6): the observation
// component, which holds no mutating forge client, its token read
// from the environment and never a flag.
func mergeObserver(kind, github, api, tokenEnv, snapshot string) (externalfact.Source, *envelope.Envelope) {
	env := tokenEnv
	if kind == "forgejo" && env == "GITHUB_TOKEN" {
		env = "FORGEJO_TOKEN"
	}
	src, err := externalfact.Open(kind, github, api, env, snapshot)
	var missing *externalfact.NoCredential
	switch {
	case err == nil:
		return src, nil
	case errors.As(err, &missing):
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	default:
		return nil, envelope.Fail(envelope.ExitUsage, "usage", err.Error())
	}
}
