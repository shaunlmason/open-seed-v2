package main

// `seed perf run` (plans/os-7508ab9e.md D7): the five ledger
// performance metrics measured against the representative history and
// printed as an envelope, beside their ceilings when a budget file is
// given. The gate itself is cmd/perfgate under `make check-next`; this
// verb is how an operator reads the numbers.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/perfgate"
)

func runPerf(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "run" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "perf takes the subverb run"), stdout, stderr)
	}
	fs := flag.NewFlagSet("perf run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	budgets := fs.String("budgets", "", "budget file to compare against (default: measure only)")
	history := fs.Int("history", 0, "contracts in the generated history (default: the budget file's, else 20)")
	writers := fs.Int("writers", 0, "concurrent appenders in the storm (default: the budget file's, else 8)")
	hook := fs.String("hook", "", "seed-admit binary the storm's remote enforces with (default: built from ./cmd/seed-admit; 'none' for no hook)")
	keep := fs.String("keep", "", "keep the storm's work dir (the remote, every writer's state dir with any refused tree) under this directory; the rebuild's projections under it are locked read-only, so make them writable before deleting")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "perf run [--budgets <file>] [--history <n>] [--writers <n>] [--hook <path>|none] [--keep <dir>]"), stdout, stderr)
	}
	var b *perfgate.Budgets
	if *budgets != "" {
		loaded, err := perfgate.Load(*budgets)
		if err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
		}
		b = loaded
		if *history == 0 {
			*history = b.History
		}
		if *writers == 0 {
			*writers = b.Writers
		}
	}
	if *history == 0 {
		*history = 20
	}
	if *writers == 0 {
		*writers = 8
	}
	hookBin := *hook
	if hookBin == "" {
		tmp, err := os.MkdirTemp("", "seed-perf-hook-*")
		if err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
		defer os.RemoveAll(tmp)
		hookBin = filepath.Join(tmp, "seed-admit")
		if out, err := exec.Command("go", "build", "-o", hookBin, "./cmd/seed-admit").CombinedOutput(); err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("building seed-admit for the storm: %v: %s", err, out)), stdout, stderr)
		}
	} else if hookBin == "none" {
		hookBin = ""
	}
	reading, err := perfgate.Measurer{Seed: 1, Contracts: *history, Writers: *writers, HookBin: hookBin, Keep: *keep}.Measure()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	result := map[string]any{"history": *history, "writers": *writers, "enforced": hookBin != "", "relinked": reading[perfgate.MetricRelinked]}
	if *keep != "" {
		result["kept"] = *keep
	}
	metrics := map[string]any{}
	over := []string{}
	for _, m := range perfgate.Required() {
		entry := map[string]any{"reading": reading[m]}
		if b != nil {
			entry["ceiling"] = b.Metrics[m].Ceiling
			if reading[m] > b.Metrics[m].Ceiling {
				over = append(over, m)
			}
		}
		metrics[m] = entry
	}
	result["metrics"] = metrics
	if b != nil {
		result["over"] = over
	}
	return render(envelope.OK(result), stdout, stderr)
}
