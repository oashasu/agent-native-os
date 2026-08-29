package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunCommandCapturesExitAndOutput(t *testing.T) {
	ws := t.TempDir()
	r, err := runCommand(context.Background(), ws, []string{"sh", "-c", "echo out; echo err 1>&2; exit 3"}, nil, 5000)
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if r.ExitCode != 3 || !strings.Contains(string(r.Stdout), "out") || !strings.Contains(string(r.Stderr), "err") {
		t.Fatalf("result: %+v", r)
	}
}

func TestRunCommandOnlyPassesAllowlistedEnv(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "sensitive")
	t.Setenv("BUILD_FLAG", "on")
	r, err := runCommand(context.Background(), t.TempDir(), []string{"sh", "-c", "echo SECRET=$SECRET_TOKEN FLAG=$BUILD_FLAG"}, []string{"BUILD_FLAG"}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	s := string(r.Stdout)
	if !strings.Contains(s, "FLAG=on") || strings.Contains(s, "sensitive") {
		t.Fatalf("env leak or missing allowlisted var: %q", s)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	r, err := runCommand(context.Background(), t.TempDir(), []string{"sh", "-c", "sleep 5"}, nil, 200)
	if err != nil {
		t.Fatalf("timeout should not be a start error: %v", err)
	}
	if !r.TimedOut {
		t.Fatalf("expected TimedOut, got %+v", r)
	}
}

func TestFingerprintStableAndSensitive(t *testing.T) {
	ws := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", ws, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", a, out)
		}
	}
	f1 := fingerprint([]string{"mvn", "test"}, nil, ws)
	f2 := fingerprint([]string{"mvn", "test"}, nil, ws)
	f3 := fingerprint([]string{"mvn", "-q", "test"}, nil, ws)
	if f1 == "" || f1 != f2 || f1 == f3 {
		t.Fatalf("fingerprint: f1=%s f2=%s f3=%s", f1, f2, f3)
	}
	_ = os.Stdout
}
