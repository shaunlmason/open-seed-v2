// The maintenance verb (plans/os-8a5f14bb.md; spec/maintenance.md):
// one unattended pass — reap, lint, file, rebuild, checkpoint — run
// with no scheduler and no wake channel, and audited as an ordinary
// actor.
//
// Why this is a CLI verb although `seed loop run` was refused: the
// worker loop has no verb because Seed does not own the work, so a
// verb would invite treating the CLI as the agent. The maintenance
// lane's work IS Seed's own — reaping, reconciling, rebuilding and
// checkpointing are defined acts over the ledger with no caller
// judgment inside them, and there is no work step to supply. The
// decision logic still lives in internal/maintain with its effects
// injected; this file is the wiring.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/checkpoint"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/externalfact"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/maintain"
	"github.com/shaunlmason/open-seed-v2/internal/obligation"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/verdict"
)

func runMaintain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "maintain requires a subverb: run"), stdout, stderr)
	}
	switch args[0] {
	case "run":
		return runMaintainRun(args[1:], stdout, stderr)
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown maintain subverb %q — the maintenance lane runs one pass: run", args[0])), stdout, stderr)
	}
}

func runMaintainRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("maintain run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("ledger", "", "ledger directory")
	repo := fs.String("repo", "", "repository the merges landed in")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the maintenance actor")
	obsDir := fs.String("obs", "", "observation channel directory")
	out := fs.String("out", "", "projection output directory (default: no rebuild)")
	artifacts := fs.String("artifacts", "", "artifact store root (default <repo>/var/artifacts)")
	asOf := fs.String("as-of", "", "declared classification instant (RFC3339; defaults to now)")
	staleAfter := fs.Duration("stale-after", 0, "how long past its expiry an unrevalidated, unretired lesson stands before lesson_stale files it (default: on expiry)")
	forgeKind := fs.String("forge", "", "poll the forge for every submission under review that names a pull request: snapshot | github | forgejo (default: the observe step is skipped, with the reason)")
	github := fs.String("github", "", "owner/name of the repository (github or forgejo forge)")
	api := fs.String("api", "", "forge API base URL")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "environment variable holding the forge token")
	prSnapshot := fs.String("snapshot", "", "pull-request snapshot file (snapshot forge)")
	ceiling := fs.Int("return-ceiling", maintain.DefaultReturnCeiling, "observation-cited returns one subject may carry before the pass escalates instead")
	reofferTTL := fs.Duration("reoffer-ttl", maintain.DefaultReofferTTL, "how long a re-offer the pass publishes on a returned subject stays live, from the append's own instant")
	if err := fs.Parse(args); err != nil || *dir == "" || *repo == "" || *keyPath == "" || *obsDir == "" || fs.NArg() != 0 || *staleAfter < 0 || *ceiling <= 0 || *reofferTTL <= 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"maintain run requires --ledger <dir> --repo <dir> --key <path> --obs <dir> [--out <dir>] [--artifacts <dir>] [--as-of <ts>] [--stale-after <duration>] [--forge <kind> ...] [--return-ceiling <n>] [--reoffer-ttl <duration>]"), stdout, stderr)
	}
	var observe func(pr string) (externalfact.Observation, error)
	if *forgeKind != "" {
		reader, failEnv := mergeObserver(*forgeKind, *github, *api, *tokenEnv, *prSnapshot)
		if failEnv != nil {
			return render(failEnv, stdout, stderr)
		}
		observe = reader.Checks
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	now := time.Now().UTC()
	if *asOf != "" {
		parsed, err := time.Parse(time.RFC3339, *asOf)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage",
				fmt.Sprintf("--as-of %q is not RFC3339: %v", *asOf, err)), stdout, stderr)
		}
		now = parsed.UTC()
	}
	st, failEnv := loadVerdictState(*dir)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	snapshot, err := obs.Load(*obsDir)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	rows, err := project.DeriveObligations(st.records)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	store := artifact.Open(artifactsDir(*artifacts, *repo))

	sess := &maintainSession{dir: *dir, st: st, signer: signer}
	deps := maintain.Deps{
		Now:        now,
		StaleAfter: *staleAfter,
		Records:    st.records,
		Table:      st.table,
		Fold:       st.fold,
		Obs:        snapshot,
		Thresholds: obs.DefaultThresholds(),
		Store:      store,
		Repo:       *repo,
		Unseal: func(s transition.SubjectState) (*verdict.SealedInput, error) {
			in, fail := unsealChecks(st.records, s, signer, store)
			if fail != nil {
				return nil, errors.New(fail.Error.Message)
			}
			return in, nil
		},
		Obligations: rows,
		Corroborate: func(subject string, fence int) maintain.Corroboration {
			// The one shared derivation, from the package that owns
			// "did this fact pass the boundary at its own position".
			// A raw unprivileged interrupt corroborates nothing.
			s, ok := st.fold.State(subject)
			if !ok {
				return maintain.Corroboration{}
			}
			return maintain.Corroboration{
				Interrupted: admit.InterruptRequested(st.records, st.table, subject, s, fence),
				Wedged:      admit.WedgeDeclared(st.records, st.table, subject, fence),
				// A revoked holder can no longer act on its claim, so
				// the ledger corroborates the reap directly
				// (plans/os-32d06c65.md D3).
				Revoked: admit.RevokedHolder(st.records, st.table, subject, fence),
			}
		},
		Append: sess.append,
		File:   sess.file,
		// The forge poll and the fresh view the return reads
		// (plans/os-0cd18799.md D6): a pass with no forge reports the
		// observe step skipped, never passed over silently.
		Observe:       observe,
		ReturnCeiling: *ceiling,
		// The re-offer's seam (plans/os-29e2fef2.md D3): the pass
		// chooses the instant once and the record is signed at it, so
		// the offer's expires and its ts derive from one reading.
		AppendAt:   sess.appendAt,
		ReofferTTL: *reofferTTL,
		Refresh: func() ([]*event.Record, *transition.Fold, []obligation.Row, error) {
			fresh, failEnv := loadVerdictState(*dir)
			if failEnv != nil {
				return nil, nil, nil, errors.New(failEnv.Error.Message)
			}
			rows, err := project.DeriveObligations(fresh.records)
			if err != nil {
				return nil, nil, nil, err
			}
			return fresh.records, fresh.fold, rows, nil
		},
	}
	if *out != "" {
		deps.Rebuild = func() ([]string, error) { return sess.rebuild(*out) }
		deps.Materialize = func() ([]byte, int, error) { return sess.materialize() }
	}
	rep, err := maintain.Run(deps)
	if err != nil {
		return render(stampTip(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), sess.count()), stdout, stderr)
	}
	body, err := json.Marshal(rep)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	return render(stampTip(envelope.OK(result), sess.count()), stdout, stderr)
}

// maintainSession carries the effects. Each act re-derives the
// admission context from the CURRENT store rather than reusing the
// pass's opening view: a reap changes the state a later act is judged
// against, and judging the second act against the first act's world
// is the laundering shape every other verb here refuses.
type maintainSession struct {
	dir    string
	st     *verdictState
	signer ed25519.PrivateKey
	filed  int
}

func (m *maintainSession) count() int { return m.st.count }

// append signs one act and pushes it through the SAME admission
// boundary every other actor crosses. There is no maintenance
// bypass, which is what "audited as an ordinary actor" has to mean:
// a refusal here is reported, never retried and never worked around.
func (m *maintainSession) append(verb, subject string, payload []byte) error {
	return m.appendAt(time.Now().UTC(), verb, subject, payload)
}

// appendAt is append at a named instant: the re-offer step chooses
// the instant once and hands it to the payload and the record alike
// (plans/os-29e2fef2.md D3), so an offer whose expires is the instant
// plus the ttl can never be born dead against its own ts.
func (m *maintainSession) appendAt(at time.Time, verb, subject string, payload []byte) error {
	store, failEnv := openStore(m.dir)
	if failEnv != nil {
		return fmt.Errorf("%s", failEnv.Error.Message)
	}
	opts, failEnv := declaredAdmitOptions()
	if failEnv != nil {
		return fmt.Errorf("%s", failEnv.Error.Message)
	}
	ctx, err := admit.ContextAt(store, opts...)
	if err != nil {
		return err
	}
	fp, err := event.Fingerprint(m.signer.Public().(ed25519.PublicKey))
	if err != nil {
		return err
	}
	rec, err := event.Sign(event.Event{
		V: ctx.Active, TS: at.UTC().Format(time.RFC3339), Actor: fp,
		Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: ctx.Tip,
	}, m.signer)
	if err != nil {
		return err
	}
	if err := admit.Check(ctx, rec); err != nil {
		return err
	}
	pos, err := store.Append(rec, ctx.Resolve)
	if err != nil {
		return err
	}
	m.st.count = pos + 1
	m.st.tip = ctx.Tip
	return nil
}

// file turns a lint finding into a FILED DEFECT CONTRACT. The subject
// id is derived from the finding so a re-run files the same id and
// the ledger's own duplicate refusal makes filing idempotent, rather
// than the loop keeping a memory of what it filed. A maintenance loop
// that remembers is a maintenance loop that can forget.
func (m *maintainSession) file(f reconcile.Finding) (string, error) {
	id := maintain.DefectID(f)
	payload, err := json.Marshal(map[string]string{
		"intent":  fmt.Sprintf("defect %s on %s: %s", f.Class, f.Subject, f.Detail),
		"tier":    "trivial",
		"budget":  "small",
		"routing": "core",
	})
	if err != nil {
		return "", err
	}
	if err := m.append("intent.filed", id, payload); err != nil {
		return "", err
	}
	m.filed++
	return id, nil
}

func (m *maintainSession) rebuild(outDir string) ([]string, error) {
	store, failEnv := openStoreReadOnly(m.dir)
	if failEnv != nil {
		return nil, fmt.Errorf("%s", failEnv.Error.Message)
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, err
	}
	results, err := project.Rebuild(m.dir, outDir, project.Default(), resolve)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Name)
	}
	return names, nil
}

// materialize renders the canonical projection state the checkpoint
// attests to. It runs the SAME registered builders the rebuild
// publishes, so the snapshot cannot describe a different derivation
// from the one on disk.
func (m *maintainSession) materialize() ([]byte, int, error) {
	st, failEnv := loadVerdictState(m.dir)
	if failEnv != nil {
		return nil, 0, fmt.Errorf("%s", failEnv.Error.Message)
	}
	files := map[string][]byte{}
	for _, p := range project.Default() {
		built, err := p.Build(st.records, project.Inputs{})
		if err != nil {
			return nil, 0, err
		}
		for name, body := range built {
			files[p.Name+"/"+name] = body
		}
	}
	body, err := checkpoint.Materialize(st.count, st.tip, files)
	return body, st.count, err
}
