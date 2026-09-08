package main

// The preseed (plans/os-0d4f2af3.md D1, D2, D5; charter §II.17,
// Appendix D.1): `seed init --preseed seed.json` bootstraps a
// deployment from the one declaration, idempotently — a second run
// appends nothing — and `seed preseed check` is the same comparison
// with no writes, which CI runs. Drift between the file and the chain
// refuses by name; a required member missing from the protected
// surface refuses as an incomplete declaration; init never edits
// history to match a file.

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/approval"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/lane"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// RequiredProtected is the protected surface the charter requires a
// declaration to enumerate (§II.14; spec/postures.md "The
// preseed"), as prefixes compared by string: the transition spec and
// every normative table, the admission rules, the standing and
// capability rules, verifier code and rubrics and the sealed-check
// machinery, the curator's gates and the policy stage, the role
// definitions, the check pipeline's own definitions, and the
// declaration itself (always on the surface by construction).
var RequiredProtected = []string{
	"spec",
	"internal/admit",
	"internal/transition",
	"internal/keyring",
	"internal/verdict",
	"internal/seal",
	"internal/eval",
	"evals",
	"internal/curation",
	"knowledge/lessons",
	"lanes",
	"cmd/seed-admit",
	"cmd/covergate",
	"Makefile",
	".github/workflows",
	"scripts",
}

// preseedDrift is a declaration the chain contradicts: the field and
// both values.
type preseedDrift struct {
	Field, Declared, Observed string
}

func (d *preseedDrift) Error() string {
	return fmt.Sprintf("preseed drift on %s: the declaration says %q, the chain has %q", d.Field, d.Declared, d.Observed)
}

// preseedIncomplete is a declaration missing something the charter
// requires it to say.
type preseedIncomplete struct{ Detail string }

func (e *preseedIncomplete) Error() string { return "preseed incomplete: " + e.Detail }

// fingerprintShape is a key fingerprint as event.Fingerprint renders
// it: the hex of a sha256, the shape an approvals entry's actor must
// have (the lint reads no ledger, so enrollment is admission's to
// judge).
var fingerprintShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// lintPreseed holds the declaration's content to the tables: tiers to
// the vocabulary, lane manifests to the shipped set, the protected
// surface to the required members, the protocol to the register. It
// reads no ledger.
func lintPreseed(cfg *posture.Config, lanesDir string) error {
	if cfg.Protocol != "" {
		known := false
		for _, v := range version.Supported() {
			if v == cfg.Protocol {
				known = true
			}
		}
		if !known {
			return &preseedIncomplete{Detail: fmt.Sprintf("protocol %q is not in this build's register (%s)", cfg.Protocol, strings.Join(version.Supported(), ", "))}
		}
	}
	if cfg.Guardrails != nil {
		for name, g := range cfg.Guardrails.Squads {
			for _, tier := range []string{g.Default, g.MaxAgent} {
				if _, ok := transition.Tier(tier); !ok {
					return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.squads.%s names tier %q, not in the vocabulary (%s)", name, tier, strings.Join(transition.TierOrder(), ", "))}
				}
			}
		}
		for _, f := range cfg.Guardrails.Paths {
			if _, ok := transition.Tier(f.Min); !ok {
				return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.paths %s names tier %q, not in the vocabulary", f.Prefix, f.Min)}
			}
		}
		// The approvals block (plans/os-5781a026.md D1): each entry
		// names a catalog verb that is not an approval verb, roster
		// kinds and a tier in their vocabularies.
		for _, a := range cfg.Guardrails.Approvals {
			if approval.IsApprovalVerb(a.Verb) {
				return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.approvals names %s, an approval verb: the three are never governed", a.Verb)}
			}
			known := false
			for _, v := range admit.CatalogVerbs() {
				if v == a.Verb {
					known = true
				}
			}
			if !known {
				return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.approvals names %q, which is no catalog verb (docs/generated/capabilities.md lists them)", a.Verb)}
			}
			for _, f := range a.Actors {
				if !fingerprintShape.MatchString(f) {
					return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.approvals.%s names actor %q, which is no key fingerprint (64 hex characters)", a.Verb, f)}
				}
			}
			for _, k := range a.Kinds {
				if !keyring.KnownKind(k) {
					return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.approvals.%s names kind %q, not a roster kind (%s)", a.Verb, k, strings.Join(keyring.Kinds(), ", "))}
				}
			}
			if a.MinTier != "" {
				if _, ok := transition.Tier(a.MinTier); !ok {
					return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.approvals.%s names min_tier %q, not in the vocabulary (%s)", a.Verb, a.MinTier, strings.Join(transition.TierOrder(), ", "))}
				}
			}
		}
		// A guardrail squad is a declared team, and an absent teams
		// block declares none.
		for name := range cfg.Guardrails.Squads {
			found := false
			if cfg.Teams != nil {
				for _, s := range cfg.Teams.Squads {
					if s.Name == name {
						found = true
					}
				}
			}
			if !found {
				return &preseedIncomplete{Detail: fmt.Sprintf("guardrails.squads.%s is not a declared team", name)}
			}
		}
	}
	if cfg.Teams != nil && lanesDir != "" {
		manifests, err := lane.Load(lanesDir)
		if err != nil {
			return fmt.Errorf("reading lane manifests: %w", err)
		}
		known := map[string]bool{}
		for _, m := range manifests {
			known[m.Lane] = true
		}
		for _, s := range cfg.Teams.Squads {
			for _, l := range s.Lanes {
				if !known[l] {
					return &preseedIncomplete{Detail: fmt.Sprintf("teams.squads.%s runs %q, which is not a manifest under %s", s.Name, l, lanesDir)}
				}
			}
		}
	}
	// A declared surface, empty included, is held to the required
	// members; only an absent block is undeclared.
	if cfg.Governance != nil || cfg.Protected != nil {
		for _, req := range RequiredProtected {
			if !cfg.Protects(req) {
				return &preseedIncomplete{Detail: fmt.Sprintf("protected omits %s, a member the charter requires on the surface (spec/postures.md)", req)}
			}
		}
	}
	return nil
}

// comparePreseed holds the chain to the declaration: the governance
// root, the protocol. It returns the versions still to activate (empty
// when the chain is at or past the declared protocol), or drift.
func comparePreseed(cfg *posture.Config, records []*event.Record) ([]string, error) {
	if len(records) == 0 {
		// The no-write comparison of an empty chain: nothing the
		// declaration names has been written, which is drift, not a
		// match; `seed init --preseed` is what writes it.
		return nil, &preseedDrift{Field: "genesis", Declared: "a chain at " + cfg.Protocol + " under " + describeRoot(cfg), Observed: "an empty chain"}
	}
	payload, err := genesis.Parse(records[0])
	if err != nil {
		return nil, fmt.Errorf("the chain's genesis does not parse: %w", err)
	}
	if cfg.Governance != nil {
		found := false
		for _, rk := range payload.GovernanceRoot {
			if rk.Fingerprint == cfg.Governance.Root {
				found = true
			}
		}
		if !found {
			roots := make([]string, 0, len(payload.GovernanceRoot))
			for _, rk := range payload.GovernanceRoot {
				roots = append(roots, rk.Fingerprint)
			}
			sort.Strings(roots)
			return nil, &preseedDrift{Field: "governance.root", Declared: cfg.Governance.Root, Observed: strings.Join(roots, ",")}
		}
	}
	ring, active, err := keyring.StateAt(records)
	if err != nil {
		return nil, err
	}
	// The posture the chain's history contradicts (D2): under the
	// forge-hosted posture the declared admission identity is the
	// ledger ref's sole writer, and a chain that has suspended or
	// revoked that key cannot hold the posture. A key the chain has
	// not enrolled yet is not a contradiction: enrollment follows the
	// preseed init that writes genesis.
	if cfg.Posture == posture.EnforcedForgeHosted && cfg.Admission != nil && cfg.Admission.Identity != "" {
		if entry, ok := ring.Get(cfg.Admission.Identity); ok && entry.Standing != keyring.StandingActive {
			return nil, &preseedDrift{Field: "posture", Declared: string(cfg.Posture) + " with admission identity " + cfg.Admission.Identity + " active",
				Observed: "the chain holds that identity " + string(entry.Standing)}
		}
	}
	if cfg.Protocol == "" {
		return nil, nil
	}
	supported := version.Supported()
	rank := func(v string) int {
		for i, s := range supported {
			if s == v {
				return i
			}
		}
		return -1
	}
	want, have := rank(cfg.Protocol), rank(active)
	if want < 0 {
		return nil, &preseedIncomplete{Detail: fmt.Sprintf("protocol %q is not in this build's register", cfg.Protocol)}
	}
	if have < 0 {
		return nil, &preseedDrift{Field: "protocol", Declared: cfg.Protocol, Observed: active}
	}
	if have > want {
		return nil, &preseedDrift{Field: "protocol", Declared: cfg.Protocol, Observed: active}
	}
	return supported[have+1 : want+1], nil
}

// applyPreseed brings an empty or matching ledger to the declaration:
// genesis under the signer when the ledger is empty, then every
// protocol version up to the declared one, in order. It returns what it
// appended. A ledger that disagrees with the file is drift, and nothing
// is written.
func applyPreseed(store *ledger.Store, cfg *posture.Config, signer ed25519.PrivateKey, extras []ed25519.PublicKey, now time.Time) ([]string, error) {
	var appended []string
	records, err := verifiedRecords(store)
	if err != nil {
		return nil, &preseedChain{Err: err}
	}
	fp, err := event.Fingerprint(signer.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		if cfg.Governance != nil && cfg.Governance.Root != fp {
			return nil, &preseedDrift{Field: "governance.root", Declared: cfg.Governance.Root, Observed: fp + " (the initializing key)"}
		}
		rec, err := genesis.Init(store, signer, extras, now)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
		appended = append(appended, "system.genesis")
	}
	todo, err := comparePreseed(cfg, records)
	if err != nil {
		return nil, err
	}
	if len(todo) == 0 {
		return appended, nil
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, err
	}
	_, active, err := keyring.StateAt(records)
	if err != nil {
		return nil, err
	}
	tip, _, err := store.Tip()
	if err != nil {
		return nil, err
	}
	for i, next := range todo {
		ring, _, err := keyring.StateAt(records)
		if err != nil {
			return nil, err
		}
		res := resolve
		if keyring.Applies(active) && ring.Seeded() {
			res = ring.Resolver()
		}
		rec, err := event.Sign(event.Event{
			V: active, TS: now.Add(time.Duration(i+1) * time.Second).UTC().Format(time.RFC3339), Actor: fp,
			Verb: ledger.UpgradeVerb, Subject: "system", Payload: []byte(`{"to": "` + next + `"}`), Prev: tip,
		}, signer)
		if err != nil {
			return nil, err
		}
		if _, err := store.Append(rec, res); err != nil {
			return nil, fmt.Errorf("activating %s: %w", next, err)
		}
		records = append(records, rec)
		tip, err = rec.Event.Hash()
		if err != nil {
			return nil, err
		}
		active = next
		appended = append(appended, ledger.UpgradeVerb+" "+next)
	}
	return appended, nil
}

// verifiedRecords is the chain verified from genesis, not merely
// parsed: a declaration is compared against, and extended over, a
// chain whose signatures, links and versions hold, or the comparison
// would bless a tampered chain and init would extend it. An empty
// store is an empty chain.
func verifiedRecords(store *ledger.Store) ([]*event.Record, error) {
	if _, count, err := store.Tip(); err != nil {
		return nil, err
	} else if count == 0 {
		return nil, nil
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, err
	}
	if _, err := store.VerifyFromGenesis(resolve); err != nil {
		return nil, err
	}
	var records []*event.Record
	if err := store.Records(func(pos int, r *event.Record) error { records = append(records, r); return nil }); err != nil {
		return nil, err
	}
	return records, nil
}

// preseedChain is a chain that does not verify from genesis, met by
// init --preseed: chain trouble, never drift.
type preseedChain struct{ Err error }

func (e *preseedChain) Error() string {
	return "the chain does not verify from genesis: " + e.Err.Error()
}

func preseedFailEnvelope(err error) *envelope.Envelope {
	var chain *preseedChain
	if errors.As(err, &chain) {
		return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error())
	}
	var drift *preseedDrift
	if errors.As(err, &drift) {
		return envelope.Fail(envelope.ExitDrift, "preseed_drift", err.Error())
	}
	var inc *preseedIncomplete
	if errors.As(err, &inc) {
		return envelope.Fail(envelope.ExitPostureInvalid, "preseed_incomplete", err.Error())
	}
	if errors.Is(err, ledger.ErrNotEmpty) {
		return envelope.Fail(envelope.ExitInvalidTransition, "ledger_not_empty", err.Error())
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}

func runPreseed(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "preseed takes the subverb check"), stdout, stderr)
	}
	fs := flag.NewFlagSet("preseed check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", posture.DeclarationPath, "deployment declaration")
	dir := fs.String("ledger", "", "ledger directory to compare the declaration against (omit to lint the file alone)")
	lanesDir := fs.String("lanes", "lanes", "lane manifests the teams block is checked against (empty to skip)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "preseed check [--config <file>] [--ledger <dir>] [--lanes <dir>]"), stdout, stderr)
	}
	cfg, failEnv := loadDeclarationFor(*config)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	if err := lintPreseed(cfg, *lanesDir); err != nil {
		return render(preseedFailEnvelope(err), stdout, stderr)
	}
	result := map[string]any{
		"config":     *config,
		"protocol":   cfg.Protocol,
		"governance": cfg.Governance != nil,
		"guardrails": cfg.Guardrails != nil,
		"teams":      cfg.Teams != nil,
		"protected":  cfg.ProtectedSurface(),
	}
	if *dir == "" {
		result["ledger"] = nil
		return render(envelope.OK(result), stdout, stderr)
	}
	var records []*event.Record
	var err error
	if entries, derr := os.ReadDir(*dir); derr == nil && len(entries) == 0 {
		// A directory with nothing in it is an empty chain, compared
		// as one rather than refused as unopenable.
		records = nil
	} else {
		store, failEnv := openStoreReadOnly(*dir)
		if failEnv != nil {
			return render(failEnv, stdout, stderr)
		}
		if records, err = verifiedRecords(store); err != nil {
			return render(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), stdout, stderr)
		}
	}
	todo, err := comparePreseed(cfg, records)
	if err != nil {
		return render(stampTip(preseedFailEnvelope(err), len(records)), stdout, stderr)
	}
	result["ledger"] = *dir
	result["pending"] = todo
	if len(todo) > 0 {
		env := envelope.Fail(envelope.ExitDrift, "preseed_drift", fmt.Sprintf("the chain has not activated %s the declaration names; `seed init --preseed` appends the missing activations", strings.Join(todo, ", ")))
		return render(stampTip(env, len(records)), stdout, stderr)
	}
	return render(stampTip(envelope.OK(result), len(records)), stdout, stderr)
}

func describeRoot(cfg *posture.Config) string {
	if cfg.Governance != nil && cfg.Governance.Root != "" {
		return cfg.Governance.Root
	}
	return "an undeclared root"
}
