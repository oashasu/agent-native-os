//go:build unix

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeBin resolves the built fixture; `bash "scripts/build.sh"` must have run.
func fakeBin(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// Resolve from this source file, not from the test process working directory.
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	p, err := filepath.Abs(filepath.Join(root, ".bin", "fake-agent-cli"))
	if err != nil || func() bool { _, e := os.Stat(p); return e != nil }() {
		t.Skipf("fixture not built at %s (run scripts/build.sh)", p)
	}
	return p
}

func runReal(t *testing.T, p RealProvider, spec RunSpec) RunResult {
	t.Helper()
	res, _ := runRealCollect(t, p, spec, "")
	return res
}

func runRealWithPID(t *testing.T, pidFile string, p RealProvider, spec RunSpec) RunResult {
	t.Helper()
	res, _ := runRealCollect(t, p, spec, pidFile)
	return res
}

func startReal(t *testing.T, p RealProvider, spec RunSpec, pidFile string) (context.CancelFunc, <-chan Frame, <-chan RunResult) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	out := make(chan Frame, 256)
	resCh := make(chan RunResult, 1)
	go func() {
		defer close(done)
		resCh <- p.Run(ctx, spec, out)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("RealProvider cleanup timed out")
		}
		killRecordedPID(pidFile)
	})
	return cancel, out, resCh
}

func runRealCollect(t *testing.T, p RealProvider, spec RunSpec, pidFile string) (RunResult, []Frame) {
	t.Helper()
	_, out, resCh := startReal(t, p, spec, pidFile)
	var frames []Frame
	for f := range out { // drain concurrently
		// Keep the full stream for tests that assert ordering or truncation.
		frames = append(frames, f)
	}
	select {
	case r := <-resCh:
		return r, frames
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return")
		return RunResult{}, frames
	}
}

func tmpl(extra ...string) func(RunSpec) []string {
	return func(s RunSpec) []string {
		a := []string{"--cd", s.WorkspacePath}
		return append(a, extra...)
	}
}

func TestCodexArgvIsExact(t *testing.T) {
	got := codexArgv(RunSpec{WorkspacePath: "/workspace", Prompt: "harden the parser"})
	want := []string{"exec", "--cd", "/workspace", "--approve-for-me", "--skip-git-repo-check", "--json", "--color", "never", "--", "harden the parser"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codex argv = %#v, want %#v", got, want)
	}
}

func TestRealProviderCompletedAndWorkspaceChange(t *testing.T) {
	bin := fakeBin(t)
	ws := t.TempDir()
	p := RealProvider{name: "fake", bin: bin, env: []string{}, argv: tmpl("--write", "Calc.java", "--line", "// hardened", "--exit", "0", "--", "harden")}
	res, collected := runRealCollect(t, p, RunSpec{WorkspacePath: ws, Prompt: "harden"}, "")
	frames := len(collected)
	sawStdout, sawStderr := false, false
	last := -1
	for _, f := range collected {
		if f.Index <= last {
			t.Fatalf("Index not monotonic: %d after %d", f.Index, last)
		}
		last = f.Index
		switch f.Kind {
		case "stdout":
			sawStdout = true
		case "stderr":
			sawStderr = true
		}
	}
	if res.Status != StatusCompleted {
		t.Fatalf("status=%s", res.Status)
	}
	if frames == 0 || !sawStdout || !sawStderr {
		t.Fatalf("frames=%d stdout=%v stderr=%v", frames, sawStdout, sawStderr)
	}
	var m map[string]any
	_ = json.Unmarshal(res.ProviderMeta, &m)
	if m["exit_code"] != float64(0) {
		t.Fatalf("meta=%s", res.ProviderMeta)
	}
	b, _ := os.ReadFile(filepath.Join(ws, "Calc.java"))
	if !strings.Contains(string(b), "// hardened") {
		t.Fatalf("workspace not changed: %q", b)
	}
}

func TestRealProviderMockKnobsIgnored(t *testing.T) {
	bin := fakeBin(t)
	ws := t.TempDir()
	// Use the production codex template here; the fake binary ignores the
	// codex-only flags but still honours --cd/--/prompt.
	p := RealProvider{name: "fake", bin: bin, env: []string{}, argv: codexArgv}
	res := runReal(t, p, RunSpec{WorkspacePath: ws, Prompt: "x", MockWriteFile: "should-not-exist.txt", MockWriteContent: "nope\n"})
	if res.Status != StatusCompleted {
		t.Fatalf("status=%s", res.Status)
	}
	if _, err := os.Stat(filepath.Join(ws, "should-not-exist.txt")); err == nil {
		t.Fatal("mock knob leaked into RealProvider")
	}
}

func TestRealProviderMetaRedacted(t *testing.T) {
	bin := fakeBin(t)
	p := RealProvider{name: "fake", bin: bin, env: []string{"SECRET=xyz"}, argv: tmpl("--exit", "0", "--", "x")}
	res := runReal(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"})
	var m map[string]any
	if err := json.Unmarshal(res.ProviderMeta, &m); err != nil {
		t.Fatalf("meta: %v", err)
	}
	if len(m) != 2 || m["provider"] != "fake" {
		t.Fatalf("meta keys = %v", m)
	}
	if _, ok := m["exit_code"]; !ok {
		t.Fatalf("no exit_code: %v", m)
	}
	if strings.Contains(string(res.ProviderMeta), "SECRET") || strings.Contains(string(res.ProviderMeta), bin) || strings.Contains(string(res.ProviderMeta), "--cd") {
		t.Fatalf("meta leaks: %s", res.ProviderMeta)
	}
}

func TestRealProviderNonZeroExitFails(t *testing.T) {
	bin := fakeBin(t)
	p := RealProvider{name: "fake", bin: bin, env: []string{}, argv: tmpl("--exit", "7", "--", "x")}
	res := runReal(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"})
	if res.Status != StatusFailed {
		t.Fatalf("status=%s", res.Status)
	}
	var m map[string]any
	_ = json.Unmarshal(res.ProviderMeta, &m)
	if m["exit_code"] != float64(7) {
		t.Fatalf("exit_code=%v", m["exit_code"])
	}
}

func TestRealProviderDeadlineTimeout(t *testing.T) {
	bin := fakeBin(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	p := RealProvider{name: "fake", bin: bin, env: []string{}, timeout: 50 * time.Millisecond,
		argv: tmpl("--pid-file", pidFile, "--sleep", "5000", "--exit", "0", "--", "x")}
	start := time.Now()
	res := runRealWithPID(t, pidFile, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"})
	if res.Status != StatusTimeout {
		t.Fatalf("status=%s", res.Status)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout took too long")
	}
	assertPidGone(t, pidFile)
}

func TestRealProviderContextCancel(t *testing.T) {
	bin := fakeBin(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	p := RealProvider{name: "fake", bin: bin, env: []string{},
		argv: tmpl("--pid-file", pidFile, "--sleep", "5000", "--exit", "0", "--", "x")}
	cancel, out, resCh := startReal(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"}, pidFile)
	select {
	case <-out:
	case <-time.After(5 * time.Second):
		t.Fatal("provider emitted no frame")
	}
	cancel()
	for range out {
	}
	res := <-resCh
	if res.Status != StatusCancelled {
		t.Fatalf("status=%s", res.Status)
	}
	assertPidGone(t, pidFile)
}

func TestRealProviderKillsProcessGroup(t *testing.T) {
	// The direct child is /bin/sh; the sleep descendant proves that the unix
	// implementation kills the process group, not merely the direct child.
	pidFile := filepath.Join(t.TempDir(), "child-pid")
	p := RealProvider{name: "shell", bin: "/bin/sh", env: []string{}, timeout: 500 * time.Millisecond,
		argv: func(RunSpec) []string { return []string{"-c", `/bin/sleep 60 & echo $! > "$1"; wait`, "sh", pidFile} }}
	res := runRealWithPID(t, pidFile, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"})
	if res.Status != StatusTimeout {
		t.Fatalf("status=%s", res.Status)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("descendant did not start: %v", err)
	}
	assertPidGone(t, pidFile)
}

func TestRealProviderStartFailure(t *testing.T) {
	p := RealProvider{name: "fake", bin: "/nonexistent/xyz", env: []string{}, argv: tmpl("--", "x")}
	res := runReal(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"})
	if res.Status != StatusFailed {
		t.Fatalf("status=%s", res.Status)
	}
	if !strings.Contains(string(res.ProviderMeta), `"exit_code":null`) {
		t.Fatalf("expected null exit_code, meta=%s", res.ProviderMeta)
	}
}

func TestRealProviderFailClosedOnNilEnvOrArgv(t *testing.T) {
	bin := fakeBin(t)
	for _, p := range []RealProvider{
		{name: "fake", bin: bin, env: nil, argv: tmpl("--", "x")},
		{name: "fake", bin: bin, env: []string{}, argv: nil},
		{name: "fake", bin: bin, env: []string{}, argv: func(RunSpec) []string { return nil }},
	} {
		res := runReal(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"})
		if res.Status != StatusFailed || !strings.Contains(string(res.ProviderMeta), `"exit_code":null`) {
			t.Fatalf("not fail-closed: %+v", res)
		}
	}
}

func TestRealProviderOversizedLine(t *testing.T) {
	bin := fakeBin(t)
	p := RealProvider{name: "fake", bin: bin, env: []string{}, argv: tmpl("--emit-bytes", "1572864", "--exit", "0", "--", "x")}
	res, frames := runRealCollect(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"}, "")
	sawTrunc := false
	for _, f := range frames {
		if len(f.Text) > maxFrameText {
			t.Fatalf("frame text %d > cap", len(f.Text))
		}
		if strings.HasSuffix(f.Text, "…[truncated]") {
			sawTrunc = true
		}
	}
	if res.Status != StatusCompleted || !sawTrunc {
		t.Fatalf("status=%s sawTrunc=%v", res.Status, sawTrunc)
	}
}

func TestRealProviderExactLimitLineIsNotTruncated(t *testing.T) {
	bin := fakeBin(t)
	p := RealProvider{name: "fake", bin: bin, env: []string{}, argv: tmpl("--emit-bytes", strconv.Itoa(maxFrameText), "--exit", "0", "--", "x")}
	res, frames := runRealCollect(t, p, RunSpec{WorkspacePath: t.TempDir(), Prompt: "x"}, "")
	sawExact := false
	for _, f := range frames {
		if len(f.Text) == maxFrameText {
			sawExact = true
			if strings.HasSuffix(f.Text, truncMark) {
				t.Fatal("exact-limit line was marked truncated")
			}
		}
	}
	if res.Status != StatusCompleted || !sawExact {
		t.Fatalf("status=%s sawExact=%v", res.Status, sawExact)
	}
}

func killRecordedPID(pidFile string) {
	if pidFile == "" {
		return
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err == nil && pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func assertPidGone(t *testing.T, pidFile string) {
	t.Helper()
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return // process was killed before it could write the file — acceptable
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return
	}
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			if err == syscall.ESRCH { // gone
				return
			}
			t.Fatalf("cannot probe pid %d: %v", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive", pid)
}
