package main

// The request ingress (plans/os-48df10a2.md; spec/requests.md):
// `request file` is the one door a proposal from a projection surface,
// a dashboard or another deployment enters the ledger by, a fact that
// changes no state and grants nothing; `request answer` is the
// dispatcher's close of one, citing the intent it filed or the reason
// it declined. Both are loop verbs in the shared transport shape, and
// the boundary judges them (internal/admit's request rule).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/request"
)

func runRequest(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "request requires a subverb: file, answer"), stdout, stderr)
	}
	switch args[0] {
	case "file":
		return runRequestFile(args[1:], stdout, stderr)
	case "answer":
		return runRequestAnswer(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown request subverb %q — file, answer", args[0])), stdout, stderr)
}

// runRequestFile appends request.filed. The subject is the contract
// the proposal concerns, or `system` when it concerns none; `about`
// is derived from it, never asked for twice.
func runRequestFile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("request file", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	origin := fs.String("origin", "", "the surface or remote the proposal came from, one token")
	kind := fs.String("kind", "", "one of "+strings.Join(request.Kinds, ", "))
	reference := fs.String("reference", "", "what was proposed: a commit-anchored ref (\"path @ commit\") or an artifact digest, never a body")
	summary := fs.String("summary", "", fmt.Sprintf("one line, at most %d bytes", request.MaxSummaryBytes))
	parseErr := fs.Parse(args)
	missing := ""
	if *origin == "" || *kind == "" || *reference == "" || *summary == "" {
		missing = "and --origin, --kind, --reference, --summary (--subject is the contract the proposal concerns, or system)"
	}
	if env := f.usage("request file", parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	about := ""
	if *f.subject != request.SystemSubject {
		about = *f.subject
	}
	payload, err := request.RenderFiled(request.Filed{Origin: *origin, Kind: *kind, Reference: *reference, Summary: *summary, About: about})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
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
	return ls.commit(f, loopAct{verb: request.FiledVerb, payload: payload, resultAt: func(pos int) map[string]any {
		// The response names the position an answer must cite.
		return map[string]any{"subject": subject, "request": fmt.Sprintf("%d", pos)}
	}}, signer, stdout, stderr)
}

// runRequestAnswer appends request.answered on the request's subject.
func runRequestAnswer(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("request answer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	req := fs.String("request", "", "the request's chain position")
	outcome := fs.String("outcome", "", "one of "+strings.Join(request.Outcomes, ", "))
	intent := fs.String("intent", "", "with filed: the position of the intent.filed appended for the request")
	reason := fs.String("reason", "", "with declined: why")
	parseErr := fs.Parse(args)
	missing := ""
	if *req == "" || *outcome == "" {
		missing = "and --request <position>, --outcome filed --intent <position> | --outcome declined --reason <text>"
	}
	if env := f.usage("request answer", parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	payload, err := json.Marshal(request.Answered{Request: *req, Outcome: *outcome, Intent: *intent, Reason: *reason})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
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
	subject, cited, out := *f.subject, *req, *outcome
	return ls.commit(f, loopAct{verb: request.AnsweredVerb, payload: payload, resultAt: func(pos int) map[string]any {
		return map[string]any{"subject": subject, "request": cited, "outcome": out, "answer": fmt.Sprintf("%d", pos)}
	}}, signer, stdout, stderr)
}
