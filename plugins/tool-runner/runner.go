package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type CmdResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	TimedOut bool
}

func runCommand(ctx context.Context, workspacePath string, argv, envAllowlist []string, timeoutMS int) (CmdResult, error) {
	if timeoutMS <= 0 {
		timeoutMS = 600000
	}
	if len(argv) == 0 {
		return CmdResult{}, &exec.Error{Name: "", Err: errors.New("empty argv")}
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = workspacePath
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for _, name := range envAllowlist {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	r := CmdResult{ExitCode: -1, Stdout: append([]byte(nil), stdout.Bytes()...), Stderr: append([]byte(nil), stderr.Bytes()...), TimedOut: cctx.Err() == context.DeadlineExceeded}
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		var ee *exec.Error
		var pe *os.PathError
		if errors.As(err, &ee) || errors.As(err, &pe) || cmd.ProcessState == nil {
			return r, err
		}
	}
	return r, nil
}

func fingerprint(argv, envAllowlist []string, workspacePath string) string {
	argvJSON, _ := json.Marshal(argv)
	envValues := make([]string, 0, len(envAllowlist))
	for _, name := range envAllowlist {
		envValues = append(envValues, name+"="+os.Getenv(name))
	}
	sort.Strings(envValues)
	envJSON, _ := json.Marshal(envValues)
	commit := commitOf(workspacePath)
	h := sha256.New()
	_, _ = h.Write(argvJSON)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(envJSON)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(commit))
	return hex.EncodeToString(h.Sum(nil))
}

func commitOf(workspacePath string) string {
	out, err := exec.Command("git", "-C", workspacePath, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
