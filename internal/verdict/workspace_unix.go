//go:build !windows

package verdict

import (
	"os/exec"
	"syscall"
)

// runnerPath is the fixed, minimal PATH a check runs under: the
// system's own tools and nothing from the caller's environment.
func runnerPath() string { return "/usr/local/bin:/usr/bin:/bin" }

// setProcessGroup puts the check in its own process group so a
// timeout kills the whole pipeline, children included.
func setProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killProcessTree kills the check's process group.
func killProcessTree(cmd *exec.Cmd) error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
