package main

// The handbook's commands run (plans/os-16e55c11.md D2, AC2): every
// fenced `seed` command names a dispatchable group and subverb, so a
// renamed verb fails the handbook rather than the reader; and the
// flagship read commands run live against the fixture, so a renamed flag
// on them fails too.

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
)

// knownGroups is the top-level verb set the CLI dispatches, read from
// the verb table rather than restated here. A hand-maintained copy of a
// list that lives somewhere else falls behind the moment a verb is
// added, and fails the handbook for documenting a command that works
// (os-f11601e0: `boundary` was dispatchable and missing from the copy).
func knownGroups() map[string]bool {
	known := map[string]bool{}
	for _, g := range catalog(strings.NewReader("")).Groups() {
		known[g.Name] = true
	}
	return known
}

// handbookCommands extracts the token lists of every `seed` command in a
// fenced code block.
func handbookCommands(t *testing.T) [][]string {
	t.Helper()
	f, err := os.Open("../../docs/handbook.md")
	if err != nil {
		t.Fatalf("read handbook: %v", err)
	}
	defer f.Close()
	var cmds [][]string
	inFence := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence && strings.HasPrefix(strings.TrimSpace(line), "seed ") {
			cmds = append(cmds, strings.Fields(strings.TrimSpace(line))[1:])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cmds) == 0 {
		t.Fatal("the handbook must carry fenced seed commands")
	}
	return cmds
}

func TestHandbookCommandsAreDispatchable(t *testing.T) {
	known := knownGroups()
	for _, toks := range handbookCommands(t) {
		if len(toks) == 0 {
			continue
		}
		group := toks[0]
		if !known[group] {
			t.Errorf("handbook uses unknown top-level verb %q", group)
			continue
		}
		// Probe the group and (if present) its subverb: a dispatchable
		// verb never answers "unknown ...". Other failures (a missing
		// ledger) are fine — we are pinning the command surface, not
		// running the deployment.
		probe := []string{group}
		if len(toks) > 1 && !strings.HasPrefix(toks[1], "-") {
			probe = append(probe, toks[1])
		}
		e, _ := runEnv(t, probe...)
		if e.Error != nil && strings.Contains(e.Error.Message, "unknown") {
			t.Errorf("handbook command %v is not dispatchable: %s", probe, e.Error.Message)
		}
	}
}

func TestHandbookFlagshipCommandsRunLive(t *testing.T) {
	// Each flagship command runs against the fixture; a renamed flag on
	// any of them fails here.
	cases := [][]string{
		{"version"},
		{"docs", "check", "--root", "../.."},
		{"lane", "list", "--lanes", "../../lanes"},
		{"doctor", "--config", "../../fixtures/deployment/seed.json"},
		{"simulate", "--lanes", "../../lanes", "--intents", "1", "--posture", "cooperative", "--work", t.TempDir()},
	}
	for _, args := range cases {
		e, code := runEnv(t, args...)
		if code != envelope.ExitOK || !e.OK {
			t.Errorf("flagship command %v must run, got exit %d: %+v", args, code, e.Error)
		}
	}
}
