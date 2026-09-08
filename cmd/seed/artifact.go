package main

// The erasure verb (plans/os-db5cd353.md; SEED-NEXT.md III.A row 7;
// spec/protocol.md "Erasure"): `artifact erase` records that an
// operator erased an artifact the chain references by digest, then
// removes the bytes from the store. The order is deliberate: a record
// with the bytes still present is a promise the next run keeps, bytes
// gone with no record is the silence the row forbids. The record is a
// loop act in the shared transport shape, judged by the boundary's
// erasure rule; an erasure that already stands is not recorded twice,
// and a re-run only finishes the removal, under the same grant the
// record took. A removal the store refuses after the record landed is
// a refusal of its own, never a success with a note.

import (
	"bytes"
	"crypto/ed25519"
	"flag"
	"fmt"
	"io"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/erasure"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

func runArtifact(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "artifact requires a subverb: erase"), stdout, stderr)
	}
	switch args[0] {
	case "erase":
		return runArtifactErase(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown artifact subverb %q — erase", args[0])), stdout, stderr)
}

// runArtifactErase appends artifact.erased on the contract that
// references the artifact (or on system), then empties the store's
// buckets under the digest and reports which.
func runArtifactErase(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("artifact erase", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	digest := fs.String("artifact", "", "the artifact's digest, a lowercase-hex sha256: the sealed commitment or the receipt the contract references")
	reason := fs.String("reason", "", fmt.Sprintf("one line naming the obligation honored, at most %d bytes", erasure.MaxReasonBytes))
	repo := fs.String("repo", "", "repository whose artifact store holds the bytes")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	parseErr := fs.Parse(args)
	missing := ""
	if *digest == "" || *reason == "" || (*repo == "" && *artifacts == "") {
		missing = "and --artifact <digest>, --reason <text>, --repo <dir> (or --artifacts <dir>); --subject is the contract that references the artifact, or system"
	}
	if env := f.usage("artifact erase", parseErr, fs.NArg(), missing); env != nil {
		return render(env, stdout, stderr)
	}
	payload, err := erasure.Render(erasure.Erased{Artifact: *digest, Reason: *reason})
	if err != nil {
		return render(envelope.Fail(envelope.ExitInvalidTransition, "erasure_refused", err.Error()), stdout, stderr)
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
	store := artifact.Open(artifactsDir(*artifacts, *repo))
	subject, art := *f.subject, *digest
	// The removal, after the record. What it emptied goes in the
	// result; what it could not remove is a refusal (review finding on
	// the task PR): the record stands and the bytes remain, which is
	// exactly the state the next run finishes, and a success envelope
	// would tell a caller the erasure is complete when it is not.
	var removed []string
	var removeErr error
	var recordedAt int
	remove := func(pos int, out map[string]any) map[string]any {
		recordedAt = pos
		removed, removeErr = store.Erase(art)
		if removed == nil {
			removed = []string{}
		}
		out["artifact"] = art
		out["erased_at"] = fmt.Sprintf("%d", pos)
		out["removed"] = removed
		return out
	}
	incomplete := func(stamp int) int {
		return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "erasure_incomplete",
			fmt.Sprintf("the erasure of %s is recorded at position %d and its bytes remain: %v (removed so far: %v); the record stands, so run the verb again once the store can remove them, which finishes the removal and is not recorded twice",
				art, recordedAt, removeErr, removed)), stamp), stdout, stderr)
	}
	// An erasure that already stands, on any subject, is finished and
	// not re-recorded: a second record would attribute an act that did
	// nothing, and the bytes may remain when a run died between the
	// append and the removal (plans/os-db5cd353.md D5). Only a record
	// that passed the boundary stands (admit.Erasure), and the resume
	// is the same governance act as the record, so the caller must hold
	// the grant the verb accepts here exactly as the grant rule holds it
	// at a fresh append (review finding on the task PR): a standing
	// record is never a way for a key without the grant to finish a
	// removal under the operator's name.
	if prior, ok := admit.Erasure(ls.ctx.Records, ls.ctx.Lifecycle, art); ok {
		fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
		}
		if accepted := keyring.AcceptedCapabilities(erasure.Verb); ls.ctx.Keyring == nil || !ls.ctx.Keyring.HasAnyCapability(fp, accepted) {
			return render(ls.refuse(remoteFailureEnvelope(&admit.OutOfGrantError{Actor: fp, Verb: erasure.Verb, Accepted: accepted}),
				subject, erasure.Verb, payload, signer), stdout, stderr)
		}
		out := remove(prior.Pos, map[string]any{"subject": prior.Subject, "by": prior.Signer, "recorded": false})
		if removeErr != nil {
			return incomplete(ls.ctx.Count)
		}
		return render(stampTip(envelope.OK(out), ls.ctx.Count), stdout, stderr)
	}
	// The shared commit renders the envelope it lands, so the success is
	// rendered into a buffer and forwarded only when the removal that ran
	// inside it succeeded; a refusal before the append ran no removal and
	// is forwarded as it is.
	var buf bytes.Buffer
	code := ls.commit(f, loopAct{verb: erasure.Verb, payload: payload, resultAt: func(pos int) map[string]any {
		return remove(pos, map[string]any{"subject": subject, "recorded": true})
	}}, signer, &buf, stderr)
	if removeErr != nil {
		return incomplete(recordedAt + 1)
	}
	if _, err := io.Copy(stdout, &buf); err != nil {
		fmt.Fprintf(stderr, "seed: envelope render failed: %v\n", err)
		return 1
	}
	return code
}
