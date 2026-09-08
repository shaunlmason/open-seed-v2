// covergate runs the coverage half of `make check-next` and applies
// the gate (plans/os-cafba959.md). It is a separate binary rather than
// six lines of make, because a retry rule living in a Makefile recipe
// is a correctness claim nothing can check — the decision it wires up
// is drilled in internal/covergate.
//
// The gate's collection is lossy at a low rate: cmd/go's
// mergeCoverProfile drops a package's profile fragment silently when
// the fragment file is missing or zero-length, so the merged total can
// read far below truth on a tree that is fine. This tool re-collects
// COLD, once, only when the reading is below the threshold.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/covergate"
)

// totalRE reads the merged total from `go tool cover -func`'s last
// line. The raw string is kept verbatim so the gate's own output
// cannot drift from go's formatting.
var totalRE = regexp.MustCompile(`(?m)^total:\s+\(statements\)\s+(([0-9.]+)%)\s*$`)

func main() {
	gate := flag.Float64("gate", 90.0, "coverage percentage the tree must meet")
	dir := flag.String("dir", ".", "module directory the suite runs in")
	flag.Parse()

	// The first reading runs package test binaries in parallel; the cold
	// re-collection, which only a low reading triggers, serializes them
	// (-p 1), so the reading that decides a failure is collected exactly
	// as the gate's behavior was established. See collect.
	attempt := 0
	verdict, msg, err := covergate.Run(*gate, covergate.Deps{
		Collect: func() (covergate.Reading, error) {
			attempt++
			return collect(*dir, attempt > 1)
		},
		CleanCache: func() error { return run(*dir, "go", "clean", "-testcache") },
	})
	if msg != "" {
		fmt.Println(msg)
	}
	if verdict == covergate.Pass {
		return
	}
	if err != nil {
		fmt.Printf("check-next: %v\n", err)
	}
	os.Exit(1)
}

// collect runs the suite and reads the merged total.
//
// serial passes -p 1, which serializes package test binaries. The
// Makefile's original comment blamed counter-file collisions between
// concurrent binaries "at the same pid and second"; that theory is
// refuted (the names carry nanoseconds, and a re-exec'd helper child
// gets its OWN temp directory from testing's coverTearDown, so it
// cannot collide with its parent at all). Serialized runs are what
// the measured behavior was established against, so the cold
// re-collection keeps them; the first reading runs packages in
// parallel (go's default, one per CPU), which halves the suite's wall
// clock. A first reading that lost a fragment reads low and is
// re-collected serially, so the parallel pass cannot produce a
// verdict the serialized one would not: it only decides pass, never
// fail.
func collect(dir string, serial bool) (covergate.Reading, error) {
	// A FRESH profile per collection, never a fixed path. Two
	// collections sharing one file - or one run inheriting a truncated
	// file a killed predecessor left behind - can be read as this
	// run's result: go tool cover reports a malformed import path and
	// the gate fails for a reason that has nothing to do with the
	// tree. Found by hitting it (two concurrent `make check` runs
	// corrupted a shared coverage.out), and the fix removes the whole
	// class rather than the instance.
	tmp, err := os.MkdirTemp("", "covergate-")
	if err != nil {
		return covergate.Reading{}, err
	}
	defer os.RemoveAll(tmp)
	profile := filepath.Join(tmp, "coverage.out")

	args := []string{"test"}
	if serial {
		args = append(args, "-p", "1")
	}
	args = append(args, "./...",
		"-coverprofile="+profile, "-covermode=atomic", "-coverpkg=./internal/...")
	out, err := output(dir, "go", args...)
	if err != nil {
		return covergate.Reading{Output: out}, fmt.Errorf("%w: %v", covergate.ErrSuiteFailed, err)
	}
	funcOut, err := output(dir, "go", "tool", "cover", "-func="+profile)
	if err != nil {
		return covergate.Reading{Output: funcOut}, fmt.Errorf("cannot read %s: %v", profile, err)
	}
	m := totalRE.FindStringSubmatch(funcOut)
	if m == nil {
		return covergate.Reading{Output: funcOut}, fmt.Errorf("no total line in the coverage report")
	}
	total, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return covergate.Reading{Output: funcOut}, fmt.Errorf("total %q is not a number: %v", m[2], err)
	}
	return covergate.Reading{Total: total, Raw: m[1]}, nil
}

func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return strings.TrimRight(string(b), "\n"), err
}

func run(dir, name string, args ...string) error {
	_, err := output(dir, name, args...)
	return err
}
