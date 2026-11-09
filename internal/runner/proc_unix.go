//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// setProcGroup выносит процесс в отдельную группу, чтобы убить её целиком.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcGroup бьёт по всей группе. Знак минус перед pid и означает группу.
func killProcGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
