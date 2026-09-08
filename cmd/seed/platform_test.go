package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// nextRoot is the module root, found from the test's own directory.
func nextRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func goSources(t *testing.T, tests bool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(nextRoot(t), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "bin" || d.Name() == ".git" || d.Name() == "trajectories" || d.Name() == "fixtures") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") && strings.HasSuffix(p, "_test.go") == tests {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no Go sources found")
	}
	return out
}

var (
	osFileCall  = regexp.MustCompile(`\bos\.(ReadFile|WriteFile|Open|OpenFile|Create|Stat|Lstat|MkdirAll|Mkdir|Remove|RemoveAll|ReadDir|Rename|Chmod|CreateTemp|MkdirTemp)\(`)
	slashJoined = regexp.MustCompile(`"/"\s*\+|\+\s*"/[^"]|(^|[^a-z])path\.Join\(|"[^"]*[^:]/[^"]*"\s*\+`)
)

// conformance: plans/os-b55e5647.md AC5 (the path lint) — every
// filesystem path is built with path/filepath: no os file call takes
// an argument joined with a literal slash or path.Join on the same
// line. Slash-joined strings feeding refs, URLs and repository-relative
// names are git's and the spec's, and are not filesystem paths; the
// lint reads the call site, so it flags what the platform would break.
func TestFilesystemPathsUseFilepath(t *testing.T) {
	var offenders []string
	for _, f := range goSources(t, false) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if osFileCall.MatchString(line) && slashJoined.MatchString(line) {
				offenders = append(offenders, filepath.Base(filepath.Dir(f))+"/"+filepath.Base(f)+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("filesystem paths built with a literal slash (use filepath.Join):\n%s", strings.Join(offenders, "\n"))
	}
}

var skipCall = regexp.MustCompile(`t\.Skipf?\(\s*("[^"]+"|[a-zA-Z_][a-zA-Z0-9_.]*)`)

// conformance: plans/os-b55e5647.md AC5, AC6 — every platform-gated
// skip names its reason: a bare t.Skip() or one whose first argument
// is an empty string would let a platform pass vacuously.
func TestEverySkipNamesItsReason(t *testing.T) {
	call := regexp.MustCompile(`\bt\.Skipf?\(`)
	bare := regexp.MustCompile(`t\.Skipf?\(\s*\)|t\.Skipf?\(\s*""`)
	found := 0
	for _, f := range goSources(t, true) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if trimmed := strings.TrimSpace(line); call.MatchString(line) && !strings.HasPrefix(trimmed, "//") {
				found++
				if bare.MatchString(line) || !skipCall.MatchString(line) {
					t.Errorf("%s:%d: a skip without a reason: %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no skips found: the lint would pass vacuously")
	}
}

// conformance: plans/os-b55e5647.md AC5 (line endings) — a ledger
// segment mangled to CRLF is refused at verification, naming the
// carriage return, never normalized: the bytes a signature covers are
// the bytes on disk.
func TestCRLFSegmentIsRefusedNotNormalized(t *testing.T) {
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if e, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatalf("init: %d %+v", code, e)
	}
	if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "seed/1"}`); code != 0 {
		t.Fatalf("append: %d %+v", code, e)
	}
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 {
		t.Fatalf("verify before: %d %+v", code, e)
	}
	segments, err := filepath.Glob(filepath.Join(ld, "segments", "*.jsonl"))
	if err != nil || len(segments) == 0 {
		t.Fatal("no segment")
	}
	b, err := os.ReadFile(segments[0])
	if err != nil {
		t.Fatal(err)
	}
	crlf := strings.ReplaceAll(string(b), "\n", "\r\n")
	if err := os.WriteFile(segments[0], []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	e, code := runEnv(t, "ledger", "verify", "--ledger", ld)
	if code == 0 || e.Error == nil || e.Error.Code != "chain_invalid" || !strings.Contains(e.Error.Message, "carriage return") {
		t.Fatalf("a CRLF segment is refused naming the carriage return: %d %+v %+v", code, e, e.Error)
	}
}

// conformance: plans/os-b55e5647.md AC4 — the doctor reports the
// platform and the postures available on it, with the reasons.
func TestDoctorReportsThePlatform(t *testing.T) {
	cfg := writeDeclaration(t, `{"posture": "cooperative"}`)
	e, _ := runEnv(t, "doctor", "--config", cfg)
	plat, _ := e.Result["platform"].(map[string]any)
	if plat == nil || plat["os"] != runtime.GOOS {
		t.Fatalf("the doctor names the platform: %+v", e.Result["platform"])
	}
	avail, _ := plat["available"].([]any)
	postures, _ := plat["postures"].([]any)
	if len(avail) < 2 || len(postures) != 3 {
		t.Fatalf("the postures and their availability: %+v", plat)
	}
	for _, p := range postures {
		row, _ := p.(map[string]any)
		if row["reason"] == nil || row["reason"] == "" {
			t.Fatalf("every posture carries a reason: %+v", row)
		}
	}
}
