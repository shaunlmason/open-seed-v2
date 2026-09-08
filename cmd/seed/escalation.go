// The escalation channel's acts (plans/os-f781f0da.md; spec
// spec/escalation.md). Two verbs and two flags, following the
// loop verbs' two principles: derive every argument the system already
// holds, and refuse before signing with the boundary's own account.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/escalation"
)

// questionFlags binds --question and the repeatable --option. They sit
// on the raise and on claim park, which is the ONE exit that may also
// ask something: from in_progress an escalation rides the park,
// because nothing new may leave that state.
type questionFlags struct {
	question *string
	options  *optionList
}

// optionList collects --option id=text in order. Order is preserved
// because a human reads the choices in the order the lane wrote them.
type optionList []escalation.Option

func (o *optionList) String() string { return fmt.Sprintf("%d option(s)", len(*o)) }

func (o *optionList) Set(v string) error {
	id, choice, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("option %q is not id=text — the answer cites an option by id", v)
	}
	*o = append(*o, escalation.Option{ID: strings.TrimSpace(id), Choice: strings.TrimSpace(choice)})
	return nil
}

func bindQuestionFlags(fs *flag.FlagSet) *questionFlags {
	opts := &optionList{}
	fs.Var(opts, "option", "an answer the raiser offers, id=text (repeatable; at least two)")
	return &questionFlags{
		question: fs.String("question", "", "the one question a human gate is being asked"),
		options:  opts,
	}
}

// present reports whether the caller asked anything at all.
func (q *questionFlags) present() bool { return *q.question != "" || len(*q.options) > 0 }

// body renders the escalation object, validating it HERE so a caller
// is told what is wrong before a session is opened, the packet's
// at-the-door posture. Returning early beats signing a record the
// boundary would refuse for a reason the caller could have been given
// sooner.
func (q *questionFlags) body(subject string) (json.RawMessage, *envelope.Envelope) {
	e := escalation.Escalation{Question: *q.question, Options: []escalation.Option(*q.options)}
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	if _, err := escalation.Parse(subject, raw); err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", err.Error())
	}
	return raw, nil
}

// runEscalation dispatches the noun's one subverb.
func runEscalation(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "raise" {
		got := ""
		if len(args) > 0 {
			got = fmt.Sprintf(" (got %q)", args[0])
		}
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"escalation requires the subverb: raise"+got), stdout, stderr)
	}
	return runEscalationRaise(args[1:], stdout, stderr)
}

// runEscalationRaise raises blocked(needs-you) from ready or review.
// It carries a packet like every escalation, and no fence: outside
// in_progress there is no active fence, and citing one refuses.
func runEscalationRaise(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("escalation raise", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	q := bindQuestionFlags(fs)
	packetPath := fs.String("packet", "", "four-part handoff packet (JSON file)")
	base := fs.String("base", "", "resume range <merge-base>..<head>, when the packet does not name it")
	repo := fs.String("repo", "", "repository the range is derived from, when neither the packet nor --base names it")
	parseErr := fs.Parse(args)
	missing := ""
	if *packetPath == "" {
		missing = "and --packet <file> (an escalation carries the packet, the question and the minimal decision)"
	}
	if env := f.usage("escalation raise", parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	question, env := q.body(*f.subject)
	if env != nil {
		return render(env, stdout, stderr)
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	body, env := loopPacket(*packetPath, *base, *repo, *f.subject)
	if env != nil {
		return render(env, stdout, stderr)
	}
	payload, err := json.Marshal(map[string]json.RawMessage{
		"packet":       body,
		escalation.Key: question,
	})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	subject := *f.subject
	return ls.commit(f, loopAct{verb: escalation.RaiseVerb, payload: payload, resultAt: func(pos int) map[string]any {
		// The response names the position an answer must cite, the
		// reserve's precedent: a caller should not have to read a
		// projection to learn the number its own act established.
		return map[string]any{"subject": subject, "escalation": fmt.Sprintf("%d", pos)}
	}}, signer, stdout, stderr)
}

// runDecision dispatches the noun's one subverb.
func runDecision(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "record" {
		got := ""
		if len(args) > 0 {
			got = fmt.Sprintf(" (got %q)", args[0])
		}
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"decision requires the subverb: record"+got), stdout, stderr)
	}
	return runDecisionRecord(args[1:], stdout, stderr)
}

// runDecisionRecord answers a standing question. The cited position is
// DERIVED from the fold rather than asked for: a value the boundary
// would refuse is not a choice the caller is being offered.
func runDecisionRecord(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("decision record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	choice := fs.String("choice", "", "the id of the option chosen: a judgment, never a derivation")
	because := fs.String("because", "", "one sentence of reasoning (optional)")
	parseErr := fs.Parse(args)
	missing := ""
	if *choice == "" {
		missing = "and --choice <id> (the answer cites an option by id)"
	}
	if env := f.usage("decision record", parseErr, fs.NArg(), missing); env != nil {
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
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		return decisionPayload(ctx, *f.subject, *choice, *because)
	}
	payload, env := derive(ls.ctx)
	if env != nil {
		return render(env, stdout, stderr)
	}
	// The answer names the RESOLUTION LATENCY it closes: the charter
	// requires it tracked, and the chain makes it derivable
	// (answer.ts - raise.ts) but nothing else surfaces it. Reporting
	// it here puts the metric where the fact is, needs no new
	// surface, and keeps the derivation a live read — the raise's ts
	// comes from the fold, the instant from this client's clock, and
	// nothing is stored (spec/escalation.md).
	subject := *f.subject
	waited := waitedSince(ls.ctx, subject, time.Now().UTC())
	return ls.commit(f, loopAct{verb: escalation.AnswerVerb, payload: payload, derive: derive,
		resultAt: func(int) map[string]any {
			out := map[string]any{"subject": subject}
			if waited >= 0 {
				out["resolved_after_seconds"] = fmt.Sprintf("%d", waited)
			}
			return out
		}}, signer, stdout, stderr)
}

// waitedSince is the resolution latency in whole seconds, or -1 when
// no question stands or its timestamp is unreadable. A negative
// interval (a clock behind the raise's) reports 0 rather than a
// nonsense number: the metric is an elapsed wait, never a direction.
func waitedSince(ctx *admit.Context, subject string, now time.Time) int64 {
	if ctx.Lifecycle == nil {
		return -1
	}
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Escalation == nil {
		return -1
	}
	raised, err := time.Parse(time.RFC3339, s.Escalation.TS)
	if err != nil {
		return -1
	}
	if d := int64(now.Sub(raised).Seconds()); d > 0 {
		return d
	}
	return 0
}

// decisionPayload derives the citation and refuses where a derivation
// cannot be made, naming what would establish it — the loop verbs'
// rule that a missing fact refuses HERE rather than at the boundary.
func decisionPayload(ctx *admit.Context, subject, choice, because string) ([]byte, *envelope.Envelope) {
	if ctx.Lifecycle == nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", "no lifecycle view to derive the standing escalation from")
	}
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Escalation == nil {
		return nil, envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf(
			"no question stands on %s — `seed escalation raise --subject %s --question <q> --option a=<…> --option b=<…>` establishes one",
			subject, subject))
	}
	q := &escalation.Escalation{Question: s.Escalation.Question, Options: s.Escalation.Options}
	if !q.Offers(choice) {
		// Refusing to pick is the point: answering outside the set is
		// a new decision, not this one.
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf(
			"%q is not offered by the question at position %d (%s) — it offers %s",
			choice, s.Escalation.Pos, s.Escalation.Question, strings.Join(q.IDs(), ", ")))
	}
	out := map[string]json.RawMessage{
		escalation.Key: json.RawMessage(strconv.Quote(fmt.Sprintf("%d", s.Escalation.Pos))),
		"choice":       json.RawMessage(strconv.Quote(choice)),
	}
	if strings.TrimSpace(because) != "" {
		out["because"] = json.RawMessage(strconv.Quote(because))
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	return payload, nil
}
