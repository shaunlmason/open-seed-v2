// Package platform states, per operating system, what Seed can and
// cannot do there (plans/os-b55e5647.md D4, D6; spec/platform.md;
// charter III.I row 4): the honest capability list the doctor reports
// and the CI matrix tests. Nothing here is a claim a test does not
// back: a posture is available on a platform because the drills run
// there, and unavailable with the reason named.
package platform

import (
	"runtime"
	"sort"
)

// Posture is one admission posture's availability on a platform.
type Posture struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	// Reason says why a posture is unavailable, or what it needs
	// when it is; never empty.
	Reason string `json:"reason"`
}

// Name is the running platform, Go's GOOS.
func Name() string { return runtime.GOOS }

// Postures lists the three admission postures and whether the named
// platform can run each. The cooperative and forge-hosted postures
// need only git and a network, and run everywhere the suites pass;
// the enforced self-hosted posture needs a git server that executes
// the pre-receive hook, which a POSIX host provides and a bare
// Windows checkout does not — there the deployment runs cooperative
// or forge-hosted, and the doctor says so.
func Postures(goos string) []Posture {
	hook := Posture{Name: "enforced-self-hosted", Available: true, Reason: "a git server on this platform executes the pre-receive hook (cmd/seed-admit); the hook drills run here"}
	switch goos {
	case "windows":
		hook = Posture{Name: "enforced-self-hosted", Available: false, Reason: "no server executes the pre-receive hook on a bare Windows checkout; run the cooperative or the forge-hosted posture, or host the ledger on a POSIX git server"}
	case "linux", "darwin":
	default:
		hook = Posture{Name: "enforced-self-hosted", Available: false, Reason: "the hook drills have not run on " + goos + "; a posture is available where its drills pass, never by assertion"}
	}
	out := []Posture{
		{Name: "cooperative", Available: true, Reason: "needs git and a remote the actors can push; the client's own admission check runs everywhere the suites pass"},
		{Name: "enforced-forge-hosted", Available: true, Reason: "needs a forge with branch protections and the admission service reachable over HTTP; nothing platform-bound"},
		hook,
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Available lists the postures the named platform can run, sorted.
func Available(goos string) []string {
	var out []string
	for _, p := range Postures(goos) {
		if p.Available {
			out = append(out, p.Name)
		}
	}
	return out
}

// Report is the doctor's platform section: the platform, its
// postures with their reasons, the line-ending and path rules the
// spec binds every platform to.
func Report() map[string]any {
	return map[string]any{
		"os":            Name(),
		"arch":          runtime.GOARCH,
		"postures":      Postures(Name()),
		"available":     Available(Name()),
		"line_endings":  "LF only: a carriage return in a ledger line is refused, never normalized",
		"path_handling": "every filesystem path is built with path/filepath; slashes are for refs and URLs",
	}
}
