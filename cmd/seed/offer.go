// The supervisor's offer surface (plans/os-c61c3392.md;
// spec/offers.md; SEED-NEXT.md §II.9): publish appends the
// eligibility-scoped, expiring invitation; list is the worker's poll —
// the pull half of offers-not-assignments, whose total wake failure
// costs only latency. Liveness is derived here, never stored: ready
// subject, unexpired, no later claim, and a signer who held the
// supervise boundary at the offer's own position.

package main

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/offers"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
)

func runOffer(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "offer requires a subverb: publish, list or wake"), stdout, stderr)
	}
	switch args[0] {
	case "wake":
		return runOfferWake(args[1:], stdout, stderr)
	case "publish":
		return runOfferPublish(args[1:], stdout, stderr)
	case "list":
		return runOfferList(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage", "offer requires a subverb: publish, list or wake"), stdout, stderr)
}

// offerEligibility is the published eligibility scope: empty arrays
// mean unscoped (any active worker, any tier, any configuration), and
// omitempty keeps the canonical payload minimal. Tuples is the
// scheduling input III.J row 3 lands as (plans/os-8e53ffd9.md D6): the
// supervisor writes the configurations it wants into the offer, and a
// qualified worker sees it only if one of its cited tuples is named.
type offerEligibility struct {
	Capabilities []string      `json:"capabilities,omitempty"`
	Tiers        []string      `json:"tiers,omitempty"`
	Tuples       []tuple.Tuple `json:"tuples,omitempty"`
}

func runOfferPublish(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("offer publish", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	subject := fs.String("subject", "", "contract in ready to invite claims on")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the supervisor")
	expires := fs.String("expires", "", "RFC3339 expiry, strictly after now")
	var capabilities, tiers, tuples repeatedFlag
	fs.Var(&capabilities, "capability", "capability the taking worker must hold (repeatable; none = any active worker)")
	fs.Var(&tiers, "tier", "contract tier the offer covers (repeatable; none = any tier)")
	fs.Var(&tuples, "tuple", "runtime tuple a qualified taker's claim grant must cite, as the strict JSON object (repeatable; none = any configuration)")
	strongest := fs.Int("strongest", 0, "fill the tuples scope with the top n of the ranking for the one --capability (spec/ranking.md); refuses when nothing ranks")
	resume := fs.Bool("resume", false, "add the prior submitter's tuple to the tuples scope, derived from the subject's latest return by observation (spec/ranking.md \"Resume\"); refuses when none derives")
	if err := fs.Parse(args); err != nil || *dir == "" || *subject == "" || *keyPath == "" || *expires == "" || fs.NArg() != 0 || *strongest < 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "offer publish requires --ledger <dir> --subject <id> --key <path> --expires <RFC3339> [--capability c]... [--tier t]... [--tuple <json>]... [--strongest <n>] [--resume]"), stdout, stderr)
	}
	// Each --tuple is parsed at the door with the same strict parser
	// admission applies, so a malformed one refuses as usage here and
	// never becomes a signed record the boundary refuses later.
	var scoped []tuple.Tuple
	for _, raw := range tuples {
		t, err := tuple.Parse([]byte(raw))
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--tuple %s: %v", raw, err)), stdout, stderr)
		}
		scoped = append(scoped, t)
	}
	// --strongest is policy filling the same scope --tuple writes by
	// hand (plans/os-c7554f18.md D2): the two refuse together, and the
	// ranking read is the one --capability's.
	if *strongest > 0 && (len(scoped) > 0 || len(capabilities) != 1) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "--strongest fills the tuples scope from the ranking of exactly one --capability, and is not combined with --tuple"), stdout, stderr)
	}
	// The tuples scope is matched against the taker's CLAIM grants
	// (offers.md), so only the claim ranking can fill it (review
	// finding on the task PR): a verdict tuple named here would be one
	// no claimer cites, an offer nobody but an operator could see.
	if *strongest > 0 && capabilities[0] != keyring.CapClaim {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--strongest reads the claim ranking: an offer's tuples scope is matched against claim grants, so --capability %s cannot fill it (spec/ranking.md)", capabilities[0])), stdout, stderr)
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err)), stdout, stderr)
	}
	store, failEnv := openStore(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	opts, failEnv := declaredAdmitOptions()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ctx, err := admit.ContextAt(store, opts...)
	if err != nil {
		return render(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), stdout, stderr)
	}
	// The offer's own instant is the record's ts: the ranking derives
	// at it, never at a second clock read.
	ts := time.Now().UTC().Format(time.RFC3339)
	if *strongest > 0 {
		r, err := ranking.Derive(ranking.Inputs{Records: ctx.Records, Ring: ctx.Keyring, AsOf: ts})
		if err != nil {
			return render(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), stdout, stderr)
		}
		scoped = ranking.Top(r, capabilities[0], *strongest)
		if len(scoped) == 0 {
			// An unscoped offer is the supervisor's explicit choice,
			// never policy running out (D2).
			return render(envelope.Fail(envelope.ExitNotFound, "ranking_empty", fmt.Sprintf("no qualified %s tuple ranks at %s: publish without --strongest to offer unscoped, or qualify a configuration first (spec/ranking.md)", capabilities[0], ts)), stdout, stderr)
		}
	}
	// --resume is the per-subject preference beside the per-capability
	// ranking (plans/os-29e2fef2.md D2): the prior submitter's tuple
	// leads the scope, alone or beside what --strongest and --tuple
	// wrote, and an empty resumption refuses in the ranking_empty
	// posture rather than widening the offer.
	var resumption map[string]any
	if *resume {
		r, ok := ranking.Resume(ctx.Records, ctx.Lifecycle, *subject)
		if !ok {
			return render(envelope.Fail(envelope.ExitNotFound, "resume_empty", fmt.Sprintf("no configuration to resume on %s: %s (publish without --resume to scope by hand or unscoped; spec/ranking.md)", *subject, r.Because)), stdout, stderr)
		}
		lead := []tuple.Tuple{*r.Tuple}
		for _, t := range scoped {
			if !t.Equal(*r.Tuple) {
				lead = append(lead, t)
			}
		}
		scoped = lead
		resumption = map[string]any{"tuple": *r.Tuple, "holder": r.Holder, "return": strconv.Itoa(r.Return)}
	}
	payload, err := json.Marshal(struct {
		Eligibility offerEligibility `json:"eligibility"`
		Expires     string           `json:"expires"`
	}{offerEligibility{Capabilities: capabilities, Tiers: tiers, Tuples: scoped}, *expires})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	rec, err := event.Sign(event.Event{
		V:       ctx.Active,
		TS:      ts,
		Actor:   fp,
		Verb:    transition.OfferPublishedVerb,
		Subject: *subject,
		Payload: json.RawMessage(payload),
		Prev:    ctx.Tip,
	}, signer)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot sign the offer: %v", err)), stdout, stderr)
	}
	if err := admit.Check(ctx, rec); err != nil {
		return render(journalAttempt(stampTip(stampAffordances(remoteFailureEnvelope(err), *dir, signer, *subject), ctx.Count), *dir, signer, "offer.published", *subject, []byte(payload)), stdout, stderr)
	}
	pos, err := store.Append(rec, ctx.Resolve)
	if err != nil {
		return render(stampAffordances(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), *dir, signer, *subject), stdout, stderr)
	}
	result := map[string]any{
		"subject": *subject,
		"expires": *expires,
	}
	if resumption != nil {
		result["resumption"] = resumption
	}
	return render(journalAttempt(stampTip(stampAffordances(envelope.OK(result), *dir, signer, *subject), pos+1), *dir, signer, "offer.published", *subject, []byte(payload)), stdout, stderr)
}

func runOfferList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("offer list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	posture := bindReadPosture(fs)
	actor := fs.String("actor", "", "polling worker's fingerprint")
	nowFlag := fs.String("now", "", "RFC3339 liveness instant (default: now)")
	if err := fs.Parse(args); err != nil || !posture.resolved() || *actor == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "offer list requires --ledger <dir> or --remote <repo> (not both), --actor <fingerprint> [--now <RFC3339>]"), stdout, stderr)
	}
	now := time.Now().UTC()
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--now %q is not an RFC3339 timestamp", *nowFlag)), stdout, stderr)
		}
		now = parsed
	}
	st, ctx, closePosture, failEnv := posture.open()
	defer closePosture()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ring, _, err := keyring.StateAt(st.records)
	if err != nil {
		return render(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), stdout, stderr)
	}
	// One derivation (internal/offers): the fold's liveness, the
	// actor's standing, the signer's position-accurate authorization,
	// the scopes, and effective readiness (plans/os-f0ae2cdf.md D4), the
	// same predicate the queue and the claim boundary read.
	rows := offers.Live(st.records, admit.Topology(ctx), ring, *actor, now)
	return render(stampTip(envelope.OK(map[string]any{
		"actor":  *actor,
		"now":    now.Format(time.RFC3339),
		"offers": rows,
	}), st.count), stdout, stderr)
}

// runOfferWake is the supervisor's advisory bridge (plans/os-f0ae2cdf.md
// D6; spec/topology.md "Advisory wakes"): the subjects effectively
// ready at the tip and not at --since, each matched to the active
// actors eligible for a live offer on it. The CLI registers no wake
// channel (every shipped adapter's Wake is the documented no-op, and
// the worker pulls), so the pass reports its candidates and wakes
// nobody; a caller that holds channels drives offers.Bridge directly.
// Polling remains the correctness path: a pass that never runs loses
// latency, never a claim.
func runOfferWake(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("offer wake", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	posture := bindReadPosture(fs)
	since := fs.Int("since", -1, "the supervisor's last observed position, the delta cursor")
	nowFlag := fs.String("now", "", "RFC3339 liveness instant (default: now)")
	if err := fs.Parse(args); err != nil || !posture.resolved() || *since < 0 || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "offer wake requires --ledger <dir> or --remote <repo> (not both), --since <position> [--now <RFC3339>]"), stdout, stderr)
	}
	now := time.Now().UTC()
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--now %q is not an RFC3339 timestamp", *nowFlag)), stdout, stderr)
		}
		now = parsed
	}
	st, _, closePosture, failEnv := posture.open()
	defer closePosture()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	ring, _, err := keyring.StateAt(st.records)
	if err != nil {
		return render(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), stdout, stderr)
	}
	res := offers.Bridge(st.records, st.table, ring, now, *since, nil)
	return render(stampTip(envelope.OK(map[string]any{
		"since":        res.Since,
		"position":     res.Position,
		"now":          now.Format(time.RFC3339),
		"became_ready": res.BecameReady,
		"candidates":   res.Candidates,
		"woken":        res.Woken,
		"channels":     0,
		"advisory":     "polling is the correctness path; a wake is a hint to re-read, never a grant",
	}), st.count), stdout, stderr)
}
