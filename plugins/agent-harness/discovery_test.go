//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// discovery tests mutate the package argvTemplates map; they must not run in parallel.
func withTemplate(t *testing.T, name string) {
	t.Helper()
	argvTemplates[name] = func(s RunSpec) []string { return []string{"--version-exit", "0"} }
	t.Cleanup(func() { delete(argvTemplates, name) })
}

func TestCandidatesFromEnv(t *testing.T) {
	t.Setenv("VIBE_AGENT_PROVIDERS", " codex, good, codex, , foo ")
	got := candidatesFromEnv()
	if strings.Join(got, ",") != "codex,good,foo" {
		t.Fatalf("candidates=%v", got)
	}
	t.Setenv("VIBE_AGENT_PROVIDERS", " , ")
	got = candidatesFromEnv()
	if len(got) != 1 || got[0] != "codex" {
		t.Fatalf("empty override candidates=%v", got)
	}
}

func TestDiscoveryRegistersGoodCandidate(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "good", `echo "good 1.0"; exit 0`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	withTemplate(t, "good")

	var logw bytes.Buffer
	m := discoverProviders([]string{"good"}, []string{"PATH"}, &logw)
	if _, ok := m["mock"]; !ok {
		t.Fatal("mock missing")
	}
	rp, ok := m["good"].(RealProvider)
	if !ok {
		t.Fatalf("good not registered: %T", m["good"])
	}
	if filepath.Base(rp.bin) != "good" || !filepath.IsAbs(rp.bin) {
		t.Fatalf("bin=%s", rp.bin)
	}
}

func TestDiscoverySkipsNonZeroVersion(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "bad", `echo "bad"; exit 1`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	withTemplate(t, "bad")
	var logw bytes.Buffer
	m := discoverProviders([]string{"bad"}, []string{"PATH"}, &logw)
	if _, ok := m["bad"]; ok {
		t.Fatal("bad should be skipped")
	}
	if len(m) != 1 {
		t.Fatalf("map=%v", m)
	}
	if !bytes.Contains(logw.Bytes(), []byte("bad")) {
		t.Fatalf("no log line: %s", logw.String())
	}
}

func TestDiscoverySkipsSlowVersion(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "probe.pid")
	childFile := filepath.Join(dir, "probe-child.pid")
	writeScript(t, dir, "slow", `echo $$ > "`+pidFile+`"; sleep 10 & child=$!; echo "$child" > "`+childFile+`"; wait; exit 0`)
	t.Cleanup(func() { killProbePID(pidFile); killProbePID(childFile) })
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	withTemplate(t, "slow")
	var logw bytes.Buffer
	done := make(chan map[string]Provider, 1)
	go func() { done <- discoverProviders([]string{"slow"}, []string{"PATH"}, &logw) }()
	select {
	case m := <-done:
		if _, ok := m["slow"]; ok {
			t.Fatal("slow should time out")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("discovery did not bound the slow probe")
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("slow probe did not start: %v", err)
	}
	if _, err := os.Stat(childFile); err != nil {
		t.Fatalf("slow probe descendant did not start: %v", err)
	}
	assertProbePidGone(t, pidFile)
	assertProbePidGone(t, childFile)
}

func TestDiscoverySkipsNoTemplate(t *testing.T) {
	var logw bytes.Buffer
	m := discoverProviders([]string{"whatever"}, []string{"PATH"}, &logw)
	if len(m) != 1 || !bytes.Contains(logw.Bytes(), []byte("no argv template")) {
		t.Fatalf("map=%v log=%s", m, logw.String())
	}
}

func TestDiscoverySkipsMockCandidate(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "mock", `echo "mock 1.0"; exit 0`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	withTemplate(t, "mock")
	var logw bytes.Buffer
	m := discoverProviders([]string{"mock"}, []string{"PATH"}, &logw)
	if _, ok := m["mock"].(MockProvider); !ok {
		t.Fatalf("seeded mock replaced: %T", m["mock"])
	}
	if !bytes.Contains(logw.Bytes(), []byte("reserved")) {
		t.Fatalf("log=%s", logw.String())
	}
}

func TestDiscoveryNothingOnStdout(t *testing.T) {
	// Both probe output and discovery logs must stay off the protocol stdout.
	dir := t.TempDir()
	writeScript(t, dir, "noisy", `echo noisy; echo noisy-err >&2; exit 0`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	withTemplate(t, "noisy")
	r, w, _ := os.Pipe()
	old := os.Stdout
	t.Cleanup(func() { os.Stdout = old; _ = r.Close(); _ = w.Close() })
	os.Stdout = w
	var logw bytes.Buffer
	_ = discoverProviders([]string{"noisy"}, []string{"PATH"}, &logw)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if buf.Len() != 0 {
		t.Fatalf("stdout polluted: %q", buf.String())
	}
}

func TestAllowlistedEnvDenylist(t *testing.T) {
	t.Setenv("FAKE_AGENT_X", "leak")
	t.Setenv("VIBE_SOMETHING", "leak")
	t.Setenv("PATH_TEST_OK", "keep")
	got := allowlistedEnv([]string{"FAKE_AGENT_X", "VIBE_SOMETHING", "PATH_TEST_OK"})
	joined := ""
	for _, e := range got {
		joined += e + "\n"
	}
	if !contains(joined, "PATH_TEST_OK=keep") {
		t.Fatalf("dropped allowed var: %q", joined)
	}
	if contains(joined, "FAKE_AGENT_X") || contains(joined, "VIBE_SOMETHING") {
		t.Fatalf("denylist bypassed: %q", joined)
	}
}

func TestAllowlistEnvOverrideStillHonoursDenylist(t *testing.T) {
	t.Setenv("VIBE_AGENT_ENV_ALLOWLIST", "FAKE_AGENT_X,PATH")
	t.Setenv("FAKE_AGENT_X", "leak")
	gotNames := allowlistFromEnv()
	if len(gotNames) != 2 || gotNames[0] != "FAKE_AGENT_X" || gotNames[1] != "PATH" {
		t.Fatalf("override names=%v", gotNames)
	}
	got := allowlistedEnv(gotNames)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "FAKE_AGENT_X=") {
		t.Fatalf("denylist bypassed: %q", joined)
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func killProbePID(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err == nil && pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func assertProbePidGone(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		t.Fatalf("bad pid file %s: %q", path, b)
	}
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			if err == syscall.ESRCH {
				return
			}
			t.Fatalf("cannot probe pid %d: %v", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive", pid)
}
