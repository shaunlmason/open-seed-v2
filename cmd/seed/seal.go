// The seal verbs (plans/os-3128535a.md; spec/sealed-checks.md):
// create authors one contract's sealed checks (salt, commit, encrypt
// to the current verifier keyring, append check.sealed); rotate
// re-encrypts every open sealed subject to the current keyring without
// touching history; audit is detection at exit 0 over the sealed
// bucket and the age headers' recipient tags. Cooperative dev tools
// over a local ledger, the established posture; admission enforces the
// same rules regardless.
package main

import (
	"bufio"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/erasure"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/seal"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
)

func runSeal(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "seal requires a subverb: create, rotate, or audit"), stdout, stderr)
	}
	switch args[0] {
	case "create":
		return runSealCreate(args[1:], stdout, stderr)
	case "rotate":
		return runSealRotate(args[1:], stdout, stderr)
	case "audit":
		return runSealAudit(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage", "seal requires a subverb: create, rotate, or audit"), stdout, stderr)
}

// unsealChecks loads and opens a subject's sealed checks with one
// identity, verifying the plaintext against the ledger's commitment.
// Nil input for an unsealed subject returns (nil, nil).
func unsealChecks(records []*event.Record, s transition.SubjectState, identity ed25519.PrivateKey, store *artifact.Store) (*verdict.SealedInput, *envelope.Envelope) {
	if s.Sealed == nil {
		return nil, nil
	}
	if failEnv := sealAuthorized(records, s.Sealed); failEnv != nil {
		return nil, failEnv
	}
	ct, err := store.GetSealed(s.Sealed.Commitment)
	if err != nil {
		// An erased body names its erasure: the refusal is the same,
		// and the reader learns who erased it and why rather than
		// meeting an absence (plans/os-db5cd353.md D4).
		if fact, ok := erasureOf(records, s.Sealed.Commitment); ok {
			return nil, envelope.Fail(envelope.ExitSealBroken, "seal_broken",
				fmt.Sprintf("the ciphertext for commitment %s was erased at position %d by %s (%s) — the sealed body a commitment points at must survive for a render; the erasure is the chain's attributable record of why it did not", s.Sealed.Commitment, fact.Pos, fact.Signer, fact.Reason))
		}
		return nil, envelope.Fail(envelope.ExitSealBroken, "seal_broken",
			fmt.Sprintf("the ciphertext for commitment %s is not retrievable: %v — the sealed body a commitment points at must survive (erasure is a surfaced state)", s.Sealed.Commitment, err))
	}
	pt, err := seal.Decrypt(ct, identity)
	if err != nil {
		var nre *seal.NotRecipientError
		if errors.As(err, &nre) {
			return nil, envelope.Fail(envelope.ExitNotRecipient, "not_recipient", err.Error())
		}
		return nil, envelope.Fail(envelope.ExitSealBroken, "seal_broken",
			fmt.Sprintf("the ciphertext for commitment %s does not open: %v", s.Sealed.Commitment, err))
	}
	env, commitment, err := seal.ParseEnvelope(pt)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitSealBroken, "seal_broken", err.Error())
	}
	if commitment != s.Sealed.Commitment {
		return nil, envelope.Fail(envelope.ExitSealBroken, "seal_broken",
			fmt.Sprintf("the decrypted envelope hashes to %s, the ledger committed %s — the sealed body is not what was committed", commitment, s.Sealed.Commitment))
	}
	return &verdict.SealedInput{Commitment: commitment, Checks: env.Checks}, nil
}

// eligibleRecipients derives the sealed-check recipient set: every
// active verdict-granted key that holds no implementation authority.
// The keyring deliberately allows one key to hold verdict and claim
// (L1 independence is per-contract, 6.1's design), but such a key must
// never be a seal recipient: it could decrypt every open contract's
// checks and then claim one, and the capability audit's invariant is
// that no implementer path can decrypt (review finding on the task
// PR). A dual-role key still renders verdicts under L1; it is simply
// not encrypted to.
func eligibleRecipients(ks *keyring.State) []keyring.Entry {
	var out []keyring.Entry
	for _, e := range ks.Granted(keyring.CapVerdict) {
		if e.Root || holdsAny(e.Grants, keyring.CapClaim, keyring.CapOperator) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func holdsAny(grants []string, caps ...string) bool {
	for _, g := range grants {
		for _, c := range caps {
			if g == c {
				return true
			}
		}
	}
	return false
}

func recipientKeys(entries []keyring.Entry) []ed25519.PublicKey {
	var pubs []ed25519.PublicKey
	for _, e := range entries {
		pubs = append(pubs, e.Key)
	}
	return pubs
}

// sealAuthorized replays the keyring to the seal's own position and
// checks the authoring boundary retroactively: the fold's window rule
// cannot see grants, so a raw seal planted by a sealer-less key inside
// the window folds as the fact and must be refused at use — the
// verdict-laundering pattern, replayed against seals (review finding
// on the task PR). seed reconcile surfaces the same condition as
// seal_unverified.
func sealAuthorized(records []*event.Record, sealed *transition.SealedFact) *envelope.Envelope {
	if sealed.Pos < 0 || sealed.Pos >= len(records) {
		return envelope.Fail(envelope.ExitSealBroken, "seal_unauthorized",
			fmt.Sprintf("the seal cites position %d outside the verified chain", sealed.Pos))
	}
	ring, _, err := keyring.StateAt(records[:sealed.Pos])
	if err != nil || ring == nil ||
		!ring.HasAnyCapability(sealed.Signer, keyring.AcceptedCapabilities(transition.CheckSealedVerb)) {
		return envelope.Fail(envelope.ExitSealBroken, "seal_unauthorized",
			fmt.Sprintf("the seal at position %d was signed by %s, which held no sealer grant there — it never passed the authoring boundary and will not be unsealed", sealed.Pos, sealed.Signer))
	}
	return nil
}

func readChecksFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var checks []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		checks = append(checks, line)
	}
	return checks, sc.Err()
}

func runSealCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("seal create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	subject := fs.String("subject", "", "contract in ready, before any claim")
	repo := fs.String("repo", "", "repository whose artifact store holds the ciphertext")
	checksPath := fs.String("checks", "", "file of sealed checks, one command per line (# and blank lines skipped)")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the sealer")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	if err := fs.Parse(args); err != nil || *dir == "" || *subject == "" || *repo == "" || *checksPath == "" || *keyPath == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "seal create requires --ledger <dir> --subject <id> --repo <dir> --checks <file> --key <path> [--artifacts <dir>]"), stdout, stderr)
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err)), stdout, stderr)
	}
	checks, err := readChecksFile(*checksPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --checks: %v", err)), stdout, stderr)
	}
	if len(checks) == 0 {
		// The vacuous-seal hole (review finding on the 6.3 plan): an
		// empty seal would mark the contract sealed while running zero
		// secret checks. Silence must never decide.
		return render(envelope.Fail(envelope.ExitSpecUnrunnable, "spec_unrunnable",
			fmt.Sprintf("--checks %s yields no commands — an empty seal would pass vacuously, so creation refuses", *checksPath)), stdout, stderr)
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
	if ctx.Keyring == nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", "the keyring is not active — sealed checks need the seed/1 capability boundary"), stdout, stderr)
	}
	recipients := recipientKeys(eligibleRecipients(ctx.Keyring))
	if len(recipients) == 0 {
		return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "unavailable",
			"the verifier keyring holds no eligible recipient — sealed checks encrypt to verdict-granted keys disjoint from claim and operator, and an empty recipient set can never be unsealed"), ctx.Count), stdout, stderr)
	}
	env, err := seal.NewEnvelope(checks)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	plaintext, err := env.Canonical()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	commitment, err := env.Commitment()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	ct, err := seal.Encrypt(plaintext, recipients)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	// Ciphertext first, event second: a refused append leaves an
	// orphan file, never a commitment without its body.
	if err := artifact.Open(artifactsDir(*artifacts, *repo)).PutSealed(commitment, ct); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	payload, err := json.Marshal(map[string]string{"commitment": commitment})
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	rec, err := event.Sign(event.Event{
		V:       ctx.Active,
		TS:      time.Now().UTC().Format(time.RFC3339),
		Actor:   fp,
		Verb:    transition.CheckSealedVerb,
		Subject: *subject,
		Payload: json.RawMessage(payload),
		Prev:    ctx.Tip,
	}, signer)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot sign the commitment: %v", err)), stdout, stderr)
	}
	if err := admit.Check(ctx, rec); err != nil {
		return render(journalAttempt(stampTip(stampAffordances(remoteFailureEnvelope(err), *dir, signer, *subject), ctx.Count), *dir, signer, "check.sealed", *subject, []byte(payload)), stdout, stderr)
	}
	pos, err := store.Append(rec, ctx.Resolve)
	if err != nil {
		return render(stampAffordances(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), *dir, signer, *subject), stdout, stderr)
	}
	return render(journalAttempt(stampTip(stampAffordances(envelope.OK(map[string]any{
		"subject":    *subject,
		"commitment": commitment,
		"checks":     fmt.Sprintf("%d", len(checks)),
		"recipients": fmt.Sprintf("%d", len(recipients)),
	}), *dir, signer, *subject), pos+1), *dir, signer, "check.sealed", *subject, []byte(payload)), stdout, stderr)
}

// terminalState reports a folded state no rotation or audit touches:
// exposure is bounded to contracts open during a compromise window.
func terminalState(state string) bool { return state == "done" || state == "cancelled" }

func runSealRotate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("seal rotate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	repo := fs.String("repo", "", "repository whose artifact store holds the ciphertexts")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key able to unseal (a current recipient)")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	if err := fs.Parse(args); err != nil || *dir == "" || *repo == "" || *keyPath == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "seal rotate requires --ledger <dir> --repo <dir> --key <path> [--artifacts <dir>]"), stdout, stderr)
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	identity, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err)), stdout, stderr)
	}
	st, ks, failEnv := loadSealState(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	recipients := recipientKeys(eligibleRecipients(ks))
	if len(recipients) == 0 {
		return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "unavailable",
			"the verifier keyring holds no eligible recipient — rotating to an empty recipient set would orphan every seal"), st.count), stdout, stderr)
	}
	store := artifact.Open(artifactsDir(*artifacts, *repo))
	rotated, skipped := []string{}, []string{}
	failed := []map[string]string{}
	for _, id := range st.fold.Subjects() {
		s, ok := st.fold.State(id)
		if !ok || s.Sealed == nil {
			continue
		}
		if terminalState(s.State) {
			skipped = append(skipped, id)
			continue
		}
		ct, err := store.GetSealed(s.Sealed.Commitment)
		if err != nil {
			failed = append(failed, map[string]string{"subject": id, "reason": fmt.Sprintf("ciphertext not retrievable: %v", err)})
			continue
		}
		pt, err := seal.Decrypt(ct, identity)
		if err != nil {
			failed = append(failed, map[string]string{"subject": id, "reason": err.Error()})
			continue
		}
		if _, commitment, perr := seal.ParseEnvelope(pt); perr != nil || commitment != s.Sealed.Commitment {
			reason := "the decrypted envelope does not verify against the committed hash"
			if perr != nil {
				reason = perr.Error()
			}
			failed = append(failed, map[string]string{"subject": id, "reason": reason})
			continue
		}
		next, err := seal.Encrypt(pt, recipients)
		if err != nil {
			failed = append(failed, map[string]string{"subject": id, "reason": err.Error()})
			continue
		}
		if err := store.PutSealed(s.Sealed.Commitment, next); err != nil {
			failed = append(failed, map[string]string{"subject": id, "reason": err.Error()})
			continue
		}
		rotated = append(rotated, id)
	}
	return render(stampTip(envelope.OK(map[string]any{
		"rotated":    rotated,
		"skipped":    skipped,
		"failed":     failed,
		"recipients": fmt.Sprintf("%d", len(recipients)),
	}), st.count), stdout, stderr)
}

func runSealAudit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("seal audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	repo := fs.String("repo", "", "repository whose artifact store holds the ciphertexts")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	if err := fs.Parse(args); err != nil || *dir == "" || *repo == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "seal audit requires --ledger <dir> --repo <dir> [--artifacts <dir>]"), stdout, stderr)
	}
	st, ks, failEnv := loadSealState(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	expected := map[string]bool{}
	for _, e := range eligibleRecipients(ks) {
		tag, err := seal.Tag(e.Key)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
		expected[tag] = true
	}
	store := artifact.Open(artifactsDir(*artifacts, *repo))
	findings := []map[string]string{}
	byClass := map[string]int{}
	add := func(subject, class, detail string) {
		findings = append(findings, map[string]string{"subject": subject, "class": class, "detail": detail})
		byClass[class]++
	}
	// An honored erasure is not a finding (plans/os-db5cd353.md D4):
	// a missing ciphertext whose commitment the chain holds an
	// artifact.erased for is listed here with its attribution, and the
	// audit stays clean; one with no record stays seal_evidence_missing,
	// the unattributed absence III.A row 7 forbids.
	erased := []map[string]string{}
	checked := 0
	for _, id := range st.fold.Subjects() {
		s, ok := st.fold.State(id)
		if !ok || s.Sealed == nil || terminalState(s.State) {
			continue
		}
		checked++
		ct, err := store.GetSealed(s.Sealed.Commitment)
		if err != nil {
			// Only a tombstone whose signer held the grant at its own
			// position is honored (admit.ErasureValid, the
			// sealAuthorized posture; review finding on the task PR):
			// a raw record under a key without it leaves the absence
			// unattributed, and a finding.
			if fact, ok := admit.Erasure(st.records, st.fold, s.Sealed.Commitment); ok {
				erased = append(erased, map[string]string{"subject": id, "commitment": s.Sealed.Commitment, "position": fmt.Sprintf("%d", fact.Pos), "by": fact.Signer, "reason": fact.Reason, "ts": fact.TS})
				continue
			}
			add(id, "seal_evidence_missing", fmt.Sprintf("the ciphertext for commitment %s is not retrievable: %v — erasure is a surfaced state, never silence", s.Sealed.Commitment, err))
			continue
		}
		tags, err := seal.RecipientTags(ct)
		if err != nil {
			add(id, "seal_evidence_missing", fmt.Sprintf("the stored bytes for commitment %s are not an age ciphertext: %v", s.Sealed.Commitment, err))
			continue
		}
		present := map[string]bool{}
		for _, t := range tags {
			present[t] = true
			if !expected[t] {
				add(id, "recipient_foreign", fmt.Sprintf("recipient tag %s matches no current verdict-granted key — a key outside the verifier keyring can unseal", t))
			}
		}
		for tag := range expected {
			if !present[tag] {
				add(id, "recipients_stale", fmt.Sprintf("current verifier tag %s is not among the seal's recipients — run seal rotate to re-encrypt to the current keyring", tag))
			}
		}
	}
	return render(stampTip(envelope.OK(map[string]any{
		"subjects": fmt.Sprintf("%d", checked),
		"findings": findings,
		"by_class": byClass,
		"erased":   erased,
		"clean":    fmt.Sprintf("%t", len(findings) == 0),
	}), st.count), stdout, stderr)
}

// erasureOf finds the erasure of a commitment among the records: the
// first artifact.erased whose payload names it and whose signer held
// the grant at its own position (admit.ErasureValid), read the way the
// fold reads it, so a render's refusal can attribute the absence it
// meets and never cites a tombstone the boundary would have refused.
func erasureOf(records []*event.Record, commitment string) (transition.ErasureFact, bool) {
	for pos, rec := range records {
		if rec.Event.Verb != erasure.Verb {
			continue
		}
		p, err := erasure.Parse(rec.Event.Subject, rec.Event.Payload)
		if err != nil || p.Artifact != commitment {
			continue
		}
		fact := transition.ErasureFact{Pos: pos, TS: rec.Event.TS, Signer: rec.Event.Actor, Subject: rec.Event.Subject, Artifact: p.Artifact, Reason: p.Reason}
		if admit.ErasureValid(records, fact) {
			return fact, true
		}
	}
	return transition.ErasureFact{}, false
}

// loadSealState is loadVerdictState plus the tip keyring the seal
// verbs consult for recipients.
func loadSealState(dir string) (*verdictState, *keyring.State, *envelope.Envelope) {
	st, failEnv := loadVerdictState(dir)
	if failEnv != nil {
		return nil, nil, failEnv
	}
	ks, _, err := keyring.StateAt(st.records)
	if err != nil {
		return nil, nil, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
	}
	if ks == nil {
		return nil, nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", "the keyring is not active — sealed checks need the seed/1 capability boundary")
	}
	return st, ks, nil
}
