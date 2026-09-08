// The loop verbs (plans/os-7e197768.md; docs/build-plan.md
// Phase 9 item 5 part (c); spec/loop-verbs.md): the acts a
// worker lane takes — take, release, park, submit, reserve, settle,
// release — as verbs that DERIVE every argument the system already
// holds and REFUSE before signing what the tables would refuse
// after, naming what IS legal in the same breath.
//
// The principle is Phase 8's, carried from legality to construction:
// one rule set, enforcement and advertisement. seed ledger append
// remains the raw seam beneath these verbs, unchanged, and that is
// exactly why a lane should not have to reach for it: the raw seam
// signs and appends without consulting the boundary at all, so a
// lane learns its act was illegal from a chain-level refusal instead
// of from the boundary that would have explained it.

package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/escalation"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/externalfact"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// The lifecycle verbs the loop takes, read from the registry rather
// than spelled here: internal/loopverb is the one authority for which
// acts exist and what each appends, and the lane validator is its
// second consumer (plans/os-cf1c9688.md D3a). A local copy of these
// names is exactly the drift the registry exists to prevent.
const (
	claimTakenVerb     = loopverb.ClaimTakenVerb
	claimReleasedVerb  = loopverb.ClaimReleasedVerb
	claimParkedVerb    = loopverb.ClaimParkedVerb
	submissionMadeVerb = loopverb.SubmissionMadeVerb
)

// loopFlags is the transport half every loop verb shares: --ledger
// xor --remote, exactly as ledger append takes it and through the
// same client machinery, because a lane in any real posture works
// against a remote ref rather than a local directory.
type loopFlags struct {
	dir       *string
	remote    *string
	refName   *string
	stateDir  *string
	config    *string
	keyPath   *string
	subject   *string
	supported *string
	as        *string
}

func bindLoopFlags(fs *flag.FlagSet) *loopFlags {
	return &loopFlags{
		dir:       fs.String("ledger", "", "ledger directory"),
		remote:    fs.String("remote", "", "remote ledger repository: the posture a lane works in"),
		refName:   fs.String("ref", DefaultRemoteRef, "remote ledger ref"),
		stateDir:  fs.String("state", "", "client state dir for the persisted verified head (default: user cache)"),
		keyPath:   fs.String("key", "", "OpenSSH ed25519 private key of the acting lane"),
		subject:   fs.String("subject", "", "the contract acted on"),
		supported: fs.String("supported", "", "comma-separated supported protocol versions (default: this build's)"),
		config:    fs.String("config", "", "deployment declaration (default: $SEED_CONFIG, else ./seed.json when present)"),
		// The identity the caller DECLARES it is acting as. Checked
		// against the key at the signing site, so a key replaced after
		// the caller last looked cannot sign in its place
		// (plans/os-9a89245c.md).
		as: fs.String("as", "", "fingerprint the --key must have: refuses if it changed under the caller"),
	}
}

// usage refuses an invocation the transport half cannot resolve,
// naming the whole shared shape once so every loop verb reads the
// same way.
func (f *loopFlags) usage(name string, parseErr error, narg int, extra string) *envelope.Envelope {
	if parseErr == nil && (*f.dir == "") != (*f.remote == "") && *f.keyPath != "" && *f.subject != "" && narg == 0 && extra == "" {
		return nil
	}
	msg := fmt.Sprintf("%s requires --ledger or --remote (not both), --key <path> and --subject <id>", name)
	if extra != "" {
		msg += ", " + extra
	}
	return envelope.Fail(envelope.ExitUsage, "usage", msg)
}

// loopSession is the ONE authoritative view an act derives from and
// pre-flights against. Sharing a single view is the point: a fence
// or reservation read from a stale local copy would be exactly wrong
// under the contention that makes claiming online-only, so on the
// remote path the derivation and the admission check both read the
// materialized remote tip (plans/os-7e197768.md D3).
type loopSession struct {
	dir    string
	remote *remoteSession
	store  *ledger.Store
	ctx    *admit.Context
	done   func()
}

func openLoopSession(f *loopFlags) (*loopSession, *envelope.Envelope) {
	if *f.remote != "" {
		// A caller that builds its flags by hand (eval file) may not
		// bind --config; the declaration is then found by its defaults.
		config := ""
		if f.config != nil {
			config = *f.config
		}
		rs, failEnv := openRemoteSessionUnder(*f.remote, *f.refName, *f.stateDir, *f.supported, config)
		if failEnv != nil {
			return nil, failEnv
		}
		ctx, err := admit.ContextAt(rs.store, rs.aopts...)
		if err != nil {
			rs.close()
			return nil, contextEnvelope(err)
		}
		return &loopSession{remote: rs, store: rs.store, ctx: ctx, done: rs.close}, nil
	}
	store, failEnv := openStore(*f.dir)
	if failEnv != nil {
		return nil, failEnv
	}
	var aopts []admit.Option
	if *f.supported != "" {
		aopts = append(aopts, admit.WithSupportedVersions(strings.Split(*f.supported, ",")...))
	}
	if f.config != nil {
		declared, failEnv := admitOptions(*f.config, aopts...)
		if failEnv != nil {
			return nil, failEnv
		}
		aopts = declared
	}
	ctx, err := admit.ContextAt(store, aopts...)
	if err != nil {
		return nil, contextEnvelope(err)
	}
	return &loopSession{dir: *f.dir, store: store, ctx: ctx, done: func() {}}, nil
}

// contextEnvelope maps a failure to build the admission context: a
// verification failure keeps its position-stamped form, and anything
// else (a genesis that will not bootstrap, a table that will not
// load) is chain trouble.
func contextEnvelope(err error) *envelope.Envelope {
	var fail *ledger.Failure
	if errors.As(err, &fail) {
		return failureEnvelope(fail)
	}
	return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
}

// refuse renders a refusal computed against this session's view,
// carrying the caller's affordances on the subject from that same
// view: "not that" and "then what may I do?" in one envelope, which
// is the whole reason these verbs are not sugar over the raw seam
// (D4). Nothing was appended, so the position is the view's own tip.
func (ls *loopSession) refuse(env *envelope.Envelope, subject, verb string, payload []byte, signer ed25519.PrivateKey) *envelope.Envelope {
	return ls.refuseAt(env, ls.ctx, subject, verb, payload, signer)
}

// refuseAt is refuse against a named view. On the remote path the
// refusal may have been computed several positions after the session
// opened, and remoteFailureEnvelope has ALREADY stamped that position:
// re-stamping would overwrite a correct answer with a stale one, so an
// envelope that carries a position keeps it, and the affordances come
// from the view it was computed at rather than from the session's
// opening tip (plans/os-9b3f3ef3.md D3).
func (ls *loopSession) refuseAt(env *envelope.Envelope, view *admit.Context, subject, verb string, payload []byte, signer ed25519.PrivateKey) *envelope.Envelope {
	if view == nil {
		view = ls.ctx
	}
	env = stampAffordancesFrom(env, view, signer, subject)
	if env.Position == nil {
		env = stampTip(env, view.Count)
	}
	return journalAttempt(env, ls.dir, signer, verb, subject, payload)
}

// loopAct is one act ready for the boundary: the verb and the
// payload the derivations completed, plus the result the success
// envelope reports, which may name the position the act lands at
// (a claim's fence, a reservation's id).
type loopAct struct {
	verb    string
	payload []byte
	// derive recomputes this payload against a view. The remote path
	// re-runs it inside the optimistic loop, because the tip can move
	// between the session opening and the push: a nil derive means the
	// payload holds nothing view-dependent and nothing can diverge.
	derive   func(ctx *admit.Context) ([]byte, *envelope.Envelope)
	resultAt func(pos int) map[string]any
}

// derivedDivergence is a refusal raised because the view a derived
// argument came from has moved. It carries the envelope to render:
// a re-derivation that now refuses says so in its own words (two open
// reservations name both candidates), and a value that merely moved
// gets the contention refusal below.
type derivedDivergence struct{ env *envelope.Envelope }

func (d *derivedDivergence) Error() string { return d.env.Error.Message }

// recheckDerivation is the guard pushDraft runs against each refreshed
// view. It never SUBSTITUTES the fresh value: a value derived from a
// view that has since moved is not a better argument, it is a
// different decision — a second reservation makes the act ambiguous,
// and a re-taken window is an authorization the lane never gave. So it
// refuses, naming what changed, and the lane re-orients
// (plans/os-9b3f3ef3.md D1).
func recheckDerivation(act loopAct, subject string) func(*admit.Context) error {
	if act.derive == nil {
		return nil
	}
	return func(ctx *admit.Context) error {
		fresh, failEnv := act.derive(ctx)
		if failEnv != nil {
			return &derivedDivergence{env: failEnv}
		}
		if bytes.Equal(fresh, act.payload) {
			return nil
		}
		return &derivedDivergence{env: envelope.Fail(envelope.ExitContention, "contention",
			fmt.Sprintf("the view this act was derived from moved before it landed: %s on %s was drafted against %s and the refreshed tip yields %s — nothing was appended, and the derived value is not silently replaced because a different value is a different decision. Re-read the situation and act again",
				act.verb, subject, string(act.payload), string(fresh)))}
	}
}

// commit is the shared pre-flight and append. It signs a draft at
// the authoritative tip, runs the SAME admit.Check admission
// enforces, and on refusal renders the boundary's own error beside
// the affordances — nothing appended, nothing signed into the chain.
// On success the transport decides where the record lands: the local
// store directly, or the optimistic loop, which re-signs and re-runs
// admission against the refreshed tip on every attempt.
func (ls *loopSession) commit(f *loopFlags, act loopAct, signer ed25519.PrivateKey, stdout, stderr io.Writer) int {
	subject := *f.subject
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	rec, err := event.Sign(event.Event{
		V:       ls.ctx.Active,
		TS:      time.Now().UTC().Format(time.RFC3339),
		Actor:   fp,
		Verb:    act.verb,
		Subject: subject,
		Payload: json.RawMessage(act.payload),
		Prev:    ls.ctx.Tip,
	}, signer)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot sign the act: %v", err)), stdout, stderr)
	}
	if err := admit.Check(ls.ctx, rec); err != nil {
		return render(ls.refuse(remoteFailureEnvelope(err), subject, act.verb, act.payload, signer), stdout, stderr)
	}
	if ls.remote != nil {
		landed, res, err := ls.remote.pushDraft(act.verb, subject, string(act.payload), signer, fp,
			recheckDerivation(act, subject))
		if err != nil {
			return render(ls.refuseAt(remoteFailureEnvelope(err), refusalView(err, ls.ctx),
				subject, act.verb, act.payload, signer), stdout, stderr)
		}
		hash, err := landed.Event.Hash()
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
		result := act.resultAt(res.Position)
		result["appended"] = hash
		result["commit"] = res.Commit
		result["attempts"] = res.Attempts
		// The remote posture keeps no local ledger to reopen at the
		// landed tip, so a success there carries no affordances and
		// journals nothing — the standing shape of every remote
		// append (spec/loop-verbs.md, deliberate absences).
		return render(stampTip(envelope.OK(result), res.Position+1), stdout, stderr)
	}
	pos, err := ls.store.Append(rec, ls.ctx.Resolve)
	if err != nil {
		return render(ls.refuse(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), subject, act.verb, act.payload, signer), stdout, stderr)
	}
	hash, err := rec.Event.Hash()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	result := act.resultAt(pos)
	result["appended"] = hash
	// Affordances on a success answer "what next", so they are
	// recomputed at the LANDED tip, not the one the act was judged
	// against.
	env := stampAffordances(envelope.OK(result), ls.dir, signer, subject)
	return render(journalAttempt(stampTip(env, pos+1), ls.dir, signer, act.verb, subject, act.payload), stdout, stderr)
}

// terse is the ordinary success result: the subject and nothing
// else. Teaching text lives in refusals (D6).
func terse(subject string) func(int) map[string]any {
	return func(int) map[string]any { return map[string]any{"subject": subject} }
}

// activeFence is the citation every holder-signed event on a held
// subject must carry, derived from the active claim window rather
// than asked for: an event citing anything else is refused anyway,
// so a flag could only ever be wrong. Outside a window there is no
// fence to cite and the key is omitted — the act is illegal for a
// reason the boundary states better than a derivation could.
func activeFence(ctx *admit.Context, subject string) (string, bool) {
	if ctx == nil || ctx.Lifecycle == nil {
		return "", false
	}
	s, ok := ctx.Lifecycle.State(subject)
	if !ok || s.Claim == nil {
		return "", false
	}
	return fmt.Sprintf("%d", s.Claim.Fence), true
}

// soleOpenReservation names the reservation a close cites. Exactly
// one open valid reservation is unambiguous. None is a missing fact,
// and the refusal says what would establish it. Several is an
// AMBIGUITY, and the refusal names the candidates rather than
// choosing among them: picking one silently would settle a spend
// decision that is the lane's to make (D3).
func soleOpenReservation(ctx *admit.Context, subject, act string) (int, *envelope.Envelope) {
	s, ok := ctx.Lifecycle.State(subject)
	if !ok {
		return 0, envelope.Fail(envelope.ExitNotFound, "not_found",
			fmt.Sprintf("no contract %s in the fold — %s cites a reservation on a contract that exists", subject, act))
	}
	open := admit.BudgetViewAt(ctx.Records, ctx.Table, subject, s).Open
	switch len(open) {
	case 1:
		return open[0].Pos, nil
	case 0:
		return 0, envelope.Fail(envelope.ExitNotFound, "not_found",
			fmt.Sprintf("no open valid reservation stands on %s — %s needs one, and seed budget reserve --amount <n> establishes it", subject, act))
	}
	names := make([]string, 0, len(open))
	for _, r := range open {
		names = append(names, fmt.Sprintf("position %d (%d units, %s)", r.Pos, r.Amount, r.Signer))
	}
	sort.Strings(names)
	return 0, envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("%s carries %d open reservations and %s cites exactly one — %s; name it through seed ledger append rather than have the choice made for you",
			subject, len(open), act, strings.Join(names, "; ")))
}

// loopPacket reads, completes and validates the four-part packet a
// deliberate exit carries. The shape is checked AT THE DOOR, before
// a session is even opened, so a malformed packet never becomes a
// signed record (D5). The base range may come from the file, from
// --base, or from the repository at --repo; a file and a flag that
// disagree refuse rather than have a winner picked for them.
func loopPacket(path, baseFlag, repo, subject string) (json.RawMessage, *envelope.Envelope) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --packet: %v", err))
	}
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--packet is not a JSON object: %v", err))
	}
	// The JSON value null unmarshals into a map with NO error, leaving
	// it nil, and writing a derived base into a nil map panics. An
	// array or a string errors above; only null reaches here, which is
	// why the malformed-packet drills missed it: every bad packet they
	// tried was an object.
	if parts == nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage",
			"--packet is the JSON value null, not an object: a packet carries four named parts (spec/packets.md)")
	}
	filed := ""
	if b, ok := parts["base"]; ok {
		if err := json.Unmarshal(b, &filed); err != nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", "the packet's base is not a string")
		}
	}
	switch {
	case filed != "" && baseFlag != "" && filed != baseFlag:
		return nil, envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("the packet names base %q and --base names %q — one range, resolved by you, not by precedence", filed, baseFlag))
	case filed == "" && baseFlag != "":
		filed = baseFlag
	case filed == "" && repo != "":
		derived, err := deriveBase(repo)
		if err != nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", err.Error())
		}
		filed = derived
	}
	if filed != "" {
		b, err := json.Marshal(filed)
		if err != nil {
			return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
		}
		parts["base"] = b
	}
	whole, err := json.Marshal(parts)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	if _, err := packet.Parse(subject, whole); err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", err.Error())
	}
	return whole, nil
}

// deriveBase reads the resume range out of the repository: the head
// commit, and the merge-base against the remote's default branch,
// which is a fact git itself holds. Where git holds no such fact the
// refusal names the missing one and what would establish it, rather
// than guessing a branch.
func deriveBase(repo string) (string, error) {
	head, err := gitOut(repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("the repository at %s has no resolvable HEAD to end the range at: %v", repo, err)
	}
	against, err := gitOut(repo, "rev-parse", "--verify", "--quiet", "origin/HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("the repository at %s has no origin/HEAD to take the merge-base against — pass --base <merge-base>..<head>, or establish it with git remote set-head origin -a", repo)
	}
	mb, err := gitOut(repo, "merge-base", against, head)
	if err != nil {
		return "", fmt.Errorf("no merge-base between origin/HEAD and HEAD in %s: %v", repo, err)
	}
	return mb + ".." + head, nil
}

func gitOut(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("git %s produced nothing", strings.Join(args, " "))
	}
	return s, nil
}

// runClaim dispatches the claim lane's three acts.
func runClaim(args []string, stdout, stderr io.Writer) int {
	known := loopverb.English(loopverb.Subverbs("claim"))
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "claim requires a subverb: "+known), stdout, stderr)
	}
	act, ok := loopverb.Lookup("claim", args[0])
	if !ok {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("unknown claim subverb %q — %s", args[0], known)), stdout, stderr)
	}
	if act.Sub == "take" {
		return runClaimTake(args[1:], stdout, stderr)
	}
	return runClaimExit(args[1:], act.Verb, act.Name(), stdout, stderr)
}

// runClaimTake takes the exclusive claim. It is remote-only: the
// table marks claim.taken alone exclusive, and only the push
// round-trip can order two rivals, so the local path refuses with
// the one account the raw seam already gives (D1).
func runClaimTake(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claim take", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	repo := fs.String("repo", "", "repository the surfacing lessons are verified against (default: none surface, their count is reported unverified)")
	nowFlag := fs.String("now", "", "RFC3339 instant the lessons' expiry is read at (default: now); admission reads no clock")
	parseErr := fs.Parse(args)
	if env := f.usage("claim take", parseErr, fs.NArg(), ""); env != nil {
		return render(env, stdout, stderr)
	}
	now := time.Now().UTC()
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--now %q is not an RFC3339 timestamp", *nowFlag)), stdout, stderr)
		}
		now = parsed
	}
	if *f.dir != "" {
		return render(exclusiveOnlineOnly(claimTakenVerb), stdout, stderr)
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
	// The lessons delivered at claim time (plans/os-96850e5a.md D6):
	// the surfacing set for the subject, verified against the
	// repository before anything surfaces; without one nothing
	// surfaces and the count is reported unverified.
	// The set is derived from the view the claim is judged against,
	// and re-derived against every refreshed view the optimistic loop
	// retries at (review finding on the task PR): a promotion or a
	// contest landing mid-flight changes what the claim receives, and
	// the response must report the set at the tip the claim landed on,
	// never the one the session opened at. The payload holds nothing
	// derived, so the re-derivation can never diverge; it only refreshes
	// the result.
	var lessons, unresolvedRows []map[string]any
	deriveLessons := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		surfaced, unresolved := curation.Surfacing(ctx.Records, ctx.Lifecycle, *repo, subject, now)
		lessons = []map[string]any{}
		for _, l := range surfaced {
			lessons = append(lessons, map[string]any{"lesson": l.Lesson, "hypothesis": l.Hypothesis, "applies_when": l.AppliesWhen, "carrier": l.Carrier, "digest": l.Digest})
		}
		unresolvedRows = []map[string]any{}
		for _, u := range unresolved {
			unresolvedRows = append(unresolvedRows, map[string]any{"lesson": u.Lesson, "hypothesis": u.Hypothesis, "reason": u.Reason})
		}
		return []byte(`{}`), nil
	}
	deriveLessons(ls.ctx)
	unresolvedCount := func() int { return len(unresolvedRows) }
	// The fence IS the admitted position, so the response names it:
	// every holder-signed event that follows cites this number, and
	// deriving it here is what spares the lane a projection read.
	return ls.commit(f, loopAct{verb: claimTakenVerb, payload: []byte(`{}`), derive: deriveLessons, resultAt: func(pos int) map[string]any {
		out := map[string]any{"subject": subject, "fence": fmt.Sprintf("%d", pos), "lessons": lessons}
		// The racing note (plans/os-56bee171.md D4): a racer learns at
		// claim time that its squad races and what the operator said
		// the race costs, so the cost sentence is in front of the lane
		// that spends it.
		if ls.ctx != nil && ls.ctx.Declaration != nil && ls.ctx.Lifecycle != nil {
			if s, ok := ls.ctx.Lifecycle.State(subject); ok {
				if racers, cost, races := ls.ctx.Declaration.RacingFor(s.Routing); races {
					out["racing"] = map[string]any{"racers": racers, "cost": cost}
				}
			}
		}
		if *repo == "" {
			out["lessons_unverified"] = fmt.Sprintf("%d", unresolvedCount())
		} else {
			out["lessons_unresolved"] = unresolvedRows
		}
		return out
	}}, signer, stdout, stderr)
}

// runClaimExit is release and park: the two deliberate exits that
// leave the contract for someone else to resume, each carrying its
// packet and citing the fence derived from the active window.
func runClaimExit(args []string, verb, name string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	packetPath := fs.String("packet", "", "four-part handoff packet (JSON file)")
	base := fs.String("base", "", "resume range <merge-base>..<head>, when the packet does not name it")
	repo := fs.String("repo", "", "repository the range is derived from, when neither the packet nor --base names it")
	// The park is the ONE exit that may also ask something: from
	// in_progress an escalation rides it, because nothing new may
	// leave that state (spec/escalation.md). Release refuses the
	// flags rather than ignoring them, so a question written on the
	// wrong verb is a refusal, never a silently dropped one.
	q := bindQuestionFlags(fs)
	parseErr := fs.Parse(args)
	missing := ""
	if *packetPath == "" {
		missing = "and --packet <file> (every deliberate exit carries one)"
	}
	if env := f.usage(name, parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	var question json.RawMessage
	if q.present() {
		if verb != claimParkedVerb {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf(
				"%s cannot carry a question — from in_progress an escalation rides `claim park`, the one exit that may also ask something",
				name)), stdout, stderr)
		}
		var qenv *envelope.Envelope
		if question, qenv = q.body(*f.subject); qenv != nil {
			return render(qenv, stdout, stderr)
		}
	}
	signer, env := loopSigner(*f.keyPath, *f.as)
	if env != nil {
		return render(env, stdout, stderr)
	}
	body, env := loopPacket(*packetPath, *base, *repo, *f.subject)
	if env != nil {
		return render(env, stdout, stderr)
	}
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		return exitPayload(ctx, *f.subject, body, false, question)
	}
	payload, env := derive(ls.ctx)
	if env != nil {
		return render(env, stdout, stderr)
	}
	return ls.commit(f, loopAct{verb: verb, payload: payload, derive: derive,
		resultAt: terse(*f.subject)}, signer, stdout, stderr)
}

// runSubmission makes the submission: the deliberate exit that hands
// the work to the verifier lane. It carries the same packet as the
// other exits, plus the approved plan's anchor where one is approved
// — the citation is derivable because an approval admits ONE exact
// revision, so no other value could ever be legal.
func runSubmission(args []string, stdout, stderr io.Writer) int {
	known := loopverb.English(loopverb.Subverbs("submission"))
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "submission requires the subverb: "+known), stdout, stderr)
	}
	// Resolved through the registry like claim and budget, rather than
	// compared against a literal: a hard-coded "make" would leave the
	// registry authoritative for the validator and the advertised
	// alternatives while the CLI quietly disagreed (review finding on
	// this PR).
	if _, ok := loopverb.Lookup("submission", args[0]); !ok {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown submission subverb %q — %s", args[0], known)), stdout, stderr)
	}
	fs := flag.NewFlagSet("submission make", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	packetPath := fs.String("packet", "", "four-part handoff packet (JSON file)")
	base := fs.String("base", "", "resume range <merge-base>..<head>, when the packet does not name it")
	repo := fs.String("repo", "", "repository the range is derived from, when neither the packet nor --base names it")
	pr := fs.String("pr", "", "the pull request the submission opened (pr/<n> or <n>), what the maintenance pass polls the forge for")
	parseErr := fs.Parse(args[1:])
	missing := ""
	if *packetPath == "" {
		missing = "and --packet <file> (every deliberate exit carries one)"
	}
	if *pr != "" {
		if _, err := externalfact.PRNumber(*pr); err != nil {
			missing = "and --pr <ref> as pr/<n> or <n>"
		}
	}
	if env := f.usage("submission make", parseErr, fs.NArg(), missing); env != nil {
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
	ls, env := openLoopSession(f)
	if env != nil {
		return render(env, stdout, stderr)
	}
	defer ls.done()
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		payload, env := exitPayload(ctx, *f.subject, body, true)
		if env != nil || *pr == "" {
			return payload, env
		}
		// The pull request the submission names (plans/os-0cd18799.md
		// D2): a sibling of the packet, defined from seed/8, what the
		// forge observation binds to. Refused before signing on an
		// earlier chain rather than appended as a field the fold of
		// that version would not read.
		if !version.ForgeChecksApply(ctx.Active) {
			return nil, envelope.Fail(envelope.ExitVersionMismatch, "version_mismatch",
				fmt.Sprintf("--pr is not defined at %s: naming the pull request needs a chain at %s", ctx.Active, version.Seed8))
		}
		return withPR(payload, *pr)
	}
	payload, env := derive(ls.ctx)
	if env != nil {
		return render(env, stdout, stderr)
	}
	return ls.commit(f, loopAct{verb: submissionMadeVerb, payload: payload, derive: derive,
		resultAt: terse(*f.subject)}, signer, stdout, stderr)
}

// withPR adds the pull-request reference beside the exit payload's
// packet, fence and plan.
func withPR(payload []byte, pr string) ([]byte, *envelope.Envelope) {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	out["pr"] = json.RawMessage(strconv.Quote(pr))
	b, err := json.Marshal(out)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	return b, nil
}

// exitPayload assembles a deliberate exit's payload: the validated
// packet, the derived fence where a window is open, and — for the
// submission — the approved plan anchor where one stands.
func exitPayload(ctx *admit.Context, subject string, body json.RawMessage, citePlan bool, question ...json.RawMessage) ([]byte, *envelope.Envelope) {
	out := map[string]json.RawMessage{"packet": body}
	for _, q := range question {
		if len(q) > 0 {
			out[escalation.Key] = q
		}
	}
	if fence, ok := activeFence(ctx, subject); ok {
		out["fence"] = json.RawMessage(strconv.Quote(fence))
	}
	if citePlan && ctx.Lifecycle != nil {
		if anchor, ok := ctx.Lifecycle.PlanApproved(subject); ok {
			out["plan"] = json.RawMessage(strconv.Quote(anchor))
		}
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	return payload, nil
}

// runBudgetLoop is reserve, settle and release: the three acts that
// open and close a spending window. The reservation a close cites is
// derived from the shared budget view, the same one admission judges
// against.
func runBudgetLoop(args []string, verb, name string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	amount := fs.String("amount", "", "units to reserve: a judgment, never a derivation")
	actuals := fs.String("actuals", "", "units actually spent: a judgment, never a derivation")
	parseErr := fs.Parse(args)
	missing := ""
	switch verb {
	case transition.BudgetReserveVerb:
		if *amount == "" {
			missing = "and --amount <n>"
		}
	case transition.BudgetSettleVerb:
		if *actuals == "" {
			missing = "and --actuals <n>"
		}
	}
	if env := f.usage(name, parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	if verb != transition.BudgetReserveVerb && *amount != "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("%s closes a reservation and never sizes one — --amount belongs to budget reserve", name)), stdout, stderr)
	}
	if verb != transition.BudgetSettleVerb && *actuals != "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("%s records no spend — --actuals belongs to budget settle, the verb that does", name)), stdout, stderr)
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
	cited := -1
	// The whole payload is a derivation of the view, so it is expressed
	// once and re-run against the refreshed tip inside the optimistic
	// loop: the fence from the active window, and for a close the sole
	// open reservation, which is exactly the value a rival can change
	// underneath the act.
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		out := map[string]json.RawMessage{}
		if fence, ok := activeFence(ctx, subject); ok {
			out["fence"] = json.RawMessage(strconv.Quote(fence))
		}
		if verb == transition.BudgetReserveVerb {
			out["amount"] = json.RawMessage(strconv.Quote(*amount))
		} else {
			pos, refusal := soleOpenReservation(ctx, subject, "a close")
			if refusal != nil {
				return nil, refusal
			}
			cited = pos
			out["reservation"] = json.RawMessage(strconv.Quote(fmt.Sprintf("%d", pos)))
			if verb == transition.BudgetSettleVerb {
				out["actuals"] = json.RawMessage(strconv.Quote(*actuals))
			}
		}
		b, mErr := json.Marshal(out)
		if mErr != nil {
			return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", mErr.Error())
		}
		return b, nil
	}
	payload, refusal := derive(ls.ctx)
	if refusal != nil {
		return render(ls.refuse(refusal, subject, verb, nil, signer), stdout, stderr)
	}
	// A reserve's landed position IS its reservation id, so the
	// response names it and the closing act needs no lookup at all.
	resultAt := func(pos int) map[string]any {
		id := cited
		if verb == transition.BudgetReserveVerb {
			id = pos
		}
		return map[string]any{"subject": subject, "reservation": fmt.Sprintf("%d", id)}
	}
	return ls.commit(f, loopAct{verb: verb, payload: payload, derive: derive,
		resultAt: resultAt}, signer, stdout, stderr)
}

// loopSigner reads the acting lane's key.
func loopSigner(path, expect string) (ed25519.PrivateKey, *envelope.Envelope) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err))
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err))
	}
	// The identity check happens HERE, on the bytes just read, because
	// this is where the signature is taken (plans/os-9a89245c.md).
	// internal/loop fingerprints the key file before every act, and
	// this function reopens the same path independently: an atomic
	// replacement between those two reads is observed by only one of
	// them. Comparing at the signing site closes that, because check
	// and signature then see the SAME bytes from the same read; making
	// two reads atomic across a process boundary was never the fix.
	//
	// Optional at the seam: the loop verbs are also reachable by hand,
	// and an operator acting once has no loop to race with. A
	// fingerprint is public - it is the actor field of every record in
	// the chain - so carrying it costs no confidentiality.
	if expect == "" {
		return signer, nil
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot fingerprint --key: %v", err))
	}
	if fp != expect {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf(
			"--as %s but --key %s now holds %s: the key changed under the caller, and signing as an identity it did not declare "+
				"is what --as exists to prevent — restart the loop to pick up the new key",
			expect, path, fp))
	}
	return signer, nil
}
