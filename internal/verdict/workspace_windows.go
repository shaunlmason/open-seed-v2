//go:build windows

package verdict

import (
	"os"
	"os/exec"
)

// runnerPath on Windows is the process's own PATH: a POSIX sh (Git
// Bash) is found there or nowhere, and the platform has no fixed
// system tool directories (spec/platform.md).
func runnerPath() string { return os.Getenv("PATH") }

// setProcessGroup is a no-op: Windows has no POSIX process groups.
func setProcessGroup(*exec.Cmd) {}

// killProcessTree kills the shell; children it spawned may outlive it,
// which platform.md states as the platform's honest limit.
func killProcessTree(cmd *exec.Cmd) error { return cmd.Process.Kill() }
