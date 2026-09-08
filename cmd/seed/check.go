// The forge observation's verb (plans/os-0cd18799.md D5;
// spec/observations-forge.md): `seed check observe` records what
// the forge says about the submission under review, on both postures,
// through the loop verbs' transport, the merge.go shape. The
// observation is read from the forge by the same adapters merge
// observe uses, or given by hand; either way it is refused before
// signing when it says nothing the standing observation does not,
// because an unchanged poll appends nothing.
package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/externalfact"
	"github.com/shaunlmason/open-seed-v2/internal/maintain"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func runCheck(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"check requires a subverb: observe"), stdout, stderr)
	}
	switch args[0] {
	case "observe":
		return runCheckObserve(args[1:], stdout, stderr)
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown check subverb %q — the forge observation has one verb: observe", args[0])), stdout, stderr)
	}
}

// runCheckObserve reads the forge's word on the pull request and
// appends it as check.observed.
func runCheckObserve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check observe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	pr := fs.String("pr", "", "the pull request observed (pr/<n> or <n>; default: the one the submission names)")
	forgeKind := fs.String("forge", "", "read the observation from the forge: snapshot | github | forgejo")
	github := fs.String("github", "", "owner/name of the repository (github or forgejo forge)")
	api := fs.String("api", "", "forge API base URL")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "environment variable holding the forge token")
	snapshot := fs.String("snapshot", "", "pull-request snapshot file (snapshot forge)")
	head := fs.String("head", "", "the head observed, given by hand instead of --forge")
	checks := fs.String("checks", "", "green | red | pending, given by hand")
	threads := fs.Int("threads", -1, "unresolved review threads, given by hand (omit where the forge cannot say)")
	review := fs.String("review", "none", "approved | changes_requested | none, given by hand")
	parseErr := fs.Parse(args)
	missing := ""
	if *forgeKind == "" && (*head == "" || *checks == "") {
		missing = "and either --forge <kind> or --head <sha> --checks <state> [--threads <n>] [--review <state>] (the forge fact, recorded as observed)"
	}
	if env := f.usage("check observe", parseErr, fs.NArg(), missing); env != nil {
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
	// The pull request defaults to the one the submission named, so
	// the pass and the observer need not repeat what the ledger holds.
	if *pr == "" {
		if s, ok := ls.ctx.Lifecycle.State(subject); ok && s.Submission != nil && s.Submission.PR != "" {
			*pr = s.Submission.PR
		} else {
			return render(envelope.Fail(envelope.ExitUsage, "usage",
				"--pr <ref> is required: the submission under review names no pull request"), stdout, stderr)
		}
	}
	var obs externalfact.Observation
	if *forgeKind != "" {
		reader, env := mergeObserver(*forgeKind, *github, *api, *tokenEnv, *snapshot)
		if env != nil {
			return render(env, stdout, stderr)
		}
		read, err := reader.Checks(*pr)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("reading %s from the %s forge: %v", *pr, *forgeKind, err)), stdout, stderr)
		}
		obs = read
	} else {
		obs = externalfact.Observation{Head: *head, Checks: *checks, Review: *review}
		if *threads >= 0 {
			n := *threads
			obs.UnresolvedThreads = &n
		}
	}
	payload, err := maintain.ObservationPayload(*pr, obs)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	// The derivation re-runs against each refreshed view: the
	// observation itself is the caller's, so nothing is re-derived,
	// but an unchanged observation refuses before signing, naming the
	// standing one (D1), and a refresh that lands the same fact
	// refuses the same way.
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		if ctx.Lifecycle != nil {
			if s, ok := ctx.Lifecycle.State(subject); ok && s.Observation != nil {
				fact := transition.CheckFact{PR: *pr, Head: obs.Head, Checks: obs.Checks, Threads: obs.UnresolvedThreads, Review: obs.Review}
				if s.Observation.Same(fact) {
					return nil, envelope.Fail(envelope.ExitInvalidTransition, "unchanged",
						fmt.Sprintf("the observation at position %d already says exactly this about %s (checks %s, review %s) — an unchanged poll appends nothing", s.Observation.Pos, *pr, obs.Checks, obs.Review))
				}
			}
		}
		return payload, nil
	}
	if _, env := derive(ls.ctx); env != nil {
		return render(env, stdout, stderr)
	}
	return ls.commit(f, loopAct{verb: transition.CheckObservedVerb, payload: payload, derive: derive,
		resultAt: terse(subject)}, signer, stdout, stderr)
}
