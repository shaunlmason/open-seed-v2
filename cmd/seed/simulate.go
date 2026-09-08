package main

// The simulation verb (plans/os-16e55c11.md D3; SEED-NEXT.md §II.16):
// `seed simulate` builds a throwaway deployment under the declared
// posture, files synthetic intents, and drives every lane to done
// through the real boundary with a mock executor and zero credentials —
// no forge, no model, no network beyond a local bare git remote. It
// prints the report, the per-intent fate, and the ledger audit.
//
// The seam it drives is loopVerbs{} — the same in-process CLI dispatch
// the modes fixtures use — so the simulation runs exactly the verbs an
// operator would, never a shortcut around them.

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/simulate"
)

func runSimulate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lanesDir := fs.String("lanes", "lanes", "directory of shipped lane manifests")
	intents := fs.Int("intents", 1, "number of synthetic intents to drive to done")
	seed := fs.Int64("seed", 0, "deterministic draw seed")
	days := fs.Int("days", 0, "accelerated-clock days over which the backlog arrives (0: a single instant)")
	postureName := fs.String("posture", string(posture.Cooperative), "cooperative or enforced-self-hosted")
	nowFlag := fs.String("now", "", "RFC3339 base instant expiry and the report read (default: the current wall clock); admission reads no clock")
	workDir := fs.String("work", "", "directory the throwaway deployment is built under (default: OS temp)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"simulate takes [--lanes <dir>] [--intents <n>] [--seed <s>] [--days <d>] [--posture cooperative|enforced-self-hosted] [--now <rfc3339>] [--work <dir>]"), stdout, stderr)
	}
	var enforced bool
	switch posture.Posture(*postureName) {
	case posture.Cooperative:
		enforced = false
	case posture.EnforcedSelfHosted:
		enforced = true
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown posture %q — cooperative or enforced-self-hosted", *postureName)), stdout, stderr)
	}
	var now time.Time
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", "--now must be RFC3339: "+err.Error()), stdout, stderr)
		}
		now = t
	}
	rep, err := simulate.Run(simulate.Config{
		LanesDir: *lanesDir,
		Verbs:    loopVerbs{},
		Intents:  *intents,
		Seed:     *seed,
		Now:      now,
		Enforced: enforced,
		Days:     *days,
		WorkDir:  *workDir,
	})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	results := make([]any, len(rep.Results))
	for i, r := range rep.Results {
		results[i] = map[string]any{"subject": r.Subject, "state": r.State, "done": r.Done}
	}
	out := map[string]any{
		"posture": rep.Posture,
		"intents": rep.Intents,
		"done":    rep.Done,
		"days":    rep.Days,
		"results": results,
	}
	if rep.Audit != nil {
		out["audit"] = map[string]any{
			"clean":               rep.Audit.Clean,
			"declared":            rep.Audit.Declared,
			"chain_violations":    rep.Audit.ChainViolations,
			"lost_updates":        rep.Audit.LostUpdates,
			"silent_abandonments": rep.Audit.SilentAbandonments,
			"guardrail_breaches":  rep.Audit.GuardrailBreaches,
			"unreserved_spend":    rep.Audit.UnreservedSpend,
		}
	}
	return render(envelope.OK(out), stdout, stderr)
}
