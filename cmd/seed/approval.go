package main

// The per-verb approval's acts (plans/os-5781a026.md D6; spec/
// protocol.md "Per-verb approval"): `approval request` files the
// request an actor's governed act waits on, `approval grant` and
// `approval deny` are the operator's answers. Loop verbs in the shared
// transport shape, following the two principles: derive every
// argument the system already holds (the actor defaults to the
// requesting key, the request to the oldest open one on the subject),
// and refuse before signing with the boundary's own account.

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func runApproval(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "approval requires a subverb: request, grant, deny"), stdout, stderr)
	}
	switch args[0] {
	case "request":
		return runApprovalRequest(args[1:], stdout, stderr)
	case "grant":
		return runApprovalAnswer(approval.GrantedVerb, args[1:], stdout, stderr)
	case "deny":
		return runApprovalAnswer(approval.DeniedVerb, args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown approval subverb %q — request, grant, deny", args[0])), stdout, stderr)
}

// runApprovalRequest appends approval.requested on the contract the
// act concerns, or on system. The actor named defaults to the
// requesting key: in the loop's case the lane asks for itself, and a
// lane asking on another's behalf names it with --for.
func runApprovalRequest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approval request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	verb := fs.String("verb", "", "the governed catalog verb the act would perform")
	forActor := fs.String("for", "", "fingerprint of the key that will act (default: the requesting key)")
	reason := fs.String("reason", "", fmt.Sprintf("why, in one line of at most %d bytes", approval.MaxReasonBytes))
	parseErr := fs.Parse(args)
	missing := ""
	if *verb == "" || *reason == "" {
		missing = "and --verb <catalog verb>, --reason <one line> (--for <fingerprint> names another key's act; --subject is the contract the act concerns, or system)"
	}
	if env := f.usage("approval request", parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	actor := *forActor
	if actor == "" {
		fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
		}
		actor = fp
	}
	payload, err := approval.RenderRequested(approval.Requested{Verb: *verb, Actor: actor, Reason: *reason})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	// The shape is judged HERE before a session is opened, the
	// packet's at-the-door posture: a request the boundary would
	// refuse for its shape is refused sooner.
	if _, err := approval.ParseRequested(*f.subject, payload); err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	subject, governed := *f.subject, *verb
	return ls.commit(f, loopAct{verb: approval.RequestedVerb, payload: payload, resultAt: func(pos int) map[string]any {
		// The response names the position an answer must cite, the
		// escalation's precedent.
		return map[string]any{"subject": subject, "approval": strconv.Itoa(pos), "verb": governed, "actor": actor}
	}}, signer, stdout, stderr)
}

// runApprovalAnswer appends approval.granted or approval.denied on the
// request's subject. The request is DERIVED from the fold rather than
// asked for: the oldest open one on the subject, refused as ambiguous
// when several stand and --request names none.
func runApprovalAnswer(verb string, args []string, stdout, stderr io.Writer) int {
	name := "approval grant"
	if verb == approval.DeniedVerb {
		name = "approval deny"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	req := fs.String("request", "", "the request's chain position (default: the oldest open request on the subject)")
	reason := fs.String("reason", "", "with deny: why, in one line")
	parseErr := fs.Parse(args)
	missing := ""
	if verb == approval.DeniedVerb && *reason == "" {
		missing = "and --reason <one line> (a denial says why; --request <position> picks among several open requests)"
	}
	if verb == approval.GrantedVerb && *reason != "" {
		missing = "and no --reason (a grant carries the request and nothing else)"
	}
	if env := f.usage(name, parseErr, fs.NArg(), missing); env != nil {
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
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		return approvalAnswerPayload(ctx, verb, subject, *req, *reason)
	}
	payload, env := derive(ls.ctx)
	if env != nil {
		return render(env, stdout, stderr)
	}
	cited := *req
	if cited == "" {
		var a approval.Answer
		_ = json.Unmarshal(payload, &a)
		cited = a.Request
	}
	key := "granted"
	if verb == approval.DeniedVerb {
		key = "denied"
	}
	return ls.commit(f, loopAct{verb: verb, payload: payload, derive: derive, resultAt: func(pos int) map[string]any {
		return map[string]any{"subject": subject, "request": cited, key: strconv.Itoa(pos)}
	}}, signer, stdout, stderr)
}

// approvalAnswerPayload derives the citation and refuses where a
// derivation cannot be made, naming what would establish it: no open
// request is not_found (the request to file), several with none named
// is a choice the caller must make, and a named one that does not
// stand open is not_found naming the ones that do.
func approvalAnswerPayload(ctx *admit.Context, verb, subject, req, reason string) ([]byte, *envelope.Envelope) {
	if ctx.Lifecycle == nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", "no lifecycle view to derive the open request from")
	}
	pending := ctx.Lifecycle.PendingApprovals(subject)
	if len(pending) == 0 {
		return nil, envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf(
			"no approval request stands open on %s — `seed approval request --subject %s --verb <verb> --reason <why>` files one",
			subject, subject))
	}
	var chosen transition.ApprovalFact
	if req == "" {
		if len(pending) > 1 {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf(
				"%d approval requests stand open on %s (%s): --request <position> names the one this answers",
				len(pending), subject, describeApprovals(pending)))
		}
		chosen = pending[0]
	} else {
		pos, err := strconv.Atoi(strings.TrimSpace(req))
		if err != nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--request %q is not a chain position", req))
		}
		found := false
		for _, a := range pending {
			if a.Pos == pos {
				chosen, found = a, true
				break
			}
		}
		if !found {
			return nil, envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf(
				"no approval request at position %d stands open on %s — open: %s", pos, subject, describeApprovals(pending)))
		}
	}
	var payload []byte
	var err error
	if verb == approval.GrantedVerb {
		payload, err = approval.RenderGranted(chosen.Pos)
	} else {
		payload, err = approval.RenderDenied(chosen.Pos, reason)
	}
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	if _, _, err := approval.ParseAnswer(verb, subject, payload); err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", err.Error())
	}
	return payload, nil
}

func describeApprovals(pending []transition.ApprovalFact) string {
	parts := make([]string, 0, len(pending))
	for _, a := range pending {
		parts = append(parts, fmt.Sprintf("%d (%s by %s)", a.Pos, a.Verb, a.Actor))
	}
	return strings.Join(parts, ", ")
}
