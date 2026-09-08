package simulate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// intent is one shipped fix-the-check fixture: a file that is "red" at
// base and "green" once the known patch is applied at head. The
// solution is known, so the simulation needs no model to solve it.
type intent struct {
	name  string
	file  string
	red   string // the base (failing) content
	green string // the head (fixed) content — the known patch
}

// shipped is the small catalog of synthetic intents. A real deployment
// adds its own; these are enough to drive the loop end to end.
var shipped = []intent{
	{"greet", "hello.txt", "hello\n", "hello, world\n"},
	{"flag", "feature.txt", "off\n", "on\n"},
	{"const", "value.txt", "0\n", "42\n"},
	{"typo", "readme.txt", "teh\n", "the\n"},
}

// catalog returns the shipped intents rotated by seed, so a run's draw
// is deterministic in the seed.
func catalog(seed int64) []intent {
	n := len(shipped)
	off := int(((seed % int64(n)) + int64(n)) % int64(n))
	out := make([]intent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, shipped[(i+off)%n])
	}
	return out
}

// repo is a built fixture repository: the source path and the three
// commits the acceptance and the receipt cite.
type repo struct {
	src, base, spec, head string
}

// buildRepo materializes one intent as a git repository with a base
// (red) commit, a spec commit carrying the acceptance, and a head
// commit applying the known patch (green).
func (d *deployment) buildRepo(subject string, in intent) (repo, error) {
	dir, err := os.MkdirTemp(d.dir, "repo-"+subject+"-")
	if err != nil {
		return repo{}, err
	}
	git := func(args ...string) (string, error) {
		full := append([]string{"-C", dir, "-c", "user.name=sim", "-c", "user.email=sim@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %v: %v: %s", args, err, out)
		}
		return string(out), nil
	}
	write := func(name, content string) error {
		return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	rev := func() (string, error) {
		out, err := git("rev-parse", "HEAD")
		return trim(out), err
	}
	if _, err := git("init", "--quiet", "-b", "main"); err != nil {
		return repo{}, err
	}
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}} {
		if _, err := git("config", kv[0], kv[1]); err != nil {
			return repo{}, err
		}
	}
	if err := write(in.file, in.red); err != nil {
		return repo{}, err
	}
	if _, err := git("add", "."); err != nil {
		return repo{}, err
	}
	if _, err := git("commit", "--quiet", "-m", "base"); err != nil {
		return repo{}, err
	}
	base, err := rev()
	if err != nil {
		return repo{}, err
	}
	// The acceptance: a gated spec (executable:false), so the verifier
	// records the gated verdict through the real receipt machinery
	// rather than running a command — deterministic and credential-free.
	accept := "# " + in.name + "\n\n## Validation Commands\n\n- Boundary: `printf ok`\n- `test -f " + in.file + "`\n"
	if err := write("accept.md", accept); err != nil {
		return repo{}, err
	}
	if _, err := git("add", "."); err != nil {
		return repo{}, err
	}
	if _, err := git("commit", "--quiet", "-m", "specs"); err != nil {
		return repo{}, err
	}
	spec, err := rev()
	if err != nil {
		return repo{}, err
	}
	if err := write(in.file, in.green); err != nil {
		return repo{}, err
	}
	if _, err := git("add", "."); err != nil {
		return repo{}, err
	}
	if _, err := git("commit", "--quiet", "-m", "head: "+in.name); err != nil {
		return repo{}, err
	}
	head, err := rev()
	if err != nil {
		return repo{}, err
	}
	return repo{src: dir, base: base, spec: spec, head: head}, nil
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// records materializes the remote ledger and returns every record, so
// the report and the audit reconstruct from the chain alone.
func (d *deployment) records() ([]*event.Record, error) {
	dir, err := d.materialize()
	if err != nil {
		return nil, err
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		return nil, err
	}
	var recs []*event.Record
	if err := store.Records(func(_ int, rec *event.Record) error {
		recs = append(recs, rec)
		return nil
	}); err != nil {
		return nil, err
	}
	return recs, nil
}
