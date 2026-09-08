package main

// The relation verbs (plans/os-f0ae2cdf.md step 1; spec/topology.md):
// `topology depend | undepend | parent | align` append the four relation
// facts on a contract through the ordinary checked admission path, the
// loop's transport shape, never a topology side store. Each is a fact
// beside the lifecycle: it moves no state, and what it changes (what
// is claimable, what is held, what a rollup counts, what warns) is
// derived by every reader from the same fold.

import (
	"flag"
	"fmt"
	"io"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
)

func runTopology(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "topology requires a subverb: depend, undepend, parent or align"), stdout, stderr)
	}
	switch args[0] {
	case "depend":
		return runTopologyRelate(topology.DependVerb, "depend", "requires", args[1:], stdout, stderr)
	case "undepend":
		return runTopologyRelate(topology.UndependVerb, "undepend", "requires", args[1:], stdout, stderr)
	case "parent":
		return runTopologyRelate(topology.ParentVerb, "parent", "parent", args[1:], stdout, stderr)
	case "align":
		return runTopologyRelate(topology.AlignVerb, "align", "mission", args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown topology subverb %q: depend, undepend, parent or align", args[0])), stdout, stderr)
}

// runTopologyRelate appends one relation fact: the subject is the
// contract the fact is on, the one flag names its target (the
// required contract, the parent, or the mission anchor), and the
// payload is shape-checked before any session opens, so a malformed
// invocation refuses at usage naming the part.
func runTopologyRelate(verb, sub, field string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("topology "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	help := map[string]string{
		"requires": "the contract this subject requires (depend) or no longer requires (undepend)",
		"parent":   "the contract this subject belongs under; a repeat replaces the current parent",
		"mission":  "the commit-anchored mission this subject serves, \"<path> @ <commit>\"; a repeat replaces",
	}[field]
	target := fs.String(field, "", help)
	parseErr := fs.Parse(args)
	missing := ""
	if *target == "" {
		missing = fmt.Sprintf("and --%s <%s>", field, field)
	}
	if env := f.usage("topology "+sub, parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	payload := []byte(fmt.Sprintf(`{%q: %q}`, field, *target))
	if _, err := topology.Parse(verb, *f.subject, payload); err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
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
	subject, named := *f.subject, *target
	return ls.commit(f, loopAct{verb: verb, payload: payload, resultAt: func(int) map[string]any {
		return map[string]any{"subject": subject, "verb": verb, field: named}
	}}, signer, stdout, stderr)
}
