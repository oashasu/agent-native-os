# M1.8 — Agent Adapter: Real Provider #1 (codex) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `RealProvider` (a subprocess-driven `Provider`) with `codex` as real provider #1, discovered at plugin startup and selectable per run; upgrade `agent.run.cancel` to actually stop the running provider. `mock` stays the default so `smoke.sh` / `qualify-done-integrity.sh` are unchanged. The real end-to-end path is a local, credential-gated manual script; the dispatched work is deterministic plumbing exercised via a committed fake CLI.

**Architecture:** A new `RealProvider` struct runs an allowlisted subprocess in the workspace directory, streams its stdout/stderr as opaque `Frame`s, and maps `{exit code, ctx deadline, ctx cancel, start error}` to `{COMPLETED, TIMEOUT, CANCELLED, FAILED}`. Runtime discovery probes each configured candidate with `<bin> --version` and registers a `RealProvider` only for names that have a built-in argv template (`codex`). A run registry maps `run_id → (cancelFunc, done)` so `agent.run.cancel` cancels the provider's context and waits for the terminal record. `engineering-workflow` and `vibe` gain an optional `provider` passthrough (empty ⇒ `"mock"`).

**Tech Stack:** Go **1.19** standard library only (`os/exec`, `bufio`, `syscall`, `context`) + `kernel/sdk/go/{protocol,pluginhost,fencing}` via `go.work`. No new external Go dependency. Newline-delimited JSON over a Unix socket. Python 3 + `jsonschema` for the contract check.

**Spec:** `docs/superpowers/specs/2026-09-01-m1-8-real-provider-adapter-design.md` — read it alongside this plan. §2 (design invariants), §4 (components), §5 (codex argv), §6 (tests), §11 (known limitations) are load-bearing.

## Global Constraints

- **Toolchain: Go 1.19.** `plugins/go.mod` / `cli/go.mod` declare `go 1.19`; the dev machine has go1.19.1. **No Go 1.20+ API** — in particular `exec.Cmd.Cancel`, `exec.Cmd.WaitDelay`, `errors.Join`, `context.WithoutCancel`. If a step seems to need one, stop and report. Verify once at the start: `go version` (expect `go1.19.x`).
- **Provider-neutral.** No `codex`/`claude` vocabulary in any contract or in `agent.run@1` / `workflow.engineering.run@1` handler logic. `provider` is a selection hint only — it never carries a path, argv, env, or credential.
- **Empty `provider` ⇒ `mock`. Unknown `provider` ⇒ `INVALID` before any store write.**
- **`RealProvider` does not parse codex event semantics** — stdout lines are opaque, even with `--json`.
- **Real-provider `provider_metadata` is redacted:** exactly `{"provider": <name>, "exit_code": <int|null>}`. Never `bin`, full `args`, env values, credentials, or the prompt. `AgentRun.Prompt` (the existing field) is untouched.
- **`mock` stays the default everywhere.** `smoke.sh`, `qualify-done-integrity.sh`, `check-arch.sh` are **not** modified. Contract count stays **31**, manifest count **10**.
- **G1 Kernel Purity:** no task modifies `kernel/`. Check: `git diff --name-only "$BASE" HEAD -- "kernel"` must be empty.
- **No `docs/M1-DESIGN.md` edit.** The reviewer reconciles §6/§8/§13 post-merge.
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **`go build ./cli/...` may drop `./vibe` in cwd** — `rm -f "./vibe"`, do not commit (`.gitignore` has `/vibe`).
- **Commit format** — every commit subject must follow the supplied `AGENTS.md` rule: `[中文模块][英文类型][中文摘要]`; commit备注使用中文。Every commit message also ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
  ```
  Use a Chinese模块名, an English类型标识 such as `add`, `fix`, or `chore`, and a Chinese摘要. Do not use English conventional-commit subjects. Author `ada <oashasu@gmail.com>` (connector may substitute — known limit).

**Base:** branch `chatgpt/m1-8-real-provider-adapter` from the tarball snapshot commit. Before Task 1: `BASE=$(git rev-parse HEAD)`; use `$BASE` for every G1 check. Do not hardcode a SHA. **`plugins/agent-harness/provider_test.go`'s race fix and this plan + the spec are already committed at `$BASE`** — they will not appear in `git diff "$BASE" HEAD`.

---

## Background the executor needs

**Current `Provider` surface** (`plugins/agent-harness/provider.go`):

```go
type Frame struct { Kind, Text string; Index int }   // Kind ∈ {"stdout","stderr"}
type RunSpec struct {
	WorkspacePath, Prompt string
	MockSteps, MockDelayMS, MockFailAt int
	MockWriteFile, MockWriteContent string
}
type RunResult struct { Status, NativeID string; ProviderMeta json.RawMessage }
type Provider interface {
	Name() string
	Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult
}
type MockProvider struct{}   // Name() == "mock"
```

**Status constants** (`store.go`): `StatusRunning`="RUNNING", `StatusCompleted`="COMPLETED", `StatusFailed`="FAILED", `StatusCancelled`="CANCELLED", `StatusTimeout`="TIMEOUT".

**`runProvider`** (`session.go`) mirrors frames to a `chan<- any` with a 2 s per-frame bounded send, then returns a `Transcript{Frames []Frame; Result RunResult}`. `Transcript.Bytes()` = one JSON line per frame + `"== result: <status> ==\n"`.

**`runDeps` + `runOnce`** (`handlers.go`): today

```go
type runDeps struct {
	Store   *Store
	Prov    Provider
	BlobPut func(payload []byte) (uri string, err error)
	Persist func(runID, status, rawRef string, frames int, meta json.RawMessage) error
	Now     func() string
}
func runOnce(ctx context.Context, d runDeps, ar AgentRun, spec RunSpec, out chan any) {
	tr := runProvider(ctx, d.Prov, spec, out)
	uri, err := d.BlobPut(tr.Bytes()); if err != nil { uri = "" }
	_ = d.Persist(ar.ID, tr.Result.Status, uri, len(tr.Frames), tr.Result.ProviderMeta)
}
```

`agentRunHandler(deps) pluginhost.ContextHandler` currently: rejects `provider != "" && != "mock"`, sets `ar.HarnessNativeID = "mock-" + runID`, `RecordStarted`, `go runOnce(rc.Context(), d, ar, spec, out)`, `rc.Stream(out)`.

**`Store`** (`store.go`): `RecordStarted(ar)`, `RecordCompleted(id, status, rawRef, frames, meta)` (requires `RUNNING`, else `ErrAlreadyTerminal`), `RecordCancelled(id)` (requires `RUNNING`), `GetByID(id) (AgentRun, bool)`, `QueryByContext(wc) []AgentRun`. Ops on the JSONL log: `agent.run.started`, `agent.run.completed` (arbitrary status string), `agent.run.cancelled`.

**`cancelHandler(s *Store)`** (`handlers.go`) is wired in `main.go` via `wrap(cancelHandler(s))` and the test drives it with a fenced envelope.

**`engineering-workflow`** (`pipeline.go`): `caps.AgentRun func(wcID, wsPath, prompt, writeFile, writeContent string) (id, status string, err error)`; `RunRequest` has `MockAgentWriteFile` / `MockAgentWriteContent`; `runPipeline` calls `c.AgentRun(wc, wsPath, req.Prompt, req.MockAgentWriteFile, req.MockAgentWriteContent)`. `realCaps` (`handlers.go`) builds the `agent.run` payload with `"provider": "mock"` hard-coded.

**`vibe` CLI** (`cli/vibe/main.go`): `command(cap, payload)` sets a fixed 30 s `Deadline`. `agentRun` builds `payload["provider"] = "mock"` + mock knobs, `command("agent.run", …)`. `agentCancel` → `command("agent.run.cancel", …)`. `workflowRun` has a `-timeout` flag already, no `-provider`.

**`scripts/build.sh`** builds `.bin/{vibe-kernel,vibe-raw,vibe}` + `plugins/bin/<name>` for each `plugins/*/` with a `main.go`. It does **not** recurse into `plugins/agent-harness/fakeagentcli/`.

**`//go:build unix`** is supported by Go 1.19 (matches darwin, linux, *bsd; not windows/plan9/js).

---

## File Structure

New (all under `plugins/agent-harness/` unless noted):
- `fakeagentcli/main.go` — deterministic fake coding CLI; everything via argv, no env. Built to `.bin/fake-agent-cli`.
- `real_provider.go` — `RealProvider` struct + `Run` + `codexArgv` + `redactedMeta`.
- `real_provider_exec_unix.go` (`//go:build unix`) / `real_provider_exec_other.go` (`//go:build !unix`) — `realProviderSupported()`, `startProcess(*exec.Cmd) error`, `killProcessGroup(*exec.Cmd)`.
- `real_provider_test.go` (`//go:build unix`) — subprocess cases via `.bin/fake-agent-cli`.
- `discovery.go` — `discoverProviders`, `argvTemplates`, `allowlistedEnv`, `candidatesFromEnv`, `allowlistFromEnv`, default lists.
- `discovery_test.go` (`//go:build unix`) — probe cases via throwaway PATH scripts.
- `runreg.go` — `runRegistry` / `runEntry`.
- `runreg_test.go` — registry semantics, no real provider.
- `scripts/verify-real-provider.sh` — reviewer-only real codex check (`0755`, guarded).

Modified:
- `plugins/agent-harness/handlers.go` — `runDeps`, `runOnce(prov, …)`, `startAgentRun` helper, `agentRunHandler`, `cancelHandler(s, runs)`.
- `plugins/agent-harness/handlers_test.go` — update fixtures to the new `runDeps`; provider-selection + live-cancel cases.
- `plugins/agent-harness/session.go` — `runProvider` mirror select gains `<-ctx.Done()`.
- `plugins/agent-harness/session_test.go` — bounded mirror-cancel regression.
- `plugins/agent-harness/main.go` — discovery wiring, `cancelHandler(s, runs)` wiring.
- `plugins/engineering-workflow/pipeline.go` — `caps.AgentRun` signature, `RunRequest.Provider`, forward in `runPipeline`.
- `plugins/engineering-workflow/handlers.go` — `realCaps` + `agentRunPayload` helper.
- `plugins/engineering-workflow/handlers_test.go` — `agentRunPayload` provider assertion.
- `plugins/engineering-workflow/pipeline_test.go` — new `caps.AgentRun` arity in fakes + provider-forwarding test.
- `cli/vibe/main.go` — `commandWithDeadline`; `agentRun` `-provider`+`-timeout`; `agentCancel` deadline; `workflowRun` `-provider`.
- `contracts/workflow.engineering.run/v1/schema.json` — `+ "provider"` optional.
- `scripts/build.sh` — build `.bin/fake-agent-cli`.

Already present at `BASE` and intentionally not changed by the dispatched tasks:
- `plugins/agent-harness/provider_test.go` — the race-synchronization fix is part of the snapshot; it must not reappear in the implementation diff.

The dispatched diff whitelist contains **22 paths**: 10 new files and 12
modified files above. The already-present `provider_test.go` is excluded from
that count and from the final `git diff --name-only "$BASE" HEAD` assertion.

---

## Task 1: fake-agent-cli fixture + build wiring

**Files:**
- Create: `plugins/agent-harness/fakeagentcli/main.go`
- Modify: `scripts/build.sh`

**Interfaces:**
- Produces: an executable `.bin/fake-agent-cli` with two forms:
  - `fake-agent-cli --version` → prints `fake-agent-cli 0.0.1\n`, exit 0.
  - `fake-agent-cli --version-exit N` → prints a version line, exit N.
  - `fake-agent-cli --cd DIR --write RELFILE --line TEXT --emit-bytes N --pid-file FILE --exit N --sleep MS -- PROMPT...` → run form.
- Consumes: nothing.

- [ ] **Step 1: Write the fixture**

Create `plugins/agent-harness/fakeagentcli/main.go`:

```go
// Command fake-agent-cli is a deterministic stand-in for a real coding CLI,
// used only by agent-harness tests. Everything is argv — no environment — so a
// test knob can never leak into a production allowlist.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	// Version forms (for discovery probing).
	for i, a := range args {
		switch a {
		case "--version":
			fmt.Println("fake-agent-cli 0.0.1")
			os.Exit(0)
		case "--version-exit":
			code := 0
			if i+1 < len(args) {
				code, _ = strconv.Atoi(args[i+1])
			}
			fmt.Println("fake-agent-cli 0.0.1")
			os.Exit(code)
		}
	}

	var cd, write, line, pidFile string
	var emitBytes, exitCode, sleepMS int
	var prompt []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cd":
			i++
			cd = get(args, i)
		case "--write":
			i++
			write = get(args, i)
		case "--line":
			i++
			line = get(args, i)
		case "--pid-file":
			i++
			pidFile = get(args, i)
		case "--emit-bytes":
			i++
			emitBytes, _ = strconv.Atoi(get(args, i))
		case "--exit":
			i++
			exitCode, _ = strconv.Atoi(get(args, i))
		case "--sleep":
			i++
			sleepMS, _ = strconv.Atoi(get(args, i))
		case "--":
			prompt = args[i+1:]
			i = len(args)
		default:
			// ignore unknown flags so a test argv template can pass extras
		}
	}

	if cd != "" {
		if err := os.Chdir(cd); err != nil {
			fmt.Fprintln(os.Stderr, "chdir:", err)
			os.Exit(1)
		}
	}
	if pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
	}

	p := strings.Join(prompt, " ")
	fmt.Println("fake-agent-cli: start")
	fmt.Println("fake-agent-cli: prompt=" + p)
	fmt.Fprintln(os.Stderr, "fake-agent-cli: working")

	if emitBytes > 0 {
		buf := make([]byte, emitBytes)
		for i := range buf {
			buf[i] = 'x'
		}
		os.Stdout.Write(buf)
		os.Stdout.Write([]byte{'\n'})
	}
	if write != "" {
		f, err := os.OpenFile(write, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			f.WriteString(line + "\n")
			f.Close()
		}
	}
	fmt.Println("fake-agent-cli: done")

	// Sleep in short slices so SIGKILL/SIGTERM from a process-group kill takes
	// effect promptly (the test observes process-group termination; we do not
	// install a handler).
	for left := sleepMS; left > 0; left -= 20 {
		d := 20
		if left < d {
			d = left
		}
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	os.Exit(exitCode)
}

func get(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
```

- [ ] **Step 2: Wire the build**

In `scripts/build.sh`, after the `( cd "cli/vibe" && … )` block and before `echo "BUILD OK"`, add:

```bash
( cd "plugins/agent-harness/fakeagentcli" && go build -o "$OLDPWD/.bin/fake-agent-cli" . )
echo "built fixture: fake-agent-cli"
```

- [ ] **Step 3: Build and smoke the fixture**

Run:
```bash
set -euo pipefail
bash "scripts/build.sh" >/dev/null
echo BUILD_OK
".bin/fake-agent-cli" --version
set +e
".bin/fake-agent-cli" --version-exit 3
rc=$?
set -e
echo "exit=$rc"
[ "$rc" -eq 3 ]
D="$(mktemp -d)"
".bin/fake-agent-cli" --cd "$D" --write out.txt --line "hello" --emit-bytes 100 --exit 0 -- do a thing
echo "run exit=$?"
wc -c "$D/out.txt"
cat "$D/out.txt"
```
Expected: `BUILD_OK`; `fake-agent-cli 0.0.1`; `exit=3`; `run exit=0`; `out.txt` contains `hello`.

- [ ] **Step 4: Verify `go build ./plugins/...` still green**

Run: `go build "./plugins/..." "./cli/..." && ( cd "kernel" && go build "./..." ) && echo BUILD_OK`
Expected: `BUILD_OK` (the fixture compiles as part of the `plugins` module).

- [ ] **Step 5: Commit**

```bash
rm -f "./vibe"
git add "plugins/agent-harness/fakeagentcli/main.go" "scripts/build.sh"
git commit -m "$(cat <<'EOF'
[代理适配器][add][新增假CLI及构建接线]

新增仅使用argv的确定性假CLI，由构建脚本输出到`.bin/fake-agent-cli`，供RealProvider和发现测试使用，断网派工环境无需真实Agent。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 2: platform exec helpers

**Files:**
- Create: `plugins/agent-harness/real_provider_exec_unix.go`, `plugins/agent-harness/real_provider_exec_other.go`
- Test: fold into `real_provider_test.go` (Task 3); this task's check is `go vet` + a targeted build.

**Interfaces:**
- Produces (package `main`, `plugins/agent-harness`):
  - `func realProviderSupported() bool`
  - `func startProcess(cmd *exec.Cmd) error` — sets platform `SysProcAttr` then `cmd.Start()`
  - `func killProcessGroup(cmd *exec.Cmd)` — best-effort, nil-safe if `cmd.Process == nil`
- Consumes: nothing.

- [ ] **Step 1: unix implementation**

Create `plugins/agent-harness/real_provider_exec_unix.go`:

```go
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
```

- [ ] **Step 2: non-unix stub**

Create `plugins/agent-harness/real_provider_exec_other.go`:

```go
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
```

- [ ] **Step 3: Verify both files compile**

Run:
```bash
set -euo pipefail
go vet "./plugins/agent-harness/"
build_tmp="$(mktemp -d)"
trap 'rm -rf "$build_tmp"' EXIT
GOOS=windows GOARCH=amd64 go build -o "$build_tmp/agent-harness.exe" "./plugins/agent-harness/" && echo WINDOWS_BUILD_OK
go build -o "$build_tmp/agent-harness" "./plugins/agent-harness/" && echo UNIX_BUILD_OK
```
Expected: `go vet` clean; package-level helper functions may be unused before Task 3 and Go still permits that. `WINDOWS_BUILD_OK` and `UNIX_BUILD_OK`.

Note: do not add placeholder references merely to silence an unused-function warning. If a real compile error appears, continue to Task 3 only after recording its cause; Task 2's platform files are committed independently.

- [ ] **Step 4: Commit**

```bash
git add "plugins/agent-harness/real_provider_exec_unix.go" "plugins/agent-harness/real_provider_exec_other.go"
git commit -m "$(cat <<'EOF'
[代理适配器][add][新增进程组平台辅助函数]

Unix实现startProcess(Setpgid)和killProcessGroup(kill-pid)，其他平台提供realProviderSupported()==false的可编译桩，避免注册真实Provider。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 3: RealProvider.Run

**Files:**
- Create: `plugins/agent-harness/real_provider.go`, `plugins/agent-harness/real_provider_test.go`

**Interfaces:**
- Consumes: `startProcess`, `killProcessGroup`, `realProviderSupported` (Task 2); `Frame`, `RunSpec`, `RunResult`, `Provider`, status constants.
- Produces:
  - `type RealProvider struct { name, bin string; argv func(RunSpec) []string; env []string; timeout time.Duration }`
  - `func (p RealProvider) Name() string`
  - `func (p RealProvider) Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult`
  - `func codexArgv(spec RunSpec) []string`
  - `const maxFrameText = 1 << 20`

- [ ] **Step 1: Write `real_provider_test.go` (failing)**

Create `plugins/agent-harness/real_provider_test.go`:

```go
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
	want := []string{"exec", "--cd", "/workspace", "-s", "workspace-write", "--approve-for-me", "--skip-git-repo-check", "--json", "--color", "never", "--", "harden the parser"}
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
		argv: func(RunSpec) []string{"-c", `/bin/sleep 5 & echo $! > "$1"; wait`, "sh", pidFile}}
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
```

Test imports: `context encoding/json os path/filepath reflect runtime strconv strings syscall testing time`.

The exact-argv test is required because the fake-run tests inject their own
argv template and therefore cannot catch a regression in `codexArgv`. The
process-group test uses `/bin/sh` with a `/bin/sleep` descendant and requires
the descendant pid file to exist before asserting that cancellation removes
the whole group; a direct-child-only test would still pass with a no-op group
kill followed by the 3 s direct-child fallback.

- [ ] **Step 2: Run — verify it fails to compile (RealProvider undefined)**

Run:
```bash
set +e
out="$(go test "./plugins/agent-harness/" -run TestRealProvider 2>&1)"
rc=$?
set -e
printf '%s\n' "$out"
[ "$rc" -ne 0 ] || { echo "expected the pre-implementation test build to fail"; exit 1; }
```
Expected: build failure — `undefined: RealProvider` / `undefined: maxFrameText`.

- [ ] **Step 3: Implement `real_provider.go`**

Create `plugins/agent-harness/real_provider.go`:

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxFrameText = 1 << 20 // 1 MiB per frame

const truncMark = "…[truncated]"

// codexArgv builds `codex exec` arguments AFTER the binary. §5 of the spec.
func codexArgv(spec RunSpec) []string {
	return []string{
		"exec",
		"--cd", spec.WorkspacePath,
		"-s", "workspace-write",
		"--approve-for-me",
		"--skip-git-repo-check",
		"--json",
		"--color", "never",
		"--", spec.Prompt,
	}
}

type RealProvider struct {
	name    string
	bin     string
	argv    func(spec RunSpec) []string
	env     []string // NEVER nil at Run time; nil is rejected fail-closed
	timeout time.Duration
}

func (p RealProvider) Name() string { return p.name }

func redactedMeta(name string, exitCode *int) json.RawMessage {
	m := map[string]any{"provider": name}
	if exitCode != nil {
		m["exit_code"] = *exitCode
	} else {
		m["exit_code"] = nil
	}
	b, _ := json.Marshal(m)
	return b
}

func failClosed(name string) RunResult {
	return RunResult{Status: StatusFailed, ProviderMeta: redactedMeta(name, nil)}
}

func (p RealProvider) Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult {
	defer close(out)

	if p.name == "" || p.bin == "" || p.argv == nil || p.env == nil {
		return failClosed(p.name)
	}
	args := p.argv(spec)
	if args == nil {
		return failClosed(p.name)
	}

	runCtx := ctx
	if p.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	if err := runCtx.Err(); err != nil {
		return statusFromCtx(runCtx, p.name)
	}

	cmd := exec.Command(p.bin, args...)
	cmd.Dir = spec.WorkspacePath
	cmd.Env = p.env

	// Ordinary OS pipes: assign the write ends to cmd, keep the read ends for
	// our readers. Do NOT use Cmd.StdoutPipe/StderrPipe — in Go 1.19 Cmd.Wait
	// closes those, and waiting for readers before Wait can deadlock if a
	// descendant inherits the fd.
	orp, owp, e1 := os.Pipe()
	erp, ewp, e2 := os.Pipe()
	if e1 != nil || e2 != nil {
		for _, f := range []*os.File{orp, owp, erp, ewp} {
			if f != nil {
				f.Close()
			}
		}
		return failClosed(p.name)
	}
	cmd.Stdout = owp
	cmd.Stderr = ewp

	if err := startProcess(cmd); err != nil {
		orp.Close()
		owp.Close()
		erp.Close()
		ewp.Close()
		return failClosed(p.name)
	}
	// The child holds its own copies now.
	owp.Close()
	ewp.Close()

	var (
		frameMu   sync.Mutex
		nextIdx   = 0
		readersWG sync.WaitGroup
		stopOnce  sync.Once
		readerStop = make(chan struct{})
	)
	stopReaders := func() {
		stopOnce.Do(func() {
			close(readerStop)
			orp.Close()
			erp.Close()
		})
	}

	emit := func(kind, text string) bool {
		frameMu.Lock()
		defer frameMu.Unlock()
		nextIdx++
		f := Frame{Kind: kind, Text: text, Index: nextIdx}
		select {
		case out <- f:
			return true
		case <-runCtx.Done():
			return false
		case <-readerStop:
			return false
		}
	}

	readPipe := func(r io.Reader, kind string) {
		defer readersWG.Done()
		br := bufio.NewReader(r)
		var acc []byte
		flush := func() bool {
			if len(acc) == 0 {
				return true
			}
			t := strings.TrimSuffix(string(acc), "\r")
			acc = acc[:0]
			return emit(kind, t)
		}
		for {
			chunk, isPrefix, err := br.ReadLine()
			room := maxFrameText - len(acc)
			if room > len(chunk) {
				room = len(chunk)
			}
			if room > 0 {
				acc = append(acc, chunk[:room]...)
			}
			if len(chunk) > room {
				// The extra bytes prove that this logical line exceeds the cap.
				// Do not mark merely because ReadLine returned isPrefix at the
				// exact cap: the next read may contain only the line terminator.
				acc = acc[:maxFrameText-len(truncMark)]
				acc = append(acc, truncMark...)
				if !emit(kind, string(acc)) {
					return
				}
				acc = acc[:0]
				// Discard remaining prefix fragments, including the current
				// fragment's unretained bytes.
				for isPrefix {
					_, isPrefix, err = br.ReadLine()
					if err != nil {
						break
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					select {
					case <-readerStop:
						// forced close — normal
					default:
						emit(kind, "[read error: "+err.Error()+"]")
					}
				}
				flush() // residual with no trailing newline
				return
			}
			if !isPrefix {
				if !flush() {
					return
				}
			}
		}
	}

	readersWG.Add(2)
	go readPipe(orp, "stdout")
	go readPipe(erp, "stderr")

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	const grace = 3 * time.Second
	var processErr error
	select {
	case processErr = <-waitErr:
		// Child exited; give readers a bounded window to drain EOF.
		drained := make(chan struct{})
		go func() { readersWG.Wait(); close(drained) }()
		select {
		case <-drained:
		case <-runCtx.Done():
			stopReaders()
			<-drained
		case <-time.After(grace):
			stopReaders()
			<-drained
		}
	case <-runCtx.Done():
		killProcessGroup(cmd)
		select {
		case processErr = <-waitErr:
		case <-time.After(grace):
			stopReaders()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			processErr = <-waitErr
		}
		stopReaders()
		readersWG.Wait()
	}
	stopReaders() // idempotent; releases descriptors on the normal path too

	return mapStatus(runCtx, p.name, processErr)
}

func statusFromCtx(ctx context.Context, name string) RunResult {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return RunResult{Status: StatusTimeout, ProviderMeta: redactedMeta(name, nil)}
	default:
		return RunResult{Status: StatusCancelled, ProviderMeta: redactedMeta(name, nil)}
	}
}

func mapStatus(ctx context.Context, name string, err error) RunResult {
	if ctx.Err() == context.DeadlineExceeded {
		return RunResult{Status: StatusTimeout, ProviderMeta: redactedMeta(name, nil)}
	}
	if ctx.Err() == context.Canceled {
		return RunResult{Status: StatusCancelled, ProviderMeta: redactedMeta(name, nil)}
	}
	if err == nil {
		z := 0
		return RunResult{Status: StatusCompleted, ProviderMeta: redactedMeta(name, &z)}
	}
	if ee, ok := err.(*exec.ExitError); ok {
		c := ee.ExitCode()
		return RunResult{Status: StatusFailed, ProviderMeta: redactedMeta(name, &c)}
	}
	return RunResult{Status: StatusFailed, ProviderMeta: redactedMeta(name, nil)}
}
```

- [ ] **Step 4: Run the tests green**

Run:
```bash
bash "scripts/build.sh" >/dev/null
go test "./plugins/agent-harness/" -run 'Test(CodexArgv|RealProvider)' -v
go test -race "./plugins/agent-harness/" -run 'Test(CodexArgv|RealProvider)'
```
Expected: all `TestRealProvider*` PASS; `-race` clean.

- [ ] **Step 5: 致残对照 — invert status mapping, watch red, restore**

In `mapStatus`, temporarily delete the `if ee, ok := err.(*exec.ExitError); ok { … }` block. Run `go test "./plugins/agent-harness/" -run TestRealProviderNonZeroExitFails`; the test must FAIL. Restore the exact original block with `apply_patch`, then re-run the test and require PASS. Do not use `git checkout`/`git restore` for the temporary mutation.

- [ ] **Step 6: Commit**

```bash
rm -f "./vibe"
git add "plugins/agent-harness/real_provider.go" "plugins/agent-harness/real_provider_test.go"
git commit -m "$(cat <<'EOF'
[代理适配器][add][新增RealProvider子进程执行适配器]

使用os.Pipe写端连接子进程，规避Go1.19的StdoutPipe等待死锁；使用bufio.ReadLine和1MiB帧上限；按runCtx映射状态并执行进程组终止；非法env/argv安全失败；provider_metadata仅保留provider和exit_code；按§5实现codexArgv。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 4: Runtime discovery

**Files:**
- Create: `plugins/agent-harness/discovery.go`, `plugins/agent-harness/discovery_test.go`

**Interfaces:**
- Consumes: `RealProvider`, `codexArgv`, `realProviderSupported`, `MockProvider`, `Provider`.
- Produces:
  - `func discoverProviders(candidates, envAllowlist []string, logw io.Writer) map[string]Provider`
  - `func candidatesFromEnv() []string` — reads `VIBE_AGENT_PROVIDERS`, default `["codex"]`
  - `func allowlistFromEnv() []string` — reads `VIBE_AGENT_ENV_ALLOWLIST`, default `defaultEnvAllowlist`
  - `func allowlistedEnv(names []string) []string` — non-nil; drops `FAKE_AGENT_*` / `VIBE_*` unconditionally
  - `var argvTemplates map[string]func(RunSpec) []string` = `{"codex": codexArgv}`

- [ ] **Step 1: Write `discovery_test.go` (failing)**

Create `plugins/agent-harness/discovery_test.go`:

```go
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
```

The slow-probe test uses the inline `time.After(8 * time.Second)` bound; do not leave an undefined `timeAfter` placeholder in the committed test.

- [ ] **Step 2: Run — fails (discoverProviders undefined)**

Run:
```bash
set +e
out="$(go test "./plugins/agent-harness/" -run 'TestDiscovery|TestAllowlisted' 2>&1)"
rc=$?
set -e
printf '%s\n' "$out"
[ "$rc" -ne 0 ] || { echo "expected the pre-implementation test build to fail"; exit 1; }
```
Expected: build failure — `undefined: discoverProviders`.

- [ ] **Step 3: Implement `discovery.go`**

Create `plugins/agent-harness/discovery.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var argvTemplates = map[string]func(RunSpec) []string{
	"codex": codexArgv,
}

var defaultEnvAllowlist = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "LC_CTYPE",
	"TERM", "TMPDIR", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR",
	"CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY",
}

func splitList(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func candidatesFromEnv() []string {
	if c := splitList(os.Getenv("VIBE_AGENT_PROVIDERS")); len(c) > 0 {
		return c
	}
	return []string{"codex"}
}

func allowlistFromEnv() []string {
	if c := splitList(os.Getenv("VIBE_AGENT_ENV_ALLOWLIST")); len(c) > 0 {
		return c
	}
	return append([]string(nil), defaultEnvAllowlist...)
}

func denied(name string) bool {
	return strings.HasPrefix(name, "FAKE_AGENT_") || strings.HasPrefix(name, "VIBE_")
}

func allowlistedEnv(names []string) []string {
	out := make([]string, 0, len(names)) // non-nil
	for _, n := range names {
		if denied(n) {
			continue
		}
		if v, ok := os.LookupEnv(n); ok {
			out = append(out, n+"="+v)
		}
	}
	return out
}

func discoverProviders(candidates, envAllowlist []string, logw io.Writer) map[string]Provider {
	m := map[string]Provider{"mock": MockProvider{}}
	logf := func(f string, a ...any) { fmt.Fprintf(logw, "agent-harness: "+f+"\n", a...) }

	if !realProviderSupported() {
		logf("platform has no process-group support; no real providers")
		return m
	}
	env := allowlistedEnv(envAllowlist)

	for _, name := range candidates {
		switch {
		case name == "mock":
			logf("candidate %q is reserved; skipped", name)
			continue
		case argvTemplates[name] == nil:
			logf("no argv template for %q; skipped", name)
			continue
		}
		bin, err := exec.LookPath(name)
		if err != nil {
			logf("provider %q not on PATH; skipped", name)
			continue
		}
		abs, err := filepath.Abs(bin)
		if err != nil {
			logf("provider %q path resolution failed: %v; skipped", name, err)
			continue
		}
		if err := probeVersion(abs, env); err != nil {
			logf("provider %q probe failed: %v; skipped", name, err)
			continue
		}
		m[name] = RealProvider{name: name, bin: abs, argv: argvTemplates[name], env: env, timeout: 0}
		logf("provider %q -> %s (registered)", name, abs)
	}
	return m
}

func probeVersion(bin string, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.Command(bin, "--version")
	cmd.Env = env
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("devnull: %w", err)
	}
	defer devnull.Close()
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	if err := startProcess(cmd); err != nil {
		return err
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case e := <-waitErr:
		return e
	case <-ctx.Done():
		killProcessGroup(cmd)
		select {
		case <-waitErr:
		case <-time.After(3 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitErr
		}
		return fmt.Errorf("--version timed out")
	}
}
```

- [ ] **Step 4: Run green + race**

Run:
```bash
go test "./plugins/agent-harness/" -run 'TestDiscovery|TestAllowlisted' -v
go test -race "./plugins/agent-harness/"
```
Expected: all pass; `-race` clean.

- [ ] **Step 5: 致残 — drop the denylist**

In `allowlistedEnv`, temporarily delete the `if denied(n) { continue }` line. Run `go test "./plugins/agent-harness/" -run 'TestAllowlistedEnvDenylist|TestAllowlistEnvOverrideStillHonoursDenylist'`; the tests must FAIL. Restore with `apply_patch`, then re-run and require PASS. Do not use `git checkout`/`git restore` for the temporary mutation.

- [ ] **Step 6: Commit**

```bash
rm -f "./vibe"
git add "plugins/agent-harness/discovery.go" "plugins/agent-harness/discovery_test.go"
git commit -m "$(cat <<'EOF'
[代理适配器][add][新增运行时Provider发现]

discoverProviders保留mock，对有argv模板且位于PATH的候选执行有界`--version`探测并注册RealProvider；mock保留名、无模板候选不可注入、探测失败仅跳过；环境白名单带不可覆盖的FAKE_AGENT_*/VIBE_*拒绝表；日志只写logw。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 5: Run registry

**Files:**
- Create: `plugins/agent-harness/runreg.go`, `plugins/agent-harness/runreg_test.go`

**Interfaces:**
- Produces:
  - `type runRegistry struct { … }` with `newRunRegistry() *runRegistry`
  - `func (r *runRegistry) register(id string, cancel context.CancelFunc, done chan struct{})`
  - `func (r *runRegistry) stop(id string) (done <-chan struct{}, live bool)` — calls `cancel` exactly once, keeps the entry until `done(id)`
  - `func (r *runRegistry) done(id string)` — unregister

- [ ] **Step 1: Write `runreg_test.go` (failing)**

```go
package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunRegistryStopCancelsOnceKeepsEntry(t *testing.T) {
	r := newRunRegistry()
	var calls int32
	done := make(chan struct{})
	r.register("a", func() { atomic.AddInt32(&calls, 1) }, done)

	type result struct {
		done <-chan struct{}
		live bool
	}
	start := make(chan struct{})
	results := make(chan result, 32)
	doneRO := (<-chan struct{})(done)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			d, live := r.stop("a")
			results <- result{done: d, live: live}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for got := range results {
		if !got.live || got.done != doneRO {
			t.Fatalf("concurrent stop result: live=%v done=%v", got.live, got.done)
		}
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("cancel called %d times", calls)
	}

	r.done("a")
	if _, live := r.stop("a"); live {
		t.Fatal("stop after done() must be non-live")
	}
}

func TestRunRegistryMissingID(t *testing.T) {
	r := newRunRegistry()
	if _, live := r.stop("ghost"); live {
		t.Fatal("missing id is not live")
	}
}
```

- [ ] **Step 2: Run — fails**

Run:
```bash
set +e
out="$(go test "./plugins/agent-harness/" -run TestRunRegistry 2>&1)"
rc=$?
set -e
printf '%s\n' "$out"
[ "$rc" -ne 0 ] || { echo "expected the pre-implementation test build to fail"; exit 1; }
```
Expected: `undefined: newRunRegistry`.

- [ ] **Step 3: Implement `runreg.go`**

```go
package main

import (
	"context"
	"sync"
)

type runEntry struct {
	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}
}

type runRegistry struct {
	mu      sync.Mutex
	entries map[string]*runEntry
}

func newRunRegistry() *runRegistry {
	return &runRegistry{entries: map[string]*runEntry{}}
}

func (r *runRegistry) register(id string, cancel context.CancelFunc, done chan struct{}) {
	r.mu.Lock()
	r.entries[id] = &runEntry{cancel: cancel, done: done}
	r.mu.Unlock()
}

// stop cancels the run's context exactly once and returns its done channel.
// The entry stays registered until done(id) so concurrent cancels all join the
// same live run instead of falling through to the store-only fallback.
func (r *runRegistry) stop(id string) (<-chan struct{}, bool) {
	r.mu.Lock()
	e := r.entries[id]
	r.mu.Unlock()
	if e == nil {
		return nil, false
	}
	e.once.Do(e.cancel)
	return e.done, true
}

func (r *runRegistry) done(id string) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}
```

- [ ] **Step 4: Run green**

Run: `go test "./plugins/agent-harness/" -run TestRunRegistry -v && go test -race "./plugins/agent-harness/" -run TestRunRegistry`
Expected: PASS, race clean.

- [ ] **Step 5: Commit**

```bash
git add "plugins/agent-harness/runreg.go" "plugins/agent-harness/runreg_test.go"
git commit -m "$(cat <<'EOF'
[代理适配器][add][新增运行中任务注册表]

run_id映射到cancelFunc和done；stop()只取消一次并保留entry直到运行协程调用done()，并发取消请求加入同一个运行。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 6: Handler wiring — provider selection

**Files:**
- Modify: `plugins/agent-harness/handlers.go`, `plugins/agent-harness/handlers_test.go`, `plugins/agent-harness/main.go`

**Interfaces:**
- Consumes: `discoverProviders`, `candidatesFromEnv`, `allowlistFromEnv`, `newRunRegistry`, `Provider`.
- Produces:
  - `runDeps` without `Prov`, with `Providers map[string]Provider`, `DefaultProvider string`, `Runs *runRegistry`.
  - `func runOnce(ctx context.Context, prov Provider, d runDeps, ar AgentRun, spec RunSpec, out chan any)`
  - `func startAgentRun(ctx context.Context, base runDeps, e protocol.Envelope, q agentRunRequest, out chan any) (AgentRun, *protocol.Error)` — validates, selects provider (unknown ⇒ INVALID **before** `RecordStarted`), records, registers, launches `runOnce`.

- [ ] **Step 1: Update `runOnce` + `runDeps`; add `startAgentRun`**

In `handlers.go`:

- `runDeps`: remove `Prov Provider`; add
  ```go
  Providers      map[string]Provider
  DefaultProvider string
  Runs           *runRegistry
  ```
- `runOnce` gains `prov Provider` (second param) and uses it: `tr := runProvider(ctx, prov, spec, out)`.
- Replace the body of `agentRunHandler`'s closure after decode with a call into `startAgentRun`. New shape:

```go
func agentRunHandler(base runDeps) pluginhost.ContextHandler {
	return func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		var q agentRunRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "work_context_id, workspace_path and prompt are required"}
		}
		out := make(chan any, 64)
		ar, perr := startAgentRun(rc.Context(), base, e, q, out)
		if perr != nil {
			close(out)
			return nil, perr
		}
		acc := rc.Stream(out)
		return map[string]any{"agent_run": ar, "stream_id": acc.StreamID}, nil
	}
}

func startAgentRun(ctx context.Context, base runDeps, e protocol.Envelope, q agentRunRequest, out chan any) (AgentRun, *protocol.Error) {
	if q.WorkContextID == "" || q.WorkspacePath == "" || q.Prompt == "" {
		return AgentRun{}, &protocol.Error{Code: "INVALID", Message: "work_context_id, workspace_path and prompt are required"}
	}
	name := q.Provider
	if name == "" {
		name = base.DefaultProvider
	}
	if name == "" {
		name = "mock"
	}
	prov, ok := base.Providers[name]
	if !ok {
		return AgentRun{}, &protocol.Error{Code: "INVALID", Message: fmt.Sprintf("unknown provider %q", name)}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if base.Now != nil {
		now = base.Now()
	}
	runID := protocol.NewID("run")
	ar := AgentRun{
		ID: runID, WorkContextID: q.WorkContextID, WorkspacePath: q.WorkspacePath,
		Prompt: q.Prompt, Provider: name, HarnessNativeID: name + "-" + runID,
		Status: StatusRunning, StartedAt: now,
	}
	if err := fencing.WithWriteFence(e, func() error { return base.Store.RecordStarted(ar) }); err != nil {
		return AgentRun{}, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
	}
	d := base
	d.Persist = func(id, status, ref string, n int, meta json.RawMessage) error {
		return fencing.WithWriteFence(e, func() error { return base.Store.RecordCompleted(id, status, ref, n, meta) })
	}
	spec := RunSpec{
		WorkspacePath: q.WorkspacePath, Prompt: q.Prompt,
		MockSteps: q.MockSteps, MockDelayMS: q.MockDelayMS, MockFailAt: q.MockFailAt,
		MockWriteFile: q.MockWriteFile, MockWriteContent: q.MockWriteContent,
	}
	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	if base.Runs != nil {
		base.Runs.register(runID, runCancel, done)
	}
	go func() {
		defer func() {
			runCancel()
			if base.Runs != nil {
				base.Runs.done(runID)
			}
			close(done)
		}()
		runOnce(runCtx, prov, d, ar, spec, out)
	}()
	return ar, nil
}
```

Add `"fmt"` to the imports if not present.

- [ ] **Step 2: Update `handlers_test.go` fixtures + add provider cases**

Replace every `runDeps{... Prov: MockProvider{} ...}` with the new fields. Update `TestRunOncePersistsTerminalRun` to call `runOnce(context.Background(), MockProvider{}, deps, ar, spec, out)`. Add:

```go
// agentDeps builds runDeps whose Store is the SAME one the fenced envelope
// expects (fencedEnv wrote the lease under dir; Load(dir) reads/writes there).
func agentDeps(t *testing.T, dir string, providers map[string]Provider) (runDeps, *Store) {
	t.Helper()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return runDeps{
		Store: s, Providers: providers, DefaultProvider: "mock", Runs: newRunRegistry(),
		Now:     func() string { return "t0" },
		BlobPut: func([]byte) (string, error) { return "blob://sha256/x", nil },
	}, s
}

func TestStartAgentRunProviderSelection(t *testing.T) {
	dir := t.TempDir()
	env := fencedEnv(t, dir)
	deps, _ := agentDeps(t, dir, map[string]Provider{"mock": MockProvider{}})

	out := make(chan any, 64)
	q := agentRunRequest{WorkContextID: "wc-1", WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 2, MockDelayMS: 1}
	ar, perr := startAgentRun(context.Background(), deps, env, q, out)
	if perr != nil {
		t.Fatalf("perr=%+v", perr)
	}
	for range out {
	}
	if ar.Provider != "mock" || ar.HarnessNativeID != "mock-"+ar.ID {
		t.Fatalf("ar=%+v", ar)
	}
}

func TestStartAgentRunUnknownProviderNoRecord(t *testing.T) {
	dir := t.TempDir()
	env := fencedEnv(t, dir)
	deps, store := agentDeps(t, dir, map[string]Provider{"mock": MockProvider{}})
	out := make(chan any, 1)
	_, perr := startAgentRun(context.Background(), deps, env, agentRunRequest{WorkContextID: "wc-1", WorkspacePath: t.TempDir(), Prompt: "p", Provider: "bogus"}, out)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
	if len(store.QueryByContext("wc-1")) != 0 {
		t.Fatal("orphan RUNNING record written for unknown provider")
	}
}
```

Note: `fencedEnv` (existing helper) sets `VIBE_DATA_DIR` and writes a lease; construct the `Store` from the same `dir` so `RecordStarted` under `WithWriteFence` succeeds. If the fenced store plumbing is awkward, follow the exact pattern in the existing `TestRunOncePersistsTerminalRun` / `TestCancelHandlerMarksRunCancelled`.

- [ ] **Step 3: Wire `main.go`**

In `plugins/agent-harness/main.go`, replace `Prov: MockProvider{}` in `deps` with:

```go
Providers:       discoverProviders(candidatesFromEnv(), allowlistFromEnv(), os.Stderr),
DefaultProvider: "mock",
Runs:            newRunRegistry(),
```

Add `"os"` if not imported (it is).

- [ ] **Step 4: Run tests**

Run:
```bash
go build "./plugins/..." "./cli/..." && echo BUILD_OK
go test "./plugins/agent-harness/" -v
go test -race "./plugins/agent-harness/"
```
Expected: `BUILD_OK`; all agent-harness tests pass. Task 6 must leave
`cancelHandler(s *Store)` and its wiring untouched; Task 7 changes that
signature and updates the old cancel test.

- [ ] **Step 5: 致残 — move the unknown-provider check after RecordStarted**

In `startAgentRun`, temporarily move the `prov, ok := …; if !ok { return INVALID }` block to just after `RecordStarted`. Run `go test "./plugins/agent-harness/" -run TestStartAgentRunUnknownProviderNoRecord`; the test must FAIL because an orphan record appears. Restore with `apply_patch`, then re-run and require PASS. Do not use `git checkout`/`git restore` for the temporary mutation.

- [ ] **Step 6: Commit**

```bash
rm -f "./vibe"
git add "plugins/agent-harness/handlers.go" "plugins/agent-harness/handlers_test.go" "plugins/agent-harness/main.go"
git commit -m "$(cat <<'EOF'
[代理适配器][add][接入按运行选择Provider]

runDeps携带Providers、DefaultProvider和运行注册表；查找到的Provider显式传给runOnce；空provider使用mock；未知provider在任何存储写入前返回INVALID；startAgentRun使确定性测试无需伪造RequestContext；main.go接入discoverProviders。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 7: Live cancel

**Files:**
- Modify: `plugins/agent-harness/handlers.go` (`cancelHandler`), `plugins/agent-harness/main.go`, `plugins/agent-harness/handlers_test.go`, `plugins/agent-harness/session.go`, `plugins/agent-harness/session_test.go`

**Interfaces:**
- Produces: `func cancelHandler(s *Store, runs *runRegistry) pluginhost.Handler`.
- Consumes: `runRegistry` (Task 5), `startAgentRun` (Task 6), `fencing`.

- [ ] **Step 1: `session.go` — mirror select honours ctx**

In `session.go`'s `runProvider`, the per-frame mirror loop currently is:

```go
	for f := range frames {
		tr.Frames = append(tr.Frames, f)
		select {
		case mirror <- f:
		case <-time.After(2 * time.Second):
		}
	}
```

Change to:

```go
	for f := range frames {
		tr.Frames = append(tr.Frames, f)
		select {
		case mirror <- f:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
```

(`ctx` is already the first param of `runProvider`.)

- [ ] **Step 2: `session_test.go` — bounded mirror-cancel regression**

Add the `time` import (`context` is already present), then add:

```go
func TestRunProviderStopsMirroringOnCancel(t *testing.T) {
	// A provider that emits one frame, then blocks until ctx is done.
	prov := blockingProvider{emitted: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	mirror := make(chan any) // unbuffered, no consumer
	doneCh := make(chan Transcript, 1)
	go func() { doneCh <- runProvider(ctx, prov, RunSpec{Prompt: "p"}, mirror) }()
	select {
	case <-prov.emitted:
	case <-time.After(1 * time.Second):
		t.Fatal("blocking provider did not emit")
	}
	cancel()
	select {
	case <-doneCh:
	case <-time.After(1 * time.Second):
		t.Fatal("runProvider did not return promptly after cancel")
	}
}

type blockingProvider struct {
	emitted chan struct{}
}

func (blockingProvider) Name() string { return "blocking" }
func (p blockingProvider) Run(ctx context.Context, _ RunSpec, out chan<- Frame) RunResult {
	defer close(out)
	out <- Frame{Kind: "stdout", Text: "one", Index: 1}
	close(p.emitted)
	<-ctx.Done()
	return RunResult{Status: StatusCancelled}
}
```

- [ ] **Step 3: Run — the new test fails on the old 2 s mirror**

If you kept Step 1: it passes. To see it bite, temporarily remove Step 1's `<-ctx.Done()` case, run `go test "./plugins/agent-harness/" -run TestRunProviderStopsMirroringOnCancel` → FAIL (~2 s). Restore the case with `apply_patch`, then re-run and require PASS (<1 s). Do not use `git checkout`/`git restore` for the temporary mutation.

- [ ] **Step 4: Rewrite `cancelHandler`**

Replace `cancelHandler(s *Store)` with:

```go
func cancelHandler(s *Store, runs *runRegistry) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q agentRunIDRequest
		if err := json.Unmarshal(e.Payload, &q); err != nil || q.AgentRunID == "" {
			return nil, &protocol.Error{Code: "INVALID", Message: "agent_run_id is required"}
		}
		ar, ok := s.GetByID(q.AgentRunID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run not found"}
		}
		if ar.Status != StatusRunning {
			return nil, &protocol.Error{Code: "CONFLICT", Message: "already " + ar.Status}
		}

		done, live := runs.stop(q.AgentRunID)
		if live {
			select {
			case <-done:
				final, ok := s.GetByID(q.AgentRunID)
				if !ok {
					return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run disappeared after cancel"}
				}
				if final.Status == StatusRunning {
					return nil, &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider stopped but terminal record was not persisted"}
				}
				return map[string]any{"agent_run": final}, nil
			case <-time.After(30 * time.Second):
				return nil, &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider did not stop within 30s"}
			}
		}

		// Not live: registry lost the run (plugin restarted) or it unregistered
		// between GetByID and stop. Re-check, then fall back to a fenced
		// store-only CANCELLED.
		cur, ok := s.GetByID(q.AgentRunID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "agent run not found"}
		}
		if cur.Status != StatusRunning {
			return nil, &protocol.Error{Code: "CONFLICT", Message: "already " + cur.Status}
		}
		err := fencing.WithWriteFence(e, func() error { return s.RecordCancelled(q.AgentRunID) })
		if err != nil {
			if errors.Is(err, ErrAlreadyTerminal) {
				again, _ := s.GetByID(q.AgentRunID)
				return nil, &protocol.Error{Code: "CONFLICT", Message: "already " + again.Status}
			}
			if errors.Is(err, ErrNotFound) {
				return nil, &protocol.Error{Code: "NOT_FOUND", Message: err.Error()}
			}
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		final, _ := s.GetByID(q.AgentRunID)
		return map[string]any{"agent_run": final}, nil
	}
}
```

Add `"errors"` to imports.

- [ ] **Step 5: `main.go` wiring**

Change `h.HandleContextCommand("agent.run.cancel", 1, wrap(cancelHandler(s)))` to `wrap(cancelHandler(s, deps.Runs))`. (Ensure `deps.Runs` is the same registry passed to `agentRunHandler(deps)`.)

- [ ] **Step 6: `handlers_test.go` — update the old cancel test + add live-cancel**

Update `TestCancelHandlerMarksRunCancelled` to `cancelHandler(s, newRunRegistry())` (empty registry ⇒ not-live ⇒ fenced fallback; existing assertions hold).

Add:

```go
func TestLiveCancelStopsProviderThenPersists(t *testing.T) {
	dir := t.TempDir()
	env := fencedEnv(t, dir)
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	runs := newRunRegistry()
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := fakeBin(t)
	rp := RealProvider{name: "fake", bin: bin, env: []string{},
		argv: func(sp RunSpec) []string {
			return []string{"--cd", sp.WorkspacePath, "--pid-file", pidFile, "--sleep", "10000", "--exit", "0", "--", "x"}
		}}
	deps := runDeps{
		Store: s, Providers: map[string]Provider{"fake": rp}, DefaultProvider: "fake", Runs: runs,
		Now:     func() string { return "t0" },
		BlobPut: func([]byte) (string, error) { return "blob://sha256/x", nil },
	}
	out := make(chan any, 64)
	ar, perr := startAgentRun(context.Background(), deps, env, agentRunRequest{WorkContextID: "wc-1", WorkspacePath: t.TempDir(), Prompt: "x", Provider: "fake"}, out)
	if perr != nil {
		t.Fatalf("start: %+v", perr)
	}
	go func() {
		for range out {
		}
	}()
	// wait for the pid file
	started := false
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(pidFile); err == nil {
			started = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started {
		t.Fatal("fake provider did not start; live cancel path was not exercised")
	}
	env.Payload = protocol.NewPayload(map[string]string{"agent_run_id": ar.ID})
	res, perr := cancelHandler(s, runs)(env)
	if perr != nil {
		t.Fatalf("cancel: %+v", perr)
	}
	b, _ := json.Marshal(res)
	var got struct {
		AgentRun AgentRun `json:"agent_run"`
	}
	_ = json.Unmarshal(b, &got)
	if got.AgentRun.Status != StatusCancelled || got.AgentRun.RawSessionRef == "" {
		t.Fatalf("final: %+v", got.AgentRun)
	}
	assertPidGone(t, pidFile)
}
```

- [ ] **Step 7: Run**

```bash
bash "scripts/build.sh" >/dev/null
go build "./plugins/..." "./cli/..." && echo BUILD_OK
go test "./plugins/agent-harness/" -v
go test -race "./plugins/agent-harness/"
```
Expected: `BUILD_OK`; all pass; `-race` clean.

- [ ] **Step 8: 致残 — drop the `<-done` wait**

In `cancelHandler`'s live branch, temporarily replace the `select { case <-done: … case <-time.After(30s): … }` with an immediate `final, _ := s.GetByID(q.AgentRunID); return map[string]any{"agent_run": final}, nil`. Run `go test "./plugins/agent-harness/" -run TestLiveCancelStopsProviderThenPersists`; the test must FAIL (status still RUNNING / RawSessionRef empty). Restore with `apply_patch`, then re-run and require PASS. Do not use `git checkout`/`git restore` for the temporary mutation.

- [ ] **Step 9: Commit**

```bash
rm -f "./vibe"
git add "plugins/agent-harness/handlers.go" "plugins/agent-harness/handlers_test.go" "plugins/agent-harness/main.go" "plugins/agent-harness/session.go" "plugins/agent-harness/session_test.go"
git commit -m "$(cat <<'EOF'
[代理适配器][fix][实现运行中任务取消]

cancelHandler(s,runs)在活跃路径取消运行上下文，最多等待30秒让运行协程通过既有agent.run.completed写入终态并返回；自然完成竞争由runOnce单写者处理；非活跃路径回退到带围栏的agent.run.cancelled；runProvider镜像选择加入<-ctx.Done()，避免停滞消费者延迟终态写入。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 8: engineering-workflow provider passthrough

**Files:**
- Modify: `plugins/engineering-workflow/pipeline.go`, `plugins/engineering-workflow/handlers.go`, `plugins/engineering-workflow/pipeline_test.go`, `plugins/engineering-workflow/handlers_test.go`

**Interfaces:**
- Produces:
  - `caps.AgentRun func(provider, wcID, wsPath, prompt, writeFile, writeContent string) (id, status string, err error)`
  - `RunRequest.Provider string` (json `"provider"`)
  - `func agentRunPayload(provider, wc, path, prompt, writeFile, writeContent string) map[string]any` in `engineering-workflow` package

- [ ] **Step 1: `pipeline.go` — signature + request field + forward**

- `caps.AgentRun`: change to `func(provider, wcID, wsPath, prompt, writeFile, writeContent string) (string, string, error)`.
- `RunRequest`: add `Provider string \`json:"provider"\`` after `Prompt`.
- In `runPipeline`, change the call:
  ```go
  prov := req.Provider
  if prov == "" {
  	prov = "mock"
  }
  ar, st, err := c.AgentRun(prov, wc, wsPath, req.Prompt, req.MockAgentWriteFile, req.MockAgentWriteContent); res.AgentRunID = ar
  ```

- [ ] **Step 2: `handlers.go` — `agentRunPayload` helper + `realCaps`**

Add near the top of `handlers.go`:

```go
func agentRunPayload(provider, wc, path, prompt, writeFile, writeContent string) map[string]any {
	return map[string]any{
		"work_context_id":   wc,
		"workspace_path":    path,
		"prompt":            prompt,
		"provider":          provider,
		"mock_write_file":   writeFile,
		"mock_write_content": writeContent,
	}
}
```

In `realCaps`, change the `AgentRun` closure signature to `func(provider, wc, path, prompt, writeFile, writeContent string) (string, string, error)` and its first line to:

```go
	stream, accepted, err := rc.CommandStream("agent.run", 1, agentRunPayload(provider, wc, path, prompt, writeFile, writeContent), 30*time.Minute)
```

- [ ] **Step 3: `pipeline_test.go` — fix fake arity + forwarding test**

Every `AgentRun: func(_, _, _, _, _ string) (string, string, error) {` becomes `func(_, _, _, _, _, _ string) (string, string, error) {` (6 params). Add:

```go
func TestRunPipelineForwardsProvider(t *testing.T) {
	f := &fakePipeline{reviews: []ReviewState{approved("art-1", true)}}
	var gotProvider string
	c := f.caps()
	c.AgentRun = func(provider, _, _, _, _, _ string) (string, string, error) {
		gotProvider = provider
		return "ar-1", "COMPLETED", nil
	}
	_ = runPipeline(context.Background(), c, RunRequest{TaskID: "task-1", Prompt: "go", Provider: "codex", BuildCommand: []string{"true"}, TestCommand: []string{"true"}, ReviewPollMS: 1})
	if gotProvider != "codex" {
		t.Fatalf("provider forwarded as %q", gotProvider)
	}

	gotProvider = ""
	c2 := f.caps()
	c2.AgentRun = func(provider, _, _, _, _, _ string) (string, string, error) {
		gotProvider = provider
		return "ar-1", "COMPLETED", nil
	}
	_ = runPipeline(context.Background(), c2, RunRequest{TaskID: "task-1", Prompt: "go", BuildCommand: []string{"true"}, TestCommand: []string{"true"}, ReviewPollMS: 1})
	if gotProvider != "mock" {
		t.Fatalf("empty provider forwarded as %q, want mock", gotProvider)
	}
}
```

- [ ] **Step 4: `handlers_test.go` — payload assertion**

Add:

```go
func TestAgentRunPayloadCarriesProvider(t *testing.T) {
	p := agentRunPayload("codex", "wc-1", "/ws", "prompt", "f", "c")
	if p["provider"] != "codex" {
		t.Fatalf("payload provider = %v", p["provider"])
	}
	p2 := agentRunPayload("mock", "wc-1", "/ws", "prompt", "", "")
	if p2["provider"] != "mock" {
		t.Fatalf("payload provider = %v", p2["provider"])
	}
}
```

- [ ] **Step 5: Run**

```bash
go test "./plugins/engineering-workflow/" -v
```
Expected: all pass, including the two new tests and the existing `TestRunPipeline*` / `TestRunHandler*`.

- [ ] **Step 6: 致残 — hard-code the provider in `agentRunPayload`**

Change `"provider": provider` to `"provider": "mock"` in `agentRunPayload`. Run `go test "./plugins/engineering-workflow/" -run 'TestAgentRunPayloadCarriesProvider|TestRunPipelineForwardsProvider'`. The payload test must FAIL (the pipeline test still passes because it stubs `AgentRun`). Restore with `apply_patch`, then re-run and require PASS. Do not use `git checkout`/`git restore` for the temporary mutation.

- [ ] **Step 7: Commit**

```bash
rm -f "./vibe"
git add "plugins/engineering-workflow/pipeline.go" "plugins/engineering-workflow/handlers.go" "plugins/engineering-workflow/pipeline_test.go" "plugins/engineering-workflow/handlers_test.go"
git commit -m "$(cat <<'EOF'
[工程工作流][add][透传Provider选择]

caps.AgentRun增加首个provider参数；RunRequest.Provider为空时使用mock；realCaps通过agentRunPayload构造agent.run载荷，直接载荷测试可捕获硬编码值。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 9: contract + CLI

**Files:**
- Modify: `contracts/workflow.engineering.run/v1/schema.json`, `cli/vibe/main.go`

- [ ] **Step 1: schema — add optional `provider`**

In `contracts/workflow.engineering.run/v1/schema.json`, add to `request.properties` (keep `additionalProperties: false`, do **not** add to `required`):

```json
      "provider": { "type": "string" },
```

- [ ] **Step 2: contract check**

Run: `python3 "scripts/check-contracts.py" --root "contracts"`
Expected: `CONTRACT CHECK: PASSED (31 contracts, …)` — count unchanged (an added optional property is not a new contract).

- [ ] **Step 3: `cli/vibe/main.go` — `commandWithDeadline` + flags**

Add next to `command`:

```go
func commandWithDeadline(capability string, payload any, timeout time.Duration) protocol.Envelope {
	e := command(capability, payload)
	e.Deadline = time.Now().Add(timeout).Format(time.RFC3339Nano)
	return e
}
```

In `agentRun`: add flags and use the deadline:

```go
	provider := fs.String("provider", "mock", "agent provider (mock | codex | …)")
	timeout := fs.Duration("timeout", 30*time.Second, "overall command deadline")
	…
	payload := map[string]any{
		"work_context_id": wcID,
		"workspace_path":  *workspace,
		"prompt":          *prompt,
		"provider":        *provider,
		"mock_steps":      *steps,
		"mock_fail_at":    *failAt,
	}
	…
	req := commandWithDeadline("agent.run", payload, *timeout)
```

Set `pollUntil := time.Now().Add(*timeout)` before calling `invokeStream`, then
replace `agentRun`'s existing fixed `for i := 0; i < 50; i++` terminal-status
poll with `for time.Now().Before(pollUntil)`. Change `fetchAgentRun` to accept
that absolute deadline and put it in the `agent.run.get` query envelope; call
it as `fetchAgentRun(socket, identity, token, out.AgentRun.ID, pollUntil)` so a
query started near expiry cannot silently fall back to the old unbounded query
deadline.
The real provider can exceed 5 seconds during blob persistence after the
stream closes; the new `-timeout` must bound that final poll as well. Keep the
existing `time.Sleep(100 * time.Millisecond)` cadence and return the existing
timeout error after the deadline.

Change the helper signature from `fetchAgentRun(socket, identity, token, runID string)` to `fetchAgentRun(socket, identity, token, runID string, deadline time.Time)`, add `Deadline: deadline.Format(time.RFC3339Nano)` to its existing query envelope, and leave the existing invoke/decode body unchanged.

In `agentCancel`: use a longer deadline than the plugin's 30 s live-cancel wait:

```go
	resp, err := invoke(socket, identity, token, commandWithDeadline("agent.run.cancel", map[string]string{"agent_run_id": args[0]}, 40*time.Second))
```

In `workflowRun`: add `provider := fs.String("provider", "mock", "agent provider")` and, after the mock knobs:

```go
	if *provider != "" {
		payload["provider"] = *provider
	}
```

- [ ] **Step 4: Build + CLI smoke**

Run:
```bash
go build "./cli/..." && echo BUILD_OK
rm -f "./vibe"
go vet "./cli/..."
```
Expected: `BUILD_OK`, vet clean.

- [ ] **Step 5: Commit**

```bash
git add "contracts/workflow.engineering.run/v1/schema.json" "cli/vibe/main.go"
git commit -m "$(cat <<'EOF'
[命令与契约][add][增加Provider参数和命令期限]

工作流契约增加可选provider且契约数保持31；增加commandWithDeadline、vibe agent run的-provider和-timeout、vibe agent cancel的40秒期限、vibe workflow run的-provider。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 10: verify-real-provider.sh (local, guarded)

**Files:**
- Create: `scripts/verify-real-provider.sh`

- [ ] **Step 1: Write the script**

Create `scripts/verify-real-provider.sh` (`chmod +x`):

```bash
#!/usr/bin/env bash
# M1.8 real-provider check. NOT part of any automated gate. Reviewer runs it
# on a machine with codex installed and authenticated:
#   VIBE_REAL_PROVIDER=codex bash "scripts/verify-real-provider.sh"
set -euo pipefail
[ "${VIBE_REAL_PROVIDER:-}" = "codex" ] || { echo "SKIP: set VIBE_REAL_PROVIDER=codex to run"; exit 0; }
cd "$(dirname "$0")/.."
source "scripts/lib/kernel-harness.sh"
build_bins

command -v codex >/dev/null || { echo "FAIL: codex not on PATH"; exit 1; }

SRC="$DATA/scratch-repo"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc {\n    int add(int a, int b) { return a + b; }\n}\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=test@example.com -c user.name=test -c commit.gpgsign=false commit -q -m init

export VIBE_AGENT_PROVIDERS=codex
restart_kernel

VD=( ".bin/vibe" -socket "$SOCK" -identity "m1-dev" -token "$DEV_TOKEN" )
RAW=( ".bin/vibe-raw" -socket "$SOCK" -identity "m1-dev" -token "$DEV_TOKEN" )
created="$("${VD[@]}" task create -title rp -goal rp -repo "$SRC" -ac AC1=x)"
WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
alloc="$("${VD[@]}" workspace allocate "$WC" -repo "$SRC")"
WSPATH="$(printf '%s\n' "$alloc" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
[ -n "$WSPATH" ] || { echo "FAIL: no workspace path: $alloc"; exit 1; }

set +e
run_out="$("${VD[@]}" agent run "$WC" -workspace "$WSPATH" -provider codex -timeout 5m \
  -prompt 'Add a one-line Javadoc above the add method in Calc.java.' 2>&1)"
run_rc=$?
set -e
printf '%s\n' "$run_out"
[ "$run_rc" -eq 0 ] || { echo "FAIL: agent run exited $run_rc"; exit 1; }
case "$run_out" in *"» "*) : ;; *) echo "FAIL: no streamed frames"; exit 1 ;; esac
RUN_ID="$(printf '%s\n' "$run_out" | sed -n 's/^agent_run \([^ ]*\).*/\1/p')"
[ -n "$RUN_ID" ] || { echo "FAIL: no run id"; exit 1; }

q="$("${RAW[@]}" -cap agent.run.query -kind query -service default-agent-harness -authority agent-runs-main \
  -payload "{\"work_context_id\":\"$WC\"}")"
printf '%s\n' "$q"
if ! query_check="$(printf '%s\n' "$q" | python3 -c 'import json,sys; run_id=sys.argv[1]; runs=json.load(sys.stdin).get("agent_runs", []); assert len(runs)==1 and runs[0].get("id")==run_id and runs[0].get("provider")=="codex"; print("OK")' "$RUN_ID")"; then
  echo "FAIL: agent.run.query did not return exactly the captured codex run: $q"
  exit 1
fi
[ "$query_check" = "OK" ] || { echo "FAIL: agent.run.query validation: $q"; exit 1; }

g="$("${RAW[@]}" -cap agent.run.get -kind query -service default-agent-harness -authority agent-runs-main \
  -payload "{\"agent_run_id\":\"$RUN_ID\"}")"
printf '%s\n' "$g"
if ! get_check="$(printf '%s\n' "$g" | python3 -c 'import json,sys; run_id=sys.argv[1]; run=json.load(sys.stdin).get("agent_run", {}); assert run.get("id")==run_id and run.get("provider")=="codex" and run.get("status")=="COMPLETED"; print("OK")' "$RUN_ID")"; then
  echo "FAIL: agent.run.get did not return the completed codex run: $g"
  exit 1
fi
[ "$get_check" = "OK" ] || { echo "FAIL: agent.run.get validation: $g"; exit 1; }

ds="$(git -C "$WSPATH" diff --stat)"
printf '%s\n' "$ds"
[ -n "$ds" ] || { echo "FAIL: codex made no change"; exit 1; }

if ! BLOB="$(printf '%s\n' "$g" | python3 -c 'import json,sys; ref=json.load(sys.stdin).get("agent_run", {}).get("raw_session_ref", ""); assert ref; print(ref)')"; then
  echo "FAIL: no raw_session_ref: $g"
  exit 1
fi
rb="$("${RAW[@]}" \
  -cap blob.get -kind query -service default-blob -authority blob-main \
  -payload "{\"uri\":\"$BLOB\"}")"
if ! blob_check="$(printf '%s\n' "$rb" | python3 -c 'import base64,json,sys; raw=base64.b64decode(json.load(sys.stdin)["content_base64"]).decode("utf-8", "replace"); assert raw and "== result: COMPLETED ==" in raw; print("OK")')"; then
  echo "FAIL: transcript blob is empty or lacks the assembled result line"
  exit 1
fi
[ "$blob_check" = "OK" ] || { echo "FAIL: transcript blob validation"; exit 1; }

echo "REAL PROVIDER (codex) VERIFY: OK"
```

After writing the file, run `chmod +x "scripts/verify-real-provider.sh"` and keep the executable mode in the commit.

If `vibe workspace allocate` output format differs (`path` token), adjust the `sed` to the actual `vibe workspace allocate` line — check `grep -n 'func workspaceAllocate' -A40 "cli/vibe/main.go"` for the `Printf`.

- [ ] **Step 2: Verify the guard (no codex needed)**

Run: `test -x "scripts/verify-real-provider.sh" && bash "scripts/verify-real-provider.sh"; echo "exit=$?"`
Expected: `SKIP: set VIBE_REAL_PROVIDER=codex to run` and `exit=0`.

- [ ] **Step 3: Commit**

```bash
git add "scripts/verify-real-provider.sh"
git commit -m "$(cat <<'EOF'
[代理适配器][add][新增真实Provider本地验证脚本]

新增仅供Reviewer执行的真实codex检查：在临时工作树运行Agent并断言帧流、provider等于codex、COMPLETED、真实git差异和非空转录blob；未设置VIBE_REAL_PROVIDER=codex时跳过；不接入smoke或check-arch。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
EOF
)"
```

---

## Task 11: Full acceptance + 致残对照 sweep

**Files:** none changed (verification only).

- [ ] **Step 1: Build (incl. fixture)**

```bash
set -euo pipefail
go build "./plugins/..." "./cli/..." && ( cd "kernel" && go build "./..." ) && bash "scripts/build.sh" >/dev/null && echo BUILD_OK
ls -la ".bin/fake-agent-cli"
rm -f "./vibe"
```
Expected: `BUILD_OK`; the fixture exists.

- [ ] **Step 2: Go tests + race**

```bash
set -euo pipefail
go test "./plugins/..." "./plugins/_template" "./cli/..." && ( cd "kernel" && go test "./..." ) && echo GO_TESTS_OK
go test -race "./plugins/agent-harness/" && echo RACE_OK
rm -f "./vibe"
```
Expected: `GO_TESTS_OK`, `RACE_OK`. Confirm `real_provider`, `discovery`, `runreg`, live-cancel, and engineering-workflow provider tests are in the output.

- [ ] **Step 3: Kernel regression**

```bash
set +e
( cd "kernel" && bash "./scripts/build.sh" >/dev/null && python3 "tests/integration/m05_qualification.py" )
rc=$?
set -e
[ "$rc" -eq 0 ] && echo M05_OK || { echo "M05 rc=$rc"; exit 1; }
```
Expected: `M0.5 ADVERSARIAL QUALIFICATION: PASSED`, `M05_OK`.

- [ ] **Step 4: Architecture checks (unchanged)**

```bash
set +e
out="$(bash "scripts/check-arch.sh" 2>&1)"
rc=$?
set -e
printf '%s\n' "$out"
[ "$rc" -eq 0 ] || { echo "check-arch rc=$rc"; exit 1; }
case "$out" in *"CONTRACT CHECK: PASSED (31 contracts"*"COMPOSITION FITNESS: PASSED (10 manifests"*"ARCH CHECKS OK"*) echo ARCH_OK ;; *) echo "unexpected"; exit 1 ;; esac
```
Expected: `ARCH_OK`.

- [ ] **Step 5: DONE-integrity qualification ×3 (must not regress)**

```bash
for i in 1 2 3; do
  set +e
  bash "scripts/qualify-done-integrity.sh" >"/tmp/m18-qual-$i.log" 2>&1
  rc=$?
  set -e
  last="$(tail -1 "/tmp/m18-qual-$i.log")"
  { [ "$rc" -eq 0 ] && [ "$last" = "DONE-INTEGRITY QUALIFICATION: OK" ]; } && echo "qual $i OK" || { echo "qual $i rc=$rc"; cat "/tmp/m18-qual-$i.log"; exit 1; }
done
```
Expected: `qual 1 OK` … `qual 3 OK`.

- [ ] **Step 6: smoke ×5 (mock still default)**

```bash
for i in 1 2 3 4 5; do
  set +e
  bash "scripts/smoke.sh" >"/tmp/m18-smoke-$i.log" 2>&1
  rc=$?
  set -e
  [ "$rc" -eq 0 ] && grep -qx 'M1 SMOKE: PASSED' "/tmp/m18-smoke-$i.log" && ! grep -q FAIL "/tmp/m18-smoke-$i.log" && echo "smoke $i OK" || { echo "smoke $i rc=$rc"; tail -30 "/tmp/m18-smoke-$i.log"; exit 1; }
done
orph="$(ps -axo pid=,comm= | awk '$2 ~ /(^|\/)(vibe-kernel|agent-harness|artifact|blob|engineering-work|event-journal|review|session|tool-runner|work-registry|workspace)([[:space:]]|$)/ {print}')"
[ -z "$orph" ] && echo NO_ORPHANS || { echo "orphans:"; printf '%s\n' "$orph"; exit 1; }
```
Expected: `smoke 1 OK` … `smoke 5 OK`, `NO_ORPHANS`.

- [ ] **Step 7: verify-real-provider.sh guard**

```bash
bash "scripts/verify-real-provider.sh"; echo "exit=$?"
```
Expected: `SKIP…`, `exit=0`.

- [ ] **Step 8: 致残对照 sweep — each red, reverted green**

| # | mutation (file) | command | expected red |
|---|---|---|---|
| M1 | `mapStatus`: delete the `*exec.ExitError` branch — `real_provider.go` | `go test "./plugins/agent-harness/" -run TestRealProviderNonZeroExitFails` | FAIL |
| M2 | `mapStatus`: delete both `ctx.Err()` checks — `real_provider.go` | `go test "./plugins/agent-harness/" -run 'TestRealProviderDeadlineTimeout|TestRealProviderContextCancel'` | FAIL |
| M3 | `killProcessGroup`: make it a no-op — `real_provider_exec_unix.go` | `go test "./plugins/agent-harness/" -run TestRealProviderKillsProcessGroup` | FAIL (the `/bin/sleep` descendant remains alive) |
| M4 | `redactedMeta`: add any `bin` or `args` key — `real_provider.go` | `go test "./plugins/agent-harness/" -run TestRealProviderMetaRedacted` | FAIL |
| M5 | `allowlistedEnv`: delete `if denied(n)` — `discovery.go` | `go test "./plugins/agent-harness/" -run 'TestAllowlistedEnvDenylist|TestAllowlistEnvOverrideStillHonoursDenylist'` | FAIL |
| M6 | `discoverProviders`: remove the `name == "mock"` skip — `discovery.go` | `go test "./plugins/agent-harness/" -run TestDiscoverySkipsMockCandidate` | FAIL |
| M7 | `startAgentRun`: move the unknown-provider check after `RecordStarted` — `handlers.go` | `go test "./plugins/agent-harness/" -run TestStartAgentRunUnknownProviderNoRecord` | FAIL |
| M8 | `cancelHandler` live branch: return immediately instead of `<-done` — `handlers.go` | `go test "./plugins/agent-harness/" -run TestLiveCancelStopsProviderThenPersists` | FAIL |
| M9 | `session.go`: remove `<-ctx.Done()` from the mirror select | `go test "./plugins/agent-harness/" -run TestRunProviderStopsMirroringOnCancel` | FAIL (~2 s) |
| M10 | `agentRunPayload`: `"provider": "mock"` literal — `engineering-workflow/handlers.go` | `go test "./plugins/engineering-workflow/" -run TestAgentRunPayloadCarriesProvider` | FAIL |
| M11 | `codexArgv`: copy `RunSpec.Mock*` into the real-provider argv — `real_provider.go` | `go test "./plugins/agent-harness/" -run 'TestCodexArgvIsExact|TestRealProviderMockKnobsIgnored'` | FAIL (the mock-selected file appears or argv no longer matches the real template) |

Before each mutation, record the exact original hunk. After the test turns red, restore that hunk with `apply_patch`, re-run the same test, and require green. Do not use `git checkout`/`git restore` for temporary mutations. After the sweep: `git status --porcelain` must be empty.

- [ ] **Step 9: G1 + change scope**

```bash
git diff --name-only "$BASE" HEAD -- "kernel"
git diff --name-only "$BASE" HEAD -- "docs/M1-DESIGN.md"
git diff --name-only "$BASE" HEAD
```
Expected: first two empty. Third lists exactly:
```
cli/vibe/main.go
contracts/workflow.engineering.run/v1/schema.json
plugins/agent-harness/discovery.go
plugins/agent-harness/discovery_test.go
plugins/agent-harness/fakeagentcli/main.go
plugins/agent-harness/handlers.go
plugins/agent-harness/handlers_test.go
plugins/agent-harness/main.go
plugins/agent-harness/real_provider.go
plugins/agent-harness/real_provider_exec_other.go
plugins/agent-harness/real_provider_exec_unix.go
plugins/agent-harness/real_provider_test.go
plugins/agent-harness/runreg.go
plugins/agent-harness/runreg_test.go
plugins/agent-harness/session.go
plugins/agent-harness/session_test.go
plugins/engineering-workflow/handlers.go
plugins/engineering-workflow/handlers_test.go
plugins/engineering-workflow/pipeline.go
plugins/engineering-workflow/pipeline_test.go
scripts/build.sh
scripts/verify-real-provider.sh
```
(`plugins/agent-harness/provider_test.go` is already at `$BASE` — it should NOT appear unless a later task touched it.)

- [ ] **Step 10: Open the PR**

Branch `chatgpt/m1-8-real-provider-adapter` → `main`, title **M1.8 — Real Provider Adapter (codex)**. Body: the 10-commit table; raw output of Steps 1–9; the 致残 sweep results (each mutation, command, exact red, green-after-revert); the note that the dispatched agent did **not** run codex (deterministic fixture only) and the real check is `scripts/verify-real-provider.sh` for the reviewer; state that the reviewer will re-run everything and redo the sweep.

---

## Self-Review

**1. Spec coverage**

| Spec item | Task |
|---|---|
| §2.0 Go 1.19, no 1.20+ API | Global Constraints; Task 3 (`os.Pipe`, manual wait), Task 2 |
| §2.1 provider-neutral | Task 8/9 (contract `provider` string only), Task 3 (no codex parsing) |
| §2.2 provider is a hint only | Task 3 (`RealProvider` internal), Task 8 RPC (`agentRunPayload` sends the name string) |
| §2.3 empty⇒mock, unknown⇒INVALID before RecordStarted | Task 6 `startAgentRun` + Task 6 Step 5 致残 |
| §2.4 no event parsing | Task 3 (`codexArgv --json`, opaque lines) |
| §2.5 deterministic status mapping | Task 3 `mapStatus` + Task 3 Step 5 + Task 11 M1/M2 |
| §2.6 mock knobs don't touch RealProvider | Task 3 `TestRealProviderMockKnobsIgnored` |
| §2.7 cancel stops provider before recording | Task 7 `cancelHandler` live path + `TestLiveCancelStopsProviderThenPersists` |
| §2.8 raw = line transcript | Task 3 (`emit` per line) + Task 7 (blob via existing `runOnce`) |
| §2.9 metadata redacted | Task 3 `redactedMeta` + `TestRealProviderMetaRedacted` + Task 11 M4 |
| §3 RPC boundary | Task 8 (`agentRunPayload`), Task 9 (schema optional `provider`) |
| §4.2 RealProvider.Run (pipes, ReadLine, grace, kill, meta) | Task 3 |
| §4.3 discovery (argvTemplates, mock reserved, denylist, DevNull, bounded probe) | Task 4 |
| §4.4 handler (Providers map, startAgentRun, runOnce(prov)) | Task 6 |
| §4.5 live cancel (runEntry.once, 30s wait, race, restart fallback) | Task 5 + Task 7 |
| §4.6 passthrough (caps.AgentRun, RunRequest.Provider, agentRunPayload, CLI flags, `agentCancel` deadline) | Task 8 + Task 9 |
| §4.7 workflow provider assertion | Task 8 Step 3/4 |
| §5 codex argv | Task 3 `codexArgv` |
| §6.1 fixture + tests (all cases) | Task 1 + Task 3 + Task 4 + Task 6 + Task 7 |
| §6.2 verify-real-provider.sh | Task 10 |
| §6.3 致残 sweep | Task 11 Step 8 |
| §7 acceptance | Task 11 |
| §8 file list | File Structure + Task 11 Step 9 |
| §9 NON-GOALS | not implemented by construction (no task adds them) |
| §11 known limitations | documented in spec; no code |

Coverage is complete after the added argv-exactness, descendant-kill, concurrent-stop, cleanup, and exact local query/blob checks.

**2. Placeholder scan** — every implementation code step has literal Go/bash or an exact edit instruction; the `…` markers in the Task 9 excerpt only denote unchanged surrounding code. Every run step has an exact command + expected output. The remaining "adjust the sed if the output format differs" note (Task 10 workspace-allocate) points at a specific verification command, not a TBD; Task 3 resolves the fixture path from the test source and has no environment-dependent adjustment.

**3. Type/name consistency** — `runDeps` fields (`Providers`, `DefaultProvider`, `Runs`) consistent across Tasks 6/7/11. `runOnce(ctx, prov, d, ar, spec, out)` 6-arg consistent (Task 6 def, Task 6/7 tests). `caps.AgentRun(provider, wc, path, prompt, writeFile, writeContent)` 6-arg consistent (Task 8 def, `pipeline_test.go` fakes, `TestRunPipelineForwardsProvider`). `RealProvider{name,bin,argv,env,timeout}` fields consistent (Task 3 def, Task 3/4/6/7 tests). `discoverProviders(candidates, envAllowlist, logw)` consistent (Task 4 def, Task 6 `main.go` call). `cancelHandler(s, runs)` consistent (Task 7 def, `main.go`, tests). `redactedMeta` / `maxFrameText` / `truncMark` / `mapStatus` / `statusFromCtx` all defined in Task 3. `agentRunPayload` defined Task 8, asserted Task 8, mutated Task 11 M10. Status strings match `store.go` constants. `assertPidGone` / `fakeBin` helpers defined in `real_provider_test.go` (Task 3) and reused in `handlers_test.go` (Task 7) — same package, fine.
