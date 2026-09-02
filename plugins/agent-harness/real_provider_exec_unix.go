//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

func realProviderSupported() bool { return true }

// startProcess puts the child in its own process group so killProcessGroup can
// take down the whole tree, not just the direct child.
func startProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Negative pid targets the process group whose id == the child's pid
	// (guaranteed by Setpgid above). Fall back to the direct child.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
