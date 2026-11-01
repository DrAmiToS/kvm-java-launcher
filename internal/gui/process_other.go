//go:build !windows

package gui

import (
	"os/exec"
	"syscall"
)

func hideChildWindow() *syscall.SysProcAttr {
	return nil
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
