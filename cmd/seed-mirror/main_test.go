package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/mirror"
	"github.com/shaunlmason/open-seed-v2/mirror/mirrortest"
)

// publish writes a build holding the rows and the envelope `seed
// project current --name contracts` would print for it, returning the
// envelope's path.
func publish(t *testing.T, rows string) string {
	t.Helper()
	dir := t.TempDir()
	build := filepath.Join(dir, "b1")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(dir, "current.json")
	// The build path is JSON-encoded: on Windows it carries backslashes.
	env, err := json.Marshal(map[string]any{"ok": true, "result": map[string]string{"name": "contracts", "position": "4", "tip": strings.Repeat("cd", 32), "version": "1", "path": build}})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		current:                                string(env),
		filepath.Join(build, "contracts.json"): rows,
	} {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return current
}

// conformance: plans/os-b45c308d.md D1 — seed-mirror plan|apply over
// every registered exporter: the plan is printed, apply performs it,
// a second apply is empty, and the command has no Seed-side flag.
func TestMirrorCommandPlansAndApplies(t *testing.T) {
	out := publish(t, `[{"subject":"c-1","state":"ready"},{"subject":"c-2","state":"done"},{"subject":"c-0","state":null}]`)
	for _, name := range mirror.Names() {
		t.Run(name, func(t *testing.T) {
			var extra []string
			switch name {
			case "github":
				srv, _ := mirrortest.GitHub()
				t.Cleanup(srv.Close)
				t.Setenv(mirror.GitHubTokenEnv, mirrortest.Token)
				extra = []string{"--owner", "o", "--repo", "r", "--base-url", srv.URL}
			case "forgejo":
				srv, _ := mirrortest.Forgejo()
				t.Cleanup(srv.Close)
				t.Setenv(mirror.ForgejoTokenEnv, mirrortest.Token)
				extra = []string{"--owner", "o", "--repo", "r", "--base-url", srv.URL}
			case "snapshot":
				extra = []string{"--snapshot", filepath.Join(t.TempDir(), "issues.json")}
			}
			args := append([]string{"--current", out, "--forge", name}, extra...)
			var stdout, stderr bytes.Buffer
			if code := run(append([]string{"plan"}, args...), &stdout, &stderr); code != 0 {
				t.Fatalf("plan: %d %s", code, stderr.String())
			}
			var doc struct {
				Exporter string      `json:"exporter"`
				Plan     mirror.Plan `json:"plan"`
				Applied  *mirror.Applied
			}
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Exporter != name || len(doc.Plan.Actions) != 2 || doc.Plan.Stamp.Position != 4 || doc.Applied != nil {
				t.Fatalf("plan lists two creations and applies nothing: %s", stdout.String())
			}
			stdout.Reset()
			if code := run(append([]string{"apply"}, args...), &stdout, &stderr); code != 0 {
				t.Fatalf("apply: %d %s", code, stderr.String())
			}
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Applied == nil || len(doc.Applied.Created) != 2 {
				t.Fatalf("apply created two: %s", stdout.String())
			}
			stdout.Reset()
			if code := run(append([]string{"apply"}, args...), &stdout, &stderr); code != 0 {
				t.Fatalf("second apply: %d %s", code, stderr.String())
			}
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Plan.Actions) != 0 || doc.Plan.Matching != 2 || len(doc.Applied.Created) != 0 {
				t.Fatalf("a second apply is a no-op: %s", stdout.String())
			}
			if strings.Contains(stdout.String(), mirrortest.Token) {
				t.Fatal("the credential is never in the output")
			}
		})
	}
}

func TestMirrorCommandRefusals(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != exitUsage {
		t.Fatalf("no subverb: %d", code)
	}
	if code := run([]string{"sync"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("unknown subverb: %d", code)
	}
	if code := run([]string{"plan", "--current", "x.json"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("no forge: %d", code)
	}
	if code := run([]string{"plan", "--forge", "snapshot", "--snapshot", "s.json"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("no resolved projection named: %d", code)
	}
	if code := run([]string{"plan", "--current", filepath.Join(t.TempDir(), "missing.json"), "--forge", "snapshot", "--snapshot", "s.json"}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("no published projection: %d", code)
	}
	out := publish(t, `[]`)
	if code := run([]string{"plan", "--current", out, "--forge", "gitlab"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "no exporter named") {
		t.Fatalf("an unregistered exporter: %d %s", code, stderr.String())
	}
	stderr.Reset()
	// Every Seed-side flag is unknown here: the component has none.
	for _, f := range []string{"--ledger", "--key", "--remote", "--config", "--as"} {
		if code := run([]string{"plan", f, "x", "--forge", "snapshot", "--snapshot", "s.json", "--current", out}, &stdout, &stderr); code != exitUsage {
			t.Fatalf("%s is not a seed-mirror flag: %d", f, code)
		}
	}
}
