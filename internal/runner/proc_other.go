//go:build !unix

package runner

import "os/exec"

// На Windows группы процессов нет; достаточно штатного убийства по контексту.

func setProcGroup(cmd *exec.Cmd) {}

func killProcGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
