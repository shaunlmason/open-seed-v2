package platform

import (
	"runtime"
	"strings"
	"testing"
)

// conformance: plans/os-b55e5647.md AC4 — the capability list is
// honest per platform: every posture carries a reason, the enforced
// self-hosted posture is unavailable on Windows with the reason named,
// available on the POSIX hosts the hook drills run on, and unavailable
// on a platform the drills have not run on rather than assumed.
func TestPosturesAreHonestPerPlatform(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "plan9"} {
		ps := Postures(goos)
		if len(ps) != 3 {
			t.Fatalf("%s: three postures, got %d", goos, len(ps))
		}
		for _, p := range ps {
			if strings.TrimSpace(p.Reason) == "" {
				t.Fatalf("%s: %s carries no reason", goos, p.Name)
			}
		}
		avail := Available(goos)
		hook := false
		for _, a := range avail {
			hook = hook || a == "enforced-self-hosted"
		}
		switch goos {
		case "linux", "darwin":
			if !hook || len(avail) != 3 {
				t.Fatalf("%s runs every posture: %v", goos, avail)
			}
		default:
			if hook || len(avail) != 2 {
				t.Fatalf("%s runs the two postures that need no hook server: %v", goos, avail)
			}
		}
	}
	rep := Report()
	if rep["os"] != runtime.GOOS || rep["available"] == nil || rep["line_endings"] == nil || rep["path_handling"] == nil {
		t.Fatalf("the report names the platform, its postures and the rules: %+v", rep)
	}
}
