// Package loop is the worker lane's loop, made executable
// (plans/os-abb206c8.md; docs/build-plan.md Phase 9 item 1).
// Promotion criterion 1 asks that a lane run poll, claim, work, meter,
// submit and a deliberate exit ENTIRELY through Seed verbs, orienting
// from one position-stamped read. A lane that cannot run that loop
// cannot run unattended, whatever a conformance report says.
//
// It is a LIBRARY, not a CLI verb (D1). Seed does not own the work:
// writing the code is the model's act, so the work step is supplied by
// the caller. A `seed loop run` verb is deliberately deferred: it would
// invite treating the CLI as the agent, and the real consumers are the
// small-team and fleet fixtures, which drive this in CI with no model
// and no wake channel.
//
// The loop holds no ledger and reimplements no verb. Every act goes
// through the Verbs seam, which is the CLI's own dispatch: a second
// implementation of claim.taken living here would consult the admission
// boundary not at all and so could not answer a refusal with what IS
// legal, which is the whole reason the loop verbs exist.
package loop

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

// Result is one invocation's outcome as the envelope reported it. A
// refusal carries its own code and message, and the loop passes both
// on verbatim rather than paraphrasing: the next worker, or the human,
// is owed the boundary's account rather than the lane's summary of it.
type Result struct {
	Exit     int
	OK       bool
	Code     string
	Message  string
	Position string
	Result   map[string]any
}

// Refused reports whether the boundary declined the act.
func (r Result) Refused() bool { return !r.OK }

// Verbs runs one `seed` invocation and returns its envelope. The
// implementation is the CLI's dispatch; the loop depends on the seam
// rather than on a binary so the fixtures can drive it in process.
type Verbs interface {
	Run(args ...string) Result
}

// Situation is what one orienting read told the lane. It is deliberately
// thin: the loop carries forward the position and the window it holds,
// and reads everything else again rather than caching a world.
type Situation struct {
	Position string
	Windows  []map[string]any
	Result   map[string]any
}

// Acceptance is the anchor the holder is judged against, as the read
// reports it. The loop's exit packet needs it: a packet's acceptance
// part is what a successor is judged against, so a lane that could not
// read it could not write its own exit.
func (s Situation) Acceptance(subject string) string {
	for _, w := range s.Windows {
		if w["subject"] != subject {
			continue
		}
		if ref, ok := w["acceptance"].(string); ok {
			return ref
		}
	}
	return ""
}

// Holds reports whether the read shows an active window on the subject,
// which is how the loop knows its claim landed without inferring it
// from the claim's own exit code.
func (s Situation) Holds(subject string) bool {
	for _, w := range s.Windows {
		if w["subject"] == subject {
			return true
		}
	}
	return false
}

// Work is the caller's step, and the one thing Seed does not own.
// Units is what the step cost, settled to the ledger at the end of the
// bracket; a step that cannot say returns zero and the settle records
// zero, which is honest rather than invented.
type Work interface {
	Do(subject string, s Situation) (units int, err error)
}

// WorkFunc adapts a function to Work.
type WorkFunc func(subject string, s Situation) (int, error)

// Do implements Work.
func (f WorkFunc) Do(subject string, s Situation) (int, error) { return f(subject, s) }

// ErrUndeclaredAct is the act gate's refusal (D2): the loop performs
// only what its manifest declares in acts_through. It is a PROGRAMMING
// error, raised before anything is signed, because a loop and a
// manifest that disagree are a bug in one of them and the system
// should say so loudly rather than act on the more permissive reading.
var ErrUndeclaredAct = errors.New("the lane manifest does not declare this act")

// Driver runs one lane's loop.
type Driver struct {
	manifest lane.Manifest
	verbs    Verbs
	posture  []string
	keyPath  string
	actor    string
	work     Work

	// since is the position the last orienting read carried, handed
	// back as --since so a resuming lane pays for the delta rather
	// than reconstructing the world.
	since string

	// repo and base supply the packet's resume coordinate. The loop
	// derives neither: a range is a fact about a workspace, and the
	// workspace belongs to the caller whose work step wrote in it.
	// Given repo, the loop verbs derive the range themselves, which is
	// the path that stays right as the work advances.
	repo string
	base string

	// obsDir is where liveness lands. The loop emits ONLY as a
	// side-effect of a declared liveness act, so there is no path on
	// which it speaks without working: that is the whole content of
	// "liveness rides the work", and the reason the loop's vocabulary
	// contains no verb whose only purpose is to report it.
	obsDir string
	fence  string
	count  int

	// acceptance is the anchor the read reports for the held window,
	// carried into every exit packet: what a successor is judged
	// against is the contract's, never the lane's paraphrase.
	acceptance string
}

// Option configures a Driver.
type Option func(*Driver)

// WithSince primes the resume position, for a lane continuing rather
// than starting.
func WithSince(position string) Option { return func(d *Driver) { d.since = position } }

// WithRepo names the workspace the resume range is derived from, which
// the loop verbs do for themselves given --repo.
func WithRepo(path string) Option { return func(d *Driver) { d.repo = path } }

// WithBase names the resume range outright, for a lane with no
// repository to derive one from: the zero-length "<mb>..<mb>" is the
// honest value when no work was pushed.
func WithBase(rng string) Option { return func(d *Driver) { d.base = rng } }

// WithObservations names the observation stream root. Without it the
// loop still runs and still works; it is simply not observable, which
// the expiry classification will eventually read as silence.
func WithObservations(dir string) Option { return func(d *Driver) { d.obsDir = dir } }

// New builds a Driver for one lane. The manifest is resolved at
// construction rather than consulted per act, so a loop whose lane
// declares nothing it does refuses at build time.
//
// posture is the transport pair the reads and acts share, e.g.
// {"--remote", repo, "--state", dir}: the loop orients and acts in ONE
// posture or it reads a view it is not judged against.
func New(m lane.Manifest, v Verbs, posture []string, keyPath string, w Work, opts ...Option) (*Driver, error) {
	if v == nil {
		return nil, errors.New("a loop with no verb seam can perform no act")
	}
	if w == nil {
		return nil, errors.New("a loop with no work step is a heartbeat, which the charter forbids")
	}
	if len(posture) == 0 {
		return nil, errors.New("a loop with no posture has no ledger to orient against")
	}
	if keyPath == "" {
		return nil, errors.New("a loop signs with one key")
	}
	// The actor is DERIVED from the signing key, never supplied beside
	// it (review finding on #191). A loop told to poll as one actor
	// while signing as another would select work under one identity's
	// eligibility, act under a second, and write its liveness under a
	// third — and the classifier, which keys the stream by the holder,
	// would see silence from a worker that was working. One key, one
	// identity, no way to pass two.
	actor, err := actorOf(keyPath)
	if err != nil {
		return nil, err
	}
	if len(m.LivenessFrom) == 0 {
		return nil, fmt.Errorf("lane %q declares no liveness_from: a loop whose steps emit nothing is "+
			"indistinguishable from a dead one, and the charter forbids a bare heartbeat to paper over it", m.Lane)
	}
	if len(m.ActsThrough) == 0 {
		return nil, fmt.Errorf("lane %q declares no acts_through: %w", m.Lane, ErrUndeclaredAct)
	}
	d := &Driver{manifest: m, verbs: v, posture: posture, keyPath: keyPath, actor: actor, work: w}
	for _, o := range opts {
		o(d)
	}
	if d.repo == "" && d.base == "" {
		return nil, errors.New("a loop needs a repo to derive its resume range from, or a base to state it: " +
			"every deliberate exit carries a packet, and a packet without a resume coordinate is not one")
	}
	return d, nil
}

// actorOf derives the lane's identity from the key it signs with. It
// is the only way a Driver learns its actor, so poll, act and observe
// cannot disagree about who is working.
func actorOf(keyPath string) (string, error) {
	b, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("cannot read the lane's key: %w", err)
	}
	key, err := event.ParsePrivateKey(b)
	if err != nil {
		return "", fmt.Errorf("cannot parse the lane's key: %w", err)
	}
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		return "", fmt.Errorf("cannot fingerprint the lane's key: %w", err)
	}
	return fp, nil
}

// ErrKeyRotated is the refusal when the signing key changes under a
// running Driver. Adopting the new identity silently would leave a
// window held by one actor and worked by another, which is worse than
// stopping: the loop refuses and the operator restarts it, that being
// the only safe way to pick up a new key (review finding on #193).
var ErrKeyRotated = errors.New("the signing key changed under a running loop")

// checkIdentity re-derives the fingerprint and compares it to the one
// the loop polls and observes under. Deriving once at construction
// closes the mismatch a parameter could carry; it does not close the
// one the FILESYSTEM can, because the loop passes --key <path> and the
// CLI signs with whatever that path holds now.
func (d *Driver) checkIdentity() error {
	now, err := actorOf(d.keyPath)
	if err != nil {
		return err
	}
	if now != d.actor {
		return fmt.Errorf("%w: polling and observing as %s, but %s now signs as %s — restart the loop "+
			"to pick up the new key", ErrKeyRotated, d.actor, d.keyPath, now)
	}
	return nil
}

// Declares reports whether the lane's manifest names the act.
func (d *Driver) Declares(act string) bool { return slices.Contains(d.manifest.ActsThrough, act) }

// act performs one loop act through the verb seam, refusing at the gate
// when the manifest does not declare it. This is the close of the
// manifest's loop: editing the manifest without editing the loop, or
// the reverse, now fails rather than drifting.
func (d *Driver) act(name, subject string, extra ...string) (Result, error) {
	return d.actGated(name, subject, false, extra...)
}

// actGated is act with the identity gate optionally bypassed for a
// last-ditch exit; see the exemption below.
func (d *Driver) actGated(name, subject string, lastDitch bool, extra ...string) (Result, error) {
	a, ok := loopverb.ByName(name)
	if !ok {
		return Result{}, fmt.Errorf("%q is not a loop act (%s): %w",
			name, strings.Join(loopverb.Names(), ", "), ErrUndeclaredAct)
	}
	if !d.Declares(name) {
		return Result{}, fmt.Errorf("lane %q acts through %s and not %q: %w",
			d.manifest.Lane, loopverb.English(d.manifest.ActsThrough), name, ErrUndeclaredAct)
	}
	// EVERY act, not just the iteration's first. A key rotated while the
	// work step runs would otherwise let the settle and the exit sign as
	// a new identity: the window would be opened by one actor and closed
	// by another, or not closed at all, leaving the claim standing —
	// which is the state the deliberate exits exist to make impossible
	// (review finding on #194). Checking here is cheap next to the
	// round-trip the act is about to make.
	//
	// The one exemption is a LAST-DITCH exit attempt, which must be
	// allowed to reach the boundary even under a rotated key: refusing
	// it here would guarantee the silent abandonment the check exists
	// to prevent. Nothing is weakened by letting it through, because
	// the fence rule is the actual protection — it admits holder-signed
	// events only from the holder, so a rotated key's exit refuses at
	// the boundary rather than succeeding wrongly.
	if !lastDitch {
		if err := d.checkIdentity(); err != nil {
			return Result{}, err
		}
	}
	args := []string{a.Group, a.Sub}
	args = append(args, d.posture...)
	args = append(args, "--key", d.keyPath, "--subject", subject)
	// The identity the loop DECLARES it is acting as, checked at the
	// signing site so a key replaced between checkIdentity above and
	// the CLI's own read cannot sign in its place
	// (plans/os-9a89245c.md).
	//
	// The last-ditch exit carries none, and that is the same exemption
	// lastDitch already names rather than a caveat on it: strand
	// attempts the exit precisely so it reaches the ADMISSION
	// BOUNDARY, where the fence rule gives the authoritative refusal.
	// Passing the cached actor here would reinstate the identity gate
	// one layer lower, inside loopSigner, and the exit would stop with
	// usage at the seam instead — re-creating the blockage the strand
	// path exists to remove.
	if !lastDitch {
		args = append(args, "--as", d.actor)
	}
	res := d.verbs.Run(append(args, extra...)...)
	if !res.Refused() {
		// An act that opens a window NAMES the fence it opened, and the
		// loop adopts it before emitting: claim take is a declared
		// liveness source, so observing it under the previous window's
		// fence (or dropping it for want of one) would leave the claim's
		// own liveness invisible to the classifier exactly when a worker
		// is most likely to stall — between taking work and starting it
		// (review finding on #191).
		if fence, ok := res.Result["fence"].(string); ok && fence != "" {
			d.fence = fence
		}
		d.observe(name, subject)
	}
	return res, nil
}

// observe emits one observation for a declared liveness act that just
// succeeded. Every guard here is load-bearing:
//
//   - only acts the manifest names in liveness_from emit, so the
//     declaration 1a could only compare as labels is now the thing that
//     decides what happens;
//   - only a SUCCEEDED act emits, so a refused step cannot pass for
//     progress;
//   - the stream is keyed to the lane's own actor and the fence of the
//     window it holds, which is exactly how internal/obs classifies it:
//     a stream written under any other key is invisible to the
//     classifier and therefore useless as liveness.
//
// A failure to write is deliberately swallowed. The channel is
// ephemeral and lossy by declaration (charter §II.3), so a lane that
// abandoned real work because its telemetry disk was full would be
// trading an authoritative act for a non-authoritative one.
func (d *Driver) observe(act, subject string) {
	if d.obsDir == "" || d.fence == "" || !slices.Contains(d.manifest.LivenessFrom, act) {
		return
	}
	d.count++
	_ = obs.Append(d.obsDir, d.actor, d.fence, obs.Line{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Subject: subject,
		Count:   d.count,
		Step:    act,
	})
}

// Act performs one declared act, for a caller driving the steps itself.
// The act gate and the identity check both apply, because act applies
// them: a caller stepping the loop by hand is not a caller entitled to
// sign under a key the loop never derived from.
func (d *Driver) Act(name, subject string, extra ...string) (Result, error) {
	return d.act(name, subject, extra...)
}

// Poll asks what may be claimed. It is a READ, not a loop act: the
// offer surface grants nothing and journals nothing, so it passes no
// gate. `offer list` already calls itself the worker's poll, and the
// loop consumes it rather than reinventing eligibility.
func (d *Driver) Poll() ([]string, Result) {
	args := append([]string{"offer", "list"}, d.posture...)
	res := d.verbs.Run(append(args, "--actor", d.actor)...)
	if res.Refused() {
		return nil, res
	}
	rows, _ := res.Result["offers"].([]any)
	var subjects []string
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			if s, ok := m["subject"].(string); ok {
				subjects = append(subjects, s)
			}
		}
	}
	return subjects, res
}

// Orient is the ONE position-stamped read the lane wakes to, carrying
// the last position forward as --since. Everything the loop believes
// about the world comes from here: a wake is a hint to read, never a
// fact about the world (the one-inbox doctrine).
func (d *Driver) Orient(subject string) (Situation, Result) {
	args := append([]string{"situation"}, d.posture...)
	args = append(args, "--key", d.keyPath)
	if subject != "" {
		args = append(args, "--subject", subject)
	}
	if d.since != "" {
		args = append(args, "--since", d.since)
	}
	res := d.verbs.Run(args...)
	if res.Refused() {
		return Situation{}, res
	}
	s := Situation{Position: res.Position, Result: res.Result}
	if rows, ok := res.Result["windows"].([]any); ok {
		for _, r := range rows {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			s.Windows = append(s.Windows, m)
			// The fence the observation stream is keyed by comes from
			// the read, never from the loop's memory of its own claim:
			// a fence invalidated by a concurrent reap must stop being
			// the key the moment the read says so.
			if m["subject"] == subject {
				if f, ok := m["fence"].(string); ok {
					d.fence = f
				}
				if a, ok := m["acceptance"].(string); ok {
					d.acceptance = a
				}
			}
		}
	}
	// The position advances only on a read that succeeded, so a refused
	// orient leaves the resume coordinate where it was rather than
	// skipping the delta it never saw.
	if s.Position != "" {
		d.since = s.Position
	}
	return s, res
}

// Outcome is how one iteration ended. Every ending is deliberate: the
// loop never abandons a window, which is what makes an expiry evidence
// of trouble rather than evidence of forgetfulness.
type Outcome string

const (
	// Submitted: the work landed and the window closed on a submission.
	Submitted Outcome = "submitted"
	// Parked: a refusal the lane could not act on closed the window
	// deliberately, with a packet carrying that refusal.
	Parked Outcome = "parked"
	// Idle: nothing was claimable. No window was opened, so none is
	// owed an exit.
	Idle Outcome = "idle"
)

// StepResult is how one iteration ended, and why. Cause is the refusal
// that stopped a parked iteration, carried out to the caller for the
// same reason the packet carries it inward: whoever is deciding what to
// do next is owed the boundary's own account.
type StepResult struct {
	Outcome Outcome
	Subject string
	Cause   Result
	Step    string
}

// Step runs one full iteration: poll, orient, claim, reserve, work,
// settle, submit — or park, when a refusal stops it after the window
// opened.
//
// The spend bracket is reserve/work/settle rather than work-then-meter:
// no execution path is unmetered (spec/executors.md), so capacity
// is committed BEFORE the work it pays for. That ordering is also what
// makes exhaustion parking reachable, since the reserve is where a
// worker learns it cannot spend.
func (d *Driver) Step(amount int) (StepResult, error) {
	// Before anything else: the key must still be the key this loop
	// derived its identity from. A rotation between iterations would
	// otherwise poll under the old fingerprint and sign under the new.
	if err := d.checkIdentity(); err != nil {
		return StepResult{Outcome: Idle, Step: "identity"}, err
	}
	subjects, res := d.Poll()
	if res.Refused() {
		return StepResult{Outcome: Idle, Cause: res, Step: "poll"},
			fmt.Errorf("poll refused (%s): %s", res.Code, res.Message)
	}
	if len(subjects) == 0 {
		return StepResult{Outcome: Idle}, nil
	}
	subject := subjects[0]

	if _, res := d.Orient(subject); res.Refused() {
		return StepResult{Outcome: Idle, Subject: subject, Cause: res, Step: "orient"},
			fmt.Errorf("orient refused (%s): %s", res.Code, res.Message)
	}

	// Claim. A lost race is not an error: the rival took it, and the
	// lane re-orients and takes different work on its next iteration.
	// Requiring admission or escalation here would manufacture an
	// escalation storm out of ordinary contention.
	take, err := d.act("claim take", subject)
	if err != nil {
		return StepResult{Outcome: Idle, Subject: subject, Step: "claim take"}, err
	}
	if take.Refused() {
		return StepResult{Outcome: Idle, Subject: subject, Cause: take, Step: "claim take"}, nil
	}

	// From here the window is OPEN, so every path below exits it
	// deliberately: there is no return that leaves a claim standing.
	s, res := d.Orient(subject)
	if res.Refused() {
		return d.park(subject, "orient", res)
	}
	// Unless the refreshed read says it is not. A concurrent reap
	// between the claim's push and this read leaves nothing to exit,
	// and the build plan's middle convergence arm is exactly this: a
	// refreshed position-stamped read showing the act is no longer
	// owed. Reserving anyway would refuse, and the park after it would
	// refuse too for want of an active claim, turning ordinary fleet
	// contention into an error (review finding on #191).
	if !s.Holds(subject) {
		return StepResult{Outcome: Idle, Subject: subject, Step: "orient"}, nil
	}

	reserve, err := d.act("budget reserve", subject, "--amount", fmt.Sprintf("%d", amount))
	if err != nil {
		return d.strand(subject, "budget reserve", err)
	}
	if reserve.Refused() {
		// Exhaustion (D4): the worker's spending gate is the reserve,
		// because run.started is the executor's act and no key this
		// loop signs with can trip it. The packet carries the refusal.
		return d.park(subject, "budget reserve", reserve)
	}

	units, workErr := d.work.Do(subject, s)
	if units < 0 {
		units = 0
	}

	// Settle before any exit, including a failed one: a reservation
	// nobody closes comes out of the next claimant's remaining, and
	// that claimant is a different worker.
	if settle, err := d.act("budget settle", subject, "--actuals", fmt.Sprintf("%d", units)); err != nil {
		return d.strand(subject, "budget settle", err)
	} else if settle.Refused() {
		return d.park(subject, "budget settle", settle)
	}
	if workErr != nil {
		return d.park(subject, "work", Result{Code: "work_failed", Message: workErr.Error()})
	}

	handoff, cleanup, err := d.successPacket()
	if err != nil {
		return d.strand(subject, "submission make", err)
	}
	defer cleanup()
	submit, err := d.act("submission make", subject, handoff...)
	if err != nil {
		return d.strand(subject, "submission make", err)
	}
	if submit.Refused() {
		return d.park(subject, "submission make", submit)
	}
	return StepResult{Outcome: Submitted, Subject: subject}, nil
}

// strand handles an ERROR raised while a window is open: the loop
// attempts the deliberate exit anyway and reports what happened.
//
// The case that motivates it is a key rotated mid-window (review
// finding on #196). Returning the error directly left the claim and
// its reservation open with no packet, which is exactly the silent
// abandonment the four deliberate exits exist to prevent — and the
// drill for it asserted that absence as correct, which was the
// mistake underneath the mistake.
//
// The attempt often fails, and that is honest rather than futile: the
// fence rule admits holder-signed events only from the holder, so a
// rotated key cannot park the window its predecessor opened. The
// window is then stranded whatever the loop does, and recovering it is
// the maintenance lane's reap. What the loop owes is to TRY, and then
// to say plainly that the window is open and why, rather than to
// return as though nothing were outstanding.
func (d *Driver) strand(subject, step string, cause error) (StepResult, error) {
	res, parkErr := d.parkGated(subject, step, Result{Code: "loop_error", Message: cause.Error()}, true)
	if parkErr == nil {
		// The exit landed after all: the window is closed and its
		// packet carries the cause.
		return res, cause
	}
	return StepResult{Outcome: Parked, Subject: subject, Step: step},
		fmt.Errorf("%w; the window on %s is left OPEN and needs a reap: the deliberate exit also "+
			"failed (%v)", cause, subject, parkErr)
}

// park closes an open window deliberately, carrying the refusal that
// stopped the lane. The findings hold the boundary's own code and
// message VERBATIM, which is the rule the build plan binds on
// escalation: the next worker is given the refusal rather than this
// lane's paraphrase of it.
func (d *Driver) park(subject, step string, cause Result) (StepResult, error) {
	res, err := d.parkGated(subject, step, cause, false)
	if err == nil {
		return res, nil
	}
	// The exit itself could not be made. A key rotated between the
	// refusal that stopped the work and this park is the case that
	// reaches here, and returning now would leave the window open with
	// no packet — the silent abandonment strand exists to prevent,
	// arrived at by a different route than an errored act.
	//
	// #196 routed errored ACTS through strand; a park triggered by a
	// REFUSAL was still returned directly, and this drill found it
	// (review finding on #202). So the last-ditch attempt is made here
	// too, and what it reports is the same: the exit was tried, and
	// whether it landed.
	return d.strand(subject, step, err)
}

func (d *Driver) parkGated(subject, step string, cause Result, lastDitch bool) (StepResult, error) {
	out := StepResult{Outcome: Parked, Subject: subject, Cause: cause, Step: step}
	extra, cleanup, err := d.packetFor(step, cause)
	if err != nil {
		return out, err
	}
	defer cleanup()
	res, err := d.actGated("claim park", subject, lastDitch, extra...)
	if err != nil {
		return out, err
	}
	if res.Refused() {
		// Both refusals, in full. The window is still open, and whoever
		// reads this needs what stopped the work as much as what
		// stopped the exit — the more so because an exhausted budget
		// arrives under the generic chain_invalid, so the code alone
		// would point at the ledger rather than the spend.
		return out, fmt.Errorf(
			"the window could not be parked after %s refused (%s: %s): park refused (%s): %s",
			step, cause.Code, cause.Message, res.Code, res.Message)
	}
	return out, nil
}

// packetFor writes the exit packet and returns the arguments naming it.
// The findings carry the refusal's code and message VERBATIM: a lane
// that paraphrased a boundary's account would hand its successor a
// worse version of the only thing that explains the stop.
func (d *Driver) packetFor(step string, cause Result) ([]string, func(), error) {
	outcome := cause.Message
	if cause.Code != "" {
		outcome = cause.Code + ": " + cause.Message
	}
	if strings.TrimSpace(outcome) == "" {
		outcome = "refused without an account, which is itself the finding"
	}
	return d.writePacket(map[string]any{
		"acceptance": d.acceptanceParts(),
		"decisions":  []any{},
		"refs":       []string{},
		"findings": []map[string]string{{
			"tried":   step,
			"outcome": outcome,
		}},
	})
}

// acceptanceParts is what a successor is judged against, taken from the
// contract's own spec as the orienting read reported it. A packet
// cannot be written without one, so a lane that never saw a window says
// the only honest thing it can: the anchor is unread, and that is
// itself the successor's first problem.
func (d *Driver) acceptanceParts() []string {
	if d.acceptance != "" {
		return []string{d.acceptance}
	}
	return []string{"the contract's acceptance spec, which this lane's read did not report"}
}

// successPacket is the submission's handoff: no findings, because
// nothing was tried and abandoned. It states what the work claims to
// have satisfied, which is the verifier's starting point.
func (d *Driver) successPacket() ([]string, func(), error) {
	return d.writePacket(map[string]any{
		"acceptance": d.acceptanceParts(),
		"decisions":  []any{},
		"refs":       []string{},
		"findings":   []map[string]string{},
	})
}

// packetFile is what writePacket writes through. The seam exists so the
// post-creation failure branches below are REACHABLE by a drill: a test
// that only ever exercises the success path cannot tell whether those
// branches unlink, and this one did not (review finding on #194). It
// follows the injection precedent internal/transition already sets.
type packetFile interface {
	Name() string
	Write([]byte) (int, error)
	Close() error
}

var createPacketFile = func() (packetFile, error) {
	return os.CreateTemp("", "seed-loop-packet-*.json")
}

// InjectPacketWriter replaces the packet writer and returns a restore
// func. Test-only: it exists to make a failure branch reachable, never
// to change production behavior.
func InjectPacketWriter(make func() (packetFile, error)) func() {
	prev := createPacketFile
	createPacketFile = make
	return func() { createPacketFile = prev }
}

// writePacket writes the packet and returns the arguments naming it
// plus a cleanup that removes the file. An unattended lane runs
// indefinitely, so a packet left behind per iteration is a slow leak of
// the host's temporary storage — and of the packets themselves, which
// are work-product content rather than scratch (review finding on
// #191). Cleanup runs on every path, refusals included.
func (d *Driver) writePacket(p map[string]any) ([]string, func(), error) {
	noop := func() {}
	if d.base != "" {
		p["base"] = d.base
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, noop, err
	}
	f, err := createPacketFile()
	if err != nil {
		return nil, noop, err
	}
	name := f.Name()
	remove := func() { _ = os.Remove(name) }
	if _, err := f.Write(body); err != nil {
		f.Close()
		remove()
		return nil, noop, err
	}
	if err := f.Close(); err != nil {
		remove()
		return nil, noop, err
	}
	args := []string{"--packet", name}
	if d.base == "" {
		args = append(args, "--repo", d.repo)
	}
	return args, remove, nil
}
