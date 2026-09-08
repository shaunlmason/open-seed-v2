package main

import (
	"fmt"
	"io"

	"github.com/shaunlmason/open-seed-v2/cmd/seed/registry"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// catalog is the verb table (plans/os-b55e5647.md D2), built over the
// stdin the one stdin-reading verb consumes: every top-level
// verb the CLI dispatches, its subverbs in the words its usage line
// speaks, and its run function. The machine protocol's method set is
// derived from this table and nothing else; a drill holds each
// group's Subs to the CLI's own usage vocabulary in both directions,
// so a subverb added to a dispatcher without a row here, or a row
// without a dispatcher case, fails the gate.
func catalog(stdin io.Reader) *registry.Registry {
	return registry.New(
		registry.Group{Name: "version", Run: runVersion},
		registry.Group{Name: "init", Run: runInit},
		registry.Group{Name: "ledger", Subs: []string{"verify", "append", "show", "audit"}, Run: runLedger},
		registry.Group{Name: "project", Subs: []string{"rebuild", "current", "start"}, Run: runProject},
		registry.Group{Name: "situation", Run: runSituation},
		registry.Group{Name: "obs", Subs: []string{"emit"}, Run: runObs},
		registry.Group{Name: "plan", Subs: []string{"lint", "classify", "propose", "approve"}, Run: func(args []string, stdout, stderr io.Writer) int {
			// plan reads a body from stdin; under the machine protocol
			// that stream is the transport's, so serve hands it an
			// empty one and the caller names a file instead.
			return runPlan(args, stdin, stdout, stderr)
		}},
		registry.Group{Name: "verdict", Subs: []string{"receipt", "render", "defer", "check", "traces"}, Run: runVerdict},
		registry.Group{Name: "seal", Subs: []string{"create", "rotate", "audit"}, Run: runSeal},
		registry.Group{Name: "artifact", Subs: []string{"erase"}, Run: runArtifact},
		registry.Group{Name: "offer", Subs: []string{"publish", "list", "wake"}, Run: runOffer},
		registry.Group{Name: "topology", Subs: []string{"depend", "undepend", "parent", "align"}, Run: runTopology},
		registry.Group{Name: "budget", Subs: []string{"status", "reserve", "settle", "release"}, Run: runBudget},
		registry.Group{Name: "claim", Subs: []string{"take", "release", "park"}, Run: runClaim},
		registry.Group{Name: "escalation", Subs: []string{"raise"}, Run: runEscalation},
		registry.Group{Name: "decision", Subs: []string{"record"}, Run: runDecision},
		registry.Group{Name: "approval", Subs: []string{"request", "grant", "deny"}, Run: runApproval},
		registry.Group{Name: "submission", Subs: []string{"make"}, Run: runSubmission},
		registry.Group{Name: "merge", Subs: []string{"request", "observe"}, Run: runMerge},
		registry.Group{Name: "check", Subs: []string{"observe"}, Run: runCheck},
		registry.Group{Name: "reconcile", Run: runReconcile},
		registry.Group{Name: "maintain", Subs: []string{"run"}, Run: runMaintain},
		registry.Group{Name: "lane", Subs: []string{"list", "show", "validate"}, Run: runLane},
		registry.Group{Name: "message", Subs: []string{"read", "send"}, Run: runMessage},
		registry.Group{Name: "request", Subs: []string{"file", "answer"}, Run: runRequest},
		registry.Group{Name: "federation", Subs: []string{"report"}, Run: runFederation},
		registry.Group{Name: "boundary", Subs: []string{"card", "check", "verify", "serve", "tasks", "fetch"}, Run: runBoundary},
		registry.Group{Name: "doctor", Run: runDoctor},
		registry.Group{Name: "protections", Subs: []string{"plan", "apply"}, Run: runProtections},
		registry.Group{Name: "perf", Subs: []string{"run"}, Run: runPerf},
		registry.Group{Name: "import", Run: runImport},
		registry.Group{Name: "preseed", Subs: []string{"check"}, Run: runPreseed},
		registry.Group{Name: "run", Subs: []string{"start"}, Run: runRun},
		registry.Group{Name: "eval", Subs: []string{"list", "check", "file", "status", "act"}, Run: runEval},
		registry.Group{Name: "knowledge", Subs: []string{"deadend", "propose", "validate", "contest", "promote", "retire", "lint", "show", "search"}, Run: runKnowledge},
		registry.Group{Name: "flywheel", Subs: []string{"shapes", "draft", "propose", "repair", "observe", "status"}, Run: runFlywheel},
		registry.Group{Name: "trajectory", Subs: []string{"record", "replay"}, Run: runTrajectory},
		registry.Group{Name: "docs", Subs: []string{"generate", "check"}, Run: runDocs},
		registry.Group{Name: "simulate", Run: runSimulate},
		registry.Group{Name: "serve", Run: runServe, Transport: true},
	)
}

// runVersion prints the build's name, version and newest protocol.
func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("version takes no arguments, got %q", args)), stdout, stderr)
	}
	return render(envelope.OK(map[string]any{
		"name":     version.Name,
		"version":  version.Version,
		"protocol": version.Protocol,
	}), stdout, stderr)
}
