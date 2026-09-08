package main

// The lane read surface (plans/os-cf1c9688.md): list the lanes, show
// one resolved from its ordered fragments, and validate the set
// against the tables that already enforce the system's rules.
//
// Read-only and idempotent like every other read: it opens no ledger,
// mutates nothing, and journals no attempt, because a read is not an
// admission-boundary attempt. It carries no position stamp either — a
// resolved role derives from checked-in files rather than from the
// ledger, so there is no position it could honestly cite.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
)

const defaultLanesDir = "lanes"

func runLane(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "lane requires a subverb: list, show, or validate"), stdout, stderr)
	}
	switch args[0] {
	case "list":
		return runLaneList(args[1:], stdout, stderr)
	case "show":
		return runLaneShow(args[1:], stdout, stderr)
	case "validate":
		return runLaneValidate(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown lane subverb %q — list, show, or validate", args[0])), stdout, stderr)
}

// laneDir binds the shared --lanes flag.
func laneDir(fs *flag.FlagSet) *string {
	return fs.String("lanes", defaultLanesDir, "directory of lane manifests")
}

func runLaneList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lane list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := laneDir(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "lane list takes [--lanes <dir>]"), stdout, stderr)
	}
	ms, err := lane.Load(*dir)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	rows := []map[string]any{}
	for _, m := range ms {
		rows = append(rows, map[string]any{
			"lane":      m.Lane,
			"kind":      m.Kind,
			"summary":   m.Summary,
			"grants":    m.Grants,
			"fragments": fmt.Sprintf("%d", len(m.Fragments)),
		})
	}
	return render(envelope.OK(map[string]any{"lanes": rows}), stdout, stderr)
}

func runLaneShow(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lane show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := laneDir(fs)
	// The name may sit before its flags, which is how anyone types it.
	// Go's flag package stops at the first non-flag argument, so the
	// operand is lifted out before parsing rather than after.
	name, rest := splitOperand(args)
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || name == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "lane show requires <name> [--lanes <dir>]"), stdout, stderr)
	}
	ms, err := lane.Load(*dir)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	for _, m := range ms {
		if m.Lane != name {
			continue
		}
		body, err := lane.Resolve(*dir, m)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
		return render(envelope.OK(map[string]any{
			"lane":          m.Lane,
			"grants":        m.Grants,
			"orients_from":  m.OrientsFrom,
			"acts_through":  m.ActsThrough,
			"liveness_from": m.LivenessFrom,
			"inbox":         m.Inbox,
			"fragments":     m.Fragments,
			"resolved":      body,
		}), stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no lane %q in %s", name, *dir)), stdout, stderr)
}

func runLaneValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lane validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := laneDir(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "lane validate takes [--lanes <dir>]"), stdout, stderr)
	}
	ms, err := lane.Load(*dir)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	findings := lane.Validate(*dir, ms)
	rows := []map[string]any{}
	for _, f := range findings {
		rows = append(rows, map[string]any{"lane": f.Lane, "field": f.Field, "message": f.Message})
	}
	if len(findings) > 0 {
		env := envelope.Fail(envelope.ExitLaneInvalid, "lane_invalid",
			fmt.Sprintf("%d lane finding(s): %s", len(findings), findings[0]))
		env.Result = map[string]any{"findings": rows, "lanes": fmt.Sprintf("%d", countKind(ms, lane.KindLane)), "roles": fmt.Sprintf("%d", countKind(ms, lane.KindRole))}
		return render(env, stdout, stderr)
	}
	return render(envelope.OK(map[string]any{"lanes": fmt.Sprintf("%d", countKind(ms, lane.KindLane)), "roles": fmt.Sprintf("%d", countKind(ms, lane.KindRole)), "findings": rows}), stdout, stderr)
}

// countKind is the validate read's split: the charter's six lanes and
// its non-loop roles are reported apart, because "eight manifests" is
// the wrong answer to "how many lanes" (plans/os-d6a52784.md D1).
func countKind(ms []lane.Manifest, kind string) int {
	n := 0
	for _, m := range ms {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

// splitOperand lifts the first non-flag argument out of args, so a
// positional may appear anywhere among the flags.
func splitOperand(args []string) (string, []string) {
	operand := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			// A flag written as two tokens consumes the next one.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		if operand == "" {
			operand = a
			continue
		}
		rest = append(rest, a)
	}
	return operand, rest
}
