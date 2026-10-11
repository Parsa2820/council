//go:build !windows

package harness

import (
	"os/exec"
	"syscall"
)

// configureProcess puts the child in its own process group so that a timeout
// kills the whole tree (CLIs such as Claude Code and Gemini CLI spawn helpers).
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
}
