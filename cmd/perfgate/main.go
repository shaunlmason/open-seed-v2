// perfgate runs the performance half of `make check-next`
// (plans/os-7508ab9e.md D6, D7): the five metrics against the
// representative history at the budget file's size, each held to its
// ceiling, a miss re-measured cold once before it fails. The last
// reading is written to perf/last.json (gitignored) so an
// operator can read it and CI can attach it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shaunlmason/open-seed-v2/internal/perfgate"
)

func main() {
	budgets := flag.String("budgets", "perf/budgets.json", "the checked-in budget file")
	dir := flag.String("dir", ".", "module directory")
	last := flag.String("last", "perf/last.json", "where the last reading is written (gitignored)")
	keep := flag.String("keep", "", "keep the storm's work dir (the remote, every writer's state dir with any refused tree) under this directory")
	flag.Parse()
	b, err := perfgate.Load(filepath.Join(*dir, *budgets))
	if err != nil {
		fmt.Printf("check-next: perf budgets: %v\n", err)
		os.Exit(1)
	}
	hook, cleanup, err := buildHook(*dir)
	if err != nil {
		fmt.Printf("check-next: perf: building seed-admit for the storm: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()
	m := perfgate.Measurer{Seed: 1, Contracts: b.History, Writers: b.Writers, HookBin: hook, Keep: *keep}
	var reading perfgate.Reading
	verdict, msg, err := perfgate.Run(b, perfgate.Deps{
		Measure: func() (perfgate.Reading, error) {
			r, err := m.Measure()
			if err == nil {
				reading = r
			}
			return r, err
		},
		CleanCache: func() error { return nil },
	})
	if reading != nil {
		if out, jerr := json.MarshalIndent(reading, "", "  "); jerr == nil {
			_ = os.WriteFile(filepath.Join(*dir, *last), append(out, '\n'), 0o644)
		}
	}
	if verdict == perfgate.Pass {
		// The passing line carries no readings: `make check` is held
		// to be deterministic (the flavor drill compares two runs of
		// it), and a timing differs every run. The readings are in the
		// last-reading file beside the budgets.
		fmt.Printf("check-next: perf ok; %d metrics under their ceilings (history %d, writers %d), the readings in %s\n", len(b.Metrics), b.History, b.Writers, *last)
		return
	}
	if msg != "" {
		fmt.Println(msg)
	}
	if err != nil {
		fmt.Printf("check-next: %v\n", err)
	}
	os.Exit(1)
}

// buildHook builds the admission hook the storm's remote enforces
// with, so the contention number is the enforced posture's.
func buildHook(dir string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "perfgate-hook-*")
	if err != nil {
		return "", func() {}, err
	}
	bin := filepath.Join(tmp, "seed-admit")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/seed-admit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		return "", func() {}, fmt.Errorf("%v: %s", err, out)
	}
	return bin, func() { os.RemoveAll(tmp) }, nil
}
