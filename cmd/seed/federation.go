package main

// Uniform read remotes (plans/os-48df10a2.md D4; spec/requests.md
// "Federation"): a deployment declares the other ledgers it reads in
// its declaration's federation block, and `seed federation report`
// fetches every remote's ledger ref, verifies each chain from its own
// genesis under its own keyring (no key crosses: the resolver is the
// remote chain's own), folds each, and renders federation.json — the
// org view. There is no super-ledger, no verb names another ledger,
// and this command takes no key: the absence is the proof the drills
// assert (a federation read appends nothing anywhere).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// FederationRemoteView is one remote's row in federation.json. A
// remote that does not verify is reported with Verified false and
// the refusal, and nothing below the tip is derived from it.
type FederationRemoteView struct {
	Name     string `json:"name"`
	Remote   string `json:"remote"`
	Ref      string `json:"ref"`
	Verified bool   `json:"verified"`
	Error    string `json:"error,omitempty"`
	Tip      string `json:"tip,omitempty"`
	Count    int    `json:"count"`
	Protocol string `json:"protocol,omitempty"`
	Halted   bool   `json:"halted"`
	// ContractsByState counts the work subjects by lifecycle state.
	ContractsByState map[string]int `json:"contracts_by_state"`
	// EscalationsOpen counts the standing questions; RequestsUnanswered
	// the inbound requests nobody has answered.
	EscalationsOpen    int `json:"escalations_open"`
	RequestsUnanswered int `json:"requests_unanswered"`
}

// FederationTotals is the org line: sums over the verified remotes.
type FederationTotals struct {
	Remotes            int            `json:"remotes"`
	Verified           int            `json:"verified"`
	Unverified         int            `json:"unverified"`
	Contracts          int            `json:"contracts"`
	ContractsByState   map[string]int `json:"contracts_by_state"`
	EscalationsOpen    int            `json:"escalations_open"`
	RequestsUnanswered int            `json:"requests_unanswered"`
}

// FederationView is federation.json: byte-identical on the same tips,
// since everything in it derives from the verified chains and the
// declaration's order.
type FederationView struct {
	Remotes []FederationRemoteView `json:"remotes"`
	Totals  FederationTotals       `json:"totals"`
}

func runFederation(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "report" {
		got := ""
		if len(args) > 0 {
			got = fmt.Sprintf(" (got %q)", args[0])
		}
		return render(envelope.Fail(envelope.ExitUsage, "usage", "federation requires the subverb: report"+got), stdout, stderr)
	}
	fs := flag.NewFlagSet("federation report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "", "deployment declaration carrying the federation block")
	stateDir := fs.String("state", "", "client state dir: one subdirectory per remote, and federation.json")
	if err := fs.Parse(args[1:]); err != nil || *config == "" || *stateDir == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "federation report requires --config <file> --state <dir>"), stdout, stderr)
	}
	cfg, err := posture.Load(*config)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read the declaration: %v", err)), stdout, stderr)
	}
	if cfg.Federation == nil || len(cfg.Federation.Remotes) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "the declaration names no federation remotes (federation.remotes)"), stdout, stderr)
	}
	view := FederationView{Remotes: []FederationRemoteView{}, Totals: FederationTotals{ContractsByState: map[string]int{}}}
	for _, r := range cfg.Federation.Remotes {
		row := federatedRead(r, filepath.Join(*stateDir, "remotes", r.Name))
		view.Remotes = append(view.Remotes, row)
		view.Totals.Remotes++
		if !row.Verified {
			view.Totals.Unverified++
			continue
		}
		view.Totals.Verified++
		for state, n := range row.ContractsByState {
			view.Totals.ContractsByState[state] += n
			view.Totals.Contracts += n
		}
		view.Totals.EscalationsOpen += row.EscalationsOpen
		view.Totals.RequestsUnanswered += row.RequestsUnanswered
	}
	out, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	out = append(out, '\n')
	path := filepath.Join(*stateDir, "federation.json")
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	result["path"] = path
	return render(envelope.OK(result), stdout, stderr)
}

// federatedRead fetches one remote's ref into its own state
// subdirectory, materializes the tip, and verifies the chain from its
// genesis with the resolver that genesis bootstraps — the remote's
// own keyring, and nothing of this deployment's. Every failure is a
// row saying so; nothing is written but the client state.
func federatedRead(r posture.FederationRemote, stateDir string) FederationRemoteView {
	row := FederationRemoteView{Name: r.Name, Remote: r.Remote, Ref: r.Ref, ContractsByState: map[string]int{}}
	refName := r.Ref
	if refName == "" {
		refName = DefaultRemoteRef
		row.Ref = refName
	}
	fail := func(what string, err error) FederationRemoteView {
		row.Error = fmt.Sprintf("%s: %v", what, err)
		return row
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fail("state", err)
	}
	unlock, err := lockStateDir(stateDir)
	if err != nil {
		return fail("state", err)
	}
	defer unlock()
	client, err := gitref.NewClient(stateDir, r.Remote, refName)
	if err != nil {
		return fail("client", err)
	}
	tip, err := client.Fetch()
	if err != nil {
		return fail("fetch", err)
	}
	if tip == "" {
		return fail("fetch", fmt.Errorf("the ref is empty"))
	}
	workDir, err := os.MkdirTemp("", "seed-federation-*")
	if err != nil {
		return fail("materialize", err)
	}
	defer os.RemoveAll(workDir)
	if err := client.Materialize(tip, workDir); err != nil {
		return fail("materialize", err)
	}
	store, err := ledger.Open(workDir)
	if err != nil {
		return fail("open", err)
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return fail("genesis", err)
	}
	st, env := verdictStateAt(store, resolve)
	if env != nil {
		msg := ""
		if env.Error != nil {
			msg = env.Error.Message
		}
		return fail("verify", fmt.Errorf("%s", msg))
	}
	row.Verified = true
	row.Tip = st.tip
	row.Count = st.count
	row.Protocol = st.active
	row.Halted = halt.StateAt(st.records).Halted
	for _, subject := range st.fold.Subjects() {
		s, ok := st.fold.State(subject)
		if !ok {
			continue
		}
		row.ContractsByState[s.State]++
		if s.Escalation != nil {
			row.EscalationsOpen++
		}
	}
	for _, q := range st.fold.Requests() {
		if q.Answered == nil {
			row.RequestsUnanswered++
		}
	}
	return row
}
