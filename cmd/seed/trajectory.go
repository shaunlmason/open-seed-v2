// The trajectory verbs (plans/os-6bd9ffff.md; spec/trajectories.md):
// record derives one lane's decision points from a local ledger and
// the attempts journal beside it, at the frames the lane decided
// from; replay recomputes those frames over the chain and the lane
// configuration as they stand now and refuses on any divergence. Both
// read the LOCAL posture only: the journal is written beside a local
// ledger and never synced, and the remote posture keeps no journal
// (spec/refusals.md), so a trajectory is recorded where the lane
// acted.

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shaunlmason/open-seed-v2/internal/decider"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/refusals"
	"github.com/shaunlmason/open-seed-v2/internal/trajectory"
)

func runTrajectory(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "trajectory requires a subverb: record or replay"), stdout, stderr)
	}
	switch args[0] {
	case "record":
		return runTrajectoryRecord(args[1:], stdout, stderr)
	case "replay":
		return runTrajectoryReplay(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown trajectory subverb %q — record or replay", args[0])), stdout, stderr)
}

// verifiedChain reads a local ledger's verified records: the same
// replay the admission context runs, without growing anything.
func verifiedChain(dir string) ([]*event.Record, *envelope.Envelope) {
	store, failEnv := openStoreReadOnly(dir)
	if failEnv != nil {
		return nil, failEnv
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
	}
	var records []*event.Record
	if _, err := store.VerifyFromGenesis(resolve, ledger.WithObserver(func(pos int, r *event.Record) {
		records = append(records, r)
	})); err != nil {
		var fail *ledger.Failure
		if errors.As(err, &fail) {
			return nil, failureEnvelope(fail)
		}
		return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
	}
	return records, nil
}

// journalBeside loads the attempts journal beside the ledger. A
// missing journal is an empty one: a lane that was never refused has
// nothing to read. A journal that does not load refuses, the
// declared-input posture, because a recording over a torn journal
// would silently omit decision points.
func journalBeside(dir string) (*refusals.Journal, *envelope.Envelope) {
	j, err := refusals.Load(filepath.Join(dir, refusals.File))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &refusals.Journal{Entries: []refusals.Entry{}}, nil
		}
		return nil, envelope.Fail(envelope.ExitUnreadable, "unreadable",
			fmt.Sprintf("the attempts journal beside the ledger does not load: %v", err))
	}
	return j, nil
}

func runTrajectoryRecord(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trajectory record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory: the chain and the attempts journal beside it")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the lane recorded")
	lanes := laneDir(fs)
	laneName := fs.String("lane", "", "the lane whose configuration the recording is taken under")
	out := fs.String("out", "", "write the canonical trajectory here (default: carried in the envelope)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" || *keyPath == "" || *laneName == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"trajectory record requires --ledger <dir> --key <path> --lane <name> [--lanes <dir>] [--out <file>]"), stdout, stderr)
	}
	signer, failEnv := loopSigner(*keyPath, "")
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	records, failEnv := verifiedChain(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	journal, failEnv := journalBeside(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	t, skipped, err := trajectory.Record(records, journal, signer, *lanes, *laneName)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	canonical, err := t.Canonical()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	admitted, refused := 0, 0
	for _, p := range t.Points {
		if p.Outcome == trajectory.OutcomeAdmitted {
			admitted++
		} else {
			refused++
		}
	}
	result := map[string]any{
		"lane": t.Lane, "actor": t.Actor, "manifest": t.Manifest, "posture": t.Posture,
		"points": len(t.Points), "admitted": admitted, "refused": refused,
		"skipped": map[string]any{"beyond_tip": skipped.BeyondTip, "other_actor": skipped.OtherActor},
	}
	if *out != "" {
		if err := os.WriteFile(*out, append(canonical, '\n'), 0o644); err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot write --out: %v", err)), stdout, stderr)
		}
		result["out"] = *out
	} else {
		result["trajectory"] = t
	}
	return render(stampTip(envelope.OK(result), len(records)), stdout, stderr)
}

func runTrajectoryReplay(args []string, stdout, stderr io.Writer) int {
	file, rest := splitOperand(args)
	fs := flag.NewFlagSet("trajectory replay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory the trajectory is replayed over")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the lane recorded")
	deciderName := fs.String("decider", "", "re-decide at each loop-act point with a named decider (scripted) and flag choice_diverged")
	lanes := laneDir(fs)
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || file == "" || *dir == "" || *keyPath == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"trajectory replay <file> --ledger <dir> --key <path> [--lanes <dir>]"), stdout, stderr)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnreadable, "unreadable", fmt.Sprintf("cannot read trajectory: %v", err)), stdout, stderr)
	}
	t, err := trajectory.Parse(raw)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnreadable, "unreadable", err.Error()), stdout, stderr)
	}
	signer, failEnv := loopSigner(*keyPath, "")
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	records, failEnv := verifiedChain(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	var res *trajectory.Result
	switch *deciderName {
	case "":
		res, err = trajectory.Replay(t, records, signer, *lanes)
	case "scripted":
		res, err = trajectory.ReplayWithDecider(t, records, signer, *lanes, decider.Scripted)
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown decider %q — scripted is the reference decider", *deciderName)), stdout, stderr)
	}
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	divergent := res.Divergent()
	same := len(res.Points) - len(divergent)
	result := map[string]any{
		"lane": res.Lane, "points": len(res.Points), "same": same, "diverged": divergent,
		"manifest": res.Manifest, "posture": res.Posture,
		"manifest_changed": res.ManifestChanged, "posture_changed": res.PostureChanged,
	}
	if !res.Diverged() {
		return render(stampTip(envelope.OK(result), len(records)), stdout, stderr)
	}
	var changed []string
	if res.ManifestChanged {
		changed = append(changed, "the manifest digest")
	}
	if res.PostureChanged {
		changed = append(changed, "the posture digest")
	}
	msg := fmt.Sprintf("trajectory of lane %s diverged: %d of %d point(s)", res.Lane, len(divergent), len(res.Points))
	for _, v := range divergent {
		msg += fmt.Sprintf("; %s %s on %s at %d: %s", v.Outcome, v.Verb, v.Subject, v.Position, v.Class)
		if v.Detail != "" {
			msg += " (" + v.Detail + ")"
		}
	}
	for _, c := range changed {
		msg += "; " + c + " changed"
	}
	env := envelope.Fail(envelope.ExitLaneInvalid, "trajectory_diverged", msg+" (spec/trajectories.md)")
	env.Result = result
	return render(stampTip(env, len(records)), stdout, stderr)
}
