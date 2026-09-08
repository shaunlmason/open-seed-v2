package main

// `seed protections plan | apply` (plans/os-5c8a312c.md D6): the forge's
// protections reconciled from the declaration. plan reads the forge and
// names every difference; apply performs the plan and re-reads. The
// snapshot forge is the credential-free arm CI runs; the github forge
// speaks to the rulesets API with a token from the environment.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/protections"
)

func runProtections(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "protections takes a subverb: plan | apply"), stdout, stderr)
	}
	switch args[0] {
	case "plan", "apply":
		return runProtectionsReconcile(args[0], args[1:], stdout, stderr)
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("unknown protections subverb %q (plan | apply)", args[0])), stdout, stderr)
	}
}

// loadDeclarationFor is the declaration read a verb that needs one
// makes: the path is explicit or the default, and every failure is the
// doctor's refusal for it.
func loadDeclarationFor(path string) (*posture.Config, *envelope.Envelope) {
	cfg, err := posture.Load(path)
	if err != nil {
		switch {
		case errors.Is(err, posture.ErrUndeclared):
			return nil, envelope.Fail(envelope.ExitNotFound, "posture_undeclared", err.Error())
		case errors.Is(err, posture.ErrUnreadable):
			return nil, envelope.Fail(envelope.ExitUnreadable, "posture_unreadable", err.Error())
		default:
			return nil, envelope.Fail(envelope.ExitPostureInvalid, "posture_invalid", err.Error())
		}
	}
	return cfg, nil
}

func runProtectionsReconcile(verb string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("protections "+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", posture.DeclarationPath, "deployment declaration")
	forgeKind := fs.String("forge", "snapshot", "forge adapter: snapshot | github | forgejo")
	snapshot := fs.String("snapshot", "", "the forge's state as a JSON file (snapshot forge)")
	repo := fs.String("repo", "", "working tree for CODEOWNERS and the workflow lint (omit to skip both)")
	github := fs.String("github", "", "owner/name of the repository (github or forgejo forge)")
	api := fs.String("api", "", "forge API base URL (github default https://api.github.com; forgejo default admission.api)")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "environment variable holding the forge token (github forge)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "protections "+verb+" [--config <file>] --forge snapshot --snapshot <file> [--repo <dir>] | --forge github|forgejo --github <owner/name> [--api <url>] [--token-env NAME] [--repo <dir>]"), stdout, stderr)
	}
	cfg, failEnv := loadDeclarationFor(*config)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	if cfg.Posture != posture.EnforcedForgeHosted {
		return render(envelope.Fail(envelope.ExitPostureInvalid, "posture_invalid", fmt.Sprintf("protections reconcile a %s deployment; this declaration says %q", posture.EnforcedForgeHosted, cfg.Posture)), stdout, stderr)
	}
	var forge protections.Forge
	switch *forgeKind {
	case "snapshot":
		if *snapshot == "" {
			return render(envelope.Fail(envelope.ExitUsage, "usage", "the snapshot forge needs --snapshot <file>"), stdout, stderr)
		}
		forge = protections.Snapshot{Path: *snapshot}
	case "github":
		owner, name, ok := strings.Cut(*github, "/")
		if !ok || owner == "" || name == "" {
			return render(envelope.Fail(envelope.ExitUsage, "usage", "the github forge needs --github <owner/name>"), stdout, stderr)
		}
		token := os.Getenv(*tokenEnv)
		if token == "" {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("the github forge needs a token in $%s", *tokenEnv)), stdout, stderr)
		}
		forge = protections.NewGitHub(*api, owner, name, token)
	case "forgejo":
		owner, name, ok := strings.Cut(*github, "/")
		if !ok || owner == "" || name == "" {
			return render(envelope.Fail(envelope.ExitUsage, "usage", "the forgejo forge needs --github <owner/name>"), stdout, stderr)
		}
		base := *api
		if base == "" && cfg.Admission != nil {
			base = cfg.Admission.Api
		}
		if base == "" {
			return render(envelope.Fail(envelope.ExitUsage, "usage", "the forgejo forge needs its instance URL in --api or admission.api"), stdout, stderr)
		}
		env := *tokenEnv
		if env == "GITHUB_TOKEN" {
			env = "FORGEJO_TOKEN" // the forge default when --token-env is left at the github default
		}
		token := os.Getenv(env)
		if token == "" {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("the forgejo forge needs a token in $%s", env)), stdout, stderr)
		}
		fj := protections.NewForgejo(base, owner, name, token)
		fj.LedgerBranch = strings.TrimPrefix(cfg.LedgerRef(), "refs/heads/")
		forge = fj
	default:
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("unknown forge %q (snapshot | github | forgejo)", *forgeKind)), stdout, stderr)
	}
	var rep *protections.Report
	var err error
	if verb == "plan" {
		rep, _, err = protections.Plan(cfg, forge, *repo)
	} else {
		rep, err = protections.Apply(cfg, forge, *repo)
	}
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	result := reportResult(rep)
	if rep.DriftCount == 0 {
		return render(envelope.OK(result), stdout, stderr)
	}
	// Drift is reported by name so a CI job can gate on the exit and an
	// operator can read what to do; the report rides stderr for humans.
	var names []string
	for _, c := range rep.Changes {
		if c.Kind != protections.ChangeManual {
			names = append(names, c.Kind+" "+c.Ruleset)
		}
	}
	if rep.Codeowners == "drift" {
		names = append(names, "CODEOWNERS drift")
	}
	for _, f := range rep.Findings {
		names = append(names, "ci "+f.File)
	}
	for _, c := range rep.Changes {
		fmt.Fprintf(stderr, "protections: %s %s: %s\n", c.Kind, c.Ruleset, c.Detail)
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(stderr, "protections: ci %s: %s\n", f.File, f.Detail)
	}
	return render(envelope.Fail(envelope.ExitDrift, "protections_drift", fmt.Sprintf("%d drift(s) against the declaration: %s", rep.DriftCount, strings.Join(names, "; "))), stdout, stderr)
}

func reportResult(rep *protections.Report) map[string]any {
	changes := make([]map[string]any, 0, len(rep.Changes))
	for _, c := range rep.Changes {
		changes = append(changes, map[string]any{"kind": c.Kind, "ruleset": c.Ruleset, "detail": c.Detail})
	}
	findings := make([]map[string]any, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		findings = append(findings, map[string]any{"file": f.File, "detail": f.Detail})
	}
	return map[string]any{
		"default_branch": rep.DefaultBranch,
		"drift":          rep.DriftCount,
		"manual":         rep.Manual,
		"changes":        changes,
		"codeowners":     rep.Codeowners,
		"ci_findings":    findings,
		"applied":        rep.Applied,
	}
}
