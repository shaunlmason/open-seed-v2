package simulate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// installHook builds the seed-admit pre-receive hook and installs it on
// the bare remote (plans/os-16e55c11.md D3, enforced-self-hosted): the
// server rejects a push the boundary would not admit. nextDir is the
// module root (<repo>/next) the hook binary is built from.
func installHook(remote, nextDir string) error {
	bin := filepath.Join(remote, "seed-admit")
	build := exec.Command("go", "build", "-o", bin, "./cmd/seed-admit")
	build.Dir = nextDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build seed-admit: %v: %s", err, out)
	}
	hookDir := filepath.Join(remote, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\nexec \"" + bin + "\" pre-receive\n"
	hook := filepath.Join(hookDir, "pre-receive")
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		return err
	}
	return nil
}
