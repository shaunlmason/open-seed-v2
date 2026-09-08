package main

// The deliberate body read (plans/os-8451d939.md D6; build plan Phase 9
// item 5(b)). `seed situation` says a message EXISTS; this returns one
// message's payload, at one cited position, to a caller it is addressed
// to.
//
// The split is the whole design. situation is read on every wake,
// unbidden, so a body there would let message.sent — the injection
// suite's named relaying residual, which needs no capability at all —
// steer a lane that never asked to be told anything. A read naming one
// position is the opposite case: the reader chose to look, after a
// notice told it who sent the thing and how big it is. That is the
// posture spec/lanes.md's residual analysis already accepts.
//
// Three things this verb is deliberately not:
//
//   - NOT a ledger verb. It appends nothing, so the build plan's "no
//     message.read verb is introduced" holds: what that forbids is a
//     FACT recording read-state, which would hand a lane a second
//     cursor to disagree with the one it already carries.
//   - NOT a new refusal code. A caller the message does not address
//     gets not_found, byte for byte what a position holding no message
//     gets, so THIS SURFACE does not become an oracle for what is
//     there. not_recipient (exit 23) is emphatically not reused: it
//     names the sealed-envelope recipient set, whose answer is "re-seal
//     to the current set" (spec/envelope.md's allocation rule
//     forbids sharing a code across two different answers).
//   - NOT confidentiality. Addressing is ROUTING (review finding on
//     #211). The ledger is plaintext and is the audit record by charter
//     design: the projections carry every payload verbatim, and
//     `seed ledger show --position P` returns any event to anyone with
//     read access to the repository, which is the same access these
//     reads need. A non-recipient can read any body there. What
//     not_found buys is that a lane acting through Seed verbs is routed
//     only its own mail, and that the message surface adds no second
//     oracle; a body that must be confidential is a sealed-checks
//     problem (spec/sealed-checks.md), not an addressing one.
//   - NOT a mailbox. One message at one position; the listing is
//     situation's job.

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)

func runMessage(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "message requires a subverb: read or send"), stdout, stderr)
	}
	switch args[0] {
	case "read":
		return runMessageRead(args[1:], stdout, stderr)
	case "send":
		return runMessageSend(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown message subverb %q — read or send", args[0])), stdout, stderr)
}

// recipientList collects a repeatable --to.
type recipientList []string

func (r *recipientList) String() string { return strings.Join(*r, ",") }

func (r *recipientList) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// fingerprintRE is the recipient form: addressing resolves against an
// actor's fingerprint (project.MessageNotice.Addresses), so a recipient
// that cannot be one is a typo that would otherwise admit and deliver
// to nobody, silently and un-notified: `to` parsed fine, it simply
// named no one who exists.
var fingerprintRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// runMessageSend appends one message.sent, the loop's relaying act.
//
// The verb exists so the cutover has one form to name rather than a
// hand-assembled payload: `docs/promotion.md` left mail as "the
// one loop act without a verb of its own", to be settled by naming the
// `ledger append` form or by adding the verb. This adds it, because the
// addressing contract is the part a hand-written payload gets wrong.
// Absent, present-and-parseable, and present-and-malformed are three
// different facts at the reading end (`project.AddressedTo`), and only
// the first two are reachable from here: this either omits `to`
// entirely (a broadcast) or writes an all-string array, so a sender
// cannot land in the undeliverable case by mistyping JSON.
//
// Everything else delegates to `ledger append`: the classification
// lint that bounds the body's size, both postures, the declaration and
// the persisted head. A second append path would be a second place for
// those to drift.
func runMessageSend(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("message send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var to recipientList
	fs.Var(&to, "to", "recipient fingerprint; repeatable; omitted addresses everyone")
	body := fs.String("body", "", "the message text")
	subject := fs.String("subject", "", "the contract the message concerns")
	// Passed through to `ledger append` untouched, so the two verbs
	// take one vocabulary and no flag means something different here.
	dir := fs.String("ledger", "", "ledger directory")
	remote := fs.String("remote", "", "remote ledger repository (cooperative posture)")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the sending actor")
	refName := fs.String("ref", DefaultRemoteRef, "remote ledger ref")
	stateDir := fs.String("state", "", "client state dir for the persisted verified head")
	config := fs.String("config", "", "deployment declaration")
	supported := fs.String("supported", "", "comma-separated supported protocol versions")
	if err := fs.Parse(args); err != nil || (*dir == "") == (*remote == "") || *keyPath == "" || *subject == "" || *body == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"message send requires --ledger <dir> or --remote <repo> (not both), --key, --subject and --body, with --to <fingerprint> repeatable"), stdout, stderr)
	}
	for _, r := range to {
		if !fingerprintRE.MatchString(r) {
			return render(envelope.Fail(envelope.ExitUsage, "usage",
				fmt.Sprintf("--to takes an actor fingerprint (64 lowercase hex), got %q: addressing resolves against fingerprints, so any other string admits and reaches nobody", r)), stdout, stderr)
		}
	}
	payload := map[string]any{"body": *body}
	if len(to) > 0 {
		payload["to"] = []string(to)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	forward := []string{
		"--key", *keyPath,
		"--verb", project.MessageSentVerb,
		"--subject", *subject,
		"--payload", string(encoded),
		"--ref", *refName,
	}
	if *dir != "" {
		forward = append(forward, "--ledger", *dir)
	} else {
		forward = append(forward, "--remote", *remote)
	}
	if *stateDir != "" {
		forward = append(forward, "--state", *stateDir)
	}
	if *config != "" {
		forward = append(forward, "--config", *config)
	}
	if *supported != "" {
		forward = append(forward, "--supported", *supported)
	}
	return runLedgerAppend(forward, stdout, stderr)
}

// messageNotFound is the ONE refusal this verb gives for every reason a
// caller does not get a body: no message at that position, an event
// that is not a message, a message addressed to someone else, and one
// whose addressing did not parse. Constructing it in a single place is
// what makes the indistinguishability a property rather than four
// strings that happen to match today.
func messageNotFound(at int) *envelope.Envelope {
	return envelope.Fail(envelope.ExitNotFound, "not_found",
		fmt.Sprintf("no message addressed to you at position %d", at))
}

func runMessageRead(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("message read", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	posture := bindReadPosture(fs)
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key: the actor the message must address")
	at := fs.String("at", "", "the ledger position of the message to read")
	if err := fs.Parse(args); err != nil || !posture.resolved() || *at == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"message read requires --ledger <dir> or --remote <repo> (not both), --key <path> and --at <position>"), stdout, stderr)
	}
	pos, err := strconv.Atoi(*at)
	if err != nil || pos < 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("--at takes a ledger position, got %q", *at)), stdout, stderr)
	}
	// A key is REQUIRED here, unlike situation's keyless whole-board
	// read: a body has a recipient set, and "no identity" addresses
	// nobody. The keyless read reports that mail exists; it does not
	// open it.
	if *keyPath == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"message read needs --key: a body is read as somebody, and a keyless read addresses no one"), stdout, stderr)
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot parse --key: %v", err)), stdout, stderr)
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	st, _, closePosture, failEnv := posture.open()
	defer closePosture()
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	if pos >= len(st.records) {
		return render(stampTip(messageNotFound(pos), st.count), stdout, stderr)
	}
	rec := st.records[pos]
	if rec.Event.Verb != project.MessageSentVerb {
		return render(stampTip(messageNotFound(pos), st.count), stdout, stderr)
	}
	to, undeliverable := project.AddressedTo(rec.Event.Payload)
	notice := project.MessageNotice{To: to, Undeliverable: undeliverable}
	if !notice.Addresses(fp) {
		return render(stampTip(messageNotFound(pos), st.count), stdout, stderr)
	}
	result := map[string]any{
		"from": rec.Event.Actor,
		"at":   fmt.Sprintf("%d", pos),
		"ts":   rec.Event.TS,
		"body": string(rec.Event.Payload),
	}
	// Same rule as the notice: the subject is a contract id or absent.
	// This is the deliberate read and the body is prose by definition,
	// but a field that CLAIMS to be an identifier must be one.
	if _, ok := st.fold.State(rec.Event.Subject); ok {
		result["subject"] = rec.Event.Subject
	}
	return render(stampTip(envelope.OK(result), st.count), stdout, stderr)
}
