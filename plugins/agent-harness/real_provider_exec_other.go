//go:build !unix

package main

import (
	"errors"
	"os/exec"
)

func realProviderSupported() bool { return false }

func startProcess(cmd *exec.Cmd) error {
	return errors.New("real provider unsupported on this platform")
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
