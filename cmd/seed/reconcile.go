// The reconcile verb (plans/os-6cdc15be.md; spec/reconciliation.md):
// divergence detection as an invocable surface. The record-derivable
// classes come from internal/reconcile over the fold; the
// evidence-grade checks run only here, where the artifact store and
// the repository may be read: attested-head reconciliation against
// the cited receipt, and target-rewrite detection against the
// repository's checked-out default branch tip. Detection is a report
// at exit 0, never a refusal; Phase 9's maintenance loop consumes the
// same output on a schedule.
package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/curation"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
)

func runReconcile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	repo := fs.String("repo", "", "repository the merges landed in")
	subject := fs.String("subject", "", "one contract (default: every folded subject)")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of an identity able to unseal, for sealed L3 verdicts")
	if err := fs.Parse(args); err != nil || *dir == "" || *repo == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "reconcile requires --ledger <dir> --repo <dir> [--subject <id>] [--artifacts <dir>] [--key <path>]"), stdout, stderr)
	}
	var identity ed25519.PrivateKey
	if *keyPath != "" {
		keyBytes, err := os.ReadFile(*keyPath)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
		}
		if identity, err = event.ParsePrivateKey(keyBytes); err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err)), stdout, stderr)
		}
	}
	st, failEnv := loadVerdictState(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	subjects := st.fold.Subjects()
	if curation.IsHypothesisSubject(*subject) {
		// A hypothesis subject: the lesson check below is the whole
		// of its evidence grade.
		subjects = nil
	} else if *subject != "" {
		if _, ok := st.fold.State(*subject); !ok {
			return render(stampTip(envelope.Fail(envelope.ExitNotFound, "not_found",
				fmt.Sprintf("no contract %s in the fold", *subject)), st.count), stdout, stderr)
		}
		subjects = []string{*subject}
	}
	store := artifact.Open(artifactsDir(*artifacts, *repo))
	verdictFindings := map[string][]reconcile.Finding{}
	for _, f := range reconcile.VerifyVerdicts(st.records, st.fold) {
		verdictFindings[f.Subject] = append(verdictFindings[f.Subject], f)
	}
	for _, f := range reconcile.VerifySeals(st.records, st.fold) {
		verdictFindings[f.Subject] = append(verdictFindings[f.Subject], f)
	}
	for _, f := range reconcile.VerifyOverrides(st.records, st.fold) {
		verdictFindings[f.Subject] = append(verdictFindings[f.Subject], f)
	}
	// The L3 reproduction opens a sealed subject only under --key, and
	// there is no silent partial verification (the `verdict check`
	// posture): a sealed L3 verdict this run cannot open refuses the
	// run, naming the subject and what it needs.
	var refusal *envelope.Envelope
	rep := reconcile.Reproduction{Records: st.records, Fold: st.fold,
		NotAttempted: func(subject, why string) {
			if refusal == nil {
				refusal = envelope.Fail(envelope.ExitUsage, "usage",
					fmt.Sprintf("contract %s carries sealed checks and an L3 verdict — evidence-grade reconcile needs --key with an identity able to unseal, or the receipt cannot be recomputed (%s)", subject, why))
			}
		}}
	if identity != nil {
		rep.Unseal = func(s transition.SubjectState) (*verdict.SealedInput, error) {
			in, fail := unsealChecks(st.records, s, identity, store)
			if fail != nil {
				if refusal == nil {
					refusal = fail
				}
				return nil, errors.New(fail.Error.Message)
			}
			return in, nil
		}
	}
	var findings []reconcile.Finding
	checked := 0
	for _, id := range subjects {
		s, ok := st.fold.State(id)
		if !ok {
			continue
		}
		checked++
		findings = append(findings, reconcile.Subject(id, s)...)
		findings = append(findings, verdictFindings[id]...)
		findings = append(findings, reconcile.EvidenceAt(id, s, store, *repo, rep)...)
		if refusal != nil {
			return render(stampTip(refusal, st.count), stdout, stderr)
		}
	}
	if *subject == "" {
		findings = append(findings, reconcile.Lessons(st.records, st.fold, *repo, time.Now().UTC())...)
	} else if curation.IsHypothesisSubject(*subject) {
		for _, f := range reconcile.Lessons(st.records, st.fold, *repo, time.Now().UTC()) {
			if f.Subject == *subject {
				findings = append(findings, f)
			}
		}
	}
	if findings == nil {
		findings = []reconcile.Finding{}
	}
	byClass := map[string]int{}
	for _, f := range findings {
		byClass[f.Class]++
	}
	return render(stampTip(envelope.OK(map[string]any{
		"subjects": fmt.Sprintf("%d", checked),
		"findings": findings,
		"by_class": byClass,
		"clean":    fmt.Sprintf("%t", len(findings) == 0),
	}), st.count), stdout, stderr)
}
