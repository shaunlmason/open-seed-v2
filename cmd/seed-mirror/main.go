// Command seed-mirror is the projection-only issue mirror
// (plans/os-b45c308d.md D1; SEED-NEXT.md III.D rows 5 and 6): it reads
// the contracts projection `seed project current` resolved and plans
// or applies the one-way export of its rows to a forge's issues. It is a separate deployable
// component from `seed` by design: it takes no ledger, no Seed key, no
// remote and no admission endpoint, and its import closure holds no
// coordination write path (internal/authoritylint pins that). The only
// way a mirror-side edit re-enters Seed is `seed request file`, a
// different component, signed by a governed identity and judged at
// admission.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/shaunlmason/open-seed-v2/mirror"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 64
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage(stderr io.Writer, msg string) int {
	fmt.Fprintln(stderr, msg)
	fmt.Fprintln(stderr, "usage: seed-mirror plan|apply --current <file> --forge github|forgejo|snapshot [--owner <o> --repo <r>] [--base-url <url>] [--token-env <VAR>] [--snapshot <file>]")
	fmt.Fprintln(stderr, "  --current is the envelope `seed project current --name contracts` printed, which names the published build")
	return exitUsage
}

// run dispatches one subverb and prints one JSON document on stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usage(stderr, "seed-mirror requires a subverb: plan or apply")
	}
	sub := args[0]
	if sub != "plan" && sub != "apply" {
		return usage(stderr, fmt.Sprintf("unknown subverb %q: the mirror has two verbs, plan and apply", sub))
	}
	fs := flag.NewFlagSet("seed-mirror "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	current := fs.String("current", "", "the `seed project current --name contracts` envelope, saved to a file")
	forge := fs.String("forge", "", "the exporter: github | forgejo | snapshot")
	owner := fs.String("owner", "", "repository owner (github, forgejo)")
	repo := fs.String("repo", "", "repository name (github, forgejo)")
	baseURL := fs.String("base-url", "", "forge API base URL (forgejo requires it; github defaults to api.github.com)")
	tokenEnv := fs.String("token-env", "", "environment variable holding the forge token (default "+mirror.GitHubTokenEnv+" or "+mirror.ForgejoTokenEnv+")")
	snapshot := fs.String("snapshot", "", "issue snapshot file (snapshot exporter)")
	if err := fs.Parse(args[1:]); err != nil {
		return usage(stderr, err.Error())
	}
	if fs.NArg() != 0 || *forge == "" || *current == "" {
		return usage(stderr, "--current <file> and --forge <exporter> are required")
	}
	rows, stamp, err := mirror.Load(*current)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	adapter, err := mirror.Open(*forge, mirror.Config{Owner: *owner, Repo: *repo, BaseURL: *baseURL, TokenEnv: *tokenEnv, Snapshot: *snapshot})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	ctx := context.Background()
	existing, err := adapter.List(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	plan, err := mirror.Compute(mirror.Render(rows, stamp), existing, stamp)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	out := map[string]any{"exporter": adapter.Name(), "plan": plan}
	code := exitOK
	if sub == "apply" {
		applied, err := mirror.Apply(ctx, adapter, plan)
		out["applied"] = applied
		if err != nil {
			out["error"] = err.Error()
			code = exitFailure
		}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}
	return code
}
