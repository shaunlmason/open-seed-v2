package simulate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/loop"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// Config declares a simulation run.
type Config struct {
	// LanesDir is the directory of shipped lane manifests
	// (<root>/lanes); the deployment's identities and grants come
	// from it.
	LanesDir string
	// Verbs is the credential-free in-process CLI seam (loopVerbs).
	Verbs loop.Verbs
	// Intents is how many synthetic contracts to drive to done.
	Intents int
	// Seed makes the intent draw deterministic.
	Seed int64
	// Now is the base instant; expiry, spot-checks and the report read
	// it (and the accelerated clock advances from it). Admission reads
	// no clock.
	Now time.Time
	// Enforced selects enforced-self-hosted (the seed-admit pre-receive
	// hook on the remote) over cooperative.
	Enforced bool
	// Days, when > 0, runs the accelerated-clock backlog over that many
	// days (D5); 0 is a single instant.
	Days int
	// WorkDir is where the throwaway deployment is built; "" uses the
	// OS temp dir.
	WorkDir string
}

func (c Config) workRoot() string { return c.WorkDir }
func (c Config) now() time.Time {
	if c.Now.IsZero() {
		// The base instant defaults to now: admission stamps each event
		// with the real wall clock, so an offer's expiry (and the
		// genesis ts) must sit against that, not a fixed epoch. The
		// accelerated clock (--days) advances a declared instant for the
		// reporting surfaces (--now/--as-of), which is a separate axis.
		return time.Now().UTC()
	}
	return c.Now.UTC()
}

// IntentResult is the fate of one synthetic contract.
type IntentResult struct {
	Subject string `json:"subject"`
	State   string `json:"state"`
	Done    bool   `json:"done"`
}

// Report is the simulation's summary.
type Report struct {
	Posture string         `json:"posture"`
	Intents int            `json:"intents"`
	Done    int            `json:"done"`
	Days    int            `json:"days"`
	Results []IntentResult `json:"results"`
	Audit   *AuditResult   `json:"audit,omitempty"`
}

func postureName(enforced bool) string {
	if enforced {
		return "enforced-self-hosted"
	}
	return "cooperative"
}

// Run provisions the deployment, drives every synthetic intent to done
// through the real boundary, audits the ledger, and returns the report.
func Run(cfg Config) (*Report, error) {
	if cfg.Intents <= 0 {
		cfg.Intents = 1
	}
	d, err := build(cfg)
	if err != nil {
		return nil, err
	}
	cat := catalog(cfg.Seed)
	rep := &Report{Posture: postureName(cfg.Enforced), Intents: cfg.Intents, Days: cfg.Days}
	base := d.now
	for i := 0; i < cfg.Intents; i++ {
		subject := fmt.Sprintf("c-%d", i+1)
		intent := cat[i%len(cat)]
		// The accelerated clock (D5): arrivals spread across the declared
		// window, the reporting instant advancing per intent. Admission
		// still stamps each event with the real wall clock; the sim
		// instant only feeds the clock-reading surfaces (offer expiry,
		// the maintenance --as-of), which is the whole point — a clock is
		// a declared input, never a fact admission reads.
		at := base
		if cfg.Days > 0 {
			at = base.Add(time.Duration(i) * time.Duration(cfg.Days) * 24 * time.Hour / time.Duration(cfg.Intents))
		}
		repo, err := d.buildRepo(subject, intent)
		if err != nil {
			return nil, fmt.Errorf("%s repo: %w", subject, err)
		}
		if err := d.stageContract(subject, repo, at); err != nil {
			return nil, fmt.Errorf("%s stage: %w", subject, err)
		}
		if err := d.drive(subject, repo); err != nil {
			return nil, fmt.Errorf("%s drive: %w", subject, err)
		}
		state := d.stateOf(subject)
		res := IntentResult{Subject: subject, State: state, Done: state == "done"}
		if res.Done {
			rep.Done++
		}
		rep.Results = append(rep.Results, res)
	}
	// The curator and maintenance lanes run their passes once over the
	// completed backlog (proposals from what recurred; the reap pass).
	end := base
	if cfg.Days > 0 {
		end = base.Add(time.Duration(cfg.Days) * 24 * time.Hour)
	}
	d.curate()
	d.maintain(end)

	records, err := d.records()
	if err != nil {
		return nil, err
	}
	// The audit judges under the declaration the run admitted under
	// (D5), so the ceiling arm reads the claims the boundary took.
	decl, err := posture.Load(d.config)
	if err != nil {
		return nil, fmt.Errorf("the deployment's declaration: %w", err)
	}
	audit := AuditUnder(records, decl)
	rep.Audit = &audit
	return rep, nil
}

// stageContract has the dispatcher file the intent and specify its
// acceptance, and the supervisor publish an offer — every act admitted
// through the boundary under the lane's own key.
func (d *deployment) stageContract(subject string, r repo, at time.Time) error {
	if err := d.append1(d.keys["dispatcher"], "intent.filed", subject,
		`{"intent": "fix the check", "tier": "trivial", "budget": "small", "routing": "core"}`); err != nil {
		return err
	}
	if err := d.append1(d.keys["dispatcher"], "contract.specified", subject,
		fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, r.spec)); err != nil {
		return err
	}
	// The offer stays live through the run: its expiry sits a generous
	// window past both the arrival instant and the real event ts.
	horizon := at.Add(time.Hour)
	if real := time.Now().UTC().Add(time.Hour); real.After(horizon) {
		horizon = real
	}
	expires := horizon.UTC().Format(time.RFC3339)
	offer := fmt.Sprintf(`{"eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}, "expires": %q}`, expires)
	return d.append1(d.keys["supervisor"], "offer.published", subject, offer)
}

// drive runs the implementer's loop to a submission, the distinct
// verifier's verdict, and the observer's merge — the terminal chain.
func (d *deployment) drive(subject string, r repo) error {
	posture := []string{"--remote", d.remote, "--state", d.state, "--config", d.config}
	man := d.manifest["implementer"]
	drv, err := loop.New(man, d.verbs, posture, d.keys["implementer"],
		loop.WorkFunc(func(s string, sit loop.Situation) (int, error) {
			if !sit.Holds(s) {
				return 0, fmt.Errorf("the work step must run inside a held window")
			}
			return 3, nil
		}), loop.WithBase(r.base+".."+r.head))
	if err != nil {
		return err
	}
	step, err := drv.Step(5)
	if err != nil {
		return fmt.Errorf("implementer loop: %w", err)
	}
	if step.Outcome != loop.Submitted || step.Subject != subject {
		return fmt.Errorf("implementer did not submit %s: outcome %s (%s)", subject, step.Outcome, step.Cause.Message)
	}
	// The verifier is a distinct key; the receipt runs through the real
	// verdict machinery over the repo.
	if res := d.verbs.Run("verdict", "render", "--remote", d.remote, "--state", d.state, "--config", d.config,
		"--subject", subject, "--repo", r.src, "--key", d.keys["verifier"], "--verdict", "pass"); res.Exit != 0 {
		return fmt.Errorf("verdict render %s refused: %s: %s", subject, res.Code, res.Message)
	}
	if res := d.verbs.Run("merge", "request", "--remote", d.remote, "--state", d.state, "--config", d.config,
		"--subject", subject, "--key", d.keys["implementer"]); res.Exit != 0 {
		return fmt.Errorf("merge request %s refused: %s: %s", subject, res.Code, res.Message)
	}
	if res := d.verbs.Run("merge", "observe", "--remote", d.remote, "--state", d.state, "--config", d.config,
		"--subject", subject, "--key", d.keys["observer"], "--merged", r.head, "--pr", "pr/"+subject); res.Exit != 0 {
		return fmt.Errorf("merge observe %s refused: %s: %s", subject, res.Code, res.Message)
	}
	return nil
}

// curate has the curator propose from what recurred; a refusal is not
// fatal to the run (there may be nothing to propose).
func (d *deployment) curate() {
	_ = d.verbs.Run("knowledge", "propose", "--remote", d.remote, "--state", d.state, "--config", d.config,
		"--key", d.keys["curator"], "--claim", "fix-the-check contracts recur", "--kind", "pattern")
}

// maintain runs the maintenance pass over the backlog.
func (d *deployment) maintain(at time.Time) {
	_ = d.verbs.Run("maintain", "run", "--remote", d.remote, "--state", d.state,
		"--key", d.keys["maintenance"], "--as-of", at.UTC().Format(time.RFC3339))
}

// materialize fetches and materializes the remote ledger into a temp
// dir and returns that dir.
func (d *deployment) materialize() (string, error) {
	c, err := gitref.NewClient(filepath.Join(d.dir, "read-work"), d.remote, ledgerRef)
	if err != nil {
		return "", err
	}
	tip, err := c.Fetch()
	if err != nil {
		return "", err
	}
	out, err := os.MkdirTemp(d.dir, "materialized-")
	if err != nil {
		return "", err
	}
	if err := c.Materialize(tip, out); err != nil {
		return "", err
	}
	return out, nil
}

// stateOf folds the remote's own chain and reports the subject's state.
func (d *deployment) stateOf(subject string) string {
	records, err := d.records()
	if err != nil {
		return "unknown"
	}
	tbl, err := transition.Default()
	if err != nil {
		return "unknown"
	}
	s, ok := tbl.StateAt(records, subject)
	if !ok {
		return "absent"
	}
	return s.State
}
