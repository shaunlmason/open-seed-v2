// The observation writer verb (plans/os-2ff8dbf1.md): one line onto
// the per-run stream, creating the file as needed. The channel is
// ephemeral, unsigned, and lossy by declaration; no reader daemon
// exists in v0 (the report build and tests are the readers), so this
// verb is the whole writer surface.
package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

func runObs(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "emit" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "obs requires the subverb: emit"), stdout, stderr)
	}
	fs := flag.NewFlagSet("obs emit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "observation channel directory")
	actor := fs.String("actor", "", "executor fingerprint")
	fence := fs.String("fence", "", "claim fence position keying the run's stream")
	subject := fs.String("subject", "", "contract subject")
	count := fs.Int("count", -1, "monotonic completed-item count")
	step := fs.String("step", "", "declared in-step state")
	ts := fs.String("ts", "", "line timestamp (RFC3339; defaults to now)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 ||
		*dir == "" || *actor == "" || *fence == "" || *subject == "" || *count < 0 || *step == "" {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "obs emit --dir <obs-dir> --actor <fp> --fence <position> --subject <id> --count <n> --step <s> [--ts <rfc3339>]"), stdout, stderr)
	}
	when := *ts
	if when == "" {
		when = time.Now().UTC().Format(time.RFC3339)
	} else if _, err := time.Parse(time.RFC3339, when); err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--ts %q is not RFC3339: %v", when, err)), stdout, stderr)
	}
	if err := obs.Append(*dir, *actor, *fence, obs.Line{TS: when, Subject: *subject, Count: *count, Step: *step}); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	return render(envelope.OK(map[string]any{"dir": *dir, "actor": *actor, "fence": *fence, "subject": *subject, "count": fmt.Sprintf("%d", *count), "step": *step, "ts": when}), stdout, stderr)
}
