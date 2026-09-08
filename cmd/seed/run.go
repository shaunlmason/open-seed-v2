// The supervisor's start verb (plans/os-8e53ffd9.md D3;
// spec/qualification.md; spec/executors.md step 2): seed run
// start appends the run.started that opens the spend bracket, deriving
// the fence from the active window and the reservation from the shared
// budget view exactly as the loop verbs do, and DECLARING the runtime
// tuple the run executes under. Two of its five fields come from the
// adapter's static report (harness, environment) and three are the
// caller's judgment (principal, model, tool policy), which the adapter
// cannot know and this verb never invents. Pre-flighted through the
// same admit.Check admission enforces, so a drift refusal reaches the
// caller as the boundary's own out_of_grant beside its affordances,
// before anything is signed.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/shaunlmason/open-seed-v2/executor/fakeoci"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/executor"
	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func runRun(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "run requires a subverb: start"), stdout, stderr)
	}
	switch args[0] {
	case "start":
		return runRunStart(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage",
		fmt.Sprintf("unknown run subverb %q — the spending gate has one act: start", args[0])), stdout, stderr)
}

// localAdapterName is the one adapter this build provisions through.
const localAdapterName = "local-worktree"

// startAdapter resolves --adapter to the adapter whose static report
// fills the fields a caller cannot honestly supply by hand. The three
// non-local substrates read their configuration from the declaration's
// executors block; an undeclared block refuses the adapter by name
// (plans/os-083112ac.md D3).
func startAdapter(name, configPath, stateDir string) (executor.Adapter, *envelope.Envelope) {
	if name == localAdapterName {
		return executor.LocalWorktree{}, nil
	}
	if name == executor.MockName {
		return executor.Mock{}, nil
	}
	known := map[string]bool{"container": true, "cloud-session": true, "remote-worker": true}
	if !known[name] {
		return nil, envelope.Fail(envelope.ExitUsage, "usage",
			fmt.Sprintf("unknown adapter %q — %s, mock, container, cloud-session or remote-worker (spec/executors.md)", name, localAdapterName))
	}
	cfg, failEnv := loadDeclarationFor(resolveConfigPath(configPath))
	if failEnv != nil {
		return nil, failEnv
	}
	ex := cfg.Executors
	if ex == nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("the %s adapter needs an executors block in the declaration", name))
	}
	switch name {
	case "container":
		if ex.Container == nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", "the container adapter needs executors.container in the declaration")
		}
		if ex.Container.Runtime == "fake" {
			return executor.Container{Runtime: fakeoci.New(), Image: ex.Container.Image, Fake: true}, nil
		}
		rt := executor.OCICommand{Bin: ex.Container.Runtime}
		if !rt.Available() {
			return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("the container runtime %q is not on PATH; declare runtime \"fake\" for the credential-free arm", ex.Container.Runtime))
		}
		return executor.Container{Runtime: rt, Image: ex.Container.Image}, nil
	case "cloud-session":
		if ex.Cloud == nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", "the cloud-session adapter needs executors.cloud in the declaration")
		}
		token := os.Getenv(ex.Cloud.Credential)
		if token == "" {
			return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("the cloud-session adapter needs a token in $%s", ex.Cloud.Credential))
		}
		return executor.CloudSession{Endpoint: ex.Cloud.Endpoint, Credential: ex.Cloud.Credential, Token: token}, nil
	case "remote-worker":
		if ex.Remote == nil || len(ex.Remote.Workers) == 0 {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", "the remote-worker adapter needs executors.remote.workers in the declaration")
		}
		if stateDir == "" {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", "the remote-worker adapter needs --state for the artifact store")
		}
		w := ex.Remote.Workers[0]
		return executor.RemoteWorker{ArtifactDir: filepath.Join(stateDir, "artifacts"), WorkerName: w.Name, Environment: w.Environment}, nil
	}
	return nil, envelope.Fail(envelope.ExitUsage, "usage", "unreachable")
}

// resolveConfigPath fills the declaration path default (the flag, then
// $SEED_CONFIG, then ./seed.json).
func resolveConfigPath(path string) string {
	if path != "" {
		return path
	}
	if env := os.Getenv("SEED_CONFIG"); env != "" {
		return env
	}
	return posture.DeclarationPath
}

// declaredFlag names the flag that supplies a tuple field the adapter
// cannot know, so a refusal can say what to pass rather than what is
// missing.
var declaredFlag = map[string]string{
	"principal":   "--principal",
	"model":       "--model",
	"tool_policy": "--tool-policy",
}

func runRunStart(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := bindLoopFlags(fs)
	adapterName := fs.String("adapter", localAdapterName, "executor adapter whose static report fills harness and environment")
	principal := fs.String("principal", "", "the principal the run executes as: a judgment the adapter cannot make")
	model := fs.String("model", "", "model family and version as <family>/<version>: a judgment the adapter cannot make")
	toolPolicy := fs.String("tool-policy", "", "the tool policy profile: a judgment the adapter cannot make")
	parseErr := fs.Parse(args)
	if env := f.usage("run start", parseErr, fs.NArg(), ""); env != nil {
		return render(env, stdout, stderr)
	}
	adapter, env := startAdapter(*adapterName, *f.config, *f.stateDir)
	if env != nil {
		return render(env, stdout, stderr)
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
	subject := *f.subject
	cited := -1
	var declared *tuple.Tuple
	// The payload is a derivation of the view and re-runs against the
	// refreshed tip inside the optimistic loop: the fence from the
	// active window, the sole open reservation, and, once the chain is
	// at seed/2, the declared tuple. The tuple is required there and
	// refused before it (spec/protocol.md), and the verb says so
	// in its own words rather than letting the boundary say it after
	// the flags were silently dropped.
	derive := func(ctx *admit.Context) ([]byte, *envelope.Envelope) {
		out := map[string]json.RawMessage{}
		if fence, ok := activeFence(ctx, subject); ok {
			out["fence"] = json.RawMessage(strconv.Quote(fence))
		}
		pos, refusal := soleOpenReservation(ctx, subject, "a run start")
		if refusal != nil {
			return nil, refusal
		}
		cited = pos
		out["reservation"] = json.RawMessage(strconv.Quote(fmt.Sprintf("%d", pos)))
		supplied := *principal != "" || *model != "" || *toolPolicy != ""
		switch {
		case tuple.Applies(ctx.Active):
			t := adapter.Tuple()
			t.Principal, t.Model, t.ToolPolicy = *principal, *model, *toolPolicy
			var missing []string
			for _, field := range tuple.Fields() {
				if t.Field(field) == "" {
					if flagName, ok := declaredFlag[field]; ok {
						missing = append(missing, flagName)
					} else {
						missing = append(missing, field+" (from the adapter)")
					}
				}
			}
			if len(missing) > 0 {
				return nil, envelope.Fail(envelope.ExitUsage, "usage",
					fmt.Sprintf("run start declares the runtime tuple and %s adapter %s reports only harness and environment: pass %s — the three fields an adapter cannot know are never invented (spec/qualification.md)",
						ctx.Active, *adapterName, strings.Join(missing, ", ")))
			}
			b, mErr := json.Marshal(t)
			if mErr != nil {
				return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", mErr.Error())
			}
			declared = &t
			out["tuple"] = b
		case supplied:
			return nil, envelope.Fail(envelope.ExitUsage, "usage",
				fmt.Sprintf("the chain is at %s and tuple semantics activate at %s: --principal, --model and --tool-policy declare a configuration this chain cannot yet judge (spec/protocol.md)",
					ctx.Active, version.Seed2))
		}
		b, mErr := json.Marshal(out)
		if mErr != nil {
			return nil, envelope.Fail(envelope.ExitUnavailable, "unavailable", mErr.Error())
		}
		return b, nil
	}
	payload, refusal := derive(ls.ctx)
	if refusal != nil {
		return render(ls.refuse(refusal, subject, transition.RunStartedVerb, nil, signer), stdout, stderr)
	}
	// The success names what was committed to: the reservation the run
	// spends under and the configuration it declared, which is what
	// Provision will hold the adapter to.
	resultAt := func(int) map[string]any {
		res := map[string]any{"subject": subject, "reservation": fmt.Sprintf("%d", cited)}
		if declared != nil {
			res["tuple"] = *declared
		}
		return res
	}
	return ls.commit(f, loopAct{verb: transition.RunStartedVerb, payload: payload, derive: derive,
		resultAt: resultAt}, signer, stdout, stderr)
}
